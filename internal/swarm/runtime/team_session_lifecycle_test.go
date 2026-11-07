package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// fakeTeamLifecycleManager 记录 6 个方法调用并暴露桩行为。
type fakeTeamLifecycleManager struct {
	// events 调用序列
	events []string
	// reporter 由 SetRoundStateReporter 注册的观察者
	reporter func(sessionID string, generation int, active bool)
	// lastEventType 最近一次 BroadcastEvent 的事件类型
	lastEventType string
	// lastPayload 最近一次 BroadcastEvent 的载荷
	lastPayload map[string]any
}

// reportRoundState 触发已注册的轮状态观察者（模拟团队运行时回调）。
func (f *fakeTeamLifecycleManager) reportRoundState(sessionID string, generation int, active bool) {
	if f.reporter != nil {
		f.reporter(sessionID, generation, active)
	}
}

// SetRoundStateReporter 记录注册的观察者，供 reportRoundState 回调。
func (f *fakeTeamLifecycleManager) SetRoundStateReporter(reporter func(sessionID string, generation int, active bool)) {
	f.events = append(f.events, "set_reporter")
	f.reporter = reporter
}

// RemoveWaiter 桩。
func (f *fakeTeamLifecycleManager) RemoveWaiter(sessionID, requestID string, generation *int) {
	f.events = append(f.events, "remove_waiter")
}

// PauseSessionRuntime 已实现。
func (f *fakeTeamLifecycleManager) PauseSessionRuntime(_ context.Context, sessionID, reason string, generation *int) (bool, error) {
	f.events = append(f.events, "pause")
	return true, nil
}

// StopSessionRuntime 已实现。
func (f *fakeTeamLifecycleManager) StopSessionRuntime(_ context.Context, sessionID, reason string, stopRunner bool, generation *int) (bool, error) {
	f.events = append(f.events, "stop")
	return true, nil
}

// BroadcastEvent 记录事件类型与载荷。
func (f *fakeTeamLifecycleManager) BroadcastEvent(sessionID, event string, payload map[string]any) {
	f.events = append(f.events, "broadcast")
	f.lastEventType = event
	f.lastPayload = payload
}

// IsGenerationRunning 桩。
func (f *fakeTeamLifecycleManager) IsGenerationRunning(sessionID string, generation int) bool {
	f.events = append(f.events, "is_running")
	return false
}

// fakeRoundStateCoordinator 轮状态协调器桩。
type fakeRoundStateCoordinator struct {
	// events 调用序列
	events []string
}

// ResumeWaitingForInput 记录带参数的调用。
func (f *fakeRoundStateCoordinator) ResumeWaitingForInput(sessionID string, generation int) {
	f.events = append(f.events, "resume:"+sessionID+":"+itoa(generation))
}

// MarkSessionWaitingForInput 记录带参数的调用。
func (f *fakeRoundStateCoordinator) MarkSessionWaitingForInput(sessionID string, generation int) {
	f.events = append(f.events, "mark_waiting:"+sessionID+":"+itoa(generation))
}

// itoa 整数转字符串（避免为一处格式化引入 strconv）。
func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestTeamLifecycle_RemoveResponseWaiter 校验移除等待者。
func TestTeamLifecycle_RemoveResponseWaiter(t *testing.T) {
	manager := &fakeTeamLifecycleManager{}
	adapter := NewTeamSessionLifecycleAdapter(manager, &fakeRoundStateCoordinator{})
	err := adapter.RemoveResponseWaiter(context.Background(), session.ExecutionLifecycleContext{
		Session:     session.SessionLifecycleContext{SessionID: "s1", Generation: 1},
		ExecutionID: "e1", RequestID: "r1",
	}, "reason")
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !containsString(manager.events, "remove_waiter") {
		t.Fatalf("应调用 RemoveWaiter，实际 %v", manager.events)
	}
}

