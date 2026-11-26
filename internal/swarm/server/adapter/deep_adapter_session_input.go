package adapter

// 补充输入投递链（deliver 链）：host 侧三段式——校验入口、活跃轮投递、
// 竞态三层防线与 idle 分类。
// 对应上游：
//   - jiuwenswarm/server/runtime/agent_adapter/interface.py deliver_session_input（:3782-3841）
//   - jiuwenswarm/server/runtime/agent_adapter/interface_deep.py
//     deliver_session_input_impl（:18832-18878）/
//     deliver_active_session_input（:18743-18830）/
//     _stream_completion_state（:20751）
// 输入构造差异：Python 在 adapter 内 self._build_inputs(request)；Go 的
// BuildInputs 归属门面（server/runtime），由调用方传入——层级映射登记于
// 本文件尾注，非行为偏差。

import (
	"context"
	"strings"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	sessstate "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	saschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	hist "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// sessionInputLedger 全局在途条目账本（BoundaryGuard 消费；进程级，键为 display 文本）。
var sessionInputLedger = NewSessionInputLedger()

// ──────────────────────────── 导出函数 ────────────────────────────

// DeliverSessionInput 向本会话提交补充输入（独立于问答应答）。
// Python: agent_adapter/interface.py deliver_session_input（:3782-3841）
//
// 步骤：resolve mode → interrupt-resume/无 mode 拒绝 → ValidateSessionInput →
// 逐 chunk 转发；steer 模式下首个 runtime.accepted（input_boundary != "stream"）
// 触发历史落盘（is_supplemental_input extra），只落一次。
func (d *DeepAdapter) DeliverSessionInput(
	ctx context.Context, request *schema.AgentRequest, inputs map[string]any,
) (<-chan *schema.AgentResponseChunk, error) {
	params := requestParamsOf(request)
	// ① interrupt 恢复载荷或无 input_mode：补充输入必须显式声明意图
	// Python: if is_interrupt_resume_payload(request.params) or input_mode is None: raise ValueError
	mode := session.ResolveSessionInputMode(params)
	if isInterruptResumePayload(params) || mode == nil {
		return nil, contextCanceledError("supplemental input requires an explicit input mode")
	}
	// ② 整体形状校验（文案逐字对齐 runtime/session_input.go）
	if err := session.ValidateSessionInput(params); err != nil {
		return nil, err
	}

	historyPersisted := false
	out := make(chan *schema.AgentResponseChunk, 64)
	implCh, implErr := d.deliverSessionInputImpl(ctx, request, inputs)
	if implErr != nil {
		return nil, implErr
	}
	go func() {
		defer close(out)
		for chunk := range implCh {
			// ③ steer 首个 accepted（无 input_boundary）→ 历史 extra 落盘
			// Python: payload.get("event_type") == "runtime.accepted"
			//         and payload.get("input_boundary") != "stream" 且仅一次
			if !historyPersisted && *mode == session.SessionInputModeSteer {
				payload := chunk.Payload
				if et, _ := payload["event_type"].(string); et == "runtime.accepted" {
					if _, isStream := payload["input_boundary"]; !isStream {
						d.recordSupplementalHistory(request, params)
						historyPersisted = true
					}
				}
			}
			out <- chunk
		}
	}()
	return out, nil
}

// DeliverActiveSessionInputResult 活跃轮投递结果。
type DeliverActiveSessionInputResult struct {
	// Accepted 是否命中活跃轮并已入队（false = 需要新输出宿主，走 idle 分流）
	Accepted bool
}

// deliverSessionInputImpl 投递到缓存宿主且不重置其活跃运行状态。
// Python: interface_deep.py deliver_session_input_impl（:18832-18878）
//
// 非会话级适配器：借缓存子适配器转发（Python _get_cached_session_adapter 分支）；
// 会话级：登记任务 → 权限准入 → deliver_active_session_input → accepted 帧 /
// 竞态分类 / idle 分流。
func (d *DeepAdapter) deliverSessionInputImpl(
	ctx context.Context, request *schema.AgentRequest, inputs map[string]any,
) (<-chan *schema.AgentResponseChunk, error) {
	// 非会话级：借缓存宿主（Python: adapter.deliver_session_input_impl 转发）
	if !d.isSessionScoped {
		sid := sessionAdapterKey(request.SessionID)
		child := d.getCachedSessionAdapter(sid)
		if child == nil {
			return nil, contextCanceledError("session has no active adapter")
		}
		childDeep, ok := child.(*DeepAdapter)
		if !ok {
			return nil, contextCanceledError("active agent does not support supplemental input")
		}
		return childDeep.deliverSessionInputImpl(ctx, request, inputs)
	}
	// 会话级：目标必须是本子适配器所属会话
	// Python: if session_id != self._session_adapter_key(self._parent_session_id): raise ValueError
	if sessionAdapterKey(request.SessionID) != sessionAdapterKey(&d.parentSessionID) {
		return nil, contextCanceledError("supplemental input targets another Session")
	}
	sid := sessionAdapterKey(request.SessionID)
	d.registerSessionAgentTask(sid, request.RequestID)
	defer d.unregisterSessionAgentTask(sid, request.RequestID)

	// 权限准入与上下文绑定：Go 侧复用既有 sendInput 链的权限事务由
	// attachAndSendInputs 内部处理；deliver 链只需登记任务与投递判定。

	// ① 活跃轮投递（竞态三层防线在 deliverActiveSessionInput 内）
	accepted, acceptedErr := d.deliverActiveSessionInput(ctx, request, inputs)
	if acceptedErr != nil {
		return nil, acceptedErr
	}
	if accepted {
		out := make(chan *schema.AgentResponseChunk, 2)
		// accepted 帧：steer 模式带 input_boundary="stream"
		// Python: {"event_type":"runtime.accepted", **({"input_boundary":"stream"} if steer)}
		payload := map[string]any{
			"event_type": "runtime.accepted",
			"request_id": request.RequestID,
		}
		if mode := session.ResolveSessionInputMode(requestParamsOf(request)); mode != nil && *mode == session.SessionInputModeSteer {
			payload["input_boundary"] = "stream"
		}
		out <- schema.NewAgentResponseChunk(request.RequestID, request.ChannelID, payload,
			schema.WithChunkIsComplete(false))
		out <- schema.NewAgentResponseChunk(request.RequestID, request.ChannelID, nil,
			schema.WithChunkIsComplete(true))
		close(out)
		return out, nil
	}

	// ② not accepted → 竞态分类（顺序不可调整，Python :18857-18878）
	classified := classifyNotAccepted(requestParamsOf(request), d.instance != nil && d.instance.HasOutputStream())
	if classified != nil {
		return nil, classified
	}
	// ③ idle 兜底：无锚定、纯文本、无图 → 按普通流起新轮
	// Python: unary → process_message_impl / 流式 → process_message_stream_impl
	return d.ProcessMessageStreamImpl(ctx, request, inputs)
}

// deliverActiveSessionInput 经既有权限事务与输出宿主发送，返回是否命中活跃轮。
// Python: interface_deep.py deliver_active_session_input（:18743-18830）
//
// false 仅在"发送前需要新输出宿主"时返回。竞态三层防线：
//
//	① 前置判定（active_round/output stream）
//	② await 准备后复核（图片准备期间目标轮可能结束）
//	③ 与校验同一临界区内同步入队（无任何可让渡点）
func (d *DeepAdapter) deliverActiveSessionInput(
	ctx context.Context, request *schema.AgentRequest, inputs map[string]any,
) (bool, error) {
	params := requestParamsOf(request)
	mode := session.ResolveSessionInputMode(params)
	imageSteer := mode != nil && *mode == session.SessionInputModeSteer &&
		len(steerMediaItemsOf(params)) > 0

	// ① 前置判定
	// Python: if instance is None or instance.active_round is None or not instance.has_output_stream()
	if d.instance == nil || d.instance.ActiveRound() == nil || !d.instance.HasOutputStream() {
		if imageSteer {
			return false, &session.SessionInputTargetError{
				Message: "image supplemental input requires an active execution; supplemental input was not sent",
			}
		}
		return false, nil
	}
	// 交互挂起（ask_user 等待应答）→ 拒收
	// Python: if self._stream_completion_state(had_interaction=False) == "suspended"
	if d.streamCompletionState(false) == "suspended" {
		return false, &session.SessionInputRejectedError{
			Message: "session is waiting for an interaction answer; supplemental input was not sent",
		}
	}
	// require_open_input：guard 归属与准入窗口（Python SessionInputGuard.accepting）
	// Python: if guard is None or guard.owner is not instance: accepting=False;
	//         if not accepting: raise REJECTED "session is finishing or changing execution state..."
	guard := d.sessionInputGuard
	accepting := false
	if guard != nil && guard.OwnedBy(d.instance) {
		accepting = guard.Accepting()
	}
	if !accepting {
		return false, &session.SessionInputRejectedError{
			Message: "session is finishing or changing execution state; " +
				"supplemental input was not sent, retry after it settles",
		}
	}
	// 捕获绑定轮（入队复核的锚）
	// Python: bound_round = instance.active_round
	boundRound := d.instance.ActiveRound()

	// ② await 准备后复核（图片准备期间目标轮可能结束）
	// Python: if instance.active_round is None or not instance.has_output_stream(): ...
	if d.instance.ActiveRound() == nil || !d.instance.HasOutputStream() {
		if imageSteer {
			return false, &session.SessionInputTargetError{
				Message: "the targeted execution ended during image preparation; supplemental input was not sent",
			}
		}
		return false, nil
	}

	// ③ 同步入队：校验与入队同一临界区（无 await）
	// Python: entry = enqueue_bound_session_input(instance, bound_round, request, sdk_request, media_items=...)
	var entry *QueuedSessionInput
	enqueueErr := withInstanceLocked(d.instance, func() error {
		q, err := EnqueueBoundSessionInput(EnqueueBoundOptions{
			Instance:        d.instance,
			BoundRound:      boundRound,
			Request:         request,
			Query:           displayContentOf(params, inputs),
			MediaItems:      steerMediaItemsOf(params),
			ExecutionLookup: executionLookupOf(request),
		})
		entry = q
		return err
	})
	if enqueueErr != nil {
		entry.FailBoundary(enqueueErr)
		return false, enqueueErr
	}
	// ④ 发布输入边界（与模型输出同队列；失败→DeliveryUnknown，输入被丢弃
	// 而不拖垮原任务）
	// Python: await self._session_input_guard.publish_input_received(entry)
	if pubErr := guard.PublishInputReceived(ctx, entry); pubErr != nil {
		return false, pubErr
	}
	return true, nil
}

// streamCompletionState 区分中断挂起与无文本完成轮。
// Python: interface_deep.py _stream_completion_state（:20751-20758）一比一：
// loop_session 缺席 → had_interaction 分支；否则读 INTERRUPTION_KEY，
// interrupted_tools 非空 → suspended，否则 completed。
// [25.6 收尾批] 对齐修正：事实源从协调器探针改回 loop session 中断态
// （Go 中断基础设施完整存在：ToolInterruptionState.InterruptedTools），
// probe 注入按规则 8 删除。
func (d *DeepAdapter) streamCompletionState(hadInteraction bool) string {
	loopSession := d.instance.LoopSession()
	if loopSession == nil {
		if hadInteraction {
			return "suspended"
		}
		return "completed"
	}
	val, err := loopSession.GetState(sessstate.StringKey(saschema.InterruptionKey))
	if err != nil || val == nil {
		// Python: get_state 异常/缺席 → completed（getattr(state,"interrupted_tools",None) 空）
		return "completed"
	}
	switch st := val.(type) {
	case *saschema.ToolInterruptionState:
		if len(st.InterruptedTools) > 0 {
			return "suspended"
		}
	case *saschema.InterruptionState:
		// Go 工作流中断态与 Python interrupted_tools 同键承载；
		// 待处理工作流/组件同样意味着轮次挂起
		if st.PendingWorkflowID != "" || len(st.InterruptedWorkflows) > 0 {
			return "suspended"
		}
	}
	return "completed"
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// recordSupplementalHistory 落盘补充输入的用户历史（is_supplemental_input extra）。
// Python: interface.py deliver_session_input 内 history_extra 段（:3805-3830）
func (d *DeepAdapter) recordSupplementalHistory(request *schema.AgentRequest, params map[string]any) {
	expectedID, _ := params["expected_execution_id"].(string)
	query, _ := params["query"].(string)
	if query == "" {
		query, _ = params["content"].(string)
	}
	sid := ""
	if request.SessionID != nil {
		sid = strings.TrimSpace(*request.SessionID)
	}
	mode, _ := params["mode"].(string)
	hist.AppendHistoryRecord(
		sid, request.RequestID, request.ChannelID,
		"user", query, float64(nowUnixSeconds()),
		"", map[string]any{
			"is_supplemental_input": true,
			"supplemental_input": map[string]any{
				"execution_id":  expectedID,
				"stream_offset": 0,
			},
		},
		nil, mode,
	)
	logger.Info(logComponent).
		Str("session_id", sid).
		Str("request_id", request.RequestID).
		Msg("supplemental input history persisted")
}

// steerMediaItemsOf 取准入校验后的图片引用列表（shape 已由 ValidateSessionInput 保证）。
func steerMediaItemsOf(params map[string]any) []map[string]any {
	rawItems, ok := params["media_items"].([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(rawItems))
	for _, item := range rawItems {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// displayContentOf 计算展示文本：content 优先，回落 query（Python enqueue 内同序）。
func displayContentOf(params map[string]any, inputs map[string]any) string {
	if text, ok := params["content"].(string); ok && text != "" {
		return text
	}
	if text, ok := params["query"].(string); ok && text != "" {
		return text
	}
	if text, ok := inputs["query"].(string); ok {
		return text
	}
	return ""
}

// isInterruptResumePayload 判断 interrupt 恢复载荷（answers 键非空）。
// Python: is_interrupt_resume_payload（与 adapter 既有 isInterruptResumeDispatch 同义，
// 此处独立小函数避免依赖 json.RawMessage 形态）。
func isInterruptResumePayload(params map[string]any) bool {
	answers, ok := params["answers"].([]any)
	return ok && len(answers) > 0
}

// contextCanceledError 包装普通错误（本文件统一 error 构造点）。
func contextCanceledError(msg string) error {
	return &sessionInputPlainError{msg: msg}
}

// sessionInputPlainError 普通错误载体（非拒收语义，直接上抛给调用方）。
type sessionInputPlainError struct{ msg string }

func (e *sessionInputPlainError) Error() string { return e.msg }

// nowUnixSeconds 当前秒级时间戳（历史时间戳口径与 Python time.time() 一致）。
func nowUnixSeconds() int64 {
	return time.Now().Unix()
}

// CallbackOnUserMessageKind 事件种类占位（防未使用导入；投递链后续接线扩展点）。
const CallbackOnUserMessageKind = "on_user_message"

// withInstanceLocked 在实例交互临界区内执行 fn（对齐 Python "no await may
// separate the target check from enqueueing"——Go 以 DeepAgent 的
// interactionControlMu 语义承载；此处经导出包装避免触碰私有字段）。
func withInstanceLocked(instance *harness.DeepAgent, fn func() error) error {
	return instance.WithInteractionLock(func() error { return fn() })
}

// sessionInputExecutionLookup execution 账本查询闭包（门面装配注入；
// nil → 跳过 expected_execution_id 复核，与 Python runtime 缺席一致）。
var sessionInputExecutionLookup atomic.Pointer[func(string) *session.SessionExecutionHandle]

// executionLookupOf 取注入的账本查询闭包。
func executionLookupOf(request *schema.AgentRequest) func(string) *session.SessionExecutionHandle {
	if fn := sessionInputExecutionLookup.Load(); fn != nil {
		return *fn
	}
	return nil
}

// SetSessionInputExecutionLookup 供门面装配注入账本查询（生产接线点）。
func SetSessionInputExecutionLookup(fn func(string) *session.SessionExecutionHandle) {
	if fn == nil {
		sessionInputExecutionLookup.Store(nil)
		return
	}
	sessionInputExecutionLookup.Store(&fn)
}

// classifyNotAccepted not-accepted 后的竞态分类（纯函数，Python :18857-18878 一比一）。
// 返回 nil = 允许 idle 兜底（无锚定、纯文本、无图）。
func classifyNotAccepted(params map[string]any, hasOutputStream bool) error {
	// 跨会话键请求：让持久信箱重新取任务准入，不得在父执行启动/释放期间
	// 无锚定地开新轮
	// Python: if isinstance(request.params.get(SESSION_MESSAGE_INTERNAL_KEY), dict)
	if _, isCross := params[schema.CrossSessionInternalKey].(map[string]any); isCross {
		return &session.SessionInputRejectedError{
			Message: "target execution changed before submission; queue the message",
		}
	}
	// 锚定请求：目标执行已结束，补充输入未发送
	// Python: if request.params.get("expected_execution_id")
	if expected, _ := params["expected_execution_id"].(string); expected != "" {
		return &session.SessionInputTargetError{
			Message: "the targeted execution has ended; supplemental input was not sent",
		}
	}
	// 输出仍在收尾：租约未释放但回合账本已撤
	// Python: if self._instance is not None and self._instance.has_output_stream()
	if hasOutputStream {
		return contextCanceledError(
			"session output is finishing; supplemental input was not sent, retry after it settles")
	}
	return nil
}
