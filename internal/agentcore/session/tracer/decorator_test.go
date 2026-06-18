package tracer

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeModelClient 用于测试的模拟模型客户端
type fakeModelClient struct {
	// invokeResult Invoke 返回的结果
	invokeResult *llmschema.AssistantMessage
	// invokeErr Invoke 返回的错误
	invokeErr error
	// generateImageResult GenerateImage 返回的结果
	generateImageResult *llmschema.ImageGenerationResponse
	// generateImageErr GenerateImage 返回的错误
	generateImageErr error
	// streamResult Stream 返回的结果
	streamResult *model_clients.StreamResult
	// streamErr Stream 返回的错误
	streamErr error
}

// fakeTool 用于测试的模拟工具
type fakeTool struct {
	// invokeResult Invoke 返回的结果
	invokeResult map[string]any
	// invokeErr Invoke 返回的错误
	invokeErr error
	// card 工具卡片
	card *tool.ToolCard
}

// fakeAgentSession 用于测试的模拟会话，实现 AgentSessionProvider 接口
// 替代 session/internal 包的真实 AgentSession，避免循环依赖
type fakeAgentSession struct {
	// tracer 追踪器
	tracer *Tracer
	// agentSpan Agent 追踪跨度
	agentSpan *TraceAgentSpan
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// Invoke 实现 model_clients.BaseModelClient 接口
func (f *fakeModelClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return f.invokeResult, f.invokeErr
}

// Stream 实现 model_clients.BaseModelClient 接口
func (f *fakeModelClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (*model_clients.StreamResult, error) {
	return f.streamResult, f.streamErr
}

// GenerateImage 实现 model_clients.BaseModelClient 接口
func (f *fakeModelClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return f.generateImageResult, f.generateImageErr
}

// GenerateSpeech 实现 model_clients.BaseModelClient 接口
func (f *fakeModelClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}

// GenerateVideo 实现 model_clients.BaseModelClient 接口
func (f *fakeModelClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}

// Release 实现 model_clients.BaseModelClient 接口
func (f *fakeModelClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// Invoke 实现 tool.Tool 接口
func (f *fakeTool) Invoke(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return f.invokeResult, f.invokeErr
}

// Stream 实现 tool.Tool 接口
func (f *fakeTool) Stream(_ context.Context, _ map[string]any, _ ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	ch := make(chan tool.StreamChunk)
	close(ch)
	return ch, nil
}

// Card 实现 tool.Tool 接口
func (f *fakeTool) Card() *tool.ToolCard {
	return f.card
}

// Tracer 实现 AgentSessionProvider 接口
func (f *fakeAgentSession) Tracer() *Tracer {
	return f.tracer
}

// AgentSpan 实现 AgentSessionProvider 接口
func (f *fakeAgentSession) AgentSpan() *TraceAgentSpan {
	return f.agentSpan
}

// TestTracedModelClient_Invoke_成功 测试 Invoke 成功时触发 TraceLLMStart 和 TraceLLMEnd 事件
func TestTracedModelClient_Invoke_成功(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	inner := &fakeModelClient{
		invokeResult: &llmschema.AssistantMessage{
			BaseMessage: llmschema.BaseMessage{Content: llmschema.NewTextContent("测试响应")},
		},
	}

	client := &TracedModelClient{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "BaseModelClient"},
	}

	messages := model_clients.NewTextMessagesParam("你好")
	result, err := client.Invoke(context.Background(), messages)

	if err != nil {
		t.Fatalf("期望无错误，实际: %v", err)
	}
	if result.Content.Text() != "测试响应" {
		t.Fatalf("期望 Content=测试响应，实际: %s", result.Content.Text())
	}
}

// TestTracedModelClient_Invoke_失败 测试 Invoke 失败时触发 TraceLLMStart 和 TraceLLMError 事件
func TestTracedModelClient_Invoke_失败(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	inner := &fakeModelClient{
		invokeErr: errors.New("调用失败"),
	}

	client := &TracedModelClient{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "BaseModelClient"},
	}

	messages := model_clients.NewTextMessagesParam("你好")
	result, err := client.Invoke(context.Background(), messages)

	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	if result != nil {
		t.Fatalf("期望 result 为 nil，实际: %v", result)
	}
	if err.Error() != "调用失败" {
		t.Fatalf("期望错误消息 '调用失败'，实际: %s", err.Error())
	}
}

// TestTracedModelClient_GenerateImage_直接委托 测试 GenerateImage 直接委托 inner
func TestTracedModelClient_GenerateImage_直接委托(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	expectedResp := &llmschema.ImageGenerationResponse{}
	inner := &fakeModelClient{
		generateImageResult: expectedResp,
	}

	client := &TracedModelClient{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "BaseModelClient"},
	}

	resp, err := client.GenerateImage(context.Background(), nil)
	if err != nil {
		t.Fatalf("期望无错误，实际: %v", err)
	}
	if resp != expectedResp {
		t.Fatal("期望返回 inner 的 GenerateImage 结果")
	}
}

