package responses

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/utils/requestsanitizer"
)

// defaultOpenAIAccountBaseURL Responses 端点默认 base url。
// Python: extensions/external_provider/openai_auth/openai_account_auth.py
// DEFAULT_OPENAI_ACCOUNT_BASE_URL（23.6 前置件：此处仅取值，OAuth 客户端本体未迁移）。
const defaultOpenAIAccountBaseURL = "https://api.openai.com/v1"

// ──────────────────────────── 结构体 ────────────────────────────

// ResponsesError Responses API 数据准备或解析失败。
// Python: responses_utils.py:30 OpenAIAccountResponsesError
type ResponsesError struct {
	// Message 错误描述
	Message string
	// StatusCode HTTP 状态码（0=非 HTTP 场景）
	StatusCode int
}

// Error 实现 error 接口。
func (e *ResponsesError) Error() string { return e.Message }

// BuildRequestBodyInput build_request_body 入参（Python :42 关键字参数形态）。
type BuildRequestBodyInput struct {
	// Model 模型名（必填）
	Model string
	// Messages 消息列表（str/dict 列表，Go 调用方固定传 dict 列表，与上游转换产物一致）
	Messages any
	// Tools 工具声明（[]map[string]any，ToolInfo 或 OpenAI dict 归一后的形态）
	Tools []map[string]any
	// Temperature 采样温度（nil 不进 body）
	Temperature any
	// TopP top-p 采样参数（nil 不进 body）
	TopP any
	// MaxTokens 最大输出 token（对应 wire 字段 max_output_tokens）
	MaxTokens any
	// Stop 停止序列
	Stop any
	// Reasoning 推理参数（Responses reasoning 字段）
	Reasoning map[string]any
	// IncludeReasoningEncryptedContent 是否回传 reasoning.encrypted_content
	IncludeReasoningEncryptedContent bool
	// ToolChoice 工具选择策略（默认 "auto"）
	ToolChoice any
	// ParallelToolCalls 是否并行工具调用（默认 true）
	ParallelToolCalls any
	// Store 是否服务端留存（默认 False）
	Store any
	// ExtraBody 额外请求体字段（一层嵌套拍平到根）
	ExtraBody map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildRequestBody 从消息对象创建 Responses API 请求体。
// Python: responses_utils.py:42 build_request_body
func BuildRequestBody(in BuildRequestBodyInput) (map[string]any, error) {
	if in.Model == "" {
		return nil, &ResponsesError{Message: "OpenAI account Responses request requires a model."}
	}

	instructions, inputItems, err := ConvertMessages(in.Messages)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"model":        in.Model,
		"instructions": instructions,
		"input":        inputItems,
		// Python 默认 store=False
		"store": false,
	}
	if in.Store != nil {
		body["store"] = in.Store
	}

	// request_options 五键：非 None 才进 body（Python :70-79）
	requestOptions := map[string]any{
		"temperature":       in.Temperature,
		"top_p":             in.TopP,
		"max_output_tokens": in.MaxTokens,
		"stop":              in.Stop,
		"reasoning":         in.Reasoning,
	}
	for key, value := range requestOptions {
		if value != nil && !isNilValue(value) {
			body[key] = value
		}
	}

	// tools 非空才带 tools + parallel_tool_calls + tool_choice（Python :81-86）
	responseTools := ConvertTools(in.Tools)
	if len(responseTools) > 0 {
		body["tools"] = responseTools
		if in.ParallelToolCalls != nil {
			body["parallel_tool_calls"] = in.ParallelToolCalls
		} else {
			body["parallel_tool_calls"] = true
		}
		if in.ToolChoice != nil {
			body["tool_choice"] = in.ToolChoice
		}
	}

	// include reasoning.encrypted_content（Python :88-89）
	if in.IncludeReasoningEncryptedContent {
		body["include"] = []string{"reasoning.encrypted_content"}
	}

	// 一层嵌套 extra_body 拍平并入（Python :90-92）
	expanded := ExpandNestedExtraBody(in.ExtraBody)
	for k, v := range expanded {
		body[k] = v
	}
	// 非变更式 unicode 清洗收尾（Python :93，复用 request_sanitizer.CleanUnicode）
	cleaned, _ := requestsanitizer.CleanUnicode(body).(map[string]any)
	return cleaned, nil
}

