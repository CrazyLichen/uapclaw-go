package interfaces

import (
	"context"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AbilityManagerInterface 能力管理器接口，Agent 通过此接口注册和调度能力。
// Python: AbilityManager 的公开方法集。
// 具体实现：ability.AbilityManager、P2PAbilityManager。
type AbilityManagerInterface interface {
	// Add 添加单个能力。
	Add(ability schema.Ability) agentschema.AddAbilityResult
	// AddAbility 注册一个可执行的工具能力（card + 具体实例）。
	//
	// 单一入口，保证 ability-manager 的 card id 与 resource-manager 的注册 key
	// 一致：有状态工具先限定 id 再 refresh 重绑，无状态工具保留裸 id 且
	// skip_if_exists 幂等。
	//
	// Python: AbilityManager.add_ability (ability_manager.py:806)
	AddAbility(card *tool.ToolCard, resource tool.Tool) agentschema.AddAbilityResult
	// AddMany 批量添加能力。
	AddMany(abilities []schema.Ability) []agentschema.AddAbilityResult
	// Remove 移除指定名称的能力。
	Remove(name string) schema.Ability
	// RemoveMany 批量移除能力。
	RemoveMany(names []string) []schema.Ability
	// Get 获取指定名称的能力。
	Get(name string) schema.Ability
	// List 列出所有已注册能力。
	List() []schema.Ability
	// ListToolInfo 列出工具信息供 LLM 使用。
	ListToolInfo(ctx context.Context, names []string, mcpServerName ...string) ([]schema.ToolInfoInterface, error)
	// Execute 执行工具调用。
	// parallelToolCalls 为 false 时按顺序逐个执行：有状态工具（浏览器动作序列、
	// worktree 切换）并发执行会打乱动作语义。
	Execute(
		ctx context.Context,
		cbc *AgentCallbackContext,
		toolCalls []*llmschema.ToolCall,
		sess sessioninterfaces.SessionFacade,
		tag string,
		parallelToolCalls bool,
	) []agentschema.ExecuteResult
	// SetContextEngine 设置上下文引擎。
	SetContextEngine(ce ceinterface.ContextEngine)
	// ReorderTools 重排工具顺序。
	ReorderTools(orderedNames []string)
	// TeardownTools 摘除本 owner 限定过的有状态工具（轮末拆除用）。
	// 无状态共享单例不受影响（其他 agent 仍在用）。
	// Python: AbilityManager.teardown_tools
	TeardownTools()
}
