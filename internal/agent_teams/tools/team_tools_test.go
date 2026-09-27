package tools

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// TestCreateTeamTools_Leader 测试 Leader 角色工具列表
func TestCreateTeamTools_Leader(t *testing.T) {
	backend := newTestTeamBackend()
	toolList := CreateTeamTools(backend, "leader", "build_mode", "temporary", "cn", nil, nil, nil)
	if len(toolList) == 0 {
		t.Fatal("Leader should have at least one tool")
	}
	names := toolNames(toolList)
	// Leader 应有 create_task
	if _, ok := names["create_task"]; !ok {
		t.Fatal("Leader should have create_task")
	}
	// Leader 不应有 claim_task
	if _, ok := names["claim_task"]; ok {
		t.Fatal("Leader should not have claim_task")
	}
}

// TestCreateTeamTools_Teammate 测试 Teammate 角色工具列表
func TestCreateTeamTools_Teammate(t *testing.T) {
	backend := newTestTeamBackend()
	toolList := CreateTeamTools(backend, "teammate", "build_mode", "temporary", "cn", nil, nil, nil)
	if len(toolList) == 0 {
		t.Fatal("Teammate should have at least one tool")
	}
	names := toolNames(toolList)
	// Teammate 应有 claim_task 但不应有 create_task
	if _, ok := names["claim_task"]; !ok {
		t.Fatal("Teammate should have claim_task")
	}
	if _, ok := names["create_task"]; ok {
		t.Fatal("Teammate should not have create_task")
	}
}

// TestCreateTeamTools_HumanAgent 测试 Human-Agent 角色工具列表
func TestCreateTeamTools_HumanAgent(t *testing.T) {
	backend := newTestTeamBackend()
	toolList := CreateTeamTools(backend, "human_agent", "build_mode", "temporary", "cn", nil, nil, nil)
	if len(toolList) == 0 {
		t.Fatal("Human-Agent should have at least one tool")
	}
	names := toolNames(toolList)
	if _, ok := names["view_task"]; !ok {
		t.Fatal("Human-Agent should have view_task")
	}
	if _, ok := names["member_complete_task"]; !ok {
		t.Fatal("Human-Agent should have member_complete_task")
	}
	if _, ok := names["send_message"]; !ok {
		t.Fatal("Human-Agent should have send_message")
	}
	// Human-Agent 不应有 claim_task / create_task
	if _, ok := names["claim_task"]; ok {
		t.Fatal("Human-Agent should not have claim_task")
	}
	if _, ok := names["create_task"]; ok {
		t.Fatal("Human-Agent should not have create_task")
	}
}

// TestCreateTeamTools_PlanMode 测试 plan_mode 下审批工具
func TestCreateTeamTools_PlanMode(t *testing.T) {
	backend := newTestTeamBackend()
	toolList := CreateTeamTools(backend, "leader", "plan_mode", "temporary", "cn", nil, nil, nil)
	names := toolNames(toolList)
	if _, ok := names["approve_plan"]; !ok {
		t.Fatal("Leader in plan_mode should have approve_plan")
	}
	if _, ok := names["approve_tool"]; !ok {
		t.Fatal("Leader in plan_mode should have approve_tool")
	}
}

// TestCreateTeamTools_BuildMode无审批工具 测试 build_mode 下无审批工具
func TestCreateTeamTools_BuildMode无审批工具(t *testing.T) {
	backend := newTestTeamBackend()
	toolList := CreateTeamTools(backend, "leader", "build_mode", "temporary", "cn", nil, nil, nil)
	names := toolNames(toolList)
	if _, ok := names["approve_plan"]; ok {
		t.Fatal("Leader in build_mode should not have approve_plan")
	}
	if _, ok := names["approve_tool"]; ok {
		t.Fatal("Leader in build_mode should not have approve_tool")
	}
	if _, ok := names["submit_plan"]; ok {
		t.Fatal("Leader in build_mode should not have submit_plan")
	}
}

// TestCreateTeamTools_Persistent无CleanTeam 测试 persistent 生命周期无 clean_team
func TestCreateTeamTools_Persistent无CleanTeam(t *testing.T) {
	backend := newTestTeamBackend()
	toolList := CreateTeamTools(backend, "leader", "build_mode", "persistent", "cn", nil, nil, nil)
	names := toolNames(toolList)
	if _, ok := names["clean_team"]; ok {
		t.Fatal("Leader in persistent lifecycle should not have clean_team")
	}
}

