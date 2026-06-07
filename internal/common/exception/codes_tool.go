package exception

// ──────────────────────────── 全局变量 ────────────────────────────

// =============================================================================================================
// Tool — Basic 182000–182099
// =============================================================================================================

var (
	// StatusToolCardInvalid 工具卡片无效
	StatusToolCardInvalid = NewStatusCode(
		"TOOL_CARD_INVALID", 182000,
		"card is invalid, card={card}, error='{reason}'")
	// StatusToolStreamNotSupported 工具流式调用不支持
	StatusToolStreamNotSupported = NewStatusCode(
		"TOOL_STREAM_NOT_SUPPORTED", 182010,
		"stream is not support, card={card}")
	// StatusToolInvokeNotSupported 工具 invoke 调用不支持
	StatusToolInvokeNotSupported = NewStatusCode(
		"TOOL_INVOKE_NOT_SUPPORTED", 182011,
		"invoke is not support, card={card}")
	// StatusToolExecutionError 工具执行错误
	StatusToolExecutionError = NewStatusCode(
		"TOOL_EXECUTION_ERROR", 182012,
		"tool execution error, too card={card}, reason={reason}")
)

// =============================================================================================================
// Tool — Restful API 182100–182199
// =============================================================================================================

var (
	// StatusToolRestfulApiCardConfigInvalid RestfulAPI 工具卡片配置无效
	StatusToolRestfulApiCardConfigInvalid = NewStatusCode(
		"TOOL_RESTFUL_API_CARD_CONFIG_INVALID", 182100,
		"config failed, {reason}")
	// StatusToolRestfulApiExecutionTimeout RestfulAPI 工具执行超时
	StatusToolRestfulApiExecutionTimeout = NewStatusCode(
		"TOOL_RESTFUL_API_EXECUTION_TIMEOUT", 182101,
		"execute {method} failed, request is timeout, timeout={timeout}s, card=[{card}]")
	// StatusToolRestfulApiResponseSizeExceedLimit RestfulAPI 工具响应体超过大小限制
	StatusToolRestfulApiResponseSizeExceedLimit = NewStatusCode(
		"TOOL_RESTFUL_API_RESPONSE_SIZE_EXCEED_LIMIT", 182102,
		"execute {method} failed, response is too big, max_size={max_length}b, actual={actual_length}b, card=[{card}]")
	// StatusToolRestfulApiResponseError RestfulAPI 工具响应错误
	StatusToolRestfulApiResponseError = NewStatusCode(
		"TOOL_RESTFUL_API_RESPONSE_ERROR", 182103,
		"execute {method} failed, response error, code={code}, error='{reason}'")
	// StatusToolRestfulApiExecutionError RestfulAPI 工具执行错误
	StatusToolRestfulApiExecutionError = NewStatusCode(
		"TOOL_RESTFUL_API_EXECUTION_ERROR", 182104,
		"RestfulApi execute {method} failed, error='{reason}', card=[{card}]")
	// StatusToolRestfulApiResponseProcessError RestfulAPI 工具响应解析错误
	StatusToolRestfulApiResponseProcessError = NewStatusCode(
		"TOOL_RESTFUL_API_RESPONSE_PROCESS_ERROR", 182105,
		"RestfulApi parse response failed, error='{reason}', card=[{card}]")
)

// =============================================================================================================
// Tool — Local Function 182200–182299
// =============================================================================================================

var (
	// StatusToolLocalFunctionFuncNotSupported 本地函数不支持
	StatusToolLocalFunctionFuncNotSupported = NewStatusCode(
		"TOOL_LOCAL_FUNCTION_FUNC_NOT_SUPPORTED", 182200,
		"func is not supported, card={card}")
	// StatusToolLocalFunctionExecutionError 本地函数执行错误
	StatusToolLocalFunctionExecutionError = NewStatusCode(
		"TOOL_LOCAL_FUNCTION_EXECUTION_ERROR", 182205,
		"execute {method} failed, error='{reason}', card={card}")
)

// =============================================================================================================
// Tool — MCP 182300–182399
// =============================================================================================================

