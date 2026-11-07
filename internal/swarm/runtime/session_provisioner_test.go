package runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// ──────────────────────────── fake 依赖 ────────────────────────────

// fakeSessionAgentManager 空实现。
type fakeSessionAgentManager struct {
	// noAgent 为 true 时 GetAgentNoWait 返回 nil
	noAgent bool
}

// ClaimPrewarmedSession 返回新建会话。
func (f *fakeSessionAgentManager) ClaimPrewarmedSession(_ context.Context, req PrewarmClaimRequest) (PrewarmClaim, error) {
	return PrewarmClaim{SessionID: "created-session", PrewarmHit: false, PrewarmStatus: "miss"}, nil
}

// ActivateSessionPrewarm 无操作。
func (f *fakeSessionAgentManager) ActivateSessionPrewarm(string) {}

// ReleaseSessionPrewarmClaim 无操作。
func (f *fakeSessionAgentManager) ReleaseSessionPrewarmClaim(context.Context, string) error {
	return nil
}

// CreateSession 生成目标会话。
func (f *fakeSessionAgentManager) CreateSession(_ context.Context, _, _ string) (string, error) {
	return "fork-target", nil
}

// GetAgentNoWait 依据 noAgent 返回。
func (f *fakeSessionAgentManager) GetAgentNoWait(string) SessionAgent {
	if f.noAgent {
		return nil
	}
	return &fakeSessionAgent{}
}

// recordingAgentManager 记录调用并可控失败。
type recordingAgentManager struct {
	fakeSessionAgentManager
	// claimCalled 是否调用过认领
	claimCalled bool
	// claimReq 认领请求
	claimReq PrewarmClaimRequest
	// releaseCalled 是否调用过释放
	releaseCalled bool
}

// ClaimPrewarmedSession 记录并返回。
func (r *recordingAgentManager) ClaimPrewarmedSession(_ context.Context, req PrewarmClaimRequest) (PrewarmClaim, error) {
	r.claimCalled = true
	r.claimReq = req
	return PrewarmClaim{SessionID: "created-session", PrewarmHit: false, PrewarmStatus: "miss"}, nil
}

// ReleaseSessionPrewarmClaim 记录。
func (r *recordingAgentManager) ReleaseSessionPrewarmClaim(context.Context, string) error {
	r.releaseCalled = true
	return nil
}

// fakeSessionAgent 最小 SessionAgent 实现。
type fakeSessionAgent struct{}

// EnsureInstance 返回自身。
func (*fakeSessionAgent) EnsureInstance(context.Context) (any, error) { return nil, nil }

// Card 返回 nil。
func (*fakeSessionAgent) Card() any { return nil }

// fakePlanController 空实现。
type fakePlanController struct{}

// ResetSession 无操作。
func (f *fakePlanController) ResetSession(string) {}

// fakeTrajectoryHook 空实现。
type fakeTrajectoryHook struct{}

// BeginSessionDelete 无操作。
func (f *fakeTrajectoryHook) BeginSessionDelete(context.Context, string) error { return nil }

// AbortSessionDelete 无操作。
func (f *fakeTrajectoryHook) AbortSessionDelete(context.Context, string) error { return nil }

// CommitSessionDelete 无操作。
func (f *fakeTrajectoryHook) CommitSessionDelete(context.Context, string) error { return nil }

// fakeTeamBindingUnbinder 空实现。
type fakeTeamBindingUnbinder struct{}

// UnbindSession 无操作。
func (f *fakeTeamBindingUnbinder) UnbindSession(string, string) error { return nil }

// fakeLifecycleJournal 空实现，recovering 恒 false。
type fakeLifecycleJournal struct{}

// RecoveringDelete 是否处于恢复中的删除。
func (f *fakeLifecycleJournal) RecoveringDelete(string) bool { return false }

// MarkCleanup 标记进入清理阶段。
func (f *fakeLifecycleJournal) MarkCleanup(string) error { return nil }

