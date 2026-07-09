package tools

// ──────────────────────────── 结构体 ────────────────────────────

// EnterWorktreeMetadataProvider enter_worktree 工具元数据提供者
type EnterWorktreeMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// enterWorktreeDescription enter_worktree 工具双语描述
var enterWorktreeDescription = map[string]string{
	"cn": `创建或恢复一个隔离的 git worktree 并将当前会话切换到其中。

## 何时使用

- 需要在独立副本中修改代码,避免与其他成员的分支冲突和文件竞争
- 需要在不影响主仓库的前提下进行实验性修改

## 何时不使用

- 仅需要创建分支或切换分支 -- 使用 git 命令
- 不涉及并行修改同一仓库的场景

## 前置条件

- 当前必须在一个 git 仓库中
- 不能已经在一个 worktree 会话中(需先 exit_worktree)

## 行为

- 指定 ` + "`name`" + ` 时定位 agent workspace 下 ` + "`.worktrees/<name>`" + `; 若已存在则直接进入,否则基于 HEAD 创建
- 未指定 ` + "`name`" + ` 时使用当前 session 的默认 worktree 名称; 第一次自动生成,后续未指定时复用同一个名称
- 跨 session 不继承默认名称; 要进入其他 session 保留的 worktree 时必须显式传入 ` + "`name`" + `
- 将会话的工作目录(CWD)切换到新 worktree
- 所有后续文件操作和 shell 命令在 worktree 内执行,不影响主仓库
- 使用 exit_worktree 离开(keep 保留或 remove 删除)

## 参数

- ` + "`name`" + `(可选):worktree 名称。传入已保留的名称可重新进入该 worktree; 不提供则使用当前 session 的默认名称。`,
	"en": `Create or resume an isolated git worktree and switch the current session into it.

## When to Use

- Use this tool only when the user explicitly asks to work in a worktree
- The user says "worktree" (for example: start a worktree, work in a worktree, create a worktree, use a worktree)

## When NOT to Use

- Only need to create or switch branches -- use git commands
- The user asks to fix a bug or work on a feature -- use the normal workflow unless they specifically mention worktrees
- Never use this tool unless the user explicitly mentions worktree

## Requirements

- Must be inside a git repository
- Must not already be in a worktree session (exit_worktree first)

## Behavior

- When ` + "`name`" + ` is provided, resolves ` + "`.worktrees/<name>`" + ` under the agent workspace; enters it if it already exists, otherwise creates a new branch and worktree from HEAD
- When ` + "`name`" + ` is omitted, uses the current session's default worktree name; the first unnamed call generates it, and later unnamed calls reuse the same name
- Default names do not cross sessions; to enter a worktree retained by another session, pass ` + "`name`" + ` explicitly
- Switches the session's working directory (CWD) to the new worktree
- All subsequent file operations and shell commands execute inside the worktree, leaving the main repo unaffected
- Use exit_worktree to leave (keep to retain or remove to delete)

## Parameters

- ` + "`name`" + ` (optional): Worktree name. Pass a retained name to re-enter that worktree; if omitted, the current session's default name is used.`,
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetEnterWorktreeMetadataProviderInputParams 构建 enter_worktree 工具的参数 Schema
func GetEnterWorktreeMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"name": {
			"cn": `可选的 worktree 名称。每个 "/" 分隔的段只能包含字母、数字、点、下划线和短横线;总长度最多 64 字符。若该名称对应的 worktree 已存在则直接进入; 不提供则使用当前 session 的默认名称,首次未指定时自动生成`,
			"en": `Optional name for the worktree. Each "/"-separated segment may contain only letters, digits, dots, underscores, and dashes; max 64 chars total. If a worktree with this name already exists, it is re-entered. If omitted, the current session's default worktree name is used; the first unnamed call generates it`,
		},
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
			"name": map[string]any{"type": "string", "description": d("name")},
		},
		"required": []any{},
	}
}

func (p *EnterWorktreeMetadataProvider) GetName() string { return "enter_worktree" }
func (p *EnterWorktreeMetadataProvider) GetDescription(language string) string {
	if d, ok := enterWorktreeDescription[language]; ok {
		return d
	}
	return enterWorktreeDescription["cn"]
}
func (p *EnterWorktreeMetadataProvider) GetInputParams(language string) map[string]any {
	return GetEnterWorktreeMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&EnterWorktreeMetadataProvider{}) }
