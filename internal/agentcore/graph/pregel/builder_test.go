package pregel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewPregelBuilder_预注册起止虚拟节点 构造期立即注册 START/END 两个虚拟节点
// 并各自配 TriggerChannel。Python: builder.py:14-19
func TestNewPregelBuilder_预注册起止虚拟节点(t *testing.T) {
	b := NewPregelBuilder()

	require.Len(t, b.nodes, 2)
	sn, ok := b.nodes[Start]
	require.True(t, ok)
	assert.Equal(t, Start, sn.name)
	require.Empty(t, sn.routers)

	en, ok := b.nodes[End]
	require.True(t, ok)
	assert.Equal(t, End, en.name)
	require.Empty(t, en.routers)

	require.Len(t, b.channels, 2)
	assert.Equal(t, Start, b.channels[0].Key())
	assert.Equal(t, End, b.channels[1].Key())
}

// TestAddNode_追加节点与触发通道 add_node 注册节点并追加 TriggerChannel，
// 重复名按 Python dict 赋值语义覆盖。Python: builder.py:21-26
func TestAddNode_追加节点与触发通道(t *testing.T) {
	b := NewPregelBuilder()
	fn := func(_ context.Context, _ NodeInput) error { return nil }

	ret := b.AddNode("a", fn)
	assert.Same(t, b, ret, "AddNode 必须返回 builder 自身（链式）")

	n, ok := b.nodes["a"]
	require.True(t, ok)
	assert.Equal(t, "a", n.name)
	require.Len(t, n.routers, 0)
	require.Len(t, b.channels, 3)
	assert.Equal(t, "a", b.channels[2].Key())

	// 重复名覆盖：dict 赋值语义，channels 仍追加一条（对齐 Python）
	b.AddNode("a", fn)
	assert.Len(t, b.nodes, 3)
	require.Len(t, b.channels, 4)
}

// TestAddNode_带routers 构造期传入的 routers 原样进入节点。
func TestAddNode_带routers(t *testing.T) {
	b := NewPregelBuilder()
	r := NewStaticRouter("x")
	b.AddNode("a", NoopNodeFunc, r)

	n := b.nodes["a"]
	require.Len(t, n.routers, 1)
	assert.Same(t, r, n.routers[0])
}

// TestAddEdge_单静态边 1→1 static：目标单节点。Python: builder.py:58-60
func TestAddEdge_单静态边(t *testing.T) {
	b := NewPregelBuilder()
	b.AddNode("a", NoopNodeFunc)
	b.AddNode("b", NoopNodeFunc)

	require.NoError(t, b.AddEdge("a", "b"))

	routers := b.nodes["a"].routers
	require.Len(t, routers, 1)
	sr, ok := routers[0].(*StaticRouter)
	require.True(t, ok)
	assert.Equal(t, []string{"b"}, sr.targets)
}

// TestAddEdge_多静态边 1→N static。Python: builder.py:54-56
func TestAddEdge_多静态边(t *testing.T) {
	b := NewPregelBuilder()
	b.AddNode("a", NoopNodeFunc)
	b.AddNode("b", NoopNodeFunc)
	b.AddNode("c", NoopNodeFunc)

	require.NoError(t, b.AddEdge("a", []string{"b", "c"}))

	sr, ok := b.nodes["a"].routers[0].(*StaticRouter)
	require.True(t, ok)
	assert.Equal(t, []string{"b", "c"}, sr.targets)
}

