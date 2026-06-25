package schema

import (
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseInterruptionState 中断状态基类。
//
// 对应 Python: BaseInterruptionState(ai_message, iteration, original_query)
type BaseInterruptionState struct {
	// AIMessage 中断时的 AI 消息
	AIMessage *llmschema.AssistantMessage
	// Iteration 中断时的迭代次数
	Iteration int
	// OriginalQuery 原始查询
	OriginalQuery string
}

// ToolInterruptEntry 工具中断条目。
//
// 对应 Python: ToolInterruptEntry(tool_call, interrupt_requests, is_sub_agent)
// Python 中 interrupt_requests 类型为 Dict[str, InterruptRequest]，但实际可存 InterruptRequest 子类
// （如 ToolCallInterruptRequest）。Go 中用 InterruptRequester 接口实现多态。
type ToolInterruptEntry struct {
	// ToolCall 触发中断的工具调用
	ToolCall *llmschema.ToolCall
	// InterruptRequests 中断请求映射 (interrupt_id → InterruptRequester)
	// 对齐 Python: Dict[str, InterruptRequest] — 实际可存 InterruptRequest 或 ToolCallInterruptRequest
	InterruptRequests map[string]InterruptRequester
	// IsSubAgent 是否来自子 Agent
	IsSubAgent bool
}

// ToolInterruptionState 工具中断状态（HITL 中断）。
//
// 对应 Python: ToolInterruptionState(BaseInterruptionState)
type ToolInterruptionState struct {
	// BaseInterruptionState 嵌入基类
	BaseInterruptionState
	// InterruptedTools 被中断的工具映射 (tool_call_id → ToolInterruptEntry)
	InterruptedTools map[string]*ToolInterruptEntry
	// AutoConfirmMapping 自动确认映射 (inner_id → auto_confirm_key)
	AutoConfirmMapping map[string]string
}

// WorkflowInterruptEntry 工作流中断条目。
//
// 对应 Python: react_agent.py L406 WorkflowInterruptEntry(tool_call, component_ids, workflow_execution_state, collected_input)
type WorkflowInterruptEntry struct {
	// ToolCall 触发中断的工具调用
	ToolCall *llmschema.ToolCall
	// ComponentIDs 工作流组件 ID 列表
	ComponentIDs []string
	// WorkflowExecutionState 工作流执行状态（WorkflowOutput）
	WorkflowExecutionState any
	// CollectedInput 已收集的用户输入（nil = 未收集）
	CollectedInput any
}

// InterruptionState 工作流中断状态。
//
// 对应 Python: react_agent.py L414 InterruptionState(BaseInterruptionState)
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

// ──────────────────────────── 枚举 ────────────────────────────

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
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
