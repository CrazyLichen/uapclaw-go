package reasoning

import (
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// logComponent reasoning 包日志组件标识。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// ReasoningPlan 翻译结果：要写入请求的字段 + 告警。
// Python: reasoning.py:89-93 ReasoningPlan（frozen dataclass）
type ReasoningPlan struct {
	// SDKParams 写进请求体顶层的字段
	SDKParams map[string]any
	// ExtraBody 写进 extra_body 的字段
	ExtraBody map[string]any
	// Warnings 翻译过程中的告警文案
	Warnings []string
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// disabledReasoningEfforts 视为「关闭」的 effort 取值。
	// Python: reasoning.py:62 _DISABLED_REASONING_EFFORTS
	disabledReasoningEfforts = map[string]struct{}{"off": {}, "none": {}}

	// legacyReasoningTopLevelFields 遗留的顶层控制字段（存在即与新式 reasoning 冲突）。
	// Python: reasoning.py:36-43 _LEGACY_REASONING_TOP_LEVEL_FIELDS
	legacyReasoningTopLevelFields = []string{
		"reasoning_effort", "thinking", "enable_thinking",
		"thinking_budget", "thinking_strategy", "chat_template_kwargs",
	}

	// legacyReasoningExtraBodyFields 遗留的 extra_body 控制字段。
	// Python: reasoning.py:44-51 _LEGACY_REASONING_EXTRA_BODY_FIELDS
	legacyReasoningExtraBodyFields = []string{
		"reasoning", "reasoning_effort", "thinking", "enable_thinking",
		"thinking_budget", "thinking_strategy",
	}

	// reasoningWireKeys 日志脱敏时允许暴露的 wire 键。
	// Python: reasoning.py:52-60 _REASONING_WIRE_KEYS
	reasoningWireKeys = []string{
		"reasoning", "reasoning_effort", "thinking", "enable_thinking",
		"thinking_budget", "thinking_strategy", "output_config",
	}

	// anthropicManualBudgetByEffort 标签档位 → Anthropic 手动预算。
	// Python: reasoning.py:705-712 _ANTHROPIC_MANUAL_BUDGET_BY_EFFORT
	anthropicManualBudgetByEffort = map[string]int{
		"minimal": 1024, "low": 2048, "medium": 8192,
		"high": 16384, "xhigh": 16384, "max": 16384,
	}

	// vllmDashscopeBudgetByEffort 标签档位 → vLLM/DashScope thinking_budget。
	// Python: reasoning.py:763-770 _VLLM_DASHSCOPE_BUDGET_BY_EFFORT
	vllmDashscopeBudgetByEffort = map[string]int{
		"minimal": 1024, "low": 1024, "medium": 4096,
		"high": 8192, "xhigh": 16384, "max": 32768,
	}

	// openRouterKnownReasoningPrefixes OpenRouter 已知会透传 reasoning 的 vendor 前缀。
	// Python: reasoning.py:901-911 known_reasoning_prefixes
	openRouterKnownReasoningPrefixes = map[string]struct{}{
		"anthropic": {}, "deepseek": {}, "google": {}, "minimax": {},
		"moonshotai": {}, "openai": {}, "poolside": {}, "qwen": {}, "z-ai": {},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveReasoningProtocol 判定客户端使用哪种协议。
// Python: reasoning.py:134-147 resolve_reasoning_protocol
func ResolveReasoningProtocol(clientConfig *llmschema.ModelClientConfig) string {
	if clientConfig == nil {
		return protocolChatCompletions
	}
	provider := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(ProviderIdentity(clientConfig))), "_", "")
	if clientConfig.APIMode == protocolAnthropicMessages || provider == "anthropic" {
		return protocolAnthropicMessages
	}
	if clientConfig.APIMode == protocolResponses ||
		clientConfig.AuthMode == llmschema.AuthModeOpenAIAccountOAuth ||
		provider == "openaiaccount" {
		return protocolResponses
	}
	return protocolChatCompletions
}

// ProviderIdentity 返回 provider 身份（归一前原名优先）。
// Python: reasoning.py:100-103 provider_identity
func ProviderIdentity(clientConfig *llmschema.ModelClientConfig) string {
	if clientConfig == nil {
		return ""
	}
	if legacy := legacyProviderName(clientConfig); legacy != "" {
		return legacy
	}
	return clientConfig.ClientProvider
}

// ResolveReasoningPlan 把厂商中立的 reasoning 意图翻译成协议相关的线上参数。
// Python: reasoning.py:309-351 resolve_reasoning_plan
func ResolveReasoningPlan(
	clientConfig *llmschema.ModelClientConfig,
	modelConfig *llmschema.ModelRequestConfig,
	requestModel string,
	explicit llmschema.ReasoningIntent,
) ReasoningPlan {
	protocol := ResolveReasoningProtocol(clientConfig)
	model := modelName(modelConfig, requestModel)
	provider := providerKeyOf(clientConfig)

	reasoningValue := explicitReasoningValue(modelConfig, explicit)
	currentValues := mergeForLegacyDetection(modelConfig, explicit)
	cfg, raw := llmschema.CoerceReasoningConfig(reasoningValue)

	// 1. 遗留裸 dict：Chat Completions / Responses 原样透传（Anthropic 丢弃）
	// Python: reasoning.py:324-327
	if raw != nil {
		if protocol == protocolChatCompletions || protocol == protocolResponses {
			return ReasoningPlan{SDKParams: map[string]any{"reasoning": raw}}
		}
		return ReasoningPlan{}
	}

	// 2. 未设置
	if cfg == nil {
		return ReasoningPlan{}
	}

	// 3. 遗留控制已存在 → 告警并放弃
	// Python: reasoning.py:332-336
	//   legacy_values = dict(explicit_kwargs) if has_explicit_reasoning else current_values
	//
	// 偏差：上游 has_explicit_reasoning 时只看显式 kwargs、不看 model_config；
	// Go 的 ReasoningIntent 只承载 reasoning 一个键，拿不到"显式 kwargs 的全部键"，
	// 故统一用 currentValues（config Extra ∪ 显式值）。后果是 config 里的遗留控制
	// 也会触发告警——比上游严格一档，但方向更安全（不会把冲突配置发给厂商）。
	if hasLegacyReasoningControls(currentValues) {
		return ReasoningPlan{Warnings: []string{
			"reasoning config ignored because explicit legacy reasoning controls are already set",
		}}
	}

	// 4. 按协议分发
	switch protocol {
	case protocolAnthropicMessages:
		return anthropicReasoningPlan(cfg, clientConfig, model)
	case protocolResponses:
		return openAIResponsesReasoningPlan(cfg)
	default:
		return openAIChatReasoningPlan(provider, model, cfg, clientConfig)
	}
}

// ApplyReasoningPlan 把 plan 写入请求参数。
// Python: reasoning.py:935-962 apply_reasoning_plan
func ApplyReasoningPlan(params map[string]any, plan ReasoningPlan, override bool) {
	for _, w := range plan.Warnings {
		logger.Warn(logComponent).Str("warning", w).Msg("Reasoning config resolution")
	}
	for k, v := range plan.SDKParams {
		if _, exists := params[k]; override || !exists {
			params[k] = v
		}
	}
	if len(plan.ExtraBody) == 0 {
		return
	}
	existing, exists := params["extra_body"]
	if !exists || existing == nil {
		merged := make(map[string]any, len(plan.ExtraBody))
		for k, v := range plan.ExtraBody {
			merged[k] = v
		}
		params["extra_body"] = merged
		return
	}
	m, ok := existing.(map[string]any)
	if !ok {
		logger.Warn(logComponent).Msg("Reasoning extra_body dropped: request extra_body is not a mapping")
		return
	}
	if override {
		for k, v := range plan.ExtraBody {
			m[k] = v
		}
		return
	}
	// 非 override：已有值优先，只补空缺
	for k, v := range plan.ExtraBody {
		if _, has := m[k]; !has {
			m[k] = v
		}
	}
}

// ReasoningRequestControls 从最终请求中提取可安全日志输出的 wire 控制字段。
// Python: reasoning.py:965-981 reasoning_request_controls
func ReasoningRequestControls(params map[string]any) map[string]any {
	controls := make(map[string]any)
	for _, k := range reasoningWireKeys {
		if v, ok := params[k]; ok {
			controls[k] = v
		}
	}
	if eb, ok := params["extra_body"].(map[string]any); ok {
		extra := make(map[string]any)
		for _, k := range reasoningWireKeys {
			if v, ok := eb[k]; ok {
				extra[k] = v
			}
		}
		if len(extra) > 0 {
			controls["extra_body"] = extra
		}
	}
	return controls
}

// ──────────────────────────── 非导出函数（协议分发）────────────────────────────

// providerKeyOf 由客户端配置推断生效的 provider key。
// Python: reasoning.py:105-110 _provider_key
func providerKeyOf(clientConfig *llmschema.ModelClientConfig) string {
	if clientConfig == nil {
		return ""
	}
	return ProviderKey(ProviderIdentity(clientConfig), clientConfig.EndpointProfile, clientConfig.APIBase)
}

// legacyProviderName 读取归一前的 provider 原名（存于 Extra）。
func legacyProviderName(clientConfig *llmschema.ModelClientConfig) string {
	if clientConfig == nil || clientConfig.Extra == nil {
		return ""
	}
	v, _ := clientConfig.Extra["legacy_client_provider"].(string)
	return v
}

// modelName 解析本次请求的模型名。
// Python: reasoning.py:245-246 _model_name
func modelName(modelConfig *llmschema.ModelRequestConfig, requestModel string) string {
	if strings.TrimSpace(requestModel) != "" {
		return strings.TrimSpace(requestModel)
	}
	if modelConfig != nil {
		return strings.TrimSpace(modelConfig.ModelName)
	}
	return ""
}

// explicitReasoningValue 取显式 reasoning：显式传入优先，否则回落 model_config。
// Python: reasoning.py:300-306 _explicit_reasoning_value
func explicitReasoningValue(modelConfig *llmschema.ModelRequestConfig, explicit llmschema.ReasoningIntent) any {
	if explicit.Set {
		return explicit.Value
	}
	if modelConfig != nil {
		return modelConfig.Reasoning
	}
	return nil
}

// mergeForLegacyDetection 合并 model_config 的额外字段与显式值，用于遗留控制检测。
// Python: reasoning.py:273-287 _model_config_extras + _merge_for_legacy_detection
func mergeForLegacyDetection(modelConfig *llmschema.ModelRequestConfig, explicit llmschema.ReasoningIntent) map[string]any {
	merged := make(map[string]any)
	if modelConfig != nil && len(modelConfig.Extra) > 0 {
		for k, v := range modelConfig.Extra {
			merged[k] = v
		}
	}
	if explicit.Set {
		if m, ok := explicit.Value.(map[string]any); ok {
			for k, v := range m {
				merged[k] = v
			}
		}
	}
	return merged
}

// hasLegacyReasoningControls 判定是否存在遗留控制字段（非 nil 即算）。
// Python: reasoning.py:290-297 _has_legacy_reasoning_controls
func hasLegacyReasoningControls(values map[string]any) bool {
	for _, key := range legacyReasoningTopLevelFields {
		if v, ok := values[key]; ok && v != nil {
			return true
		}
	}
	if eb, ok := values["extra_body"].(map[string]any); ok {
		for _, key := range legacyReasoningExtraBodyFields {
			if v, exists := eb[key]; exists && v != nil {
				return true
			}
		}
	}
	return false
}

// isDisabledReasoningEffort 判定 effort 是否表示「关闭」。
// Python: reasoning.py:984-985 _is_disabled_reasoning_effort
func isDisabledReasoningEffort(effort *string) bool {
	if effort == nil {
		return false
	}
	_, disabled := disabledReasoningEfforts[strings.ToLower(strings.TrimSpace(*effort))]
	return disabled
}

// isDeepSeek 判定客户端是否指向 DeepSeek。
// Python: reasoning.py:354-361 _is_deepseek
func isDeepSeek(clientConfig *llmschema.ModelClientConfig) bool {
	if providerKeyOf(clientConfig) == "deepseek" {
		return true
	}
	identity := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(ProviderIdentity(clientConfig))), "_", "-")
	if identity == "deepseek" {
		return true
	}
	if clientConfig == nil {
		return false
	}
	return strings.Contains(strings.ToLower(clientConfig.APIBase), "deepseek.com")
}

