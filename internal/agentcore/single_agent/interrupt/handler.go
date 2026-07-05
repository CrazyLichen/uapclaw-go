package interrupt

import (
	"context"
	"encoding/json"
	"fmt"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
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
//
// 对应 Python: @dataclass ResumeContext
type ResumeContext struct {
	// State 工具中断状态
	State *ToolInterruptionState
	// UserInput 用户输入
	UserInput any
	// Ctx Agent 回调上下文
	Ctx *rail.AgentCallbackContext
	// ModelContext 上下文引擎的 ModelContext
	ModelContext ceinterface.ModelContext
	// Session 会话（可选）
	Session sessioninterfaces.SessionFacade
	// InvokeInputs 调用输入
	// 对应 Python: Optional[InvokeInputs]
	InvokeInputs *rail.InvokeInputs
	// ExecuteToolCall 工具调用执行函数
	// 对应 Python: Optional[Callable]
	// 实际赋值在 ReActAgent.reactLoop 中，指向 ReActAgent.executeToolCalls
	ExecuteToolCall ExecuteToolCallFunc
}

// ToolInterruptHandler 工具中断处理器。
//
// 对应 Python: ToolInterruptHandler(agent)
type ToolInterruptHandler struct {
	// agent ReActAgent 引用（最小接口）
	agent InterruptAgent
	// key 中断状态存储键
	key string
}

// ExecuteToolCallFunc 工具调用执行函数类型。
// 对应 Python: Optional[Callable] — handle_resume 中调用 execute_tool_call(ctx, tools, session, context)。
// 实际赋值在 ReActAgent.reactLoop 中，指向 ReActAgent.executeToolCalls。
// 返回 []agentschema.ExecuteResult（替代原 []any），提供类型安全。
type ExecuteToolCallFunc func(

	// ──────────────────────────── 常量 ────────────────────────────

	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	modelCtx ceinterface.ModelContext,
) ([]agentschema.ExecuteResult, error)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
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
// 收集 interrupted_tools、payloads、auto_confirm_mapping。
// 无中断时返回 (nil, nil)。
//
// 对齐 Python: build_interrupt_state(results, tool_calls, ai_message, iteration, original_query)
func (h *ToolInterruptHandler) BuildInterruptState(
	results []agentschema.ExecuteResult,
	toolCalls []*llmschema.ToolCall,
	aiMessage *llmschema.AssistantMessage,
	iteration int,
	originalQuery string,
) (*ToolInterruptionState, []PayloadEntry) {
	interruptedTools, payloads, autoConfirmMapping := h.collectInterrupts(results, toolCalls)

	if len(interruptedTools) == 0 {
		return nil, nil
	}

	intState := &ToolInterruptionState{
		BaseInterruptionState: BaseInterruptionState{
			AIMessage:     aiMessage,
			Iteration:     iteration,
			OriginalQuery: originalQuery,
		},
		InterruptedTools:   interruptedTools,
		AutoConfirmMapping: autoConfirmMapping,
	}

	return intState, payloads
}

// Save 保存中断状态到 session。
//
// 对齐 Python: save(state, session)
func (h *ToolInterruptHandler) Save(intState *ToolInterruptionState, sess sessioninterfaces.SessionFacade) {
	if sess != nil {
		sess.UpdateState(map[string]any{h.key: intState})
	}
}

// Load 从 session 加载中断状态。
//
// 对齐 Python: load(session)
func (h *ToolInterruptHandler) Load(sess sessioninterfaces.SessionFacade) *ToolInterruptionState {
	if sess != nil {
		val, _ := sess.GetState(state.StringKey(h.key))
		if st, ok := val.(*ToolInterruptionState); ok {
			return st
		}
	}
	return nil
}

// Clear 清除 session 中的中断状态。
//
// 对齐 Python: clear(session)
func (h *ToolInterruptHandler) Clear(sess sessioninterfaces.SessionFacade) {
	if sess != nil {
		sess.UpdateState(map[string]any{h.key: nil})
	}
}

// CommitInterrupt 提交中断到 ReAct 循环。
// 持久化上下文引擎状态 → 保存中断状态到 session → 构建中断结果 → 设置 invokeInputs.result。
//
// 对齐 Python: commit_interrupt(state, context, session, invoke_inputs, sub_agent_outputs)
func (h *ToolInterruptHandler) CommitInterrupt(
	ctx context.Context,
	intState *ToolInterruptionState,
	modelCtx ceinterface.ModelContext,
	sess sessioninterfaces.SessionFacade,
	invokeInputs *rail.InvokeInputs,
	subAgentOutputs []PayloadEntry,
) (map[string]any, error) {
	// 持久化上下文引擎状态
	// 对齐 Python: await self._agent.context_engine.save_contexts(session)
	// Python 中 save_contexts 失败直接抛异常传播给调用方
	ce := h.agent.ContextEngine()
	if ce != nil && sess != nil {
		if _, err := ce.SaveContexts(ctx, sess, nil); err != nil {
			logger.Error(logComponent).Err(err).Msg("commit_interrupt: 保存上下文失败")
			return nil, fmt.Errorf("commit_interrupt: 保存上下文失败: %w", err)
		}
	}

	// 保存中断状态到 session
	h.Save(intState, sess)

	// 构建中断结果
	result := BuildInterruptResult(subAgentOutputs)

	// 设置 invokeInputs.result
	// 对齐 Python: invoke_inputs.result = result
	if invokeInputs != nil {
		invokeInputs.Result = result
	}

	return result, nil
}

// WriteInterruptToStream 写入中断到 session stream。
// 遍历 result["state"] 列表，逐个 OutputSchema 写入 session.WriteStream。
//
// 对齐 Python: write_interrupt_to_stream(result, session)
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
				logger.Warn(logComponent).Err(err).Msg("write_interrupt_to_stream: 写入流失败")
				// 对齐 Python: await session.write_stream(schema) 失败时异常传播给调用方
				return fmt.Errorf("write_interrupt_to_stream: 写入流失败: %w", err)
			}
		}
	}
	return nil
}

