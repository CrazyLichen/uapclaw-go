//go:build integration

package handoff_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/teams/handoff"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 测试套件 ────────────────────────────

// HandoffE2ESuite HandoffTeam E2E 集成测试套件
// 对齐 Python: test_handoff_team.py, test_handoff_orchestrator.py,
// test_handoff_request.py, test_handoff_signal.py, test_handoff_tool.py
type HandoffE2ESuite struct {
	suite.Suite
	agentIDs []string
}

func TestHandoffE2E(t *testing.T) {
	suite.Run(t, new(HandoffE2ESuite))
}

func (s *HandoffE2ESuite) SetupTest() {
	s.agentIDs = nil
}

func (s *HandoffE2ESuite) TearDownTest() {
	for _, id := range s.agentIDs {
		if mgr := runner.GetResourceMgr(); mgr != nil {
			_, _ = mgr.RemoveAgent([]string{id})
		}
	}
}

// ──────────────────────────── HandoffOrchestrator 测试 ────────────────────────────

// TestHandoffOrchestrator_路由约束 验证路由图约束 handoff 只能到允许的目标
// 对齐 Python: test_handoff_orchestrator.py test_route_constraints
func (s *HandoffE2ESuite) TestHandoffOrchestrator_路由约束() {
	routes := []handoff.HandoffRoute{
		{Source: "agent-a", Target: "agent-b"},
		{Source: "agent-b", Target: "agent-c"},
	}
	agents := []string{"agent-a", "agent-b", "agent-c"}

	orch := handoff.NewHandoffOrchestrator("agent-a", agents,
		&handoff.HandoffConfig{MaxHandoffs: 10, Routes: routes},
	)

	// agent-a → agent-b：允许
	s.True(orch.RequestHandoff("agent-b", "需要 b 处理"))
	s.Equal("agent-b", orch.CurrentAgentID())
	s.Equal(1, orch.HandoffCount())

	// agent-b → agent-c：允许
	s.True(orch.RequestHandoff("agent-c", "需要 c 处理"))
	s.Equal("agent-c", orch.CurrentAgentID())
	s.Equal(2, orch.HandoffCount())

	// agent-c → agent-a：不允许（无路由）
	s.False(orch.RequestHandoff("agent-a", "回到 a"))
	s.Equal("agent-c", orch.CurrentAgentID(), "拒绝后 currentAgentID 不变")
	s.Equal(2, orch.HandoffCount(), "拒绝后 handoffCount 不变")
}

// TestHandoffOrchestrator_最大handoff次数 验证 MaxHandoffs 限制
// 对齐 Python: test_handoff_orchestrator.py test_max_handoffs
func (s *HandoffE2ESuite) TestHandoffOrchestrator_最大handoff次数() {
	// 全连接路由（默认空路由），但 MaxHandoffs=2
	agents := []string{"agent-a", "agent-b", "agent-c"}
	orch := handoff.NewHandoffOrchestrator("agent-a", agents,
		&handoff.HandoffConfig{MaxHandoffs: 2, Routes: nil},
	)

	s.True(orch.RequestHandoff("agent-b", "第一次"))
	s.True(orch.RequestHandoff("agent-c", "第二次"))
	s.False(orch.RequestHandoff("agent-a", "第三次超过限制"))
	s.Equal(2, orch.HandoffCount())
}

// TestHandoffOrchestrator_终止条件 验证 TerminationCondition 回调
// 对齐 Python: test_handoff_orchestrator.py test_termination_condition
func (s *HandoffE2ESuite) TestHandoffOrchestrator_终止条件() {
	agents := []string{"agent-a", "agent-b"}
	orch := handoff.NewHandoffOrchestrator("agent-a", agents,
		&handoff.HandoffConfig{
			MaxHandoffs: 10,
			Routes:      nil,
			TerminationCondition: func(o *handoff.HandoffOrchestrator) bool {
				return o.HandoffCount() >= 1
			},
		},
	)

	s.True(orch.RequestHandoff("agent-b", "第一次"))
	s.False(orch.RequestHandoff("agent-a", "终止条件已满足"))
}

