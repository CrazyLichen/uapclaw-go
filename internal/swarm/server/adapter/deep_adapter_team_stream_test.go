package adapter

import (
	"context"
	"testing"

	agentschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestProcessTeamMessageStream_NilRequest 验证 req 为 nil 时返回已关闭的空流。
func TestProcessTeamMessageStream_NilRequest(t *testing.T) {
	d := NewDeepAdapter()
	ch, err := d.processTeamMessageStream(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if ch == nil {
		t.Fatal("应返回非 nil 通道")
	}
	// 通道应已关闭，读取立即返回零值且 ok=false
	if _, ok := <-ch; ok {
		t.Fatal("通道应已关闭")
	}
}

// TestEnsureTeamEvolutionWatcher_无rail 验证未注册 rail 时安全返回（不下发任何任务）。
func TestEnsureTeamEvolutionWatcher_无rail(t *testing.T) {
	d := NewDeepAdapter()
	assertNotPanicsAdapter(t, func() {
		d.ensureTeamEvolutionWatcher(context.Background(), "chan-nora", "sess-nora", "test")
	})
}

// TestEnsureMonitorForActiveRuntime_无活跃运行时 验证无活跃 TeamAgent 时仅告警不 panic。
func TestEnsureMonitorForActiveRuntime_无活跃运行时(t *testing.T) {
	d := NewDeepAdapter()
	assertNotPanicsAdapter(t, func() {
		d.ensureMonitorForActiveRuntime(context.Background(), "chan-nomon", "sess-nomon", "team-x", false)
	})
}

// TestProcessTeamMessageStream_非nil请求 验证正常请求下能拿到流并正常结束。
// 无活跃团队时下游会走降级路径，这里只要求不 panic 且最终关闭通道。
func TestProcessTeamMessageStream_非nil请求(t *testing.T) {
	d := NewDeepAdapter()
	sessionID := "sess-stream-1"
	channelID := "chan-stream-1"
	req := &agentschema.AgentRequest{
		SessionID: &sessionID,
		ChannelID: channelID,
	}

	ch, err := d.processTeamMessageStream(context.Background(), req, map[string]any{"query": "hi"})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if ch == nil {
		t.Fatal("应返回非 nil 通道")
	}
	// 排空通道，直到关闭（防止 goroutine 阻塞在发送上）
	drained := 0
	for range ch {
		drained++
	}
	t.Logf("stream 共产出 %d 个 chunk 后关闭", drained)
}

// assertNotPanicsAdapter 断言 fn 不 panic。
func assertNotPanicsAdapter(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("不应 panic，实际 %v", r)
		}
	}()
	fn()
}