// TestCreateTeamTools_Temporary有CleanTeam 测试 temporary 生命周期有 clean_team
func TestCreateTeamTools_Temporary有CleanTeam(t *testing.T) {
	backend := newTestTeamBackend()
	toolList := CreateTeamTools(backend, "leader", "build_mode", "temporary", "cn", nil, nil, nil)
	names := toolNames(toolList)
	if _, ok := names["clean_team"]; !ok {
		t.Fatal("Leader in temporary lifecycle should have clean_team")
	}
}

// TestCreateTeamTools_ExcludeTools 测试排除工具
func TestCreateTeamTools_ExcludeTools(t *testing.T) {
	backend := newTestTeamBackend()
	exclude := map[string]struct{}{"build_team": {}}
	toolList := CreateTeamTools(backend, "leader", "build_mode", "temporary", "cn", nil, nil, exclude)
	names := toolNames(toolList)
	if _, ok := names["build_team"]; ok {
		t.Fatal("Excluded tool should not be present")
	}
}

// TestQualifyTeamToolIDs 测试工具 ID 后缀
func TestQualifyTeamToolIDs(t *testing.T) {
	backend := newTestTeamBackend()
	toolList := CreateTeamTools(backend, "leader", "build_mode", "temporary", "cn", nil, nil, nil)
	QualifyTeamToolIDs(toolList, "my-team", "alice")
	for _, tl := range toolList {
		card := tl.Card()
		if card == nil {
			continue
		}
		expectedSuffix := ".my-team.alice"
		if card.ID != "" && len(card.ID) < len(expectedSuffix) {
			t.Fatalf("tool ID %q too short after qualification", card.ID)
		}
		if card.ID != "" && card.ID[len(card.ID)-len(expectedSuffix):] != expectedSuffix {
			t.Fatalf("tool ID %q should end with %s after qualification", card.ID, expectedSuffix)
		}
	}
}

// TestQualifyTeamToolIDs_默认值 测试空 team/member 时使用默认值
func TestQualifyTeamToolIDs_默认值(t *testing.T) {
	backend := newTestTeamBackend()
	toolList := CreateTeamTools(backend, "leader", "build_mode", "temporary", "cn", nil, nil, nil)
	QualifyTeamToolIDs(toolList, "", "")
	for _, tl := range toolList {
		card := tl.Card()
		if card == nil || card.ID == "" {
			continue
		}
		expectedSuffix := ".default.unknown"
		if card.ID[len(card.ID)-len(expectedSuffix):] != expectedSuffix {
			t.Fatalf("tool ID %q should end with %s for empty team/member", card.ID, expectedSuffix)
		}
	}
}

// TestLeaderSet 对齐 Python 权限集合
func TestLeaderSet(t *testing.T) {
	for _, name := range []string{"build_team", "clean_team", "spawn_member", "shutdown_member",
		"approve_plan", "approve_tool", "create_task", "update_task", "list_members",
		"view_task", "send_message"} {
		if _, ok := leaderSet[name]; !ok {
			t.Fatalf("leaderSet should contain %s", name)
		}
	}
}

// TestMemberSet 对齐 Python 权限集合
func TestMemberSet(t *testing.T) {
	for _, name := range []string{"claim_task", "submit_plan", "view_task", "send_message"} {
		if _, ok := memberSet[name]; !ok {
			t.Fatalf("memberSet should contain %s", name)
		}
	}
}

// TestHumanAgentSet 对齐 Python 权限集合
func TestHumanAgentSet(t *testing.T) {
	for _, name := range []string{"view_task", "member_complete_task", "send_message"} {
		if _, ok := humanAgentSet[name]; !ok {
			t.Fatalf("humanAgentSet should contain %s", name)
		}
	}
}

// TestMemberNameRegexp 测试成员名正则
func TestMemberNameRegexp(t *testing.T) {
	pattern := MemberNameRegexp()
	// 合法名称
	if !pattern.MatchString("alice") {
		t.Fatal("alice should match member name pattern")
	}
	if !pattern.MatchString("dev-1") {
		t.Fatal("dev-1 should match member name pattern")
	}
	// 非法名称
	if pattern.MatchString("Alice") {
		t.Fatal("Alice (uppercase) should not match member name pattern")
	}
	if pattern.MatchString("1alice") {
		t.Fatal("1alice (leading digit) should not match member name pattern")
	}
	if pattern.MatchString("中文") {
		t.Fatal("中文 should not match member name pattern")
	}
}

// toolNames 从工具列表提取名称映射
func toolNames(tools []tool.Tool) map[string]struct{} {
	names := make(map[string]struct{})
	for _, tl := range tools {
		if tl.Card() != nil {
			names[tl.Card().Name] = struct{}{}
		}
	}
	return names
}
