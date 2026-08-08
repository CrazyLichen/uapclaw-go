package adapter

import (
	"fmt"
	"os"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// embedModelKeyMap embed 配置中的模型名称键映射。
// 对齐 Python: _EMBED_MODEL_KEY_MAP
var embedModelKeyMap = map[string]string{
	"audio":     "audio_model",
	"vision":    "vision_model",
	"video":     "video_model",
	"image_gen": "image_gen_model",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ApplyVideoModelConfigFromYAML 从 config.yaml 读取视频模型配置并设置环境变量。
// 对齐 Python: apply_video_model_config_from_yaml(config_base)
//
// 配置优先级:
//  1. models.video.model_config
//  2. embed.video_model + embed.embed_api_key/embed_api_base（strict=false 时回退）
//  3. 环境变量 VIDEO_API_KEY/VIDEO_API_BASE/VIDEO_MODEL_NAME
func ApplyVideoModelConfigFromYAML(configBase map[string]any) {
	if configBase == nil {
		_ = os.Unsetenv("VIDEO_UNDERSTANDING_STRICT")
		return
	}

	mc := getModelConfig(configBase, "video")
	apiKey := strVal(mc["api_key"])
	apiBase := strVal(mc["api_base"])
	modelName := strOr(mc["model_name"], mc["model"])
	provider := strVal(mc["model_provider"])
	strict := parseBool(mc["strict"], false)

	if strict {
		_ = os.Setenv("VIDEO_UNDERSTANDING_STRICT", "1")
	} else {
		_ = os.Unsetenv("VIDEO_UNDERSTANDING_STRICT")
		embedCfg := getEmbedConfig(configBase)
		if apiKey == "" {
			apiKey = strOr(embedCfg["embed_api_key"], os.Getenv("API_KEY"))
		}
		if apiBase == "" {
			apiBase = strOr(embedCfg["embed_api_base"], os.Getenv("API_BASE"))
		}
		if modelName == "" {
			modelName = strOr(getEmbedModelName(embedCfg, "video"), os.Getenv("MODEL_NAME"))
		}
		if provider == "" {
			provider = os.Getenv("MODEL_PROVIDER")
		}
	}

	if apiKey != "" {
		_ = os.Setenv("VIDEO_API_KEY", apiKey)
	}
	if apiBase != "" {
		_ = os.Setenv("VIDEO_API_BASE", apiBase)
	}
	if modelName != "" {
		_ = os.Setenv("VIDEO_MODEL_NAME", modelName)
	}
	if provider != "" {
		_ = os.Setenv("VIDEO_PROVIDER", provider)
	}
}

// ApplyVisionModelConfigFromYAML 从 config.yaml 读取视觉模型配置并设置环境变量。
// 对齐 Python: apply_vision_model_config_from_yaml(config_base)
//
// 配置优先级:
//  1. models.vision.model_config
//  2. embed.vision_model + embed.embed_api_key/embed_api_base（strict=false 时回退）
//  3. 环境变量 VISION_API_KEY/VISION_API_BASE/VISION_MODEL_NAME
func ApplyVisionModelConfigFromYAML(configBase map[string]any) {
	if configBase == nil {
		return
	}

	mc := getModelConfig(configBase, "vision")
	apiKey := strVal(mc["api_key"])
	apiBase := strVal(mc["api_base"])
	modelName := strOr(mc["model_name"], mc["model"])
	provider := strVal(mc["model_provider"])
	strict := parseBool(mc["strict"], false)

	if !strict {
		embedCfg := getEmbedConfig(configBase)
		if apiKey == "" {
			apiKey = strOr(embedCfg["embed_api_key"], os.Getenv("API_KEY"))
		}
		if apiBase == "" {
			apiBase = strOr(embedCfg["embed_api_base"], os.Getenv("API_BASE"))
		}
		if modelName == "" {
			modelName = strOr(getEmbedModelName(embedCfg, "vision"), os.Getenv("MODEL_NAME"))
		}
		if provider == "" {
			provider = os.Getenv("MODEL_PROVIDER")
		}
	}

	if apiKey != "" {
		_ = os.Setenv("VISION_API_KEY", apiKey)
	}
	if apiBase != "" {
		_ = os.Setenv("VISION_API_BASE", apiBase)
	}
	if modelName != "" {
		_ = os.Setenv("VISION_MODEL_NAME", modelName)
	}
	if provider != "" {
		_ = os.Setenv("VISION_PROVIDER", provider)
	}
}

// ApplyAudioModelConfigFromYAML 从 config.yaml 读取音频模型配置并设置环境变量。
// 对齐 Python: apply_audio_model_config_from_yaml(config_base)
//
// 配置优先级:
//  1. models.audio.model_config
//  2. embed.audio_model + embed.embed_api_key/embed_api_base（strict=false 时回退）
//  3. 环境变量 AUDIO_API_KEY/AUDIO_API_BASE/AUDIO_MODEL_NAME
func ApplyAudioModelConfigFromYAML(configBase map[string]any) {
	if configBase == nil {
		return
	}

	mc := getModelConfig(configBase, "audio")
	apiKey := strVal(mc["api_key"])
	apiBase := strVal(mc["api_base"])
	modelName := strOr(mc["model_name"], mc["model"])
	provider := strVal(mc["model_provider"])
	strict := parseBool(mc["strict"], false)

	if !strict {
		embedCfg := getEmbedConfig(configBase)
		if apiKey == "" {
			apiKey = strOr(embedCfg["embed_api_key"], os.Getenv("API_KEY"))
		}
		if apiBase == "" {
			apiBase = strOr(embedCfg["embed_api_base"], os.Getenv("API_BASE"))
		}
		if modelName == "" {
			modelName = strOr(getEmbedModelName(embedCfg, "audio"), os.Getenv("MODEL_NAME"))
		}
		if provider == "" {
			provider = os.Getenv("MODEL_PROVIDER")
		}
	}

	if apiKey != "" {
		_ = os.Setenv("AUDIO_API_KEY", apiKey)
	}
	if apiBase != "" {
		_ = os.Setenv("AUDIO_API_BASE", apiBase)
	}
	if modelName != "" {
		_ = os.Setenv("AUDIO_MODEL_NAME", modelName)
	}
	if provider != "" {
		_ = os.Setenv("AUDIO_PROVIDER", provider)
	}
}

// DedicatedMultimodalModelConfigured 检查 models.{modelType} 是否有独立 api_key。
// 对齐 Python: dedicated_multimodal_model_configured(config_base, model_type)
//
// 仅当 models.{type}.model_config 中显式配置了 api_key 时才返回 true，
// 用于注册门控：无独立 key 时不挂载多模态工具。
func DedicatedMultimodalModelConfigured(configBase map[string]any, modelType string) bool {
	if modelType != "audio" && modelType != "vision" && modelType != "video" {
		return false
	}
	mc := getModelConfig(configBase, modelType)
	apiKey := strings.TrimSpace(strVal(mc["api_key"]))
	return apiKey != ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getModelConfig 从 config.yaml 解析 models.{modelType}.model_config。
// 对齐 Python: _get_model_config(config_base, model_type)
func getModelConfig(configBase map[string]any, modelType string) map[string]any {
	if configBase == nil {
		return nil
	}
	rawModels, _ := configBase["models"].(map[string]any)
	if rawModels != nil {
		inner, _ := rawModels[modelType].(map[string]any)
		if inner != nil {
			mc, _ := inner["model_config"].(map[string]any)
			if mc != nil {
				return mc
			}
			mc2, _ := inner["model_client_config"].(map[string]any)
			if mc2 != nil {
				return mc2
			}
		}
		return nil
	}
	// models 可能是列表格式
	rawModelsList, _ := configBase["models"].([]any)
	if rawModelsList != nil {
		for _, block := range rawModelsList {
			b, _ := block.(map[string]any)
			if b == nil {
				continue
			}
			inner, _ := b[modelType].(map[string]any)
			if inner != nil {
				mc, _ := inner["model_config"].(map[string]any)
				if mc != nil {
					return mc
				}
				mc2, _ := inner["model_client_config"].(map[string]any)
				if mc2 != nil {
					return mc2
				}
			}
		}
	}
	return nil
}

// getEmbedConfig 从 config.yaml 解析 embed 配置。
// 对齐 Python: _get_embed_config(config_base)
func getEmbedConfig(configBase map[string]any) map[string]any {
	if configBase == nil {
		return nil
	}
	embed, _ := configBase["embed"].(map[string]any)
	return embed
}

// getEmbedModelName 从 embed 配置获取模型名称。
// 对齐 Python: _get_embed_model_name(embed_cfg, model_type)
func getEmbedModelName(embedCfg map[string]any, modelType string) string {
	key, ok := embedModelKeyMap[modelType]
	if !ok || embedCfg == nil {
		return ""
	}
	return strings.TrimSpace(strVal(embedCfg[key]))
}

// parseBool 解析布尔值。
// 对齐 Python: _parse_bool(val, default)
func parseBool(val any, defaultVal bool) bool {
	if val == nil {
		return defaultVal
	}
	if b, ok := val.(bool); ok {
		return b
	}
	s := strings.ToLower(strings.TrimSpace(strVal(val)))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}

// strVal 提取字符串值。
// 对齐 Python: str(val).strip()
func strVal(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

// strOr 返回第一个非空字符串值。
// 对齐 Python: val1 or val2 逻辑
func strOr(v1, v2 any) string {
	s1 := strVal(v1)
	if s1 != "" {
		return s1
	}
	return strVal(v2)
}
