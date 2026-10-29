package gateway

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

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

// makeAcpEventMsg 构造 event 消息（测试辅助）。
func makeAcpEventMsg(requestID, sessionID string, eventType schema.EventType, payload map[string]any) *schema.Message {
	return &schema.Message{
		ID:        requestID,
		Type:      schema.MessageTypeEvent,
		ChannelID: "acp",
		SessionID: sessionID,
		EventType: eventType,
		Payload:   payload,
		Timestamp: schema.NowTimestamp(),
	}
}

// registerAcpPrompt 注册一个 prompt ctx（测试辅助，返回 requestID）。
func registerAcpPrompt(t *testing.T, h *acpBridgeHarness, sessionID, userMessageID string) string {
	t.Helper()
	data := map[string]any{"jsonrpc": "2.0", "id": 6, "method": "session/prompt", "params": map[string]any{
		"sessionId": sessionID, "messageId": userMessageID, "text": "hi",
	}}
	if !h.bridge.HandleJsonrpcRequest(context.Background(), nil, data) {
		t.Fatal("session/prompt should be handled")
	}
	return h.dispatched[len(h.dispatched)-1].msg.ID
}

// TestAcpBridge_SendMessageDelta delta → session/update 通知 + markStreamActivity。
func TestAcpBridge_SendMessageDelta(t *testing.T) {
	h := newAcpBridgeHarness()
	rid := registerAcpPrompt(t, h, "s1", "u1")
	h.bridge.mu.Lock()
	ctxEntry := h.bridge.requestCtxByReqID[rid]
	h.bridge.mu.Unlock()
	// 预置 sawProcessingIdle=true，验证 delta 会 markStreamActivity 重置
	ctxEntry.SawProcessingIdle = true
	msg := makeAcpEventMsg(rid, "s1", schema.EventTypeChatDelta, map[string]any{"content": "hello"})
	if !h.bridge.SendMessage(context.Background(), msg, nil) {
		t.Fatal("delta should be handled")
	}
	if ctxEntry.SawProcessingIdle {
		t.Error("delta should reset sawProcessingIdle (markStreamActivity)")
	}
	if ctxEntry.State.AssistantText != "hello" {
		t.Errorf("AssistantText = %q, want hello", ctxEntry.State.AssistantText)
	}
}

// TestAcpBridge_SendMessageReasoningAndTool reasoning/tool 系 → update + activity 重置。
func TestAcpBridge_SendMessageReasoningAndTool(t *testing.T) {
	h := newAcpBridgeHarness()
	rid := registerAcpPrompt(t, h, "s1", "u1")
	msg := makeAcpEventMsg(rid, "s1", schema.EventTypeChatToolCall, map[string]any{
		"tool_call": map[string]any{"id": "tc-1", "name": "bash", "arguments": map[string]any{"command": "ls"}},
	})
	if !h.bridge.SendMessage(context.Background(), msg, nil) {
		t.Fatal("tool_call should be handled")
	}
	msg2 := makeAcpEventMsg(rid, "s1", schema.EventTypeChatSubtaskUpdate, map[string]any{"tasks": []any{"a"}})
	if !h.bridge.SendMessage(context.Background(), msg2, nil) {
		t.Fatal("subtask_update should be handled")
	}
}

// TestAcpBridge_SendMessageFinalAndIdleEndTurn final 后 idle → end_turn + ctx 清理。
func TestAcpBridge_SendMessageFinalAndIdleEndTurn(t *testing.T) {
	h := newAcpBridgeHarness()
	rid := registerAcpPrompt(t, h, "s1", "u1")
	// chat.final 先到（无既有文本 → 全新直发 chunk）
	finalMsg := makeAcpEventMsg(rid, "s1", schema.EventTypeChatFinal, map[string]any{"content": "done"})
	if !h.bridge.SendMessage(context.Background(), finalMsg, nil) {
		t.Fatal("chat.final should be handled")
	}
	h.bridge.mu.Lock()
	sawFinal := h.bridge.requestCtxByReqID[rid].SawChatFinal
	h.bridge.mu.Unlock()
	if !sawFinal {
		t.Fatal("SawChatFinal should be true after chat.final")
	}
	// processing idle 后到 → 直接收尾（无 pending、无需等 final text——已见过）
	idleMsg := makeAcpEventMsg(rid, "s1", schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false})
	if !h.bridge.SendMessage(context.Background(), idleMsg, nil) {
		t.Fatal("processing idle should be handled")
	}
	h.bridge.mu.Lock()
	_, hasCtx := h.bridge.requestCtxByReqID[rid]
	h.bridge.mu.Unlock()
	if hasCtx {
		t.Error("ctx should be cleared after end_turn finalize")
	}
}

