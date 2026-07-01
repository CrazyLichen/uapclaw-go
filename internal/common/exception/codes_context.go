package exception

// ──────────────────────────── 全局变量 ────────────────────────────

// =============================================================================================================
// 上下文引擎 150000–154999
// =============================================================================================================
var (
	// StatusContextMessageProcessError 上下文消息处理错误
	StatusContextMessageProcessError = NewStatusCode(
		"CONTEXT_MESSAGE_PROCESS_ERROR", 153000,
		"context message process error, reason: {error_msg}")
	// StatusContextExecutionError 上下文执行错误
	StatusContextExecutionError = NewStatusCode(
		"CONTEXT_EXECUTION_ERROR", 153001,
		"context execution execution error, reason: {error_msg}")
	// StatusContextMessageInvalid 上下文消息无效
	StatusContextMessageInvalid = NewStatusCode(
		"CONTEXT_MESSAGE_INVALID", 153003,
		"context message is invalid, reason: {error_msg}")
)

// =============================================================================================================
// 知识库检索 — 向量化 155000–155099
// =============================================================================================================

var (
	// StatusRetrievalEmbeddingInputInvalid 检索 embedding 输入无效
	StatusRetrievalEmbeddingInputInvalid = NewStatusCode(
		"RETRIEVAL_EMBEDDING_INPUT_INVALID", 155000,
		"retrieval embedding_input is invalid, reason: {error_msg}")
	// StatusRetrievalEmbeddingModelNotFound 检索 embedding 模型未找到
	StatusRetrievalEmbeddingModelNotFound = NewStatusCode(
		"RETRIEVAL_EMBEDDING_MODEL_NOT_FOUND", 155001,
		"retrieval embedding_model not found, reason: {error_msg}")
	// StatusRetrievalEmbeddingCallFailed 检索 embedding 调用失败
	StatusRetrievalEmbeddingCallFailed = NewStatusCode(
		"RETRIEVAL_EMBEDDING_CALL_FAILED", 155002,
		"retrieval embedding call failed, reason: {error_msg}")
	// StatusRetrievalEmbeddingResponseInvalid 检索 embedding 响应无效
	StatusRetrievalEmbeddingResponseInvalid = NewStatusCode(
		"RETRIEVAL_EMBEDDING_RESPONSE_INVALID", 155003,
		"retrieval embedding_response is invalid, reason: {error_msg}")
	// StatusRetrievalEmbeddingRequestCallFailed 检索 embedding 请求调用失败
	StatusRetrievalEmbeddingRequestCallFailed = NewStatusCode(
		"RETRIEVAL_EMBEDDING_REQUEST_CALL_FAILED", 155004,
		"retrieval embedding_request call failed, reason: {error_msg}")
	// StatusRetrievalEmbeddingUnreachableCallFailed 检索 embedding 不可达调用失败
	StatusRetrievalEmbeddingUnreachableCallFailed = NewStatusCode(
		"RETRIEVAL_EMBEDDING_UNREACHABLE_CALL_FAILED", 155005,
		"retrieval embedding call failed, reason: {error_msg}")
	// StatusRetrievalEmbeddingCallbackInvalid 检索 embedding 回调无效
	StatusRetrievalEmbeddingCallbackInvalid = NewStatusCode(
		"RETRIEVAL_EMBEDDING_CALLBACK_INVALID", 155006,
		"retrieval embedding_callback is invalid, reason: {error_msg}")
)

// =============================================================================================================
// 知识库检索 — 索引 155100–155199
// =============================================================================================================

