package auto_permission

import (
	"context"
	"reflect"
	"testing"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// boundOwner 用于测试匹配的接收者
type boundOwner struct {
	// tag 区分实例
	tag string
}

// Do 目标方法
func (o *boundOwner) Do() {}

// Other 另一个方法，用于验证函数不同
func (o *boundOwner) Other() {}

// TestMatchesBoundMethod_正常 验证接收者与函数都相同时为 true
func TestMatchesBoundMethod_正常(t *testing.T) {
	owner := &boundOwner{tag: "a"}
	if !MatchesBoundMethod(owner, (*boundOwner).Do, owner, (*boundOwner).Do) {
		t.Fatal("接收者与函数均相同应返回 true")
	}
}

// TestMatchesBoundMethod_owner为nil 验证 expectedOwner 为 nil 时短路
func TestMatchesBoundMethod_owner为nil(t *testing.T) {
	owner := &boundOwner{tag: "a"}
	if MatchesBoundMethod(owner, (*boundOwner).Do, nil, (*boundOwner).Do) {
		t.Fatal("expectedOwner 为 nil 应返回 false")
	}
}

// TestMatchesBoundMethod_func为nil 验证 expectedFunc 为 nil 时短路
func TestMatchesBoundMethod_func为nil(t *testing.T) {
	owner := &boundOwner{tag: "a"}
	if MatchesBoundMethod(owner, (*boundOwner).Do, owner, nil) {
		t.Fatal("expectedFunc 为 nil 应返回 false")
	}
}

// TestMatchesBoundMethod_receiver不同 验证不同实例的同名方法判不匹配
//
// 这是本函数存在的核心理由：Go 里 a.Do 与 b.Do 的代码指针相同，
// 只有显式比较 receiver 才能区分。
func TestMatchesBoundMethod_receiver不同(t *testing.T) {
	a := &boundOwner{tag: "a"}
	b := &boundOwner{tag: "b"}
	if MatchesBoundMethod(a, (*boundOwner).Do, b, (*boundOwner).Do) {
		t.Fatal("接收者不同应返回 false")
	}
}

// TestMatchesBoundMethod_函数不同 验证同一接收者的不同方法判不匹配
func TestMatchesBoundMethod_函数不同(t *testing.T) {
	owner := &boundOwner{tag: "a"}
	if MatchesBoundMethod(owner, (*boundOwner).Other, owner, (*boundOwner).Do) {
		t.Fatal("函数不同应返回 false")
	}
}

// TestMatchesBoundMethod_非指针接收者 验证值类型接收者的同一性判断
func TestMatchesBoundMethod_非指针接收者(t *testing.T) {
	type valueOwner struct{ tag string }
	if MatchesBoundMethod(valueOwner{tag: "a"}, valueOwner{tag: "a"}, valueOwner{tag: "a"}, valueOwner{tag: "a"}) {
		t.Fatal("值类型不同实例（即使字段相等）应返回 false")
	}
}

// ──────────────────────────── fake 设施 ────────────────────────────

// fakeAbilityManager 只实现 Get，其余方法返回零值。
// AbilityManagerInterface 共 10 个方法，按规则 10.1-2「测试替身自己补齐」
// 逐个空写。
type fakeAbilityManager struct {
	// abilities 按名称索引的能力
	abilities map[string]commonschema.Ability
}

func (f *fakeAbilityManager) Add(commonschema.Ability) saschema.AddAbilityResult {
	return saschema.AddAbilityResult{}
}

func (f *fakeAbilityManager) AddMany([]commonschema.Ability) []saschema.AddAbilityResult {
	return nil
}

func (f *fakeAbilityManager) Remove(string) commonschema.Ability { return nil }

func (f *fakeAbilityManager) RemoveMany([]string) []commonschema.Ability { return nil }

func (f *fakeAbilityManager) Get(name string) commonschema.Ability {
	return f.abilities[name]
}

func (f *fakeAbilityManager) List() []commonschema.Ability { return nil }

func (f *fakeAbilityManager) ListToolInfo(context.Context, []string, ...string) ([]commonschema.ToolInfoInterface, error) {
	return nil, nil
}

func (f *fakeAbilityManager) Execute(
	context.Context,
	*agentinterfaces.AgentCallbackContext,
	[]*llmschema.ToolCall,
	sessioninterfaces.SessionFacade,
	string,
	bool,
) []saschema.ExecuteResult {
	return nil
}

func (f *fakeAbilityManager) SetContextEngine(ceinterface.ContextEngine) {}

func (f *fakeAbilityManager) ReorderTools([]string) {}

// fakeTool 最小工具实现，用于精确类型校验
type fakeTool struct {
	// card 工具卡片
	card *tool.ToolCard
}

func (f *fakeTool) Card() *tool.ToolCard { return f.card }

func (f *fakeTool) Invoke(context.Context, map[string]any, ...tool.ToolOption) (map[string]any, error) {
	return map[string]any{"ok": true}, nil
}

func (f *fakeTool) Stream(context.Context, map[string]any, ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, nil
}

func (f *fakeTool) GetCallTimeout(map[string]any, float64) float64 { return 0 }

// fakeToolSub fakeTool 的子类，用于验证「子类不算合格」
type fakeToolSub struct {
	fakeTool
}

// fakeAbility 仅实现 Ability 三方法，用于构造非 ToolCard 能力
type fakeAbility struct {
	// name 能力名称
	name string
	// id 能力标识
	id string
}

func (f fakeAbility) AbilityName() string { return f.name }

func (f fakeAbility) AbilityID() string { return f.id }

func (f fakeAbility) AbilityKind() commonschema.AbilityKind { return commonschema.AbilityKindTool }

// reflectTypeOfTool 取实例的 reflect.Type
func reflectTypeOfTool(v any) reflect.Type { return reflect.TypeOf(v) }

// ──────────────────────────── ResolveToolBinding 测试 ────────────────────────────

// withResolveToolByID 替换工具查找实现并在测试结束恢复
func withResolveToolByID(t *testing.T, fn func(string) tool.Tool) {
	t.Helper()
	orig := resolveToolByID
	SetResolveToolByIDFunc(fn)
	t.Cleanup(func() { resolveToolByID = orig })
}

// TestResolveToolBinding_正常 验证类型与卡片都匹配时返回该实例
func TestResolveToolBinding_正常(t *testing.T) {
	card := tool.NewToolCardWithID("t1", "read_file", "读文件", nil, nil)
	impl := &fakeTool{card: card}
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{"read_file": card}}
	withResolveToolByID(t, func(string) tool.Tool { return impl })

	got := ResolveToolBinding(am, "read_file", reflectTypeOfTool(&fakeTool{}))
	if got != tool.Tool(impl) {
		t.Fatalf("应返回注册的实现，实际 %v", got)
	}
}

