package rails

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	jsonrepair "github.com/RealAlexandreAI/json-repair"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// EnsureJSONArguments 确保工具调用参数为合法 JSON 字符串，对齐 Python: _ensure_json_arguments
//
// 三阶段修复：
// 1. 直接 json.Valid → 成功返回原文
// 2. json_repair.RepairJSON → 修复后验证
// 3. fixMissingQuotes 规则修复 → 修复后验证
// 兜底：返回 "{}"
func ensureJSONArguments(arguments string) string {
	if arguments == "" {
		return "{}"
	}
	text := strings.TrimSpace(arguments)
	if text == "" {
		return "{}"
	}

	// 阶段 1: 直接解析验证，对齐 Python: json.loads(_arguments)
	if json.Valid([]byte(text)) {
		return arguments
	}

	// 阶段 2: json_repair 库，对齐 Python: json_repair.loads(_arguments)
	repaired, repairErr := jsonrepair.RepairJSON(text)
	// jsonrepair 可能：
	// - 返回修改后的合法 JSON（如补括号、补引号）
	// - 把整个字符串包成 JSON string（对非结构输入如 "not json at all"）
	// - 原样返回无法修复的输入
	// 工具参数必须是 JSON object（map），所以用 isJSONObjectMap 验证
	if repairErr == nil && repaired != "" {
		if isJSONObjectMap(repaired) {
			if repaired != text {
				logger.Info(logComponent).Str("stage", "json_repair").Str("outcome", "success").Msg("ensureJSONArguments 阶段2修复成功")
			}
			return repaired
		}
	}
	logger.Warn(logComponent).Str("stage", "json_repair").Str("outcome", "failed").Msg("ensureJSONArguments 阶段2修复失败")

	// 阶段 3: 规则修复，对齐 Python: _fix_missing_quotes
	fixed := fixMissingQuotes(text)
	if fixed != text {
		if isJSONObjectMap(fixed) {
			logger.Info(logComponent).Str("stage", "rule_fix").Str("outcome", "success").Msg("ensureJSONArguments 阶段3修复成功")
			return fixed
		}
	}
	logger.Warn(logComponent).Str("stage", "rule_fix").Str("outcome", "failed").Msg("ensureJSONArguments 阶段3修复失败")

	logger.Warn(logComponent).Str("outcome", "failed_all_stages").Msg("ensureJSONArguments 所有阶段修复失败")
	return "{}"
}