// newTestProvisioner 构造最小可用的 provisioner。
func newTestProvisioner() *RuntimeSessionProvisioner {
	p := NewRuntimeSessionProvisioner(ProvisionerDeps{
		AgentManager:        &fakeSessionAgentManager{},
		PlanController:      &fakePlanController{},
		TrajectoryHook:      &fakeTrajectoryHook{},
		TeamBinding:         &fakeTeamBindingUnbinder{},
		LifecycleJournal:    &fakeLifecycleJournal{},
		ParticipantRegistry: session.NewRuntimeParticipantRegistry(),
	})
	p.CheckpointEnsure = func(context.Context) error { return nil }
	p.RunnerRelease = func(context.Context, string) error { return nil }
	p.RemoveMetadataCache = func(string) {}
	p.extra.ResolveSessionDir = func(string) (string, bool) { return "", false }
	p.extra.RemoveSessionDir = func(string) error { return nil }
	p.extra.IsValidSessionID = func(string) bool { return true }
	p.extra.ResolveWorkMode = func(map[string]any, string) projectBinding { return projectBinding{} }
	p.extra.ResolveCanonicalMode = func(map[string]any, string) (string, bool) { return "agent", false }
	p.extra.PrewarmModelEligible = func(string) bool { return true }
	p.extra.DeletePending = func(string) bool { return false }
	return p
}

// ──────────────────────────── 契约测试 ────────────────────────────

// TestProvision_Timing 校验三种操作的提交时机。
func TestProvision_Timing(t *testing.T) {
	if SessionCreateCommitTiming != SessionProvisionCommitTimingAfterResultDelivery {
		t.Fatal("create 应为 AFTER_RESULT_DELIVERY")
	}
	if SessionSwitchCommitTiming != SessionProvisionCommitTimingBeforeResultDelivery {
		t.Fatal("switch 应为 BEFORE_RESULT_DELIVERY")
	}
	if SessionForkCommitTiming != SessionProvisionCommitTimingBeforeResultDelivery {
		t.Fatal("fork 应为 BEFORE_RESULT_DELIVERY")
	}
}

// TestProvision_初始状态为PREPARED 校验租约起点。
func TestProvision_初始状态为PREPARED(t *testing.T) {
	p := newTestProvisioner()
	lease := p.stageSessionProvision(SessionForkResult{ChannelID: "tui"},
		SessionProvisionCommitTimingBeforeResultDelivery, nil, nil, false)
	if lease.State() != SessionProvisionStatePrepared {
		t.Fatalf("初始状态应为 PREPARED，实际 %s", lease.State())
	}
}

// TestProvision_Commit幂等 校验重复提交返回同一结果。
func TestProvision_Commit幂等(t *testing.T) {
	p := newTestProvisioner()
	lease := p.stageSessionProvision(SessionForkResult{SessionID: "t2"},
		SessionProvisionCommitTimingBeforeResultDelivery, nil, nil, false)
	ctx := NewEmptyCommitContext()
	first, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx)
	if err != nil {
		t.Fatalf("首次 commit 失败: %v", err)
	}
	second, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx)
	if err != nil {
		t.Fatalf("重复 commit 应幂等，实际 %v", err)
	}
	if first != second {
		t.Fatal("重复 commit 应返回同一结果")
	}
}

// TestProvision_CommitTiming不符报错 校验时机校验。
func TestProvision_CommitTiming不符报错(t *testing.T) {
	p := newTestProvisioner()
	lease := p.stageSessionProvision(SessionForkResult{},
		SessionProvisionCommitTimingBeforeResultDelivery, nil, nil, false)
	ctx := NewEmptyCommitContext()
	_, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingAfterResultDelivery, &ctx)
	if err == nil {
		t.Fatal("timing 不符应报错")
	}
	want := "session provision commit timing mismatch: expected before_result_delivery, got after_result_delivery"
	if err.Error() != want {
		t.Fatalf("错误文案 = %q, want %q", err.Error(), want)
	}
}

// TestProvision_Commit已中止报错 校验终态互斥。
func TestProvision_Commit已中止报错(t *testing.T) {
	p := newTestProvisioner()
	lease := p.stageSessionProvision(SessionForkResult{},
		SessionProvisionCommitTimingBeforeResultDelivery, nil, nil, false)
	if err := p.AbortSessionProvision(lease); err != nil {
		t.Fatalf("abort 失败: %v", err)
	}
	ctx := NewEmptyCommitContext()
	_, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx)
	if err == nil || err.Error() != "cannot commit an aborted session provision" {
		t.Fatalf("应报 cannot commit an aborted，实际 %v", err)
	}
}

