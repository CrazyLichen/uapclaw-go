package team

import (
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
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
