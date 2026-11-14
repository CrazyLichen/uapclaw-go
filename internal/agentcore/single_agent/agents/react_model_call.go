package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/support"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/utils"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// streamDrainTimeout 流异常路径排空内层通道的最长等待（同 llm 包，兜底用）。
var streamDrainTimeout = 2 * time.Second

// ──────────────────────────── 非导出函数 ────────────────────────────

// callModel 调用 LLM 模型（经 Rail 钩子包装）。
// Python: ReActAgent._call_model()
func (a *ReActAgent) callModel(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	modelCtx ceinterface.ModelContext,
	tools []cschema.ToolInfoInterface,
	sess sessioninterfaces.SessionFacade,
) (*llmschema.AssistantMessage, error) {
	// Python: L619-625: preview messages 包含 system prompt 前缀
	previewMsgs := make([]llmschema.BaseMessage, 0)
	previewPrompt := a.promptBuilder.Build()
	if previewPrompt != "" {
		previewMsgs = append(previewMsgs, llmschema.NewSystemMessage(previewPrompt))
	}
	if modelCtx != nil {
		msgs, _ := modelCtx.GetMessages(0, true)
		previewMsgs = append(previewMsgs, msgs...)
	}
	// Python: L648-652: ctx.inputs = ModelCallInputs(messages=..., tools=..., model_context=...)
	// 保存当前 inputs，Rail 执行后恢复（对齐 Python: model_call rail 内部修改 ctx.inputs，
	// 但退出 rail 后 ctx.inputs 应恢复为调用前的类型，否则 reactLoop 中
	// cbc.Inputs().(*InvokeInputs) 类型断言会失败）
	savedInputs := cbc.Inputs()
	modelCallInputs := &interfaces.ModelCallInputs{
		Messages:     previewMsgs,
		Tools:        tools,
		ModelContext: modelCtx,
		// Python: react_agent.py:1043 react_iteration=int(ctx.extra.get("_react_iteration", 0) or 0)
		ReactIteration: extraInt(cbc, "_react_iteration"),
	}
	cbc.SetInputs(modelCallInputs)

	var result *llmschema.AssistantMessage
	// 归属随会话元数据变化（团队/成员可能在轮内被改写），每次模型调用前重新解析一次，
	// 并同时挂到 ctx 与本次模型调用输入上（后者是上报侧读取面）。
	// Python: _report_context_usage 里重解析并回写 ctx.context_usage_attribution
	//         与 ctx.inputs.context_usage_attribution（react_agent.py L1477-1483）
	usageAttribution := a.resolveContextUsageAttribution(cbc)
	cbc.SetContextUsageAttribution(usageAttribution)
	modelCallInputs.ContextUsageAttribution = usageAttribution
	// 同步刷新 ctx 上的绑定盒：同一条 ctx 链上的子代理委派能读到完整 14 键。
	// Python: ctx.context_usage_attribution.clear() + update(attribution)（react_agent.py L1478-1479）
	rail.UpdateUsageAttribution(ctx, usageAttribution)
	err := rail.ModelCallRail.Execute(ctx, cbc, func() error {
		var e error
		result, e = a.railedModelCall(ctx, cbc, sess)
		return e
	})

	// 恢复 inputs，确保 reactLoop 中 cbc.Inputs() 仍为 *InvokeInputs
	cbc.SetInputs(savedInputs)

	// Python: L659: log_llm_response
	if result != nil {
		logLLMResponse(result)
	}

	return result, err
}

