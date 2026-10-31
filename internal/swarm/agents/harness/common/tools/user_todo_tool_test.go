package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

	todos, err := parseTodosFile(file)
	if err != nil {
		t.Fatalf("parseTodosFile 失败: %v", err)
	}
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
	if got, err := parseTodosFile(filepath.Join(dir, "nope.md")); err != nil || len(got) != 0 {
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

// 以下测试由 Task 2 追加
// TestHandleUserTodos_六action 全分支
func TestHandleUserTodos_六action(t *testing.T) {
	dir := t.TempDir()
	SetGlobalWorkspaceDir(dir)
	t.Cleanup(func() { SetGlobalWorkspaceDir(".") })

	// create 创建待办
	res, err := handleUserTodos(context.Background(), map[string]any{
		"action": "create", "title": "meeting", "description": "standup",
		"due_at": "2026-11-01T10:00:00Z",
	})
	if err != nil || res["success"] != true {
		t.Fatalf("create res=%v err=%v", res, err)
	}
	// remind_at 自动推算 due-5min（合法 RFC3339）
	todo := res["todo"].(map[string]any)
	if todo["remind_at"] != "2026-11-01T09:55:00Z" {
		t.Errorf("remind_at = %v, want 2026-11-01T09:55:00Z", todo["remind_at"])
	}
	todoID := todo["id"].(string)
	if len(todoID) != 8 {
		t.Errorf("id len = %d, want 8", len(todoID))
	}

	// create 非法 due_at → remind_at ""
	res2, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "create", "title": "x", "due_at": "not-a-time",
	})
	if res2["todo"].(map[string]any)["remind_at"] != "" {
		t.Errorf("invalid due_at should give empty remind_at")
	}

	// get 查询单条
	res3, _ := handleUserTodos(context.Background(), map[string]any{"action": "get", "todo_id": todoID})
	if res3["success"] != true || res3["todo"].(map[string]any)["title"] != "meeting" {
		t.Errorf("get res=%v", res3)
	}
	res3b, _ := handleUserTodos(context.Background(), map[string]any{"action": "get", "todo_id": "nope"})
	if res3b["success"] != false {
		t.Errorf("get missing should be success=false: %v", res3b)
	}

	// 必填校验
	for _, act := range []string{"get", "create", "update", "delete", "search"} {
		res, _ := handleUserTodos(context.Background(), map[string]any{"action": act})
		if res["success"] != false {
			t.Errorf("action %s without required param should fail: %v", act, res)
		}
	}

	// update（部分字段）
	res4, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "update", "todo_id": todoID, "status": "completed",
	})
	if res4["todo"].(map[string]any)["status"] != "completed" {
		t.Errorf("update res=%v", res4)
	}

	// list + search 列表与大小写不敏感搜索
	res5, _ := handleUserTodos(context.Background(), map[string]any{"action": "list"})
	if res5["count"].(int) < 1 {
		t.Errorf("list count=%v", res5["count"])
	}
	res6, _ := handleUserTodos(context.Background(), map[string]any{"action": "search", "query": "MEET"})
	if res6["count"].(int) != 1 {
		t.Errorf("search (case-insensitive title) count=%v, want 1", res6["count"])
	}

	// delete 删除待办
	res7, _ := handleUserTodos(context.Background(), map[string]any{"action": "delete", "todo_id": todoID})
	if res7["success"] != true {
		t.Errorf("delete res=%v", res7)
	}
	res8, _ := handleUserTodos(context.Background(), map[string]any{"action": "list"})
	if res8["count"].(int) != 1 { // 剩 x 一条
		t.Errorf("after delete count=%v, want 1", res8["count"])
	}

	// unknown action 未知 action 报错
	res9, _ := handleUserTodos(context.Background(), map[string]any{"action": "bogus"})
	if res9["error"] != "Unknown action: bogus. Valid actions: list, get, create, update, delete, search" {
		t.Errorf("unknown action error = %v", res9["error"])
	}
}

