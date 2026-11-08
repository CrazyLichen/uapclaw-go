package runtime

import "strings"

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ClientToolInterruptSource 客户端自己执行了工具、回来交结果的 chat.send 来源。
	// Python: gateway/common/interrupt_resume.py CLIENT_TOOL_INTERRUPT_SOURCE
	ClientToolInterruptSource = "client_tool_interrupt"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// InterruptResumeSources 回答被暂停工具调用的 chat.send 来源集合。
	// Python: interrupt_resume.py INTERRUPT_RESUME_SOURCES（5 项 frozenset）
	//
	// 这类发送是"接着被打断的轮次跑"，而不是新起一轮：Gateway 保留运行中的
	// 流，Runtime 把它路由到暂停的执行，adapter 把 answers 转成 InteractiveInput。
	InterruptResumeSources = map[string]struct{}{
		"permission_interrupt":    {},
		"confirm_interrupt":       {},
		"ask_user_interrupt":      {},
		"evolution_interrupt":     {},
		ClientToolInterruptSource: {},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsClientToolResume chat.send 是否携带客户端执行工具的结果。
// Python: interrupt_resume.py is_client_tool_resume
func IsClientToolResume(params map[string]any) bool {
	if params == nil {
		return false
	}
	source, _ := params["source"].(string)
	return source == ClientToolInterruptSource
}

// AnswersPausedCalls chat.send 是否在回答一个被暂停的轮次。
// Python: interrupt_resume.py answers_paused_calls
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
