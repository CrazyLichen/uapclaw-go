package subagent

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"

	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hsubagent "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/subagent"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeBaseAgentForTest 实现 agentinterfaces.BaseAgent 的 mock
type fakeBaseAgentForTest struct {
	cbMgr   *agentinterfaces.AgentCallbackManager
	builder *saprompt.SystemPromptBuilder
	card    *agentschema.AgentCard
}

func newFakeBaseAgentForTest() *fakeBaseAgentForTest {
	return &fakeBaseAgentForTest{
		cbMgr:   agentinterfaces.NewAgentCallbackManager("test-agent"),
		builder: saprompt.NewSystemPromptBuilder(),
		card: &agentschema.AgentCard{
			BaseCard: cschema.BaseCard{ID: "test-agent", Name: "TestAgent"},
		},
	}
}

func (f *fakeBaseAgentForTest) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (f *fakeBaseAgentForTest) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeBaseAgentForTest) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}
func (f *fakeBaseAgentForTest) Card() *agentschema.AgentCard                            { return f.card }
func (f *fakeBaseAgentForTest) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *fakeBaseAgentForTest) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeBaseAgentForTest) CallbackManager() *agentinterfaces.AgentCallbackManager {
	return f.cbMgr
}
func (f *fakeBaseAgentForTest) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	if f.builder == nil {
		return nil
	}
	return f.builder
}
func (f *fakeBaseAgentForTest) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgentForTest) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}
func (f *fakeBaseAgentForTest) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// fakeDeepAgentForTest 实现 hinterfaces.DeepAgentInterface 的 mock
type fakeDeepAgentForTest struct {
	fakeBaseAgentForTest
	deepConfig *hschema.DeepAgentConfig
	// am AbilityManager 返回值（M-06 extractAgentTools 测试注入用；nil 与内嵌默认行为一致）
	am agentinterfaces.AbilityManagerInterface
	// sessionToolkit 记录 SetSessionToolkit 设置的 toolkit（async 分支断言用）
	sessionToolkit *hsubagent.SessionToolkit
}

func newFakeDeepAgentForTest() *fakeDeepAgentForTest {
	return &fakeDeepAgentForTest{
		fakeBaseAgentForTest: *newFakeBaseAgentForTest(),
	}
}

func (f *fakeDeepAgentForTest) ReactAgent() *agents.ReActAgent                        { return nil }
func (f *fakeDeepAgentForTest) LoopCoordinator() hinterfaces.LoopCoordinatorInterface { return nil }
func (f *fakeDeepAgentForTest) LoopController() controller.ControllerInterface        { return nil }
func (f *fakeDeepAgentForTest) EventHandler() modules.EventHandler                    { return nil }

// AbilityManager 遮蔽内嵌 fakeBaseAgentForTest 的默认 nil 实现，支持注入（M-06 测试用）。
func (f *fakeDeepAgentForTest) AbilityManager() agentinterfaces.AbilityManagerInterface { return f.am }

// SpecName 实现 hschema.SubagentSpec（生产 *harness.DeepAgent 同样实现该接口）。
func (f *fakeDeepAgentForTest) SpecName() string { return "general-purpose" }
func (f *fakeDeepAgentForTest) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return nil
}
func (f *fakeDeepAgentForTest) DeepConfig() *hschema.DeepAgentConfig { return f.deepConfig }
func (f *fakeDeepAgentForTest) IsInvokeActive() bool                 { return false }
func (f *fakeDeepAgentForTest) IsAutoInvokeScheduled() bool          { return false }
func (f *fakeDeepAgentForTest) SetAutoInvokeScheduled(_ bool)        {}
func (f *fakeDeepAgentForTest) ScheduleAutoInvokeOnSpawnDone(_ context.Context, _ string, _ float64) error {
	return nil
}
func (f *fakeDeepAgentForTest) CreateSubagent(_ context.Context, _ string, _ string) (hinterfaces.DeepAgentInterface, error) {
	return nil, nil
}
func (f *fakeDeepAgentForTest) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (f *fakeDeepAgentForTest) SwitchMode(_ sessioninterfaces.SessionFacade, _ string)     {}
func (f *fakeDeepAgentForTest) RestoreModeAfterPlanExit(_ sessioninterfaces.SessionFacade) {}
func (f *fakeDeepAgentForTest) GetPlanFilePath(_ sessioninterfaces.SessionFacade) string   { return "" }
func (f *fakeDeepAgentForTest) SaveState(_ sessioninterfaces.SessionFacade, _ *hschema.DeepAgentState) {
}
func (f *fakeDeepAgentForTest) FindRailsByType(_ ...reflect.Type) []agentinterfaces.AgentRail {
	return nil
}
func (f *fakeDeepAgentForTest) AddRail(_ agentinterfaces.AgentRail) {} // M-02: 接口新增 AddRail 的测试桩

