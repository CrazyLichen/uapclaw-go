package agent

import (
	"context"
	"fmt"
	"reflect"
	"regexp"
	"sync"
	"time"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	streambase "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamController 管理轮次生命周期、流式分块处理、输入投递、中断处理和重试逻辑。
// 对齐 Python: StreamController (openjiuwen/agent_teams/agent/stream_controller.py)
//
// 职责：
//   - 轮次生命周期管理（startRound → executeRound → 自动续轮）
//   - 流式分块处理（harness.RunStreaming → tagChunk → streamQueue + observer 扇出）
//   - 输入投递（steer/followUp/pendingInputs 排队）
//   - 中断处理（hasPendingInterrupt / isValidInterruptResume / dequeueValidInterruptResume）
//   - 协作取消（cooperativeCancel: 两阶段关闭）
//   - 重试逻辑（错误码 181001 最多重试 10 次）
type StreamController struct {
	// mu 保护并发访问的字段（cancelRequested, pendingInputs, pendingInterruptResumes, streamingActive）
	mu sync.Mutex
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
	// ⤵️ 待 9.62 CoordinationKernel 章节回填：实际回调从 TeamAgent 传入
	wakeMailboxCb func(ctx context.Context) error
	// requestCompletionPollCb 轮次干净结束时请求完成轮询的回调（仅 Leader 传入）
	// ⤵️ 待 9.62 CoordinationKernel 章节回填：实际回调从 TeamAgent 传入
	requestCompletionPollCb func(ctx context.Context) error

	// streamQueue 流式分块队列（nil sentinel 关闭流）
	// Python: self.stream_queue: Optional[asyncio.Queue] = None
	streamQueue chan streambase.Schema
	// cancelRound 取消当前轮次的 context cancel 函数
	cancelRound context.CancelFunc
	// roundDone 当前轮次是否完成的信号（goroutine 关闭表示完成）
	roundDone chan struct{}
	// streamingActive 是否正在流式输出
	streamingActive bool
	// cancelRequested 当前轮次是否被协作取消
	cancelRequested bool
	// pendingInterruptResumes 待处理的中断恢复输入
	// ⤵️ 待 9.55 TeamAgent 完善后回填具体类型（可能为 *interaction.InteractPayload 或 *sessioninteraction.InteractiveInput）
	// 已实现 Interaction 层（9.59b），类型定义在 interaction 包中
	pendingInterruptResumes []any
	// pendingInputs 待处理的输入队列（轮次结束后自动消费）
	pendingInputs []any
	// chunkObservers 分块观察者列表（SpawnManager 注册，用于 Teammate chunk 转发到 Leader）
	chunkObservers []atschema.ChunkObserver
}

// ──────────────────────────── 枚举 ────────────────────────────

// StreamControllerOption 流式控制器可选配置
type StreamControllerOption func(*StreamController)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxRetryAttempts 最大重试次数
	// 对齐 Python: _MAX_RETRY_ATTEMPTS = 10
	maxRetryAttempts = 10
	// cooperativeAbortTimeout 协作取消超时
	// 对齐 Python: _COOPERATIVE_ABORT_TIMEOUT_SECONDS = 2.0
	cooperativeAbortTimeout = 2 * time.Second
	// retryQuery 重试时的查询内容
	// 对齐 Python: _RETRY_QUERY = "刚才有异常状况，继续执行"
	retryQuery = "刚才有异常状况，继续执行"
	// taskFailedPayloadType task_failed 载荷类型
	// 对齐 Python: _TASK_FAILED_PAYLOAD_TYPE = "task_failed"
	taskFailedPayloadType = "task_failed"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// retryableErrorCodes 可重试的错误码集合
	// 对齐 Python: _RETRYABLE_ERROR_CODES = {181001}
	retryableErrorCodes = map[int]bool{181001: true}
	// errorCodePattern 错误码正则
	// 对齐 Python: _ERROR_CODE_PATTERN = re.compile(r"^\[(\d+)\]")
	errorCodePattern = regexp.MustCompile(`^\[(\d+)\]`)
	// scLogComponent StreamController 日志组件
	scLogComponent = logger.ComponentCommon
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

// NewStreamController 创建新的流式控制器。
// 对齐 Python: StreamController.__init__(blueprint_getter, state, resources, status_updater,
//
//	execution_updater, wake_mailbox_callback, request_completion_poll_callback)
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
// 对齐 Python: StreamController.add_chunk_observer(cb)
// 观察者在分块标注来源成员并写入 streamQueue 之后触发。
func (sc *StreamController) AddChunkObserver(cb atschema.ChunkObserver) {
	sc.chunkObservers = append(sc.chunkObservers, cb)
}

// RemoveChunkObserver 移除分块观察者（幂等）。
// 对齐 Python: StreamController.remove_chunk_observer(cb)
func (sc *StreamController) RemoveChunkObserver(cb atschema.ChunkObserver) {
	for i, ob := range sc.chunkObservers {
		// 比较函数指针地址（对齐 Python list.remove 按引用比较）
		if reflect.ValueOf(ob).Pointer() == reflect.ValueOf(cb).Pointer() {
			sc.chunkObservers = append(sc.chunkObservers[:i], sc.chunkObservers[i+1:]...)
			return
		}
	}
	// 幂等：找不到也无声返回（对齐 Python contextlib.suppress(ValueError)）
}

// IsAgentRunning Agent 是否正在运行（流式输出中）。
// 对齐 Python: StreamController.is_agent_running()
func (sc *StreamController) IsAgentRunning() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.streamingActive
}

