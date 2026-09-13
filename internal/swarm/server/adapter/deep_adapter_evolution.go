package adapter

import (
	"context"
	"strings"
	"time"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	sessionmd "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// recentMessageWindow recap 取最近消息的窗口大小。
// Python: RECENT_MESSAGE_WINDOW = 30
const recentMessageWindow = 30

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CompressContext 触发上下文压缩。
// Python: compress_context() (line 5380-5570)
// 编排薄层：获取 context_engine → 调 CompressContext → 统计 token → 返回结果。
// 不依赖 SessionHistory JSONL，数据来自内存中的 ContextEngine。
func (d *DeepAdapter) CompressContext(ctx context.Context, sessionID string, session sessioninterfaces.SessionFacade, returnState bool) (map[string]any, error) {
	if d.instance == nil {
		return map[string]any{"result": "noop"}, nil
	}
	reactAgent := d.instance.ReactAgent()
	if reactAgent == nil {
		return map[string]any{"result": "noop"}, nil
	}
	contextEngine := reactAgent.ContextEngine()
	if contextEngine == nil {
		return map[string]any{"result": "noop"}, nil
	}

	// 获取上下文
	modelCtx := contextEngine.GetContext("default_context", sessionID)
	if modelCtx == nil {
		return map[string]any{"result": "noop", "stats": nil}, nil
	}

	// 计算压缩前 token 数
	rawTotalTokens, _ := d.countFullContextTokens(ctx, sessionID)

	// 执行压缩
	compactResult, err := contextEngine.CompressContext(ctx, "default_context", session,
		ceinterface.WithCompressSessionID(sessionID),
		ceinterface.WithReturnState(returnState),
	)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("CompressContext 压缩失败")
		return map[string]any{"result": "error", "error": err.Error()}, err
	}

	// Python: 解析压缩结果
	result := compactResult.Result
	response := map[string]any{"result": result}

	if returnState && compactResult.State != nil {
		response["state"] = compactResult.State
		if compactResult.CompactSummary != "" {
			response["compact_summary"] = compactResult.CompactSummary
		}
	}

	if result == "compressed" {
		// Python: (L5440-5441): context = context_engine.get_context(session_id=session_id)
		// 压缩后重新获取 context，确保统计的是压缩后的数据
		newModelCtx := contextEngine.GetContext("default_context", sessionID)
		if newModelCtx != nil {
			// Python: (L5443-5445): total_tokens = await self._count_full_context_tokens(context, react_agent, session_id)
			totalTokens, _ := d.countFullContextTokens(ctx, sessionID)
			// Python: (L5447): stats = context.statistic()
			stats := newModelCtx.Statistic()
			response["stats"] = map[string]any{
				"total_messages":   stats.TotalMessages,
				"total_tokens":     totalTokens,
				"raw_total_tokens": rawTotalTokens,
			}
			// Python: (L5453-5455):
			//   if summary:
			//     response["summary"] = summary
			//     response.setdefault("compact_summary", summary)
			if compactResult.CompactSummary != "" {
				response["summary"] = compactResult.CompactSummary
				// setdefault: 仅在 compact_summary 未设置时才写入
				if _, exists := response["compact_summary"]; !exists {
					response["compact_summary"] = compactResult.CompactSummary
				}
			}
		}
	}

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Str("result", result).
		Msg("CompressContext 完成")

	return response, nil
}

// GetContextUsage 获取上下文窗口占用率。
// Python: get_context_usage() (line 5572-5588)
func (d *DeepAdapter) GetContextUsage(ctx context.Context, sessionID string) (map[string]any, error) {
	if d.instance == nil {
		return nil, nil
	}
	// Python: 直接调 instance.get_context_usage()
	usage, err := d.instance.GetContextUsage(ctx, sessionID, "")
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("GetContextUsage 失败")
		return nil, err
	}
	return usage, nil
}

