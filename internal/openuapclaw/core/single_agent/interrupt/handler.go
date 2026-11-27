package interrupt

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	ceinterface "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InterruptAgent ToolInterruptHandler 所需的最小 Agent 接口。
// 在 interrupt 包内定义，避免 interrupt → agents 循环依赖。
// ReActAgent 隐式满足此接口。
type InterruptAgent interface {
	// ContextEngine 返回上下文引擎
	ContextEngine() ceinterface.ContextEngine
}

// ResumeContext 恢复上下文，携带用户输入和恢复信息。
// Python: @dataclass ResumeContext
type ResumeContext struct {
	// State 工具中断状态
	State *saschema.ToolInterruptionState
	// UserInput 用户输入
	UserInput any
	// Ctx Agent 回调上下文
	Ctx *interfaces.AgentCallbackContext
	// ModelContext 上下文引擎的 ModelContext
	ModelContext ceinterface.ModelContext
	// Session 会话（可选）
	Session sessioninterfaces.SessionFacade
	// InvokeInputs 调用输入
	// Python: Optional[InvokeInputs]
	InvokeInputs *interfaces.InvokeInputs
	// ExecuteToolCall 工具调用执行函数
	// Python: Optional[Callable]
	// 实际赋值在 ReActAgent.reactLoop 中，指向 ReActAgent.executeToolCalls
	ExecuteToolCall ExecuteToolCallFunc
}

// ToolInterruptHandler 工具中断处理器。
// Python: ToolInterruptHandler(agent)
type ToolInterruptHandler struct {
	// agent ReActAgent 引用（最小接口）
	agent InterruptAgent
	// key 中断状态存储键
	key string
}

// ExecuteToolCallFunc 工具调用执行函数类型。
// Python: Optional[Callable] — handle_resume 中调用 execute_tool_call(ctx, tools, session, context)。
// 实际赋值在 ReActAgent.reactLoop 中，指向 ReActAgent.executeToolCalls。
// 返回 []saschema.ExecuteResult（替代原 []any），提供类型安全。
type ExecuteToolCallFunc func(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	modelCtx ceinterface.ModelContext,
) ([]saschema.ExecuteResult, error)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
	// resumeUserInputKeyMetadata request.metadata 里宿主定向恢复键的名字。
	// Python: _RESUME_USER_INPUT_KEY_METADATA = "resume_user_input_key"（handler.py:48）
	resumeUserInputKeyMetadata = "resume_user_input_key"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewToolInterruptHandler 创建 ToolInterruptHandler。
func NewToolInterruptHandler(agent InterruptAgent) *ToolInterruptHandler {
	return &ToolInterruptHandler{
		agent: agent,
		key:   InterruptionKey,
	}
}

// BuildInterruptState 构建中断状态。
// 遍历工具执行结果，区分 ToolInterruptException 和子 Agent 中断，
// 收集 interrupted_tools 与 payloads。
// 无中断时返回 (nil, nil)。
// Python: build_interrupt_state(results, tool_calls, ai_message, iteration, original_query)
func (h *ToolInterruptHandler) BuildInterruptState(
	results []saschema.ExecuteResult,
	toolCalls []*llmschema.ToolCall,
	aiMessage *llmschema.AssistantMessage,
	iteration int,
	originalQuery string,
) (*saschema.ToolInterruptionState, []PayloadEntry) {
	interruptedTools, payloads := h.collectInterrupts(results, toolCalls, nil)

	if len(interruptedTools) == 0 {
		return nil, nil
	}

	intState := &saschema.ToolInterruptionState{
		BaseInterruptionState: saschema.BaseInterruptionState{
			AIMessage:     aiMessage,
			Iteration:     iteration,
			OriginalQuery: originalQuery,
		},
		InterruptedTools: interruptedTools,
	}

	return intState, payloads
}

// Save 保存中断状态到 session。
// Python: save(state, session)
func (h *ToolInterruptHandler) Save(intState *saschema.ToolInterruptionState, sess sessioninterfaces.SessionFacade) {
	if sess != nil {
		sess.UpdateState(map[string]any{h.key: intState})
	}
}

