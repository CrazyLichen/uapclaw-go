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
