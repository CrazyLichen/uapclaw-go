package session

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// newTestCoordinator 构造默认协调器。
func newTestCoordinator() *RuntimeSessionCoordinator {
	return NewRuntimeSessionCoordinator(DefaultCoordinatorOptions())
}

// mustRegister 注册会话并返回快照。
func mustRegister(t *testing.T, c *RuntimeSessionCoordinator, sid string) RuntimeSessionSnapshot {
	t.Helper()
	snap, err := c.RegisterSession(sid, "web", SessionPersistencePolicyPersistent)
	if err != nil {
		t.Fatalf("RegisterSession(%q) 失败: %v", sid, err)
	}
	return snap
}

// intPtrLocal 取 int 指针（测试用）。
func intPtrLocal(v int) *int { return &v }

// TestCoordinator_Register首次代次为1 校验代次起点与初始状态。
func TestCoordinator_Register首次代次为1(t *testing.T) {
	c := newTestCoordinator()
	snap := mustRegister(t, c, "s1")
	if snap.Generation != 1 {
		t.Fatalf("首次注册代次应为 1，实际 %d", snap.Generation)
	}
	if snap.State != RuntimeSessionStateReady {
		t.Fatalf("初始状态应为 READY，实际 %s", snap.State)
	}
	if snap.ChannelID != "web" {
		t.Fatalf("渠道标识应为 web，实际 %q", snap.ChannelID)
	}
}

// TestCoordinator_Register重复注册不递增代次 校验同一活跃会话。
func TestCoordinator_Register重复注册不递增代次(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	snap := mustRegister(t, c, "s1")
	if snap.Generation != 1 {
		t.Fatalf("重复注册不应递增代次，实际 %d", snap.Generation)
	}
}

// TestCoordinator_Register空标识报错 校验参数校验。
func TestCoordinator_Register空标识报错(t *testing.T) {
	c := newTestCoordinator()
	if _, err := c.RegisterSession("  ", "web", SessionPersistencePolicyPersistent); err == nil {
		t.Fatal("空 session_id 应报错")
	}
}

// TestCoordinator_Register渠道归一 校验空渠道回落 default。
func TestCoordinator_Register渠道归一(t *testing.T) {
	c := newTestCoordinator()
	snap, err := c.RegisterSession("s1", "", SessionPersistencePolicyPersistent)
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if snap.ChannelID != "default" {
		t.Fatalf("空渠道应归一为 default，实际 %q", snap.ChannelID)
	}
}

// TestCoordinator_Close后注册报错 校验关闭后拒绝。
//
// Close 在 Task 10 落地；此处用 accepting 字段直接验证语义。
func TestCoordinator_Close后注册报错(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	c.mu.Lock()
	c.accepting = false
	c.mu.Unlock()
	if _, err := c.RegisterSession("s2", "web", SessionPersistencePolicyPersistent); err == nil {
		t.Fatal("关闭后注册应报错")
	}
	if _, err := c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindChatUnary,
		func(context.Context) (any, error) { return nil, nil }, UnaryOptions{}); err == nil {
		t.Fatal("关闭后 RunUnary 应报错")
	}
}

// TestCoordinator_RunUnary返回结果并置终态 校验主流程。
func TestCoordinator_RunUnary返回结果并置终态(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	got, err := c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindChatUnary,
		func(context.Context) (any, error) { return "answer", nil }, UnaryOptions{})
	if err != nil || got != "answer" {
		t.Fatalf("RunUnary = (%v, %v), want (answer, nil)", got, err)
	}
	snap := c.SnapshotSession("s1")
	if snap == nil || len(snap.Executions) != 1 {
		t.Fatalf("应有 1 条执行记录，实际 %+v", snap)
	}
	if snap.Executions[0].State != SessionExecutionStateSucceeded {
		t.Fatalf("执行应为 SUCCEEDED，实际 %s", snap.Executions[0].State)
	}
	// MarkTerminal 关闭 Terminal；Done 由执行方 defer 关闭
	handle := c.registry.Get(snap.Executions[0].ExecutionID)
	if handle == nil {
		// 已被回收也算正常（RetainOwnerTask=false 且 Done 已关）
		return
	}
	if !handle.Terminal.IsTerminalClosed() {
		t.Fatal("Terminal 应已关闭")
	}
}

// TestCoordinator_RunUnary失败置FAILED 校验错误路径。
func TestCoordinator_RunUnary失败置FAILED(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	boom := errors.New("boom")
	_, err := c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindChatUnary,
		func(context.Context) (any, error) { return nil, boom }, UnaryOptions{})
	if !errors.Is(err, boom) {
		t.Fatalf("应返回原错误，实际 %v", err)
	}
	snap := c.SnapshotSession("s1")
	if snap.Executions[0].State != SessionExecutionStateFailed {
		t.Fatalf("执行应为 FAILED，实际 %s", snap.Executions[0].State)
	}
	if snap.Executions[0].Error != "boom" {
		t.Fatalf("Error 应记录原文，实际 %q", snap.Executions[0].Error)
	}
}