// Load 从 session 加载中断状态。
// Python: load(session)
func (h *ToolInterruptHandler) Load(sess sessioninterfaces.SessionFacade) *saschema.ToolInterruptionState {
	if sess != nil {
		val, _ := sess.GetState(state.StringKey(h.key))
		if st, ok := val.(*saschema.ToolInterruptionState); ok {
			// 旧持久化数据无 interrupt_id 的请求按其调用派生一个，
			// 让每个加载方（含子 Agent 的副本）对同一中断取到一致 id。
			// Python: ToolInterruptEntry.__setstate__（state.py:75-81，
			// unpickle 恢复路径）；Go 的恢复入口即 Load。
			for _, entry := range st.InterruptedTools {
				if entry == nil {
					continue
				}
				for callID, request := range entry.InterruptRequests {
					if request.GetInterruptID() == "" {
						request.SetInterruptID("intr_" + callID)
					}
				}
			}
			return st
		}
	}
	return nil
}

// Clear 清除 session 中的中断状态。
// Python: clear(session)
func (h *ToolInterruptHandler) Clear(sess sessioninterfaces.SessionFacade) {
	if sess != nil {
		sess.UpdateState(map[string]any{h.key: nil})
	}
}

// CommitInterrupt 提交中断到 ReAct 循环。
// 持久化上下文引擎状态 → 保存中断状态到 session → 构建中断结果 → 设置 invokeInputs.result。
// Python: commit_interrupt(state, context, session, invoke_inputs, sub_agent_outputs)
func (h *ToolInterruptHandler) CommitInterrupt(
	ctx context.Context,
	intState *saschema.ToolInterruptionState,
	modelCtx ceinterface.ModelContext,
	sess sessioninterfaces.SessionFacade,
	invokeInputs *interfaces.InvokeInputs,
	subAgentOutputs []PayloadEntry,
) (map[string]any, error) {
	// 持久化上下文引擎状态
	// Python: await self._agent.context_engine.save_contexts(session)
	// Python 中 save_contexts 失败直接抛异常传播给调用方
	ce := h.agent.ContextEngine()
	if ce != nil && sess != nil {
		if _, err := ce.SaveContexts(ctx, sess, nil); err != nil {
			logger.Error(logComponent).Err(err).Msg("commit_interrupt: failed to save context")
			return nil, fmt.Errorf("commit_interrupt: 保存上下文失败: %w", err)
		}
	}

	// 保存中断状态到 session
	h.Save(intState, sess)

	// 构建中断结果
	result := BuildInterruptResult(subAgentOutputs)

	// 设置 invokeInputs.result
	// Python: invoke_inputs.result = result
	if invokeInputs != nil {
		invokeInputs.Result = result
	}

	return result, nil
}

// WriteInterruptToStream 写入中断到 session stream。
// 遍历 result["state"] 列表，逐个 OutputSchema 写入 session.WriteStream。
// Python: write_interrupt_to_stream(result, session)
func (h *ToolInterruptHandler) WriteInterruptToStream(
	_ context.Context,
	result map[string]any,
	sess sessioninterfaces.SessionFacade,
) error {
	schemasVal, ok := result["state"]
	if !ok {
		return nil
	}
	schemas, ok := schemasVal.([]any)
	if !ok {
		return nil
	}

	for _, schema := range schemas {
		if sess != nil {
			if err := sess.WriteStream(context.Background(), schema); err != nil {
				logger.Warn(logComponent).Err(err).Msg("write_interrupt_to_stream: stream write failed")
				// Python: await session.write_stream(schema) 失败时异常传播给调用方
				return fmt.Errorf("write_interrupt_to_stream: 写入流失败: %w", err)
			}
		}
	}
	return nil
}

