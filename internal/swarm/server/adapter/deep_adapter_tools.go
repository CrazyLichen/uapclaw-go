package adapter

import (
	"context"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/multimodal"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/web_tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	skilltools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/tools"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// 工具名称常量，对齐 Python tool_cards 中的工具名。
// 工具名称常量
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

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

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
	// ⤵️ 10.6.24: 根据 toolGroup 解析 configBase 中的工具配置
	var toolInstancesToAdd []tool.Tool  // ResourceMgr.AddTool 需要 tool.Tool
	var toolCardsToAdd []*tool.ToolCard // AbilityManager.Add 需要 schema.Ability（*ToolCard 实现）
	var toolIDsToRemove []string
	_ = toolInstancesToAdd // ⤵️ 10.6.24: 从 configBase 解析待添加的工具实例
	_ = toolCardsToAdd     // ⤵️ 10.6.24: 从 configBase 解析待添加的工具卡片
	_ = toolIDsToRemove    // ⤵️ 10.6.24: 从 AbilityManager 查询当前已注册的同组工具

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
// 接收工具实例列表，从实例上分别取 card.id 和 card.name，
// 传给 ResourceMgr.remove_tool(card.id) 和 AbilityManager.remove(card.name)。
func (d *DeepAdapter) removeRegisteredTools(tools []tool.Tool) {
	if len(tools) == 0 {
		return
	}

	am := d.getAbilityManager()
	rm := runner.GetResourceMgr()

	for _, t := range tools {
		card := t.Card()
		// 对齐 Python: Runner.resource_mgr.remove_tool(tool.card.id)
		if rm != nil {
			if _, err := rm.RemoveTool([]string{card.ID}); err != nil {
				logger.Warn(logComponent).Err(err).Str("card_id", card.ID).Msg("RemoveTool 从 ResourceMgr 失败")
			}
		}
		// 对齐 Python: self._instance.ability_manager.remove(tool.card.name)
		if am != nil {
			am.Remove(card.Name)
		}
	}

	logger.Info(logComponent).Int("count", len(tools)).Msg("removeRegisteredTools 完成")
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
// Vision/Audio/Video 注册/注销已回填，image_gen 待后续回填
func (d *DeepAdapter) syncMultimodalToolsForRuntime(ctx context.Context) {
	if d.instance == nil {
		return
	}
	reactAgent := d.instance.ReactAgent()
	if reactAgent == nil {
		return
	}

	// ── 视觉工具同步 ──
	// 对齐 Python: Python: self._vision_tools, self._vision_tools_registered = self._sync_tool_group(
	//   Python: current_tools=self._vision_tools, registered=self._vision_tools_registered,
	//   Python: enabled=self._vision_model_config is not None, create_fn=..., warn_label="vision tools")
	if d.visionModelConfig != nil && !d.visionToolsRegistered {
		client := d.resolveVisionModelClient()
		visionTools := multimodal.CreateVisionTools(client, d.visionModelConfig, d.resolveRuntimeLanguage(), "")
		cards := make([]*tool.ToolCard, len(visionTools))
		for i, t := range visionTools {
			cards[i] = t.Card()
		}
		d.syncToolsToManager(ctx, cards, visionTools, nil, "vision")
		d.visionTools = visionTools // 对齐 Python: self._vision_tools = tools
		d.visionToolsRegistered = true
	}
	if d.visionModelConfig == nil && d.visionToolsRegistered {
		d.removeRegisteredTools(d.visionTools) // 对齐 Python: 传完整工具实例列表
		// 对齐 Python: self._prune_tool_cards({t.card.name for t in current_tools})
		namesToRemove := make(map[string]bool, len(d.visionTools))
		for _, t := range d.visionTools {
			namesToRemove[t.Card().Name] = true
		}
		d.toolCards = d.pruneToolCards(d.toolCards, namesToRemove)
		d.visionTools = nil
		d.visionToolsRegistered = false
	}

	// ── 音频工具同步 ──
	// 对齐 Python: _iter_runtime_audio_tools 三种情况:
	//   1. 无 api_key → 不注册任何音频工具
	//   2. 有 api_key 但 audio_model_config 为 None → 仅注册 audio_metadata
	//   3. 有完整配置 → 全部音频工具
	audioDedicated := DedicatedMultimodalModelConfigured(d.configCache, "audio")
	if !audioDedicated {
		// 情况1: 无独立音频 key → 不注册任何音频工具
		if d.audioToolsRegistered {
			d.removeRegisteredTools(d.audioTools)
			namesToRemove := make(map[string]bool, len(d.audioTools))
			for _, t := range d.audioTools {
				namesToRemove[t.Card().Name] = true
			}
			d.toolCards = d.pruneToolCards(d.toolCards, namesToRemove)
			d.audioTools = nil
			d.audioToolsRegistered = false
		}
	} else if d.audioModelConfig == nil {
		// 情况2: 有独立 key 但无 model config → 仅注册 audio_metadata（metadata-only）
		// 对齐 Python: _iter_runtime_audio_tools 在 audio_model_config=None 时
		// 调用 create_audio_tools(audio_model_config=None) 然后过滤出 audio_metadata
		if !d.audioToolsRegistered {
			// 对齐 Python: create_audio_tools(audio_model_config=None) → 过滤 audio_metadata
			// 注意：Python 此模式下 audio_metadata 调用时 config=None 会报错，
			// Go 保持一致行为：创建工具但 config 不完整时调用会返回错误
			metadataTool := multimodal.NewAudioMetadataTool(nil, nil, d.resolveRuntimeLanguage(), "")
			d.syncToolsToManager(ctx, []*tool.ToolCard{metadataTool.Card()}, []tool.Tool{metadataTool}, nil, "audio")
			d.audioTools = []tool.Tool{metadataTool}
			d.audioToolsRegistered = true
			logger.Info(logComponent).Msg("音频工具: metadata-only 模式（跳过 audio_transcription & audio_question_answering: incomplete audio LLM config）")
		}
	} else {
		// 情况3: 完整配置 → 全部音频工具
		if !d.audioToolsRegistered {
			client := d.resolveAudioModelClient()
			audioTools := multimodal.CreateAudioTools(client, d.audioModelConfig, d.resolveRuntimeLanguage(), "")
			cards := make([]*tool.ToolCard, len(audioTools))
			for i, t := range audioTools {
				cards[i] = t.Card()
			}
			d.syncToolsToManager(ctx, cards, audioTools, nil, "audio")
			d.audioTools = audioTools // 对齐 Python: self._audio_tools = tools
			d.audioToolsRegistered = true
		}
	}

	// ── 视频工具同步 ──
	if d.videoModelConfig != nil && !d.videoToolRegistered {
		client := d.resolveVideoModelClient()
		videoTool := multimodal.NewVideoUnderstandingTool(client, d.videoModelConfig, d.resolveRuntimeLanguage(), "")
		d.syncToolsToManager(ctx, []*tool.ToolCard{videoTool.Card()}, []tool.Tool{videoTool}, nil, "video")
		d.videoTool = videoTool // 对齐 Python: self._video_tools = [video_understanding]
		d.videoToolRegistered = true
	}
	if d.videoModelConfig == nil && d.videoToolRegistered {
		d.removeRegisteredTools([]tool.Tool{d.videoTool}) // 对齐 Python: 传完整工具实例列表
		d.toolCards = d.pruneToolCards(d.toolCards, map[string]bool{d.videoTool.Card().Name: true})
		d.videoTool = nil
		d.videoToolRegistered = false
	}

	// 图片生成工具同步
	if d.imageGenToolRegistered {
		// TODO(#10.6.24): ⤵️ 确保图片生成工具已注册
		_ = d.imageGenToolRegistered
	}
}

// syncPaidSearchToolForRuntime 热同步付费搜索工具。
// 对齐 Python: _sync_paid_search_tool_for_runtime() (line 1240-1270)
func (d *DeepAdapter) syncPaidSearchToolForRuntime() {
	if d.instance == nil {
		return
	}

	rm := runner.GetResourceMgr()
	if d.paidSearchRegistered {
		// 注册 paid_search 工具
		if d.paidSearchTool == nil {
			d.paidSearchTool = web_tools.NewWebPaidSearchTool(d.resolveRuntimeLanguage(), d.agentName)
		}
		if rm != nil {
			if err := rm.AddTool(d.paidSearchTool); err != nil {
				logger.Warn(logComponent).Err(err).Msg("注册付费搜索工具到 ResourceMgr 失败")
			}
		}
	} else {
		// 移除 paid_search 工具
		if d.paidSearchTool != nil {
			if rm != nil {
				if _, err := rm.RemoveTool([]string{d.paidSearchTool.Card().ID}); err != nil {
					logger.Warn(logComponent).Err(err).Msg("移除付费搜索工具从 ResourceMgr 失败")
				}
			}
		}
	}
	logger.Info(logComponent).Bool("registered", d.paidSearchRegistered).Msg("syncPaidSearchToolForRuntime 完成")
}

// refreshMultimodalConfigs 刷新多模态配置。
// 对齐 Python: _refresh_multimodal_configs(config_base) (line 1304-1317)
// 注意：Python 不在此方法中设置 _video_tool_registered / _vision_tools_registered / _audio_tools_registered，
// 注册状态由 _sync_multimodal_tools_for_runtime 管理。
func (d *DeepAdapter) refreshMultimodalConfigs(configBase map[string]any) {
	d.visionModelConfig = d.buildVisionModelConfig(configBase)
	d.audioModelConfig = d.buildAudioModelConfig(configBase)
	d.videoModelConfig = d.buildVideoModelConfig(configBase)
	// 对齐 Python: 不在此处设置 registered 标志，由 syncMultimodalToolsForRuntime 管理
	// 对齐 Python: 将 config 同步到已有工具实例
	for _, t := range d.visionTools {
		if setter, ok := t.(interface {
			SetVisionModelConfig(*schema.VisionModelConfig)
		}); ok {
			setter.SetVisionModelConfig(d.visionModelConfig)
		}
	}
	for _, t := range d.audioTools {
		if setter, ok := t.(interface {
			SetAudioModelConfig(*schema.AudioModelConfig)
		}); ok {
			setter.SetAudioModelConfig(d.audioModelConfig)
		}
	}
}

// buildVisionModelConfig 从配置构建视觉模型配置。
// 对齐 Python: _build_vision_model_config(config_base) (line 1171-1193)
//
// 链路: 先 dedicated_multimodal_model_configured 门控 → 再 apply_vision_model_config_from_yaml → 从 env 读取
func (d *DeepAdapter) buildVisionModelConfig(configBase map[string]any) *schema.VisionModelConfig {
	// 1. 先检查 models.vision 是否有独立 api_key（对齐 Python 先检查再 apply 的顺序）
	if !DedicatedMultimodalModelConfigured(configBase, "vision") {
		logger.Info(logComponent).Msg("跳过 vision tools: config.yaml 中 models.vision 无独立 api_key")
		return nil
	}

	// 2. YAML 配置映射到环境变量
	ApplyVisionModelConfigFromYAML(configBase)

	// 3. 从环境变量构建 VisionModelConfig
	cfg := schema.VisionModelConfig{}.FromEnv()
	if cfg.APIKey == "" || cfg.BaseURL == "" || cfg.Model == "" {
		logger.Info(logComponent).Msg("vision tools 跳过: 配置不完整")
		return nil
	}
	return &cfg
}

// buildAudioModelConfig 从配置构建音频模型配置。
// 对齐 Python: _build_audio_model_config(config_base) (line 1196-1241)
//
// 链路: 先 dedicated_multimodal_model_configured 门控 → 再 apply_audio_model_config_from_yaml → 从 env 读取
func (d *DeepAdapter) buildAudioModelConfig(configBase map[string]any) *schema.AudioModelConfig {
	// 1. 先检查 models.audio 是否有独立 api_key（对齐 Python 先检查再 apply 的顺序）
	if !DedicatedMultimodalModelConfigured(configBase, "audio") {
		logger.Info(logComponent).Msg("跳过 audio tools: config.yaml 中 models.audio 无独立 api_key")
		return nil
	}

	// 2. YAML 配置映射到环境变量
	ApplyAudioModelConfigFromYAML(configBase)

	// 3. 从环境变量构建 AudioModelConfig
	cfg := schema.AudioModelConfig{}.FromEnv()
	if cfg.APIKey == "" || cfg.BaseURL == "" {
		logger.Info(logComponent).Msg("audio tools 跳过: 配置不完整")
		return nil
	}
	return &cfg
}

// buildVideoModelConfig 构建视频模型配置。
// 对齐 Python: _build_video_model_config(config_base) (line 1244-1260)
//
// 注意：此处 Apply 先于 Dedicated 检查，与 vision/audio 的 Dedicated 先于 Apply 不同。
// 这是 Python 本身的设计差异（Go 忠实复刻），Apply 先执行意味着即使 Dedicated 检查失败，
// 环境变量也已被设置。vision/audio 的 Apply 在 Dedicated 检查失败时不会执行。
func (d *DeepAdapter) buildVideoModelConfig(configBase map[string]any) *schema.VideoModelConfig {
	// 1. YAML 配置映射到环境变量
	ApplyVideoModelConfigFromYAML(configBase)

	// 2. 检查 models.video 是否有独立 api_key
	if !DedicatedMultimodalModelConfigured(configBase, "video") {
		logger.Info(logComponent).Msg("跳过 video_understanding: config.yaml 中 models.video 无独立 api_key")
		return nil
	}

	// 3. 从环境变量构建 VideoModelConfig
	cfg := schema.VideoModelConfig{}.FromEnv()
	if cfg.APIKey == "" {
		logger.Info(logComponent).Msg("视频工具跳过: 配置不完整 (VIDEO_API_KEY 未设置)")
		return nil
	}
	return &cfg
}

// buildImageGenModelConfig 构建图片生成模型配置。
// 对齐 Python: _build_image_gen_model_config(config_base) (line 1261-1270)
// 返回 bool 表示图片生成工具是否启用（Python 原实现返回 bool，通过环境变量传递配置）。
// ⤵️ 10.6.24 Swarm 内置工具集: apply_image_gen_model_config_from_yaml
func (d *DeepAdapter) buildImageGenModelConfig(configBase map[string]any) bool {
	// ⤵️ 10.6.24: apply_image_gen_model_config_from_yaml(configBase) — 将 YAML 配置映射到环境变量
	// 待实现：应用图像生成模型配置 applyImageGenModelConfigFromYAML(configBase)

	if os.Getenv("IMAGE_GEN_API_KEY") == "" {
		logger.Info(logComponent).Msg("图片生成工具跳过: 配置不完整 (IMAGE_GEN_API_KEY 未设置)")
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
//  9. SkillToolkit（SkillToolkit.get_tools 获取技能工具）
//
// 10. acp_chat (acp_agents 配置检查)
func (d *DeepAdapter) getToolCards(agentID string) []*tool.ToolCard {
	var toolCards []*tool.ToolCard

	// ── 步骤 1: wiki 工具 ──
	// 对齐 Python:
	//   Python: for wtool in [wiki_ingest, wiki_query, wiki_lint]:
	//       Python: if not Runner.resource_mgr.get_tool(wtool.card.id):
	//           Python: Runner.resource_mgr.add_tool(wtool)
	//       Python: tool_cards.append(wtool.card)
	// ⤵️ 10.6.24: wiki_ingest / wiki_query / wiki_lint 工具类尚未实现
	// 待实现：注册Wiki工具 for _, wtool := range []tool.Tool{wikiIngest, wikiQuery, wikiLint} {
	//     if rm.GetTool([]string{wtool.Card().ID}) == nil {
	//         _ = rm.AddTool(wtool)
	//     }
	//     toolCards = append(toolCards, wtool.Card())
	// }

	// ── 步骤 2: 付费搜索工具 ──
	// 对齐 Python:
	//   Python: if is_paid_search_enabled():
	//       Python: self._paid_search_tool = WebPaidSearchTool(language=..., agent_id=agent_id)
	//       Python: Runner.resource_mgr.add_tool(self._paid_search_tool)
	//       Python: tool_cards.append(self._paid_search_tool.card)
	//       self._paid_search_registered = True
	if web_tools.IsPaidSearchEnabled() {
		paidSearchTool := web_tools.NewWebPaidSearchTool(d.resolveRuntimeLanguage(), agentID)
		if err := runner.GetResourceMgr().AddTool(paidSearchTool); err != nil {
			logger.Warn(logComponent).Err(err).Msg("注册付费搜索工具到 ResourceMgr 失败")
		}
		toolCards = append(toolCards, paidSearchTool.Card())
		d.paidSearchTool = paidSearchTool
		d.paidSearchRegistered = true
	}

	// ── 步骤 3: 免费搜索工具 ──
	// 对齐 Python:
	//   Python: for tool_cls in [WebFreeSearchTool, WebFetchWebpageTool]:
	//       Python: tool_instance = tool_cls(agent_id=agent_id)
	//       Python: Runner.resource_mgr.add_tool(tool_instance)
	//       Python: tool_cards.append(tool_instance.card)
	for _, toolCls := range []func(string, string) tool.Tool{
		web_tools.NewWebFreeSearchTool,
		web_tools.NewWebFetchWebpageTool,
	} {
		toolInst := toolCls(d.resolveRuntimeLanguage(), agentID)
		if err := runner.GetResourceMgr().AddTool(toolInst); err != nil {
			logger.Warn(logComponent).Err(err).Str("tool_name", toolInst.Card().Name).Msg("注册搜索工具到 ResourceMgr 失败")
		}
		toolCards = append(toolCards, toolInst.Card())
	}

	// ── 步骤 4: 视觉工具 ──
	// 对齐 Python:
	//   Python: if self._vision_model_config is not None:
	//       Python: for tool in create_vision_tools(language=..., vision_model_config=..., agent_id=...):
	//           Python: Runner.resource_mgr.add_tool(tool)
	//           Python: tool_cards.append(tool.card)
	//           Python: self._vision_tools.append(tool)
	//       Python: self._vision_tools_registered = bool(self._vision_tools)
	if d.visionModelConfig != nil {
		client := d.resolveVisionModelClient()
		visionTools := multimodal.CreateVisionTools(client, d.visionModelConfig, d.resolveRuntimeLanguage(), agentID)
		for _, t := range visionTools {
			if err := runner.GetResourceMgr().AddTool(t); err != nil {
				logger.Warn(logComponent).Err(err).Msg("注册 vision 工具到 ResourceMgr 失败")
			}
			toolCards = append(toolCards, t.Card())
		}
		d.visionTools = visionTools // 对齐 Python: self._vision_tools = tools
		d.visionToolsRegistered = len(visionTools) > 0
	}

	// ── 步骤 5: 音频工具 ──
	// 对齐 Python: _iter_runtime_audio_tools 三种情况:
	//   1. 无 api_key → 不注册任何音频工具
	//   2. 有 api_key 但 audio_model_config 为 None → 仅注册 audio_metadata
	//   3. 有完整配置 → 全部音频工具
	audioDedicated := DedicatedMultimodalModelConfigured(d.configCache, "audio")
	if !audioDedicated {
		// 情况1: 无独立音频 key → 不注册任何音频工具
	} else if d.audioModelConfig == nil {
		// 情况2: 有独立 key 但无 model config → 仅注册 audio_metadata（metadata-only）
		metadataTool := multimodal.NewAudioMetadataTool(nil, nil, d.resolveRuntimeLanguage(), agentID)
		if err := runner.GetResourceMgr().AddTool(metadataTool); err != nil {
			logger.Warn(logComponent).Err(err).Msg("注册 audio_metadata 工具到 ResourceMgr 失败")
		}
		toolCards = append(toolCards, metadataTool.Card())
		d.audioTools = []tool.Tool{metadataTool}
		d.audioToolsRegistered = true
		logger.Info(logComponent).Msg("getToolCards: 音频工具 metadata-only 模式")
	} else {
		// 情况3: 完整配置 → 全部音频工具
		client := d.resolveAudioModelClient()
		audioTools := multimodal.CreateAudioTools(client, d.audioModelConfig, d.resolveRuntimeLanguage(), agentID)
		for _, t := range audioTools {
			if err := runner.GetResourceMgr().AddTool(t); err != nil {
				logger.Warn(logComponent).Err(err).Msg("注册 audio 工具到 ResourceMgr 失败")
			}
			toolCards = append(toolCards, t.Card())
		}
		d.audioTools = audioTools // 对齐 Python: self._audio_tools = tools
		d.audioToolsRegistered = len(audioTools) > 0
	}

	// ── 步骤 6: 视频工具 ──
	// 对齐 Python:
	//   Python: if self._video_model_config:
	//       Python: Runner.resource_mgr.add_tool(video_understanding)
	//       Python: tool_cards.append(video_understanding.card)
	//       Python: self._video_tool_registered = True
	if d.videoModelConfig != nil {
		client := d.resolveVideoModelClient()
		videoTool := multimodal.NewVideoUnderstandingTool(client, d.videoModelConfig, d.resolveRuntimeLanguage(), agentID)
		if err := runner.GetResourceMgr().AddTool(videoTool); err != nil {
			logger.Warn(logComponent).Err(err).Msg("注册 video_understanding 到 ResourceMgr 失败")
		}
		toolCards = append(toolCards, videoTool.Card())
		d.videoTool = videoTool // 对齐 Python: self._video_tools = [video_understanding]
		d.videoToolRegistered = true
	}

	// ── 步骤 7: 图片生成工具 ──
	// 对齐 Python:
	//   Python: if self._image_gen_model_config:
	//       Python: Runner.resource_mgr.add_tool(generate_image)
	//       Python: tool_cards.append(generate_image.card)
	//       Python: self._image_gen_tool_registered = True
	if d.imageGenToolRegistered {
		// ⤵️ 10.6.24: generate_image 工具实例尚未实现
		// 待实现：注册图像生成工具 _ = rm.AddTool(generateImage)
		// toolCards = append(toolCards, generateImage.Card())
		logger.Info(logComponent).Msg("getToolCards: 图片生成工具配置已就绪，等待 10.6.24 回填 generate_image")
	}

	// ── 步骤 8: 小艺手机端工具 ──
	// 对齐 Python:
	//   Python: xiaoyi_phone_tools_enabled = config_base.get("channels", {}).get("xiaoyi", {}).get("phone_tools_enabled", False)
	//   Python: if xiaoyi_phone_tools_enabled and not self._xiaoyi_phone_tools_registered:
	//    Python: _xiaoyi_tools 工具列表（get_user_location, create_note, search_notes 等）
	//       Python: for xt in _xiaoyi_tools:
	//           Python: Runner.resource_mgr.add_tool(xt)
	//           Python: tool_cards.append(xt.card)
	//       Python: self._xiaoyi_phone_tools_registered = True
	// ⤵️ 10.6.24: 小艺手机端工具类 (28个) 尚未实现
	// 待实现：检查小艺手机端工具是否启用 xiaoyiEnabled := false
	// if channels, ok := configBase["channels"].(map[string]any); ok {
	//     if xiaoyi, ok := channels["xiaoyi"].(map[string]any); ok {
	//         if v, ok := xiaoyi["phone_tools_enabled"].(bool); ok {
	//             xiaoyiEnabled = v
	//         }
	//     }
	// }
	// 如果 xiaoyi 已启用但尚未注册 xiaoyi 手机工具
	//     xiaoyiTools := []tool.Tool{getUserLocation, createNote, searchNotes, ...}
	//     for _, xt := range xiaoyiTools {
	//         _ = rm.AddTool(xt)
	//         toolCards = append(toolCards, xt.Card())
	//     }
	//     d.xiaoyiPhoneToolsRegistered = true
	// }

	// ── 步骤 9: SkillToolkit ──
	// 对齐 Python:
	//   Python: skill_toolkit = SkillToolkit(manager=self._skill_manager)
	//   Python: for tool in skill_toolkit.get_tools():
	//       Python: if not Runner.resource_mgr.get_tool(tool.card.id):
	//           Python: Runner.resource_mgr.add_tool(tool)
	//       Python: tool_cards.append(tool.card)
	if d.skillManager != nil {
		skillToolkit := skilltools.NewSkillToolkit(d.skillManager)
		for _, t := range skillToolkit.GetTools() {
			existing, _ := runner.GetResourceMgr().GetTool([]string{t.Card().ID})
			if len(existing) == 0 {
				_ = runner.GetResourceMgr().AddTool(t)
			}
			toolCards = append(toolCards, t.Card())
		}
		logger.Info(logComponent).Msg("getToolCards: SkillToolkit 已注册")
	}

	// ── 步骤 10: acp_chat ──
	// 对齐 Python:
	//   Python: acp_cfg = get_config().get("acp_agents")
	//   Python: if isinstance(acp_cfg, dict) and acp_cfg:
	//       Python: if not Runner.resource_mgr.get_tool(acp_chat.card.id):
	//           Python: Runner.resource_mgr.add_tool(acp_chat)
	//       Python: tool_cards.append(acp_chat.card)
	// ⤵️ 10.6.24: acp_chat 工具尚未实现
	// 待实现：ACP配置检查 acpCfg, _ := configBase["acp_agents"].(map[string]any)
	// 如果存在 ACP 配置
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
