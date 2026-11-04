package iface

import (
	"testing"

	"github.com/stretchr/testify/require"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestWithWindowMutators_追加语义 验证多次注册按序累积。
func TestWithWindowMutators_追加语义(t *testing.T) {
	first := func(w *ContextWindow) *ContextWindow { return w }
	second := func(w *ContextWindow) *ContextWindow { return w }

	got := WindowMutatorsFromOption(
		WithWindowMutators(first),
		WithWindowMutators(second),
	)
	require.Len(t, got, 2, "两次注册应累积而不是覆盖")
}

// TestWindowMutatorsFromOption_无选项 验证空选项返回 nil。
func TestWindowMutatorsFromOption_无选项(t *testing.T) {
	require.Nil(t, WindowMutatorsFromOption())
	require.Empty(t, WindowMutatorsFromOption(WithWindowMutators()))
}

// TestWindowMutatorsFromOption_Extra已存在其他键 验证不覆盖 Extra。
func TestWindowMutatorsFromOption_Extra已存在其他键(t *testing.T) {
	po := NewProcessorOption(func(o *ProcessorOption) {
		o.Extra = map[string]any{"keep": "v"}
	}, WithWindowMutators(func(w *ContextWindow) *ContextWindow { return w }))
	require.Equal(t, "v", po.Extra["keep"])
	require.Len(t, po.Extra[WindowMutatorsExtraKey].([]WindowMutator), 1)
}

// TestWindowMutator_类型可赋值 验证投影函数签名与消息替换配合。
func TestWindowMutator_类型可赋值(t *testing.T) {
	var mutate WindowMutator = func(w *ContextWindow) *ContextWindow {
		cp := *w
		cp.ContextMessages = append([]llmschema.BaseMessage{}, w.ContextMessages...)
		cp.ContextMessages = append(cp.ContextMessages, llmschema.NewUserMessage("x"))
		return &cp
	}
	window := NewContextWindow()
	out := mutate(window)
	require.NotNil(t, out)
	require.Len(t, out.ContextMessages, 1)
	require.Empty(t, window.ContextMessages, "不得修改入参")
}
