package llm

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionLike 会话最小接口，用于 BuildKVCacheInvokeKwargs 获取 session_id。
//
// 对应 Python: Model.build_kv_cache_invoke_kwargs() 中 session.get_session_id()
type SessionLike interface {
	// GetSessionID 返回会话唯一标识。
	GetSessionID() string
}

// Model 统一 LLM 调用入口（门面模式）。
//
// 职责：
//  1. 根据 ModelClientConfig 创建 BaseModelClient 实例
//  2. 在 Invoke/Stream 前后触发 CallbackFramework 回调事件
//  3. 对外暴露统一的 LLM 调用接口
//
// 对应 Python: openjiuwen/core/foundation/llm/model.py (Model)
//
// 使用方式：
//
//	model, err := NewModel(clientConfig, modelConfig)
//	result, err := model.Invoke(ctx, messages, opts...)
type Model struct {
	// ModelConfig 模型请求配置
	ModelConfig *llmschema.ModelRequestConfig
	// ClientConfig 模型客户端配置
	ClientConfig *llmschema.ModelClientConfig
	// client 底层模型客户端
	client model_clients.BaseModelClient
	// callbackFramework 回调框架实例
	callbackFramework *callback.CallbackFramework
}

// ModelOption Model 构造选项函数。
type ModelOption func(*Model)

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewModel 创建 Model 实例。
//
// 对应 Python: Model.__init__(model_client_config, model_config)
//
// 创建流程：
//  1. 校验 clientConfig 非空
//  2. 调用 CreateModelClient 创建底层客户端
//  3. 关联 CallbackFramework（默认使用全局实例）
func NewModel(
	clientConfig *llmschema.ModelClientConfig,
	modelConfig *llmschema.ModelRequestConfig,
	opts ...ModelOption,
) (*Model, error) {
	if clientConfig == nil {
		return nil, exception.NewBaseError(
			exception.NewStatusCode("MODEL_SERVICE_CONFIG_ERROR", 181002, ""),
			exception.WithMsg("model client config is none"),
		)
	}

	// 创建底层客户端
	client, err := model_clients.CreateModelClient(clientConfig, modelConfig)
	if err != nil {
		return nil, err
	}

	m := &Model{
		ModelConfig:       modelConfig,
		ClientConfig:      clientConfig,
		client:            client,
		callbackFramework: callback.GetCallbackFramework(),
	}

	// 应用选项
	for _, opt := range opts {
		opt(m)
	}

	return m, nil
}

// WithCallbackFramework 设置自定义回调框架实例。
func WithCallbackFramework(fw *callback.CallbackFramework) ModelOption {
	return func(m *Model) { m.callbackFramework = fw }
}

// Invoke 非流式调用 LLM。
//
// 对应 Python: Model.invoke()
// 执行顺序：① transform_io input → ② emit_before → client.Invoke → ③ transform_io output → ④ emit_after
func (m *Model) Invoke(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	// 提取参数用于事件数据
	params := model_clients.NewInvokeParams(opts...)
	modelName := m.resolveModelName(params.Model)
	fw := m.callbackFramework

	// ① transform_io 输入变换（对齐 Python transform_io 的 input_fn）
	if transformed := fw.TransformLLMIOInput(ctx, callback.LLMInvokeInput, messages); transformed != nil {
		if v, ok := transformed.(model_clients.MessagesParam); ok {
			messages = v
		} else {
			logger.Warn(logger.ComponentAgentCore).
				Str("event", "TransformLLMIOInput").
				Str("expected", "MessagesParam").
				Str("actual", fmt.Sprintf("%T", transformed)).
				Msg("TransformIO 返回类型不匹配，使用原始输入")
		}
	}

	// ② emit_before: 触发 callback.LLMInvokeInput 事件（调用前）
	_ = fw.TriggerLLM(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMInvokeInput,
		ModelName:     modelName,
		ModelProvider: m.ClientConfig.ClientProvider,
		IsStream:      false,
		Messages:      messages,
		Extra: map[string]any{
			"model_config":        m.ModelConfig,
			"model_client_config": m.ClientConfig,
		},
	})

	// 调用底层客户端
	result, err := m.client.Invoke(ctx, messages, opts...)
	if err != nil {
		// 触发 callback.LLMCallError 事件
		_ = fw.TriggerLLM(ctx, &callback.LLMCallEventData{
			Event:         callback.LLMCallError,
			ModelName:     modelName,
			ModelProvider: m.ClientConfig.ClientProvider,
			IsStream:      false,
			Messages:      messages,
			Error:         err,
			Extra: map[string]any{
				"model_config":        m.ModelConfig,
				"model_client_config": m.ClientConfig,
			},
		})
		return nil, err
	}

	// ③ transform_io 输出变换（对齐 Python transform_io 的 output_fn）
	if transformed := fw.TransformLLMIOOutput(ctx, callback.LLMInvokeOutput, result); transformed != nil {
		if v, ok := transformed.(*llmschema.AssistantMessage); ok {
			result = v
		} else {
			logger.Warn(logger.ComponentAgentCore).
				Str("event", "TransformLLMIOOutput").
				Str("expected", "*AssistantMessage").
				Str("actual", fmt.Sprintf("%T", transformed)).
				Msg("TransformIO 返回类型不匹配，使用原始输出")
		}
	}

	// ④ emit_after: 触发 callback.LLMInvokeOutput 事件（调用后）
	eventData := &callback.LLMCallEventData{
		Event:         callback.LLMInvokeOutput,
		ModelName:     modelName,
		ModelProvider: m.ClientConfig.ClientProvider,
		IsStream:      false,
		Response:      result,
		Extra: map[string]any{
			"model_config":        m.ModelConfig,
			"model_client_config": m.ClientConfig,
		},
	}
	if result != nil && result.UsageMetadata != nil {
		eventData.Usage = result.UsageMetadata
	}
	_ = fw.TriggerLLM(ctx, eventData)

	return result, nil
}

