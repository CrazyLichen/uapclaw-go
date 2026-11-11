//go:build windows

package processcli

import (
	"os"

	"golang.org/x/sys/windows"
)

// dupStdoutToStderr Windows 实现：DuplicateHandle 出可写副本，再 SetStdHandle 改 fd1。
// Python: machine_entry.py:66-80（os.dup + os.dup2）
//
// 平台绑定代码：按项目规范 3.2 第 3 条用 build tag 隔离，不纳入非 Windows 覆盖率基线。
//
// 用 golang.org/x/sys/windows 而非 syscall：syscall 未导出 SetStdHandle
// （已核实 x/sys v0.49.0 同时提供 DuplicateHandle 与 SetStdHandle）。
func dupStdoutToStderr() (func() error, *os.File, error) {
	stdoutHandle := windows.Handle(os.Stdout.Fd())
	stderrHandle := windows.Handle(os.Stderr.Fd())
	proc := windows.CurrentProcess()

	// Python: output_fd = os.dup(stdout_fd)
	var dataHandle windows.Handle
	if err := windows.DuplicateHandle(proc, stdoutHandle, proc, &dataHandle,
		0, false, windows.DUPLICATE_SAME_ACCESS); err != nil {
		return nil, nil, err
	}
	data := os.NewFile(uintptr(dataHandle), "protocol-stdout")

	// Python: os.dup2(stderr_fd, stdout_fd)
	if err := windows.SetStdHandle(uint32(windows.STD_OUTPUT_HANDLE), stderrHandle); err != nil {
		_ = data.Close()
		return nil, nil, err
	}

	restore := func() error {
		// Python: :75-80 用 dup 出的句柄恢复
		restoreErr := windows.SetStdHandle(uint32(windows.STD_OUTPUT_HANDLE), stdoutHandle)
		// Python: with contextlib.suppress(OSError): output.close()
		_ = data.Close()
		return restoreErr
	}
	return restore, data, nil
}
