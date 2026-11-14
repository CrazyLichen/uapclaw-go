// 中断答案归属：把恢复输入翻译成按 interrupt_id 索引、带 owner 的答案。
// 这三者是 Python ToolInterruptHandler 的 _attribute_answers / _call_answers /
// _unanswered_interrupts 的直接对应物，也是"答案按 interrupt_owner 路由"的
// 实现核心：
//
//   - 归属：每条答案带上被回答中断的 owner 与 kind，rail 只读自己 owner 那份
//     （InterruptOwnerMixin.find_own_answer 的 Go 对应读 ResumeInterruptAnswersKey）
//   - 分发：把 {interrupt_id: value} 重排成 {tool_call_id: {owner: answer}}，
//     上一轮答案先铺底、本轮覆盖
//   - 未答：本轮没答到的中断要保留其 id，下一轮 commit 时不能重新发号
//
// 对应 Python 代码：openjiuwen/core/single_agent/interrupt/handler.py（L450-L565）
package interrupt

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PendingInterrupt 一个待决中断的宿主视图。
// Python: state.PendingInterrupt(request.interrupt_id, call_id, kind)
type PendingInterrupt struct {
	// InterruptID 中断唯一标识
	InterruptID string
	// ToolCallID 该中断挂在其上的工具调用标识
	ToolCallID string
	// Kind 中断类型
	Kind string
}

// MatchedAnswers 一次恢复输入回答了哪些待决中断。
// Python: state.MatchedAnswers(answers, unmatched)
type MatchedAnswers struct {
	// Answers {interrupt_id: value}，只含被答到的中断
	Answers map[string]any
	// Unmatched 输入里那些对不上任何待决中断的键（如过期答案）
	Unmatched []string
}

// CallAnswers 按 call_id → owner 组织的答案表。
// Python: _CallAnswers = Dict[str, Dict[str, InterruptAnswer]]
type CallAnswers map[string]map[string]*saschema.InterruptAnswer

// ──────────────────────────── 全局变量 ────────────────────────────

