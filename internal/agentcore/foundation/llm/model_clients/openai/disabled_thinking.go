package openai

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/reasoning"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// rejectingThinkingSet 进程内「已知拒绝 disabled thinking」的模型名集合。
//
// 上游是实例属性 set（openai_model_client.py:456），Go 客户端可能被并发调用，
// 故加锁（偏差 D-4）。
type rejectingThinkingSet struct {
	mu     sync.RWMutex
	models map[string]struct{}
}

// statusCoder 可由错误实现以暴露 HTTP 状态码。
type statusCoder interface {
	StatusCode() int
}

// paramsCarrier 由 exception.BaseError 实现，可取结构化上下文。
// 本包只依赖「能取 params」这一能力，避免直接耦合 exception 包的具体类型。
type paramsCarrier interface {
	Params() map[string]any
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// disabledThinkingTypes 表示「关闭」的 thinking.type 取值。
	// Python: openai_model_client.py:132 _DISABLED_THINKING_TYPES
	disabledThinkingTypes = map[string]struct{}{"disabled": {}}

	// disabledReasoningEfforts 表示「关闭」的 effort 取值。
	// Python: openai_model_client.py:133 _DISABLED_REASONING_EFFORTS
	disabledReasoningEfforts = map[string]struct{}{"off": {}, "none": {}}

	// disabledThinkingNonRetryStatuses 这些状态码不是参数问题，不重试。
	// Python: openai_model_client.py:133 _DISABLED_THINKING_NON_RETRY_STATUSES
	disabledThinkingNonRetryStatuses = map[int]struct{}{401: {}, 403: {}, 408: {}, 429: {}}

	// unsupportedDisabledThinkingMarkers「不支持」语义标记。
	// Python: openai_model_client.py:134-144 _UNSUPPORTED_DISABLED_THINKING_MARKERS
	unsupportedDisabledThinkingMarkers = []string{
		"unsupported", "not support", "does not support", "doesn't support",
		"cannot disable", "can't disable", "不支持", "不能关闭", "不允许关闭",
	}

	// disabledThinkingValueMarkers「关闭」语义标记。
	// Python: openai_model_client.py:145-150 _DISABLED_THINKING_VALUE_MARKERS（4 个）
	disabledThinkingValueMarkers = []string{
		"disabled", "disable", "off", "关闭",
	}

	// falseFlagStrings 视为 false 的字符串。
	// Python: openai_model_client.py:1162-1166 _is_false_flag
	falseFlagStrings = map[string]struct{}{"false": {}, "0": {}, "no": {}}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// 本文件无导出函数：三道闸门只服务于本包的 Invoke/Stream。

// ──────────────────────────── 非导出函数（类型方法）────────────────────────────

// newRejectingThinkingSet 创建空的拒绝集。
func newRejectingThinkingSet() *rejectingThinkingSet {
	return &rejectingThinkingSet{models: make(map[string]struct{})}
}

// add 记录模型已知拒绝 disabled thinking。
func (s *rejectingThinkingSet) add(model string) {
	if s == nil || model == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.models[model] = struct{}{}
}

// has 判定模型是否已知拒绝。
func (s *rejectingThinkingSet) has(model string) bool {
	if s == nil || model == "" {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.models[model]
	return ok
}

// ──────────────────────────── 非导出函数（判据）────────────────────────────

// isDisabledThinkingType 判定 thinking.type 是否为 disabled。
// Python: openai_model_client.py:1158-1159 _is_disabled_thinking_type
func isDisabledThinkingType(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	_, disabled := disabledThinkingTypes[strings.ToLower(strings.TrimSpace(s))]
	return disabled
}

// isFalseFlag 判定值是否显式为 false（bool false / "false" / "0" / "no"）。
// Python: openai_model_client.py:1161-1168 _is_false_flag
func isFalseFlag(value any) bool {
	switch v := value.(type) {
	case bool:
		return !v
	case string:
		_, isFalse := falseFlagStrings[strings.ToLower(strings.TrimSpace(v))]
		return isFalse
	}
	return false
}

// isDisabledReasoningEffortValue 判定 reasoning_effort 是否为关闭态。
// Python: openai_model_client.py:1170-1172 _is_disabled_reasoning_effort
func isDisabledReasoningEffortValue(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	_, disabled := disabledReasoningEfforts[strings.ToLower(strings.TrimSpace(s))]
	return disabled
}

// mappingHasDisabledThinkingParams 判定单个 mapping 是否携带关闭思考的字段。
// Python: openai_model_client.py:1174-1196 _mapping_has_disabled_thinking_params
func mappingHasDisabledThinkingParams(value map[string]any) bool {
	if value == nil {
		return false
	}
	if thinking, ok := value["thinking"].(map[string]any); ok {
		if isDisabledThinkingType(thinking["type"]) {
			return true
		}
	}
	if isFalseFlag(value["enable_thinking"]) {
		return true
	}
	if ctk, ok := value["chat_template_kwargs"].(map[string]any); ok {
		if isFalseFlag(ctk["enable_thinking"]) {
			return true
		}
	}
	if reasoning, ok := value["reasoning"].(map[string]any); ok {
		if isFalseFlag(reasoning["enabled"]) {
			return true
		}
	}
	return isDisabledReasoningEffortValue(value["reasoning_effort"])
}

// hasDisabledThinkingParams 判定请求（含 extra_body）是否携带关闭思考的字段。
// Python: openai_model_client.py:1198-1201 _has_disabled_thinking_params
func hasDisabledThinkingParams(params map[string]any) bool {
	if mappingHasDisabledThinkingParams(params) {
		return true
	}
	if eb, ok := params["extra_body"].(map[string]any); ok {
		return mappingHasDisabledThinkingParams(eb)
	}
	return false
}

// stripDisabledThinkingFields 就地剥离单个 mapping 中的关闭思考字段。
//
// 注意 thinking 的处理：只删 type 键，其余键（如 budget_tokens）保留；
// 删空才整体移除该键。这对 chat_template_kwargs / reasoning 同理。
//
// Python: openai_model_client.py:1204-1239 _strip_disabled_thinking_fields
func stripDisabledThinkingFields(params map[string]any) {
	if thinking, ok := params["thinking"].(map[string]any); ok {
		if isDisabledThinkingType(thinking["type"]) {
			stripped := make(map[string]any, len(thinking))
			for k, v := range thinking {
				if k != "type" {
					stripped[k] = v
				}
			}
			if len(stripped) > 0 {
				params["thinking"] = stripped
			} else {
				delete(params, "thinking")
			}
		}
	}

	if isFalseFlag(params["enable_thinking"]) {
		delete(params, "enable_thinking")
	}

	if ctk, ok := params["chat_template_kwargs"].(map[string]any); ok {
		if isFalseFlag(ctk["enable_thinking"]) {
			stripped := make(map[string]any, len(ctk))
			for k, v := range ctk {
				if k != "enable_thinking" {
					stripped[k] = v
				}
			}
			if len(stripped) > 0 {
				params["chat_template_kwargs"] = stripped
			} else {
				delete(params, "chat_template_kwargs")
			}
		}
	}

	if reasoning, ok := params["reasoning"].(map[string]any); ok {
		if isFalseFlag(reasoning["enabled"]) {
			stripped := make(map[string]any, len(reasoning))
			for k, v := range reasoning {
				if k != "enabled" {
					stripped[k] = v
				}
			}
			if len(stripped) > 0 {
				params["reasoning"] = stripped
			} else {
				delete(params, "reasoning")
			}
		}
	}

	if isDisabledReasoningEffortValue(params["reasoning_effort"]) {
		delete(params, "reasoning_effort")
	}
}

// withoutDisabledThinkingParams 返回剥离关闭思考字段后的深拷贝。
// Python: openai_model_client.py:1242-1254 _without_disabled_thinking_params
func withoutDisabledThinkingParams(params map[string]any) map[string]any {
	cleaned := deepCopyParams(params)
	stripDisabledThinkingFields(cleaned)

	if eb, ok := cleaned["extra_body"].(map[string]any); ok {
		stripped := make(map[string]any, len(eb))
		for k, v := range eb {
			stripped[k] = v
		}
		stripDisabledThinkingFields(stripped)
		if len(stripped) > 0 {
			cleaned["extra_body"] = stripped
		} else {
			delete(cleaned, "extra_body")
		}
	}
	return cleaned
}

// disabledThinkingModelKey 取模型名作为缓存键。
// Python: openai_model_client.py:1257-1262 _disabled_thinking_model_key
func disabledThinkingModelKey(params map[string]any) string {
	model, ok := params["model"]
	if !ok || model == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(model))
}

// applyDisabledThinkingCache 若该模型已知拒绝且请求带关闭字段，先剥离。
// Python: openai_model_client.py:1264-1270 _apply_disabled_thinking_cache
func (c *OpenAIModelClient) applyDisabledThinkingCache(params map[string]any) map[string]any {
	modelKey := disabledThinkingModelKey(params)
	if !c.rejectingDisabledThinking.has(modelKey) {
		return params
	}
	if !hasDisabledThinkingParams(params) {
		return params
	}
	return withoutDisabledThinkingParams(params)
}

// exceptionChain 展开错误链（含 Unwrap 与嵌套）。
// Python: openai_model_client.py:1273-1280 _iter_exception_chain
func exceptionChain(err error) []error {
	if err == nil {
		return nil
	}
	var chain []error
	seen := make(map[error]struct{})
	current := err
	for current != nil {
		if _, dup := seen[current]; dup {
			break
		}
		seen[current] = struct{}{}
		chain = append(chain, current)
		current = errors.Unwrap(current)
	}
	return chain
}

// exceptionStatusCode 从错误链中提取 HTTP 状态码。
//
// 两个来源（按优先级）：
//  1. 实现 statusCoder 接口的错误（如测试替身、第三方错误类型）
//  2. BaseError.Params()["http_status"] —— HandleHTTPError 写入的位置
//
// Python: openai_model_client.py:1301-1311 _exception_status_code
// （上游读 exc.response.status_code；Go 侧 HTTP 状态放在 BaseError params 里）
func exceptionStatusCode(err error) (int, bool) {
	for _, item := range exceptionChain(err) {
		if sc, ok := item.(statusCoder); ok {
			if code := sc.StatusCode(); code > 0 {
				return code, true
			}
		}
		if paramed, ok := item.(paramsCarrier); ok {
			if params := paramed.Params(); params != nil {
				if code, ok := toInt(params["http_status"]); ok && code > 0 {
					return code, true
				}
			}
		}
	}
	return 0, false
}

// toInt 把 params 中的数值转 int（JSON 反序列化后可能是 float64）。
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case int64:
		return int(n), true
	case float64:
		return int(n), true
	}
	return 0, false
}

