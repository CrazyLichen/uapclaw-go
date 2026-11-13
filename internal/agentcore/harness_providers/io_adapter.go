package harness_providers

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	harnessprotocol "github.com/uapclaw/uapclaw-go/internal/agentcore/harness_protocol"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	interruptpkg "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// 本文件对应 Python openjiuwen/harness_providers/io_adapter.py（669 行）：
// DeepAgent 式门面：拥有唯一的连续 events() 消费者，把 provider 事件投影成
// OutputSchema 块，并充当宿主的 HarnessInteractionHandler，让 UserInputRequest
// 以 __interaction__ 块浮现、由后续 InteractiveInput 应答。
//
// 三个方向的投影：
//   provider 事件 → OutputSchema 块（llm_output / llm_reasoning / tool_call /
//     tool_result / __interaction__ / turn_lifecycle）
//   InteractiveInput → 应答挂起交互或投递 provider
//   宿主 → provider 的 abort / pause / resume 能力协商
//
// ⚠️ 不实现 HarnessProtocol（spec Q17）：上游 start/send/abort 签名本来就多
// immediate / auto_approve 等 host 侧语义；只实现 HarnessInteractionHandler。
//
// ⚠️ 锁约定（偏差 D4 的通用原则）：mu 的临界区只包 map / 队列 / 计数等状态
// 读写，绝不包 harness.Start / harness.Send / hooks.* 等可能阻塞的调用。
// Python 的 asyncio.Lock 是协程级（等待只挂起协程），Go 的 sync.Mutex 是
// 线程级（持锁等待死锁）。

// ──────────────────────────── 常量 ────────────────────────────

// 投影块类型与标记（Python: io_adapter.py:74-76）。
const (
	// InteractiveInputKind InteractiveInput 的 metadata.kind 标记
	InteractiveInputKind = "interactive_input"
	// TurnLifecycleChunkType 轮次生命周期块类型
	TurnLifecycleChunkType = "turn_lifecycle"
)

// endMarker 输出流的结束哨兵专有类型。
// Python: io_adapter.py:76 `_END: Any = object()`——Go 无对象哨兵，
// 用专有类型保证不会与任何合法块混淆。
type endMarker struct{}

// ──────────────────────────── 结构体 ────────────────────────────

// EventObserver 每条原始事件在投影前调用的观察者。
// Python: io_adapter.py:72 EventObserver
type EventObserver func(ctx context.Context, event harnessprotocol.HarnessEvent) error

// ProviderInteractionHandler 宿主应答 provider 扩展请求的回调。
// Python: io_adapter.py:73 ProviderInteractionHandler
type ProviderInteractionHandler func(ctx context.Context,
	request *harnessprotocol.ProviderInteractionRequest) (*harnessprotocol.ProviderInteractionResponse, error)

// pendingInteraction 一个挂起交互的请求与其应答通道。
// Python: io_adapter.py:97-100 _PendingInteraction（request + asyncio.Future）
type pendingInteraction struct {
	// request 挂起的交互请求
	request harnessprotocol.HarnessInteractionRequest
	// done 应答就绪时关闭；关闭后 response / err 只读
	done chan struct{}
	// response 应答结果
	response harnessprotocol.HarnessInteractionResponse
	// err 应答错误
	err error
}

