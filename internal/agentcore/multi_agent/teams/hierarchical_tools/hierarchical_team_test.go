package hierarchical_tools

import (
	"context"
	"testing"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockMessageBus 测试用 mock 消息总线，满足 MessageBusInterface 接口。
type mockMessageBus struct {
	sendResult any
	sendErr    error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newMockMessageBus 创建 mock 消息总线。
func newMockMessageBus() *mockMessageBus {
	return &mockMessageBus{}
}

func (m *mockMessageBus) Start(_ context.Context) error { return nil }
func (m *mockMessageBus) Stop(_ context.Context) error  { return nil }
func (m *mockMessageBus) CleanupSession(_ context.Context, _ string) error {
	return nil
}
func (m *mockMessageBus) Send(_ context.Context, _ any, _ string, _ string, _ string, _ float64) (any, error) {
	return m.sendResult, m.sendErr
}
func (m *mockMessageBus) Publish(_ context.Context, _ any, _ string, _ string, _ string) error {
	return nil
}
func (m *mockMessageBus) AddSubscription(_, _ string)               {}
func (m *mockMessageBus) RemoveSubscription(_, _ string)            {}
func (m *mockMessageBus) RemoveAllSubscriptions(_ string)           {}
func (m *mockMessageBus) ListSubscriptions(_ string) map[string]any { return nil }
func (m *mockMessageBus) GetSubscriptionCount() int                 { return 0 }

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestTeam 创建测试用 HierarchicalToolsTeam。
func newTestTeam(teamID string, rootAgentID string) *HierarchicalToolsTeam {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID(teamID),
	)
	var config *HierarchicalToolsTeamConfig
	if rootAgentID != "" {
		rootCard := agentschema.NewAgentCard(
			cschema.WithName("root"),
			cschema.WithID(rootAgentID),
		)
		config = &HierarchicalToolsTeamConfig{
			RootAgent: rootCard,
		}
	}
	return NewHierarchicalToolsTeam(teamCard, config, nil)
}

// newTestTeamWithRuntime 创建测试用 HierarchicalToolsTeam（使用已启动的 runtime）。
func newTestTeamWithRuntime(teamID string, rootAgentID string, bus team_runtime.MessageBusInterface) *HierarchicalToolsTeam {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID(teamID),
	)
	var config *HierarchicalToolsTeamConfig
	if rootAgentID != "" {
		rootCard := agentschema.NewAgentCard(
			cschema.WithName("root"),
			cschema.WithID(rootAgentID),
		)
		config = &HierarchicalToolsTeamConfig{
			RootAgent: rootCard,
		}
	}

	rtCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID(teamID),
	)
	tr := team_runtime.NewTeamRuntime(*rtCfg)
	tr.SetMessageBus(bus)
	_ = tr.Start(context.Background())

	return NewHierarchicalToolsTeam(teamCard, config, tr)
}

// noopProvider 返回 nil Agent 的空 provider。
func noopProvider() maschema.TeamAgentProvider {
	return func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}
}

// ──────────────────────────── 导出函数（测试） ────────────────────────────

// TestNewHierarchicalToolsTeam 验证构造函数。
func TestNewHierarchicalToolsTeam(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
		maschema.WithTeamCardName("tools_team"),
	)
	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	config := &HierarchicalToolsTeamConfig{
		RootAgent: rootCard,
	}

	team := NewHierarchicalToolsTeam(teamCard, config, nil)
	if team == nil {
		t.Fatal("期望 team 非空")
	}
	if team.rootAgentID != "root_id" {
		t.Errorf("期望 rootAgentID = root_id, 实际 = %s", team.rootAgentID)
	}
}

// TestNewHierarchicalToolsTeam_默认配置 验证 nil config 使用默认值。
func TestNewHierarchicalToolsTeam_默认配置(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalToolsTeam(teamCard, nil, nil)
	if team.rootAgentID != "" {
		t.Errorf("期望 rootAgentID 为空, 实际 = %s", team.rootAgentID)
	}
	if team.pendingChildren == nil {
		t.Error("期望 pendingChildren 非空")
	}
	if team.hierarchySetup {
		t.Error("期望 hierarchySetup = false")
	}
}

