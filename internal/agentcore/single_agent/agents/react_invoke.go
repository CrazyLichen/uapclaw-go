package agents

import (
	"context"
	"fmt"
	"strings"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Invoke 非流式调用，包含回调包装骨架。
// 执行顺序：① transform_io input → ② emit_before → ③ invokeImpl → ④ transform_io output → ⑤ emit_after
//
// 对应 Python: _AgentMeta 元类装饰后的 invoke
func (a *ReActAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
	fw := callback.GetCallbackFramework()
	agentOpts := interfaces.NewAgentOptions(opts...)

	// ① transform_io 输入变换（对齐 Python transform_io 的 input_fn）
	if transformed := fw.TransformAgentIOInput(ctx, callback.GlobalAgentInvokeInput, inputs); transformed != nil {
		if v, ok := transformed.(map[string]any); ok {
			inputs = v
		} else {
			logger.Warn(logger.ComponentAgentCore).
				Str("event", "TransformAgentIOInput").
				Str("agent_id", a.card.ID).
				Str("expected", "map[string]any").
				Str("actual", fmt.Sprintf("%T", transformed)).
				Msg("TransformIO 返回类型不匹配，使用原始输入")
		}
	}

	// ② emit_before: 触发全局 AgentInvokeInput 事件
	fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
		Event:     callback.GlobalAgentInvokeInput,
		AgentID:   a.card.ID,
		AgentName: a.card.Name,
		Inputs:    inputs,
		Session:   agentOpts.Session,
	})

	// ③ 执行真实逻辑
	result, err := a.invokeImpl(ctx, inputs, opts...)
	if err != nil {
		// context.Canceled 时清除上下文消息
		if ctx.Err() == context.Canceled {
			a.ClearContextMessages(agentOpts.Session)
		}
		if _, ok := err.(*exception.BaseError); ok {
			return nil, err
		}
		logger.Error(logger.ComponentAgentCore).
			Str("agent_id", a.card.ID).
			Err(err).
			Msg("Agent invoke 错误")
		return nil, exception.NewBaseError(exception.StatusAgentControllerRuntimeError,
			exception.WithCause(err),
		)
	}

	// ④ transform_io 输出变换（对齐 Python transform_io 的 output_fn）
	result = fw.TransformAgentIOOutput(ctx, callback.GlobalAgentInvokeOutput, result)

	// ⑤ emit_after: 触发全局 AgentInvokeOutput 事件
	fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
		Event:     callback.GlobalAgentInvokeOutput,
		AgentID:   a.card.ID,
		AgentName: a.card.Name,
		Result:    result,
	})

	return result, nil
}

