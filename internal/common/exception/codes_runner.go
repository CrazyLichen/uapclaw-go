package exception

// ──────────────────────────── 全局变量 ────────────────────────────

// =============================================================================================================
// 运行器执行 110000–110099
// =============================================================================================================

var (
	// StatusRunnerTerminationError Runner 已终止
	StatusRunnerTerminationError = NewStatusCode(
		"RUNNER_TERMINATION_ERROR", 110002,
		"runner is already terminate")
	// StatusRunnerRunAgentError Runner 运行 agent 失败
	StatusRunnerRunAgentError = NewStatusCode(
		"RUNNER_RUN_AGENT_ERROR", 110022,
		"runner run agent '{agent}' failed, error='{reason}'")
)

// =============================================================================================================
// 分布式执行 110100–110199
// =============================================================================================================

var (
	// StatusRemoteAgentExecutionTimeout 远程 agent 执行超时
	StatusRemoteAgentExecutionTimeout = NewStatusCode(
		"REMOTE_AGENT_EXECUTION_TIMEOUT", 110100,
		"remote agent '{agent_id}' execute exceed {timeout} seconds")
	// StatusRemoteAgentExecutionError 远程 agent 执行错误
	StatusRemoteAgentExecutionError = NewStatusCode(
		"REMOTE_AGENT_EXECUTION_ERROR", 110101,
		"remote agent '{agent_id}' execute error, error='{reason}'")
	// StatusRemoteAgentResponseProcessError 远程 agent 响应处理错误
	StatusRemoteAgentResponseProcessError = NewStatusCode(
		"REMOTE_AGENT_RESPONSE_PROCESS_ERROR", 110102,
		"remote agent request process error, message_id='{message_id}', process_id='{process_id}', response='{code={error_code}', msg='{error_msg}'")
)

// =============================================================================================================
// 消息队列 110200–110299
// =============================================================================================================

var (
	// StatusMessageQueueInitiationError 消息队列初始化错误
	StatusMessageQueueInitiationError = NewStatusCode(
		"MESSAGE_QUEUE_INITIATION_ERROR", 110200,
		"init type '{type}' message queue error, error='{reason}'")
	// StatusMessageQueueTopicSubscriptionError 消息队列主题订阅错误
	StatusMessageQueueTopicSubscriptionError = NewStatusCode(
		"MESSAGE_QUEUE_TOPIC_SUBSCRIPTION_ERROR", 110210,
		"subscribe topic error, topic='{topic}', error='{reason}'")
	// StatusMessageQueueTopicMessageProductionError 消息队列主题消息生产错误
	StatusMessageQueueTopicMessageProductionError = NewStatusCode(
		"MESSAGE_QUEUE_TOPIC_MESSAGE_PRODUCTION_ERROR", 110211,
		"produce message error, topic='{topic}', message='{message}', error='{reason}'")
	// StatusMessageQueueMessageConsumeError 消息队列消息消费错误
	StatusMessageQueueMessageConsumeError = NewStatusCode(
		"MESSAGE_QUEUE_MESSAGE_CONSUME_ERROR", 110212,
		"consume message error, error='{reason}'")
	// StatusMessageQueueMessageProcessExecutionError 消息队列消息处理执行错误
	StatusMessageQueueMessageProcessExecutionError = NewStatusCode(
		"MESSAGE_QUEUE_MESSAGE_PROCESS_EXECUTION_ERROR", 110213,
		"process message error, error='{reason}'")
)

// =============================================================================================================
// 分布式消息队列 110300–110399
// =============================================================================================================

var (
	// StatusDistMessageQueueClientStartError 分布式消息队列客户端启动错误
	StatusDistMessageQueueClientStartError = NewStatusCode(
		"DIST_MESSAGE_QUEUE_CLIENT_START_ERROR", 110300,
		"distribute message queue client start error, error='{reason}'")
)

// =============================================================================================================
// 资源管理器 110400–110599
// =============================================================================================================

