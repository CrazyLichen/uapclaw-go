package memory

import (
	"context"
	"fmt"
	"testing"

	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCreateMemoryTools_默认 测试创建记忆工具集返回 5 个 Tool
func TestCreateMemoryTools_默认(t *testing.T) {
	ctx := lite.NewMemoryToolContext()
	tools := CreateMemoryTools(ctx, "cn", "agent1")
	if len(tools) != 5 {
		t.Fatalf("期望 5 个 Tool，实际 %d", len(tools))
	}
	for i, tl := range tools {
		if tl == nil {
			t.Fatalf("tools[%d] 为 nil", i)
		}
		if tl.Card() == nil {
			t.Fatalf("tools[%d].Card() 为 nil", i)
		}
	}
	// 验证类型顺序：MemorySearchTool, MemoryGetTool, WriteMemoryTool, EditMemoryTool, ReadMemoryTool
	expectedTypes := []string{
		"*memory.MemorySearchTool",
		"*memory.MemoryGetTool",
		"*memory.WriteMemoryTool",
		"*memory.EditMemoryTool",
		"*memory.ReadMemoryTool",
	}
	for i, tl := range tools {
		typeName := fmt.Sprintf("%T", tl)
		if typeName != expectedTypes[i] {
			t.Fatalf("tools[%d] 类型 = %q, want %q", i, typeName, expectedTypes[i])
		}
	}
}

// TestMemorySearchTool_Invoke_查询缺失 测试 query 为空返回错误
func TestMemorySearchTool_Invoke_查询缺失(t *testing.T) {
	ctx := lite.NewMemoryToolContext()
	tools := CreateMemoryTools(ctx, "cn", "agent1")
	searchTool := tools[0].(*MemorySearchTool)

	_, err := searchTool.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("期望返回错误（query 缺失），实际为 nil")
	}

	_, err = searchTool.Invoke(context.Background(), map[string]any{"query": ""})
	if err == nil {
		t.Fatal("期望返回错误（query 为空），实际为 nil")
	}
}

// TestMemoryGetTool_Invoke_路径缺失 测试 path 为空返回错误
func TestMemoryGetTool_Invoke_路径缺失(t *testing.T) {
	ctx := lite.NewMemoryToolContext()
	tools := CreateMemoryTools(ctx, "cn", "agent1")
	getTool := tools[1].(*MemoryGetTool)

	_, err := getTool.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("期望返回错误（path 缺失），实际为 nil")
	}

	_, err = getTool.Invoke(context.Background(), map[string]any{"path": ""})
	if err == nil {
		t.Fatal("期望返回错误（path 为空），实际为 nil")
	}
}

// TestWriteMemoryTool_Invoke_路径缺失 测试 path 为空返回错误
func TestWriteMemoryTool_Invoke_路径缺失(t *testing.T) {
	ctx := lite.NewMemoryToolContext()
	tools := CreateMemoryTools(ctx, "cn", "agent1")
	writeTool := tools[2].(*WriteMemoryTool)

	_, err := writeTool.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("期望返回错误（path 缺失），实际为 nil")
	}

	_, err = writeTool.Invoke(context.Background(), map[string]any{"path": ""})
	if err == nil {
		t.Fatal("期望返回错误（path 为空），实际为 nil")
	}

	// path 有值但 content 为空
	_, err = writeTool.Invoke(context.Background(), map[string]any{"path": "test.md", "content": ""})
	if err == nil {
		t.Fatal("期望返回错误（content 为空），实际为 nil")
	}
}

// TestEditMemoryTool_Invoke_路径缺失 测试 path/old_text/new_text 为空返回错误
func TestEditMemoryTool_Invoke_路径缺失(t *testing.T) {
	ctx := lite.NewMemoryToolContext()
	tools := CreateMemoryTools(ctx, "cn", "agent1")
	editTool := tools[3].(*EditMemoryTool)

	// path 缺失
	_, err := editTool.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("期望返回错误（path 缺失），实际为 nil")
	}

	// path 为空
	_, err = editTool.Invoke(context.Background(), map[string]any{"path": ""})
	if err == nil {
		t.Fatal("期望返回错误（path 为空），实际为 nil")
	}

	// old_text 为空
	_, err = editTool.Invoke(context.Background(), map[string]any{"path": "test.md", "old_text": ""})
	if err == nil {
		t.Fatal("期望返回错误（old_text 为空），实际为 nil")
	}

	// new_text 为空
	_, err = editTool.Invoke(context.Background(), map[string]any{"path": "test.md", "old_text": "old", "new_text": ""})
	if err == nil {
		t.Fatal("期望返回错误（new_text 为空），实际为 nil")
	}
}

// TestReadMemoryTool_Invoke_路径缺失 测试 path 为空返回错误
func TestReadMemoryTool_Invoke_路径缺失(t *testing.T) {
	ctx := lite.NewMemoryToolContext()
	tools := CreateMemoryTools(ctx, "cn", "agent1")
	readTool := tools[4].(*ReadMemoryTool)

	_, err := readTool.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("期望返回错误（path 缺失），实际为 nil")
	}

	_, err = readTool.Invoke(context.Background(), map[string]any{"path": ""})
	if err == nil {
		t.Fatal("期望返回错误（path 为空），实际为 nil")
	}
}
