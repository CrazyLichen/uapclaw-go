package tracer

// ──────────────────────────── 枚举 ────────────────────────────

// InvokeType 调用类型枚举，对应 Python InvokeType。
type InvokeType string

const (
	// InvokeTypePrompt 提示词调用
	InvokeTypePrompt InvokeType = "prompt"
	// InvokeTypeLLM LLM 调用
	InvokeTypeLLM InvokeType = "llm"
	// InvokeTypePlugin 插件调用
	InvokeTypePlugin InvokeType = "plugin"
	// InvokeTypeWorkflow 工作流调用
	InvokeTypeWorkflow InvokeType = "workflow"
	// InvokeTypeChain 链式调用
	InvokeTypeChain InvokeType = "chain"
	// InvokeTypeRetriever 检索调用
	InvokeTypeRetriever InvokeType = "retriever"
	// InvokeTypeEvaluator 评估调用
	InvokeTypeEvaluator InvokeType = "evaluator"
)

// NodeStatus 节点状态枚举，对应 Python NodeStatus。
type NodeStatus string

const (
	// NodeStatusStart 开始
	NodeStatusStart NodeStatus = "start"
	// NodeStatusFinish 完成
	NodeStatusFinish NodeStatus = "finish"
	// NodeStatusRunning 运行中
	NodeStatusRunning NodeStatus = "running"
	// NodeStatusInterrupted 已中断
	NodeStatusInterrupted NodeStatus = "interrupted"
	// NodeStatusError 错误
	NodeStatusError NodeStatus = "error"
)

// TracerHandlerName 追踪处理器名称枚举，对应 Python TracerHandlerName。
// 用于标识触发事件的处理器类型，替代硬编码字符串。
type TracerHandlerName string

const (
	// TracerHandlerAgent Agent 追踪处理器名称
	TracerHandlerAgent TracerHandlerName = "tracer_agent"
	// TracerHandlerWorkflow 工作流追踪处理器名称
	TracerHandlerWorkflow TracerHandlerName = "tracer_workflow"
)

// TraceEvent 追踪事件枚举，替代 Python 的字符串反射分发。
type TraceEvent string

const (
	// ─── Agent 事件（由装饰器触发） ───

	// TraceChainStart 链式调用开始
	TraceChainStart TraceEvent = "on_chain_start"
	// TraceChainEnd 链式调用结束
	TraceChainEnd TraceEvent = "on_chain_end"
	// TraceChainError 链式调用错误
	TraceChainError TraceEvent = "on_chain_error"
	// TraceLLMStart LLM 调用开始
	TraceLLMStart TraceEvent = "on_llm_start"
	// TraceLLMRequest LLM 请求详情
	TraceLLMRequest TraceEvent = "on_llm_request"
	// TraceLLMEnd LLM 调用结束
	TraceLLMEnd TraceEvent = "on_llm_end"
	// TraceLLMError LLM 调用错误
	TraceLLMError TraceEvent = "on_llm_error"
	// TracePromptStart 提示词调用开始
	TracePromptStart TraceEvent = "on_prompt_start"
	// TracePromptEnd 提示词调用结束
	TracePromptEnd TraceEvent = "on_prompt_end"
	// TracePromptError 提示词调用错误
	TracePromptError TraceEvent = "on_prompt_error"
	// TracePluginStart 插件调用开始
	TracePluginStart TraceEvent = "on_plugin_start"
	// TracePluginEnd 插件调用结束
	TracePluginEnd TraceEvent = "on_plugin_end"
	// TracePluginError 插件调用错误
	TracePluginError TraceEvent = "on_plugin_error"
	// TraceRetrieverStart 检索调用开始
	TraceRetrieverStart TraceEvent = "on_retriever_start"
	// TraceRetrieverEnd 检索调用结束
	TraceRetrieverEnd TraceEvent = "on_retriever_end"
	// TraceRetrieverError 检索调用错误
	TraceRetrieverError TraceEvent = "on_retriever_error"
	// TraceEvaluatorStart 评估调用开始
	TraceEvaluatorStart TraceEvent = "on_evaluator_start"
	// TraceEvaluatorEnd 评估调用结束
	TraceEvaluatorEnd TraceEvent = "on_evaluator_end"
	// TraceEvaluatorError 评估调用错误
	TraceEvaluatorError TraceEvent = "on_evaluator_error"
	// TraceWorkflowStart 工作流调用开始（Agent 层视角）
	TraceWorkflowStart TraceEvent = "on_workflow_start"
	// TraceWorkflowEnd 工作流调用结束（Agent 层视角）
	TraceWorkflowEnd TraceEvent = "on_workflow_end"
	// TraceWorkflowError 工作流调用错误（Agent 层视角）
	TraceWorkflowError TraceEvent = "on_workflow_error"

	// ─── Workflow 事件（由 TracerWorkflowUtils 触发） ───

	// TraceWFCallStart 组件调用开始
	TraceWFCallStart TraceEvent = "on_call_start"
	// TraceWFPreInvoke 组件预调用
	TraceWFPreInvoke TraceEvent = "on_pre_invoke"
	// TraceWFPreStream 组件预流式
	TraceWFPreStream TraceEvent = "on_pre_stream"
	// TraceWFInvoke 组件调用中（运行时数据/错误）
	TraceWFInvoke TraceEvent = "on_invoke"
	// TraceWFPostStream 组件后流式
	TraceWFPostStream TraceEvent = "on_post_stream"
	// TraceWFPostInvoke 组件后调用
	TraceWFPostInvoke TraceEvent = "on_post_invoke"
	// TraceWFCallDone 组件调用完成
	TraceWFCallDone TraceEvent = "on_call_done"
	// TraceWFInteract 组件交互
	TraceWFInteract TraceEvent = "on_interact"
)