// TestHandleUserTodos_channel优先级 params > ctx > "default"
func TestHandleUserTodos_channel优先级(t *testing.T) {
	dir := t.TempDir()
	SetGlobalWorkspaceDir(dir)
	t.Cleanup(func() { SetGlobalWorkspaceDir(".") })

	// ctx 绑定（cron 上下文）
	ctx := cronbridge.BindRuntimeCronContext(context.Background(), "ctxchan", "sess", nil, "req", "")
	handleUserTodos(ctx, map[string]any{"action": "create", "title": "a"})
	if _, err := os.Stat(filepath.Join(dir, "memory", "user_todos", "ctxchan.md")); err != nil {
		t.Errorf("ctx channel file missing: %v", err)
	}
	// params 覆盖 ctx
	handleUserTodos(ctx, map[string]any{"action": "create", "title": "b", "channel_id": "paramchan"})
	if _, err := os.Stat(filepath.Join(dir, "memory", "user_todos", "paramchan.md")); err != nil {
		t.Errorf("param channel file missing: %v", err)
	}
	// 无绑定 → default
	handleUserTodos(context.Background(), map[string]any{"action": "create", "title": "c"})
	if _, err := os.Stat(filepath.Join(dir, "memory", "user_todos", "default.md")); err != nil {
		t.Errorf("default channel file missing: %v", err)
	}
}

// TestNewUserTodosTool_卡片与注册 GetDecoratedTools
func TestNewUserTodosTool_卡片与注册(t *testing.T) {
	tools := GetDecoratedTools()
	if len(tools) != 1 {
		t.Fatalf("GetDecoratedTools len=%d, want 1", len(tools))
	}
	card := tools[0].Card()
	if card.Name != "user_todos" {
		t.Errorf("card name=%s", card.Name)
	}
	if !strings.Contains(card.Description, "do NOT use it for internal agent task planning") {
		t.Errorf("description missing NOTE: %s", card.Description)
	}
	// action 参数必填
	for _, p := range card.InputParams {
		if p.Name == "action" && !p.Required {
			t.Errorf("action param should be required")
		}
	}
}

// TestNewUserTodosTool_独立构造 导出构造器与 GetGlobalWorkspaceDir
func TestNewUserTodosTool_独立构造(t *testing.T) {
	if NewUserTodosTool() == nil {
		t.Error("NewUserTodosTool returned nil")
	}
	if GetGlobalWorkspaceDir() == "" {
		t.Error("GetGlobalWorkspaceDir should be non-empty（默认 '.'）")
	}
	SetGlobalWorkspaceDir("/tmp/ws-test")
	t.Cleanup(func() { SetGlobalWorkspaceDir(".") })
	if GetGlobalWorkspaceDir() != "/tmp/ws-test" {
		t.Errorf("GetGlobalWorkspaceDir=%q", GetGlobalWorkspaceDir())
	}
	// agentWorkspaceDir 为空串时 SetGlobalWorkspaceDir("") 也可写入（Python 语义）
	SetGlobalWorkspaceDir("")
	if GetGlobalWorkspaceDir() != "" {
		t.Errorf("empty set failed: %q", GetGlobalWorkspaceDir())
	}
}

// TestHandleUserTodos_create带createdBy与status 显式参数路径
func TestHandleUserTodos_create带createdBy与status(t *testing.T) {
	dir := t.TempDir()
	SetGlobalWorkspaceDir(dir)
	t.Cleanup(func() { SetGlobalWorkspaceDir(".") })

	res, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "create", "title": "t", "created_by": "alice",
		"status": "in_progress", "priority": "low", "remind_at": "2026-11-01T09:00:00Z",
		"description": "d",
	})
	todo := res["todo"].(map[string]any)
	if todo["created_by"] != "alice" || todo["status"] != "in_progress" || todo["priority"] != "low" {
		t.Errorf("create with explicit fields=%v", todo)
	}
	// 显式 remind_at 不被 due-5min 覆盖（无 due_at）
	if todo["remind_at"] != "2026-11-01T09:00:00Z" {
		t.Errorf("explicit remind_at=%v", todo["remind_at"])
	}
}

