package schema

import "strings"

// 本文件放两类无状态纯函数：工具策略提示、中断续答载荷判定。
//
// 为什么在 schema：它们的实现在 runtime 包（tool_allowlist.go / interrupt_*.go），
// 但 server/adapter（上层）也要调用；若留在 runtime，server/adapter → runtime
// 会让 runtime 无法反向引用 server 侧类型（P-7 的环）。
// 两个函数都只做「读 map + 拼字符串」，无状态、无 runtime 依赖，属共享契约层。
//
// 上游位置：jiuwenswarm/runtime/tool_allowlist.py、interrupt_helpers.py。
// Go 下沉到 schema 属包组织差异（Python 的 agent_adapter 是 server/runtime 的
// 子目录，Go 是兄弟包），已在 spec 登记。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ClientToolInterruptSource 客户端工具中断来源
	// Python: interrupt_resume.py CLIENT_TOOL_INTERRUPT_SOURCE
	ClientToolInterruptSource = "client_tool_interrupt"
	// LegacySkillEvolutionApprovalSource 旧版技能演进审批来源
	LegacySkillEvolutionApprovalSource = "skill_evolution_approval"
	// disabledToolsPrompt 白名单为空时追加给模型的说明。
	// Python: tool_allowlist.py:22-26
	disabledToolsPrompt = "\n\nAll tools are disabled for this Agent. Do not call or claim access " +
		"to tools, files, shell commands, network resources, or delegates. " +
		"Answer using only the conversation and your knowledge."
)

// ──────────────────────────── 全局变量 ────────────────────────────

// InterruptResumeSources 回答被暂停工具调用的 chat.send 来源集合。
// Python: interrupt_resume.py INTERRUPT_RESUME_SOURCES
var InterruptResumeSources = map[string]struct{}{
	"permission_interrupt":    {},
	"confirm_interrupt":       {},
	"ask_user_interrupt":      {},
	"evolution_interrupt":     {},
	ClientToolInterruptSource: {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ApplyToolPolicyPrompt 向模型同时描述并强制"显式空能力集"。
// Python: tool_allowlist.py:16-26 apply_tool_policy_prompt
//
// 语义与直觉相反：**名单为空**才追加"工具已全部禁用"的说明；
// 名单非空时原样返回（收窄由 install_tool_allowlist 在边界执行）。
func ApplyToolPolicyPrompt(systemPrompt string, toolNames []string) string {
	if len(toolNames) > 0 {
		return systemPrompt
	}
	return systemPrompt + disabledToolsPrompt
}

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

// AnswersPausedCalls 载荷是否在回答被暂停的工具调用。
// Python: interrupt_helpers.py answers_paused_calls
//
// 判据（顺序与上游一致）：
//  1. source 去空白后必须在 resume 集合内
//  2. answers 必须是非空列表
//  3. 除 client tool resume 外，request_id 必须非空
//     （客户端工具结果不指名问题，它的每个答案指名自己跑过的调用）
func AnswersPausedCalls(params map[string]any) bool {
	if params == nil {
		return false
	}
	source, _ := params["source"].(string)
	source = strings.TrimSpace(source)
	if _, ok := InterruptResumeSources[source]; !ok {
		return false
	}
	answers, ok := params["answers"].([]any)
	if !ok || len(answers) == 0 {
		return false
	}
	if source == ClientToolInterruptSource {
		return true
	}
	requestID, _ := params["request_id"].(string)
	return strings.TrimSpace(requestID) != ""
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
