package op

import (
	"context"
	"fmt"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_evolver/core/context"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SequentialOp 顺序组合。
//
// 按 ops 顺序依次执行，任一失败立即返回 error。
// Then 支持扁平化嵌套的 SequentialOp（对齐 Python SequentialOp.__rshift__）。
//
// Python: openjiuwen/extensions/context_evolver/core/op/sequential_op.py
type SequentialOp struct {
	ops []BaseOp
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Then 追加操作，返回扩展后的 SequentialOp。
//
// 对齐 Python SequentialOp.__rshift__(other)。
// 如果 other 是 *SequentialOp，则扁平化追加其所有操作（对齐 Python 的 __rshift__ 扁平化）。
func (s *SequentialOp) Then(other BaseOp) *SequentialOp {
	if nested, ok := other.(*SequentialOp); ok {
		s.ops = append(s.ops, nested.ops...)
	} else {
		s.ops = append(s.ops, other)
	}
	return s
}

// Execute 顺序执行所有操作。
// 对齐 Python SequentialOp.async_execute(context)。
// 任一操作失败立即返回 error，后续操作不执行。
// 如果 context 被取消，返回 context 错误。
// Python 的 asyncio 顺序执行等价于 Go 的 for 循环。
func (s *SequentialOp) Execute(ctx context.Context, rc *cecontext.RuntimeContext) error {
	for _, op := range s.ops {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("sequential op context cancelled: %w", err)
		}
		if err := op.Execute(ctx, rc); err != nil {
			return fmt.Errorf("sequential op failed: %w", err)
		}
	}
	return nil
}

// Ops 返回操作列表（测试用）。
func (s *SequentialOp) Ops() []BaseOp {
	return s.ops
}

// String 实现 Stringer 接口。
// 对齐 Python SequentialOp.__repr__()，显示 ops 用 >> 连接。
func (s *SequentialOp) String() string {
	return fmt.Sprintf("SequentialOp(len=%d)", len(s.ops))
}
