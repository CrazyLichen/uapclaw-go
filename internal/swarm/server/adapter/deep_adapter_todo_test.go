package adapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	// blank import 触发 local 包 init()：向 GlobalRegistry 注册 fs/shell/code 本地实现
	// （生产路径经 sysop_builder import 链触发；测试直连 sysop 需显式引入）
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
)

// TestCancelPendingTodos_无实例返回nil 验证 instance nil 的防御分支。
// Python: _cancel_pending_todos (interface_deep.py:4358-4359)
func TestCancelPendingTodos_无实例返回nil(t *testing.T) {
	d := NewDeepAdapter()
	if got := d.cancelPendingTodos(t.Context(), "s1"); got != nil {
		t.Errorf("instance nil 应返回 nil, got %v", got)
	}
}

// TestCancelPendingTodos_全链路标记cancelled 验证完整链路：
// workspace todo 节点 → LoadTodos → CancelTodos → SaveTodos → 重载 → 前端格式。
// Python: _cancel_pending_todos 主路径（interface_deep.py:4351-4410）。
func TestCancelPendingTodos_全链路标记cancelled(t *testing.T) {
	root := t.TempDir()
	ws := hworkspace.NewWorkspace(root, "cn")

	// 预置 todo 文件：一个 pending、一个 completed（仅 pending 应被标记 cancelled 后过滤）
	todoDir := filepath.Join(root, "todo", "sess-1")
	if err := os.MkdirAll(todoDir, 0o755); err != nil {
		t.Fatal(err)
	}
	seed := []map[string]any{
		{"id": "a", "content": "任务a", "status": "pending"},
		{"id": "b", "content": "任务b", "status": "completed"},
	}
	data, _ := json.MarshalIndent(seed, "", "  ")
	if err := os.WriteFile(filepath.Join(todoDir, "todo.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	agent := harness.NewDeepAgent(agentschema.NewAgentCard())
	if err := agent.ConfigureDeepConfig(context.Background(), &hschema.DeepAgentConfig{
		Workspace: ws,
	}); err != nil {
		t.Fatal(err)
	}
	d := NewDeepAdapter()
	d.instance = agent
	// 注入本地 SysOperation（对齐生产路径 createSysOperation 的 LOCAL 模式构造）
	d.sysOperation = sysop.NewLocalSysOperation(sysop.NewSysOperationCard(
		sysop.WithSysOpMode(sysop.OperationModeLocal),
	))

	got := d.cancelPendingTodos(context.Background(), "sess-1")
	if len(got) != 1 {
		t.Fatalf("completed 保留 1 项，pending 已 cancelled 被过滤: %v", got)
	}
	if got[0]["id"] != "b" || got[0]["status"] != "completed" {
		t.Errorf("保留项应为 b/completed: %v", got[0])
	}

	// 落盘验证：任务 a 已标记 cancelled
	raw, err := os.ReadFile(filepath.Join(todoDir, "todo.json"))
	if err != nil {
		t.Fatal(err)
	}
	var persisted []map[string]any
	if err := json.Unmarshal(raw, &persisted); err != nil {
		t.Fatal(err)
	}
	if len(persisted) != 2 {
		t.Fatalf("落盘应保留 2 项: %v", persisted)
	}
	if persisted[0]["status"] != "cancelled" {
		t.Errorf("任务 a 落盘应为 cancelled: %v", persisted[0])
	}
}

// TestFormatTodosForFrontendLocal_过滤cancelled 验证前端格式化的过滤与映射。
// 对齐 commrails.formatTodosForFrontend (stream_event_emit.go:234)
func TestFormatTodosForFrontendLocal_过滤cancelled(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "1", Content: "a", Status: hschema.TodoStatusPending},
		{ID: "2", Content: "b", Status: hschema.TodoStatusCancelled},
		{ID: "3", Content: "c", Status: hschema.TodoStatusCompleted},
	}
	got := formatTodosForFrontendLocal(todos)
	if len(got) != 2 {
		t.Fatalf("应过滤 cancelled 剩 2: %v", got)
	}
	if got[0]["status"] != "pending" || got[1]["status"] != "completed" {
		t.Error("状态映射异常")
	}
	if got[0]["id"] != "1" || got[0]["content"] != "a" || got[0]["activeForm"] != "" {
		t.Errorf("字段映射异常: %v", got[0])
	}
}

// TestFormatTodosForFrontendLocal_进行中状态 验证 in_progress 映射。
func TestFormatTodosForFrontendLocal_进行中状态(t *testing.T) {
	todos := []hschema.TodoItem{
		{ID: "9", Content: "x", ActiveForm: "正在做 x", Status: hschema.TodoStatusInProgress},
	}
	got := formatTodosForFrontendLocal(todos)
	if len(got) != 1 || got[0]["status"] != "in_progress" {
		t.Fatalf("in_progress 映射异常: %v", got)
	}
	if got[0]["activeForm"] != "正在做 x" {
		t.Errorf("activeForm 异常: %v", got[0])
	}
}

// TestFormatTodosForFrontendLocal_空列表 验证空列表返回空切片非 nil。
func TestFormatTodosForFrontendLocal_空列表(t *testing.T) {
	got := formatTodosForFrontendLocal(nil)
	if len(got) != 0 {
		t.Errorf("空列表应返回空: %v", got)
	}
}
