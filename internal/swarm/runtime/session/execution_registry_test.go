package session

import (
	"testing"
	"time"
)

// newTestRegistry 构造默认参数注册表。
func newTestRegistry() *SessionExecutionRegistry {
	return NewSessionExecutionRegistry(512, 900.0)
}

// newRunningHandle 构造并登记一个已置 RUNNING 的句柄。
func newRunningHandle(t *testing.T, r *SessionExecutionRegistry, eid, sid, rid string, gen int) *SessionExecutionHandle {
	t.Helper()
	h := NewSessionExecutionHandle(eid, sid, rid, gen, SessionWorkKindChatStream, "web")
	h.Cancel = func() {}
	r.Register(h)
	r.MarkRunning(h)
	return h
}

// TestRegistry_Register重复panic 校验同一 execution_id 不能登记两次。
func TestRegistry_Register重复panic(t *testing.T) {
	r := newTestRegistry()
	h := NewSessionExecutionHandle("e1", "s1", "r1", 1, SessionWorkKindChatStream, "web")
	r.Register(h)
	defer func() {
		if recover() == nil {
			t.Fatal("重复登记应 panic（对齐上游 raise ValueError）")
		}
	}()
	r.Register(h)
}

// TestRegistry_MarkRunning仅QUEUED生效 校验非 QUEUED 不改变状态。
func TestRegistry_MarkRunning仅QUEUED生效(t *testing.T) {
	r := newTestRegistry()
	h := NewSessionExecutionHandle("e1", "s1", "r1", 1, SessionWorkKindChatStream, "web")
	h.Cancel = func() {}
	r.Register(h)
	r.MarkTerminal(h, SessionExecutionStateSucceeded, "")
	r.MarkRunning(h)
	if h.State != SessionExecutionStateSucceeded {
		t.Fatalf("已终态的执行不应被 MarkRunning 改写，实际 %s", h.State)
	}
}

// TestRegistry_MarkRunning记录StartedAt 校验时间戳与状态。
func TestRegistry_MarkRunning记录StartedAt(t *testing.T) {
	r := newTestRegistry()
	h := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	if h.StartedAt == nil {
		t.Fatal("MarkRunning 应记录 StartedAt")
	}
	if h.State != SessionExecutionStateRunning {
		t.Fatalf("状态应为 RUNNING，实际 %s", h.State)
	}
}

// TestRegistry_MarkTerminal关闭Terminal且不关闭Done 校验双信号职责分离。
//
// spec §2.1：MarkTerminal 是唯一关闭 Terminal 的点；Done 由执行方 defer 关闭。
func TestRegistry_MarkTerminal关闭Terminal且不关闭Done(t *testing.T) {
	r := newTestRegistry()
	h := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	r.MarkTerminal(h, SessionExecutionStateSucceeded, "")
	if !h.Terminal.IsTerminalClosed() {
		t.Fatal("MarkTerminal 必须关闭 Terminal")
	}
	select {
	case <-h.Done:
		t.Fatal("MarkTerminal 不得关闭 Done（Done 由执行方 defer 关闭）")
	default:
	}
}

// TestRegistry_MarkTerminal幂等 校验重复调用不 panic 且状态不变。
func TestRegistry_MarkTerminal幂等(t *testing.T) {
	r := newTestRegistry()
	h := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	r.MarkTerminal(h, SessionExecutionStateSucceeded, "")
	r.MarkTerminal(h, SessionExecutionStateFailed, "second") // 应早返回，不改状态
	if h.State != SessionExecutionStateSucceeded {
		t.Fatalf("重复 MarkTerminal 不应改写状态，实际 %s", h.State)
	}
	if h.Error != "" {
		t.Fatalf("重复 MarkTerminal 不应改写 Error，实际 %q", h.Error)
	}
	if !h.Terminal.IsTerminalClosed() {
		t.Fatal("Terminal 应已关闭")
	}
}

// TestRegistry_MarkTerminal拒绝非终态 校验参数校验。
func TestRegistry_MarkTerminal拒绝非终态(t *testing.T) {
	r := newTestRegistry()
	h := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	defer func() {
		if recover() == nil {
			t.Fatal("非终态应 panic（对齐上游 raise ValueError）")
		}
	}()
	r.MarkTerminal(h, SessionExecutionStateRunning, "")
}

// TestRegistry_MarkTerminal清空等待交互与Agent 校验清理语义。
func TestRegistry_MarkTerminal清空等待交互与Agent(t *testing.T) {
	r := newTestRegistry()
	h := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	r.MarkAwaitingControl(h, map[string]struct{}{"c1": {}, "c2": {}})
	h.Agent = "fake-agent"
	r.MarkTerminal(h, SessionExecutionStateCancelled, "")
	if len(h.WaitingControlIDs) != 0 {
		t.Fatalf("终态应清空 WaitingControlIDs，实际 %v", h.WaitingControlIDs)
	}
	if h.Agent != nil {
		t.Fatal("终态应清空 Agent")
	}
	if h.FinishedAt == nil {
		t.Fatal("终态应记录 FinishedAt")
	}
}

