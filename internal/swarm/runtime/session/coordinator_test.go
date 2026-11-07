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

// TestCoordinator_RunStream按序产出 校验流式基本语义。
func TestCoordinator_RunStream按序产出(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	var got []any
	err := c.RunStream(context.Background(), "s1", "r1", SessionWorkKindChatStream,
		func(context.Context) (<-chan any, error) {
			ch := make(chan any, 3)
			ch <- "a"
			ch <- "b"
			ch <- "c"
			close(ch)
			return ch, nil
		}, StreamOptions{}, func(v any) bool { got = append(got, v); return true })
	if err != nil {
		t.Fatalf("RunStream 失败: %v", err)
	}
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Fatalf("产出顺序错误: %v", got)
	}
}

// TestCoordinator_RunStream终态不丢 校验终态标记在有界缓冲满时仍送达。
// 对应上游 finally 的 "A terminal marker must not be dropped when the
// bounded buffer is full" 注释。
func TestCoordinator_RunStream终态不丢(t *testing.T) {
	opts := DefaultCoordinatorOptions()
	opts.StreamBufferSize = 1 // 故意压到 1，制造缓冲压力
	c := NewRuntimeSessionCoordinator(opts)
	mustRegister(t, c, "s1")
	count := 0
	err := c.RunStream(context.Background(), "s1", "r1", SessionWorkKindChatStream,
		func(context.Context) (<-chan any, error) {
			ch := make(chan any, 10)
			for i := 0; i < 5; i++ {
				ch <- i
			}
			close(ch)
			return ch, nil
		}, StreamOptions{}, func(any) bool { count++; return true })
	if err != nil {
		t.Fatalf("RunStream 失败: %v", err)
	}
	if count != 5 {
		t.Fatalf("应收到 5 个元素，实际 %d（终态标记可能被丢弃导致提前退出）", count)
	}
}

// TestCoordinator_RunStream关闭Done且置终态 校验流式路径的收尾。
func TestCoordinator_RunStream关闭Done且置终态(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	_ = c.RunStream(context.Background(), "s1", "r1", SessionWorkKindChatStream,
		func(context.Context) (<-chan any, error) {
			ch := make(chan any)
			close(ch)
			return ch, nil
		}, StreamOptions{}, func(any) bool { return true })
	snap := c.SnapshotSession("s1")
	if len(snap.Executions) != 1 {
		t.Fatalf("应有 1 条执行，实际 %d", len(snap.Executions))
	}
	if snap.Executions[0].State != SessionExecutionStateSucceeded {
		t.Fatalf("执行应为 SUCCEEDED，实际 %s", snap.Executions[0].State)
	}
}

// TestCoordinator_RunStream拒绝团队种类 校验 work_kind 闸门。
func TestCoordinator_RunStream拒绝团队种类(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	err := c.RunStream(context.Background(), "s1", "r1", SessionWorkKindTeamRequest,
		func(context.Context) (<-chan any, error) { return nil, nil },
		StreamOptions{}, func(any) bool { return true })
	if err == nil || err.Error() != "Team work must use run_team_stream" {
		t.Fatalf("应拒绝团队种类并给出上游文案，实际 %v", err)
	}
}

// TestCoordinator_RunStream中途报错上抛 校验错误传播。
func TestCoordinator_RunStream中途报错上抛(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	boom := errors.New("stream boom")
	err := c.RunStream(context.Background(), "s1", "r1", SessionWorkKindChatStream,
		func(context.Context) (<-chan any, error) { return nil, boom },
		StreamOptions{}, func(any) bool { return true })
	if !errors.Is(err, boom) {
		t.Fatalf("应上抛原错误，实际 %v", err)
	}
	snap := c.SnapshotSession("s1")
	if snap.Executions[0].State != SessionExecutionStateFailed {
		t.Fatalf("执行应为 FAILED，实际 %s", snap.Executions[0].State)
	}
}

// TestCoordinator_RunStream消费方主动停止 校验 emit 返回 false 时收尾干净。
func TestCoordinator_RunStream消费方主动停止(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	seen := 0
	err := c.RunStream(context.Background(), "s1", "r1", SessionWorkKindChatStream,
		func(ctx context.Context) (<-chan any, error) {
			ch := make(chan any, 10)
			for i := 0; i < 10; i++ {
				ch <- i
			}
			close(ch)
			return ch, nil
		}, StreamOptions{}, func(any) bool {
			seen++
			return seen < 2 // 第二个元素后主动停
		})
	if err != nil {
		t.Fatalf("主动停止不应报错: %v", err)
	}
	if seen != 2 {
		t.Fatalf("应在第 2 个元素后停止，实际收到 %d 个", seen)
	}
}

