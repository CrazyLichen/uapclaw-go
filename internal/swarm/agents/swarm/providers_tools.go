// Python 源：jiuwenswarm/agents/swarm/providers/tools.py 的多模态配置参数段
// （:270-355 区段：vision_model_config_params / audio_dedicated_configured /
// audio_model_config_params / _parse_int）+ 头部常量段（:117-127）。
//
// scoped 落地说明（偏差 D-15.16-e10 ③）：tools.py 其余函数是 rail/tool 的
// 构造函数段（_build_skill_toolkit_tools / skill_retrieval_toolkit_for_context /
// _build_video_tools 等，:161-270/:362-687），消费方是 rail 注册缝
// （register_swarm_providers 的 tools 注册段），⤴️15.16f（2026-12-02 用户裁决改标：15.7/15.8 仅 cc/cx SDK
// provider，swarm 平台要素归 15.16f 新小节）；常量字面值已由 registry.go 常量段承载。

package swarm

import (
	"os"
	"strconv"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// VisionModelConfigParams 返回 VisionModelConfig 构造 kwargs，未配置得空 map。
// 要求专用 models.vision 键 + yaml 应用后完整的 VISION_* env 映射
// （与单 agent 构建同款判定）。
// Python: vision_model_config_params (providers/tools.py:277-304)
func VisionModelConfigParams(config map[string]any) map[string]any {
	if !tools.MultimodalModelEnabled(config, "vision") ||
		!tools.CompleteMultimodalModelConfigured(config, "vision") {
		return map[string]any{}
	}
	tools.ApplyVisionModelConfigFromYAML(config)
	apiKey := strings.TrimSpace(os.Getenv("VISION_API_KEY"))
	baseURL := strings.TrimSpace(orEnv("VISION_BASE_URL", "VISION_API_BASE"))
	modelName := strings.TrimSpace(orEnv("VISION_MODEL", "VISION_MODEL_NAME"))
	if apiKey == "" || baseURL == "" || modelName == "" {
		return map[string]any{}
	}
	return map[string]any{
		"api_key":     apiKey,
		"base_url":    baseURL,
		"model":       modelName,
		"max_retries": parseIntDefault(os.Getenv("VISION_MAX_RETRIES"), 3),
	}
}

// AudioDedicatedConfigured 是否配置了完整的专用音频模型。
// Python: audio_dedicated_configured (providers/tools.py:306-308)
func AudioDedicatedConfigured(config map[string]any) bool {
	return tools.MultimodalModelEnabled(config, "audio") &&
		tools.CompleteMultimodalModelConfigured(config, "audio")
}

// AudioModelConfigParams 返回 AudioModelConfig 构造 kwargs，不完整得空 map。
// Python: audio_model_config_params (providers/tools.py:313-355 区段)
func AudioModelConfigParams(config map[string]any) map[string]any {
	if !AudioDedicatedConfigured(config) {
		return map[string]any{}
	}
	tools.ApplyAudioModelConfigFromYAML(config)
	apiKey := strings.TrimSpace(os.Getenv("AUDIO_API_KEY"))
	baseURL := strings.TrimSpace(orEnv("AUDIO_BASE_URL", "AUDIO_API_BASE"))
	if apiKey == "" || baseURL == "" {
		return map[string]any{}
	}
	transcriptionModel := strings.TrimSpace(orEnv("AUDIO_TRANSCRIPTION_MODEL", "AUDIO_MODEL_NAME"))
	questionAnsweringModel := strings.TrimSpace(orEnv("AUDIO_QUESTION_ANSWERING_MODEL", "AUDIO_MODEL_NAME"))
	configKwargs := map[string]any{
		"api_key":                  apiKey,
		"base_url":                 baseURL,
		"max_retries":              parseIntDefault(os.Getenv("AUDIO_MAX_RETRIES"), 3),
		"http_timeout":             parseIntDefault(os.Getenv("AUDIO_HTTP_TIMEOUT"), 20),
		"max_audio_bytes":          parseIntDefault(os.Getenv("AUDIO_MAX_AUDIO_BYTES"), 25*1024*1024),
		"transcription_model":      transcriptionModel,
		"question_answering_model": questionAnsweringModel,
	}
	// ACR 三键仅在配置了 access key 时写入（Python 条件 setdefault 语义）。
	// Python: providers/tools.py:343-354
	acrAccessKey := strings.TrimSpace(os.Getenv("ACR_ACCESS_KEY"))
	acrAccessSecret := strings.TrimSpace(os.Getenv("ACR_ACCESS_SECRET"))
	acrBaseURL := strings.TrimSpace(os.Getenv("ACR_BASE_URL"))
	if acrAccessKey != "" {
		configKwargs["acr_access_key"] = acrAccessKey
	}
	if acrAccessSecret != "" {
		configKwargs["acr_access_secret"] = acrAccessSecret
	}
	if acrBaseURL != "" {
		configKwargs["acr_base_url"] = acrBaseURL
	}
	return configKwargs
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseIntDefault 宽松整数解析（Python _parse_int，providers/tools.py:261-263）。
func parseIntDefault(value string, defaultVal int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return n
	}
	return defaultVal
}

// orEnv 取首个非空 env 值（Python `os.getenv(A) or os.getenv(B) or ""`）。
func orEnv(primary, fallback string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	return os.Getenv(fallback)
}
