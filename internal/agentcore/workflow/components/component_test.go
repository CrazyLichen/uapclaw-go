package components

import (
	"context"
	"strings"
	"testing"

	graphexe "github.com/uapclaw/uapclaw-go/internal/agentcore/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
)

// TestWorkflowComponent_OnInvoke_会话类型拒绝 验证非顶点会话返回编排错误
// （component.py:96-98 isinstance(session, NodeSession) 校验）。
func TestWorkflowComponent_OnInvoke_会话类型拒绝(t *testing.T) {
	comp := &WorkflowComponent{InvokeFn: func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
		return inputs, nil
	}}
	_, err := comp.OnInvoke(context.Background(), map[string]any{"a": 1}, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "NodeSession") {
		t.Fatalf("期望 NodeSession 类型错误, got %v", err)
	}
}

// TestWorkflowComponent_OnInvoke_透传 验证 InvokeFn 收到组件级会话并透传输入。
func TestWorkflowComponent_OnInvoke_透传(t *testing.T) {
	var gotSess *session.NodeSessionFacade
	comp := &WorkflowComponent{InvokeFn: func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
		gotSess = sess
		return inputs, nil
	}}
	inner := newFakeVertexSession(t, "n1", "Start")
	out, err := comp.OnInvoke(context.Background(), map[string]any{"a": 1}, inner, nil)
	if err != nil || out["a"] != 1 || gotSess == nil {
		t.Fatalf("透传失败: out=%v err=%v sess=%v", out, err, gotSess)
	}
	// 组件级会话 streamMode=false（invoke 用 Session(session, False)）
	if gotSess.GetComponentID() != "n1" {
		t.Fatalf("组件级会话 node_id 不符: %s", gotSess.GetComponentID())
	}
}

// TestWorkflowComponent_默认钩子未实现 验证未覆写时走 BaseExecutable 默认错误。
func TestWorkflowComponent_默认钩子未实现(t *testing.T) {
	comp := &WorkflowComponent{}
	inner := newFakeVertexSession(t, "n1", "Start")
	if _, err := comp.OnStream(context.Background(), map[string]any{}, inner, nil); err == nil {
		t.Fatal("OnStream 期望默认未实现错误")
	}
	in := make(chan map[string]any)
	defer close(in)
	if _, err := comp.OnCollect(context.Background(), in, inner, nil); err == nil {
		t.Fatal("OnCollect 期望默认未实现错误")
	}
	if _, err := comp.OnTransform(context.Background(), in, inner, nil); err == nil {
		t.Fatal("OnTransform 期望默认未实现错误")
	}
}

// TestWorkflowComponent_TraceMetadata_默认nil 验证未覆写时返回 nil
// （component.py:124-135 默认 None）。
func TestWorkflowComponent_TraceMetadata_默认nil(t *testing.T) {
	comp := &WorkflowComponent{}
	if comp.TraceMetadata() != nil {
		t.Fatal("默认 TraceMetadata 应为 nil")
	}
	comp2 := &WorkflowComponent{TraceMetadataFn: func() map[string]any { return map[string]any{"k": "v"} }}
	if comp2.TraceMetadata()["k"] != "v" {
		t.Fatal("覆写后应返回自定义元数据")
	}
}

// TestWorkflowComponent_OnCollect_流式会话包装 验证 collect/transform 的
// streamMode=true 路径（component.py:114 Session(session, True)）。
func TestWorkflowComponent_OnCollect_流式会话包装(t *testing.T) {
	comp := &WorkflowComponent{CollectFn: func(ctx context.Context, inputs <-chan map[string]any, sess *session.NodeSessionFacade) (map[string]any, error) {
		if sess == nil {
			t.Fatal("collect 应收到组件级会话")
		}
		return map[string]any{"ok": true}, nil
	}}
	inner := newFakeVertexSession(t, "n1", "End")
	in := make(chan map[string]any, 1)
	in <- map[string]any{"x": 1}
	close(in)
	out, err := comp.OnCollect(context.Background(), in, inner, nil)
	if err != nil || out["ok"] != true {
		t.Fatalf("collect 转调失败: out=%v err=%v", out, err)
	}
}

// TestWorkflowComponent_AbilityValues_别名锚点 验证别名委托（D-17-8 锚点保持）。
func TestWorkflowComponent_AbilityValues_别名锚点(t *testing.T) {
	vals := AbilityValues()
	if len(vals) != 4 || vals[0] != ComponentAbilityInvoke {
		t.Fatalf("AbilityValues 委托不符: %v", vals)
	}
	if ComponentAbilityInvoke.Name() != "invoke" || ComponentAbilityTransform.Desc() != "stream in, stream out" {
		t.Fatal("别名方法透传不符")
	}
}

// TestWorkflowComponent_OnStream_流输出转换触发 验证 transformStreamOutput
// 逐帧转换路径（component.py output_event 逐项语义）。
func TestWorkflowComponent_OnStream_流输出转换触发(t *testing.T) {
	fw := callback.GetCallbackFramework()
	fw.RegisterWorkflowTransformIO(callback.ComponentBatchInput, callback.ComponentStreamOutput,
		nil,
		func(ctx context.Context, event callback.WorkflowEventType, output any) any {
			return map[string]any{"wrapped": true}
		})
	defer fw.RegisterWorkflowTransformIO(callback.ComponentBatchInput, callback.ComponentStreamOutput, nil, nil)

	comp := &WorkflowComponent{StreamFn: func(ctx context.Context, inputs map[string]any, sess *session.NodeSessionFacade) (<-chan graphexe.StreamChunk[map[string]any], error) {
		ch := make(chan graphexe.StreamChunk[map[string]any], 1)
		ch <- graphexe.StreamChunk[map[string]any]{Val: map[string]any{"raw": 1}}
		close(ch)
		return ch, nil
	}}
	inner := newFakeVertexSession(t, "n1", "End")
	ch, err := comp.OnStream(context.Background(), map[string]any{}, inner, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := <-ch
	if got.Err != nil {
		t.Fatal(got.Err)
	}
	if got.Val["wrapped"] != true {
		t.Fatalf("流输出应经转换包装: %v", got.Val)
	}
}
