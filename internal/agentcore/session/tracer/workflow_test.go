package tracer

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeWorkflowSession 实现 BaseWorkflowSession 接口，用于测试
type fakeWorkflowSession struct {
	tracer       *Tracer
	executableID string
	parentID     string
	workflowID   string
	nodeID       string
	nodeType     string
	sessionState state.SessionState
	config       any
}

// fakeSessionState 实现 state.SessionState 接口，用于测试
type fakeSessionState struct {
	data map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

func (f *fakeWorkflowSession) Tracer() *Tracer           { return f.tracer }
func (f *fakeWorkflowSession) ExecutableID() string      { return f.executableID }
func (f *fakeWorkflowSession) ParentID() string          { return f.parentID }
func (f *fakeWorkflowSession) WorkflowID() string        { return f.workflowID }
func (f *fakeWorkflowSession) NodeID() string            { return f.nodeID }
func (f *fakeWorkflowSession) NodeType() string          { return f.nodeType }
func (f *fakeWorkflowSession) State() state.SessionState { return f.sessionState }
func (f *fakeWorkflowSession) Config() any               { return f.config }

func (f *fakeSessionState) GetGlobal(key state.StateKey) any {
	if f.data == nil {
		return nil
	}
	// 仅支持字符串类型键
	if key.Type() == state.StateKeyString {
		return f.data[key.String()]
	}
	return nil
}
func (f *fakeSessionState) SetGlobal(map[string]any)         {}
func (f *fakeSessionState) UpdateGlobal(map[string]any)      {}
func (f *fakeSessionState) UpdateTrace(span any)             {}
func (f *fakeSessionState) Update(data map[string]any) error { return nil }
func (f *fakeSessionState) Get(key state.StateKey) any       { return nil }
func (f *fakeSessionState) Dump() map[string]any             { return nil }
func (f *fakeSessionState) GetState() map[string]any         { return nil }
func (f *fakeSessionState) SetState(map[string]any)          {}

// newTestTracer 创建测试用 Tracer（带 StreamWriterManager）
func newTestTracer() *Tracer {
	t := NewTracer()
	emitter := stream.NewStreamEmitter()
	swm := stream.NewStreamWriterManager(emitter)
	t.Init(swm)
	return t
}

// newFakeSession 创建测试用 fakeWorkflowSession
// parentID 使用 "" 以对齐 Tracer.Init() 默认注册的 WorkflowSpanManagerDict[""] 键
func newFakeSession() *fakeWorkflowSession {
	return &fakeWorkflowSession{
		tracer:       newTestTracer(),
		executableID: "exec-001",
		parentID:     "",
		workflowID:   "wf-001",
		nodeID:       "node-001",
		nodeType:     "LLM",
		sessionState: &fakeSessionState{},
		config:       nil,
	}
}

// TestTracerWorkflowUtils_TraceWorkflowStart 测试追踪工作流开始
func TestTracerWorkflowUtils_TraceWorkflowStart(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}
	inputs := map[string]any{"query": "hello"}

	utils.TraceWorkflowStart(context.Background(), session, inputs)

	// 验证 span 已创建：WorkflowSpanManagerDict[""] 中应有 session.WorkflowID() 对应的 span
	span := session.Tracer().GetWorkflowSpan(session.WorkflowID(), "")
	if span == nil {
		t.Fatal("TraceWorkflowStart 未创建 workflow span")
	}
	if span.InvokeID != session.WorkflowID() {
		t.Errorf("span.InvokeID = %q, 期望 %q", span.InvokeID, session.WorkflowID())
	}
	if span.Inputs == nil {
		t.Error("span.Inputs 为 nil，期望非 nil")
	}
}

