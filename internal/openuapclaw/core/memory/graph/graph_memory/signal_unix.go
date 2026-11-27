//go:build !windows

package graph_memory

import "syscall"

// resendInterruptSignal 收到 SIGINT 后恢复时向自身重发中断信号
// （对齐 Python 的 signal.default_int_handler 重发语义）。
func resendInterruptSignal() error {
	return syscall.Kill(syscall.Getpid(), syscall.SIGINT)
}
