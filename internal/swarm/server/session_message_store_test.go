// Package server 会话消息信箱 store 单元测试。
// Python 对应测试：tests/unit_tests/agentserver/test_session_messaging.py（store 分支）。
package server

import (
	"testing"

	"github.com/stretchr/testify/suite"
)

// SessionMessageStoreSuite 信箱存储测试套件。
type SessionMessageStoreSuite struct {
	suite.Suite
	store *SessionMessageStore
}

// TestSessionMessageStoreSuite 入口（tests/integration 审计硬要求同款姿势）。
func TestSessionMessageStoreSuite(t *testing.T) {
	suite.Run(t, new(SessionMessageStoreSuite))
}

// SetupTest 每用例独立 TempDir（禁真实共享目录）。
func (s *SessionMessageStoreSuite) SetupTest() {
	s.store = NewSessionMessageStore(s.T().TempDir()+"/session_messages.sqlite3", "WAL")
	s.Require().NoError(s.store.EnsureSchema())
}

// enqueueParams 便捷构造（hop 默认 1，幂等键缺省唯一）。
func (s *SessionMessageStoreSuite) enqueueParams(mods ...func(*SessionMessageEnqueueParams)) SessionMessageEnqueueParams {
	p := SessionMessageEnqueueParams{
		OwnerScopeID:        "user-1",
		SourceSessionID:     "sess-A",
		SourceTitleSnapshot: "A title",
		SourceRequestID:     "req-1",
		SourceToolCallID:    "call-1",
		IdempotencyKey:      "idem-" + newUUIDHex(),
		TargetSessionID:     "sess-B",
		Content:             "hello",
		HopCount:            1,
		MaxPendingPerTarget: 100,
		MaxMessagesPerChain: 16,
	}
	for _, m := range mods {
		m(&p)
	}
	return p
}

// TestEnqueue_新建与幂等去重 同键同参数返回既有记录 created=false。
func (s *SessionMessageStoreSuite) TestEnqueue_新建与幂等去重() {
	p := s.enqueueParams()
	rec, created, err := s.store.Enqueue(p)
	s.Require().NoError(err)
	s.True(created)
	s.Equal(StatusQueued, rec.Status)
	s.Equal("sess-A", rec.SourceSessionID)
	s.Equal(rec.MessageID, rec.ChainID) // 空 chain_id → 取 message_id
	s.EqualValues(1, rec.HopCount)

	rec2, created2, err := s.store.Enqueue(p)
	s.Require().NoError(err)
	s.False(created2)
	s.Equal(rec.MessageID, rec2.MessageID)
}

// TestEnqueue_幂等键冲突 同键不同内容 → IdempotencyConflict。
func (s *SessionMessageStoreSuite) TestEnqueue_幂等键冲突() {
	p := s.enqueueParams()
	_, _, err := s.store.Enqueue(p)
	s.Require().NoError(err)
	p.Content = "changed"
	_, _, err = s.store.Enqueue(p)
	s.Require().Error(err)
	var conflict *SessionMessageIdempotencyConflict
	s.ErrorAs(err, &conflict)
}

// TestEnqueue_目标信箱满 矩阵 #5 对应容量校验。
func (s *SessionMessageStoreSuite) TestEnqueue_目标信箱满() {
	base := s.enqueueParams()
	for i := 0; i < 3; i++ {
		_, _, err := s.store.Enqueue(base)
		s.Require().NoError(err)
		// 每条换幂等键
		base.IdempotencyKey = "idem-" + newUUIDHex()
	}
	full := base
	full.MaxPendingPerTarget = 3
	full.IdempotencyKey = "idem-" + newUUIDHex()
	_, _, err := s.store.Enqueue(full)
	s.Require().Error(err)
	var limit *SessionMessageLimitExceeded
	s.ErrorAs(err, &limit)
	s.Contains(limit.Error(), "target mailbox is full")
}

// TestEnqueue_链路满 chain 容量校验。
func (s *SessionMessageStoreSuite) TestEnqueue_链路满() {
	chain := "chain-x"
	p := s.enqueueParams(func(q *SessionMessageEnqueueParams) { q.ChainID = chain })
	for i := 0; i < 16; i++ {
		_, _, err := s.store.Enqueue(p)
		s.Require().NoError(err)
		p.IdempotencyKey = "idem-" + newUUIDHex()
		p.ParentMessageID = "sm-parent"
	}
	_, _, err := s.store.Enqueue(p)
	s.Require().Error(err)
	var limit *SessionMessageLimitExceeded
	s.ErrorAs(err, &limit)
	s.Contains(limit.Error(), "message chain limit exceeded")
}

