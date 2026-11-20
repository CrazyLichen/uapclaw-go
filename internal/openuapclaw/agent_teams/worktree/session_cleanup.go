package worktree

import (
	"context"
	"os"
	"path/filepath"

	agent_teams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/tools/worktree"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// isUnderRootPath path 是否在 root 下（Python session_cleanup.py _is_under_root，
// commonpath 语义；不兼容根返回 false）。
// Python: session_cleanup.py _is_under_root(path, root) (:17-22)
func isUnderRootPath(path, root string) bool {
	return isUnderRoot(path, root)
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RemoveSessionWorktrees 删除一个团队会话下的全部受管 worktree。
// Python: session_cleanup.py remove_session_worktrees(team_name, session_id,
// *, manager=None) (:24-111)
//
// manager 为 nil 时构造默认启用隔离的 WorktreeManager（Python :52-53 等价）。
// 返回整体是否全部成功。
func RemoveSessionWorktrees(ctx context.Context, teamName string, sessionID string, manager *worktree.WorktreeManager) bool {
	if teamName == "" || sessionID == "" {
		return true
	}

	worktreesRoot := agent_teams.TeamSessionWorktreesDir(teamName, sessionID)
	st, err := os.Stat(worktreesRoot)
	if err != nil || !st.IsDir() {
		// Python: worktrees_root 不存在 → True；存在但非目录（含 symlink）→ 告警 False
		if os.IsNotExist(err) {
			return true
		}
		logger.Warn(logComponent).
			Str("team_name", teamName).
			Str("session_id", sessionID).
			Str("worktrees_root", worktreesRoot).
			Msg("Keeping unexpected session worktrees path")
		return false
	}
	if lstat, lerr := os.Lstat(worktreesRoot); lerr == nil && lstat.Mode()&os.ModeSymlink != 0 {
		logger.Warn(logComponent).
			Str("team_name", teamName).
			Str("session_id", sessionID).
			Str("worktrees_root", worktreesRoot).
			Msg("Keeping unexpected session worktrees path")
		return false
	}

	// 越界防护：worktrees 根必须落在会话目录下（Python :48-60）
	sessionRootReal := realPath(agent_teams.TeamSessionDir(teamName, sessionID))
	worktreesRootReal := realPath(worktreesRoot)
	if !isUnderRootPath(worktreesRootReal, sessionRootReal) {
		logger.Warn(logComponent).
			Str("team_name", teamName).
			Str("session_id", sessionID).
			Str("worktrees_root", worktreesRoot).
			Msg("Keeping session worktrees outside team session root")
		return false
	}

	if manager == nil {
		cfg := worktree.NewWorktreeConfig()
		cfg.Enabled = true
		manager = worktree.NewWorktreeManager(cfg, nil)
	}

	success := true
	entries, err := os.ReadDir(worktreesRoot)
	if err != nil {
		logger.Warn(logComponent).Str("worktrees_root", worktreesRoot).Err(err).
			Msg("Failed to list session worktrees root")
		return false
	}
	for _, entry := range entries {
		entryPath := filepath.Join(worktreesRoot, entry.Name())
		entryReal := realPath(entryPath)
		if !isUnderRootPath(entryReal, worktreesRootReal) {
			logger.Warn(logComponent).
				Str("team_name", teamName).
				Str("session_id", sessionID).
				Str("entry_path", entryPath).
				Msg("Keeping session worktree path outside worktrees root")
			success = false
			continue
		}

		info, ierr := entry.Info()
		if ierr != nil {
			continue
		}
		isSymlink := entry.Type()&os.ModeSymlink != 0
		if isSymlink || info.Mode().IsRegular() {
			// 符号链接与普通文件直接 unlink（Python :78-80）
			if uerr := os.Remove(entryPath); uerr != nil {
				logger.Warn(logComponent).Str("entry_path", entryPath).Err(uerr).
					Msg("Failed to remove session worktree entry")
				success = false
			}
			continue
		}
		if !info.IsDir() {
			continue
		}

		repoRoot, gerr := worktree.FindCanonicalGitRoot(ctx, entryPath)
		removed := false
		if gerr == nil && repoRoot != "" {
			removed = manager.RemoveWorktreeForce(ctx, entryPath, repoRoot)
		}
		if !removed {
			if _, serr := os.Stat(entryPath); serr == nil {
				// Python _rmtree（asyncio.to_thread 的 Go 直删等价）
				if rerr := os.RemoveAll(entryPath); rerr != nil {
					logger.Warn(logComponent).
						Str("team_name", teamName).
						Str("session_id", sessionID).
						Str("entry_path", entryPath).
						Err(rerr).
						Msg("Failed to remove session worktree")
					success = false
				}
			}
		}
	}

	// 清 worktrees 根目录本身（Python :98-105）
	if _, serr := os.Stat(worktreesRoot); serr == nil {
		if rerr := os.RemoveAll(worktreesRoot); rerr != nil {
			logger.Warn(logComponent).
				Str("team_name", teamName).
				Str("session_id", sessionID).
				Str("worktrees_root", worktreesRoot).
				Err(rerr).
				Msg("Failed to remove session worktrees root")
			success = false
		}
	}
	return success
}