// TestTracerWorkflowUtils_TraceWorkflowDone 测试追踪工作流完成
func TestTracerWorkflowUtils_TraceWorkflowDone(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}
	outputs := map[string]any{"result": "done"}

	// 先触发 start 以创建 span
	utils.TraceWorkflowStart(context.Background(), session, nil)
	utils.TraceWorkflowDone(context.Background(), session, outputs)

	span := session.Tracer().GetWorkflowSpan(session.WorkflowID(), "")
	if span == nil {
		t.Fatal("TraceWorkflowDone 后 span 不应为 nil")
	}
	if span.Outputs == nil {
		t.Error("span.Outputs 为 nil，期望非 nil")
	}
}

// TestTracerWorkflowUtils_TraceComponentBegin 测试追踪组件开始
func TestTracerWorkflowUtils_TraceComponentBegin(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}
	sourceIDs := []string{"src-001", "src-002"}

	utils.TraceComponentBegin(context.Background(), session, sourceIDs)

	span := session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Fatal("TraceComponentBegin 未创建 workflow span")
	}
	if span.InvokeID != session.ExecutableID() {
		t.Errorf("span.InvokeID = %q, 期望 %q", span.InvokeID, session.ExecutableID())
	}
	if len(span.SourceIDs) != len(sourceIDs) {
		t.Errorf("span.SourceIDs 长度 = %d, 期望 %d", len(span.SourceIDs), len(sourceIDs))
	}
}

// TestTracerWorkflowUtils_TraceComponentInputs 测试追踪组件输入
func TestTracerWorkflowUtils_TraceComponentInputs(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}
	inputs := map[string]any{"prompt": "test"}

	// 先触发 begin 以创建 span
	utils.TraceComponentBegin(context.Background(), session, nil)
	utils.TraceComponentInputs(context.Background(), session, inputs, true)

	span := session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Fatal("span 不应为 nil")
	}
	if span.Inputs == nil {
		t.Error("span.Inputs 为 nil，期望非 nil")
	}
}

// TestTracerWorkflowUtils_TraceComponentOutputs 测试追踪组件输出
func TestTracerWorkflowUtils_TraceComponentOutputs(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}
	outputs := map[string]any{"answer": "result"}

	// 先触发 begin 以创建 span
	utils.TraceComponentBegin(context.Background(), session, nil)
	utils.TraceComponentOutputs(context.Background(), session, outputs)

	span := session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Fatal("span 不应为 nil")
	}
	if span.Outputs == nil {
		t.Error("span.Outputs 为 nil，期望非 nil")
	}
}

// TestTracerWorkflowUtils_TraceComponentDone 测试追踪组件完成
func TestTracerWorkflowUtils_TraceComponentDone(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}

	// 先触发 begin 以创建 span
	utils.TraceComponentBegin(context.Background(), session, nil)

	// 验证 span 存在
	span := session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Fatal("TraceComponentBegin 后 span 不应为 nil")
	}

	utils.TraceComponentDone(context.Background(), session)

	// 当前循环组件（8.20）未实现，loop_id 为空，
	// 对齐 Python：非循环组件不执行 PopWorkflowSpan，span 仍保留在缓存中。
	// 8.20 实现后，循环组件在 state 中写入 LOOP_ID，
	// TraceComponentDone 读取到非空 loop_id 后才执行 PopWorkflowSpan。
	span = session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Error("TraceComponentDone 后非循环组件 span 不应被移除（loop_id 为空时不 Pop）")
	}
}

// TestTracerWorkflowUtils_Trace 测试追踪运行时数据
func TestTracerWorkflowUtils_Trace(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}
	data := map[string]any{"step": "processing"}

	// 先触发 begin 以创建 span
	utils.TraceComponentBegin(context.Background(), session, nil)
	utils.Trace(context.Background(), session, data)

	span := session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Fatal("span 不应为 nil")
	}
	if len(span.OnInvokeData) == 0 {
		t.Error("span.OnInvokeData 为空，期望非空")
	}
}

