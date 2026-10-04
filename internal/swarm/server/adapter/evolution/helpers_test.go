package evolution

import (
	"context"
	"testing"

	gatewaypush "github.com/uapclaw/uapclaw-go/internal/swarm/server/gateway_push"
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockPushTransport 用于测试的模拟推送传输
type mockPushTransport struct {
	pushed []map[string]any
	err    error
}

func (m *mockPushTransport) SendPush(_ context.Context, msg map[string]any) error {
	if m.err != nil {
		return m.err
	}
	m.pushed = append(m.pushed, msg)
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──────────────────────────── PushEvolutionStatus 测试 ────────────────────────────

// TestPushEvolutionStatus 测试推送状态。
func TestPushEvolutionStatus(t *testing.T) {
	transport := &mockPushTransport{}
	pushCtx := &EvolutionPushContext{
		Transport: transport,
		ChannelID: "ch-1",
		SessionID: "sess-1",
	}
	update := evolutionlogic.BuildEvolutionStatusUpdate("req-1", "running", "generating", "Updating")
	buildMsgFn := func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any {
		return map[string]any{
			"session_id": sessionID,
			"request_id": requestID,
			"payload":    payload,
		}
	}

	err := PushEvolutionStatus(t.Context(), pushCtx, update, buildMsgFn)
	if err != nil {
		t.Errorf("PushEvolutionStatus() 返回错误: %v", err)
	}
	if len(transport.pushed) != 1 {
		t.Fatalf("pushed = %d, 期望 1", len(transport.pushed))
	}
	msg := transport.pushed[0]
	if msg["session_id"] != "sess-1" {
		t.Errorf("session_id = %q, 期望 sess-1", msg["session_id"])
	}
	payload, _ := msg["payload"].(map[string]any)
	if payload["event_type"] != "chat.evolution_status" {
		t.Errorf("event_type = %q, 期望 chat.evolution_status", payload["event_type"])
	}
}

// TestPushEvolutionStatus_noRequestID 测试不包含 request_id。
func TestPushEvolutionStatus_noRequestID(t *testing.T) {
	transport := &mockPushTransport{}
	pushCtx := &EvolutionPushContext{
		Transport: transport,
		ChannelID: "ch-1",
		SessionID: "sess-1",
	}
	update := evolutionlogic.BuildEvolutionStatusUpdate("req-1", "running", "generating")
	buildMsgFn := func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any {
		return payload
	}

	err := PushEvolutionStatus(t.Context(), pushCtx, update, buildMsgFn, false)
	if err != nil {
		t.Errorf("PushEvolutionStatus() 返回错误: %v", err)
	}
	payload := transport.pushed[0]
	if _, ok := payload["request_id"]; ok {
		t.Error("request_id 不应出现在 payload 中")
	}
}

// ──────────────────────────── PushEvolutionEvent 测试 ────────────────────────────

// TestPushEvolutionEvent 测试推送事件。
func TestPushEvolutionEvent(t *testing.T) {
	transport := &mockPushTransport{}
	pushCtx := &EvolutionPushContext{
		Transport: transport,
		ChannelID: "ch-1",
		SessionID: "sess-1",
	}
	evt := map[string]any{"event_type": "chat.answer", "content": "result"}
	buildMsgFn := func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any {
		return map[string]any{"session_id": sessionID, "payload": payload}
	}

	err := PushEvolutionEvent(t.Context(), pushCtx, "req-1", evt, buildMsgFn)
	if err != nil {
		t.Errorf("PushEvolutionEvent() 返回错误: %v", err)
	}
	if len(transport.pushed) != 1 {
		t.Fatalf("pushed = %d, 期望 1", len(transport.pushed))
	}
	msg := transport.pushed[0]
	payload, _ := msg["payload"].(map[string]any)
	if payload["request_id"] != "req-1" {
		t.Errorf("request_id = %q, 期望 req-1", payload["request_id"])
	}
}

// ──────────────────────────── BroadcastEvolutionProgress 测试 ────────────────────────────

// TestBroadcastEvolutionProgress 测试广播进度。
func TestBroadcastEvolutionProgress(t *testing.T) {
	var broadcasted []map[string]any
	broadcastFn := func(channelID *string, sessionID string, parsed map[string]any) {
		broadcasted = append(broadcasted, parsed)
	}
	parseFn := func(evt map[string]any) map[string]any {
		return evt
	}

	events := []map[string]any{
		{"event_type": "chat.answer", "data": "stream1"},
		{"event_type": "chat.ask_user_question"}, // 应跳过
		{"data": "stream2"},
	}
	chID := "ch-1"
	err := BroadcastEvolutionProgress(t.Context(), &chID, "sess-1", events, parseFn, broadcastFn)
	if err != nil {
		t.Errorf("BroadcastEvolutionProgress() 返回错误: %v", err)
	}
	if len(broadcasted) != 2 {
		t.Errorf("broadcasted = %d, 期望 2 (跳过审批事件)", len(broadcasted))
	}
}

// ──────────────────────────── PushEvolutionProgress 测试 ────────────────────────────

// TestPushEvolutionProgress 测试推送进度。
func TestPushEvolutionProgress(t *testing.T) {
	transport := &mockPushTransport{}
	pushCtx := &EvolutionPushContext{
		Transport: transport,
		ChannelID: "ch-1",
		SessionID: "sess-1",
	}
	parseFn := func(evt map[string]any) map[string]any {
		return evt
	}
	buildMsgFn := func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any {
		return map[string]any{"session_id": sessionID, "payload": payload}
	}

	events := []map[string]any{
		{"data": "chunk1"},
		{"event_type": "chat.ask_user_question"}, // 应跳过
	}
	err := PushEvolutionProgress(t.Context(), pushCtx, "req-1", events, parseFn, buildMsgFn)
	if err != nil {
		t.Errorf("PushEvolutionProgress() 返回错误: %v", err)
	}
	if len(transport.pushed) != 1 {
		t.Errorf("pushed = %d, 期望 1 (跳过审批事件)", len(transport.pushed))
	}
}

// TestPushEvolutionProgress_nilChunk 测试 nil chunk 跳过。
func TestPushEvolutionProgress_nilChunk(t *testing.T) {
	transport := &mockPushTransport{}
	pushCtx := &EvolutionPushContext{
		Transport: transport,
		ChannelID: "ch-1",
		SessionID: "sess-1",
	}
	parseFn := func(evt map[string]any) map[string]any { return nil }
	buildMsgFn := func(sessionID, requestID string, payload map[string]any, fallbackChannelID ...string) map[string]any {
		return payload
	}

	events := []map[string]any{{"data": "chunk"}}
	err := PushEvolutionProgress(t.Context(), pushCtx, "req-1", events, parseFn, buildMsgFn)
	if err != nil {
		t.Errorf("PushEvolutionProgress() 返回错误: %v", err)
	}
	if len(transport.pushed) != 0 {
		t.Errorf("pushed = %d, 期望 0 (nil chunk 应跳过)", len(transport.pushed))
	}
}

// ──────────────────────────── 接口合规测试 ────────────────────────────

// TestGatewayPushTransport接口合规 测试 mockPushTransport 实现 GatewayPushTransport。
func TestGatewayPushTransport接口合规(t *testing.T) {
	var _ gatewaypush.GatewayPushTransport = (*mockPushTransport)(nil)
}
