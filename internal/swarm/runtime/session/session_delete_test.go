package session

import (
	"context"
	"sync"
	"testing"
)

// noopDeleteParticipant 空实现，仅用于接口形状断言。
type noopDeleteParticipant struct{}

// Name 参与者名称。
func (noopDeleteParticipant) Name() string { return "noop" }

// BeforeDelete 无操作。
func (noopDeleteParticipant) BeforeDelete(context.Context, SessionLifecycleTarget) error { return nil }

// ReleaseResources 无操作。
func (noopDeleteParticipant) ReleaseResources(context.Context, SessionLifecycleTarget) error {
	return nil
}

// DeleteFailed 无操作。
func (noopDeleteParticipant) DeleteFailed(context.Context, SessionLifecycleTarget, bool) error {
	return nil
}

// DeleteCommitted 无操作。
func (noopDeleteParticipant) DeleteCommitted(context.Context, SessionLifecycleTarget) error {
	return nil
}

// noopTeamExecutionController 空实现，仅用于接口形状断言。
type noopTeamExecutionController struct{}

// QuiesceForDelete 无操作。
func (noopTeamExecutionController) QuiesceForDelete(context.Context, SessionLifecycleTarget, string) error {
	return nil
}

// DisposeAfterResourceRelease 无操作。
func (noopTeamExecutionController) DisposeAfterResourceRelease(context.Context, SessionLifecycleTarget, string) error {
	return nil
}

// DeleteAborted 无操作。
func (noopTeamExecutionController) DeleteAborted(SessionLifecycleTarget) {}

// DeleteCommitted 无操作。
func (noopTeamExecutionController) DeleteCommitted(SessionLifecycleTarget) {}

// TestSessionDeleteResultFailure 校验失败构造器。
func TestSessionDeleteResultFailure(t *testing.T) {
	r := SessionDeleteResultFailure("s1", "NOT_FOUND", "session not found")
	if r.OK || r.SessionID != "s1" || r.ErrorCode != "NOT_FOUND" || r.ErrorMessage != "session not found" {
		t.Fatalf("Failure 字段错误: %+v", r)
	}
	if r.Deleted || r.RecoveryRequired {
		t.Fatal("Failure 默认 Deleted 与 RecoveryRequired 应为 false")
	}
}

// TestTeamDeletionGate_删除期间拒绝变更 校验 gate 语义。
func TestTeamDeletionGate_删除期间拒绝变更(t *testing.T) {
	g := NewTeamDeletionGate()
	if !g.BeginDeleteLocked("t1") {
		t.Fatal("首次 BeginDeleteLocked 应返回 true")
	}
	if g.BeginDeleteLocked("t1") {
		t.Fatal("重复 BeginDeleteLocked 应返回 false")
	}
	err := g.AssertNotDeletingLocked("t1")
	if err == nil {
		t.Fatal("删除期间 AssertNotDeletingLocked 应报错")
	}
	dtErr, ok := err.(*TeamDeletionInProgress)
	if !ok {
		t.Fatalf("应返回 TeamDeletionInProgress，实际 %T", err)
	}
	if dtErr.Code() != "TEAM_DELETING" {
		t.Fatalf("错误码应为 TEAM_DELETING，实际 %q", dtErr.Code())
	}
	want := "TEAM_DELETING: team is being permanently deleted: t1"
	if err.Error() != want {
		t.Fatalf("错误文案 = %q, want %q", err.Error(), want)
	}
	g.FinishDeleteLocked("t1")
	if err := g.AssertNotDeletingLocked("t1"); err != nil {
		t.Fatalf("删除结束后不应报错: %v", err)
	}
}

// TestTeamDeletionGate_不同团队互不影响 校验隔离性。
func TestTeamDeletionGate_不同团队互不影响(t *testing.T) {
	g := NewTeamDeletionGate()
	g.BeginDeleteLocked("t1")
	if err := g.AssertNotDeletingLocked("t2"); err != nil {
		t.Fatalf("其他团队不应受影响: %v", err)
	}
}

// TestTeamDeletionGate_MutationLock同一团队同一把 校验锁复用。
func TestTeamDeletionGate_MutationLock同一团队同一把(t *testing.T) {
	g := NewTeamDeletionGate()
	a := g.MutationLock("t1")
	b := g.MutationLock("t1")
	c := g.MutationLock("t2")
	if a != b {
		t.Fatal("同一团队应返回同一把锁")
	}
	if a == c {
		t.Fatal("不同团队应返回不同锁")
	}
}

// TestSessionDeleteLock_同会话互斥 校验同一 session_id 拿同一把锁且真正互斥。
func TestSessionDeleteLock_同会话互斥(t *testing.T) {
	a := SessionDeleteLock("s1")
	b := SessionDeleteLock("s1")
	c := SessionDeleteLock("s2")
	if a != b {
		t.Fatal("同一 session_id 应返回同一把锁")
	}
	if a == c {
		t.Fatal("不同 session_id 应返回不同锁")
	}
	// 实际互斥性
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mu := SessionDeleteLock("s1")
			mu.Lock()
			defer mu.Unlock()
		}()
	}
	wg.Wait()
}

// TestTeamDeleteLock_独立于会话锁 校验两把锁不互相干扰。
func TestTeamDeleteLock_独立于会话锁(t *testing.T) {
	tl := TeamDeleteLock("t1")
	sl := SessionDeleteLock("t1") // 同名不同域
	if tl == sl {
		t.Fatal("团队锁与会话锁必须是不同对象")
	}
	if TeamDeleteLock("t1") != tl {
		t.Fatal("同一团队名应返回同一把锁")
	}
}

// TestReleaseDeleteLock_显式回收 校验 Go 无弱引用需手动回收（spec §8 D-6）。
func TestReleaseDeleteLock_显式回收(t *testing.T) {
	first := SessionDeleteLock("s-recycle")
	ReleaseSessionDeleteLock("s-recycle")
	second := SessionDeleteLock("s-recycle")
	if first == second {
		t.Fatal("回收后应分配新的锁对象（旧对象已从表中移除）")
	}

	firstTeam := TeamDeleteLock("t-recycle")
	ReleaseTeamDeleteLock("t-recycle")
	secondTeam := TeamDeleteLock("t-recycle")
	if firstTeam == secondTeam {
		t.Fatal("团队锁回收后应分配新对象")
	}
}

// TestGlobalTeamDeletionGate_单例 校验全局围栏可访问且唯一。
func TestGlobalTeamDeletionGate_单例(t *testing.T) {
	if GlobalTeamDeletionGate() == nil {
		t.Fatal("全局围栏不应为 nil")
	}
	if GlobalTeamDeletionGate() != GlobalTeamDeletionGate() {
		t.Fatal("全局围栏必须是同一实例")
	}
}

// TestSessionDeleteParticipant_接口形状 校验编译期实现。
func TestSessionDeleteParticipant_接口形状(t *testing.T) {
	var _ SessionDeleteParticipant = noopDeleteParticipant{}
	name := noopDeleteParticipant{}.Name()
	if name != "noop" {
		t.Fatalf("Name() 应为 noop，实际 %q", name)
	}
}

// TestTeamExecutionController_接口形状 校验编译期实现。
func TestTeamExecutionController_接口形状(t *testing.T) {
	var _ TeamExecutionController = noopTeamExecutionController{}
}
