package agent_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// TestNewSpawnPayloadBuilder 测试构造函数
func TestNewSpawnPayloadBuilder(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, MemberName: "leader_1"}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)
	assert.NotNil(t, b)
}

// TestSpawnPayloadBuilder_BuildSpawnPayload 测试输出 map 键完整性
func TestSpawnPayloadBuilder_BuildSpawnPayload(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	teamSpec := &atschema.TeamSpec{
		TeamName:         "test_team",
		DisplayName:      "Test Team",
		LeaderMemberName: "leader_1",
	}
	ctx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "teammate_1",
		Persona:    "coder",
		TeamSpec:   teamSpec,
	}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	payload := b.BuildSpawnPayload(ctx, "Hello team")

	// 验证顶层键
	assert.Contains(t, payload, "coordination")
	assert.Contains(t, payload, "query")

	// 验证 query
	assert.Equal(t, "Hello team", payload["query"])

	// 验证 coordination 内容
	coord, ok := payload["coordination"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "test_team", coord["team_name"])
	assert.Equal(t, "Test Team", coord["display_name"])
	assert.Equal(t, "leader_1", coord["leader_member_name"])
	assert.Equal(t, "teammate_1", coord["member_name"])
	assert.Equal(t, "teammate", coord["role"])
	assert.Equal(t, "coder", coord["persona"])
	assert.Nil(t, coord["transport"])
}

// TestSpawnPayloadBuilder_BuildSpawnPayload_默认Query 测试空 initialMessage 时使用默认 query
func TestSpawnPayloadBuilder_BuildSpawnPayload_默认Query(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleTeammate, MemberName: "t1"}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	payload := b.BuildSpawnPayload(ctx, "")
	assert.Equal(t, "Join the team and wait for your first assignment.", payload["query"])
}

// TestSpawnPayloadBuilder_BuildSpawnPayload_NilTeamSpec 测试 TeamSpec 为 nil
func TestSpawnPayloadBuilder_BuildSpawnPayload_NilTeamSpec(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleTeammate, MemberName: "t1"}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	payload := b.BuildSpawnPayload(ctx, "hi")
	coord := payload["coordination"].(map[string]any)
	assert.Equal(t, "", coord["team_name"])
	assert.Equal(t, "", coord["display_name"])
	assert.Equal(t, "", coord["leader_member_name"])
}

// TestSpawnPayloadBuilder_BuildMemberContext 测试成员上下文构造
func TestSpawnPayloadBuilder_BuildMemberContext(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	teamSpec := &atschema.TeamSpec{TeamName: "test_team"}
	ctx := atschema.TeamRuntimeContext{
		Role:     atschema.TeamRoleLeader,
		TeamSpec: teamSpec,
		DBConfig: database.NewDatabaseConfig(),
	}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	memberSpec := atschema.TeamMemberSpec{
		MemberName: "teammate_1",
		RoleType:   atschema.TeamRoleTeammate,
		Persona:    "coder",
	}

	result := b.BuildMemberContext(memberSpec)
	assert.Equal(t, atschema.TeamRoleTeammate, result.Role)
	assert.Equal(t, "teammate_1", result.MemberName)
	assert.Equal(t, "coder", result.Persona)
	assert.Equal(t, teamSpec, result.TeamSpec)
	assert.Equal(t, database.NewDatabaseConfig(), result.DBConfig)
}

// TestSpawnPayloadBuilder_BuildMemberMessagerConfig_未实现 测试返回 nil（TODO 占位）
func TestSpawnPayloadBuilder_BuildMemberMessagerConfig_未实现(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)
	assert.Nil(t, b.BuildMemberMessagerConfig("t1"))
}

// TestSpawnPayloadBuilder_BuildSpawnConfig_未实现 测试返回 nil（TODO 占位）
func TestSpawnPayloadBuilder_BuildSpawnConfig_未实现(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)
	assert.Nil(t, b.BuildSpawnConfig(ctx))
}
