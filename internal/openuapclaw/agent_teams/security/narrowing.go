// Package security 提供团队权限收窄能力。
//
// 对齐 Python: openjiuwen/agent_teams/security/（narrowing.py）。
// 收窄语义：override 只能收紧（restrict），不能放宽（loosen）——
// 对 override 中列出的每个工具，生效级别取 base 与 override 的 strictest()。
//
// 文件目录：
//
//	security/
//	├── doc.go           # 包文档
//	└── narrowing.go    # NarrowPermissions 收窄 + FormatBasePermissionsForDesc 描述格式化
//
// 对应 Python 代码：openjiuwen/agent_teams/security/narrowing.py
package security

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/utils"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/security"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NarrowPermissions 以工具级 override 收窄基础权限配置。
//
// 对 override 中的每个工具，结果级别取 strictest(base_level, override_level)；
// 工具不在 base["tools"] 中时，base 级别按 defaults 解析
// （逐键 → 通配 "*" → 兜底 ASK）后再取 strictest()。
// 其余字段（defaults/rules/approval_overrides/permission_mode 等）原样保留。
//
// Python: narrow_permissions (narrowing.py:22-66)
// 偏差：Python 无返回值（非法级别抛 ValueError 传播），Go 以 error 显式传播。
func NarrowPermissions(baseConfig map[string]any, toolsOverride map[string]string) (map[string]any, error) {
	// Python: narrowed = copy.deepcopy(base_config)
	narrowed := utils.DeepCopyMap(baseConfig)
	if narrowed == nil {
		narrowed = map[string]any{}
	}
	// Python: base_tools = narrowed.get("tools", {}); defaults = narrowed.get("defaults", {})
	baseTools, _ := narrowed["tools"].(map[string]any)
	if baseTools == nil {
		baseTools = map[string]any{}
	}
	defaults, _ := narrowed["defaults"].(map[string]any)

	for toolName, overrideLevelStr := range toolsOverride {
		// Python: override_level = PermissionLevel(override_level_str) —— 非法串抛 ValueError
		overrideLevel, err := security.ParsePermissionLevel(overrideLevelStr)
		if err != nil {
			return nil, fmt.Errorf("narrow_permissions: 工具 %q 的权限级别 %q 非法: %w", toolName, overrideLevelStr, err)
		}
		baseLevelStr, hasBase := baseTools[toolName].(string)

		var narrowedLevel security.PermissionLevel
		if hasBase {
			// Python: base_level = _parse_level(base_level_str)；_parse_level 解析失败回退 ASK
			baseLevel := parseLevelFallbackASK(baseLevelStr)
			narrowedLevel = security.Strictest(baseLevel, overrideLevel)
		} else {
			// Python: default_str = defaults.get(tool_name) or defaults.get("*")；空值回退 ASK
			defaultLevel := defaultLevelOf(defaults, toolName)
			narrowedLevel = security.Strictest(defaultLevel, overrideLevel)
		}

		// Python: base_tools[tool_name] = narrowed_level.value
		baseTools[toolName] = narrowedLevel.String()
	}

	narrowed["tools"] = baseTools
	return narrowed, nil
}

// FormatBasePermissionsForDesc 将基础权限规则格式化为可读文本。
//
// 生成多行描述：显式工具规则 + defaults 兜底 + 收窄约束说明，
// 用于注入 prompt 或工具描述。tools 与 defaults 均空时返回空串。
// lang 取 "en" 或 "cn"（双语输出）。
//
// Python: format_base_permissions_for_desc (narrowing.py:69-134)
func FormatBasePermissionsForDesc(config map[string]any, lang string) string {
	// Python: tools = config.get("tools", {}); defaults = config.get("defaults", {})
	tools, _ := config["tools"].(map[string]any)
	defaults, _ := config["defaults"].(map[string]any)
	if len(tools) == 0 && len(defaults) == 0 {
		return ""
	}

	var lines []string

	if lang == "cn" {
		lines = append(lines, "当前 teammate 的基础权限规则：")
	} else {
		lines = append(lines, "Current teammate base permissions:")
	}

	// Python: for tool, level in sorted(tools.items()) —— 显式规则按工具名排序
	toolNames := make([]string, 0, len(tools))
	for tool := range tools {
		toolNames = append(toolNames, tool)
	}
	sortStrings(toolNames)
	for _, tool := range toolNames {
		lines = append(lines, fmt.Sprintf("- %s: %v", tool, tools[tool]))
	}

	// Python: wildcard = defaults.get("*") —— 通配兜底展示
	if wildcard, ok := defaults["*"]; ok && wildcard != nil && fmt.Sprintf("%v", wildcard) != "" {
		if lang == "cn" {
			lines = append(lines, fmt.Sprintf("- 其他未列出的工具: %v (defaults 兜底)", wildcard))
		} else {
			lines = append(lines, fmt.Sprintf("- Other tools not listed above: %v (defaults fallback)", wildcard))
		}
	}

	// Python: 收窄规则说明段
	if lang == "cn" {
		lines = append(lines, "")
		lines = append(lines,
			"创建成员的时候，你可以根据任务风险设置更严格的工具权限；"+
				"例如对只读分析任务禁用写入类工具，或将高风险执行类工具从 allow 收紧为 ask/deny。"+
				"不得授予比上述基础权限更宽的权限。")
		lines = append(lines, "收窄规则：只能收紧，不能放宽。")
		lines = append(lines, "ask → deny ✓  |  allow → ask/deny ✓  |  deny → allow/ask ✗ (自动修正)")
	} else {
		lines = append(lines, "")
		lines = append(lines,
			"When creating a member, you may set stricter tool permissions based on task risk; for example, "+
				"disable write tools for read-only analysis tasks, or narrow high-risk execution tools from allow "+
				"to ask/deny. You must not grant permissions broader than the base permissions above.")
		lines = append(lines, "Narrowing rules: only tightening, never loosening.")
		lines = append(lines, "ask → deny ✓  |  allow → ask/deny ✓  |  deny → allow/ask ✗ (auto-corrected)")
	}

	return joinLines(lines)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseLevelFallbackASK 解析权限级别字符串，解析失败回退 ASK。
//
// Python: _parse_level(value) (tiered_policy.py L113-115，Go 侧非导出不可跨包，等价复刻)
func parseLevelFallbackASK(value string) security.PermissionLevel {
	level, err := security.ParsePermissionLevel(value)
	if err != nil {
		return security.PermissionLevelAsk
	}
	return level
}

// defaultLevelOf 从 defaults 解析兜底级别：逐键 → 通配 "*" → 兜底 ASK。
//
// Python: default_str = defaults.get(tool_name) or defaults.get("*")
//
//	default_level = _parse_level(default_str) if default_str else PermissionLevel.ASK
func defaultLevelOf(defaults map[string]any, toolName string) security.PermissionLevel {
	var defaultStr string
	if v, ok := defaults[toolName].(string); ok && v != "" {
		defaultStr = v
	} else if v, ok := defaults["*"].(string); ok && v != "" {
		defaultStr = v
	}
	if defaultStr == "" {
		return security.PermissionLevelAsk
	}
	return parseLevelFallbackASK(defaultStr)
}

// sortStrings 原地排序字符串切片（对齐 Python sorted）。
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// joinLines 以换行拼接多行文本（对齐 Python "\n".join）。
func joinLines(lines []string) string {
	out := ""
	for i, l := range lines {
		if i > 0 {
			out += "\n"
		}
		out += l
	}
	return out
}
