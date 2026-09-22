package worktree

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// MaxSlugLength worktree 名称最大长度。
// Python: MAX_SLUG_LENGTH = 64
const MaxSlugLength = 64

// ──────────────────────────── 全局变量 ────────────────────────────

// validSlugSegment 合法 slug 段正则。
// Python: VALID_SLUG_SEGMENT = re.compile(r"^[a-zA-Z0-9._-]+$")
var validSlugSegment = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateSlug 校验 worktree slug 安全性。
// Python: validate_slug(slug)
//
// 拒绝路径遍历、绝对路径、shell 元字符和超长名称。
func ValidateSlug(slug string) error {
	if len(slug) > MaxSlugLength {
		return fmt.Errorf("Invalid worktree name: must be %d characters or fewer (got %d)", MaxSlugLength, len(slug))
	}
	for _, segment := range strings.Split(slug, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("Invalid worktree name %q: must not contain \".\" or \"..\" path segments", slug)
		}
		if !validSlugSegment.MatchString(segment) {
			return fmt.Errorf("Invalid worktree name %q: each segment must be non-empty and contain only letters, digits, dots, underscores, and dashes", slug)
		}
	}
	return nil
}

// WorktreeBranchName 将 slug 转为 git 分支名。
// Python: worktree_branch_name(slug)
//
// 将 "/" 替换为 "+"，前缀加 "worktree-"。
// 例：feature-auth → worktree-feature-auth，user/feature-login → worktree-user+feature-login
func WorktreeBranchName(slug string) string {
	return "worktree-" + strings.ReplaceAll(slug, "/", "+")
}

// WorktreePathFor 计算 worktree 目录路径。
// Python: worktree_path_for(base_dir, slug)
//
// 路径格式：{baseDir}/.worktrees/{slug}
func WorktreePathFor(baseDir, slug string) string {
	return filepath.Join(baseDir, ".worktrees", slug)
}

// WorktreesDir 返回所有 worktree 的父目录。
// Python: worktrees_dir(base_dir)
func WorktreesDir(baseDir string) string {
	return filepath.Join(baseDir, ".worktrees")
}

// ──────────────────────────── 非导出函数 ────────────────────────────
