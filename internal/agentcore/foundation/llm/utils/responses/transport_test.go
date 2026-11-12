package responses

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// sseServer 搭一个按给定事件序列输出 SSE 的服务端。
func sseServer(t *testing.T, events []string, hook func(r *http.Request, body map[string]any)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if hook != nil {
			hook(r, body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range events {
			fmt.Fprintln(w, e)
			fmt.Fprintln(w)
		}
		fmt.Fprintln(w, "data: [DONE]")
		fmt.Fprintln(w)
	}))
}

// TestTransportStreamResponse SSE 逐事件交付与鉴权头。
func TestTransportStreamResponse(t *testing.T) {
	events := []string{
		`event: response.output_text.delta` + "\n" + `data: {"type":"response.output_text.delta","delta":"hi"}`,
		`data: {"type":"response.completed","response":{"id":"r1","model":"m","usage":{"input_tokens":1,"output_tokens":2}}}`,
	}
	var gotAuth, gotPath, gotStreamFlag string
	gotStream := false
	srv := sseServer(t, events, func(r *http.Request, body map[string]any) {
		gotAuth = r.Header.Get("Authorization")
		gotPath = r.URL.Path
		if v, ok := body["stream"].(bool); ok {
			gotStream = v
		}
		if sid := r.Header.Get("session_id"); sid != "" {
			gotStreamFlag = sid // 复用变量存 session_id 校验
		}
	})
	defer srv.Close()

	tr := NewTransport(TransportOptions{BaseURL: srv.URL + "/v1", TimeoutSeconds: 5})
	ch, err := tr.StreamResponse(context.Background(), StreamRequest{
		Body:        map[string]any{"model": "m"},
		AccessToken: "tok",
		ModelName:   "m",
		SessionID:   "sess-9",
	})
	if err != nil {
		t.Fatal(err)
	}
	var got []*llmschema.AssistantMessageChunk
	for c := range ch {
		got = append(got, c)
	}
	if len(got) != 2 {
		t.Fatalf("chunk 数: %d", len(got))
	}
	if got[0].Content.String() != "hi" {
		t.Fatalf("首帧: %+v", got[0])
	}
	if got[1].UsageMetadata == nil || got[1].UsageMetadata.TotalTokens != 3 {
		t.Fatalf("终帧 usage: %+v", got[1].UsageMetadata)
	}
	if gotAuth != "Bearer tok" {
		t.Fatalf("auth: %q", gotAuth)
	}
	if gotPath != "/v1/responses" {
		t.Fatalf("path: %q", gotPath)
	}
	if !gotStream {
		t.Fatal("stream=true 未设置")
	}
	if gotStreamFlag != "sess-9" {
		t.Fatalf("session_id 头: %q", gotStreamFlag)
	}
}

