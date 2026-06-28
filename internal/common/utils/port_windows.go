// port_windows.go 实现 Windows 平台的进程存在性检查。
// 使用 os.FindProcess + Signal 检查进程是否存在。

//go:build windows

package utils

import (
	"os"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// processExists 检查指定 PID 的进程是否存在。
// Windows 实现：os.FindProcess 在 Windows 上始终成功，
// 通过发送 signal 0 检查进程是否存在。
func processExists(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 在 Windows 上不发送信号，仅检查进程是否存在
	err = proc.Signal(nil)
	return err == nil
}
