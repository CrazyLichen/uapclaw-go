package harness

import (
	"context"
	"fmt"
	"sync"
	"time"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentcore "github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
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

// Send 投递输入。
//
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
//
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
//
// Python: NativeHarness.subscribe (native_harness.py:532)
func (h *NativeHarness) Subscribe(ctx context.Context, onState StateHandler, onRound RoundHandler) error {
	return h.events.subscribe(ctx, onState, onRound)
}

// RunOnce 执行一次非流式调用，绕过 supervisor。
//
// Python: NativeHarness.run_once (native_harness.py:301)
func (h *NativeHarness) RunOnce(ctx context.Context, content any, sess *session.Session) (map[string]any, error) {
	h.startMu.Lock()
	started := h.started
	h.startMu.Unlock()
	if started {
		return nil, fmt.Errorf("harness: supervisor 运行中不允许 run_once（请用 Send）")
	}

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

	return h.agent.Invoke(ctx, map[string]any{"query": content})
}

// Dispose 永久收尾：停机。
//
// Python: NativeHarness.dispose / _close_memory_managers (native_harness.py:464/499)
func (h *NativeHarness) Dispose(ctx context.Context) error {
	return h.Stop(ctx)
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
	case *cmdSend:
		ack(cmd.reply, h.onSend(ctx, &cmd.msg))
	case *cmdAbort:
		ack(cmd.reply, h.onAbort(cmd))
	case *cmdPause:
		ack(cmd.reply, ErrPauseUnsupported)
	case *cmdResume:
		ack(cmd.reply, ErrPauseUnsupported)
	case *cmdRoundFinished:
		h.settleRoundDone(ctx, cmd)
		ack(cmd.reply, nil)
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
//
// Python: NativeHarness._on_send (native_harness.py:867)
func (h *NativeHarness) onSend(ctx context.Context, msg *InboxMessage) error {
	phase := h.st.Phase()
	switch phase {
	case HarnessStateIdle:
		active := h.startRound(ctx, msg.Content)
		h.transition(ctx, HarnessStateRunning)
		h.events.emitRound(ctx, HarnessRoundEvent{Kind: "started", RoundID: active.RoundID})
	case HarnessStateRunning:
		if msg.Immediate && h.st.Active() != nil {
			return h.steerActive(ctx, msg.Content)
		}
		h.enqueueInbox(*msg)
	default:
		// PAUSED 段 ⤵️ 25.1c；TERMINATED 已由 enqueue 守卫拦截
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
//
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
//
// Python: NativeHarness._start_round (native_harness.py:1287)
// 本阶段不含快照（pre_round_snapshot）与 turn 解析（⤵️ 25.1c / 25.1e）。
func (h *NativeHarness) startRound(ctx context.Context, query any) *ActiveRound {
	roundID := h.st.NextRoundID()
	roundCtx, cancel := context.WithCancel(ctx)
	active := &ActiveRound{
		RoundID: roundID,
		TaskID:  fmt.Sprintf("round-%d", roundID),
		Query:   query,
		Cancel:  cancel,
		Done:    make(chan struct{}),
	}
	h.st.SetActive(active)

	go h.runRound(roundCtx, active)
	return active
}

// runRound 驱动一轮并回报结果。
//
// Python: NativeHarness._run_round（提交轮次 + 等待完成）
func (h *NativeHarness) runRound(ctx context.Context, active *ActiveRound) {
	defer close(active.Done)

	query, _ := active.Query.(string)
	// isStreaming 必须为 true：对齐 Python submit_round 后 executor 硬编码 _streaming=True。
	// 传 false 会走 callLLMInvoke（非流式），Leader 一轮的输出（tool_call/tool_result/answer）
	// 批量到齐，首条流在成员 spawn 的 tool_result 被观测前就 final，团队时序契约
	// （第二条消息在首条流未结束时被受理）失稳。
	err := h.controller.SubmitRound(ctx, h.sess, query, false, true,
		agentinterfaces.RunKindNormal, nil, task_loop.WithTaskID(active.TaskID))
	var result map[string]any
	if err == nil {
		result = h.controller.WaitRoundCompletion(ctx, nil)
		// 写轮次结果到流（对齐 Python _persist_and_write_round_result → _write_round_result_to_stream）。
		// task_loop 的 result 走 controller_output(task_completion)，被 team 消费端丢弃；
		// 必须把 answer 写成 answer chunk 写流，否则 chat.final 缺失、首条流永不结束。
		// control result（error / cooperative-stop）不写流（对齐 _is_control_result）。
		if !isControlResult(result) {
			h.agent.WriteRoundResultToStream(ctx, result, h.sess)
		}
	}

	reply := make(chan error, 1)
	select {
	case h.cmd <- &cmdRoundFinished{roundID: active.RoundID, result: result, err: err, reply: reply}:
		_ = waitAck(context.WithoutCancel(ctx), reply)
	case <-ctx.Done():
	}
}

// onAbort 处理中断。
//
// Python: NativeHarness._on_abort (native_harness.py:928)
// 本阶段两种模式都不回滚（spec D-2）；差异只在是否立刻取消调度任务。
func (h *NativeHarness) onAbort(cmd *cmdAbort) error {
	active := h.st.Active()
	if active == nil {
		return nil
	}
	if cmd.immediate {
		h.abortActiveRound()
		return nil
	}
	// 优雅：请求中止，让本轮在下一迭代边界自然收尾
	if h.coordinator != nil {
		h.coordinator.RequestAbort()
	}
	return nil
}

// settleRoundDone 结算一轮：发事件、清活动轮、按决策优先级决定下一轮。
//
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
		if replayable, ok := active.Query.(string); ok && replayable != "" {
			nxt := h.startRound(ctx, active.Query)
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
		nxt := h.startRound(ctx, content)
		h.events.emitRound(ctx, HarnessRoundEvent{Kind: "started", RoundID: nxt.RoundID})
		return
	}
	if h.controller.HasFollowUp() || len(st.PendingFollowUps) > 0 {
		followUps := h.controller.DrainFollowUp()
		nxt := h.startRound(ctx, followUps)
		h.events.emitRound(ctx, HarnessRoundEvent{Kind: "started", RoundID: nxt.RoundID})
		return
	}

	// resume 单轮语义 ⤵️ 25.1c（本阶段无 InteractiveInput resume）

	// 剩余任务：任务计划仍有下一项则继续（原查询驱动）
	if h.hasRemainingTasks() {
		if q, ok := active.Query.(string); ok && q != "" {
			nxt := h.startRound(ctx, active.Query)
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

// abortActiveRound 取消当前活动轮次。
// abortActiveRound 取消当前活动轮次并等待其 goroutine 退出。
//
// Python: NativeHarness._abort_round（取消调度任务 + await 轮次任务）。
// 等待 active.Done 是必要的：Stop 的 UnbindSession 会 RemoveSession，
// 若 runRound goroutine 还在 SubmitRound 会与 scheduler 并发读写 session map（race）。
func (h *NativeHarness) abortActiveRound() {
	active := h.st.Active()
	if active == nil {
		return
	}
	if active.Cancel != nil {
		active.Cancel()
	}
	if active.Done != nil {
		// 有界等待：runRound 的 RunOneRound 在 cancel 后应尽快返回
		select {
		case <-active.Done:
		case <-time.After(5 * time.Second):
		}
	}
}

// isControlResult 判断轮次结果是否为控制信号而非真实答案。
//
// Python: NativeHarness._is_control_result（native_harness.py）——
// {"error": ...}（取消的 wait）与 cooperative-stop 载荷不得写流。
// 25.1a 阶段无 pause/graceful-abort（⤵️ 25.1c），cooperative-stop 分支不可达，
// 仅判断 error 非空。
func isControlResult(result map[string]any) bool {
	if result == nil {
		return false
	}
	if e, ok := result["error"]; ok {
		if s, isStr := e.(string); isStr && s != "" {
			return true
		}
	}
	return false
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
