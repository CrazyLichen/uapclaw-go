package processcli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DuplexControlError 非法路由或不支持的交互（不回显输入）。
// Python: duplex_control.py:33-38 DuplexControlError
type DuplexControlError struct {
	// Message 失败原因
	Message string
	// Code 错误码（默认 INVALID_CONTROL）
	Code string
}

// Error 实现 error 接口。
func (e *DuplexControlError) Error() string { return e.Message }

// CodeValue 错误码提取（machine.go exceptionInfo 的错误分流面）。
func (e *DuplexControlError) CodeValue() string { return e.Code }

// streamItem 输出队列条目。
// Python: duplex_control.py:41-46 _StreamItem
type streamItem struct {
	// operationID 所属流标识
	operationID string
	// event 运行时事件（与 err/done 互斥）
	event *swruntime.RuntimeEvent
	// err 泵内捕获的异常
	err error
	// done 是否为流结束哨兵
	done bool
}

// streamPump 一条 Runtime 事件流的泵（Python 的 asyncio.Task 等价物）。
type streamPump struct {
	// cancel 取消本条泵
	cancel context.CancelFunc
	// done 泵结束信号；携带 rethrow 错误（stopping 场景）
	done chan error
}

// toolCallOutcome 宿主工具回填载体（Python asyncio.Future → 缓冲 1 channel）。
type toolCallOutcome struct {
	// result 工具结果（与 err 互斥）
	result any
	// err 工具错误（非 nil 表示失败/取消）
	err *string
}

// answerStreamer answer 流开启面（依赖倒置窄接口：
// DuplexController 作为使用方依赖协作方能力；*InProcessRuntimeClient 满足）。
// Python: duplex_control.py:15 InProcessRuntimeClient.stream_interaction_answer
type answerStreamer interface {
	StreamInteractionAnswer(ctx context.Context, answer *swruntime.InteractionAnswerInput,
		onEvent func(*swruntime.RuntimeEvent) bool) error
}

// DuplexController 在唯一持有的 Session 内把 answer 路由到已观察的交互卡。
// Python: duplex_control.py:49-79 DuplexController
//
// 原始流与 answer 流可以同时保持打开：Runtime 决定谁拥有后续输出。
// 它们的 EOF/ack 不是独立的命令结果。
type DuplexController struct {
	// reader 控制输入行读取器（nil = unattended）
	reader *DuplexLineReader
	// writer stdout 事件写出器
	writer *OneShotWriter
	// unattended 无 stdin 时对权限卡自动 reject
	unattended bool

	// failure 首条终态失败信息
	failure *protocol.RuntimeErrorInfo
	// failureStatus 终态（默认 FAILED）
	failureStatus string
	// failureExitCode 退出码（默认 2；取消 → 130）
	failureExitCode int

	mu sync.Mutex
	// cancelOwner 取消 owner（consume 主循环的 ctx）
	cancelOwner context.CancelFunc
	// inputCancel 取消 stdin 读取 goroutine
	inputCancel context.CancelFunc
	// inputDone stdin 读取 goroutine 结束信号
	inputDone chan struct{}
	// client answer 流开启方（consume 时注入）
	client answerStreamer
	// request 当前执行的请求
	request *schema.AgentRequest
	// pending 等答案的交互卡（token → deepcopy payload），上限 16
	pending map[string]map[string]any
	// pendingTools 等结果的宿主工具调用（call_id → outcome channel），上限 16
	pendingTools map[string]chan toolCallOutcome
	// queue 输出队列，容量 32（Python asyncio.Queue(maxsize=32)）
	queue chan streamItem
	// streams 活跃泵（operationID → pump）
	streams map[string]*streamPump
	// stoppingInput 命令消费方已停止接收
	stoppingInput bool
	// stoppingStreams 流已停止入队
	stoppingStreams bool
	// inputClosed stdin 已读到 EOF
	inputClosed bool
}

// ──────────────────────────── 常量 ────────────────────────────

// duplexInteractions 支持的交互事件集合。
// Python: duplex_control.py:24-26 _INTERACTIONS
var duplexInteractions = []string{
	"chat.ask_user_question",
	"plan.approval_required",
	"harness.activate_interaction",
}

// duplexAckEvents 回执事件集合（不参与完成判定）。
// Python: duplex_control.py:27-29 _ACK_EVENTS
var duplexAckEvents = []string{
	"runtime.accepted",
	"chat.answer_accepted",
	"harness.activate_resume_ack",
}

