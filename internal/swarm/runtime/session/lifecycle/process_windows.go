//go:build windows

package lifecycle

import (
	"golang.org/x/sys/windows"
)

// inspectProcess 查询进程存活状态与启动时刻。
// Python: lifecycle.py 里的 psutil.Process(pid).create_time()
//
// Windows 上无权限查询时返回错误，调用方按"占用"处理（保守）。
// 启动时刻用 GetProcessTimes 的创建时间换算；读不到返回 0，
// 此时断言退化为"只看进程是否存活"。
func inspectProcess(pid int) (bool, float64, error) {
	const processQueryLimitedInformation = 0x1000
	handle, err := windows.OpenProcess(processQueryLimitedInformation, false, uint32(pid))
	if err != nil {
		if err == windows.ERROR_INVALID_PARAMETER || err == windows.ERROR_ACCESS_DENIED {
			// 无此进程或无权限：前者按已退出处理，后者交由 err 分支保守处理
			if err == windows.ERROR_INVALID_PARAMETER {
				return false, 0, nil
			}
			return false, 0, err
		}
		return false, 0, nil
	}
	defer func() { _ = windows.CloseHandle(handle) }()

	var creation, exit, kernel, user windows.Filetime
	if err := windows.GetProcessTimes(handle, &creation, &exit, &kernel, &user); err != nil {
		return true, 0, nil
	}
	// Filetime 为 1601 纪元的 100ns 计数，换算为 Unix 秒
	const windowsToUnixEpochSeconds = 11644473600
	startedAt := float64(uint64(creation.HighDateTime)<<32 | uint64(creation.LowDateTime))
	startedAt = startedAt/1e7 - windowsToUnixEpochSeconds
	return true, startedAt, nil
}
