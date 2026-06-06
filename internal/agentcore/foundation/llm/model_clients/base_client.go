package model_clients

import (
	"context"
	"fmt"
	"os"
	"strconv"

	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 接口 ────────────────────────────

// BaseOutputParser LLM 输出解析器最小接口。
//
// 2.6 节仅定义最小接口，2.16 节实现完整的 JsonOutputParser/MarkdownOutputParser。
// 当前仅包含 Parse 方法，后续可扩展。
//
// 对应 Python: openjiuwen/core/foundation/llm/output_parsers/output_parser.py (BaseOutputParser)
type BaseOutputParser interface {
	// Parse 解析 LLM 输出文本，返回结构化结果。
	Parse(text string) (any, error)
}

// BaseModelClient LLM 模型客户端接口，所有模型客户端实现必须满足此接口。
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/base_model_client.py (BaseModelClient)
type BaseModelClient interface {
	// Invoke 非流式调用 LLM，返回完整的助手消息。
	Invoke(ctx context.Context, messages MessagesParam, opts ...InvokeOption) (*llmschema.AssistantMessage, error)

	// Stream 流式调用 LLM，返回流式结果（含 chunk channel + Final 合并方法）。
	Stream(ctx context.Context, messages MessagesParam, opts ...StreamOption) (*StreamResult, error)

	// GenerateImage 生成图片。
	GenerateImage(ctx context.Context, messages []*llmschema.UserMessage, opts ...GenerateImageOption) (*llmschema.ImageGenerationResponse, error)

	// GenerateSpeech 生成语音。
	GenerateSpeech(ctx context.Context, messages []*llmschema.UserMessage, opts ...GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error)

	// GenerateVideo 生成视频。
	GenerateVideo(ctx context.Context, messages []*llmschema.UserMessage, opts ...GenerateVideoOption) (*llmschema.VideoGenerationResponse, error)

	// Release 释放模型缓存或资源（如 vLLM KV Cache）。
	//
	// 对应 Python: InferenceAffinityModelClient.release()
	// 仅 InferenceAffinity 客户端有实际实现，其他客户端返回不支持错误。
	Release(ctx context.Context, opts ...ReleaseOption) (bool, error)
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent model_clients 包日志组件标识（AgentCore 层）。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// BaseClientEmbed BaseModelClient 的共享实现，具体客户端嵌入此结构体复用通用逻辑。
//
// 对应 Python: BaseModelClient 中的具体方法（非抽象方法）
//
// 使用方式：
//
//	type OpenAIModelClient struct {
//	    BaseClientEmbed
//	    // ... OpenAI 特有字段
//	}
type BaseClientEmbed struct {
	// ModelConfig 模型请求配置
	ModelConfig *llmschema.ModelRequestConfig
	// ClientConfig 模型客户端配置
	ClientConfig *llmschema.ModelClientConfig
	// clientName 客户端名称（子类通过 WithClientName 设置）
	clientName string
}

// BaseClientEmbedOption BaseClientEmbed 构造选项函数。
type BaseClientEmbedOption func(*BaseClientEmbed)

// ──────────────────────────── 导出函数 ────────────────────────────

// WithClientName 设置客户端名称（用于错误消息和日志）。
func WithClientName(name string) BaseClientEmbedOption {
	return func(e *BaseClientEmbed) { e.clientName = name }
}

// NewBaseClientEmbed 创建 BaseClientEmbed 实例，并校验配置。
//
// 对应 Python: BaseModelClient.__init__(model_config, model_client_config)
func NewBaseClientEmbed(
	modelConfig *llmschema.ModelRequestConfig,
	clientConfig *llmschema.ModelClientConfig,
	opts ...BaseClientEmbedOption,
) (*BaseClientEmbed, error) {
	e := &BaseClientEmbed{
		ModelConfig:  modelConfig,
		ClientConfig: clientConfig,
		clientName:   "BaseModelClient",
	}
	for _, opt := range opts {
		opt(e)
	}
	if err := e.ValidateConfig(); err != nil {
		return nil, err
	}
	return e, nil
}

// ValidateConfig 校验模型客户端配置参数。
//
// 校验规则：
//   - api_key: 必填
//   - api_base: 必填
//   - verify_ssl 为 true 时 ssl_cert 必填
//
// 对应 Python: BaseModelClient._validate_config()
func (e *BaseClientEmbed) ValidateConfig() error {
	clientName := e.GetClientName()

	if e.ClientConfig.APIKey == "" {
		return exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg(fmt.Sprintf("model client config api_key is required for %s.", clientName)),
		)
	}
	if e.ClientConfig.APIBase == "" {
		return exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg(fmt.Sprintf("model client config api_base is required for %s.", clientName)),
		)
	}
	if e.ClientConfig.VerifySSL && e.ClientConfig.SSLCert == "" {
		return exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg("model client config ssl_cert is required when verify_ssl is true."),
		)
	}
	return nil
}

