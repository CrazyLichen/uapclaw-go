package cwd

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestGetCwd_无CwdState返回进程cwd 验证 ctx 无绑定时的正常路径。
func TestGetCwd_无CwdState返回进程cwd(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	got, err := GetCwd(context.Background())
	require.NoError(t, err)
	assert.Equal(t, wd, got)
}

// TestGetCwd_有CwdState返回状态值 验证三级回退第一级命中。
func TestGetCwd_有CwdState返回状态值(t *testing.T) {
	dir := t.TempDir()
	state := InitCwd(dir)
	ctx := WithCwdState(context.Background(), state)

	got, err := GetCwd(ctx)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(dir), got)
}

// deletedCwd 把进程切到一个随后被删除的目录，使 os.Getwd() 失败。
// 返回恢复函数（务必 defer 调用，避免污染同包其他测试）。
func deletedCwd(t *testing.T) func() {
	t.Helper()
	origWd, err := os.Getwd()
	require.NoError(t, err)
	deleted := t.TempDir()
	require.NoError(t, os.Chdir(deleted))
	require.NoError(t, os.RemoveAll(deleted))
	return func() { _ = os.Chdir(origWd) }
}

// TestGetCwd_osGetwd失败返回error 验证 os.Getwd() 的 error 被传播。
//
// 对应 Python get_cwd() 最后一级 os.getcwd() 抛 OSError。
func TestGetCwd_osGetwd失败返回error(t *testing.T) {
	defer deletedCwd(t)()

	_, wdErr := os.Getwd()
	require.Error(t, wdErr, "os.Getwd() 应在已删除目录下失败")

	got, err := GetCwd(context.Background())
	assert.Error(t, err)
	assert.Empty(t, got)
}

// TestGetCwd_CwdState三级回退到失败 验证 CwdState 三字段全空时落到 os.Getwd()。
func TestGetCwd_CwdState三级回退到失败(t *testing.T) {
	defer deletedCwd(t)()

	state := InitCwd("") // cwd / originalCwd / projectRoot 全空
	ctx := WithCwdState(context.Background(), state)

	got, err := GetCwd(ctx)
	assert.Error(t, err)
	assert.Empty(t, got)
}

// TestGetCwd_前两级命中不调用osGetwd 验证有值时不会因 cwd 已删而失败。
func TestGetCwd_前两级命中不调用osGetwd(t *testing.T) {
	defer deletedCwd(t)()

	state := InitCwd("/some/explicit/cwd")
	ctx := WithCwdState(context.Background(), state)

	got, err := GetCwd(ctx)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean("/some/explicit/cwd"), got)
}

// TestGetCwd_第二级originalCwd命中 验证 cwd 为空但 originalCwd 非空。
func TestGetCwd_第二级originalCwd命中(t *testing.T) {
	defer deletedCwd(t)()

	state := InitCwd("")
	state.SetOriginalCwd("/fallback/original")
	ctx := WithCwdState(context.Background(), state)

	got, err := GetCwd(ctx)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean("/fallback/original"), got)
}

// TestGetProjectRoot_无CwdState返回进程cwd 验证正常路径。
func TestGetProjectRoot_无CwdState返回进程cwd(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	got, err := GetProjectRoot(context.Background())
	require.NoError(t, err)
	assert.Equal(t, wd, got)
}

// TestGetProjectRoot_有CwdState返回projectRoot 验证第一级优先级。
func TestGetProjectRoot_有CwdState返回projectRoot(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	state := InitCwd(dir, WithProjectRoot(root))
	ctx := WithCwdState(context.Background(), state)

	got, err := GetProjectRoot(ctx)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(root), got)
}

// TestGetProjectRoot_osGetwd失败返回error 验证 error 传播。
func TestGetProjectRoot_osGetwd失败返回error(t *testing.T) {
	defer deletedCwd(t)()

	got, err := GetProjectRoot(context.Background())
	assert.Error(t, err)
	assert.Empty(t, got)
}

// TestGetProjectRoot_CwdState三级回退到失败 验证方法层 error 传播。
func TestGetProjectRoot_CwdState三级回退到失败(t *testing.T) {
	defer deletedCwd(t)()

	state := InitCwd("")
	ctx := WithCwdState(context.Background(), state)

	got, err := GetProjectRoot(ctx)
	assert.Error(t, err)
	assert.Empty(t, got)
}

// TestGetProjectRoot_前两级命中不调用osGetwd 验证 projectRoot 优先。
func TestGetProjectRoot_前两级命中不调用osGetwd(t *testing.T) {
	defer deletedCwd(t)()

	state := InitCwd("/some/explicit/cwd")
	ctx := WithCwdState(context.Background(), state)

	got, err := GetProjectRoot(ctx)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean("/some/explicit/cwd"), got)
}

// TestResolveCwd_返回error 验证连锁函数也传播 error。
func TestResolveCwd_返回error(t *testing.T) {
	defer deletedCwd(t)()

	_, err := ResolveCwd(context.Background(), "")
	assert.Error(t, err)
}

// TestResolveCwd_绝对路径不读Cwd 验证绝对路径分支不受 os.Getwd() 影响。
func TestResolveCwd_绝对路径不读Cwd(t *testing.T) {
	defer deletedCwd(t)()

	got, err := ResolveCwd(context.Background(), "/abs/path")
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean("/abs/path"), got)
}

// TestResolvePath_返回error 验证连锁函数也传播 error。
func TestResolvePath_返回error(t *testing.T) {
	defer deletedCwd(t)()

	_, err := ResolvePath(context.Background(), "relative.txt")
	assert.Error(t, err)
}

// TestResolvePath_绝对路径不读Cwd 验证绝对路径分支不受影响。
func TestResolvePath_绝对路径不读Cwd(t *testing.T) {
	defer deletedCwd(t)()

	got, err := ResolvePath(context.Background(), "/abs/file.txt")
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean("/abs/file.txt"), got)
}
