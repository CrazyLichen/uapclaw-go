package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/gorilla/websocket"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// acpDispatched dispatch 回调记录条目。
type acpDispatched struct {
	msg *schema.Message
}

// acpBridgeHarness 带记录的 dispatch 回调桥 harness。
// ws 句柄测试传 nil（桥只存句柄不写）；真 WS 帧断言在 Task 4 httptest 用例。
type acpBridgeHarness struct {
	bridge     *AcpGatewayBridge
	dispatched []*acpDispatched
	bound      []string // bindSessionClient 记录
}

// newAcpBridgeHarness 构造 harness（dispatch/bind 均记录）。
func newAcpBridgeHarness() *acpBridgeHarness {
	h := &acpBridgeHarness{}
	h.bridge = NewAcpGatewayBridge(func(ctx context.Context, msg *schema.Message) error {
		h.dispatched = append(h.dispatched, &acpDispatched{msg: msg})
		return nil
	},
		WithBindSessionClient(func(sessionID string, ws *websocket.Conn) {
			h.bound = append(h.bound, sessionID)
		}))
	return h
}

// TestAcpBridge_Initialize initialize 分派（能力合并经真 WS 帧断言，Task 4）。
func TestAcpBridge_Initialize(t *testing.T) {
	h := newAcpBridgeHarness()
	data := map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize",
		"params": map[string]any{
			"clientCapabilities": map[string]any{"fs": map[string]any{"readTextFile": true}},
		}}
	if !h.bridge.HandleJsonrpcRequest(context.Background(), nil, data) {
		t.Fatal("initialize should be handled")
	}
}

// TestAcpBridge_SessionNewAndList session/new 生成 acp_ 前缀会话 + bind + list 回显。
func TestAcpBridge_SessionNewAndList(t *testing.T) {
	h := newAcpBridgeHarness()
	data := map[string]any{"jsonrpc": "2.0", "id": 2, "method": "session/new", "params": map[string]any{}}
	if !h.bridge.HandleJsonrpcRequest(context.Background(), nil, data) {
		t.Fatal("session/new should be handled")
	}
	h.bridge.mu.Lock()
	known := len(h.bridge.knownSessions)
	h.bridge.mu.Unlock()
	if known != 1 {
		t.Fatalf("knownSessions should have 1 entry, got %d", known)
	}
	if len(h.bound) != 1 || !strings.HasPrefix(h.bound[0], "acp_") {
		t.Errorf("bound = %v, want one acp_-prefixed session", h.bound)
	}
	data2 := map[string]any{"jsonrpc": "2.0", "id": 3, "method": "session/list", "params": map[string]any{}}
	if !h.bridge.HandleJsonrpcRequest(context.Background(), nil, data2) {
		t.Fatal("session/list should be handled")
	}
}

// TestAcpBridge_SessionNewWithExplicitId 显式 sessionId 优先。
func TestAcpBridge_SessionNewWithExplicitId(t *testing.T) {
	h := newAcpBridgeHarness()
	data := map[string]any{"jsonrpc": "2.0", "id": 2, "method": "session/new",
		"params": map[string]any{"sessionId": "acp_explicit"}}
	if !h.bridge.HandleJsonrpcRequest(context.Background(), nil, data) {
		t.Fatal("session/new should be handled")
	}
	h.bridge.mu.Lock()
	_, ok := h.bridge.knownSessions["acp_explicit"]
	h.bridge.mu.Unlock()
	if !ok {
		t.Error("explicit sessionId should be registered in knownSessions")
	}
}

// TestAcpBridge_SessionLoadRejected session/load → -32601。
func TestAcpBridge_SessionLoadRejected(t *testing.T) {
	h := newAcpBridgeHarness()
	data := map[string]any{"jsonrpc": "2.0", "id": 4, "method": "session/load", "params": map[string]any{}}
	if !h.bridge.HandleJsonrpcRequest(context.Background(), nil, data) {
		t.Fatal("session/load should be handled (with -32601 error)")
	}
}

// TestAcpBridge_UnknownMethodRejected 未知方法 → -32601（返回 true 已处理）。
func TestAcpBridge_UnknownMethodRejected(t *testing.T) {
	h := newAcpBridgeHarness()
	data := map[string]any{"jsonrpc": "2.0", "id": 5, "method": "unknown.method", "params": map[string]any{}}
	if !h.bridge.HandleJsonrpcRequest(context.Background(), nil, data) {
		t.Fatal("unknown method should still be handled with -32601 (Python L332 return True)")
	}
	if len(h.dispatched) != 0 {
		t.Errorf("unknown method should not dispatch, got %d", len(h.dispatched))
	}
}

