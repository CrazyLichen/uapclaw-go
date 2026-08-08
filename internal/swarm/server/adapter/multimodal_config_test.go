package adapter

import (
	"os"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestApplyVideoModelConfigFromYAML_YAML配置优先 测试 YAML 配置优先级
func TestApplyVideoModelConfigFromYAML_YAML配置优先(t *testing.T) {
	// 清理环境变量
	videoEnvVars := []string{
		"VIDEO_API_KEY", "VIDEO_API_BASE", "VIDEO_MODEL_NAME", "VIDEO_PROVIDER",
		"VIDEO_UNDERSTANDING_STRICT", "API_KEY", "API_BASE", "MODEL_NAME", "MODEL_PROVIDER",
	}
	for _, v := range videoEnvVars {
		_ = os.Unsetenv(v)
	}

	configBase := map[string]any{
		"models": map[string]any{
			"video": map[string]any{
				"model_config": map[string]any{
					"api_key":    "yaml-video-key",
					"api_base":   "https://yaml-video.example.com/v1",
					"model_name": "yaml-video-model",
				},
			},
		},
	}

	ApplyVideoModelConfigFromYAML(configBase)
	defer func() {
		for _, v := range videoEnvVars {
			_ = os.Unsetenv(v)
		}
	}()

	if os.Getenv("VIDEO_API_KEY") != "yaml-video-key" {
		t.Errorf("VIDEO_API_KEY = %v，期望 yaml-video-key", os.Getenv("VIDEO_API_KEY"))
	}
	if os.Getenv("VIDEO_API_BASE") != "https://yaml-video.example.com/v1" {
		t.Errorf("VIDEO_API_BASE = %v，期望 https://yaml-video.example.com/v1", os.Getenv("VIDEO_API_BASE"))
	}
	if os.Getenv("VIDEO_MODEL_NAME") != "yaml-video-model" {
		t.Errorf("VIDEO_MODEL_NAME = %v，期望 yaml-video-model", os.Getenv("VIDEO_MODEL_NAME"))
	}
}

// TestApplyVideoModelConfigFromYAML_Embed回退 测试 embed 配置回退
func TestApplyVideoModelConfigFromYAML_Embed回退(t *testing.T) {
	// 清理环境变量
	videoEnvVars := []string{
		"VIDEO_API_KEY", "VIDEO_API_BASE", "VIDEO_MODEL_NAME", "VIDEO_PROVIDER",
		"VIDEO_UNDERSTANDING_STRICT", "API_KEY", "API_BASE", "MODEL_NAME", "MODEL_PROVIDER",
	}
	for _, v := range videoEnvVars {
		_ = os.Unsetenv(v)
	}

	configBase := map[string]any{
		"embed": map[string]any{
			"embed_api_key":  "embed-key",
			"embed_api_base": "https://embed.example.com/v1",
			"video_model":    "embed-video-model",
		},
	}

	ApplyVideoModelConfigFromYAML(configBase)
	defer func() {
		for _, v := range videoEnvVars {
			_ = os.Unsetenv(v)
		}
	}()

	if os.Getenv("VIDEO_API_KEY") != "embed-key" {
		t.Errorf("VIDEO_API_KEY = %v，期望 embed-key", os.Getenv("VIDEO_API_KEY"))
	}
	if os.Getenv("VIDEO_API_BASE") != "https://embed.example.com/v1" {
		t.Errorf("VIDEO_API_BASE = %v，期望 https://embed.example.com/v1", os.Getenv("VIDEO_API_BASE"))
	}
	if os.Getenv("VIDEO_MODEL_NAME") != "embed-video-model" {
		t.Errorf("VIDEO_MODEL_NAME = %v，期望 embed-video-model", os.Getenv("VIDEO_MODEL_NAME"))
	}
}

// TestApplyVideoModelConfigFromYAML_Strict模式 测试 strict 模式
func TestApplyVideoModelConfigFromYAML_Strict模式(t *testing.T) {
	// 清理环境变量
	videoEnvVars := []string{
		"VIDEO_API_KEY", "VIDEO_API_BASE", "VIDEO_MODEL_NAME", "VIDEO_PROVIDER",
		"VIDEO_UNDERSTANDING_STRICT", "API_KEY", "API_BASE", "MODEL_NAME", "MODEL_PROVIDER",
	}
	for _, v := range videoEnvVars {
		_ = os.Unsetenv(v)
	}

	configBase := map[string]any{
		"models": map[string]any{
			"video": map[string]any{
				"model_config": map[string]any{
					"api_key": "strict-key",
					"strict":  true,
				},
			},
		},
		"embed": map[string]any{
			"embed_api_key": "should-not-be-used",
		},
	}

	ApplyVideoModelConfigFromYAML(configBase)
	defer func() {
		for _, v := range videoEnvVars {
			_ = os.Unsetenv(v)
		}
	}()

	if os.Getenv("VIDEO_API_KEY") != "strict-key" {
		t.Errorf("VIDEO_API_KEY = %v，期望 strict-key", os.Getenv("VIDEO_API_KEY"))
	}
	if os.Getenv("VIDEO_UNDERSTANDING_STRICT") != "1" {
		t.Error("VIDEO_UNDERSTANDING_STRICT 应为 1")
	}
	// strict 模式下不应回退到 embed
	if os.Getenv("VIDEO_API_BASE") != "" {
		t.Error("strict 模式下 VIDEO_API_BASE 应为空（不回退 embed）")
	}
}

// TestApplyVideoModelConfigFromYAML_NilConfigBase 测试 nil configBase
func TestApplyVideoModelConfigFromYAML_NilConfigBase(t *testing.T) {
	_ = os.Setenv("VIDEO_UNDERSTANDING_STRICT", "1")
	defer func() { _ = os.Unsetenv("VIDEO_UNDERSTANDING_STRICT") }()

	ApplyVideoModelConfigFromYAML(nil)

	if os.Getenv("VIDEO_UNDERSTANDING_STRICT") != "" {
		t.Error("nil configBase 时应清除 VIDEO_UNDERSTANDING_STRICT")
	}
}

// TestApplyVideoModelConfigFromYAML_环境变量回退 测试环境变量回退
func TestApplyVideoModelConfigFromYAML_环境变量回退(t *testing.T) {
	// 清理环境变量
	videoEnvVars := []string{
		"VIDEO_API_KEY", "VIDEO_API_BASE", "VIDEO_MODEL_NAME", "VIDEO_PROVIDER",
		"VIDEO_UNDERSTANDING_STRICT",
	}
	for _, v := range videoEnvVars {
		_ = os.Unsetenv(v)
	}
	_ = os.Setenv("API_KEY", "env-api-key")
	defer func() { _ = os.Unsetenv("API_KEY") }()
	_ = os.Setenv("API_BASE", "https://env-api.example.com/v1")
	defer func() { _ = os.Unsetenv("API_BASE") }()
	_ = os.Setenv("MODEL_NAME", "env-video-model")
	defer func() { _ = os.Unsetenv("MODEL_NAME") }()
	_ = os.Setenv("MODEL_PROVIDER", "env-provider")
	defer func() { _ = os.Unsetenv("MODEL_PROVIDER") }()

	// 空 models 配置，回退到环境变量
	configBase := map[string]any{
		"models": map[string]any{},
	}

	ApplyVideoModelConfigFromYAML(configBase)
	defer func() {
		for _, v := range videoEnvVars {
			_ = os.Unsetenv(v)
		}
	}()

	if os.Getenv("VIDEO_API_KEY") != "env-api-key" {
		t.Errorf("VIDEO_API_KEY = %v，期望 env-api-key", os.Getenv("VIDEO_API_KEY"))
	}
	if os.Getenv("VIDEO_API_BASE") != "https://env-api.example.com/v1" {
		t.Errorf("VIDEO_API_BASE = %v", os.Getenv("VIDEO_API_BASE"))
	}
	if os.Getenv("VIDEO_MODEL_NAME") != "env-video-model" {
		t.Errorf("VIDEO_MODEL_NAME = %v，期望 env-video-model", os.Getenv("VIDEO_MODEL_NAME"))
	}
	if os.Getenv("VIDEO_PROVIDER") != "env-provider" {
		t.Errorf("VIDEO_PROVIDER = %v，期望 env-provider", os.Getenv("VIDEO_PROVIDER"))
	}
}

// TestApplyVideoModelConfigFromYAML_model_client_config 测试 model_client_config 兼容
func TestApplyVideoModelConfigFromYAML_model_client_config(t *testing.T) {
	// 清理环境变量
	videoEnvVars := []string{
		"VIDEO_API_KEY", "VIDEO_API_BASE", "VIDEO_MODEL_NAME", "VIDEO_PROVIDER",
		"VIDEO_UNDERSTANDING_STRICT", "API_KEY", "API_BASE", "MODEL_NAME", "MODEL_PROVIDER",
	}
	for _, v := range videoEnvVars {
		_ = os.Unsetenv(v)
	}

	// model_client_config 而非 model_config
	configBase := map[string]any{
		"models": map[string]any{
			"video": map[string]any{
				"model_client_config": map[string]any{
					"api_key":    "mcc-key",
					"api_base":   "https://mcc.example.com/v1",
					"model_name": "mcc-model",
				},
			},
		},
	}

	ApplyVideoModelConfigFromYAML(configBase)
	defer func() {
		for _, v := range videoEnvVars {
			_ = os.Unsetenv(v)
		}
	}()

	if os.Getenv("VIDEO_API_KEY") != "mcc-key" {
		t.Errorf("VIDEO_API_KEY = %v，期望 mcc-key", os.Getenv("VIDEO_API_KEY"))
	}
	if os.Getenv("VIDEO_API_BASE") != "https://mcc.example.com/v1" {
		t.Errorf("VIDEO_API_BASE = %v", os.Getenv("VIDEO_API_BASE"))
	}
	if os.Getenv("VIDEO_MODEL_NAME") != "mcc-model" {
		t.Errorf("VIDEO_MODEL_NAME = %v，期望 mcc-model", os.Getenv("VIDEO_MODEL_NAME"))
	}
}

// TestDedicatedMultimodalModelConfigured_有独立APIKey 测试有独立 api_key
func TestDedicatedMultimodalModelConfigured_有独立APIKey(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"video": map[string]any{
				"model_config": map[string]any{
					"api_key": "my-video-key",
				},
			},
		},
	}
	if !DedicatedMultimodalModelConfigured(configBase, "video") {
		t.Error("应有独立 api_key")
	}
}

