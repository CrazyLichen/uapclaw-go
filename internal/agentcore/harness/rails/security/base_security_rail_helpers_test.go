package security

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 测试用结构体 ────────────────────────────

// memModelContext 仅实现消息读写的最小 ModelContext 替身。
type memModelContext struct {
	// messages 当前消息段
	messages []llmschema.BaseMessage
}

// Len 返回消息数。
func (m *memModelContext) Len() int { return len(m.messages) }

// GetMessages 返回消息副本。
func (m *memModelContext) GetMessages(_ int, _ bool) ([]llmschema.BaseMessage, error) {
	return append([]llmschema.BaseMessage(nil), m.messages...), nil
}

// SetMessages 覆盖消息段。
func (m *memModelContext) SetMessages(messages []llmschema.BaseMessage, _ bool) {
	m.messages = append([]llmschema.BaseMessage(nil), messages...)
}

// PopMessages 从尾部弹出 n 条。
func (m *memModelContext) PopMessages(size int, _ bool) []llmschema.BaseMessage {
	if size <= 0 || size > len(m.messages) {
		size = len(m.messages)
	}
	popped := append([]llmschema.BaseMessage(nil), m.messages[len(m.messages)-size:]...)
	m.messages = m.messages[:len(m.messages)-size]
	return popped
}

// ClearMessages 清空消息。
func (m *memModelContext) ClearMessages(context.Context, bool, ...ceinterface.Option) error {
	m.messages = nil
	return nil
}

// AddMessages 追加消息。
func (m *memModelContext) AddMessages(_ context.Context, messages []llmschema.BaseMessage, _ ...ceinterface.Option) ([]llmschema.BaseMessage, error) {
	m.messages = append(m.messages, messages...)
	return m.messages, nil
}

// GetContextWindow 不支持。
func (m *memModelContext) GetContextWindow(context.Context, []llmschema.BaseMessage, []commonschema.ToolInfoInterface, int, int, ...ceinterface.Option) (*ceinterface.ContextWindow, error) {
	return nil, nil
}

// Statistic 返回空统计。
func (m *memModelContext) Statistic() *ceinterface.ContextStats { return nil }

// SessionID 返回测试会话。
func (m *memModelContext) SessionID() string { return "sess" }

// ContextID 返回测试上下文。
func (m *memModelContext) ContextID() string { return "ctx" }

// TokenCounter 未实现。
func (m *memModelContext) TokenCounter() token.TokenCounter { return nil }

// ReloaderTool 未实现。
func (m *memModelContext) ReloaderTool() tool.Tool { return nil }

// WorkspaceDir 返回空目录。
func (m *memModelContext) WorkspaceDir() string { return "" }

// SetSessionRef 忽略会话引用。
func (m *memModelContext) SetSessionRef(interfaces.SessionFacade) {}

// GetSessionRef 无会话引用。
func (m *memModelContext) GetSessionRef() interfaces.SessionFacade { return nil }

// OffloadMessages 忽略卸载。
func (m *memModelContext) OffloadMessages(string, []llmschema.BaseMessage) {}

// SaveState 返回空状态。
func (m *memModelContext) SaveState() map[string]any { return nil }

// LoadState 忽略状态。
func (m *memModelContext) LoadState(map[string]any) {}

// CompressContext 不支持。
func (m *memModelContext) CompressContext(context.Context, ...ceinterface.CompressContextOption) (*ceinterface.CompressContextResult, error) {
	return nil, nil
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// newSecurityCBC 构造挂载指定上下文与消息的 callback context。
func newSecurityCBC(messages ...llmschema.BaseMessage) (*agentinterfaces.AgentCallbackContext, *memModelContext) {
	mc := &memModelContext{messages: messages}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)
	cbc.SetModelContext(mc)
	return cbc, mc
}

// ──────────────────────────── 用例 ────────────────────────────

// TestExtractMessageContent_多种输入 覆盖消息、字符串、map 与任意值。
func TestExtractMessageContent_多种输入(t *testing.T) {
	rail := NewBaseSecurityRail()

	assert.Equal(t, "", rail.ExtractMessageContent(nil))
	assert.Equal(t, "hello", rail.ExtractMessageContent(llmschema.NewUserMessage("hello")))
	assert.Equal(t, "raw", rail.ExtractMessageContent("raw"))
	assert.Equal(t, "from-map", rail.ExtractMessageContent(map[string]any{"content": "from-map"}))
	assert.Contains(t, rail.ExtractMessageContent(map[string]any{"other": 1}), "other")
	assert.Contains(t, rail.ExtractMessageContent(42), "42")
}

