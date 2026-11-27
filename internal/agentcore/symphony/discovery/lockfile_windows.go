//go:build windows

package discovery

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFileExclusive 对文件加阻塞式排他锁（portalocker 等价，Windows 走 LockFileEx）。
func lockFileExclusive(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(windows.Handle(f.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped)
}

// unlockFile 解除文件锁。
func unlockFile(f *os.File) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, &overlapped)
}
