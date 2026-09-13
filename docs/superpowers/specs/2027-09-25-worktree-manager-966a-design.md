# 9.66a WorktreeManager 设计文档

## 概述

完整移植 Python `openjiuwen/harness/tools/worktree/` 包，实现 Git Worktree 隔离系统。WorktreeManager 是 Git Worktree 生命周期的单一业务逻辑入口，提供每个 Agent 独立的工作副本隔离、session 状态管理、事件分发和 lifecycle hook 体系。

**实现范围**：完整移植 Python 12 个文件，包括 Manager、GitBackend、git 命令封装、Enter/ExitWorktreeTool、WorktreeRail、WorktreeLifecycleRail（AutoSetupRail/DiffSummaryRail）、session 状态、slug 校验、cleanup、notice。DISTRIBUTED 模式的 `RemoteWorktreeBackend` 延后（占位方法签名保留）。

## 在 Agent 会话中的流程位置

```
AgentConfigurator.SetupInfra(spec, ctx)
  ├── 1. 解析 team_home / worktree 路径
  ├── 2. 创建 Messager
  ├── 3. 创建 TeamBackend
  ├── 4. 创建 WorkspaceManager ← 9.66 (已完成)
  ├── 5. WorkspaceManager.MountIntoWorkspace()
  ├── 6. 模型分配
  ├── 7. 创建 WorktreeManager ← 9.66a
  │     └── NewWorktreeManager(WorktreeConfig, GitBackend, eventHandler=镜像回调)
  ├── 8. MountWorktree → .worktree/{slug} symlink
  └── 9. 注册 cleanup path

AgentConfigurator.SetupAgent(spec, ctx)
  ├── init_cwd(WithTeamWorkspace(path))
  ├── 构建 DeepAgent
  │     └── AddRail(WorktreeRail) ← 9.66a
  └── ...

DeepAgent 运行时:
  ├── agent.invoke() 前后:
  │     ├── WorktreeRail.BeforeInvoke: 从 Session.state 恢复 → SetCurrentSession + SetCwd
  │     └── WorktreeRail.AfterInvoke:  将 GetCurrentSession → 持久化到 Session.state
  │
  └── 每次 tool_call:
        ├── EnterWorktreeTool.Invoke()
        │     ├── manager.Enter(ctx, slug) → GitBackend.Create() → 4阶段创建
        │     ├── CwdState.SetCwd(worktreePath) + SetOriginalCwd
        │     └── 事件 → WorkspaceManager.MountWorktree()
        │
        └── ExitWorktreeTool.Invoke()
              ├── CountChanges(ctx, session) → 变更检测（fail-closed）
              ├── manager.Exit(ctx, action) → GitBackend.Remove() 或保留
              ├── CwdState.SetCwd(originalCwd) + SetOriginalCwd
              └── 事件 → WorkspaceManager.UnmountWorktree()
```

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 实现范围 | 完整移植 12 个 Python 文件 | 用户明确要求 |
| 前置章节 | 无，直接开始 | 现有基础设施够用 |
| WorktreeTool 基类 | worktreeToolBase（包内不导出） | 1:1 对齐 Python `_WorktreeToolBase`，语义正确，不污染公共 API |
| 异步模型 | 同步签名 + context.Context | 与 CwdState/SessionState/TeamWorkspaceManager 一致，Go 惯用法 |
| ContextVar 对齐 | *WorktreeSessionState 指针放 context.Value | 与 CwdState/SessionState 同模式，同 Agent 内 goroutine 共享引用 |
| 包位置 | 全部放 agentcore/harness/tools/worktree/ | 对齐 Python |
| WorktreeRail | 嵌入 DeepAgentRail，覆写 BeforeInvoke/AfterInvoke/Init/Uninit | Go 端 Rail 体系完全支持 |
| LifecycleRail hooks | 接口 + 切片，Manager 直接调用 | 1:1 对齐 Python `_fire_rail()` |

## 包结构

