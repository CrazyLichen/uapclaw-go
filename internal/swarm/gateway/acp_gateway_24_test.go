package gateway

// 11.25 Task 4：桥 24 例映射测试（Python test_app_gateway_acp.py 基线）。
// Go 侧 harness 差异：Python FakeWebSocket 直接注入 bind_*_client 表；
// Go 用真 WS（dialWS + ReadMessage 收帧），帧断言从 sent_frames[N] 变为
// readFrame 依序读取。bind_request_client/bind_session_client 的 Python 探针
// 对应 Go 同包直写 requestToClient/sessionToClient 表（服务器侧 conn，
// 经拨号差集追踪获得——塞客户端句柄会把出站帧写回服务器读循环）。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试 harness ────────────────────────────

// acpTestServer 带 /acp 路由 + ACP 桥的测试服务器。
type acpTestServer struct {
	server  *GatewayServer
	httpSrv *httptest.Server
	bridge  *AcpGatewayBridge
}

// newAcpTestServer 构造 /acp 测试服务器（forward chat.send/history.get，对齐
// Python build_server 的 forward_methods 集合）。
func newAcpTestServer(t *testing.T) *acpTestServer {
	return newAcpTestServerOpt(t, nil)
}

// newAcpTestServerOpt newAcpTestServer 的可配置版本（onMessage 回调 + 桥选项）。
func newAcpTestServerOpt(t *testing.T, onMessage func(ctx context.Context, msg *schema.Message) error, opts ...AcpBridgeOption) *acpTestServer {
	t.Helper()
	route := &RouteConfig{
		Path:                         "/acp",
		ChannelID:                    "acp",
		ForwardMethods:               map[string]bool{"chat.send": true, "history.get": true},
		ForwardNoLocalHandlerMethods: map[string]bool{"history.get": true},
		LocalHandlers:                map[string]LocalHandlerFunc{},
	}
	s, httpSrv := newTestGatewayServer(t, route)
	a := &acpTestServer{server: s, httpSrv: httpSrv}
	cb := onMessage
	if cb == nil {
		cb = func(ctx context.Context, msg *schema.Message) error { return nil }
	}
	a.bridge = NewAcpGatewayBridge(cb, append([]AcpBridgeOption{
		WithBindSessionClient(func(sessionID string, ws *websocket.Conn) {
			// 对齐 Python bind_session_client → _bind_acp_session_client
			a.server.clientsMu.Lock()
			a.server.sessionToClient[[2]string{"acp", sessionID}] = ws
			a.server.clientsMu.Unlock()
		}),
	}, opts...)...)
	// 对齐 Python 测试的 server.on_message(cb)：桥（jsonrpc 派发）与
	// handleRawMessage（type=req 转发）共享同一回调；jsonrpc 分支早退
	// 不经 s.onMessageCb，无双重派发。
	a.server.OnMessage(cb)
	s.SetAcpBridge(a.bridge)
	s.InstallDefaultAcpHooks()
	return a
}

// dialAcpTracked 拨号 /acp 并追踪服务器侧 conn（拨号差集）。
// 返回 (客户端句柄, 服务器侧句柄)——出站定向表必须存服务器侧句柄。
func (a *acpTestServer) dialAcpTracked(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	before := a.snapshotClients()
	conn := dialWS(t, a.httpSrv, "/acp")
	_ = readFrame(t, conn) // connection.ack 首帧
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		after := a.snapshotClients()
		for c := range after {
			if _, seen := before[c]; !seen {
				return conn, c
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("server-side conn not registered after dial")
	return nil, nil
}

// snapshotClients 拍 clients 集合快照。
func (a *acpTestServer) snapshotClients() map[*websocket.Conn]struct{} {
	a.server.clientsMu.RLock()
	defer a.server.clientsMu.RUnlock()
	out := make(map[*websocket.Conn]struct{}, len(a.server.clients))
	for c := range a.server.clients {
		out[c] = struct{}{}
	}
	return out
}

// bindAcpRequestClient 直写 requestToClient（对齐 Python probe.bind_request_client）。
func (a *acpTestServer) bindAcpRequestClient(requestID string, ws *websocket.Conn) {
	a.server.clientsMu.Lock()
	a.server.requestToClient[[2]string{"acp", requestID}] = ws
	a.server.clientsMu.Unlock()
}

// bindAcpSessionClient 直写 sessionToClient（对齐 Python probe.bind_session_client）。
func (a *acpTestServer) bindAcpSessionClient(sessionID string, ws *websocket.Conn) {
	a.server.clientsMu.Lock()
	a.server.sessionToClient[[2]string{"acp", sessionID}] = ws
	a.server.clientsMu.Unlock()
}

// acpEventMsg 出站事件消息模板（output_request 场景 eventType 传空——
// Python 测试的 event_type=None，不命中 send_message 五分支）。
func acpEventMsg(requestID, sessionID string, eventType schema.EventType, payload map[string]any) *schema.Message {
	return &schema.Message{
		ID: requestID, Type: schema.MessageTypeEvent, ChannelID: "acp", SessionID: sessionID,
		Params:    json.RawMessage(`{}`),
		Timestamp: schema.NowTimestamp(),
		OK:        true,
		EventType: eventType,
		Payload:   payload,
	}
}

// acpOutputRequestMsg output_request 出站消息（event_type=None 对齐 Python）。
func acpOutputRequestMsg(requestID, sessionID string, rawFrame map[string]any) *schema.Message {
	return acpEventMsg(requestID, sessionID, "", map[string]any{
		"event_type": "acp.output_request", "jsonrpc": rawFrame,
	})
}

// acpDispatchRecorder dispatch 回调记录器（channel 同步：分派在读循环 goroutine）。
type acpDispatchRecorder struct {
	ch chan *schema.Message
}

// newAcpDispatchRecorder 构造记录器。
func newAcpDispatchRecorder() *acpDispatchRecorder {
	return &acpDispatchRecorder{ch: make(chan *schema.Message, 16)}
}

// callback 返回 on_message 回调。
func (r *acpDispatchRecorder) callback() func(ctx context.Context, msg *schema.Message) error {
	return func(ctx context.Context, msg *schema.Message) error {
		select {
		case r.ch <- msg:
		default:
		}
		return nil
	}
}

// next 取下一条（2s 超时 Fatal）。
func (r *acpDispatchRecorder) next(t *testing.T) *schema.Message {
	t.Helper()
	select {
	case msg := <-r.ch:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("on_message dispatch not invoked")
		return nil
	}
}

// sendPromptAndWaitReqID 发 prompt 帧并等分派（返回派生请求键）。
func (a *acpTestServer) sendPromptAndWaitReqID(t *testing.T, conn *websocket.Conn, rpcID any, sessionID, text, userMessageID string, rec *acpDispatchRecorder) string {
	t.Helper()
	params := map[string]any{"sessionId": sessionID, "text": text}
	if userMessageID != "" {
		params["messageId"] = userMessageID
	}
	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": rpcID, "method": "session/prompt", "params": params,
	})
	return rec.next(t).ID
}

// assertNoFrame 短超时断言无帧到达。
// ⚠️ gorilla/websocket 陷阱：ReadMessage 一旦出错（含 deadline 超时），
// 连接 readErr 粘性——后续 ReadMessage 恒立即失败。故本断言只能作为
// 连接上最后一次读使用；需要后续继续读的测试改用 startFrameCollector。
func assertNoFrame(t *testing.T, conn *websocket.Conn, d time.Duration, msg string) {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(d))
	if _, _, err := conn.ReadMessage(); err == nil {
		t.Fatal(msg)
	}
}