// FixMissingQuotes 尝试修复 JSON 中缺失的引号，对齐 Python: _fix_missing_quotes
//
// 三种修复模式：
// 1. Windows 路径：{"path": D:/work/file.txt} → {"path": "D:/work/file.txt"}
// 2. 缺结束引号：{"query": hello} → {"query": "hello"}
// 3. 缺键引号：{query: "hello"} → {"query": "hello"}
func fixMissingQuotes(jsonStr string) string {
	s := strings.TrimSpace(jsonStr)

	// 模式 1: 修复 Windows 路径，对齐 Python: re.sub(r':\s+([A-Za-z]:/[^\{\[]*?)(?=\s*[,\}\]])', ...)
	// Go regexp 不支持 lookahead，改为匹配包含后续逗号/闭括号，然后手动处理
	winPathRe := regexp.MustCompile(`:\s+([A-Za-z]:/[^\{\[]*?)(\s*[,\}\]])`)
	s = winPathRe.ReplaceAllString(s, `: "$1"$2`)

	// 模式 2: 修复缺结束引号的值，对齐 Python: re.sub(r':\s+(?!"|true|false|null|\d+|{|\[|:|"|[A-Za-z]:/)([^\s,\}\[\]"]+?)(?=\s*[,}\]])', ...)
	// Go regexp 不支持 lookahead 和 negative lookahead，简化为匹配冒号后非关键字/非引号的裸值
	missingEndQuoteRe := regexp.MustCompile(`:\s+([^\s,\}\[\]":{|]+?)(\s*[,}\]])`)
	s = missingEndQuoteRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := missingEndQuoteRe.FindStringSubmatch(m)
		if len(sub) > 1 {
			val := sub[1]
			// 跳过已经是合法值的情况
			lower := strings.ToLower(val)
			if lower == "true" || lower == "false" || lower == "null" {
				return m
			}
			// 跳过数字
			if isNumeric(val) {
				return m
			}
			// 跳过 Windows 路径（已在模式 1 处理）
			if len(val) > 2 && val[1] == ':' && (val[0] >= 'A' && val[0] <= 'Z' || val[0] >= 'a' && val[0] <= 'z') {
				return m
			}
			return fmt.Sprintf(`: "%s"%s`, sub[1], sub[2])
		}
		return m
	})

	// 模式 3: 修复缺键引号，对齐 Python: re.sub(r'{\s+([a-zA-Z_][a-zA-Z0-9_]*)\s*:', '{"$1":', s)
	// Python 仅匹配 { 后有空格的裸键，但 LLM 输出中 {key: 更常见，扩展支持
	// 使用 ReplaceAllStringFunc 过滤已有引号的键，避免重复加引号
	missingKeyQuoteRe := regexp.MustCompile(`([{\s,])([a-zA-Z_][a-zA-Z0-9_]*)\s*:`)
	s = missingKeyQuoteRe.ReplaceAllStringFunc(s, func(m string) string {
		sub := missingKeyQuoteRe.FindStringSubmatch(m)
		if len(sub) > 2 {
			// 检查键前面是否有引号（如 {"key":），如果有则不加引号
			prefix := sub[1]
			key := sub[2]
			// prefix 的最后一个字符如果是 " 则跳过（已经有引号）
			if strings.HasSuffix(prefix, `"`) {
				return m
			}
			return fmt.Sprintf(`%s"%s":`, prefix, key)
		}
		return m
	})

	return s
}

// FixIncompleteToolContext 修复不完整的工具上下文，对齐 Python: _fix_incomplete_tool_context
//
// 遍历消息列表，确保每个 tool_call 都有对应的 ToolMessage，
// 缺失时插入占位 ToolMessage（内容为"工具执行被中断"）。
func fixIncompleteToolContext(ctx context.Context, modelCtx ceinterface.ModelContext, getPromptLanguage func() string) error {
	messages := modelCtx.PopMessages(0, true)
	if len(messages) == 0 {
		return nil
	}

	var toolIDCache []toolCacheEntry
	toolMessageCache := make(map[string]llmschema.BaseMessage)

	for i := range messages {
		msg := messages[i]
		role := msg.GetRole()

		switch role {
		case llmschema.RoleTypeAssistant:
			asstMsg, ok := msg.(*llmschema.AssistantMessage)
			if !ok {
				// 非 AssistantMessage 类型，当作普通消息处理
				if len(toolIDCache) > 0 {
					insertPlaceholderToolMessages(ctx, modelCtx, toolIDCache, toolMessageCache, getPromptLanguage)
					toolIDCache = nil
				}
				_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{msg})
				continue
			}

			if len(toolIDCache) > 0 {
				// 有未匹配的 tool_call → 插入占位 ToolMessage，对齐 Python: logger.info("Fixed incomplete tool context with placeholder messages")
				insertPlaceholderToolMessages(ctx, modelCtx, toolIDCache, toolMessageCache, getPromptLanguage)
				toolIDCache = nil
				logger.Info(logComponent).Msg("修复不完整工具上下文：已插入占位 ToolMessage")
			}

			// 记录新的 tool_calls，对齐 Python: tool_id_cache.append({"tool_call_id": ..., "tool_name": ...})
			if asstMsg.ToolCalls != nil {
				for _, tc := range asstMsg.ToolCalls {
					// 修复 Arguments，对齐 Python: arguments = self._ensure_json_arguments(arguments)
					if tc.Arguments != "" {
						tc.Arguments = ensureJSONArguments(tc.Arguments)
					}
					toolIDCache = append(toolIDCache, toolCacheEntry{
						toolCallID: tc.ID,
						toolName:   tc.Name,
					})
				}
			}

			_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{msg})

		case llmschema.RoleTypeTool:
			toolMsg, ok := msg.(*llmschema.ToolMessage)
			if !ok {
				if len(toolIDCache) > 0 {
					insertPlaceholderToolMessages(ctx, modelCtx, toolIDCache, toolMessageCache, getPromptLanguage)
					toolIDCache = nil
				}
				_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{msg})
				continue
			}

			if len(toolIDCache) == 0 {
				// 无待匹配的 tool_call → 存入 toolMessageCache，对齐 Python: tool_message_cache[messages[i].tool_call_id] = messages[i]
				toolMessageCache[toolMsg.ToolCallID] = msg
				continue
			}

			// 尝试匹配 toolIDCache 中的第一个，对齐 Python: if messages[i].tool_call_id == tool_id_cache[0]["tool_call_id"]
			if toolMsg.ToolCallID == toolIDCache[0].toolCallID {
				_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{msg})
				toolIDCache = toolIDCache[1:]
			} else {
				toolMessageCache[toolMsg.ToolCallID] = msg
			}

		default:
			if len(toolIDCache) > 0 {
				insertPlaceholderToolMessages(ctx, modelCtx, toolIDCache, toolMessageCache, getPromptLanguage)
				toolIDCache = nil
				logger.Info(logComponent).Msg("修复不完整工具上下文：已插入占位 ToolMessage")
			}
			_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{msg})
		}
	}

	// 遍历结束后仍有未匹配的 tool_call
	if len(toolIDCache) > 0 {
		insertPlaceholderToolMessages(ctx, modelCtx, toolIDCache, toolMessageCache, getPromptLanguage)
		logger.Info(logComponent).Msg("修复不完整工具上下文：尾部已插入占位 ToolMessage")
	}

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toolCacheEntry 待匹配的 tool_call 缓存条目，对齐 Python: tool_id_cache item
type toolCacheEntry struct {
	toolCallID string
	toolName   string
}

