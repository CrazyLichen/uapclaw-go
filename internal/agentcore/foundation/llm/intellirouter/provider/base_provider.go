// Package provider provider 适配器。← intelli_router/provider/ 包
//
// 每个 provider（OpenAI / Anthropic 等）实现适配器接口，intelli_router
// 在 HTTP 请求层插入 adapter 完成格式转换。
package provider

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	intellirouter "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter"
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseProviderAdapter 适配器接口。← provider/base_provider.py:17 BaseProviderAdapter(ABC)
type BaseProviderAdapter interface {
	// GetAPIURL 构建 API URL（含路径和 query params）。← py:35 get_api_url
	GetAPIURL(deployment *intelliroutercore.Deployment, stream bool) string
	// GetHeaders 构建 HTTP 请求头（含认证信息）。← py:40 get_headers
	GetHeaders(deployment *intelliroutercore.Deployment) map[string]string
	// TransformRequest 将 OpenAI 风格参数转换为 Provider 原生请求体。
	// ← py:44 transform_request（默认实现：直接合并 model + messages + kwargs）
	TransformRequest(model string, messages []map[string]any, deployment *intelliroutercore.Deployment, kwargs map[string]any) map[string]any
	// ValidateRequestConfig 校验路由级请求配置。← py:57 validate_request_config
	ValidateRequestConfig(deployment *intelliroutercore.Deployment, config map[string]any)
	// TransformResponse 将 Provider 原生响应转换为 OpenAI 统一格式。
	// 返回结果中必须包含 usage.completion_tokens 字段（ReliableRouter 依赖）。
	// ← py:69 transform_response（默认透传）
	TransformResponse(rawResponse map[string]any, model string, deployment *intelliroutercore.Deployment) map[string]any
	// TransformStreamChunk 将原生 streaming chunk 转换为 OpenAI 统一 SSE 格式。
	// 返回 nil 表示该 chunk 应被跳过（例如 keepalive / ping）。← py:82
	TransformStreamChunk(chunk map[string]any, model string, deployment *intelliroutercore.Deployment) map[string]any
	// SignRequest 签名请求并返回更新后的 headers；默认原样返回。← py:95
	SignRequest(method, url string, headers map[string]string, body []byte, deployment *intelliroutercore.Deployment) map[string]string
	// IterStreamEvents 从 HTTP 响应 body 中迭代流式事件。← py:143
	// 默认实现：标准 SSE 格式解析（data: ... 行）。
	IterStreamEvents(body io.Reader, deploymentID string) (*StreamEventIter, error)
}

// StreamEventIter 流式事件迭代器（async 生成器的 Go 等价物）。
// Next 返回 (chunk, ok, err)：ok=false 且 err=nil 表示流正常耗尽。
type StreamEventIter struct {
	lines     *bufio.Scanner
	depID     string
	sawDone   bool
	sawFinish bool
	next      map[string]any
	err       error
	started   bool
}

// Next 迭代下一个事件。断流检测（py:150-159 doc）：流耗尽时既没收到 [DONE]
// 哨兵、也没有任何 chunk 携带终结信号 → 抛 DeploymentNetworkError（网络/
// 部署侧异常，会被 router 的 except 捕获触发 fallback）。
//
// 兼容性说明（py:160-163 原文）：不发送 [DONE] 也不给任何 chunk 带终结信号
// 就关闭连接的端点会被判为断流——这是有意的 fail-fast，避免半截内容被静默
// 当成功。
func (it *StreamEventIter) Next() (map[string]any, bool, error) {
	if it.err != nil {
		return nil, false, it.err
	}
	if !it.started {
		it.started = true
	}
	for it.lines.Scan() {
		line := strings.TrimSpace(it.lines.Text()) // ← py:169-170 strip
		if line == "" {
			continue // ← py:170
		}
		if strings.HasPrefix(line, ":") {
			continue // ← py:172-173 SSE 注释行
		}
		var data string
		if strings.HasPrefix(line, "data:") {
			data = strings.TrimSpace(line[5:]) // ← py:175-176
		} else if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
			data = line // ← py:177-178 裸 JSON 行
		} else {
			continue // ← py:179-180
		}
		if data == "[DONE]" {
			it.sawDone = true // ← py:182-184
			break
		}
		var chunk map[string]any
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			// 无法解析的 data 行（典型场景：上游网关返回 HTML 错误页片段）
			// 不再静默：打 debug 日志便于排查，仍跳过该行。← py:187-192
			continue
		}
		if ChunkHasFinishSignal(chunk) {
			it.sawFinish = true // ← py:194-195
		}
		return chunk, true, nil
	}
	if scanErr := it.lines.Err(); scanErr != nil && it.err == nil {
		it.err = intellirouter.NewDeploymentNetworkError(it.depID, scanErr.Error())
		return nil, false, it.err
	}
	// 断流检测 ← py:197-204
	if !it.sawDone && !it.sawFinish {
		it.err = intellirouter.NewDeploymentNetworkError(it.depID,
			"stream ended without [DONE] sentinel or finish signal; treating as truncated stream")
		return nil, false, it.err
	}
	return nil, false, nil
}