func (f *fakeDeepAgentForTest) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}
func (f *fakeDeepAgentForTest) InnerInvokeOverride() func(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil
}

// 10.6.19 接口面扩展桩（Uapswarm 访问器）
// 注：AbilityManager/SystemPromptBuilder 由内嵌 fakeBaseAgentForTest 提供实现，此处不再遮蔽
func (f *fakeDeepAgentForTest) UapswarmProjectDir() string       { return "" }
func (f *fakeDeepAgentForTest) SetUapswarmProjectDir(string)     {}
func (f *fakeDeepAgentForTest) UapswarmCodeProjectDir() string   { return "" }
func (f *fakeDeepAgentForTest) SetUapswarmCodeProjectDir(string) {}
func (f *fakeDeepAgentForTest) UapswarmAdapterMode() string      { return "" }
func (f *fakeDeepAgentForTest) SetUapswarmAdapterMode(string)    {}

// SetSessionToolkit 实现 sessionToolkitSetter 本地接口，记录 toolkit 供断言。
func (f *fakeDeepAgentForTest) SetSessionToolkit(toolkit *hsubagent.SessionToolkit) {
	f.sessionToolkit = toolkit
}

// 编译时验证
var _ agentinterfaces.BaseAgent = (*fakeBaseAgentForTest)(nil)
var _ hinterfaces.DeepAgentInterface = (*fakeDeepAgentForTest)(nil)
var _ hschema.SubagentSpec = (*fakeDeepAgentForTest)(nil)

// fakeAbilityManagerForTools 仅覆盖 List 的最小 mock（M-06 extractAgentTools 测试用）。
type fakeAbilityManagerForTools struct {
	abilities []cschema.Ability
}

func (m *fakeAbilityManagerForTools) Add(ability cschema.Ability) agentschema.AddAbilityResult {
	return agentschema.AddAbilityResult{Added: true}
}
func (m *fakeAbilityManagerForTools) AddMany(_ []cschema.Ability) []agentschema.AddAbilityResult {
	return nil
}
func (m *fakeAbilityManagerForTools) Remove(string) cschema.Ability           { return nil }
func (m *fakeAbilityManagerForTools) RemoveMany(_ []string) []cschema.Ability { return nil }
func (m *fakeAbilityManagerForTools) Get(string) cschema.Ability              { return nil }
func (m *fakeAbilityManagerForTools) List() []cschema.Ability                 { return m.abilities }
func (m *fakeAbilityManagerForTools) ListToolInfo(context.Context, []string, ...string) ([]cschema.ToolInfoInterface, error) {
	return nil, nil
}
func (m *fakeAbilityManagerForTools) Execute(context.Context, *agentinterfaces.AgentCallbackContext, []*llmschema.ToolCall, sessioninterfaces.SessionFacade, string) []agentschema.ExecuteResult {
	return nil
}
func (m *fakeAbilityManagerForTools) SetContextEngine(ceinterface.ContextEngine) {}
func (m *fakeAbilityManagerForTools) ReorderTools([]string)                      {}

// 编译时验证 fakeAbilityManagerForTools 满足 AbilityManagerInterface
var _ agentinterfaces.AbilityManagerInterface = (*fakeAbilityManagerForTools)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewSubagentRail_默认配置 测试默认配置
func TestNewSubagentRail_默认配置(t *testing.T) {
	r := NewSubagentRail()

	assert.Equal(t, 95, r.Priority(), "SubagentRail 优先级应为 95")
	assert.False(t, r.enableAsyncSubagent, "默认不启用异步子代理")
}

