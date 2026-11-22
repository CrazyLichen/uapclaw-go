package rails

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails/skills"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamSkillLibraryReloadRail 在写入全局技能库后重载成员的技能视图。
//
// 团队成员共享唯一物理技能库，彼此仅以可见性元数据区分可见范围。
// 技能源变更后无需拷贝或链接：成员只需重读技能库——
// 本 rail 在「类写入工具」触碰技能库根时触发该重载。
//
// Python: TeamSkillLibraryReloadRail (team_skill_library_reload_rail.py:64-144)
type TeamSkillLibraryReloadRail struct {
	rails.DeepAgentRail
	// globalSkillsDir 全体 agent 共享的唯一技能库根
	globalSkillsDir string
	// reloadSkillViews 可选钩子，替代内建重载（工厂缝注入；nil 时重载 agent 自身技能 rail）
	reloadSkillViews func() error
}

// ──────────────────────────── 常量 ────────────────────────────

// teamSkillLibraryReloadLogComponent 日志组件
const teamSkillLibraryReloadLogComponent = logger.ComponentAgentServer

// writeTools 触发重载的类写入工具集合。
// Python: TeamSkillLibraryReloadRail.WRITE_TOOLS = frozenset({"write_file", "edit_file"})
var writeTools = map[string]struct{}{"write_file": {}, "edit_file": {}}

// skillUseRailType SkillUseRail 的反射类型（FindRailsByType 过滤用）
var skillUseRailType = reflect.TypeOf(&skills.SkillUseRail{})

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamSkillLibraryReloadRail 将 rail 绑定到唯一物理技能库。
//
// Python: TeamSkillLibraryReloadRail.__init__ (team_skill_library_reload_rail.py:76-93)
func NewTeamSkillLibraryReloadRail(globalSkillsDir string, reloadSkillViews func() error) *TeamSkillLibraryReloadRail {
	return &TeamSkillLibraryReloadRail{
		DeepAgentRail:    *rails.NewDeepAgentRail(),
		globalSkillsDir:  globalSkillsDir,
		reloadSkillViews: reloadSkillViews,
	}
}

// AfterToolCall 类写入工具触碰全局技能库后重载技能视图。
//
// Python: TeamSkillLibraryReloadRail.after_tool_call (team_skill_library_reload_rail.py:95-107)
func (r *TeamSkillLibraryReloadRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	toolName := strings.TrimSpace(inputs.ToolName)
	if _, isWrite := writeTools[toolName]; !isWrite {
		return nil
	}
	filePath := extractFilePath(inputs.ToolArgs)
	if filePath == "" || !r.isUnderGlobalSkillsDir(filePath) {
		return nil
	}
	return r.reload(ctx, cbc.Agent())
}

