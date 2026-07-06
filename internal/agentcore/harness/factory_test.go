package harness

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	resourcesmanager "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── fake 定义 ────────────────────────────

// fakeTool 测试用的工具桩，实现 tool.Tool 接口
type fakeTool struct {
	card *tool.ToolCard
}

// Card 返回工具配置卡片
func (f *fakeTool) Card() *tool.ToolCard { return f.card }

// Invoke 返回不支持 Invoke 的错误
func (f *fakeTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return nil, tool.ErrInvokeNotSupported
}

// Stream 返回不支持 Stream 的错误
func (f *fakeTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// newFakeTool 创建指定名称的 fakeTool
func newFakeTool(name string) *fakeTool {
	return &fakeTool{
		card: tool.NewToolCard(name, name+" description", nil, nil),
	}
}

// fakeRail 测试用的 Rail 桩，实现 rail.AgentRail 接口
type fakeRail struct {
	rail.BaseRail
}

// ──────────────────────────── normalizeTools 测试 ────────────────────────────

// TestNormalizeTools_空输入 nil 输入返回空列表
func TestNormalizeTools_空输入(t *testing.T) {
	cards, instances := normalizeTools(nil)
	assert.Empty(t, cards)
	assert.Empty(t, instances)
}

// TestNormalizeTools_提取Card 输入 Tool 实例，验证 Card 提取
func TestNormalizeTools_提取Card(t *testing.T) {
	ft := newFakeTool("test_tool")
	cards, instances := normalizeTools([]tool.Tool{ft})

	require.Len(t, cards, 1)
	assert.Equal(t, "test_tool", cards[0].GetName())
	require.Len(t, instances, 1)
	assert.Same(t, ft, instances[0])
}

// ──────────────────────────── isDisabledFreeSearchTool 测试 ────────────────────────────

// TestIsDisabledFreeSearchTool_当前始终返回False 当前实现始终返回 false
func TestIsDisabledFreeSearchTool_当前始终返回False(t *testing.T) {
	card := tool.NewToolCard("free_search", "free search tool", nil, nil)
	assert.False(t, isDisabledFreeSearchTool(card))
	// nil 卡片也返回 false
	assert.False(t, isDisabledFreeSearchTool(nil))
}

// ──────────────────────────── registerToolInstances 测试 ────────────────────────────

// TestRegisterToolInstances_空输入 nil 输入不报错
func TestRegisterToolInstances_空输入(t *testing.T) {
	err := registerToolInstances(nil, "test")
	assert.NoError(t, err)
}

// TestRegisterToolInstances_正常注册 正常注册不报错（需要初始化 ResourceMgr）
func TestRegisterToolInstances_正常注册(t *testing.T) {
	// 确保 ResourceMgr 已初始化
	rm := ensureResourceMgr()
	require.NotNil(t, rm)

	ft := newFakeTool("reg_test_tool")
	err := registerToolInstances([]tool.Tool{ft}, "test_tag")
	assert.NoError(t, err)
}

// ──────────────────────────── injectGeneralPurposeSubagent 测试 ────────────────────────────

// TestInjectGeneralPurposeSubagent_不注入 add=false 时不变
func TestInjectGeneralPurposeSubagent_不注入(t *testing.T) {
	subagents := []schema.SubAgentConfig{
		{SystemPrompt: "existing"},
	}
	result := injectGeneralPurposeSubagent(
		subagents,
		false, // addGeneralPurposeAgent
		"cn",  // resolvedLanguage
		nil,   // rails
		"",    // systemPrompt
		nil,   // toolInstances
		nil,   // mcps
		nil,   // model
		nil,   // skills
	)
	assert.Len(t, result, 1)
	assert.Equal(t, "existing", result[0].SystemPrompt)
}

// TestInjectGeneralPurposeSubagent_注入到头部 add=true 时注入到列表头部
func TestInjectGeneralPurposeSubagent_注入到头部(t *testing.T) {
	subagents := []schema.SubAgentConfig{
		{SystemPrompt: "existing"},
	}
	result := injectGeneralPurposeSubagent(
		subagents,
		true,  // addGeneralPurposeAgent
		"cn",  // resolvedLanguage
		nil,   // rails
		"",    // systemPrompt
		nil,   // toolInstances
		nil,   // mcps
		nil,   // model
		nil,   // skills
	)
	require.Len(t, result, 2)
	// 注入的 general-purpose 应在头部
	require.NotNil(t, result[0].AgentCard)
	assert.Equal(t, "general-purpose", result[0].AgentCard.GetName())
	// 原有子 Agent 在尾部
	assert.Equal(t, "existing", result[1].SystemPrompt)
}

// TestInjectGeneralPurposeSubagent_已存在不注入 已有 general-purpose 时不重复注入
func TestInjectGeneralPurposeSubagent_已存在不注入(t *testing.T) {
	subagents := []schema.SubAgentConfig{
		{
			AgentCard: agentschema.NewAgentCard(
				agentschema.WithAgentName("general-purpose"),
			),
		},
	}
	result := injectGeneralPurposeSubagent(
		subagents,
		true,  // addGeneralPurposeAgent，但已存在
		"cn",  // resolvedLanguage
		nil,   // rails
		"",    // systemPrompt
		nil,   // toolInstances
		nil,   // mcps
		nil,   // model
		nil,   // skills
	)
	assert.Len(t, result, 1)
}

// ──────────────────────────── buildWorkspace 测试 ────────────────────────────

// TestBuildWorkspace_nil创建默认 nil 时创建默认 Workspace(root_path="./")
func TestBuildWorkspace_nil创建默认(t *testing.T) {
	ws := buildWorkspace(nil, "cn")
	require.NotNil(t, ws)
	assert.Equal(t, "./", ws.RootPath)
}

// TestBuildWorkspace_已有实例 传入已有实例直接返回
func TestBuildWorkspace_已有实例(t *testing.T) {
	existing := workspace.NewWorkspace("/tmp/test", "cn")
	ws := buildWorkspace(existing, "cn")
	assert.Same(t, existing, ws)
}

// ──────────────────────────── buildSysOperation 测试 ────────────────────────────

// TestBuildSysOperation_提供时直接使用 提供了 SysOperation 时直接返回
func TestBuildSysOperation_提供时直接使用(t *testing.T) {
	provided := &sysop.BaseSysOperation{}
	card := agentschema.NewAgentCard(agentschema.WithAgentName("test"))
	result, err := buildSysOperation(card, provided, false)
	require.NoError(t, err)
	assert.Same(t, provided, result)
}

// TestBuildSysOperation_未提供时创建默认 未提供时创建 BaseSysOperation 桩
func TestBuildSysOperation_未提供时创建默认(t *testing.T) {
	// 确保 ResourceMgr 已初始化，buildSysOperation 内部会尝试注册
	ensureResourceMgr()

	card := agentschema.NewAgentCard(agentschema.WithAgentName("test_agent"))
	result, err := buildSysOperation(card, nil, false)
	require.NoError(t, err)
	require.NotNil(t, result)
	// 返回的是 BaseSysOperation 桩实例
	_, ok := result.(*sysop.BaseSysOperation)
	assert.True(t, ok)
}

// ──────────────────────────── alreadyProvided 测试 ────────────────────────────

// TestAlreadyProvided_匹配 同类型匹配
func TestAlreadyProvided_匹配(t *testing.T) {
	target := &fakeRail{}
	rails := []rail.AgentRail{&fakeRail{}, rail.NewBaseRail()}
	assert.True(t, alreadyProvided(rails, target))
}

// TestAlreadyProvided_空列表 空列表返回 false
func TestAlreadyProvided_空列表(t *testing.T) {
	target := &fakeRail{}
	assert.False(t, alreadyProvided(nil, target))
	assert.False(t, alreadyProvided([]rail.AgentRail{}, target))
}

// ──────────────────────────── collectDisabledSkillsFromState 测试 ────────────────────────────

// TestCollectDisabledSkillsFromState_目录不存在 目录不存在返回空
func TestCollectDisabledSkillsFromState_目录不存在(t *testing.T) {
	result := collectDisabledSkillsFromState([]string{"/nonexistent/path"})
	assert.Empty(t, result)
}

// TestCollectDisabledSkillsFromState_正常读取 正常读取 skills_state.json
func TestCollectDisabledSkillsFromState_正常读取(t *testing.T) {
	dir := t.TempDir()
	// 写入 skills_state.json
	stateData := map[string]any{
		"skill_configs": map[string]any{
			"search": map[string]any{
				"enabled": false,
			},
			"code_gen": map[string]any{
				"enabled": true,
			},
			"translate": map[string]any{
				"enabled": false,
			},
		},
	}
	data, err := json.Marshal(stateData)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir, "skills_state.json"), data, 0o644)
	require.NoError(t, err)

	result := collectDisabledSkillsFromState([]string{dir})
	// 应返回 search 和 translate，按字母排序
	assert.Equal(t, []string{"search", "translate"}, result)
}

