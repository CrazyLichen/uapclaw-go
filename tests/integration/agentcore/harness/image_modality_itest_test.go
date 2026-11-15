//go:build integration

package harness_test

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/suite"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/modalityprobe"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	serverruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)

// ImageModalitySuite 图像模态探测跨子系统集成套件：
// swarm/server/runtime（预热/重探）+ harness/modalityprobe（探测器与缓存）+
// harness（真实 DeepAgent）+ harness/rails（解析链消费）。
// Python: test_image_modality_warmup.py / test_deep_agent.py 的集成面（23.7 Task 7）
type ImageModalitySuite struct {
	suite.Suite
}

// TestImageModalitySuite 集成入口
func TestImageModalitySuite(t *testing.T) {
	suite.Run(t, new(ImageModalitySuite))
}

// probeEntry 构造 models.defaults 风格条目
func (s *ImageModalitySuite) probeEntry(apiBase, modelName string, supportsVision any) map[string]any {
	mcc := map[string]any{"model_name": modelName, "api_base": apiBase, "api_key": "itest-key"}
	if supportsVision != nil {
		mcc["supports_vision"] = supportsVision
	}
	return map[string]any{
		"model_client_config": mcc,
		"model_config_obj":    map[string]any{},
	}
}

// SetupTest 每用例独立缓存，防止跨用例污染
func (s *ImageModalitySuite) SetupTest() {
	modalityprobe.ResetImageSupportCache()
}

// newRealDeepAgent 构造真实 DeepAgent 并注入 auto 模式 + 指定模型
func (s *ImageModalitySuite) newRealDeepAgent(apiBase, modelName string) *harness.DeepAgent {
	card := agentschema.NewAgentCard(
		agentschema.WithAgentName("itest-image-modality"),
		agentschema.WithAgentDescription("23.7 集成测试"),
	)
	d := harness.NewDeepAgent(card)
	model := &llm.Model{
		ModelConfig:  &llmschema.ModelRequestConfig{ModelName: modelName},
		ClientConfig: &llmschema.ModelClientConfig{APIBase: apiBase},
	}
	cfg := &hschema.DeepAgentConfig{Model: model} // EnableReadImageMultimodal nil = auto
	s.Require().NoError(d.ConfigureDeepConfig(context.Background(), cfg))
	return d
}

// TestWarmupThenDeepAgentConsumesCache 预热落缓存 → 真实 DeepAgent 解析链消费结论。
// 对齐 Python「第一个会话降级、后续会话享用结论」的跨会话传递。
func (s *ImageModalitySuite) TestWarmupThenDeepAgentConsumesCache() {
	var probeCalls int32
	restore := serverruntime.SetProbeImageSupportFuncForTest(func(_ context.Context, m *llm.Model) *bool {
		atomic.AddInt32(&probeCalls, 1)
		yes := true
		// 对齐真实 ProbeImageSupport：结论性结果落缓存（Python: probe_image_support:436-438）
		modalityprobe.SetCachedImageSupport(*modalityprobe.ProbeCacheKeyOf(m), true)
		return &yes
	})
	defer restore()

	// ① 服务启动预热：探测落缓存
	cfg := map[string]any{
		"models": map[string]any{"defaults": []any{s.probeEntry("http://itest-warm", "vision-m", nil)}},
	}

	// ② 真实 DeepAgent：预热前构建（缓存空）→ auto 下解析为 false（降级，
	// 对齐 Python「探测后台跑、元数据模式先行」）
	d := s.newRealDeepAgent("http://itest-warm", "vision-m")
	s.Require().False(rails.ShouldEnableReadImageMultimodal(d, nil), "缓存空 → 元数据模式")

	// ③ 服务预热落缓存 → 解析链动态消费 → true（长生命周期工具跟随结论）
	available := serverruntime.WarmImageModalityCacheStatus(cfg, "startup")
	s.Require().True(available)
	s.Require().True(rails.ShouldEnableReadImageMultimodal(d, nil), "缓存命中 → 原生图开启")
	s.Require().Equal(int32(1), atomic.LoadInt32(&probeCalls), "解析链不重复探测")
}

// TestReloadResetsCacheAndDeclaredSkips reload 重探清缓存；supports_vision 声明免探测。
func (s *ImageModalitySuite) TestReloadResetsCacheAndDeclaredSkips() {
	var probeCalls int32
	restore := serverruntime.SetProbeImageSupportFuncForTest(func(context.Context, *llm.Model) *bool {
		atomic.AddInt32(&probeCalls, 1)
		yes := true
		return &yes
	})
	defer restore()

	// ① 声明条目：预热直接预填缓存，探测零次
	cfg := map[string]any{
		"models": map[string]any{"defaults": []any{s.probeEntry("http://itest-decl", "declared", false)}},
	}
	s.Require().True(serverruntime.WarmImageModalityCacheStatus(cfg, "startup"))
	s.Require().Equal(int32(0), atomic.LoadInt32(&probeCalls))

	// ② DeepAgent：显式声明优先于缓存 → false（声明 false 关闭原生图）
	d := s.newRealDeepAgent("http://itest-decl", "declared")
	s.Require().False(rails.ShouldEnableReadImageMultimodal(d, nil))

	// ③ 模型配置变更重探：清缓存；声明条目重新预填，仍零探测
	serverruntime.RefreshImageModalityCache(cfg, "model config change")
	s.Require().Equal(int32(0), atomic.LoadInt32(&probeCalls))
	v, ok := modalityprobe.GetCachedImageSupportByKey("http://itest-decl", "declared")
	s.Require().True(ok)
	s.Require().False(v)
}