```
agentcore/harness/tools/worktree/
├── doc.go              ← 更新：补充新文件列表
├── models.go           ← 已有（不变）
├── models_test.go      ← 已有（不变）
├── manager.go          ← 新增：WorktreeManager（~400行）
├── manager_test.go     ← 新增
├── git.go              ← 新增：Git 命令封装（~300行）
├── git_test.go         ← 新增
├── backend.go          ← 新增：WorktreeBackend 接口 + GitBackend + 注册表（~200行）
├── backend_test.go     ← 新增
├── session.go          ← 新增：WorktreeSessionState（~80行，对齐 CwdState 模式）
├── session_test.go     ← 新增
├── slug.go             ← 新增：validate_slug + worktree_branch_name（~60行）
├── slug_test.go        ← 新增
├── events.go           ← 新增：WorktreeCreatedEvent/RemovedEvent/WorktreeEventHandler（~50行）
├── rails.go            ← 新增：WorktreeRail + WorktreeLifecycleRail + AutoSetupRail + DiffSummaryRail（~350行）
├── rails_test.go       ← 新增
├── tools.go            ← 新增：worktreeToolBase + EnterWorktreeTool + ExitWorktreeTool（~250行）
├── tools_test.go       ← 新增
├── cleanup.go          ← 新增：CleanupStaleWorktrees（~90行）
├── cleanup_test.go     ← 新增
├── notice.go           ← 新增：BuildWorktreeNotice（~30行）
└── notice_test.go      ← 新增
```

## 1. WorktreeSessionState

对齐 Python `session.py` 的 `WorktreeSessionState` + Go 项目 CwdState/SessionState 模式。

```go
// WorktreeSessionState 每-Agent 的可变 worktree 会话状态容器。
// Python: WorktreeSessionState (session.py)
//
// 通过 context.Value 传播 *WorktreeSessionState 指针：
//   - 同一 Agent 内的 goroutine 共享同一引用，SetCurrentSession 后立即可见
//   - WorktreeRail.BeforeInvoke 从 Session.state 恢复，AfterInvoke 持久化
//
// 并发安全：所有字段读写通过 sync.RWMutex 保护。
type WorktreeSessionState struct {
    mu                  sync.RWMutex
    session             *WorktreeSession
    defaultWorktreeName string
}
```

函数签名对齐 CwdState 模式：

| 函数 | Python 对齐 | 说明 |
|------|------------|------|
| `InitWorktreeSessionState() *WorktreeSessionState` | `init_session_state()` | 创建空 holder |
| `WithWorktreeSessionState(ctx, state) context.Context` | `_state.set(s)` | 注入 context |
| `WorktreeSessionStateFromCtx(ctx) *WorktreeSessionState` | `_get_state()` | 从 context 取出 |
| `GetCurrentSession(ctx) *WorktreeSession` | `get_current_session()` | 读取当前会话 |
| `SetCurrentSession(ctx, session)` | `set_current_session(session)` | 设置/清空 |
| `GetDefaultWorktreeName(ctx) string` | `get_default_worktree_name()` | 读取默认名 |
| `SetDefaultWorktreeName(ctx, name)` | `set_default_worktree_name(name)` | 设置默认名 |
| `RequireCurrentSession(ctx) (*WorktreeSession, error)` | `require_current_session()` | 不存在返回 error |

## 2. WorktreeBackend

对齐 Python `backend.py`。

```go
// WorktreeBackend worktree 后端接口。
// Python: WorktreeBackend(Protocol)
type WorktreeBackend interface {
    Create(ctx context.Context, slug string, repoRoot string, targetPath string) (*WorktreeCreateResult, error)
    Remove(ctx context.Context, worktreePath string, repoRoot string) (bool, error)
    Exists(ctx context.Context, worktreePath string) (bool, error)
}
```

### GitBackend

四阶段创建流程（1:1 对齐 Python `GitBackend.create()`）：

1. **快速恢复**：`readWorktreeHeadSHA` 读 HEAD，已有 worktree 直接返回
2. **基准解析**：`_resolveBase` — 先尝试本地 `origin/<default>` 引用（省 6-8s），fetch 失败再回退 HEAD
3. **创建 worktree**：`worktreeAdd -B`
4. **稀疏检出**（可选）：`sparseCheckoutSet`，失败回滚

