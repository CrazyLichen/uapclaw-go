//go:build !windows

package tasks

import "syscall"

// openLeaseFile 打开租约锁文件（O_CREAT|O_RDWR），返回 fd。
func openLeaseFile(path string) (int, error) {
	return syscall.Open(path, syscall.O_CREAT|syscall.O_RDWR, 0o644)
}

// tryLockLease 非阻塞加排他锁（偏差 D-1：portalocker.Lock(timeout=0)）。
func tryLockLease(fd int) error {
	return syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
}

// unlockLease 解除租约锁。
func unlockLease(fd int) error {
	return syscall.Flock(fd, syscall.LOCK_UN)
}

// closeLease 关闭租约锁文件。
func closeLease(fd int) error {
	return syscall.Close(fd)
}