// startFrameCollector 起后台读 goroutine 收帧（绕过 gorilla 超时粘性）。
// 返回帧 channel；stop 等待 goroutine 退出（防止 -race 报泄漏）。
func startFrameCollector(t *testing.T, conn *websocket.Conn) (<-chan map[string]any, func()) {
	t.Helper()
	ch := make(chan map[string]any, 32)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var f map[string]any
			_ = json.Unmarshal(raw, &f)
			ch <- f
		}
	}()
	return ch, func() {
		_ = conn.Close()
		<-done
	}
}

// nextFrame 从 collector channel 取帧（超时 Fatal）。
func nextFrame(t *testing.T, ch <-chan map[string]any) map[string]any {
	t.Helper()
	select {
	case f := <-ch:
		return f
	case <-time.After(2 * time.Second):
		t.Fatal("frame not received within 2s")
		return nil
	}
}

// expectSilence 断言 channel 在 d 内无帧（超时后收残帧用 drain）。
func expectSilence(t *testing.T, ch <-chan map[string]any, d time.Duration, msg string) {
	t.Helper()
	select {
	case f := <-ch:
		t.Fatalf("%s (got %v)", msg, f)
	case <-time.After(d):
	}
}

// mapValue 取 map 字段（缺失 Fatal）。
func mapValue(t *testing.T, m map[string]any, key string) map[string]any {
	t.Helper()
	v, _ := m[key].(map[string]any)
	if v == nil {
		t.Fatalf("frame[%v] 缺失或非 map: %v", key, m)
	}
	return v
}

// frameUpdateParams 取 params.update。
func frameUpdateParams(f map[string]any) map[string]any {
	params, _ := f["params"].(map[string]any)
	if params == nil {
		return nil
	}
	update, _ := params["update"].(map[string]any)
	return update
}

// frameResult 取 result 字段（map 断言）。
func frameResult(f map[string]any) map[string]any {
	result, _ := f["result"].(map[string]any)
	return result
}

// ──────────────────────────── 例 1-6 基础帧 ────────────────────────────

// TestAcpGateway_InitializeCapabilities 例 1：initialize 返回全量能力。
// Python: test_gateway_server_initialize_returns_current_acp_capabilities (107)。
func TestAcpGateway_InitializeCapabilities(t *testing.T) {
	a := newAcpTestServer(t)
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()
	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": map[string]any{},
	})
	frame := readFrame(t, conn)
	if frame["jsonrpc"] != "2.0" || frame["id"] != float64(1) {
		t.Fatalf("frame header = %v", frame)
	}
	result := mapValue(t, frame, "result")
	if result["protocolVersion"] != float64(1) {
		t.Fatalf("protocolVersion missing: %v", frame)
	}
	agentInfo := mapValue(t, result, "agentInfo")
	if agentInfo["name"] != "jiuwenswarm" {
		t.Errorf("agentInfo.name = %v, want jiuwenswarm（品牌字面沿用）", agentInfo["name"])
	}
	caps := mapValue(t, result, "agentCapabilities")
	if caps["loadSession"] != false {
		t.Errorf("agentCapabilities.loadSession = %v", caps["loadSession"])
	}
	promptCaps := mapValue(t, caps, "promptCapabilities")
	if promptCaps["image"] != false || promptCaps["audio"] != false || promptCaps["embeddedContext"] != false {
		t.Errorf("promptCapabilities = %v", promptCaps)
	}
	sessionCaps := mapValue(t, caps, "sessionCapabilities")
	if _, has := sessionCaps["list"]; !has {
		t.Errorf("sessionCapabilities.list missing: %v", sessionCaps)
	}
	mcpCaps := mapValue(t, caps, "mcpCapabilities")
	if mcpCaps["http"] != false || mcpCaps["sse"] != false {
		t.Errorf("mcpCapabilities = %v", mcpCaps)
	}
	authMethods, has := result["authMethods"]
	if !has {
		t.Fatalf("authMethods missing: %v", result)
	}
	if arr, ok := authMethods.([]any); !ok || len(arr) != 0 {
		t.Errorf("authMethods = %v, want []", authMethods)
	}
}

// TestAcpGateway_SessionLoadRejected 例 2：session/load -32601。
// Python: test_gateway_server_rejects_session_load_when_capability_is_disabled (145)。
func TestAcpGateway_SessionLoadRejected(t *testing.T) {
	a := newAcpTestServer(t)
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()
	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 2, "method": "session/load",
		"params": map[string]any{"sessionId": "sess-load"},
	})
	frame := readFrame(t, conn)
	errObj := mapValue(t, frame, "error")
	if frame["id"] != float64(2) || errObj["code"] != float64(-32601) {
		t.Fatalf("error frame = %v", frame)
	}
	if errObj["message"] != "Method not supported by agent capabilities: session/load" {
		t.Errorf("error message = %v", errObj["message"])
	}
}

// TestAcpGateway_SessionNewAndList 例 3：session/new + session/list。
// Python: test_gateway_server_session_new_and_list_return_known_sessions (175)。
func TestAcpGateway_SessionNewAndList(t *testing.T) {
	a := newAcpTestServer(t)
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()
	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 3, "method": "session/new",
		"params": map[string]any{"sessionId": "sess-known"},
	})
	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 4, "method": "session/list", "params": map[string]any{},
	})
	f1 := readFrame(t, conn)
	if f1["id"] != float64(3) {
		t.Fatalf("frame1 id = %v", f1)
	}
	r1 := mapValue(t, f1, "result")
	if r1["sessionId"] != "sess-known" {
		t.Errorf("session/new result = %v", r1)
	}
	if co, ok := r1["configOptions"].([]any); !ok || len(co) != 0 {
		t.Errorf("configOptions = %v, want []", r1["configOptions"])
	}
	f2 := readFrame(t, conn)
	if f2["id"] != float64(4) {
		t.Fatalf("frame2 id = %v", f2)
	}
	r2 := mapValue(t, f2, "result")
	sessions, _ := r2["sessions"].([]any)
	found := false
	for _, s := range sessions {
		sm, _ := s.(map[string]any)
		if sm["sessionId"] == "sess-known" {
			found = true
		}
	}
	if !found {
		t.Errorf("session/list sessions = %v, want sess-known", r2["sessions"])
	}
}