// TestAcpBridge_SendMessageIdleWaitsLateFinal idle 先到（有文本无 final）→ timer 挂起。
func TestAcpBridge_SendMessageIdleWaitsLateFinal(t *testing.T) {
	h := newAcpBridgeHarness()
	rid := registerAcpPrompt(t, h, "s1", "u1")
	// 先 delta（产生 assistant 文本）
	deltaMsg := makeAcpEventMsg(rid, "s1", schema.EventTypeChatDelta, map[string]any{"content": "partial"})
	h.bridge.SendMessage(context.Background(), deltaMsg, nil)
	// idle 到达：sawChatFinal=false 且 assistantText 非空 → shouldWait=true → 调度 timer
	idleMsg := makeAcpEventMsg(rid, "s1", schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false})
	if !h.bridge.SendMessage(context.Background(), idleMsg, nil) {
		t.Fatal("idle should be handled")
	}
	h.bridge.mu.Lock()
	_, hasCtx := h.bridge.requestCtxByReqID[rid]
	timerActive := h.bridge.requestCtxByReqID[rid] != nil && h.bridge.requestCtxByReqID[rid].idleFinalizeTimerSlot != nil
	h.bridge.mu.Unlock()
	if !hasCtx {
		t.Fatal("ctx should still exist (waiting for late final)")
	}
	if !timerActive {
		t.Error("idleFinalizeTimer should be scheduled when waiting for late final text")
	}
	// 迟到 final 到达 → cancel timer + 补发 delta 后缀 + 收尾
	lateFinal := makeAcpEventMsg(rid, "s1", schema.EventTypeChatFinal, map[string]any{"content": "partial full"})
	if !h.bridge.SendMessage(context.Background(), lateFinal, nil) {
		t.Fatal("late final should be handled")
	}
	// 收尾是 clearRequestContext（等同步路径完成）
	h.bridge.mu.Lock()
	_, hasCtx2 := h.bridge.requestCtxByReqID[rid]
	h.bridge.mu.Unlock()
	if hasCtx2 {
		t.Error("ctx should be cleared after late final + sawProcessingIdle=true finalize")
	}
}

// TestAcpBridge_SendMessageError chat.error → -32603 + ctx 清理。
func TestAcpBridge_SendMessageError(t *testing.T) {
	h := newAcpBridgeHarness()
	rid := registerAcpPrompt(t, h, "s1", "u1")
	errMsg := makeAcpEventMsg(rid, "s1", schema.EventTypeChatError, map[string]any{"error": "boom"})
	if !h.bridge.SendMessage(context.Background(), errMsg, nil) {
		t.Fatal("chat.error should be handled")
	}
	h.bridge.mu.Lock()
	_, hasCtx := h.bridge.requestCtxByReqID[rid]
	h.bridge.mu.Unlock()
	if hasCtx {
		t.Error("ctx should be cleared after error")
	}
}

// TestAcpBridge_SendMessageUnknownRequestID 无 ctx → false（不处理）。
func TestAcpBridge_SendMessageUnknownRequestID(t *testing.T) {
	h := newAcpBridgeHarness()
	msg := makeAcpEventMsg("acp_nonexistent", "s1", schema.EventTypeChatDelta, map[string]any{"content": "x"})
	if h.bridge.SendMessage(context.Background(), msg, nil) {
		t.Error("unknown request id should return false (not handled)")
	}
}

