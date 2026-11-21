package agent

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

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

// newForkDispatchAgent 构造分派测试用 TeamAgent + 注入 fake 读取缝
//（forkContextReader 为具体闭包结构体，测试直接注入 fake 读取函数——
// 不再经 spawnedHandles/接口断言链）。
func newForkDispatchAgent(t *testing.T, source string, msgs []llmschema.BaseMessage) (*TeamAgent, *forkContextReader) {
	t.Helper()
	a := newTestTeamAgent(t)
	reader := &forkContextReader{
		read: func(_ context.Context) ([]llmschema.BaseMessage, error) {
			return msgs, nil
		},
		persisted: func() []llmschema.BaseMessage { return nil },
	}
	return a, reader
}

// ──────────────────────────── 测试 ────────────────────────────

// TestBuildForkContext_LiveForkFullContext 验证 live fork（fork=true）忽略
// 非 full mode 并返回全量上下文。Python: team_agent.py:1551-1555
func TestBuildForkContext_LiveForkFullContext(t *testing.T) {
	a, reader := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: true, Source: "alice", ForkMode: "before"}, reader)
	require.NotNil(t, forkCtx)
	require.Len(t, forkCtx.Messages, 4) // 全量，mode 被忽略
	require.Nil(t, forkCtx.CompactSplit)
}

// TestBuildForkContext_NamedBefore 验证 named fork before 模式：按 checkpoint
// 下标截断并跨边界携带闭合 ToolMessage。Python: team_agent.py:1571-1574
func TestBuildForkContext_NamedBefore(t *testing.T) {
	a, reader := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	require.Nil(t, a.SetCheckpoint("code-ready", 2, "", "alice"))
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "code-ready", Source: "alice", ForkMode: "before"}, reader)
	require.NotNil(t, forkCtx)
	// u1 + a1 + 跨边界补入的 t1 = 3 条
	require.Len(t, forkCtx.Messages, 3)
	require.Nil(t, forkCtx.CompactSplit)
}

// TestBuildForkContext_NamedAfter 验证 after 模式：剥前导孤儿 ToolMessage。
// Python: team_agent.py:1575-1578
func TestBuildForkContext_NamedAfter(t *testing.T) {
	a, reader := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	require.Nil(t, a.SetCheckpoint("code-ready", 2, "", "alice"))
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "code-ready", Source: "alice", ForkMode: "after"}, reader)
	require.NotNil(t, forkCtx)
	require.Len(t, forkCtx.Messages, 1) // 仅 a2
}

// TestBuildForkContext_KeepBeforeCompactAfter 验证 compact 模式：全量抓取 +
// compact_split/direction 标注。Python: team_agent.py:1580-1588
func TestBuildForkContext_KeepBeforeCompactAfter(t *testing.T) {
	a, reader := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	require.Nil(t, a.SetCheckpoint("code-ready", 2, "", "alice"))
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "code-ready", Source: "alice", ForkMode: "keep_before_compact_after"}, reader)
	require.NotNil(t, forkCtx)
	require.Len(t, forkCtx.Messages, 4) // 全量
	require.NotNil(t, forkCtx.CompactSplit)
	require.Equal(t, 2, *forkCtx.CompactSplit)
	require.Equal(t, "after", forkCtx.CompactDirection)
}

// TestBuildForkContext_KeepAfterCompactBefore 验证另一 compact 模式方向默认 before。
func TestBuildForkContext_KeepAfterCompactBefore(t *testing.T) {
	a, reader := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	require.Nil(t, a.SetCheckpoint("code-ready", 2, "", "alice"))
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "code-ready", Source: "alice", ForkMode: "keep_after_compact_before"}, reader)
	require.NotNil(t, forkCtx)
	require.NotNil(t, forkCtx.CompactSplit)
	require.Equal(t, 2, *forkCtx.CompactSplit)
	require.Equal(t, "before", forkCtx.CompactDirection)
}