// TestTeamLifecycle_PauseSession 校验暂停。
func TestTeamLifecycle_PauseSession(t *testing.T) {
	manager := &fakeTeamLifecycleManager{}
	adapter := NewTeamSessionLifecycleAdapter(manager, &fakeRoundStateCoordinator{})
	paused, err := adapter.PauseSession(context.Background(),
		session.SessionLifecycleContext{SessionID: "s1", Generation: 1}, "reason", "web")
	if err != nil || !paused {
		t.Fatalf("PauseSession = (%v, %v), want (true, nil)", paused, err)
	}
	if !containsString(manager.events, "pause") {
		t.Fatalf("应调用 PauseSessionRuntime，实际 %v", manager.events)
	}
}

// TestTeamLifecycle_StopSession 校验停止。
func TestTeamLifecycle_StopSession(t *testing.T) {
	manager := &fakeTeamLifecycleManager{}
	adapter := NewTeamSessionLifecycleAdapter(manager, &fakeRoundStateCoordinator{})
	if err := adapter.StopSession(context.Background(),
		session.SessionLifecycleContext{SessionID: "s1", Generation: 1}, "reason"); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !containsString(manager.events, "stop") {
		t.Fatalf("应调用 StopSessionRuntime，实际 %v", manager.events)
	}
}

// TestTeamLifecycle_PublishPauseCompleted 校验发布暂停完成。
func TestTeamLifecycle_PublishPauseCompleted(t *testing.T) {
	manager := &fakeTeamLifecycleManager{}
	adapter := NewTeamSessionLifecycleAdapter(manager, &fakeRoundStateCoordinator{})
	if err := adapter.PublishPauseCompleted(context.Background(), "s1", nil, "web"); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !containsString(manager.events, "broadcast") {
		t.Fatalf("应广播事件，实际 %v", manager.events)
	}
}

// TestTeamLifecycle_IsRunning 校验运行探针。
func TestTeamLifecycle_IsRunning(t *testing.T) {
	manager := &fakeTeamLifecycleManager{}
	adapter := NewTeamSessionLifecycleAdapter(manager, &fakeRoundStateCoordinator{})
	if adapter.IsRunning(session.SessionLifecycleContext{SessionID: "s1", Generation: 1}) {
		t.Fatal("桩实现应返回 false")
	}
	if !containsString(manager.events, "is_running") {
		t.Fatalf("应调用 IsGenerationRunning，实际 %v", manager.events)
	}
}

// TestTeamLifecycle_实现Runtime端口 校验编译期接口满足。
func TestTeamLifecycle_实现Runtime端口(t *testing.T) {
	var _ session.SessionRuntimeLifecyclePort = (*TeamSessionLifecycleAdapter)(nil)
}

// ──────────────────────────── 删除事务 fake ────────────────────────────

// deleteEventRecorder 记录删除事务各阶段。
type deleteEventRecorder struct {
	// events 阶段序列
	events []string
	// commitFail 轨迹提交是否注入失败
	commitFail bool
}

// add 记录一个事件。
func (r *deleteEventRecorder) add(e string) { r.events = append(r.events, e) }

// has 是否含某事件。
func (r *deleteEventRecorder) has(e string) bool {
	for _, v := range r.events {
		if v == e {
			return true
		}
	}
	return false
}

// count 某事件出现次数。
func (r *deleteEventRecorder) count(e string) int {
	n := 0
	for _, v := range r.events {
		if v == e {
			n++
		}
	}
	return n
}

// recordingDeleteParticipant 记录参与者回调。
type recordingDeleteParticipant struct {
	// recorder 事件记录器
	recorder *deleteEventRecorder
}

// Name 参与者名称。
func (*recordingDeleteParticipant) Name() string { return "recording" }

// BeforeDelete 记录事件。
func (p *recordingDeleteParticipant) BeforeDelete(context.Context, session.SessionLifecycleTarget) error {
	p.recorder.add("participant.before")
	return nil
}

