//go:build windows

package tasks

import (
	"os"

	"golang.org/x/sys/windows"
)

// openLeaseFile 打开租约锁文件（O_CREAT|O_RDWR），返回句柄（int 承载 Windows HANDLE）。
func openLeaseFile(path string) (int, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return -1, err
	}
	return int(f.Fd()), nil
}

// tryLockLease 非阻塞加排他锁（偏差 D-1：portalocker.Lock(timeout=0)）。
func tryLockLease(fd int) error {
	var overlapped windows.Overlapped
	return windows.LockFileEx(windows.Handle(fd),
		windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, &overlapped)
}

// unlockLease 解除租约锁。
func unlockLease(fd int) error {
	var overlapped windows.Overlapped
	return windows.UnlockFileEx(windows.Handle(fd), 0, 1, 0, &overlapped)
}

// closeLease 关闭租约锁文件句柄。
func closeLease(fd int) error {
	return windows.CloseHandle(windows.Handle(fd))
}
