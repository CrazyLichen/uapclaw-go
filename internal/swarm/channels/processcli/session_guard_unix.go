//go:build !windows

package processcli

import (
	"os"
	"syscall"
)

// lockSessionFileNonBlocking 非阻塞排它锁。
// Python: portalocker.Lock(flags=LOCK_EX | LOCK_NB)（POSIX 走 fcntl/flock）
func lockSessionFileNonBlocking(handle *os.File) error {
	return syscall.Flock(int(handle.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// unlockSessionFile 解锁。
func unlockSessionFile(handle *os.File) {
	_ = syscall.Flock(int(handle.Fd()), syscall.LOCK_UN)
}