// HandleResume 处理恢复逻辑。
// 保存自动确认 → 设置 ctx.extra[ResumeUserInputKey] → 深拷贝并重执行中断工具 →
// 重新收集中断 → 有新中断则 commit，无则设置 ctx.extra[ResumeStartIterationKey]。
//
// 对齐 Python: handle_resume(resume_ctx)
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
		Msg("从迭代恢复工具中断")

	// 保存自动确认配置
	saveAutoConfirmFromState(intState, userInput, sess)

	// 设置恢复用户输入到 ctx.extra
	// 对齐 Python: ctx.extra[RESUME_USER_INPUT_KEY] = user_input
	cbc.Extra()[ResumeUserInputKey] = userInput

	// 构建需要重执行的工具调用列表
	// 对齐 Python: copy.deepcopy(entry.tool_call) + 子 Agent 注入 query
	toolsToExecute := make([]*llmschema.ToolCall, 0)
	for _, entry := range intState.InterruptedTools {
		tc := deepCopyToolCall(entry.ToolCall)
		if entry.IsSubAgent {
			tc = buildSubAgentResumeToolCall(tc, userInput)
		}
		toolsToExecute = append(toolsToExecute, tc)
	}

	// 重执行工具调用
	// 对齐 Python: results = await execute_tool_call(ctx, tools_to_execute, session, context)
	// Python 中 execute_tool_call 失败时异常直接传播给调用方
	var results []agentschema.ExecuteResult
	if len(toolsToExecute) > 0 && resumeCtx.ExecuteToolCall != nil {
		var err error
		results, err = resumeCtx.ExecuteToolCall(ctx, cbc, toolsToExecute, sess, modelCtx)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("handle_resume: 重执行工具调用失败")
			return nil, fmt.Errorf("handle_resume: 重执行工具调用失败: %w", err)
		}
	}

	// 清除恢复用户输入
	// 对齐 Python: ctx.extra.pop(RESUME_USER_INPUT_KEY, None)
	delete(cbc.Extra(), ResumeUserInputKey)

	// 重新收集中断
	newInterruptedTools, subAgentOutputs, autoConfirmMapping := h.collectInterrupts(results, toolsToExecute)

	intState.InterruptedTools = newInterruptedTools
	intState.AutoConfirmMapping = autoConfirmMapping

	// 有新中断则 commit
	if len(newInterruptedTools) > 0 {
		return h.CommitInterrupt(ctx, intState, modelCtx, sess, invokeInputs, subAgentOutputs)
	}

	// 无新中断，设置恢复起始迭代
	// 对齐 Python: ctx.extra[RESUME_START_ITERATION_KEY] = resume_iteration + 1
	cbc.Extra()[ResumeStartIterationKey] = resumeIteration + 1
	return nil, nil
}