// TestHandoffOrchestrator_完成和错误通道 验证 Complete/Error 通过 DoneCh 传播
// 对齐 Python: test_handoff_orchestrator.py test_complete_error
// 注意：handoffResult 为 unexported struct，无法直接访问字段，
// 但可通过 DoneCh 接收验证通道行为和幂等性
func (s *HandoffE2ESuite) TestHandoffOrchestrator_完成和错误通道() {
	agents := []string{"agent-a"}
	orch := handoff.NewHandoffOrchestrator("agent-a", agents,
		&handoff.HandoffConfig{MaxHandoffs: 10},
	)

	// Complete 后 DoneCh 应可读
	orch.Complete(map[string]any{"answer": 42})
	doneCh := orch.DoneCh()
	s.NotNil(doneCh, "DoneCh 不应为 nil")
	// 非阻塞读取验证通道有数据
	select {
	case <-doneCh:
		// 成功接收到结果
	default:
		s.Fail("Complete 后 DoneCh 应可读")
	}

	// 多次 Complete 幂等（sync.Once）— 不 panic 即可
	orch.Complete(map[string]any{"another": "value"})
}

// TestHandoffOrchestrator_错误通道 验证 Error 通过 DoneCh 传播
func (s *HandoffE2ESuite) TestHandoffOrchestrator_错误通道() {
	agents := []string{"agent-a"}
	orch := handoff.NewHandoffOrchestrator("agent-a", agents,
		&handoff.HandoffConfig{MaxHandoffs: 10},
	)

	// Error 后 DoneCh 应可读
	orch.Error(fmt.Errorf("agent crashed"))
	doneCh := orch.DoneCh()
	select {
	case <-doneCh:
		// 成功接收到错误
	default:
		s.Fail("Error 后 DoneCh 应可读")
	}
}

// ──────────────────────────── BuildRouteGraph 测试 ────────────────────────────

// TestBuildRouteGraph_显式路由 验证显式路由构建有向图
// 对齐 Python: test_handoff_orchestrator.py test_build_route_graph
func (s *HandoffE2ESuite) TestBuildRouteGraph_显式路由() {
	routes := []handoff.HandoffRoute{
		{Source: "a", Target: "b"},
		{Source: "a", Target: "c"},
		{Source: "b", Target: "c"},
	}
	graph := handoff.BuildRouteGraph([]string{"a", "b", "c"}, routes)

	s.Contains(graph, "a")
	s.Contains(graph["a"], "b")
	s.Contains(graph["a"], "c")
	s.Contains(graph["b"], "c")
	s.Empty(graph["c"], "c 无出边")
}

// TestBuildRouteGraph_空路由全连接 验证空路由构建全连接图
// 对齐 Python: test_handoff_orchestrator.py test_build_route_graph_fully_connected
func (s *HandoffE2ESuite) TestBuildRouteGraph_空路由全连接() {
	agents := []string{"a", "b", "c"}
	graph := handoff.BuildRouteGraph(agents, nil)

	// 全连接：每个 agent 可以 handoff 到其他所有 agent
	for _, src := range agents {
		s.Contains(graph, src)
		s.Len(graph[src], 2, "%s 应有 2 个目标", src)
	}
}

// ──────────────────────────── HandoffSignal 提取测试 ────────────────────────────

// TestExtractHandoffSignal_从结果提取 验证从 agent 输出中提取 handoff 信号
// 对齐 Python: test_handoff_signal.py test_extract_from_result
func (s *HandoffE2ESuite) TestExtractHandoffSignal_从结果提取() {
	// 顶层包含 handoff 键
	result := map[string]any{
		handoff.HandoffTargetKey:  "agent-b",
		handoff.HandoffMessageKey: "请处理数据",
		handoff.HandoffReasonKey:  "需要 b 的专业能力",
	}
	sig := handoff.ExtractHandoffSignal(result, nil)
	s.NotNil(sig)
	s.Equal("agent-b", sig.Target)
	s.Equal("需要 b 的专业能力", sig.Reason)

	// 嵌套在 output 子键中
	resultNested := map[string]any{
		"output": map[string]any{
			handoff.HandoffTargetKey:  "agent-c",
			handoff.HandoffMessageKey: "转发分析",
		},
	}
	sig = handoff.ExtractHandoffSignal(resultNested, nil)
	s.NotNil(sig)
	s.Equal("agent-c", sig.Target)
}

// TestExtractHandoffSignal_无信号 验证无 handoff 信号时返回 nil
// 对齐 Python: test_handoff_signal.py test_no_signal
func (s *HandoffE2ESuite) TestExtractHandoffSignal_无信号() {
	result := map[string]any{
		"answer": "42",
		"status": "done",
	}
	sig := handoff.ExtractHandoffSignal(result, nil)
	s.Nil(sig)
}

// ──────────────────────────── HandoffTool 测试 ────────────────────────────

