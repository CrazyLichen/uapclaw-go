package extract

import (
	"context"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractLongTermMemory 从对话消息中提取长期记忆碎片。
//
// 流程：
//  1. 拼接 history_messages + messages 为 reference_str
//  2. 拼接 user 角色消息为 input_msg_str
//  3. scopeConfig 为 nil 时使用默认值
//  4. 调用 buildTimeContext 构建中文周范围
//  5. 通过 PromptApplier 加载 fragment_memory_prompt 模板
//  6. 调用 LLM + JsonOutputParser 解析，最多重试 retries 次
//  7. 返回 map[string]any，预期键：
//     has_explict_instruct / instruct_memories / user_profile / semantic_memory / episodic_memory
//
// Python: LongTermMemoryExtractor.extract_long_term_memory
func ExtractLongTermMemory(
	ctx context.Context,
	params *ExtractMemoryParams,
	timestamp string,
	scopeConfig *config.MemoryScopeConfig,
	retries int,
) (map[string]any, error) {
	// 步骤 1-2：拼接对话上下文（对齐 Python: reference_str / input_msg_str）
	// Python: for msg in extract_memory_paras.history_messages:
	//   reference_str += f"{msg.name or msg.role}: {msg.content}\n"
	// Python: for msg in extract_memory_paras.messages:
	//   reference_str += f"{msg.name or msg.role}: {msg.content}\n"
	//   if msg.role == "user": input_msg_str += f"{msg.name or msg.role}: {msg.content}\n"
	// 修复 S-38: Python 的 msg.content 对多模态消息（list[Union[str,dict]]）在 f-string 中
	// 会转为 str(list)，Go 的 Text() 对多模态消息返回空串导致内容丢失。
	// 使用 safeContentText 辅助函数，多模态消息回退到 String()。
	referenceStr := ""
	inputMsgStr := ""
	for _, msg := range params.HistoryMessages {
		name := msg.GetName()
		if name == "" {
			name = string(msg.GetRole().String())
		}
		referenceStr += fmt.Sprintf("%s: %s\n", name, safeContentText(msg))
	}
	for _, msg := range params.Messages {
		name := msg.GetName()
		if name == "" {
			name = string(msg.GetRole().String())
		}
		referenceStr += fmt.Sprintf("%s: %s\n", name, safeContentText(msg))
		if msg.GetRole() == schema.RoleTypeUser {
			inputMsgStr += fmt.Sprintf("%s: %s\n", name, safeContentText(msg))
		}
	}

	// 步骤 3：scopeConfig nil 时使用默认值（对齐 Python: if not scope_config: scope_config = MemoryScopeConfig()）
	if scopeConfig == nil {
		scopeConfig = config.DefaultMemoryScopeConfig()
	}

	// 步骤 4：构建时间上下文（对齐 Python: current_week = LongTermMemoryExtractor._build_time_context(timestamp)）
	currentWeek := buildTimeContext(timestamp)

	// 步骤 5：加载提示词模板（对齐 Python: PromptApplier().apply("fragment_memory_prompt", {...})）
	userPrompt, err := prompts.DefaultApplier().Apply("fragment_memory_prompt", map[string]any{
		"conversation_time":          timestamp,
		"input_messages":             inputMsgStr,
		"reference_messages":         referenceStr,
		"user_profile_definition":    scopeConfig.UserProfileDefinition,
		"semantic_memory_definition": scopeConfig.SemanticMemoryDefinition,
		"episodic_memory_definition": scopeConfig.EpisodicMemoryDefinition,
		"current_week":               currentWeek,
	})
	if err != nil {
		return nil, fmt.Errorf("加载长期记忆提取提示词模板失败: %w", err)
	}

	// 步骤 6：构造消息（对齐 Python: model_input = [{"role": "user", "content": prompt_content}]）
	formatted := prompt.NewPromptTemplate("fragment_memory_prompt_user", userPrompt)
	messages, err := formatted.ToMessages()
	if err != nil {
		return nil, fmt.Errorf("构造长期记忆提取消息失败: %w", err)
	}
	msgsParam := model_clients.NewMessagesParam(messages...)

	// 步骤 7：LLM 调用 + JSON 解析（对齐 Python: for attempt in range(retries)）
	parser := output_parsers.NewJsonOutputParser()

	for attempt := 0; attempt < retries; attempt++ {
		response, invokeErr := params.BaseModel.Invoke(ctx, msgsParam,
			model_clients.WithInvokeOutputParser(parser))
		if invokeErr != nil {
			// 对齐 Python: invoke 异常向上传播（不在 except JSONDecodeError 内）
			return nil, fmt.Errorf("长期记忆提取 LLM 调用失败: %w", invokeErr)
		}

		parsedResult := response.ParserContent
		if parsedResult == nil {
			if attempt < retries-1 {
				continue
			}
			// 对齐 Python: 全部重试失败返回空 dict
			// Python: memory_logger.error("Long term memory extractor model output format error",
			//   event_type=LogEventType.MEMORY_PROCESS, exception=str(e))
			logger.Error(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Str("exception", "JSON解析结果为nil").
				Msg("长期记忆提取模型输出格式错误")
			return map[string]any{}, nil
		}

		result, ok := parsedResult.(map[string]any)
		if !ok {
			if attempt < retries-1 {
				continue
			}
			logger.Error(logComponent).
				Str("event_type", "MEMORY_PROCESS").
				Str("exception", fmt.Sprintf("JSON解析结果类型非map[string]any，实际: %T", parsedResult)).
				Msg("长期记忆提取模型输出格式错误")
			return map[string]any{}, nil
		}

		return result, nil
	}

	return map[string]any{}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildTimeContext 将 ISO 时间戳转换为中文周范围字符串。
//
// 输出格式: "{年}年{月}月{日}日(周一)～{年}年{月}月{日}日(周日)（即{MM.DD}～{MM.DD}）"
// 解析失败时原样返回 timestamp（对齐 Python: except (ValueError, TypeError): return timestamp）。
//
// Python: LongTermMemoryExtractor._build_time_context
func buildTimeContext(timestamp string) string {
	// 尝试多种格式解析（对齐 Python: datetime.fromisoformat）
	formats := []string{
		"2006-01-02T15:04:05Z07:00",
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05",
		"2006-01-02 15:04:05",
		"2006-01-02",
	}

	var dt time.Time
	var parseErr error
	for _, layout := range formats {
		dt, parseErr = time.Parse(layout, timestamp)
		if parseErr == nil {
			break
		}
	}
	if parseErr != nil {
		// 对齐 Python: except (ValueError, TypeError): return timestamp
		return timestamp
	}

	// 对齐 Python: monday = dt - timedelta(days=dt.weekday())
	// Go 中 time.Weekday() 周日=0，Python 中 weekday() 周一=0
	// 需要转换：Python weekday() = (Go Weekday() + 6) % 7
	weekday := int(dt.Weekday())
	pyWeekday := (weekday + 6) % 7 // 0=周一, 6=周日，对齐 Python
	monday := dt.AddDate(0, 0, -pyWeekday)
	sunday := monday.AddDate(0, 0, 6)

	// 对齐 Python 输出格式：
	// f"{monday.year}年{monday.month}月{monday.day}日(周一)～"
	// f"{sunday.year}年{sunday.month}月{sunday.day}日(周日)"
	// f"（即{monday.strftime('%m.%d')}～{sunday.strftime('%m.%d')}）"
	return fmt.Sprintf("%d年%d月%d日(周一)～%d年%d月%d日(周日)（即%s～%s）",
		monday.Year(), monday.Month(), monday.Day(),
		sunday.Year(), sunday.Month(), sunday.Day(),
		monday.Format("01.02"), sunday.Format("01.02"))
}

// safeContentText 安全提取消息文本内容。
// 纯文本消息使用 Text()，多模态消息（含图片/视频等）回退到 String()，
// 对齐 Python 中 msg.content 在 f-string 中的行为（str(list) 不丢失内容）。
func safeContentText(msg schema.BaseMessage) string {
	c := msg.GetContent()
	if c.IsText() {
		return c.Text()
	}
	return c.String()
}
