package prompts

import (
	"strings"
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// TestRolePolicy_Leader 测试 Leader 角色策略
func TestRolePolicy_Leader(t *testing.T) {
	result := mustRolePolicy(t, atschema.TeamRoleLeader, "cn")
	if result == "" {
		t.Fatal("mustRolePolicy(t, leader, cn) returned empty")
	}
	// 应包含角色相关策略文本
	result = mustRolePolicy(t, atschema.TeamRoleLeader, "en")
	if result == "" {
		t.Fatal("mustRolePolicy(t, leader, en) returned empty")
	}
}

// TestRolePolicy_Teammate 测试 Teammate 角色策略
func TestRolePolicy_Teammate(t *testing.T) {
	result := mustRolePolicy(t, atschema.TeamRoleTeammate, "cn")
	if result == "" {
		t.Fatal("mustRolePolicy(t, teammate, cn) returned empty")
	}
	result = mustRolePolicy(t, atschema.TeamRoleTeammate, "en")
	if result == "" {
		t.Fatal("mustRolePolicy(t, teammate, en) returned empty")
	}
}

// TestRolePolicy_HumanAgent 测试 HumanAgent 使用 teammate 策略
func TestRolePolicy_HumanAgent(t *testing.T) {
	result := mustRolePolicy(t, atschema.TeamRoleHumanAgent, "cn")
	if result == "" {
		t.Fatal("mustRolePolicy(t, human_agent, cn) returned empty")
	}
	// human_agent 不是 leader，所以应该加载 teammate_policy
	teammateResult := mustRolePolicy(t, atschema.TeamRoleTeammate, "cn")
	if result != teammateResult {
		t.Fatal("human_agent should use teammate_policy (same as teammate)")
	}
}

// TestBuildSystemPrompt 完整组装测试
func TestBuildSystemPrompt(t *testing.T) {
	info := &TeamInfo{TeamName: "test", DisplayName: "Test", Description: "Desc"}
	members := []TeamMember{{MemberName: "bob", DisplayName: "Bob", Description: "Eng"}}
	result := mustBuildSystemPrompt(t, "alice", atschema.TeamRoleLeader, "cn", "高级工程师", "temporary", "default",
		info, members, "extra prompt")
	if result == "" {
		t.Fatal("BuildSystemPrompt returned empty")
	}
	// 应包含 member_name
	if !strings.Contains(result, "alice") {
		t.Fatal("BuildSystemPrompt should contain member_name")
	}
	// 应包含 persona
	if !strings.Contains(result, "高级工程师") {
		t.Fatal("BuildSystemPrompt should contain persona")
	}
	// 应包含 base prompt
	if !strings.Contains(result, "extra prompt") {
		t.Fatal("BuildSystemPrompt should contain base prompt")
	}
}

// TestBuildSystemPrompt_Teammate 测试 Teammate 组装（无工作流/生命周期）
func TestBuildSystemPrompt_Teammate(t *testing.T) {
	result := mustBuildSystemPrompt(t, "bob", atschema.TeamRoleTeammate, "cn", "助手", "temporary", "default",
		nil, nil, "")
	if result == "" {
		t.Fatal("BuildSystemPrompt returned empty for teammate")
	}
	// Teammate 不应有工作流内容
	leaderResult := mustBuildSystemPrompt(t, "alice", atschema.TeamRoleLeader, "cn", "人设", "temporary", "default",
		nil, nil, "")
	if len(result) >= len(leaderResult) {
		t.Fatal("Teammate system prompt should be shorter than leader's (no workflow/lifecycle)")
	}
}

// TestBuildSystemPrompt_English 测试英文组装
func TestBuildSystemPrompt_English(t *testing.T) {
	info := &TeamInfo{TeamName: "team1", DisplayName: "Team One", Description: "Test team"}
	result := mustBuildSystemPrompt(t, "leader1", atschema.TeamRoleLeader, "en", "Senior Engineer", "persistent", "hybrid",
		info, nil, "")
	if result == "" {
		t.Fatal("BuildSystemPrompt returned empty for English")
	}
	if !strings.Contains(result, "leader1") {
		t.Fatal("English system prompt should contain member_name")
	}
	if !strings.Contains(result, "Senior Engineer") {
		t.Fatal("English system prompt should contain persona")
	}
}

// TestBuildSystemPrompt_无BasePrompt 测试无 base prompt 时无多余换行
func TestBuildSystemPrompt_无BasePrompt(t *testing.T) {
	result := mustBuildSystemPrompt(t, "bob", atschema.TeamRoleTeammate, "cn", "助手", "temporary", "default",
		nil, nil, "")
	if result == "" {
		t.Fatal("BuildSystemPrompt returned empty")
	}
}

// TestBuildSystemPrompt_PersistentLifecycle 测试 persistent 生命周期
func TestBuildSystemPrompt_PersistentLifecycle(t *testing.T) {
	result := mustBuildSystemPrompt(t, "alice", "leader", "cn", "人设", "persistent", "default",
		nil, nil, "")
	if result == "" {
		t.Fatal("BuildSystemPrompt returned empty for persistent lifecycle")
	}
}

// TestPolicyLabelsFor 测试 policy 标签回退
func TestPolicyLabelsFor(t *testing.T) {
	lbl := policyLabelsFor("cn")
	if lbl["persona"] == "" {
		t.Fatal("cn persona label should not be empty")
	}
	lbl = policyLabelsFor("en")
	if lbl["persona"] == "" {
		t.Fatal("en persona label should not be empty")
	}
	lbl = policyLabelsFor("fr")
	if lbl["persona"] != policyLabels["cn"]["persona"] {
		t.Fatal("unknown language should fallback to cn")
	}
}

// TestFormatTeamInfo 测试团队信息格式化
func TestFormatTeamInfo(t *testing.T) {
	lbl := policyLabelsFor("cn")
	info := &TeamInfo{TeamName: "my-team", DisplayName: "My Team", Description: "A test team"}
	result := formatTeamInfo(info, lbl)
	if !strings.Contains(result, "团队信息") {
		t.Fatal("should contain team info heading")
	}
	if !strings.Contains(result, "my-team") {
		t.Fatal("should contain team_name")
	}
}

// TestFormatTeamMembers 测试成员关系格式化
func TestFormatTeamMembers(t *testing.T) {
	lbl := policyLabelsFor("cn")
	members := []TeamMember{
		{MemberName: "bob", DisplayName: "Bob", Description: "Eng"},
		{MemberName: "alice", DisplayName: "Alice", Description: "Lead"},
	}
	result := formatTeamMembers(members, lbl, "alice")
	if !strings.Contains(result, "成员关系") {
		t.Fatal("should contain relationships heading")
	}
	if !strings.Contains(result, "bob") {
		t.Fatal("should contain bob")
	}
	if strings.Contains(result, "alice") {
		t.Fatal("should NOT contain self member alice")
	}
}

// mustRolePolicy 断言 RolePolicy 无错误并返回策略文本。
func mustRolePolicy(t *testing.T, role atschema.TeamRole, language string) string {
	t.Helper()
	text, err := RolePolicy(role, language)
	if err != nil {
		t.Fatalf("RolePolicy(%v, %s) 返回错误: %v", role, language, err)
	}
	return text
}

// mustBuildSystemPrompt 断言 BuildSystemPrompt 无错误并返回渲染结果。
func mustBuildSystemPrompt(
	t *testing.T,
	memberName string,
	role atschema.TeamRole,
	language string,
	persona string,
	lifecycle string,
	teamMode string,
	teamInfo *TeamInfo,
	teamMembers []TeamMember,
	basePrompt string,
) string {
	t.Helper()
	text, err := BuildSystemPrompt(memberName, role, language, persona, lifecycle, teamMode,
		teamInfo, teamMembers, basePrompt)
	if err != nil {
		t.Fatalf("BuildSystemPrompt 返回错误: %v", err)
	}
	return text
}

// TestRolePolicy_模板缺失返回错误 语言无效时模板缺失应报错而非返回空串
func TestRolePolicy_模板缺失返回错误(t *testing.T) {
	if _, err := RolePolicy(atschema.TeamRoleLeader, "zz"); err == nil {
		t.Fatal("模板缺失时 RolePolicy 应返回错误")
	}
}

// TestBuildTeamRoleSection_模板缺失返回错误 语言无效时模板缺失应报错
func TestBuildTeamRoleSection_模板缺失返回错误(t *testing.T) {
	if _, err := BuildTeamRoleSection(atschema.TeamRoleLeader, "alice", "build_mode", "zz"); err == nil {
		t.Fatal("模板缺失时 BuildTeamRoleSection 应返回错误")
	}
}
