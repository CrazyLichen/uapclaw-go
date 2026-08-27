package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestNewTeamMemoryManager 测试创建团队记忆管理器
func TestNewTeamMemoryManager(t *testing.T) {
	params := TeamMemoryManagerParams{
		MemberName:        "t1",
		TeamName:          "team_a",
		Role:              TeamRoleTeammate,
		Lifecycle:         TeamLifecycleTemporary,
		Scenario:          TeamScenarioCoding,
		Language:          TeamLanguageCN,
		PromptMode:        PromptModePassive,
		EnableAutoExtract: true,
	}
	mgr := NewTeamMemoryManager(params)
	require.NotNil(t, mgr)
	assert.Equal(t, "t1", mgr.MemberName())
	assert.Equal(t, "team_a", mgr.TeamName())
	assert.Equal(t, TeamRoleTeammate, mgr.Role())
}

// TestNewTeamMemoryManager_无共享记忆 测试不启用共享记忆
func TestNewTeamMemoryManager_无共享记忆(t *testing.T) {
	params := TeamMemoryManagerParams{
		MemberName:   "t1",
		TeamName:     "team_a",
		SharedMemory: false,
	}
	mgr := NewTeamMemoryManager(params)
	require.NotNil(t, mgr)
	assert.Nil(t, mgr.SharedManager())
}

// TestTeamMemoryManager_InitToolkit 测试初始化工具集（当前返回 false）
func TestTeamMemoryManager_InitToolkit(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	ok, err := mgr.InitToolkit(context.Background())
	assert.False(t, ok)
	assert.NoError(t, err)
}

// TestTeamMemoryManager_RegisterTools 测试注册工具（当前空实现）
func TestTeamMemoryManager_RegisterTools(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	mgr.RegisterTools(nil) // 不 panic
}

// TestTeamMemoryManager_LoadAndInject 测试加载注入（当前空实现）
func TestTeamMemoryManager_LoadAndInject(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	err := mgr.LoadAndInject(context.Background(), nil, "")
	assert.NoError(t, err)
}

// TestTeamMemoryManager_ExtractAfterRound 测试提取（当前空实现）
func TestTeamMemoryManager_ExtractAfterRound(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	err := mgr.ExtractAfterRound(context.Background())
	assert.NoError(t, err)
}

// TestTeamMemoryManager_Close 测试关闭（当前空实现）
func TestTeamMemoryManager_Close(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	err := mgr.Close(context.Background())
	assert.NoError(t, err)
}

// TestTeamMemoryManager_ExtractionModel 测试提取模型 getter/setter
func TestTeamMemoryManager_ExtractionModel(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	assert.Nil(t, mgr.ExtractionModel())
	// 设置 nil 仍正常工作
	mgr.SetExtractionModel(nil)
	assert.Nil(t, mgr.ExtractionModel())
}

// TestTeamMemoryManager_Getter 测试各种 getter
func TestTeamMemoryManager_Getter(t *testing.T) {
	params := TeamMemoryManagerParams{
		MemberName: "t1",
		TeamName:   "team_a",
		Role:       TeamRoleLeader,
	}
	mgr := NewTeamMemoryManager(params)
	assert.Equal(t, "t1", mgr.MemberName())
	assert.Equal(t, "team_a", mgr.TeamName())
	assert.Equal(t, TeamRoleLeader, mgr.Role())
}

// TestNewTeamMemoryManager_启用共享记忆 测试启用共享记忆时创建 SharedManager
func TestNewTeamMemoryManager_启用共享记忆(t *testing.T) {
	dir := "/tmp/test-team-memory"
	params := TeamMemoryManagerParams{
		MemberName:    "leader1",
		TeamName:      "team_b",
		SharedMemory:  true,
		TeamMemoryDir: &dir,
		SysOperation:  &fakeSysOperation{},
	}
	mgr := NewTeamMemoryManager(params)
	require.NotNil(t, mgr)
	assert.NotNil(t, mgr.SharedManager(), "启用共享记忆时应创建 SharedManager")
}

// TestNewTeamMemoryManager_共享记忆Dir为nil 测试 TeamMemoryDir 为 nil 时不创建 SharedManager
func TestNewTeamMemoryManager_共享记忆Dir为nil(t *testing.T) {
	params := TeamMemoryManagerParams{
		MemberName:   "t1",
		SharedMemory: true,
		// TeamMemoryDir 为 nil
	}
	mgr := NewTeamMemoryManager(params)
	require.NotNil(t, mgr)
	assert.Nil(t, mgr.SharedManager(), "TeamMemoryDir 为 nil 时不应创建 SharedManager")
}

// TestTeamMemoryManager_RegisterTools_不panic 测试注册工具不 panic
func TestTeamMemoryManager_RegisterTools_不panic(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	assert.NotPanics(t, func() {
		mgr.RegisterTools(nil)
	})
}

// TestTeamMemoryManager_ExtractionModel_设置有效值 测试提取模型设置有效值
func TestTeamMemoryManager_ExtractionModel_设置有效值(t *testing.T) {
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{MemberName: "t1"})
	assert.Nil(t, mgr.ExtractionModel())
	model := &llm.Model{}
	mgr.SetExtractionModel(model)
	assert.Equal(t, model, mgr.ExtractionModel())
}