// Stream 流式调用，包含回调包装骨架。
// 执行顺序：① transform_io input → ② emit_before → streamImpl → per-item { ③ transform_io output → ④ emit_after }
//
// 对应 Python: _AgentMeta 元类装饰后的 stream
func (a *ReActAgent) Stream(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error) {
	fw := callback.GetCallbackFramework()
	agentOpts := interfaces.NewAgentOptions(opts...)

	// ① transform_io 输入变换（对齐 Python transform_io 的 input_fn）
	if transformed := fw.TransformAgentIOInput(ctx, callback.GlobalAgentStreamInput, inputs); transformed != nil {
		if v, ok := transformed.(map[string]any); ok {
			inputs = v
		} else {
			logger.Warn(logger.ComponentAgentCore).
				Str("event", "TransformAgentIOInput").
				Str("agent_id", a.card.ID).
				Str("expected", "map[string]any").
				Str("actual", fmt.Sprintf("%T", transformed)).
				Msg("TransformIO 返回类型不匹配，使用原始输入")
		}
	}

	// ② emit_before: 触发全局 AgentStreamInput 事件
	fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
		Event:     callback.GlobalAgentStreamInput,
		AgentID:   a.card.ID,
		AgentName: a.card.Name,
		Inputs:    inputs,
		Session:   agentOpts.Session,
	})

	// 调用真实 stream
	ch, err := a.streamImpl(ctx, inputs, opts...)
	if err != nil {
		if _, ok := err.(*exception.BaseError); ok {
			return nil, err
		}
		logger.Error(logger.ComponentAgentCore).
			Str("agent_id", a.card.ID).
			Err(err).
			Msg("Agent stream 错误")
		return nil, exception.NewBaseError(exception.StatusAgentControllerRuntimeError,
			exception.WithCause(err),
		)
	}

	// 包装 channel：per-item { ③ transform_io 输出变换 → ④ emit_after }
	out := make(chan stream.Schema)
	go func() {
		defer close(out)
		for item := range ch {
			// ③ transform_io 输出变换（对齐 Python transform_io 的 output_fn，per item）
			if transformed := fw.TransformAgentIOOutput(ctx, callback.GlobalAgentStreamOutput, item); transformed != nil {
				if v, ok := transformed.(stream.Schema); ok {
					item = v
				} else {
					logger.Warn(logger.ComponentAgentCore).
						Str("event", "TransformAgentIOOutput").
						Str("agent_id", a.card.ID).
						Str("expected", "stream.Schema").
						Str("actual", fmt.Sprintf("%T", transformed)).
						Msg("TransformIO 返回类型不匹配，使用原始输出")
				}
			}
			// ④ emit_after (per_item)
			fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
				Event:     callback.GlobalAgentStreamOutput,
				AgentID:   a.card.ID,
				AgentName: a.card.Name,
				Result:    item,
			})
			out <- item
		}
	}()

	return out, nil
}

// AfterExecuteToolCallForHITL 执行工具后检测 HITL 中断。
//
// 对应 Python: ReActAgent._after_execute_tool_call_for_hitl()
func (a *ReActAgent) AfterExecuteToolCallForHITL(
	results []ability.ExecuteResult,
	toolCalls []*llmschema.ToolCall,
	aiMessage *llmschema.AssistantMessage,
	iteration int,
	originalQuery string,
) (*interrupt.ToolInterruptionState, []interrupt.PayloadEntry) {
	if a.hitlHandler == nil {
		return nil, nil
	}
	intState, payloads := a.hitlHandler.BuildInterruptState(
		results, toolCalls, aiMessage, iteration, originalQuery,
	)
	if intState == nil {
		return nil, nil
	}
	return intState, payloads
}

// CommitInterrupt 提交中断状态。
//
// 对应 Python: ReActAgent._commit_interrupt() 的 HITL 分支
func (a *ReActAgent) CommitInterrupt(
	ctx context.Context,
	intState *interrupt.ToolInterruptionState,
	modelCtx ceinterface.ModelContext,
	sess sessioninterfaces.SessionFacade,
	invokeInputs *rail.InvokeInputs,
	subAgentOutputs []interrupt.PayloadEntry,
) (map[string]any, error) {
	if a.hitlHandler == nil {
		return nil, nil
	}
	return a.hitlHandler.CommitInterrupt(ctx, intState, modelCtx, sess, invokeInputs, subAgentOutputs)
}

// ClearContextMessages 清除当前上下文消息（保留历史）。
//
// 对应 Python: ReActAgent.clear_context_messages(with_history=False)
func (a *ReActAgent) ClearContextMessages(sess sessioninterfaces.SessionFacade) {
	if a.contextEngine == nil {
		return
	}
	ctx := context.Background()
	sessionID := sess.GetSessionID()
	mc := a.contextEngine.GetContext(
		"default_context", sessionID,
	)
	if mc != nil {
		_ = mc.ClearMessages(ctx, false)
	}
}

