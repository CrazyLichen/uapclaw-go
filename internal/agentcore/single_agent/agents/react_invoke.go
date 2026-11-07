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
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interrupt"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// emptyResponseMsg 空响应最终错误的用户可见文案
	// Python: EMPTY_RESPONSE_MSG (react_agent.py L125)
	emptyResponseMsg = "模型未返回有效内容（空响应），请重试或检查上下文。"
	// emptyResponseMaxRetries 空响应重试次数上限
	// Python: EMPTY_RESPONSE_MAX_RETRIES (react_agent.py L126)
	emptyResponseMaxRetries = 1
	// emptyResponseNotice 重试前注入的提示消息
	// Python: _EMPTY_RESPONSE_NOTICE (react_agent.py L127-130)
	emptyResponseNotice = "[EMPTY_RESPONSE] The previous turn had no content and no tool_calls. Reply with user-visible content or a tool call."
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Invoke 非流式调用，包含回调包装骨架。
// 执行顺序：① transform_io input → ② emit_before → ③ invokeImpl → ④ transform_io output → ⑤ emit_after
//
// Python: _AgentMeta 元类装饰后的 invoke
func (a *ReActAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (map[string]any, error) {
	fw := callback.GetCallbackFramework()
	agentOpts := interfaces.NewAgentOptions(opts...)

	// ① transform_io 输入变换（对齐 Python transform_io 的 input_fn）
	if transformed := fw.TransformAgentIOInput(ctx, callback.GlobalAgentInvokeInput, inputs); transformed != nil {
		if v, ok := transformed.(map[string]any); ok {
			inputs = v
		} else {
			logger.Warn(logComponent).
				Str("event", "TransformAgentIOInput").
				Str("agent_id", a.card.ID).
				Str("expected", "map[string]any").
				Str("actual", fmt.Sprintf("%T", transformed)).
				Msg("TransformIO returned mismatched type, using original input")
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
	// 注：取消时的上下文清理只在 invokeImpl 内部做一次（对齐 Python
	// react_agent.py:1435-1441 的 except CancelledError 分支），此处不再重复清理。
	result, err := a.invokeImpl(ctx, inputs, opts...)
	if err != nil {
		if _, ok := err.(*exception.BaseError); ok {
			return nil, err
		}
		logger.Error(logComponent).
			Str("agent_id", a.card.ID).
			Err(err).
			Msg("Agent invoke error")
		return nil, exception.NewBaseError(exception.StatusAgentControllerRuntimeError,
			exception.WithParam("error_msg", err.Error()),
			exception.WithCause(err),
		)
	}

	// ④ transform_io 输出变换（对齐 Python transform_io 的 output_fn）
	if transformed := fw.TransformAgentIOOutput(ctx, callback.GlobalAgentInvokeOutput, result); transformed != nil {
		if v, ok := transformed.(map[string]any); ok {
			result = v
		} else {
			logger.Warn(logComponent).
				Str("event", "TransformAgentIOOutput").
				Str("agent_id", a.card.ID).
				Str("expected", "map[string]any").
				Str("actual", fmt.Sprintf("%T", transformed)).
				Msg("TransformIO returned mismatched type, using original output")
		}
	}

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
// Python: _AgentMeta 元类装饰后的 stream
func (a *ReActAgent) Stream(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error) {
	fw := callback.GetCallbackFramework()
	agentOpts := interfaces.NewAgentOptions(opts...)

	// ① transform_io 输入变换（对齐 Python transform_io 的 input_fn）
	if transformed := fw.TransformAgentIOInput(ctx, callback.GlobalAgentStreamInput, inputs); transformed != nil {
		if v, ok := transformed.(map[string]any); ok {
			inputs = v
		} else {
			logger.Warn(logComponent).
				Str("event", "TransformAgentIOInput").
				Str("agent_id", a.card.ID).
				Str("expected", "map[string]any").
				Str("actual", fmt.Sprintf("%T", transformed)).
				Msg("TransformIO returned mismatched type, using original input")
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
		logger.Error(logComponent).
			Str("agent_id", a.card.ID).
			Err(err).
			Msg("Agent stream error")
		return nil, exception.NewBaseError(exception.StatusAgentControllerRuntimeError,
			exception.WithParam("error_msg", err.Error()),
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
					logger.Warn(logComponent).
						Str("event", "TransformAgentIOOutput").
						Str("agent_id", a.card.ID).
						Str("expected", "stream.Schema").
						Str("actual", fmt.Sprintf("%T", transformed)).
						Msg("TransformIO returned mismatched type, using original output")
				}
			}
			// ④ 发射后置事件 (逐项)
			fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
				Event:     callback.GlobalAgentStreamOutput,
				AgentID:   a.card.ID,
				AgentName: a.card.Name,
				Result:    item,
			})
			// M-49: select+ctx.Done 保护——消费者放弃读取 out 且取消 ctx 时
			// goroutine 不永久阻塞；drain ch 是替卡在 innerStream 发送侧的消费到底，
			// 让它跑到自己的 defer close(outCh) 责任点（等价 Python generator aclose 语义）
			select {
			case out <- item:
			case <-ctx.Done():
				for range ch {
				}
				return
			}
		}
	}()

	return out, nil
}

// AfterExecuteToolCallForHITL 执行工具后检测 HITL 中断。
//
// Python: ReActAgent._after_execute_tool_call_for_hitl()
func (a *ReActAgent) AfterExecuteToolCallForHITL(
	results []agentschema.ExecuteResult,
	toolCalls []*llmschema.ToolCall,
	aiMessage *llmschema.AssistantMessage,
	iteration int,
	originalQuery string,
) (*agentschema.ToolInterruptionState, []interrupt.PayloadEntry) {
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
// Python: ReActAgent._commit_interrupt() 的 HITL 分支
func (a *ReActAgent) CommitInterrupt(
	ctx context.Context,
	intState *agentschema.ToolInterruptionState,
	modelCtx ceinterface.ModelContext,
	sess sessioninterfaces.SessionFacade,
	invokeInputs *interfaces.InvokeInputs,
	subAgentOutputs []interrupt.PayloadEntry,
) (map[string]any, error) {
	if a.hitlHandler == nil {
		return nil, nil
	}
	return a.hitlHandler.CommitInterrupt(ctx, intState, modelCtx, sess, invokeInputs, subAgentOutputs)
}

// ClearContextMessages 清除当前上下文消息（保留历史）。
//
// Python: ReActAgent.clear_context_messages(with_history=False)
func (a *ReActAgent) ClearContextMessages(ctx context.Context, sess sessioninterfaces.SessionFacade) {
	if a.contextEngine == nil {
		return
	}
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
// Python: ReActAgent._write_invoke_result_to_stream()
func (a *ReActAgent) WriteInvokeResultToStream(
	ctx context.Context,
	result map[string]any,
	sess sessioninterfaces.SessionFacade,
) {
	// ctx 已取消时不再写任何结果帧：对齐 Python —— 取消后 generator 终止，
	// 既不会写 answer 也不会写 error，避免旧轮把"取消"当错误写进流与 history。
	// 该守卫同时覆盖 DeepAgent 的 writeRoundResultToStream 路径。
	if utils.Canceled(ctx) {
		logger.Info(logComponent).
			Str("event_type", "stream_result_skipped").
			Msg("skip writing invoke result: request canceled")
		return
	}

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
		// 待实现：提取组件ID componentIDs := result["component_ids"]
		// 待实现：获取待处理ID pendingID := componentIDs[0]
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

// extractUserParts 把本轮 query 归一成 ON_USER_MESSAGE 看到的输入列表。
//
// 一轮可能由多条排队输入共同驱动，调用方把它们不 join 地交下来，rails
// 才能看到接缝；其余情况下就是 query 本身这一个输入。
//
// Python: ReActAgent._extract_user_parts (react_agent.py L1002-1012)
func extractUserParts(cbc *interfaces.AgentCallbackContext, query interfaces.InvokeQuery) []string {
	if cbc != nil {
		if raw, ok := cbc.GetExtra("_input_parts"); ok {
			if parts, ok2 := raw.([]string); ok2 && len(parts) > 0 {
				return append([]string(nil), parts...)
			}
		}
	}
	if query == nil {
		return nil
	}
	return []string{query.PlainText()}
}

// admitUserMessage 把一批已消费的输入 join 成一条 UserMessage 写入历史。
//
// Agent 消费的每个输入——新一轮 query、忙碌时排队的 follow-up、一批
// steering、恢复的工作流中断——都从这里进入历史。ON_USER_MESSAGE 在
// join **之前**对列表触发，rails 因此仍能看到单条输入，把被后续条目取代的
// 整条丢掉。这是 rails 唯一能把它们当作**输入**处理的时刻：join 写入后
// 就是普通历史，受 compaction 影响，不能再按位置定位。
//
// Python: ReActAgent._admit_user_message (react_agent.py L917-955)
func (a *ReActAgent) admitUserMessage(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	modelCtx ceinterface.ModelContext,
	parts []string,
	source string,
	prefix string,
) error {
	prev := cbc.Inputs()
	inputs := &interfaces.UserMessageInputs{Parts: parts, Source: source}
	cbc.SetInputs(inputs)
	// 必须 defer：fire 里 panic 时 inputs 若停在 UserMessageInputs，
	// 后续 before_model_call 会拿到错误类型直接崩（Python 用 finally 等价）。
	defer cbc.SetInputs(prev)

	if err := cbc.Fire(ctx, interfaces.CallbackOnUserMessage); err != nil {
		return err
	}
	// Python: if not parts: return —— rails 可能把整批丢光
	if len(inputs.Parts) == 0 {
		return nil
	}
	if modelCtx == nil {
		return nil
	}
	// 附件先落历史，保证顺序为 [附件变更][用户消息]
	if syncPromptAttachments(ctx, a.promptAttachments, modelCtx) {
		runCommitCallbacks(cbc)
	}
	body := prefix + strings.Join(inputs.Parts, "\n")
	_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{llmschema.NewUserMessage(body)})
	return nil
}

// drainSteeringBatch 取本次模型调用可以吸收的那部分 steering 积压。
//
// 全部一次塞进同一轮对"两三条叮嘱"是对的，对"忙碌一段后回来看到十几条"
// 是灾难：模型被迫在一轮里同时处理所有事项。批大小因此是策略问题，由 rails
// 在**队列被取走之前**表态——事后再限制意味着先全取再把多余塞回去，而塞回去的
// 会排在期间新到的消息之后。
//
// 无队列或无积压时直接返回，不触发事件。
//
// Python: ReActAgent._drain_steering_batch (react_agent.py L964-1000)
func drainSteeringBatch(ctx context.Context, cbc *interfaces.AgentCallbackContext) ([]string, error) {
	if cbc == nil || cbc.SteeringQueue() == nil || !cbc.HasPendingSteering() {
		return nil, nil
	}
	prev := cbc.Inputs()
	inputs := &interfaces.SteeringDrainInputs{Pending: cbc.SteeringPendingCount()}
	cbc.SetInputs(inputs)
	defer cbc.SetInputs(prev)
	fireErr := cbc.Fire(ctx, interfaces.CallbackBeforeSteeringDrain)

	// Fire 出错仍要取走（否则积压永不消费、ReAct 循环空转），错误照抛。
	// spec D-4：上游 fire 不返回值，无此分支。
	return cbc.DrainSteering(inputs.Limit), fireErr
}

// shouldReportStreamError 判断 streamProcess 是否应把错误写入流。
//
// ctx 取消不是错误：对齐 Python react_agent.py:1549-1554 —— `except Exception`
// 抓不到 asyncio.CancelledError，因此取消时不会产生 chat.error 帧、也不会落 history。
func shouldReportStreamError(ctx context.Context, err error) bool {
	return err != nil && !utils.Canceled(ctx)
}

// invokeImpl 非流式调用的真实逻辑。
//
// Python: ReActAgent.invoke()
func (a *ReActAgent) invokeImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (map[string]any, error) {
	agentOpts := interfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session

	// 先提取 conversationID，自建 session 时用作 sessionID
	// Python: session_id = conversation_id or "default_session"
	conversationID, _ := inputs["conversation_id"].(string)

	needCleanup := false // Python: need_cleanup，仅在自建 session 时为 true
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
	// Python: 外部传入 session 时，不调 pre_run（由调用方负责）

	invokeQuery := interfaces.QueryFromInputs(inputs)
	invokeInputs := &interfaces.InvokeInputs{
		Query:          invokeQuery,
		ConversationID: sess.GetSessionID(),
	}
	cbc := interfaces.NewAgentCallbackContext(a, invokeInputs, sess)

	// 设置 extra（对齐 Python L1289-1296）
	if userID, ok := inputs["user_id"].(string); ok {
		cbc.SetExtra("user_id", userID)
	}
	if runKind, ok := inputs["run_kind"].(string); ok {
		cbc.SetExtra("run_kind", runKind)
	}
	if runContext, ok := inputs["run_context"].(string); ok {
		cbc.SetExtra("run_context", runContext)
	}
	if streaming, ok := inputs["_streaming"].(bool); ok {
		cbc.SetExtra("_streaming", streaming)
	} else {
		cbc.SetExtra("_streaming", false)
	}
	if sq, ok := inputs["_steering_queue"]; ok {
		if ch, ok2 := sq.(chan string); ok2 {
			cbc.BindSteeringQueue(ch)
		}
	}

	// abortPersisted 标记中断路径是否已完成「清洗 + 持久化 + commit」，
	// 避免 finally 里重复 save/commit（对齐 Python abort_persisted）。
	abortPersisted := false
	// streamLifecycleOwner 为真时，上下文清洗与 finally 清理都归 stream 侧所有，
	// 本次 invoke 一律不碰（上游 _stream_lifecycle_owner 语义）。
	streamLifecycleOwner := agentOpts.StreamLifecycleOwner

	// Python: try/finally: cleanup 始终执行（无论 FireLifecycle 是否返回错误）
	if needCleanup && !streamLifecycleOwner {
		defer func() {
			if !abortPersisted {
				// 取消/异常路径同样走脱离取消的 ctx，否则写库失败、本轮上下文丢失
				a.saveContextsDetached(ctx, sess)
			}
			// Python: session.close_stream() + session.commit()
			if as, ok := sess.(*session.Session); ok {
				_ = as.CloseStream()
				if !abortPersisted {
					_ = as.Commit(context.WithoutCancel(ctx))
				}
			}
		}()
	}

	var result map[string]any
	var loopErr error

	err := cbc.FireLifecycle(ctx, interfaces.CallbackBeforeInvoke, interfaces.CallbackAfterInvoke, func() error {
		// 从 cbc 重新取 inputs（对齐 Python: user_input = ctx.inputs.query）
		// before_invoke 钩子可能修改 cbc.inputs，必须从 cbc 重新取值
		curInputs := invokeInputs
		if ci, ok := cbc.Inputs().(*interfaces.InvokeInputs); ok && ci != nil {
			curInputs = ci
		}

		// Python: L1301-1302: 空 query 校验
		// InteractiveInput（中断恢复）不校验 PlainText 是否为空
		if curInputs.Query.PlainText() == "" && !curInputs.Query.IsInteractiveInput() {
			return fmt.Errorf("输入必须包含 'query'")
		}

		// 加载 HITL 中断状态
		hitlState := a.hitlHandler.Load(sess)
		var interruptionState agentschema.ToolInterruptionState
		if hitlState != nil {
			a.hitlHandler.Clear(sess)
			interruptionState = *hitlState
		}
		// ⤵️ Workflow: 中断状态加载（暂未实现）
		// Python: interruptionState = a.loadInterruptionState(sess)

		// 如果存在中断状态，恢复原始 query
		if hitlState != nil {
			cbc.SetExtra("_original_query", hitlState.OriginalQuery)
		}

		// 初始化上下文
		modelCtx, ctxErr := a.initContext(ctx, sess)
		if ctxErr != nil {
			return fmt.Errorf("初始化上下文失败: %w", ctxErr)
		}
		cbc.SetModelContext(modelCtx)

		// Python: L1317-1326: 在 invoke 入口构建 system prompt
		// 用渲染后的 prompt 更新 identity section（替换 Configure 时的模板名）。
		//
		// 渲染源必须是 config.prompt_template（固定模板），不能是 promptBuilder：
		// Python 用 _build_rendered_system_prompt(self._config.prompt_template)
		// （react_agent.py L582-601）；若从 builder 渲染，上一轮写回 identity 的
		// 全文会被再次包含，逐轮自我嵌套膨胀（2026-11-03：模式切换后 identity
		// 3990→10921、系统提示词整段重复即此因）。
		renderedPrompt := a.buildRenderedSystemPrompt()
		if renderedPrompt != "" {
			a.AddPromptBuilderSection(identitySection, renderedPrompt, identitySectionPriority)
		}
		// ⤴️ Skill: 更新技能提示词区段
		a.updateSkillPromptBuilderSection(ctx)

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
			if si, ok := cbc.DeleteExtra(interrupt.ResumeStartIterationKey); ok {
				if siInt, ok := si.(int); ok {
					startIteration = siInt
				}
			}
		} else {
			// 正常路径：把本轮输入批次写入历史
			// Python: _admit_user_message(parts, source="query") (react_agent.py L2990-3000)
			plainText := curInputs.Query.PlainText()
			if plainText != "" && modelCtx != nil {
				parts := extractUserParts(cbc, curInputs.Query)
				if err := a.admitUserMessage(ctx, cbc, modelCtx, parts, "query", ""); err != nil {
					return err
				}
			}
		}

		// 调用 ReAct 循环（initContext 和 UserMessage 已在上方完成）
		// S-55: 对齐 Python L1353——守卫读原始 invoke_inputs（rail 替换前对象），
		// 结构上消灭 (nil, nil)：守卫 true → reactLoop 成功恒非 nil；守卫 false → 原始对象 Result 非 nil
		if invokeInputs.Result == nil {
			result, loopErr = a.reactLoop(ctx, cbc, sess, modelCtx, startIteration)
		}
		return loopErr
	})

	// 中断/异常路径：清洗本轮上下文（保留用户提问与完整 tool 对、丢弃半截 tool 块、
	// 结尾补 marker）后持久化，使下一轮同 session 能从本轮继续而不是回退到旧检查点。
	//
	// Python: react_agent.py L3263-3281 —— CancelledError 与 Exception 各用一个 marker。
	if err != nil {
		if streamLifecycleOwner {
			// 生命周期归 stream：不在这里清洗/持久化，由 stream 侧统一处理
			return nil, err
		}
		if utils.Canceled(ctx) {
			abortPersisted = a.handleContextAbort(ctx, sess, "[Request cancelled by user]")
		} else {
			abortPersisted = a.handleContextAbort(ctx, sess, "[Request interrupted by an unexpected error]")
		}
		return nil, err
	}

	// Python: L1434: return ctx.extra.get("invoke_result", invoke_inputs.result)
	if invokeResult, ok := cbc.GetExtra("invoke_result"); ok {
		if r, ok2 := invokeResult.(map[string]any); ok2 {
			return r, nil
		}
	}

	// 从 cbc 取最终结果（对齐 Python: invoke_inputs.result）
	if curInputs, ok := cbc.Inputs().(*interfaces.InvokeInputs); ok && curInputs.Result != nil {
		return curInputs.Result, nil
	}
	if invokeInputs.Result != nil {
		return invokeInputs.Result, nil
	}
	return result, nil
}

// streamImpl 流式调用的真实逻辑。
//
// Python: ReActAgent.stream()
func (a *ReActAgent) streamImpl(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (<-chan stream.Schema, error) {
	agentOpts := interfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session

	// 先提取 conversationID，自建 session 时用作 sessionID
	// Python: session_id = conversation_id or "default_session"
	conversationID, _ := inputs["conversation_id"].(string)

	needCleanup := false // Python: need_cleanup，仅在自建 session 时为 true
	if sess == nil {
		sessionID := conversationID
		if sessionID == "" {
			sessionID = "default_session"
		}
		// Python: stream_modes 传入 StreamWriterManager
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
	// Python: 外部传入 session 时，不调 pre_run（由调用方负责）

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
// Python: ReActAgent._inner_stream()
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
		// abortPersisted 记录中断/异常路径是否已完成清洗+持久化+commit，
		// 置真后 finally 不再重复 save/commit（对齐 Python abort_persisted）。
		abortPersisted := false
		defer func() {
			// finally: 清理（对齐 Python L1555-1560）
			if needCleanup && !abortPersisted {
				// 消费者中途断开时 ctx 已取消，必须脱离取消才能写库
				a.saveContextsDetached(ctx, sess)
			}
			// Python: if self.is_agent_session: session.close_stream() + session.commit()
			if isAgentSess {
				_ = agentSess.CloseStream()
				if !abortPersisted {
					_ = agentSess.Commit(context.WithoutCancel(ctx))
				}
			}
		}()

		// 捕获 panic 防止 goroutine 崩溃
		defer func() {
			if r := recover(); r != nil {
				logger.Error(logComponent).Any("panic", r).Msg("streamProcess panic occurred")
				a.WriteInvokeResultToStream(ctx, map[string]any{
					"output":      fmt.Sprintf("panic: %v", r),
					"result_type": "error",
				}, sess)
			}
		}()

		// 走完整 Invoke（对齐 Python: self.invoke(..., _stream_lifecycle_owner=True)）
		// 生命周期归 stream：invoke 侧不再清洗/持久化，由这里统一处理。
		invokeOpts := append(append([]interfaces.AgentOption{}, opts...), interfaces.WithStreamLifecycleOwner())
		result, err := a.Invoke(ctx, inputs, invokeOpts...)
		if err != nil {
			// ctx 取消不是错误：不写错误帧、不落 history
			// （对齐 Python react_agent.py:1549-1554 —— except Exception 抓不到 CancelledError）
			if !shouldReportStreamError(ctx, err) {
				// Python: except CancelledError → marker=[Request cancelled by user]
				abortPersisted = a.handleContextAbort(ctx, sess, "[Request cancelled by user]")
				logger.Info(logComponent).
					Str("event_type", "stream_canceled").
					Msg("stream invoke canceled by caller")
				return
			}
			// Python: except BaseError / except Exception → marker=[Request interrupted by an unexpected error]
			abortPersisted = a.handleContextAbort(ctx, sess, "[Request interrupted by an unexpected error]")
			// 错误结果写入流
			logger.Error(logComponent).Err(err).Str("event_type", "LLM_CALL_ERROR").Msg("streamProcess invoke error")
			a.WriteInvokeResultToStream(ctx, map[string]any{
				"output":      err.Error(),
				"result_type": "error",
			}, sess)
			return
		}

		// 正常结果写入流
		a.WriteInvokeResultToStream(ctx, result, sess)
	}

	if isAgentSess {
		// Agent 会话：启动 streamProcess 协程，从 StreamIterator 消费
		go streamProcess()

		for chunk := range agentSess.StreamIterator() {
			// M-49: select+ctx.Done 保护——outCh（缓冲 64）满且消费者放弃时
			// 不永久阻塞；drain StreamIterator 让 streamProcess 侧与
			// defer close(outCh)（streamImpl）正常收敛（等价 Python generator aclose 语义）
			select {
			case outCh <- chunk:
			case <-ctx.Done():
				for range agentSess.StreamIterator() {
				}
				return
			}
		}
	} else {
		// 工作流会话：直接执行 streamProcess
		// 输出通过 session.WriteStream → StreamWriterManager 传递给 Workflow
		streamProcess()
	}
}

// reactLoop ReAct 循环核心。
//
// Python: ReActAgent._inner_invoke() 中的主循环
// 注意：initContext 和 UserMessage 已在 invokeImpl 中完成，此处不再重复。
func (a *ReActAgent) reactLoop(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	sess sessioninterfaces.SessionFacade,
	modelCtx ceinterface.ModelContext,
	startIteration int,
) (map[string]any, error) {
	// 保存原始 invokeInputs 引用。
	// callModel 中 cbc.SetInputs(&ModelCallInputs{}) 会覆盖 cbc.inputs，
	// 循环内 HITL 中断检测需要原始 invokeInputs 来设置 Result。
	var savedInvokeInputs *interfaces.InvokeInputs
	if ii, ok := cbc.Inputs().(*interfaces.InvokeInputs); ok {
		savedInvokeInputs = ii
	}

	maxIter := defaultMaxIterations
	if a.config != nil && a.config.MaxIterations > 0 {
		maxIter = a.config.MaxIterations
	}

	// S-54: 对齐 Python react_agent.py L1328——get_tools 异常直接传播导致 invoke 失败
	//（fail-fast，不降级为空工具列表继续执行）
	tools, err := a.getTools(ctx)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Msg("获取工具列表失败")
		return nil, fmt.Errorf("获取工具列表失败: %w", err)
	}

	var iterResult map[string]any
	// emptyResponseRetries 空回答重试计数（循环级，跨轮累加而非每轮重置）
	emptyResponseRetries := 0
	for iteration := startIteration; iteration < maxIter; iteration++ {
		// Python: L1355: 迭代计数日志
		logger.Info(logComponent).
			Int("iteration", iteration+1).
			Int("max_iterations", maxIter).
			Msg("ReAct iteration")

		// 轮首消费：上一轮 after_react_iteration 钩子请求的 force_finish。
		// Python: react_agent.py L3100-3108 —— 让优雅中止在下一轮轮首生效，
		// 而不是先白跑一次模型调用才被消费。
		if finish := cbc.ConsumeForceFinish(); finish != nil {
			a.saveContexts(ctx, sess)
			iterResult = finish.Result
			break
		}

		// steering 注入：rails 决定本次吸收几条，其余留队等下一次模型调用。
		// Python: react_agent.py L3111-3119
		if modelCtx != nil {
			steeringMsgs, drainErr := drainSteeringBatch(ctx, cbc)
			if drainErr != nil {
				return nil, fmt.Errorf("迭代 %d steering 批次获取失败: %w", iteration, drainErr)
			}
			if len(steeringMsgs) > 0 {
				if syncPromptAttachments(ctx, a.promptAttachments, modelCtx) {
					runCommitCallbacks(cbc)
				}
				for _, msg := range steeringMsgs {
					_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{llmschema.NewUserMessage("[STEERING] " + msg)})
				}
			}
		}

		// 调用 LLM
		aiMsg, err := a.callModel(ctx, cbc, modelCtx, tools, sess)
		if err != nil {
			return nil, fmt.Errorf("迭代 %d 模型调用失败: %w", iteration, err)
		}

		// 强制完成 #1
		if finish := cbc.ConsumeForceFinish(); finish != nil {
			a.saveContexts(ctx, sess)
			iterResult = finish.Result
			break
		}

		if aiMsg != nil && modelCtx != nil {
			_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{aiMsg})
		}

		// 无工具调用
		if aiMsg == nil || len(aiMsg.ToolCalls) == 0 {
			if cbc.HasPendingSteering() {
				continue
			}
			// 视觉降级：上一轮仅因"不支持图片输入"失败时，把提示留在上下文里继续下一轮，
			// 让模型看到错误并尝试文本替代方案（不是 break）。
			// Python: react_agent.py L3160-3166
			if v, ok := cbc.GetExtra("_last_image_unsupported"); ok {
				if flagged, isBool := v.(bool); isBool && flagged {
					cbc.SetExtra("_last_image_unsupported", false)
					a.saveContexts(ctx, sess)
					continue
				}
			}

			content := ""
			finishReason := ""
			reasoningPresent := false
			if aiMsg != nil {
				content = aiMsg.Content.Text()
				finishReason = aiMsg.FinishReason
				reasoningPresent = aiMsg.ReasoningContent != ""
			}

			// 空回答（无正文且无 tool_calls）：重试一次把提示塞进上下文；仍空则判失败。
			// 上游旧行为会把空响应当作成功的 "answer"，导致本轮以空答案结束。
			// Python: react_agent.py L3167-3196
			if strings.TrimSpace(content) == "" {
				if emptyResponseRetries < emptyResponseMaxRetries && iteration+1 < maxIter {
					emptyResponseRetries++
					if modelCtx != nil {
						_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{
							llmschema.NewUserMessage(emptyResponseNotice, llmschema.WithMetadata(map[string]any{
								llmschema.MetadataKeyMessageSourceKind: "empty_response",
							})),
						})
					}
					logger.Info(logComponent).
						Int("retry", emptyResponseRetries).
						Int("max_retries", emptyResponseMaxRetries).
						Int("iteration", iteration+1).
						Bool("reasoning_present", reasoningPresent).
						Msg("empty no-tool turn, retrying")
					continue
				}
				a.saveContexts(ctx, sess)
				shownFinishReason := finishReason
				if shownFinishReason == "" {
					shownFinishReason = "null"
				}
				iterResult = map[string]any{
					"output":            emptyResponseMsg,
					"result_type":       "error",
					"finish_reason":     shownFinishReason,
					"reasoning_present": reasoningPresent,
				}
				break
			}

			a.saveContexts(ctx, sess)
			iterResult = map[string]any{"output": content, "result_type": "answer"}
			break
		}

		// 执行工具（对齐 Python: 整体错误时终止循环）
		results, err := a.executeToolCalls(ctx, cbc, aiMsg.ToolCalls, sess, modelCtx)
		if err != nil {
			logger.Error(logComponent).Str("event_type", "tool_execution_error").Int("iteration", iteration).Err(err).Msg("toolexecution failed")
			return nil, fmt.Errorf("工具执行失败: %w", err)
		}

		// 强制完成 #2
		if finish := cbc.ConsumeForceFinish(); finish != nil {
			a.saveContexts(ctx, sess)
			iterResult = finish.Result
			break
		}

		// HITL 中断检测
		originalQuery := ""
		if val, ok := cbc.GetExtra("_original_query"); ok {
			if oq, ok := val.(string); ok {
				originalQuery = oq
			}
		}
		hitlInterrupt, subAgentOutputs := a.AfterExecuteToolCallForHITL(
			results, aiMsg.ToolCalls, aiMsg, iteration, originalQuery,
		)
		if hitlInterrupt != nil {
			// 使用循环开始时保存的 savedInvokeInputs，而非 cbc.Inputs()。
			// callModel 中 cbc.SetInputs(&ModelCallInputs{}) 会临时覆盖 cbc.inputs，
			// 虽已修复为恢复，但仍使用 savedInvokeInputs 更安全。
			if savedInvokeInputs != nil {
				commitResult, commitErr := a.CommitInterrupt(ctx, hitlInterrupt, modelCtx, sess, savedInvokeInputs, subAgentOutputs)
				if commitErr == nil && commitResult != nil {
					iterResult = commitResult
				}
			}
			break
		}
		// ⤵️ 6.11: Workflow 中断检测

		// 单轮完全成功（模型响应 + 全部工具结果 + ToolMessage 均已写入）后触发。
		//
		// 触发范围与上游严格一致：只有完整工具轮触发一次（一个迭代内 N 个
		// tool_call 也只 fire 一次），任何 break path（答案轮、force-finish、
		// HITL、workflow 中断）都不触发。上游 rail/base.py 事件枚举 docstring
		// 明确写 "Only fires on fully successful iterations, not on any break
		// path"；react_agent.py L3239-3241 的触发行也落在 tool_calls 分支内，
		// 无工具调用的答案分支在其之前就 break 了。
		//
		// 语义理由：消费者（如 SnapshotRail）要的是「可回滚的安全状态」——上下文
		// 尾部是完整的 assistant(tool_calls) + ToolMessage 对，快照不会出现
		// tool_calls 悬空；答案轮结束即整次 invoke 结束，由 after_invoke 覆盖。
		// 该钩子同时是 boundary force_finish 的来源，故放在轮末而非轮首。
		//
		// Python: react_agent.py L3239-3241
		if fireErr := cbc.Fire(ctx, interfaces.CallbackAfterReactIteration); fireErr != nil {
			logger.Warn(logComponent).
				Str("event", string(interfaces.CallbackAfterReactIteration)).
				Err(fireErr).
				Msg("after_react_iteration callback errored")
		}
	}

	if iterResult == nil {
		// Python: for-else（react_agent.py L3243-3252）——预算耗尽时先消费最后一轮
		// after_react_iteration 设置的 force_finish（如优雅中止），否则落 max_iterations 错误。
		// Python: await self.context_engine.save_contexts(session)
		a.saveContexts(ctx, sess)
		if finish := cbc.ConsumeForceFinish(); finish != nil {
			iterResult = finish.Result
		} else {
			iterResult = map[string]any{
				"output":                      "Max iterations reached without completion",
				"result_type":                 "error",
				"termination_reason":          "max_iterations",
				"iterations_used":             maxIter,
				"truncated_by_max_iterations": true,
				"max_iterations":              maxIter,
				"finalization_attempted":      false,
				"finalization_succeeded":      false,
			}
			logger.Warn(logComponent).
				Int("iterations_used", maxIter).
				Int("max_iterations", maxIter).
				Str("strategy", "error").
				Msg("ReAct iteration limit reached")
		}
	}

	if invokeInputs, ok := cbc.Inputs().(*interfaces.InvokeInputs); ok {
		invokeInputs.Result = iterResult
	}

	return iterResult, nil
}

