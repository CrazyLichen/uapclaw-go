// 独立外部测试包（package agent_test）：从 spawn payload 恢复链需要
// 注册平台种子工厂（swarm/agents/swarm）——15.16e 收口批后 swarm 经
// external 反向依赖 agent 包，agent 包内测试文件 import swarm 会形成
// test-only 环；Go 外部测试包不在生产依赖链上，环由包边界化解。

package agent_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agent "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	swarm "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm"
)

// TestFromSpawnPayload_materializeBuildContext 接线对齐 Python
// from_spawn_payload 的 configure 前 materialize（team_agent.py:1467）。
func TestFromSpawnPayload_materializeBuildContext(t *testing.T) {
	swarm.RegisterSwarmProviders()
	spec := atschema.NewTeamAgentSpec()
	spec.TeamName = "team_seed"
	spec.Agents = map[string]atschema.DeepAgentSpec{"leader": {}}
	spec.BuildContextSeed = map[string]any{"project_dir": "/proj"}
	rctx := atschema.TeamRuntimeContext{
		Role:     atschema.TeamRoleLeader,
		TeamSpec: &atschema.TeamSpec{TeamName: "team_seed"},
	}
	ta, err := agent.FromSpawnPayload(context.Background(), map[string]any{
		"spec":    &spec,
		"context": rctx,
	})
	require.NoError(t, err)
	require.NotNil(t, ta)
	require.NotNil(t, spec.BuildContext, "materialize 应在 Configure 前重建活载体")
	assert.Equal(t, "/proj", spec.BuildContext.GetProjectDir())
}
