package compressor

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractToolName 从 ToolCall 中提取工具名称。
//
// 优先返回 ToolCall.Name，为空时返回空字符串。
// Go 端 ToolCall 结构直接包含 Name 字段（与 Python 的 Function.Name 不同）。
//
// 对应 Python: ContextUtils.extract_tool_name()
func ExtractToolName(toolCall *llm_schema.ToolCall) string {
	if toolCall == nil {
		return ""
	}
	if toolCall.Name != "" {
		return toolCall.Name
	}
	return ""
}

// ResolveToolCallFromMessage 从 ToolMessage 回溯查找对应的 ToolCall 对象。
//
// 通过 ToolMessage 的 ToolCallID 匹配 AssistantMessage.ToolCalls 中的 ID，
// 从后往前遍历 contextMessages 查找最近的匹配。
//
// 对应 Python: ContextUtils.resolve_tool_call_from_message()
func ResolveToolCallFromMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) *llm_schema.ToolCall {
	tm, ok := message.(*llm_schema.ToolMessage)
	if !ok {
		return nil
	}
	toolCallID := tm.ToolCallID
	if toolCallID == "" {
		return nil
	}

	for i := len(contextMessages) - 1; i >= 0; i-- {
		am, ok := contextMessages[i].(*llm_schema.AssistantMessage)
		if !ok {
			continue
		}
		for _, tc := range am.ToolCalls {
			if tc.ID == toolCallID {
				return tc
			}
		}
	}
	return nil
}

// ResolveToolNameFromMessage 从 ToolMessage 回溯查找对应的工具名称。
//
// 内部调用 ResolveToolCallFromMessage 找到 ToolCall，再调用 ExtractToolName 提取名称。
// 未找到时返回空字符串。
//
// 对应 Python: ContextUtils.resolve_tool_name_from_message()
func ResolveToolNameFromMessage(message llm_schema.BaseMessage, contextMessages []llm_schema.BaseMessage) string {
	toolCall := ResolveToolCallFromMessage(message, contextMessages)
	return ExtractToolName(toolCall)
}

// MessageToText 提取消息纯文本内容。
//
// 优先返回 GetContent().Text()，为空时尝试从 Parts 中提取文本。
//
// 对应 Python: FullCompactProcessor._message_to_text() / util.message_to_text()
func MessageToText(msg llm_schema.BaseMessage) string {
	content := msg.GetContent().Text()
	if content != "" {
		return content
	}
	// 尝试从 parts 获取文本
	parts := msg.GetContent().Parts()
	if len(parts) > 0 {
		var texts []string
		for _, p := range parts {
			if p.Text != "" {
				texts = append(texts, p.Text)
			}
		}
		if len(texts) > 0 {
			return strings.Join(texts, "\n")
		}
	}
	return ""
}

// GroupCompletedAPIRoundsMessages 按已完成 API 轮次分组返回消息子列表。
//
// 内部调用 processor.GroupCompletedAPIRounds 获取轮次范围，再按范围切分消息。
//
// 对应 Python: FullCompactProcessor._group_messages_by_api_round()
func GroupCompletedAPIRoundsMessages(messages []llm_schema.BaseMessage) [][]llm_schema.BaseMessage {
	ranges := processor.GroupCompletedAPIRounds(messages)
	groups := make([][]llm_schema.BaseMessage, 0, len(ranges))
	for _, r := range ranges {
		groups = append(groups, messages[r[0]:r[1]])
	}
	return groups
}

// MessageSignature 生成消息签名（用于去重）。
//
// 格式为 "role|text|toolCallIDs"，其中 toolCallIDs 以 "|" 连接。
//
// 对应 Python: FullCompactProcessor._message_signature()
func MessageSignature(msg llm_schema.BaseMessage) string {
	var toolCallIDs []string
	if am, ok := msg.(*llm_schema.AssistantMessage); ok {
		for _, tc := range am.ToolCalls {
			toolCallIDs = append(toolCallIDs, tc.ID)
		}
	}
	return fmt.Sprintf("%s|%s|%s", msg.GetRole().String(), MessageToText(msg), strings.Join(toolCallIDs, "|"))
}

// RoundSignature 生成轮次签名，将轮次内所有消息签名用 "|" 连接。
//
// 对应 Python: FullCompactProcessor._round_signature()
func RoundSignature(messages []llm_schema.BaseMessage) string {
	var sigs []string
	for _, msg := range messages {
		sigs = append(sigs, MessageSignature(msg))
	}
	return strings.Join(sigs, "|")
}

