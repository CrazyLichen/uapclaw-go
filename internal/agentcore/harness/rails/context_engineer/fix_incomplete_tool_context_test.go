package context_engineer

import (
	"context"
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	tokeniface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	tooliface "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── Mock ────────────────────────────

// mockModelContext FixIncompleteToolContext 测试用 mock
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
func (m *mockModelContext) Statistic() *iface.ContextStats { return nil }
func (m *mockModelContext) SessionID() string              { return "test" }
func (m *mockModelContext) ContextID() string              { return "test" }
func (m *mockModelContext) TokenCounter() tokeniface.TokenCounter   { return nil }
func (m *mockModelContext) ReloaderTool() tooliface.Tool             { return nil }
func (m *mockModelContext) WorkspaceDir() string           { return "" }
func (m *mockModelContext) SetSessionRef(sess sessioninterfaces.SessionFacade) {}
func (m *mockModelContext) GetSessionRef() sessioninterfaces.SessionFacade { return nil }
func (m *mockModelContext) OffloadMessages(handle string, messages []llmschema.BaseMessage) {}
func (m *mockModelContext) SaveState() map[string]any      { return nil }
func (m *mockModelContext) LoadState(state map[string]any)  {}
func (m *mockModelContext) CompressContext(ctx context.Context, opts ...iface.CompressContextOption) (string, error) {
	return "noop", nil
}

// 确保 mock 实现了 ModelContext 接口
var _ iface.ModelContext = (*mockModelContext)(nil)

// mockMinimalSession 最小化 Session mock
type mockMinimalSession struct{}

func (m *mockMinimalSession) GetSessionID() string                                         { return "test" }
func (m *mockMinimalSession) UpdateState(data map[string]any)                              {}
func (m *mockMinimalSession) GetState(key sessstate.StateKey) (interface{}, error)         { return nil, nil }
func (m *mockMinimalSession) DumpState() map[string]any                                    { return nil }
func (m *mockMinimalSession) WriteStream(ctx context.Context, data interface{}) error      { return nil }
func (m *mockMinimalSession) WriteCustomStream(ctx context.Context, data interface{}) error { return nil }
func (m *mockMinimalSession) GetEnv(key string, defaultValue ...interface{}) interface{}   { return nil }
func (m *mockMinimalSession) Interact(ctx context.Context, value interface{}) error        { return nil }

var _ sessioninterfaces.SessionFacade = (*mockMinimalSession)(nil)

// newCallbackContextWithMC 创建带有 ModelContext 的 AgentCallbackContext
func newCallbackContextWithMC(mc iface.ModelContext) *sainterfaces.AgentCallbackContext {
	ctx := sainterfaces.NewAgentCallbackContext(nil, nil, &mockMinimalSession{})
	ctx.SetModelContext(mc)
	return ctx
}

// ──────────────────────────── EnsureJSONArguments 测试 ────────────────────────────

func TestEnsureJSONArguments_合法JSON(t *testing.T) {
	result := EnsureJSONArguments(`{"key": "value"}`)
	if result != `{"key": "value"}` {
		t.Errorf("合法 JSON 应原样返回, got %q", result)
	}
}

func TestEnsureJSONArguments_空字符串(t *testing.T) {
	result := EnsureJSONArguments("")
	if result != "{}" {
		t.Errorf("空字符串应返回 {}, got %q", result)
	}
}

func TestEnsureJSONArguments_非法JSON(t *testing.T) {
	result := EnsureJSONArguments("not json")
	if result != "{}" {
		t.Errorf("非法 JSON 应返回 {}, got %q", result)
	}
}

func TestEnsureJSONArguments_非Object类型(t *testing.T) {
	result := EnsureJSONArguments(`[1, 2, 3]`)
	if result != "{}" {
		t.Errorf("非 Object 类型应返回 {}, got %q", result)
	}
}

func TestEnsureJSONArguments_数字(t *testing.T) {
	result := EnsureJSONArguments(`42`)
	if result != "{}" {
		t.Errorf("数字应返回 {}, got %q", result)
	}
}

// ──────────────────────────── FixIncompleteToolContext 测试 ────────────────────────────

func TestFixIncompleteToolContext_nilModelContext(t *testing.T) {
	ctx := &sainterfaces.AgentCallbackContext{}
	// ModelContext 为 nil，不应 panic
	FixIncompleteToolContext(context.Background(), ctx)
}

func TestFixIncompleteToolContext_空消息(t *testing.T) {
	mc := newMockModelContext()
	ctx := newCallbackContextWithMC(mc)

	FixIncompleteToolContext(context.Background(), ctx)

	if len(mc.messages) != 0 {
		t.Error("空消息列表不应改变")
	}
}

func TestFixIncompleteToolContext_正常配对(t *testing.T) {
	mc := newMockModelContext()
	// 构造正常的 tool_call + ToolMessage 配对
	assistantMsg := llmschema.NewAssistantMessage("I'll help you",
		llmschema.WithToolCalls([]*llmschema.ToolCall{
			llmschema.NewToolCall("tc1", "read_file", `{"path": "/test"}`),
		}),
	)
	toolMsg := llmschema.NewToolMessage("tc1", "file content")
	mc.messages = []llmschema.BaseMessage{assistantMsg, toolMsg}

	ctx := newCallbackContextWithMC(mc)

	FixIncompleteToolContext(context.Background(), ctx)

	// 消息应保持 2 条
	if len(mc.messages) != 2 {
		t.Errorf("消息数量 = %d, want 2", len(mc.messages))
	}
}

func TestFixIncompleteToolContext_缺失ToolMessage(t *testing.T) {
	mc := newMockModelContext()
	// 只有 tool_call，没有对应的 ToolMessage
	assistantMsg := llmschema.NewAssistantMessage("I'll help you",
		llmschema.WithToolCalls([]*llmschema.ToolCall{
			llmschema.NewToolCall("tc1", "read_file", `{"path": "/test"}`),
		}),
	)
	mc.messages = []llmschema.BaseMessage{assistantMsg}

	ctx := newCallbackContextWithMC(mc)

	FixIncompleteToolContext(context.Background(), ctx)

	// 应自动补一条占位 ToolMessage
	if len(mc.messages) != 2 {
		t.Fatalf("消息数量 = %d, want 2", len(mc.messages))
	}
	toolMsg, ok := mc.messages[1].(*llmschema.ToolMessage)
	if !ok {
		t.Fatal("第二条消息应为 ToolMessage")
	}
	if toolMsg.ToolCallID != "tc1" {
		t.Errorf("ToolCallID = %q, want tc1", toolMsg.ToolCallID)
	}
}

func TestFixIncompleteToolContext_多个ToolCall部分缺失(t *testing.T) {
	mc := newMockModelContext()
	// 2 个 tool_call，只有 1 个 ToolMessage
	assistantMsg := llmschema.NewAssistantMessage("",
		llmschema.WithToolCalls([]*llmschema.ToolCall{
			llmschema.NewToolCall("tc1", "read_file", `{"path": "/test"}`),
			llmschema.NewToolCall("tc2", "write_file", `{"path": "/test2"}`),
		}),
	)
	toolMsg1 := llmschema.NewToolMessage("tc1", "file content")
	mc.messages = []llmschema.BaseMessage{assistantMsg, toolMsg1}

	ctx := newCallbackContextWithMC(mc)

	FixIncompleteToolContext(context.Background(), ctx)

	// 应有 3 条消息：AssistantMessage + ToolMessage(tc1) + 占位 ToolMessage(tc2)
	if len(mc.messages) != 3 {
		t.Fatalf("消息数量 = %d, want 3", len(mc.messages))
	}
	toolMsg2, ok := mc.messages[2].(*llmschema.ToolMessage)
	if !ok {
		t.Fatal("第三条消息应为 ToolMessage")
	}
	if toolMsg2.ToolCallID != "tc2" {
		t.Errorf("ToolCallID = %q, want tc2", toolMsg2.ToolCallID)
	}
}

func TestFixIncompleteToolContext_中间中断(t *testing.T) {
	mc := newMockModelContext()
	// AssistantMessage(tool_call) + UserMessage(中断) — 缺少 ToolMessage
	assistantMsg := llmschema.NewAssistantMessage("",
		llmschema.WithToolCalls([]*llmschema.ToolCall{
			llmschema.NewToolCall("tc1", "read_file", `{"path": "/test"}`),
		}),
	)
	userMsg := llmschema.NewUserMessage("stop!")
	mc.messages = []llmschema.BaseMessage{assistantMsg, userMsg}

	ctx := newCallbackContextWithMC(mc)

	FixIncompleteToolContext(context.Background(), ctx)

	// 应在 AssistantMessage 和 UserMessage 之间插入占位 ToolMessage
	if len(mc.messages) != 3 {
		t.Fatalf("消息数量 = %d, want 3", len(mc.messages))
	}
	toolMsg, ok := mc.messages[1].(*llmschema.ToolMessage)
	if !ok {
		t.Fatal("第二条消息应为占位 ToolMessage")
	}
	if toolMsg.ToolCallID != "tc1" {
		t.Errorf("ToolCallID = %q, want tc1", toolMsg.ToolCallID)
	}
}

func TestFixIncompleteToolContext_非法Arguments修复(t *testing.T) {
	mc := newMockModelContext()
	// tool_call 的 arguments 不是合法 JSON
	assistantMsg := llmschema.NewAssistantMessage("",
		llmschema.WithToolCalls([]*llmschema.ToolCall{
			llmschema.NewToolCall("tc1", "read_file", "not json"),
		}),
	)
	toolMsg := llmschema.NewToolMessage("tc1", "result")
	mc.messages = []llmschema.BaseMessage{assistantMsg, toolMsg}

	ctx := newCallbackContextWithMC(mc)

	FixIncompleteToolContext(context.Background(), ctx)

	// 检查 arguments 被修复为 "{}"
	am, ok := mc.messages[0].(*llmschema.AssistantMessage)
	if !ok {
		t.Fatal("第一条消息应为 AssistantMessage")
	}
	if len(am.ToolCalls) != 1 || am.ToolCalls[0].Arguments != "{}" {
		t.Errorf("Arguments = %q, want {}", am.ToolCalls[0].Arguments)
	}
}
