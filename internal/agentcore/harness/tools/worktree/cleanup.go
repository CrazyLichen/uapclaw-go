package worktree

import (
	"context"
	"os"
	"regexp"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ephemeralPatterns 临时 worktree slug 匹配模式。
// Python: EPHEMERAL_PATTERNS
var ephemeralPatterns = []*regexp.Regexp{
	// teammate-<member_name first 8 hex chars>
	regexp.MustCompile(`^teammate-[0-9a-f]{8}$`),
	// agent-<7hex>（向后兼容）
	regexp.MustCompile(`^agent-[0-9a-f]{7}$`),
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsEphemeralSlug 检查 slug 是否匹配临时模式。
// Python: is_ephemeral_slug(slug)
func IsEphemeralSlug(slug string) bool {
	for _, p := range ephemeralPatterns {
		if p.MatchString(slug) {
			return true
		}
	}
	return false
}

// CleanupStaleWorktrees 清理过期的临时 worktree。
// Python: cleanup_stale_worktrees(config, backend, *, current_worktree_path)
//
// 安全策略（fail-closed）：
// 1. 只清理匹配 ephemeral pattern 的 slug
// 2. 跳过当前 session 的 worktree
// 3. 检查未提交变更（git status）
// 4. 检查未推送提交（git rev-list）
// 5. 任何检查失败 → 跳过
func CleanupStaleWorktrees(ctx context.Context, config WorktreeConfig, backend WorktreeBackend, currentWorktreePath string) (int, error) {
	repoRoot, err := FindCanonicalGitRoot(ctx, cwd.GetCwd(ctx))
	if err != nil || repoRoot == "" {
		return 0, nil
	}

	workspace := cwd.GetWorkspace(ctx)
	if workspace == "" {
		return 0, nil
	}

	wtDir := WorktreesDir(workspace)
	entries, err := os.ReadDir(wtDir)
	if err != nil {
		return 0, nil
	}

	cutoffTime := time.Now().Add(-time.Duration(config.CleanupAfterDays) * 24 * time.Hour)
	removed := 0

	for _, entry := range entries {
		slug := entry.Name()
		if !IsEphemeralSlug(slug) {
			continue
		}

		wtPath := wtDir + string(os.PathSeparator) + slug

		// 跳过当前 session 的 worktree
		if currentWorktreePath != "" && wtPath == currentWorktreePath {
			continue
		}

		// 检查修改时间
		info, err := os.Stat(wtPath)
		if err != nil {
			continue
		}
		if info.ModTime().After(cutoffTime) {
			continue
		}

		// 检查未提交变更
		changes, err := StatusPorcelain(ctx, wtPath)
		if err != nil || len(changes) > 0 {
			continue
		}

		// 检查未推送提交
		unpushed := HasUnpushedCommits(ctx, wtPath)
		if unpushed == nil || *unpushed {
			continue
		}

		if backend.Remove(ctx, wtPath, repoRoot) {
			removed++
		}
	}

	if removed > 0 {
		_ = WorktreePrune(ctx, repoRoot)
	}

	return removed, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
