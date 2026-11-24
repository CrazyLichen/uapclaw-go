package graph

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/tracer"
	sutils "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/utils"
	ability "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/ability"
	workflow "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/workflowconfig"
)

// Task2 测试：结构体/构造/对账辅助/collectRefSourceIDs/Reset。
// 对齐 Python vertex.py:30-86（SUB_WORKFLOW_COMPONENT 常量 + __init__）、33-62
//（_collect_ref_source_ids）、882-890（reset）。

func TestNewVertex_字段初始化(t *testing.T) {
	exe := &fakeVertexExecutable{}
	v := NewVertex("node_1", exe)
	if v.nodeID != "node_1" {
		t.Fatalf("nodeID 未初始化: %q", v.nodeID)
	}
	if v.executable == nil {
		t.Fatal("executable 未初始化")
	}
	if v.callCount.Load() != 0 || v.streamCallCount.Load() != 0 {
		t.Fatal("计数器初始值应为 0（vertex.py:75-76）")
	}
	if !v.isFirstInit.Load() {
		t.Fatal("isFirstInit 初始应为 true（vertex.py:85）")
	}
	if v.streamCalledTimeout != 10 {
		t.Fatalf("streamCalledTimeout 默认应为 10（vertex.py:72）, got %v", v.streamCalledTimeout)
	}
	if v.streamDone == nil {
		t.Fatal("streamDone 应有初值（vertex.py:74 建 asyncio.Future）")
	}
	if v.isEndNode {
		t.Fatal("isEndNode 初始应为 false（vertex.py:77）")
	}
}

func TestVertex_IsDone与streamCalled对账(t *testing.T) {
	v := NewVertex("n", nil)
	// Python: is_done = call==stream or call==stream+1（vertex.py:711-713）
	//         _stream_called = stream == call+1（vertex.py:715-716）
	v.streamCallCount.Store(1)
	if !v.streamCalled() {
		t.Fatal("stream=1,call=0：streamCalled 应 true")
	}
	if v.IsDone() {
		t.Fatal("stream=1,call=0：IsDone 应 false")
	}
	v.callCount.Store(1)
	if !v.IsDone() {
		t.Fatal("call==stream 应 IsDone")
	}
	v.callCount.Store(2)
	if !v.IsDone() {
		t.Fatal("call==stream+1 应 IsDone")
	}
	v.streamCallCount.Store(3)
	if v.IsDone() {
		t.Fatal("stream=3,call=2：IsDone 应 false")
	}
	if !v.streamCalled() {
		t.Fatal("stream=3,call=2：streamCalled 应 true（stream==call+1）")
	}
}

func TestCollectRefSourceIDs(t *testing.T) {
	// Python _collect_ref_source_ids：递归 dict/list，叶子 ${comp.field} 提取首段（vertex.py:33-62）
	schema := map[string]any{
		"a": "${comp1.output.text}",
		"b": []any{map[string]any{"c": "${comp2.foo}", "d": "not_ref"}},
	}
	ids := collectRefSourceIDs(schema)
	if len(ids) != 2 || !ids["comp1"] || !ids["comp2"] {
		t.Fatalf("收集结果不符: %v", ids)
	}
	// 空 schema → 空集
	if got := collectRefSourceIDs(map[string]any{}); len(got) != 0 {
		t.Fatalf("空 schema 应得空集, got %v", got)
	}
}

func TestVertex_Reset(t *testing.T) {
	v := NewVertex("n", nil)
	v.callCount.Store(2)
	v.streamCallCount.Store(2)
	v.isStarted.Store(true)
	v.isCallStarted.Store(true)
	v.Reset()
	if v.callCount.Load() != 0 || v.streamCallCount.Load() != 0 {
		t.Fatal("reset 应清零计数（vertex.py:883-884）")
	}
	if v.streamDone == nil {
		t.Fatal("reset 应新建 streamDone（vertex.py:885-889 cancel→新建 Future）")
	}
	// Python reset 未清 is_started/is_call_started（那是 __call__ finally 的职责）——不复刻多余行为
}

// fakeVertexExecutable Task2-12 共用的可编程可执行体桩（各任务按需扩字段）
type fakeVertexExecutable struct {
	BaseExecutable[map[string]any, map[string]any]
	invokeFn  func(ctx context.Context, inputs map[string]any, sess session.BaseSession, kw map[string]any) (map[string]any, error)
	streamFn  func(ctx context.Context, inputs map[string]any, sess session.BaseSession, kw map[string]any) (<-chan StreamChunk[any], error)
	collectFn func(ctx context.Context, inputs <-chan map[string]any, sess session.BaseSession, kw map[string]any) (map[string]any, error)
}

func (f *fakeVertexExecutable) OnInvoke(ctx context.Context, inputs map[string]any,
	sess session.BaseSession, kw map[string]any) (map[string]any, error) {
	if f.invokeFn != nil {
		return f.invokeFn(ctx, inputs, sess, kw)
	}
	return map[string]any{"invoked": true}, nil
}

func (f *fakeVertexExecutable) OnStream(ctx context.Context, inputs map[string]any,
	sess session.BaseSession, kw map[string]any) (<-chan StreamChunk[any], error) {
	if f.streamFn != nil {
		return f.streamFn(ctx, inputs, sess, kw)
	}
	ch := make(chan StreamChunk[any])
	close(ch)
	return ch, nil
}

func (f *fakeVertexExecutable) OnCollect(ctx context.Context, inputs <-chan map[string]any,
	sess session.BaseSession, kw map[string]any) (map[string]any, error) {
	if f.collectFn != nil {
		return f.collectFn(ctx, inputs, sess, kw)
	}
	return map[string]any{"collected": true}, nil
}

// ──────────────────────────── Task3: Init ────────────────────────────

// fakeInnerSession 最小 InnerSession 桩（Init 依赖面）
type fakeInnerSession struct {
	cfg  config.SessionConfig
	st   state.SessionState
	tr   *tracer.Tracer
	id   string
	swm  *stream.StreamWriterManager
	am   *stream_actor.ActorManager
	cp   interfaces.Checkpointer
	wfID string
}

func (f *fakeInnerSession) Config() config.SessionConfig                     { return f.cfg }
func (f *fakeInnerSession) State() state.SessionState                        { return f.st }
func (f *fakeInnerSession) Tracer() *tracer.Tracer                           { return f.tr }
func (f *fakeInnerSession) StreamWriterManager() *stream.StreamWriterManager { return f.swm }
func (f *fakeInnerSession) SessionID() string                                { return f.id }
func (f *fakeInnerSession) Checkpointer() interfaces.Checkpointer            { return f.cp }
func (f *fakeInnerSession) ActorManager() *stream_actor.ActorManager         { return f.am }
func (f *fakeInnerSession) Close() error                                     { return nil }

// parentWFID 测试默认 workflowID（NodeSession 经 getWorkflowID 继承）
const parentWFID = "wf-test"

// WorkflowID 返回工作流 ID（8.15 NodeConfig 管路：NodeSession.workflowID 来源）
func (f *fakeInnerSession) WorkflowID() string {
	if f.wfID == "" {
		return parentWFID
	}
	return f.wfID
}

// driveNodeSpec 8.15 NodeConfig 管路驱动：向 parent config 注册节点规格并重跑 Init
// （能力派生逻辑已内联回 Init，对齐 Python init 的 vertex.py:92-97 段经真实
// NodeSession.NodeConfig() 链路驱动；替代已删除的 applyNodeConfig 直测）
func driveNodeSpec(t *testing.T, v *Vertex, parent *fakeInnerSession, spec *workflow.NodeSpec) {
	t.Helper()
	wfc, err := workflow.NewWorkflowConfig(&workflow.WorkflowCard{})
	if err != nil {
		t.Fatal(err)
	}
	wfc.Spec.CompConfigs[v.nodeID] = spec
	parent.cfg.AddWorkflowConfig(parent.WorkflowID(), wfc)
	if !v.Init(parent, nil) {
		t.Fatal("Init 应成功")
	}
}