// TestEnqueue_非法InputMode 矩阵 #5。
func (s *SessionMessageStoreSuite) TestEnqueue_非法InputMode() {
	p := s.enqueueParams(func(q *SessionMessageEnqueueParams) { q.InputMode = "bogus" })
	_, _, err := s.store.Enqueue(p)
	s.Require().Error(err)
	s.Contains(err.Error(), "unsupported cross-Session input mode")
}

// TestNextQueued_队列与屏障 矩阵 #8/#10：FIFO 队头 + running 屏障 + steer 例外。
func (s *SessionMessageStoreSuite) TestNextQueued_队列与屏障() {
	first, _, err := s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)
	second, _, err := s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)

	head, err := s.store.NextQueued("sess-B", "", false)
	s.Require().NoError(err)
	s.Require().NotNil(head)
	s.Equal(first.MessageID, head.MessageID)

	// claim 后 running → 屏障
	claimed, err := s.store.Claim(head.MessageID, "er-1", "run-1", nil)
	s.Require().NoError(err)
	s.Require().NotNil(claimed)
	s.Equal(StatusRunning, claimed.Status)
	s.Equal("er-1", claimed.ExecutionRequestID)

	none, err := s.store.NextQueued("sess-B", "", false)
	s.Require().NoError(err)
	s.Nil(none)
	_ = second
}

// TestNextQueued_SteerBarrier 矩阵 #8：steer 道 running 不阻塞 steer 队头。
func (s *SessionMessageStoreSuite) TestNextQueued_SteerBarrier() {
	rec, _, err := s.store.Enqueue(s.enqueueParams(func(q *SessionMessageEnqueueParams) {
		q.InputMode = "steer"
	}))
	s.Require().NoError(err)
	claimed, err := s.store.Claim(rec.MessageID, "er-1", "run-1", strPtr("steer"))
	s.Require().NoError(err)
	s.Require().NotNil(claimed)
	// steer+running 不作为 steer 道屏障 → 下一条 steer 可取（本例队列空则 nil 无屏障报错差异难辨，补第二条）
	rec2, _, err := s.store.Enqueue(s.enqueueParams(func(q *SessionMessageEnqueueParams) {
		q.InputMode = "steer"
	}))
	s.Require().NoError(err)
	head, err := s.store.NextQueued("sess-B", "steer", false)
	s.Require().NoError(err)
	s.Require().NotNil(head)
	s.Equal(rec2.MessageID, head.MessageID)
}

// TestDeferSteering_归一化 矩阵 #8：steer → task 道且保序。
func (s *SessionMessageStoreSuite) TestDeferSteering_归一化() {
	rec, _, err := s.store.Enqueue(s.enqueueParams(func(q *SessionMessageEnqueueParams) {
		q.InputMode = "steer"
	}))
	s.Require().NoError(err)
	claimed, err := s.store.Claim(rec.MessageID, "er-1", "run-1", strPtr("steer"))
	s.Require().NoError(err)

	deferred, err := s.store.DeferSteering(claimed.MessageID)
	s.Require().NoError(err)
	s.Require().NotNil(deferred)
	s.Equal("", deferred.InputMode)
	s.Equal(StatusQueued, deferred.Status)
	s.Equal("", deferred.ExecutionRequestID)

	// 非 steer 记录 defer → nil
	plain, _, err := s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)
	none, err := s.store.DeferSteering(plain.MessageID)
	s.Require().NoError(err)
	s.Nil(none)
}

// TestNextQueued_DeferSteering 归一化后可从 task 道取出。
func (s *SessionMessageStoreSuite) TestNextQueued_DeferSteering() {
	rec, _, err := s.store.Enqueue(s.enqueueParams(func(q *SessionMessageEnqueueParams) {
		q.InputMode = "steer"
	}))
	s.Require().NoError(err)
	_, err = s.store.DeferSteering(rec.MessageID)
	s.Require().NoError(err)
	head, err := s.store.NextQueued("sess-B", "", true)
	s.Require().NoError(err)
	s.Require().NotNil(head)
	s.Equal(rec.MessageID, head.MessageID)
}