```go
type GitBackend struct {
    config WorktreeConfig
}
```

注册表：`registerWorktreeBackend(name, factory)` / `createBackend(name, config)`，默认注册 `"git": GitBackend`。

## 3. git.go

对齐 Python `git.py`，全部同步 + `context.Context`。使用 `os/exec.CommandContext` + 30s 超时。

核心类型：

```go
// GitError Git 命令执行错误。
type GitError struct {
    Command    string
    ReturnCode int
    Stderr     string
}

// GitResult Git 命令执行结果。
type GitResult struct {
    ReturnCode int
    Stdout     string
    Stderr     string
}
func (r GitResult) OK() bool { return r.ReturnCode == 0 }
```

函数清单：

| Go 函数 | Python 对齐 | 说明 |
|---------|------------|------|
| `runGit(ctx, args, cwd) GitResult` | `_run_git` | 核心执行器 |
| `findGitRoot(ctx, cwd) (string, error)` | `find_git_root` | |
| `findCanonicalGitRoot(ctx, cwd) (string, error)` | `find_canonical_git_root` | 从 worktree 找主仓库 |
| `getCurrentBranch(ctx, cwd) (string, error)` | `get_current_branch` | |
| `getDefaultBranch(ctx, cwd) (string, error)` | `get_default_branch` | |
| `revParse(ctx, ref, cwd) (string, error)` | `rev_parse` | |
| `worktreeAdd(ctx, repoRoot, wtPath, branch, baseRef string, noCheckout bool) error` | `worktree_add` | |
| `worktreeRemove(ctx, wtPath string, repoRoot string, force bool) (bool, error)` | `worktree_remove` | |
| `worktreePrune(ctx, repoRoot) error` | `worktree_prune` | |
| `branchDelete(ctx, branch, repoRoot) error` | `branch_delete` | |
| `fetchRef(ctx, repoRoot, ref string, remote ...string) (bool, error)` | `fetch_ref` | |
| `sparseCheckoutSet(ctx, wtPath string, paths []string) error` | `sparse_checkout_set` | |
| `statusPorcelain(ctx, cwd) ([]string, error)` | `status_porcelain` | |
| `countCommitsSince(ctx, baseCommit, cwd string) (*int, error)` | `count_commits_since` | nil 表示无法确定 |
| `hasUnpushedCommits(ctx, cwd) (*bool, error)` | `has_unpushed_commits` | |
| `readWorktreeHeadSHA(wtPath string) (string, error)` | `read_worktree_head_sha` | 快速路径，不调 git |

`readWorktreeHeadSHA` 是快速路径：直接读 `.git` 文件找 commondir → 读 HEAD → 读 ref，~0.5ms，不调 git 子进程。

## 4. WorktreeManager

对齐 Python `manager.py`（727行）。

```go
type WorktreeManager struct {
    config       WorktreeConfig
    backend      WorktreeBackend
    eventHandler WorktreeEventHandler   // func(ctx, WorktreeEvent) error
    rails        []WorktreeLifecycleRail
}
```

方法清单（全部同步签名 + context.Context）：

### 构造

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `NewWorktreeManager(config, backend, opts ...ManagerOption) *WorktreeManager` | `__init__` | opts 支持 WithEventHandler/WithLifecycleRails |
| `Backend() WorktreeBackend` | `backend` 属性 | 访问器 |

### Session 级（工具调用）

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `Enter(ctx, slug, memberName, teamName) (*WorktreeSession, error)` | `enter()` | 创建或恢复 worktree + 设置 ContextVar |
| `Exit(ctx, action string, discardChanges bool) (map[string]string, error)` | `exit()` | 退出 worktree（keep/remove）|

