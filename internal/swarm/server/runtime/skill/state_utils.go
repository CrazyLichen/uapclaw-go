package skill

import (
	"encoding/json"
	"fmt"
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

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GetStateFile 返回技能状态文件路径：getAgentSkillsDir()/skills_state.json
// Python: state_utils.get_state_file()
func GetStateFile() string {
	return filepath.Join(getAgentSkillsDir(), stateFileName)
}

// NormalizeMarketplaces 规范化 marketplace 列表：过滤非 dict 项、过滤 name/url 为空的项、补全 enabled 默认值。
// Python: SkillManager.normalize_marketplaces(raw_marketplaces) (skill_manager.py)
func NormalizeMarketplaces(rawMarketplaces any) []map[string]any {
	if rawMarketplaces == nil {
		return nil
	}
	rawList, ok := toSliceOfAny(rawMarketplaces)
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
		url := trimSpace(toString(itemMap["url"]))
		if name == "" || url == "" {
			continue
		}
		// Python: bool(item.get("enabled", True))
		if _, exists := itemMap["enabled"]; !exists {
			itemMap["enabled"] = true
		}
		normalized = append(normalized, itemMap)
	}
	return normalized
}

// NormalizeSkillConfigs 规范化每个技能的配置记录
// Python: state_utils.normalize_skill_configs(raw_configs)
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
// Python: state_utils.get_registered_skill_names(state)
func GetRegisteredSkillNames(state map[string]any) map[string]bool {
	names := make(map[string]bool)

	for _, key := range []string{"installed_plugins", "local_skills"} {
		items, ok := state[key]
		if !ok {
			continue
		}
		// 兼容 normalizeState 前后：normalizeState 后是 []map[string]any，之前是 []any
		switch v := items.(type) {
		case []map[string]any:
			for _, item := range v {
				name := trimSpace(toString(item["name"]))
				if name != "" {
					names[name] = true
				}
			}
		case []any:
			for _, item := range v {
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
	}
	return names
}

// NormalizeLocalSkills 保留仍然存在于本地技能目录中的本地技能记录
// Python: state_utils.normalize_local_skills(raw_local_skills, existing_local_skill_names)
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
// Python: state_utils.get_skill_enabled(state, skill_name)
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
		// Python: bool(config.get("enabled", True)) — 非 bool 默认 true
		if b, ok := v.(bool); ok {
			return b
		}
		return true
	}
	return true
}

// SetSkillEnabled 将技能的 enabled 标志持久化到 state 中
// Python: state_utils.set_skill_enabled(state, skill_name, enabled)
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
// Python: state_utils.list_disabled_skills(state)
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
		// Python: if config.get("enabled") is False — 只有布尔值 False 才算禁用
		if v, exists := configMap["enabled"]; exists {
			if b, ok := v.(bool); ok && !b {
				disabled = append(disabled, name)
			}
		}
	}
	sort.Strings(disabled)
	return disabled
}

// ListExecutionDisabledSkills 返回当前已安装的已禁用技能名称列表
// Python: state_utils.list_execution_disabled_skills(state)
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
// Python: state_utils.load_execution_disabled_skills()
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
// Python: state_utils.filter_visible_skill_names(names)
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

// NormalizeState 规范化状态，确保所有 key 有默认值且类型正确。
// JSON 反序列化后 []map[string]any 会变成 []any，此函数将 installed_plugins 和 local_skills
// 转为 []map[string]any 存回 state，使后续方法可以直接断言 .([]map[string]any)。
// Python: SkillManager._normalize_state(state) (skill_manager.py)
func NormalizeState(state map[string]any, existingLocalSkillNames map[string]bool) {
	// Python: state.setdefault(...)
	setStateDefault(state, "marketplaces", []any{})
	setStateDefault(state, "installed_plugins", []any{})
	setStateDefault(state, "local_skills", []any{})
	setStateDefault(state, "skill_configs", map[string]any{})

	// 规范化 marketplaces
	state["marketplaces"] = NormalizeMarketplaces(state["marketplaces"])

	// 规范化 local_skills（过滤磁盘上已不存在的记录）
	state["local_skills"] = NormalizeLocalSkills(state["local_skills"], existingLocalSkillNames)

	// 规范化 skill_configs
	state["skill_configs"] = normalizeSkillConfigsForState(state["skill_configs"])

	// 将 installed_plugins 从 []any 转为 []map[string]any
	state["installed_plugins"] = anySliceToMapSlice(state["installed_plugins"])

	// 将 local_skills 从 []any 转为 []map[string]any（NormalizeLocalSkills 返回的已经是 []map[string]any，但需兜底）
	state["local_skills"] = anySliceToMapSlice(state["local_skills"])
}

// getAgentSkillsDir 返回 Agent 技能目录路径
// Python: jiuwenswarm.common.utils.get_agent_skills_dir()
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

// anySliceToMapSlice 将 any（[]any 或 []map[string]any）转为 []map[string]any。
// JSON 反序列化后数组中的对象是 map[string]any 包在 []any 中，此函数统一转换为 []map[string]any。
func anySliceToMapSlice(v any) []map[string]any {
	if v == nil {
		return nil
	}
	// 已经是 []map[string]any 的情况
	if slice, ok := v.([]map[string]any); ok {
		return slice
	}
	// []any 中每个元素断言为 map[string]any
	rawList, ok := toSliceOfAny(v)
	if !ok {
		return nil
	}
	var result []map[string]any
	for _, item := range rawList {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		result = append(result, itemMap)
	}
	return result
}

// normalizeSkillConfigsForState 规范化 skill_configs 并以 map[string]any 形式返回（存入 state）。
// 与 NormalizeSkillConfigs 不同，此函数返回 map[string]any 以兼容 state 存储格式。
// Python: normalize_skill_configs 返回 dict[str, dict[str, bool]]，存入 state 后仍为 dict。
func normalizeSkillConfigsForState(rawConfigs any) map[string]any {
	if rawConfigs == nil {
		return make(map[string]any)
	}
	configs, ok := rawConfigs.(map[string]any)
	if !ok {
		return make(map[string]any)
	}

	normalized := make(map[string]any, len(configs))
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
		normalized[name] = map[string]any{"enabled": enabled}
	}
	return normalized
}

// setStateDefault 如果 state 中不存在 key 则设置默认值
// Python: dict.setdefault(key, default)
func setStateDefault(state map[string]any, key string, defaultVal any) {
	if _, exists := state[key]; !exists {
		state[key] = defaultVal
	}
}

// safeChildPath 构建安全的子路径，防止路径遍历攻击。
// Python: _safe_child_path (skill_manager.py)
// 先调用 safePathName 规范化名称，再检查解析后的路径在 base 目录内。
func safeChildPath(base string, name string, label string) (string, error) {
	safeName, err := safePathName(name, label)
	if err != nil {
		return "", err
	}
	baseResolved, err := filepath.EvalSymlinks(base)
	if err != nil {
		return "", fmt.Errorf("解析基础路径失败: %w", err)
	}
	candidate := filepath.Join(base, safeName)
	candidateResolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		// 路径不存在时，用 filepath.Join 结果做前缀检查
		candidateResolved = filepath.Clean(candidate)
	}
	// 对齐 Python: candidate.resolve().relative_to(base_resolved) 失败则 ValueError
	if !strings.HasPrefix(candidateResolved, baseResolved+string(filepath.Separator)) && candidateResolved != baseResolved {
		return "", fmt.Errorf("无效的 %s 路径: %s", label, safeName)
	}
	return candidate, nil
}
