package exception

// ──────────────────────────── 全局变量 ────────────────────────────

// =============================================================================================================
// Component Session 111000–111009
// =============================================================================================================

var (
	// StatusCompSessionInteractError 组件交互不支持
	StatusCompSessionInteractError = NewStatusCode(
		"COMP_SESSION_INTERACT_ERROR", 111005,
		"interact is not support, error='{reason}', comp_id={comp_id}, workflow={workflow}")
)

// =============================================================================================================
// Interaction 111110–111119
// =============================================================================================================

var (
	// StatusInteractionInputInvalid 交互输入无效
	StatusInteractionInputInvalid = NewStatusCode(
		"INTERACTION_INPUT_INVALID", 111110,
		"interaction input is invalid, reason={reason}")
)

// =============================================================================================================
// Checkpointer 111120–111129
// =============================================================================================================

var (
	// StatusCheckpointerPostWorkflowExecutionError 检查点后置工作流执行错误
	StatusCheckpointerPostWorkflowExecutionError = NewStatusCode(
		"CHECKPOINTER_POST_WORKFLOW_EXECUTION_ERROR", 111120,
		"post workflow execute error, session_id={session_id}, workflow={workflow}, error='{reason}'")
	// StatusCheckpointerPreWorkflowExecutionError 检查点前置工作流执行错误
	StatusCheckpointerPreWorkflowExecutionError = NewStatusCode(
		"CHECKPOINTER_PRE_WORKFLOW_EXECUTION_ERROR", 111121,
		"pre workflow execute error, session_id={session_id}, workflow={workflow}, error='{reason}'")
	// StatusCheckpointerInterruptAgentError 检查点中断 agent 执行错误
	StatusCheckpointerInterruptAgentError = NewStatusCode(
		"CHECKPOINTER_INTERRUPT_AGENT_ERROR", 111122,
		"interrupt agent execute error, session_id={session_id}, agent={agent}, error='{reason}'")
	// StatusCheckpointerPostAgentExecutionError 检查点后置 agent 执行错误
	StatusCheckpointerPostAgentExecutionError = NewStatusCode(
		"CHECKPOINTER_POST_AGENT_EXECUTION_ERROR", 111123,
		"post agent execute error, session_id={session_id}, agent={agent}, error='{reason}'")
	// StatusCheckpointerConfigError 检查点配置错误
	StatusCheckpointerConfigError = NewStatusCode(
		"CHECKPOINTER_CONFIG_ERROR", 111124,
		"checkpointer config error, session_id={session_id}, error='{reason}'")
)

// =============================================================================================================
// Stream Writer 111130–111139
// =============================================================================================================

var (
	// StatusStreamWriterManagerAddWriterError 添加流写入器错误
	StatusStreamWriterManagerAddWriterError = NewStatusCode(
		"STREAM_WRITER_MANAGER_ADD_WRITER_ERROR", 111130,
		"add new stream writer error, mode={mode}, error='{reason}'")
	// StatusStreamWriterManagerRemoveWriterError 移除流写入器错误
	StatusStreamWriterManagerRemoveWriterError = NewStatusCode(
		"STREAM_WRITER_MANAGER_REMOVE_WRITER_ERROR", 111131,
		"remove stream writer error, mode={mode}, error='{reason}'")
	// StatusStreamWriterWriteStreamValidationError 流写入数据校验错误
	StatusStreamWriterWriteStreamValidationError = NewStatusCode(
		"STREAM_WRITER_WRITE_STREAM_VALIDATION_ERROR", 111132,
		"writer stream data validate error, stream_type={schema_type}, stream_data={stream_data}, error='{reason}'")
	// StatusStreamWriterWriteStreamError 流写入数据错误
	StatusStreamWriterWriteStreamError = NewStatusCode(
		"STREAM_WRITER_WRITE_STREAM_ERROR", 111133,
		"writer stream data error, stream_data={stream_data}, error='{reason}'")
	// StatusStreamOutputFirstChunkIntervalTimeout 流输出首块超时
	StatusStreamOutputFirstChunkIntervalTimeout = NewStatusCode(
		"STREAM_OUTPUT_FIRST_CHUNK_INTERVAL_TIMEOUT", 111134,
		"stream output first stream chunk timeout, timeout={timeout}s, error='{reason}'")
	// StatusStreamOutputChunkIntervalTimeout 流输出后续块超时
	StatusStreamOutputChunkIntervalTimeout = NewStatusCode(
		"STREAM_OUTPUT_CHUNK_INTERVAL_TIMEOUT", 111135,
		"stream output next stream chunk timeout, interval_timeout={timeout}s, error='{reason}'")
)

// =============================================================================================================
// Tracer 111140–111149
// =============================================================================================================

var (
	// StatusTracerWorkflowTraceError 工作流追踪错误
	StatusTracerWorkflowTraceError = NewStatusCode(
		"TRACER_WORKFLOW_TRACE_ERROR", 111140,
		"trace workflow error, error='{reason}'")
	// StatusTracerAgentTraceError Agent 追踪错误
	StatusTracerAgentTraceError = NewStatusCode(
		"TRACER_AGENT_TRACE_ERROR", 111141,
		"trace agent error, error='{reason}'")
)