// TestContainsAnyPattern_命中与未命中 任一正则命中即为真，非法正则忽略。
func TestContainsAnyPattern_命中与未命中(t *testing.T) {
	rail := NewBaseSecurityRail()

	assert.True(t, rail.ContainsAnyPattern("secret token", []string{`token`}))
	assert.True(t, rail.ContainsAnyPattern("secret token", []string{`nomatch`, `secret`}))
	assert.False(t, rail.ContainsAnyPattern("public", []string{`secret`}))
	assert.False(t, rail.ContainsAnyPattern("x", []string{`[`}), "非法正则不得 panic")
	assert.False(t, rail.ContainsAnyPattern("x", nil))
}

// TestPopMatchingMessages_移除命中消息 命中正则的消息被移出上下文。
func TestPopMatchingMessages_移除命中消息(t *testing.T) {
	rail := NewBaseSecurityRail()
	cbc, mc := newSecurityCBC(
		llmschema.NewUserMessage("keep me"),
		llmschema.NewUserMessage("api_key=sk-123"),
	)

	popped := rail.PopMatchingMessages(cbc, []string{`api_key`}, false)
	require.Len(t, popped, 1)
	assert.Equal(t, "api_key=sk-123", popped[0].GetContent().Text())
	require.Len(t, mc.messages, 1)
	assert.Equal(t, "keep me", mc.messages[0].GetContent().Text())
}

// TestPopMatchingMessages_无匹配不改写 未命中时保留全部消息。
func TestPopMatchingMessages_无匹配不改写(t *testing.T) {
	rail := NewBaseSecurityRail()
	cbc, mc := newSecurityCBC(llmschema.NewUserMessage("plain"))

	assert.Empty(t, rail.PopMatchingMessages(cbc, []string{`secret`}, false))
	assert.Len(t, mc.messages, 1)
}

// TestPopLastUserMessage_弹出末尾用户消息 逆序找到 user 消息后弹出。
func TestPopLastUserMessage_弹出末尾用户消息(t *testing.T) {
	rail := NewBaseSecurityRail()
	cbc, mc := newSecurityCBC(
		llmschema.NewUserMessage("first"),
		llmschema.NewAssistantMessage("answer"),
		llmschema.NewUserMessage("last"),
	)

	popped := rail.PopLastUserMessage(cbc)
	require.Len(t, popped, 1)
	require.Len(t, mc.messages, 2, "只弹出最后一条消息")
}

// TestPopLastUserMessage_无用户消息 只有助手消息时返回空。
func TestPopLastUserMessage_无用户消息(t *testing.T) {
	rail := NewBaseSecurityRail()
	cbc, _ := newSecurityCBC(llmschema.NewAssistantMessage("only assistant"))

	assert.Empty(t, rail.PopLastUserMessage(cbc))
}

// TestSanitizeMatchingMessages_替换命中内容 命中片段被替换为占位符。
func TestSanitizeMatchingMessages_替换命中内容(t *testing.T) {
	rail := NewBaseSecurityRail()
	cbc, mc := newSecurityCBC(
		llmschema.NewUserMessage("token=abc123"),
		llmschema.NewUserMessage("harmless"),
	)

	sanitized := rail.SanitizeMatchingMessages(cbc, []string{`abc\d+`}, "[REDACTED]", false)
	require.Len(t, sanitized, 1)
	require.Len(t, mc.messages, 2)
	assert.Equal(t, "token=[REDACTED]", mc.messages[0].GetContent().Text())
	assert.Equal(t, "harmless", mc.messages[1].GetContent().Text())
}

// TestPopHelpers_无上下文 未挂载 ModelContext 时安全返回空。
func TestPopHelpers_无上下文(t *testing.T) {
	rail := NewBaseSecurityRail()
	cbc := agentinterfaces.NewAgentCallbackContext(nil, &agentinterfaces.InvokeInputs{}, nil)

	assert.Empty(t, rail.PopLastUserMessage(cbc))
	assert.Empty(t, rail.PopMatchingMessages(cbc, []string{"x"}, false))
	assert.Empty(t, rail.SanitizeMatchingMessages(cbc, []string{"x"}, "", false))
}
