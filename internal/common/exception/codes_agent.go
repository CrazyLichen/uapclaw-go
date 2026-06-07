package exception

// ──────────────────────────── 全局变量 ────────────────────────────

// =============================================================================================================
// Agent Orchestration — ReAct Agent 120000–120999
// =============================================================================================================

var (
	// StatusAgentToolNotFound Agent 工具未找到
	StatusAgentToolNotFound = NewStatusCode(
		"AGENT_TOOL_NOT_FOUND", 120000,
		"agent tool not found, reason: {error_msg}")
	// StatusAgentToolExecutionError Agent 工具执行错误
	StatusAgentToolExecutionError = NewStatusCode(
		"AGENT_TOOL_EXECUTION_ERROR", 120001,
		"agent tool execution error, reason: {error_msg}")
	// StatusAgentTaskNotSupport Agent 任务类型不支持
	StatusAgentTaskNotSupport = NewStatusCode(
		"AGENT_TASK_NOT_SUPPORT", 120002,
		"agent task is not supported, reason: {error_msg}")
	// StatusAgentWorkflowExecutionError Agent 工作流执行错误
	StatusAgentWorkflowExecutionError = NewStatusCode(
		"AGENT_WORKFLOW_EXECUTION_ERROR", 120003,
		"agent workflow execution error, reason: {error_msg}")
	// StatusAgentPromptParamError Agent 提示词参数错误
	StatusAgentPromptParamError = NewStatusCode(
		"AGENT_PROMPT_PARAM_ERROR", 120004,
		"agent prompt parameter error, reason: {error_msg}")
	// StatusAbilityExecutionError 能力执行错误（AbilityManager 统一异常）
	StatusAbilityExecutionError = NewStatusCode(
		"ABILITY_EXECUTION_ERROR", 120005,
		"ability execution error, reason: {error_msg}")
	// StatusAbilityNotFound 能力未找到
	StatusAbilityNotFound = NewStatusCode(
		"ABILITY_NOT_FOUND", 120006,
		"ability not found, name: {ability_name}")
	// StatusAbilityMalformedArguments 工具参数 JSON 格式错误
	StatusAbilityMalformedArguments = NewStatusCode(
		"ABILITY_MALFORMED_ARGUMENTS", 120007,
		"malformed tool arguments, tool: {tool_name}, reason: {error_msg}")
)

// =============================================================================================================
// Agent Controller 123000–123999
// =============================================================================================================

var (
	// StatusAgentControllerInvokeCallFailed Agent controller invoke 调用失败
	StatusAgentControllerInvokeCallFailed = NewStatusCode(
		"AGENT_CONTROLLER_INVOKE_CALL_FAILED", 123000,
		"agent controller_invoke call failed, reason: {error_msg}")
	// StatusAgentSubTaskTypeNotSupport Agent 子任务类型不支持
	StatusAgentSubTaskTypeNotSupport = NewStatusCode(
		"AGENT_SUB_TASK_TYPE_NOT_SUPPORT", 123001,
		"agent sub_task_type is not supported, reason: {error_msg}")
	// StatusAgentControllerUserInputProcessError Agent controller 用户输入处理错误
	StatusAgentControllerUserInputProcessError = NewStatusCode(
		"AGENT_CONTROLLER_USER_INPUT_PROCESS_ERROR", 123002,
		"agent controller_user_input process error, reason: {error_msg}")
	// StatusAgentControllerRuntimeError Agent controller 运行时错误
	StatusAgentControllerRuntimeError = NewStatusCode(
		"AGENT_CONTROLLER_RUNTIME_ERROR", 123003,
		"agent controller runtime error, reason: {error_msg}")
	// StatusAgentControllerExecutionCallFailed Agent controller execution 调用失败
	StatusAgentControllerExecutionCallFailed = NewStatusCode(
		"AGENT_CONTROLLER_EXECUTION_CALL_FAILED", 123004,
		"agent controller_execution call failed, reason: {error_msg}")
	// StatusAgentControllerToolExecutionProcessError Agent controller 工具执行处理错误
	StatusAgentControllerToolExecutionProcessError = NewStatusCode(
		"AGENT_CONTROLLER_TOOL_EXECUTION_PROCESS_ERROR", 123005,
		"agent controller_tool_execution process error, reason: {error_msg}")
	// StatusAgentControllerTaskParamError controller 任务参数错误
	StatusAgentControllerTaskParamError = NewStatusCode(
		"AGENT_CONTROLLER_TASK_PARAM_ERROR", 123006,
		"controller task parameter error, reason: {error_msg}")
	// StatusAgentControllerIntentParamError controller 意图参数错误
	StatusAgentControllerIntentParamError = NewStatusCode(
		"AGENT_CONTROLLER_INTENT_PARAM_ERROR", 123007,
		"controller intention parameter error, reason: {error_msg}")
	// StatusAgentControllerTaskExecutionError controller 任务执行错误
	StatusAgentControllerTaskExecutionError = NewStatusCode(
		"AGENT_CONTROLLER_TASK_EXECUTION_ERROR", 123008,
		"controller task execution error, reason: {error_msg}")
	// StatusAgentControllerEventHandlerError controller 事件处理器错误
	StatusAgentControllerEventHandlerError = NewStatusCode(
		"AGENT_CONTROLLER_EVENT_HANDLER_ERROR", 123009,
		"controller event handler error, reason: {error_msg}")
	// StatusAgentControllerEventQueueError Agent controller 事件队列执行错误
	StatusAgentControllerEventQueueError = NewStatusCode(
		"AGENT_CONTROLLER_EVENT_QUEUE_ERROR", 123010,
		"agent controller event queue execution error, reason: {error_msg}")
)

