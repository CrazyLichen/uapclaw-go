package coordination

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KernelHost CoordinationKernel 对宿主 TeamAgent 所需的窄接口。
// 与 Python CoordinationKernel.__init__(host: TeamAgent) 对应，
// 但 Go 提取为窄接口避免循环依赖。
type KernelHost interface {
	types.DispatcherHost
	// Role 返回团队角色
	Role() schema.TeamRole
	// MemberName 返回成员名
	MemberName() string
	// Blueprint 返回 blueprint
	Blueprint() types.DispatcherBlueprint
	// Infra 返回 infra
	Infra() types.DispatcherInfra
}

// CoordinationKernel 协调子系统门面，拥有 EventBus + EventDispatcher 的完整生命周期。
//
// 持有宿主 TeamAgent 的反向引用，因为协调需要跨多个协作者
// （stream_controller, session_manager, spawn_manager 等）。
// 将协调逻辑集中在此处，保持 TeamAgent 专注于 BaseAgent 公共契约。
//
// 内部事件总线和分发器在此构造和拥有；
// 调用者通过此 kernel 操作，不直接接触 bus。
//
// 生命周期状态机：idle → running(start) → paused(pause) → stopped(stop)。
// stopped 是终态；后续 pause/stop 调用变为 no-op。
//
// Python: CoordinationKernel
type CoordinationKernel struct {
	// host 宿主 TeamAgent 引用
	host KernelHost
	// eventBus 事件总线（setup 后非 nil）
	eventBus *EventBus
	// dispatcher 事件分发器（setup 后非 nil）
	dispatcher *EventDispatcher
	// subscribedTopics 已订阅的传输主题列表
	subscribedTopics []string
	// lifecycleState 生命周期状态："idle" | "running" | "paused" | "stopped"
	lifecycleState string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// kernelStateIdle 空闲态
	kernelStateIdle = "idle"
	// kernelStateRunning 运行态
	kernelStateRunning = "running"
	// kernelStatePaused 暂停态
	kernelStatePaused = "paused"
	// kernelStateStopped 停止态（终态）
	kernelStateStopped = "stopped"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCoordinationKernel 创建协调内核实例。
// Python: CoordinationKernel.__init__
func NewCoordinationKernel(host KernelHost) *CoordinationKernel {
	return &CoordinationKernel{
		host:           host,
		lifecycleState: kernelStateIdle,
	}
}

// Setup 构造 EventBus + EventDispatcher。
// 在 TeamAgent.configure() 确定角色后调用。
// 先建 bus，再将 bus 作为 PollController 传给 dispatcher。
// dispatcher.dispatch 在 Start() 时绑定回 bus 作为 wake callback，此处不绑定。
// Python: CoordinationKernel.setup(role)
func (k *CoordinationKernel) Setup(role schema.TeamRole, bp types.DispatcherBlueprint, inf types.DispatcherInfra) {
	eventBus := NewEventBus(role, 30.0, 30.0)
	dispatcher := NewEventDispatcher(k.host, bp, inf, eventBus)
	k.eventBus = eventBus
	k.dispatcher = dispatcher

	logger.Info(logComponent).
		Str("role", string(role)).
		Msg("CoordinationKernel setup 完成")
}

// EventBus 返回事件总线实例（setup 前为 nil）。
func (k *CoordinationKernel) EventBus() *EventBus {
	return k.eventBus
}

// Dispatcher 返回事件分发器实例（setup 前为 nil）。
func (k *CoordinationKernel) Dispatcher() *EventDispatcher {
	return k.dispatcher
}

// SubscribedTopics 返回已订阅的传输主题列表。
func (k *CoordinationKernel) SubscribedTopics() []string {
	return k.subscribedTopics
}

// IsRunning 返回事件总线是否运行中。
func (k *CoordinationKernel) IsRunning() bool {
	return k.eventBus != nil && k.eventBus.IsRunning()
}

// LifecycleState 返回当前生命周期状态。
func (k *CoordinationKernel) LifecycleState() string {
	return k.lifecycleState
}

// Start 启动协调子系统。
// 绑定 dispatcher.dispatch 为 wake callback，启动事件总线和轮询定时器。
// 对齐 Python: CoordinationKernel.start(session)
//
// 注意：Python 的 start() 包含大量基础设施初始化（session bind、workspace init、
// memory toolkit wire、member status update 等），这些在 Go 端由各自的 Manager 负责，
// kernel 只关注协调子系统的启动。
func (k *CoordinationKernel) Start(ctx context.Context) {
	if k.eventBus == nil {
		return
	}
	memberName := k.host.MemberName()
	if memberName == "" {
		memberName = "?"
	}
	logger.Info(logComponent).
		Str("member_name", memberName).
		Msg("coordination starting")

	// 对齐 Python: bus.start(wake_callback=dispatcher.dispatch)
	if k.dispatcher == nil {
		logger.Error(logComponent).Msg("CoordinationKernel.start() requires setup() before start()")
		return
	}
	if !k.eventBus.IsRunning() {
		k.eventBus.Start(ctx, func(ctx context.Context, event types.CoordinationEvent) {
			k.dispatcher.Dispatch(ctx, event)
		})
	}

	// 对齐 Python: 重新装备 team-completion 上升沿保护
	if k.dispatcher != nil && k.dispatcher.TeamCompletion != nil {
		k.dispatcher.TeamCompletion.Rearm()
	}

	k.lifecycleState = kernelStateRunning
}

// Pause 暂停协调子系统（幂等）。
// 仅从 running 态有效转换；paused/stopped/idle 短路返回。
// Python: CoordinationKernel.pause()
// TODO(#9.63): 等基础设施就绪后补充完整暂停逻辑
// （drain_agent_task, persist_allocator_state, mark_live_teammates, unsubscribe, close_stream 等）
func (k *CoordinationKernel) Pause() {
	if k.lifecycleState != kernelStateRunning {
		return
	}
	memberName := k.host.MemberName()
	if memberName == "" {
		memberName = "?"
	}
	logger.Info(logComponent).
		Str("member_name", memberName).
		Msg("coordination pausing (persistent)")

	// 停止事件总线
	if k.eventBus != nil {
		k.eventBus.Stop()
	}

	k.lifecycleState = kernelStatePaused
}

// Stop 停止协调子系统（幂等，终态）。
// idle/stopped 为 no-op；paused → stop 和 running → stop 均有效。
// Python: CoordinationKernel.stop()
// TODO(#9.63): 等基础设施就绪后补充完整停止逻辑
// （drain_agent_task, shutdown_all_handles, memory_manager.close 等）
func (k *CoordinationKernel) Stop() {
	if k.lifecycleState == kernelStateIdle || k.lifecycleState == kernelStateStopped {
		return
	}
	memberName := k.host.MemberName()
	if memberName == "" {
		memberName = "?"
	}
	logger.Info(logComponent).
		Str("member_name", memberName).
		Msg("coordination stopping")

	// 停止事件总线
	if k.eventBus != nil {
		k.eventBus.Stop()
	}

	k.lifecycleState = kernelStateStopped
}

// EnqueueUserInput 将用户输入事件推入总线。
// 对齐 Python: CoordinationKernel.enqueue_user_input(inputs)
func (k *CoordinationKernel) EnqueueUserInput(content string) {
	if k.eventBus == nil {
		return
	}
	k.eventBus.Enqueue(types.CoordinationEvent{
		Inner: &types.InnerEventMessage{
			EventType: types.InnerEventTypeUserInput,
			Payload:   map[string]any{"content": content},
		},
	})
}

// Enqueue 将事件推入处理队列。
// 对齐 Python: CoordinationKernel.enqueue(event)
func (k *CoordinationKernel) Enqueue(event types.CoordinationEvent) {
	if k.eventBus == nil {
		return
	}
	k.eventBus.Enqueue(event)
}

// WakeMailboxIfInterruptCleared Teammate 专有：无 pending interrupt 时唤醒邮箱轮询。
// 对齐 Python: CoordinationKernel.wake_mailbox_if_interrupt_cleared
func (k *CoordinationKernel) WakeMailboxIfInterruptCleared() {
	if k.host.Role() != schema.TeamRoleTeammate {
		return
	}
	if k.host.HasPendingInterrupt() {
		return
	}
	if k.eventBus == nil {
		return
	}
	k.eventBus.Enqueue(types.CoordinationEvent{
		Inner: &types.InnerEventMessage{
			EventType: types.InnerEventTypePollMailbox,
		},
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────