// awaitResponse 等待应答；ctx 取消时返回错误。
func (p *pendingInteraction) awaitResponse(ctx context.Context) (
	harnessprotocol.HarnessInteractionResponse, error) {
	select {
	case <-p.done:
		return p.response, p.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// complete 投递应答（幂等；已完成的 entry 忽略）。
func (p *pendingInteraction) complete(response harnessprotocol.HarnessInteractionResponse, err error) {
	if p.isCompleted() {
		return
	}
	p.response = response
	p.err = err
	close(p.done)
}

// isCompleted 是否已有应答。
// Python: io_adapter.py:419/427 `pending.future.done()` / `if not pending.future.done()`
func (p *pendingInteraction) isCompleted() bool {
	select {
	case <-p.done:
		return true
	default:
		return false
	}
}

// HarnessIOAdapterOptions 构造参数。
// Python: io_adapter.py:130-139 的 keyword-only 参数
type HarnessIOAdapterOptions struct {
	// EventObserver 可选：每条原始事件在投影前调用（生命周期回调、遥测桥）
	EventObserver EventObserver
	// EmitTurnLifecycle 投影轮次开始与终态事件到输出队列；默认 false
	EmitTurnLifecycle bool
	// AutoApproveTools true 时 provider 工具审批不问宿主；false 时以
	// __interaction__ 块浮现、由 {"approved": bool} 应答。
	// agent 自身中断要求的审批（provider_data 带 interrupt_id）总是浮现。
	// 上游默认 true；零值 false 的调用方在构造后调 SetAutoApproveTools(false)。
	AutoApproveTools bool
	// StopOnUnsupportedForceAbort 宿主要求立即中止而 provider 不能交付时
	// 停掉整个周期
	StopOnUnsupportedForceAbort bool
	// ProviderInteractionHandler 宿主应答 provider 扩展请求的回调；
	// nil 时每个 ProviderInteractionRequest 都被拒绝
	ProviderInteractionHandler ProviderInteractionHandler
}

// HarnessIOAdapter 一个 HarnessProtocol 实例上的 DeepAgent 式门面。
// Python: io_adapter.py:103-153 HarnessIOAdapter
type HarnessIOAdapter struct {
	// harness 驱动的协议实现
	harness harnessprotocol.HarnessProtocol
	// options 构造参数（启动前可经 SetAutoApproveTools 修改）
	options HarnessIOAdapterOptions

	// mu 保护全部可变状态。Python 用 _lifecycle_lock（生命周期）+
	// asyncio 单线程（其余）；Go 合并成一把锁，临界区只包状态读写。
	mu sync.Mutex
	// queueCond 输出队列的等待队列（消费者等块）
	queueCond *sync.Cond
	// outputQueue 投影输出队列（Python: _output_queue）
	outputQueue []any
	// pumping 事件泵是否在跑（Python: _event_task 非 nil）
	pumping bool
	// stopped 周期是否未启动或已停止（Python: _stopped）
	stopped bool
	// outputIndex 下一个块的序号（Python: _output_index）
	outputIndex int
	// outputText 各 output_id 已发射的累计文本（Python: _output_text）
	outputText map[string]string
	// pending 挂起交互：request_id → entry（Python: _pending）
	pending map[string]*pendingInteraction
	// toolGateway 宿主原生工具网关（PrepareContext 时缓存）
	toolGateway harnessprotocol.ToolGateway
	// pumpDone 事件泵退出信号（对应 await task）
	pumpDone chan struct{}
}

// ──────────────────────────── 导出函数：构造与只读面 ────────────────────────────

// NewHarnessIOAdapter 构造适配器。
// Python: io_adapter.py:130-153 __init__
//
// AutoApproveTools 上游默认 true；需要 false 的调用方在构造后调
// SetAutoApproveTools(false)（Go 零值布尔无法区分「未设置」与「显式 false」）。
func NewHarnessIOAdapter(harness harnessprotocol.HarnessProtocol,
	opts HarnessIOAdapterOptions) *HarnessIOAdapter {
	adapter := &HarnessIOAdapter{
		harness:    harness,
		options:    opts,
		stopped:    true,
		outputText: map[string]string{},
		pending:    map[string]*pendingInteraction{},
	}
	adapter.queueCond = sync.NewCond(&adapter.mu)
	return adapter
}

// SetAutoApproveTools 启动前修改工具审批策略。
// 上游无对应方法（构造参数只读）；为绕开 Go 零值歧义而补（登记为偏差）。
func (a *HarnessIOAdapter) SetAutoApproveTools(value bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.options.AutoApproveTools = value
}

// Harness 返回驱动的协议实现。
// Python: io_adapter.py:159-161 harness
func (a *HarnessIOAdapter) Harness() harnessprotocol.HarnessProtocol { return a.harness }

// State 返回底层 harness 的当前相位。
// Python: io_adapter.py:163-165 state
func (a *HarnessIOAdapter) State() harnessprotocol.HarnessState { return a.harness.State() }

// SessionID 返回 provider 原生会话标识。
// Python: io_adapter.py:167-169 session_id
func (a *HarnessIOAdapter) SessionID() *string { return a.harness.ProviderSessionID() }

// PendingInterruptIDs 返回等待 InteractiveInput 的交互 ID。
// Python: io_adapter.py:171-174 pending_interrupt_ids
func (a *HarnessIOAdapter) PendingInterruptIDs() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	ids := make([]string, 0, len(a.pending))
	for id := range a.pending {
		ids = append(ids, id)
	}
	return ids
}

// OutputCount 线程安全地读已投影块数。
// 供宿主/测试在泵运行期间观察投影进度（outputIndex 由泵协程持锁写）。
func (a *HarnessIOAdapter) OutputCount() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.outputIndex
}

// HasPendingInterrupt 是否有挂起交互。
// Python: io_adapter.py:176-177 has_pending_interrupt
func (a *HarnessIOAdapter) HasPendingInterrupt() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.pending) > 0
}

// IsPendingInterruptResumeValid user_input 是否至少答到一个挂起交互。
// Python: io_adapter.py:179-185 is_pending_interrupt_resume_valid
func (a *HarnessIOAdapter) IsPendingInterruptResumeValid(userInput *interaction.InteractiveInput) bool {
	if userInput == nil {
		return false
	}
	a.mu.Lock()
	count := len(a.pending)
	a.mu.Unlock()
	if count == 0 {
		return false
	}
	if userInput.RawInputs != nil {
		return count == 1
	}
	return len(a.matchedAnswers(userInput)) > 0
}

// ──────────────────────────── 导出函数：生命周期 ────────────────────────────

// PrepareContext 返回把本适配器装成交互宿主后的上下文。
// Python: io_adapter.py:203-218 prepare_context
func (a *HarnessIOAdapter) PrepareContext(harnessContext harnessprotocol.HarnessContext) harnessprotocol.HarnessContext {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.prepareContextLocked(harnessContext)
}

// prepareContextLocked 持锁版本的 PrepareContext。
//
// ⚠️ Start 在自己的临界区里调用本方法——Python 的 prepare_context 是同步
// 方法且外层 _lifecycle_lock 是协程级锁，嵌套安全；Go 的 sync.Mutex 不可
// 重入，Start 若调 PrepareContext（再 Lock）会自锁（实测死锁）。
func (a *HarnessIOAdapter) prepareContextLocked(harnessContext harnessprotocol.HarnessContext) harnessprotocol.HarnessContext {
	capabilities := make(map[harnessprotocol.HostCapability]bool,
		len(harnessContext.HostCapabilities)+4)
	for capability, declared := range harnessContext.HostCapabilities {
		capabilities[capability] = declared
	}
	// Python: :205 `set(context.host_capabilities) | {HostCapability.USER_INPUT}`
	capabilities[harnessprotocol.HostCapabilityUserInput] = true
	if !a.options.AutoApproveTools {
		capabilities[harnessprotocol.HostCapabilityToolApproval] = true
	}
	if a.options.ProviderInteractionHandler != nil {
		capabilities[harnessprotocol.HostCapabilityProviderInteraction] = true
	}
	if harnessContext.Tools != nil {
		// Python: :210-211
		capabilities[harnessprotocol.HostCapabilityNativeToolGateway] = true
		capabilities[harnessprotocol.HostCapabilityDynamicToolCall] = true
	}
	a.toolGateway = harnessContext.Tools
	// interactions 为空时装本适配器（Python: :213）
	if harnessContext.Interactions == nil {
		harnessContext.Interactions = a
	}
	harnessContext.HostCapabilities = capabilities
	return harnessContext
}