// HandleResume 处理恢复逻辑。
// 保存自动确认 → 设置 ctx.extra[ResumeUserInputKey] → 深拷贝并重执行中断工具 →
// 重新收集中断 → 有新中断则 commit，无则设置 ctx.extra[ResumeStartIterationKey]。
// Python: handle_resume(resume_ctx)
// 返回 nil 表示无新中断，调用方应继续 ReAct 循环。
// 返回非 nil map 表示仍有中断，调用方应 break。
func (h *ToolInterruptHandler) HandleResume(
	ctx context.Context,
	resumeCtx *ResumeContext,
) (map[string]any, error) {
	intState := resumeCtx.State
	userInput := resumeCtx.UserInput
	cbc := resumeCtx.Ctx
	modelCtx := resumeCtx.ModelContext
	sess := resumeCtx.Session
	invokeInputs := resumeCtx.InvokeInputs

	resumeIteration := intState.Iteration
	logger.Info(logComponent).
		Int("resume_iteration", resumeIteration+1).
		Msg("resuming tool interrupt from iteration")

	// 把本次恢复输入翻译成按 interrupt_id 索引、带 owner 的答案。
	// Python: fresh_answers = self._attribute_answers(state, user_input)
	freshAnswers := attributeAnswers(intState, userInput)

	// 保存自动确认配置（按 kind 白名单，只认 approval 形状的答案）
	// Python: if isinstance(user_input, InteractiveInput): self._save_auto_confirm(fresh_answers, session)
	if _, isInteractive := userInput.(*interaction.InteractiveInput); isInteractive {
		saveAutoConfirm(freshAnswers, sess)
	}

	// 按 call_id → owner 重排，供重放期间的 rail 读取
	// Python: call_answers = self._call_answers(state, fresh_answers)
	callAnswersMap := callAnswers(intState, freshAnswers)

	// 本轮没答到的中断，commit 时沿用其 id
	// Python: unanswered = self._unanswered_interrupts(state, fresh_answers)
	unanswered := unansweredInterrupts(intState, freshAnswers)

	// 无人回答的 client tool 调用按原样保持暂停：重放它们会重跑 rails 并
	// 重新 build_request，而客户端可能还在处理它已拿到的请求。
	// Python: held = {outer_id: entry for ... if self._is_waiting_for_client(entry, fresh_answers)}
	held := make(map[string]*saschema.ToolInterruptEntry)
	for outerID, entry := range intState.InterruptedTools {
		if isWaitingForClient(entry, freshAnswers) {
			held[outerID] = entry
		}
	}

	// 设置恢复用户输入到 ctx.extra：定向键按 request.metadata 里的
	// resume_user_input_key，其余走泛化键（两者可并存）。
	// Python: resume_user_input_keys = self._resume_user_input_keys_for_state(state)
	//         ctx.extra.update({key: user_input for key in resume_user_input_keys})
	// S-24: 裸 map 写迁移 SetExtra（线程安全）
	resumeUserInputKeys := resumeUserInputKeysForState(intState)
	for _, key := range resumeUserInputKeys {
		cbc.SetExtra(key, userInput)
	}

	// 答案归属表：rail 通过 FindOwnAnswer 只读自己 owner 那份
	// Python: ctx.extra[RESUME_INTERRUPT_ANSWERS_KEY] = call_answers
	cbc.SetExtra(saschema.ResumeInterruptAnswersKey, callAnswersMap)

	// 已完成调用的图片项随恢复注入，等本轮没有任何调用暂停时再送模型
	// Python: ctx.extra[DEFERRED_TOOL_IMAGES_KEY] = list(state.deferred_tool_images)
	cbc.SetExtra(saschema.DeferredToolImagesKey, append([]map[string]any(nil), intState.DeferredToolImages...))

	// 仍有调用保持暂停时打标记，rail 据此知道本轮没有完整重放
	// Python: if held: ctx.extra[TOOL_CALLS_STILL_PAUSED_KEY] = True
	if len(held) > 0 {
		cbc.SetExtra(saschema.ToolCallsStillPausedKey, true)
	}

	// 构建需要重执行的工具调用列表（held 的调用跳过）
	// Python: copy.deepcopy(entry.tool_call) + 子 Agent 注入 query
	toolsToExecute := make([]*llmschema.ToolCall, 0)
	for outerID, entry := range intState.InterruptedTools {
		if _, isHeld := held[outerID]; isHeld {
			continue
		}
		tc := deepCopyToolCall(entry.ToolCall)
		if entry.IsSubAgent {
			tc = buildSubAgentResumeToolCall(tc, userInput)
		}
		toolsToExecute = append(toolsToExecute, tc)
	}

	// 重执行工具调用
	// Python: results = await execute_tool_call(ctx, tools_to_execute, session, context)
	// Python 中 execute_tool_call 失败时异常直接传播给调用方
	var results []saschema.ExecuteResult
	executeErr := error(nil)
	if len(toolsToExecute) > 0 && resumeCtx.ExecuteToolCall != nil {
		results, executeErr = resumeCtx.ExecuteToolCall(ctx, cbc, toolsToExecute, sess, modelCtx)
	}

	// finally 块：无论执行成败都清理恢复期注入的 ctx.extra
	// Python: finally: pop resume_user_input_keys / RESUME_INTERRUPT_ANSWERS_KEY /
	//         TOOL_CALLS_STILL_PAUSED_KEY / DEFERRED_TOOL_IMAGES_KEY
	for _, key := range resumeUserInputKeys {
		cbc.DeleteExtra(key)
	}
	cbc.DeleteExtra(saschema.ResumeInterruptAnswersKey)
	cbc.DeleteExtra(saschema.ToolCallsStillPausedKey)
	var deferredImages []map[string]any
	if raw, ok := cbc.GetExtra(saschema.DeferredToolImagesKey); ok {
		if list, isList := raw.([]map[string]any); isList {
			deferredImages = list
		}
	}
	cbc.DeleteExtra(saschema.DeferredToolImagesKey)

	// 执行失败在清理之后上抛（对齐 Python finally 后异常继续传播）
	if executeErr != nil {
		logger.Error(logComponent).Err(executeErr).Msg("handle_resume: re-execute tool call failed")
		return nil, fmt.Errorf("handle_resume: 重执行工具调用失败: %w", executeErr)
	}

	// 重新收集中断
	newInterruptedTools, subAgentOutputs := h.collectInterrupts(results, toolsToExecute, unanswered)

	// 本轮又暂停的调用，把答案带进下一轮（标记 carried），供 rail 知道
	// 副作用已执行过、不要重复。
	// Python: self._carry_answers(new_interrupted_tools, call_answers)
	carryAnswers(newInterruptedTools, callAnswersMap)

	// 为客户端保持暂停的调用按原顺序合回，payload 用 paused_call 重建
	// Python: new_interrupted_tools, sub_agent_outputs = self._merge_held_calls(
	//             state, held, new_interrupted_tools, sub_agent_outputs)
	newInterruptedTools, subAgentOutputs = mergeHeldCalls(intState, held, newInterruptedTools, subAgentOutputs)

	intState.InterruptedTools = newInterruptedTools

	// 有新中断则 commit
	if len(newInterruptedTools) > 0 {
		// Python: state.deferred_tool_images = deferred_images if new_interrupted_tools else []
		intState.DeferredToolImages = deferredImages
		return h.CommitInterrupt(ctx, intState, modelCtx, sess, invokeInputs, subAgentOutputs)
	}

	// 无新中断，清空 deferred images（本turn已无暂停调用，图片随本轮送模型）
	intState.DeferredToolImages = nil

	// 无新中断，设置恢复起始迭代
	// Python: ctx.extra[RESUME_START_ITERATION_KEY] = resume_iteration + 1
	// S-24: 裸 map 写迁移 SetExtra（线程安全）
	cbc.SetExtra(ResumeStartIterationKey, resumeIteration+1)
	return nil, nil
}

