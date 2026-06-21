package compressor

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
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

// ──────────────────────────── 非导出函数 ────────────────────────────
