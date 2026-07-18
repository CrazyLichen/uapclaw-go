package adapter

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/types"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──── filterToolCards 测试 ────

// TestFilterToolCards_通配符 测试 allowedTools=["*"] 时返回全部
func TestFilterToolCards_通配符(t *testing.T) {
	cards := []*tool.ToolCard{
		tool.NewToolCardWithID("read_file", "Read", "读取文件", nil, nil),
		tool.NewToolCardWithID("write_file", "Write", "写入文件", nil, nil),
		tool.NewToolCardWithID("bash", "Bash", "执行命令", nil, nil),
	}
	result := filterToolCards(cards, []string{"*"}, nil)
	assert.Len(t, result, 3)
}

// TestFilterToolCards_显示名匹配 测试用显示名过滤
func TestFilterToolCards_显示名匹配(t *testing.T) {
	cards := []*tool.ToolCard{
		tool.NewToolCardWithID("read_file", "Read", "读取文件", nil, nil),
		tool.NewToolCardWithID("write_file", "Write", "写入文件", nil, nil),
		tool.NewToolCardWithID("bash", "Bash", "执行命令", nil, nil),
	}
	result := filterToolCards(cards, []string{"Read", "Bash"}, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, "read_file", result[0].ID)
	assert.Equal(t, "bash", result[1].ID)
}

// TestFilterToolCards_内部名匹配 测试用内部名过滤
func TestFilterToolCards_内部名匹配(t *testing.T) {
	cards := []*tool.ToolCard{
		tool.NewToolCardWithID("read_file", "Read", "读取文件", nil, nil),
		tool.NewToolCardWithID("write_file", "Write", "写入文件", nil, nil),
	}
	result := filterToolCards(cards, []string{"read_file"}, nil)
	assert.Len(t, result, 1)
	assert.Equal(t, "read_file", result[0].ID)
}

// TestFilterToolCards_含disallowed 测试 disallowedTools 移除
func TestFilterToolCards_含disallowed(t *testing.T) {
	cards := []*tool.ToolCard{
		tool.NewToolCardWithID("read_file", "Read", "读取文件", nil, nil),
		tool.NewToolCardWithID("write_file", "Write", "写入文件", nil, nil),
		tool.NewToolCardWithID("bash", "Bash", "执行命令", nil, nil),
	}
	result := filterToolCards(cards, []string{"*"}, []string{"Write", "bash"})
	assert.Len(t, result, 1)
	assert.Equal(t, "read_file", result[0].ID)
}

// TestFilterToolCards_空allowedTools 测试空允许列表
func TestFilterToolCards_空allowedTools(t *testing.T) {
	cards := []*tool.ToolCard{
		tool.NewToolCardWithID("read_file", "Read", "读取文件", nil, nil),
		tool.NewToolCardWithID("write_file", "Write", "写入文件", nil, nil),
	}
	result := filterToolCards(cards, nil, nil)
	assert.Len(t, result, 0)
}

// TestFilterToolCards_显示名到内部名映射 测试 displayToInternal 映射
func TestFilterToolCards_显示名到内部名映射(t *testing.T) {
	cards := []*tool.ToolCard{
		tool.NewToolCardWithID("bash", "Bash", "执行命令", nil, nil),
		tool.NewToolCardWithID("ls", "LS", "列出目录", nil, nil),
		tool.NewToolCardWithID("grep", "Grep", "搜索内容", nil, nil),
	}
	// "ListDir" 映射到 "ls"
	result := filterToolCards(cards, []string{"ListDir", "Grep"}, nil)
	assert.Len(t, result, 2)
	assert.Equal(t, "ls", result[0].ID)
	assert.Equal(t, "grep", result[1].ID)
}

// ──── buildAgentToolCard 测试 ────

// TestBuildAgentToolCard_基本 测试基本构建
func TestBuildAgentToolCard_基本(t *testing.T) {
	agents := []*types.AgentDefinition{
		{
			Name:        "code-reviewer",
			Description: "代码审查",
			WhenToUse:   "审查代码质量",
			Tools:       []string{"Read", "Grep"},
		},
	}
	card := buildAgentToolCard(agents, "test_agent")
	assert.Equal(t, "Agent", card.Name)
	assert.Contains(t, card.Description, "code-reviewer")
	assert.Contains(t, card.Description, "审查代码质量")
	assert.Contains(t, card.Description, "Read, Grep")
	assert.Contains(t, card.ID, "agent_tool_test_agent")
}

// TestBuildAgentToolCard_空列表 测试无自定义 Agent
func TestBuildAgentToolCard_空列表(t *testing.T) {
	card := buildAgentToolCard(nil, "test_agent")
	assert.Equal(t, "Agent", card.Name)
	assert.Contains(t, card.Description, "Launch a new agent")
}

// TestBuildAgentToolCard_无WhenToUse时用Description 测试 WhenToUse fallback
func TestBuildAgentToolCard_无WhenToUse时用Description(t *testing.T) {
	agents := []*types.AgentDefinition{
		{
			Name:        "tester",
			Description: "测试专家",
			Tools:       []string{"*"},
		},
	}
	card := buildAgentToolCard(agents, "test_agent")
	assert.Contains(t, card.Description, "tester")
	assert.Contains(t, card.Description, "测试专家")
}

// ──── CodeAgentRail 测试 ────

// TestNewCodeAgentRail 测试创建 CodeAgentRail 实例
func TestNewCodeAgentRail(t *testing.T) {
	rail := NewCodeAgentRail("/tmp/workspace", nil)
	assert.NotNil(t, rail)
	assert.Equal(t, codeAgentRailPriority, rail.Priority())
}

// TestCodeAgentRail_Init_无配置列表器 测试 configLister 为 nil 时跳过注册
func TestCodeAgentRail_Init_无配置列表器(t *testing.T) {
	rail := NewCodeAgentRail("/tmp/workspace", nil)
	// 不应 panic，无自定义 Agent 应正常返回
	err := rail.Init(nil)
	assert.NoError(t, err)
}

// TestCodeAgentRail_Uninit_无工具 测试 agentTool 为 nil 时直接返回
func TestCodeAgentRail_Uninit_无工具(t *testing.T) {
	rail := NewCodeAgentRail("/tmp/workspace", nil)
	err := rail.Uninit(nil)
	assert.NoError(t, err)
}

// ──── sortedKeys 测试 ────

// TestSortedKeys 测试排序键
func TestSortedKeys(t *testing.T) {
	m := map[string]*types.AgentDefinition{
		"charlie": {},
		"alpha":   {},
		"bravo":   {},
	}
	keys := sortedKeys(m)
	assert.Equal(t, []string{"alpha", "bravo", "charlie"}, keys)
}

// ──── 共享常量测试 ────

// TestDisallowedForSubagents 测试共享常量包含所有禁用工具
func TestDisallowedForSubagents(t *testing.T) {
	expected := []string{"Agent", "task", "enter_plan_mode", "exit_plan_mode",
		"ask_user_question", "task_stop", "switch_mode"}
	for _, name := range expected {
		assert.True(t, disallowedForSubagents[name], "应包含 %s", name)
	}
}
