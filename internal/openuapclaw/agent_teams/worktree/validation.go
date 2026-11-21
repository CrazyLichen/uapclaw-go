package worktree

import (
	"context"
	"fmt"

	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/worktree"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateMemberWorktreeIsolation 成员注册前校验项目 scope 与 git 仓库。
// Python: agent_configurator.py _validate_member_worktree_isolation(spec,
// team_name, member_name) (:68-84)
//
// project_dir 不在 git 仓库中时报错（文案一比一，Python RuntimeError 语义）。
func ValidateMemberWorktreeIsolation(ctx context.Context, spec *atschema.TeamAgentSpec, teamName, memberName string) error {
	scope, err := BuildWorktreeOwnerScope(ctx, teamName, memberName, spec, nil)
	if err != nil {
		return err
	}
	repoRoot, err := worktree.FindCanonicalGitRoot(ctx, scope.ProjectDir)
	if err != nil || repoRoot == "" {
		return fmt.Errorf("Team worktree isolation project_dir is not in a git repository: %s", scope.ProjectDir)
	}
	return nil
}
