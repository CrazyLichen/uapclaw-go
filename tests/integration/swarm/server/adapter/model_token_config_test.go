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

	_, err := testhelpers.CallWebRPC(s.T(), nil, "models.replace_all", map[string]any{
		"models": []any{map[string]any{
			"model_name":     "mock-model",
			"api_base":       "http://127.0.0.1:1",
			"api_key":        "mock-api-key",
			"model_provider": "OpenAI", // models.replace_all 只接受 availableModelProviders
			"context_window": 32000,
			"max_tokens":     8192,
			"origin_index":   0,
		}},
	})
	s.Require().NoError(err)

	// 落盘校验
	raw, readErr := os.ReadFile(filepath.Join(s.env.DataDir, "config", "config.yaml"))
	s.Require().NoError(readErr)
	text := string(raw)
	s.Require().Contains(text, "context_window: 32000", "config.yaml 应写入上下文窗口")
	s.Require().Contains(text, "max_tokens: 8192", "config.yaml 应写入输出上限（model_config_obj.max_tokens）")

	// 回填校验（帧经真实 WS 走 JSON，列表解出来是 []any / 数值是 float64）
	resp, listErr := testhelpers.CallWebRPC(s.T(), nil, "models.list", nil)
	s.Require().NoError(listErr)
	models, ok := resp["models"].([]any)
	s.Require().True(ok && len(models) == 1, "models.list 应返回 1 条，实际 %+v", resp["models"])
	entry, isMap := models[0].(map[string]any)
	s.Require().True(isMap, "模型条目应为对象")
	// 137c7d4f 后 models.list 回填的是扁平化后的 context_window / max_tokens
	s.Equal(float64(32000), entry["context_window"])
	s.Equal(float64(8192), entry["max_tokens"])
}

// TestContextWindow_FromModelEntry 用例 2：adapter 装配后窗口值进入 ContextEngineConfig，
// 并被 ContextUtils.resolve_context_max 命中（不再回落 200000）
func (s *ModelTokenConfigSuite) TestContextWindow_FromModelEntry() {
	s.writeEnvConfig(32000, 8192, true)

	d := s.newSessionScopedAdapterForTest()

	deepCfg := d.Instance().DeepConfig()
	s.Require().NotNil(deepCfg)
	s.Require().NotNil(deepCfg.ContextEngineConfig)
	ce := deepCfg.ContextEngineConfig

	// 设计 §4.5：恢复 Python ReActAgentConfig 默认值
	s.Equal(200, ce.MaxContextMessageNum)
	s.Equal(10, ce.DefaultWindowRoundNum)
	// 137c7d4f 起 model_config_obj.context_window 收集到 override（低优先于全局映射）
	s.Equal(32000, ce.ModelContextWindowTokensOverride)
	// 137c7d4f 起压缩阈值比例化：ContextEngineConfig 不再持有 trigger/target 比例，
	// 比例落在各压缩器配置上（见 TestCompressionThresholds_ShrinkByWindow）。

	// 137c7d4f 起 context_window 走 override：adapter 会把它合并进全局映射，
	// 因此 resolve_context_max 命中第 2 优先级（自定义映射）而不是 200000 兜底
	resolved := ce.ModelContextWindowTokens
	if ce.ModelContextWindowTokensOverride > 0 {
		resolved = map[string]int{ce.ModelName: ce.ModelContextWindowTokensOverride}
	}
	s.Equal(32000, cecontext.ResolveContextMax(ce.ModelName, ce.ContextWindowTokens, resolved))
}

// TestMaxOutputTokens_IntoModelConfig 用例 3：max_output_tokens 进入 ModelRequestConfig.MaxTokens
func (s *ModelTokenConfigSuite) TestMaxOutputTokens_IntoModelConfig() {
	s.writeEnvConfig(32000, 8192, true)

	d := s.newSessionScopedAdapterForTest()

	model, ok := d.ModelCache()["mock-model"]
	s.Require().True(ok, "模型缓存应包含 mock-model")
	s.Require().NotNil(model.ModelConfig)
	s.Require().NotNil(model.ModelConfig.MaxTokens, "MaxTokens 不应为 nil")
	s.Equal(8192, *model.ModelConfig.MaxTokens)
}

