package exception

// ──────────────────────────── 全局变量 ────────────────────────────

// =============================================================================================================
// Optimization Toolchain — Prompt Self-optimization 170000–170999
// =============================================================================================================

var (
	// StatusToolchainAgentParamError 工具链 agent 参数错误
	StatusToolchainAgentParamError = NewStatusCode(
		"TOOLCHAIN_AGENT_PARAM_ERROR", 170000,
		"toolchain agent parameter error, reason: {error_msg}")
	// StatusToolchainOptimizerBackwardExecutionError 工具链 optimizer backward 执行错误
	StatusToolchainOptimizerBackwardExecutionError = NewStatusCode(
		"TOOLCHAIN_OPTIMIZER_BACKWARD_EXECUTION_ERROR", 170001,
		"toolchain optimizer_backword execution error, reason: {error_msg}")
	// StatusToolchainOptimizerUpdateExecutionError 工具链 optimizer update 执行错误
	StatusToolchainOptimizerUpdateExecutionError = NewStatusCode(
		"TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR", 170002,
		"toolchain optimizer_update execution error, reason: {error_msg}")
	// StatusToolchainOptimizerParamError 工具链 optimizer 参数错误
	StatusToolchainOptimizerParamError = NewStatusCode(
		"TOOLCHAIN_OPTIMIZER_PARAM_ERROR", 170003,
		"toolchain optimizer parameter error, reason: {error_msg}")
	// StatusToolchainEvaluatorExecutionError 工具链评估器执行错误
	StatusToolchainEvaluatorExecutionError = NewStatusCode(
		"TOOLCHAIN_EVALUATOR_EXECUTION_ERROR", 170004,
		"toolchain evaluator execution error, reason: {error_msg}")
	// StatusToolchainTrainerExecutionError 工具链训练器执行错误
	StatusToolchainTrainerExecutionError = NewStatusCode(
		"TOOLCHAIN_TRAINER_EXECUTION_ERROR", 170005,
		"toolchain trainer execution error, reason: {error_msg}")
)

// =============================================================================================================
// Optimization Toolchain — AgentRL 172000–172999
// =============================================================================================================