// railedModelCall 在 Rail 钩子内执行 LLM 调用。
func (a *ReActAgent) railedModelCall(ctx context.Context, cbc *interfaces.AgentCallbackContext, sess sessioninterfaces.SessionFacade) (*llmschema.AssistantMessage, error) {
	systemPrompt := a.promptBuilder.Build()
	systemMsgs := []llmschema.BaseMessage{llmschema.NewSystemMessage(systemPrompt)}

	modelCtx := cbc.ModelContext()

	// KV Cache 释放逻辑（对应 Python _railed_model_call L686-720）
	var enableKVRelease bool
	if a.config != nil {
		enableKVRelease = a.config.ContextEngineConfig.EnableKVCacheRelease
	}

	// 提前获取 LLM 实例（KV Cache 检查和 GetContextWindow 都需要）
	llmModel, llmErr := a.getLLM()
	if llmErr != nil {
		return nil, llmErr
	}
	if llmModel == nil {
		return nil, fmt.Errorf("LLM 实例为 nil")
	}

	supportsKVRelease := llmModel.SupportsKVCacheRelease()

	// 不支持时一次性警告
	if enableKVRelease && !supportsKVRelease && !a.kvReleaseWarningLogged {
		logger.Warn(logComponent).
			Str("event_type", "kv_cache_release_not_supported").
			Msg("enable_kv_cache_release enabled but current LLM does not support KV Cache release")
		a.kvReleaseWarningLogged = true
	}

	// 构建 GetContextWindow 选项：支持 KV Cache 时传入 model
	var contextWindowOpts []ceinterface.Option
	if enableKVRelease && supportsKVRelease {
		contextWindowOpts = append(contextWindowOpts, ceinterface.WithModel(llmModel))
	}
	// 附件投影：仅当注入了 manager 时生效（对齐 Python _build_model_context_window 的 window_mutators）
	contextWindowOpts = append(contextWindowOpts, attachmentWindowOptions(a.promptAttachments, llmModel, sess.GetSessionID())...)

	var messages []llmschema.BaseMessage
	var contextTools []cschema.ToolInfoInterface

	// buildWindow 拉取本轮上下文窗口；压缩后重试时也用它重建（消息已变）。
	buildWindow := func() error {
		if modelCtx == nil {
			messages = systemMsgs
			// modelCtx 为 nil 时直接沿用 callModel 预置的 tools（对齐
			// Python：ctx.inputs.tools 仍会传给 LLM 调用）
			if inputs, ok := cbc.Inputs().(*interfaces.ModelCallInputs); ok {
				contextTools = inputs.Tools
			}
			return nil
		}
		// Python: tools=ctx.inputs.tools if ctx.inputs.tools else None
		// （_railed_model_call L715-717）——从 ModelCallInputs 读取
		// callModel 预置的 tools，而非硬编码 nil（此前 Go 传 nil 导致
		// 工具列表整体丢失，模型调用请求不含任何 tools）
		var inputTools []cschema.ToolInfoInterface
		if inputs, ok := cbc.Inputs().(*interfaces.ModelCallInputs); ok {
			inputTools = inputs.Tools
		}
		// 附件同步：保证本轮模型看到最新动态提示词（对齐 Python _build_model_context_window）
		// Sync 成功才执行投递回调（25.0a ①）
		if syncPromptAttachments(ctx, a.promptAttachments, modelCtx) {
			runCommitCallbacks(cbc)
		}
		contextWindow, err := modelCtx.GetContextWindow(ctx, systemMsgs, inputTools, 0, 0, contextWindowOpts...)
		if err != nil {
			return fmt.Errorf("获取上下文窗口失败: %w", err)
		}
		messages = contextWindow.GetMessages()
		contextTools = contextWindow.GetTools()
		return nil
	}
	if err := buildWindow(); err != nil {
		return nil, err
	}

	// 写回 cbc.Inputs()（对齐 Python L727-728: ctx.inputs.messages = ...; ctx.inputs.tools = ...）
	if inputs, ok := cbc.Inputs().(*interfaces.ModelCallInputs); ok {
		inputs.Messages = messages
		inputs.Tools = contextTools
	}

	// Python: L730: log_llm_request
	logLLMRequest(messages, contextTools)

	// 构建 KV Cache extra kwargs（对齐 react_agent.py:1742-1830）。
	// ① ResolveRuntime ② ResolveLineage ③ ResolveTurnNum ④ BuildInvokeKwargs。
	// 第④步（Python 的 handle_context_window_change → _evict_changed_window →
	// llm.evict_kvc）在 Go 侧由既有 KVCacheManager.Release 在
	// SessionModelContext.GetContextWindow（session_model_context.go:592）中完成，
	// 故此处不重复调用（spec §8 DEV-2）。
	// ★ 与 recommendation 包的 _main_call_parameters 共用同一 hook 实例与同一份
	//   参数，保证父请求与重放请求逐字一致（prompt cache 命中的唯一保障）。
	kvHook := a.kvCacheModelCallHook()
	kvRuntime := kvHook.ResolveRuntime(llmModel, a.kvCacheAffinityConfig())
	kvSess := newKVCacheSessionAdapter(sess)
	var extraKVPairs map[string]any
	if kvRuntime.EnableAffinity && kvRuntime.SupportsAffinity {
		// 亲和生效才解析血缘与轮次（Python 的 resolve_lineage/resolve_turn_num
		// 在能力判定之外调用，关闭时直接返回 None，不读 session）。
		fallbackSID := ""
		if modelCtx != nil {
			fallbackSID = modelCtx.SessionID()
		}
		affinitySID, affinityParentSID := kvHook.ResolveLineage(kvRuntime, kvSess, fallbackSID)
		affinityTurnNum := kvHook.ResolveTurnNum(cbc.Extra(), kvSess)
		extraKVPairs = kvHook.BuildInvokeKwargs(kvRuntime, llmModel, kvSess,
			affinitySID, affinityParentSID, affinityTurnNum)
	} else {
		// 未开亲和：保留 KVCacheRelease 既有路径的行为
		extraKVPairs = llmModel.BuildKVCacheInvokeKwargs(sess, enableKVRelease)
	}

	// 补充 llm_return_token_ids 和 llm_logprobs 配置（对齐 Python L744-749）
	if a.config != nil {
		if a.config.LLMReturnTokenIDs {
			extraKVPairs["return_token_ids"] = true
		}
		if a.config.LLMLogprobs {
			extraKVPairs["logprobs"] = true
			extraKVPairs["top_logprobs"] = a.config.LLMTopLogprobs
		}
	}

	isStreaming := false
	if val, ok := cbc.GetExtra("_streaming"); ok {
		isStreaming, _ = val.(bool)
	}
	modelName := ""
	if a.config != nil {
		modelName = a.config.ModelNameVal
	}

	var aiMsg *llmschema.AssistantMessage
	overflowRecovered := false
	// Python: model.py:30-56 write_llm_route——框架 register_sync 注册的流桥
	// 在 LLM_ROUTE 事件触发时把 route_metadata 写成 llm_route chunk（session
	// 由 contextvar get_current_session 注入）。Go 无 contextvar：改为在模型
	// 调用期间注册临时 OnCustom(LLMRoute) 处理器，闭包捕获 sess（处理器在
	// 路由器选路时同步触发、与调用同 goroutine），写入体内一比一复刻
	// write_llm_route（payload 拷贝 + result_type 兜底 answer + index 0）。
	// 结构偏差 D-7：注册点从「全局一次性注册」改为「每次调用临时注册」，
	// 会话注入从 contextvar 改为闭包——写入时机、chunk 形状、消费方完全一致。
	routeWriteOff := writeLLMRouteDuring(sess)
	defer routeWriteOff()
	for {
		var err error
		if isStreaming {
			aiMsg, err = a.callLLMStream(ctx, cbc, llmModel, modelName, messages, contextTools, sess, extraKVPairs)
		} else {
			aiMsg, err = a.callLLMInvoke(ctx, llmModel, modelName, messages, contextTools, extraKVPairs)
		}
		if err == nil {
			break
		}
		// 拒图降级：请求里带了图、且错误属于「模型/端点不支持图片输入」时，
		// 不抛错，而是造一条双语提示当 assistant 消息塞进上下文，让模型下一轮换方案。
		// Python: react_agent.py L1850-1859（invoke）与 L1928-1936（stream）
		if messagesContainImageInput(messages) && isImageInputUnsupportedError(err) {
			degraded := buildImageInputUnsupportedMessage()
			cbc.SetExtra("_last_image_unsupported", true)
			if inputs, ok := cbc.Inputs().(*interfaces.ModelCallInputs); ok {
				inputs.Response = degraded
			}
			logger.Info(logComponent).
				Str("event_type", "image_input_unsupported").
				Msg("model rejected image input, degrading to text guidance")
			return degraded, nil
		}
		// 上下文溢出：压缩一次再重试本轮（只重试一次；流式已吐字则不允许，
		// 否则会把已发送的前缀重发一遍）。Python: recover_from_model_context_overflow。
		if !overflowRecovered && modelCtx != nil && a.contextEngine != nil &&
			support.IsContextOverflowError(err) {
			emitted := 0
			if isStreaming && cbc.ModelVisibleOutputEmitted() {
				emitted = 1
			}
			recovered := a.contextEngine.RecoverFromModelContextOverflow(
				ctx, sess, modelCtx.ContextID(), isStreaming, emitted)
			logger.Warn(logComponent).
				Str("event_type", "context_overflow_recovery").
				Bool("recovered", recovered).
				Bool("streaming", isStreaming).
				Int("stream_chunks_emitted", emitted).
				Err(err).
				Msg("model reported context overflow")
			if recovered {
				overflowRecovered = true
				if buildErr := buildWindow(); buildErr != nil {
					return nil, buildErr
				}
				if inputs, ok := cbc.Inputs().(*interfaces.ModelCallInputs); ok {
					inputs.Messages = messages
					inputs.Tools = contextTools
				}
				continue
			}
		}
		return nil, err
	}
	// 写回 Response（对齐 Python: ctx.inputs.response = ai_message）
	if inputs, ok := cbc.Inputs().(*interfaces.ModelCallInputs); ok {
		inputs.Response = aiMsg
	}
	// 对齐 Python: self._raise_for_model_response_error(ai_message)
	// —— 输出上限截断与 provider 错误响应都必须转成异常，不能当成功空答案返回。
	if err := raiseForModelResponseError(aiMsg); err != nil {
		return nil, err
	}
	return aiMsg, nil
}

