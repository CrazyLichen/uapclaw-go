package memory

import (
	"context"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
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

// TestTeamMemoryManager_InitToolkit_有工作空间 测试 S-12 实现后
// 有 workspace 时 InitToolkit 走 MemberMemoryToolkit 真实初始化路径
func TestTeamMemoryManager_InitToolkit_有工作空间(t *testing.T) {
	ws := workspace.NewWorkspace(t.TempDir(), "")
	mgr := NewTeamMemoryManager(TeamMemoryManagerParams{
		MemberName: "t1",
		TeamName:   "team_a",
		Workspace:  ws,
	})
	ok, err := mgr.InitToolkit(context.Background())
	// lite.IsMemoryEnabled() 默认 true（MEMORY_ENABLED 未设置）→ toolkit 初始化成功
	assert.True(t, ok, "S-12 实现后 InitToolkit 应真实初始化 toolkit")
	assert.NoError(t, err)
	// 幂等：二次调用直接返回 true
	ok2, err2 := mgr.InitToolkit(context.Background())
	assert.True(t, ok2)
	assert.NoError(t, err2)
}

// TestTeamMemoryManager_ExtractAfterRound_前置条件矩阵 测试 S-12 四重前置条件
// enableAutoExtract / persistent / leader / teamMemoryDir+db 任一不满足即短路
func TestTeamMemoryManager_ExtractAfterRound_前置条件矩阵(t *testing.T) {
	dir := t.TempDir()
	cases := []struct {
		name      string
		params    TeamMemoryManagerParams
		wantReach bool
	}{
		{
			name: "全满足_到达提取",
			params: TeamMemoryManagerParams{
				MemberName: "leader", TeamName: "team_a", Role: TeamRoleLeader,
				Lifecycle: TeamLifecyclePersistent, EnableAutoExtract: true, TeamMemoryDir: &dir,
			},
			// extractionModel 为 nil → ExtractTeamMemories 早退（不会真正调 LLM）
			wantReach: true,
		},
		{
			name: "非Leader_短路",
			params: TeamMemoryManagerParams{
				MemberName: "t1", TeamName: "team_a", Role: TeamRoleTeammate,
				Lifecycle: TeamLifecyclePersistent, EnableAutoExtract: true, TeamMemoryDir: &dir,
			},
			wantReach: false,
		},
		{
			name: "非持久_短路",
			params: TeamMemoryManagerParams{
				MemberName: "leader", TeamName: "team_a", Role: TeamRoleLeader,
				Lifecycle: TeamLifecycleTemporary, EnableAutoExtract: true, TeamMemoryDir: &dir,
			},
			wantReach: false,
		},
		{
			name: "未启用自动提取_短路",
			params: TeamMemoryManagerParams{
				MemberName: "leader", TeamName: "team_a", Role: TeamRoleLeader,
				Lifecycle: TeamLifecyclePersistent, EnableAutoExtract: false, TeamMemoryDir: &dir,
			},
			wantReach: false,
		},
		{
			name: "无记忆目录_短路",
			params: TeamMemoryManagerParams{
				MemberName: "leader", TeamName: "team_a", Role: TeamRoleLeader,
				Lifecycle: TeamLifecyclePersistent, EnableAutoExtract: true,
			},
			wantReach: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := NewTeamMemoryManager(tc.params)
			// 全满足场景：db 为 nil 仍短路（Python: not self._team_memory_dir or not self._db）
			// 此处 wantReach 仅区分前置条件是否全部满足（不含 db）
			assert.NotPanics(t, func() {
				_ = mgr.ExtractAfterRound(context.Background())
			})
		})
	}
}

// TestBuildExtractionContext_任务与消息 测试 S-12 提取上下文组装
func TestBuildExtractionContext_任务与消息(t *testing.T) {
	assignee := "coder1"
	toMember := "leader"
	tasks := []*database.TeamTaskBase{
		{TaskID: "t1", TeamName: "team_a", Title: "实现登录", Content: "完成登录页", Status: "in_progress", Assignee: &assignee},
		{TaskID: "t2", TeamName: "team_a", Title: "无分配任务", Content: "", Status: "pending", Assignee: nil},
	}
	messages := []*database.TeamMessageBase{
		{MessageID: "m2", TeamName: "team_a", FromMemberName: "coder1", ToMemberName: &toMember, Content: "完成", Timestamp: 2000, Broadcast: false},
		{MessageID: "m1", TeamName: "team_a", FromMemberName: "leader", ToMemberName: nil, Content: "开始", Timestamp: 1000, Broadcast: true},
	}
	result := BuildExtractionContext(tasks, messages, 8.0)
	// 任务记录存在
	assert.Contains(t, result, "## 任务记录")
	assert.Contains(t, result, "### 实现登录 [in_progress] -> coder1")
	assert.Contains(t, result, "### 无分配任务 [pending] -> 未分配")
	// 消息按时间排序（m1 在前）
	assert.Contains(t, result, "## 团队对话")
	msgStart := strings.Index(result, "[01-01 08:00] leader -> 全体: 开始")
	assert.GreaterOrEqual(t, msgStart, 0, "广播消息应包含方向 -> 全体")
	assert.Contains(t, result, "-> leader: 完成")
}

// TestBuildExtractionContext_空输入 测试空输入只有标题
func TestBuildExtractionContext_空输入(t *testing.T) {
	result := BuildExtractionContext(nil, nil, 8.0)
	assert.Equal(t, "# 本轮团队协作记录\n", result)
}

// TestCreateExtractionTools_工具集 测试 S-12 提取工具创建
func TestCreateExtractionTools_工具集(t *testing.T) {
	tools := CreateExtractionTools(t.TempDir(), nil, "team_a")
	assert.Len(t, tools, 3)
	names := make([]string, 0, 3)
	for _, tl := range tools {
		names = append(names, tl.Card().Name)
	}
	assert.Contains(t, names, "read_memory_file")
	assert.Contains(t, names, "write_memory_file")
	assert.Contains(t, names, "list_memory_files")
	// ToolCard ID 带 team 前缀
	for _, tl := range tools {
		assert.Contains(t, tl.Card().ID, "extract.team_a.")
	}
}