// exceptionText 拼接错误链全部文本（供关键字判定）。
// Python: openai_model_client.py:1313-1347 _collect_exception_text_parts
func exceptionText(err error) string {
	var sb strings.Builder
	for _, item := range exceptionChain(err) {
		if msg := item.Error(); msg != "" {
			sb.WriteString(msg)
			sb.WriteString(" ")
		}
	}
	return strings.ToLower(sb.String())
}

// isDisabledThinkingRejection 判定错误是否为「不支持关闭思考」。
//
// 四层过滤，任一不满足即不重试：
//  1. 状态码是鉴权/限流/服务端故障（401/403/408/429 或 >=500）→ 不是参数问题
//  2. 错误文本含 "1210" → 厂商特定码，直接认
//  3. 必须提到 thinking / reasoning / 思考
//  4. 必须表达「不支持」且（表达「关闭」或提到 parameter/参数）
//
// Python: openai_model_client.py:1349-1370 _is_disabled_thinking_rejection
func isDisabledThinkingRejection(err error) bool {
	if status, ok := exceptionStatusCode(err); ok {
		if _, nonRetry := disabledThinkingNonRetryStatuses[status]; nonRetry || status >= 500 {
			return false
		}
	}

	text := exceptionText(err)
	if strings.Contains(text, "1210") {
		return true
	}

	mentionsThinking := strings.Contains(text, "thinking") ||
		strings.Contains(text, "reasoning") ||
		strings.Contains(text, "思考")
	if !mentionsThinking {
		return false
	}

	if !containsAnyMarker(text, unsupportedDisabledThinkingMarkers) {
		return false
	}

	if containsAnyMarker(text, disabledThinkingValueMarkers) {
		return true
	}
	return strings.Contains(text, "parameter") || strings.Contains(text, "参数")
}

