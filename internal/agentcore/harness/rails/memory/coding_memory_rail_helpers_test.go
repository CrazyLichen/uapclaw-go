package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newLocalSysOpForRail 构造不限制沙箱的本地 SysOperation。
func newLocalSysOpForRail(t *testing.T) sysop.SysOperation {
	t.Helper()
	op, err := sysop.NewSysOperation(sysop.NewSysOperationCard(
		sysop.WithSysOpMode(sysop.OperationModeLocal),
		sysop.WithSysOpWorkConfig(&sysop.LocalWorkConfig{RestrictToSandbox: false}),
	))
	if err != nil {
		t.Fatalf("创建 SysOperation 失败: %v", err)
	}
	return op
}

// newRailWithDir 构造指向指定记忆目录、带真实文件系统的 rail。
func newRailWithDir(t *testing.T, dir string) *CodingMemoryRail {
	t.Helper()
	r := NewCodingMemoryRail(dir, nil, "cn")
	r.SetSysOperation(newLocalSysOpForRail(t))
	return r
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestCodingMemoryRail_ReadFileSafe 验证安全读取的各分支。
func TestCodingMemoryRail_ReadFileSafe(t *testing.T) {
	ctx := context.Background()

	t.Run("无 SysOperation 返回空串", func(t *testing.T) {
		r := NewCodingMemoryRail(t.TempDir(), nil, "cn")
		if got := r.readFileSafe(ctx, "/any/path"); got != "" {
			t.Fatalf("应返回空串，实际 %q", got)
		}
	})

	t.Run("正常读取返回内容", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "a.md")
		if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
			t.Fatalf("写入文件失败: %v", err)
		}
		r := newRailWithDir(t, dir)
		if got := r.readFileSafe(ctx, path); got != "hello" {
			t.Fatalf("内容 = %q, 期望 hello", got)
		}
	})

	t.Run("文件不存在返回空串", func(t *testing.T) {
		dir := t.TempDir()
		r := newRailWithDir(t, dir)
		if got := r.readFileSafe(ctx, filepath.Join(dir, "missing.md")); got != "" {
			t.Fatalf("应返回空串，实际 %q", got)
		}
	})
}

// TestCodingMemoryRail_CountMemoryFiles 验证记忆文件计数的过滤规则。
func TestCodingMemoryRail_CountMemoryFiles(t *testing.T) {
	ctx := context.Background()

	t.Run("无 SysOperation 返回 0", func(t *testing.T) {
		r := NewCodingMemoryRail(t.TempDir(), nil, "cn")
		if got := r.countMemoryFiles(ctx); got != 0 {
			t.Fatalf("应返回 0，实际 %d", got)
		}
	})

	t.Run("只统计非 MEMORY.md 的 md 文件", func(t *testing.T) {
		dir := t.TempDir()
		for _, name := range []string{"a.md", "b.MD", "MEMORY.md", "note.txt"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
				t.Fatalf("写入 %s 失败: %v", name, err)
			}
		}
		if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
			t.Fatalf("创建子目录失败: %v", err)
		}
		r := newRailWithDir(t, dir)
		if got := r.countMemoryFiles(ctx); got != 2 {
			t.Fatalf("应统计 2 个，实际 %d", got)
		}
	})

	t.Run("空目录返回 0", func(t *testing.T) {
		r := newRailWithDir(t, t.TempDir())
		if got := r.countMemoryFiles(ctx); got != 0 {
			t.Fatalf("应返回 0，实际 %d", got)
		}
	})
}
