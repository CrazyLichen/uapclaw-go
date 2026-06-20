package tracer

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockStreamWriter 模拟流写入器，记录所有 Write 调用参数
type mockStreamWriter struct {
	// writes 记录写入的数据
	writes []stream.TraceSchema
	// writeErr 模拟写入错误
	writeErr error
}

// testGraphInterrupt 测试用的图中断信号，满足 graphInterrupter 接口
type testGraphInterrupt struct{}

func (*testGraphInterrupt) isGraphInterrupt() {}

// ──────────────────────────── 导出函数 ────────────────────────────

// Write 实现 StreamWriter 接口，记录写入参数
func (m *mockStreamWriter) Write(_ context.Context, schema stream.Schema) error {
	if m.writeErr != nil {
		return m.writeErr
	}
	if ts, ok := schema.(stream.TraceSchema); ok {
		m.writes = append(m.writes, ts)
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newTestAgentHandler 创建测试用 Agent 追踪处理器
func newTestAgentHandler() (*TraceAgentHandler, *mockStreamWriter) {
	sw := &mockStreamWriter{}
	sm := NewSpanManager("test-trace-id")
	h := NewTraceAgentHandler(sw, sm)
	return h, sw
}

// newTestWorkflowHandler 创建测试用工作流追踪处理器
func newTestWorkflowHandler() (*TraceWorkflowHandler, *mockStreamWriter) {
	sw := &mockStreamWriter{}
	sm := NewSpanManager("test-trace-id")
	h := NewTraceWorkflowHandler(sw, sm)
	return h, sw
}

// TestTraceAgentHandler_OnLLMStart 测试 LLM 调用开始
func TestTraceAgentHandler_OnLLMStart(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	instanceInfo := map[string]any{
		"class_name": "TestLLM",
		"model":      "qwen-max",
	}
	inputs := map[string]any{"prompt": "hello"}

	err := h.OnLLMStart(context.Background(), span, inputs, instanceInfo)
	if err != nil {
		t.Fatalf("OnLLMStart 返回错误: %v", err)
	}

	// 验证 span 字段
	if span.InvokeType != string(InvokeTypeLLM) {
		t.Errorf("InvokeType = %q, want %q", span.InvokeType, InvokeTypeLLM)
	}
	if span.Name != "TestLLM" {
		t.Errorf("Name = %q, want %q", span.Name, "TestLLM")
	}
	if span.StartTime == nil {
		t.Error("StartTime 不应为 nil")
	}
	if span.Inputs == nil {
		t.Error("Inputs 不应为 nil")
	}

	// 验证 streamWriter 写入
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
	if sw.writes[0].Type != "tracer_agent" {
		t.Errorf("写入类型 = %q, want %q", sw.writes[0].Type, "tracer_agent")
	}
}

// TestTraceAgentHandler_OnLLMRequest 测试 LLM 请求详情
func TestTraceAgentHandler_OnLLMRequest(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	// 先设置 StartTime
	span.StartTime = &time.Time{}
	span.InvokeType = string(InvokeTypeLLM)

	data := map[string]any{"token_usage": 100, "model": "qwen-max"}
	err := h.OnLLMRequest(context.Background(), span, data)
	if err != nil {
		t.Fatalf("OnLLMRequest 返回错误: %v", err)
	}

	// 验证 OnInvokeData 追加
	if len(span.OnInvokeData) != 1 {
		t.Fatalf("OnInvokeData 长度 = %d, want 1", len(span.OnInvokeData))
	}
	if span.OnInvokeData[0]["token_usage"] != 100 {
		t.Errorf("OnInvokeData[0][token_usage] = %v, want 100", span.OnInvokeData[0]["token_usage"])
	}

	// 验证 streamWriter 写入
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnLLMEnd 测试 LLM 调用结束
func TestTraceAgentHandler_OnLLMEnd(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	// 先设置 StartTime
	now := time.Now()
	span.StartTime = &now
	span.InvokeType = string(InvokeTypeLLM)

	outputs := map[string]any{"result": "world"}
	err := h.OnLLMEnd(context.Background(), span, outputs)
	if err != nil {
		t.Fatalf("OnLLMEnd 返回错误: %v", err)
	}

	// 验证 span 字段
	if span.EndTime == nil {
		t.Error("EndTime 不应为 nil")
	}
	if span.Outputs == nil {
		t.Error("Outputs 不应为 nil")
	}
	if span.ElapsedTime == "" {
		t.Error("ElapsedTime 不应为空")
	}

	// 验证 streamWriter 写入
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnLLMError 测试 LLM 调用错误
func TestTraceAgentHandler_OnLLMError(t *testing.T) {
	h, _ := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	// 先设置 StartTime
	now := time.Now()
	span.StartTime = &now

	testErr := fmt.Errorf("LLM 调用失败")
	err := h.OnLLMError(context.Background(), span, testErr)
	if err != nil {
		t.Fatalf("OnLLMError 返回错误: %v", err)
	}

	// 验证 span 字段
	if span.EndTime == nil {
		t.Error("EndTime 不应为 nil")
	}
	if span.Error == nil {
		t.Error("Error 不应为 nil")
	}
	if span.ElapsedTime == "" {
		t.Error("ElapsedTime 不应为空")
	}
	// 普通错误使用 WORKFLOW_EXECUTION_ERROR 码
	if span.Error["error_code"] != exception.StatusWorkflowExecutionError.Code() {
		t.Errorf("error_code = %v, want %d", span.Error["error_code"], exception.StatusWorkflowExecutionError.Code())
	}
}

// TestTraceAgentHandler_OnPluginStart 测试插件调用开始
func TestTraceAgentHandler_OnPluginStart(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	instanceInfo := map[string]any{
		"class_name": "TestPlugin",
	}
	inputs := map[string]any{"param": "value"}

	err := h.OnPluginStart(context.Background(), span, inputs, instanceInfo)
	if err != nil {
		t.Fatalf("OnPluginStart 返回错误: %v", err)
	}

	if span.InvokeType != string(InvokeTypePlugin) {
		t.Errorf("InvokeType = %q, want %q", span.InvokeType, InvokeTypePlugin)
	}
	if span.Name != "TestPlugin" {
		t.Errorf("Name = %q, want %q", span.Name, "TestPlugin")
	}

	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_updateStartTraceData_Marshal失败 测试元数据序列化失败
func TestTraceAgentHandler_updateStartTraceData_Marshal失败(t *testing.T) {
	h, _ := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	// 创建包含不可序列化值的 instanceInfo
	instanceInfo := map[string]any{
		"class_name": "TestClass",
		"ch":         make(chan int), // chan 不可被 json.Marshal
	}

	h.updateStartTraceData(span, string(InvokeTypeLLM), "inputs", instanceInfo)

	// 序列化失败后应直接返回，不应设置任何字段
	if span.InvokeType != "" {
		t.Errorf("序列化失败后 InvokeType 应为空，实际 = %q", span.InvokeType)
	}
	if span.StartTime != nil {
		t.Error("序列化失败后 StartTime 应为 nil")
	}
}

// TestGetElapsedTime_毫秒 测试耗时计算（毫秒级）
func TestGetElapsedTime_毫秒(t *testing.T) {
	h, _ := newTestAgentHandler()

	start := time.Now()
	end := start.Add(500 * time.Millisecond)

	result := h.GetElapsedTime(start, end)
	if result != "500ms" {
		t.Errorf("GetElapsedTime = %q, want %q", result, "500ms")
	}
}

// TestGetElapsedTime_秒 测试耗时计算（秒级）
func TestGetElapsedTime_秒(t *testing.T) {
	h, _ := newTestAgentHandler()

	start := time.Now()
	end := start.Add(2500 * time.Millisecond)

	result := h.GetElapsedTime(start, end)
	if result != "2.50s" {
		t.Errorf("GetElapsedTime = %q, want %q", result, "2.50s")
	}
}

// TestGetNodeStatus_各状态 测试节点状态判断
func TestGetNodeStatus_各状态(t *testing.T) {
	h, _ := newTestAgentHandler()

	tests := []struct {
		name string
		span Span
		want NodeStatus
	}{
		{
			name: "有错误时返回 ERROR",
			span: Span{Error: map[string]any{"error_code": 100, "message": "err"}},
			want: NodeStatusError,
		},
		{
			name: "有 OnInvokeData 且无 EndTime 返回 RUNNING",
			span: Span{OnInvokeData: []map[string]any{{"key": "val"}}},
			want: NodeStatusRunning,
		},
		{
			name: "有 OnInvokeData 且有 EndTime 返回 FINISH",
			span: Span{
				OnInvokeData: []map[string]any{{"key": "val"}},
				EndTime:      &time.Time{},
			},
			want: NodeStatusFinish,
		},
		{
			name: "无 OnInvokeData 有 EndTime 返回 FINISH",
			span: Span{EndTime: &time.Time{}},
			want: NodeStatusFinish,
		},
		{
			name: "无任何数据返回 START",
			span: Span{},
			want: NodeStatusStart,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := h.GetNodeStatus(&tt.span)
			if got != tt.want {
				t.Errorf("GetNodeStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestTraceAgentHandler_FormatData 测试 Agent 追踪数据格式化
func TestTraceAgentHandler_FormatData(t *testing.T) {
	h, _ := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	// 设置 span 字段
	span.InvokeType = string(InvokeTypeLLM)
	span.Name = "TestLLM"

	// 注册到 handler 的缓存中
	h.agentSpans[span.InvokeID] = span

	data := h.FormatData(&span.Span)

	if data["type"] != "tracer_agent" {
		t.Errorf("type = %v, want %q", data["type"], "tracer_agent")
	}
	payload, ok := data["payload"].(*TraceAgentSpan)
	if !ok {
		t.Fatalf("payload 类型错误，期望 *TraceAgentSpan")
	}
	if payload.Name != "TestLLM" {
		t.Errorf("payload.Name = %q, want %q", payload.Name, "TestLLM")
	}
}

// TestTraceWorkflowHandler_OnCallStart 测试组件调用开始
func TestTraceWorkflowHandler_OnCallStart(t *testing.T) {
	h, sw := newTestWorkflowHandler()

	metadata := map[string]any{
		"ComponentID":   "comp-1",
		"ComponentName": "LLM组件",
		"ComponentType": "LLM",
	}
	inputs := map[string]any{"prompt": "hello"}
	sourceIDs := []string{"src-1"}

	// needSend=true 时应写入流
	err := h.OnCallStart(context.Background(), "invoke-1", metadata, inputs, true, sourceIDs)
	if err != nil {
		t.Fatalf("OnCallStart 返回错误: %v", err)
	}

	if len(sw.writes) != 1 {
		t.Fatalf("needSend=true 时写入次数 = %d, want 1", len(sw.writes))
	}

	// 验证 span 字段
	span := h.workflowSpans["invoke-1"]
	if span == nil {
		t.Fatal("workflowSpans[invoke-1] 不应为 nil")
	}
	if span.StartTime == nil {
		t.Error("StartTime 不应为 nil")
	}
	if span.ComponentID != "comp-1" {
		t.Errorf("ComponentID = %q, want %q", span.ComponentID, "comp-1")
	}
	if span.ComponentType != "LLM" {
		t.Errorf("ComponentType = %q, want %q", span.ComponentType, "LLM")
	}

	// needSend=false 时不应写入流
	sw2 := &mockStreamWriter{}
	h2 := NewTraceWorkflowHandler(sw2, NewSpanManager("trace-2"))
	err = h2.OnCallStart(context.Background(), "invoke-2", metadata, inputs, false, nil)
	if err != nil {
		t.Fatalf("OnCallStart 返回错误: %v", err)
	}
	if len(sw2.writes) != 0 {
		t.Errorf("needSend=false 时写入次数 = %d, want 0", len(sw2.writes))
	}
}

// TestTraceWorkflowHandler_OnPreInvoke 测试组件预调用
func TestTraceWorkflowHandler_OnPreInvoke(t *testing.T) {
	h, sw := newTestWorkflowHandler()

	// 先创建 span
	_ = h.getTracerWorkflowSpan("invoke-1")

	inputs := map[string]any{"prompt": "updated"}
	componentMetadata := map[string]any{
		"ComponentName": "新组件",
	}

	err := h.OnPreInvoke(context.Background(), "invoke-1", inputs, componentMetadata, true)
	if err != nil {
		t.Fatalf("OnPreInvoke 返回错误: %v", err)
	}

	span := h.workflowSpans["invoke-1"]
	if span.Inputs == nil {
		t.Error("Inputs 不应为 nil")
	}
	if span.ComponentName != "新组件" {
		t.Errorf("ComponentName = %q, want %q", span.ComponentName, "新组件")
	}

	// needSend=true 时应写入流
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceWorkflowHandler_OnInvoke_正常 测试组件正常调用
func TestTraceWorkflowHandler_OnInvoke_正常(t *testing.T) {
	h, sw := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	onInvokeData := map[string]any{
		"token_usage": 50,
	}
	err := h.OnInvoke(context.Background(), "invoke-1", onInvokeData, nil)
	if err != nil {
		t.Fatalf("OnInvoke 返回错误: %v", err)
	}

	span := h.workflowSpans["invoke-1"]
	if len(span.OnInvokeData) != 1 {
		t.Fatalf("OnInvokeData 长度 = %d, want 1", len(span.OnInvokeData))
	}
	if span.OnInvokeData[0]["token_usage"] != 50 {
		t.Errorf("OnInvokeData[0][token_usage] = %v, want 50", span.OnInvokeData[0]["token_usage"])
	}
	// 正常调用不应设置 EndTime
	if span.EndTime != nil {
		t.Error("正常调用 EndTime 应为 nil")
	}

	// 验证 streamWriter 写入
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceWorkflowHandler_OnInvoke_异常 测试组件调用异常
func TestTraceWorkflowHandler_OnInvoke_异常(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name       string
		exc        any
		wantCode   int
		wantStatus NodeStatus
	}{
		{
			name:       "BaseError",
			exc:        exception.NewBaseError(exception.StatusWorkflowExecutionError, exception.WithMsg("执行失败")),
			wantCode:   exception.StatusWorkflowExecutionError.Code(),
			wantStatus: "",
		},
		{
			name:       "GraphInterrupt",
			exc:        &testGraphInterrupt{},
			wantCode:   0, // 不设置 Error
			wantStatus: NodeStatusInterrupted,
		},
		{
			name:       "普通错误",
			exc:        fmt.Errorf("普通错误"),
			wantCode:   exception.StatusWorkflowExecutionError.Code(),
			wantStatus: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sw := &mockStreamWriter{}
			h := NewTraceWorkflowHandler(sw, NewSpanManager("trace-"+tt.name))
			span := h.getTracerWorkflowSpan("invoke-err")
			span.StartTime = &now

			err := h.OnInvoke(context.Background(), "invoke-err", nil, tt.exc)
			if err != nil {
				t.Fatalf("OnInvoke 返回错误: %v", err)
			}

			if tt.wantCode > 0 {
				if span.Error == nil {
					t.Fatal("Error 不应为 nil")
				}
				if span.Error["error_code"] != tt.wantCode {
					t.Errorf("error_code = %v, want %d", span.Error["error_code"], tt.wantCode)
				}
			}
			if tt.wantStatus != "" {
				if span.Status != string(tt.wantStatus) {
					t.Errorf("Status = %q, want %q", span.Status, tt.wantStatus)
				}
			}
			if span.EndTime == nil {
				t.Error("异常时 EndTime 不应为 nil")
			}
		})
	}
}

// TestTraceWorkflowHandler_OnPostInvoke 测试组件后调用
func TestTraceWorkflowHandler_OnPostInvoke(t *testing.T) {
	h, _ := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	outputs := map[string]any{"result": "ok"}
	err := h.OnPostInvoke(context.Background(), "invoke-1", outputs, nil)
	if err != nil {
		t.Fatalf("OnPostInvoke 返回错误: %v", err)
	}

	span := h.workflowSpans["invoke-1"]
	if span.Outputs == nil {
		t.Error("Outputs 不应为 nil")
	}
}

// TestTraceWorkflowHandler_OnPostStream 测试组件后流式
func TestTraceWorkflowHandler_OnPostStream(t *testing.T) {
	h, _ := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	chunk := map[string]any{"text": "hello"}
	err := h.OnPostStream(context.Background(), "invoke-1", chunk)
	if err != nil {
		t.Fatalf("OnPostStream 返回错误: %v", err)
	}

	span := h.workflowSpans["invoke-1"]
	if len(span.StreamOutputs) != 1 {
		t.Fatalf("StreamOutputs 长度 = %d, want 1", len(span.StreamOutputs))
	}
}

// TestTraceWorkflowHandler_OnCallDone 测试组件调用完成
func TestTraceWorkflowHandler_OnCallDone(t *testing.T) {
	h, sw := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")
	span := h.workflowSpans["invoke-1"]
	now := time.Now()
	span.StartTime = &now

	outputs := map[string]any{"result": "done"}
	err := h.OnCallDone(context.Background(), "invoke-1", outputs)
	if err != nil {
		t.Fatalf("OnCallDone 返回错误: %v", err)
	}

	if span.EndTime == nil {
		t.Error("EndTime 不应为 nil")
	}
	if span.Outputs == nil {
		t.Error("Outputs 不应为 nil")
	}

	// 验证 streamWriter 写入
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceWorkflowHandler_OnInteract 测试组件交互
func TestTraceWorkflowHandler_OnInteract(t *testing.T) {
	h, sw := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	inputs := map[string]any{"user_input": "继续"}
	componentMetadata := map[string]any{
		"ComponentType": "Interaction",
	}

	err := h.OnInteract(context.Background(), "invoke-1", inputs, componentMetadata, true)
	if err != nil {
		t.Fatalf("OnInteract 返回错误: %v", err)
	}

	span := h.workflowSpans["invoke-1"]
	if span.InteractiveInputs == nil {
		t.Error("InteractiveInputs 不应为 nil")
	}
	if span.ComponentType != "Interaction" {
		t.Errorf("ComponentType = %q, want %q", span.ComponentType, "Interaction")
	}

	// needSend=true 时应写入流
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceWorkflowHandler_FormatData 测试工作流追踪数据格式化
func TestTraceWorkflowHandler_FormatData(t *testing.T) {
	h, _ := newTestWorkflowHandler()

	span := h.getTracerWorkflowSpan("invoke-1")
	span.ComponentID = "comp-1"
	span.ComponentName = "LLM"
	span.ComponentType = "LLM"
	span.WorkflowID = "wf-1"

	data := h.FormatData(&span.Span)

	if data["type"] != "tracer_workflow" {
		t.Errorf("type = %v, want %q", data["type"], "tracer_workflow")
	}
	payload, ok := data["payload"].(map[string]any)
	if !ok {
		t.Fatalf("payload 类型错误，期望 map[string]any")
	}
	if payload["componentId"] != "comp-1" {
		t.Errorf("payload[componentId] = %v, want %q", payload["componentId"], "comp-1")
	}
	if payload["workflowId"] != "wf-1" {
		t.Errorf("payload[workflowId] = %v, want %q", payload["workflowId"], "wf-1")
	}
	// 验证排除字段
	if _, exists := payload["childInvokes"]; exists {
		t.Error("payload 不应包含 childInvokes")
	}
	if _, exists := payload["llmInvokeData"]; exists {
		t.Error("payload 不应包含 llmInvokeData")
	}
}

// TestTraceAgentHandler_OnLLMError_BaseError 测试 LLM 调用 BaseError 错误
func TestTraceAgentHandler_OnLLMError_BaseError(t *testing.T) {
	h, _ := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	now := time.Now()
	span.StartTime = &now

	baseErr := exception.NewBaseError(exception.StatusWorkflowExecutionError, exception.WithMsg("执行失败"))
	err := h.OnLLMError(context.Background(), span, baseErr)
	if err != nil {
		t.Fatalf("OnLLMError 返回错误: %v", err)
	}

	if span.Error == nil {
		t.Fatal("Error 不应为 nil")
	}
	if span.Error["error_code"] != exception.StatusWorkflowExecutionError.Code() {
		t.Errorf("error_code = %v, want %d", span.Error["error_code"], exception.StatusWorkflowExecutionError.Code())
	}
	if span.Error["message"] != "执行失败" {
		t.Errorf("message = %v, want %q", span.Error["message"], "执行失败")
	}
}

// TestTraceWorkflowHandler_OnInvoke_内部错误 测试组件调用 inner_error
func TestTraceWorkflowHandler_OnInvoke_内部错误(t *testing.T) {
	h, _ := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	onInvokeData := map[string]any{
		"inner_error": map[string]any{"code": 500, "msg": "内部错误"},
		"retry_count": 1,
	}
	err := h.OnInvoke(context.Background(), "invoke-1", onInvokeData, nil)
	if err != nil {
		t.Fatalf("OnInvoke 返回错误: %v", err)
	}

	span := h.workflowSpans["invoke-1"]
	if span.InnerError == nil {
		t.Fatal("InnerError 不应为 nil")
	}
	if span.InnerError["code"] != 500 {
		t.Errorf("InnerError[code] = %v, want 500", span.InnerError["code"])
	}
}

// TestBuildWorkflowPayload_排除字段 测试构建工作流 payload 排除字段
func TestBuildWorkflowPayload_排除字段(t *testing.T) {
	span := &TraceWorkflowSpan{
		Span: Span{
			TraceID:  "trace-1",
			InvokeID: "invoke-1",
			Inputs:   "inputs",
			Outputs:  "outputs",
		},
		ComponentID:   "comp-1",
		StreamOutputs: []any{"chunk1"},
	}

	// 排除 outputs 和 streamOutputs
	exclude := map[string]bool{"outputs": true, "streamOutputs": true}
	result := buildWorkflowPayloadWithExclude(span, exclude)

	if _, exists := result["outputs"]; exists {
		t.Error("排除后不应包含 outputs")
	}
	if _, exists := result["streamOutputs"]; exists {
		t.Error("排除后不应包含 streamOutputs")
	}
	// 未排除的字段应存在
	if result["componentId"] != "comp-1" {
		t.Errorf("componentId = %v, want %q", result["componentId"], "comp-1")
	}
}

// TestEmitStreamWriter_写入失败 测试流写入失败
func TestEmitStreamWriter_写入失败(t *testing.T) {
	sw := &mockStreamWriter{writeErr: fmt.Errorf("写入失败")}
	sm := NewSpanManager("test-trace-id")
	h := NewTraceAgentHandler(sw, sm)
	span := sm.CreateAgentSpan()

	err := h.EmitStreamWriter(context.Background(), &span.Span)
	if err == nil {
		t.Error("期望返回写入错误，但返回 nil")
	}
}

// TestEmitStreamWriter_StreamWriter为nil 测试 StreamWriter 为 nil 时不报错
func TestEmitStreamWriter_StreamWriter为nil(t *testing.T) {
	sm := NewSpanManager("test-trace-id")
	h := NewTraceAgentHandler(nil, sm)
	span := sm.CreateAgentSpan()

	err := h.EmitStreamWriter(context.Background(), &span.Span)
	if err != nil {
		t.Errorf("StreamWriter 为 nil 时不应返回错误，实际: %v", err)
	}
}

// TestTraceWorkflowHandler_OnPreStream 测试组件预流式
func TestTraceWorkflowHandler_OnPreStream(t *testing.T) {
	h, sw := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	chunk := map[string]any{"text": "streaming"}
	err := h.OnPreStream(context.Background(), "invoke-1", chunk, true)
	if err != nil {
		t.Fatalf("OnPreStream 返回错误: %v", err)
	}

	span := h.workflowSpans["invoke-1"]
	if len(span.StreamInputs) != 1 {
		t.Fatalf("StreamInputs 长度 = %d, want 1", len(span.StreamInputs))
	}

	// needSend=true 时应写入流
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceWorkflowHandler_OnPreStream_空Dict不追加 测试空 dict 不追加到 StreamInputs
// 对齐 Python: if chunk and isinstance(chunk, dict) — 空 dict 为 falsy，不追加
func TestTraceWorkflowHandler_OnPreStream_空Dict不追加(t *testing.T) {
	h, _ := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	// 空 dict 不应追加
	err := h.OnPreStream(context.Background(), "invoke-1", map[string]any{}, true)
	if err != nil {
		t.Fatalf("OnPreStream 返回错误: %v", err)
	}

	span := h.workflowSpans["invoke-1"]
	if len(span.StreamInputs) != 0 {
		t.Errorf("空 dict 时 StreamInputs 长度 = %d, want 0", len(span.StreamInputs))
	}
}

// TestTraceWorkflowHandler_OnPreStream_Nil不追加 测试 nil chunk 不追加到 StreamInputs
func TestTraceWorkflowHandler_OnPreStream_Nil不追加(t *testing.T) {
	h, _ := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	err := h.OnPreStream(context.Background(), "invoke-1", nil, false)
	if err != nil {
		t.Fatalf("OnPreStream 返回错误: %v", err)
	}

	span := h.workflowSpans["invoke-1"]
	if len(span.StreamInputs) != 0 {
		t.Errorf("nil chunk 时 StreamInputs 长度 = %d, want 0", len(span.StreamInputs))
	}
}

// TestTraceWorkflowHandler_OnPreStream_字符串不追加 测试 string chunk 不追加到 StreamInputs
func TestTraceWorkflowHandler_OnPreStream_字符串不追加(t *testing.T) {
	h, _ := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	err := h.OnPreStream(context.Background(), "invoke-1", "string chunk", false)
	if err != nil {
		t.Fatalf("OnPreStream 返回错误: %v", err)
	}

	span := h.workflowSpans["invoke-1"]
	if len(span.StreamInputs) != 0 {
		t.Errorf("string chunk 时 StreamInputs 长度 = %d, want 0", len(span.StreamInputs))
	}
}

// TestTraceWorkflowHandler_OnPostInvoke_End组件更新Inputs 测试 End/Message 组件更新 Inputs
func TestTraceWorkflowHandler_OnPostInvoke_End组件更新Inputs(t *testing.T) {
	h, _ := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")
	span := h.workflowSpans["invoke-1"]
	span.ComponentType = "End"

	outputs := map[string]any{"result": "done"}
	inputs := map[string]any{"final_input": "value"}

	err := h.OnPostInvoke(context.Background(), "invoke-1", outputs, inputs)
	if err != nil {
		t.Fatalf("OnPostInvoke 返回错误: %v", err)
	}

	if span.Inputs == nil {
		t.Error("End 组件应更新 Inputs")
	}
}

// TestUpdateStartTraceData_MetaData序列化 测试元数据序列化处理
func TestUpdateStartTraceData_MetaData序列化(t *testing.T) {
	h, _ := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	instanceInfo := map[string]any{
		"class_name": "TestClass",
		"model":      "qwen-max",
		"tools":      []string{"tool1", "tool2"},
	}

	h.updateStartTraceData(span, string(InvokeTypeLLM), "inputs", instanceInfo)

	if span.MetaData == nil {
		t.Fatal("MetaData 不应为 nil")
	}
	// 验证 MetaData 通过了 json.Marshal/Unmarshal 循环
	if _, err := json.Marshal(span.MetaData); err != nil {
		t.Errorf("MetaData 无法序列化: %v", err)
	}
	if span.MetaData["class_name"] != "TestClass" {
		t.Errorf("MetaData[class_name] = %v, want %q", span.MetaData["class_name"], "TestClass")
	}
}

// TestTraceAgentHandler_OnChainStart 测试链式调用开始
func TestTraceAgentHandler_OnChainStart(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	instanceInfo := map[string]any{"class_name": "TestChain"}
	err := h.OnChainStart(context.Background(), span, nil, instanceInfo)
	if err != nil {
		t.Fatalf("OnChainStart 返回错误: %v", err)
	}
	if span.InvokeType != string(InvokeTypeChain) {
		t.Errorf("InvokeType = %q, want %q", span.InvokeType, InvokeTypeChain)
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnChainEnd 测试链式调用结束
func TestTraceAgentHandler_OnChainEnd(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()
	now := time.Now()
	span.StartTime = &now

	err := h.OnChainEnd(context.Background(), span, "outputs")
	if err != nil {
		t.Fatalf("OnChainEnd 返回错误: %v", err)
	}
	if span.EndTime == nil {
		t.Error("EndTime 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnChainError 测试链式调用错误
func TestTraceAgentHandler_OnChainError(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()
	now := time.Now()
	span.StartTime = &now

	err := h.OnChainError(context.Background(), span, fmt.Errorf("chain error"))
	if err != nil {
		t.Fatalf("OnChainError 返回错误: %v", err)
	}
	if span.Error == nil {
		t.Error("Error 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnPromptStart 测试提示词调用开始
func TestTraceAgentHandler_OnPromptStart(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	instanceInfo := map[string]any{"class_name": "TestPrompt"}
	err := h.OnPromptStart(context.Background(), span, nil, instanceInfo)
	if err != nil {
		t.Fatalf("OnPromptStart 返回错误: %v", err)
	}
	if span.InvokeType != string(InvokeTypePrompt) {
		t.Errorf("InvokeType = %q, want %q", span.InvokeType, InvokeTypePrompt)
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnPromptEnd 测试提示词调用结束
func TestTraceAgentHandler_OnPromptEnd(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()
	now := time.Now()
	span.StartTime = &now

	err := h.OnPromptEnd(context.Background(), span, "outputs")
	if err != nil {
		t.Fatalf("OnPromptEnd 返回错误: %v", err)
	}
	if span.EndTime == nil {
		t.Error("EndTime 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnPromptError 测试提示词调用错误
func TestTraceAgentHandler_OnPromptError(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()
	now := time.Now()
	span.StartTime = &now

	err := h.OnPromptError(context.Background(), span, fmt.Errorf("prompt error"))
	if err != nil {
		t.Fatalf("OnPromptError 返回错误: %v", err)
	}
	if span.Error == nil {
		t.Error("Error 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnRetrieverStart 测试检索调用开始
func TestTraceAgentHandler_OnRetrieverStart(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	instanceInfo := map[string]any{"class_name": "TestRetriever"}
	err := h.OnRetrieverStart(context.Background(), span, nil, instanceInfo)
	if err != nil {
		t.Fatalf("OnRetrieverStart 返回错误: %v", err)
	}
	if span.InvokeType != string(InvokeTypeRetriever) {
		t.Errorf("InvokeType = %q, want %q", span.InvokeType, InvokeTypeRetriever)
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnRetrieverEnd 测试检索调用结束
func TestTraceAgentHandler_OnRetrieverEnd(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()
	now := time.Now()
	span.StartTime = &now

	err := h.OnRetrieverEnd(context.Background(), span, "outputs")
	if err != nil {
		t.Fatalf("OnRetrieverEnd 返回错误: %v", err)
	}
	if span.EndTime == nil {
		t.Error("EndTime 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnRetrieverError 测试检索调用错误
func TestTraceAgentHandler_OnRetrieverError(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()
	now := time.Now()
	span.StartTime = &now

	err := h.OnRetrieverError(context.Background(), span, fmt.Errorf("retriever error"))
	if err != nil {
		t.Fatalf("OnRetrieverError 返回错误: %v", err)
	}
	if span.Error == nil {
		t.Error("Error 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnEvaluatorStart 测试评估调用开始
func TestTraceAgentHandler_OnEvaluatorStart(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	instanceInfo := map[string]any{"class_name": "TestEvaluator"}
	err := h.OnEvaluatorStart(context.Background(), span, nil, instanceInfo)
	if err != nil {
		t.Fatalf("OnEvaluatorStart 返回错误: %v", err)
	}
	if span.InvokeType != string(InvokeTypeEvaluator) {
		t.Errorf("InvokeType = %q, want %q", span.InvokeType, InvokeTypeEvaluator)
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnEvaluatorEnd 测试评估调用结束
func TestTraceAgentHandler_OnEvaluatorEnd(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()
	now := time.Now()
	span.StartTime = &now

	err := h.OnEvaluatorEnd(context.Background(), span, "outputs")
	if err != nil {
		t.Fatalf("OnEvaluatorEnd 返回错误: %v", err)
	}
	if span.EndTime == nil {
		t.Error("EndTime 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnEvaluatorError 测试评估调用错误
func TestTraceAgentHandler_OnEvaluatorError(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()
	now := time.Now()
	span.StartTime = &now

	err := h.OnEvaluatorError(context.Background(), span, fmt.Errorf("evaluator error"))
	if err != nil {
		t.Fatalf("OnEvaluatorError 返回错误: %v", err)
	}
	if span.Error == nil {
		t.Error("Error 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnWorkflowStart 测试工作流调用开始
func TestTraceAgentHandler_OnWorkflowStart(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	instanceInfo := map[string]any{"class_name": "TestWorkflow"}
	err := h.OnWorkflowStart(context.Background(), span, nil, instanceInfo)
	if err != nil {
		t.Fatalf("OnWorkflowStart 返回错误: %v", err)
	}
	if span.InvokeType != string(InvokeTypeWorkflow) {
		t.Errorf("InvokeType = %q, want %q", span.InvokeType, InvokeTypeWorkflow)
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnWorkflowEnd 测试工作流调用结束
func TestTraceAgentHandler_OnWorkflowEnd(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()
	now := time.Now()
	span.StartTime = &now

	err := h.OnWorkflowEnd(context.Background(), span, "outputs")
	if err != nil {
		t.Fatalf("OnWorkflowEnd 返回错误: %v", err)
	}
	if span.EndTime == nil {
		t.Error("EndTime 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnWorkflowError 测试工作流调用错误
func TestTraceAgentHandler_OnWorkflowError(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()
	now := time.Now()
	span.StartTime = &now

	err := h.OnWorkflowError(context.Background(), span, fmt.Errorf("workflow error"))
	if err != nil {
		t.Fatalf("OnWorkflowError 返回错误: %v", err)
	}
	if span.Error == nil {
		t.Error("Error 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnPluginEnd 测试插件调用结束
func TestTraceAgentHandler_OnPluginEnd(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()
	now := time.Now()
	span.StartTime = &now

	err := h.OnPluginEnd(context.Background(), span, "outputs")
	if err != nil {
		t.Fatalf("OnPluginEnd 返回错误: %v", err)
	}
	if span.EndTime == nil {
		t.Error("EndTime 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceAgentHandler_OnPluginError 测试插件调用错误
func TestTraceAgentHandler_OnPluginError(t *testing.T) {
	h, sw := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()
	now := time.Now()
	span.StartTime = &now

	err := h.OnPluginError(context.Background(), span, fmt.Errorf("plugin error"))
	if err != nil {
		t.Fatalf("OnPluginError 返回错误: %v", err)
	}
	if span.Error == nil {
		t.Error("Error 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceWorkflowHandler_EmitStreamWriter 测试工作流处理器流写入
func TestTraceWorkflowHandler_EmitStreamWriter(t *testing.T) {
	h, sw := newTestWorkflowHandler()
	span := h.getTracerWorkflowSpan("invoke-1")

	err := h.EmitStreamWriter(context.Background(), &span.Span)
	if err != nil {
		t.Fatalf("EmitStreamWriter 返回错误: %v", err)
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
	if sw.writes[0].Type != "tracer_workflow" {
		t.Errorf("写入类型 = %q, want %q", sw.writes[0].Type, "tracer_workflow")
	}
}

// TestTraceWorkflowHandler_EmitStreamWriter_写入失败 测试工作流处理器流写入失败
func TestTraceWorkflowHandler_EmitStreamWriter_写入失败(t *testing.T) {
	sw := &mockStreamWriter{writeErr: fmt.Errorf("写入失败")}
	sm := NewSpanManager("test-trace-id")
	h := NewTraceWorkflowHandler(sw, sm)
	span := h.getTracerWorkflowSpan("invoke-1")

	err := h.EmitStreamWriter(context.Background(), &span.Span)
	if err == nil {
		t.Error("期望返回写入错误，但返回 nil")
	}
}

// TestTraceWorkflowHandler_EmitStreamWriter_StreamWriter为nil 测试工作流处理器 StreamWriter 为 nil
func TestTraceWorkflowHandler_EmitStreamWriter_StreamWriter为nil(t *testing.T) {
	sm := NewSpanManager("test-trace-id")
	h := NewTraceWorkflowHandler(nil, sm)
	span := h.getTracerWorkflowSpan("invoke-1")

	err := h.EmitStreamWriter(context.Background(), &span.Span)
	if err != nil {
		t.Errorf("StreamWriter 为 nil 时不应返回错误，实际: %v", err)
	}
}

// TestSetWorkflowMetadata 测试工作流元数据设置
func TestSetWorkflowMetadata(t *testing.T) {
	span := &TraceWorkflowSpan{}

	metadata := map[string]any{
		"ExecutionID":     "exec-1",
		"WorkflowID":      "wf-1",
		"WorkflowVersion": "v1.0",
		"WorkflowName":    "测试工作流",
		"ComponentID":     "comp-1",
		"ComponentName":   "LLM",
		"ComponentType":   "LLM",
		"LoopNodeID":      "loop-1",
		"LoopIndex":       3,
		"ParentNodeID":    "parent-1",
		"UnknownField":    "should be ignored",
	}

	setWorkflowMetadata(span, metadata)

	if span.ExecutionID != "exec-1" {
		t.Errorf("ExecutionID = %q, want %q", span.ExecutionID, "exec-1")
	}
	if span.WorkflowID != "wf-1" {
		t.Errorf("WorkflowID = %q, want %q", span.WorkflowID, "wf-1")
	}
	if span.WorkflowVersion != "v1.0" {
		t.Errorf("WorkflowVersion = %q, want %q", span.WorkflowVersion, "v1.0")
	}
	if span.WorkflowName != "测试工作流" {
		t.Errorf("WorkflowName = %q, want %q", span.WorkflowName, "测试工作流")
	}
	if span.ComponentID != "comp-1" {
		t.Errorf("ComponentID = %q, want %q", span.ComponentID, "comp-1")
	}
	if span.ComponentName != "LLM" {
		t.Errorf("ComponentName = %q, want %q", span.ComponentName, "LLM")
	}
	if span.ComponentType != "LLM" {
		t.Errorf("ComponentType = %q, want %q", span.ComponentType, "LLM")
	}
	if span.LoopNodeID != "loop-1" {
		t.Errorf("LoopNodeID = %q, want %q", span.LoopNodeID, "loop-1")
	}
	if span.LoopIndex == nil || *span.LoopIndex != 3 {
		t.Errorf("LoopIndex = %v, want 3", span.LoopIndex)
	}
	if span.ParentNodeID != "parent-1" {
		t.Errorf("ParentNodeID = %q, want %q", span.ParentNodeID, "parent-1")
	}
}

// TestSendData_写入失败 测试 sendData 写入失败
func TestSendData_写入失败(t *testing.T) {
	sw := &mockStreamWriter{writeErr: fmt.Errorf("写入失败")}
	sm := NewSpanManager("test-trace-id")
	h := NewTraceWorkflowHandler(sw, sm)

	span := h.getTracerWorkflowSpan("invoke-1")

	err := h.sendData(span, nil)
	if err == nil {
		t.Error("期望返回写入错误，但返回 nil")
	}
}

// TestGetTracerAgentSpan_缓存测试 测试 Agent 追踪跨度缓存
func TestGetTracerAgentSpan_缓存测试(t *testing.T) {
	h, _ := newTestAgentHandler()

	// 创建 span
	span := h.getTracerAgentSpan("")
	if span == nil {
		t.Fatal("getTracerAgentSpan 不应返回 nil")
	}

	// 通过 invokeID 再次查找应返回缓存的 span
	cached := h.getTracerAgentSpan(span.InvokeID)
	if cached == nil {
		t.Fatal("缓存的 span 不应为 nil")
	}
	if cached.InvokeID != span.InvokeID {
		t.Errorf("缓存 InvokeID = %q, want %q", cached.InvokeID, span.InvokeID)
	}
}

// TestGetTracerWorkflowSpan_缓存测试 测试工作流追踪跨度缓存
func TestGetTracerWorkflowSpan_缓存测试(t *testing.T) {
	h, _ := newTestWorkflowHandler()

	// 创建 span
	span := h.getTracerWorkflowSpan("invoke-1")
	if span == nil {
		t.Fatal("getTracerWorkflowSpan 不应返回 nil")
	}

	// 通过 invokeID 再次查找应返回缓存的 span
	cached := h.getTracerWorkflowSpan("invoke-1")
	if cached == nil {
		t.Fatal("缓存的 span 不应为 nil")
	}
	if cached.InvokeID != "invoke-1" {
		t.Errorf("缓存 InvokeID = %q, want %q", cached.InvokeID, "invoke-1")
	}
}

// TestTraceWorkflowHandler_OnInvoke_LLM异常更新Span 测试 LLM 组件异常时额外更新 Span
func TestTraceWorkflowHandler_OnInvoke_LLM异常更新Span(t *testing.T) {
	sw := &mockStreamWriter{}
	sm := NewSpanManager("test-trace-id")
	h := NewTraceWorkflowHandler(sw, sm)

	span := h.getTracerWorkflowSpan("invoke-1")
	span.ComponentType = "LLM"
	now := time.Now()
	span.StartTime = &now

	err := h.OnInvoke(context.Background(), "invoke-1", nil, fmt.Errorf("LLM error"))
	if err != nil {
		t.Fatalf("OnInvoke 返回错误: %v", err)
	}

	// LLM 组件异常时，应额外调用 UpdateSpan
	if span.Error == nil {
		t.Error("Error 不应为 nil")
	}
}

// TestBuildWorkflowPayload_完整字段 测试构建完整工作流 payload
func TestBuildWorkflowPayload_完整字段(t *testing.T) {
	loopIdx := 2
	span := &TraceWorkflowSpan{
		Span: Span{
			TraceID:        "trace-1",
			InvokeID:       "invoke-1",
			ParentInvokeID: "parent-1",
			Status:         "running",
			StartTime:      &time.Time{},
			EndTime:        &time.Time{},
			Inputs:         "inputs",
			Outputs:        "outputs",
			Error:          map[string]any{"error_code": 100},
			OnInvokeData:   []map[string]any{{"key": "val"}},
		},
		ExecutionID:       "exec-1",
		SourceIDs:         []string{"src-1"},
		WorkflowID:        "wf-1",
		WorkflowVersion:   "v1",
		WorkflowName:      "测试",
		ComponentID:       "comp-1",
		ComponentName:     "LLM",
		ComponentType:     "LLM",
		LoopNodeID:        "loop-1",
		LoopIndex:         &loopIdx,
		ParentNodeID:      "parent-1",
		StreamInputs:      []any{"in1"},
		StreamOutputs:     []any{"out1"},
		InteractiveInputs: "interact",
		InnerError:        map[string]any{"code": 500},
	}

	result := buildWorkflowPayload(span)

	// 验证所有字段存在
	expectedKeys := []string{
		"traceId", "invokeId", "parentInvokeId", "status",
		"startTime", "endTime", "inputs", "outputs", "error", "onInvokeData",
		"executionId", "sourceIds", "workflowId", "workflowVersion", "workflowName",
		"componentId", "componentName", "componentType",
		"loopNodeId", "loopIndex", "parentNodeId",
		"streamInputs", "streamOutputs", "interactiveInputs", "innerError",
	}
	for _, key := range expectedKeys {
		if _, exists := result[key]; !exists {
			t.Errorf("payload 缺少字段 %q", key)
		}
	}
	// 不应包含 childInvokes 和 llmInvokeData
	if _, exists := result["childInvokes"]; exists {
		t.Error("payload 不应包含 childInvokes")
	}
	if _, exists := result["llmInvokeData"]; exists {
		t.Error("payload 不应包含 llmInvokeData")
	}
}

// TestTraceAgentHandler_updateStartTraceData_无ClassName 测试元数据无 class_name 字段
func TestTraceAgentHandler_updateStartTraceData_无ClassName(t *testing.T) {
	h, _ := newTestAgentHandler()
	span := h.spanManager.CreateAgentSpan()

	instanceInfo := map[string]any{
		"model": "qwen-max",
	}
	h.updateStartTraceData(span, string(InvokeTypeLLM), "inputs", instanceInfo)

	if span.Name != "" {
		t.Errorf("无 class_name 时 Name 应为空，实际 = %q", span.Name)
	}
	if span.InvokeType != string(InvokeTypeLLM) {
		t.Errorf("InvokeType = %q, want %q", span.InvokeType, InvokeTypeLLM)
	}
}

// TestTraceWorkflowHandler_OnPreInvoke_needSend为false 测试组件预调用不发送
func TestTraceWorkflowHandler_OnPreInvoke_needSend为false(t *testing.T) {
	h, sw := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	inputs := map[string]any{"prompt": "updated"}
	componentMetadata := map[string]any{
		"ComponentName": "新组件",
	}

	err := h.OnPreInvoke(context.Background(), "invoke-1", inputs, componentMetadata, false)
	if err != nil {
		t.Fatalf("OnPreInvoke 返回错误: %v", err)
	}

	// needSend=false 时不应写入流
	if len(sw.writes) != 0 {
		t.Fatalf("needSend=false 时写入次数 = %d, want 0", len(sw.writes))
	}
}

// TestTraceWorkflowHandler_OnInteract_needSend为false 测试组件交互不发送
func TestTraceWorkflowHandler_OnInteract_needSend为false(t *testing.T) {
	h, sw := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	inputs := map[string]any{"user_input": "继续"}
	componentMetadata := map[string]any{
		"ComponentType": "Interaction",
	}

	err := h.OnInteract(context.Background(), "invoke-1", inputs, componentMetadata, false)
	if err != nil {
		t.Fatalf("OnInteract 返回错误: %v", err)
	}

	if len(sw.writes) != 0 {
		t.Fatalf("needSend=false 时写入次数 = %d, want 0", len(sw.writes))
	}
}

// TestTraceWorkflowHandler_OnPreStream_needSend为false 测试组件预流式不发送
func TestTraceWorkflowHandler_OnPreStream_needSend为false(t *testing.T) {
	h, sw := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	chunk := map[string]any{"text": "streaming"}
	err := h.OnPreStream(context.Background(), "invoke-1", chunk, false)
	if err != nil {
		t.Fatalf("OnPreStream 返回错误: %v", err)
	}

	if len(sw.writes) != 0 {
		t.Fatalf("needSend=false 时写入次数 = %d, want 0", len(sw.writes))
	}
}

// TestTraceWorkflowHandler_OnInvoke_异常innerError 测试组件调用异常含 inner_error
func TestTraceWorkflowHandler_OnInvoke_异常innerError(t *testing.T) {
	sw := &mockStreamWriter{}
	sm := NewSpanManager("test-trace-id")
	h := NewTraceWorkflowHandler(sw, sm)

	span := h.getTracerWorkflowSpan("invoke-1")
	now := time.Now()
	span.StartTime = &now

	onInvokeData := map[string]any{
		"inner_error": map[string]any{"code": 500, "msg": "内部错误"},
		"retry":       1,
	}
	err := h.OnInvoke(context.Background(), "invoke-1", onInvokeData, fmt.Errorf("test error"))
	if err != nil {
		t.Fatalf("OnInvoke 返回错误: %v", err)
	}

	if span.InnerError == nil {
		t.Fatal("InnerError 不应为 nil")
	}
	if span.EndTime == nil {
		t.Error("异常时 EndTime 不应为 nil")
	}
}

// TestTraceWorkflowHandler_OnInvoke_OnInvokeData为nil 测试组件调用 OnInvokeData 为 nil
func TestTraceWorkflowHandler_OnInvoke_OnInvokeData为nil(t *testing.T) {
	h, sw := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")

	err := h.OnInvoke(context.Background(), "invoke-1", nil, nil)
	if err != nil {
		t.Fatalf("OnInvoke 返回错误: %v", err)
	}

	span := h.workflowSpans["invoke-1"]
	if span.OnInvokeData == nil {
		t.Error("OnInvokeData 应初始化为空切片")
	}
	if len(span.OnInvokeData) != 0 {
		t.Errorf("OnInvokeData 长度 = %d, want 0", len(span.OnInvokeData))
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestTraceWorkflowHandler_OnCallDone_outputs为nil 测试组件调用完成 outputs 为 nil
func TestTraceWorkflowHandler_OnCallDone_outputs为nil(t *testing.T) {
	h, sw := newTestWorkflowHandler()

	_ = h.getTracerWorkflowSpan("invoke-1")
	span := h.workflowSpans["invoke-1"]
	now := time.Now()
	span.StartTime = &now

	err := h.OnCallDone(context.Background(), "invoke-1", nil)
	if err != nil {
		t.Fatalf("OnCallDone 返回错误: %v", err)
	}

	if span.EndTime == nil {
		t.Error("EndTime 不应为 nil")
	}
	if len(sw.writes) != 1 {
		t.Fatalf("写入次数 = %d, want 1", len(sw.writes))
	}
}

// TestGetTracerWorkflowSpan_有父Span 测试获取工作流追踪跨度时有父 Span
func TestGetTracerWorkflowSpan_有父Span(t *testing.T) {
	sw := &mockStreamWriter{}
	sm := NewSpanManager("test-trace-id")
	h := NewTraceWorkflowHandler(sw, sm)

	// 先创建一个 span 作为 lastSpan
	parentSpan := h.getTracerWorkflowSpan("parent-1")

	// 再创建子 span，应通过 LastSpan 找到父 span
	childSpan := h.getTracerWorkflowSpan("child-1")
	if childSpan == nil {
		t.Fatal("子 span 不应为 nil")
	}
	// 验证子 span 的 ParentInvokeID 与父 span 关联
	if parentSpan.InvokeID != "" && childSpan.ParentInvokeID != parentSpan.InvokeID {
		t.Errorf("ParentInvokeID = %q, want %q", childSpan.ParentInvokeID, parentSpan.InvokeID)
	}
}