// GenerateRecap 生成会话回顾摘要。
// Python: generate_recap() (line 5572-5591)
// 从 ContextEngine 内存获取最近消息 → 调模型生成 1-3 句摘要。
// 不依赖 SessionHistory JSONL。
func (d *DeepAdapter) GenerateRecap(ctx context.Context, sessionID string) (map[string]any, error) {
	if d.instance == nil {
		return map[string]any{"status": "no_turn"}, nil
	}

	// 获取最近消息
	messages := d.getRecentMessages(sessionID)
	if len(messages) == 0 {
		return map[string]any{"status": "no_turn"}, nil
	}

	// 构建 recap 提示词
	prompt := buildRecapPrompt("")

	// 调用模型生成摘要
	summaryText, err := d.callModelForRecap(ctx, messages, prompt)
	if err != nil || summaryText == "" {
		return map[string]any{"status": "failed", "error": "模型返回空响应"}, nil
	}

	logger.Info(logComponent).Str("session_id", sessionID).Msg("GenerateRecap 完成")
	return map[string]any{"status": "ok", "summary": strings.TrimSpace(summaryText)}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// watchEvolutionAndPush 启动 evolution 观察任务。
// ✅ 已回填（对齐 Python: _watch_evolution_and_push()）
//
// 轮询 SkillEvolutionRail.DrainPendingApprovalEvents → 推送到前端
func (d *DeepAdapter) watchEvolutionAndPush(ctx context.Context, sessionID string, channelID string, requestID string) error {
	if d.skillEvolutionRail == nil {
		return nil
	}

	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			events := d.skillEvolutionRail.DrainPendingApprovalEvents(true, nil)
			if len(events) == 0 {
				continue
			}

			// 将 []*stream.OutputSchema 转为 []map[string]any
			dicts := make([]map[string]any, 0, len(events))
			for _, e := range events {
				dict := outputSchemaToDict(e)
				if dict != nil {
					dicts = append(dicts, dict)
				}
			}

			// Python: await push_evolution_progress(push_context, rid, events, ...)
			// 通过 globalSendPushFunc 推送进度事件，避免循环依赖
			if globalSendPushFunc != nil {
				for _, evt := range dicts {
					if isEvolutionApprovalEventLocal(evt) || isEvolutionOutcomeEventLocal(evt) {
						continue
					}
					msg := sessionmd.BuildServerPushMessage(sessionID, requestID, evt, channelID)
					_ = globalSendPushFunc(ctx, msg)
				}
			}

			// 逐个处理审批/结果事件
			for _, event := range events {
				evt := outputSchemaToDict(event)
				if evt == nil {
					continue
				}
				if isEvolutionApprovalEventLocal(evt) || isEvolutionOutcomeEventLocal(evt) {
					d.pushEventToFrontend(event, sessionID, channelID, requestID)
				}
				// 检查是否包含 outcome 事件（完成/失败/超时）
				if isOutcomeEvent(event) {
					return nil
				}
			}
		}
	}
}

// onEvolutionWatcherDone evolution 观察任务完成回调。
// ✅ 已回填（对齐 Python: _on_evolution_watcher_done()）
func (d *DeepAdapter) onEvolutionWatcherDone(sessionID string) {
	logger.Info(logComponent).Str("session_id", sessionID).Msg("onEvolutionWatcherDone: evolution watcher 已完成")
}

// buildRecapPrompt 构建 recap 提示词。
// Python: recap_prompts.build_recap_prompt(memory: str | None) -> str
// memory 为空字符串时等同 Python 的 memory=None（不拼接 memory 前缀块）。
func buildRecapPrompt(memory string) string {
	memoryBlock := ""
	if memory != "" {
		memoryBlock = "Session memory (broader context):\n" + memory + "\n\n"
	}
	return memoryBlock +
		"The user is requesting a quick recap of the current session. " +
		"Write exactly 1-3 short sentences. " +
		"Start by stating the high-level task — what they are building or debugging, not implementation details. " +
		"Next: the concrete next step. " +
		"Skip status reports and commit recaps."
}

// handleEvolutionApproval 处理演进审批。
// ✅ 已回填（对齐 Python: _handle_evolution_approval()）
//
// 根据 request_id 前缀路由到 SkillEvolutionRail.ApproveRecord / RejectRecord
func (d *DeepAdapter) handleEvolutionApproval(requestID string, answers []ApprovalAnswer) bool {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("request_id", requestID).Msg("handleEvolutionApproval: SkillEvolutionRail 未初始化")
		return false
	}

	ctx := context.Background()

	// 解析 answers 为 approve/reject
	approved := parseApprovalAnswers(answers)

	if approved {
		err := d.skillEvolutionRail.ApproveRecord(ctx, requestID)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("handleEvolutionApproval: ApproveRecord 失败")
			return false
		}
		logger.Info(logComponent).Str("request_id", requestID).Msg("handleEvolutionApproval: 已批准")
		return true
	}

	err := d.skillEvolutionRail.RejectRecord(ctx, requestID)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("handleEvolutionApproval: RejectRecord 失败")
		return false
	}
	logger.Info(logComponent).Str("request_id", requestID).Msg("handleEvolutionApproval: 已拒绝")
	return true
}

