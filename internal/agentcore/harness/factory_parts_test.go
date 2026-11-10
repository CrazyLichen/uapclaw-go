package harness_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
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

// TestSpecResolveParts_默认rails在resolve阶段算好 spec 路径也必须含默认 rails。
//
// 上游 spec.resolve_parts 委托 resolve_deep_agent_parts，后者在同一次调用里
// 跑完 default_rails 循环（factory.py:394-415）。Go 侧通过本包 init
// 注入的计算器保证这一点；apply 阶段只做挂载。
//
// 回归守护：曾出现 apply 侧改成"不再补默认"后，spec 路径静默丢失
// 全部默认 rails（Security/AskUser/SysOp/...）的缺口。
func TestSpecResolveParts_默认rails在resolve阶段算好(t *testing.T) {
	spec := &hschema.DeepAgentSpec{Language: "cn"}

	parts, err := harness.ResolveDeepAgentSpecParts(spec, nil)

	require.NoError(t, err)
	require.NotNil(t, parts)
	assert.NotEmpty(t, parts.Rails, "spec 路径产出的 parts.Rails 必须含默认 rails")
}

// TestSpecResolveParts_不重复追加已声明的rail spec 声明过的 rail 类型不再补默认。
func TestSpecResolveParts_不重复追加已声明的rail(t *testing.T) {
	hschema.ResetRailRegistryForTest()
	t.Cleanup(hschema.ResetRailRegistryForTest)

	// 用一个真实 rail 类型注册 provider，验证去重键生效
	provided := rails.NewSysOperationRail()
	hschema.RegisterRailProvider("test.dup.rail", func(
		map[string]any, hschema.BuildContextInterface,
	) (any, error) {
		return provided, nil
	})
	spec := &hschema.DeepAgentSpec{
		Language: "cn",
		Rails:    []hschema.RailSpec{{Type: "test.dup.rail"}},
	}

	parts, err := harness.ResolveDeepAgentSpecParts(spec, nil)

	require.NoError(t, err)
	count := 0
	for _, r := range parts.Rails {
		if _, ok := r.(*rails.SysOperationRail); ok {
			count++
		}
	}
	assert.Equal(t, 1, count, "spec 已声明 SysOperationRail 时默认补齐不得再追加")
}