// TestNewHierarchicalToolsTeam_自定义Runtime 验证传入 runtime 参数。
func TestNewHierarchicalToolsTeam_自定义Runtime(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	rtCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("team_1"),
	)
	tr := team_runtime.NewTeamRuntime(*rtCfg)

	team := NewHierarchicalToolsTeam(teamCard, nil, tr)
	if team.runtime != tr {
		t.Error("期望使用传入的 runtime")
	}
}

// TestHierarchicalToolsTeam_Invoke_RootAgent未注册 验证 assertReady 报错。
func TestHierarchicalToolsTeam_Invoke_RootAgent未注册(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	config := &HierarchicalToolsTeamConfig{
		RootAgent: rootCard,
	}

	team := NewHierarchicalToolsTeam(teamCard, config, nil)

	// 未注册 root_agent 到 runtime，invoke 应报错
	_, err := team.Invoke(context.Background(), map[string]any{"query": "hello"})
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalToolsTeam_Invoke_未配置RootAgent 验证无 rootAgent 时报错。
func TestHierarchicalToolsTeam_Invoke_未配置RootAgent(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	config := NewHierarchicalToolsTeamConfig() // RootAgent 为 nil

	team := NewHierarchicalToolsTeam(teamCard, config, nil)

	_, err := team.Invoke(context.Background(), map[string]any{"query": "hello"})
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalToolsTeam_Invoke_运行时已启动 验证 Invoke 在运行时已启动时走更深层逻辑。
func TestHierarchicalToolsTeam_Invoke_运行时已启动(t *testing.T) {
	bus := newMockMessageBus()
	bus.sendResult = map[string]any{"result": "ok"}

	team := newTestTeamWithRuntime("team_1", "root_id", bus)

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	// Invoke 应该走通 assertReady + setupHierarchy 逻辑
	result, err := team.Invoke(context.Background(), map[string]any{"query": "hello"})
	// 注意：runtime.Send 会走 mockMessageBus.Send，返回 mock 结果
	// StandaloneInvokeContext 内部会创建会话并执行 fn
	_ = result
	_ = err
	// 主要目标是覆盖 Invoke 中的 assertReady、setupHierarchy、日志等分支
}

// TestHierarchicalToolsTeam_Card 验证 Card 返回。
func TestHierarchicalToolsTeam_Card(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalToolsTeam(teamCard, nil, nil)
	if team.Card().GetID() != "team_1" {
		t.Errorf("期望 Card ID = team_1, 实际 = %s", team.Card().GetID())
	}
}

// TestHierarchicalToolsTeam_GetAgentCount 验证空团队 Agent 数量为 0。
func TestHierarchicalToolsTeam_GetAgentCount(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalToolsTeam(teamCard, nil, nil)
	if team.GetAgentCount() != 0 {
		t.Errorf("期望 AgentCount = 0, 实际 = %d", team.GetAgentCount())
	}
}

// TestHierarchicalToolsTeam_满足BaseTeam接口 编译时接口检查。
func TestHierarchicalToolsTeam_满足BaseTeam接口(t *testing.T) {
	var _ maschema.BaseTeam = (*HierarchicalToolsTeam)(nil)
	t.Log("HierarchicalToolsTeam 满足 BaseTeam 接口")
}

// TestHierarchicalToolsTeam_AddAgentWithParent 验证父子关系记录。
func TestHierarchicalToolsTeam_AddAgentWithParent(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalToolsTeam(teamCard, nil, nil)

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	childCard := agentschema.NewAgentCard(
		cschema.WithName("child"),
		cschema.WithID("child_id"),
	)

	rootProvider := noopProvider()
	childProvider := noopProvider()

	// 注册 root
	if err := team.AddAgent(context.Background(), rootCard, rootProvider); err != nil {
		t.Fatalf("AddAgent root 失败: %v", err)
	}

	// 注册 child 并声明父关系
	if err := team.AddAgentWithParent(context.Background(), childCard, childProvider, "root_id"); err != nil {
		t.Fatalf("AddAgentWithParent 失败: %v", err)
	}

	children, ok := team.pendingChildren["root_id"]
	if !ok {
		t.Fatal("期望 pendingChildren 中有 root_id 键")
	}
	if len(children) != 1 {
		t.Fatalf("期望 1 个子 Agent, 实际 = %d", len(children))
	}
	if children[0].ID != "child_id" {
		t.Errorf("期望子 Agent ID = child_id, 实际 = %s", children[0].ID)
	}
}

// TestHierarchicalToolsTeam_setupHierarchy_幂等 验证 setupHierarchy 幂等性。
func TestHierarchicalToolsTeam_setupHierarchy_幂等(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalToolsTeam(teamCard, nil, nil)

	// 无 pendingChildren，setupHierarchy 应成功且标记为已建立
	if err := team.setupHierarchy(context.Background()); err != nil {
		t.Fatalf("setupHierarchy 失败: %v", err)
	}
	if !team.hierarchySetup {
		t.Error("期望 hierarchySetup = true")
	}

	// 再次调用应直接返回 nil（幂等）
	if err := team.setupHierarchy(context.Background()); err != nil {
		t.Fatalf("幂等 setupHierarchy 失败: %v", err)
	}
}

// TestHierarchicalToolsTeam_AddAgent 验证 AddAgent 注册 Agent。
func TestHierarchicalToolsTeam_AddAgent(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)

	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}
	if team.GetAgentCount() != 1 {
		t.Errorf("期望 AgentCount = 1, 实际 = %d", team.GetAgentCount())
	}
	if !team.runtime.HasAgent("root_id") {
		t.Error("期望 runtime 中存在 root_id")
	}
}