// anthropicReasoningPlan Anthropic 协议分发。
// Python: reasoning.py:364-381 _anthropic_reasoning_plan
func anthropicReasoningPlan(
	cfg *llmschema.ReasoningConfig,
	clientConfig *llmschema.ModelClientConfig,
	model string,
) ReasoningPlan {
	provider := providerKeyOf(clientConfig)
	if provider == wireVLLM {
		return vllmEnableThinkingPlan(cfg)
	}
	if profile := FindReasoningProfile(provider, model); profile != nil {
		return profileReasoningPlan(profile, protocolAnthropicMessages, model, cfg)
	}
	return standardAnthropicReasoningPlan(cfg)
}

// standardAnthropicReasoningPlan 标准 Anthropic 编码。
// Python: reasoning.py:384-399 _standard_anthropic_reasoning_plan
func standardAnthropicReasoningPlan(cfg *llmschema.ReasoningConfig) ReasoningPlan {
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
		return ReasoningPlan{SDKParams: map[string]any{"thinking": map[string]any{"type": "disabled"}}}
	}
	hasEffort := cfg.Effort != nil && *cfg.Effort != ""
	hasBudget := cfg.BudgetTokens != nil
	if cfg.Mode == "auto" && !hasEffort && !hasBudget {
		return ReasoningPlan{}
	}
	thinkingType := "adaptive"
	if hasBudget {
		thinkingType = "enabled"
	}
	thinking := map[string]any{"type": thinkingType}
	if hasBudget {
		thinking["budget_tokens"] = *cfg.BudgetTokens
	}
	sdk := map[string]any{"thinking": thinking}
	if hasEffort {
		sdk["output_config"] = map[string]any{"effort": *cfg.Effort}
	}
	return ReasoningPlan{SDKParams: sdk}
}

