package hierarchical_msgbus

import (
	"context"
	"testing"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/team_runtime"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewHierarchicalTeam 验证构造函数。
func TestNewHierarchicalTeam(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
		maschema.WithTeamCardName("hierarchical_team"),
	)
	supervisorCard := agentschema.NewAgentCard(
		cschema.WithName("supervisor"),
		cschema.WithID("supervisor_id"),
	)
	config := &HierarchicalTeamConfig{
		SupervisorAgent: supervisorCard,
		Timeout:         600.0,
	}

	team := NewHierarchicalTeam(teamCard, config, nil)
	if team == nil {
		t.Fatal("期望 team 非空")
	}
	if team.supervisorID != "supervisor_id" {
		t.Errorf("期望 supervisorID = supervisor_id, 实际 = %s", team.supervisorID)
	}
	if team.config.Timeout != 600.0 {
		t.Errorf("期望 Timeout = 600.0, 实际 = %v", team.config.Timeout)
	}
}

// TestNewHierarchicalTeam_默认配置 验证 nil config 使用默认值。
func TestNewHierarchicalTeam_默认配置(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)
	if team.config.Timeout != defaultP2PTimeout {
		t.Errorf("期望 Timeout = %v, 实际 = %v", defaultP2PTimeout, team.config.Timeout)
	}
}

// TestNewHierarchicalTeam_传入Runtime 验证传入自定义 runtime。
func TestNewHierarchicalTeam_传入Runtime(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	rtCfg := team_runtime.NewRuntimeConfig(
		team_runtime.WithRuntimeTeamID("team_1"),
	)
	rt := team_runtime.NewTeamRuntime(*rtCfg)

	team := NewHierarchicalTeam(teamCard, nil, rt)
	if team.runtime != rt {
		t.Error("期望使用传入的 runtime")
	}
}

// TestHierarchicalTeam_Invoke_Supervisor未注册 验证 assertReady 报错。
func TestHierarchicalTeam_Invoke_Supervisor未注册(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	supervisorCard := agentschema.NewAgentCard(
		cschema.WithName("supervisor"),
		cschema.WithID("supervisor_id"),
	)
	config := &HierarchicalTeamConfig{
		SupervisorAgent: supervisorCard,
	}

	team := NewHierarchicalTeam(teamCard, config, nil)

	// 未注册 supervisor 到 runtime，invoke 应报错
	_, err := team.Invoke(context.Background(), map[string]any{"query": "hello"})
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalTeam_Invoke_未配置Supervisor 验证无 supervisor 时报错。
func TestHierarchicalTeam_Invoke_未配置Supervisor(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	config := NewHierarchicalTeamConfig() // SupervisorAgent 为 nil

	team := NewHierarchicalTeam(teamCard, config, nil)

	_, err := team.Invoke(context.Background(), map[string]any{"query": "hello"})
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalTeam_Card 验证 Card 返回。
func TestHierarchicalTeam_Card(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)
	if team.Card().GetID() != "team_1" {
		t.Errorf("期望 Card ID = team_1, 实际 = %s", team.Card().GetID())
	}
}

// TestHierarchicalTeam_GetAgentCount 验证空团队 Agent 数量为 0。
func TestHierarchicalTeam_GetAgentCount(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)
	if team.GetAgentCount() != 0 {
		t.Errorf("期望 AgentCount = 0, 实际 = %d", team.GetAgentCount())
	}
}

// TestHierarchicalTeam_满足BaseTeam接口 编译时接口检查。
func TestHierarchicalTeam_满足BaseTeam接口(t *testing.T) {
	var _ maschema.BaseTeam = (*HierarchicalTeam)(nil)
	t.Log("HierarchicalTeam 满足 BaseTeam 接口")
}

// dummyProvider 创建空 Agent 提供者，用于测试。
func dummyProvider() maschema.TeamAgentProvider {
	return func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}
}

// TestHierarchicalTeam_AddAgent 验证向团队注册 Agent。
func TestHierarchicalTeam_AddAgent(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)

	agentCard := agentschema.NewAgentCard(
		cschema.WithName("worker"),
		cschema.WithID("worker_1"),
	)

	err := team.AddAgent(context.Background(), agentCard, dummyProvider())
	if err != nil {
		t.Errorf("期望无错误，实际 = %v", err)
	}
	if !team.runtime.HasAgent("worker_1") {
		t.Error("期望 Agent 已注册到运行时")
	}
}

