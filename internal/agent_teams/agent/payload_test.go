package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	runnerspawn "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
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

// TestSpawnPayloadBuilder_BuildSpawnConfig 测试返回 SpawnAgentConfig
func TestSpawnPayloadBuilder_BuildSpawnConfig(t *testing.T) {
	spec := atschema.NewTeamAgentSpec()
	ctx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader}
	b := agent.NewSpawnPayloadBuilder(spec, ctx)

	cfg := b.BuildSpawnConfig(ctx)
	assert.Equal(t, runnerspawn.SpawnAgentKindTeamAgent, cfg.AgentKind)
	assert.NotNil(t, cfg.Payload, "Payload 应非 nil")
	// S-26: payload 形状是 FromSpawnPayload 公共契约——必须含 spec/context 键
	//（Python build_spawn_config: payload={"spec": model_dump, "context": model_dump}）
	assert.Contains(t, cfg.Payload, "spec", "payload 必须含 spec 键")
	assert.Contains(t, cfg.Payload, "context", "payload 必须含 context 键")
	specMap, ok := cfg.Payload["spec"].(map[string]any)
	assert.True(t, ok, "spec 应为 map[string]any（model_dump 等价）")
	if ok {
		assert.Contains(t, specMap, "team_name", "spec dump 应含 team_name")
	}
	ctxMap, ok := cfg.Payload["context"].(map[string]any)
	assert.True(t, ok, "context 应为 map[string]any（model_dump 等价）")
	if ok {
		assert.Contains(t, ctxMap, "role", "context dump 应含 role")
	}
	// payload 不应混入 coordination/query（那是 BuildSpawnPayload 的 inputs 形状）
	assert.NotContains(t, cfg.Payload, "coordination", "payload 不应含 coordination（属于 inputs）")
	assert.NotContains(t, cfg.Payload, "query", "payload 不应含 query（属于 inputs）")
}

// TestSpawnPayloadBuilder_BuildSpawnConfig_消费链端到端 测试 BuildSpawnConfig 产出
// 可被 FromSpawnPayload 消费（子进程路径闭环）。
// S-26/S-27: spawn payload wire 契约——BuildSpawnConfig → FromSpawnPayload 往返。
func TestSpawnPayloadBuilder_BuildSpawnConfig_消费链端到端(t *testing.T) {
	memberSpec := atschema.TeamMemberSpec{
		MemberName: "teammate_1",
		RoleType:   atschema.TeamRoleTeammate,
		Persona:    "coder",
	}
	spec := atschema.NewTeamAgentSpec()
	spec.TeamName = "team_e2e"
	spec.Agents = map[string]atschema.DeepAgentSpec{
		"leader": {},
	}
	leaderCtx := atschema.TeamRuntimeContext{Role: atschema.TeamRoleLeader, TeamSpec: &atschema.TeamSpec{TeamName: "team_e2e"}}
	b := agent.NewSpawnPayloadBuilder(spec, leaderCtx)

	memberCtx := b.BuildMemberContext(memberSpec)
	cfg := b.BuildSpawnConfig(memberCtx)

	// FromSpawnPayload 消费（agentConfig.Payload 直传）
	ta, err := agent.FromSpawnPayload(context.Background(), cfg.Payload)
	assert.NoError(t, err)
	assert.NotNil(t, ta)
}
