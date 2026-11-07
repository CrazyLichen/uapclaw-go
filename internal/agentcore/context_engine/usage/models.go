package usage

import "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"

// ──────────────────────────── 结构体 ────────────────────────────

// ContextPartUsage 单个类别的用量。
// Python: ContextPartUsage
type ContextPartUsage struct {
	// Category 类别标识（与 map 键一致）
	Category string `json:"category"`
	// Tokens 该类别的 token 数
	Tokens int `json:"tokens"`
	// PercentageOfWindow 占窗口比例（0~1），窗口未知为 nil
	PercentageOfWindow *float64 `json:"percentage_of_window"`
	// Estimated 是否为估算值
	Estimated bool `json:"estimated"`
	// Source 计量来源（tokenizer / provider_usage）
	Source string `json:"source"`
}

// ContextWindowUsage 上下文窗口用量。
// Python: ContextWindowUsage
type ContextWindowUsage struct {
	// LimitTokens 窗口上限，nil 表示未知
	LimitTokens *int `json:"limit_tokens"`
	// InputTokens 本轮输入 token，nil 表示未上报
	InputTokens *int `json:"input_tokens"`
	// OccupancyRate 占用率（0~1），窗口未知为 nil
	OccupancyRate *float64 `json:"occupancy_rate"`
	// TokensSource 输入量来源（provider_usage / local_estimate）
	TokensSource string `json:"tokens_source"`
}

// RequestKVCacheUsage 单次请求的 KV cache 用量。
// Python: RequestKVCacheUsage
type RequestKVCacheUsage struct {
	// InputTokens provider 上报的输入 token
	InputTokens *int `json:"input_tokens"`
	// CacheReadTokens 缓存命中读入 token
	CacheReadTokens *int `json:"cache_read_tokens"`
	// CacheMissTokens 未命中 token（输入 − 命中）
	CacheMissTokens *int `json:"cache_miss_tokens"`
	// HitRate 命中率（0~1）
	HitRate *float64 `json:"hit_rate"`
	// Status 上报状态（reported / not_reported）
	Status string `json:"status"`
	// Source 数据来源
	Source string `json:"source"`
}

// SessionKVCacheUsage 会话级 KV cache 聚合。
// Python: SessionKVCacheUsage
type SessionKVCacheUsage struct {
	// CallsTotal 调用总次数
	CallsTotal int `json:"calls_total"`
	// CallsObserved 上报完整的次数
	CallsObserved int `json:"calls_observed"`
	// CallsPartial 只上报了输入的次数
	CallsPartial int `json:"calls_partial"`
	// CallsNotReported 完全未上报的次数
	CallsNotReported int `json:"calls_not_reported"`
	// InputTokensTotal 输入 token 合计
	InputTokensTotal int `json:"input_tokens_total"`
	// CacheReadTokensTotal 命中 token 合计
	CacheReadTokensTotal int `json:"cache_read_tokens_total"`
	// WeightedHitRate token 加权命中率
	WeightedHitRate *float64 `json:"weighted_hit_rate"`
	// Status 聚合状态
	Status string `json:"status"`
	// DataQuality 数据完整度（full / partial / not_reported）
	DataQuality string `json:"data_quality"`
}

// ContextUsageSnapshot 上下文用量快照（context-usage.v1）。
// 字段名与前端校验一一对应：缺 event_type / schema_version / phase /
// request_id / product_session_id / context_window / parts 中任一项，前端整条丢弃。
// Python: ContextUsageSnapshot
type ContextUsageSnapshot struct {
	// EventType 事件类型，固定 context.usage
	EventType string `json:"event_type"`
	// SchemaVersion 协议版本，固定 context-usage.v1
	SchemaVersion string `json:"schema_version"`
	// Phase 采集时点（pre_call / post_call / post_compact）
	Phase string `json:"phase"`
	// RequestID 请求标识
	RequestID string `json:"request_id"`
	// SessionID 会话标识
	SessionID string `json:"session_id"`
	// ProductSessionID 产品侧会话标识（前端靠它归属）
	ProductSessionID string `json:"product_session_id"`
	// AgentID agent 标识
	AgentID string `json:"agent_id,omitempty"`
	// TeamID 团队标识（无团队为空串）
	TeamID string `json:"team_id"`
	// MemberName 团队成员名（非团队为空串）
	MemberName string `json:"member_name"`
	// Role 团队角色（leader / teammate，非团队为空）
	Role string `json:"role,omitempty"`
	// AgentPath 调用链路径
	AgentPath []string `json:"agent_path"`
	// Depth 调用深度
	Depth int `json:"depth"`
	// ContextWindow 窗口用量
	ContextWindow ContextWindowUsage `json:"context_window"`
	// Parts 分类用量
	Parts map[string]ContextPartUsage `json:"parts"`
	// SessionKVCacheHitRate 会话级加权命中率，nil 输出 null
	SessionKVCacheHitRate *float64 `json:"session_kv_cache_hit_rate"`
	// RequestCache 本轮 KV cache 用量
	RequestCache RequestKVCacheUsage `json:"request_kv_cache"`
	// SessionCache 会话级 KV cache 聚合
	SessionCache *SessionKVCacheUsage `json:"session_kv_cache,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// 快照采集时点。
const (
	// PhasePreCall 模型调用前
	PhasePreCall = "pre_call"
	// PhasePostCall 模型调用后
	PhasePostCall = "post_call"
	// PhasePostCompact 压缩后
	PhasePostCompact = "post_compact"
)

// ──────────────────────────── 常量 ────────────────────────────

// 上下文用量分类。
const (
	// CategorySystemPrompt 系统提示词
	CategorySystemPrompt = "system_prompt"
	// CategoryTools 工具定义
	CategoryTools = "tools"
	// CategorySkills 技能
	CategorySkills = "skills"
	// CategoryMessages 消息
	CategoryMessages = "messages"
)

// SchemaVersion 事件协议版本。
const SchemaVersion = "context-usage.v1"

// EventTypeContextUsage 事件类型。
const EventTypeContextUsage = "context.usage"

// upstreamUsage 判断模型是否上报了可用的输入量。
func providerInputTokens(meta *schema.UsageMetadata) *int {
	if meta == nil || meta.InputTokens <= 0 {
		return nil
	}
	input := meta.InputTokens
	return &input
}
