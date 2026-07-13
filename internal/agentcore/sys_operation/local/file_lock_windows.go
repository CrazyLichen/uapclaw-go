//go:build windows

package local

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// acquireFileLockPlatform Windows 平台文件锁实现。
// 对齐 Python msvcrt.locking + 轮询超时。
func acquireFileLockPlatform(filePath string, timeout time.Duration) (*FileLock, error) {
	lockPath := filePath + ".lock"
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("创建锁文件失败: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		err := syscall.LockFileEx(syscall.Handle(fd), syscall.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, &syscall.Overlapped{})
		if err == nil {
			return &FileLock{filePath: filePath, lockPath: lockPath, fd: uintptr(fd)}, nil
		}
		if time.Now().After(deadline) {
			_ = syscall.Close(syscall.Handle(fd))
			return nil, fmt.Errorf("获取文件锁超时(%v): %s", timeout, filePath)
		}
		time.Sleep(fileLockPollInterval)
	}
}

// releaseFileLockPlatform Windows 平台释放文件锁。
func releaseFileLockPlatform(lock *FileLock) error {
	if lock.fd == 0 {
		return nil
	}
	_ = syscall.UnlockFile(syscall.Handle(lock.fd), 0, 0, 1, 0)
	_ = syscall.Close(syscall.Handle(lock.fd))
	_ = os.Remove(lock.lockPath)
	return nil
}