// TestResolveToolBinding_能力不存在 验证 am.Get 返回 nil 时返回 nil
func TestResolveToolBinding_能力不存在(t *testing.T) {
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{}}
	withResolveToolByID(t, func(string) tool.Tool {
		t.Fatal("能力不存在时不应查找工具")
		return nil
	})

	got := ResolveToolBinding(am, "nope", reflectTypeOfTool(&fakeTool{}))
	if got != nil {
		t.Fatalf("应返回 nil，实际 %v", got)
	}
}

// TestResolveToolBinding_类型不匹配 验证 expectedType 不符时返回 nil
func TestResolveToolBinding_类型不匹配(t *testing.T) {
	card := tool.NewToolCardWithID("t1", "read_file", "读文件", nil, nil)
	impl := &fakeTool{card: card}
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{"read_file": card}}
	withResolveToolByID(t, func(string) tool.Tool { return impl })

	// 用 fakeToolSub 的类型做期望，实际实现是 fakeTool
	got := ResolveToolBinding(am, "read_file", reflectTypeOfTool(&fakeToolSub{}))
	if got != nil {
		t.Fatalf("应返回 nil，实际 %v", got)
	}
}

// TestResolveToolBinding_子类不算合格 验证子类实现在期望父类类型时不通过
func TestResolveToolBinding_子类不算合格(t *testing.T) {
	card := tool.NewToolCardWithID("t1", "read_file", "读文件", nil, nil)
	impl := &fakeToolSub{fakeTool: fakeTool{card: card}}
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{"read_file": card}}
	withResolveToolByID(t, func(string) tool.Tool { return impl })

	got := ResolveToolBinding(am, "read_file", reflectTypeOfTool(&fakeTool{}))
	if got != nil {
		t.Fatalf("子类实现不应被判为父类，实际返回 %v", got)
	}
}

// TestResolveToolBinding_卡片不同一 验证实现持有的卡片与注册卡片不同时判不合格
func TestResolveToolBinding_卡片不同一(t *testing.T) {
	registered := tool.NewToolCardWithID("t1", "read_file", "读文件", nil, nil)
	other := tool.NewToolCardWithID("t1", "read_file", "读文件", nil, nil)
	impl := &fakeTool{card: other}
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{"read_file": registered}}
	withResolveToolByID(t, func(string) tool.Tool { return impl })

	got := ResolveToolBinding(am, "read_file", reflectTypeOfTool(&fakeTool{}))
	if got != nil {
		t.Fatalf("卡片不同一应返回 nil，实际 %v", got)
	}
}

