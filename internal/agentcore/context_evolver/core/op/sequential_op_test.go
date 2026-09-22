package op

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
)

// mockOp 用于测试的 mock 操作
type mockOp struct {
	name    string
	execute func(ctx context.Context, rc *cecontext.RuntimeContext) error
}

func (m *mockOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	if m.execute != nil {
		return m.execute(ctx, rc)
	}
	rc.Set(m.name+"_called", true)
	return nil
}

func TestSeq_单操作包装(t *testing.T) {
	op := &mockOp{name: "a"}
	seq := Seq(op)
	require.NotNil(t, seq)
	assert.Len(t, seq.Ops(), 1)
}

func TestPar_单操作包装(t *testing.T) {
	op := &mockOp{name: "a"}
	par := Par(op)
	require.NotNil(t, par)
	assert.Len(t, par.Ops(), 1)
}

func TestSequentialOp_Then_链式(t *testing.T) {
	rc := cecontext.NewRuntimeContext()
	op1 := &mockOp{name: "a"}
	op2 := &mockOp{name: "b"}
	op3 := &mockOp{name: "c"}

	flow := Seq(op1).Then(op2).Then(op3)
	err := flow.Execute(context.Background(), rc)
	require.NoError(t, err)

	assert.True(t, rc.Get("a_called").(bool))
	assert.True(t, rc.Get("b_called").(bool))
	assert.True(t, rc.Get("c_called").(bool))
}

func TestSequentialOp_Then_扁平化嵌套(t *testing.T) {
	rc := cecontext.NewRuntimeContext()
	inner := Seq(&mockOp{name: "a"}).Then(&mockOp{name: "b"})
	outer := Seq(&mockOp{name: "c"}).Then(inner)
	// inner 是 *SequentialOp，Then 应扁平化而非嵌套
	assert.Len(t, outer.Ops(), 3)

	// 执行验证
	err := outer.Execute(context.Background(), rc)
	require.NoError(t, err)
	assert.True(t, rc.Get("a_called").(bool))
	assert.True(t, rc.Get("b_called").(bool))
	assert.True(t, rc.Get("c_called").(bool))
}

func TestSequentialOp_中间失败短路(t *testing.T) {
	rc := cecontext.NewRuntimeContext()
	op1 := &mockOp{name: "a"}
	op2 := &mockOp{name: "b", execute: func(ctx context.Context, rc *cecontext.RuntimeContext) error {
		return fmt.Errorf("op2 failed")
	}}
	op3 := &mockOp{name: "c"}

	flow := Seq(op1).Then(op2).Then(op3)
	err := flow.Execute(context.Background(), rc)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "op2 failed")

	assert.True(t, rc.Get("a_called").(bool))
	assert.Nil(t, rc.Get("c_called")) // op3 未执行
}

func TestSequentialOp_数据传递(t *testing.T) {
	rc := cecontext.NewRuntimeContext()
	op1 := &mockOp{name: "a", execute: func(ctx context.Context, rc *cecontext.RuntimeContext) error {
		rc.Set("result", 42)
		return nil
	}}
	op2 := &mockOp{name: "b", execute: func(ctx context.Context, rc *cecontext.RuntimeContext) error {
		val := rc.Get("result")
		rc.Set("doubled", val.(int)*2)
		return nil
	}}

	flow := Seq(op1).Then(op2)
	err := flow.Execute(context.Background(), rc)
	require.NoError(t, err)
	assert.Equal(t, 84, rc.Get("doubled"))
}

func TestSequentialOp_Context取消(t *testing.T) {
	rc := cecontext.NewRuntimeContext()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	op := &mockOp{name: "a"}
	flow := Seq(op)
	err := flow.Execute(ctx, rc)
	assert.Error(t, err)
}

func TestSequentialOp_String(t *testing.T) {
	flow := Seq(&mockOp{name: "a"}).Then(&mockOp{name: "b"})
	s := flow.String()
	assert.Contains(t, s, "SequentialOp")
	assert.Contains(t, s, "2")
}
