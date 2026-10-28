//go:build windows

package acp

import (
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
//
// 本文件为 Windows 平台的进程组/信号实现（Python win32 分支对齐）。
// 平台绑定代码：按项目规范 3.2 第 3 条（硬件/OS 绑定）用 build tag 隔离，
// 不纳入非 Windows 平台的覆盖率计算基线。

// newPgroupAttr Windows 无进程组概念（Python win32 不设 start_new_session；
// killProcessGroup 在 Windows 恒 false，本函数不会被调用，返回零值属性）。
func newPgroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}

// signalProcessLeader Windows 用 Kill/Terminate（Python L210-216：
// proc.terminate() 与 proc.kill() 在 win32 均为 TerminateProcess）。
func signalProcessLeader(cmd *exec.Cmd, brutal bool) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
}

// resolveSpawnCommand 解析 Windows 命令 shim（.cmd/.exe/.bat）。
// Python: _resolve_spawn_command (stdio_client.py L121-132)
func resolveSpawnCommand(command string) string {
	cmd := strings.TrimSpace(command)
	if cmd == "" {
		return cmd
	}
	if strings.ContainsAny(cmd, "/\\") || filepath.Ext(cmd) != "" {
		return cmd
	}
	for _, candidate := range []string{cmd, cmd + ".cmd", cmd + ".exe", cmd + ".bat"} {
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved
		}
	}
	return cmd
}

// trimCommand 命令 trim（Windows 版）。
func trimCommand(command string) string {
	return strings.TrimSpace(command)
}
