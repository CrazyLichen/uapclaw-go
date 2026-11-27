//go:build !windows

package discovery

import (
	"os"
	"syscall"
)

// lockFileExclusive 对文件加阻塞式排他锁（portalocker 等价，Unix 走 flock）。
func lockFileExclusive(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
}

// unlockFile 解除文件锁。
func unlockFile(f *os.File) error {
	return syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
}
