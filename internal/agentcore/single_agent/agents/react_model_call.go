package agents

import (
	"context"
	"fmt"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/rail"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// callModel 调用 LLM 模型（经 Rail 钩子包装）。
//
// 对应 Python: ReActAgent._call_model()
func (a *ReActAgent) callModel(
	ctx context.Context,
	cbc *rail.AgentCallbackContext,
	modelCtx ceinterface.ModelContext,
	tools []*cschema.ToolInfo,
	sess sessioninterfaces.SessionFacade,
) (*llmschema.AssistantMessage, error) {
	// 对齐 Python L619-625: preview messages 包含 system prompt 前缀
	previewMsgs := make([]llmschema.BaseMessage, 0)
	previewPrompt := a.promptBuilder.Build()
	if previewPrompt != "" {
		previewMsgs = append(previewMsgs, llmschema.NewSystemMessage(previewPrompt))
	}
	if modelCtx != nil {
		msgs, _ := modelCtx.GetMessages(0, true)
		previewMsgs = append(previewMsgs, msgs...)
	}
	// 对齐 Python L648-652: ctx.inputs = ModelCallInputs(messages=..., tools=..., model_context=...)
	cbc.SetInputs(&rail.ModelCallInputs{
		Messages:     previewMsgs,
		Tools:        tools,
		ModelContext: modelCtx,
	})

	var result *llmschema.AssistantMessage
	err := rail.ModelCallRail.Execute(ctx, cbc, func() error {
		var e error
		result, e = a.railedModelCall(ctx, cbc, sess)
		return e
	})

	// 对齐 Python L659: log_llm_response
	if result != nil {
		logLLMResponse(result)
	}

	return result, err
}

// railedModelCall 在 Rail 钩子内执行 LLM 调用。
func (a *ReActAgent) railedModelCall(ctx context.Context, cbc *rail.AgentCallbackContext, sess sessioninterfaces.SessionFacade) (*llmschema.AssistantMessage, error) {
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
			Msg("enable_kv_cache_release 已启用但当前 LLM 不支持 KV Cache 释放")
		a.kvReleaseWarningLogged = true
	}

	// 构建 GetContextWindow 选项：支持 KV Cache 时传入 model
	var contextWindowOpts []ceinterface.Option
	if enableKVRelease && supportsKVRelease {
		contextWindowOpts = append(contextWindowOpts, ceinterface.WithModel(llmModel))
	}

	var messages []llmschema.BaseMessage
	var contextTools []*cschema.ToolInfo

	if modelCtx != nil {
		contextWindow, err := modelCtx.GetContextWindow(ctx, systemMsgs, nil, 0, 0, contextWindowOpts...)
		if err != nil {
			return nil, fmt.Errorf("获取上下文窗口失败: %w", err)
		}
		messages = contextWindow.GetMessages()
		contextTools = contextWindow.GetTools()
	} else {
		messages = systemMsgs
	}

	// 写回 cbc.Inputs()（对齐 Python L727-728: ctx.inputs.messages = ...; ctx.inputs.tools = ...）
	if inputs, ok := cbc.Inputs().(*rail.ModelCallInputs); ok {
		inputs.Messages = messages
		inputs.Tools = contextTools
	}

	// 对齐 Python L730: log_llm_request
	logLLMRequest(messages, contextTools)

	// 构建 KV Cache extra kwargs（对应 Python L736-742）
	extraKVPairs := llmModel.BuildKVCacheInvokeKwargs(sess, enableKVRelease)

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

	isStreaming, _ := cbc.Extra()["_streaming"].(bool)
	modelName := ""
	if a.config != nil {
		modelName = a.config.ModelNameVal
	}

	if isStreaming {
		aiMsg, err := a.callLLMStream(ctx, llmModel, modelName, messages, contextTools, sess, extraKVPairs)
		if err != nil {
			return nil, err
		}
		// 写回 Response（对齐 Python L803: ctx.inputs.response = ai_message）
		if inputs, ok := cbc.Inputs().(*rail.ModelCallInputs); ok {
			inputs.Response = aiMsg
		}
		return aiMsg, nil
	}
	aiMsg, err := a.callLLMInvoke(ctx, llmModel, modelName, messages, contextTools, extraKVPairs)
	if err != nil {
		return nil, err
	}
	// 写回 Response（对齐 Python L758: ctx.inputs.response = ai_message）
	if inputs, ok := cbc.Inputs().(*rail.ModelCallInputs); ok {
		inputs.Response = aiMsg
	}
	return aiMsg, nil
}

// callLLMInvoke 非流式 LLM 调用。
func (a *ReActAgent) callLLMInvoke(
	ctx context.Context,
	llmModel *llm.Model,
	modelName string,
	messages []llmschema.BaseMessage,
	tools []*cschema.ToolInfo,
	extraKVPairs map[string]any,
) (*llmschema.AssistantMessage, error) {
	toolProviders := make([]cschema.ToolInfoProvider, len(tools))
	for i, t := range tools {
		toolProviders[i] = t
	}
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
func (a *ReActAgent) callLLMStream(
	ctx context.Context,
	llmModel *llm.Model,
	modelName string,
	messages []llmschema.BaseMessage,
	tools []*cschema.ToolInfo,
	sess sessioninterfaces.SessionFacade,
	extraKVPairs map[string]any,
) (*llmschema.AssistantMessage, error) {
	toolProviders := make([]cschema.ToolInfoProvider, len(tools))
	for i, t := range tools {
		toolProviders[i] = t
	}
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
	for chunk := range chunkCh {
		// 使用 Merge 增量合并（对齐 Python __add__ 语义）
		if accumulated == nil {
			accumulated = chunk
		} else {
			accumulated = accumulated.Merge(chunk)
		}

		// 实时写入 session stream（对齐 Python railed_model_call L776-809）
		// Python 先写 reasoning_content，再写 content
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
//
// 对应 Python: log_llm_request()
func logLLMRequest(messages []llmschema.BaseMessage, tools []*cschema.ToolInfo) {
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
//
// 对应 Python: log_llm_response()
func logLLMResponse(aiMsg *llmschema.AssistantMessage) {
	if aiMsg == nil {
		return
	}
	contentLen := len(aiMsg.Content.Text())
	tcCount := len(aiMsg.ToolCalls)

	event := logger.Info(logComponent).
		Int("content_len", contentLen).
		Int("tool_call_count", tcCount)
	if aiMsg.UsageMetadata != nil {
		event = event.
			Int("input_tokens", aiMsg.UsageMetadata.InputTokens).
			Int("output_tokens", aiMsg.UsageMetadata.OutputTokens)
	}
	event.Msg("[LLM] <<< response")

	for _, tc := range aiMsg.ToolCalls {
		logger.Info(logComponent).
			Str("tool_name", tc.Name).
			Str("args", tc.Arguments).
			Msg("[LLM]   tool_call")
	}
}
