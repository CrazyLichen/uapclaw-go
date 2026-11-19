package common

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// 本文件对齐 Python jiuwenswarm/common/config_panel/config_set_handlers.py 的
// detect_external_cli_agent / _detect_cli_at（:464-563）与版本参考表（:330-342）。
// 15.16d 批次回收偏差 D-15.16-d9：detect 原落在 gateway 包（历史落点），按
// Python 家移位到 swarm/common（与写链同包直调）；gateway 仅作 RPC 薄壳调用。

// ──────────────────────────── 常量 ────────────────────────────

// externalCLIVersionRe 从 --version 输出里提取语义版本号。
// Python: re.search(r"(\d+\.\d+\.\d+)", version_output) (config_set_handlers.py:537)。
var externalCLIVersionRe = regexp.MustCompile(`(\d+\.\d+\.\d+)`)

// ──────────────────────────── 全局变量 ────────────────────────────

// externalCLIReferenceVersions 各内置 CLI 的参考版本（"期望版本"）。
// Python 读安装包元数据（claude_agent_sdk._cli_version / openai-codex dist）；
// Go 侧无这些 Python 依赖，改用常量表，取上游 uv.lock 的锁定版本。
// **上游升版时需同步这里**，否则会把正常版本误报为"偏旧"。
var externalCLIReferenceVersions = map[string]string{
	"claude": "0.2.115",
	"codex":  "0.144.4",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsKnownExternalCLIAgent 判断是否为已知外部 CLI 种类（内置 + 注册表自定义）。
func IsKnownExternalCLIAgent(agent string) bool {
	for _, kind := range ExternalCliAgentKinds() {
		if kind == agent {
			return true
		}
	}
	return false
}

// DetectExternalCLIAgent 探测外部 CLI 的路径/版本，返回上游同形状的 DTO。
// Python: detect_external_cli_agent (config_set_handlers.py:949)。本仓差异：
//   - reference_version 取 Python 侧包元数据（claude_agent_sdk / openai-codex），本仓无此依赖，恒回常量表；
//   - 不做 use_builtin 的托管 CLI 解析（managed runtime 未移植，偏差 D-15.16-d7 桌面版专属），退回 PATH 搜索。
func DetectExternalCLIAgent(cliAgent, cliPath string, useBuiltin bool) map[string]any {
	if !IsKnownExternalCLIAgent(cliAgent) {
		return map[string]any{
			"cli_agent":         cliAgent,
			"status":            "unavailable",
			"path":              "",
			"version":           "",
			"reference_version": "",
			"message":           "unsupported cli_agent: " + cliAgent,
		}
	}
	_ = useBuiltin
	referenceVersion := externalCLIReferenceVersions[cliAgent]

	resolved, reason, message := resolveExternalCLIPath(cliAgent, cliPath)
	if resolved == "" {
		status := "missing"
		if reason == "directory" || reason == "windows_script" {
			status = "unsupported"
		}
		return map[string]any{
			"cli_agent":         cliAgent,
			"status":            status,
			"path":              "",
			"version":           "",
			"reference_version": referenceVersion,
			"reason":            reason,
			"message":           message,
		}
	}

	versionOutput, versionErr := runExternalCLIVersionCommand(resolved)
	version := ""
	if match := externalCLIVersionRe.FindStringSubmatch(versionOutput); match != nil {
		version = match[1]
	}
	if strings.TrimSpace(versionOutput) == "" {
		return map[string]any{
			"cli_agent":         cliAgent,
			"status":            "unavailable",
			"path":              resolved,
			"version":           "",
			"reference_version": referenceVersion,
			"message":           firstNonEmpty(versionErr, "version command failed"),
		}
	}
	status := "ok"
	message = ""
	if version == "" {
		status = "warning"
		message = "version output could not be parsed"
	} else if detected, okDetected := parseVersionTuple(version); okDetected {
		// 只有"探测版本 < 参考版本"才降级为 warning（对齐上游 has_comparable_versions 分支）
		if reference, okReference := parseVersionTuple(referenceVersion); okReference && compareVersionTuple(detected, reference) < 0 {
			status = "warning"
			message = fmt.Sprintf("version %s may be incompatible with expected version %s", version, referenceVersion)
		}
	}
	return map[string]any{
		"cli_agent":         cliAgent,
		"status":            status,
		"path":              resolved,
		"version":           version,
		"reference_version": referenceVersion,
		"message":           message,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveExternalCLIPath 解析外部 CLI 可执行文件路径。
// 显式路径：必须是存在的文件且不是目录（Python resolve_external_cli_path 的
// directory / windows_script 两个 reason 在此覆盖）；空路径回退 PATH 搜索。
func resolveExternalCLIPath(cliAgent, cliPath string) (resolved, reason, message string) {
	if p := strings.TrimSpace(cliPath); p != "" {
		expanded := pathutil.ExpandHome(p)
		info, err := os.Stat(expanded)
		if err != nil {
			if os.IsNotExist(err) {
				return "", "not_found", "not found: " + p
			}
			return "", "unreadable", err.Error()
		}
		if info.IsDir() {
			return "", "directory", "path is a directory: " + p
		}
		if runtime.GOOS == "windows" && isWindowsScriptPath(expanded) {
			return "", "windows_script", "windows_script"
		}
		return expanded, "", ""
	}
	found, err := exec.LookPath(cliAgent)
	if err != nil {
		return "", "not_found", err.Error()
	}
	if runtime.GOOS == "windows" && isWindowsScriptPath(found) {
		return "", "windows_script", "windows_script"
	}
	return found, "", ""
}

// isWindowsScriptPath 判断是否是不受支持的 Windows 脚本（.bat/.cmd/.ps1）。
func isWindowsScriptPath(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".bat", ".cmd", ".ps1":
		return true
	}
	return false
}

// runExternalCLIVersionCommand 执行 `<cli> --version` 并返回合并输出。
// 失败时返回的错误信息会作为 message 透传给前端（对齐上游 version_error）。
func runExternalCLIVersionCommand(resolved string) (string, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, resolved, "--version")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil && text == "" {
		return "", err.Error()
	}
	return text, ""
}

// parseVersionTuple 解析三段版本元组；无法解析得 (零值, false)。
func parseVersionTuple(version string) ([3]int, bool) {
	var out [3]int
	text := strings.TrimSpace(version)
	if text == "" {
		return out, false
	}
	parts := strings.Split(text, ".")
	for i := 0; i < len(parts) && i < 3; i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			return out, false
		}
		out[i] = n
	}
	return out, true
}

// compareVersionTuple 比较两个三段版本元组：a<b 返回 -1，相等 0，a>b 返回 1。
func compareVersionTuple(a, b [3]int) int {
	for i := 0; i < 3; i++ {
		switch {
		case a[i] < b[i]:
			return -1
		case a[i] > b[i]:
			return 1
		}
	}
	return 0
}
