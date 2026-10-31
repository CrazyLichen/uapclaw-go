package a2a

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

const sendMessageBody = `{"jsonrpc":"2.0","id":"t1","method":"SendMessage","params":{"message":{"messageId":"m1","contextId":"c1","role":"ROLE_USER","parts":[{"text":"ping"}]}}}`

// newTestChannel 构造只回一条最终消息的通道
func newTestChannel(t *testing.T) *A2AChannel {
	t.Helper()
	ch := NewA2AChannel(testConfig(19100))
	ch.OnMessage(func(msg *schema.Message) bool {
		ch.pending[msg.ID].ch <- &schema.Message{
			ID: msg.ID, Type: schema.MessageTypeEvent, EventType: schema.EventTypeChatFinal,
			Payload: map[string]any{"content": "pong", "is_complete": true},
		}
		return true
	})
	return ch
}

// postRPC 发送 JSON-RPC 请求（withVersion 控制是否带 A2A-Version 头）
func postRPC(t *testing.T, url, body string, withVersion bool) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader([]byte(body)))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if withVersion {
		req.Header.Set("A2A-Version", "1.0")
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	return resp
}

// readRPCError 解析 JSON-RPC 错误码
func readRPCError(t *testing.T, resp *http.Response) int {
	t.Helper()
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var out struct {
		Error struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("响应解析失败: %v body=%s", err, raw)
	}
	return out.Error.Code
}

// TestHTTP_SendMessage_非流式 端到端：返回 Task(COMPLETED) + artifact
func TestHTTP_SendMessage_非流式(t *testing.T) {
	srv := httptest.NewServer(newTestChannel(t).newMux())
	defer srv.Close()

	resp := postRPC(t, srv.URL+"/a2a", sendMessageBody, true)
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", resp.StatusCode, raw)
	}
	var out struct {
		Result struct {
			Task struct {
				ID     string `json:"id"`
				Status struct {
					State string `json:"state"`
				} `json:"status"`
				Artifacts []struct {
					ArtifactID string `json:"artifactId"`
					Parts      []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"artifacts"`
			} `json:"task"`
		} `json:"result"`
		Error any `json:"error"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("响应解析失败: %v body=%s", err, raw)
	}
	if out.Error != nil {
		t.Fatalf("服务端返回错误: %v", out.Error)
	}
	if out.Result.Task.ID == "" {
		t.Error("task.id 为空")
	}
	if out.Result.Task.Status.State != "TASK_STATE_COMPLETED" {
		t.Errorf("状态 = %q, 期望 TASK_STATE_COMPLETED", out.Result.Task.Status.State)
	}
	if len(out.Result.Task.Artifacts) != 1 || len(out.Result.Task.Artifacts[0].Parts) != 1 {
		t.Fatalf("artifacts = %+v", out.Result.Task.Artifacts)
	}
	if out.Result.Task.Artifacts[0].Parts[0].Text != "pong" {
		t.Errorf("artifact 文本 = %q", out.Result.Task.Artifacts[0].Parts[0].Text)
	}
	if !strings.HasSuffix(out.Result.Task.Artifacts[0].ArtifactID, "_response") {
		t.Errorf("artifactId = %q, 期望以 _response 结尾", out.Result.Task.Artifacts[0].ArtifactID)
	}
}

// TestHTTP_缺少版本头被拒 偏差 D7：缺 A2A-Version 按 0.3 → -32009
func TestHTTP_缺少版本头被拒(t *testing.T) {
	srv := httptest.NewServer(newTestChannel(t).newMux())
	defer srv.Close()

	resp := postRPC(t, srv.URL+"/a2a", sendMessageBody, false)
	if code := readRPCError(t, resp); code != -32009 {
		t.Errorf("错误码 = %d, 期望 -32009", code)
	}
}

// TestHTTP_未知方法 未知方法 → -32601
func TestHTTP_未知方法(t *testing.T) {
	srv := httptest.NewServer(newTestChannel(t).newMux())
	defer srv.Close()

	resp := postRPC(t, srv.URL+"/a2a", `{"jsonrpc":"2.0","id":"t2","method":"NoSuchMethod","params":{}}`, true)
	if code := readRPCError(t, resp); code != -32601 {
		t.Errorf("错误码 = %d, 期望 -32601", code)
	}
}

// TestHTTP_参数非法 缺 message → -32602
func TestHTTP_参数非法(t *testing.T) {
	srv := httptest.NewServer(newTestChannel(t).newMux())
	defer srv.Close()

	resp := postRPC(t, srv.URL+"/a2a", `{"jsonrpc":"2.0","id":"t4","method":"SendMessage","params":{}}`, true)
	if code := readRPCError(t, resp); code != -32602 {
		t.Errorf("错误码 = %d, 期望 -32602", code)
	}
}

// TestHTTP_ExtendedCard 端点返回同一张卡
func TestHTTP_ExtendedCard(t *testing.T) {
	srv := httptest.NewServer(newTestChannel(t).newMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/agent/authenticatedExtendedCard")
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d, body=%s", resp.StatusCode, raw)
	}
	if !strings.Contains(string(raw), `"name":"UapClaw Gateway A2A Server"`) {
		t.Errorf("extended card 内容 = %s", raw)
	}
}

// TestHTTP_流式SSE 帧序：artifactUpdate 早于终态 statusUpdate
func TestHTTP_流式SSE(t *testing.T) {
	srv := httptest.NewServer(newTestChannel(t).newMux())
	defer srv.Close()

	body := `{"jsonrpc":"2.0","id":"t3","method":"SendStreamingMessage","params":{"message":{"messageId":"m3","contextId":"c3","role":"ROLE_USER","parts":[{"text":"ping"}]}}}`
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/a2a", bytes.NewReader([]byte(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	req.Header.Set("A2A-Version", "1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	text := string(raw)

	if !strings.Contains(text, "artifactUpdate") {
		t.Fatalf("SSE 缺 artifactUpdate: %s", text)
	}
	if !strings.Contains(text, "TASK_STATE_COMPLETED") {
		t.Fatalf("SSE 缺终态: %s", text)
	}
	if strings.Index(text, "artifactUpdate") > strings.Index(text, "TASK_STATE_COMPLETED") {
		t.Errorf("artifact 应早于终态: %s", text)
	}
}