### Owner 级（spawn 逻辑）

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `CreateOwnerWorktree(ctx, slug) (*WorktreeCreateResult, error)` | `create_owner_worktree()` | 不改 ContextVar，不改 CWD |
| `CreateAgentWorktree(ctx, slug) (*WorktreeCreateResult, error)` | `create_agent_worktree` | 向后兼容别名 |

### 恢复

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `RecoverWorktreeForOwner(ctx, ownerID, tag string) (*WorktreeSession, error)` | `recover_worktree_for_owner()` | 恢复持久 owner 的 worktree |
| `RecoverWorktreeForMember(ctx, memberName, teamName string) (*WorktreeSession, error)` | `recover_worktree_for_member()` | 别名 |

### 变更检测

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `CountChanges(ctx, session *WorktreeSession) (*WorktreeChangeSummary, error)` | `count_changes()` | nil 表示无法确定（fail-closed）|

### 批量清理

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `CleanupWorktreesByPrefix(ctx, slugPrefix string, force bool) ([]string, error)` | `cleanup_worktrees_by_prefix()` | |
| `CleanupTeamWorktrees(ctx, teamName string, force bool) ([]string, error)` | `cleanup_team_worktrees()` | 别名 |
| `RemoveWorktree(ctx, worktreePath, repoRoot string) (bool, error)` | `remove_worktree()` | 单个删除 |

### 内部辅助

| 方法 | 说明 |
|------|------|
| `resolveTargetPath(ctx, slug) (string, error)` | Python `_resolve_target_path` |
| `ownerSlug(ownerID) string` | Python `_owner_slug` → `"teammate-{first8}"` |
| `resolvePolicy() WorktreeLifecyclePolicy` | Python `_resolve_policy` |
| `fireRail(method, args)` | Python `_fire_rail` — 遍历 lifecycleRails 直接调用 |
| `checkChanges(ctx, wtPath) (*WorktreeChangeSummary, error)` | Python `_check_changes` |
| `removeWorktreeInternal(ctx, wtPath, repoRoot) (bool, error)` | Python `_remove_worktree` |
| `postCreationSetup(ctx, repoRoot, worktreePath) error` | Python `_post_creation_setup` |
| `copyIncludeFiles(ctx, repoRoot, worktreePath, patterns) ([]string, error)` | Python `_copy_include_files` |
| `configureHooksPath(ctx, repoRoot, worktreePath) error` | Python `_configure_hooks_path` |

## 5. WorktreeLifecycleRail

对齐 Python `rails.py` 的 `WorktreeLifecycleRail`。接口 + 切片模式，Manager 直接调用（不走 AgentCallbackEvent 路由）。

```go
// WorktreeLifecycleRail worktree 生命周期 hook 基类。
// Python: WorktreeLifecycleRail (rails.py)
// 子类覆盖关心的 hook 方法，WorktreeManager.fireRail 直接调用。
type WorktreeLifecycleRail struct {
    rails.DeepAgentRail
}
```

Hook 方法（全部有默认 no-op 实现，子类按需覆盖）：

| 方法 | Python 对齐 | 返回值语义 |
|------|------------|-----------|
| `BeforeWorktreeCreate(ctx, cbc, slug, repoRoot) (string, error)` | `before_worktree_create` | 返回修改后的 slug，nil 不干预 |
| `AfterWorktreeCreate(ctx, cbc, session) error` | `after_worktree_create` | 创建后的 setup（如 AutoSetupRail） |
| `BeforeWorktreeExit(ctx, cbc, session, action) (string, error)` | `before_worktree_exit` | 返回修改后的 action，nil 不干预 |
| `AfterWorktreeExit(ctx, cbc, session, action) error` | `after_worktree_exit` | 退出后通知 |
| `OnWorktreeFileWrite(ctx, cbc, session, filePath) (bool, error)` | `on_worktree_file_write` | true 允许，false 阻止 |
| `BeforeWorktreeCommit(ctx, cbc, session, message, files) (string, error)` | `before_worktree_commit` | 返回修改后的 message |
| `AfterWorktreeCommit(ctx, cbc, session, commitSHA) error` | `after_worktree_commit` | 提交后通知 |
| `OnWorktreeSync(ctx, cbc, session, direction, files) ([]string, error)` | `on_worktree_sync` | 返回过滤后的文件列表 |

