package harness

import (
	"context"
	"fmt"
	"sync"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentcore "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// NativeHarness 团队成员的多轮交互层，组合一台 DeepAgent。
//
// Python: NativeHarness(DeepAgent) (agent_teams/harness/native_harness.py)。
// 差异（spec D-3）：Python 用继承，Go 用组合——因为嵌入会与
// DeepAgent.Abort 同名冲突，且跨包拿不到私有成员。
type NativeHarness struct {
	// agent 内层 DeepAgent（组合，不是嵌入）
	agent *agentcore.DeepAgent
	// spec 构造规格（25.1b 起用于自配置，本阶段仅保存）
	spec atschema.DeepAgentSpec
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
//
// 25.1a 阶段接收外部已建好的 agent（因为 rail 仍由 BuildTeamHarness 手工挂载）；
// 25.1b 改为接收 spec 并内部自建（对齐 Python 的 spec 前向构造）。
func NewNativeHarness(agent *agentcore.DeepAgent, spec atschema.DeepAgentSpec) *NativeHarness {
	h := &NativeHarness{
		agent:   agent,
		spec:    spec,
		st:      newInternalState(),
		cmd:     make(chan controlCmd, controlQueueSize),
		events:  newEventBus(),
		queue:   make(chan stream.Schema, outputQueueSize),
		startMu: sync.Mutex{},
	}
	h.out = newOutputIterator(h.queue)
	return h
}

// State 返回当前相位。
func (h *NativeHarness) State() HarnessState { return h.st.Phase() }

// SessionID 返回绑定会话标识。
//
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
//
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

	// 步骤 2：初始化 rails（模板热加载在 25.1b 随 spec 驱动一起落）
	if _, err := h.agent.EnsureInitialized(ctx); err != nil {
		return fmt.Errorf("harness: 初始化失败: %w", err)
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
	h.transition(ctx, HarnessStateRunning)

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
//
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
	// Python: ability_manager.teardown_tools()（native_harness.py:457）。
	// Go 的 AbilityManagerInterface 无 teardown_tools 等价物，属既有 parity 缺口
	// （⤵️ 见计划 25.1a 偏差登记；25.1b spec 驱动装配时一并补齐）。
	_ = h.agent.AbilityManager() // 占位保留接入点，避免误删步骤结构

	h.startMu.Lock()
	h.stopping = false
	h.startMu.Unlock()
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
//
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
//
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
		h.abortActiveRound()
		ack(cmd.reply, nil)
		return true
	case *cmdPause:
		ack(cmd.reply, ErrPauseUnsupported)
	case *cmdResume:
		ack(cmd.reply, ErrPauseUnsupported)
	default:
		ack(c.ack(), nil)
	}
	return false
}

// abortActiveRound 取消当前活动轮次。
func (h *NativeHarness) abortActiveRound() {
	if active := h.st.Active(); active != nil && active.Cancel != nil {
		active.Cancel()
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
