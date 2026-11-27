// Plan approval 常量与反馈文案。
//
// 本文件对齐 Python jiuwenswarm/agents/harness/code/prompt/plan_approval.py
// 中被 server adapter（_build_interactive_input_from_answers）消费的常量部分：
// plan_execute / plan_skip / plan_revise 三个稳定取值与"跳过"反馈文案。
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
