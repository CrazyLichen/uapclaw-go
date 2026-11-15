package runtime

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/modalityprobe"
)

// 本文件为 image_modality_warmup.go 的单元测试。
// Python: tests/unit_tests/agentserver/test_image_modality_warmup.py（23.7 Task 5）

// probeEntry 构造一个 models.defaults 风格的模型条目
func probeEntry(apiBase, modelName string, supportsVision any) map[string]any {
	mcc := map[string]any{"model_name": modelName, "api_base": apiBase, "api_key": "test-key"}
	if supportsVision != nil {
		mcc["supports_vision"] = supportsVision
	}
	return map[string]any{"model_client_config": mcc, "model_config_obj": map[string]any{}}
}

// TestBuildProbeModels_DeclaredSupportsVision 声明条目跳探测直接预填缓存。
// Python: _build_probe_models:104-113
func TestBuildProbeModels_DeclaredSupportsVision(t *testing.T) {
	modalityprobe.ResetImageSupportCache()

	models := buildProbeModels([]map[string]any{
		probeEntry("http://declared", "m1", true),
	})

	assert.Empty(t, models)
	v, ok := modalityprobe.GetCachedImageSupportByKey("http://declared", "m1")
	require.True(t, ok)
	assert.True(t, v)
}

// TestBuildProbeModels_DedupeAndSkipBad 同 key 去重、坏条目跳过不阻断。
// Python: _build_probe_models:99-102 去重 + :92-98 坏条目跳过
func TestBuildProbeModels_DedupeAndSkipBad(t *testing.T) {
	modalityprobe.ResetImageSupportCache()

	bad := probeEntry("http://bad", "m", nil)
	bad["model_client_config"] = map[string]any{"api_base": "http://bad"} // 缺 model_name

	models := buildProbeModels([]map[string]any{
		bad,
		probeEntry("http://dedupe", "m2", nil),
		probeEntry("http://dedupe", "m2", nil), // 同 key 重复
	})

	assert.Len(t, models, 1)
}

// TestWarmImageModalityCache_ExplicitConfigSkip react.enable_read_image_multimodal
// 显式设置时跳过整轮预热。
// Python: warm_image_modality_cache:151-157
func TestWarmImageModalityCache_ExplicitConfigSkip(t *testing.T) {
	modalityprobe.ResetImageSupportCache()

	cfg := map[string]any{
		"react":  map[string]any{"enable_read_image_multimodal": false},
		"models": map[string]any{"defaults": []any{probeEntry("http://skip", "m", nil)}},
	}
	assert.True(t, warmImageModalityCache(context.Background(), cfg, "test"))
	// 未配置模型列表 → 不产生任何结论
	_, ok := modalityprobe.GetCachedImageSupportByKey("http://skip", "m")
	assert.False(t, ok)
}

// TestWarmImageModalityCache_ReportStatus 整轮报告语义：
// 空列表 → True（Python: :160-162 not models → True）；
// 探测结论未知 → False（Python: :193-194 verdict None → available=False）。
func TestWarmImageModalityCache_ReportStatus(t *testing.T) {
	modalityprobe.ResetImageSupportCache()

	// 空模型列表：无可探测对象，不算失败
	cfg := map[string]any{"models": map[string]any{"defaults": []any{}}}
	assert.True(t, warmImageModalityCache(context.Background(), cfg, "test"))

	// 探测返回未知（nil）→ available=false
	modalityprobe.ResetImageSupportCache()
	orig := probeImageSupportFunc
	probeImageSupportFunc = func(context.Context, *llm.Model) *bool { return nil }
	defer func() { probeImageSupportFunc = orig }()
	cfgFail := map[string]any{"models": map[string]any{"defaults": []any{
		probeEntry("http://unknown", "mu", nil), // api_key 必填由 BuildModelFromEntry 校验
	}}}
	assert.False(t, warmImageModalityCache(context.Background(), cfgFail, "test"))
}
