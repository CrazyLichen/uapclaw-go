package externalcli

import (
	"path/filepath"
	"strings"

	agentteams "github.com/uapclaw/uapclaw-go/internal/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveExternalPaths 解析外部成员的 cwd 与额外可访问目录。
// cwd 选择顺序：显式配置 → worktree → 项目目录 → 团队工作区；
// 四个候选中非 cwd 者（去重）作为额外目录（如 Claude --add-dir）。
// Python: _resolve_external_paths (external_cli_spawn.py:281-298)
func ResolveExternalPaths(spec *schema.TeamAgentSpec, ctx *schema.TeamRuntimeContext,
	configuredCwd string, teamName string) (string, []string) {
	explicitCwd := pathValue(configuredCwd)
	worktreePath := pathValue(ctx.WorktreePath)
	projectDir := buildContextProjectDir(spec)
	teamWorkspace := teamWorkspacePath(spec, teamName)
	cwd := explicitCwd
	if cwd == "" {
		cwd = worktreePath
	}
	if cwd == "" {
		cwd = projectDir
	}
	if cwd == "" {
		cwd = teamWorkspace
	}
	var extraDirs []string
	for _, path := range []string{explicitCwd, worktreePath, projectDir, teamWorkspace} {
		appendExtraDir(&extraDirs, path, cwd)
	}
	return cwd, extraDirs
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// pathValue 非字符串化空值归一：空白/空串返回空串。
// Python: _path_value (239-243)；Python 返回 str | None，Go 收窄为 string
// （偏差 D-15.12-f：ResolveExternalPaths 入参 configuredCwd string 已对齐调用面语义）。
func pathValue(value string) string {
	return strings.TrimSpace(value)
}

// samePath 路径等价比较（normcase+normpath 语义）。
// Python: _same_path (246-247)
func samePath(left, right string) bool {
	return normalizePath(left) == normalizePath(right)
}

// normalizePath normpath+normcase 的 Go 等价（Clean + 小写化，Windows 大小写不敏感语义保留）。
func normalizePath(path string) string {
	return strings.ToLower(filepath.Clean(path))
}

// appendExtraDir 追加额外目录：空/与 cwd 相同/重复者跳过。
// Python: _append_extra_dir (250-257)
func appendExtraDir(result *[]string, path string, cwd string) {
	if path == "" {
		return
	}
	if cwd != "" && samePath(path, cwd) {
		return
	}
	for _, existing := range *result {
		if samePath(path, existing) {
			return
		}
	}
	*result = append(*result, path)
}

// teamWorkspacePath 团队共享工作区绝对路径：配置 root_path 优先，否则默认布局。
// Python: _team_workspace_path (260-264)
func teamWorkspacePath(spec *schema.TeamAgentSpec, teamName string) string {
	if spec.Workspace != nil && spec.Workspace.RootPath != "" {
		return spec.Workspace.RootPath
	}
	return agentteams.TeamWorkspaceDir(teamName)
}

// buildContextProjectDir 从 build_context 提取项目目录。
// Python: _build_context_project_dir (267-271)；getattr → 接口方法
func buildContextProjectDir(spec *schema.TeamAgentSpec) string {
	if spec.BuildContext == nil {
		return ""
	}
	return pathValue(spec.BuildContext.GetProjectDir())
}

// buildContextTeamOutputsDir 从 build_context 提取团队产物目录。
// Python: _build_context_team_outputs_dir (274-278)
func buildContextTeamOutputsDir(spec *schema.TeamAgentSpec) string {
	if spec.BuildContext == nil {
		return ""
	}
	return pathValue(spec.BuildContext.GetTeamOutputsDir())
}
