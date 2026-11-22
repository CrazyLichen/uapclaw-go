package prompt

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ShellCapabilitiesShell 能力探测片段。
//
// 对齐 Python: jiuwenswarm/agents/harness/common/prompt/shell_environment.py（339 行 1:1）。
// 单飞探测跨成员/线程共享；超时的文件系统调用不可取消，
// 单飞槽位保留到真正返回为止。asyncio 默认执行器不排队工作。
//
// 文件目录条目见包 doc.go。

// ──────────────────────────── 常量 ────────────────────────────

const (
	// probeWaitSeconds 探测等待窗口
	// Python: _PROBE_WAIT_SECONDS = 0.2
	probeWaitSeconds = 200 * time.Millisecond

	// probeRetrySeconds 探测失败后的重试间隔
	// Python: _PROBE_RETRY_SECONDS = 30.0
	probeRetrySeconds = 30 * time.Second
)

// probeEnvNames 参与探测缓存身份的环境变量名。
// Python: _PROBE_ENV_NAMES
var probeEnvNames = []string{
	"PATH", "PATHEXT", "SystemRoot", "GIT_BASH", "GIT_BASH_PATH",
	"ProgramFiles", "ProgramFiles(x86)", "LocalAppData", "HOME",
	"USERPROFILE", "HOMEDRIVE", "HOMEPATH", "NoDefaultCurrentDirectoryInExePath",
}

// shellProbeLogComponent 日志组件
var shellProbeLogComponent = logger.ComponentAgentServer

// ──────────────────────────── 结构体 ────────────────────────────

// ShellCapabilities 完成的能力快照，或显式未验证的兜底。
//
// Python: ShellCapabilities (dataclass, frozen)
type ShellCapabilities struct {
	// Detected 是否完成过探测
	Detected bool
	// PathBash PATH 中找到的 bash
	PathBash string
	// PowerShell 探测到的 PowerShell
	PowerShell string
	// GitBash 探测到的 Git Bash
	GitBash string
	// PosixShell POSIX shell（bash 或 sh）
	PosixShell string
}

// shellProbe 单次探测槽位。
//
// Python: _ShellProbe
type shellProbe struct {
	// key 探测发起时的环境身份
	key environmentKey
	// started 发起时间（monotonic）
	started time.Time
	// resultPtr 探测结果（nil = 未完成）；用指针通道语义承载
	resultPtr *ShellCapabilities
	// timeoutLogged 是否已记录过超时告警
	timeoutLogged bool
}

// shellCapabilityCache 共享的有界探测缓存。
//
// Python: _ShellCapabilityCache
type shellCapabilityCache struct {
	mu        sync.Mutex
	probe     *shellProbe
	cachedKey environmentKey
	cached    ShellCapabilities
	retryAt   time.Time
	nowFunc   func() time.Time
}

// environmentKey 探测缓存身份：osType + cwd + 相关环境变量取值。
// 含切片字段不可比较，相等性用 equal 方法。
//
// Python: _environment_key(os_type)
type environmentKey struct {
	osType string
	cwd    string
	values []string
}