// Start 启动 provider 周期与它唯一的连续事件泵。
// Python: io_adapter.py:220-239 start
func (a *HarnessIOAdapter) Start(ctx context.Context,
	harnessContext harnessprotocol.HarnessContext) error {
	a.mu.Lock()
	if !a.stopped {
		a.mu.Unlock()
		return &harnessprotocol.HarnessStateError{Msg: "harness IO adapter is already started"}
	}
	// Python: :225-227 重置输出状态
	a.outputQueue = nil
	a.outputIndex = 0
	a.outputText = map[string]string{}
	// Python: :228 prepared = self.prepare_context(context)——同一临界区内
	// 完成；Go 走持锁内部版避免自锁
	prepared := a.prepareContextLocked(harnessContext)
	a.mu.Unlock()

	// harness.Start 可能阻塞，锁外调用（Python: :230）
	if err := a.harness.Start(ctx, prepared); err != nil {
		a.safeStopHarness(ctx)
		return err
	}
	cursor := a.harness.Events()

	a.mu.Lock()
	a.stopped = false
	a.pumping = true
	done := make(chan struct{})
	a.pumpDone = done
	a.mu.Unlock()

	// Python: :236-239 asyncio.create_task(self._pump_events(cursor))
	go a.pumpEvents(ctx, cursor, done)
	return nil
}

// Stop 停止 provider 并关闭投影输出流。
// Python: io_adapter.py:241-258 stop
//
// ⚠️ 事件泵协程不得直接调本方法（Python: :243-244 的 current_task 检查）。
// Go 侧由约定保证：观察者回调里同步调 Stop 会死锁（等自己退出），
// 必须另起协程。
func (a *HarnessIOAdapter) Stop(ctx context.Context) error {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return nil
	}
	// Python: :248 fail_pending(HARNESS_STOPPED)
	a.failPendingLocked(harnessprotocol.InteractionCancelReasonHarnessStopped)
	done := a.pumpDone
	a.mu.Unlock()

	// harness.Stop 可能阻塞，锁外调用（Python: :249）
	if err := a.harness.Stop(ctx); err != nil {
		return err
	}
	// 等事件泵收敛（Python: :250-256 await task）
	if done != nil {
		<-done
	}
	a.mu.Lock()
	a.stopped = true
	// Python: :257 投 _END 唤醒消费者
	a.queueCond.Broadcast()
	a.mu.Unlock()
	return nil
}

// safeStopHarness 启动失败后的清理。
// Python: io_adapter.py:260-264 _safe_stop_harness
func (a *HarnessIOAdapter) safeStopHarness(ctx context.Context) {
	if err := a.harness.Stop(ctx); err != nil {
		logger.Error(logComponent).Err(err).Msg("harness cleanup failed after start")
	}
}

// ──────────────────────────── 导出函数：输出流 ────────────────────────────

// OutputIterator 队列背书的单消费者输出迭代器。
// Python: io_adapter.py:79-94 _OutputIterator
type OutputIterator struct {
	adapter *HarnessIOAdapter
}

// Next 取下一个投影块；流结束返回 false。
// Python: io_adapter.py:90-94 __anext__（取到 _END → StopAsyncIteration）
func (it *OutputIterator) Next(ctx context.Context) (any, bool) {
	adapter := it.adapter
	adapter.mu.Lock()
	defer adapter.mu.Unlock()
	for {
		if len(adapter.outputQueue) > 0 {
			item := adapter.outputQueue[0]
			adapter.outputQueue = adapter.outputQueue[1:]
			if _, isEnd := item.(endMarker); isEnd {
				return nil, false
			}
			return item, true
		}
		// 队列空：泵已停（或从未启动）时流终结；否则等待
		if !adapter.pumping {
			return nil, false
		}
		// ctx 取消不唤醒 cond；调用方应以短超时或关闭流收尾
		_ = ctx
		adapter.queueCond.Wait()
	}
}

// Outputs 返回队列背书的单消费者输出迭代器。
// Python: io_adapter.py:266-268 outputs
func (a *HarnessIOAdapter) Outputs() *OutputIterator {
	return &OutputIterator{adapter: a}
}

// ──────────────────────────── 导出函数：输入 ────────────────────────────

// Send 投递用户文本、原始 HarnessInput 或中断应答。
// Python: io_adapter.py:274-300 send
//
// InteractiveInput 只应答挂起交互、不投 provider 时返回 nil 回执。
func (a *HarnessIOAdapter) Send(ctx context.Context, content any, immediate bool) (*harnessprotocol.SendReceipt, error) {
	var externalInput harnessprotocol.HarnessInput
	var err error
	if interactive, ok := content.(*interaction.InteractiveInput); ok {
		// Python: :282-284 先试应答挂起交互
		if a.resolvePending(interactive) {
			return nil, nil
		}
		externalInput, err = interactiveHarnessInput(interactive)
	} else {
		externalInput, err = ToHarnessInput(content)
	}
	if err != nil {
		return nil, err
	}
	mode := a.DeliveryMode(immediate)
	receipt, sendErr := a.harness.Send(ctx, externalInput, mode)
	if sendErr == nil {
		return &receipt, nil
	}
	var stateErr *harnessprotocol.HarnessStateError
	// 终态事件可能在 RUNNING 快照后、provider STEER 接受前赢得竞态。
	// 仅当 provider 确认现在 IDLE 时重试 AUTO；被拒的命令未被接受。
	// Python: :294-300
	if !errors.As(sendErr, &stateErr) || mode != harnessprotocol.DeliveryModeSteer ||
		a.harness.State() != harnessprotocol.HarnessStateIdle {
		return nil, sendErr
	}
	retry, retryErr := a.harness.Send(ctx, externalInput, harnessprotocol.DeliveryModeAuto)
	if retryErr != nil {
		return nil, retryErr
	}
	return &retry, nil
}