// openAIResponsesReasoningPlan Responses 协议编码。
// Python: reasoning.py:402-408 _openai_responses_reasoning_plan
func openAIResponsesReasoningPlan(cfg *llmschema.ReasoningConfig) ReasoningPlan {
	if cfg.Mode == "disabled" {
		return ReasoningPlan{SDKParams: map[string]any{"reasoning": nil}}
	}
	payload := make(map[string]any)
	if cfg.Effort != nil && *cfg.Effort != "" {
		payload["effort"] = *cfg.Effort
	}
	if len(payload) == 0 {
		return ReasoningPlan{}
	}
	return ReasoningPlan{SDKParams: map[string]any{"reasoning": payload}}
}

// openAIChatReasoningPlan Chat Completions 协议分发。
// Python: reasoning.py:411-431 _openai_chat_reasoning_plan
func openAIChatReasoningPlan(
	provider, model string,
	cfg *llmschema.ReasoningConfig,
	clientConfig *llmschema.ModelClientConfig,
) ReasoningPlan {
	if provider == wireVLLM {
		// 能力仍来自模型名 fallback 表，这里只强制 wire
		return vllmEnableThinkingPlan(cfg)
	}
	if clientConfig != nil && isDeepSeek(clientConfig) {
		provider = "deepseek"
	}
	if profile := FindReasoningProfile(provider, model); profile != nil {
		return profileReasoningPlan(profile, protocolChatCompletions, model, cfg)
	}
	return reasoningEffortPlan(cfg, "none")
}

