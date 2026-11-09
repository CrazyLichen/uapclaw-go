package browser_move

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 用例 ────────────────────────────

// TestBuildBrowserRuntimeTools_工具集 七个运行时工具卡片齐全且不可流式调用。
func TestBuildBrowserRuntimeTools_工具集(t *testing.T) {
	runtime := NewBrowserAgentRuntime("", "", "", "", nil, nil)
	tools := BuildBrowserRuntimeTools(runtime)
	require.Len(t, tools, 7)

	names := make(map[string]bool, len(tools))
	for _, tl := range tools {
		card := tl.Card()
		require.NotNil(t, card, "工具必须有卡片")
		assert.NotEmpty(t, card.Name)
		names[card.Name] = true

		assert.Greater(t, tl.GetCallTimeout(map[string]any{}, 30), float64(0), "%s 预算必须为正", card.Name)

		ch, err := tl.Stream(context.Background(), map[string]any{})
		require.ErrorIs(t, err, tool.ErrStreamNotSupported, "%s 不支持流式", card.Name)
		assert.Nil(t, ch)
	}

	for _, want := range []string{
		"browser_cancel_run",
		"browser_clear_cancel",
		"browser_custom_action",
		"browser_list_custom_actions",
		"browser_probe_interactives",
		"browser_probe_cards",
		"browser_runtime_health",
	} {
		assert.True(t, names[want], "缺少工具卡片 %s（实际：%v）", want, names)
	}
}

// TestBrowserListActionsTool_列出动作 未连接浏览器时也能返回动作清单结构。
func TestBrowserListActionsTool_列出动作(t *testing.T) {
	runtime := NewBrowserAgentRuntime("", "", "", "", nil, nil)
	tl := NewBrowserListActionsTool(runtime)

	result, err := tl.Invoke(context.Background(), map[string]any{})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// TestBrowserClearCancelTool_清除标记 无会话时清除取消标记不报错。
func TestBrowserClearCancelTool_清除标记(t *testing.T) {
	runtime := NewBrowserAgentRuntime("", "", "", "", nil, nil)
	tl := NewBrowserClearCancelTool(runtime)

	result, err := tl.Invoke(context.Background(), map[string]any{
		"session_id": "sess-1",
		"request_id": "req-1",
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}
