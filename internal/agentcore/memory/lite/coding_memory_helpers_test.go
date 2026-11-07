package lite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/wk8/go-ordered-map/v2"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	_ "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/local"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// newLocalSysOp 在 root 下建一个不限制沙箱的本地 SysOperation。
func newLocalSysOp(t *testing.T, root string) sysop.SysOperation {
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

// newCodingCtx 构造带真实文件系统的编程记忆工具上下文。
func newCodingCtx(t *testing.T) (*CodingMemoryToolContext, string) {
	t.Helper()
	dir := t.TempDir()
	op := newLocalSysOp(t, dir)
	ctx := NewCodingMemoryToolContext().WithSysOperation(op)
	ctx.CodingMemoryDir = dir
	return ctx, dir
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────
// TestSearchSimilar 验证相似记忆检索的边界分支。
func TestSearchSimilar(t *testing.T) {
	ctx := context.Background()

	t.Run("上下文为 nil 返回空 map", func(t *testing.T) {
		got := searchSimilar(ctx, nil, "body", "self.md", 5, 0.5)
		if got == nil || got.Len() != 0 {
			t.Fatalf("应返回空有序 map，实际 %v", got)
		}
	})

	t.Run("Manager 为 nil 返回空 map", func(t *testing.T) {
		got := searchSimilar(ctx, &CodingMemoryToolContext{}, "body", "self.md", 5, 0.5)
		if got.Len() != 0 {
			t.Fatalf("应返回空 map，实际 %v", got)
		}
	})
}

// TestRunChecker 验证无模型时直接返回空动作列表。
func TestRunChecker(t *testing.T) {
	got := runChecker(context.Background(), nil, "id-1", "body", orderedmap.New[string, string]())
	if got == nil {
		t.Fatal("应返回非 nil 空切片")
	}
	if len(got) != 0 {
		t.Fatalf("无模型时应返回空列表，实际 %v", got)
	}
}

// TestSnapshotMemoryFiles 验证记忆文件快照的过滤规则。
func TestSnapshotMemoryFiles(t *testing.T) {
	ctx := context.Background()

	t.Run("空目录路径返回 nil", func(t *testing.T) {
		toolCtx, _ := newCodingCtx(t)
		if got := snapshotMemoryFiles(ctx, toolCtx, ""); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})

	t.Run("无 SysOperation 返回 nil", func(t *testing.T) {
		if got := snapshotMemoryFiles(ctx, &CodingMemoryToolContext{}, t.TempDir()); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})

	t.Run("只收集非 MEMORY.md 的 md 文件", func(t *testing.T) {
		toolCtx, dir := newCodingCtx(t)
		for _, name := range []string{"a.md", "b.MD", "MEMORY.md", "note.txt"} {
			if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
				t.Fatalf("写入 %s 失败: %v", name, err)
			}
		}
		if err := os.MkdirAll(filepath.Join(dir, "sub"), 0o755); err != nil {
			t.Fatalf("创建子目录失败: %v", err)
		}
		got := snapshotMemoryFiles(ctx, toolCtx, dir)
		if len(got) != 2 {
			t.Fatalf("应收集 2 个 md 文件，实际 %v", got)
		}
		if !got["a.md"] || !got["b.MD"] {
			t.Fatalf("快照内容不符: %v", got)
		}
	})

	t.Run("目录不存在返回空快照", func(t *testing.T) {
		// 本地 Fs().ListFiles 对不存在的目录不报错而是回空列表，
		// 故此处得到空 map 而非 nil——按实际行为断言。
		toolCtx, dir := newCodingCtx(t)
		if got := snapshotMemoryFiles(ctx, toolCtx, filepath.Join(dir, "missing")); len(got) != 0 {
			t.Fatalf("应返回空快照，实际 %v", got)
		}
	})
} // TestUpsertMemoryIndex 验证 MEMORY.md 索引的插入、更新与裁剪。
func TestUpsertMemoryIndex(t *testing.T) {
	ctx := context.Background()

	t.Run("name 或 description 为空时跳过", func(t *testing.T) {
		toolCtx, dir := newCodingCtx(t)
		upsertMemoryIndex(ctx, toolCtx, dir, "a.md", map[string]string{"name": "", "description": "d"})
		if _, err := os.Stat(filepath.Join(dir, "MEMORY.md")); !os.IsNotExist(err) {
			t.Fatal("跳过时不应创建 MEMORY.md")
		}
	})

	t.Run("首次插入新条目", func(t *testing.T) {
		toolCtx, dir := newCodingCtx(t)
		upsertMemoryIndex(ctx, toolCtx, dir, "a.md", map[string]string{"name": "A", "description": "da"})
		data, err := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
		if err != nil {
			t.Fatalf("读取索引失败: %v", err)
		}
		if want := "- [A](a.md) — da"; !strings.Contains(string(data), want) {
			t.Fatalf("索引内容 %q 应含 %q", string(data), want)
		}
	})

	t.Run("已存在条目被就地更新", func(t *testing.T) {
		toolCtx, dir := newCodingCtx(t)
		upsertMemoryIndex(ctx, toolCtx, dir, "a.md", map[string]string{"name": "A", "description": "old"})
		upsertMemoryIndex(ctx, toolCtx, dir, "a.md", map[string]string{"name": "A2", "description": "new"})
		data, _ := os.ReadFile(filepath.Join(dir, "MEMORY.md"))
		if !strings.Contains(string(data), "- [A2](a.md) — new") {
			t.Fatalf("应更新为新描述，实际 %q", string(data))
		}
		if strings.Contains(string(data), "old") {
			t.Fatalf("旧描述应被替换，实际 %q", string(data))
		}
	})

	t.Run("无 SysOperation 时静默返回", func(t *testing.T) {
		upsertMemoryIndex(ctx, &CodingMemoryToolContext{}, t.TempDir(), "a.md",
			map[string]string{"name": "A", "description": "d"})
	})
}

// TestAppendToExistingFile 验证追加内容并刷新 frontmatter 时间。
func TestAppendToExistingFile(t *testing.T) {
	ctx := context.Background()

	t.Run("追加到已有文件", func(t *testing.T) {
		toolCtx, dir := newCodingCtx(t)
		path := filepath.Join(dir, "a.md")
		if err := os.WriteFile(path, []byte("---\nname: A\ndescription: d\ntype: user\n---\nbody"), 0o644); err != nil {
			t.Fatalf("写入文件失败: %v", err)
		}
		appendToExistingFile(ctx, toolCtx, path, "appended", map[string]string{"name": "A"})
		data, _ := os.ReadFile(path)
		if !strings.Contains(string(data), "appended") {
			t.Fatalf("追加内容应存在，实际 %q", string(data))
		}
	})

	t.Run("无 SysOperation 时静默返回", func(t *testing.T) {
		appendToExistingFile(ctx, &CodingMemoryToolContext{}, filepath.Join(t.TempDir(), "a.md"), "x", nil)
	})

	t.Run("目标文件不存在时不 panic", func(t *testing.T) {
		toolCtx, dir := newCodingCtx(t)
		appendToExistingFile(ctx, toolCtx, filepath.Join(dir, "missing.md"), "x", nil)
	})
}

// TestReadFileSafe 验证安全读取的各返回分支。
func TestReadFileSafe(t *testing.T) {
	ctx := context.Background()

	t.Run("正常读取", func(t *testing.T) {
		toolCtx, dir := newCodingCtx(t)
		path := filepath.Join(dir, "a.md")
		if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
		if got := readFileSafe(ctx, toolCtx, path); got != "hello" {
			t.Fatalf("内容 = %q, 期望 hello", got)
		}
	})

	t.Run("无 SysOperation 返回空串", func(t *testing.T) {
		if got := readFileSafe(ctx, &CodingMemoryToolContext{}, "/any"); got != "" {
			t.Fatalf("应返回空串，实际 %q", got)
		}
	})

	t.Run("文件不存在返回空串", func(t *testing.T) {
		toolCtx, dir := newCodingCtx(t)
		if got := readFileSafe(ctx, toolCtx, filepath.Join(dir, "nope.md")); got != "" {
			t.Fatalf("应返回空串，实际 %q", got)
		}
	})
}
