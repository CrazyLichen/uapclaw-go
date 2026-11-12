package runtime

import (
	"context"
	"reflect"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	harnessinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// readyProbeAgent 实现 ChatAgentPort 且满足 DeepAgentInterface 的替身。
//
// 嵌入 UnimplementedDeepAgentInteraction（规则 10）拿到交互方法的空实现，
// 只覆盖断言需要的部分；ChatAgentPort 的五个方法单独实现。
type readyProbeAgent struct {
	harnessinterfaces.UnimplementedDeepAgentInteraction
}

// ProcessMessage 非流式执行（用例不用）。
func (a *readyProbeAgent) ProcessMessage(context.Context, *schema.AgentRequest) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{}, nil
}

// ProcessMessageStream 流式执行（用例不用）。
func (a *readyProbeAgent) ProcessMessageStream(context.Context, *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, nil
}

// ExecuteMessage 由 Runtime 调度的非流式执行（用例不用）。
func (a *readyProbeAgent) ExecuteMessage(context.Context, *schema.AgentRequest) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{}, nil
}

// ExecuteMessageStream 由 Runtime 调度的流式执行（用例走这条）。
func (a *readyProbeAgent) ExecuteMessageStream(context.Context, *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, nil
}

// PlanStateAccessor plan 状态入口（用例不用）。
func (a *readyProbeAgent) PlanStateAccessor() PlanStateAccessor { return nil }

// ── 以下为满足 DeepAgentInterface 的空实现（用例只用其身份，不调这些方法）──

// AbilityManager 能力管理器。
func (a *readyProbeAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }

// ReactAgent 底层 ReAct agent。
func (a *readyProbeAgent) ReactAgent() *agents.ReActAgent { return nil }

// Card Agent 身份卡。
func (a *readyProbeAgent) Card() *agentschema.AgentCard { return nil }

// SystemPromptBuilder 系统提示词构建器。
func (a *readyProbeAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return nil
}

// LoopCoordinator 循环协调器。
func (a *readyProbeAgent) LoopCoordinator() harnessinterfaces.LoopCoordinatorInterface {
	return nil
}

// LoopController 循环控制器。
func (a *readyProbeAgent) LoopController() controller.ControllerInterface { return nil }

// EventHandler 事件处理器。
func (a *readyProbeAgent) EventHandler() modules.EventHandler { return nil }

// LoadState 读会话状态。
func (a *readyProbeAgent) LoadState(sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	return nil
}

// DeepConfig Deep 配置。
func (a *readyProbeAgent) DeepConfig() *hschema.DeepAgentConfig { return nil }

// IsInvokeActive 是否有进行中的 invoke。
func (a *readyProbeAgent) IsInvokeActive() bool { return false }

// IsAutoInvokeScheduled 是否已排期自动 invoke。
func (a *readyProbeAgent) IsAutoInvokeScheduled() bool { return false }

// SetAutoInvokeScheduled 设置自动 invoke 排期标记。
func (a *readyProbeAgent) SetAutoInvokeScheduled(bool) {}

// ScheduleAutoInvokeOnSpawnDone 子代理完成后排期自动 invoke。
func (a *readyProbeAgent) ScheduleAutoInvokeOnSpawnDone(context.Context, string, float64) error {
	return nil
}

// CreateSubagent 创建子代理。
func (a *readyProbeAgent) CreateSubagent(context.Context, string, string) (harnessinterfaces.DeepAgentInterface, error) {
	return nil, nil
}

// Invoke 直接执行。
func (a *readyProbeAgent) Invoke(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}

// SwitchMode 切换模式。
func (a *readyProbeAgent) SwitchMode(sessioninterfaces.SessionFacade, string) {}

// RestoreModeAfterPlanExit plan 退出后恢复模式。
func (a *readyProbeAgent) RestoreModeAfterPlanExit(sessioninterfaces.SessionFacade) {}

// GetPlanFilePath plan 文件路径。
func (a *readyProbeAgent) GetPlanFilePath(sessioninterfaces.SessionFacade) string { return "" }

// SaveState 存会话状态。
func (a *readyProbeAgent) SaveState(sessioninterfaces.SessionFacade, *hschema.DeepAgentState) {}

// FindRailsByType 按类型查 rail。
func (a *readyProbeAgent) FindRailsByType(...reflect.Type) []agentinterfaces.AgentRail {
	return nil
}

// AddRail 注册 rail。
func (a *readyProbeAgent) AddRail(agentinterfaces.AgentRail) {}

// UnregisterRail 注销 rail。
func (a *readyProbeAgent) UnregisterRail(context.Context, agentinterfaces.AgentRail) error {
	return nil
}

// InnerInvokeOverride 内层 invoke 覆盖。
func (a *readyProbeAgent) InnerInvokeOverride() func(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil
}

// UapswarmProjectDir 工程目录。
func (a *readyProbeAgent) UapswarmProjectDir() string { return "" }

// SetUapswarmProjectDir 设置工程目录。
func (a *readyProbeAgent) SetUapswarmProjectDir(string) {}

// UapswarmCodeProjectDir code 工程目录。
func (a *readyProbeAgent) UapswarmCodeProjectDir() string { return "" }

// SetUapswarmCodeProjectDir 设置 code 工程目录。
func (a *readyProbeAgent) SetUapswarmCodeProjectDir(string) {}

