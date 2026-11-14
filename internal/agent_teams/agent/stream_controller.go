package agent

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	streambase "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamController 管理轮次生命周期、流式分块处理、输入投递、中断处理和重试逻辑。
// Python: StreamController (openjiuwen/agent_teams/agent/stream_controller.py)
//
// 职责：
//   - 轮次生命周期管理（startRound → executeRound → 自动续轮）
//   - 流式分块处理（harness.RunStreaming → tagChunk → streamQueue + observer 扇出）
//   - 输入投递（steer/followUp/pendingInputs 排队）
//   - 中断处理（hasPendingInterrupt / isValidInterruptResume / dequeueValidInterruptResume）
//   - 协作取消（cooperativeCancel: 两阶段关闭）
//   - 重试逻辑（错误码 181001 最多重试 10 次）
type StreamController struct {
	// mu 保护并发访问的字段（cancelRequested, pendingInputs, pendingInterruptResumes, streamingActive, chunkObservers）
	mu sync.RWMutex
	// getBlueprint 蓝图获取器（延迟获取，configure 后才有值）
	getBlueprint func() *TeamAgentBlueprint
	// state 共享可变状态
	state *TeamAgentState
	// resources 每实例运行时资源（持有 harness）
	resources *PrivateAgentResources
	// updateStatus 成员状态更新回调
	updateStatus func(ctx context.Context, status atschema.MemberStatus) error
	// updateExecution 执行状态更新回调
	updateExecution func(ctx context.Context, status atschema.ExecutionStatus) error
	// wakeMailboxCb 中断清除后唤醒邮箱回调
	// ⤵️(#9.62) CoordinationKernel 骨架已实现，实际回调待回填
	wakeMailboxCb func(ctx context.Context) error
	// requestCompletionPollCb 轮次干净结束时请求完成轮询的回调（仅 Leader 传入）
	requestCompletionPollCb func(ctx context.Context) error
	// reconcileMemberStartupCb 成员启动对账回调（⤵️ 25.2 提供实现，本阶段仅留钩子）
	reconcileMemberStartupCb func(ctx context.Context) error

	// streamQueue 流式分块队列（nil sentinel 关闭流）
	// Python: self.stream_queue: Optional[asyncio.Queue] = None
	streamQueue chan streambase.Schema
	// forwardCancel 取消当前 cycle 的输出泵（S-01）
	forwardCancel context.CancelFunc
	// forwardDone 输出泵退出信号（S-01）
	forwardDone chan struct{}
	// retryAttempt 重试计数（Python self._retry_attempt，start 时清零）
	retryAttempt int
	// swallowFailedRound 是否吞掉失败轮剩余的 chunk（Python self._swallow_failed_round）
	swallowFailedRound bool
	// chunkObservers 分块观察者列表（SpawnManager 注册，用于 Teammate chunk 转发到 Leader）
	chunkObservers []atschema.ChunkObserver
	// modelGroupLabelGetter 模型组名访问器（模型组池外返回空串）。
	// Python: stream_controller.py:165 model_group_label_getter
	modelGroupLabelGetter func() string
	// servedModelRecorder 路由记账回调（写 member DB 行，返回行是否存在）。
	// Python: stream_controller.py:166 served_model_recorder
	servedModelRecorder func(modelName string) bool
	// pendingRoute 最近一次 selected 的 llm_route 载荷（py: _pending_route，mu 保护）
	pendingRoute map[string]any
	// recordedModel 最近已记账的模型（py: _recorded_model，mu 保护）
	recordedModel string
	// servedModelWG 在途记账任务集合（asyncio task 集合的 Go 等价，防泄漏观察）
	servedModelWG sync.WaitGroup
}

// taskFailedError task_failed 错误，携带 errorCode 和 errorText。
// Python: _detect_task_failed 返回 Optional[Tuple[Optional[int], str]]
type taskFailedError struct {
	// code 错误码（0 表示无码）
	code int
	// text 错误文本
	text string
}