// getRecentMessages 获取最近消息列表。
// Python: _get_recent_messages() (line 5593-5609)
// 从 ContextEngine 内存中获取，不读 JSONL。
// 返回原始 BaseMessage 列表，由 callModelForRecap 负责提取 role/content。
func (d *DeepAdapter) getRecentMessages(sessionID string) []llmschema.BaseMessage {
	if d.instance == nil {
		return nil
	}
	reactAgent := d.instance.ReactAgent()
	if reactAgent == nil {
		return nil
	}
	contextEngine := reactAgent.ContextEngine()
	if contextEngine == nil {
		return nil
	}

	modelCtx := contextEngine.GetContext("default_context", sessionID)
	if modelCtx == nil {
		return nil
	}

	// Python: all_messages = list(context.get_messages() or [])
	allMessages, err := modelCtx.GetMessages(0, true)
	if err != nil || len(allMessages) == 0 {
		return nil
	}

	// Python: return all_messages[-window:]
	window := recentMessageWindow
	if len(allMessages) < window {
		window = len(allMessages)
	}

	return allMessages[len(allMessages)-window:]
}

// callModelForRecap 调用模型生成 recap。
// Python: _call_model_for_recap() (line 5611-5663)
// 不传 system prompt，prompt 作为最后一条 user message。
func (d *DeepAdapter) callModelForRecap(ctx context.Context, messages []llmschema.BaseMessage, prompt string) (string, error) {
	if d.model == nil {
		logger.Error(logComponent).Msg("callModelForRecap: 无可用模型实例")
		return "", nil
	}

	// 构建消息列表：原始消息 + recap 提示词作为最后一条 user message
	recapMessages := make([]llmschema.BaseMessage, 0, len(messages)+1)
	for _, msg := range messages {
		// Python: role = getattr(msg, "role", None) or ""
		role := msg.GetRole()

		// Python: content = getattr(msg, "content", None) or ""
		//   if isinstance(content, list): → 多模态，提取文本部分
		mc := msg.GetContent()
		var content string
		if mc.IsText() {
			content = mc.Text()
		} else {
			// 多模态：拼接 Parts() 中 Type=="text" 的 Text 字段
			// Python: " ".join(str(p) for p in content if isinstance(p, str) or (isinstance(p, dict) and p.get("type") == "text"))
			var textParts []string
			for _, part := range mc.Parts() {
				if part.Type == "text" && part.Text != "" {
					textParts = append(textParts, part.Text)
				}
			}
			content = strings.Join(textParts, " ")
		}

		// Python: if not content.strip(): continue
		if strings.TrimSpace(content) == "" {
			continue
		}

		// Python: if role == "user": UserMessage / elif role == "assistant": AssistantMessage / else: UserMessage
		switch role {
		case llmschema.RoleTypeUser:
			recapMessages = append(recapMessages, llmschema.NewUserMessage(content))
		case llmschema.RoleTypeAssistant:
			recapMessages = append(recapMessages, llmschema.NewAssistantMessage(content))
		default:
			recapMessages = append(recapMessages, llmschema.NewUserMessage(content))
		}
	}
	// Python: prompt 作为最后一条 user message 追加
	recapMessages = append(recapMessages, llmschema.NewUserMessage(prompt))

	// 调用模型，对齐 Python: model.invoke(messages, max_tokens=300, temperature=0)
	result, err := d.model.Invoke(ctx, model_clients.NewMessagesParam(recapMessages...),
		model_clients.WithInvokeMaxTokens(300),
		model_clients.WithInvokeTemperature(0),
	)
	if err != nil {
		logger.Error(logComponent).Err(err).Msg("callModelForRecap 模型调用失败")
		return "", err
	}
	if result == nil {
		return "", nil
	}
	return result.Content.Text(), nil
}

