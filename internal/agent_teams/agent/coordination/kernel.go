package coordination

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

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
	host types.KernelHost
	// eventBus 事件总线（setup 后非 nil）
	eventBus *EventBus
	// dispatcher 事件分发器（setup 后非 nil）
	dispatcher *EventDispatcher
	// subscribedTopics 已订阅的传输主题列表
	subscribedTopics []string
	// lifecycleState 生命周期状态："idle" | "running" | "paused" | "stopped"
	lifecycleState string
}

// kernelOptions 内部选项结构体
type kernelOptions struct {
	mailboxPollInterval float64
	taskPollInterval    float64
}

// KernelOption CoordinationKernel.Setup 的选项函数。
type KernelOption func(*kernelOptions)

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

// WithMailboxPollInterval 设置邮箱轮询间隔（秒）。
func WithMailboxPollInterval(interval float64) KernelOption {
	return func(o *kernelOptions) {
		o.mailboxPollInterval = interval
	}
}

// WithTaskPollInterval 设置任务轮询间隔（秒）。
func WithTaskPollInterval(interval float64) KernelOption {
	return func(o *kernelOptions) {
		o.taskPollInterval = interval
	}
}

// NewCoordinationKernel 创建协调内核实例。
// Python: CoordinationKernel.__init__
func NewCoordinationKernel(host types.KernelHost) *CoordinationKernel {
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
func (k *CoordinationKernel) Setup(role schema.TeamRole, bp types.DispatcherBlueprint, inf types.DispatcherInfra, opts ...KernelOption) {
	// 修复 S-05: 对齐 Python kernel.py:55-69
	// Python 从 host 获取 blueprint/infra，当 blueprint 或 infra 为 None 时抛 RuntimeError。
	// Go 侧当前 TeamAgent.Configure 传 nil（host 通过 KernelHost 子接口可达 blueprint/infra），
	// 当 bp/inf 为 nil 时记录警告，Dispatcher 会在实际使用时处理 nil 情况。
	if bp == nil || inf == nil {
		logger.Warn(logComponent).
			Bool("blueprint_nil", bp == nil).
			Bool("infra_nil", inf == nil).
			Msg("Setup 收到 nil blueprint/infra，对齐 Python 应从 host 获取")
	}
	// 应用选项
	o := &kernelOptions{}
	for _, opt := range opts {
		opt(o)
	}
	mailboxPollInterval := 30.0
	taskPollInterval := 30.0
	if o.mailboxPollInterval > 0 {
		mailboxPollInterval = o.mailboxPollInterval
	}
	if o.taskPollInterval > 0 {
		taskPollInterval = o.taskPollInterval
	}

	eventBus := NewEventBus(role, mailboxPollInterval, taskPollInterval)
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

// Start 启动协调子系统，编排完整初始化链。
// 对齐 Python: CoordinationKernel.start(session)
func (k *CoordinationKernel) Start(ctx context.Context, session ...any) {
	if k.eventBus == nil {
		return
	}
	memberName := k.host.MemberName()
	if memberName == "" {
		memberName = "?"
	}
	logger.Info(logComponent).Str("member_name", memberName).Msg("coordination starting")

	// 步骤 1: setMemberId
	k.host.SetMemberID(memberName)

	// 步骤 2: DB 初始化
	backendAccessor := k.host.TeamBackendAccessor()
	if backendAccessor != nil && backendAccessor.DB() != nil {
		_ = backendAccessor.DB().Initialize(ctx)
	}

	// 步骤 3: Session bind/release
	sessCtrl := k.host.SessionController()
	var sess any
	if len(session) > 0 {
		sess = session[0]
	}
	if sess != nil && sessCtrl != nil {
		_, _ = sessCtrl.BindSession(ctx, sess)
	} else if sessCtrl != nil {
		_ = sessCtrl.ReleaseSession(ctx)
	}

	// 步骤 4: [Leader] 检查全 SHUTDOWN → clean_team 或 recover
	if k.host.Role() == schema.TeamRoleLeader && backendAccessor != nil {
		if backendAccessor.DB() != nil && backendAccessor.DB().Team() != nil {
			existing, _ := backendAccessor.DB().Team().GetTeam(ctx, backendAccessor.TeamName())
			if existing != nil {
				members, _ := backendAccessor.ListMembers(ctx)
				if members != nil && allMembersShutdown(members) {
					logger.Warn(logComponent).Str("team_name", backendAccessor.TeamName()).
						Msg("team found with all teammates in SHUTDOWN — finalizing prior incomplete cleanup")
					_, _ = backendAccessor.CleanTeam(ctx)
				} else {
					_, _ = k.host.RecoverTeam(ctx)
				}
			}
		}
	}

	// 步骤 5: Workspace 初始化
	wsMgr := k.host.WorkspaceManager()
	if wsMgr != nil {
		var remoteURL string
		specAccessor := k.host.(types.BlueprintAccessor)
		if specAccessor != nil && specAccessor.SpecAny() != nil {
			remoteURL = extractWorkspaceRemoteURL(specAccessor.SpecAny())
		}
		_ = wsMgr.Initialize(ctx, remoteURL)
		k.host.SetWorkspaceInitialized()
	}

	// 步骤 6: Memory toolkit init
	memMgr := k.host.MemoryManager()
	harnessAccessor := k.host.HarnessAccessor()
	if memMgr != nil && harnessAccessor != nil {
		success, _ := memMgr.InitToolkit(ctx)
		if success {
			harnessAccessor.RegisterMemberTools(memMgr)
			if memMgr.ExtractionModel() == nil {
				memMgr.SetExtractionModel(harnessAccessor.Model())
			}
			_ = harnessAccessor.InjectMemberMemory(ctx, memMgr, "")
		}
	}

	// 步骤 7: 更新状态
	_ = k.host.UpdateStatus(ctx, schema.MemberStatusReady)

	// 步骤 8: EventBus 启动
	if k.dispatcher == nil {
		logger.Error(logComponent).Msg("CoordinationKernel.start() requires setup()")
		return
	}
	if !k.eventBus.IsRunning() {
		k.eventBus.Start(ctx, func(ctx context.Context, event types.CoordinationEvent) {
			k.dispatcher.Dispatch(ctx, event)
		})
	}

	// 步骤 9: Subscribe transport
	_ = k.host.SubscribeTransport(ctx)

	// 步骤 10: TeamCompletion rearmed
	if k.dispatcher != nil && k.dispatcher.TeamCompletion != nil {
		k.dispatcher.TeamCompletion.Rearm()
	}

	k.lifecycleState = kernelStateRunning
}

// Pause 暂停协调子系统（幂等）。
// 仅从 running 态有效转换；paused/stopped/idle 短路返回。
// 对齐 Python: CoordinationKernel.pause()
func (k *CoordinationKernel) Pause(ctx context.Context) {
	if k.lifecycleState != kernelStateRunning {
		return
	}
	memberName := k.host.MemberName()
	if memberName == "" {
		memberName = "?"
	}
	logger.Info(logComponent).Str("member_name", memberName).
		Str("lifecycle", k.host.Lifecycle()).Msg("coordination pausing (persistent)")

	// 步骤 1: Drain agent task
	k.host.DrainAgentTask(ctx)

	// 步骤 2: Persist allocator state
	k.host.PersistAllocatorState()

	// 步骤 3: [Leader] 标记活跃成员
	if k.host.Role() == schema.TeamRoleLeader {
		_ = k.host.MarkLiveTeammates(ctx, string(schema.MemberStatusPaused))
	}

	// 步骤 4: [Leader] 取消恢复任务
	// TODO(#9.55): k.host.CancelRecoveryTasks()

	// 步骤 5: 持久化生命周期状态 "paused"
	// TODO(#9.55): persistLifecycleState("paused")

	// 步骤 6: [Leader] 发布 TEAM_STANDBY
	// TODO(#9.55): publishTeamStandby()

	// 步骤 7: Unsubscribe transport
	_ = k.host.UnsubscribeTransport()

	// 步骤 8: 停止事件总线
	if k.eventBus != nil {
		k.eventBus.Stop()
	}

	// 步骤 9: Close stream
	k.host.CloseStream()

	// 步骤 10: Release session
	sessCtrl := k.host.SessionController()
	if sessCtrl != nil {
		_ = sessCtrl.ReleaseSession(ctx)
	}

	k.lifecycleState = kernelStatePaused
}

// Stop 停止协调子系统（幂等，终态）。
// idle/stopped 为 no-op；paused → stop 和 running → stop 均有效。
// 对齐 Python: CoordinationKernel.stop()
func (k *CoordinationKernel) Stop(ctx context.Context) {
	if k.lifecycleState == kernelStateIdle || k.lifecycleState == kernelStateStopped {
		return
	}
	memberName := k.host.MemberName()
	if memberName == "" {
		memberName = "?"
	}
	logger.Info(logComponent).Str("member_name", memberName).Msg("coordination stopping")

	// 步骤 1: Drain agent task
	k.host.DrainAgentTask(ctx)

	// 步骤 2: Persist allocator state
	k.host.PersistAllocatorState()

	// 步骤 3: [Leader] 标记活跃成员
	if k.host.Role() == schema.TeamRoleLeader {
		_ = k.host.MarkLiveTeammates(ctx, string(schema.MemberStatusStopped))
	}

	// 步骤 4: Unsubscribe transport
	_ = k.host.UnsubscribeTransport()

	// 步骤 5: Shutdown all handles
	// TODO(#9.55): k.host.ShutdownAllHandles()

	// 步骤 6: Memory close
	memMgr := k.host.MemoryManager()
	if memMgr != nil {
		_ = memMgr.Close(ctx)
	}

	// 步骤 7: 停止事件总线
	if k.eventBus != nil {
		k.eventBus.Stop()
	}

	// 步骤 8: Close stream
	k.host.CloseStream()

	// 步骤 9: Release session
	sessCtrl := k.host.SessionController()
	if sessCtrl != nil {
		_ = sessCtrl.ReleaseSession(ctx)
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

// EnqueueMailboxAfterFirstIteration 等待首轮迭代门控后投递 POLL_MAILBOX 事件。
// 对齐 Python: CoordinationKernel.enqueue_mailbox_after_first_iteration (kernel.py:391-401)
//
// 仅 Teammate 使用（Leader 直接 return）。确保 Teammate 在首次迭代完成前
// 不做邮箱 sweep，避免启动竞争。
func (k *CoordinationKernel) EnqueueMailboxAfterFirstIteration(ctx context.Context) {
	// Python: if host.role == TeamRole.LEADER: return
	if k.host.Role() == schema.TeamRoleLeader {
		return
	}
	// Python: gate = host.resources.first_iter_gate
	gate := k.host.FirstIterGate()
	if gate == nil || k.eventBus == nil {
		return
	}
	// Python: await gate.wait()
	if err := gate.Wait(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Msg("enqueueMailboxAfterFirstIteration: gate wait 被取消")
		return
	}
	// Python: await self._event_bus.enqueue(InnerEventMessage(event_type=InnerEventType.POLL_MAILBOX))
	k.eventBus.Enqueue(types.CoordinationEvent{
		Inner: &types.InnerEventMessage{
			EventType: types.InnerEventTypePollMailbox,
		},
	})
}

// FinalizeRound 执行轮次结束清理：记忆提取 + 释放 streamQueue。
// 对齐 Python: CoordinationKernel.finalize_round (kernel.py:421-434)
//
// 纯 round-end hook，仅做两件事：
// 1. memory_manager.extract_after_round()
// 2. stream_controller.stream_queue = None
// 不做 pause/stop/lifecycle 决策（由 Runner 层负责）。
func (k *CoordinationKernel) FinalizeRound(ctx context.Context) {
	// Python: memory_manager = host.resources.memory_manager
	memMgr := k.host.MemoryManager()
	if memMgr != nil {
		if err := memMgr.ExtractAfterRound(ctx); err != nil {
			logger.Error(logComponent).Err(err).Msg("finalizeRound: extract_after_round 失败")
		}
	}
	// Python: host.stream_controller.stream_queue = None
	sc := k.host.StreamController()
	if sc != nil {
		sc.ResetStreamQueue()
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// allMembersShutdown 检查所有成员是否都是 SHUTDOWN 状态。
func allMembersShutdown(members []*database.TeamMember) bool {
	for _, m := range members {
		if m.Status != string(schema.MemberStatusShutdown) {
			return false
		}
	}
	return true
}

// extractWorkspaceRemoteURL 从 spec 中提取工作空间远程 URL。
func extractWorkspaceRemoteURL(spec any) string {
	// 尝试通过反射获取 Workspace.RemoteURL 字段
	type specWithWorkspace interface {
		GetWorkspaceRemoteURL() string
	}
	if s, ok := spec.(specWithWorkspace); ok {
		return s.GetWorkspaceRemoteURL()
	}
	return ""
}
