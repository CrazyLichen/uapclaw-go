package adapter

import (
	"context"
	"strings"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// recentMessageWindow recap 取最近消息的窗口大小。
// 对齐 Python: RECENT_MESSAGE_WINDOW = 30
const recentMessageWindow = 30

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
	)
	if err != nil {
		logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("CompressContext 压缩失败")
		return map[string]any{"result": "error", "error": err.Error()}, err
	}

	// 对齐 Python: 解析压缩结果
	result := compactResult
	response := map[string]any{"result": result}

	if result == "compressed" {
		// 压缩后重新统计 token
		totalTokens, _ := d.countFullContextTokens(ctx, sessionID)
		stats := modelCtx.Statistic()
		response["stats"] = map[string]any{
			"total_messages":    stats.TotalMessages,
			"total_tokens":     totalTokens,
			"raw_total_tokens": rawTotalTokens,
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
	prompt := buildRecapPrompt(nil)

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

// isApprovalEvent 检查 request_id 是否为审批事件。
// 对齐 Python: is_approval_event() — 检查前缀
func (d *DeepAdapter) isApprovalEvent(requestID string) bool {
	return strings.HasPrefix(requestID, "skill_evolve_") ||
		strings.HasPrefix(requestID, "evolve_simplify_") ||
		strings.HasPrefix(requestID, "team_skill_evolve_")
}

// buildRecapPrompt 构建 recap 提示词。
// 对齐 Python: recap_prompts.build_recap_prompt(memory=None)
func buildRecapPrompt(memory any) string {
	// 对齐 Python: 固定提示词模板
	return `请根据以下对话历史，用 1-3 句话总结本次对话的关键内容和结论。只输出总结内容，不要附加任何其他说明。`
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
func (d *DeepAdapter) getRecentMessages(sessionID string) []map[string]any {
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

	// 获取所有消息，取最近 recentMessageWindow 条
	// GetMessages(size<=0, withHistory=true) 返回全部消息
	allMessages, err := modelCtx.GetMessages(0, true)
	if err != nil || len(allMessages) == 0 {
		return nil
	}

	window := recentMessageWindow
	if len(allMessages) < window {
		window = len(allMessages)
	}

	recent := allMessages[len(allMessages)-window:]
	result := make([]map[string]any, 0, len(recent))
	for _, msg := range recent {
		entry := map[string]any{
			"role":    msg.GetRole().String(),
			"content": msg.GetContent().Text(),
		}
		result = append(result, entry)
	}
	return result
}

// callModelForRecap 调用模型生成 recap。
// 对齐 Python: _call_model_for_recap() (line 5611-5663)
// 不传 system prompt，prompt 作为最后一条 user message。
func (d *DeepAdapter) callModelForRecap(ctx context.Context, messages []map[string]any, prompt string) (string, error) {
	if d.model == nil {
		logger.Error(logComponent).Msg("callModelForRecap: 无可用模型实例")
		return "", nil
	}

	// 构建消息列表：原始消息 + recap 提示词作为最后一条 user message
	recapMessages := make([]llmschema.BaseMessage, 0, len(messages)+1)
	for _, msg := range messages {
		content, _ := msg["content"].(string)
		if strings.TrimSpace(content) == "" {
			continue
		}
		role, _ := msg["role"].(string)
		switch role {
		case "user":
			recapMessages = append(recapMessages, llmschema.NewUserMessage(content))
		case "assistant":
			recapMessages = append(recapMessages, llmschema.NewAssistantMessage(content))
		default:
			recapMessages = append(recapMessages, llmschema.NewUserMessage(content))
		}
	}
	// 对齐 Python: prompt 作为最后一条 user message 追加
	recapMessages = append(recapMessages, llmschema.NewUserMessage(prompt))

	// 调用模型，对齐 Python: model.invoke(messages, max_tokens=300, temperature=0)
	result, err := d.model.Invoke(ctx, model_clients.NewMessagesParam(recapMessages...))
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

	// 对齐 Python: context.token_counter().count(system_prompt) + context token 计数
	tc := modelCtx.TokenCounter()
	if tc == nil {
		// 无 token 计数器时使用统计值
		stats := modelCtx.Statistic()
		if stats != nil {
			return stats.TotalTokens, nil
		}
		return 0, nil
	}

	// 获取模型名称用于 token 计数
	modelName := ""
	if reactAgent.Config() != nil {
		modelName = reactAgent.Config().ModelName()
	}

	// 计算 system prompt token 数
	totalTokens := 0
	pb := reactAgent.PromptBuilder()
	if pb != nil {
		systemPrompt := pb.Build()
		if systemPrompt != "" {
			count, _ := tc.Count(systemPrompt, modelName)
			totalTokens += count
		}
	}

	// 加上上下文消息的 token 数
	messages, err := modelCtx.GetMessages(0, true)
	if err == nil {
		for _, msg := range messages {
			text := msg.GetContent().Text()
			if text != "" {
				count, _ := tc.Count(text, modelName)
				totalTokens += count
			}
		}
	}

	return totalTokens, nil
}