// StreamControllerOption 流式控制器可选配置
type StreamControllerOption func(*StreamController)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxRetryAttempts 最大重试次数
	// Python: _MAX_RETRY_ATTEMPTS = 10
	maxRetryAttempts = 10
	// cooperativeAbortTimeout 协作取消超时
	// Python: _COOPERATIVE_ABORT_TIMEOUT_SECONDS = 2.0
	cooperativeAbortTimeout = 2 * time.Second
	// retryQuery 重试时的查询内容
	// Python: _RETRY_QUERY = "刚才有异常状况，继续执行"
	retryQuery = "刚才有异常状况，继续执行"
	// taskFailedPayloadType task_failed 载荷类型
	// Python: _TASK_FAILED_PAYLOAD_TYPE = "task_failed"
	taskFailedPayloadType = "task_failed"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// retryableErrorCodes 可重试的错误码集合
	// Python: _RETRYABLE_ERROR_CODES = {181001}
	retryableErrorCodes = map[int]bool{181001: true}
	// errorCodePattern 错误码正则
	// Python: _ERROR_CODE_PATTERN = re.compile(r"^\[(\d+)\]")
	errorCodePattern = regexp.MustCompile(`^\[(\d+)\]`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithWakeMailbox 设置中断清除后的邮箱唤醒回调
func WithWakeMailbox(cb func(ctx context.Context) error) StreamControllerOption {
	return func(sc *StreamController) { sc.wakeMailboxCb = cb }
}

// WithRequestCompletionPoll 设置轮次干净结束后的完成轮询回调
func WithRequestCompletionPoll(cb func(ctx context.Context) error) StreamControllerOption {
	return func(sc *StreamController) { sc.requestCompletionPollCb = cb }
}

// WithModelGroupLabelGetter 设置模型组名访问器。
// Python: StreamController.__init__ 的 model_group_label_getter 参数（py:165）
func WithModelGroupLabelGetter(getter func() string) StreamControllerOption {
	return func(sc *StreamController) { sc.modelGroupLabelGetter = getter }
}

// WithServedModelRecorder 设置路由记账回调。
// Python: StreamController.__init__ 的 served_model_recorder 参数（py:166）
func WithServedModelRecorder(recorder func(modelName string) bool) StreamControllerOption {
	return func(sc *StreamController) { sc.servedModelRecorder = recorder }
}

// NewStreamController 创建新的流式控制器。
// Python: StreamController.__init__(blueprint_getter, state, resources, status_updater,
//
// Python 参数: execution_updater, wake_mailbox_callback, request_completion_poll_callback)
func NewStreamController(
	getBlueprint func() *TeamAgentBlueprint,
	state *TeamAgentState,
	resources *PrivateAgentResources,
	updateStatus func(ctx context.Context, status atschema.MemberStatus) error,
	updateExecution func(ctx context.Context, status atschema.ExecutionStatus) error,
	opts ...StreamControllerOption,
) *StreamController {
	sc := &StreamController{
		getBlueprint:    getBlueprint,
		state:           state,
		resources:       resources,
		updateStatus:    updateStatus,
		updateExecution: updateExecution,
	}
	for _, opt := range opts {
		opt(sc)
	}
	return sc
}

// AddChunkObserver 注册分块观察者。
// Python: StreamController.add_chunk_observer(cb)
// 观察者在分块标注来源成员并写入 streamQueue 之后触发。
func (sc *StreamController) AddChunkObserver(cb atschema.ChunkObserver) {
	sc.mu.Lock()
	sc.chunkObservers = append(sc.chunkObservers, cb)
	sc.mu.Unlock()
}

// RemoveChunkObserver 移除分块观察者（幂等）。
// Python: StreamController.remove_chunk_observer(cb)
func (sc *StreamController) RemoveChunkObserver(cb atschema.ChunkObserver) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for i, ob := range sc.chunkObservers {
		// 比较函数指针地址（对齐 Python list.remove 按引用比较）
		if reflect.ValueOf(ob).Pointer() == reflect.ValueOf(cb).Pointer() {
			sc.chunkObservers = append(sc.chunkObservers[:i], sc.chunkObservers[i+1:]...)
			return
		}
	}
	// 幂等：找不到也无声返回（对齐 Python contextlib.suppress(ValueError)）
}

