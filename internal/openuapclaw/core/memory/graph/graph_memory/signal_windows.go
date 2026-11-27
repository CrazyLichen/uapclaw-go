//go:build windows

package graph_memory

import (
	"os"

	"golang.org/x/sys/windows"
)

// resendInterruptSignal Windows 无 syscall.Kill，用 GenerateConsoleCtrlEvent
// 向调用进程组发 CTRL_C_EVENT；失败则静默（Windows 下无对应重发语义）。
func resendInterruptSignal() error {
	return windows.GenerateConsoleCtrlEvent(windows.CTRL_C_EVENT, uint32(os.Getpid()))
}
