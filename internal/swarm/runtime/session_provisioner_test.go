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
	// 透传请求模式，并按 "team" 前缀判定是否团队（与上游 is_team_mode 同构的最小替身）
	p.extra.ResolveCanonicalMode = func(params map[string]any, _ string) (string, bool) {
		mode, _ := params["mode"].(string)
		return mode, mode == "team" || (len(mode) > 5 && mode[:5] == "team.")
	}
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

// ──────────────────────────── fake 团队与参与者 ────────────────────────────

// fakeTeamManager 记录 prepare 调用。
type fakeTeamManager struct {
	// prepared 已准备的目标会话
	prepared []string
	// previous 传参的上一会话
	previous []string
	// reasons 传参的缘由
	reasons []string
}

// PrepareSessionSwitch 记录调用。
func (f *fakeTeamManager) PrepareSessionSwitch(_ context.Context, target, previous, reason string) error {
	f.prepared = append(f.prepared, target)
	f.previous = append(f.previous, previous)
	f.reasons = append(f.reasons, reason)
	return nil
}

// fakeActivityParticipant 记录前台事件。
type fakeActivityParticipant struct {
	// events 事件序列
	events []string
}

// SessionInputIntent 无操作。
func (f *fakeActivityParticipant) SessionInputIntent(context.Context, session.SessionInputIntentEvent) (session.SessionInputIntentDisposition, error) {
	return session.SessionInputIntentDispositionNotNeeded, nil
}

// ForegroundChanged 记录事件。
func (f *fakeActivityParticipant) ForegroundChanged(_ context.Context, e session.SessionForegroundEvent) error {
	if e.Visible {
		f.events = append(f.events, "visible:"+e.Target.Descriptor.SessionID)
	} else {
		f.events = append(f.events, "hidden:"+e.Target.Descriptor.SessionID)
	}
	return nil
}

// ExecutionStarted 无操作。
func (f *fakeActivityParticipant) ExecutionStarted(context.Context, session.SessionExecutionEvent) error {
	return nil
}

// ExecutionFinished 无操作。
func (f *fakeActivityParticipant) ExecutionFinished(session.SessionExecutionFinishedEvent) {}

// SessionInactive 无操作。
func (f *fakeActivityParticipant) SessionInactive(context.Context, session.SessionInactiveEvent) error {
	return nil
}

// fakeSessionOps 记录会话复制调用。
type fakeSessionOps struct {
	// forkErr fork 注入错误
	forkErr error
	// forceHistory 最近一次 force_history 参数
	forceHistory bool
	// copyContextCalled 是否调用过复制上下文
	copyContextCalled bool
	// copyStateCalled 是否调用过复制 state
	copyStateCalled bool
	// equipmentOverride 最近一次设备覆盖
	equipmentOverride map[string]any
	// cutoffMessageID 最近一次截断消息标识
	cutoffMessageID string
	// cutoffRole 最近一次截断角色
	cutoffRole string
	// cutoffContent 最近一次截断内容
	cutoffContent string
	// cutoffTimestamp 最近一次截断时间戳
	cutoffTimestamp any
	// metadata 预置元数据
	metadata map[string]map[string]any
	// initCalled 是否调用过初始化元数据
	initCalled bool
	// initParams 最近一次初始化参数
	initParams InitMetadataParams
	// initErr 初始化元数据注入错误
	initErr error
}

// ForkSession 记录参数并返回。
func (f *fakeSessionOps) ForkSession(params ForkSessionParams) (ForkSessionResult, error) {
	if f.forkErr != nil {
		return ForkSessionResult{}, f.forkErr
	}
	f.equipmentOverride = params.SessionEquipmentOverride
	f.cutoffMessageID = params.CutoffMessageID
	f.cutoffRole = params.CutoffRole
	f.cutoffContent = params.CutoffContent
	f.cutoffTimestamp = params.CutoffTimestamp
	return ForkSessionResult{
		SourceSessionID: params.SourceSessionID,
		SessionID:       params.TargetSessionID,
		Title:           params.Title,
	}, nil
}

// CopySessionContext 记录调用。
func (f *fakeSessionOps) CopySessionContext(_ context.Context, _ SessionAgent, _, _ string, forceHistory bool) error {
	f.copyContextCalled = true
	f.forceHistory = forceHistory
	return nil
}

// CopySessionState 记录调用。
func (f *fakeSessionOps) CopySessionState(context.Context, string, string, any, SessionAgent) error {
	f.copyStateCalled = true
	return nil
}

// GetSessionMetadata 返回预置 metadata。
func (f *fakeSessionOps) GetSessionMetadata(sessionID string) map[string]any {
	if f.metadata == nil {
		return nil
	}
	return f.metadata[sessionID]
}

