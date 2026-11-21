//go:build !windows

package worktree

import (
	"os"
	"syscall"
)

// flockExclusive 以排它模式锁定文件（非阻塞，需轮询重试）。
// Python: filelock.AsyncFileLock.acquire 的 POSIX 侧等价（D-25.7-d）。
func flockExclusive(fd *os.File) error {
	return syscall.Flock(int(fd.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
}

// flockRelease 释放文件锁。
func flockRelease(fd *os.File) error {
	return syscall.Flock(int(fd.Fd()), syscall.LOCK_UN)
}
