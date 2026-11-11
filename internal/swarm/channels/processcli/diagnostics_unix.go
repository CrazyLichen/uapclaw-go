//go:build !windows

package processcli

import (
	"os"

	"golang.org/x/sys/unix"
	"golang.org/x/term"
)

// dupFile 复制文件描述符，返回指向同一目标的新 *os.File。
// Python: diagnostics.py:126-131 os.fdopen(os.dup(fd), ...)
func dupFile(f *os.File) (*os.File, error) {
	fd, err := unix.Dup(int(f.Fd()))
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), f.Name()), nil
}

// redirectStdio 把 fd1（可选）与 fd2 指向 target，返回各自的恢复函数。
// Python: diagnostics.py:141-146 os.dup2(log_file.fileno(), fd)
//
// 先 dup 出原 fd 作为恢复用副本，再 dup2 覆盖；恢复时按相反顺序写回。
func redirectStdio(target *os.File, includeStdout bool) (func(), func(), error) {
	origStdout, err := unix.Dup(int(os.Stdout.Fd()))
	if err != nil {
		return nil, nil, err
	}
	origStderr, err := unix.Dup(int(os.Stderr.Fd()))
	if err != nil {
		_ = unix.Close(origStdout)
		return nil, nil, err
	}
	if includeStdout {
		if err := unix.Dup2(int(target.Fd()), int(os.Stdout.Fd())); err != nil {
			_ = unix.Close(origStdout)
			_ = unix.Close(origStderr)
			return nil, nil, err
		}
	}
	if err := unix.Dup2(int(target.Fd()), int(os.Stderr.Fd())); err != nil {
		_ = unix.Close(origStdout)
		_ = unix.Close(origStderr)
		return nil, nil, err
	}
	restoreStdout := func() {
		if includeStdout {
			_ = unix.Dup2(origStdout, int(os.Stdout.Fd()))
		}
		_ = unix.Close(origStdout)
	}
	restoreStderr := func() {
		_ = unix.Dup2(origStderr, int(os.Stderr.Fd()))
		_ = unix.Close(origStderr)
	}
	return restoreStdout, restoreStderr, nil
}

// isTerminal 判断文件是否为终端。
//
// 注意 /dev/null 也是字符设备，os.ModeCharDevice 会误判
// （见 internal/common/logger/format.go:70 的历史坑），故用 term.IsTerminal。
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
