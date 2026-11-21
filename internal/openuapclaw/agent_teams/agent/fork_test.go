package agent_test

import (
	"testing"

	"github.com/stretchr/testify/require"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/agent"
)

// ──────────────────────────── 测试 ────────────────────────────

// forkTestMessages 构造测试消息序列：[user, assistant(带 tool_calls), tool, assistant]。
// 对齐 fork.py 截断用例的典型形态。
func forkTestMessages() []llmschema.BaseMessage {
	toolCall := llmschema.NewToolCall("call-1", "read_file", "{}")
	return []llmschema.BaseMessage{
		llmschema.NewUserMessage("u1"),
		llmschema.NewAssistantMessage("a1", llmschema.WithToolCalls([]*llmschema.ToolCall{toolCall})),
		llmschema.NewToolMessage("call-1", "t1-result"),
		llmschema.NewAssistantMessage("a2"),
	}
}

// TestForkContextFromMessages_Before跨边界携带闭合ToolMessage 验证 before 截断时
// 闭合 ToolMessage 块跨边界携带（checkpoint 落在工具调用 assistant 与其结果之间）。
// Python: fork.py:89-105
func TestForkContextFromMessages_Before跨边界携带闭合ToolMessage(t *testing.T) {
	msgs := forkTestMessages()
	checkpoint := 2
	fc := agent.ForkContextFromMessages(msgs, &checkpoint, "before")
	// u1, a1（带 tool_calls）+ 跨边界补入的 t1 → 3 条，无悬挂 tool call
	require.Len(t, fc.Messages, 3)
	decoded := fc.ToForkMessages()
	require.Len(t, decoded, 3)
	require.Equal(t, "user", decoded[0].GetRole().String())
	require.Equal(t, "assistant", decoded[1].GetRole().String())
	require.Equal(t, "tool", decoded[2].GetRole().String())
}

// TestForkContextFromMessages_After剥孤儿ToolMessage 验证 after 窗口起点的
// 孤儿 ToolMessage 被剥除（其 assistant 未被继承）。
// Python: fork.py:88 + _trim_leading_orphan_tool_messages (:119-131)
func TestForkContextFromMessages_After剥孤儿ToolMessage(t *testing.T) {
	msgs := forkTestMessages()
	checkpoint := 2
	fc := agent.ForkContextFromMessages(msgs, &checkpoint, "after")
	// 窗口 [t1(孤儿), a2] → 剥掉 t1 → 仅 a2
	require.Len(t, fc.Messages, 1)
	decoded := fc.ToForkMessages()
	require.Len(t, decoded, 1)
	require.Equal(t, "assistant", decoded[0].GetRole().String())
}

// TestForkContextFromMessages_剥SystemMessage 验证全程剥 SystemMessage
// （源角色身份不得泄漏进 fork 目标）。Python: fork.py:66
func TestForkContextFromMessages_剥SystemMessage(t *testing.T) {
	msgs := []llmschema.BaseMessage{
		llmschema.NewSystemMessage("you are alice"),
		llmschema.NewUserMessage("u1"),
	}
	fc := agent.ForkContextFromMessages(msgs, nil, "before")
	require.Len(t, fc.Messages, 1)
	require.False(t, fc.IsEmpty())
}

// TestForkContextFromMessages_无checkpoint全量 验证 checkpoint=nil 返回全量。
func TestForkContextFromMessages_无checkpoint全量(t *testing.T) {
	msgs := forkTestMessages()
	fc := agent.ForkContextFromMessages(msgs, nil, "before")
	require.Len(t, fc.Messages, 4)
}

// TestForkContextFromMessages_checkpoint越界无效 验证越界下标不截断。
func TestForkContextFromMessages_checkpoint越界无效(t *testing.T) {
	msgs := forkTestMessages()
	checkpoint := 99
	fc := agent.ForkContextFromMessages(msgs, &checkpoint, "before")
	require.Len(t, fc.Messages, 4)
	negative := -1
	fc = agent.ForkContextFromMessages(msgs, &negative, "before")
	require.Len(t, fc.Messages, 4)
}

// TestForkContextFromMessages_空消息 验证空序列 → IsEmpty。
func TestForkContextFromMessages_空消息(t *testing.T) {
	fc := agent.ForkContextFromMessages(nil, nil, "before")
	require.True(t, fc.IsEmpty())
}

// TestForkContext_RoundTrip 验证 Messages 编解码往返保真（role/content）。
// Python: to_messages (fork.py:177-179)
func TestForkContext_RoundTrip(t *testing.T) {
	msgs := []llmschema.BaseMessage{llmschema.NewUserMessage("你好，世界")}
	fc := agent.ForkContextFromMessages(msgs, nil, "before")
	decoded := fc.ToForkMessages()
	require.Len(t, decoded, 1)
	require.Equal(t, "你好，世界", decoded[0].GetContent().String())
}

// TestNormalizeMessages 验证 NormalizeMessages 的三分支：
// BaseMessage 直收 / dict decode / 其他 → nil。
// Python: normalize_messages (fork.py:150-175)
func TestNormalizeMessages(t *testing.T) {
	// BaseMessage 直收
	base := []llmschema.BaseMessage{llmschema.NewUserMessage("u1")}
	require.Len(t, agent.NormalizeForkMessages(base), 1)
	// dict decode
	dicts := []any{map[string]any{"role": "user", "content": "u1"}}
	require.Len(t, agent.NormalizeForkMessageValues(dicts), 1)
	// 非法形态 → nil
	require.Nil(t, agent.NormalizeForkMessageValues([]any{"junk"}))
}
