package a2a

import (
	"encoding/base64"
	"testing"

	sdka2a "github.com/a2aproject/a2a-go/v2/a2a"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestIsCompletionSentinelText 对齐 Python is_completion_sentinel_text (a2a_connect.py L425-428)
func TestIsCompletionSentinelText(t *testing.T) {
	cases := map[string]bool{
		"{'is_complete': True}":   true,
		`{"is_complete": true}`:   true,
		"  {'is_complete':true} ": true,
		`{"is_complete": false}`:  false,
		"is_complete":             false,
		"":                        false,
	}
	for in, want := range cases {
		if got := IsCompletionSentinelText(in); got != want {
			t.Errorf("IsCompletionSentinelText(%q) = %v, 期望 %v", in, got, want)
		}
	}
}

// TestIsTerminalMessage 对齐 Python is_terminal_message (L430-444)
func TestIsTerminalMessage(t *testing.T) {
	res := &schema.Message{Type: schema.MessageTypeRes}
	errEvt := &schema.Message{Type: schema.MessageTypeEvent, EventType: schema.EventTypeChatError}
	interrupt := &schema.Message{Type: schema.MessageTypeEvent, EventType: schema.EventTypeChatInterruptResult}
	complete := &schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatFinal,
		Payload:   map[string]any{"is_complete": true},
	}
	delta := &schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatDelta,
		Payload:   map[string]any{"content": "x"},
	}
	notBool := &schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatFinal,
		Payload:   map[string]any{"is_complete": "true"},
	}

	cases := map[string]struct {
		msg  *schema.Message
		want bool
	}{
		"res":                {res, true},
		"chat.error":         {errEvt, true},
		"interrupt":          {interrupt, true},
		"is_complete":        {complete, true},
		"delta":              {delta, false},
		"is_complete 非 bool": {notBool, false},
		"nil":                {nil, false},
	}
	for name, tc := range cases {
		if got := IsTerminalMessage(tc.msg); got != tc.want {
			t.Errorf("%s: IsTerminalMessage = %v, 期望 %v", name, got, tc.want)
		}
	}
}

// TestMessageToText 对齐 Python message_to_text (L348-357)
func TestMessageToText(t *testing.T) {
	// ① CHAT_ERROR：error 优先
	got := MessageToText(&schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatError,
		Payload:   map[string]any{"error": "boom", "content": "ignored"},
	})
	if got != "boom" {
		t.Errorf("CHAT_ERROR error 分支 = %q, 期望 boom", got)
	}

	// ② CHAT_ERROR：error 空则 content
	got = MessageToText(&schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatError,
		Payload:   map[string]any{"error": "", "content": "from-content"},
	})
	if got != "from-content" {
		t.Errorf("CHAT_ERROR content 分支 = %q, 期望 from-content", got)
	}

	// ③ CHAT_ERROR：都空则兜底文案
	got = MessageToText(&schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatError,
		Payload:   map[string]any{},
	})
	if got != "agent request failed" {
		t.Errorf("CHAT_ERROR 兜底 = %q, 期望 agent request failed", got)
	}

	// ④ 有 content 键取字面
	if got = MessageToText(&schema.Message{Payload: map[string]any{"content": "hello"}}); got != "hello" {
		t.Errorf("content 分支 = %q, 期望 hello", got)
	}

	// ⑤ 非空 payload 整体 JSON 化（偏差 D5：Python str(dict) 形态不同）
	if got = MessageToText(&schema.Message{Payload: map[string]any{"a": float64(1)}}); got != `{"a":1}` {
		t.Errorf("payload 兜底 = %q, 期望 {\"a\":1}", got)
	}

	// ⑥ 空 payload
	if got = MessageToText(&schema.Message{Payload: map[string]any{}}); got != "" {
		t.Errorf("空 payload = %q, 期望空串", got)
	}

	// ⑦ nil 消息
	if got = MessageToText(nil); got != "" {
		t.Errorf("nil 消息 = %q, 期望空串", got)
	}
}

// TestMapA2APartsToParams 对齐 Python map_a2a_parts_to_params (L457-500)
func TestMapA2APartsToParams(t *testing.T) {
	msg := &sdka2a.Message{
		Parts: sdka2a.ContentParts{
			sdka2a.NewTextPart("hello"),
			{Content: sdka2a.URL("https://example.com/test.txt"), Filename: "sample-url.txt", MediaType: "text/plain"},
			{Content: sdka2a.Data{Value: "aGVsbG8gd29ybGQ="}, Filename: "inline.txt", MediaType: "text/plain"},
			{Content: sdka2a.Raw("opaque-bytes")},
		},
	}

	query, files := MapA2APartsToParams(msg)

	if query != "hello" {
		t.Errorf("query = %q, 期望 hello", query)
	}
	if len(files) != 3 {
		t.Fatalf("files 数量 = %d, 期望 3", len(files))
	}
	if files[0]["filename"] != "sample-url.txt" || files[0]["url"] != "https://example.com/test.txt" ||
		files[0]["uri"] != "https://example.com/test.txt" {
		t.Errorf("files[0] = %v", files[0])
	}
	if files[0]["name"] != "sample-url.txt" || files[0]["media_type"] != "text/plain" || files[0]["type"] != "text/plain" {
		t.Errorf("files[0] 冗余键 = %v", files[0])
	}
	if files[1]["data"] != "aGVsbG8gd29ybGQ=" || files[1]["encoding"] != "base64" {
		t.Errorf("files[1] = %v", files[1])
	}
	// raw-only：合成文件名用 Part 原下标
	if files[2]["filename"] != "a2a_part_3" {
		t.Errorf("files[2] filename = %v, 期望 a2a_part_3", files[2]["filename"])
	}
	// 偏差 D4：Python 直接 str(bytes) 会 TypeError，Go 走 base64
	if files[2]["raw"] != base64.StdEncoding.EncodeToString([]byte("opaque-bytes")) {
		t.Errorf("files[2] raw = %v", files[2]["raw"])
	}
}

