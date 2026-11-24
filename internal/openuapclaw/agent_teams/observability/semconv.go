package observability

// ──────────────────────────── 常量 ────────────────────────────

// OpenLLMetry / GenAI 标准属性
// Python: GEN_AI_* 常量
const (
	// GenAISystem gen_ai.system 属性键
	GenAISystem = "gen_ai.system"
	// GenAIRequestModel gen_ai.request.model 属性键
	GenAIRequestModel = "gen_ai.request.model"
	// GenAIRequestTemperature gen_ai.request.temperature 属性键
	GenAIRequestTemperature = "gen_ai.request.temperature"
	// GenAIRequestTopP gen_ai.request.top_p 属性键
	GenAIRequestTopP = "gen_ai.request.top_p"
	// GenAIRequestMaxTokens gen_ai.request.max_tokens 属性键
	GenAIRequestMaxTokens = "gen_ai.request.max_tokens"
	// GenAIPrompt gen_ai.prompt 属性键
	GenAIPrompt = "gen_ai.prompt"
	// GenAICompletion gen_ai.completion 属性键
	GenAICompletion = "gen_ai.completion"
	// GenAIPromptAttachmentKind 附件消息条目类型标注（openjiuwen.kind）后缀
	GenAIPromptAttachmentKind = "openjiuwen.kind"
	// GenAIPromptAttachmentMode 附件消息交付模式标注（openjiuwen.mode）后缀
	GenAIPromptAttachmentMode = "openjiuwen.mode"
	// PromptAttachmentHistoryKind 附件历史条目类型值
	PromptAttachmentHistoryKind = "prompt_attachment_history"
	// GenAIUsagePromptTokens gen_ai.usage.prompt_tokens 属性键
	GenAIUsagePromptTokens = "gen_ai.usage.prompt_tokens"
	// GenAIUsageCompletionTokens gen_ai.usage.completion_tokens 属性键
	GenAIUsageCompletionTokens = "gen_ai.usage.completion_tokens"
	// GenAIUsageTotalTokens gen_ai.usage.total_tokens 属性键
	GenAIUsageTotalTokens = "gen_ai.usage.total_tokens"
	// GenAIResponseFinishReason gen_ai.response.finish_reason 属性键
	GenAIResponseFinishReason = "gen_ai.response.finish_reason"
	// GenAIResponseModel gen_ai.response.model 属性键
	GenAIResponseModel = "gen_ai.response.model"
	// GenAIResponseTTFTMs gen_ai.response.time_to_first_token_ms 属性键
	GenAIResponseTTFTMs = "gen_ai.response.time_to_first_token_ms"
	// GenAIToolName gen_ai.tool.name 属性键
	GenAIToolName = "gen_ai.tool.name"
	// GenAIToolInput gen_ai.tool.input 属性键
	GenAIToolInput = "gen_ai.tool.input"
	// GenAIToolOutput gen_ai.tool.output 属性键
	GenAIToolOutput = "gen_ai.tool.output"
	// GenAIToolID gen_ai.tool.id 属性键
	GenAIToolID = "gen_ai.tool.id"
)

// agentteam.* — Team 协作属性（监控处理器）
// Python: AT_* 常量
const (
	// ATTeamName agentteam.team.name 属性键
	ATTeamName = "agentteam.team.name"
	// ATTeamDisplayName agentteam.team.display_name 属性键
	ATTeamDisplayName = "agentteam.team.display_name"
	// ATEventType agentteam.event_type 属性键
	ATEventType = "agentteam.event_type"
	// ATAgentID agentteam.agent.id 属性键
	ATAgentID = "agentteam.agent.id"
	// ATAgentRole agentteam.agent.role 属性键
	ATAgentRole = "agentteam.agent.role"
	// ATAgentInput agentteam.agent.input 属性键
	ATAgentInput = "agentteam.agent.input"
	// ATAgentOutput agentteam.agent.output 属性键
	ATAgentOutput = "agentteam.agent.output"
	// ATMemberName agentteam.member.name 属性键
	ATMemberName = "agentteam.member.name"
	// ATMemberStatusOld agentteam.member.status.old 属性键
	ATMemberStatusOld = "agentteam.member.status.old"
	// ATMemberStatusNew agentteam.member.status.new 属性键
	ATMemberStatusNew = "agentteam.member.status.new"
	// ATMemberRestartReason agentteam.member.restart_reason 属性键
	ATMemberRestartReason = "agentteam.member.restart_reason"
	// ATMemberRestartCount agentteam.member.restart_count 属性键
	ATMemberRestartCount = "agentteam.member.restart_count"
	// ATMemberShutdownForce agentteam.member.shutdown_force 属性键
	ATMemberShutdownForce = "agentteam.member.shutdown_force"
	// ATMessageID agentteam.message.id 属性键
	ATMessageID = "agentteam.message.id"
	// ATMessageFrom agentteam.message.from 属性键
	ATMessageFrom = "agentteam.message.from"
	// ATMessageTo agentteam.message.to 属性键
	ATMessageTo = "agentteam.message.to"
	// ATMessageBroadcast agentteam.message.broadcast 属性键
	ATMessageBroadcast = "agentteam.message.broadcast"
	// ATTaskID agentteam.task.id 属性键
	ATTaskID = "agentteam.task.id"
	// ATTaskStatus agentteam.task.status 属性键
	ATTaskStatus = "agentteam.task.status"
	// ATTaskAssignee agentteam.task.assignee 属性键
	ATTaskAssignee = "agentteam.task.assignee"
	// ATTeamLeader agentteam.team.leader 属性键
	ATTeamLeader = "agentteam.team.leader"
)