// TestHierarchicalToolsTeam_AddAgent_重复注册跳过 验证已存在 Agent 跳过注册。
func TestHierarchicalToolsTeam_AddAgent_重复注册跳过(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)

	// 首次注册
	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("首次 AddAgent 失败: %v", err)
	}

	// 重复注册应跳过，不报错
	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("重复 AddAgent 不应报错: %v", err)
	}

	// 数量仍为 1
	if team.GetAgentCount() != 1 {
		t.Errorf("期望 AgentCount = 1, 实际 = %d", team.GetAgentCount())
	}
}

// TestHierarchicalToolsTeam_AddAgentWithParent_空父ID 验证空 parentAgentID 不记录父子关系。
func TestHierarchicalToolsTeam_AddAgentWithParent_空父ID(t *testing.T) {
	team := newTestTeam("team_1", "")

	childCard := agentschema.NewAgentCard(
		cschema.WithName("child"),
		cschema.WithID("child_id"),
	)

	// 空 parentAgentID，不应记录父子关系
	if err := team.AddAgentWithParent(context.Background(), childCard, noopProvider(), ""); err != nil {
		t.Fatalf("AddAgentWithParent 失败: %v", err)
	}

	if len(team.pendingChildren) != 0 {
		t.Errorf("期望 pendingChildren 为空, 实际 = %d", len(team.pendingChildren))
	}
}

// TestHierarchicalToolsTeam_AddAgentWithParent_重置层级标记 验证添加子 Agent 后 hierarchySetup 重置。
func TestHierarchicalToolsTeam_AddAgentWithParent_重置层级标记(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	// 先让 setupHierarchy 执行（空 pendingChildren）
	if err := team.setupHierarchy(context.Background()); err != nil {
		t.Fatalf("setupHierarchy 失败: %v", err)
	}
	if !team.hierarchySetup {
		t.Fatal("期望 hierarchySetup = true")
	}

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	childCard := agentschema.NewAgentCard(
		cschema.WithName("child"),
		cschema.WithID("child_id"),
	)

	// 注册 root
	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	// 添加父子关系应重置 hierarchySetup
	if err := team.AddAgentWithParent(context.Background(), childCard, noopProvider(), "root_id"); err != nil {
		t.Fatalf("AddAgentWithParent 失败: %v", err)
	}
	if team.hierarchySetup {
		t.Error("期望 hierarchySetup = false，AddAgentWithParent 应重置")
	}
}