// ReleaseResources 记录事件。
func (p *recordingDeleteParticipant) ReleaseResources(context.Context, session.SessionLifecycleTarget) error {
	p.recorder.add("participant.release")
	return nil
}

// DeleteFailed 记录事件。
func (p *recordingDeleteParticipant) DeleteFailed(_ context.Context,
	_ session.SessionLifecycleTarget, destructive bool) error {
	if destructive {
		p.recorder.add("participant.failed.true")
	} else {
		p.recorder.add("participant.failed.false")
	}
	return nil
}

// DeleteCommitted 记录事件。
func (p *recordingDeleteParticipant) DeleteCommitted(context.Context, session.SessionLifecycleTarget) error {
	p.recorder.add("participant.committed")
	return nil
}

// recordingTrajectoryHook 记录轨迹钩子。
type recordingTrajectoryHook struct {
	// recorder 事件记录器
	recorder *deleteEventRecorder
}

// BeginSessionDelete 记录。
func (h *recordingTrajectoryHook) BeginSessionDelete(context.Context, string) error {
	h.recorder.add("trajectory.begin")
	return nil
}

// AbortSessionDelete 记录。
func (h *recordingTrajectoryHook) AbortSessionDelete(context.Context, string) error {
	h.recorder.add("trajectory.abort")
	return nil
}

// CommitSessionDelete 记录（可注入失败）。
func (h *recordingTrajectoryHook) CommitSessionDelete(context.Context, string) error {
	if h.recorder.commitFail {
		return errors.New("trajectory commit failed")
	}
	h.recorder.add("trajectory.commit")
	return nil
}

// recordingTeamExecutionController 记录团队执行控制面调用。
type recordingTeamExecutionController struct {
	// recorder 事件记录器
	recorder *deleteEventRecorder
}

// QuiesceForDelete 记录。
func (c *recordingTeamExecutionController) QuiesceForDelete(context.Context,
	session.SessionLifecycleTarget, string) error {
	c.recorder.add("team.quiesce")
	return nil
}

// DisposeAfterResourceRelease 记录。
func (c *recordingTeamExecutionController) DisposeAfterResourceRelease(context.Context,
	session.SessionLifecycleTarget, string) error {
	c.recorder.add("team.dispose")
	return nil
}

// DeleteAborted 记录。
func (c *recordingTeamExecutionController) DeleteAborted(session.SessionLifecycleTarget) {
	c.recorder.add("team.aborted")
}

// DeleteCommitted 记录。
func (c *recordingTeamExecutionController) DeleteCommitted(session.SessionLifecycleTarget) {
	c.recorder.add("team.committed")
}

// recordingDeleteLifecycle 记录删除生命周期。
type recordingDeleteLifecycle struct {
	// recorder 事件记录器
	recorder *deleteEventRecorder
}

// BeginSessionDelete 记录。
func (l *recordingDeleteLifecycle) BeginSessionDelete(string) error {
	l.recorder.add("lifecycle.begin")
	return nil
}

// AbortSessionDelete 记录。
func (l *recordingDeleteLifecycle) AbortSessionDelete(string, string) error {
	l.recorder.add("lifecycle.abort")
	return nil
}

// CommitSessionDelete 记录。
func (l *recordingDeleteLifecycle) CommitSessionDelete(string) error {
	l.recorder.add("lifecycle.commit")
	return nil
}

// recordingPlanController 记录 reset。
type recordingPlanController struct {
	// recorder 事件记录器
	recorder *deleteEventRecorder
}

// ResetSession 记录。
func (c *recordingPlanController) ResetSession(string) { c.recorder.add("plan.reset") }