// IsAgentRunning Agent 是否正在运行（有活动轮）。
// Python: StreamController.is_agent_running() → harness.state is HarnessState.RUNNING
func (sc *StreamController) IsAgentRunning() bool {
	h := sc.resources.Harness
	if h == nil {
		return false
	}
	return h.State() == harnessprotocol.HarnessStateRunning
}

// HasInFlightRound 是否有飞行中的轮次。
// Python: StreamController.has_in_flight_round() → is_agent_running()
func (sc *StreamController) HasInFlightRound() bool {
	return sc.IsAgentRunning()
}

// HasPendingInterrupt 是否有待处理的中断。
// Python: StreamController.has_pending_interrupt()
func (sc *StreamController) HasPendingInterrupt() bool {
	harness := sc.resources.Harness
	if harness == nil {
		return false
	}
	return harness.HasPendingInterrupt()
}

// IsValidInterruptResume 验证用户输入是否为有效中断恢复。
// Python: StreamController.is_valid_interrupt_resume(user_input)
func (sc *StreamController) IsValidInterruptResume(userInput *interaction.InteractiveInput) bool {
	harness := sc.resources.Harness
	if harness == nil {
		return false
	}
	return harness.IsPendingInterruptResumeValid(userInput)
}

// CancelAgent 取消飞行中的轮次。
// Python: StreamController.cancel_agent() → runtime.abort(immediate=True)
func (sc *StreamController) CancelAgent(ctx context.Context) error {
	if !sc.HasInFlightRound() {
		return nil
	}
	_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelRequested)
	_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelling)
	h := sc.resources.Harness
	if h == nil {
		return nil
	}
	return h.Abort(ctx, true)
}

// CloseStream 关闭流（向 streamQueue 写入 nil sentinel）。
// Python: StreamController.close_stream()
func (sc *StreamController) CloseStream() {
	q := sc.streamQueueSnapshot()
	if q != nil {
		// 非阻塞写入 nil sentinel
		select {
		case q <- nil:
		default:
		}
	}
}

// EmitCompletionAndClose 发出 team.completed 标记块再关闭流。
// Python: StreamController.emit_completion_and_close(member_count, task_count)
func (sc *StreamController) EmitCompletionAndClose(memberCount, taskCount int) {
	q := sc.streamQueueSnapshot()
	if q == nil {
		return
	}
	bp := sc.getBlueprint()
	marker := &atschema.TeamOutputSchema{
		OutputSchema: streambase.OutputSchema{
			Type:  "message",
			Index: 0,
			Payload: map[string]any{
				"event_type":   "team.completed",
				"member_count": memberCount,
				"task_count":   taskCount,
			},
		},
	}
	if bp != nil {
		name := bp.MemberName()
		role := bp.Role()
		marker.SourceMember = name
		marker.Role = &role
	}
	// marker 先入队，然后 nil sentinel 关闭流
	select {
	case q <- marker:
	default:
	}
	sc.CloseStream()
}

// DrainAgentTask 拆卸飞行中的轮次（用于协调生命周期暂停/停止）。
// Python: StreamController.drain_agent_task() → cancel_agent()
// 队列语义已上收到 supervisor，此处只做取消。
func (sc *StreamController) DrainAgentTask(ctx context.Context) error {
	return sc.CancelAgent(ctx)
}

// PauseAgent 暂停在飞轮次到最近的内层迭代边界。
// Python: StreamController.pause_agent (stream_controller.py L711-720)
//
// 与 CancelAgent 的区别：轮次被保留——停在模型调用上的轮被中断并回退到
// 上一边界，正在跑工具的迭代则先跑完。ResumeAgent 从该边界继续。
func (sc *StreamController) PauseAgent(ctx context.Context) error {
	if sc.resources == nil || sc.resources.Harness == nil {
		return nil
	}
	return sc.resources.Harness.Pause(ctx)
}

// ResumeAgent 就地续跑暂停轮；query 非 nil 为冷恢复。
// Python: StreamController.resume_agent (stream_controller.py L722-730)
func (sc *StreamController) ResumeAgent(ctx context.Context, query *string) error {
	if sc.resources == nil || sc.resources.Harness == nil {
		return nil
	}
	if query == nil {
		return sc.resources.Harness.Resume(ctx, nil)
	}
	return sc.resources.Harness.Resume(ctx, *query)
}