### AutoSetupRail

对齐 Python `AutoSetupRail`。自动检测项目类型（pyproject.toml → `uv sync --quiet`，package.json → `npm install --silent`），在 `AfterWorktreeCreate` 中运行 setup 命令。

### DiffSummaryRail

对齐 Python `DiffSummaryRail`。在 `BeforeWorktreeExit` 中，当 action="keep" 时记录 `git diff --stat`。

## 6. WorktreeRail

对齐 Python `rails.py` 的 `WorktreeRail`（注入 rail）。

```go
type WorktreeRail struct {
    rails.DeepAgentRail
    userConfig     WorktreeConfig
    eventHandler   WorktreeEventHandler
    lifecycleRails []WorktreeLifecycleRail
    manager        *WorktreeManager    // Init 时创建
    tools          []tool.Tool         // Init 时创建
}
```

覆写方法：

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `Priority() int` | `priority = 100` | 与 SysOperationRail 同级 |
| `Init(ctx, agent) error` | `init(agent)` | 创建 Manager + InitWorktreeSessionState + 注册工具 |
| `Uninit(agent) error` | `uninit(agent)` | 移除工具 + 清空 Manager |
| `BeforeInvoke(ctx, cbc) error` | `before_invoke(ctx)` | 从 Session.state 恢复 → SetCurrentSession + SetCwd + SetOriginalCwd |
| `AfterInvoke(ctx, cbc) error` | `after_invoke(ctx)` | 将 GetCurrentSession → 持久化到 Session.state |

Session state 持久化 key：

| Go 常量 | Python | 说明 |
|---------|--------|------|
| `sessionStateKey = "_worktree_session"` | `_SESSION_STATE_KEY` | WorktreeSession JSON |
| `defaultWorktreeNameKey = "_worktree_default_name"` | `_DEFAULT_WORKTREE_NAME_KEY` | 默认 worktree 名 |

`BeforeInvoke` 逻辑：
1. 从 `cbc.Session().GetState(defaultWorktreeNameKey)` 恢复 defaultName
2. 从 `cbc.Session().GetState(sessionStateKey)` 恢复 session（dict → `WorktreeSession` 反序列化）
3. `SetCurrentSession(ctx, session)`
4. `CwdStateFromCtx(ctx).SetCwd(session.WorktreePath)` + `SetOriginalCwd`

`AfterInvoke` 逻辑：
1. `GetCurrentSession(ctx)` → 序列化为 map[string]any
2. `cbc.Session().UpdateState({sessionStateKey: payload, defaultWorktreeNameKey: defaultName})`

## 7. tools.go

### worktreeToolBase

包内私有基类，对齐 Python `_WorktreeToolBase(Tool)`。

```go
// worktreeToolBase 包内私有工具基类。
// Python: _WorktreeToolBase(Tool)
type worktreeToolBase struct {
    card    *tool.ToolCard
    manager *WorktreeManager
}

func (t *worktreeToolBase) Card() *tool.ToolCard { return t.card }
func (t *worktreeToolBase) Stream(ctx context.Context, inputs map[string]any, opts ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
    return nil, tool.ErrStreamNotSupported
}
```

### EnterWorktreeTool

对齐 Python `EnterWorktreeTool`。

```go
type EnterWorktreeTool struct {
    worktreeToolBase
    language string
    agentID  string
}
```

`Invoke` 逻辑：
1. `GetCurrentSession(ctx)` — 已在 worktree → 返回 `{"error": "Already in worktree..."}`
2. `_resolveSlug(ctx, inputs)` — 无 name → 生成随机 `<adj>-<noun>-<4hex>`，validate
3. `_resolveOwner(opts)` → ownerID, tag（从 ToolCallOptions 的 kwargs 中提取）
4. `manager.Enter(ctx, slug, ownerID, tag)`
5. `CwdStateFromCtx(ctx).SetCwd(session.WorktreePath)` + `SetOriginalCwd`
6. 返回 `{"worktree_path": ..., "worktree_branch": ..., "message": ...}`

