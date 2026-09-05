package evolution

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeApprovalManager 用于测试的模拟审批管理器。
type fakeApprovalManager struct {
	approveResult experience.ExperienceApplyResult
	approveErr    error
	rejectResult  experience.ExperienceApplyResult
	rejectErr     error
	approveCalled bool
	rejectCalled  bool
}

func (f *fakeApprovalManager) ApproveRequest(_ context.Context, _ string) (experience.ExperienceApplyResult, error) {
	f.approveCalled = true
	return f.approveResult, f.approveErr
}

func (f *fakeApprovalManager) RejectRequest(_ context.Context, _ string) (experience.ExperienceApplyResult, error) {
	f.rejectCalled = true
	return f.rejectResult, f.rejectErr
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestLookupPendingApprovalSnapshot_Found(t *testing.T) {
	// 对齐 Python: lookup_pending_approval_snapshot(request_id="req_1", rail_name="skill", action_name="approve")
	snapshots := PendingApprovalSnapshotStore{
		"req_1": &experience.PendingChange{SkillName: "my_skill"},
	}
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, snapshots)
	pending := rt.LookupPendingApprovalSnapshot("req_1", "skill", "approve")
	if pending == nil {
		t.Fatal("应找到 pending snapshot")
	}
	if pending.SkillName != "my_skill" {
		t.Errorf("SkillName = %q, want %q", pending.SkillName, "my_skill")
	}
}

func TestLookupPendingApprovalSnapshot_NotFound(t *testing.T) {
	// 对齐 Python: lookup_pending_approval_snapshot(request_id="nonexistent", ...) → None
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	pending := rt.LookupPendingApprovalSnapshot("nonexistent", "skill", "approve")
	if pending != nil {
		t.Error("未找到时应返回 nil")
	}
}

func TestApprovePendingRequest_Success(t *testing.T) {
	// 对齐 Python: approve_pending_request(request_id="req_1", rail_name="skill", action_name="approve") → (pending, result)
	pendingChange := &experience.PendingChange{SkillName: "my_skill"}
	snapshots := PendingApprovalSnapshotStore{"req_1": pendingChange}
	mgr := &fakeApprovalManager{
		approveResult: experience.ExperienceApplyResult{AppliedCount: 3, PendingCount: 0},
	}
	rt := NewEvolutionApprovalRuntime(mgr, snapshots)

	pending, result, err := rt.ApprovePendingRequest(context.Background(), "req_1", "skill", "approve")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pending != pendingChange {
		t.Error("pending 不匹配")
	}
	if result.AppliedCount != 3 {
		t.Errorf("AppliedCount = %d, want 3", result.AppliedCount)
	}
	if !mgr.approveCalled {
		t.Error("ApproveRequest 应被调用")
	}
}

func TestApprovePendingRequest_PartialFailure(t *testing.T) {
	// 对齐 Python: pending_count > 0 时记录 Warn 日志
	pendingChange := &experience.PendingChange{SkillName: "my_skill"}
	snapshots := PendingApprovalSnapshotStore{"req_1": pendingChange}
	mgr := &fakeApprovalManager{
		approveResult: experience.ExperienceApplyResult{AppliedCount: 2, PendingCount: 1},
	}
	rt := NewEvolutionApprovalRuntime(mgr, snapshots)

	pending, result, err := rt.ApprovePendingRequest(context.Background(), "req_1", "skill", "approve")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pending == nil {
		t.Fatal("pending 不应为 nil")
	}
	if result.PendingCount != 1 {
		t.Errorf("PendingCount = %d, want 1", result.PendingCount)
	}
}

func TestApprovePendingRequest_NotFound(t *testing.T) {
	// 对齐 Python: pending 为 None 时返回 (None, None)
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	pending, result, err := rt.ApprovePendingRequest(context.Background(), "nonexistent", "skill", "approve")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pending != nil {
		t.Error("pending 应为 nil")
	}
	if result != nil {
		t.Error("result 应为 nil")
	}
}