// TestTracedTool_Invoke_成功 测试工具 Invoke 成功时触发 TracePluginStart 和 TracePluginEnd 事件
func TestTracedTool_Invoke_成功(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	expectedResult := map[string]any{"key": "value"}
	inner := &fakeTool{
		invokeResult: expectedResult,
	}

	tracedTool := &TracedTool{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "Tool"},
	}

	result, err := tracedTool.Invoke(context.Background(), map[string]any{"input": "test"})
	if err != nil {
		t.Fatalf("期望无错误，实际: %v", err)
	}
	if result["key"] != "value" {
		t.Fatalf("期望 key=value，实际: %v", result["key"])
	}
}

// TestTracedTool_Invoke_失败 测试工具 Invoke 失败时触发 TracePluginStart 和 TracePluginError 事件
func TestTracedTool_Invoke_失败(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	inner := &fakeTool{
		invokeErr: errors.New("工具调用失败"),
	}

	tracedTool := &TracedTool{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "Tool"},
	}

	result, err := tracedTool.Invoke(context.Background(), map[string]any{"input": "test"})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	if result != nil {
		t.Fatalf("期望 result 为 nil，实际: %v", result)
	}
	if err.Error() != "工具调用失败" {
		t.Fatalf("期望错误消息 '工具调用失败'，实际: %s", err.Error())
	}
}

// TestTracedTool_Card_委托 测试 Card 方法直接委托 inner
func TestTracedTool_Card_委托(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	expectedCard := tool.NewToolCard("test-tool", "测试工具", []*schema.Param{}, nil)
	inner := &fakeTool{
		card: expectedCard,
	}

	tracedTool := &TracedTool{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "Tool"},
	}

	card := tracedTool.Card()
	if card != expectedCard {
		t.Fatal("期望返回 inner 的 Card 结果")
	}
	if card.Name != "test-tool" {
		t.Fatalf("期望 Name=test-tool，实际: %s", card.Name)
	}
}

// TestDecorateModelWithTrace_有Tracer 测试有 Tracer 时返回 TracedModelClient
func TestDecorateModelWithTrace_有Tracer(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	inner := &fakeModelClient{}
	session := &fakeAgentSession{
		tracer:    tracer,
		agentSpan: agentSpan,
	}

	decorated := DecorateModelWithTrace(inner, session)

	tracedClient, ok := decorated.(*TracedModelClient)
	if !ok {
		t.Fatal("期望返回 *TracedModelClient 类型")
	}
	if tracedClient.inner != inner {
		t.Fatal("期望 TracedModelClient.inner 等于原始 model")
	}
	if tracedClient.tracer != tracer {
		t.Fatal("期望 TracedModelClient.tracer 等于 session 的 tracer")
	}
	if tracedClient.agentSpan != agentSpan {
		t.Fatal("期望 TracedModelClient.agentSpan 等于 session 的 agentSpan")
	}
}

// TestDecorateModelWithTrace_无Tracer 测试无 Tracer 时返回原始 model
func TestDecorateModelWithTrace_无Tracer(t *testing.T) {
	inner := &fakeModelClient{}
	session := &fakeAgentSession{}

	decorated := DecorateModelWithTrace(inner, session)

	if decorated != inner {
		t.Fatal("期望返回原始 model（无 Tracer 时不装饰）")
	}
}