// ReloadAgentSkillViews 重载 agent 上挂载的每个 SkillUseRail。
//
// 技能只存在于唯一物理库中，agent 对它的视图由其 skills-visibility.json
// 文档决定，而非物化的按 agent 目录。让库变更可见只需请每个技能 rail
// 重读库与元数据。
//
// Python: reload_agent_skill_views (team_skill_library_reload_rail.py:33-61，模块级函数)
func ReloadAgentSkillViews(ctx context.Context, agent agentinterfaces.BaseAgent) int {
	reloaded := 0
	deepAgent, ok := agent.(hinterfaces.DeepAgentInterface)
	if !ok {
		logger.Warn(teamSkillLibraryReloadLogComponent).
			Msg("[TeamSkillLibraryReload] cannot enumerate skill rails: agent is not a DeepAgent")
		return reloaded
	}
	skillRails := deepAgent.FindRailsByType(skillUseRailType)

	for _, rail := range skillRails {
		reloader, ok := rail.(interface {
			ReloadSkills(ctx context.Context) error
		})
		if !ok {
			continue
		}
		if err := reloader.ReloadSkills(ctx); err != nil {
			logger.Warn(teamSkillLibraryReloadLogComponent).
				Err(err).
				Msg("[TeamSkillLibraryReload] skill view reload failed")
			continue
		}
		reloaded++
	}
	return reloaded
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// reload 运行注入钩子，或重载 agent 自身的技能 rail。
//
// Python: TeamSkillLibraryReloadRail._reload (team_skill_library_reload_rail.py:109-119)
func (r *TeamSkillLibraryReloadRail) reload(ctx context.Context, agent agentinterfaces.BaseAgent) error {
	if r.reloadSkillViews == nil {
		ReloadAgentSkillViews(ctx, agent)
		return nil
	}
	if err := r.reloadSkillViews(); err != nil {
		logger.Warn(teamSkillLibraryReloadLogComponent).
			Err(err).
			Msg("[TeamSkillLibraryReload] reload hook failed")
	}
	return nil
}

// extractFilePath 从工具参数提取文件路径。
//
// Python: TeamSkillLibraryReloadRail._extract_file_path (team_skill_library_reload_rail.py:121-133)
// 偏差：Go ToolCallInputs.ToolArgs 已是 map[string]any，无 Python str-JSON 分支。
func extractFilePath(args map[string]any) string {
	if args == nil {
		return ""
	}
	if v, ok := args["file_path"].(string); ok && v != "" {
		return strings.TrimSpace(v)
	}
	if v, ok := args["path"].(string); ok && v != "" {
		return strings.TrimSpace(v)
	}
	return ""
}

// isUnderGlobalSkillsDir 判断路径是否位于全局技能库内。
//
// Python: TeamSkillLibraryReloadRail._is_under_global_skills_dir (team_skill_library_reload_rail.py:135-144)
func (r *TeamSkillLibraryReloadRail) isUnderGlobalSkillsDir(filePath string) bool {
	// Python: Path(os.path.expanduser(file_path))
	candidate := expandUser(filePath)
	if !filepath.IsAbs(candidate) {
		// Python: Path(get_cwd()).expanduser().resolve() / candidate
		cwd, err := os.Getwd()
		if err != nil {
			return false
		}
		candidate = filepath.Join(expandUser(cwd), candidate)
	}
	resolvedCandidate, err := resolveSymlinks(candidate)
	if err != nil {
		return false
	}
	resolvedRoot, err := resolveSymlinks(expandUser(r.globalSkillsDir))
	if err != nil {
		return false
	}
	if resolvedCandidate == resolvedRoot {
		return true
	}
	return strings.HasPrefix(resolvedCandidate, resolvedRoot+string(filepath.Separator))
}

// expandUser 展开路径中的 ~ 前缀（对齐 Python Path.expanduser）。
func expandUser(path string) string {
	if path == "~" {
		home, _ := os.UserHomeDir()
		return home
	}
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// resolveSymlinks 解析符号链接（对齐 Python Path.resolve 的 strict=False 语义：
// 尽可能解析符号链接；不存在的尾部按普通名拼接）。
func resolveSymlinks(path string) (string, error) {
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	// 悬空链接：对齐 Python resolve(strict=False)——仍然穿透最终符号链接，
	// 解析其指向的目标路径（目标可以不存在）。
	if fi, lerr := os.Lstat(path); lerr == nil && fi.Mode()&os.ModeSymlink != 0 {
		target, rerr := os.Readlink(path)
		if rerr != nil {
			return "", rerr
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(filepath.Dir(path), target)
		}
		return resolveSymlinks(target)
	}
	// 普通缺失路径：解析最近存在的祖先目录后拼接尾段
	parent := filepath.Dir(path)
	resolvedParent, err2 := filepath.EvalSymlinks(parent)
	if err2 != nil {
		// 祖先也缺失：返回绝对化规整路径
		abs, err3 := filepath.Abs(path)
		if err3 != nil {
			return "", err3
		}
		return filepath.Clean(abs), nil
	}
	return filepath.Join(resolvedParent, filepath.Base(path)), nil
}

// jsonMarshalCompact 紧凑 JSON 序列化（ensure_ascii=False 语义由 encoding/json 保证 UTF-8 直出）。
func jsonMarshalCompact(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(b)
}