// retryParamsWithoutDisabledThinking 判定是否可重试，可则返回剥离后的参数。
// Python: openai_model_client.py:1372-1381 _retry_params_without_disabled_thinking
func retryParamsWithoutDisabledThinking(params map[string]any, err error) map[string]any {
	if !hasDisabledThinkingParams(params) {
		return nil
	}
	if !isDisabledThinkingRejection(err) {
		return nil
	}
	return withoutDisabledThinkingParams(params)
}

// doWithDisabledThinkingFallback 执行发送，按需做一次 disabled-thinking 回退。
//
// 三道闸门：
//  1. 不在 scope 内 → 直接发，任何失败都不重试
//  2. 该模型已知拒绝 → 先剥离再发
//  3. 错误判据不满足 → 原样抛出；满足则记入缓存并重试一次（重试失败直接抛）
//
// Python: openai_model_client.py:1383-1410
// _create_chat_completion_with_disabled_thinking_fallback
func (c *OpenAIModelClient) doWithDisabledThinkingFallback(
	ctx context.Context,
	params map[string]any,
	send func(params map[string]any) error,
) error {
	// 闸门一：默认不重试
	if !reasoning.IsDisabledThinkingFallbackAllowed(ctx) {
		return send(params)
	}

	// 闸门二：已知拒绝的先剥离
	requestParams := c.applyDisabledThinkingCache(params)

	err := send(requestParams)
	if err == nil {
		return nil
	}

	// 闸门三：判据不满足则原样抛出
	retryParams := retryParamsWithoutDisabledThinking(requestParams, err)
	if retryParams == nil {
		return err
	}

	modelKey := disabledThinkingModelKey(requestParams)
	c.rejectingDisabledThinking.add(modelKey)

	provider := ""
	if c.ClientConfig != nil {
		provider = c.ClientConfig.ClientProvider
	}
	logger.Warn(logComponent).
		Str("model_name", modelKey).
		Str("model_provider", provider).
		Err(err).
		Msg("OpenAI-compatible model rejected disabled thinking; retrying with model defaults.")

	// 重试不套 try：失败直接抛（对齐上游）
	return send(retryParams)
}