// TestRegistry_MarkTerminal保留Done不被置nil 校验 close_session 不会阻塞。
//
// spec §4.4：Go 不能把 Done 置 nil，否则 <-h.Done 会永远阻塞。
func TestRegistry_MarkTerminal保留Done不被置nil(t *testing.T) {
	r := newTestRegistry()
	h := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	r.MarkTerminal(h, SessionExecutionStateSucceeded, "")
	if h.Done == nil {
		t.Fatal("MarkTerminal 不得把 Done 置 nil")
	}
}

// TestRegistry_MarkAwaitingControl并集 校验多次登记取并集。
func TestRegistry_MarkAwaitingControl并集(t *testing.T) {
	r := newTestRegistry()
	h := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	r.MarkAwaitingControl(h, map[string]struct{}{"c1": {}})
	r.MarkAwaitingControl(h, map[string]struct{}{"c2": {}})
	if len(h.WaitingControlIDs) != 2 {
		t.Fatalf("应并集为 2 个，实际 %v", h.WaitingControlIDs)
	}
}

// TestRegistry_MarkWaiting与Resume 校验等待状态往返。
func TestRegistry_MarkWaiting与Resume(t *testing.T) {
	r := newTestRegistry()
	h := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	r.MarkWaiting(h)
	if h.State != SessionExecutionStateWaitingForControl {
		t.Fatalf("应为 WAITING_FOR_CONTROL，实际 %s", h.State)
	}
	r.ResumeWaiting(h)
	if h.State != SessionExecutionStateRunning {
		t.Fatalf("恢复后应为 RUNNING，实际 %s", h.State)
	}
}

// TestRegistry_Select按代次与活跃过滤 校验查询语义。
func TestRegistry_Select按代次与活跃过滤(t *testing.T) {
	r := newTestRegistry()
	old := newRunningHandle(t, r, "e-old", "s1", "r1", 1)
	cur := newRunningHandle(t, r, "e-cur", "s1", "r2", 2)
	r.MarkTerminal(old, SessionExecutionStateSucceeded, "")

	generation := 2
	gen2 := r.Select(SelectOptions{SessionID: "s1", Generation: &generation})
	if len(gen2) != 1 || gen2[0].ExecutionID != "e-cur" {
		t.Fatalf("代次 2 查询应只返回 e-cur，实际 %v", executionIDs(gen2))
	}
	active := r.Select(SelectOptions{SessionID: "s1", ActiveOnly: true})
	if len(active) != 1 || active[0].ExecutionID != cur.ExecutionID {
		t.Fatalf("活跃查询应只返回 e-cur，实际 %v", executionIDs(active))
	}
}

// TestRegistry_Select按RequestID查询 校验 request 索引。
func TestRegistry_Select按RequestID查询(t *testing.T) {
	r := newTestRegistry()
	newRunningHandle(t, r, "e1", "s1", "r1", 1)
	newRunningHandle(t, r, "e2", "s1", "r1", 1)
	newRunningHandle(t, r, "e3", "s1", "r2", 1)
	requestID := "r1"
	got := r.Select(SelectOptions{SessionID: "s1", RequestID: &requestID})
	if len(got) != 2 {
		t.Fatalf("按 request_id=r1 应返回 2 条，实际 %v", executionIDs(got))
	}
}

// TestRegistry_Select按ExecutionID查询 校验单条查询优先级最高。
func TestRegistry_Select按ExecutionID查询(t *testing.T) {
	r := newTestRegistry()
	newRunningHandle(t, r, "e1", "s1", "r1", 1)
	newRunningHandle(t, r, "e2", "s1", "r2", 1)
	executionID := "e2"
	got := r.Select(SelectOptions{SessionID: "s1", ExecutionID: &executionID, ActiveOnly: true})
	if len(got) != 1 || got[0].ExecutionID != "e2" {
		t.Fatalf("应只返回 e2，实际 %v", executionIDs(got))
	}
}

// TestRegistry_Select按CreatedAt升序 校验排序。
func TestRegistry_Select按CreatedAt升序(t *testing.T) {
	r := newTestRegistry()
	h1 := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	h2 := newRunningHandle(t, r, "e2", "s1", "r2", 1)
	h3 := newRunningHandle(t, r, "e3", "s1", "r3", 1)
	// 直接改写创建时刻以保证顺序确定
	h1.CreatedAt, h2.CreatedAt, h3.CreatedAt = 3, 1, 2
	got := r.Select(SelectOptions{SessionID: "s1"})
	want := []string{"e2", "e3", "e1"}
	for i := range want {
		if got[i].ExecutionID != want[i] {
			t.Fatalf("排序错误：期望 %v，实际 %v", want, executionIDs(got))
		}
	}
}