### ExitWorktreeTool

对齐 Python `ExitWorktreeTool`。

```go
type ExitWorktreeTool struct {
    worktreeToolBase
    language string
    agentID  string
}
```

`Invoke` 逻辑：
1. `GetCurrentSession(ctx)` — 不在 worktree → 返回 `{"error": "No active worktree session"}`
2. 校验 action ∈ {"keep", "remove"}
3. action="remove" + !discardChanges → `CountChanges` → 有变更 → 返回错误信息
4. `manager.Exit(ctx, action, discardChanges)`
5. `CwdStateFromCtx(ctx).SetCwd(originalCwd)` + `SetOriginalCwd`
6. 返回 `{"action": ..., "worktree_name": ..., "message": ...}`

### 辅助函数

| 函数 | Python 对齐 | 说明 |
|------|------------|------|
| `generateRandomSlug() string` | `_generate_random_slug()` | `<adj>-<noun>-<4hex>` |
| `resolveOwner(opts) (string, string)` | `_resolve_owner(kwargs)` | owner_id/tag 或 member_name/team_name |
| `resolveWorkspaceWorktreePath(ctx, slug) (string, error)` | `_resolve_workspace_worktree_path(slug)` | 基于 workspace 计算路径 |

## 8. events.go

harness 层的 worktree 事件类型。与 agent_teams 层的 `schema/events.WorktreeCreatedEvent` 是**两套独立体系**，通过 `AgentConfigurator.createWorktreeManager()` 中的 event_handler 回调桥接。

```go
// WorktreeEventHandler worktree 生命周期事件回调。
// Python: WorktreeEventHandler = Callable[[WorktreeEvent], Awaitable[None]]
type WorktreeEventHandler func(ctx context.Context, event WorktreeEvent) error

// WorktreeEvent worktree 事件联合类型。
// Python: WorktreeEvent = Union[WorktreeCreatedEvent, WorktreeRemovedEvent]
type WorktreeEvent interface {
    isWorktreeEvent()
}

type WorktreeCreatedEvent struct {
    WorktreeName string `json:"worktree_name"`
    WorktreePath string `json:"worktree_path"`
    OwnerID      string `json:"owner_id,omitempty"`
    Tag          string `json:"tag,omitempty"`
    Existed      bool   `json:"existed"`
}

type WorktreeRemovedEvent struct {
    WorktreeName string `json:"worktree_name"`
    WorktreePath string `json:"worktree_path"`
    OwnerID      string `json:"owner_id,omitempty"`
    Tag          string `json:"tag,omitempty"`
}
```

**桥接逻辑**（在 `agent/agent_configurator.go` 的 `CreateWorktreeManager` 中）：

```go
eventHandler := func(ctx context.Context, event worktree.WorktreeEvent) error {
    switch e := event.(type) {
    case *worktree.WorktreeCreatedEvent:
        // 转调 TeamBackend.PublishEvent 发布 schema 层事件
        // 同时调用 wsMgr.MountWorktree(e.WorktreeName, e.WorktreePath)
    case *worktree.WorktreeRemovedEvent:
        // 转调 TeamBackend.PublishEvent
        // 同时调用 wsMgr.UnmountWorktree(e.WorktreeName)
    }
    return nil
}
```

## 9. slug.go

对齐 Python `slug.py`。

| 函数 | Python 对齐 | 说明 |
|------|------------|------|
| `ValidateSlug(slug string) error` | `validate_slug(slug)` | 路径遍历防护、64字符限制、段内只允许 `alphanumeric/.-_` |
| `WorktreeBranchName(slug string) string` | `worktree_branch_name(slug)` | `"feature-auth"` → `"worktree-feature-auth"`，`/` → `+` |
| `WorktreePathFor(baseDir, slug string) string` | `worktree_path_for(base_dir, slug)` | `{baseDir}/.worktrees/{slug}` |
| `WorktreesDir(baseDir string) string` | `worktrees_dir(base_dir)` | `{baseDir}/.worktrees` |

