// Package reasoning 提供厂商中立的思考/推理档位解析与线上参数编码。
//
// 上游把「用户选的档位」翻译成各厂商不同的请求字段：OpenAI 用 reasoning_effort、
// Anthropic 用 thinking.type+budget_tokens、DashScope 用 enable_thinking+thinking_budget、
// OpenRouter 用 reasoning{effort,enabled,max_tokens} 等。本包只负责翻译，不负责发送。
//
// 文件目录：
//
//	reasoning/
//	├── doc.go       # 包文档
//	├── profiles.go  # 42 条 provider profile + 23 条 model fallback + 能力查询
//	└── plan.go      # ReasoningPlan + resolve/apply + wire 编码器
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/llm/reasoning_profiles.py
//	openjiuwen/core/foundation/llm/reasoning.py
package reasoning

import (
	"net/url"
	"strings"

	"github.com/danwakefield/fnmatch"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReasoningCapability 用户可选的思考档位与推荐值。
// Python: reasoning_profiles.py:18-26 ReasoningCapability
type ReasoningCapability struct {
	// Options 可选档位；空表示该模型无思考能力或恒定开启
	Options []string
	// Recommended 推荐档位；nil 表示无推荐
	Recommended *string
}

// ReasoningProfile 一个 provider 作用域下的模型族及其线上编码器。
// Python: reasoning_profiles.py:29-38 ReasoningProfile
type ReasoningProfile struct {
	// Provider provider key（小写；"*" 表示跨 provider 的 fallback 条目）
	Provider string
	// ModelPatterns 模型名通配 pattern（fnmatchcase 语义）
	ModelPatterns []string
	// Capability OpenAI 协议的默认能力
	Capability ReasoningCapability
	// OpenAIWire OpenAI 协议使用的编码器名
	OpenAIWire string
	// AnthropicWire Anthropic 协议使用的编码器名
	AnthropicWire string
	// AnthropicCapability Anthropic 协议的独立能力；nil 表示复用 Capability
	AnthropicCapability *ReasoningCapability
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// toggleCapability 开关型：off/on，推荐 on。
	// Python: reasoning_profiles.py:41 TOGGLE
	toggleCapability = ReasoningCapability{Options: []string{"off", "on"}, Recommended: strPtr("on")}

	// alwaysOnCapability 恒定开启：无档位可选。
	// Python: reasoning_profiles.py:43 ALWAYS_ON
	alwaysOnCapability = ReasoningCapability{}

	// unsupportedCapability 不支持思考。
	// Python: reasoning_profiles.py:45 UNSUPPORTED
	unsupportedCapability = ReasoningCapability{}

	// anthropicManualBudgetCapability 仅支持预算的 Anthropic（Claude Sonnet 4.5 及更早）。
	// Python: reasoning_profiles.py:51-54 ANTHROPIC_MANUAL_BUDGET
	anthropicManualBudgetCapability = ReasoningCapability{
		Options: []string{"off", "low", "medium", "high"}, Recommended: strPtr("medium"),
	}

	// deepseekV4Capability DeepSeek V4 档位。
	// Python: reasoning_profiles.py:56-59 DEEPSEEK_V4
	deepseekV4Capability = ReasoningCapability{
		Options: []string{"off", "low", "high", "max"}, Recommended: strPtr("high"),
	}

	// glm52Capability GLM-5.2 档位。
	// Python: reasoning_profiles.py:60-63 GLM_52
	glm52Capability = ReasoningCapability{
		Options: []string{"off", "high", "max"}, Recommended: strPtr("max"),
	}

	// qwen38Capability Qwen3.8-Max 档位。
	// Python: reasoning_profiles.py:64-67 QWEN_38
	qwen38Capability = ReasoningCapability{
		Options: []string{"off", "low", "medium", "xhigh"}, Recommended: strPtr("xhigh"),
	}

	// kimiK3Capability Kimi K3 档位（无 off）。
	// Python: reasoning_profiles.py:68-71 KIMI_K3
	kimiK3Capability = ReasoningCapability{
		Options: []string{"low", "high", "max"}, Recommended: strPtr("high"),
	}

	// volcengineSeed20Capability 火山 Seed-2.0 档位。
	// Python: reasoning_profiles.py:72-75 VOLCENGINE_SEED_20
	volcengineSeed20Capability = ReasoningCapability{
		Options: []string{"off", "low", "medium", "high"}, Recommended: strPtr("medium"),
	}

	// openaiGPT56Capability GPT-5.6 档位。
	// Python: reasoning_profiles.py:76-79 OPENAI_GPT_56
	openaiGPT56Capability = ReasoningCapability{
		Options: []string{"off", "low", "medium", "high", "xhigh", "max"}, Recommended: strPtr("medium"),
	}

	// openaiGPT55Capability GPT-5.5 档位（无 max）。
	// Python: reasoning_profiles.py:80-83 OPENAI_GPT_55
	openaiGPT55Capability = ReasoningCapability{
		Options: []string{"off", "low", "medium", "high", "xhigh"}, Recommended: strPtr("medium"),
	}

	// openaiGPT51To54Capability GPT-5.1~5.4 档位（推荐 off）。
	// Python: reasoning_profiles.py:84-87 OPENAI_GPT_51_TO_54
	openaiGPT51To54Capability = ReasoningCapability{
		Options: []string{"off", "low", "medium", "high", "xhigh"}, Recommended: strPtr("off"),
	}

	// openaiGPT5Capability GPT-5 档位（minimal 起步，无 off）。
	// Python: reasoning_profiles.py:88-91 OPENAI_GPT_5
	openaiGPT5Capability = ReasoningCapability{
		Options: []string{"minimal", "low", "medium", "high"}, Recommended: strPtr("medium"),
	}

	// standardOpenAICapability OpenAI 协议默认能力。
	// Python: reasoning_profiles.py:359-361 STANDARD_OPENAI_CAPABILITY
	standardOpenAICapability = ReasoningCapability{
		Options: []string{"off", "low", "medium", "high"},
	}

	// standardAnthropicCapability Anthropic 协议默认能力（多一档 max）。
	// Python: reasoning_profiles.py:362-364 STANDARD_ANTHROPIC_CAPABILITY
	standardAnthropicCapability = ReasoningCapability{
		Options: []string{"off", "low", "medium", "high", "max"},
	}

	// genericEndpointProfiles 通用端点档案集合：命中时按 api_base 推断 provider。
	// Python: reasoning.py:63 _GENERIC_ENDPOINT_PROFILES
	genericEndpointProfiles = map[string]struct{}{
		"": {}, "openai": {}, "openai-compatible": {}, "openai_compatible": {},
	}

	// apiBaseProviderHints api_base host → provider 推断表（21 条）。
	// Python: reasoning.py:64-86 _API_BASE_PROVIDER_HINTS
	apiBaseProviderHints = [][2]string{
		{"openrouter.ai", "openrouter"},
		{"dashscope.aliyuncs.com", "dashscope"},
		{"dashscope-intl.aliyuncs.com", "dashscope"},
		{"maas.aliyuncs.com", "dashscope"},
		{"api.deepseek.com", "deepseek"},
		{"api.moonshot.cn", "moonshot"},
		{"api.moonshot.ai", "moonshot"},
		{"api.kimi.com", "moonshot"},
		{"api.minimax.io", "minimax"},
		{"api.minimaxi.com", "minimax"},
		{"api.modelarts-maas.com", "modelarts"},
		{"api-ap-southeast-1.modelarts-maas.com", "modelarts"},
		{"qianfan.baidubce.com", "qianfan"},
		{"open.bigmodel.cn", "zhipu"},
		{"api.z.ai", "zhipu"},
		{"ark.cn-beijing.volces.com", "volcengine"},
		{"ark.volces.com", "volcengine"},
		{"api.xiaomimimo.com", "mimo"},
		{"token-plan-cn.xiaomimimo.com", "mimo"},
		{"token-plan-sgp.xiaomimimo.com", "mimo"},
		{"token-plan-ams.xiaomimimo.com", "mimo"},
	}
)

// 数据表由脚本从上游 AST 生成，避免 65 条 pattern 手抄出错。
// 生成脚本：解析 reasoning_profiles.py 的 REASONING_PROFILES / MODEL_REASONING_FALLBACKS 赋值表达式。
var (
	inlineCapability1 = ReasoningCapability{Options: []string{"off", "high", "xhigh"}, Recommended: strPtr("high")}
	inlineCapability2 = ReasoningCapability{Options: []string{"off", "high", "xhigh"}, Recommended: strPtr("high")}

	// REASONING_PROFILES 上游 42 条
	reasoningProfiles = []ReasoningProfile{
	{Provider: "openai", ModelPatterns: []string{"gpt-5.6*"}, Capability: openaiGPT56Capability, OpenAIWire: "standard_openai", AnthropicWire: "standard_anthropic"},
	{Provider: "openai", ModelPatterns: []string{"gpt-5.5*"}, Capability: openaiGPT55Capability, OpenAIWire: "standard_openai", AnthropicWire: "standard_anthropic"},
	{Provider: "openai", ModelPatterns: []string{"gpt-5.4*", "gpt-5.3*", "gpt-5.2*", "gpt-5.1*"}, Capability: openaiGPT51To54Capability, OpenAIWire: "standard_openai", AnthropicWire: "standard_anthropic"},
	{Provider: "openai", ModelPatterns: []string{"gpt-5*"}, Capability: openaiGPT5Capability, OpenAIWire: "standard_openai", AnthropicWire: "standard_anthropic"},
	{Provider: "anthropic", ModelPatterns: []string{"claude-sonnet-4-5*", "claude-sonnet-4.5*"}, Capability: anthropicManualBudgetCapability, OpenAIWire: "standard_openai", AnthropicWire: "anthropic_manual"},
	{Provider: "openrouter", ModelPatterns: []string{"openai/gpt-5.6-*"}, Capability: openaiGPT56Capability, OpenAIWire: "openrouter", AnthropicWire: "openrouter"},
	{Provider: "openrouter", ModelPatterns: []string{"qwen/qwen3.8-max*"}, Capability: qwen38Capability, OpenAIWire: "openrouter", AnthropicWire: "openrouter"},
	{Provider: "openrouter", ModelPatterns: []string{"z-ai/glm-5.2*"}, Capability: inlineCapability1, OpenAIWire: "openrouter", AnthropicWire: "openrouter"},
	{Provider: "openrouter", ModelPatterns: []string{"deepseek/deepseek-v4-pro*"}, Capability: inlineCapability2, OpenAIWire: "openrouter", AnthropicWire: "openrouter"},
	{Provider: "openrouter", ModelPatterns: []string{"moonshotai/kimi-k3*"}, Capability: kimiK3Capability, OpenAIWire: "openrouter", AnthropicWire: "openrouter"},
	{Provider: "openrouter", ModelPatterns: []string{"minimax/minimax-m3*"}, Capability: toggleCapability, OpenAIWire: "openrouter", AnthropicWire: "openrouter"},
	{Provider: "openrouter", ModelPatterns: []string{"anthropic/claude-sonnet-4.5*"}, Capability: toggleCapability, OpenAIWire: "openrouter", AnthropicWire: "openrouter"},
	{Provider: "openrouter", ModelPatterns: []string{"google/gemini-2.5-flash*", "poolside/laguna-s-2.1*"}, Capability: toggleCapability, OpenAIWire: "openrouter", AnthropicWire: "openrouter"},
	{Provider: "dashscope", ModelPatterns: []string{"qwen3.8-max*"}, Capability: qwen38Capability, OpenAIWire: "qwen38", AnthropicWire: "qwen38_anthropic"},
	{Provider: "dashscope", ModelPatterns: []string{"qwen3-vl-*-thinking*"}, Capability: alwaysOnCapability, OpenAIWire: "always_on", AnthropicWire: "always_on"},
	{Provider: "dashscope", ModelPatterns: []string{"qwen3*", "qwen-plus*", "qwen-max*", "qwen-flash*"}, Capability: toggleCapability, OpenAIWire: "enable_thinking", AnthropicWire: "dashscope_budget"},
	{Provider: "dashscope", ModelPatterns: []string{"deepseek-v4-*"}, Capability: deepseekV4Capability, OpenAIWire: "deepseek", AnthropicWire: "deepseek_anthropic"},
	{Provider: "dashscope", ModelPatterns: []string{"glm-5.2*"}, Capability: glm52Capability, OpenAIWire: "glm", AnthropicWire: "dashscope_glm_anthropic"},
	{Provider: "dashscope", ModelPatterns: []string{"glm-5*", "glm-4.7*", "kimi-k2.6*", "kimi-k2.5*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "dashscope", ModelPatterns: []string{"kimi-k3*"}, Capability: kimiK3Capability, OpenAIWire: "kimi_k3", AnthropicWire: "kimi_k3"},
	{Provider: "deepseek", ModelPatterns: []string{"deepseek-v4-*"}, Capability: deepseekV4Capability, OpenAIWire: "deepseek", AnthropicWire: "deepseek_anthropic"},
	{Provider: "moonshot", ModelPatterns: []string{"kimi-k3*"}, Capability: kimiK3Capability, OpenAIWire: "kimi_k3", AnthropicWire: "kimi_k3"},
	{Provider: "moonshot", ModelPatterns: []string{"kimi-k2.7-code*", "kimi-k2-thinking*"}, Capability: alwaysOnCapability, OpenAIWire: "always_on", AnthropicWire: "always_on"},
	{Provider: "moonshot", ModelPatterns: []string{"kimi-k2.6*", "kimi-k2.5*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "minimax", ModelPatterns: []string{"minimax-m3*"}, Capability: toggleCapability, OpenAIWire: "minimax_m3", AnthropicWire: "minimax_m3_anthropic"},
	{Provider: "minimax", ModelPatterns: []string{"minimax-m2*", "minimax-m1*"}, Capability: alwaysOnCapability, OpenAIWire: "minimax_m2", AnthropicWire: "always_on"},
	{Provider: "zhipu", ModelPatterns: []string{"glm-5.2*"}, Capability: glm52Capability, OpenAIWire: "glm", AnthropicWire: "glm_anthropic"},
	{Provider: "zhipu", ModelPatterns: []string{"glm-5*", "glm-4.7*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "zhipu", ModelPatterns: []string{"codegeex-4*"}, Capability: unsupportedCapability, OpenAIWire: "unsupported", AnthropicWire: "unsupported"},
	{Provider: "modelarts", ModelPatterns: []string{"openpangu-2.0-*", "glm-5.1*", "kimi-k2.6*", "qwen3-32b*", "qwen3-30b-a3b*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "modelarts", ModelPatterns: []string{"glm-5.2*"}, Capability: glm52Capability, OpenAIWire: "glm", AnthropicWire: "glm_anthropic"},
	{Provider: "modelarts", ModelPatterns: []string{"deepseek-v4-*"}, Capability: toggleCapability, OpenAIWire: "modelarts_deepseek", AnthropicWire: "modelarts_deepseek_anthropic"},
	{Provider: "volcengine", ModelPatterns: []string{"seed-2.0-*", "seed-2-0-*", "doubao-seed-2-0-*", "doubao-seed-2.0-*"}, Capability: volcengineSeed20Capability, OpenAIWire: "volcengine_seed", AnthropicWire: "standard_anthropic"},
	{Provider: "volcengine", ModelPatterns: []string{"seed-1.6*", "doubao-seed-1-6*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "volcengine", ModelPatterns: []string{"deepseek-v4-*"}, Capability: deepseekV4Capability, OpenAIWire: "deepseek", AnthropicWire: "deepseek_anthropic"},
	{Provider: "volcengine", ModelPatterns: []string{"doubao-1.5-pro*"}, Capability: unsupportedCapability, OpenAIWire: "unsupported", AnthropicWire: "unsupported"},
	{Provider: "qianfan", ModelPatterns: []string{"deepseek-v4-*"}, Capability: deepseekV4Capability, OpenAIWire: "deepseek", AnthropicWire: "deepseek_anthropic"},
	{Provider: "qianfan", ModelPatterns: []string{"glm-5.2*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "qianfan", ModelPatterns: []string{"glm-5*", "kimi-k2.6*", "kimi-k2.5*", "mimo-v2.5*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "qianfan", ModelPatterns: []string{"ernie-5.*", "emie-5.*"}, Capability: toggleCapability, OpenAIWire: "qianfan_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "qianfan", ModelPatterns: []string{"qianfan-ocr-*"}, Capability: unsupportedCapability, OpenAIWire: "unsupported", AnthropicWire: "unsupported"},
	{Provider: "mimo", ModelPatterns: []string{"mimo-v2.5*", "mino-v2.5*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	}
	// MODEL_REASONING_FALLBACKS 上游 23 条
	modelReasoningFallbacks = []ReasoningProfile{
	{Provider: "*", ModelPatterns: []string{"gpt-5.6*"}, Capability: openaiGPT56Capability, OpenAIWire: "standard_openai", AnthropicWire: "standard_anthropic"},
	{Provider: "*", ModelPatterns: []string{"gpt-5.5*"}, Capability: openaiGPT55Capability, OpenAIWire: "standard_openai", AnthropicWire: "standard_anthropic"},
	{Provider: "*", ModelPatterns: []string{"gpt-5.4*", "gpt-5.3*", "gpt-5.2*", "gpt-5.1*"}, Capability: openaiGPT51To54Capability, OpenAIWire: "standard_openai", AnthropicWire: "standard_anthropic"},
	{Provider: "*", ModelPatterns: []string{"gpt-5*"}, Capability: openaiGPT5Capability, OpenAIWire: "standard_openai", AnthropicWire: "standard_anthropic"},
	{Provider: "*", ModelPatterns: []string{"claude-sonnet-4-5*", "claude-sonnet-4.5*"}, Capability: anthropicManualBudgetCapability, OpenAIWire: "standard_openai", AnthropicWire: "anthropic_manual"},
	{Provider: "*", ModelPatterns: []string{"qwen3.8-max*"}, Capability: qwen38Capability, OpenAIWire: "qwen38", AnthropicWire: "qwen38_anthropic"},
	{Provider: "*", ModelPatterns: []string{"qwen3-vl-*-thinking*"}, Capability: alwaysOnCapability, OpenAIWire: "always_on", AnthropicWire: "always_on"},
	{Provider: "*", ModelPatterns: []string{"qwen3*", "qwen-plus*", "qwen-max*", "qwen-flash*"}, Capability: toggleCapability, OpenAIWire: "enable_thinking", AnthropicWire: "dashscope_budget"},
	{Provider: "*", ModelPatterns: []string{"deepseek-v4-*"}, Capability: deepseekV4Capability, OpenAIWire: "deepseek", AnthropicWire: "deepseek_anthropic"},
	{Provider: "*", ModelPatterns: []string{"kimi-k3*"}, Capability: kimiK3Capability, OpenAIWire: "kimi_k3", AnthropicWire: "kimi_k3"},
	{Provider: "*", ModelPatterns: []string{"kimi-k2.7-code*", "kimi-k2-thinking*"}, Capability: alwaysOnCapability, OpenAIWire: "always_on", AnthropicWire: "always_on"},
	{Provider: "*", ModelPatterns: []string{"kimi-k2.6*", "kimi-k2.5*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "*", ModelPatterns: []string{"minimax-m3*"}, Capability: toggleCapability, OpenAIWire: "minimax_m3", AnthropicWire: "minimax_m3_anthropic"},
	{Provider: "*", ModelPatterns: []string{"minimax-m2*", "minimax-m1*"}, Capability: alwaysOnCapability, OpenAIWire: "minimax_m2", AnthropicWire: "always_on"},
	{Provider: "*", ModelPatterns: []string{"glm-5.2*"}, Capability: glm52Capability, OpenAIWire: "glm", AnthropicWire: "glm_anthropic"},
	{Provider: "*", ModelPatterns: []string{"glm-5*", "glm-4.7*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "*", ModelPatterns: []string{"codegeex-4*"}, Capability: unsupportedCapability, OpenAIWire: "unsupported", AnthropicWire: "unsupported"},
	{Provider: "*", ModelPatterns: []string{"openpangu-2.0-*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "*", ModelPatterns: []string{"seed-2.0-*", "seed-2-0-*", "doubao-seed-2-0-*", "doubao-seed-2.0-*"}, Capability: volcengineSeed20Capability, OpenAIWire: "volcengine_seed", AnthropicWire: "standard_anthropic"},
	{Provider: "*", ModelPatterns: []string{"seed-1.6*", "doubao-seed-1-6*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "*", ModelPatterns: []string{"doubao-1.5-pro*", "qianfan-ocr-*"}, Capability: unsupportedCapability, OpenAIWire: "unsupported", AnthropicWire: "unsupported"},
	{Provider: "*", ModelPatterns: []string{"ernie-5.*", "emie-5.*"}, Capability: toggleCapability, OpenAIWire: "qianfan_toggle", AnthropicWire: "anthropic_toggle"},
	{Provider: "*", ModelPatterns: []string{"mimo-v2.5*", "mino-v2.5*"}, Capability: toggleCapability, OpenAIWire: "thinking_toggle", AnthropicWire: "anthropic_toggle"},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// FindReasoningProfile 按 provider + 模型名查找档案。
// 先查 provider 作用域表，再查跨 provider 的模型名 fallback 表。
// Python: reasoning_profiles.py:367-383 find_reasoning_profile
func FindReasoningProfile(provider, model string) *ReasoningProfile {
	providerKey := normalizeProviderKey(provider)
	modelKey := strings.ToLower(strings.TrimSpace(model))

	// 1. provider 作用域优先
	for i := range reasoningProfiles {
		p := &reasoningProfiles[i]
		if p.Provider != providerKey {
			continue
		}
		if matchAnyPattern(modelKey, p.ModelPatterns) {
			return p
		}
	}

	// 2. fallback：同时用完整 modelKey 与去掉 vendor 前缀的裸名匹配
	// Python: reasoning_profiles.py:375 fallback_model_keys = {model_key, model_key.rsplit("/",1)[-1]}
	candidates := []string{modelKey}
	if idx := strings.LastIndex(modelKey, "/"); idx >= 0 {
		candidates = append(candidates, modelKey[idx+1:])
	}
	for i := range modelReasoningFallbacks {
		p := &modelReasoningFallbacks[i]
		for _, c := range candidates {
			if matchAnyPattern(c, p.ModelPatterns) {
				return p
			}
		}
	}
	return nil
}

// StandardReasoningCapability 返回协议默认能力。
// Python: reasoning_profiles.py:386-389 standard_reasoning_capability
func StandardReasoningCapability(protocol string) ReasoningCapability {
	if strings.ToLower(strings.TrimSpace(protocol)) == "anthropic_messages" {
		return standardAnthropicCapability
	}
	return standardOpenAICapability
}

// ProfileReasoningCapability 返回档案在指定协议下的能力。
// Python: reasoning_profiles.py:391-395 profile_reasoning_capability
func ProfileReasoningCapability(profile *ReasoningProfile, protocol string) ReasoningCapability {
	if profile == nil {
		return StandardReasoningCapability(protocol)
	}
	if strings.ToLower(strings.TrimSpace(protocol)) == "anthropic_messages" && profile.AnthropicCapability != nil {
		return *profile.AnthropicCapability
	}
	return profile.Capability
}

// ProviderKey 由 provider 名 / endpoint_profile / api_base 推断生效的 provider key。
// 顺序：非通用 endpoint_profile 直接用 → api_base host 推断 → provider 名。
// Python: reasoning.py:113-119 _provider_key_from_values
func ProviderKey(provider, endpointProfile, apiBase string) string {
	profile := normalizeProviderKey(endpointProfile)
	if profile != "" {
		if _, generic := genericEndpointProfiles[profile]; !generic {
			return profile
		}
	}
	if inferred := providerFromAPIBase(apiBase); inferred != "" {
		return inferred
	}
	if profile != "" {
		return profile
	}
	return normalizeProviderKey(provider)
}

// ProviderFromAPIBase 由 api_base host 推断 provider；无法推断返回空串。
// Python: reasoning.py:122-131 _provider_from_api_base
func ProviderFromAPIBase(apiBase string) string { return providerFromAPIBase(apiBase) }

// GetReasoningCapability 解析 UI 可用的思考档位，不构造客户端。
// 表内模型返回精确能力；未列模型继承所选协议的默认能力。
// Python: reasoning.py:150-177 get_reasoning_capability
func GetReasoningCapability(provider, model, protocol, apiBase, endpointProfile string) ReasoningCapability {
	protocolKey := normalizeProtocolKey(protocol)
	providerKey := ProviderKey(provider, endpointProfile, apiBase)
	if profile := FindReasoningProfile(providerKey, model); profile != nil {
		return ProfileReasoningCapability(profile, protocolKey)
	}
	return StandardReasoningCapability(protocolKey)
}

// GetProviderReasoningRules 返回该 provider 的 pattern 规则（供前端能力匹配）。
// 顺序敏感：调用方须取第一条命中项，与 FindReasoningProfile 一致。
// Python: reasoning.py:180-215 get_provider_reasoning_rules
func GetProviderReasoningRules(provider, apiBase, endpointProfile string) []map[string]any {
	providerKey := ProviderKey(provider, endpointProfile, apiBase)
	out := make([]map[string]any, 0, len(reasoningProfiles))
	for i := range reasoningProfiles {
		p := &reasoningProfiles[i]
		if p.Provider != providerKey {
			continue
		}
		out = append(out, map[string]any{
			"patterns": p.ModelPatterns,
			"capabilities": map[string]any{
				"openai":    capabilityToMap(ProfileReasoningCapability(p, protocolChatCompletions)),
				"anthropic": capabilityToMap(ProfileReasoningCapability(p, protocolAnthropicMessages)),
			},
		})
	}
	return out
}

// GetReasoningCapabilityCatalog 返回前端用的紧凑能力目录。
// Python: reasoning.py:218-242 get_reasoning_capability_catalog
func GetReasoningCapabilityCatalog() map[string]any {
	fallbacks := make([]any, 0, len(modelReasoningFallbacks))
	for i := range modelReasoningFallbacks {
		p := &modelReasoningFallbacks[i]
		fallbacks = append(fallbacks, map[string]any{
			"patterns": p.ModelPatterns,
			"capabilities": map[string]any{
				"openai":    capabilityToMap(ProfileReasoningCapability(p, protocolChatCompletions)),
				"anthropic": capabilityToMap(ProfileReasoningCapability(p, protocolAnthropicMessages)),
			},
		})
	}
	return map[string]any{
		"protocol_defaults": map[string]any{
			"openai":    capabilityToMap(StandardReasoningCapability(protocolChatCompletions)),
			"anthropic": capabilityToMap(StandardReasoningCapability(protocolAnthropicMessages)),
		},
		"model_fallbacks": fallbacks,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeProviderKey provider/endpoint 名归一：小写 + 下划线转横线。
// Python: reasoning_profiles.py:368 / reasoning.py:114
func normalizeProviderKey(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), "_", "-")
}

// normalizeProtocolKey 协议名归一（接受别名）。
// Python: reasoning.py:164-168
func normalizeProtocolKey(protocol string) string {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "anthropic", "messages", "anthropic-messages":
		return protocolAnthropicMessages
	case "openai", "chat", "chat-completions":
		return protocolChatCompletions
	}
	return strings.ToLower(strings.TrimSpace(protocol))
}

// matchAnyPattern 用 fnmatch 逐条匹配（对齐 Python fnmatchcase：区分大小写）。
// Python: reasoning_profiles.py:373 any(fnmatchcase(model_key, pattern) for pattern in ...)
func matchAnyPattern(name string, patterns []string) bool {
	for _, p := range patterns {
		if fnmatch.Match(p, name, 0) {
			return true
		}
	}
	return false
}

// providerFromAPIBase 由 api_base host 推断 provider。
// Python: reasoning.py:122-131 _provider_from_api_base
func providerFromAPIBase(apiBase string) string {
	raw := strings.ToLower(strings.TrimSpace(apiBase))
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	host := u.Hostname()
	for _, hint := range apiBaseProviderHints {
		suffix, provider := hint[0], hint[1]
		if host == suffix || strings.HasSuffix(host, "."+suffix) {
			return provider
		}
	}
	return ""
}

// capabilityToMap 把能力转成前端可消费的 map。
// Python: reasoning_profiles.py:25-26 ReasoningCapability.to_dict
func capabilityToMap(c ReasoningCapability) map[string]any {
	options := c.Options
	if options == nil {
		options = []string{}
	}
	var recommended any
	if c.Recommended != nil {
		recommended = *c.Recommended
	}
	return map[string]any{"options": options, "recommended": recommended}
}

// capPtr 返回能力指针（供 AnthropicCapability 字段使用）。
func capPtr(c ReasoningCapability) *ReasoningCapability { return &c }

// strPtr 返回字符串指针。
func strPtr(s string) *string { return &s }