// TestTracerWorkflowUtils_TraceError 测试追踪错误
func TestTracerWorkflowUtils_TraceError(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}
	testErr := errors.New("something went wrong")

	// 先触发 begin 以创建 span
	utils.TraceComponentBegin(context.Background(), session, nil)
	utils.TraceError(context.Background(), session, testErr)

	span := session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Fatal("span 不应为 nil")
	}
	if span.Error == nil {
		t.Error("span.Error 为 nil，期望非 nil")
	}
}

// TestTracerWorkflowUtils_Tracer为nil_静默返回 测试 Tracer 为 nil 时静默返回
func TestTracerWorkflowUtils_Tracer为nil_静默返回(t *testing.T) {
	session := &fakeWorkflowSession{
		tracer:       nil,
		executableID: "exec-001",
		parentID:     "parent-001",
		workflowID:   "wf-001",
		nodeID:       "node-001",
		nodeType:     "LLM",
		sessionState: &fakeSessionState{},
		config:       nil,
	}
	utils := TracerWorkflowUtils{}

	// 所有方法在 Tracer 为 nil 时都应静默返回，不 panic
	utils.TraceWorkflowStart(context.Background(), session, nil)
	utils.TraceComponentBegin(context.Background(), session, nil)
	utils.TraceComponentInputs(context.Background(), session, nil, true)
	utils.TraceComponentOutputs(context.Background(), session, nil)
	utils.TraceComponentDone(context.Background(), session)
	utils.Trace(context.Background(), session, nil)
	utils.TraceError(context.Background(), session, errors.New("err"))
	utils.TraceWorkflowDone(context.Background(), session, nil)
	utils.TraceComponentStreamInput(context.Background(), session, nil, true)
	utils.TraceComponentStreamOutput(context.Background(), session, nil)
	utils.TraceComponentInteractiveInputs(context.Background(), session, nil, true)
}

// TestTracerWorkflowUtils_TraceComponentStreamInput 测试追踪组件流式输入
func TestTracerWorkflowUtils_TraceComponentStreamInput(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}

	// 先触发 begin 以创建 span
	utils.TraceComponentBegin(context.Background(), session, nil)

	// chunk 为 string 时应跳过
	utils.TraceComponentStreamInput(context.Background(), session, "string chunk", true)
	span := session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Fatal("span 不应为 nil")
	}
	if len(span.StreamInputs) != 0 {
		t.Error("chunk 为 string 时 StreamInputs 应为空")
	}

	// chunk 为 map[string]any 时应正常处理
	chunk := map[string]any{"token": "hello"}
	utils.TraceComponentStreamInput(context.Background(), session, chunk, true)
	span = session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if len(span.StreamInputs) == 0 {
		t.Error("chunk 为 map 时 StreamInputs 不应为空")
	}
}

// TestTracerWorkflowUtils_TraceComponentStreamOutput 测试追踪组件流式输出
func TestTracerWorkflowUtils_TraceComponentStreamOutput(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}

	// 先触发 begin 以创建 span
	utils.TraceComponentBegin(context.Background(), session, nil)

	// chunk 为 string 时应跳过
	utils.TraceComponentStreamOutput(context.Background(), session, "string chunk")
	span := session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Fatal("span 不应为 nil")
	}
	if len(span.StreamOutputs) != 0 {
		t.Error("chunk 为 string 时 StreamOutputs 应为空")
	}

	// chunk 为 map[string]any 时应正常处理
	chunk := map[string]any{"token": "world"}
	utils.TraceComponentStreamOutput(context.Background(), session, chunk)
	span = session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if len(span.StreamOutputs) == 0 {
		t.Error("chunk 为 map 时 StreamOutputs 不应为空")
	}
}

// TestTracerWorkflowUtils_TraceComponentInteractiveInputs 测试追踪组件交互式输入
func TestTracerWorkflowUtils_TraceComponentInteractiveInputs(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}
	inputs := map[string]any{"user_input": "confirm"}

	// 先触发 begin 以创建 span
	utils.TraceComponentBegin(context.Background(), session, nil)
	utils.TraceComponentInteractiveInputs(context.Background(), session, inputs, true)

	span := session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Fatal("span 不应为 nil")
	}
	if span.InteractiveInputs == nil {
		t.Error("span.InteractiveInputs 为 nil，期望非 nil")
	}
}