var (
	// StatusRetrievalIndexingChunkSizeInvalid 检索索引分块大小无效
	StatusRetrievalIndexingChunkSizeInvalid = NewStatusCode(
		"RETRIEVAL_INDEXING_CHUNK_SIZE_INVALID", 155100,
		"retrieval indexing_chunk_size is invalid, reason: {error_msg}")
	// StatusRetrievalIndexingChunkOverlapInvalid 检索索引分块重叠无效
	StatusRetrievalIndexingChunkOverlapInvalid = NewStatusCode(
		"RETRIEVAL_INDEXING_CHUNK_OVERLAP_INVALID", 155101,
		"retrieval indexing_chunk_overlap is invalid, reason: {error_msg}")
	// StatusRetrievalIndexingTokenizerProcessError 检索索引分词器处理错误
	StatusRetrievalIndexingTokenizerProcessError = NewStatusCode(
		"RETRIEVAL_INDEXING_TOKENIZER_PROCESS_ERROR", 155102,
		"retrieval indexing_tokenizer process error, reason: {error_msg}")
	// StatusRetrievalIndexingFileNotFound 检索索引文件未找到
	StatusRetrievalIndexingFileNotFound = NewStatusCode(
		"RETRIEVAL_INDEXING_FILE_NOT_FOUND", 155103,
		"retrieval indexing_file not found, reason: {error_msg}")
	// StatusRetrievalIndexingFormatNotSupport 检索索引格式不支持
	StatusRetrievalIndexingFormatNotSupport = NewStatusCode(
		"RETRIEVAL_INDEXING_FORMAT_NOT_SUPPORT", 155104,
		"retrieval indexing_format is not supported, reason: {error_msg}")
	// StatusRetrievalIndexingEmbedModelNotFound 检索索引 embedding 模型未找到
	StatusRetrievalIndexingEmbedModelNotFound = NewStatusCode(
		"RETRIEVAL_INDEXING_EMBED_MODEL_NOT_FOUND", 155105,
		"retrieval indexing_embed_model not found, reason: {error_msg}")
	// StatusRetrievalIndexingDimensionNotFound 检索索引维度未找到
	StatusRetrievalIndexingDimensionNotFound = NewStatusCode(
		"RETRIEVAL_INDEXING_DIMENSION_NOT_FOUND", 155106,
		"retrieval indexing_dimension not found, reason: {error_msg}")
	// StatusRetrievalIndexingPathNotFound 检索索引路径未找到
	StatusRetrievalIndexingPathNotFound = NewStatusCode(
		"RETRIEVAL_INDEXING_PATH_NOT_FOUND", 155107,
		"retrieval indexing_path not found, reason: {error_msg}")
	// StatusRetrievalIndexingAddDocRuntimeError 检索索引添加文档运行时错误
	StatusRetrievalIndexingAddDocRuntimeError = NewStatusCode(
		"RETRIEVAL_INDEXING_ADD_DOC_RUNTIME_ERROR", 155108,
		"retrieval indexing_add_doc runtime error, reason: {error_msg}")
	// StatusRetrievalIndexingVectorFieldInvalid 检索索引向量字段无效
	StatusRetrievalIndexingVectorFieldInvalid = NewStatusCode(
		"RETRIEVAL_INDEXING_VECTOR_FIELD_INVALID", 155109,
		"retrieval indexing_vector_field is invalid, reason: {error_msg}")
	// StatusRetrievalIndexingFetchError 检索索引获取或解析错误
	StatusRetrievalIndexingFetchError = NewStatusCode(
		"RETRIEVAL_INDEXING_FETCH_ERROR", 155110,
		"retrieval indexing fetch or parse error, reason: {error_msg}")
)

// =============================================================================================================
// 知识库检索 — 检索器 155200–155299
// =============================================================================================================

