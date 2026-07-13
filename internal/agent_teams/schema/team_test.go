package schema

import (
	"encoding/json"
	"testing"
)

// TestNewMemberOpResultSuccess 测试创建成功结果
func TestNewMemberOpResultSuccess(t *testing.T) {
	r := NewMemberOpResultSuccess()
	if !r.OK {
		t.Error("期望 OK=true")
	}
	if r.Reason != "" {
		t.Errorf("期望 Reason 为空，实际=%q", r.Reason)
	}
}

// TestNewMemberOpResultFail 测试创建失败结果
func TestNewMemberOpResultFail(t *testing.T) {
	r := NewMemberOpResultFail("出错了")
	if r.OK {
		t.Error("期望 OK=false")
	}
	if r.Reason != "出错了" {
		t.Errorf("期望 '出错了'，实际=%q", r.Reason)
	}
}

// TestTeamMemberSpec_JSON序列化 测试 TeamMemberSpec JSON
func TestTeamMemberSpec_JSON序列化(t *testing.T) {
	spec := TeamMemberSpec{MemberName: "coder", DisplayName: "代码员", RoleType: TeamRoleTeammate, ModelName: "qwen-max"}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var decoded TeamMemberSpec
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if decoded.MemberName != spec.MemberName {
		t.Errorf("不匹配")
	}
}

// TestTeamSpec_JSON序列化 测试 TeamSpec JSON
func TestTeamSpec_JSON序列化(t *testing.T) {
	spec := TeamSpec{TeamName: "test-team", DisplayName: "测试团队", LeaderMemberName: "team_leader", Language: "cn"}
	data, err := json.Marshal(spec)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var decoded TeamSpec
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if decoded.TeamName != spec.TeamName {
		t.Errorf("不匹配")
	}
}

// TestTeamRuntimeContext_JSON序列化 测试 TeamRuntimeContext JSON
func TestTeamRuntimeContext_JSON序列化(t *testing.T) {
	ctx := TeamRuntimeContext{Role: TeamRoleLeader, MemberName: "team_leader", Persona: "管理者"}
	data, err := json.Marshal(ctx)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var decoded TeamRuntimeContext
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if decoded.Role != ctx.Role {
		t.Errorf("不匹配")
	}
}

// TestTeamLifecycle_值 测试 TeamLifecycle 枚举值
func TestTeamLifecycle_值(t *testing.T) {
	if string(TeamLifecycleTemporary) != "temporary" {
		t.Errorf("不匹配")
	}
	if string(TeamLifecyclePersistent) != "persistent" {
		t.Errorf("不匹配")
	}
}

// TestTeamRole_值 测试 TeamRole 枚举值
func TestTeamRole_值(t *testing.T) {
	if string(TeamRoleLeader) != "leader" {
		t.Errorf("不匹配")
	}
	if string(TeamRoleTeammate) != "teammate" {
		t.Errorf("不匹配")
	}
	if string(TeamRoleHumanAgent) != "human_agent" {
		t.Errorf("不匹配")
	}
}