// countFullContextTokens 计算完整上下文 token 数。
// Python: _count_full_context_tokens() (line 5665-5723)
// 包含三部分：1. system prompt  2. 对话消息  3. 工具定义
func (d *DeepAdapter) countFullContextTokens(ctx context.Context, sessionID string) (int, error) {
	if d.instance == nil {
		return 0, nil
	}
	reactAgent := d.instance.ReactAgent()
	if reactAgent == nil {
		return 0, nil
	}
	contextEngine := reactAgent.ContextEngine()
	if contextEngine == nil {
		return 0, nil
	}

	modelCtx := contextEngine.GetContext("default_context", sessionID)
	if modelCtx == nil {
		return 0, nil
	}

	// Python: token_counter = context.token_counter()
	tc := modelCtx.TokenCounter()

	// Python: 无 token_counter 时使用 len // 4 粗估 fallback
	useFallback := tc == nil

	// 获取模型名称用于 token 计数
	modelName := reactAgent.Config().ModelName()

	totalTokens := 0

	// Python 步骤1: 计算系统消息的 tokens (L5686-5697)
	//   if hasattr(react_agent, "prompt_builder") and react_agent.prompt_builder is not None:
	//     system_prompt = react_agent.prompt_builder.build()
	//   elif hasattr(react_agent, "system_prompt_builder") and react_agent.system_prompt_builder is not None:
	//     system_prompt = react_agent.system_prompt_builder.build()
	//
	// Go 中 PromptBuilder() 返回 *SystemPromptBuilder（具体类型，有 Build()），
	// SystemPromptBuilder() 返回接口（无 Build()），需类型断言。
	systemPrompt := ""
	pb := reactAgent.PromptBuilder()
	if pb != nil {
		systemPrompt = pb.Build()
	}
	if strings.TrimSpace(systemPrompt) == "" {
		spb := reactAgent.SystemPromptBuilder()
		if spb != nil {
			// SystemPromptBuilderInterface 无 Build()，断言为具体类型
			if builder, ok := spb.(*prompts.SystemPromptBuilder); ok {
				systemPrompt = builder.Build()
			}
		}
	}
	if systemPrompt != "" {
		if !useFallback {
			count, _ := tc.Count(systemPrompt, modelName)
			totalTokens += count
		} else {
			// Python: total_tokens += len(system_prompt) // 4
			totalTokens += len(systemPrompt) / 4
		}
	}

	// Python 步骤2: 计算对话消息的 tokens (L5699-5705)
	//   if token_counter is not None:
	//     total_tokens += token_counter.count_messages(context_messages)
	//   else:
	//     total_tokens += sum(len(str(msg.content)) // 4 for msg in context_messages)
	messages, err := modelCtx.GetMessages(0, true)
	if err == nil && len(messages) > 0 {
		if !useFallback {
			count, _ := tc.CountMessages(messages, modelName)
			totalTokens += count
		} else {
			for _, msg := range messages {
				text := msg.GetContent().Text()
				if text != "" {
					totalTokens += len(text) / 4
				}
			}
		}
	}

	// Python 步骤3: 计算工具定义的 tokens (L5707-5721)
	//   tools = []
	//   if hasattr(react_agent, "ability_manager") and react_agent.ability_manager is not None:
	//     for card in react_agent.ability_manager.list() or []:
	//       if hasattr(card, "to_tool_info"): tools.append(card.to_tool_info())
	//   if tools and token_counter is not None:
	//     total_tokens += token_counter.count_tools(tools)
	am := reactAgent.AbilityManager()
	if am != nil {
		toolInfos, listErr := am.ListToolInfo(ctx, nil)
		if listErr == nil && len(toolInfos) > 0 {
			if !useFallback {
				count, _ := tc.CountTools(toolInfos, modelName)
				totalTokens += count
			}
			// Python: tc==nil 时无 count_tools fallback（Python 也不做）
		}
	}

	return totalTokens, nil
}

// outputSchemaToDict 将 OutputSchema 转为 helpers 期望的 map[string]any 格式。
// helpers 函数（PushEvolutionProgress 等）全部基于 map[string]any，
// 而 DrainPendingApprovalEvents 返回 []*stream.OutputSchema。
func outputSchemaToDict(schema *stream.OutputSchema) map[string]any {
	if schema == nil {
		return nil
	}
	if payload, ok := schema.Payload.(map[string]any); ok {
		return payload
	}
	// fallback：将 OutputSchema 整体转为 dict
	return map[string]any{
		"type":    schema.Type,
		"index":   schema.Index,
		"payload": schema.Payload,
	}
}

// pushEventToFrontend 将演进事件推送到前端。
// Python: _push_event_to_frontend(event)
// ✅ 已回填：通过 globalSendPushFunc 推送，避免 adapter→server 循环依赖
func (d *DeepAdapter) pushEventToFrontend(event *stream.OutputSchema, sessionID string, channelID string, requestID string) {
	evt := outputSchemaToDict(event)
	if evt == nil {
		return
	}
	if globalSendPushFunc == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("pushEventToFrontend: globalSendPushFunc 未注入")
		return
	}

	evtKind := evolutionEventKindLocal(evt)
	switch evtKind {
	case "approval":
		// Python: await push_evolution_event(push_context, rid, evt, build_server_push_message)
		msg := sessionmd.BuildServerPushMessage(sessionID, requestID, evt, channelID)
		if err := globalSendPushFunc(context.Background(), msg); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("推送审批事件失败")
		}
	case "outcome":
		// Python: await push_evolution_status(push_context, update, build_server_push_message)
		outcome := evolutionOutcomeFromEventLocal(evt)
		payload := map[string]any{
			"event_type": "chat.evolution_status",
			"status":     outcome["status"],
			"stage":      outcome["status"],
			"message":    outcome["message"],
			"request_id": requestID,
		}
		msg := sessionmd.BuildServerPushMessage(sessionID, requestID, payload, channelID)
		if err := globalSendPushFunc(context.Background(), msg); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("推送结果状态失败")
		}
	default:
		logger.Debug(logComponent).Str("session_id", sessionID).Str("event_kind", evtKind).Msg("演进进度事件已在批量推送中处理")
	}
}

