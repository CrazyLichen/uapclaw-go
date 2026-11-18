package tracer

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
)

// ──────────────────────────── 结构体 ────────────────────────────

// fakeWorkflowSession 实现 WorkflowNodeSession 接口，用于测试
type fakeWorkflowSession struct {
	tracer       *Tracer
	executableID string
	parentID     string
	workflowID   string
	nodeID       string
	nodeType     string
	sessionState state.SessionState
	config       config.SessionConfig
}

// fakeSessionState 实现 state.SessionState 接口，用于测试
type fakeSessionState struct {
	data map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

func (f *fakeWorkflowSession) Tracer() *Tracer              { return f.tracer }
func (f *fakeWorkflowSession) ExecutableID() string         { return f.executableID }
func (f *fakeWorkflowSession) ParentID() string             { return f.parentID }
func (f *fakeWorkflowSession) WorkflowID() string           { return f.workflowID }
func (f *fakeWorkflowSession) NodeID() string               { return f.nodeID }
func (f *fakeWorkflowSession) NodeType() string             { return f.nodeType }
func (f *fakeWorkflowSession) State() state.SessionState    { return f.sessionState }
func (f *fakeWorkflowSession) Config() config.SessionConfig { return f.config }

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

	utils.TraceComponentBegin(context.Background(), session, sourceIDs, nil)

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

// TestTracerWorkflowUtils_TraceComponentBegin_extraMetadata 合并 extraMetadata 进组件元数据
// （Python trace_component_begin extra_metadata 形参，workflow_tracer.py:67-69）。
// Python Span.update（span.py:28-32）对 span 上不存在的属性静默跳过——extra_metadata
// 仅能覆盖既有 span 字段，未知键丢弃；Go setWorkflowMetadata 的 switch+default 语义一致。
func TestTracerWorkflowUtils_TraceComponentBegin_extraMetadata(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}
	originWorkflowID := session.WorkflowID()

	utils.TraceComponentBegin(context.Background(), session, nil, map[string]any{
		"WorkflowID":   "override-id", // 覆盖既有 span 字段（Python **metadata 展开 setattr 语义）
		"custom_top_k": 5,             // span 无此字段 → Python hasattr 跳过 / Go default 忽略
	})

	span := session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Fatal("TraceComponentBegin 未创建 workflow span")
	}
	if span.WorkflowID != "override-id" {
		t.Errorf("extraMetadata 覆盖 WorkflowID = %q, 期望 override-id", span.WorkflowID)
	}
	_ = originWorkflowID
}

// TestTracerWorkflowUtils_TraceComponentBegin_metadata落span字段 端到端锁定：
// getComponentMetadata（Camel 键）→ TriggerWorkflow(TraceWFCallStart) → setWorkflowMetadata
// → span.ComponentID/WorkflowID 真实填充。曾因 producer snake 键与 switch Camel 键失配，
// 元数据全落 default 被忽略（8.11 实施后审查发现并修正）。
func TestTracerWorkflowUtils_TraceComponentBegin_metadata落span字段(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}

	utils.TraceComponentBegin(context.Background(), session, nil, nil)

	span := session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Fatal("TraceComponentBegin 未创建 workflow span")
	}
	if span.ComponentID != session.NodeID() {
		t.Errorf("span.ComponentID = %q, 期望 %q（metadata 键名失配会落空）", span.ComponentID, session.NodeID())
	}
	if span.ComponentType != session.NodeType() {
		t.Errorf("span.ComponentType = %q, 期望 %q", span.ComponentType, session.NodeType())
	}
	if span.WorkflowID != session.WorkflowID() {
		t.Errorf("span.WorkflowID = %q, 期望 %q", span.WorkflowID, session.WorkflowID())
	}
}

