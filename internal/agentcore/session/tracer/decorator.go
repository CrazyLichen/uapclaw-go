package tracer

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/internal"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

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

// Invoke 非流式调用 LLM，在调用前后触发追踪事件。
// 流程：CreateAgentSpan(agentSpan) → TriggerAgent(TraceLLMStart) → inner.Invoke → TriggerAgent(TraceLLMEnd/Error)
func (c *TracedModelClient) Invoke(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	span := c.tracer.AgentSpanManager.CreateAgentSpan(c.agentSpan)
	c.tracer.TriggerAgent(ctx, TraceLLMStart, &TriggerParams{
		Span:         &span.Span,
		Inputs:       messages,
		InstanceInfo: c.instanceInfo,
	})

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

// Stream 流式调用 LLM，在调用前后触发追踪事件，收集流式结果后触发结束事件。
// 流程：CreateAgentSpan(agentSpan) → TriggerAgent(TraceLLMStart) → inner.Stream → Final() 收集结果 → TriggerAgent(TraceLLMEnd/Error)
func (c *TracedModelClient) Stream(ctx context.Context, messages model_clients.MessagesParam, opts ...model_clients.StreamOption) (*model_clients.StreamResult, error) {
	span := c.tracer.AgentSpanManager.CreateAgentSpan(c.agentSpan)
	c.tracer.TriggerAgent(ctx, TraceLLMStart, &TriggerParams{
		Span:         &span.Span,
		Inputs:       messages,
		InstanceInfo: c.instanceInfo,
	})

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
func DecorateModelWithTrace(model model_clients.BaseModelClient, session *internal.AgentSession) model_clients.BaseModelClient {
	tracerVal := session.Tracer()
	if tracerVal == nil {
		return model
	}
	tracer, ok := tracerVal.(*Tracer)
	if !ok {
		logger.Warn(logComponent).
			Str("action", "decorate_model_with_trace").
			Msg("session.Tracer() 类型不是 *Tracer，跳过装饰")
		return model
	}

	agentSpanVal := session.AgentSpan()
	if agentSpanVal == nil {
		return model
	}
	agentSpan, ok := agentSpanVal.(*TraceAgentSpan)
	if !ok {
		logger.Warn(logComponent).
			Str("action", "decorate_model_with_trace").
			Msg("session.AgentSpan() 类型不是 *TraceAgentSpan，跳过装饰")
		return model
	}

	return &TracedModelClient{
		inner:     model,
		tracer:    tracer,
		agentSpan: agentSpan,
		instanceInfo: map[string]any{
			"class_name": "BaseModelClient",
		},
	}
}

// DecorateToolWithTrace 为工具添加追踪装饰。
// 如果 session.Tracer() 或 session.AgentSpan() 为 nil，返回原始 tool。
func DecorateToolWithTrace(t tool.Tool, session *internal.AgentSession) tool.Tool {
	tracerVal := session.Tracer()
	if tracerVal == nil {
		return t
	}
	tracer, ok := tracerVal.(*Tracer)
	if !ok {
		logger.Warn(logComponent).
			Str("action", "decorate_tool_with_trace").
			Msg("session.Tracer() 类型不是 *Tracer，跳过装饰")
		return t
	}

	agentSpanVal := session.AgentSpan()
	if agentSpanVal == nil {
		return t
	}
	agentSpan, ok := agentSpanVal.(*TraceAgentSpan)
	if !ok {
		logger.Warn(logComponent).
			Str("action", "decorate_tool_with_trace").
			Msg("session.AgentSpan() 类型不是 *TraceAgentSpan，跳过装饰")
		return t
	}

	return &TracedTool{
		inner:     t,
		tracer:    tracer,
		agentSpan: agentSpan,
		instanceInfo: map[string]any{
			"class_name": "Tool",
		},
	}
}

// DecorateWorkflowWithTrace 为工作流添加追踪装饰。
// 如果 session.Tracer() 或 session.AgentSpan() 为 nil，返回原始 w。
// ⤵️ 领域6 定义具体 WorkflowInterface 后替换返回类型。
func DecorateWorkflowWithTrace(w any, session *internal.AgentSession) any {
	tracerVal := session.Tracer()
	if tracerVal == nil {
		return w
	}
	tracer, ok := tracerVal.(*Tracer)
	if !ok {
		return w
	}

	agentSpanVal := session.AgentSpan()
	if agentSpanVal == nil {
		return w
	}
	agentSpan, ok := agentSpanVal.(*TraceAgentSpan)
	if !ok {
		return w
	}

	return &TracedWorkflow{
		inner:        w,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "Workflow"},
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
