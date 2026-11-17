// 本文件承载 team.mq.publish（Gateway 中继）的外部团队事件 wire 编解码。
//
// 归包偏差（D-15.16-b8）：Python 定义在 interaction/payload.py 的
// ExternalTeamEvent；Go 侧 messager → interaction 是 import cycle
// （tools → messager 且 interaction → tools），且 messager/hybrid.go 是它
// 的唯一生产写方、Gateway 中继 handler 是唯一生产读方，故落在双方都能
// 引用的 schema/events 包。
//
// Python: openjiuwen/agent_teams/interaction/payload.py:36,128-153
package events

import "fmt"

// ──────────────────────────── 常量 ────────────────────────────

// ExternalTeamEventType team.mq.publish 携带的外部团队事件 wire 类型标记。
// 协议字符串，不本地化。
// Python: _EXTERNAL_TEAM_EVENT_TYPE (payload.py:36)
const ExternalTeamEventType = "team.external_event"

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalTeamEvent 经外部交互通道收到的一条标准团队事件。
// Python: ExternalTeamEvent (interaction/payload.py:128-133)
type ExternalTeamEvent struct {
	// Topic 事件主题类别
	Topic TeamTopic
	// Event 事件消息
	Event *EventMessage
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ExternalTeamEventToWire 返回 team.mq.publish 携带的 JSON-safe 载荷。
// Python: ExternalTeamEvent.to_wire (payload.py:134-140)
func ExternalTeamEventToWire(topic TeamTopic, event *EventMessage) (map[string]any, error) {
	if event == nil {
		return nil, fmt.Errorf("malformed external team event: nil event")
	}
	// Python: event.model_dump(mode="json")——Go 侧按 EventMessage 字段逐个
	// 平铺，保 JSON 兼容（EventMessage 无嵌套结构体）。
	eventMap := map[string]any{
		"event_type": event.EventType,
		"payload":    event.Payload,
		"sender_id":  event.SenderID,
	}
	return map[string]any{
		"type":  ExternalTeamEventType,
		"topic": string(topic),
		"event": eventMap,
	}, nil
}

// ExternalTeamEventFromWire 解析该载荷形状；其它交互输入返回 nil。
// Python: ExternalTeamEvent.from_wire (payload.py:142-153)
func ExternalTeamEventFromWire(payload any) (*ExternalTeamEvent, error) {
	m, ok := payload.(map[string]any)
	if !ok {
		return nil, nil
	}
	if t, _ := m["type"].(string); t != ExternalTeamEventType {
		return nil, nil
	}
	topic, _ := m["topic"].(string)
	event, ok := m["event"].(map[string]any)
	if topic == "" || !ok {
		return nil, fmt.Errorf("malformed external team event")
	}
	msg := &EventMessage{}
	if et, ok := event["event_type"].(string); ok {
		msg.EventType = et
	}
	if p, ok := event["payload"].(map[string]any); ok {
		msg.Payload = p
	}
	if s, ok := event["sender_id"].(string); ok {
		msg.SenderID = s
	}
	return &ExternalTeamEvent{Topic: TeamTopic(topic), Event: msg}, nil
}