// ──────────────────────────── 非导出函数（wire 编码器）────────────────────────────

// profileReasoningPlan 按 profile 的 wire 名分发给具体编码器。
// Python: reasoning.py:434-521 _profile_reasoning_plan
func profileReasoningPlan(profile *ReasoningProfile, protocol, model string, cfg *llmschema.ReasoningConfig) ReasoningPlan {
	wire := profile.OpenAIWire
	if protocol == protocolAnthropicMessages {
		wire = profile.AnthropicWire
	}
	modelKey := strings.ToLower(model)
	capability := ProfileReasoningCapability(profile, protocol)

	disabled := cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort)
	// minimax_m2 在 disabled 判断之前（上游 :452-453）
	if wire == wireMinimaxM2 {
		return ReasoningPlan{ExtraBody: map[string]any{"reasoning_split": true}}
	}
	if disabled && !containsString(capability.Options, "off") {
		return ReasoningPlan{}
	}
	if wire == wireUnsupported || wire == wireAlwaysOn {
		return ReasoningPlan{}
	}

	switch wire {
	case wireStandardOpenAI:
		return reasoningEffortPlan(cfg, "none")
	case wireStandardAnthropic:
		return standardAnthropicReasoningPlan(cfg)
	case wireAnthropicManual:
		return anthropicManualBudgetPlan(cfg)
	case wireOpenRouter:
		return openRouterReasoningPlan(modelKey, cfg)
	case wireQwen38:
		return dashscopeQwen38ReasoningPlan(cfg)
	case wireEnableThinking, wireQianfanToggle:
		return qwenEnableThinkingPlan(cfg)
	case wireDashscopeBudget:
		return dashscopeAnthropicReasoningPlan(cfg)
	case wireQwen38Anthropic:
		return anthropicOutputConfigEffortPlan(cfg, normalizeQwen38Effort)
	case wireDashscopeGlmAnthropic:
		return anthropicOutputConfigEffortPlan(cfg, normalizeGlmEffort)
	case wireThinkingToggle:
		return thinkingTypeTogglePlan(cfg, "enabled")
	case wireAnthropicToggle:
		return anthropicThinkingTogglePlan(cfg)
	case wireDeepSeek:
		return deepseekChatReasoningPlan(cfg, modelKey)
	case wireDeepSeekAnthropic:
		return deepseekAnthropicReasoningPlan(cfg, modelKey)
	case wireModelartsDeepSeek:
		return thinkingTypeWithEffortPlan(cfg, true, func(effort *string) string {
			if effort == nil || *effort == "" {
				return ""
			}
			return "high"
		})
	case wireModelartsDeepSeekAnthropic:
		return anthropicThinkingWithExtraEffortPlan(cfg, func(effort *string) string {
			if effort == nil || *effort == "" {
				return ""
			}
			return "high"
		})
	case wireGlm:
		return thinkingTypeWithEffortPlan(cfg, true, normalizeGlmEffort)
	case wireGlmAnthropic:
		return anthropicThinkingWithExtraEffortPlan(cfg, normalizeGlmEffort)
	case wireKimiK3:
		if protocol == protocolAnthropicMessages {
			return moonshotAnthropicReasoningPlan(model, cfg)
		}
		return moonshotChatReasoningPlan(model, cfg)
	case wireMinimaxM3:
		return minimaxChatReasoningPlan(modelKey, cfg)
	case wireMinimaxM3Anthropic:
		return minimaxAnthropicReasoningPlan(model, cfg)
	case wireVolcengineSeed:
		effort := normalizeVolcengineSeedEffort(cfg.Effort)
		if cfg.Mode == "disabled" {
			return ReasoningPlan{SDKParams: map[string]any{"reasoning_effort": "minimal"}}
		}
		if effort != "" {
			return ReasoningPlan{SDKParams: map[string]any{"reasoning_effort": effort}}
		}
		return ReasoningPlan{}
	}
	return ReasoningPlan{}
}

