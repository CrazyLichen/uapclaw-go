//go:build !windows

package processcli

import (
	"os"
	"syscall"
)

// dupStdoutToStderr POSIX 实现：dup 一份原 fd1 给 writer，再把 fd1 dup2 到 stderr。
// Python: machine_entry.py:66-80（os.dup + os.dup2）
//
// 顺序不可颠倒：必须先 dup 出原 fd1 的副本（data），再 dup2 覆盖 fd1。
// 反过来的话 data 拿到的是已经指向 stderr 的 fd，JSONL 会写进 stderr。
func dupStdoutToStderr() (func() error, *os.File, error) {
	stdoutFD := int(os.Stdout.Fd())
	stderrFD := int(os.Stderr.Fd())

	// Python: output_fd = os.dup(stdout_fd)
	dataFD, err := syscall.Dup(stdoutFD)
	if err != nil {
		return nil, nil, err
	}
	data := os.NewFile(uintptr(dataFD), "protocol-stdout")

	// Python: os.dup2(stderr_fd, stdout_fd)
	if err := syscall.Dup2(stderrFD, stdoutFD); err != nil {
		// Python: with contextlib.suppress(OSError): output.close()
		_ = data.Close()
		return nil, nil, err
	}

	restore := func() error {
		// Python: :75-80 用 dup 出的 fd 恢复，不 close 后重开
		restoreErr := syscall.Dup2(dataFD, stdoutFD)
		// Python: with contextlib.suppress(OSError): output.close()
		_ = data.Close()
		return restoreErr
	}
	return restore, data, nil
}
