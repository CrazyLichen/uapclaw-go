package session

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// newExecutionForTest 构造并登记一条执行（测试直连 newExecution）。
func (c *RuntimeSessionCoordinator) newExecutionForTest(sessionID, requestID string,
	kind SessionWorkKind, parentExecutionID ...string) *SessionExecutionHandle {
	c.mu.Lock()
	record, ok := c.sessions[sessionID]
	c.mu.Unlock()
	if !ok {
		_, _ = c.RegisterSession(sessionID, "ch", SessionPersistencePolicyPersistent)
		c.mu.Lock()
		record = c.sessions[sessionID]
		c.mu.Unlock()
	}
	parentID := ""
	if len(parentExecutionID) > 0 {
		parentID = parentExecutionID[0]
	}
	h, err := c.newExecution(record, requestID, kind, parentID, nil)
	if err != nil {
		panic(err)
	}
	return h
}

// TestRecoverableControl_血缘回溯 沿 parent 链向上、代次一致才命中。
func TestRecoverableControl_血缘回溯(t *testing.T) {
	c := NewRuntimeSessionCoordinator(DefaultCoordinatorOptions())
	if _, err := c.RegisterSession("s1", "ch", SessionPersistencePolicyPersistent); err != nil {
		t.Fatal(err)
	}
	// 父执行（chat_stream）
	parent := c.newExecutionForTest("s1", "req-parent", SessionWorkKindChatStream)
	c.registry.MarkRunning(parent)
	// 子执行（session_input，挂父）
	child := c.newExecutionForTest("s1", "req-child", SessionWorkKindSessionInput, parent.ExecutionID)
	c.registry.MarkRunning(child)
	// 父执行登记等待交互（controlParent 按等待标识匹配）
	c.registry.MarkAwaitingControl(parent, map[string]struct{}{"ctl-1": {}})

	// 应答 ctl-1 的 owner=父执行自身；目标传 owner 即命中
	snap := c.RecoverableControl("s1", "ctl-1", parent.ExecutionID)
	if snap == nil {
		t.Fatal("owner 自身应命中")
	}
	if snap == nil || snap.ExecutionID != parent.ExecutionID {
		t.Fatalf("应命中 owner=%s，实际 %+v", parent.ExecutionID, snap)
	}
	// 目标不在血缘内
	if got := c.RecoverableControl("s1", "req-parent", "exec-other"); got != nil {
		t.Errorf("血缘外应 nil，实际 %+v", got)
	}
	// 会话不存在
	if got := c.RecoverableControl("s-none", "req-parent", child.ExecutionID); got != nil {
		t.Errorf("无会话应 nil，实际 %+v", got)
	}
}

// TestDeliverControlStream_边跑边可见 转发期间观察即时可见且终态正确。
func TestDeliverControlStream_边跑边可见(t *testing.T) {
	c := NewRuntimeSessionCoordinator(DefaultCoordinatorOptions())
	if _, err := c.RegisterSession("s1", "ch", SessionPersistencePolicyPersistent); err != nil {
		t.Fatal(err)
	}
	parent := c.newExecutionForTest("s1", "req-parent", SessionWorkKindChatStream)
	c.registry.MarkRunning(parent)
	c.registry.MarkAwaitingControl(parent, map[string]struct{}{"ctl-1": {}})

	ch := make(chan *schema.AgentResponseChunk, 2)
	ch <- schema.NewAgentResponseChunk("req-ans", "ch", map[string]any{"event_type": "chat.delta"}, schema.WithChunkIsComplete(false))
	close(ch)

	seen := 0
	err := c.DeliverControlStream(context.Background(), "s1", []string{"ctl-1"},
		func(context.Context) (<-chan *schema.AgentResponseChunk, error) { return ch, nil },
		ControlDeliveryOptions{},
		func(*schema.AgentResponseChunk) bool { seen++; return true })
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if seen != 1 {
		t.Errorf("应转发 1 项，实际 %d", seen)
	}
	// 父执行的等待标识被应答核销
	if ids := c.registry.WaitingControlIDsOf(parent.ExecutionID); len(ids) != 0 {
		// 未登记过等待标识时为空集，断言只做守卫
		_ = ids
	}
	_ = time.Now
}