var (
	// StatusResourceIDValueInvalid 资源 ID 无效
	StatusResourceIDValueInvalid = NewStatusCode(
		"RESOURCE_ID_VALUE_INVALID", 110400,
		"{resource_type} id is invalid, reason='{reason}'")
	// StatusResourceTagValueInvalid 资源标签无效
	StatusResourceTagValueInvalid = NewStatusCode(
		"RESOURCE_TAG_VALUE_INVALID", 110401,
		"tag is invalid, tag={tag}, reason='{reason}'")
	// StatusResourceCardValueInvalid 资源卡片无效
	StatusResourceCardValueInvalid = NewStatusCode(
		"RESOURCE_CARD_VALUE_INVALID", 110402,
		"{resource_type} card is invalid, reason='{reason}'")
	// StatusResourceProviderInvalid 资源 provider 无效
	StatusResourceProviderInvalid = NewStatusCode(
		"RESOURCE_PROVIDER_INVALID", 110403,
		"{resource_type} provider is invalid, reason='{reason}'")
	// StatusResourceValueInvalid 资源值无效
	StatusResourceValueInvalid = NewStatusCode(
		"RESOURCE_VALUE_INVALID", 110404,
		"{resource_type} value is invalid, reason='{reason}'")
	// StatusResourceAddError 资源添加失败
	StatusResourceAddError = NewStatusCode(
		"RESOURCE_ADD_ERROR", 110430,
		"resource add failed, card='{card}', error='{reason}'")
	// StatusResourceGetError 资源获取失败
	StatusResourceGetError = NewStatusCode(
		"RESOURCE_GET_ERROR", 110431,
		"resource get failed, resource_id='{resource_id}', resource_type='{resource_type}', error='{reason}'")
)

// 标签管理器 110480–110499

var (
	// StatusResourceTagRemoveTagError 标签无效导致移除失败
	StatusResourceTagRemoveTagError = NewStatusCode(
		"RESOURCE_TAG_REMOVE_TAG_ERROR", 110480,
		"tag is invalid, tag='{tag}', error='{reason}'")
	// StatusResourceTagAddResourceTagError 添加资源标签失败
	StatusResourceTagAddResourceTagError = NewStatusCode(
		"RESOURCE_TAG_ADD_RESOURCE_TAG_ERROR", 110481,
		"add tag failed, resource_id='{resource_id}', tag='{tag}', error='{reason}'")
	// StatusResourceTagRemoveResourceTagError 移除资源标签失败
	StatusResourceTagRemoveResourceTagError = NewStatusCode(
		"RESOURCE_TAG_REMOVE_RESOURCE_TAG_ERROR", 110482,
		"remove resource tag failed, resource_id='{resource_id}', tags='{tags}', error='{reason}'")
	// StatusResourceTagReplaceResourceTagError 替换资源标签失败
	StatusResourceTagReplaceResourceTagError = NewStatusCode(
		"RESOURCE_TAG_REPLACE_RESOURCE_TAG_ERROR", 110483,
		"replace resource tag failed, resource_id='{resource_id}', tags='{tags}', error='{reason}'")
	// StatusResourceTagFindResourceError 查找资源标签失败
	StatusResourceTagFindResourceError = NewStatusCode(
		"RESOURCE_TAG_FIND_RESOURCE_ERROR", 110484,
		"replace resource tag failed, resource_id='{resource_id}', tags='{tags}', error='{reason}'")
)

// MCP 资源 110510–110519

var (
	// StatusResourceMCPServerParamInvalid MCP 服务器参数无效
	StatusResourceMCPServerParamInvalid = NewStatusCode(
		"RESOURCE_MCP_SERVER_PARAM_INVALID", 110510,
		"server param is invalid, server_config='{server_config}', error='{reason}'")
	// StatusResourceMCPServerConnectionError MCP 服务器连接失败
	StatusResourceMCPServerConnectionError = NewStatusCode(
		"RESOURCE_MCP_SERVER_CONNECTION_ERROR", 110511,
		"mcp server connect failed, server_config={server_config}, error='{reason}'")
	// StatusResourceMCPServerAddError MCP 服务器添加失败
	StatusResourceMCPServerAddError = NewStatusCode(
		"RESOURCE_MCP_SERVER_ADD_ERROR", 110512,
		"mcp server add failed, server_config={server_config}, error='{reason}'")
	// StatusResourceMCPServerRefreshError MCP 服务器刷新失败
	StatusResourceMCPServerRefreshError = NewStatusCode(
		"RESOURCE_MCP_SERVER_REFRESH_ERROR", 110513,
		"mcp server refresh failed, server_id={server_id}, error='{reason}'")
	// StatusResourceMCPServerRemoveError MCP 服务器移除失败
	StatusResourceMCPServerRemoveError = NewStatusCode(
		"RESOURCE_MCP_SERVER_REMOVE_ERROR", 110514,
		"mcp server remove failed, server_id={server_id}, error='{reason}'")
	// StatusResourceMCPToolGetError MCP 服务器工具获取失败
	StatusResourceMCPToolGetError = NewStatusCode(
		"RESOURCE_MCP_TOOL_GET_ERROR", 110515,
		"mcp server tool get failed, server_id={server_id}, error='{reason}'")
)

// =============================================================================================================
// 回调框架 110600–110699
// =============================================================================================================

var (
	// StatusCallbackExecutionAborted 回调执行被中止
	StatusCallbackExecutionAborted = NewStatusCode(
		"CALLBACK_EXECUTION_ABORTED", 110600,
		"callback execution aborted: {reason}")
)
