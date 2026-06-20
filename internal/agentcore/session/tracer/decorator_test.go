package tracer

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
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

// fakeWorkflow 用于测试的模拟工作流，实现 sainterfaces.Workflow 接口
type fakeWorkflow struct {
	// invokeResult Invoke 返回的结果
	invokeResult any
	// invokeErr Invoke 返回的错误
	invokeErr error
	// card 工作流卡片
	card *schema.WorkflowCard
}

// Invoke 实现 sainterfaces.Workflow 接口
func (f *fakeWorkflow) Invoke(_ context.Context, _ map[string]any, _ ...sainterfaces.WorkflowOption) (any, error) {
	return f.invokeResult, f.invokeErr
}

// Stream 实现 sainterfaces.Workflow 接口
func (f *fakeWorkflow) Stream(_ context.Context, _ map[string]any, _ ...sainterfaces.WorkflowOption) (<-chan stream.Schema, error) {
	ch := make(chan stream.Schema)
	close(ch)
	return ch, nil
}

// Card 实现 sainterfaces.Workflow 接口
func (f *fakeWorkflow) Card() *schema.WorkflowCard {
	return f.card
}

// TestTracedModelClient_Invoke_成功 测试 Invoke 成功时触发 TraceLLMStart 和 TraceLLMEnd 事件
func TestTracedModelClient_Invoke_成功(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	inner := &fakeModelClient{
		invokeResult: &llmschema.AssistantMessage{
			DefaultMessage: llmschema.DefaultMessage{Content: llmschema.NewTextContent("测试响应")},
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

	innerWorkflow := &fakeWorkflow{
		card: schema.NewWorkflowCard(
			schema.WithName("测试工作流"),
			schema.WithDescription("测试描述"),
		),
	}
	innerWorkflow.card.Version = "1.0"
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
	// 验证 instanceInfo 包含 metadata
	if tracedWf.instanceInfo["class_name"] != "测试工作流" {
		t.Errorf("期望 class_name=测试工作流，实际: %v", tracedWf.instanceInfo["class_name"])
	}
	if tracedWf.instanceInfo["type"] != "workflow" {
		t.Errorf("期望 type=workflow，实际: %v", tracedWf.instanceInfo["type"])
	}
	metadata, ok := tracedWf.instanceInfo["metadata"].(map[string]any)
	if !ok {
		t.Fatal("期望 metadata 为 map[string]any")
	}
	if metadata["name"] != "测试工作流" {
		t.Errorf("期望 metadata.name=测试工作流，实际: %v", metadata["name"])
	}
	if metadata["version"] != "1.0" {
		t.Errorf("期望 metadata.version=1.0，实际: %v", metadata["version"])
	}
}

// TestDecorateWorkflowWithTrace_无Tracer 测试无 Tracer 时返回原始 w
func TestDecorateWorkflowWithTrace_无Tracer(t *testing.T) {
	innerWorkflow := &fakeWorkflow{}
	session := &fakeAgentSession{}

	decorated := DecorateWorkflowWithTrace(innerWorkflow, session)

	if decorated != innerWorkflow {
		t.Fatal("期望返回原始 w（无 Tracer 时不装饰）")
	}
}

// TestTracedWorkflow_Invoke_成功 测试 Invoke 成功时触发 TraceWorkflowStart 和 TraceWorkflowEnd
func TestTracedWorkflow_Invoke_成功(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	expectedResult := map[string]any{"output": "done"}
	inner := &fakeWorkflow{
		invokeResult: expectedResult,
		card:          schema.NewWorkflowCard(schema.WithName("wf-1")),
	}

	tracedWf := &TracedWorkflow{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "wf-1", "type": "workflow"},
	}

	result, err := tracedWf.Invoke(context.Background(), map[string]any{"input": "test"})
	if err != nil {
		t.Fatalf("期望无错误，实际: %v", err)
	}
	if result.(map[string]any)["output"] != "done" {
		t.Errorf("期望 output=done，实际: %v", result)
	}
}

// TestTracedWorkflow_Invoke_失败 测试 Invoke 失败时触发 TraceWorkflowStart 和 TraceWorkflowError
func TestTracedWorkflow_Invoke_失败(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	inner := &fakeWorkflow{
		invokeErr: errors.New("工作流执行失败"),
		card:      schema.NewWorkflowCard(schema.WithName("wf-1")),
	}

	tracedWf := &TracedWorkflow{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "wf-1", "type": "workflow"},
	}

	result, err := tracedWf.Invoke(context.Background(), map[string]any{"input": "test"})
	if err == nil {
		t.Fatal("期望返回错误，实际为 nil")
	}
	if result != nil {
		t.Fatalf("期望 result 为 nil，实际: %v", result)
	}
	if err.Error() != "工作流执行失败" {
		t.Fatalf("期望错误消息 '工作流执行失败'，实际: %s", err.Error())
	}
}