// FlattenGroups 将消息分组展平为单一切片。
//
// 对应 Python: FullCompactProcessor.flatten_groups()
func FlattenGroups(groups [][]llm_schema.BaseMessage) []llm_schema.BaseMessage {
	var result []llm_schema.BaseMessage
	for _, g := range groups {
		result = append(result, g...)
	}
	return result
}

// IsSkillFilePath 判断文件路径是否为 skill 文件。
//
// 将路径统一转小写并标准化斜杠后，检查是否以 /skill.md 或 skill.md 结尾。
//
// 对应 Python: FullCompactProcessor._is_skill_file_path()
func IsSkillFilePath(filePath string) bool {
	if filePath == "" {
		return false
	}
	normalized := strings.ReplaceAll(strings.ToLower(filePath), "\\", "/")
	return strings.HasSuffix(normalized, "/skill.md") || strings.HasSuffix(normalized, "skill.md")
}

// ExtractArgumentValue 从 JSON 参数中提取指定 key 的值。
//
// 优先从 parsedArgs 映射中查找，然后尝试 JSON 解析 argumentsText，
// 最后使用正则表达式回退提取。
//
// 对应 Python: FullCompactProcessor._extract_argument_value()
func ExtractArgumentValue(parsedArgs map[string]any, argumentsText string, keys ...string) string {
	// 优先从已解析的 map 中查找
	if parsedArgs != nil {
		for _, key := range keys {
			if val, ok := parsedArgs[key].(string); ok && strings.TrimSpace(val) != "" {
				return strings.TrimSpace(val)
			}
		}
	}

	// 尝试 JSON 解析 argumentsText
	if argumentsText != "" {
		var parsed map[string]any
		if err := json.Unmarshal([]byte(argumentsText), &parsed); err == nil {
			for _, key := range keys {
				if val, ok := parsed[key].(string); ok && strings.TrimSpace(val) != "" {
					return strings.TrimSpace(val)
				}
			}
		}
		// fallback：正则提取
		for _, key := range keys {
			pattern := fmt.Sprintf(`"%s"\s*:\s*"([^"]+)"`, regexp.QuoteMeta(key))
			if match := regexp.MustCompile(pattern).FindStringSubmatch(argumentsText); len(match) > 1 {
				return strings.TrimSpace(match[1])
			}
		}
	}
	return ""
}

// RoundContainsSkillRead 检查轮次中是否包含 skill 文件读取。
//
// 遍历轮次内所有 AssistantMessage 的 ToolCalls，查找 read_file 工具调用
// 并判断其 file_path 参数是否指向 skill 文件。
//
// 对应 Python: FullCompactProcessor._round_contains_skill_read()
func RoundContainsSkillRead(messages []llm_schema.BaseMessage) bool {
	for _, msg := range messages {
		am, ok := msg.(*llm_schema.AssistantMessage)
		if !ok {
			continue
		}
		for _, tc := range am.ToolCalls {
			if tc.Name != "read_file" {
				continue
			}
			filePath := ExtractArgumentValue(nil, tc.Arguments, "file_path")
			if IsSkillFilePath(filePath) {
				return true
			}
		}
	}
	return false
}

// EstimateContentTokens 估算内容的 Token 数（字符长度 / 3）。
//
// 支持字符串和任意类型（JSON 序列化后估算）。
//
// 对应 Python: DialogueCompressor._estimate_content_tokens()
func EstimateContentTokens(content any) int {
	if str, ok := content.(string); ok {
		return len(str) / 3
	}
	data, err := json.Marshal(content)
	if err != nil {
		return len(fmt.Sprintf("%v", content)) / 3
	}
	return len(data) / 3
}

// IsSummaryMessage 判断消息是否为指定标记的摘要消息。
//
// 对应 Python: util.is_summary_message()
func IsSummaryMessage(msg llm_schema.BaseMessage, marker string) bool {
	_, ok := msg.(*llm_schema.UserMessage)
	return ok && strings.HasPrefix(msg.GetContent().Text(), marker)
}

// CollectSummaryIndices 收集所有指定标记的摘要消息索引。
//
// 对应 Python: util.collect_summary_indices()
func CollectSummaryIndices(messages []llm_schema.BaseMessage, marker string) []int {
	var indices []int
	for i, msg := range messages {
		if IsSummaryMessage(msg, marker) {
			indices = append(indices, i)
		}
	}
	return indices
}