// deepseekChatReasoningPlan DeepSeek Chat 编码。
// Python: reasoning.py:524-535 _deepseek_chat_reasoning_plan
func deepseekChatReasoningPlan(cfg *llmschema.ReasoningConfig, modelKey string) ReasoningPlan {
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
		return ReasoningPlan{ExtraBody: map[string]any{"thinking": map[string]any{"type": "disabled"}}}
	}
	effort := normalizeDeepSeekEffort(cfg.Effort, modelKey)
	sdk := make(map[string]any)
	extra := make(map[string]any)
	if cfg.Mode == "enabled" {
		extra["thinking"] = map[string]any{"type": "enabled"}
	}
	if effort != "" {
		sdk["reasoning_effort"] = effort
	}
	return ReasoningPlan{SDKParams: sdk, ExtraBody: extra}
}

// deepseekAnthropicReasoningPlan DeepSeek Anthropic 编码。
// Python: reasoning.py:538-548 _deepseek_anthropic_reasoning_plan
func deepseekAnthropicReasoningPlan(cfg *llmschema.ReasoningConfig, modelKey string) ReasoningPlan {
	sdk := make(map[string]any)
	effort := normalizeDeepSeekEffort(cfg.Effort, modelKey)
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
		sdk["thinking"] = map[string]any{"type": "disabled"}
	} else if effort != "" {
		sdk["thinking"] = map[string]any{"type": "enabled"}
		sdk["output_config"] = map[string]any{"effort": effort}
	} else if cfg.Mode == "enabled" {
		sdk["thinking"] = map[string]any{"type": "enabled"}
	}
	return ReasoningPlan{SDKParams: sdk}
}

// normalizeDeepSeekEffort DeepSeek 档位归一。
// Python: reasoning.py:551-563 _normalize_deepseek_effort
func normalizeDeepSeekEffort(effort *string, modelKey string) string {
	if effort == nil {
		return ""
	}
	s := *effort
	switch v := strings.ToLower(strings.TrimSpace(s)); v {
	case "minimal":
		return "low"
	case "medium":
		return "high"
	case "xhigh":
		if strings.Contains(modelKey, "pro") {
			return "max"
		}
		return "high"
	case "low", "high", "max":
		return v
	}
	return ""
}

// normalizeGlmEffort GLM 档位归一。
// Python: reasoning.py:566-576 _normalize_glm_effort
func normalizeGlmEffort(effort *string) string {
	if effort == nil {
		return ""
	}
	s := *effort
	switch v := strings.ToLower(strings.TrimSpace(s)); v {
	case "none", "off", "minimal":
		return "none"
	case "low", "medium", "high":
		return "high"
	case "xhigh", "max":
		return "max"
	}
	return ""
}

// normalizeVolcengineSeedEffort 火山 Seed 档位归一。
// Python: reasoning.py:579-589 _normalize_volcengine_seed_effort
func normalizeVolcengineSeedEffort(effort *string) string {
	if effort == nil {
		return ""
	}
	s := *effort
	switch v := strings.ToLower(strings.TrimSpace(s)); v {
	case "none", "off", "minimal":
		return "minimal"
	case "low", "medium", "high":
		return v
	case "xhigh", "max":
		return "high"
	}
	return ""
}

// normalizeKimiK3Effort Kimi K3 档位归一。
// Python: reasoning.py:644-654 _normalize_kimi_k3_effort
func normalizeKimiK3Effort(effort *string) string {
	if effort == nil {
		return ""
	}
	s := *effort
	switch v := strings.ToLower(strings.TrimSpace(s)); v {
	case "ultra", "xhigh", "max":
		return "max"
	case "medium", "high":
		return "high"
	case "minimum", "minimal", "light", "low":
		return "low"
	}
	return ""
}

// normalizeQwen38Effort Qwen3.8 档位归一。
// Python: reasoning.py:831-843 _normalize_qwen38_effort
func normalizeQwen38Effort(effort *string) string {
	if effort == nil {
		return ""
	}
	s := *effort
	switch v := strings.ToLower(strings.TrimSpace(s)); v {
	case "none", "off":
		return "none"
	case "minimal", "low":
		return "low"
	case "medium":
		return "medium"
	case "high", "xhigh", "max":
		return "xhigh"
	}
	return ""
}

// moonshotModelFamily 判定 Kimi 模型族。
// Python: reasoning.py:592-600 _moonshot_model_family
func moonshotModelFamily(model string) string {
	modelKey := strings.ToLower(model)
	if idx := strings.LastIndex(modelKey, "/"); idx >= 0 {
		modelKey = modelKey[idx+1:]
	}
	switch {
	case strings.HasPrefix(modelKey, "kimi-k3"):
		return "k3"
	case strings.Contains(modelKey, "k2.7-code"):
		return "k27_code"
	case strings.HasPrefix(modelKey, "kimi-k2.6"), strings.HasPrefix(modelKey, "kimi-k2.5"):
		return "k2_toggle"
	}
	return "unknown"
}