// CooperativeCancel 协作取消（优雅中断）。
// Python: StreamController.cooperative_cancel() → runtime.abort(immediate=False)
func (sc *StreamController) CooperativeCancel(ctx context.Context) error {
	if !sc.HasInFlightRound() {
		return nil
	}
	h := sc.resources.Harness
	if h == nil {
		return nil
	}
	if err := h.Abort(ctx, false); err != nil {
		logger.Debug(logComponent).Str("member_name", sc.memberName()).
			Err(err).Msg("harness.Abort failed")
	}
	return nil
}

// Error 实现 error 接口
func (e *taskFailedError) Error() string {
	if e.code != 0 {
		return fmt.Sprintf("[%d] %s", e.code, e.text)
	}
	return e.text
}

// Code 返回错误码
func (e *taskFailedError) Code() int {
	return e.code
}

// Text 返回错误文本
func (e *taskFailedError) Text() string {
	return e.text
}

// ResetStreamQueue 释放 streamQueue 引用（设为 nil）。
// 满足 types.StreamControllerAccessor 接口。
// 对齐 Python: StreamController.stream_queue = None（finalize_round 中调用）
func (sc *StreamController) ResetStreamQueue() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.streamQueue = nil
}

// streamQueueSnapshot 在读锁内取当前流队列快照。
// 所有读路径（转发、关闭、观察者转发）都必须经此取值：
// finalize_round 会在另一协程把 streamQueue 置 nil。
func (sc *StreamController) streamQueueSnapshot() chan streambase.Schema {
	sc.mu.RLock()
	defer sc.mu.RUnlock()
	return sc.streamQueue
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// memberName 解析当前成员名。
// Python: StreamController._member_name()
func (sc *StreamController) memberName() string {
	bp := sc.getBlueprint()
	if bp == nil {
		return ""
	}
	name := bp.MemberName()
	if name == nil {
		return ""
	}
	return *name
}

// tagChunk 标注分块的来源成员和角色。
// Python: StreamController._tag_chunk(chunk)
//
// 四种情况：
//  1. 非 OutputSchema（TraceSchema/CustomSchema）或无 memberName → 透传
//  2. 已是 *TeamOutputSchema 且标签匹配 → 透传
//  3. 已是 *TeamOutputSchema 但标签不匹配 → 浅拷贝更新标签
//  4. 是 *OutputSchema → 升级为 *TeamOutputSchema
func (sc *StreamController) tagChunk(chunk streambase.Schema) streambase.Schema {
	bp := sc.getBlueprint()
	if bp == nil {
		return chunk
	}
	memberName := bp.MemberName()
	if memberName == nil || *memberName == "" {
		return chunk
	}

	// 情况 2+3：已是 TeamOutputSchema
	if teamChunk, ok := chunk.(*atschema.TeamOutputSchema); ok {
		if teamChunk.SourceMember != nil && *teamChunk.SourceMember == *memberName &&
			teamChunk.Role != nil && *teamChunk.Role == bp.Role() {
			return chunk // 标签匹配 → 透传
		}
		// 标签不匹配 → 浅拷贝更新（对齐 Python model_copy(update=...)）
		role := bp.Role()
		newChunk := *teamChunk
		newChunk.SourceMember = memberName
		newChunk.Role = &role
		return &newChunk
	}

	// 情况 4：是 OutputSchema → 升级为 TeamOutputSchema
	if outChunk, ok := chunk.(*streambase.OutputSchema); ok {
		role := bp.Role()
		return atschema.NewTeamOutputSchema(*outChunk, memberName, &role)
	}

	// 情况 1：TraceSchema/CustomSchema → 透传
	return chunk
}

// fanOutToObservers 扇出分块到观察者，异常时自动移除。
// Python: _chunk_observers 循环 + exception auto-detach
// Python: for ob in list(self._chunk_observers) — 先拷贝再迭代，避免迭代中修改
func (sc *StreamController) fanOutToObservers(ctx context.Context, tagged streambase.Schema) {
	// Python: list(self._chunk_observers) — 先拷贝切片，迭代中使用拷贝
	sc.mu.Lock()
	observers := make([]atschema.ChunkObserver, len(sc.chunkObservers))
	copy(observers, sc.chunkObservers)
	sc.mu.Unlock()

	for _, ob := range observers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error(logComponent).
						Str("member_name", sc.memberName()).
						Any("panic", r).
						Msg("chunk observer panic; detached")
					sc.RemoveChunkObserver(ob)
				}
			}()
			if err := ob(ctx, tagged); err != nil {
				logger.Error(logComponent).
					Str("member_name", sc.memberName()).
					Err(err).
					Msg("chunk observer error; detached")
				sc.RemoveChunkObserver(ob)
			}
		}()
	}
}

