package task_loop

import (
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoopQueues 双队列缓冲，桥接 EventHandler 与 Executor/Loop。
// steering: 引导指令队列，由 executor 每次内部 invoke 前排空
// followUp: 后续消息队列，由外层任务循环每次迭代完成后排空
// 对齐 Python: LoopQueues
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
// 对齐 Python: LoopQueues.__init__
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
// 对齐 Python: LoopQueues.push_steer (put_nowait)
// 满队列时丢弃并记录日志。
func (q *LoopQueues) PushSteer(msg string) {
	select {
	case q.steering <- msg:
	default:
		logger.Warn(logComponent).
			Str("queue", "steering").
			Str("msg", msg).
			Msg("队列已满，丢弃引导指令")
	}
}

// PushFollowUp 非阻塞推入后续消息。
// 对齐 Python: LoopQueues.push_follow_up (put_nowait)
// 满队列时丢弃并记录日志。
func (q *LoopQueues) PushFollowUp(msg string) {
	select {
	case q.followUp <- msg:
	default:
		logger.Warn(logComponent).
			Str("queue", "follow_up").
			Str("msg", msg).
			Msg("队列已满，丢弃后续消息")
	}
}

// HasFollowUp 非阻塞检查是否有待处理的后续消息。
// 对齐 Python: LoopQueues.has_follow_up
func (q *LoopQueues) HasFollowUp() bool {
	return len(q.followUp) > 0
}

// DrainSteering 非阻塞一次性排空所有引导指令。
// 对齐 Python: LoopQueues.drain_steering
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
// 对齐 Python: LoopQueues.drain_follow_up
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

// ──────────────────────────── 非导出函数 ────────────────────────────