// deepagent.* — DeepAgent task-loop 属性（Rail）
// Python: DA_* 常量
const (
	// DATaskIteration deepagent.task.iteration 属性键
	DATaskIteration = "deepagent.task.iteration"
	// DATaskIsFollowUp deepagent.task.is_follow_up 属性键
	DATaskIsFollowUp = "deepagent.task.is_follow_up"
)

// ──────────────────────────── 轨迹 schema v2 语义约定（⤴️22.4，随 15.11 提前交付） ────────────────────────────

// openjiuwen.* 轨迹与关联属性键。
// Python: extensions/observability/semconv.py:47-125（逐条抄字符串值）
const (
	// OJTraceRoot 根 span 标记
	OJTraceRoot = "openjiuwen.trace.root"
	// OJSpanInput 无专用标准载体的 span 输入（JSON 字符串）
	OJSpanInput = "openjiuwen.span.input"
	// OJSpanOutput 无专用标准载体的 span 输出（JSON 字符串）
	OJSpanOutput = "openjiuwen.span.output"
	// OJRequestID 请求标识
	OJRequestID = "openjiuwen.request.id"
	// OJRequestMessageCount 请求消息条数
	OJRequestMessageCount = "openjiuwen.request.message_count"
	// OJRunID 运行标识
	OJRunID = "openjiuwen.run.id"
	// OJTurnID 轮次标识
	OJTurnID = "openjiuwen.turn.id"
	// OJTurnNumber 轮次序号（1 起）
	OJTurnNumber = "openjiuwen.turn.number"
	// OJStepID 步骤标识
	OJStepID = "openjiuwen.step.id"
	// OJStepNumber 步骤序号
	OJStepNumber = "openjiuwen.step.number"
	// OJInferenceID 推理 span 标识
	OJInferenceID = "openjiuwen.inference.id"
	// OJInferenceInputObserved false=推理未观测到请求本身（缺省=已观测）
	OJInferenceInputObserved = "openjiuwen.inference.input_observed"
	// OJRequestNumber 请求序号
	OJRequestNumber = "openjiuwen.request.number"
	// OJRequestPurpose 请求用途（assistant / compaction …）
	OJRequestPurpose = "openjiuwen.request.purpose"
	// OJTrajectoryRecordKind 轨迹记录类别（闭集见 TrajectoryRecordKinds）
	OJTrajectoryRecordKind = "openjiuwen.trajectory.record.kind"
	// OJTrajectorySchemaVersion span 级单一契约版本
	OJTrajectorySchemaVersion = "openjiuwen.trajectory.schema_version"
	// OJTrajectoryEventID v2 事件唯一标识
	OJTrajectoryEventID = "openjiuwen.trajectory.event_id"
	// OJTrajectoryEventKind v2 事件类别（闭集见 TrajectoryEventKinds）
	OJTrajectoryEventKind = "openjiuwen.trajectory.event_kind"
	// OJTrajectorySubjectID 事件归属主体
	OJTrajectorySubjectID = "openjiuwen.trajectory.subject_id"
	// OJTrajectorySequenceEpoch 序号纪元
	OJTrajectorySequenceEpoch = "openjiuwen.trajectory.sequence_epoch"
	// OJTrajectorySubjectSequence 主体内单调序号
	OJTrajectorySubjectSequence = "openjiuwen.trajectory.subject_sequence"
	// OJTrajectoryRecordedAtUnixNano 记录时刻（纳秒）
	OJTrajectoryRecordedAtUnixNano = "openjiuwen.trajectory.recorded_at_unix_nano"
	// OJTrajectoryPayload 事件载荷（JSON 字符串）
	OJTrajectoryPayload = "openjiuwen.trajectory.payload"
	// OJAgentMode 宿主运行的规范 agent 模式
	OJAgentMode = "openjiuwen.agent.mode"
	// OJExecutionSubjectID 执行主体标识
	OJExecutionSubjectID = "openjiuwen.execution.subject.id"
	// OJExecutionSubjectDisplayName 执行主体展示名
	OJExecutionSubjectDisplayName = "openjiuwen.execution.subject.display_name"
	// OJExecutionSubjectKind 执行主体类别
	OJExecutionSubjectKind = "openjiuwen.execution.subject.kind"
	// OJExecutionSubjectParentID 执行主体父标识
	OJExecutionSubjectParentID = "openjiuwen.execution.subject.parent_id"
	// OJExecutionSubjectSessionID 执行主体归属会话
	OJExecutionSubjectSessionID = "openjiuwen.execution.subject.session_id"
	// OJExecutionSubjectRequestNumber 执行主体请求序号
	OJExecutionSubjectRequestNumber = "openjiuwen.execution.subject.request.number"
	// OJGenAIUsageTotalCost 标准不建模的费用事实
	OJGenAIUsageTotalCost = "openjiuwen.gen_ai.usage.total_cost"
	// OJGenAIInputMessageProvenance 输入消息来源（附件等）
	OJGenAIInputMessageProvenance = "openjiuwen.gen_ai.input.message_provenance"
	// OJGenAIResponseTotalLatencyMs 整次调用端到端毫秒时长
	OJGenAIResponseTotalLatencyMs = "openjiuwen.gen_ai.response.total_latency_ms"
	// OJToolAuthoritative 工具可归位到引发它的推理请求
	OJToolAuthoritative = "openjiuwen.tool.authoritative"
	// OJRequestRetryCount 传输层在本次响应前完成的重试次数
	// Python: semconv.py:67 OJ_REQUEST_RETRY_COUNT
	OJRequestRetryCount = "openjiuwen.request.retry_count"
	// OJModelGroupID 模型组标识
	// Python: semconv.py:71 OJ_MODEL_GROUP_ID
	OJModelGroupID = "openjiuwen.model_group.id"
	// OJModelGroupRouteID 模型组路由标识
	// Python: semconv.py:72 OJ_MODEL_GROUP_ROUTE_ID
	OJModelGroupRouteID = "openjiuwen.model_group.route_id"
	// OJGenAIResponseTimeToFirstByteMs 响应首字节时延（毫秒）
	// Python: semconv.py:140 OJ_GEN_AI_RESPONSE_TIME_TO_FIRST_BYTE_MS
	OJGenAIResponseTimeToFirstByteMs = "openjiuwen.gen_ai.response.time_to_first_byte_ms"
	// OJContextOperationID 上下文操作标识（压缩等）
	// Python: semconv.py:73 OJ_CONTEXT_OPERATION_ID
	OJContextOperationID = "openjiuwen.context.operation.id"
	// OJCompactionNumber 压缩操作在主体内的序号（1 起）
	// Python: semconv.py:78 OJ_COMPACTION_NUMBER
	OJCompactionNumber = "openjiuwen.compaction.number"
	// ErrorType 错误类别
	ErrorType = "error.type"
)