// TestResolveToolBinding_非ToolCard能力 验证能力不是 *tool.ToolCard 时返回 nil
//
// Python 的顺序是先 get_tool 再判类型，因此这里**同样会**按卡片 ID 查一次工具
// （workflow 的 id 在工具表里查不到，返回 None），随后 type(None) is expected_type
// 为 False 短路，安静返回 None。Go 与之一致：先查（返回 nil）即提前返回。
func TestResolveToolBinding_非ToolCard能力(t *testing.T) {
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{
		"some_workflow": fakeAbility{name: "some_workflow", id: "w1"},
	}}
	lookedUp := ""
	withResolveToolByID(t, func(id string) tool.Tool {
		lookedUp = id
		return nil
	})

	got := ResolveToolBinding(am, "some_workflow", reflectTypeOfTool(&fakeTool{}))
	if lookedUp != "w1" {
		t.Fatalf("应按卡片 ID w1 查找工具（对齐 Python get_tool(card.id)），实际 %q", lookedUp)
	}
	if got != nil {
		t.Fatalf("应返回 nil，实际 %v", got)
	}
}

// TestResolveToolBinding_工具不存在 验证卡在而工具不在时返回 nil
//
// Python 此时 get_tool 返回 None（不抛异常），Go 同样返回 nil。
func TestResolveToolBinding_工具不存在(t *testing.T) {
	card := tool.NewToolCardWithID("t1", "read_file", "读文件", nil, nil)
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{"read_file": card}}
	withResolveToolByID(t, func(string) tool.Tool { return nil })

	got := ResolveToolBinding(am, "read_file", reflectTypeOfTool(&fakeTool{}))
	if got != nil {
		t.Fatalf("应返回 nil，实际 %v", got)
	}
}

// ──────────────────────────── 辅助函数分支测试 ────────────────────────────

// TestIdentityEqual_nil与类型不同 验证 nil 短路与类型不一致分支
func TestIdentityEqual_nil与类型不同(t *testing.T) {
	if identityEqual(nil, "x") {
		t.Fatal("nil 与非 nil 应不相等")
	}
	if identityEqual("x", nil) {
		t.Fatal("非 nil 与 nil 应不相等")
	}
	if identityEqual(nil, nil) {
		t.Fatal("nil 与 nil 应不相等（Python is 语义由调用方短路）")
	}
	if identityEqual("x", 1) {
		t.Fatal("类型不同应不相等")
	}
}

// TestIdentityEqual_map与slice 验证引用类型走指针比较
func TestIdentityEqual_map与slice(t *testing.T) {
	m1 := map[string]int{"a": 1}
	m2 := map[string]int{"a": 1}
	if !identityEqual(m1, m1) {
		t.Fatal("同一 map 应相等")
	}
	if identityEqual(m1, m2) {
		t.Fatal("不同 map 实例应不相等（即使内容相同）")
	}
	s1 := []int{1}
	s2 := []int{1}
	if !identityEqual(s1, s1) {
		t.Fatal("同一 slice 应相等")
	}
	if identityEqual(s1, s2) {
		t.Fatal("不同 slice 实例应不相等")
	}
}

// TestIdentityEqual_值类型相等 验证非引用类型走 == 比较
func TestIdentityEqual_值类型相等(t *testing.T) {
	if !identityEqual(42, 42) {
		t.Fatal("相同整数应相等")
	}
	if identityEqual(42, 43) {
		t.Fatal("不同整数应不相等")
	}
}

// TestFuncPtrEqual_非函数与类型不同 验证非函数值与签名不一致的分支
func TestFuncPtrEqual_非函数与类型不同(t *testing.T) {
	if funcPtrEqual("not-func", "not-func") {
		t.Fatal("非函数值应返回 false")
	}
	f1 := func() {}
	var f2 func(int)
	if funcPtrEqual(f1, f2) {
		t.Fatal("签名不同的函数应返回 false")
	}
	if funcPtrEqual(nil, f1) {
		t.Fatal("nil 与函数应返回 false")
	}
}

// TestSetResolveToolByIDFunc_nil不覆盖 验证传 nil 时保留原实现
func TestSetResolveToolByIDFunc_nil不覆盖(t *testing.T) {
	orig := resolveToolByID
	SetResolveToolByIDFunc(nil)
	if reflect.ValueOf(resolveToolByID).Pointer() != reflect.ValueOf(orig).Pointer() {
		t.Fatal("传 nil 不应覆盖原实现")
	}
}

// TestResolveToolBinding_非ToolCard但工具存在 验证断言失败分支
//
// 工具能按 ID 查到、类型也对，但卡片不是 *tool.ToolCard 时必须判不合格。
// Python 侧等价场景：card 非 AbilityCard 实例时 type(resource) is expected_type
// 可能仍为真，但 resource.card is card 必然为假。
func TestResolveToolBinding_非ToolCard但工具存在(t *testing.T) {
	card := tool.NewToolCardWithID("w1", "some_workflow", "工作流", nil, nil)
	impl := &fakeTool{card: card}
	am := &fakeAbilityManager{abilities: map[string]commonschema.Ability{
		"some_workflow": fakeAbility{name: "some_workflow", id: "w1"},
	}}
	withResolveToolByID(t, func(string) tool.Tool { return impl })

	got := ResolveToolBinding(am, "some_workflow", reflectTypeOfTool(&fakeTool{}))
	if got != nil {
		t.Fatalf("卡片非 *tool.ToolCard 时应返回 nil，实际 %v", got)
	}
}