// newTestDeleteProvisioner 构造可完整走删除事务的 provisioner。
func newTestDeleteProvisioner(recorder *deleteEventRecorder) *RuntimeSessionProvisioner {
	p := newTestProvisioner()
	p.PlanController = &recordingPlanController{recorder: recorder}
	p.TrajectoryHook = &recordingTrajectoryHook{recorder: recorder}
	p.DeleteLifecycle = &recordingDeleteLifecycle{recorder: recorder}
	p.extra.ResolveSessionDir = func(string) (string, bool) { return "/tmp/session", true }
	p.extra.RemoveSessionDir = func(string) error { return nil }
	p.CheckpointEnsure = func(context.Context) error { return nil }
	p.RunnerRelease = func(context.Context, string) error {
		recorder.add("runner.release")
		return nil
	}
	p.RemoveMetadataCache = func(string) {}
	p.SetSessionOps(&fakeSessionOps{metadata: map[string]map[string]any{
		"s1": {"mode": "agent.plan", "channel_id": "web"},
	}})
	p.participants.ReplaceSessionParticipants(nil, []session.SessionDeleteParticipant{
		&recordingDeleteParticipant{recorder: recorder},
	})
	return p
}

// ──────────────────────────── 删除事务测试 ────────────────────────────

// TestDelete_空会话标识报BAD_REQUEST 校验参数校验。
func TestDelete_空会话标识报BAD_REQUEST(t *testing.T) {
	p := newTestProvisioner()
	res, err := p.DeleteSession(context.Background(), DeleteSessionOptions{})
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if res.OK || res.ErrorCode != "BAD_REQUEST" || res.ErrorMessage != "session_id is required" {
		t.Fatalf("应是 BAD_REQUEST，实际 %+v", res)
	}
}

// TestDelete_会话目录不存在报NOT_FOUND 校验存在性。
func TestDelete_会话目录不存在报NOT_FOUND(t *testing.T) {
	p := newTestProvisioner()
	p.extra.ResolveSessionDir = func(string) (string, bool) { return "", false }
	res, _ := p.DeleteSession(context.Background(), DeleteSessionOptions{SessionID: "s1"})
	if res.OK || res.ErrorCode != "NOT_FOUND" {
		t.Fatalf("应是 NOT_FOUND，实际 %+v", res)
	}
}

// TestDelete_七阶段顺序 校验 destructive_started 分界前的阶段顺序。
func TestDelete_七阶段顺序(t *testing.T) {
	recorder := &deleteEventRecorder{}
	p := newTestDeleteProvisioner(recorder)
	res, err := p.DeleteSession(context.Background(), DeleteSessionOptions{
		ChannelID: "web", SessionID: "s1",
		QuiesceSession: func(context.Context, string, string) error {
			recorder.add("quiesce")
			return nil
		},
		DisposeSession: func(context.Context, string, string) error {
			recorder.add("dispose")
			return nil
		},
	})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !res.OK || !res.Deleted {
		t.Fatalf("应删除成功，实际 %+v", res)
	}
	want := []string{
		"participant.before",
		"trajectory.begin",
		"lifecycle.begin",
		"quiesce",
		"participant.release",
		"dispose",
		"runner.release",
		"trajectory.commit",
		"lifecycle.commit",
		"plan.reset",
		"participant.committed",
	}
	if len(recorder.events) != len(want) {
		t.Fatalf("阶段数应为 %d，实际 %d：%v", len(want), len(recorder.events), recorder.events)
	}
	for i := range want {
		if recorder.events[i] != want[i] {
			t.Fatalf("阶段顺序错误：第 %d 步应为 %q，实际 %v", i+1, want[i], recorder.events)
		}
	}
}

