// Python 源：spawn_manager.py:46-54/_template_fields + :536-568 恢复段 +
// agent_configurator.py:1099-1107 resolve_agent_spec 的快照挂载分支（15.16e 收口批）
//
// 场景：
//   - expert 成员行（options.agent_template）→ BuildContextFromDB 读行 →
//     ctx.AgentTemplateSpec 非空
//   - ResolveAgentSpec：共享 base 叠快照（skills 并集保序去重）；自身条目
//     优先不叠快照
//   - 非 expert 成员恢复 → 快照/外部配置双 nil

package agent

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/messager"
	atschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/tools/database"
	hschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/harness/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/single_agent/schema"
)

// newExpertMemberFixture 经 RegisterTeammate（真实 spawn 链）建一个带快照的
// expert 成员行，返回可直接构造 SpawnManager 的 backend 与 configurator。
func newExpertMemberFixture(t *testing.T, snapshot *hschema.AgentTemplateSpec) (*tools.TeamBackend, *AgentConfigurator) {
	t.Helper()
	db := database.NewInMemoryTeamDatabase()
	backend := tools.NewTeamBackend("team-x", "leader", true, db,
		messager.NewInProcessMessager(atschema.NewMessagerTransportConfig()))
	outcome, err := backend.BuildTeam(context.Background(), "T", "desc", "Leader", "ld", nil, nil)
	require.NoError(t, err)
	require.NotNil(t, outcome)

	var templateRecord *database.MemberAgentTemplate
	if snapshot != nil {
		data, err := json.Marshal(snapshot)
		require.NoError(t, err)
		var snapMap map[string]any
		require.NoError(t, json.Unmarshal(data, &snapMap))
		templateRecord = &database.MemberAgentTemplate{
			Ref:      map[string]any{"name": "writer"},
			Version:  "1.0",
			Snapshot: snapMap,
		}
	}
	result := tools.RegisterTeammate(context.Background(), backend, tools.RegisterTeammateInput{
		MemberName:    "dev-writer",
		DisplayName:   "Dev Writer",
		Desc:          "expert member",
		Prompt:        "expert persona",
		AgentTemplate: templateRecord,
	})
	require.True(t, result.OK)

	configurator := NewAgentConfigurator(&agentschema.AgentCard{})
	configurator.SetTeamBackend(backend)
	configurator.blueprint = &TeamAgentBlueprint{Ctx: atschema.TeamRuntimeContext{MemberName: "leader"}}
	return backend, configurator
}

// TestBuildContextFromDB_expert快照读行 恢复段读行：options.agent_template → ctx。
func TestBuildContextFromDB_expert快照读行(t *testing.T) {
	snapshot := &hschema.AgentTemplateSpec{}
	snapshot.AgentCard.BaseCard.Name = "writer"
	_, configurator := newExpertMemberFixture(t, snapshot)

	manager := &SpawnManager{configurator: configurator}
	ctx, err := manager.BuildContextFromDB(context.Background(), "dev-writer")
	require.NoError(t, err)
	require.NotNil(t, ctx.AgentTemplateSpec)
	assert.Equal(t, "writer", ctx.AgentTemplateSpec.AgentCard.BaseCard.Name)
	assert.Nil(t, ctx.TemplateExternalCli)
}

// TestBuildContextFromDB_普通成员双nil 非 expert 成员恢复 → 快照/外部配置双 nil。
func TestBuildContextFromDB_普通成员双nil(t *testing.T) {
	_, configurator := newExpertMemberFixture(t, nil)
	manager := &SpawnManager{configurator: configurator}
	ctx, err := manager.BuildContextFromDB(context.Background(), "dev-writer")
	require.NoError(t, err)
	assert.Nil(t, ctx.AgentTemplateSpec)
	assert.Nil(t, ctx.TemplateExternalCli)
}

// TestTemplateFields_externalCLI 外部 CLI 模板记录反序列化。
func TestTemplateFields_externalCLI(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	backend := tools.NewTeamBackend("team-x", "leader", true, db,
		messager.NewInProcessMessager(atschema.NewMessagerTransportConfig()))
	result := tools.RegisterTeammate(context.Background(), backend, tools.RegisterTeammateInput{
		MemberName:  "dev-cli",
		DisplayName: "Dev CLI",
		Desc:        "cli expert",
		AgentTemplate: &database.MemberAgentTemplate{
			Ref: map[string]any{"name": "cli-expert"},
			ExternalCLI: map[string]any{
				"cli_agent":      "codex",
				"skill_conflict": "append",
			},
		},
	})
	require.True(t, result.OK)
	member, err := backend.GetMember(context.Background(), "dev-cli")
	require.NoError(t, err)

	snapshot, externalCLI := templateFields(member)
	assert.Nil(t, snapshot)
	require.NotNil(t, externalCLI)
	assert.Equal(t, "codex", externalCLI.CLIAgent)
	assert.Equal(t, "append", externalCLI.SkillConflict)
}

// TestResolveAgentSpec_快照挂载 共享 base 叠快照；自身条目优先不叠。
func TestResolveAgentSpec_快照挂载(t *testing.T) {
	baseSpec := atschema.DeepAgentSpec{}
	baseSpec.SystemPrompt = "base prompt"
	baseSpec.Skills = []string{"base-skill"}
	spec := atschema.TeamAgentSpec{
		Agents: map[string]atschema.DeepAgentSpec{"teammate": baseSpec},
	}
	snapshot := &hschema.AgentTemplateSpec{}
	snapshot.AgentCard.BaseCard.Name = "writer"
	snapshot.Skills = []hschema.SkillSpec{{Dir: "/s/py", Mode: "all"}}

	// 共享 base 叠快照：skills 并集保序去重（Python apply_template_snapshot :85-96）
	resolved := ResolveAgentSpec(spec, atschema.TeamRoleTeammate, "", snapshot)
	assert.Equal(t, "base prompt", resolved.SystemPrompt)
	require.Len(t, resolved.Skills, 2)
	assert.Equal(t, "base-skill", resolved.Skills[0])
	assert.Equal(t, "py", resolved.Skills[1])

	// 自身条目优先：不叠快照（Python :1099-1100）
	specWithSelf := atschema.TeamAgentSpec{
		Agents: map[string]atschema.DeepAgentSpec{
			"teammate":   baseSpec,
			"dev-writer": baseSpec,
		},
	}
	resolved = ResolveAgentSpec(specWithSelf, atschema.TeamRoleTeammate, "dev-writer", snapshot)
	assert.Len(t, resolved.Skills, 1) // 无快照叠加

	// 无快照时行为不变
	resolved = ResolveAgentSpec(spec, atschema.TeamRoleTeammate, "")
	assert.Len(t, resolved.Skills, 1)
}
