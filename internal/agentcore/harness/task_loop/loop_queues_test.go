package task_loop

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestNewLoopQueues 测试创建双队列缓冲
func TestNewLoopQueues(t *testing.T) {
	q := NewLoopQueues(16)
	assert.NotNil(t, q)
}

// TestLoopQueues_PushAndDrainSteering 测试 steering 队列推入和排空
func TestLoopQueues_PushAndDrainSteering(t *testing.T) {
	q := NewLoopQueues(16)
	assert.Empty(t, q.DrainSteering())

	q.PushSteer("msg1")
	q.PushSteer("msg2")
	msgs := q.DrainSteering()
	assert.Equal(t, []string{"msg1", "msg2"}, msgs)
	// 排空后应为空
	assert.Empty(t, q.DrainSteering())
}

// TestLoopQueues_PushAndDrainFollowUp 测试 follow_up 队列推入和排空
func TestLoopQueues_PushAndDrainFollowUp(t *testing.T) {
	q := NewLoopQueues(16)
	assert.Empty(t, q.DrainFollowUp())
	assert.False(t, q.HasFollowUp())

	q.PushFollowUp("f1")
	q.PushFollowUp("f2")
	assert.True(t, q.HasFollowUp())
	msgs := q.DrainFollowUp()
	assert.Equal(t, []string{"f1", "f2"}, msgs)
	assert.False(t, q.HasFollowUp())
	// 排空后应为空
	assert.Empty(t, q.DrainFollowUp())
}

// TestLoopQueues_HasFollowUp 测试 HasFollowUp
func TestLoopQueues_HasFollowUp(t *testing.T) {
	q := NewLoopQueues(16)
	assert.False(t, q.HasFollowUp())

	q.PushFollowUp("msg")
	assert.True(t, q.HasFollowUp())

	q.DrainFollowUp()
	assert.False(t, q.HasFollowUp())
}

// TestLoopQueues_FollowUpLen 测试 FollowUpLen 返回当前积压长度
// （集成测试同步屏障的轮询依据）
func TestLoopQueues_FollowUpLen(t *testing.T) {
	q := NewLoopQueues(16)
	assert.Equal(t, 0, q.FollowUpLen())

	q.PushFollowUp("f1")
	assert.Equal(t, 1, q.FollowUpLen())

	q.PushFollowUp("f2")
	q.PushFollowUp("f3")
	assert.Equal(t, 3, q.FollowUpLen())

	// 排空后归零
	q.DrainFollowUp()
	assert.Equal(t, 0, q.FollowUpLen())
}

// TestLoopQueues_SteerLen 测试 SteerLen 返回当前积压长度
// （集成测试同步屏障的轮询依据，与 FollowUpLen 同族）
func TestLoopQueues_SteerLen(t *testing.T) {
	q := NewLoopQueues(16)
	assert.Equal(t, 0, q.SteerLen())

	q.PushSteer("s1")
	assert.Equal(t, 1, q.SteerLen())

	q.PushSteer("s2")
	q.PushSteer("s3")
	assert.Equal(t, 3, q.SteerLen())

	// 排空后归零
	q.DrainSteering()
	assert.Equal(t, 0, q.SteerLen())
}

// TestLoopQueues_DrainSteering_空队列 测试空队列排空
func TestLoopQueues_DrainSteering_空队列(t *testing.T) {
	q := NewLoopQueues(16)
	assert.Empty(t, q.DrainSteering())
}

// TestLoopQueues_DrainFollowUp_空队列 测试空队列排空
func TestLoopQueues_DrainFollowUp_空队列(t *testing.T) {
	q := NewLoopQueues(16)
	assert.Empty(t, q.DrainFollowUp())
}

// TestLoopQueues_满队列Push丢弃 测试满队列时非阻塞丢弃
func TestLoopQueues_满队列Push丢弃(t *testing.T) {
	q := NewLoopQueues(2) // 容量仅为 2
	q.PushSteer("a")
	q.PushSteer("b")
	q.PushSteer("c") // 应被丢弃（满），记录日志但不阻塞

	msgs := q.DrainSteering()
	assert.Equal(t, []string{"a", "b"}, msgs) // c 被丢弃
}

// TestLoopQueues_交替操作 测试交替推入排空
func TestLoopQueues_交替操作(t *testing.T) {
	q := NewLoopQueues(16)

	q.PushSteer("s1")
	q.PushFollowUp("f1")
	assert.True(t, q.HasFollowUp())

	steerMsgs := q.DrainSteering()
	assert.Equal(t, []string{"s1"}, steerMsgs)
	assert.True(t, q.HasFollowUp()) // steering 排空不影响 follow_up

	followMsgs := q.DrainFollowUp()
	assert.Equal(t, []string{"f1"}, followMsgs)
	assert.False(t, q.HasFollowUp())
}

// TestNewLoopQueues_默认容量 测试默认容量
func TestNewLoopQueues_默认容量(t *testing.T) {
	q := NewLoopQueues(0) // 0 使用默认值
	assert.NotNil(t, q)

	q2 := NewLoopQueues(-1) // 负数使用默认值
	assert.NotNil(t, q2)
}
