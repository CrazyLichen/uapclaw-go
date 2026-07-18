package skill

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentServer
	// stateFileName 技能状态文件名
	stateFileName = "skills_state.json"
)

// GetStateFile 返回技能状态文件路径：getAgentSkillsDir()/skills_state.json
// 对应 Python: state_utils.get_state_file()
// ──────────────────────────── 导出函数 ────────────────────────────

func GetStateFile() string {
	return filepath.Join(getAgentSkillsDir(), stateFileName)
}

// NormalizeSkillConfigs 规范化每个技能的配置记录
// 对应 Python: state_utils.normalize_skill_configs(raw_configs)
func NormalizeSkillConfigs(rawConfigs any) map[string]map[string]bool {
	normalized := make(map[string]map[string]bool)

	if rawConfigs == nil {
		return normalized
	}
	configs, ok := rawConfigs.(map[string]any)
	if !ok {
		return normalized
	}

	for rawName, rawCfg := range configs {
		name := trimSpace(rawName)
		if name == "" {
			continue
		}
		enabled := true
		if cfg, ok := rawCfg.(map[string]any); ok {
			if v, exists := cfg["enabled"]; exists {
				enabled = toBool(v)
			}
		}
		normalized[name] = map[string]bool{"enabled": enabled}
	}
	return normalized
}

// GetRegisteredSkillNames 返回 installed_plugins 和 local_skills 中记录的所有技能名称
// 对应 Python: state_utils.get_registered_skill_names(state)
func GetRegisteredSkillNames(state map[string]any) map[string]bool {
	names := make(map[string]bool)

	for _, key := range []string{"installed_plugins", "local_skills"} {
		items, ok := state[key]
		if !ok {
			continue
		}
		itemsList, ok := toSliceOfAny(items)
		if !ok {
			continue
		}
		for _, item := range itemsList {
			itemMap, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name := trimSpace(toString(itemMap["name"]))
			if name != "" {
				names[name] = true
			}
		}
	}
	return names
}

// NormalizeLocalSkills 保留仍然存在于本地技能目录中的本地技能记录
// 对应 Python: state_utils.normalize_local_skills(raw_local_skills, existing_local_skill_names)
func NormalizeLocalSkills(rawLocalSkills any, existingLocalSkillNames map[string]bool) []map[string]any {
	if rawLocalSkills == nil {
		return nil
	}
	rawList, ok := toSliceOfAny(rawLocalSkills)
	if !ok {
		return nil
	}

	var normalized []map[string]any
	for _, item := range rawList {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := trimSpace(toString(itemMap["name"]))
		if name == "" || !existingLocalSkillNames[name] {
			continue
		}
		normalized = append(normalized, itemMap)
	}
	return normalized
}

// GetSkillEnabled 读取技能的 enabled 标志，默认为 true（向后兼容）
// 对应 Python: state_utils.get_skill_enabled(state, skill_name)
func GetSkillEnabled(state map[string]any, skillName string) bool {
	if skillName == "" {
		return true
	}

	configs, ok := state["skill_configs"]
	if !ok {
		return true
	}
	configsMap, ok := configs.(map[string]any)
	if !ok {
		return true
	}

	config, ok := configsMap[skillName]
	if !ok {
		return true
	}
	configMap, ok := config.(map[string]any)
	if !ok {
		return true
	}

	if v, exists := configMap["enabled"]; exists {
		return toBool(v)
	}
	return true
}

// SetSkillEnabled 将技能的 enabled 标志持久化到 state 中
// 对应 Python: state_utils.set_skill_enabled(state, skill_name, enabled)
func SetSkillEnabled(state map[string]any, skillName string, enabled bool) {
	configs, ok := state["skill_configs"]
	if !ok {
		configs = make(map[string]any)
		state["skill_configs"] = configs
	}
	configsMap, ok := configs.(map[string]any)
	if !ok {
		configsMap = make(map[string]any)
		state["skill_configs"] = configsMap
	}
	configsMap[skillName] = map[string]any{"enabled": enabled}
}

// ListDisabledSkills 从 skill_configs 中返回已禁用的技能名称列表（排序）
// 对应 Python: state_utils.list_disabled_skills(state)
func ListDisabledSkills(state map[string]any) []string {
	configs, ok := state["skill_configs"]
	if !ok {
		return nil
	}
	configsMap, ok := configs.(map[string]any)
	if !ok {
		return nil
	}

	var disabled []string
	for name, config := range configsMap {
		configMap, ok := config.(map[string]any)
		if !ok {
			continue
		}
		if v, exists := configMap["enabled"]; exists && !toBool(v) {
			disabled = append(disabled, name)
		}
	}
	sort.Strings(disabled)
	return disabled
}

// ListExecutionDisabledSkills 返回当前已安装的已禁用技能名称列表
// 对应 Python: state_utils.list_execution_disabled_skills(state)
func ListExecutionDisabledSkills(state map[string]any) []string {
	registered := GetRegisteredSkillNames(state)
	if len(registered) == 0 {
		return nil
	}

	disabled := ListDisabledSkills(state)
	var result []string
	for _, name := range disabled {
		if registered[name] {
			result = append(result, name)
		}
	}
	return result
}

// LoadExecutionDisabledSkills 读取 skills_state.json 并返回已安装的已禁用技能名称列表
// 对应 Python: state_utils.load_execution_disabled_skills()
func LoadExecutionDisabledSkills() []string {
	stateFile := GetStateFile()
	data, err := os.ReadFile(stateFile)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn(logComponent).Str("error", err.Error()).Msg("加载禁用技能列表失败")
		}
		return nil
	}

	var state map[string]any
	if err := json.Unmarshal(data, &state); err != nil {
		logger.Warn(logComponent).Str("error", err.Error()).Msg("解析技能状态文件失败")
		return nil
	}

	return ListExecutionDisabledSkills(state)
}

// FilterVisibleSkillNames 返回未被禁用的技能名称列表
// 对应 Python: state_utils.filter_visible_skill_names(names)
func FilterVisibleSkillNames(names []string) []string {
	disabled := LoadExecutionDisabledSkills()
	disabledSet := make(map[string]bool, len(disabled))
	for _, d := range disabled {
		disabledSet[d] = true
	}
	if len(disabledSet) == 0 {
		return names
	}

	var visible []string
	for _, n := range names {
		if !disabledSet[n] {
			visible = append(visible, n)
		}
	}
	return visible
}

// getAgentSkillsDir 返回 Agent 技能目录路径
// 对应 Python: jiuwenswarm.common.utils.get_agent_skills_dir()
// ──────────────────────────── 非导出函数 ────────────────────────────

func getAgentSkillsDir() string {
	return workspace.AgentSkillsDir()
}

// trimSpace 去除首尾空白
func trimSpace(s string) string {
	// 标准库 strings.TrimSpace 的内联包装，便于同包引用
	return strings.TrimSpace(s)
}

// toString 将 any 转为字符串
func toString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// toBool 将 any 转为 bool（非零值均为 true）
func toBool(v any) bool {
	if v == nil {
		return false
	}
	switch val := v.(type) {
	case bool:
		return val
	case int:
		return val != 0
	case float64:
		return val != 0
	case string:
		return val != ""
	default:
		return true
	}
}

// toSliceOfAny 将 any 转为 []any
func toSliceOfAny(v any) ([]any, bool) {
	if v == nil {
		return nil, false
	}
	slice, ok := v.([]any)
	if !ok {
		return nil, false
	}
	return slice, true
}