// raiseForModelResponseError 把 provider 错误响应与"输出上限命中且无答案"转成异常。
// 两段判定（顺序不可颠倒；命中第一段即返回）：
//  1. finish_reason ∈ {length, max_tokens} 且既无正文也无 tool_calls —— 推理模型把
//     输出预算花在思维链上，把空响应当最终答案会让本轮以空 "answer" 结束。
//  2. finish_reason ∈ {error, failed} 或 usage.code != 0 —— provider 返回了错误响应。
//
// 刻意不处理：length 且有正文（返回部分文本）、length 且有 tool_calls（照常执行工具）。
// Python: ReActAgent._raise_for_model_response_error() (react_agent.py L2021-2057)
func raiseForModelResponseError(msg *llmschema.AssistantMessage) error {
	if msg == nil {
		return nil
	}
	finishReason := strings.ToLower(strings.TrimSpace(msg.FinishReason))
	code := 0
	outputTokens := 0
	providerMessage := ""
	if msg.UsageMetadata != nil {
		code = msg.UsageMetadata.Code
		outputTokens = msg.UsageMetadata.OutputTokens
		providerMessage = msg.UsageMetadata.ErrMsg
	}
	content := msg.Content.Text()
	noAnswer := len(msg.ToolCalls) == 0 && strings.TrimSpace(content) == ""

	if (finishReason == "length" || finishReason == "max_tokens") && noAnswer {
		return exception.NewBaseError(exception.StatusModelCallFailed,
			exception.WithMsg(fmt.Sprintf(
				"model hit its output limit (finish_reason=%s, output_tokens=%d) before producing an answer or a tool call",
				finishReason, outputTokens)),
			// Python: raise_error(..., details={...}) —— 结构化详情走 details 而非 params
			exception.WithDetails(map[string]any{
				"finish_reason": finishReason,
				"output_tokens": outputTokens,
			}),
		)
	}

	if finishReason != "error" && finishReason != "failed" && code == 0 {
		return nil
	}
	if providerMessage == "" {
		providerMessage = content
	}
	// 上游 error_message[:500] 是「字符」切片；Go 字符串按字节切会把多字节字符劈成
	// 两半产出乱码，故按 rune 截断（provider 中文错误文案是常见情形）。
	shownMessage := utils.TruncateRunes(providerMessage, 500)
	shownFinishReason := finishReason
	if shownFinishReason == "" {
		shownFinishReason = "unknown"
	}
	detail := shownMessage
	if detail == "" {
		detail = "provider returned no error detail"
	}
	return exception.NewBaseError(exception.StatusModelCallFailed,
		exception.WithMsg(fmt.Sprintf(
			"provider response error: code=%d, finish_reason=%s, message=%s",
			code, shownFinishReason, detail)),
		// Python: raise_error(..., details={...})
		exception.WithDetails(map[string]any{
			"provider_code":    code,
			"finish_reason":    shownFinishReason,
			"provider_message": shownMessage,
		}),
	)
}

