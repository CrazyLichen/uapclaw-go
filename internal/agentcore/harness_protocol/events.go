package harness_protocol

import "math"

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

// ModelRequestEvent 轮内观测到的一次物理模型请求。
// 只有当宿主声明 HostCapabilityModelRequestObservation 时才发出；
// 事件在该请求结束后发出一次，且必定先于它引发的任何 item 与该轮终态事件。
// Python: events.py:185-271 ModelRequestEvent（19 字段）
type ModelRequestEvent struct {
	// RequestID 会话内 provider 稳定的请求标识
	RequestID string
	// Status 请求终态
	Status ModelRequestStatus
	// StartedAt 请求发出时的 Unix 秒
	StartedAt float64
	// EndedAt 响应完成时的 Unix 秒
	EndedAt float64
	// Model 服务该请求的模型（空串表示未提供）
	Model string
	// ProviderName 服务该请求的厂商（空串表示未提供）
	ProviderName string
	// SystemInstructions 对话之外单独提供的指令
	SystemInstructions []ContentBlock
	// InputMessages 实际随请求发送的对话，按序
	InputMessages []TurnMessage
	// InputObserved 是否从请求本身观测到输入侧
	InputObserved bool
	// OutputMessage 该请求产出的助手消息
	OutputMessage *TurnMessage
	// ToolDefinitions 提供给模型的工具（已冻结）
	ToolDefinitions JsonValue
	// RequestParameters 请求携带的采样参数（已冻结）
	RequestParameters JsonObject
	// ResponseID provider 给出的响应标识（空串表示未提供）
	ResponseID string
	// TimeToFirstChunk 从发出到首个流式块之间的秒数
	TimeToFirstChunk *float64
	// FinishReasons 厂商口径的生成停止原因
	FinishReasons []string
	// Usage 仅本次请求的 token 用量
	Usage *TurnUsage
	// Cost 本次请求的精确金额
	Cost *MonetaryAmount
	// Error status 非 completed 时的归一化失败
	Error *TurnError
	// Data 命名空间化的 provider 诊断数据（已冻结）
	Data JsonObject
}

// ModelRequestOptions ModelRequestEvent 的构造入参。
// Python: events.py:232-250（dataclass 字段与默认值）
type ModelRequestOptions struct {
	// RequestID 请求标识
	RequestID string
	// Status 请求终态
	Status ModelRequestStatus
	// StartedAt 发出时间
	StartedAt float64
	// EndedAt 完成时间
	EndedAt float64
	// Model 模型名
	Model string
	// ProviderName 厂商名
	ProviderName string
	// SystemInstructions 系统指令
	SystemInstructions []ContentBlock
	// InputMessages 输入对话
	InputMessages []TurnMessage
	// InputObserved 输入是否可观测
	InputObserved bool
	// OutputMessage 输出消息
	OutputMessage *TurnMessage
	// ToolDefinitions 工具定义
	ToolDefinitions JsonValue
	// RequestParameters 采样参数
	RequestParameters JsonObject
	// ResponseID 响应标识
	ResponseID string
	// TimeToFirstChunk 首块延迟
	TimeToFirstChunk *float64
	// FinishReasons 停止原因
	FinishReasons []string
	// Usage 本次用量
	Usage *TurnUsage
	// Cost 本次金额
	Cost *MonetaryAmount
	// Error 失败信息
	Error *TurnError
	// Data 诊断数据
	Data JsonObject
}

// UnknownEvent 本消费者不认识的新 schema 共享事件，解码时无损保留。
// Python: events.py:350-361 UnknownEvent
// 注：它的线格式 event_type 取自自身 EventType（serialization.py:357-358），
// 15.4 codec 必须特判 *UnknownEvent，不得依赖 payloadKind()。
type UnknownEvent struct {
	// EventType 未知事件类型
	EventType string
	// SchemaVersion 该事件的 schema 版本
	SchemaVersion string
	// Payload 原始载荷（已冻结）
	Payload JsonObject
}

// EventBufferConfig 实现方声明的有界观测缓冲契约。
// Python: events.py:97-106 EventBufferConfig
type EventBufferConfig struct {
	// Capacity 缓冲容量（上游默认 1024）
	Capacity int
	// Overflow 缓冲满时的策略（上游默认 coalesce_or_block）
	Overflow EventOverflowPolicy
}