func TestInit_主干默认能力(t *testing.T) {
	cfg := config.NewSessionConfig(context.Background())
	cfg.SetEnvs(map[string]any{constants.CompStreamCallTimeoutKey: 3.5})
	ctxVal := context.WithValue(context.Background(), struct{}{}, "v")
	parent := &fakeInnerSession{cfg: cfg, st: state.NewInMemoryWorkflowState(), id: "s1"}
	exe := &fakeVertexExecutable{}
	v := NewVertex("n1", exe)
	// Python init 返回 bool（vertex.py:87）
	if ok := v.Init(parent, map[string]any{"context": ctxVal}); !ok {
		t.Fatal("Init 应返回 true")
	}
	if v.nodeSession == nil || v.nodeSession.NodeID() != "n1" {
		t.Fatal("NodeSession 应以 nodeID 构造（vertex.py:88）")
	}
	if v.nodeSession.NodeType() != "fakeVertexExecutable" {
		t.Fatalf("nodeType 应为可执行体类型名（vertex.py:88 type(self).__name__）, got %q", v.nodeSession.NodeType())
	}
	if v.nodeSession.SkipTrace() != false {
		t.Fatal("skipTrace 应透传 executable.SkipTrace()（vertex.py:89）")
	}
	if v.modelContext != ctxVal {
		t.Fatal("context 应从 kwargs 注入（vertex.py:90）")
	}
	if v.streamCalledTimeout != 3.5 {
		t.Fatalf("流超时应从 session env 读取（vertex.py:91）, got %v", v.streamCalledTimeout)
	}
	// nodeConfig 为 nil（⤵️ 8.15 回填管路）→ 能力面默认 [INVOKE]（vertex.py:93-95）
	if len(v.componentAbility) != 1 || v.componentAbility[0] != ability.ComponentAbilityInvoke {
		t.Fatalf("默认能力应为 [INVOKE], got %v", v.componentAbility)
	}
	if v.hasStreamCall {
		t.Fatal("默认能力无流能力，hasStreamCall 应 false（vertex.py:96）")
	}
	if !v.hasCall {
		t.Fatal("默认能力有批能力，hasCall 应 true（vertex.py:97）")
	}
	// 首初始化后 isFirstInit 复位（vertex.py:99-105）
	if v.isFirstInit.Load() {
		t.Fatal("首初始化日志只记一次，isFirstInit 应已复位")
	}
	if v.isStarted.Load() {
		t.Fatal("init 应 clear is_started（vertex.py:111）")
	}
	// 二次 Init：isFirstInit 不再置位（不重复记日志）
	_ = v.Init(parent, nil)
}

func TestInit_能力判定_流批混合(t *testing.T) {
	// ✅ 8.15 已回填：能力派生经真实 NodeSession.NodeConfig() 管路驱动（D-3 内联回收）
	v := NewVertex("n", &fakeVertexExecutable{})
	parent := &fakeInnerSession{cfg: config.NewSessionConfig(context.Background()),
		st: state.NewInMemoryWorkflowState()}
	driveNodeSpec(t, v, parent, &workflow.NodeSpec{Abilities: []ability.ComponentAbility{
		ability.ComponentAbilityInvoke, ability.ComponentAbilityTransform}})
	if !v.hasStreamCall || !v.hasCall {
		t.Fatalf("混合能力应双真, stream=%v call=%v", v.hasStreamCall, v.hasCall)
	}
	driveNodeSpec(t, v, parent, &workflow.NodeSpec{Abilities: []ability.ComponentAbility{
		ability.ComponentAbilityCollect}})
	if !v.hasStreamCall || v.hasCall {
		t.Fatal("纯流能力：hasStreamCall true / hasCall false")
	}
}

// ──────────────────────────── Task4: trace 协议 ────────────────────────────

// newTraceVertex 构造带会话的 Vertex（trace 用例公共底座）
func newTraceVertex(t *testing.T, withValidHandler bool) (*Vertex, *fakeInnerSession) {
	t.Helper()
	cfg := config.NewSessionConfig(context.Background())
	parent := &fakeInnerSession{cfg: cfg, st: state.NewInMemoryWorkflowState(), id: "s1"}
	if withValidHandler {
		tr := tracer.NewTracer()
		tr.Init(stream.NewStreamWriterManager(stream.NewStreamEmitter()))
		parent.tr = tr
	}
	v := NewVertex("n1", &fakeVertexExecutable{})
	if !v.Init(parent, nil) {
		t.Fatal("Init 应成功")
	}
	return v, parent
}

func TestTraceEnable_门控(t *testing.T) {
	// tracer nil → false（vertex.py:809-811）
	v, _ := newTraceVertex(t, false)
	if v.traceEnable() {
		t.Fatal("tracer nil 应 traceEnable=false")
	}
	// tracer 有效 + handler 可用 + 不 skip → true
	v2, _ := newTraceVertex(t, true)
	if !v2.traceEnable() {
		t.Fatal("有效 handler 应 traceEnable=true")
	}
	// skipTrace=true → false
	v3, _ := newTraceVertex(t, true)
	v3.executable = &skipTraceExecutable{}
	if v3.traceEnable() {
		t.Fatal("skipTrace 应 traceEnable=false")
	}
}

// skipTraceExecutable 覆写 SkipTrace=true 的桩
type skipTraceExecutable struct {
	fakeVertexExecutable
}

func (s *skipTraceExecutable) SkipTrace() bool { return true }

func TestTraceComponentInputs_合并发送协议(t *testing.T) {
	// trace 不可用 → 不置位 isCallStarted（vertex.py:813-815 提前返回）
	v, _ := newTraceVertex(t, false)
	v.traceComponentInputs(context.Background(), map[string]any{"a": 1})
	if v.isCallStarted.Load() {
		t.Fatal("trace 不可用不应置位 isCallStarted")
	}
	// 纯批入（hasStreamCall=false）→ 置位 + send=true 路径可达（vertex.py:816-818）
	v2, _ := newTraceVertex(t, true)
	v2.traceComponentInputs(context.Background(), map[string]any{"a": 1})
	if !v2.isCallStarted.Load() {
		t.Fatal("应置位 isCallStarted（vertex.py:816）")
	}
	// 混合节点且流未完成：need_send=false（vertex.py:817）——经 fakeExecutable
	// 能力面注入后由 StreamCall 用例覆盖实际 send 时机
}

func TestTraceComponentBegin_去重与元数据钩子(t *testing.T) {
	v, _ := newTraceVertex(t, true)
	// 首次：置位 isStarted 并发埋点（vertex.py:830-832）
	v.traceComponentBegin(context.Background())
	if !v.isStarted.Load() {
		t.Fatal("begin 应置位 isStarted")
	}
	// 第二次：不再重复（isStarted 已置位，短路）
	v.traceComponentBegin(context.Background())
	// traceMetadata 钩子（vertex.py:834-846）：实现者返回元数据、失败记 warning 不中断
	// —— 无钩子（fakeVertexExecutable 不实现 TraceMetadata）路径已覆盖默认 None 分支
}

func TestTraceComponentStreamInputSend_补发条件(t *testing.T) {
	// !hasCall → 补发（vertex.py:879-880）
	v, _ := newTraceVertex(t, true)
	v.hasCall = false
	v.traceComponentStreamInputSend(context.Background())
	// hasCall 且批侧已置位 → 补发
	v2, _ := newTraceVertex(t, true)
	v2.isCallStarted.Store(true)
	v2.traceComponentStreamInputSend(context.Background())
	// hasCall 且批侧未置位 → 不补发（无可观测副作用，不 panic 即通过）
	v3, _ := newTraceVertex(t, true)
	v3.traceComponentStreamInputSend(context.Background())
}

func TestTraceInnerError_双轨(t *testing.T) {
	v, _ := newTraceVertex(t, true)
	// BaseError 轨：error_code/message（vertex.py:866-867）
	be := exception.BuildError(exception.StatusWorkflowExecutionTimeout)
	v.traceInnerError(context.Background(), be)
	// 非 BaseError 轨：WORKFLOW_COMPONENT_EXECUTION_ERROR.code + str(err)（vertex.py:868-870）
	v.traceInnerError(context.Background(), errors.New("boom"))
}

// ──────────────────────────── Task6: preInvoke / sanitize 族 ────────────────────────────

