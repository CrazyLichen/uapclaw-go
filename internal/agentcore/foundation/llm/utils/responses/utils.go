package responses

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

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
			calls, _ := message["tool_calls"].([]any)
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