// moonshotChatReasoningPlan Kimi Chat 编码。
// Python: reasoning.py:603-621 _moonshot_chat_reasoning_plan
func moonshotChatReasoningPlan(model string, cfg *llmschema.ReasoningConfig) ReasoningPlan {
	switch moonshotModelFamily(model) {
	case "k3":
		if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
			return ReasoningPlan{}
		}
		if effort := normalizeKimiK3Effort(cfg.Effort); effort != "" {
			return ReasoningPlan{SDKParams: map[string]any{"reasoning_effort": effort}}
		}
		return ReasoningPlan{}
	case "k27_code":
		return ReasoningPlan{}
	case "k2_toggle":
		extra := map[string]any{}
		if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
			extra["thinking"] = map[string]any{"type": "disabled"}
		} else if cfg.Mode == "enabled" {
			extra["thinking"] = map[string]any{"type": "enabled"}
		}
		return ReasoningPlan{ExtraBody: extra}
	}
	return ReasoningPlan{}
}

// moonshotAnthropicReasoningPlan Kimi Anthropic 编码。
// Python: reasoning.py:624-641 _moonshot_anthropic_reasoning_plan
func moonshotAnthropicReasoningPlan(model string, cfg *llmschema.ReasoningConfig) ReasoningPlan {
	switch moonshotModelFamily(model) {
	case "k3":
		if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
			return ReasoningPlan{}
		}
		if effort := normalizeKimiK3Effort(cfg.Effort); effort != "" {
			return ReasoningPlan{ExtraBody: map[string]any{"reasoning_effort": effort}}
		}
		return ReasoningPlan{}
	case "k27_code":
		return ReasoningPlan{}
	case "k2_toggle":
		if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
			return ReasoningPlan{SDKParams: map[string]any{"thinking": map[string]any{"type": "disabled"}}}
		}
		if cfg.Mode == "auto" {
			return ReasoningPlan{}
		}
		return ReasoningPlan{SDKParams: map[string]any{"thinking": map[string]any{"type": "enabled"}}}
	}
	return ReasoningPlan{}
}

// dashscopeAnthropicReasoningPlan DashScope Anthropic 开关（无 effort 字段）。
// Python: reasoning.py:657-672 _dashscope_anthropic_reasoning_plan
func dashscopeAnthropicReasoningPlan(cfg *llmschema.ReasoningConfig) ReasoningPlan {
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
		return ReasoningPlan{SDKParams: map[string]any{"thinking": map[string]any{"type": "disabled"}}}
	}
	hasEffort := cfg.Effort != nil && *cfg.Effort != ""
	if cfg.Mode == "auto" && !hasEffort && cfg.BudgetTokens == nil {
		return ReasoningPlan{}
	}
	thinking := map[string]any{"type": "enabled"}
	if cfg.BudgetTokens != nil {
		thinking["budget_tokens"] = *cfg.BudgetTokens
	}
	return ReasoningPlan{SDKParams: map[string]any{"thinking": thinking}}
}

// anthropicOutputConfigEffortPlan Anthropic 原生 output_config.effort 编码。
// 显式 budget 优先（DashScope 拒绝 budget + effort 并存）。
// Python: reasoning.py:675-697 _anthropic_output_config_effort_plan
func anthropicOutputConfigEffortPlan(cfg *llmschema.ReasoningConfig, normalizer func(*string) string) ReasoningPlan {
	effort := normalizer(cfg.Effort)
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) || effort == "none" {
		return ReasoningPlan{SDKParams: map[string]any{"thinking": map[string]any{"type": "disabled"}}}
	}
	hasEffort := cfg.Effort != nil && *cfg.Effort != ""
	if cfg.Mode == "auto" && !hasEffort && cfg.BudgetTokens == nil {
		return ReasoningPlan{}
	}
	thinking := map[string]any{"type": "enabled"}
	sdk := map[string]any{"thinking": thinking}
	if cfg.BudgetTokens != nil {
		thinking["budget_tokens"] = *cfg.BudgetTokens
	} else if effort != "" {
		sdk["output_config"] = map[string]any{"effort": effort}
	}
	return ReasoningPlan{SDKParams: sdk}
}

// anthropicManualBudgetPlan 仅支持预算的 Anthropic 编码（Claude Sonnet 4.5 及更早）。
// Python: reasoning.py:715-735 _anthropic_manual_budget_plan
func anthropicManualBudgetPlan(cfg *llmschema.ReasoningConfig) ReasoningPlan {
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
		return ReasoningPlan{SDKParams: map[string]any{"thinking": map[string]any{"type": "disabled"}}}
	}
	hasEffort := cfg.Effort != nil && *cfg.Effort != ""
	if cfg.Mode == "auto" && !hasEffort && cfg.BudgetTokens == nil {
		return ReasoningPlan{}
	}
	var budget int
	if cfg.BudgetTokens != nil {
		budget = *cfg.BudgetTokens
	} else {
		effort := ""
		if cfg.Effort != nil {
			effort = strings.ToLower(strings.TrimSpace(*cfg.Effort))
		}
		if b, ok := anthropicManualBudgetByEffort[effort]; ok {
			budget = b
		} else {
			budget = 8192
		}
	}
	return ReasoningPlan{SDKParams: map[string]any{
		"thinking": map[string]any{"type": "enabled", "budget_tokens": budget},
	}}
}

