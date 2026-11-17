package graph

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
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