// DeliveryMode 选择 provider 能兑现的投递模式。
// Python: io_adapter.py:302-308 delivery_mode
func (a *HarnessIOAdapter) DeliveryMode(immediate bool) harnessprotocol.DeliveryMode {
	if a.harness.State() != harnessprotocol.HarnessStateRunning {
		return harnessprotocol.DeliveryModeAuto
	}
	if immediate && a.harness.Card().Supports(harnessprotocol.HarnessCapabilitySteer) {
		return harnessprotocol.DeliveryModeSteer
	}
	return harnessprotocol.DeliveryModeFollowUp
}

// Abort 中止活跃轮次，按能力回退。
// Python: io_adapter.py:310-328 abort
func (a *HarnessIOAdapter) Abort(ctx context.Context, immediate bool) error {
	card := a.harness.Card()
	requested := harnessprotocol.HarnessCapabilityGracefulAbort
	fallback := harnessprotocol.HarnessCapabilityForceAbort
	if immediate {
		requested = harnessprotocol.HarnessCapabilityForceAbort
		fallback = harnessprotocol.HarnessCapabilityGracefulAbort
	}
	if card.Supports(requested) {
		mode := harnessprotocol.AbortModeGraceful
		if immediate {
			mode = harnessprotocol.AbortModeForce
		}
		return a.harness.Abort(ctx, mode)
	}
	if card.Supports(fallback) {
		mode := harnessprotocol.AbortModeForce
		if immediate {
			mode = harnessprotocol.AbortModeGraceful
		}
		return a.harness.Abort(ctx, mode)
	}
	if a.harness.State() != harnessprotocol.HarnessStateRunning {
		return nil
	}
	if immediate && a.options.StopOnUnsupportedForceAbort {
		return a.Stop(ctx)
	}
	return &harnessprotocol.UnsupportedHarnessCapabilityError{
		Msg: fmt.Sprintf("harness %q does not support %s",
			card.Name, capabilityLabel(requested)),
	}
}

// Pause 暂停（未运行时是 no-op）。
// Python: io_adapter.py:330-337 pause
func (a *HarnessIOAdapter) Pause(ctx context.Context) error {
	if a.harness.State() != harnessprotocol.HarnessStateRunning {
		return nil
	}
	if !a.harness.Card().Supports(harnessprotocol.HarnessCapabilityPauseResume) {
		return &harnessprotocol.UnsupportedHarnessCapabilityError{
			Msg: fmt.Sprintf("harness %q does not support pause/resume", a.harness.Card().Name),
		}
	}
	return a.harness.Pause(ctx)
}

// Resume 恢复。
// Python: io_adapter.py:339-347 resume
func (a *HarnessIOAdapter) Resume(ctx context.Context, query any) error {
	if a.harness.State() != harnessprotocol.HarnessStatePaused && query == nil {
		return nil
	}
	if !a.harness.Card().Supports(harnessprotocol.HarnessCapabilityPauseResume) {
		return &harnessprotocol.UnsupportedHarnessCapabilityError{
			Msg: fmt.Sprintf("harness %q does not support pause/resume", a.harness.Card().Name),
		}
	}
	var externalQuery *harnessprotocol.HarnessInput
	if query != nil {
		input, err := ToHarnessInput(query)
		if err != nil {
			return err
		}
		externalQuery = &input
	}
	return a.harness.Resume(ctx, externalQuery)
}

// ──────────────────────────── HarnessInteractionHandler 交互处理器适配 ────────────────────────────

// Handle 实现宿主交互处理器。
// Python: io_adapter.py:353-410 handle
//
// 分支顺序不可调：自动批准 → MCP 引出 → 动态工具网关 → provider 扩展 →
// 普通交互入队。
func (a *HarnessIOAdapter) Handle(ctx context.Context,
	request harnessprotocol.HarnessInteractionRequest) (harnessprotocol.HarnessInteractionResponse, error) {
	// 分支 1：工具审批自动批准；agent 自身中断要求的审批仍浮现
	// （Python: :354-359）
	if approval, ok := request.(*harnessprotocol.ToolApprovalRequest); ok &&
		a.autoApprove() && !hasInterruptID(approval.ProviderData) {
		return &harnessprotocol.ToolApprovalResponse{
			RequestID: approval.RequestID,
			Decision:  harnessprotocol.ToolApprovalDecisionAllow,
		}, nil
	}
	// 分支 2：MCP 引出直接拒绝（Python: :360-361）
	if elicitation, ok := request.(*harnessprotocol.McpElicitationRequest); ok {
		return &harnessprotocol.McpElicitationResponse{
			RequestID: elicitation.RequestID,
			Status:    harnessprotocol.InteractionResponseStatusDeclined,
		}, nil
	}
	// 分支 3：动态工具调用走宿主网关（Python: :362-392）
	if dynamic, ok := request.(*harnessprotocol.DynamicToolCallRequest); ok {
		return a.handleDynamicToolCall(ctx, dynamic)
	}
	// 分支 4：provider 扩展（Python: :393-400）
	if providerRequest, ok := request.(*harnessprotocol.ProviderInteractionRequest); ok {
		handler := a.options.ProviderInteractionHandler
		if handler == nil {
			return &harnessprotocol.ProviderInteractionResponse{
				RequestID: providerRequest.RequestID,
				Status:    harnessprotocol.InteractionResponseStatusDeclined,
			}, nil
		}
		return handler(ctx, providerRequest)
	}
	// 分支 5：普通交互入队等应答（Python: :401-410）
	requestID := interactionRequestID(request)
	a.mu.Lock()
	if _, exists := a.pending[requestID]; exists {
		a.mu.Unlock()
		return nil, &harnessprotocol.HarnessStateError{
			Msg: fmt.Sprintf("interaction %q is already pending", requestID),
		}
	}
	entry := &pendingInteraction{request: request, done: make(chan struct{})}
	a.pending[requestID] = entry
	a.outputQueue = append(a.outputQueue, a.interactionChunk(request))
	a.queueCond.Broadcast()
	a.mu.Unlock()

	response, err := entry.awaitResponse(ctx)
	// Python: :409-410 finally 注销
	a.mu.Lock()
	delete(a.pending, requestID)
	a.mu.Unlock()
	return response, err
}

