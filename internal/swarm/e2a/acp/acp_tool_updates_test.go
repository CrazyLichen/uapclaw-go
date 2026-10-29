package acp

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNormalizeToolName(t *testing.T) {
	cases := map[string]string{
		"free_search":  "mcp_free_search",
		"exec_command": "mcp_exec_command",
		"read_file":    "read_file",
		"":             "",
	}
	for in, want := range cases {
		if got := NormalizeToolName(in); got != want {
			t.Errorf("NormalizeToolName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInferToolKind(t *testing.T) {
	cases := map[string]string{
		"rm":              "delete",
		"rename":          "move",
		"edit_file":       "edit",
		"bash":            "execute",
		"create_terminal": "execute",
		"fetch":           "fetch",
		"grep":            "search",
		"read_file":       "read",
		"ls":              "read",
		"unknown_tool":    "other",
	}
	for in, want := range cases {
		if got := inferToolKind(in); got != want {
			t.Errorf("inferToolKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestBuildAcpToolDescriptor_基本字段(t *testing.T) {
	d := BuildAcpToolDescriptor("read_file", `{"path": "/tmp/a.go"}`,
		WithToolCallID("call-1"), WithStatus("pending"))
	if d["toolCallId"] != "call-1" {
		t.Errorf("toolCallId = %v", d["toolCallId"])
	}
	if d["status"] != "pending" {
		t.Errorf("status = %v", d["status"])
	}
	if d["kind"] != "read" {
		t.Errorf("kind = %v", d["kind"])
	}
	if d["title"] != "Reading tmp/a.go" {
		t.Errorf("title = %v", d["title"])
	}
	// rawInput：JSON 字符串参数被解析为 map
	ri, _ := d["rawInput"].(map[string]any)
	if ri == nil || ri["path"] != "/tmp/a.go" {
		t.Errorf("rawInput = %v", d["rawInput"])
	}
	// locations：path 键命中 _PATH_KEYS
	locs, _ := d["locations"].([]map[string]any)
	if len(locs) != 1 || locs[0]["path"] != "/tmp/a.go" {
		t.Errorf("locations = %v", d["locations"])
	}
}

func TestBuildAcpToolDescriptor_非JSON参数(t *testing.T) {
	d := BuildAcpToolDescriptor("grep", "plain text",
		WithToolCallID("c1"))
	ri, _ := d["rawInput"].(map[string]any)
	if ri == nil || ri["input"] != "plain text" {
		t.Errorf("rawInput 应为 {input: ...}，实际 %v", d["rawInput"])
	}
	if d["kind"] != "search" {
		t.Errorf("kind = %v", d["kind"])
	}
}

func TestBuildAcpToolDescriptor_JSON数组参数(t *testing.T) {
	// Python L308: JSON 解析成功但非 dict → {"input": 原字符串}
	d := BuildAcpToolDescriptor("bash", `["a","b"]`, WithToolCallID("c1"))
	ri, _ := d["rawInput"].(map[string]any)
	if ri == nil || ri["input"] != `["a","b"]` {
		t.Errorf("rawInput 应为 {input: 原串}，实际 %v", d["rawInput"])
	}
}

func TestBuildAcpToolDescriptor_空参数无rawInput(t *testing.T) {
	d := BuildAcpToolDescriptor("bash", "", WithToolCallID("c1"))
	if _, exists := d["rawInput"]; exists {
		t.Error("空参数不应有 rawInput")
	}
	// bash 无 command 参数 → title "Running command"
	if d["title"] != "Running command" {
		t.Errorf("title = %v", d["title"])
	}
}

func TestBuildAcpToolDescriptor_title覆盖(t *testing.T) {
	d := BuildAcpToolDescriptor("read_file", `{}`,
		WithToolCallID("c1"), WithTitle("自定义"), WithKind("other"))
	if d["title"] != "自定义" || d["kind"] != "other" {
		t.Errorf("title/kind 覆盖失败: %v / %v", d["title"], d["kind"])
	}
}

func TestBuildAcpToolDescriptor_terminalId标题(t *testing.T) {
	d := BuildAcpToolDescriptor("wait_for_terminal_exit",
		`{"terminalId": "t1"}`, WithToolCallID("c1"))
	if d["title"] != "Waiting for terminal exit t1" {
		t.Errorf("title = %v", d["title"])
	}
}

func TestBuildAcpToolDescriptor_rawOutput(t *testing.T) {
	d := BuildAcpToolDescriptor("read_file", `{}`,
		WithToolCallID("c1"), WithRawOutput(map[string]any{"content": "x"}))
	if d["rawOutput"] == nil {
		t.Error("rawOutput 应存在")
	}
}

func TestBuildAcpToolDescriptor_paths多位置(t *testing.T) {
	d := BuildAcpToolDescriptor("write_text_file",
		`{"path": "/a/b.txt", "source": "/c/d.txt"}`, WithToolCallID("c1"))
	locs, _ := d["locations"].([]map[string]any)
	if len(locs) != 2 {
		t.Errorf("locations 应含 path+source 两项，实际 %v", d["locations"])
	}
}

func TestIsReasoningEvent(t *testing.T) {
	if !IsReasoningEvent("chat.reasoning", map[string]any{}) {
		t.Error("chat.reasoning 应为 reasoning")
	}
	if !IsReasoningEvent("chat.delta", map[string]any{"source_chunk_type": "llm_reasoning"}) {
		t.Error("source_chunk_type=llm_reasoning 应为 reasoning")
	}
	if IsReasoningEvent("chat.delta", map[string]any{}) {
		t.Error("普通 delta 不是 reasoning")
	}
}

func TestBuildAcpTodoUpdate(t *testing.T) {
	u := BuildAcpTodoUpdate(map[string]any{
		"todos": []any{map[string]any{"id": "1"}},
	})
	if u["sessionUpdate"] != "todo_update" {
		t.Errorf("sessionUpdate = %v", u["sessionUpdate"])
	}
	if BuildAcpTodoUpdate(map[string]any{"todos": "not-iterable"}) != nil {
		t.Error("非可迭代 todos 应返回 nil")
	}
	if BuildAcpTodoUpdate(map[string]any{}) != nil {
		t.Error("无 todos 应返回 nil")
	}
}

func TestBuildAcpToolCallUpdate(t *testing.T) {
	cache := map[string]map[string]any{}
	u := BuildAcpToolCallUpdate(map[string]any{
		"tool_call": map[string]any{
			"id": "c1", "name": "read_file", "arguments": `{"path": "/x"}`,
		},
	}, cache)
	if u == nil {
		t.Fatal("应产出 update")
	}
	tc, _ := u["toolCall"].(map[string]any)
	if tc == nil || tc["id"] != "c1" || tc["name"] != "read_file" {
		t.Errorf("toolCall = %v", u["toolCall"])
	}
	if u["sessionUpdate"] != "tool_call" {
		t.Errorf("sessionUpdate = %v", u["sessionUpdate"])
	}
	if cache["c1"] == nil || cache["c1"]["toolName"] != "read_file" {
		t.Errorf("cache 未写入: %v", cache)
	}
	if BuildAcpToolCallUpdate(map[string]any{}, cache) != nil {
		t.Error("无 tool_call 应返回 nil")
	}
	if BuildAcpToolCallUpdate(map[string]any{
		"tool_call": map[string]any{"name": "x"}, // 无 id
	}, cache) != nil {
		t.Error("无 id 应返回 nil")
	}
}

func TestBuildAcpToolResultUpdate(t *testing.T) {
	cache := map[string]map[string]any{
		"c1": {"toolName": "read_file", "title": "Reading /x", "kind": "read"},
	}
	u := BuildAcpToolResultUpdate(map[string]any{
		"tool_call_id": "c1", "result": "done", "status": "completed",
	}, cache)
	if u["sessionUpdate"] != "tool_call_update" {
		t.Errorf("sessionUpdate = %v", u["sessionUpdate"])
	}
	if u["status"] != "completed" {
		t.Errorf("status = %v", u["status"])
	}
	if u["toolName"] != "read_file" {
		t.Errorf("toolName = %v", u["toolName"])
	}
	// content blocks
	cb, _ := u["content"].([]map[string]any)
	if len(cb) != 1 {
		t.Errorf("content = %v", u["content"])
	}
}

func TestBuildAcpToolResultUpdate_状态推断(t *testing.T) {
	// 无 status：terminal create + terminalId → in_progress
	u := BuildAcpToolResultUpdate(map[string]any{
		"tool_call_id": "c2", "tool_name": "create_terminal",
		"rawOutput": map[string]any{"terminalId": "t9"},
	}, nil)
	if u["status"] != "in_progress" {
		t.Errorf("terminal create status = %v", u["status"])
	}
	// error → failed
	u2 := BuildAcpToolResultUpdate(map[string]any{
		"tool_call_id": "c3", "tool_name": "bash", "error": "boom",
	}, nil)
	if u2["status"] != "failed" {
		t.Errorf("error status = %v", u2["status"])
	}
	// 有 result 无 status → completed
	u3 := BuildAcpToolResultUpdate(map[string]any{
		"tool_call_id": "c4", "tool_name": "bash", "result": "ok",
	}, nil)
	if u3["status"] != "completed" {
		t.Errorf("completed status = %v", u3["status"])
	}
	// 无 result 无 error → in_progress
	u4 := BuildAcpToolResultUpdate(map[string]any{
		"tool_call_id": "c5", "tool_name": "bash",
	}, nil)
	if u4["status"] != "in_progress" {
		t.Errorf("in_progress status = %v", u4["status"])
	}
}

func TestBuildAcpToolResultUpdate_terminalContent(t *testing.T) {
	// terminalId 命中 → terminal content block 替代 result
	u := BuildAcpToolResultUpdate(map[string]any{
		"tool_call_id": "c6", "tool_name": "create_terminal",
		"result": "ignored", "rawOutput": map[string]any{"terminalId": "t1"},
	}, nil)
	if _, hasResult := u["result"]; hasResult {
		t.Error("terminalId 存在时不应有 result 字段")
	}
	cb, _ := u["content"].([]map[string]any)
	if len(cb) != 1 || cb[0]["type"] != "terminal" || cb[0]["terminalId"] != "t1" {
		t.Errorf("content = %v", u["content"])
	}
}

func TestSummarizePath(t *testing.T) {
	if got := summarizePath("/a/b/c/d/e.txt"); got != "c/d/e.txt" {
		t.Errorf("summarizePath = %q", got)
	}
	if got := summarizePath(""); got != "" {
		t.Errorf("空路径 = %q", got)
	}
}

func TestHumanizeToolName(t *testing.T) {
	if got := humanizeToolName("my_tool.name"); got != "My Tool Name" {
		t.Errorf("humanizeToolName = %q", got)
	}
	if got := humanizeToolName(""); got != "Using tool" {
		t.Errorf("空名 = %q", got)
	}
}

func TestBuildToolTitle_分支链(t *testing.T) {
	// Python _summarize_path 会截取末 3 段："/x"→"x"、"/a"→"a"（单段路径丢根斜杠）
	cases := []struct {
		name string
		args map[string]any
		want string
	}{
		{"bash", map[string]any{"command": "ls -la"}, "Running ls -la"},
		{"bash", map[string]any{"path": "/x"}, "Running in x"},
		{"fetch", map[string]any{"url": "https://x"}, "Fetching https://x"},
		{"grep", map[string]any{"query": "foo", "path": "/b"}, "Searching foo in b"},
		{"grep", map[string]any{"query": "foo"}, "Searching foo"},
		{"grep", map[string]any{"path": "/b"}, "Searching in b"},
		{"grep", nil, "Searching"},
		{"edit_file", map[string]any{"path": "/a"}, "Editing a"},
		{"edit_file", nil, "Editing files"},
		{"rm", map[string]any{"path": "/a"}, "Deleting a"},
		{"rm", nil, "Deleting files"},
		{"move", map[string]any{"path": "/a", "destination": "/b"}, "Moving a to b"},
		{"ls", map[string]any{"path": "/a"}, "Listing a"},
		{"ls", nil, "Listing files"},
		{"read_file", map[string]any{"path": "/a"}, "Reading a"},
		{"read_file", nil, "Reading data"},
	}
	for _, tc := range cases {
		if got := buildToolTitle(tc.name, tc.args); got != tc.want {
			t.Errorf("buildToolTitle(%q, %v) = %q, want %q", tc.name, tc.args, got, tc.want)
		}
	}
}

func TestExtractLocations_line与paths(t *testing.T) {
	locs := extractLocations(map[string]any{
		"path": "/a.go", "line": 5, "paths": []any{"/a.go", "/b.go", "/c.go", "/d.go", "/e.go", "/f.go", "/g.go"},
	})
	// paths 截断前 5，其中 /a.go 与 path 键 marker (path,line) 相同被去重 → 共 5 项
	if len(locs) != 5 {
		t.Errorf("locations = %v", locs)
	}
	if locs[0]["line"] != 5 {
		t.Errorf("line = %v", locs[0]["line"])
	}
}

func TestLegacyArguments(t *testing.T) {
	// dict 原样
	m := map[string]any{"a": 1}
	if got, ok := legacyArguments(m).(map[string]any); !ok || got["a"] != 1 {
		t.Errorf("dict 应原样: %v", got)
	}
	// JSON str → map
	if got := legacyArguments(`{"a":1}`); got == nil {
		t.Errorf("JSON str 应转 map: %v", got)
	}
	// 非 JSON 文本 → {input:...}
	got, _ := legacyArguments("text").(map[string]any)
	if got["input"] != "text" {
		t.Errorf("text = %v", got)
	}
	// nil → {}
	if got := legacyArguments(nil); got == nil {
		t.Errorf("nil = %v", got)
	}
}
