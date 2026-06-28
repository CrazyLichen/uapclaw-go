// ssl_utils_unix.go 实现 Unix 平台的安全文件打开。
// 使用 syscall.O_NOFOLLOW 防止符号链接攻击。

//go:build !windows

package security

import (
	"os"
	"syscall"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// openFileNoFollow 安全打开文件，使用 O_NOFOLLOW 防止符号链接攻击。
func openFileNoFollow(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW, 0)
}