// approvedAnswerKinds 只有这几种中断类型的答案形如 {"approved": ...}。
// 用 kind 白名单挡住"宿主把 approval 形状的答案发给别的类型"（例如技能演化
// 审批期望的是 {"action": ...}），避免误开 auto_confirm。
//
// 必须包含 InterruptKindUndeclared：抛出方未声明 kind 的 rail（例如
// AskUserRail）同样以 {"approved": ...} 作答，Python 侧 APPROVED_ANSWER_KINDS
// 也把 UNDECLARED 列入（answers.py:47-52，注释明写 "an undeclared kind included"）。
// 漏掉它会令这类 rail 的 auto_confirm 永远写不进 session。
// Python: APPROVED_ANSWER_KINDS
var approvedAnswerKinds = map[string]struct{}{
	saschema.InterruptKindPermission:       {},
	saschema.InterruptKindConfirm:          {},
	saschema.InterruptKindTeamToolApproval: {},
	saschema.InterruptKindUndeclared:       {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// PendingInterrupts 列出会话中断状态里全部待决中断（含子 Agent 内部的）。
// 非 ToolInterruptionState 一律视为无待决中断。
// Python: state.pending_interrupts(state)
func PendingInterrupts(interruptState *saschema.ToolInterruptionState) []PendingInterrupt {
	if interruptState == nil {
		return nil
	}
	pending := make([]PendingInterrupt, 0)
	for _, entry := range interruptState.InterruptedTools {
		if entry == nil {
			continue
		}
		// InterruptRequests 的键是 inner call_id（对齐 Python state.py L66 注释）
		for callID, request := range entry.InterruptRequests {
			pending = append(pending, PendingInterrupt{
				InterruptID: requestIDOf(request),
				ToolCallID:  callID,
				Kind:        requestKindOf(request),
			})
		}
	}
	return pending
}

// MatchAnswers 把一次恢复输入翻译成按 interrupt_id 索引的答案。
// 取值优先按 interrupt_id（InterruptAnswers），其次按 tool_call_id（UserInputs）。
// 两个映射里对不上任何待决中断的键会被记入 Unmatched，供调用方记日志。
// Python: state.match_answers(user_input, pending)
func MatchAnswers(userInput *interaction.InteractiveInput, pending []PendingInterrupt) MatchedAnswers {
	answers := make(map[string]any, len(pending))
	if userInput == nil {
		return MatchedAnswers{Answers: answers}
	}

	for _, interrupt := range pending {
		if value, ok := userInput.InterruptAnswers[interrupt.InterruptID]; ok {
			answers[interrupt.InterruptID] = value
			continue
		}
		if value, ok := userInput.UserInputs[interrupt.ToolCallID]; ok {
			answers[interrupt.InterruptID] = value
		}
	}

	callIDs := make(map[string]struct{}, len(pending))
	interruptIDs := make(map[string]struct{}, len(pending))
	for _, interrupt := range pending {
		callIDs[interrupt.ToolCallID] = struct{}{}
		interruptIDs[interrupt.InterruptID] = struct{}{}
	}

	unmatched := make([]string, 0)
	for key := range userInput.UserInputs {
		if _, ok := callIDs[key]; !ok {
			unmatched = append(unmatched, key)
		}
	}
	for key := range userInput.InterruptAnswers {
		if _, ok := interruptIDs[key]; !ok {
			unmatched = append(unmatched, key)
		}
	}
	return MatchedAnswers{Answers: answers, Unmatched: unmatched}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// attributeAnswers 把本次恢复输入翻译成按 interrupt_id 索引、带 owner 的答案。
// 这是 rail 收到的答案的**唯一翻译点**（rail 侧用 FindOwnAnswer 读取）；
// 原始输入仍留在 resume user input 键下，供宿主自己的 rail 使用。
// 三种输入形态（对齐 Python 注释）：
//  1. InteractiveInput：按 match_answers 翻译（answer(interrupt_id, v)，
//     或 update(tool_call_id, v) 答该调用上挂起的中断）；
//  2. 键含待决 call id 的 dict：只答这些调用，各用自己的值；
//  3. 其它输入（纯文本、别的 dict）：提供给每个调用的待决中断。
//     纯文本标记 Unaddressed：它是新消息而非答案。
//
// Python: _attribute_answers(state, user_input)
func attributeAnswers(
	interruptState *saschema.ToolInterruptionState,
	userInput any,
) map[string]*saschema.InterruptAnswer {
	if userInput == nil {
		return map[string]*saschema.InterruptAnswer{}
	}

	pending := PendingInterrupts(interruptState)

	// requests：interrupt_id → 中断请求，用于取 owner 与 kind。
	// 注意 InterruptRequests 的键是 call_id，这里重排为 interrupt_id
	// （对齐 Python _attribute_answers 的 requests 构造）。
	requests := make(map[string]saschema.InterruptRequester)
	for _, entry := range interruptState.InterruptedTools {
		if entry == nil {
			continue
		}
		for _, request := range entry.InterruptRequests {
			requests[requestIDOf(request)] = request
		}
	}

	var values map[string]any
	switch typed := userInput.(type) {
	case *interaction.InteractiveInput:
		matched := MatchAnswers(typed, pending)
		if len(matched.Unmatched) > 0 {
			logger.Info(logComponent).
				Strs("unmatched", matched.Unmatched).
				Msg("ignoring answers that address no pending interrupt")
		}
		values = matched.Answers
	case map[string]any:
		// 键含待决 call id → 只答这些调用
		answered := make(map[string]any)
		for _, interrupt := range pending {
			if value, ok := typed[interrupt.ToolCallID]; ok {
				answered[interrupt.InterruptID] = value
			}
		}
		if len(answered) > 0 {
			values = answered
			break
		}
		// 否则退化为"提供给每个调用"
		values = make(map[string]any, len(pending))
		for _, interrupt := range pending {
			values[interrupt.InterruptID] = userInput
		}
	default:
		values = make(map[string]any, len(pending))
		for _, interrupt := range pending {
			values[interrupt.InterruptID] = userInput
		}
	}

	_, plainText := userInput.(string)
	answers := make(map[string]*saschema.InterruptAnswer, len(values))
	for interruptID, value := range values {
		request := requests[interruptID]
		answers[interruptID] = &saschema.InterruptAnswer{
			Owner:       requestOwnerOf(request),
			Kind:        requestKindOf(request),
			Value:       value,
			Request:     requestAsInterruptRequest(request),
			Unaddressed: plainText,
		}
	}
	return answers
}

// callAnswers 整理重放期间 rail 该读到的答案：上一轮的铺底，本轮覆盖。
// 子 Agent 调用只暴露挂在自己（outer）id 下的答案，用于门控该子 Agent 调用
// 本身的 rail；子 Agent 会自己归属它自己的恢复输入。
// Python: _call_answers(state, fresh_answers)
func callAnswers(
	interruptState *saschema.ToolInterruptionState,
	freshAnswers map[string]*saschema.InterruptAnswer,
) CallAnswers {
	result := make(CallAnswers, len(interruptState.InterruptedTools))

	for _, entry := range interruptState.InterruptedTools {
		if entry == nil {
			continue
		}
		// 1. 上一轮答案先铺底
		for callID, answers := range entry.CarriedAnswers {
			if result[callID] == nil {
				result[callID] = make(map[string]*saschema.InterruptAnswer, len(answers))
			}
			for owner, answer := range answers {
				result[callID][owner] = answer
			}
		}
		if entry.IsSubAgent {
			continue
		}
		// 2. 本轮答案覆盖
		for callID, request := range entry.InterruptRequests {
			answer, ok := freshAnswers[requestIDOf(request)]
			if !ok || answer == nil {
				continue
			}
			if result[callID] == nil {
				result[callID] = make(map[string]*saschema.InterruptAnswer, 1)
			}
			result[callID][answer.Owner] = answer
		}
	}
	return result
}

// unansweredInterrupts 返回本次恢复没答到的中断，用于保留其 id。
// 子 Agent 条目不参与：子 Agent 自己发号。
// Python: _unanswered_interrupts(state, fresh_answers)
func unansweredInterrupts(
	interruptState *saschema.ToolInterruptionState,
	freshAnswers map[string]*saschema.InterruptAnswer,
) map[string]saschema.AnswerOwnerAndID {
	result := make(map[string]saschema.AnswerOwnerAndID)

	for _, entry := range interruptState.InterruptedTools {
		if entry == nil || entry.IsSubAgent {
			continue
		}
		for callID, request := range entry.InterruptRequests {
			if _, answered := freshAnswers[requestIDOf(request)]; answered {
				continue
			}
			result[callID] = saschema.AnswerOwnerAndID{
				Owner:       requestOwnerOf(request),
				InterruptID: requestIDOf(request),
			}
		}
	}
	return result
}

// carryAnswers 把本轮又暂停的调用的答案保留到下一轮。
// 若同一 rail 在同一调用上又重新提问，说明上轮答案没被接受，该答案被丢弃。
// 现在暂停进子 Agent 的调用，保留给其自己（outer）id 的答案。
// Python: _carry_answers(interrupted_tools, call_answers)
func carryAnswers(
	interruptedTools map[string]*saschema.ToolInterruptEntry,
	answers CallAnswers,
) {
	for outerID, entry := range interruptedTools {
		if entry == nil {
			continue
		}

		// asking: inner call_id → 该调用上正在提问的 owner。
		// InterruptRequests 的键正是 inner call_id。
		asking := make(map[string]string)
		callIDs := map[string]struct{}{outerID: {}}
		if !entry.IsSubAgent {
			for callID, request := range entry.InterruptRequests {
				asking[callID] = requestOwnerOf(request)
				callIDs[callID] = struct{}{}
			}
		}

		for callID := range callIDs {
			kept := make(map[string]*saschema.InterruptAnswer)
			for owner, answer := range answers[callID] {
				if owner == asking[callID] {
					// 同一 rail 又提问 → 上轮答案未被接受，丢弃
					continue
				}
				if answer == nil {
					continue
				}
				carried := *answer
				carried.Carried = true
				kept[owner] = &carried
			}
			if len(kept) > 0 {
				if entry.CarriedAnswers == nil {
					entry.CarriedAnswers = make(map[string]map[string]*saschema.InterruptAnswer)
				}
				entry.CarriedAnswers[callID] = kept
			}
		}
	}
}

// saveAutoConfirm 记住以 auto_confirm 作答的批准。
// 只有 kind 在 approvedAnswerKinds 里的中断才可能带 auto_confirm 形状，
// 借此挡住"宿主把 approval 形状的答案发给别的类型"（如技能演化审批期望
// {"action": ...}），避免误开自动确认。
// Python: _save_auto_confirm(answers, session)
func saveAutoConfirm(
	answers map[string]*saschema.InterruptAnswer,
	sess sessioninterfaces.SessionFacade,
) {
	if sess == nil {
		return
	}

	config := make(map[string]any)
	if raw, _ := sess.GetState(state.StringKey(saschema.InterruptAutoConfirmKey)); raw != nil {
		if existing, ok := raw.(map[string]any); ok {
			config = existing
		}
	}

	for _, answer := range answers {
		if answer == nil {
			continue
		}
		if _, approvedKind := approvedAnswerKinds[answer.Kind]; !approvedKind {
			continue
		}
		value, ok := answer.Value.(map[string]any)
		if !ok {
			continue
		}
		autoConfirm, _ := value["auto_confirm"].(bool)
		if !autoConfirm || answer.Request == nil || answer.Request.AutoConfirmKey == "" {
			continue
		}
		config[answer.Request.AutoConfirmKey] = true
	}

	sess.UpdateState(map[string]any{saschema.InterruptAutoConfirmKey: config})
}

// requestIDOf 取中断请求的 interrupt_id。
func requestIDOf(request saschema.InterruptRequester) string {
	// Python: request.interrupt_id（基类属性，任意子类多态可读）。
	// Go 经 InterruptRequester.GetInterruptID 接口等价实现；旧式 *InterruptRequest
	// 断言对 ClientToolRequest 等子类失效（2026-11-14 client tool 恢复链实锤）。
	if request == nil {
		return ""
	}
	return request.GetInterruptID()
}

// requestOwnerOf 取中断请求的 interrupt_owner。
// 走 InterruptRequester 的 GetInterruptOwner，子类（如 AskUserRequest）通过
// 嵌入的 InterruptRequest 自动继承，无需各自实现。
func requestOwnerOf(request saschema.InterruptRequester) string {
	if request == nil {
		return ""
	}
	return request.GetInterruptOwner()
}

// requestKindOf 取中断类型，兼容 kind 只存在 metadata 里的历史形态。
// 走 InterruptRequester 的 GetInterruptKind，子类与包装类型都能取到；
// 历史数据把 kind 只存在 metadata 里的情况由 InterruptKindOf 兜住。
func requestKindOf(request saschema.InterruptRequester) string {
	if request == nil {
		return saschema.InterruptKindUndeclared
	}
	ir, ok := request.(*saschema.InterruptRequest)
	if !ok {
		// 包装类型（ToolCallInterruptRequest 等）走接口投影
		if provider, ok := request.(interface{ GetInterruptKind() string }); ok {
			return provider.GetInterruptKind()
		}
		return saschema.InterruptKindUndeclared
	}
	return saschema.InterruptKindOf(ir)
}

// requestAsInterruptRequest 把中断请求转成 *InterruptRequest，非该类型返回 nil。
func requestAsInterruptRequest(request saschema.InterruptRequester) *saschema.InterruptRequest {
	if ir, ok := request.(*saschema.InterruptRequest); ok {
		return ir
	}
	return nil
}

// ──────────────────────────── owner 路由读取（InterruptOwnerMixin 对应） ────────────────────────────

// FindOwnAnswer 返回本次恢复中该 owner 在指定调用上的答案（无则 nil）。
// rail 只读自己 owner 那份：答案按 interrupt_owner 路由（与 kind 无关）。
// Python: core/single_agent/interrupt/ownership.py:53-55
// InterruptOwnerMixin.find_own_answer(extra, tool_call_id) —— 
// extra[RESUME_INTERRUPT_ANSWERS_KEY][tool_call_id][self.interrupt_owner]
func FindOwnAnswer(
	cbc *agentinterfaces.AgentCallbackContext,
	owner string,
	toolCallID string,
) *saschema.InterruptAnswer {
	raw, ok := cbc.GetExtra(saschema.ResumeInterruptAnswersKey)
	if !ok {
		return nil
	}
	callAnswers, ok := raw.(CallAnswers)
	if !ok {
		return nil
	}
	return callAnswers[toolCallID][owner]
}

// FindCallAnswer 从归属表原始值中按调用与 owner 查答案。
// CallAnswers 是本包具名类型，跨包断言同名匿名类型必失败，故经导出函数读取。
func FindCallAnswer(raw any, toolCallID, owner string) *saschema.InterruptAnswer {
	callAnswers, ok := raw.(CallAnswers)
	if !ok {
		return nil
	}
	return callAnswers[toolCallID][owner]
}

// IsCarriedAnswer 该 rail 在此调用上的答案是否来自更早恢复轮。
// 已被 acted on：再次接受但不重复外部副作用（如写盘持久化）。
// Python: ownership.py:57-64 is_carried_answer
func IsCarriedAnswer(cbc *agentinterfaces.AgentCallbackContext, owner, toolCallID string) bool {
	answer := FindOwnAnswer(cbc, owner, toolCallID)
	return answer != nil && answer.Carried
}
