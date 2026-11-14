package rsi

import (
	"context"
	"encoding/json"
)

// events.go —— RSI 场景共享的引擎事件契约。
// Python: openjiuwen/rsi/events.py
//
// harness 与 artifact 引擎使用同一回调边界。快照类事件携带完整持久化数据，
// NodeStageEvent 是既有节点内部子过程流转的透传通知。

// ──────────────────────────── 结构体 ────────────────────────────

// EventStatus 已持久化的任务状态流转。
// Python: EventStatus
type EventStatus struct {
	status RsiStatus `json:"status"`
}

// NewEventStatus 构造状态事件。
func NewEventStatus(status RsiStatus) *EventStatus { return &EventStatus{status: status} }

// Status 新状态。
func (e *EventStatus) Status() RsiStatus { return e.status }

// EventType 恒定事件名。Python: event_type = "status"
func (e *EventStatus) EventType() string { return "status" }

// EventProgress 已持久化的累计进度快照。
// Python: EventProgress
type EventProgress struct {
	iteration       int       `json:"iteration"`
	totalIterations int       `json:"total_iterations"`
	score           *float64  `json:"score"`
	baseline        *float64  `json:"baseline"`
	usage           *RsiUsage `json:"usage"`
}

// NewEventProgress 构造进度事件。
func NewEventProgress(iteration, totalIterations int, score, baseline *float64, usage *RsiUsage) *EventProgress {
	return &EventProgress{iteration: iteration, totalIterations: totalIterations, score: score, baseline: baseline, usage: usage}
}

// Iteration 当前迭代代号。
func (e *EventProgress) Iteration() int { return e.iteration }

// TotalIterations 总迭代次数。
func (e *EventProgress) TotalIterations() int { return e.totalIterations }

// Score 当前分数（可为 nil）。
func (e *EventProgress) Score() *float64 { return e.score }

// Baseline 基线分数（可为 nil）。
func (e *EventProgress) Baseline() *float64 { return e.baseline }

// Usage 累计用量（可为 nil）。
func (e *EventProgress) Usage() *RsiUsage { return e.usage }

// EventType 恒定事件名。Python: event_type = "progress"
func (e *EventProgress) EventType() string { return "progress" }

// EventNode 已持久化的完整树节点快照。
// Python: EventNode；artifacts 缺省空列表（Python default_factory=list）。
type EventNode struct {
	node      *RsiTreeNode        `json:"node"`
	artifacts []map[string]string `json:"artifacts"`
}

// NewEventNode 构造节点事件；artifacts 传 nil 时取空列表。
func NewEventNode(node *RsiTreeNode, artifacts []map[string]string) *EventNode {
	if artifacts == nil {
		artifacts = []map[string]string{}
	}
	return &EventNode{node: node, artifacts: artifacts}
}

// Node 节点快照。
func (e *EventNode) Node() *RsiTreeNode { return e.node }

// Artifacts 节点 artifact 摘要列表。
func (e *EventNode) Artifacts() []map[string]string { return e.artifacts }

// EventType 恒定事件名。Python: event_type = "node"
func (e *EventNode) EventType() string { return "node" }

// EventUsage 单笔原始用量增量（非累计进度快照）。
// Python: EventUsage——call_id 在本地账本中保持稳定供服务端去重；
// 既有事件变体的线上形态保持不变。
type EventUsage struct {
	eventID   int           `json:"event_id"`
	taskID    string        `json:"task_id"`
	ts        string        `json:"ts"`
	callID    string        `json:"call_id"`
	modelCall *RsiModelCall `json:"model_call"`
	nodeRef   *string       `json:"node_ref"`
	stageRef  *string       `json:"stage_ref"`
}

// NewEventUsage 构造用量事件。
func NewEventUsage(eventID int, taskID, ts, callID string, modelCall *RsiModelCall, nodeRef, stageRef *string) *EventUsage {
	return &EventUsage{eventID: eventID, taskID: taskID, ts: ts, callID: callID, modelCall: modelCall, nodeRef: nodeRef, stageRef: stageRef}
}

// EventID 事件序号。
func (e *EventUsage) EventID() int { return e.eventID }