// TestCollectDisabledSkillsFromState_JSON解析失败 JSON 格式错误返回空
func TestCollectDisabledSkillsFromState_JSON解析失败(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(filepath.Join(dir, "skills_state.json"), []byte("invalid json"), 0o644)
	require.NoError(t, err)

	result := collectDisabledSkillsFromState([]string{dir})
	assert.Empty(t, result)
}

// TestCollectDisabledSkillsFromState_多目录合并 多个目录合并去重排序
func TestCollectDisabledSkillsFromState_多目录合并(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()

	// 目录1：search 禁用
	state1 := map[string]any{
		"skill_configs": map[string]any{
			"search": map[string]any{"enabled": false},
		},
	}
	data1, err := json.Marshal(state1)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir1, "skills_state.json"), data1, 0o644)
	require.NoError(t, err)

	// 目录2：search 禁用（重复）+ code_gen 禁用
	state2 := map[string]any{
		"skill_configs": map[string]any{
			"search":   map[string]any{"enabled": false},
			"code_gen": map[string]any{"enabled": false},
		},
	}
	data2, err := json.Marshal(state2)
	require.NoError(t, err)
	err = os.WriteFile(filepath.Join(dir2, "skills_state.json"), data2, 0o644)
	require.NoError(t, err)

	result := collectDisabledSkillsFromState([]string{dir1, dir2})
	// 去重后排序：code_gen, search
	assert.Equal(t, []string{"code_gen", "search"}, result)
}