// insertPlaceholderToolMessages 为未匹配的 tool_call 插入占位 ToolMessage
// 对齐 Python: await context.add_messages(ToolMessage(content=self._tool_interrupted_message(tool_name), tool_call_id=tool_call_id))
func insertPlaceholderToolMessages(
	ctx context.Context,
	modelCtx ceinterface.ModelContext,
	toolIDCache []toolCacheEntry,
	toolMessageCache map[string]llmschema.BaseMessage,
	getPromptLanguage func() string,
) {
	for _, entry := range toolIDCache {
		if cached, ok := toolMessageCache[entry.toolCallID]; ok {
			_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{cached})
			delete(toolMessageCache, entry.toolCallID)
		} else {
			content := toolInterruptedMessage(entry.toolName, getPromptLanguage)
			placeholder := llmschema.NewToolMessage(entry.toolCallID, content)
			_, _ = modelCtx.AddMessages(ctx, []llmschema.BaseMessage{placeholder})
		}
	}
}

// toolInterruptedMessage 构建语言感知的工具中断消息，对齐 Python: _tool_interrupted_message
func toolInterruptedMessage(toolName string, getPromptLanguage func() string) string {
	if getPromptLanguage != nil {
		lang := getPromptLanguage()
		if lang == "en" {
			return fmt.Sprintf("[Tool interrupted] Tool %s was interrupted by the user and has no result.", toolName)
		}
	}
	return fmt.Sprintf("[工具执行被中断] 工具 %s 执行过程中被用户打断，没有执行结果。", toolName)
}

// isJSONObjectMap 判断字符串是否为合法的 JSON object（map），而非 JSON string/array/number 等
// jsonrepair.RepairJSON 可能把非结构输入包成合法 JSON string，对工具参数无意义
func isJSONObjectMap(s string) bool {
	var m map[string]any
	return json.Unmarshal([]byte(s), &m) == nil
}

// isNumeric 判断字符串是否为数字
func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if c == '-' && i == 0 {
			continue
		}
		if c == '.' && i > 0 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
