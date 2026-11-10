package harness_protocol

// 本文件对应 Python openjiuwen/harness_protocol/events.py（456 行）：
// provider-neutral 的有序观测事件信封与其载荷。
// 本文件只定义契约与构造期校验，不含任何缓冲/游标行为
// （有界缓冲与单消费者游标在 15.5 的 provider 基座落地，Python 依据
// harness_providers/stream.py:19-155）。

// ──────────────────────────── 枚举 ────────────────────────────

// TurnEventKind 一轮外部输入驱动的轮次生命周期转换。
// Python: events.py:31-39 TurnEventKind（6 项）
type TurnEventKind string

const (
	// TurnEventKindStarted 轮次开始
	TurnEventKindStarted TurnEventKind = "started"
	// TurnEventKindPaused 轮次暂停（非终态）
	TurnEventKindPaused TurnEventKind = "paused"
	// TurnEventKindResumed 轮次恢复（非终态）
	TurnEventKindResumed TurnEventKind = "resumed"
	// TurnEventKindFinished 轮次正常完成（终态）
	TurnEventKindFinished TurnEventKind = "finished"
	// TurnEventKindAborted 轮次被中止（终态）
	TurnEventKindAborted TurnEventKind = "aborted"
	// TurnEventKindFailed 轮次失败（终态）
	TurnEventKindFailed TurnEventKind = "failed"
)

// OutputKind 输出内容的可移植表示。
// Python: events.py:51-55 OutputKind（2 项）
type OutputKind string

const (
	// OutputKindText 纯文本
	OutputKindText OutputKind = "text"
	// OutputKindStructured 结构化 JSON
	OutputKindStructured OutputKind = "structured"
)

// OutputChannel 承载输出块的语义通道。
// Python: events.py:58-63 OutputChannel（3 项）
type OutputChannel string

const (
	// OutputChannelAnswer 答复通道
	OutputChannelAnswer OutputChannel = "answer"
	// OutputChannelReasoning 推理通道
	OutputChannelReasoning OutputChannel = "reasoning"
	// OutputChannelSystem 系统通道
	OutputChannelSystem OutputChannel = "system"
)

// OutputOperation 一次输出更新如何改变所标识的内容块。
// Python: events.py:66-71 OutputOperation（3 项）
type OutputOperation string

const (
	// OutputOperationDelta 增量追加
	OutputOperationDelta OutputOperation = "delta"
	// OutputOperationSnapshot 全量快照
	OutputOperationSnapshot OutputOperation = "snapshot"
	// OutputOperationFinal 最终定稿
	OutputOperationFinal OutputOperation = "final"
)

// UsageUpdateMode 用量更新是增量还是累计。
// Python: events.py:74-78 UsageUpdateMode（2 项）
type UsageUpdateMode string

const (
	// UsageUpdateModeDelta 增量
	UsageUpdateModeDelta UsageUpdateMode = "delta"
	// UsageUpdateModeCumulative 累计
	UsageUpdateModeCumulative UsageUpdateMode = "cumulative"
)

// EventRetention 由事件载荷推导出的背压保留类别。
// Python: events.py:81-86 EventRetention（3 项）
type EventRetention string

const (
	// EventRetentionRequired 必需，永不丢弃
	EventRetentionRequired EventRetention = "required"
	// EventRetentionCoalescible 可合并，只保留最新
	EventRetentionCoalescible EventRetention = "coalescible"
	// EventRetentionBestEffort 尽力而为，可丢
	EventRetentionBestEffort EventRetention = "best_effort"
)

// EventOverflowPolicy 有界观测缓冲满时允许的行为。
// Python: events.py:89-94 EventOverflowPolicy（3 项）
type EventOverflowPolicy string

const (
	// EventOverflowPolicyBlock 阻塞等待
	EventOverflowPolicyBlock EventOverflowPolicy = "block"
	// EventOverflowPolicyCoalesceOrBlock 可合并则合并，否则阻塞
	EventOverflowPolicyCoalesceOrBlock EventOverflowPolicy = "coalesce_or_block"
	// EventOverflowPolicyDropBestEffortOrBlock 可丢尽力而为类，否则阻塞
	EventOverflowPolicyDropBestEffortOrBlock EventOverflowPolicy = "drop_best_effort_or_block"
)

// ItemEventKind provider item（如工具调用）的生命周期转换。
// Python: events.py:109-114 ItemEventKind（3 项）
type ItemEventKind string

const (
	// ItemEventKindStarted item 开始
	ItemEventKindStarted ItemEventKind = "started"
	// ItemEventKindUpdated item 更新
	ItemEventKindUpdated ItemEventKind = "updated"
	// ItemEventKindCompleted item 完成
	ItemEventKindCompleted ItemEventKind = "completed"
)

// HookEventPhase 钩子调用的可观测相位。
// Python: events.py:117-121 HookEventPhase（2 项）
type HookEventPhase string

const (
	// HookEventPhaseStarted 钩子开始
	HookEventPhaseStarted HookEventPhase = "started"
	// HookEventPhaseFinished 钩子结束
	HookEventPhaseFinished HookEventPhase = "finished"
)

// DiagnosticLevel 诊断事件的严重程度。
// Python: events.py:124-130 DiagnosticLevel（4 项）
type DiagnosticLevel string

const (
	// DiagnosticLevelDebug 调试
	DiagnosticLevelDebug DiagnosticLevel = "debug"
	// DiagnosticLevelInfo 信息
	DiagnosticLevelInfo DiagnosticLevel = "info"
	// DiagnosticLevelWarning 警告
	DiagnosticLevelWarning DiagnosticLevel = "warning"
	// DiagnosticLevelError 错误
	DiagnosticLevelError DiagnosticLevel = "error"
)

// ModelRequestStatus 轮内一次物理模型请求的终态。
// Python: events.py:133-138 ModelRequestStatus（3 项）
type ModelRequestStatus string

const (
	// ModelRequestStatusCompleted 完成
	ModelRequestStatusCompleted ModelRequestStatus = "completed"
	// ModelRequestStatusFailed 失败
	ModelRequestStatusFailed ModelRequestStatus = "failed"
	// ModelRequestStatusCancelled 取消
	ModelRequestStatusCancelled ModelRequestStatus = "cancelled"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// TerminalTurnEventKinds 终态轮次事件集合。
// Python: events.py:42-48 TERMINAL_TURN_EVENT_KINDS（frozenset，3 项）
// Go 无 frozenset，用 map 表示并约定只读；判成员请用 IsTerminalTurnEventKind。
var TerminalTurnEventKinds = map[TurnEventKind]bool{
	TurnEventKindFinished: true,
	TurnEventKindAborted:  true,
	TurnEventKindFailed:   true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsTerminalTurnEventKind 判定轮次事件是否为终态。
// Python: events.py:42-48（kind in TERMINAL_TURN_EVENT_KINDS）
func IsTerminalTurnEventKind(kind TurnEventKind) bool {
	return TerminalTurnEventKinds[kind]
}