// ──────────────────────────── 常量 ────────────────────────────

// anthropicStopReasonMap Anthropic 系（Anthropic / Bedrock Converse）通用
// stop_reason → OpenAI finish_reason 映射。Bedrock 多出的 content_filtered
// 键对纯 Anthropic 无害（查表只命中存在的键）。← py:25-33
var anthropicStopReasonMap = map[string]string{
	"end_turn":         "stop",
	"stop_sequence":    "stop",
	"tool_use":         "tool_calls",
	"max_tokens":       "length",
	"content_filtered": "content_filter",
}

// toolCallFuncFields 清洗 tool_calls 保留的字段。← py:120 _TOOL_CALL_FUNC_FIELDS
var toolCallFuncFields = []string{"name", "arguments"}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamEventIter 创建 SSE 事件迭代器（BaseProviderAdapter.IterStreamEvents
// 的默认实现）。← py:143 iter_stream_events
func NewStreamEventIter(body io.Reader, deploymentID string) (*StreamEventIter, error) {
	return &StreamEventIter{
		lines: bufio.NewScanner(body),
		depID: deploymentID,
	}, nil
}

// MapStopReason 查 stop_reason 映射表（对齐 py:33 ANTHROPIC_STOP_REASON_MAP.get(k, "stop")）。
func MapStopReason(stopReason string) string {
	if v, ok := anthropicStopReasonMap[stopReason]; ok {
		return v
	}
	return "stop"
}

// ChunkHasFinishSignal 宽松判定一个流式 chunk 是否携带「正常终结」信号。
// ← py:108 _chunk_has_finish_signal（静态方法 → 导出函数：StreamEventIter 与
// ReliableRouter._has_visible_stream_output 均需使用）
//
// 覆盖实际流经 iter_stream_events 的各 provider 原生（转换前）形态：
//   - OpenAI 兼容：choices[*].finish_reason 非空
//   - Anthropic 原生：message_delta 的 delta.stop_reason 非空，或 message_stop 事件
//   - Gemini 原生：candidates[*].finishReason 非空
//   - Bedrock Converse（事件流 payload）：顶层 stopReason
//
// 一个静态 helper 覆盖实际存在的形态即可，不做穷举式设计。
func ChunkHasFinishSignal(chunk map[string]any) bool {
	if chunk == nil {
		return false
	}
	if choices, ok := chunk["choices"].([]any); ok {
		for _, choice := range choices {
			if c, ok := choice.(map[string]any); ok {
				if fr := c["finish_reason"]; fr != nil && fr != "" {
					return true // ← py:115-119
				}
			}
		}
	}
	if chunk["type"] == "message_stop" {
		return true // ← py:121-122
	}
	if delta, ok := chunk["delta"].(map[string]any); ok {
		if sr := delta["stop_reason"]; sr != nil && sr != "" {
			return true // ← py:124-126
		}
	}
	if candidates, ok := chunk["candidates"].([]any); ok {
		for _, candidate := range candidates {
			if c, ok := candidate.(map[string]any); ok {
				if fr := c["finishReason"]; fr != nil && fr != "" {
					return true // ← py:128-132
				}
			}
		}
	}
	return chunk["stopReason"] != nil && chunk["stopReason"] != "" // ← py:133
}

// BuildCompletionResponse 构建标准 OpenAI chat.completion 响应信封。
// ← py:138 _build_completion_response（静态方法 → 导出函数）
func BuildCompletionResponse(id, model string, message map[string]any, finishReason string, promptTokens, completionTokens int) map[string]any {
	return map[string]any{
		"id":      id,
		"object":  "chat.completion",
		"created": time.Now().Unix(), // ← py:142 int(time.time())
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"message":       message,
			"finish_reason": finishReason,
		}},
		"usage": map[string]any{
			"prompt_tokens":     promptTokens,
			"completion_tokens": completionTokens,
			"total_tokens":      promptTokens + completionTokens,
		},
	}
}

// BuildChunkResponse 构建标准 OpenAI chat.completion.chunk 响应信封。
// ← py:152 _build_chunk_response（静态方法 → 导出函数）
func BuildChunkResponse(model string, delta map[string]any, finishReason *string, id string, usage map[string]any) map[string]any {
	chunk := map[string]any{
		"id":      id,
		"object":  "chat.completion.chunk",
		"created": time.Now().Unix(),
		"model":   model,
		"choices": []any{map[string]any{
			"index":         0,
			"delta":         delta,
			"finish_reason": finishReason, // nil 对齐 Python None
		}},
	}
	if usage != nil {
		chunk["usage"] = usage
	}
	return chunk
}

// ExtractSystemMessages 将 system 消息与非 system 消息分离。
// 返回 (system_messages, non_system_messages)。← py:168 _extract_system_messages
func ExtractSystemMessages(messages []map[string]any) ([]map[string]any, []map[string]any) {
	var system, nonSystem []map[string]any
	for _, msg := range messages {
		if msg["role"] == "system" {
			system = append(system, msg)
		} else {
			nonSystem = append(nonSystem, msg)
		}
	}
	return system, nonSystem
}

