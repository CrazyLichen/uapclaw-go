package team

import (
	"os"
	"strings"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	ce "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	skillruntime "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberInfo 成员身份信息。
// 对齐 Python: MemberInfo dataclass (team_runtime_inheritance.py L39-44)
type MemberInfo struct {
	// AgentName 成员名称（Python 默认 "team_member"）
	AgentName string
	// ModelName 模型名称（Python 默认 "gpt-4"）
	ModelName string
	// Role 角色（leader/teammate），nil 表示未指定
	Role *string
}

// RuntimeInfo 运行时环境信息。
// 对齐 Python: RuntimeInfo dataclass (L47-51)
type RuntimeInfo struct {
	// Channel 渠道标识（Python 默认 "default"）
	Channel string
	// Language 语言标识（Python 默认 "cn"）
	Language string
}

// TeamWorkspaceInfo Team 共享 workspace 信息。
// 对齐 Python: TeamWorkspaceInfo dataclass (L54-61)
type TeamWorkspaceInfo struct {
	// RootDir 工作区根目录
	RootDir *string
	// SkillsDir 技能目录
	SkillsDir *string
	// TeamID 团队标识
	TeamID *string
	// Config 配置字典
	Config map[string]any
	// TrajectoryRegistry 轨迹注册表（同时实现 TrajectorySource + TrajectorySink）
	// 对齐 Python: TeamWorkspaceInfo.trajectory_registry（实际为 InMemoryTrajectoryRegistry）
	TrajectoryRegistry trajectory.TrajectorySource
}

// TeamRailMountContext 重建 team rails 所需的上下文。
// 对齐 Python: TeamRailMountContext dataclass (team_manager.py L110-117)
type TeamRailMountContext struct {
	// Agent 重建 rails 时关联的 Agent 实例
	Agent interfaces.DeepAgentInterface
	// MemberInfo 成员信息
	MemberInfo *MemberInfo
	// Runtime 运行时信息
	Runtime *RuntimeInfo
	// TeamWorkspace 团队工作区信息
	TeamWorkspace *TeamWorkspaceInfo
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// railWhitelist 成员 rail 白名单（按 Go 实际反射类型名）。
// 对齐 Python: RAIL_WHITELIST (team_runtime_inheritance.py L64-79)
//
// 差异说明：
//   - Python "SecurityRail" → Go 实际类型名 "SafetyPromptRail"（type SecurityRail = SafetyPromptRail
//     类型别名，reflect Elem().Name() 返回 SafetyPromptRail）
//   - "FileSystemRail" 为 Python 侧类型名，Go 无对应类型，保留占位键永不匹配
var railWhitelist = map[string]bool{
	"RuntimePromptRail":           true,
	"ResponsePromptRail":          true,
	"JiuClawStreamEventRail":      true,
	"TaskPlanningRail":            true,
	"SafetyPromptRail":            true,
	"HeartbeatRail":               true,
	"AvatarPromptRail":            true,
	"FileSystemRail":              true,
	"SysOperationRail":            true,
	"TeamSkillEvolutionRail":      true,
	"TeamSkillCreateRail":         true,
	"SkillEvolutionRail":          true,
	"TeamWorkspaceReportPathRail": true,
	"ContextProcessorRail":        true,
}

// toolWhitelist 可继承工具白名单。
// 对齐 Python: TOOL_WHITELIST (L81-126)
var toolWhitelist = map[string]bool{
	"free_search": true, "fetch_webpage": true, "paid_search": true, "vision": true,
	"audio": true, "image_ocr": true, "visual_question_answering": true,
	"generate_image": true, "audio_transcription": true, "audio_question_answering": true,
	"audio_metadata": true, "video_understanding": true, "search_skill": true,
	"install_skill": true, "uninstall_skill": true, "user_todos": true,
	"get_user_location": true, "create_note": true, "search_notes": true,
	"modify_note": true, "create_calendar_event": true, "search_calendar_event": true,
	"search_contact": true, "search_photo_gallery": true, "upload_photo": true,
	"search_file": true, "upload_file": true, "call_phone": true, "send_message": true,
	"search_message": true, "create_alarm": true, "search_alarms": true,
	"modify_alarm": true, "delete_alarm": true, "xiaoyi_collection": true,
	"image_reading": true, "xiaoyi_gui_agent": true, "web_free_search": true,
	"web_fetch_webpage": true, "web_paid_search": true, "skill_toolkit": true,
	"enter_worktree": true, "exit_worktree": true, "acp_chat": true,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetEvolutionAutoScanEnabled 从 config 读取 auto_scan 开关。
// 对齐 Python: get_evolution_auto_scan_enabled (L381-385)
//
// env EVOLUTION_AUTO_SCAN 优先（true/1/yes 不区分大小写），
// 回退 react.evolution.auto_scan → evolution.auto_scan，默认 false。
func GetEvolutionAutoScanEnabled(config map[string]any) bool {
	if envVal, ok := getBoolEnv("EVOLUTION_AUTO_SCAN"); ok {
		return envVal
	}
	return getEvolutionConfigField(config, "auto_scan")
}

// GetSkillCreateEnabled 从 config 读取 skill_create 开关。
// 对齐 Python: get_skill_create_enabled (L388-392)
func GetSkillCreateEnabled(config map[string]any) bool {
	if envVal, ok := getBoolEnv("SKILL_CREATE"); ok {
		return envVal
	}
	return getEvolutionConfigField(config, "skill_create")
}

// GetContextEngineEnabled 检查上下文压缩是否启用。
// 对齐 Python: get_context_engine_enabled (L525-537)
//
// 读取 react.context_engine_config.enabled，默认 true。
func GetContextEngineEnabled(config map[string]any) bool {
	if config == nil {
		return true
	}
	react, _ := config["react"].(map[string]any)
	if react != nil {
		ctxCfg, _ := react["context_engine_config"].(map[string]any)
		if ctxCfg != nil {
			if b, ok := ctxCfg["enabled"].(bool); ok {
				return b
			}
		}
	}
	return true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getEvolutionConfigField 从嵌套路径读取 evolution 配置字段（bool）。
// 对齐 Python _get_evolution_config (L369-378)：
// 优先 config["react"]["evolution"]，回退 config["evolution"]，均无则空 → 默认 false
func getEvolutionConfigField(config map[string]any, field string) bool {
	evolutionConfig := getEvolutionConfig(config)
	if b, ok := evolutionConfig[field].(bool); ok {
		return b
	}
	return false
}

// getEvolutionConfig 解析嵌套的 evolution 配置段。
// 对齐 Python _get_evolution_config (L369-378)
func getEvolutionConfig(config map[string]any) map[string]any {
	if config == nil {
		return nil
	}
	if reactConfig, ok := config["react"].(map[string]any); ok {
		if evolutionConfig, ok := reactConfig["evolution"].(map[string]any); ok {
			return evolutionConfig
		}
	}
	if evolutionConfig, ok := config["evolution"].(map[string]any); ok {
		return evolutionConfig
	}
	return nil
}

// getBoolEnv 读取布尔环境变量。
// 对齐 Python _get_bool_env (L363-366)：
// 值为 true/1/yes（不区分大小写）→ true；设置但非真值 → false；未设置 → (false, false)
func getBoolEnv(key string) (value bool, ok bool) {
	raw, exists := os.LookupEnv(key)
	if !exists {
		return false, false
	}
	return strings.EqualFold(raw, "true") || raw == "1" || strings.EqualFold(raw, "yes"), true
}

// GetDefaultModelName 从配置获取默认 model_name。
// 对齐 Python: get_default_model_name (team_runtime_inheritance.py L335-360)
//
// 读取 models.default.model_client_config.model_name（注意：Python 读旧格式
// default 单对象而非 defaults 列表），失败回退 "gpt-4"。
func GetDefaultModelName(cfgMap map[string]any) string {
	cfg := cfgMap
	if cfg == nil {
		loaded, err := teamLoadConfig()
		if err != nil {
			logger.Warn(logComponent).Err(err).
				Msg("[TeamRuntime] Failed to load config for default model")
			return "gpt-4"
		}
		cfg = loaded
	}
	models, _ := cfg["models"].(map[string]any)
	if models != nil {
		defaultEntry, _ := models["default"].(map[string]any)
		if defaultEntry != nil {
			if mcc, _ := defaultEntry["model_client_config"].(map[string]any); mcc != nil {
				if name, ok := mcc["model_name"].(string); ok && name != "" {
					return name
				}
			}
		}
	}
	return "gpt-4"
}

// ResolveModelConfig 从配置字典解析 model 相关参数。
// 对齐 Python: resolve_model_config (L395-448)
//
// 优先 models.defaults 列表 is_default=true 条目 → 第一个条目 →
// models.default 单对象 → react 段回退。返回浅拷贝（对齐 Python .copy()）。
func ResolveModelConfig(config map[string]any) (mcc map[string]any, mco map[string]any, modelName string) {
	if config == nil {
		return map[string]any{}, map[string]any{}, "gpt-4"
	}
	models, _ := config["models"].(map[string]any)
	if models == nil {
		models = map[string]any{}
	}

	// 优先从 models.defaults 列表取 is_default=true 的条目
	if defaults, ok := models["defaults"].([]any); ok && len(defaults) > 0 {
		for _, item := range defaults {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if b, ok := entry["is_default"].(bool); ok && b {
				mcc = copyMapShallow(entry["model_client_config"])
				mco = copyMapShallow(entry["model_config_obj"])
				if name := mapStrField(mcc, "model_name"); name != "" {
					return mcc, mco, name
				}
			}
		}
		// 无 is_default=true 时取第一个
		if first, ok := defaults[0].(map[string]any); ok {
			mcc = copyMapShallow(first["model_client_config"])
			mco = copyMapShallow(first["model_config_obj"])
			if name := mapStrField(mcc, "model_name"); name != "" {
				return mcc, mco, name
			}
		}
	}

	// 回退到旧格式：models.default + react 段
	defaultModelConfig, _ := models["default"].(map[string]any)
	if defaultModelConfig == nil {
		defaultModelConfig = map[string]any{}
	}
	reactConfig, _ := config["react"].(map[string]any)
	if reactConfig == nil {
		reactConfig = map[string]any{}
	}

	modelClientConfig, _ := defaultModelConfig["model_client_config"].(map[string]any)
	if modelClientConfig == nil {
		modelClientConfig, _ = reactConfig["model_client_config"].(map[string]any)
	}
	if modelClientConfig == nil {
		modelClientConfig = map[string]any{}
	}

	modelName = mapStrField(modelClientConfig, "model_name")
	if modelName == "" {
		modelName = mapStrField(reactConfig, "model_name")
	}
	if modelName == "" {
		modelName = "gpt-4"
	}

	modelConfigObj, _ := defaultModelConfig["model_config_obj"].(map[string]any)
	if modelConfigObj == nil {
		modelConfigObj, _ = reactConfig["model_config_obj"].(map[string]any)
	}
	if modelConfigObj == nil {
		modelConfigObj = map[string]any{}
	}

	return modelClientConfig, modelConfigObj, modelName
}

// BuildEvolutionLLM 从配置构造 evolution 使用的 LLM Model 实例。
// 对齐 Python: build_evolution_llm (L451-476)
//
// temperature 取 model_config_obj.temperature，默认 0.95。
// 构造失败（provider/api_key/api_base 缺失或 Model 创建失败）返回 (nil, model_name)，
// 调用方按 Python try/except 语义跳过该 rail。
func BuildEvolutionLLM(config map[string]any) (*llm.Model, string) {
	mcc, mco, modelName := ResolveModelConfig(config)
	temperature := 0.95
	if t, ok := mco["temperature"].(float64); ok {
		temperature = t
	}
	requestConfig := llmschema.NewModelRequestConfig(
		llmschema.WithModelName(modelName),
		llmschema.WithTemperature(temperature),
	)

	provider := mapStrField(mcc, "client_provider")
	apiKey := mapStrField(mcc, "api_key")
	apiBase := mapStrField(mcc, "api_base")

	var clientConfigOpts []llmschema.ModelClientConfigOption
	if t, ok := mcc["timeout"].(float64); ok {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithTimeout(t))
	}
	if v, ok := mcc["verify_ssl"].(bool); ok {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithVerifySSL(v))
	}
	if headers := customHeadersToStringMap(mcc["custom_headers"]); headers != nil {
		clientConfigOpts = append(clientConfigOpts, llmschema.WithCustomHeaders(headers))
	}

	clientConfig, err := llmschema.NewModelClientConfig(provider, apiKey, apiBase, clientConfigOpts...)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("model_name", modelName).
			Msg("[TeamRuntime] evolution LLM client config invalid")
		return nil, modelName
	}
	model, err := llm.NewModel(clientConfig, requestConfig)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("model_name", modelName).
			Msg("[TeamRuntime] evolution LLM creation failed")
		return nil, modelName
	}
	return model, modelName
}

