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

// TestGetCwdE_无CwdState返回进程cwd 验证包级函数在 ctx 无绑定时的正常路径。
func TestGetCwdE_无CwdState返回进程cwd(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	got, err := GetCwdE(context.Background())
	require.NoError(t, err)
	assert.Equal(t, wd, got)
}

// TestGetCwdE_有CwdState返回状态值 验证包级函数委托给 CwdState。
func TestGetCwdE_有CwdState返回状态值(t *testing.T) {
	dir := t.TempDir()
	state := InitCwd(dir)
	ctx := WithCwdState(context.Background(), state)

	got, err := GetCwdE(ctx)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(dir), got)
}

// TestGetCwdE_osGetwd失败传播error 验证 os.Getwd() 的 error 不再被吞掉。
//
// 构造方式：Chdir 到一个随后被删除的目录，此时 os.Getwd() 会失败。
// 用 defer 恢复原 cwd，避免污染同包其他测试。
func TestGetCwdE_osGetwd失败传播error(t *testing.T) {
	origWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origWd) }()

	deleted := t.TempDir()
	require.NoError(t, os.Chdir(deleted))
	require.NoError(t, os.RemoveAll(deleted))

	// 确认 os.Getwd() 确实失败
	_, wdErr := os.Getwd()
	require.Error(t, wdErr, "os.Getwd() 应在已删除目录下失败")

	// 包级函数：ctx 无 CwdState 时应把 error 传播出来
	got, err := GetCwdE(context.Background())
	assert.Error(t, err)
	assert.Empty(t, got)
}

// TestGetCwdE_CwdState三级回退到失败 验证 CwdState 方法层的 error 传播。
func TestGetCwdE_CwdState三级回退到失败(t *testing.T) {
	origWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origWd) }()

	deleted := t.TempDir()
	require.NoError(t, os.Chdir(deleted))
	require.NoError(t, os.RemoveAll(deleted))

	// InitCwd("") 的三字段全空，三级回退最后落到 os.Getwd()
	state := InitCwd("")
	ctx := WithCwdState(context.Background(), state)

	got, err := GetCwdE(ctx)
	assert.Error(t, err)
	assert.Empty(t, got)
}

// TestGetCwd_失败时仍返回空串 验证既有函数行为完全不变（向后兼容）。
func TestGetCwd_失败时仍返回空串(t *testing.T) {
	origWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origWd) }()

	deleted := t.TempDir()
	require.NoError(t, os.Chdir(deleted))
	require.NoError(t, os.RemoveAll(deleted))

	// 既有签名不返回 error，仍按原实现返回空串
	got := GetCwd(context.Background())
	assert.Empty(t, got)
}

// TestGetProjectRootE_无CwdState返回进程cwd 验证正常路径。
func TestGetProjectRootE_无CwdState返回进程cwd(t *testing.T) {
	wd, err := os.Getwd()
	require.NoError(t, err)

	got, err := GetProjectRootE(context.Background())
	require.NoError(t, err)
	assert.Equal(t, wd, got)
}

// TestGetProjectRootE_有CwdState返回projectRoot 验证委托与优先级。
func TestGetProjectRootE_有CwdState返回projectRoot(t *testing.T) {
	dir := t.TempDir()
	root := t.TempDir()
	state := InitCwd(dir, WithProjectRoot(root))
	ctx := WithCwdState(context.Background(), state)

	got, err := GetProjectRootE(ctx)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean(root), got)
}

// TestGetProjectRootE_osGetwd失败传播error 验证 error 传播。
func TestGetProjectRootE_osGetwd失败传播error(t *testing.T) {
	origWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origWd) }()

	deleted := t.TempDir()
	require.NoError(t, os.Chdir(deleted))
	require.NoError(t, os.RemoveAll(deleted))

	got, err := GetProjectRootE(context.Background())
	assert.Error(t, err)
	assert.Empty(t, got)
}

// TestGetProjectRootE_CwdState三级回退到失败 验证方法层 error 传播。
func TestGetProjectRootE_CwdState三级回退到失败(t *testing.T) {
	origWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origWd) }()

	deleted := t.TempDir()
	require.NoError(t, os.Chdir(deleted))
	require.NoError(t, os.RemoveAll(deleted))

	state := InitCwd("")
	ctx := WithCwdState(context.Background(), state)

	got, err := GetProjectRootE(ctx)
	assert.Error(t, err)
	assert.Empty(t, got)
}

// TestGetProjectRoot_失败时仍返回空串 验证既有函数行为不变。
func TestGetProjectRoot_失败时仍返回空串(t *testing.T) {
	origWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origWd) }()

	deleted := t.TempDir()
	require.NoError(t, os.Chdir(deleted))
	require.NoError(t, os.RemoveAll(deleted))

	got := GetProjectRoot(context.Background())
	assert.Empty(t, got)
}

// TestGetCwdE_前两级命中不调用osGetwd 验证有值时不会因 cwd 已删而失败。
func TestGetCwdE_前两级命中不调用osGetwd(t *testing.T) {
	origWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origWd) }()

	deleted := t.TempDir()
	require.NoError(t, os.Chdir(deleted))
	require.NoError(t, os.RemoveAll(deleted))

	// 显式 cwd 非空 → 不会落到 os.Getwd()
	state := InitCwd("/some/explicit/cwd")
	ctx := WithCwdState(context.Background(), state)

	got, err := GetCwdE(ctx)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean("/some/explicit/cwd"), got)
}

// TestGetProjectRootE_前两级命中不调用osGetwd 验证同上。
func TestGetProjectRootE_前两级命中不调用osGetwd(t *testing.T) {
	origWd, err := os.Getwd()
	require.NoError(t, err)
	defer func() { _ = os.Chdir(origWd) }()

	deleted := t.TempDir()
	require.NoError(t, os.Chdir(deleted))
	require.NoError(t, os.RemoveAll(deleted))

	state := InitCwd("/some/explicit/cwd")
	ctx := WithCwdState(context.Background(), state)

	got, err := GetProjectRootE(ctx)
	require.NoError(t, err)
	assert.Equal(t, filepath.Clean("/some/explicit/cwd"), got)
}
