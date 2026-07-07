package interfaces

import (
	"context"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AbilityManagerInterface 能力管理器接口，Agent 通过此接口注册和调度能力。
//
// 对应 Python: AbilityManager 的公开方法集。
// 具体实现：ability.AbilityManager、P2PAbilityManager。
type AbilityManagerInterface interface {
	// Add 添加单个能力。
	Add(ability schema.Ability) agentschema.AddAbilityResult
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
	Execute(
		ctx context.Context,
		cbc *AgentCallbackContext,
		toolCalls []*llmschema.ToolCall,
		sess sessioninterfaces.SessionFacade,
		tag string,
	) []agentschema.ExecuteResult
	// SetContextEngine 设置上下文引擎。
	SetContextEngine(ce ceinterface.ContextEngine)
	// ReorderTools 重排工具顺序。
	ReorderTools(orderedNames []string)
}
