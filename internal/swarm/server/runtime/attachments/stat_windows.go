//go:build windows

package attachments

import "os"

// syscallStatT Windows 无 syscall.Stat_t，定义占位别名（含 Dev/Ino 字段形状）；
// fi.Sys() 实际为 *Win32FileAttributeData，类型断言恒失败，sameInode 走退化分支。
type syscallStatT = struct {
	Dev   uint64
	Ino   uint64
	Nlink uint64
}

// statNlink Windows 无硬链接数语义，恒返回 1（跳过单链接校验）。
func statNlink(fi os.FileInfo) uint64 {
	return 1
}