// TestCoordinator_RunTeamStream拒绝非团队种类 校验反向闸门。
func TestCoordinator_RunTeamStream拒绝非团队种类(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	err := c.RunTeamStream(context.Background(), "s1", "r1", SessionWorkKindChatStream,
		func(context.Context, TeamExecutionContext) (<-chan any, error) { return nil, nil },
		StreamOptions{Details: &TeamExecutionDetails{}}, func(any) bool { return true })
	if err == nil || err.Error() != "non-Team work must use run_stream" {
		t.Fatalf("应拒绝非团队种类，实际 %v", err)
	}
}

// TestCoordinator_RunTeamStream传入团队上下文 校验 TeamExecutionContext 字段。
func TestCoordinator_RunTeamStream传入团队上下文(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	var captured TeamExecutionContext
	err := c.RunTeamStream(context.Background(), "s1", "r-team", SessionWorkKindTeamRequest,
		func(_ context.Context, teamCtx TeamExecutionContext) (<-chan any, error) {
			captured = teamCtx
			ch := make(chan any)
			close(ch)
			return ch, nil
		}, StreamOptions{Details: &TeamExecutionDetails{ControlID: "c1"}},
		func(any) bool { return true })
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if captured.ExecutionID == "" || captured.RequestID != "r-team" {
		t.Fatalf("团队上下文身份错误: %+v", captured)
	}
	if captured.Session.SessionID != "s1" || captured.Session.Generation != 1 {
		t.Fatalf("团队上下文会话错误: %+v", captured.Session)
	}
	if captured.Details.ControlID != "c1" {
		t.Fatalf("团队详情未透传: %+v", captured.Details)
	}
}

// TestCoordinator_RunStream交互标识登记 校验 InteractionIDs 回调。
func TestCoordinator_RunStream交互标识登记(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	err := c.RunStream(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
		func(ctx context.Context) (<-chan any, error) {
			ch := make(chan any, 1)
			ch <- "ask"
			close(ch)
			return ch, nil
		}, StreamOptions{
			InteractionIDs: func(v any) map[string]struct{} {
				if v == "ask" {
					return map[string]struct{}{"c1": {}}
				}
				return nil
			},
		}, func(any) bool { return true })
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	snap := c.SnapshotSession("s1")
	if len(snap.Executions) != 1 {
		t.Fatalf("应有 1 条执行，实际 %d", len(snap.Executions))
	}
	// 有等待交互时应为 WAITING_FOR_CONTROL
	if snap.Executions[0].State != SessionExecutionStateWaitingForControl {
		t.Fatalf("有交互应置 WAITING_FOR_CONTROL，实际 %s", snap.Executions[0].State)
	}
}

// TestCoordinator_RunStream空流正常结束 校验零元素路径。
func TestCoordinator_RunStream空流正常结束(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	count := 0
	err := c.RunStream(context.Background(), "s1", "r1", SessionWorkKindChatStream,
		func(context.Context) (<-chan any, error) {
			ch := make(chan any)
			close(ch)
			return ch, nil
		}, StreamOptions{}, func(any) bool { count++; return true })
	if err != nil || count != 0 {
		t.Fatalf("空流应正常结束且无元素，实际 (err=%v, count=%d)", err, count)
	}
}

// TestCancelExecution_精确取消单个 校验按 execution_id 命中。
func TestCancelExecution_精确取消单个(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(ctx context.Context) (any, error) {
				close(started)
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-release:
					return nil, nil
				}
			}, UnaryOptions{})
	}()
	<-started
	snap := c.SnapshotSession("s1")
	eid := snap.Executions[0].ExecutionID
	res, err := c.CancelExecution("s1", CancelExecutionOptions{ExecutionID: &eid})
	if err != nil {
		t.Fatalf("CancelExecution 失败: %v", err)
	}
	if res.Matched != 1 || res.Cancelled != 1 {
		t.Fatalf("应命中并取消 1 条，实际 %+v", res)
	}
	close(release)
	<-done
}