// GetClientName 返回客户端名称（用于错误消息和日志）。
//
// 对应 Python: BaseModelClient._get_client_name()
func (e *BaseClientEmbed) GetClientName() string {
	return e.clientName
}

// ConvertMessagesToDict 将消息参数转换为 OpenAI API 格式的 dict 列表。
//
// 对应 Python: BaseModelClient._convert_messages_to_dict()
//
// 转换规则：
//   - IsText     → [{"role":"user","content":text}]
//   - IsDicts    → 直接返回（零转换开销）
//   - IsMessages → 遍历消息列表，通过类型断言处理：
//     *AssistantMessage → 追加 tool_calls（OpenAI 嵌套格式）+ reasoning_content
//     *ToolMessage      → 追加 tool_call_id
//     其他              → 仅 role + content
func (e *BaseClientEmbed) ConvertMessagesToDict(messages MessagesParam) ([]map[string]any, error) {
	// 空消息 → 报错
	if messages.IsEmpty() {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_INVOKE_PARAM_ERROR", 181004, ""),
			exception.WithMsg("The message sent to the llm cannot be empty."),
		)
	}

	// 纯文本 → 包装为 UserMessage
	if messages.IsText() {
		return []map[string]any{
			{"role": "user", "content": messages.Text()},
		}, nil
	}

	// 已是 dict 格式 → 直接返回
	if messages.IsDicts() {
		return messages.Dicts(), nil
	}

	// 消息列表 → 逐条转换
	result := make([]map[string]any, 0, len(messages.Messages()))
	for _, msg := range messages.Messages() {
		msgDict, err := e.convertOneMessage(msg)
		if err != nil {
			return nil, err
		}
		result = append(result, msgDict)
	}
	return result, nil
}

// ConvertToolsToDict 将工具信息列表转换为 OpenAI API 格式。
//
// 对应 Python: BaseModelClient._convert_tools_to_dict()
//
// 输出格式：[{"type": "function", "function": {"name": "...", "description": "...", "parameters": {...}}}]
func (e *BaseClientEmbed) ConvertToolsToDict(tools []*commonschema.ToolInfo) []map[string]any {
	if len(tools) == 0 {
		return nil
	}

	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		parameters := tool.Parameters
		if parameters == nil {
			parameters = make(map[string]any)
		}
		toolDict := map[string]any{
			"type": tool.Type,
			"function": map[string]any{
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  parameters,
			},
		}
		result = append(result, toolDict)
	}
	return result
}

