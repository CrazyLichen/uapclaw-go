package coding_memory

import (
	"context"
	"testing"

	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
)

// TestCreateCodingMemoryTools_默认 验证返回 3 个 Tool
func TestCreateCodingMemoryTools_默认(t *testing.T) {
	ctx := lite.NewCodingMemoryToolContext()
	tools := CreateCodingMemoryTools(ctx, "cn", "test-agent")

	if len(tools) != 3 {
		t.Fatalf("期望 3 个 Tool，实际 %d 个", len(tools))
	}

	// 验证类型
	if _, ok := tools[0].(*CodingMemoryReadTool); !ok {
		t.Errorf("tools[0] 期望 CodingMemoryReadTool，实际 %T", tools[0])
	}
	if _, ok := tools[1].(*CodingMemoryWriteTool); !ok {
		t.Errorf("tools[1] 期望 CodingMemoryWriteTool，实际 %T", tools[1])
	}
	if _, ok := tools[2].(*CodingMemoryEditTool); !ok {
		t.Errorf("tools[2] 期望 CodingMemoryEditTool，实际 %T", tools[2])
	}

	// 验证 Card 非空
	for i, tool := range tools {
		if tool.Card() == nil {
			t.Errorf("tools[%d].Card() 不应为 nil", i)
		}
	}
}

// TestCodingMemoryReadTool_Invoke_路径缺失 path 为空返回错误
func TestCodingMemoryReadTool_Invoke_路径缺失(t *testing.T) {
	ctx := lite.NewCodingMemoryToolContext()
	tools := CreateCodingMemoryTools(ctx, "cn", "test-agent")
	readTool := tools[0].(*CodingMemoryReadTool)

	_, err := readTool.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("path 缺失时应返回错误")
	}
}

// TestCodingMemoryWriteTool_Invoke_路径缺失 path 为空返回错误
func TestCodingMemoryWriteTool_Invoke_路径缺失(t *testing.T) {
	ctx := lite.NewCodingMemoryToolContext()
	tools := CreateCodingMemoryTools(ctx, "cn", "test-agent")
	writeTool := tools[1].(*CodingMemoryWriteTool)

	_, err := writeTool.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("path 缺失时应返回错误")
	}

	// path 存在但 content 缺失
	_, err = writeTool.Invoke(context.Background(), map[string]any{"path": "test.md"})
	if err == nil {
		t.Fatal("content 缺失时应返回错误")
	}
}

// TestCodingMemoryEditTool_Invoke_路径缺失 path/old_text/new_text 为空返回错误
func TestCodingMemoryEditTool_Invoke_路径缺失(t *testing.T) {
	ctx := lite.NewCodingMemoryToolContext()
	tools := CreateCodingMemoryTools(ctx, "cn", "test-agent")
	editTool := tools[2].(*CodingMemoryEditTool)

	// path 缺失
	_, err := editTool.Invoke(context.Background(), map[string]any{})
	if err == nil {
		t.Fatal("path 缺失时应返回错误")
	}

	// old_text 缺失
	_, err = editTool.Invoke(context.Background(), map[string]any{"path": "test.md"})
	if err == nil {
		t.Fatal("old_text 缺失时应返回错误")
	}

	// new_text 缺失
	_, err = editTool.Invoke(context.Background(), map[string]any{"path": "test.md", "old_text": "old"})
	if err == nil {
		t.Fatal("new_text 缺失时应返回错误")
	}
}
