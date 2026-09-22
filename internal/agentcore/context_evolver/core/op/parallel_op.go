package op

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ParallelOp 并行组合。
//
// 使用 errgroup.Group 并行执行，任一失败 cancel 其余。
// 共享 RuntimeContext（需调用方保证并发安全，对齐 Python asyncio.gather 共享 context）。
// With 支持扁平化嵌套的 ParallelOp（对齐 Python ParallelOp.__or__）。
//
// Python: openjiuwen/extensions/context_evolver/core/op/parallel_op.py
type ParallelOp struct {
	ops []BaseOp
}

// ──────────────────────────── 导出方法 ────────────────────────────

// With 追加并行操作，返回扩展后的 ParallelOp。
//
// 对齐 Python ParallelOp.__or__(other)。
// 如果 other 是 *ParallelOp，则扁平化追加其所有操作（对齐 Python 的 __or__ 扁平化）。
func (p *ParallelOp) With(other BaseOp) *ParallelOp {
	if nested, ok := other.(*ParallelOp); ok {
		p.ops = append(p.ops, nested.ops...)
	} else {
		p.ops = append(p.ops, other)
	}
	return p
}

// Execute 并行执行所有操作。
// 对齐 Python ParallelOp.async_execute(context)，使用 asyncio.gather。
// Go 使用 errgroup.Group，任一操作失败会 cancel 其余操作。
// Python 的 asyncio.gather 不设 return_exceptions=True，异常会传播并取消 gather，Go 对齐此行为。
func (p *ParallelOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("parallel op context cancelled: %w", err)
	}
	g, gctx := errgroup.WithContext(ctx)
	for _, op := range p.ops {
		op := op // 捕获循环变量
		g.Go(func() error {
			return op.Execute(gctx, rc)
		})
	}
	if err := g.Wait(); err != nil {
		return fmt.Errorf("parallel op failed: %w", err)
	}
	return nil
}

// Ops 返回操作列表（测试用）。
func (p *ParallelOp) Ops() []BaseOp {
	return p.ops
}

// String 实现 Stringer 接口。
// 对齐 Python ParallelOp.__repr__()，显示 ops 用 | 连接。
func (p *ParallelOp) String() string {
	return fmt.Sprintf("ParallelOp(len=%d)", len(p.ops))
}
