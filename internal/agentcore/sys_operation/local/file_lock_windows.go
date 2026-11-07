//go:build windows

package local

import (
	"fmt"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// ──────────────────────────── 非导出函数 ────────────────────────────
//
// 本文件为 Windows 平台文件锁实现。
// 平台绑定代码：按项目规范 3.2 第 3 条（硬件/OS 绑定）用 build tag 隔离，
// 不纳入非 Windows 平台的覆盖率计算基线。
// 注意：Go 标准库 syscall 包在 Windows 下已不提供 LockFileEx/UnlockFile，
// 统一改用 golang.org/x/sys/windows（与 syscall 同源，API 等价）。

// acquireFileLockPlatform Windows 平台文件锁实现。
// Python: msvcrt.locking + 轮询超时。
func acquireFileLockPlatform(filePath string, timeout time.Duration) (*FileLock, error) {
	lockPath := filePath + ".lock"
	pathPtr, err := windows.UTF16PtrFromString(lockPath)
	if err != nil {
		return nil, fmt.Errorf("创建锁文件失败: %w", err)
	}
	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("创建锁文件失败: %w", err)
	}

	overlapped := new(windows.Overlapped)
	deadline := time.Now().Add(timeout)
	for {
		err := windows.LockFileEx(handle, windows.LOCKFILE_EXCLUSIVE_LOCK, 0, 1, 0, overlapped)
		if err == nil {
			return &FileLock{filePath: filePath, lockPath: lockPath, fd: uintptr(handle)}, nil
		}
		if time.Now().After(deadline) {
			_ = windows.CloseHandle(handle)
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
	handle := windows.Handle(lock.fd)
	_ = windows.UnlockFileEx(handle, 0, 1, 0, new(windows.Overlapped))
	_ = windows.CloseHandle(handle)
	_ = os.Remove(lock.lockPath)
	return nil
}
