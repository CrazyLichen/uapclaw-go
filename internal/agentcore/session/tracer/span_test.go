package tracer

import (
	"encoding/json"
	"testing"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSpan_Update 验证 Span.Update 通过反射设置字段值
func TestSpan_Update(t *testing.T) {
	now := time.Now()
	s := &Span{TraceID: "trace-1"}

	s.Update(map[string]any{
		"Status":  "running",
		"Inputs":  map[string]any{"key": "val"},
		"Outputs": "result",
		"Error":   map[string]any{"code": "500"},
		"EndTime": &now,
	})

	if s.Status != "running" {
		t.Errorf("Status: got %q, want %q", s.Status, "running")
	}
	if s.Inputs == nil {
		t.Error("Inputs 不应为 nil")
	}
	if s.Outputs != "result" {
		t.Errorf("Outputs: got %v, want %q", s.Outputs, "result")
	}
	if s.Error == nil {
		t.Error("Error 不应为 nil")
	}
	if s.EndTime == nil || !s.EndTime.Equal(now) {
		t.Error("EndTime 未正确设置")
	}
	// 不存在的字段应被忽略
	s.Update(map[string]any{"NonExistent": "val"})
	if s.Status != "running" {
		t.Error("更新不存在的字段不应影响已有字段")
	}
}

// TestSpan_AppendChildInvokeID 验证追加子调用标识
func TestSpan_AppendChildInvokeID(t *testing.T) {
	s := &Span{}
	s.AppendChildInvokeID("child-1")
	if len(s.ChildInvokesID) != 1 || s.ChildInvokesID[0] != "child-1" {
		t.Errorf("ChildInvokesID: got %v, want [child-1]", s.ChildInvokesID)
	}
	s.AppendChildInvokeID("child-2")
	if len(s.ChildInvokesID) != 2 || s.ChildInvokesID[1] != "child-2" {
		t.Errorf("ChildInvokesID: got %v, want [child-1 child-2]", s.ChildInvokesID)
	}
}

// TestTraceAgentSpan_JSON序列化 验证 camelCase json tag
func TestTraceAgentSpan_JSON序列化(t *testing.T) {
	now := time.Now()
	s := &TraceAgentSpan{
		Span: Span{
			TraceID:   "trace-1",
			InvokeID:  "invoke-1",
			StartTime: &now,
			Status:    "running",
		},
		InvokeType:  "llm",
		Name:        "test-agent",
		ElapsedTime: "100ms",
		MetaData:    map[string]any{"tokens": 42},
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	raw := string(data)

	// 验证 camelCase tag
	for _, key := range []string{"traceId", "invokeId", "startTime", "invokeType", "metaData", "elapsedTime"} {
		if !containsKey(raw, key) {
			t.Errorf("JSON 缺少 camelCase 键 %q, 原始输出: %s", key, raw)
		}
	}
	// 验证 snake_case 键不存在
	for _, key := range []string{"trace_id", "invoke_id", "start_time", "invoke_type"} {
		if containsKey(raw, key) {
			t.Errorf("JSON 不应包含 snake_case 键 %q, 原始输出: %s", key, raw)
		}
	}
}

// TestTraceWorkflowSpan_JSON序列化 验证 LLMInvokeData 被 json:"-" exclude
func TestTraceWorkflowSpan_JSON序列化(t *testing.T) {
	s := &TraceWorkflowSpan{
		Span: Span{
			TraceID:  "trace-1",
			InvokeID: "invoke-1",
		},
		ExecutionID:   "exec-1",
		WorkflowID:    "wf-1",
		WorkflowName:  "test-workflow",
		ComponentID:   "comp-1",
		ComponentName: "test-component",
		ComponentType: "llm",
		LLMInvokeData: map[string]map[string]any{"model-a": {"tokens": 100}},
		StreamInputs:  []any{"in1"},
		StreamOutputs: []any{"out1"},
		InnerError:    map[string]any{"msg": "fail"},
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}
	raw := string(data)

	// 验证 LLMInvokeData 被排除
	if containsKey(raw, "llmInvokeData") || containsKey(raw, "LLMInvokeData") {
		t.Errorf("LLMInvokeData 应被 json:\"-\" 排除, 原始输出: %s", raw)
	}
	// 验证其他 camelCase 键存在
	for _, key := range []string{"executionId", "workflowId", "workflowName", "componentId", "streamInputs", "streamOutputs", "innerError"} {
		if !containsKey(raw, key) {
			t.Errorf("JSON 缺少 camelCase 键 %q, 原始输出: %s", key, raw)
		}
	}
}

// TestTraceWorkflowSpan_AppendStreamOutput 验证追加流式输出
func TestTraceWorkflowSpan_AppendStreamOutput(t *testing.T) {
	s := &TraceWorkflowSpan{}
	s.AppendStreamOutput("chunk1")
	if len(s.StreamOutputs) != 1 || s.StreamOutputs[0] != "chunk1" {
		t.Errorf("StreamOutputs: got %v, want [chunk1]", s.StreamOutputs)
	}
	s.AppendStreamOutput("chunk2")
	if len(s.StreamOutputs) != 2 || s.StreamOutputs[1] != "chunk2" {
		t.Errorf("StreamOutputs: got %v, want [chunk1 chunk2]", s.StreamOutputs)
	}
}

// TestTraceWorkflowSpan_AppendStreamInputs 验证追加流式输入
func TestTraceWorkflowSpan_AppendStreamInputs(t *testing.T) {
	s := &TraceWorkflowSpan{}
	s.AppendStreamInputs("in1")
	if len(s.StreamInputs) != 1 || s.StreamInputs[0] != "in1" {
		t.Errorf("StreamInputs: got %v, want [in1]", s.StreamInputs)
	}
	s.AppendStreamInputs("in2")
	if len(s.StreamInputs) != 2 || s.StreamInputs[1] != "in2" {
		t.Errorf("StreamInputs: got %v, want [in1 in2]", s.StreamInputs)
	}
}

// TestSpanManager_CreateAgentSpan 验证创建 Agent Span
func TestSpanManager_CreateAgentSpan(t *testing.T) {
	m := NewSpanManager("trace-1")
	span := m.CreateAgentSpan()

	if span.TraceID != "trace-1" {
		t.Errorf("TraceID: got %q, want %q", span.TraceID, "trace-1")
	}
	if span.InvokeID == "" {
		t.Error("InvokeID 不应为空")
	}
	if span.ParentInvokeID != "" {
		t.Errorf("ParentInvokeID 应为空, got %q", span.ParentInvokeID)
	}
	// 验证已注册到 manager
	got := m.GetSpan(span.InvokeID)
	if got == nil || got.InvokeID != span.InvokeID {
		t.Error("CreateAgentSpan 后应能通过 GetSpan 获取")
	}
}

// TestSpanManager_CreateAgentSpan_有父Span 验证父子关系建立
func TestSpanManager_CreateAgentSpan_有父Span(t *testing.T) {
	m := NewSpanManager("trace-1")
	parent := m.CreateAgentSpan()
	child := m.CreateAgentSpan(parent)

	if child.ParentInvokeID != parent.InvokeID {
		t.Errorf("ParentInvokeID: got %q, want %q", child.ParentInvokeID, parent.InvokeID)
	}
	if len(parent.ChildInvokesID) != 1 || parent.ChildInvokesID[0] != child.InvokeID {
		t.Errorf("parent.ChildInvokesID: got %v, want [%s]", parent.ChildInvokesID, child.InvokeID)
	}
}

// TestSpanManager_CreateWorkflowSpan 验证创建工作流 Span
func TestSpanManager_CreateWorkflowSpan(t *testing.T) {
	m := NewSpanManager("trace-1", "parent-node-1")
	span := m.CreateWorkflowSpan("invoke-1")

	if span.TraceID != "trace-1" {
		t.Errorf("TraceID: got %q, want %q", span.TraceID, "trace-1")
	}
	if span.InvokeID != "invoke-1" {
		t.Errorf("InvokeID: got %q, want %q", span.InvokeID, "invoke-1")
	}
	if span.ExecutionID != "trace-1" {
		t.Errorf("ExecutionID 应等于 traceID: got %q, want %q", span.ExecutionID, "trace-1")
	}
	if span.ParentNodeID != "parent-node-1" {
		t.Errorf("ParentNodeID: got %q, want %q", span.ParentNodeID, "parent-node-1")
	}
	if span.ParentInvokeID != "" {
		t.Errorf("ParentInvokeID 应为空, got %q", span.ParentInvokeID)
	}
}

// TestSpanManager_GetSpan 验证获取和未获取场景
func TestSpanManager_GetSpan(t *testing.T) {
	m := NewSpanManager("trace-1")
	span := m.CreateAgentSpan()

	got := m.GetSpan(span.InvokeID)
	if got == nil || got.InvokeID != span.InvokeID {
		t.Error("应能获取已创建的 Span")
	}

	if m.GetSpan("non-existent") != nil {
		t.Error("获取不存在的 invokeID 应返回 nil")
	}
}

// TestSpanManager_PopSpan 验证移除 Span
func TestSpanManager_PopSpan(t *testing.T) {
	m := NewSpanManager("trace-1")
	span := m.CreateAgentSpan()
	invokeID := span.InvokeID

	m.PopSpan(invokeID)
	if m.GetSpan(invokeID) != nil {
		t.Error("PopSpan 后应无法获取该 Span")
	}
	if m.LastSpan() != nil {
		t.Error("PopSpan 唯一 Span 后 LastSpan 应返回 nil")
	}
}

// TestSpanManager_UpdateSpan 验证更新 Span 字段
func TestSpanManager_UpdateSpan(t *testing.T) {
	m := NewSpanManager("trace-1")
	span := m.CreateAgentSpan()

	m.UpdateSpan(&span.Span, map[string]any{
		"Status": "finished",
	})

	got := m.GetSpan(span.InvokeID)
	if got == nil || got.Status != "finished" {
		t.Errorf("UpdateSpan 后 Status: got %q, want %q", got.Status, "finished")
	}
}

// TestSpanManager_LastSpan 验证获取最后一个 Span
func TestSpanManager_LastSpan(t *testing.T) {
	m := NewSpanManager("trace-1")
	s1 := m.CreateAgentSpan()
	s2 := m.CreateAgentSpan()

	last := m.LastSpan()
	if last == nil || last.InvokeID != s2.InvokeID {
		t.Errorf("LastSpan: got invokeID %q, want %q", last.InvokeID, s2.InvokeID)
	}

	m.PopSpan(s2.InvokeID)
	last = m.LastSpan()
	if last == nil || last.InvokeID != s1.InvokeID {
		t.Errorf("PopSpan 后 LastSpan: got invokeID %q, want %q", last.InvokeID, s1.InvokeID)
	}
}

// TestSpanManager_LastSpan_空时返回nil 验证无 Span 时返回 nil
func TestSpanManager_LastSpan_空时返回nil(t *testing.T) {
	m := NewSpanManager("trace-1")
	if last := m.LastSpan(); last != nil {
		t.Error("空 SpanManager 的 LastSpan 应返回 nil")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// containsKey 检查 JSON 字符串中是否包含指定键
func containsKey(jsonStr, key string) bool {
	m := make(map[string]json.RawMessage)
	if err := json.Unmarshal([]byte(jsonStr), &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}
