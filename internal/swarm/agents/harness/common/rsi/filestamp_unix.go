//go:build !windows

package rsi

import (
	"os"
	"syscall"
	"time"
)

// fileStampParts 取文件指纹的 ino 与 ctime 纳秒部分（对齐 Python st_ino/st_ctime）。
func fileStampParts(info os.FileInfo) (int64, int64) {
	var ino, ctime int64
	if st, ok := info.Sys().(*syscall.Stat_t); ok {
		ino = int64(st.Ino)
		ctime = st.Ctim.Sec*int64(time.Second) + st.Ctim.Nsec
	}
	return ino, ctime
}