// TestBuildForkContext_未知名降级Full 验证未知名回退全量。Python: :1571-1579
func TestBuildForkContext_未知名降级Full(t *testing.T) {
	a, reader := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "no-such-cp", Source: "alice", ForkMode: "before"}, reader)
	require.NotNil(t, forkCtx)
	require.Len(t, forkCtx.Messages, 4) // full 兜底
}

// TestBuildForkContext_Creator不匹配降级Full 验证 creator/fork_source 不匹配时
// 下标清空降级 full。Python: team_agent.py:1519-1545
func TestBuildForkContext_Creator不匹配降级Full(t *testing.T) {
	a, reader := newForkDispatchAgent(t, "bob", forkDispatchMessages())
	// checkpoint 由 alice 创建，fork_source 却是 bob
	require.Nil(t, a.SetCheckpoint("code-ready", 2, "", "alice"))
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "code-ready", Source: "bob", ForkMode: "before"}, reader)
	require.NotNil(t, forkCtx)
	require.Len(t, forkCtx.Messages, 4) // full 兜底
	require.Nil(t, forkCtx.CompactSplit)
}

// TestBuildForkContext_Named默认ModeBefore 验证 named fork 未显式给 fork_mode
// 时默认 before（Python :1508-1509）。用全量形态差异间接验证：before 在
// checkpoint=1 时保留 [u1]（+闭合块）。
func TestBuildForkContext_Named默认ModeBefore(t *testing.T) {
	a, reader := newForkDispatchAgent(t, "alice", forkDispatchMessages())
	require.Nil(t, a.SetCheckpoint("cp1", 1, "", "alice"))
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: "cp1", Source: "alice"}, reader) // ForkMode 空
	require.NotNil(t, forkCtx)
	// before@1：u1；checkpoint 落在 assistant(带 tool_calls) 之前，无闭合块可补
	require.Len(t, forkCtx.Messages, 1)
}

// TestBuildForkContext_抓取失败返回Nil 验证源读取失败、兜底也无果时经 recover
// 收敛为 nil（无继承继续 spawn）。
// Python: fork.py:65 raise + team_agent.py:1618-1622 except。
func TestBuildForkContext_抓取失败返回Nil(t *testing.T) {
	a := newTestTeamAgent(t)
	reader := &forkContextReader{
		read:      func(_ context.Context) ([]llmschema.BaseMessage, error) { return nil, errors.New("pool 未懒加载") },
		persisted: func() []llmschema.BaseMessage { return nil },
	}
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: true, Source: "", ForkMode: "full"}, reader)
	require.Nil(t, forkCtx)
}

// TestBuildForkContext_抓取失败回落持久化 验证内存读取失败时回落兜底消息——
// fork 继承依然生效。Python: fork.py:57-64 except → _read_persisted_messages。
func TestBuildForkContext_抓取失败回落持久化(t *testing.T) {
	a := newTestTeamAgent(t)
	persisted := forkDispatchMessages()
	reader := &forkContextReader{
		read:      func(_ context.Context) ([]llmschema.BaseMessage, error) { return nil, errors.New("pool 未懒加载") },
		persisted: func() []llmschema.BaseMessage { return persisted },
	}
	forkCtx := a.buildForkContext(context.Background(), "mate",
		&tools.ForkInfo{Fork: true, Source: "", ForkMode: "full"}, reader)
	require.NotNil(t, forkCtx)
	require.Len(t, forkCtx.Messages, 4)
}

// TestResolveForkSource_自己 reader 构造验证：source 为空/等于自己 → leader
// 自身读取缝。Python: _resolve_fork_native 的 self 分支（:1645-1647）。
func TestResolveForkSource_自己(t *testing.T) {
	a := newTestTeamAgent(t)
	require.NotNil(t, a.resolveForkSource(""))
	require.NotNil(t, a.resolveForkSource(a.MemberName()))
}
