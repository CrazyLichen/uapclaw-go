package pregel

import (
	"context"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubRouter 测试用路由：只带一个标识，供断言挂载顺序与身份；Dispatch 恒返回空。
type stubRouter struct {
	id string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Dispatch 恒返回空消息，本类型只用于断言节点持有的路由身份。
func (s *stubRouter) Dispatch(context.Context, string) ([]Message, error) {
	return nil, nil
}

// TestNodeFunc_接收NodeInput 节点函数按 NodeFunc 签名被调用时，ctx 与 NodeInput 原样透传，State 为 nil。
// Python: task.py:137-154（按形参名嗅探后拼 kwargs 调用 func，state 恒 None）
func TestNodeFunc_接收NodeInput(t *testing.T) {
	type ctxKey string
	key := ctxKey("trace")
	ctx := context.WithValue(context.Background(), key, "abc")

	var gotCtx context.Context
	var gotIn NodeInput
	fn := NodeFunc(func(c context.Context, in NodeInput) error {
		gotCtx = c
		gotIn = in
		return nil
	})

	in := NodeInput{Config: InnerPregelConfig{PregelConfig: PregelConfig{Ns: "wf", SessionID: "s1"}}}
	if err := fn(ctx, in); err != nil {
		t.Fatalf("节点函数返回错误：%v", err)
	}
	if gotCtx.Value(key) != "abc" {
		t.Errorf("ctx 未透传：期望 %q", "abc")
	}
	if gotIn.Config.Ns != "wf" || gotIn.Config.SessionID != "s1" {
		t.Errorf("Config 未透传：Ns=%q SessionID=%q", gotIn.Config.Ns, gotIn.Config.SessionID)
	}
	if gotIn.State != nil {
		t.Errorf("State 应为 nil，实际 %v", gotIn.State)
	}
}

// TestNewPregelNode_字段与顺序 构造出的节点保住 name、fn 与路由顺序。
// Python: base.py:46-50（三行赋值，routers 保序）
func TestNewPregelNode_字段与顺序(t *testing.T) {
	r1 := &stubRouter{id: "r1"}
	r2 := &stubRouter{id: "r2"}
	n := NewPregelNode("A", NoopNodeFunc, r1, r2)

	if n.name != "A" {
		t.Errorf("name = %q，期望 \"A\"", n.name)
	}
	if n.fn == nil {
		t.Fatal("fn 不应为 nil")
	}
	if len(n.routers) != 2 {
		t.Fatalf("routers 长度 = %d，期望 2", len(n.routers))
	}
	if n.routers[0] != Router(r1) || n.routers[1] != Router(r2) {
		t.Errorf("routers 顺序或身份不符：%v", n.routers)
	}
}

// TestNewPregelNode_复制入参切片 外部改动用于展开的切片，不影响节点内部的路由列表。
// 偏差 D3：Python base.py:50 存引用，Go 构造期复制（同 8.5 D5）。
func TestNewPregelNode_复制入参切片(t *testing.T) {
	r1 := &stubRouter{id: "r1"}
	r2 := &stubRouter{id: "r2"}
	list := []Router{r1, r2}
	n := NewPregelNode("A", NoopNodeFunc, list...)

	list[0] = &stubRouter{id: "changed"}

	if n.routers[0] != Router(r1) {
		t.Errorf("节点内部切片被外部改动污染：%v", n.routers[0])
	}
}

// TestNewPregelNode_nilFnPanics 节点函数为 nil 时构造期 panic。
// 偏差 D4：Python 要等到执行期 inspect.signature(None) 才抛 TypeError。
func TestNewPregelNode_nilFnPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("fn 为 nil 时期望 panic，未发生")
		}
	}()
	NewPregelNode("A", nil)
}

// TestNewPregelNode_nilRouterPanics nil 路由夹在合法路由中间也必须被拦下。
// 偏差 D4：Python 对 None router 是调用期 AttributeError。
func TestNewPregelNode_nilRouterPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("router 为 nil 时期望 panic，未发生")
		}
	}()
	NewPregelNode("A", NoopNodeFunc, &stubRouter{id: "r1"}, nil)
}

// TestAddRouter_追加保序 builder 在构图期按边声明顺序追加路由，节点侧必须保序。
// Python: builder.py:49-68（nodes[src].routers.append(...) 保序）
func TestAddRouter_追加保序(t *testing.T) {
	n := NewPregelNode("A", NoopNodeFunc)
	r1 := &stubRouter{id: "r1"}
	r2 := &stubRouter{id: "r2"}
	r3 := &stubRouter{id: "r3"}

	n.addRouter(r1)
	n.addRouter(r2)
	n.addRouter(r3)

	if len(n.routers) != 3 {
		t.Fatalf("routers 长度 = %d，期望 3", len(n.routers))
	}
	if n.routers[0] != Router(r1) || n.routers[1] != Router(r2) || n.routers[2] != Router(r3) {
		t.Errorf("追加顺序不符：%v", n.routers)
	}
	if n.name != "A" || n.fn == nil {
		t.Errorf("追加路由影响了 name/fn：name=%q", n.name)
	}
}

// TestAddRouter_nilPanics 追加 nil 路由与构造期同口径 panic。
// 偏差 D4：Python 无此方法，直接 append(None) 要到 dispatch 才炸。
func TestAddRouter_nilPanics(t *testing.T) {
	n := NewPregelNode("A", NoopNodeFunc)
	defer func() {
		if recover() == nil {
			t.Fatal("追加 nil 路由时期望 panic，未发生")
		}
	}()
	n.addRouter(nil)
}

// TestNoopNodeFunc_返nil 空节点函数恒返回 nil，且不因入参取值而改变；用于 START/END 占位。
// Python: builder.py:18-19（add_node(START, lambda: None, [])）
func TestNoopNodeFunc_返nil(t *testing.T) {
	cases := []NodeInput{
		{},
		{Config: InnerPregelConfig{PregelConfig: PregelConfig{Ns: "wf", SessionID: "s1"}}},
	}
	for _, in := range cases {
		if err := NoopNodeFunc(context.Background(), in); err != nil {
			t.Errorf("NoopNodeFunc 返回错误：%v", err)
		}
	}
}
