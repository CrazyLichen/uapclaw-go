//go:build windows

package lifecycle

import (
	"os"
	"syscall"
	"unsafe"
)

// lockFileHandle 加排他锁（阻塞）。
// Python: project_store.file_lock（Windows 上走 msvcrt.locking / LockFileEx）
func lockFileHandle(handle *os.File) error {
	var overlapped syscall.Overlapped
	return syscall.LockFileEx(syscall.Handle(handle.Fd()),
		syscall.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped)
}

// unlockFileHandle 解锁。
func unlockFileHandle(handle *os.File) {
	var overlapped syscall.Overlapped
	_ = syscall.UnlockFileEx(syscall.Handle(handle.Fd()), 0, 1, 0,
		unsafe.Pointer(&overlapped))
}
