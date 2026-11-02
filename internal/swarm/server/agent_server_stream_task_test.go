package server

import (
	"context"
	"sync/atomic"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestRegisterStreamTask_ReturnEntry 防 D1 回归：注册必须返回可比较身份的 entry。
func TestRegisterStreamTask_ReturnEntry(t *testing.T) {
	s := newTestAgentServer()
	cancelA, _ := countingCancel()
	entry := s.registerStreamTask("sess-1", "req-A", cancelA)

	s.sessionStreamTasksMu.RLock()
	cur := s.sessionStreamTasks["sess-1"]
	s.sessionStreamTasksMu.RUnlock()

	if cur != entry {
		t.Fatalf("槽位应指向 register 返回的 entry，实际 %+v", cur)
	}
	if entry.requestID != "req-A" {
		t.Fatalf("entry.requestID 应为 req-A，实际 %q", entry.requestID)
	}
}

// TestReleaseStreamTask_OnlyRemovesOwnEntry 是 D1 的核心防线：
// 旧轮收尾只能移除自己的登记，绝不能取消窗口内新注册的下一轮。
func TestReleaseStreamTask_OnlyRemovesOwnEntry(t *testing.T) {
	s := newTestAgentServer()

	cancelA, hitsA := countingCancel()
	entryA := s.registerStreamTask("sess-1", "req-A", cancelA)

	// 模拟 handleCancel：取消旧轮并删除槽位
	s.cancelStreamTask("sess-1")
	if got := atomic.LoadInt32(hitsA); got != 1 {
		t.Fatalf("cancelA 应被调用 1 次，实际 %d", got)
	}

	// 新一轮注册（resp 之后的新 chat.send）
	cancelB, hitsB := countingCancel()
	entryB := s.registerStreamTask("sess-1", "req-B", cancelB)

	// 旧轮收尾
	s.releaseStreamTask("sess-1", entryA)

	if got := atomic.LoadInt32(hitsB); got != 0 {
		t.Fatalf("旧轮收尾不得取消新一轮，cancelB 实际被调用 %d 次", got)
	}
	s.sessionStreamTasksMu.RLock()
	cur := s.sessionStreamTasks["sess-1"]
	s.sessionStreamTasksMu.RUnlock()
	if cur != entryB {
		t.Fatalf("槽位应仍是 B 的登记，实际 %+v", cur)
	}
}

// TestReleaseStreamTask_EmptySlotNoop 槽已被 handleCancel 删除时 release 必须无害。
func TestReleaseStreamTask_EmptySlotNoop(t *testing.T) {
	s := newTestAgentServer()
	cancelA, hitsA := countingCancel()
	entryA := s.registerStreamTask("sess-1", "req-A", cancelA)

	s.cancelStreamTask("sess-1") // 槽已空
	s.releaseStreamTask("sess-1", entryA)

	if got := atomic.LoadInt32(hitsA); got != 1 {
		t.Fatalf("release 不得再次取消，cancelA 实际被调用 %d 次", got)
	}
	if len(s.sessionStreamTasks) != 0 {
		t.Fatalf("表应为空，实际 %d 项", len(s.sessionStreamTasks))
	}
}

// TestCancelStreamTask_CancelsCurrentOnly 取消只作用于当前槽，且清理槽位。
func TestCancelStreamTask_CancelsCurrentOnly(t *testing.T) {
	s := newTestAgentServer()
	cancel1, hits1 := countingCancel()
	cancel2, hits2 := countingCancel()
	s.registerStreamTask("sess-1", "req-1", cancel1)
	s.registerStreamTask("sess-2", "req-2", cancel2)

	s.cancelStreamTask("sess-1")

	if got := atomic.LoadInt32(hits1); got != 1 {
		t.Fatalf("sess-1 的 cancel 应被调用 1 次，实际 %d", got)
	}
	if got := atomic.LoadInt32(hits2); got != 0 {
		t.Fatalf("sess-2 的 cancel 不应被调用，实际 %d", got)
	}
	s.sessionStreamTasksMu.RLock()
	_, exists := s.sessionStreamTasks["sess-1"]
	s.sessionStreamTasksMu.RUnlock()
	if exists {
		t.Fatal("sess-1 应从表中移除")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// countingCancel 返回带调用计数的 cancel 函数。
func countingCancel() (context.CancelFunc, *int32) {
	var n int32
	return func() { atomic.AddInt32(&n, 1) }, &n
}

// TestRegisterStreamTask_OverwritesWithoutCancel 锁定单槽语义（对齐 Python
// agent_ws_server.py:1172）：同 session 后注册覆盖先注册，覆盖时不触发任何 cancel。
//
// 已知限制：被覆盖的旧流此后无法被 cancelStreamTask 选中（注册处会打 warn）。
// 本用例防止将来有人把 map 改成追加却忘了同步改 cancel 语义。
func TestRegisterStreamTask_OverwritesWithoutCancel(t *testing.T) {
	s := newTestAgentServer()

	cancelA, hitsA := countingCancel()
	entryA := s.registerStreamTask("sess-1", "req-A", cancelA)
	cancelB, hitsB := countingCancel()
	entryB := s.registerStreamTask("sess-1", "req-B", cancelB)

	if got := atomic.LoadInt32(hitsA); got != 0 {
		t.Fatalf("覆盖注册不得取消旧流，cancelA 实际被调用 %d 次", got)
	}
	if got := atomic.LoadInt32(hitsB); got != 0 {
		t.Fatalf("注册不得取消新流，cancelB 实际被调用 %d 次", got)
	}
	s.sessionStreamTasksMu.RLock()
	cur := s.sessionStreamTasks["sess-1"]
	s.sessionStreamTasksMu.RUnlock()
	if cur != entryB {
		t.Fatalf("槽位应被新 entry 覆盖，实际 %+v", cur)
	}

	// cancelStreamTask 只能停到最新的那条
	s.cancelStreamTask("sess-1")
	if got := atomic.LoadInt32(hitsB); got != 1 {
		t.Fatalf("cancelStreamTask 应取消新流，实际 %d 次", got)
	}
	if got := atomic.LoadInt32(hitsA); got != 0 {
		t.Fatalf("旧流已不可达，不应被取消，实际 %d 次", got)
	}

	// 旧流收尾是 no-op（不得误删新流的痕迹、不得 panic）
	s.releaseStreamTask("sess-1", entryA)
	if len(s.sessionStreamTasks) != 0 {
		t.Fatalf("表应为空（cancel 已删除），实际 %d 项", len(s.sessionStreamTasks))
	}
}
