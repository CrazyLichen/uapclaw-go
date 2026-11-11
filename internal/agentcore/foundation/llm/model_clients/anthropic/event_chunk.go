package anthropic

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// errStreamDone 流自然结束哨兵（data: [DONE] 或流关闭）。
var errStreamDone = errors.New("stream done")

// ──────────────────────────── 导出函数 ────────────────────────────

// EventToChunk 把一个 Anthropic SSE 事件映射为 AssistantMessageChunk（无产出返回 nil）。
// 事件序：message_start → content_block_start → content_block_delta（一个或多个）
// → content_block_stop → message_delta（携带 stop_reason + usage）→ message_stop。
// Python: anthropic_model_client.py:1419-1615 _event_to_chunk
func EventToChunk(event map[string]any, toolUseAcc map[int]map[string]any) *llmschema.AssistantMessageChunk {
	etype := strArg(event, "type")

	if etype == "message_start" {
		msg := dictArg(event, "message")
		usageMetadata := UsageFromAnthropic(dictArg(msg, "usage"), strArg(msg, "model"))
		return llmschema.NewAssistantMessageChunk("",
			llmschema.WithChunkUsageMetadata(usageMetadata),
			llmschema.WithChunkFinishReason(llmschema.FinishReasonNull),
			llmschema.WithChunkResponseID(strArg(msg, "id")),
			llmschema.WithChunkResponseModel(strArg(msg, "model")),
		)
	}

	if etype == "content_block_start" {
		block := dictArg(event, "content_block")
		idx := intOf(event["index"])
		if block == nil {
			return nil
		}
		blockType := strArg(block, "type")
		switch blockType {
		case "tool_use":
			toolUseAcc[idx] = map[string]any{
				"type": "tool_use", "id": strArg(block, "id"),
				"name": strArg(block, "name"), "input": block["input"],
				"args_str": "",
			}
		case "thinking":
			toolUseAcc[idx] = map[string]any{
				"type": "thinking", "thinking": strArg(block, "thinking"),
				"signature": strArg(block, "signature"),
			}
		case "redacted_thinking":
			toolUseAcc[idx] = map[string]any{
				"type": "redacted_thinking", "data": strArg(block, "data"),
			}
			return llmschema.NewAssistantMessageChunk("",
				llmschema.WithChunkFinishReason(llmschema.FinishReasonNull),
			)
		case "text":
			toolUseAcc[idx] = map[string]any{"type": "text", "text": strArg(block, "text")}
		}
		return nil
	}

	if etype == "content_block_delta" {
		delta := dictArg(event, "delta")
		if delta == nil {
			return nil
		}
		idx := intOf(event["index"])
		dtype := strArg(delta, "type")

		if dtype == "text_delta" {
			text := strArg(delta, "text")
			if text == "" {
				return nil
			}
			if state, exists := toolUseAcc[idx]; exists {
				state["text"] = strOrEmpty(state["text"]) + text
			} else {
				toolUseAcc[idx] = map[string]any{"type": "text", "text": text}
			}
			return llmschema.NewAssistantMessageChunk(text,
				llmschema.WithChunkFinishReason(llmschema.FinishReasonNull),
			)
		}
		if dtype == "thinking_delta" {
			thinking := strArg(delta, "thinking")
			if thinking == "" {
				return nil
			}
			state, exists := toolUseAcc[idx]
			if !exists {
				state = map[string]any{"type": "thinking", "thinking": "", "signature": ""}
				toolUseAcc[idx] = state
			}
			state["thinking"] = strOrEmpty(state["thinking"]) + thinking
			return llmschema.NewAssistantMessageChunk("",
				llmschema.WithChunkReasoningContent(thinking),
				llmschema.WithChunkFinishReason(llmschema.FinishReasonNull),
			)
		}
		if dtype == "signature_delta" {
			signature := strArg(delta, "signature")
			if signature == "" {
				return nil
			}
			state, exists := toolUseAcc[idx]
			if !exists {
				state = map[string]any{"type": "thinking", "thinking": "", "signature": ""}
				toolUseAcc[idx] = state
			}
			state["signature"] = strOrEmpty(state["signature"]) + signature
			return llmschema.NewAssistantMessageChunk("",
				llmschema.WithChunkFinishReason(llmschema.FinishReasonNull),
			)
		}
		// input_json_delta：累积进 args_str，块结束时整体产出
		if isToolInputJSONDelta(dtype, idx, toolUseAcc) {
			toolUseAcc[idx]["args_str"] = strOrEmpty(toolUseAcc[idx]["args_str"]) + strArg(delta, "partial_json")
			return nil
		}
		return nil
	}

	if etype == "content_block_stop" {
		idx := intOf(event["index"])
		block, exists := toolUseAcc[idx]
		if !exists {
			return nil
		}
		if block["type"] != "tool_use" {
			return llmschema.NewAssistantMessageChunk("",
				llmschema.WithChunkFinishReason(llmschema.FinishReasonNull),
			)
		}
		argsStr := strOrEmpty(block["args_str"])
		if argsStr == "" {
			argsStr = "{}"
		}
		block["input"] = toolUseInput(argsAnyOf(block["input"]), argsStr)
		return llmschema.NewAssistantMessageChunk("",
			llmschema.WithChunkFinishReason(llmschema.FinishReasonNull),
			llmschema.WithChunkToolCalls([]*llmschema.ToolCall{{
				ID:        strArg(block, "id"),
				Type:      "function",
				Name:      strArg(block, "name"),
				Arguments: argsStr,
				Index:     idx,
			}}),
		)
	}

	if etype == "message_delta" {
		delta := dictArg(event, "delta")
		stopReason := ""
		if delta != nil {
			stopReason = strArg(delta, "stop_reason")
		}
		finishReason := "stop"
		if stopReason == "tool_use" {
			finishReason = "tool_calls"
		} else if stopReason != "" && stopReason != "end_turn" &&
			stopReason != "stop_sequence" && stopReason != "max_tokens" {
			finishReason = stopReason
		}
		usageMetadata := UsageFromAnthropic(dictArg(event, "usage"), "")
		chunk := llmschema.NewAssistantMessageChunk("",
			llmschema.WithChunkFinishReason(finishReason),
		)
		chunk.UsageMetadata = usageMetadata
		if stopReason != "" {
			chunk.ProviderMetadata = map[string]any{"stop_reason": stopReason}
		}
		return chunk
	}

	// message_stop 与未知事件：无产出
	return nil
}

