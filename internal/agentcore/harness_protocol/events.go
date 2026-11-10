package harness_protocol

// 本文件对应 Python openjiuwen/harness_protocol/events.py（456 行）：
// provider-neutral 的有序观测事件信封与其载荷。
// 本文件只定义契约与构造期校验，不含任何缓冲/游标行为
// （有界缓冲与单消费者游标在 15.5 的 provider 基座落地，Python 依据
// harness_providers/stream.py:19-155）。

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessEventPayload 一条观测事件的载荷标记接口。
// Python: events.py:364-375 HarnessEventPayload（TypeAlias，10 分支 union）
// Go 用接口 + 10 个具体结构体表达；分派一律走 type switch，
// 顺序对齐 serialization.py:_payload_to_wire 的 isinstance 链。
type HarnessEventPayload interface {
	// payloadKind 返回线格式 discriminator（供 15.4 codec 使用）
	payloadKind() string
}

// OutputEvent 一个稳定输出内容块的 provider-neutral 更新。
// Python: events.py:141-160 OutputEvent
type OutputEvent struct {
	// OutputID 稳定的输出块标识
	OutputID string
	// Kind 内容表示（text / structured）
	Kind OutputKind
	// Content 内容（已冻结）
	Content JsonValue
	// Operation 更新方式（delta / snapshot / final）
	Operation OutputOperation
	// Channel 语义通道（answer / reasoning / system）
	Channel OutputChannel
	// ContentIndex 同一块内的内容序号
	ContentIndex int
	// ContentType MIME 提示（空串表示未提供）
	ContentType string
	// Data 附加数据（已冻结）
	Data JsonObject
}

// ItemLifecycleEvent provider item（如工具调用）的生命周期更新。
// Python: events.py:163-174 ItemLifecycleEvent
type ItemLifecycleEvent struct {
	// Kind item 生命周期转换
	Kind ItemEventKind
	// ItemType item 类别（"step" / "tool" / "command" / "file_change" / "subagent" …）
	ItemType string
	// Data item 的原生形状（已冻结）
	Data JsonObject
}

// UsageUpdatedEvent 累计或增量的归一化用量信息。
// Python: events.py:177-182 UsageUpdatedEvent
type UsageUpdatedEvent struct {
	// Usage 归一化 token 用量
	Usage TurnUsage
	// Mode 更新模式（delta / cumulative）
	Mode UsageUpdateMode
}

// HookObservedEvent 钩子执行的观测镜像，绝不承载控制应答。
// Python: events.py:306-318 HookObservedEvent
type HookObservedEvent struct {
	// Phase 钩子相位
	Phase HookEventPhase
	// HookName 钩子名
	HookName string
	// HookID 钩子标识
	HookID string
	// Data 附加数据（已冻结）
	Data JsonObject
}

// StateChangedEvent 一次高层 harness 状态转换。
// Python: events.py:273-278 StateChangedEvent
type StateChangedEvent struct {
	// Old 转换前状态
	Old HarnessState
	// New 转换后状态
	New HarnessState
}

// ProviderEvent 保留 provider 扩展而不改核心的命名空间事件。
// Python: events.py:335-347 ProviderEvent
type ProviderEvent struct {
	// Provider provider 名
	Provider string
	// EventType provider 自有事件类型
	EventType string
	// SchemaVersion provider 自有 schema 版本
	SchemaVersion string
	// Payload 扩展载荷（已冻结）
	Payload JsonObject
}

// DiagnosticEvent harness 实现发出的非输出诊断。
// Python: events.py:321-332 DiagnosticEvent
type DiagnosticEvent struct {
	// Level 严重程度
	Level DiagnosticLevel
	// Message 诊断文案
	Message string
	// Data 附加数据（已冻结）
	Data JsonObject
}

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

// NewOutputEvent 创建输出更新事件。
// Python: events.py:154-160 OutputEvent.__post_init__
func NewOutputEvent(
	outputID string,
	kind OutputKind,
	content JsonValue,
	operation OutputOperation,
	channel OutputChannel,
	contentIndex int,
	contentType string,
	data JsonObject,
) (OutputEvent, error) {
	// 步骤 1：output_id 非空
	if outputID == "" {
		return OutputEvent{}, &HarnessProtocolError{Msg: "output_id must not be empty"}
	}
	// 步骤 2：content_index 非负
	if contentIndex < 0 {
		return OutputEvent{}, &HarnessProtocolError{Msg: "output content_index must be non-negative"}
	}
	// 步骤 3：冻结 content
	frozenContent, err := FreezeJsonValue(content)
	if err != nil {
		return OutputEvent{}, err
	}
	// 步骤 4：冻结 data
	frozenData, err := FreezeJsonObject(data)
	if err != nil {
		return OutputEvent{}, err
	}
	return OutputEvent{
		OutputID:     outputID,
		Kind:         kind,
		Content:      frozenContent,
		Operation:    operation,
		Channel:      channel,
		ContentIndex: contentIndex,
		ContentType:  contentType,
		Data:         frozenData,
	}, nil
}

