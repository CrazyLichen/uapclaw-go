//go:build !windows

package local

import (
	"fmt"
	"os"
	"syscall"
	"time"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// acquireFileLockPlatform Unix 平台文件锁实现。
// 对齐 Python fcntl.flock(fd, LOCK_EX|LOCK_NB) + 轮询超时。
func acquireFileLockPlatform(filePath string, timeout time.Duration) (*FileLock, error) {
	lockPath := filePath + ".lock"
	fd, err := syscall.Open(lockPath, syscall.O_CREAT|syscall.O_RDWR, 0644)
	if err != nil {
		return nil, fmt.Errorf("创建锁文件失败: %w", err)
	}

	deadline := time.Now().Add(timeout)
	for {
		err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return &FileLock{filePath: filePath, lockPath: lockPath, fd: uintptr(fd)}, nil
		}
		if err != syscall.EWOULDBLOCK && err != syscall.EAGAIN {
			_ = syscall.Close(fd)
			return nil, fmt.Errorf("获取文件锁失败: %w", err)
		}
		if time.Now().After(deadline) {
			_ = syscall.Close(fd)
			return nil, fmt.Errorf("获取文件锁超时(%v): %s", timeout, filePath)
		}
		time.Sleep(fileLockPollInterval)
	}
}

// releaseFileLockPlatform Unix 平台释放文件锁。
func releaseFileLockPlatform(lock *FileLock) error {
	if lock.fd == 0 {
		return nil
	}
	_ = syscall.Flock(int(lock.fd), syscall.LOCK_UN)
	_ = syscall.Close(int(lock.fd))
	_ = os.Remove(lock.lockPath)
	return nil
}