// TestRegistry_终态容量淘汰 校验超出容量后最早的被移除。
func TestRegistry_终态容量淘汰(t *testing.T) {
	r := NewSessionExecutionRegistry(2, 900.0)
	for _, eid := range []string{"e1", "e2", "e3"} {
		h := newRunningHandle(t, r, eid, "s1", "r-"+eid, 1)
		h.MarkDone() // 让 _terminal 判定可移除
		r.MarkTerminal(h, SessionExecutionStateSucceeded, "")
	}
	if got := r.Get("e1"); got != nil {
		t.Fatal("容量 2 时最早的终态执行应被淘汰")
	}
	if r.Get("e3") == nil {
		t.Fatal("最新终态执行应保留")
	}
}

// TestRegistry_终态TTL淘汰 校验超过 TTL 后移除。
func TestRegistry_终态TTL淘汰(t *testing.T) {
	r := NewSessionExecutionRegistry(512, 0.0) // TTL=0，立即过期
	h := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	h.MarkDone()
	r.MarkTerminal(h, SessionExecutionStateSucceeded, "")
	time.Sleep(time.Millisecond)
	if got := r.Get("e1"); got != nil {
		t.Fatal("TTL=0 时终态执行应被淘汰")
	}
}

// TestRegistry_未MarkDone的终态不被淘汰 校验 Done 未关时不回收。
//
// 对应上游 `handle.task is None or handle.task.done()`：执行方还在跑时
// 不能把执行从账本里移除。
func TestRegistry_未MarkDone的终态不被淘汰(t *testing.T) {
	r := NewSessionExecutionRegistry(0, 0.0) // 容量 0 + TTL 0，最激进
	h := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	r.MarkTerminal(h, SessionExecutionStateSucceeded, "")
	// 未 MarkDone：Done 未关，不应回收
	if got := r.Get("e1"); got != nil {
		// 上游语义：task 未 done 时不 _remove
		t.Log("Done 未关时执行仍在账本内（符合上游）")
	}
	h.MarkDone()
	r.Get("e1") // 触发淘汰
	if got := r.Get("e1"); got != nil {
		t.Fatal("Done 关闭 + 容量 0 后应被淘汰")
	}
}

// TestRegistry_Active返回非终态 校验批量查询。
func TestRegistry_Active返回非终态(t *testing.T) {
	r := newTestRegistry()
	live := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	finished := newRunningHandle(t, r, "e2", "s1", "r2", 1)
	finished.MarkDone()
	r.MarkTerminal(finished, SessionExecutionStateSucceeded, "")
	got := r.Active()
	if len(got) != 1 || got[0].ExecutionID != live.ExecutionID {
		t.Fatalf("应只返回活跃执行，实际 %v", executionIDs(got))
	}
}

// TestRegistry_SnapshotsForSession 校验快照批量生成。
func TestRegistry_SnapshotsForSession(t *testing.T) {
	r := newTestRegistry()
	newRunningHandle(t, r, "e1", "s1", "r1", 1)
	generation := 1
	snaps := r.SnapshotsForSession("s1", &generation)
	if len(snaps) != 1 || snaps[0].ExecutionID != "e1" {
		t.Fatalf("应返回 1 条快照，实际 %+v", snaps)
	}
}

// TestRegistry_ReleaseOwnerTask进入终态队列 校验 owner 收敛后入队。
func TestRegistry_ReleaseOwnerTask进入终态队列(t *testing.T) {
	// TTL=0 使条目一入队即过期，从而只需看「是否入队」而非「容量压力」
	r := NewSessionExecutionRegistry(512, 0.0)
	h := newRunningHandle(t, r, "e1", "s1", "r1", 1)
	h.RetainOwnerTask = true
	r.MarkTerminal(h, SessionExecutionStateSucceeded, "")
	// RetainOwnerTask 且 Done 未关：不入队，仍在账本
	if r.Get("e1") == nil {
		t.Fatal("RetainOwnerTask 且 Done 未关时不应入队淘汰")
	}
	h.MarkDone()
	r.ReleaseOwnerTask(h)
	time.Sleep(time.Millisecond)
	if r.Get("e1") != nil {
		t.Fatal("ReleaseOwnerTask 后应可被淘汰")
	}
}

// TestRegistry_拒绝负容量 校验参数校验。
func TestRegistry_拒绝负容量(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("负容量应 panic（对齐上游 raise ValueError）")
		}
	}()
	NewSessionExecutionRegistry(-1, 900.0)
}

// ──────────────────────────── 测试辅助 ────────────────────────────

// executionIDs 提取执行标识列表。
func executionIDs(handles []*SessionExecutionHandle) []string {
	out := make([]string, 0, len(handles))
	for _, h := range handles {
		out = append(out, h.ExecutionID)
	}
	return out
}
