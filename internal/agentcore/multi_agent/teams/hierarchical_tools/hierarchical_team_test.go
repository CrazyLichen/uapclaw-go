package hierarchical_tools

import (
	"context"
	"testing"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

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

	rootProvider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}
	childProvider := func(ctx context.Context, card *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
		return nil, nil
	}

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