// TestNewSubagentRail_启用异步 测试 WithEnableAsyncSubagent
func TestNewSubagentRail_启用异步(t *testing.T) {
	r := NewSubagentRail(WithEnableAsyncSubagent(true))

	assert.True(t, r.enableAsyncSubagent, "WithEnableAsyncSubagent(true) 应启用异步")
}

// TestBuildAvailableAgentsDescription_空列表 测试空子代理列表
func TestBuildAvailableAgentsDescription_空列表(t *testing.T) {
	r := NewSubagentRail()
	result := r.buildAvailableAgentsDescription(nil)

	assert.Empty(t, result, "空列表应返回空字符串")
}

// TestBuildAvailableAgentsDescription_多个子代理 测试格式验证
func TestBuildAvailableAgentsDescription_多个子代理(t *testing.T) {
	r := NewSubagentRail()

	subagents := []hschema.SubagentSpec{
		&hschema.SubAgentConfig{
			AgentCard: agentschema.NewAgentCard(
				agentschema.WithAgentName("explore_agent"),
				agentschema.WithAgentDescription("探索助手"),
			),
		},
		&hschema.SubAgentConfig{
			AgentCard: agentschema.NewAgentCard(
				agentschema.WithAgentName("plan_agent"),
				agentschema.WithAgentDescription("规划助手"),
			),
		},
	}

	result := r.buildAvailableAgentsDescription(subagents)

	assert.Contains(t, result, "- explore_agent: 探索助手")
	assert.Contains(t, result, "- plan_agent: 规划助手")
	assert.Contains(t, result, "Tools:")
}

// TestExtractAgentMeta_SubAgentConfig 测试从 SubAgentConfig 提取元信息
func TestExtractAgentMeta_SubAgentConfig(t *testing.T) {
	r := NewSubagentRail()

	cfg := &hschema.SubAgentConfig{
		AgentCard: agentschema.NewAgentCard(
			agentschema.WithAgentName("test_agent"),
			agentschema.WithAgentDescription("测试代理"),
		),
	}

	name, desc := r.extractAgentMeta(cfg)
	assert.Equal(t, "test_agent", name)
	assert.Equal(t, "测试代理", desc)
}

// TestExtractAgentTools_显式工具 测试 SubAgentConfig 有 tools 时
func TestExtractAgentTools_显式工具(t *testing.T) {
	r := NewSubagentRail()

	cfg := &hschema.SubAgentConfig{
		Tools: []*tool.ToolCard{
			tool.NewToolCard("read_file", "读取文件", nil, nil),
			tool.NewToolCard("bash", "执行命令", nil, nil),
		},
	}

	result := r.extractAgentTools(cfg, "custom_agent")
	assert.Equal(t, "read_file, bash", result)
}

// TestExtractAgentTools_已知默认 测试已知代理工具映射
func TestExtractAgentTools_已知默认(t *testing.T) {
	r := NewSubagentRail()

	cfg := &hschema.SubAgentConfig{} // 无显式 tools

	assert.Equal(t, "bash, glob, grep, list_files, read_file", r.extractAgentTools(cfg, "explore_agent"))
	assert.Equal(t, "bash, glob, grep, list_files, read_file", r.extractAgentTools(cfg, "plan_agent"))
}

// TestExtractAgentTools_回退 测试未知代理返回 "All tools"
func TestExtractAgentTools_回退(t *testing.T) {
	r := NewSubagentRail()

	cfg := &hschema.SubAgentConfig{} // 无显式 tools
	result := r.extractAgentTools(cfg, "unknown_agent")

	assert.Equal(t, "All tools", result)
}

// TestExtractAgentTools_DeepAgent实例 测试 DeepAgent 实例走 ability_manager 路径（M-06）
// Python: test_extract_agent_tools_deep_agent_instance
func TestExtractAgentTools_DeepAgent实例(t *testing.T) {
	r := NewSubagentRail()

	// DeepAgent 实例（非 SubAgentConfig），注册了两个工具
	agent := newFakeDeepAgentForTest()
	agent.am = &fakeAbilityManagerForTools{
		abilities: []cschema.Ability{
			tool.NewToolCard("bash", "执行命令", nil, nil),
			tool.NewToolCard("read_file", "读取文件", nil, nil),
		},
	}

	result := r.extractAgentTools(agent, "general-purpose")
	assert.Equal(t, "bash, read_file", result)
}

