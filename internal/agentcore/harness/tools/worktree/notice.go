package worktree

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildWorktreeNotice 构建 worktree 上下文提示。
// Python: build_worktree_notice(parent_cwd, worktree_cwd)
//
// 注入到 Agent 系统提示中，告知 Agent 当前处于隔离 worktree。
// 提示词内容 1:1 复刻 Python notice.py，禁止自行翻译或改写。
func BuildWorktreeNotice(parentCwd, worktreeCwd string) string {
	return fmt.Sprintf(
		"You are operating in an isolated git worktree at %s. "+
			"The parent context lives in %s — same repository, "+
			"same relative file structure, separate working copy.\n\n"+
			"Important:\n"+
			"- Paths from the parent context refer to %s\n"+
			"- Translate them to your worktree root before use\n"+
			"- Re-read files before editing if the parent may have modified them\n"+
			"- Your changes stay in this worktree and will not affect the parent",
		worktreeCwd, parentCwd, parentCwd)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