// minimaxChatReasoningPlan MiniMax Chat 编码。
// Python: reasoning.py:737-742 _minimax_chat_reasoning_plan
func minimaxChatReasoningPlan(modelKey string, cfg *llmschema.ReasoningConfig) ReasoningPlan {
	if strings.HasPrefix(modelKey, "minimax-m3") {
		return thinkingTypeTogglePlan(cfg, "adaptive")
	}
	if strings.HasPrefix(modelKey, "minimax-m2") {
		return ReasoningPlan{ExtraBody: map[string]any{"reasoning_split": true}}
	}
	return ReasoningPlan{}
}

// minimaxAnthropicReasoningPlan MiniMax Anthropic 编码。
// Python: reasoning.py:745-754 _minimax_anthropic_reasoning_plan
func minimaxAnthropicReasoningPlan(model string, cfg *llmschema.ReasoningConfig) ReasoningPlan {
	modelKey := strings.ToLower(model)
	if strings.HasPrefix(modelKey, "minimax-m3") {
		if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
			return ReasoningPlan{SDKParams: map[string]any{"thinking": map[string]any{"type": "disabled"}}}
		}
		if cfg.Mode == "enabled" {
			return ReasoningPlan{SDKParams: map[string]any{"thinking": map[string]any{"type": "adaptive"}}}
		}
	}
	return ReasoningPlan{}
}

// vllmEnableThinkingPlan vLLM/DashScope 风格自建网关编码。
// Python: reasoning.py:773-803 _vllm_enable_thinking_plan
func vllmEnableThinkingPlan(cfg *llmschema.ReasoningConfig) ReasoningPlan {
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
		return ReasoningPlan{ExtraBody: map[string]any{
			"enable_thinking":      false,
			"chat_template_kwargs": map[string]any{"enable_thinking": false},
		}}
	}
	hasEffort := cfg.Effort != nil && *cfg.Effort != ""
	if cfg.Mode == "auto" && !hasEffort && cfg.BudgetTokens == nil {
		return ReasoningPlan{}
	}
	extra := map[string]any{
		"enable_thinking":      true,
		"chat_template_kwargs": map[string]any{"enable_thinking": true},
	}
	if cfg.BudgetTokens != nil {
		extra["thinking_budget"] = *cfg.BudgetTokens
	} else {
		effort := ""
		if cfg.Effort != nil {
			effort = strings.ToLower(strings.TrimSpace(*cfg.Effort))
		}
		if b, ok := vllmDashscopeBudgetByEffort[effort]; ok {
			extra["thinking_budget"] = b
		}
	}
	return ReasoningPlan{ExtraBody: extra}
}

// qwenEnableThinkingPlan Qwen enable_thinking 编码。
// Python: reasoning.py:806-814 _qwen_enable_thinking_plan
func qwenEnableThinkingPlan(cfg *llmschema.ReasoningConfig) ReasoningPlan {
	extra := make(map[string]any)
	if cfg.Mode == "enabled" && !isDisabledReasoningEffort(cfg.Effort) {
		extra["enable_thinking"] = true
	} else if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
		extra["enable_thinking"] = false
	}
	if cfg.BudgetTokens != nil {
		extra["thinking_budget"] = *cfg.BudgetTokens
	}
	return ReasoningPlan{ExtraBody: extra}
}

// dashscopeQwen38ReasoningPlan Qwen3.8 编码。
// Python: reasoning.py:817-828 _dashscope_qwen38_reasoning_plan
func dashscopeQwen38ReasoningPlan(cfg *llmschema.ReasoningConfig) ReasoningPlan {
	extra := make(map[string]any)
	effort := normalizeQwen38Effort(cfg.Effort)
	if cfg.Mode == "disabled" || effort == "none" {
		return ReasoningPlan{ExtraBody: map[string]any{"enable_thinking": false}}
	}
	if cfg.Mode == "enabled" {
		extra["enable_thinking"] = true
	}
	if cfg.BudgetTokens != nil {
		extra["thinking_budget"] = *cfg.BudgetTokens
	} else if effort != "" {
		extra["reasoning_effort"] = effort
	}
	return ReasoningPlan{ExtraBody: extra}
}

// thinkingTypeTogglePlan thinking.type 开关编码（extra_body）。
// Python: reasoning.py:846-855 _thinking_type_toggle_plan
func thinkingTypeTogglePlan(cfg *llmschema.ReasoningConfig, enabledType string) ReasoningPlan {
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
		return ReasoningPlan{ExtraBody: map[string]any{"thinking": map[string]any{"type": "disabled"}}}
	}
	if cfg.Mode == "enabled" {
		return ReasoningPlan{ExtraBody: map[string]any{"thinking": map[string]any{"type": enabledType}}}
	}
	return ReasoningPlan{}
}

