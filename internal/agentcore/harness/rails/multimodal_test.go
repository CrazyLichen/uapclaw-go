package rails

import (
	"testing"

	"github.com/stretchr/testify/assert"
	hllm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/modalityprobe"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
)

// 本文件为 rails/multimodal.go 的单元测试。
// Python: tests/unit_tests/harness/test_deep_agent.py:277/304 优先级矩阵 + 23.7 计划 Task 3

// probeKey 构造探测缓存键
func probeKey(apiBase, modelName string) modalityprobe.ProbeCacheKey {
	return modalityprobe.ProbeCacheKey{APIBase: apiBase, ModelName: modelName}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newModelWithVision 构造带 supports_vision 声明的 Model
func newModelWithVision(supported bool) *hllm.Model {
	return &hllm.Model{ClientConfig: &llmschema.ModelClientConfig{SupportsVision: &supported}}
}

// TestResolveImageMultimodal 优先级：显式 > 全局 > 声明 > 缓存
// Python: _resolve_image_multimodal:30-49
func TestResolveImageMultimodal(t *testing.T) {
	yes := newModelWithVision(true)
	no := newModelWithVision(false)
	trueVal, falseVal := true, false

	// 1. 显式覆盖优先于一切
	// Python: explicit_value is not None → return explicit_value
	assert.True(t, resolveImageMultimodal(&hschema.DeepAgentConfig{Model: no}, &trueVal))
	assert.False(t, resolveImageMultimodal(&hschema.DeepAgentConfig{Model: yes}, &falseVal))

	// 2. 声明优先于缓存
	// Python: vision = _model_supports_vision(model); if vision is not None: return vision
	assert.True(t, resolveImageMultimodal(&hschema.DeepAgentConfig{Model: yes}, nil))
	assert.False(t, resolveImageMultimodal(&hschema.DeepAgentConfig{Model: no}, nil))

	// 3. 无声明时走探测缓存
	// Python: return get_cached_image_support(model) is True
	cfg := &hschema.DeepAgentConfig{Model: &hllm.Model{
		ModelConfig:  &llmschema.ModelRequestConfig{ModelName: "m"},
		ClientConfig: &llmschema.ModelClientConfig{APIBase: "http://resolve"},
	}}
	modalityprobe.ResetImageSupportCache()
	assert.False(t, resolveImageMultimodal(cfg, nil)) // 缓存空 → false
	modalityprobe.SetCachedImageSupport(probeKey("http://resolve", "m"), true)
	assert.True(t, resolveImageMultimodal(cfg, nil))

	// 4. 全局配置 auto（nil）时无模型 → false
	// Python: model 为 None 时 _model_supports_vision 返回 None、缓存查 None → False
	assert.False(t, resolveImageMultimodal(&hschema.DeepAgentConfig{}, nil))

	// 5. 全局配置显式 false 时不看声明
	// Python: configured_value isinstance bool → return configured_value
	assert.False(t, resolveImageMultimodal(
		&hschema.DeepAgentConfig{EnableReadImageMultimodal: &falseVal, Model: yes}, nil))
}

// TestModelSupportsVision 声明读取边界（Python: _model_supports_vision:14-27）
func TestModelSupportsVision(t *testing.T) {
	assert.Nil(t, ModelSupportsVision(nil))
	assert.Nil(t, ModelSupportsVision(&hllm.Model{}))
	assert.Nil(t, ModelSupportsVision(&hllm.Model{ClientConfig: &llmschema.ModelClientConfig{}}))
	assert.NotNil(t, ModelSupportsVision(newModelWithVision(true)))
}

// TestBuildReadImageMultimodalResolver 闭包不持有 agent，动态反映缓存变化
// Python: build_read_image_multimodal_resolver:77-91
func TestBuildReadImageMultimodalResolver(t *testing.T) {
	modalityprobe.ResetImageSupportCache()
	cfg := &hschema.DeepAgentConfig{Model: &hllm.Model{
		ModelConfig:  &llmschema.ModelRequestConfig{ModelName: "m"},
		ClientConfig: &llmschema.ModelClientConfig{APIBase: "http://resolver"},
	}}
	fake := &fakeDeepAgentForHeartbeat{fakeBaseAgent: *newFakeBaseAgent(), deepConfig: cfg}
	resolve := BuildReadImageMultimodalResolver(fake, nil)
	assert.False(t, resolve()) // 未探测 → false
	modalityprobe.SetCachedImageSupport(probeKey("http://resolver", "m"), true)
	assert.True(t, resolve()) // 缓存落地后动态生效（browser 运行时路径）
}

// 编译时验证见 heartbeat_test.go 的 fakeDeepAgentForHeartbeat（复用其 DeepConfig 注入能力）
