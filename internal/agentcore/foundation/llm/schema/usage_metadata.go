package schema

// ──────────────────────────── 结构体 ────────────────────────────

// UsageMetadata 模型调用用量元数据，记录 token 消耗、延迟和费用信息。
//
// 所有数值字段默认为零值，保证实例化时无需提供任何参数。
//
// 对应 Python: openjiuwen/core/foundation/llm/schema/message.py (UsageMetadata)
type UsageMetadata struct {
	// Code 状态码，0 表示成功
	Code int `json:"code"`
	// ErrMsg 错误消息
	ErrMsg string `json:"err_msg"`
	// Prompt 提示词
	Prompt string `json:"prompt"`
	// TaskID 任务 ID
	TaskID string `json:"task_id"`
	// ModelName 模型名称
	ModelName string `json:"model_name"`
	// TotalLatency 总延迟（秒）
	TotalLatency float64 `json:"total_latency"`
	// FirstTokenTime 首个 token 时间
	FirstTokenTime string `json:"first_token_time"`
	// RequestStartTime 请求开始时间
	RequestStartTime string `json:"request_start_time"`
	// InputTokens 输入 token 数
	InputTokens int `json:"input_tokens"`
	// OutputTokens 输出 token 数
	OutputTokens int `json:"output_tokens"`
	// TotalTokens 总 token 数
	TotalTokens int `json:"total_tokens"`
	// CacheTokens 缓存 token 数
	CacheTokens int `json:"cache_tokens"`
	// InputCost 输入费用
	InputCost float64 `json:"input_cost"`
	// OutputCost 输出费用
	OutputCost float64 `json:"output_cost"`
	// TotalCost 总费用
	TotalCost float64 `json:"total_cost"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewUsageMetadata 创建 UsageMetadata 实例，所有数值字段默认为零值。
//
// 对应 Python: UsageMetadata() — Pydantic BaseModel 默认值即零值
func NewUsageMetadata() *UsageMetadata {
	return &UsageMetadata{}
}