// SessionDirHasMetadata 依据预置 metadata 判定。
func (f *fakeSessionOps) SessionDirHasMetadata(sessionID string) bool {
	return f.metadata != nil && f.metadata[sessionID] != nil
}

// InitSessionMetadata 记录参数（可注入失败）。
func (f *fakeSessionOps) InitSessionMetadata(params InitMetadataParams) error {
	f.initCalled = true
	f.initParams = params
	return f.initErr
}

// SetSessionModelSelection 无操作。
func (f *fakeSessionOps) SetSessionModelSelection(string, map[string]any) error { return nil }

// HistoryExists 恒 false。
func (f *fakeSessionOps) HistoryExists(string) bool { return false }

// newTestProvisionerWithMetadata 构造带 metadata 的 provisioner。
func newTestProvisionerWithMetadata(metadata map[string]map[string]any) *RuntimeSessionProvisioner {
	p := newTestProvisioner()
	p.SetSessionOps(&fakeSessionOps{metadata: metadata})
	return p
}

// ──────────────────────────── switch ────────────────────────────

// TestSwitch_空目标报BAD_REQUEST 校验参数校验。
func TestSwitch_空目标报BAD_REQUEST(t *testing.T) {
	p := newTestProvisioner()
	_, err := p.PrepareSessionSwitch(context.Background(), SessionSwitchInput{ChannelID: "web"})
	var pe *SessionProvisionError
	if !errors.As(err, &pe) || pe.Code != "BAD_REQUEST" || pe.Message != "session_id is required" {
		t.Fatalf("应是 BAD_REQUEST/session_id is required，实际 %v", err)
	}
}

// TestSwitch_提交时机为BEFORE_RESULT_DELIVERY 校验租约声明。
func TestSwitch_提交时机为BEFORE_RESULT_DELIVERY(t *testing.T) {
	p := newTestProvisionerWithMetadata(map[string]map[string]any{"t1": {"mode": "agent.plan"}})
	lease, err := p.PrepareSessionSwitch(context.Background(), SessionSwitchInput{
		ChannelID: "web", TargetSessionID: "t1",
	})
	if err != nil {
		t.Fatalf("PrepareSessionSwitch 失败: %v", err)
	}
	if lease.CommitTiming() != SessionProvisionCommitTimingBeforeResultDelivery {
		t.Fatalf("timing 错误: %s", lease.CommitTiming())
	}
	res := lease.Result().(SessionSwitchResult)
	if res.SessionID != "t1" || !res.Switched {
		t.Fatalf("结果错误: %+v", res)
	}
}