// TestCompressionThresholds_ShrinkByWindow 用例 4：压缩阈值按窗口比例收口（只下调不放大）
func (s *ModelTokenConfigSuite) TestCompressionThresholds_ShrinkByWindow() {
	s.writeEnvConfig(32000, 8192, true)

	d := s.newSessionScopedAdapterForTest()

	reactAgent := d.Instance().ReactAgent()
	s.Require().NotNil(reactAgent)
	reactCfg, ok := reactAgent.Config().(*saconfig.ReActAgentConfig)
	s.Require().True(ok, "应为 *ReActAgentConfig，实际 %T", reactAgent.Config())

	trigger, target := compressionThresholds(reactCfg.ContextProcessors, 32000)
	// 32K × 0.8 = 25600：比例化后生效阈值由"窗口 × TriggerContextRatio"在运行时算出
	s.Equal(25600, trigger, "RoundLevel 触发阈值应按窗口收口")
	s.Equal(160000, target, "RoundLevel 目标预算为固定值")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newSessionScopedAdapterForTest 造一个会话级 adapter 用于验证运行时装配结果。
//
// 10.3.28f：根 adapter 是纯 router，不建 DeepAgent 也不建模型缓存
// （对齐 Python _skip_own_instance_build :13109 —— 早退在 _create_model
// :13654 之前，根的 _model_cache 同样为空）。模型解析、ContextEngineConfig、
// 压缩阈值这些装配结果都长在各会话的 child 上，因此本套件在会话级 adapter
// 上验证，与真实运行路径一致（主循环读的正是 child 的这份）。
func (s *ModelTokenConfigSuite) newSessionScopedAdapterForTest() *adapter.DeepAdapter {
	s.T().Helper()

	d := adapter.NewDeepAdapter()
	// 标记为 session-scoped：create_instance 不会走根的早退分支
	d.MarkAsSessionScoped("model-token-cfg-session")
	s.Require().NoError(d.CreateInstance(s.Ctx, map[string]any{}, "agent.work.plan", "", nil))
	s.T().Cleanup(func() { _ = d.Cleanup() })
	return d
}

// writeEnvConfig 写入含 token 上限与压缩段的 config.yaml。
func (s *ModelTokenConfigSuite) writeEnvConfig(maxInput, maxOutput int, enabled bool) {
	s.T().Helper()
	// 137c7d4f 起：token 上限改由 model_config_obj.context_window / max_tokens 承载，
	// context_engine_config 不再有 compression_*_ratio（比例落在各压缩器上）。
	yaml := fmt.Sprintf(`models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: %s
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
      model_config_obj:
        context_window: %d
        max_tokens: %d
      is_default: true

modes:
  code:
    tools: []
    rails: []

react:
  agent_name: main_agent
  context_engine_config:
    enabled: %t
`, s.env.Provider, maxInput, maxOutput, enabled)

	if err := os.WriteFile(filepath.Join(s.env.DataDir, "config", "config.yaml"), []byte(yaml), 0o644); err != nil {
		s.T().Fatalf("写 config.yaml 失败: %v", err)
	}
}

// compressionThresholds 从处理器列表取出 RoundLevelCompressor 的触发/目标阈值。
//
// 137c7d4f 起触发阈值比例化：配置只留 TriggerContextRatio，
// 生效阈值 = 窗口 × 比例；目标仍是固定预算 TargetTotalTokens。
func compressionThresholds(specs []ceiface.ProcessorSpec, window int) (int, int) {
	for _, spec := range specs {
		if cfg, ok := spec.Config.(*compressor.RoundLevelCompressorConfig); ok {
			return int(float64(window) * cfg.TriggerContextRatio), cfg.TargetTotalTokens
		}
	}
	return 0, 0
}