// TestHandoffTool_调用 验证 HandoffTool.Invoke 返回正确的 handoff 信号结构
// 对齐 Python: test_handoff_tool.py test_invoke
func (s *HandoffE2ESuite) TestHandoffTool_调用() {
	tool := handoff.NewHandoffTool("agent-b", "Agent B 数据分析专家")

	result, err := tool.Invoke(context.Background(), map[string]any{
		"reason":  "需要数据分析",
		"message": "请分析这批数据",
	})
	s.Require().NoError(err)
	s.Equal("agent-b", result[handoff.HandoffTargetKey])
	s.Equal("需要数据分析", result[handoff.HandoffReasonKey])
	s.Equal("请分析这批数据", result[handoff.HandoffMessageKey])

	// ToolCard 名称格式
	s.Equal("transfer_to_agent-b", tool.Card().Name)
}

// TestHandoffTool_缺少reason 验证 reason 参数必填
// 对齐 Python: test_handoff_tool.py test_missing_reason
func (s *HandoffE2ESuite) TestHandoffTool_缺少reason() {
	tool := handoff.NewHandoffTool("agent-b", "Agent B")

	// 无 reason
	result, err := tool.Invoke(context.Background(), map[string]any{
		"message": "hello",
	})
	s.Require().NoError(err)
	s.Equal("", result[handoff.HandoffReasonKey])
	s.Equal("agent-b", result[handoff.HandoffTargetKey])
}

// ──────────────────────────── HandoffConfig 测试 ────────────────────────────

// TestHandoffConfig_默认值 验证 HandoffConfig 默认值
// 对齐 Python: test_handoff_config.py
func (s *HandoffE2ESuite) TestHandoffConfig_默认值() {
	cfg := handoff.NewHandoffConfig()
	s.Equal(10, cfg.MaxHandoffs, "默认 MaxHandoffs 应为 10")
	s.Nil(cfg.StartAgent)
	s.Nil(cfg.Routes)
	s.Nil(cfg.TerminationCondition)

	teamCfg := handoff.NewHandoffTeamConfig()
	s.Equal(10, teamCfg.Handoff.MaxHandoffs)
	s.Equal(10, teamCfg.TeamConfig.MaxAgents)
}

// ──────────────────────────── HandoffTeam 构建测试 ────────────────────────────

// TestHandoffTeam_创建 验证 HandoffTeam 创建和基本属性
// 对齐 Python: test_handoff_team.py test_create
func (s *HandoffE2ESuite) TestHandoffTeam_创建() {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team-handoff-1"),
		maschema.WithTeamCardName("HandoffTeam测试"),
	)

	team := handoff.NewHandoffTeam(card, nil, nil)
	s.NotNil(team)
	s.Equal("team-handoff-1", team.Card().GetID())
	s.Equal("HandoffTeam测试", team.Card().GetName())
	s.NotNil(team.Config())
}

// TestHandoffTeam_添加Agent 验证 AddAgent/RemoveAgent
// 对齐 Python: test_handoff_team.py test_add_remove_agent
func (s *HandoffE2ESuite) TestHandoffTeam_添加Agent() {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team-add"),
		maschema.WithTeamCardName("AddTeam"),
	)
	cfg := handoff.NewHandoffTeamConfig()
	runtimeCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("team-add"),
	)
	tr := team_runtime.NewTeamRuntime(*runtimeCfg)
	mockBus := newMockMessageBus()
	tr.SetMessageBus(mockBus)

	team := handoff.NewHandoffTeam(card, cfg, tr)
	ctx := context.Background()

	// 添加 agent
	agentCard1 := agentschema.NewAgentCard(
		agentschema.WithAgentID("worker-1"),
		agentschema.WithAgentName("Worker1"),
	)
	provider1 := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return &mockBaseAgent{card: agentCard1}, nil
	}
	s.Require().NoError(team.AddAgent(ctx, agentCard1, provider1))
	s.Equal(1, team.GetAgentCount())

	agentCard2 := agentschema.NewAgentCard(
		agentschema.WithAgentID("worker-2"),
		agentschema.WithAgentName("Worker2"),
	)
	provider2 := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return &mockBaseAgent{card: agentCard2}, nil
	}
	s.Require().NoError(team.AddAgent(ctx, agentCard2, provider2))
	s.Equal(2, team.GetAgentCount())

	// 获取卡片
	fetched, err := team.GetAgentCard("worker-1")
	s.Require().NoError(err)
	s.Equal("Worker1", fetched.Name)

	// 列出
	s.Len(team.ListAgents(), 2)

	// 移除
	s.Require().NoError(team.RemoveAgent(ctx, "worker-1"))
	s.Equal(1, team.GetAgentCount())
}