// TestAcpBridge_SessionPromptDispatch session/prompt → CHAT_SEND + mode=agent.plan +
// 三键 content/query/session_id + ctx/activePrompt 登记 + metadata。
func TestAcpBridge_SessionPromptDispatch(t *testing.T) {
	h := newAcpBridgeHarness()
	data := map[string]any{"jsonrpc": "2.0", "id": 6, "method": "session/prompt", "params": map[string]any{
		"sessionId": "s1", "messageId": "u1",
		"prompt":    []any{map[string]any{"type": "text", "text": "hello"}},
	}}
	if !h.bridge.HandleJsonrpcRequest(context.Background(), nil, data) {
		t.Fatal("session/prompt should be handled")
	}
	if len(h.dispatched) != 1 {
		t.Fatalf("session/prompt should dispatch exactly 1 message, got %d", len(h.dispatched))
	}
	m := h.dispatched[0].msg
	if m.ReqMethod != schema.ReqMethodChatSend {
		t.Errorf("ReqMethod = %v, want chat.send", m.ReqMethod)
	}
	if m.Mode != schema.ModeAgentPlan {
		t.Errorf("Mode = %v, want agent.plan", m.Mode)
	}
	if m.ChannelID != "acp" {
		t.Errorf("ChannelID = %v, want acp", m.ChannelID)
	}
	if m.SessionID != "s1" {
		t.Errorf("SessionID = %v, want s1", m.SessionID)
	}
	var params map[string]any
	if err := json.Unmarshal(m.Params, &params); err != nil {
		t.Fatalf("params unmarshal: %v", err)
	}
	if params["content"] != "hello" || params["query"] != "hello" || params["session_id"] != "s1" {
		t.Errorf("params content/query/session_id = %v/%v/%v, want hello/hello/s1",
			params["content"], params["query"], params["session_id"])
	}
	if !strings.HasPrefix(m.ID, "acp_") {
		t.Errorf("request ID = %q, want acp_ prefix", m.ID)
	}
	// ctx + activePrompt 登记
	h.bridge.mu.Lock()
	ctxEntry, hasCtx := h.bridge.requestCtxByReqID[m.ID]
	active, hasActive := h.bridge.activePromptBySession["s1"]
	h.bridge.mu.Unlock()
	if !hasCtx || ctxEntry == nil {
		t.Fatal("request ctx should be registered")
	}
	if ctxEntry.UserMessageID != "u1" {
		t.Errorf("ctx.UserMessageID = %q, want u1", ctxEntry.UserMessageID)
	}
	if !hasActive || active != m.ID {
		t.Errorf("activePromptBySession[s1] = %q, want %q", active, m.ID)
	}
}

// TestAcpBridge_SessionCancel session/cancel → cancelled result + ctx 清理。
func TestAcpBridge_SessionCancel(t *testing.T) {
	h := newAcpBridgeHarness()
	data := map[string]any{"jsonrpc": "2.0", "id": 6, "method": "session/prompt", "params": map[string]any{
		"sessionId": "s1", "text": "hi",
	}}
	h.bridge.HandleJsonrpcRequest(context.Background(), nil, data)
	cancel := map[string]any{"jsonrpc": "2.0", "id": 7, "method": "session/cancel",
		"params": map[string]any{"sessionId": "s1"}}
	if !h.bridge.HandleJsonrpcRequest(context.Background(), nil, cancel) {
		t.Fatal("session/cancel should be handled")
	}
	h.bridge.mu.Lock()
	_, hasActive := h.bridge.activePromptBySession["s1"]
	h.bridge.mu.Unlock()
	if hasActive {
		t.Error("activePrompt should be cleared after cancel")
	}
}

// TestAcpBridge_IsJsonrpcRequestAndExtractPromptText 判定与提取。
func TestAcpBridge_IsJsonrpcRequestAndExtractPromptText(t *testing.T) {
	if IsJsonrpcRequest(nil) {
		t.Error("nil data → false")
	}
	if IsJsonrpcRequest(map[string]any{"jsonrpc": "1.0", "method": "x"}) {
		t.Error("non-2.0 → false")
	}
	if IsJsonrpcRequest(map[string]any{"jsonrpc": "2.0", "method": "x"}) {
		// method 为 string → true（无 id 也可，Python 同）
	} else {
		t.Error("jsonrpc 2.0 + string method → true")
	}
	if !IsJsonrpcRequest(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "x"}) {
		t.Error("full request → true")
	}
	// extract：prompt list 形态
	got := ExtractPromptText(map[string]any{
		"prompt": []any{map[string]any{"type": "text", "text": "a"}, map[string]any{"type": "text", "text": "b"}},
	})
	if got != "a\nb" {
		t.Errorf("list prompt join = %q, want a\\nb", got)
	}
	// text/content/query 回退
	if ExtractPromptText(map[string]any{"text": " t1 "}) != "t1" {
		t.Error("text fallback should trim")
	}
	if ExtractPromptText(map[string]any{"content": "c1"}) != "c1" {
		t.Error("content fallback")
	}
	if ExtractPromptText(map[string]any{"query": "q1"}) != "q1" {
		t.Error("query fallback")
	}
	if ExtractPromptText(map[string]any{}) != "" {
		t.Error("no prompt → empty")
	}
}