var (
	// StatusRetrievalRetrieverModeNotSupport 检索 retriever 模式不支持
	StatusRetrievalRetrieverModeNotSupport = NewStatusCode(
		"RETRIEVAL_RETRIEVER_MODE_NOT_SUPPORT", 155200,
		"retrieval retriever_mode is not supported, reason: {error_msg}")
	// StatusRetrievalRetrieverScoreThresholdInvalid 检索 retriever 分数阈值无效
	StatusRetrievalRetrieverScoreThresholdInvalid = NewStatusCode(
		"RETRIEVAL_RETRIEVER_SCORE_THRESHOLD_INVALID", 155201,
		"retrieval retriever_score_threshold is invalid, reason: {error_msg}")
	// StatusRetrievalRetrieverEmbedModelNotFound 检索 retriever embedding 模型未找到
	StatusRetrievalRetrieverEmbedModelNotFound = NewStatusCode(
		"RETRIEVAL_RETRIEVER_EMBED_MODEL_NOT_FOUND", 155202,
		"retrieval retriever_embed_model not found, reason: {error_msg}")
	// StatusRetrievalRetrieverIndexTypeNotSupport 检索 retriever 索引类型不支持
	StatusRetrievalRetrieverIndexTypeNotSupport = NewStatusCode(
		"RETRIEVAL_RETRIEVER_INDEX_TYPE_NOT_SUPPORT", 155203,
		"retrieval retriever_index_type is not supported, reason: {error_msg}")
	// StatusRetrievalRetrieverModeInvalid 检索 retriever 模式无效
	StatusRetrievalRetrieverModeInvalid = NewStatusCode(
		"RETRIEVAL_RETRIEVER_MODE_INVALID", 155204,
		"retrieval retriever_mode is invalid, reason: {error_msg}")
	// StatusRetrievalRetrieverCapabilityNotSupport 检索 retriever 能力不支持
	StatusRetrievalRetrieverCapabilityNotSupport = NewStatusCode(
		"RETRIEVAL_RETRIEVER_CAPABILITY_NOT_SUPPORT", 155205,
		"retrieval retriever_capability is not supported, reason: {error_msg}")
	// StatusRetrievalRetrieverVectorStoreNotFound 检索 retriever 向量存储未找到
	StatusRetrievalRetrieverVectorStoreNotFound = NewStatusCode(
		"RETRIEVAL_RETRIEVER_VECTOR_STORE_NOT_FOUND", 155206,
		"retrieval retriever_vector_store not found, reason: {error_msg}")
	// StatusRetrievalRetrieverCollectionNotFound 检索 retriever 集合未找到
	StatusRetrievalRetrieverCollectionNotFound = NewStatusCode(
		"RETRIEVAL_RETRIEVER_COLLECTION_NOT_FOUND", 155207,
		"retrieval retriever_collection not found, reason: {error_msg}")
	// StatusRetrievalRetrieverNotFound 检索 retriever 未找到
	StatusRetrievalRetrieverNotFound = NewStatusCode(
		"RETRIEVAL_RETRIEVER_NOT_FOUND", 155208,
		"retrieval retriever not found, reason: {error_msg}")
	// StatusRetrievalRetrieverLlmClientNotFound 检索 retriever LLM 客户端未找到
	StatusRetrievalRetrieverLlmClientNotFound = NewStatusCode(
		"RETRIEVAL_RETRIEVER_LLM_CLIENT_NOT_FOUND", 155209,
		"retrieval retriever_llm_client not found, reason: {error_msg}")
	// StatusRetrievalRetrieverTopKInvalid 检索 retriever top_k 无效
	StatusRetrievalRetrieverTopKInvalid = NewStatusCode(
		"RETRIEVAL_RETRIEVER_TOP_K_INVALID", 155210,
		"retrieval retriever_top_k is invalid, reason: {error_msg}")
	// StatusRetrievalRetrieverInvalid 检索 retriever 无效
	StatusRetrievalRetrieverInvalid = NewStatusCode(
		"RETRIEVAL_RETRIEVER_INVALID", 155211,
		"retrieval retriever is invalid, reason: {error_msg}")
)

// =============================================================================================================
// 知识库检索 — 工具 155300–155399
// =============================================================================================================

var (
	// StatusRetrievalUtilsConfigFileNotFound 检索工具配置文件未找到
	StatusRetrievalUtilsConfigFileNotFound = NewStatusCode(
		"RETRIEVAL_UTILS_CONFIG_FILE_NOT_FOUND", 155300,
		"retrieval utils_config_file not found, reason: {error_msg}")
	// StatusRetrievalUtilsPyyamlNotFound 检索工具 pyyaml 未找到
	StatusRetrievalUtilsPyyamlNotFound = NewStatusCode(
		"RETRIEVAL_UTILS_PYYAML_NOT_FOUND", 155301,
		"retrieval utils_pyyaml not found, reason: {error_msg}")
	// StatusRetrievalUtilsConfigFormatNotSupport 检索工具配置格式不支持
	StatusRetrievalUtilsConfigFormatNotSupport = NewStatusCode(
		"RETRIEVAL_UTILS_CONFIG_FORMAT_NOT_SUPPORT", 155302,
		"retrieval utils_config_format is not supported, reason: {error_msg}")
	// StatusRetrievalUtilsConfigNotFound 检索工具配置未找到
	StatusRetrievalUtilsConfigNotFound = NewStatusCode(
		"RETRIEVAL_UTILS_CONFIG_NOT_FOUND", 155303,
		"retrieval utils_config not found, reason: {error_msg}")
	// StatusRetrievalUtilsConfigProcessError 检索工具配置处理错误
	StatusRetrievalUtilsConfigProcessError = NewStatusCode(
		"RETRIEVAL_UTILS_CONFIG_PROCESS_ERROR", 155304,
		"retrieval utils_config process error, reason: {error_msg}")
)