// ToolCallInput 从 assistant.tool_calls 解析出的 (id, name, input_dict)。
type ToolCallInput struct {
	ID    string
	Name  string
	Input map[string]any
}

// IterToolCallInputs 从 OpenAI assistant.tool_calls 中解析出条目列表。
// ← py:186 _iter_tool_call_inputs
//
// 封装 arguments 的 JSON 反序列化与异常回退（失败时 input 为 {}），
// 各 provider 据此拼装自己的 tool_use / toolUse / functionCall 块。
func IterToolCallInputs(toolCalls []any) []ToolCallInput {
	var result []ToolCallInput
	for _, item := range toolCalls {
		tc, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, _ := tc["function"].(map[string]any)
		argsStr := ""
		if fn != nil {
			if s, ok := fn["arguments"].(string); ok {
				argsStr = s
			}
		}
		var argsInput map[string]any
		if argsStr != "" {
			_ = json.Unmarshal([]byte(argsStr), &argsInput) // ← py:193 失败回退 {}
		}
		if argsInput == nil {
			argsInput = map[string]any{}
		}
		name := ""
		if fn != nil {
			name, _ = fn["name"].(string)
		}
		id, _ := tc["id"].(string)
		result = append(result, ToolCallInput{ID: id, Name: name, Input: argsInput})
	}
	return result
}

// ToolSpec 从 OpenAI tools 解析出的 (name, description, parameters)。
type ToolSpec struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// IterToolSpecs 从 OpenAI tools 中解析出规格列表。
// ← py:206 _iter_tool_specs
//
// 兼容两种输入：标准 {"function": {...}} 包裹，或直接的 function 对象。
func IterToolSpecs(tools []any) []ToolSpec {
	var specs []ToolSpec
	for _, item := range tools {
		tool, ok := item.(map[string]any)
		if !ok {
			continue
		}
		fn, ok := tool["function"].(map[string]any)
		if !ok {
			fn = tool // ← py:211 func = tool.get("function", tool)
		}
		name, _ := fn["name"].(string)
		desc, _ := fn["description"].(string)
		params, _ := fn["parameters"].(map[string]any)
		specs = append(specs, ToolSpec{Name: name, Description: desc, Parameters: params})
	}
	return specs
}

// SanitizeToolCalls 清洗 tool_calls，只保留 OpenAI 标准字段：
// id/type/function.name/function.arguments。
// ← py:128 sanitize_tool_calls
//
// 一些 provider（SiliconFlow、InferenceAffinity 等）对 tool_calls 中的
// 非标准字段（如 index）敏感，需要清洗。
//
// 不修改调用方传入的消息：对含 tool_calls 的 assistant 消息返回浅拷贝替换，
// 其余消息原样保留引用。
func SanitizeToolCalls(messages []map[string]any) []map[string]any {
	newMessages := make([]map[string]any, len(messages))
	copy(newMessages, messages)
	for i, msg := range messages {
		if msg["role"] != "assistant" {
			continue // ← py:145
		}
		calls, ok := msg["tool_calls"].([]any)
		if !ok {
			continue // ← py:146-147 isinstance(calls, list)
		}
		var normalized []any
		for _, c := range calls {
			call, ok := c.(map[string]any)
			if !ok {
				continue // ← py:149 if isinstance(c, dict)
			}
			rawFunc, _ := call["function"].(map[string]any)
			if rawFunc == nil {
				rawFunc = map[string]any{}
			}
			fn := map[string]any{}
			for _, k := range toolCallFuncFields {
				v, _ := rawFunc[k].(string)
				fn[k] = v // ← py:143 {k: raw_func.get(k, "")}
			}
			id, _ := call["id"].(string)
			normalized = append(normalized, map[string]any{
				"id":       id,
				"type":     "function", // ← py:144
				"function": fn,
			})
		}
		// ← py:150 {**msg, "tool_calls": [...]} 浅拷贝替换
		shallow := make(map[string]any, len(msg)+1)
		for k, v := range msg {
			shallow[k] = v
		}
		shallow["tool_calls"] = normalized
		newMessages[i] = shallow
	}
	return newMessages
}

// DefaultTransformRequest TransformRequest 的默认实现（OpenAI 兼容透传）。
// ← py:51-55 {"model": model, "messages": messages, **kwargs}
func DefaultTransformRequest(model string, messages []map[string]any, kwargs map[string]any) map[string]any {
	body := map[string]any{"model": model, "messages": messages}
	for k, v := range kwargs {
		body[k] = v
	}
	return body
}

// UnknownProviderError 未知 provider 错误。← registry.py:18-22 ValueError
func UnknownProviderError(providerName string, supported []string) error {
	return fmt.Errorf("Unknown provider: '%s'. Supported providers: %v", providerName, supported)
}
