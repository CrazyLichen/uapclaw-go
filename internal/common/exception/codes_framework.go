package exception

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// StatusSuccess 成功
	StatusSuccess = NewStatusCode("SUCCESS", 0, "success")
	// StatusError 通用错误
	StatusError = NewStatusCode("ERROR", -1, "error")
)

// =============================================================================================================
// 180. Foundation — Prompt 180000–180999
// =============================================================================================================

var (
	// StatusPromptAssemblerVariableInitFailed prompt assembler 变量初始化失败
	StatusPromptAssemblerVariableInitFailed = NewStatusCode(
		"PROMPT_ASSEMBLER_VARIABLE_INIT_FAILED", 180000,
		"prompt assembler_variable initialization failed, reason: {error_msg}")
	// StatusPromptAssemblerTemplateParamError prompt assembler 模板参数错误
	StatusPromptAssemblerTemplateParamError = NewStatusCode(
		"PROMPT_ASSEMBLER_TEMPLATE_PARAM_ERROR", 180001,
		"prompt assembler_template parameter error, reason: {error_msg}")
	// StatusPromptTemplateRuntimeError prompt 模板运行时错误
	StatusPromptTemplateRuntimeError = NewStatusCode(
		"PROMPT_TEMPLATE_RUNTIME_ERROR", 180002,
		"prompt template runtime error, reason: {error_msg}")
	// StatusPromptTemplateNotFound prompt 模板未找到
	StatusPromptTemplateNotFound = NewStatusCode(
		"PROMPT_TEMPLATE_NOT_FOUND", 180003,
		"prompt template not found, reason: {error_msg}")
	// StatusPromptTemplateInvalid prompt 模板无效
	StatusPromptTemplateInvalid = NewStatusCode(
		"PROMPT_TEMPLATE_INVALID", 180004,
		"prompt template is invalid, reason: {error_msg}")
)

// =============================================================================================================
// 181. Foundation — Model API 181000–181999
// =============================================================================================================

var (
	// StatusModelProviderInvalid 模型 provider 无效
	StatusModelProviderInvalid = NewStatusCode(
		"MODEL_PROVIDER_INVALID", 181000,
		"model provider is invalid, reason: {error_msg}")
	// StatusModelCallFailed 模型调用失败
	StatusModelCallFailed = NewStatusCode(
		"MODEL_CALL_FAILED", 181001,
		"model call failed, reason: {error_msg}")
	// StatusModelServiceConfigError 模型服务配置错误
	StatusModelServiceConfigError = NewStatusCode(
		"MODEL_SERVICE_CONFIG_ERROR", 181002,
		"model service config error, reason: {error_msg}")
	// StatusModelConfigError 模型配置错误
	StatusModelConfigError = NewStatusCode(
		"MODEL_CONFIG_ERROR", 181003,
		"model config error, reason: {error_msg}")
	// StatusModelInvokeParamError 模型调用参数错误
	StatusModelInvokeParamError = NewStatusCode(
		"MODEL_INVOKE_PARAM_ERROR", 181004,
		"model invoke parameter error, reason: {error_msg}")
	// StatusModelClientConfigInvalid 模型客户端配置无效
	StatusModelClientConfigInvalid = NewStatusCode(
		"MODEL_CLIENT_CONFIG_INVALID", 181005,
		"model client_config is invalid, reason: {error_msg}")
)

// =============================================================================================================
// 183. Foundation — Logger 183000–183999
// =============================================================================================================

var (
	// StatusCommonLogPathInvalid 日志路径无效
	StatusCommonLogPathInvalid = NewStatusCode(
		"COMMON_LOG_PATH_INVALID", 183000,
		"common log_path is invalid, reason: {error_msg}")
	// StatusCommonLogPathInitFailed 日志路径初始化失败
	StatusCommonLogPathInitFailed = NewStatusCode(
		"COMMON_LOG_PATH_INIT_FAILED", 183001,
		"common log_path initialization failed, reason: {error_msg}")
	// StatusCommonLogConfigProcessError 日志配置处理错误
	StatusCommonLogConfigProcessError = NewStatusCode(
		"COMMON_LOG_CONFIG_PROCESS_ERROR", 183002,
		"common log_config process error, reason: {error_msg}")
	// StatusCommonLogConfigInvalid 日志配置无效
	StatusCommonLogConfigInvalid = NewStatusCode(
		"COMMON_LOG_CONFIG_INVALID", 183003,
		"common log_config is invalid, reason: {error_msg}")
	// StatusCommonLogExecutionRuntimeError 日志执行运行时错误
	StatusCommonLogExecutionRuntimeError = NewStatusCode(
		"COMMON_LOG_EXECUTION_RUNTIME_ERROR", 183004,
		"common log_execution runtime error, reason: {error_msg}")
)

// =============================================================================================================
// 184. Foundation — TaskManager 184000–184999
// =============================================================================================================

var (
	// StatusCommonTaskConfigError 协程任务配置错误
	StatusCommonTaskConfigError = NewStatusCode(
		"COMMON_TASK_CONFIG_ERROR", 184000,
		"common coroutine task config error, reason: {error_msg}")
	// StatusCommonTaskNotFound 协程任务未找到
	StatusCommonTaskNotFound = NewStatusCode(
		"COMMON_TASK_NOT_FOUND", 184001,
		"common coroutine task not found, reason: {error_msg}")
)

// =============================================================================================================
// 185. Foundation — Support MCP Tool 185000–185999
// =============================================================================================================

// =============================================================================================================
// 186. Foundation — Store supporting 186000–186100
// =============================================================================================================