// TestCancelExecution_已请求取消的不重复取消 校验两批语义。
// 对应上游 _request_direct_cancel 的 fresh/pending 分流。
func TestCancelExecution_已请求取消的不重复取消(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(ctx context.Context) (any, error) {
				close(started)
				<-release // 故意不响应 ctx
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	snap := c.SnapshotSession("s1")
	eid := snap.Executions[0].ExecutionID
	shortTimeout := 50 * time.Millisecond
	first, _ := c.CancelExecution("s1", CancelExecutionOptions{
		ExecutionID: &eid, WaitTimeout: &shortTimeout,
	})
	if len(first.TimedOut) != 1 {
		t.Fatalf("首次应超时 1 条，实际 %+v", first)
	}
	// 第二次：已 CancellationRequested，走 pending 分支只等
	second, err := c.CancelExecution("s1", CancelExecutionOptions{
		ExecutionID: &eid, WaitTimeout: &shortTimeout,
	})
	if err != nil {
		t.Fatalf("重复 CancelExecution 应不报错: %v", err)
	}
	if len(second.TimedOut) != 1 {
		t.Fatalf("pending 分支也应超时，实际 %+v", second)
	}
	close(release)
	<-done
}

// TestCancelExecution_超时不标CANCELLED 校验 D25 语义。
func TestCancelExecution_超时不标CANCELLED(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-release
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	snap := c.SnapshotSession("s1")
	eid := snap.Executions[0].ExecutionID
	shortTimeout := 50 * time.Millisecond
	res, _ := c.CancelExecution("s1", CancelExecutionOptions{
		ExecutionID: &eid, WaitTimeout: &shortTimeout,
	})
	if len(res.TimedOut) != 1 {
		t.Fatalf("应超时 1 条，实际 %+v", res)
	}
	// 超时的执行状态保持原样（不标 CANCELLED）
	got := c.GetExecution(eid)
	if got == nil {
		t.Fatal("超时后执行不应被回收")
	}
	if got.State == SessionExecutionStateCancelled {
		t.Fatal("超时的执行不得标 CANCELLED（否则下次重试查不到残留）")
	}
	close(release)
	<-done
}

// TestCancelExecution_无匹配返回零值 校验空结果。
func TestCancelExecution_无匹配返回零值(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	notExist := "nope"
	res, err := c.CancelExecution("s1", CancelExecutionOptions{ExecutionID: &notExist})
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if res.Matched != 0 || res.Cancelled != 0 || len(res.TimedOut) != 0 {
		t.Fatalf("无匹配应返回零值，实际 %+v", res)
	}
}

// TestCancelExecution_展开后代执行 校验 withDescendants。
func TestCancelExecution_展开后代执行(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "parent", SessionWorkKindHeartbeat,
			func(ctx context.Context) (any, error) {
				close(started)
				<-release
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	// 先建一条挂在 parent 下的子执行，取消父时应当连带展开到它。
	if _, err := c.NewExecutionForTest("s1", "child", SessionWorkKindHeartbeat,
		ChildOptions{ParentRequestID: "parent"}); err != nil {
		t.Fatalf("创建子执行失败: %v", err)
	}
	snap := c.SnapshotSession("s1")
	parentID := ""
	for _, e := range snap.Executions {
		if e.RequestID == "parent" {
			parentID = e.ExecutionID
		}
	}
	shortTimeout := 50 * time.Millisecond
	res, _ := c.CancelExecution("s1", CancelExecutionOptions{
		ExecutionID: &parentID, WaitTimeout: &shortTimeout,
	})
	if res.Matched < 2 {
		t.Fatalf("应展开父+子共 2 条，实际 %+v", res)
	}
	// 子执行是否仍在登记表里不做断言：取消流程允许它已被回收。
	close(release)
	<-done
}

// TestCancelExecution_排除指定执行 校验 ExcludedExecutionIDs。
func TestCancelExecution_排除指定执行(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-release
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	snap := c.SnapshotSession("s1")
	eid := snap.Executions[0].ExecutionID
	res, _ := c.CancelExecution("s1", CancelExecutionOptions{
		ExecutionID:          &eid,
		ExcludedExecutionIDs: []string{eid},
	})
	if res.Matched != 0 {
		t.Fatalf("被排除的执行不应命中，实际 %+v", res)
	}
	close(release)
	<-done
}

// TestCloseSession_无执行时直接关闭 校验基本路径。
func TestCloseSession_无执行时直接关闭(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	res, err := c.CloseSession("s1", nil, nil)
	if err != nil {
		t.Fatalf("CloseSession 失败: %v", err)
	}
	if !res.Existed || len(res.TimedOut) != 0 {
		t.Fatalf("应存在且无超时，实际 %+v", res)
	}
	if res.Generation == nil || *res.Generation != 1 {
		t.Fatalf("应返回代次 1，实际 %+v", res.Generation)
	}
	snap := c.SnapshotSession("s1")
	if snap.State != RuntimeSessionStateClosed {
		t.Fatalf("关闭后状态应为 CLOSED，实际 %s", snap.State)
	}
}

// TestCloseSession_有执行时先取消再关 校验清场语义。
func TestCloseSession_有执行时先取消再关(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(ctx context.Context) (any, error) {
				close(started)
				<-ctx.Done()
				return nil, ctx.Err()
			}, UnaryOptions{})
	}()
	<-started
	timeout := 2 * time.Second
	res, err := c.CloseSession("s1", nil, &timeout)
	if err != nil {
		t.Fatalf("CloseSession 失败: %v", err)
	}
	if !res.Existed {
		t.Fatal("会话应存在")
	}
	if len(res.TimedOut) != 0 {
		t.Fatalf("响应取消的执行不应超时，实际 %v", res.TimedOut)
	}
	<-finished
	snap := c.SnapshotSession("s1")
	if snap.State != RuntimeSessionStateClosed {
		t.Fatalf("应置 CLOSED，实际 %s", snap.State)
	}
}

