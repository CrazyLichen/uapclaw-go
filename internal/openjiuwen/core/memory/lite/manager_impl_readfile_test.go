package lite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newIndexManagerForRead 构造只带 memoryDir（可选 sysOperation）的索引管理器。
func newIndexManagerForRead(t *testing.T, withSysOp bool) (*memoryIndexManager, string) {
	t.Helper()
	ws := newTestWorkspace(t, "memory")
	memDir := filepath.Join(ws.RootPath, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatalf("创建记忆目录失败: %v", err)
	}
	m := &memoryIndexManager{
		workspace: ws,
		memoryDir: memDir,
	}
	if withSysOp {
		m.sysOperation = newLocalSysOp(t, ws.RootPath)
	}
	return m, memDir
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestMemoryIndexManager_ReadFile_OS路径 验证无 sysOperation 时走 os.ReadFile。
func TestMemoryIndexManager_ReadFile_OS路径(t *testing.T) {
	ctx := context.Background()
	m, memDir := newIndexManagerForRead(t, false)
	if err := os.WriteFile(filepath.Join(memDir, "a.md"), []byte("l1\nl2\nl3"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	t.Run("全文读取", func(t *testing.T) {
		got, err := m.ReadFile(ctx, "a.md", nil, nil)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if got.TotalLines != 3 {
			t.Fatalf("TotalLines = %d, 期望 3", got.TotalLines)
		}
		if got.Text != "l1\nl2\nl3" {
			t.Fatalf("Text = %q", got.Text)
		}
	})

	t.Run("按行范围读取", func(t *testing.T) {
		from, lines := 2, 1
		got, err := m.ReadFile(ctx, "a.md", &from, &lines)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if got.Text != "l2" {
			t.Fatalf("Text = %q, 期望 l2", got.Text)
		}
	})

	t.Run("fromLine 超出范围时返回空", func(t *testing.T) {
		// 回归：Python 切片 all_lines[start:end] 在 start 越过末尾时返回空列表，
		// Go 的 allLines[start:end] 会 panic（slice bounds out of range [98:3]），
		// 曾经因此在 from_line 越界时直接崩掉整个读取。
		from := 99
		got, err := m.ReadFile(ctx, "a.md", &from, nil)
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if got.Text != "" {
			t.Fatalf("超出范围应返回空，实际 %q", got.Text)
		}
		if got.ToLine != from-1 {
			t.Fatalf("ToLine = %d, 期望 %d（空结果）", got.ToLine, from-1)
		}
	})

	t.Run("文件不存在报错", func(t *testing.T) {
		if _, err := m.ReadFile(ctx, "missing.md", nil, nil); err == nil {
			t.Fatal("文件不存在应报错")
		}
	})
}

// TestMemoryIndexManager_ReadFile_SysOp路径 验证有 sysOperation 时走 Fs().ReadFile。
func TestMemoryIndexManager_ReadFile_SysOp路径(t *testing.T) {
	ctx := context.Background()
	m, memDir := newIndexManagerForRead(t, true)
	if err := os.WriteFile(filepath.Join(memDir, "b.md"), []byte("x\ny"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	got, err := m.ReadFile(ctx, "b.md", nil, nil)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.Text != "x\ny" {
		t.Fatalf("Text = %q", got.Text)
	}

	t.Run("文件不存在报错", func(t *testing.T) {
		if _, err := m.ReadFile(ctx, "missing.md", nil, nil); err == nil {
			t.Fatal("文件不存在应报错")
		}
	})
}

// TestMemoryIndexManager_ReadFile_绝对值路径 验证绝对路径直接使用。
func TestMemoryIndexManager_ReadFile_绝对值路径(t *testing.T) {
	ctx := context.Background()
	m, _ := newIndexManagerForRead(t, false)
	dir := t.TempDir()
	abs := filepath.Join(dir, "abs.md")
	if err := os.WriteFile(abs, []byte("abs-content"), 0o644); err != nil {
		t.Fatalf("写入文件失败: %v", err)
	}

	got, err := m.ReadFile(ctx, abs, nil, nil)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	if got.Text != "abs-content" {
		t.Fatalf("Text = %q", got.Text)
	}
}

// TestMemoryIndexManager_LLM 验证未注入 LLM 时返回 nil。
func TestMemoryIndexManager_LLM(t *testing.T) {
	m, _ := newIndexManagerForRead(t, false)
	if got := m.LLM(); got != nil {
		t.Fatalf("未注入时应返回 nil，实际 %#v", got)
	}
}