// UapswarmAdapterMode adapter 模式。
func (a *readyProbeAgent) UapswarmAdapterMode() string { return "" }

// SetUapswarmAdapterMode 设置 adapter 模式。
func (a *readyProbeAgent) SetUapswarmAdapterMode(string) {}

// readyProbeManager 固定返回同一 agent 的 manager。
type readyProbeManager struct {
	agent ChatAgentPort
}

// GetChatAgent 取聊天 agent（不存在时创建）。
func (m *readyProbeManager) GetChatAgent(
	context.Context, string, string, string, string, map[string]any, string,
) (ChatAgentPort, error) {
	return m.agent, nil
}

// GetChatAgentNoWait 非阻塞取已有 agent。
func (m *readyProbeManager) GetChatAgentNoWait(string, string, string, string) ChatAgentPort {
	return m.agent
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestStream_触发OnAgentReady 就绪回调在启动 stream 之前执行一次。
// Python: service.py:2587-2590 —— on_agent_ready(agent) 在 process_message_stream 之前。
func TestStream_触发OnAgentReady(t *testing.T) {
	agent := &readyProbeAgent{}
	rt := NewAgentRuntime(AgentRuntimeDeps{Manager: &readyProbeManager{agent: agent}})

	var calls int
	var gotAgent harnessinterfaces.DeepAgentInterface
	err := rt.Stream(context.Background(),
		&schema.AgentRequest{RequestID: "r1", ChannelID: "web"},
		func(*RuntimeEvent) bool { return true },
		WithOnAgentReady(func(_ context.Context, a harnessinterfaces.DeepAgentInterface) error {
			calls++
			gotAgent = a
			return nil
		}))
	require.NoError(t, err)
	require.Equal(t, 1, calls, "就绪回调应恰好触发一次")
	require.Same(t, agent, gotAgent, "回调应收到当轮执行的 agent")
}

// TestStream_未设OnAgentReady不报错 未传回调时不触发也不失败。
func TestStream_未设OnAgentReady不报错(t *testing.T) {
	rt := NewAgentRuntime(AgentRuntimeDeps{Manager: &readyProbeManager{agent: &readyProbeAgent{}}})

	err := rt.Stream(context.Background(),
		&schema.AgentRequest{RequestID: "r1", ChannelID: "web"},
		func(*RuntimeEvent) bool { return true })
	require.NoError(t, err)
}

// TestStream_OnAgentReady错误向上传播 回调失败时 Stream 返回该错误。
// Python: service.py:2588-2590 —— await on_agent_ready(agent) 的异常直接冒泡。
func TestStream_OnAgentReady错误向上传播(t *testing.T) {
	rt := NewAgentRuntime(AgentRuntimeDeps{Manager: &readyProbeManager{agent: &readyProbeAgent{}}})

	wantErr := context.Canceled
	err := rt.Stream(context.Background(),
		&schema.AgentRequest{RequestID: "r1", ChannelID: "web"},
		func(*RuntimeEvent) bool { return true },
		WithOnAgentReady(func(context.Context, harnessinterfaces.DeepAgentInterface) error {
			return wantErr
		}))
	require.ErrorIs(t, err, wantErr)
}

// TestStream_OnAgentReady回调先于流式执行 回调执行时流式方法尚未被调用。
// Python: service.py:2593 `response_stream = agent.process_message_stream(request)` 在回调之后。
func TestStream_OnAgentReady回调先于流式执行(t *testing.T) {
	var streamStarted bool
	agent := &orderedProbeAgent{streamStarted: &streamStarted}
	rt := NewAgentRuntime(AgentRuntimeDeps{Manager: &readyProbeManager{agent: agent}})

	err := rt.Stream(context.Background(),
		&schema.AgentRequest{RequestID: "r1", ChannelID: "web"},
		func(*RuntimeEvent) bool { return true },
		WithOnAgentReady(func(context.Context, harnessinterfaces.DeepAgentInterface) error {
			require.False(t, streamStarted, "回调执行时流式方法不应已开始")
			return nil
		}))
	require.NoError(t, err)
	require.True(t, streamStarted, "流式方法最终应被调用")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// orderedProbeAgent 记录流式方法是否已开始，用于验证回调顺序。
type orderedProbeAgent struct {
	harnessinterfaces.UnimplementedDeepAgentInteraction
	streamStarted *bool
}

// ProcessMessage 非流式执行（用例不用）。
func (a *orderedProbeAgent) ProcessMessage(context.Context, *schema.AgentRequest) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{}, nil
}

// ProcessMessageStream 流式执行（用例不用）。
func (a *orderedProbeAgent) ProcessMessageStream(context.Context, *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	*a.streamStarted = true
	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, nil
}

// ExecuteMessage 由 Runtime 调度的非流式执行（用例不用）。
func (a *orderedProbeAgent) ExecuteMessage(context.Context, *schema.AgentRequest) (*schema.AgentResponse, error) {
	return &schema.AgentResponse{}, nil
}

// ExecuteMessageStream 由 Runtime 调度的流式执行，标记已开始。
func (a *orderedProbeAgent) ExecuteMessageStream(context.Context, *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	*a.streamStarted = true
	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, nil
}

// PlanStateAccessor plan 状态入口（用例不用）。
func (a *orderedProbeAgent) PlanStateAccessor() PlanStateAccessor { return nil }