// TestDedicatedMultimodalModelConfigured_无独立APIKey 测试无独立 api_key
func TestDedicatedMultimodalModelConfigured_无独立APIKey(t *testing.T) {
	configBase := map[string]any{
		"models": map[string]any{
			"video": map[string]any{
				"model_config": map[string]any{},
			},
		},
	}
	if DedicatedMultimodalModelConfigured(configBase, "video") {
		t.Error("不应有独立 api_key")
	}
}

// TestDedicatedMultimodalModelConfigured_非法类型 测试非法 modelType
func TestDedicatedMultimodalModelConfigured_非法类型(t *testing.T) {
	if DedicatedMultimodalModelConfigured(nil, "unknown") {
		t.Error("非法 modelType 应返回 false")
	}
}

// TestDedicatedMultimodalModelConfigured_NilConfigBase 测试 nil configBase
func TestDedicatedMultimodalModelConfigured_NilConfigBase(t *testing.T) {
	if DedicatedMultimodalModelConfigured(nil, "video") {
		t.Error("nil configBase 时应返回 false")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestStrVal 测试 strVal 辅助函数
func TestStrVal(t *testing.T) {
	if strVal(nil) != "" {
		t.Error("nil 应返回空字符串")
	}
	if strVal("hello") != "hello" {
		t.Errorf("字符串应原样返回，实际 %q", strVal("hello"))
	}
	if strVal("  spaces  ") != "spaces" {
		t.Errorf("应 trim 空格，实际 %q", strVal("  spaces  "))
	}
	if strVal(123) != "123" {
		t.Errorf("整数应转为字符串，实际 %q", strVal(123))
	}
	if strVal(true) != "true" {
		t.Errorf("布尔应转为字符串，实际 %q", strVal(true))
	}
}

// TestStrOr 测试 strOr 辅助函数
func TestStrOr(t *testing.T) {
	if strOr("first", "second") != "first" {
		t.Error("v1 非空时应返回 v1")
	}
	if strOr("", "second") != "second" {
		t.Error("v1 空时应返回 v2")
	}
	if strOr(nil, "fallback") != "fallback" {
		t.Error("v1 nil 时应返回 v2")
	}
}

// TestParseBool 测试 parseBool 辅助函数
func TestParseBool(t *testing.T) {
	if parseBool(nil, false) != false {
		t.Error("nil 应返回默认值 false")
	}
	if parseBool(nil, true) != true {
		t.Error("nil 应返回默认值 true")
	}
	if !parseBool(true, false) {
		t.Error("bool true 应返回 true")
	}
	if parseBool(false, true) {
		t.Error("bool false 应返回 false")
	}
	if !parseBool("1", false) {
		t.Error("字符串 '1' 应返回 true")
	}
	if !parseBool("true", false) {
		t.Error("字符串 'true' 应返回 true")
	}
	if !parseBool("yes", false) {
		t.Error("字符串 'yes' 应返回 true")
	}
	if !parseBool("on", false) {
		t.Error("字符串 'on' 应返回 true")
	}
	if parseBool("0", false) {
		t.Error("字符串 '0' 应返回 false")
	}
	if parseBool("false", false) {
		t.Error("字符串 'false' 应返回 false")
	}
}

// TestGetModelConfig 测试 getModelConfig 辅助函数
func TestGetModelConfig(t *testing.T) {
	// nil 配置基础
	if getModelConfig(nil, "video") != nil {
		t.Error("nil configBase 应返回 nil")
	}

	// dict 格式 models
	configBase := map[string]any{
		"models": map[string]any{
			"video": map[string]any{
				"model_config": map[string]any{
					"api_key": "test",
				},
			},
		},
	}
	mc := getModelConfig(configBase, "video")
	if mc == nil || strVal(mc["api_key"]) != "test" {
		t.Error("应正确解析 dict 格式 models")
	}

	// model_client_config 兼容
	configBase2 := map[string]any{
		"models": map[string]any{
			"video": map[string]any{
				"model_client_config": map[string]any{
					"api_key": "test2",
				},
			},
		},
	}
	mc2 := getModelConfig(configBase2, "video")
	if mc2 == nil || strVal(mc2["api_key"]) != "test2" {
		t.Error("应支持 model_client_config 兼容")
	}

	// 不存在的 modelType
	mc3 := getModelConfig(configBase, "audio")
	if mc3 != nil {
		t.Error("不存在的 modelType 应返回 nil")
	}

	// list 格式 models
	configBase4 := map[string]any{
		"models": []any{
			map[string]any{
				"video": map[string]any{
					"model_config": map[string]any{
						"api_key": "list-key",
					},
				},
			},
		},
	}
	mc4 := getModelConfig(configBase4, "video")
	if mc4 == nil || strVal(mc4["api_key"]) != "list-key" {
		t.Error("应支持 list 格式 models")
	}
}

// TestGetEmbedModelName 测试 getEmbedModelName 辅助函数
func TestGetEmbedModelName(t *testing.T) {
	embedCfg := map[string]any{
		"video_model":  "embed-video-model",
		"vision_model": "embed-vision-model",
		"audio_model":  "embed-audio-model",
	}
	if getEmbedModelName(embedCfg, "video") != "embed-video-model" {
		t.Errorf("video 应返回 embed-video-model，实际 %q", getEmbedModelName(embedCfg, "video"))
	}
	if getEmbedModelName(embedCfg, "vision") != "embed-vision-model" {
		t.Errorf("vision 应返回 embed-vision-model，实际 %q", getEmbedModelName(embedCfg, "vision"))
	}
	if getEmbedModelName(nil, "video") != "" {
		t.Error("nil embedCfg 应返回空")
	}
	if getEmbedModelName(embedCfg, "unknown") != "" {
		t.Error("未知 modelType 应返回空")
	}
}