var (
	// StatusToolMcpClientNotSupported MCP 客户端不支持
	StatusToolMcpClientNotSupported = NewStatusCode(
		"TOOL_MCP_CLIENT_NOT_SUPPORTED", 182300,
		"mcp client is not supported, card={card}")
	// StatusToolMcpExecutionError MCP 工具执行错误
	StatusToolMcpExecutionError = NewStatusCode(
		"TOOL_MCP_EXECUTION_ERROR", 182301,
		"execute {method} failed, error='{reason}', card={card}")
	// StatusToolMcpClientTypeUnknown MCP 客户端类型未知
	StatusToolMcpClientTypeUnknown = NewStatusCode(
		"TOOL_MCP_CLIENT_TYPE_UNKNOWN", 182302,
		"mcp client type is unknown, client_type={client_type}")
	// StatusToolMcpNotConnected MCP 客户端未连接
	StatusToolMcpNotConnected = NewStatusCode(
		"TOOL_MCP_NOT_CONNECTED", 182303,
		"mcp client is not connected, server_name={server_name}")
)

// =============================================================================================================
// Tool — OpenAPI 182400–182499
// =============================================================================================================

var (
	// StatusToolOpenapiClientExecutionError OpenAPI 客户端执行错误
	StatusToolOpenapiClientExecutionError = NewStatusCode(
		"TOOL_OPENAPI_CLIENT_EXECUTION_ERROR", 182400,
		"openapi client execute error, error='{reason}'")
)

// =============================================================================================================
// Tool — Harness 182500–182699
// =============================================================================================================

var (
	// StatusToolTodosLoadFailed todo 工具加载失败
	StatusToolTodosLoadFailed = NewStatusCode(
		"TOOL_TODOS_LOAD_FAILED", 182500,
		"todo tool loads failed, error='{reason}'")
	// StatusToolTodosSaveFailed todo 工具保存失败
	StatusToolTodosSaveFailed = NewStatusCode(
		"TOOL_TODOS_SAVE_FAILED", 182501,
		"todo tool saves failed, error='{reason}'")
	// StatusToolTodosClearFailed todo 工具清除失败
	StatusToolTodosClearFailed = NewStatusCode(
		"TOOL_TODOS_CLEAR_FAILED", 182502,
		"todo tool clears failed, error='{reason}'")
	// StatusToolTodosValidationInvalid todo 工具校验无效
	StatusToolTodosValidationInvalid = NewStatusCode(
		"TOOL_TODOS_VALIDATION_INVALID", 182503,
		"todo tool validation invalid, error='{reason}'")
	// StatusToolTodosInvokeFailed todo 工具调用失败
	StatusToolTodosInvokeFailed = NewStatusCode(
		"TOOL_TODOS_INVOKE_FAILED", 182504,
		"todo tool invoke failed, error='{reason}'")
	// StatusToolTaskToolInvoked task 工具调用失败
	StatusToolTaskToolInvoked = NewStatusCode(
		"TOOL_TASK_TOOL_INVOKED", 182505,
		"task tool invoked failed, error='{reason}'")
	// StatusToolWebSearchEngineError web 搜索引擎错误
	StatusToolWebSearchEngineError = NewStatusCode(
		"TOOL_WEB_SEARCH_ENGINE_ERROR", 182506,
		"web search engine error, engine='{engine}', reason='{reason}'")
	// StatusToolWebSearchAllEnginesFailed 所有 web 搜索引擎均失败
	StatusToolWebSearchAllEnginesFailed = NewStatusCode(
		"TOOL_WEB_SEARCH_ALL_ENGINES_FAILED", 182507,
		"all web search engines failed, errors='{errors}'")
	// StatusToolWebApiKeyNotSet web 工具 API key 未设置
	StatusToolWebApiKeyNotSet = NewStatusCode(
		"TOOL_WEB_API_KEY_NOT_SET", 182508,
		"web tool api key is not set, key_name='{key_name}'")
	// StatusToolSessionToolInvoked session 工具调用失败
	StatusToolSessionToolInvoked = NewStatusCode(
		"TOOL_SESSION_TOOL_INVOKED", 182509,
		"session tool invoked failed, error='{reason}'")
)

// =============================================================================================================
// Tool — Worktree 182510–182519
// =============================================================================================================

var (
	// StatusToolWorktreeExitInvalid worktree 退出无效
	StatusToolWorktreeExitInvalid = NewStatusCode(
		"TOOL_WORKTREE_EXIT_INVALID", 182510,
		"worktree exit is invalid, reason='{reason}'")
)
