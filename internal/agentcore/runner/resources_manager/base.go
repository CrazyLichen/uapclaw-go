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

type PromptEntry struct {
	// ID 模板标识
	ID string
	// Template 提示模板
	Template *prompt.PromptTemplate
}

type WorkflowEntry struct {
	// ID 工作流标识
	ID string
	// Provider 工作流提供者
	Provider WorkflowProvider
}

type ModelEntry struct {
	// ID 模型标识
	ID string
	// Provider 模型提供者
	Provider ModelProvider
}

type AgentEntry struct {
	// Card Agent 身份元数据
	Card *agentschema.AgentCard
	// Provider Agent 提供者
	Provider AgentProvider
}

type AgentTeamEntry struct {
	// Card 团队身份元数据
	Card maschema.TeamCardInterface
	// Provider 团队提供者
	Provider maschema.AgentTeamProvider
}

// AgentProvider Agent 提供者函数类型。
type AgentProvider func(ctx context.Context, card *agentschema.AgentCard) (interfaces.BaseAgent, error)

// WorkflowProvider 工作流提供者函数类型。
type WorkflowProvider func(ctx context.Context, card *schema.WorkflowCard) (interfaces.Workflow, error)

// ModelProvider 模型提供者函数类型。
type ModelProvider func(ctx context.Context, modelID string) (model_clients.BaseModelClient, error)

// ──────────────────────────── 枚举 ────────────────────────────

type TagMatchStrategy int

type TagUpdateStrategy int

// Tag 标签类型别名。
type Tag = string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TagMatchAll 全匹配策略：资源必须包含所有指定标签
	TagMatchAll TagMatchStrategy = iota
	// TagMatchAny 任一匹配策略：资源包含任一指定标签即可
	TagMatchAny
)

const (
	// TagUpdateMerge 合并策略：新标签与已有标签合并
	TagUpdateMerge TagUpdateStrategy = iota
	// TagUpdateReplace 替换策略：新标签完全替换已有标签
	TagUpdateReplace
)

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