// CountMessagesTokens 计算 Token 数，优先使用 TokenCounter，失败时降级到字符估算。
//
// 对应 Python: util.count_messages_tokens()
func CountMessagesTokens(tokenCounter token.TokenCounter, messages []llm_schema.BaseMessage, modelName string, processorType string) int {
	if len(messages) == 0 {
		return 0
	}
	if tokenCounter != nil {
		count, err := tokenCounter.CountMessages(messages, modelName)
		if err == nil {
			return count
		}
		prefix := ""
		if processorType != "" {
			prefix = fmt.Sprintf("[%s] ", processorType)
		}
		logger.Warn(logger.ComponentAgentCore).
			Str("processor_type", processorType).
			Err(err).
			Msg(prefix + "token_counter 返回错误，降级为字符估算")
	}
	total := 0
	for _, msg := range messages {
		total += EstimateContentTokens(msg.GetContent().Text())
	}
	return total
}

// FindLastCompletedAPIRoundEndIdx 找到范围内最后一个完整 API 轮次的结束索引。
//
// 对应 Python: util.find_last_completed_api_round_end_idx()
func FindLastCompletedAPIRoundEndIdx(messages []llm_schema.BaseMessage, startIdx int, endIdx int) int {
	if endIdx < startIdx {
		return endIdx
	}
	candidateMessages := messages[startIdx : endIdx+1]
	completedRounds := processor.GroupCompletedAPIRounds(candidateMessages)
	if len(completedRounds) == 0 {
		return startIdx - 1
	}
	lastRound := completedRounds[len(completedRounds)-1]
	return startIdx + lastRound[1] - 1
}

// IterSummaryMergeRanges 返回连续摘要消息范围，用于二次合并。
//
// 对应 Python: util.iter_summary_merge_ranges()
func IterSummaryMergeRanges(messages []llm_schema.BaseMessage, marker string, minBlocks int) [][2]int {
	var ranges [][2]int
	var startIdx *int
	var previousIdx *int

	for idx, msg := range messages {
		if IsSummaryMessage(msg, marker) {
			if startIdx == nil {
				s := idx
				startIdx = &s
			}
			p := idx
			previousIdx = &p
			continue
		}
		if startIdx != nil && previousIdx != nil {
			if *previousIdx-*startIdx+1 >= minBlocks {
				ranges = append(ranges, [2]int{*startIdx, *previousIdx})
			}
			startIdx = nil
			previousIdx = nil
		}
	}

	if startIdx != nil && previousIdx != nil {
		if *previousIdx-*startIdx+1 >= minBlocks {
			ranges = append(ranges, [2]int{*startIdx, *previousIdx})
		}
	}

	return ranges
}

// ParseToolArguments 解析工具调用 JSON 参数。
//
// 对应 Python: util.parse_tool_arguments()
func ParseToolArguments(argumentsText string) map[string]any {
	if argumentsText == "" {
		return map[string]any{}
	}
	var parsed map[string]any
	if err := json.Unmarshal([]byte(argumentsText), &parsed); err != nil {
		return map[string]any{}
	}
	return parsed
}

// DescribeToolCall 生成工具调用的可读描述。
//
// 对应 Python: util.describe_tool_call()
func DescribeToolCall(toolName string, argumentsText string) string {
	parsed := ParseToolArguments(argumentsText)
	switch toolName {
	case "read_file":
		filePath := ExtractArgumentValue(parsed, argumentsText, "file_path")
		return fmt.Sprintf("read_file path=%s", filePathOrDefault(filePath))
	case "write_file":
		filePath := ExtractArgumentValue(parsed, argumentsText, "file_path")
		return fmt.Sprintf("write_file path=%s", filePathOrDefault(filePath))
	case "edit_file":
		filePath := ExtractArgumentValue(parsed, argumentsText, "file_path")
		return fmt.Sprintf("edit_file path=%s", filePathOrDefault(filePath))
	case "glob":
		pattern := ExtractArgumentValue(parsed, argumentsText, "pattern")
		path := ExtractArgumentValue(parsed, argumentsText, "path")
		return fmt.Sprintf("glob pattern=%s path=%s", filePathOrDefault(pattern), pathOrDefault(path))
	case "grep":
		pattern := ExtractArgumentValue(parsed, argumentsText, "pattern")
		path := ExtractArgumentValue(parsed, argumentsText, "path", "file_path")
		return fmt.Sprintf("grep pattern=%s path=%s", filePathOrDefault(pattern), filePathOrDefault(path))
	default:
		return fmt.Sprintf("%s args=%s", toolName, argumentsText)
	}
}

