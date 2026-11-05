//go:build integration

package testhelpers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager/web"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// webRPCTimeout 单次 RPC 等待上限
	webRPCTimeout = 10 * time.Second
	// webRPCReqIDPrefix 测试请求 id 前缀，便于日志定位
	webRPCReqIDPrefix = "itest-req-"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CallWebRPC 经真实 WebSocket 链路调用一个 web RPC 方法并返回 payload。
//
// B1a 删掉了 RPCDispatcher.Dispatch 兼容壳，本地 handler 只经 WebChannel 分发
// （对齐上游 handler 闭包 + register_method），因此集成测试必须走
// httptest + HandleWebSocket 的完整入站链路，不能直接调 handler。
//
// bind 为 nil 时注册裸通道（无回调）；需要热重载等回调时传入带回调的 bind。
// 帧级 ok=false 会折算成 error（携带 code）。
func CallWebRPC(t *testing.T, bind *web.WebHandlersBindParams, method string, params map[string]any) (map[string]any, error) {
	t.Helper()

	wc := web.RegisterWebHandlers(bind)
	if err := wc.Start(t.Context()); err != nil {
		return nil, fmt.Errorf("启动 WebChannel 失败: %w", err)
	}
	t.Cleanup(func() { _ = wc.Stop(t.Context()) })

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wc.HandleWebSocket)
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		return nil, fmt.Errorf("连接测试 WS 失败: %w", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	reqID := webRPCReqIDPrefix + method
	req := map[string]any{"type": "req", "id": reqID, "method": method, "params": params}
	reqData, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}
	if err := conn.WriteMessage(websocket.TextMessage, reqData); err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}

	if err := conn.SetReadDeadline(time.Now().Add(webRPCTimeout)); err != nil {
		return nil, fmt.Errorf("设置读超时失败: %w", err)
	}
	_, respData, err := conn.ReadMessage()
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}
	var frame map[string]any
	if err := json.Unmarshal(respData, &frame); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}
	payload, _ := frame["payload"].(map[string]any)
	if payload == nil {
		payload = map[string]any{}
	}
	if ok, _ := frame["ok"].(bool); !ok {
		errMsg, _ := frame["error"].(string)
		code, _ := frame["code"].(string)
		return payload, fmt.Errorf("%s 失败（code=%s）: %s", method, code, errMsg)
	}
	return payload, nil
}