// WriteInvokeResultToStream 将 invoke 结果写入会话流。
//
// 对应 Python: ReActAgent._write_invoke_result_to_stream()
func (a *ReActAgent) WriteInvokeResultToStream(
	ctx context.Context,
	result map[string]any,
	sess sessioninterfaces.SessionFacade,
) {
	resultType, _ := result["result_type"].(string)
	if resultType == "interrupt" {
		if _, hasInterruptIDs := result["interrupt_ids"]; hasInterruptIDs {
			// HITL 中断写入流
			if a.hitlHandler != nil {
				_ = a.hitlHandler.WriteInterruptToStream(ctx, result, sess)
			}
		}
		// ⤵️ 6.12: Workflow 中断写入流（暂未实现）
		// else 分支：workflowState := result["workflow_execution_state"]
		// componentIDs := result["component_ids"]
		// pendingID := componentIDs[0]
		// 遍历 workflowState.result，写入 payload.id == pendingID 的 schema
	} else {
		// 正常 answer 结果
		output, _ := result["output"].(string)
		_ = sess.WriteStream(ctx, &stream.OutputSchema{
			Type:  "answer",
			Index: 0,
			Payload: map[string]any{
				"output":      output,
				"result_type": resultType,
			},
		})
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// invokeImpl 非流式调用的真实逻辑。
//
// 对应 Python: ReActAgent.invoke()
func (a *ReActAgent) invokeImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
	agentOpts := interfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session

	// 先提取 conversationID，自建 session 时用作 sessionID
	// 对齐 Python: session_id = conversation_id or "default_session"
	conversationID, _ := inputs["conversation_id"].(string)

	needCleanup := false // 对齐 Python: need_cleanup，仅在自建 session 时为 true
	if sess == nil {
		sessionID := conversationID
		if sessionID == "" {
			sessionID = "default_session"
		}
		newSess := session.NewSession(session.WithSessionID(sessionID))
		if err := newSess.PreRun(ctx, inputs); err != nil {
			return nil, err
		}
		sess = newSess
		needCleanup = true
	}
	// 对齐 Python: 外部传入 session 时，不调 pre_run（由调用方负责）

	invokeQuery := rail.QueryFromInputs(inputs)
	invokeInputs := &rail.InvokeInputs{
		Query:          invokeQuery,
		ConversationID: sess.GetSessionID(),
	}
	cbc := rail.NewAgentCallbackContext(a, invokeInputs, sess)

	// 设置 extra（对齐 Python L1289-1296）
	if userID, ok := inputs["user_id"].(string); ok {
		cbc.Extra()["user_id"] = userID
	}
	if runKind, ok := inputs["run_kind"].(string); ok {
		cbc.Extra()["run_kind"] = runKind
	}
	if runContext, ok := inputs["run_context"].(string); ok {
		cbc.Extra()["run_context"] = runContext
	}
	if streaming, ok := inputs["_streaming"].(bool); ok {
		cbc.Extra()["_streaming"] = streaming
	} else {
		cbc.Extra()["_streaming"] = false
	}
	if sq, ok := inputs["_steering_queue"]; ok {
		if ch, ok2 := sq.(chan string); ok2 {
			cbc.BindSteeringQueue(ch)
		}
	}

	// 对齐 Python try/finally: cleanup 始终执行（无论 FireLifecycle 是否返回错误）
	if needCleanup {
		defer func() {
			a.saveContexts(sess)
			// 对齐 Python: session.close_stream() + session.commit()
			if as, ok := sess.(*session.Session); ok {
				_ = as.CloseStream()
				_ = as.Commit(ctx)
			}
		}()
	}

	var result map[string]any
	var loopErr error

	err := cbc.FireLifecycle(rail.CallbackBeforeInvoke, rail.CallbackAfterInvoke, func() error {
		// 从 cbc 重新取 inputs（对齐 Python: user_input = ctx.inputs.query）
		// before_invoke 钩子可能修改 cbc.inputs，必须从 cbc 重新取值
		curInputs := invokeInputs
		if ci, ok := cbc.Inputs().(*rail.InvokeInputs); ok && ci != nil {
			curInputs = ci
		}

		// 对齐 Python L1301-1302: 空 query 校验
		// InteractiveInput（中断恢复）不校验 PlainText 是否为空
		if curInputs.Query.PlainText() == "" && !curInputs.Query.IsInteractiveInput() {
			return fmt.Errorf("input must contain 'query'")
		}

		// 加载 HITL 中断状态
		hitlState := a.hitlHandler.Load(sess)
		var interruptionState interrupt.ToolInterruptionState
		if hitlState != nil {
			a.hitlHandler.Clear(sess)
			interruptionState = *hitlState
		}
		// ⤵️ Workflow: interruptionState = a.loadInterruptionState(sess)

		// 如果存在中断状态，恢复原始 query
		if hitlState != nil {
			cbc.Extra()["_original_query"] = hitlState.OriginalQuery
		}

		// 初始化上下文
		modelCtx, ctxErr := a.initContext(ctx, sess)
		if ctxErr != nil {
			return fmt.Errorf("初始化上下文失败: %w", ctxErr)
		}
		cbc.SetModelContext(modelCtx)

		// 对齐 Python L1317-1326: 在 invoke 入口构建 system prompt
		// 用渲染后的 prompt 更新 identity section（替换 Configure 时的模板名）
		renderedPrompt := a.promptBuilder.Build()
		if renderedPrompt != "" {
			a.AddPromptBuilderSection(identitySection, renderedPrompt, identitySectionPriority)
		}
		// ⤴️ Skill: 更新技能提示词区段
		a.updateSkillPromptBuilderSection(ctx, renderedPrompt)

		startIteration := 0
		if hitlState != nil {
			// HITL 中断恢复分支
			resumeResult, resumeErr := a.hitlHandler.HandleResume(ctx, &interrupt.ResumeContext{
				State:           &interruptionState,
				UserInput:       curInputs.Query,
				Ctx:             cbc,
				ModelContext:    modelCtx,
				Session:         sess,
				InvokeInputs:    curInputs,
				ExecuteToolCall: a.executeToolCalls,
			})
			if resumeErr != nil {
				return resumeErr
			}
			if resumeResult != nil {
				// 仍有中断，curInputs.result 已设置
				result = resumeResult
				return nil
			}
			// 无新中断，从恢复起始迭代继续
			if si, ok := cbc.Extra()[interrupt.ResumeStartIterationKey].(int); ok {
				startIteration = si
				delete(cbc.Extra(), interrupt.ResumeStartIterationKey)
			}
		} else {
			// 正常路径：添加 UserMessage
			// 对齐 Python: _extract_user_text(user_input)
			plainText := curInputs.Query.PlainText()
			if plainText != "" && modelCtx != nil {
				_, _ = modelCtx.AddMessages(ctx, llmschema.NewUserMessage(plainText))
			}
		}

		// 调用 ReAct 循环（initContext 和 UserMessage 已在上方完成）
		if curInputs.Result == nil {
			result, loopErr = a.reactLoop(ctx, cbc, sess, modelCtx, startIteration)
		}
		return loopErr
	})

	// 合并错误判断：context.Canceled 时清除上下文消息
	if err != nil {
		if ctx.Err() == context.Canceled {
			a.ClearContextMessages(sess)
		}
		return nil, err
	}

	// 对齐 Python L1434: return ctx.extra.get("invoke_result", invoke_inputs.result)
	if invokeResult, ok := cbc.Extra()["invoke_result"]; ok {
		if r, ok2 := invokeResult.(map[string]any); ok2 {
			return r, nil
		}
	}

	// 从 cbc 取最终结果（对齐 Python: invoke_inputs.result）
	if curInputs, ok := cbc.Inputs().(*rail.InvokeInputs); ok && curInputs.Result != nil {
		return curInputs.Result, nil
	}
	if invokeInputs.Result != nil {
		return invokeInputs.Result, nil
	}
	return result, nil
}

// streamImpl 流式调用的真实逻辑。
//
// 对应 Python: ReActAgent.stream()
func (a *ReActAgent) streamImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error) {
	agentOpts := interfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session

	// 先提取 conversationID，自建 session 时用作 sessionID
	// 对齐 Python: session_id = conversation_id or "default_session"
	conversationID, _ := inputs["conversation_id"].(string)

	needCleanup := false // 对齐 Python: need_cleanup，仅在自建 session 时为 true
	if sess == nil {
		sessionID := conversationID
		if sessionID == "" {
			sessionID = "default_session"
		}
		// 对齐 Python: stream_modes 传入 StreamWriterManager
		agentOptsForModes := interfaces.NewAgentOptions(opts...)
		modes := agentOptsForModes.StreamModes

		var newSess *session.Session
		if len(modes) > 0 {
			emitter := stream.NewStreamEmitter()
			mgr := stream.NewStreamWriterManager(emitter, modes...)
			newSess = session.NewSession(
				session.WithSessionID(sessionID),
				session.WithStreamWriterManager(mgr),
			)
		} else {
			newSess = session.NewSession(session.WithSessionID(sessionID))
		}
		if err := newSess.PreRun(ctx, inputs); err != nil {
			return nil, err
		}
		sess = newSess
		opts = append(opts, interfaces.WithSession(sess))
		needCleanup = true
	}
	// 对齐 Python: 外部传入 session 时，不调 pre_run（由调用方负责）

	inputs["_streaming"] = true
	outCh := make(chan stream.Schema, 64)

	go func() {
		defer close(outCh)
		a.innerStream(ctx, sess, needCleanup, inputs, opts, outCh)
	}()

	return outCh, nil
}