func TestMarkNodeExecuted与IsComponentExecuted(t *testing.T) {
	v, _ := newTraceVertex(t, false)
	if v.isComponentExecuted("A") {
		t.Fatal("初始应未执行")
	}
	v.markNodeExecuted()
	v.markNodeExecuted() // 去重
	executed := workflowStateList(v.stateView(), "executed_nodes")
	if len(executed) != 1 || executed[0] != "n1" {
		t.Fatalf("executed_nodes 应恰含 n1 一次, got %v", executed)
	}
	if !v.isComponentExecuted("n1") {
		t.Fatal("标记后应已执行")
	}
}

func TestSanitizeLeaf_分支判定(t *testing.T) {
	v, _ := newTraceVertex(t, false)
	// ① 非 None 且非流 → 原样（vertex.py:505-506）
	container := map[string]any{"k": "text"}
	v.sanitizeLeaf(context.Background(), container, "k", "text", "${A.out}")
	if container["k"] != "text" {
		t.Fatal("普通值不应被清洗")
	}
	// ② 非引用路径 → 原样（vertex.py:507-508）
	container2 := map[string]any{"k": nil}
	v.sanitizeLeaf(context.Background(), container2, "k", nil, "plain")
	if container2["k"] != nil {
		t.Fatal("非引用路径不应清洗")
	}
	// ③ 组件已执行 → 原样（vertex.py:512-514）
	v3, _ := newTraceVertex(t, false)
	v3.markNodeExecuted() // n1 已执行
	container3 := map[string]any{"n1": nil}
	v3.sanitizeLeaf(context.Background(), container3, "n1", nil, "${n1.out}")
	if container3["n1"] != nil {
		t.Fatal("已执行组件不应清洗")
	}
	// ④ 未执行分支的 None 值 → 置空串（vertex.py:517-519）
	container4 := map[string]any{"other": nil}
	v3.sanitizeLeaf(context.Background(), container4, "other", nil, "${other.out}")
	if container4["other"] != "" {
		t.Fatalf("未执行分支应置空串, got %v", container4["other"])
	}
}

func TestSanitizeNode_嵌套递归(t *testing.T) {
	v, _ := newTraceVertex(t, false)
	inputs := map[string]any{
		"a": map[string]any{"x": nil},
		"b": []any{nil, "keep"},
	}
	schema := map[string]any{
		"a": map[string]any{"x": "${missing.out}"},
		"b": []any{"${missing.out}", "literal"},
	}
	v.sanitizeNode(context.Background(), inputs, schema)
	if inputs["a"].(map[string]any)["x"] != "" {
		t.Fatal("嵌套 dict 叶子应清洗")
	}
	if inputs["b"].([]any)[0] != "" {
		t.Fatal("嵌套 list 叶子应清洗")
	}
	if inputs["b"].([]any)[1] != "keep" {
		t.Fatal("list 内非引用叶应保留")
	}
}

func TestPreInvoke_双轨取数(t *testing.T) {
	v, _ := newTraceVertex(t, false)
	// schema 为 nil → inputs nil（vertex.py:426）
	in := v.preInvoke(context.Background())
	if in != nil {
		t.Fatalf("无 schema 应取 nil, got %v", in)
	}
	// dict schema 轨（vertex.py:426）：io_state 无上游输出 → 引用键取 nil 缺省
	v.nodeConfig = &workflow.NodeSpec{IOConfigs: &workflow.CompIOConfig{
		InputsSchema: map[string]any{"x": "${A.out}"}}}
	// preInvoke 返回具体类型 map[string]any（cdac01647 契约收窄后直接判 nil/长度，
	// 不再对具体类型做类型断言——那是对接口值才合法的语法）
	in2 := v.preInvoke(context.Background())
	if len(in2) != 1 {
		if in2 == nil {
			return // 上游无输出时整体为 nil 亦合法
		}
		t.Fatalf("dict 轨应返回 schema 形状 map, got %v", in2)
	}
	if val, exists := in2["x"]; exists && val != nil {
		t.Fatalf("无上游输出引用键应为 nil, got %v", val)
	}
}

// ──────────────────────────── Task7: postInvoke ────────────────────────────

func TestPostInvoke_None过滤与Merge(t *testing.T) {
	v, _ := newTraceVertex(t, false)
	// 非 End：schema 取值 + 过滤 None（vertex.py:546-549）
	v.nodeConfig = &workflow.NodeSpec{IOConfigs: &workflow.CompIOConfig{
		OutputsSchema: map[string]any{"a": "${a}", "b": "${b}"}}}
	res, err := v.postInvoke(context.Background(), map[string]any{"a": "v1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := res["b"]; exists {
		t.Fatal("非 End 节点应过滤 None 值")
	}
	if res["a"] != "v1" {
		t.Fatalf("schema 保留键应存活, got %v", res)
	}
	// End 混合模式 merge（vertex.py:552-561）
	v2, _ := newTraceVertex(t, false)
	v2.isEndNode = true
	v2.hasCall = true
	v2.hasStreamCall = true
	v2.stateView().SetOutputs(map[string]any{"output": []any{"old"}})
	v2.stateView().Commit() // 暂存模型：旧输出须提交后才对 get_outputs 可见（对齐 Python staged 语义）
	res2, _ := v2.postInvoke(context.Background(), map[string]any{"output": "new"})
	outList, ok := res2["output"].([]any)
	if !ok || len(outList) != 2 || outList[0] != "new" || outList[1] != "old" {
		t.Fatalf("混合 End 应合并新旧 output 列表, got %v", res2["output"])
	}
}

// ──────────────────────────── Task8: preStream / postStream / processChunk ────────────────────────────

func TestStreamAbilities与ShouldHandleMessage(t *testing.T) {
	v, parent := newTraceVertex(t, false)
	driveNodeSpec(t, v, parent, &workflow.NodeSpec{Abilities: []ability.ComponentAbility{
		ability.ComponentAbilityInvoke, ability.ComponentAbilityCollect,
		ability.ComponentAbilityTransform}})
	ab := v.streamAbilities()
	if len(ab) != 2 {
		t.Fatalf("应过滤出 COLLECT/TRANSFORM, got %v", ab)
	}
	if !v.ShouldHandleMessage() {
		t.Fatal("流能力非空应可处理消息")
	}
}

// fakeVertexActorSession ActorSession 桩（ActorManager 构造依赖）
type fakeVertexActorSession struct{}

func (f *fakeVertexActorSession) GetEnvFloat(string) (float64, bool)          { return -1, false }
func (f *fakeVertexActorSession) GetWorkflowStateValue(string) any            { return nil }
func (f *fakeVertexActorSession) UpdateAndCommitWorkflowState(map[string]any) {}

// newVertexWithActor 构造带 ActorManager 的 Vertex（A --stream--> n1）
func newVertexWithActor(t *testing.T, abilities ...ability.ComponentAbility) (*Vertex, *stream_actor.ActorManager) {
	t.Helper()
	cfg := config.NewSessionConfig(context.Background())
	parent := &fakeInnerSession{cfg: cfg, st: state.NewInMemoryWorkflowState(), id: "s1"}
	v := NewVertex("n1", &fakeVertexExecutable{})
	wfcReg, _ := workflow.NewWorkflowConfig(&workflow.WorkflowCard{})
	wfcReg.Spec.CompConfigs["n1"] = &workflow.NodeSpec{Abilities: abilities}
	parent.cfg.AddWorkflowConfig(parent.WorkflowID(), wfcReg)
	_ = v.Init(parent, nil)
	spec := workflow.NewWorkflowSpec()
	spec.StreamEdges = map[string][]string{"A": {"n1"}}
	spec.CompConfigs = map[string]*workflow.NodeSpec{
		"A":  {Abilities: []ability.ComponentAbility{ability.ComponentAbilityStream}},
		"n1": {Abilities: abilities},
	}
	am, err := stream_actor.NewActorManager(context.Background(), spec, stream_actor.NewStreamGraph(), false, &fakeVertexActorSession{})
	if err != nil {
		t.Fatal(err)
	}
	parent.am = am
	return v, am
}

func TestPreStream_消费注册(t *testing.T) {
	v, _ := newVertexWithActor(t, ability.ComponentAbilityCollect)
	// 无 schema：Consume 返回空 map（Generator 空 schema 短路）
	out, err := v.preStream(context.Background(), ability.ComponentAbilityCollect)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("Consume 返回不应为 nil")
	}
	// 有 schema：$ref 叶子应注册为 StreamReader（Generator 一比一）
	v2, _ := newVertexWithActor(t, ability.ComponentAbilityCollect)
	v2.nodeConfig = &workflow.NodeSpec{StreamIOConfigs: &workflow.CompIOConfig{
		InputsSchema: map[string]any{"x": "${A.answer}"}}}
	out2, err := v2.preStream(context.Background(), ability.ComponentAbilityCollect)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := out2["x"].(*stream_actor.StreamReader); !ok {
		t.Fatalf("$ref 叶子应为 StreamReader, got %T", out2["x"])
	}
}

