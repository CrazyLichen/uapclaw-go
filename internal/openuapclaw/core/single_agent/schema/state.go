package schema

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowbase"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseInterruptionState 中断状态基类。
// Python: BaseInterruptionState(ai_message, iteration, original_query)
type BaseInterruptionState struct {
	// AIMessage 中断时的 AI 消息
	AIMessage *llmschema.AssistantMessage
	// Iteration 中断时的迭代次数
	Iteration int
	// OriginalQuery 原始查询
	OriginalQuery string
}

// ToolInterruptEntry 工具中断条目。
// Python: ToolInterruptEntry(tool_call, interrupt_requests, is_sub_agent)
// Python 中 interrupt_requests 类型为 Dict[str, InterruptRequest]，但实际可存 InterruptRequest 子类
// （如 ToolCallInterruptRequest）。Go 中用 InterruptRequester 接口实现多态。
type ToolInterruptEntry struct {
	// ToolCall 触发中断的工具调用
	ToolCall *llmschema.ToolCall
	// InterruptRequests 中断请求映射 (interrupt_id → InterruptRequester)
	// Python: Dict[str, InterruptRequest] — 实际可存 InterruptRequest 或 ToolCallInterruptRequest
	InterruptRequests map[string]InterruptRequester
	// IsSubAgent 是否来自子 Agent
	IsSubAgent bool
	// CarriedAnswers 更早恢复轮次已给出的答案，按 call_id → owner 索引。
	// 一个调用若在本轮又暂停，它上一轮拿到的答案要保留到下一轮（标记 carried），
	// 让 rail 知道"这个答案已经执行过副作用，不要重复执行"。若本轮该 rail
	// 在同一调用上又重新提问，说明上轮的答案没被接受，那条答案会被丢弃。
	// Python: ToolInterruptEntry.carried_answers
	CarriedAnswers map[string]map[string]*InterruptAnswer
	// PausedCall rail 或客户端工具中断所针对的（inner）调用；为客户端
	// 保持暂停的调用再次暂停时仍带着它。子 Agent 调用为 nil。
	// Python: ToolInterruptEntry.paused_call（state.py:71-73）
	PausedCall *llmschema.ToolCall
}

// ToolInterruptionState 工具中断状态（HITL 中断）。
// Python: ToolInterruptionState(BaseInterruptionState)
type ToolInterruptionState struct {
	// BaseInterruptionState 嵌入基类
	BaseInterruptionState
	// InterruptedTools 被中断的工具映射 (tool_call_id → ToolInterruptEntry)
	InterruptedTools map[string]*ToolInterruptEntry
	// DeferredToolImages 本轮已完成调用的图片项；等本轮没有任何调用暂停时
	// 才送进模型（tool message 必须聚在一个 turn 里）。
	// Python: ToolInterruptionState.deferred_tool_images（state.py:88-90）
	DeferredToolImages []map[string]any
}

// WorkflowInterruptEntry 工作流中断条目。
// Python: react_agent.py L406 WorkflowInterruptEntry(tool_call, component_ids, workflow_execution_state, collected_input)
type WorkflowInterruptEntry struct {
	// ToolCall 触发中断的工具调用
	ToolCall *llmschema.ToolCall
	// ComponentIDs 工作流组件 ID 列表
	ComponentIDs []string
	// WorkflowExecutionState 工作流执行状态（8.26 收窄回填：原 any 破环承载，
	// 叶包 workflowbase 抽取后直接强类型；运行时恒 *WorkflowOutput）
	WorkflowExecutionState *workflowbase.WorkflowOutput
	// CollectedInput 已收集的用户输入（nil = 未收集；8.26 收窄回填：强类型
	// InteractiveInput，schema→session/interaction 无环已核）
	CollectedInput *interaction.InteractiveInput
}

