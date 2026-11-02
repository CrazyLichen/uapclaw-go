//go:build windows

package local

import (
	"os/exec"
	"strconv"
)

// ──────────────────────────── 非导出函数 ────────────────────────────
//
// 本文件为 Windows 平台进程绑定实现（进程组与进程树终止）。
// 平台绑定代码：按项目规范 3.2 第 3 条（硬件/OS 绑定）用 build tag 隔离，
// 不纳入非 Windows 平台的覆盖率计算基线。

// applyProcessGroup Windows 不做改造：无 POSIX 进程组语义，
// 子进程树终止改由 taskkill /T 完成。
func applyProcessGroup(_ *exec.Cmd) {}

// killProcessTreePlatform 终止子进程及其整个进程树。
// Python: Windows 分支不使用 os.killpg，Go 侧对齐为 taskkill /T /F（树终止），
// 失败时回退直接 Kill 进程本体。
func killProcessTreePlatform(h *AsyncProcessHandler) {
	if h.cmd.Process == nil {
		return
	}
	pid := strconv.Itoa(h.cmd.Process.Pid)
	if err := exec.Command("taskkill", "/T", "/F", "/PID", pid).Run(); err != nil {
		_ = h.cmd.Process.Kill()
	}
}