// BuildInterruptResult 构建中断结果 dict。
// 遍历 payloads（每个元素为 PayloadEntry{InnerID, Payload}），
// payload 为 OutputSchema 时直接追加，否则包装为 OutputSchema(type=INTERACTION)。
// Python: build_interrupt_result(payloads)
// 返回 {"result_type": "interrupt", "state": [...], "interrupt_ids": [...]}
func BuildInterruptResult(payloads []PayloadEntry) map[string]any {
	// interrupt_ids 必须是 []any：消费方（权限快照校验 interruptPartsOf、
	// interruptIDContains）统一按 []any 断言读取——[]string 断言失败会让
	// 普通 ask_user 中断被误判为「快照不合规」→ 会话被隔离 → 恢复轮被
	// session_runtime_quarantined 拒绝（2026-11-14 问答闭环实锤）。
	// Python 的 result["interrupt_ids"] 是通用 list，此处对齐其形状。
	interruptIDs := make([]any, 0)
	stateOutputs := make([]any, 0)

	for idx, entry := range payloads {
		interruptIDs = append(interruptIDs, entry.InnerID)

		// Python: isinstance(payload, OutputSchema) 分支
		if _, isOutputSchema := entry.Payload.(*stream.OutputSchema); isOutputSchema {
			stateOutputs = append(stateOutputs, entry.Payload)
		} else {
			// 包装为 OutputSchema(type=INTERACTION, index=idx, payload=InteractionOutput{id, value})
			stateOutputs = append(stateOutputs, &stream.OutputSchema{
				Type:  interaction.InteractionType,
				Index: idx,
				Payload: &interaction.InteractionOutput{
					ID:    entry.InnerID,
					Value: entry.Payload,
				},
			})
		}
	}

	return map[string]any{
		"result_type":   "interrupt",
		"state":         stateOutputs,
		"interrupt_ids": interruptIDs,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// collectInterrupts 收集工具中断和子 Agent 中断。
// 遍历 results，根据 ToolCallResult.Result 类型分派到
// handleToolInterruptException 或 handleSubAgentInterrupt。
// Python: _collect_interrupts(results, tool_calls)
// 返回:
//   - interruptedTools: outerID → ToolInterruptEntry（中断工具条目）
//   - payloads: []PayloadEntry（负载条目列表）
//
// collectInterrupts 收集中断并入表。
// 每个工具中断在这里拿到 id（issue_interrupt_id）；本次恢复留下未答的调用，
// 其新中断沿用原有 id。子 Agent 的中断保留它自己处理器分配的 id。
// unanswered 可为 nil（首次收集中断，或调用方不关心 id 沿用）。
// Python: _collect_interrupts(results, tool_calls, unanswered=None)
func (h *ToolInterruptHandler) collectInterrupts(
	results []saschema.ExecuteResult,
	toolCalls []*llmschema.ToolCall,
	unanswered map[string]saschema.AnswerOwnerAndID,
) (map[string]*saschema.ToolInterruptEntry, []PayloadEntry) {
	interruptedTools := make(map[string]*saschema.ToolInterruptEntry)
	var payloads []PayloadEntry

	for i, result := range results {
		if i >= len(toolCalls) {
			break
		}
		toolCall := toolCalls[i]
		toolResult := result.Result

		// Python: isinstance(tool_result, ToolInterruptException)
		if tie, ok := toolResult.(*saschema.ToolInterruptException); ok {
			// 先发号，再入表（对齐 Python 的两步顺序）
			// Python: issue_interrupt_id(tool_result.request, (tool_result.tool_call or tool_call).id, unanswered)
			owner := toolCall
			if tie.ToolCall != nil {
				owner = tie.ToolCall
			}
			issueIDFor(tie.Request, owner.ID, unanswered)
			handleToolInterruptException(tie, toolCall, interruptedTools, &payloads)
		} else if isSubAgentInterrupt(toolResult) {
			handleSubAgentInterrupt(toolResult, toolCall, interruptedTools, &payloads)
		}
	}

	return interruptedTools, payloads
}

// issueIDFor 给中断请求发号，兼容子类（AskUserRequest 等）。
// Python: issue_interrupt_id(request, tool_call_id, unanswered)
func issueIDFor(
	request saschema.InterruptRequester,
	toolCallID string,
	unanswered map[string]saschema.AnswerOwnerAndID,
) {
	// Python: issue_interrupt_id(request, tool_call_id, unanswered) —— 对任意
	// InterruptRequest 子类就地发号（基类属性多态）。Go 经接口方法等价实现。
	saschema.IssueInterruptRequesterID(request, toolCallID, unanswered)
}

// isSubAgentInterrupt 检查结果是否为子 Agent 中断 dict。
// Python: _is_sub_agent_interrupt(result)
// 判断条件: isinstance(tool_result, dict) and tool_result.get("result_type") == "interrupt" and "interrupt_ids" in tool_result
func isSubAgentInterrupt(result any) bool {
	dict, ok := result.(map[string]any)
	if !ok {
		return false
	}

	resultType, _ := dict["result_type"].(string)
	_, hasInterruptIDs := dict["interrupt_ids"]

	return resultType == "interrupt" && hasInterruptIDs
}

// handleToolInterruptException 处理 ToolInterruptException 类型的中断。
// 构造 ToolInterruptEntry 和 ToolCallInterruptRequest payload，加入 interruptedTools 和 payloads。
// Python: _handle_tool_interrupt_exception(tool_result, tool_call, interrupted_tools, payloads, auto_confirm_mapping)
func handleToolInterruptException(
	tie *saschema.ToolInterruptException,
	toolCall *llmschema.ToolCall,
	interruptedTools map[string]*saschema.ToolInterruptEntry,
	payloads *[]PayloadEntry,
) {
	// 审批标识的是嵌套目标，但恢复必须重放模型可见的外层调用，
	// wrapper 才能重建授权。
	// Python: inner_tool_call = tool_result.tool_call or tool_call
	//         outer_id = tool_call.id
	//         inner_id = inner_tool_call.id
	innerToolCall := tie.ToolCall
	if innerToolCall == nil {
		innerToolCall = toolCall
	}
	outerID := toolCall.ID
	innerID := innerToolCall.ID

	// 构造 ToolInterruptEntry
	// Python: interrupted_tools[outer_id] = ToolInterruptEntry(
	//     tool_call=tool_call,                        # 外层模型可见调用
	//     interrupt_requests={inner_id: tool_result.request},
	//     paused_call=inner_tool_call,
	// )
	interruptedTools[outerID] = &saschema.ToolInterruptEntry{
		ToolCall: toolCall,
		InterruptRequests: map[string]saschema.InterruptRequester{
			innerID: tie.Request,
		},
		PausedCall: innerToolCall,
	}

	// 构造 payload: ToolCallInterruptRequest（按 inner call 上下文）
	payload := NewToolCallInterruptRequest(tie.Request, innerToolCall)
	*payloads = append(*payloads, PayloadEntry{InnerID: innerID, Payload: payload})
}

// handleSubAgentInterrupt 处理子 Agent 中断 dict。
// 从 dict["state"] 中提取 OutputSchema/InteractionOutput，
// 构造 ToolInterruptEntry(is_sub_agent=True) 和 payload。
// Python: _handle_sub_agent_interrupt(tool_result, tool_call, interrupted_tools, payloads)
func handleSubAgentInterrupt(
	toolResult any,
	toolCall *llmschema.ToolCall,
	interruptedTools map[string]*saschema.ToolInterruptEntry,
	payloads *[]PayloadEntry,
) {
	outerID := toolCall.ID

	dict, ok := toolResult.(map[string]any)
	if !ok {
		return
	}

	subState, ok := dict["state"].([]any)
	if !ok {
		subState = nil
	}

	interruptRequests := make(map[string]saschema.InterruptRequester)

	for _, output := range subState {
		outputSchema, ok := output.(*stream.OutputSchema)
		if !ok {
			continue
		}
		payload, ok := outputSchema.Payload.(*interaction.InteractionOutput)
		if !ok {
			continue
		}

		innerID := payload.ID
		payloadObj := payload.Value

		// Python: isinstance(payload_obj, ToolCallInterruptRequest)
		// Python 中 interrupt_requests[inner_id] = payload_obj（存子类实例，保留全部字段）
		// Go 中直接存 tcir（*ToolCallInterruptRequest 满足 InterruptRequester 接口），不丢子类字段
		if tcir, ok := payloadObj.(*saschema.ToolCallInterruptRequest); ok {
			interruptRequests[innerID] = tcir
			*payloads = append(*payloads, PayloadEntry{InnerID: innerID, Payload: outputSchema})
		}
	}

	if _, exists := interruptedTools[outerID]; !exists {
		interruptedTools[outerID] = &saschema.ToolInterruptEntry{
			ToolCall:          toolCall,
			InterruptRequests: interruptRequests,
			IsSubAgent:        true,
		}
	}
}

// buildSubAgentResumeToolCall 构建子 Agent 恢复时的 ToolCall。
// JSON 解析 tool_call.arguments → 注入 args["query"] = user_input → 返回修改后的 ToolCall。
// Python: _build_sub_agent_resume_tool_call(tool_call, user_input)
func buildSubAgentResumeToolCall(toolCall *llmschema.ToolCall, userInput any) *llmschema.ToolCall {
	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Arguments), &args); err != nil {
		// Python: except (json.JSONDecodeError, TypeError): args = {}
		args = make(map[string]any)
	}

	args["query"] = userInput

	// 将修改后的 args 序列化回 JSON 字符串
	if argsJSON, err := json.Marshal(args); err == nil {
		toolCall.Arguments = string(argsJSON)
	} else {
		// 序列化失败，使用 fmt.Sprintf 兜底
		toolCall.Arguments = fmt.Sprintf("%v", args)
		logger.Warn(logComponent).Err(err).Msg("buildSubAgentResumeToolCall: args serialization failed")
	}

	return toolCall
}