// BuildInterruptResult 构建中断结果 dict。
// 遍历 payloads（每个元素为 PayloadEntry{InnerID, Payload}），
// payload 为 OutputSchema 时直接追加，否则包装为 OutputSchema(type=INTERACTION)。
//
// 对齐 Python: build_interrupt_result(payloads)
// 返回 {"result_type": "interrupt", "state": [...], "interrupt_ids": [...]}
func BuildInterruptResult(payloads []PayloadEntry) map[string]any {
	interruptIDs := make([]string, 0)
	stateOutputs := make([]any, 0)

	for idx, entry := range payloads {
		interruptIDs = append(interruptIDs, entry.InnerID)

		// 对齐 Python: isinstance(payload, OutputSchema) 分支
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
//
// 对齐 Python: _collect_interrupts(results, tool_calls)
// 返回:
//   - interruptedTools: outerID → ToolInterruptEntry
//   - payloads: []PayloadEntry
//   - autoConfirmMapping: innerID → autoConfirmKey
func (h *ToolInterruptHandler) collectInterrupts(
	results []agentschema.ExecuteResult,
	toolCalls []*llmschema.ToolCall,
) (map[string]*ToolInterruptEntry, []PayloadEntry, map[string]string) {
	interruptedTools := make(map[string]*ToolInterruptEntry)
	var payloads []PayloadEntry
	autoConfirmMapping := make(map[string]string)

	for i, result := range results {
		if i >= len(toolCalls) {
			break
		}
		toolCall := toolCalls[i]
		toolResult := result.Result

		// 对齐 Python: isinstance(tool_result, ToolInterruptException)
		if tie, ok := toolResult.(*ToolInterruptException); ok {
			handleToolInterruptException(tie, toolCall, interruptedTools, &payloads, autoConfirmMapping)
		} else if isSubAgentInterrupt(toolResult) {
			handleSubAgentInterrupt(toolResult, toolCall, interruptedTools, &payloads, autoConfirmMapping)
		}
	}

	return interruptedTools, payloads, autoConfirmMapping
}

// isSubAgentInterrupt 检查结果是否为子 Agent 中断 dict。
//
// 对齐 Python: _is_sub_agent_interrupt(result)
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
//
// 对齐 Python: _handle_tool_interrupt_exception(tool_result, tool_call, interrupted_tools, payloads, auto_confirm_mapping)
func handleToolInterruptException(
	tie *ToolInterruptException,
	toolCall *llmschema.ToolCall,
	interruptedTools map[string]*ToolInterruptEntry,
	payloads *[]PayloadEntry,
	autoConfirmMapping map[string]string,
) {
	// 对齐 Python: tc = tool_result.tool_call or tool_call
	tc := tie.ToolCall
	if tc == nil {
		tc = toolCall
	}
	outerID := tc.ID
	innerID := outerID

	// 构造 ToolInterruptEntry
	// 对齐 Python: interrupt_requests={inner_id: tool_result.request}
	// tie.Request 是 *InterruptRequest，满足 InterruptRequester 接口
	interruptedTools[outerID] = &ToolInterruptEntry{
		ToolCall: tc,
		InterruptRequests: map[string]InterruptRequester{
			innerID: tie.Request,
		},
	}

	// 构造 payload: ToolCallInterruptRequest
	payload := NewToolCallInterruptRequest(tie.Request, tc)
	*payloads = append(*payloads, PayloadEntry{InnerID: innerID, Payload: payload})

	// 记录 auto_confirm_mapping
	// 对齐 Python: auto_confirm_mapping[inner_id] = tool_result.request.auto_confirm_key
	autoConfirmMapping[innerID] = tie.Request.GetAutoConfirmKey()
}

// handleSubAgentInterrupt 处理子 Agent 中断 dict。
// 从 dict["state"] 中提取 OutputSchema/InteractionOutput，
// 构造 ToolInterruptEntry(is_sub_agent=True) 和 payload。
//
// 对齐 Python: _handle_sub_agent_interrupt(tool_result, tool_call, interrupted_tools, payloads, auto_confirm_mapping)
func handleSubAgentInterrupt(
	toolResult any,
	toolCall *llmschema.ToolCall,
	interruptedTools map[string]*ToolInterruptEntry,
	payloads *[]PayloadEntry,
	autoConfirmMapping map[string]string,
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

	interruptRequests := make(map[string]InterruptRequester)

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

		// 对齐 Python: isinstance(payload_obj, ToolCallInterruptRequest)
		// Python 中 interrupt_requests[inner_id] = payload_obj（存子类实例，保留全部字段）
		// Go 中直接存 tcir（*ToolCallInterruptRequest 满足 InterruptRequester 接口），不丢子类字段
		if tcir, ok := payloadObj.(*ToolCallInterruptRequest); ok {
			interruptRequests[innerID] = tcir
			*payloads = append(*payloads, PayloadEntry{InnerID: innerID, Payload: outputSchema})
			if tcir.GetAutoConfirmKey() != "" {
				autoConfirmMapping[innerID] = tcir.GetAutoConfirmKey()
			}
		}
	}

	if _, exists := interruptedTools[outerID]; !exists {
		interruptedTools[outerID] = &ToolInterruptEntry{
			ToolCall:          toolCall,
			InterruptRequests: interruptRequests,
			IsSubAgent:        true,
		}
	}
}

// saveAutoConfirmFromState 从用户输入中提取 auto_confirm 配置，保存到 session state。
//
// 对齐 Python: _save_auto_confirm_from_state(state, user_input, session)
func saveAutoConfirmFromState(
	intState *ToolInterruptionState,
	userInput any,
	sess sessioninterfaces.SessionFacade,
) {
	if sess == nil {
		return
	}

	interactiveInput, ok := userInput.(*interaction.InteractiveInput)
	if !ok {
		return
	}

	// 对齐 Python: config = session.get_state(INTERRUPT_AUTO_CONFIRM_KEY) or {}
	configVal, _ := sess.GetState(state.StringKey(InterruptAutoConfirmKey))
	config, ok := configVal.(map[string]any)
	if !ok {
		config = make(map[string]any)
	}

	// 对齐 Python: 遍历 user_input.user_inputs，检查 auto_confirm
	for innerID, userValue := range interactiveInput.UserInputs {
		userDict, ok := userValue.(map[string]any)
		if !ok {
			continue
		}
		if autoConfirm, _ := userDict["auto_confirm"].(bool); autoConfirm {
			autoConfirmKey := intState.AutoConfirmMapping[innerID]
			if autoConfirmKey != "" {
				config[autoConfirmKey] = true
			}
		}
	}

	sess.UpdateState(map[string]any{InterruptAutoConfirmKey: config})
}

// buildSubAgentResumeToolCall 构建子 Agent 恢复时的 ToolCall。
// JSON 解析 tool_call.arguments → 注入 args["query"] = user_input → 返回修改后的 ToolCall。
//
// 对齐 Python: _build_sub_agent_resume_tool_call(tool_call, user_input)
func buildSubAgentResumeToolCall(toolCall *llmschema.ToolCall, userInput any) *llmschema.ToolCall {
	var args map[string]any
	if err := json.Unmarshal([]byte(toolCall.Arguments), &args); err != nil {
		// 对齐 Python: except (json.JSONDecodeError, TypeError): args = {}
		args = make(map[string]any)
	}

	args["query"] = userInput

	// 将修改后的 args 序列化回 JSON 字符串
	if argsJSON, err := json.Marshal(args); err == nil {
		toolCall.Arguments = string(argsJSON)
	} else {
		// 序列化失败，使用 fmt.Sprintf 兜底
		toolCall.Arguments = fmt.Sprintf("%v", args)
		logger.Warn(logComponent).Err(err).Msg("buildSubAgentResumeToolCall: 序列化 args 失败")
	}

	return toolCall
}

// deepCopyToolCall 深拷贝 ToolCall。
// 对齐 Python: copy.deepcopy(entry.tool_call)
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