// innerStream 内部流式执行。
//
// 对应 Python: ReActAgent._inner_stream()
func (a *ReActAgent) innerStream(
	ctx context.Context,
	sess sessioninterfaces.SessionFacade,
	needCleanup bool,
	inputs map[string]any,
	opts []interfaces.AgentOption,
	outCh chan<- stream.Schema,
) {
	// 断言 *session.Session 以获取生命周期方法和 StreamIterator
	agentSess, isAgentSess := sess.(*session.Session)

	// streamProcess: 在后台执行 invoke，结果写入 session stream
	streamProcess := func() {
		defer func() {
			// finally: 清理（对齐 Python L1555-1560）
			if needCleanup {
				a.saveContexts(sess)
			}
			// 对齐 Python: if self.is_agent_session: session.close_stream() + session.commit()
			if isAgentSess {
				_ = agentSess.CloseStream()
				_ = agentSess.Commit(ctx)
			}
		}()

		// 捕获 panic 防止 goroutine 崩溃
		defer func() {
			if r := recover(); r != nil {
				logger.Error(logComponent).Any("panic", r).Msg("streamProcess panic")
				a.WriteInvokeResultToStream(ctx, map[string]any{
					"output":      fmt.Sprintf("panic: %v", r),
					"result_type": "error",
				}, sess)
			}
		}()

		// 走完整 Invoke（对齐 Python: self.invoke(inputs, session, _streaming=True)）
		result, err := a.Invoke(ctx, inputs, opts...)
		if err != nil {
			// 错误结果写入流
			logger.Error(logComponent).Err(err).Str("event_type", "LLM_CALL_ERROR").Msg("streamProcess invoke 错误")
			a.WriteInvokeResultToStream(ctx, map[string]any{
				"output":      err.Error(),
				"result_type": "error",
			}, sess)
			return
		}

		// 正常结果写入流
		if resultMap, ok := result.(map[string]any); ok {
			a.WriteInvokeResultToStream(ctx, resultMap, sess)
		} else if resultList, ok := result.([]stream.Schema); ok {
			// invoke 返回 schema 列表（中断路径）
			for _, schema := range resultList {
				_ = sess.WriteStream(ctx, schema)
			}
		}
	}

	if isAgentSess {
		// Agent session: 启动 streamProcess goroutine，从 StreamIterator 消费
		go streamProcess()

		for chunk := range agentSess.StreamIterator() {
			outCh <- chunk
		}
	} else {
		// Workflow session: 直接执行 streamProcess
		// 输出通过 session.WriteStream → StreamWriterManager 传递给 Workflow
		streamProcess()
	}
}

