package e2a

import (
	"fmt"
	"strconv"
)

// ──────────────────────────── 结构体 ────────────────────────────

// E2AResponse E2A 统一响应：每条出站记录（含流式多帧）一条实例；与 E2AEnvelope 对称。
//
// Metadata：通道/业务自定义键值；兼容旧版 AgentResponse.metadata；协议转换失败时可临时写入兜底信息
// （如原始片段、错误说明），与 a2a_metadata / acp_meta 分工不同。
//
// 对应 Python: jiuwenswarm/common/e2a/models.py (E2AResponse)
type E2AResponse struct {
	// ─── 核心响应字段 ───
	// ProtocolVersion E2A 载荷版本（默认 "1.0"）
	ProtocolVersion string `json:"protocol_version"`
	// ResponseID 响应唯一标识
	ResponseID string `json:"response_id"`
	// RequestID 对应请求 id
	RequestID string `json:"request_id"`
	// Sequence 流式序号（默认 0）
	Sequence int `json:"sequence"`
	// IsFinal 是否为最终响应（默认 false）
	IsFinal bool `json:"is_final"`
	// Status 响应状态（默认 "in_progress"）
	Status string `json:"status"`
	// ResponseKind 响应类型（对应 constants 中 E2AResponseKind*）
	ResponseKind string `json:"response_kind"`
	// Timestamp RFC 3339 UTC 字符串
	Timestamp string `json:"timestamp"`
	// Provenance 出处
	Provenance E2AProvenance `json:"provenance"`
	// Body 响应体
	Body map[string]any `json:"body"`

	// ─── 关联字段 ───
	// JSONRPCID JSON-RPC id（str / int / None）
	JSONRPCID any `json:"jsonrpc_id"`
	// CorrelationID 分布式追踪 id
	CorrelationID string `json:"correlation_id"`
	// TaskID 对齐 A2A task id
	TaskID string `json:"task_id"`
	// ContextID 对齐 A2A context id
	ContextID string `json:"context_id"`
	// SessionID 会话 id
	SessionID string `json:"session_id"`
	// MessageID 消息 id
	MessageID string `json:"message_id"`
	// IsStream 是否流式
	IsStream bool `json:"is_stream"`
	// IdentityOrigin 身份来源（默认 AGENT，与 E2AEnvelope 默认 USER 对称）
	IdentityOrigin IdentityOrigin `json:"identity_origin"`
	// Channel 渠道标识
	Channel string `json:"channel"`
	// UserID 用户 id
	UserID string `json:"user_id"`
	// SourceAgentID 来源 Agent id
	SourceAgentID string `json:"source_agent_id"`
	// Method 网关 RPC 方法名
	Method string `json:"method"`

	// ─── 扩展槽 ───
	// Projections 投影
	Projections map[string]any `json:"projections"`
	// ChannelContext 通道上下文
	ChannelContext map[string]any `json:"channel_context"`
	// Metadata 通道/业务自定义键值（兼容旧版 AgentResponse.metadata）
	Metadata map[string]any `json:"metadata"`
	// A2AMetadata A2A 互操作元数据
	A2AMetadata map[string]any `json:"a2a_metadata"`
	// ACPMeta ACP 互操作元数据
	ACPMeta map[string]any `json:"acp_meta"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewE2AResponse 创建 E2AResponse 实例，设置默认值。
// 默认值：ProtocolVersion="1.0", Status="in_progress", IdentityOrigin=AGENT, Provenance=NewE2AProvenance()
func NewE2AResponse() *E2AResponse {
	return &E2AResponse{
		ProtocolVersion: E2AProtocolVersion,
		Status:          E2AResponseStatusInProgress,
		IdentityOrigin:  IdentityOriginAgent,
		Provenance:      *NewE2AProvenance(),
	}
}

// EnsureTimestamp 若未设置 Timestamp，则填当前 UTC ISO8601。
// 对应 Python: E2AResponse.ensure_timestamp()
func (r *E2AResponse) EnsureTimestamp() {
	if r.Timestamp == "" {
		r.Timestamp = UTCNowISO()
	}
}

// ToMap 序列化为 JSON 友好 map（枚举转为值）。
// 对应 Python: E2AResponse.to_dict() → _dataclass_to_json_dict()
func (r *E2AResponse) ToMap() map[string]any {
	return structToMap(r)
}

// ResponseFromMap 从 map 反序列化为 E2AResponse。
// 对应 Python: E2AResponse.from_dict(data) → _e2a_response_from_dict(data)
func ResponseFromMap(data map[string]any) *E2AResponse {
	// 1. provenance 解析（无 legacy binding 迁移，与 envelope 不同）
	prov := provenanceFromMap(data["provenance"])

	// 2. identity_origin 解析（默认 AGENT，与 envelope 默认 USER 不同）
	origin := IdentityOriginAgent
	if raw, exists := data["identity_origin"]; exists {
		if s, ok := raw.(string); ok && IsValidIdentityOrigin(s) {
			origin, _ = ParseIdentityOrigin(s)
		}
	}

	// 3. sequence 解析（容错：非整数 → 0，对齐 Python try/except）
	sequence := 0
	if raw, exists := data["sequence"]; exists {
		switch v := raw.(type) {
		case float64:
			sequence = int(v)
		case int:
			sequence = v
		case int64:
			sequence = int(v)
		default:
			s := fmt.Sprintf("%v", raw)
			if n, e := parseIntSafe(s); e == nil {
				sequence = n
			}
		}
	}

	// 4. channel 兼容 channel_id（与 envelope 相同逻辑）
	ch := getString(data, "channel")
	if ch == "" {
		ch = getString(data, "channel_id")
	}

	// 5. 构造 E2AResponse
	return &E2AResponse{
		ProtocolVersion: getStringWithDefault(data, "protocol_version", E2AProtocolVersion),
		ResponseID:      getString(data, "response_id"),
		RequestID:       getString(data, "request_id"),
		Sequence:        sequence,
		IsFinal:         getBool(data, "is_final", false),
		Status:          getStringWithDefault(data, "status", E2AResponseStatusInProgress),
		ResponseKind:    getStringOrEmpty(data, "response_kind"),
		Timestamp:       normalizeTimestampValue(data["timestamp"]),
		Provenance:      prov,
		Body:            getMapAny(data, "body"),
		JSONRPCID:       data["jsonrpc_id"],
		CorrelationID:   getString(data, "correlation_id"),
		TaskID:          getString(data, "task_id"),
		ContextID:       getString(data, "context_id"),
		SessionID:       getString(data, "session_id"),
		MessageID:       getString(data, "message_id"),
		IsStream:        getBool(data, "is_stream", false),
		IdentityOrigin:  origin,
		Channel:         ch,
		UserID:          getString(data, "user_id"),
		SourceAgentID:   getString(data, "source_agent_id"),
		Method:          getString(data, "method"),
		Projections:     getMapAny(data, "projections"),
		ChannelContext:  getMapAny(data, "channel_context"),
		Metadata:        getMapAny(data, "metadata"),
		A2AMetadata:     getMapAny(data, "a2a_metadata"),
		ACPMeta:         getMapAny(data, "acp_meta"),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseIntSafe 安全解析整数
func parseIntSafe(s string) (int, error) {
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return int(f), nil
	}
	return 0, fmt.Errorf("无法解析为整数: %q", s)
}