// TestAcpGateway_SendResponseTargetsRequestClient 例 4：res 帧定向 requestToClient。
// Python: test_gateway_server_send_response_targets_request_client (219)。
func TestAcpGateway_SendResponseTargetsRequestClient(t *testing.T) {
	a := newAcpTestServer(t)
	conn, serverConn := a.dialAcpTracked(t)
	defer conn.Close()
	a.bindAcpRequestClient("req-1", serverConn)

	err := a.server.Send(context.Background(), &schema.Message{
		ID: "req-1", Type: schema.MessageTypeRes, ChannelID: "acp", SessionID: "sess-1",
		Params: json.RawMessage(`{}`), Timestamp: schema.NowTimestamp(), OK: true,
		Payload: map[string]any{"accepted": true},
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	frame := readFrame(t, conn)
	if frame["type"] != "res" || frame["id"] != "req-1" || frame["ok"] != true {
		t.Fatalf("res frame = %v", frame)
	}
	payload := mapValue(t, frame, "payload")
	if payload["accepted"] != true {
		t.Errorf("payload = %v", payload)
	}
}

// TestAcpGateway_SendEventTargetsSessionClient 例 5：event 帧定向 sessionToClient。
// Python: test_gateway_server_send_event_targets_session_client (248)。
func TestAcpGateway_SendEventTargetsSessionClient(t *testing.T) {
	a := newAcpTestServer(t)
	conn, serverConn := a.dialAcpTracked(t)
	defer conn.Close()
	a.bindAcpSessionClient("sess-2", serverConn)

	err := a.server.Send(context.Background(), &schema.Message{
		ID: "req-2", Type: schema.MessageTypeEvent, ChannelID: "acp", SessionID: "sess-2",
		Params: json.RawMessage(`{}`), Timestamp: schema.NowTimestamp(), OK: true,
		Payload: map[string]any{"content": "hello"}, EventType: schema.EventTypeChatDelta,
	})
	if err != nil {
		t.Fatalf("send: %v", err)
	}
	frame := readFrame(t, conn)
	if frame["type"] != "event" || frame["event"] != "chat.delta" {
		t.Fatalf("event frame = %v", frame)
	}
	payload := mapValue(t, frame, "payload")
	if payload["content"] != "hello" || payload["session_id"] != "sess-2" {
		t.Errorf("payload = %v", payload)
	}
}

// TestAcpGateway_LegacySingleRoute 例 6：单路由兜底配置 + output_request 透传。
// Python: test_gateway_server_accepts_legacy_single_route_config_and_session_map (280)。
// Go 侧单路由形态 = GatewayServerConfig{Path, ChannelID} 兜底（applyConfigFallback）。
func TestAcpGateway_LegacySingleRoute(t *testing.T) {
	cfg := NewGatewayServerConfig()
	cfg.Path = "/acp"
	cfg.ChannelID = "acp"
	s := NewGatewayServer(cfg)
	route, ok := cfg.Routes["/acp"]
	if !ok || route == nil {
		t.Fatal("single-route fallback should build /acp route")
	}

	bridge := NewAcpGatewayBridge(func(ctx context.Context, msg *schema.Message) error { return nil },
		WithBindSessionClient(func(sessionID string, ws *websocket.Conn) {}))
	s.SetAcpBridge(bridge)
	s.InstallDefaultAcpHooks()

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.HandleConnection)
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(func() {
		_ = s.Stop()
		httpSrv.Close()
	})

	before := func() map[*websocket.Conn]struct{} {
		s.clientsMu.RLock()
		defer s.clientsMu.RUnlock()
		out := make(map[*websocket.Conn]struct{}, len(s.clients))
		for c := range s.clients {
			out[c] = struct{}{}
		}
		return out
	}
	snapshot := before()
	conn := dialWS(t, httpSrv, "/acp")
	defer conn.Close()
	_ = readFrame(t, conn) // connection.ack
	var serverConn *websocket.Conn
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		now := before()
		for c := range now {
			if _, seen := snapshot[c]; !seen {
				serverConn = c
			}
		}
		if serverConn != nil {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if serverConn == nil {
		t.Fatal("server-side conn not tracked")
	}
	s.clientsMu.Lock()
	s.sessionToClient[[2]string{"acp", "sess-tool"}] = serverConn
	s.clientsMu.Unlock()

	rawFrame := map[string]any{
		"jsonrpc": "2.0", "id": "tool-legacy", "method": "fs/read_text_file",
		"params": map[string]any{"path": "workspace/demo.txt", "sessionId": "sess-tool"},
	}
	if err := s.Send(context.Background(), acpOutputRequestMsg("req-tool", "sess-tool", rawFrame)); err != nil {
		t.Fatalf("send: %v", err)
	}
	frame := readFrame(t, conn)
	if frame["id"] != "tool-legacy" || frame["method"] != "fs/read_text_file" {
		t.Fatalf("legacy single route passthrough = %v", frame)
	}
	params := mapValue(t, frame, "params")
	if params["path"] != "workspace/demo.txt" {
		t.Errorf("params = %v", params)
	}
}

// ──────────────────────────── 例 7-11 pending/透传 ────────────────────────────

// TestAcpGateway_OutputRequestPassthrough 例 7：acp.output_request 逐字段透传 + pending 注册。
// Python: test_gateway_server_passthroughs_acp_output_request_as_raw_jsonrpc (327)。
func TestAcpGateway_OutputRequestPassthrough(t *testing.T) {
	a := newAcpTestServer(t)
	conn, serverConn := a.dialAcpTracked(t)
	defer conn.Close()
	a.bindAcpSessionClient("sess-tool", serverConn)

	rawFrame := map[string]any{
		"jsonrpc": "2.0", "id": "tool-1", "method": "fs/read_text_file",
		"params": map[string]any{"path": "workspace/demo.txt", "sessionId": "sess-tool"},
	}
	if err := a.server.Send(context.Background(), acpOutputRequestMsg("req-tool", "sess-tool", rawFrame)); err != nil {
		t.Fatalf("send: %v", err)
	}
	frame := readFrame(t, conn)
	if frame["id"] != "tool-1" || frame["method"] != "fs/read_text_file" {
		t.Fatalf("raw jsonrpc passthrough frame = %v", frame)
	}
	params := mapValue(t, frame, "params")
	if params["path"] != "workspace/demo.txt" {
		t.Errorf("params = %v", params)
	}
	a.bridge.mu.Lock()
	cnt := a.bridge.pendingRPCCountBySession["sess-tool"]
	_, hasPending := a.bridge.pendingRPCSessionByID["tool-1"]
	a.bridge.mu.Unlock()
	if cnt != 1 || !hasPending {
		t.Errorf("pending registration: count=%d hasEntry=%v", cnt, hasPending)
	}
}

// TestAcpGateway_DeferEndTurnUntilPendingRPC 例 8：pending 未决时 idle 挂起，回流后 end_turn。
// Python: test_gateway_server_defers_end_turn_until_pending_client_rpc_resolves (364)。
// ⚠️ 用 frameCollector：gorilla ReadMessage 超时后 readErr 粘性，
// assertNoFrame 的同步读会使后续 readFrame 全部失败。
func TestAcpGateway_DeferEndTurnUntilPendingRPC(t *testing.T) {
	rec := newAcpDispatchRecorder()
	a := newAcpTestServerOpt(t, rec.callback())
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()
	frames, stop := startFrameCollector(t, conn)
	defer stop()

	reqID := a.sendPromptAndWaitReqID(t, conn, 211, "sess-pending-rpc", "hello", "user-msg-pending-rpc", rec)

	// final → agent_message_chunk（无 idle，不收尾）
	if err := a.server.Send(context.Background(), acpEventMsg(reqID, "sess-pending-rpc", schema.EventTypeChatFinal, map[string]any{"content": "final before client rpc"})); err != nil {
		t.Fatalf("send final: %v", err)
	}
	f1 := nextFrame(t, frames)
	if frameMethodOf(f1) != "session/update" {
		t.Fatalf("frame1 = %v", f1)
	}
	if u := frameUpdateParams(f1); u["sessionUpdate"] != "agent_message_chunk" {
		t.Errorf("frame1 update = %v", u)
	}

	// output_request（event_type=None）→ 透传 + pending 注册
	rawFrame := map[string]any{
		"jsonrpc": "2.0", "id": "tool-pending-2", "method": "fs/write_text_file",
		"params": map[string]any{"path": "workspace/demo.txt", "content": "hello", "sessionId": "sess-pending-rpc"},
	}
	if err := a.server.Send(context.Background(), acpOutputRequestMsg(reqID, "sess-pending-rpc", rawFrame)); err != nil {
		t.Fatalf("send output_request: %v", err)
	}
	f2 := nextFrame(t, frames)
	if f2["id"] != "tool-pending-2" || frameMethodOf(f2) != "fs/write_text_file" {
		t.Fatalf("passthrough frame = %v", f2)
	}
	f2params := mapValue(t, f2, "params")
	if f2params["content"] != "hello" || f2params["sessionId"] != "sess-pending-rpc" {
		t.Errorf("passthrough params = %v", f2params)
	}

	// processing idle → 挂起（无 result 帧）
	if err := a.server.Send(context.Background(), acpEventMsg(reqID, "sess-pending-rpc", schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false})); err != nil {
		t.Fatalf("send idle: %v", err)
	}
	f3 := nextFrame(t, frames)
	if u := frameUpdateParams(f3); u["sessionUpdate"] != "session_info_update" || u["status"] != "idle" {
		t.Fatalf("idle update frame = %v", f3)
	}
	expectSilence(t, frames, 300*time.Millisecond, "should NOT emit result while pending RPC unresolved")

	// 工具响应回流 → dispatch acp.tool_response + deferred end_turn
	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": "tool-pending-2", "result": map[string]any{"ok": true},
	})
	respMsg := rec.next(t)
	if respMsg.ReqMethod != schema.ReqMethodACPToolResponse {
		t.Errorf("dispatched req_method = %v, want acp.tool_response", respMsg.ReqMethod)
	}
	resultFrame := nextFrame(t, frames)
	if resultFrame["id"] != float64(211) {
		t.Fatalf("result frame id = %v", resultFrame)
	}
	if r := frameResult(resultFrame); r["stopReason"] != "end_turn" || r["userMessageId"] != "user-msg-pending-rpc" {
		t.Errorf("result = %v", resultFrame["result"])
	}
}