// ──────────────────────────── 非导出函数（模型解析辅助） ────────────────────────────

// teamLoadConfig 加载默认 config.yaml。
// 对齐 Python: get_config()
func teamLoadConfig() (map[string]any, error) {
	c, err := config.New("")
	if err != nil {
		return nil, err
	}
	return c.Load()
}

// copyMapShallow 浅拷贝 map[string]any（对齐 Python dict.copy()）。
// 非 map 输入返回空 map（对齐 Python (x or {}).copy() 的 None 容错）。
func copyMapShallow(v any) map[string]any {
	m, ok := v.(map[string]any)
	if !ok || m == nil {
		return map[string]any{}
	}
	result := make(map[string]any, len(m))
	for k, val := range m {
		result[k] = val
	}
	return result
}

// mapStrField 从 map 提取字符串字段（非 string 或缺失返回空串）。
func mapStrField(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if s, ok := m[key].(string); ok {
		return s
	}
	return ""
}

// customHeadersToStringMap 将 custom_headers 配置转为 map[string]string。
//
// 输入可能是 JSON 字符串或 map[string]any（config 已解析路径），
// 对齐 Python ModelClientConfig(custom_headers=...) 的 dict 语义。
func customHeadersToStringMap(v any) map[string]string {
	if v == nil {
		return nil
	}
	if m, ok := v.(map[string]any); ok {
		result := make(map[string]string, len(m))
		for k, val := range m {
			if s, ok := val.(string); ok {
				result[k] = s
			}
		}
		if len(result) == 0 && len(m) > 0 {
			return nil
		}
		return result
	}
	if s, ok := v.(string); ok && s != "" {
		parsed := config.ParseCustomHeaders(s)
		if parsed == nil {
			return nil
		}
		result := make(map[string]string, len(parsed))
		for k, val := range parsed {
			if sv, ok := val.(string); ok {
				result[k] = sv
			}
		}
		return result
	}
	return nil
}