// NewItemLifecycleEvent 创建 item 生命周期事件。
// Python: events.py:171-174 ItemLifecycleEvent.__post_init__
func NewItemLifecycleEvent(kind ItemEventKind, itemType string, data JsonObject) (ItemLifecycleEvent, error) {
	// 步骤 1：item_type 非空
	if itemType == "" {
		return ItemLifecycleEvent{}, &HarnessProtocolError{Msg: "item_type must not be empty"}
	}
	// 步骤 2：冻结 data
	frozenData, err := FreezeJsonObject(data)
	if err != nil {
		return ItemLifecycleEvent{}, err
	}
	return ItemLifecycleEvent{Kind: kind, ItemType: itemType, Data: frozenData}, nil
}

// NewUsageUpdatedEvent 创建用量更新事件（上游无 __post_init__）。
// Python: events.py:177-182
func NewUsageUpdatedEvent(usage TurnUsage, mode UsageUpdateMode) (UsageUpdatedEvent, error) {
	return UsageUpdatedEvent{Usage: usage, Mode: mode}, nil
}

// NewHookObservedEvent 创建钩子观测事件。
// Python: events.py:315-318 HookObservedEvent.__post_init__
func NewHookObservedEvent(phase HookEventPhase, hookName, hookID string, data JsonObject) (HookObservedEvent, error) {
	// 步骤 1：钩子名与 id 均非空
	if hookName == "" || hookID == "" {
		return HookObservedEvent{}, &HarnessProtocolError{Msg: "hook observation name and id must not be empty"}
	}
	// 步骤 2：冻结 data
	frozenData, err := FreezeJsonObject(data)
	if err != nil {
		return HookObservedEvent{}, err
	}
	return HookObservedEvent{Phase: phase, HookName: hookName, HookID: hookID, Data: frozenData}, nil
}

// NewStateChangedEvent 创建状态转换事件（上游无 __post_init__）。
// Python: events.py:273-278
func NewStateChangedEvent(old, newState HarnessState) (StateChangedEvent, error) {
	return StateChangedEvent{Old: old, New: newState}, nil
}

// NewProviderEvent 创建 provider 扩展事件。
// Python: events.py:344-347 ProviderEvent.__post_init__
func NewProviderEvent(provider, eventType, schemaVersion string, payload JsonObject) (ProviderEvent, error) {
	// 步骤 1：三个标识均非空
	if provider == "" || eventType == "" || schemaVersion == "" {
		return ProviderEvent{}, &HarnessProtocolError{
			Msg: "provider event namespace, type, and schema version must not be empty",
		}
	}
	// 步骤 2：冻结 payload
	frozenPayload, err := FreezeJsonObject(payload)
	if err != nil {
		return ProviderEvent{}, err
	}
	return ProviderEvent{
		Provider:      provider,
		EventType:     eventType,
		SchemaVersion: schemaVersion,
		Payload:       frozenPayload,
	}, nil
}

// NewDiagnosticEvent 创建诊断事件。
// Python: events.py:329-332 DiagnosticEvent.__post_init__
func NewDiagnosticEvent(level DiagnosticLevel, message string, data JsonObject) (DiagnosticEvent, error) {
	// 步骤 1：message 非空
	if message == "" {
		return DiagnosticEvent{}, &HarnessProtocolError{Msg: "diagnostic message must not be empty"}
	}
	// 步骤 2：冻结 data
	frozenData, err := FreezeJsonObject(data)
	if err != nil {
		return DiagnosticEvent{}, err
	}
	return DiagnosticEvent{Level: level, Message: message, Data: frozenData}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// payloadKind 返回线格式 discriminator。
// Python: serialization.py:278-292（isinstance(event, OutputEvent) → "output"）
func (OutputEvent) payloadKind() string { return "output" }

// payloadKind 返回线格式 discriminator。
// Python: serialization.py:293-302（→ "item_lifecycle"）
func (ItemLifecycleEvent) payloadKind() string { return "item_lifecycle" }

// payloadKind 返回线格式 discriminator。
// Python: serialization.py:314-315（→ "state_changed"）
func (StateChangedEvent) payloadKind() string { return "state_changed" }

// payloadKind 返回线格式 discriminator。
// Python: serialization.py:346-356（→ "provider"）
func (ProviderEvent) payloadKind() string { return "provider" }

// payloadKind 返回线格式 discriminator。
// Python: serialization.py:336-345（→ "diagnostic"）
func (DiagnosticEvent) payloadKind() string { return "diagnostic" }

// payloadKind 返回线格式 discriminator。
// Python: serialization.py:303-311（→ "usage_updated"）
func (UsageUpdatedEvent) payloadKind() string { return "usage_updated" }

// payloadKind 返回线格式 discriminator。
// Python: serialization.py:325-335（→ "hook_observed"）
func (HookObservedEvent) payloadKind() string { return "hook_observed" }
