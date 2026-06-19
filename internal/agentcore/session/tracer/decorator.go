package tracer

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// tracerSession 追踪装饰所需的会话接口，避免 tracer → internal 循环依赖。
// internal.AgentSession 天然满足此接口。
type tracerSession interface {
	// Tracer 获取追踪器
	Tracer() *Tracer
	// AgentSpan 获取 Agent 追踪跨度
	AgentSpan() *TraceAgentSpan
}

// TracedModelClient 追踪装饰的模型客户端，包装 BaseModelClient 并在 Invoke/Stream 调用时触发追踪事件。
// 实现 model_clients.BaseModelClient 接口。
//
// 对应 Python: TracedModelClient (openjiuwen/core/session/tracer/decorator.py)
type TracedModelClient struct {
	// inner 被装饰的原始模型客户端
	inner model_clients.BaseModelClient
	// tracer 追踪器
	tracer *Tracer
	// agentSpan Agent 追踪跨度
	agentSpan *TraceAgentSpan
	// instanceInfo 实例信息
	instanceInfo map[string]any
}

// TracedTool 追踪装饰的工具，包装 tool.Tool 并在 Invoke 调用时触发追踪事件。
// 实现 tool.Tool 接口。
//
// 对应 Python: TracedTool (openjiuwen/core/session/tracer/decorator.py)
type TracedTool struct {
	// inner 被装饰的原始工具
	inner tool.Tool
	// tracer 追踪器
	tracer *Tracer
	// agentSpan Agent 追踪跨度
	agentSpan *TraceAgentSpan
	// instanceInfo 实例信息
	instanceInfo map[string]any
}

// TracedWorkflow 追踪装饰的工作流占位结构体。
// ⤵️ 领域6 定义具体 WorkflowInterface 后替换 inner 类型并补充方法实现。
//
// 对应 Python: TracedWorkflow (openjiuwen/core/session/tracer/decorator.py)
type TracedWorkflow struct {
	// inner 被装饰的原始工作流实例
	inner any
	// tracer 追踪器
	tracer *Tracer
	// agentSpan Agent 追踪跨度
	agentSpan *TraceAgentSpan
	// instanceInfo 实例信息
	instanceInfo map[string]any
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Invoke 非流式调用 LLM，在调用前后触发追踪事件，并通过 tracer_record_data 回调记录中间过程。
// 流程：CreateAgentSpan → TriggerAgent(TraceLLMStart) → 注入 tracer_record_data 回调 → inner.Invoke → TriggerAgent(TraceLLMEnd/Error)
// 对齐 Python: decorate_model_with_trace 中 call_kwargs["tracer_record_data"] = tracer_record_data
func (c *TracedModelClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	span := c.tracer.AgentSpanManager.CreateAgentSpan(c.agentSpan)
	c.tracer.TriggerAgent(ctx, TraceLLMStart, &TriggerParams{
		Span:         &span.Span,
		Inputs:       messages,
		InstanceInfo: c.instanceInfo,
	})

	// 创建 tracer_record_data 回调闭包，对齐 Python:
	//   async def tracer_record_data(**kw):
	//       await tracer.trigger("tracer_agent", f"on_{invoke_type.value}_request", span=span, **kw)
	// 底层模型客户端在请求发送前和响应解析后调用此回调，触发 TraceLLMRequest 事件。
	spanPtr := &span.Span
	tracerRecordData := func(data map[string]any) {
		c.tracer.TriggerAgent(ctx, TraceLLMRequest, &TriggerParams{
			Span:         spanPtr,
			OnInvokeData: data,
		})
	}
	opts = append(opts, model_clients.WithInvokeTracerRecordData(tracerRecordData))

	result, err := c.inner.Invoke(ctx, messages, opts...)
	if err != nil {
		c.tracer.TriggerAgent(ctx, TraceLLMError, &TriggerParams{
			Span:  &span.Span,
			Error: err,
		})
		return nil, err
	}

	c.tracer.TriggerAgent(ctx, TraceLLMEnd, &TriggerParams{
		Span:    &span.Span,
		Outputs: result,
	})
	return result, nil
}

// Stream 流式调用 LLM，在调用前后触发追踪事件，并通过 tracer_record_data 回调记录中间过程。
// 流程：CreateAgentSpan → TriggerAgent(TraceLLMStart) → 注入 tracer_record_data 回调 → inner.Stream → Final() 收集结果 → TriggerAgent(TraceLLMEnd/Error)
// 对齐 Python: decorate_model_with_trace 中 call_kwargs["tracer_record_data"] = tracer_record_data
func (c *TracedModelClient) Stream(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.StreamOption) (*model_clients.StreamResult, error) {
	span := c.tracer.AgentSpanManager.CreateAgentSpan(c.agentSpan)
	c.tracer.TriggerAgent(ctx, TraceLLMStart, &TriggerParams{
		Span:         &span.Span,
		Inputs:       messages,
		InstanceInfo: c.instanceInfo,
	})

	// 创建 tracer_record_data 回调闭包，对齐 Python 同 Invoke
	spanPtr := &span.Span
	tracerRecordData := func(data map[string]any) {
		c.tracer.TriggerAgent(ctx, TraceLLMRequest, &TriggerParams{
			Span:         spanPtr,
			OnInvokeData: data,
		})
	}
	opts = append(opts, model_clients.WithStreamTracerRecordData(tracerRecordData))

	streamResult, err := c.inner.Stream(ctx, messages, opts...)
	if err != nil {
		c.tracer.TriggerAgent(ctx, TraceLLMError, &TriggerParams{
			Span:  &span.Span,
			Error: err,
		})
		return nil, err
	}

	// 等待流式结果收集完毕
	finalMsg := streamResult.Final()

	// 将最终合并的 chunk 转换为 AssistantMessage 作为输出记录
	var outputs any
	if finalMsg != nil {
		outputs = finalMsg.ToAssistantMessage()
	}

	c.tracer.TriggerAgent(ctx, TraceLLMEnd, &TriggerParams{
		Span:    &span.Span,
		Outputs: outputs,
	})

	return streamResult, nil
}

// GenerateImage 生成图片，直接委托 inner。
func (c *TracedModelClient) GenerateImage(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return c.inner.GenerateImage(ctx, messages, opts...)
}

// GenerateSpeech 生成语音，直接委托 inner。
func (c *TracedModelClient) GenerateSpeech(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return c.inner.GenerateSpeech(ctx, messages, opts...)
}

// GenerateVideo 生成视频，直接委托 inner。
func (c *TracedModelClient) GenerateVideo(ctx context.Context, messages []*llmschema.UserMessage, opts ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return c.inner.GenerateVideo(ctx, messages, opts...)
}

// Release 释放模型缓存或资源，直接委托 inner。
func (c *TracedModelClient) Release(ctx context.Context, opts ...model_clients.ReleaseOption) (bool, error) {
	return c.inner.Release(ctx, opts...)
}

// Invoke 执行工具，在调用前后触发追踪事件。
// 流程：CreateAgentSpan(agentSpan) → TriggerAgent(TracePluginStart) → inner.Invoke → TriggerAgent(TracePluginEnd/Error)
func (t *TracedTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	span := t.tracer.AgentSpanManager.CreateAgentSpan(t.agentSpan)
	t.tracer.TriggerAgent(ctx, TracePluginStart, &TriggerParams{
		Span:         &span.Span,
		Inputs:       inputs,
		InstanceInfo: t.instanceInfo,
	})

	result, err := t.inner.Invoke(ctx, inputs, opts...)
	if err != nil {
		t.tracer.TriggerAgent(ctx, TracePluginError, &TriggerParams{
			Span:  &span.Span,
			Error: err,
		})
		return nil, err
	}

	t.tracer.TriggerAgent(ctx, TracePluginEnd, &TriggerParams{
		Span:    &span.Span,
		Outputs: result,
	})
	return result, nil
}

// Stream 流式执行工具，直接委托 inner。
func (t *TracedTool) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return t.inner.Stream(ctx, inputs, opts...)
}

