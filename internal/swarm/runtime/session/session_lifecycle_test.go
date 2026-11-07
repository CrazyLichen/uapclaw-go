package session

import (
	"context"
	"sync"
	"testing"
)

// fakeActivityParticipant 记录调用顺序的活动参与者。
type fakeActivityParticipant struct {
	// mu 保护 events
	mu sync.Mutex
	// events 事件序列
	events []string
}

// SessionInputIntent 记录意图事件。
func (f *fakeActivityParticipant) SessionInputIntent(_ context.Context, _ SessionInputIntentEvent) (SessionInputIntentDisposition, error) {
	f.record("intent")
	return SessionInputIntentDispositionNotNeeded, nil
}

// ForegroundChanged 记录前台变更。
func (f *fakeActivityParticipant) ForegroundChanged(_ context.Context, e SessionForegroundEvent) error {
	if e.Visible {
		f.record("foreground.visible:" + e.Target.Descriptor.SessionID)
	} else {
		f.record("foreground.hidden:" + e.Target.Descriptor.SessionID)
	}
	return nil
}

// ExecutionStarted 记录执行开始。
func (f *fakeActivityParticipant) ExecutionStarted(_ context.Context, _ SessionExecutionEvent) error {
	f.record("execution.started")
	return nil
}

// ExecutionFinished 记录执行结束。
func (f *fakeActivityParticipant) ExecutionFinished(_ SessionExecutionFinishedEvent) {
	f.record("execution.finished")
}

// SessionInactive 记录会话空闲。
func (f *fakeActivityParticipant) SessionInactive(_ context.Context, _ SessionInactiveEvent) error {
	f.record("inactive")
	return nil
}

// record 追加事件。
func (f *fakeActivityParticipant) record(e string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, e)
}

// snapshot 取事件快照。
func (f *fakeActivityParticipant) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.events))
	copy(out, f.events)
	return out
}

// TestRegistry_替换与快照 校验参与者替换后快照返回新集合。
func TestRegistry_替换与快照(t *testing.T) {
	r := NewRuntimeParticipantRegistry()
	a1 := &fakeActivityParticipant{}
	a2 := &fakeActivityParticipant{}
	r.ReplaceSessionParticipants([]SessionActivityParticipant{a1}, nil)
	if got := r.SnapshotActivity(); len(got) != 1 {
		t.Fatalf("快照应返回 1 个活动参与者，实际 %d", len(got))
	}
	r.ReplaceSessionParticipants([]SessionActivityParticipant{a2}, nil)
	got := r.SnapshotActivity()
	if len(got) != 1 || got[0] != a2 {
		t.Fatal("替换后快照应返回新集合")
	}
}

// TestRegistry_快照是副本 校验修改返回值不影响内部状态。
func TestRegistry_快照是副本(t *testing.T) {
	r := NewRuntimeParticipantRegistry()
	a1 := &fakeActivityParticipant{}
	r.ReplaceSessionParticipants([]SessionActivityParticipant{a1}, nil)
	snap := r.SnapshotActivity()
	snap[0] = nil
	if got := r.SnapshotActivity(); got[0] != a1 {
		t.Fatal("快照必须是副本，修改返回值不应影响内部状态")
	}
}

// TestRegistry_记住与遗忘Target 校验 target 索引。
func TestRegistry_记住与遗忘Target(t *testing.T) {
	r := NewRuntimeParticipantRegistry()
	target := SessionLifecycleTarget{
		Descriptor: SessionDescriptor{SessionID: "s1"},
		Kind:       SessionKindAgent,
	}
	r.RememberTarget(target)
	if got := r.Target("s1"); got == nil || got.Descriptor.SessionID != "s1" {
		t.Fatal("RememberTarget 后应能取到")
	}
	r.ForgetTarget("s1")
	if got := r.Target("s1"); got != nil {
		t.Fatal("ForgetTarget 后应取不到")
	}
	r.RememberTarget(target)
	r.ClearTargets()
	if got := r.Target("s1"); got != nil {
		t.Fatal("ClearTargets 后应全部清空")
	}
}

// TestRegistry_取不存在的Target返回nil 校验空值语义。
func TestRegistry_取不存在的Target返回nil(t *testing.T) {
	r := NewRuntimeParticipantRegistry()
	if got := r.Target("nope"); got != nil {
		t.Fatalf("不存在的会话应返回 nil，实际 %+v", got)
	}
}

// TestRegistry_Target返回值是副本 校验修改不影响内部。
func TestRegistry_Target返回值是副本(t *testing.T) {
	r := NewRuntimeParticipantRegistry()
	r.RememberTarget(SessionLifecycleTarget{
		Descriptor: SessionDescriptor{SessionID: "s1", Mode: "agent"},
		Kind:       SessionKindAgent,
	})
	got := r.Target("s1")
	got.Descriptor.Mode = "team"
	if again := r.Target("s1"); again.Descriptor.Mode != "agent" {
		t.Fatal("Target 返回值必须是副本")
	}
}

// TestSessionDescriptor_字段完整 校验 7 个字段可赋值（编译期保证）。
func TestSessionDescriptor_字段完整(t *testing.T) {
	d := SessionDescriptor{
		SessionID: "s1", ChannelID: "web", Mode: "agent.plan",
		WorkMode: "work", ProjectID: "p1", ProjectDir: "/tmp/p", UserID: "u1",
	}
	if d.SessionID != "s1" || d.UserID != "u1" || d.ProjectDir != "/tmp/p" {
		t.Fatalf("SessionDescriptor 字段赋值失败: %+v", d)
	}
}

// TestRegistry_并发安全 校验 -race 下读写不冲突。
func TestRegistry_并发安全(t *testing.T) {
	r := NewRuntimeParticipantRegistry()
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r.RememberTarget(SessionLifecycleTarget{
				Descriptor: SessionDescriptor{SessionID: "s"},
				Kind:       SessionKindAgent,
			})
		}()
		go func() {
			defer wg.Done()
			_ = r.SnapshotActivity()
			_ = r.SnapshotDelete()
			_ = r.Target("s")
		}()
	}
	wg.Wait()
}
