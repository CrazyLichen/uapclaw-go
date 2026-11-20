package tool

import (
	"strings"
	"testing"
)

// ClientToolStatus 枚举取值（Python: client.py:39-49）。
func TestClientToolStatus_枚举取值(t *testing.T) {
	cases := map[ClientToolStatus]string{
		ClientToolStatusCompleted:  "completed",
		ClientToolStatusError:      "error",
		ClientToolStatusTimeout:    "timeout",
		ClientToolStatusClientGone: "client_gone",
		ClientToolStatusCancelled:  "cancelled",
	}
	for status, want := range cases {
		if string(status) != want {
			t.Fatalf("%q != %q", status, want)
		}
	}
	if len(cases) != 5 {
		t.Fatalf("枚举成员数 %d != 5", len(cases))
	}
}

// 可重试状态集合（Python: client.py:53-57 RETRYABLE_CLIENT_TOOL_STATUSES）。
func TestRetryableClientToolStatuses_集合成员(t *testing.T) {
	if !RetryableClientToolStatuses[ClientToolStatusTimeout] ||
		!RetryableClientToolStatuses[ClientToolStatusClientGone] ||
		!RetryableClientToolStatuses[ClientToolStatusCancelled] {
		t.Fatal("TIMEOUT/CLIENT_GONE/CANCELLED 应可重试")
	}
	if RetryableClientToolStatuses[ClientToolStatusCompleted] ||
		RetryableClientToolStatuses[ClientToolStatusError] {
		t.Fatal("COMPLETED/ERROR 不应可重试")
	}
}

// content 归一（Python: client.py:80-90 _normalize_content）。
func TestNewClientToolResult_content归一(t *testing.T) {
	if got := NewClientToolResult(ClientToolStatusCompleted, nil, false, nil).Content; got != "" {
		t.Fatalf("nil → %v, want \"\"", got)
	}
	blocks := NewClientToolResult(ClientToolStatusCompleted,
		map[string]any{"type": "text", "text": "hi"}, false, nil).Content
	if _, ok := blocks.([]map[string]any); !ok {
		t.Fatalf("单内容块 → %T, want []map[string]any", blocks)
	}
	structured := NewClientToolResult(ClientToolStatusCompleted,
		map[string]any{"type": "unknown", "x": 1}, false, nil).Content
	if s, ok := structured.(string); !ok || !strings.HasPrefix(s, `{"type"`) {
		t.Fatalf("非块 dict → JSON 文本, got %v", structured)
	}
	mixed := NewClientToolResult(ClientToolStatusCompleted, []any{"str"}, false, nil).Content
	if s, ok := mixed.(string); !ok || s != `["str"]` {
		t.Fatalf("list 非全 dict → JSON 文本, got %v", mixed)
	}
	kept := NewClientToolResult(ClientToolStatusCompleted,
		[]any{map[string]any{"type": "text", "text": "a"}}, false, nil).Content
	if _, ok := kept.([]any); !ok {
		t.Fatalf("list 全 dict → 原样, got %T", kept)
	}
}

// Failed 判定（Python: client.py:92-95）。
func TestClientToolResult_Failed(t *testing.T) {
	if NewClientToolResult(ClientToolStatusCompleted, "", true, nil).Failed() != true {
		t.Fatal("is_error=true 应失败")
	}
	if NewClientToolResult(ClientToolStatusTimeout, "", false, nil).Failed() != true {
		t.Fatal("status=timeout 应失败")
	}
	if NewClientToolResult(ClientToolStatusCompleted, "", false, nil).Failed() != false {
		t.Fatal("completed 不失败")
	}
}