// HasInFlightRound 是否有飞行中的轮次。
// 对齐 Python: StreamController.has_in_flight_round()
// Python: agent_task is not None and not agent_task.done()
// Go: roundDone != nil 且未关闭
func (sc *StreamController) HasInFlightRound() bool {
	if sc.roundDone == nil {
		return false
	}
	select {
	case <-sc.roundDone:
		return false
	default:
		return true
	}
}

// HasPendingInterrupt 是否有待处理的中断。
// 对齐 Python: StreamController.has_pending_interrupt()
func (sc *StreamController) HasPendingInterrupt() bool {
	harness := sc.resources.Harness
	if harness == nil {
		return false
	}
	return harness.HasPendingInterrupt()
}

// IsValidInterruptResume 验证用户输入是否为有效中断恢复。
// 对齐 Python: StreamController.is_valid_interrupt_resume(user_input)
func (sc *StreamController) IsValidInterruptResume(userInput any) bool {
	harness := sc.resources.Harness
	if harness == nil {
		return false
	}
	return harness.IsPendingInterruptResumeValid(userInput)
}

// StartRound 启动一个新轮次。
// 对齐 Python: StreamController.start_round(content)
func (sc *StreamController) StartRound(ctx context.Context, content any) error {
	harness := sc.resources.Harness
	if harness == nil || sc.streamQueue == nil {
		return nil
	}
	preview := "non-string"
	if s, ok := content.(string); ok {
		if len(s) > 120 {
			preview = s[:120]
		} else {
			preview = s
		}
	}
	logger.Info(scLogComponent).Str("member_name", sc.memberName()).
		Str("preview", preview).Msg("start_agent")

	sc.startRound(ctx, content)
	return nil
}

// Steer 运行中转向。
// 对齐 Python: StreamController.steer(content)
func (sc *StreamController) Steer(ctx context.Context, content string) error {
	harness := sc.resources.Harness
	if harness != nil {
		return harness.Steer(ctx, content)
	}
	return nil
}

// FollowUp 追加输入。
// 对齐 Python: StreamController.follow_up(content)
func (sc *StreamController) FollowUp(ctx context.Context, content string) error {
	harness := sc.resources.Harness
	if harness != nil {
		return harness.FollowUp(ctx, content)
	}
	return nil
}

// CancelAgent 取消飞行中的轮次，推进执行状态机。
// 对齐 Python: StreamController.cancel_agent()
// 状态机：RUNNING → CANCEL_REQUESTED → CANCELLING → CooperativeCancel
func (sc *StreamController) CancelAgent(ctx context.Context) error {
	if !sc.HasInFlightRound() {
		return nil
	}
	_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelRequested)
	_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelling)
	return sc.CooperativeCancel(ctx)
}

// CloseStream 关闭流（向 streamQueue 写入 nil sentinel）。
// 对齐 Python: StreamController.close_stream()
func (sc *StreamController) CloseStream() {
	if sc.streamQueue != nil {
		// 非阻塞写入 nil sentinel
		select {
		case sc.streamQueue <- nil:
		default:
		}
	}
}

// EmitCompletionAndClose 发出 team.completed 标记块再关闭流。
// 对齐 Python: StreamController.emit_completion_and_close(member_count, task_count)
func (sc *StreamController) EmitCompletionAndClose(memberCount, taskCount int) {
	if sc.streamQueue == nil {
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
	case sc.streamQueue <- marker:
	default:
	}
	sc.CloseStream()
}

