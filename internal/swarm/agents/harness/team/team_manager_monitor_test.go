package team

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestEnsureMonitor_已有运行中handler直接返回 验证幂等短路分支。
func TestEnsureMonitor_已有运行中handler直接返回(t *testing.T) {
	m := NewTeamManager()
	fake := newFakeMonitorHandler()
	m.teamMonitors["sess-1"] = fake

	if err := m.EnsureMonitor(context.Background(), "sess-1", "team-a", false); err != nil {
		t.Fatalf("已有运行中 handler 时应返回 nil，实际: %v", err)
	}
	if m.teamMonitors["sess-1"] != TeamMonitorHandler(fake) {
		t.Fatal("已有运行中 handler 不应被替换")
	}
}

// TestEnsureMonitor_运行时池无条目返回nil 验证 pool 未命中分支。
// 未激活的 (team, session) 在 runtime 池中查不到条目，GetAgentTeamMonitor
// 返回 (nil, nil)。T-10 之后 EnsureMonitor 返回哨兵 errNoActiveRuntime，
// 由调用方决定是否只是接受（两个生产调用方都只记 Warn 不中断）。
func TestEnsureMonitor_运行时池无条目返回nil(t *testing.T) {
	m := NewTeamManager()

	err := m.EnsureMonitor(context.Background(), "sess-not-active", "team-not-active", false)
	if !errors.Is(err, ErrNoActiveRuntime) {
		t.Fatalf("运行时池无条目时应返回 ErrNoActiveRuntime，实际: %v", err)
	}
	if m.teamMonitors["sess-not-active"] != nil {
		t.Fatal("未启动 monitor 时不应注册 handler")
	}
}

// TestConsumeMonitorEvents_广播事件 验证 monitor 事件经回调广播出去。
func TestConsumeMonitorEvents_广播事件(t *testing.T) {
	m := NewTeamManager()

	got := make(chan string, 4)
	m.onEventBroadcast = func(_, _ string, event map[string]any) {
		if v, ok := event["event_type"].(string); ok {
			got <- v
		}
	}

	// 先入队再关闭：consumeMonitorEvents 同步 range，通道关闭后自然返回
	fake := newFakeMonitorHandler()
	fake.eventsCh <- map[string]any{"event_type": "team.member"}
	close(fake.eventsCh)

	m.consumeMonitorEvents(context.Background(), "sess-1", fake)

	select {
	case v := <-got:
		if v != "team.member" {
			t.Fatalf("广播事件类型应为 team.member，实际: %s", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("未在超时内收到广播事件")
	}
}