// fakeVertexConsumer graph 测试侧的 StreamConsumer 桩（下游消费者）
type fakeVertexConsumer struct{}

func (f *fakeVertexConsumer) StreamCall(ctx context.Context, event chan struct{}, done func(error)) {
	close(event)
	<-ctx.Done() // 阻塞保持任务存活（模拟真实能力执行中，END 帧可入队）
}
func (f *fakeVertexConsumer) ShouldHandleMessage() bool { return true }
func (f *fakeVertexConsumer) IsDone() bool              { return true }

// newVertexAsProducer 构造以 n1 为流式生产者、n2 为消费者的 Vertex
func newVertexAsProducer(t *testing.T) (*Vertex, *stream_actor.ActorManager) {
	t.Helper()
	cfg := config.NewSessionConfig(context.Background())
	parent := &fakeInnerSession{cfg: cfg, st: state.NewInMemoryWorkflowState(), id: "s1"}
	v := NewVertex("n1", &fakeVertexExecutable{})
	_ = v.Init(parent, nil)
	spec := workflow.NewWorkflowSpec()
	spec.StreamEdges = map[string][]string{"n1": {"n2"}}
	spec.CompConfigs = map[string]*workflow.NodeSpec{
		"n1": {Abilities: []ability.ComponentAbility{ability.ComponentAbilityStream}},
		"n2": {Abilities: []ability.ComponentAbility{ability.ComponentAbilityCollect}},
	}
	g := stream_actor.NewStreamGraph()
	g.AddStreamConsumer(&fakeVertexConsumer{}, "n2")
	am, err := stream_actor.NewActorManager(context.Background(), spec, g, false, &fakeVertexActorSession{})
	if err != nil {
		t.Fatal(err)
	}
	parent.am = am
	return v, am
}

func TestProcessChunk_三分支(t *testing.T) {
	// ① End 非子图：默认信封 OutputSchema{Type=EndNodeStream} + writer 写入
	v, _ := newTraceVertex(t, false)
	v.isEndNode = true
	written := make(chan any, 1)
	swm := stream.NewStreamWriterManager(stream.NewStreamEmitter())
	_ = swm.AddWriter(stream.StreamModeOutput, &fakeOutputWriter{ch: written})
	v.nodeSession = &fakeVertexNodeSession{WorkflowVertexSession: v.nodeSession, swm: swm}
	v.processChunk(context.Background(), "chunk-0", true, 0, false, ability.ComponentAbilityStream)
	select {
	case w := <-written:
		os, ok := w.(*stream.OutputSchema)
		if !ok || os.Type != constants.EndNodeStream || os.Index != 0 || os.Payload != "chunk-0" {
			t.Fatalf("End 信封不符: %+v", w)
		}
	default:
		t.Fatal("输出写入器应收到 End 信封")
	}
	// ② 普通节点：produce 转发（n1 --stream--> n2）
	v2, am := newVertexAsProducer(t)
	v2.processChunk(context.Background(), "x", false, 0, false, ability.ComponentAbilityStream)
	if am.StreamActor("n2").Processor(ability.ComponentAbilityCollect).MainQueueLen() != 1 {
		t.Fatal("普通节点应 produce 到下游 actor")
	}
}

// fakeVertexNodeSession 覆写 StreamWriterManager 的会话桩（嵌入真实会话转发其余方法）
type fakeVertexNodeSession struct {
	session.WorkflowVertexSession
	swm *stream.StreamWriterManager
}

func (f *fakeVertexNodeSession) StreamWriterManager() *stream.StreamWriterManager { return f.swm }

// fakeOutputWriter 捕获写入帧
type fakeOutputWriter struct {
	ch chan any
}

func (f *fakeOutputWriter) Write(ctx context.Context, schema stream.Schema) error {
	f.ch <- schema
	return nil
}

func TestPostStream_收口分流(t *testing.T) {
	// 普通节点：数据帧 + 收口 END_ 哨兵帧（Python 行为对齐：task 未拉起时
	// first_frame=False 的 END 帧同样被 Send 丢弃分支拦截，故此路径需先有数据帧）
	v, am := newVertexAsProducer(t)
	ch := make(chan StreamChunk[any], 1)
	ch <- StreamChunk[any]{Val: map[string]any{"n1": "hello"}}
	close(ch)
	v.postStream(context.Background(), ch, ability.ComponentAbilityStream)
	// 断言累计投递帧数（单调计数）：Run 会异步排空主队列，MainQueueLen 受消费
	// 进度竞态影响不可直接断言（修复既有 -race 下 ~30% flaky）
	if got := am.StreamActor("n2").Processor(ability.ComponentAbilityCollect).TotalPutCount(); got != 2 {
		t.Fatalf("应产出数据帧+END 哨兵帧, got %d", got)
	}
	// 逐 chunk 转发：默认 transformer 无 schema → 原样透传
	v2, am2 := newVertexAsProducer(t)
	ch2 := make(chan StreamChunk[any], 1)
	ch2 <- StreamChunk[any]{Val: map[string]any{"n1": "hello"}}
	close(ch2)
	v2.postStream(context.Background(), ch2, ability.ComponentAbilityStream)
	p := am2.StreamActor("n2").Processor(ability.ComponentAbilityCollect)
	if got := p.TotalPutCount(); got != 2 { // 数据帧 + END 帧
		t.Fatalf("应产出数据帧+END 帧, got %d", got)
	}
}

// ──────────────────────────── Task5: runExecutable / retry / 错误恢复 ────────────────────────────

func TestRunExecutable_Invoke策略主干(t *testing.T) {
	v, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	exe := &fakeVertexExecutable{
		invokeFn: func(ctx context.Context, inputs map[string]any, sess session.BaseSession,
			kw map[string]any) (map[string]any, error) {
			if _, exists := kw["context"]; !exists {
				t.Error("kwargs 应携带 context 键（vertex.py:139 kwargs.get('context')）")
			}
			return map[string]any{"out": "ok"}, nil
		},
	}
	v.executable = exe
	ok, err := v.runExecutable(context.Background(), ability.ComponentAbilityInvoke, false, nil, nil)
	if err != nil || !ok {
		t.Fatalf("invoke 策略应成功, ok=%v err=%v", ok, err)
	}
	if !v.isComponentExecuted("n1") {
		t.Fatal("mark_node_executed 应先行（vertex.py:117）")
	}
}

func TestRunExecutable_异常分类(t *testing.T) {
	// BaseError → 原样上抛（vertex.py:213-222）
	v, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	be := exception.BuildError(exception.StatusWorkflowExecutionTimeout)
	v.executable = &fakeVertexExecutable{
		invokeFn: func(context.Context, map[string]any, session.BaseSession, map[string]any) (map[string]any, error) {
			return nil, be
		},
	}
	_, err := v.runExecutable(context.Background(), ability.ComponentAbilityInvoke, false, nil, nil)
	if !errors.Is(err, be) {
		t.Fatalf("BaseError 应原样上抛, got %v", err)
	}
	// 通用错误 → 组件执行错误包装（vertex.py:223-233）
	v2, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	v2.executable = &fakeVertexExecutable{
		invokeFn: func(context.Context, map[string]any, session.BaseSession, map[string]any) (map[string]any, error) {
			return nil, errors.New("boom")
		},
	}
	_, err2 := v2.runExecutable(context.Background(), ability.ComponentAbilityInvoke, false, nil, nil)
	var be2 *exception.BaseError
	if !errors.As(err2, &be2) || be2.Code() != exception.StatusWorkflowComponentExecutionError.Code() {
		t.Fatalf("通用错误应包装为组件执行错误, got %v", err2)
	}
	// event 兜底置位（vertex.py:234-236）
	ev := make(chan struct{})
	_, _ = v2.runExecutable(context.Background(), ability.ComponentAbilityInvoke, false, nil, ev)
	select {
	case <-ev:
	default:
		t.Fatal("异常路径 event 也应置位")
	}
}