// TestSwitch_存储模式优先于请求模式 校验 stored mode 覆盖。
func TestSwitch_存储模式优先于请求模式(t *testing.T) {
	p := newTestProvisionerWithMetadata(map[string]map[string]any{"t1": {"mode": "team"}})
	lease, err := p.PrepareSessionSwitch(context.Background(), SessionSwitchInput{
		ChannelID: "web", TargetSessionID: "t1", Mode: "agent.plan",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if got := lease.Result().(SessionSwitchResult).Mode; got != "team" {
		t.Fatalf("应以存储模式 team 为准，实际 %q", got)
	}
}

// TestSwitch_无metadata时用请求模式 校验回退。
func TestSwitch_无metadata时用请求模式(t *testing.T) {
	p := newTestProvisionerWithMetadata(nil)
	lease, err := p.PrepareSessionSwitch(context.Background(), SessionSwitchInput{
		ChannelID: "web", TargetSessionID: "t1", Mode: "code.normal",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if got := lease.Result().(SessionSwitchResult).Mode; got != "code.normal" {
		t.Fatalf("应用请求模式，实际 %q", got)
	}
}

// TestSwitch_团队会话触发prepare 校验团队归属准备。
func TestSwitch_团队会话触发prepare(t *testing.T) {
	fake := &fakeTeamManager{}
	p := newTestProvisioner()
	p.SetTeamManagerProvider(func(string) TeamSwitchPreparer { return fake })
	if _, err := p.PrepareSessionSwitch(context.Background(), SessionSwitchInput{
		ChannelID: "web", TargetSessionID: "t1", TeamHint: true,
	}); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if len(fake.prepared) != 1 {
		t.Fatal("团队会话应触发 team manager 的 prepare_session_switch")
	}
	if fake.reasons[0] != "session.switch: " {
		t.Fatalf("缘由应为 session.switch: ，实际 %q", fake.reasons[0])
	}
}

// TestSwitch_MoaEnabled透传 校验 metadata 字段。
func TestSwitch_MoaEnabled透传(t *testing.T) {
	p := newTestProvisionerWithMetadata(map[string]map[string]any{
		"t1": {"mode": "agent.plan", "moa_enabled": true},
	})
	lease, err := p.PrepareSessionSwitch(context.Background(), SessionSwitchInput{
		ChannelID: "web", TargetSessionID: "t1",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !lease.Result().(SessionSwitchResult).MoaEnabled {
		t.Fatal("MoaEnabled 应透传")
	}
}

// TestSwitch_commit调用前台切换 校验 commit hook。
func TestSwitch_commit调用前台切换(t *testing.T) {
	participant := &fakeActivityParticipant{}
	p := newTestProvisioner()
	p.participants.ReplaceSessionParticipants([]session.SessionActivityParticipant{participant}, nil)
	lease, err := p.PrepareSessionSwitch(context.Background(), SessionSwitchInput{
		ChannelID: "web", TargetSessionID: "t1",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	ctx := SessionProvisionCommitContext{ForegroundScopeID: "view-1"}
	if _, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx); err != nil {
		t.Fatalf("commit 失败: %v", err)
	}
	if len(participant.events) == 0 {
		t.Fatal("commit 应派发前台切换事件")
	}
	if participant.events[len(participant.events)-1] != "visible:t1" {
		t.Fatalf("最后事件应为 visible:t1，实际 %v", participant.events)
	}
}

// TestSwitch_已准备租约状态为PREPARED 校验租约起点。
func TestSwitch_已准备租约状态为PREPARED(t *testing.T) {
	p := newTestProvisionerWithMetadata(nil)
	lease, err := p.PrepareSessionSwitch(context.Background(), SessionSwitchInput{
		ChannelID: "web", TargetSessionID: "t1",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if lease.State() != SessionProvisionStatePrepared {
		t.Fatalf("应为 PREPARED，实际 %s", lease.State())
	}
}

// TestSwitch_前后会话都传participant 校验前台切换双事件。
func TestSwitch_前后会话都传participant(t *testing.T) {
	participant := &fakeActivityParticipant{}
	p := newTestProvisionerWithMetadata(map[string]map[string]any{
		"prev": {"mode": "agent.plan"},
		"t1":   {"mode": "agent.plan"},
	})
	p.participants.ReplaceSessionParticipants([]session.SessionActivityParticipant{participant}, nil)
	lease, err := p.PrepareSessionSwitch(context.Background(), SessionSwitchInput{
		ChannelID: "web", TargetSessionID: "t1", PreviousSessionID: "prev",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	ctx := SessionProvisionCommitContext{ForegroundScopeID: "v"}
	if _, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx); err != nil {
		t.Fatalf("commit 失败: %v", err)
	}
	if len(participant.events) != 2 {
		t.Fatalf("应有 hidden:prev + visible:t1 两个事件，实际 %v", participant.events)
	}
	if participant.events[0] != "hidden:prev" || participant.events[1] != "visible:t1" {
		t.Fatalf("事件顺序错误: %v", participant.events)
	}
}

// ──────────────────────────── fork ────────────────────────────

// TestFork_空源报BAD_REQUEST 校验参数校验。
func TestFork_空源报BAD_REQUEST(t *testing.T) {
	p := newTestProvisioner()
	_, err := p.PrepareSessionFork(context.Background(), SessionForkInput{ChannelID: "tui"})
	var pe *SessionProvisionError
	if !errors.As(err, &pe) || pe.Code != "BAD_REQUEST" || pe.Message != "source_session_id is required" {
		t.Fatalf("应是 BAD_REQUEST/source_session_id is required，实际 %v", err)
	}
}

// TestFork_空目标由AgentManager生成 校验自动分配。
func TestFork_空目标由AgentManager生成(t *testing.T) {
	p := newTestProvisioner()
	p.SetSessionOps(&fakeSessionOps{})
	lease, err := p.PrepareSessionFork(context.Background(), SessionForkInput{
		ChannelID: "tui", SourceSessionID: "s1",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if got := lease.Result().(SessionForkResult).SessionID; got != "fork-target" {
		t.Fatalf("应使用 AgentManager.CreateSession 的返回值，实际 %q", got)
	}
}

// TestFork_提交时机为BEFORE_RESULT_DELIVERY 校验租约声明。
func TestFork_提交时机为BEFORE_RESULT_DELIVERY(t *testing.T) {
	p := newTestProvisioner()
	p.SetSessionOps(&fakeSessionOps{})
	lease, _ := p.PrepareSessionFork(context.Background(), SessionForkInput{
		ChannelID: "tui", SourceSessionID: "s1", TargetSessionID: "t2",
	})
	if lease.CommitTiming() != SessionProvisionCommitTimingBeforeResultDelivery {
		t.Fatalf("timing 错误: %s", lease.CommitTiming())
	}
}

// TestFork_有cutoff时强制copyHistory 校验 cutoff 分支。
func TestFork_有cutoff时强制copyHistory(t *testing.T) {
	fake := &fakeSessionOps{}
	p := newTestProvisioner()
	p.SetSessionOps(fake)
	if _, err := p.PrepareSessionFork(context.Background(), SessionForkInput{
		ChannelID: "tui", SourceSessionID: "s1", TargetSessionID: "t2",
		CutoffMessageID: "m1",
	}); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !fake.forceHistory {
		t.Fatal("有 cutoff 时应以 force_history=true 复制上下文")
	}
	if fake.copyStateCalled {
		t.Fatal("有 cutoff 时不应复制 checkpoint state（上游语义）")
	}
}

// TestFork_无cutoff时复制state 校验正常分支。
func TestFork_无cutoff时复制state(t *testing.T) {
	fake := &fakeSessionOps{}
	p := newTestProvisioner()
	p.SetSessionOps(fake)
	if _, err := p.PrepareSessionFork(context.Background(), SessionForkInput{
		ChannelID: "tui", SourceSessionID: "s1", TargetSessionID: "t2",
	}); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if fake.forceHistory {
		t.Fatal("无 cutoff 时不应 force_history")
	}
	if !fake.copyStateCalled {
		t.Fatal("无 cutoff 时应复制 checkpoint state")
	}
}

// TestFork_ValueError映射错误码 校验 not found / already exists。
func TestFork_ValueError映射错误码(t *testing.T) {
	fake := &fakeSessionOps{forkErr: errors.New("session not found")}
	p := newTestProvisioner()
	p.SetSessionOps(fake)
	_, err := p.PrepareSessionFork(context.Background(), SessionForkInput{
		ChannelID: "tui", SourceSessionID: "s1", TargetSessionID: "t2",
	})
	var pe *SessionProvisionError
	if !errors.As(err, &pe) || pe.Code != "NOT_FOUND" {
		t.Fatalf("应映射为 NOT_FOUND，实际 %v", err)
	}
}

// TestFork_无agent时跳过上下文复制 校验降级路径。
func TestFork_无agent时跳过上下文复制(t *testing.T) {
	fake := &fakeSessionOps{}
	p := newTestProvisioner()
	p.SetSessionOps(fake)
	p.AgentManager = &fakeSessionAgentManager{noAgent: true}
	if _, err := p.PrepareSessionFork(context.Background(), SessionForkInput{
		ChannelID: "tui", SourceSessionID: "s1", TargetSessionID: "t2",
	}); err != nil {
		t.Fatalf("无 agent 不应报错: %v", err)
	}
	if fake.copyContextCalled {
		t.Fatal("无 agent 时应跳过内存上下文复制")
	}
}

// TestFork_结果字段回填 校验 SourceSessionID/Title。
func TestFork_结果字段回填(t *testing.T) {
	p := newTestProvisioner()
	p.SetSessionOps(&fakeSessionOps{})
	lease, err := p.PrepareSessionFork(context.Background(), SessionForkInput{
		ChannelID: "tui", SourceSessionID: "s1", TargetSessionID: "t2", Title: "Branch",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	res := lease.Result().(SessionForkResult)
	if res.SourceSessionID != "s1" || res.SessionID != "t2" || res.Title != "Branch" {
		t.Fatalf("结果字段错误: %+v", res)
	}
}

// TestFork_SessionEquipmentOverride透传 校验设备覆盖。
func TestFork_SessionEquipmentOverride透传(t *testing.T) {
	fake := &fakeSessionOps{}
	p := newTestProvisioner()
	p.SetSessionOps(fake)
	override := map[string]any{"model": "m2"}
	if _, err := p.PrepareSessionFork(context.Background(), SessionForkInput{
		ChannelID: "tui", SourceSessionID: "s1", TargetSessionID: "t2",
		SessionEquipmentOverride: override,
	}); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if fake.equipmentOverride["model"] != "m2" {
		t.Fatalf("设备覆盖未透传，实际 %v", fake.equipmentOverride)
	}
}

// TestFork_四个cutoff字段全透传 校验 params 组装。
func TestFork_四个cutoff字段全透传(t *testing.T) {
	fake := &fakeSessionOps{}
	p := newTestProvisioner()
	p.SetSessionOps(fake)
	if _, err := p.PrepareSessionFork(context.Background(), SessionForkInput{
		ChannelID: "tui", SourceSessionID: "s1", TargetSessionID: "t2",
		CutoffMessageID: "m1", CutoffRole: "assistant",
		CutoffContent: "hello", CutoffTimestamp: 1.5,
	}); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if fake.cutoffMessageID != "m1" || fake.cutoffRole != "assistant" ||
		fake.cutoffContent != "hello" || fake.cutoffTimestamp != 1.5 {
		t.Fatalf("cutoff 字段未全透传: %+v", fake)
	}
}

// TestFork_结果无hook可提交 校验 fork 租约无 commit hook。
func TestFork_结果无hook可提交(t *testing.T) {
	p := newTestProvisioner()
	p.SetSessionOps(&fakeSessionOps{})
	lease, _ := p.PrepareSessionFork(context.Background(), SessionForkInput{
		ChannelID: "tui", SourceSessionID: "s1", TargetSessionID: "t2",
	})
	ctx := NewEmptyCommitContext()
	if _, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingBeforeResultDelivery, &ctx); err != nil {
		t.Fatalf("fork 提交应成功（无 hook）: %v", err)
	}
	if lease.State() != SessionProvisionStateCommitted {
		t.Fatalf("应为 COMMITTED，实际 %s", lease.State())
	}
}

// ──────────────────────────── create ────────────────────────────

// TestCreate_非TUI显式ID报错 校验 session_id 不再被接受。
func TestCreate_非TUI显式ID报错(t *testing.T) {
	p := newTestProvisioner()
	_, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "web", RequestedSessionID: "ext",
	})
	var pe *SessionProvisionError
	if !errors.As(err, &pe) {
		t.Fatalf("应返回 SessionProvisionError，实际 %v", err)
	}
	want := "session.create no longer accepts session_id; use session.switch to restore"
	if pe.Message != want {
		t.Fatalf("文案 = %q, want %q", pe.Message, want)
	}
}

// TestCreate_缺createToken报错 校验非 TUI 路径必须带令牌。
func TestCreate_缺createToken报错(t *testing.T) {
	p := newTestProvisioner()
	_, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{ChannelID: "web"})
	var pe *SessionProvisionError
	if !errors.As(err, &pe) || pe.Message != "create_token is required" {
		t.Fatalf("应报 create_token is required，实际 %v", err)
	}
}

// TestCreate_提交时机为AFTER_RESULT_DELIVERY 校验租约声明。
func TestCreate_提交时机为AFTER_RESULT_DELIVERY(t *testing.T) {
	p := newTestProvisioner()
	p.SetSessionOps(&fakeSessionOps{})
	lease, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "web", CreateToken: "tok",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if lease.CommitTiming() != SessionProvisionCommitTimingAfterResultDelivery {
		t.Fatalf("timing 错误: %s", lease.CommitTiming())
	}
}

// TestCreate_认领预热池 校验 claim 调用与令牌透传。
func TestCreate_认领预热池(t *testing.T) {
	manager := &recordingAgentManager{}
	p := newTestProvisioner()
	p.AgentManager = manager
	p.SetSessionOps(&fakeSessionOps{})
	if _, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "web", CreateToken: "tok",
	}); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !manager.claimCalled {
		t.Fatal("应调用 ClaimPrewarmedSession")
	}
	if manager.claimReq.CreateToken != "tok" {
		t.Fatalf("令牌未透传: %+v", manager.claimReq)
	}
}

// TestCreate_写入metadata 校验 InitSessionMetadata 调用。
func TestCreate_写入metadata(t *testing.T) {
	ops := &fakeSessionOps{}
	p := newTestProvisioner()
	p.SetSessionOps(ops)
	if _, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "web", CreateToken: "tok", Title: "T", UserID: "u1",
	}); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !ops.initCalled {
		t.Fatal("应调用 InitSessionMetadata")
	}
	if ops.initParams.Title != "T" || ops.initParams.UserID != "u1" {
		t.Fatalf("metadata 参数错误: %+v", ops.initParams)
	}
}

// TestCreate_metadata已存在时created为false 校验复用路径。
func TestCreate_metadata已存在时created为false(t *testing.T) {
	ops := &fakeSessionOps{metadata: map[string]map[string]any{
		"created-session": {"persist_session": true},
	}}
	p := newTestProvisioner()
	p.SetSessionOps(ops)
	lease, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "web", CreateToken: "tok",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	res := lease.Result().(SessionCreateResult)
	if res.Created {
		t.Fatal("metadata 已存在时 Created 应为 false")
	}
	if ops.initCalled {
		t.Fatal("metadata 已存在时不应重写")
	}
}

// TestCreate_TUI显式ID跳过预热 校验 prewarm_status=bypassed。
func TestCreate_TUI显式ID跳过预热(t *testing.T) {
	p := newTestProvisioner()
	p.extra.IsValidSessionID = func(string) bool { return true }
	p.SetSessionOps(&fakeSessionOps{})
	lease, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "tui", RequestedSessionID: "ext-id",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	res := lease.Result().(SessionCreateResult)
	if res.PrewarmStatus != "bypassed" || res.SessionID != "ext-id" {
		t.Fatalf("TUI 显式 ID 结果错误: %+v", res)
	}
	if !res.ExplicitIDCompatibility {
		t.Fatal("应标记走显式 ID 兼容路径")
	}
}

// TestCreate_TUI非法ID报错 校验 is_valid_session_id。
func TestCreate_TUI非法ID报错(t *testing.T) {
	p := newTestProvisioner()
	p.extra.IsValidSessionID = func(string) bool { return false }
	_, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "tui", RequestedSessionID: "bad/id",
	})
	var pe *SessionProvisionError
	if !errors.As(err, &pe) || pe.Message != "invalid session_id" {
		t.Fatalf("应报 invalid session_id，实际 %v", err)
	}
}

// TestCreate_团队会话触发归属准备 校验 team manager。
func TestCreate_团队会话触发归属准备(t *testing.T) {
	fake := &fakeTeamManager{}
	p := newTestProvisioner()
	p.SetSessionOps(&fakeSessionOps{})
	p.SetTeamManagerProvider(func(string) TeamSwitchPreparer { return fake })
	if _, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "web", CreateToken: "tok", TeamHint: true,
	}); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if len(fake.prepared) != 1 {
		t.Fatal("团队会话应触发归属准备")
	}
	if fake.reasons[0] != "session.create switch: " {
		t.Fatalf("缘由应为 session.create switch: ，实际 %q", fake.reasons[0])
	}
}

// TestCreate_失败补偿释放认领 校验异常路径。
func TestCreate_失败补偿释放认领(t *testing.T) {
	manager := &recordingAgentManager{}
	p := newTestProvisioner()
	p.AgentManager = manager
	ops := &fakeSessionOps{initErr: errors.New("init failed")}
	p.SetSessionOps(ops)
	_, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "web", CreateToken: "tok",
	})
	if err == nil {
		t.Fatal("应失败")
	}
	if !manager.releaseCalled {
		t.Fatal("失败后应释放预热认领")
	}
}