// 标准 GenAI 键（本文件基线块未覆盖的部分）。
// Python: extensions/observability/gen_ai_semconv.py（逐条抄字符串值）
const (
	// GenAIConversationID 会话/对话标识
	GenAIConversationID = "gen_ai.conversation.id"
	// GenAIOperationName 操作名（invoke_agent / chat / execute_tool）
	GenAIOperationName = "gen_ai.operation.name"
	// GenAIAgentName Agent 展示名
	GenAIAgentName = "gen_ai.agent.name"
	// GenAIProviderName 厂商名
	GenAIProviderName = "gen_ai.provider.name"
	// GenAIRequestTopK 采样参数
	GenAIRequestTopK = "gen_ai.request.top_k"
	// GenAIRequestSeed 采样参数
	GenAIRequestSeed = "gen_ai.request.seed"
	// GenAIRequestChoiceCount 采样参数
	GenAIRequestChoiceCount = "gen_ai.request.choice.count"
	// GenAIRequestPresencePenalty 采样参数
	GenAIRequestPresencePenalty = "gen_ai.request.presence_penalty"
	// GenAIRequestFrequencyPenalty 采样参数
	GenAIRequestFrequencyPenalty = "gen_ai.request.frequency_penalty"
	// GenAIRequestReasoningLevel 采样参数
	GenAIRequestReasoningLevel = "gen_ai.request.reasoning.level"
	// GenAIRequestStream 采样参数
	GenAIRequestStream = "gen_ai.request.stream"
	// GenAIRequestStopSequences 采样参数
	GenAIRequestStopSequences = "gen_ai.request.stop_sequences"
	// GenAIResponseFinishReasons 停止原因列表（JSON）
	GenAIResponseFinishReasons = "gen_ai.response.finish_reasons"
	// GenAIResponseID 响应标识
	GenAIResponseID = "gen_ai.response.id"
	// GenAIResponseTimeToFirstChunk 到首个流式块的秒数（标准键以秒为单位）
	GenAIResponseTimeToFirstChunk = "gen_ai.response.time_to_first_chunk"
	// GenAIUsageInputTokens 输入 token
	GenAIUsageInputTokens = "gen_ai.usage.input_tokens"
	// GenAIUsageOutputTokens 输出 token
	GenAIUsageOutputTokens = "gen_ai.usage.output_tokens"
	// GenAIUsageCacheReadInputTokens 缓存输入 token
	GenAIUsageCacheReadInputTokens = "gen_ai.usage.cache_read.input_tokens"
	// GenAIUsageReasoningOutputTokens 推理输出 token
	GenAIUsageReasoningOutputTokens = "gen_ai.usage.reasoning.output_tokens"
	// GenAIToolCallArguments 工具调用参数
	GenAIToolCallArguments = "gen_ai.tool.call.arguments"
	// GenAIToolCallID 工具调用标识
	GenAIToolCallID = "gen_ai.tool.call.id"
	// GenAIToolCallResult 工具调用结果
	GenAIToolCallResult = "gen_ai.tool.call.result"
	// GenAIToolDefinitions 提供给模型的工具定义（JSON）
	GenAIToolDefinitions = "gen_ai.tool.definitions"
	// GenAISystemInstructions 对话之外单独提供的指令（JSON）
	GenAISystemInstructions = "gen_ai.system_instructions"
	// GenAIInputMessages 请求消息列表（JSON）
	GenAIInputMessages = "gen_ai.input.messages"
	// GenAIOutputMessages 响应消息列表（JSON）
	GenAIOutputMessages = "gen_ai.output.messages"
	// GenAIAgentID Agent 标识（Python semconv.py GEN_AI_AGENT_ID）
	GenAIAgentID = "gen_ai.agent.id"
	// GenAIAgentDescription Agent 描述（GEN_AI_AGENT_DESCRIPTION）
	GenAIAgentDescription = "gen_ai.agent.description"
	// GenAIAgentVersion Agent 版本（GEN_AI_AGENT_VERSION）
	GenAIAgentVersion = "gen_ai.agent.version"
	// GenAIToolDescription 工具描述（GEN_AI_TOOL_DESCRIPTION）
	GenAIToolDescription = "gen_ai.tool.description"
	// GenAIToolType 工具类别（GEN_AI_TOOL_TYPE）
	GenAIToolType = "gen_ai.tool.type"
	// OJToolResourceID 工具资源 id（OJ_TOOL_RESOURCE_ID）
	OJToolResourceID = "openjiuwen.tool.resource.id"
	// OJToolProtocol 工具协议（OJ_TOOL_PROTOCOL）
	OJToolProtocol = "openjiuwen.tool.protocol"
	// DATaskLoopEvent 触发任务迭代的事件（DA_TASK_LOOP_EVENT）
	DATaskLoopEvent = "deepagent.task.loop_event"
)

// ToolReportedFailure 工具结果报告自身失败（区别于框架侧异常）。
// Python: extensions/observability/tool_outcome.py:27
const ToolReportedFailure = "ToolReportedFailure"

// TrajectorySpanSchemaVersion span 级单一契约版本。
// Python: extensions/observability/semconv.py:83
const TrajectorySpanSchemaVersion = "2"

// TrajectoryRecordKindInference 推理记录类别。
// Python: semconv.py:231 "inference"
const TrajectoryRecordKindInference = "inference"

// TrajectoryRecordKinds OJ_TRAJECTORY_RECORD_KIND 取值闭集。
// Python: semconv.py:229-232
var TrajectoryRecordKinds = map[string]struct{}{
	"turn": {}, "step": {}, "inference": {}, "reasoning": {}, "tool": {}, "agent": {}, "event": {},
}

// TrajectoryEventKinds OJ_TRAJECTORY_EVENT_KIND 取值闭集。
// Python: semconv.py:235-240
var TrajectoryEventKinds = map[string]struct{}{
	"context.window.commit": {}, "compaction.completed": {}, "ask_user.requested": {}, "ask_user.resolved": {},
}
