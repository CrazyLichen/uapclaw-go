package exception

// ──────────────────────────── 全局变量 ────────────────────────────

// =============================================================================================================
// 100. 工作流 100000–100999
// =============================================================================================================
var (
	// StatusWorkflowComponentIDInvalid 工作流组件 ID 无效
	StatusWorkflowComponentIDInvalid = NewStatusCode(
		"WORKFLOW_COMPONENT_ID_INVALID", 100010,
		"the component id is invalid for component '{comp_id}', reason='{reason}', workflow='{workflow}'")
	// StatusWorkflowComponentAbilityInvalid 工作流组件能力无效
	StatusWorkflowComponentAbilityInvalid = NewStatusCode(
		"WORKFLOW_COMPONENT_ABILITY_INVALID", 100011,
		"the ability is invalid for component '{comp_id}', ability={ability}, reason='{reason}', workflow='{workflow}'")
	// StatusWorkflowEdgeInvalid 工作流边无效
	StatusWorkflowEdgeInvalid = NewStatusCode(
		"WORKFLOW_EDGE_INVALID", 100012,
		"edge is invalid, reason='{reason}', source='{src_cmp_id}', target='{target_cmp_id}', workflow='{workflow}'")
	// StatusWorkflowConditionEdgeInvalid 工作流条件边无效
	StatusWorkflowConditionEdgeInvalid = NewStatusCode(
		"WORKFLOW_CONDITION_EDGE_INVALID", 100013,
		"condition edge is invalid, reason='{reason}'. source='{src_cmp_id}', workflow='{workflow}'")
	// StatusWorkflowComponentSchemaInvalid 工作流组件输入/输出 schema 无效
	StatusWorkflowComponentSchemaInvalid = NewStatusCode(
		"WORKFLOW_COMPONENT_SCHEMA_INVALID", 100014,
		"component input/output schema is invalid for component '{comp_id}', reason='{reason}', workflow='{workflow}'")
	// StatusWorkflowStreamEdgeInvalid 工作流流式边无效
	StatusWorkflowStreamEdgeInvalid = NewStatusCode(
		"WORKFLOW_STREAM_EDGE_INVALID", 100015,
		"stream edge is invalid, reason='{reason}', source='{src_cmp_id}', target='{target_cmp_id}', workflow='{workflow}'")
	// StatusWorkflowExecuteInputInvalid 工作流执行输入无效
	StatusWorkflowExecuteInputInvalid = NewStatusCode(
		"WORKFLOW_EXECUTE_INPUT_INVALID", 100016,
		"workflow execute input is invalid, inputs='{inputs}', reason='{reason}', workflow='{workflow}'")
	// StatusWorkflowExecuteSessionInvalid 工作流执行会话无效
	StatusWorkflowExecuteSessionInvalid = NewStatusCode(
		"WORKFLOW_EXECUTE_SESSION_INVALID", 100017,
		"execute session is invalid, reason='{reason}', workflow='{workflow}'")
)

// 1. 工作流执行错误码 (100100 - 100199)

var (
	// StatusWorkflowCompileError 工作流编译错误
	StatusWorkflowCompileError = NewStatusCode(
		"WORKFLOW_COMPILE_ERROR", 100100,
		"workflow compilation has error, error='{reason}', workflow={workflow}")
	// StatusWorkflowExecutionTimeout 工作流执行超时
	StatusWorkflowExecutionTimeout = NewStatusCode(
		"WORKFLOW_EXECUTION_TIMEOUT", 100101,
		"workflow execution exceeded time limit of {timeout} seconds, workflow='{workflow}'")
	// StatusWorkflowExecutionError 工作流执行错误
	StatusWorkflowExecutionError = NewStatusCode(
		"WORKFLOW_EXECUTION_ERROR", 100102,
		"workflow execution has error, error='{reason}', workflow='{workflow}'")
)

// 2. 工作流组件编排错误码 (100200 - 100299)

var (
	// StatusWorkflowInnerOrchestrationError 工作流内部编排错误
	StatusWorkflowInnerOrchestrationError = NewStatusCode(
		"WORKFLOW_INNER_ORCHESTRATION_ERROR", 100053,
		"workflow inner orchestration error, error='{reason}'")
	// StatusWorkflowComponentExecutionError 工作流组件执行错误
	StatusWorkflowComponentExecutionError = NewStatusCode(
		"WORKFLOW_COMPONENT_EXECUTION_ERROR", 100054,
		"component '{comp}' execute '{ability}' error, reason='{reason}', workflow='{workflow}'")
)

