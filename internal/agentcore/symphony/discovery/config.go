// config.go 对齐 Python discovery/config.py（189 行）：设置契约与配置装载。
package discovery

import (
	"fmt"
	"strconv"
	"strings"
)

// SkillIndexToolName skill_index 工具名（对齐 toolkit.py SKILL_INDEX_TOOL_NAME）
const SkillIndexToolName = "skill_index"

// 默认设置常量（对齐 config.py:11-17）
const (
	// DefaultCandidateBudgetRatio 候选预算占比 1%
	DefaultCandidateBudgetRatio = 0.01
	// DefaultMaxOutputChars 输出上限
	DefaultMaxOutputChars = 12000
	// DefaultMaxResults 默认结果数
	DefaultMaxResults = 10
	// DefaultMaxListEntries 列表条目上限
	DefaultMaxListEntries = 40
	// DefaultIncrementalNoticeMaxChars 增量通知上限
	DefaultIncrementalNoticeMaxChars = 4000
)

// DefaultPromptPreferredSkills 优先技能名单（对齐 config.py:18-22）
var DefaultPromptPreferredSkills = []string{"skill-creator", "swarmskill-creator", "symphony-assistant"}

// discoverySettingNames 设置白名单（对齐 _DISCOVERY_SETTING_NAMES）
var discoverySettingNames = map[string]bool{
	"candidate_budget_ratio": true, "context_window_tokens": true,
	"max_output_chars": true, "max_results": true, "max_list_entries": true,
	"incremental_notice_max_chars": true, "use_existing_index": true,
	"prompt_preferred_skills": true,
}

// DiscoverySettings 单个 SkillFS 环境的任务无关设置（frozen dataclass → 只读结构体）
type DiscoverySettings struct {
	// CandidateBudgetRatio 候选预算占比
	CandidateBudgetRatio float64
	// ContextWindowTokens 上下文窗口 token 数
	ContextWindowTokens int
	// MaxOutputChars 输出字符上限
	MaxOutputChars int
	// MaxResults 结果数上限
	MaxResults int
	// MaxListEntries 列表条目上限
	MaxListEntries int
	// IncrementalNoticeMaxChars 增量通知上限
	IncrementalNoticeMaxChars int
	// UseExistingIndex 是否使用既有索引
	UseExistingIndex bool
	// PromptPreferredSkills 提示词优先技能
	PromptPreferredSkills []string
}

// DefaultDiscoverySettings 默认设置
func DefaultDiscoverySettings() DiscoverySettings {
	pref := make([]string, len(DefaultPromptPreferredSkills))
	copy(pref, DefaultPromptPreferredSkills)
	return DiscoverySettings{
		CandidateBudgetRatio:      DefaultCandidateBudgetRatio,
		ContextWindowTokens:       200000,
		MaxOutputChars:            DefaultMaxOutputChars,
		MaxResults:                DefaultMaxResults,
		MaxListEntries:            DefaultMaxListEntries,
		IncrementalNoticeMaxChars: DefaultIncrementalNoticeMaxChars,
		UseExistingIndex:          false,
		PromptPreferredSkills:     pref,
	}
}

// CandidateBudgetTokens 候选 token 预算（对齐 config.py:43-44 property）
func (s DiscoverySettings) CandidateBudgetTokens() int {
	tokens := int(float64(s.ContextWindowTokens) * s.CandidateBudgetRatio)
	if tokens < 1 {
		tokens = 1
	}
	return tokens
}

// LoadDiscoverySettings 从应用配置装载设置。
// 对齐 Python load_discovery_settings（config.py:51-88）：三层路径
// symphony.skill_retrieval.discovery → 顶层白名单键 → 默认值。
func LoadDiscoverySettings(config map[string]any) DiscoverySettings {
	resolved := config
	if resolved == nil {
		resolved = map[string]any{}
	}
	symphony := mappingOf(resolved["symphony"])
	retrieval := mappingOf(symphony["skill_retrieval"])
	raw := mappingOf(retrieval["discovery"])
	if len(raw) == 0 {
		for k := range resolved {
			if discoverySettingNames[k] {
				raw = resolved
				break
			}
		}
	}
	settings := DefaultDiscoverySettings()
	settings.CandidateBudgetRatio = ratioOf(raw["candidate_budget_ratio"], DefaultCandidateBudgetRatio)
	settings.ContextWindowTokens = contextWindowTokens(resolved, raw)
	settings.MaxOutputChars = positiveIntOf(raw["max_output_chars"], DefaultMaxOutputChars)
	settings.MaxResults = positiveIntOf(raw["max_results"], DefaultMaxResults)
	settings.MaxListEntries = positiveIntOf(raw["max_list_entries"], DefaultMaxListEntries)
	settings.IncrementalNoticeMaxChars = positiveIntOf(raw["incremental_notice_max_chars"], DefaultIncrementalNoticeMaxChars)
	settings.UseExistingIndex = boolOf(raw["use_existing_index"], false)
	settings.PromptPreferredSkills = stringTupleOf(raw["prompt_preferred_skills"], DefaultPromptPreferredSkills)
	return settings
}

