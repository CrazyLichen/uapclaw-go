package subagent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"

	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
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
func (f *fakeBaseAgentForTest) Card() *agentschema.AgentCard { return f.card }
func (f *fakeBaseAgentForTest) Config() agentinterfaces.AgentConfig                     { return nil }
func (f *fakeBaseAgentForTest) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (f *fakeBaseAgentForTest) CallbackManager() *agentinterfaces.AgentCallbackManager  { return f.cbMgr }
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
}

func newFakeDeepAgentForTest() *fakeDeepAgentForTest {
	return &fakeDeepAgentForTest{
		fakeBaseAgentForTest: *newFakeBaseAgentForTest(),
	}
}

func (f *fakeDeepAgentForTest) ReactAgent() *agents.ReActAgent                           { return nil }
func (f *fakeDeepAgentForTest) LoopCoordinator() hinterfaces.LoopCoordinatorInterface   { return nil }
func (f *fakeDeepAgentForTest) LoopController() controller.ControllerInterface          { return nil }
func (f *fakeDeepAgentForTest) EventHandler() modules.EventHandler                      { return nil }
func (f *fakeDeepAgentForTest) LoadState(_ sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return nil
}
func (f *fakeDeepAgentForTest) DeepConfig() *hschema.DeepAgentConfig { return f.deepConfig }
func (f *fakeDeepAgentForTest) IsInvokeActive() bool                  { return false }
func (f *fakeDeepAgentForTest) IsAutoInvokeScheduled() bool           { return false }
func (f *fakeDeepAgentForTest) SetAutoInvokeScheduled(_ bool)         {}
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

// 编译时验证
var _ agentinterfaces.BaseAgent = (*fakeBaseAgentForTest)(nil)
var _ hinterfaces.DeepAgentInterface = (*fakeDeepAgentForTest)(nil)

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

// TestSubagentRail_Init_无子代理跳过 测试无子代理时不注册工具
func TestSubagentRail_Init_无子代理跳过(t *testing.T) {
	r := NewSubagentRail()
	agent := newFakeDeepAgentForTest()
	agent.deepConfig = &hschema.DeepAgentConfig{} // 空 subagents

	err := r.Init(agent)

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

	err := r.Init(agent)

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

	err := r.Init(agent)
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