// duplexMaxPending pending 上限。
// Python: duplex_control.py:30 _MAX_PENDING = 16
const duplexMaxPending = 16

// duplexQueueSize 输出队列容量。
// Python: duplex_control.py:75 asyncio.Queue(maxsize=32)
const duplexQueueSize = 32

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDuplexController 构造控制器。
// Python: duplex_control.py:56-79 __init__
func NewDuplexController(reader *DuplexLineReader, writer *OneShotWriter, unattended bool) *DuplexController {
	return &DuplexController{
		reader:          reader,
		writer:          writer,
		unattended:      unattended,
		failureStatus:   string(protocol.StatusFailed),
		failureExitCode: 2,
		pending:         map[string]map[string]any{},
		pendingTools:    map[string]chan toolCallOutcome{},
		queue:           make(chan streamItem, duplexQueueSize),
		streams:         map[string]*streamPump{},
	}
}

// Start 登记 owner 并启动 stdin 读取 goroutine。
// Python: duplex_control.py:81-86 start
func (c *DuplexController) Start(owner context.Context, cancel context.CancelFunc) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cancelOwner = cancel
	if c.reader != nil {
		inputCtx, inputCancel := context.WithCancel(context.Background())
		c.inputCancel = inputCancel
		c.inputDone = make(chan struct{})
		go c.readControls(inputCtx)
	}
	_ = owner
}

// Consume 消费 Runtime 流直至完成；返回 completed 布尔值。
// Python: duplex_control.py:315-381 consume
//
// 步骤序严格对齐：run.started 通知 → 开根流泵 → 主循环（队列驱动）→
// done 项弹流并等待泵结束 → 错误事件短路返回 false → 交互事件登记/分流 →
// 普通事件完成判定（三形态 + suspended 排除）→ 流尽返回 completed。
func (c *DuplexController) Consume(
	ctx context.Context,
	streamFn streamDriver,
	client answerStreamer,
	request *schema.AgentRequest,
	observe func(*swruntime.RuntimeEvent) error,
) (bool, error) {
	c.mu.Lock()
	c.client = client
	c.request = request
	c.mu.Unlock()
	// Python: :325 self._notice("run.started")
	c.notice("run.started", nil)
	// Python: :326 self._add_stream(request.request_id, stream)
	c.addStream(request.RequestID, streamFn)
	completed := false
	hadInteraction := false
	// Python: :329 while self._streams or self._pending:
	for {
		c.mu.Lock()
		alive := len(c.streams) > 0 || len(c.pending) > 0
		c.mu.Unlock()
		if !alive {
			break
		}
		var item streamItem
		select {
		case item = <-c.queue:
		case <-ctx.Done():
			// owner 被 cancel/cancel 帧终止：Python 经 CancelledError 传播
			return false, ctx.Err()
		}
		// Python: :331-336 done 项：弹出泵并等待其结束；错误上抛
		if item.done {
			c.mu.Lock()
			pump := c.streams[item.operationID]
			delete(c.streams, item.operationID)
			c.mu.Unlock()
			if pump != nil {
				if pumpErr := <-pump.done; pumpErr != nil {
					return false, pumpErr
				}
			}
			if item.err != nil {
				return false, item.err
			}
			continue
		}
		event := item.event
		if event == nil {
			continue
		}
		// Python: :340-342 错误事件：observe 后直接返回失败
		if !event.OK || event.EventType() == "chat.error" || event.EventType() == "runtime.error" {
			if err := observe(event); err != nil {
				return false, err
			}
			return false, nil
		}
		// Python: :343-362 交互事件
		if _, isInteraction := interactionEvents[event.EventType()]; isInteraction {
			completed = false
			hadInteraction = true
			token, payload, regErr := c.registerInteraction(event)
			if regErr != nil {
				return false, regErr
			}
			if err := observe(event); err != nil {
				return false, err
			}
			if c.unattended {
				// Python: :348-355 权限卡自动 reject
				answers, rejErr := c.permissionRejection(payload)
				if rejErr != nil {
					return false, rejErr
				}
				c.mu.Lock()
				delete(c.pending, token)
				c.mu.Unlock()
				answer, ansErr := c.answerInput(payload, answers)
				if ansErr != nil {
					return false, ansErr
				}
				c.addStream(answer.RequestID, c.answerStreamFn(ctx, answer))
				c.notice("interaction.auto_denied", map[string]any{"interaction_id": token})
			} else {
				// Python: :357-362 通知宿主 + EOF 检查
				c.notice("interaction.requested", map[string]any{
					"interaction_id": token,
					"interaction":    payload,
				})
				c.checkAnswerAvailable()
			}
			continue
		}
		// Python: :363-364 普通事件：observe
		if err := observe(event); err != nil {
			return false, err
		}
		// Python: :365-381 完成判定
		if !containsString(duplexAckEvents, event.EventType()) && event.OK {
			rootTerminal := !hadInteraction &&
				item.operationID == request.RequestID &&
				event.IsComplete
			// UI 分段刷出与流 EOF 不是执行结果。Runtime 区分暂停刷出与
			// 完成（可能无文本）的运行，不靠文本猜测。
			final := event.EventType() == "chat.final" && len(c.pending) == 0
			explicit := event.RuntimeCompletion == "completed" && len(c.pending) == 0
			completionObserved := final || rootTerminal || explicit
			if event.RuntimeCompletion != "suspended" && completionObserved {
				completed = true
			}
		}
	}
	// Python: :381 return completed
	return completed, nil
}

