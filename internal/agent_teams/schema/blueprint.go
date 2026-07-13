package schema

import (
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamModelConfig 可序列化的团队模型配置。
// 对齐 Python: TeamModelConfig (openjiuwen/agent_teams/schema/deep_agent_spec.py)
//
// 用于团队角色级别的模型配置，包含客户端配置和请求配置。
type TeamModelConfig struct {
	// ModelClientConfig 模型客户端配置
	ModelClientConfig llmschema.ModelClientConfig
	// ModelRequestConfig 模型请求配置（可选）
	ModelRequestConfig *llmschema.ModelRequestConfig
}

// LeaderSpec Leader 身份规格。
// 对齐 Python: LeaderSpec (openjiuwen/agent_teams/schema/blueprint.py)
type LeaderSpec struct {
	// MemberName Leader 成员名
	MemberName string
	// DisplayName 显示名
	DisplayName string
	// Persona 人设描述
	Persona string
	// ModelName 模型池分配名称（可选）
	ModelName string
}

// TransportSpec 可插拔传输层规格。
// 对齐 Python: TransportSpec
type TransportSpec struct {
	// Type 传输类型（inprocess/pyzmq 等）
	Type string
	// Params 传输参数
	Params map[string]any
}

// StorageSpec 可插拔存储层规格。
// 对齐 Python: StorageSpec
type StorageSpec struct {
	// Type 存储类型（sqlite/postgresql/mysql/memory 等）
	Type string
	// Params 存储参数
	Params map[string]any
}

// DeepAgentSpec 单角色 DeepAgent 规格。
// 对齐 Python: DeepAgentSpec (openjiuwen/agent_teams/schema/deep_agent_spec.py)
// ⤵️ 回填: 9.57 — DeepAgentSpec 完整字段（system_prompt/tools/mcps/subagents/rails/workspace 等），当前仅保留最小集
type DeepAgentSpec struct {
	// Card Agent 身份卡片
	Card *agentschema.AgentCard
	// Model 团队模型配置
	Model *TeamModelConfig
	// Language 语言偏好
	Language string
}

// TeamAgentSpec 构造 TeamAgent 的完整 JSON 可序列化规格。
// 对齐 Python: TeamAgentSpec (openjiuwen/agent_teams/schema/blueprint.py)
//
// 组合每角色 DeepAgentSpec 与团队级配置。
// agents 的 key 对应 TeamRole 值（"leader"、"teammate"）。
type TeamAgentSpec struct {
	// Agents 每角色 DeepAgent 规格
	Agents map[string]DeepAgentSpec
	// TeamName 团队名
	TeamName string
	// Lifecycle 生命周期模式（temporary/persistent）
	Lifecycle TeamLifecycle
	// EnableTeamPlan Leader 是否以单 Agent plan 模式启动
	EnableTeamPlan bool
	// TeammateMode 成员执行模式（build_mode/plan_mode）
	TeammateMode MemberMode
	// SpawnMode 生成模式（process/inprocess）
	SpawnMode string
	// Leader Leader 规格
	Leader LeaderSpec
	// PredefinedMembers 预定义成员列表
	PredefinedMembers []TeamMemberSpec
	// ModelPool LLM 端点池（⤵️ 回填: 9.64 — []ModelPoolEntry 类型）
	ModelPool any
	// ModelRouter 单端点路由配置（⤵️ 回填: 9.64 — ModelRouterConfig 类型）
	ModelRouter any
	// ModelPoolStrategy 池分配策略（round_robin/by_model_name/router）
	ModelPoolStrategy string
	// TeamMode 团队操作模式（default/predefined/hybrid）
	TeamMode string
	// Transport 传输层规格
	Transport *TransportSpec
	// Storage 存储层规格
	Storage *StorageSpec
	// Worktree Worktree 隔离配置（⤵️ 回填: 9.66 — WorktreeConfig 类型）
	Worktree any
	// Workspace 共享工作空间配置（⤵️ 回填: 9.66 — TeamWorkspaceConfig 类型）
	Workspace any
	// Metadata 元数据
	Metadata map[string]any
	// EnableHITT Human-in-the-Team 能力天花板
	EnableHITT bool
	// ExposeHumanAgentsToTeammates 是否向 Teammate 暴露人类代理名单
	ExposeHumanAgentsToTeammates bool
	// Language 首选语言（cn/en）
	Language string
	// AgentCustomizer 可选回调，在每个成员的 DeepAgent 创建后调用（不可序列化）
	// ⤵️ 回填: 9.57 — AgentCustomizer 回调类型
	AgentCustomizer any
	// Memory 团队记忆配置（⤵️ 回填: 9.64 — TeamMemoryConfig 类型）
	Memory any
}