// HarnessEvent 携带共享关联元数据的有序事件信封。
// Python: events.py:392-426 HarnessEvent（10 字段）
type HarnessEvent struct {
	// Sequence 一个 start/stop 周期内严格递增的序号
	Sequence int
	// Timestamp 观测时刻的 Unix 秒
	Timestamp float64
	// Event 载荷
	Event HarnessEventPayload
	// HostSessionID 宿主会话标识
	HostSessionID string
	// AgentID agent 标识
	AgentID string
	// ProviderSessionID provider 原生会话标识（空串表示未提供）
	ProviderSessionID string
	// TurnID 所属轮次（空串表示不属于某轮）
	TurnID string
	// ItemID 所属 provider item（空串表示未提供）
	ItemID string
	// CorrelationID 聚合同一逻辑 trace（空串表示未提供）
	CorrelationID string
	// CausationIDs 实际触发本事件的消息/请求
	CausationIDs []string
}

// HarnessEventOptions HarnessEvent 的构造入参。
// Python: events.py:396-405（dataclass 字段与默认值）
type HarnessEventOptions struct {
	// Sequence 序号
	Sequence int
	// Timestamp 观测时刻
	Timestamp float64
	// Event 载荷
	Event HarnessEventPayload
	// HostSessionID 宿主会话
	HostSessionID string
	// AgentID agent 标识
	AgentID string
	// ProviderSessionID provider 会话（空串 = 未提供）
	ProviderSessionID string
	// TurnID 所属轮次（空串 = 未提供）
	TurnID string
	// ItemID item 标识（空串 = 未提供）
	ItemID string
	// CorrelationID trace 聚合（空串 = 未提供）
	CorrelationID string
	// CausationIDs 因果 ID
	CausationIDs []string
}

