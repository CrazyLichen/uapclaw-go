package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 结构体 ────────────────────────────

// resFrameCase 一条 res 帧用例。
type resFrameCase struct {
	// name 用例名
	name string
	// emit 在服务端连接上写一帧
	emit func(conn *websocket.Conn)
	// wantOK 期望的 ok 字段
	wantOK bool
	// wantPayload 期望带 payload 键
	wantPayload bool
	// wantError 期望的 error 文案（空表示不该有 error 键）
	wantError string
	// wantCode 期望的 code
	wantCode string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSendResFrame 校验 res 帧形状：成功帧恒带 payload，错误帧带 error/code。
//
// T-09 删掉 includePayload 参数后，SendResFrame 恒带 "payload": {}（对齐 Python
// send_response 的 `"payload": payload or {}`）；内联错误帧（sendInlineError）
// 仍是四键形状（type/id/ok/error），由它自己构造。
func TestSendResFrame(t *testing.T) {
	server := &GatewayServer{}
	cases := []resFrameCase{
		{
			name: "成功帧带 payload",
			emit: func(conn *websocket.Conn) {
				server.SendResFrame(conn, "req-ok", true, map[string]any{"k": "v"}, "", "")
			},
			wantOK:      true,
			wantPayload: true,
		},
		{
			name: "成功帧 payload 为 nil 时补空对象",
			emit: func(conn *websocket.Conn) {
				server.SendResFrame(conn, "req-nil", true, nil, "", "")
			},
			wantOK:      true,
			wantPayload: true,
		},
		{
			name: "错误帧带 error 与 code",
			emit: func(conn *websocket.Conn) {
				server.SendResFrame(conn, "req-err", false, nil, "boom", "E_BOOM")
			},
			wantOK:      false,
			wantPayload: true,
			wantError:   "boom",
			wantCode:    "E_BOOM",
		},
		{
			name: "错误帧缺 error 文案时补默认值",
			emit: func(conn *websocket.Conn) {
				server.SendResFrame(conn, "req-err2", false, nil, "", "")
			},
			wantOK:      false,
			wantPayload: true,
			wantError:   "request failed",
		},
		{
			name: "内联错误帧不带 payload",
			emit: func(conn *websocket.Conn) {
				server.sendInlineError(conn, "req-inline", "bad request")
			},
			wantOK:      false,
			wantPayload: false,
			wantError:   "bad request",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			frame := roundTripResFrame(t, tc.emit)
			assert.Equal(t, "res", frame["type"])
			assert.Equal(t, tc.wantOK, frame["ok"])
			if tc.wantPayload {
				payload, has := frame["payload"].(map[string]any)
				require.True(t, has, "成功/错误帧都应带 payload 键，实际 %v", frame)
				require.NotNil(t, payload)
			} else {
				_, has := frame["payload"]
				assert.False(t, has, "内联错误帧不应带 payload 键，实际 %v", frame)
			}
			if tc.wantError != "" {
				assert.Equal(t, tc.wantError, frame["error"])
			} else {
				_, has := frame["error"]
				assert.False(t, has, "成功帧不应带 error 键，实际 %v", frame)
			}
			if tc.wantCode != "" {
				assert.Equal(t, tc.wantCode, frame["code"])
			}
		})
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// roundTripResFrame 起一个回环 WS 服务，在服务端写一帧并读出客户端收到的 JSON。
func roundTripResFrame(t *testing.T, emit func(conn *websocket.Conn)) map[string]any {
	t.Helper()
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		emit(conn)
	}))
	defer srv.Close()

	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	_, raw, err := conn.ReadMessage()
	require.NoError(t, err)
	var frame map[string]any
	require.NoError(t, json.Unmarshal(raw, &frame))
	return frame
}