// CallHostTool 挂起一个工具直到同一子进程收到其关联结果。
// Python: duplex_control.py:263-285 call_host_tool
func (c *DuplexController) CallHostTool(ctx context.Context, name string, arguments map[string]any) (any, *string) {
	c.mu.Lock()
	unavailable := c.reader == nil || c.inputClosed || c.stoppingInput
	full := len(c.pendingTools) >= duplexMaxPending
	var outCh chan toolCallOutcome
	var callID string
	if !unavailable && !full {
		callID = newRequestIDWithPrefix("")
		outCh = make(chan toolCallOutcome, 1)
		c.pendingTools[callID] = outCh
	}
	c.mu.Unlock()
	if unavailable {
		return nil, ctrlStrPtr("Host tool callback is unavailable.")
	}
	if full {
		return nil, ctrlStrPtr("Too many pending host tool calls.")
	}
	defer func() {
		c.mu.Lock()
		delete(c.pendingTools, callID)
		c.mu.Unlock()
	}()
	// Python: :277-282 host_tool.requested 通知（arguments deepcopy）
	argsCopy := deepCopyAny(arguments).(map[string]any)
	c.notice("host_tool.requested", map[string]any{
		"call_id":   callID,
		"name":      name,
		"arguments": argsCopy,
	})
	// Python: :283 return await future
	select {
	case out := <-outCh:
		return out.result, out.err
	case <-ctx.Done():
		return nil, ctrlStrPtr("Host tool call was cancelled.")
	}
}

// StopInput 命令消费方已停止：停收控制、排空队列、关 reader、取消工具等待。
// Python: duplex_control.py:383-401 stop_input
//
// 命令消费方已停止。Runtime 取消可能在 close_streams 之前关闭生产者；
// 其 finally 不得在向未排空的满队列投递终项时阻塞。
// 先排空队列解除 put() 阻塞的生产者，继续消费 Runtime 输出直到
// 公共取消 API 终止执行，但不再为已停止的命令消费方入队观测。
func (c *DuplexController) StopInput() {
	c.mu.Lock()
	c.stoppingInput = true
	c.stoppingStreams = true
	// Python: :392-393 排空队列，解除阻塞在 put() 上的生产者
	for {
		select {
		case <-c.queue:
		default:
			goto drained
		}
	}
drained:
	reader := c.reader
	c.mu.Unlock()
	if reader != nil {
		_ = reader.Close()
	}
	// Python: :396-398 取消全部待回填工具
	c.mu.Lock()
	for callID, outCh := range c.pendingTools {
		outCh <- toolCallOutcome{err: ctrlStrPtr("Host tool call was cancelled.")}
		delete(c.pendingTools, callID)
	}
	c.mu.Unlock()
	// Python: :399-401 取消 stdin 读取 goroutine 并等待
	if c.inputCancel != nil {
		c.inputCancel()
		if c.inputDone != nil {
			<-c.inputDone
		}
	}
}

