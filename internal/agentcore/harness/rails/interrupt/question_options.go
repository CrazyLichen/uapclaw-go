// Package interrupt 的审批选项选择。
//
// 本文件对齐 Python interrupt_helpers.py 的 _plan_approval_interrupt_options
// （L1676-1686）、_fallback_interrupt_options（L1689-1696）与
// _question_options_from_ui_options（L1701-1722）。
//
// # 三条选择路径（优先级由高到低）
//
//  1. CLI 单次允许模式：中断元数据声明 process_cli_allow_once_only 时，
//     只给「本次允许 + 拒绝」两档——CLI 没有持久化授权的交互能力
//  2. 中断自带的 ui_options：rail 自己声明了选项就用它
//  3. 兜底：计划审批专用选项（批准/拒绝），否则用四档权限选项
//
// 企业版在第 2、3 步都要剔除 always 档。
package interrupt

import (
	"strings"

	planrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/code/rails"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// processCLIAllowOnceOnlyKey 中断元数据里标记「只允许一次」的键
	//
	// Python: metadata.get("process_cli_allow_once_only")
	processCLIAllowOnceOnlyKey = "process_cli_allow_once_only"
	// exitPlanModeToolName 触发计划审批的工具名
	//
	// Python: tool_name == "exit_plan_mode"
	exitPlanModeToolName = "exit_plan_mode"
	// confirmInterruptSource 确认类中断的来源标记
	confirmInterruptSource = "confirm_interrupt"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// planApprovalInterruptOptions 返回计划审批专用的批准/拒绝选项。
//
// Python: _plan_approval_interrupt_options(source, tool_name, message) (L1676-1686)
//
// 三个条件同时满足才生效：确认类中断 + exit_plan_mode + 文案是计划审批。
// 不满足返回 nil，由调用方回落到四档选项。
func planApprovalInterruptOptions(source string, toolName string, message string) []map[string]any {
	if source != confirmInterruptSource ||
		toolName != exitPlanModeToolName ||
		!planrails.IsPlanApprovalMessage(message) {
		return nil
	}
	return planrails.BuildPlanApprovalOptionsFromMessage(message)
}

// fallbackInterruptOptions 返回兜底选项：计划审批专用，否则四档权限选项。
//
// Python: _fallback_interrupt_options(source, tool_name, message) (L1689-1696)
func fallbackInterruptOptions(source string, toolName string, message string) []map[string]any {
	if options := planApprovalInterruptOptions(source, toolName, message); options != nil {
		return options
	}
	return defaultInterruptOptions()
}

// questionOptionsFromUIOptions 从 ui_options 派生问题选项。
//
// Python: _question_options_from_ui_options(value_obj, source, tool_name, message) (L1701-1722)
//
// 三步（顺序不可调换）：
//  1. CLI 单次允许模式：只留「本次允许 + 拒绝」
//  2. 归一化 ui_options，丢弃 label 为空的项；有结果时企业版再剔除 always 档
//  3. 无结果（或企业版剔除后为空）→ 兜底
func questionOptionsFromUIOptions(
	valueObj any,
	source string,
	toolName string,
	message string,
) []map[string]any {
	if flag, ok := extractInterruptMetadata(valueObj)[processCLIAllowOnceOnlyKey].(bool); ok && flag {
		result := make([]map[string]any, 0, 2)
		for _, option := range defaultInterruptOptions() {
			if option["value"] == AllowOnce || option["value"] == Reject {
				result = append(result, option)
			}
		}
		return result
	}

	options := make([]map[string]any, 0, 4)
	for _, raw := range extractUIOptions(valueObj) {
		normalized := normalizeQuestionOption(raw)
		if label, _ := normalized["label"].(string); strings.TrimSpace(label) != "" {
			options = append(options, normalized)
		}
	}

	if len(options) > 0 {
		// 企业版统一隐藏 always 档：权限「永久记住」与技能演进「总是允许」都不保留。
		if IsEnterprise() {
			filtered := make([]map[string]any, 0, len(options))
			for _, option := range options {
				if !isAlwaysAllowOption(option) {
					filtered = append(filtered, option)
				}
			}
			if len(filtered) == 0 {
				return fallbackInterruptOptions(source, toolName, message)
			}
			return filtered
		}
		return options
	}
	return fallbackInterruptOptions(source, toolName, message)
}
