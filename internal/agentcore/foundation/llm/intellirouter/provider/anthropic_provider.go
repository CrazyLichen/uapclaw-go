package provider

import (
	"encoding/json"
	"fmt"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"io"
	"strings"

	intellirouter "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter"
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

// ──────────────────────────── 常量 ────────────────────────────

// Anthropic 常量。← anthropic_provider.py:15-17
const (
	// AnthropicVersion API 版本
	AnthropicVersion = "2023-06-01"
	// DefaultMaxTokens 默认最大输出 token
	DefaultMaxTokens = 4096
	// ThinkingBudgetMargin thinking 预算富余量
	ThinkingBudgetMargin = 1024
)

// ──────────────────────────── 结构体 ────────────────────────────

// AnthropicAdapter Anthropic 消息/工具/Streaming 格式转换适配器。
// ← anthropic_provider.py:19 AnthropicProviderAdapter
type AnthropicAdapter struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAnthropicAdapter 构造。
func NewAnthropicAdapter() *AnthropicAdapter { return &AnthropicAdapter{} }

// GetAPIURL /v1/messages 端点。← py:26-31
func (a *AnthropicAdapter) GetAPIURL(deployment *intelliroutercore.Deployment, stream bool) string {
	return messagesURL(deployment.APIBase)
}

// GetHeaders 请求头（x-api-key 认证）。← py:33-38
func (a *AnthropicAdapter) GetHeaders(deployment *intelliroutercore.Deployment) map[string]string {
	return map[string]string{
		"x-api-key":         deployment.APIKey,
		"anthropic-version": AnthropicVersion,
		"Content-Type":      "application/json",
	}
}

// TransformRequest OpenAI 参数 → Anthropic 请求体。← py:40-115 transform_request
func (a *AnthropicAdapter) TransformRequest(model string, messages []map[string]any, _ *intelliroutercore.Deployment, kwargs map[string]any) map[string]any {
	// ← py:41 stream = kwargs.pop("stream", False)
	stream := popBool(kwargs, "stream", false)
	systemBlocks, nonSystem := a.extractSystem(messages) // ← py:42
	anthropicMessages := convertMessages(nonSystem)      // ← py:43

	body := map[string]any{
		"model":    model,
		"messages": anthropicMessages,
	}
	if stream {
		body["stream"] = true // ← py:48-49
	}
	// ← py:51 max_tokens = kwargs.pop("max_tokens", DEFAULT_MAX_TOKENS)
	body["max_tokens"] = popInt(kwargs, "max_tokens", DefaultMaxTokens)

	tools := popAny(kwargs, "tools") // ← py:53
	if tools != nil {
		body["tools"] = convertTools(tools.([]any))
	}
	toolChoice := popAny(kwargs, "tool_choice") // ← py:56
	if toolChoice != nil {
		body["tool_choice"] = convertToolChoice(toolChoice)
	}

	// ← py:60-79 sampling_params 分流
	samplingParams := map[string]any{}
	for k, v := range kwargs {
		switch k {
		case "temperature", "top_p", "top_k":
			samplingParams[k] = v // ← py:64-65
		case "stop":
			// ← py:66-67 stop_sequences
			if s, ok := v.(string); ok {
				body["stop_sequences"] = []any{s}
			} else {
				body["stop_sequences"] = v
			}
		case "metadata":
			body["metadata"] = v
		case "thinking", "output_config", "reasoning_effort", "thinking_budget",
			"thinking_strategy", "enable_thinking", "service_tier", "chat_template_kwargs":
			body[k] = v // ← py:69-78
		}
	}

	// ← py:81-86 thinking budget 富余（isinstance(budget, int)：int/float64 均收）
	thinking, _ := body["thinking"].(map[string]any)
	if thinking != nil {
		if budget := intOf(thinking["budget_tokens"]); budget > 0 {
			maxTokens := intOf(body["max_tokens"])
			if maxTokens <= budget {
				body["max_tokens"] = budget + ThinkingBudgetMargin
			}
		}
	}
	// ← py:87-91 thinking_type 判定
	thinkingType := ""
	if thinking != nil {
		thinkingType = strings.ToLower(strings.TrimSpace(strOf(thinking["type"])))
	}
	// ← py:92-100 非思考模式才注入采样参数
	if thinkingType != "enabled" && thinkingType != "adaptive" {
		if samplingParams["temperature"] != nil {
			body["temperature"] = samplingParams["temperature"]
		} else if samplingParams["top_p"] != nil && samplingParams["top_p"] != float64(1.0) {
			body["top_p"] = samplingParams["top_p"]
		}
		if samplingParams["top_k"] != nil {
			body["top_k"] = samplingParams["top_k"]
		}
	}

	// ← py:102-106 extra_body 合并
	extraBody, exists := kwargs["extra_body"]
	if exists {
		if m, ok := extraBody.(map[string]any); ok {
			for k, v := range m {
				body[k] = v
			}
		} else if extraBody != nil {
			body["extra_body"] = extraBody
		}
	}

	// ← py:108-110 system blocks
	if len(systemBlocks) > 0 {
		body["system"] = systemBlocks
	}
	return body
}

// TransformResponse 原生响应 → OpenAI 统一格式。← py:117-157 transform_response
func (a *AnthropicAdapter) TransformResponse(rawResponse map[string]any, model string, _ *intelliroutercore.Deployment) map[string]any {
	var content []string
	var reasoning []string
	var toolCalls []any
	for _, item := range listOf(rawResponse["content"]) {
		block, ok := item.(map[string]any)
		if !ok {
			continue
		}
		switch block["type"] {
		case "text": // ← py:122-123
			content = append(content, strOf(block["text"]))
		case "thinking": // ← py:124-125
			reasoning = append(reasoning, strOf(block["thinking"]))
		case "tool_use": // ← py:126-133
			inputJSON, _ := json.Marshal(block["input"])
			toolCalls = append(toolCalls, map[string]any{
				"id":   block["id"],
				"type": "function",
				"function": map[string]any{
					"name":      block["name"],
					"arguments": string(inputJSON),
				},
			})
		}
	}

	message := map[string]any{
		"role": "assistant",
		// ← py:136 "".join(content) if content else None
		"content": joinOrNil(content),
	}
	if len(reasoning) > 0 {
		message["reasoning_content"] = strings.Join(reasoning, "") // ← py:138-139
	}
	if len(toolCalls) > 0 {
		message["tool_calls"] = toolCalls // ← py:140-141
	}

	// ← py:144 finish_reason 映射
	finishReason := MapStopReason(strOf(rawResponse["stop_reason"]))

	result := BuildCompletionResponse(
		strOf(rawResponse["id"]),
		model,
		message,
		finishReason,
		0, 0, // ← py:147-148 prompt_tokens=0 completion_tokens=0
	)
	result["usage"] = usageToOpenAIUsage(rawResponse["usage"]) // ← py:150
	result["provider_content"] = rawResponse                   // ← py:151
	return result
}

// TransformStreamChunk 原生流事件 → OpenAI SSE chunk。← py:159-227
func (a *AnthropicAdapter) TransformStreamChunk(chunk map[string]any, model string, _ *intelliroutercore.Deployment) map[string]any {
	eventType := strOf(chunk["type"]) // ← py:160

	switch eventType {
	case "message_start": // ← py:162-176
		msg, _ := chunk["message"].(map[string]any)
		var usage map[string]any
		if msg != nil {
			if u, ok := msg["usage"].(map[string]any); ok {
				usage = usageToOpenAIUsage(u)
			}
		}
		id := ""
		if msg != nil {
			id = strOf(msg["id"])
		}
		var usageArg map[string]any
		if msg != nil && msg["usage"] != nil {
			usageArg = usage
		}
		return withProviderContent(BuildChunkResponse(model, map[string]any{"role": "assistant"}, nil, id, usageArg), chunk)

	case "content_block_delta": // ← py:177-204
		blockIndex := intOf(chunk["index"])
		delta, _ := chunk["delta"].(map[string]any)
		if delta == nil {
			return nil
		}
		switch delta["type"] {
		case "text_delta": // ← py:182-186
			return withProviderContent(BuildChunkResponse(model, map[string]any{"content": strOf(delta["text"])}, nil, "", nil), chunk)
		case "thinking_delta": // ← py:187-194
			thinking := strOf(delta["thinking"])
			if thinking == "" {
				return nil // ← py:190-191
			}
			return withProviderContent(BuildChunkResponse(model, map[string]any{"reasoning_content": thinking}, nil, "", nil), chunk)
		case "signature_delta":
			return nil // ← py:195-196
		case "input_json_delta": // ← py:197-204
			return withProviderContent(BuildChunkResponse(model, map[string]any{
				"tool_calls": []any{map[string]any{
					"index":    blockIndex,
					"function": map[string]any{"arguments": strOf(delta["partial_json"])},
				}},
			}, nil, "", nil), chunk)
		}

	case "content_block_start": // ← py:205-220
		blockIndex := intOf(chunk["index"])
		block, _ := chunk["content_block"].(map[string]any)
		if block != nil && block["type"] == "tool_use" { // ← py:208-219
			return withProviderContent(BuildChunkResponse(model, map[string]any{
				"tool_calls": []any{map[string]any{
					"index": blockIndex,
					"id":    strOf(block["id"]),
					"type":  "function",
					"function": map[string]any{
						"name":      strOf(block["name"]),
						"arguments": "",
					},
				}},
			}, nil, "", nil), chunk)
		}

	case "message_delta": // ← py:221-227
		delta, _ := chunk["delta"].(map[string]any)
		stopReason := ""
		if delta != nil {
			stopReason = strOf(delta["stop_reason"])
		}
		var usageArg map[string]any
		if chunk["usage"] != nil {
			if u, ok := chunk["usage"].(map[string]any); ok {
				usageArg = usageToOpenAIUsage(u)
			}
		}
		return withProviderContent(BuildChunkResponse(model, map[string]any{}, ptrStr(MapStopReason(stopReason)), "", usageArg), chunk)

	case "ping":
		return nil // ← py:228-229
	}
	return nil // ← py:231
}

// ValidateRequestConfig 默认无校验（基类默认）。
func (a *AnthropicAdapter) ValidateRequestConfig(_ *intelliroutercore.Deployment, _ map[string]any) {}

// SignRequest 默认不签名。
func (a *AnthropicAdapter) SignRequest(_, _ string, headers map[string]string, _ []byte, _ *intelliroutercore.Deployment) map[string]string {
	return headers
}

// IterStreamEvents 标准 SSE 解析（默认实现）。
func (a *AnthropicAdapter) IterStreamEvents(body io.Reader, deploymentID string) (*StreamEventIter, error) {
	return NewStreamEventIter(body, deploymentID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// messagesURL 归一化 /v1/messages URL。← py:22-26 _messages_url
func messagesURL(apiBase string) string {
	base := strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if strings.HasSuffix(base, "/v1/messages") {
		base = strings.TrimRight(base[:len(base)-len("/v1/messages")], "/")
	} else if strings.HasSuffix(base, "/v1") {
		base = strings.TrimRight(base[:len(base)-len("/v1")], "/")
	}
	return base + "/v1/messages"
}

// withProviderContent 附加 provider_content。← py:231-235 _with_provider_content
func withProviderContent(response map[string]any, raw map[string]any) map[string]any {
	if response != nil {
		response["provider_content"] = raw
	}
	return response
}

// usageToOpenAIUsage Anthropic usage → OpenAI usage。← py:238-267
func usageToOpenAIUsage(usage any) map[string]any {
	m, ok := usage.(map[string]any)
	if !ok {
		m = map[string]any{} // ← py:243-244
	}
	// ← py:246-253 _int_value：None/bool → 0，float 安全转换，负值截 0
	intValue := func(name string) int {
		v := m[name]
		if v == nil {
			return 0
		}
		if _, isBool := v.(bool); isBool {
			return 0
		}
		f, err := toFloat(v)
		if err != nil {
			return 0
		}
		i := int(f)
		if i < 0 {
			i = 0
		}
		return i
	}
	uncachedInput := intValue("input_tokens")
	outputTokens := intValue("output_tokens")
	cacheRead := intValue("cache_read_input_tokens")
	cacheWrite := intValue("cache_creation_input_tokens")
	promptTokens := uncachedInput + cacheRead + cacheWrite // ← py:256
	cacheMiss := uncachedInput + cacheWrite                // ← py:257
	result := map[string]any{
		"prompt_tokens":       promptTokens,
		"completion_tokens":   outputTokens,
		"total_tokens":        promptTokens + outputTokens,
		"cache_tokens":        cacheRead,
		"cache_read_tokens":   cacheRead,
		"cache_miss_tokens":   cacheMiss,
		"cache_write_tokens":  cacheWrite,
		"cache_status":        "observed",       // ← py:263
		"cache_source":        "provider_usage", // ← py:264
		"cache_authoritative": true,             // ← py:265
	}
	if _, exists := m["cache_creation_input_tokens"]; exists {
		// ← py:267-268 仅原响应携带该键时回填
		result["cache_creation_input_tokens"] = cacheWrite
	}
	return result
}

// extractSystem 提取 system 消息。← py:271-282 _extract_system
func (a *AnthropicAdapter) extractSystem(messages []map[string]any) ([]map[string]any, []map[string]any) {
	systemMsgs, nonSystem := ExtractSystemMessages(messages)
	var systemBlocks []map[string]any
	for _, msg := range systemMsgs {
		content := strOf(msg["content"])
		if content != "" { // ← py:278 if content:
			systemBlocks = append(systemBlocks, map[string]any{"type": "text", "text": content})
		}
	}
	return systemBlocks, nonSystem
}

// convertMessages OpenAI 消息 → Anthropic 消息。← py:285-337 _convert_messages
//
// 关键点：
//   - content 字段必须为 list（不能是纯字符串）
//   - tool 结果消息映射为 tool_result content block
//   - 并行 tool_calls 的多条紧邻 tool 结果合并进同一条 user 消息
//     （Anthropic 要求 user/assistant 严格交替）
func convertMessages(messages []map[string]any) []map[string]any {
	var result []map[string]any
	for _, msg := range messages {
		role := strOf(msg["role"])
		if role == "" {
			role = "user" // ← py:292 role = msg.get("role", "user")
		}
		content := msg["content"] // ← py:293

		switch role {
		case "tool": // ← py:294-315
			contentStr := ""
			if s, ok := content.(string); ok {
				contentStr = s
			} else {
				b, _ := json.Marshal(content) // ← py:298 json.dumps(content)
				contentStr = string(b)
			}
			toolCallID := strOf(msg["tool_call_id"])
			toolResultBlock := map[string]any{
				"type":        "tool_result",
				"tool_use_id": toolCallID,
				"content":     contentStr,
			}
			// ← py:301-311 只有紧邻的 tool 消息才合并
			merged := false
			if len(result) > 0 {
				last := result[len(result)-1]
				if last["role"] == "user" {
					if blocks, ok := last["content"].([]any); ok && len(blocks) > 0 {
						allToolResult := true
						for _, b := range blocks {
							bm, _ := b.(map[string]any)
							if bm == nil || bm["type"] != "tool_result" {
								allToolResult = false
								break
							}
						}
						if allToolResult {
							last["content"] = append(blocks, toolResultBlock)
							merged = true
						}
					}
				}
			}
			if !merged {
				result = append(result, map[string]any{"role": "user", "content": []any{toolResultBlock}})
			}
		case "assistant": // ← py:316-333
			var blocks []any
			if str, ok := content.(string); ok && str != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": str})
			}
			// ← py:322-324 tool_calls 只来自 msg["tool_calls"]，与 content 无关
			for _, tc := range IterToolCallInputs(listOf(msg["tool_calls"])) {
				blocks = append(blocks, map[string]any{
					"type":  "tool_use",
					"id":    tc.ID,
					"name":  tc.Name,
					"input": tc.Input,
				})
			}
			// Anthropic requires non-empty content array ← py:332-333
			if len(blocks) == 0 {
				blocks = append(blocks, map[string]any{"type": "text", "text": ""})
			}
			result = append(result, map[string]any{"role": "assistant", "content": blocks})
		default: // user ← py:334-347
			if str, ok := content.(string); ok {
				result = append(result, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": str}}})
			} else if list, ok := content.([]any); ok {
				var converted []any
				for _, block := range list {
					bm, ok := block.(map[string]any)
					if !ok {
						converted = append(converted, map[string]any{"type": "text", "text": strOf(block)})
						continue
					}
					converted = append(converted, convertContentBlock(bm)...)
				}
				result = append(result, map[string]any{"role": "user", "content": converted})
			} else {
				result = append(result, map[string]any{"role": "user", "content": []any{map[string]any{"type": "text", "text": strOf(content)}}})
			}
		}
	}
	return result
}

// convertContentBlock 单个 content block 转换。← py:340-397 _convert_content_block
//
// 返回列表：正常情况返回 [converted_block]，不支持的类型返回 []（跳过）。
func convertContentBlock(block map[string]any) []any {
	blockType := strOf(block["type"])

	switch blockType {
	case "text":
		return []any{block} // ← py:346-347 透传

	case "image_url": // ← py:350-366
		imageURL, _ := block["image_url"].(map[string]any)
		url := ""
		if imageURL != nil {
			url = strOf(imageURL["url"])
		}
		if intellirouter.IsDataURI(url) {
			mimeType, data, err := intellirouter.ParseDataURI(url)
			if err != nil {
				return nil
			}
			return []any{map[string]any{"type": "image", "source": map[string]any{
				"type": "base64", "media_type": mimeType, "data": data,
			}}}
		}
		if strings.HasPrefix(url, "http://") {
			// ← py:360-362 HTTPS 警告（日志语义，warn 级别）
			logger.Warn(logger.ComponentAgentCore).
				Str("event_type", "ANTHROPIC_INSECURE_IMAGE_URL").
				Str("url", url).
				Msg("image url uses insecure http scheme")
		}
		return []any{map[string]any{"type": "image", "source": map[string]any{"type": "url", "url": url}}}

	case "file": // ← py:368-381
		fileInfo, _ := block["file"].(map[string]any)
		if fileInfo == nil {
			return nil
		}
		fileData := strOf(fileInfo["file_data"])
		if fileData != "" && intellirouter.IsDataURI(fileData) {
			mimeType, data, err := intellirouter.ParseDataURI(fileData)
			if err != nil {
				return nil
			}
			return []any{map[string]any{"type": "document", "source": map[string]any{
				"type": "base64", "media_type": mimeType, "data": data,
			}}}
		}
		fileID := strOf(fileInfo["file_id"])
		if fileID != "" {
			if strings.HasPrefix(fileID, "http") { // ← py:375-376
				return []any{map[string]any{"type": "document", "source": map[string]any{"type": "url", "url": fileID}}}
			}
			return []any{map[string]any{"type": "document", "source": map[string]any{"type": "file", "file_id": fileID}}}
		}
		return nil

	case "input_audio", "video_url":
		// ← py:384-390 Anthropic 不支持音频/视频输入，跳过
		return nil
	}
	// 未知类型 — 透传（兼容调用方直接传入 Anthropic 原生格式）← py:393
	return []any{block}
}

// convertTools OpenAI tools → Anthropic tools。← py:400-407 _convert_tools
func convertTools(tools []any) []any {
	var result []any
	for _, spec := range IterToolSpecs(tools) {
		result = append(result, map[string]any{
			"name":         spec.Name,
			"description":  spec.Description,
			"input_schema": spec.Parameters,
		})
	}
	return result
}

// convertToolChoice OpenAI tool_choice → Anthropic tool_choice。← py:410-428
func convertToolChoice(toolChoice any) map[string]any {
	if s, ok := toolChoice.(string); ok {
		switch s {
		case "any":
			return map[string]any{"type": "any"} // ← py:414-415
		case "none":
			return map[string]any{"type": "none"} // ← py:416-417
		}
		return map[string]any{"type": "auto"} // ← py:418
	}
	if m, ok := toolChoice.(map[string]any); ok {
		tcType := strOf(m["type"])
		if tcType == "function" {
			fn, _ := m["function"].(map[string]any)
			name := ""
			if fn != nil {
				name = strOf(fn["name"])
			}
			return map[string]any{"type": "tool", "name": name} // ← py:421-423
		}
		if tcType == "auto" || tcType == "any" || tcType == "none" {
			return map[string]any{"type": tcType} // ← py:424-425
		}
	}
	return map[string]any{"type": "auto"} // ← py:427-428
}

// joinOrNil "".join(x) if x else None。← py:136
func joinOrNil(parts []string) any {
	if len(parts) == 0 {
		return nil
	}
	return strings.Join(parts, "")
}

// ──────────────────────────── 非导出函数（kwargs 弹出助手） ─────────────────

// popBool kwargs.pop(k, default) 的 bool 版。
func popBool(kwargs map[string]any, key string, def bool) bool {
	if v, ok := kwargs[key]; ok {
		delete(kwargs, key)
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return def
}

// popInt kwargs.pop(k, default) 的 int 版。
func popInt(kwargs map[string]any, key string, def int) int {
	if v, ok := kwargs[key]; ok {
		delete(kwargs, key)
		if i, ok := v.(int); ok {
			return i
		}
		if f, ok := v.(float64); ok {
			return int(f)
		}
	}
	return def
}

// popAny kwargs.pop(k) 版（缺失返回 nil）。
func popAny(kwargs map[string]any, key string) any {
	v, ok := kwargs[key]
	if ok {
		delete(kwargs, key)
	}
	return v
}

// strOf any → string（nil 安全）。
func strOf(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// intOf any → int。
func intOf(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	default:
		return 0
	}
}

// toFloat any → float64。
func toFloat(v any) (float64, error) {
	switch t := v.(type) {
	case int:
		return float64(t), nil
	case float64:
		return t, nil
	default:
		return 0, fmt.Errorf("not a number")
	}
}

// listOf any → []any（nil 安全）。
func listOf(v any) []any {
	if list, ok := v.([]any); ok {
		return list
	}
	return nil
}

// ptrStr *string 构造。
func ptrStr(s string) *string { return &s }