// Cancel 撤销一个挂起交互（幂等）。
// Python: io_adapter.py:412-422 cancel
func (a *HarnessIOAdapter) Cancel(ctx context.Context, requestID string,
	opts ...harnessprotocol.CancelOption) error {
	a.mu.Lock()
	entry := a.pending[requestID]
	delete(a.pending, requestID)
	a.mu.Unlock()
	if entry == nil || entry.isCompleted() {
		return nil
	}
	entry.complete(cancelledResponse(entry.request), nil)
	reason := harnessprotocol.InteractionCancelReasonProviderWithdrew
	for _, opt := range opts {
		opt(&reason)
	}
	logger.Debug(logComponent).Str("request_id", requestID).
		Str("reason", string(reason)).Msg("harness interaction cancelled")
	return nil
}

// ──────────────────────────── 非导出函数：挂起交互 ────────────────────────────

// failPendingLocked 释放全部挂起交互（持锁调用）。
// Python: io_adapter.py:424-429 _fail_pending
func (a *HarnessIOAdapter) failPendingLocked(reason harnessprotocol.InteractionCancelReason) {
	for requestID, entry := range a.pending {
		delete(a.pending, requestID)
		if !entry.isCompleted() {
			entry.complete(cancelledResponse(entry.request), nil)
		}
	}
	logger.Debug(logComponent).Str("reason", string(reason)).
		Msg("harness pending interactions released")
}

// resolvePending 用 InteractiveInput 应答挂起交互；有匹配时返回 true。
// Python: io_adapter.py:431-439 _resolve_pending
func (a *HarnessIOAdapter) resolvePending(userInput *interaction.InteractiveInput) bool {
	answered := false
	// raw_inputs 只答唯一挂起项（Python: :434-436）
	a.mu.Lock()
	if userInput.RawInputs != nil && len(a.pending) == 1 {
		for requestID := range a.pending {
			a.mu.Unlock()
			a.answer(requestID, userInput.RawInputs)
			return true
		}
	}
	a.mu.Unlock()
	// 按 interrupt_id / tool_call_id 匹配（Python: :437-438）
	for requestID, value := range a.matchedAnswers(userInput) {
		answered = a.answer(requestID, value) || answered
	}
	return answered
}

// answer 向一个挂起交互投递应答；无此项或已应答时返回 false。
// Python: io_adapter.py:441-446 _answer
func (a *HarnessIOAdapter) answer(requestID string, value any) bool {
	a.mu.Lock()
	entry := a.pending[requestID]
	a.mu.Unlock()
	if entry == nil || entry.isCompleted() {
		return false
	}
	response, err := responseFor(entry.request, value)
	if err != nil {
		// 应答构造失败：按撤销处理，等待方拿到错误
		entry.complete(nil, err)
		return true
	}
	entry.complete(response, nil)
	return true
}

// matchedAnswers 把挂起交互映射成 match_answers 的输入并取回答案。
// Python: io_adapter.py:187-197 _matched_answers
//
// 每个挂起项记两条 PendingInterrupt（request_id + provider_data 里的
// tool_call_id），native harness 用后者点名调用。
func (a *HarnessIOAdapter) matchedAnswers(userInput *interaction.InteractiveInput) map[string]any {
	a.mu.Lock()
	pendingList := make([]interruptpkg.PendingInterrupt, 0, len(a.pending)*2)
	for requestID, entry := range a.pending {
		pendingList = append(pendingList,
			interruptpkg.PendingInterrupt{InterruptID: requestID, ToolCallID: requestID})
		if approval, ok := entry.request.(*harnessprotocol.ToolApprovalRequest); ok {
			callID := interruptIDFrom(approval.ProviderData)
			if callID != "" && callID != requestID {
				pendingList = append(pendingList,
					interruptpkg.PendingInterrupt{InterruptID: requestID, ToolCallID: callID})
			}
		}
	}
	a.mu.Unlock()
	return interruptpkg.MatchAnswers(userInput, pendingList).Answers
}

// autoApprove 读工具审批策略。
func (a *HarnessIOAdapter) autoApprove() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.options.AutoApproveTools
}

// ──────────────────────────── 非导出函数：事件泵与投影 ────────────────────────────

// pumpEvents 消费协议事件流并投影到输出队列。
// Python: io_adapter.py:452-470 _pump_events
func (a *HarnessIOAdapter) pumpEvents(ctx context.Context,
	cursor harnessprotocol.HarnessEventCursor, done chan struct{}) {
	defer func() {
		// 停泵要同时重置 pumping 并唤醒消费者：Next 在队列空且 pumping=true
		// 时等待；不重置会让 collectOutputs 在 Stop 之后永久挂起（实测死锁）。
		a.mu.Lock()
		a.pumping = false
		a.queueCond.Broadcast()
		a.mu.Unlock()
		// Python: :469-470 finally: await cursor.aclose()
		_ = cursor.Close(ctx)
		close(done)
	}()
	for {
		envelope, err := cursor.Next(ctx)
		if errors.Is(err, harnessprotocol.ErrEventStreamDone) || envelope == nil {
			return
		}
		if err != nil {
			// Python: :255-256 的 except Exception 兜底（stop 时）
			logger.Error(logComponent).Err(err).Msg("harness IO adapter event pump failed")
			return
		}
		// 观察者先于投影（Python: :455-457）
		if observer := a.options.EventObserver; observer != nil {
			if obsErr := observer(ctx, *envelope); obsErr != nil {
				logger.Error(logComponent).Err(obsErr).Msg("harness IO adapter event observer failed")
			}
		}
		chunk := a.projectEvent(envelope)
		if chunk == nil {
			continue
		}
		a.mu.Lock()
		a.outputQueue = append(a.outputQueue, chunk)
		a.queueCond.Broadcast()
		a.mu.Unlock()
	}
}

