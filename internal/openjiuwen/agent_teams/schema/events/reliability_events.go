// Package events 团队可靠性事件（25.1g）。
//
// 对应 Python：schema/events.py 的 AnomalyDetectedEvent / ExternalRuntimeRetryingEvent
// 及 TeamEvent.ANOMALY_DETECTED / EXTERNAL_RUNTIME_RETRYING。
package events

// ──────────────────────────── 常量 ────────────────────────────

// TeamEventAnomalyDetected 可靠性 detector 标记成员不健康状态时发布。
//
// Python: events.py TeamEvent.ANOMALY_DETECTED = "anomaly_detected"。
const TeamEventAnomalyDetected = "anomaly_detected"

// TeamEventExternalRuntimeRetrying 三方 SDK 运行时自动重试中发布。
//
// Python: events.py TeamEvent.EXTERNAL_RUNTIME_RETRYING = "external_runtime_retrying"。
const TeamEventExternalRuntimeRetrying = "external_runtime_retrying"

// ──────────────────────────── 结构体 ────────────────────────────

// AnomalyDetectedEvent 可靠性 detector 标记成员不健康状态。
//
// Python: events.py AnomalyDetectedEvent(BaseEventMessage)。成员级：member_name
// 为受影响成员。跨进程承载供 leader 的 reliability handler 按补救策略路由。
// kind/severity 是 AnomalyKind/Severity 的字符串值，schema 不依赖 reliability 包。
type AnomalyDetectedEvent struct {
	BaseEventMessage
	// Detector 产出异常的 detector 标识
	Detector string
	// Kind 异常类别（AnomalyKind 值）
	Kind string
	// Severity 严重度（Severity 值）
	Severity string
	// Summary 面向人/LLM 的一行描述
	Summary string
	// Evidence 支撑判定的证据快照
	Evidence map[string]any
	// PeerMember 团队级异常的对端成员（无对端为 nil）
	PeerMember *string
}

// ExternalRuntimeRetryingEvent 三方 SDK 运行时自动重试中。
//
// Python: events.py ExternalRuntimeRetryingEvent(BaseEventMessage)。成员级非持久
// 进度信号——不结束当前轮、不改变成员最终状态。跨进程承载供 leader 的
// ExternalRuntimeHandler 上浮重试进度。agent_kind/phase/category 为 external_runtime
// 包的字符串值（D-g3：ExternalRuntimeFailureReason 本批以 map 形状承载，
// 15.7/15.8 移植后改 typed）。
type ExternalRuntimeRetryingEvent struct {
	BaseEventMessage
	// AgentKind 产生重试的 SDK
	AgentKind string
	// Model CLI/SDK 确认的生效模型（未知为空）
	Model string
	// Phase 启动或轮次（external_runtime 值）
	Phase string
	// Category 重试错误的失败类别（external_runtime 值）
	Category string
	// Summary 面向人/LLM 的一行描述
	Summary string
	// Reason 结构化原因快照（D-g3：map 形状，键 message/http_status/sdk_error_code）
	Reason map[string]any
	// Attempt 重试次数（未知为 nil）
	Attempt *int
	// MaxAttempts 最大重试次数（未知为 nil）
	MaxAttempts *int
	// RoundID 成员轮次 id（启动期为 nil）
	RoundID *int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EventTypeName 返回异常检出事件类型名。
func (e AnomalyDetectedEvent) EventTypeName() string { return TeamEventAnomalyDetected }

// ToPayload 转换为事件载荷。
//
// Python: pydantic model_dump（BaseEventMessage 含 team_name/member_name）。
func (e AnomalyDetectedEvent) ToPayload() map[string]any {
	payload := map[string]any{
		"team_name":   e.TeamName,
		"member_name": e.MemberName,
		"detector":    e.Detector,
		"kind":        e.Kind,
		"severity":    e.Severity,
		"summary":     e.Summary,
		"evidence":    e.Evidence,
	}
	if e.PeerMember != nil {
		payload["peer_member"] = *e.PeerMember
	} else {
		payload["peer_member"] = nil
	}
	return payload
}

// EventTypeName 返回三方运行时重试事件类型名。
func (e ExternalRuntimeRetryingEvent) EventTypeName() string { return TeamEventExternalRuntimeRetrying }

// ToPayload 转换为事件载荷。
//
// Python: pydantic model_dump。
func (e ExternalRuntimeRetryingEvent) ToPayload() map[string]any {
	payload := map[string]any{
		"team_name":    e.TeamName,
		"member_name":  e.MemberName,
		"agent_kind":   e.AgentKind,
		"model":        e.Model,
		"phase":        e.Phase,
		"category":     e.Category,
		"summary":      e.Summary,
		"reason":       e.Reason,
		"attempt":      e.Attempt,
		"max_attempts": e.MaxAttempts,
		"round_id":     e.RoundID,
	}
	return payload
}