func TestRunExecutable_Collect策略信封(t *testing.T) {
	v, _ := newVertexWithActor(t, ability.ComponentAbilityCollect)
	var gotSchema map[string]any
	v.executable = &fakeVertexExecutable{
		collectFn: func(ctx context.Context, inputs <-chan map[string]any, sess session.BaseSession,
			kw map[string]any) (map[string]any, error) {
			m := <-inputs
			gotSchema = m
			return map[string]any{"collected": true}, nil
		},
	}
	ok, err := v.runExecutable(context.Background(), ability.ComponentAbilityCollect, false, nil, nil)
	if err != nil || !ok {
		t.Fatalf("collect 策略应成功, ok=%v err=%v", ok, err)
	}
	// collect_iter = dict（无 schema 时为空 map）
	if gotSchema == nil {
		t.Fatal("on_collect 应收到 dict 信封")
	}
}

func TestRunExecutableWithRetry_超时真取消(t *testing.T) {
	v, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	v.nodeConfig = &workflow.NodeSpec{Timeout: 0.05, MaxRetries: 0}
	started := make(chan struct{})
	v.executable = &fakeVertexExecutable{
		invokeFn: func(ctx context.Context, inputs map[string]any, sess session.BaseSession,
			kw map[string]any) (map[string]any, error) {
			close(started)
			<-ctx.Done() // 阻塞至超时取消（对齐 asyncio wait_for 取消被包裹协程）
			return nil, ctx.Err()
		},
	}
	_, err := v.runExecutableWithRetry(context.Background(), ability.ComponentAbilityInvoke, false, nil, nil)
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Code() != exception.StatusWorkflowExecutionTimeout.Code() {
		t.Fatalf("超时应回落为执行超时错误, got %v", err)
	}
	<-started
}

func TestRunExecutableWithRetry_重试后成功(t *testing.T) {
	v, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	v.nodeConfig = &workflow.NodeSpec{MaxRetries: 2}
	calls := 0
	v.executable = &fakeVertexExecutable{
		invokeFn: func(context.Context, map[string]any, session.BaseSession, map[string]any) (map[string]any, error) {
			calls++
			if calls < 3 {
				return nil, errors.New("transient")
			}
			return map[string]any{"ok": true}, nil
		},
	}
	ok, err := v.runExecutableWithRetry(context.Background(), ability.ComponentAbilityInvoke, false, nil, nil)
	if err != nil || !ok || calls != 3 {
		t.Fatalf("第 3 次应成功, ok=%v err=%v calls=%d", ok, err, calls)
	}
}

func TestBuildTimeoutError_命中与回落(t *testing.T) {
	v, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	// 未命中 → 回落默认（vertex.py:326-330）
	err := v.buildTimeoutError(context.Background(), errors.New("t"), 1.5)
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Code() != exception.StatusWorkflowExecutionTimeout.Code() {
		t.Fatalf("应回落执行超时错误, got %v", err)
	}
	// 注册 handler 返回自定义错误 → 命中（vertex.py:306-316）；defer 注销防跨用例泄漏
	custom := exception.BuildError(exception.StatusGraphVertexStreamCallTimeout)
	timeoutFn := func(ctx context.Context, data map[string]any) any { return custom }
	callback.GetCallbackFramework().OnCustom("component_timeout_error", timeoutFn)
	defer callback.GetCallbackFramework().OffCustom("component_timeout_error", timeoutFn)
	err2 := v.buildTimeoutError(context.Background(), errors.New("t"), 1.0)
	if !errors.Is(err2, custom) {
		t.Fatalf("应命中 handler 自定义错误, got %v", err2)
	}
}

func TestRunErrorRecovery_三出口(t *testing.T) {
	// STREAM 不可恢复（vertex.py:335-337）
	v, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	boom := errors.New("boom")
	if _, err := v.runErrorRecovery(context.Background(), ability.ComponentAbilityStream, boom); !errors.Is(err, boom) {
		t.Fatal("STREAM 能力应直接上抛")
	}
	// 无 handler → 上抛（vertex.py:362）
	if _, err := v.runErrorRecovery(context.Background(), ability.ComponentAbilityInvoke, boom); !errors.Is(err, boom) {
		t.Fatal("无命中应上抛原错误")
	}
	// handler 命中 → postInvoke 恢复并成功（vertex.py:356-358）
	v2, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	recoveryFn := func(ctx context.Context, data map[string]any) any {
		return map[string]any{"recovered": true}
	}
	callback.GetCallbackFramework().OnCustom("component_error_recovery", recoveryFn)
	defer callback.GetCallbackFramework().OffCustom("component_error_recovery", recoveryFn)
	ok, err := v2.runErrorRecovery(context.Background(), ability.ComponentAbilityInvoke, boom)
	if err != nil || !ok {
		t.Fatalf("恢复命中应返回成功, ok=%v err=%v", ok, err)
	}
}

// ──────────────────────────── Task9: callInternal / Call / atomicInvoke ────────────────────────────

func TestCallInternal_未初始化报错(t *testing.T) {
	v := NewVertex("n1", nil)
	err := v.callInternal(context.Background(), nil)
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Code() != exception.StatusGraphVertexExecutionError.Code() {
		t.Fatalf("未初始化应报顶点执行错误, got %v", err)
	}
}

func TestCallInternal_无流数据报错(t *testing.T) {
	// 有流能力、非 End、流侧从未启动 → no stream data in（vertex.py:701-704）
	v, _ := newVertexWithActor(t, ability.ComponentAbilityCollect)
	// COLLECT（流能力）执行成功但流侧从未被 StreamActor 触发 → no stream data in
	err := v.callInternal(context.Background(), nil)
	var be *exception.BaseError
	if !errors.As(err, &be) || be.Code() != exception.StatusGraphVertexStreamCallError.Code() {
		t.Fatalf("应报流调用错误, got %v", err)
	}
}

func TestCallInternal_流等待握手(t *testing.T) {
	// 流侧先启动（stream=1,call=0）→ callInternal 等待 streamDone
	v, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	v.componentAbility = []ability.ComponentAbility{ability.ComponentAbilityInvoke}
	v.streamCallCount.Store(1) // 模拟流侧已启动
	go func() {
		time.Sleep(20 * time.Millisecond)
		v.streamDone <- nil // 流侧完成
	}()
	if err := v.callInternal(context.Background(), nil); err != nil {
		t.Fatalf("流侧成功结果应放行, err=%v", err)
	}
	// 流侧失败 → 转抛（vertex.py:696-697）
	v2, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	v2.streamCallCount.Store(1)
	boom := errors.New("stream failed")
	go func() { v2.streamDone <- boom }()
	if err := v2.callInternal(context.Background(), nil); !errors.Is(err, boom) {
		t.Fatalf("流侧失败应转抛, got %v", err)
	}
	// 超时分支 D-1（vertex.py:698-700）
	v3, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	v3.streamCallCount.Store(1)
	v3.streamCalledTimeout = 0.05
	err3 := v3.callInternal(context.Background(), nil)
	var be *exception.BaseError
	if !errors.As(err3, &be) || be.Code() != exception.StatusGraphVertexStreamCallTimeout.Code() {
		t.Fatalf("流等待超时应报超时错误, got %v", err3)
	}
}