// TestAcpGateway_ExpiredPendingRPC 例 9：过期 pending 清扫后不阻塞 end_turn。
// Python: test_gateway_server_expired_pending_rpc_does_not_block_end_turn (489)。
// Go 侧 monkeypatch 不可用 → 注入老 createdAt + 手动 sweep（等价 Python timeout=-1
// 时 OutboundIntercept 注册后同调 sweep 立即清过期）。
func TestAcpGateway_ExpiredPendingRPC(t *testing.T) {
	rec := newAcpDispatchRecorder()
	a := newAcpTestServerOpt(t, rec.callback())
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	reqID := a.sendPromptAndWaitReqID(t, conn, 212, "sess-expired-rpc", "hello", "", rec)

	// final → chunk
	if err := a.server.Send(context.Background(), acpEventMsg(reqID, "sess-expired-rpc", schema.EventTypeChatFinal, map[string]any{"content": "final before expired rpc"})); err != nil {
		t.Fatalf("send final: %v", err)
	}
	f1 := readFrame(t, conn)
	if frameMethodOf(f1) != "session/update" {
		t.Fatalf("frame1 = %v", f1)
	}

	// output_request → 透传 + pending 注册
	rawFrame := map[string]any{
		"jsonrpc": "2.0", "id": "tool-expired", "method": "fs/write_text_file",
		"params": map[string]any{"path": "workspace/demo.txt", "content": "hello", "sessionId": "sess-expired-rpc"},
	}
	if err := a.server.Send(context.Background(), acpOutputRequestMsg(reqID, "sess-expired-rpc", rawFrame)); err != nil {
		t.Fatalf("send output_request: %v", err)
	}
	f2 := readFrame(t, conn)
	if f2["id"] != "tool-expired" {
		t.Fatalf("passthrough frame = %v", f2)
	}

	// 回拨 createdAt 至 120s 前 + 手动 sweep（等价 monkeypatch timeout=-1）
	a.bridge.mu.Lock()
	entry, ok := a.bridge.pendingRPCSessionByID["tool-expired"]
	if !ok {
		a.bridge.mu.Unlock()
		t.Fatal("pending entry missing before aging")
	}
	entry.createdAt = nowFloat() - 120.0
	a.bridge.pendingRPCSessionByID["tool-expired"] = entry
	a.bridge.mu.Unlock()
	a.bridge.sweepStalePending()

	// idle → 无 pending → end_turn 直发（不挂起）
	if err := a.server.Send(context.Background(), acpEventMsg(reqID, "sess-expired-rpc", schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false})); err != nil {
		t.Fatalf("send idle: %v", err)
	}
	f3 := readFrame(t, conn)
	if u := frameUpdateParams(f3); u["sessionUpdate"] != "session_info_update" || u["status"] != "idle" {
		t.Fatalf("idle update = %v", f3)
	}
	f4 := readFrame(t, conn)
	if f4["id"] != float64(212) {
		t.Fatalf("result frame = %v", f4)
	}
	if r := frameResult(f4); r["stopReason"] != "end_turn" {
		t.Errorf("result = %v", f4["result"])
	}
}

// TestAcpGateway_ToolResponseForwarded 例 10：回流 → dispatch acp.tool_response 完整参数。
// Python: test_gateway_server_handle_raw_jsonrpc_response_forwards_acp_tool_response (594)。
func TestAcpGateway_ToolResponseForwarded(t *testing.T) {
	rec := newAcpDispatchRecorder()
	a := newAcpTestServerOpt(t, rec.callback())
	conn, serverConn := a.dialAcpTracked(t)
	defer conn.Close()
	a.bindAcpSessionClient("sess-tool", serverConn)

	rawFrame := map[string]any{
		"jsonrpc": "2.0", "id": "tool-1", "method": "fs/read_text_file",
		"params": map[string]any{"path": "workspace/demo.txt", "sessionId": "sess-tool"},
	}
	if err := a.server.Send(context.Background(), acpOutputRequestMsg("req-tool", "sess-tool", rawFrame)); err != nil {
		t.Fatalf("send: %v", err)
	}
	f := readFrame(t, conn) // 透传帧
	if f["id"] != "tool-1" {
		t.Fatalf("passthrough = %v", f)
	}

	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": "tool-1", "result": map[string]any{"content": "hello"},
	})
	msg := rec.next(t)
	if msg.ChannelID != "acp" || msg.SessionID != "sess-tool" {
		t.Errorf("msg channel/session = %v/%v", msg.ChannelID, msg.SessionID)
	}
	if msg.ReqMethod != schema.ReqMethodACPToolResponse {
		t.Errorf("req_method = %v, want acp.tool_response", msg.ReqMethod)
	}
	var params map[string]any
	_ = json.Unmarshal(msg.Params, &params)
	if params["jsonrpc_id"] != "tool-1" {
		t.Errorf("params.jsonrpc_id = %v", params["jsonrpc_id"])
	}
	resp, _ := params["response"].(map[string]any)
	if resp == nil || resp["result"] == nil {
		t.Errorf("params.response = %v", params["response"])
	}
	if params["session_id"] != "sess-tool" {
		t.Errorf("params.session_id = %v", params["session_id"])
	}
}