// callLLMInvoke 非流式 LLM 调用。
func (a *ReActAgent) callLLMInvoke(
	ctx context.Context,
	llmModel *llm.Model,
	modelName string,
	messages []llmschema.BaseMessage,
	tools []cschema.ToolInfoInterface,
	extraKVPairs map[string]any,
) (*llmschema.AssistantMessage, error) {
	toolProviders := tools
	msgsParam := model_clients.NewMessagesParam(messages...)

	// 构建 invoke 选项（含 KV Cache extra kwargs）
	invokeOpts := []model_clients.InvokeOption{
		model_clients.WithInvokeModel(modelName),
		model_clients.WithTools(toolProviders...),
	}
	if len(extraKVPairs) > 0 {
		invokeOpts = append(invokeOpts, model_clients.WithInvokeExtra(extraKVPairs))
	}

	resp, err := llmModel.Invoke(ctx, msgsParam, invokeOpts...)
	if err != nil {
		return nil, fmt.Errorf("LLM invoke 失败: %w", err)
	}
	return resp, nil
}

// callLLMStream 流式 LLM 调用。
// cbc 用于流式分片检查器与可见输出标志（对齐 Python: _railed_model_call 的 ctx）。
// cbc 为 nil 时跳过这两项（仅测试会传 nil）。
func (a *ReActAgent) callLLMStream(
	ctx context.Context,
	cbc *interfaces.AgentCallbackContext,
	llmModel *llm.Model,
	modelName string,
	messages []llmschema.BaseMessage,
	tools []cschema.ToolInfoInterface,
	sess sessioninterfaces.SessionFacade,
	extraKVPairs map[string]any,
) (*llmschema.AssistantMessage, error) {
	toolProviders := tools
	msgsParam := model_clients.NewMessagesParam(messages...)

	// 构建 stream 选项（含 KV Cache extra kwargs）
	streamOpts := []model_clients.StreamOption{
		model_clients.WithStreamModel(modelName),
		model_clients.WithStreamTools(toolProviders...),
	}
	if len(extraKVPairs) > 0 {
		streamOpts = append(streamOpts, model_clients.WithStreamExtra(extraKVPairs))
	}

	chunkCh, err := llmModel.Stream(ctx, msgsParam, streamOpts...)
	if err != nil {
		return nil, fmt.Errorf("LLM stream 失败: %w", err)
	}

	var accumulated *llmschema.AssistantMessageChunk
	chunkIndex := 0
	// 每次流式调用开始时复位可见输出标志（对齐 Python: react_agent.py L1878）。
	if cbc != nil {
		cbc.SetModelVisibleOutputEmitted(false)
	}
	for chunk := range chunkCh {
		// 流中断哨兵：模型客户端读 SSE 失败时投递，通道随后关闭。
		// 必须优先于合并判断——否则半截 tool_call 会被当成完整调用执行，
		// 表现为"参数 JSON 非法"这种误导性报错（Python: raise MODEL_CALL_FAILED）。
		// 错误日志由客户端 FailStream 统一打点（带 model_provider/is_stream），此处不重复。
		if chunk != nil && chunk.StreamError != nil {
			// 对齐 Python: async for _chunk in stream_iter —— 异常路径也必须把剩余流消费完，
			// 否则包装层/客户端 goroutine 与 HTTP 连接会被挂住。
			drained, completed := utils.DrainWithTimeout(chunkCh, streamDrainTimeout)
			if !completed {
				logger.Warn(logComponent).
					Str("event_type", "llm_stream_drain_timeout").
					Str("layer", "agent").
					Msg("drain after stream error did not finish; client may ignore ctx")
			}
			logger.Debug(logComponent).
				Str("event_type", "llm_stream_error_drained").
				Int("drained", drained).
				Err(chunk.StreamError).
				Msg("drained remaining stream after error")
			return nil, chunk.StreamError
		}

		// 使用 Merge 增量合并（对齐 Python __add__ 语义）
		if accumulated == nil {
			accumulated = chunk
		} else {
			accumulated = accumulated.Merge(chunk)
		}

		// 流式分片检查器（对齐 Python: react_agent.py L1897-1905）：
		// 在写流之前跑——被判为重复的文本不应再发给用户。
		// 返回前必须排空 chunkCh，否则客户端 goroutine 卡在 send、连接不关。
		if cbc != nil {
			for _, inspector := range cbc.StreamChunkInspectors() {
				if inspector == nil {
					continue
				}
				if inspectErr := inspector(ctx, cbc, chunk); inspectErr != nil {
					drained, completed := utils.DrainWithTimeout(chunkCh, streamDrainTimeout)
					if !completed {
						logger.Warn(logComponent).
							Str("event_type", "llm_stream_drain_timeout").
							Str("layer", "agent").
							Msg("drain after inspector abort did not finish; client may ignore ctx")
					}
					logger.Debug(logComponent).
						Str("event_type", "stream_chunk_inspector_aborted").
						Int("drained", drained).
						Err(inspectErr).
						Msg("stream chunk inspector aborted the stream")
					return nil, inspectErr
				}
			}
		}

		// 实时写入 session stream（对齐 Python railed_model_call L776-809）
		// Python 先写 reasoning_content，再写 content
		//
		// 有可见内容即置位（对齐 Python: 在 write_stream 之前置位，与 session 是否存在无关）：
		// 上层据此判断"已向用户吐字，瞬时失败不再重试"，避免重复输出。
		if cbc != nil && (chunk.ReasoningContent != "" || chunk.Content.Text() != "") {
			cbc.SetModelVisibleOutputEmitted(true)
		}
		if sess != nil {
			if chunk.ReasoningContent != "" {
				_ = sess.WriteStream(ctx, &stream.OutputSchema{
					Type:  "llm_reasoning",
					Index: chunkIndex,
					Payload: map[string]any{
						"content":     chunk.ReasoningContent,
						"result_type": "answer",
					},
				})
				chunkIndex++
			}
			if chunk.Content.Text() != "" {
				_ = sess.WriteStream(ctx, &stream.OutputSchema{
					Type:  "llm_output",
					Index: chunkIndex,
					Payload: map[string]any{
						"content":     chunk.Content.Text(),
						"result_type": "answer",
					},
				})
				chunkIndex++
			}
		}
	}

	// 取消判定：流可能因 ctx 取消而正常关闭（客户端只关通道、不回错误），
	// 此时若继续返回累积消息，半截回答会被当成最终答复。
	// 对齐 Python：取消发生在 await 点时抛 CancelledError，不会产出 answer。
	if utils.Canceled(ctx) {
		return nil, ctx.Err()
	}

	// 从累积 chunk 转换为最终 AssistantMessage（对齐 Python L791-802）
	var finalMsg *llmschema.AssistantMessage
	if accumulated != nil {
		finalMsg = accumulated.ToAssistantMessage()
	} else {
		finalMsg = llmschema.NewAssistantMessage("")
	}

	// 写入 usage_metadata（对齐 Python L804-809）
	if sess != nil && finalMsg.UsageMetadata != nil {
		_ = sess.WriteStream(ctx, &stream.OutputSchema{
			Type:  "llm_usage",
			Index: 0,
			Payload: map[string]any{
				"usage_metadata": finalMsg.UsageMetadata,
				"result_type":    "answer",
			},
		})
	}

	return finalMsg, nil
}

