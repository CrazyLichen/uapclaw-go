//go:build !windows

package lifecycle

import (
	"errors"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// inspectProcess 查询进程存活状态与启动时刻。
// Python: lifecycle.py 里的 psutil.Process(pid).create_time()
//
// 返回 (alive, startedAtSeconds, err)：
//   - 进程不存在 → (false, 0, nil)
//   - 无权限判断 → (false, 0, err)，调用方按"占用"处理
//
// 启动时刻从 /proc/<pid>/stat 的 starttime 换算（Linux 固定 100Hz 时钟）。
// 非 Linux 平台读不到 /proc 时返回 0，此时断言退化为"只看进程是否存活"，
// 这偏保守（更倾向认为被占用），不会误删。
func inspectProcess(pid int) (bool, float64, error) {
	process, err := os.FindProcess(pid)
	if err != nil {
		// Go 1.23+ 的 FindProcess 对已结束进程返回 ErrProcessDone，
		// 这不是"无法判断"，而是明确的"进程已不在"。
		if errors.Is(err, os.ErrProcessDone) {
			return false, 0, nil
		}
		return false, 0, nil
	}
	if signalErr := process.Signal(syscall.Signal(0)); signalErr != nil {
		// ESRCH = 进程不存在；EPERM = 存在但无权限（保守按存活处理）
		if errors.Is(signalErr, syscall.ESRCH) || errors.Is(signalErr, os.ErrProcessDone) {
			return false, 0, nil
		}
		return false, 0, signalErr
	}
	return true, processStartSeconds(pid), nil
}

// processStartSeconds 读进程启动时刻（秒）。读不到返回 0。
func processStartSeconds(pid int) float64 {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return 0
	}
	text := string(raw)
	// comm 字段可能含空格与括号，因此从最后一个 ')' 之后开始切
	closeIndex := strings.LastIndexByte(text, ')')
	if closeIndex < 0 {
		return 0
	}
	fields := strings.Fields(text[closeIndex+1:])
	// 除去 pid 与 comm 后，starttime 是第 20 个字段（原第 22 个）
	const startTimeFieldIndex = 19
	if len(fields) <= startTimeFieldIndex {
		return 0
	}
	ticks, err := strconv.ParseInt(fields[startTimeFieldIndex], 10, 64)
	if err != nil || ticks <= 0 {
		return 0
	}
	return float64(ticks) / 100.0
}