// TestTransportHTTPError 400 错误体解析。
func TestTransportHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"error":{"message":"bad request"}}`)
	}))
	defer srv.Close()
	tr := NewTransport(TransportOptions{BaseURL: srv.URL})
	_, err := tr.StreamResponse(context.Background(), StreamRequest{
		Body: map[string]any{"model": "m"}, AccessToken: "tok",
	})
	if err == nil {
		t.Fatal("应报错")
	}
	re, ok := err.(*ResponsesError)
	if !ok || re.StatusCode != 400 || re.Message != "bad request" {
		t.Fatalf("错误: %v", err)
	}

	// HTML 体 → SummarizeProviderErrorText 兜底
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprint(w, "<html><body>oops</body></html>")
	}))
	defer srv2.Close()
	_, err = NewTransport(TransportOptions{BaseURL: srv2.URL}).StreamResponse(context.Background(),
		StreamRequest{Body: map[string]any{"model": "m"}, AccessToken: "tok"})
	if err == nil {
		t.Fatal("应报错")
	}
	if strings.Contains(err.Error(), "<html>") {
		t.Fatalf("HTML 未折叠: %v", err)
	}

	// 空体 → 固定文案
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv3.Close()
	_, err = NewTransport(TransportOptions{BaseURL: srv3.URL}).StreamResponse(context.Background(),
		StreamRequest{Body: map[string]any{"model": "m"}, AccessToken: "tok"})
	if err == nil || !strings.Contains(err.Error(), "status 500") {
		t.Fatalf("固定文案: %v", err)
	}
}

// TestTransportStreamFailed 事件解析失败 → StreamError chunk。
func TestTransportStreamFailed(t *testing.T) {
	srv := sseServer(t, []string{
		`data: {"type":"response.output_text.delta","delta":"x"}`,
		`data: {"type":"response.failed","message":"boom"}`,
	}, nil)
	defer srv.Close()
	tr := NewTransport(TransportOptions{BaseURL: srv.URL})
	ch, err := tr.StreamResponse(context.Background(),
		StreamRequest{Body: map[string]any{"model": "m"}, AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	var sawErr error
	count := 0
	for c := range ch {
		count++
		if c.StreamError != nil {
			sawErr = c.StreamError
		}
	}
	if sawErr == nil || sawErr.Error() != "boom" {
		t.Fatalf("StreamError: %v", sawErr)
	}
	if count != 2 {
		t.Fatalf("chunk 数: %d", count)
	}
}

// TestTransportCreateResponse 流累积为单条消息。
func TestTransportCreateResponse(t *testing.T) {
	srv := sseServer(t, []string{
		`data: {"type":"response.output_text.delta","delta":"he"}`,
		`data: {"type":"response.output_text.delta","delta":"llo"}`,
		`data: {"type":"response.completed","response":{"id":"r1","model":"m","usage":{"input_tokens":1,"output_tokens":2}}}`,
	}, nil)
	defer srv.Close()
	tr := NewTransport(TransportOptions{BaseURL: srv.URL})
	msg, err := tr.CreateResponse(context.Background(),
		StreamRequest{Body: map[string]any{"model": "m"}, AccessToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content.String() != "hello" {
		t.Fatalf("content: %q", msg.Content.String())
	}
	if msg.FinishReason != "stop" || msg.ResponseID != "r1" {
		t.Fatalf("finish/response_id: %q/%q", msg.FinishReason, msg.ResponseID)
	}
	if msg.UsageMetadata == nil || msg.UsageMetadata.TotalTokens != 3 {
		t.Fatalf("usage: %+v", msg.UsageMetadata)
	}
}

// TestTransportClientReuse 同一 Transport 两次请求复用连接。
func TestTransportClientReuse(t *testing.T) {
	srv := sseServer(t, []string{`data: {"type":"response.completed","response":{"id":"r","model":"m"}}`}, nil)
	defer srv.Close()
	tr := NewTransport(TransportOptions{BaseURL: srv.URL})
	for i := 0; i < 2; i++ {
		ch, err := tr.StreamResponse(context.Background(),
			StreamRequest{Body: map[string]any{"model": "m"}, AccessToken: "tok"})
		if err != nil {
			t.Fatal(err)
		}
		for range ch {
		}
	}
	if tr.httpClient == nil {
		t.Fatal("httpClient 未懒建")
	}
	_ = tr.Close()
}

// TestTransportConnectionError 连接失败 → error 返回。
func TestTransportConnectionError(t *testing.T) {
	tr := NewTransport(TransportOptions{BaseURL: "http://127.0.0.1:1"})
	_, err := tr.StreamResponse(context.Background(),
		StreamRequest{Body: map[string]any{"model": "m"}, AccessToken: "tok"})
	if err == nil {
		t.Fatal("应报错")
	}
}

// TestRaiseForHTTPError 直接单测错误链。
func TestRaiseForHTTPError(t *testing.T) {
	if got := raiseForHTTPError(429, []byte(`{"detail":"rate limited"}`)); got.Message != "rate limited" {
		t.Fatalf("detail 分支: %q", got.Message)
	}
	if got := raiseForHTTPError(500, nil); !strings.Contains(got.Message, "status 500") {
		t.Fatalf("固定文案: %q", got.Message)
	}
}
