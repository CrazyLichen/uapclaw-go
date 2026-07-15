package decorator

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ModelConfigProvider 模型配置提供者接口，用于从模型客户端获取模型名称。
// 对齐 Python model.config.model_config.model_name 访问方式。
// 具体的模型客户端（如 OpenAIModelClient）嵌入 BaseClientEmbed，实现此接口。
type ModelConfigProvider interface {
	// GetModelName 获取模型名称
	GetModelName() string
}

// TracerSession 追踪装饰所需的会话最小接口。
//
// 从 tracer 父包抽出至 decorator 子包，避免 tracer → single_agent/interfaces 循环依赖。
// internal.AgentSession 天然满足此接口。
type TracerSession interface {
	// Tracer 获取追踪器
	Tracer() *tracer.Tracer
	// AgentSpan 获取 Agent 追踪跨度
	AgentSpan() *tracer.TraceAgentSpan
}

// TracedModelClient 追踪装饰的模型客户端，包装 BaseModelClient 并在 Invoke/Stream 调用时触发追踪事件。
// 实现 model_clients.BaseModelClient 接口。
//
// 对应 Python: TracedModelClient (openjiuwen/core/session/tracer/decorator.py)
type TracedModelClient struct {
	// inner 被装饰的原始模型客户端
	inner model_clients.BaseModelClient
	// tracer 追踪器
	tracer *tracer.Tracer
	// agentSpan Agent 追踪跨度
	agentSpan *tracer.TraceAgentSpan
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
	tracer *tracer.Tracer
	// agentSpan Agent 追踪跨度
	agentSpan *tracer.TraceAgentSpan
	// instanceInfo 实例信息
	instanceInfo map[string]any
}

// TracedWorkflow 追踪装饰的工作流，包装 Workflow 并在 Invoke/Stream 调用时触发追踪事件。
// 实现 sainterfaces.Workflow 接口。
//
// 对应 Python: decorate_workflow_with_trace 返回的 _TraceProxy (openjiuwen/core/session/tracer/decorator.py)
// Python 同时包装 invoke 和 stream，Go 当前包装 Invoke，Stream 在领域八扩展时回填。
type TracedWorkflow struct {
	// inner 被装饰的原始工作流实例
	inner sainterfaces.Workflow
	// tracer 追踪器
	tracer *tracer.Tracer
	// agentSpan Agent 追踪跨度
	agentSpan *tracer.TraceAgentSpan
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
	c.tracer.TriggerAgent(ctx, tracer.TraceLLMStart, &tracer.TriggerParams{
		Span:         &span.Span,
		Inputs:       messages,
		InstanceInfo: c.instanceInfo,
	})

	// 创建 tracer_record_data 回调闭包，对齐 Python:
	//
	//	async def tracer_record_data(**kw):
	//	    await tracer.trigger("tracer_agent", f"on_{invoke_type.value}_request", span=span, **kw)
	//
	// 底层模型客户端在请求发送前和响应解析后调用此回调，触发 TraceLLMRequest 事件。
	spanPtr := &span.Span
	tracerRecordData := func(data map[string]any) {
		c.tracer.TriggerAgent(ctx, tracer.TraceLLMRequest, &tracer.TriggerParams{
			Span:         spanPtr,
			OnInvokeData: data,
		})
	}
	opts = append(opts, model_clients.WithInvokeTracerRecordData(tracerRecordData))

	result, err := c.inner.Invoke(ctx, messages, opts...)
	if err != nil {
		c.tracer.TriggerAgent(ctx, tracer.TraceLLMError, &tracer.TriggerParams{
			Span:  &span.Span,
			Error: err,
		})
		return nil, err
	}

	c.tracer.TriggerAgent(ctx, tracer.TraceLLMEnd, &tracer.TriggerParams{
		Span:    &span.Span,
		Outputs: result,
	})
	return result, nil
}

