// Package memoryrpc 的 handler 实现见各函数。
// 包注释见 doc.go（多文件包注释只在 doc.go 一处）。
package memoryrpc

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	utilpath "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件标识（对齐 memory 包既有约定）
var logComponent = logger.ComponentAgentServer

// ──────────────────────────── 非导出函数 ────────────────────────────

// isForbiddenEnabled 判断禁止记忆定义是否启用。
// Python: _is_forbidden_enabled (memory_rpc.py L30-33)
func isForbiddenEnabled(cfg map[string]any) bool {
	// Python: mem_cfg = (config or {}).get("memory", {}) if isinstance(config, dict) else {}
	memCfg, _ := cfg["memory"].(map[string]any)
	if memCfg == nil {
		memCfg = map[string]any{}
	}
	// Python: forbidden = mem_cfg.get("forbidden_memory_definition", {})
	forbidden, _ := memCfg["forbidden_memory_definition"].(map[string]any)
	if forbidden == nil {
		forbidden = map[string]any{}
	}
	// Python: return bool(forbidden.get("enabled", False))
	enabled, _ := forbidden["enabled"].(bool)
	return enabled
}

// getCodingMemoryDir 返回 coding memory 目录路径。
// Python: _get_coding_memory_dir (L36-38)：project_dir 为空时用 "default"
func getCodingMemoryDir(workspace, projectDir string) string {
	projectName := "default"
	if projectDir != "" {
		projectName = filepath.Base(projectDir)
	}
	return filepath.Join(workspace, "coding_memory", projectName)
}

// getAllowedDirs 返回记忆编辑白名单目录。
// Python: _get_allowed_dirs (L41-51)：
// memory/coding_memory/workspace·.uapclaw/~/.uapclaw/project_dir/project_dir·.uapclaw
func getAllowedDirs(workspace, projectDir string) []string {
	allowedDirs := []string{
		filepath.Join(workspace, "memory"),
		getCodingMemoryDir(workspace, projectDir),
		filepath.Join(workspace, ".uapclaw"),
		filepath.Join(utilpath.UserHomeDir(), ".uapclaw"),
	}
	if projectDir != "" {
		allowedDirs = append(allowedDirs, projectDir)
		allowedDirs = append(allowedDirs, filepath.Join(projectDir, ".uapclaw"))
	}
	// Python: return [os.path.normpath(d) for d in allowed_dirs]
	for i, d := range allowedDirs {
		allowedDirs[i] = filepath.Clean(d)
	}
	return allowedDirs
}

// isInAllowedDirs 判断绝对路径是否在白名单目录内。
// Python: _is_in_allowed_dirs (L54-67)：Windows 大小写不敏感
func isInAllowedDirs(absPath, workspace, projectDir string) bool {
	if runtime.GOOS == "windows" {
		// Python: abs_path_lower = abs_path.lower()
		lower := strings.ToLower(absPath)
		for _, dir := range getAllowedDirs(workspace, projectDir) {
			dirLower := strings.ToLower(dir)
			if lower == dirLower || strings.HasPrefix(lower, dirLower+string(os.PathSeparator)) {
				return true
			}
		}
		return false
	}
	for _, dir := range getAllowedDirs(workspace, projectDir) {
		if absPath == dir || strings.HasPrefix(absPath, dir+string(os.PathSeparator)) {
			return true
		}
	}
	return false
}