## 10. cleanup.go

对齐 Python `cleanup.py`。Fail-closed 安全策略。

```go
// IsEphemeralSlug 检查 slug 是否匹配临时模式。
// Python: is_ephemeral_slug(slug)
func IsEphemeralSlug(slug string) bool

// CleanupStaleWorktrees 清理过期的临时 worktree。
// Python: cleanup_stale_worktrees(config, backend, current_worktree_path)
func CleanupStaleWorktrees(ctx context.Context, config WorktreeConfig, backend WorktreeBackend, currentWorktreePath string) (int, error)
```

安全策略：
1. 只清理匹配 ephemeral pattern 的 slug
2. 跳过当前 session 的 worktree
3. 检查未提交变更（`statusPorcelain`）
4. 检查未推送提交（`hasUnpushedCommits`）
5. 任何检查失败 → 跳过

## 11. notice.go

对齐 Python `notice.py`。

```go
// BuildWorktreeNotice 构建 worktree 上下文提示。
// Python: build_worktree_notice(parent_cwd, worktree_cwd)
func BuildWorktreeNotice(parentCwd, worktreeCwd string) string
```

## 12. 回填点清单

| 文件 | 当前 | 修改 |
|------|------|------|
| `agent/resources.go:20` | `WorktreeManager any` + `TODO(#9.66a)` | 改为 `*worktree.WorktreeManager` |
| `agent/agent_configurator.go:427-428` | `CreateWorktreeManager` 空方法体 | 实现：构造 `NewWorktreeManager` + event_handler 镜像回调 |
| `agent/agent_configurator.go:215-217` | `SetupInfra` 步骤 9 调用 | 调用 `c.SetWorktreeManager(c.CreateWorktreeManager(spec, ctx))` |
| `agent/agent_configurator.go:622` | `WorktreeManager() any` | 返回 `*worktree.WorktreeManager` |
| `agent/agent_configurator.go:626` | `SetWorktreeManager(v any)` | 参数改为 `*worktree.WorktreeManager` |
| `swarm/server/adapter/code_adapter.go` | `buildWorktreeRail()` 返回 nil | 构造 `WorktreeRail` 实例 |

## 13. Go-Python 适配对照

| Go 约束 | Python 行为 | 适配策略 |
|---------|------------|---------|
| `sync.RWMutex` | `asyncio.Lock` / GIL | 所有 mutable 状态加锁 |
| `os/exec.CommandContext` | `asyncio.create_subprocess_exec` | 同步 + 30s 超时 + ctx 取消 |
| `context.Value` 存指针 | `ContextVar` 存 mutable holder | 同 CwdState/SessionState 模式 |
| `Tool.Invoke() → (map[string]any, error)` | `Tool.invoke() → ToolOutput(success, error, data)` | 成功返回 `{"worktree_path": ...}`，失败返回 `{"error": "..."}` |
| `WorktreeEventHandler func(ctx, event) error` | `WorktreeEventHandler = Callable[[WorktreeEvent], Awaitable[None]]` | 同步签名 |
| `exception.BuildError(StatusCode, ...)` | `raise_error(StatusCode, reason=...)` | 在 exception/codes.go 补充 `StatusToolWorktreeExitInvalid` |
| `SessionFacade.UpdateState(map[string]any)` | `ctx.session.update_state(dict)` | 通过 SessionFacade 接口方法 |
| `SessionFacade.GetState(StateKey) (any, error)` | `ctx.session.get_state(key)` | 反序列化 dict → WorktreeSession |

## 14. 测试计划

### session_test.go
- `TestInitWorktreeSessionState` — 构造
- `TestWithWorktreeSessionState` — context 注入和取出
- `TestSetCurrentSession` — 设置后读取
- `TestRequireCurrentSession_存在` — 正常返回
- `TestRequireCurrentSession_不存在` — 返回 error
- `TestGetSetDefaultWorktreeName` — 默认名读写