// Card 返回工具的配置卡片，直接委托 inner。
func (t *TracedTool) Card() *tool.ToolCard {
	return t.inner.Card()
}

// DecorateModelWithTrace 为模型客户端添加追踪装饰。
// 如果 session.Tracer() 或 session.AgentSpan() 为 nil，返回原始 model。
// instanceInfo 中 class_name 从模型配置获取（对齐 Python model.config.model_config.model_name），
// type 固定为 "llm"（对齐 Python decorate_model_with_trace）。
func DecorateModelWithTrace(model model_clients.BaseModelClient, session tracerSession) model_clients.BaseModelClient {
	tracerVal := session.Tracer()
	if tracerVal == nil {
		return model
	}

	agentSpan := session.AgentSpan()
	if agentSpan == nil {
		return model
	}

	// 尝试获取模型名称，对齐 Python model.config.model_config.model_name
	className := "BaseModelClient"
	if provider, ok := model.(ModelConfigProvider); ok {
		if name := provider.GetModelName(); name != "" {
			className = name
		}
	}

	return &TracedModelClient{
		inner:     model,
		tracer:    tracerVal,
		agentSpan: agentSpan,
		instanceInfo: map[string]any{
			"class_name": className,
			"type":       "llm",
		},
	}
}

// DecorateToolWithTrace 为工具添加追踪装饰。
// 如果 session.Tracer() 或 session.AgentSpan() 为 nil，返回原始 tool。
// instanceInfo 中 class_name 从 tool.Card().Name 获取（对齐 Python tool.card.name），
// type 固定为 "tool"（对齐 Python decorate_tool_with_trace）。
func DecorateToolWithTrace(t tool.Tool, session tracerSession) tool.Tool {
	tracerVal := session.Tracer()
	if tracerVal == nil {
		return t
	}

	agentSpan := session.AgentSpan()
	if agentSpan == nil {
		return t
	}

	// 从 tool.Card().Name 获取工具名称，对齐 Python tool.card.name
	className := "Tool"
	if card := t.Card(); card != nil && card.Name != "" {
		className = card.Name
	}

	return &TracedTool{
		inner:     t,
		tracer:    tracerVal,
		agentSpan: agentSpan,
		instanceInfo: map[string]any{
			"class_name": className,
			"type":       "tool",
		},
	}
}

// DecorateWorkflowWithTrace 为工作流添加追踪装饰。
// 如果 session.Tracer() 或 session.AgentSpan() 为 nil，返回原始 w。
// ⤵️ 领域6 定义具体 WorkflowInterface 后替换返回类型。
func DecorateWorkflowWithTrace(w any, session tracerSession) any {
	tracerVal := session.Tracer()
	if tracerVal == nil {
		return w
	}

	agentSpan := session.AgentSpan()
	if agentSpan == nil {
		return w
	}

	return &TracedWorkflow{
		inner:        w,
		tracer:       tracerVal,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "Workflow", "type": "workflow"},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// ModelConfigProvider 模型配置提供者接口，用于从模型客户端获取模型名称。
// 对齐 Python model.config.model_config.model_name 访问方式。
// 具体的模型客户端（如 OpenAIModelClient）嵌入 BaseClientEmbed，实现此接口。
type ModelConfigProvider interface {
	// GetModelName 获取模型名称
	GetModelName() string
}
