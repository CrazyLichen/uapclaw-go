package handoff

import (
	"context"
	"sync"
	"testing"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewHandoffTeam 测试 NewHandoffTeam 构造函数
func TestNewHandoffTeam(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("test_team"),
		maschema.WithTeamCardName("测试团队"),
	)

	team := NewHandoffTeam(card, nil, nil)

	if team == nil {
		t.Fatal("NewHandoffTeam 返回 nil")
	}
	if team.Card().GetID() != "test_team" {
		t.Errorf("Card ID = %q, want %q", team.Card().GetID(), "test_team")
	}
	if team.runtime == nil {
		t.Error("runtime 不应为 nil")
	}
	if len(team.agentProviders) != 0 {
		t.Errorf("agentProviders 长度 = %d, want 0", len(team.agentProviders))
	}
	if team.internalAgentsReady {
		t.Error("internalAgentsReady 应为 false")
	}
}

// TestNewHandoffTeam_带配置 测试带配置的构造函数
func TestNewHandoffTeam_带配置(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("test_team"),
	)

	config := NewHandoffTeamConfig()
	config.Handoff.MaxHandoffs = 5

	team := NewHandoffTeam(card, config, nil)

	if team.config.Handoff.MaxHandoffs != 5 {
		t.Errorf("MaxHandoffs = %d, want 5", team.config.Handoff.MaxHandoffs)
	}
}

// TestNewHandoffTeam_带运行时 测试带运行时的构造函数
func TestNewHandoffTeam_带运行时(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("test_team"),
	)

	rtCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("test_team"),
	)
	rt := team_runtime.NewTeamRuntime(*rtCfg)

	team := NewHandoffTeam(card, nil, rt)

	if team.runtime != rt {
		t.Error("runtime 应为传入的运行时实例")
	}
}

// TestHandoffTeam_Card 测试 Card() 方法
func TestHandoffTeam_Card(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
		maschema.WithTeamCardName("团队1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	if team.Card().GetID() != "team1" {
		t.Errorf("Card ID = %q, want %q", team.Card().GetID(), "team1")
	}
	if team.Card().GetName() != "团队1" {
		t.Errorf("Card Name = %q, want %q", team.Card().GetName(), "团队1")
	}
}

// TestHandoffTeam_Config 测试 Config() 方法
func TestHandoffTeam_Config(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	cfg := team.Config()
	if cfg == nil {
		t.Fatal("Config 返回 nil")
	}
	if cfg.MaxAgents != 10 {
		t.Errorf("MaxAgents = %d, want 10", cfg.MaxAgents)
	}
}

// TestHandoffTeam_AddAgent 测试 AddAgent 方法
func TestHandoffTeam_AddAgent(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	agentCard := agentschema.NewAgentCard(
		schema.WithID("agent1"),
		schema.WithName("Agent1"),
	)

	provider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}

	err := team.AddAgent(context.Background(), agentCard, provider)
	if err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	if len(team.agentProviders) != 1 {
		t.Errorf("agentProviders 长度 = %d, want 1", len(team.agentProviders))
	}

	if _, ok := team.agentProviders["agent1"]; !ok {
		t.Error("agentProviders 中应包含 agent1")
	}

	if team.internalAgentsReady {
		t.Error("AddAgent 后 internalAgentsReady 应为 false")
	}
}

// TestHandoffTeam_AddAgent_重复注册 测试重复注册 Agent
func TestHandoffTeam_AddAgent_重复注册(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	agentCard := agentschema.NewAgentCard(
		schema.WithID("agent1"),
		schema.WithName("Agent1"),
	)

	provider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}

	// 第一次注册
	err := team.AddAgent(context.Background(), agentCard, provider)
	if err != nil {
		t.Fatalf("AddAgent 失败: %v", err)
	}

	// 第二次注册（应跳过）
	err = team.AddAgent(context.Background(), agentCard, provider)
	if err != nil {
		t.Fatalf("重复 AddAgent 失败: %v", err)
	}

	if len(team.agentProviders) != 1 {
		t.Errorf("agentProviders 长度 = %d, want 1", len(team.agentProviders))
	}
}