// TaskID 任务标识。
func (e *EventUsage) TaskID() string { return e.taskID }

// Ts 事件时间戳。
func (e *EventUsage) Ts() string { return e.ts }

// CallID 调用标识（账本去重键）。
func (e *EventUsage) CallID() string { return e.callID }

// ModelCall 模型调用记录。
func (e *EventUsage) ModelCall() *RsiModelCall { return e.modelCall }

// NodeRef 归属节点（可为 nil）。
func (e *EventUsage) NodeRef() *string { return e.nodeRef }

// StageRef 归属阶段（可为 nil）。
func (e *EventUsage) StageRef() *string { return e.stageRef }

// Family 恒定值。Python: family = "progress"
func (e *EventUsage) Family() string { return "progress" }

// Kind 恒定值。Python: kind = "usage"
func (e *EventUsage) Kind() string { return "usage" }

// EventType 恒定事件名。Python: event_type = "progress.usage"
func (e *EventUsage) EventType() string { return "progress.usage" }

// MarshalJSON 以 Python asdict 键名序列化（账本逐行落盘形态）。
func (e *EventUsage) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		EventID   int           `json:"event_id"`
		TaskID    string        `json:"task_id"`
		Ts        string        `json:"ts"`
		CallID    string        `json:"call_id"`
		ModelCall *RsiModelCall `json:"model_call"`
		NodeRef   *string       `json:"node_ref"`
		StageRef  *string       `json:"stage_ref"`
		Family    string        `json:"family"`
		Kind      string        `json:"kind"`
		EventType string        `json:"event_type"`
	}{
		e.eventID, e.taskID, e.ts, e.callID, e.modelCall, e.nodeRef, e.stageRef,
		"progress", "usage", "progress.usage",
	})
}

// NodeStageEvent 既有树节点的子过程流转事件。
// Python: NodeStageEvent——stage 是有意保留的 JSON 透传对象：阶段分类由
// 引擎持有，AgentServer 只转发不解释其取值。
type NodeStageEvent struct {
	nodeRef string         `json:"node_ref"`
	stage   map[string]any `json:"stage"`
	note    *string        `json:"note"`
}

// NewNodeStageEvent 构造节点阶段事件。
func NewNodeStageEvent(nodeRef string, stage map[string]any, note *string) *NodeStageEvent {
	return &NodeStageEvent{nodeRef: nodeRef, stage: stage, note: note}
}

// NodeRef 归属节点标识。
func (e *NodeStageEvent) NodeRef() string { return e.nodeRef }

// Stage 阶段透传对象（引擎自有分类，AgentServer 不解释）。
func (e *NodeStageEvent) Stage() map[string]any { return e.stage }

// Note 备注（可为 nil）。
func (e *NodeStageEvent) Note() *string { return e.note }

// EventType 恒定事件名。Python: event_type = "node.stage"
func (e *NodeStageEvent) EventType() string { return "node.stage" }

// ──────────────────────────── 类型别名 ────────────────────────────

// EventNodeStage 保留旧 artifact 事件名给调用方使用；规范名随端到端事件契约。
// Python: EventNodeStage: TypeAlias = NodeStageEvent
type EventNodeStage = *NodeStageEvent

// ──────────────────────────── 导出函数 ────────────────────────────

// EngineEvent RSI 引擎事件联合类型。
// Python: EngineEvent = EventStatus | EventProgress | EventNode | NodeStageEvent | EventUsage
type EngineEvent interface {
	// EventType 恒定事件名（对齐 Python dataclass 的 event_type 字段）。
	EventType() string
}

// OnEvent 事件回调。
// Python: Callable[[EngineEvent], Awaitable[None]]——回调异常有意不吞：
// 观察通道由 AgentServer 持有，由其决定如何记录或补偿投递失败。
type OnEvent func(ctx context.Context, event EngineEvent) error

// Emit 发出事件：回调为 nil 时跳过；回调错误原样透传不吞。
// Python: events.py emit()——引擎须在事件引用的 state/node/report/artifact
// 均已持久化之后才调用本函数。
func Emit(ctx context.Context, onEvent OnEvent, event EngineEvent) error {
	if onEvent == nil {
		return nil
	}
	return onEvent(ctx, event)
}
