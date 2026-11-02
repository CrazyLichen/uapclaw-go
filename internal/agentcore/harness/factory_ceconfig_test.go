package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCreateDeepAgent_透传ContextEngineConfig 验证 CreateDeepAgent 把
// CreateDeepAgentParams.ContextEngineConfig 落到 DeepAgentConfig。
//
// Python: factory.py L305-310「Forward extra kwargs to config fields」——
// create_deep_agent(context_engine_config=...) 通过 kwargs 转发到 DeepAgentConfig。
func TestCreateDeepAgent_透传ContextEngineConfig(t *testing.T) {
	ce := ceschema.NewContextEngineConfig()
	ce.MaxContextMessageNum = 200
	ce.DefaultWindowRoundNum = 10
	ce.ModelName = "mock-model"
	ce.ModelContextWindowTokens["mock-model"] = 32000
	ce.CompressionTriggerRatio = 0.85
	ce.CompressionTargetRatio = 0.55

	agent, err := CreateDeepAgent(context.Background(), hconfig.CreateDeepAgentParams{
		ContextEngineConfig: &ce,
		Language:            "cn",
	})
	require.NoError(t, err)
	require.NotNil(t, agent)

	deepCfg := agent.DeepConfig()
	require.NotNil(t, deepCfg)
	require.NotNil(t, deepCfg.ContextEngineConfig, "ContextEngineConfig 不应被丢弃")
	assert.Equal(t, 32000, deepCfg.ContextEngineConfig.ModelContextWindowTokens["mock-model"])
	assert.Equal(t, "mock-model", deepCfg.ContextEngineConfig.ModelName)
	assert.InDelta(t, 0.85, deepCfg.ContextEngineConfig.CompressionTriggerRatio, 1e-9)
	assert.InDelta(t, 0.55, deepCfg.ContextEngineConfig.CompressionTargetRatio, 1e-9)
}