// BuildRequestParams 构建完整的 OpenAI 兼容请求参数。
//
// 对应 Python: BaseModelClient._build_request_params()
//
// 优先级：方法参数 > model_config 默认值
// 合并顺序：基础参数 → model_config.Extra → params.Extra
func (e *BaseClientEmbed) BuildRequestParams(messagesDict []map[string]any, params *InvokeParams, stream bool) (map[string]any, error) {
	// 1. 校验 model 非空
	model := params.Model
	if model == "" && e.ModelConfig != nil {
		model = e.ModelConfig.ModelName
	}
	if model == "" {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_CONFIG_ERROR", 181003, ""),
			exception.WithMsg("The model cannot be None."),
		)
	}

	// 2. 构建基础参数
	reqParams := map[string]any{
		"model":    model,
		"messages": messagesDict,
		"stream":   stream,
	}

	// 3. 合并参数（优先级：params > model_config）
	if e.ModelConfig != nil {
		if params.Temperature == nil && e.ModelConfig.Temperature != 0 {
			reqParams["temperature"] = e.ModelConfig.Temperature
		}
		if params.TopP == nil && e.ModelConfig.TopP != 0 {
			reqParams["top_p"] = e.ModelConfig.TopP
		}
		if params.MaxTokens == nil && e.ModelConfig.MaxTokens != nil {
			reqParams["max_tokens"] = *e.ModelConfig.MaxTokens
		}
		if params.Stop == nil && e.ModelConfig.Stop != nil {
			reqParams["stop"] = *e.ModelConfig.Stop
		}
		// 合并 model_config.Extra
		if len(e.ModelConfig.Extra) > 0 {
			for k, v := range e.ModelConfig.Extra {
				reqParams[k] = v
			}
		}
	}

	// 方法参数覆盖 model_config
	if params.Temperature != nil {
		reqParams["temperature"] = *params.Temperature
	}
	if params.TopP != nil {
		reqParams["top_p"] = *params.TopP
	}
	if params.MaxTokens != nil {
		reqParams["max_tokens"] = *params.MaxTokens
	}
	if params.Stop != nil {
		reqParams["stop"] = *params.Stop
	}

	// 4. 转换 tools
	if len(params.Tools) > 0 {
		reqParams["tools"] = e.ConvertToolsToDict(params.Tools)
		reqParams["tool_choice"] = "auto"
	}

	// 5. 合并 params.Extra（覆盖 model_config.Extra 同名键）
	if len(params.Extra) > 0 {
		// 过滤内部参数
		internalParams := map[string]bool{
			"output_parser":       true,
			"tracer_record_data": true,
			"custom_headers":     true,
		}
		for k, v := range params.Extra {
			if !internalParams[k] {
				reqParams[k] = v
			}
		}
	}

	// 6. 日志记录
	// 对齐 Python: 敏感模式（默认）不记录 messages/tools；非敏感模式记录。
	// 环境变量 IS_SENSITIVE=false 时为非敏感模式，默认为敏感模式。
	isSensitive := true
	if v := os.Getenv("IS_SENSITIVE"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			isSensitive = b
		}
	}

	// 提取需要记录的字段值
	modelProvider := ""
	if e.ClientConfig != nil {
		modelProvider = e.ClientConfig.ClientProvider
	}
	temperature, _ := reqParams["temperature"]
	topP, _ := reqParams["top_p"]
	maxTokens, _ := reqParams["max_tokens"]
	stop, _ := reqParams["stop"]

	// 计算额外参数（排除基础参数）
	extraParams := make(map[string]any)
	baseKeys := map[string]bool{
		"model": true, "messages": true, "stream": true,
		"temperature": true, "top_p": true, "max_tokens": true, "stop": true,
		"tools": true, "tool_choice": true,
	}
	for k, v := range reqParams {
		if !baseKeys[k] {
			extraParams[k] = v
		}
	}

	if isSensitive {
		// 敏感模式：不记录 messages/tools
		logger.Info(logComponent).
			Str("event_type", "LLM_CALL_START").
			Str("model_name", model).
			Str("model_provider", modelProvider).
			Any("temperature", temperature).
			Any("top_p", topP).
			Any("max_tokens", maxTokens).
			Bool("is_stream", stream).
			Any("stop", stop).
			Str("client_name", e.GetClientName()).
			Any("extra_params", extraParams).
			Msg("Before request chat model, LLM request params ready.")
	} else {
		// 非敏感模式：记录 messages/tools
		logger.Info(logComponent).
			Str("event_type", "LLM_CALL_START").
			Str("model_name", model).
			Str("model_provider", modelProvider).
			Any("messages", messagesDict).
			Any("tools", reqParams["tools"]).
			Any("temperature", temperature).
			Any("top_p", topP).
			Any("max_tokens", maxTokens).
			Bool("is_stream", stream).
			Str("client_name", e.GetClientName()).
			Any("extra_params", extraParams).
			Msg("Before request chat model, LLM request params ready.")
	}

	return reqParams, nil
}

