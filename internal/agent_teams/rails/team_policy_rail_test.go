package rails

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/prompts"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// TestNewTeamPolicyRail_默认值 测试默认构造
func TestNewTeamPolicyRail_默认值(t *testing.T) {
	r := NewTeamPolicyRail()
	if r == nil {
		t.Fatal("NewTeamPolicyRail returned nil")
	}
}

// TestNewTeamPolicyRail_WithOptions 测试选项构造
func TestNewTeamPolicyRail_WithOptions(t *testing.T) {
	r := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyPersona("高级工程师"),
		WithPolicyMemberName("alice"),
		WithPolicyLifecycle("temporary"),
		WithPolicyTeammateMode("build_mode"),
		WithPolicyLanguage("cn"),
		WithPolicyTeamMode("default"),
	)
	if r.role != atschema.TeamRoleLeader {
		t.Fatalf("expected role=leader, got %s", r.role)
	}
	if r.persona != "高级工程师" {
		t.Fatalf("expected persona=高级工程师, got %s", r.persona)
	}
}

// TestTeamPolicyRail_静态Sections_Leader 测试 Leader 静态 Section 数量
func TestTeamPolicyRail_静态Sections_Leader(t *testing.T) {
	r := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyMemberName("alice"),
		WithPolicyTeammateMode("build_mode"),
		WithPolicyLifecycle("temporary"),
		WithPolicyLanguage("cn"),
		WithPolicyTeamMode("default"),
		WithPolicyPersona("高级工程师"),
	)
	// Leader 应有：Role(P:11) + Workflow(P:13) + Lifecycle(P:14) + Persona(P:15) = 4 个
	if len(r.staticSections) < 3 {
		t.Fatalf("expected at least 3 static sections for leader, got %d", len(r.staticSections))
	}
	// 第一个 section 应为 team_role
	if r.staticSections[0].Name != string(prompts.SectionRole) {
		t.Fatalf("expected first section name=%s, got %s", prompts.SectionRole, r.staticSections[0].Name)
	}
}

// TestTeamPolicyRail_静态Sections_Teammate 测试 Teammate 静态 Section
func TestTeamPolicyRail_静态Sections_Teammate(t *testing.T) {
	r := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleTeammate),
		WithPolicyMemberName("bob"),
		WithPolicyTeammateMode("build_mode"),
		WithPolicyLifecycle("temporary"),
		WithPolicyLanguage("cn"),
		WithPolicyTeamMode("default"),
	)
	// Teammate 应有：Role(P:11) = 至少 1 个（无 workflow/lifecycle）
	if len(r.staticSections) < 1 {
		t.Fatalf("expected at least 1 static section for teammate, got %d", len(r.staticSections))
	}
}

// TestTeamPolicyRail_无TeamBackend 测试无 TeamBackend 时不创建动态缓存
func TestTeamPolicyRail_无TeamBackend(t *testing.T) {
	r := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
	)
	if r.infoCache != nil {
		t.Fatal("expected nil infoCache without team_backend")
	}
	if r.membersCache != nil {
		t.Fatal("expected nil membersCache without team_backend")
	}
}

// TestTeamPolicyRail_Init 测试 Init 缓存 prompt builder
func TestTeamPolicyRail_Init(t *testing.T) {
	r := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
	)
	agent := newFakeBaseAgentTeam()
	err := r.Init(context.Background(), agent)
	if err != nil {
		t.Fatalf("Init returned error: %v", err)
	}
	if r.systemPromptBuilder == nil {
		t.Fatal("expected non-nil systemPromptBuilder after Init")
	}
}

// TestTeamPolicyRail_Uninit 测试 Uninit 清理
func TestTeamPolicyRail_Uninit(t *testing.T) {
	r := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
	)
	agent := newFakeBaseAgentTeam()
	_ = r.Init(context.Background(), agent)
	_ = r.Uninit(agent)
	if r.systemPromptBuilder != nil {
		t.Fatal("expected nil systemPromptBuilder after Uninit")
	}
}

// TestTeamPolicyRail_StaticSections 测试 StaticSections 访问器
func TestTeamPolicyRail_StaticSections(t *testing.T) {
	r := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
	)
	sections := r.StaticSections()
	if sections == nil {
		t.Fatal("expected non-nil static sections")
	}
}

// TestTeamPolicyRail_ExtraSection 测试 basePrompt 非空时包含 Extra Section
func TestTeamPolicyRail_ExtraSection(t *testing.T) {
	r := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
		WithPolicyBasePrompt("custom instructions"),
	)
	found := false
	for _, s := range r.staticSections {
		if s.Name == string(prompts.SectionExtra) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected team_extra section when basePrompt is set")
	}
}

// TestTeamPolicyRail_PersonaSection 测试 persona 非空时包含 Persona Section
func TestTeamPolicyRail_PersonaSection(t *testing.T) {
	r := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
		WithPolicyPersona("高级工程师"),
	)
	found := false
	for _, s := range r.staticSections {
		if s.Name == string(prompts.SectionPersona) {
			found = true
		}
	}
	if !found {
		t.Fatal("expected team_persona section when persona is set")
	}
}

// TestTeamPolicyRail_BeforeModelCall 测试 BeforeModelCall 注入 section
func TestTeamPolicyRail_BeforeModelCall(t *testing.T) {
	r := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyMemberName("alice"),
		WithPolicyLanguage("cn"),
	)
	agent := newFakeBaseAgentTeam()
	_ = r.Init(context.Background(), agent)
	err := r.BeforeModelCall(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeforeModelCall returned error: %v", err)
	}
}

// TestTeamPolicyRail_BeforeModelCall_无Builder 测试无 builder 时不 panic
func TestTeamPolicyRail_BeforeModelCall_无Builder(t *testing.T) {
	r := NewTeamPolicyRail(
		WithPolicyRole(atschema.TeamRoleLeader),
		WithPolicyLanguage("cn"),
	)
	// 不调用 Init，systemPromptBuilder 为 nil
	err := r.BeforeModelCall(context.Background(), nil)
	if err != nil {
		t.Fatalf("BeforeModelCall should not fail with nil builder: %v", err)
	}
}