// projectEvent 按载荷类型分派投影。
// Python: io_adapter.py:459-466 的 isinstance 链
func (a *HarnessIOAdapter) projectEvent(envelope *harnessprotocol.HarnessEvent) any {
	switch payload := envelope.Event.(type) {
	case harnessprotocol.OutputEvent:
		return a.projectOutput(payload)
	case *harnessprotocol.OutputEvent:
		return a.projectOutput(*payload)
	case harnessprotocol.ItemLifecycleEvent:
		return a.projectItem(envelope.ItemID, payload)
	case *harnessprotocol.ItemLifecycleEvent:
		return a.projectItem(envelope.ItemID, *payload)
	case harnessprotocol.TurnLifecycleEvent:
		if a.options.EmitTurnLifecycle {
			return a.projectTurnLifecycle(envelope.TurnID, payload)
		}
	case *harnessprotocol.TurnLifecycleEvent:
		if a.options.EmitTurnLifecycle && payload != nil {
			return a.projectTurnLifecycle(envelope.TurnID, *payload)
		}
	}
	return nil
}

// projectTurnLifecycle 把轮次生命周期投影成 turn_lifecycle 块。
// Python: io_adapter.py:472-487 _project_turn_lifecycle
func (a *HarnessIOAdapter) projectTurnLifecycle(turnID string,
	event harnessprotocol.TurnLifecycleEvent) any {
	// 只投影 STARTED 与终态（Python: :475-476）
	if event.Kind != harnessprotocol.TurnEventKindStarted &&
		!harnessprotocol.IsTerminalTurnEventKind(event.Kind) {
		return nil
	}
	payload := map[string]any{
		"turn_id": turnID,
		"kind":    string(event.Kind),
	}
	if result := event.Result; result != nil {
		if result.Error != nil {
			payload["error_message"] = result.Error.Message
			if result.Error.Code != nil {
				payload["error_code"] = *result.Error.Code
			}
		}
		if result.Termination != nil {
			payload["termination_kind"] = string(result.Termination.Kind)
		}
		if text, ok := result.FinalOutput.(string); ok {
			payload["final_output"] = text
		}
	}
	return stream.OutputSchema{
		Type:    TurnLifecycleChunkType,
		Index:   a.nextOutputIndex(),
		Payload: payload,
	}
}

// projectOutput 把输出事件投影成 llm_output / llm_reasoning 块。
// Python: io_adapter.py:489-518 _project_output
//
// 三态：DELTA 直接发射；非前缀快照丢弃（append-only 语义，协议事件仍是
// 权威）；前缀快照发射增量。
func (a *HarnessIOAdapter) projectOutput(output harnessprotocol.OutputEvent) any {
	value := harnessprotocol.JsonValueToBuiltin(output.Content)
	text, isText := value.(string)
	if !isText {
		// Python: :491 json.dumps(value, ensure_ascii=False, separators=(",", ":"))
		encoded, marshalErr := utils.MarshalNoEscape(value)
		if marshalErr != nil {
			return nil
		}
		text = string(encoded)
	}
	a.mu.Lock()
	previous := a.outputText[output.OutputID]
	a.mu.Unlock()

	var emitted string
	switch {
	case output.Operation == harnessprotocol.OutputOperationDelta:
		emitted = text
		a.mu.Lock()
		a.outputText[output.OutputID] = previous + text
		a.mu.Unlock()
	case previous == "":
		emitted = text
		a.mu.Lock()
		a.outputText[output.OutputID] = text
		a.mu.Unlock()
	case strings.HasPrefix(text, previous):
		emitted = text[len(previous):]
		a.mu.Lock()
		a.outputText[output.OutputID] = text
		a.mu.Unlock()
	default:
		// OutputSchema 有 append-only 语义。协议事件仍是权威；
		// 避免在 deltas 之后重复一个 FINAL 快照（Python: :502-505）
		return nil
	}
	if emitted == "" {
		return nil
	}
	// Python: :508
	chunkType := "llm_output"
	if output.Channel == harnessprotocol.OutputChannelReasoning {
		chunkType = "llm_reasoning"
	}
	return stream.OutputSchema{
		Type:  chunkType,
		Index: a.nextOutputIndex(),
		Payload: map[string]any{
			"content":     emitted,
			"result_type": "answer",
			"output_id":   output.OutputID,
			"operation":   string(output.Operation),
		},
	}
}

// projectItem 把 item 生命周期投影成 tool_call / tool_result 块。
// Python: io_adapter.py:520-555 _project_item
func (a *HarnessIOAdapter) projectItem(itemID string, item harnessprotocol.ItemLifecycleEvent) any {
	if item.ItemType != "tool" {
		return nil
	}
	data := harnessprotocol.JsonValueToBuiltin(item.Data)
	mapping, ok := data.(map[string]any)
	if !ok {
		return nil
	}
	if item.Kind == harnessprotocol.ItemEventKindStarted {
		return stream.OutputSchema{
			Type:  "tool_call",
			Index: a.nextOutputIndex(),
			Payload: map[string]any{
				"name":         firstNonEmpty(mapping["name"], mapping["tool_name"]),
				"arguments":    argumentsAsJSON(mapping["arguments"]),
				"tool_call_id": itemID,
			},
		}
	}
	if item.Kind == harnessprotocol.ItemEventKindCompleted {
		payload := map[string]any{
			"tool_name":    firstNonEmpty(mapping["tool_name"], mapping["name"]),
			"result":       mapping["result"],
			"tool_call_id": itemID,
		}
		// 与结构化 result 并列的模型可见文本；
		// 不渲染工具结果的 provider 直接省略（Python: :545-549）
		if rendered, isText := mapping["rendered_result"].(string); isText {
			payload["rendered_result"] = rendered
		}
		return stream.OutputSchema{
			Type:    "tool_result",
			Index:   a.nextOutputIndex(),
			Payload: payload,
		}
	}
	return nil
}