// ExtractCostInfo 从响应对象提取费用信息。
//
// 对应 Python: BaseModelClient._extract_cost_info()
//
// 支持三种格式：
//  1. cost 为简单数值（int/float）
//  2. cost / usage_cost 为对象，含 input_cost/output_cost/total_cost 属性
//  3. cost_details 含 upstream_inference_*_cost 字段
//
// 返回：(inputCost, outputCost, totalCost)
func ExtractCostInfo(obj map[string]any) (inputCost, outputCost, totalCost float64) {
	// 尝试从 cost 或 usage_cost 提取
	costInfo, _ := obj["cost"]
	if costInfo == nil {
		costInfo, _ = obj["usage_cost"]
	}

	if costInfo != nil {
		switch v := costInfo.(type) {
		case float64:
			totalCost = v
		case int:
			totalCost = float64(v)
		case map[string]any:
			inputCost = floatVal(v, "input_cost", "prompt_cost")
			outputCost = floatVal(v, "output_cost", "completion_cost")
			totalCost = floatVal(v, "total_cost")
			if totalCost == 0 {
				totalCost = inputCost + outputCost
			}
		}
	}

	// cost_details 兜底
	if inputCost == 0 && outputCost == 0 {
		costDetails, _ := obj["cost_details"]
		if details, ok := costDetails.(map[string]any); ok {
			inputCost = floatVal(details, "upstream_inference_prompt_cost")
			outputCost = floatVal(details, "upstream_inference_completions_cost")
			if totalCost == 0 {
				totalCost = floatVal(details, "upstream_inference_cost")
				if totalCost == 0 {
					totalCost = inputCost + outputCost
				}
			}
		}
	}

	return inputCost, outputCost, totalCost
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// convertOneMessage 将单条消息转换为 OpenAI API 格式的 dict。
//
// 通过类型断言处理不同消息类型的特有字段：
//   - *AssistantMessage → 追加 tool_calls（OpenAI 嵌套格式）+ reasoning_content
//   - *ToolMessage → 追加 tool_call_id
//   - 其他 → 仅 role + content
func (e *BaseClientEmbed) convertOneMessage(msg any) (map[string]any, error) {
	// 先提取 BaseMessage（所有消息类型都嵌入它）
	baseMsg, ok := toBaseMessage(msg)
	if !ok {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_INVOKE_PARAM_ERROR", 181004, ""),
			exception.WithMsg(fmt.Sprintf("unsupported message type: %T", msg)),
		)
	}

	msgDict := map[string]any{
		"role":    baseMsg.Role.String(),
		"content": baseMsg.Content,
	}

	if baseMsg.Name != "" {
		msgDict["name"] = baseMsg.Name
	}

	// AssistantMessage 特有字段
	if am, ok := msg.(*llmschema.AssistantMessage); ok {
		if len(am.ToolCalls) > 0 {
			calls := make([]map[string]any, 0, len(am.ToolCalls))
			for _, tc := range am.ToolCalls {
				calls = append(calls, tc.ToOpenAIFormat())
			}
			msgDict["tool_calls"] = calls
		}
		if am.ReasoningContent != "" {
			msgDict["reasoning_content"] = am.ReasoningContent
		}
	}

	// ToolMessage 特有字段
	if tm, ok := msg.(*llmschema.ToolMessage); ok {
		if tm.ToolCallID != "" {
			msgDict["tool_call_id"] = tm.ToolCallID
		}
	}

	return msgDict, nil
}

// floatVal 从 map 中提取浮点值，支持多个候选键名。
func floatVal(m map[string]any, keys ...string) float64 {
	for _, key := range keys {
		if v, ok := m[key]; ok {
			switch val := v.(type) {
			case float64:
				return val
			case int:
				return float64(val)
			}
		}
	}
	return 0
}