// TestProvision_Abort已提交报错 校验反向互斥。
func TestProvision_Abort已提交报错(t *testing.T) {
	p := newTestProvisioner()
	lease := p.stageSessionProvision(SessionForkResult{},
		SessionProvisionCommitTimingBeforeResultDelivery, nil, nil, false)
	ctx := NewEmptyCommitContext()
	if _, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx); err != nil {
		t.Fatalf("commit 失败: %v", err)
	}
	err := p.AbortSessionProvision(lease)
	if err == nil || err.Error() != "cannot abort a committed session provision" {
		t.Fatalf("应报 cannot abort a committed，实际 %v", err)
	}
}

// TestProvision_Abort幂等 校验重复中止不报错。
func TestProvision_Abort幂等(t *testing.T) {
	p := newTestProvisioner()
	lease := p.stageSessionProvision(SessionForkResult{},
		SessionProvisionCommitTimingBeforeResultDelivery, nil, nil, false)
	if err := p.AbortSessionProvision(lease); err != nil {
		t.Fatalf("首次 abort 失败: %v", err)
	}
	if err := p.AbortSessionProvision(lease); err != nil {
		t.Fatalf("重复 abort 应幂等，实际 %v", err)
	}
}

// TestProvision_提交上下文不匹配报错 校验 ctx 一致性。
func TestProvision_提交上下文不匹配报错(t *testing.T) {
	p := newTestProvisioner()
	lease := p.stageSessionProvision(SessionForkResult{},
		SessionProvisionCommitTimingBeforeResultDelivery,
		func(SessionProvisionCommitContext) error { return errors.New("boom") }, nil, false)
	ctx1 := SessionProvisionCommitContext{ForegroundScopeID: "v1"}
	if _, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx1); err == nil {
		t.Fatal("commit hook 应报错")
	}
	ctx2 := SessionProvisionCommitContext{ForegroundScopeID: "v2"}
	_, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx2)
	if err == nil || err.Error() != "session provision commit context does not match first attempt" {
		t.Fatalf("应报 context 不匹配，实际 %v", err)
	}
}

// TestProvision_非终态提交失败可重试 校验 COMMITTING 状态保留。
func TestProvision_非终态提交失败可重试(t *testing.T) {
	p := newTestProvisioner()
	attempts := 0
	lease := p.stageSessionProvision(SessionForkResult{SessionID: "ok"},
		SessionProvisionCommitTimingBeforeResultDelivery,
		func(SessionProvisionCommitContext) error {
			attempts++
			if attempts == 1 {
				return errors.New("first fails")
			}
			return nil
		}, nil, false)
	ctx := NewEmptyCommitContext()
	if _, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx); err == nil {
		t.Fatal("首次应失败")
	}
	if lease.State() != SessionProvisionStateCommitting {
		t.Fatalf("非终态提交失败后状态应留 COMMITTING，实际 %s", lease.State())
	}
	if _, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx); err != nil {
		t.Fatalf("重试应成功，实际 %v", err)
	}
	if lease.State() != SessionProvisionStateCommitted {
		t.Fatalf("重试成功后应为 COMMITTED，实际 %s", lease.State())
	}
}

// TestProvision_终态提交失败保持COMMITTED 校验不可回滚语义。
func TestProvision_终态提交失败保持COMMITTED(t *testing.T) {
	p := newTestProvisioner()
	boom := errors.New("kvc dispatch failed")
	lease := p.stageSessionProvision(SessionForkResult{},
		SessionProvisionCommitTimingAfterResultDelivery,
		func(SessionProvisionCommitContext) error { return boom }, nil, true)
	ctx := NewEmptyCommitContext()
	_, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingAfterResultDelivery, &ctx)
	if !errors.Is(err, boom) {
		t.Fatalf("应原样上抛 hook 错误，实际 %v", err)
	}
	if lease.State() != SessionProvisionStateCommitted {
		t.Fatalf("终态提交失败后状态应为 COMMITTED（不可回滚），实际 %s", lease.State())
	}
	if err := p.AbortSessionProvision(lease); err == nil {
		t.Fatal("已 COMMITTED 后 abort 应报错")
	}
}

