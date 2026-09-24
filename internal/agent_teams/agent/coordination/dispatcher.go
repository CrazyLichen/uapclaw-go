package coordination

import (
	"context"
	"fmt"

	callback "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EventDispatcher 将协调事件路由到场景 Handler。
//
// 持有触发规则 + 私有 CallbackFramework 实例。
// 每团队注册隔离：framework 是本 dispatcher 私有的，协调事件不进入全局 Runner.callback_framework。
//
// Python: EventDispatcher
type EventDispatcher struct {
	// round dispatch() 只需要 round-readiness 查询
	round AgentRoundController
	// blueprint 静态身份
	blueprint DispatcherBlueprint
	// infra per-process 容器
	infra DispatcherInfra
	// framework 私有回调框架实例（与全局单例隔离）
	framework *callback.CallbackFramework

	// 六个场景 handler（公开暴露供测试直接访问）
	// TODO(#9.63-Task6): handler 字段在 Task 6 接线后非 nil
	Lifecycle      CallbacksProvider
	Member         CallbacksProvider
	Message        CallbacksProvider
	TaskBoard      CallbacksProvider
	StaleTask      CallbacksProvider
	TeamCompletion CallbacksProvider
}

// ──────────────────────────── 枚举 ────────────────────────────

// coordCallbackFunc coordination handler 的原生回调签名。
type coordCallbackFunc func(ctx context.Context, event CoordinationEvent)

// CallbacksProvider handler 回调注册接口。
// 由 BaseCoordinationHandler 实现，供 EventDispatcher 遍历注册。
type CallbacksProvider interface {
	// GetCallbacks 返回 event_key → 回调方法，供 framework 注册。
	GetCallbacks() map[string]EventCallbackFunc
}

// EventCallbackFunc 协调事件回调函数类型（同包兼容签名）。
type EventCallbackFunc func(ctx context.Context, event CoordinationEvent)

// ──────────────────────────── 常量 ────────────────────────────

// coordEventMapKey 适配层：在 map[string]any 中存储 CoordinationEvent 的 key。
const coordEventMapKey = "__coordination_event"

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// AgentRoundController Round 级控制面，与 TeamHarness 打交道。
// Python: AgentRoundController(Protocol)
type AgentRoundController interface {
	// IsAgentReady 返回 agent 是否已完成初始化
	IsAgentReady() bool
	// IsAgentRunning 返回 agent 是否在活跃 round 中
	IsAgentRunning() bool
	// HasInFlightRound 返回是否有已调度但未完成的 round
	HasInFlightRound() bool
	// HasPendingInterrupt 返回是否有未解决的工具中断
	HasPendingInterrupt() bool
	// CancelAgent 取消运行中的 agent 任务
	CancelAgent(ctx context.Context) error
	// DeliverInput 确保内容到达 DeepAgent，不论当前状态
	DeliverInput(ctx context.Context, content any, useSteer bool) error
	// ResumeInterrupt 以结构化输入恢复 HITL 中断
	ResumeInterrupt(ctx context.Context, userInput any) error
}

// TeamLifecycleController TeamAgent 级生命周期效果，跨多个 manager。
// Python: TeamLifecycleController(Protocol)
type TeamLifecycleController interface {
	// ShutdownSelf 响应团队解散强制关闭自身
	ShutdownSelf(ctx context.Context) error
	// ConcludeCompletedRound 发出 team-completed 标记块，关闭 leader 流
	ConcludeCompletedRound(ctx context.Context, memberCount, taskCount int) error
}

// PollController EventBus 自身的周期轮询控制面。
// Python: PollController(Protocol)
// Handler 直接操作此接口而非通过 host 中转——EventBus 已知道如何暂停/恢复自己的 poll 任务。
type PollController interface {
	// PausePolls 暂停事件总线的周期轮询
	PausePolls()
	// ResumePolls 恢复事件总线的周期轮询
	ResumePolls()
}

// DispatcherHost 组合 host 契约，供 kernel 和 dispatcher 使用。
// Python: DispatcherHost(AgentRoundController, TeamLifecycleController, Protocol)
type DispatcherHost interface {
	AgentRoundController
	TeamLifecycleController
}

// DispatcherBlueprint Dispatcher 构造所需的 blueprint 接口。
// 从 TeamAgentBlueprint 中提取 dispatcher 实际需要的窄接口。
type DispatcherBlueprint interface {
	// Role 返回团队角色
	Role() schema.TeamRole
	// MemberName 返回成员名
	MemberName() string
}

// DispatcherInfra Dispatcher 构造所需的 infra 接口。
// 从 TeamInfra 中提取 dispatcher 实际需要的窄接口。
type DispatcherInfra interface{}

// wrapCallback 将 coordination handler 回调包装为 CallbackFramework 的 CustomCallbackFunc。
// 注册时使用：fw.OnCustom(eventKey, wrapCallback(handlerMethod))
func wrapCallback(fn coordCallbackFunc) callback.CustomCallbackFunc {
	return func(ctx context.Context, data map[string]any) any {
		raw, ok := data[coordEventMapKey]
		if !ok {
			return nil
		}
		event, ok := raw.(CoordinationEvent)
		if !ok {
			return nil
		}
		fn(ctx, event)
		return nil
	}
}

// packEvent 将 CoordinationEvent 打包进 map[string]any，供 TriggerCustom 使用。
func packEvent(event CoordinationEvent) map[string]any {
	return map[string]any{coordEventMapKey: event}
}

// NewEventDispatcher 创建事件分发器。
// Python: EventDispatcher.__init__
// handlerProviders 按 (lifecycle, member, message, task_board, stale_task, team_completion) 顺序传入。
func NewEventDispatcher(
	host DispatcherHost,
	bp DispatcherBlueprint,
	inf DispatcherInfra,
	pollCtrl PollController,
	handlerProviders []CallbacksProvider,
) *EventDispatcher {
	fw := callback.NewCallbackFramework()

	d := &EventDispatcher{
		round:     host,
		blueprint: bp,
		infra:     inf,
		framework: fw,
	}

	// 按 Python 注册顺序存储 handler
	if len(handlerProviders) >= 1 {
		d.Lifecycle = handlerProviders[0]
	}
	if len(handlerProviders) >= 2 {
		d.Member = handlerProviders[1]
	}
	if len(handlerProviders) >= 3 {
		d.Message = handlerProviders[2]
	}
	if len(handlerProviders) >= 4 {
		d.TaskBoard = handlerProviders[3]
	}
	if len(handlerProviders) >= 5 {
		d.StaleTask = handlerProviders[4]
	}
	if len(handlerProviders) >= 6 {
		d.TeamCompletion = handlerProviders[5]
	}

	// 注册顺序决定 fan-out 顺序，与 Python 一致：
	// (lifecycle, member, message, task_board, stale_task, team_completion)
	for _, handler := range handlerProviders {
		for eventKey, cb := range handler.GetCallbacks() {
			fw.OnCustom(eventKey, wrapCallback(coordCallbackFunc(cb)))
		}
	}

	return d
}

// Dispatch 唤醒入口。应用粗筛规则，然后触发 framework。
// Python: EventDispatcher.dispatch
func (d *EventDispatcher) Dispatch(ctx context.Context, event CoordinationEvent) {
	// 粗筛 1：agent 未就绪，跳过
	if !d.round.IsAgentReady() {
		logger.Debug(logComponent).Msg("agent not ready, skipping coordination wake")
		return
	}

	role := d.blueprint.Role()

	// 内部事件分支
	if event.IsInner() {
		// Human-agent 绝不自发轮询
		if role == schema.TeamRoleHumanAgent &&
			(event.Inner.EventType == InnerEventTypePollTask ||
				event.Inner.EventType == InnerEventTypePollMailbox) {
			return
		}
		logger.Debug(logComponent).
			Str("event_type", string(event.Inner.EventType)).
			Any("payload", event.Inner.Payload).
			Msg("inner event received")
		d.framework.TriggerCustom(ctx, string(event.Inner.EventType), packEvent(event))
		return
	}

	// --- Transport 事件（跨进程 EventMessage）---
	if d.blueprint.MemberName() == "" {
		logger.Debug(logComponent).Msg("no member_name, skipping transport event")
		return
	}

	// Human-agent 角色白名单门控
	// Python: 对齐 dispatcher.py L262-272 的 7 种允许事件
	if role == schema.TeamRoleHumanAgent {
		allowed := map[string]bool{
			"team_cleaned":        true,
			"member_shutdown":     true,
			"member_canceled":     true,
			"team_standby":        true,
			"message":             true,
			"broadcast":           true,
			"task_claimed":        true,
		}
		if !allowed[event.Transport.EventType] {
			return
		}
	}

	d.framework.TriggerCustom(ctx, event.Transport.EventType, packEvent(event))
}

// Framework 返回私有回调框架实例（仅供同包测试使用）。
func (d *EventDispatcher) Framework() *callback.CallbackFramework {
	return d.framework
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// verifyDispatcherReady 校验 dispatcher 构造完整。
func (d *EventDispatcher) verifyDispatcherReady() error {
	if d.Lifecycle == nil || d.Member == nil || d.Message == nil ||
		d.TaskBoard == nil || d.StaleTask == nil || d.TeamCompletion == nil {
		return fmt.Errorf("EventDispatcher 未完成 handler 接线")
	}
	return nil
}
