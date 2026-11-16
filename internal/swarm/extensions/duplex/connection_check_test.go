package duplex

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

// TestCheckSavedConnection_JoyAI分支 对齐 connection_check.py:28-42。
func TestCheckSavedConnection_JoyAI分支(t *testing.T) {
	// 缺配置直接报错（settings 中 provider=joyai 且三项为空）
	setEnvFileForTest("")
	t.Setenv("VIDEO_LIVE_MODE", "joyai")
	t.Setenv("JOYAI_API_BASE", "")
	t.Setenv("JOYAI_MODEL_NAME", "")
	t.Setenv("JOYAI_API_KEY", "")
	if err := CheckSavedConnection(); err == nil || err.Error() != "Configure the JoyAI endpoint, model and API key first." {
		t.Fatalf("缺配置报错不符: %v", err)
	}

	// 模型不在 provider 列表
	model := "joyai-large"
	server := newJoyaiModelsServer(t, "joyai-other") // provider 列表不含已配置模型
	t.Setenv("JOYAI_API_BASE", server.URL)
	t.Setenv("JOYAI_MODEL_NAME", model)
	t.Setenv("JOYAI_API_KEY", "key-1")
	if err := CheckSavedConnection(); err == nil || err.Error() != "The configured model is not listed by the provider." {
		t.Fatalf("模型未列出报错不符: %v", err)
	}
}

// TestCheckSavedConnection_Qwen分支 对齐 connection_check.py:19-27。
func TestCheckSavedConnection_Qwen分支(t *testing.T) {
	setEnvFileForTest("")
	t.Setenv("VIDEO_LIVE_MODE", "realtime")
	t.Setenv("VIDEO_REALTIME_PROVIDER", "qwen_omni")
	t.Setenv("QWEN_OMNI_REALTIME_URL", "ws://"+strings.TrimPrefix(emptyWSServer(t).URL, "http://"))
	t.Setenv("QWEN_OMNI_API_KEY", "k")
	t.Setenv("QWEN_OMNI_MODEL_NAME", "m")
	t.Setenv("QWEN_OMNI_VOICE", "Tina")
	if err := CheckSavedConnection(); err != nil {
		t.Fatalf("session.created 应判定连通, got %v", err)
	}
	// error 事件应拒绝会话
	t.Setenv("QWEN_OMNI_REALTIME_URL", "ws://"+strings.TrimPrefix(errorWSServer(t).URL, "http://"))
	if err := CheckSavedConnection(); err == nil || err.Error() != "The provider rejected the session. Check the model and API key." {
		t.Fatalf("error 事件报错不符: %v", err)
	}
}

// ───────────── 测试辅助（对齐 Python tests/backend/test_connection* 的桩） ─────────────

// newJoyaiModelsServer 起一个 /models 假端点。
// Python 侧无对应实现：Python 测试由 httpx mock 承担，Go 用 httptest。
func newJoyaiModelsServer(t *testing.T, model string) *httptest.Server {
	t.Helper()
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.Header.Get("Authorization") != "Bearer key-1" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": model}}})
	})
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return server
}

// emptyWSServer 首帧回 session.created 的假实时上游。
func emptyWSServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newWSServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteJSON(map[string]any{"type": "session.created"})
	})
}

// errorWSServer 首帧回 error 的假实时上游。
func errorWSServer(t *testing.T) *httptest.Server {
	t.Helper()
	return newWSServer(t, func(conn *websocket.Conn) {
		_ = conn.WriteJSON(map[string]any{"type": "error"})
	})
}

// newWSServer 起一个只处理一次连接的 WS 假上游。
// Python 侧无对应实现：Python 测试由 websockets mock 承担。
func newWSServer(t *testing.T, onConn func(*websocket.Conn)) *httptest.Server {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		onConn(conn)
	}))
	t.Cleanup(server.Close)
	return server
}