// wakeMailboxIfInterruptCleared 打断结束后唤醒邮箱，让积压的输入继续被处理。
func (sc *StreamController) wakeMailboxIfInterruptCleared(ctx context.Context) error {
	if sc.wakeMailboxCb == nil {
		return nil
	}
	return sc.wakeMailboxCb(ctx)
}

// ──────────────────────────── 订阅驱动（对齐上游 stream_controller.py）────────────────────────────

// Start 挂载输出泵与状态/轮次映射器（一个 run cycle 一次）。
// Python: StreamController.start()（stream_controller.py:305-321）
// 由 CoordinationKernel.Start 在 harness.Start 之后调用（对齐 kernel.py:168）：
// controller 消费 runtime 的输出，故 runtime 必须先启动。
func (sc *StreamController) Start(ctx context.Context) {
	h := sc.resources.Harness
	if h == nil {
		return
	}
	sc.mu.Lock()
	sc.retryAttempt = 0
	sc.swallowFailedRound = false
	fctx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	sc.forwardCancel = cancel
	sc.forwardDone = done
	sc.mu.Unlock()
	_ = h.Subscribe(ctx, sc.mapState, sc.mapRound)
	go func() {
		defer close(done)
		sc.forwardOutputs(fctx)
	}()
}

// Stop 取消输出泵并等待其退出（一个 run cycle 一次）。
// Python: StreamController.stop()（stream_controller.py:320-334）
func (sc *StreamController) Stop(ctx context.Context) {
	sc.mu.Lock()
	cancel := sc.forwardCancel
	done := sc.forwardDone
	sc.forwardCancel = nil
	sc.forwardDone = nil
	sc.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-ctx.Done():
		}
	}
}

// forwardOutputs 把 runtime.outputs() 泵入流队列 + 观察者（一个 cycle 一次）。
// Python: StreamController._forward_outputs()（stream_controller.py:336-369）
func (sc *StreamController) forwardOutputs(ctx context.Context) {
	h := sc.resources.Harness
	if h == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).Str("member_name", sc.memberName()).
				Any("panic", r).Msg("output forwarder panic")
		}
	}()

	it := h.Outputs()
	if it == nil {
		return
	}
	for {
		chunk, ok := it.Next(ctx)
		if !ok {
			return
		}
		if sc.handleRetry(ctx, chunk) {
			continue
		}
		sc.mu.Lock()
		swallow := sc.swallowFailedRound
		sc.mu.Unlock()
		if swallow {
			continue
		}
		// Python: py:350 —— chunk = await self._name_model_group_model(chunk)
		// 在 tagChunk 之前执行：模型组成员把 "*" 改名为实际路由到的模型
		chunk = sc.nameModelGroupModel(chunk)
		tagged := sc.tagChunk(chunk)
		if q := sc.streamQueueSnapshot(); q != nil {
			select {
			case q <- tagged:
			case <-ctx.Done():
				return
			}
		}
		sc.fanOutToObservers(ctx, tagged)
	}
}

// mapState 把运行时相位迁移映射到成员状态。
// Python: StreamController._map_state(new)（stream_controller.py:440-458）
// RUNNING→BUSY（清 idle_since ⤵️ 25.2）、IDLE→READY + onIdleSettled；
// PAUSING/PAUSED/TERMINATED 留给生命周期层。
func (sc *StreamController) mapState(ctx context.Context, ev harness.HarnessStateEvent) {
	switch ev.New {
	case harnessprotocol.HarnessStateRunning:
		_ = sc.updateStatus(ctx, atschema.MemberStatusBusy)
	case harnessprotocol.HarnessStateIdle:
		_ = sc.updateStatus(ctx, atschema.MemberStatusReady)
		sc.onIdleSettled(ctx)
	}
}