// reactLoop ReAct 循环核心。
//
// 对应 Python: ReActAgent._inner_invoke() 中的主循环
// 注意：initContext 和 UserMessage 已在 invokeImpl 中完成，此处不再重复。
func (a *ReActAgent) reactLoop(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	sess sessioninterfaces.SessionFacade,
	modelCtx ceinterface.ModelContext,
	startIteration int,
) (map[string]any, error) {
	maxIter := defaultMaxIterations
	if a.config != nil && a.config.MaxIterations > 0 {
		maxIter = a.config.MaxIterations
	}

	tools, _ := a.getTools()

	var iterResult map[string]any
	for iteration := startIteration; iteration < maxIter; iteration++ {
		// 对齐 Python L1355: 迭代计数日志
		logger.Info(logComponent).
			Int("iteration", iteration+1).
			Int("max_iterations", maxIter).
			Msg("ReAct 迭代")

		// steering 注入
		if steeringMsgs := cbc.DrainSteering(); len(steeringMsgs) > 0 && modelCtx != nil {
			for _, msg := range steeringMsgs {
				_, _ = modelCtx.AddMessages(ctx, llmschema.NewUserMessage("[STEERING] "+msg))
			}
		}

		// 调用 LLM
		aiMsg, err := a.callModel(ctx, cbc, modelCtx, tools, sess)
		if err != nil {
			return nil, fmt.Errorf("迭代 %d 模型调用失败: %w", iteration, err)
		}

		// 强制完成 #1
		if finish := cbc.ConsumeForceFinish(); finish != nil {
			a.saveContexts(sess)
			iterResult = finish.Result
			break
		}

		if aiMsg != nil && modelCtx != nil {
			_, _ = modelCtx.AddMessages(ctx, aiMsg)
		}

		// 无工具调用
		if aiMsg == nil || len(aiMsg.ToolCalls) == 0 {
			if cbc.HasPendingSteering() {
				continue
			}
			content := ""
			if aiMsg != nil {
				content = aiMsg.Content.Text()
			}
			a.saveContexts(sess)
			iterResult = map[string]any{"output": content, "result_type": "answer"}
			break
		}

		// 执行工具（对齐 Python: 整体错误时终止循环）
		results, err := a.executeToolCalls(ctx, cbc, aiMsg.ToolCalls, sess, modelCtx)
		if err != nil {
			logger.Error(logComponent).Str("event_type", "tool_execution_error").Int("iteration", iteration).Err(err).Msg("工具执行失败")
			return nil, fmt.Errorf("工具执行失败: %w", err)
		}

		// 强制完成 #2
		if finish := cbc.ConsumeForceFinish(); finish != nil {
			a.saveContexts(sess)
			iterResult = finish.Result
			break
		}

		// HITL 中断检测
		originalQuery := ""
		if oq, ok := cbc.Extra()["_original_query"].(string); ok {
			originalQuery = oq
		}
		hitlInterrupt, _ := a.AfterExecuteToolCallForHITL(
			results, aiMsg.ToolCalls, aiMsg, iteration, originalQuery,
		)
		if hitlInterrupt != nil {
			if invokeInputs, ok := cbc.Inputs().(*rail.InvokeInputs); ok {
				_, _ = a.CommitInterrupt(ctx, hitlInterrupt, modelCtx, sess, invokeInputs, nil)
			}
			break
		}
		// ⤵️ 6.11: Workflow 中断检测
	}

	if iterResult == nil {
		// 对齐 Python for-else: max iterations 时执行 save_contexts
		a.saveContexts(sess)
		iterResult = map[string]any{"output": "Max iterations reached without completion", "result_type": "error"}
	}

	if invokeInputs, ok := cbc.Inputs().(*rail.InvokeInputs); ok {
		invokeInputs.Result = iterResult
	}

	return iterResult, nil
}

