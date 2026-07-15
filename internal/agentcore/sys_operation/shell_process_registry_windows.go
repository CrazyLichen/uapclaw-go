//go:build windows

package sys_operation

import (
	"os"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// isProcessExitedPlatform Windows 平台检查进程是否已退出（非阻塞）
// Windows 上 Signal(0) 不可用，保守返回 false，让后续 terminate 路径自行处理。
func isProcessExitedPlatform(proc *os.Process) bool {
	return false
}

// terminateShellProcessPlatform Windows 平台两阶段终止：先 Interrupt，等 3 秒后 Kill
func terminateShellProcessPlatform(proc *os.Process) bool {
	return terminateShellProcessWindows(proc)
}

// waitProcessWithTimeout 带超时等待进程退出。返回 true 表示进程已退出。
// Windows 使用 proc.Wait() 替代 syscall.Wait4。
func waitProcessWithTimeout(proc *os.Process, timeout time.Duration) bool {
	done := make(chan struct{}, 1)
	go func() {
		_, _ = proc.Wait()
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// terminateShellProcessWindows Windows 平台两阶段终止：先 Interrupt，等 3 秒后 Kill
func terminateShellProcessWindows(proc *os.Process) bool {
	// 阶段 1：发送 os.Interrupt（等价于 Python 的 proc.terminate()）
	if err := proc.Signal(os.Interrupt); err != nil {
		logger.Warn(logComponent).Int("pid", proc.Pid).Err(err).Msg("Windows Interrupt 失败，尝试直接 Kill")
		if killErr := proc.Kill(); killErr != nil {
			logger.Warn(logComponent).Int("pid", proc.Pid).Err(killErr).Msg("Windows Kill 失败")
			return false
		}
		waitProcessWithTimeout(proc, forceKillWait)
		return true
	}

	// 等待进程退出
	if waitProcessWithTimeout(proc, terminateGracePeriod) {
		return true
	}

	// 阶段 2：超时，强制 Kill
	if err := proc.Kill(); err != nil {
		logger.Warn(logComponent).Int("pid", proc.Pid).Err(err).Msg("Windows 强制 Kill 失败")
		return false
	}
	waitProcessWithTimeout(proc, forceKillWait)
	return true
}
