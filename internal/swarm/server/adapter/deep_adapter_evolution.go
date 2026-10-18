package adapter

import (
	"context"
	"fmt"
	"strings"
	"time"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	evolutionlogic "github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter/evolution/logic"
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
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("CompressContext compression failed")
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
		Msg("CompressContext completed")

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
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("GetContextUsage failed")
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

	logger.Info(logComponent).Str("session_id", sessionID).Msg("GenerateRecap completed")
	return map[string]any{"status": "ok", "summary": strings.TrimSpace(summaryText)}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// watchEvolutionAndPush 后台轮询 SkillEvolutionRail 的被动演进事件并推送。
// ✅ 已回填（对齐 Python: _watch_evolution_and_push() interface_deep.py L5725-5912）
//
// S-03: 对齐 Python 轮询语义——wait=false 非阻塞 drain + 1s 空闲休眠 + 事件超时检测；
// M-05: 每个退出路径调用 cleanupEvolutionRail 清理后台任务（对齐 Python _cleanup_evolution_rail）。
//
// 主循环结构（对齐 Python）：
//  1. auto_scan 检查（入口 + 每次循环）
//  2. 首个可见 progress 时推送 "start" status
//  3. 推送中间 "progress" status
//  4. 审批事件 → 推送 approval + "end"，清理后返回
//  5. outcome 事件 → hidden terminal 映射 + start/end，清理后返回
//  6. 空闲超时 → 推送 "end" hidden timeout 消息，清理后返回
func (d *DeepAdapter) watchEvolutionAndPush(ctx context.Context, sessionID string, channelID string, requestID string) error {
	if d.skillEvolutionRail == nil {
		return nil
	}
	// Python: if not getattr(self._skill_evolution_rail, "auto_scan", True): return
	if !d.skillEvolutionRail.AutoScan() {
		return nil
	}

	// 推送 evolution status（内联对齐 Python: _push_status）
	pushStatus := func(status, stage, message string) {
		sendPush := d.getSendPushFunc()
		if sendPush == nil {
			logger.Warn(logComponent).Str("session_id", sessionID).Msg("watchEvolutionAndPush: sendPushFunc not injected")
			return
		}
		update := evolutionlogic.BuildEvolutionStatusUpdate(requestID, status, stage, message)
		payload := map[string]any{
			"event_type": "chat.evolution_status",
			"status":     update.Status,
			"stage":      update.Stage,
			"message":    update.Message,
		}
		// Python: include_payload_request_id=False
		msg := sessionmd.BuildServerPushMessage(sessionID, requestID, payload, channelID)
		if err := sendPush(ctx, msg); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).
				Str("status", status).Str("stage", stage).
				Msg("watchEvolutionAndPush: failed to push evolution status")
		}
	}

	// 清理后台任务（对齐 Python: _cleanup_evolution_rail）
	cleanupEvolutionRail := func() {
		if d.skillEvolutionRail == nil {
			return
		}
		if err := d.skillEvolutionRail.CleanupBackgroundTasks(); err != nil {
			logger.Warn(logComponent).Err(err).Str("request_id", requestID).Str("session_id", sessionID).
				Msg("evolution cleanup failed")
		}
	}

	active := false
	lastEventAt := time.Now()
	// Python: event_timeout_sec = resolve_evolution_event_timeout_sec(rail, fallback_sec=TEAM_EVOLUTION_EVENT_TIMEOUT_SEC)
	eventTimeoutSec := d.skillEvolutionRail.GetEvolutionTotalTimeoutSecs()
	if eventTimeoutSec <= 0 {
		eventTimeoutSec = evolutionlogic.TeamEvolutionEventTimeoutSec
	} else {
		eventTimeoutSec += evolutionlogic.TeamEvolutionEventTimeoutGraceSec
	}

	ticker := time.NewTicker(time.Duration(evolutionlogic.TeamEvolutionIdleSleepSec * float64(time.Second)))
	defer ticker.Stop()

	for {
		// Python: if self._skill_evolution_rail is None: return
		if d.skillEvolutionRail == nil {
			return nil
		}
		// Python: if not getattr(..., "auto_scan", True): if active → push end hidden; cleanup; return
		if !d.skillEvolutionRail.AutoScan() {
			if active {
				pushStatus("end", evolutionlogic.TeamEvolutionHiddenStage, "")
			}
			cleanupEvolutionRail()
			return nil
		}

		select {
		case <-ctx.Done():
			// Python: except CancelledError → cleanup; raise
			cleanupEvolutionRail()
			return ctx.Err()
		case <-ticker.C:
		}

		// S-03: 对齐 Python wait=False 非阻塞 drain
		events := d.skillEvolutionRail.DrainPendingApprovalEvents(false, nil)
		if len(events) == 0 {
			idleFor := time.Since(lastEventAt).Seconds()
			if idleFor >= eventTimeoutSec {
				logger.Warn(logComponent).
					Str("request_id", requestID).Str("session_id", sessionID).
					Float64("idle_for", idleFor).
					Msg("evolution watcher timed out")
				if active {
					message := fmt.Sprintf("Evolution analysis timed out after %.0fs without host events", eventTimeoutSec)
					pushStatus("end", evolutionlogic.TeamEvolutionHiddenStage, message)
				}
				cleanupEvolutionRail()
				return nil
			}
			continue
		}
		lastEventAt = time.Now()

		// 将 []*stream.OutputSchema 转为 []map[string]any
		eventsMap := make([]map[string]any, 0, len(events))
		for _, e := range events {
			dict := outputSchemaToDict(e)
			if dict != nil {
				eventsMap = append(eventsMap, dict)
			}
		}

		// Python: visible_progress_statuses = visible_evolution_progress_from_events(events)
		visibleProgressStatuses := evolutionlogic.VisibleEvolutionProgressFromEvents(eventsMap)

		// Python: 首个可见 progress → push "start"
		justStarted := false
		if !active && len(visibleProgressStatuses) > 0 {
			startStage := visibleProgressStatuses[0].Stage
			startMessage := visibleProgressStatuses[0].Message
			pushStatus("start", startStage, startMessage)
			active = true
			justStarted = true
		}

		// Python: await push_evolution_progress(push_context, rid, events, ...)——批量推送非审批/结果事件
		sendPush := d.getSendPushFunc()
		if sendPush != nil {
			for _, evt := range eventsMap {
				if evolutionlogic.IsEvolutionApprovalEvent(evt) || evolutionlogic.IsEvolutionOutcomeEvent(evt) || evolutionlogic.TeamEvolutionTerminalProgress(evt) != nil {
					continue
				}
				msg := sessionmd.BuildServerPushMessage(sessionID, requestID, evt, channelID)
				_ = sendPush(ctx, msg)
			}
		}

		// Python: 中间 "progress" status（just_started 时跳过第一个——已在 start 中推送）
		progressStatusesToPush := visibleProgressStatuses
		if justStarted && len(visibleProgressStatuses) > 1 {
			progressStatusesToPush = visibleProgressStatuses[1:]
		}
		for _, ps := range progressStatusesToPush {
			if ps.Terminal {
				continue
			}
			pushStatus("progress", ps.Stage, ps.Message)
		}

		// Python: approval_events → push approval + "end"，清理后返回
		var approvalEvents []map[string]any
		for _, evt := range eventsMap {
			if evolutionlogic.IsEvolutionApprovalEvent(evt) {
				approvalEvents = append(approvalEvents, evt)
			}
		}
		if len(approvalEvents) > 0 {
			if !active {
				pushStatus("start", "approval_required", "")
				active = true
			}
			for _, evt := range approvalEvents {
				d.pushEventToFrontend(ctx, eventFromDict(evt), sessionID, channelID, requestID)
			}
			pushStatus("end", "approval_required", "")
			cleanupEvolutionRail()
			return nil
		}

		// Python: outcomes → hidden terminal 映射 + start/end，清理后返回
		var outcomes []map[string]string
		for _, evt := range eventsMap {
			if evolutionlogic.IsEvolutionOutcomeEvent(evt) {
				outcomes = append(outcomes, evolutionlogic.EvolutionOutcomeFromEvent(evt))
			}
		}
		if len(outcomes) > 0 {
			outcome := outcomes[len(outcomes)-1]
			stage := strings.TrimSpace(strings.ToLower(outcome["status"]))
			if stage == "" {
				stage = "completed"
			}
			message := outcome["message"]
			endStage := stage
			if _, hidden := evolutionlogic.TeamEvolutionHiddenTerminalStages[stage]; hidden {
				endStage = evolutionlogic.TeamEvolutionHiddenStage
			}
			if !active && endStage == evolutionlogic.TeamEvolutionHiddenStage {
				// Python: if not active and end_stage == "hidden": cleanup; return
				cleanupEvolutionRail()
				return nil
			}
			if !active {
				pushStatus("start", endStage, message)
				active = true
			}
			if message == "" {
				message = "Evolution analysis completed"
			}
			pushStatus("end", endStage, message)
			cleanupEvolutionRail()
			return nil
		}

		// Python: terminal_progress → end_stage 映射 + start/end，清理后返回
		terminalProgress := evolutionlogic.TerminalProgressFromEvents(eventsMap)
		if len(terminalProgress) > 0 {
			terminal := terminalProgress[len(terminalProgress)-1]
			endStage := evolutionlogic.TerminalStage(terminal.Terminal)
			if endStage == "" {
				endStage = evolutionlogic.TeamEvolutionNoopStage
			}
			if _, hidden := evolutionlogic.TeamEvolutionHiddenTerminalStages[endStage]; hidden {
				endStage = evolutionlogic.TeamEvolutionHiddenStage
			}
			if !active && endStage == evolutionlogic.TeamEvolutionHiddenStage {
				cleanupEvolutionRail()
				return nil
			}
			message := terminal.Terminal["message"]
			if !active {
				pushStatus("start", endStage, message)
				active = true
			}
			pushStatus("end", endStage, message)
			cleanupEvolutionRail()
			return nil
		}
	}
}

