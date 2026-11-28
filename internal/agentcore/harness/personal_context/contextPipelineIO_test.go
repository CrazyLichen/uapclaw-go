package personal_context

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDigest_稳定32位(t *testing.T) {
	require.Len(t, Digest("abc"), 32)
	require.Equal(t, Digest("abc"), Digest("abc"))
	require.NotEqual(t, Digest("abc"), Digest("abd"))
}

func TestSplitBlocks_段落与上限(t *testing.T) {
	blocks := SplitBlocks("# A\n\npara one\n\npara two")
	require.Len(t, blocks, 3)
	// 超长段落按 maxBlockChars 切
	long := make([]rune, maxBlockChars+10)
	for i := range long {
		long[i] = 'a'
	}
	blocks = SplitBlocks(string(long))
	require.Len(t, blocks, 2)
	require.Len(t, []rune(blocks[0]), maxBlockChars)
	// 空输入
	require.Equal(t, []string{""}, SplitBlocks(""))
}

func TestNormalizeMarkdown_规范化(t *testing.T) {
	out := NormalizeMarkdown("a\x00b\r\nline1   \rline2")
	require.Equal(t, "ab\nline1\nline2\n", out)
}

func TestDeterministicBriefingPreview_大纲与首段(t *testing.T) {
	content := "---\nfront: matter\n---\n\n# 标题一\n\n## 标题二\n\n首段内容第一行\n首段内容第二行\n\n```\n围栏 # 不是标题\n```\n"
	preview := DeterministicBriefingPreview(content)
	require.Len(t, preview["headings"].([]map[string]any), 2)
	summary := preview["summary"].(string)
	require.Contains(t, summary, "首段内容第一行")
	require.NotContains(t, summary, "不是标题")
}

func TestCancelSafeToThread_取消等待落定(t *testing.T) {
	ctx := context.Background()
	settled := make(chan struct{})
	err := CancelSafeToThread(ctx, func() error {
		time.Sleep(20 * time.Millisecond)
		close(settled)
		return nil
	})
	require.NoError(t, err)
	<-settled

	// 立即取消：等待 goroutine 落定后返回 ctx.Err()
	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	err = CancelSafeToThread(ctx2, func() error { return nil })
	require.ErrorIs(t, err, context.Canceled)
}

func TestCancelCooperativeToThread_轮询标志(t *testing.T) {
	ctx := context.Background()
	err := CancelCooperativeToThread(ctx, func(cancelRequested func() bool) error {
		require.False(t, cancelRequested())
		return nil
	})
	require.NoError(t, err)

	ctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	err = CancelCooperativeToThread(ctx2, func(cancelRequested func() bool) error {
		require.True(t, cancelRequested())
		return nil
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestWalkTreePaths_确定性顺序(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "b"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "b", "f.md"), []byte("x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "e.md"), []byte("x"), 0o644))
	paths := walkTreePaths(root)
	var relative []string
	for _, p := range paths {
		rel, _ := filepath.Rel(root, p)
		relative = append(relative, filepath.ToSlash(rel))
	}
	require.Equal(t, []string{"a", "a/e.md", "b", "b/f.md"}, relative)
}

func TestPipelineAtomicWrite_原子替换与清理(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "page.md")
	require.NoError(t, pipelineAtomicWrite(target, []byte("v1")))
	require.NoError(t, pipelineAtomicWrite(target, []byte("v2")))
	data, _ := os.ReadFile(target)
	require.Equal(t, "v2", string(data))
	entries, _ := os.ReadDir(root)
	require.Len(t, entries, 1, "无临时文件残留")
}

func TestAssertNoSymlinks_拒绝(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "sub"), 0o755))
	require.NoError(t, assertNoSymlinks(root))
	require.NoError(t, os.Symlink(root, filepath.Join(root, "sub", "loop")))
	require.ErrorContains(t, assertNoSymlinks(root), "must not contain symlinks")
}

func TestAssertPipelinePathChainNoSymlinks_父链(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	require.NoError(t, os.Mkdir(sub, 0o755))
	require.NoError(t, assertPipelinePathChainNoSymlinks(sub))
	require.NoError(t, os.Symlink(root, filepath.Join(root, "link")))
	require.ErrorContains(t, assertPipelinePathChainNoSymlinks(filepath.Join(root, "link")), "traverse symlinks")
}

func TestCopyTree_完整复制(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "src")
	target := filepath.Join(root, "dst")
	require.NoError(t, os.MkdirAll(filepath.Join(source, "nested"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(source, "a.md"), []byte("A"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(source, "nested", "b.md"), []byte("B"), 0o644))
	require.NoError(t, copyTree(source, target))
	data, _ := os.ReadFile(filepath.Join(target, "nested", "b.md"))
	require.Equal(t, "B", string(data))
	// 不存在源 → 只建目标目录
	require.NoError(t, copyTree(filepath.Join(root, "missing"), filepath.Join(root, "empty")))
	require.True(t, pipelinePathIsDir(filepath.Join(root, "empty")))
}

func TestCommitContextTree_增量发布与清理(t *testing.T) {
	root := t.TempDir()
	candidate := filepath.Join(root, "candidate")
	target := filepath.Join(root, "published")
	require.NoError(t, os.MkdirAll(filepath.Join(candidate, "old"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(target, "old"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(candidate, "description.md"), []byte("# 新根\n"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(candidate, "new.md"), []byte("新增"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(target, "old", "stale.md"), []byte("旧"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(target, "description.md"), []byte("# 旧根\n"), 0o644))

	require.NoError(t, commitContextTree(candidate, target))
	data, _ := os.ReadFile(filepath.Join(target, "description.md"))
	require.Equal(t, "# 新根\n", string(data))
	data, _ = os.ReadFile(filepath.Join(target, "new.md"))
	require.Equal(t, "新增", string(data))
	_, err := os.Stat(filepath.Join(target, "old", "stale.md"))
	require.True(t, os.IsNotExist(err), "废弃文件应清理")
	_, err = os.Stat(filepath.Join(target, "old"))
	require.True(t, os.IsNotExist(err), "空目录应移除")
}

func TestValidatedRelativePath_拒绝(t *testing.T) {
	out, err := validatedRelativePath("a/b/c.md", "page")
	require.NoError(t, err)
	require.Equal(t, "a/b/c.md", out)
	_, err = validatedRelativePath("a\\b.md", "page") // 反斜杠归一为斜杠
	require.NoError(t, err)
	for _, bad := range []string{"", "/abs.md", "C:/x.md", "a/../b.md", "a//b.md", "."} {
		_, err = validatedRelativePath(bad, "page")
		require.Error(t, err, bad)
	}
}

func TestRemoveEmptyDirectories_自深而浅(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "a", "b", "c"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, "a", "keep.md"), []byte("k"), 0o644))
	require.NoError(t, removeEmptyDirectories(root))
	require.True(t, pipelinePathIsDir(filepath.Join(root, "a")))
	require.False(t, pipelinePathIsDir(filepath.Join(root, "a", "b")))
}