// TestDelete_破坏性前失败可回滚 校验 destructive_started=false 的恢复。
func TestDelete_破坏性前失败可回滚(t *testing.T) {
	recorder := &deleteEventRecorder{}
	p := newTestDeleteProvisioner(recorder)
	res, _ := p.DeleteSession(context.Background(), DeleteSessionOptions{
		ChannelID: "web", SessionID: "s1",
		QuiesceSession: func(context.Context, string, string) error {
			return errors.New("quiesce failed")
		},
		DisposeSession: func(context.Context, string, string) error { return nil },
	})
	if res.OK || res.RecoveryRequired {
		t.Fatalf("破坏性前失败应可回滚（RecoveryRequired=false），实际 %+v", res)
	}
	if res.ErrorCode != "DELETE_FAILED" {
		t.Fatalf("错误码应为 DELETE_FAILED，实际 %q", res.ErrorCode)
	}
	if !recorder.has("trajectory.abort") || !recorder.has("lifecycle.abort") {
		t.Fatalf("应回滚轨迹与生命周期，实际 %v", recorder.events)
	}
	if recorder.has("participant.release") {
		t.Fatal("破坏性前失败不应释放参与者资源")
	}
	if !recorder.has("participant.failed.false") {
		t.Fatalf("参与者应收到 destructive_started=false 的失败通知，实际 %v", recorder.events)
	}
}

// TestDelete_破坏性后失败需恢复 校验 destructive_started=true 的语义。
func TestDelete_破坏性后失败需恢复(t *testing.T) {
	recorder := &deleteEventRecorder{}
	p := newTestDeleteProvisioner(recorder)
	res, _ := p.DeleteSession(context.Background(), DeleteSessionOptions{
		ChannelID: "web", SessionID: "s1",
		QuiesceSession: func(context.Context, string, string) error { return nil },
		DisposeSession: func(context.Context, string, string) error {
			return errors.New("dispose failed")
		},
	})
	if res.OK || !res.RecoveryRequired {
		t.Fatalf("破坏性后失败应 RecoveryRequired=true，实际 %+v", res)
	}
	if recorder.has("trajectory.abort") || recorder.has("lifecycle.abort") {
		t.Fatalf("破坏性已开始不应回滚，实际 %v", recorder.events)
	}
	if !recorder.has("participant.failed.true") {
		t.Fatalf("参与者应收到 destructive_started=true 的失败通知，实际 %v", recorder.events)
	}
}

// TestDelete_成功后写重试缓存 校验 60 秒幂等。
func TestDelete_成功后写重试缓存(t *testing.T) {
	recorder := &deleteEventRecorder{}
	p := newTestDeleteProvisioner(recorder)
	first, _ := p.DeleteSession(context.Background(), DeleteSessionOptions{
		ChannelID: "web", SessionID: "s1",
		QuiesceSession: func(context.Context, string, string) error { return nil },
		DisposeSession: func(context.Context, string, string) error { return nil },
	})
	// 第二次：目录已不存在，应命中缓存
	p.extra.ResolveSessionDir = func(string) (string, bool) { return "", false }
	second, _ := p.DeleteSession(context.Background(), DeleteSessionOptions{
		ChannelID: "web", SessionID: "s1",
	})
	if first != second {
		t.Fatalf("重试应返回缓存结果，first=%+v second=%+v", first, second)
	}
}

// TestDelete_缓存过期后回到NOT_FOUND 校验 TTL。
func TestDelete_缓存过期后回到NOT_FOUND(t *testing.T) {
	now := time.Now()
	p := newTestProvisioner()
	p.nowFunc = func() time.Time { return now }
	p.completedDeletes = newRecentDeleteResults[session.SessionDeleteResult](256, 60*time.Second, p.nowFunc)
	p.extra.ResolveSessionDir = func(string) (string, bool) { return "/tmp/s", true }
	p.extra.RemoveSessionDir = func(string) error { return nil }
	p.CheckpointEnsure = func(context.Context) error { return nil }
	p.SetSessionOps(&fakeSessionOps{})
	p.DeleteSession(context.Background(), DeleteSessionOptions{
		ChannelID: "web", SessionID: "s1",
		QuiesceSession: func(context.Context, string, string) error { return nil },
		DisposeSession: func(context.Context, string, string) error { return nil },
	})
	now = now.Add(61 * time.Second)
	p.extra.ResolveSessionDir = func(string) (string, bool) { return "", false }
	res, _ := p.DeleteSession(context.Background(), DeleteSessionOptions{SessionID: "s1"})
	if res.ErrorCode != "NOT_FOUND" {
		t.Fatalf("缓存过期后应回到 NOT_FOUND，实际 %+v", res)
	}
}

