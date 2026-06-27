package callback

import (
	"context"
	"sync/atomic"
	"testing"

	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestToolCallEventType_值(t *testing.T) {
	tests := []struct {
		event ToolCallEventType
		want  string
	}{
		{ToolCallStarted, "_framework:tool_call_started"},
		{ToolCallFinished, "_framework:tool_call_finished"},
		{ToolCallError, "_framework:tool_call_error"},
		{ToolResultReceived, "_framework:tool_result_received"},
		{ToolParseStarted, "_framework:tool_parse_started"},
		{ToolParseFinished, "_framework:tool_parse_finished"},
		{ToolInvokeInput, "_framework:tool_invoke_input"},
		{ToolInvokeOutput, "_framework:tool_invoke_output"},
		{ToolStreamInput, "_framework:tool_stream_input"},
		{ToolStreamOutput, "_framework:tool_stream_output"},
		{ToolAuth, "_framework:tool_auth"},
	}
	for _, tt := range tests {
		if string(tt.event) != tt.want {
			t.Errorf("ToolCallEventType = %q, want %q", tt.event, tt.want)
		}
	}
}

func TestCallbackFramework_OnTool和TriggerTool(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fw.OnTool(ToolCallStarted, func(_ context.Context, data *ToolCallEventData) any {
		if data.ToolName != "weather" {
			t.Errorf("ToolName = %q, want weather", data.ToolName)
		}
		atomic.AddInt32(&called, 1)
		return nil
	})

	card := commonschema.NewBaseCard(commonschema.WithName("weather"))
	data := NewToolCallEventData(ToolCallStarted, card)
	fw.TriggerTool(context.Background(), data)

	if atomic.LoadInt32(&called) != 1 {
		t.Errorf("called = %d, want 1", called)
	}
}

func TestCallbackFramework_注销Tool(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32

	fn := func(_ context.Context, _ *ToolCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	}

	fw.OnTool(ToolCallStarted, fn)
	fw.OffTool(ToolCallStarted, fn)

	data := NewToolCallEventData(ToolCallStarted, nil)
	fw.TriggerTool(context.Background(), data)

	if atomic.LoadInt32(&called) != 0 {
		t.Errorf("OffTool 后不应触发，called = %d", called)
	}
}

func TestCallbackFramework_多Tool回调按序执行(t *testing.T) {
	fw := NewCallbackFramework()
	var order []int

	fw.OnTool(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) any {
		order = append(order, 1)
		return nil
	})
	fw.OnTool(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) any {
		order = append(order, 2)
		return nil
	})

	data := NewToolCallEventData(ToolCallStarted, nil)
	fw.TriggerTool(context.Background(), data)

	if len(order) != 2 || order[0] != 1 || order[1] != 2 {
		t.Errorf("执行顺序 = %v, want [1 2]", order)
	}
}

func TestCallbackFramework_TriggerTool_Nil上下文(t *testing.T) {
	fw := NewCallbackFramework()
	var called int32
	fw.OnTool(ToolCallStarted, func(_ context.Context, _ *ToolCallEventData) any {
		atomic.AddInt32(&called, 1)
		return nil
	})
	fw.TriggerTool(nil, NewToolCallEventData(ToolCallStarted, nil)) //nolint:staticcheck // SA1012: 专门测试 nil context 的防御逻辑
	if atomic.LoadInt32(&called) != 0 {
		t.Error("nil context 不应触发回调")
	}
}

func TestNewToolCallEventData(t *testing.T) {
	card := commonschema.NewBaseCard(commonschema.WithName("test"), commonschema.WithID("abc123"))
	data := NewToolCallEventData(ToolCallStarted, card)
	if data.Event != ToolCallStarted {
		t.Errorf("Event = %v, want ToolCallStarted", data.Event)
	}
	if data.ToolName != "test" {
		t.Errorf("ToolName = %q, want test", data.ToolName)
	}
	if data.ToolID != "abc123" {
		t.Errorf("ToolID = %q, want abc123", data.ToolID)
	}
}

func TestNewToolCallEventData_NilCard(t *testing.T) {
	data := NewToolCallEventData(ToolCallError, nil)
	if data.Event != ToolCallError {
		t.Errorf("Event = %v, want ToolCallError", data.Event)
	}
	if data.ToolName != "" {
		t.Errorf("ToolName 应为空，实际 %q", data.ToolName)
	}
}

// TestToolCallEventType_String 验证 ToolCallEventType.String() 返回字符串值。
func TestToolCallEventType_String(t *testing.T) {
	if got := ToolCallStarted.String(); got != string(ToolCallStarted) {
		t.Errorf("ToolCallStarted.String() = %q, want %q", got, string(ToolCallStarted))
	}
}