// interactionChunk 把交互请求包成 __interaction__ 块（持锁调用）。
// Python: io_adapter.py:557-580 _interaction_chunk
//
// ⚠️ 唯一调用点 Handle 分支 5 在 a.mu 临界区内；序号走 nextOutputIndexLocked。
// 投影三函数（projectOutput / projectItem / projectTurnLifecycle）在锁外跑，
// 走自取锁的 nextOutputIndex。
func (a *HarnessIOAdapter) interactionChunk(request harnessprotocol.HarnessInteractionRequest) stream.OutputSchema {
	var value map[string]any
	if userInput, ok := request.(*harnessprotocol.UserInputRequest); ok {
		// Python: :558-565
		value = map[string]any{
			"kind":          "user_input",
			"interrupt_id":  userInput.RequestID,
			"prompt":        userInput.Prompt,
			"choices":       userInput.Choices,
			"provider_data": harnessprotocol.JsonValueToBuiltin(userInput.ProviderData),
		}
	} else {
		// ToolApprovalRequest（Python: :566-575）
		approval := request.(*harnessprotocol.ToolApprovalRequest)
		value = map[string]any{
			"kind":          "tool_approval",
			"interrupt_id":  approval.RequestID,
			"tool_name":     approval.ToolName,
			"arguments":     harnessprotocol.JsonValueToBuiltin(approval.Arguments),
			"reason":        reasonText(approval.Reason),
			"provider_data": harnessprotocol.JsonValueToBuiltin(approval.ProviderData),
		}
	}
	return stream.OutputSchema{
		Type:  interaction.InteractionType,
		Index: a.nextOutputIndexLocked(),
		Payload: interaction.InteractionOutput{
			ID:    interactionRequestID(request),
			Value: value,
		},
	}
}

// nextOutputIndex 发射自增序号（自取锁版）。
// Python: io_adapter.py:582-585 _next_output_index
func (a *HarnessIOAdapter) nextOutputIndex() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.nextOutputIndexLocked()
}

// nextOutputIndexLocked 持锁版本的序号自增。
// interactionChunk 在 Handle 的临界区内调用（Python: :406 在锁内 put 块），
// sync.Mutex 不可重入，必须走本方法。
func (a *HarnessIOAdapter) nextOutputIndexLocked() int {
	index := a.outputIndex
	a.outputIndex++
	return index
}

// ──────────────────────────── 非导出函数：动态工具网关 ────────────────────────────

// handleDynamicToolCall 把动态工具调用转发给宿主网关。
// Python: io_adapter.py:362-392
func (a *HarnessIOAdapter) handleDynamicToolCall(ctx context.Context,
	request *harnessprotocol.DynamicToolCallRequest) (harnessprotocol.HarnessInteractionResponse, error) {
	a.mu.Lock()
	gateway := a.toolGateway
	a.mu.Unlock()
	if gateway == nil {
		message := "no native tool gateway is available"
		return &harnessprotocol.DynamicToolCallResponse{
			RequestID:    request.RequestID,
			Status:       harnessprotocol.InteractionResponseStatusDeclined,
			IsError:      true,
			ErrorMessage: &message,
		}, nil
	}
	result, err := gateway.Invoke(ctx, harnessprotocol.ToolInvocation{
		CallID:    request.CallID,
		Name:      request.ToolName,
		Arguments: request.Arguments,
	})
	if err != nil {
		// 宿主工具失败要浮出给 provider（Python: :379-386）
		logger.Error(logComponent).Str("tool_name", request.ToolName).Err(err).
			Msg("native tool failed")
		message := err.Error()
		if message == "" {
			message = errorTypeName(err)
		}
		return &harnessprotocol.DynamicToolCallResponse{
			RequestID:    request.RequestID,
			Status:       harnessprotocol.InteractionResponseStatusCompleted,
			IsError:      true,
			ErrorMessage: &message,
		}, nil
	}
	return &harnessprotocol.DynamicToolCallResponse{
		RequestID: request.RequestID,
		Status:    harnessprotocol.InteractionResponseStatusCompleted,
		Result:    result.Content,
		IsError:   result.IsError,
	}, nil
}

// ──────────────────────────── 包级助手 ────────────────────────────

// ToHarnessInput 把 DeepAgent 式内容包成协议输入。
// Python: io_adapter.py:588-594 to_harness_input
//
// 三分支：已是 HarnessInput 直通；基础 JSON 类型直接包；其余转字符串。
func ToHarnessInput(content any) (harnessprotocol.HarnessInput, error) {
	if input, ok := content.(harnessprotocol.HarnessInput); ok {
		return input, nil
	}
	switch content.(type) {
	case nil, string, bool, int, int64, float64, float32,
		[]any, map[string]any:
		return harnessprotocol.NewHarnessInput(content, nil)
	default:
		return harnessprotocol.NewHarnessInput(fmt.Sprint(content), nil)
	}
}

// interactiveHarnessInput 把 InteractiveInput 包成协议输入。
// Python: io_adapter.py:285-288 content=content.model_dump(mode="json") +
// metadata={"kind": INTERACTIVE_INPUT_KIND}
func interactiveHarnessInput(input *interaction.InteractiveInput) (harnessprotocol.HarnessInput, error) {
	value := map[string]any{
		"user_inputs":       input.UserInputs,
		"raw_inputs":        input.RawInputs,
		"interrupt_answers": input.InterruptAnswers,
	}
	metadata, err := harnessprotocol.FreezeJsonObject(map[string]any{"kind": InteractiveInputKind})
	if err != nil {
		return harnessprotocol.HarnessInput{}, err
	}
	return harnessprotocol.NewHarnessInput(value, metadata)
}

// responseFor 把宿主答案转成对应请求类型的应答。
// Python: io_adapter.py:597-630 _response_for
func responseFor(request harnessprotocol.HarnessInteractionRequest,
	value any) (harnessprotocol.HarnessInteractionResponse, error) {
	if approval, ok := request.(*harnessprotocol.ToolApprovalRequest); ok {
		return approvalResponseFor(approval, approvalAnswer(value)), nil
	}
	content, err := harnessprotocol.FreezeJsonValue(jsonReady(value))
	if err != nil {
		return nil, err
	}
	return &harnessprotocol.UserInputResponse{
		RequestID: requestIDOf(request),
		Status:    harnessprotocol.InteractionResponseStatusCompleted,
		Content:   content,
	}, nil
}