### git_test.go
- `TestRunGit_正常命令` — `git --version`
- `TestRunGit_错误命令` — 非零返回码
- `TestFindCanonicalGitRoot` — 真实 git 仓库
- `TestReadWorktreeHeadSHA` — 快速路径验证
- `TestStatusPorcelain` — 在临时 git 仓库中验证
- `TestGetCurrentBranch` — 分支名读取
- `TestWorktreeAddRemove` — 完整创建删除流程（`//go:build integration`）

### backend_test.go
- `TestGitBackend_Create_新建` — 4阶段创建
- `TestGitBackend_Create_恢复已有` — Phase 1 快速恢复
- `TestGitBackend_Remove` — 删除 + 分支清理
- `TestGitBackend_Exists` — 存在/不存在
- `TestCreateBackend_默认Git` — 注册表默认
- `TestCreateBackend_未知名称` — 返回 error

### manager_test.go
- `TestNewWorktreeManager` — 构造
- `TestEnter_创建Worktree` — 完整 enter 流程
- `TestEnter_恢复已有` — existed=true
- `TestExit_Keep` — 保留
- `TestExit_Remove无变更` — 删除
- `TestExit_Remove有变更拒绝` — fail-closed
- `TestExit_Remove有变更确认` — discardChanges=true
- `TestCreateOwnerWorktree` — 不改 ContextVar
- `TestCountChanges` — 有/无变更
- `TestRecoverWorktreeForOwner` — 恢复
- `TestCleanupWorktreesByPrefix` — 批量清理
- `TestPostCreationSetup_Symlink` — symlink 目录
- `TestPostCreationSetup_IncludeFiles` — 拷贝 gitignored 文件
- `TestPostCreationSetup_HooksPath` — hooks 路径配置

### slug_test.go
- `TestValidateSlug_合法` — 各种合法 slug
- `TestValidateSlug_路径遍历` — `..` 拒绝
- `TestValidateSlug_超长` — 65+ 字符拒绝
- `TestValidateSlug_非法字符` — 特殊字符拒绝
- `TestWorktreeBranchName` — `/` → `+` 转换
- `TestWorktreePathFor` — 路径计算

### tools_test.go
- `TestEnterWorktreeTool_Invoke_成功` — 正常进入
- `TestEnterWorktreeTool_Invoke_已在Worktree` — 返回错误
- `TestEnterWorktreeTool_Invoke_无Name生成随机` — 自动 slug
- `TestEnterWorktreeTool_Invoke_非Git仓库` — 返回错误
- `TestExitWorktreeTool_Invoke_Keep` — 保留
- `TestExitWorktreeTool_Invoke_Remove` — 删除
- `TestExitWorktreeTool_Invoke_无Session` — 返回错误
- `TestExitWorktreeTool_Invoke_无效Action` — 返回错误
- `TestGenerateRandomSlug` — 格式验证

### rails_test.go
- `TestWorktreeRail_Init` — 工具注册
- `TestWorktreeRail_Uninit` — 工具清理
- `TestWorktreeRail_BeforeInvoke_恢复Session` — Session.state → ContextVar + CWD
- `TestWorktreeRail_BeforeInvoke_无Session` — 清空 ContextVar
- `TestWorktreeRail_AfterInvoke_持久化Session` — ContextVar → Session.state
- `TestWorktreeRail_AfterInvoke_退出后清空` — nil session 持久化
- `TestAutoSetupRail_Python` — pyproject.toml 检测
- `TestAutoSetupRail_Node` — package.json 检测
- `TestDiffSummaryRail_Keep` — action=keep 时记录 diff
- `TestDiffSummaryRail_Remove` — action=remove 时跳过

### cleanup_test.go
- `TestIsEphemeralSlug` — 匹配/不匹配
- `TestCleanupStaleWorktrees_清理过期` — 删除过期 ephemeral
- `TestCleanupStaleWorktrees_跳过当前` — currentWorktreePath
- `TestCleanupStaleWorktrees_跳过有变更` — fail-closed

### notice_test.go
- `TestBuildWorktreeNotice` — 输出格式验证
