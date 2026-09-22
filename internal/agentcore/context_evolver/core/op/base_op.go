package op

import (
	"context"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
)

// ──────────────────────────── 接口 ────────────────────────────

// BaseOp 操作接口。
//
// 操作是可组合的计算原子，通过 Then（顺序）和 With（并行）组合成流水线。
// Python 用 __rshift__ (>>) 和 __or__ (|) 运算符，Go 用方法链。
//
// Python BaseOp.__call__ 返回同一个 context 对象（原地变异），
// Go 改为 Execute 返回 error，通过 RuntimeContext 传递中间结果。
//
// Python: openjiuwen/extensions/context_evolver/core/op/base_op.py
type BaseOp interface {
	// Execute 执行操作，读写 RuntimeContext
	Execute(ctx context.Context, rc *cecontext.RuntimeContext) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Seq 单操作包装为 SequentialOp（链式起点）。
// 对齐 Python BaseOp.__rshift__(other) → SequentialOp(self, other)。
func Seq(op BaseOp) *SequentialOp {
	return &SequentialOp{ops: []BaseOp{op}}
}

// Par 单操作包装为 ParallelOp（链式起点）。
// 对齐 Python BaseOp.__or__(other) → ParallelOp(self, other)。
func Par(op BaseOp) *ParallelOp {
	return &ParallelOp{ops: []BaseOp{op}}
}