func TestApprovePendingRequest_ManagerError(t *testing.T) {
	pendingChange := &experience.PendingChange{SkillName: "my_skill"}
	snapshots := PendingApprovalSnapshotStore{"req_1": pendingChange}
	mgr := &fakeApprovalManager{
		approveErr: context.DeadlineExceeded,
	}
	rt := NewEvolutionApprovalRuntime(mgr, snapshots)

	pending, result, err := rt.ApprovePendingRequest(context.Background(), "req_1", "skill", "approve")
	if err == nil {
		t.Fatal("应返回错误")
	}
	if pending == nil {
		t.Error("pending 应不为 nil（查找成功但 approve 失败）")
	}
	if result != nil {
		t.Error("result 应为 nil")
	}
}

func TestRejectPendingRequest_Success(t *testing.T) {
	// 对齐 Python: reject_pending_request(request_id="req_1", ...) → (pending, result)
	pendingChange := &experience.PendingChange{SkillName: "my_skill"}
	snapshots := PendingApprovalSnapshotStore{"req_1": pendingChange}
	mgr := &fakeApprovalManager{
		rejectResult: experience.ExperienceApplyResult{RejectedCount: 3},
	}
	rt := NewEvolutionApprovalRuntime(mgr, snapshots)

	pending, result, err := rt.RejectPendingRequest(context.Background(), "req_1", "skill", "reject")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pending != pendingChange {
		t.Error("pending 不匹配")
	}
	if result.RejectedCount != 3 {
		t.Errorf("RejectedCount = %d, want 3", result.RejectedCount)
	}
	if !mgr.rejectCalled {
		t.Error("RejectRequest 应被调用")
	}
}

func TestRejectPendingRequest_NotFound(t *testing.T) {
	// 对齐 Python: pending 为 None 时返回 (None, None)
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	pending, result, err := rt.RejectPendingRequest(context.Background(), "nonexistent", "skill", "reject")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if pending != nil {
		t.Error("pending 应为 nil")
	}
	if result != nil {
		t.Error("result 应为 nil")
	}
}

func TestFinalizeStagedEvolutionRequest_RequiresApproval(t *testing.T) {
	// 对齐 Python: finalize_staged_evolution_request(request, requires_approval=True, emit_approval_request=fn)
	var called bool
	emitFn := func(_ any) error {
		called = true
		return nil
	}
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	err := rt.FinalizeStagedEvolutionRequest("some_request", true, emitFn, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !called {
		t.Error("emitApprovalRequest 应被调用")
	}
}

func TestFinalizeStagedEvolutionRequest_AutoApproved(t *testing.T) {
	// 对齐 Python: finalize_staged_evolution_request(request, requires_approval=False, on_auto_approved=fn)
	var called bool
	autoFn := func(_ any) error {
		called = true
		return nil
	}
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	err := rt.FinalizeStagedEvolutionRequest("some_request", false, nil, autoFn)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !called {
		t.Error("onAutoApproved 应被调用")
	}
}

func TestFinalizeStagedEvolutionRequest_NilRequest(t *testing.T) {
	// 对齐 Python L102: if request is None: return None
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	err := rt.FinalizeStagedEvolutionRequest(nil, true, nil, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
}

func TestFinalizeStagedEvolutionRequest_NoAutoApprovedCallback(t *testing.T) {
	// 对齐 Python L111: if on_auto_approved is not None → on_auto_approved 为 None 时跳过
	rt := NewEvolutionApprovalRuntime(&fakeApprovalManager{}, PendingApprovalSnapshotStore{})
	err := rt.FinalizeStagedEvolutionRequest("some_request", false, nil, nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
}

func TestApprovalManager_ExperienceManager满足接口(t *testing.T) {
	// 编译期检查：如果 ExperienceManager 不满足 ApprovalManager，此行会编译失败
	var _ ApprovalManager = (*experience.ExperienceManager)(nil)
}
