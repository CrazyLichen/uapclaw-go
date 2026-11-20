package coordination

import (
	"context"
	"fmt"
	"sync"

	handlers "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/agent/coordination/handlers"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/agent/coordination/types"
	reliabilitypkg "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/rails/reliability"
	schema "github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/agent_teams/schema/events"
	callback "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// EventDispatcher 将协调事件路由到场景 Handler。
// 持有触发规则 + 私有 CallbackFramework 实例。
// 每团队注册隔离：framework 是本 dispatcher 私有的，协调事件不进入全局 Runner.callback_framework。
// Python: EventDispatcher
type EventDispatcher struct {
	// dispatchMu 保护 Dispatch 串行化，避免 EventBus goroutine 与外部调用并发访问 CallbackFramework
	dispatchMu sync.Mutex
	// round dispatch() 只需要 round-readiness 查询
	round types.AgentRoundController
	// blueprint 静态身份
	blueprint types.DispatcherBlueprint
	// infra per-process 容器
	infra types.DispatcherInfra
	// framework 私有回调框架实例（与全局单例隔离）
	framework *callback.CallbackFramework

	// 六个场景 handler（公开暴露供测试直接访问和 kernel 回调）
	// Python: (self.lifecycle, self.member, self.message, self.task_board, self.stale_task, self.team_completion)
	// Lifecycle 生命周期处理器
	Lifecycle *handlers.AgentLifecycleHandler
	// Member 成员处理器
	Member *handlers.MemberHandler
	// Message 消息处理器
	Message *handlers.MessageHandler
	// TaskBoard 任务面板处理器（autonomous/scheduled 变体二选一，F_62）
	// 两者都实现 GetCallbacks；按 dispatch_mode 在装配处选择。
	TaskBoard types.CallbacksProvider
	// StaleTask 过期任务处理器（autonomous/scheduled 变体二选一）
	StaleTask types.CallbacksProvider
	// TeamCompletion 团队完成处理器
	TeamCompletion *handlers.TeamCompletionHandler
	// Reliability 可靠性处理器（未启用为 nil）。
	// Python: dispatcher.reliability（dispatcher.py:263）
	Reliability *handlers.ReliabilityHandler
	// ExternalRuntime 外部运行时重试处理器（恒注册）。
	// Python: dispatcher.external_runtime（dispatcher.py:286）
	ExternalRuntime *handlers.ExternalRuntimeHandler
}

// ──────────────────────────── 枚举 ────────────────────────────

// coordCallbackFunc coordination handler 的原生回调签名。
type coordCallbackFunc func(ctx context.Context, event types.CoordinationEvent)

// ──────────────────────────── 常量 ────────────────────────────

// coordEventMapKey 适配层：在 map[string]any 中存储 CoordinationEvent 的 key。
const coordEventMapKey = "__coordination_event"

// ──────────────────────────── 导出函数 ────────────────────────────

// NewEventDispatcher 创建事件分发器，内部创建 6 个 handler 并注册回调。
// 对齐 Python EventDispatcher.__init__：在 __init__ 内部创建所有 handler。
// handler 创建顺序和注册顺序一致：
//
//	(lifecycle, member, message, task_board, stale_task, team_completion)
//	即：生命周期、成员、消息、任务面板、过期任务、团队完成
//
// 共享 staleClaimThrottle 映射在 Member 和 StaleTask 之间传递。
// Python: EventDispatcher.__init__
func NewEventDispatcher(
	host types.DispatcherHost,
	bp types.DispatcherBlueprint,
	inf types.DispatcherInfra,
	pollCtrl types.PollController,
) *EventDispatcher {
	fw := callback.NewCallbackFramework()

	// 对齐 Python: 创建共享节流映射
	staleClaimThrottle := make(map[string]float64)

	// 对齐 Python 注册顺序创建 handler
	lifecycle := handlers.NewAgentLifecycleHandler(host, bp, inf, pollCtrl)
	member := handlers.NewMemberHandler(host, bp, inf, pollCtrl, staleClaimThrottle)
	message := handlers.NewMessageHandler(host, bp, inf, pollCtrl)
	// F_62: dispatch mode 是静态 spec 配置——每个角色直接从它装配模式自有的
	// handler 变体。选择发生在装配处，从不发生在 handler 方法内部。
	// Python: EventDispatcher.__init__ 的 _TASK_BOARD_CLASS/_STALE_TASK_CLASS
	// 按 dispatch_mode 选类（dispatcher.py L215/L219）。
	var taskBoard types.CallbacksProvider
	var staleTask types.CallbacksProvider
	dispatchMode := ""
	if spec := bp.DispatcherSpec(); spec != nil {
		dispatchMode = spec.DispatchMode
	}
	if dispatchMode == "scheduled" {
		taskBoard = handlers.NewScheduledTaskBoardHandler(host, bp, inf, pollCtrl)
		staleTask = handlers.NewScheduledStaleTaskHandler(host, bp, inf, pollCtrl)
	} else {
		taskBoard = handlers.NewTaskBoardHandler(host, bp, inf, pollCtrl)
		staleTask = handlers.NewStaleTaskHandler(host, bp, inf, pollCtrl, staleClaimThrottle)
	}
	teamCompletion := handlers.NewTeamCompletionHandler(host, bp, inf, pollCtrl)

	// Python: dispatcher.py:258-286 — reliability handler 仅在团队 opt-in
	// （spec.reliability.enabled）时装配（含团队级 pingpong detector 的构造）；
	// external runtime 重试 handler 恒注册（leader-only 方法内守卫）。
	var reliabilityHandler *handlers.ReliabilityHandler
	if cfg := bp.DispatcherReliability(); cfg != nil && cfg.Enabled {
		reliabilityHandler = handlers.NewReliabilityHandler(
			host, bp, inf, pollCtrl,
			reliabilitypkg.BuildRemediationPolicy(cfg),
			reliabilitypkg.BuildPingPongDetector(cfg),
		)
	}
	externalRuntime := handlers.NewExternalRuntimeHandler(host, bp, inf, pollCtrl)

	d := &EventDispatcher{
		round:           host,
		blueprint:       bp,
		infra:           inf,
		framework:       fw,
		Lifecycle:       lifecycle,
		Member:          member,
		Message:         message,
		TaskBoard:       taskBoard,
		StaleTask:       staleTask,
		TeamCompletion:  teamCompletion,
		Reliability:     reliabilityHandler,
		ExternalRuntime: externalRuntime,
	}

	// 注册顺序决定 fan-out 顺序，与 Python 一致：
	// 依次为 lifecycle、member、message、task_board、stale_task、team_completion，
	// 之后 reliability（非 nil 时）、external_runtime 恒最后。
	// 即：生命周期、成员、消息、任务面板、过期任务、团队完成、可靠性、外部运行时
	handlerList := []types.CallbacksProvider{lifecycle, member, message, taskBoard, staleTask, teamCompletion}
	if reliabilityHandler != nil {
		handlerList = append(handlerList, reliabilityHandler)
	}
	handlerList = append(handlerList, externalRuntime)
	for _, handler := range handlerList {
		for eventKey, cb := range handler.GetCallbacks() {
			fw.OnCustom(eventKey, wrapCallback(coordCallbackFunc(cb)))
		}
	}

	return d
}

// Dispatch 唤醒入口。应用粗筛规则，然后触发 framework。
// Python: EventDispatcher.dispatch
func (d *EventDispatcher) Dispatch(ctx context.Context, event types.CoordinationEvent) {
	d.dispatchMu.Lock()
	defer d.dispatchMu.Unlock()

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
			(event.Inner.EventType == types.InnerEventTypePollTask ||
				event.Inner.EventType == types.InnerEventTypePollMailbox) {
			return
		}
		logger.Debug(logComponent).
			Str("event_type", string(event.Inner.EventType)).
			Any("payload", event.Inner.Payload).
			Msg("inner event received")
		d.framework.TriggerCustom(ctx, string(event.Inner.EventType), packEvent(event))
		return
	}

	// ── Transport 事件（跨进程 EventMessage）──
	if d.blueprint.DispatcherMemberName() == "" {
		logger.Debug(logComponent).Msg("no member_name, skipping transport event")
		return
	}

	// Human-agent 角色白名单门控
	// Python: 对齐 dispatcher.py L262-272 的 7 种允许事件
	if role == schema.TeamRoleHumanAgent {
		allowed := map[string]bool{
			events.TeamEventCleaned:        true,
			events.TeamEventMemberShutdown: true,
			events.TeamEventMemberCanceled: true,
			events.TeamEventStandby:        true,
			events.TeamEventMessage:        true,
			events.TeamEventBroadcast:      true,
			events.TeamEventTaskClaimed:    true,
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

// wrapCallback 将 coordination handler 回调包装为 CallbackFramework 的 CustomCallbackFunc。
// 注册时使用：fw.OnCustom(eventKey, wrapCallback(handlerMethod))
func wrapCallback(fn coordCallbackFunc) callback.CustomCallbackFunc {
	return func(ctx context.Context, data map[string]any) any {
		raw, ok := data[coordEventMapKey]
		if !ok {
			return nil
		}
		event, ok := raw.(types.CoordinationEvent)
		if !ok {
			return nil
		}
		fn(ctx, event)
		return nil
	}
}

// packEvent 将 CoordinationEvent 打包进 map[string]any，供 TriggerCustom 使用。
func packEvent(event types.CoordinationEvent) map[string]any {
	return map[string]any{coordEventMapKey: event}
}

// verifyDispatcherReady 校验 dispatcher 构造完整。
func (d *EventDispatcher) verifyDispatcherReady() error {
	if d.Lifecycle == nil || d.Member == nil || d.Message == nil ||
		d.TaskBoard == nil || d.StaleTask == nil || d.TeamCompletion == nil {
		return fmt.Errorf("EventDispatcher 未完成 handler 接线")
	}
	return nil
}