// TestHandleUserTodos_update存在性判断 显式空串也更新（对齐 Python is not None）
func TestHandleUserTodos_update存在性判断(t *testing.T) {
	dir := t.TempDir()
	SetGlobalWorkspaceDir(dir)
	t.Cleanup(func() { SetGlobalWorkspaceDir(".") })

	res, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "create", "title": "orig", "description": "keep",
	})
	id := res["todo"].(map[string]any)["id"].(string)

	// 显式空串 title → 更新为空（Python is not None 语义）
	res2, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "update", "todo_id": id, "title": "",
	})
	if res2["todo"].(map[string]any)["title"] != "" {
		t.Errorf("explicit empty title should update: %v", res2["todo"])
	}
	// status 空串跳过（偏差#8）
	res3, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "update", "todo_id": id, "status": "",
	})
	if res3["todo"].(map[string]any)["status"] != "pending" {
		t.Errorf("empty status should skip（偏差#8）: %v", res3["todo"])
	}
}

// TestParseTodosFile_无frontmatter边界 描述含 --- 与 header 混排
func TestParseTodosFile_无frontmatter边界(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "edge.md")
	// 只有一对 ---（无闭合 frontmatter）→ body 吞 EOF
	content := "# User Todos\n\n---\nid: only\n---\nbody line\nstays\n"
	os.WriteFile(file, []byte(content), 0o644)
	todos, err := parseTodosFile(file)
	if err != nil {
		t.Fatalf("parseTodosFile 失败: %v", err)
	}
	if len(todos) != 1 || todos[0].Description != "body line\nstays" {
		t.Errorf("edge parse=%+v", todos)
	}
}

// TestParseTodoStatusPriority_M04门卫 M-04：非法值拒绝、合法值直通
func TestParseTodoStatusPriority_M04门卫(t *testing.T) {
	for _, s := range []string{"pending", "in_progress", "completed", "cancelled"} {
		if _, ok := parseTodoStatus(s); !ok {
			t.Errorf("parseTodoStatus(%q) should be valid", s)
		}
	}
	for _, s := range []string{"", "Pending", "DONE", "done", "in-progress", "unknown"} {
		if _, ok := parseTodoStatus(s); ok {
			t.Errorf("parseTodoStatus(%q) should be invalid", s)
		}
	}
	for _, p := range []string{"high", "medium", "low"} {
		if _, ok := parseTodoPriority(p); !ok {
			t.Errorf("parseTodoPriority(%q) should be valid", p)
		}
	}
	for _, p := range []string{"", "High", "urgent", "none", "0"} {
		if _, ok := parseTodoPriority(p); ok {
			t.Errorf("parseTodoPriority(%q) should be invalid", p)
		}
	}
}

// TestParseTodosFile_坏status跳过该条 M-04：非法 status/priority 的条目
// 对齐 Python except Exception → warn + 跳过，不影响其余条目。
func TestParseTodosFile_坏status跳过该条(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "bad.md")
	content := "# User Todos\n\n" +
		"---\nid: good1\ntitle: ok\nstatus: pending\npriority: medium\n---\nfine\n\n" +
		"---\nid: bad1\ntitle: broken status\nstatus: DONE\npriority: medium\n---\nbad\n\n" +
		"---\nid: bad2\ntitle: broken priority\nstatus: pending\npriority: urgent\n---\nbad2\n\n" +
		"---\nid: good2\ntitle: also ok\n---\nalso fine\n"
	os.WriteFile(file, []byte(content), 0o644)
	todos, err := parseTodosFile(file)
	if err != nil {
		t.Fatalf("parseTodosFile 失败: %v", err)
	}
	if len(todos) != 2 {
		t.Fatalf("bad items should be skipped, got %d todos", len(todos))
	}
	if todos[0].ID != "good1" || todos[1].ID != "good2" {
		t.Errorf("surviving ids = %s, %s; want good1, good2", todos[0].ID, todos[1].ID)
	}
}