// TestCloseSession_代次不符返回不存在 校验代次判定。
func TestCloseSession_代次不符返回不存在(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1") // generation=1
	other := 9
	res, _ := c.CloseSession("s1", &other, nil)
	if res.Existed {
		t.Fatal("代次不符应返回 Existed=false")
	}
}

// TestCloseSession_超时后不置CLOSED 校验超时保状态供重试。
// spec §2.2 最后一行 + D25：超时只是"不再等"，不是"对方不停"。
func TestCloseSession_超时后不置CLOSED(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	block := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-block // 故意不响应 ctx，模拟不检查取消的阻塞点
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	shortTimeout := 50 * time.Millisecond
	res, err := c.CloseSession("s1", nil, &shortTimeout)
	if err != nil {
		t.Fatalf("CloseSession 失败: %v", err)
	}
	if len(res.TimedOut) == 0 {
		t.Fatal("应记录超时执行")
	}
	snap := c.SnapshotSession("s1")
	if snap.State == RuntimeSessionStateClosed {
		t.Fatal("超时后不应置 CLOSED（否则下次重试查不到残留）")
	}
	if snap.State != RuntimeSessionStateQuiescing {
		t.Fatalf("超时后应保持 QUIESCING，实际 %s", snap.State)
	}
	close(block)
	<-done
}

// TestCloseSession_双删幂等 校验并发重复关闭。
func TestCloseSession_双删幂等(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	var wg sync.WaitGroup
	results := make([]CloseSessionResult, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			res, _ := c.CloseSession("s1", nil, nil)
			results[n] = res
		}(i)
	}
	wg.Wait()
	if !results[0].Existed && !results[1].Existed {
		t.Fatal("至少一次应返回 Existed=true")
	}
}

// TestCoordinator_Close关闭全部会话 校验 Runtime 关闭。
func TestCoordinator_Close关闭全部会话(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	mustRegister(t, c, "s2")
	timeout := time.Second
	var timedOut []string
	if err := c.Close(&timeout, &timedOut); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if len(timedOut) != 0 {
		t.Fatalf("不应有超时，实际 %v", timedOut)
	}
	if _, err := c.RegisterSession("s3", "web", SessionPersistencePolicyPersistent); err == nil {
		t.Fatal("关闭后注册应报错")
	}
}