// TestTransitionStatus_全转换 矩阵 #11/#12：终态时间戳 + 非法状态拒绝 + 预期不符 nil。
func (s *SessionMessageStoreSuite) TestTransitionStatus_全转换() {
	rec, _, err := s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)

	updated, err := s.store.TransitionStatus(rec.MessageID, StatusDelivered,
		[]string{StatusRunning}, "", "")
	s.Require().NoError(err) // 不在 expected → nil
	s.Nil(updated)

	updated, err = s.store.TransitionStatus(rec.MessageID, StatusDelivered,
		[]string{StatusQueued}, "", "")
	s.Require().NoError(err)
	s.Require().NotNil(updated)
	s.Equal(StatusDelivered, updated.Status)
	s.Require().NotNil(updated.FinishedAt)

	_, err = s.store.TransitionStatus(rec.MessageID, "bogus", []string{StatusDelivered}, "", "")
	s.Require().Error(err)

	// unknown 也写 finished_at
	rec2, _, err := s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)
	_, err = s.store.Claim(rec2.MessageID, "er-2", "run-2", nil)
	s.Require().NoError(err)
	u, err := s.store.TransitionStatus(rec2.MessageID, StatusUnknown,
		[]string{StatusRunning}, "EXECUTION_WATCHDOG_TIMEOUT", "timeout")
	s.Require().NoError(err)
	s.Require().NotNil(u)
	s.Equal(StatusUnknown, u.Status)
	s.Equal("EXECUTION_WATCHDOG_TIMEOUT", u.LastErrorCode)
}

// TestWaitingAndSupersede 矩阵 #13/#14。
func (s *SessionMessageStoreSuite) TestWaitingAndSupersede() {
	rec, _, err := s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)
	_, err = s.store.Claim(rec.MessageID, "er-1", "run-1", nil)
	s.Require().NoError(err)

	marked, err := s.store.MarkWaiting(rec.MessageID, "interrupt-1", "ask_user")
	s.Require().NoError(err)
	s.Require().NotNil(marked)
	s.Equal(StatusWaitingUser, marked.Status)
	s.Equal("interrupt-1", marked.InterruptRequestID)

	got, err := s.store.WaitingForTarget("sess-B")
	s.Require().NoError(err)
	s.Equal(rec.MessageID, got.MessageID)

	got, err = s.store.WaitingForPause("sess-B", []string{"other", "interrupt-1"})
	s.Require().NoError(err)
	s.Equal(rec.MessageID, got.MessageID)

	got, err = s.store.WaitingForPause("sess-B", []string{"nope"})
	s.Require().NoError(err)
	s.Nil(got)

	// 替换关联
	replaced, err := s.store.ReplaceWaitingCorrelation(rec.MessageID, "interrupt-2", "ask_user")
	s.Require().NoError(err)
	s.Require().NotNil(replaced)
	s.Equal("interrupt-2", replaced.InterruptRequestID)

	// supersede → failed
	failed, err := s.store.SupersedeWaitingForTarget("sess-B")
	s.Require().NoError(err)
	s.Len(failed, 1)
	s.Equal(StatusFailed, failed[0].Status)
	s.Equal("SUPERSEDED_BY_USER_TURN", failed[0].LastErrorCode)
}

// TestResolveUnknown_三档 矩阵 #11。
func (s *SessionMessageStoreSuite) TestResolveUnknown_三档() {
	mk := func() *SessionMessageRecord {
		rec, _, err := s.store.Enqueue(s.enqueueParams())
		s.Require().NoError(err)
		_, err = s.store.Claim(rec.MessageID, "er", "run", nil)
		s.Require().NoError(err)
		u, err := s.store.TransitionStatus(rec.MessageID, StatusUnknown, []string{StatusRunning}, "", "")
		s.Require().NoError(err)
		return u
	}
	// succeeded_by_user
	rec := mk()
	res, err := s.store.ResolveUnknown(rec.MessageID, "user-1", "sess-A", StatusSucceeded)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Equal(StatusSucceeded, res.Status)
	s.Equal("succeeded_by_user", res.Resolution)

	// cancelled_by_user
	rec = mk()
	res, err = s.store.ResolveUnknown(rec.MessageID, "user-1", "sess-B", StatusCancelled)
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Equal(StatusCancelled, res.Status)
	s.Equal("UNKNOWN_CANCELLED", res.LastErrorCode)

	// continue_queued 只放闸门不改状态
	rec = mk()
	res, err = s.store.ResolveUnknown(rec.MessageID, "user-1", "sess-B", "continue_queued")
	s.Require().NoError(err)
	s.Require().NotNil(res)
	s.Equal(StatusUnknown, res.Status)
	s.Require().NotNil(res.QueueReleasedAt)

	// 非 owner/participant → nil
	rec = mk()
	res, err = s.store.ResolveUnknown(rec.MessageID, "user-1", "sess-Z", "continue_queued")
	s.Require().NoError(err)
	s.Nil(res)
}

// TestRecoverInflightAsUnknown 宿主重启隔离。
func (s *SessionMessageStoreSuite) TestRecoverInflightAsUnknown() {
	rec, _, err := s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)
	_, err = s.store.Claim(rec.MessageID, "er", "run", nil)
	s.Require().NoError(err)
	n, err := s.store.RecoverInflightAsUnknown()
	s.Require().NoError(err)
	s.Equal(1, n)
	got, err := s.store.Get(rec.MessageID)
	s.Require().NoError(err)
	s.Equal(StatusUnknown, got.Status)
	s.Equal("HOST_RESTARTED", got.LastErrorCode)
}

