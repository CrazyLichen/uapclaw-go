package harness

import (
	"testing"

	"github.com/stretchr/testify/assert"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/modalityprobe"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// 本文件验证 DeepAgent 构建期原生图片输入解析。
// Python: deep_agent.py:1364-1400 _resolve_read_image_multimodal + :1461 调用点（23.7 Task 4）

// multimodalProbeModel 构造带 (api_base, model_name) 的最小模型
func multimodalProbeModel(apiBase, modelName string) *llm.Model {
	return &llm.Model{
		ModelConfig:  &llmschema.ModelRequestConfig{ModelName: modelName},
		ClientConfig: &llmschema.ModelClientConfig{APIBase: apiBase},
	}
}

// TestResolveReadImageMultimodal_CachedHit 缓存命中：记日志直接用，不再调度探测。
// Python: cached is not None → info + return
func TestResolveReadImageMultimodal_CachedHit(t *testing.T) {
	modalityprobe.ResetImageSupportCache()
	d := newTestDeepAgent()
	d.deepConfig = &hschema.DeepAgentConfig{
		Model: multimodalProbeModel("http://deepagent-hit", "m"),
	}
	modalityprobe.SetCachedImageSupport(modalityprobe.ProbeCacheKey{APIBase: "http://deepagent-hit", ModelName: "m"}, false)

	d.resolveReadImageMultimodal()

	assert.False(t, modalityprobe.IsProbeInFlight(d.deepConfig.Model)) // 未调度
	// 缓存结论不被改写
	v := modalityprobe.GetCachedImageSupport(d.deepConfig.Model)
	assert.NotNil(t, v)
	assert.False(t, *v)
}

// TestResolveReadImageMultimodal_BackgroundProbe 缓存未命中：后台调度探测（不阻塞构建）。
// Python: schedule_image_support_probe(config.model)
func TestResolveReadImageMultimodal_BackgroundProbe(t *testing.T) {
	modalityprobe.ResetImageSupportCache()
	d := newTestDeepAgent()
	d.deepConfig = &hschema.DeepAgentConfig{
		Model: multimodalProbeModel("http://deepagent-bg", "m"),
	}

	d.resolveReadImageMultimodal()

	// ScheduleImageSupportProbe 同步登记在途标记后再起 goroutine
	assert.True(t, modalityprobe.IsProbeInFlight(d.deepConfig.Model))
	// 探测不写显式配置：auto 语义保持，后续主模型变更仍跟随
	assert.Nil(t, d.deepConfig.EnableReadImageMultimodal)
}

// TestResolveReadImageMultimodal_ExplicitSkip 显式配置时直接返回（不探测）。
// Python: config.enable_read_image_multimodal is not None → return
func TestResolveReadImageMultimodal_ExplicitSkip(t *testing.T) {
	modalityprobe.ResetImageSupportCache()
	d := newTestDeepAgent()
	yes := true
	d.deepConfig = &hschema.DeepAgentConfig{
		EnableReadImageMultimodal: &yes,
		Model:                     multimodalProbeModel("http://deepagent-explicit", "m"),
	}

	d.resolveReadImageMultimodal()

	assert.False(t, modalityprobe.IsProbeInFlight(d.deepConfig.Model))
}

// TestResolveReadImageMultimodal_NoModel 无模型：debug 日志后返回。
// Python: config.model is None → debug + return
func TestResolveReadImageMultimodal_NoModel(t *testing.T) {
	modalityprobe.ResetImageSupportCache()
	d := newTestDeepAgent()
	d.deepConfig = &hschema.DeepAgentConfig{}

	d.resolveReadImageMultimodal()

	assert.Nil(t, d.deepConfig.Model)
}

// TestResolveReadImageMultimodal_NilConfig 无配置：直接返回。
// Python: config is None → return
func TestResolveReadImageMultimodal_NilConfig(t *testing.T) {
	d := newTestDeepAgent()
	d.deepConfig = nil

	d.resolveReadImageMultimodal() // 不 panic 即通过
}
