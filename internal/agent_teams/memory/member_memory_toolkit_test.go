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

// TestNewMemberMemoryToolkit 测试创建成员记忆工具集
func TestNewMemberMemoryToolkit(t *testing.T) {
	toolkit := NewMemberMemoryToolkit("m1", "team1", nil, TeamScenarioGeneral, nil, nil, false)
	require.NotNil(t, toolkit)
	assert.Equal(t, "m1", toolkit.MemberName())
	assert.Equal(t, "team1", toolkit.TeamName())
	assert.Equal(t, TeamScenarioGeneral, toolkit.scenario)
	assert.False(t, toolkit.readOnly)
}

// TestNewMemberMemoryToolkit_只读模式 测试创建只读工具集
func TestNewMemberMemoryToolkit_只读模式(t *testing.T) {
	toolkit := NewMemberMemoryToolkit("m1", "team1", nil, TeamScenarioCoding, nil, nil, true)
	require.NotNil(t, toolkit)
	assert.True(t, toolkit.readOnly)
	assert.Equal(t, TeamScenarioCoding, toolkit.scenario)
}

// TestMemberMemoryToolkit_GetTools 测试获取工具列表（未初始化时为空）
func TestMemberMemoryToolkit_GetTools(t *testing.T) {
	toolkit := NewMemberMemoryToolkit("m1", "team1", nil, TeamScenarioGeneral, nil, nil, false)
	assert.Nil(t, toolkit.GetTools())
}

// TestMemberMemoryToolkit_GetToolCards 测试获取工具卡片列表（未初始化时为空）
func TestMemberMemoryToolkit_GetToolCards(t *testing.T) {
	toolkit := NewMemberMemoryToolkit("m1", "team1", nil, TeamScenarioGeneral, nil, nil, false)
	cards := toolkit.GetToolCards()
	assert.Empty(t, cards)
}

// TestMemberMemoryToolkit_Manager 测试获取管理器（未初始化时为 nil）
func TestMemberMemoryToolkit_Manager(t *testing.T) {
	toolkit := NewMemberMemoryToolkit("m1", "team1", nil, TeamScenarioGeneral, nil, nil, false)
	assert.Nil(t, toolkit.Manager())
}

// TestMemberMemoryToolkit_Ctx 测试获取上下文（未初始化时为 nil）
func TestMemberMemoryToolkit_Ctx(t *testing.T) {
	toolkit := NewMemberMemoryToolkit("m1", "team1", nil, TeamScenarioGeneral, nil, nil, false)
	assert.Nil(t, toolkit.Ctx())
}

// TestMemberMemoryToolkit_TeamName 测试获取团队名称
func TestMemberMemoryToolkit_TeamName(t *testing.T) {
	toolkit := NewMemberMemoryToolkit("m1", "team1", nil, TeamScenarioGeneral, nil, nil, false)
	assert.Equal(t, "team1", toolkit.TeamName())
}

// TestMemberMemoryToolkit_MemberName 测试获取成员名称
func TestMemberMemoryToolkit_MemberName(t *testing.T) {
	toolkit := NewMemberMemoryToolkit("m1", "team1", nil, TeamScenarioGeneral, nil, nil, false)
	assert.Equal(t, "m1", toolkit.MemberName())
}

// TestMemberMemoryToolkit_Close 测试关闭未初始化的工具集
func TestMemberMemoryToolkit_Close(t *testing.T) {
	toolkit := NewMemberMemoryToolkit("m1", "team1", nil, TeamScenarioGeneral, nil, nil, false)
	assert.NoError(t, toolkit.Close(context.Background()))
	assert.Nil(t, toolkit.Manager())
	assert.Nil(t, toolkit.Ctx())
	assert.Nil(t, toolkit.GetTools())
	assert.False(t, toolkit.initialized)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