// thinkingTypeWithEffortPlan thinking.type + reasoning_effort 组合编码。
// Python: reasoning.py:858-873 _thinking_type_with_effort_plan
func thinkingTypeWithEffortPlan(
	cfg *llmschema.ReasoningConfig,
	effortInExtraBody bool,
	normalizer func(*string) string,
) ReasoningPlan {
	effort := normalizer(cfg.Effort)
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) || effort == "none" {
		return ReasoningPlan{ExtraBody: map[string]any{"thinking": map[string]any{"type": "disabled"}}}
	}
	plan := thinkingTypeTogglePlan(cfg, "enabled")
	// 拷贝，避免与 plan 共享底层 map
	extra := make(map[string]any, len(plan.ExtraBody)+1)
	for k, v := range plan.ExtraBody {
		extra[k] = v
	}
	sdk := make(map[string]any, len(plan.SDKParams))
	for k, v := range plan.SDKParams {
		sdk[k] = v
	}
	if effort != "" {
		if effortInExtraBody {
			extra["reasoning_effort"] = effort
		} else {
			sdk["reasoning_effort"] = effort
		}
	}
	return ReasoningPlan{SDKParams: sdk, ExtraBody: extra}
}

// anthropicThinkingTogglePlan Anthropic thinking.type 开关（sdk_params）。
// Python: reasoning.py:876-881 _anthropic_thinking_toggle_plan
func anthropicThinkingTogglePlan(cfg *llmschema.ReasoningConfig) ReasoningPlan {
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
		return ReasoningPlan{SDKParams: map[string]any{"thinking": map[string]any{"type": "disabled"}}}
	}
	if cfg.Mode == "enabled" {
		return ReasoningPlan{SDKParams: map[string]any{"thinking": map[string]any{"type": "enabled"}}}
	}
	return ReasoningPlan{}
}

// anthropicThinkingWithExtraEffortPlan Anthropic thinking + extra_body reasoning_effort。
// Python: reasoning.py:884-896 _anthropic_thinking_with_extra_effort_plan
func anthropicThinkingWithExtraEffortPlan(cfg *llmschema.ReasoningConfig, normalizer func(*string) string) ReasoningPlan {
	effort := normalizer(cfg.Effort)
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) || effort == "none" {
		return ReasoningPlan{SDKParams: map[string]any{"thinking": map[string]any{"type": "disabled"}}}
	}
	plan := anthropicThinkingTogglePlan(cfg)
	sdk := make(map[string]any, len(plan.SDKParams))
	for k, v := range plan.SDKParams {
		sdk[k] = v
	}
	extra := make(map[string]any)
	if effort != "" {
		extra["reasoning_effort"] = effort
	}
	return ReasoningPlan{SDKParams: sdk, ExtraBody: extra}
}

// openRouterReasoningPlan OpenRouter 统一 reasoning 对象编码。
// Python: reasoning.py:899-924 _openrouter_reasoning_plan
func openRouterReasoningPlan(modelKey string, cfg *llmschema.ReasoningConfig) ReasoningPlan {
	providerPrefix := ""
	if idx := strings.Index(modelKey, "/"); idx >= 0 {
		providerPrefix = modelKey[:idx]
	}
	if providerPrefix != "" {
		if _, known := openRouterKnownReasoningPrefixes[providerPrefix]; !known {
			return ReasoningPlan{}
		}
	}
	payload := make(map[string]any)
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
		payload["effort"] = "none"
	} else if cfg.Mode == "enabled" {
		payload["enabled"] = true
	}
	if cfg.BudgetTokens != nil {
		payload["max_tokens"] = *cfg.BudgetTokens
	} else if cfg.Effort != nil && *cfg.Effort != "" && payload["effort"] != "none" {
		payload["effort"] = *cfg.Effort
	}
	if len(payload) == 0 {
		return ReasoningPlan{}
	}
	return ReasoningPlan{ExtraBody: map[string]any{"reasoning": payload}}
}

// reasoningEffortPlan 通用 reasoning_effort 编码。
// Python: reasoning.py:927-932 _reasoning_effort_plan
func reasoningEffortPlan(cfg *llmschema.ReasoningConfig, disabledValue string) ReasoningPlan {
	if cfg.Mode == "disabled" || isDisabledReasoningEffort(cfg.Effort) {
		return ReasoningPlan{SDKParams: map[string]any{"reasoning_effort": disabledValue}}
	}
	if cfg.Effort != nil && *cfg.Effort != "" {
		return ReasoningPlan{SDKParams: map[string]any{"reasoning_effort": *cfg.Effort}}
	}
	return ReasoningPlan{}
}

// containsString 判定切片是否含目标串。
func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}
