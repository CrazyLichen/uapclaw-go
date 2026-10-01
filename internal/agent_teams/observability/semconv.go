package observability

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

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

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