// logLLMRequest 记录 LLM 请求诊断日志。
// Python: log_llm_request()
func logLLMRequest(messages []llmschema.BaseMessage, tools []cschema.ToolInfoInterface) {
	msgCount := len(messages)
	toolCount := len(tools)
	logger.Info(logComponent).
		Int("msg_count", msgCount).
		Int("tool_count", toolCount).
		Msg("[LLM] >>> request")

	for idx, msg := range messages {
		role := msg.GetRole().String()
		contentStr := msg.GetContent().Text()
		if len(contentStr) > 300 {
			contentStr = contentStr[:300]
		}
		event := logger.Info(logComponent).
			Int("msg_idx", idx).
			Str("role", role)
		if contentStr != "" {
			event = event.Str("content", contentStr)
		}
		event.Msg("[LLM]   msg")
	}
}

// logLLMResponse 记录 LLM 响应诊断日志。
// Python: log_llm_response()
func logLLMResponse(aiMsg *llmschema.AssistantMessage) {
	if aiMsg == nil {
		return
	}
	contentLen := len(aiMsg.Content.Text())
	tcCount := len(aiMsg.ToolCalls)

	// finish_reason 必须记录：工具参数 JSON 非法时，它是区分"输出被上限
	// 截断"（length）与"模型自己拼错 JSON"（stop/tool_calls）的唯一线索。
	// reasoning_len 必须记录：思考内容不进正文，界面「有没有思考」只取决于端点
	// 是否回 delta.reasoning_content。为 0 说明模型/端点没吐思维链；大于 0 而
	// 界面没有思考块，则缺口在流式写入、适配层或前端渲染。
	event := logger.Info(logComponent).
		Int("content_len", contentLen).
		Int("reasoning_len", len(aiMsg.ReasoningContent)).
		Int("tool_call_count", tcCount).
		Str("finish_reason", aiMsg.FinishReason)
	if aiMsg.UsageMetadata != nil {
		event = event.
			Int("input_tokens", aiMsg.UsageMetadata.InputTokens).
			Int("output_tokens", aiMsg.UsageMetadata.OutputTokens)
	}
	event.Msg("[LLM] <<< response")

	for _, tc := range aiMsg.ToolCalls {
		argsValid := json.Valid([]byte(tc.Arguments))
		logger.Info(logComponent).
			Str("tool_name", tc.Name).
			Int("args_len", len(tc.Arguments)).
			Bool("args_valid", argsValid).
			Str("args", tc.Arguments).
			Msg("[LLM]   tool_call")
		// 非法参数单独告警：正常调用不该命中，命中即说明模型输出不可用，
		// 后续 AbilityManager 会把解析错误回灌给模型重试。
		if !argsValid {
			logger.Warn(logComponent).
				Str("event_type", "tool_call_args_invalid").
				Str("tool_name", tc.Name).
				Str("finish_reason", aiMsg.FinishReason).
				Int("args_len", len(tc.Arguments)).
				Msg("[LLM]   tool_call arguments are not valid JSON")
		}
	}
}