// TestProvision_ownerToken防跨provisioner串用 校验租约归属。
func TestProvision_ownerToken防跨provisioner串用(t *testing.T) {
	p1 := newTestProvisioner()
	p2 := newTestProvisioner()
	lease := p1.stageSessionProvision(SessionForkResult{},
		SessionProvisionCommitTimingBeforeResultDelivery, nil, nil, false)
	ctx := NewEmptyCommitContext()
	_, err := p2.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx)
	if err == nil || err.Error() != "prepared session provision belongs to a different provisioner" {
		t.Fatalf("跨 provisioner 使用应报错，实际 %v", err)
	}
}

// TestProvision_并发commit与abort互斥 校验 -race 下状态机正确。
func TestProvision_并发commit与abort互斥(t *testing.T) {
	p := newTestProvisioner()
	for i := 0; i < 20; i++ {
		lease := p.stageSessionProvision(SessionForkResult{},
			SessionProvisionCommitTimingBeforeResultDelivery, nil, nil, false)
		ctx := NewEmptyCommitContext()
		done := make(chan struct{}, 2)
		go func() {
			defer func() { done <- struct{}{} }()
			_, _ = p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx)
		}()
		go func() {
			defer func() { done <- struct{}{} }()
			_ = p.AbortSessionProvision(lease)
		}()
		<-done
		<-done
		state := lease.State()
		if state != SessionProvisionStateCommitted && state != SessionProvisionStateAborted {
			t.Fatalf("并发后状态必须是两个终态之一，实际 %s", state)
		}
	}
}

// TestProvision_CommitContext零值等价None 校验默认上下文。
func TestProvision_CommitContext零值等价None(t *testing.T) {
	p := newTestProvisioner()
	lease := p.stageSessionProvision(SessionForkResult{SessionID: "s"},
		SessionProvisionCommitTimingBeforeResultDelivery,
		func(ctx SessionProvisionCommitContext) error {
			if ctx.ForegroundScopeID != "" {
				t.Errorf("默认 context 的 ForegroundScopeID 应为空，实际 %q", ctx.ForegroundScopeID)
			}
			return nil
		}, nil, false)
	if _, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, nil); err != nil {
		t.Fatalf("nil context 应按零值处理，实际 %v", err)
	}
}

// TestProvision_AbortHook先于ABORTED 校验中止钩子时序。
func TestProvision_AbortHook先于ABORTED(t *testing.T) {
	p := newTestProvisioner()
	hookCalled := false
	lease := p.stageSessionProvision(SessionForkResult{},
		SessionProvisionCommitTimingBeforeResultDelivery, nil,
		func() error { hookCalled = true; return nil }, false)
	if err := p.AbortSessionProvision(lease); err != nil {
		t.Fatalf("abort 失败: %v", err)
	}
	if !hookCalled {
		t.Fatal("abort hook 应被调用")
	}
	if lease.State() != SessionProvisionStateAborted {
		t.Fatalf("状态应为 ABORTED，实际 %s", lease.State())
	}
}

// TestProvision_状态枚举取值 校验线协议字符串。
func TestProvision_状态枚举取值(t *testing.T) {
	pairs := []struct {
		// got 实际取值
		got string
		// want 期望取值
		want string
	}{
		{string(SessionProvisionStatePrepared), "prepared"},
		{string(SessionProvisionStateCommitting), "committing"},
		{string(SessionProvisionStateAborting), "aborting"},
		{string(SessionProvisionStateCommitted), "committed"},
		{string(SessionProvisionStateAborted), "aborted"},
		{string(SessionProvisionCommitTimingBeforeResultDelivery), "before_result_delivery"},
		{string(SessionProvisionCommitTimingAfterResultDelivery), "after_result_delivery"},
	}
	for _, p := range pairs {
		if p.got != p.want {
			t.Errorf("枚举取值 = %q, want %q", p.got, p.want)
		}
	}
}