// mapRound 把运行时轮次事件映射到执行状态，走合法边。
// Python: StreamController._map_round(kind)（stream_controller.py:460-478）
func (sc *StreamController) mapRound(ctx context.Context, ev harness.HarnessRoundEvent) {
	switch ev.Kind {
	case "started":
		sc.mu.Lock()
		sc.swallowFailedRound = false
		sc.mu.Unlock()
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusStarting)
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusRunning)
	case "finished":
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusCompleting)
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusCompleted)
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusIdle)
	case "aborted", "paused":
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelRequested)
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelling)
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelled)
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusIdle)
	case "failed":
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusFailed)
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusIdle)
	}
}

// onIdleSettled 轮链结束（runtime IDLE）：拆机时关流，否则轮询/唤醒。
// Python: StreamController._on_idle_settled()（stream_controller.py:480-499）
// 顺序有语义：wakeMailbox → reconcileMemberStartup（钩子）→ completionPoll。
func (sc *StreamController) onIdleSettled(ctx context.Context) {
	if sc.state != nil && sc.state.TeamCleaned {
		logger.Info(logComponent).Str("member_name", sc.memberName()).
			Msg("team_cleaned set; closing stream")
		sc.CloseStream()
		return
	}
	// SHUTDOWN_REQUESTED 成员：关流（对齐上游 status()==SHUTDOWN_REQUESTED 分支）
	if sc.state != nil && sc.state.TeamMember != nil {
		if status, err := sc.state.TeamMember.Status(ctx); err == nil && status == atschema.MemberStatusShutdownRequested {
			sc.CloseStream()
			return
		}
	}
	_ = sc.wakeMailboxIfInterruptCleared(ctx)
	if sc.reconcileMemberStartupCb != nil {
		_ = sc.reconcileMemberStartupCb(ctx)
	}
	if sc.requestCompletionPollCb != nil {
		_ = sc.requestCompletionPollCb(ctx)
	}
}

// handleRetry 检测 task_failed 并驱动瞬时重试；返回 true 表示已消费该 chunk。
// Python: StreamController._handle_retry(chunk)（stream_controller.py:396-425）
func (sc *StreamController) handleRetry(ctx context.Context, chunk streambase.Schema) bool {
	detected := detectTaskFailed(chunk)
	if detected == nil {
		return false
	}
	taskErr, ok := detected.(*taskFailedError)
	if !ok {
		return false
	}
	code := taskErr.Code()
	text := taskErr.Text()
	h := sc.resources.Harness
	sc.mu.Lock()
	attempt := sc.retryAttempt
	sc.mu.Unlock()
	if h != nil && isRetryableErrorCode(code) && attempt < maxRetryAttempts {
		sc.mu.Lock()
		sc.retryAttempt++
		attempt = sc.retryAttempt
		sc.swallowFailedRound = true
		sc.mu.Unlock()
		logger.Warn(logComponent).Int("error_code", code).
			Int("attempt", attempt).Int("max_attempts", maxRetryAttempts).
			Str("error_text", text).Msg("DeepAgent round transient error")
		sc.emitRetryOutput(ctx, code, text)
		_ = h.Send(ctx, retryQuery, false)
		return true
	}
	logger.Error(logComponent).Int("error_code", code).
		Int("attempts", attempt).Str("error_text", text).
		Msg("DeepAgent round failed")
	return false
}

// emitRetryOutput 把可重试失败暴露为可见的流式模型输出。
// Python: StreamController._emit_retry_output(code, text)（stream_controller.py:371-394）
func (sc *StreamController) emitRetryOutput(ctx context.Context, code int, text string) {
	sc.mu.Lock()
	attempt := sc.retryAttempt
	sc.mu.Unlock()
	retryChunk := sc.tagChunk(&streambase.OutputSchema{
		Type:  "llm_output",
		Index: 0,
		Payload: map[string]any{
			"content":      fmt.Sprintf("\n\n[Retry %d/%d] %s\n\n", attempt, maxRetryAttempts, text),
			"retrying":     true,
			"attempt":      attempt,
			"max_attempts": maxRetryAttempts,
			"error_code":   code,
		},
	})
	if sc.streamQueue != nil {
		select {
		case sc.streamQueue <- retryChunk:
		case <-ctx.Done():
		}
	}
	sc.fanOutToObservers(ctx, retryChunk)
}

