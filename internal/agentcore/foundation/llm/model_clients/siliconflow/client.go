package siliconflow

import (
	"context"
	"reflect"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients/openai"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent siliconflow 包日志组件标识（AgentCore 层）。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// SiliconFlowModelClient SiliconFlow 模型客户端。
//
// 嵌入 OpenAIModelClient 复用 HTTP 请求/响应解析/SSE 等基础能力，
// 覆写 Invoke/Stream，在委托前对消息中的 tool_calls 做清洗（sanitize）。
//
// SiliconFlow API 兼容 OpenAI Chat Completion 协议，但对请求中的非标准字段严格：
//   - tool_calls 中的 type 必须为 "function"，其他值会报错
//   - tool_calls 中不能包含非标准扩展字段，否则 API 报错
//   - 本客户端在发送请求前对 assistant 消息的 tool_calls 做清洗，只保留标准字段
//
// 对应 Python: openjiuwen/core/foundation/llm/model_clients/siliconflow_model_client.py (SiliconFlowModelClient)
type SiliconFlowModelClient struct {
	openai.OpenAIModelClient
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSiliconFlowModelClient 创建 SiliconFlow 客户端。
//
// 构造流程与 DeepSeek 客户端一致：
//  1. 先构造 OpenAI 客户端（复用 baseHeaders 初始化等）
//  2. 覆盖 clientName 为 "SiliconFlow client"
//
// 对应 Python: SiliconFlowModelClient.__init__(model_config, model_client_config)
func NewSiliconFlowModelClient(
	modelConfig *llmschema.ModelRequestConfig,
	clientConfig *llmschema.ModelClientConfig,
) (*SiliconFlowModelClient, error) {
	// 1. 构造 OpenAI 客户端（复用 baseHeaders 初始化等）
	openaiClient, err := openai.NewOpenAIModelClient(modelConfig, clientConfig)
	if err != nil {
		return nil, err
	}

	// 2. 覆盖 clientName 为 SiliconFlow（OpenAI 构造函数设置的是 "OpenAI client"）
	embed, err := model_clients.NewBaseClientEmbed(
		modelConfig,
		clientConfig,
		model_clients.WithClientName("SiliconFlow client"),
	)
	if err != nil {
		return nil, err
	}
	openaiClient.BaseClientEmbed = *embed

	return &SiliconFlowModelClient{
		OpenAIModelClient: *openaiClient,
	}, nil
}

// Invoke 非流式调用 SiliconFlow API。
//
// 覆写 OpenAI 客户端的 Invoke，在委托前对消息中的 tool_calls 做清洗。
// SiliconFlow API 对非标准字段严格，需要只保留标准字段并强制 type="function"。
//
// 对应 Python: SiliconFlowModelClient.invoke()
func (c *SiliconFlowModelClient) Invoke(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	// 1. 预处理消息：基类转换 + sanitize tool_calls
	sanitizedMsgs, err := c.sanitizeMessages(messages)
	if err != nil {
		return nil, err
	}

	// 2. 委托给 OpenAI 客户端（Dicts 模式直接透传，不会二次转换）
	return c.OpenAIModelClient.Invoke(ctx, sanitizedMsgs, opts...)
}

// Stream 流式调用 SiliconFlow API。
//
// 覆写 OpenAI 客户端的 Stream，在委托前对消息中的 tool_calls 做清洗。
// 逻辑与 Invoke 一致，详见 Invoke 注释。
//
// 对应 Python: SiliconFlowModelClient.stream()
func (c *SiliconFlowModelClient) Stream(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.StreamOption,
) (*model_clients.StreamResult, error) {
	// 1. 预处理消息：基类转换 + sanitize tool_calls
	sanitizedMsgs, err := c.sanitizeMessages(messages)
	if err != nil {
		return nil, err
	}

	// 2. 委托给 OpenAI 客户端
	return c.OpenAIModelClient.Stream(ctx, sanitizedMsgs, opts...)
}

// GenerateImage 生成图片（当前不支持）。
//
// SiliconFlow Chat Completion API 不支持图片生成。
func (c *SiliconFlowModelClient) GenerateImage(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateImageOption,
) (*llmschema.ImageGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("SiliconFlow client does not support image generation"),
	)
}

// GenerateSpeech 生成语音（当前不支持）。
func (c *SiliconFlowModelClient) GenerateSpeech(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateSpeechOption,
) (*llmschema.AudioGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("SiliconFlow client does not support speech generation"),
	)
}