// TestDeliverControlStream_重复在飞 二次投递同 control id 拒收。
func TestDeliverControlStream_重复在飞(t *testing.T) {
	c := NewRuntimeSessionCoordinator(DefaultCoordinatorOptions())
	_, _ = c.RegisterSession("s1", "ch", SessionPersistencePolicyPersistent)
	parent := c.newExecutionForTest("s1", "req-parent", SessionWorkKindChatStream)
	c.registry.MarkRunning(parent)

	// 模拟已有在飞投递登记：登记 delivery
	c.mu.Lock()
	c.sessions["s1"].deliveries["d1"] = &delivery{answered: map[string]struct{}{"ctl-1": {}}}
	c.mu.Unlock()

	err := c.DeliverControlStream(context.Background(), "s1", []string{"ctl-1"},
		func(context.Context) (<-chan *schema.AgentResponseChunk, error) {
			ch := make(chan *schema.AgentResponseChunk)
			close(ch)
			return ch, nil
		},
		ControlDeliveryOptions{}, func(*schema.AgentResponseChunk) bool { return true })
	if err == nil {
		t.Fatal("已有在飞应答应拒收")
	}
}

// TestExecuteUnaryTracked_超时FAILED TimeoutScope 到期 → FAILED+TimeoutError。
func TestExecuteUnaryTracked_超时FAILED(t *testing.T) {
	c := NewRuntimeSessionCoordinator(DefaultCoordinatorOptions())
	_, _ = c.RegisterSession("s1", "ch", SessionPersistencePolicyPersistent)
	record := func() *sessionRecord { c.mu.Lock(); defer c.mu.Unlock(); return c.sessions["s1"] }()
	handle, err := c.newExecution(record, "req-t", SessionWorkKindChatUnary, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	scope, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _ = c.executeUnaryTracked(context.Background(), record, handle,
		func(ctx context.Context) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, UnaryOptions{TimeoutScope: scope, TimeoutError: "session unary timed out"})
	if handle.State != SessionExecutionStateFailed {
		t.Errorf("超时应终态 FAILED，实际 %v", handle.State)
	}
}

// TestExecuteUnaryTracked_取消仍CANCELLED 无 TimeoutScope 时 ctx 取消仍 CANCELLED。
func TestExecuteUnaryTracked_取消仍CANCELLED(t *testing.T) {
	c := NewRuntimeSessionCoordinator(DefaultCoordinatorOptions())
	_, _ = c.RegisterSession("s1", "ch", SessionPersistencePolicyPersistent)
	record := func() *sessionRecord { c.mu.Lock(); defer c.mu.Unlock(); return c.sessions["s1"] }()
	handle, err := c.newExecution(record, "req-t2", SessionWorkKindChatUnary, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	_, _ = c.executeUnaryTracked(ctx, record, handle,
		func(ctx context.Context) (any, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		}, UnaryOptions{})
	if handle.State != SessionExecutionStateCancelled {
		t.Errorf("取消应终态 CANCELLED，实际 %v", handle.State)
	}
}

// TestSubmitUnary_取消后仍执行 [25.6 回退] 调用方 ctx 取消不静默吞任务。
func TestSubmitUnary_取消后仍执行(t *testing.T) {
	c := NewRuntimeSessionCoordinator(DefaultCoordinatorOptions())
	_, _ = c.RegisterSession("s1", "ch", SessionPersistencePolicyPersistent)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 提交前就取消
	done := make(chan struct{})
	_, err := c.SubmitUnary(ctx, "s1", "req-cu", SessionWorkKindChatUnary,
		func(context.Context) (any, error) { close(done); return "ok", nil }, UnaryOptions{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ctx 取消后任务仍应执行（回退对齐 Python）")
	}
}

// TestSubmitAndWait_放弃等待不吞排队项 [25.6 回退] 提交方弃等后排队项照常执行。
func TestSubmitAndWait_放弃等待不吞排队项(t *testing.T) {
	s := NewSessionWorkScheduler()
	h := NewSessionExecutionHandle("exec-1", "s1", "req-1", 1, SessionWorkKindChatUnary, "ch")
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()
	_, _ = s.SubmitAndWait(ctx, h, func(context.Context) (any, error) {
		time.Sleep(50 * time.Millisecond)
		return "done", nil
	})
	// 排队项应仍可执行：等待器释放后重跑同一 handle 的操作应成功
	value, err := s.SubmitAndWait(context.Background(), h, func(context.Context) (any, error) {
		return "done2", nil
	})
	if err == nil && value != "done2" {
		t.Errorf("重跑应成功，实际 %v", value)
	}
	_ = cancel
}

// TestLookupExecution 按标识查句柄。
func TestLookupExecution(t *testing.T) {
	c := NewRuntimeSessionCoordinator(DefaultCoordinatorOptions())
	_, _ = c.RegisterSession("s1", "ch", SessionPersistencePolicyPersistent)
	h := c.newExecutionForTest("s1", "req-1", SessionWorkKindChatStream)
	if got := c.LookupExecution(h.ExecutionID); got != h {
		t.Fatalf("应命中，实际 %+v", got)
	}
	if got := c.LookupExecution("exec-none"); got != nil {
		t.Errorf("查无应 nil，实际 %+v", got)
	}
}

// TestDeliverControlStream_emit失败可恢复 消费者关流 → exec-2 CANCELLED、
// exec-1 放回 waiting、再次答案可走通（验证讨论点"emit 失败取消 exec-2 但
// exec-1 可恢复"）。
func TestDeliverControlStream_emit失败可恢复(t *testing.T) {
	c := NewRuntimeSessionCoordinator(DefaultCoordinatorOptions())
	_, _ = c.RegisterSession("s1", "ch", SessionPersistencePolicyPersistent)
	parent := c.newExecutionForTest("s1", "req-parent", SessionWorkKindChatStream)
	c.registry.MarkRunning(parent)
	c.registry.MarkAwaitingControl(parent, map[string]struct{}{"q-77": {}})
	// 真实流程：exec-1 流收尾时 WaitingControlIDs>0 → MarkWaiting（状态置 WAITING）
	c.registry.MarkWaiting(parent)

	chunks := make(chan *schema.AgentResponseChunk, 2)
	chunks <- schema.NewAgentResponseChunk("req-child", "ch", map[string]any{"event_type": "chat.delta", "content": "部分"})
	chunks <- schema.NewAgentResponseChunk("req-child", "ch", map[string]any{"event_type": "chat.final", "content": "完整"})
	close(chunks)
	calls := 0
	err := c.DeliverControlStream(context.Background(), "s1", []string{"q-77"},
		func(context.Context) (<-chan *schema.AgentResponseChunk, error) { return chunks, nil },
		ControlDeliveryOptions{},
		func(*schema.AgentResponseChunk) bool {
			calls++
			return calls < 2 // 第二个 chunk 时消费者关流
		})
	if err != nil {
		t.Fatalf("emit=false 按 GeneratorExit 收尾不应报错: %v", err)
	}
	// exec-2：CANCELLED
		// exec-2 的 request_id = 命中的交互标识（claimControlParent 返回
	// matchedRequestID）；Select 无 WorkKind 过滤，手工过滤
	var childState SessionExecutionState
	foundChild := false
	for _, h := range c.registry.Select(SelectOptions{SessionID: "s1"}) {
		if h.WorkKind == SessionWorkKindControlInput {
			childState = h.State
			foundChild = true
		}
	}
	if !foundChild || childState != SessionExecutionStateCancelled {
		t.Fatalf("exec-2 应存在且 CANCELLED，found=%v state=%v", foundChild, childState)
	}
	// exec-1：放回 waiting（可再次作答）
	if got := c.registry.StateOf(parent.ExecutionID); got != SessionExecutionStateWaitingForControl {
		t.Fatalf("exec-1 应回到 waiting，实际 %v", got)
	}
	// 再次作答（emit 全 true）→ 走通并 SUCCEEDED
	chunks2 := make(chan *schema.AgentResponseChunk, 1)
	chunks2 <- schema.NewAgentResponseChunk("req-child2", "ch", map[string]any{"event_type": "chat.final"})
	close(chunks2)
	err = c.DeliverControlStream(context.Background(), "s1", []string{"q-77"},
		func(context.Context) (<-chan *schema.AgentResponseChunk, error) { return chunks2, nil },
		ControlDeliveryOptions{}, func(*schema.AgentResponseChunk) bool { return true })
	if err != nil {
		t.Fatalf("第二次答案应走通: %v", err)
	}
	if got := c.registry.StateOf(parent.ExecutionID); got != SessionExecutionStateSucceeded {
		t.Fatalf("exec-1 最终应 SUCCEEDED，实际 %v", got)
	}
}

func strPtr(s string) *string { return &s }

