package harnessrsi

import (
	"context"
	"errors"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ErrTaskCancelled 协作式取消哨兵错误（对齐 Python asyncio.CancelledError 的桩期形态）。
var ErrTaskCancelled = errors.New("task cancelled")

// ──────────────────────────── 导出函数 ────────────────────────────

// RaiseIfCancelled 协作式取消检查入口：ctx 已取消/超时即返回 ErrTaskCancelled。
//
// 对齐 Python single_harness/iterative.py:91 _raise_if_cancelled（模块级函数，
// asyncio.current_task().cancelling() > 0 时抛 CancelledError）。Go 侧无 asyncio，
// 协作式取消等价物为 context.Context。
//
// ⤴️ 17.4f 登记回填：17.7 协作式终止落地后按规则 8 删除本桩；调用点由 17.4d
// 按 Python 原位置接线（iterative.py 4 处 + continuous.py 2 处）。届时本文件
// 随包迁移至 internal/openjiuwen/agent_evolving/rsi/harness_rsi/single_harness/。
func RaiseIfCancelled(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return ErrTaskCancelled
	}
	return nil
}