// TestHandleUserTodos_create非法status拒绝 M-04：create 传非法 status/priority
// → {"success": false}（Python TodoStatus() ValueError → 外层 except）。
func TestHandleUserTodos_create非法status拒绝(t *testing.T) {
	dir := t.TempDir()
	SetGlobalWorkspaceDir(dir)
	defer SetGlobalWorkspaceDir(".")

	res, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "create", "title": "t", "status": "DONE",
	})
	if res["success"] != false {
		t.Errorf("invalid status should fail: %v", res)
	}
	if e, _ := res["error"].(string); !strings.Contains(e, "invalid status") {
		t.Errorf("error should mention invalid status: %v", res["error"])
	}

	res2, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "create", "title": "t", "priority": "urgent",
	})
	if res2["success"] != false {
		t.Errorf("invalid priority should fail: %v", res2)
	}

	// 合法值仍可创建
	res3, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "create", "title": "ok", "status": "in_progress", "priority": "high",
	})
	if res3["success"] != true {
		t.Errorf("valid status/priority should create: %v", res3)
	}
	// 拒绝后未写入任何条目
	res4, _ := handleUserTodos(context.Background(), map[string]any{"action": "list"})
	if res4["count"] != 1 {
		t.Errorf("only the valid create should persist, count = %v", res4["count"])
	}
}

// TestHandleUserTodos_update非法status拒绝 M-04：update 传非法 status → 失败。
func TestHandleUserTodos_update非法status拒绝(t *testing.T) {
	dir := t.TempDir()
	SetGlobalWorkspaceDir(dir)
	defer SetGlobalWorkspaceDir(".")

	created, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "create", "title": "base",
	})
	id := created["todo"].(map[string]any)["id"].(string)

	res, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "update", "todo_id": id, "status": "bogus",
	})
	if res["success"] != false {
		t.Errorf("invalid status update should fail: %v", res)
	}
	res2, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "update", "todo_id": id, "priority": "top",
	})
	if res2["success"] != false {
		t.Errorf("invalid priority update should fail: %v", res2)
	}
	// 合法值仍可更新
	res3, _ := handleUserTodos(context.Background(), map[string]any{
		"action": "update", "todo_id": id, "status": "completed",
	})
	if res3["success"] != true {
		t.Errorf("valid status update should succeed: %v", res3)
	}
}

// TestParseTodosFile_读失败上抛 回归（M-10）：只有「文件不存在」容错为空，
// 其他读错误必须上抛（旧实现一律返回 nil，create 会以单条覆盖整个待办文件）。
func TestParseTodosFile_读失败上抛(t *testing.T) {
	dir := t.TempDir()
	// 用目录冒充文件：ReadFile 返回 EISDIR，不属于 ErrNotExist
	if _, err := parseTodosFile(dir); err == nil {
		t.Fatal("读目录应返回错误，不得当作空列表")
	}
}

// TestWriteTodosFile_写失败上抛 回归（M-10）：写盘失败必须上抛（旧实现 `_ = os.WriteFile`
// 吞错，工具仍向模型回 success=true）。
func TestWriteTodosFile_写失败上抛(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "blocker")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatalf("构造阻塞文件失败: %v", err)
	}
	// 父路径是普通文件 → MkdirAll 必失败
	if err := writeTodosFile(filepath.Join(blocker, "todos.md"), nil); err == nil {
		t.Fatal("父路径为文件时应返回写入错误")
	}
}

// TestLockTodosFile_串行化 回归（M-10）：同一路径的锁互斥，不同路径互不影响。
func TestLockTodosFile_串行化(t *testing.T) {
	pathA := filepath.Join(t.TempDir(), "a.md")
	pathB := filepath.Join(t.TempDir(), "b.md")

	unlockA := lockTodosFile(pathA)
	acquiredB := make(chan struct{})
	go func() {
		unlockB := lockTodosFile(pathB)
		unlockB()
		close(acquiredB)
	}()
	select {
	case <-acquiredB:
	case <-time.After(time.Second):
		t.Fatal("不同路径的锁不应互相阻塞")
	}

	acquiredA := make(chan struct{})
	go func() {
		unlockAgain := lockTodosFile(pathA)
		unlockAgain()
		close(acquiredA)
	}()
	select {
	case <-acquiredA:
		t.Fatal("同一路径的锁应互斥（第二次获取应等待）")
	case <-time.After(50 * time.Millisecond):
	}
	unlockA()
	select {
	case <-acquiredA:
	case <-time.After(time.Second):
		t.Fatal("释放后应能获取锁")
	}
}