// TestExtractAgentTools_DeepAgent实例空列表回退 测试 DeepAgent 工具列表为空时回退已知默认（M-06）
// Python: ability_manager.cards 为空 → 落到 _KNOWN_AGENT_TOOLS
func TestExtractAgentTools_DeepAgent实例空列表回退(t *testing.T) {
	r := NewSubagentRail()

	// DeepAgent 实例但未注册任何工具
	agent := newFakeDeepAgentForTest()
	agent.am = &fakeAbilityManagerForTools{}

	// general-purpose 不在已知默认表内 → 最终回退 "All tools"
	result := r.extractAgentTools(agent, "general-purpose")
	assert.Equal(t, "All tools", result)
}

// TestExtractAgentTools_DeepAgent实例无管理器回退 测试 AbilityManager 为 nil 时不 panic（M-06）
func TestExtractAgentTools_DeepAgent实例无管理器回退(t *testing.T) {
	r := NewSubagentRail()

	// DeepAgent 实例但 am 为 nil（newFakeDeepAgentForTest 默认）
	agent := newFakeDeepAgentForTest()

	// SpecName 为 general-purpose，不在已知默认表内 → 回退 "All tools"
	result := r.extractAgentTools(agent, "general-purpose")
	assert.Equal(t, "All tools", result)
}

// TestSubagentRail_Init_无子代理跳过 测试无子代理时不注册工具
func TestSubagentRail_Init_无子代理跳过(t *testing.T) {
	r := NewSubagentRail()
	agent := newFakeDeepAgentForTest()
	agent.deepConfig = &hschema.DeepAgentConfig{} // 空 subagents

	err := r.Init(context.Background(), agent)

	require.NoError(t, err)
	assert.Empty(t, r.tools, "无子代理时不应注册工具")
}

// TestSubagentRail_Init_有子代理注册TaskTool 测试有子代理时注册 TaskTool
func TestSubagentRail_Init_有子代理注册TaskTool(t *testing.T) {
	r := NewSubagentRail()
	agent := newFakeDeepAgentForTest()
	agent.deepConfig = &hschema.DeepAgentConfig{
		Subagents: []hschema.SubagentSpec{
			&hschema.SubAgentConfig{
				AgentCard: agentschema.NewAgentCard(
					agentschema.WithAgentName("explore_agent"),
				),
			},
		},
	}

	err := r.Init(context.Background(), agent)

	require.NoError(t, err)
	assert.Len(t, r.tools, 1, "有子代理时应注册 1 个 TaskTool")
}

// TestSubagentRail_BeforeModelCall_注入Section 测试注入 task_tool section
func TestSubagentRail_BeforeModelCall_注入Section(t *testing.T) {
	r := NewSubagentRail()
	agent := newFakeDeepAgentForTest()
	agent.deepConfig = &hschema.DeepAgentConfig{
		Subagents: []hschema.SubagentSpec{
			&hschema.SubAgentConfig{
				AgentCard: agentschema.NewAgentCard(agentschema.WithAgentName("explore_agent")),
			},
		},
	}

	err := r.Init(context.Background(), agent)
	require.NoError(t, err)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	err = r.BeforeModelCall(context.Background(), cbc)

	require.NoError(t, err)
	assert.True(t, agent.builder.HasSection("task_tool"), "应注入 task_tool section")
}

// TestSubagentRail_BeforeModelCall_无工具跳过 测试无工具时不注入
func TestSubagentRail_BeforeModelCall_无工具跳过(t *testing.T) {
	r := NewSubagentRail()
	// 不调用 Init，tools 为空

	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	err := r.BeforeModelCall(context.Background(), cbc)

	require.NoError(t, err)
}

// ──────────────────────────── 异步模式（enableAsyncSubagent） ────────────────────────────

