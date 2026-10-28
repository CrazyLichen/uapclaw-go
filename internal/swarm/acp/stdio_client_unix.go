//go:build !windows

package acp

import (
	"os/exec"
	"strings"
	"syscall"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// newPgroupAttr 构造新进程组属性（Python start_new_session=True 等价）。
// Python: spawn_kw["start_new_session"] = True (stdio_client.py L497-498)
func newPgroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// signalProcessLeader 向受管子进程（及其 POSIX 进程组）发信号。
// Python: _signal_process_leader (stdio_client.py L193-217)
func signalProcessLeader(cmd *exec.Cmd, brutal bool) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	pid := cmd.Process.Pid
	sig := syscall.SIGKILL
	if !brutal {
		sig = syscall.SIGTERM
	}
	if killProcessGroup() {
		// 进程组信号（子进程为组长，负 pid 定位组）
		if err := syscall.Kill(-pid, sig); err == nil {
			return
		}
		// killpg 失败（如 ESRCH）回退单进程信号
	}
	_ = cmd.Process.Signal(sig)
}

// resolveSpawnCommand 解析 Windows 命令 shim；非 Windows 原样返回（trim 后）。
// Python: _resolve_spawn_command (stdio_client.py L121-132)
func resolveSpawnCommand(command string) string {
	return trimCommand(command)
}

// trimCommand 命令 trim（Unix 版）。
func trimCommand(command string) string {
	if command == "" {
		return ""
	}
	return strings.TrimSpace(command)
}