// =============================================================================================================
// 101. 内置工作流组件 101000–101999
// =============================================================================================================

// 00. 开始组件 101000 - 101009

// 01. 结束组件 101010 - 101019

var (
	// StatusComponentEndParamInvalid 结束组件参数无效
	StatusComponentEndParamInvalid = NewStatusCode(
		"COMPONENT_END_PARAM_INVALID", 100010,
		"component end params is invalid, error='{reason}'")
)

// 02. 分支组件 101020 - 101029

var (
	// StatusComponentBranchParamInvalid 分支组件参数无效
	StatusComponentBranchParamInvalid = NewStatusCode(
		"COMPONENT_BRANCH_PARAM_INVALID", 101020,
		"component branch params is invalid, error='{reason}'")
	// StatusComponentBranchExecutionError 分支组件执行错误
	StatusComponentBranchExecutionError = NewStatusCode(
		"COMPONENT_BRANCH_EXECUTION_ERROR", 101021,
		"component branch execution error, error='{reason}'")
	// StatusExpressionSyntaxError 表达式语法错误
	StatusExpressionSyntaxError = NewStatusCode(
		"EXPRESSION_SYNTAX_ERROR", 101024,
		"expression syntax error")
	// StatusExpressionEvalError 表达式求值错误
	StatusExpressionEvalError = NewStatusCode(
		"EXPRESSION_EVAL_ERROR", 101025,
		"expression evaluation error, reason: {error_msg}")
	// StatusArrayConditionError 数组条件错误
	StatusArrayConditionError = NewStatusCode(
		"ARRAY_CONDITION_ERROR", 101026,
		"array condition error")
	// StatusNumberConditionError 数值条件错误
	StatusNumberConditionError = NewStatusCode(
		"NUMBER_CONDITION_ERROR", 101027,
		"number condition error, reason: {error_msg}")
)

// 03. 循环组件 101030 - 101049

var (
	// StatusComponentLoopGroupParamInvalid 循环组参数无效
	StatusComponentLoopGroupParamInvalid = NewStatusCode(
		"COMPONENT_LOOP_GROUP_PARAM_INVALID", 101030,
		"loop group params is invalid, error='{reason}'")
	// StatusComponentLoopSetVarParamInvalid 循环设置变量参数无效
	StatusComponentLoopSetVarParamInvalid = NewStatusCode(
		"COMPONENT_LOOP_SET_VAR_PARAM_INVALID", 101031,
		"loop set_var params invalid, error='{reason}'")
	// StatusComponentLoopExecutionError 循环执行错误
	StatusComponentLoopExecutionError = NewStatusCode(
		"COMPONENT_LOOP_EXECUTION_ERROR", 101040,
		"loop execution error, error='{reason}', comp='{comp}'")
	// StatusComponentLoopConditionExecutionError 循环条件执行错误
	StatusComponentLoopConditionExecutionError = NewStatusCode(
		"COMPONENT_LOOP_CONDITION_EXECUTION_ERROR", 101041,
		"loop condition execution error, error='{reason}', comp='{comp}'")
	// StatusComponentLoopBreakExecutionError 循环中断执行错误
	StatusComponentLoopBreakExecutionError = NewStatusCode(
		"COMPONENT_LOOP_BREAK_EXECUTION_ERROR", 101042,
		"loop break execution error, error='{reason}', comp='{comp}'")
	// StatusComponentLoopSetVarExecutionError 循环设置变量执行错误
	StatusComponentLoopSetVarExecutionError = NewStatusCode(
		"COMPONENT_LOOP_SET_VAR_EXECUTION_ERROR", 101043,
		"loop set_var execution error, error='{reason}', comp='{comp}'")
)

// 05. 子工作流组件 101150 - 101159

var (
	// StatusComponentSubWorkflowParamInvalid 子工作流组件参数无效
	StatusComponentSubWorkflowParamInvalid = NewStatusCode(
		"COMPONENT_SUB_WORKFLOW_PARAM_INVALID", 101150,
		"component sub_workflow param is invalid, error='{reason}'")
)

// LLM 组件 101000 - 101049

