package anthropic

import (
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ParseResponse 把 Anthropic Message 响应转换为 AssistantMessage。
// 四类块分派：text 归 content、thinking 归 reasoning_content、redacted_thinking
// 只进回放（无可显示文本）、tool_use 归 ToolCalls（index=块序）。
// Python: anthropic_model_client.py:1272-1366 _parse_response
func ParseResponse(
	response map[string]any,
	parser model_clients.BaseOutputParser,
	modelName string,
	modelProvider string,
) *llmschema.AssistantMessage {
	contentBlocks, _ := response["content"].([]any)
	textParts := make([]string, 0)
	reasoningParts := make([]string, 0)
	replayBlocks := make([]map[string]any, 0)
	toolCalls := make([]*llmschema.ToolCall, 0)

	for idx, blockAny := range contentBlocks {
		block, ok := blockAny.(map[string]any)
		if !ok {
			continue
		}
		blockType := strArg(block, "type")
		// 回放块 sanitize 后保留（签名原样带回）
		if replayBlock := SanitizeReplayBlock(block); replayBlock != nil {
			replayBlocks = append(replayBlocks, replayBlock)
		}
		switch blockType {
		case "text":
			textParts = append(textParts, strArg(block, "text"))
		case "thinking":
			reasoningParts = append(reasoningParts, strArg(block, "thinking"))
		case "redacted_thinking":
			// 加密数据保留进 metadata 供回放，但无可显示的推理文本
			continue
		case "tool_use":
			inputObj := block["input"]
			if inputObj == nil {
				inputObj = map[string]any{}
			}
			argsStr, isStr := inputObj.(string)
			if !isStr {
				argsStr = mustJSONString(inputObj)
			}
			toolCalls = append(toolCalls, &llmschema.ToolCall{
				ID:        strArg(block, "id"),
				Type:      "function",
				Name:      strArg(block, "name"),
				Arguments: argsStr,
				Index:     idx,
			})
		}
	}

	content := joinStrings(textParts)
	reasoningContent := joinStrings(reasoningParts)
	if reasoningContent == "" {
		// 部分 Anthropic 兼容网关暴露 OpenAI 风格的额外响应字段；为显示归一是安全的，
		// 但回放时绝不能替代带签名的 Anthropic thinking 块。
		extraReasoning := response["reasoning_content"]
		if extraReasoning == nil {
			if modelExtra, ok := response["model_extra"].(map[string]any); ok {
				extraReasoning = modelExtra["reasoning_content"]
			}
		}
		if s, isStr := extraReasoning.(string); isStr && s != "" {
			reasoningContent = s
		}
	}

	usageMetadata := UsageFromAnthropic(dictArg(response, "usage"), modelName)

	parserContent := any(nil)
	if parser != nil && content != "" {
		if parsed, err := parser.Parse(content); err == nil {
			parserContent = parsed
		}
		// 解析失败静默（Python: llm_logger.warning）
	}

	stopReason := strArg(response, "stop_reason")
	finishReason := stopReason
	if len(toolCalls) > 0 {
		finishReason = "tool_calls"
	} else if stopReason == "end_turn" || stopReason == "stop_sequence" || stopReason == "max_tokens" {
		finishReason = "stop"
	} else if stopReason == "" {
		finishReason = "stop"
	}

	// provider_metadata 仅收标量非空值（stop_reason / stop_sequence）
	providerMetadata := make(map[string]any)
	for _, key := range []string{"stop_reason", "stop_sequence"} {
		if v := response[key]; v != nil {
			switch t := v.(type) {
			case string:
				if t != "" {
					providerMetadata[key] = t
				}
			case float64, int, bool:
				providerMetadata[key] = t
			}
		}
	}

	msg := llmschema.NewAssistantMessage(content,
		llmschema.WithFinishReason(finishReason),
		llmschema.WithReasoningContent(reasoningContent),
		llmschema.WithResponseID(strArg(response, "id")),
		llmschema.WithResponseModel(strArg(response, "model")),
	)
	msg.ParserContent = parserContent
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	msg.UsageMetadata = usageMetadata
	if len(replayBlocks) > 0 {
		msg.Metadata = map[string]any{AnthropicContentBlocksMetadataKey: replayBlocks}
	}
	msg.ProviderMetadata = providerMetadata
	return msg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// joinStrings 拼接字符串片段。
func joinStrings(parts []string) string {
	out := ""
	for _, p := range parts {
		out += p
	}
	return out
}

// mustJSONString 序列化为 JSON 字符串（input 已是对象时）。
func mustJSONString(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}
