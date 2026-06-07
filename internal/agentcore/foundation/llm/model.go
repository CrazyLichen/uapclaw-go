package llm

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 接口 ────────────────────────────

// SessionLike 会话最小接口，用于 BuildKVCacheInvokeKwargs 获取 session_id。
//
// 对应 Python: Model.build_kv_cache_invoke_kwargs() 中 session.get_session_id()
type SessionLike interface {
	// GetSessionID 返回会话唯一标识。
	GetSessionID() string
}

// ──────────────────────────── 结构体 ────────────────────────────

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
// 流程：Trigger(callback.LLMInvokeInput) → client.Invoke → Trigger(callback.LLMInvokeOutput)
func (m *Model) Invoke(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.InvokeOption,
) (*llmschema.AssistantMessage, error) {
	// 提取参数用于事件数据
	params := model_clients.NewInvokeParams(opts...)
	modelName := m.resolveModelName(params.Model)

	// 1. 触发 callback.LLMInvokeInput 事件（调用前）
	_ = m.callbackFramework.TriggerLLM(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMInvokeInput,
		ModelName:     modelName,
		ModelProvider: m.ClientConfig.ClientProvider,
		IsStream:      false,
		Extra: map[string]any{
			"model_config":        m.ModelConfig,
			"model_client_config": m.ClientConfig,
		},
	})

	// 2. 调用底层客户端
	result, err := m.client.Invoke(ctx, messages, opts...)
	if err != nil {
		// 触发 callback.LLMCallError 事件
		_ = m.callbackFramework.TriggerLLM(ctx, &callback.LLMCallEventData{
			Event:         callback.LLMCallError,
			ModelName:     modelName,
			ModelProvider: m.ClientConfig.ClientProvider,
			IsStream:      false,
			Error:         err,
			Extra: map[string]any{
				"model_config":        m.ModelConfig,
				"model_client_config": m.ClientConfig,
			},
		})
		return nil, err
	}

	// 3. 触发 callback.LLMInvokeOutput 事件（调用后）
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
	_ = m.callbackFramework.TriggerLLM(ctx, eventData)

	return result, nil
}

// Stream 流式调用 LLM。
//
// 对应 Python: Model.stream()
// 流程：Trigger(callback.LLMStreamInput) → client.Stream → Trigger(callback.LLMStreamOutput)
//
// 注意：2.14 节仅在流开始和流结束时触发事件，
// 逐项触发（Python 的 emit_after + item_key="result"）在 6.24 节实现。
func (m *Model) Stream(
	ctx context.Context,
	messages model_clients.MessagesParam,
	opts ...model_clients.StreamOption,
) (*model_clients.StreamResult, error) {
	// 提取参数用于事件数据
	params := model_clients.NewStreamParams(opts...)
	modelName := m.resolveStreamModelName(params.Model)

	// 1. 触发 callback.LLMStreamInput 事件（流开始前）
	_ = m.callbackFramework.TriggerLLM(ctx, &callback.LLMCallEventData{
		Event:         callback.LLMStreamInput,
		ModelName:     modelName,
		ModelProvider: m.ClientConfig.ClientProvider,
		IsStream:      true,
		Extra: map[string]any{
			"model_config":        m.ModelConfig,
			"model_client_config": m.ClientConfig,
		},
	})

	// 2. 调用底层客户端
	result, err := m.client.Stream(ctx, messages, opts...)
	if err != nil {
		// 触发 callback.LLMCallError 事件
		_ = m.callbackFramework.TriggerLLM(ctx, &callback.LLMCallEventData{
			Event:         callback.LLMCallError,
			ModelName:     modelName,
			ModelProvider: m.ClientConfig.ClientProvider,
			IsStream:      true,
			Error:         err,
			Extra: map[string]any{
				"model_config":        m.ModelConfig,
				"model_client_config": m.ClientConfig,
			},
		})
		return nil, err
	}

	// 3. 启动后台 goroutine，流结束后触发 callback.LLMStreamOutput 事件
	go func() {
		// Final() 阻塞等待流结束
		finalChunk := result.Final()

		var usage *llmschema.UsageMetadata
		if finalChunk != nil && finalChunk.UsageMetadata != nil {
			usage = finalChunk.UsageMetadata
		}

		_ = m.callbackFramework.TriggerLLM(ctx, &callback.LLMCallEventData{
			Event:         callback.LLMStreamOutput,
			ModelName:     modelName,
			ModelProvider: m.ClientConfig.ClientProvider,
			IsStream:      true,
			Response:      finalChunk,
			Usage:         usage,
			Extra: map[string]any{
				"model_config":        m.ModelConfig,
				"model_client_config": m.ClientConfig,
			},
		})
	}()

	return result, nil
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
// 通过尝试调用 Release 并检查返回的错误判断是否支持。
// 更高效的做法：检查 client 是否实现了特定接口（6.24 节优化）。
//
// 对应 Python: Model.supports_kv_cache_release()
func (m *Model) SupportsKVCacheRelease() bool {
	// 尝试无副作用的判断：调用 Release(nil ctx 和空参数) 看返回的错误类型
	// 这里简化实现：委托给底层 client，如果 Release 返回 "不支持" 错误则为 false
	_, err := m.client.Release(context.Background())
	if err != nil {
		// 返回不支持错误时，说明不支持 KV cache release
		return false
	}
	return true
}

// BuildKVCacheInvokeKwargs 为 InferenceAffinity 客户端构建额外参数。
//
// 当底层客户端为 InferenceAffinity 时：
//   - session_id: 使用 session.GetSessionID()
//   - enable_cache_sharing: 跟随 enableKVCacheRelease 参数
//
// 对应 Python: Model.build_kv_cache_invoke_kwargs()
func (m *Model) BuildKVCacheInvokeKwargs(session SessionLike, enableKVCacheRelease bool) map[string]any {
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

// Format 实现 fmt.Formatter 接口，支持格式化输出 Model 信息。
func (m *Model) Format(f fmt.State, _ rune) {
	_, _ = fmt.Fprintf(f, "Model(provider=%s, model=%s)",
		m.ClientConfig.ClientProvider,
		m.resolveModelName(""),
	)
}