// DrainAgentTask 拆卸飞行中的轮次（用于协调生命周期暂停/停止）。
// 对齐 Python: StreamController.drain_agent_task()
// 清除 pendingInputs + pendingInterruptResumes，然后 CancelAgent
func (sc *StreamController) DrainAgentTask(ctx context.Context) error {
	sc.mu.Lock()
	sc.pendingInputs = nil
	sc.pendingInterruptResumes = nil
	sc.mu.Unlock()
	return sc.CancelAgent(ctx)
}

// CooperativeCancel 协作取消：两阶段关闭。
// 对齐 Python: StreamController.cooperative_cancel()
// Phase 1: 设 cancelRequested + harness.Abort()
// Phase 2: 等 2s → 超时则 cancelRound()（强制取消 goroutine）
func (sc *StreamController) CooperativeCancel(ctx context.Context) error {
	if !sc.HasInFlightRound() {
		return nil
	}
	sc.mu.Lock()
	sc.cancelRequested = true
	sc.mu.Unlock()
	harness := sc.resources.Harness
	if harness != nil {
		if err := harness.Abort(ctx); err != nil {
			logger.Debug(scLogComponent).Str("member_name", sc.memberName()).
				Err(err).Msg("harness.Abort 失败")
		}
	}
	// 等待 goroutine 自然完成或超时
	select {
	case <-sc.roundDone:
		return nil
	case <-time.After(cooperativeAbortTimeout):
		if sc.cancelRound != nil {
			sc.cancelRound()
		}
		// 等待 goroutine 退出
		select {
		case <-sc.roundDone:
		case <-ctx.Done():
		}
		return nil
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// memberName 解析当前成员名。
// 对齐 Python: StreamController._member_name()
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
// 对齐 Python: StreamController._tag_chunk(chunk)
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
// 对齐 Python: _chunk_observers 循环 + exception auto-detach
func (sc *StreamController) fanOutToObservers(ctx context.Context, tagged streambase.Schema) {
	for _, ob := range sc.chunkObservers {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error(scLogComponent).
						Str("member_name", sc.memberName()).
						Any("panic", r).
						Msg("chunk 观察者 panic; 已分离")
					sc.RemoveChunkObserver(ob)
				}
			}()
			if err := ob(ctx, tagged); err != nil {
				logger.Error(scLogComponent).
					Str("member_name", sc.memberName()).
					Err(err).
					Msg("chunk 观察者异常; 已分离")
				sc.RemoveChunkObserver(ob)
			}
		}()
	}
}

// startRound 启动一个新轮次（内部方法，由 StartRound 和 runOneRound 的续轮逻辑调用）。
// 对齐 Python: StreamController.start_round(content) 内部启动逻辑
func (sc *StreamController) startRound(ctx context.Context, content any) {
	harness := sc.resources.Harness
	if harness == nil || sc.streamQueue == nil {
		return
	}
	roundCtx, cancel := context.WithCancel(ctx)
	sc.cancelRound = cancel
	sc.roundDone = make(chan struct{})
	go func() {
		defer close(sc.roundDone)
		defer sc.logRoundPanic()
		sc.runOneRound(roundCtx, content)
	}()
}

// logRoundPanic 记录轮次 goroutine 的 panic。
// 对齐 Python: StreamController._log_agent_task_exception(task)
func (sc *StreamController) logRoundPanic() {
	if r := recover(); r != nil {
		logger.Error(scLogComponent).Str("member_name", sc.memberName()).
			Any("panic", r).Msg("_run_one_round 协程 panic")
	}
}