// =============================================================================================================
// 知识库检索 — 向量存储 155400–155499
// =============================================================================================================

var (
	// StatusRetrievalVectorStorePathNotFound 检索向量存储路径未找到
	StatusRetrievalVectorStorePathNotFound = NewStatusCode(
		"RETRIEVAL_VECTOR_STORE_PATH_NOT_FOUND", 155400,
		"retrieval vector_store_path not found, reason: {error_msg}")
	// StatusRetrievalVectorStoreQueryInvalid 检索向量存储查询无效
	StatusRetrievalVectorStoreQueryInvalid = NewStatusCode(
		"RETRIEVAL_VECTOR_STORE_QUERY_INVALID", 155401,
		"retrieval vector_store_query not valid, reason: {error_msg}")
	// StatusRetrievalVectorStoreProviderInvalid 检索向量存储 provider 不支持
	StatusRetrievalVectorStoreProviderInvalid = NewStatusCode(
		"RETRIEVAL_VECTOR_STORE_PROVIDER_INVALID", 155402,
		"retrieval vector_store_provider is not supported, reason: {error_msg}")
)

// =============================================================================================================
// 知识库检索 — 知识库 155500–155599
// =============================================================================================================

var (
	// StatusRetrievalKbParserNotFound 检索知识库解析器未找到
	StatusRetrievalKbParserNotFound = NewStatusCode(
		"RETRIEVAL_KB_PARSER_NOT_FOUND", 155500,
		"retrieval kb_parser not found, reason: {error_msg}")
	// StatusRetrievalKbChunkerNotFound 检索知识库分块器未找到
	StatusRetrievalKbChunkerNotFound = NewStatusCode(
		"RETRIEVAL_KB_CHUNKER_NOT_FOUND", 155501,
		"retrieval kb_chunker not found, reason: {error_msg}")
	// StatusRetrievalKbIndexManagerNotFound 检索知识库索引管理器未找到
	StatusRetrievalKbIndexManagerNotFound = NewStatusCode(
		"RETRIEVAL_KB_INDEX_MANAGER_NOT_FOUND", 155502,
		"retrieval kb_index_manager not found, reason: {error_msg}")
	// StatusRetrievalKbVectorStoreNotFound 检索知识库向量存储未找到
	StatusRetrievalKbVectorStoreNotFound = NewStatusCode(
		"RETRIEVAL_KB_VECTOR_STORE_NOT_FOUND", 155503,
		"retrieval kb_vector_store not found, reason: {error_msg}")
	// StatusRetrievalKbIndexBuildExecutionError 检索知识库索引构建执行错误
	StatusRetrievalKbIndexBuildExecutionError = NewStatusCode(
		"RETRIEVAL_KB_INDEX_BUILD_EXECUTION_ERROR", 155504,
		"retrieval kb_index_build execution error, reason: {error_msg}")
	// StatusRetrievalKbChunkIndexBuildExecutionError 检索知识库分块索引构建执行错误
	StatusRetrievalKbChunkIndexBuildExecutionError = NewStatusCode(
		"RETRIEVAL_KB_CHUNK_INDEX_BUILD_EXECUTION_ERROR", 155505,
		"retrieval kb_chunk_index_build execution error, reason: {error_msg}")
	// StatusRetrievalKbTripleIndexBuildExecutionError 检索知识库三元组索引构建执行错误
	StatusRetrievalKbTripleIndexBuildExecutionError = NewStatusCode(
		"RETRIEVAL_KB_TRIPLE_INDEX_BUILD_EXECUTION_ERROR", 155506,
		"retrieval kb_triple_index_build execution error, reason: {error_msg}")
	// StatusRetrievalKbTripleExtractionProcessError 检索知识库三元组抽取处理错误
	StatusRetrievalKbTripleExtractionProcessError = NewStatusCode(
		"RETRIEVAL_KB_TRIPLE_EXTRACTION_PROCESS_ERROR", 155507,
		"retrieval kb_triple_extraction process error, reason: {error_msg}")
	// StatusRetrievalKbDatabaseConfigInvalid 检索知识库数据库配置无效
	StatusRetrievalKbDatabaseConfigInvalid = NewStatusCode(
		"RETRIEVAL_KB_DATABASE_CONFIG_INVALID", 155508,
		"retrieval kb_database_config is invalid, reason: {error_msg}")
)