// CloseStreams 取消全部泵并等待，异常上抛。
// Python: duplex_control.py:403-414 close_streams
func (c *DuplexController) CloseStreams() error {
	c.mu.Lock()
	c.stoppingStreams = true
	pumps := make([]*streamPump, 0, len(c.streams))
	for _, pump := range c.streams {
		pumps = append(pumps, pump)
	}
	c.streams = map[string]*streamPump{}
	c.pending = map[string]map[string]any{}
	c.pendingTools = map[string]chan toolCallOutcome{}
	c.mu.Unlock()
	for _, pump := range pumps {
		pump.cancel()
	}
	// Python: :411-414 gather(return_exceptions=True)；首个异常上抛
	var firstErr error
	for _, pump := range pumps {
		if err := <-pump.done; err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// FailureInfo 首条终态失败信息（machine.go 错误归一用）。
func (c *DuplexController) FailureInfo() *protocol.RuntimeErrorInfo {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failure
}

// FailureStatus 终态状态。
func (c *DuplexController) FailureStatus() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failureStatus
}

// FailureExitCode 终态退出码。
func (c *DuplexController) FailureExitCode() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.failureExitCode
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readControls stdin 读取 goroutine 主体。
// Python: duplex_control.py:107-161 _read_controls
//
// 循环读行 → 解码 → 身份校验 → 按 kind 分流；任何错误终止整个 run
// （control 输入不可恢复）。
func (c *DuplexController) readControls(inputCtx context.Context) {
	defer close(c.inputDone)
	// Python: :109-110 reader is None → RuntimeError（构造已保证非 nil）
	for {
		c.mu.Lock()
		stopping := c.stoppingInput
		c.mu.Unlock()
		if stopping {
			return
		}
		// Python: :113 line = await reader.read_line()
		line, err := c.reader.ReadLine()
		c.mu.Lock()
		stopping = c.stoppingInput
		c.mu.Unlock()
		if stopping {
			return
		}
		if err != nil {
			// Python: :155-161 except 分支：DuplexInputError → INVALID_CONTROL
			c.terminate("INVALID_CONTROL",
				"Control input was rejected; no answer was delivered.", false, nil)
			return
		}
		if line == nil {
			// Python: :116-119 EOF：input_closed = True + 待答检查
			c.mu.Lock()
			c.inputClosed = true
			c.mu.Unlock()
			c.checkAnswerAvailable()
			return
		}
		// Python: :120 control = decode_control(line)
		control, decErr := DecodeControl(line)
		if decErr != nil {
			c.terminate(controlErrorCode(decErr),
				"Control input was rejected; no answer was delivered.",
				false, controlErrorDetails(decErr))
			return
		}
		// Python: :121-122 request_id 必须匹配本次运行
		if control.RequestID != c.writer.RequestID {
			c.terminate("INVALID_CONTROL",
				"Control input was rejected; no answer was delivered.", false, nil)
			return
		}
		// Python: :123-127 session_id 匹配校验（给出可诊断的错误）
		if control.SessionID != nil && *control.SessionID != derefWriterSession(c.writer) {
			c.terminate("INVALID_CONTROL",
				"Control input was rejected; no answer was delivered.", false, nil)
			return
		}
		// Python: :128-134 cancel → 终止 owner（CANCELLED/130）
		if control.Kind == "cancel" {
			c.terminate("CANCELLED", "Command cancelled by its caller.", true, nil)
			return
		}
		// Python: :135-143 tool_result → 唤醒对应 pendingTool
		if control.Kind == "tool_result" {
			callID := ""
			if control.CallID != nil {
				callID = *control.CallID
			}
			c.mu.Lock()
			outCh, ok := c.pendingTools[callID]
			if ok {
				delete(c.pendingTools, callID)
			}
			c.mu.Unlock()
			if !ok {
				c.terminate("HOST_TOOL_NOT_PENDING",
					"Control input was rejected; no answer was delivered.",
					false, map[string]any{})
				return
			}
			outCh <- toolCallOutcome{result: control.Result, err: control.Error}
			continue
		}
		// Python: :144-154 answer → 取 pending、构造回答、开 answer 流
		interactionID := ""
		if control.InteractionID != nil {
			interactionID = *control.InteractionID
		}
		c.mu.Lock()
		payload, ok := c.pending[interactionID]
		if ok {
			delete(c.pending, interactionID)
		}
		client := c.client
		request := c.request
		c.mu.Unlock()
		if !ok {
			c.terminate("INTERACTION_NOT_PENDING",
				"Control input was rejected; no answer was delivered.",
				false, map[string]any{})
			return
		}
		if request == nil || client == nil {
			c.terminate("INVALID_CONTROL",
				"Control input was rejected; no answer was delivered.", false, nil)
			return
		}
		answer, ansErr := c.answerInput(payload, control.Answers)
		if ansErr != nil {
			c.terminate("INVALID_CONTROL",
				"Control input was rejected; no answer was delivered.", false, nil)
			return
		}
		c.addStream(answer.RequestID, c.answerStreamFn(inputCtx, answer))
	}
}

// checkAnswerAvailable stdin EOF 且仍有待答 → 终止。
// Python: duplex_control.py:163-169 _check_answer_available
func (c *DuplexController) checkAnswerAvailable() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.checkAnswerAvailableLocked()
}