// approvalResponseFor 审批答案 → 审批应答。
// Python: io_adapter.py:598-625
func approvalResponseFor(approval *harnessprotocol.ToolApprovalRequest,
	answer map[string]any) harnessprotocol.HarnessInteractionResponse {
	providerData := harnessprotocol.JsonObject{}
	approved, remember := false, false
	if action, exists := answer["action"]; exists {
		// skill evolution 审批（Python: :601-603）
		actionText, _ := action.(string)
		approved = actionText == "allow_once" || actionText == "allow_always"
		remember = actionText == "allow_always"
	} else {
		// 编辑参数即批准，历来如此（Python: :605-610）
		_, hasUpdated := answer["updated_arguments"]
		approved = truthy(answer["approved"]) || hasUpdated
		remember = truthy(answer["auto_confirm"])
		// auto_confirm 而 persist_allow 不是 false 时允许永久；
		// native harness 会兑现它
		if remember && answer["persist_allow"] != false {
			providerData["persist_allow"] = true
		}
	}
	decision := harnessprotocol.ToolApprovalDecisionDeny
	if approved && remember {
		decision = harnessprotocol.ToolApprovalDecisionAllowForSession
	} else if approved {
		decision = harnessprotocol.ToolApprovalDecisionAllow
	}
	var updated harnessprotocol.JsonObject
	if updatedMap, isMap := answer["updated_arguments"].(map[string]any); isMap {
		updated = harnessprotocol.JsonObject(updatedMap)
	}
	var reason *string
	if feedback, isText := answer["feedback"].(string); isText && feedback != "" {
		reason = &feedback
	}
	return &harnessprotocol.ToolApprovalResponse{
		RequestID:        approval.RequestID,
		Decision:         decision,
		UpdatedArguments: updated,
		Reason:           reason,
		ProviderData:     providerData,
	}
}

// approvalAnswer 审批答案的 dict 形态；文本按 CONFIRM 解析。
// Python: io_adapter.py:633-640 _approval_answer
func approvalAnswer(value any) map[string]any {
	if mapping, ok := value.(map[string]any); ok {
		return mapping
	}
	if text, isText := value.(string); isText {
		// Python: interrupt_answer(InterruptKind.CONFIRM, text=value)
		result, err := interruptpkg.InterruptAnswerFor(
			saschema.InterruptKindConfirm, interruptpkg.InterruptAnswerOptions{Text: &text})
		if err == nil {
			if mapping, ok := result.(map[string]any); ok {
				return mapping
			}
		}
	}
	// bool 决定；其余值（None、数字、列表）一律拒绝（Python: :639-640）
	return map[string]any{"approved": value == true}
}

// cancelledResponse 交互被撤销时的应答。
// Python: io_adapter.py:643-650 _cancelled_response
func cancelledResponse(request harnessprotocol.HarnessInteractionRequest) harnessprotocol.HarnessInteractionResponse {
	if approval, ok := request.(*harnessprotocol.ToolApprovalRequest); ok {
		reason := "interaction cancelled"
		return &harnessprotocol.ToolApprovalResponse{
			RequestID: approval.RequestID,
			Decision:  harnessprotocol.ToolApprovalDecisionDeny,
			Reason:    &reason,
		}
	}
	return &harnessprotocol.UserInputResponse{
		RequestID: requestIDOf(request),
		Status:    harnessprotocol.InteractionResponseStatusCancelled,
	}
}

// jsonReady 值归一化到 JSON 可表示形态。
// Python: io_adapter.py:653-659 _json_ready
//
// Python 探测 model_dump / dataclass；Go 的协议类型已实现 MarshalJSON，
// 原样透传即可。
func jsonReady(value any) any { return value }

// truthy 布尔真值判断（nil 与非 bool 一律 false）。
func truthy(value any) bool {
	flag, ok := value.(bool)
	return ok && flag
}

// hasInterruptID provider_data 是否带 interrupt_id。
// Python: :357 `"interrupt_id" not in request.provider_data`
func hasInterruptID(providerData harnessprotocol.JsonObject) bool {
	_, exists := providerData["interrupt_id"]
	return exists
}

// interruptIDFrom 从 provider_data 取 tool_call_id。
// Python: :194 `interaction.request.provider_data.get("tool_call_id")`
func interruptIDFrom(providerData harnessprotocol.JsonObject) string {
	value, _ := providerData["tool_call_id"].(string)
	return value
}

// reasonText 取审批原因文本（nil 安全）。
func reasonText(reason *string) string {
	if reason == nil {
		return ""
	}
	return *reason
}

// firstNonEmpty 取第一个非空字符串值（None 之外的）。
// Python: :534 `data.get("name") or data.get("tool_name") or "unknown"`
func firstNonEmpty(values ...any) string {
	for _, value := range values {
		if text, ok := value.(string); ok && text != "" {
			return text
		}
	}
	return "unknown"
}

// argumentsAsJSON 参数转 JSON 字符串（已是字符串则原样）。
// Python: :527-529
func argumentsAsJSON(arguments any) string {
	if text, ok := arguments.(string); ok {
		return text
	}
	encoded, err := utils.MarshalNoEscape(arguments)
	if err != nil {
		return ""
	}
	return string(encoded)
}

// capabilityLabel 能力的线协议字面量。
// Python: :327 `capability.value`
func capabilityLabel(capability harnessprotocol.HarnessCapability) string {
	return strings.TrimPrefix(string(capability), "HarnessCapability")
}

// requestIDOf 从交互请求取 request_id。
// Python: :622/646 `request.request_id`
func requestIDOf(request harnessprotocol.HarnessInteractionRequest) string {
	switch typed := request.(type) {
	case *harnessprotocol.ToolApprovalRequest:
		if typed != nil {
			return typed.RequestID
		}
	case *harnessprotocol.UserInputRequest:
		if typed != nil {
			return typed.RequestID
		}
	}
	return ""
}