// FindToolResultText 根据 toolCallID 查找工具结果文本。
//
// 对应 Python: util.find_tool_result_text()
func FindToolResultText(messages []llm_schema.BaseMessage, toolCallID string) string {
	if toolCallID == "" {
		return ""
	}
	for i := len(messages) - 1; i >= 0; i-- {
		tm, ok := messages[i].(*llm_schema.ToolMessage)
		if ok && tm.ToolCallID == toolCallID {
			return MessageToText(tm)
		}
	}
	return ""
}

// ExtractToolResultHint 提取工具结果的简要提示。
//
// 对应 Python: util.extract_tool_result_hint()
func ExtractToolResultHint(toolName string, resultText string, allowedToolNames []string) string {
	if resultText == "" {
		return ""
	}
	allowed := false
	for _, name := range allowedToolNames {
		if name == toolName {
			allowed = true
			break
		}
	}
	if !allowed {
		return ""
	}
	switch toolName {
	case "read_file":
		filePathMatch := regexp.MustCompile(`"file_path"\s*:\s*"([^"]+)"`).FindStringSubmatch(resultText)
		lineCountMatch := regexp.MustCompile(`"line_count"\s*:\s*(\d+)`).FindStringSubmatch(resultText)
		var parts []string
		if len(filePathMatch) > 1 {
			parts = append(parts, fmt.Sprintf("result_path=%s", filePathMatch[1]))
		}
		if len(lineCountMatch) > 1 {
			parts = append(parts, fmt.Sprintf("lines=%s", lineCountMatch[1]))
		}
		return strings.Join(parts, " ")
	case "glob":
		countMatch := regexp.MustCompile(`"count"\s*:\s*(\d+)`).FindStringSubmatch(resultText)
		if len(countMatch) > 1 {
			return fmt.Sprintf("matches=%s", countMatch[1])
		}
	case "grep":
		countMatch := regexp.MustCompile(`"count"\s*:\s*(\d+)`).FindStringSubmatch(resultText)
		if len(countMatch) > 1 {
			return fmt.Sprintf("hits=%s", countMatch[1])
		}
	case "edit_file":
		replacementsMatch := regexp.MustCompile(`"replacements"\s*:\s*(\d+)`).FindStringSubmatch(resultText)
		if len(replacementsMatch) > 1 {
			return fmt.Sprintf("replacements=%s", replacementsMatch[1])
		}
	case "write_file":
		bytesMatch := regexp.MustCompile(`"bytes_written"\s*:\s*(\d+)`).FindStringSubmatch(resultText)
		if len(bytesMatch) > 1 {
			return fmt.Sprintf("bytes_written=%s", bytesMatch[1])
		}
	}
	return ""
}

// ExtractSkillNameFromPath 从文件路径中提取 skill 名称。
//
// 对应 Python: util.extract_skill_name_from_path()
func ExtractSkillNameFromPath(filePath string) string {
	if filePath == "" {
		return ""
	}
	normalized := strings.ReplaceAll(filePath, "\\", "/")
	normalized = strings.TrimRight(normalized, "/")
	parts := strings.Split(normalized, "/")
	if len(parts) >= 2 && strings.EqualFold(parts[len(parts)-1], "skill.md") {
		return parts[len(parts)-2]
	}
	return ""
}

// ExtractSkillFileContent 提取 skill 文件内容。
//
// truncateFn 用于截断文本，通常为 FullCompactProcessor.TruncateStateText。
// 对应 Python: util.extract_skill_file_content()
func ExtractSkillFileContent(truncateFn func(string) string, resultText string) string {
	if resultText == "" {
		return ""
	}
	contentMatch := regexp.MustCompile(`"content"\s*:\s*"((?:[^"\\]|\\.)*)"`).FindStringSubmatch(resultText)
	content := ""
	if len(contentMatch) > 1 {
		rawContent := contentMatch[1]
		var err error
		content, err = stringUnescape(rawContent)
		if err != nil {
			content = strings.ReplaceAll(strings.ReplaceAll(rawContent, `\"`, `"`), `\n`, "\n")
		}
	} else {
		content = resultText
	}
	content = strings.TrimSpace(content)
	if content == "" {
		return ""
	}
	if truncateFn != nil {
		return truncateFn(content)
	}
	return content
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func filePathOrDefault(path string) string {
	if path == "" {
		return "[unknown]"
	}
	return path
}

func pathOrDefault(path string) string {
	if path == "" {
		return "."
	}
	return path
}

// stringUnescape 对 JSON 字符串进行反转义
func stringUnescape(s string) (string, error) {
	var result string
	err := json.Unmarshal([]byte(`"`+s+`"`), &result)
	return result, err
}
