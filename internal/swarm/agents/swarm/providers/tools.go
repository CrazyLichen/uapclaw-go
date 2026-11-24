package providers

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	discovery16 "github.com/uapclaw/uapclaw-go/internal/agentcore/symphony/discovery"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	teamtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	agenttoolkits "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/tools"
	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// tools.go —— swarm 工具工厂（~20 个 build_*）+ 可见性过滤 + 多模态参数段。
//
// Python: jiuwenswarm/agents/swarm/providers/tools.py（687 行）。
// 常量值由 registry.go 常量层承载；多模态配置参数段（:270-355）自
// providers_tools.go（e10③ scoped 拆分壳）迁入——规则 8 回收。
//
// 基座缺口：xiaoyi 28 工具（⤴️10.6.24e）、
// symphony toolkit（⤴️领域十六）、a4p tools（⤴️15.20/领域）、visual_gen
// （Python visual_gen_enabled/configured 无 Go 基座 ⤴️15.20）、
// mark_stateless（Go tool.Tool 无无状态标志位 ⤴️15.20 登记偏差）。

// ──────────────────────────── 常量 ────────────────────────────

const (
	constSkillToolkit           = "swarm.skill_toolkit"
	constSkillRetrieval         = "swarm.skill_retrieval"
	constUserTodos              = "swarm.user_todos"
	constVideo                  = "swarm.video"
	constImageGen               = "swarm.image_gen"
	constVideoGen               = "swarm.video_gen"
	constVisualGen              = "swarm.visual_gen"
	constXiaoyiPhone            = "swarm.xiaoyi_phone"
	constSymphonyToolkit        = "swarm.symphony_toolkit"
	constA4PIntentAuthorization = "swarm.a4p_intent_authorization"
	constCodeExtraTools         = "swarm.code_extra_tools"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// filterWhitelist 只保留 card.Name 在 swarm TOOL_WHITELIST 里的工具。
// 白名单单源：team 包 toolWhitelist（runtime_inheritance.go:106，对齐
// Python TOOL_WHITELIST L81-126）——经 toolWhitelistNames() 导出访问。
// Python: _filter_whitelist (tools.py:161-164)
func filterWhitelist(tools []tool.Tool) []tool.Tool {
	out := make([]tool.Tool, 0, len(tools))
	for _, t := range tools {
		if toolWhitelistNames()[t.Card().Name] {
			out = append(out, t)
		}
	}
	return out
}

// markStatelessToo 模块级单例工具的无状态标记。
// Python: _mark_stateless (tools.py:166-175)——Go tool.Tool 无无状态标志位，
// 偏差登记 ⤴️15.20；当前原样返回（共享 id 改写行为由 harness 侧现状承担）。
func markStatelessToo(tools []tool.Tool) []tool.Tool {
	return tools
}

// skillLibraryDir 本上下文读取的唯一物理 Skill 库。
// Python: _skill_library_dir (tools.py:182-188)：global_skills_dir 优先，
// 缺省 get_agent_skills_dir()（15.16c paths.go GlobalSkillsDir）。
func skillLibraryDir(view *buildctx.SwarmBuildContext) string {
	if raw := view.GlobalSkillsDir; raw != "" {
		return raw
	}
	return agentpathsGlobalSkillsDir()
}

// scanLibrarySkillNames 库内全部已安装 Skill 名（无视可见性）。
// 目录含 SKILL.md 计为 Skill；`_`/`.` 前缀目录是内部物（状态文件、锁），
// 永不是 Skill。
// Python: _scan_library_skill_names (tools.py:190-213)
func scanLibrarySkillNames(libraryDir string) map[string]struct{} {
	names := make(map[string]struct{})
	entries, err := os.ReadDir(libraryDir)
	if err != nil {
		return names
	}
	for _, child := range entries {
		if !child.IsDir() || strings.HasPrefix(child.Name(), "_") || strings.HasPrefix(child.Name(), ".") {
			continue
		}
		if stat, err := os.Stat(filepath.Join(libraryDir, child.Name(), "SKILL.md")); err == nil && !stat.IsDir() {
			names[child.Name()] = struct{}{}
		}
	}
	return names
}

// applySkillVisibility 按 SkillUseRail 同款方式过滤名字集合。
// Python: _apply_skill_visibility (tools.py:214-237)
func applySkillVisibility(names map[string]struct{}, enabledSkills, disabledSkills map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(names))
	for name := range names {
		if _, disabled := disabledSkills[name]; disabled {
			continue
		}
		if _, ok := enabledSkills[name]; ok {
			out[name] = struct{}{}
		}
	}
	return out
}