// eventFromDict 将 map 形式的事件包装回 OutputSchema（approval 推送用）。
// pushEventToFrontend 需要 *stream.OutputSchema，此处仅传递 payload。
func eventFromDict(dict map[string]any) *stream.OutputSchema {
	return &stream.OutputSchema{Payload: dict}
}

// onEvolutionWatcherDone evolution 观察任务完成回调。
// ✅ 已回填（对齐 Python: _on_evolution_watcher_done()）
func (d *DeepAdapter) onEvolutionWatcherDone(sessionID string) {
	logger.Info(logComponent).Str("session_id", sessionID).Msg("onEvolutionWatcherDone: evolution watcher done")
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
func (d *DeepAdapter) handleEvolutionApproval(ctx context.Context, requestID string, answers []ApprovalAnswer) bool {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("request_id", requestID).Msg("handleEvolutionApproval: SkillEvolutionRail not initialized")
		return false
	}

	// 解析 answers 为 approve/reject
	approved := parseApprovalAnswers(answers)

	if approved {
		err := d.skillEvolutionRail.ApproveRecord(ctx, requestID)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("handleEvolutionApproval: ApproveRecord failed")
			return false
		}
		logger.Info(logComponent).Str("request_id", requestID).Msg("handleEvolutionApproval: approved")
		return true
	}

	err := d.skillEvolutionRail.RejectRecord(ctx, requestID)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("handleEvolutionApproval: RejectRecord failed")
		return false
	}
	logger.Info(logComponent).Str("request_id", requestID).Msg("handleEvolutionApproval: rejected")
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
		logger.Error(logComponent).Msg("callModelForRecap: no model instance available")
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
		logger.Error(logComponent).Err(err).Msg("callModelForRecap model invocation failed")
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
// ✅ 已回填：通过 d.getSendPushFunc() 推送，避免 adapter→server 循环依赖
func (d *DeepAdapter) pushEventToFrontend(ctx context.Context, event *stream.OutputSchema, sessionID string, channelID string, requestID string) {
	evt := outputSchemaToDict(event)
	if evt == nil {
		return
	}
	sendPush := d.getSendPushFunc()
	if sendPush == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("pushEventToFrontend: sendPushFunc not injected")
		return
	}

	evtKind := evolutionlogic.EvolutionEventKind(evt)
	switch evtKind {
	case "approval":
		// Python: await push_evolution_event(push_context, rid, evt, build_server_push_message)
		msg := sessionmd.BuildServerPushMessage(sessionID, requestID, evt, channelID)
		if err := sendPush(ctx, msg); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("failed to push approval event")
		}
	case "outcome":
		// Python: await push_evolution_status(push_context, update, build_server_push_message)
		outcome := evolutionlogic.EvolutionOutcomeFromEvent(evt)
		payload := map[string]any{
			"event_type": "chat.evolution_status",
			"status":     outcome["status"],
			"stage":      outcome["status"],
			"message":    outcome["message"],
			"request_id": requestID,
		}
		msg := sessionmd.BuildServerPushMessage(sessionID, requestID, payload, channelID)
		if err := sendPush(ctx, msg); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("failed to push outcome status")
		}
	default:
		logger.Debug(logComponent).Str("session_id", sessionID).Str("event_kind", evtKind).Msg("evolution progress event already handled in batch push")
	}
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
