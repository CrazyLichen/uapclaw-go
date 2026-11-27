//go:build windows

package processcli

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockSessionFileNonBlocking 非阻塞排它锁。
// Python: portalocker（Windows 走 msvcrt/LockFileEx）
func lockSessionFileNonBlocking(handle *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(windows.Handle(handle.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
}

// unlockSessionFile 解锁。
func unlockSessionFile(handle *os.File) {
	var overlapped windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(handle.Fd()), 0, 1, 0, &overlapped)
}