// Stream 流式调用 LLM，在调用前后触发追踪事件，并通过 tracer_record_data 回调记录中间过程。
// 执行顺序：CreateAgentSpan → TriggerAgent(TraceLLMStart) → 注入 tracer_record_data 回调 → inner.Stream → 逐 chunk 透传 → TriggerAgent(TraceLLMEnd/Error)
// 对齐 Python: _make_trace_stream_wrap_handler 中 async for item in call_next(...): yield item
func (c *TracedModelClient) Stream(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	span := c.tracer.AgentSpanManager.CreateAgentSpan(c.agentSpan)
	c.tracer.TriggerAgent(ctx, tracer.TraceLLMStart, &tracer.TriggerParams{
		Span:         &span.Span,
		Inputs:       messages,
		InstanceInfo: c.instanceInfo,
	})

	// 注入 tracer_record_data 回调（对齐 Python: call_kwargs["tracer_record_data"] = tracer_record_data）
	spanPtr := &span.Span
	tracerRecordData := func(data map[string]any) {
		c.tracer.TriggerAgent(ctx, tracer.TraceLLMRequest, &tracer.TriggerParams{
			Span:         spanPtr,
			OnInvokeData: data,
		})
	}
	opts = append(opts, model_clients.WithStreamTracerRecordData(tracerRecordData))

	chunkChan, err := c.inner.Stream(ctx, messages, opts...)
	if err != nil {
		c.tracer.TriggerAgent(ctx, tracer.TraceLLMError, &tracer.TriggerParams{
			Span:  &span.Span,
			Error: err,
		})
		return nil, err
	}

	// 逐 chunk 透传，流结束后触发 TraceLLMEnd（对齐 Python _make_trace_stream_wrap_handler）
	out := make(chan *llmschema.AssistantMessageChunk)
	go func() {
		defer close(out)
		for chunk := range chunkChan {
			out <- chunk
		}
		// 流结束，触发 TraceLLMEnd
		c.tracer.TriggerAgent(ctx, tracer.TraceLLMEnd, &tracer.TriggerParams{
			Span: &span.Span,
		})
	}()

	return out, nil
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

// SupportsKVCacheRelease 委托给底层客户端判断是否支持 KV Cache 释放。
func (c *TracedModelClient) SupportsKVCacheRelease() bool {
	return c.inner.SupportsKVCacheRelease()
}

// Invoke 执行工具，在调用前后触发追踪事件。
// 流程：CreateAgentSpan(agentSpan) → TriggerAgent(TracePluginStart) → inner.Invoke → TriggerAgent(TracePluginEnd/Error)
func (t *TracedTool) Invoke(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (map[string]any, error) {
	span := t.tracer.AgentSpanManager.CreateAgentSpan(t.agentSpan)
	t.tracer.TriggerAgent(ctx, tracer.TracePluginStart, &tracer.TriggerParams{
		Span:         &span.Span,
		Inputs:       inputs,
		InstanceInfo: t.instanceInfo,
	})

	result, err := t.inner.Invoke(ctx, inputs, opts...)
	if err != nil {
		t.tracer.TriggerAgent(ctx, tracer.TracePluginError, &tracer.TriggerParams{
			Span:  &span.Span,
			Error: err,
		})
		return nil, err
	}

	t.tracer.TriggerAgent(ctx, tracer.TracePluginEnd, &tracer.TriggerParams{
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

// Invoke 非流式执行工作流，在调用前后触发追踪事件。
// 流程：CreateAgentSpan → TriggerAgent(TraceWorkflowStart) → inner.Invoke → TriggerAgent(TraceWorkflowEnd/Error)
//
// 对应 Python: async_trace(workflow.invoke, session, InvokeType.WORKFLOW, instance_info)
func (w *TracedWorkflow) Invoke(ctx context.Context, inputs map[string]any, opts ...sainterfaces.WorkflowOption) (any, error) {
	span := w.tracer.AgentSpanManager.CreateAgentSpan(w.agentSpan)
	w.tracer.TriggerAgent(ctx, tracer.TraceWorkflowStart, &tracer.TriggerParams{
		Span:         &span.Span,
		Inputs:       inputs,
		InstanceInfo: w.instanceInfo,
	})

	result, err := w.inner.Invoke(ctx, inputs, opts...)
	if err != nil {
		w.tracer.TriggerAgent(ctx, tracer.TraceWorkflowError, &tracer.TriggerParams{
			Span:  &span.Span,
			Error: err,
		})
		return nil, err
	}

	w.tracer.TriggerAgent(ctx, tracer.TraceWorkflowEnd, &tracer.TriggerParams{
		Span:    &span.Span,
		Outputs: result,
	})
	return result, nil
}

// Stream 流式执行工作流，直接委托 inner。
// ⤵️ 领域八回填：流式 trace 包装逻辑，对齐 Python decorate_workflow_with_trace 中的 stream 包装。
func (w *TracedWorkflow) Stream(ctx context.Context, inputs map[string]any, opts ...sainterfaces.WorkflowOption) (<-chan stream.Schema, error) {
	return w.inner.Stream(ctx, inputs, opts...)
}

// Card 返回工作流配置卡片，直接委托 inner。
//
// 对应 Python: workflow.card 属性
func (w *TracedWorkflow) Card() *schema.WorkflowCard {
	return w.inner.Card()
}

// DecorateModelWithTrace 为模型客户端添加追踪装饰。
// 如果 session.Tracer() 或 session.AgentSpan() 为 nil，返回原始 model。
// instanceInfo 中 class_name 从模型配置获取（对齐 Python model.config.model_config.model_name），
// type 固定为 "llm"（对齐 Python decorate_model_with_trace）。
func DecorateModelWithTrace(model model_clients.BaseModelClient, session TracerSession) model_clients.BaseModelClient {
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
func DecorateToolWithTrace(t tool.Tool, session TracerSession) tool.Tool {
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
// instanceInfo 中 class_name 和 metadata 从 workflow.Card() 获取
// （对齐 Python workflow.card.name / workflow.card.id 等），
// type 固定为 "workflow"（对齐 Python decorate_workflow_with_trace）。
func DecorateWorkflowWithTrace(w sainterfaces.Workflow, session TracerSession) sainterfaces.Workflow {
	tracerVal := session.Tracer()
	if tracerVal == nil {
		return w
	}

	agentSpan := session.AgentSpan()
	if agentSpan == nil {
		return w
	}

	// 从 workflow.Card() 提取元数据，对齐 Python:
	//
	//	metadata = dict(id=workflow.card.id, name=workflow.card.name,
	//	                description=workflow.card.description,
	//	                version=workflow.card.version)
	//	instance_info = {"class_name": workflow.card.name, "type": "workflow", "metadata": metadata}
	className := "Workflow"
	instanceInfo := map[string]any{"class_name": className, "type": "workflow"}

	if card := w.Card(); card != nil {
		if card.Name != "" {
			className = card.Name
		}
		instanceInfo["class_name"] = className
		instanceInfo["metadata"] = map[string]any{
			"id":          card.ID,
			"name":        card.Name,
			"description": card.Description,
			"version":     card.Version,
		}
	}

	return &TracedWorkflow{
		inner:        w,
		tracer:       tracerVal,
		agentSpan:    agentSpan,
		instanceInfo: instanceInfo,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
