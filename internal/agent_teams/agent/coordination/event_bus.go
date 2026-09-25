package coordination

import (
	"context"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EventBus 事件驱动的唤醒循环，用于团队协调。
//
// 两条唤醒路径，同一回调：
//  1. 事件驱动：transport/Messager 事件触发即时唤醒
//  2. 轮询回退：周期性定时器，捕获空闲 agent 可能遗漏的状态变更
//
// 所有决策逻辑在 DeepAgent 中——本结构体只管生命周期和唤醒。
// Python: EventBus
type EventBus struct {
	// mu 保护 running / pollsPaused / cancelFunc / mailboxPollCancel / taskPollCancel 等字段
	mu sync.Mutex
	// role 拥有此事件循环的角色
	role schema.TeamRole
	// mailboxPollInterval 邮箱轮询间隔（秒）
	mailboxPollInterval float64
	// taskPollInterval 任务轮询间隔（秒）
	taskPollInterval float64
	// wakeCallback 在 start() 时绑定，而非构造时绑定
	// 这样 CoordinationKernel 可以打破 bus ↔ dispatcher 循环依赖：
	// 建 bus → 建 dispatcher（bus 作为 PollController）→ start 时绑定 dispatcher.dispatch
	wakeCallback WakeCallback
	// running 事件循环是否运行中
	running bool
	// pollsPaused 周期轮询是否暂停
	pollsPaused bool
	// periodicPollEnabled 是否启用周期轮询（HUMAN_AGENT 角色为 false）
	periodicPollEnabled bool
	// eventCh 事件队列 channel
	eventCh chan types.CoordinationEvent
	// cancelFunc 用于停止 runLoop goroutine
	cancelFunc context.CancelFunc
	// mailboxPollCancel 邮箱轮询定时器取消函数
	mailboxPollCancel context.CancelFunc
	// taskPollCancel 任务轮询定时器取消函数
	taskPollCancel context.CancelFunc
}

// ──────────────────────────── 枚举 ────────────────────────────

// WakeCallback 事件唤醒回调，当事件到达时调用。
// Python: WakeCallback = Callable[[CoordinationEvent], Awaitable[None]]
type WakeCallback func(ctx context.Context, event types.CoordinationEvent)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 协调子系统的日志组件
var logComponent = logger.ComponentChannel

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEventBus 创建事件总线实例。
// Python: EventBus.__init__
func NewEventBus(role schema.TeamRole, mailboxPollInterval, taskPollInterval float64) *EventBus {
	return &EventBus{
		role:                role,
		mailboxPollInterval: mailboxPollInterval,
		taskPollInterval:    taskPollInterval,
		periodicPollEnabled: role != schema.TeamRoleHumanAgent,
		eventCh:             make(chan types.CoordinationEvent, 256),
	}
}

// Role 返回拥有此事件循环的角色。
func (b *EventBus) Role() schema.TeamRole { return b.role }

// IsRunning 返回事件循环是否运行中。
func (b *EventBus) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}

// PollsPaused 返回周期轮询是否暂停。
func (b *EventBus) PollsPaused() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pollsPaused
}

// Start 启动事件循环和轮询定时器。
// wakeCallback 在此绑定而非构造时，以便 CoordinationKernel 打破循环依赖。
// Python: EventBus.start
func (b *EventBus) Start(ctx context.Context, wakeCallback WakeCallback) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.running {
		return
	}
	if wakeCallback != nil {
		b.wakeCallback = wakeCallback
	}
	logger.Info(logComponent).Str("role", string(b.role)).Msg("EventBus starting")
	b.running = true
	loopCtx, cancel := context.WithCancel(ctx)
	b.cancelFunc = cancel
	go b.runLoop(loopCtx)
	b.startPollTasksLocked(loopCtx)
}