// FilterInheritableAbilityCards 从主 agent 获取可继承的 ToolCard 白名单。
// 对齐 Python: filter_inheritable_ability_cards (team_runtime_inheritance.py L307-332)
func FilterInheritableAbilityCards(mainAgent interfaces.DeepAgentInterface) []*tool.ToolCard {
	result := make([]*tool.ToolCard, 0)
	if mainAgent == nil {
		return result
	}
	abilities := mainAgent.AbilityManager().List()
	for _, ability := range abilities {
		card, ok := ability.(*tool.ToolCard)
		if !ok {
			logger.Debug(logComponent).Str("ability", ability.AbilityName()).
				Msg("[TeamRuntime] Skipping non-ToolCard ability")
			continue
		}
		if toolWhitelist[card.Name] {
			result = append(result, card)
		} else {
			logger.Debug(logComponent).Str("tool", card.Name).
				Msg("[TeamRuntime] Tool not in whitelist, skipped")
		}
	}
	return result
}

// BuildContextProcessorRail 为团队成员构造 preset ContextProcessorRail。
// 对齐 Python: _build_context_processor_rail (L540-587)
//
// 读取 react.context_engine_config 下四段处理器配置，
// 转 []ceiface.ProcessorSpec{Type, ConfigOverrides}（Python (name, dict) pairs）。
func BuildContextProcessorRail(config map[string]any) *ce.ContextProcessorRail {
	userProcessors := make([]ceiface.ProcessorSpec, 0, 4)
	ctxCfg := map[string]any{}
	if config != nil {
		if react, _ := config["react"].(map[string]any); react != nil {
			if c, _ := react["context_engine_config"].(map[string]any); c != nil {
				ctxCfg = c
			}
		}
	}

	// 四段配置按 Python 顺序追加 (name, dict) pairs
	processorSpecs := []struct {
		configKey string
		typeName  string
	}{
		{"message_summary_offloader_config", "MessageSummaryOffloader"},
		{"dialogue_compressor_config", "DialogueCompressor"},
		{"current_round_compressor_config", "CurrentRoundCompressor"},
		{"round_level_compressor_config", "RoundLevelCompressor"},
	}
	for _, spec := range processorSpecs {
		if cfgMap, _ := ctxCfg[spec.configKey].(map[string]any); cfgMap != nil && len(cfgMap) > 0 {
			userProcessors = append(userProcessors, ceiface.ProcessorSpec{
				Type:           spec.typeName,
				ConfigOverrides: utils.DeepCopyMap(cfgMap),
			})
		}
	}

	rail := ce.NewContextProcessorRail(
		ce.WithPreset(true),
		ce.WithUserProcessors(userProcessors),
	)
	names := make([]string, 0, len(userProcessors))
	for _, p := range userProcessors {
		names = append(names, p.Type)
	}
	logger.Info(logComponent).Strs("user_processors", names).
		Msg("[TeamRuntime] ContextProcessorRail created (preset=True)")
	return rail
}