var (
	// StatusComponentLLMTemplateConfigError LLM 模板配置错误
	StatusComponentLLMTemplateConfigError = NewStatusCode(
		"COMPONENT_LLM_TEMPLATE_CONFIG_ERROR", 101000,
		"component llm_template config error, reason: {error_msg}")
	// StatusComponentLLMResponseConfigInvalid LLM 响应配置无效
	StatusComponentLLMResponseConfigInvalid = NewStatusCode(
		"COMPONENT_LLM_RESPONSE_CONFIG_INVALID", 101001,
		"component llm_response_config is invalid, reason: {error_msg}")
	// StatusComponentLLMConfigError LLM 配置错误
	StatusComponentLLMConfigError = NewStatusCode(
		"COMPONENT_LLM_CONFIG_ERROR", 101002,
		"component llm config error, reason: {error_msg}")
	// StatusComponentLLMInvokeCallFailed LLM 调用失败
	StatusComponentLLMInvokeCallFailed = NewStatusCode(
		"COMPONENT_LLM_INVOKE_CALL_FAILED", 101003,
		"component llm_invoke call failed, reason: {error_msg}")
	// StatusComponentLLMExecutionProcessError LLM 执行过程错误
	StatusComponentLLMExecutionProcessError = NewStatusCode(
		"COMPONENT_LLM_EXECUTION_PROCESS_ERROR", 101004,
		"component llm_execution process error, reason: {error_msg}")
	// StatusComponentLLMInitFailed LLM 初始化失败
	StatusComponentLLMInitFailed = NewStatusCode(
		"COMPONENT_LLM_INIT_FAILED", 101005,
		"component llm initialization failed, reason: {error_msg}")
	// StatusComponentLLMTemplateProcessError LLM 模板处理错误
	StatusComponentLLMTemplateProcessError = NewStatusCode(
		"COMPONENT_LLM_TEMPLATE_PROCESS_ERROR", 101006,
		"component llm_template process error, reason: {error_msg}")
	// StatusComponentLLMConfigInvalid LLM 配置无效
	StatusComponentLLMConfigInvalid = NewStatusCode(
		"COMPONENT_LLM_CONFIG_INVALID", 101007,
		"component llm_config is invalid, reason: {error_msg}")
)

// 意图检测组件 101050 - 101069

var (
	// StatusComponentIntentDetectionInputParamError 意图检测输入参数错误
	StatusComponentIntentDetectionInputParamError = NewStatusCode(
		"COMPONENT_INTENT_DETECTION_INPUT_PARAM_ERROR", 101050,
		"component intent_detection_input parameter error, reason: {error_msg}")
	// StatusComponentIntentDetectionLLMInitFailed 意图检测 LLM 初始化失败
	StatusComponentIntentDetectionLLMInitFailed = NewStatusCode(
		"COMPONENT_INTENT_DETECTION_LLM_INIT_FAILED", 101051,
		"component intent_detection_llm initialization failed, reason: {error_msg}")
	// StatusComponentIntentDetectionInvokeCallFailed 意图检测调用失败
	StatusComponentIntentDetectionInvokeCallFailed = NewStatusCode(
		"COMPONENT_INTENT_DETECTION_INVOKE_CALL_FAILED", 101052,
		"component intent_detection_invoke call failed, reason: {error_msg}")
)

// 问题组件 101070 - 101099

var (
	// StatusComponentQuestionerInputParamError 提问组件输入参数错误
	StatusComponentQuestionerInputParamError = NewStatusCode(
		"COMPONENT_QUESTIONER_INPUT_PARAM_ERROR", 101070,
		"component questioner_input parameter error, reason: {error_msg}")
	// StatusComponentQuestionerConfigError 提问组件配置错误
	StatusComponentQuestionerConfigError = NewStatusCode(
		"COMPONENT_QUESTIONER_CONFIG_ERROR", 101071,
		"component questioner config error, reason: {error_msg}")
	// StatusComponentQuestionerInputInvalid 提问组件输入无效
	StatusComponentQuestionerInputInvalid = NewStatusCode(
		"COMPONENT_QUESTIONER_INPUT_INVALID", 101072,
		"component questioner_input is invalid, reason: {error_msg}")
	// StatusComponentQuestionerStateInitFailed 提问组件状态初始化失败
	StatusComponentQuestionerStateInitFailed = NewStatusCode(
		"COMPONENT_QUESTIONER_STATE_INIT_FAILED", 101073,
		"component questioner_state initialization failed, reason: {error_msg}")
	// StatusComponentQuestionerRuntimeError 提问组件运行时错误
	StatusComponentQuestionerRuntimeError = NewStatusCode(
		"COMPONENT_QUESTIONER_RUNTIME_ERROR", 101074,
		"component questioner runtime error, reason: {error_msg}")
	// StatusComponentQuestionerInvokeCallFailed 提问组件调用失败
	StatusComponentQuestionerInvokeCallFailed = NewStatusCode(
		"COMPONENT_QUESTIONER_INVOKE_CALL_FAILED", 101075,
		"component questioner_invoke call failed, reason: {error_msg}")
	// StatusComponentQuestionerExecutionProcessError 提问组件执行过程错误
	StatusComponentQuestionerExecutionProcessError = NewStatusCode(
		"COMPONENT_QUESTIONER_EXECUTION_PROCESS_ERROR", 101076,
		"component questioner_execution process error, reason: {error_msg}")
)