// deepCopyToolCall 深拷贝 ToolCall。
// Python: copy.deepcopy(entry.tool_call)
// ToolCall 全部字段为基本类型（string + int），直接逐字段赋值即可。
func deepCopyToolCall(tc *llmschema.ToolCall) *llmschema.ToolCall {
	if tc == nil {
		return nil
	}
	return &llmschema.ToolCall{
		ID:        tc.ID,
		Type:      tc.Type,
		Name:      tc.Name,
		Arguments: tc.Arguments,
		Index:     tc.Index,
	}
}

// isWaitingForClient 该调用是否在等客户端（client tool 中断且本轮无人回答）。
// Python: _is_waiting_for_client(entry, fresh_answers)（handler.py:395-401）
func isWaitingForClient(
	entry *saschema.ToolInterruptEntry,
	freshAnswers map[string]*saschema.InterruptAnswer,
) bool {
	if entry == nil || entry.IsSubAgent || len(entry.InterruptRequests) == 0 {
		return false
	}
	for _, request := range entry.InterruptRequests {
		if request.GetInterruptOwner() != saschema.ClientToolInterruptOwner {
			return false
		}
		if _, answered := freshAnswers[request.GetInterruptID()]; answered {
			return false
		}
	}
	return true
}

// resumeUserInputKeysForState 收集本次恢复应注入 ctx.extra 的全部恢复输入键。
// 带 resume_user_input_key 元数据的请求暴露到各自的定向键下；任一请求没有
// 定向键（或整个状态没有定向键）时同时暴露泛化键。
// Python: _resume_user_input_keys_for_state(state)（handler.py:430-447）
func resumeUserInputKeysForState(intState *saschema.ToolInterruptionState) []string {
	keys := make([]string, 0)
	needsGenericKey := false
	for _, entry := range intState.InterruptedTools {
		if entry == nil {
			continue
		}
		for _, request := range entry.InterruptRequests {
			metadata := request.GetMetadata()
			if metadata == nil {
				needsGenericKey = true
				continue
			}
			raw, hasKey := metadata[resumeUserInputKeyMetadata]
			if !hasKey {
				needsGenericKey = true
				continue
			}
			key, isStr := raw.(string)
			if !isStr || key == "" {
				needsGenericKey = true
				continue
			}
			keys = append(keys, key)
		}
	}
	if needsGenericKey || len(keys) == 0 {
		keys = append(keys, ResumeUserInputKey)
	}
	return keys
}