const (
	// imageInputScanMaxDepth 错误值/内容形状递归扫描的深度上限
	// Python: _IMAGE_INPUT_SCAN_MAX_DEPTH = 8 (react_agent.py L114)
	imageInputScanMaxDepth = 8
)

// imageInputUnsupportedCodes provider 拒图的结构化错误码（与上游逐一对应）。
// Python: _IMAGE_INPUT_UNSUPPORTED_ERROR_CODES (react_agent.py L131-138)
var imageInputUnsupportedCodes = []string{
	"invalid_image_input",
	"image_input_unsupported",
	"unsupported_content_type",
	"unsupported_image",
	"unsupported_image_input",
	"unsupported_message_content_type",
}

// imageInputUnsupportedPatterns provider 拒图的文案片段（与上游逐一对应）。
// Python: _IMAGE_INPUT_UNSUPPORTED_ERROR_PATTERNS (react_agent.py L139-153)
var imageInputUnsupportedPatterns = []string{
	"no endpoints found that support image input",
	"does not accept images",
	"does not support image",
	"doesn't accept images",
	"doesn't support image",
	"do not support image",
	"image input is not supported",
	"image input not supported",
	"image_url is not supported",
	"images are not supported",
	"multimodal input is not supported",
	"not support image input",
	"unsupported image",
	"vision is not supported",
}