// InterruptionState 工作流中断状态。
// Python: react_agent.py L414 InterruptionState(BaseInterruptionState)
type InterruptionState struct {
	// BaseInterruptionState 嵌入基类
	BaseInterruptionState
	// InterruptedWorkflows 被中断的工作流映射 (workflow_id → WorkflowInterruptEntry)
	InterruptedWorkflows map[string]*WorkflowInterruptEntry
	// PendingWorkflowID 待处理的工作流 ID
	PendingWorkflowID string
	// PendingComponentID 待处理的组件 ID
	PendingComponentID string
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// InterruptionKey ReActAgent 中断状态键
	InterruptionKey = "__react_agent_interruption__"
	// ResumeUserInputKey 恢复时用户输入的键
	ResumeUserInputKey = "_resume_user_input"
	// InterruptAutoConfirmKey 自动确认配置键
	InterruptAutoConfirmKey = "__interrupt_auto_confirm__"
	// ResumeStartIterationKey 恢复时起始迭代键
	ResumeStartIterationKey = "_resume_start_iteration"
	// ResumeInterruptAnswersKey 恢复期间按调用归属的答案键。
	// 结构：{tool_call_id: {interrupt_owner: *InterruptAnswer}}。rail 只读取
	// 自己 owner 那份答案，因此不同 rail 在同一调用上的中断不会串台。
	// Python: RESUME_INTERRUPT_ANSWERS_KEY
	ResumeInterruptAnswersKey = "_resume_interrupt_answers"
	// WorkflowResumeInputsKey 工作流中断恢复时按调用归属的交互输入对象键（D-26-5）。
	// 结构：{tool_call_id: any（*interaction.InteractiveInput）}。
	// Python 经 tool_call.arguments 字段走私对象（pydantic 不校验赋值 +
	// _parse_tool_arguments_with_repair isinstance(str) 短路透传）；
	// Go 的 string Arguments 无法承载对象，改由 cbc extra 对象旁路承载，
	// 起点终点与 finally 清理语义同 Python（仅载体异，spec D-26-5 重写版）。
	WorkflowResumeInputsKey = "_workflow_resume_inputs"
	// DeferredToolImagesKey 等待同批全部调用补齐 tool message 的图片项
	// Python: DEFERRED_TOOL_IMAGES_KEY
	DeferredToolImagesKey = "_deferred_tool_images"
	// ToolCallsStillPausedKey 本轮仍有调用保持暂停、未被重放的标记
	// Python: TOOL_CALLS_STILL_PAUSED_KEY
	ToolCallsStillPausedKey = "_tool_calls_still_paused"
)

// ──────────────────────────── 结构体（恢复答案） ────────────────────────────

// InterruptAnswer 一条答案，由中断处理器归属到它所回答的中断。
// 字段含义（对齐 Python InterruptAnswer）：
//   - Owner：被回答中断的 interrupt_owner；只有那个 rail 会收到这条答案。
//   - Kind：被回答中断的 interrupt_kind。
//   - Value：宿主发来的答案原值。
//   - Request：被回答的请求（如带客户端运行参数的 ClientToolRequest）。
//   - Carried：该答案在同一个仍暂停的调用的更早一轮恢复中已给出并被处理。
//   - Unaddressed：该恢复输入是纯文本，未指定任何中断（提供给该调用上挂起的中断）。
//
// Python: class InterruptAnswer(BaseModel)
type InterruptAnswer struct {
	// Owner 被回答中断的 owner
	Owner string
	// Kind 被回答中断的类型
	Kind string
	// Value 宿主发来的答案
	Value any
	// Request 被回答的请求。Python 存 InterruptRequest 子类实例
	// （如 AskUserRequest、ClientToolRequest）；Go 用 InterruptRequester
	// 接口保留子类完整字段（saveAutoConfirm 等经 GetAutoConfirmKey 读取）。
	Request InterruptRequester
	// Carried 是否为更早恢复轮次已处理的答案
	Carried bool
	// Unaddressed 是否为未指定中断的纯文本输入
	Unaddressed bool
}