// TestCreate_commit后释放外锁 校验 TUI 锁回收。
func TestCreate_commit后释放外锁(t *testing.T) {
	p := newTestProvisioner()
	p.extra.IsValidSessionID = func(string) bool { return true }
	p.SetSessionOps(&fakeSessionOps{})
	lease, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "tui", RequestedSessionID: "ext-id",
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	ctx := NewEmptyCommitContext()
	if _, err := p.CommitSessionProvision(lease, SessionProvisionCommitTimingAfterResultDelivery, &ctx); err != nil {
		t.Fatalf("commit 失败: %v", err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = p.PrepareSessionCreate(context.Background(), SessionCreateInput{
			ChannelID: "tui", RequestedSessionID: "ext-id",
		})
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("commit 应已释放外锁，否则同 ID 创建会永久阻塞")
	}
}

// TestCreate_并发同ID创建串行 校验外锁互斥。
func TestCreate_并发同ID创建串行(t *testing.T) {
	p := newTestProvisioner()
	p.extra.IsValidSessionID = func(string) bool { return true }
	p.SetSessionOps(&fakeSessionOps{})
	first, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "tui", RequestedSessionID: "same-id",
	})
	if err != nil {
		t.Fatalf("首次失败: %v", err)
	}
	second := make(chan error, 1)
	go func() {
		_, e := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
			ChannelID: "tui", RequestedSessionID: "same-id",
		})
		second <- e
	}()
	select {
	case <-second:
		t.Fatal("首个租约未提交/中止前，同 ID 创建应被阻塞")
	case <-time.After(200 * time.Millisecond):
	}
	ctx := NewEmptyCommitContext()
	_, _ = p.CommitSessionProvision(first, SessionProvisionCommitTimingAfterResultDelivery, &ctx)
	select {
	case <-second:
	case <-time.After(2 * time.Second):
		t.Fatal("提交后第二个创建应被放行")
	}
}

