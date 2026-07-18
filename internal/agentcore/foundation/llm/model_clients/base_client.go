package model_clients

import (
	"context"
	"fmt"
	"os"
	"strconv"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseOutputParser LLM 输出解析器接口。
//
// 2.6 节定义最小接口，2.16 节扩展完整方法（StreamParse + Parse 签名改为 any）。
//
// 对应 Python: openjiuwen/core/foundation/llm/output_parsers/output_parser.py (BaseOutputParser)
type BaseOutputParser interface {
	// Parse 解析 LLM 输出，返回结构化结果。
	//
	// 输入可以是 string 或 *AssistantMessage（对齐 Python Union[str, AssistantMessage]），
	// 实现时应通过类型断言处理。
	Parse(input any) (any, error)

	// StreamParse 流式解析 LLM 输出。
	//
	// 对应 Python: BaseOutputParser.stream_parse()。
	// chunks 支持 string 和 *AssistantMessageChunk 两种类型（对齐 Python Union[str, AssistantMessageChunk]）。
	// 注意：当前 model client 的 _astream_with_parser 路径不调用此方法，
	// 而是反复调用 Parse()。此方法为独立流式解析场景预留。
	StreamParse(chunks <-chan any) <-chan StreamParsedResult
}

// BaseModelClient LLM 模型客户端接口，所有模型客户端实现必须满足此接口。
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/base_model_client.py (BaseModelClient)
type BaseModelClient interface {
	// Invoke 非流式调用 LLM，返回完整的助手消息。
	Invoke(ctx context.Context, messages MessagesParam, opts ...InvokeOption) (*llmschema.AssistantMessage, error)

	// Stream 流式调用 LLM，返回纯 chunk channel，调用方通过 range chunkChan 消费。
	Stream(ctx context.Context, messages MessagesParam, opts ...StreamOption) (<-chan *llmschema.AssistantMessageChunk, error)

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

	// SupportsKVCacheRelease 检查客户端是否支持 KV Cache 释放。
	//
	// 零副作用判断：仅 InferenceAffinity 返回 true，其他客户端返回 false。
	// 对应 Python: Model.supports_kv_cache_release() 中 isinstance(self._client, InferenceAffinityModelClient)
	SupportsKVCacheRelease() bool
}

// StreamParsedResult 流式解析结果。
type StreamParsedResult struct {
	// Content 解析结果
	Content any
	// Error 解析错误
	Error error
}

// BaseClientEmbed BaseModelClient 的共享实现，具体客户端嵌入此结构体复用通用逻辑。
//
// 对应 Python: BaseModelClient 中的具体方法（非抽象方法）
//
// 使用方式：
//
//	type OpenAIModelClient struct {
//	    BaseClientEmbed（嵌入基础客户端）
//	    // ... OpenAI 特有字段
//	}
type BaseClientEmbed struct {
	// ModelConfig 模型请求配置
	ModelConfig *llmschema.ModelRequestConfig
	// ClientConfig 模型客户端配置
	ClientConfig *llmschema.ModelClientConfig
	// clientName 客户端名称（子类通过 WithClientName 设置）
	clientName string
	// skipValidate 是否跳过 ValidateConfig 校验（IntelliRouter 不需要 api_key/api_base）
	skipValidate bool
}

// BaseClientEmbedOption BaseClientEmbed 构造选项函数。
type BaseClientEmbedOption func(*BaseClientEmbed)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent model_clients 包日志组件标识（AgentCore 层）。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// WithClientName 设置客户端名称（用于错误消息和日志）。
func WithClientName(name string) BaseClientEmbedOption {
	return func(e *BaseClientEmbed) { e.clientName = name }
}

// WithSkipValidate 跳过 ValidateConfig 校验。
//
// 适用场景：IntelliRouter 等客户端不需要 api_key/api_base
// （在 deployment 级别配置，而非 ModelClientConfig 级别），
// 使用此选项跳过 BaseClientEmbed 的标准校验。
//
// 对应 Python: IntelliRouterModelClient._validate_config() 覆写为空
func WithSkipValidate() BaseClientEmbedOption {
	return func(e *BaseClientEmbed) { e.skipValidate = true }
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
//   - skipValidate 为 true 时跳过所有校验
//   - api_key: 必填
//   - api_base: 必填
//   - verify_ssl 为 true 时 ssl_cert 必填
//
// 对应 Python: BaseModelClient._validate_config()
func (e *BaseClientEmbed) ValidateConfig() error {
	// 跳过校验（IntelliRouter 等客户端不需要 api_key/api_base）
	if e.skipValidate {
		return nil
	}

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

// GetModelName 获取模型名称，供 tracer decorator 获取 instanceInfo 中的 class_name。
// 优先使用 ModelConfig.ModelName，为空时降级为 clientName。
func (e *BaseClientEmbed) GetModelName() string {
	if e.ModelConfig != nil && e.ModelConfig.ModelName != "" {
		return e.ModelConfig.ModelName
	}
	if e.clientName != "" {
		return e.clientName
	}
	return "BaseModelClient"
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
// 与 Python 一致：只提取 ToolInfo 公共字段（type/name/description/parameters），
// McpToolInfo 的 ServerName 字段不发送给 LLM。
//
// 输出格式：[{"type": "function", "function": {"name": "...", "description": "...", "parameters": {...}}}]
func (e *BaseClientEmbed) ConvertToolsToDict(tools []commonschema.ToolInfoInterface) []map[string]any {
	if len(tools) == 0 {
		return nil
	}

	result := make([]map[string]any, 0, len(tools))
	for _, tool := range tools {
		parameters := tool.GetParameters()
		if parameters == nil {
			parameters = make(map[string]any)
		}
		toolDict := map[string]any{
			"type": tool.GetType(),
			"function": map[string]any{
				"name":        tool.GetName(),
				"description": tool.GetDescription(),
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
func (e *BaseClientEmbed) BuildRequestParams(ctx context.Context, messagesDict []map[string]any, params *InvokeParams, stream bool) (map[string]any, error) {
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
		if params.TopP == nil && e.ModelConfig.TopP != nil {
			reqParams["top_p"] = *e.ModelConfig.TopP
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
			"output_parser":      true,
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
	temperature := reqParams["temperature"]
	topP := reqParams["top_p"]
	maxTokens := reqParams["max_tokens"]
	stop := reqParams["stop"]

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

	// 对齐 Python: _build_request_params 中使用 llm_logger.info（LogEventType.LLM_CALL_START），非回调
	if isSensitive {
		// 敏感模式：不记录 messages/tools
		evt := logger.Info(logComponent).
			Str("event_type", "llm_call_start").
			Str("model_name", model).
			Str("model_provider", modelProvider).
			Bool("is_stream", stream).
			Str("client_name", e.GetClientName())
		if temperature != nil {
			evt = evt.Any("temperature", temperature)
		}
		if topP != nil {
			evt = evt.Any("top_p", topP)
		}
		if maxTokens != nil {
			evt = evt.Any("max_tokens", maxTokens)
		}
		if stop != nil {
			evt = evt.Any("stop", stop)
		}
		evt.Any("extra_params", extraParams).
			Msg("Before request chat model, LLM request params ready.")
	} else {
		// 非敏感模式：记录 messages/tools
		evt := logger.Info(logComponent).
			Str("event_type", "llm_call_start").
			Str("model_name", model).
			Str("model_provider", modelProvider).
			Any("messages", messagesDict).
			Any("tools", reqParams["tools"]).
			Bool("is_stream", stream).
			Str("client_name", e.GetClientName())
		if temperature != nil {
			evt = evt.Any("temperature", temperature)
		}
		if topP != nil {
			evt = evt.Any("top_p", topP)
		}
		if maxTokens != nil {
			evt = evt.Any("max_tokens", maxTokens)
		}
		if stop != nil {
			evt = evt.Any("stop", stop)
		}
		evt.Any("extra_params", extraParams).
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
	costInfo := obj["cost"]
	if costInfo == nil {
		costInfo = obj["usage_cost"]
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
		costDetails := obj["cost_details"]
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
func (e *BaseClientEmbed) convertOneMessage(msg llmschema.BaseMessage) (map[string]any, error) {
	msgDict := map[string]any{
		"role":    msg.GetRole().String(),
		"content": msg.GetContent(),
	}

	if msg.GetName() != "" {
		msgDict["name"] = msg.GetName()
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
