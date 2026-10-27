//go:build !windows

package tools

// ──────────────────────────── 非导出函数 ────────────────────────────
//
// 本文件为非 Windows 平台的 Windows 专属 shell 探测桩。
// 这些函数仅在 resolveExecutionPlan 的 Windows 分支被引用；
// 非 Windows 平台编译期替换为零值实现，使平台绑定代码不进入覆盖率基线
// （对齐项目规范 3.2：硬件/OS 绑定代码用 build tag 隔离）。

// availablePowershell 探测 PowerShell（非 Windows 恒空）。
// Python: _available_powershell
func availablePowershell() string {
	return "powershell"
}

// isWSLBashPath 判断是否 WSL bash 路径（非 Windows 恒 false）。
// Python: _is_wsl_bash_path
func isWSLBashPath(_ string) bool {
	return false
}

// gitBashCandidates 收集 Git Bash 候选路径（非 Windows 恒空）。
// Python: _git_bash_candidates
func gitBashCandidates() []string {
	return nil
}

// availableGitBash 探测 Git Bash（非 Windows 恒 false）。
// Python: _available_git_bash
func availableGitBash() (string, bool) {
	return "", false
}

// availableBash 探测 bash（非 Windows 恒 false——Linux 侧用 availableUnixShell）。
// Python: _available_bash
func availableBash(_ bool) (string, bool) {
	return "", false
}

// availableSh 探测 sh（非 Windows 恒 false——Linux 侧用 availableUnixShell）。
// Python: _available_sh
func availableSh() (string, bool) {
	return "", false
}

// looksLikePosix 判断命令是否以 POSIX 命令开头（非 Windows 恒 false）。
// Python: _looks_like_posix
func looksLikePosix(_ string) bool {
	return false
}

// resolveExecutionPlanWindows resolveExecutionPlan 的 Windows 分支（非 Windows 不可达）。
func resolveExecutionPlanWindows(command, _ string) ([]string, bool, string) {
	return nil, false, "windows-unreachable"
}