// TestCoordinator_RunUnarypanic置FAILED并重抛 校验 panic 兜底。
//
// 用非 scheduled 种类（HEARTBEAT），使执行发生在调用方 goroutine，
// 测试才能 recover 到重抛的 panic；scheduled 类会在 lane goroutine 里跑。
func TestCoordinator_RunUnarypanic置FAILED并重抛(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("panic 应重新抛出")
			}
		}()
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) { panic("boom") }, UnaryOptions{})
	}()
	snap := c.SnapshotSession("s1")
	if snap.Executions[0].State != SessionExecutionStateFailed {
		t.Fatalf("panic 后执行应为 FAILED，实际 %s", snap.Executions[0].State)
	}
}

// TestCoordinator_未注册会话报错 校验会话必须先注册。
func TestCoordinator_未注册会话报错(t *testing.T) {
	c := newTestCoordinator()
	_, err := c.RunUnary(context.Background(), "nope", "r1", SessionWorkKindChatUnary,
		func(context.Context) (any, error) { return nil, nil }, UnaryOptions{})
	if err == nil {
		t.Fatal("未注册会话应报错")
	}
	if want := "session is not registered: nope"; err.Error() != want {
		t.Fatalf("错误文案 = %q, want %q", err.Error(), want)
	}
}

// TestCoordinator_QUIESCING会话拒绝新工作 校验静默期闸门。
func TestCoordinator_QUIESCING会话拒绝新工作(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	lifecycleCtx, err := c.QuiesceSession("s1", nil)
	if err != nil {
		t.Fatalf("QuiesceSession 失败: %v", err)
	}
	if lifecycleCtx == nil || lifecycleCtx.Generation != 1 {
		t.Fatalf("应返回代次 1 的上下文，实际 %+v", lifecycleCtx)
	}
	_, err = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindChatUnary,
		func(context.Context) (any, error) { return nil, nil }, UnaryOptions{})
	if err == nil {
		t.Fatal("QUIESCING 会话应拒绝新工作")
	}
	if want := "session is not accepting work: s1"; err.Error() != want {
		t.Fatalf("错误文案 = %q, want %q", err.Error(), want)
	}
}

// TestCoordinator_QuiesceSession代次不符返回nil 校验代次判定。
func TestCoordinator_QuiesceSession代次不符返回nil(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	other := 9
	got, err := c.QuiesceSession("s1", &other)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got != nil {
		t.Fatalf("代次不符应返回 nil，实际 %+v", got)
	}
}

// TestCoordinator_QuiesceSession未注册返回nil 校验空值语义。
func TestCoordinator_QuiesceSession未注册返回nil(t *testing.T) {
	c := newTestCoordinator()
	got, err := c.QuiesceSession("nope", nil)
	if err != nil || got != nil {
		t.Fatalf("未注册应返回 (nil, nil)，实际 (%v, %v)", got, err)
	}
}

// TestCoordinator_SnapshotSessions返回全部 校验批量快照。
func TestCoordinator_SnapshotSessions返回全部(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	mustRegister(t, c, "s2")
	if got := len(c.SnapshotSessions()); got != 2 {
		t.Fatalf("应有 2 个会话快照，实际 %d", got)
	}
}

// TestCoordinator_SnapshotSession未注册返回nil 校验空值语义。
func TestCoordinator_SnapshotSession未注册返回nil(t *testing.T) {
	c := newTestCoordinator()
	if got := c.SnapshotSession("nope"); got != nil {
		t.Fatalf("未注册应返回 nil，实际 %+v", got)
	}
}

// TestCoordinator_GetExecution 校验单条执行查询。
func TestCoordinator_GetExecution(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	if _, err := c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindChatUnary,
		func(context.Context) (any, error) { return nil, nil }, UnaryOptions{}); err != nil {
		t.Fatalf("失败: %v", err)
	}
	snap := c.SnapshotSession("s1")
	eid := snap.Executions[0].ExecutionID
	got := c.GetExecution(eid)
	if got == nil || got.ExecutionID != eid {
		t.Fatalf("GetExecution 应返回该执行，实际 %+v", got)
	}
	if c.GetExecution("nope") != nil {
		t.Fatal("不存在的执行应返回 nil")
	}
}