// TestCoordinator_Close有超时返回错误 校验 SessionCloseTimeoutError。
func TestCoordinator_Close有超时返回错误(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	block := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-block
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	shortTimeout := 50 * time.Millisecond
	var timedOut []string
	err := c.Close(&shortTimeout, &timedOut)
	if err == nil {
		t.Fatal("有超时应返回错误")
	}
	var closeErr *SessionCloseTimeoutError
	if !errors.As(err, &closeErr) {
		t.Fatalf("应返回 SessionCloseTimeoutError，实际 %T", err)
	}
	if closeErr.SessionID != "runtime" {
		t.Fatalf("SessionID 应为 runtime，实际 %q", closeErr.SessionID)
	}
	if len(timedOut) != 1 {
		t.Fatalf("timedOut 出参应有 1 条，实际 %v", timedOut)
	}
	close(block)
	<-done
}

// TestCoordinator_RecordInteraction 校验执行内交互登记。
func TestCoordinator_RecordInteraction(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-release
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	if !c.RecordInteraction("s1", "r1", map[string]struct{}{"c1": {}}) {
		t.Fatal("RecordInteraction 应成功")
	}
	if got := c.WaitingInteractions("s1", "r1"); len(got) != 1 {
		t.Fatalf("应记录 1 个交互，实际 %v", got)
	}
	close(release)
	<-done
}

// TestCoordinator_RecordInteraction会话非活跃返回false 校验前置条件。
func TestCoordinator_RecordInteraction会话非活跃返回false(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1") // 状态 READY
	if c.RecordInteraction("s1", "r1", map[string]struct{}{"c1": {}}) {
		t.Fatal("会话非 ACTIVE 时应返回 false")
	}
	if c.RecordInteraction("nope", "r1", map[string]struct{}{"c1": {}}) {
		t.Fatal("未注册会话应返回 false")
	}
}

// TestCoordinator_UnansweredInteractions 校验未应答集合。
func TestCoordinator_UnansweredInteractions(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-release
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	c.RecordInteraction("s1", "r1", map[string]struct{}{"c1": {}, "c2": {}})
	got := c.UnansweredInteractions("s1")
	if len(got) != 2 {
		t.Fatalf("应有 2 个未应答交互，实际 %v", got)
	}
	close(release)
	<-done
}

// TestCoordinator_HasControlTarget 校验探针。
func TestCoordinator_HasControlTarget(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	if c.HasControlTarget("s1", "nope") {
		t.Fatal("无活跃执行时应返回 false")
	}
	if c.HasControlTarget("unregistered", "nope") {
		t.Fatal("未注册会话应返回 false")
	}
}

// TestCoordinator_BindAgent 校验 agent 绑定。
func TestCoordinator_BindAgent(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	release := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-release
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	c.BindAgent("s1", "r1", "agent-x")
	if got := c.ControlTargetAgent("s1", []string{"c1"}); got != nil {
		t.Fatalf("无等待交互时不应返回 agent，实际 %v", got)
	}
	close(release)
	<-done
}

// TestCoordinator_WaitingInteractions未注册返回空 校验空值语义。
func TestCoordinator_WaitingInteractions未注册返回空(t *testing.T) {
	c := newTestCoordinator()
	if got := c.WaitingInteractions("nope", "r1"); len(got) != 0 {
		t.Fatalf("未注册应返回空集合，实际 %v", got)
	}
	if got := c.UnansweredInteractions("nope"); len(got) != 0 {
		t.Fatalf("未注册应返回空集合，实际 %v", got)
	}
	if got := c.ControlTargetAgent("nope", []string{"c1"}); got != nil {
		t.Fatalf("未注册应返回 nil，实际 %v", got)
	}
	if _, ok := c.ControlTargetWorkKind("nope", "r1"); ok {
		t.Fatal("未注册应返回 false")
	}
}

// TestDeliverControl_无目标时报错 校验 NoControlTargetError。
func TestDeliverControl_无目标时报错(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	_, err := c.DeliverControl(context.Background(), "s1", []string{"c1"},
		func(context.Context) (any, error) { return nil, nil }, ControlDeliveryOptions{})
	var target *NoControlTargetError
	if !errors.As(err, &target) {
		t.Fatalf("应返回 NoControlTargetError，实际 %v", err)
	}
	want := "session has no active execution: s1"
	if err.Error() != want {
		t.Fatalf("错误文案 = %q, want %q", err.Error(), want)
	}
}

