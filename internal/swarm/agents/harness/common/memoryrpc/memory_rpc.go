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

// validateEditPath 校验编辑路径合法性并返回解析后的绝对路径。
// Python: _validate_edit_path (L70-102)
func validateEditPath(rawPath, workspace string, projectDir string) (bool, string) {
	// Python: normalized = raw_path.replace("\\", "/")
	normalized := strings.ReplaceAll(rawPath, "\\", "/")
	// Python: expanded = os.path.expanduser(normalized)
	expanded := expandUser(normalized)

	var absPath string
	if filepath.IsAbs(expanded) {
		// Python: abs_path = os.path.normpath(expanded)
		absPath = filepath.Clean(expanded)
	} else {
		// Python: workspace_resolved = os.path.normpath(os.path.abspath(os.path.join(workspace, expanded)))
		absPath = filepath.Clean(absoluteJoin(workspace, expanded))
		if projectDir != "" {
			// Python: project_resolved 在白名单内时优先采用（L79-82）
			projectResolved := filepath.Clean(absoluteJoin(projectDir, expanded))
			if isInAllowedDirs(projectResolved, workspace, projectDir) {
				absPath = projectResolved
			}
		}
	}

	if isInAllowedDirs(absPath, workspace, projectDir) {
		return true, absPath
	}

	// 特例：UAPCLAWSWARM.md / UAPCLAWSWARM.local.md 且父目录是 workspace/project_dir 根
	// Python: basename in ("JIUWENSWARM.md", "JIUWENSWARM.local.md")（品牌映射）
	basename := filepath.Base(absPath)
	if basename == "UAPCLAWSWARM.md" || basename == "UAPCLAWSWARM.local.md" {
		parent := filepath.Dir(absPath)
		workspaceNorm := filepath.Clean(workspace)
		if runtime.GOOS == "windows" {
			// Python: parent.lower() == workspace_norm.lower()
			if strings.EqualFold(parent, workspaceNorm) {
				return true, absPath
			}
			if projectDir != "" && strings.EqualFold(parent, filepath.Clean(projectDir)) {
				return true, absPath
			}
		} else {
			if parent == workspaceNorm {
				return true, absPath
			}
			if projectDir != "" && parent == filepath.Clean(projectDir) {
				return true, absPath
			}
		}
	}

	return false, "Path not in allowed memory directories: " + rawPath
}

// classifyMemoryFile 分类记忆文件 kind。
// Python: _classify_memory_file (L105-118)
func classifyMemoryFile(path, workspace string) string {
	// Python: if "coding_memory" in path: return "coding"
	if strings.Contains(path, "coding_memory") {
		return "coding"
	}
	// Python: parts = Path(path).parts; if "memory" in parts: return "auto"
	if pathPartContains(path, "memory") {
		return "auto"
	}
	// Python: if ".jiuwen" in parts: return "project"（品牌映射 .uapclaw）
	if pathPartContains(path, ".uapclaw") {
		return "project"
	}
	// Python: if path.endswith(".local.md"): return "local"
	if strings.HasSuffix(path, ".local.md") {
		return "local"
	}
	// Python: jiuwen_dir = os.path.expanduser("~/.jiuwen"); if path.startswith(jiuwen_dir): return "user"
	jiuwenDir := filepath.Join(utilpath.UserHomeDir(), ".uapclaw")
	if strings.HasPrefix(path, jiuwenDir) {
		return "user"
	}
	return "project"
}

// pathPartContains 判断路径段中是否含指定目录名（对齐 Python Path.parts 成员判定）
func pathPartContains(path, part string) bool {
	for _, p := range strings.Split(filepath.ToSlash(path), "/") {
		if p == part {
			return true
		}
	}
	return false
}

// relativePath 返回相对 project_dir 或 workspace 的路径，都不在则返回原路径。
// Python: _relative_path (L121-156)
func relativePath(absPath, workspace, projectDir string) string {
	absPathNorm := filepath.Clean(absPath)
	if projectDir != "" {
		projectDirNorm := filepath.Clean(projectDir)
		if runtime.GOOS == "windows" {
			absLower := strings.ToLower(absPathNorm)
			projLower := strings.ToLower(projectDirNorm)
			if strings.HasPrefix(absLower, projLower+string(os.PathSeparator)) || absLower == projLower {
				// Python: return os.path.relpath(abs_path, project_dir)（原始 abs_path）
				if rel, err := filepath.Rel(projectDirNorm, absPath); err == nil {
					return rel
				}
			}
		} else {
			if strings.HasPrefix(absPathNorm, projectDirNorm+string(os.PathSeparator)) || absPathNorm == projectDirNorm {
				if rel, err := filepath.Rel(projectDirNorm, absPath); err == nil {
					return rel
				}
			}
		}
	}
	workspaceNorm := filepath.Clean(workspace)
	if runtime.GOOS == "windows" {
		absLower := strings.ToLower(absPathNorm)
		wsLower := strings.ToLower(workspaceNorm)
		if strings.HasPrefix(absLower, wsLower+string(os.PathSeparator)) || absLower == wsLower {
			if rel, err := filepath.Rel(workspaceNorm, absPath); err == nil {
				return rel
			}
		}
	} else {
		if strings.HasPrefix(absPathNorm, workspaceNorm+string(os.PathSeparator)) || absPathNorm == workspaceNorm {
			if rel, err := filepath.Rel(workspaceNorm, absPath); err == nil {
				return rel
			}
		}
	}
	// Python: return abs_path（原始路径兜底）
	return absPath
}

// expandUser 展开 ~ 前缀（对齐 Python os.path.expanduser 的 ~ 与 ~/ 前缀分支）
func expandUser(path string) string {
	if path == "~" {
		return utilpath.UserHomeDir()
	}
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(utilpath.UserHomeDir(), strings.TrimPrefix(path, "~/"))
	}
	return path
}

// absoluteJoin 拼接并转绝对路径（对齐 Python os.path.abspath：相对基路径时补 cwd）
func absoluteJoin(base, rel string) string {
	joined := filepath.Join(base, rel)
	if !filepath.IsAbs(joined) {
		if abs, err := filepath.Abs(joined); err == nil {
			return abs
		}
	}
	return joined
}