// iterExceptionErrorValues 递归展开一个值，产出所有可参与匹配的字符串。
// Python: ReActAgent._iter_exception_error_values() (react_agent.py L2100-2119)
// 深度上限与上游一致（_IMAGE_INPUT_SCAN_MAX_DEPTH=8）。
func iterExceptionErrorValues(value any, depth int) []string {
	if depth > imageInputScanMaxDepth || value == nil {
		return nil
	}
	switch v := value.(type) {
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	case error:
		return []string{v.Error()}
	case map[string]any:
		var out []string
		for _, child := range v {
			out = append(out, iterExceptionErrorValues(child, depth+1)...)
		}
		return out
	case []any:
		var out []string
		for _, child := range v {
			out = append(out, iterExceptionErrorValues(child, depth+1)...)
		}
		return out
	case []string:
		var out []string
		for _, child := range v {
			if child != "" {
				out = append(out, child)
			}
		}
		return out
	case nil:
		return nil
	default:
		// 数字/布尔等标量：字符串化后参与匹配（上游对 int/float 同样 yield str）
		return []string{fmt.Sprintf("%v", v)}
	}
}

// extractExceptionErrorValues 收集错误对象上所有可匹配文本。
// Python: ReActAgent._extract_exception_error_values() (react_agent.py L2121-2144)。
// 上游还抽 exc.response.status_code / response.json() / response.text——Go 侧异常
// 没有 response 对象，等价载体是 BaseError 的 Params 与 Details，故在此展开，
// 这是与上游的已知差异（覆盖面等价，取数位置不同）。
func extractExceptionErrorValues(err error) []string {
	var out []string
	for err != nil {
		out = append(out, err.Error())
		var be *exception.BaseError
		if errors.As(err, &be) {
			for _, v := range be.Params() {
				out = append(out, iterExceptionErrorValues(v, 0)...)
			}
			out = append(out, iterExceptionErrorValues(be.Details(), 0)...)
		}
		err = errors.Unwrap(err)
	}
	return out
}

// isImageInputUnsupportedError 判断错误是否属于「模型/端点不支持图片输入」。
// Python: ReActAgent._is_image_input_unsupported_error() (react_agent.py L2147-2164)
func isImageInputUnsupportedError(err error) bool {
	if err == nil {
		return false
	}
	texts := extractExceptionErrorValues(err)
	lowered := make([]string, 0, len(texts))
	for _, t := range texts {
		lowered = append(lowered, strings.ToLower(t))
	}
	normalizer := strings.NewReplacer("-", "_", " ", "_")
	for _, v := range lowered {
		normalized := normalizer.Replace(v)
		for _, code := range imageInputUnsupportedCodes {
			if normalized == code || strings.HasSuffix(normalized, "_"+code) {
				return true
			}
		}
	}
	joined := strings.Join(lowered, "\n")
	for _, pattern := range imageInputUnsupportedPatterns {
		if strings.Contains(joined, pattern) {
			return true
		}
	}
	return false
}

