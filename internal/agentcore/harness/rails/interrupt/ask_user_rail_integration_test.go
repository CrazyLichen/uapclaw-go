//go:build integration

package interrupt

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestAskUserRail_Init_Integration 集成测试 AskUserRail.Init
// 需要真实 Runner 和 ResourceMgr
// 运行方式: go test -tags=integration ./internal/agentcore/harness/rails/interrupt/
func TestAskUserRail_Init_Integration(t *testing.T) {
	// 确保 Runner 已初始化
	_ = runner.GetResourceMgr()

	r := NewAskUserRail()
	agent := &mockBaseAgent{card: &agentschema.AgentCard{ID: "test_agent"}}

	err := r.Init(agent)
	assert.NoError(t, err)
	assert.Len(t, r.tools, 1)
	assert.Equal(t, "ask_user", r.tools[0].Card().Name)
}

// TestAskUserRail_Uninit_Integration 集成测试 AskUserRail.Uninit
// 需要真实 Runner 和 ResourceMgr
// 运行方式: go test -tags=integration ./internal/agentcore/harness/rails/interrupt/
func TestAskUserRail_Uninit_Integration(t *testing.T) {
	_ = runner.GetResourceMgr()

	r := NewAskUserRail()
	agent := &mockBaseAgent{card: &agentschema.AgentCard{ID: "test_agent"}}

	err := r.Init(agent)
	require.NoError(t, err)

	err = r.Uninit(agent)
	assert.NoError(t, err)
	assert.Nil(t, r.tools)
}

// TestAskUserRail_Uninit_空工具 验证无工具时 Uninit 不报错
func TestAskUserRail_Uninit_空工具(t *testing.T) {
	r := NewAskUserRail()
	agent := &mockBaseAgent{card: &agentschema.AgentCard{ID: "test_agent"}}

	err := r.Uninit(agent)
	assert.NoError(t, err)
}

// ──────────────────────────── mock ────────────────────────────

// mockBaseAgent 最小化 BaseAgent mock
type mockBaseAgent struct {
	card *agentschema.AgentCard
}

func (m *mockBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}
func (m *mockBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}
func (m *mockBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan any, error) {
	return nil, nil
}
func (m *mockBaseAgent) Card() *agentschema.AgentCard                            { return m.card }
func (m *mockBaseAgent) Config() agentinterfaces.AgentConfig                     { return nil }
func (m *mockBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }
func (m *mockBaseAgent) CallbackManager() *agentinterfaces.AgentCallbackManager  { return nil }
func (m *mockBaseAgent) SystemPromptBuilder() agentinterfaces.SystemPromptBuilderInterface {
	return nil
}
