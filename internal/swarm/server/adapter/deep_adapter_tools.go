package adapter

import (
	"os"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// syncToolGroup 同步工具组。
// 对齐 Python: _sync_tool_group() (line 1319-1350)
// ⤵️ agentcore: 需要实例化后的 AbilityManager
func (d *DeepAdapter) syncToolGroup(toolGroup string, configBase map[string]any) {
	// ⤵️ agentcore: 调用 AbilityManager.Add/Remove 同步工具组
	logger.Info(logComponent).Str("tool_group", toolGroup).Msg("syncToolGroup 等待回填")
}

// removeRegisteredTools 移除已注册的工具。
// 对齐 Python: _remove_registered_tools() (line 1351-1380)
func (d *DeepAdapter) removeRegisteredTools(toolIDs []string) {
	// ⤵️ agentcore: 调用 AbilityManager.Remove
	logger.Info(logComponent).Int("count", len(toolIDs)).Msg("removeRegisteredTools 等待回填")
}

// appendToolCard 追加工具卡片。
// 对齐 Python: _append_tool_card() (line 1381-1410)
func (d *DeepAdapter) appendToolCard(cards []any) {
	// ⤵️ agentcore: 调用 AbilityManager.Add
	logger.Info(logComponent).Int("count", len(cards)).Msg("appendToolCard 等待回填")
}

// prioritizePaidSearchToolCard 优先付费搜索工具卡片。
// 对齐 Python: _prioritize_paid_search_tool_card() (line 1411-1440)
func (d *DeepAdapter) prioritizePaidSearchToolCard(cards []any) []any {
	// ⤵️ agentcore: 付费搜索工具优先排序
	return cards
}

// pruneToolCards 裁剪工具卡片。
// 对齐 Python: _prune_tool_cards() (line 1441-1476)
func (d *DeepAdapter) pruneToolCards(cards []any, mode string) []any {
	// ⤵️ agentcore: 按模式裁剪工具卡片
	return cards
}

// syncMultimodalToolsForRuntime 热同步多模态工具。
// 对齐 Python: _sync_multimodal_tools_for_runtime() (line 1170-1238)
func (d *DeepAdapter) syncMultimodalToolsForRuntime() {
	// ⤵️ agentcore: 根据 visionModelConfig/audioModelConfig 注册/注销多模态工具
	if d.visionModelConfig != nil && !d.visionToolsRegistered {
		logger.Info(logComponent).Msg("视觉模型配置已就绪，等待工具注册回填")
	}
	if d.audioModelConfig != nil && !d.audioToolsRegistered {
		logger.Info(logComponent).Msg("音频模型配置已就绪，等待工具注册回填")
	}
}

// syncPaidSearchToolForRuntime 热同步付费搜索工具。
// 对齐 Python: _sync_paid_search_tool_for_runtime() (line 1240-1270)
func (d *DeepAdapter) syncPaidSearchToolForRuntime() {
	// ⤵️ agentcore: 付费搜索工具热同步
	logger.Info(logComponent).Msg("syncPaidSearchToolForRuntime 等待回填")
}

// refreshMultimodalConfigs 刷新多模态配置。
// 对齐 Python: _refresh_multimodal_configs(config_base) (line 1170-1318)
func (d *DeepAdapter) refreshMultimodalConfigs(configBase map[string]any) {
	d.visionModelConfig = d.buildVisionModelConfig(configBase)
	d.audioModelConfig = d.buildAudioModelConfig(configBase)
	d.videoToolRegistered = d.buildVideoModelConfig(configBase)
	d.imageGenToolRegistered = d.buildImageGenModelConfig(configBase)
}

// buildVisionModelConfig 从配置构建视觉模型配置。
// 对齐 Python: _build_vision_model_config(config_base) (line 1170-1238)
func (d *DeepAdapter) buildVisionModelConfig(configBase map[string]any) *schema.VisionModelConfig {
	modelsSection, _ := configBase["models"].(map[string]any)
	if modelsSection == nil {
		return nil
	}
	visionSection, _ := modelsSection["vision"].(map[string]any)
	if visionSection == nil {
		return nil
	}
	apiKey, _ := visionSection["api_key"].(string)
	baseURL, _ := visionSection["base_url"].(string)
	model, _ := visionSection["model"].(string)
	if apiKey == "" && model == "" {
		return nil
	}
	maxRetries := 3
	if v, ok := visionSection["max_retries"]; ok {
		if f, ok := v.(float64); ok {
			maxRetries = int(f)
		}
	}
	return &schema.VisionModelConfig{
		APIKey:     apiKey,
		BaseURL:    baseURL,
		Model:      model,
		MaxRetries: maxRetries,
	}
}

// buildAudioModelConfig 从配置构建音频模型配置。
// 对齐 Python: _build_audio_model_config(config_base) (line 1240-1318)
func (d *DeepAdapter) buildAudioModelConfig(configBase map[string]any) *schema.AudioModelConfig {
	modelsSection, _ := configBase["models"].(map[string]any)
	if modelsSection == nil {
		return nil
	}
	audioSection, _ := modelsSection["audio"].(map[string]any)
	if audioSection == nil {
		return nil
	}
	apiKey, _ := audioSection["api_key"].(string)
	baseURL, _ := audioSection["base_url"].(string)
	if apiKey == "" {
		return nil
	}
	transcriptionModel, _ := audioSection["transcription_model"].(string)
	qaModel, _ := audioSection["qa_model"].(string)
	maxRetries := 3
	if v, ok := audioSection["max_retries"]; ok {
		if f, ok := v.(float64); ok {
			maxRetries = int(f)
		}
	}
	httpTimeout := 30
	if v, ok := audioSection["http_timeout"]; ok {
		if f, ok := v.(float64); ok {
			httpTimeout = int(f)
		}
	}
	maxAudioBytes := 25000000
	if v, ok := audioSection["max_audio_bytes"]; ok {
		if f, ok := v.(float64); ok {
			maxAudioBytes = int(f)
		}
	}
	return &schema.AudioModelConfig{
		APIKey:             apiKey,
		BaseURL:            baseURL,
		TranscriptionModel: transcriptionModel,
		QAModel:            qaModel,
		MaxRetries:         maxRetries,
		HTTPTimeout:        httpTimeout,
		MaxAudioBytes:      maxAudioBytes,
	}
}

// buildVideoModelConfig 构建视频模型配置。
// 对齐 Python: _build_video_model_config(config_base) (line 1244-1260)
// 返回 bool 表示视频工具是否启用（Python 原实现返回 bool，通过环境变量传递配置）。
// ⤵️ 9.38-49 Harness 工具集: apply_video_model_config_from_yaml + dedicated_multimodal_model_configured
func (d *DeepAdapter) buildVideoModelConfig(configBase map[string]any) bool {
	// ⤵️ 9.38-49: apply_video_model_config_from_yaml(configBase) — 将 YAML 配置映射到环境变量
	// applyVideoModelConfigFromYAML(configBase)

	// ⤵️ 9.38-49: dedicated_multimodal_model_configured(config_base, "video") — 检查 models.video 是否有独立 api_key
	// if !dedicatedMultimodalModelConfigured(configBase, "video") {
	// 	logger.Info(logComponent).Msg("skip video_understanding: models.video has no dedicated api_key in config.yaml")
	// 	return false
	// }

	if os.Getenv("VIDEO_API_KEY") == "" {
		logger.Info(logComponent).Msg("video tools skipped: incomplete config (VIDEO_API_KEY not set)")
		return false
	}
	return true
}

// buildImageGenModelConfig 构建图片生成模型配置。
// 对齐 Python: _build_image_gen_model_config(config_base) (line 1261-1270)
// 返回 bool 表示图片生成工具是否启用（Python 原实现返回 bool，通过环境变量传递配置）。
// ⤵️ 9.38-49 Harness 工具集: apply_image_gen_model_config_from_yaml
func (d *DeepAdapter) buildImageGenModelConfig(configBase map[string]any) bool {
	// ⤵️ 9.38-49: apply_image_gen_model_config_from_yaml(configBase) — 将 YAML 配置映射到环境变量
	// applyImageGenModelConfigFromYAML(configBase)

	if os.Getenv("IMAGE_GEN_API_KEY") == "" {
		logger.Info(logComponent).Msg("image_gen tool skipped: incomplete config (IMAGE_GEN_API_KEY not set)")
		return false
	}
	return true
}
