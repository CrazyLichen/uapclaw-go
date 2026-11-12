package runtime

import "github.com/uapclaw/uapclaw-go/internal/swarm/schema"

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimeEvent Runtime 流中的一个事件（线缆渲染之前的中间形态）。
// Python: jiuwenswarm/runtime/events.py RuntimeEvent
type RuntimeEvent struct {
	// RequestID 请求标识
	RequestID string
	// ChannelID 渠道标识
	ChannelID string
	// SessionID 会话标识（可为空）
	SessionID string
	// Payload 事件负载（含 event_type 等业务字段）
	Payload map[string]any
	// IsComplete 是否为完成事件
	IsComplete bool
	// OK 执行是否成功
	OK bool
}

// ──────────────────────────── 全局变量 ────────────────────────────

// TerminalErrorEventTypes 终态错误事件类型集合：模型/agent/runtime 失败以
// 流事件形式出现，而不是抛出异常。
// Python: events.py:127-133 TERMINAL_ERROR_EVENT_TYPES
//
// 这类事件可能带 ok=True —— RuntimeEvent.from_agent_message 的 ok 默认 True，
// 所以消费方必须同时看 event_type，只看 event.ok 会把失败的运行判成成功
// （上游注释记录的真实缺陷：心跳任务显示「上次运行成功」而每次模型调用都失败）。
//
// 各消费方（AgentServer 心跳/会话消息执行器、cron 调度、turn 确认）共用本集合，
// 新增终态错误类型只改这一处。
var TerminalErrorEventTypes = map[string]struct{}{
	"chat.error":      {},
	"runtime.error":   {},
	"execution.error": {},
	"team.error":      {},
	"error":           {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsTerminalErrorEvent 事件是否为终态错误（ok 不可信，按 event_type 判定）。
// Python: `event.event_type in TERMINAL_ERROR_EVENT_TYPES`
func IsTerminalErrorEvent(ev *RuntimeEvent) bool {
	_, ok := TerminalErrorEventTypes[ev.EventType()]
	return ok
}

// Control 构造控制事件。
// Python: RuntimeEvent.control(request_id=..., channel_id=..., session_id=..., payload=...)
func Control(req *schema.AgentRequest, payload map[string]any) *RuntimeEvent {
	ev := &RuntimeEvent{Payload: payload, OK: true}
	if req == nil {
		return ev
	}
	ev.RequestID, ev.ChannelID = req.RequestID, req.ChannelID
	if req.SessionID != nil {
		ev.SessionID = *req.SessionID
	}
	return ev
}

// FromAgentResponse 由非流式响应包装为 RuntimeEvent。
// Python: RuntimeEvent.from_agent_message
func FromAgentResponse(resp *schema.AgentResponse) *RuntimeEvent {
	if resp == nil {
		return &RuntimeEvent{OK: true, IsComplete: true}
	}
	return &RuntimeEvent{
		RequestID:  resp.RequestID,
		ChannelID:  resp.ChannelID,
		Payload:    resp.Payload,
		IsComplete: true,
		OK:         resp.OK,
	}
}

// FromAgentResponseChunk 由流式片段包装为 RuntimeEvent。
func FromAgentResponseChunk(chunk *schema.AgentResponseChunk) *RuntimeEvent {
	if chunk == nil {
		return &RuntimeEvent{OK: true, IsComplete: true}
	}
	return &RuntimeEvent{
		RequestID:  chunk.RequestID,
		ChannelID:  chunk.ChannelID,
		Payload:    chunk.Payload,
		IsComplete: chunk.IsComplete,
		OK:         true,
	}
}

// EventType 返回 payload 中的 event_type。
// Python: RuntimeEvent.event_type
func (e *RuntimeEvent) EventType() string {
	if e == nil || e.Payload == nil {
		return ""
	}
	s, _ := e.Payload["event_type"].(string)
	return s
}

// ToChunk 还原为流式片段（只取线缆需要的字段）。
func (e *RuntimeEvent) ToChunk() *schema.AgentResponseChunk {
	return &schema.AgentResponseChunk{
		RequestID:  e.RequestID,
		ChannelID:  e.ChannelID,
		Payload:    e.Payload,
		IsComplete: e.IsComplete,
	}
}

// ToDict 序列化为字典（对齐 Python to_dict，不含仅供执行期使用的字段）。
func (e *RuntimeEvent) ToDict() map[string]any {
	return map[string]any{
		"request_id":  e.RequestID,
		"channel_id":  e.ChannelID,
		"session_id":  e.SessionID,
		"payload":     e.Payload,
		"is_complete": e.IsComplete,
		"ok":          e.OK,
	}
}