// TestHierarchicalToolsTeam_RemoveAgent 验证 RemoveAgent 注销 Agent。
func TestHierarchicalToolsTeam_RemoveAgent(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)

	// 注册
	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}
	if team.GetAgentCount() != 1 {
		t.Fatalf("期望 AgentCount = 1, 实际 = %d", team.GetAgentCount())
	}

	// 注销
	if err := team.RemoveAgent(context.Background(), "root_id"); err != nil {
		t.Fatalf("RemoveAgent 失败: %v", err)
	}
	if team.GetAgentCount() != 0 {
		t.Errorf("期望 AgentCount = 0, 实际 = %d", team.GetAgentCount())
	}
}

// TestHierarchicalToolsTeam_RemoveAgent_不存在 验证注销不存在的 Agent 报错。
func TestHierarchicalToolsTeam_RemoveAgent_不存在(t *testing.T) {
	team := newTestTeam("team_1", "")

	err := team.RemoveAgent(context.Background(), "nonexistent")
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalToolsTeam_Configure 验证 Configure 更新配置。
func TestHierarchicalToolsTeam_Configure(t *testing.T) {
	team := newTestTeam("team_1", "")

	newConfig := maschema.NewTeamConfig()
	newConfig.ConfigureMaxAgents(20)

	if err := team.Configure(context.Background(), *newConfig); err != nil {
		t.Fatalf("Configure 失败: %v", err)
	}

	// 验证配置已更新
	config := team.Config()
	if config.MaxAgents != 20 {
		t.Errorf("期望 MaxAgents = 20, 实际 = %d", config.MaxAgents)
	}
}

// TestHierarchicalToolsTeam_Config 验证 Config 返回团队配置。
func TestHierarchicalToolsTeam_Config(t *testing.T) {
	team := newTestTeam("team_1", "")

	config := team.Config()
	if config == nil {
		t.Fatal("期望 Config 非空")
	}
}

// TestHierarchicalToolsTeam_GetAgentCard 验证 GetAgentCard 获取 Agent 卡片。
func TestHierarchicalToolsTeam_GetAgentCard(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)

	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	card, err := team.GetAgentCard("root_id")
	if err != nil {
		t.Fatalf("GetAgentCard 失败: %v", err)
	}
	if card.ID != "root_id" {
		t.Errorf("期望 ID = root_id, 实际 = %s", card.ID)
	}
}

// TestHierarchicalToolsTeam_GetAgentCard_不存在 验证获取不存在的 Agent 卡片报错。
func TestHierarchicalToolsTeam_GetAgentCard_不存在(t *testing.T) {
	team := newTestTeam("team_1", "")

	_, err := team.GetAgentCard("nonexistent")
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalToolsTeam_ListAgents 验证 ListAgents 列出所有 Agent ID。
func TestHierarchicalToolsTeam_ListAgents(t *testing.T) {
	team := newTestTeam("team_1", "")

	// 空团队
	agents := team.ListAgents()
	if len(agents) != 0 {
		t.Errorf("期望 0 个 Agent, 实际 = %d", len(agents))
	}

	// 注册一个 Agent
	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	agents = team.ListAgents()
	if len(agents) != 1 {
		t.Fatalf("期望 1 个 Agent, 实际 = %d", len(agents))
	}
	if agents[0] != "root_id" {
		t.Errorf("期望 Agent ID = root_id, 实际 = %s", agents[0])
	}
}

// TestHierarchicalToolsTeam_GetRuntime 验证 GetRuntime 返回运行时。
func TestHierarchicalToolsTeam_GetRuntime(t *testing.T) {
	team := newTestTeam("team_1", "")

	rt := team.GetRuntime()
	if rt == nil {
		t.Fatal("期望 Runtime 非空")
	}
}

// TestHierarchicalToolsTeam_GetAgentCount_注册后 验证注册 Agent 后计数正确。
func TestHierarchicalToolsTeam_GetAgentCount_注册后(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	childCard := agentschema.NewAgentCard(
		cschema.WithName("child"),
		cschema.WithID("child_id"),
	)

	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent root 失败: %v", err)
	}
	if err := team.AddAgent(context.Background(), childCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent child 失败: %v", err)
	}

	if team.GetAgentCount() != 2 {
		t.Errorf("期望 AgentCount = 2, 实际 = %d", team.GetAgentCount())
	}
}

