package openai

// ──────────────────────────── 结构体 ────────────────────────────

// ChatCompletionResponse OpenAI Chat Completion API 非流式响应结构体。
//
// 对应 OpenAI API 文档: https://platform.openai.com/docs/api-reference/chat/object
// 包含 vLLM 等兼容服务的扩展字段。
type ChatCompletionResponse struct {
	// ID 响应唯一标识
	ID string `json:"id"`
	// Object 对象类型，通常为 "chat.completion"
	Object string `json:"object"`
	// Created 创建时间戳
	Created int64 `json:"created"`
	// Model 实际使用的模型名称
	Model string `json:"model"`
	// Choices 响应选项列表
	Choices []ResponseChoice `json:"choices"`
	// Usage 用量信息（可选）
	Usage *ResponseUsage `json:"usage,omitempty"`
	// PromptTokenIDs vLLM 扩展：输入 token ID 列表
	PromptTokenIDs []int `json:"prompt_token_ids,omitempty"`
}

// ResponseChoice 非流式响应选项。
type ResponseChoice struct {
	// Index 选项索引
	Index int `json:"index"`
	// Message 消息内容
	Message *ResponseMessage `json:"message"`
	// FinishReason 完成原因（"stop" / "tool_calls" / "length" 等）
	FinishReason string `json:"finish_reason"`
	// Logprobs 对数概率信息（可选）
	Logprobs any `json:"logprobs,omitempty"`
	// TokenIDs vLLM 扩展：输出 token ID 列表
	TokenIDs []int `json:"token_ids,omitempty"`
}

// ResponseMessage 非流式响应消息。
type ResponseMessage struct {
	// Role 角色类型，通常为 "assistant"
	Role string `json:"role"`
	// Content 文本内容（nullable，tool_calls 场景可能为 null）
	Content *string `json:"content"`
	// ReasoningContent 推理内容（思维链，如 DeepSeek-R1）
	ReasoningContent *string `json:"reasoning_content,omitempty"`
	// ToolCalls 工具调用列表（可选）
	ToolCalls []ResponseToolCall `json:"tool_calls,omitempty"`
}

// ResponseToolCall 非流式响应中的工具调用。
type ResponseToolCall struct {
	// ID 工具调用 ID
	ID string `json:"id,omitempty"`
	// Type 类型，通常为 "function"
	Type string `json:"type"`
	// Function 函数调用信息
	Function ResponseFunction `json:"function"`
	// Index 工具调用索引（可选，流式场景使用）
	Index *int `json:"index,omitempty"`
}

// ResponseFunction 函数调用信息。
type ResponseFunction struct {
	// Name 函数名称
	Name string `json:"name,omitempty"`
	// Arguments 函数参数（JSON 字符串）
	Arguments string `json:"arguments,omitempty"`
}

// ResponseUsage 用量信息。
type ResponseUsage struct {
	// PromptTokens 输入 token 数
	PromptTokens int `json:"prompt_tokens"`
	// CompletionTokens 输出 token 数
	CompletionTokens int `json:"completion_tokens"`
	// TotalTokens 总 token 数
	TotalTokens int `json:"total_tokens"`
	// PromptTokensDetails 输入 token 明细（含缓存信息）
	PromptTokensDetails *ResponsePromptTokensDetails `json:"prompt_tokens_details,omitempty"`
	// Cost 费用信息（部分 provider 返回，可能是数值或对象）
	Cost any `json:"cost,omitempty"`
	// UsageCost 费用信息（部分 provider 返回，可能是数值或对象）
	UsageCost any `json:"usage_cost,omitempty"`
	// CostDetails 费用明细（部分 provider 返回）
	CostDetails any `json:"cost_details,omitempty"`
}

// ResponsePromptTokensDetails 输入 token 明细。
type ResponsePromptTokensDetails struct {
	// CachedTokens 缓存命中 token 数
	CachedTokens int `json:"cached_tokens"`
}

// ChatCompletionChunkResponse OpenAI Chat Completion API 流式响应块。
//
// 对应 OpenAI API 文档: https://platform.openai.com/docs/api-reference/chat/streaming
type ChatCompletionChunkResponse struct {
	// ID 响应唯一标识
	ID string `json:"id"`
	// Object 对象类型，通常为 "chat.completion.chunk"
	Object string `json:"object"`
	// Created 创建时间戳
	Created int64 `json:"created"`
	// Model 实际使用的模型名称
	Model string `json:"model"`
	// Choices 响应选项列表（流式块可能为空，仅携带 usage）
	Choices []ChunkChoice `json:"choices,omitempty"`
	// Usage 用量信息（流式最后一个块或 stream_options.include_usage=true 时返回）
	Usage *ResponseUsage `json:"usage,omitempty"`
	// PromptTokenIDs vLLM 扩展：输入 token ID 列表（仅首个块携带）
	PromptTokenIDs []int `json:"prompt_token_ids,omitempty"`
}

// ChunkChoice 流式响应选项。
type ChunkChoice struct {
	// Index 选项索引
	Index int `json:"index"`
	// Delta 增量消息内容
	Delta *ChunkDelta `json:"delta"`
	// FinishReason 完成原因（nullable，流式中间块为 null）
	FinishReason *string `json:"finish_reason"`
	// Logprobs 对数概率信息（可选）
	Logprobs any `json:"logprobs,omitempty"`
	// TokenIDs vLLM 扩展：输出 token ID 列表
	TokenIDs []int `json:"token_ids,omitempty"`
}

// ChunkDelta 流式响应增量消息。
type ChunkDelta struct {
	// Role 角色类型（仅首个块可能携带）
	Role string `json:"role,omitempty"`
	// Content 增量文本内容（nullable）
	Content *string `json:"content"`
	// ReasoningContent 增量推理内容（可选）
	ReasoningContent *string `json:"reasoning_content,omitempty"`
	// ToolCalls 增量工具调用列表（可选）
	ToolCalls []ChunkToolCall `json:"tool_calls,omitempty"`
	// TokenIDs vLLM 扩展：增量 token ID 列表
	TokenIDs []int `json:"token_ids,omitempty"`
}

// ChunkToolCall 流式响应增量工具调用。
type ChunkToolCall struct {
	// ID 工具调用 ID（仅首个 delta 携带）
	ID string `json:"id,omitempty"`
	// Type 类型，通常为 "function"
	Type string `json:"type,omitempty"`
	// Function 增量函数调用信息
	Function ChunkFunction `json:"function"`
	// Index 工具调用索引
	Index *int `json:"index,omitempty"`
}

// ChunkFunction 流式响应增量函数调用。
type ChunkFunction struct {
	// Name 函数名称（仅首个 delta 携带）
	Name string `json:"name,omitempty"`
	// Arguments 增量函数参数（逐 chunk 拼接）
	Arguments string `json:"arguments,omitempty"`
}

// ErrorResponse OpenAI API 错误响应。
type ErrorResponse struct {
	// Error 错误详情
	Error ErrorDetail `json:"error"`
}

// ErrorDetail 错误详情。
type ErrorDetail struct {
	// Message 错误消息
	Message string `json:"message"`
	// Type 错误类型
	Type string `json:"type"`
	// Code 错误代码
	Code string `json:"code,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
