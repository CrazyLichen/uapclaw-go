//go:build !windows

package local

import (
	"os/exec"
	"syscall"
	"time"
)

// ──────────────────────────── 非导出函数 ────────────────────────────
//
// 本文件为 Unix 平台进程组绑定实现（Setpgid / kill 进程组）。
// 平台绑定代码：按项目规范 3.2 第 3 条（硬件/OS 绑定）用 build tag 隔离，
// 不纳入非 Unix 平台的覆盖率计算基线。

// applyProcessGroup 让子进程独立成组，便于整组终止。
// Python: subprocess.Popen(start_new_session=True)。
func applyProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessTreePlatform 终止子进程所在的整个进程组（SIGTERM → 100ms → SIGKILL）。
// Python: os.killpg(os.getpgid(pid), signal.SIGTERM) 后再 SIGKILL。
func killProcessTreePlatform(h *AsyncProcessHandler) {
	if h.cmd.Process == nil {
		return
	}
	pid := h.cmd.Process.Pid
	// 尝试终止整个进程组
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.Sleep(100 * time.Millisecond)
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}