// TestTracerWorkflowUtils_TraceComponentInputs 测试追踪组件输入
func TestTracerWorkflowUtils_TraceComponentInputs(t *testing.T) {
	session := newFakeSession()
	utils := TracerWorkflowUtils{}
	inputs := map[string]any{"prompt": "test"}

	// 先触发 begin 以创建 span
	utils.TraceComponentBegin(context.Background(), session, nil, nil)
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
	utils.TraceComponentBegin(context.Background(), session, nil, nil)
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
	utils.TraceComponentBegin(context.Background(), session, nil, nil)

	// 验证 span 存在
	span := session.Tracer().GetWorkflowSpan(session.ExecutableID(), session.ParentID())
	if span == nil {
		t.Fatal("TraceComponentBegin 后 span 不应为 nil")
	}

	utils.TraceComponentDone(context.Background(), session)

	// 当前循环组件（8.20）未实现，loop_id 为空，
	// Python: 非循环组件不执行 PopWorkflowSpan，span 仍保留在缓存中。
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
	utils.TraceComponentBegin(context.Background(), session, nil, nil)
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
	utils.TraceComponentBegin(context.Background(), session, nil, nil)
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
	utils.TraceComponentBegin(context.Background(), session, nil, nil)
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
	utils.TraceComponentBegin(context.Background(), session, nil, nil)

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
	utils.TraceComponentBegin(context.Background(), session, nil, nil)

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
	utils.TraceComponentBegin(context.Background(), session, nil, nil)
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

	if metadata["ComponentID"] != "node-001" {
		t.Errorf("component_id = %v, 期望 node-001", metadata["ComponentID"])
	}
	if metadata["ComponentName"] != "node-001" {
		t.Errorf("component_name = %v, 期望 node-001", metadata["ComponentName"])
	}
	if metadata["ComponentType"] != "LLM" {
		t.Errorf("component_type = %v, 期望 LLM", metadata["ComponentType"])
	}
	if metadata["WorkflowID"] != "wf-001" {
		t.Errorf("workflow_id = %v, 期望 wf-001", metadata["WorkflowID"])
	}
	// 不在循环中，不应有 LoopNodeID/LoopIndex
	if _, ok := metadata["LoopNodeID"]; ok {
		t.Error("LoopNodeID 不应存在")
	}
	if _, ok := metadata["LoopIndex"]; ok {
		t.Error("LoopIndex 不应存在")
	}
}

// TestGetComponentMetadata_有循环信息 测试在循环中时额外返回 LoopNodeID/LoopIndex
// Python: loop_id = state.get_global(LOOP_ID); index = state.get_global(loop_id + "." + "index")
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

	if metadata["LoopNodeID"] != "loop_node_1" {
		t.Errorf("loop_node_id = %v, 期望 loop_node_1", metadata["LoopNodeID"])
	}
	if metadata["LoopIndex"] != 2 {
		t.Errorf("loop_index = %v, 期望 2", metadata["LoopIndex"])
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

	if metadata["ComponentID"] != "node-001" {
		t.Errorf("component_id = %v, 期望 node-001", metadata["ComponentID"])
	}
	if _, ok := metadata["LoopNodeID"]; ok {
		t.Error("LoopNodeID 不应存在")
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

	if _, ok := metadata["LoopNodeID"]; ok {
		t.Error("loop_node_id 不应存在（空字符串等同于无循环）")
	}
}

// TestGetWorkflowMetadata_card提取 WorkflowConfig 实现后提取 card.version/name（Python workflow.py:261-264）
func TestGetWorkflowMetadata_card提取(t *testing.T) {
	cfg := config.NewSessionConfig(context.Background())
	wfc, _ := workflow.NewWorkflowConfig(&workflow.WorkflowCard{Version: "v2"})
	wfc.Card.Name = "主流程"
	cfg.AddWorkflowConfig("wf1", wfc)
	sess := &fakeWorkflowSession{workflowID: "wf1", config: cfg}

	md := getWorkflowMetadata(sess)
	if md["WorkflowVersion"] != "v2" || md["WorkflowName"] != "主流程" {
		t.Fatalf("应提取 card 字段: %v", md)
	}
}

// TestGetWorkflowMetadata_无config兜底 无配置时空串兜底
func TestGetWorkflowMetadata_无config兜底(t *testing.T) {
	sess := &fakeWorkflowSession{workflowID: "wf1",
		config: config.NewSessionConfig(context.Background())}
	md := getWorkflowMetadata(sess)
	if md["WorkflowVersion"] != "" || md["WorkflowName"] != "" {
		t.Fatalf("无配置应空串兜底: %v", md)
	}
}