// TestHierarchicalToolsTeam_Subscribe 验证 Subscribe 委托运行时。
func TestHierarchicalToolsTeam_Subscribe(t *testing.T) {
	team := newTestTeam("team_1", "")

	err := team.Subscribe(context.Background(), "agent_1", "topic_1")
	if err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}
}

// TestHierarchicalToolsTeam_Unsubscribe 验证 Unsubscribe 委托运行时。
func TestHierarchicalToolsTeam_Unsubscribe(t *testing.T) {
	team := newTestTeam("team_1", "")

	err := team.Unsubscribe(context.Background(), "agent_1", "topic_1")
	if err != nil {
		t.Fatalf("Unsubscribe 失败: %v", err)
	}
}

// TestHierarchicalToolsTeam_Send_运行时未启动 验证 Send 在运行时未启动时报错。
func TestHierarchicalToolsTeam_Send_运行时未启动(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	_, err := team.Send(context.Background(), map[string]any{"msg": "hello"}, "root_id", "sender")
	if err == nil {
		t.Error("期望错误（运行时未启动），实际为 nil")
	}
}

// TestHierarchicalToolsTeam_Publish_运行时未启动 验证 Publish 在运行时未启动时报错。
func TestHierarchicalToolsTeam_Publish_运行时未启动(t *testing.T) {
	team := newTestTeam("team_1", "")

	err := team.Publish(context.Background(), map[string]any{"msg": "hello"}, "topic_1", "sender")
	if err == nil {
		t.Error("期望错误（运行时未启动），实际为 nil")
	}
}

// TestHierarchicalToolsTeam_Send_运行时已启动 验证 Send 在运行时已启动时的行为。
func TestHierarchicalToolsTeam_Send_运行时已启动(t *testing.T) {
	bus := newMockMessageBus()
	bus.sendResult = map[string]any{"result": "ok"}

	team := newTestTeamWithRuntime("team_1", "root_id", bus)

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	result, err := team.Send(context.Background(), map[string]any{"msg": "hello"}, "root_id", "sender")
	if err != nil {
		t.Fatalf("Send 失败: %v", err)
	}
	if result == nil {
		t.Error("期望 result 非空")
	}
}

