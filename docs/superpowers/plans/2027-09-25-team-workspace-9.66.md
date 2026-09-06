# 9.66 Team Workspace 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现团队共享工作空间（Team Workspace），提供 LOCAL 模式全功能（内存锁 + git 版本控制 + symlink 挂载 + auto_commit + 事件发布），同时建立 TeamTool 基类和 i18n locales 体系。

**Architecture:** TeamWorkspaceManager 是核心管理器（锁 + git + 挂载），TeamWorkspaceRail 作为 DeepAgentRail 子类拦截 .team/ 路径的读写，WorkspaceMetaTool 提供 lock/unlock/locks/history 4 个 action。三者紧密协作但各司其职：Manager 管状态，Rail 管拦截，Tool 管接口。i18n locales 包提供 Translator 闭包 + Markdown 描述文件加载机制。

**Tech Stack:** Go 1.22+, os/exec (git), os/symlink, sync.Mutex, go:embed (Markdown), schema.TypedEvent 事件体系

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 创建 | `internal/agent_teams/tools/locales/doc.go` | 包文档 |
| 创建 | `internal/agent_teams/tools/locales/translator.go` | Translator 类型 + MakeTranslator + loadToolDesc |
| 创建 | `internal/agent_teams/tools/locales/translator_test.go` | Translator 测试 |
| 创建 | `internal/agent_teams/tools/locales/descs/cn/workspace_meta.md` | 中文工具描述 |
| 创建 | `internal/agent_teams/tools/locales/descs/en/workspace_meta.md` | 英文工具描述 |
| 修改 | `internal/agent_teams/schema/i18n.go` | 补充 workspace_meta key |
| 创建 | `internal/agent_teams/tools/team_tool.go` | TeamTool 基类 |
| 创建 | `internal/agent_teams/tools/team_tool_test.go` | TeamTool 测试 |
| 修改 | `internal/agent_teams/team_workspace/doc.go` | 更新文件列表 |
| 修改 | `internal/agent_teams/team_workspace/models.go` | 补 HistoryEntry |
| 创建 | `internal/agent_teams/team_workspace/manager.go` | TeamWorkspaceManager |
| 创建 | `internal/agent_teams/team_workspace/manager_test.go` | Manager 测试 |
| 创建 | `internal/agent_teams/team_workspace/rail.go` | TeamWorkspaceRail |
| 创建 | `internal/agent_teams/team_workspace/rail_test.go` | Rail 测试 |
| 创建 | `internal/agent_teams/team_workspace/tool.go` | WorkspaceMetaTool |
| 创建 | `internal/agent_teams/team_workspace/tool_test.go` | Tool 测试 |
| 修改 | `internal/agent_teams/agent/infra.go` | WorkspaceManager 类型 any → *TeamWorkspaceManager |
| 修改 | `internal/agent_teams/agent/agent_configurator.go` | 回填 TODO(#9.66) |
| 修改 | `internal/agent_teams/harness.go` | MountedRails.TeamWorkspace 类型 |

---

### Task 1: i18n locales 基础设施

**Files:**
- Create: `internal/agent_teams/tools/locales/doc.go`
- Create: `internal/agent_teams/tools/locales/translator.go`
- Create: `internal/agent_teams/tools/locales/translator_test.go`
- Create: `internal/agent_teams/tools/locales/descs/cn/workspace_meta.md`
- Create: `internal/agent_teams/tools/locales/descs/en/workspace_meta.md`
- Modify: `internal/agent_teams/schema/i18n.go`

- [ ] **Step 1: 创建 descs 目录和 Markdown 文件**

创建 `internal/agent_teams/tools/locales/descs/cn/workspace_meta.md`（复刻 Python `openjiuwen/agent_teams/tools/locales/descs/cn/workspace_meta.md`）：

```markdown
团队共享工作空间的元数据工具：**文件锁管理** 和 **版本历史查询**。多个成员协作修改共享文件时使用。

## 范围

本工具**不做**文件读写。共享工作空间以 `.team/` 挂载点暴露，文件读写请使用标准的 `read_file` / `write_file` / `glob` 等文件系统工具访问 `.team/...` 路径。

## action

- **lock** — 获取文件独占锁（需 `path`）
- **unlock** — 释放文件锁（需 `path`）
- **locks** — 列出当前所有未过期的锁
- **history** — 查看文件的 git 版本历史（需 `path`），返回最近 10 条 commit（含 commit 哈希、作者、时间、消息）

## Lock 语义

- **独占**：同一文件同一时间只能有一个持有者
- **超时**：默认 300 秒自动过期，其他成员可在过期后回收
- **重入**：同一持有者对已持有的锁再次 `lock` 会刷新超时时间
- **协作性约定**：`write_file` 本身不会自动检查锁。写入共享文件前，成员应主动 `lock` → `write_file` → `unlock`，避免互相覆盖
- **获取失败**：返回 `Locked by {holder_name}`，说明当前持有者

## 何时使用

- 多成员并行修改 `.team/` 下的共享文件时，写入前先 `lock`，写完后 `unlock`
- 需要查看共享文件历次修改记录时使用 `history`
- 协调冲突前用 `locks` 查看当前谁在持有什么

## 何时不使用

- 只读场景（无写冲突风险），不需要 lock
- 成员自己的 worktree 副本（`.agent_teams/worktrees/<slug>/`），由 worktree 隔离天然无冲突，不需要 lock
```

创建 `internal/agent_teams/tools/locales/descs/en/workspace_meta.md`（复刻 Python `openjiuwen/agent_teams/tools/locales/descs/en/workspace_meta.md`）：

```markdown
Metadata tool for the team shared workspace: **file lock management** and **git version history queries**. Use when multiple members collaborate on shared files.

## Scope

This tool does **not** perform file I/O. The shared workspace is exposed as the `.team/` mount point — use the standard `read_file` / `write_file` / `glob` tools against `.team/...` paths for reads and writes.

## action

- **lock** — acquire an exclusive file lock (requires `path`)
- **unlock** — release a file lock (requires `path`)
- **locks** — list all currently active (non-expired) locks
- **history** — query the file's git version history (requires `path`); returns the most recent 10 commits with hash, author, date, and message

## Lock Semantics

- **Exclusive**: only one holder per file at a time
- **Timeout**: default 300 seconds; other members may reclaim an expired lock
- **Re-entrant**: calling `lock` again from the same holder refreshes the timeout
- **Advisory**: `write_file` does **not** automatically check locks. Before writing to a shared file, members should explicitly `lock` → `write_file` → `unlock` to avoid overwriting each other
- **Acquire failure**: returns `Locked by {holder_name}` indicating the current holder

## When to Use

- Before writing to files under `.team/` in multi-member collaboration: `lock`, write, then `unlock`
- Use `history` to inspect revisions of a shared file
- Use `locks` to see who is currently holding what (useful for coordination)

## When NOT to Use

- Read-only access — no lock needed
- Files in a member's private worktree (`.agent_teams/worktrees/<slug>/`) — worktree isolation already prevents conflicts
```

- [ ] **Step 2: 创建 translator.go**

创建 `internal/agent_teams/tools/locales/translator.go`：

```go
package locales

import (
	"embed"
	"strings"
	"sync"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// descKey 工具描述 key 后缀
const descKey = "_desc"

// ──────────────────────────── 全局变量 ────────────────────────────

//go:embed descs/*.md
var descFS embed.FS

// descCache 工具描述缓存（lang+"/"+tool → content）
var descCache sync.Map

// ──────────────────────────── 导出函数 ────────────────────────────

// Translator 翻译器闭包。
// 对齐 Python: Translator = Callable[..., str]
//
// 调用方式:
//   t("workspace_meta")           → 加载 descs/{lang}/workspace_meta.md 作为 _desc
//   t("workspace_meta", "action") → 查 STRINGS["workspace_meta.action"]
type Translator func(tool string, key ...string) string

// MakeTranslator 创建绑定到指定语言的翻译器闭包。
// 对齐 Python: make_translator(lang)
func MakeTranslator(lang atschema.Language) Translator {
	return func(tool string, key ...string) string {
		// 无 key 或 key 为 "_desc" → 加载 Markdown 描述
		if len(key) == 0 || key[0] == descKey {
			return loadToolDesc(tool, string(lang))
		}
		// 其他 key → 查 STRINGS 映射
		dictKey := tool + "." + key[0]
		table, ok := atschema.STRINGS[lang]
		if !ok {
			table = atschema.STRINGS[atschema.LanguageCN]
		}
		if val, ok := table[dictKey]; ok {
			return val
		}
		// 回退到默认语言
		if table = atschema.STRINGS[atschema.LanguageCN]; table != nil {
			if val, ok := table[dictKey]; ok {
				return val
			}
		}
		// key 缺失：返回 key 本身
		return dictKey
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// loadToolDesc 从嵌入的 Markdown 文件中加载工具描述。
// 对齐 Python: _load_desc(tool, lang)
// 文件路径格式：descs/{lang}/{tool}.md
func loadToolDesc(tool, lang string) string {
	cacheKey := lang + "/" + tool
	if cached, ok := descCache.Load(cacheKey); ok {
		return cached.(string)
	}
	// 尝试读取 {lang}/{tool}.md
	path := "descs/" + lang + "/" + tool + ".md"
	data, err := descFS.ReadFile(path)
	if err != nil {
		// 回退到中文
		if lang != string(atschema.LanguageCN) {
			path = "descs/" + string(atschema.LanguageCN) + "/" + tool + ".md"
			data, err = descFS.ReadFile(path)
		}
		if err != nil {
			descCache.Store(cacheKey, "")
			return ""
		}
	}
	content := strings.TrimSpace(string(data))
	descCache.Store(cacheKey, content)
	return content
}
```

- [ ] **Step 3: 创建 translator_test.go**

创建 `internal/agent_teams/tools/locales/translator_test.go`：

```go
package locales

import (
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// TestMakeTranslator_中文 测试中文翻译器加载 Markdown 描述
func TestMakeTranslator_中文(t *testing.T) {
	tFunc := MakeTranslator(atschema.LanguageCN)
	desc := tFunc("workspace_meta")
	if desc == "" {
		t.Error("workspace_meta 中文描述不应为空")
	}
	if !containsSubstring(desc, "文件锁管理") {
		t.Error("workspace_meta 中文描述应包含'文件锁管理'")
	}
}

// TestMakeTranslator_英文 测试英文翻译器加载 Markdown 描述
func TestMakeTranslator_英文(t *testing.T) {
	tFunc := MakeTranslator(atschema.LanguageEN)
	desc := tFunc("workspace_meta")
	if desc == "" {
		t.Error("workspace_meta 英文描述不应为空")
	}
	if !containsSubstring(desc, "file lock management") {
		t.Error("workspace_meta 英文描述应包含'file lock management'")
	}
}

// TestMakeTranslator_STRINGS参数 测试从 STRINGS 映射查询参数描述
func TestMakeTranslator_STRINGS参数(t *testing.T) {
	tFunc := MakeTranslator(atschema.LanguageCN)
	actionDesc := tFunc("workspace_meta", "action")
	if actionDesc == "" {
		t.Error("workspace_meta.action 不应为空")
	}
	if !containsSubstring(actionDesc, "lock") {
		t.Error("workspace_meta.action 应包含'lock'")
	}
}

// TestMakeTranslator_Desc缺失 测试不存在的工具返回空字符串
func TestMakeTranslator_Desc缺失(t *testing.T) {
	tFunc := MakeTranslator(atschema.LanguageCN)
	desc := tFunc("nonexistent_tool")
	if desc != "" {
		t.Errorf("不存在的工具描述应为空字符串，实际: %s", desc)
	}
}

// TestMakeTranslator_Key缺失 测试不存在的 key 返回 key 本身
func TestMakeTranslator_Key缺失(t *testing.T) {
	tFunc := MakeTranslator(atschema.LanguageCN)
	result := tFunc("workspace_meta", "nonexistent_key")
	if result != "workspace_meta.nonexistent_key" {
		t.Errorf("缺失 key 应返回 'workspace_meta.nonexistent_key'，实际: %s", result)
	}
}

// containsSubstring 检查字符串是否包含子串
func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		(len(s) > 0 && len(sub) > 0 && findSubstring(s, sub)))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 创建 locales/doc.go**

创建 `internal/agent_teams/tools/locales/doc.go`：

```go
// Package locales 提供团队工具的多语言支持。
//
// 基于 Translator 闭包 + Markdown 描述文件 + STRINGS 映射表三层体系：
//   - Translator 闭包：绑定到特定语言，提供 tool 级别的 i18n 查询
//   - Markdown 文件：长文本工具描述（_desc），嵌入二进制
//   - STRINGS 映射表：短文本参数描述，定义在 schema/i18n.go
//
// 文件目录：
//
//	locales/
//	├── doc.go              # 包文档
//	├── translator.go       # Translator 类型 + MakeTranslator + loadToolDesc
//	└── descs/
//	    ├── cn/
//	    │   └── workspace_meta.md  # workspace_meta 工具中文描述
//	    └── en/
//	        └── workspace_meta.md  # workspace_meta 工具英文描述
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/locales/
package locales
```

- [ ] **Step 5: 补充 schema/i18n.go 的 STRINGS key**

在 `internal/agent_teams/schema/i18n.go` 的 `STRINGS` CN 和 EN 映射中各添加 workspace_meta 参数描述。

CN 部分末尾（`"hitt.msg_received_for_human":` 之后）添加：
```go
		// ===== workspace_meta =====
		"workspace_meta.action": "操作类型：lock（获取文件锁）、unlock（释放文件锁）、locks（列出所有活跃锁）、history（查看文件版本历史）",
		"workspace_meta.path":   "目标文件的相对路径（lock/unlock/history 时必填）",
```

EN 部分末尾（`"hitt.msg_received_for_human":` 之后）添加：
```go
		// ===== workspace_meta =====
		"workspace_meta.action": "Operation type: lock (acquire file lock), unlock (release file lock), locks (list active locks), history (view file version history)",
		"workspace_meta.path":   "Relative path of the target file (required for lock/unlock/history)",
```

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/tools/locales/... ./internal/agent_teams/schema/... -v -count=1`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agent_teams/tools/locales/ internal/agent_teams/schema/i18n.go
git commit -m "feat(locales): 添加 i18n locales 基础设施 + workspace_meta 描述

- Translator 闭包类型 + MakeTranslator + loadToolDesc
- go:embed 嵌入 Markdown 描述文件（CN/EN）
- STRINGS 补充 workspace_meta.action/path key
- 5 个单元测试"
```

---

### Task 2: TeamTool 基类

**Files:**
- Create: `internal/agent_teams/tools/team_tool.go`
- Create: `internal/agent_teams/tools/team_tool_test.go`
- Modify: `internal/agent_teams/tools/doc.go`

- [ ] **Step 1: 创建 team_tool.go**

创建 `internal/agent_teams/tools/team_tool.go`：

```go
package tools

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamTool 团队工具基类。
// 对齐 Python: TeamTool(Tool, ABC)
// 子类嵌入 TeamTool 获得 Card() 默认实现，只需实现 Invoke()。
type TeamTool struct {
	// card 工具配置卡片
	card *tool.ToolCard
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamTool 创建 TeamTool 实例。
func NewTeamTool(card *tool.ToolCard) TeamTool {
	return TeamTool{card: card}
}

// Card 返回工具配置卡片。
func (t *TeamTool) Card() *tool.ToolCard {
	return t.card
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 创建 team_tool_test.go**

创建 `internal/agent_teams/tools/team_tool_test.go`：

```go
package tools

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/schema"
)

// TestNewTeamTool 测试 TeamTool 构造
func TestNewTeamTool(t *testing.T) {
	card := tool.NewToolCard("test.tool", "测试工具", nil)
	tt := NewTeamTool(card)
	if tt.Card() != card {
		t.Error("Card() 应返回构造时传入的 ToolCard")
	}
	if tt.Card().ID() != "test.tool" {
		t.Errorf("Card().ID() 期望 test.tool，实际 %s", tt.Card().ID())
	}
}

// TestTeamTool_Card_Nil 测试 Card 为 nil 时返回 nil
func TestTeamTool_Card_Nil(t *testing.T) {
	tt := TeamTool{}
	if tt.Card() != nil {
		t.Error("未初始化的 TeamTool Card() 应返回 nil")
	}
}
```

- [ ] **Step 3: 更新 tools/doc.go**

在 `internal/agent_teams/tools/doc.go` 的文件目录树中添加 `team_tool.go` 条目。

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/tools/... -run TestTeamTool -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/tools/team_tool.go internal/agent_teams/tools/team_tool_test.go internal/agent_teams/tools/doc.go
git commit -m "feat(tools): 添加 TeamTool 基类

- TeamTool 结构体 + Card() 默认实现
- 2 个单元测试"
```

---

### Task 3: TeamWorkspaceManager

**Files:**
- Modify: `internal/agent_teams/team_workspace/doc.go`
- Modify: `internal/agent_teams/team_workspace/models.go` (补充 HistoryEntry)
- Create: `internal/agent_teams/team_workspace/manager.go`
- Create: `internal/agent_teams/team_workspace/manager_test.go`

这是最大的任务。manager.go 约 400 行代码，manager_test.go 约 500 行。

- [ ] **Step 1: 补充 models.go 中的 HistoryEntry**

在 `internal/agent_teams/team_workspace/models.go` 的结构体区块中，`WorkspaceFileLock` 之后添加：

```go
// HistoryEntry 版本历史条目。
// 对齐 Python: get_history() 返回的 dict
type HistoryEntry struct {
	// Commit 提交哈希
	Commit string `json:"commit"`
	// Author 作者
	Author string `json:"author"`
	// Date 日期
	Date string `json:"date"`
	// Message 提交消息
	Message string `json:"message"`
}
```

- [ ] **Step 2: 创建 manager.go**

创建 `internal/agent_teams/team_workspace/manager.go`，包含：
- `PublishEventFunc` 类型定义
- `ManagerOption` / `WithPublishEvent` 函数式选项
- `TeamWorkspaceManager` 结构体
- `NewTeamWorkspaceManager` 构造函数
- `Initialize` — 目录创建 + git init
- `MountIntoWorkspace` / `MountWorktree` / `UnmountWorktree` / `MountIntoWorktree` — symlink 操作
- `AutoCommit` / `GetHistory` — git 版本控制
- `GetLock` / `AcquireLock` / `ReleaseLock` / `ListLocks` — 文件锁
- `Pull` / `Push` / `RemoteAcquireLock` / `RemoteReleaseLock` / `HandleLockRequest` / `HandleLockResponse` — DISTRIBUTED 占位
- `Config` / `WorkspacePath` / `TeamName` / `Mode` — 访问器
- 非导出：`runGit` / `revParse` / `mountDirectory` / `prepareMountPath` / `mergeExistingMountContents` / `backupExistingMountPath` / `isMountedToWorkspace` / `maybePull` / `resolveWorkspaceRelative`
- `ErrDistributedNotImplemented` 错误变量

对齐 Python `openjiuwen/agent_teams/team_workspace/manager.py`，逐方法对照实现。注意：
- Go 用 `sync.Mutex` 替代 `asyncio.Lock`
- git 辅助用 `os/exec.CommandContext` + 30s 超时
- symlink 用 `os.Symlink`
- Windows junction 延后（标注 `// TODO: Windows junction fallback`）
- 日志用 `logger.Info(ComponentChannel)` 等组件级日志
- 所有注释用中文
- 声明顺序：结构体→枚举→常量→全局变量→导出函数→非导出函数

- [ ] **Step 3: 更新 doc.go**

更新 `internal/agent_teams/team_workspace/doc.go`，文件目录改为：

```
// 文件目录：
//
//	team_workspace/
//	├── doc.go           # 包文档
//	├── models.go        # 核心数据结构与枚举定义
//	├── manager.go       # 团队工作空间管理器
//	├── rail.go          # TeamWorkspaceRail（DeepAgentRail 拦截器）
//	└── tool.go          # WorkspaceMetaTool（锁管理+版本历史工具）
```

- [ ] **Step 4: 创建 manager_test.go**

创建 `internal/agent_teams/team_workspace/manager_test.go`，覆盖设计文档中的 25 个测试用例。使用 `t.TempDir()` 创建临时目录，真实执行 git 命令。所有测试函数名使用中文场景描述。

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/team_workspace/... -v -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agent_teams/team_workspace/
git commit -m "feat(team_workspace): 实现 TeamWorkspaceManager (LOCAL 模式)

- 目录初始化 + git init + 空 commit
- symlink 挂载（MountIntoWorkspace/MountWorktree/MountIntoWorktree）
- 文件锁（AcquireLock/ReleaseLock/ListLocks/GetLock）+ sync.Mutex
- git 版本控制（AutoCommit/GetHistory）
- 事件发布回调（PublishEventFunc）
- DISTRIBUTED 占位方法（ErrDistributedNotImplemented）
- HistoryEntry 结构体
- 25 个单元测试"
```

---

### Task 4: TeamWorkspaceRail

**Files:**
- Create: `internal/agent_teams/team_workspace/rail.go`
- Create: `internal/agent_teams/team_workspace/rail_test.go`

- [ ] **Step 1: 创建 rail.go**

创建 `internal/agent_teams/team_workspace/rail.go`，包含：
- `teamPrefix` / `pullIntervalDefault` 常量
- `writeTools` / `readTools` 变量
- `TeamWorkspaceRail` 结构体（嵌入 `rails.DeepAgentRail`）
- `NewTeamWorkspaceRail` 构造函数
- `Init` — 调用 `CwdState.SetTeamWorkspace`
- `BeforeToolCall` — 读→maybePull，写→锁检查（设 Extra 标记，不阻断）
- `AfterToolCall` — 写→autoCommit + publishEvent
- 非导出：`maybePull` / `resolveWorkspaceRelative`

对齐 Python `openjiuwen/agent_teams/team_workspace/rails.py`。

- [ ] **Step 2: 创建 rail_test.go**

创建 `internal/agent_teams/team_workspace/rail_test.go`，覆盖 10 个测试用例。需要构造 mock AgentCallbackContext（用 `NewAgentCallbackContext` + 设置 `Inputs` 为 `ToolCallInputs`）。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/team_workspace/... -run TestTeamWorkspaceRail -v -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/team_workspace/rail.go internal/agent_teams/team_workspace/rail_test.go
git commit -m "feat(team_workspace): 实现 TeamWorkspaceRail

- 嵌入 DeepAgentRail，拦截 .team/ 路径读写
- BeforeToolCall：读操作放行，写操作锁检查（Extra 标记不阻断）
- AfterToolCall：写操作 auto_commit + publish_event
- Init：设置 CwdState.SetTeamWorkspace
- 10 个单元测试"
```

---

### Task 5: WorkspaceMetaTool

**Files:**
- Create: `internal/agent_teams/team_workspace/tool.go`
- Create: `internal/agent_teams/team_workspace/tool_test.go`

- [ ] **Step 1: 创建 tool.go**

创建 `internal/agent_teams/team_workspace/tool.go`，包含：
- `WorkspaceMetaTool` 结构体（嵌入 `TeamTool`）
- `NewWorkspaceMetaTool` 构造函数（使用 `locales.MakeTranslator` 加载描述和参数 schema）
- `Invoke` 方法（4 个 action: lock/unlock/locks/history）

对齐 Python `openjiuwen/agent_teams/team_workspace/tools.py`。

- [ ] **Step 2: 创建 tool_test.go**

创建 `internal/agent_teams/team_workspace/tool_test.go`，覆盖 8 个测试用例。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/team_workspace/... -run TestWorkspaceMetaTool -v -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/team_workspace/tool.go internal/agent_teams/team_workspace/tool_test.go
git commit -m "feat(team_workspace): 实现 WorkspaceMetaTool

- lock/unlock/locks/history 4 个 action
- 使用 locales.MakeTranslator 加载 i18n 描述
- 8 个单元测试"
```

---

### Task 6: 回填 agent/infra.go + agent_configurator.go

**Files:**
- Modify: `internal/agent_teams/agent/infra.go`
- Modify: `internal/agent_teams/agent/agent_configurator.go`

- [ ] **Step 1: 修改 infra.go**

1. 添加 import `"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"`
2. 将 `WorkspaceManager any` 改为 `WorkspaceManager *team_workspace.TeamWorkspaceManager`
3. 删除 `// TODO(#9.66): TeamWorkspaceManager 类型` 注释

- [ ] **Step 2: 修改 agent_configurator.go**

1. 添加 import `"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"`
2. `WorkspaceManager()` 返回类型 `any` → `*team_workspace.TeamWorkspaceManager`
3. `SetWorkspaceManager(v any)` → `SetWorkspaceManager(v *team_workspace.TeamWorkspaceManager)`
4. `CreateWorkspaceManager` 实现：构造 `NewTeamWorkspaceManager` + 调用 `Initialize`，返回 `*team_workspace.TeamWorkspaceManager`
5. `SetupInfra` 步骤 6：替换 `TODO(#9.66)` 为实际调用
6. `SetupAgent` 步骤 3：替换 `TODO(#9.66)` 为 mount 调用
7. `SetupAgent` 步骤 5：替换 `TODO(#9.66)` 为 worktree mount 调用
8. `SetupTeamBackend` 步骤 9：替换 `TODO(#9.66)` 为 `tb.RegisterCleanupPath(ws.WorkspacePath())`
9. `CreateWorktreeManager` 中的 `TODO(#9.66)` 不在本次范围（属于 worktree 管理），保留不动

- [ ] **Step 3: 运行编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 4: 运行现有测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/... -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/infra.go internal/agent_teams/agent/agent_configurator.go
git commit -m "refactor(agent): 回填 TeamWorkspaceManager 类型，清零 TODO(#9.66)

- infra.go: WorkspaceManager any → *TeamWorkspaceManager
- agent_configurator.go: CreateWorkspaceManager 实现
- SetupInfra/SetupAgent/SetupTeamBackend 中的 TODO(#9.66) 替换为实际调用"
```

---

### Task 7: 回填 harness.go

**Files:**
- Modify: `internal/agent_teams/harness.go`

- [ ] **Step 1: 修改 MountedRails.TeamWorkspace 类型**

1. 添加 import `"github.com/uapclaw/uapclaw-go/internal/agent_teams/team_workspace"`
2. `MountedRails.TeamWorkspace any` → `TeamWorkspace *team_workspace.TeamWorkspaceRail`
3. `TODO(#9.66+#9.68)` → `TODO(#9.68)`
4. `BuildTeamHarness` 参数 `teamWorkspaceRail any` → `teamWorkspaceRail *team_workspace.TeamWorkspaceRail`
5. `BuildTeamAgent` 中的 `TODO(#9.66+#9.68)` → `TODO(#9.68)`

- [ ] **Step 2: 运行编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/... -v -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/harness.go
git commit -m "refactor(harness): MountedRails.TeamWorkspace 类型 any → *TeamWorkspaceRail

- TODO 标记从 #9.66+#9.68 改为 #9.68
- Rail 实例化和 AddRail 归 9.68"
```

---

### Task 8: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.66 状态**

将 `| 9.66 | ☐ | Team Workspace |` 改为 `| 9.66 | ✅ | Team Workspace |` 并补充实现摘要（TeamWorkspaceManager+TeamWorkspaceRail+WorkspaceMetaTool+TeamTool基类+i18n locales+回填）。

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 9.66 状态为 ✅"
```
