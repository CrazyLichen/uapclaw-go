package messager

import (
	"context"
	"strings"
	"testing"

	atevents "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema/events"
)

// TestWebSocketEventPublisherBuildRequest topic 门卫与信封形状，
// 对齐 Python _build_request（hybrid.py:73-93）。
func TestWebSocketEventPublisherBuildRequest(t *testing.T) {
	p := NewWebSocketEventPublisher("ws://127.0.0.1:19001/ws", "sess1", "team1", 10)
	msg := atevents.NewEventMessage(atevents.TeamEventMessage, map[string]any{"team_name": "team1"}, "")
	topic := atevents.TeamTopicMessage.Build("sess1", "team1")
	req, err := p.buildRequest(topic, msg, "team-event-abc")
	if err != nil {
		t.Fatalf("合法 topic 不应报错: %v", err)
	}
	if req["type"] != "req" || req["id"] != "team-event-abc" {
		t.Fatalf("RPC 信封形状不符: %v", req)
	}
	if req["method"] != "team.mq.publish" {
		t.Fatalf("method 应为 team.mq.publish，实际 %v", req["method"])
	}
	if req["is_stream"] != false {
		t.Fatal("is_stream 应为 false")
	}
	params := req["params"].(map[string]any)
	if params["session_id"] != "sess1" {
		t.Fatal("session_id 应进 params")
	}

	// 非法 topic 枚举 → error（Python TeamTopic(...) ValueError）
	if _, err := p.buildRequest("session:sess1:team:team1:bogus", msg, "id2"); err == nil {
		t.Fatal("未知 topic 枚举应报错")
	}
	// topic 与 client 身份不匹配 → error（Python 归属校验）
	if _, err := p.buildRequest(atevents.TeamTopicMessage.Build("other", "team1"), msg, "id3"); err == nil {
		t.Fatal("topic 与身份不匹配应报错")
	}
}

// TestHybridMessagerPublishOnly subscribe/send 面全部拒绝（Python NotImplementedError）。
func TestHybridMessagerPublishOnly(t *testing.T) {
	m := NewHybridMessager(nil, "member1")
	ctx := context.Background()
	if err := m.Subscribe(ctx, "t", nil); err == nil || !strings.Contains(err.Error(), "publishing") {
		t.Fatalf("Subscribe 应拒绝: %v", err)
	}
	if err := m.Unsubscribe(ctx, "t"); err == nil {
		t.Fatal("Unsubscribe 应拒绝")
	}
	if err := m.Send(ctx, "a", nil); err == nil {
		t.Fatal("Send 应拒绝")
	}
	if err := m.RegisterDirectMessageHandler(ctx, nil); err == nil {
		t.Fatal("RegisterDirectMessageHandler 应拒绝")
	}
	if err := m.UnregisterDirectMessageHandler(ctx); err == nil {
		t.Fatal("UnregisterDirectMessageHandler 应拒绝")
	}
}

// TestExternalTeamEventWireRoundTrip to_wire/from_wire 往返。
func TestExternalTeamEventWireRoundTrip(t *testing.T) {
	msg := atevents.NewEventMessage("team.message_sent", map[string]any{"team_name": "t1"}, "sender1")
	topic := atevents.TeamTopicMessage
	wire, err := atevents.ExternalTeamEventToWire(topic, msg)
	if err != nil {
		t.Fatalf("to_wire 失败: %v", err)
	}
	if wire["type"] != "team.external_event" || wire["topic"] != string(topic) {
		t.Fatalf("wire 形状不符: %v", wire)
	}
	parsed, err := atevents.ExternalTeamEventFromWire(wire)
	if err != nil || parsed == nil {
		t.Fatalf("from_wire 失败: %v", err)
	}
	if parsed.Topic != topic || parsed.Event.SenderID != "sender1" {
		t.Fatalf("往返不保真: %+v", parsed)
	}
	// 非 external_event 形状 → nil（Python from_wire 返回 None）
	if parsed, _ := atevents.ExternalTeamEventFromWire(map[string]any{"type": "other"}); parsed != nil {
		t.Fatal("其它形状应返回 nil")
	}
}