// TestAcpGateway_UnknownResponseSoftIgnored 例 11：未知 id → 软忽略帧，无 dispatch。
// Python: test_gateway_server_handle_unknown_jsonrpc_response_is_soft_ignored (655)。
func TestAcpGateway_UnknownResponseSoftIgnored(t *testing.T) {
	rec := newAcpDispatchRecorder()
	a := newAcpTestServerOpt(t, rec.callback())
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": "tool-unknown", "result": map[string]any{"content": "late"},
	})
	frame := readFrame(t, conn)
	if frame["type"] != "res" || frame["id"] != "tool-unknown" || frame["ok"] != true {
		t.Fatalf("soft-ignore frame = %v", frame)
	}
	payload := mapValue(t, frame, "payload")
	if payload["accepted"] != false || payload["ignored"] != true || payload["reason"] != "unknown_or_late_response" {
		t.Errorf("payload = %v", payload)
	}
	select {
	case msg := <-rec.ch:
		t.Fatalf("unexpected dispatch: %v", msg.ID)
	default:
	}
}

// ──────────────────────────── 例 12-17 流式/end_turn ────────────────────────────

// TestAcpGateway_PromptStreamsJsonrpc 例 12：prompt → dispatch chat.send + 三事件 → 帧流。
// Python: test_gateway_server_handles_acp_jsonrpc_prompt_and_streams_back_jsonrpc (693)。
// 注意：final text == delta text → 增量后缀为空 → final 不再发 update（三态）。
func TestAcpGateway_PromptStreamsJsonrpc(t *testing.T) {
	rec := newAcpDispatchRecorder()
	var a *acpTestServer
	a = newAcpTestServerOpt(t, func(ctx context.Context, msg *schema.Message) error {
		rec.ch <- msg
		for _, ev := range []struct {
			eventType schema.EventType
			payload   map[string]any
		}{
			{schema.EventTypeChatDelta, map[string]any{"content": "hello from gateway"}},
			{schema.EventTypeChatFinal, map[string]any{"content": "hello from gateway"}},
			{schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}},
		} {
			if err := a.server.Send(ctx, acpEventMsg(msg.ID, msg.SessionID, ev.eventType, ev.payload)); err != nil {
				return err
			}
		}
		return nil
	})
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 99, "method": "session/prompt",
		"params": map[string]any{"sessionId": "sess-jsonrpc", "text": "hello"},
	})
	msg := rec.next(t)
	if msg.ReqMethod != schema.ReqMethodChatSend {
		t.Errorf("req_method = %v, want chat.send", msg.ReqMethod)
	}
	if msg.SessionID != "sess-jsonrpc" {
		t.Errorf("session_id = %v", msg.SessionID)
	}
	var params map[string]any
	_ = json.Unmarshal(msg.Params, &params)
	if params["query"] != "hello" {
		t.Errorf("params.query = %v", params["query"])
	}

	f1 := readFrame(t, conn) // delta chunk
	f2 := readFrame(t, conn) // idle update（final 同文不发 update）
	if frameMethodOf(f1) != "session/update" || frameMethodOf(f2) != "session/update" {
		t.Fatalf("frames = %v %v", f1, f2)
	}
	p1 := mapValue(t, f1, "params")
	if p1["sessionId"] != "sess-jsonrpc" {
		t.Errorf("sessionId = %v", p1["sessionId"])
	}
	if u := frameUpdateParams(f1); u["sessionUpdate"] != "agent_message_chunk" {
		t.Errorf("frame1 update = %v", u)
	}
	if u := frameUpdateParams(f2); u["sessionUpdate"] != "session_info_update" || u["status"] != "idle" {
		t.Errorf("frame2 update = %v", u)
	}
	last := readFrame(t, conn)
	if last["id"] != float64(99) {
		t.Fatalf("result frame = %v", last)
	}
	if r := frameResult(last); r["stopReason"] != "end_turn" {
		t.Errorf("result = %v", last["result"])
	}
}

// TestAcpGateway_FinalNoDeltaEmitsChunk 例 13：final 无 delta → 直发 agent_message_chunk。
// Python: test_gateway_server_emits_agent_message_chunk_from_chat_final_when_no_delta (774)。
func TestAcpGateway_FinalNoDeltaEmitsChunk(t *testing.T) {
	rec := newAcpDispatchRecorder()
	var a *acpTestServer
	a = newAcpTestServerOpt(t, func(ctx context.Context, msg *schema.Message) error {
		rec.ch <- msg
		for _, ev := range []struct {
			eventType schema.EventType
			payload   map[string]any
		}{
			{schema.EventTypeChatFinal, map[string]any{"content": "gateway final only"}},
			{schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}},
		} {
			if err := a.server.Send(ctx, acpEventMsg(msg.ID, msg.SessionID, ev.eventType, ev.payload)); err != nil {
				return err
			}
		}
		return nil
	})
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 199, "method": "session/prompt",
		"params": map[string]any{"sessionId": "sess-gateway-final", "text": "hello"},
	})
	_ = rec.next(t)

	f1 := readFrame(t, conn)
	if frameMethodOf(f1) != "session/update" {
		t.Fatalf("frame1 = %v", f1)
	}
	p1 := mapValue(t, f1, "params")
	if p1["sessionId"] != "sess-gateway-final" {
		t.Errorf("sessionId = %v", p1["sessionId"])
	}
	u1 := frameUpdateParams(f1)
	if u1["sessionUpdate"] != "agent_message_chunk" {
		t.Errorf("frame1 update = %v", u1)
	}
	content, _ := u1["content"].(map[string]any)
	if content == nil || content["text"] != "gateway final only" || content["type"] != "text" {
		t.Errorf("content = %v", u1["content"])
	}
	f2 := readFrame(t, conn)
	if u2 := frameUpdateParams(f2); u2["sessionUpdate"] != "session_info_update" || u2["status"] != "idle" {
		t.Errorf("frame2 update = %v", u2)
	}
	f3 := readFrame(t, conn)
	if f3["id"] != float64(199) {
		t.Fatalf("result frame = %v", f3)
	}
	if r := frameResult(f3); r["stopReason"] != "end_turn" {
		t.Errorf("result = %v", f3["result"])
	}
}

