// Package common - team_artifacts.go 提供团队范围的最终产物目录与
// 成员工作目录。
//
// 无 project_dir 的团队成员仍需要稳定的两类位置：per-member 临时工作目录
// （cwd，中间文件隔离落点）与共享最终产物目录（全队产物按文件名区分汇聚）。
//
// 磁盘布局——dated `chat-<n>` 目录、`.session_id` 碰撞标记与 `metadata.json`
// sidecar——由单 agent 无项目分配器（projectless_workspace.go）分配；本模块
// 只固定根：团队产物放在团队工作空间自身内部（`<team-workspace>/artifacts/`），
// 使 TeamWorkspaceRail 自动提交与 workspace_meta 工具的 git 历史无需额外
// 管线即可作用于产物目录：
//
//	<team-workspace>/artifacts/<YYYY-MM-DD>/chat-<n>/
//	    work/<member_slug>/      # per-member 临时工作目录（cwd）
//	    outputs/                 # 共享最终产物（按文件名区分）
//
// 全队成员共享同一 `chat-<n>` 目录：outputs/ 共享（一个目录，文件名区分）；
// work/ 按成员隔离。
//
// 对应 Python 代码：jiuwenswarm/common/team_artifacts.py（130 行）
package common

import (
	"os"
	"path/filepath"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// artifactsSubdir 产物根目录名。
	// Python: _ARTIFACTS_SUBDIR (team_artifacts.py:39)
	artifactsSubdir = "artifacts"
	// workSubdir 成员工作目录名。
	// Python: _WORK_SUBDIR (team_artifacts.py:40)
	workSubdir = "work"
	// outputsSubdir 共享产物目录名。
	// Python: _OUTPUTS_SUBDIR (team_artifacts.py:41)
	outputsSubdir = "outputs"
	// teamArtifactsRegistrySubdir 团队产物注册子目录。
	// Python: _REGISTRY_SUBDIR (team_artifacts.py:42)
	teamArtifactsRegistrySubdir = ".team_artifacts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamArtifactWorkspace 暴露给一个团队会话的共享根与产物目录。
// 两个字段都是 per-team（全队成员共享）。成员自己的临时工作目录是
// per-member 的，经 ResolveMemberWorkDir 单独解析——分配器运行时
// per-member 视图尚不存在。
// Python: TeamArtifactWorkspace (team_artifacts.py:48-58)
type TeamArtifactWorkspace struct {
	// RootDir 共享根目录（dated chat-<n>）
	RootDir string
	// OutputsDir 共享最终产物目录
	OutputsDir string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetTeamArtifactsDir 返回团队工作空间的产物根目录。
// 恒为 `<team-workspace>/artifacts`；其下 dated `chat-<n>` 子目录的分配
// 在 GetTeamArtifactWorkspace 进行。
// Python: get_team_artifacts_dir (team_artifacts.py:61-68)
func GetTeamArtifactsDir(teamWsRoot string) string {
	return resolvePath(filepath.Join(teamWsRoot, artifactsSubdir))
}

// GetTeamArtifactWorkspace 为团队会话创建或复用共享产物目录。
// 同一团队会话期间（全队成员共享）按团队 session id 复用同一 `chat-<n>`
// 目录。dated 目录/标记/元数据/注册分配委托给单 agent 无项目分配器
// AllocateTaskWorkspace，仅把根重定向到团队工作空间的 artifacts/ 并把
// 注册收敛到其 `.team_artifacts` 子目录，重定位的团队工作空间保持自包含。
// Python: get_team_artifact_workspace (team_artifacts.py:71-107)
func GetTeamArtifactWorkspace(teamWsRoot string, sessionID string, taskName string) (TeamArtifactWorkspace, error) {
	artifactsDir := GetTeamArtifactsDir(teamWsRoot)
	registryDir := filepath.Join(artifactsDir, teamArtifactsRegistrySubdir)
	root, err := AllocateTaskWorkspace(artifactsDir, registryDir, sessionID, taskName)
	if err != nil {
		return TeamArtifactWorkspace{}, err
	}
	outputsDir := filepath.Join(root, outputsSubdir)
	if err := os.MkdirAll(outputsDir, 0o755); err != nil {
		return TeamArtifactWorkspace{}, err
	}
	return TeamArtifactWorkspace{RootDir: root, OutputsDir: outputsDir}, nil
}

// ResolveMemberWorkDir 创建或复用成员的隔离临时工作目录。
// 无项目团队的每个成员在共享产物根下的自己的 `work/<member_slug>/` 中
// 运行，中间文件、缓存与草稿脚本彼此隔离。目录首次调用时创建，后续
// 调用复用。mkdir 失败沿 Python 抛出语义以 error 上抛。
// Python: resolve_member_work_dir (team_artifacts.py:110-129)
func ResolveMemberWorkDir(rootDir string, memberName string) (string, error) {
	safeMember := Slugify(memberName, "default", "member")
	workDir := filepath.Join(rootDir, workSubdir, safeMember)
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return "", err
	}
	return workDir, nil
}