// ──────────────────────────── 非导出函数（工具）────────────────────────────

// containsAnyMarker 判定文本是否含任一标记（调用方已转小写）。
func containsAnyMarker(text string, markers []string) bool {
	for _, m := range markers {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// deepCopyParams 深拷贝请求参数（含嵌套 map / 切片）。
// Python: reasoning 侧的 deepcopy；Go 无内建，按需实现两层即可。
func deepCopyParams(params map[string]any) map[string]any {
	out := make(map[string]any, len(params))
	for k, v := range params {
		out[k] = deepCopyValue(v)
	}
	return out
}

// deepCopyValue 递归拷贝 map / 切片，其余类型按值复制。
func deepCopyValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for k, item := range typed {
			out[k] = deepCopyValue(item)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = deepCopyValue(item)
		}
		return out
	}
	return v
}

// applyReasoningPlan 把 reasoning 意图翻译后写入请求参数。
//
// 取值优先级（对齐 Python reasoning.py:300-306 _explicit_reasoning_value）：
//  1. 本次调用显式传入（InvokeParams.Extra["reasoning"]），用两值断言区分
//     「没传」与「显式传 nil」——不能用 != nil 判断，会把显式 nil 误当未传而回落配置
//  2. 否则回落 model_config.reasoning
//
// 注意 base_client 会把 Extra 平铺进 reqParams，键名仍是 reasoning，
// 所以这里必须读 params.Extra（含 exists 信息）。
//
// Python: openai_model_client.py:1002-1016（_build_request_params 内）
func (c *OpenAIModelClient) applyReasoningPlan(reqParams map[string]any, params *model_clients.InvokeParams) {
	intent := llmschema.ReasoningIntent{Set: false}
	if v, exists := params.Extra["reasoning"]; exists {
		intent = llmschema.ReasoningIntent{Set: true, Value: v}
	} else if c.ModelConfig != nil && c.ModelConfig.Reasoning != nil {
		intent = llmschema.ReasoningIntent{Set: true, Value: c.ModelConfig.Reasoning}
	}

	plan := reasoning.ResolveReasoningPlan(
		c.ClientConfig,
		c.ModelConfig,
		modelNameOf(reqParams),
		intent,
	)
	reasoning.ApplyReasoningPlan(reqParams, plan, llmschema.IsReasoningConfigIntent(intent.Value))

	if controls := reasoning.ReasoningRequestControls(reqParams); len(controls) > 0 {
		logger.Info(logComponent).
			Interface("controls", controls).
			Str("model_name", modelNameOf(reqParams)).
			Str("model_provider", providerOf(c)).
			Msg("Resolved OpenAI-compatible reasoning request controls.")
	}
}

// modelNameOf 从请求参数取模型名（用于 profile 查找与日志）。
func modelNameOf(reqParams map[string]any) string {
	model, ok := reqParams["model"]
	if !ok || model == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(model))
}

// providerOf 取客户端 provider（nil 安全）。
func providerOf(c *OpenAIModelClient) string {
	if c == nil || c.ClientConfig == nil {
		return ""
	}
	return c.ClientConfig.ClientProvider
}