// Stream 流式调用 LLM。
//
// 对应 Python: Model.stream()
// 执行顺序：① transform_io input → ② emit_before → client.Stream → per-item { ③ transform_io output → ④ emit_after }
//
// 对齐 Python 装饰器链：
//
//	fn = _fw.emit_before(LLM_STREAM_INPUT)(fn)
//	fn = _fw.transform_io(LLM_STREAM_INPUT, LLM_STREAM_OUTPUT)(fn)
//	fn = _fw.emit_after(LLM_STREAM_OUTPUT, item_key="result")(fn)
func (m *Model) Stream(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.StreamOption,
) (<-chan *llmschema.AssistantMessageChunk, error) {
	// 提取参数用于事件数据
	params := model_clients.NewStreamParams(opts...)
	modelName := m.resolveStreamModelName(params.Model)
	fw := m.callbackFramework

	// ① transform_io 输入变换（对齐 Python transform_io 的 input_fn）
	if transformed := fw.TransformLLMIOInput(ctx, callback.LLMStreamInput, messages); transformed != nil {
		if v, ok := transformed.(model_clients.MessagesParam); ok {
			messages = v
		} else {
			logger.Warn(logger.ComponentAgentCore).
				Str("event", "TransformLLMIOInput").
				Str("expected", "MessagesParam").
				Str("actual", fmt.Sprintf("%T", transformed)).
				Msg("TransformIO 返回类型不匹配，使用原始输入")
		}
	}

	// ② emit_before: 触发 LLMStreamInput 事件（流开始前）
	_ = fw.TriggerLLM(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMStreamInput,
		ModelName:     modelName,
		ModelProvider: m.ClientConfig.ClientProvider,
		IsStream:      true,
		Messages:      messages,
		Extra: map[string]any{
			"model_config":        m.ModelConfig,
			"model_client_config": m.ClientConfig,
		},
	})

	// 调用底层客户端
	chunkChan, err := m.client.Stream(ctx, messages, opts...)
	if err != nil {
		// 触发 callback.LLMCallError 事件
		_ = fw.TriggerLLM(ctx, &callback.LLMCallEventData{
			Event:         callback.LLMCallError,
			ModelName:     modelName,
			ModelProvider: m.ClientConfig.ClientProvider,
			IsStream:      true,
			Messages:      messages,
			Error:         err,
			Extra: map[string]any{
				"model_config":        m.ModelConfig,
				"model_client_config": m.ClientConfig,
			},
		})
		return nil, err
	}

	// 包装 channel：per-item { ③ transform_io 输出变换 → ④ emit_after }
	out := make(chan *llmschema.AssistantMessageChunk)
	go func() {
		defer close(out)
		for chunk := range chunkChan {
			// ③ transform_io 输出变换（对齐 Python transform_io 的 output_fn，per item）
			if transformed := fw.TransformLLMIOOutput(ctx, callback.LLMStreamOutput, chunk); transformed != nil {
				if v, ok := transformed.(*llmschema.AssistantMessageChunk); ok {
					chunk = v
				} else {
					logger.Warn(logger.ComponentAgentCore).
						Str("event", "TransformLLMIOOutput").
						Str("expected", "*AssistantMessageChunk").
						Str("actual", fmt.Sprintf("%T", transformed)).
						Msg("TransformIO 返回类型不匹配，使用原始输出")
				}
			}

			// ④ emit_after (per_item): 每 chunk 触发 LLMStreamOutput
			var usage *llmschema.UsageMetadata
			if chunk != nil && chunk.UsageMetadata != nil {
				usage = chunk.UsageMetadata
			}
			_ = fw.TriggerLLM(ctx, &callback.LLMCallEventData{
				Event:         callback.LLMStreamOutput,
				ModelName:     modelName,
				ModelProvider: m.ClientConfig.ClientProvider,
				IsStream:      true,
				Response:      chunk,
				Usage:         usage,
				Extra: map[string]any{
					"model_config":        m.ModelConfig,
					"model_client_config": m.ClientConfig,
				},
			})

			out <- chunk
		}
	}()

	return out, nil
}