// equal 逐字段比较（Python 元组 == 语义）。
func (k environmentKey) equal(other environmentKey) bool {
	if k.osType != other.osType || k.cwd != other.cwd || len(k.values) != len(other.values) {
		return false
	}
	for i := range k.values {
		if k.values[i] != other.values[i] {
			return false
		}
	}
	return true
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetShellCapabilities 获取共享能力，不在调用方 goroutine 上执行可执行文件发现。
//
// Python: get_shell_capabilities (shell_environment.py:160-162)，
// async 轮询改为条件变量等待。
func GetShellCapabilities(ctx context.Context, osType string) ShellCapabilities {
	return capabilityCache.get(ctx, osType)
}

// BuildShellEnvironmentPrompt 渲染能力快照，不探测文件系统。
//
// Python: build_shell_environment_prompt (shell_environment.py:285-339)
func BuildShellEnvironmentPrompt(language, osType string, capabilities ShellCapabilities) string {
	if !capabilities.Detected {
		if language == "cn" {
			return "Shell 能力：尚未确认。请使用当前平台的命令和路径语法；" +
				"不要假定 PowerShell、Git Bash 或 bash/sh 已安装。"
		}
		return ("Shell capabilities: not yet verified. Use commands and paths appropriate " +
			"for this platform; do not assume PowerShell, Git Bash or bash/sh is installed.")
	}

	if strings.HasPrefix(osType, "win") {
		return buildWindowsShellPrompt(language, capabilities)
	}
	shellPath := capabilities.PosixShell
	if language == "cn" {
		return "Shell 能力：\n" +
			"- Bash/sh：" + status(language, shellPath) + "\n\n" +
			"Shell 选择规则：\n" +
			"- Linux/macOS 使用 bash/sh 风格命令；bash/sh 不可用时，使用当前平台实际可用的 Shell 或专用工具。\n" +
			"- 使用与当前平台匹配的命令和路径语法。"
	}
	return "Shell capabilities:\n" +
		"- Bash/sh: " + status(language, shellPath) + "\n\n" +
		"Shell selection rules:\n" +
		"- On Linux/macOS, use bash/sh-style commands; if bash/sh is unavailable, use the Shell actually available on the platform or a dedicated tool.\n" +
		"- Use command and path syntax that matches the current platform."
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// capabilityCache 进程级共享缓存。
// Python: _CAPABILITY_CACHE = _ShellCapabilityCache()
var capabilityCache = newShellCapabilityCache()

// newShellCapabilityCache 创建缓存（测试可注入 nowFunc）。
func newShellCapabilityCache() *shellCapabilityCache {
	return &shellCapabilityCache{nowFunc: time.Now}
}

// currentEnvironmentKey 计算当前环境身份。
func currentEnvironmentKey(osType string) environmentKey {
	// which() 也会搜索进程 cwd 与 PATH。展开 Git 覆盖变量同样跟踪其引用的自定义环境变量。
	cwd, _ := os.Getwd()
	overrides := []string{
		os.ExpandEnv(os.Getenv("GIT_BASH")),
		os.ExpandEnv(os.Getenv("GIT_BASH_PATH")),
	}
	// 缺失 PATH 用 OS 默认，而显式空 PATH 不是。缓存身份同时保留存在性与取值。
	values := make([]string, 0, len(probeEnvNames)+len(overrides))
	for _, name := range probeEnvNames {
		v, present := os.LookupEnv(name)
		_ = present
		values = append(values, v)
	}
	values = append(values, overrides...)
	return environmentKey{osType: osType, cwd: cwd, values: values}
}

// get 单飞获取能力快照。
//
// Python: _ShellCapabilityCache.get（轮询改为条件变量）
func (c *shellCapabilityCache) get(ctx context.Context, osType string) ShellCapabilities {
	key := currentEnvironmentKey(osType)
	start := false
	var probe *shellProbe
	c.mu.Lock()
	now := c.nowFunc()
	if c.cachedKey.equal(key) && (c.cached.Detected || now.Before(c.retryAt)) {
		cached := c.cached
		c.mu.Unlock()
		return cached
	}
	probe = c.probe
	if probe == nil {
		probe = &shellProbe{key: key, started: now}
		c.probe = probe
		start = true
	} else if !probe.key.equal(key) {
		// 配置变更不得绕过单探测上限。
		c.mu.Unlock()
		return ShellCapabilities{}
	}
	c.mu.Unlock()

	if start {
		go func() {
			result := detectShellCapabilities(osType)
			c.finish(probe, result, osType)
		}()
	}

	// Python 以轮询只读内存态避免共享 loop 持有的 Future；
	// Go 以条件变量等待结果写入，等价且无忙等。
	deadline := probe.started.Add(probeWaitSeconds)
	for {
		c.mu.Lock()
		result := probe.resultPtr
		c.mu.Unlock()
		if result != nil {
			if key.equal(currentEnvironmentKey(osType)) {
				return *result
			}
			return ShellCapabilities{}
		}
		if c.nowFunc().After(deadline) {
			c.mu.Lock()
			logTimeout := !probe.timeoutLogged
			probe.timeoutLogged = true
			c.mu.Unlock()
			if logTimeout {
				logger.Warn(shellProbeLogComponent).
					Msg("Shell capability probe exceeded 0.2s; continuing with unverified " +
						"capabilities while the single background probe remains active")
			}
			return ShellCapabilities{}
		}
		select {
		case <-ctx.Done():
			return ShellCapabilities{}
		case <-time.After(10 * time.Millisecond):
		}
	}
}

// finish 完成探测槽位并更新缓存。
//
// Python: _ShellCapabilityCache._finish
func (c *shellCapabilityCache) finish(probe *shellProbe, result ShellCapabilities, osType string) {
	unchanged := probe.key.equal(currentEnvironmentKey(osType))
	now := c.nowFunc()
	c.mu.Lock()
	// 绝不把跨环境变更采集到的结果标记为任一配置的已验证。
	if unchanged {
		cached := result
		probe.resultPtr = &cached
		c.cachedKey = probe.key
		c.cached = result
		c.retryAt = now.Add(probeRetrySeconds)
	} else {
		probe.resultPtr = &ShellCapabilities{}
	}
	c.probe = nil
	c.mu.Unlock()
	if now.Sub(probe.started) > probeWaitSeconds {
		logger.Info(shellProbeLogComponent).
			Msg("Shell capability probe finished after " + now.Sub(probe.started).String())
	}
}

// detectShellCapabilities 仅在后台 goroutine 执行同步 OS 探测。
//
// Python: _detect_shell_capabilities (shell_environment.py:251-265)
func detectShellCapabilities(osType string) ShellCapabilities {
	pathBash, _ := exec.LookPath("bash")
	if strings.HasPrefix(osType, "win") {
		return ShellCapabilities{
			Detected:   true,
			PathBash:   pathBash,
			PowerShell: availablePowershell(),
			GitBash:    availableGitBash(),
		}
	}
	posix := pathBash
	if posix == "" {
		posix, _ = exec.LookPath("sh")
	}
	return ShellCapabilities{Detected: true, PathBash: pathBash, PosixShell: posix}
}

// isWSLBashPath 判断是否 WSL stub bash。
//
// Python: _is_wsl_bash_path (shell_environment.py:164-172)
func isWSLBashPath(path string) bool {
	normalized := strings.ReplaceAll(path, "/", "\\")
	normalized = strings.ToLower(normalized)
	return strings.HasSuffix(normalized, "\\system32\\bash.exe") ||
		strings.HasSuffix(normalized, "\\sysnative\\bash.exe") ||
		strings.Contains(normalized, "\\windowsapps\\bash.exe")
}

// existingExecutable 存在且是普通文件时返回原路径。
//
// Python: _existing_executable
func existingExecutable(path string) string {
	if fi, err := os.Stat(path); err == nil && fi.Mode().IsRegular() {
		return path
	}
	return ""
}

// gitBashCandidates 收集 Git Bash 候选路径。
//
// Python: _git_bash_candidates (shell_environment.py:186-216)
func gitBashCandidates() []string {
	var candidates []string

	for _, envName := range []string{"GIT_BASH", "GIT_BASH_PATH"} {
		if envValue := os.Getenv(envName); envValue != "" {
			expanded := filepath.Clean(os.ExpandEnv(strings.Trim(envValue, `"`)))
			if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(expanded, "~") {
				expanded = filepath.Join(home, expanded[1:])
			}
			candidates = append(candidates, expanded)
		}
	}

	for _, baseEnv := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
		if basePath := os.Getenv(baseEnv); basePath != "" {
			gitRoot := filepath.Join(basePath, "Git")
			candidates = append(candidates,
				filepath.Join(gitRoot, "bin", "bash.exe"),
				filepath.Join(gitRoot, "usr", "bin", "bash.exe"),
			)
		}
	}

	if gitExe, err := exec.LookPath("git"); err == nil {
		dir := filepath.Dir(gitExe)
		for {
			if strings.EqualFold(filepath.Base(dir), "git") {
				candidates = append(candidates,
					filepath.Join(dir, "bin", "bash.exe"),
					filepath.Join(dir, "usr", "bin", "bash.exe"),
				)
				break
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	// Python: _dedupe_paths —— 按 key 小写去重保序
	seen := map[string]struct{}{}
	var result []string
	for _, p := range candidates {
		k := strings.ToLower(p)
		if _, dup := seen[k]; dup {
			continue
		}
		seen[k] = struct{}{}
		result = append(result, p)
	}
	return result
}

// availableGitBash 返回首个存在的 Git Bash 候选。
//
// Python: _available_git_bash
func availableGitBash() string {
	for _, candidate := range gitBashCandidates() {
		if exe := existingExecutable(candidate); exe != "" {
			return exe
		}
	}
	return ""
}

// availablePowershell 探测 PowerShell 可执行文件。
//
// Python: _available_powershell
func availablePowershell() string {
	for _, command := range []string{"pwsh", "powershell"} {
		if exe, err := exec.LookPath(command); err == nil && exe != "" {
			return exe
		}
	}
	if systemRoot := os.Getenv("SystemRoot"); systemRoot != "" {
		return existingExecutable(filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"))
	}
	return ""
}

// status 渲染「可用/不可用」状态片段。
//
// Python: _status
func status(language, path string) string {
	if path != "" {
		if language == "cn" {
			return "可用，路径 `" + path + "`"
		}
		return "available at `" + path + "`"
	}
	if language == "cn" {
		return "不可用"
	}
	return "unavailable"
}

// pathBashStatus 渲染 PATH bash 状态（含 WSL stub 提示）。
//
// Python: _path_bash_status
func pathBashStatus(language, path string) string {
	if path == "" {
		if language == "cn" {
			return "不可用"
		}
		return "unavailable"
	}
	if isWSLBashPath(path) {
		if language == "cn" {
			return "`" + path + "`（WSL stub，不作为 Git Bash 使用）"
		}
		return "`" + path + "` (WSL stub; do not treat it as Git Bash)"
	}
	if language == "cn" {
		return "可用，路径 `" + path + "`"
	}
	return "available at `" + path + "`"
}

// buildWindowsShellPrompt 渲染 Windows 平台 Shell 能力段。
//
// Python: build_shell_environment_prompt 的 win 分支
func buildWindowsShellPrompt(language string, capabilities ShellCapabilities) string {
	if language == "cn" {
		return "Shell 能力：\n" +
			"- PowerShell：" + status(language, capabilities.PowerShell) + "\n" +
			"- Git Bash：" + status(language, capabilities.GitBash) + "\n" +
			"- PATH bash：" + pathBashStatus(language, capabilities.PathBash) + "\n\n" +
			"Shell 选择规则：\n" +
			"- Windows 且 Git Bash 可用，或 PATH bash 明确不是 WSL stub 时，可以使用 bash/Git Bash 执行 POSIX 命令，例如 `ls`、`grep`、`cat`、`mkdir -p`、bash 脚本。\n" +
			"- Windows 且 Git Bash 不可用、PATH bash 也不可用或只是 WSL stub 时，不要使用 POSIX 命令；优先使用 PowerShell 或 cmd。\n" +
			"- PowerShell cmdlet 不要包在 bash 里执行，应直接使用 PowerShell。\n" +
			"- 使用与当前平台匹配的命令和路径语法。"
	}
	return "Shell capabilities:\n" +
		"- PowerShell: " + status(language, capabilities.PowerShell) + "\n" +
		"- Git Bash: " + status(language, capabilities.GitBash) + "\n" +
		"- PATH bash: " + pathBashStatus(language, capabilities.PathBash) + "\n\n" +
		"Shell selection rules:\n" +
		"- On Windows, when Git Bash is available, or PATH bash is clearly not a WSL stub, use bash/Git Bash for POSIX commands such as `ls`, `grep`, `cat`, `mkdir -p`, and bash scripts.\n" +
		"- On Windows, when Git Bash is unavailable and PATH bash is unavailable or only a WSL stub, do not use POSIX commands; prefer PowerShell or cmd.\n" +
		"- Do not wrap PowerShell cmdlets in bash; invoke PowerShell directly.\n" +
		"- Use command and path syntax that matches the current platform."
}