// TestHierarchicalTeam_AddAgent_重复注册 验证重复注册跳过。
func TestHierarchicalTeam_AddAgent_重复注册(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)

	agentCard := agentschema.NewAgentCard(
		cschema.WithName("worker"),
		cschema.WithID("worker_1"),
	)

	// 第一次注册
	err := team.AddAgent(context.Background(), agentCard, dummyProvider())
	if err != nil {
		t.Fatalf("第一次注册期望无错误，实际 = %v", err)
	}
	// 第二次注册（应跳过）
	err = team.AddAgent(context.Background(), agentCard, dummyProvider())
	if err != nil {
		t.Errorf("重复注册期望无错误，实际 = %v", err)
	}
}

// TestHierarchicalTeam_AddAgent_注册Supervisor 验证注册 supervisor 时设置 P2P timeout。
func TestHierarchicalTeam_AddAgent_注册Supervisor(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	supervisorCard := agentschema.NewAgentCard(
		cschema.WithName("supervisor"),
		cschema.WithID("supervisor_1"),
	)
	config := &HierarchicalTeamConfig{
		SupervisorAgent: supervisorCard,
		Timeout:         600.0,
	}
	team := NewHierarchicalTeam(teamCard, config, nil)

	err := team.AddAgent(context.Background(), supervisorCard, dummyProvider())
	if err != nil {
		t.Errorf("期望无错误，实际 = %v", err)
	}
	// 注册 supervisor 后 P2P timeout 应被设置
	if team.runtime.GetP2PTimeout() != 600.0 {
		t.Errorf("期望 P2PTimeout = 600.0, 实际 = %v", team.runtime.GetP2PTimeout())
	}
}

// TestHierarchicalTeam_RemoveAgent 验证从团队注销 Agent。
func TestHierarchicalTeam_RemoveAgent(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)

	agentCard := agentschema.NewAgentCard(
		cschema.WithName("worker"),
		cschema.WithID("worker_1"),
	)

	// 先注册
	err := team.AddAgent(context.Background(), agentCard, dummyProvider())
	if err != nil {
		t.Fatalf("注册期望无错误，实际 = %v", err)
	}

	// 再注销
	err = team.RemoveAgent(context.Background(), "worker_1")
	if err != nil {
		t.Errorf("注销期望无错误，实际 = %v", err)
	}
	if team.runtime.HasAgent("worker_1") {
		t.Error("期望 Agent 已从运行时注销")
	}
}

// TestHierarchicalTeam_RemoveAgent_不存在 验证注销不存在的 Agent 报错。
func TestHierarchicalTeam_RemoveAgent_不存在(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)

	err := team.RemoveAgent(context.Background(), "non_existent")
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalTeam_Send 验证 P2P 发送委托运行时。
func TestHierarchicalTeam_Send(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)

	// 运行时未启动，Send 应返回错误
	_, err := team.Send(context.Background(), map[string]any{"query": "hello"}, "recipient", "sender")
	if err == nil {
		t.Error("期望错误（运行时未启动），实际为 nil")
	}
}

// TestHierarchicalTeam_Publish 验证 Pub-Sub 发布委托运行时。
func TestHierarchicalTeam_Publish(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)

	// 运行时未启动，Publish 应返回错误
	err := team.Publish(context.Background(), map[string]any{"query": "hello"}, "topic_1", "sender")
	if err == nil {
		t.Error("期望错误（运行时未启动），实际为 nil")
	}
}

// TestHierarchicalTeam_Subscribe 验证订阅委托运行时。
func TestHierarchicalTeam_Subscribe(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)

	// 运行时未启动，Subscribe 不会报错（messageBus 为 nil 时静默返回）
	err := team.Subscribe(context.Background(), "agent_1", "topic_1")
	if err != nil {
		t.Errorf("期望无错误，实际 = %v", err)
	}
}

// TestHierarchicalTeam_Unsubscribe 验证取消订阅委托运行时。
func TestHierarchicalTeam_Unsubscribe(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)

	err := team.Unsubscribe(context.Background(), "agent_1", "topic_1")
	if err != nil {
		t.Errorf("期望无错误，实际 = %v", err)
	}
}

// TestHierarchicalTeam_Configure 验证配置团队。
func TestHierarchicalTeam_Configure(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)

	newConfig := maschema.NewTeamConfig()
	newConfig.MaxAgents = 20

	err := team.Configure(context.Background(), *newConfig)
	if err != nil {
		t.Errorf("期望无错误，实际 = %v", err)
	}
	if team.Config().MaxAgents != 20 {
		t.Errorf("期望 MaxAgents = 20, 实际 = %d", team.Config().MaxAgents)
	}
}

// TestHierarchicalTeam_Config 验证 Config 返回。
func TestHierarchicalTeam_Config(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)

	cfg := team.Config()
	if cfg == nil {
		t.Fatal("期望 Config 非空")
	}
}

