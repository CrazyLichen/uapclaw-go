package observability

// ──────────────────────────── 常量 ────────────────────────────

// OpenLLMetry / GenAI 标准属性
// Python: GEN_AI_* 常量
const (
	GenAISystem                = "gen_ai.system"
	GenAIRequestModel          = "gen_ai.request.model"
	GenAIRequestTemperature    = "gen_ai.request.temperature"
	GenAIRequestTopP           = "gen_ai.request.top_p"
	GenAIRequestMaxTokens      = "gen_ai.request.max_tokens"
	GenAIPrompt                = "gen_ai.prompt"
	GenAICompletion            = "gen_ai.completion"
	GenAIUsagePromptTokens     = "gen_ai.usage.prompt_tokens"
	GenAIUsageCompletionTokens = "gen_ai.usage.completion_tokens"
	GenAIUsageTotalTokens      = "gen_ai.usage.total_tokens"
	GenAIResponseFinishReason  = "gen_ai.response.finish_reason"
	GenAIResponseModel         = "gen_ai.response.model"
	GenAIResponseTTFTMs        = "gen_ai.response.time_to_first_token_ms"
	GenAIToolName              = "gen_ai.tool.name"
	GenAIToolInput             = "gen_ai.tool.input"
	GenAIToolOutput            = "gen_ai.tool.output"
	GenAIToolID                = "gen_ai.tool.id"
)

// agentteam.* — Team 协作属性（Monitor handler）
// Python: AT_* 常量
const (
	ATTeamName            = "agentteam.team.name"
	ATTeamDisplayName     = "agentteam.team.display_name"
	ATEventType           = "agentteam.event_type"
	ATAgentID             = "agentteam.agent.id"
	ATAgentRole           = "agentteam.agent.role"
	ATAgentInput          = "agentteam.agent.input"
	ATAgentOutput         = "agentteam.agent.output"
	ATMemberName          = "agentteam.member.name"
	ATMemberStatusOld     = "agentteam.member.status.old"
	ATMemberStatusNew     = "agentteam.member.status.new"
	ATMemberRestartReason = "agentteam.member.restart_reason"
	ATMemberRestartCount  = "agentteam.member.restart_count"
	ATMemberShutdownForce = "agentteam.member.shutdown_force"
	ATMessageID           = "agentteam.message.id"
	ATMessageFrom         = "agentteam.message.from"
	ATMessageTo           = "agentteam.message.to"
	ATMessageBroadcast    = "agentteam.message.broadcast"
	ATTaskID              = "agentteam.task.id"
	ATTaskStatus          = "agentteam.task.status"
	ATTaskAssignee        = "agentteam.task.assignee"
	ATTeamLeader          = "agentteam.team.leader"
)

// deepagent.* — DeepAgent task-loop 属性（Rail）
// Python: DA_* 常量
const (
	DATaskIteration  = "deepagent.task.iteration"
	DATaskIsFollowUp = "deepagent.task.is_follow_up"
)