// checkAnswerAvailableLocked 已持锁版本。
func (c *DuplexController) checkAnswerAvailableLocked() {
	if c.inputClosed && (len(c.pending) > 0 || len(c.pendingTools) > 0) {
		c.terminateLocked("INPUT_CLOSED",
			"Control input closed while an interaction requires an answer.", true, nil)
	}
}

// answerInput 从 payload + 原 request 参数构造类型化回答。
// Python: duplex_control.py:171-196 _answer_input
func (c *DuplexController) answerInput(payload map[string]any, answers []map[string]any) (*swruntime.InteractionAnswerInput, error) {
	c.mu.Lock()
	request := c.request
	c.mu.Unlock()
	if request == nil {
		return nil, &DuplexControlError{Message: "Execution has not started.", Code: "INVALID_CONTROL"}
	}
	// Python: :177 params = request.params or {}
	params := map[string]any{}
	if len(request.Params) > 0 {
		_ = json.Unmarshal(request.Params, &params)
	}
	// Python: :179-196 字段来源一比一
	input := swruntime.InteractionAnswerInput{
		// Python: :179 request_id=f"answer_{uuid4().hex}"
		RequestID:      "answer_" + newRequestIDWithPrefix(""),
		ChannelID:      request.ChannelID,
		SessionID:      derefTextPtr(request.SessionID),
		InteractionID:  textOr(payload, "request_id", textOr(payload, "interaction_id", "")),
		Answers:        answers,
		Source:         textOr(payload, "source", ""),
		InterruptID:    textOr(payload, "interrupt_id", ""),
		ApprovalSchema: textOr(payload, "approval_schema", ""),
		Mode:           textOr(params, "mode", ""),
		WorkMode:       textOr(params, "work_mode", ""),
		ProjectDir:     textOr(params, "project_dir", ""),
		Cwd:            textOr(params, "cwd", ""),
	}
	// Python: :189 evolution_meta=payload.get("evolution_meta")
	if meta, ok := payload["evolution_meta"].(map[string]any); ok {
		input.EvolutionMeta = meta
	}
	// Python: :194 trusted_dirs=tuple(params.get("trusted_dirs") or ())
	if dirs, ok := params["trusted_dirs"].([]any); ok {
		for _, dir := range dirs {
			if s, isStr := dir.(string); isStr {
				input.TrustedDirs = append(input.TrustedDirs, s)
			}
		}
	}
	// Python: :195 run_permissions=params.get("run_permissions")
	if perms, ok := params["run_permissions"].(map[string]any); ok {
		input.RunPermissions = perms
	}
	// Python dataclass __post_init__ 校验 → Go 构造器校验
	validated, err := swruntime.NewInteractionAnswerInput(input)
	if err != nil {
		return nil, &DuplexControlError{Message: err.Error(), Code: "INVALID_CONTROL"}
	}
	return validated, nil
}

// answerStreamFn 把 client.StreamInteractionAnswer 适配成 streamDriver。
// Python: duplex_control.py:152-154 client.stream_interaction_answer(answer)
func (c *DuplexController) answerStreamFn(ctx context.Context, answer *swruntime.InteractionAnswerInput) streamDriver {
	return func(handler func(*swruntime.RuntimeEvent) bool) error {
		c.mu.Lock()
		client := c.client
		c.mu.Unlock()
		if client == nil {
			return &DuplexControlError{Message: "Execution has not started.", Code: "INVALID_CONTROL"}
		}
		return client.StreamInteractionAnswer(ctx, answer, handler)
	}
}