// detectTaskFailed 检测 chunk 中的 task_failed 错误。
// Python: _detect_task_failed(chunk) → Optional[Tuple[Optional[int], str]]
// 返回 nil 表示不是 task_failed，返回 error 表示检测到 task_failed。
// errorCode 和 errorText 信息携带在 error 中，供重试逻辑使用。
func detectTaskFailed(chunk streambase.Schema) error {
	outChunk, ok := chunk.(*streambase.OutputSchema)
	if !ok {
		return nil
	}
	payload := outChunk.Payload
	if payload == nil {
		return nil
	}

	// S-03 直读分支：生产端（task_loop executor 等）产出
	// *cschema.ControllerOutputPayload 结构体指针——原实现只断言
	// map[string]any，结构体 payload 恒返回 nil，task_failed 重试机制失效。
	if cop, ok := payload.(*cschema.ControllerOutputPayload); ok {
		if cop == nil || cop.Type != taskFailedPayloadType {
			return nil
		}
		var text string
		if len(cop.Data) > 0 {
			if tdf, ok := cop.Data[0].(*cschema.TextDataFrame); ok && tdf != nil {
				text = tdf.Text
			}
		}
		return newTaskFailedErrorFromText(text)
	}

	var payloadType string
	var text string
	if cop, ok := payload.(*cschema.ControllerOutputPayload); ok && cop != nil {
		payloadType = cop.Type
		if len(cop.Data) > 0 {
			if td, ok := cop.Data[0].(*cschema.TextDataFrame); ok {
				text = td.Text
			}
		}
	} else if payloadMap, ok := payload.(map[string]any); ok {
		payloadType, _ = payloadMap["type"].(string)
		data, _ := payloadMap["data"].([]any)
		if len(data) > 0 {
			if dataMap, ok := data[0].(map[string]any); ok {
				text, _ = dataMap["text"].(string)
			}
		}
	} else {
		return nil
	}
	if payloadType != taskFailedPayloadType {
		return nil
	}
	return newTaskFailedErrorFromText(text)
}

// newTaskFailedErrorFromText 从错误文本提取错误码并构造 taskFailedError。
// Python: error_code 正则提取 + logger.debug 记录。
func newTaskFailedErrorFromText(text string) error {
	match := errorCodePattern.FindStringSubmatch(text)
	var code int
	if len(match) > 1 {
		if _, err := fmt.Sscanf(match[1], "%d", &code); err != nil {
			code = 0
		}
	}
	logger.Debug(logComponent).
		Int("error_code", code).
		Str("error_text", text).
		Msg("task_failed detected")

	return &taskFailedError{code: code, text: text}
}

// isRetryableErrorCode 检查错误码是否可重试。
// Python: error_code in _RETRYABLE_ERROR_CODES
func isRetryableErrorCode(code int) bool {
	return retryableErrorCodes[code]
}

// llmRouteChunkType 路由通告 chunk 类型。
// Python: _LLM_ROUTE_CHUNK_TYPE (stream_controller.py:70)
const llmRouteChunkType = "llm_route"

// contextUsageChunkType 上下文用量快照 chunk 类型。
// Python: _CONTEXT_USAGE_CHUNK_TYPE (stream_controller.py:71)
// 取值与 usage_snapshot.EventTypeContextUsage 相同（"context.usage"），本地
// 常量避免 agent 包引入 context_engine 依赖。
const contextUsageChunkType = "context.usage"

