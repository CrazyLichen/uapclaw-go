//go:build integration

package adapter_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor/compressor"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/channel_manager/web"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelTokenConfigSuite 验证 max_input_tokens / max_output_tokens 从 config.yaml
// 一路生效到运行时：web 写入 → adapter 装配 → ContextEngineConfig/ResolveContextMax
// → ModelRequestConfig.MaxTokens → ContextProcessorRail 阈值收口。
//
// 对应 Python：interface_deep.py `_deep_agent_context_engine_config` /
// `_build_model_from_entry` 与 `ContextUtils.resolve_context_max`。
type ModelTokenConfigSuite struct {
	isuite.BaseIntegrationSuite
	// env 每测试隔离的集成环境（数据目录 / MockLLM provider / env 备份）
	env *testhelpers.AdapterTestEnv
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestModelTokenConfigSuite(t *testing.T) {
	suite.Run(t, new(ModelTokenConfigSuite))
}

// SetupTest 每个测试方法前构建隔离环境（数据目录 / MockLLM / env 备份）。
func (s *ModelTokenConfigSuite) SetupTest() {
	s.env = testhelpers.NewAdapterTestEnv(s.T(), s.MockLLM, "mock_token_cfg")
}

// TestWriteModelTokens_RoundTrip 用例 1：models.replace_all → config.yaml → models.list
func (s *ModelTokenConfigSuite) TestWriteModelTokens_RoundTrip() {
	s.writeEnvConfig(0, 0, true)

	dispatcher := web.RegisterWebHandlers(nil)
	_, err := dispatcher.Dispatch("models.replace_all", map[string]any{
		"models": []any{map[string]any{
			"model_name":        "mock-model",
			"api_base":          "http://127.0.0.1:1",
			"api_key":           "mock-api-key",
			"model_provider":    "OpenAI", // models.replace_all 只接受 availableModelProviders
			"max_input_tokens":  32000,
			"max_output_tokens": 8192,
			"origin_index":      0,
		}},
	}, "")
	s.Require().NoError(err)

	// 落盘校验
	raw, readErr := os.ReadFile(filepath.Join(s.env.DataDir, "config", "config.yaml"))
	s.Require().NoError(readErr)
	text := string(raw)
	s.Require().Contains(text, "max_input_tokens: 32000", "config.yaml 应写入输入上限")
	s.Require().Contains(text, "max_output_tokens: 8192", "config.yaml 应写入输出上限")

	// 回填校验
	resp, listErr := dispatcher.Dispatch("models.list", nil, "")
	s.Require().NoError(listErr)
	models, ok := resp["models"].([]map[string]any)
	s.Require().True(ok && len(models) == 1, "models.list 应返回 1 条")
	s.Equal(32000, models[0]["max_input_tokens"])
	s.Equal(8192, models[0]["max_output_tokens"])
}

// TestContextWindow_FromModelEntry 用例 2：adapter 装配后窗口值进入 ContextEngineConfig，
// 并被 ContextUtils.resolve_context_max 命中（不再回落 200000）
func (s *ModelTokenConfigSuite) TestContextWindow_FromModelEntry() {
	s.writeEnvConfig(32000, 8192, true)

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	deepCfg := d.Instance().DeepConfig()
	s.Require().NotNil(deepCfg)
	s.Require().NotNil(deepCfg.ContextEngineConfig)
	ce := deepCfg.ContextEngineConfig

	// 设计 §4.5：恢复 Python ReActAgentConfig 默认值
	s.Equal(200, ce.MaxContextMessageNum)
	s.Equal(10, ce.DefaultWindowRoundNum)
	// 设计 D4/D14
	s.Equal(32000, ce.ModelContextWindowTokens["mock-model"])
	s.InDelta(0.85, ce.CompressionTriggerRatio, 1e-9)
	s.InDelta(0.55, ce.CompressionTargetRatio, 1e-9)

	// resolve_context_max 命中自定义映射（第 2 优先级），不是 200000 兜底
	s.Equal(32000, cecontext.ResolveContextMax(
		ce.ModelName, ce.ContextWindowTokens, ce.ModelContextWindowTokens))
}

// TestMaxOutputTokens_IntoModelConfig 用例 3：max_output_tokens 进入 ModelRequestConfig.MaxTokens
func (s *ModelTokenConfigSuite) TestMaxOutputTokens_IntoModelConfig() {
	s.writeEnvConfig(32000, 8192, true)

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	model, ok := d.ModelCache()["mock-model"]
	s.Require().True(ok, "模型缓存应包含 mock-model")
	s.Require().NotNil(model.ModelConfig)
	s.Require().NotNil(model.ModelConfig.MaxTokens, "MaxTokens 不应为 nil")
	s.Equal(8192, *model.ModelConfig.MaxTokens)
}

// TestCompressionThresholds_ShrinkByWindow 用例 4：压缩阈值按窗口比例收口（只下调不放大）
func (s *ModelTokenConfigSuite) TestCompressionThresholds_ShrinkByWindow() {
	s.writeEnvConfig(32000, 8192, true)

	d := adapter.NewDeepAdapter()
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.plan", ""))
	s.T().Cleanup(func() { _ = d.Cleanup() })

	reactAgent := d.Instance().ReactAgent()
	s.Require().NotNil(reactAgent)
	reactCfg, ok := reactAgent.Config().(*saconfig.ReActAgentConfig)
	s.Require().True(ok, "应为 *ReActAgentConfig，实际 %T", reactAgent.Config())

	trigger, target := compressionThresholds(reactCfg.ContextProcessors)
	// 32K × 0.85 = 27200，0.85 比例值小于绝对阈值 230000 → 下调
	s.Equal(27200, trigger, "RoundLevel 触发阈值应按窗口收口")
	s.Equal(17600, target, "RoundLevel 目标阈值应按窗口收口")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeEnvConfig 写入含 token 上限与压缩段的 config.yaml。
func (s *ModelTokenConfigSuite) writeEnvConfig(maxInput, maxOutput int, enabled bool) {
	s.T().Helper()
	yaml := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        max_input_tokens: %d
        max_output_tokens: %d
      is_default: true

modes:
  code:
    tools: []
    rails: []

react:
  agent_name: main_agent
  context_engine_config:
    enabled: %t
    compression_trigger_ratio: 0.85
    compression_target_ratio: 0.55
    round_level_compressor_config:
      trigger_total_tokens: 230000
      target_total_tokens: 160000
`, s.env.Provider, maxInput, maxOutput, enabled)

	if err := os.WriteFile(filepath.Join(s.env.DataDir, "config", "config.yaml"), []byte(yaml), 0o644); err != nil {
		s.T().Fatalf("写 config.yaml 失败: %v", err)
	}
}

// compressionThresholds 从处理器列表取出 RoundLevelCompressor 的触发/目标阈值。
func compressionThresholds(specs []ceiface.ProcessorSpec) (int, int) {
	for _, spec := range specs {
		if cfg, ok := spec.Config.(*compressor.RoundLevelCompressorConfig); ok {
			return cfg.TriggerTotalTokens, cfg.TargetTotalTokens
		}
	}
	return 0, 0
}
