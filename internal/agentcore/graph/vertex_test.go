package graph

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
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
