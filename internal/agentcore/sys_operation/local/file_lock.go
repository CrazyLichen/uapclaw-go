package local

import (
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// FileLock 跨进程文件锁。
// 对齐 Python _file_lock：fcntl.flock(fd, LOCK_EX|LOCK_NB) + 轮询超时。
type FileLock struct {
	// filePath 被锁文件路径
	filePath string
	// lockPath 锁文件路径（.lock 后缀）
	lockPath string
	// fd 锁文件描述符（平台相关）
	fd uintptr
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// fileLockPollInterval 文件锁轮询间隔，对齐 Python 50ms
	fileLockPollInterval = 50 * time.Millisecond
	// fileLockDefaultTimeout 文件锁默认超时，对齐 Python 10s
	fileLockDefaultTimeout = 10 * time.Second
)

// ──────────────────────────── 导出函数 ────────────────────────────

// AcquireFileLock 获取文件锁，超时返回 error。
// 对齐 Python _file_lock(data_path)：创建 .lock 文件 + fcntl.flock + 轮询。
func AcquireFileLock(filePath string, timeout time.Duration) (*FileLock, error) {
	return acquireFileLockPlatform(filePath, timeout)
}

// ReleaseFileLock 释放文件锁。
// 对齐 Python _file_lock 的 __exit__ / finally。
func ReleaseFileLock(lock *FileLock) error {
	return releaseFileLockPlatform(lock)
}