// TestHandoffTeam_GetAgentCount 测试 GetAgentCount 方法
func TestHandoffTeam_GetAgentCount(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	if team.GetAgentCount() != 0 {
		t.Errorf("GetAgentCount = %d, want 0", team.GetAgentCount())
	}

	agentCard := agentschema.NewAgentCard(
		schema.WithID("agent1"),
		schema.WithName("Agent1"),
	)
	provider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}
	_ = team.AddAgent(context.Background(), agentCard, provider)

	if team.GetAgentCount() != 1 {
		t.Errorf("GetAgentCount = %d, want 1", team.GetAgentCount())
	}
}

// TestHandoffTeam_ListAgents 测试 ListAgents 方法
func TestHandoffTeam_ListAgents(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	agentCard1 := agentschema.NewAgentCard(
		schema.WithID("agent1"),
		schema.WithName("Agent1"),
	)
	agentCard2 := agentschema.NewAgentCard(
		schema.WithID("agent2"),
		schema.WithName("Agent2"),
	)
	provider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}

	_ = team.AddAgent(context.Background(), agentCard1, provider)
	_ = team.AddAgent(context.Background(), agentCard2, provider)

	agents := team.ListAgents()
	if len(agents) != 2 {
		t.Fatalf("ListAgents 长度 = %d, want 2", len(agents))
	}

	// 检查包含两个 Agent
	agentSet := make(map[string]struct{})
	for _, a := range agents {
		agentSet[a] = struct{}{}
	}
	if _, ok := agentSet["agent1"]; !ok {
		t.Error("ListAgents 应包含 agent1")
	}
	if _, ok := agentSet["agent2"]; !ok {
		t.Error("ListAgents 应包含 agent2")
	}
}

// TestHandoffTeam_RemoveAgent 测试 RemoveAgent 方法
func TestHandoffTeam_RemoveAgent(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	agentCard := agentschema.NewAgentCard(
		schema.WithID("agent1"),
		schema.WithName("Agent1"),
	)
	provider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}

	_ = team.AddAgent(context.Background(), agentCard, provider)
	if team.GetAgentCount() != 1 {
		t.Fatalf("GetAgentCount = %d, want 1", team.GetAgentCount())
	}

	err := team.RemoveAgent(context.Background(), "agent1")
	if err != nil {
		t.Fatalf("RemoveAgent 失败: %v", err)
	}

	if team.GetAgentCount() != 0 {
		t.Errorf("RemoveAgent 后 GetAgentCount = %d, want 0", team.GetAgentCount())
	}

	if _, ok := team.agentProviders["agent1"]; ok {
		t.Error("RemoveAgent 后 agentProviders 不应包含 agent1")
	}
}

// TestHandoffTeam_getStartAgentID_配置了StartAgent 测试 getStartAgentID 配置了起始 Agent
func TestHandoffTeam_getStartAgentID_配置了StartAgent(t *testing.T) {
	startAgent := agentschema.NewAgentCard(
		schema.WithID("start_agent"),
		schema.WithName("起始Agent"),
	)

	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
		maschema.WithAgentCards([]*agentschema.AgentCard{
			agentschema.NewAgentCard(schema.WithID("agent1"), schema.WithName("Agent1")),
			agentschema.NewAgentCard(schema.WithID("agent2"), schema.WithName("Agent2")),
		}),
	)

	config := NewHandoffTeamConfig()
	config.Handoff.StartAgent = startAgent

	team := NewHandoffTeam(card, config, nil)

	if team.getStartAgentID() != "start_agent" {
		t.Errorf("getStartAgentID = %q, want %q", team.getStartAgentID(), "start_agent")
	}
}

// TestHandoffTeam_getStartAgentID_未配置StartAgent 测试 getStartAgentID 未配置起始 Agent
func TestHandoffTeam_getStartAgentID_未配置StartAgent(t *testing.T) {
	agent1 := agentschema.NewAgentCard(
		schema.WithID("agent1"),
		schema.WithName("Agent1"),
	)
	agent2 := agentschema.NewAgentCard(
		schema.WithID("agent2"),
		schema.WithName("Agent2"),
	)

	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
		maschema.WithAgentCards([]*agentschema.AgentCard{agent1, agent2}),
	)

	team := NewHandoffTeam(card, nil, nil)

	if team.getStartAgentID() != "agent1" {
		t.Errorf("getStartAgentID = %q, want %q", team.getStartAgentID(), "agent1")
	}
}

