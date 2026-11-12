package interrupt

import (
	"strings"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// 本文件对应 Python openjiuwen/core/single_agent/interrupt/answers.py（129 行）：
// 按中断类型构造宿主应答的形状。
//
// 每种中断期待自己的答案形状（answers.py:5-20 的表格）：
//
//	permission / confirm / team_tool_approval / 未声明
//	    {"approved": bool, "feedback": str, "auto_confirm": bool}
//	    （auto_confirm + "persist_allow": false = 本次会话记住）
//	skill_evolution_approval
//	    {"action": "allow_once" | "allow_always" | "reject", "feedback": str}
//	ask_user
//	    {"answers": {question: answer}} 或答案文本
//	client_tool
//	    ClientToolResult 或其 dict 形式，或结果文本
//
// 未声明类型可按 approved 形状作答——在 kinds 出现之前所有中断都是这样，
// 本文件的助手把它当审批处理。
//
// 文本词表（answers.py:31-41 的表格，大小写不敏感）：
//
//	approve once  y / yes / ok / approve / true / 1 / allow / allow_once
//	always        a / always / allow_always（本次会话批准的同意）
//	reject        n / no / deny / reject
//
// 其余文本（含空文本）一律拒绝，并把它当作反馈。

// ──────────────────────────── 结构体 ────────────────────────────

// InterruptAnswerOptions InterruptAnswerFor 的可选参数。
// Python: answers.py:83-90 interrupt_answer 的 keyword-only 参数
// （approved / remember / feedback / text）。
//
// Go 无默认参数与关键字参数，改用可省略的结构体。
// Approved 与 Text 用指针以区分「未提供」与「提供零值」——上游用 None 区分。
type InterruptAnswerOptions struct {
	// Approved 批准（true）或拒绝（false）审批类中断；nil 表示未提供
	Approved *bool
	// Remember 同时批准后续匹配的调用（会话级）
	Remember bool
	// Feedback 拒绝时给出的理由
	Feedback string
	// Text 人打的字；nil 表示未提供
	Text *string
}

// ──────────────────────────── 全局变量 ────────────────────────────

// ApprovalInterruptKinds 按审批形状作答的中断类型（含未声明类型）。
// Python: answers.py:57 APPROVAL_INTERRUPT_KINDS
// = APPROVED_ANSWER_KINDS | {SKILL_EVOLUTION_APPROVAL}
//
// 注意与 answer_attribution.go 的私有 approvedAnswerKinds 区分：
// 后者只有 4 项（不含 skill_evolution_approval），语义是「答案里有
// auto_confirm 字段」，与本集合的「是不是审批类」不同。两者本就该并存，
// 合并会把 skill_evolution_approval 错误纳入 auto_confirm 白名单。
var ApprovalInterruptKinds = map[string]bool{
	saschema.InterruptKindPermission:             true,
	saschema.InterruptKindConfirm:                true,
	saschema.InterruptKindTeamToolApproval:       true,
	saschema.InterruptKindUndeclared:             true,
	saschema.InterruptKindSkillEvolutionApproval: true,
}

// ApprovalChoices 纯文本宿主可提供的审批选项。
// Python: answers.py:62 APPROVAL_CHOICES
var ApprovalChoices = []string{"allow", "allow_always", "deny"}

// allowWords 一次批准的用词。
// Python: answers.py:59 _ALLOW_WORDS
// 逐字复制，不得增删——文案是前端与宿主共用的协议值。
var allowWords = map[string]bool{
	"y": true, "yes": true, "ok": true, "approve": true,
	"true": true, "1": true, "allow": true, "allow_once": true,
}

// alwaysWords 本次会话批准的用词。
// Python: answers.py:60 _ALWAYS_WORDS
var alwaysWords = map[string]bool{"a": true, "always": true, "allow_always": true}

// denyWords 拒绝的用词。
// Python: answers.py:61 _DENY_WORDS
var denyWords = map[string]bool{"n": true, "no": true, "deny": true, "reject": true}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsApprovalKind kind 是否按审批作答（未声明类型也算）。
// Python: answers.py:65-67 is_approval_kind
func IsApprovalKind(kind string) bool {
	return ApprovalInterruptKinds[kind]
}

// ParseApprovalText 从人打的字里读出（批准、记住、反馈）三元组。
// Python: answers.py:70-80 _parse_approval_text
//
// 三步（顺序不可调）：
//  1. 去首尾空白后转小写匹配（Python: word = text.strip(); lowered = word.lower()）
//  2. always / allow / deny 三组词表逐个判定
//  3. 匹配失败时把去空白后的**原文**当反馈返回（不是小写形式）
func ParseApprovalText(text string) (approved bool, remember bool, feedback string) {
	word := strings.TrimSpace(text)
	lowered := strings.ToLower(word)
	if alwaysWords[lowered] {
		return true, true, ""
	}
	if allowWords[lowered] {
		return true, false, ""
	}
	if denyWords[lowered] {
		return false, false, ""
	}
	return false, false, word
}

// InterruptAnswerFor 构造 kind 类型中断的答案。
// Python: answers.py:83-128 interrupt_answer
//
// 六个步骤（顺序不可调，对齐上游）：
//  1. approved 给了但 kind 不是审批类 → 报错（answers.py:108-109）
//  2. approved 与 text 都没给 → 报错（answers.py:110-111）
//  3. kind 不是审批类 → 直接返回 text（answers.py:112-113）
//  4. approved 没给 → 用 text 解析出 approved/remember/feedback（answers.py:114-116）
//  5. kind 是 skill_evolution_approval → {"action": ...} 形状（answers.py:117-120）
//  6. 其余审批类 → {"approved", "feedback", "auto_confirm"} 形状（answers.py:121-128）
//
// 第 4 步的反馈优先级：Python `feedback = feedback or parsed_feedback`，
// 即显式 feedback 非空时用它，否则用解析出的。
func InterruptAnswerFor(kind string, opts InterruptAnswerOptions) (any, error) {
	approved, remember, feedback := opts.Approved, opts.Remember, opts.Feedback

	// 步骤 1：非审批类不得用 approved 作答
	if approved != nil && !IsApprovalKind(kind) {
		return nil, exception.RaiseError(exception.StatusInteractionInputInvalid,
			exception.WithParam("reason",
				"interrupts of kind "+kind+" are not approvals; answer them with text"))
	}
	// 步骤 2：必须给 approved 或 text
	if approved == nil && opts.Text == nil {
		return nil, exception.RaiseError(exception.StatusInteractionInputInvalid,
			exception.WithParam("reason", "pass approved or text"))
	}
	// 步骤 3：非审批类直接返回文本
	if !IsApprovalKind(kind) {
		return *opts.Text, nil
	}
	// 步骤 4：从文本解析（approved 未提供时）
	if approved == nil {
		parsedApproved, parsedRemember, parsedFeedback := ParseApprovalText(*opts.Text)
		approved = &parsedApproved
		remember = parsedRemember
		if feedback == "" {
			feedback = parsedFeedback
		}
	}
	// 步骤 5：skill_evolution_approval 的 {"action": ...}
	if kind == saschema.InterruptKindSkillEvolutionApproval {
		if *approved {
			action := "allow_once"
			if remember {
				action = "allow_always"
			}
			return map[string]any{"action": action}, nil
		}
		return map[string]any{"action": "reject", "feedback": feedback}, nil
	}
	// 步骤 6：其余审批类的 {"approved", "feedback", "auto_confirm"}
	answer := map[string]any{
		"approved":     *approved,
		"feedback":     feedbackIfApproved(*approved, feedback),
		"auto_confirm": *approved && remember,
	}
	if *approved && remember {
		answer["persist_allow"] = false
	}
	return answer, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// feedbackIfApproved 批准时反馈恒为空串。
// Python: answers.py:123 `"feedback": "" if approved else feedback`
func feedbackIfApproved(approved bool, feedback string) string {
	if approved {
		return ""
	}
	return feedback
}