// TestMapA2APartsToParams_边界 空消息与纯文本
func TestMapA2APartsToParams_边界(t *testing.T) {
	query, files := MapA2APartsToParams(nil)
	if query != "" || files != nil {
		t.Errorf("nil 消息应返回空, 实际 query=%q files=%v", query, files)
	}
	query, files = MapA2APartsToParams(&sdka2a.Message{Parts: sdka2a.ContentParts{
		sdka2a.NewTextPart("a"), sdka2a.NewTextPart("b"),
	}})
	if query != "a\nb" {
		t.Errorf("多文本段应以换行拼接, 实际 %q", query)
	}
	if len(files) != 0 {
		t.Errorf("纯文本不应产生 files, 实际 %v", files)
	}
}

// TestMessageToA2AParts_sentinel过滤 对齐 Python L359-423
func TestMessageToA2AParts_sentinel过滤(t *testing.T) {
	msg := &schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatFinal,
		Payload:   map[string]any{"content": "{'is_complete': True}"},
	}
	if parts := MessageToA2AParts(msg, false); len(parts) != 0 {
		t.Errorf("sentinel 应被过滤, 实际 %d 个 part", len(parts))
	}
}

// TestMessageToA2AParts_工具事件 对齐 Python L384-394
func TestMessageToA2AParts_工具事件(t *testing.T) {
	call := &schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatToolCall,
		Payload:   map[string]any{"tool_call": map[string]any{"name": "view_file"}},
	}
	callParts := MessageToA2AParts(call, false)
	if len(callParts) != 1 || callParts[0].Text() != "[tool_call] view_file" {
		t.Fatalf("tool_call parts = %v", callParts)
	}

	result := &schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatToolResult,
		Payload:   map[string]any{"tool_name": "view_file", "result": "ok"},
	}
	resultParts := MessageToA2AParts(result, false)
	if len(resultParts) != 2 {
		t.Fatalf("tool_result parts 数量 = %d, 期望 2", len(resultParts))
	}
	if resultParts[0].Text() != "ok" || resultParts[1].Text() != "[tool_result:view_file] ok" {
		t.Errorf("tool_result parts = %q / %q", resultParts[0].Text(), resultParts[1].Text())
	}
}

// TestMessageToA2AParts_错误与回退
func TestMessageToA2AParts_错误与回退(t *testing.T) {
	errMsg := &schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatError,
		Payload:   map[string]any{"error": "boom"},
	}
	parts := MessageToA2AParts(errMsg, false)
	if len(parts) != 1 || parts[0].Text() != "boom" {
		t.Fatalf("CHAT_ERROR parts = %v", parts)
	}

	empty := &schema.Message{Type: schema.MessageTypeEvent, EventType: schema.EventTypeChatDelta, Payload: map[string]any{}}
	if got := MessageToA2AParts(empty, false); len(got) != 0 {
		t.Errorf("fallback=false 应为空, 实际 %v", got)
	}
	withText := &schema.Message{Payload: map[string]any{"content": "plain"}}
	got := MessageToA2AParts(withText, true)
	if len(got) != 1 || got[0].Text() != "plain" {
		t.Errorf("fallback=true parts = %v", got)
	}
}

// TestMessageToA2AParts_文件载荷 对齐 Python L396-417
func TestMessageToA2AParts_文件载荷(t *testing.T) {
	msg := &schema.Message{
		Payload: map[string]any{"files": []any{
			map[string]any{"filename": "a.txt", "media_type": "text/plain", "url": "https://x/a.txt"},
			map[string]any{"name": "b.txt", "data": "aGk="},
			"非法项",
		}},
	}
	parts := MessageToA2AParts(msg, false)
	if len(parts) != 2 {
		t.Fatalf("parts 数量 = %d, 期望 2", len(parts))
	}
	if string(parts[0].URL()) != "https://x/a.txt" || parts[0].Filename != "a.txt" || parts[0].MediaType != "text/plain" {
		t.Errorf("url part = %+v", parts[0])
	}
	if parts[1].Filename != "b.txt" {
		t.Errorf("data part filename = %q", parts[1].Filename)
	}
	if d, ok := parts[1].Data().(string); !ok || d != "aGk=" {
		t.Errorf("data part value = %v", parts[1].Data())
	}
}

// TestBuildRequestParams 对齐 Python _build_request_params (L446-455)
func TestBuildRequestParams(t *testing.T) {
	p := BuildRequestParams("hi", nil)
	if len(p) != 1 || p["query"] != "hi" {
		t.Errorf("无 files 时 params = %v", p)
	}
	p = BuildRequestParams("hi", []map[string]any{{"filename": "x.txt"}})
	if _, ok := p["files"]; !ok {
		t.Errorf("有 files 时应带键, 实际 %v", p)
	}
}
