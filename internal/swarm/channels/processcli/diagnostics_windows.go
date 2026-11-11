//go:build windows

package processcli

import (
	"os"

	"golang.org/x/sys/windows"
	"golang.org/x/term"
)

// dupFile 复制句柄。
// Python: diagnostics.py:126-131 os.fdopen(os.dup(fd), ...)
func dupFile(f *os.File) (*os.File, error) {
	proc, err := windows.GetCurrentProcess()
	if err != nil {
		return nil, err
	}
	var target windows.Handle
	if err := windows.DuplicateHandle(
		proc, windows.Handle(f.Fd()), proc, &target,
		0, false, windows.DUPLICATE_SAME_ACCESS,
	); err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(target), f.Name()), nil
}

// redirectStdio Windows 下不改 CRT fd（会破坏控制台句柄），只返回空恢复函数。
// 与上游 diagnostics.py:100-118 的 Windows 分支语义一致——那条分支只做
// Python 层重定向，Go 无语言层，等价于只记录路径。
func redirectStdio(_ *os.File, _ bool) (func(), func(), error) {
	return func() {}, func() {}, nil
}

// isTerminal 判断是否为终端。
func isTerminal(f *os.File) bool {
	if f == nil {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