// TestAcpBridge_PendingRPCDeferEndTurn pending RPC>0 → end_turn 挂起，回流后收尾。
func TestAcpBridge_PendingRPCDeferEndTurn(t *testing.T) {
	h := newAcpBridgeHarness()
	rid := registerAcpPrompt(t, h, "s1", "u1")
	// outbound_intercept 捕获 acp.output_request → pending 注册
	raw := map[string]any{"jsonrpc": "2.0", "id": "tool-1", "method": "session/request_permission",
		"params": map[string]any{"sessionId": "s1", "options": []any{}}}
	outMsg := &schema.Message{
		ID: rid, Type: schema.MessageTypeEvent, ChannelID: "acp", SessionID: "s1",
		EventType: schema.EventTypeChatToolResult,
		Payload: map[string]any{
			"event_type": "acp.output_request",
			"jsonrpc":    raw,
		},
	}
	// OutboundIntercept 签名是 (ws, data{"raw": msg})：
	if !h.bridge.OutboundIntercept(nil, map[string]any{"raw": outMsg}) {
		t.Fatal("output_request should be intercepted")
	}
	h.bridge.mu.Lock()
	cnt := h.bridge.pendingRPCCountBySession["s1"]
	h.bridge.mu.Unlock()
	if cnt != 1 {
		t.Fatalf("pendingRPCCount[s1] = %d, want 1", cnt)
	}
	// idle 到达（无文本无 final）→ pending>0 → 挂起不收尾
	idleMsg := makeAcpEventMsg(rid, "s1", schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false})
	h.bridge.SendMessage(context.Background(), idleMsg, nil)
	h.bridge.mu.Lock()
	_, hasCtx := h.bridge.requestCtxByReqID[rid]
	pendingStop := h.bridge.requestCtxByReqID[rid] != nil && h.bridge.requestCtxByReqID[rid].PendingStopReason == "end_turn"
	h.bridge.mu.Unlock()
	if !hasCtx || !pendingStop {
		t.Fatal("ctx should be deferred with PendingStopReason=end_turn")
	}
	// 工具响应回流 → inbound_intercept → dispatch acp.tool_response → pending 清零 → maybe finalize
	resp := map[string]any{"jsonrpc": "2.0", "id": "tool-1", "result": map[string]any{"outcome": "selected", "optionId": "o1"}}
	if !h.bridge.InboundIntercept(nil, resp) {
		t.Fatal("jsonrpc response should be intercepted")
	}
	if len(h.dispatched) != 2 {
		t.Fatalf("dispatch count = %d, want 2 (prompt + tool_response)", len(h.dispatched))
	}
	toolMsg := h.dispatched[1].msg
	if toolMsg.ReqMethod != schema.ReqMethodACPToolResponse {
		t.Errorf("tool response ReqMethod = %v, want acp.tool_response", toolMsg.ReqMethod)
	}
	// Python L559-562：pending_stop_reason 先清，ws 为 None → continue（ctx 保留）。
	// 本 harness 注册 prompt 时 ws=nil；真 WS 收尾路径在 Task 4 真 WS 用例验证。
	h.bridge.mu.Lock()
	_, hasCtx2 := h.bridge.requestCtxByReqID[rid]
	stopCleared := h.bridge.requestCtxByReqID[rid] != nil && h.bridge.requestCtxByReqID[rid].PendingStopReason == ""
	h.bridge.mu.Unlock()
	if !hasCtx2 {
		t.Error("ctx should remain when client ws is nil (Python: ws is None → continue)")
	}
	if !stopCleared {
		t.Error("PendingStopReason should be cleared before the ws check (Python L559)")
	}
}

// TestAcpBridge_InboundInterceptUnknownResponse 未知/迟到响应 → accepted=false 软忽略。
func TestAcpBridge_InboundInterceptUnknownResponse(t *testing.T) {
	h := newAcpBridgeHarness()
	resp := map[string]any{"jsonrpc": "2.0", "id": "unknown-1", "result": map[string]any{}}
	if !h.bridge.InboundIntercept(nil, resp) {
		t.Fatal("unknown response should be intercepted (soft-ignored)")
	}
	if len(h.dispatched) != 0 {
		t.Errorf("unknown response should not dispatch, got %d", len(h.dispatched))
	}
}

// TestAcpBridge_InboundInterceptNonResponse 非 JSON-RPC 响应帧 → false。
func TestAcpBridge_InboundInterceptNonResponse(t *testing.T) {
	h := newAcpBridgeHarness()
	if h.bridge.InboundIntercept(nil, map[string]any{"jsonrpc": "2.0", "id": 1, "method": "x"}) {
		t.Error("request frame should not be intercepted by InboundIntercept")
	}
	if h.bridge.InboundIntercept(nil, map[string]any{"jsonrpc": "2.0", "method": "notify"}) {
		t.Error("notification frame should not be intercepted")
	}
}