// TestSubagentRail_Init_异步模式注册SessionTools 测试异步模式注册 3 个会话工具并设置 toolkit
// 对齐 Python: SubagentRail.init() enable_async_subagent=True 分支
func TestSubagentRail_Init_异步模式注册SessionTools(t *testing.T) {
	r := NewSubagentRail(WithEnableAsyncSubagent(true))
	agent := newFakeDeepAgentForTest()
	agent.deepConfig = &hschema.DeepAgentConfig{
		Subagents: []hschema.SubagentSpec{
			&hschema.SubAgentConfig{
				AgentCard: agentschema.NewAgentCard(
					agentschema.WithAgentName("research_agent"),
				),
			},
		},
	}

	err := r.Init(context.Background(), agent)

	require.NoError(t, err)
	// 应注册 3 个会话工具：sessions_list / sessions_spawn / sessions_cancel
	assert.Len(t, r.tools, 3, "异步模式应注册 3 个 SessionTools")
	if len(r.tools) == 3 {
		assert.Equal(t, "sessions_list", r.tools[0].Card().Name)
		assert.Equal(t, "sessions_spawn", r.tools[1].Card().Name)
		assert.Equal(t, "sessions_cancel", r.tools[2].Card().Name)
	}
	// toolkit 应创建并注入 agent
	assert.NotNil(t, r.toolkit, "异步模式应创建 SessionToolkit")
	assert.Equal(t, r.toolkit, agent.sessionToolkit, "toolkit 应通过 SetSessionToolkit 注入 agent")
}

// TestSubagentRail_BeforeModelCall_异步模式注入SessionToolsSection 测试异步模式注入 session_tools 节
// 对齐 Python: SubagentRail.before_model_call() enable_async_subagent=True 分支
func TestSubagentRail_BeforeModelCall_异步模式注入SessionToolsSection(t *testing.T) {
	r := NewSubagentRail(WithEnableAsyncSubagent(true))
	agent := newFakeDeepAgentForTest()
	agent.deepConfig = &hschema.DeepAgentConfig{
		Subagents: []hschema.SubagentSpec{
			&hschema.SubAgentConfig{
				AgentCard: agentschema.NewAgentCard(agentschema.WithAgentName("research_agent")),
			},
		},
	}

	err := r.Init(context.Background(), agent)
	require.NoError(t, err)

	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	err = r.BeforeModelCall(context.Background(), cbc)

	require.NoError(t, err)
	assert.True(t, agent.builder.HasSection("session_tools"), "应注入 session_tools section")
}

// TestSubagentRail_Uninit_异步模式清理 测试异步模式 Uninit 清空 toolkit 与工具
// 对齐 Python: SubagentRail.uninit() enable_async_subagent=True 分支
func TestSubagentRail_Uninit_异步模式清理(t *testing.T) {
	r := NewSubagentRail(WithEnableAsyncSubagent(true))
	agent := newFakeDeepAgentForTest()
	agent.deepConfig = &hschema.DeepAgentConfig{
		Subagents: []hschema.SubagentSpec{
			&hschema.SubAgentConfig{
				AgentCard: agentschema.NewAgentCard(agentschema.WithAgentName("research_agent")),
			},
		},
	}

	err := r.Init(context.Background(), agent)
	require.NoError(t, err)
	require.Len(t, r.tools, 3)
	require.NotNil(t, agent.sessionToolkit)

	err = r.Uninit(agent)
	require.NoError(t, err)

	assert.Empty(t, r.tools, "Uninit 后 tools 应清空")
	assert.Nil(t, r.toolkit, "Uninit 后 toolkit 应清空")
	assert.Nil(t, agent.sessionToolkit, "Uninit 后 agent 的 toolkit 应被置 nil")
}

// TestSubagentRail_Uninit_同步模式不清理toolkit 测试同步模式 Uninit 不触碰 toolkit
func TestSubagentRail_Uninit_同步模式不清理toolkit(t *testing.T) {
	r := NewSubagentRail()
	agent := newFakeDeepAgentForTest()
	agent.deepConfig = &hschema.DeepAgentConfig{
		Subagents: []hschema.SubagentSpec{
			&hschema.SubAgentConfig{
				AgentCard: agentschema.NewAgentCard(agentschema.WithAgentName("research_agent")),
			},
		},
	}

	err := r.Init(context.Background(), agent)
	require.NoError(t, err)
	require.Len(t, r.tools, 1)

	err = r.Uninit(agent)
	require.NoError(t, err)

	assert.Empty(t, r.tools, "Uninit 后 tools 应清空")
	assert.Nil(t, agent.sessionToolkit, "同步模式 Uninit 不应设置 toolkit（保持 nil）")
}
