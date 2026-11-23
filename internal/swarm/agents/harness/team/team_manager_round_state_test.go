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
	if m.BroadcastEvent("s-none", "chat.chunk", map[string]any{"x": 1}, "") {
		t.Fatal("no waiters must return false")
	}
	// 送达 → true + event_type 注入。
	if !m.BroadcastEvent("s1", "chat.chunk", map[string]any{"delta": "hi"}, "") {
		t.Fatal("delivery must return true")
	}
	event := <-ch
	if event["event_type"] != "chat.chunk" || event["delta"] != "hi" {
		t.Fatalf("event = %v", event)
	}
	_ = event
	// 先占满队列，再广播 → 丢弃不阻塞，全丢弃应返回 false。
	m.BroadcastEvent("s1", "chat.chunk", map[string]any{"delta": "fill"}, "")
	if m.BroadcastEvent("s1", "chat.chunk", map[string]any{"delta": "2"}, "") {
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

// TestBroadcastEvent_终态无等待者走PushFallback 无 waiter 终态帧走 push fallback。
// Python: broadcast_event :772-779 elif terminal → _push_event_without_waiter
func TestBroadcastEvent_终态无等待者走PushFallback(t *testing.T) {
	m := newRoundStateTM()
	var pushedChannel, pushedSession []string
	var pushedPayloads []map[string]any
	m.SetPushFallback(func(channelID, sessionID string, payload map[string]any) {
		pushedChannel = append(pushedChannel, channelID)
		pushedSession = append(pushedSession, sessionID)
		pushedPayloads = append(pushedPayloads, payload)
	})
	payload := map[string]any{"event_type": "chat.processing_status", "is_processing": false, "is_complete": true}
	if m.BroadcastEvent("s1", "chat.processing_status", payload, "xiaoya") {
		t.Fatal("no waiters must return false")
	}
	if len(pushedPayloads) != 1 {
		t.Fatalf("fallback must fire once, got %d", len(pushedPayloads))
	}
	if pushedChannel[0] != "xiaoya" || pushedSession[0] != "s1" {
		t.Fatalf("channel/session = %s/%s", pushedChannel[0], pushedSession[0])
	}
	if pushedPayloads[0]["is_complete"] != true {
		t.Fatalf("payload = %v", pushedPayloads[0])
	}
}

// TestBroadcastEvent_终态无渠道缺省Web channel 空缺省 web。
// Python: team_manager.py:907 str(channel_id or "").strip() or "web"
func TestBroadcastEvent_终态无渠道缺省Web(t *testing.T) {
	m := newRoundStateTM()
	var got string
	m.SetPushFallback(func(channelID, _ string, _ map[string]any) { got = channelID })
	m.BroadcastEvent("s1", "chat.processing_status",
		map[string]any{"event_type": "chat.processing_status", "is_processing": false, "is_complete": true}, "")
	if got != "web" {
		t.Fatalf("default channel = %s", got)
	}
}

// TestBroadcastEvent_非终态无等待者不Fallback 非 terminal 且非 workflow-terminal：不 fallback。
func TestBroadcastEvent_非终态无等待者不Fallback(t *testing.T) {
	m := newRoundStateTM()
	fired := false
	m.SetPushFallback(func(channelID, sessionID string, payload map[string]any) { fired = true })
	m.BroadcastEvent("s1", "chat.chunk", map[string]any{"delta": "hi"}, "")
	if fired {
		t.Fatal("non-terminal frame must not trigger fallback")
	}
	// 终态但已注册 waiter 且送达：也不 fallback
	ch := make(chan map[string]any, 1)
	m.RegisterSessionWaiter("s2", "req-1", ch, true, false, 1)
	m.BroadcastEvent("s2", "chat.processing_status",
		map[string]any{"event_type": "chat.processing_status", "is_processing": false, "is_complete": true}, "web")
	if fired {
		t.Fatal("delivered terminal frame must not trigger fallback")
	}
}

// TestHasInteractiveWaiter 交互持有判定（Python has_interactive_waiter :562）。
func TestHasInteractiveWaiter(t *testing.T) {
	m := newRoundStateTM()
	if m.HasInteractiveWaiter("s-hi") {
		t.Fatal("empty session must have no interactive waiter")
	}
	// 仅 exclusive：不算交互持有（exclusive 可被自动化接管）
	m.RegisterSessionWaiter("s-hi", "req-x", make(chan map[string]any, 1), true, false, 1)
	if m.HasInteractiveWaiter("s-hi") {
		t.Fatal("exclusive-only must not count as interactive")
	}
	// 非 exclusive 登记（persistent owner）→ 交互持有
	m.RegisterSessionWaiter("s-hi", "req-p", make(chan map[string]any, 1), false, false, 1)
	if !m.HasInteractiveWaiter("s-hi") {
		t.Fatal("non-exclusive waiter must count as interactive")
	}
	// attach 订阅按 Python 实现也算交互持有（any(rid != exclusive)）：
	// 它不是自动化消费者，与 persistent owner 同类
	m.RegisterSessionWaiter("s-h2", "req-a", make(chan map[string]any, 1), false, true, 1)
	if !m.HasInteractiveWaiter("s-h2") {
		t.Fatal("attach subscription must count as interactive (Python any(rid != exclusive))")
	}
}

// TestSelectBroadcastWaiters 三级择一（Python _select_broadcast_waiters :641）。
func TestSelectBroadcastWaiters(t *testing.T) {
	m := newRoundStateTM()
	exCh := make(chan map[string]any, 8)
	perCh := make(chan map[string]any, 8)
	attCh := make(chan map[string]any, 8)
	att2Ch := make(chan map[string]any, 8)
	m.RegisterSessionWaiter("s-sel", "req-ex", exCh, true, false, 1)
	m.RegisterSessionWaiter("s-sel", "req-per", perCh, false, false, 1)
	m.RegisterSessionWaiter("s-sel", "req-a1", attCh, false, true, 1)
	m.RegisterSessionWaiter("s-sel", "req-a2", att2Ch, false, true, 1)

	// 规则 1：exclusive 存在 → 只投 exclusive
	m.mu.Lock()
	picked := m.selectBroadcastWaitersLocked("s-sel")
	m.mu.Unlock()
	if len(picked) != 1 || picked[0].RequestID != "req-ex" {
		t.Fatalf("exclusive must win, got %+v", picked)
	}

	// 规则 2：无 exclusive → 非 attach 优先
	m.RemoveWaiter("s-sel", "req-ex", nil)
	m.mu.Lock()
	picked = m.selectBroadcastWaitersLocked("s-sel")
	m.mu.Unlock()
	if len(picked) != 1 || picked[0].RequestID != "req-per" {
		t.Fatalf("input waiter must win, got %+v", picked)
	}

	// 规则 3：全 attach → 只选第一条
	m.RemoveWaiter("s-sel", "req-per", nil)
	m.mu.Lock()
	picked = m.selectBroadcastWaitersLocked("s-sel")
	m.mu.Unlock()
	if len(picked) != 1 || picked[0].RequestID != "req-a1" {
		t.Fatalf("single attach must be selected, got %+v", picked)
	}
}

// TestBroadcastEvent_择一投递 端到端：exclusive 在场时 attach 不收实时帧。
func TestBroadcastEvent_择一投递(t *testing.T) {
	m := newRoundStateTM()
	exCh := make(chan map[string]any, 4)
	attCh := make(chan map[string]any, 4)
	m.RegisterSessionWaiter("s-bd", "req-ex", exCh, true, false, 1)
	m.RegisterSessionWaiter("s-bd", "req-att", attCh, false, true, 1)

	m.BroadcastEvent("s-bd", "team.task", map[string]any{"delta": 1}, "")
	if len(exCh) != 1 {
		t.Fatalf("exclusive must receive, got %d", len(exCh))
	}
	if len(attCh) != 0 {
		t.Fatal("attach must not receive while exclusive present")
	}

	// exclusive 摘除后 → 非 attach 无、仅 attach → attach 收（规则 3）
	m.RemoveWaiter("s-bd", "req-ex", nil)
	m.BroadcastEvent("s-bd", "team.task", map[string]any{"delta": 2}, "")
	if len(attCh) != 1 {
		t.Fatalf("attach must receive after exclusive gone, got %d", len(attCh))
	}
}