// TestDelete_并发双删幂等 校验 session 级锁。
func TestDelete_并发双删幂等(t *testing.T) {
	recorder := &deleteEventRecorder{}
	p := newTestDeleteProvisioner(recorder)
	// 目录在删除成功后应消失：第二次进入时走缓存/NOT_FOUND 分支
	existed := true
	p.extra.ResolveSessionDir = func(string) (string, bool) { return "/tmp/session", existed }
	p.extra.RemoveSessionDir = func(string) error {
		existed = false
		return nil
	}
	var wg sync.WaitGroup
	results := make([]session.SessionDeleteResult, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			results[n], _ = p.DeleteSession(context.Background(), DeleteSessionOptions{
				ChannelID: "web", SessionID: "s1",
				QuiesceSession: func(context.Context, string, string) error {
					recorder.add("quiesce")
					return nil
				},
				DisposeSession: func(context.Context, string, string) error { return nil },
			})
		}(i)
	}
	wg.Wait()
	if !results[0].Deleted && !results[1].Deleted {
		t.Fatal("至少一次应成功删除")
	}
	if got := recorder.count("quiesce"); got != 1 {
		t.Fatalf("quiesce 应只执行一次（锁串行化），实际 %d", got)
	}
}

// TestDelete_checkpoint不可用报错 校验依赖检查。
func TestDelete_checkpoint不可用报错(t *testing.T) {
	p := newTestProvisioner()
	p.CheckpointEnsure = func(context.Context) error { return errors.New("no checkpointer") }
	p.extra.ResolveSessionDir = func(string) (string, bool) { return "/tmp/x", true }
	res, _ := p.DeleteSession(context.Background(), DeleteSessionOptions{SessionID: "s1"})
	if res.ErrorCode != "CHECKPOINT_UNAVAILABLE" {
		t.Fatalf("应是 CHECKPOINT_UNAVAILABLE，实际 %+v", res)
	}
}

// TestDelete_提交失败标记DELETE_COMMIT_PENDING 校验提交阶段失败。
func TestDelete_提交失败标记DELETE_COMMIT_PENDING(t *testing.T) {
	recorder := &deleteEventRecorder{commitFail: true}
	p := newTestDeleteProvisioner(recorder)
	res, _ := p.DeleteSession(context.Background(), DeleteSessionOptions{
		ChannelID: "web", SessionID: "s1",
		QuiesceSession: func(context.Context, string, string) error { return nil },
		DisposeSession: func(context.Context, string, string) error { return nil },
	})
	if res.OK || res.ErrorCode != "DELETE_COMMIT_PENDING" || !res.Deleted || !res.RecoveryRequired {
		t.Fatalf("应是 DELETE_COMMIT_PENDING 且 deleted=true recovery=true，实际 %+v", res)
	}
}

// ──────────────────────────── 团队删除测试 ────────────────────────────

// TestDeleteTeam_空团队名报错 校验参数校验。
func TestDeleteTeam_空团队名报错(t *testing.T) {
	p := newTestProvisioner()
	res, err := p.DeleteTeam(context.Background(), "", "web")
	if err != nil {
		t.Fatalf("不应返回 error: %v", err)
	}
	if res.OK || res.ErrorCode != "BAD_REQUEST" {
		t.Fatalf("应是 BAD_REQUEST，实际 %+v", res)
	}
}

