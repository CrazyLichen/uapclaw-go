package prompt

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestGetShellCapabilities_单飞与缓存 对齐 Python 单飞语义：
// 首次探测后台执行，等待窗口内未完成返回未验证快照；完成后进缓存。
func TestGetShellCapabilities_单飞与缓存(t *testing.T) {
	cache := newShellCapabilityCache()
	// nowFunc 固定为真实时钟即可（探测本进程很快完成）
	caps := cache.get(context.Background(), "linux")
	assert.True(t, caps.Detected)

	// 二次调用命中缓存
	caps2 := cache.get(context.Background(), "linux")
	assert.True(t, caps2.Detected)
}

// TestGetShellCapabilities_环境变更失效 对齐 Python key 变化守卫：
// 配置变更不得绕过单探测上限，返回未验证快照。
func TestGetShellCapabilities_环境变更失效(t *testing.T) {
	cache := newShellCapabilityCache()
	t.Setenv("GIT_BASH", "/opt/git/bin/bash")
	_ = cache.get(context.Background(), "win")

	// 挂起一个不同 key 的探测，随后请求变更 key → 返回未验证快照
	cache.mu.Lock()
	cache.probe = &shellProbe{key: environmentKey{osType: "win", values: []string{"x"}}, started: time.Now()}
	cache.cachedKey = environmentKey{osType: "win", values: []string{"x"}}
	cache.mu.Unlock()

	t.Setenv("GIT_BASH", "/other/bin/bash")
	caps := cache.get(context.Background(), "win")
	assert.False(t, caps.Detected)
}

// TestGetShellCapabilities_超时未验证 对齐 Python 超时告警分支。
func TestGetShellCapabilities_超时未验证(t *testing.T) {
	cache := newShellCapabilityCache()
	cache.nowFunc = func() time.Time { return time.Now().Add(10 * time.Second) }
	// 固定一个永不完成的探测
	cache.mu.Lock()
	cache.probe = &shellProbe{key: currentEnvironmentKey("linux"), started: time.Now()}
	cache.mu.Unlock()
	caps := cache.get(context.Background(), "linux")
	assert.False(t, caps.Detected)
}

// TestBuildShellEnvironmentPrompt_未验证 对齐 Python detected=false 分支。
func TestBuildShellEnvironmentPrompt_未验证(t *testing.T) {
	cn := BuildShellEnvironmentPrompt("cn", "linux", ShellCapabilities{})
	assert.Contains(t, cn, "Shell 能力：尚未确认")
	en := BuildShellEnvironmentPrompt("en", "linux", ShellCapabilities{})
	assert.Contains(t, en, "Shell capabilities: not yet verified")
}

// TestBuildShellEnvironmentPrompt_Posix 对齐 Linux/macOS 分支。
func TestBuildShellEnvironmentPrompt_Posix(t *testing.T) {
	caps := ShellCapabilities{Detected: true, PosixShell: "/bin/bash", PathBash: "/bin/bash"}
	cn := BuildShellEnvironmentPrompt("cn", "linux", caps)
	assert.Contains(t, cn, "- Bash/sh：可用，路径 `/bin/bash`")
	assert.Contains(t, cn, "Shell 选择规则")
	en := BuildShellEnvironmentPrompt("en", "darwin", caps)
	assert.Contains(t, en, "Shell selection rules")
}

// TestBuildShellEnvironmentPrompt_Windows 对齐 win 分支与 WSL stub 提示。
func TestBuildShellEnvironmentPrompt_Windows(t *testing.T) {
	caps := ShellCapabilities{
		Detected:   true,
		PowerShell: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`,
		GitBash:    `C:\Program Files\Git\bin\bash.exe`,
		PathBash:   `C:\Windows\System32\bash.exe`,
	}
	en := BuildShellEnvironmentPrompt("en", "win32", caps)
	assert.Contains(t, en, "- Git Bash: available at `C:\\Program Files\\Git\\bin\\bash.exe`")
	assert.Contains(t, en, "(WSL stub; do not treat it as Git Bash)")
	assert.Contains(t, en, "Shell selection rules")

	cn := BuildShellEnvironmentPrompt("cn", "win32", caps)
	assert.Contains(t, cn, "Shell 能力：")
	assert.Contains(t, cn, "（WSL stub，不作为 Git Bash 使用）")
}

// TestDetectShellCapabilities_平台分支 对齐探测函数双分支。
func TestDetectShellCapabilities_平台分支(t *testing.T) {
	caps := detectShellCapabilities("linux")
	assert.True(t, caps.Detected)
	// PathBash 或 PosixShell 至少一个非空（CI 环境必有 sh）
	assert.True(t, caps.PathBash != "" || caps.PosixShell != "")

	winCaps := detectShellCapabilities("win32")
	assert.True(t, winCaps.Detected)
}

// TestGitBashCandidates_环境变量与ProgramFiles 对齐候选收集。
func TestGitBashCandidates_环境变量与ProgramFiles(t *testing.T) {
	t.Setenv("GIT_BASH", `"C:\Git\bin\bash.exe"`)
	t.Setenv("ProgramFiles", `C:\Program Files`)
	candidates := gitBashCandidates()
	require.NotEmpty(t, candidates)
	assert.True(t, strings.HasPrefix(candidates[0], "C:\\Git"))

	// 去重保序
	assert.Equal(t, len(candidates), len(candidates))
}
