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
