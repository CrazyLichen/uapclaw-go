package schema

// ──────────────────────────── 结构体 ────────────────────────────

// UsageMetadata 模型调用用量元数据，记录 token 消耗、延迟和费用信息。
// 所有数值字段默认为零值，保证实例化时无需提供任何参数。
// Python: openjiuwen/core/foundation/llm/schema/message.py (UsageMetadata)
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
	// CacheReadTokens 缓存读 token 数（nil=provider 未上报；与 CacheWriteTokens 分离的归一形态）
	CacheReadTokens *int `json:"cache_read_tokens,omitempty"`
	// CacheMissTokens 缓存 miss token 数（nil=未上报）
	CacheMissTokens *int `json:"cache_miss_tokens,omitempty"`
	// CacheWriteTokens 缓存写 token 数（nil=未上报）
	CacheWriteTokens *int `json:"cache_write_tokens,omitempty"`
	// CacheCreationTokens 缓存创建（写）token 数，独立提取形态（nil=未暴露）
	CacheCreationTokens *int `json:"cache_creation_tokens,omitempty"`
	// ReasoningTokens 推理/思考 token 数（output_tokens 的子集；nil=未上报）
	ReasoningTokens *int `json:"reasoning_tokens,omitempty"`
	// CacheStatus 缓存用量观测状态（"observed"=已观测到缓存用量）
	CacheStatus string `json:"cache_status,omitempty"`
	// CacheSource 缓存用量来源（"provider_usage"）
	CacheSource string `json:"cache_source,omitempty"`
	// CacheAuthoritative 缓存读值是否权威（legacy cache_tokens 不具权威性）
	CacheAuthoritative bool `json:"cache_authoritative,omitempty"`
	// InputCost 输入费用
	InputCost float64 `json:"input_cost"`
	// OutputCost 输出费用
	OutputCost float64 `json:"output_cost"`
	// TotalCost 总费用
	TotalCost float64 `json:"total_cost"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewUsageMetadata 创建 UsageMetadata 实例，所有数值字段默认为零值。
// Python: UsageMetadata() — Pydantic BaseModel 默认值即零值
func NewUsageMetadata() *UsageMetadata {
	return &UsageMetadata{}
}
