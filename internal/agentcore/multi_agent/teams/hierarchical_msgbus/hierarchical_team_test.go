package hierarchical_msgbus

import (
	"context"
	"testing"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
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
