package tools

// 多模态工具配置管理模块。
//
// 配置优先级:
//  1. models.{audio/vision/video/image_gen}.model_config
//  2. embed.{audio_model/video_model/vision_model/image_gen_model} 和 embed.embed_api_key/embed_api_base
//  3. 环境变量 MODEL_NAME, API_KEY, API_BASE
//
// Python: jiuwenswarm/agents/harness/common/tools/multimodal_config.py（327 行一比一移植，23.8）
// 注：apply_image_gen_model_config_from_yaml 历史上先落位在 image_gen.go（带偏差注释），维持不动。

import (
	"os"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// MultimodalToolGroups 多模态工具分组清单。
// Python: MULTIMODAL_TOOL_GROUPS:14-21
var MultimodalToolGroups = map[string][]string{
	"vision tools":         {"image_ocr", "visual_question_answering"},
	"audio tools":          {"audio_transcription", "audio_question_answering", "audio_metadata"},
	"video tool":           {"video_understanding"},
	"generate_image tool":  {"generate_image"},
	"generate_video tools": {"generate_video", "check_video_status"},
	"generate_visual tool": {"generate_visual"},
}

// multimodalModelTypes 支持独立配置判定的模型类型。
// Python: _MULTIMODAL_MODEL_TYPES:84
var multimodalModelTypes = []string{"audio", "vision", "video"}

// embedModelKeyMap embed 配置中的模型名称键映射。
// Python: _EMBED_MODEL_KEY_MAP:77-82
var embedModelKeyMap = map[string]string{
	"audio":     "audio_model",
	"vision":    "vision_model",
	"video":     "video_model",
	"image_gen": "image_gen_model",
}

// ──────────────────────────── 全局变量 ────────────────────────────

// multimodalToolNames 全部多模态工具名集合（frozenset 等价物）。
// Python: MULTIMODAL_TOOL_NAMES:22-25
var multimodalToolNames = buildMultimodalToolNames()

// ──────────────────────────── 导出函数 ────────────────────────────

// IsMultimodalToolName 判断给定工具名是否属于多模态工具集合。
// Python: name in MULTIMODAL_TOOL_NAMES
func IsMultimodalToolName(name string) bool {
	_, ok := multimodalToolNames[name]
	return ok
}

// CompleteMultimodalModelConfigured models.{model_type} 是否 key/URL/模型名/provider 四样齐全。
// Python: complete_multimodal_model_configured:95-109
func CompleteMultimodalModelConfigured(configBase map[string]any, modelType string) bool {
	if !isMultimodalModelType(modelType) {
		return false
	}
	mc := getModelConfig(configBase, modelType)
	apiKey := stripAny(mc["api_key"])
	apiBase := stripAny(strOrAny(mc["api_base"], mc["base_url"]))
	modelName := stripAny(strOrAny(mc["model_name"], mc["model"]))
	provider := modelProvider(mc)
	return apiKey != "" && apiBase != "" && modelName != "" && provider != ""
}

// MultimodalModelEnabled 显式能力开关，保留完整遗留配置的兼容判定。
// Python: multimodal_model_enabled:111-130
func MultimodalModelEnabled(configBase map[string]any, modelType string) bool {
	if !isMultimodalModelType(modelType) {
		return false
	}
	// Python: enabled_env = os.getenv(f"{model_type.upper()}_ENABLED")
	if enabledEnv, ok := os.LookupEnv(strings.ToUpper(modelType) + "_ENABLED"); ok {
		return parseBool(enabledEnv, false)
	}

	// Python: raw_models = config_base.get("models")；dict 形状且 model_block 带 "enabled" 键
	rawModels, _ := configBase["models"].(map[string]any)
	if rawModels != nil {
		modelBlock, _ := rawModels[modelType].(map[string]any)
		if modelBlock != nil {
			if _, hasEnabled := modelBlock["enabled"]; hasEnabled {
				return parseBool(modelBlock["enabled"], false)
			}
		}
	}

	// Python: Deterministic compatibility for workspaces created before explicit flags.
	return CompleteMultimodalModelConfigured(configBase, modelType)
}

// ApplyAudioModelConfigFromYAML 从 config.yaml 读取音频模型配置并设置环境变量。
// Python: apply_audio_model_config_from_yaml:150-196
//
// 配置优先级:
//  1. models.audio.model_config
//  2. embed.audio_model + embed.embed_api_key/embed_api_base
//  3. 环境变量 MODEL_NAME, API_KEY, API_BASE
func ApplyAudioModelConfigFromYAML(configBase map[string]any) {
	if configBase == nil {
		return
	}
	mc := getModelConfig(configBase, "audio")
	embedCfg := getEmbedConfig(configBase)

	apiKey := stripAny(mc["api_key"])
	apiBase := stripAny(mc["api_base"])
	modelName := stripAny(strOrAny(mc["model_name"], mc["model"]))
	provider := modelProvider(mc)
	strict := parseBool(mc["strict"], false)

	if !strict {
		if apiKey == "" {
			apiKey = stripAny(strOrAny(embedCfg["embed_api_key"], os.Getenv("API_KEY")))
		}
		if apiBase == "" {
			apiBase = stripAny(strOrAny(embedCfg["embed_api_base"], os.Getenv("API_BASE")))
		}
		if modelName == "" {
			modelName = stripAny(strOrAny(getEmbedModelName(embedCfg, "audio"), os.Getenv("MODEL_NAME")))
		}
		if provider == "" {
			provider = strings.TrimSpace(os.Getenv("MODEL_PROVIDER"))
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

// ApplyVisionModelConfigFromYAML 从 config.yaml 读取视觉模型配置并设置环境变量。
// Python: apply_vision_model_config_from_yaml:198-244
//
// 配置优先级同 ApplyAudioModelConfigFromYAML，仅模型槽与目标环境变量前缀不同。
func ApplyVisionModelConfigFromYAML(configBase map[string]any) {
	if configBase == nil {
		return
	}
	mc := getModelConfig(configBase, "vision")
	embedCfg := getEmbedConfig(configBase)

	apiKey := stripAny(mc["api_key"])
	apiBase := stripAny(mc["api_base"])
	modelName := stripAny(strOrAny(mc["model_name"], mc["model"]))
	provider := modelProvider(mc)
	strict := parseBool(mc["strict"], false)

	if !strict {
		if apiKey == "" {
			apiKey = stripAny(strOrAny(embedCfg["embed_api_key"], os.Getenv("API_KEY")))
		}
		if apiBase == "" {
			apiBase = stripAny(strOrAny(embedCfg["embed_api_base"], os.Getenv("API_BASE")))
		}
		if modelName == "" {
			modelName = stripAny(strOrAny(getEmbedModelName(embedCfg, "vision"), os.Getenv("MODEL_NAME")))
		}
		if provider == "" {
			provider = strings.TrimSpace(os.Getenv("MODEL_PROVIDER"))
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

// ApplyVideoModelConfigFromYAML 从 config.yaml 读取视频模型配置并设置环境变量。
// Python: apply_video_model_config_from_yaml:246-296
//
// 与 audio/vision 的差异：config_base 为 nil 时清除 VIDEO_UNDERSTANDING_STRICT；
// strict=true 时设置 VIDEO_UNDERSTANDING_STRICT=1 并跳过回退链。
func ApplyVideoModelConfigFromYAML(configBase map[string]any) {
	if configBase == nil {
		_ = os.Unsetenv("VIDEO_UNDERSTANDING_STRICT")
		return
	}
	mc := getModelConfig(configBase, "video")
	embedCfg := getEmbedConfig(configBase)

	apiKey := stripAny(mc["api_key"])
	apiBase := stripAny(mc["api_base"])
	modelName := stripAny(strOrAny(mc["model_name"], mc["model"]))
	provider := modelProvider(mc)
	strict := parseBool(mc["strict"], false)

	if strict {
		_ = os.Setenv("VIDEO_UNDERSTANDING_STRICT", "1")
	} else {
		_ = os.Unsetenv("VIDEO_UNDERSTANDING_STRICT")
		if apiKey == "" {
			apiKey = stripAny(strOrAny(embedCfg["embed_api_key"], os.Getenv("API_KEY")))
		}
		if apiBase == "" {
			apiBase = stripAny(strOrAny(embedCfg["embed_api_base"], os.Getenv("API_BASE")))
		}
		if modelName == "" {
			modelName = stripAny(strOrAny(getEmbedModelName(embedCfg, "video"), os.Getenv("MODEL_NAME")))
		}
		if provider == "" {
			provider = strings.TrimSpace(os.Getenv("MODEL_PROVIDER"))
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

// DedicatedMultimodalModelConfigured 检查 models.{modelType} 是否有独立 api_key。
// Python 侧无此函数（Go 既有装配门控，23.8 Task13 将切换为 multimodal_model_enabled +
// complete_multimodal_model_configured 双门控后按规则 8 删除）。
func DedicatedMultimodalModelConfigured(configBase map[string]any, modelType string) bool {
	if !isMultimodalModelType(modelType) {
		return false
	}
	mc := getModelConfig(configBase, modelType)
	return strings.TrimSpace(stripAny(mc["api_key"])) != ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildMultimodalToolNames 从分组清单展开全部工具名。
// Python: MULTIMODAL_TOOL_NAMES:22-25
func buildMultimodalToolNames() map[string]struct{} {
	names := make(map[string]struct{})
	for _, group := range MultimodalToolGroups {
		for _, name := range group {
			names[name] = struct{}{}
		}
	}
	return names
}

// isMultimodalModelType 判定是否在三类独立配置模型类型内。
// Python: model_type not in _MULTIMODAL_MODEL_TYPES
func isMultimodalModelType(modelType string) bool {
	for _, t := range multimodalModelTypes {
		if t == modelType {
			return true
		}
	}
	return false
}

// modelProvider 读取模型提供方：client_provider 优先，回落 model_provider。
// Python: _model_provider:87-93
func modelProvider(mc map[string]any) string {
	return stripAny(strOrAny(mc["client_provider"], mc["model_provider"]))
}

// parseBool 解析宽松布尔值。
// Python: _parse_bool:27-34
func parseBool(val any, defaultVal bool) bool {
	if val == nil {
		return defaultVal
	}
	if b, ok := val.(bool); ok {
		return b
	}
	s := strings.ToLower(strings.TrimSpace(stripAny(val)))
	return s == "1" || s == "true" || s == "yes" || s == "on"
}

// getModelConfig 从 config.yaml 解析 models.{modelType}.model_config（或 model_client_config）。
// 支持 dict 与 list 两种 config 形状。
// Python: _get_model_config:41-75
func getModelConfig(configBase map[string]any, modelType string) map[string]any {
	// Python: if not isinstance(config_base, dict): return {}
	if configBase == nil {
		return map[string]any{}
	}

	// Python: raw_models = config_base.get("models")；dict 形状
	rawModels, ok := configBase["models"].(map[string]any)
	if ok {
		inner, _ := rawModels[modelType].(map[string]any)
		if inner != nil {
			if mc, ok := inner["model_config"].(map[string]any); ok {
				return mc
			}
			if mc, ok := inner["model_client_config"].(map[string]any); ok {
				return mc
			}
		}
		return map[string]any{}
	}

	// Python: list 形状逐块扫描
	rawModelsList, _ := configBase["models"].([]any)
	for _, block := range rawModelsList {
		b, _ := block.(map[string]any)
		if b == nil {
			continue
		}
		inner, _ := b[modelType].(map[string]any)
		if inner == nil {
			continue
		}
		if mc, ok := inner["model_config"].(map[string]any); ok {
			return mc
		}
		if mc, ok := inner["model_client_config"].(map[string]any); ok {
			return mc
		}
	}
	return map[string]any{}
}

// getEmbedConfig 从 config.yaml 解析 embed 配置。
// Python: _get_embed_config:36-39
func getEmbedConfig(configBase map[string]any) map[string]any {
	if configBase == nil {
		return map[string]any{}
	}
	embed, _ := configBase["embed"].(map[string]any)
	return embed
}

// getEmbedModelName 从 embed 配置获取指定类型模型名称。
// Python: _get_embed_model_name:133-148
func getEmbedModelName(embedCfg map[string]any, modelType string) string {
	key, ok := embedModelKeyMap[modelType]
	if !ok || embedCfg == nil {
		return ""
	}
	return strings.TrimSpace(stripAny(embedCfg[key]))
}

// strOr 返回第一个 strip 后非空的值。
// Python: val1 or val2 短路逻辑。
func strOrAny(v1, v2 any) string {
	s1 := stripAny(v1)
	if s1 != "" {
		return s1
	}
	return stripAny(v2)
}
