package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// exitWorktreeDescription exit_worktree 工具双语描述
var exitWorktreeDescription = map[string]string{
	"cn": `退出由 enter_worktree 创建的 worktree 会话,将工作目录恢复到原始位置。

## 作用范围

仅操作当前会话中由 enter_worktree 创建的 worktree。不会触碰:
- 手动通过 ` + "`git worktree add`" + ` 创建的 worktree
- 其他成员的 worktree
- 从未调用 enter_worktree 时当前所在的目录

在 enter_worktree 会话之外调用为空操作(no-op)。

## 何时使用

- 任务完成,需要退出 worktree
- 需要切换到其他工作上下文

## 参数

- ` + "`action`" + `(必填):` + "`\"keep\"`" + ` 或 ` + "`\"remove\"`" + `
  - ` + "`\"keep\"`" + ` -- 保留 worktree 目录和分支在磁盘上,后续可再次进入
  - ` + "`\"remove\"`" + ` -- 删除 worktree 目录及其分支,适用于工作已完成或已放弃
- ` + "`discard_changes`" + `(可选,默认 false):仅在 ` + "`action=\"remove\"`" + ` 时有意义。当 worktree 有未提交文件或未合并提交时,工具会拒绝删除并列出变更,需设为 true 确认丢弃

## 行为

- 恢复会话工作目录到 enter_worktree 之前的位置
- action=remove 时,先检测未提交变更和新提交,有变更则拒绝(除非 discard_changes=true)
- 退出后可再次调用 enter_worktree 创建新的 worktree`,
	"en": `Exit a worktree session created by enter_worktree and restore the working directory to its original location.

## Scope

Only operates on the worktree created by enter_worktree in the current session. Will NOT touch:
- Worktrees created manually with ` + "`git worktree add`" + `
- Other members' worktrees
- The current directory if enter_worktree was never called

Calling outside an enter_worktree session is a no-op.

## When to Use

- Task is complete and you need to leave the worktree
- Need to switch to a different working context

## Parameters

- ` + "`action`" + ` (required): ` + "`\"keep\"`" + ` or ` + "`\"remove\"`" + `
  - ` + "`\"keep\"`" + ` -- leave the worktree directory and branch on disk for later use
  - ` + "`\"remove\"`" + ` -- delete the worktree directory and its branch; use when work is done or abandoned
- ` + "`discard_changes`" + ` (optional, default false): only meaningful with ` + "`action: \"remove\"`" + `. When the worktree has uncommitted files or unmerged commits, the tool refuses to remove and lists changes; set to true to confirm discard

## Behavior

- Restores the session's working directory to where it was before enter_worktree
- On action=remove, detects uncommitted changes and new commits first; refuses unless discard_changes=true
- After exit, enter_worktree can be called again to create a fresh worktree`,
}

// ──────────────────────────── 结构体 ────────────────────────────

// ExitWorktreeMetadataProvider exit_worktree 工具元数据提供者
type ExitWorktreeMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetExitWorktreeMetadataProviderInputParams 构建 exit_worktree 工具的参数 Schema
func GetExitWorktreeMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"action":          {"cn": "\"keep\" 保留 worktree 目录和分支在磁盘上;\"remove\" 删除目录和分支", "en": "\"keep\" leaves the worktree and branch on disk; \"remove\" deletes both"},
		"discard_changes": {"cn": "仅在 action=\"remove\" 且 worktree 有未提交文件或未合并提交时需设为 true。工具会先拒绝并列出变更,确认后再设此参数重新调用", "en": "Required true when action is \"remove\" and the worktree has uncommitted files or unmerged commits. The tool will refuse and list them otherwise"},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action":          map[string]any{"type": "string", "enum": []any{"keep", "remove"}, "description": d("action")},
			"discard_changes": map[string]any{"type": "boolean", "description": d("discard_changes")},
		},
		"required": []any{"action"},
	}
}

func (p *ExitWorktreeMetadataProvider) GetName() string { return "exit_worktree" }
func (p *ExitWorktreeMetadataProvider) GetDescription(language string) string {
	if d, ok := exitWorktreeDescription[language]; ok {
		return d
	}
	return exitWorktreeDescription["cn"]
}
func (p *ExitWorktreeMetadataProvider) GetInputParams(language string) map[string]any {
	return GetExitWorktreeMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&ExitWorktreeMetadataProvider{}) }