// isOutcomeEvent 判断是否为结果事件（完成/失败/超时）。
func isOutcomeEvent(event *stream.OutputSchema) bool {
	if event == nil {
		return false
	}
	payload, ok := event.Payload.(map[string]any)
	if !ok {
		return false
	}
	meta, ok := payload["_evolution_meta"].(map[string]string)
	if !ok {
		return false
	}
	return meta["event_kind"] == "outcome"
}

// parseApprovalAnswers 解析审批答案为 bool。
// Python 中 answers 为 list[dict]，每个 dict 包含 label 字段
func parseApprovalAnswers(answers []ApprovalAnswer) bool {
	for _, ans := range answers {
		for _, opt := range ans.SelectedOptions {
			if opt == "接收" || opt == "Accept" || opt == "approve" || opt == "执行" {
				return true
			}
		}
	}
	return false
}

// isEvolutionApprovalEventLocal 判断是否为演进审批事件（内联，避免循环导入 helpers）。
// 对齐 Python: is_evolution_approval_event()
func isEvolutionApprovalEventLocal(evt map[string]any) bool {
	return eventTypeLocal(evt) == "chat.ask_user_question"
}

// isEvolutionOutcomeEventLocal 判断是否为演进结果事件（内联）。
// 对齐 Python: is_evolution_outcome_event()
func isEvolutionOutcomeEventLocal(evt map[string]any) bool {
	return evolutionEventKindLocal(evt) == "outcome"
}

// eventTypeLocal 从事件中提取 event_type（内联）。
func eventTypeLocal(evt map[string]any) string {
	payload := eventPayloadDictLocal(evt)
	if payload == nil {
		return ""
	}
	if t, ok := payload["event_type"].(string); ok {
		return t
	}
	return ""
}

// eventPayloadDictLocal 从事件提取 payload dict（内联）。
func eventPayloadDictLocal(evt map[string]any) map[string]any {
	if evt == nil {
		return nil
	}
	if payload, ok := evt["payload"].(map[string]any); ok {
		return payload
	}
	// evt 本身可能就是 payload（从 OutputSchema.Payload 提取）
	return evt
}

// evolutionEventKindLocal 判断事件类别（内联）。
// 对齐 Python: evolution_event_kind()
func evolutionEventKindLocal(evt map[string]any) string {
	payload := eventPayloadDictLocal(evt)
	if meta, ok := payload["_evolution_meta"].(map[string]any); ok {
		if kind, ok := meta["event_kind"].(string); ok && strings.TrimSpace(kind) != "" {
			return kind
		}
	}
	if isEvolutionApprovalEventLocal(evt) {
		return "approval"
	}
	return "progress"
}

// evolutionOutcomeFromEventLocal 提取演进结果（内联）。
// 对齐 Python: evolution_outcome_from_event()
func evolutionOutcomeFromEventLocal(evt map[string]any) map[string]string {
	payload := eventPayloadDictLocal(evt)
	if payload == nil {
		return map[string]string{"status": "completed", "message": ""}
	}

	meta, _ := evt["_evolution_meta"].(map[string]any)
	var metaStatus any
	if meta != nil {
		metaStatus = meta["status"]
	}

	status := "completed"
	if s, ok := evt["status"].(string); ok && strings.TrimSpace(s) != "" {
		status = strings.TrimSpace(strings.ToLower(s))
	} else if ms, ok := metaStatus.(string); ok && strings.TrimSpace(ms) != "" {
		status = strings.TrimSpace(strings.ToLower(ms))
	}
	if status == "" {
		status = "completed"
	}

	message := ""
	if m, ok := evt["message"].(string); ok {
		message = m
	} else if c, ok := evt["content"].(string); ok {
		message = c
	}

	return map[string]string{"status": status, "message": message}
}