// ExpandNestedExtraBody 把一层嵌套的 extra_body 拍平到根字段。
// 调用方常传 model_dump 形态（内含 extra_body 键），wire 格式要求这些字段在根层级；
// 冲突时外层键优先。
// Python: responses_utils.py:96 expand_nested_extra_body
func ExpandNestedExtraBody(extraBody map[string]any) map[string]any {
	if len(extraBody) == 0 {
		return map[string]any{}
	}
	// pending = dict(extra_body); nested = pending.pop("extra_body")
	pending := make(map[string]any, len(extraBody))
	for k, v := range extraBody {
		pending[k] = v
	}
	nested, _ := pending["extra_body"].(map[string]any)
	delete(pending, "extra_body")

	expanded := map[string]any{}
	if nested != nil {
		for k, v := range nested {
			expanded[k] = v
		}
	}
	// 外层键覆盖嵌套键（Python expanded.update(pending)）
	for k, v := range pending {
		expanded[k] = v
	}
	return expanded
}

// BuildHeaders 构造账号侧 Responses 请求头。
// Python: responses_utils.py:115 build_headers
func BuildHeaders(accessToken string, sessionID string, extraHeaders map[string]string) (map[string]string, error) {
	if accessToken == "" {
		return nil, &ResponsesError{Message: "OpenAI account Responses request requires an access token."}
	}
	headers := map[string]string{}
	for k, v := range extraHeaders {
		headers[k] = v
	}
	headers["Authorization"] = "Bearer " + accessToken
	headers["Accept"] = "application/json"
	headers["Content-Type"] = "application/json"
	if sessionID != "" {
		headers["session_id"] = sessionID
		headers["x-client-request-id"] = sessionID
	}
	return headers, nil
}

// ConvertMessages 把消息映射为 Responses 的 instructions 与 input items。
// system/developer → instructions；tool → function_call_output；
// assistant → output_text 内容 + 逐 tool_call 的 function_call item；
// 其余 → user input_text。
// Python: responses_utils.py:135 convert_messages
func ConvertMessages(messages any) (string, []map[string]any, error) {
	instructions := []string{}
	inputs := []map[string]any{}
	fallbackToolCallIndex := 0

	messageDicts, err := messageDicts(messages)
	if err != nil {
		return "", nil, err
	}

	for _, message := range messageDicts {
		role, _ := message["role"].(string)
		if role == "" {
			role = "user"
		}
		contentText := contentAsText(message["content"])

		// system/developer → instructions（Python :145-148）
		if role == "system" || role == "developer" {
			if contentText != "" {
				instructions = append(instructions, contentText)
			}
			continue
		}

		// tool → function_call_output（Python :150-154）
		if role == "tool" {
			if toolResult := toolResultItem(message, contentText); toolResult != nil {
				inputs = append(inputs, toolResult)
			}
			continue
		}

		// assistant → output_text + function_call items（Python :156-167）
		if role == "assistant" {
			if contentText != "" {
				inputs = append(inputs, map[string]any{
					"role": "assistant",
					"content": []map[string]any{
						{"type": "output_text", "text": contentText},
					},
				})
			}
			calls := toolCallsOfMessage(message["tool_calls"])
			for _, callItem := range calls {
				inputs = append(inputs, responseInputToolCall(callItem, fallbackToolCallIndex))
				fallbackToolCallIndex++
			}
			continue
		}

		// 其余角色 → user input_text（Python :169-174）
		inputs = append(inputs, map[string]any{
			"role": "user",
			"content": []map[string]any{
				{"type": "input_text", "text": contentText},
			},
		})
	}

	joined := ""
	for i, s := range instructions {
		if i > 0 {
			joined += "\n\n"
		}
		joined += s
	}
	return joined, inputs, nil
}

// ConvertTools 把工具声明映射为 Responses tools 形态。
// Python: responses_utils.py:179 convert_tools
func ConvertTools(tools []map[string]any) []map[string]any {
	if len(tools) == 0 {
		return []map[string]any{}
	}
	converted := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		converted = append(converted, toolDefinition(tool))
	}
	return converted
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// messageDicts 归一消息输入。
// Python: responses_utils.py:428 _message_dicts
// Go 偏差（语言等价）：调用方固定传 []map[string]any（上游 ConvertMessagesToDict 已转换），
// BaseMessage/ToolMessage 分支不再出现；str 单条 user 分支保留。
func messageDicts(messages any) ([]map[string]any, error) {
	if s, ok := messages.(string); ok {
		return []map[string]any{{"role": "user", "content": s}}, nil
	}
	dicts, ok := messages.([]map[string]any)
	if !ok {
		if anyList, isList := messages.([]any); isList {
			converted := make([]map[string]any, 0, len(anyList))
			for _, item := range anyList {
				m, ok := item.(map[string]any)
				if !ok {
					return nil, &ResponsesError{Message: fmt.Sprintf("Unsupported message type: %T", item)}
				}
				converted = append(converted, m)
			}
			dicts = converted
		} else {
			return nil, &ResponsesError{Message: fmt.Sprintf("Unsupported messages type: %T", messages)}
		}
	}
	if len(dicts) == 0 {
		return nil, &ResponsesError{Message: "OpenAI account Responses request requires at least one message."}
	}
	return dicts, nil
}