// =============================================================================================================
// DeepAgent 123020–123039
// =============================================================================================================

var (
	// StatusDeepagentConfigParamError deepagent 配置参数错误
	StatusDeepagentConfigParamError = NewStatusCode(
		"DEEPAGENT_CONFIG_PARAM_ERROR", 123020,
		"deepagent config parameter error, reason: {error_msg}")
	// StatusDeepagentInputParamError deepagent 输入参数错误
	StatusDeepagentInputParamError = NewStatusCode(
		"DEEPAGENT_INPUT_PARAM_ERROR", 123021,
		"deepagent input parameter error, reason: {error_msg}")
	// StatusDeepagentContextParamError deepagent 回调上下文参数错误
	StatusDeepagentContextParamError = NewStatusCode(
		"DEEPAGENT_CONTEXT_PARAM_ERROR", 123022,
		"deepagent callback context parameter error, reason: {error_msg}")
	// StatusDeepagentRuntimeError deepagent 运行时错误
	StatusDeepagentRuntimeError = NewStatusCode(
		"DEEPAGENT_RUNTIME_ERROR", 123023,
		"deepagent runtime error, reason: {error_msg}")
	// StatusDeepagentTaskLoopNotImplemented deepagent 任务循环未实现
	StatusDeepagentTaskLoopNotImplemented = NewStatusCode(
		"DEEPAGENT_TASK_LOOP_NOT_IMPLEMENTED", 123024,
		"deepagent task loop not implemented, reason: {error_msg}")
	// StatusDeepagentCreateSubagentNotFound 子 agent 未找到
	StatusDeepagentCreateSubagentNotFound = NewStatusCode(
		"DEEPAGENT_CREATE_SUBAGENT_NOT_FOUND", 123025,
		"subagent not found, reason: {error_msg}")
)

// =============================================================================================================
// Multi-Agent 130000–130999
// =============================================================================================================

