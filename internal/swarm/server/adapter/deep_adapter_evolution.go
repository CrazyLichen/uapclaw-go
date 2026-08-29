package adapter

import (
	"context"
	"strings"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// recentMessageWindow recap 取最近消息的窗口大小。
// 对齐 Python: RECENT_MESSAGE_WINDOW = 30
const recentMessageWindow = 30

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CompressContext 触发上下文压缩。
// 对齐 Python: compress_context() (line 5380-5570)
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

	// 对齐 Python: 解析压缩结果
	result := compactResult.Result
	response := map[string]any{"result": result}

	if returnState && compactResult.State != nil {
		response["state"] = compactResult.State
		if compactResult.CompactSummary != "" {
			response["compact_summary"] = compactResult.CompactSummary
		}
	}

	if result == "compressed" {
		// 对齐 Python (L5440-5441): context = context_engine.get_context(session_id=session_id)
		// 压缩后重新获取 context，确保统计的是压缩后的数据
		newModelCtx := contextEngine.GetContext("default_context", sessionID)
		if newModelCtx != nil {
			// 对齐 Python (L5443-5445): total_tokens = await self._count_full_context_tokens(context, react_agent, session_id)
			totalTokens, _ := d.countFullContextTokens(ctx, sessionID)
			// 对齐 Python (L5447): stats = context.statistic()
			stats := newModelCtx.Statistic()
			response["stats"] = map[string]any{
				"total_messages":   stats.TotalMessages,
				"total_tokens":     totalTokens,
				"raw_total_tokens": rawTotalTokens,
			}
			// 对齐 Python (L5453-5455):
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
// 对齐 Python: get_context_usage() (line 5572-5588)
func (d *DeepAdapter) GetContextUsage(ctx context.Context, sessionID string) (map[string]any, error) {
	if d.instance == nil {
		return nil, nil
	}
	// 对齐 Python: 直接调 instance.get_context_usage()
	usage, err := d.instance.GetContextUsage(ctx, sessionID, "")
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("GetContextUsage 失败")
		return nil, err
	}
	return usage, nil
}

// GenerateRecap 生成会话回顾摘要。
// 对齐 Python: generate_recap() (line 5572-5591)
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
// 对齐 Python: _watch_evolution_and_push() (line 5725-5923)
// ⤵️ 10.6.3-10: 依赖 SkillEvolutionRail
func (d *DeepAdapter) watchEvolutionAndPush(ctx context.Context, sessionID string, requestID string) error {
	// ⤵️ 10.6.3-10: 实现 evolution watcher
	logger.Info(logComponent).Str("session_id", sessionID).Msg("watchEvolutionAndPush 等待 10.6.3-10 回填")
	return nil
}

// onEvolutionWatcherDone evolution 观察任务完成回调。
// 对齐 Python: _on_evolution_watcher_done()
// ⤵️ 10.6.3-10: 依赖 SkillEvolutionRail
func (d *DeepAdapter) onEvolutionWatcherDone(sessionID string) {
	// ⤵️ 10.6.3-10: 清理 evolution watcher
	logger.Info(logComponent).Str("session_id", sessionID).Msg("onEvolutionWatcherDone 等待 10.6.3-10 回填")
}

// buildRecapPrompt 构建 recap 提示词。
// 对齐 Python: recap_prompts.build_recap_prompt(memory: str | None) -> str
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
// 对齐 Python: _handle_evolution_approval() (line 3626-3648)
// ⤵️ 10.6.3-10: 依赖 SkillEvolutionRail
func (d *DeepAdapter) handleEvolutionApproval(requestID string, answers any) bool {
	// ⤵️ 10.6.3-10: 实现 evolution 审批
	logger.Info(logComponent).Str("request_id", requestID).Msg("handleEvolutionApproval 等待 10.6.3-10 回填")
	return false
}

// getRecentMessages 获取最近消息列表。
// 对齐 Python: _get_recent_messages() (line 5593-5609)
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

	// 对齐 Python: all_messages = list(context.get_messages() or [])
	allMessages, err := modelCtx.GetMessages(0, true)
	if err != nil || len(allMessages) == 0 {
		return nil
	}

	// 对齐 Python: return all_messages[-window:]
	window := recentMessageWindow
	if len(allMessages) < window {
		window = len(allMessages)
	}

	return allMessages[len(allMessages)-window:]
}

// callModelForRecap 调用模型生成 recap。
// 对齐 Python: _call_model_for_recap() (line 5611-5663)
// 不传 system prompt，prompt 作为最后一条 user message。
func (d *DeepAdapter) callModelForRecap(ctx context.Context, messages []llmschema.BaseMessage, prompt string) (string, error) {
	if d.model == nil {
		logger.Error(logComponent).Msg("callModelForRecap: 无可用模型实例")
		return "", nil
	}

	// 构建消息列表：原始消息 + recap 提示词作为最后一条 user message
	recapMessages := make([]llmschema.BaseMessage, 0, len(messages)+1)
	for _, msg := range messages {
		// 对齐 Python: role = getattr(msg, "role", None) or ""
		role := msg.GetRole()

		// 对齐 Python: content = getattr(msg, "content", None) or ""
		//   if isinstance(content, list): → 多模态，提取文本部分
		mc := msg.GetContent()
		var content string
		if mc.IsText() {
			content = mc.Text()
		} else {
			// 多模态：拼接 Parts() 中 Type=="text" 的 Text 字段
			// 对齐 Python: " ".join(str(p) for p in content if isinstance(p, str) or (isinstance(p, dict) and p.get("type") == "text"))
			var textParts []string
			for _, part := range mc.Parts() {
				if part.Type == "text" && part.Text != "" {
					textParts = append(textParts, part.Text)
				}
			}
			content = strings.Join(textParts, " ")
		}

		// 对齐 Python: if not content.strip(): continue
		if strings.TrimSpace(content) == "" {
			continue
		}

		// 对齐 Python: if role == "user": UserMessage / elif role == "assistant": AssistantMessage / else: UserMessage
		switch role {
		case llmschema.RoleTypeUser:
			recapMessages = append(recapMessages, llmschema.NewUserMessage(content))
		case llmschema.RoleTypeAssistant:
			recapMessages = append(recapMessages, llmschema.NewAssistantMessage(content))
		default:
			recapMessages = append(recapMessages, llmschema.NewUserMessage(content))
		}
	}
	// 对齐 Python: prompt 作为最后一条 user message 追加
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
// 对齐 Python: _count_full_context_tokens() (line 5665-5723)
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

	// 对齐 Python: token_counter = context.token_counter()
	tc := modelCtx.TokenCounter()

	// 对齐 Python: 无 token_counter 时使用 len // 4 粗估 fallback
	useFallback := tc == nil

	// 获取模型名称用于 token 计数
	modelName := reactAgent.Config().ModelName()

	totalTokens := 0

	// 对齐 Python 步骤1: 计算系统消息的 tokens (L5686-5697)
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
			// 对齐 Python: total_tokens += len(system_prompt) // 4
			totalTokens += len(systemPrompt) / 4
		}
	}

	// 对齐 Python 步骤2: 计算对话消息的 tokens (L5699-5705)
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

	// 对齐 Python 步骤3: 计算工具定义的 tokens (L5707-5721)
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
			// 对齐 Python: tc==nil 时无 count_tools fallback（Python 也不做）
		}
	}

	return totalTokens, nil
}