// Coerce 分支（Python: client.py:98-127）。
func TestCoerceClientToolResult_分支(t *testing.T) {
	original := NewClientToolResult(ClientToolStatusError, "boom", true, nil)
	if CoerceClientToolResult(original) != original {
		t.Fatal("原值原样返回")
	}
	empty := CoerceClientToolResult(nil)
	if empty.Content != "" || empty.Status != ClientToolStatusCompleted {
		t.Fatalf("nil → 空结果, got %+v", empty)
	}
	text := CoerceClientToolResult("hello")
	if text.Content != "hello" || text.Data != nil {
		t.Fatalf("string → content, got %+v", text)
	}
	list := CoerceClientToolResult([]map[string]any{{"type": "text", "text": "a"}})
	if _, ok := list.Content.([]map[string]any); !ok {
		t.Fatalf("list → content 原样, got %T", list.Content)
	}
	number := CoerceClientToolResult(42)
	if number.Content != "42" || number.Data == nil {
		t.Fatalf("非 dict → JSON 文本 + data, got %+v", number)
	}
	envelope := CoerceClientToolResult(map[string]any{
		"status": "error", "content": "boom", "is_error": true, "data": nil,
	})
	if envelope.Status != ClientToolStatusError || envelope.Content != "boom" || !envelope.IsError {
		t.Fatalf("envelope dict → 校验信封, got %+v", envelope)
	}
	whole := CoerceClientToolResult(map[string]any{"answer": 1, "extra": 2})
	if whole.Data == nil || whole.Content == nil {
		t.Fatalf("其他 dict → JSON 文本 + data, got %+v", whole)
	}
	// Python: {"status":"timeout"} 键全在信封内 → model_validate 成功（data 保持 None）。
	withStatus := CoerceClientToolResult(map[string]any{"status": "timeout"})
	if withStatus.Status != ClientToolStatusTimeout || withStatus.Data != nil {
		t.Fatalf("纯信封 → 校验通过, got %+v", withStatus)
	}
	// Python: 键超出信封 → 其他 dict 分支，仍探测 status（client.py:121-127）。
	withJunk := CoerceClientToolResult(map[string]any{"status": "timeout", "junk": 1})
	if withJunk.Status != ClientToolStatusTimeout || withJunk.Data == nil {
		t.Fatalf("带 status 的其他 dict → 探测状态 + data, got %+v", withJunk)
	}
}

// 信封非法时落到"其他 dict"分支（Python: client.py:118-120 except ValidationError）。
func TestCoerceClientToolResult_非法信封回落(t *testing.T) {
	bad := CoerceClientToolResult(map[string]any{"status": "bogus", "content": 123.5})
	if bad.Status != ClientToolStatusCompleted || bad.Data == nil {
		t.Fatalf("非法信封 → 其他 dict 分支, got %+v", bad)
	}
}

// DefaultClientOutput 成功/失败（Python: client.py:203-225）。
func TestDefaultClientOutput_成功失败(t *testing.T) {
	ok := DefaultClientOutput(NewClientToolResult(ClientToolStatusCompleted, "done", false, nil), "")
	if ok["success"] != true {
		t.Fatalf("成功输出, got %+v", ok)
	}
	data := ok["data"].(map[string]any)
	if data["content"] != "done" {
		t.Fatalf("data[content], got %+v", data)
	}
	fail := DefaultClientOutput(NewClientToolResult(ClientToolStatusTimeout, "late", false, nil), "doc")
	if fail["success"] != false {
		t.Fatalf("失败输出, got %+v", fail)
	}
	fdata := fail["data"].(map[string]any)
	if fdata["status"] != "timeout" || fdata["retryable"] != true {
		t.Fatalf("data[status/retryable], got %+v", fdata)
	}
	if !strings.Contains(fail["error"].(string), "did not return a result in time") {
		t.Fatalf("超时文案, got %v", fail["error"])
	}
}

// 图片内容进 multimodal（Python: client.py:214-217）。
func TestDefaultClientOutput_图片multimodal(t *testing.T) {
	out := DefaultClientOutput(NewClientToolResult(ClientToolStatusCompleted,
		[]map[string]any{{"type": "image", "data_url": "data:image/png;base64,AAA"}}, false, nil), "draw")
	data := out["data"].(map[string]any)
	mm, ok := data["multimodal"].([]map[string]any)
	if !ok || mm[0]["data_url"] != "data:image/png;base64,AAA" || mm[0]["source_path"] != "draw result" {
		t.Fatalf("multimodal, got %+v", data)
	}
}

// splitContent 三形态（Python: client.py:270-302）。
func TestSplitClientContent_形态(t *testing.T) {
	text, images := splitClientContent("plain")
	if text != "plain" || len(images) != 0 {
		t.Fatalf("纯文本, got %q %v", text, images)
	}
	text, images = splitClientContent([]map[string]any{
		{"type": "text", "text": "a"},
		{"type": "image", "data_url": "data:image/png;base64,AAA"},
		{"type": "image_url", "image_url": map[string]any{"url": "data:image/jpeg;base64,BBB"}},
		{"type": "image"},
		{"type": "other"},
	})
	if text == "" || strings.Contains(text, "base64,AAA") {
		t.Fatalf("文本聚合, got %q", text)
	}
	if !strings.Contains(text, "[image omitted: only base64 data images are supported]") {
		t.Fatalf("非法图片占位, got %q", text)
	}
	if len(images) != 2 || images[0] != "data:image/png;base64,AAA" || images[1] != "data:image/jpeg;base64,BBB" {
		t.Fatalf("图片提取, got %v", images)
	}
	base64Form, _ := splitClientContent([]map[string]any{{
		"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/png", "data": "CCC"},
	}})
	_ = base64Form
	_, images2 := splitClientContent([]map[string]any{{
		"type": "image", "source": map[string]any{"type": "base64", "media_type": "image/gif", "data": "DDD"},
	}})
	if len(images2) != 1 || images2[0] != "data:image/gif;base64,DDD" {
		t.Fatalf("base64 source 形态, got %v", images2)
	}
}

