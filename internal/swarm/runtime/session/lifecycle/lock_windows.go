//go:build windows

package lifecycle

import (
	"os"

	"golang.org/x/sys/windows"
)

// lockFileHandle 加排他锁（阻塞）。
// Python: project_store.file_lock（Windows 上走 msvcrt.locking / LockFileEx）
func lockFileHandle(handle *os.File) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(windows.Handle(handle.Fd()),
		windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped)
}

// unlockFileHandle 解锁。
func unlockFileHandle(handle *os.File) {
	var overlapped windows.Overlapped
	_ = windows.UnlockFileEx(windows.Handle(handle.Fd()), 0, 1, 0, &overlapped)
}