// TestAcpGateway_DeferEndTurnUntilIdle 例 14：final → tool_result → idle 才 end_turn。
// Python: test_gateway_server_defers_end_turn_until_processing_idle_after_final_and_tool_updates (844)。
func TestAcpGateway_DeferEndTurnUntilIdle(t *testing.T) {
	rec := newAcpDispatchRecorder()
	var a *acpTestServer
	a = newAcpTestServerOpt(t, func(ctx context.Context, msg *schema.Message) error {
		rec.ch <- msg
		for _, ev := range []struct {
			eventType schema.EventType
			payload   map[string]any
		}{
			{schema.EventTypeChatDelta, map[string]any{"content": "partial"}},
			{schema.EventTypeChatFinal, map[string]any{"content": "final"}},
			{schema.EventTypeChatToolResult, map[string]any{"tool_name": "read_file", "tool_call_id": "tool-call-9", "result": "still running"}},
			{schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}},
		} {
			if err := a.server.Send(ctx, acpEventMsg(msg.ID, msg.SessionID, ev.eventType, ev.payload)); err != nil {
				return err
			}
		}
		return nil
	})
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 299, "method": "session/prompt",
		"params": map[string]any{"sessionId": "sess-final-tool", "text": "hello"},
	})
	_ = rec.next(t)

	var updates []map[string]any
	for i := 0; i < 4; i++ {
		f := readFrame(t, conn)
		if frameMethodOf(f) != "session/update" {
			t.Fatalf("frame[%d] = %v, want session/update", i, f)
		}
		updates = append(updates, frameUpdateParams(f))
	}
	if updates[0]["sessionUpdate"] != "agent_message_chunk" {
		t.Errorf("update[0] = %v", updates[0])
	}
	if updates[1]["sessionUpdate"] != "agent_message_chunk" {
		t.Errorf("update[1] = %v", updates[1])
	}
	c1, _ := updates[1]["content"].(map[string]any)
	if c1 == nil || c1["text"] != "final" {
		t.Errorf("update[1].content = %v", updates[1]["content"])
	}
	if updates[2]["sessionUpdate"] != "tool_call_update" {
		t.Fatalf("update[2] = %v", updates[2])
	}
	if updates[2]["toolCallId"] != "tool-call-9" || updates[2]["toolName"] != "read_file" {
		t.Errorf("update[2] tool = %v", updates[2])
	}
	if updates[2]["title"] != "Reading data" || updates[2]["kind"] != "read" || updates[2]["status"] != "completed" {
		t.Errorf("update[2] fields = %v", updates[2])
	}
	if updates[2]["result"] != "still running" {
		t.Errorf("update[2].result = %v", updates[2]["result"])
	}
	if updates[3]["sessionUpdate"] != "session_info_update" || updates[3]["status"] != "idle" {
		t.Errorf("update[3] = %v", updates[3])
	}
	last := readFrame(t, conn)
	if last["id"] != float64(299) {
		t.Fatalf("result frame = %v", last)
	}
	if r := frameResult(last); r["stopReason"] != "end_turn" {
		t.Errorf("result = %v", last["result"])
	}
}

// TestAcpGateway_LateFinalSuffixOnly 例 15：idle 后 timer 窗口内 final 只补后缀。
// Python: test_gateway_server_waits_for_late_chat_final_after_idle_and_only_emits_missing_suffix (956)。
// Go 侧 timer 缩短经 WithIdleFinalizeSeconds(0.2)（替代 Python monkeypatch，计划 15b）。
func TestAcpGateway_LateFinalSuffixOnly(t *testing.T) {
	rec := newAcpDispatchRecorder()
	a := newAcpTestServerOpt(t, rec.callback(), WithIdleFinalizeSeconds(func() float64 { return 0.2 }))
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	reqID := a.sendPromptAndWaitReqID(t, conn, 301, "sess-gateway-late-final", "hello", "", rec)

	// delta "partial" → idle（无 final）→ timer 启动
	if err := a.server.Send(context.Background(), acpEventMsg(reqID, "sess-gateway-late-final", schema.EventTypeChatDelta, map[string]any{"content": "partial"})); err != nil {
		t.Fatalf("send delta: %v", err)
	}
	f1 := readFrame(t, conn)
	if u := frameUpdateParams(f1); u["sessionUpdate"] != "agent_message_chunk" {
		t.Fatalf("frame1 update = %v", u)
	}
	midMessageID := frameUpdateParams(f1)["messageId"]

	if err := a.server.Send(context.Background(), acpEventMsg(reqID, "sess-gateway-late-final", schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false})); err != nil {
		t.Fatalf("send idle: %v", err)
	}
	f2 := readFrame(t, conn)
	if u := frameUpdateParams(f2); u["sessionUpdate"] != "session_info_update" || u["status"] != "idle" {
		t.Fatalf("frame2 update = %v", u)
	}

	// timer 窗口内迟到 final "partial full" → 只补 " full" + 收尾
	if err := a.server.Send(context.Background(), acpEventMsg(reqID, "sess-gateway-late-final", schema.EventTypeChatFinal, map[string]any{"content": "partial full"})); err != nil {
		t.Fatalf("send final: %v", err)
	}
	f3 := readFrame(t, conn)
	if u := frameUpdateParams(f3); u["sessionUpdate"] != "agent_message_chunk" {
		t.Fatalf("frame3 update = %v", u)
	}
	c3, _ := frameUpdateParams(f3)["content"].(map[string]any)
	if c3 == nil || c3["text"] != " full" {
		t.Errorf("late final suffix = %v, want ' full'", c3)
	}
	if frameUpdateParams(f3)["messageId"] != midMessageID {
		t.Errorf("messageId mismatch: %v vs %v", frameUpdateParams(f3)["messageId"], midMessageID)
	}
	last := readFrame(t, conn)
	if last["id"] != float64(301) {
		t.Fatalf("result frame = %v", last)
	}
	if r := frameResult(last); r["stopReason"] != "end_turn" {
		t.Errorf("result = %v", last["result"])
	}
}

// TestAcpGateway_PromptResultEchoesUserMessageID 例 16：result 帧 userMessageId 回显。
// Python: test_gateway_server_prompt_result_echoes_user_message_id (1064)。
func TestAcpGateway_PromptResultEchoesUserMessageID(t *testing.T) {
	rec := newAcpDispatchRecorder()
	var a *acpTestServer
	a = newAcpTestServerOpt(t, func(ctx context.Context, msg *schema.Message) error {
		rec.ch <- msg
		for _, ev := range []struct {
			eventType schema.EventType
			payload   map[string]any
		}{
			{schema.EventTypeChatFinal, map[string]any{"content": "gateway final"}},
			{schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}},
		} {
			if err := a.server.Send(ctx, acpEventMsg(msg.ID, msg.SessionID, ev.eventType, ev.payload)); err != nil {
				return err
			}
		}
		return nil
	})
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 300, "method": "session/prompt",
		"params": map[string]any{"sessionId": "sess-user-message-id", "messageId": "user-msg-1", "text": "hello"},
	})
	_ = rec.next(t)

	_ = readFrame(t, conn) // final chunk
	_ = readFrame(t, conn) // idle update
	last := readFrame(t, conn)
	if last["id"] != float64(300) {
		t.Fatalf("result frame = %v", last)
	}
	if r := frameResult(last); r["userMessageId"] != "user-msg-1" || r["stopReason"] != "end_turn" {
		t.Errorf("result = %v", last["result"])
	}
}

