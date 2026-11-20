//go:build windows

package worktree

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// flockExclusive 以排它模式锁定文件（非阻塞，需轮询重试）。
// Python: filelock.AsyncFileLock.acquire 的 Windows 侧等价（D-25.7-d）。
// LockFileEx 无阻塞即失败，返回 error 交由上层轮询。
func flockExclusive(fd *os.File) error {
	ol := new(windows.Overlapped)
	return windows.LockFileEx(windows.Handle(fd.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY,
		0, 1, 0, ol)
}

// flockRelease 释放文件锁。
func flockRelease(fd *os.File) error {
	ol := new(windows.Overlapped)
	return errors.Join(windows.UnlockFileEx(windows.Handle(fd.Fd()), 0, 1, 0, ol))
}
