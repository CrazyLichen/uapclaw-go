//go:build !windows

package sys_operation

import (
	"os"
	"syscall"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// isProcessExitedPlatform POSIX 平台检查进程是否已退出（非阻塞）
func isProcessExitedPlatform(proc *os.Process) bool {
	// 用 Signal(0) 探测进程是否存活（POSIX 标准）
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return true // 进程不存在
	}
	return false
}

// terminateShellProcessPlatform POSIX 平台两阶段终止：先 SIGTERM 进程组，等 3 秒后 SIGKILL
func terminateShellProcessPlatform(proc *os.Process) bool {
	return terminateShellProcessPOSIX(proc)
}

// waitProcessWithTimeout 带超时等待进程退出。返回 true 表示进程已退出。
func waitProcessWithTimeout(proc *os.Process, timeout time.Duration) bool {
	done := make(chan struct{}, 1)
	go func() {
		var status syscall.WaitStatus
		_, _ = syscall.Wait4(proc.Pid, &status, 0, nil)
		close(done)
	}()

	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// terminateShellProcessPOSIX POSIX 平台两阶段终止：先 SIGTERM 进程组，等 3 秒后 SIGKILL
func terminateShellProcessPOSIX(proc *os.Process) bool {
	pid := proc.Pid
	if pid <= 0 {
		return false
	}

	// 阶段 1：SIGTERM 整个进程组
	if err := syscall.Kill(-pid, syscall.SIGTERM); err != nil {
		// 进程组 SIGTERM 失败，尝试单进程 Signal
		logger.Warn(logComponent).Int("pid", pid).Err(err).Msg("进程组 SIGTERM 失败，尝试单进程终止")
		if sigErr := proc.Signal(syscall.SIGTERM); sigErr != nil {
			// 单进程也失败，尝试 SIGKILL
			return forceKillPOSIX(proc)
		}
	}

	// 等待进程退出
	if waitProcessWithTimeout(proc, terminateGracePeriod) {
		return true
	}

	// 阶段 2：超时，SIGKILL 整个进程组
	return forceKillPOSIX(proc)
}

// forceKillPOSIX 强制杀死 POSIX 进程组
func forceKillPOSIX(proc *os.Process) bool {
	pid := proc.Pid
	if err := syscall.Kill(-pid, syscall.SIGKILL); err != nil {
		logger.Warn(logComponent).Int("pid", pid).Err(err).Msg("进程组 SIGKILL 失败，尝试单进程 Kill")
		if killErr := proc.Kill(); killErr != nil {
			logger.Warn(logComponent).Int("pid", pid).Err(killErr).Msg("单进程 Kill 也失败")
			return false
		}
	}
	// 等待进程退出
	waitProcessWithTimeout(proc, forceKillWait)
	return true
}