var (
	// StatusAgentRlProxyNotInitialized AgentRL proxy 未初始化
	StatusAgentRlProxyNotInitialized = NewStatusCode(
		"AGENT_RL_PROXY_NOT_INITIALIZED", 172000,
		"agent_rl proxy has not been initialized, reason: {error_msg}")
	// StatusAgentRlProxyServerStartFailed AgentRL proxy 服务启动失败
	StatusAgentRlProxyServerStartFailed = NewStatusCode(
		"AGENT_RL_PROXY_SERVER_START_FAILED", 172001,
		"agent_rl proxy server failed to start, host='{host}', port='{port}'")
	// StatusAgentRlExecutorNotInitialized AgentRL executor 未配置
	StatusAgentRlExecutorNotInitialized = NewStatusCode(
		"AGENT_RL_EXECUTOR_NOT_INITIALIZED", 172010,
		"agent_rl executor is not configured, reason: {error_msg}")
	// StatusAgentRlProcessorNotFound AgentRL processor 未找到
	StatusAgentRlProcessorNotFound = NewStatusCode(
		"AGENT_RL_PROCESSOR_NOT_FOUND", 172020,
		"agent_rl {processor_type} processor not found, name='{name}', available='{available}'")
	// StatusAgentRlDependencyInitFailed AgentRL 依赖初始化失败
	StatusAgentRlDependencyInitFailed = NewStatusCode(
		"AGENT_RL_DEPENDENCY_INIT_FAILED", 172030,
		"agent_rl required dependency initialization failed, reason: {error_msg}")
	// StatusAgentRlRolloutBatchExecutionError AgentRL rollout 批量执行错误
	StatusAgentRlRolloutBatchExecutionError = NewStatusCode(
		"AGENT_RL_ROLLOUT_BATCH_EXECUTION_ERROR", 172040,
		"agent_rl rollout batch execution error, reason: {error_msg}")
	// StatusAgentRlStrategyNotSupported AgentRL 训练策略不支持
	StatusAgentRlStrategyNotSupported = NewStatusCode(
		"AGENT_RL_STRATEGY_NOT_SUPPORTED", 172050,
		"agent_rl training strategy is not supported, strategy='{strategy}'")
	// StatusAgentRlTrainerNotInitialized AgentRL trainer 未初始化
	StatusAgentRlTrainerNotInitialized = NewStatusCode(
		"AGENT_RL_TRAINER_NOT_INITIALIZED", 172051,
		"agent_rl trainer not initialized, reason: {error_msg}")
	// StatusAgentRlTaskRunnerNotInitialized AgentRL task runner 未初始化
	StatusAgentRlTaskRunnerNotInitialized = NewStatusCode(
		"AGENT_RL_TASK_RUNNER_NOT_INITIALIZED", 172052,
		"agent_rl task runner not initialized, reason: {error_msg}")
	// StatusAgentRlValidationDatasetInvalid AgentRL 验证数据集无效
	StatusAgentRlValidationDatasetInvalid = NewStatusCode(
		"AGENT_RL_VALIDATION_DATASET_INVALID", 172060,
		"agent_rl validation dataset is invalid, reason: {error_msg}")
	// StatusAgentRlBatchDataTypeInvalid AgentRL 批量数据类型无效
	StatusAgentRlBatchDataTypeInvalid = NewStatusCode(
		"AGENT_RL_BATCH_DATA_TYPE_INVALID", 172061,
		"agent_rl batch data type is invalid, data_type='{data_type}'")
	// StatusAgentRlRewardNameInvalid AgentRL 奖励名称无效
	StatusAgentRlRewardNameInvalid = NewStatusCode(
		"AGENT_RL_REWARD_NAME_INVALID", 172070,
		"agent_rl reward name is invalid, reason: {error_msg}")
	// StatusAgentRlRewardNotFound AgentRL 奖励函数未找到
	StatusAgentRlRewardNotFound = NewStatusCode(
		"AGENT_RL_REWARD_NOT_FOUND", 172071,
		"agent_rl reward function not found, name='{name}'")
)

// =============================================================================================================
// Optimization Toolchain — Prompt Builder 173000–173999
// =============================================================================================================

var (
	// StatusToolchainMetaTemplateExecutionError 工具链 meta_template 执行错误
	StatusToolchainMetaTemplateExecutionError = NewStatusCode(
		"TOOLCHAIN_META_TEMPLATE_EXECUTION_ERROR", 173000,
		"toolchain meta_template execution error, reason: {error_msg}")
	// StatusToolchainFeedbackTemplateExecutionError 工具链 feedback_template 执行错误
	StatusToolchainFeedbackTemplateExecutionError = NewStatusCode(
		"TOOLCHAIN_FEEDBACK_TEMPLATE_EXECUTION_ERROR", 173001,
		"toolchain feedback_template execution error, reason: {error_msg}")
	// StatusToolchainBadCaseTemplateExecutionError 工具链 bad_case_template 执行错误
	StatusToolchainBadCaseTemplateExecutionError = NewStatusCode(
		"TOOLCHAIN_BAD_CASE_TEMPLATE_EXECUTION_ERROR", 173002,
		"toolchain bad_case_template execution error, reason: {error_msg}")
)

// =============================================================================================================
// Optimization Toolchain — Task Memory 174000–174025
// =============================================================================================================

