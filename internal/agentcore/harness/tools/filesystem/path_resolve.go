package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveToolFilePath 解析工具文件路径。
// 对齐 Python: _resolve_tool_file_path (filesystem.py L255-266)
// 1. 展开 ~ 为用户目录
// 2. UNC 路径 (\\ 或 // 开头) 或绝对路径 → 直接返回
// 3. 相对路径 → 基于 cwd 解析为绝对路径
func ResolveToolFilePath(ctx context.Context, filePath string) string {
	// 展开 ~ 为用户主目录
	expanded, err := expandUser(filePath)
	if err != nil {
		expanded = filePath
	}

	// UNC 路径或绝对路径 → 直接返回
	if isUNCPath(expanded) || filepath.IsAbs(expanded) {
		return expanded
	}

	// 相对路径 → 基于 cwd 解析为绝对路径
	workDir := cwd.GetCwd(ctx)
	absWorkDir, err := filepath.Abs(workDir)
	if err != nil {
		absWorkDir = workDir
	}
	return filepath.Join(absWorkDir, expanded)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// expandUser 展开路径中的 ~ 为用户主目录。
// 对齐 Python: os.path.expanduser
func expandUser(path string) (string, error) {
	if !strings.HasPrefix(path, "~") {
		return path, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path, err
	}
	return strings.Replace(path, "~", home, 1), nil
}

// isUNCPath 判断是否为 UNC 路径（\\ 或 // 开头）。
// 对齐 Python: _is_unc_path (filesystem.py L269-270)
func isUNCPath(pathValue string) bool {
	return strings.HasPrefix(pathValue, `\\`) || strings.HasPrefix(pathValue, "//")
}
