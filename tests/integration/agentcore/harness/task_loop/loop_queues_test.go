//go:build integration

package task_loop

import (
	"testing"

	"github.com/stretchr/testify/suite"

	task_loop "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoopQueuesSuite 双队列缓冲集成测试。
// 对照 Python: tests/unit_tests/harness/test_loop_queues.py
type LoopQueuesSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewLoopQueues 验证创建 LoopQueues
func (s *LoopQueuesSuite) TestNewLoopQueues() {
	q := task_loop.NewLoopQueues(16)
	s.NotNil(q)

	// cap <= 0 使用默认值
	q2 := task_loop.NewLoopQueues(0)
	s.NotNil(q2)

	q3 := task_loop.NewLoopQueues(-1)
	s.NotNil(q3)
}

// TestLoopQueues_EnqueueFollowUp 验证入队后续消息并排空
func (s *LoopQueuesSuite) TestLoopQueues_EnqueueFollowUp() {
	q := task_loop.NewLoopQueues(16)
	q.PushFollowUp("msg1")
	q.PushFollowUp("msg2")

	s.True(q.HasFollowUp())

	msgs := q.DrainFollowUp()
	s.Equal(2, len(msgs))
	s.Contains(msgs, "msg1")
	s.Contains(msgs, "msg2")

	// 排空后无待处理
	s.False(q.HasFollowUp())
	s.Empty(q.DrainFollowUp())
}

// TestLoopQueues_EnqueueSteer 验证入队引导指令并排空
func (s *LoopQueuesSuite) TestLoopQueues_EnqueueSteer() {
	q := task_loop.NewLoopQueues(16)
	q.PushSteer("steer1")
	q.PushSteer("steer2")

	msgs := q.DrainSteering()
	s.Equal(2, len(msgs))
	s.Contains(msgs, "steer1")
	s.Contains(msgs, "steer2")

	// 排空后无剩余
	s.Empty(q.DrainSteering())
}

// TestLoopQueues_EnqueueAbort 验证中止事件通过 steering 队列传递
// LoopQueues 使用 steering/followUp 双通道模型，abort 消息走 steering 队列
func (s *LoopQueuesSuite) TestLoopQueues_EnqueueAbort() {
	q := task_loop.NewLoopQueues(16)
	q.PushSteer("abort")

	msgs := q.DrainSteering()
	s.Equal(1, len(msgs))
	s.Equal("abort", msgs[0])
}

// TestLoopQueues_DrainAll 验证排空所有事件
func (s *LoopQueuesSuite) TestLoopQueues_DrainAll() {
	q := task_loop.NewLoopQueues(16)
	q.PushSteer("s1")
	q.PushFollowUp("f1")
	q.PushSteer("s2")
	q.PushFollowUp("f2")

	steerMsgs := q.DrainSteering()
	followMsgs := q.DrainFollowUp()

	s.Equal(2, len(steerMsgs))
	s.Equal(2, len(followMsgs))
	s.Contains(steerMsgs, "s1")
	s.Contains(steerMsgs, "s2")
	s.Contains(followMsgs, "f1")
	s.Contains(followMsgs, "f2")
}

// TestLoopQueues_优先级排序 验证 steering 队列优先排空（executor 每轮先 drain steering）
func (s *LoopQueuesSuite) TestLoopQueues_优先级排序() {
	q := task_loop.NewLoopQueues(16)
	q.PushFollowUp("followup-msg")
	q.PushSteer("steer-msg")

	// steering 先排空，体现 steer 优先于 followup
	steerMsgs := q.DrainSteering()
	s.Equal(1, len(steerMsgs))
	s.Equal("steer-msg", steerMsgs[0])

	// followup 后排空
	followMsgs := q.DrainFollowUp()
	s.Equal(1, len(followMsgs))
	s.Equal("followup-msg", followMsgs[0])
}

// TestLoopQueues_HasPending 验证 HasFollowUp 检查待处理事件
func (s *LoopQueuesSuite) TestLoopQueues_HasPending() {
	q := task_loop.NewLoopQueues(16)
	s.False(q.HasFollowUp())

	q.PushFollowUp("msg")
	s.True(q.HasFollowUp())

	q.DrainFollowUp()
	s.False(q.HasFollowUp())
}

// TestLoopQueues_IsAborted 验证中止检测通过协调器 IsAborted 而非队列
// LoopQueues 本身无 IsAborted 方法，abort 由 LoopCoordinator 管理
func (s *LoopQueuesSuite) TestLoopQueues_IsAborted() {
	lc := task_loop.NewLoopCoordinator(nil)
	s.False(lc.IsAborted())

	lc.RequestAbort()
	s.True(lc.IsAborted())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestLoopQueuesSuite 运行 LoopQueuesSuite
func TestLoopQueuesSuite(t *testing.T) {
	suite.Run(t, new(LoopQueuesSuite))
}
