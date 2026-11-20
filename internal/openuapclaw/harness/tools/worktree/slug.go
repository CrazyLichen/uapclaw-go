package worktree

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

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
// Python: validate_slug (worktree/slug.py:20-40)
//
// 拒绝路径遍历、绝对路径、Windows 分隔符与盘符、shell 元字符和超长名称。
func ValidateSlug(slug string) error {
	if len(slug) > MaxSlugLength {
		return fmt.Errorf("invalid worktree name: must be %d characters or fewer (got %d)", MaxSlugLength, len(slug))
	}
	// 反斜杠在 Windows 上也是分隔符，先归一化再分段校验：
	// 否则 `..\evil` 会被当成单个段名，报的是"字符集不合法"，
	// 掩盖了真正的路径穿越意图。
	normalized := strings.ReplaceAll(slug, "\\", "/")
	for _, segment := range strings.Split(normalized, "/") {
		if segment == "." || segment == ".." {
			return fmt.Errorf("invalid worktree name %q: must not contain \".\" or \"..\" path segments", slug)
		}
		if !validSlugSegment.MatchString(segment) {
			return fmt.Errorf("invalid worktree name %q: each segment must be non-empty and contain only letters, digits, dots, underscores, and dashes", slug)
		}
	}
	return nil
}

// flattenSlug 把已校验的 slug 扁平化为单层路径段。
// Python: _flatten_slug (worktree/slug.py:42-52)
//
// 形如 "user/feature" 的嵌套 slug 对用户友好，但映射到嵌套目录时，
// 删除父 worktree 会连带删掉其下所有子 worktree。
// 上游在此处调用 validate_slug；Go 版返回 (string, bool)，非法时由调用方
// 决定降级方式（空串），避免改变既有函数签名。
func flattenSlug(slug string) (string, bool) {
	if err := ValidateSlug(slug); err != nil {
		return "", false
	}
	return strings.ReplaceAll(slug, "/", "+"), true
}

// WorktreeBranchName 将 slug 转为 git 分支名。
// Python: worktree_branch_name(slug)
//
// 前缀 "worktree-" + 扁平化 slug（"/" 替换为 "+"）。slug 非法时返回空串，
// 调用方应视为错误而非继续拼命令。
// 例：feature-auth → worktree-feature-auth，user/feature-login → worktree-user+feature-login
func WorktreeBranchName(slug string) string {
	flat, ok := flattenSlug(slug)
	if !ok {
		return ""
	}
	return "worktree-" + flat
}

// WorktreePathFor 计算 worktree 目录路径。
// Python: worktree_path_for(base_dir, slug)
//
// 路径格式：{baseDir}/.worktrees/{flattened-slug}。slug 非法时返回空串。
func WorktreePathFor(baseDir, slug string) string {
	flat, ok := flattenSlug(slug)
	if !ok {
		return ""
	}
	return filepath.Join(WorktreesDir(baseDir), flat)
}

// WorktreesDir 返回所有 worktree 的父目录。
// Python: worktrees_dir(base_dir)
func WorktreesDir(baseDir string) string {
	return filepath.Join(baseDir, ".worktrees")
}