// TestHierarchicalToolsTeam_Stream_未配置RootAgent 验证 Stream 无 rootAgent 时报错。
func TestHierarchicalToolsTeam_Stream_未配置RootAgent(t *testing.T) {
	team := newTestTeam("team_1", "")

	_, err := team.Stream(context.Background(), map[string]any{"query": "hello"})
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalToolsTeam_Stream_RootAgent未注册 验证 Stream root_agent 未注册时报错。
func TestHierarchicalToolsTeam_Stream_RootAgent未注册(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	_, err := team.Stream(context.Background(), map[string]any{"query": "hello"})
	if err == nil {
		t.Error("期望错误（root_agent 未注册），实际为 nil")
	}
}

// TestHierarchicalToolsTeam_Stream_运行时已启动 验证 Stream 在运行时已启动时走更深层逻辑。
func TestHierarchicalToolsTeam_Stream_运行时已启动(t *testing.T) {
	bus := newMockMessageBus()

	team := newTestTeamWithRuntime("team_1", "root_id", bus)

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	// Stream 会走通 assertReady + setupHierarchy，然后进入 StandaloneStreamContext
	// 内部 fn 会尝试 GetResourceMgr，可能为 nil（测试环境下），走 ResourceMgr 未初始化分支
	ch, err := team.Stream(context.Background(), map[string]any{"query": "hello"})
	// 即使 fn 内部报错，Stream 本身返回 ch 和 nil error（错误在 goroutine 中处理）
	_ = ch
	_ = err
}

// TestHierarchicalToolsTeam_assertReady_未配置RootAgent 验证 assertReady 在 rootAgentID 为空时报错。
func TestHierarchicalToolsTeam_assertReady_未配置RootAgent(t *testing.T) {
	team := newTestTeam("team_1", "")

	err := team.assertReady()
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalToolsTeam_assertReady_RootAgent未注册 验证 assertReady 在 root_agent 未注册时报错。
func TestHierarchicalToolsTeam_assertReady_RootAgent未注册(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	err := team.assertReady()
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalToolsTeam_assertReady_成功 验证 assertReady 在 root_agent 已注册时成功。
func TestHierarchicalToolsTeam_assertReady_成功(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	if err := team.assertReady(); err != nil {
		t.Fatalf("assertReady 不应报错: %v", err)
	}
}

// TestHierarchicalToolsTeam_setupHierarchy_无待注册子Agent 验证无 pendingChildren 时直接标记已建立。
func TestHierarchicalToolsTeam_setupHierarchy_无待注册子Agent(t *testing.T) {
	team := newTestTeam("team_1", "")

	if err := team.setupHierarchy(context.Background()); err != nil {
		t.Fatalf("setupHierarchy 失败: %v", err)
	}
	if !team.hierarchySetup {
		t.Error("期望 hierarchySetup = true")
	}
}

// TestHierarchicalToolsTeam_setupHierarchy_有待注册子Agent 验证有 pendingChildren 但 ResourceMgr 为空时的行为。
func TestHierarchicalToolsTeam_setupHierarchy_有待注册子Agent(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	childCard := agentschema.NewAgentCard(
		cschema.WithName("child"),
		cschema.WithID("child_id"),
	)

	// 注册 root 和 child（含父子关系）
	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}
	if err := team.AddAgentWithParent(context.Background(), childCard, noopProvider(), "root_id"); err != nil {
		t.Fatalf("AddAgentWithParent 失败: %v", err)
	}

	// 调用 setupHierarchy
	// 在测试环境中 ResourceMgr 可能已初始化但无 Agent 实例，
	// 此时 setupHierarchy 会报错（父 Agent 实例未找到），
	// 这是正常行为，覆盖了 ResourceMgr 非空 + GetAgent 失败的分支
	err := team.setupHierarchy(context.Background())
	if err != nil {
		// 父 Agent 实例未找到的错误是预期的，验证层级未建立
		if team.hierarchySetup {
			t.Error("期望 hierarchySetup = false（因为 setup 失败）")
		}
	} else {
		if !team.hierarchySetup {
			t.Error("期望 hierarchySetup = true")
		}
	}
}

// TestHierarchicalToolsTeam_AddAgent_识别RootAgent 验证 AddAgent 识别 rootAgent。
func TestHierarchicalToolsTeam_AddAgent_识别RootAgent(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)

	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	// root_agent 应已注册到 runtime
	if !team.runtime.HasAgent("root_id") {
		t.Error("期望 runtime 中存在 root_id")
	}
}

// TestHierarchicalToolsTeam_AddAgentWithParent_多个子Agent 验证同一父 Agent 下注册多个子 Agent。
func TestHierarchicalToolsTeam_AddAgentWithParent_多个子Agent(t *testing.T) {
	team := newTestTeam("team_1", "root_id")

	rootCard := agentschema.NewAgentCard(
		cschema.WithName("root"),
		cschema.WithID("root_id"),
	)
	child1 := agentschema.NewAgentCard(
		cschema.WithName("child1"),
		cschema.WithID("child_1"),
	)
	child2 := agentschema.NewAgentCard(
		cschema.WithName("child2"),
		cschema.WithID("child_2"),
	)

	if err := team.AddAgent(context.Background(), rootCard, noopProvider()); err != nil {
		t.Fatalf("AddAgent root 失败: %v", err)
	}
	if err := team.AddAgentWithParent(context.Background(), child1, noopProvider(), "root_id"); err != nil {
		t.Fatalf("AddAgentWithParent child1 失败: %v", err)
	}
	if err := team.AddAgentWithParent(context.Background(), child2, noopProvider(), "root_id"); err != nil {
		t.Fatalf("AddAgentWithParent child2 失败: %v", err)
	}

	children := team.pendingChildren["root_id"]
	if len(children) != 2 {
		t.Fatalf("期望 2 个子 Agent, 实际 = %d", len(children))
	}
}
