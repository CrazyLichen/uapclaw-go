package runtime

import "strings"

// ──────────────────────────── 常量 ────────────────────────────

const (
	// LegacySkillEvolutionApprovalSource 老式技能演进审批的 chat.send source 取值。
	// Python: agents/harness/common/rails/interrupt/interrupt_helpers.py
	//         LEGACY_SKILL_EVOLUTION_APPROVAL_SOURCE（:57）
	LegacySkillEvolutionApprovalSource = "skill_evolution_approval"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// HasInterruptResumePayload 载荷是否带 request_id 与非空 answers。
// Python: interrupt_helpers.py has_interrupt_resume_payload（:151）
func HasInterruptResumePayload(params map[string]any) bool {
	if params == nil {
		return false
	}
	requestID, _ := params["request_id"].(string)
	if strings.TrimSpace(requestID) == "" {
		return false
	}
	answers, ok := params["answers"].([]any)
	return ok && len(answers) > 0
}

// IsInterruptResumePayload 载荷是否在回答一个被暂停的轮次。
// Python: interrupt_helpers.py is_interrupt_resume_payload（:160）
//
// 三条路径，顺序不得调整（短路顺序会影响副作用边界）：
//  1. answers_paused_calls 命中即为 true
//  2. 不是标准 resume 载荷则 false
//  3. source 必须是 legacy 技能演进审批，且 evolution_meta.approval_transport == "interrupt"
func IsInterruptResumePayload(params map[string]any) bool {
	if AnswersPausedCalls(params) {
		return true
	}
	if !HasInterruptResumePayload(params) {
		return false
	}
	source, _ := params["source"].(string)
	if strings.TrimSpace(source) != LegacySkillEvolutionApprovalSource {
		return false
	}
	evolutionMeta, ok := params["evolution_meta"].(map[string]any)
	if !ok {
		return false
	}
	transport, _ := evolutionMeta["approval_transport"].(string)
	return transport == "interrupt"
}

// IsInterruptResumeRequest 请求是否为中断续答。
// Python: runtime/service.py _is_interrupt_resume_request（:3601）
//
// 保持上游的一行转调形状：ask_user / permission 的答案以 chat.send 运输，
// 原轮次仍持有会话准入；若让答案再等一次准入，交互会死锁、Heartbeat 会无限阻塞。
func IsInterruptResumeRequest(params map[string]any) bool {
	return IsInterruptResumePayload(params)
}
