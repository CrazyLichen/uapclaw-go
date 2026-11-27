//go:build !windows

package adapter

import (
	"os"
	"syscall"
)

// fileCTimeMs 从 Stat_t 取 inode 变更时间（对齐 Python st_ctime；
// 平台无 Stat_t.Ctim 时回退 mtime）。
func fileCTimeMs(fi os.FileInfo) int64 {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return st.Ctim.Sec*1000 + st.Ctim.Nsec/1e6
	}
	return fi.ModTime().UnixMilli()
}