// TestAcpBridge_Cleanup 断连按 ws 清 pending。
func TestAcpBridge_Cleanup(t *testing.T) {
	h := newAcpBridgeHarness()
	registerAcpPrompt(t, h, "s1", "u1")
	raw := map[string]any{"jsonrpc": "2.0", "id": "tool-1", "method": "session/request_permission"}
	outMsg := &schema.Message{
		ID: "x", Type: schema.MessageTypeEvent, ChannelID: "acp", SessionID: "s1",
		EventType: schema.EventTypeChatToolResult,
		Payload:   map[string]any{"event_type": "acp.output_request", "jsonrpc": raw},
	}
	h.bridge.OutboundIntercept(nil, map[string]any{"raw": outMsg})
	h.bridge.mu.Lock()
	cnt := h.bridge.pendingRPCCountBySession["s1"]
	h.bridge.mu.Unlock()
	if cnt != 1 {
		t.Fatalf("pending count = %d, want 1", cnt)
	}
	// cleanup(nil) 不 panic；真实 ws 句柄场景在 Task 4 真 WS 用例验证
	h.bridge.Cleanup(nil)
}

// TestAcpBridge_TimerSuperseded timer 被 superseded 后不误收尾（Go 增强）。
func TestAcpBridge_TimerSuperseded(t *testing.T) {
	h := newAcpBridgeHarness()
	rid := registerAcpPrompt(t, h, "s1", "u1")
	h.bridge.mu.Lock()
	ctxEntry := h.bridge.requestCtxByReqID[rid]
	h.bridge.mu.Unlock()
	// 手动调度两个 timer：第一个被第二个 supersede
	h.bridge.scheduleIdleFinalize(rid, ctxEntry, nil)
	first := ctxEntry.idleFinalizeTimerSlot
	h.bridge.scheduleIdleFinalize(rid, ctxEntry, nil)
	if ctxEntry.idleFinalizeTimerSlot == first {
		t.Fatal("second schedule should replace first timer")
	}
}

// TestAcpBridge_IdleFinalizeAfterTimeout idle timer 超时 → 直发 end_turn（缺尾部文本语义）。
func TestAcpBridge_IdleFinalizeAfterTimeout(t *testing.T) {
	short := 0.02
	h := &acpBridgeHarness{}
	// dispatch 回调必须记录（rid 取自 dispatched 尾部）
	h.bridge = NewAcpGatewayBridge(func(ctx context.Context, msg *schema.Message) error {
		h.dispatched = append(h.dispatched, &acpDispatched{msg: msg})
		return nil
	},
		WithIdleFinalizeSeconds(func() float64 { return short }))
	data := map[string]any{"jsonrpc": "2.0", "id": 6, "method": "session/prompt",
		"params": map[string]any{"sessionId": "s1", "text": "hi"}}
	h.bridge.HandleJsonrpcRequest(context.Background(), nil, data)
	rid := h.dispatched[len(h.dispatched)-1].msg.ID
	deltaMsg := makeAcpEventMsg(rid, "s1", schema.EventTypeChatDelta, map[string]any{"content": "partial"})
	h.bridge.SendMessage(context.Background(), deltaMsg, nil)
	idleMsg := makeAcpEventMsg(rid, "s1", schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false})
	h.bridge.SendMessage(context.Background(), idleMsg, nil)
	// 等 timer 超时触发
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		h.bridge.mu.Lock()
		_, hasCtx := h.bridge.requestCtxByReqID[rid]
		h.bridge.mu.Unlock()
		if !hasCtx {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.bridge.mu.Lock()
	_, hasCtx := h.bridge.requestCtxByReqID[rid]
	h.bridge.mu.Unlock()
	if hasCtx {
		t.Fatal("ctx should be cleared by idle finalize timer after timeout")
	}
}

