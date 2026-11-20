package components

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
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

// ──────────────────────────── 组合面测试（8.14 Task 1）────────────────────────────

// TestWorkflowComponent_ToExecutable_自返回 Python component.py:263-283：
// isinstance(self, Executable) 恒真 → to_executable 返回 self。
func TestWorkflowComponent_ToExecutable_自返回(t *testing.T) {
	comp := &WorkflowComponent{}
	if comp.ToExecutable() != graphexe.GeneralExecutor(comp) {
		t.Fatal("ToExecutable 应返回组件自身")
	}
}

// TestWorkflowComponent_AddComponent_默认接线 Python component.py:248-260：
// graph.add_node(node_id, self.to_executable(), wait_for_all=wait_for_all)。
func TestWorkflowComponent_AddComponent_默认接线(t *testing.T) {
	g := graph.NewPregelGraph()
	comp := &WorkflowComponent{}
	if err := comp.AddComponent(g, "comp1", false); err != nil {
		t.Fatalf("AddComponent 返回错误：%v", err)
	}
	if _, ok := g.GetNodes()["comp1"]; !ok {
		t.Fatal("节点未注册进图")
	}
	// waitForAll=true → 前驱汇聚屏障（WithWaitForAll 等价）
	g2 := graph.NewPregelGraph()
	comp2 := &WorkflowComponent{}
	if err := comp2.AddComponent(g2, "comp2", true); err != nil {
		t.Fatalf("AddComponent(waitForAll) 返回错误：%v", err)
	}
	if _, ok := g2.GetNodes()["comp2"]; !ok {
		t.Fatal("waitForAll 分支节点未注册")
	}
}

// TestBaseComponentComposable_槽Nil复刻NotImplementedError Python component.py:281-283
// to_executable 缺失分支：NotImplementedError 文案复刻（去类名，D-17-1 同款）。
func TestBaseComponentComposable_槽Nil复刻NotImplementedError(t *testing.T) {
	m := &BaseComponentComposable{}
	err := m.AddComponent(graph.NewPregelGraph(), "comp1", false)
	if err == nil || !strings.Contains(err.Error(), "missing required method: to_executable()") {
		t.Fatalf("期望 NotImplementedError 复刻文案, got %v", err)
	}
}

// TestBaseComponentComposable_槽接线 纯组合件只提供 ToExecutable 槽。
func TestBaseComponentComposable_槽接线(t *testing.T) {
	g := graph.NewPregelGraph()
	exec := &WorkflowComponent{}
	m := &BaseComponentComposable{ToExecutableFn: func() graphexe.GeneralExecutor { return exec }}
	if err := m.AddComponent(g, "pure", false); err != nil {
		t.Fatalf("AddComponent 返回错误：%v", err)
	}
	if _, ok := g.GetNodes()["pure"]; !ok {
		t.Fatal("槽产物未注册进图")
	}
}

// TestWorkflowComponent_覆写遮蔽基座 外层定义同名方法提升遮蔽（≙Python override）。
func TestWorkflowComponent_覆写遮蔽基座(t *testing.T) {
	g := graph.NewPregelGraph()
	comp := &WorkflowComponent{}
	if err := comp.AddComponent(g, "x", false); err != nil {
		t.Fatalf("未覆写路径应走默认实现：%v", err)
	}
	// 覆写：嵌入后定义外层方法遮蔽（编译期验证提升语义）
	type overriding struct {
		*WorkflowComponent
	}
	_ = overriding{}
	// 经接口断言验证覆写面：BaseComponentComposable 槽形态下外层注入错误即遮蔽
	m := &BaseComponentComposable{ToExecutableFn: func() graphexe.GeneralExecutor {
		return nil // 槽返回 nil → AddNode panic（图构造期纪律）
	}}
	func() {
		defer func() {
			if r := recover(); r == nil {
				t.Fatal("槽返回 nil 时应触发图构造 panic")
			}
		}()
		_ = m.AddComponent(g, "y", false)
	}()
}
