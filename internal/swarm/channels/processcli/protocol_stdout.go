package processcli

import (
	"os"
)

// protocolStdout 把 fd1 重定向到 stderr，返回 JSONL 专用 writer 与恢复函数。
// Python: machine_entry.py:49-80 protocol_stdout()
//
// 目的：依赖库、工具子进程、C 扩展的 print 全部落 stderr，stdout 只剩 JSONL。
//
// 差异 D-6：Python 用 contextlib.redirect_stdout 改 Python 层 sys.stdout；
// Go 无全局 sys.stdout 对象，fmt 直接写 os.Stdout 的 fd，被 dup2 后自动落 stderr，
// 故无需第三件事。
func protocolStdout() (restore func() error, data *os.File, err error) {
	return dupStdoutToStderr()
}