// messagesContainImageInput 判断请求消息里是否含图片内容块。
// Python: ReActAgent._messages_contain_image_input()
func messagesContainImageInput(messages []llmschema.BaseMessage) bool {
	// 上游是 JSON 形状递归扫描（dict 里 type in (image, image_url) 或出现 image_url/
	// image 键即算），因为 Python 的 content 可能是原始 dict/list。Go 的 MessageContent
	// 已是强类型（text XOR parts），没有可递归的嵌套容器，等价判定退化为「扫 Parts：
	// type 属于 image/image_url，或 ImageURL 字段存在」——注意上游只认键存在，不看
	// URL 是否为空，这里保持同样的宽口径。
	for _, m := range messages {
		for _, part := range m.GetContent().Parts() {
			if part.Type == "image" || part.Type == "image_url" || part.ImageURL != nil {
				return true
			}
		}
	}
	return false
}

// buildImageInputUnsupportedMessage 构造拒图降级提示消息（双语，文案照抄上游）。
// Python: ReActAgent._build_image_input_unsupported_message() (react_agent.py L2166-2186)
func buildImageInputUnsupportedMessage() *llmschema.AssistantMessage {
	return llmschema.NewAssistantMessage("[Vision Input Unsupported] The current model or endpoint does not support image input.\n" +
		"当前主模型或模型服务端点不支持图片输入。\n\n" +
		"Suggested alternatives / 建议替代方案：\n" +
		"1. If reading a PDF, use `read_file` in text mode or use `pypdf`/`pdfplumber` " +
		"to extract text and form fields directly, without rendering pages to images.\n" +
		"   如果读取的是 PDF，请使用文本模式读取，或用 `pypdf` 提取文本和表单字段。\n" +
		"2. If reading an image (JPG/PNG), describe what information you need and ask the " +
		"user for a text transcription, or use an available OCR tool if one is configured.\n" +
		"   如果读取的是图片，请描述所需信息并请用户提供文字转录。\n" +
		"3. If the task absolutely requires vision, configure a `vision_model_config` " +
		"with a vision-capable endpoint.\n" +
		"   如果任务必须依赖视觉，请配置支持视觉输入的模型。")
}

// extraInt 从回调上下文 extra 里取正整数，缺失或类型不符返回 0。
// Python: int(ctx.extra.get(key, 0) or 0)（react_agent.py:1043）
func extraInt(cbc *interfaces.AgentCallbackContext, key string) int {
	if cbc == nil {
		return 0
	}
	raw, ok := cbc.GetExtra(key)
	if !ok {
		return 0
	}
	switch v := raw.(type) {
	case int:
		if v > 0 {
			return v
		}
	case int64:
		if v > 0 {
			return int(v)
		}
	case float64:
		if v > 0 {
			return int(v)
		}
	}
	return 0
}

// writeLLMRouteDuring 在模型调用期间注册临时 LLM_ROUTE 流桥，返回注销函数。
// 处理器体一比一复刻 Python write_llm_route（model.py:42-54）：payload 取
// route_metadata 拷贝、result_type 缺省 "answer"，写入
// OutputSchema{type="llm_route", index=0, payload}；session 为 nil 时跳过
//（对齐 Python session is None → return）。
// Python: model.py:30-56 _ensure_llm_route_stream_writer_registered
// 结构偏差 D-7：Python 全局注册一次 + contextvar 注入 session；Go 无
// contextvar，改为每次模型调用临时注册、闭包捕获 sess——触发时机（路由器
// 选路时同步触发）与 chunk 形状一致。
func writeLLMRouteDuring(sess sessioninterfaces.SessionFacade) func() {
	if sess == nil {
		return func() {}
	}
	handler := func(ctx context.Context, data map[string]any) any {
		metadata, _ := data["route_metadata"].(map[string]any)
		if len(metadata) == 0 {
			return nil
		}
		payload := make(map[string]any, len(metadata)+1)
		for k, v := range metadata {
			payload[k] = v
		}
		if _, ok := payload["result_type"]; !ok {
			payload["result_type"] = "answer"
		}
		_ = sess.WriteStream(ctx, &stream.OutputSchema{
			Type:    "llm_route",
			Index:   0,
			Payload: payload,
		})
		return nil
	}
	fw := callback.GetCallbackFramework()
	fw.OnCustom(string(callback.LLMRoute), handler)
	return func() { fw.OffCustom(string(callback.LLMRoute), handler) }
}