// TestProvision_ForkErrorCode映射 校验 ValueError → code 映射。
func TestProvision_ForkErrorCode映射(t *testing.T) {
	cases := []struct {
		// msg 错误文案
		msg string
		// want 期望错误码
		want string
	}{
		{"session not found", "NOT_FOUND"},
		{"already exists", "ALREADY_EXISTS"},
		{"some other problem", "BAD_REQUEST"},
	}
	for _, c := range cases {
		if got := sessionForkErrorCode(errors.New(c.msg)); got != c.want {
			t.Errorf("sessionForkErrorCode(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
}

// TestProvision_CommitContext相等语义 校验同内容 context 相等。
func TestProvision_CommitContext相等语义(t *testing.T) {
	a := SessionProvisionCommitContext{ForegroundScopeID: "x"}
	b := SessionProvisionCommitContext{ForegroundScopeID: "x"}
	if a != b {
		t.Fatal("相同内容的 context 应相等")
	}
}

// ──────────────────────────── 骨架与依赖注入 ────────────────────────────

// TestProvisioner_依赖可注入 校验 5 个依赖接口都能在测试中替换。
func TestProvisioner_依赖可注入(t *testing.T) {
	p := &RuntimeSessionProvisioner{
		AgentManager:     &fakeSessionAgentManager{},
		PlanController:   &fakePlanController{},
		TrajectoryHook:   &fakeTrajectoryHook{},
		TeamBinding:      &fakeTeamBindingUnbinder{},
		LifecycleJournal: &fakeLifecycleJournal{},
	}
	if p.AgentManager == nil || p.PlanController == nil || p.TrajectoryHook == nil ||
		p.TeamBinding == nil || p.LifecycleJournal == nil {
		t.Fatal("5 个依赖都必须可注入")
	}
}

// TestProvisioner_NowFunc默认timeNow 校验时钟注入（spec D10 / D-8）。
func TestProvisioner_NowFunc默认timeNow(t *testing.T) {
	p := NewRuntimeSessionProvisioner(ProvisionerDeps{})
	if p.nowFunc == nil {
		t.Fatal("nowFunc 必须默认 time.Now")
	}
	before := time.Now()
	got := p.nowFunc()
	after := time.Now()
	if got.Before(before) || got.After(after) {
		t.Fatal("默认 nowFunc 应返回当前时刻")
	}
}

// TestProvisioner_NowFunc可替换 校验假时钟。
func TestProvisioner_NowFunc可替换(t *testing.T) {
	fixed := time.Date(2026, 11, 7, 0, 0, 0, 0, time.UTC)
	p := NewRuntimeSessionProvisioner(ProvisionerDeps{NowFunc: func() time.Time { return fixed }})
	if !p.nowFunc().Equal(fixed) {
		t.Fatal("注入的 nowFunc 应生效")
	}
}

// TestProvisioner_ParticipantRegistry默认自建 校验空值回落。
func TestProvisioner_ParticipantRegistry默认自建(t *testing.T) {
	p := NewRuntimeSessionProvisioner(ProvisionerDeps{})
	if p.participants == nil {
		t.Fatal("ParticipantRegistry 应默认自建")
	}
}

// TestRecentDeleteResults_TTL 校验 60 秒窗口内命中、过期后失效。
func TestRecentDeleteResults_TTL(t *testing.T) {
	now := time.Now()
	r := newRecentDeleteResults[session.SessionDeleteResult](256, 60*time.Second, func() time.Time { return now })
	r.put("s1", session.SessionDeleteResult{SessionID: "s1", Deleted: true})
	if _, ok := r.get("s1"); !ok {
		t.Fatal("未过期应命中")
	}
	now = now.Add(61 * time.Second)
	if _, ok := r.get("s1"); ok {
		t.Fatal("过期后不应命中")
	}
}

// TestRecentDeleteResults_容量淘汰 校验超出容量后最早的被移除。
func TestRecentDeleteResults_容量淘汰(t *testing.T) {
	r := newRecentDeleteResults[session.SessionDeleteResult](2, 60*time.Second, time.Now)
	r.put("a", session.SessionDeleteResult{SessionID: "a"})
	r.put("b", session.SessionDeleteResult{SessionID: "b"})
	r.put("c", session.SessionDeleteResult{SessionID: "c"})
	if _, ok := r.get("a"); ok {
		t.Fatal("容量 2 时最早的应被淘汰")
	}
	if _, ok := r.get("c"); !ok {
		t.Fatal("最新条目应保留")
	}
}

// TestRecentDeleteResults_remove 校验显式移除。
func TestRecentDeleteResults_remove(t *testing.T) {
	r := newRecentDeleteResults[session.SessionDeleteResult](256, 60*time.Second, time.Now)
	r.put("a", session.SessionDeleteResult{SessionID: "a"})
	r.remove("a")
	if _, ok := r.get("a"); ok {
		t.Fatal("移除后不应命中")
	}
}
