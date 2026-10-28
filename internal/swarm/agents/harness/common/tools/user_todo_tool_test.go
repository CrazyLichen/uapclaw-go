package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cronbridge "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/cron"
)

// TestSerializeTodoItem_RoundTrip 序列化→解析 round-trip 字段一致
func TestSerializeTodoItem_RoundTrip(t *testing.T) {
	todo := &TodoItem{
		ID:          "abc12345",
		Title:       "buy milk",
		Status:      TodoStatusInProgress,
		Priority:    TodoPriorityHigh,
		DueAt:       "2026-11-01T10:00:00Z",
		RemindAt:    "2026-11-01T09:55:00Z",
		CreateAt:    "2026-10-28T00:00:00Z",
		UpdateAt:    "2026-10-28T00:00:00Z",
		CreatedBy:   "user1",
		Description: "from the corner store",
	}
	content := serializeTodoItem(todo)
	if !strings.Contains(content, "id: abc12345") || !strings.Contains(content, "status: in_progress") {
		t.Errorf("serializeTodoItem missing fields:\n%s", content)
	}
	// 空可选字段输出 "key: "（对齐 Python todo.due_at or ''）
	empty := &TodoItem{ID: "x", Title: "t", CreateAt: "c", UpdateAt: "u"}
	emptyContent := serializeTodoItem(empty)
	if !strings.Contains(emptyContent, "due_at: \n") {
		t.Errorf("empty due_at should be 'due_at: ' line, got:\n%s", emptyContent)
	}
}

// TestParseTodosFile_解析与容错 逐行状态机解析（偏差#2：lookahead 正则手动等价）
func TestParseTodosFile_解析与容错(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "ch.md")
	content := "# User Todos\n\n" +
		"---\nid: t1\ntitle: a\nstatus: pending\npriority: medium\ndue_at: \nremind_at: \n" +
		"create_at: 2026-01-01T00:00:00Z\nupdate_at: 2026-01-01T00:00:00Z\ncreated_by: \n---\ndesc one\n\n" +
		"---\nid: t2\ntitle: b\nstatus: completed\n---\ndesc two with : colon\n\n" +
		"---\ntitle: no id item\n---\nshould be skipped\n"
	os.WriteFile(file, []byte(content), 0o644)

	todos := parseTodosFile(file)
	if len(todos) != 2 {
		t.Fatalf("parseTodosFile len = %d, want 2（无 id 项跳过）", len(todos))
	}
	if todos[0].ID != "t1" || todos[0].Description != "desc one" || todos[0].Status != TodoStatusPending {
		t.Errorf("todos[0] = %+v", todos[0])
	}
	if todos[1].Title != "b" || todos[1].Description != "desc two with : colon" {
		t.Errorf("todos[1] = %+v", todos[1])
	}
	// 文件不存在 → 空列表
	if got := parseTodosFile(filepath.Join(dir, "nope.md")); len(got) != 0 {
		t.Errorf("missing file should be empty, got %d", len(got))
	}
}

// TestSanitizeChannelID 路径穿越清洗
func TestSanitizeChannelID(t *testing.T) {
	cases := map[string]string{
		"../../etc/passwd": "______etc_passwd",
		"web":              "web",
		"feishu-group-01":  "feishu-group-01",
		"a.b/c":            "a_b_c",
	}
	for in, want := range cases {
		if got := sanitizeChannelID(in); got != want {
			t.Errorf("sanitizeChannelID(%q) = %q, want %q", in, got, want)
		}
	}
}

// 以下测试由 Task 2 追加（保留 import 消费：context/cronbridge 在 Task 2 用例中使用）
var _ = context.Background
var _ = cronbridge.BindRuntimeCronContext