// TestLLMCallEventType_String 验证 LLMCallEventType.String() 返回字符串值。
func TestLLMCallEventType_String(t *testing.T) {
	if got := LLMCallStarted.String(); got != string(LLMCallStarted) {
		t.Errorf("LLMCallStarted.String() = %q, want %q", got, string(LLMCallStarted))
	}
}

// TestToolCallEventData_String 验证 ToolCallEventData.String() 返回简洁描述。
func TestToolCallEventData_String(t *testing.T) {
	card := commonschema.NewBaseCard(commonschema.WithName("test"), commonschema.WithID("id123"))
	data := NewToolCallEventData(ToolCallStarted, card)
	got := data.String()
	if got == "" {
		t.Error("String() 不应返回空字符串")
	}
}

// TestSessionCallEventType_字符串值 测试 Session 事件类型字符串值
func TestSessionCallEventType_字符串值(t *testing.T) {
	if SessionCreated != "_framework:session_created" {
		t.Errorf("SessionCreated 期望 _framework:session_created，实际 %s", SessionCreated)
	}
	if AgentSessionCreated != "_framework:agent_session_created" {
		t.Errorf("AgentSessionCreated 期望 _framework:agent_session_created，实际 %s", AgentSessionCreated)
	}
}

// TestSessionCallEventType_String 测试 String 方法
func TestSessionCallEventType_String(t *testing.T) {
	if SessionCreated.String() != "_framework:session_created" {
		t.Errorf("String() 期望 _framework:session_created，实际 %s", SessionCreated.String())
	}
}

// TestSessionCallEventData_String 测试 String 方法
func TestSessionCallEventData_String(t *testing.T) {
	data := &SessionCallEventData{
		Event:     AgentSessionCreated,
		SessionID: "test-123",
	}
	result := data.String()
	if result == "" {
		t.Error("String() 不应返回空字符串")
	}
}

// TestContextCallEventType_字符串值 测试 Context 事件类型字符串值
func TestContextCallEventType_字符串值(t *testing.T) {
	tests := []struct {
		event ContextCallEventType
		want  string
	}{
		{ContextUpdated, "_framework:context_updated"},
		{ContextOffloaded, "_framework:context_offloaded"},
		{ContextRetrieved, "_framework:context_retrieved"},
		{ContextCleared, "_framework:context_cleared"},
		{ContextCompressionStateEvent, "_framework:context.compression_state"},
	}
	for _, tt := range tests {
		if string(tt.event) != tt.want {
			t.Errorf("ContextCallEventType = %q, want %q", tt.event, tt.want)
		}
	}
}

// TestContextCallEventType_String 测试 String 方法
func TestContextCallEventType_String(t *testing.T) {
	if ContextUpdated.String() != "_framework:context_updated" {
		t.Errorf("String() = %q, want _framework:context_updated", ContextUpdated.String())
	}
}

// TestContextCallEventData_String 测试 String 方法
func TestContextCallEventData_String(t *testing.T) {
	data := &ContextCallEventData{
		Event:     ContextCleared,
		SessionID: "sess-001",
		ContextID: "ctx-001",
	}
	result := data.String()
	if result == "" {
		t.Error("String() 不应返回空字符串")
	}
}

// TestContextCallEventData_NilString 测试 nil String
func TestContextCallEventData_NilString(t *testing.T) {
	var d *ContextCallEventData
	if d.String() != "nil" {
		t.Errorf("nil String() = %q, want nil", d.String())
	}
}

// TestBuildEventName 测试 BuildEventName 构建带 scope 的事件名
func TestBuildEventName(t *testing.T) {
	got := BuildEventName("agent", "started")
	if got != "agent:started" {
		t.Errorf("BuildEventName = %q, want %q", got, "agent:started")
	}
}

// TestParseEventName 测试 ParseEventName 解析带 scope 的事件名
func TestParseEventName(t *testing.T) {
	scope, name := ParseEventName("_framework:llm_call_started")
	if scope != "_framework" || name != "llm_call_started" {
		t.Errorf("ParseEventName = %q, %q; want _framework, llm_call_started", scope, name)
	}
	scope2, name2 := ParseEventName("no_colon")
	if scope2 != "_framework" || name2 != "no_colon" {
		t.Errorf("ParseEventName(无冒号) = %q, %q; want _framework, no_colon", scope2, name2)
	}
}

// TestEventBase_GetEvent 测试 EventBase.GetEvent 获取带 scope 的完整事件名
func TestEventBase_GetEvent(t *testing.T) {
	eb := EventBase{Scope: "workflow"}
	got := eb.GetEvent("started")
	if got != "workflow:started" {
		t.Errorf("GetEvent = %q, want %q", got, "workflow:started")
	}
}