// TestAcpGateway_NoEndTurnBeforeLateToolResult 例 17：final 单独不收尾，idle 后 end_turn。
// Python: test_gateway_server_does_not_end_turn_from_chat_final_before_late_tool_result (1126)。
// Go 侧 timer 缩短 WithIdleFinalizeSeconds(0.05)（替代 Python monkeypatch 0.01，计划 17b）。
func TestAcpGateway_NoEndTurnBeforeLateToolResult(t *testing.T) {
	rec := newAcpDispatchRecorder()
	var a *acpTestServer
	a = newAcpTestServerOpt(t, func(ctx context.Context, msg *schema.Message) error {
		rec.ch <- msg
		for _, ev := range []struct {
			eventType schema.EventType
			payload   map[string]any
		}{
			{schema.EventTypeChatFinal, map[string]any{"content": "final answer"}},
			{schema.EventTypeChatToolResult, map[string]any{"tool_name": "write_text_file", "tool_call_id": "tool-call-late-1", "result": "index.html written"}},
			{schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}},
		} {
			if err := a.server.Send(ctx, acpEventMsg(msg.ID, msg.SessionID, ev.eventType, ev.payload)); err != nil {
				return err
			}
		}
		return nil
	}, WithIdleFinalizeSeconds(func() float64 { return 0.05 }))
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 399, "method": "session/prompt",
		"params": map[string]any{"sessionId": "sess-idle-fallback", "text": "hello"},
	})
	_ = rec.next(t)

	f1 := readFrame(t, conn)
	if u := frameUpdateParams(f1); u["sessionUpdate"] != "agent_message_chunk" {
		t.Fatalf("frame1 update = %v", u)
	}
	f2 := readFrame(t, conn)
	if u := frameUpdateParams(f2); u["sessionUpdate"] != "tool_call_update" {
		t.Fatalf("frame2 update = %v", u)
	}
	if u := frameUpdateParams(f2); u["toolCallId"] != "tool-call-late-1" || u["toolName"] != "write_text_file" {
		t.Errorf("frame2 tool = %v", u)
	}
	if u := frameUpdateParams(f2); u["title"] != "Editing files" || u["kind"] != "edit" || u["status"] != "completed" {
		t.Errorf("frame2 fields = %v", u)
	}
	if u := frameUpdateParams(f2); u["result"] != "index.html written" {
		t.Errorf("frame2 result = %v", u["result"])
	}
	f3 := readFrame(t, conn)
	if u := frameUpdateParams(f3); u["sessionUpdate"] != "session_info_update" || u["status"] != "idle" {
		t.Fatalf("frame3 update = %v", u)
	}
	last := readFrame(t, conn)
	if last["id"] != float64(399) {
		t.Fatalf("result frame = %v", last)
	}
	if r := frameResult(last); r["stopReason"] != "end_turn" {
		t.Errorf("result = %v", last["result"])
	}
}

// TestAcpGateway_ReasoningThenIdle 例 18：reasoning → agent_thought_chunk；idle → end_turn。
// Python: test_gateway_server_emits_direct_reasoning_update_then_waits_for_idle (1221)。
func TestAcpGateway_ReasoningThenIdle(t *testing.T) {
	rec := newAcpDispatchRecorder()
	var a *acpTestServer
	a = newAcpTestServerOpt(t, func(ctx context.Context, msg *schema.Message) error {
		rec.ch <- msg
		for _, ev := range []struct {
			eventType schema.EventType
			payload   map[string]any
		}{
			{schema.EventTypeChatReasoning, map[string]any{"content": "reasoning step", "event_type": "chat.reasoning"}},
			{schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false}},
		} {
			if err := a.server.Send(ctx, acpEventMsg(msg.ID, msg.SessionID, ev.eventType, ev.payload)); err != nil {
				return err
			}
		}
		return nil
	})
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	sendAcpJSON(t, conn, map[string]any{
		"jsonrpc": "2.0", "id": 401, "method": "session/prompt",
		"params": map[string]any{"sessionId": "sess-reasoning-gateway", "text": "hello"},
	})
	_ = rec.next(t)

	f1 := readFrame(t, conn)
	if u := frameUpdateParams(f1); u["sessionUpdate"] != "agent_thought_chunk" {
		t.Fatalf("frame1 update = %v", u)
	}
	c1, _ := frameUpdateParams(f1)["content"].(map[string]any)
	if c1 == nil || c1["text"] != "reasoning step" {
		t.Errorf("content = %v", frameUpdateParams(f1)["content"])
	}
	f2 := readFrame(t, conn)
	if u := frameUpdateParams(f2); u["sessionUpdate"] != "session_info_update" || u["status"] != "idle" {
		t.Errorf("frame2 update = %v", u)
	}
	last := readFrame(t, conn)
	if last["id"] != float64(401) {
		t.Fatalf("result frame = %v", last)
	}
	if r := frameResult(last); r["stopReason"] != "end_turn" {
		t.Errorf("result = %v", last["result"])
	}
}

// TestAcpGateway_TodoThenIdle 例 19：todo.updated → todo_update 帧。
// Python: test_gateway_server_emits_todo_update_then_waits_for_idle (1290)。
func TestAcpGateway_TodoThenIdle(t *testing.T) {
	rec := newAcpDispatchRecorder()
	a := newAcpTestServerOpt(t, rec.callback())
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	reqID := a.sendPromptAndWaitReqID(t, conn, 402, "sess-todo-gateway", "hello", "", rec)

	todos := []any{map[string]any{
		"id": "todo-1", "content": "Implement ACP todo update", "activeForm": "Implementing ACP todo update",
		"status": "in_progress", "createdAt": "2026-04-16T00:00:00Z", "updatedAt": "2026-04-16T00:05:00Z",
	}}
	if err := a.server.Send(context.Background(), acpEventMsg(reqID, "sess-todo-gateway", schema.EventTypeTodoUpdated, map[string]any{"todos": todos})); err != nil {
		t.Fatalf("send todo: %v", err)
	}
	if err := a.server.Send(context.Background(), acpEventMsg(reqID, "sess-todo-gateway", schema.EventTypeChatProcessingStatus, map[string]any{"is_processing": false})); err != nil {
		t.Fatalf("send idle: %v", err)
	}

	f1 := readFrame(t, conn)
	u1 := frameUpdateParams(f1)
	if u1["sessionUpdate"] != "todo_update" {
		t.Fatalf("frame1 update = %v", u1)
	}
	tds, _ := u1["todos"].([]any)
	if len(tds) != 1 {
		t.Fatalf("todos = %v", u1["todos"])
	}
	td, _ := tds[0].(map[string]any)
	if td["id"] != "todo-1" || td["content"] != "Implement ACP todo update" || td["status"] != "in_progress" {
		t.Errorf("todo = %v", td)
	}
	if td["activeForm"] != "Implementing ACP todo update" || td["createdAt"] != "2026-04-16T00:00:00Z" || td["updatedAt"] != "2026-04-16T00:05:00Z" {
		t.Errorf("todo fields = %v", td)
	}
	f2 := readFrame(t, conn)
	if u2 := frameUpdateParams(f2); u2["sessionUpdate"] != "session_info_update" || u2["status"] != "idle" {
		t.Errorf("frame2 update = %v", u2)
	}
	last := readFrame(t, conn)
	if last["id"] != float64(402) {
		t.Fatalf("result frame = %v", last)
	}
	if r := frameResult(last); r["stopReason"] != "end_turn" {
		t.Errorf("result = %v", last["result"])
	}
}

