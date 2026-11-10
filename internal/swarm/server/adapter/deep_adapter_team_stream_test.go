package adapter

import (
	"context"
	"testing"
	"time"

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

// TestProcessTeamMessageStream_非nil请求 验证请求被受理且取消后流能正常关闭。
//
// 契约说明（F-01，见 25.1b spec §7.1）：这条链路**不保证「流自己结束」**。
// 上游的流由协调层事件驱动收尾——persistent 团队靠 `team.completion` 的
// `is_team_completed` 上升沿（`team.py:1899-1900` 明确「无任务 → None」），
// temporary 团队靠 leader 主动调 `clean_team` 工具。空 spec / 无模型的请求
// 两边都不会自动收尾。
//
// 因此本用例断言真实契约：请求被受理、返回非 nil 流、**取消 ctx 后流关闭**
// （`consumeStreamWithQuery` 的 ctx.Done 分支 → PopStreamTask → 主循环退出
// → close(ch)）。这才是这条链路在任何输入下都成立的保证。
func TestProcessTeamMessageStream_非nil请求(t *testing.T) {
	d := NewDeepAdapter()
	sessionID := "sess-stream-1"
	channelID := "chan-stream-1"
	req := &agentschema.AgentRequest{
		SessionID: &sessionID,
		ChannelID: channelID,
	}

	ctx, cancel := context.WithCancel(context.Background())
	ch, err := d.processTeamMessageStream(ctx, req, map[string]any{"query": "hi"})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if ch == nil {
		t.Fatal("应返回非 nil 通道")
	}

	// 取消请求：模拟客户端断连，这是流结束的确定性触发点。
	cancel()

	drained := 0
	timeout := time.After(10 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				t.Logf("stream 在取消后关闭，共产出 %d 个 chunk", drained)
				return
			}
			drained++
		case <-timeout:
			t.Fatalf("取消后流未在 10s 内关闭（已产出 %d 个 chunk）", drained)
		}
	}
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
