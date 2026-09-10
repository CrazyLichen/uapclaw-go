package filesystem

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveToolFilePath 解析工具文件路径。
// Python: _resolve_tool_file_path (filesystem.py L255-266)
// 1. 展开 ~ 为用户目录
// 2. UNC 路径 (\\ 或 // 开头) 或绝对路径 → 直接返回
// 3. 相对路径 → 基于 cwd 解析为绝对路径
func ResolveToolFilePath(ctx context.Context, filePath string) string {
	// 展开 ~ 为用户主目录
	expanded := pathutil.ExpandHome(filePath)

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

// isUNCPath 判断是否为 UNC 路径（\\ 或 // 开头）。
// Python: _is_unc_path (filesystem.py L269-270)
func isUNCPath(pathValue string) bool {
	return strings.HasPrefix(pathValue, `\\`) || strings.HasPrefix(pathValue, "//")
}
