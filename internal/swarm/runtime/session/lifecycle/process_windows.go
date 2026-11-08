//go:build windows

package lifecycle

import (
	"os"
	"syscall"
)

// inspectProcess 查询进程存活状态与启动时刻。
// Python: lifecycle.py 里的 psutil.Process(pid).create_time()
//
// Windows 上无权限查询时返回错误，调用方按"占用"处理（保守）。
// 启动时刻用 GetProcessTimes 的创建时间换算；读不到返回 0，
// 此时断言退化为"只看进程是否存活"。
func inspectProcess(pid int) (bool, float64, error) {
	const processQueryLimitedInformation = 0x1000
	handle, err := syscall.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		if err == syscall.ERROR_INVALID_PARAMETER || err == syscall.ERROR_ACCESS_DENIED {
			// 无此进程或无权限：前者按已退出处理，后者交由 err 分支保守处理
			if err == syscall.ERROR_INVALID_PARAMETER {
				return false, 0, nil
			}
			return false, 0, err
		}
		return false, 0, nil
	}
	defer func() { _ = syscall.CloseHandle(handle) }()

	var creation, exit, kernel, user syscall.Filetime
	if err := syscall.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return true, 0, nil
	}
	// Filetime 为 1601 纪元的 100ns 计数，换算为 Unix 秒
	const windowsToUnixEpochSeconds = 11644473600
	startedAt := float64(creation.HighDateTime)<<32 + float64(creation.LowDateTime)
	startedAt = startedAt/1e7 - windowsToUnixEpochSeconds
	return true, startedAt, nil
}

// 确保 os 被使用（Windows 分支下由 syscall 完成全部工作）。
var _ = os.Getpid