// visibleSkillNamesForListSkill list_skill 可见 Skill 名。
// Python: visible_skill_names_for_list_skill (tools.py:238-260)——读
// team_skill_visibility_path 的 enabled/disabled 集合再过滤扫描结果。
func visibleSkillNamesForListSkill(view *buildctx.SwarmBuildContext) map[string]struct{} {
	names := scanLibrarySkillNames(skillLibraryDir(view))
	enabled, disabled := loadSkillVisibilitySets(view)
	return applySkillVisibility(names, enabled, disabled)
}

// loadSkillVisibilitySets 读 skills-visibility.json 的 enabled/disabled。
// Python: load_execution_disabled_skills / visibility 读取段（skills.py 侧）；
// JSON 读写基座随 skills.go（Task 5 同批）。
func loadSkillVisibilitySets(view *buildctx.SwarmBuildContext) (map[string]struct{}, map[string]struct{}) {
	enabled := make(map[string]struct{})
	disabled := make(map[string]struct{})
	path := view.TeamSkillVisibilityPath
	if path == "" {
		return enabled, disabled
	}
	readSkillVisibilityFile(path, enabled, disabled)
	return enabled, disabled
}

// buildSkillToolkitTools 构建绑定共享 agent 工作区的技能管理工具。
//
// 安装落在唯一物理 Skill 库，管理器以共享 agent 工作区为根——成员工作区
// 只持有可见性文档，永不是 Skill 源。构造失败 warn + 空表。
// Python: _build_skill_toolkit_tools (tools.py:362-378)
func buildSkillToolkitTools() []tool.Tool {
	// Python: SkillManager(workspace_dir=str(get_agent_workspace_dir()))
	manager := skill.NewSkillManager(agentWorkspaceDir())
	toolkit := agenttoolkits.NewSkillToolkit(manager)
	return toolkit.GetTools()
}

// buildUserTodoTools 用户个人 todo 工具。
// Python: _build_user_todo_tools (tools.py:415-422)
func buildUserTodoTools() []tool.Tool {
	// Python: _mark_stateless(list(get_decorated_tools()))
	return markStatelessToo(getDecoratedTools())
}

// buildVideoTools models.video 完整时构建视频理解工具。
// Python: _build_video_tools (tools.py:424-440)
func buildVideoTools(view *buildctx.SwarmBuildContext) []tool.Tool {
	config := view.Config
	if config == nil {
		config = map[string]any{}
	}
	if !teamtools.MultimodalModelEnabled(config, "video") ||
		!teamtools.CompleteMultimodalModelConfigured(config, "video") {
		return nil
	}
	teamtools.ApplyVideoModelConfigFromYAML(config)
	videoAPIKey := strings.TrimSpace(os.Getenv("VIDEO_API_KEY"))
	videoAPIBase := strings.TrimSpace(os.Getenv("VIDEO_API_BASE"))
	videoModelName := strings.TrimSpace(os.Getenv("VIDEO_MODEL_NAME"))
	if videoAPIKey == "" || videoAPIBase == "" || videoModelName == "" {
		return nil
	}
	return markStatelessToo([]tool.Tool{teamtools.CreateVideoUnderstandingTool()})
}

