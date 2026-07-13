package agent

import (
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── TeamAgentBlueprint ────────────────────────────

// TestTeamAgentBlueprint_Role 测试 Role 属性
func TestTeamAgentBlueprint_Role(t *testing.T) {
	bp := &TeamAgentBlueprint{
		Ctx: atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader},
	}
	if bp.Role() != atschema.TeamRoleLeader {
		t.Errorf("期望 Role=leader, 实际=%q", bp.Role())
	}
}

// TestTeamAgentBlueprint_MemberName_有值 测试 MemberName 非空时返回指针
func TestTeamAgentBlueprint_MemberName_有值(t *testing.T) {
	bp := &TeamAgentBlueprint{
		Ctx: atschema.TeamRuntimeContext{MemberName: "coder"},
	}
	name := bp.MemberName()
	if name == nil {
		t.Fatal("期望非 nil，实际为 nil")
	}
	if *name != "coder" {
		t.Errorf("期望 *name='coder', 实际=%q", *name)
	}
}

// TestTeamAgentBlueprint_MemberName_空 测试 MemberName 为空时返回 nil
func TestTeamAgentBlueprint_MemberName_空(t *testing.T) {
	bp := &TeamAgentBlueprint{
		Ctx: atschema.TeamRuntimeContext{MemberName: ""},
	}
	if bp.MemberName() != nil {
		t.Errorf("期望 nil，实际=%v", bp.MemberName())
	}
}

// TestTeamAgentBlueprint_Lifecycle 测试 Lifecycle 属性
func TestTeamAgentBlueprint_Lifecycle(t *testing.T) {
	bp := &TeamAgentBlueprint{
		Spec: atschema.TeamAgentSpec{Lifecycle: atschema.TeamLifecyclePersistent},
	}
	if bp.Lifecycle() != atschema.TeamLifecyclePersistent {
		t.Errorf("期望 Lifecycle=persistent, 实际=%q", bp.Lifecycle())
	}
}

// TestTeamAgentBlueprint_TeamSpec_有值 测试 TeamSpec 非空时返回指针
func TestTeamAgentBlueprint_TeamSpec_有值(t *testing.T) {
	ts := &atschema.TeamSpec{TeamName: "test_team"}
	bp := &TeamAgentBlueprint{
		Ctx: atschema.TeamRuntimeContext{TeamSpec: ts},
	}
	result := bp.TeamSpec()
	if result == nil {
		t.Fatal("期望非 nil，实际为 nil")
	}
	if result.TeamName != "test_team" {
		t.Errorf("期望 TeamName='test_team', 实际=%q", result.TeamName)
	}
}

// TestTeamAgentBlueprint_TeamSpec_空 测试 TeamSpec 为 nil 时返回 nil
func TestTeamAgentBlueprint_TeamSpec_空(t *testing.T) {
	bp := &TeamAgentBlueprint{
		Ctx: atschema.TeamRuntimeContext{TeamSpec: nil},
	}
	if bp.TeamSpec() != nil {
		t.Errorf("期望 nil，实际=%v", bp.TeamSpec())
	}
}

// TestTeamAgentBlueprint_完整构造 测试完整构造和所有属性
func TestTeamAgentBlueprint_完整构造(t *testing.T) {
	card := agentschema.NewAgentCard()
	card.Name = "test_agent"
	spec := atschema.NewTeamAgentSpec()
	spec.TeamName = "full_team"
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "coder",
	}

	bp := &TeamAgentBlueprint{
		Card:       card,
		Spec:       spec,
		Ctx:        ctx,
		RolePolicy: "default",
		Language:   "cn",
	}

	if bp.Card.Name != "test_agent" {
		t.Errorf("期望 Card.Name='test_agent', 实际=%q", bp.Card.Name)
	}
	if bp.Spec.TeamName != "full_team" {
		t.Errorf("期望 Spec.TeamName='full_team', 实际=%q", bp.Spec.TeamName)
	}
	if bp.Role() != atschema.TeamRoleTeammate {
		t.Errorf("期望 Role=teammate, 实际=%q", bp.Role())
	}
	name := bp.MemberName()
	if name == nil || *name != "coder" {
		t.Errorf("期望 MemberName='coder', 实际=%v", name)
	}
	if bp.RolePolicy != "default" {
		t.Errorf("期望 RolePolicy='default', 实际=%q", bp.RolePolicy)
	}
	if bp.Language != "cn" {
		t.Errorf("期望 Language='cn', 实际=%q", bp.Language)
	}
}
