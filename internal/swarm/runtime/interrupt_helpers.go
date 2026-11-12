package runtime

import "github.com/uapclaw/uapclaw-go/internal/swarm/schema"

// ──────────────────────────── 常量 ────────────────────────────

// LegacySkillEvolutionApprovalSource 老式技能演进审批的 chat.send source 取值。
// Python: agents/harness/common/rails/interrupt/interrupt_helpers.py
//
//	LEGACY_SKILL_EVOLUTION_APPROVAL_SOURCE（:57）
const LegacySkillEvolutionApprovalSource = schema.LegacySkillEvolutionApprovalSource

// ──────────────────────────── 导出函数 ────────────────────────────

// HasInterruptResumePayload 载荷是否带 request_id 与非空 answers。
// Python: interrupt_helpers.py has_interrupt_resume_payload（:151）
var HasInterruptResumePayload = schema.HasInterruptResumePayload

// IsInterruptResumePayload 载荷是否在回答一个被暂停的轮次。
// Python: interrupt_helpers.py is_interrupt_resume_payload（:160）
var IsInterruptResumePayload = schema.IsInterruptResumePayload

// IsInterruptResumeRequest 请求是否为中断续答。
// Python: runtime/service.py _is_interrupt_resume_request（:3601）
//
// 保持上游的一行转调形状：ask_user / permission 的答案以 chat.send 运输，
// 原轮次仍持有会话准入；若让答案再等一次准入，交互会死锁、Heartbeat 会无限阻塞。
func IsInterruptResumeRequest(params map[string]any) bool {
	return IsInterruptResumePayload(params)
}