// executeToolCalls 执行工具调用列表。
func (a *ReActAgent) executeToolCalls(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	modelCtx ceinterface.ModelContext,
) ([]ability.ExecuteResult, error) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	for _, tc := range toolCalls {
		argsPreview := tc.Arguments
		if len(argsPreview) > 100 {
			argsPreview = argsPreview[:100]
		}
		logger.Info(logComponent).
			Str("event_type", "tool_call").
			Str("tool_name", tc.Name).
			Str("args_preview", argsPreview).
			Msg("执行工具调用")
	}

	am := a.getAbilityManager()
	if am == nil {
		return nil, fmt.Errorf("AbilityManager 未初始化")
	}

	results := am.Execute(ctx, cbc, toolCalls, sess, "")

	for _, r := range results {
		if r.ToolMsg != nil && modelCtx != nil {
			_, _ = modelCtx.AddMessages(ctx, r.ToolMsg)
		}
	}

	// 对齐 Python L866-870: 多模态工具结果写入上下文
	multimodalMsg := a.buildMultimodalToolResultsMessage(results)
	if multimodalMsg != nil && modelCtx != nil {
		_, _ = modelCtx.AddMessages(ctx, multimodalMsg)
	}

	return results, nil
}

// buildMultimodalToolResultsMessage 从工具结果中提取多模态图片数据，
// 构建包含 image_url content blocks 的 UserMessage。
//
// 对应 Python: ReActAgent._build_multimodal_tool_results_message()
func (a *ReActAgent) buildMultimodalToolResultsMessage(results []ability.ExecuteResult) llmschema.BaseMessage {
	var parts []llmschema.ContentPart
	var loadedPaths []string

	for _, r := range results {
		for _, item := range iterMultimodalImageItems(r.Result) {
			sourcePath, _ := item["source_path"].(string)
			if sourcePath == "" {
				sourcePath = "unknown image"
			}
			dataURL, _ := item["data_url"].(string)
			loadedPaths = append(loadedPaths, sourcePath)
			parts = append(parts,
				llmschema.ContentPart{
					Type: "text",
					Text: fmt.Sprintf("Image loaded from read_file: %s", sourcePath),
				},
				llmschema.ContentPart{
					Type:     "image_url",
					ImageURL: &llmschema.ImageURL{URL: dataURL},
				},
			)
		}
	}

	if len(parts) == 0 {
		return nil
	}

	// 多张图片时，前置摘要
	if len(loadedPaths) > 1 {
		summaryLines := []string{"Images loaded by tool results:"}
		for i, path := range loadedPaths {
			summaryLines = append(summaryLines, fmt.Sprintf("%d. %s", i+1, path))
		}
		summary := llmschema.ContentPart{
			Type: "text",
			Text: strings.Join(summaryLines, "\n"),
		}
		parts = append([]llmschema.ContentPart{summary}, parts...)
	}

	msg := llmschema.NewUserMessage("", llmschema.WithMultiModalContent(parts...))
	return msg
}

// iterMultimodalImageItems 从工具结果中迭代多模态图片项。
//
// 对应 Python: ReActAgent._iter_multimodal_image_items()
func iterMultimodalImageItems(toolResult any) []map[string]any {
	resultMap, ok := toolResult.(map[string]any)
	if !ok {
		return nil
	}
	data, _ := resultMap["data"].(map[string]any)
	if data == nil {
		return nil
	}
	multimodalItems, _ := data["multimodal"].([]any)
	if multimodalItems == nil {
		return nil
	}

	var imageItems []map[string]any
	for _, itemAny := range multimodalItems {
		item, ok := itemAny.(map[string]any)
		if !ok {
			continue
		}
		if itemType, _ := item["type"].(string); itemType != "image" {
			continue
		}
		dataURL, _ := item["data_url"].(string)
		if !hasImagePrefix(dataURL) {
			continue
		}
		imageItems = append(imageItems, item)
	}
	return imageItems
}

// hasImagePrefix 检查 dataURL 是否以 data:image/ 开头。
func hasImagePrefix(dataURL string) bool {
	return len(dataURL) >= 11 && dataURL[:11] == "data:image/"
}
