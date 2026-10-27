//go:build windows

package tools

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// ──────────────────────────── 非导出函数 ────────────────────────────
//
// 本文件为 Windows 专属 shell 探测实现（对齐 Python command_tools.py 同名函数）。
// 平台绑定代码：按项目规范 3.2 第 3 条（硬件/OS 绑定）用 build tag 隔离，
// 不纳入非 Windows 平台的覆盖率计算基线。

// availablePowershell 探测 PowerShell 可执行文件路径。
// Python: _available_powershell
func availablePowershell() string {
	if runtime.GOOS == "windows" {
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			systemRoot = os.Getenv("WINDIR")
		}
		if systemRoot == "" {
			systemRoot = `C:\Windows`
		}
		systemPowershell := filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, err := os.Stat(systemPowershell); err == nil {
			return systemPowershell
		}
	}
	for _, candidate := range []string{"pwsh", "powershell", "powershell.exe"} {
		if resolved, err := exec.LookPath(candidate); err == nil {
			return resolved
		}
	}
	return "powershell"
}

// isWSLBashPath 判断是否 WSL bash 路径（Windows 专属）。
// Python: _is_wsl_bash_path
func isWSLBashPath(path string) bool {
	if runtime.GOOS != "windows" {
		return false
	}
	normalized := strings.ToLower(filepath.Clean(path))
	systemRoot := strings.ToLower(filepath.Clean(os.Getenv("SystemRoot")))
	if systemRoot == "" {
		systemRoot = `C:\Windows`
	}
	return normalized == filepath.Join(systemRoot, "system32", "bash.exe") ||
		strings.Contains(normalized, `\microsoft\windowsapps\bash.exe`)
}

// gitBashCandidates 收集 Git Bash 候选路径。
// Python: _git_bash_candidates
func gitBashCandidates() []string {
	var candidates []string
	envPath := os.Getenv("GIT_BASH")
	if envPath == "" {
		envPath = os.Getenv("GIT_BASH_PATH")
	}
	if envPath != "" {
		candidates = append(candidates, envPath)
	}

	programFiles := []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")}
	if localAppData := os.Getenv("LocalAppData"); localAppData != "" {
		programFiles = append(programFiles, filepath.Join(localAppData, "Programs"))
	}
	for _, root := range programFiles {
		if root != "" {
			candidates = append(candidates, filepath.Join(root, "Git", "bin", "bash.exe"))
		}
	}

	if gitPath, err := exec.LookPath("git"); err == nil {
		gitExe := filepath.Dir(gitPath)
		candidates = append(candidates, filepath.Join(filepath.Dir(gitExe), "bin", "bash.exe"))
	}
	return candidates
}

// availableGitBash 探测 Git Bash（仅 Windows）。
// Python: _available_git_bash
func availableGitBash() (string, bool) {
	if runtime.GOOS != "windows" {
		return "", false
	}
	for _, candidate := range gitBashCandidates() {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, true
		}
	}
	return "", false
}

// availableBash 探测 bash 可执行文件。
// Python: _available_bash（allow_wsl 语义逐字）
func availableBash(allowWSL bool) (string, bool) {
	if runtime.GOOS == "windows" {
		if gitBash, ok := availableGitBash(); ok {
			return gitBash, true
		}
	}
	if resolved, err := exec.LookPath("bash"); err == nil {
		if allowWSL || !isWSLBashPath(resolved) {
			return resolved, true
		}
	}
	return "", false
}

// availableSh 探测 sh 可执行文件。
// Python: _available_sh
func availableSh() (string, bool) {
	if runtime.GOOS == "windows" {
		if gitBash, ok := availableGitBash(); ok {
			shPath := filepath.Join(filepath.Dir(filepath.Dir(gitBash)), "usr", "bin", "sh.exe")
			if _, err := os.Stat(shPath); err == nil {
				return shPath, true
			}
		}
	}
	if resolved, err := exec.LookPath("sh"); err == nil {
		return resolved, true
	}
	return "", false
}

// looksLikePosix 判断命令是否以 POSIX 命令开头。
// Python: _looks_like_posix
func looksLikePosix(command string) bool {
	for _, segment := range splitShellSegments(command) {
		if posixCommands[segmentBaseCommand(segment)] {
			return true
		}
	}
	return false
}

// resolveExecutionPlanWindows resolveExecutionPlan 的 Windows 分支。
// Python: _resolve_execution_plan 的 isWindows 块（command_tools.py）
func resolveExecutionPlanWindows(command, normalized string) ([]string, bool, string) {
	if normalized == "auto" {
		powershellCommand := unwrapPowershellCommand(command)
		if powershellCommand != "" {
			exe := availablePowershell()
			return []string{exe, "-NoProfile", "-NonInteractive", "-Command", powershellCommand}, false, "powershell"
		}
		if looksLikePowershell(command) {
			exe := availablePowershell()
			return []string{exe, "-NoProfile", "-NonInteractive", "-Command", command}, false, "powershell"
		}
		if looksLikePosix(command) {
			if exe, ok := availableBash(false); ok {
				return []string{exe, "-lc", normalizeWindowsPathsForBash(command)}, false, "bash"
			}
		}
		normalized = "cmd"
	}
	if normalized == "powershell" {
		exe := availablePowershell()
		unwrapped := unwrapPowershellCommand(command)
		if unwrapped == "" {
			unwrapped = command
		}
		return []string{exe, "-NoProfile", "-NonInteractive", "-Command", unwrapped}, false, "powershell"
	}
	if normalized == "cmd" {
		return []string{command}, true, "cmd"
	}
	if normalized == "bash" || normalized == "sh" {
		var exe string
		if normalized == "bash" {
			if resolved, ok := availableBash(true); ok {
				exe = resolved
			}
		} else {
			if resolved, ok := availableSh(); ok {
				exe = resolved
			}
		}
		if exe == "" {
			// Python raise RuntimeError；Go 侧由调用方（runCommandBlocking/Background）
			// 通过 exec.LookPath 的报错兜底，此处返回原命令让 exec 报错。
			return []string{exe}, false, normalized
		}
		flag := "-c"
		if normalized == "bash" {
			flag = "-lc"
		}
		return []string{exe, flag, normalizeWindowsPathsForBash(command)}, false, normalized
	}
	// 兜底（normalizeShellType 保证不会到这里）
	return []string{command}, true, "cmd"
}