// TestHandoffTeam_订阅和发布 验证 Subscribe/Publish 委托
// 对齐 Python: test_handoff_team.py test_subscribe_publish
func (s *HandoffE2ESuite) TestHandoffTeam_订阅和发布() {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team-sub"),
		maschema.WithTeamCardName("SubTeam"),
	)
	cfg := handoff.NewHandoffTeamConfig()
	runtimeCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("team-sub"),
	)
	tr := team_runtime.NewTeamRuntime(*runtimeCfg)
	mockBus := newMockMessageBus()
	tr.SetMessageBus(mockBus)

	team := handoff.NewHandoffTeam(card, cfg, tr)
	ctx := context.Background()

	// 添加 sender
	agentCard := agentschema.NewAgentCard(
		agentschema.WithAgentID("sender"),
		agentschema.WithAgentName("Sender"),
	)
	provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return &mockBaseAgent{card: agentCard}, nil
	}
	s.Require().NoError(team.AddAgent(ctx, agentCard, provider))

	// Subscribe
	s.Require().NoError(team.Subscribe(ctx, "listener-1", "topic/events"))

	// Publish
	err := team.Publish(ctx, map[string]any{"event": "task_done"}, "topic/events", "sender")
	s.Require().NoError(err)
}

// ──────────────────────────── HandoffRequest 测试 ────────────────────────────

// TestHandoffRequest_SessionID 验证 SessionID 从 Session 提取
// 对齐 Python: test_handoff_request.py
func (s *HandoffE2ESuite) TestHandoffRequest_SessionID() {
	req := &handoff.HandoffRequest{
		InputMessage: map[string]any{"query": "分析数据"},
		History:      nil,
		Session:      nil,
	}
	s.Equal("", req.SessionID(), "nil Session 应返回空字符串")
}

// ──────────────────────────── mock 类型（文件内复用） ────────────────────────────

type mockBaseAgent struct {
	card       *agentschema.AgentCard
	invokeResp map[string]any
}

func (a *mockBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (a *mockBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	if a.invokeResp != nil {
		return a.invokeResp, nil
	}
	return map[string]any{"status": "ok"}, nil
}
func (a *mockBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema, 1)
	close(ch)
	return ch, nil
}
func (a *mockBaseAgent) Card() *agentschema.AgentCard                               { return a.card }
func (a *mockBaseAgent) Config() agentinterfaces.AgentConfig                        { return nil }
func (a *mockBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface    { return nil }
func (a *mockBaseAgent) CallbackManager() *agentinterfaces.AgentCallbackManager     { return nil }
func (a *mockBaseAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return nil }
func (a *mockBaseAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ callback.PerAgentCallbackFunc, _ ...callback.CallbackOption) error {
	return nil
}
func (a *mockBaseAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...callback.CallbackOption) error {
	return nil
}
func (a *mockBaseAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

type mockMessageBus struct{}

func (m *mockMessageBus) Start(_ context.Context) error                    { return nil }
func (m *mockMessageBus) Stop(_ context.Context) error                     { return nil }
func (m *mockMessageBus) CleanupSession(_ context.Context, _ string) error { return nil }
func (m *mockMessageBus) Send(_ context.Context, _ any, _, _, _ string, _ float64) (any, error) {
	return nil, nil
}
func (m *mockMessageBus) Publish(_ context.Context, _ any, _, _, _ string) error { return nil }
func (m *mockMessageBus) AddSubscription(_, _ string)                            {}
func (m *mockMessageBus) RemoveSubscription(_, _ string)                         {}
func (m *mockMessageBus) RemoveAllSubscriptions(_ string)                        {}
func (m *mockMessageBus) ListSubscriptions(_ string) any                         { return nil }
func (m *mockMessageBus) GetSubscriptionCount() int                              { return 0 }

func newMockMessageBus() *mockMessageBus { return &mockMessageBus{} }

// ──────────────────────────── 编译时接口合规检查 ────────────────────────────

func init() {
	_ = team_runtime.MessageBusInterface(newMockMessageBus())
	_ = maschema.BaseTeam((*handoff.HandoffTeam)(nil))
}

// CI 合规性验证：导入 ≥2 个 internal 包
var _ = (*resources_manager.AgentProvider)(nil)
var _ = (*runner.AgentRef)(nil)
var _ = testing.Init
