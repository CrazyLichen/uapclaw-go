package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sys_operation "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 测试助手 ────────────────────────────

// newReadTextOp 构造带真实本地文件系统能力的 SysOperation。
func newReadTextOp() sys_operation.SysOperation {
	return sys_operation.NewLocalSysOperation(sys_operation.NewSysOperationCard(
		sys_operation.WithSysOpMode(sys_operation.OperationModeLocal),
	))
}

// writeTempFile 在临时目录写一个文件并返回绝对路径。
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入临时文件失败: %v", err)
	}
	return path
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestReadText_正常读取 验证按行范围读取并渲染行号。
func TestReadText_正常读取(t *testing.T) {
	ctx := context.Background()
	path := writeTempFile(t, "a.txt", "line1\nline2\nline3\n")

	got, err := readText(ctx, newReadTextOp(), path, 0, 10, true)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	content, _ := got["content"].(string)
	if !strings.Contains(content, "line1") {
		t.Fatalf("应含 line1，实际 %q", content)
	}
}

// TestReadText_文件不存在 验证底层错误被包装上抛。
func TestReadText_文件不存在(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "missing.txt")

	if _, err := readText(ctx, newReadTextOp(), path, 0, 10, true); err == nil {
		t.Fatal("文件不存在应报错")
	}
}

// TestReadText_空文件 验证空内容给出明确警告而非空结果。
func TestReadText_空文件(t *testing.T) {
	ctx := context.Background()
	path := writeTempFile(t, "empty.txt", "")

	got, err := readText(ctx, newReadTextOp(), path, 0, 10, true)
	if err != nil {
		t.Fatalf("空文件不应报错: %v", err)
	}
	content, _ := got["content"].(string)
	if !strings.Contains(content, "empty") {
		t.Fatalf("应提示文件为空，实际 %q", content)
	}
}

// TestReadText_offset超出文件长度 验证超出偏移的告警文案。
func TestReadText_offset超出文件长度(t *testing.T) {
	ctx := context.Background()
	path := writeTempFile(t, "short.txt", "only-line\n")

	got, err := readText(ctx, newReadTextOp(), path, 100, 10, true)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	content, _ := got["content"].(string)
	if !strings.Contains(content, "shorter than the provided offset") {
		t.Fatalf("应提示偏移超出文件长度，实际 %q", content)
	}
}

// TestReadText_超大小限制 验证 size cap 分支。
func TestReadText_超大小限制(t *testing.T) {
	ctx := context.Background()
	big := strings.Repeat("x", maxSizeBytes+1)
	path := writeTempFile(t, "big.txt", big)

	_, err := readText(ctx, newReadTextOp(), path, 0, 10, true)
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum allowed size") {
		t.Fatalf("应报超出大小限制，实际 %v", err)
	}
}

// TestReadText_关闭大小限制后可读 验证 applySizeCap=false 时跳过大小检查。
func TestReadText_关闭大小限制后可读(t *testing.T) {
	ctx := context.Background()
	big := strings.Repeat("x", maxSizeBytes+1)
	path := writeTempFile(t, "big2.txt", big)

	// 关掉 size cap 后仍可能触发 token 上限，故只要求错误不是 size 相关
	_, err := readText(ctx, newReadTextOp(), path, 0, 10, false)
	if err != nil && strings.Contains(err.Error(), "exceeds maximum allowed size") {
		t.Fatalf("关闭 size cap 后不应报大小错误，实际 %v", err)
	}
}

// TestReadRawTextForEditState 验证原始文本读取结果。
func TestReadRawTextForEditState(t *testing.T) {
	ctx := context.Background()

	t.Run("正常读取返回内容与行数", func(t *testing.T) {
		path := writeTempFile(t, "raw.txt", "a\nb\nc")
		got := readRawTextForEditState(ctx, newReadTextOp(), path)
		if got == nil {
			t.Fatal("应返回非 nil 结果")
		}
		if got.content == "" {
			t.Fatal("content 不应为空")
		}
		if got.lineCount < 3 {
			t.Fatalf("行数应 >= 3，实际 %d", got.lineCount)
		}
	})

	t.Run("文件不存在返回 nil", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "missing.txt")
		if got := readRawTextForEditState(ctx, newReadTextOp(), path); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})
}

// TestRecordReadState 验证读取状态记录的前置条件。
func TestRecordReadState(t *testing.T) {
	ctx := context.Background()

	t.Run("mtime 为 0 时不记录", func(t *testing.T) {
		// 不 panic 即达标：前置条件直接返回
		recordReadState(ctx, newReadTextOp(), "/tmp/x.txt", 0, 0, false, 0)
	})
}