// =============================================================================================================
// 知识库检索 — 重排序 155600–155699
// =============================================================================================================

var (
	// StatusRetrievalRerankerRequestCallFailed 检索重排序请求调用失败
	StatusRetrievalRerankerRequestCallFailed = NewStatusCode(
		"RETRIEVAL_RERANKER_REQUEST_CALL_FAILED", 155600,
		"retrieval reranker_request call failed, reason: {error_msg}")
	// StatusRetrievalRerankerUnreachableCallFailed 检索重排序不可达调用失败
	StatusRetrievalRerankerUnreachableCallFailed = NewStatusCode(
		"RETRIEVAL_RERANKER_UNREACHABLE_CALL_FAILED", 155601,
		"retrieval reranker call failed, reason: {error_msg}")
	// StatusRetrievalRerankerInputInvalid 检索重排序输入无效
	StatusRetrievalRerankerInputInvalid = NewStatusCode(
		"RETRIEVAL_RERANKER_INPUT_INVALID", 155602,
		"retrieval reranker_input is invalid, reason: {error_msg}")
)

// =============================================================================================================
// 知识库检索 — 查询改写 155603–155609
// =============================================================================================================

var (
	// StatusRetrievalQueryRewriterPromptNotFound 检索查询重写器提示词文件未找到
	StatusRetrievalQueryRewriterPromptNotFound = NewStatusCode(
		"RETRIEVAL_QUERY_REWRITER_PROMPT_NOT_FOUND", 155603,
		"retrieval query_rewriter prompt file not found, reason: {error_msg}")
	// StatusRetrievalQueryRewriterOutputInvalid 检索查询重写器 LLM 输出无效 JSON
	StatusRetrievalQueryRewriterOutputInvalid = NewStatusCode(
		"RETRIEVAL_QUERY_REWRITER_OUTPUT_INVALID", 155604,
		"retrieval query_rewriter llm output is not valid JSON, reason: {error_msg}")
	// StatusRetrievalQueryRewriterLlmInvokeFailed 检索查询重写器 LLM 调用失败
	StatusRetrievalQueryRewriterLlmInvokeFailed = NewStatusCode(
		"RETRIEVAL_QUERY_REWRITER_LLM_INVOKE_FAILED", 155605,
		"retrieval query_rewriter llm invoke failed, reason: {error_msg}")
	// StatusRetrievalQueryRewriterInputInvalid 检索查询重写器输入无效
	StatusRetrievalQueryRewriterInputInvalid = NewStatusCode(
		"RETRIEVAL_QUERY_REWRITER_INPUT_INVALID", 155606,
		"retrieval query_rewriter input is invalid, reason: {error_msg}")
)

// =============================================================================================================
// 记忆引擎 158000–159999
// =============================================================================================================