// TestCreate_persistSession不可变 校验 CONFLICT。
func TestCreate_persistSession不可变(t *testing.T) {
	ops := &fakeSessionOps{metadata: map[string]map[string]any{
		"created-session": {"persist_session": true, "channel_id": "tui"},
	}}
	p := newTestProvisioner()
	p.SetSessionOps(ops)
	_, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "tui", RequestedSessionID: "created-session",
		PersistSession: false, PersistSessionSupplied: true,
	})
	var pe *SessionProvisionError
	if !errors.As(err, &pe) || pe.Code != "CONFLICT" {
		t.Fatalf("应报 CONFLICT，实际 %v", err)
	}
}

// TestCreate_渠道归属冲突报错 校验 channel 校验。
func TestCreate_渠道归属冲突报错(t *testing.T) {
	ops := &fakeSessionOps{metadata: map[string]map[string]any{
		"created-session": {"channel_id": "web"},
	}}
	p := newTestProvisioner()
	p.SetSessionOps(ops)
	_, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "tui", RequestedSessionID: "created-session",
	})
	var pe *SessionProvisionError
	if !errors.As(err, &pe) || pe.Message != "session_id is already owned by another channel" {
		t.Fatalf("应报渠道归属冲突，实际 %v", err)
	}
}

// TestCreate_projectNotFound报错 校验 NOT_FOUND。
func TestCreate_projectNotFound报错(t *testing.T) {
	p := newTestProvisioner()
	p.SetSessionOps(&fakeSessionOps{})
	p.extra.ResolveWorkMode = func(map[string]any, string) projectBinding {
		return projectBinding{Error: "project not found: p1", Code: "NOT_FOUND"}
	}
	_, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "web", CreateToken: "tok",
	})
	var pe *SessionProvisionError
	if !errors.As(err, &pe) || pe.Code != "NOT_FOUND" {
		t.Fatalf("应透传 NOT_FOUND，实际 %v", err)
	}
}

