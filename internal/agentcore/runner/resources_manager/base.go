package resources_manager

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptEntry Prompt 批量添加条目
//
// 对应 Python: (template_id, PromptTemplate) 元组
type PromptEntry struct {
	// ID 模板标识
	ID string
	// Template 提示模板
	Template *prompt.PromptTemplate
}

// WorkflowEntry Workflow 批量添加条目
//
// 对应 Python: (workflow_id, WorkflowProvider) 元组
type WorkflowEntry struct {
	// ID 工作流标识
	ID string
	// Provider 工作流提供者
	Provider WorkflowProvider
}

// ModelEntry Model 批量添加条目
//
// 对应 Python: (model_id, ModelProvider) 元组
type ModelEntry struct {
	// ID 模型标识
	ID string
	// Provider 模型提供者
	Provider ModelProvider
}

// AgentEntry Agent 批量添加条目
//
// 对应 Python: (AgentCard, AgentProvider) 元组
type AgentEntry struct {
	// Card Agent 身份元数据
	Card *agentschema.AgentCard
	// Provider Agent 提供者
	Provider AgentProvider
}

// AgentTeamEntry AgentTeam 批量添加条目。
//
// 对应 Python: (TeamCard, AgentTeamProvider) 元组
type AgentTeamEntry struct {
	// Card 团队身份元数据
	Card maschema.TeamCardInterface
	// Provider 团队提供者
	Provider maschema.AgentTeamProvider
}

// ──────────────────────────── 枚举 ────────────────────────────

// TagMatchStrategy 标签匹配策略
//
// 对应 Python: TagMatchStrategy (openjiuwen/core/runner/resources_manager/base.py)
type TagMatchStrategy int

const (
	// TagMatchAll 全匹配策略：资源必须包含所有指定标签
	TagMatchAll TagMatchStrategy = iota
	// TagMatchAny 任一匹配策略：资源包含任一指定标签即可
	TagMatchAny
)

// TagUpdateStrategy 标签更新策略
//
// 对应 Python: TagUpdateStrategy (openjiuwen/core/runner/resources_manager/base.py)
type TagUpdateStrategy int

const (
	// TagUpdateMerge 合并策略：新标签与已有标签合并
	TagUpdateMerge TagUpdateStrategy = iota
	// TagUpdateReplace 替换策略：新标签完全替换已有标签
	TagUpdateReplace
)

// Tag 标签类型
//
// 对应 Python: Tag = str (openjiuwen/core/runner/resources_manager/base.py)
type Tag = string

// AgentProvider Agent 资源提供者函数，接受 AgentCard 返回 BaseAgent 实例。
// 用于延迟加载，注册时传入工厂函数而非实际实例。
//
// 对应 Python: AgentProvider = Callable[[AgentCard], Awaitable[BaseAgent]] | Callable[[AgentCard], BaseAgent]
type AgentProvider func(ctx context.Context, card *agentschema.AgentCard) (interfaces.BaseAgent, error)

// WorkflowProvider Workflow 资源提供者函数，接受 WorkflowCard 返回 Workflow 实例。
//
// 对应 Python: WorkflowProvider = Callable[[WorkflowCard], Awaitable[Workflow]] | Callable[[WorkflowCard], Workflow]
type WorkflowProvider func(ctx context.Context, card *schema.WorkflowCard) (interfaces.Workflow, error)

// ModelProvider Model 资源提供者函数，接受 modelID 返回 BaseModelClient 实例。
//
// 对应 Python: ModelProvider = Callable[[...], Awaitable[BaseModel]] | Callable[[...], BaseModel]
type ModelProvider func(ctx context.Context, modelID string) (model_clients.BaseModelClient, error)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TagAll 匹配所有资源的特殊标签
	//
	// 对应 Python: ALL = "*"
	TagAll Tag = "*"
	// TagGlobal 全局标签，未分类资源的默认标签
	//
	// 对应 Python: GLOBAL = "__global__"
	TagGlobal Tag = "__global__"
	// TagActive 活跃状态标签
	//
	// 对应 Python: ACTIVE = "__active__"
	TagActive Tag = "__active__"
	// TagInactive 非活跃状态标签
	//
	// 对应 Python: INACTIVE = "__inactive__"
	TagInactive Tag = "__inactive__"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