// TestCoordinator_newExecution父执行继承根身份 校验父子关系。
func TestCoordinator_newExecution父执行继承根身份(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	released := make(chan struct{})
	go func() {
		_, _ = c.RunUnary(context.Background(), "s1", "parent", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-released
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	child, err := c.NewExecutionForTest("s1", "child", SessionWorkKindSessionInput,
		ChildOptions{ParentRequestID: "parent"})
	if err != nil {
		t.Fatalf("创建子执行失败: %v", err)
	}
	if child.ParentExecutionID == "" {
		t.Fatal("子执行应有父执行标识")
	}
	if child.RootRequestID != "parent" {
		t.Fatalf("子执行根请求应为 parent，实际 %q", child.RootRequestID)
	}
	if child.RootWorkKind != SessionWorkKindHeartbeat {
		t.Fatalf("子执行根种类应为 heartbeat，实际 %s", child.RootWorkKind)
	}
	close(released)
}

// TestCoordinator_newExecution团队详情必须配对 校验种类与详情一致性。
func TestCoordinator_newExecution团队详情必须配对(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	// 非团队种类 + 团队详情
	_, err := c.NewExecutionForTest("s1", "r1", SessionWorkKindChatUnary,
		ChildOptions{Team: &TeamExecutionDetails{}})
	if err == nil || err.Error() != "Team execution details must match the work kind" {
		t.Fatalf("应拒绝非团队种类带团队详情，实际 %v", err)
	}
	// 团队种类 + 无详情
	_, err = c.NewExecutionForTest("s1", "r2", SessionWorkKindTeamRequest, ChildOptions{})
	if err == nil || err.Error() != "Team execution details must match the work kind" {
		t.Fatalf("应拒绝团队种类无详情，实际 %v", err)
	}
}

// TestCoordinator_supersede取消同类等待执行 校验取代语义。
//
// Python: _new_execution 内 superseded_kinds 分支
func TestCoordinator_supersede取消同类等待执行(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")

	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = c.RunUnary(context.Background(), "s1", "old", SessionWorkKindHeartbeat,
			func(ctx context.Context) (any, error) {
				close(started)
				<-release
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started

	// 把旧执行置为 WAITING_FOR_CONTROL（新聊天应取代它）
	snap := c.SnapshotSession("s1")
	old := c.registry.Get(snap.Executions[0].ExecutionID)
	c.registry.MarkWaiting(old)

	// 新聊天执行应取消 WAITING 的旧执行
	_, err := c.NewExecutionForTest("s1", "new", SessionWorkKindChatStream, ChildOptions{})
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if got := c.registry.Get(old.ExecutionID); got != nil && !got.State.Terminal() {
		t.Fatalf("旧执行应已取消，实际状态 %s", got.State)
	}
	if !old.CancellationRequested {
		t.Fatal("旧执行应标记 CancellationRequested")
	}
	close(release)
}

// TestCoordinator_GoalStream取代 校验 goal_stream 取代 goal_stream/goal_attach。
func TestCoordinator_GoalStream取代(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = c.RunUnary(context.Background(), "s1", "g1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-release
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started

	snap := c.SnapshotSession("s1")
	old := c.registry.Get(snap.Executions[0].ExecutionID)
	old.WorkKind = SessionWorkKindGoalAttach
	c.registry.MarkWaiting(old)

	if _, err := c.NewExecutionForTest("s1", "g2", SessionWorkKindGoalStream, ChildOptions{}); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if got := c.registry.Get(old.ExecutionID); got != nil && !got.State.Terminal() {
		t.Fatalf("goal_attach 应被 goal_stream 取代，实际状态 %s", got.State)
	}
	close(release)
}

// TestCoordinator_非取代种类不取消 校验 heartbeat 不取代 chat。
func TestCoordinator_非取代种类不取消(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	release := make(chan struct{})
	go func() {
		_, _ = c.RunUnary(context.Background(), "s1", "c1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-release
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	snap := c.SnapshotSession("s1")
	old := c.registry.Get(snap.Executions[0].ExecutionID)
	c.registry.MarkWaiting(old)

	// 新 heartbeat 不在 supersede 列表，不应取消旧执行
	if _, err := c.NewExecutionForTest("s1", "h2", SessionWorkKindHeartbeat, ChildOptions{}); err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if got := c.registry.Get(old.ExecutionID); got == nil || got.State.Terminal() {
		t.Fatal("heartbeat 不应被另一个 heartbeat 取代")
	}
	close(release)
}

// TestCoordinator_WaitForTerminal等终端 校验 waitForTerminal 走 Terminal 通道。
func TestCoordinator_WaitForTerminal等终端(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	got, err := c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindChatUnary,
		func(context.Context) (any, error) { return "v", nil },
		UnaryOptions{WaitForTerminal: true})
	if err != nil || got != "v" {
		t.Fatalf("RunUnary = (%v, %v), want (v, nil)", got, err)
	}
}

// TestCoordinator_并发RunUnary 校验 -race 与并发安全。
func TestCoordinator_并发RunUnary(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = c.RunUnary(context.Background(), "s1", "r", SessionWorkKindChatUnary,
				func(context.Context) (any, error) { return nil, nil }, UnaryOptions{})
		}()
	}
	wg.Wait()
	time.Sleep(20 * time.Millisecond)
}

// TestCoordinator_SubmitUnary立即返回快照 校验排队提交。
func TestCoordinator_SubmitUnary立即返回快照(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	release := make(chan struct{})
	snap, err := c.SubmitUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
		func(context.Context) (any, error) {
			<-release
			return nil, nil
		}, UnaryOptions{})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if snap.RequestID != "r1" || snap.State == SessionExecutionStateSucceeded {
		t.Fatalf("应立即返回未完成的快照，实际 %+v", snap)
	}
	close(release)
}
