package team

import (
	"testing"
)

// team_manager_round_state_test.go R-7 四行为测试。
// 上游：team_manager.py 四方法语义一比一。

func newRoundStateTM() *TeamManager { return NewTeamManager() }

// TestSetRoundStateReporter_代次校验 reporter 转发（代次匹配才转发）。
// Python: report_round_state L453-477。
func TestSetRoundStateReporter_代次校验(t *testing.T) {
	m := newRoundStateTM()
	var calls []string
	m.SetRoundStateReporter(func(sessionID string, generation int, running bool) {
		calls = append(calls, sessionID)
	})
	gen := 2
	m.SetSessionGeneration("s1", 2)
	// 代次匹配 → 转发。
	m.ReportRoundState("s1", &gen, true)
	if len(calls) != 1 || calls[0] != "s1" {
		t.Fatalf("calls = %v", calls)
	}
	// 代次不匹配 → 不转发。
	wrongGen := 3
	m.ReportRoundState("s1", &wrongGen, false)
	if len(calls) != 1 {
		t.Fatalf("mismatched generation must not forward: %v", calls)
	}
	// generation=None → 不转发（Python report_round_state 同守卫：无执行持有
	// 调用方时无需对齐）。
	m.ReportRoundState("s1", nil, false)
	if len(calls) != 1 {
		t.Fatalf("nil generation must not forward: %v", calls)
	}
}

// TestRemoveWaiter_代次账本清理 四账本清理语义。
// Python: remove_waiter L597-624。
func TestRemoveWaiter_代次账本清理(t *testing.T) {
	m := newRoundStateTM()
	ch1 := make(chan map[string]any, 1)
	ch2 := make(chan map[string]any, 1)
	m.RegisterSessionWaiter("s1", "req-1", ch1, true, false, 1)
	m.RegisterSessionWaiter("s1", "req-2", ch2, false, true, 1)

	// 代次不匹配 → 不动作。
	wrongGen := 9
	m.RemoveWaiter("s1", "req-1", &wrongGen)
	if len(m.pendingWaitersTM["s1"]) != 2 {
		t.Fatal("mismatched generation must not remove")
	}
	// 匹配 → 清理排他 + pending。
	correctGen := 1
	m.RemoveWaiter("s1", "req-1", &correctGen)
	if len(m.pendingWaitersTM["s1"]) != 1 {
		t.Fatalf("pending after remove = %d", len(m.pendingWaitersTM["s1"]))
	}
	if _, has := m.exclusiveWaiters["s1"]; has {
		t.Fatal("exclusive waiter must be cleaned")
	}
	if _, has := m.waiterGenerations["s1\x00req-1"]; has {
		t.Fatal("waiter generation must be cleaned")
	}
	// 仅观察者清理后 attach 集合为空也清。
	m.RemoveWaiter("s1", "req-2", &correctGen)
	if _, has := m.attachWaiterIDs["s1"]; has {
		t.Fatal("empty attach set must be cleaned")
	}
	if _, has := m.pendingWaitersTM["s1"]; has {
		t.Fatal("empty pending list must be cleaned")
	}
}

// TestBroadcastEvent_送达与丢弃 事件送达非满队列；满队列丢弃。
// Python: broadcast_event。
func TestBroadcastEvent_送达与丢弃(t *testing.T) {
	m := newRoundStateTM()
	ch := make(chan map[string]any, 1)
	m.RegisterSessionWaiter("s1", "req-1", ch, true, false, 1)
	// 无等待者的会话 → false。
	if m.BroadcastEvent("s-none", "chat.chunk", map[string]any{"x": 1}) {
		t.Fatal("no waiters must return false")
	}
	// 送达 → true + event_type 注入。
	if !m.BroadcastEvent("s1", "chat.chunk", map[string]any{"delta": "hi"}) {
		t.Fatal("delivery must return true")
	}
	event := <-ch
	if event["event_type"] != "chat.chunk" || event["delta"] != "hi" {
		t.Fatalf("event = %v", event)
	}
	_ = event
	// 先占满队列，再广播 → 丢弃不阻塞，全丢弃应返回 false。
	m.BroadcastEvent("s1", "chat.chunk", map[string]any{"delta": "fill"})
	if m.BroadcastEvent("s1", "chat.chunk", map[string]any{"delta": "2"}) {
		t.Fatal("full queue drop must return false when nothing delivered")
	}
}

// TestHasInputWaiters_仅观察不翻轮 attach-only 等待者不触发 follow-up。
// Python: has_input_waiters L628-646。
func TestHasInputWaiters_仅观察不翻轮(t *testing.T) {
	m := newRoundStateTM()
	ch := make(chan map[string]any, 1)
	m.RegisterSessionWaiter("s1", "obs-1", ch, false, true, 1)
	if m.HasInputWaiters("s1") {
		t.Fatal("attach-only waiter must not count as input waiter")
	}
	chMain := make(chan map[string]any, 1)
	m.RegisterSessionWaiter("s1", "main-1", chMain, true, false, 1)
	if !m.HasInputWaiters("s1") {
		t.Fatal("exclusive waiter must count")
	}
}

// TestIsGenerationRunning_代次运行判定 代次匹配 + active session 判定。
// Python: is_generation_running L3008-3012。
func TestIsGenerationRunning_代次运行判定(t *testing.T) {
	m := newRoundStateTM()
	m.SetSessionGeneration("s1", 1)
	m.CommitRuntimeReady("s1", "alpha")
	if !m.IsGenerationRunning("s1", 1) {
		t.Fatal("active session with matching generation must be running")
	}
	if m.IsGenerationRunning("s1", 2) {
		t.Fatal("mismatched generation must not be running")
	}
	if m.IsGenerationRunning("s-other", 1) {
		t.Fatal("other session must not be running")
	}
}
