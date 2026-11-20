package reasoning

// ──────────────────────────── 常量 ────────────────────────────

// 协议标识。
// Python: schema/config.py:34-37 LLMApiMode
const (
	// protocolChatCompletions Chat Completions 协议
	protocolChatCompletions = "chat_completions"
	// protocolResponses Responses API 协议
	protocolResponses = "responses"
	// protocolAnthropicMessages Anthropic Messages 协议
	protocolAnthropicMessages = "anthropic_messages"
)

// wire 编码器名。
// Python: reasoning_profiles.py 表内 wire 字面量并集（25 个），
// 其中 unsupported/always_on 共用前置分支、qianfan_toggle 与 enable_thinking 共用。
const (
	wireAlwaysOn                   = "always_on"
	wireAnthropicManual            = "anthropic_manual"
	wireAnthropicToggle            = "anthropic_toggle"
	wireDashscopeBudget            = "dashscope_budget"
	wireDashscopeGlmAnthropic      = "dashscope_glm_anthropic"
	wireDeepSeek                   = "deepseek"
	wireDeepSeekAnthropic          = "deepseek_anthropic"
	wireEnableThinking             = "enable_thinking"
	wireGlm                        = "glm"
	wireGlmAnthropic               = "glm_anthropic"
	wireKimiK3                     = "kimi_k3"
	wireMinimaxM2                  = "minimax_m2"
	wireMinimaxM3                  = "minimax_m3"
	wireMinimaxM3Anthropic         = "minimax_m3_anthropic"
	wireModelartsDeepSeek          = "modelarts_deepseek"
	wireModelartsDeepSeekAnthropic = "modelarts_deepseek_anthropic"
	wireOpenRouter                 = "openrouter"
	wireQianfanToggle              = "qianfan_toggle"
	wireQwen38                     = "qwen38"
	wireQwen38Anthropic            = "qwen38_anthropic"
	wireStandardAnthropic          = "standard_anthropic"
	wireStandardOpenAI             = "standard_openai"
	wireThinkingToggle             = "thinking_toggle"
	wireUnsupported                = "unsupported"
	wireVolcengineSeed             = "volcengine_seed"

	// wireVLLM 不在 profile 表内，是 endpoint_profile 的 wire 覆盖
	// （在 profile 查找之前短路处理）。
	wireVLLM = "vllm"
)
