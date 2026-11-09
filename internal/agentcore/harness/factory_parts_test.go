package harness_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
)

// TestResolveDeepAgentParts_产出配置与rails 验证 resolve 只组装不物化。
func TestResolveDeepAgentParts_产出配置与rails(t *testing.T) {
	params := harness_config.CreateDeepAgentParams{
		Language:            "cn",
		EnableTaskLoop:      true,
		MaxIterations:       5,
		AutoCreateWorkspace: true,
	}

	parts, err := harness.ResolveDeepAgentParts(&params)

	require.NoError(t, err)
	require.NotNil(t, parts)
	require.NotNil(t, parts.Config, "config 必须产出")
	assert.Equal(t, "cn", parts.Config.Language)
	assert.Equal(t, 5, parts.Config.MaxIterations)
}

// TestResolveDeepAgentParts_nil报错 nil params 明确报错。
func TestResolveDeepAgentParts_nil报错(t *testing.T) {
	_, err := harness.ResolveDeepAgentParts(nil)
	require.Error(t, err)
}

// TestResolveDeepAgentParts_原params不被改写 resolve 不得修改入参（无副作用）。
func TestResolveDeepAgentParts_原params不被改写(t *testing.T) {
	params := harness_config.CreateDeepAgentParams{
		Language: "cn",
		Card:     nil,
	}

	_, err := harness.ResolveDeepAgentParts(&params)
	require.NoError(t, err)

	assert.Nil(t, params.Card, "resolve 不得回写入参的 Card")
}

// TestApplyDeepAgentParts_配置与工具落地 验证 apply 把 parts 落到 agent。
func TestApplyDeepAgentParts_配置与工具落地(t *testing.T) {
	params := harness_config.CreateDeepAgentParams{
		Language:       "cn",
		EnableTaskLoop: true,
	}
	parts, err := harness.ResolveDeepAgentParts(&params)
	require.NoError(t, err)

	agent, err := harness.ApplyDeepAgentParts(t.Context(), parts)

	require.NoError(t, err)
	require.NotNil(t, agent)
	require.NotNil(t, agent.DeepConfig())
	assert.Equal(t, "cn", agent.DeepConfig().Language)
}

// TestApplyDeepAgentParts_nil报错 nil parts 明确报错。
func TestApplyDeepAgentParts_nil报错(t *testing.T) {
	_, err := harness.ApplyDeepAgentParts(t.Context(), nil)
	require.Error(t, err)
}

// TestCreateDeepAgent_等价于resolve加apply 验证既有入口行为不变（重构不改语义）。
func TestCreateDeepAgent_等价于resolve加apply(t *testing.T) {
	params := harness_config.CreateDeepAgentParams{
		Language:       "cn",
		EnableTaskLoop: true,
	}

	direct, err := harness.CreateDeepAgent(t.Context(), params)
	require.NoError(t, err)

	parts, err := harness.ResolveDeepAgentParts(&params)
	require.NoError(t, err)
	viaParts, err := harness.ApplyDeepAgentParts(t.Context(), parts)
	require.NoError(t, err)

	// 关键配置字段一致（不比较指针）
	assert.Equal(t, direct.DeepConfig().Language, viaParts.DeepConfig().Language)
	assert.Equal(t, direct.DeepConfig().EnableTaskLoop, viaParts.DeepConfig().EnableTaskLoop)
	assert.Equal(t, direct.DeepConfig().MaxIterations, viaParts.DeepConfig().MaxIterations)
}
