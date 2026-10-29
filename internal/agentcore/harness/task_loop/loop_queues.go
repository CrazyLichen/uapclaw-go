package task_loop

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoopQueues 双队列缓冲，桥接 EventHandler 与 Executor/Loop。
// steering: 引导指令队列，由 executor 每次内部 invoke 前排空
// followUp: 后续消息队列，由外层任务循环每次迭代完成后排空
// Python: LoopQueues
type LoopQueues struct {
	// steering 引导指令队列
	steering chan string
	// followUp 后续消息队列
	followUp chan string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// 默认队列缓冲区大小
const defaultQueueCap = 64

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewLoopQueues 创建双队列缓冲。
// cap 为各队列缓冲区大小，若 cap <= 0 则使用默认值 64。
// Python: LoopQueues.__init__
func NewLoopQueues(cap int) *LoopQueues {
	if cap <= 0 {
		cap = defaultQueueCap
	}
	return &LoopQueues{
		steering: make(chan string, cap),
		followUp: make(chan string, cap),
	}
}

// PushSteer 非阻塞推入引导指令。
// Python: LoopQueues.push_steer (put_nowait)
// 满队列时丢弃并记录日志。
func (q *LoopQueues) PushSteer(msg string) {
	select {
	case q.steering <- msg:
	default:
		logger.Warn(logComponent).
			Str("queue", "steering").
			Str("msg", msg).
			Msg("queue full, discarding bootstrap instruction")
	}
}

// PushFollowUp 非阻塞推入后续消息。
// Python: LoopQueues.push_follow_up (put_nowait)
// 满队列时丢弃并记录日志。
func (q *LoopQueues) PushFollowUp(msg string) {
	select {
	case q.followUp <- msg:
	default:
		logger.Warn(logComponent).
			Str("queue", "follow_up").
			Str("msg", msg).
			Msg("queue full, discarding follow-up message")
	}
}

// HasFollowUp 非阻塞检查是否有待处理的后续消息。
// Python: LoopQueues.has_follow_up
func (q *LoopQueues) HasFollowUp() bool {
	return len(q.followUp) > 0
}

// FollowUpLen 返回 follow-up 队列当前积压长度（只读）。
// [Go 扩展] 测试同步用：等待 N 条 follow-up 全部入队的轮询依据。
func (q *LoopQueues) FollowUpLen() int {
	return len(q.followUp)
}

// SteerLen 返回 steering 队列当前积压长度（只读）。
// [Go 扩展] 测试同步用：Steer 发布是 fire-and-forget（事件经 MQ 消费者
// goroutine 异步转入本队列），Go 真并行下消费者与 ReAct 迭代 drain 自由
// 竞争——集成测试需轮询本访问器确认 steer 已真实入队后再继续，消除
// Steer→drain 之间的竞态窗口（asyncio 单线程 FIFO 无此问题）。
func (q *LoopQueues) SteerLen() int {
	return len(q.steering)
}

// DrainSteering 非阻塞一次性排空所有引导指令。
// Python: LoopQueues.drain_steering
func (q *LoopQueues) DrainSteering() []string {
	msgs := make([]string, 0, len(q.steering))
	for {
		select {
		case msg := <-q.steering:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}

// DrainFollowUp 非阻塞一次性排空所有后续消息。
// Python: LoopQueues.drain_follow_up
func (q *LoopQueues) DrainFollowUp() []string {
	msgs := make([]string, 0, len(q.followUp))
	for {
		select {
		case msg := <-q.followUp:
			msgs = append(msgs, msg)
		default:
			return msgs
		}
	}
}
