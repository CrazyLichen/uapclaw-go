// ssl_utils_windows.go 实现 Windows 平台的安全文件打开。
// Windows 不支持 O_NOFOLLOW，使用普通 os.OpenFile 打开。

//go:build windows

package security

import (
	"os"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// openFileNoFollow 安全打开文件。
// Windows 不支持 O_NOFOLLOW，退化为普通只读打开。
func openFileNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY, 0)
}
