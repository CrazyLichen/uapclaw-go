package worktree

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	tool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试用结构体 ────────────────────────────

// stubAbilityManager 记录工具增删的 AbilityManager 替身。
type stubAbilityManager struct {
	// added 已添加的工具名
	added []string
	// removed 已移除的工具名
	removed []string
}

// Add 记录添加。
func (m *stubAbilityManager) Add(ability commonschema.Ability) agentschema.AddAbilityResult {
	if card, ok := ability.(*tool.ToolCard); ok {
		m.added = append(m.added, card.Name)
		return agentschema.AddAbilityResult{Name: card.Name, Added: true}
	}
	return agentschema.AddAbilityResult{}
}

// AddAbility 记录添加。
func (m *stubAbilityManager) AddAbility(card *tool.ToolCard, _ tool.Tool) agentschema.AddAbilityResult {
	return m.Add(card)
}

// AddMany 批量添加。
func (m *stubAbilityManager) AddMany(abilities []commonschema.Ability) []agentschema.AddAbilityResult {
	results := make([]agentschema.AddAbilityResult, 0, len(abilities))
	for _, a := range abilities {
		results = append(results, m.Add(a))
	}
	return results
}

// Remove 记录移除。
func (m *stubAbilityManager) Remove(name string) commonschema.Ability {
	m.removed = append(m.removed, name)
	return nil
}

// RemoveMany 批量移除。
func (m *stubAbilityManager) RemoveMany(names []string) []commonschema.Ability {
	for _, n := range names {
		m.Remove(n)
	}
	return nil
}

// Get 未实现。
func (m *stubAbilityManager) Get(string) commonschema.Ability { return nil }

// List 返回空列表。
func (m *stubAbilityManager) List() []commonschema.Ability { return nil }

// ListToolInfo 返回空列表。
func (m *stubAbilityManager) ListToolInfo(context.Context, []string, ...string) ([]commonschema.ToolInfoInterface, error) {
	return nil, nil
}

// Execute 返回空结果。
func (m *stubAbilityManager) Execute(context.Context, *agentinterfaces.AgentCallbackContext, []*llmschema.ToolCall, sessioninterfaces.SessionFacade, string, bool) []agentschema.ExecuteResult {
	return nil
}

// SetContextEngine 忽略。
func (m *stubAbilityManager) SetContextEngine(ceinterface.ContextEngine) {}

// ReorderTools 忽略。
func (m *stubAbilityManager) ReorderTools([]string) {}

// TeardownTools 忽略。
func (m *stubAbilityManager) TeardownTools() {}

// stubAgent WorktreeRail.Init/Uninit 所需的最小 BaseAgent 替身。
type stubAgent struct {
	// am 能力管理器
	am *stubAbilityManager
	// builder 提示词构建器
	builder *saprompt.SystemPromptBuilder
}

// Configure 忽略配置。
func (a *stubAgent) Configure(context.Context, agentinterfaces.AgentConfig) error { return nil }

// Invoke 返回空结果。
func (a *stubAgent) Invoke(context.Context, map[string]any, ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}

// Stream 返回空流。
func (a *stubAgent) Stream(context.Context, map[string]any, ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}

// Card 返回固定卡片。
func (a *stubAgent) Card() *agentschema.AgentCard {
	return &agentschema.AgentCard{BaseCard: commonschema.BaseCard{ID: "wt-agent", Name: "WtAgent"}}
}

// Config 返回空配置。
func (a *stubAgent) Config() agentinterfaces.AgentConfig { return nil }

// AbilityManager 返回替身。
func (a *stubAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return a.am }

// CallbackManager 返回新管理器。
func (a *stubAgent) CallbackManager() *agentinterfaces.AgentCallbackManager {
	return agentinterfaces.NewAgentCallbackManager("wt-agent")
}

// SystemPromptBuilder 返回构建器。
func (a *stubAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return a.builder }

// RegisterCallback 忽略。
func (a *stubAgent) RegisterCallback(context.Context, agentinterfaces.AgentCallbackEvent, callback.PerAgentCallbackFunc, ...callback.CallbackOption) error {
	return nil
}

// RegisterRail 忽略。
func (a *stubAgent) RegisterRail(context.Context, agentinterfaces.AgentRail, ...callback.CallbackOption) error {
	return nil
}

// UnregisterRail 忽略。
func (a *stubAgent) UnregisterRail(context.Context, agentinterfaces.AgentRail) error { return nil }

// ──────────────────────────── 用例 ────────────────────────────

// TestWorktreeRail_Init注册工具 Init 构造管理器并注册 enter/exit 工具。
func TestWorktreeRail_Init注册工具(t *testing.T) {
	rail := NewWorktreeRail()
	agent := &stubAgent{am: &stubAbilityManager{}, builder: saprompt.NewSystemPromptBuilder()}

	require.NoError(t, rail.Init(context.Background(), agent))
	assert.NotNil(t, rail.Manager())
	assert.Len(t, rail.tools, 2)
	assert.NotEmpty(t, agent.am.added, "工具必须注册到 ability_manager")
}

// TestWorktreeRail_Uninit移除工具 Uninit 清空管理器并从 ability_manager 移除工具。
func TestWorktreeRail_Uninit移除工具(t *testing.T) {
	rail := NewWorktreeRail()
	agent := &stubAgent{am: &stubAbilityManager{}, builder: saprompt.NewSystemPromptBuilder()}
	require.NoError(t, rail.Init(context.Background(), agent))

	require.NoError(t, rail.Uninit(agent))
	assert.Nil(t, rail.Manager())
	assert.Empty(t, rail.tools)
	assert.NotEmpty(t, agent.am.removed, "工具必须从 ability_manager 移除")
}

// TestWorktreeRail_Init空Agent 未提供 agent 时使用默认语言并跳过能力注册。
func TestWorktreeRail_Init空Agent(t *testing.T) {
	rail := NewWorktreeRail()
	require.NoError(t, rail.Init(context.Background(), nil))
	assert.NotNil(t, rail.Manager())
}

// TestWorktreeRail_Priority 优先级固定 1000 之前（低于系统轨）。
func TestWorktreeRail_Priority(t *testing.T) {
	rail := NewWorktreeRail()
	assert.Greater(t, rail.Priority(), 0)
}
