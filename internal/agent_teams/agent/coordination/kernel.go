package coordination

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/metadata"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CoordinationKernel 协调子系统门面，拥有 EventBus + EventDispatcher 的完整生命周期。
// 持有宿主 TeamAgent 的反向引用，因为协调需要跨多个协作者
// （stream_controller, session_manager, spawn_manager 等）。
// 将协调逻辑集中在此处，保持 TeamAgent 专注于 BaseAgent 公共契约。
// 内部事件总线和分发器在此构造和拥有；
// 调用者通过此 kernel 操作，不直接接触 bus。
// 生命周期状态机：idle → running(start) → paused(pause) → stopped(stop)。
// stopped 是终态；后续 pause/stop 调用变为 no-op。
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
// Python: CoordinationKernel.setup(*, role)
//
// M-02: blueprint/infra 从 host 获取（对齐 Python self._host.blueprint/infra），
// 不再由调用方显式传入——避免调用方传的依赖与 host 内部状态成为两份来源。
func (k *CoordinationKernel) Setup(role schema.TeamRole, opts ...KernelOption) {
	// M-01: 对齐 Python kernel.py:55-69
	// Python 从 host 获取 blueprint/infra，当 blueprint 或 infra 为 None 时抛 RuntimeError。
	// Go 侧当 bp/inf 为 nil 时记录错误并返回，防止后续 nil pointer panic。
	// 注意：签名不变（返回 void），调用方需检查 Setup 后 EventBus/Dispatcher 是否为 nil。
	bp := k.host.DispatcherBlueprint()
	inf := k.host.DispatcherInfra()
	if bp == nil || inf == nil {
		logger.Error(logComponent).
			Bool("blueprint_nil", bp == nil).
			Bool("infra_nil", inf == nil).
			Msg("Setup received nil blueprint/infra, aligned with Python RuntimeError, cannot continue")
		return
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
		Msg("CoordinationKernel setup completed")
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

// AddSubscribedTopic 追加已订阅的传输主题。
func (k *CoordinationKernel) AddSubscribedTopic(topicID string) {
	k.subscribedTopics = append(k.subscribedTopics, topicID)
}

// ClearSubscribedTopics 清空已订阅的传输主题列表。
func (k *CoordinationKernel) ClearSubscribedTopics() {
	k.subscribedTopics = nil
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
//
// session 接收 any（对齐 Python 联合类型 str | AgentTeamSession）：harness.start
// 需要 AgentTeamSession 对象来建子会话（继承团队会话 ID），故这里不能像旧实现
// 那样只传 sessionID（会把 TeamSession 存成 nil，导致 harness.Start 拿不到团队会话）。
func (k *CoordinationKernel) Start(ctx context.Context, session any) {
	if k.eventBus == nil {
		return
	}
	memberName := k.host.MemberName()
	if memberName == "" {
		memberName = "?"
	}
	logger.Info(logComponent).Str("member_name", memberName).Msg("coordination starting")

	// 步骤 1: setMemberId
	ctx = k.host.SetMemberID(ctx, memberName)

	// 步骤 2: DB 初始化
	backendAccessor := k.host.TeamBackendAccessor()
	if backendAccessor != nil && backendAccessor.DB() != nil {
		_ = backendAccessor.DB().Initialize(ctx)
	}

	// 步骤 3: Session bind/release
	// M-19: 对齐 Python kernel.py——bind_session 返回的 ctx 必须用于后续步骤传播
	// （Python 端 set_session_id 写 contextvar 后天然全局可见；Go 端 ctx 是值传递，
	// 丢弃 newCtx 会导致步骤 4-10 拿不到 SessionState，DB 表/恢复/记忆注入全部失联）。
	sessCtrl := k.host.SessionController()
	if session != nil && sessCtrl != nil {
		newCtx, err := sessCtrl.BindSession(ctx, session)
		if err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("member_name", memberName).
				Msg("kernel start: bind session failed")
		} else {
			ctx = newCtx
			// 同步把 session_id 告知记忆管理器：pause/stop 阶段的记忆提取要用它做
			// 日志 trace 与消息隔离，而那时 session 可能已 release。
			// 对齐 Python kernel.py:130-132。
			if memMgr := k.host.MemoryManager(); memMgr != nil {
				if idProvider, ok := session.(interface{ GetSessionID() string }); ok {
					memMgr.BindSessionID(idProvider.GetSessionID())
				}
			}
		}
	} else if sessCtrl != nil {
		_ = sessCtrl.ReleaseSession(ctx)
		// 解绑后清空记忆管理器持有的 session_id，避免下一次提取串用旧会话。
		// 对齐 Python kernel.py:142-144。
		if memMgr := k.host.MemoryManager(); memMgr != nil {
			memMgr.BindSessionID("")
		}
	}

	// 步骤 4: [Leader] 检查全 SHUTDOWN → clean_team 或 recover
	if k.host.Role() == schema.TeamRoleLeader && backendAccessor != nil {
		if backendAccessor.DB() != nil && backendAccessor.DB().Team() != nil {
			existing, _ := backendAccessor.DB().Team().GetTeam(ctx, backendAccessor.TeamName())
			if existing != nil {
				members, _ := backendAccessor.ListMembers(ctx)
				// S-01: 走 shouldCleanTeam 而非裸条件——ListMembers 用 make([]T, 0, n)
				// 返回非 nil 空切片，而 allMembersShutdown 对空切片恒为 true（空循环），
				// 会把"只有 leader 的团队"误判成"全 SHUTDOWN"→ clean_team 删团队并
				// DropCurSessionTables。Python kernel.py:130 `if non_leader_members and all(...)`。
				if shouldCleanTeam(members) {
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
	// S-36: 对齐 Python kernel.py L147-151 `if infra.workspace_manager and not infra.workspace_initialized`
	wsMgr := k.host.WorkspaceManager()
	if wsMgr != nil && !k.host.IsWorkspaceInitialized() {
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
			// M-30: 对齐 Python host.state.pending_user_query——
			// 记忆注入带上当前用户查询（Invoke/Stream 先设值再调 Start，时序已就绪）。
			// 错误处理保持吞错模式（对齐既有 `_ = k.host.UpdateStatus` 吞错模式），
			// 补 Error 日志保留排查线索。
			if err := harnessAccessor.InjectMemberMemory(ctx, memMgr, k.host.PendingUserQuery()); err != nil {
				logger.Error(logComponent).
					Err(err).
					Str("member_name", k.host.MemberName()).
					Msg("kernel start: inject member memory failed")
			}
		}
	}

	// 步骤 6.5: 启动成员运行时（native supervisor + 子会话），再挂 controller 的输出泵。
	// 对齐 Python kernel.py L167-168：bind_session 之后
	//   if resources.harness is not None:
	//       await resources.harness.start(team_session=session)
	//       await host.stream_controller.start()
	// 顺序有语义：controller 消费 runtime 的输出（outputs()），故 runtime 必须先启动。
	// 仅在有会话（session 非 nil）时启动——无会话的调用（如 DestroyTeam 前的解绑路径）
	// 不建子会话、不跑 supervisor。
	if session != nil && harnessAccessor != nil {
		if err := harnessAccessor.Start(ctx, session); err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("member_name", memberName).
				Msg("kernel start: harness.start failed")
		} else if sc := k.host.StreamController(); sc != nil {
			sc.Start(ctx)
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
	// S-35: 对齐 Python kernel.py L174-177 `if team_name and not self._subscribed_topics`
	if teamName := k.host.TeamName(); teamName != "" && len(k.subscribedTopics) == 0 {
		_ = k.host.SubscribeTransport(ctx, teamName)
	}

	// 步骤 10: TeamCompletion rearmed
	if k.dispatcher != nil && k.dispatcher.TeamCompletion != nil {
		k.dispatcher.TeamCompletion.Rearm()
	}

	k.lifecycleState = kernelStateRunning

	// 步骤 11: 续跑被生命周期暂停挂起的轮次（warm / cold）。
	// Python: await self.resume_paused_round()（kernel.py L291）——内核完全就绪后最后执行。
	k.resumePausedRound(ctx)
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

	// 步骤 1: 暂停在飞轮次（就地停车，不拆毁）。
	// Python: await self.pause_agent_round()（kernel.py L343）
	// 旧实现走 drain_agent_task → abort(immediate=True)，会丢掉被中断那一轮
	// 已完成的工作；pause 必须停在干净边界且可续跑。
	if sc := k.host.StreamController(); sc != nil {
		if err := sc.PauseAgent(ctx); err != nil {
			logger.Warn(logComponent).Err(err).Msg("pause: pause_agent_round failed")
		}
	}

	// 步骤 2: Persist allocator state
	k.host.PersistAllocatorState()

	// 步骤 3: 记忆提取。
	//
	// 时机从 finalize_round（每轮回合都跑）挪到这里（每个 run cycle 只跑一次）：
	// 此时 session 仍绑定、DB 仍可访问，提取条件齐备。
	// 对齐 Python CoordinationKernel.pause（kernel.py:206-213）。
	if memMgr := k.host.MemoryManager(); memMgr != nil {
		if err := memMgr.ExtractAfterRound(ctx); err != nil {
			logger.Warn(logComponent).Err(err).Msg("pause: extract_after_round failed")
		}
	}

	// 步骤 4-6: [Leader] 标记 PAUSED → 取消恢复任务 → 关闭句柄 →
	// 复位暂停成员的执行状态 → 持久化生命周期与冷恢复标记。
	// 顺序对齐 Python kernel.py:351-362：paused_teammates 必须在
	// MarkLiveTeammates / ShutdownAllHandles 之前抓取，否则句柄已被关闭。
	if k.host.Role() == schema.TeamRoleLeader {
		pausedTeammates := k.host.SpawnedHandleNames()
		_ = k.host.MarkLiveTeammates(ctx, string(schema.MemberStatusPaused))
		k.host.CancelRecoveryTasks()
		k.host.ShutdownAllHandles(ctx)
		// 暂停后成员运行时已停，DB 里不能还留着 running 之类的执行状态：
		// 否则列表/看板显示"还在跑"，且下次启动的 FSM 可能拒绝合法转换。
		// Python: reset_paused_member_execution_status (kernel.py:355-357)
		if backend := k.host.TeamBackendAccessor(); backend != nil && k.host.TeamName() != "" {
			if reset := backend.DB().Member().ResetPausedMemberExecutionStatus(ctx, k.host.TeamName(), pausedTeammates); reset > 0 {
				logger.Debug(logComponent).Int("reset", reset).
					Str("team_name", k.host.TeamName()).
					Msg("pause: reset paused member execution status")
			}
		}
		k.persistTeamLifecycle(ctx, "paused")
		// 记录冷恢复标记：让 pause → stop → start 等价于 pause → resume。
		// Python: self._persist_pending_resume()（kernel.py L362）
		k.persistPendingResume(ctx)
	}

	// 步骤 7: [Leader] 发布 TEAM_STANDBY 事件
	// 修复 S-06: 对齐 Python: messager.publish(TeamTopic.TEAM, TeamStandbyEvent)
	if k.host.Role() == schema.TeamRoleLeader {
		k.publishTeamStandby(ctx)
	}

	// 步骤 8: Unsubscribe transport
	_ = k.host.UnsubscribeTransport(ctx)

	// 步骤 9: 停止事件总线
	if k.eventBus != nil {
		k.eventBus.Stop()
	}

	// 步骤 10: Close stream
	k.host.CloseStream()

	// 步骤 11: Release session
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

	// 步骤 3: 记忆提取（仅当从 running 直接进入 stop）。
	//
	// 从 paused 进来时提取已在 Pause 做过、session 也已释放，再查 DB 会失败，故跳过。
	// 对齐 Python CoordinationKernel.stop（kernel.py:322-330）。
	if k.lifecycleState == kernelStateRunning {
		if memMgr := k.host.MemoryManager(); memMgr != nil {
			if err := memMgr.ExtractAfterRound(ctx); err != nil {
				logger.Warn(logComponent).Err(err).Msg("stop: extract_after_round failed")
			}
		}
	}

	// 步骤 4: [Leader] 标记活跃成员为 STOPPED（在关闭句柄前，避免任务取消与状态写入竞争）
	if k.host.Role() == schema.TeamRoleLeader {
		_ = k.host.MarkLiveTeammates(ctx, string(schema.MemberStatusStopped))
	}

	// 步骤 4: Unsubscribe transport
	_ = k.host.UnsubscribeTransport(ctx)

	// 步骤 5: 取消恢复任务（所有角色，非 Leader 专有）
	// 修复 S-07: 对齐 Python: await host.spawn_manager.cancel_recovery_tasks()
	k.host.CancelRecoveryTasks()

	// 步骤 6: 关闭所有已生成句柄（所有角色，非 Leader 专有）
	// 修复 S-07: 对齐 Python: await host.spawn_manager.shutdown_all_handles()
	k.host.ShutdownAllHandles(ctx)

	// 步骤 7: Memory close
	memMgr := k.host.MemoryManager()
	if memMgr != nil {
		_ = memMgr.Close(ctx)
	}

	// 步骤 8: 停止事件总线
	if k.eventBus != nil {
		k.eventBus.Stop()
	}

	// 步骤 9: Close stream
	k.host.CloseStream()

	// 步骤 9.5: 销毁原生运行时 + 停 messager。
	// Python: kernel.py:590-593 —— close_stream 之后、release_session 之前。
	// Dispose 是永久收尾（Stop 只做轮末拆除），不接这一步会让 supervisor/
	// forwarder 持有会话继续跑、sys_operation 不从全局 ResourceMgr 摘除。
	if harnessAcc := k.host.HarnessAccessor(); harnessAcc != nil {
		if err := harnessAcc.Dispose(ctx); err != nil {
			logger.Warn(logComponent).Err(err).Msg("stop: harness dispose failed")
		}
	}
	if infra := k.host.DispatcherInfra(); infra != nil {
		if m := infra.Messager(); m != nil {
			if err := m.Stop(ctx); err != nil {
				logger.Warn(logComponent).Err(err).Msg("stop: messager stop failed")
			}
		}
	}

	// 步骤 10: Release session
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
// S-33: 签名对齐 StreamController 回调类型 func(ctx) error，便于直接作为方法值注入
func (k *CoordinationKernel) WakeMailboxIfInterruptCleared(ctx context.Context) error {
	if k.host.Role() != schema.TeamRoleTeammate {
		return nil
	}
	if k.host.HasPendingInterrupt() {
		return nil
	}
	if k.eventBus == nil {
		return nil
	}
	k.eventBus.Enqueue(types.CoordinationEvent{
		Inner: &types.InnerEventMessage{
			EventType: types.InnerEventTypePollMailbox,
		},
	})
	return nil
}

// EnqueueMailboxAfterFirstIteration 首轮邮箱轮询。
//
// 上游 kernel.py:701-711：leader 直接 return，成员直接 enqueue POLL_MAILBOX。
// §3.4.2 决策 C7 已删除 FirstIterationGate（kernel.py:709 明确
// "old FirstIterationGate wait is gone"），故这里不再等 gate。
func (k *CoordinationKernel) EnqueueMailboxAfterFirstIteration(ctx context.Context) {
	if k.host.Role() == schema.TeamRoleLeader {
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
	_ = ctx
}

// EnqueueInitialTaskPoll 启动时的一次性任务板普查。
//
// 上游 kernel.py:714-730 / team_agent.py:975：不等 30s 周期 tick，
// 立刻投一个 POLL_TASK，让 TeamCompletionHandler 有机会在首次流里
// 判定完成（否则空团队只能等周期轮询，流永不结束）。
//
// Python: CoordinationKernel.enqueue_initial_task_poll
func (k *CoordinationKernel) EnqueueInitialTaskPoll(ctx context.Context) {
	if k.eventBus == nil {
		return
	}
	k.eventBus.Enqueue(types.CoordinationEvent{
		Inner: &types.InnerEventMessage{
			EventType: types.InnerEventTypePollTask,
		},
	})
	_ = ctx
}

// FinalizeRound 执行轮次结束清理：释放 streamQueue。
// 对齐 Python: CoordinationKernel.finalize_round (kernel.py:456-470)
//
// 纯 round-end hook，只释放本轮运行时资源。
// 记忆提取已挪到 Pause/Stop——原来放在这里会导致每个**流式回合**都提取一次，
// 而语义上应当每个 run cycle 一次（对齐上游 06def473）。
// 不做 pause/stop/lifecycle 决策（由 Runner 层负责）。
func (k *CoordinationKernel) FinalizeRound(ctx context.Context) {
	// Python: host.stream_controller.stream_queue = None
	sc := k.host.StreamController()
	if sc != nil {
		sc.ResetStreamQueue()
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// persistTeamLifecycle 将团队生命周期状态写入 session 的 per-team 命名空间。
// 对齐 Python: CoordinationKernel._persist_team_lifecycle(lifecycle) (kernel.py:278-294)
func (k *CoordinationKernel) persistTeamLifecycle(ctx context.Context, lifecycle string) {
	// TODO(#ctx): GetState/UpdateState 目前不接受 ctx，未来添加 ctx 支持后应传入
	_ = ctx
	teamName := k.host.TeamName()
	if teamName == "" {
		return
	}
	sessCtrl := k.host.SessionController()
	if sessCtrl == nil {
		return
	}
	teamSession := sessCtrl.TeamSession()
	if teamSession == nil {
		return
	}
	// 对齐 Python: merge_team_namespace(session, team_name, {"lifecycle": lifecycle})
	// T-16: 原内联匿名断言 GetState() map[string]any 与 AgentTeamSession 真实签名
	// GetState(key StateKey) (any, error) 不匹配导致恒 false（生命周期从未持久化），
	// 改用 metadata.MergeTeamNamespace 权威实现。
	// M-29: TeamSession 已是具体类型 *session.AgentTeamSession（实现 SessionFacade），无需断言。
	metadata.MergeTeamNamespace(teamSession, teamName, map[string]any{"lifecycle": lifecycle})
	logger.Debug(logComponent).Str("team_name", teamName).Str("lifecycle", lifecycle).
		Msg("persistTeamLifecycle completed")
}

// persistPendingResume 记录冷恢复标记（仅 leader 且确有暂停轮）。
// Python: _persist_pending_resume (kernel.py L458-493)
//
// 记录的 query 不会重放进续跑轮上下文（上下文来自 checkpoint），
// 它驱动的是**其后**的轮次：任务计划续跑或崩溃重试。
func (k *CoordinationKernel) persistPendingResume(_ context.Context) {
	teamName := k.host.TeamName()
	if teamName == "" {
		return
	}
	harnessAcc := k.host.HarnessAccessor()
	if harnessAcc == nil {
		return
	}
	if harnessAcc.State() != harness.HarnessStatePaused {
		// 没有挂起的轮，无需记录
		return
	}
	query, _ := harnessAcc.PausedQuery()
	sessCtrl := k.host.SessionController()
	if sessCtrl == nil {
		return
	}
	teamSession := sessCtrl.TeamSession()
	if teamSession == nil {
		return
	}
	metadata.MergePendingResume(teamSession, teamName, map[string]any{"query": query})
	logger.Debug(logComponent).Str("team_name", teamName).Msg("pending resume persisted")
}

// resumePausedRound 续跑被生命周期暂停挂起的轮次。
// Python: resume_paused_round (kernel.py L747-790)
//
// 两条路径：
//   - warm：本进程仍 PAUSED，轮还在内存里，直接续跑。
//   - cold：harness 被重建（IDLE），上下文来自会话 checkpoint，
//     标记里的 query 驱动续跑。仅 leader（标记在团队共享桶里）。
func (k *CoordinationKernel) resumePausedRound(ctx context.Context) {
	harnessAcc := k.host.HarnessAccessor()
	sc := k.host.StreamController()
	if harnessAcc == nil || sc == nil {
		return
	}
	if harnessAcc.State() == harness.HarnessStatePaused {
		logger.Info(logComponent).Str("member_name", k.host.MemberName()).
			Msg("resuming the paused round in place")
		if err := sc.ResumeAgent(ctx, nil); err != nil {
			logger.Warn(logComponent).Err(err).Msg("resume: warm resume failed")
		}
		k.clearPendingResume()
		return
	}
	if k.host.Role() != schema.TeamRoleLeader {
		return
	}
	pending, ok := k.readPendingResume()
	if !ok {
		return
	}
	logger.Info(logComponent).Str("member_name", k.host.MemberName()).
		Msg("resuming the paused round from the session checkpoint")
	query, _ := pending["query"].(string)
	if err := sc.ResumeAgent(ctx, &query); err != nil {
		logger.Warn(logComponent).Err(err).Msg("resume: cold resume failed")
		return
	}
	k.clearPendingResume()
}

// readPendingResume 读取冷恢复标记。
func (k *CoordinationKernel) readPendingResume() (map[string]any, bool) {
	teamName := k.host.TeamName()
	if teamName == "" {
		return nil, false
	}
	sessCtrl := k.host.SessionController()
	if sessCtrl == nil {
		return nil, false
	}
	teamSession := sessCtrl.TeamSession()
	if teamSession == nil {
		return nil, false
	}
	return metadata.ReadPendingResume(teamSession, teamName)
}

// clearPendingResume 清除冷恢复标记。
func (k *CoordinationKernel) clearPendingResume() {
	teamName := k.host.TeamName()
	if teamName == "" {
		return
	}
	sessCtrl := k.host.SessionController()
	if sessCtrl == nil {
		return
	}
	teamSession := sessCtrl.TeamSession()
	if teamSession == nil {
		return
	}
	metadata.ClearPendingResume(teamSession, teamName)
}

// publishTeamStandby 发布 TEAM_STANDBY 事件。
// 对齐 Python: CoordinationKernel.pause() 中内联的 messager.publish(TeamTopic.TEAM, TeamStandbyEvent) (kernel.py:201-218)
func (k *CoordinationKernel) publishTeamStandby(ctx context.Context) {
	teamName := k.host.TeamName()
	if teamName == "" {
		return
	}
	// 对齐 Python: await messager.publish(topic_id, EventMessage.from_event(TeamStandbyEvent(team_name=team_name)))
	if err := k.host.PublishTeamEvent(ctx, "team_standby", map[string]any{"team_name": teamName}); err != nil {
		logger.Error(logComponent).Err(err).Str("team_name", teamName).Msg("publishTeamStandby: publish TEAM_STANDBY failed")
	}
}

// allMembersShutdown 检查所有成员是否都是 SHUTDOWN 状态。
// 注意：空切片会返回 true（空循环），调用方必须先用 shouldCleanTeam 判空。
func allMembersShutdown(members []*database.TeamMember) bool {
	for _, m := range members {
		if m.Status != string(schema.MemberStatusShutdown) {
			return false
		}
	}
	return true
}

// shouldCleanTeam 判断启动时是否应清理团队（对齐 Python kernel.py:130 的真值语义）。
// Python: `if non_leader_members and all(m.status == SHUTDOWN for m in non_leader_members)`
// —— 空列表为假，走 recover_team；Go 的 ListMembers 返回非 nil 空切片，
// 因此必须显式判长度，否则"只有 leader 的团队"会被 clean_team 误删。
func shouldCleanTeam(members []*database.TeamMember) bool {
	return len(members) > 0 && allMembersShutdown(members)
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