// TestCreate_workModeMismatch报BAD_REQUEST 校验 37 空格分隔符。
func TestCreate_workModeMismatch报BAD_REQUEST(t *testing.T) {
	p := newTestProvisioner()
	p.SetSessionOps(&fakeSessionOps{})
	p.extra.ResolveWorkMode = func(map[string]any, string) projectBinding {
		return projectBinding{
			ProjectID: "p1", WorkMode: "code", ProjectWorkMode: "work",
			HasExplicitWorkMode: true,
		}
	}
	_, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "web", CreateToken: "tok",
	})
	var pe *SessionProvisionError
	if !errors.As(err, &pe) || pe.Code != "BAD_REQUEST" {
		t.Fatalf("应报 BAD_REQUEST，实际 %v", err)
	}
	want := "work_mode mismatch: project is 'work'" +
		"                                     " + "but request specified 'code'"
	if pe.Message != want {
		t.Fatalf("文案 = %q\n want  = %q", pe.Message, want)
	}
}

// TestCreate_默认项目不校验workMode 校验 is_default_project_id。
func TestCreate_默认项目不校验workMode(t *testing.T) {
	p := newTestProvisioner()
	p.SetSessionOps(&fakeSessionOps{})
	p.extra.ResolveWorkMode = func(map[string]any, string) projectBinding {
		return projectBinding{
			ProjectID: "default", WorkMode: "code", ProjectWorkMode: "work",
			HasExplicitWorkMode: true,
		}
	}
	if _, err := p.PrepareSessionCreate(context.Background(), SessionCreateInput{
		ChannelID: "web", CreateToken: "tok",
	}); err != nil {
		t.Fatalf("默认项目不应校验 work_mode 一致性，实际 %v", err)
	}
}