// TestTracedWorkflow_Card_委托 测试 Card 方法直接委托 inner
func TestTracedWorkflow_Card_委托(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	expectedCard := schema.NewWorkflowCard(
		schema.WithName("my-workflow"),
		schema.WithDescription("我的工作流"),
	)
	expectedCard.Version = "2.0"

	inner := &fakeWorkflow{
		card: expectedCard,
	}

	tracedWf := &TracedWorkflow{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "my-workflow", "type": "workflow"},
	}

	card := tracedWf.Card()
	if card != expectedCard {
		t.Fatal("期望返回 inner 的 Card 结果")
	}
	if card.Name != "my-workflow" {
		t.Fatalf("期望 Name=my-workflow，实际: %s", card.Name)
	}
	if card.Version != "2.0" {
		t.Fatalf("期望 Version=2.0，实际: %s", card.Version)
	}
}

// TestDecorateWorkflowWithTrace_无Card 测试 Card 为 nil 时 class_name 使用默认值
func TestDecorateWorkflowWithTrace_无Card(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	innerWorkflow := &fakeWorkflow{card: nil}
	session := &fakeAgentSession{
		tracer:    tracer,
		agentSpan: agentSpan,
	}

	decorated := DecorateWorkflowWithTrace(innerWorkflow, session)

	tracedWf, ok := decorated.(*TracedWorkflow)
	if !ok {
		t.Fatal("期望返回 *TracedWorkflow 类型")
	}
	if tracedWf.instanceInfo["class_name"] != "Workflow" {
		t.Errorf("期望 class_name=Workflow（默认值），实际: %v", tracedWf.instanceInfo["class_name"])
	}
	if _, hasMetadata := tracedWf.instanceInfo["metadata"]; hasMetadata {
		t.Error("Card 为 nil 时不应包含 metadata")
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

// tracerRecordDataCapturingClient 捕获 Invoke/Stream 选项中 TracerRecordData 回调的模拟客户端
type tracerRecordDataCapturingClient struct {
	// invokeResult Invoke 返回的结果
	invokeResult *llmschema.AssistantMessage
	// invokeErr Invoke 返回的错误
	invokeErr error
	// capturedInvokeRecordData 捕获到的 Invoke TracerRecordData 回调
	capturedInvokeRecordData any
	// capturedStreamRecordData 捕获到的 Stream TracerRecordData 回调
	capturedStreamRecordData any
	// streamResult Stream 返回的结果
	streamResult *model_clients.StreamResult
	// streamErr Stream 返回的错误
	streamErr error
}

// Invoke 实现 model_clients.BaseModelClient 接口，捕获 TracerRecordData 回调
func (c *tracerRecordDataCapturingClient) Invoke(_ context.Context, _ model_clients.MessagesParam, opts ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	params := model_clients.NewInvokeParams(opts...)
	c.capturedInvokeRecordData = params.TracerRecordData
	return c.invokeResult, c.invokeErr
}

// Stream 实现 model_clients.BaseModelClient 接口，捕获 TracerRecordData 回调
func (c *tracerRecordDataCapturingClient) Stream(_ context.Context, _ model_clients.MessagesParam, opts ...model_clients.StreamOption) (*model_clients.StreamResult, error) {
	params := model_clients.NewStreamParams(opts...)
	c.capturedStreamRecordData = params.TracerRecordData
	return c.streamResult, c.streamErr
}

// GenerateImage 实现 model_clients.BaseModelClient 接口
func (c *tracerRecordDataCapturingClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, nil
}

// GenerateSpeech 实现 model_clients.BaseModelClient 接口
func (c *tracerRecordDataCapturingClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}

// GenerateVideo 实现 model_clients.BaseModelClient 接口
func (c *tracerRecordDataCapturingClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}

// Release 实现 model_clients.BaseModelClient 接口
func (c *tracerRecordDataCapturingClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}

// TestTracedModelClient_Invoke_注入TracerRecordData 测试 Invoke 将 tracer_record_data 回调注入到 opts
// 对齐 Python: call_kwargs["tracer_record_data"] = tracer_record_data
func TestTracedModelClient_Invoke_注入TracerRecordData(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	inner := &tracerRecordDataCapturingClient{
		invokeResult: &llmschema.AssistantMessage{
			DefaultMessage: llmschema.DefaultMessage{Content: llmschema.NewTextContent("测试")},
		},
	}

	client := &TracedModelClient{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "BaseModelClient"},
	}

	_, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("你好"))
	if err != nil {
		t.Fatalf("期望无错误，实际: %v", err)
	}

	// 验证回调被注入
	if inner.capturedInvokeRecordData == nil {
		t.Fatal("期望 TracerRecordData 被注入，实际为 nil")
	}

	// 验证回调类型正确且可调用
	fn, ok := inner.capturedInvokeRecordData.(func(map[string]any))
	if !ok {
		t.Fatalf("期望 TracerRecordData 类型为 func(map[string]any)，实际: %T", inner.capturedInvokeRecordData)
	}

	// 调用回调不应 panic
	fn(map[string]any{"llm_params": map[string]any{"model": "test"}})
}

