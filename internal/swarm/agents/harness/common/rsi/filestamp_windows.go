//go:build windows

package rsi

import "os"

// fileStampParts Windows 无 syscall.Stat_t，退化为 0（仅 mtime/size 参与指纹）。
func fileStampParts(info os.FileInfo) (int64, int64) {
	return 0, 0
}