// TestDeleteTeam_会话逐个释放后Runner一次 校验编排顺序。
func TestDeleteTeam_会话逐个释放后Runner一次(t *testing.T) {
	recorder := &deleteEventRecorder{}
	p := newTestProvisioner()
	p.extra.ResolveSessionDir = func(string) (string, bool) { return "/tmp/s", true }
	p.extra.RemoveSessionDir = func(string) error {
		recorder.add("rmtree")
		return nil
	}
	p.CheckpointEnsure = func(context.Context) error { return nil }
	p.RunnerRelease = func(context.Context, string) error {
		recorder.add("runner.release")
		return nil
	}
	p.DeleteAgentTeam = func(_ context.Context, teamName string, sessionIDs []string) (bool, error) {
		recorder.add("runner.team")
		return true, nil
	}
	p.extra.ListTeamSessions = func(teamName string) []string { return []string{"s1", "s2"} }
	p.TeamExecutionController = &recordingTeamExecutionController{recorder: recorder}
	p.SetSessionOps(&fakeSessionOps{metadata: map[string]map[string]any{
		"s1": {"mode": "team", "channel_id": "web", "team_name": "t1"},
		"s2": {"mode": "team", "channel_id": "web", "team_name": "t1"},
	}})
	res, err := p.DeleteTeam(context.Background(), "t1", "web")
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if !res.OK {
		t.Fatalf("应成功，实际 %+v", res)
	}
	if len(res.SessionIDs) != 2 {
		t.Fatalf("应删除 2 个会话，实际 %v", res.SessionIDs)
	}
	// 团队会话走 _delete_team_runner（DeleteAgentTeam），不走单 agent 的 Runner.release
	// quiesce 3 次 = 团队整体 1 次（delete_team_locked 入口）+ 每会话各 1 次
	if got := recorder.count("team.quiesce"); got != 3 {
		t.Fatalf("应为 1 次团队整体 + 2 次会话级 quiesce，实际 %d", got)
	}
	if got := recorder.count("team.dispose"); got != 2 {
		t.Fatalf("每个团队会话应 dispose 一次，实际 %d", got)
	}
	// runner.team 3 次 = 每会话各 1 次（_delete_team_runner）+ delete_team_locked 末尾整体 1 次
	if got := recorder.count("runner.team"); got != 3 {
		t.Fatalf("应为 2 次会话级 + 1 次团队整体 Runner 删除，实际 %d", got)
	}
	if got := recorder.count("rmtree"); got != 2 {
		t.Fatalf("每个会话应删一次目录，实际 %d", got)
	}
}

// ──────────────────────────── 10.3.28c 补齐（对齐上游 197 行版本） ────────────────────────────

// TestTeamLifecycle_构造时注册轮状态观察者 校验 __init__ 末尾的
// set_round_state_reporter(self._report_round_state)。
//
// 上游：team_session_lifecycle.py L100-105
func TestTeamLifecycle_构造时注册轮状态观察者(t *testing.T) {
	manager := &fakeTeamLifecycleManager{}
	NewTeamSessionLifecycleAdapter(manager, &fakeRoundStateCoordinator{})
	if !containsString(manager.events, "set_reporter") {
		t.Fatalf("构造时必须注册轮状态观察者，实际调用序列 %v", manager.events)
	}
}

// TestTeamLifecycle_轮状态运行中恢复等待输入 校验 _report_round_state(running=true)。
//
// 上游：L107-120，running 为真 → coordinator.resume_waiting_for_input
func TestTeamLifecycle_轮状态运行中恢复等待输入(t *testing.T) {
	manager := &fakeTeamLifecycleManager{}
	coordinator := &fakeRoundStateCoordinator{}
	NewTeamSessionLifecycleAdapter(manager, coordinator)
	manager.reportRoundState("s1", 3, true)
	if len(coordinator.events) != 1 || coordinator.events[0] != "resume:s1:3" {
		t.Fatalf("running=true 应调用 resume_waiting_for_input(s1,3)，实际 %v", coordinator.events)
	}
}