// ReadStreamBody 逐事件读取 SSE 流（Anthropic data: 行承载 JSON）。
// 返回解析后的事件 map；流结束返回 errStreamDone。
func readStreamEvent(body io.Reader) (map[string]any, error) {
	reader := bufio.NewReader(body)
	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return nil, errStreamDone
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "data:") {
			payload := strings.TrimSpace(line[5:])
			if payload == "[DONE]" {
				return nil, errStreamDone
			}
			var event map[string]any
			if json.Unmarshal([]byte(payload), &event) != nil {
				continue
			}
			return event, nil
		}
		// event: 行承载事件名（data 内 type 同值），跳过
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isToolInputJSONDelta 判断是否为 tool_use 的参数增量。
// Python: anthropic_model_client.py:1601-1615 _is_tool_input_json_delta
func isToolInputJSONDelta(dtype string, idx int, toolUseAcc map[int]map[string]any) bool {
	if dtype != "input_json_delta" {
		return false
	}
	state, exists := toolUseAcc[idx]
	return exists && state["type"] == "tool_use"
}

// strOrEmpty 安全把 any 转 string（nil → ""）。
func strOrEmpty(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// argsAnyOf 输入若已是对象则优先（Python args_str 非 str 时原样使用）。
func argsAnyOf(v any) any { return v }