// TestGetComponentMetadata_无循环信息 测试不在循环中时只返回基础4字段
func TestGetComponentMetadata_无循环信息(t *testing.T) {
	session := &fakeWorkflowSession{
		workflowID:   "wf-001",
		nodeID:       "node-001",
		nodeType:     "LLM",
		sessionState: &fakeSessionState{data: map[string]any{}},
	}

	metadata := getComponentMetadata(session)

	if metadata["component_id"] != "node-001" {
		t.Errorf("component_id = %v, 期望 node-001", metadata["component_id"])
	}
	if metadata["component_name"] != "node-001" {
		t.Errorf("component_name = %v, 期望 node-001", metadata["component_name"])
	}
	if metadata["component_type"] != "LLM" {
		t.Errorf("component_type = %v, 期望 LLM", metadata["component_type"])
	}
	if metadata["workflow_id"] != "wf-001" {
		t.Errorf("workflow_id = %v, 期望 wf-001", metadata["workflow_id"])
	}
	// 不在循环中，不应有 loop_node_id/loop_index
	if _, ok := metadata["loop_node_id"]; ok {
		t.Error("loop_node_id 不应存在")
	}
	if _, ok := metadata["loop_index"]; ok {
		t.Error("loop_index 不应存在")
	}
}

// TestGetComponentMetadata_有循环信息 测试在循环中时额外返回 loop_node_id/loop_index
// 对齐 Python: loop_id = state.get_global(LOOP_ID); index = state.get_global(loop_id + "." + "index")
func TestGetComponentMetadata_有循环信息(t *testing.T) {
	session := &fakeWorkflowSession{
		workflowID: "wf-001",
		nodeID:     "node-001",
		nodeType:   "LLM",
		sessionState: &fakeSessionState{data: map[string]any{
			loopID:              "loop_node_1", // state.GetGlobal(LOOP_ID) → "loop_node_1"
			"loop_node_1.index": 2,             // state.GetGlobal("loop_node_1.index") → 2
		}},
	}

	metadata := getComponentMetadata(session)

	if metadata["loop_node_id"] != "loop_node_1" {
		t.Errorf("loop_node_id = %v, 期望 loop_node_1", metadata["loop_node_id"])
	}
	if metadata["loop_index"] != 2 {
		t.Errorf("loop_index = %v, 期望 2", metadata["loop_index"])
	}
}

// TestGetComponentMetadata_State为nil 测试 State 为 nil 时不 panic，只返回基础字段
func TestGetComponentMetadata_State为nil(t *testing.T) {
	session := &fakeWorkflowSession{
		workflowID:   "wf-001",
		nodeID:       "node-001",
		nodeType:     "LLM",
		sessionState: nil,
	}

	metadata := getComponentMetadata(session)

	if metadata["component_id"] != "node-001" {
		t.Errorf("component_id = %v, 期望 node-001", metadata["component_id"])
	}
	if _, ok := metadata["loop_node_id"]; ok {
		t.Error("loop_node_id 不应存在")
	}
}

// TestGetComponentMetadata_LOOP_ID为空字符串 测试 LOOP_ID 为空字符串时等同于无循环
func TestGetComponentMetadata_LOOP_ID为空字符串(t *testing.T) {
	session := &fakeWorkflowSession{
		workflowID: "wf-001",
		nodeID:     "node-001",
		nodeType:   "LLM",
		sessionState: &fakeSessionState{data: map[string]any{
			loopID: "",
		}},
	}

	metadata := getComponentMetadata(session)

	if _, ok := metadata["loop_node_id"]; ok {
		t.Error("loop_node_id 不应存在（空字符串等同于无循环）")
	}
}
