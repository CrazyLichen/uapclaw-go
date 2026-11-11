// Package rails 的计划审批消息解析。
//
// 本文件对齐 Python jiuwenswarm/agents/harness/code/rails/code_plan_approval_interrupt_rail.py
// 中与「计划审批文案」相关的四个纯字符串函数（L93-190）。
//
// # 为什么要识别这段文案
//
// exit_plan_mode 的确认中断会把整份计划正文塞进 message。中断投影层拿到这条
// message 后要判断「这是否是计划审批」，并把它渲染成结构化卡片而非普通确认框：
// 剥离原生附带的文字版选项列表，抽出计划正文与语言，再生成前端可用的选项。
//
// 这些函数是纯字符串处理，不依赖 rail 生命周期，故独立成文件。
package rails

import (
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// planApprovalTitleCN 中文计划审批标题
	planApprovalTitleCN = "**计划审批**"
	// planApprovalTitleEN 英文计划审批标题
	planApprovalTitleEN = "**Plan Approval**"
	// planApprovalIntroCN 中文引导语
	planApprovalIntroCN = "Agent 已完成计划制定，等待你审批："
	// planApprovalIntroEN 英文引导语
	planApprovalIntroEN = "The agent has completed a plan and is requesting your approval:"
	// planEmptyIntroCN 中文空计划说明
	planEmptyIntroCN = "Agent 请求退出 plan 模式，但计划文件为空。"
	// planEmptyIntroEN 英文空计划说明
	planEmptyIntroEN = "The agent is requesting to exit plan mode, but the plan file is empty."
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// planChoiceMarkers 文案里附带的选项列表分隔标记。
	//
	// Python: _PLAN_CHOICE_MARKERS
	planChoiceMarkers = []string{"\n---\n请选择：", "\n---\nChoose:"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsPlanApprovalMessage 判断文案是否为计划审批中断。
//
// Python: is_plan_approval_message(message) (code_plan_approval_interrupt_rail.py L102-105)
func IsPlanApprovalMessage(message string) bool {
	normalized := strings.TrimSpace(message)
	return strings.Contains(normalized, planApprovalTitleCN) ||
		strings.Contains(normalized, planApprovalTitleEN)
}

// StripInlinePlanApprovalChoices 去掉计划审批文案里文字版的选项列表。
//
// Python: strip_inline_plan_approval_choices(message) (L108-113)
//
// 前端会渲染结构化按钮，原生文字列表必须剥离，否则用户看到两套选项。
func StripInlinePlanApprovalChoices(message string) string {
	for _, marker := range planChoiceMarkers {
		if index := strings.Index(message, marker); index >= 0 {
			return strings.TrimRight(message[:index], " \t\r\n")
		}
	}
	return message
}

// ExtractPlanApprovalContent 从计划审批文案中抽出 (计划正文, 语言)。
//
// Python: extract_plan_approval_content(message) (L116-141)
//
// 语言判定靠标题前缀：命中英文标题返回 "en"，命中中文标题返回 "cn"，
// 都不命中按 "cn" 兜底（Python 的默认分支）。
func ExtractPlanApprovalContent(message string) (string, string) {
	stripped := strings.TrimSpace(StripInlinePlanApprovalChoices(message))

	if strings.HasPrefix(stripped, planApprovalTitleEN) {
		body := strings.TrimLeft(stripped[len(planApprovalTitleEN):], " \t\r\n")
		if strings.HasPrefix(body, planEmptyIntroEN) {
			return "", "en"
		}
		if strings.HasPrefix(body, planApprovalIntroEN) {
			body = strings.TrimLeft(body[len(planApprovalIntroEN):], " \t\r\n")
		}
		return strings.TrimSpace(body), "en"
	}

	if strings.HasPrefix(stripped, planApprovalTitleCN) {
		body := strings.TrimLeft(stripped[len(planApprovalTitleCN):], " \t\r\n")
		if strings.HasPrefix(body, planEmptyIntroCN) {
			return "", "cn"
		}
		if strings.HasPrefix(body, planApprovalIntroCN) {
			body = strings.TrimLeft(body[len(planApprovalIntroCN):], " \t\r\n")
		}
		return strings.TrimSpace(body), "cn"
	}

	return stripped, "cn"
}

// BuildPlanApprovalOptionsFromMessage 从计划审批文案生成结构化的批准/拒绝选项。
//
// Python: build_plan_approval_options_from_message(message) (L144-168)
//
// 分「有正文」与「空计划」两种文案：空计划时批准描述要说明「无计划内容」，
// 否则用户以为批准的是有内容的计划。
func BuildPlanApprovalOptionsFromMessage(message string) []map[string]any {
	stripped := strings.TrimSpace(StripInlinePlanApprovalChoices(message))
	_, language := ExtractPlanApprovalContent(message)

	if language == "en" {
		if strings.Contains(stripped, planEmptyIntroEN) {
			return []map[string]any{
				{"label": "Approve", "value": "approve", "description": "Exit plan mode (no plan content)"},
				{"label": "Reject", "value": "reject", "description": "Stay in plan mode, let the agent write a plan first"},
			}
		}
		return []map[string]any{
			{"label": "Approve", "value": "approve", "description": "Exit plan mode and begin implementation"},
			{"label": "Reject", "value": "reject", "description": "Stay in plan mode, the agent will revise the plan based on your feedback"},
		}
	}

	if strings.Contains(stripped, planEmptyIntroCN) {
		return []map[string]any{
			{"label": "批准", "value": "approve", "description": "退出 plan 模式（无计划内容）"},
			{"label": "拒绝", "value": "reject", "description": "留在 plan 模式，让 Agent 先写好计划"},
		}
	}
	return []map[string]any{
		{"label": "批准", "value": "approve", "description": "退出 plan 模式，开始执行计划"},
		{"label": "拒绝", "value": "reject", "description": "留在 plan 模式，Agent 将根据你的反馈修改计划"},
	}
}
