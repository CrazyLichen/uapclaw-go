package agent

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/spawn"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeForkSource 可注入的 fork 源（SpawnableAgent + ForkContextSource 双面）。
type fakeForkSource struct {
	msgs []llmschema.BaseMessage
}

func (f *fakeForkSource) AgentCard() *agentschema.AgentCard { return nil }
func (f *fakeForkSource) GetCurrentContext(_ context.Context, _, _ string) ([]llmschema.BaseMessage, error) {
	return f.msgs, nil
}

// forkDispatchMessages 构造五分派用消息：[user, assistant(带 tool_calls), tool, assistant]。
func forkDispatchMessages() []llmschema.BaseMessage {
	toolCall := llmschema.NewToolCall("call-1", "read_file", "{}")
	return []llmschema.BaseMessage{
		llmschema.NewUserMessage("u1"),
		llmschema.NewAssistantMessage("a1", llmschema.WithToolCalls([]*llmschema.ToolCall{toolCall})),
		llmschema.NewToolMessage("call-1", "t1"),
		llmschema.NewAssistantMessage("a2"),
	}
}

// newForkDispatchAgent 构造分派测试用 TeamAgent，并把名为 source 的队友
// 句柄塞进 spawnedHandles（fake 源）。
func newForkDispatchAgent(t *testing.T, source string, msgs []llmschema.BaseMessage) *TeamAgent {
	t.Helper()
	a := newTestTeamAgent(t)
	handle := spawn.NewInProcessSpawnHandle("pid-1", nil, make(chan struct{}), &fakeForkSource{msgs: msgs})
	a.spawnManager.spawnedHandles[source] = handle
	return a
}

// ──────────────────────────── 测试 ────────────────────────────

// TestBuildForkContext_LiveForkFullContext 验证 live fork（fork=true）忽略
// 非 full mode 并返回全量上下文。Python: team_agent.py:1551-1555
func TestBuildForkContext_LiveForkFullContext(t *testing.T) {
	a := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: true, Source: "alice", ForkMode: "before"})
	require.NotNil(t, forkCtx)
	require.Len(t, forkCtx.Messages, 4) // 全量，mode 被忽略
	require.Nil(t, forkCtx.CompactSplit)
}

// TestBuildForkContext_NamedBefore 验证 named fork before 模式：按 checkpoint
// 下标截断并跨边界携带闭合 ToolMessage。Python: team_agent.py:1571-1574
func TestBuildForkContext_NamedBefore(t *testing.T) {
	a := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	require.Nil(t, a.SetCheckpoint("code-ready", 2, "", "alice"))
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "code-ready", Source: "alice", ForkMode: "before"})
	require.NotNil(t, forkCtx)
	// u1 + a1 + 跨边界补入的 t1 = 3 条
	require.Len(t, forkCtx.Messages, 3)
	require.Nil(t, forkCtx.CompactSplit)
}

// TestBuildForkContext_NamedAfter 验证 after 模式：剥前导孤儿 ToolMessage。
// Python: team_agent.py:1575-1578
func TestBuildForkContext_NamedAfter(t *testing.T) {
	a := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	require.Nil(t, a.SetCheckpoint("code-ready", 2, "", "alice"))
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "code-ready", Source: "alice", ForkMode: "after"})
	require.NotNil(t, forkCtx)
	require.Len(t, forkCtx.Messages, 1) // 仅 a2
}

// TestBuildForkContext_KeepBeforeCompactAfter 验证 compact 模式：全量抓取 +
// compact_split/direction 标注。Python: team_agent.py:1580-1588
func TestBuildForkContext_KeepBeforeCompactAfter(t *testing.T) {
	a := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	require.Nil(t, a.SetCheckpoint("code-ready", 2, "", "alice"))
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "code-ready", Source: "alice", ForkMode: "keep_before_compact_after"})
	require.NotNil(t, forkCtx)
	require.Len(t, forkCtx.Messages, 4) // 全量
	require.NotNil(t, forkCtx.CompactSplit)
	require.Equal(t, 2, *forkCtx.CompactSplit)
	require.Equal(t, "after", forkCtx.CompactDirection)
}

// TestBuildForkContext_KeepAfterCompactBefore 验证另一 compact 模式方向默认 before。
func TestBuildForkContext_KeepAfterCompactBefore(t *testing.T) {
	a := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	require.Nil(t, a.SetCheckpoint("code-ready", 2, "", "alice"))
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "code-ready", Source: "alice", ForkMode: "keep_after_compact_before"})
	require.NotNil(t, forkCtx)
	require.NotNil(t, forkCtx.CompactSplit)
	require.Equal(t, 2, *forkCtx.CompactSplit)
	require.Equal(t, "before", forkCtx.CompactDirection)
}

// TestBuildForkContext_未知名降级Full 验证未知名回退全量。Python: :1571-1579
func TestBuildForkContext_未知名降级Full(t *testing.T) {
	a := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "no-such-cp", Source: "alice", ForkMode: "before"})
	require.NotNil(t, forkCtx)
	require.Len(t, forkCtx.Messages, 4) // full 兜底
}

// TestBuildForkContext_Creator不匹配降级Full 验证 creator/fork_source 不匹配时
// 下标清空降级 full。Python: team_agent.py:1519-1545
func TestBuildForkContext_Creator不匹配降级Full(t *testing.T) {
	a := newForkDispatchAgent(t, "bob", forkDispatchMessages())
	// checkpoint 由 alice 创建，fork_source 却是 bob
	require.Nil(t, a.SetCheckpoint("code-ready", 2, "", "alice"))
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "code-ready", Source: "bob", ForkMode: "before"})
	require.NotNil(t, forkCtx)
	require.Len(t, forkCtx.Messages, 4) // full 兜底
	require.Nil(t, forkCtx.CompactSplit)
}

// TestBuildForkContext_未知名默认ModeBefore 验证 named fork 未显式给 fork_mode
// 时默认 before（Python :1508-1509）。用全量形态差异间接验证：before 在
// checkpoint=1 时保留 [u1]（+闭合块）。
func TestBuildForkContext_Named默认ModeBefore(t *testing.T) {
	a := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	require.Nil(t, a.SetCheckpoint("cp1", 1, "", "alice"))
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "cp1", Source: "alice"}) // ForkMode 空
	require.NotNil(t, forkCtx)
	// before@1：u1；checkpoint 落在 assistant(带 tool_calls) 之前，无闭合块可补
	require.Len(t, forkCtx.Messages, 1)
}

// TestBuildForkContext_抓取失败返回Nil 验证源读取失败时经 recover 收敛为 nil
// （无继承继续 spawn）。Python: fork.py:65 raise + team_agent.py:1618-1622 except。
func TestBuildForkContext_抓取失败返回Nil(t *testing.T) {
	a := newTestTeamAgent(t)
	// 源=自己（默认），但 Harness 未装配 → GetCurrentContext 报错 → 兜底 nil
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: true, Source: "", ForkMode: "full"})
	require.Nil(t, forkCtx)
}
