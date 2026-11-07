package rails

import (
	"context"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockSession 简单的 SessionFacade mock（rails 包内多处测试共用）。
type mockSession struct {
	// state 状态表
	state map[string]any
}

// fakeBaseAgent 满足 BaseAgent 接口的测试 mock。
type fakeBaseAgent struct {
	// cbMgr 回调管理器
	cbMgr *agentinterfaces.AgentCallbackManager
	// builder 系统提示词构建器
	builder *saprompt.SystemPromptBuilder
	// goalMgr 会话级 Goal 能力面（默认 nil，供 Init 继承兜底用例）
	goalMgr *goal.GoalManager
}

// ──────────────────────────── 导出函数 ────────────────────────────

// newMockSession 创建会话 mock。
func newMockSession() *mockSession {
	return &mockSession{state: make(map[string]any)}
}

// GetSessionID 返回固定会话 id。
func (m *mockSession) GetSessionID() string { return "test-session" }

// UpdateState 合并写入状态。
func (m *mockSession) UpdateState(data map[string]any) {
	for key, value := range data {
		m.state[key] = value
	}
}

// GetState 读取状态。
func (m *mockSession) GetState(key state.StateKey) (any, error) {
	if key.Type() != state.StateKeyString {
		return nil, nil
	}
	value, ok := m.state[key.String()]
	if !ok {
		return nil, nil
	}
	return value, nil
}

// DumpState 导出状态。
func (m *mockSession) DumpState() map[string]any { return m.state }

// WriteStream 空实现。
func (m *mockSession) WriteStream(_ context.Context, _ any) error { return nil }

// WriteCustomStream 空实现。
func (m *mockSession) WriteCustomStream(_ context.Context, _ any) error { return nil }

// GetEnv 返回 nil。
func (m *mockSession) GetEnv(_ string, _ ...any) any { return nil }

// Interact 空实现。
func (m *mockSession) Interact(_ context.Context, _ any) error { return nil }

// ClearSession 空实现。
func (m *mockSession) ClearSession(_ context.Context) {}

// newFakeBaseAgent 创建 Agent mock。
func newFakeBaseAgent() *fakeBaseAgent {
	return &fakeBaseAgent{
		cbMgr:   agentinterfaces.NewAgentCallbackManager("test-agent"),
		builder: saprompt.NewSystemPromptBuilder(),
	}
}

// Configure 空实现。
func (f *fakeBaseAgent) Configure(_ context.Context, _ agentinterfaces.AgentConfig) error {
	return nil
}

// Invoke 空实现。
func (f *fakeBaseAgent) Invoke(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (map[string]any, error) {
	return nil, nil
}

// Stream 空实现。
func (f *fakeBaseAgent) Stream(_ context.Context, _ map[string]any, _ ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	return nil, nil
}

// Card 返回 Agent 卡片。
func (f *fakeBaseAgent) Card() *agentschema.AgentCard {
	return &agentschema.AgentCard{
		BaseCard: cschema.BaseCard{ID: "test-agent", Name: "TestAgent"},
	}
}

// Config 返回 nil。
func (f *fakeBaseAgent) Config() agentinterfaces.AgentConfig { return nil }

// AbilityManager 返回 nil。
func (f *fakeBaseAgent) AbilityManager() agentinterfaces.AbilityManagerInterface { return nil }

// GoalManager 实现 goalManagerProvider 接口（供 Init 继承兜底用例）。
func (f *fakeBaseAgent) GoalManager() *goal.GoalManager { return f.goalMgr }

// CallbackManager 返回回调管理器。
func (f *fakeBaseAgent) CallbackManager() *agentinterfaces.AgentCallbackManager { return f.cbMgr }

// SystemPromptBuilder 返回构建器。
func (f *fakeBaseAgent) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface {
	if f.builder == nil {
		return nil
	}
	return f.builder
}

// RegisterCallback 空实现。
func (f *fakeBaseAgent) RegisterCallback(_ context.Context, _ agentinterfaces.AgentCallbackEvent, _ cb.PerAgentCallbackFunc, _ ...cb.CallbackOption) error {
	return nil
}

// RegisterRail 空实现。
func (f *fakeBaseAgent) RegisterRail(_ context.Context, _ agentinterfaces.AgentRail, _ ...cb.CallbackOption) error {
	return nil
}

// UnregisterRail 空实现。
func (f *fakeBaseAgent) UnregisterRail(_ context.Context, _ agentinterfaces.AgentRail) error {
	return nil
}

// UapswarmProjectDir 空实现。
func (f *fakeBaseAgent) UapswarmProjectDir() string { return "" }

// SetUapswarmProjectDir 空实现。
func (f *fakeBaseAgent) SetUapswarmProjectDir(string) {}

// UapswarmCodeProjectDir 空实现。
func (f *fakeBaseAgent) UapswarmCodeProjectDir() string { return "" }

// SetUapswarmCodeProjectDir 空实现。
func (f *fakeBaseAgent) SetUapswarmCodeProjectDir(string) {}

// UapswarmAdapterMode 空实现。
func (f *fakeBaseAgent) UapswarmAdapterMode() string { return "" }

// SetUapswarmAdapterMode 空实现。
func (f *fakeBaseAgent) SetUapswarmAdapterMode(string) {}

// 编译时验证 mock 满足接口
var (
	_ sessioninterfaces.SessionFacade = (*mockSession)(nil)
	_ agentinterfaces.BaseAgent       = (*fakeBaseAgent)(nil)
	_                                 = llmschema.RoleTypeUser
)