// TestHandoffTeam_getStartAgentID_空AgentCards 测试 getStartAgentID 空 Agent 列表
func TestHandoffTeam_getStartAgentID_空AgentCards(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)

	team := NewHandoffTeam(card, nil, nil)

	if team.getStartAgentID() != "" {
		t.Errorf("getStartAgentID = %q, want 空字符串", team.getStartAgentID())
	}
}

// TestHandoffTeam_lookupCoordinator_有协调器 测试 lookupCoordinator 有协调器
func TestHandoffTeam_lookupCoordinator_有协调器(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	coord := NewHandoffOrchestrator("agent1", []string{"agent1", "agent2"}, nil)
	team.coordinatorRegistry["session1"] = coord

	found := team.lookupCoordinator("session1")
	if found != coord {
		t.Error("lookupCoordinator 应返回注册的协调器")
	}
}

// TestHandoffTeam_lookupCoordinator_无协调器 测试 lookupCoordinator 无协调器
func TestHandoffTeam_lookupCoordinator_无协调器(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	found := team.lookupCoordinator("nonexistent")
	if found != nil {
		t.Error("lookupCoordinator 对不存在的会话应返回 nil")
	}
}

// TestHandoffTeam_Configure 测试 Configure 方法
func TestHandoffTeam_Configure(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	newConfig := maschema.NewTeamConfig()
	newConfig.MaxAgents = 20

	err := team.Configure(context.Background(), *newConfig)
	if err != nil {
		t.Fatalf("Configure 失败: %v", err)
	}

	if team.Config().MaxAgents != 20 {
		t.Errorf("MaxAgents = %d, want 20", team.Config().MaxAgents)
	}
}

// TestHandoffTeam_GetAgentCard 测试 GetAgentCard 方法
func TestHandoffTeam_GetAgentCard(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	agentCard := agentschema.NewAgentCard(
		schema.WithID("agent1"),
		schema.WithName("Agent1"),
	)
	provider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}
	_ = team.AddAgent(context.Background(), agentCard, provider)

	found, err := team.GetAgentCard("agent1")
	if err != nil {
		t.Fatalf("GetAgentCard 失败: %v", err)
	}
	if found.ID != "agent1" {
		t.Errorf("GetAgentCard ID = %q, want %q", found.ID, "agent1")
	}
}

// TestFilterInterruptHistory 测试过滤中断历史
func TestFilterInterruptHistory(t *testing.T) {
	history := []HandoffHistoryEntry{
		{
			AgentID: "agent1",
			Output:  map[string]any{"result": "正常结果"},
		},
		{
			AgentID: "agent2",
			Output:  map[string]any{"result_type": "interrupt", "message": "中断"},
		},
		{
			AgentID: "agent3",
			Output:  map[string]any{"result": "另一个正常结果"},
		},
		{
			AgentID: "agent4",
			Output:  nil,
		},
	}

	filtered := filterInterruptHistory(history)

	if len(filtered) != 3 {
		t.Fatalf("filterInterruptHistory 长度 = %d, want 3", len(filtered))
	}

	if filtered[0].AgentID != "agent1" {
		t.Errorf("filtered[0] AgentID = %q, want %q", filtered[0].AgentID, "agent1")
	}
	if filtered[1].AgentID != "agent3" {
		t.Errorf("filtered[1] AgentID = %q, want %q", filtered[1].AgentID, "agent3")
	}
	if filtered[2].AgentID != "agent4" {
		t.Errorf("filtered[2] AgentID = %q, want %q", filtered[2].AgentID, "agent4")
	}
}

// TestFilterInterruptHistory_空历史 测试过滤空历史
func TestFilterInterruptHistory_空历史(t *testing.T) {
	filtered := filterInterruptHistory(nil)
	if len(filtered) != 0 {
		t.Errorf("filterInterruptHistory(nil) 长度 = %d, want 0", len(filtered))
	}
}