// addStream 开一条流泵。
// Python: duplex_control.py:198-204 _add_stream
func (c *DuplexController) addStream(operationID string, streamFn streamDriver) {
	pumpCtx, cancel := context.WithCancel(context.Background())
	pump := &streamPump{cancel: cancel, done: make(chan error, 1)}
	c.mu.Lock()
	c.streams[operationID] = pump
	c.mu.Unlock()
	go c.pump(pumpCtx, pump, operationID, streamFn)
}

// pump 流泵：把事件投队列；异常包进终项。
// Python: duplex_control.py:206-233 _pump
func (c *DuplexController) pump(pumpCtx context.Context, pump *streamPump, operationID string, streamFn streamDriver) {
	var pumpErr error
	handler := func(event *swruntime.RuntimeEvent) bool {
		c.mu.Lock()
		stopping := c.stoppingStreams
		c.mu.Unlock()
		if stopping {
			return false
		}
		// Python: :213 await queue.put(...)（满则阻塞；stop_input 排空解锁）
		select {
		case c.queue <- streamItem{operationID: operationID, event: event}:
			return true
		case <-pumpCtx.Done():
			return false
		}
	}
	// Python: :211 async for event in stream
	if err := streamFn(handler); err != nil {
		pumpErr = err
	}
	// Python: :219-229 流关闭边界（Go 的 streamFn 返回即流关闭，无 aclose）
	if !c.isStoppingStreams() {
		// Python: :230-231 入队终项
		select {
		case c.queue <- streamItem{operationID: operationID, err: pumpErr, done: true}:
		case <-pumpCtx.Done():
		}
		pump.done <- nil
		return
	}
	// Python: :232-233 stopping 后错误仅上抛，不入队
	pump.done <- pumpErr
}