// buildRenderedSystemPrompt 从配置的 PromptTemplate 渲染系统提示词（固定源）。
// Python: _build_rendered_system_prompt (react_agent.py L582-601)。
// 只取 role=system 且内容非空的消息，按序拼接——渲染源绝不能用
// promptBuilder，否则上一轮写回 identity 的全文会被再次包含，逐轮自我嵌套。
func (a *ReActAgent) buildRenderedSystemPrompt() string {
	cfg := a.config
	if cfg == nil {
		return ""
	}
	parts := make([]string, 0, len(cfg.PromptTemplate))
	for _, msg := range cfg.PromptTemplate {
		role, _ := msg["role"].(string)
		if role != "system" {
			continue
		}
		content, _ := msg["content"].(string)
		if content != "" {
			parts = append(parts, content)
		}
	}
	return strings.Join(parts, "\n\n")
}

// executeToolCalls 执行工具调用列表。
func (a *ReActAgent) executeToolCalls(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	toolCalls []*llmschema.ToolCall,
	sess sessioninterfaces.SessionFacade,
	modelCtx ceinterface.ModelContext,
) ([]agentschema.ExecuteResult, error) {
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
			Msg("executing tool call")
	}

	am := a.getAbilityManager()
	if am == nil {
		return nil, fmt.Errorf("AbilityManager 未初始化")
	}

	results := am.Execute(ctx, cbc, toolCalls, sess, "", a.parallelToolCalls())

	for _, r := range results {
		if r.ToolMsg != nil && modelCtx != nil {
			_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{r.ToolMsg})
		}
	}

	// Python: L866-870: 多模态工具结果写入上下文
	multimodalMsg := a.buildMultimodalToolResultsMessage(results)
	if multimodalMsg != nil && modelCtx != nil {
		_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{multimodalMsg})
	}

	return results, nil
}

