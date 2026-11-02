package tui

import (
	"testing"

	"github.com/gorilla/websocket"

	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeChannel 同时实现 RouteServer（注册 + 发帧），替代真实 GatewayServer。
type fakeChannel struct {
	// calls 按调用顺序记录 res 帧
	calls []sentFrame
	// handlers method → 注册的 handler
	handlers map[string]routing.LocalHandlerFunc
	// paths 记录注册用到的 path
	paths []string
}

// sentFrame 一次 SendResFrame 调用的快照。
type sentFrame struct {
	reqID   string
	ok      bool
	payload map[string]any
	errMsg  string
	code    string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SendResFrame 实现 ResponseSender，仅记录调用。
func (f *fakeChannel) SendResFrame(_ *websocket.Conn, reqID string, ok bool, payload map[string]any, errMsg, code string) {
	f.calls = append(f.calls, sentFrame{
		reqID:   reqID,
		ok:      ok,
		payload: payload,
		errMsg:  errMsg,
		code:    code,
	})
}

// RegisterLocalHandler 实现 RouteServer，仅记录注册。
func (f *fakeChannel) RegisterLocalHandler(path, method string, handler routing.LocalHandlerFunc) {
	if f.handlers == nil {
		f.handlers = make(map[string]routing.LocalHandlerFunc)
	}
	f.handlers[method] = handler
	f.paths = append(f.paths, path)
}

// TestForwardSetDiff 两集合差集恰为 5 个 chat 类方法。
// Python: CLI_FORWARD_REQ_METHODS − CLI_FORWARD_NO_LOCAL_HANDLER_METHODS
func TestForwardSetDiff(t *testing.T) {
	diff := make([]string, 0)
	for m := range ForwardReqMethods {
		if !ForwardNoLocalHandlerMethods[m] {
			diff = append(diff, m)
		}
	}
	want := []string{
		"chat.interrupt", "chat.resume", "chat.send",
		"chat.user_answer", "history.get",
	}
	if len(diff) != len(want) {
		t.Fatalf("差集大小 = %d, 期望 5；差集: %v", len(diff), diff)
	}
	set := make(map[string]bool)
	for _, m := range diff {
		set[m] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Errorf("差集缺少 %s", w)
		}
	}
}

// TestForwardSetCounts 集合计数（80/75，机器核对基准）。
// Python: tui_connect.py L101-197 / L199-329
func TestForwardSetCounts(t *testing.T) {
	if len(ForwardReqMethods) != 80 {
		t.Errorf("ForwardReqMethods = %d 项, 期望 80", len(ForwardReqMethods))
	}
	if len(ForwardNoLocalHandlerMethods) != 75 {
		t.Errorf("ForwardNoLocalHandlerMethods = %d 项, 期望 75", len(ForwardNoLocalHandlerMethods))
	}
}

// TestBuildRouteBinding binding 字段完整性。
func TestBuildRouteBinding(t *testing.T) {
	rb := BuildRouteBinding(RouteParams{})
	if rb.Path != "/tui" {
		t.Errorf("Path = %s, 期望 /tui", rb.Path)
	}
	if rb.ChannelID != "tui" {
		t.Errorf("ChannelID = %s, 期望 tui", rb.ChannelID)
	}
	if rb.DisconnectHandler == nil {
		t.Error("DisconnectHandler 不应为 nil")
	}
	if rb.Install == nil {
		t.Fatal("Install 不应为 nil（TUI 本地 handler 靠它注册）")
	}
	if !rb.ForwardMethods["chat.send"] || rb.ForwardNoLocalHandlerMethods["chat.send"] {
		t.Error("chat.send 应在 forward 集合且不在 no-local 集合")
	}
	// 拦截器与 CleanupHandler 由 InstallDefaultAcpHooks 后装，binding 阶段应为 nil
	// （对齐 Python _build_acp_route_binding / build_cli_route_binding 均不设这两个）
	if rb.InboundInterceptor != nil || rb.OutboundInterceptor != nil {
		t.Error("binding 阶段不应预装拦截器（由 InstallDefaultAcpHooks 后装）")
	}
}

// TestBuildRouteBinding_Install注册5个handler Install 在 RouteServer 上注册 5 个本地 handler。
func TestBuildRouteBinding_Install注册5个handler(t *testing.T) {
	rb := BuildRouteBinding(RouteParams{})
	server := &fakeChannel{}
	rb.Install(server)

	for _, m := range []string{"chat.send", "chat.interrupt", "chat.resume", "chat.user_answer", "history.get"} {
		if _, ok := server.handlers[m]; !ok {
			t.Errorf("Install 后缺少 %s handler", m)
		}
	}
	if len(server.handlers) != 5 {
		t.Errorf("注册 handler 数 = %d, 期望 5", len(server.handlers))
	}
	for _, p := range server.paths {
		if p != "/tui" {
			t.Errorf("注册 path = %s, 期望 /tui", p)
		}
	}
}

// TestBuildRouteBinding_Install非RouteServer时跳过 传入不满足接口的实例时不 panic。
func TestBuildRouteBinding_Install非RouteServer时跳过(t *testing.T) {
	rb := BuildRouteBinding(RouteParams{})
	rb.Install(struct{}{}) // 不满足 RouteServer，跳过且不 panic
}

// TestLocalHandlersAck 5 个本地 handler 的 res 帧内容（走注入的发送器，无需真实 WS）。
func TestLocalHandlersAck(t *testing.T) {
	cases := []struct {
		name       string
		method     string
		params     map[string]any
		want       map[string]any
		absentKeys []string
	}{
		{
			name:   "chat.send",
			method: "chat.send",
			params: map[string]any{},
			want:   map[string]any{"accepted": true, "session_id": "sess-ack"},
		},
		{
			name:   "chat.resume",
			method: "chat.resume",
			params: map[string]any{},
			want:   map[string]any{"accepted": true, "session_id": "sess-ack"},
		},
		{
			name:   "chat.interrupt 带 intent",
			method: "chat.interrupt",
			params: map[string]any{"intent": "cancel"},
			want:   map[string]any{"accepted": true, "session_id": "sess-ack", "intent": "cancel"},
		},
		{
			name:       "chat.interrupt 无 intent",
			method:     "chat.interrupt",
			params:     map[string]any{},
			want:       map[string]any{"accepted": true, "session_id": "sess-ack"},
			absentKeys: []string{"intent"},
		},
		{
			name:   "chat.user_answer 带 request_id",
			method: "chat.user_answer",
			params: map[string]any{"request_id": "req-9"},
			want:   map[string]any{"accepted": true, "session_id": "sess-ack", "request_id": "req-9"},
		},
		{
			name:       "chat.user_answer 无 request_id",
			method:     "chat.user_answer",
			params:     map[string]any{},
			want:       map[string]any{"accepted": true, "session_id": "sess-ack"},
			absentKeys: []string{"request_id"},
		},
		{
			name:   "history.get session_id 覆写 + page_idx",
			method: "history.get",
			params: map[string]any{"session_id": "sess-override", "page_idx": 2},
			want:   map[string]any{"accepted": true, "session_id": "sess-override", "page_idx": 2},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := &fakeChannel{}
			BuildRouteBinding(RouteParams{}).Install(server)

			handler, ok := server.handlers[tc.method]
			if !ok {
				t.Fatalf("缺少 %s handler", tc.method)
			}
			if err := handler(nil, "req-ack-1", tc.params, "sess-ack"); err != nil {
				t.Fatalf("handler 返回错误: %v", err)
			}
			if len(server.calls) != 1 {
				t.Fatalf("SendResFrame 调用次数 = %d, 期望 1", len(server.calls))
			}
			call := server.calls[0]
			if call.reqID != "req-ack-1" || !call.ok {
				t.Errorf("帧头不符: %+v", call)
			}
			for k, v := range tc.want {
				got, has := call.payload[k]
				if !has {
					t.Errorf("payload 缺少 %s", k)
					continue
				}
				if got != v {
					t.Errorf("payload[%s] = %v(%T), 期望 %v(%T)", k, got, got, v, v)
				}
			}
			for _, k := range tc.absentKeys {
				if _, leaked := call.payload[k]; leaked {
					t.Errorf("payload 不应包含 %s", k)
				}
			}
		})
	}
}

// TestDisconnectHandlerNilSafe nil MessageHandler 时安全跳过。
func TestDisconnectHandlerNilSafe(t *testing.T) {
	h := disconnectHandler(nil)
	h(nil, [][2]string{{"tui", "sess-1"}}) // 不应 panic
	h(nil, nil)                            // 空 keys 不应 panic
}
