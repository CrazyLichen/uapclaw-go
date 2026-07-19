package agent

import (
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestResolveTeamMode_场景描述 测试 resolveTeamMode 各分支。
func TestResolveTeamMode_场景描述(t *testing.T) {
	tests := []struct {
		name string
		spec atschema.TeamAgentSpec
		want string
	}{
		{
			name: "TeamMode已设置时直接返回",
			spec: atschema.TeamAgentSpec{
				TeamMode: "hybrid",
			},
			want: "hybrid",
		},
		{
			name: "TeamMode为自定义值时直接返回",
			spec: atschema.TeamAgentSpec{
				TeamMode: "custom_mode",
			},
			want: "custom_mode",
		},
		{
			name: "TeamMode为空且无非人类成员时返回default",
			spec: atschema.TeamAgentSpec{
				PredefinedMembers: []atschema.TeamMemberSpec{
					{RoleType: atschema.TeamRoleHumanAgent},
				},
			},
			want: "default",
		},
		{
			name: "TeamMode为空且无预定义成员时返回default",
			spec: atschema.TeamAgentSpec{
				PredefinedMembers: []atschema.TeamMemberSpec{},
			},
			want: "default",
		},
		{
			name: "TeamMode为空且有非人类成员时返回hybrid",
			spec: atschema.TeamAgentSpec{
				PredefinedMembers: []atschema.TeamMemberSpec{
					{RoleType: atschema.TeamRoleHumanAgent},
					{RoleType: atschema.TeamRoleTeammate},
				},
			},
			want: "hybrid",
		},
		{
			name: "TeamMode为空且第一个成员就是非人类时返回hybrid",
			spec: atschema.TeamAgentSpec{
				PredefinedMembers: []atschema.TeamMemberSpec{
					{RoleType: atschema.TeamRoleTeammate},
					{RoleType: atschema.TeamRoleHumanAgent},
				},
			},
			want: "hybrid",
		},
		{
			name: "TeamMode为空且成员为Leader时返回hybrid",
			spec: atschema.TeamAgentSpec{
				PredefinedMembers: []atschema.TeamMemberSpec{
					{RoleType: atschema.TeamRoleLeader},
				},
			},
			want: "hybrid",
		},
		{
			name: "TeamMode已设置时忽略成员类型",
			spec: atschema.TeamAgentSpec{
				TeamMode: "default",
				PredefinedMembers: []atschema.TeamMemberSpec{
					{RoleType: atschema.TeamRoleTeammate},
				},
			},
			want: "default",
		},
		{
			name: "空Spec返回default",
			spec: atschema.TeamAgentSpec{},
			want: "default",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveTeamMode(tt.spec)
			if got != tt.want {
				t.Errorf("resolveTeamMode() = %v, 期望 %v", got, tt.want)
			}
		})
	}
}
