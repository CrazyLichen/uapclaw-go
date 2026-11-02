package context_engineer

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/compressor"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// railCfgAgent 在既有 railAgent 桩上覆写 Config()，用于验证 Init 的阈值收口接线。
type railCfgAgent struct {
	*railAgent
	cfg sainterfaces.AgentConfig
}

// Config 返回注入的 ReActAgentConfig。
func (a *railCfgAgent) Config() sainterfaces.AgentConfig { return a.cfg }

// ──────────────────────────── 导出函数 ────────────────────────────

// TestContextProcessorRail_Init按窗口收口阈值 验证 Init 用 ContextEngineConfig 的窗口与比例
// 对 preset 处理器做 min(绝对, ratio×window) 收口。
func TestContextProcessorRail_Init按窗口收口阈值(t *testing.T) {
	reactCfg := saconfig.NewReActAgentConfig()
	reactCfg.ModelNameVal = "mock-model"
	reactCfg.ContextEngineConfig = ceschema.ContextEngineConfig{
		MaxContextMessageNum:     200,
		DefaultWindowRoundNum:    10,
		ModelName:                "mock-model",
		ModelContextWindowTokens: map[string]int{"mock-model": 32000},
		CompressionTriggerRatio:  0.85,
		CompressionTargetRatio:   0.55,
	}

	agent := &railCfgAgent{
		railAgent: &railAgent{builder: saprompt.NewSystemPromptBuilder()},
		cfg:       reactCfg,
	}

	rail := NewContextProcessorRail()
	require.NoError(t, rail.Init(context.Background(), agent))

	// preset 已注入到 react config
	require.NotEmpty(t, reactCfg.ContextProcessors, "Init 应把处理器写入 ContextProcessors")

	var roundLevel *compressor.RoundLevelCompressorConfig
	for _, spec := range reactCfg.ContextProcessors {
		if cfg, ok := spec.Config.(*compressor.RoundLevelCompressorConfig); ok {
			roundLevel = cfg
		}
	}
	require.NotNil(t, roundLevel, "应包含 RoundLevelCompressor")
	assert.Equal(t, 27200, roundLevel.TriggerTotalTokens, "触发阈值应按 32000×0.85 收口")
	assert.Equal(t, 17600, roundLevel.TargetTotalTokens, "目标阈值应按 32000×0.55 收口")
}

// TestContextProcessorRail_Init无窗口时保持绝对值 验证未配置窗口映射时不收敛。
func TestContextProcessorRail_Init无窗口时保持绝对值(t *testing.T) {
	reactCfg := saconfig.NewReActAgentConfig()
	reactCfg.ModelNameVal = "unknown-model"
	reactCfg.ContextEngineConfig = ceschema.ContextEngineConfig{
		MaxContextMessageNum:    200,
		DefaultWindowRoundNum:   10,
		CompressionTriggerRatio: 0.85,
		CompressionTargetRatio:  0.55,
	}

	agent := &railCfgAgent{
		railAgent: &railAgent{builder: saprompt.NewSystemPromptBuilder()},
		cfg:       reactCfg,
	}

	rail := NewContextProcessorRail()
	require.NoError(t, rail.Init(context.Background(), agent))

	var roundLevel *compressor.RoundLevelCompressorConfig
	for _, spec := range reactCfg.ContextProcessors {
		if cfg, ok := spec.Config.(*compressor.RoundLevelCompressorConfig); ok {
			roundLevel = cfg
		}
	}
	require.NotNil(t, roundLevel)
	// 未知模型回落 200000 兜底窗口：0.85×200000 = 170000 < 230000 → 仍会收口
	assert.Equal(t, 170000, roundLevel.TriggerTotalTokens, "兜底窗口 200000 下触发阈值为 170000")
	assert.Equal(t, 110000, roundLevel.TargetTotalTokens, "兜底窗口 200000 下目标阈值为 110000")
}