var (
	// StatusAgentTeamAddRuntimeError Agent team 添加运行时错误
	StatusAgentTeamAddRuntimeError = NewStatusCode(
		"AGENT_TEAM_ADD_RUNTIME_ERROR", 132000,
		"agent team_add runtime error, reason: {error_msg}")
	// StatusAgentTeamCreateRuntimeError Agent team 创建运行时错误
	StatusAgentTeamCreateRuntimeError = NewStatusCode(
		"AGENT_TEAM_CREATE_RUNTIME_ERROR", 132001,
		"agent team_create runtime error, reason: {error_msg}")
	// StatusAgentTeamExecutionError Agent team 执行错误
	StatusAgentTeamExecutionError = NewStatusCode(
		"AGENT_TEAM_EXECUTION_ERROR", 132002,
		"agent team execution error, reason: {error_msg}")
	// StatusAgentTeamAgentNotFound Agent team 中 agent 未找到
	StatusAgentTeamAgentNotFound = NewStatusCode(
		"AGENT_TEAM_AGENT_NOT_FOUND", 132003,
		"agent team agent not found error, reason: {error_msg}")
	// StatusAgentTeamConfigInvalid Agent team 配置无效
	StatusAgentTeamConfigInvalid = NewStatusCode(
		"AGENT_TEAM_CONFIG_INVALID", 132004,
		"agent team config invalid, reason: {reason}")
	// StatusAgentTeamBusyInvalid Agent team 正忙
	StatusAgentTeamBusyInvalid = NewStatusCode(
		"AGENT_TEAM_BUSY_INVALID", 132005,
		"agent team is busy, team='{team_name}', session='{session_id}', reason: {reason}")
	// StatusAgentTeamStateInvalid Agent team 状态不一致
	StatusAgentTeamStateInvalid = NewStatusCode(
		"AGENT_TEAM_STATE_INVALID", 132006,
		"agent team state inconsistent, reason: {reason}")
)

// =============================================================================================================
// DevTools / AgentBuilder 140000–140099
// =============================================================================================================

var (
	// StatusAgentBuilderResourceParseError Agent builder 资源解析错误
	StatusAgentBuilderResourceParseError = NewStatusCode(
		"AGENT_BUILDER_RESOURCE_PARSE_ERROR", 140000,
		"agent builder resource parse error, resource_type='{resource_type}', error='{reason}'")
	// StatusAgentBuilderLLMServiceError Agent builder LLM 服务错误
	StatusAgentBuilderLLMServiceError = NewStatusCode(
		"AGENT_BUILDER_LLM_SERVICE_ERROR", 140001,
		"agent builder llm service error, error='{reason}'")
	// StatusAgentBuilderGeneratorParseError Agent builder 生成器解析错误
	StatusAgentBuilderGeneratorParseError = NewStatusCode(
		"AGENT_BUILDER_GENERATOR_PARSE_ERROR", 140030,
		"agent builder generator parse error, error='{reason}'")
	// StatusAgentBuilderResourceRetrieveError Agent builder 资源检索失败
	StatusAgentBuilderResourceRetrieveError = NewStatusCode(
		"AGENT_BUILDER_RESOURCE_RETRIEVE_ERROR", 140031,
		"agent builder resource retrieve failed, error='{reason}'")
	// StatusAgentBuilderAgentTypeNotSupported Agent builder agent 类型不支持
	StatusAgentBuilderAgentTypeNotSupported = NewStatusCode(
		"AGENT_BUILDER_AGENT_TYPE_NOT_SUPPORTED", 140032,
		"agent builder agent_type is not supported, agent_type='{agent_type}', supported_types='{supported_types}'")
	// StatusAgentBuilderTransformerError Agent builder 转换器错误
	StatusAgentBuilderTransformerError = NewStatusCode(
		"AGENT_BUILDER_TRANSFORMER_ERROR", 140060,
		"agent builder transformer error, error='{reason}'")
	// StatusWorkflowDLGenerationError 工作流 DSL 生成错误
	StatusWorkflowDLGenerationError = NewStatusCode(
		"WORKFLOW_DL_GENERATION_ERROR", 140061,
		"workflow dl generation error, reason: {error_msg}")
	// StatusWorkflowIntentionDetectError 工作流意图检测错误
	StatusWorkflowIntentionDetectError = NewStatusCode(
		"WORKFLOW_INTENTION_DETECT_ERROR", 140062,
		"workflow intention detect error, reason: {error_msg}")
	// StatusLLMAgentStateError LLM agent 状态错误
	StatusLLMAgentStateError = NewStatusCode(
		"LLM_AGENT_STATE_ERROR", 140063,
		"llm agent state error, reason: {error_msg}")
)