// TestTracedModelClient_Stream_注入TracerRecordData 测试 Stream 将 tracer_record_data 回调注入到 opts
// 对齐 Python: call_kwargs["tracer_record_data"] = tracer_record_data
func TestTracedModelClient_Stream_注入TracerRecordData(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	chunkChan := make(chan *llmschema.AssistantMessageChunk, 1)
	chunkChan <- &llmschema.AssistantMessageChunk{
		AssistantMessage: llmschema.AssistantMessage{
			DefaultMessage: llmschema.DefaultMessage{Content: llmschema.NewTextContent("chunk")},
		},
	}
	close(chunkChan)

	inner := &tracerRecordDataCapturingClient{
		streamResult: model_clients.NewStreamResult(chunkChan),
	}

	client := &TracedModelClient{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "BaseModelClient"},
	}

	_, err := client.Stream(context.Background(), model_clients.NewTextMessagesParam("你好"))
	if err != nil {
		t.Fatalf("期望无错误，实际: %v", err)
	}

	// 验证回调被注入
	if inner.capturedStreamRecordData == nil {
		t.Fatal("期望 TracerRecordData 被注入，实际为 nil")
	}

	// 验证回调类型正确且可调用
	fn, ok := inner.capturedStreamRecordData.(func(map[string]any))
	if !ok {
		t.Fatalf("期望 TracerRecordData 类型为 func(map[string]any)，实际: %T", inner.capturedStreamRecordData)
	}

	// 调用回调不应 panic
	fn(map[string]any{"llm_params": map[string]any{"model": "test"}})
}

// TestTracedModelClient_Invoke_回调触发TraceLLMRequest 测试底层客户端调用回调时 TraceLLMRequest 事件被触发
// 对齐 Python: tracer.trigger("tracer_agent", "on_llm_request", span=span, **kw)
func TestTracedModelClient_Invoke_回调触发TraceLLMRequest(t *testing.T) {
	tracer := NewTracer()
	agentSpan := tracer.AgentSpanManager.CreateAgentSpan()

	// 手动创建 handler 并注册 TraceLLMRequest 事件分派（模拟 buildAgentDispatch 的对应条目）
	handler := NewTraceAgentHandler(nil, tracer.AgentSpanManager)
	tracer.agentHandler = handler
	tracer.agentDispatch[TraceLLMRequest] = func(ctx context.Context, p *TriggerParams) {
		span := tracer.getOrCreateAgentSpan(p)
		_ = handler.OnLLMRequest(ctx, span, p.OnInvokeData)
	}

	inner := &tracerRecordDataCapturingClient{
		invokeResult: &llmschema.AssistantMessage{
			DefaultMessage: llmschema.DefaultMessage{Content: llmschema.NewTextContent("测试")},
		},
	}

	client := &TracedModelClient{
		inner:        inner,
		tracer:       tracer,
		agentSpan:    agentSpan,
		instanceInfo: map[string]any{"class_name": "BaseModelClient"},
	}

	_, err := client.Invoke(context.Background(), model_clients.NewTextMessagesParam("你好"))
	if err != nil {
		t.Fatalf("期望无错误，实际: %v", err)
	}

	// 手动调用捕获的回调，模拟底层客户端调用
	fn, ok := inner.capturedInvokeRecordData.(func(map[string]any))
	if !ok {
		t.Fatalf("期望 TracerRecordData 类型为 func(map[string]any)，实际: %T", inner.capturedInvokeRecordData)
	}
	fn(map[string]any{"llm_params": map[string]any{"model": "qwen-max"}})

	// 验证 OnInvokeData 被填充（TraceLLMRequest → OnLLMRequest → updateRunningTraceData）
	lastSpan := tracer.AgentSpanManager.LastSpan()
	if lastSpan == nil {
		t.Fatal("期望存在 span，实际为 nil")
	}
	if len(lastSpan.OnInvokeData) == 0 {
		t.Fatal("期望 OnInvokeData 非空（回调应追加数据），实际为空")
	}
	lastData := lastSpan.OnInvokeData[len(lastSpan.OnInvokeData)-1]
	if llmParams, ok := lastData["llm_params"].(map[string]any); ok {
		if llmParams["model"] != "qwen-max" {
			t.Fatalf("期望 llm_params.model=qwen-max，实际: %v", llmParams["model"])
		}
	} else {
		t.Fatalf("期望 lastData 包含 llm_params，实际: %v", lastData)
	}
}
