//go:build !windows

package lifecycle

import (
	"os"
	"syscall"
)

// lockFileHandle 加排他锁（阻塞）。
// Python: project_store.file_lock（fcntl.flock）
func lockFileHandle(handle *os.File) error {
	return syscall.Flock(int(handle.Fd()), syscall.LOCK_EX)
}

// unlockFileHandle 解锁。
func unlockFileHandle(handle *os.File) {
	_ = syscall.Flock(int(handle.Fd()), syscall.LOCK_UN)
}
