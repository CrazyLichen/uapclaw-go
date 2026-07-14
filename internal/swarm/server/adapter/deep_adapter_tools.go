package adapter

import (
	"context"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// 工具名称常量，对齐 Python tool_cards 中的工具名。
// ⤵️ 9.38-49 Harness 工具集: 具体工具名称待回填
const (
	// ToolNamePaidSearch 付费搜索工具
	ToolNamePaidSearch = "paid_search"
	// ToolNameFreeSearch 免费搜索工具
	ToolNameFreeSearch = "free_search"
	// ToolNameWebSearch Web 搜索工具
	ToolNameWebSearch = "web_search"
	// ToolNameLocalSearch 本地搜索工具
	ToolNameLocalSearch = "local_search"
	// ToolNameCodeSearch 代码搜索工具
	ToolNameCodeSearch = "code_search"
	// ToolNameFileSearch 文件搜索工具
	ToolNameFileSearch = "file_search"
	// ToolNameReadFile 读文件工具
	ToolNameReadFile = "read_file"
	// ToolNameWriteFile 写文件工具
	ToolNameWriteFile = "write_file"
	// ToolNameListDir 列目录工具
	ToolNameListDir = "list_dir"
	// ToolNameShellExec Shell 执行工具
	ToolNameShellExec = "shell_exec"
	// ToolNameApplyPatch 应用补丁工具
	ToolNameApplyPatch = "apply_patch"
	// ToolNameAskUser 询问用户工具
	ToolNameAskUser = "ask_user"
	// ToolNameTodoRead 待办读取工具
	ToolNameTodoRead = "todo_read"
	// ToolNameTodoWrite 待办写入工具
	ToolNameTodoWrite = "todo_write"
	// ToolNameVideoUnderstanding 视频理解工具
	ToolNameVideoUnderstanding = "video_understanding"
	// ToolNameImageGeneration 图片生成工具
	ToolNameImageGeneration = "image_gen"
	// ToolNameVision 视觉工具
	ToolNameVision = "vision"
	// ToolNameAudioTranscription 音频转录工具
	ToolNameAudioTranscription = "audio_transcription"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// syncToolGroup 同步工具组。
// 对齐 Python: _sync_tool_group() (line 1319-1350)
//
// 双重操作：调用 AbilityManager.Add/Remove 同步工具到 Agent，
// 同时调用 ResourceMgr.AddTool/RemoveTool 同步到资源管理器。
func (d *DeepAdapter) syncToolGroup(toolGroup string, configBase map[string]any) {
	if d.instance == nil {
		logger.Warn(logComponent).Str("tool_group", toolGroup).Msg("syncToolGroup: instance 未初始化，跳过")
		return
	}

	reactAgent := d.instance.ReactAgent()
	if reactAgent == nil {
		logger.Warn(logComponent).Str("tool_group", toolGroup).Msg("syncToolGroup: ReactAgent 为 nil，跳过")
		return
	}

	// 步骤 1: 从配置解析该工具组应启用的工具
	// ⤵️ 9.38-49: 根据 toolGroup 解析 configBase 中的工具配置
	var toolInstancesToAdd []tool.Tool  // ResourceMgr.AddTool 需要 tool.Tool
	var toolCardsToAdd []*tool.ToolCard // AbilityManager.Add 需要 schema.Ability（*ToolCard 实现）
	var toolIDsToRemove []string
	_ = toolInstancesToAdd // ⤵️ 9.38-49: 从 configBase 解析待添加的工具实例
	_ = toolCardsToAdd     // ⤵️ 9.38-49: 从 configBase 解析待添加的工具卡片
	_ = toolIDsToRemove    // ⤵️ 9.38-49: 从 AbilityManager 查询当前已注册的同组工具

	// 步骤 2: 双重操作 — Add
	am := reactAgent.AbilityManager()
	for _, tc := range toolCardsToAdd {
		if am != nil {
			am.Add(tc)
		}
	}
	for _, t := range toolInstancesToAdd {
		if err := runner.GetResourceMgr().AddTool(t, resources_manager.WithTag(resources_manager.Tag(toolGroup))); err != nil {
			logger.Warn(logComponent).Err(err).Str("tool_group", toolGroup).Msg("AddTool 到 ResourceMgr 失败")
		}
	}

	// 步骤 3: 双重操作 — Remove
	if am != nil && len(toolIDsToRemove) > 0 {
		am.RemoveMany(toolIDsToRemove)
	}
	if len(toolIDsToRemove) > 0 {
		if _, err := runner.GetResourceMgr().RemoveTool(toolIDsToRemove, resources_manager.WithTag(resources_manager.Tag(toolGroup))); err != nil {
			logger.Warn(logComponent).Err(err).Str("tool_group", toolGroup).Msg("RemoveTool 从 ResourceMgr 失败")
		}
	}

	logger.Info(logComponent).Str("tool_group", toolGroup).Int("add_count", len(toolCardsToAdd)).Int("remove_count", len(toolIDsToRemove)).Msg("syncToolGroup 完成")
}

// removeRegisteredTools 移除已注册的工具。
// 对齐 Python: _remove_registered_tools() (line 1351-1380)
//
// 双重操作：调用 AbilityManager.Remove + ResourceMgr.RemoveTool。
func (d *DeepAdapter) removeRegisteredTools(toolIDs []string) {
	if len(toolIDs) == 0 {
		return
	}

	if d.instance != nil {
		if reactAgent := d.instance.ReactAgent(); reactAgent != nil {
			if am := reactAgent.AbilityManager(); am != nil {
				am.RemoveMany(toolIDs)
			}
		}
	}

	if _, err := runner.GetResourceMgr().RemoveTool(toolIDs); err != nil {
		logger.Warn(logComponent).Err(err).Int("count", len(toolIDs)).Msg("RemoveTool 从 ResourceMgr 失败")
	}

	logger.Info(logComponent).Int("count", len(toolIDs)).Msg("removeRegisteredTools 完成")
}

// appendToolCard 追加工具卡片。
// 对齐 Python: _append_tool_card() (line 1381-1410)
//
// 去重追加到 d.toolCards：若已有同名 ToolCard 则跳过。
func (d *DeepAdapter) appendToolCard(cards []*tool.ToolCard) {
	if len(cards) == 0 {
		return
	}

	// 获取当前 toolCards 列表
	current := d.toolCards

	// 去重：收集已有名称
	existing := make(map[string]bool, len(current))
	for _, c := range current {
		existing[c.Name] = true
	}

	// 追加新卡片（去重）
	for _, c := range cards {
		if !existing[c.Name] {
			current = append(current, c)
			existing[c.Name] = true
		}
	}

	d.toolCards = current
	logger.Info(logComponent).Int("total_count", len(current)).Msg("appendToolCard 完成")
}

// prioritizePaidSearchToolCard 优先付费搜索工具卡片。
// 对齐 Python: _prioritize_paid_search_tool_card() (line 1411-1440)
//
// 将 paid_search 工具排在 free_search 工具之前。
// 若付费搜索已注册，则将 free_search 降权排后。
func (d *DeepAdapter) prioritizePaidSearchToolCard(cards []*tool.ToolCard) []*tool.ToolCard {
	if len(cards) == 0 {
		return cards
	}

	// 检查是否有付费搜索工具
	hasPaidSearch := false
	for _, c := range cards {
		if c.Name == ToolNamePaidSearch {
			hasPaidSearch = true
			break
		}
	}

	if !hasPaidSearch {
		return cards
	}

	// 将 paid_search 排在 free_search 之前
	var paid []*tool.ToolCard
	var free []*tool.ToolCard
	var other []*tool.ToolCard

	for _, c := range cards {
		switch c.Name {
		case ToolNamePaidSearch, ToolNameWebSearch:
			paid = append(paid, c)
		case ToolNameFreeSearch, ToolNameLocalSearch, ToolNameCodeSearch, ToolNameFileSearch:
			free = append(free, c)
		default:
			other = append(other, c)
		}
	}

	result := make([]*tool.ToolCard, 0, len(cards))
	result = append(result, paid...)
	result = append(result, other...)
	result = append(result, free...)
	return result
}

// pruneToolCards 裁剪工具卡片。
// 对齐 Python: _prune_tool_cards() (line 1441-1476)
//
// 按名称集合移除指定的工具卡片。
func (d *DeepAdapter) pruneToolCards(cards []*tool.ToolCard, namesToRemove map[string]bool) []*tool.ToolCard {
	if len(namesToRemove) == 0 || len(cards) == 0 {
		return cards
	}

	result := make([]*tool.ToolCard, 0, len(cards))
	for _, c := range cards {
		if !namesToRemove[c.Name] {
			result = append(result, c)
		}
	}
	return result
}

// syncMultimodalToolsForRuntime 热同步多模态工具。
// 对齐 Python: _sync_multimodal_tools_for_runtime() (line 1170-1238)
// ⤵️ 9.38-49 Harness 工具集: 多模态工具注册/注销
func (d *DeepAdapter) syncMultimodalToolsForRuntime() {
	if d.instance == nil {
		return
	}
	reactAgent := d.instance.ReactAgent()
	if reactAgent == nil {
		return
	}

	// 视觉工具同步
	if d.visionModelConfig != nil && !d.visionToolsRegistered {
		// ⤵️ 9.38-49: 注册视觉工具到 AbilityManager + ResourceMgr
		logger.Info(logComponent).Msg("视觉模型配置已就绪，等待工具注册回填")
	}
	if d.visionModelConfig == nil && d.visionToolsRegistered {
		d.removeRegisteredTools([]string{ToolNameVision})
		d.visionToolsRegistered = false
	}

	// 音频工具同步
	if d.audioModelConfig != nil && !d.audioToolsRegistered {
		// ⤵️ 9.38-49: 注册音频工具到 AbilityManager + ResourceMgr
		logger.Info(logComponent).Msg("音频模型配置已就绪，等待工具注册回填")
	}
	if d.audioModelConfig == nil && d.audioToolsRegistered {
		d.removeRegisteredTools([]string{ToolNameAudioTranscription})
		d.audioToolsRegistered = false
	}

	// 视频工具同步
	if d.videoToolRegistered {
		// TODO: ⤵️ 9.38-49: 确保视频工具已注册
		_ = d.videoToolRegistered
	}

	// 图片生成工具同步
	if d.imageGenToolRegistered {
		// TODO: ⤵️ 9.38-49: 确保图片生成工具已注册
		_ = d.imageGenToolRegistered
	}
}

// syncPaidSearchToolForRuntime 热同步付费搜索工具。
// 对齐 Python: _sync_paid_search_tool_for_runtime() (line 1240-1270)
// ⤵️ 9.38-49 Harness 工具集: 付费搜索工具热同步
func (d *DeepAdapter) syncPaidSearchToolForRuntime() {
	if d.instance == nil {
		return
	}

	// ⤵️ 9.38-49: 根据 paidSearchRegistered 状态注册/注销付费搜索工具
	// 待实现：付费搜索注册检测 if d.paidSearchRegistered {
	//     // 注册 paid_search 工具
	// } else {
	//     // 移除 paid_search 工具
	// }
	logger.Info(logComponent).Bool("registered", d.paidSearchRegistered).Msg("syncPaidSearchToolForRuntime 等待回填")
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
	// 待实现：应用视频模型配置 applyVideoModelConfigFromYAML(configBase)

	// ⤵️ 9.38-49: dedicated_multimodal_model_configured(config_base, "video") — 检查 models.video 是否有独立 api_key
	// 待实现：检查视频模型是否已配置 if !dedicatedMultimodalModelConfigured(configBase, "video") {
	// 	logger.Info(logComponent).Msg("跳过video_understanding: config.yaml中models.video无独立api_key")
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
	// 待实现：应用图像生成模型配置 applyImageGenModelConfigFromYAML(configBase)

	if os.Getenv("IMAGE_GEN_API_KEY") == "" {
		logger.Info(logComponent).Msg("image_gen tool skipped: incomplete config (IMAGE_GEN_API_KEY not set)")
		return false
	}
	return true
}

// getToolCards 获取工具卡片列表。
// 对齐 Python: _get_tool_cards(agent_id) (interface_deep.py L2355-2512)
//
// 注意：此方法只负责非 SysOperation 工具（wiki/web_search/vision/audio/video/image_gen/xiaoyi/skill/acp_chat）。
// fs/shell/code 工具由 SysOperationRail 在 CreateDeepAgent 内部自动注册。
//
// Python 步骤对照：
//  1. wiki 工具 (wiki_ingest, wiki_query, wiki_lint)
//  2. 付费搜索工具 (WebPaidSearchTool)
//  3. 免费搜索工具 (WebFreeSearchTool, WebFetchWebpageTool)
//  4. 视觉工具 (create_vision_tools)
//  5. 音频工具 (_iter_runtime_audio_tools)
//  6. 视频工具 (video_understanding)
//  7. 图片生成工具 (generate_image)
//  8. 小艺手机端工具 (28个 xiaoyi_phone_tools)
//  9. SkillToolkit (SkillToolkit.get_tools)
//
// 10. acp_chat (acp_agents 配置检查)
func (d *DeepAdapter) getToolCards(agentID string) []*tool.ToolCard {
	var toolCards []*tool.ToolCard

	// ── 步骤 1: wiki 工具 ──
	// 对齐 Python:
	//   for wtool in [wiki_ingest, wiki_query, wiki_lint]:
	//       if not Runner.resource_mgr.get_tool(wtool.card.id):
	//           Runner.resource_mgr.add_tool(wtool)
	//       tool_cards.append(wtool.card)
	// ⤵️ 9.38-49: wiki_ingest / wiki_query / wiki_lint 工具类尚未实现
	// 待实现：注册Wiki工具 for _, wtool := range []tool.Tool{wikiIngest, wikiQuery, wikiLint} {
	//     if rm.GetTool([]string{wtool.Card().ID}) == nil {
	//         _ = rm.AddTool(wtool)
	//     }
	//     toolCards = append(toolCards, wtool.Card())
	// }

	// ── 步骤 2: 付费搜索工具 ──
	// 对齐 Python:
	//   if is_paid_search_enabled():
	//       self._paid_search_tool = WebPaidSearchTool(language=..., agent_id=agent_id)
	//       Runner.resource_mgr.add_tool(self._paid_search_tool)
	//       tool_cards.append(self._paid_search_tool.card)
	//       self._paid_search_registered = True
	// ⤵️ 9.38-49: WebPaidSearchTool + is_paid_search_enabled() 尚未实现
	// 待实现：付费搜索启用检测 if isPaidSearchEnabled() {
	//     paidSearchTool := NewWebPaidSearchTool(d.resolveRuntimeLanguage(), agentID)
	//     _ = rm.AddTool(paidSearchTool)
	//     toolCards = append(toolCards, paidSearchTool.Card())
	//     d.paidSearchTool = paidSearchTool
	//     d.paidSearchRegistered = true
	// }

	// ── 步骤 3: 免费搜索工具 ──
	// 对齐 Python:
	//   for tool_cls in [WebFreeSearchTool, WebFetchWebpageTool]:
	//       tool_instance = tool_cls(agent_id=agent_id)
	//       Runner.resource_mgr.add_tool(tool_instance)
	//       tool_cards.append(tool_instance.card)
	// ⤵️ 9.38-49: WebFreeSearchTool / WebFetchWebpageTool 尚未实现
	// 待实现：注册免费搜索和网页抓取工具 for _, toolCls := range []func(string) tool.Tool{NewWebFreeSearchTool, NewWebFetchWebpageTool} {
	//     toolInst := toolCls(agentID)
	//     _ = rm.AddTool(toolInst)
	//     toolCards = append(toolCards, toolInst.Card())
	// }

	// ── 步骤 4: 视觉工具 ──
	// 对齐 Python:
	//   if self._vision_model_config is not None:
	//       for tool in create_vision_tools(language=..., vision_model_config=..., agent_id=...):
	//           Runner.resource_mgr.add_tool(tool)
	//           tool_cards.append(tool.card)
	//           self._vision_tools.append(tool)
	//       self._vision_tools_registered = bool(self._vision_tools)
	if d.visionModelConfig != nil {
		// ⤵️ 9.38-49: create_vision_tools() 尚未实现
		// 待实现：创建视觉工具 visionTools := createVisionTools(d.resolveRuntimeLanguage(), d.visionModelConfig, agentID)
		// 待实现：注册视觉工具到ResourceMgr for _, t := range visionTools {
		//     _ = rm.AddTool(t)
		//     toolCards = append(toolCards, t.Card())
		// }
		// d.visionTools = visionTools
		// d.visionToolsRegistered = len(visionTools) > 0
		logger.Info(logComponent).Msg("getToolCards: 视觉模型配置已就绪，等待 9.38-49 回填 create_vision_tools")
	}

	// ── 步骤 5: 音频工具 ──
	// 对齐 Python:
	//   self._audio_tools = self._iter_runtime_audio_tools(agent_id)
	//   for tool in self._audio_tools:
	//       Runner.resource_mgr.add_tool(tool)
	//       tool_cards.append(tool.card)
	//   self._audio_tools_registered = bool(self._audio_tools)
	if d.audioModelConfig != nil {
		// ⤵️ 9.38-49: _iter_runtime_audio_tools() 尚未实现
		// 待实现：迭代音频工具 audioTools := d.iterRuntimeAudioTools(agentID)
		// 待实现：注册音频工具到ResourceMgr for _, t := range audioTools {
		//     _ = rm.AddTool(t)
		//     toolCards = append(toolCards, t.Card())
		// }
		// d.audioTools = audioTools
		// d.audioToolsRegistered = len(audioTools) > 0
		logger.Info(logComponent).Msg("getToolCards: 音频模型配置已就绪，等待 9.38-49 回填 _iter_runtime_audio_tools")
	}

	// ── 步骤 6: 视频工具 ──
	// 对齐 Python:
	//   if self._video_model_config:
	//       Runner.resource_mgr.add_tool(video_understanding)
	//       tool_cards.append(video_understanding.card)
	//       self._video_tool_registered = True
	if d.videoToolRegistered {
		// ⤵️ 9.38-49: video_understanding 工具实例尚未实现
		// 待实现：注册视频理解工具 _ = rm.AddTool(videoUnderstanding)
		// toolCards = append(toolCards, videoUnderstanding.Card())
		logger.Info(logComponent).Msg("getToolCards: 视频工具配置已就绪，等待 9.38-49 回填 video_understanding")
	}

	// ── 步骤 7: 图片生成工具 ──
	// 对齐 Python:
	//   if self._image_gen_model_config:
	//       Runner.resource_mgr.add_tool(generate_image)
	//       tool_cards.append(generate_image.card)
	//       self._image_gen_tool_registered = True
	if d.imageGenToolRegistered {
		// ⤵️ 9.38-49: generate_image 工具实例尚未实现
		// 待实现：注册图像生成工具 _ = rm.AddTool(generateImage)
		// toolCards = append(toolCards, generateImage.Card())
		logger.Info(logComponent).Msg("getToolCards: 图片生成工具配置已就绪，等待 9.38-49 回填 generate_image")
	}

	// ── 步骤 8: 小艺手机端工具 ──
	// 对齐 Python:
	//   xiaoyi_phone_tools_enabled = config_base.get("channels", {}).get("xiaoyi", {}).get("phone_tools_enabled", False)
	//   if xiaoyi_phone_tools_enabled and not self._xiaoyi_phone_tools_registered:
	//       _xiaoyi_tools = [get_user_location, create_note, search_notes, ...]
	//       for xt in _xiaoyi_tools:
	//           Runner.resource_mgr.add_tool(xt)
	//           tool_cards.append(xt.card)
	//       self._xiaoyi_phone_tools_registered = True
	// ⤵️ 9.38-49: 小艺手机端工具类 (28个) 尚未实现
	// 待实现：检查小艺手机端工具是否启用 xiaoyiEnabled := false
	// if channels, ok := configBase["channels"].(map[string]any); ok {
	//     if xiaoyi, ok := channels["xiaoyi"].(map[string]any); ok {
	//         if v, ok := xiaoyi["phone_tools_enabled"].(bool); ok {
	//             xiaoyiEnabled = v
	//         }
	//     }
	// }
	// if xiaoyiEnabled && !d.xiaoyiPhoneToolsRegistered {
	//     xiaoyiTools := []tool.Tool{getUserLocation, createNote, searchNotes, ...}
	//     for _, xt := range xiaoyiTools {
	//         _ = rm.AddTool(xt)
	//         toolCards = append(toolCards, xt.Card())
	//     }
	//     d.xiaoyiPhoneToolsRegistered = true
	// }

	// ── 步骤 9: SkillToolkit ──
	// 对齐 Python:
	//   skill_toolkit = SkillToolkit(manager=self._skill_manager)
	//   for tool in skill_toolkit.get_tools():
	//       if not Runner.resource_mgr.get_tool(tool.card.id):
	//           Runner.resource_mgr.add_tool(tool)
	//       tool_cards.append(tool.card)
	if d.skillManager != nil {
		// ⤵️ 9.38-49: SkillToolkit 工具尚未实现
		// 待实现：创建技能工具包 skillToolkit := NewSkillToolkit(d.skillManager)
		// 待实现：注册技能工具 for _, t := range skillToolkit.GetTools() {
		//     if rm.GetTool([]string{t.Card().ID}) == nil {
		//         _ = rm.AddTool(t)
		//     }
		//     toolCards = append(toolCards, t.Card())
		// }
		logger.Info(logComponent).Msg("getToolCards: SkillManager 已就绪，等待 9.38-49 回填 SkillToolkit")
	}

	// ── 步骤 10: acp_chat ──
	// 对齐 Python:
	//   acp_cfg = get_config().get("acp_agents")
	//   if isinstance(acp_cfg, dict) and acp_cfg:
	//       if not Runner.resource_mgr.get_tool(acp_chat.card.id):
	//           Runner.resource_mgr.add_tool(acp_chat)
	//       tool_cards.append(acp_chat.card)
	// ⤵️ 9.38-49: acp_chat 工具尚未实现
	// 待实现：ACP配置检查 acpCfg, _ := configBase["acp_agents"].(map[string]any)
	// if len(acpCfg) > 0 {
	//     if rm.GetTool([]string{acpChat.Card().ID}) == nil {
	//         _ = rm.AddTool(acpChat)
	//     }
	//     toolCards = append(toolCards, acpChat.Card())
	// }

	// 优先付费搜索
	toolCards = d.prioritizePaidSearchToolCard(toolCards)

	logger.Info(logComponent).
		Str("agent_id", agentID).
		Int("tool_count", len(toolCards)).
		Msg("getToolCards 完成")

	return toolCards
}

// ──────────────────────────── AbilityManager 辅助 ────────────────────────────

// getAbilityManager 获取当前实例的 AbilityManager。
func (d *DeepAdapter) getAbilityManager() sainterfaces.AbilityManagerInterface {
	if d.instance == nil {
		return nil
	}
	reactAgent := d.instance.ReactAgent()
	if reactAgent == nil {
		return nil
	}
	return reactAgent.AbilityManager()
}

// syncToolsToManager 将工具同步到 AbilityManager 和 ResourceMgr。
// 内部辅助方法，供 syncToolGroup 和 removeRegisteredTools 使用。
//
// toolCards 用于 AbilityManager.Add（需要 schema.Ability），
// toolInstances 用于 ResourceMgr.AddTool（需要 tool.Tool）。
func (d *DeepAdapter) syncToolsToManager(ctx context.Context, toolCards []*tool.ToolCard, toolInstances []tool.Tool, toRemove []string, tag string) {
	am := d.getAbilityManager()
	rm := runner.GetResourceMgr()

	// 添加工具卡片到 AbilityManager
	for _, tc := range toolCards {
		if am != nil {
			am.Add(tc)
		}
	}

	// 添加工具实例到 ResourceMgr
	for _, t := range toolInstances {
		if tag != "" {
			_ = rm.AddTool(t, resources_manager.WithTag(resources_manager.Tag(tag)))
		} else {
			_ = rm.AddTool(t)
		}
	}

	// 移除工具
	if len(toRemove) > 0 {
		if am != nil {
			am.RemoveMany(toRemove)
		}
		if tag != "" {
			_, _ = rm.RemoveTool(toRemove, resources_manager.WithTag(resources_manager.Tag(tag)))
		} else {
			_, _ = rm.RemoveTool(toRemove)
		}
	}
}
