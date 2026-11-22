package harnessrsi

import (
	"context"
	"errors"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrTaskCancelled 协作式取消哨兵错误（对齐 Python asyncio.CancelledError）。
var ErrTaskCancelled = errors.New("task cancelled")

// ──────────────────────────── 导出函数 ────────────────────────────

// RaiseIfCancelled 协作式取消检查入口：ctx 已取消/超时即返回 ErrTaskCancelled。
//
// 对齐 Python single_harness/iterative.py:91 _raise_if_cancelled（模块级函数，
// asyncio.current_task().cancelling() > 0 时抛 CancelledError）。Go 侧无 asyncio，
// 协作式取消等价物为 context.Context（偏差 D-17.7-1，见 spec
// docs/superpowers/specs/2026-11-22-rsi-17.7-task-lifecycle-design.md §5）。
// 17.7 已回填：检查点调用点见 singleharness/iterative.go raiseIfCancelled
// 包装（4 处循环边界）与 continuous.go（2 处循环边界）。
func RaiseIfCancelled(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return ErrTaskCancelled
	}
	return nil
}