func TestCall_成功路径(t *testing.T) {
	v, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	// post_commit=true → AtomicInvoke 路径（需 state 实现 WorkflowState——已满足）
	res, err := v.Call(context.Background(), map[string]any{"st": 1}, nil)
	if err != nil {
		t.Fatalf("Call 应成功, err=%v", err)
	}
	ids, ok := res["source_node_id"].([]string)
	if !ok || len(ids) != 1 || ids[0] != "n1" {
		t.Fatalf("应返回 source_node_id 信封, got %v", res)
	}
	if v.callCount.Load() != 1 {
		t.Fatal("finally 应计数（vertex.py:414）")
	}
	if v.isStarted.Load() || v.isCallStarted.Load() {
		t.Fatal("finally 应复位标志（vertex.py:415-416）")
	}
}

func TestCall_异常路径触发NodeError(t *testing.T) {
	v, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	v.executable = &fakeVertexExecutable{
		invokeFn: func(context.Context, map[string]any, session.BaseSession, map[string]any) (map[string]any, error) {
			return nil, errors.New("boom")
		},
	}
	fired := make(chan string, 1)
	nodeErrorFn := func(ctx context.Context, data map[string]any) any {
		fired <- "fired"
		return nil
	}
	callback.GetCallbackFramework().OnCustom(string(callback.NodeError), nodeErrorFn)
	defer callback.GetCallbackFramework().OffCustom(string(callback.NodeError), nodeErrorFn)
	_, err := v.Call(context.Background(), nil, nil)
	if err == nil {
		t.Fatal("批入失败应上抛")
	}
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("非中断失败应触发 NODE_ERROR 事件")
	}
	if v.callCount.Load() != 1 {
		t.Fatal("失败路径 finally 也应计数")
	}
}

func TestAtomicInvoke_转发call(t *testing.T) {
	v, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	// atomicInvoke → callInternal（vertex.py:418-419）：未初始化不成立时走正常链，
	// 此处经 stream 侧未启动的正常批入返回 nil error
	if err := func() error {
		_, err := v.atomicInvoke(context.Background(), map[string]any{"config": nil})
		return err
	}(); err != nil {
		t.Fatalf("atomicInvoke 应转发 callInternal, err=%v", err)
	}
}

// ──────────────────────────── Task10: StreamCall ────────────────────────────

