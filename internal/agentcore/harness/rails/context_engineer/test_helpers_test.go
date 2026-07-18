package context_engineer

import (
	"context"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	tokeniface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	tooliface "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── Mock ────────────────────────────

// mockSessionFacade SessionFacade 测试用 mock
//
// 提供 states 和 updated 字段，支持 GetState/UpdateState 验证。
// 同时满足 RefreshTaskStateRuntime 和 FixIncompleteToolContext 测试需求。
type mockSessionFacade struct {
	states  map[sessstate.StateKey]any
	updated map[string]any
}

func newMockSessionFacade() *mockSessionFacade {
	return &mockSessionFacade{
		states:  make(map[sessstate.StateKey]any),
		updated: make(map[string]any),
	}
}

func (m *mockSessionFacade) GetSessionID() string { return "test-session" }
func (m *mockSessionFacade) GetState(key sessstate.StateKey) (any, error) {
	return m.states[key], nil
}
func (m *mockSessionFacade) UpdateState(data map[string]any) {
	for k, v := range data {
		m.updated[k] = v
	}
}
func (m *mockSessionFacade) DumpState() map[string]any                       { return m.updated }
func (m *mockSessionFacade) WriteStream(ctx context.Context, data any) error { return nil }
func (m *mockSessionFacade) WriteCustomStream(ctx context.Context, data any) error {
	return nil
}
func (m *mockSessionFacade) GetEnv(key string, defaultValue ...any) any    { return nil }
func (m *mockSessionFacade) Interact(ctx context.Context, value any) error { return nil }

// 确保 mock 实现了 SessionFacade 接口
var _ sessioninterfaces.SessionFacade = (*mockSessionFacade)(nil)

// mockModelContext ModelContext 测试用 mock
type mockModelContext struct {
	messages []llmschema.BaseMessage
}

func newMockModelContext() *mockModelContext {
	return &mockModelContext{}
}

func (m *mockModelContext) Len() int { return len(m.messages) }
func (m *mockModelContext) GetMessages(size int, withHistory bool) ([]llmschema.BaseMessage, error) {
	return m.messages, nil
}
func (m *mockModelContext) SetMessages(messages []llmschema.BaseMessage, withHistory bool) {
	m.messages = messages
}
func (m *mockModelContext) PopMessages(size int, withHistory bool) []llmschema.BaseMessage {
	popped := m.messages
	m.messages = nil
	return popped
}
func (m *mockModelContext) ClearMessages(ctx context.Context, withHistory bool, opts ...iface.Option) error {
	m.messages = nil
	return nil
}
func (m *mockModelContext) AddMessages(ctx context.Context, message llmschema.BaseMessage, opts ...iface.Option) ([]llmschema.BaseMessage, error) {
	m.messages = append(m.messages, message)
	return m.messages, nil
}
func (m *mockModelContext) GetContextWindow(ctx context.Context, systemMessages []llmschema.BaseMessage, tools []cschema.ToolInfoInterface, windowSize int, dialogueRound int, opts ...iface.Option) (*iface.ContextWindow, error) {
	return nil, nil
}
func (m *mockModelContext) Statistic() *iface.ContextStats                                  { return nil }
func (m *mockModelContext) SessionID() string                                               { return "test" }
func (m *mockModelContext) ContextID() string                                               { return "test" }
func (m *mockModelContext) TokenCounter() tokeniface.TokenCounter                           { return nil }
func (m *mockModelContext) ReloaderTool() tooliface.Tool                                    { return nil }
func (m *mockModelContext) WorkspaceDir() string                                            { return "" }
func (m *mockModelContext) SetSessionRef(sess sessioninterfaces.SessionFacade)              {}
func (m *mockModelContext) GetSessionRef() sessioninterfaces.SessionFacade                  { return nil }
func (m *mockModelContext) OffloadMessages(handle string, messages []llmschema.BaseMessage) {}
func (m *mockModelContext) SaveState() map[string]any                                       { return nil }
func (m *mockModelContext) LoadState(state map[string]any)                                  {}
func (m *mockModelContext) CompressContext(ctx context.Context, opts ...iface.CompressContextOption) (string, error) {
	return "noop", nil
}

// 确保 mock 实现了 ModelContext 接口
var _ iface.ModelContext = (*mockModelContext)(nil)

// ──────────────────────────── 辅助 ────────────────────────────

// newCallbackContextWithMC 创建带有 ModelContext 的 AgentCallbackContext
func newCallbackContextWithMC(mc iface.ModelContext) *sainterfaces.AgentCallbackContext {
	ctx := sainterfaces.NewAgentCallbackContext(nil, nil, newMockSessionFacade())
	ctx.SetModelContext(mc)
	return ctx
}

// setCallbackSession 使用构造函数设置 AgentCallbackContext 的 session 字段
func setCallbackSession(ctx *sainterfaces.AgentCallbackContext, sess sessioninterfaces.SessionFacade) {
	*ctx = *sainterfaces.NewAgentCallbackContext(nil, nil, sess)
}