var (
	// StatusToolchainEvolvingMemoryRetrieveExecutionError 工具链演进记忆检索执行错误
	StatusToolchainEvolvingMemoryRetrieveExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_RETRIEVE_EXECUTION_ERROR", 174000,
		"toolchain evolving memory retrieve execution error, reason: {error_msg}")
	// StatusToolchainEvolvingMemorySummarizeExecutionError 工具链演进记忆摘要执行错误
	StatusToolchainEvolvingMemorySummarizeExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_SUMMARIZE_EXECUTION_ERROR", 174001,
		"toolchain evolving memory summarize execution error, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryAddExecutionError 工具链演进记忆添加执行错误
	StatusToolchainEvolvingMemoryAddExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_ADD_EXECUTION_ERROR", 174002,
		"toolchain evolving memory add memory execution error, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryFetchExecutionError 工具链演进记忆获取 playbook 执行错误
	StatusToolchainEvolvingMemoryFetchExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_FETCH_EXECUTION_ERROR", 174003,
		"toolchain evolving memory get playbook execution error, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryClearExecutionError 工具链演进记忆清除 playbook 执行错误
	StatusToolchainEvolvingMemoryClearExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_CLEAR_EXECUTION_ERROR", 174004,
		"toolchain evolving memory clear playbook execution error, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryConfigInvalid 工具链演进记忆配置无效
	StatusToolchainEvolvingMemoryConfigInvalid = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_CONFIG_INVALID", 174005,
		"toolchain evolving memory config is invalid, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryServiceInitFailed 工具链演进记忆服务初始化失败
	StatusToolchainEvolvingMemoryServiceInitFailed = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_SERVICE_INIT_FAILED", 174006,
		"toolchain evolving memory service initialization failed, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryEmbeddingExecutionError 工具链演进记忆 embedding 执行错误
	StatusToolchainEvolvingMemoryEmbeddingExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_EMBEDDING_EXECUTION_ERROR", 174007,
		"toolchain evolving memory embedding execution error, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryLlmGenerationExecutionError 工具链演进记忆 LLM 生成执行错误
	StatusToolchainEvolvingMemoryLlmGenerationExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_LLM_GENERATION_EXECUTION_ERROR", 174008,
		"toolchain evolving memory llm generation execution error, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryDbConnectorExecutionError 工具链演进记忆数据库连接器执行错误
	StatusToolchainEvolvingMemoryDbConnectorExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_DB_CONNECTOR_EXECUTION_ERROR", 174009,
		"toolchain evolving memory db connector execution error, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryFileIoExecutionError 工具链演进记忆文件 I/O 执行错误
	StatusToolchainEvolvingMemoryFileIoExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_FILE_IO_EXECUTION_ERROR", 174010,
		"toolchain evolving memory file I/O execution error, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryVectorStoreExecutionError 工具链演进记忆向量存储执行错误
	StatusToolchainEvolvingMemoryVectorStoreExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_VECTOR_STORE_EXECUTION_ERROR", 174011,
		"toolchain evolving memory vector store execution error, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryInjectionExecutionError 工具链演进记忆提示注入执行错误
	StatusToolchainEvolvingMemoryInjectionExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_INJECTION_EXECUTION_ERROR", 174012,
		"toolchain evolving memory prompt injection execution error, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryStateRestoreExecutionError 工具链演进记忆状态恢复执行错误
	StatusToolchainEvolvingMemoryStateRestoreExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_STATE_RESTORE_EXECUTION_ERROR", 174013,
		"toolchain evolving memory prompt state restore execution error, reason: {error_msg}")
	// StatusToolchainEvolvingMemoryInputInvalid 工具链演进记忆输入无效
	StatusToolchainEvolvingMemoryInputInvalid = NewStatusCode(
		"TOOLCHAIN_EVOLVING_MEMORY_INPUT_INVALID", 174014,
		"toolchain evolving memory input is invalid, reason: {error_msg}")
)

// =============================================================================================================
// Optimization Toolchain — Tool Self-optimization 174025–174049
// =============================================================================================================

