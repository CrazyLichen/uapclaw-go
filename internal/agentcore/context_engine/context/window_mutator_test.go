package context

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	ceiface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// newSessionModelContextForMutator 构造最小可用上下文（nil 计数器/工作区/系统操作）。
func newSessionModelContextForMutator(t *testing.T) *SessionModelContext {
	t.Helper()
	return NewSessionModelContext(
		"ctx-1", "sess-1",
		ceschema.NewContextEngineConfig(),
		nil, nil, nil, nil, nil, nil,
	)
}

// TestGetContextWindow_应用Mutator 验证投影链按序应用且只影响返回窗口。
func TestGetContextWindow_应用Mutator(t *testing.T) {
	mc := newSessionModelContextForMutator(t)
	_, err := mc.AddMessages(context.Background(), []llmschema.BaseMessage{llmschema.NewUserMessage("hi")})
	require.NoError(t, err)

	appendMutator := func(w *ceiface.ContextWindow) *ceiface.ContextWindow {
		cp := *w
		cp.ContextMessages = append([]llmschema.BaseMessage{}, w.ContextMessages...)
		cp.ContextMessages = append(cp.ContextMessages, llmschema.NewUserMessage("mutated"))
		return &cp
	}
	window, err := mc.GetContextWindow(
		context.Background(),
		[]llmschema.BaseMessage{llmschema.NewSystemMessage("sys")},
		nil, 0, 0,
		ceiface.WithWindowMutators(appendMutator),
	)
	require.NoError(t, err)
	require.NotEmpty(t, window.ContextMessages)
	require.Equal(t, "mutated", window.ContextMessages[len(window.ContextMessages)-1].GetContent().Text())

	stored, err := mc.GetMessages(0, true)
	require.NoError(t, err)
	require.Len(t, stored, 1, "投影不得写回历史")
	require.Equal(t, "hi", stored[0].GetContent().Text())
}

// TestGetContextWindow_Mutator链顺序 验证多 mutator 按传入顺序执行。
func TestGetContextWindow_Mutator链顺序(t *testing.T) {
	mc := newSessionModelContextForMutator(t)
	_, err := mc.AddMessages(context.Background(), []llmschema.BaseMessage{llmschema.NewUserMessage("hi")})
	require.NoError(t, err)

	order := []string{}
	first := func(w *ceiface.ContextWindow) *ceiface.ContextWindow {
		order = append(order, "first")
		return w
	}
	second := func(w *ceiface.ContextWindow) *ceiface.ContextWindow {
		order = append(order, "second")
		return w
	}
	_, err = mc.GetContextWindow(context.Background(), nil, nil, 0, 0,
		ceiface.WithWindowMutators(first, second))
	require.NoError(t, err)
	require.Equal(t, []string{"first", "second"}, order)
}

// TestWindowMutatorsFromOption_nil与空 验证无选项时返回 nil。
func TestWindowMutatorsFromOption_nil与空(t *testing.T) {
	require.Nil(t, ceiface.WindowMutatorsFromOption())
	require.Empty(t, ceiface.WindowMutatorsFromOption(ceiface.WithWindowMutators()))
}
