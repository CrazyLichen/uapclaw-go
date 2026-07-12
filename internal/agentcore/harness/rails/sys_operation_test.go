//go:build test

package rails

import (
	"context"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/shell"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeDeepAgentForSysOperation SysOperationRail 测试用 fakeBaseAgent 包装
// 注意：不实现完整 DeepAgentInterface（接口太大），
// Init 内部类型断言 agent.(hinterfaces.DeepAgentInterface) 不匹配时跳过 DeepConfig 读取。
type fakeDeepAgentForSysOperation struct {
	*fakeBaseAgent
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func newFakeDeepAgentForSysOperation() *fakeDeepAgentForSysOperation {
	return &fakeDeepAgentForSysOperation{fakeBaseAgent: newFakeBaseAgent()}
}

// TestNewSysOperationRail_默认值 测试默认构造
func TestNewSysOperationRail_默认值(t *testing.T) {
	t.Parallel()
	r := NewSysOperationRail()
	assert.NotNil(t, r)
	assert.Equal(t, sysOpRailPriority, r.Priority())
	assert.False(t, r.withCodeTool)
	assert.False(t, r.readOnly)
	assert.Equal(t, shell.PermissionModeAuto, r.permissionMode)
	assert.Nil(t, r.enableReadImageMultimodal)
	assert.Nil(t, r.denyPatterns)
	assert.Nil(t, r.allowPatterns)
	assert.Empty(t, r.tools)
}

// TestNewSysOperationRail_WithOptions 测试选项构造
func TestNewSysOperationRail_WithOptions(t *testing.T) {
	t.Parallel()
	r := NewSysOperationRail(
		WithCodeTool(true),
		WithReadOnly(true),
		WithPermissionMode(shell.PermissionModeBypass),
		WithDenyPatterns([]string{"rm -rf"}),
		WithAllowPatterns([]string{"ls", "cat"}),
	)
	assert.True(t, r.withCodeTool)
	assert.True(t, r.readOnly)
	assert.Equal(t, shell.PermissionModeBypass, r.permissionMode)
	assert.Equal(t, []string{"rm -rf"}, r.denyPatterns)
	assert.Equal(t, []string{"ls", "cat"}, r.allowPatterns)
}

// TestWithEnableReadImageMultimodal 测试图片多模态选项
func TestWithEnableReadImageMultimodal(t *testing.T) {
	t.Parallel()
	r := NewSysOperationRail(WithEnableReadImageMultimodal(false))
	require.NotNil(t, r.enableReadImageMultimodal)
	assert.False(t, *r.enableReadImageMultimodal)
}

// TestSysOperationRail_Init注册工具 测试 Init 注册工具数量
func TestSysOperationRail_Init注册工具(t *testing.T) {
	t.Parallel()
	agent := newFakeDeepAgentForSysOperation()
	r := NewSysOperationRail()
	err := r.Init(agent)
	require.NoError(t, err)
	// 非 readOnly 模式: read + write + edit + glob + listDir + grep + bash = 7
	// Windows 额外 +1 powershell
	expectedCount := 7
	if runtime.GOOS == "windows" {
		expectedCount = 8
	}
	assert.Equal(t, expectedCount, len(r.tools))
}

// TestSysOperationRail_InitReadOnly 测试只读模式注册工具
func TestSysOperationRail_InitReadOnly(t *testing.T) {
	t.Parallel()
	agent := newFakeDeepAgentForSysOperation()
	r := NewSysOperationRail(WithReadOnly(true))
	err := r.Init(agent)
	require.NoError(t, err)
	// readOnly 模式: read + glob + listDir + grep + bash = 5
	expectedCount := 5
	if runtime.GOOS == "windows" {
		expectedCount = 6
	}
	assert.Equal(t, expectedCount, len(r.tools))
}

// TestSysOperationRail_InitWithCodeTool 测试 CodeTool 注册
func TestSysOperationRail_InitWithCodeTool(t *testing.T) {
	t.Parallel()
	agent := newFakeDeepAgentForSysOperation()
	r := NewSysOperationRail(WithCodeTool(true))
	err := r.Init(agent)
	require.NoError(t, err)
	// 非 readOnly + codeTool: read + write + edit + glob + listDir + grep + bash + code = 8
	expectedCount := 8
	if runtime.GOOS == "windows" {
		expectedCount = 9
	}
	assert.Equal(t, expectedCount, len(r.tools))
}

// TestSysOperationRail_InitWithCodeToolReadOnly CodeTool 在只读模式下不注册
func TestSysOperationRail_InitWithCodeToolReadOnly(t *testing.T) {
	t.Parallel()
	agent := newFakeDeepAgentForSysOperation()
	r := NewSysOperationRail(WithCodeTool(true), WithReadOnly(true))
	err := r.Init(agent)
	require.NoError(t, err)
	// readOnly 时 codeTool 不注册，同 TestSysOperationRail_InitReadOnly
	expectedCount := 5
	if runtime.GOOS == "windows" {
		expectedCount = 6
	}
	assert.Equal(t, expectedCount, len(r.tools))
}

// TestSysOperationRail_Uninit 测试注销
func TestSysOperationRail_Uninit(t *testing.T) {
	t.Parallel()
	agent := newFakeDeepAgentForSysOperation()
	r := NewSysOperationRail()
	err := r.Init(agent)
	require.NoError(t, err)
	assert.NotEmpty(t, r.tools)

	err = r.Uninit(agent)
	require.NoError(t, err)
	assert.Nil(t, r.tools)
}

// TestSysOperationRail_Uninit空工具不报错 测试空工具列表注销
func TestSysOperationRail_Uninit空工具不报错(t *testing.T) {
	t.Parallel()
	agent := newFakeDeepAgentForSysOperation()
	r := NewSysOperationRail()
	// 不调用 Init，tools 为空
	err := r.Uninit(agent)
	require.NoError(t, err)
}

// TestSysOperationRail_BeforeInvoke 测试 BeforeInvoke 返回 nil
func TestSysOperationRail_BeforeInvoke(t *testing.T) {
	t.Parallel()
	r := NewSysOperationRail()
	err := r.BeforeInvoke(context.Background(), nil)
	assert.Nil(t, err)
}

// TestSysOperationRail_AfterInvoke 测试 AfterInvoke 返回 nil
func TestSysOperationRail_AfterInvoke(t *testing.T) {
	t.Parallel()
	r := NewSysOperationRail()
	err := r.AfterInvoke(context.Background(), nil)
	assert.Nil(t, err)
}
