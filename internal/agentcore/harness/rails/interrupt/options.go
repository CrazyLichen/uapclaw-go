// Package interrupt 的审批选项构建与归一化。
//
// 本文件对齐 Python jiuwenswarm/agents/harness/common/rails/interrupt/interrupt_helpers.py
// 中与「选项提取 / 归一化 / 默认档位」相关的助手（L1528-1670）。
//
// # 企业版差异
//
// 企业版不提供 always 档（权限卡「永久记住」/ 演进卡「总是允许」）：写回本地
// user overlay / approval_overrides 或跨次自动放行，与 Manager 权威源不一致。
// 个人版仍保留四档权限选项。
package interrupt

import (
	"regexp"
	"strings"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// permanentRememberHintPattern 权限 ask 文案里的「永久记住」提示。
	//
	// Python: _PERMANENT_REMEMBER_HINT_RE (interrupt_helpers.py L1618-1620)
	//
	// agent-core 权限 ask 文案里会附带「选择「永久记住」…」提示；
	// 企业版不出该按钮时一并去掉。
	permanentRememberHintPattern = regexp.MustCompile(`[；;]?\s*选择「永久记住」[^。\n]*。?`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// extractUIOptions 从 value 中取 ui_options 列表，只保留 dict 项。
//
// Python: _extract_ui_options(value_obj) (interrupt_helpers.py L1528-1539)
func extractUIOptions(valueObj any) []map[string]any {
	mapping, ok := valueObj.(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := mapping["ui_options"].([]any)
	if !ok {
		return nil
	}
	result := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if typed, ok := item.(map[string]any); ok {
			result = append(result, typed)
		}
	}
	return result
}

// normalizeQuestionOption 归一化单个选项。
//
// Python: _normalize_question_option(option) (interrupt_helpers.py L1622-1636)
//
// label 优先取 label 字段，缺失回落 value；description 一律保留（可空）；
// value / preview 仅在非空字符串时保留。
func normalizeQuestionOption(option map[string]any) map[string]any {
	label := strings.TrimSpace(anyToString(option["label"]))
	if label == "" {
		label = strings.TrimSpace(anyToString(option["value"]))
	}
	normalized := map[string]any{
		"label":       label,
		"description": strings.TrimSpace(anyToString(option["description"])),
	}
	if value, ok := option["value"].(string); ok && value != "" {
		normalized["value"] = value
	}
	if preview, ok := option["preview"].(string); ok && strings.TrimSpace(preview) != "" {
		normalized["preview"] = preview
	}
	return normalized
}

// defaultInterruptOptions 返回权限审批的兜底选项。
//
// Python: _default_interrupt_options() (interrupt_helpers.py L1649-1665)
//
// value 取自 permission_options 词表，与解析回答用的是同一份：
// web / CLI 回传 value，TUI 回传 label，两条路都要能被解出同一个动作。
// 企业版不插入「永久记住」档。
func defaultInterruptOptions() []map[string]any {
	options := []map[string]any{
		{"value": AllowOnce, "label": "本次允许", "description": "仅本次授权执行"},
		{"value": SessionAllow, "label": "会话内记住", "description": "本次会话内自动放行同类操作"},
		{"value": Reject, "label": "拒绝", "description": "拒绝执行此工具"},
	}
	if !IsEnterprise() {
		options = append(options[:2], append([]map[string]any{
			{"value": AlwaysAllow, "label": "永久记住", "description": "写回磁盘，所有会话均自动放行"},
		}, options[2:]...)...)
	}
	return options
}

// isAlwaysAllowOption 判断选项是否为 always 档。
//
// Python: _is_always_allow_option(option) (interrupt_helpers.py L1667-1669)
//
// 权限「永久记住」与演进「总是允许」均映射到 AlwaysAllow。
func isAlwaysAllowOption(option map[string]any) bool {
	value := option["value"]
	if value == nil || anyToString(value) == "" {
		value = option["label"]
	}
	return ResolvePermissionAction(value) == AlwaysAllow
}

// stripPermanentRememberHint 去掉「永久记住」提示。
//
// Python: _strip_permanent_remember_hint(message) (interrupt_helpers.py L1672-1673)
func stripPermanentRememberHint(message string) string {
	return strings.TrimSpace(permanentRememberHintPattern.ReplaceAllString(message, ""))
}