// buildImageGenTools models.image_gen 配置时构建图片生成工具。
// Python: _build_image_gen_tools (tools.py:441-448)
func buildImageGenTools(view *buildctx.SwarmBuildContext) []tool.Tool {
	config := view.Config
	if config == nil {
		config = map[string]any{}
	}
	teamtools.ApplyImageGenModelConfigFromYAML(config)
	if os.Getenv("IMAGE_GEN_API_KEY") == "" {
		return nil
	}
	return markStatelessToo([]tool.Tool{newGenerateImageToolPtr()})
}

// buildVideoGenTools 开启且 Video Model 配置完整时构建视频生成工具组。
// Python: _build_video_gen_tools (tools.py:449-458)
func buildVideoGenTools(view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = view
	if !teamtools.VideoGenEnabled() || !teamtools.VideoGenConfigured() {
		return nil
	}
	return markStatelessToo([]tool.Tool{
		teamtools.CreateGenerateVideoTool(),
		teamtools.CreateCheckVideoStatusTool(),
	})
}

// buildVisualGenTools visual 配置完整时构建视觉生成工具。
// Python 基座 visual_gen_enabled/configured（tools.py:459-467）Go 无对应
// 模块 ⤴️15.20——门控恒 false 对齐「未配置」分支。
// Python: _build_visual_gen_tools (tools.py:459-467)
func buildVisualGenTools(view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = view
	return nil
}

// buildXiaoyiPhoneTools channels.xiaoyi.phone_tools_enabled 时构建小艺电话工具。
// 工具本体（28 个 HTTP 工具）⤴️10.6.24e（缝 F5）——开关门控全量保留。
// Python: _build_xiaoyi_phone_tools (tools.py:469-479)
func buildXiaoyiPhoneTools(view *buildctx.SwarmBuildContext) []tool.Tool {
	config := view.Config
	if config == nil {
		return nil
	}
	channels, _ := config["channels"].(map[string]any)
	xiaoyi, _ := channels["xiaoyi"].(map[string]any)
	enabled, _ := xiaoyi["phone_tools_enabled"].(bool)
	if !enabled {
		return nil
	}
	// Python: _mark_stateless(list(_XIAOYI_PHONE_TOOLS))——工具本体 ⤴️10.6.24e
	return nil
}

// buildSymphonyTools leader 的 Symphony 工具（领域十六基座缝）。
// Python: _build_symphony_tools (tools.py:480-490)
func buildSymphonyTools(view *buildctx.SwarmBuildContext) []tool.Tool {
	if view.Role != "leader" {
		return nil
	}
	// Python: SymphonyToolkit().get_tools(config)——基座 ⤴️领域十六
	return nil
}