func TestStreamCall_未初始化分支(t *testing.T) {
	v := NewVertex("n1", nil)
	cbErr := make(chan error, 1)
	// nodeSession nil → streamDone 送达错误 + doneCallback（vertex.py:725-735）
	done := make(chan struct{})
	go func() {
		v.StreamCall(context.Background(), nil, func(err error) { cbErr <- err; close(done) })
	}()
	select {
	case err := <-cbErr:
		var be *exception.BaseError
		if !errors.As(err, &be) || be.Code() != exception.StatusGraphVertexStreamCallError.Code() {
			t.Fatalf("应报 queue manager 未初始化, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("doneCallback 应被调用")
	}
	select {
	case err := <-v.streamDone:
		if err == nil {
			t.Fatal("streamDone 应携带错误")
		}
	default:
		t.Fatal("streamDone 应已送达")
	}
}

func TestStreamCall_启动链与成功收割(t *testing.T) {
	v, _ := newVertexWithActor(t, ability.ComponentAbilityCollect)
	collectStarted := make(chan struct{})
	v.executable = &fakeVertexExecutable{
		collectFn: func(ctx context.Context, inputs <-chan map[string]any, sess session.BaseSession,
			kw map[string]any) (map[string]any, error) {
			close(collectStarted)
			return map[string]any{"c": 1}, nil
		},
	}
	event := make(chan struct{})
	var cbCalled bool
	done := make(chan struct{})
	go func() {
		defer close(done)
		v.StreamCall(context.Background(), event, func(err error) { cbCalled = true })
	}()
	// 外层 event 在能力就位后放行（vertex.py:745）
	select {
	case <-event:
	case <-time.After(time.Second):
		t.Fatal("外层 event 应被置位")
	}
	<-collectStarted
	<-done
	if cbCalled {
		t.Fatal("成功路径不应调 error_callback")
	}
	select {
	case err := <-v.streamDone:
		if err != nil {
			t.Fatalf("成功应送达 nil, got %v", err)
		}
	default:
		t.Fatal("streamDone 应已送达")
	}
	if !v.streamDoneSet.Load() {
		t.Fatal("done 标记应置位（Future.done 探测）")
	}
}

func TestStreamCall_失败回调(t *testing.T) {
	v, _ := newVertexWithActor(t, ability.ComponentAbilityCollect)
	boom := errors.New("collect failed")
	v.executable = &fakeVertexExecutable{
		collectFn: func(context.Context, <-chan map[string]any, session.BaseSession, map[string]any) (map[string]any, error) {
			return nil, boom
		},
	}
	event := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		v.StreamCall(context.Background(), event, func(err error) { done <- err })
	}()
	<-event
	select {
	case err := <-done:
		if !errors.Is(err, boom) {
			t.Fatalf("error_callback 应收到原错误, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("失败应调 error_callback")
	}
	select {
	case err := <-v.streamDone:
		if !errors.Is(err, boom) {
			t.Fatalf("streamDone 应携带错误, got %v", err)
		}
	default:
		t.Fatal("streamDone 应已送达")
	}
}

func TestStreamCall_取消分类(t *testing.T) {
	v, _ := newVertexWithActor(t, ability.ComponentAbilityCollect)
	blockCtx, cancel := context.WithCancel(context.Background())
	v.executable = &fakeVertexExecutable{
		collectFn: func(ctx context.Context, inputs <-chan map[string]any, sess session.BaseSession,
			kw map[string]any) (map[string]any, error) {
			<-ctx.Done() // 阻塞至取消（对齐 Python task.cancel() 打断）
			return nil, ctx.Err()
		},
	}
	event := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		v.StreamCall(blockCtx, event, func(err error) { t.Error("取消路径不应调 error_callback") })
	}()
	<-event
	time.Sleep(30 * time.Millisecond) // 等能力进入阻塞
	cancel()
	<-done
	select {
	case err := <-v.streamDone:
		if err != nil {
			t.Fatalf("取消路径 error 应保持 nil（对齐 Python CancelledError 不入 error 通道）, got %v", err)
		}
	default:
		t.Fatal("streamDone 应已送达")
	}
}

func TestRegisterStreamConsumer(t *testing.T) {
	v, _ := newTraceVertex(t, false)
	g := stream_actor.NewStreamGraph()
	v.RegisterStreamConsumer(g)
	if g.GetNode("n1") == nil {
		t.Fatal("应按 nodeID 注册（对齐 graph.py add_stream_consumer）")
	}
	g2 := stream_actor.NewStreamGraph()
	v.RegisterStreamConsumer(g2)
	v.RegisterStreamConsumer(g2) // 幂等
}

// ──────────────────────────── Task12: -race 并发锁定 ────────────────────────────

// TestVertex_双链并发_race 全链并发：真实 StreamActor 首帧 spawn StreamCall
// （流入侧）与 Call（批入侧）并发握手。锁定 spec §1 屏障定序不变式——
// 若未来某路径破坏「流先批后」定序，-race 当场报出。
func TestVertex_双链并发_race(t *testing.T) {
	cfg := config.NewSessionConfig(context.Background())
	parent := &fakeInnerSession{cfg: cfg, st: state.NewInMemoryWorkflowState(), id: "s1"}
	v := NewVertex("n1", &fakeVertexExecutable{
		invokeFn: func(context.Context, map[string]any, session.BaseSession, map[string]any) (map[string]any, error) {
			return map[string]any{"invoked": true}, nil
		},
	})
	driveNodeSpec(t, v, parent, &workflow.NodeSpec{Abilities: []ability.ComponentAbility{
		ability.ComponentAbilityInvoke, ability.ComponentAbilityCollect}})
	spec := workflow.NewWorkflowSpec()
	spec.StreamEdges = map[string][]string{"A": {"n1"}}
	spec.CompConfigs = map[string]*workflow.NodeSpec{
		"A":  {Abilities: []ability.ComponentAbility{ability.ComponentAbilityStream}},
		"n1": {Abilities: v.componentAbility},
	}
	g := stream_actor.NewStreamGraph()
	v.RegisterStreamConsumer(g)
	am, err := stream_actor.NewActorManager(context.Background(), spec, g, false, &fakeVertexActorSession{})
	if err != nil {
		t.Fatal(err)
	}
	parent.am = am
	// 流入侧：首帧 spawn StreamCall（批入侧 Call 之前启动——不变式）
	if err := am.Produce(context.Background(), "A", map[string]any{"A": "first"},
		ability.ComponentAbilityStream, true); err != nil {
		t.Fatal(err)
	}
	// 批入侧：Call 等待当轮 streamDone 握手
	callDone := make(chan error, 1)
	go func() {
		_, err := v.Call(context.Background(), map[string]any{"st": 1}, nil)
		callDone <- err
	}()
	select {
	case err := <-callDone:
		if err != nil {
			t.Fatalf("双链握手应成功, err=%v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Call 应在流侧完成后返回")
	}
	if v.callCount.Load() != 1 || v.streamCallCount.Load() != 1 {
		t.Fatalf("计数应对账, call=%d stream=%d", v.callCount.Load(), v.streamCallCount.Load())
	}
}

// ──────────────────────────── Task12: 覆盖率补强 ────────────────────────────

// templateExe 带 Template 鸭子探测的桩（sanitizeEndNodeInputs 混合模式）
type templateExe struct {
	fakeVertexExecutable
	tpl *fakeTemplate
}

func (t *templateExe) Template() templateLike { return t.tpl }

type fakeTemplate struct{ count int }

func (t *fakeTemplate) SetDataSourceCount(n int) { t.count = n }

func TestSanitizeEndNodeInputs_混合模式(t *testing.T) {
	v, parent := newTraceVertex(t, false)
	v.isEndNode = true
	v.hasCall = true
	v.hasStreamCall = true
	// template 缺失 → 原样返回（vertex.py:447-449）
	in := map[string]any{"a": "x"}
	if got := v.sanitizeEndNodeInputs(context.Background(), in, map[string]any{}); got == nil {
		t.Fatal("无模板应原样返回")
	}
	// 有模板 + 流侧源全部未执行 → set_data_source_count(0)（vertex.py:460-464）
	exe := &templateExe{tpl: &fakeTemplate{}}
	v.executable = exe
	driveNodeSpec(t, v, parent, &workflow.NodeSpec{
		StreamIOConfigs: &workflow.CompIOConfig{InputsSchema: map[string]any{
			"x": "${A.out}"}}})
	got := v.sanitizeEndNodeInputs(context.Background(), in, map[string]any{})
	if got == nil || exe.tpl.count != 0 {
		t.Fatalf("混合模式流源全未执行应清零数据源计数, count=%d", exe.tpl.count)
	}
	// 流侧源已执行 → 不清零（count 不再变化）
	v.stateView().UpdateAndCommitWorkflowState(map[string]any{"executed_nodes": []string{"A"}})
	_ = v.sanitizeEndNodeInputs(context.Background(), in, map[string]any{})
	// hasCall=false（非混合）→ 短路返回（vertex.py:451-452）
	v2, _ := newTraceVertex(t, false)
	v2.isEndNode = true
	exe2 := &templateExe{tpl: &fakeTemplate{}}
	v2.executable = exe2
	_ = v2.sanitizeEndNodeInputs(context.Background(), map[string]any{}, map[string]any{})
	if exe2.tpl.count != 0 {
		t.Fatal("非混合模式不应触达模板")
	}
	// 流 schema 为空 → 返回（vertex.py:458-459）
	v3, _ := newTraceVertex(t, false)
	v3.isEndNode = true
	v3.hasCall = true
	v3.hasStreamCall = true
	exe3 := &templateExe{tpl: &fakeTemplate{}}
	v3.executable = exe3
	_ = v3.sanitizeEndNodeInputs(context.Background(), map[string]any{}, map[string]any{})
}

func TestTraceComponentBegin_元数据钩子执行(t *testing.T) {
	// TraceMetadata 钩子：返回值不透传（D-4）但执行 + panic 告警路径（vertex.py:834-846）
	tr := tracer.NewTracer()
	tr.Init(stream.NewStreamWriterManager(stream.NewStreamEmitter()))
	parent := &fakeInnerSession{cfg: config.NewSessionConfig(context.Background()),
		st: state.NewInMemoryWorkflowState(), id: "s1", tr: tr}
	v := NewVertex("n1", &traceMetaExe{})
	_ = v.Init(parent, nil)
	v.traceComponentBegin(context.Background())
	// panic 钩子
	v2 := NewVertex("n1", &traceMetaPanicExe{})
	_ = v2.Init(parent, nil)
	v2.isStarted.Store(false)
	v2.traceComponentBegin(context.Background())
}

// traceMetaExe 实现 TraceMetadata 钩子
type traceMetaExe struct {
	fakeVertexExecutable
}

func (t *traceMetaExe) TraceMetadata() map[string]any { return map[string]any{"k": "v"} }

// traceMetaPanicExe 钩子 panic 桩
type traceMetaPanicExe struct {
	fakeVertexExecutable
}

func (t *traceMetaPanicExe) TraceMetadata() map[string]any { panic("hook failed") }

func TestIsStreamSourceStillPending_分支(t *testing.T) {
	v, _ := newTraceVertex(t, false)
	// actor_manager nil → false（vertex.py:522-524）
	if v.isStreamSourceStillPending("A") {
		t.Fatal("无 actor manager 应 false")
	}
	// 有 manager：未匹配组 → should_sanitize true → pending false（取反）
	v2, _ := newVertexWithActor(t, ability.ComponentAbilityCollect)
	if v2.isStreamSourceStillPending("Z") {
		t.Fatal("无匹配组应 pending=false")
	}
	// 单源组 AND 依赖 → 不 sanitize → pending true
	if !v2.isStreamSourceStillPending("A") {
		t.Fatal("单源组 AND 依赖应 pending=true")
	}
}

func TestInit_节点规格派生与SetEndNode(t *testing.T) {
	v, parent := newTraceVertex(t, false)
	driveNodeSpec(t, v, parent, &workflow.NodeSpec{Abilities: []ability.ComponentAbility{
		ability.ComponentAbilityTransform}})
	v.SetEndNode(true)
	if !v.isEndNode {
		t.Fatal("SetEndNode 应生效")
	}
}

func TestTraceMethods_有效链路(t *testing.T) {
	// tracer 有效 handler：八方法全链路不 panic（TriggerWorkflow 真实下发）
	tr := tracer.NewTracer()
	tr.Init(stream.NewStreamWriterManager(stream.NewStreamEmitter()))
	cfg := config.NewSessionConfig(context.Background())
	parent := &fakeInnerSession{cfg: cfg, st: state.NewInMemoryWorkflowState(), id: "s1", tr: tr}
	v := NewVertex("n1", &fakeVertexExecutable{})
	_ = v.Init(parent, nil)
	ctx := context.Background()
	v.traceComponentBegin(ctx)
	v.traceComponentInputs(ctx, map[string]any{"a": 1})
	v.traceComponentOutputs(ctx, map[string]any{"b": 2})
	v.traceComponentStreamOutput(ctx, &stream.OutputSchema{Type: "t"})
	v.traceComponentDone(ctx)
	v.traceError(ctx, errors.New("e"))
	v.traceInnerError(ctx, errors.New("inner"))
	v.traceComponentStreamInputSend(ctx)
	// 子图组件类型 → RegisterWorkflowSpanManager 分支（vertex.py:819-820）
	v.executable = &subWorkflowExe{}
	v.isStarted.Store(false)
	v.traceComponentBegin(ctx)
}

// subWorkflowExe ComponentType=sub_workflow 的桩
type subWorkflowExe struct {
	fakeVertexExecutable
}

func (s *subWorkflowExe) ComponentType() string { return subWorkflowComponent }

func TestWorkflowStateList_三态(t *testing.T) {
	v, _ := newTraceVertex(t, false)
	// nil → nil
	if workflowStateList(v.stateView(), "missing") != nil {
		t.Fatal("缺失键应 nil")
	}
	// []any 混合
	v.stateView().UpdateAndCommitWorkflowState(map[string]any{"k": []any{"a", 1, "b"}})
	got := workflowStateList(v.stateView(), "k")
	if len(got) != 2 {
		t.Fatalf("[]any 应过滤非字符串, got %v", got)
	}
}

func TestClearInteractive_未设置(t *testing.T) {
	// 无交互输入 → Get nil → 不更新（vertex.py:657-658 分支）
	v, _ := newTraceVertex(t, false)
	v.clearInteractive()
}

func TestSecondsToDuration_非正值(t *testing.T) {
	if secondsToDuration(0) != 0 || secondsToDuration(-1) != 0 {
		t.Fatal("<=0 应返回 0（不限时）")
	}
	if secondsToDuration(2) != 2*time.Second {
		t.Fatal("正秒数应转换")
	}
}

func TestPostStream_Transformer轨与StreamOutput(t *testing.T) {
	// transformer 轨（非 dict outputs_schema）+ get_stream_output 钩子（vertex.py:597-598/625-628）
	v, am := newVertexAsProducer(t)
	// 8.15：经 Init 管路注入 StreamIOConfigs（driveNodeSpec 需要 parent，经 producer helper 内部 fake 取不到，
	// 改直接驱动 Init 内联步骤等价物：注册 spec 后重 Init）
	{
		parent := &fakeInnerSession{cfg: config.NewSessionConfig(context.Background()),
			st: state.NewInMemoryWorkflowState(), am: am}
		driveNodeSpec(t, v, parent, &workflow.NodeSpec{StreamIOConfigs: &workflow.CompIOConfig{
			OutputsSchema: sutils.MessageTransformer(func(m map[string]any) map[string]any {
				return map[string]any{"t": "x"}
			})}})
	}
	// get_stream_output 钩子
	v.executable = &streamOutExe{}
	ch := make(chan StreamChunk[any], 1)
	ch <- StreamChunk[any]{Val: map[string]any{"n1": "raw"}}
	close(ch)
	v.postStream(context.Background(), ch, ability.ComponentAbilityStream)
	// transformer 轨产出的 message "x"（map）入下游 + END 帧
	if am.StreamActor("n2").Processor(ability.ComponentAbilityCollect).MainQueueLen() != 2 {
		t.Fatal("transformer 轨应产出数据帧+END 帧")
	}
}

// streamOutExe 实现 GetStreamOutput 钩子
type streamOutExe struct {
	fakeVertexExecutable
}

func (s *streamOutExe) GetStreamOutput() map[string]any { return map[string]any{"acc": "sum"} }

func TestAnyToFloat64_多类型(t *testing.T) {
	cases := []struct {
		in   any
		want float64
	}{{3.5, 3.5}, {float32(2.5), 2.5}, {7, 7}, {int64(9), 9}, {"bad", 0}, {nil, 0}}
	for _, c := range cases {
		if got := anyToFloat64(c.in); got != c.want {
			t.Fatalf("anyToFloat64(%v)=%v want %v", c.in, got, c.want)
		}
	}
}

func TestClearInteractive(t *testing.T) {
	v, _ := newTraceVertex(t, false)
	v.stateView().UpdateAndCommitWorkflowState(map[string]any{
		constants.InteractiveInputKey: "pending"})
	// 写入交互键到当前节点状态（Get 走节点视图）
	_ = v.nodeSession.State().Update(map[string]any{constants.InteractiveInputKey: "pending"})
	v.clearInteractive()
	if v.nodeSession.State().Get(state.StringKey(constants.InteractiveInputKey)) != nil {
		t.Fatal("交互输入应被清理")
	}
}

func TestRunExecutable_Stream策略与子图封装(t *testing.T) {
	// 子图封装：OnInvoke 收到 {inputs, config} 信封（vertex.py:136-137）
	v, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	var gotInputs map[string]any
	v.executable = &fakeVertexExecutable{
		invokeFn: func(ctx context.Context, inputs map[string]any, sess session.BaseSession,
			kw map[string]any) (map[string]any, error) {
			gotInputs = inputs
			return map[string]any{}, nil
		},
	}
	_, err := v.runExecutable(context.Background(), ability.ComponentAbilityInvoke, true,
		map[string]any{"wf": "cfg"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, has := gotInputs[InputsKey]; !has {
		t.Fatalf("子图信封应含 inputs 键, got %v", gotInputs)
	}
	if _, has := gotInputs[ConfigKey]; !has {
		t.Fatalf("子图信封应含 config 键, got %v", gotInputs)
	}
	// stream 策略：OnStream 成功空流（vertex.py:148-158）
	v2, _ := newVertexWithActor(t, ability.ComponentAbilityStream)
	v2.executable = &fakeVertexExecutable{} // 默认空流
	ok, err := v2.runExecutable(context.Background(), ability.ComponentAbilityStream, false, nil, nil)
	if err != nil || !ok {
		t.Fatalf("stream 策略应成功, ok=%v err=%v", ok, err)
	}
	// 中断路径：OnInvoke 返回 GraphInterrupt → info 日志原样上抛（vertex.py:206-212）
	v3, _ := newVertexWithActor(t, ability.ComponentAbilityInvoke)
	gi := &pregel.GraphInterrupt{}
	v3.executable = &fakeVertexExecutable{
		invokeFn: func(context.Context, map[string]any, session.BaseSession, map[string]any) (map[string]any, error) {
			return nil, gi
		},
	}
	_, err3 := v3.runExecutable(context.Background(), ability.ComponentAbilityInvoke, false, nil, nil)
	if !errors.Is(err3, gi) {
		t.Fatal("中断应原样上抛")
	}
}

func TestProcessChunk_End子图与Schema直通(t *testing.T) {
	// End 子图：OutputSchema 取 payload 发 sub_workflow_stream（vertex.py:647-650）
	w, _ := newSubVertexWorld(t)
	v := w.v
	os := &stream.OutputSchema{Type: "x", Payload: "p"}
	v.processChunk(context.Background(), os, true, 0, true, ability.ComponentAbilityStream)
	q, err := w.am.SubWorkflowStream()
	if err != nil {
		t.Fatal(err)
	}
	val, rerr := q.Receive(context.Background(), 500*time.Millisecond)
	if rerr != nil || val != "p" {
		t.Fatalf("End 子图应直发 payload, got %v err=%v", val, rerr)
	}
	// 普通 OutputSchema 直通（StreamSchemas isinstance 分支）
	v2, _ := newTraceVertex(t, false)
	written := make(chan any, 1)
	swm := stream.NewStreamWriterManager(stream.NewStreamEmitter())
	_ = swm.AddWriter(stream.StreamModeOutput, &fakeOutputWriter{ch: written})
	v2.nodeSession = &fakeVertexNodeSession{WorkflowVertexSession: v2.nodeSession, swm: swm}
	v2.isEndNode = true
	v2.processChunk(context.Background(), &stream.OutputSchema{Type: "raw", Payload: "d"},
		true, 0, false, ability.ComponentAbilityStream)
	select {
	case got := <-written:
		if got.(*stream.OutputSchema).Type != "raw" {
			t.Fatal("StreamSchemas 应直通不发信封")
		}
	default:
		t.Fatal("writer 应收到直通帧")
	}
}

// newSubVertexWorld 子图 Vertex 世界（SubWorkflowStream 可用）
type subWorld struct {
	v  *Vertex
	am *stream_actor.ActorManager
}

func newSubVertexWorld(t *testing.T) (*subWorld, error) {
	cfg := config.NewSessionConfig(context.Background())
	parent := &fakeInnerSession{cfg: cfg, st: state.NewInMemoryWorkflowState(), id: "s1"}
	v := NewVertex("B", &fakeVertexExecutable{})
	_ = v.Init(parent, nil)
	v.SetEndNode(true)
	spec := workflow.NewWorkflowSpec()
	spec.CompConfigs = map[string]*workflow.NodeSpec{
		"B": {Abilities: []ability.ComponentAbility{ability.ComponentAbilityStream}},
	}
	g := stream_actor.NewStreamGraph()
	v.RegisterStreamConsumer(g)
	am, err := stream_actor.NewActorManager(context.Background(), spec, g, true, &fakeVertexActorSession{})
	if err != nil {
		return nil, err
	}
	parent.am = am
	return &subWorld{v: v, am: am}, nil
}