// BuildSkillEvolutionRail 为 Team member 构造 SkillEvolutionRail。
// 对齐 Python: build_skill_evolution_rail (L479-522)
//
// auto_save=true（成员自演化跳过审批）；LLM 构造失败返回 nil（对齐 Python
// except 返回 None）。teamTrajectorySink 非 nil 且 teamID 非空时绑定
// SetTrajectorySink(sink, teamID, "teammate")。
func BuildSkillEvolutionRail(skillsDir string, config map[string]any, teamTrajectorySink trajectory.TrajectorySink, teamID string) *evolution.SkillEvolutionRail {
	llmModel, modelName := BuildEvolutionLLM(config)
	if llmModel == nil {
		return nil
	}
	evolutionAutoScan := GetEvolutionAutoScanEnabled(config)

	// Python 未显式传 language，使用默认 "cn"
	rail := evolution.NewSkillEvolutionRail(
		[]string{skillsDir},
		llmModel,
		modelName,
		"cn",
		evolution.WithAutoScan(evolutionAutoScan),
		evolution.WithAutoSave(true),
		evolution.WithDisabledSkillsSet(skillruntime.LoadExecutionDisabledSkills()),
	)

	if teamTrajectorySink != nil && teamID != "" {
		if err := rail.SetTrajectorySink(teamTrajectorySink, teamID, "teammate"); err != nil {
			logger.Warn(logComponent).Err(err).Str("team_id", teamID).
				Msg("[TeamRuntime] SkillEvolutionRail set_trajectory_sink failed")
		}
	}

	logger.Info(logComponent).
		Str("model", modelName).
		Bool("auto_scan", evolutionAutoScan).
		Bool("team_trajectory_sink", teamTrajectorySink != nil && teamID != "").
		Msg("[TeamRuntime] SkillEvolutionRail created")
	return rail
}
