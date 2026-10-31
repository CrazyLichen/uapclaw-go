package gateway

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestTuiForwardSetDiff 测试两集合差集恰为 5 个 chat 类方法。
func TestTuiForwardSetDiff(t *testing.T) {
	diff := make([]string, 0)
	for m := range TuiForwardReqMethods {
		if !TuiForwardNoLocalHandlerMethods[m] {
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

// TestTuiForwardSetCounts 测试集合计数（80/75，机器核对基准）。
func TestTuiForwardSetCounts(t *testing.T) {
	if len(TuiForwardReqMethods) != 80 {
		t.Errorf("TuiForwardReqMethods = %d 项, 期望 80", len(TuiForwardReqMethods))
	}
	if len(TuiForwardNoLocalHandlerMethods) != 75 {
		t.Errorf("TuiForwardNoLocalHandlerMethods = %d 项, 期望 75", len(TuiForwardNoLocalHandlerMethods))
	}
}

// TestBuildTuiRouteBinding 测试 binding 字段完整性 + Install 注册效果。
func TestBuildTuiRouteBinding(t *testing.T) {
	rb := BuildTuiRouteBinding(TuiRouteParams{})
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

	// Install 效果：经 RouteConfig 转换后由 GatewayServer.RegisterLocalHandler 注册
	routes := BuildRouteConfigMap([]*routing.RouteBinding{rb})
	rc := routes["/tui"]
	if rc == nil {
		t.Fatal("BuildRouteConfigMap 未产出 /tui 路由")
	}
	gs := NewGatewayServer(&GatewayServerConfig{Routes: routes})
	rb.Install(gs)
	for _, m := range []string{"chat.send", "chat.interrupt", "chat.resume", "chat.user_answer", "history.get"} {
		if _, ok := rc.LocalHandlers[m]; !ok {
			t.Errorf("Install 后 LocalHandlers 缺少 %s", m)
		}
	}
	if len(rc.LocalHandlers) != 5 {
		t.Errorf("LocalHandlers = %d 个, 期望 5", len(rc.LocalHandlers))
	}
}

// TestTuiLocalHandlersAck 用真实 WS 连接验证 5 个 handler 的 ack 帧。
func TestTuiLocalHandlersAck(t *testing.T) {
	cases := []struct {
		name    string
		handler LocalHandlerFunc
		params  map[string]any
		want    map[string]any // payload 断言（键→期望值）
	}{
		{
			name:    "chat.send",
			handler: tuiChatSendHandler,
			params:  map[string]any{},
			want:    map[string]any{"accepted": true, "session_id": "sess-ack"},
		},
		{
			name:    "chat.resume",
			handler: tuiChatResumeHandler,
			params:  map[string]any{},
			want:    map[string]any{"accepted": true, "session_id": "sess-ack"},
		},
		{
			name:    "chat.interrupt 带 intent",
			handler: tuiChatInterruptHandler,
			params:  map[string]any{"intent": "cancel"},
			want:    map[string]any{"accepted": true, "session_id": "sess-ack", "intent": "cancel"},
		},
		{
			name:    "chat.interrupt 无 intent",
			handler: tuiChatInterruptHandler,
			params:  map[string]any{},
			want:    map[string]any{"accepted": true, "session_id": "sess-ack"},
		},
		{
			name:    "chat.user_answer 带 request_id",
			handler: tuiChatUserAnswerHandler,
			params:  map[string]any{"request_id": "req-9"},
			want:    map[string]any{"accepted": true, "session_id": "sess-ack", "request_id": "req-9"},
		},
		{
			name:    "chat.user_answer 无 request_id",
			handler: tuiChatUserAnswerHandler,
			params:  map[string]any{},
			want:    map[string]any{"accepted": true, "session_id": "sess-ack"},
		},
		{
			name:    "history.get session_id 覆写 + page_idx",
			handler: tuiHistoryGetHandler,
			params:  map[string]any{"session_id": "sess-override", "page_idx": 2},
			want:    map[string]any{"accepted": true, "session_id": "sess-override", "page_idx": float64(2)},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// gorilla 服务器 + 客户端直连
			upgrader := websocket.Upgrader{}
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				ws, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer ws.Close()
				if err := tc.handler(ws, "req-ack-1", tc.params, "sess-ack"); err != nil {
					t.Errorf("handler 返回错误: %v", err)
				}
			}))
			defer srv.Close()

			wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				t.Fatalf("连接失败: %v", err)
			}
			defer conn.Close()

			_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
			_, data, err := conn.ReadMessage()
			if err != nil {
				t.Fatalf("读帧失败: %v", err)
			}
			var frame map[string]any
			if err := json.Unmarshal(data, &frame); err != nil {
				t.Fatalf("帧 JSON 解析失败: %v", err)
			}
			if frame["type"] != "res" || frame["id"] != "req-ack-1" || frame["ok"] != true {
				t.Errorf("帧头不符: %v", frame)
			}
			payload, _ := frame["payload"].(map[string]any)
			for k, v := range tc.want {
				got := payload[k]
				// page_idx JSON 数字解出 float64（已知坑：YAML/JSON 数字双类型）
				if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", v) {
					t.Errorf("payload[%s] = %v, 期望 %v", k, got, v)
				}
			}
			// 无 intent/request_id 时不应出现该键
			if _, has := tc.want["intent"]; !has {
				if _, leaked := payload["intent"]; leaked {
					t.Errorf("payload 不应包含 intent")
				}
			}
			if _, has := tc.want["request_id"]; !has {
				if _, leaked := payload["request_id"]; leaked {
					t.Errorf("payload 不应包含 request_id")
				}
			}
		})
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestTuiDisconnectHandlerNilSafe 测试 nil MessageHandler 时安全跳过。
func TestTuiDisconnectHandlerNilSafe(t *testing.T) {
	h := tuiDisconnectHandler(nil)
	h(nil, [][2]string{{"tui", "sess-1"}}) // 不应 panic
	h(nil, nil)                            // 空 keys 不应 panic
}