// mergeHeldCalls 把为客户端保持暂停的调用按本轮原顺序合回。
// held 的调用按原样暂停：请求与中断 id 不变，payload 用 paused_call 重建。
// Python: _merge_held_calls(state, held, replayed, payloads)（handler.py:403-428）
func mergeHeldCalls(
	intState *saschema.ToolInterruptionState,
	held map[string]*saschema.ToolInterruptEntry,
	replayed map[string]*saschema.ToolInterruptEntry,
	payloads []PayloadEntry,
) (map[string]*saschema.ToolInterruptEntry, []PayloadEntry) {
	if len(held) == 0 {
		return replayed, payloads
	}
	payloadByID := make(map[string]any, len(payloads))
	for _, entry := range payloads {
		payloadByID[entry.InnerID] = entry.Payload
	}
	merged := make(map[string]*saschema.ToolInterruptEntry)
	mergedPayloads := make([]PayloadEntry, 0)
	for outerID := range intState.InterruptedTools {
		if entry, isHeld := held[outerID]; isHeld {
			merged[outerID] = entry
			// Python: [(inner_id, request)] = entry.interrupt_requests.items()
			// （held 恒为单请求条目；Go map 无序，取唯一元素等价）
			for innerID, request := range entry.InterruptRequests {
				innerCall := entry.PausedCall
				if innerCall == nil {
					innerCall = entry.ToolCall
				}
				mergedPayloads = append(mergedPayloads, PayloadEntry{
					InnerID: innerID,
					Payload: NewToolCallInterruptRequest(request, innerCall),
				})
			}
		} else if entry, wasReplayed := replayed[outerID]; wasReplayed {
			merged[outerID] = entry
			for innerID := range entry.InterruptRequests {
				if payload, ok := payloadByID[innerID]; ok {
					mergedPayloads = append(mergedPayloads, PayloadEntry{InnerID: innerID, Payload: payload})
				}
			}
		}
	}
	return merged, mergedPayloads
}
