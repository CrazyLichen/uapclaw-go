//go:build integration

package interrupt_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AskUserSuite AskUserRail 集成测试套件
type AskUserSuite struct {
	isuite.BaseIntegrationSuite
}

// mockBaseAgent 最小化 BaseAgent mock
type mockBaseAgent struct {
	card *agentschema.AgentCard
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestAskUserSuite 运行 AskUserRail 集成测试套件
func TestAskUserSuite(t *testing.T) {
	suite.Run(t, new(AskUserSuite))
}

// TestAskUserRail_Init_Integration 集成测试 AskUserRail.Init
// 需要真实 Runner 和 ResourceMgr
// 运行方式: go test -tags=integration ./tests/integration/agentcore/harness/rails/interrupt/...
func (s *AskUserSuite) TestAskUserRail_Init_Integration() {
	_ = runner.GetResourceMgr()

	r := interrupt.NewAskUserRail()
	agent := &mockBaseAgent{card: agentschema.NewAgentCard(
		agentschema.WithAgentID("test_agent"),
		agentschema.WithAgentName("测试Agent"),
	)}

	err := r.Init(context.Background(), agent)
	assert.NoError(s.T(), err)
}

// TestAskUserRail_Uninit_Integration 集成测试 AskUserRail.Uninit
// 需要真实 Runner 和 ResourceMgr
// 运行方式: go test -tags=integration ./tests/integration/agentcore/harness/rails/interrupt/...
func (s *AskUserSuite) TestAskUserRail_Uninit_Integration() {
	_ = runner.GetResourceMgr()

	r := interrupt.NewAskUserRail()
	agent := &mockBaseAgent{card: agentschema.NewAgentCard(
		agentschema.WithAgentID("test_agent"),
		agentschema.WithAgentName("测试Agent"),
	)}

	err := r.Init(context.Background(), agent)
	require.NoError(s.T(), err)

	err = r.Uninit(agent)
	assert.NoError(s.T(), err)
}

// TestAskUserRail_Uninit_空工具 验证无工具时 Uninit 不报错
func (s *AskUserSuite) TestAskUserRail_Uninit_空工具() {
	r := interrupt.NewAskUserRail()
	agent := &mockBaseAgent{card: agentschema.NewAgentCard(
		agentschema.WithAgentID("test_agent"),
		agentschema.WithAgentName("测试Agent"),
	)}

	err := r.Uninit(agent)
	assert.NoError(s.T(), err)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mockBaseAgent 实现 agentinterfaces.BaseAgent 接口的所有方法

func (m *mockBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}

func (m *mockBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}

func (m *mockBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}

func (m *mockBaseAgent) Card() *agentschema.AgentCard {
	return m.card
}

func (m *mockBaseAgent) Config() agentinterfaces.AgentConfig {
	return nil
}

func (m *mockBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return nil
}

func (m *mockBaseAgent) CallbackManager() *agentinterfaces.AgentCallbackManager {
	return nil
}

func (m *mockBaseAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	return nil
}

func (m *mockBaseAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}

func (m *mockBaseAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}

func (m *mockBaseAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}
