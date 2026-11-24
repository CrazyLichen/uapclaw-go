package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	agent_teams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation/cwd"
	harnessschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/worktree"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WorktreeOwnerScope 一个受管团队 worktree 的已解析所有者身份。
// Python: session_scope.py WorktreeOwnerScope（frozen dataclass，:21-30）；
// Go 按约定不可变（构造后不修改）。
type WorktreeOwnerScope struct {
	// TeamName 团队名
	TeamName string
	// MemberName 成员名
	MemberName string
	// SessionID 所属团队会话
	SessionID string
	// ProjectDir 锚定的项目目录（realpath）
	ProjectDir string
	// ProjectHash 项目目录哈希段
	ProjectHash string
	// ManagedRoot 会话拥有的 worktrees 根目录
	ManagedRoot string
	// WorktreeName 确定性 worktree slug
	WorktreeName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildWorktreeOwnerScope 为一个 worktree 解析严格的会话/项目所有者 scope。
// Python: session_scope.py build_worktree_owner_scope(*, team_name, member_name,
// spec, build_context) (:57-91)
//
// 偏差 D-25.7-e：Python 的 get_session_id() 读 contextvar 全局态；Go 显式
// 传 ctx（数据同源：sessionctx.GetSessionID）。
func BuildWorktreeOwnerScope(
	ctx context.Context,
	teamName string,
	memberName string,
	spec *atschema.TeamAgentSpec,
	buildContext harnessschema.BuildContextInterface,
) (WorktreeOwnerScope, error) {
	sessionID := agent_teams.GetSessionID(ctx)
	if sessionID == "" {
		return WorktreeOwnerScope{}, fmt.Errorf("Team worktree isolation requires an active team session_id")
	}
	projectDir, err := resolveProjectDir(spec, buildContext)
	if err != nil {
		return WorktreeOwnerScope{}, err
	}
	projectHash, err := agent_teams.ProjectWorktreeHash(projectDir)
	if err != nil {
		return WorktreeOwnerScope{}, err
	}
	managedRoot := agent_teams.TeamSessionWorktreesDir(teamName, sessionID)
	worktreeName, err := BuildTeammateWorktreeName(teamName, memberName, sessionID, projectHash)
	if err != nil {
		return WorktreeOwnerScope{}, err
	}
	return WorktreeOwnerScope{
		TeamName:     teamName,
		MemberName:   memberName,
		SessionID:    sessionID,
		ProjectDir:   projectDir,
		ProjectHash:  projectHash,
		ManagedRoot:  managedRoot,
		WorktreeName: worktreeName,
	}, nil
}

// ManagerForWorktreeScope 返回钉在 scope.ManagedRoot 上的 manager 视图。
// Python: session_scope.py manager_for_worktree_scope(manager, scope) (:93-100)
func ManagerForWorktreeScope(manager *worktree.WorktreeManager, scope WorktreeOwnerScope) (*worktree.WorktreeManager, error) {
	return manager.WithWorktreesDir(scope.ManagedRoot)
}

// EnterProjectWorktreeContext 把 worktree git 命令临时锚定到显式项目根。
// Python: session_scope.py enter_project_worktree_context(project_dir, worktree_root) (:102-125)
//
// 快照当前 cwd 五层状态，构造锚定的新 CwdState 注入 ctx。返回新 ctx 与
// 恢复闭包（restore 返回原 ctx）。偏差 D-25.7-e：Python 全局 contextvars
// 原地修改 + 显式 restore；Go ctx 不可变，恢复即回到旧 ctx（数据同源）。
func EnterProjectWorktreeContext(
	ctx context.Context,
	projectDir string,
	worktreeRoot string,
) (context.Context, func() context.Context, error) {
	// Python get_cwd() 快照失败语义：无 CwdState 时回退 os.getcwd()；
	// Go ctx 无 CwdState 时 GetCwd 报错——直接放行（后续操作自身会校验环境）
	_, err := cwd.GetCwd(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Python 快照五元组 (get_cwd, get_original_cwd, get_project_root,
	// get_workspace, get_team_workspace)（:114-120）。Go ctx 不可变：快照即
	// 旧 ctx 本身（restore 闭包直接返回），仅 original_cwd 需要显式带到新态。
	snapshotOriginal := cwd.GetOriginalCwd(ctx)
	snapshotTeamWorkspace := cwd.GetTeamWorkspace(ctx)

	// Python init_cwd(project_dir, project_root=project_dir,
	//                 workspace=worktree_root, team_workspace=snapshot[4])
	state := cwd.InitCwd(projectDir,
		cwd.WithProjectRoot(projectDir),
		cwd.WithWorkspace(worktreeRoot),
		cwd.WithTeamWorkspace(snapshotTeamWorkspace),
	)
	// original_cwd 保持快照值（Python restore 里 set_original_cwd；Go 在快照态一次成型）
	state.SetOriginalCwd(snapshotOriginal)
	newCtx := cwd.WithCwdState(ctx, state)
	restore := func() context.Context {
		return ctx
	}
	return newCtx, restore, nil
}

// RestoreProjectWorktreeContext 恢复会话级 worktree 操作后的 cwd 状态。
// Python: session_scope.py restore_project_worktree_context(snapshot) (:127-141)
//
// Go 侧 ctx 不可变，恢复即返回 Enter 时保存的旧 ctx；本函数保留以对齐
// Python 两个独立入口的形状（restore 闭包由调用方透传）。
func RestoreProjectWorktreeContext(restore func() context.Context) context.Context {
	return restore()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveProjectDir 解析并校验项目目录。
// Python: session_scope.py _resolve_project_dir(*, spec, build_context) (:44-56)
//
// 优先活载体 build_context.project_dir，缺失时回退种子 build_context_seed；
// 仍为空或目录不存在时显式报错（不得静默用 cwd/workspace 替代）。
func resolveProjectDir(spec *atschema.TeamAgentSpec, buildContext harnessschema.BuildContextInterface) (string, error) {
	var projectDir string
	if buildContext != nil {
		projectDir = buildContext.GetProjectDir()
	} else if spec != nil && spec.BuildContext != nil {
		projectDir = spec.BuildContext.GetProjectDir()
	}
	if projectDir == "" && spec != nil {
		// 种子回退：build_context_seed["project_dir"]（Python _seed_attr，:37-42）
		if seed, ok := spec.BuildContextSeed["project_dir"].(string); ok {
			projectDir = seed
		}
	}
	if strings.TrimSpace(projectDir) == "" {
		return "", fmt.Errorf("Team worktree isolation requires explicit build_context.project_dir")
	}
	// Python os.path.realpath → Go Abs+EvalSymlinks 组合（偏差 D-25.7-h，D-15.12-a 同款）
	resolved, err := filepathAbsEval(projectDir)
	if err != nil {
		return "", err
	}
	if st, err := os.Stat(resolved); err != nil || !st.IsDir() {
		return "", fmt.Errorf("Team worktree isolation project_dir does not exist: %s", projectDir)
	}
	return resolved, nil
}

// filepathAbsEval Python os.path.realpath 的 Go 等价：Abs + EvalSymlinks。
func filepathAbsEval(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if r, err := filepath.EvalSymlinks(abs); err == nil {
		return r, nil
	}
	return abs, nil
}