// TestDeliverControl_投递后恢复父执行 校验 answered 语义。
func TestDeliverControl_投递后恢复父执行(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	hold := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-hold
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	c.RecordInteraction("s1", "r1", map[string]struct{}{"c1": {}})

	// 手动把父执行置 WAITING_FOR_CONTROL 以模拟等待应答
	generation := 1
	for _, h := range c.registry.Select(SelectOptions{
		SessionID: "s1", Generation: &generation, ActiveOnly: true,
	}) {
		c.registry.MarkWaiting(h)
	}

	got, err := c.DeliverControl(context.Background(), "s1", []string{"c1"},
		func(context.Context) (any, error) { return "answered", nil }, ControlDeliveryOptions{})
	if err != nil {
		t.Fatalf("DeliverControl 失败: %v", err)
	}
	if got != "answered" {
		t.Fatalf("返回值错误: %v", got)
	}
	close(hold)
	<-done
}

// TestDeliverControl_重复投递报AnswerInFlight 校验在飞保护。
func TestDeliverControl_重复投递报AnswerInFlight(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	hold := make(chan struct{})
	deliverRelease := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-hold
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	c.RecordInteraction("s1", "r1", map[string]struct{}{"c1": {}})
	generation := 1
	for _, h := range c.registry.Select(SelectOptions{
		SessionID: "s1", Generation: &generation, ActiveOnly: true,
	}) {
		c.registry.MarkWaiting(h)
	}

	// 第一个投递挂住不返回
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = c.DeliverControl(context.Background(), "s1", []string{"c1"},
			func(context.Context) (any, error) {
				<-deliverRelease
				return "first", nil
			}, ControlDeliveryOptions{})
	}()
	time.Sleep(50 * time.Millisecond)

	// 第二个投递应报 AnswerInFlightError
	_, err := c.DeliverControl(context.Background(), "s1", []string{"c1"},
		func(context.Context) (any, error) { return "second", nil }, ControlDeliveryOptions{})
	var inFlight *AnswerInFlightError
	if !errors.As(err, &inFlight) {
		t.Fatalf("第二次投递应报 AnswerInFlightError，实际 %v", err)
	}
	close(deliverRelease)
	<-firstDone
	close(hold)
	<-done
}

// TestDeliverControl_父执行已结束报错 校验 SessionExecutionEndedError。
func TestDeliverControl_父执行已结束报错(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	hold := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-hold
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	c.RecordInteraction("s1", "r1", map[string]struct{}{"c1": {}})
	generation := 1
	var parent *SessionExecutionHandle
	for _, h := range c.registry.Select(SelectOptions{
		SessionID: "s1", Generation: &generation, ActiveOnly: true,
	}) {
		c.registry.MarkWaiting(h)
		parent = h
	}
	// 投递过程中把父执行置终态
	_, err := c.DeliverControl(context.Background(), "s1", []string{"c1"},
		func(context.Context) (any, error) {
			c.registry.MarkTerminal(parent, SessionExecutionStateCancelled, "")
			return "late", nil
		}, ControlDeliveryOptions{})
	var ended *SessionExecutionEndedError
	if !errors.As(err, &ended) {
		t.Fatalf("父执行已结束应报 SessionExecutionEndedError，实际 %v", err)
	}
	close(hold)
	<-done
}

// TestCoordinator_OnExecution回调 校验投递期执行快照回调。
func TestCoordinator_OnExecution回调(t *testing.T) {
	c := newTestCoordinator()
	mustRegister(t, c, "s1")
	started := make(chan struct{})
	hold := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.RunUnary(context.Background(), "s1", "r1", SessionWorkKindHeartbeat,
			func(context.Context) (any, error) {
				close(started)
				<-hold
				return nil, nil
			}, UnaryOptions{})
	}()
	<-started
	c.RecordInteraction("s1", "r1", map[string]struct{}{"c1": {}})
	generation := 1
	for _, h := range c.registry.Select(SelectOptions{
		SessionID: "s1", Generation: &generation, ActiveOnly: true,
	}) {
		c.registry.MarkWaiting(h)
	}
	var captured *SessionExecutionSnapshot
	_, err := c.DeliverControl(context.Background(), "s1", []string{"c1"},
		func(context.Context) (any, error) { return nil, nil },
		ControlDeliveryOptions{OnExecution: func(s SessionExecutionSnapshot) { captured = &s }})
	if err != nil {
		t.Fatalf("失败: %v", err)
	}
	if captured == nil || captured.WorkKind != SessionWorkKindControlInput {
		t.Fatalf("OnExecution 应收到 control_input 快照，实际 %+v", captured)
	}
	close(hold)
	<-done
}