// buildMultimodalToolResultsMessage 从工具结果中提取多模态图片数据，
// 构建包含 image_url content blocks 的 UserMessage。
//
// Python: ReActAgent._build_multimodal_tool_results_message()
func (a *ReActAgent) buildMultimodalToolResultsMessage(results []agentschema.ExecuteResult) llmschema.BaseMessage {
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
// Python: ReActAgent._iter_multimodal_image_items()
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

// sanitizeAbortedTurnMessages 返回被中断轮次的 LLM 安全前缀。
//
// 规则：保留用户消息与完整的 assistant/tool 对；丢弃未完成的 tool_call 块（assistant +
// 半截 tool 消息一起丢）与孤立 ToolMessage；结尾若为 UserMessage 则补一条 marker 形式的
// AssistantMessage，避免出现「只有提问没有回复」的非法尾。
//
// Python: ReActAgent._sanitize_cancelled_turn_messages() (react_agent.py L3721-3788)
func sanitizeAbortedTurnMessages(messages []llmschema.BaseMessage, marker string) []llmschema.BaseMessage {
	kept := make([]llmschema.BaseMessage, 0, len(messages))
	for i := 0; i < len(messages); {
		msg := messages[i]
		if am, ok := msg.(*llmschema.AssistantMessage); ok && len(am.ToolCalls) > 0 {
			needed := make(map[string]bool, len(am.ToolCalls))
			for _, tc := range am.ToolCalls {
				if tc != nil && tc.ID != "" {
					needed[tc.ID] = true
				}
			}
			found := make(map[string]*llmschema.ToolMessage, len(needed))
			j := i + 1
			for ; j < len(messages); j++ {
				tm, isTool := messages[j].(*llmschema.ToolMessage)
				if !isTool {
					break
				}
				found[tm.ToolCallID] = tm
			}
			complete := len(needed) > 0
			for id := range needed {
				if found[id] == nil {
					complete = false
					break
				}
			}
			if complete {
				kept = append(kept, am)
				for _, tc := range am.ToolCalls {
					if tc != nil && found[tc.ID] != nil {
						kept = append(kept, found[tc.ID])
					}
				}
			}
			i = j
			continue
		}
		if _, isTool := msg.(*llmschema.ToolMessage); isTool {
			// 孤立 tool 结果（前面没有对应 tool_calls）：丢弃
			i++
			continue
		}
		kept = append(kept, msg)
		i++
	}
	if n := len(kept); n > 0 {
		if _, isUser := kept[n-1].(*llmschema.UserMessage); isUser {
			kept = append(kept, llmschema.NewAssistantMessage(marker))
		}
	}
	return kept
}

// handleContextAbort 取消/异常后清洗并持久化本轮上下文。
//
// 返回 true 表示已持久化（调用方据此抑制 finally 里的重复 save/commit）。
// 外部传入的 agent session 也要 commit：正常路径由调用方 commit，但异常路径走不到。
//
// Python: ReActAgent._handle_context_abort() (react_agent.py L3593-3624)
func (a *ReActAgent) handleContextAbort(
	ctx context.Context,
	sess sessioninterfaces.SessionFacade,
	marker string,
) bool {
	if a.contextEngine == nil || sess == nil {
		return false
	}
	sessionID := sess.GetSessionID()
	mc := a.contextEngine.GetContext("default_context", sessionID)
	if mc != nil {
		if current, readErr := mc.GetMessages(0, false); readErr == nil && len(current) > 0 {
			mc.SetMessages(sanitizeAbortedTurnMessages(current, marker), false)
		}
	}
	// 落盘用脱离取消的 ctx：调用方取消后仍需把清洗结果写进 session，
	// 否则下一轮 create_context 会用取消前的旧快照重建（对齐 Python asyncio.shield）。
	a.saveContextsDetached(ctx, sess)
	if as, ok := sess.(*session.Session); ok {
		if commitErr := as.Commit(context.WithoutCancel(ctx)); commitErr != nil {
			logger.Warn(logComponent).
				Str("event_type", "context_abort_commit_failed").
				Err(commitErr).
				Msg("commit session after abort failed")
			return false
		}
	}
	return true
}
