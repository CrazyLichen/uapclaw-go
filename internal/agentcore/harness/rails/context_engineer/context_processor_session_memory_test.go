package context_engineer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 构造 + 选项测试 ────────────────────────────

// TestNewContextProcessorRail_默认值 验证默认构造
func TestNewContextProcessorRail_默认值(t *testing.T) {
	r := NewContextProcessorRail()
	assert.True(t, r.preset)
	assert.False(t, r.sessionMemoryEnabled)
	assert.Nil(t, r.sessionMemoryConfig)
	assert.Nil(t, r.sessionMemoryMgr)
}

// TestWithSessionMemoryEnabled 验证 WithSessionMemoryEnabled 选项
func TestWithSessionMemoryEnabled(t *testing.T) {
	r := NewContextProcessorRail(WithSessionMemoryEnabled(true))
	assert.True(t, r.sessionMemoryEnabled)
	assert.Nil(t, r.sessionMemoryConfig) // 仅启用标志，未设 config
}

// TestWithSessionMemoryConfig 验证 WithSessionMemoryConfig 选项。
// 对齐 Python: session_memory=config → 自动创建 SessionMemoryManager
func TestWithSessionMemoryConfig(t *testing.T) {
	cfg := cecontext.NewSessionMemoryConfig()
	r := NewContextProcessorRail(WithSessionMemoryConfig(&cfg))

	assert.True(t, r.sessionMemoryEnabled, "传入 config 应自动启用 sessionMemoryEnabled")
	assert.NotNil(t, r.sessionMemoryConfig)
	assert.NotNil(t, r.sessionMemoryMgr, "传入 config 应自动创建 SessionMemoryManager")
}

// TestWithSessionMemoryConfig_Nil 验证 nil config 不启用
func TestWithSessionMemoryConfig_Nil(t *testing.T) {
	r := NewContextProcessorRail(WithSessionMemoryConfig(nil))
	assert.False(t, r.sessionMemoryEnabled)
	assert.Nil(t, r.sessionMemoryConfig)
	assert.Nil(t, r.sessionMemoryMgr)
}

// ──────────────────────────── Uninit Shutdown 测试 ────────────────────────────

// TestContextProcessorRail_Uninit_Shutdown 验证 Uninit 时关闭 session memory
func TestContextProcessorRail_Uninit_Shutdown(t *testing.T) {
	cfg := cecontext.NewSessionMemoryConfig()
	r := NewContextProcessorRail(WithSessionMemoryConfig(&cfg))
	require.NotNil(t, r.sessionMemoryMgr)

	// Uninit 调用 Shutdown 不 panic（agent 为 nil 时 getReactAgentConfig 返回 nil）
	err := r.Uninit(nil)
	require.NoError(t, err)
}

// ──────────────────────────── AfterModelCall session memory 调度测试 ────────────────────────────

// TestContextProcessorRail_AfterModelCall_无SessionMemory 验证无 session memory 时正常工作
func TestContextProcessorRail_AfterModelCall_无SessionMemory(t *testing.T) {
	r := NewContextProcessorRail()

	cbc := sainterfaces.NewAgentCallbackContext(nil, nil, newMockSessionFacade())
	err := r.AfterModelCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// TestContextProcessorRail_AfterModelCall_有SessionMemory 验证有 session memory 时正常工作
func TestContextProcessorRail_AfterModelCall_有SessionMemory(t *testing.T) {
	cfg := cecontext.NewSessionMemoryConfig()
	r := NewContextProcessorRail(WithSessionMemoryConfig(&cfg))

	cbc := sainterfaces.NewAgentCallbackContext(nil, nil, newMockSessionFacade())
	err := r.AfterModelCall(context.Background(), cbc)
	assert.NoError(t, err)
}

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestMaybeScheduleSessionMemoryUpdate_未启用 验证未启用时跳过
func TestMaybeScheduleSessionMemoryUpdate_未启用(t *testing.T) {
	r := NewContextProcessorRail()
	cbc := sainterfaces.NewAgentCallbackContext(nil, nil, nil)
	// 未启用 → 不 panic
	r.maybeScheduleSessionMemoryUpdate(context.Background(), cbc)
}

// TestMaybeScheduleSessionMemoryUpdate_启用但无Workspace 验证无 workspace 时跳过
func TestMaybeScheduleSessionMemoryUpdate_启用但无Workspace(t *testing.T) {
	cfg := cecontext.NewSessionMemoryConfig()
	r := NewContextProcessorRail(WithSessionMemoryConfig(&cfg))
	cbc := sainterfaces.NewAgentCallbackContext(nil, nil, newMockSessionFacade())
	// 无 workspace → 不 panic，跳过调度
	r.maybeScheduleSessionMemoryUpdate(context.Background(), cbc)
}
