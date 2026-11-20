// Package interrupt 的权限/确认审批选项统一词表。
//
// 本文件对齐 Python jiuwenswarm/agents/harness/common/rails/interrupt/permission_options.py（92 行）。
//
// # 为什么需要统一词表
//
// 审批提示的选项文案同时充当协议取值：渲染端拿到 label/value，回答时 web、CLI
// 回传 option.value || option.label，TUI 一律回传 option.label，后端再把这个
// 字符串解回 approved / auto_confirm / persist_allow。出题端与解题端必须共用一份
// 词表，否则任何一端改动文案都会让回答落到「未知选项」，而未知选项按拒绝处理——
// 用户点了同意，工具却被拒。
//
// 本文件只放稳定标识和它们的别名，不放任何展示文案：展示文案归出题端所有、可以
// 改，而这里的取值不随之变化。
package interrupt

import (
	"regexp"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// AllowOnce 本次允许
	AllowOnce = "allow_once"
	// SessionAllow 会话内记住
	SessionAllow = "session_allow"
	// AlwaysAllow 永久记住（企业版不提供）
	AlwaysAllow = "always_allow"
	// Reject 拒绝
	Reject = "reject"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// optionSeparators 归一分隔符：空格 / 下划线 / 连字符的混用都不影响解析。
	//
	// Python: _SEPARATORS = re.compile(r"[\s_\-]+")
	optionSeparators = regexp.MustCompile(`[\s_\-]+`)

	// optionAliasSources 别名 → 动作的映射来源。
	//
	// Python: _ALIAS_SOURCES (permission_options.py L44-61)
	//
	// 只收录各出题端确实下发过的写法。不要加入 "skip"/"跳过"/"ok" 这类通用词：
	// 别的确认流出现同名按钮会被误判。
	optionAliasSources = []struct {
		action  string
		aliases []string
	}{
		{
			action: AllowOnce,
			aliases: []string{
				"allow_once", "approve", "accept", "proceed",
				"本次允许", "批准", "开始执行", "接收", "接受",
			},
		},
		{
			action:  SessionAllow,
			aliases: []string{"session_allow", "会话内记住"},
		},
		{
			action:  AlwaysAllow,
			aliases: []string{"always_allow", "allow_always", "永久记住", "总是允许"},
		},
		{
			// reject_once 是 ACP 的写法（reject-once）。ACP 走的是
			// session/request_permission，正常不会回到这里，收下只是兜底。
			action: Reject,
			aliases: []string{
				"reject", "reject_once", "拒绝", "继续规划", "其他意见", "keep_planning",
			},
		},
	}

	// optionAliases 归一化别名到动作的索引。
	//
	// Python: _OPTION_ALIASES
	optionAliases = buildOptionAliases()

	// keepPlanningValues 归一化后仍表示「继续规划」而非「拒绝」的取值，
	// 用于挑选写给模型的反馈文案。
	//
	// Python: _KEEP_PLANNING_VALUES
	keepPlanningValues = buildKeepPlanningValues()
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeOptionValue 把回传的选项字符串归一化成可比较的形式。
//
// Python: normalize_option_value(value) (permission_options.py L32-37)
//
// 首尾空白、大小写、空格/下划线/连字符混用都不影响解析：
// "Allow Once"、"allow_once"、"allow-once" 是同一个取值的不同写法。
func NormalizeOptionValue(value any) string {
	text := strings.TrimSpace(anyToString(value))
	if text == "" {
		return ""
	}
	return strings.ToLower(optionSeparators.ReplaceAllString(text, "_"))
}

// ResolvePermissionAction 解出选项对应的动作，无法识别时返回空串。
//
// Python: resolve_permission_action(value) (permission_options.py L70-76)
//
// 返回空串表示「没认出来」，与「用户拒绝」是两回事：调用方应当记录告警后
// 再按拒绝兜底，这样一次解析失败是可诊断的，而不是静默变成一次拒绝。
func ResolvePermissionAction(value any) string {
	return optionAliases[NormalizeOptionValue(value)]
}

// IsKeepPlanningValue 判断拒绝类取值是否属于「继续规划」。
//
// Python: is_keep_planning_value(value) (permission_options.py L79-81)
func IsKeepPlanningValue(value any) bool {
	return keepPlanningValues[NormalizeOptionValue(value)]
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildOptionAliases 构建归一化别名到动作的索引。
func buildOptionAliases() map[string]string {
	result := make(map[string]string, 16)
	for _, source := range optionAliasSources {
		for _, alias := range source.aliases {
			result[NormalizeOptionValue(alias)] = source.action
		}
	}
	return result
}

// buildKeepPlanningValues 构建「继续规划」取值集合。
func buildKeepPlanningValues() map[string]bool {
	aliases := []string{"keep_planning", "继续规划", "其他意见"}
	result := make(map[string]bool, len(aliases))
	for _, alias := range aliases {
		result[NormalizeOptionValue(alias)] = true
	}
	return result
}

// anyToString 把值转成字符串，nil 返回空串。
func anyToString(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