// TestWorkflowEventType_值验证 测试 Workflow 事件类型枚举值
func TestWorkflowEventType_值验证(t *testing.T) {
	if WorkflowStarted != "_framework:workflow_started" {
		t.Errorf("WorkflowStarted = %q", WorkflowStarted)
	}
}

// TestMemoryEventType_值验证 测试 Memory 事件类型枚举值
func TestMemoryEventType_值验证(t *testing.T) {
	if MemoryAdded != "_framework:memory_added" {
		t.Errorf("MemoryAdded = %q", MemoryAdded)
	}
}

// TestTaskManagerEventType_值验证 测试 TaskManager 事件类型枚举值
func TestTaskManagerEventType_值验证(t *testing.T) {
	if TaskCreated != "_framework:task_created" {
		t.Errorf("TaskCreated = %q", TaskCreated)
	}
}

// ──────────────────────────── 覆盖率补充测试 ────────────────────────────

// TestWorkflowEventData_String 测试 WorkflowEventData.String() 方法
func TestWorkflowEventData_String(t *testing.T) {
	data := &WorkflowEventData{
		Event:      WorkflowStarted,
		WorkflowID: "wf-001",
		NodeID:     "node-001",
	}
	result := data.String()
	if result == "" {
		t.Error("String() 不应返回空字符串")
	}
}

// TestWorkflowEventData_NilString 测试 nil WorkflowEventData.String()
func TestWorkflowEventData_NilString(t *testing.T) {
	var d *WorkflowEventData
	if d.String() != "nil" {
		t.Errorf("nil String() = %q, want nil", d.String())
	}
}

// TestAgentTeamEventData_String 测试 AgentTeamEventData.String() 方法
func TestAgentTeamEventData_String(t *testing.T) {
	data := &AgentTeamEventData{
		Event:   AgentP2PReceived,
		AgentID: "agent-001",
	}
	result := data.String()
	if result == "" {
		t.Error("String() 不应返回空字符串")
	}
}

// TestAgentTeamEventData_NilString 测试 nil AgentTeamEventData.String()
func TestAgentTeamEventData_NilString(t *testing.T) {
	var d *AgentTeamEventData
	if d.String() != "nil" {
		t.Errorf("nil String() = %q, want nil", d.String())
	}
}

// TestRetrievalEventData_String 测试 RetrievalEventData.String() 方法
func TestRetrievalEventData_String(t *testing.T) {
	data := &RetrievalEventData{
		Event: RetrievalStarted,
		Query: "test-query",
	}
	result := data.String()
	if result == "" {
		t.Error("String() 不应返回空字符串")
	}
}

// TestRetrievalEventData_NilString 测试 nil RetrievalEventData.String()
func TestRetrievalEventData_NilString(t *testing.T) {
	var d *RetrievalEventData
	if d.String() != "nil" {
		t.Errorf("nil String() = %q, want nil", d.String())
	}
}

// TestMemoryEventData_String 测试 MemoryEventData.String() 方法
func TestMemoryEventData_String(t *testing.T) {
	data := &MemoryEventData{
		Event: MemoryAdded,
		Key:   "test-key",
	}
	result := data.String()
	if result == "" {
		t.Error("String() 不应返回空字符串")
	}
}

// TestMemoryEventData_NilString 测试 nil MemoryEventData.String()
func TestMemoryEventData_NilString(t *testing.T) {
	var d *MemoryEventData
	if d.String() != "nil" {
		t.Errorf("nil String() = %q, want nil", d.String())
	}
}

// TestTaskManagerEventData_String 测试 TaskManagerEventData.String() 方法
func TestTaskManagerEventData_String(t *testing.T) {
	data := &TaskManagerEventData{
		Event:  TaskCreated,
		TaskID: "task-001",
		Status: "running",
	}
	result := data.String()
	if result == "" {
		t.Error("String() 不应返回空字符串")
	}
}

// TestTaskManagerEventData_NilString 测试 nil TaskManagerEventData.String()
func TestTaskManagerEventData_NilString(t *testing.T) {
	var d *TaskManagerEventData
	if d.String() != "nil" {
		t.Errorf("nil String() = %q, want nil", d.String())
	}
}

// TestGlobalAgentEventData_String 测试 GlobalAgentEventData.String() 方法
func TestGlobalAgentEventData_String(t *testing.T) {
	data := &GlobalAgentEventData{
		Event:     GlobalAgentStarted,
		AgentID:   "agent-001",
		AgentName: "test-agent",
	}
	result := data.String()
	if result == "" {
		t.Error("String() 不应返回空字符串")
	}
}

// TestGlobalAgentEventData_NilString 测试 nil GlobalAgentEventData.String()
func TestGlobalAgentEventData_NilString(t *testing.T) {
	var d *GlobalAgentEventData
	if d.String() != "nil" {
		t.Errorf("nil String() = %q, want nil", d.String())
	}
}
