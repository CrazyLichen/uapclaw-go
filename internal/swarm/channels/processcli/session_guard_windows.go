//go:build windows

package processcli

import (
	"os"
	"syscall"
	"unsafe"
)

// lockSessionFileNonBlocking 非阻塞排它锁。
// Python: portalocker（Windows 走 msvcrt/LockFileEx）
func lockSessionFileNonBlocking(handle *os.File) error {
	var overlapped syscall.Overlapped
	return syscall.LockFileEx(syscall.Handle(handle.Fd()),
		syscall.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &overlapped)
}

// unlockSessionFile 解锁。
func unlockSessionFile(handle *os.File) {
	var overlapped syscall.Overlapped
	_ = syscall.UnlockFileEx(syscall.Handle(handle.Fd()), 0, 1, 0,
		unsafe.Pointer(&overlapped))
}