// runOneRound 执行一个完整轮次。
// 对齐 Python: StreamController._run_one_round(message)
func (sc *StreamController) runOneRound(ctx context.Context, message any) {
	// 重置取消标记（对齐 Python: self._cancel_requested = False）
	sc.mu.Lock()
	sc.cancelRequested = false
	sc.mu.Unlock()
	// ⤵️ 待 9.62 CoordinationKernel 章节回填：set_member_id 上下文变量

	harness := sc.resources.Harness
	if harness != nil {
		harness.InitCwdForRound()
	}

	_ = sc.updateStatus(ctx, atschema.MemberStatusReady)
	_ = sc.updateStatus(ctx, atschema.MemberStatusBusy)

	cancelled := false
	func() {
		defer func() {
			// 对齐 Python: agent_task = None
			sc.cancelRound = nil

			// 对齐 Python: if self._state.team_cleaned: close_stream()
			if sc.state.TeamCleaned {
				logger.Info(scLogComponent).Str("member_name", sc.memberName()).
					Msg("team_cleaned set; closing stream after round")
				sc.CloseStream()
				return
			}

			// 对齐 Python: elif not cancelled and not _cancel_requested:
			sc.mu.Lock()
			shouldContinue := !cancelled && !sc.cancelRequested
			sc.mu.Unlock()
			if shouldContinue {
				nextResume := sc.dequeueValidInterruptResume()
				sc.mu.Lock()
				hasPending := len(sc.pendingInputs) > 0 && sc.streamQueue != nil
				sc.mu.Unlock()
				if nextResume != nil && sc.streamQueue != nil {
					sc.startRound(ctx, nextResume)
				} else if hasPending {
					sc.mu.Lock()
					drained := sc.pendingInputs
					sc.pendingInputs = nil
					sc.mu.Unlock()
					combined := sc.combinePendingInputs(drained)
					sc.startRound(ctx, combined)
				} else {
					_ = sc.wakeMailboxIfInterruptCleared(ctx)
					// ⤵️ 待 TeamMember 状态检查回填：检查 SHUTDOWN_REQUESTED → CloseStream()
					// 对齐 Python: if team_member and await team_member.status() == MemberStatus.SHUTDOWN_REQUESTED: close_stream()
					if sc.requestCompletionPollCb != nil {
						_ = sc.requestCompletionPollCb(ctx)
					}
				}
			}
		}()

		// 对齐 Python: await _execute_round(message)
		// 检查 context 是否已取消
		if ctx.Err() != nil {
			cancelled = true
			_ = sc.updateStatus(ctx, atschema.MemberStatusError)
			return
		}
		sc.executeRound(ctx, message)
		_ = sc.updateStatus(ctx, atschema.MemberStatusReady)
	}()
}

// executeRound 执行状态机。
// 对齐 Python: StreamController._execute_round(message)
// 状态转换：STARTING → RUNNING → (COMPLETING→COMPLETED | CANCELLED | FAILED) → IDLE
func (sc *StreamController) executeRound(ctx context.Context, message any) {
	_ = sc.updateExecution(ctx, atschema.ExecutionStatusStarting)
	_ = sc.updateExecution(ctx, atschema.ExecutionStatusRunning)

	if ctx.Err() != nil {
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelled)
		_ = sc.updateExecution(ctx, atschema.ExecutionStatusIdle)
		return
	}

	err := sc.runRetryingStream(ctx, message)

	sc.mu.Lock()
	isCancelRequested := sc.cancelRequested
	sc.mu.Unlock()

	if err != nil {
		if isCancelRequested {
			_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelled)
		} else {
			logger.Error(scLogComponent).Err(err).Str("member_name", sc.memberName()).
				Msg("DeepAgent 循环错误")
			_ = sc.updateExecution(ctx, atschema.ExecutionStatusFailed)
		}
	} else {
		if isCancelRequested {
			// 对齐 Python: if self._cancel_requested: CANCELLED
			_ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelled)
		} else {
			_ = sc.updateExecution(ctx, atschema.ExecutionStatusCompleting)
			_ = sc.updateExecution(ctx, atschema.ExecutionStatusCompleted)
		}
	}
	_ = sc.updateExecution(ctx, atschema.ExecutionStatusIdle)
}

// streamOneRound 执行单轮流式读取。
// 对齐 Python: StreamController._stream_one_round(query)
// 返回 nil 表示成功，返回 (errorCode, errorText) 表示检测到 task_failed
func (sc *StreamController) streamOneRound(ctx context.Context, query any) (errorCode *int, errorText string) {
	sc.mu.Lock()
	sc.streamingActive = true
	sc.mu.Unlock()
	defer func() {
		sc.mu.Lock()
		sc.streamingActive = false
		sc.mu.Unlock()
	}()

	harness := sc.resources.Harness
	if harness == nil {
		return nil, ""
	}

	// ⤵️ 待 9.55 TeamAgent 完善后回填 sessionID 和 teamSession
	// 已实现 Interaction 层（9.59b），sessionID 可从 SessionState 读取
	inputMap := map[string]any{"query": query}
	chunkCh, err := harness.RunStreaming(ctx, inputMap, "", nil)
	if err != nil {
		return nil, ""
	}

	// 对齐 Python: async for chunk in harness.run_streaming(...)
	errorSeen := false
	for chunk := range chunkCh {
		if chunk == nil {
			// nil sentinel，流关闭
			break
		}
		if errorSeen {
			continue
		}
		detectedCode, detectedText := detectTaskFailed(chunk)
		if detectedCode != nil || detectedText != "" {
			errorSeen = true
			errorCode = detectedCode
			errorText = detectedText
			continue
		}
		tagged := sc.tagChunk(chunk)
		if sc.streamQueue != nil {
			select {
			case sc.streamQueue <- tagged:
			case <-ctx.Done():
				return nil, ""
			}
		}
		sc.fanOutToObservers(ctx, tagged)
	}
	if !errorSeen {
		return nil, ""
	}
	return errorCode, errorText
}

