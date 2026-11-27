//go:build windows

package adapter

import "os"

// fileCTimeMs Windows 无 syscall.Stat_t.Ctim，回退 mtime。
func fileCTimeMs(fi os.FileInfo) int64 {
	return fi.ModTime().UnixMilli()
}