// 失败文案四分支（Python: client.py:305-323）。
func TestFailureText_分支(t *testing.T) {
	clientGone := failureText(ClientToolStatusClientGone, "doc", "ctx")
	if !strings.Contains(clientGone, "could not finish") || !strings.HasSuffix(clientGone, "\nctx") {
		t.Fatalf("CLIENT_GONE 文案, got %q", clientGone)
	}
	cancelled := failureText(ClientToolStatusCancelled, "doc", "")
	if !strings.Contains(cancelled, "was cancelled before the client returned") {
		t.Fatalf("CANCELLED 文案, got %q", cancelled)
	}
	err := failureText(ClientToolStatusError, "doc", "oops")
	if err != "oops" {
		t.Fatalf("error 回落文本, got %q", err)
	}
	silent := failureText(ClientToolStatusError, "doc", "")
	if silent != "Client tool 'doc' failed without an error message." {
		t.Fatalf("无错误信息兜底, got %q", silent)
	}
}

// ClientToolUnavailableOutput（Python: client.py:171-177）。
func TestClientToolUnavailableOutput(t *testing.T) {
	out := ClientToolUnavailableOutput(&ClientToolUnavailable{Message: "no doc", Retryable: true})
	if out["success"] != false || out["error"] != "no doc" {
		t.Fatalf("输出, got %+v", out)
	}
	data := out["data"].(map[string]any)
	if data["status"] != "unavailable" || data["retryable"] != true {
		t.Fatalf("data, got %+v", data)
	}
}

// AsClientToolOutput 文本替换（Python: client.py:180-195）。
func TestAsClientToolOutput_文本替换(t *testing.T) {
	result := NewClientToolResult(ClientToolStatusCompleted, "img", false, map[string]any{"x": 1})
	custom := AsClientToolOutput("replaced", result, "draw")
	data := custom["data"].(map[string]any)
	if data["content"] != "replaced" {
		t.Fatalf("成功时替换 content, got %+v", data)
	}
	if data["result"].(map[string]any)["x"] != 1 {
		t.Fatalf("保留 result, got %+v", data)
	}
	fail := AsClientToolOutput("bad", NewClientToolResult(ClientToolStatusError, "", false, nil), "")
	if fail["success"] != false || fail["error"] != "bad" {
		t.Fatalf("失败时替换 error, got %+v", fail)
	}
	passthrough := AsClientToolOutput(map[string]any{"success": true}, result, "")
	if passthrough["success"] != true {
		t.Fatalf("ToolOutput 形态原样, got %+v", passthrough)
	}
}

// ClientTool 基类：默认钩子与 invoke 拒绝（Python: client.py:228-263）。
func TestClientTool_基类(t *testing.T) {
	card := &ToolCard{}
	card.Name = "doc_tool"
	tt := NewClientTool(card, nil, nil)
	if tt.Card().Execution != ToolExecutionClient {
		t.Fatal("卡片缺省改写为 client")
	}
	caller := &ToolCard{}
	caller.Name = "keep"
	caller.Execution = "server"
	NewClientTool(caller, nil, nil)
	if caller.Execution != "server" {
		t.Fatal("调用方卡片不应被改写")
	}
	req, err := tt.BuildRequest(map[string]any{"a": 1}, &ClientToolContext{})
	if err != nil || len(req) != 0 {
		t.Fatalf("默认 build_request 空, got %v %v", req, err)
	}
	out, err := tt.Complete(map[string]any{}, NewClientToolResult(ClientToolStatusCompleted, "ok", false, nil), &ClientToolContext{})
	if err != nil {
		t.Fatalf("默认 complete, got %v", err)
	}
	if out.(map[string]any)["success"] != true {
		t.Fatalf("默认输出, got %+v", out)
	}
	if _, err := tt.Invoke(t.Context(), nil); err == nil ||
		!strings.Contains(err.Error(), "'doc_tool' is a client tool") {
		t.Fatalf("invoke 拒绝, got %v", err)
	}
	ch, err := tt.Stream(t.Context(), nil)
	if err != nil {
		t.Fatalf("stream, got %v", err)
	}
	<-ch
}

// ClientToolContext.SessionID（Python: client.py:152-155）。
func TestClientToolContext_SessionID(t *testing.T) {
	if (&ClientToolContext{}).SessionID() != "" {
		t.Fatal("无会话 → 空串")
	}
}