// TestAddEdge_屏障边 N→1 barrier：每源节点独占一组（全 AND 形态，偏差 A1）、
// 每个源节点挂 BarrierRouter 且目标为 BarrierChannel.Key()（只转手）。
// Python: builder.py:34-53
func TestAddEdge_屏障边(t *testing.T) {
	b := NewPregelBuilder()
	for _, name := range []string{"a", "b", "c"} {
		b.AddNode(name, NoopNodeFunc)
	}

	require.NoError(t, b.AddEdge([]string{"a", "b"}, "c"))

	// 通道追加：BarrierChannel 在第 6 位（Start/End/a/b/c 的 Trigger 之后）
	require.Len(t, b.channels, 6)
	barrier, ok := b.channels[5].(*BarrierChannel)
	require.True(t, ok)
	assert.Equal(t, "c", barrier.name)
	assert.Len(t, barrier.groups, 2)
	for i, want := range []string{"a", "b"} {
		group := barrier.groups[i]
		_, hit := group[want]
		assert.True(t, hit, "组 %d 应只含 %s", i, want)
	}

	// 每个源节点挂 BarrierRouter，目标 == barrier.Key()（合成路由键转手）
	for _, name := range []string{"a", "b"} {
		routers := b.nodes[name].routers
		require.Len(t, routers, 1)
		br, ok := routers[0].(*BarrierRouter)
		require.True(t, ok)
		assert.Equal(t, []string{barrier.Key()}, br.targets)
	}
}

// TestAddEdge_非法组合返回错误 非 string/[]string 组合一律报错，
// 文案含两端的 Go 类型。Python: builder.py:62-63（ValueError）
func TestAddEdge_非法组合返回错误(t *testing.T) {
	b := NewPregelBuilder()
	err := b.AddEdge([]string{"a"}, []string{"b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported edge format")

	err = b.AddEdge(1, "b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported edge format")

	err = b.AddEdge("a", 2)
	require.Error(t, err)
}

// TestAddEdge_未注册源节点报错 偏差 A2：Python KeyError → Go error。
func TestAddEdge_未注册源节点报错(t *testing.T) {
	b := NewPregelBuilder()
	b.AddNode("b", NoopNodeFunc)

	err := b.AddEdge("ghost", "b")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown node: ghost")
}

// TestAddBranch_追加条件路由 selector 原样进入 ConditionalRouter，链式返回。
// Python: builder.py:67-69
func TestAddBranch_追加条件路由(t *testing.T) {
	b := NewPregelBuilder()
	b.AddNode("a", NoopNodeFunc)
	sel := SelectorFunc(func(_ context.Context, _ any) ([]string, error) {
		return []string{"b"}, nil
	})

	ret := b.AddBranch("a", sel)
	assert.Same(t, b, ret)

	cr, ok := b.nodes["a"].routers[0].(*ConditionalRouter)
	require.True(t, ok)
	require.NotNil(t, cr.selector)
	got, err := cr.selector.Select(context.Background(), nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"b"}, got, "selector 原样进入 ConditionalRouter")
}

// TestAddBranch_未注册源节点panic 偏差 A2：保持链式签名，未注册 src
// 显式 panic（对齐 NewPregelNode 构造期 panic 纪律）。Python KeyError。
func TestAddBranch_未注册源节点panic(t *testing.T) {
	b := NewPregelBuilder()
	assert.PanicsWithValue(t, "unknown node: ghost", func() {
		b.AddBranch("ghost", SelectorFunc(nil))
	})
}

// TestPregelBuilder_Build Build 交付调度引擎（Python builder.py:71-77；8.3a 方案 A 回填兑现）
func TestPregelBuilder_Build(t *testing.T) {
	b := NewPregelBuilder()
	b.AddNode("a", NoopNodeFunc)
	require.NoError(t, b.AddEdge(Start, "a"))
	require.NoError(t, b.AddEdge("a", End))
	sv := &fakeSaver{}
	hooked := 0
	pg := b.Build(sv, func(_ context.Context, loop *PregelLoop) {
		hooked++
		loop.Step += 100 // D6 字段导出的消费面验证
	})
	assert.Same(t, sv, pg.Store)
	assert.Equal(t, Start, pg.Initial)
	require.Len(t, pg.Nodes, 3) // START/END/a
	res, err := pg.Run(context.Background(), nil)
	assert.NoError(t, err)
	assert.Empty(t, res)
	assert.Equal(t, 2, hooked) // step0(START)/step1(a)
}