var (
	// StatusStoreVectorSchemaInvalid 向量 Schema 无效
	StatusStoreVectorSchemaInvalid = NewStatusCode(
		"STORE_VECTOR_SCHEMA_INVALID", 186000,
		"store vector_schema is invalid, reason: {error_msg}")
	// StatusStoreVectorDocInvalid 向量文档无效
	StatusStoreVectorDocInvalid = NewStatusCode(
		"STORE_VECTOR_DOC_INVALID", 186001,
		"store vector_doc is invalid, reason: {error_msg}")
	// StatusStoreVectorCollectionNotFound 向量集合未找到
	StatusStoreVectorCollectionNotFound = NewStatusCode(
		"STORE_VECTOR_COLLECTION_NOT_FOUND", 186002,
		"store vector_collection not found, collection_name={collection_name}")
	// StatusStoreGraphParamInvalid 图存储参数无效
	StatusStoreGraphParamInvalid = NewStatusCode(
		"STORE_GRAPH_PARAM_INVALID", 186003,
		"store graph_param invalid, reason: {error_msg}")
	// StatusStoreGraphBackendNameInvalid 图存储后端名称无效
	StatusStoreGraphBackendNameInvalid = NewStatusCode(
		"STORE_GRAPH_BACKEND_NAME_INVALID", 186004,
		"store graph_backend name invalid, reason: {error_msg}")
	// StatusStoreGraphBackendAlreadyExists 图存储后端已存在
	StatusStoreGraphBackendAlreadyExists = NewStatusCode(
		"STORE_GRAPH_BACKEND_ALREADY_EXISTS", 186005,
		"store graph_backend exists, name={name}, existing={existing}")
	// StatusStoreGraphProtocolNotImplemented 图存储协议未实现
	StatusStoreGraphProtocolNotImplemented = NewStatusCode(
		"STORE_GRAPH_PROTOCOL_NOT_IMPLEMENTED", 186006,
		"store graph_protocol not implemented, reason: {error_msg}")
	// StatusStoreGraphBackendNotFound 图存储后端未找到
	StatusStoreGraphBackendNotFound = NewStatusCode(
		"STORE_GRAPH_BACKEND_NOT_FOUND", 186007,
		"store graph_backend not found, please register it first, name={name}")
	// StatusStoreGraphFactoryNotInstantiable 图存储工厂不可实例化
	StatusStoreGraphFactoryNotInstantiable = NewStatusCode(
		"STORE_GRAPH_FACTORY_NOT_INSTANTIABLE", 186008,
		"store graph_factory must not be instantiated, class={class_name}")
	// StatusStoreGraphCollectionNotSupported 图存储集合不支持
	StatusStoreGraphCollectionNotSupported = NewStatusCode(
		"STORE_GRAPH_COLLECTION_NOT_SUPPORTED", 186009,
		"store graph_collection not supported, collection={collection}")
)

// =============================================================================================================
// 188. Foundation — Common 188000–188999
// =============================================================================================================

var (
	// StatusCommonSSLContextInitFailed SSL 上下文初始化失败
	StatusCommonSSLContextInitFailed = NewStatusCode(
		"COMMON_SSL_CONTEXT_INIT_FAILED", 188000,
		"common ssl_context initialization failed, reason: {error_msg}")
	// StatusCommonUserConfigProcessError 用户配置处理错误
	StatusCommonUserConfigProcessError = NewStatusCode(
		"COMMON_USER_CONFIG_PROCESS_ERROR", 188001,
		"common user_config process error, reason: {error_msg}")
	// StatusCommonJsonInputProcessError JSON 输入处理错误
	StatusCommonJsonInputProcessError = NewStatusCode(
		"COMMON_JSON_INPUT_PROCESS_ERROR", 188002,
		"common json_input process error, reason: {error_msg}")
	// StatusCommonJsonExecutionProcessError JSON 执行处理错误
	StatusCommonJsonExecutionProcessError = NewStatusCode(
		"COMMON_JSON_EXECUTION_PROCESS_ERROR", 188003,
		"common json_execution process error, reason: {error_msg}")
	// StatusCommonURLInputInvalid URL 输入无效
	StatusCommonURLInputInvalid = NewStatusCode(
		"COMMON_URL_INPUT_INVALID", 188004,
		"common url_input is invalid, reason: {error_msg}")
	// StatusCommonSSLCertInvalid SSL 证书无效
	StatusCommonSSLCertInvalid = NewStatusCode(
		"COMMON_SSL_CERT_INVALID", 188005,
		"common ssl_cert is invalid, reason: {error_msg}")
	// StatusCommonEncryptionError 加密错误
	StatusCommonEncryptionError = NewStatusCode(
		"COMMON_ENCRYPTION_ERROR", 188006,
		"encryption failed, reason: {error_msg}")
	// StatusCommonDecryptionError 解密错误
	StatusCommonDecryptionError = NewStatusCode(
		"COMMON_DECRYPTION_ERROR", 188007,
		"decryption failed, reason: {error_msg}")
)

// =============================================================================================================
// 189. Foundation — Schema 189000–189999
// =============================================================================================================

var (
	// StatusSchemaValidateInvalid Schema 验证无效
	StatusSchemaValidateInvalid = NewStatusCode(
		"SCHEMA_VALIDATE_INVALID", 189001,
		"validate data with schema failed, error='{reason}', data={data}")
	// StatusSchemaFormatInvalid Schema 格式化无效
	StatusSchemaFormatInvalid = NewStatusCode(
		"SCHEMA_FORMAT_INVALID", 189002,
		"format data with schema failed, error='{reason}', data={data}")
)
