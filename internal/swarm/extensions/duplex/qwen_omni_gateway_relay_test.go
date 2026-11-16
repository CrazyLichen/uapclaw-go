package duplex

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/gorilla/websocket"
)

// echoUpstream 收什么回什么的假实时上游。
// Python 侧无对应实现：Python 测试由 websockets mock 承担。
func echoUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			messageType, data, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if err := conn.WriteMessage(messageType, data); err != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return server
}

// TestServeQwenOmniWebsocket_双向中继 对齐 qwen_omni_gateway.py:102-146。
func TestServeQwenOmniWebsocket_双向中继(t *testing.T) {
	upstream := echoUpstream(t)
	wsURL := "ws://" + strings.TrimPrefix(upstream.URL, "http://")
	t.Setenv("QWEN_OMNI_REALTIME_URL", wsURL)
	t.Setenv("QWEN_OMNI_API_KEY", "secret-key-1")
	t.Setenv("QWEN_OMNI_MODEL_NAME", "omni-model")

	proxy := httptest.NewServer(http.HandlerFunc(ServeQwenOmniWebsocket))
	defer proxy.Close()
	header := http.Header{"Authorization": []string{"Bearer browser-token"}}
	conn, resp, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second}).Dial(
		"ws://"+strings.TrimPrefix(proxy.URL, "http://"), header)
	if err != nil {
		t.Fatalf("代理握手失败: %v %v", err, resp)
	}
	defer conn.Close()
	// 浏览器 → 上游 → 浏览器
	if err := conn.WriteMessage(websocket.TextMessage, []byte(`{"type":"ping"}`)); err != nil {
		t.Fatal(err)
	}
	_, data, err := conn.ReadMessage()
	if err != nil || string(data) != `{"type":"ping"}` {
		t.Fatalf("中继应回显: %s %v", data, err)
	}
	// 二进制
	if err := conn.WriteMessage(websocket.BinaryMessage, []byte{1, 2, 3}); err != nil {
		t.Fatal(err)
	}
	_, data, err = conn.ReadMessage()
	if err != nil || len(data) != 3 {
		t.Fatalf("二进制中继: %v", err)
	}
	_ = conn.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
}

// TestServeQwenOmniWebsocket_配置错误 对齐 :107-114。
func TestServeQwenOmniWebsocket_配置错误(t *testing.T) {
	t.Setenv("QWEN_OMNI_REALTIME_URL", "")
	t.Setenv("QWEN_OMNI_API_KEY", "")
	t.Setenv("QWEN_OMNI_MODEL_NAME", "")
	proxy := httptest.NewServer(http.HandlerFunc(ServeQwenOmniWebsocket))
	defer proxy.Close()
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second}).Dial(
		"ws://"+strings.TrimPrefix(proxy.URL, "http://"), nil)
	if err != nil {
		t.Fatalf("代理应接受连接再报错: %v", err)
	}
	defer conn.Close()
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var frame struct {
		Type  string `json:"type"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(data, &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Type != "error" || frame.Error.Code != "qwen_gateway_config_error" ||
		frame.Error.Message != "请配置 QWEN_OMNI_REALTIME_URL" {
		t.Fatalf("配置错误帧不符: %s", data)
	}
}

// TestSafeUpstreamError_脱密钥 对齐 qwen_omni_gateway.py:73-78。
func TestSafeUpstreamError_脱密钥(t *testing.T) {
	err := errorsNew("failed with key secret-key-1 at https://x")
	got := safeUpstreamError(err, "secret-key-1")
	if strings.Contains(got, "secret-key-1") || !strings.Contains(got, "******") {
		t.Fatalf("密钥应打码: %s", got)
	}
	long := strings.Repeat("a", 1200)
	if len(safeUpstreamError(errorsNew(long), "")) != 1000 {
		t.Fatal("应截断至 1000")
	}
}

// TestSendGatewayError 帧结构对齐 :61-71。
func TestSendGatewayError_帧结构(t *testing.T) {
	server := newWSServer(t, func(conn *websocket.Conn) {
		sendGatewayError(&safeConn{conn: conn}, "qwen_gateway_upstream_closed", "连接关闭")
	})
	conn, _, err := (&websocket.Dialer{HandshakeTimeout: 5 * time.Second}).Dial(
		"ws://"+strings.TrimPrefix(server.URL, "http://"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, data, err := conn.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	var frame map[string]any
	if json.Unmarshal(data, &frame) != nil || frame["type"] != "error" {
		t.Fatalf("错误帧不符: %s", data)
	}
	inner := frame["error"].(map[string]any)
	if inner["code"] != "qwen_gateway_upstream_closed" || inner["message"] != "连接关闭" {
		t.Fatalf("错误码/文案不符: %v", inner)
	}
}

// TestTruncateReason_UTF8安全 对齐 :169。
func TestTruncateReason_UTF8安全(t *testing.T) {
	// CJK 截断不得出现无效字节
	reason := strings.Repeat("长", 100)
	got := truncateReason(reason)
	if !utf8.ValidString(got) {
		t.Fatal("截断后必须仍是合法 UTF-8")
	}
	if len(got) > 123 {
		t.Fatalf("应 ≤123 字节: %d", len(got))
	}
}