// TestHierarchicalTeam_GetAgentCard 验证获取 Agent 卡片。
func TestHierarchicalTeam_GetAgentCard(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)

	// Agent 不存在
	_, err := team.GetAgentCard("non_existent")
	if err == nil {
		t.Error("期望错误（Agent 不存在），实际为 nil")
	}

	// 注册 Agent 后再获取
	agentCard := agentschema.NewAgentCard(
		cschema.WithName("worker"),
		cschema.WithID("worker_1"),
	)
	err = team.AddAgent(context.Background(), agentCard, dummyProvider())
	if err != nil {
		t.Fatalf("注册期望无错误，实际 = %v", err)
	}

	card, err := team.GetAgentCard("worker_1")
	if err != nil {
		t.Errorf("获取期望无错误，实际 = %v", err)
	}
	if card.ID != "worker_1" {
		t.Errorf("期望 ID = worker_1, 实际 = %s", card.ID)
	}
}

// TestHierarchicalTeam_ListAgents 验证列出所有 Agent ID。
func TestHierarchicalTeam_ListAgents(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	team := NewHierarchicalTeam(teamCard, nil, nil)

	// 空团队
	agentList := team.ListAgents()
	if len(agentList) != 0 {
		t.Errorf("期望 0 个 Agent, 实际 = %d", len(agentList))
	}

	// 注册 Agent 后再列出
	agentCard := agentschema.NewAgentCard(
		cschema.WithName("worker"),
		cschema.WithID("worker_1"),
	)
	err := team.AddAgent(context.Background(), agentCard, dummyProvider())
	if err != nil {
		t.Fatalf("注册期望无错误，实际 = %v", err)
	}

	agentList = team.ListAgents()
	if len(agentList) != 1 {
		t.Errorf("期望 1 个 Agent, 实际 = %d", len(agentList))
	}
}

// TestHierarchicalTeam_Stream_Supervisor未注册 验证 Stream 时 assertReady 报错。
func TestHierarchicalTeam_Stream_Supervisor未注册(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	supervisorCard := agentschema.NewAgentCard(
		cschema.WithName("supervisor"),
		cschema.WithID("supervisor_id"),
	)
	config := &HierarchicalTeamConfig{
		SupervisorAgent: supervisorCard,
	}

	team := NewHierarchicalTeam(teamCard, config, nil)

	// 未注册 supervisor 到 runtime，Stream 应报错
	_, err := team.Stream(context.Background(), map[string]any{"query": "hello"})
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalTeam_Stream_未配置Supervisor 验证 Stream 无 supervisor 时报错。
func TestHierarchicalTeam_Stream_未配置Supervisor(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	config := NewHierarchicalTeamConfig()

	team := NewHierarchicalTeam(teamCard, config, nil)

	_, err := team.Stream(context.Background(), map[string]any{"query": "hello"})
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
}

// TestHierarchicalTeam_assertReady_已注册 验证 supervisor 已注册时 assertReady 通过。
func TestHierarchicalTeam_assertReady_已注册(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	supervisorCard := agentschema.NewAgentCard(
		cschema.WithName("supervisor"),
		cschema.WithID("supervisor_id"),
	)
	config := &HierarchicalTeamConfig{
		SupervisorAgent: supervisorCard,
	}
	team := NewHierarchicalTeam(teamCard, config, nil)

	// 注册 supervisor 到 runtime
	err := team.AddAgent(context.Background(), supervisorCard, dummyProvider())
	if err != nil {
		t.Fatalf("注册期望无错误，实际 = %v", err)
	}

	// assertReady 应通过
	err = team.assertReady()
	if err != nil {
		t.Errorf("期望 assertReady 通过，实际 = %v", err)
	}
}

// TestHierarchicalTeam_assertReady_Supervisor未注册 验证 supervisor 未注册时报错。
func TestHierarchicalTeam_assertReady_Supervisor未注册(t *testing.T) {
	teamCard := maschema.NewTeamCard(
		maschema.WithTeamCardID("team_1"),
	)
	supervisorCard := agentschema.NewAgentCard(
		cschema.WithName("supervisor"),
		cschema.WithID("supervisor_id"),
	)
	config := &HierarchicalTeamConfig{
		SupervisorAgent: supervisorCard,
	}
	team := NewHierarchicalTeam(teamCard, config, nil)

	err := team.assertReady()
	if err == nil {
		t.Error("期望错误，实际为 nil")
	}
	// 验证错误码
	baseErr, ok := err.(*exception.BaseError)
	if !ok {
		t.Fatal("期望 BaseError 类型")
	}
	if baseErr.Status() != exception.StatusAgentTeamExecutionError {
		t.Errorf("期望状态码 = %v, 实际 = %v", exception.StatusAgentTeamExecutionError, baseErr.Status())
	}
}
