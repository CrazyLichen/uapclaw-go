package harness

import (
	"context"

	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentcore "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	runner "github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// NativeHarness 团队成员的多轮交互层，组合一台 DeepAgent。
// Python: NativeHarness(DeepAgent) (agent_teams/harness/native_harness.py)。
// 差异（spec D-3）：Python 用继承，Go 用组合——因为嵌入会与
// DeepAgent.Abort 同名冲突，且跨包拿不到私有成员。
type NativeHarness struct {
	// agent 内层 DeepAgent（组合，不是嵌入）
	agent *agentcore.DeepAgent
	// spec 构造规格（构造期据此 resolve parts 自配置）
	spec atschema.DeepAgentSpec
	// buildCtx 构建上下文（构造期解析 parts 用；之后供用量归属读取）
	buildCtx hschema.BuildContextInterface
	// prepared 是否已完成 prepare（rail 初始化 + 模板热加载）
	prepared bool
	// st 唯一可变状态容器
	st *HarnessInternalState
	// cmd 控制命令队列
	cmd chan controlCmd
	// events 生命周期事件总线
	events *eventBus
	// out 输出迭代器（对上层暴露）
	out *OutputIterator
	// queue 输出队列（forwarder 写、out 读）
	queue chan stream.Schema

	// startMu 串行化 Start/Stop
	startMu sync.Mutex
	// started 是否已启动
	started bool
	// stopping 是否正在停止
	stopping bool

	// sess 绑定的会话
	sess *session.Session
	// ownSession 会话是否由本实例创建（决定停机时是否 post_run）
	ownSession bool
	// controller 任务循环控制器
	controller *task_loop.TaskLoopController
	// coordinator 任务循环协调器
	coordinator *task_loop.LoopCoordinator

	// supervisorDone supervisor 退出信号
	supervisorDone chan struct{}
	// forwarderDone forwarder 退出信号
	forwarderDone chan struct{}
	// ready supervisor 抵达首个等待点的信号
	ready chan struct{}
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// controlQueueSize 控制队列容量
	controlQueueSize = 64
	// outputQueueSize 输出队列容量
	outputQueueSize = 256
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewNativeHarness 由已构建的 DeepAgent 构造 NativeHarness。
// 25.1a 阶段接收外部已建好的 agent（因为 rail 仍由 TeamHarnessBuild 手工挂载）；
// 25.1b 改为接收 spec 并内部自建（对齐 Python 的 spec 前向构造）。
// NewNativeHarness 从 spec 物化自身（前向构造）。
//
// 顺序不可换：先 resolve parts，再用 spec 的真实 card 配置 DeepAgent——
// 上游注释：「Resolve parts before super().__init__ so the spec's real card
// configures this DeepAgent at construction (mirrors DeepAgentSpec.build's
// DeepAgent(parts.config.card))」。
//
// 与 25.1a 的差别：那时收的是"外部已建好的 agent"（rail 仍由
// TeamHarnessBuild 手工挂载）；现在 spec 自己解析出全部 parts，
// 不再有 throwaway 模板 DeepAgent。
//
// 构造是同步的：宿主构造完即可从 DeepConfig 读到 workspace / sys_operation。
//
// Python: NativeHarness.__init__ (native_harness.py:150-232)
func NewNativeHarness(spec atschema.DeepAgentSpec, buildCtx hschema.BuildContextInterface) (*NativeHarness, error) {
	// 步骤 1：先解析 parts（对应上游 "Resolve parts before super().__init__"）
	parts, err := agentcore.ResolveDeepAgentSpecParts(&spec, buildCtx)
	if err != nil {
		return nil, fmt.Errorf("native harness: resolve parts failed: %w", err)
	}

	// 步骤 2：用 parts 物化 DeepAgent
	// Go 无继承，上游的「apply parts 到自己」在这里等价于「建实例再 apply」
	agent, err := agentcore.ApplyDeepAgentParts(context.Background(), parts)
	if err != nil {
		return nil, fmt.Errorf("native harness: apply parts failed: %w", err)
	}

	// 步骤 3：初始化内部状态
	h := &NativeHarness{
		agent:    agent,
		spec:     spec,
		buildCtx: buildCtx,
		st:       newInternalState(),
		cmd:      make(chan controlCmd, controlQueueSize),
		events:   newEventBus(),
		queue:    make(chan stream.Schema, outputQueueSize),
		startMu:  sync.Mutex{},
	}
	// 相位跟踪 + 边界快照轨；add_rail 按事件路由：
	// model/tool/react-iteration 桥到内层 ReActAgent，AFTER_TASK_ITERATION 留外层。
	// Python: self.add_rail(self._snapshot_rail)（native_harness.py L229）
	agent.AddRail(newPhaseSnapshotRail(h))

	h.out = newOutputIterator(h.queue)
	return h, nil
}

// prepare 初始化基础 rails，再应用序列化的 AgentTemplate。
//
// 幂等。Start 与 RunOnce 都先调它；同步的配置 + rail 挂载已在构造期完成。
// 基础 rails 必须先初始化再热加载模板：模板的技能要绑到已存在的
// SkillUseRail 上。此方法返回前不会有模型调用，所以成员启动时
// 一定拿到了完整模板。
//
// Python: NativeHarness._prepare (native_harness.py:281-301)
func (h *NativeHarness) prepare(ctx context.Context) error {
	if h.prepared {
		return nil
	}
	if _, err := h.agent.EnsureInitialized(ctx); err != nil {
		return fmt.Errorf("harness: 初始化失败: %w", err)
	}
	// ⤵️ 15.7-15.9：AgentTemplateSpec 真实现（此时应 model_validate + 热加载）
	if h.spec.AgentTemplateSpec != nil {
		return ErrTemplateUnsupported
	}
	h.prepared = true
	return nil
}

// State 返回当前相位。
func (h *NativeHarness) State() HarnessState { return h.st.Phase() }

// SessionID 返回绑定会话标识。
// 注意：不加锁——sess 指针的读写都发生在持有 startMu 的 Start/Stop 内，
// 这里直接读指针（Go 的指针读取是原子的），避免 Start 持锁时经 transition
// 再取锁造成自死锁。
func (h *NativeHarness) SessionID() string {
	if h.sess == nil {
		return ""
	}
	return h.sess.GetSessionID()
}

// DeepAgent 返回内层 DeepAgent（团队侧需要 rails/workspace 等能力时使用）。
func (h *NativeHarness) DeepAgent() *agentcore.DeepAgent { return h.agent }

// Outputs 返回输出迭代器。
func (h *NativeHarness) Outputs() *OutputIterator { return h.out }

// Start 初始化内核并启动 supervisor 与 forwarder。
// Python: NativeHarness.start (native_harness.py:348)
func (h *NativeHarness) Start(ctx context.Context, sess *session.Session) error {
	h.startMu.Lock()
	defer h.startMu.Unlock()

	if h.started {
		if h.sess != nil && sess != nil && h.sess.GetSessionID() != sess.GetSessionID() {
			return fmt.Errorf("harness: 交互循环已绑定会话 %s，无法改绑 %s",
				h.sess.GetSessionID(), sess.GetSessionID())
		}
		return nil
	}

	// 步骤 2：初始化基础 rails + 应用模板（模板热加载 ⤵️ 15.7-15.9）
	if err := h.prepare(ctx); err != nil {
		return err
	}

	// 步骤 3：定会话（25.1a 阶段要求显式会话；自建会话语义随 25.1b spec 驱动补齐）
	if sess == nil {
		return fmt.Errorf("harness: Start 需要显式会话")
	}
	h.sess = sess
	h.ownSession = false

	// 步骤 4：建任务循环内核
	coord, ctrl, err := h.agent.PrepareInteractionTaskLoop(ctx, sess)
	if err != nil {
		return fmt.Errorf("harness: 准备任务循环内核失败: %w", err)
	}
	h.coordinator = coord
	h.controller = ctrl
	coord.Reset()

	// 步骤 5：起两条协程
	h.supervisorDone = make(chan struct{})
	h.forwarderDone = make(chan struct{})
	h.ready = make(chan struct{}, 1)
	h.started = true
	// 注意：Start 后相位保持 IDLE（newInternalState 初始值），
	// 对齐上游 Python start 不设 RUNNING——第一条 send 走 IDLE 分支才开轮。

	go h.supervisorLoop(ctx)
	go h.forwardOutputs(ctx)

	// 步骤 6：等 supervisor 抵达首个等待点
	select {
	case <-h.ready:
	case <-ctx.Done():
		return ctx.Err()
	}
	return nil
}

// Stop 取消在飞轮次、停 supervisor、关闭输出。
// Python: NativeHarness.stop (native_harness.py:393)
func (h *NativeHarness) Stop(ctx context.Context) error {
	h.startMu.Lock()
	if !h.started || h.stopping {
		h.startMu.Unlock()
		return nil
	}
	h.stopping = true
	supervisor := h.supervisorDone
	forwarder := h.forwarderDone
	ctrl := h.controller
	sess := h.sess
	h.startMu.Unlock()

	// 步骤 1：入队停机命令（含 ack 等待；supervisor 崩溃时降级由 done 通道兜底）
	reply := make(chan error, 1)
	select {
	case h.cmd <- &cmdStop{reply: reply}:
		select {
		case <-reply:
		case <-ctx.Done():
		}
	case <-ctx.Done():
	}

	// 步骤 2：等 supervisor 退出
	if supervisor != nil {
		select {
		case <-supervisor:
		case <-ctx.Done():
		}
	}

	// 步骤 3：拆任务循环内核
	if ctrl != nil && sess != nil {
		_ = ctrl.UnbindSession(ctx, sess)
		_ = ctrl.Stop(ctx)
	}

	// 步骤 4：关流，让 forwarder 的 stream_iterator 终止并投 END
	if sess != nil {
		_ = sess.CloseStream()
	}
	if forwarder != nil {
		select {
		case <-forwarder:
		case <-ctx.Done():
		}
	}

	// 步骤 5：自建会话则 post_run（本阶段恒为注入会话，逻辑保留供 25.1b）
	if h.ownSession && sess != nil {
		_ = sess.PostRun(ctx)
	}

	// 步骤 6：工具拆除。
	// 轮末拆除本 owner 的有状态工具：native 每个运行周期都会重建
	// （TeamHarness.start），不留这一步会让每周期都把工具重新注册到陈旧 id 上，
	// 而 add_ability 的 refresh 路径会为每个残留 id 记一条告警。
	// 无状态共享工具保留，其他 agent 还在用。
	// Python: ability_manager.teardown_tools()（native_harness.py:457）
	if am := h.agent.AbilityManager(); am != nil {
		am.TeardownTools()
	}

	h.startMu.Lock()
	h.stopping = false
	h.startMu.Unlock()
	return nil
}

// Send 投递输入。
// Python: NativeHarness.send (native_harness.py:562)
// 返回消息序号（本阶段暂不暴露，接口只要求 error）。
func (h *NativeHarness) Send(ctx context.Context, content any, immediate bool) error {
	reply := make(chan error, 1)
	msg := InboxMessage{Content: content, Immediate: immediate}
	if err := h.enqueue(ctx, &cmdSend{msg: msg, reply: reply}); err != nil {
		return err
	}
	return waitAck(ctx, reply)
}

// Abort 中断当前轮。
// Python: NativeHarness.abort (native_harness.py:605)。
// 本阶段 immediate=true 只取消调度任务，不回滚上下文（spec D-2，⤵️ 25.1c 补回滚）。
func (h *NativeHarness) Abort(ctx context.Context, immediate bool) error {
	reply := make(chan error, 1)
	if err := h.enqueue(ctx, &cmdAbort{immediate: immediate, reply: reply}); err != nil {
		return err
	}
	return waitAck(ctx, reply)
}

// Pause 暂停（25.1c 前不支持）。
func (h *NativeHarness) Pause(ctx context.Context) error {
	reply := make(chan error, 1)
	if err := h.enqueue(ctx, &cmdPause{reply: reply}); err != nil {
		return err
	}
	return waitAck(ctx, reply)
}

// Resume 继续（25.1c 前不支持）。
func (h *NativeHarness) Resume(ctx context.Context, query any) error {
	reply := make(chan error, 1)
	if err := h.enqueue(ctx, &cmdResume{query: query, reply: reply}); err != nil {
		return err
	}
	return waitAck(ctx, reply)
}

// Subscribe 订阅生命周期事件。
// Python: NativeHarness.subscribe (native_harness.py:532)
func (h *NativeHarness) Subscribe(ctx context.Context, onState StateHandler, onRound RoundHandler) error {
	return h.events.subscribe(ctx, onState, onRound)
}

// RunOnce 执行一次非流式调用，绕过 supervisor。
// Python: NativeHarness.run_once (native_harness.py:301)
func (h *NativeHarness) RunOnce(ctx context.Context, content any, sess *session.Session) (map[string]any, error) {
	h.startMu.Lock()
	started := h.started
	h.startMu.Unlock()
	if started {
		return nil, fmt.Errorf("harness: supervisor 运行中不允许 run_once（请用 Send）")
	}

	// Python: await self._prepare()（run_once 第一步）
	if err := h.prepare(ctx); err != nil {
		return nil, err
	}
	// 轮末拆除本 owner 的有状态工具：停掉的 agent 不该把逐 agent 的工具实例
	// 留在进程级资源管理器里，否则下一周期重建会为每个残留 id 记一条告警。
	// 与上游一致：失败只记日志，不影响本次调用的返回。
	// Python: finally 里 self.ability_manager.teardown_tools()
	defer func() {
		if am := h.agent.AbilityManager(); am != nil {
			am.TeardownTools()
		}
	}()

	own := false
	if sess == nil {
		sess = session.CreateAgentSession("default", h.agent.Card(), nil)
		if err := sess.PreRun(ctx, map[string]any{}); err != nil {
			return nil, err
		}
		own = true
	}
	if own {
		defer func() { _ = sess.PostRun(ctx) }()
	}

	// 必须传 WithSession：DeepAgent.Invoke 走任务循环（EnableTaskLoop=true）时
	// 需要 *session.Session 会话；不传则 sess=nil，runTaskLoopInvoke 报
	// "任务循环模式需要 *session.Session 类型会话"。
	return h.agent.Invoke(ctx, map[string]any{"query": content},
		agentinterfaces.WithSession(sess))
}

// Dispose 永久收尾：停机 + 释放本成员的进程级资源。
//
// 与 Stop 的区别：Stop 是轮末拆除，native 会在同一 session 上重建，所以它的
// sys_operation（按 session 稳定）是**有意保留**供下一轮复用的。Dispose 才是销毁钩子，
// 必须把该成员的资源真正摘掉，否则退役成员/废弃会话会一直占着它们。
//
// 三件事：先停 supervisor（停掉后 ContextEngine 不再通过 sys_operation 读写），
// 再关该成员打开的记忆索引管理器，最后幂等释放 sys_operation。Stop 本身幂等，
// 因此 finalize 已停过时这里不重复动作。
//
// Python: NativeHarness.dispose (native_harness.py:469-502)。
func (h *NativeHarness) Dispose(ctx context.Context) error {
	if err := h.Stop(ctx); err != nil {
		return err
	}

	if h.agent == nil {
		return nil
	}

	// 关闭该成员在工作空间上打开的记忆索引管理器（各带文件监听 goroutine、
	// 定时器与 SQLite 连接）。只关自己的，不影响同进程其他 agent。
	agentID := ""
	if card := h.agent.Card(); card != nil {
		agentID = card.GetID()
	}
	if agentID != "" {
		var ws *workspace.Workspace
		if dc := h.agent.DeepConfig(); dc != nil {
			ws = dc.Workspace
		}
		if closed := lite.CloseMemoryManagersFor(agentID, ws); closed > 0 {
			logger.Info(logComponent).
				Str("agent_id", agentID).
				Int("closed", closed).
				Msg("dispose: closed memory manager(s)")
		}
	}

	// 幂等释放 sys_operation：只在仍注册时移除，二次 dispose 是空操作。
	if dc := h.agent.DeepConfig(); dc != nil && dc.SysOperation != nil {
		if card := dc.SysOperation.Card(); card != nil {
			id := card.GetID()
			if id != "" {
				rm := runner.GetResourceMgr()
				existing, getErr := rm.GetSysOperation([]string{id})
				if getErr != nil {
					logger.Warn(logComponent).
						Str("sys_operation_id", id).
						Err(getErr).
						Msg("dispose: query sys_operation failed")
				} else if len(existing) > 0 {
					if rmErr := rm.RemoveSysOperation([]string{id}); rmErr != nil {
						logger.Warn(logComponent).
							Str("sys_operation_id", id).
							Err(rmErr).
							Msg("dispose: remove sys_operation failed")
					}
				}
			}
		}
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// transition 切相位并发事件（仅 supervisor 或 Start 内调用）。
func (h *NativeHarness) transition(ctx context.Context, next HarnessState) {
	old := h.st.Phase()
	if old == next {
		return
	}
	h.st.setPhase(next)
	h.events.emitState(ctx, HarnessStateEvent{
		Old:       old,
		New:       next,
		SessionID: h.SessionID(),
	})
}

// supervisorLoop 控制命令唯一消费者与状态唯一写者。
// Python: NativeHarness._supervisor (native_harness.py:777)
func (h *NativeHarness) supervisorLoop(ctx context.Context) {
	defer close(h.supervisorDone)
	defer h.failRemainingCommands(ctx)

	// 对齐 Python：supervisor 一启动即 set _started_event，
	// 让 start() 能立即返回；不能等第一条命令才发。
	signalReady := func() {
		select {
		case h.ready <- struct{}{}:
		default:
		}
	}
	signalReady()

	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case c := <-h.cmd:
			if stop := h.dispatch(ctx, c); stop {
				return
			}
		}
	}
}

// forwardOutputs 把会话流泵入输出队列，结束时投 END。
// Python: NativeHarness._forward_outputs (native_harness.py:1658)
func (h *NativeHarness) forwardOutputs(ctx context.Context) {
	defer close(h.forwarderDone)
	defer func() {
		// 结束后投一次 END，让 outputs() 终止（C17）
		select {
		case h.queue <- endSentinel:
		default:
		}
	}()

	ch := h.sess.StreamIterator()
	for {
		select {
		case <-ctx.Done():
			return
		case chunk, ok := <-ch:
			if !ok {
				return
			}
			select {
			case h.queue <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}
}

// dispatch 处理一条控制命令；返回 true 表示应退出 supervisor。
func (h *NativeHarness) dispatch(ctx context.Context, c controlCmd) bool {
	switch cmd := c.(type) {
	case *cmdStop:
		h.stopping = true
		h.transition(ctx, HarnessStateTerminated)
		h.hardCancelRound(h.st.Active())
		h.st.SetActive(nil)
		ack(cmd.reply, nil)
		return true
	case *cmdSend:
		ack(cmd.reply, h.onSend(ctx, &cmd.msg))
	case *cmdAbort:
		ack(cmd.reply, h.onAbort(cmd))
	case *cmdPause:
		// 工具相位的暂停把 ack 延迟到轮末结算，此处不能提前应答。
		if err := h.onPause(cmd); err != nil {
			ack(cmd.reply, err)
		} else if h.st.Phase() != HarnessStatePausing {
			ack(cmd.reply, nil)
		}
	case *cmdResume:
		ack(cmd.reply, h.onResume(cmd))
	case *cmdRoundFinished:
		// 轮末命令无应答方
		h.settleRoundDone(ctx, cmd)
	default:
		ack(c.ack(), nil)
	}
	return false
}

// enqueue 把命令放入控制队列；未启动、已终止或上下文取消时立刻返回错误。
func (h *NativeHarness) enqueue(ctx context.Context, c controlCmd) error {
	h.startMu.Lock()
	started := h.started
	h.startMu.Unlock()
	if !started {
		return fmt.Errorf("harness: 尚未启动（须先 Start）")
	}
	if h.State() == HarnessStateTerminated {
		return fmt.Errorf("harness: 已终止，拒绝新命令")
	}
	select {
	case h.cmd <- c:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// waitAck 等待命令应答。
func waitAck(ctx context.Context, reply chan error) error {
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// onSend 按当前相位路由一条投递。
// Python: NativeHarness._on_send (native_harness.py:867)
func (h *NativeHarness) onSend(ctx context.Context, msg *InboxMessage) error {
	phase := h.st.Phase()
	switch phase {
	case HarnessStateIdle:
		active := h.startRound(ctx, msg.Content, startModeDefault, nil)
		h.transition(ctx, HarnessStateRunning)
		h.events.emitRound(ctx, HarnessRoundEvent{Kind: "started", RoundID: active.RoundID})
	case HarnessStateRunning:
		if msg.Immediate && h.st.Active() != nil {
			return h.steerActive(ctx, msg.Content)
		}
		h.enqueueInbox(*msg)
	case HarnessStatePaused:
		// InteractiveInput 自带单轮语义，不能被 steer 进续跑轮。
		if _, isInteractive := msg.Content.(*interaction.InteractiveInput); isInteractive {
			h.st.ClearPausedQuery()
			active := h.startRound(ctx, msg.Content, startModeDefault, nil)
			h.transition(ctx, HarnessStateRunning)
			h.events.emitRound(ctx, HarnessRoundEvent{Kind: "started", RoundID: active.RoundID})
			return nil
		}
		// 就地续跑 + 把新内容 steer 进去（不是拼接后重启，重启会丢掉已完成的迭代）。
		query, _ := h.st.PausedQuery()
		h.st.ClearPausedQuery()
		active := h.startRound(ctx, query, startModeResumeContinuation, nil)
		h.transition(ctx, HarnessStateRunning)
		h.events.emitRound(ctx, HarnessRoundEvent{Kind: "started", RoundID: active.RoundID})
		if err := h.steerActive(ctx, msg.Content); err != nil {
			logger.Warn(logComponent).Err(err).Msg("send while paused: steer failed")
		}
		return nil
	default:
		// TERMINATED 已由 enqueue 守卫拦截
	}
	return nil
}

// enqueueInbox 追加待驱动输入（对齐 Python pending_queue）。
func (h *NativeHarness) enqueueInbox(m InboxMessage) {
	h.st.mu.Lock()
	defer h.st.mu.Unlock()
	h.st.queue = append(h.st.queue, m)
}

// steerActive 把转向输入交给当前轮的内核 steering 队列。
// Python: NativeHarness._push_steer
func (h *NativeHarness) steerActive(_ context.Context, content any) error {
	text, ok := content.(string)
	if !ok {
		return fmt.Errorf("harness: 转向输入必须是字符串，实际 %T", content)
	}
	if h.controller == nil {
		return fmt.Errorf("harness: 无任务循环控制器")
	}
	h.controller.EnqueueSteer(text)
	return nil
}

// startRound 起一轮执行并返回活动轮次。
// Python: NativeHarness._start_round (native_harness.py L1287-1359)
//
// 快照轨通过 harness 反向引用定位本轮（harness.st.active），
// 该引用在内层循环所在的 TaskScheduler 执行协程里同样有效。
func (h *NativeHarness) startRound(
	ctx context.Context,
	query any,
	mode startMode,
	runContext *agentinterfaces.RunContext,
) *ActiveRound {
	roundID := h.st.NextRoundID()
	roundCtx, cancel := context.WithCancel(ctx)

	var pre *SafeStateSnapshot
	if h.sess != nil {
		zero := 0
		pre = captureSnapshot(h.agent, h.sess, nil, &zero)
	}
	keepsTurn := mode == startModeContinuesTurn ||
		mode == startModeFailureRetry ||
		mode == startModeResumeContinuation
	if _, isInteractive := query.(*interaction.InteractiveInput); isInteractive {
		keepsTurn = true
	}
	var store TurnStateStore
	if h.sess != nil {
		store = h.sess
	}
	turn, opened := ResolveMemberTurn(store, keepsTurn)

	active := &ActiveRound{
		RoundID:          roundID,
		TaskID:           uuid.NewString(),
		OriginalQuery:    query,
		RunContext:       runContext,
		Turn:             turn,
		TurnOpened:       opened,
		Cancel:           cancel,
		Done:             make(chan struct{}),
		PreRoundSnapshot: pre,
		FailureRetry:     mode == startModeFailureRetry,
		mode:             mode,
	}
	active.SetPhase(RoundPhaseBoundary)
	h.st.SetActive(active)

	go h.runRound(roundCtx, active)

	logger.Info(logComponent).
		Int("round_id", roundID).
		Str("query", truncateForLog(query, 120)).
		Bool("follow_up", mode == startModeFollowUp).
		Int("turn_number", turn.TurnNumber).
		Bool("turn_opened", opened).
		Msg("round started")
	return active
}

// runRound 驱动一轮并回报结果。
// Python: NativeHarness._run_round (native_harness.py L1361-1447)
//
// supervisor 直接驱动外圈轮（绕过 DeepAgent.invoke），所以 invoke 级回调
// 没有别的触发点；每个外圈轮就是一个逻辑 invoke，用 FireLifecycle 包裹。
func (h *NativeHarness) runRound(ctx context.Context, active *ActiveRound) {
	defer close(active.Done)

	h.commitOpenedTurn(ctx, active)

	inv := &agentinterfaces.InvokeInputs{
		Query:          toInvokeQuery(active.OriginalQuery),
		ConversationID: h.SessionID(),
		RunContext:     active.RunContext,
	}
	cbc := agentinterfaces.NewAgentCallbackContext(h.agent, inv, h.sess)

	var result map[string]any
	var runErr error
	lifeErr := cbc.FireLifecycle(ctx,
		agentinterfaces.CallbackBeforeInvoke,
		agentinterfaces.CallbackAfterInvoke,
		func() error {
			if h.controller == nil {
				runErr = fmt.Errorf("harness: 无任务循环控制器")
				return runErr
			}
			query := queryText(active.OriginalQuery)
			// isStreaming 必须为 true：对齐 Python submit_round 后 executor 硬编码 _streaming=True。
			// 传 false 会走 callLLMInvoke（非流式），Leader 一轮的输出（tool_call/tool_result/answer）
			// 批量到齐，首条流在成员 spawn 的 tool_result 被观测前就 final，团队时序契约
			// （第二条消息在首条流未结束时被受理）失稳。
			err := h.controller.SubmitRound(ctx, h.sess, query,
				active.mode == startModeFollowUp, true,
				agentinterfaces.RunKindNormal, active.RunContext,
				task_loop.WithTaskID(active.TaskID),
				task_loop.WithTurnNumber(active.Turn.TurnNumber),
				task_loop.WithResumeContinuation(active.mode == startModeResumeContinuation),
			)
			if err != nil {
				runErr = err
				return err
			}
			result = h.controller.WaitRoundCompletion(ctx, nil)
			// 写轮次结果到流（对齐 Python _persist_and_write_round_result → _write_round_result_to_stream）。
			// task_loop 的 result 走 controller_output(task_completion)，被 team 消费端丢弃；
			// 必须把 answer 写成 answer chunk 写流，否则 chat.final 缺失、首条流永不结束。
			// control result（error / cooperative-stop）不写流（对齐 _is_control_result）。
			if !isControlResult(result) {
				h.agent.WriteRoundResultToStream(ctx, result, h.sess)
				inv.Result = result
			}
			return nil
		})
	if lifeErr != nil && runErr == nil {
		runErr = lifeErr
	}

	// 轮末命令无应答方（对齐 Python _CmdRoundFinished 无 ack）：
	// 若在此等 supervisor 处理，会与 hardCancelRound 等 active.Done 互等。
	select {
	case h.cmd <- &cmdRoundFinished{roundID: active.RoundID, result: result, err: runErr}:
	case <-ctx.Done():
	}
}

// onPause 暂停到最近的内层迭代边界。
// Python: _on_pause (native_harness.py L982-1043)
//
// 两种策略由内层循环当前所在相位决定：
//   - LLM 相位（model_call_in_flight）：执行任务停在模型调用 await 上，
//     是唯一可安全硬取消的窗口；取消并回退到最近干净边界。
//   - 工具/边界相位：已开始的迭代必须跑完（工具副作用不可逆），
//     武装协作停止，由快照轨在下一个模型调用边界强制收尾。
//
// pause_requested 在硬取消之前武装：若模型调用在取消准备期间完成，
// after_model_call 仍会在任何工具启动前强制收尾。
func (h *NativeHarness) onPause(cmd *cmdPause) error {
	if h.st.Phase() != HarnessStateRunning {
		// PAUSING：协作暂停已在飞；PAUSED/IDLE/TERMINATED：无可暂停。
		return nil
	}
	active := h.st.Active()
	if active == nil {
		h.transition(context.Background(), HarnessStatePaused)
		return nil
	}

	// 记住本轮原始查询：resume 把它交给续跑轮当 original_query，
	// 使任务计划续跑仍能复用；InteractiveInput 单轮载荷不可重放，缓存为空。
	h.st.SetPausedQuery(queryText(active.OriginalQuery))

	// 先武装协作停止——它是正确性权威，硬取消只是及时性优化。
	active.PauseRequested.Store(true)

	if active.ModelCallInFlight.Load() {
		h.hardCancelRound(active)
		h.rollback(firstNonNilSnapshot(active.LastIterSnapshot, active.PreRoundSnapshot))
		h.resetCoordinator()
		// 被中断迭代的流式 chunk 作废：它的 AssistantMessage 从未落进上下文。
		h.emitAborted(active.RoundID, "pause")
		h.events.emitRound(context.Background(), HarnessRoundEvent{Kind: "paused", RoundID: active.RoundID})
		h.st.SetActive(nil)
		h.transition(context.Background(), HarnessStatePaused)
		return nil
	}

	// 协作：让在飞迭代跑完，ack 由轮末结算（_on_round_done）兑现。
	active.PauseAck = cmd.reply
	h.transition(context.Background(), HarnessStatePausing)
	return nil
}

// onResume 就地续跑暂停轮；query 非 nil 为冷恢复。
// Python: _on_resume (native_harness.py L1045-1072)
func (h *NativeHarness) onResume(cmd *cmdResume) error {
	phase := h.st.Phase()
	var query string
	switch {
	case phase == HarnessStatePaused:
		// warm：轮仍在内存里，用暂停时缓存的查询。
		query, _ = h.st.PausedQuery()
	case phase == HarnessStateIdle && cmd.query != nil:
		// cold：harness 被重建，上下文来自会话 checkpoint，调用方提供查询。
		query, _ = cmd.query.(string)
	default:
		return nil
	}
	h.st.ClearPausedQuery()
	active := h.startRound(context.Background(), query, startModeResumeContinuation, nil)
	h.transition(context.Background(), HarnessStateRunning)
	h.events.emitRound(context.Background(), HarnessRoundEvent{Kind: "started", RoundID: active.RoundID})
	return nil
}

// onAbort 中断当前轮；两种模式都回到 IDLE。
// Python: _on_abort (native_harness.py L928-980)
func (h *NativeHarness) onAbort(cmd *cmdAbort) error {
	phase := h.st.Phase()
	if phase == HarnessStateIdle {
		return nil
	}
	if phase == HarnessStatePaused {
		h.st.ClearPausedQuery()
		h.transition(context.Background(), HarnessStateIdle)
		return nil
	}
	active := h.st.Active()
	if active == nil {
		h.transition(context.Background(), HarnessStateIdle)
		return nil
	}

	// abort 取代仍在等边界的协作暂停（PAUSING）。
	if active.PauseRequested.Load() {
		active.PauseRequested.Store(false)
		h.resolvePauseAck(active)
	}

	if cmd.immediate {
		h.hardCancelRound(active)
		h.rollback(firstNonNilSnapshot(active.LastIterSnapshot, active.PreRoundSnapshot))
		h.resetCoordinator()
		h.emitAborted(active.RoundID, "abort")
		h.events.emitRound(context.Background(), HarnessRoundEvent{Kind: "aborted", RoundID: active.RoundID})
		h.st.SetActive(nil)
		h.transition(context.Background(), HarnessStateIdle)
		return nil
	}

	// 优雅：由快照轨在模型调用边界强制收尾，轮末看到 graceful_abort 后 IDLE。
	active.GracefulAbort.Store(true)
	if phase == HarnessStatePausing {
		h.transition(context.Background(), HarnessStateRunning)
	}
	return nil
}

// settleRoundDone 结算一轮：发事件、清活动轮、按决策优先级决定下一轮。
// Python: NativeHarness._settle_round_done (native_harness.py:1098)
// 完整对齐上游决策优先级：错误分类 → 推进 coordinator + 持久化停止条件 →
// 优雅中止 → 崩溃重试 → interrupt/abort → follow-up 队列 → 内部 follow-up →
// resume 单轮 → 剩余任务 → IDLE。
func (h *NativeHarness) settleRoundDone(ctx context.Context, cmd *cmdRoundFinished) {
	active := h.st.Active()
	if active == nil || active.RoundID != cmd.roundID {
		// 已被 abort/pause 取消取代，跳过
		return
	}

	resultError := ""
	if cmd.result != nil {
		if e, ok := cmd.result["error"].(string); ok {
			resultError = e
		}
	}
	taskFailed := resultError != "" && resultError != "cancelled" && resultError != "no active round"
	runtimeCrashed := cmd.err != nil
	h.st.SetActive(nil)

	// 协作暂停 ⤵️ 25.1c（本阶段无 pause_requested，跳过）

	if cmd.err != nil {
		h.events.emitRound(ctx, HarnessRoundEvent{Kind: "failed", RoundID: cmd.roundID, Result: cmd.result})
	} else if taskFailed {
		h.events.emitRound(ctx, HarnessRoundEvent{Kind: "failed", RoundID: cmd.roundID, Result: cmd.result})
	} else {
		h.events.emitRound(ctx, HarnessRoundEvent{Kind: "finished", RoundID: cmd.roundID, Result: cmd.result})
	}

	if h.sess == nil || h.coordinator == nil {
		h.transition(ctx, HarnessStateIdle)
		return
	}

	// 推进 coordinator + 持久化停止条件（对齐 _run_task_loop 每轮尾部）
	h.coordinator.IncrementIteration()
	if cmd.result != nil {
		h.coordinator.SetLastResult(cmd.result)
	}
	st := h.agent.LoadState(h.sess)
	exported := h.coordinator.ExportState()
	st.StopConditionState = map[string]any{
		"iteration":        exported.Iteration,
		"token_usage":      exported.TokenUsage,
		"stop_reason":      exported.StopReason,
		"evaluator_states": exported.EvaluatorStates,
	}
	h.agent.SaveState(h.sess, st)

	// 崩溃重试：轮次异常死亡、查询未消费，重放一次；第二次放弃。
	if runtimeCrashed {
		if replayable, ok := active.OriginalQuery.(string); ok && replayable != "" {
			nxt := h.startRound(ctx, active.OriginalQuery, startModeFailureRetry, active.RunContext)
			h.events.emitRound(ctx, HarnessRoundEvent{Kind: "started", RoundID: nxt.RoundID})
			return
		}
		h.transition(ctx, HarnessStateIdle)
		return
	}

	resultType := ""
	if cmd.result != nil {
		if rt, ok := cmd.result["result_type"].(string); ok {
			resultType = rt
		}
	}
	if resultType == "interrupt" || h.coordinator.IsAborted() {
		h.transition(ctx, HarnessStateIdle)
		return
	}

	// 决策优先级：外部 follow-up 队列 > 内部 follow-up > 剩余任务 > IDLE
	if h.st.dequeueInbox() {
		content := h.st.popInboxContent()
		nxt := h.startRound(ctx, content, startModeFollowUp, nil)
		h.events.emitRound(ctx, HarnessRoundEvent{Kind: "started", RoundID: nxt.RoundID})
		return
	}
	if h.controller.HasFollowUp() || len(st.PendingFollowUps) > 0 {
		followUps := h.controller.DrainFollowUp()
		nxt := h.startRound(ctx, followUps, startModeFollowUp, nil)
		h.events.emitRound(ctx, HarnessRoundEvent{Kind: "started", RoundID: nxt.RoundID})
		return
	}

	// resume 单轮语义 ⤵️ 25.1c（本阶段无 InteractiveInput resume）

	// 剩余任务：任务计划仍有下一项则继续（原查询驱动）
	if h.hasRemainingTasks() {
		if q, ok := active.OriginalQuery.(string); ok && q != "" {
			nxt := h.startRound(ctx, active.OriginalQuery, startModeContinuesTurn, nil)
			h.events.emitRound(ctx, HarnessRoundEvent{Kind: "started", RoundID: nxt.RoundID})
			return
		}
	}

	h.transition(ctx, HarnessStateIdle)
}

// hasRemainingTasks 判断任务计划是否仍有待办（对齐 Python _has_remaining_tasks）。
func (h *NativeHarness) hasRemainingTasks() bool {
	st := h.agent.LoadState(h.sess)
	if st.TaskPlan == nil {
		return false
	}
	return st.TaskPlan.GetNextTask() != nil
}

// hardCancelRound 取消在飞轮次并等其协程退出。
// Python: _hard_cancel_round (native_harness.py L1527-1574)
//
// Go 无 scheduler.cancel_task，取消即 active.Cancel()；等 Done 不设超时
// （cmdRoundFinished 已去 ack，Done 会及时关闭）。等待是必要的：
// Stop 的 UnbindSession 会 RemoveSession，若轮协程仍在 SubmitRound，
// 会与 scheduler 并发读写 session map（race）。
func (h *NativeHarness) hardCancelRound(active *ActiveRound) {
	if active == nil {
		return
	}
	if active.Cancel != nil {
		active.Cancel()
	}
	if active.Done != nil {
		<-active.Done
	}
}

// resolvePauseAck 结算延迟的暂停应答。
// Python: _resolve_pause_ack (native_harness.py L1090-1096)
func (h *NativeHarness) resolvePauseAck(active *ActiveRound) {
	if active == nil || active.PauseAck == nil {
		return
	}
	ackCh := active.PauseAck
	active.PauseAck = nil
	ack(ackCh, nil)
}

// resetCoordinator 重置协调器，避免上一轮 cancel 的 is_aborted 污染下一轮。
// Python: _reset_coordinator (native_harness.py L1647-1656)
func (h *NativeHarness) resetCoordinator() {
	if h.coordinator != nil {
		h.coordinator.Reset()
	}
}

// emitAborted 投递“本轮已作废”标记，让消费者丢弃已收到的半轮 chunk。
// Python: _emit_round_aborted (native_harness.py L1675-1688)
//
// 非阻塞写：Python 输出队列无界，Go 有界；阻塞会让 supervisor 死锁。
func (h *NativeHarness) emitAborted(roundID int, kind string) {
	chunk := stream.OutputSchema{
		Type:    "round_aborted",
		Index:   0,
		Payload: map[string]any{"round_id": roundID, "kind": kind},
	}
	select {
	case h.queue <- chunk:
	default:
		logger.Warn(logComponent).
			Str("event_type", "HARNESS_ROUND_ABORTED_DROPPED").
			Int("round_id", roundID).
			Str("kind", kind).
			Msg("output queue full, round_aborted marker dropped")
	}
}

// rollback 回滚到快照；无快照时清空本轮段（保留历史）。
// Python: _rollback_to_snapshot (native_harness.py L1694-1734)
func (h *NativeHarness) rollback(snapshot *SafeStateSnapshot) {
	if h.sess == nil {
		return
	}
	react := h.agent.ReactAgent()
	if react == nil {
		return
	}
	ctx := context.Background()
	ce := react.ContextEngine()
	if ce == nil {
		react.ClearContextMessages(ctx, h.sess)
		return
	}
	mc := ce.GetContext(reactContextID, h.sess.GetSessionID())
	if snapshot != nil && mc != nil {
		mc.SetMessages(snapshot.ContextMessages, false)
		restored := hschema.DeepAgentState{}.FromSessionDict(snapshot.DeepAgentState)
		h.agent.SaveState(h.sess, &restored)
		logger.Info(logComponent).
			Int("iteration", snapshot.IterationIndex).
			Int("msgs", len(snapshot.ContextMessages)).
			Msg("rolled back to snapshot")
		return
	}
	react.ClearContextMessages(ctx, h.sess)
	logger.Info(logComponent).Msg("no snapshot; cleared current-round messages")
}

// firstNonNilSnapshot 返回第一个非 nil 快照。
func firstNonNilSnapshot(items ...*SafeStateSnapshot) *SafeStateSnapshot {
	for _, s := range items {
		if s != nil {
			return s
		}
	}
	return nil
}

// isControlResult 判断轮次结果是否为控制信号而非真实答案。
// Python: _is_control_result (native_harness.py L1484-1494)
// {"error": ...}（取消的 wait）与 cooperative-stop 载荷不得写流。
func isControlResult(result map[string]any) bool {
	if result == nil {
		return false
	}
	if e, ok := result["error"]; ok {
		if s, isStr := e.(string); isStr && s != "" {
			return true
		}
	}
	if rt, ok := result["result_type"].(string); ok && rt == cooperativeStopType {
		return true
	}
	return false
}

// commitOpenedTurn 在新开 turn 的轮次运行前提交会话状态。
// Python: _commit_opened_turn (native_harness.py L1449-1470)
// 提交失败只记日志：成员的工作比 turn 编号重要。
func (h *NativeHarness) commitOpenedTurn(ctx context.Context, active *ActiveRound) {
	if !active.TurnOpened || h.sess == nil {
		return
	}
	if err := h.sess.Commit(ctx); err != nil {
		logger.Debug(logComponent).Err(err).Int("round_id", active.RoundID).
			Msg("turn state commit failed")
	}
}

// toInvokeQuery 把查询转成 InvokeQuery。
func toInvokeQuery(q any) agentinterfaces.InvokeQuery {
	switch v := q.(type) {
	case string:
		return agentinterfaces.InvokeQueryString(v)
	case *interaction.InteractiveInput:
		return v
	case []string:
		return agentinterfaces.InvokeQueryString(strings.Join(v, "\n"))
	default:
		return agentinterfaces.InvokeQueryString(fmt.Sprintf("%v", v))
	}
}

// truncateForLog 截断日志字段到 n 个 rune。
func truncateForLog(v any, n int) string {
	s := fmt.Sprintf("%v", v)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// queryText 把本轮查询压平成可重放的纯文本。
// Python: _query_text (native_harness.py L120-131)
// string → 原样；[]string → "\n" 连接；*InteractiveInput → ""（单轮载荷不可重放）。
func queryText(query any) string {
	switch v := query.(type) {
	case string:
		return v
	case []string:
		return strings.Join(v, "\n")
	default:
		return ""
	}
}

// failRemainingCommands supervisor 退出时把队列中剩余命令统一回错误，
// 避免调用方永久阻塞（对齐 Python _fail_remaining_commands）。
func (h *NativeHarness) failRemainingCommands(_ context.Context) {
	for {
		select {
		case c := <-h.cmd:
			ack(c.ack(), fmt.Errorf("harness: supervisor 已退出"))
		default:
			return
		}
	}
}