// sendAcpJSON 发送 JSON 帧（测试辅助，gateway harness 既有 WriteMessage 模式）。
func sendAcpJSON(t *testing.T, conn *websocket.Conn, frame map[string]any) {
	t.Helper()
	data, err := json.Marshal(frame)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
		t.Fatalf("write: %v", err)
	}
}

// TestGatewayServerAcp_SetBridgeAndHooks SetAcpBridge + InstallDefaultAcpHooks
// 把桥拦截器装进 /acp 路由 + JSON-RPC 请求经 acp 分支进入桥。
func TestGatewayServerAcp_SetBridgeAndHooks(t *testing.T) {
	route := &RouteConfig{
		Path: "/acp", ChannelID: "acp",
		ForwardMethods: map[string]bool{"chat.send": true},
	}
	s, httpSrv := newTestGatewayServer(t, route)
	bridge := NewAcpGatewayBridge(func(ctx context.Context, msg *schema.Message) error { return nil })
	s.SetAcpBridge(bridge)
	s.InstallDefaultAcpHooks()

	// 拦截器已装填
	if route.InboundInterceptor == nil || route.OutboundInterceptor == nil {
		t.Fatal("InstallDefaultAcpHooks should fill route interceptors")
	}

	conn := dialWS(t, httpSrv, "/acp")
	_ = readFrame(t, conn) // connection.ack 首帧

	// JSON-RPC 请求经 handleRawMessage 的 acp 分支进入桥（返回 initialize result）
	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{},
	})
	frame := readFrame(t, conn)
	if _, hasResult := frame["result"]; !hasResult {
		t.Fatalf("initialize should return result via acp branch, got %v", frame)
	}
}

// TestGatewayServerAcp_SendBranch Send 的 acp 分支调用桥 SendMessage。
// session/prompt 派生 acp_xxx 请求键并注册桥 ctx；bindSessionClient 把
// 服务器侧 conn 写入 sessionToClient（对齐 Python _bind_acp_session_client）；
// ChatDelta 事件经 Send acp 分支转 session/update 通知帧。
func TestGatewayServerAcp_SendBranch(t *testing.T) {
	route := &RouteConfig{
		Path: "/acp", ChannelID: "acp",
		ForwardMethods: map[string]bool{"chat.send": true},
	}
	s, httpSrv := newTestGatewayServer(t, route)

	// dispatch 回调捕获派生请求键（分派在读循环 goroutine 同步执行，
	// channel 做测试主 goroutine 的同步点）
	reqIDCh := make(chan string, 1)
	bridge := NewAcpGatewayBridge(func(ctx context.Context, msg *schema.Message) error {
		select {
		case reqIDCh <- msg.ID:
		default:
		}
		return nil
	},
		// 注册的是服务器侧 conn（HandleConnection 升级出的连接）——
		// 直接塞 dialWS 返回的客户端句柄会把出站帧写回服务器，
		// 触发读循环 invalid request res 帧（gateway_server_test.go 同坑）
		WithBindSessionClient(func(sessionID string, ws *websocket.Conn) {
			s.clientsMu.Lock()
			s.sessionToClient[[2]string{"acp", sessionID}] = ws
			s.clientsMu.Unlock()
		}))
	s.SetAcpBridge(bridge)
	s.InstallDefaultAcpHooks()

	conn := dialWS(t, httpSrv, "/acp")
	_ = readFrame(t, conn) // connection.ack 首帧

	// session/prompt：注册桥请求 ctx + 会话路由（无即时响应帧，end_turn 等事件回流）
	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 5, "method": "session/prompt",
		"params": map[string]any{"sessionId": "s1", "text": "hi"},
	})

	var requestID string
	select {
	case requestID = <-reqIDCh:
	case <-time.After(2 * time.Second):
		t.Fatal("session/prompt dispatch not invoked")
	}

	msg := &schema.Message{
		ID: requestID, Type: schema.MessageTypeEvent, ChannelID: "acp", SessionID: "s1",
		EventType: schema.EventTypeChatDelta, Payload: map[string]any{"content": "hello"},
	}
	if err := s.Send(context.Background(), msg); err != nil {
		t.Fatalf("send: %v", err)
	}
	frame := readFrame(t, conn)
	if frame["method"] != "session/update" {
		t.Fatalf("acp branch should convert delta to session/update, got %v", frame)
	}
}