// Stop 停止事件循环、取消轮询定时器。
// Python: EventBus.stop
func (b *EventBus) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.running {
		return
	}
	logger.Info(logComponent).Str("role", string(b.role)).Msg("EventBus stopping")
	b.running = false
	// 重置暂停标志，确保后续 start() 不继承残留状态
	b.pollsPaused = false

	// 取消轮询定时器
	if b.mailboxPollCancel != nil {
		b.mailboxPollCancel()
		b.mailboxPollCancel = nil
	}
	if b.taskPollCancel != nil {
		b.taskPollCancel()
		b.taskPollCancel = nil
	}

	// 发送 shutdown 事件通知 runLoop 退出
	select {
	case b.eventCh <- types.CoordinationEvent{Inner: &types.InnerEventMessage{EventType: types.InnerEventTypeShutdown}}:
	default:
		// channel 满则跳过，context cancel 也会退出
	}

	// 取消 runLoop 的 context
	if b.cancelFunc != nil {
		b.cancelFunc()
		b.cancelFunc = nil
	}
}

// PausePolls 停止周期轮询，但保持事件循环运行。
// Python: EventBus.pause_polls
func (b *EventBus) PausePolls() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.pollsPaused {
		return
	}
	logger.Info(logComponent).Str("role", string(b.role)).Msg("EventBus pausing polls")
	if b.mailboxPollCancel != nil {
		b.mailboxPollCancel()
		b.mailboxPollCancel = nil
	}
	if b.taskPollCancel != nil {
		b.taskPollCancel()
		b.taskPollCancel = nil
	}
	b.pollsPaused = true
}

// ResumePolls 恢复周期轮询。
// Python: EventBus.resume_polls
func (b *EventBus) ResumePolls() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.pollsPaused || !b.running {
		return
	}
	logger.Info(logComponent).Str("role", string(b.role)).Msg("EventBus resuming polls")
	b.startPollTasksLocked(context.Background())
	b.pollsPaused = false
}

// Enqueue 将事件推入处理队列。
// Python: EventBus.enqueue
func (b *EventBus) Enqueue(event types.CoordinationEvent) {
	b.eventCh <- event
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// startPollTasksLocked 启动周期轮询定时器（邮箱+任务各一个 goroutine）。
// Human-agent 角色不启动周期轮询（periodicPollEnabled = false）。
// 调用者必须持有 b.mu。
// Python: EventBus._start_poll_tasks
func (b *EventBus) startPollTasksLocked(ctx context.Context) {
	if !b.periodicPollEnabled {
		return
	}
	mailboxCtx, mailboxCancel := context.WithCancel(ctx)
	b.mailboxPollCancel = mailboxCancel
	go b.pollLoop(mailboxCtx, types.InnerEventTypePollMailbox, b.mailboxPollInterval)

	taskCtx, taskCancel := context.WithCancel(ctx)
	b.taskPollCancel = taskCancel
	go b.pollLoop(taskCtx, types.InnerEventTypePollTask, b.taskPollInterval)
}

// runLoop 后台 goroutine：从 channel 读取事件，调用 wakeCallback。
// Python: EventBus._run_loop
func (b *EventBus) runLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case event := <-b.eventCh:
			// 检查 shutdown
			if event.Inner != nil && event.Inner.EventType == types.InnerEventTypeShutdown {
				return
			}
			if b.wakeCallback != nil {
				// 铁律 3：吞普通异常（log + continue），对齐 Python AsyncCallbackFramework
				func() {
					defer func() {
						if r := recover(); r != nil {
							eventType := event.EventType()
							logger.Error(logComponent).
								Str("event_type", eventType).
								Any("recover", r).
								Msg("EventBus: error in wakeCallback")
						}
					}()
					b.wakeCallback(ctx, event)
				}()
			}
		}
	}
}

// pollLoop 周期回退：每隔 interval 秒入队一个轮询事件。
// Python: EventBus._poll_loop
func (b *EventBus) pollLoop(ctx context.Context, eventType types.InnerEventType, interval float64) {
	ticker := time.NewTicker(time.Duration(interval * float64(time.Second)))
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.mu.Lock()
			isRunning := b.running
			b.mu.Unlock()
			if !isRunning {
				return
			}
			b.Enqueue(types.CoordinationEvent{
				Inner: &types.InnerEventMessage{EventType: eventType},
			})
		}
	}
}
