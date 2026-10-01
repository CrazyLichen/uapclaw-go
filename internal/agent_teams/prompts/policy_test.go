package prompts

import (
	"strings"
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// TestRolePolicy_Leader 测试 Leader 角色策略
func TestRolePolicy_Leader(t *testing.T) {
	result := RolePolicy(atschema.TeamRoleLeader, "cn")
	if result == "" {
		t.Fatal("RolePolicy(leader, cn) returned empty")
	}
	// 应包含角色相关策略文本
	result = RolePolicy(atschema.TeamRoleLeader, "en")
	if result == "" {
		t.Fatal("RolePolicy(leader, en) returned empty")
	}
}

// TestRolePolicy_Teammate 测试 Teammate 角色策略
func TestRolePolicy_Teammate(t *testing.T) {
	result := RolePolicy(atschema.TeamRoleTeammate, "cn")
	if result == "" {
		t.Fatal("RolePolicy(teammate, cn) returned empty")
	}
	result = RolePolicy(atschema.TeamRoleTeammate, "en")
	if result == "" {
		t.Fatal("RolePolicy(teammate, en) returned empty")
	}
}

// TestRolePolicy_HumanAgent 测试 HumanAgent 使用 teammate 策略
func TestRolePolicy_HumanAgent(t *testing.T) {
	result := RolePolicy(atschema.TeamRoleHumanAgent, "cn")
	if result == "" {
		t.Fatal("RolePolicy(human_agent, cn) returned empty")
	}
	// human_agent 不是 leader，所以应该加载 teammate_policy
	teammateResult := RolePolicy(atschema.TeamRoleTeammate, "cn")
	if result != teammateResult {
		t.Fatal("human_agent should use teammate_policy (same as teammate)")
	}
}

// TestBuildSystemPrompt 完整组装测试
func TestBuildSystemPrompt(t *testing.T) {
	info := &TeamInfo{TeamName: "test", DisplayName: "Test", Description: "Desc"}
	members := []TeamMember{{MemberName: "bob", DisplayName: "Bob", Description: "Eng"}}
	result := BuildSystemPrompt("alice", "leader", "cn", "高级工程师", "temporary", "default",
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
	result := BuildSystemPrompt("bob", "teammate", "cn", "助手", "temporary", "default",
		nil, nil, "")
	if result == "" {
		t.Fatal("BuildSystemPrompt returned empty for teammate")
	}
	// Teammate 不应有工作流内容
	leaderResult := BuildSystemPrompt("alice", "leader", "cn", "人设", "temporary", "default",
		nil, nil, "")
	if len(result) >= len(leaderResult) {
		t.Fatal("Teammate system prompt should be shorter than leader's (no workflow/lifecycle)")
	}
}

// TestBuildSystemPrompt_English 测试英文组装
func TestBuildSystemPrompt_English(t *testing.T) {
	info := &TeamInfo{TeamName: "team1", DisplayName: "Team One", Description: "Test team"}
	result := BuildSystemPrompt("leader1", "leader", "en", "Senior Engineer", "persistent", "hybrid",
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
	result := BuildSystemPrompt("bob", "teammate", "cn", "助手", "temporary", "default",
		nil, nil, "")
	if result == "" {
		t.Fatal("BuildSystemPrompt returned empty")
	}
}

// TestBuildSystemPrompt_PersistentLifecycle 测试 persistent 生命周期
func TestBuildSystemPrompt_PersistentLifecycle(t *testing.T) {
	result := BuildSystemPrompt("alice", "leader", "cn", "人设", "persistent", "default",
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