// TestAcpGateway_DeltaOnlyNoFinalize 例 20：delta 后无 finalize（timer 未启动）。
// Python: test_gateway_server_delta_only_does_not_trigger_idle_finalize (1378)。
func TestAcpGateway_DeltaOnlyNoFinalize(t *testing.T) {
	rec := newAcpDispatchRecorder()
	a := newAcpTestServerOpt(t, rec.callback(), WithIdleFinalizeSeconds(func() float64 { return 0.05 }))
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	reqID := a.sendPromptAndWaitReqID(t, conn, 400, "sess-delta-only", "hello", "", rec)

	if err := a.server.Send(context.Background(), acpEventMsg(reqID, "sess-delta-only", schema.EventTypeChatDelta, map[string]any{"content": "partial answer"})); err != nil {
		t.Fatalf("send delta: %v", err)
	}
	f1 := readFrame(t, conn)
	if frameMethodOf(f1) != "session/update" {
		t.Fatalf("frame1 = %v", f1)
	}
	p1 := mapValue(t, f1, "params")
	if p1["sessionId"] != "sess-delta-only" {
		t.Errorf("sessionId = %v", p1["sessionId"])
	}
	u1 := frameUpdateParams(f1)
	if u1["sessionUpdate"] != "agent_message_chunk" {
		t.Errorf("update = %v", u1)
	}
	if _, isStr := u1["messageId"].(string); !isStr {
		t.Errorf("messageId = %v, want string", u1["messageId"])
	}
	c1, _ := u1["content"].(map[string]any)
	if c1 == nil || c1["text"] != "partial answer" {
		t.Errorf("content = %v", u1["content"])
	}
	// idle timer 未启动（对齐 Python pending_ctx[0].idle_finalize_task is None）
	a.bridge.mu.Lock()
	pctx := a.bridge.requestCtxByReqID[reqID]
	var timerNil bool
	if pctx != nil {
		timerNil = pctx.idleFinalizeTimerSlot == nil
	}
	a.bridge.mu.Unlock()
	if pctx == nil || !timerNil {
		t.Errorf("idle finalize timer should be nil after delta-only")
	}
	assertNoFrame(t, conn, 300*time.Millisecond, "delta-only should not trigger idle finalize")
}

// ──────────────────────────── 例 21-23 路由杂项 ────────────────────────────

// TestAcpGateway_LegacyRequestForwarded 例 21：type=req chat.send → 转发 + acp early-return。
// Python: test_gateway_server_handle_raw_message_forwards_request (1434)。
func TestAcpGateway_LegacyRequestForwarded(t *testing.T) {
	rec := newAcpDispatchRecorder()
	a := newAcpTestServerOpt(t, rec.callback())
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	sendAcpJSON(t, conn, map[string]any{
		"type": "req", "id": "req-3", "method": "chat.send",
		"params": map[string]any{"session_id": "sess-3", "content": "hello", "mode": "agent.fast"},
	})
	msg := rec.next(t)
	if msg.ID != "req-3" || msg.ChannelID != "acp" || msg.SessionID != "sess-3" {
		t.Fatalf("msg = %v %v %v", msg.ID, msg.ChannelID, msg.SessionID)
	}
	if msg.ReqMethod != schema.ReqMethodChatSend {
		t.Errorf("req_method = %v", msg.ReqMethod)
	}
	var params map[string]any
	_ = json.Unmarshal(msg.Params, &params)
	if params["content"] != "hello" {
		t.Errorf("params.content = %v", params["content"])
	}
	if msg.Mode != schema.ModeAgentFast {
		t.Errorf("mode = %v, want agent.fast", msg.Mode)
	}
	assertNoFrame(t, conn, 300*time.Millisecond, "acp early-return should not ack")
}

// TestAcpGateway_UnknownMethodRejected 例 22：type=req unknown.x → error 帧。
// Python: test_gateway_server_handle_raw_message_rejects_unknown_method (1473)。
func TestAcpGateway_UnknownMethodRejected(t *testing.T) {
	a := newAcpTestServer(t)
	conn, _ := a.dialAcpTracked(t)
	defer conn.Close()

	sendAcpJSON(t, conn, map[string]any{
		"type": "req", "id": "req-4", "method": "unknown.method",
		"params": map[string]any{"session_id": "sess-4"},
	})
	frame := readFrame(t, conn)
	if frame["type"] != "res" || frame["id"] != "req-4" || frame["ok"] != false {
		t.Fatalf("error frame = %v", frame)
	}
	if frame["error"] != "unknown method: unknown.method" {
		t.Errorf("error = %v", frame["error"])
	}
}

// TestAcpGateway_SessionRouteByChannel 例 23：同 session 不同 channel 定向隔离。
// Python: test_gateway_server_send_event_routes_same_session_id_by_channel (1499)。
func TestAcpGateway_SessionRouteByChannel(t *testing.T) {
	cfg := NewGatewayServerConfig()
	cfg.Routes["/acp"] = &RouteConfig{
		Path: "/acp", ChannelID: "acp",
		ForwardMethods:               map[string]bool{"chat.send": true},
		ForwardNoLocalHandlerMethods: map[string]bool{"chat.send": true},
	}
	cfg.Routes["/tui"] = &RouteConfig{
		Path: "/tui", ChannelID: "tui",
		ForwardMethods:               map[string]bool{"chat.send": true},
		ForwardNoLocalHandlerMethods: map[string]bool{"chat.send": true},
	}
	s := NewGatewayServer(cfg)
	bridge := NewAcpGatewayBridge(func(ctx context.Context, msg *schema.Message) error { return nil },
		WithBindSessionClient(func(sessionID string, ws *websocket.Conn) {}))
	s.SetAcpBridge(bridge)
	s.InstallDefaultAcpHooks()
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.HandleConnection)
	httpSrv := httptest.NewServer(mux)
	t.Cleanup(func() {
		_ = s.Stop()
		httpSrv.Close()
	})

	snapshot := func() map[*websocket.Conn]struct{} {
		s.clientsMu.RLock()
		defer s.clientsMu.RUnlock()
		out := make(map[*websocket.Conn]struct{}, len(s.clients))
		for c := range s.clients {
			out[c] = struct{}{}
		}
		return out
	}
	dialTracked := func(path string) (*websocket.Conn, *websocket.Conn) {
		before := snapshot()
		conn := dialWS(t, httpSrv, path)
		_ = readFrame(t, conn) // connection.ack
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			now := snapshot()
			for c := range now {
				if _, seen := before[c]; !seen {
					return conn, c
				}
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("server-side conn not tracked for %s", path)
		return nil, nil
	}
	acpClient, acpServer := dialTracked("/acp")
	defer acpClient.Close()
	tuiClient, tuiServer := dialTracked("/tui")
	defer tuiClient.Close()

	s.clientsMu.Lock()
	s.sessionToClient[[2]string{"acp", "shared-session"}] = acpServer
	s.sessionToClient[[2]string{"tui", "shared-session"}] = tuiServer
	s.clientsMu.Unlock()

	// tui 渠道消息只定向 tui 连接
	if err := s.Send(context.Background(), &schema.Message{
		ID: "req-cli", Type: schema.MessageTypeEvent, ChannelID: "tui", SessionID: "shared-session",
		Params: json.RawMessage(`{}`), Timestamp: schema.NowTimestamp(), OK: true,
		Payload: map[string]any{"content": "hello cli"}, EventType: schema.EventTypeChatDelta,
	}); err != nil {
		t.Fatalf("send: %v", err)
	}
	frame := readFrame(t, tuiClient)
	if frame["type"] != "event" || frame["event"] != "chat.delta" {
		t.Fatalf("tui event frame = %v", frame)
	}
	tp := mapValue(t, frame, "payload")
	if tp["content"] != "hello cli" || tp["session_id"] != "shared-session" {
		t.Errorf("payload = %v", tp)
	}
	assertNoFrame(t, acpClient, 300*time.Millisecond, "acp client should not receive tui channel event")
}

// frameMethodOf 取 method 字段。
func frameMethodOf(f map[string]any) string {
	m, _ := f["method"].(string)
	return m
}

// 例 24（chat.resume → interrupt+resume）已在 11.9 web 包实现
// （NormalizeGatewayMessage + web_normalize_test.go：TestNormalizeGatewayMessage_resume转cancel），
// 按计划映射表核对后跳过。