var (
	// StatusMemoryRegisterStoreExecutionError 注册存储到记忆引擎失败
	StatusMemoryRegisterStoreExecutionError = NewStatusCode(
		"MEMORY_REGISTER_STORE_EXECUTION_ERROR", 158000,
		"failed to register {store_type} to memory engine, reason: {error_msg}")
	// StatusMemorySetConfigExecutionError 设置记忆配置失败
	StatusMemorySetConfigExecutionError = NewStatusCode(
		"MEMORY_SET_CONFIG_EXECUTION_ERROR", 158001,
		"failed to set {config_type} config, reason: {error_msg}")
	// StatusMemoryAddMemoryExecutionError 添加记忆失败
	StatusMemoryAddMemoryExecutionError = NewStatusCode(
		"MEMORY_ADD_MEMORY_EXECUTION_ERROR", 158002,
		"failed to add {memory_type} memory, reason: {error_msg}")
	// StatusMemoryDeleteMemoryExecutionError 删除记忆失败
	StatusMemoryDeleteMemoryExecutionError = NewStatusCode(
		"MEMORY_DELETE_MEMORY_EXECUTION_ERROR", 158003,
		"failed to delete {memory_type} memory, reason: {error_msg}")
	// StatusMemoryUpdateMemoryExecutionError 更新记忆失败
	StatusMemoryUpdateMemoryExecutionError = NewStatusCode(
		"MEMORY_UPDATE_MEMORY_EXECUTION_ERROR", 158004,
		"failed to update {memory_type} memory, reason: {error_msg}")
	// StatusMemoryGetMemoryExecutionError 获取记忆失败
	StatusMemoryGetMemoryExecutionError = NewStatusCode(
		"MEMORY_GET_MEMORY_EXECUTION_ERROR", 158005,
		"failed to get {memory_type} memory, reason: {error_msg}")
	// StatusMemoryStoreInitFailed 记忆存储初始化失败
	StatusMemoryStoreInitFailed = NewStatusCode(
		"MEMORY_STORE_INIT_FAILED", 158006,
		"failed to init {store_type}, reason: {error_msg}")
	// StatusMemoryConnectStoreExecutionError 连接记忆存储失败
	StatusMemoryConnectStoreExecutionError = NewStatusCode(
		"MEMORY_CONNECT_STORE_EXECUTION_ERROR", 158007,
		"failed to connect {store_type}, reason: {error_msg}")
	// StatusMemoryStoreValidationInvalid 记忆存储校验失败
	StatusMemoryStoreValidationInvalid = NewStatusCode(
		"MEMORY_STORE_VALIDATION_INVALID", 158008,
		"{store_type} validation failed, reason: {error_msg}")
	// StatusMemoryRegisterOperationValidationInvalid 注册记忆操作校验失败
	StatusMemoryRegisterOperationValidationInvalid = NewStatusCode(
		"MEMORY_REGISTER_OPERATION_VALIDATION_INVALID", 158009,
		"failed to register operation for {entity_key}:{schema_version}, reason: {error_msg}")
	// StatusMemoryMigrateMemoryExecutionError 迁移记忆失败
	StatusMemoryMigrateMemoryExecutionError = NewStatusCode(
		"MEMORY_MIGRATE_MEMORY_EXECUTION_ERROR", 158010,
		"failed to migrate memory, reason: {error_msg}")
	// StatusMemoryBackupNotFound 备份不存在
	StatusMemoryBackupNotFound = NewStatusCode(
		"MEMORY_BACKUP_NOT_FOUND", 158011,
		"backup not found, backup_id: {backup_id}")
)

// =============================================================================================================
// 记忆引擎 — 通用工具 158200–158299
// =============================================================================================================

var (
	// StatusMemoryGraphLanguageInvalid 图记忆语言无效
	StatusMemoryGraphLanguageInvalid = NewStatusCode(
		"MEMORY_GRAPH_LANGUAGE_INVALID", 158200,
		"graph memory language invalid: {error_msg}")
	// StatusMemoryGraphEmbeddingCallFailed 图记忆 embedding 调用失败
	StatusMemoryGraphEmbeddingCallFailed = NewStatusCode(
		"MEMORY_GRAPH_EMBEDDING_CALL_FAILED", 158201,
		"graph memory embedding call failed, reason: {error_msg}")
	// StatusMemoryGraphEmbedModelNotFound 图记忆 embedding 模型未配置
	StatusMemoryGraphEmbedModelNotFound = NewStatusCode(
		"MEMORY_GRAPH_EMBED_MODEL_NOT_FOUND", 158202,
		"graph memory embedder not configured: {error_msg}")
	// StatusMemoryGraphInvokeLlmFailed 图记忆 LLM 调用失败
	StatusMemoryGraphInvokeLlmFailed = NewStatusCode(
		"MEMORY_GRAPH_INVOKE_LLM_FAILED", 158203,
		"graph memory LLM invoke failed, reason: {error_msg}")
	// StatusMemoryGraphPromptFilesMissing 图记忆提示词文件缺失
	StatusMemoryGraphPromptFilesMissing = NewStatusCode(
		"MEMORY_GRAPH_PROMPT_FILES_MISSING", 158204,
		"graph memory prompt files not found in directory {prompt_dir}")
)