// registerInteraction 登记交互卡并返回 token 与 payload。
// Python: duplex_control.py:235-251 _register_interaction
func (c *DuplexController) registerInteraction(event *swruntime.RuntimeEvent) (string, map[string]any, error) {
	// Python: :236 payload = deepcopy(event.payload or {})
	payload := deepCopyAny(event.Payload).(map[string]any)
	// Python: :237 locator = payload.get("request_id") or payload.get("interaction_id")
	locator := textOr(payload, "request_id", textOr(payload, "interaction_id", ""))
	// Python: :238-246 harness.activate_interaction 或 locator 缺失 → 不支持
	if event.EventType() == "harness.activate_interaction" || locator == "" {
		return "", nil, &DuplexControlError{
			Message: "This interaction is not supported by the typed Runtime API.",
			Code:    "INTERACTION_UNSUPPORTED",
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	// Python: :247-248 pending 上限
	if len(c.pending) >= duplexMaxPending {
		return "", nil, &DuplexControlError{Message: "Too many pending interactions.", Code: "INVALID_CONTROL"}
	}
	token := newRequestIDWithPrefix("")
	c.pending[token] = payload
	return token, payload, nil
}

// notice 经 RuntimeEvent.control 包一层控制事件写 stdout。
// Python: duplex_control.py:253-261 _notice
func (c *DuplexController) notice(eventType string, payload map[string]any) {
	merged := map[string]any{"event_type": eventType}
	for key, value := range payload {
		merged[key] = value
	}
	// Python: :255-260 RuntimeEvent.control(request_id=writer.request_id,
	// channel_id="process_cli", session_id=writer.session_id, payload=...)
	req := &schema.AgentRequest{
		RequestID: c.writer.RequestID,
		ChannelID: "process_cli",
	}
	if c.writer.SessionID != nil {
		sid := *c.writer.SessionID
		req.SessionID = &sid
	}
	event := swruntime.Control(req, merged)
	_ = c.writer.WriteEvent(oneShotEventOf(event))
}

// permissionRejection 只用 Runtime 权限卡的显式拒绝选项。
// Python: duplex_control.py:287-313 _permission_rejection
func (c *DuplexController) permissionRejection(payload map[string]any) ([]map[string]any, error) {
	// Python: :290-294 非权限卡需要宿主回答
	if source := textOr(payload, "source", ""); source != "permission_interrupt" {
		return nil, &DuplexControlError{
			Message: "A non-permission interaction needs a host answer.",
			Code:    "INTERACTION_REQUIRED",
		}
	}
	questions, isArray := payload["questions"].([]any)
	if !isArray || len(questions) == 0 {
		return nil, &DuplexControlError{
			Message: "Permission card cannot be safely rejected.",
			Code:    "INTERACTION_UNSUPPORTED",
		}
	}
	answers := make([]map[string]any, 0, len(questions))
	for _, q := range questions {
		question, isObj := q.(map[string]any)
		if !isObj {
			return nil, &DuplexControlError{
				Message: "Permission card has no explicit reject option.",
				Code:    "INTERACTION_UNSUPPORTED",
			}
		}
		options, isArr := question["options"].([]any)
		hasReject := false
		for _, option := range options {
			opt, optObj := option.(map[string]any)
			if optObj && textOr(opt, "value", "") == "reject" {
				hasReject = true
				break
			}
		}
		if !isArr || !hasReject {
			return nil, &DuplexControlError{
				Message: "Permission card has no explicit reject option.",
				Code:    "INTERACTION_UNSUPPORTED",
			}
		}
		answers = append(answers, map[string]any{"selected_options": []any{"reject"}})
	}
	return answers, nil
}

// terminate 幂等终止：设置 failure 并取消 owner。
// Python: duplex_control.py:88-105 _terminate
func (c *DuplexController) terminate(code, message string, cancelled bool, details map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.terminateLocked(code, message, cancelled, details)
}

// terminateLocked 已持锁版本。
func (c *DuplexController) terminateLocked(code, message string, cancelled bool, details map[string]any) {
	// Python: :96-97 幂等守卫
	if c.stoppingInput || c.failure != nil {
		return
	}
	info := &protocol.RuntimeErrorInfo{Code: code, Message: message}
	if details != nil {
		info.Details = details
	}
	c.failure = info
	if cancelled {
		// Python: :101-104 CANCELLED/130
		c.failureStatus = string(protocol.StatusCancelled)
		c.failureExitCode = 130
	}
	// Python: :104-105 取消 owner
	if c.cancelOwner != nil {
		c.cancelOwner()
	}
}

// isStoppingStreams 查询 stopping_streams 标志。
func (c *DuplexController) isStoppingStreams() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.stoppingStreams
}

// ──────────────────────────── 小助手 ────────────────────────────

// containsString 集合包含判断。
func containsString(fields []string, key string) bool {
	for _, f := range fields {
		if f == key {
			return true
		}
	}
	return false
}

// ctrlStrPtr 字符串指针（machine_policy_test.go 已占 strPtr 名）。
func ctrlStrPtr(s string) *string { return &s }

// derefTextPtr 可空字符串解引用。
func derefTextPtr(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// derefWriterSession writer 会话标识解引用。
func derefWriterSession(writer *OneShotWriter) string {
	if writer.SessionID == nil {
		return ""
	}
	return *writer.SessionID
}

// textOr 取字符串字段，缺失/非字符串回退 fallback（Python `x.get(k) or fallback`）。
func textOr(payload map[string]any, key, fallback string) string {
	if v, ok := payload[key]; ok {
		if s, isStr := v.(string); isStr && s != "" {
			return s
		}
	}
	return fallback
}

// deepCopyAny JSON 值深拷贝（Python deepcopy of payload）。
func deepCopyAny(value any) any {
	raw, err := json.Marshal(value)
	if err != nil {
		return value
	}
	var out any
	if err := json.Unmarshal(raw, &out); err != nil {
		return value
	}
	return out
}

// controlErrorCode 取错误的 code 属性（Python getattr(error, "code", "INVALID_CONTROL")）。
func controlErrorCode(err error) string {
	var dpe *DuplexProtocolError
	if errors.As(err, &dpe) {
		return dpe.Code()
	}
	var dce *DuplexControlError
	if errors.As(err, &dce) {
		return dce.Code
	}
	return "INVALID_CONTROL"
}

// controlErrorDetails 取错误的 details 属性。
func controlErrorDetails(err error) map[string]any {
	var dpe *DuplexProtocolError
	if errors.As(err, &dpe) {
		return dpe.Details
	}
	return nil
}

// ensure fmt 依赖（MachineRunError 分流在 machine.go 使用）。
var _ = fmt.Sprintf
var _ = strings.TrimSpace
var _ = time.Second