var (
	// StatusToolchainEvolvingToolCallConfigError 工具链 tool_call 配置错误
	StatusToolchainEvolvingToolCallConfigError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_TOOL_CALL_CONFIG_ERROR", 174025,
		"toolchain optimizer tool_call config error, reason: {error_msg}")
	// StatusToolchainEvolvingToolCallParamError 工具链 tool_call 参数错误
	StatusToolchainEvolvingToolCallParamError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_TOOL_CALL_PARAM_ERROR", 174026,
		"toolchain optimizer tool_call parameter error, reason: {error_msg}")
	// StatusToolchainEvolvingToolCallRuntimeError 工具链 tool_call 运行时错误
	StatusToolchainEvolvingToolCallRuntimeError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_TOOL_CALL_RUNTIME_ERROR", 174027,
		"toolchain optimizer tool_call runtime error, reason: {error_msg}")
	// StatusToolchainEvolvingToolCallExampleStageExecutionError 工具链 tool_call example_stage 执行错误
	StatusToolchainEvolvingToolCallExampleStageExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_TOOL_CALL_EXAMPLE_STAGE_EXECUTION_ERROR", 174028,
		"toolchain optimizer tool_call example_stage execution error, reason: {error_msg}")
	// StatusToolchainEvolvingToolCallBeamSearchExecutionError 工具链 tool_call beam_search 执行错误
	StatusToolchainEvolvingToolCallBeamSearchExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_TOOL_CALL_BEAM_SEARCH_EXECUTION_ERROR", 174029,
		"toolchain optimizer tool_call beam_search execution error, reason: {error_msg}")
	// StatusToolchainEvolvingToolCallEvaluatorExecutionError 工具链 tool_call 评估器执行错误
	StatusToolchainEvolvingToolCallEvaluatorExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_TOOL_CALL_EVALUATOR_EXECUTION_ERROR", 174030,
		"toolchain optimizer tool_call evaluator execution error, reason: {error_msg}")
	// StatusToolchainEvolvingToolCallLlmCallExecutionError 工具链 tool_call LLM 调用执行错误
	StatusToolchainEvolvingToolCallLlmCallExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_TOOL_CALL_LLM_CALL_EXECUTION_ERROR", 174031,
		"toolchain optimizer tool_call llm_call execution error, reason: {error_msg}")
	// StatusToolchainEvolvingToolCallReviewerExecutionError 工具链 tool_call reviewer 执行错误
	StatusToolchainEvolvingToolCallReviewerExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_TOOL_CALL_REVIEWER_EXECUTION_ERROR", 174032,
		"toolchain optimizer tool_call reviewer execution error, reason: {error_msg}")
	// StatusToolchainEvolvingToolCallSchemaExtractExecutionError 工具链 tool_call schema 提取执行错误
	StatusToolchainEvolvingToolCallSchemaExtractExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_TOOL_CALL_SCHEMA_EXTRACT_EXECUTION_ERROR", 174033,
		"toolchain optimizer tool_call schema_extract execution error, reason: {error_msg}")
	// StatusToolchainEvolvingToolCallOutputParseError 工具链 tool_call 输出解析错误
	StatusToolchainEvolvingToolCallOutputParseError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_TOOL_CALL_OUTPUT_PARSE_ERROR", 174034,
		"toolchain optimizer tool_call output parse error, reason: {error_msg}")
	// StatusToolchainEvolvingToolCallLoggingExecutionError 工具链 tool_call 日志执行错误
	StatusToolchainEvolvingToolCallLoggingExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_TOOL_CALL_LOGGING_EXECUTION_ERROR", 174035,
		"toolchain optimizer tool_call logging execution error, reason: {error_msg}")
	// StatusToolchainEvolvingToolCallResultPersistExecutionError 工具链 tool_call 结果持久化执行错误
	StatusToolchainEvolvingToolCallResultPersistExecutionError = NewStatusCode(
		"TOOLCHAIN_EVOLVING_TOOL_CALL_RESULT_PERSIST_EXECUTION_ERROR", 174036,
		"toolchain optimizer tool_call result persist execution error, reason: {error_msg}")
)
