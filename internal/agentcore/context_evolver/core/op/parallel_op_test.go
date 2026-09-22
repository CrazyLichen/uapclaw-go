package op

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
)

func TestParallelOp_With_链式(t *testing.T) {
	rc := cecontext.NewRuntimeContext()
	op1 := &mockOp{name: "a"}
	op2 := &mockOp{name: "b"}

	flow := Par(op1).With(op2)
	err := flow.Execute(context.Background(), rc)
	require.NoError(t, err)

	assert.True(t, rc.Get("a_called").(bool))
	assert.True(t, rc.Get("b_called").(bool))
}

func TestParallelOp_With_扁平化嵌套(t *testing.T) {
	inner := Par(&mockOp{name: "a"}).With(&mockOp{name: "b"})
	outer := Par(&mockOp{name: "c"}).With(inner)
	assert.Len(t, outer.Ops(), 3)
}

func TestParallelOp_并行执行(t *testing.T) {
	rc := cecontext.NewRuntimeContext()
	var counter atomic.Int32

	makeOp := func(name string) *mockOp {
		return &mockOp{name: name, execute: func(ctx context.Context, rc *cecontext.RuntimeContext) error {
			counter.Add(1)
			rc.Set(name+"_called", true)
			return nil
		}}
	}

	flow := Par(makeOp("a")).With(makeOp("b")).With(makeOp("c"))
	err := flow.Execute(context.Background(), rc)
	require.NoError(t, err)
	assert.Equal(t, int32(3), counter.Load())
}

func TestParallelOp_单失败取消其余(t *testing.T) {
	rc := cecontext.NewRuntimeContext()
	op1 := &mockOp{name: "a", execute: func(ctx context.Context, rc *cecontext.RuntimeContext) error {
		return fmt.Errorf("op1 failed")
	}}
	op2 := &mockOp{name: "b", execute: func(ctx context.Context, rc *cecontext.RuntimeContext) error {
		<-ctx.Done()
		return ctx.Err()
	}}

	flow := Par(op1).With(op2)
	err := flow.Execute(context.Background(), rc)
	require.Error(t, err)
}

func TestParallelOp_Then_混合顺序并行(t *testing.T) {
	rc := cecontext.NewRuntimeContext()
	loadOp := &mockOp{name: "load", execute: func(ctx context.Context, rc *cecontext.RuntimeContext) error {
		rc.Set("loaded", true)
		return nil
	}}
	reflectOp := &mockOp{name: "reflect", execute: func(ctx context.Context, rc *cecontext.RuntimeContext) error {
		rc.Set("reflected", true)
		return nil
	}}
	parallelReflectOp := &mockOp{name: "parallel_reflect", execute: func(ctx context.Context, rc *cecontext.RuntimeContext) error {
		rc.Set("parallel_reflected", true)
		return nil
	}}
	applyOp := &mockOp{name: "apply", execute: func(ctx context.Context, rc *cecontext.RuntimeContext) error {
		rc.Set("applied", true)
		return nil
	}}

	// 对齐 Python: flow = LoadPlaybookOp() >> (ReflectOp() | ParallelReflectOp()) >> ApplyDeltaOp()
	// Go: Seq(load).Then(Par(reflect).With(parallelReflect)).Then(apply)
	flow := Seq(loadOp).Then(Par(reflectOp).With(parallelReflectOp)).Then(applyOp)
	err := flow.Execute(context.Background(), rc)
	require.NoError(t, err)

	assert.True(t, rc.Get("loaded").(bool))
	assert.True(t, rc.Get("reflected").(bool))
	assert.True(t, rc.Get("parallel_reflected").(bool))
	assert.True(t, rc.Get("applied").(bool))
}

func TestParallelOp_Context取消(t *testing.T) {
	rc := cecontext.NewRuntimeContext()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	op := &mockOp{name: "a"}
	flow := Par(op)
	err := flow.Execute(ctx, rc)
	assert.Error(t, err)
}

func TestParallelOp_String(t *testing.T) {
	flow := Par(&mockOp{name: "a"}).With(&mockOp{name: "b"})
	s := flow.String()
	assert.Contains(t, s, "ParallelOp")
	assert.Contains(t, s, "2")
}