// TestDecorateModelWithTrace_无AgentSpan 测试有 Tracer 但无 AgentSpan 时返回原始 model
func TestDecorateModelWithTrace_无AgentSpan(t *testing.T) {
	tracer := NewTracer()
	inner := &fakeModelClient{}
	session := &fakeAgentSession{
		tracer: tracer,
	}

	decorated := DecorateModelWithTrace(inner, session)

	if decorated != inner {
		t.Fatal("期望返回原始 model（无 AgentSpan 时不装饰）")
	}
}

// TestDecorateToolWithTrace_有Tracer 测试有 Tracer 时返回 TracedTool
func TestDecorateToolWithTrace_有Tracer(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	inner := &fakeTool{}
	session := &fakeAgentSession{
		tracer:    tracer,
		agentSpan: agentSpan,
	}

	decorated := DecorateToolWithTrace(inner, session)

	tracedT, ok := decorated.(*TracedTool)
	if !ok {
		t.Fatal("期望返回 *TracedTool 类型")
	}
	if tracedT.inner != inner {
		t.Fatal("期望 TracedTool.inner 等于原始 tool")
	}
	if tracedT.tracer != tracer {
		t.Fatal("期望 TracedTool.tracer 等于 session 的 tracer")
	}
	if tracedT.agentSpan != agentSpan {
		t.Fatal("期望 TracedTool.agentSpan 等于 session 的 agentSpan")
	}
}

// TestDecorateToolWithTrace_无Tracer 测试无 Tracer 时返回原始 tool
func TestDecorateToolWithTrace_无Tracer(t *testing.T) {
	inner := &fakeTool{}
	session := &fakeAgentSession{}

	decorated := DecorateToolWithTrace(inner, session)

	if decorated != inner {
		t.Fatal("期望返回原始 tool（无 Tracer 时不装饰）")
	}
}

// TestDecorateWorkflowWithTrace_有Tracer 测试有 Tracer 时返回 TracedWorkflow
func TestDecorateWorkflowWithTrace_有Tracer(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	innerWorkflow := "fake_workflow"
	session := &fakeAgentSession{
		tracer:    tracer,
		agentSpan: agentSpan,
	}

	decorated := DecorateWorkflowWithTrace(innerWorkflow, session)

	tracedWf, ok := decorated.(*TracedWorkflow)
	if !ok {
		t.Fatal("期望返回 *TracedWorkflow 类型")
	}
	if tracedWf.inner != innerWorkflow {
		t.Fatal("期望 TracedWorkflow.inner 等于原始 w")
	}
	if tracedWf.tracer != tracer {
		t.Fatal("期望 TracedWorkflow.tracer 等于 session 的 tracer")
	}
}

// TestDecorateWorkflowWithTrace_无Tracer 测试无 Tracer 时返回原始 w
func TestDecorateWorkflowWithTrace_无Tracer(t *testing.T) {
	innerWorkflow := "fake_workflow"
	session := &fakeAgentSession{}

	decorated := DecorateWorkflowWithTrace(innerWorkflow, session)

	if decorated != innerWorkflow {
		t.Fatal("期望返回原始 w（无 Tracer 时不装饰）")
	}
}

// TestTracedModelClient_Release_直接委托 测试 Release 直接委托 inner
func TestTracedModelClient_Release_直接委托(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	inner := &fakeModelClient{}
	client := &TracedModelClient{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "BaseModelClient"},
	}

	released, err := client.Release(context.Background())
	if err != nil {
		t.Fatalf("期望无错误，实际: %v", err)
	}
	if released != false {
		t.Fatalf("期望 released=false，实际: %v", released)
	}
}

// TestTracedTool_Stream_直接委托 测试 Stream 直接委托 inner
func TestTracedTool_Stream_直接委托(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	inner := &fakeTool{}
	tracedTool := &TracedTool{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "Tool"},
	}

	ch, err := tracedTool.Stream(context.Background(), map[string]any{"input": "test"})
	if err != nil {
		t.Fatalf("期望无错误，实际: %v", err)
	}
	// channel 应该已关闭
	_, ok := <-ch
	if ok {
		t.Fatal("期望 channel 已关闭")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
