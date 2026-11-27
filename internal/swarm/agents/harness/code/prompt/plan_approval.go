// Plan approval 常量、动作描述构建器与反馈文案。
//
// 本文件对齐 Python jiuwenswarm/agents/harness/code/prompt/plan_approval.py。
// 已移植：三个稳定取值常量、PLAN_EXECUTE_CTX_KEY 等 ctx/params 标记、
// _PLAN_APPROVAL_ACTIONS 结构 + label 表、build_plan_approval_actions、
// PLAN_SKIP_FEEDBACK / PLAN_SKIP_TURN_OUTPUT 文案。
// 未移植（消费方在 Go 未实施的 plan approval rail，⤴️15.20 随批补）：
// PENDING_APPROVAL_MARKER / APPROVED_NOTIFICATION / FEEDBACK_INJECTION 三段
// 注入模板（tool_result 后缀与下一轮 system-reminder）。
// 结构与文案分离：取值语言无关，label 归出题端（前端）所有。
package prompt

import "strings"

// ──────────────────────────── 常量 ────────────────────────────

const (
	// PlanExecutePayloadKey Web 专用"执行"：批准并退出 plan，本轮到此结束；
	// 真正的执行由前端紧接着补发的普通消息开启新一轮完成。
	// Python: PLAN_EXECUTE_PAYLOAD_KEY = "plan_execute"
	PlanExecutePayloadKey = "plan_execute"
	// PlanSkipPayloadKey Web 专用"跳过"：不退出 plan，也不继续修改，直接结束本轮。
	// Python: PLAN_SKIP_PAYLOAD_KEY = "plan_skip"
	PlanSkipPayloadKey = "plan_skip"
	// PlanRevisePayloadKey Web 专用"下一步"：不退出 plan，按修改意见续跑。
	// Python: PLAN_REVISE_PAYLOAD_KEY = "plan_revise"
	PlanRevisePayloadKey = "plan_revise"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// planExecuteOptionValues 只认 build_plan_approval_actions 实际下发的取值。
// 不要放 "skip"/"跳过" 这类通用词：消费它的 if/elif 是所有确认类中断共用的
// 选项映射，别的确认流出现同名按钮会被误判。
// Python: PLAN_EXECUTE_OPTION_VALUES = frozenset({"plan_execute"})
var planExecuteOptionValues = map[string]struct{}{PlanExecutePayloadKey: {}}

// planSkipOptionValues Python: PLAN_SKIP_OPTION_VALUES = frozenset({"plan_skip"})
var planSkipOptionValues = map[string]struct{}{PlanSkipPayloadKey: {}}

// planReviseOptionValues Python: PLAN_REVISE_OPTION_VALUES = frozenset({"plan_revise"})
var planReviseOptionValues = map[string]struct{}{PlanRevisePayloadKey: {}}

// planSkipFeedbackMap "跳过"时写给模型的反馈文案。
// Python: PLAN_SKIP_FEEDBACK
var planSkipFeedbackMap = map[string]string{
	"cn": "用户选择跳过本次计划审批，暂不执行，也未提出修改意见。",
	"en": "The user skipped this plan approval: do not implement yet, and no revision notes were given.",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsPlanExecuteOptionValue value 是否为 Web"执行"取值。
func IsPlanExecuteOptionValue(value string) bool {
	_, ok := planExecuteOptionValues[value]
	return ok
}

// IsPlanSkipOptionValue value 是否为 Web"跳过"取值。
func IsPlanSkipOptionValue(value string) bool {
	_, ok := planSkipOptionValues[value]
	return ok
}

// IsPlanReviseOptionValue value 是否为 Web"下一步"取值。
func IsPlanReviseOptionValue(value string) bool {
	_, ok := planReviseOptionValues[value]
	return ok
}

// PlanSkipFeedback 按语言取"跳过"时写给模型的反馈文案。
// Python: plan_skip_feedback(language)（plan_approval.py:108-115）
func PlanSkipFeedback(language string) string {
	lang := strings.ToLower(strings.TrimSpace(language))
	if strings.HasPrefix(lang, "en") {
		return planSkipFeedbackMap["en"]
	}
	return planSkipFeedbackMap["cn"]
}

// ──────────────────────────── 常量（plan_approval.py 其余段） ────────────────────────────

const (
	// PlanExecuteCtxKey ctx.extra 标记：PlanApprovalInterruptRail 在恢复中断时
	// 设置，CodeAgentModeRail 在 exit_plan_mode 跑完后据此结束本轮。
	// Python: PLAN_EXECUTE_CTX_KEY = "_plan_execute_deferred"
	PlanExecuteCtxKey = "_plan_execute_deferred"
	// PlanReminderOriginalQueryKey 进入 plan 那轮注入 <system-reminder> 前保存的
	// 用户原文键（写会话历史时还原用）。
	// Python: PLAN_REMINDER_ORIGINAL_QUERY_KEY = "_plan_reminder_original_query"
	PlanReminderOriginalQueryKey = "_plan_reminder_original_query"
	// ApproveCmdPrefix 批准命令前缀（前端补发的 query 文本）。
	// Python: APPROVE_CMD_PREFIX = "plan.approve"
	ApproveCmdPrefix = "plan.approve"
	// RejectCmdPrefix 拒绝命令前缀。
	// Python: REJECT_CMD_PREFIX = "plan.reject"
	RejectCmdPrefix = "plan.reject"
	// PlanUserApprovedFlag 用户本轮显式批准计划的标记（params 标记）。
	// Python: PLAN_USER_APPROVED_FLAG = "_plan_user_approved"
	PlanUserApprovedFlag = "_plan_user_approved"
)

// planApprovalActions 三个按钮的结构：结构与文案分离，label 按语言取。
// Python: _PLAN_APPROVAL_ACTIONS
var planApprovalActions = []map[string]string{
	{"kind": "execute", "value": PlanExecutePayloadKey, "requires_input": "no"},
	{"kind": "skip", "value": PlanSkipPayloadKey, "requires_input": "empty"},
	{"kind": "revise", "value": PlanRevisePayloadKey, "requires_input": "yes"},
}

// planActionLabels 三个按钮的展示文案，按语言取。
// Python: _PLAN_ACTION_LABELS
var planActionLabels = map[string]map[string]string{
	"cn": {"execute": "执行", "skip": "跳过", "revise": "下一步"},
	"en": {"execute": "Execute", "skip": "Skip", "revise": "Next"},
}

// planSkipTurnOutput "跳过"后写给用户的轮次输出。
// Python: PLAN_SKIP_TURN_OUTPUT
var planSkipTurnOutput = map[string]string{
	"cn": "已跳过本次计划审批。计划已保存，当前仍处于计划模式，可以继续补充意见。",
	"en": "Plan approval skipped. The plan is saved and you are still in plan mode; send revision notes whenever you are ready.",
}

// ──────────────────────────── 导出函数（构建器） ────────────────────────────

// BuildPlanApprovalActions 构建 Web 计划审批动作描述列表。
//
// Web 弹窗由"执行"按钮 + 修改框 + 动态按钮组成：修改框为空时按钮是"跳过"，
// 有内容时是"下一步"。三者都复用既有的 approve / reject 通道：
//
//   - plan_execute：批准并退出 plan，本轮到此结束；前端随后补发一条普通
//     非 plan 消息来真正执行。
//   - plan_skip：留在 plan，结束本轮，不执行也不修改。
//   - plan_revise + custom_input：留在 plan，按修改意见继续完善同一份计划。
//
// TUI 不读取该字段，仍使用 questions[].options 的 approve / reject。
//
// Python: build_plan_approval_actions(language)（plan_approval.py:120-144）
func BuildPlanApprovalActions(language string) []map[string]string {
	labels, ok := planActionLabels[language]
	if !ok {
		labels = planActionLabels["cn"]
	}
	result := make([]map[string]string, 0, len(planApprovalActions))
	for _, action := range planApprovalActions {
		item := make(map[string]string, len(action)+1)
		for key, value := range action {
			item[key] = value
		}
		item["label"] = labels[action["kind"]]
		result = append(result, item)
	}
	return result
}

// PlanSkipTurnOutput 按语言取"跳过"后写给用户的轮次输出。
func PlanSkipTurnOutput(language string) string {
	lang := strings.ToLower(strings.TrimSpace(language))
	if strings.HasPrefix(lang, "en") {
		return planSkipTurnOutput["en"]
	}
	return planSkipTurnOutput["cn"]
}