// runRetryingStream 带重试的流式执行。
// 对齐 Python: StreamController._run_retrying_stream(initial_query)
func (sc *StreamController) runRetryingStream(ctx context.Context, initialQuery any) error {
	currentQuery := initialQuery
	attempt := 0
	for {
		errorCode, errorText := sc.streamOneRound(ctx, currentQuery)
		if errorCode == nil {
			return nil
		}
		code := *errorCode
		if isRetryableErrorCode(code) && attempt < maxRetryAttempts {
			attempt++
			logger.Warn(scLogComponent).Int("error_code", code).
				Int("attempt", attempt).Int("max_attempts", maxRetryAttempts).
				Str("error_text", errorText).Msg("DeepAgent 循环瞬态错误")
			currentQuery = retryQuery
			continue
		}
		logger.Error(scLogComponent).Int("error_code", code).
			Int("attempts", attempt).Str("error_text", errorText).
			Msg("DeepAgent 循环失败")
		return fmt.Errorf("streaming task failed after %d retries, last error code=%d: %s",
			attempt, code, errorText)
	}
}

// dequeueValidInterruptResume 弹出有效中断恢复。
// 对齐 Python: StreamController._dequeue_valid_interrupt_resume()
// ⤵️ 待 9.55 TeamAgent 完善后回填具体类型（interaction 包已实现）
func (sc *StreamController) dequeueValidInterruptResume() any {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	for len(sc.pendingInterruptResumes) > 0 {
		candidate := sc.pendingInterruptResumes[0]
		sc.pendingInterruptResumes = sc.pendingInterruptResumes[1:]
		if sc.IsValidInterruptResume(candidate) {
			return candidate
		}
	}
	return nil
}

// wakeMailboxIfInterruptCleared 中断清除后唤醒邮箱。
// 对齐 Python: StreamController._wake_mailbox_if_interrupt_cleared()
func (sc *StreamController) wakeMailboxIfInterruptCleared(ctx context.Context) error {
	if sc.wakeMailboxCb == nil {
		return nil
	}
	return sc.wakeMailboxCb(ctx)
}

// combinePendingInputs 合并多个待处理输入。
// 对齐 Python: "\n\n---\n\n".join(items) — 多个用分隔符合并，单个直接用
func (sc *StreamController) combinePendingInputs(items []any) any {
	if len(items) == 1 {
		return items[0]
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		if s, ok := item.(string); ok {
			parts = append(parts, s)
		} else {
			parts = append(parts, fmt.Sprintf("%v", item))
		}
	}
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += "\n\n---\n\n"
		}
		result += p
	}
	return result
}

// detectTaskFailed 检测 chunk 中的 task_failed 错误。
// 对齐 Python: _detect_task_failed(chunk)
func detectTaskFailed(chunk streambase.Schema) (errorCode *int, errorText string) {
	outChunk, ok := chunk.(*streambase.OutputSchema)
	if !ok {
		return nil, ""
	}
	payload := outChunk.Payload
	if payload == nil {
		return nil, ""
	}
	payloadMap, ok := payload.(map[string]any)
	if !ok {
		return nil, ""
	}
	payloadType, _ := payloadMap["type"].(string)
	if payloadType != taskFailedPayloadType {
		return nil, ""
	}
	data, _ := payloadMap["data"].([]any)
	var text string
	if len(data) > 0 {
		if dataMap, ok := data[0].(map[string]any); ok {
			text, _ = dataMap["text"].(string)
		}
	}
	match := errorCodePattern.FindStringSubmatch(text)
	if len(match) > 1 {
		code := 0
		if _, err := fmt.Sscanf(match[1], "%d", &code); err == nil {
			return &code, text
		}
	}
	return nil, text
}

// isRetryableErrorCode 检查错误码是否可重试。
// 对齐 Python: error_code in _RETRYABLE_ERROR_CODES
func isRetryableErrorCode(code int) bool {
	return retryableErrorCodes[code]
}