// TestCancelPendingForTarget 矩阵 #14：删除中目标消息终结且内容清空。
func (s *SessionMessageStoreSuite) TestCancelPendingForTarget() {
	rec, _, err := s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)
	_, _, err = s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)
	failed, err := s.store.CancelPendingForTarget("sess-B")
	s.Require().NoError(err)
	s.Len(failed, 2)
	for _, f := range failed {
		s.Equal(StatusCancelled, f.Status)
		s.Equal("TARGET_DELETED", f.LastErrorCode)
		s.Equal("", f.Content)
	}
	_ = rec
}

// TestQueuedTargets_补扫 矩阵 #1。
func (s *SessionMessageStoreSuite) TestQueuedTargets_补扫() {
	_, _, err := s.store.Enqueue(s.enqueueParams(func(q *SessionMessageEnqueueParams) {
		q.TargetSessionID = "sess-B"
	}))
	s.Require().NoError(err)
	_, _, err = s.store.Enqueue(s.enqueueParams(func(q *SessionMessageEnqueueParams) {
		q.TargetSessionID = "sess-C"
	}))
	s.Require().NoError(err)
	targets, err := s.store.QueuedTargets()
	s.Require().NoError(err)
	s.Equal([]string{"sess-B", "sess-C"}, targets)
}

// TestPendingCountsAndBlockingStates 投影查询。
func (s *SessionMessageStoreSuite) TestPendingCountsAndBlockingStates() {
	rec, _, err := s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)
	counts, err := s.store.PendingCounts([]string{"sess-B", "sess-Z"})
	s.Require().NoError(err)
	s.Equal(1, counts["sess-B"])
	_, ok := counts["sess-Z"]
	s.False(ok)

	states, err := s.store.BlockingStates([]string{"sess-B"})
	s.Require().NoError(err)
	s.Empty(states)

	_, err = s.store.Claim(rec.MessageID, "er", "run", nil)
	s.Require().NoError(err)
	_, err = s.store.MarkWaiting(rec.MessageID, "ir", "src")
	s.Require().NoError(err)
	states, err = s.store.BlockingStates([]string{"sess-B"})
	s.Require().NoError(err)
	s.Equal(StatusWaitingUser, states["sess-B"])
}

// TestListMessagesAndPendingTargets 列表投影。
func (s *SessionMessageStoreSuite) TestListMessagesAndPendingTargets() {
	a, _, err := s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)
	b, _, err := s.store.Enqueue(s.enqueueParams(func(q *SessionMessageEnqueueParams) {
		q.IdempotencyKey = "idem-" + newUUIDHex()
	}))
	s.Require().NoError(err)
	msgs, err := s.store.ListMessages("user-1", "sess-B", 50, 0)
	s.Require().NoError(err)
	s.Len(msgs, 2)
	s.Equal(b.MessageID, msgs[0].MessageID) // 倒序

	targets, err := s.store.PendingTargetsForSession("user-1", "sess-A", 21)
	s.Require().NoError(err)
	s.Equal([]string{"sess-B"}, targets)
	_ = a
}

// TestContinueQueuedForTarget 矩阵 #1：有 queued 工作时释放 unknown 屏障。
func (s *SessionMessageStoreSuite) TestContinueQueuedForTarget() {
	// unknown 屏蔽 + 同道 queued
	recU, _, err := s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)
	_, err = s.store.Claim(recU.MessageID, "er", "run", nil)
	s.Require().NoError(err)
	_, err = s.store.TransitionStatus(recU.MessageID, StatusUnknown, []string{StatusRunning}, "", "")
	s.Require().NoError(err)
	_, _, err = s.store.Enqueue(s.enqueueParams())
	s.Require().NoError(err)

	n, err := s.store.ContinueQueuedForTarget("user-1", "sess-B")
	s.Require().NoError(err)
	s.Equal(1, n)

	// 无 queued 时不动（用独立目标 sess-C 隔离）
	recU2, _, err := s.store.Enqueue(s.enqueueParams(func(q *SessionMessageEnqueueParams) {
		q.TargetSessionID = "sess-C"
	}))
	s.Require().NoError(err)
	_, err = s.store.Claim(recU2.MessageID, "er2", "run2", nil)
	s.Require().NoError(err)
	_, err = s.store.TransitionStatus(recU2.MessageID, StatusUnknown, []string{StatusRunning}, "", "")
	s.Require().NoError(err)
	n, err = s.store.ContinueQueuedForTarget("user-1", "sess-C")
	s.Require().NoError(err)
	s.Equal(0, n)
}

// strPtr 便捷指针。