// TurnLifecycleEvent 一轮的生命周期转换。
// Python: events.py:281-303 TurnLifecycleEvent
type TurnLifecycleEvent struct {
	// Kind 轮次生命周期转换
	Kind TurnEventKind
	// Result 终态结果（非终态必须为 nil）
	Result *TurnResult
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

// EventRetentionOf 返回生产者对 payload 唯一可用的保留类别。
// 这是背压保留档位的唯一来源：生产者不得自行选择（SPEC.md 不变量 12）。
// Python: events.py:378-389 event_retention（5 条 if 短路，顺序不可调）
func EventRetentionOf(payload HarnessEventPayload) EventRetention {
	// 规则 1：快照输出可合并（events.py:381-382）
	if out, ok := payload.(*OutputEvent); ok && out.Operation == OutputOperationSnapshot {
		return EventRetentionCoalescible
	}
	// 规则 2：累计用量可合并（events.py:383-384）
	if usage, ok := payload.(*UsageUpdatedEvent); ok && usage.Mode == UsageUpdateModeCumulative {
		return EventRetentionCoalescible
	}
	// 规则 3：钩子观测只是镜像，可丢（events.py:385-386）
	if _, ok := payload.(*HookObservedEvent); ok {
		return EventRetentionBestEffort
	}
	// 规则 4：debug/info 级诊断可丢（events.py:387-388）
	if diag, ok := payload.(*DiagnosticEvent); ok &&
		(diag.Level == DiagnosticLevelDebug || diag.Level == DiagnosticLevelInfo) {
		return EventRetentionBestEffort
	}
	// 规则 5：其余一律必需，永不丢弃（events.py:389）
	return EventRetentionRequired
}

// NewEventBufferConfig 创建有界观测缓冲契约。
// Python: events.py:104-106 EventBufferConfig.__post_init__
func NewEventBufferConfig(capacity int, overflow EventOverflowPolicy) (EventBufferConfig, error) {
	// 步骤 1：容量必须为正
	if capacity <= 0 {
		return EventBufferConfig{}, &HarnessProtocolError{Msg: "event buffer capacity must be positive"}
	}
	return EventBufferConfig{Capacity: capacity, Overflow: overflow}, nil
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

// NewModelRequestEvent 创建物理模型请求事件。
// Python: events.py:252-270 ModelRequestEvent.__post_init__
func NewModelRequestEvent(opts ModelRequestOptions) (ModelRequestEvent, error) {
	// 步骤 1：request_id 非空
	if opts.RequestID == "" {
		return ModelRequestEvent{}, &HarnessProtocolError{Msg: "model request_id must not be empty"}
	}
	// 步骤 2：两个时间戳有限且非负（顺序 started_at → ended_at）
	for _, ts := range []struct {
		name  string
		value float64
	}{{"started_at", opts.StartedAt}, {"ended_at", opts.EndedAt}} {
		if math.IsNaN(ts.value) || math.IsInf(ts.value, 0) || ts.value < 0 {
			return ModelRequestEvent{}, &HarnessProtocolError{
				Msg: "model request " + ts.name + " must be a finite Unix timestamp",
			}
		}
	}
	// 步骤 3：ended_at 不得早于 started_at
	if opts.EndedAt < opts.StartedAt {
		return ModelRequestEvent{}, &HarnessProtocolError{Msg: "model request ended_at must not precede started_at"}
	}
	// 步骤 4：completed 不得带 error
	if opts.Status == ModelRequestStatusCompleted && opts.Error != nil {
		return ModelRequestEvent{}, &HarnessProtocolError{Msg: "completed model request must not contain error"}
	}
	// 步骤 5：time_to_first_chunk 非负有限
	if opts.TimeToFirstChunk != nil {
		v := *opts.TimeToFirstChunk
		if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
			return ModelRequestEvent{}, &HarnessProtocolError{
				Msg: "model request time_to_first_chunk must be a non-negative duration",
			}
		}
	}
	// 步骤 6：两处切片拷贝为独立切片（Go 取代 Python 的 tuple(t)）
	systemInstructions := append([]ContentBlock(nil), opts.SystemInstructions...)
	inputMessages := append([]TurnMessage(nil), opts.InputMessages...)
	// 步骤 7a：冻结 tool_definitions
	frozenToolDefs, err := FreezeJsonValue(opts.ToolDefinitions)
	if err != nil {
		return ModelRequestEvent{}, err
	}
	// 步骤 7b：冻结 request_parameters
	frozenParams, err := FreezeJsonObject(opts.RequestParameters)
	if err != nil {
		return ModelRequestEvent{}, err
	}
	// 步骤 7c：finish_reasons 拷贝
	finishReasons := append([]string(nil), opts.FinishReasons...)
	// 步骤 7d：冻结 data
	frozenData, err := FreezeJsonObject(opts.Data)
	if err != nil {
		return ModelRequestEvent{}, err
	}
	return ModelRequestEvent{
		RequestID:          opts.RequestID,
		Status:             opts.Status,
		StartedAt:          opts.StartedAt,
		EndedAt:            opts.EndedAt,
		Model:              opts.Model,
		ProviderName:       opts.ProviderName,
		SystemInstructions: systemInstructions,
		InputMessages:      inputMessages,
		InputObserved:      opts.InputObserved,
		OutputMessage:      opts.OutputMessage,
		ToolDefinitions:    frozenToolDefs,
		RequestParameters:  frozenParams,
		ResponseID:         opts.ResponseID,
		TimeToFirstChunk:   opts.TimeToFirstChunk,
		FinishReasons:      finishReasons,
		Usage:              opts.Usage,
		Cost:               opts.Cost,
		Error:              opts.Error,
		Data:               frozenData,
	}, nil
}

// NewUnknownEvent 创建未知事件（解码兜底用）。
// Python: events.py:358-361 UnknownEvent.__post_init__
func NewUnknownEvent(eventType, schemaVersion string, payload JsonObject) (UnknownEvent, error) {
	// 步骤 1：event_type 与 schema_version 均非空
	if eventType == "" || schemaVersion == "" {
		return UnknownEvent{}, &HarnessProtocolError{
			Msg: "unknown event type and schema version must not be empty",
		}
	}
	// 步骤 2：冻结 payload
	frozenPayload, err := FreezeJsonObject(payload)
	if err != nil {
		return UnknownEvent{}, err
	}
	return UnknownEvent{EventType: eventType, SchemaVersion: schemaVersion, Payload: frozenPayload}, nil
}

// NewHarnessEvent 创建事件信封。
// Python: events.py:407-426 HarnessEvent.__post_init__（9 步）
func NewHarnessEvent(opts HarnessEventOptions) (HarnessEvent, error) {
	// 步骤 1：sequence 非负
	if opts.Sequence < 0 {
		return HarnessEvent{}, &HarnessProtocolError{Msg: "event sequence must be non-negative"}
	}
	// 步骤 2：timestamp 有限且非负
	if math.IsNaN(opts.Timestamp) || math.IsInf(opts.Timestamp, 0) || opts.Timestamp < 0 {
		return HarnessEvent{}, &HarnessProtocolError{Msg: "event timestamp must be a finite Unix timestamp"}
	}
	// 步骤 3：host_session_id 与 agent_id 非空
	if opts.HostSessionID == "" || opts.AgentID == "" {
		return HarnessEvent{}, &HarnessProtocolError{Msg: "event host_session_id and agent_id must not be empty"}
	}
	// 步骤 4：4 个 optional ID 一律用 string 表达，空串即"未提供"——
	// 用户 2026-11-10 Q4 决策：Python 的 str|None 区分 None 与空串非法，
	// Go 统一为 string 后空串 = 未提供，不存在"有值但为空"的中间态。
	// Python: events.py:414-416（any(identifier == "" ...) 拒绝）在 Go 恒不成立。
	// 步骤 5：causation_ids 拷贝并逐项非空
	causationIDs := append([]string(nil), opts.CausationIDs...)
	for _, id := range causationIDs {
		if id == "" {
			return HarnessEvent{}, &HarnessProtocolError{Msg: "event causation_ids must not contain empty values"}
		}
	}
	// 步骤 6：causation_ids 不得重复
	seen := make(map[string]struct{}, len(causationIDs))
	for _, id := range causationIDs {
		if _, dup := seen[id]; dup {
			return HarnessEvent{}, &HarnessProtocolError{Msg: "event causation_ids must not contain duplicates"}
		}
		seen[id] = struct{}{}
	}
	// 步骤 7：载荷不得为 nil。
	// 注：上游不显式校验此项（dataclass 类型注解已排除 None）。
	// Go 侧 nil 接口会让下游 type switch 静默走 default，故显式拒绝。
	// Python 侧无对应实现（Go 专有防御）。
	if opts.Event == nil {
		return HarnessEvent{}, &HarnessProtocolError{Msg: "event payload must not be nil"}
	}
	// 步骤 8：轮次生命周期事件必须有 turn_id
	if _, ok := opts.Event.(*TurnLifecycleEvent); ok && opts.TurnID == "" {
		return HarnessEvent{}, &HarnessProtocolError{Msg: "turn lifecycle event requires turn_id"}
	}
	// 步骤 9：模型请求事件必须有 turn_id
	if _, ok := opts.Event.(*ModelRequestEvent); ok && opts.TurnID == "" {
		return HarnessEvent{}, &HarnessProtocolError{Msg: "model request event requires turn_id"}
	}
	return HarnessEvent{
		Sequence:          opts.Sequence,
		Timestamp:         opts.Timestamp,
		Event:             opts.Event,
		HostSessionID:     opts.HostSessionID,
		AgentID:           opts.AgentID,
		ProviderSessionID: opts.ProviderSessionID,
		TurnID:            opts.TurnID,
		ItemID:            opts.ItemID,
		CorrelationID:     opts.CorrelationID,
		CausationIDs:      causationIDs,
	}, nil
}

// NewTurnLifecycleEvent 创建轮次生命周期事件。
// Python: events.py:288-303 TurnLifecycleEvent.__post_init__
func NewTurnLifecycleEvent(kind TurnEventKind, result *TurnResult) (TurnLifecycleEvent, error) {
	// 分支 A（非终态）：events.py:289-292
	if !IsTerminalTurnEventKind(kind) {
		if result != nil {
			return TurnLifecycleEvent{}, &HarnessProtocolError{
				Msg: "non-terminal " + string(kind) + " turn event must not contain a result",
			}
		}
		return TurnLifecycleEvent{Kind: kind}, nil
	}
	// 分支 B：终态必须带 result（events.py:294-295）
	if result == nil {
		return TurnLifecycleEvent{}, &HarnessProtocolError{Msg: "terminal turn event requires a result"}
	}
	// 分支 C：result.status 必须与 kind 匹配（events.py:297-303）
	expected := map[TurnEventKind]TurnStatus{
		TurnEventKindFinished: TurnStatusCompleted,
		TurnEventKindAborted:  TurnStatusInterrupted,
		TurnEventKindFailed:   TurnStatusFailed,
	}[kind]
	if result.Status != expected {
		return TurnLifecycleEvent{}, &HarnessProtocolError{
			Msg: string(kind) + " turn event requires " + string(expected) + " result",
		}
	}
	return TurnLifecycleEvent{Kind: kind, Result: result}, nil
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

// payloadKind 返回线格式 discriminator。
// Python: serialization.py:316-324（→ "turn_lifecycle"）
func (TurnLifecycleEvent) payloadKind() string { return "turn_lifecycle" }

// payloadKind 返回线格式 discriminator。
// Python: serialization.py:312-313（→ "model_request"）
func (ModelRequestEvent) payloadKind() string { return "model_request" }

// payloadKind 返回内部标记（该类型不进 wire discriminator 映射，
// 15.4 codec 必须特判 *UnknownEvent 取自身 EventType）。
// Python: serialization.py:357-358
func (UnknownEvent) payloadKind() string { return "unknown" }