// TestHandoffTeam_makeContainerProvider 测试 makeContainerProvider 创建 ContainerAgent provider
func TestHandoffTeam_makeContainerProvider(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	agentCard := agentschema.NewAgentCard(
		schema.WithID("agent1"),
		schema.WithName("Agent1"),
	)
	provider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}
	team.agentProviders["agent1"] = provider

	allowedTargets := map[string]struct{}{
		"agent2": {},
		"agent3": {},
	}

	containerProvider := team.makeContainerProvider(agentCard, "agent1", allowedTargets)

	// 调用 provider 创建 ContainerAgent
	agent, err := containerProvider(context.Background(), nil)
	if err != nil {
		t.Fatalf("containerProvider 调用失败: %v", err)
	}

	container, ok := agent.(*ContainerAgent)
	if !ok {
		t.Fatal("containerProvider 应返回 *ContainerAgent")
	}

	if container.targetCard.ID != "agent1" {
		t.Errorf("ContainerAgent targetCard ID = %q, want %q", container.targetCard.ID, "agent1")
	}
}

// TestHandoffTeam_ensureInternalAgents 测试 ensureInternalAgents 双检锁
func TestHandoffTeam_ensureInternalAgents(t *testing.T) {
	agent1 := agentschema.NewAgentCard(
		schema.WithID("agent1"),
		schema.WithName("Agent1"),
	)
	agent2 := agentschema.NewAgentCard(
		schema.WithID("agent2"),
		schema.WithName("Agent2"),
	)

	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
		maschema.WithAgentCards([]*agentschema.AgentCard{agent1, agent2}),
	)

	team := NewHandoffTeam(card, nil, nil)

	provider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}
	_ = team.AddAgent(context.Background(), agent1, provider)
	_ = team.AddAgent(context.Background(), agent2, provider)

	err := team.ensureInternalAgents(context.Background())
	if err != nil {
		t.Fatalf("ensureInternalAgents 失败: %v", err)
	}

	if !team.internalAgentsReady {
		t.Error("ensureInternalAgents 后 internalAgentsReady 应为 true")
	}

	// 检查端点 Agent 是否注册到运行时
	ep1ID := "__handoff_ep_team1_agent1"
	ep2ID := "__handoff_ep_team1_agent2"

	if !team.runtime.HasAgent(ep1ID) {
		t.Error("端点 __handoff_ep_team1_agent1 应已注册")
	}
	if !team.runtime.HasAgent(ep2ID) {
		t.Error("端点 __handoff_ep_team1_agent2 应已注册")
	}

	// 再次调用应是幂等的
	err = team.ensureInternalAgents(context.Background())
	if err != nil {
		t.Fatalf("重复 ensureInternalAgents 失败: %v", err)
	}
}

// TestHandoffTeam_ensureInternalAgents_并发 测试 ensureInternalAgents 并发安全
func TestHandoffTeam_ensureInternalAgents_并发(t *testing.T) {
	agent1 := agentschema.NewAgentCard(
		schema.WithID("agent1"),
		schema.WithName("Agent1"),
	)

	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
		maschema.WithAgentCards([]*agentschema.AgentCard{agent1}),
	)

	team := NewHandoffTeam(card, nil, nil)

	provider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}
	_ = team.AddAgent(context.Background(), agent1, provider)

	var wg sync.WaitGroup
	errCount := 0
	var mu sync.Mutex

	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := team.ensureInternalAgents(context.Background()); err != nil {
				mu.Lock()
				errCount++
				mu.Unlock()
			}
		}()
	}

	wg.Wait()

	if errCount > 0 {
		t.Errorf("并发 ensureInternalAgents 有 %d 个错误", errCount)
	}

	if !team.internalAgentsReady {
		t.Error("并发 ensureInternalAgents 后 internalAgentsReady 应为 true")
	}
}

// TestHandoffTeam_implementsBaseTeam 编译时验证 HandoffTeam 实现 BaseTeam 接口
func TestHandoffTeam_implementsBaseTeam(t *testing.T) {
	var _ maschema.BaseTeam = (*HandoffTeam)(nil)
}

// TestHandoffTeam_GetRuntime 测试 GetRuntime 方法
func TestHandoffTeam_GetRuntime(t *testing.T) {
	card := maschema.NewTeamCard(
		maschema.WithTeamCardID("team1"),
	)
	team := NewHandoffTeam(card, nil, nil)

	if team.GetRuntime() == nil {
		t.Error("GetRuntime 不应返回 nil")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