// GenerateVideo 生成视频（当前不支持）。
func (c *SiliconFlowModelClient) GenerateVideo(
	_ context.Context,
	_ []*llmschema.UserMessage,
	_ ...model_clients.GenerateVideoOption,
) (*llmschema.VideoGenerationResponse, error) {
	return nil, exception.NewBaseError(
		exception.StatusModelCallFailed,
		exception.WithMsg("SiliconFlow client does not support video generation"),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// init 注册 SiliconFlow 客户端到全局注册表（2.6 回填点）。
func init() {
	registry := model_clients.GetClientRegistry()

	siliconFlowFactory := func(mc *llmschema.ModelRequestConfig, cc *llmschema.ModelClientConfig) model_clients.BaseModelClient {
		client, err := NewSiliconFlowModelClient(mc, cc)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("创建 SiliconFlow 客户端失败")
			return nil
		}
		return client
	}

	registry.Register("SiliconFlow", "llm", siliconFlowFactory)
}

// sanitizeMessages 对消息做预处理：先调用基类转换，再清洗 tool_calls。
//
// 处理流程：
//  1. 先调用 OpenAI 基类的 ConvertMessagesToDict 做标准转换
//  2. 对转换后的消息做 sanitizeToolCalls（只保留标准字段，强制 type="function"）
//  3. 包装为 Dicts 模式回传（Dicts 模式直接透传，零转换开销）
//
// 对应 Python: SiliconFlowModelClient._build_and_sanitize_params()
func (c *SiliconFlowModelClient) sanitizeMessages(
	messages model_clients.MessagesParam,
) (model_clients.MessagesParam, error) {
	// 1. 先调用基类转换
	result, err := c.OpenAIModelClient.ConvertMessagesToDict(messages)
	if err != nil {
		return model_clients.MessagesParam{}, err
	}

	// 2. 对消息做 sanitize tool_calls
	sanitizeToolCalls(result)

	// 3. 包装为 Dicts 模式（直接透传，不二次转换）
	return model_clients.NewDictsMessagesParam(result), nil
}

// sanitizeToolCalls 清洗消息中的 tool_calls，只保留 OpenAI 标准字段。
//
// SiliconFlow API 对请求中的非标准字段严格，遇到不认识的字段会报错。
// 本函数对每个 assistant 消息的 tool_calls 做清洗：
//   - 只保留标准字段：id、type、index、function.name、function.arguments
//   - 强制 type 为 "function"（某些 LLM 返回的 type 可能为空或其他值）
//   - 移除非标准扩展字段（LLM 返回的原始 tool_calls 可能包含额外字段）
//
// 对应 Python: SiliconFlowModelClient._sanitize_tool_calls()
func sanitizeToolCalls(messages []map[string]any) {
	for _, msg := range messages {
		// 仅处理 assistant 消息
		if role, ok := msg["role"].(string); !ok || role != "assistant" {
			continue
		}

		// 获取 tool_calls 列表（支持 []map[string]any 和 []any 两种类型）
		// Go 中 map[string]any 存储的 slice 可能是 []map[string]any（如 ToOpenAIFormat 输出）
		// 或 []any（如 JSON 反序列化或 Dicts 模式手动构造），两种都需要处理
		var toolCalls []map[string]any
		switch tc := msg["tool_calls"].(type) {
		case []map[string]any:
			toolCalls = tc
		case []any:
			// 将 []any 中的每个元素断言为 map[string]any
			for _, item := range tc {
				if dict, ok := item.(map[string]any); ok {
					toolCalls = append(toolCalls, dict)
				}
			}
		default:
			// 使用反射处理其他 slice 类型（如 JSON 反序列化产生的类型）
			rv := reflect.ValueOf(msg["tool_calls"])
			if rv.Kind() != reflect.Slice || rv.IsNil() {
				continue
			}
			for i := 0; i < rv.Len(); i++ {
				if dict, ok := rv.Index(i).Interface().(map[string]any); ok {
					toolCalls = append(toolCalls, dict)
				}
			}
		}
		if len(toolCalls) == 0 {
			continue
		}

		cleaned := make([]map[string]any, 0, len(toolCalls))
		for _, tcDict := range toolCalls {
			// 提取 function 部分
			funcPart, _ := tcDict["function"].(map[string]any)
			if funcPart == nil {
				funcPart = make(map[string]any)
			}
			name, _ := funcPart["name"].(string)
			args, _ := funcPart["arguments"].(string)

			// 只保留标准字段，强制 type="function"
			cleanedTC := map[string]any{
				"id":   tcDict["id"],
				"type": "function",
				"function": map[string]any{
					"name":      name,
					"arguments": args,
				},
			}

			// 保留 index（如果存在）
			if idx, ok := tcDict["index"]; ok {
				cleanedTC["index"] = idx
			}

			cleaned = append(cleaned, cleanedTC)
		}
		msg["tool_calls"] = cleaned
	}
}