// TestCreate_预热资格判据 校验 swarm/cron+web/moa 三条排除。
func TestCreate_预热资格判据(t *testing.T) {
	cases := []struct {
		// name 用例名
		name string
		// input 输入
		input SessionCreateInput
		// canonical 归一化模式
		canonical string
		// isTeam 是否团队
		isTeam bool
		// want 期望 prewarm_eligible
		want bool
	}{
		{"swarm 排除", SessionCreateInput{ChannelID: "web", CreateToken: "t"}, "agent", true, false},
		{"cron+web 排除", SessionCreateInput{ChannelID: "web", CreateToken: "t", CronID: "c1"}, "agent", false, false},
		{"非预热模式排除", SessionCreateInput{ChannelID: "web", CreateToken: "t"}, "team", false, false},
		{"正常放行", SessionCreateInput{ChannelID: "web", CreateToken: "t"}, "agent", false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := newTestProvisioner()
			manager := &recordingAgentManager{}
			p.AgentManager = manager
			p.SetSessionOps(&fakeSessionOps{})
			p.extra.ResolveCanonicalMode = func(map[string]any, string) (string, bool) {
				return c.canonical, c.isTeam
			}
			_, _ = p.PrepareSessionCreate(context.Background(), c.input)
			if manager.claimReq.PrewarmEligible != c.want {
				t.Fatalf("prewarm_eligible = %v, want %v", manager.claimReq.PrewarmEligible, c.want)
			}
		})
	}
}

