package rsi

// 引擎事件模型（内部 v3 §3.3 四子类 + 公共信封）。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/events.py。
//
// 信封：event_id（任务内调试序号）/ task_id / ts / family / kind；
// 四子类：progress.metric / progress.usage / node.created / node.stage。
// 字段级解析收口在本文件；消费方只消费规范化字段。

// ──────────────────────────── 全局变量 ────────────────────────────

// eventKindByFamily 合法 family → kind 集合（对齐 Python _EVENT_KIND_BY_FAMILY）。
var eventKindByFamily = map[string]map[string]bool{
	"progress": {"metric": true, "usage": true},
	"node":     {"created": true, "stage": true},
}

// ──────────────────────────── 结构体 ────────────────────────────

// EngineEvent 公共信封 + 规范化载荷。
type EngineEvent struct {
	// Family 事件族（progress / node）。
	Family string
	// Kind 事件类（metric / usage / created / stage）。
	Kind string
	// TaskID 任务标识。
	TaskID string
	// EventID 任务内调试序号（可空）。
	EventID *int
	// Ts 事件时间戳。
	Ts string
	// Payload 规范化载荷。
	Payload map[string]any
}

// IsProgressMetric progress.metric 判定。
func (e EngineEvent) IsProgressMetric() bool { return e.Family == "progress" && e.Kind == "metric" }

// IsProgressUsage progress.usage 判定。
func (e EngineEvent) IsProgressUsage() bool { return e.Family == "progress" && e.Kind == "usage" }

// IsNodeCreated node.created 判定。
func (e EngineEvent) IsNodeCreated() bool { return e.Family == "node" && e.Kind == "created" }

// IsNodeStage node.stage 判定。
func (e EngineEvent) IsNodeStage() bool { return e.Family == "node" && e.Kind == "stage" }

// ToDict 对齐 Python EngineEvent.to_dict。
func (e EngineEvent) ToDict() map[string]any {
	payload := map[string]any{}
	for k, v := range e.Payload {
		payload[k] = v
	}
	return map[string]any{
		"event_id": nullableAny(e.EventID),
		"task_id":  e.TaskID,
		"ts":       e.Ts,
		"family":   e.Family,
		"kind":     e.Kind,
		"payload":  payload,
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseEngineEvent 把引擎侧事件 dict 规范化为 EngineEvent。
// 兼容两种形态（对齐 Python parse_engine_event）：
//  1. 引擎未来事件化：信封字段在顶层，载荷内联或 payload；
//  2. 内部已规范化事件：直接透传。
func ParseEngineEvent(raw map[string]any, defaultTaskID string) *EngineEvent {
	family := valueToString(raw["family"])
	kind := valueToString(raw["kind"])
	if family != "" && kind != "" {
		kindMap := eventKindByFamily[family]
		if kindMap == nil || !kindMap[kind] {
			return nil
		}
		payload := map[string]any{}
		if p, ok := raw["payload"].(map[string]any); ok {
			for k, v := range p {
				payload[k] = v
			}
		}
		if len(payload) == 0 {
			// 顶层扁平载荷 → 收口到 payload（agent-core 事件化常见形态）。
			for _, key := range []string{"iteration", "score", "baseline", "node_ref", "model_call", "node", "artifacts", "stage"} {
				if v, ok := raw[key]; ok {
					payload[key] = v
				}
			}
		}
		event := &EngineEvent{
			Family:  family,
			Kind:    kind,
			TaskID:  firstNonEmpty(valueToString(raw["task_id"]), defaultTaskID),
			Ts:      valueToString(raw["ts"]),
			Payload: payload,
		}
		if raw["event_id"] != nil {
			id := safeInt(raw["event_id"])
			event.EventID = &id
		}
		return event
	}
	if raw["family"] == nil && raw["kind"] == nil && raw["payload"] != nil {
		return engineEventFromDict(raw)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// engineEventFromDict 规范化信封还原（对齐 Python EngineEvent.from_dict）。
func engineEventFromDict(data map[string]any) *EngineEvent {
	event := &EngineEvent{
		Family:  valueToString(data["family"]),
		Kind:    valueToString(data["kind"]),
		TaskID:  valueToString(data["task_id"]),
		Ts:      valueToString(data["ts"]),
		Payload: map[string]any{},
	}
	if p, ok := data["payload"].(map[string]any); ok {
		for k, v := range p {
			event.Payload[k] = v
		}
	}
	if data["event_id"] != nil {
		id := safeInt(data["event_id"])
		event.EventID = &id
	}
	return event
}