// Release 释放模型缓存/资源。
//
// 仅 InferenceAffinity 客户端支持 KV Cache 释放，其他客户端返回不支持错误。
//
// 对应 Python: Model.release()
func (m *Model) Release(ctx context.Context, opts ...model_clients.ReleaseOption) (bool, error) {
	return m.client.Release(ctx, opts...)
}

// SupportsKVCacheRelease 检查底层客户端是否支持 KV cache release。
//
// 零副作用判断：委托给底层 client 的 SupportsKVCacheRelease 方法，
// 仅 InferenceAffinity 返回 true，其他客户端返回 false。
//
// 对应 Python: Model.supports_kv_cache_release()
//
//	Python 使用 isinstance(self._client, InferenceAffinityModelClient) 判断，
//	Go 通过接口方法实现等价语义。
func (m *Model) SupportsKVCacheRelease() bool {
	return m.client.SupportsKVCacheRelease()
}

// BuildKVCacheInvokeKwargs 为 InferenceAffinity 客户端构建额外参数。
//
// 仅当底层客户端支持 KV Cache 释放时才构建 kwargs，
// 其他客户端直接返回空 map，避免向 API 请求注入无意义参数。
//
// 当底层客户端为 InferenceAffinity 时：
//   - session_id: 使用 session.GetSessionID()
//   - enable_cache_sharing: 跟随 enableKVCacheRelease 参数
//
// 对应 Python: Model.build_kv_cache_invoke_kwargs()
func (m *Model) BuildKVCacheInvokeKwargs(session SessionLike, enableKVCacheRelease bool) map[string]any {
	// 对齐 Python: 仅 InferenceAffinity 客户端需要构建 KV cache kwargs
	if !m.SupportsKVCacheRelease() {
		return map[string]any{}
	}

	extra := make(map[string]any)

	if session != nil {
		extra["session_id"] = session.GetSessionID()
	}
	if enableKVCacheRelease {
		extra["enable_cache_sharing"] = true
	}

	return extra
}

// GenerateImage 生成图片。
//
// 对应 Python: Model.generate_image()
func (m *Model) GenerateImage(
	ctx context.Context,
	messages []*llmschema.UserMessage,
	opts ...model_clients.GenerateImageOption,
) (*llmschema.ImageGenerationResponse, error) {
	return m.client.GenerateImage(ctx, messages, opts...)
}

// GenerateSpeech 生成语音。
//
// 对应 Python: Model.generate_speech()
func (m *Model) GenerateSpeech(
	ctx context.Context,
	messages []*llmschema.UserMessage,
	opts ...model_clients.GenerateSpeechOption,
) (*llmschema.AudioGenerationResponse, error) {
	return m.client.GenerateSpeech(ctx, messages, opts...)
}

// GenerateVideo 生成视频。
//
// 对应 Python: Model.generate_video()
func (m *Model) GenerateVideo(
	ctx context.Context,
	messages []*llmschema.UserMessage,
	opts ...model_clients.GenerateVideoOption,
) (*llmschema.VideoGenerationResponse, error) {
	return m.client.GenerateVideo(ctx, messages, opts...)
}

// GetClient 获取底层客户端实例。
//
// 用于类型断言（如判断是否为 InferenceAffinity 客户端），
// 大多数场景不需要直接访问底层客户端。
func (m *Model) GetClient() model_clients.BaseModelClient {
	return m.client
}

// Format 实现 fmt.Formatter 接口，支持格式化输出 Model 信息。
func (m *Model) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "Model(provider=%s, model=%s)",
		m.ClientConfig.ClientProvider,
		m.resolveModelName(""),
	)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveModelName 解析非流式调用的模型名称。
//
// 优先使用参数中指定的模型名称，其次使用 ModelConfig 中的默认值。
func (m *Model) resolveModelName(paramModel string) string {
	if paramModel != "" {
		return paramModel
	}
	if m.ModelConfig != nil {
		return m.ModelConfig.ModelName
	}
	return ""
}

// resolveStreamModelName 解析流式调用的模型名称。
func (m *Model) resolveStreamModelName(paramModel string) string {
	if paramModel != "" {
		return paramModel
	}
	if m.ModelConfig != nil {
		return m.ModelConfig.ModelName
	}
	return ""
}
