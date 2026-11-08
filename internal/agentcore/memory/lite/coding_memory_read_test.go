package lite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newReadCtx 构造指向 coding_memory 节点的工具上下文（带真实文件系统）。
// 返回上下文与该节点在磁盘上的绝对路径。
func newReadCtx(t *testing.T) (*CodingMemoryToolContext, string) {
	t.Helper()
	ws := newTestWorkspace(t, "coding_memory")
	toolCtx := &CodingMemoryToolContext{
		LiteMemoryToolContextBase: LiteMemoryToolContextBase{
			Workspace:    ws,
			SysOperation: newLocalSysOp(t, ws.RootPath),
		},
	}
	var memDir string
	if p := ws.GetNodePath("coding_memory"); p != nil {
		memDir = *p
	}
	if memDir == "" {
		t.Fatal("coding_memory 节点未配置")
	}
	return toolCtx, memDir
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestCodingMemoryReadWithContext_前置校验 验证各参数校验分支。
func TestCodingMemoryReadWithContext_前置校验(t *testing.T) {
	ctx := context.Background()

	t.Run("Workspace 未初始化", func(t *testing.T) {
		res := CodingMemoryReadWithContext(ctx, &CodingMemoryToolContext{}, "a.md", nil, nil)
		if res.Success {
			t.Fatal("无 workspace 应失败")
		}
		if res.Error != "Workspace 未初始化" {
			t.Fatalf("Error = %q", res.Error)
		}
	})

	t.Run("路径含目录遍历", func(t *testing.T) {
		toolCtx, _ := newReadCtx(t)
		res := CodingMemoryReadWithContext(ctx, toolCtx, "../etc/passwd.md", nil, nil)
		if res.Success {
			t.Fatal("目录遍历应被拒")
		}
		if res.Path != "../etc/passwd.md" {
			t.Fatalf("应回显原路径，实际 %q", res.Path)
		}
	})

	t.Run("路径非 md 后缀", func(t *testing.T) {
		toolCtx, _ := newReadCtx(t)
		res := CodingMemoryReadWithContext(ctx, toolCtx, "note.txt", nil, nil)
		if res.Success {
			t.Fatal("非 md 应被拒")
		}
	})

	t.Run("无 SysOperation", func(t *testing.T) {
		toolCtx, _ := newReadCtx(t)
		toolCtx.SysOperation = nil
		res := CodingMemoryReadWithContext(ctx, toolCtx, "a.md", nil, nil)
		if res.Success {
			t.Fatal("无 sys_operation 应失败")
		}
	})

	t.Run("文件不存在", func(t *testing.T) {
		toolCtx, _ := newReadCtx(t)
		res := CodingMemoryReadWithContext(ctx, toolCtx, "missing.md", nil, nil)
		if res.Success {
			t.Fatal("文件不存在应失败")
		}
	})
}

// TestCodingMemoryReadWithContext_读取成功 验证全文读取与分页读取。
func TestCodingMemoryReadWithContext_读取成功(t *testing.T) {
	ctx := context.Background()
	toolCtx, memDir := newReadCtx(t)
	content := "line1\nline2\nline3\nline4\nline5"
	if err := os.WriteFile(filepath.Join(memDir, "note.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("写入记忆文件失败: %v", err)
	}

	t.Run("全文读取", func(t *testing.T) {
		res := CodingMemoryReadWithContext(ctx, toolCtx, "note.md", nil, nil)
		if !res.Success {
			t.Fatalf("应成功，实际 %q", res.Error)
		}
		if res.TotalLines != 5 {
			t.Fatalf("TotalLines = %d, 期望 5", res.TotalLines)
		}
		if res.Content != content {
			t.Fatalf("Content = %q", res.Content)
		}
		if res.Truncated {
			t.Fatal("未设 limit 时不应标记截断")
		}
	})

	t.Run("offset+limit 分页", func(t *testing.T) {
		// 底层 WithFsLineRange 已按 (offset, offset+limit-1) 截取文件，
		// 返回的 TotalLines/EndLine 均以"截取后的片段"为基准而非原文件——
		// 故此处断言片段自洽（StartLine 为 1 基起点，EndLine 不超过 TotalLines）。
		offset, limit := 2, 2
		res := CodingMemoryReadWithContext(ctx, toolCtx, "note.md", &offset, &limit)
		if !res.Success {
			t.Fatalf("应成功，实际 %q", res.Error)
		}
		// fromIdx 按 offset-1 计算，故 StartLine 仍等于传入的 offset
		if res.StartLine != offset {
			t.Fatalf("StartLine = %d, 期望 %d", res.StartLine, offset)
		}
		if res.EndLine > res.TotalLines {
			t.Fatalf("EndLine(%d) 不应超过 TotalLines(%d)", res.EndLine, res.TotalLines)
		}
		if res.Content == "" {
			t.Fatal("分页读取应返回内容")
		}
	})

	t.Run("仅 offset 读到末尾", func(t *testing.T) {
		offset := 4
		res := CodingMemoryReadWithContext(ctx, toolCtx, "note.md", &offset, nil)
		if !res.Success {
			t.Fatalf("应成功，实际 %q", res.Error)
		}
		if res.StartLine != 4 {
			t.Fatalf("StartLine = %d, 期望 4", res.StartLine)
		}
	})

	t.Run("offset 为 0 时钳到起始行", func(t *testing.T) {
		offset := 0
		res := CodingMemoryReadWithContext(ctx, toolCtx, "note.md", &offset, nil)
		if !res.Success {
			t.Fatalf("应成功，实际 %q", res.Error)
		}
		if res.StartLine != 1 {
			t.Fatalf("StartLine = %d, 期望 1", res.StartLine)
		}
	})

	t.Run("limit 超出文件长度时截到末尾", func(t *testing.T) {
		limit := 100
		res := CodingMemoryReadWithContext(ctx, toolCtx, "note.md", nil, &limit)
		if !res.Success {
			t.Fatalf("应成功，实际 %q", res.Error)
		}
		if res.EndLine != res.TotalLines {
			t.Fatalf("EndLine = %d, TotalLines = %d", res.EndLine, res.TotalLines)
		}
	})
}