// nameModelGroupModel 把逻辑 "*" 模型改名为模型组实际运行的模型。
// 仅在模型组池上生效。selected 的 llm_route 事件被记住（路由模型变化时
// 持久化）；紧随该次调用的 post-call context.usage 快照——同一次调用的
// 那份——用它的 model 与 route 命名。其它 "*" 快照（未选路由、或背后没有
// 模型调用）改用模型组名命名。其余 chunk 原样透传。
// Python: _name_model_group_model (stream_controller.py:233-283)
func (sc *StreamController) nameModelGroupModel(chunk streambase.Schema) streambase.Schema {
	output, ok := chunk.(*streambase.OutputSchema)
	if !ok {
		return chunk
	}
	if output.Type != llmRouteChunkType && output.Type != contextUsageChunkType {
		return chunk
	}
	payload, _ := output.Payload.(map[string]any)
	if payload == nil || sc.modelGroupLabelGetter == nil {
		return chunk
	}
	label := sc.modelGroupLabelGetter()
	if label == "" {
		return chunk
	}
	if output.Type == llmRouteChunkType {
		if status, _ := payload["status"].(string); status == "selected" {
			if modelName, _ := payload["model_name"].(string); modelName != "" {
				sc.mu.Lock()
				sc.pendingRoute = payload
				sc.mu.Unlock()
				// 名册里模型按组对外提供的名字书写（即路由提示词收到的名字），
				// 不是 provider 名。成员行写入走后台任务：用户正在读的流绝不等待它。
				name, _ := payload["routed_model"].(string)
				if name == "" {
					name = modelName
				}
				sc.recordServedModelSoon(name)
			}
		}
		return chunk
	}
	// context.usage：model 非 "*" 才继续
	if model, _ := payload["model"].(string); model != models.INTELLI_ROUTER_UNIFIED_MODEL {
		return chunk
	}
	sc.mu.Lock()
	var route map[string]any
	if _, ok := payload["phase"].(string); ok && payload["phase"] == "post_call" {
		route = sc.pendingRoute
		sc.pendingRoute = nil
	}
	sc.mu.Unlock()
	// 用组对外提供的名字（路由提示词本身收到的名字）；provider 名兜底
	name := label
	if route != nil {
		routed, _ := route["routed_model"].(string)
		if routed == "" {
			routed, _ = route["model_name"].(string)
		}
		if routed != "" {
			name = routed
		}
	}
	named := renameLogicalModel(payload, name)
	if route != nil {
		if routeID, _ := route["route_id"].(string); routeID != "" {
			if _, has := named["deployment"]; !has {
				named["deployment"] = routeID
			}
		}
	}
	output.Payload = named
	return output
}

// renameLogicalModel 拷贝 payload 并把逻辑模型名改为实际模型名。
// Python: _rename_logical_model（同文件；只改 model/model_name 键，浅拷贝防共享 map）
func renameLogicalModel(payload map[string]any, name string) map[string]any {
	copied := make(map[string]any, len(payload)+2)
	for k, v := range payload {
		copied[k] = v
	}
	copied["model"] = name
	copied["model_name"] = name
	return copied
}

// recordServedModelSoon 把模型记账移出流。
// 写目标是团队 DB 的成员行，绝不能阻塞用户正在读的分块流。任务引用被
// 保留，失败会被观察到而不是在途被回收。
// Python: _record_served_model_soon (stream_controller.py:274-281)
func (sc *StreamController) recordServedModelSoon(modelName string) {
	sc.mu.RLock()
	recorder := sc.servedModelRecorder
	last := sc.recordedModel
	sc.mu.RUnlock()
	if recorder == nil || modelName == last {
		return
	}
	sc.servedModelWG.Add(1)
	go func() {
		defer sc.servedModelWG.Done()
		// Python: _record_served_model——同模型短路已在上方判定；这里调用并
		// 仅在确认持久化成功后更新去重标记（成员行未建时下次路由再试）
		if recorder(modelName) {
			sc.mu.Lock()
			sc.recordedModel = modelName
			sc.mu.Unlock()
		} else {
			// 成员行未建（leader 首次路由早于 build_team 写行）→ 下次路由再试
			logEvt := logger.Debug(logComponent).Str("model", modelName)
			if sc.getBlueprint != nil {
				logEvt = logEvt.Str("member_name", sc.memberName())
			}
			logEvt.Msg("served-model record deferred: member row not present yet")
		}
	}()
}