// TestTeamLifecycle_轮状态空闲标记等待输入 校验 _report_round_state(running=false)。
//
// 上游：L107-120，running 为假 → coordinator.mark_session_waiting_for_input
func TestTeamLifecycle_轮状态空闲标记等待输入(t *testing.T) {
	manager := &fakeTeamLifecycleManager{}
	coordinator := &fakeRoundStateCoordinator{}
	NewTeamSessionLifecycleAdapter(manager, coordinator)
	manager.reportRoundState("s2", 5, false)
	if len(coordinator.events) != 1 || coordinator.events[0] != "mark_waiting:s2:5" {
		t.Fatalf("running=false 应调用 mark_session_waiting_for_input(s2,5)，实际 %v", coordinator.events)
	}
}

// TestTeamLifecycle_发布暂停完成的事件契约 校验 event_type 与 payload 字段。
//
// 上游：L165-186 —— event_type 必须是 chat.processing_status，
// 且带 is_processing=False / is_complete=True。
// 前端按 event_type 分支，字符串不得改动。
func TestTeamLifecycle_发布暂停完成的事件契约(t *testing.T) {
	manager := &fakeTeamLifecycleManager{}
	coordinator := &fakeRoundStateCoordinator{}
	adapter := NewTeamSessionLifecycleAdapter(manager, coordinator)
	generation := 4
	if err := adapter.PublishPauseCompleted(context.Background(),
		"s3", &generation, "web"); err != nil {
		t.Fatalf("失败: %v", err)
	}

	if manager.lastEventType != "chat.processing_status" {
		t.Fatalf("event_type = %q, want chat.processing_status", manager.lastEventType)
	}
	if got, ok := manager.lastPayload["is_processing"].(bool); !ok || got {
		t.Fatalf("is_processing 应为 false，实际 %v", manager.lastPayload["is_processing"])
	}
	if got, ok := manager.lastPayload["is_complete"].(bool); !ok || !got {
		t.Fatalf("is_complete 应为 true，实际 %v", manager.lastPayload["is_complete"])
	}
	if manager.lastPayload["session_id"] != "s3" {
		t.Fatalf("session_id = %v, want s3", manager.lastPayload["session_id"])
	}
}

// TestTeamLifecycle_发布暂停完成时刷新轮状态 校验 generation 非空时
// 先 mark_session_waiting_for_input。
//
// 上游：L170-175 —— `if generation is not None: mark_session_waiting_for_input(...)`
func TestTeamLifecycle_发布暂停完成时刷新轮状态(t *testing.T) {
	manager := &fakeTeamLifecycleManager{}
	coordinator := &fakeRoundStateCoordinator{}
	adapter := NewTeamSessionLifecycleAdapter(manager, coordinator)
	generation := 7
	if err := adapter.PublishPauseCompleted(context.Background(),
		"s4", &generation, "web"); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if len(coordinator.events) != 1 || coordinator.events[0] != "mark_waiting:s4:7" {
		t.Fatalf("generation 非空时应先标记等待输入，实际 %v", coordinator.events)
	}
}

// TestTeamLifecycle_发布暂停完成时generation为空不刷新轮状态 校验反向。
func TestTeamLifecycle_发布暂停完成时generation为空不刷新轮状态(t *testing.T) {
	manager := &fakeTeamLifecycleManager{}
	coordinator := &fakeRoundStateCoordinator{}
	adapter := NewTeamSessionLifecycleAdapter(manager, coordinator)
	if err := adapter.PublishPauseCompleted(context.Background(),
		"s5", nil, "web"); err != nil {
		t.Fatalf("失败: %v", err)
	}
	if len(coordinator.events) != 0 {
		t.Fatalf("generation 为空时不应刷新轮状态，实际 %v", coordinator.events)
	}
}

// containsString 判断切片是否含目标字符串。
func containsString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}
