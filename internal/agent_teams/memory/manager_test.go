package memory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	mgr.SetExtractionModel("test_model")
	assert.Equal(t, "test_model", mgr.ExtractionModel())
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
