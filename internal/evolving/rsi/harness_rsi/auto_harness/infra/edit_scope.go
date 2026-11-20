package infra

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
)

// allowedSourceEditPrefixes 源码路径允许前缀（对齐 ALLOWED_SOURCE_EDIT_PREFIXES）
var allowedSourceEditPrefixes = []string{
	"jiuwenswarm/",
	"openjiuwen/dev_tools/",
	"openjiuwen/harness/",
	"openjiuwen/core/",
}

// allowedSupportEditPrefixes 配套路径允许前缀（对齐 ALLOWED_SUPPORT_EDIT_PREFIXES）
var allowedSupportEditPrefixes = []string{
	"tests/",
	"examples/",
	"docs/en/",
	"docs/zh/",
}

// allowedEditPrefixes 编辑范围总前缀（对齐 ALLOWED_EDIT_PREFIXES）
var allowedEditPrefixes = append(
	append([]string{}, allowedSourceEditPrefixes...),
	allowedSupportEditPrefixes...,
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NormalizeRepoPath 将工具路径归一化为 repo 相对 POSIX 路径
// （对齐 normalize_repo_path；超出项目根时返回绝对 POSIX 路径）。
func NormalizeRepoPath(ctx context.Context, path string) string {
	raw := strings.TrimSpace(path)
	if raw == "" {
		return ""
	}

	currentCwd, err := cwd.GetCwd(ctx)
	if err != nil || currentCwd == "" {
		currentCwd, _ = filepath.Abs(".")
	}
	projectRoot, err := cwd.GetProjectRoot(ctx)
	if err != nil || projectRoot == "" {
		projectRoot = currentCwd
	}
	expanded := expandUserPath(raw)
	var resolved string
	if filepath.IsAbs(expanded) {
		resolved, _ = filepath.Abs(expanded)
	} else {
		resolved, _ = filepath.Abs(filepath.Join(currentCwd, expanded))
	}
	if rel, err := filepath.Rel(projectRoot, resolved); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(resolved)
}

// expandUserPath 展开 ~ 前缀（对齐 Path.expanduser()）
func expandUserPath(path string) string {
	if strings.HasPrefix(path, "~/") || path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// IsAllowedRepoEditPath 判断路径是否在 auto-harness 允许的编辑范围内
// （对齐 is_allowed_repo_edit_path）
func IsAllowedRepoEditPath(ctx context.Context, path string) bool {
	normalized := NormalizeRepoPath(ctx, path)
	for _, prefix := range allowedEditPrefixes {
		if strings.HasPrefix(normalized, prefix) {
			return true
		}
	}
	return false
}

// RenderEditScope 渲染提示词用的稳定编辑范围块（对齐 render_edit_scope；
// 提示词内容逐字同步，品牌字符串不改）
func RenderEditScope(header string) string {
	if header == "" {
		header = "本轮允许变更范围"
	}
	return header + ":\n" +
		"- 源码路径允许 `jiuwenswarm/**`、`openjiuwen/dev_tools/**`、`openjiuwen/harness/**`、`openjiuwen/core/**`\n" +
		"- `jiuwenswarm/**`、`openjiuwen/dev_tools/**`、`openjiuwen/harness/**`、`openjiuwen/core/**` 下的模块内 " +
		"README/Markdown 视为源码目录内容，可正常修改，例如 " +
		"`jiuwenswarm/agents/harness/README.md` 或 `openjiuwen/harness/cli/README.md`\n" +
		"- 配套文件允许新增或修改 `tests/**`、`examples/**`\n" +
		"- 如果任务需要新增或更新仓库级文档，只能写入 `docs/en/` 和 `docs/zh/` 下的 Markdown 文件；不要在 `docs/` 根目录或其他子目录新增文档\n" +
		"- 不要修改 `openjiuwen/rsi/harness_rsi/auto_harness/**` 或其他源码目录\n" +
		"- 如果任务必须改到范围外路径，停止并明确报告范围冲突，不要自行越界"
}