// buildA4PIntentAuthorizationTools 仅交互式 Web team leader 的 A4P 请求工具。
// 基座 is_a4p_preauthorization_active / AUTHORIZATION_EXECUTION_CONTEXTS /
// get_a4p_tools Go 未实施 ⤴️15.20——leader 门控保留，恒 false 对齐「未激活」。
// Python: _build_a4p_intent_authorization_tools (tools.py:492-504)
func buildA4PIntentAuthorizationTools(view *buildctx.SwarmBuildContext) []tool.Tool {
	if view.Role != "leader" || !isA4PPreauthorizationActive(view.Config) {
		return nil
	}
	// Python: AUTHORIZATION_EXECUTION_CONTEXTS.get(session_id) 交互校验 +
	// get_a4p_tools(session_id=...)——基座 ⤴️15.20
	return nil
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SkillToolkitForContext 成员运行时共享的 skill_index 与提示 rail 共用体。
// 16.4b 兑现：基座 SkillRetrievalToolkit 真身装配（context 槽缓存复用）。
// Python: skill_retrieval_toolkit_for_context (tools.py:380-413)
func SkillToolkitForContext(view *buildctx.SwarmBuildContext) *teamtools.SkillRetrievalToolkit {
	return skillRetrievalToolkitForContext(view)
}

// skillRetrievalToolkitForContext 返回成员运行时共享体（context 缓存复用）。
// Python: skill_retrieval_toolkit_for_context (tools.py:380-413)
func skillRetrievalToolkitForContext(view *buildctx.SwarmBuildContext) *teamtools.SkillRetrievalToolkit {
	// Python: existing = getattr(ctx, "skill_retrieval_toolkit", None)
	if view.SkillRetrievalToolkit != nil {
		return view.SkillRetrievalToolkit
	}
	memberScope := orEmpty(view.MemberCardID, view.MemberName, view.Role, "member")
	// Python: disabled_skills=load_execution_disabled_skills（skills.py 模块级）
	// Python: visible_skill_names=lambda: visible_skill_names_for_list_skill(ctx)
	visible := visibleSkillNamesForListSkill(view)
	visibleSet := map[string]bool{}
	for name := range visible {
		visibleSet[name] = true
	}
	// Python: session_scope = f"{ctx.session_id or 'default'}:{ctx.team_id or 'team'}:{member_scope}"
	sessionScope := orEmpty(view.SessionID, "default") + ":" + orEmpty(view.TeamID, "team") + ":" + memberScope
	// Python: settings=build_model_discovery_settings(ctx.config, model=extras["_parent_model"])
	var modelName string
	if extras := view.GetExtras(); extras != nil {
		modelName, _ = extras["_parent_model"].(string)
	}
	toolkit, err := teamtools.NewSkillRetrievalToolkit(teamtools.ToolkitOptions{
		SkillDirectories:  func() []string { return []string{skillLibraryDir(view)} },
		DisabledSkills:    LoadGlobalDisabledSkills,
		VisibleSkillNames: func() map[string]bool { return visibleSet },
		SourceByName:      func() map[string]string { return teamtools.SkillSourcesFromManager(nil) },
		SessionScope:      sessionScope,
		ConfigBase:        view.Config,
		Settings:          ptrOf(teamtools.BuildModelDiscoverySettings(view.Config, modelName, nil, nil)),
		AutoBuildIndex:    true,
	})
	if err != nil {
		logger.Warn(logger.ComponentAgentServer).
			Str("event_type", "skill_retrieval_toolkit_create_failed").
			Msg("[swarm.skill_retrieval] toolkit construction failed")
		return nil
	}
	view.SkillRetrievalToolkit = toolkit
	return toolkit
}

// ptrOf 设置指针（Go 侧助手，Python 无对应）。
func ptrOf(s discovery16.DiscoverySettings) *discovery16.DiscoverySettings {
	return &s
}

// BuildSkillToolkit 白名单过滤后的技能管理工具（workspace_root 门控）。
// Python: build_skill_toolkit (tools.py:531-543)
func BuildSkillToolkit(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	if memberWorkspaceRoot(view) == "" {
		return nil
	}
	return filterWhitelist(buildSkillToolkitTools())
}

// BuildSkillRetrieval 白名单过滤后的已安装 Skill 目录检索工具。
// 16.4b 兑现：`swarm.skill_retrieval` 工厂桩闭合转真身 + V-6 skill 类绑定
// （skill_index 工具装配期身份锚点 = discovery.ToolEntry.ToolID）。
// Python: build_skill_retrieval (tools.py:545-563)
func BuildSkillRetrieval(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	// Python: SkillRetrievalInput.resolve(params, ctx)——无 params 键纯 context
	// 消费，Go 侧保留步骤序。
	if !isSkillRetrievalEnabled(view.Config) {
		logger.Info(logger.ComponentAgentServer).Msg("[swarm.skill_retrieval] skipped: disabled")
		return nil
	}
	// Python: try: return _filter_whitelist(list(...get_tools())) except: warn+[]
	toolkit := skillRetrievalToolkitForContext(view)
	if toolkit == nil {
		return nil
	}
	entries := toolkit.GetTools()
	tools := make([]tool.Tool, 0, len(entries))
	for _, entry := range entries {
		tools = append(tools, newSkillIndexTool(entry))
	}
	return filterWhitelist(tools)
}

// newSkillIndexTool discovery.ToolEntry → foundation tool.Tool 适配
// （Go 侧载体转换；Python get_tools 返回的即 Tool 列表）。
func newSkillIndexTool(entry discovery16.ToolEntry) tool.Tool {
	card := tool.NewToolCardWithID(
		entry.ToolID,
		entry.Name,
		toStringField(entry.Card["description"]),
		nil,
		entry.Card,
	)
	return &skillIndexTool{card: card, entry: entry}
}

// skillIndexTool skill_index 工具载体（ToolRetrievalToolkit toolSearchTool 同款）。
type skillIndexTool struct {
	card  *tool.ToolCard
	entry discovery16.ToolEntry
}

// Card 工具卡片。
func (s *skillIndexTool) Card() *tool.ToolCard { return s.card }

// Invoke 一次性执行 skill_index。
func (s *skillIndexTool) Invoke(_ context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	result, err := s.entry.Invoke(inputs)
	if err != nil {
		return map[string]any{"success": false, "error": err.Error()}, nil
	}
	return map[string]any{
		"success": true,
		// 模型可见文本键（toolSearchTool 同契约）
		"__mapped_content": result.ModelContent,
		// 结构化诊断（SkillDCICommandResult.detailed_output 透传）
		"detailed_output": result.DetailedOutput,
	}, nil
}

// Stream 不支持流式。
func (s *skillIndexTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}

// GetCallTimeout 沿用默认外层预算。
func (s *skillIndexTool) GetCallTimeout(_ map[string]any, defaultTimeout float64) float64 {
	return defaultTimeout
}

// toStringField 卡片字段安全取字符串。
func toStringField(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// BuildUserTodos 白名单过滤后的用户 todo 工具。
// Python: build_user_todos (tools.py:565-572)
func BuildUserTodos(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	_ = view
	return filterWhitelist(buildUserTodoTools())
}

// BuildVideoTools 白名单过滤后的视频工具。
// Python: build_video_tools (tools.py:575-582)
func BuildVideoTools(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	return filterWhitelist(buildVideoTools(view))
}

// BuildImageGenTools 白名单过滤后的图片生成工具。
// Python: build_image_gen_tools (tools.py:585-592)
func BuildImageGenTools(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	return filterWhitelist(buildImageGenTools(view))
}

// BuildVideoGenTools 白名单过滤后的视频生成工具。
// Python: build_video_gen_tools (tools.py:595-602)
func BuildVideoGenTools(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	return filterWhitelist(buildVideoGenTools(view))
}

// BuildVisualGenTools 白名单过滤后的视觉生成工具。
// Python: build_visual_gen_tools (tools.py:605-612)
func BuildVisualGenTools(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	return filterWhitelist(buildVisualGenTools(view))
}

// BuildXiaoyiPhone 白名单过滤后的小艺电话工具。
// Python: build_xiaoyi_phone (tools.py:615-622)
func BuildXiaoyiPhone(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	return filterWhitelist(buildXiaoyiPhoneTools(view))
}

// BuildSymphonyToolkit leader 的 Symphony 工具（teammate 无工具）。
// Python: build_symphony_toolkit (tools.py:625-632)
func BuildSymphonyToolkit(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	return buildSymphonyTools(view)
}

// BuildA4PIntentAuthorization leader-only A4P 授权请求工具。
// Python: build_a4p_intent_authorization (tools.py:635-642)
func BuildA4PIntentAuthorization(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	return buildA4PIntentAuthorizationTools(view)
}

// BuildCodeExtraTools code 模式独占工具（当前 acp_chat）。
// acp_enabled（config.acp_agents 配置）门控。
// Python: build_code_extra_tools (tools.py:655-672)
func BuildCodeExtraTools(params map[string]any, view *buildctx.SwarmBuildContext) []tool.Tool {
	_ = params
	_ = view
	// Python: acp_enabled param（default False）→ acp_chat 单工具；
	// Go acp_chat 基座在 acp_chat 子包（10.6.24d），按 tools.py 语义
	// 由 param acp_enabled 门控——param 烘焙点在 config_specs（swarm.acp）。
	// acp_enabled 缺省 False，此处对齐「未配置」分支。
	return nil
}

// ──────────────────────────── 导出函数（多模态参数段，自 providers_tools.go 迁入） ────────────────────────────

// VisionModelConfigParams 返回 VisionModelConfig 构造 kwargs，未配置得空 map。
// 要求专用 models.vision 键 + yaml 应用后完整的 VISION_* env 映射
// （与单 agent 构建同款判定）。
// Python: vision_model_config_params (providers/tools.py:277-304)
func VisionModelConfigParams(config map[string]any) map[string]any {
	if !teamtools.MultimodalModelEnabled(config, "vision") ||
		!teamtools.CompleteMultimodalModelConfigured(config, "vision") {
		return map[string]any{}
	}
	teamtools.ApplyVisionModelConfigFromYAML(config)
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
	return teamtools.MultimodalModelEnabled(config, "audio") &&
		teamtools.CompleteMultimodalModelConfigured(config, "audio")
}

// AudioModelConfigParams 返回 AudioModelConfig 构造 kwargs，不完整得空 map。
// Python: audio_model_config_params (providers/tools.py:313-355 区段)
func AudioModelConfigParams(config map[string]any) map[string]any {
	if !AudioDedicatedConfigured(config) {
		return map[string]any{}
	}
	teamtools.ApplyAudioModelConfigFromYAML(config)
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

// parseIntDefault 宽松整数解析。
// Python: _parse_int (providers/tools.py:261-263)
func parseIntDefault(value string, defaultVal int) int {
	if n, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return n
	}
	return defaultVal
}

// orEnv 取首个非空 env 值。
// Python: os.getenv(A) or os.getenv(B) or ""（:280-281 等处内联；Go 提取助手，
// 注明 Go 侧助手 Python 无对应）。
func orEnv(primary, fallback string) string {
	if v := os.Getenv(primary); v != "" {
		return v
	}
	return os.Getenv(fallback)
}

// ──────────────────────────── 导出函数（注册） ────────────────────────────

// RegisterToolProviders 注册 tools 全部 tool provider。
// 清单顺序按 tools.py @harness_element(kind=TOOL) 声明序。
// Python: tools.py 全部 @harness_element(kind=TOOL) 段。
func RegisterToolProviders() {
	hschema.RegisterToolProvider(constSkillToolkit, toolProviderAdapter(BuildSkillToolkit))
	hschema.RegisterToolProvider(constSkillRetrieval, toolProviderAdapter(BuildSkillRetrieval))
	hschema.RegisterToolProvider(constUserTodos, toolProviderAdapter(BuildUserTodos))
	hschema.RegisterToolProvider(constVideo, toolProviderAdapter(BuildVideoTools))
	hschema.RegisterToolProvider(constImageGen, toolProviderAdapter(BuildImageGenTools))
	hschema.RegisterToolProvider(constVideoGen, toolProviderAdapter(BuildVideoGenTools))
	hschema.RegisterToolProvider(constVisualGen, toolProviderAdapter(BuildVisualGenTools))
	hschema.RegisterToolProvider(constXiaoyiPhone, toolProviderAdapter(BuildXiaoyiPhone))
	hschema.RegisterToolProvider(constSymphonyToolkit, toolProviderAdapter(BuildSymphonyToolkit))
	hschema.RegisterToolProvider(constA4PIntentAuthorization, toolProviderAdapter(BuildA4PIntentAuthorization))
	hschema.RegisterToolProvider(constCodeExtraTools, toolProviderAdapter(BuildCodeExtraTools))
}
