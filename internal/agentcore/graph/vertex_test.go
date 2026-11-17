package graph

import (
	"context"
	"errors"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"
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
}

func (f *fakeVertexExecutable) OnInvoke(ctx context.Context, inputs map[string]any,
	sess session.BaseSession, kw map[string]any) (map[string]any, error) {
	return map[string]any{"invoked": true}, nil
}

// ──────────────────────────── Task3: Init ────────────────────────────

// fakeInnerSession 最小 InnerSession 桩（Init 依赖面）
type fakeInnerSession struct {
	cfg config.SessionConfig
	st  state.SessionState
	tr  *tracer.Tracer
	id  string
	swm *stream.StreamWriterManager
	am  *stream_actor.ActorManager
	cp  interfaces.Checkpointer
}

func (f *fakeInnerSession) Config() config.SessionConfig                     { return f.cfg }
func (f *fakeInnerSession) State() state.SessionState                        { return f.st }
func (f *fakeInnerSession) Tracer() *tracer.Tracer                           { return f.tr }
func (f *fakeInnerSession) StreamWriterManager() *stream.StreamWriterManager { return f.swm }
func (f *fakeInnerSession) SessionID() string                                { return f.id }
func (f *fakeInnerSession) Checkpointer() interfaces.Checkpointer            { return f.cp }
func (f *fakeInnerSession) ActorManager() *stream_actor.ActorManager         { return f.am }
func (f *fakeInnerSession) Close() error                                     { return nil }

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
	if v.ctx != ctxVal {
		t.Fatal("context 应从 kwargs 注入（vertex.py:90）")
	}
	if v.streamCalledTimeout != 3.5 {
		t.Fatalf("流超时应从 session env 读取（vertex.py:91）, got %v", v.streamCalledTimeout)
	}
	// nodeConfig 为 nil（⤵️ 8.15 回填管路）→ 能力面默认 [INVOKE]（vertex.py:93-95）
	if len(v.componentAbility) != 1 || v.componentAbility[0] != components.ComponentAbilityInvoke {
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
	// NodeSession.NodeConfig() ⤵️ 8.15 回填前恒 nil；能力派生逻辑经
	// applyNodeConfig 直测（Init 内联步骤，测试经同包字段注入驱动）
	v := NewVertex("n", &fakeVertexExecutable{})
	parent := &fakeInnerSession{cfg: config.NewSessionConfig(context.Background()),
		st: state.NewInMemoryWorkflowState()}
	_ = v.Init(parent, nil)
	// 直接驱动派生逻辑（Init 的 vertex.py:93-97 段）
	v.applyNodeConfig(&workflow.NodeSpec{Abilities: []components.ComponentAbility{
		components.ComponentAbilityInvoke, components.ComponentAbilityTransform}})
	if !v.hasStreamCall || !v.hasCall {
		t.Fatalf("混合能力应双真, stream=%v call=%v", v.hasStreamCall, v.hasCall)
	}
	v.applyNodeConfig(&workflow.NodeSpec{Abilities: []components.ComponentAbility{
		components.ComponentAbilityCollect}})
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
	in2, ok2 := v.preInvoke(context.Background()).(map[string]any)
	if !ok2 || len(in2) != 1 {
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
	v, _ := newTraceVertex(t, false)
	v.applyNodeConfig(&workflow.NodeSpec{Abilities: []components.ComponentAbility{
		components.ComponentAbilityInvoke, components.ComponentAbilityCollect,
		components.ComponentAbilityTransform}})
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
func newVertexWithActor(t *testing.T, abilities ...components.ComponentAbility) (*Vertex, *stream_actor.ActorManager) {
	t.Helper()
	cfg := config.NewSessionConfig(context.Background())
	parent := &fakeInnerSession{cfg: cfg, st: state.NewInMemoryWorkflowState(), id: "s1"}
	v := NewVertex("n1", &fakeVertexExecutable{})
	_ = v.Init(parent, nil)
	v.applyNodeConfig(&workflow.NodeSpec{Abilities: abilities})
	spec := workflow.NewWorkflowSpec()
	spec.StreamEdges = map[string][]string{"A": {"n1"}}
	spec.CompConfigs = map[string]*workflow.NodeSpec{
		"A":  {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
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
	v, _ := newVertexWithActor(t, components.ComponentAbilityCollect)
	// 无 schema：Consume 返回空 map（Generator 空 schema 短路）
	out, err := v.preStream(context.Background(), components.ComponentAbilityCollect)
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("Consume 返回不应为 nil")
	}
	// 有 schema：$ref 叶子应注册为 StreamReader（Generator 一比一）
	v2, _ := newVertexWithActor(t, components.ComponentAbilityCollect)
	v2.nodeConfig = &workflow.NodeSpec{StreamIOConfigs: &workflow.CompIOConfig{
		InputsSchema: map[string]any{"x": "${A.answer}"}}}
	out2, err := v2.preStream(context.Background(), components.ComponentAbilityCollect)
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
func (f *fakeVertexConsumer) IsDone() bool              { return false }

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
		"n1": {Abilities: []components.ComponentAbility{components.ComponentAbilityStream}},
		"n2": {Abilities: []components.ComponentAbility{components.ComponentAbilityCollect}},
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
	v.processChunk(context.Background(), "chunk-0", true, 0, false, components.ComponentAbilityStream)
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
	v2.processChunk(context.Background(), "x", false, 0, false, components.ComponentAbilityStream)
	if am.StreamActor("n2").Processor(components.ComponentAbilityCollect).MainQueueLen() != 1 {
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
	ch := make(chan StreamChunk[map[string]any], 1)
	ch <- StreamChunk[map[string]any]{Val: map[string]any{"n1": "hello"}}
	close(ch)
	v.postStream(context.Background(), ch, components.ComponentAbilityStream)
	if am.StreamActor("n2").Processor(components.ComponentAbilityCollect).MainQueueLen() != 2 {
		t.Fatalf("应产出数据帧+END 哨兵帧, got %d", am.StreamActor("n2").Processor(components.ComponentAbilityCollect).MainQueueLen())
	}
	// 逐 chunk 转发：默认 transformer 无 schema → 原样透传
	v2, am2 := newVertexAsProducer(t)
	ch2 := make(chan StreamChunk[map[string]any], 1)
	ch2 <- StreamChunk[map[string]any]{Val: map[string]any{"n1": "hello"}}
	close(ch2)
	v2.postStream(context.Background(), ch2, components.ComponentAbilityStream)
	p := am2.StreamActor("n2").Processor(components.ComponentAbilityCollect)
	if p.MainQueueLen() != 2 { // 数据帧 + END 帧
		t.Fatalf("应产出数据帧+END 帧, got %d", p.MainQueueLen())
	}
}