// 知识检索组件 101100 - 101149

var (
	// StatusComponentKnowledgeRetrievalInvokeCallFailed 知识检索组件调用失败
	StatusComponentKnowledgeRetrievalInvokeCallFailed = NewStatusCode(
		"COMPONENT_KNOWLEDGE_RETRIEVAL_INVOKE_CALL_FAILED", 101100,
		"component knowledge_retrieval invoke call failed, reason: {error_msg}")
	// StatusComponentKnowledgeRetrievalEmbedModelInitError 知识检索嵌入模型初始化错误
	StatusComponentKnowledgeRetrievalEmbedModelInitError = NewStatusCode(
		"COMPONENT_KNOWLEDGE_RETRIEVAL_EMBED_MODEL_INIT_ERROR", 101101,
		"component knowledge_retrieval embed_model initialization error, reason: {error_msg}")
	// StatusComponentKnowledgeRetrievalInputParamError 知识检索输入参数错误
	StatusComponentKnowledgeRetrievalInputParamError = NewStatusCode(
		"COMPONENT_KNOWLEDGE_RETRIEVAL_INPUT_PARAM_ERROR", 101102,
		"component knowledge_retrieval input parameter error, reason: {error_msg}")
	// StatusComponentKnowledgeRetrievalLLMModelInitError 知识检索 LLM 模型初始化错误
	StatusComponentKnowledgeRetrievalLLMModelInitError = NewStatusCode(
		"COMPONENT_KNOWLEDGE_RETRIEVAL_LLM_MODEL_INIT_ERROR", 101103,
		"component knowledge_retrieval llm_model initialization failed, reason: {error_msg}")
)

// 记忆写入组件 101150 - 101199

var (
	// StatusComponentMemoryWriteInputParamError 记忆写入输入参数错误
	StatusComponentMemoryWriteInputParamError = NewStatusCode(
		"COMPONENT_MEMORY_WRITE_INPUT_PARAM_ERROR", 101150,
		"component memory_write input parameter error, reason: {error_msg}")
	// StatusComponentMemoryWriteInvokeCallFailed 记忆写入调用失败
	StatusComponentMemoryWriteInvokeCallFailed = NewStatusCode(
		"COMPONENT_MEMORY_WRITE_INVOKE_CALL_FAILED", 101151,
		"component memory_write invoke call failed, reason: {error_msg}")
)

// 记忆检索组件 101200 - 101249

var (
	// StatusComponentMemoryRetrievalInputParamError 记忆检索输入参数错误
	StatusComponentMemoryRetrievalInputParamError = NewStatusCode(
		"COMPONENT_MEMORY_RETRIEVAL_INPUT_PARAM_ERROR", 101200,
		"component memory_retrieval input parameter error, reason: {error_msg}")
	// StatusComponentMemoryRetrievalInvokeCallFailed 记忆检索调用失败
	StatusComponentMemoryRetrievalInvokeCallFailed = NewStatusCode(
		"COMPONENT_MEMORY_RETRIEVAL_INVOKE_CALL_FAILED", 101201,
		"component memory_retrieval invoke call failed, reason: {error_msg}")
)

// 工具组件 102000 - 102019

var (
	// StatusComponentToolExecutionError 工具组件执行错误
	StatusComponentToolExecutionError = NewStatusCode(
		"COMPONENT_TOOL_EXECUTION_ERROR", 102000,
		"component tool execution error, reason: {error_msg}")
	// StatusComponentToolInputParamError 工具组件输入参数错误
	StatusComponentToolInputParamError = NewStatusCode(
		"COMPONENT_TOOL_INPUT_PARAM_ERROR", 102001,
		"component tool_input parameter error, reason: {error_msg}")
	// StatusComponentToolInitFailed 工具组件初始化失败
	StatusComponentToolInitFailed = NewStatusCode(
		"COMPONENT_TOOL_INIT_FAILED", 102002,
		"component tool initialization failed, reason: {error_msg}")
)