// contextWindowTokens 上下文窗口三级解析。
// 对齐 Python _context_window_tokens（config.py:91-112）：显式 raw 值 →
// model_client 名称 + context_engine fallback → ContextUtils.resolve_context_max
// （core 包延迟导入在 Go 侧按调用语义内联，异常回落 fallback or 200000）。
func contextWindowTokens(config map[string]any, raw map[string]any) int {
	if explicit := optionalPositiveInt(raw["context_window_tokens"]); explicit != nil {
		return *explicit
	}
	modelClient := defaultModelClientConfig(config)
	modelName := strings.TrimSpace(firstNonEmptyStr(modelClient, "model_name", "model"))
	react := mappingOf(config["react"])
	contextEngine := mappingOf(react["context_engine_config"])
	fallback := optionalPositiveInt(contextEngine["context_window_tokens"])
	// ContextUtils.resolve_context_max 内联：无 model_name/fallback/窗口表时用默认 200000
	if modelName == "" && fallback == nil {
		return 200000
	}
	if fallback != nil {
		return *fallback
	}
	return 200000
}

// defaultModelClientConfig 默认模型客户端配置。
// 对齐 Python _default_model_client_config（config.py:115-133）。
func defaultModelClientConfig(config map[string]any) map[string]any {
	models := mappingOf(config["models"])
	if defaults, ok := models["defaults"].([]any); ok {
		var firstCandidate map[string]any
		for _, item := range defaults {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			candidate := mappingOf(m["model_client_config"])
			if len(candidate) == 0 {
				continue
			}
			if b, ok := m["is_default"].(bool); ok && b {
				return candidate
			}
			if firstCandidate == nil {
				firstCandidate = candidate
			}
		}
		if firstCandidate != nil {
			return firstCandidate
		}
	}
	return mappingOf(mappingOf(models["default"])["model_client_config"])
}

// mappingOf any→map（nil/类型不符回落空表）
func mappingOf(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		out := make(map[string]any, len(m))
		for k, v := range m {
			out[k] = v
		}
		return out
	}
	return map[string]any{}
}

// positiveIntOf 正整数回落
func positiveIntOf(value any, def int) int {
	if parsed := optionalPositiveInt(value); parsed != nil {
		return *parsed
	}
	return def
}

// optionalPositiveInt any→正整数（失败 nil）
func optionalPositiveInt(value any) *int {
	switch t := value.(type) {
	case int:
		if t > 0 {
			return &t
		}
	case float64:
		if t > 0 && t == float64(int(t)) {
			i := int(t)
			return &i
		}
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(t)); err == nil && n > 0 {
			return &n
		}
	}
	return nil
}

// ratioOf 占比解析（(0,1] 之外回落默认）
func ratioOf(value any, def float64) float64 {
	switch t := value.(type) {
	case float64:
		if t > 0 && t <= 1 {
			return t
		}
	case int:
		if float64(t) > 0 && float64(t) <= 1 {
			return float64(t)
		}
	}
	return def
}

// boolOf 宽容布尔（对齐 Python _bool 的 {1,true,yes,on,enabled} 集合）
func boolOf(value any, def bool) bool {
	if value == nil {
		return def
	}
	if b, ok := value.(bool); ok {
		return b
	}
	text := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", value)))
	if text == "" {
		return def
	}
	switch text {
	case "1", "true", "yes", "on", "enabled":
		return true
	}
	return false
}

// stringTupleOf 字符串列表（逗号串/数组 → 去重保序）
func stringTupleOf(value any, def []string) []string {
	if value == nil {
		return append([]string(nil), def...)
	}
	var items []any
	switch t := value.(type) {
	case string:
		for _, s := range strings.Split(t, ",") {
			items = append(items, s)
		}
	case []any:
		items = t
	default:
		return append([]string(nil), def...)
	}
	var out []string
	seen := map[string]bool{}
	for _, item := range items {
		text := strings.TrimSpace(fmt.Sprintf("%v", item))
		if text != "" && !seen[text] {
			seen[text] = true
			out = append(out, text)
		}
	}
	return out
}

// firstNonEmptyStr 首个非空字符串键
func firstNonEmptyStr(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