// ──────────────────────────── 覆盖率补齐 ────────────────────────────

// TestProvisioner_Setter注入 校验 3 个 Set* 注入通道。
func TestProvisioner_Setter注入(t *testing.T) {
	p := newTestProvisioner()
	p.SetResolveSessionDir(func(string) (string, bool) { return "/x", true })
	p.SetRemoveSessionDir(func(string) error { return nil })
	if p.extra.ResolveSessionDir == nil || p.extra.RemoveSessionDir == nil {
		t.Fatal("SetResolveSessionDir / SetRemoveSessionDir 应生效")
	}
	dir, ok := p.extra.ResolveSessionDir("s1")
	if dir != "/x" || !ok {
		t.Fatalf("注入的目录解析未生效: (%q, %v)", dir, ok)
	}
}

// TestProvisionError_Error 校验两个错误类型的 Error()。
func TestProvisionError_Error(t *testing.T) {
	if got := (&SessionProvisionError{Message: "boom"}).Error(); got != "boom" {
		t.Fatalf("SessionProvisionError.Error() = %q", got)
	}
	if got := (&SessionProvisionStateError{Message: "state"}).Error(); got != "state" {
		t.Fatalf("SessionProvisionStateError.Error() = %q", got)
	}
}

// TestProvision_WarnOnParticipantError 校验参与者报错走告警分支。
//
// 覆盖 logProvisionWarn 与 dispatchForegroundTransition 的错误吞掉语义。
func TestProvision_WarnOnParticipantError(t *testing.T) {
	p := newTestProvisioner()
	p.participants.ReplaceSessionParticipants([]session.SessionActivityParticipant{
		&failingActivityParticipant{},
	}, nil)
	p.dispatchForegroundTransition(context.Background(),
		session.SessionLifecycleTarget{
			Descriptor: session.SessionDescriptor{SessionID: "t1"},
			Kind:       session.SessionKindAgent,
		}, nil, "view")
	// 未 panic 即通过：错误只告警不外抛
}

// TestDeleteTeamRunner_缺团队名回落Runner 校验降级路径。
func TestDeleteTeamRunner_缺团队名回落Runner(t *testing.T) {
	p := newTestProvisioner()
	called := false
	p.RunnerRelease = func(context.Context, string) error {
		called = true
		return nil
	}
	p.DeleteAgentTeam = func(context.Context, string, []string) (bool, error) {
		t.Fatal("缺团队名时不应调用 DeleteAgentTeam")
		return false, nil
	}
	ok := p.deleteTeamRunner(context.Background(), session.SessionDeleteResult{SessionID: "s1"})
	if !ok || !called {
		t.Fatalf("缺团队名应回落 RunnerRelease，ok=%v called=%v", ok, called)
	}
}

// TestDeleteTeamRunner_DeleteAgentTeam报错返回false 校验失败路径。
func TestDeleteTeamRunner_DeleteAgentTeam报错返回false(t *testing.T) {
	p := newTestProvisioner()
	p.DeleteAgentTeam = func(context.Context, string, []string) (bool, error) {
		return false, errors.New("boom")
	}
	ok := p.deleteTeamRunner(context.Background(), session.SessionDeleteResult{
		SessionID: "s1", TeamName: "t1",
	})
	if ok {
		t.Fatal("DeleteAgentTeam 报错时应返回 false")
	}
}

// TestToLowerSpaces 校验去空白并小写。
func TestToLowerSpaces(t *testing.T) {
	if got := toLowerSpaces("  CODE  "); got != "code" {
		t.Fatalf("toLowerSpaces = %q, want code", got)
	}
}

// failingActivityParticipant 前台变更恒报错的参与者。
type failingActivityParticipant struct{}

// SessionInputIntent 无操作。
func (f *failingActivityParticipant) SessionInputIntent(context.Context, session.SessionInputIntentEvent) (session.SessionInputIntentDisposition, error) {
	return session.SessionInputIntentDispositionNotNeeded, nil
}

// ForegroundChanged 恒报错（覆盖告警分支）。
func (f *failingActivityParticipant) ForegroundChanged(context.Context, session.SessionForegroundEvent) error {
	return errors.New("participant failed")
}

// ExecutionStarted 无操作。
func (f *failingActivityParticipant) ExecutionStarted(context.Context, session.SessionExecutionEvent) error {
	return nil
}

// ExecutionFinished 无操作。
func (f *failingActivityParticipant) ExecutionFinished(session.SessionExecutionFinishedEvent) {}

// SessionInactive 无操作。
func (f *failingActivityParticipant) SessionInactive(context.Context, session.SessionInactiveEvent) error {
	return nil
}