// toolCallsOfMessage 归一 tool_calls 形态（Go 上游产物 []map[string]any，
// JSON round-trip 产物 []any，两者都收）。
func toolCallsOfMessage(value any) []any {
	switch v := value.(type) {
	case []any:
		return v
	case []map[string]any:
		out := make([]any, len(v))
		for i, m := range v {
			out[i] = m
		}
		return out
	default:
		return nil
	}
}

// contentAsText 消息 content 归一为纯文本。
// Python: responses_utils.py:450 _content_as_text
func contentAsText(content any) string {
	if s, ok := content.(string); ok {
		return s
	}
	if content == nil {
		return ""
	}
	parts, ok := content.([]any)
	if !ok {
		return fmt.Sprintf("%v", content)
	}
	var textParts string
	for _, part := range parts {
		if text := contentPartText(part); text != "" {
			textParts += text
		}
	}
	return textParts
}

// contentPartText 取 content part 的文本（text/content 键兜底）。
// Python: responses_utils.py:466 _content_part_text
func contentPartText(part any) string {
	if s, ok := part.(string); ok {
		return s
	}
	partMap, ok := part.(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"text", "content"} {
		if v, ok := partMap[key]; ok && v != nil {
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

// toolResultItem 构造 function_call_output item；无 call_id 时丢弃（Python 返回 None）。
// Python: responses_utils.py:479 _tool_result_item
func toolResultItem(message map[string]any, output string) map[string]any {
	toolCallID, _ := message["tool_call_id"].(string)
	if toolCallID == "" {
		toolCallID, _ = message["call_id"].(string)
	}
	toolCallID = trimSpace(toolCallID)
	if toolCallID == "" {
		return nil
	}
	return map[string]any{
		"type":    "function_call_output",
		"call_id": toolCallID,
		"output":  output,
	}
}

// responseInputToolCall 把历史 tool_call 重建为 function_call item。
// ToolCall dict 形态（上游转换产物）：id 优先 response_item_id；call_id 兜底 id/fallback_index。
// Python: responses_utils.py:490 _response_input_tool_call
func responseInputToolCall(toolCall any, fallbackIndex int) map[string]any {
	callMap, ok := toolCall.(map[string]any)
	if !ok {
		// Python: raise OpenAIAccountResponsesError——Go 调用链上游已归一为 dict，
		// 不应到达；兜底按空 dict 处理保持不 panic
		callMap = map[string]any{}
	}
	function := dictOrEmpty(callMap["function"])
	arguments, _ := callMap["arguments"].(string)
	if arguments == "" {
		arguments, _ = function["arguments"].(string)
	}
	if arguments == "" {
		arguments = "{}"
	}
	name, _ := callMap["name"].(string)
	if name == "" {
		name, _ = function["name"].(string)
	}
	callID, _ := callMap["call_id"].(string)
	if callID == "" {
		callID, _ = callMap["id"].(string)
	}
	if callID == "" {
		// Python: f"call_{uuid.uuid4().hex}"（ToolCall 实例分支）/ f"call_{fallback_index}"（dict 分支）。
		// Go 侧统一走 dict 分支形态，兜底对齐 fallback_index；uuid hex 由 randomCallID 提供
		callID = "call_" + randomCallID()
	}
	itemID, _ := callMap["response_item_id"].(string)
	if itemID == "" {
		itemID, _ = callMap["id"].(string)
	}
	if itemID == "" {
		itemID = callID
	}
	return map[string]any{
		"type":      "function_call",
		"id":        itemID,
		"call_id":   callID,
		"name":      name,
		"arguments": jsonArgumentString(arguments),
	}
}

// toolDefinition 工具声明归一为 Responses tools 形态。
// Python: responses_utils.py:518 _tool_definition
func toolDefinition(tool map[string]any) map[string]any {
	function := dictOrEmpty(tool["function"])
	if len(function) > 0 {
		return map[string]any{
			"type":        "function",
			"name":        stringOrEmpty(function["name"]),
			"description": stringOrEmpty(function["description"]),
			"parameters":  schemaDict(function["parameters"]),
		}
	}
	if typ, _ := tool["type"].(string); typ == "function" {
		if name, _ := tool["name"].(string); name != "" {
			return map[string]any{
				"type":        "function",
				"name":        name,
				"description": stringOrEmpty(tool["description"]),
				"parameters":  schemaDict(tool["parameters"]),
			}
		}
	}
	return map[string]any{
		"type":        "function",
		"name":        stringOrEmpty(tool["name"]),
		"description": stringOrEmpty(tool["description"]),
		"parameters":  schemaDict(tool["parameters"]),
	}
}

// schemaDict schema 归一：dict 原样；非法形态兜底空 object。
// Python: responses_utils.py:547 _schema_dict（pydantic 分支为语言差异，Go 无该形态）
func schemaDict(schema any) map[string]any {
	if m, ok := schema.(map[string]any); ok {
		return m
	}
	return map[string]any{"type": "object", "properties": map[string]any{}}
}

// dictOrEmpty 非 dict 返回空 map。
// Python: responses_utils.py:756 _dict_or_empty
func dictOrEmpty(value any) map[string]any {
	if m, ok := value.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// isNilValue 判断接口内是否承载 typed-nil（如 map[string]any(nil)）。
func isNilValue(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Map, reflect.Slice, reflect.Ptr, reflect.Func, reflect.Interface, reflect.Chan:
		return v.IsNil()
	}
	return false
}

// stringOrEmpty 转 string（nil 返回空串）。
func stringOrEmpty(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

// jsonArgumentString 参数归一为 JSON 字符串。
// Python: responses_utils.py:767 _json_argument_string
func jsonArgumentString(value any) string {
	if s, ok := value.(string); ok {
		return s
	}
	b, err := json.Marshal(value)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// randomCallID 生成 32 位 hex（对齐 Python uuid4().hex 长度）。
func randomCallID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// trimSpace 等价 Python str.strip()（仅空白）。
func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && isSpaceByte(s[start]) {
		start++
	}
	for end > start && isSpaceByte(s[end-1]) {
		end--
	}
	return s[start:end]
}

// isSpaceByte ASCII 空白判断。
func isSpaceByte(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}

// ParseResponse 把完整 Responses payload 转为 AssistantMessage。
// Python: responses_utils.py:190 parse_response
func ParseResponse(payload map[string]any, modelName string) (*llmschema.AssistantMessage, error) {
	contentParts, reasoningParts, toolCalls := readOutputItems(payload["output"])
	// output_text 兜底（Python :193-194）
	if len(contentParts) == 0 {
		if outputText, ok := payload["output_text"]; ok && outputText != nil {
			contentParts = append(contentParts, fmt.Sprintf("%v", outputText))
		}
	}

	finishReason := finishReasonFromPayload(payload, len(toolCalls) > 0)
	msg := llmschema.NewAssistantMessage(strings.Join(contentParts, ""),
		llmschema.WithToolCalls(toolCalls),
		llmschema.WithFinishReason(finishReason),
		llmschema.WithReasoningContent(strings.Join(reasoningParts, "\n")),
		llmschema.WithResponseID(stringOrEmpty(payload["id"])),
		llmschema.WithResponseModel(orModelFallback(stringOrEmpty(payload["model"]), modelName)),
		llmschema.WithProviderMetadata(responseProviderMetadata(payload)),
	)
	msg.UsageMetadata = usageFromPayload(payload["usage"], modelName)
	return msg, nil
}

// MessageFromStreamChunk 把累积的流式 chunk 转为完整 AssistantMessage。
// nil → 空 stop 消息；finish_reason 归一：有 tool_calls → tool_calls、null → stop。
// Python: responses_utils.py:403 message_from_stream_chunk
func MessageFromStreamChunk(chunk *llmschema.AssistantMessageChunk) *llmschema.AssistantMessage {
	if chunk == nil {
		return llmschema.NewAssistantMessage("", llmschema.WithFinishReason("stop"))
	}
	finishReason := chunk.FinishReason
	if len(chunk.ToolCalls) > 0 {
		finishReason = "tool_calls"
	}
	if finishReason == "null" {
		finishReason = "stop"
	}
	msg := chunk.ToAssistantMessage()
	msg.FinishReason = finishReason
	return msg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readOutputItems 遍历 output items，产出 content/reasoning 文本与 tool_calls。
// Python: responses_utils.py:557 _read_output_items
func readOutputItems(output any) (contentParts []string, reasoningParts []string, toolCalls []*llmschema.ToolCall) {
	items, ok := output.([]any)
	if !ok {
		return nil, nil, nil
	}
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		itemType, _ := itemMap["type"].(string)
		switch itemType {
		case "message":
			contentParts = append(contentParts, messageContentText(itemMap)...)
		case "reasoning":
			reasoningParts = append(reasoningParts, reasoningText(itemMap)...)
		case "function_call":
			toolCalls = append(toolCalls, toolCallFromResponseItem(itemMap, len(toolCalls)))
		}
	}
	return contentParts, reasoningParts, toolCalls
}

// messageContentText 读 message item 的 output_text/text 部分。
// Python: responses_utils.py:580 _message_content_text
func messageContentText(item map[string]any) []string {
	content, ok := item["content"].([]any)
	if !ok {
		return nil
	}
	texts := []string{}
	for _, part := range content {
		partMap, ok := part.(map[string]any)
		if !ok {
			continue
		}
		partType, _ := partMap["type"].(string)
		if (partType == "output_text" || partType == "text") && partMap["text"] != nil {
			texts = append(texts, fmt.Sprintf("%v", partMap["text"]))
		}
	}
	return texts
}

// reasoningText 读 reasoning item 的 summary 与 text。
// Python: responses_utils.py:594 _reasoning_text
func reasoningText(item map[string]any) []string {
	texts := []string{}
	if summary, ok := item["summary"].([]any); ok {
		for _, part := range summary {
			if s, ok := part.(string); ok {
				texts = append(texts, s)
				continue
			}
			if partMap, ok := part.(map[string]any); ok && partMap["text"] != nil {
				texts = append(texts, fmt.Sprintf("%v", partMap["text"]))
			}
		}
	}
	if item["text"] != nil {
		texts = append(texts, fmt.Sprintf("%v", item["text"]))
	}
	return texts
}

// toolCallFromResponseItem 从 function_call item 构造 ToolCall。
// call_id 兜底 item id；response_item_id 单独保留。
// Python: responses_utils.py:610 _tool_call_from_response_item
func toolCallFromResponseItem(item map[string]any, index int) *llmschema.ToolCall {
	responseItemID := trimSpace(stringOrEmpty(item["id"]))
	callID := stringOrEmpty(item["call_id"])
	if callID == "" {
		callID = responseItemID
	}
	return llmschema.NewToolCall(callID, stringOrEmpty(item["name"]),
		jsonArgumentString(orDefaultArgs(item["arguments"])),
		llmschema.WithToolCallIndex(index),
		llmschema.WithToolCallResponseItemID(responseItemID))
}

// orDefaultArgs arguments 兜底 "{}"。
func orDefaultArgs(value any) any {
	if value == nil {
		return "{}"
	}
	return value
}

// usageFromPayload 用量归一：input/output tokens、cache 三态、reasoning_tokens。
// Python: responses_utils.py:677 _usage_from_payload
func usageFromPayload(usage any, modelName string) *llmschema.UsageMetadata {
	usageMap, ok := usage.(map[string]any)
	if !ok {
		return nil
	}
	inputTokens := intOrZero(firstPresent(usageMap, "input_tokens", "prompt_tokens"))
	outputTokens := intOrZero(firstPresent(usageMap, "output_tokens", "completion_tokens"))
	totalTokens := optionalInt(usageMap["total_tokens"])
	if totalTokens == nil {
		total := inputTokens + outputTokens
		totalTokens = &total
	}

	cacheTokens := 0
	var cacheCreationTokens *int
	var tokenDetails map[string]any
	if details, ok := usageMap["input_tokens_details"].(map[string]any); ok {
		tokenDetails = details
	} else if details, ok := usageMap["prompt_tokens_details"].(map[string]any); ok {
		tokenDetails = details
	}
	if tokenDetails != nil {
		cacheTokens = intOrZero(tokenDetails["cached_tokens"])
		cacheCreationTokens = optionalInt(firstPresent(tokenDetails, "cache_creation_tokens", "cache_write_tokens"))
	}

	reasoningTokens := 0
	var outputDetails map[string]any
	if details, ok := usageMap["output_tokens_details"].(map[string]any); ok {
		outputDetails = details
	} else if details, ok := usageMap["completion_tokens_details"].(map[string]any); ok {
		outputDetails = details
	}
	if outputDetails != nil {
		reasoningTokens = intOrZero(outputDetails["reasoning_tokens"])
	}

	u := llmschema.NewUsageMetadata()
	u.ModelName = modelName
	u.InputTokens = inputTokens
	u.OutputTokens = outputTokens
	u.TotalTokens = *totalTokens
	u.CacheTokens = cacheTokens
	if tokenDetails != nil {
		u.CacheReadTokens = &cacheTokens
		u.CacheStatus = "observed"
		u.CacheSource = "provider_usage"
		u.CacheAuthoritative = true
	}
	u.CacheCreationTokens = cacheCreationTokens
	if outputDetails != nil {
		u.ReasoningTokens = &reasoningTokens
	}
	return u
}

// responseProviderMetadata 只保留非敏感 Responses 状态元数据。
// Python: responses_utils.py:716 _response_provider_metadata
func responseProviderMetadata(payload map[string]any) map[string]any {
	metadata := map[string]any{}
	for _, key := range []string{"status", "service_tier"} {
		if value, ok := payload[key]; ok && isScalar(value) && fmt.Sprintf("%v", value) != "" {
			metadata[key] = value
		}
	}
	if incomplete, ok := payload["incomplete_details"].(map[string]any); ok {
		clone := map[string]any{}
		for k, v := range incomplete {
			clone[k] = v
		}
		metadata["incomplete_details"] = clone
	}
	return metadata
}

// finishReasonFromPayload 终态归因：tool_calls 优先，incomplete → length，否则 stop。
// Python: responses_utils.py:729 _finish_reason
func finishReasonFromPayload(payload map[string]any, hasToolCalls bool) string {
	if hasToolCalls {
		return "tool_calls"
	}
	if status, _ := payload["status"].(string); status == "incomplete" {
		return "length"
	}
	return "stop"
}

// httpErrorMessage 解析 HTTP 错误体的 error.message / detail。
// Python: responses_utils.py:737 _http_error_message（输入为响应体字节）
func httpErrorMessage(body []byte) (string, bool) {
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", false
	}
	if errorObj, ok := payload["error"].(map[string]any); ok {
		if msg, _ := errorObj["message"].(string); msg != "" {
			return msg, true
		}
	}
	if msg, ok := payload["error"].(string); ok && msg != "" {
		return msg, true
	}
	if msg, _ := payload["detail"].(string); msg != "" {
		return msg, true
	}
	return "", false
}

// firstPresent 依次取首个非 nil 的键值。
// Python: responses_utils.py:760 _first_present
func firstPresent(payload map[string]any, keys ...string) any {
	for _, key := range keys {
		if payload[key] != nil {
			return payload[key]
		}
	}
	return nil
}

// intOrZero 转 int，失败为 0。
// Python: responses_utils.py:773 _int_or_zero
func intOrZero(value any) int {
	parsed := optionalInt(value)
	if parsed == nil {
		return 0
	}
	return *parsed
}

// optionalInt 转 int 指针，nil/bool/非数值返回 nil。
// Python: responses_utils.py:778 _optional_int
func optionalInt(value any) *int {
	if value == nil {
		return nil
	}
	if _, isBool := value.(bool); isBool {
		return nil
	}
	switch v := value.(type) {
	case int:
		return &v
	case int64:
		i := int(v)
		return &i
	case float64:
		i := int(v)
		return &i
	case json.Number:
		i, err := v.Int64()
		if err != nil {
			return nil
		}
		out := int(i)
		return &out
	case string:
		var i int
		if _, err := fmt.Sscanf(v, "%d", &i); err != nil {
			return nil
		}
		return &i
	}
	return nil
}

// isScalar 是否 Python 意义下的标量（str/int/float/bool）。
func isScalar(value any) bool {
	switch value.(type) {
	case string, int, int64, float64, bool:
		return true
	}
	return false
}

// orModelFallback 服务模型名兜底。
func orModelFallback(model, fallback string) string {
	if model != "" {
		return model
	}
	return fallback
}

// ──────────────────────────── 结构体 ────────────────────────────

// StreamParser 把一次 Responses 流的 SSE 事件逐个转为 chunk。
//
// function-call arguments 随生成流式交付（response.output_item.added 命名调用，
// 每个 response.function_call_arguments.delta 扩展它），长工具调用有进度而非
// 等 item 完成一次性到达——该进度正是 TTFT 与流空闲超时观测的对象。
// response.output_item.done 会重复完整 arguments，而流式 chunk 在下游拼接，
// 因此 parser 按 item 记住已流出的部分，done 只贡献未流出的 remainder；
// 后端若跳过 delta 事件也能由 done 交付完整调用。
//
// 一个 parser 实例只服务一条响应流。
//
// Python: responses_utils.py:208 ResponsesStreamParser
type StreamParser struct {
	modelName string
	// streamedCalls item id → (tool call id, 已流出的 arguments)。
	streamedCalls map[string]streamedCall
}

// streamedCall 已登记的流式工具调用状态。
type streamedCall struct {
	callID     string
	emitted    string
	hasEmitted bool
}

// StreamEvent 迭代交付的单个事件（Event=nil 时 Err 携带解析失败）。
type StreamEvent struct {
	// Event 解析后的事件 dict（[DONE]/空块为 nil）
	Event map[string]any
	// Err 块解析失败
	Err error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamParser 创建流式解析器。
// Python: ResponsesStreamParser.__init__(model_name)
func NewStreamParser(modelName string) *StreamParser {
	return &StreamParser{
		modelName:     modelName,
		streamedCalls: map[string]streamedCall{},
	}
}

// Parse 把一个 Responses SSE 事件转为 AssistantMessageChunk。
// event 为 nil（空块）时返回 nil。
// Python 抛 OpenAIAccountResponsesError 的分支改为返回 error（Go 无异常，
// 行为等价：调用方在流循环里终止）。
// Python: responses_utils.py:230 parse
func (p *StreamParser) Parse(event map[string]any) (*llmschema.AssistantMessageChunk, error) {
	if event == nil {
		return nil, nil
	}

	eventType, _ := event["type"].(string)
	if eventType == "" {
		eventType, _ = event["event"].(string)
	}
	switch eventType {
	case "response.output_text.delta", "response.refusal.delta":
		return textDeltaChunk(event), nil
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		return reasoningDeltaChunk(event), nil
	case "response.output_item.added":
		return p.addedFunctionCallChunk(event), nil
	case "response.function_call_arguments.delta":
		return p.functionCallArgumentsDeltaChunk(event), nil
	case "response.output_item.done":
		return p.doneOutputItemChunk(event), nil
	case "response.completed", "response.incomplete":
		return terminalStreamChunk(event, eventType, p.modelName), nil
	case "response.failed", "error":
		return nil, &ResponsesError{Message: streamErrorMessage(event)}
	default:
		return nil, nil
	}
}

// IterSSEEvents 从原始 SSE 行产出解码后的事件 dict。
// Python: responses_utils.py:330 iter_sse_events
func IterSSEEvents(lines []string) <-chan StreamEvent {
	out := make(chan StreamEvent)
	go func() {
		defer close(out)
		var block []string
		for _, line := range lines {
			if line != "" {
				block = append(block, line)
				continue
			}
			event, err := ParseSSEBlock(block)
			block = nil
			if err != nil {
				out <- StreamEvent{Err: err}
				return
			}
			if event != nil {
				out <- StreamEvent{Event: event}
			}
		}
		if len(block) > 0 {
			event, err := ParseSSEBlock(block)
			if err != nil {
				out <- StreamEvent{Err: err}
				return
			}
			if event != nil {
				out <- StreamEvent{Event: event}
			}
		}
	}()
	return out
}

// ParseSSEBlock 解码单个 SSE 块。
// event/data 字段解析、[DONE] → (nil, nil)、非法 JSON 报 ResponsesError。
// Python: responses_utils.py:349 parse_sse_block
func ParseSSEBlock(lines []string) (map[string]any, error) {
	eventType := ""
	dataParts := []string{}

	for _, rawLine := range lines {
		line := strings.TrimRight(rawLine, "\r")
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		field, value, found := cutColon(line)
		if !found {
			continue
		}
		value = strings.TrimPrefix(value, " ")

		switch field {
		case "event":
			eventType = trimSpace(value)
		case "data":
			dataParts = append(dataParts, trimSpace(value))
		}
	}

	if len(dataParts) == 0 {
		if eventType == "" {
			return nil, nil
		}
		return map[string]any{"type": eventType}, nil
	}

	rawData := strings.Join(dataParts, "\n")
	if rawData == "[DONE]" {
		return nil, nil
	}

	var parsed map[string]any
	if err := json.Unmarshal([]byte(rawData), &parsed); err != nil {
		return nil, &ResponsesError{
			Message: fmt.Sprintf("Invalid OpenAI account SSE JSON: %s", rawData),
		}
	}
	if _, ok := parsed["type"]; !ok {
		fallback := eventType
		if fallback == "" {
			fallback, _ = parsed["event"].(string)
		}
		if fallback != "" {
			parsed["type"] = fallback
		}
	}
	return parsed, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// addedFunctionCallChunk 处理 output_item.added（function_call 起帧）。
// 登记 item_id → (call_id, 初始 arguments)，交付带完整初始 arguments 的帧；
// 无 item id 时跳过（done 事件兜底交付完整调用）。
// Python: responses_utils.py:262 _added_function_call_chunk
func (p *StreamParser) addedFunctionCallChunk(event map[string]any) *llmschema.AssistantMessageChunk {
	item, ok := event["item"].(map[string]any)
	if !ok || item["type"] != "function_call" {
		return nil
	}
	itemID := trimSpace(stringOrEmpty(item["id"]))
	if itemID == "" {
		return nil
	}
	toolCall := toolCallFromResponseItem(item, 0)
	initialArguments := jsonArgumentString(orDefaultArgs(item["arguments"]))
	toolCall.Arguments = initialArguments
	p.streamedCalls[itemID] = streamedCall{callID: toolCall.ID, emitted: initialArguments, hasEmitted: true}
	return llmschema.NewAssistantMessageChunk("", llmschema.WithChunkToolCalls([]*llmschema.ToolCall{toolCall}))
}

// functionCallArgumentsDeltaChunk 处理 arguments 增量。
// Python: responses_utils.py:277 _function_call_arguments_delta_chunk
func (p *StreamParser) functionCallArgumentsDeltaChunk(event map[string]any) *llmschema.AssistantMessageChunk {
	itemID := trimSpace(stringOrEmpty(event["item_id"]))
	delta, _ := event["delta"].(string)
	streamed, ok := p.streamedCalls[itemID]
	if !ok || delta == "" {
		return nil
	}
	p.streamedCalls[itemID] = streamedCall{callID: streamed.callID, emitted: streamed.emitted + delta, hasEmitted: true}
	return llmschema.NewAssistantMessageChunk("", llmschema.WithChunkToolCalls([]*llmschema.ToolCall{
		llmschema.NewToolCall(streamed.callID, "", delta,
			llmschema.WithToolCallResponseItemID(itemID)),
	}))
}

// doneOutputItemChunk 处理 output_item.done（function_call 收尾）。
// done 参数以已流出为前缀时只补 remainder；分歧时保已流出（不可撤回）并 Warn。
// Python: responses_utils.py:300 _done_output_item_chunk
func (p *StreamParser) doneOutputItemChunk(event map[string]any) *llmschema.AssistantMessageChunk {
	item, ok := event["item"].(map[string]any)
	if !ok || item["type"] != "function_call" {
		return nil
	}

	toolCall := toolCallFromResponseItem(item, 0)
	itemID := trimSpace(stringOrEmpty(item["id"]))
	streamed, hadStreamed := p.streamedCalls[itemID]
	if hadStreamed {
		delete(p.streamedCalls, itemID)
		remainder := ""
		if strings.HasPrefix(toolCall.Arguments, streamed.emitted) {
			remainder = toolCall.Arguments[len(streamed.emitted):]
		} else {
			// 流出的碎片已在下游拼接、无法撤回，保留它们而非重复
			logWarn("Responses function call %s arguments diverged from the streamed deltas; keeping the streamed arguments.", itemID)
		}
		toolCall.Name = ""
		toolCall.Arguments = remainder
	}
	return llmschema.NewAssistantMessageChunk("", llmschema.WithChunkToolCalls([]*llmschema.ToolCall{toolCall}),
		llmschema.WithChunkFinishReason("tool_calls"))
}

// textDeltaChunk 文本/refusal delta。
// Python: responses_utils.py:623 _text_delta_chunk
func textDeltaChunk(event map[string]any) *llmschema.AssistantMessageChunk {
	delta, _ := event["delta"].(string)
	if delta == "" {
		return nil
	}
	return llmschema.NewAssistantMessageChunk(delta)
}

// reasoningDeltaChunk 推理 delta。
// Python: responses_utils.py:630 _reasoning_delta_chunk
func reasoningDeltaChunk(event map[string]any) *llmschema.AssistantMessageChunk {
	delta, _ := event["delta"].(string)
	if delta == "" {
		return nil
	}
	return llmschema.NewAssistantMessageChunk("",
		llmschema.WithChunkReasoningContent(delta))
}

// terminalStreamChunk completed/incomplete 终帧（usage 归一 + 元数据）。
// Python: responses_utils.py:637 _terminal_stream_chunk
func terminalStreamChunk(event map[string]any, eventType string, modelName string) *llmschema.AssistantMessageChunk {
	responsePayload, _ := event["response"].(map[string]any)
	var usage any
	if responsePayload != nil {
		usage = responsePayload["usage"]
	} else {
		usage = event["usage"]
	}
	finishReason := "stop"
	if eventType == "response.incomplete" {
		finishReason = "length"
	}
	opts := []llmschema.AssistantMessageChunkOption{
		llmschema.WithChunkFinishReason(finishReason),
	}
	if u := usageFromPayload(usage, modelName); u != nil {
		opts = append(opts, llmschema.WithChunkUsageMetadata(u))
	}
	if responsePayload != nil {
		if id := stringOrEmpty(responsePayload["id"]); id != "" {
			opts = append(opts, llmschema.WithChunkResponseID(id))
		}
		if model := orModelFallback(stringOrEmpty(responsePayload["model"]), modelName); model != "" {
			opts = append(opts, llmschema.WithChunkResponseModel(model))
		}
		opts = append(opts, llmschema.WithChunkProviderMetadata(responseProviderMetadata(responsePayload)))
	} else {
		if modelName != "" {
			opts = append(opts, llmschema.WithChunkResponseModel(modelName))
		}
	}
	return llmschema.NewAssistantMessageChunk("", opts...)
}

// streamErrorMessage 流内失败文案。
// Python: responses_utils.py:668 _stream_error_message
func streamErrorMessage(event map[string]any) string {
	if msg, _ := event["message"].(string); msg != "" {
		return msg
	}
	if errorObj, ok := event["error"].(map[string]any); ok {
		if msg, _ := errorObj["message"].(string); msg != "" {
			return msg
		}
	}
	return "OpenAI account Responses stream failed."
}

// cutColon 按第一个冒号切分（SSE 字段解析）。
func cutColon(line string) (string, string, bool) {
	for i := 0; i < len(line); i++ {
		if line[i] == ':' {
			return line[:i], line[i+1:], true
		}
	}
	return line, "", false
}

// logWarn 包级告警日志。
// Python: responses_utils.py:313 logger.warning（llm_logger）
func logWarn(format string, args ...any) {
	logger.Warn(logger.ComponentAgentCore).
		Msgf(format, args...)
}
