package e2a

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// 来源协议（对应 Python E2A_SOURCE_PROTOCOL_*）
const (
	// E2ASourceProtocolE2A 原生 E2A 协议
	E2ASourceProtocolE2A = "e2a"
	// E2ASourceProtocolACP ACP 协议
	E2ASourceProtocolACP = "acp"
	// E2ASourceProtocolA2A A2A 协议
	E2ASourceProtocolA2A = "a2a"
)

// 响应状态（对应 Python E2A_RESPONSE_STATUS_*）
const (
	// E2AResponseStatusSucceeded 成功
	E2AResponseStatusSucceeded = "succeeded"
	// E2AResponseStatusFailed 失败
	E2AResponseStatusFailed = "failed"
	// E2AResponseStatusInProgress 进行中
	E2AResponseStatusInProgress = "in_progress"
)

// 响应类型（对应 Python E2A_RESPONSE_KIND_*，共 12 种）
const (
	// E2AResponseKindE2AComplete E2A 完整响应
	E2AResponseKindE2AComplete = "e2a.complete"
	// E2AResponseKindE2AChunk E2A 流式块
	E2AResponseKindE2AChunk = "e2a.chunk"
	// E2AResponseKindE2AError E2A 错误
	E2AResponseKindE2AError = "e2a.error"
	// E2AResponseKindACPSessionUpdate ACP 会话更新
	E2AResponseKindACPSessionUpdate = "acp.session_update"
	// E2AResponseKindACPPromptResult ACP 提示结果
	E2AResponseKindACPPromptResult = "acp.prompt_result"
	// E2AResponseKindACPJSONRPCError ACP JSON-RPC 错误
	E2AResponseKindACPJSONRPCError = "acp.jsonrpc_error"
	// E2AResponseKindACPOutputRequest ACP 输出请求
	E2AResponseKindACPOutputRequest = "acp.output_request"
	// E2AResponseKindA2ATask A2A 任务
	E2AResponseKindA2ATask = "a2a.task"
	// E2AResponseKindA2AMessage A2A 消息
	E2AResponseKindA2AMessage = "a2a.message"
	// E2AResponseKindA2AStreamEvent A2A 流事件
	E2AResponseKindA2AStreamEvent = "a2a.stream_event"
	// E2AResponseKindCron 定时任务
	E2AResponseKindCron = "cron"
	// E2AResponseKindExt 扩展
	E2AResponseKindExt = "ext"
)

// Wire 内部键（对应 Python E2A_WIRE_*）
const (
	// E2AWireLegacyAgentResponseKey 编码失败时旧 AgentResponse 写入 metadata 的键
	E2AWireLegacyAgentResponseKey = "_e2a_wire_legacy_agent_response"
	// E2AWireLegacyAgentChunkKey 编码失败时旧 AgentChunk 写入 metadata 的键
	E2AWireLegacyAgentChunkKey = "_e2a_wire_legacy_agent_chunk"
	// E2AWireServerPushKey 服务端推送标识键
	E2AWireServerPushKey = "_jiuwenswarm_server_push"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// E2AResponseKinds 运行时完整响应类型列表（12 种）
	E2AResponseKinds = []string{
		E2AResponseKindE2AComplete,
		E2AResponseKindE2AChunk,
		E2AResponseKindE2AError,
		E2AResponseKindACPSessionUpdate,
		E2AResponseKindACPPromptResult,
		E2AResponseKindACPJSONRPCError,
		E2AResponseKindACPOutputRequest,
		E2AResponseKindA2ATask,
		E2AResponseKindA2AMessage,
		E2AResponseKindA2AStreamEvent,
		E2AResponseKindCron,
		E2AResponseKindExt,
	}

	// E2AA2AStreamBranches A2A 流分支（4 种）
	E2AA2AStreamBranches = []string{
		"task",
		"message",
		"status_update",
		"artifact_update",
	}

	// ACPClientToAgentMethods 客户端→Agent JSON-RPC 方法（13 个）
	ACPClientToAgentMethods = []string{
		"initialize",
		"authenticate",
		"session/new",
		"session/load",
		"session/list",
		"session/set_mode",
		"session/set_config_option",
		"session/prompt",
		"session/set_model",
		"session/fork",
		"session/resume",
		"session/close",
		"logout",
	}

	// ACPAgentToClientMethods Agent→客户端方法（10 个）
	ACPAgentToClientMethods = []string{
		"session/update",
		"session/request_permission",
		"fs/read_text_file",
		"fs/write_text_file",
		"terminal/create",
		"terminal/output",
		"terminal/release",
		"terminal/wait_for_exit",
		"terminal/kill",
		"session/elicitation",
	}

	// ACPNotificationNames ACP 通知名（3 个）
	ACPNotificationNames = []string{
		"session/cancel",
		"session/update",
		"session/elicitation/complete",
	}

	// ACPSessionUpdateKinds 会话更新类型（12 个）
	ACPSessionUpdateKinds = []string{
		"user_message_chunk",
		"agent_message_chunk",
		"agent_thought_chunk",
		"tool_call",
		"tool_call_update",
		"todo_update",
		"plan",
		"available_commands_update",
		"current_mode_update",
		"config_option_update",
		"session_info_update",
		"usage_update",
	}

	// E2AWireInternalMetadataKeys Wire 内部元数据键集合
	E2AWireInternalMetadataKeys = map[string]struct{}{
		E2AWireServerPushKey:          {},
		E2AWireLegacyAgentChunkKey:    {},
		E2AWireLegacyAgentResponseKey: {},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