// ──────────────────────────── CreateDeepAgent 测试 ────────────────────────────

// TestCreateDeepAgent_最小参数 最小参数创建 DeepAgent
func TestCreateDeepAgent_最小参数(t *testing.T) {
	// 确保 ResourceMgr 已初始化
	ensureResourceMgr()

	ctx := context.Background()
	agent, err := CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{})
	require.NoError(t, err)
	require.NotNil(t, agent)
	// 默认卡片名称为 deep_agent
	assert.Equal(t, "deep_agent", agent.Card().GetName())
}

// TestCreateDeepAgent_自定义Card 自定义 AgentCard
func TestCreateDeepAgent_自定义Card(t *testing.T) {
	// 确保 ResourceMgr 已初始化
	ensureResourceMgr()

	customCard := agentschema.NewAgentCard(
		agentschema.WithAgentName("my_agent"),
		agentschema.WithAgentDescription("custom description"),
	)
	ctx := context.Background()
	agent, err := CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Card: customCard,
	})
	require.NoError(t, err)
	require.NotNil(t, agent)
	assert.Equal(t, "my_agent", agent.Card().GetName())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ensureResourceMgr 确保全局资源管理器已初始化，返回其指针
func ensureResourceMgr() *resourcesmanager.ResourceMgr {
	// GetResourceMgr 内部通过 getRunner() 懒初始化，
	// 首次调用会自动创建 Runner 及 ResourceMgr 实例
	return runner.GetResourceMgr()
}
