package workflow

import (
	"fmt"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	graphexe "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/executable"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// newTestWorkflow 构造带两个组件的测试工作流基座。
func newTestWorkflow(t *testing.T) *BaseWorkflow {
	t.Helper()
	bw := NewBaseWorkflow(nil, nil)
	for _, id := range []string{"a", "b", "c"} {
		if err := bw.AddWorkflowComp(id, newFakeStartComp(), boolPtr(false),
			nil, nil, nil, nil, nil, 0, -1.0, nil, nil); err != nil {
			t.Fatalf("注册组件 %s 失败：%v", id, err)
		}
	}
	return bw
}

func boolPtr(b bool) *bool    { return &b }
func strPtr(s string) *string { return &s }

// ──────────────────────────── 构造与校验族 ────────────────────────────

// TestNewBaseWorkflow_默认兜底 graph/config nil 兜底（Python _workflow.py:149-160）。
func TestNewBaseWorkflow_默认兜底(t *testing.T) {
	bw := NewBaseWorkflow(nil, nil)
	if bw.graph == nil {
		t.Fatal("graph 兜底缺失")
	}
	if bw.Config() == nil || bw.Config().Card.ID == "" {
		t.Fatal("config nil 兜底缺失（≙ WorkflowCard(id=uuid4().hex)）")
	}
	if bw.workflowSpec == nil || bw.workflowSpec.Edges == nil {
		t.Fatal("spec 未挂接")
	}
}

// TestValidateCompID_规则 对齐 _workflow.py:492-507 四分支。
func TestValidateCompID_规则(t *testing.T) {
	bw := NewBaseWorkflow(nil, nil)
	cases := []struct {
		id    string
		valid bool
	}{
		{"", false}, {"a b", false}, {"中", false},
		{strings.Repeat("a", 101), false},
		{"A-z_9", true},
	}
	for _, c := range cases {
		err := bw.validateCompID(c.id)
		if c.valid && err != nil {
			t.Errorf("%q 应合法，实际 %v", c.id, err)
		}
		if !c.valid && err == nil {
			t.Errorf("%q 应非法", c.id)
		}
	}
}

// TestValidateSchemas_键冲突 对齐 _workflow.py:545-569 flatten_dict 冲突检测。
func TestValidateSchemas_键冲突(t *testing.T) {
	bw := NewBaseWorkflow(nil, nil)
	if err := bw.validateSchemas("c1",
		map[string]any{"a": map[string]any{"b": 1}}, nil,
		map[string]any{"a": map[string]any{"b": 2}}, nil); err == nil {
		t.Fatal("a.b 冲突应拒绝")
	}
	if err := bw.validateSchemas("c1",
		map[string]any{"a": 1}, nil, map[string]any{"x": 2}, nil); err != nil {
		t.Fatalf("无冲突应放行：%v", err)
	}
}

// TestValidateCompAbility_流能力强制等待 对齐 _workflow.py:509-519。
func TestValidateCompAbility_流能力强制等待(t *testing.T) {
	bw := NewBaseWorkflow(nil, nil)
	wf := false
	if err := bw.validateCompAbility("c1", []components.ComponentAbility{components.ComponentAbilityTransform}, &wf); err == nil {
		t.Fatal("TRANSFORM + wait_for_all=false 应拒绝")
	}
	if err := bw.validateCompAbility("c1", []components.ComponentAbility{components.ComponentAbilityTransform}, boolPtr(true)); err != nil {
		t.Fatalf("wait_for_all=true 应放行：%v", err)
	}
	if err := bw.validateCompAbility("c1", nil, &wf); err != nil {
		t.Fatalf("abilities nil 应直通：%v", err)
	}
}

// TestValidateEdge_形态 对齐 _workflow.py:521-543（D-14-6 收窄 []string：
// str 形态由调用方包一元素列表，非 str/list 形态由类型系统排除）。
func TestValidateEdge_形态(t *testing.T) {
	bw := NewBaseWorkflow(nil, nil)
	if err := bw.validateEdge([]string{""}, "b", exception.StatusWorkflowEdgeInvalid); err == nil {
		t.Fatal("空源应拒绝")
	}
	if err := bw.validateEdge([]string{"a", ""}, "b", exception.StatusWorkflowEdgeInvalid); err == nil {
		t.Fatal("列表含空应拒绝")
	}
	if err := bw.validateEdge([]string{"a"}, "b", exception.StatusWorkflowEdgeInvalid); err != nil {
		t.Fatalf("合法形态应放行：%v", err)
	}
}

// TestFlattenDict_点路径 对齐 dict_utils.flatten_dict。
func TestFlattenDict_点路径(t *testing.T) {
	got := flattenDict(map[string]any{"a": map[string]any{"b": map[string]any{"c": 1}}, "d": 2})
	if got["a.b.c"] != 1 || got["d"] != 2 {
		t.Fatalf("flatten 结果不符：%v", got)
	}
}

// ──────────────────────────── 构图方法 ────────────────────────────

// TestAddWorkflowComp_注册与组合面接线 对齐 _workflow.py:168-202。
func TestAddWorkflowComp_注册与组合面接线(t *testing.T) {
	bw := NewBaseWorkflow(nil, nil)
	if err := bw.AddWorkflowComp("s", newFakeStartComp(), boolPtr(false),
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil); err != nil {
		t.Fatalf("AddWorkflowComp：%v", err)
	}
	if _, ok := bw.Config().Spec.CompConfigs["s"]; !ok {
		t.Fatal("spec.CompConfigs 未登记")
	}
	if _, ok := bw.graph.GetNodes()["s"]; !ok {
		t.Fatal("组合面 AddComponent 未注册进图")
	}
}

// TestAddWorkflowComp_非法ID拒绝 校验先于注册（Python validate → NodeSpec → add_component）。
func TestAddWorkflowComp_非法ID拒绝(t *testing.T) {
	bw := NewBaseWorkflow(nil, nil)
	if err := bw.AddWorkflowComp("bad id", newFakeStartComp(), boolPtr(false),
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil); err == nil {
		t.Fatal("非法 ID 应拒绝")
	}
	if len(bw.Config().Spec.CompConfigs) != 0 {
		t.Fatal("拒绝后不应写 spec")
	}
}

// TestStartEndComp 对齐 _workflow.py:204-223。
func TestStartEndComp(t *testing.T) {
	bw := newTestWorkflow(t)
	if err := bw.StartComp("a"); err != nil {
		t.Fatalf("StartComp：%v", err)
	}
	if err := bw.EndComp("c"); err != nil {
		t.Fatalf("EndComp：%v", err)
	}
	if len(bw.workflowSpec.StartNodes) != 1 || bw.workflowSpec.StartNodes[0] != "a" {
		t.Fatalf("StartNodes 未登记：%v", bw.workflowSpec.StartNodes)
	}
}

// TestAddConnection_单源与多源 对齐 _workflow.py:225-243。
func TestAddConnection_单源与多源(t *testing.T) {
	bw := newTestWorkflow(t)
	if err := bw.AddConnection([]string{"a"}, "b"); err != nil {
		t.Fatalf("单源连接：%v", err)
	}
	if err := bw.AddConnection([]string{"a", "b"}, "c"); err != nil {
		t.Fatalf("多源连接：%v", err)
	}
	if got := bw.workflowSpec.Edges["a"]; len(got) != 2 || got[0] != "b" || got[1] != "c" {
		t.Fatalf("Edges[a] 不符：%v", got)
	}
	if got := bw.workflowSpec.Edges["b"]; len(got) != 1 || got[0] != "c" {
		t.Fatalf("Edges[b] 不符：%v", got)
	}
}

// TestAddStreamConnection_流消费者注册 对齐 _workflow.py:245-257。
func TestAddStreamConnection_流消费者注册(t *testing.T) {
	bw := newTestWorkflow(t)
	if err := bw.AddStreamConnection("a", "b"); err != nil {
		t.Fatalf("流连接：%v", err)
	}
	if got := bw.workflowSpec.StreamEdges["a"]; len(got) != 1 || got[0] != "b" {
		t.Fatalf("StreamEdges 不符：%v", got)
	}
}

// TestAddConditionalConnection_校验 对齐 _workflow.py:259-295（BranchRouter 分支留 8.19）。
func TestAddConditionalConnection_校验(t *testing.T) {
	bw := newTestWorkflow(t)
	if err := bw.AddConditionalConnection("", nil); err == nil {
		t.Fatal("空 src 应拒绝")
	}
}

// TestAutoCompleteAbilities_三段推断 对齐 _workflow.py:397-441 推断规则。
func TestAutoCompleteAbilities_三段推断(t *testing.T) {
	build := func(edges, streamEdges map[string][]string, abilities map[string][]components.ComponentAbility) *BaseWorkflow {
		bw := NewBaseWorkflow(nil, nil)
		ids := map[string]bool{}
		for s, ts := range edges {
			ids[s] = true
			for _, t2 := range ts {
				ids[t2] = true
			}
		}
		for s, ts := range streamEdges {
			ids[s] = true
			for _, t2 := range ts {
				ids[t2] = true
			}
		}
		for id := range ids {
			if err := bw.AddWorkflowComp(id, newFakeStartComp(), boolPtr(false),
				nil, nil, nil, nil, abilities[id], 0, -1.0, nil, nil); err != nil {
				t.Fatalf("注册 %s：%v", id, err)
			}
		}
		for s, ts := range edges {
			for _, t2 := range ts {
				_ = bw.AddConnection([]string{s}, t2)
			}
		}
		for s, ts := range streamEdges {
			for _, t2 := range ts {
				_ = bw.AddStreamConnection(s, t2)
			}
		}
		return bw
	}

	t.Run("流边源加普通目标_补STREAM", func(t *testing.T) {
		bw := build(map[string][]string{"p": {"q"}}, map[string][]string{"p": {"r"}}, nil)
		bw.AutoCompleteAbilities()
		abs := bw.workflowSpec.CompConfigs["p"].Abilities
		if !containsAbility(abs, components.ComponentAbilityStream) {
			t.Fatalf("p 应补 STREAM：%v", abs)
		}
	})
	t.Run("流边源也是流边目标_补TRANSFORM", func(t *testing.T) {
		// TRANSFORM = 流出 ∩ 流入（Python:423-424 source_stream ∩ target_stream）
		bw := build(nil, map[string][]string{"p": {"r"}, "y": {"p"}}, nil)
		bw.AutoCompleteAbilities()
		if !containsAbility(bw.workflowSpec.CompConfigs["p"].Abilities, components.ComponentAbilityTransform) {
			t.Fatal("p 应补 TRANSFORM")
		}
	})
	t.Run("流边目标有普通输出_补COLLECT", func(t *testing.T) {
		bw := build(map[string][]string{"r": {"s"}}, map[string][]string{"p": {"r"}}, nil)
		bw.AutoCompleteAbilities()
		if !containsAbility(bw.workflowSpec.CompConfigs["r"].Abilities, components.ComponentAbilityCollect) {
			t.Fatal("r 应补 COLLECT")
		}
	})
	t.Run("普通边目标且有普通输出_补INVOKE", func(t *testing.T) {
		// Python _complete_invoke_abilities：target_map 且 source_map 双重命中才补
		bw := build(map[string][]string{"a": {"b"}, "b": {"c"}}, nil, nil)
		bw.AutoCompleteAbilities()
		if !containsAbility(bw.workflowSpec.CompConfigs["b"].Abilities, components.ComponentAbilityInvoke) {
			t.Fatal("b（有入边+出边）应补 INVOKE")
		}
		if containsAbility(bw.workflowSpec.CompConfigs["c"].Abilities, components.ComponentAbilityInvoke) {
			t.Fatal("c（无出边）不应补 INVOKE")
		}
	})
	t.Run("用户已声明_不补", func(t *testing.T) {
		bw := build(map[string][]string{"a": {"b"}}, nil,
			map[string][]components.ComponentAbility{"b": {components.ComponentAbilityStream}})
		bw.AutoCompleteAbilities()
		if containsAbility(bw.workflowSpec.CompConfigs["b"].Abilities, components.ComponentAbilityInvoke) {
			t.Fatal("用户已声明不应补全")
		}
	})
}

// TestValidateEdgeNodes_缺注册节点panic 对齐 _workflow.py:363-376（raise→panic 构造期纪律）。
func TestValidateEdgeNodes_缺注册节点panic(t *testing.T) {
	bw := NewBaseWorkflow(nil, nil)
	_ = bw.AddWorkflowComp("a", newFakeStartComp(), boolPtr(false),
		nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	_ = bw.AddConnection([]string{"a"}, "ghost")
	defer func() {
		r := recover()
		if r == nil {
			t.Fatal("缺注册节点应 panic（Python build_error raise）")
		}
		if !strings.Contains(strings.ToLower(panicReason(r)), "component id mismatch") {
			t.Fatalf("panic 原因不符：%v", r)
		}
	}()
	bw.AutoCompleteAbilities()
}

// TestCompleteStreamSourceGroups_键格式 对齐 _workflow.py:449-479。
func TestCompleteStreamSourceGroups_键格式(t *testing.T) {
	bw := NewBaseWorkflow(nil, nil)
	_ = bw.AddWorkflowComp("p", newFakeStartComp(), boolPtr(false), nil, nil, nil, nil,
		[]components.ComponentAbility{components.ComponentAbilityStream}, 0, -1.0, nil, nil)
	_ = bw.AddWorkflowComp("q", newFakeStartComp(), boolPtr(false), nil, nil, nil, nil, nil, 0, -1.0, nil, nil)
	_ = bw.AddStreamConnection("p", "q")
	bw.AutoCompleteAbilities()
	groups := bw.workflowSpec.StreamSourceGroups["q"]
	if len(groups) != 1 || len(groups[0]) != 1 || groups[0][0] != "p-STREAM" {
		t.Fatalf("source_groups 键格式不符：%v", groups)
	}
}

// TestSourceToTargetMap_反转索引。
func TestSourceToTargetMap_反转索引(t *testing.T) {
	tm := sourceToTargetMap(map[string][]string{"a": {"b", "c"}})
	if len(tm["b"]) != 1 || tm["b"][0] != "a" || len(tm["c"]) != 1 {
		t.Fatalf("反转不符：%v", tm)
	}
}

// TestCompile_Panic包装COMPILE_ERROR 对齐 _workflow.py:310-315。
func TestCompile_Panic包装COMPILE_ERROR(t *testing.T) {
	bw := NewBaseWorkflow(nil, nil)
	// 无 start 节点直接编译 → graph.Compile panic（8.1）→ 包装为错误
	_, err := bw.Compile(nil, nil)
	if err == nil {
		t.Fatal("空图编译应报错")
	}
	if !strings.Contains(err.Error(), "workflow") {
		t.Fatalf("错误应携带 workflow 参数：%v", err)
	}
}

func containsAbility(abs []components.ComponentAbility, want components.ComponentAbility) bool {
	for _, a := range abs {
		if a == want {
			return true
		}
	}
	return false
}

func panicReason(r any) string {
	if err, ok := r.(error); ok {
		return err.Error()
	}
	return fmt.Sprint(r)
}

// newFakeStartComp 测试用最小组合件（替代 flow.NewStart：
// flow→workflow 与 workflow 测试→flow 会构成测试期 import cycle，
// 故根包测试内联组合件桩；仅注册面使用，不执行）。
type fakeStartExec struct {
	graphexe.BaseExecutable[map[string]any, map[string]any]
}

func newFakeStartComp() components.ComponentComposable {
	return &fakeComposable{exec: &fakeStartExec{}}
}

type fakeComposable struct {
	exec graphexe.GeneralExecutor
}

func (f *fakeComposable) AddComponent(g *graph.PregelGraph, nodeID string, waitForAll bool) error {
	opts := []graph.AddNodeOption{}
	if waitForAll {
		opts = append(opts, graph.WithWaitForAll())
	}
	g.AddNode(nodeID, f.exec, opts...)
	return nil
}

func (f *fakeComposable) ToExecutable() graphexe.GeneralExecutor { return f.exec }
