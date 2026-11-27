//go:build !windows

package attachments

import (
	"os"
	"syscall"
)

// syscallStatT 平台 stat 别名（steer_images.sameInode 使用）。
type syscallStatT = syscall.Stat_t

// statNlink 返回硬链接数；平台不支持时返回 1（跳过单链接校验）。
func statNlink(fi os.FileInfo) uint64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Nlink
	}
	return 1
}
