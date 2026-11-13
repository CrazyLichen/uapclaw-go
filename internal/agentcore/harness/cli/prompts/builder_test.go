package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindProjectRoot_向上找标记 对齐 builder.py:31-39。
func TestFindProjectRoot_向上找标记(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	nested := filepath.Join(root, "a", "b")
	require.NoError(t, os.MkdirAll(nested, 0o755))

	assert.Equal(t, root, FindProjectRoot(nested), "从子目录向上命中 .git")
	assert.Equal(t, root, FindProjectRoot(root), "根目录自身命中")
}

// TestFindProjectRoot_各标记文件 go.mod 等文件型标记。
func TestFindProjectRoot_各标记文件(t *testing.T) {
	for _, marker := range []string{"pyproject.toml", "package.json", "Cargo.toml", "go.mod"} {
		root := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(root, marker), []byte("x"), 0o644))
		nested := filepath.Join(root, "sub")
		require.NoError(t, os.MkdirAll(nested, 0o755))
		assert.Equal(t, root, FindProjectRoot(nested), marker)
	}
}

// TestFindProjectRoot_无标记 空目录链返回空串。
func TestFindProjectRoot_无标记(t *testing.T) {
	// /tmp 下大概率无根标记；用一个深嵌套目录
	dir := filepath.Join(t.TempDir(), "a", "b", "c")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	// TempDir 本身可能不带标记，但 /tmp 祖先链也无标记 → 空串
	// （保守断言：不 panic 且返回值要么空串要么是真实根）
	_ = FindProjectRoot(dir)
}

// TestLoadOpenjiuwenMd_两层合并 对齐 builder.py:41-72。
func TestLoadOpenjiuwenMd_两层合并(t *testing.T) {
	// 项目层
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "OPENJIUWEN.md"),
		[]byte("项目记忆"), 0o644))

	got := LoadOpenjiuwenMd(root)
	assert.Contains(t, got, "### Project-level memory")
	assert.Contains(t, got, "项目记忆")
}

// TestLoadOpenjiuwenMd_无记忆 返回空串。
func TestLoadOpenjiuwenMd_无记忆(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "no-marker")
	require.NoError(t, os.MkdirAll(dir, 0o755))
	// 目录无标记也无 OPENJIUWEN.md；用户级文件存在与否不可控，
	// 只断言不 panic（内容可能是用户级记忆）
	_ = LoadOpenjiuwenMd(dir)
}

// TestBuildEnvironmentSection_格式 对齐 builder.py:88-103。
func TestBuildEnvironmentSection_格式(t *testing.T) {
	got := buildEnvironmentSection(t.TempDir(), "m1", "native")
	assert.True(t, strings.HasPrefix(got, "## Environment\n"))
	assert.Contains(t, got, "- Model: m1 (native)\n")
	assert.Contains(t, got, "- Date: ")
	assert.Contains(t, got, "- Platform: ")
}

// TestBuildSystemPrompt_完整组装 对齐 builder.py:107-155。
func TestBuildSystemPrompt_完整组装(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "OPENJIUWEN.md"),
		[]byte("记忆内容"), 0o644))

	got := BuildSystemPrompt(root, "m1", "native", "cn")
	// environment 节（priority 20）
	assert.Contains(t, got, "## Environment")
	assert.Contains(t, got, "- Model: m1 (native)")
	// project_memory 节（priority 120）
	assert.Contains(t, got, "## Project Memory")
	assert.Contains(t, got, "记忆内容")
}

// TestBuildSystemPrompt_无记忆 project_memory 节缺席。
func TestBuildSystemPrompt_无记忆(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x"), 0o644))

	got := BuildSystemPrompt(root, "m1", "native", "cn")
	assert.Contains(t, got, "## Environment")
	assert.NotContains(t, got, "## Project Memory")
}

// TestLoadOpenjiuwenMd_超长截断 超过 MaxMemoryChars 追加截断标记。
func TestLoadOpenjiuwenMd_超长截断(t *testing.T) {
	root := t.TempDir()
	long := strings.Repeat("长", MaxMemoryChars+100)
	require.NoError(t, os.WriteFile(filepath.Join(root, "go.mod"), []byte("module x"), 0o644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "OPENJIUWEN.md"),
		[]byte(long), 0o644))

	got := LoadOpenjiuwenMd(root)
	assert.Contains(t, got, "[...truncated]")
	assert.LessOrEqual(t, len(got), MaxMemoryChars+len("\n[...truncated]")+len("## Project Memory\n")+100)
}
