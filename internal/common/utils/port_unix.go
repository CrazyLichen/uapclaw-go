// port_unix.go 实现 Unix 平台的进程存在性检查。
// 使用 syscall.Kill(pid, 0) 检查进程是否存在（信号 0 不发送信号，仅检查权限/存在性）。

//go:build !windows

package utils

import (
	"syscall"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// processExists 检查指定 PID 的进程是否存在。
// Unix 实现：syscall.Kill(pid, 0) 返回 syscall.ESRCH 表示进程不存在。
func processExists(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err != syscall.ESRCH
}
