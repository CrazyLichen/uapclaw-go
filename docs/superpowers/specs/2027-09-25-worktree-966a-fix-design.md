# 9.66a WorktreeManager 实现偏差修复设计

## 概述

9.66a WorktreeManager 初始实现后，对照计划和 Python 源码审查发现 4 个结构性偏差需要修复。本设计定义每个偏差的修复方案，确保 Go 实现对齐 Python 语义且类型安全。

**原则**：fireRail 定义但未调用——与 Python 端状态一致，本次只修签名类型安全，不改变"未接线"状态。

## 修复项

### 1. WorktreeRail.Init/Uninit 补全 resourceMgr 注册

**偏差**：`WorktreeRail.Init()` 只注册了 `AbilityManager`，缺少 `resourceMgr.AddTool()`。Python 的 `WorktreeRail.init()` 做了两步：
1. `Runner.resource_mgr.add_tool(self._tools)` — 注册到资源管理器
2. `for tool in self._tools: agent.ability_manager.add(tool.card)` — 注册到能力管理器

**修复**：对齐 Go 已有的 `SysOperationRail.Init()` 模式：

```go
// Init 中添加（在 AbilityManager 注册之后）：
resourceMgr := runner.GetResourceMgr()
if resourceMgr != nil {
    for _, t := range r.tools {
        _ = resourceMgr.AddTool(t)
    }
}

// Uninit 中添加（在 AbilityManager 清理之前）：
resourceMgr := runner.GetResourceMgr()
if resourceMgr != nil {
    for _, t := range r.tools {
        toolID := t.Card().ID
        if toolID != "" {
            _, _ = resourceMgr.RemoveTool([]string{toolID})
        }
    }
}
```

**新增 import**：`"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"`

### 2. fireRail 类型安全化

**偏差**：`fireRail(method string, args ...any) any` 使用 string 方法名 + any 参数，放弃了编译期类型安全。调度逻辑内用 type assertion（`args[0].(string)`），参数数量/类型错误只能在运行时发现。

**修复**：删除 `fireRail(method string, args ...any) any`，替换为 8 个类型安全的独立方法：

```go
// fireBeforeCreate 调用 BeforeWorktreeCreate hook。
// 返回最后一个非空 slug 修改，nil 表示不干预。
func (m *WorktreeManager) fireBeforeCreate(ctx context.Context, slug, repoRoot string) (string, error) {
    var lastResult string
    for _, rail := range m.lifecycleRails {
        r, err := rail.BeforeWorktreeCreate(ctx, slug, repoRoot)
        if err != nil {
            logger.Warn(logComponent).Err(err).Msg("fireBeforeCreate hook 失败")
            continue
        }
        if r != "" {
            lastResult = r
        }
    }
    if lastResult != "" {
        return lastResult, nil
    }
    return "", nil
}

// fireAfterCreate 调用 AfterWorktreeCreate hook。
func (m *WorktreeManager) fireAfterCreate(ctx context.Context, session *WorktreeSession) {
    for _, rail := range m.lifecycleRails {
        if err := rail.AfterWorktreeCreate(ctx, session); err != nil {
            logger.Warn(logComponent).Err(err).Msg("fireAfterCreate hook 失败")
        }
    }
}

// fireBeforeExit 调用 BeforeWorktreeExit hook。
// 返回最后一个非空 action 修改，nil 表示不干预。
func (m *WorktreeManager) fireBeforeExit(ctx context.Context, session *WorktreeSession, action string) (string, error) {
    var lastResult string
    for _, rail := range m.lifecycleRails {
        r, err := rail.BeforeWorktreeExit(ctx, session, action)
        if err != nil {
            logger.Warn(logComponent).Err(err).Msg("fireBeforeExit hook 失败")
            continue
        }
        if r != "" {
            lastResult = r
        }
    }
    if lastResult != "" {
        return lastResult, nil
    }
    return "", nil
}

// fireAfterExit 调用 AfterWorktreeExit hook。
func (m *WorktreeManager) fireAfterExit(ctx context.Context, session *WorktreeSession, action string) {
    for _, rail := range m.lifecycleRails {
        if err := rail.AfterWorktreeExit(ctx, session, action); err != nil {
            logger.Warn(logComponent).Err(err).Msg("fireAfterExit hook 失败")
        }
    }
}

// fireOnFileWrite 调用 OnWorktreeFileWrite hook。
func (m *WorktreeManager) fireOnFileWrite(ctx context.Context, session *WorktreeSession, filePath string) error {
    for _, rail := range m.lifecycleRails {
        if err := rail.OnWorktreeFileWrite(ctx, session, filePath); err != nil {
            return err
        }
    }
    return nil
}

// fireBeforeCommit 调用 BeforeWorktreeCommit hook。
// 返回最后一个非空 message 修改，nil 表示不干预。
func (m *WorktreeManager) fireBeforeCommit(ctx context.Context, session *WorktreeSession, message string) (string, error) {
    var lastResult string
    for _, rail := range m.lifecycleRails {
        r, err := rail.BeforeWorktreeCommit(ctx, session, message)
        if err != nil {
            logger.Warn(logComponent).Err(err).Msg("fireBeforeCommit hook 失败")
            continue
        }
        if r != "" {
            lastResult = r
        }
    }
    if lastResult != "" {
        return lastResult, nil
    }
    return "", nil
}

// fireAfterCommit 调用 AfterWorktreeCommit hook。
func (m *WorktreeManager) fireAfterCommit(ctx context.Context, session *WorktreeSession, commitHash string) {
    for _, rail := range m.lifecycleRails {
        if err := rail.AfterWorktreeCommit(ctx, session, commitHash); err != nil {
            logger.Warn(logComponent).Err(err).Msg("fireAfterCommit hook 失败")
        }
    }
}

// fireOnSync 调用 OnWorktreeSync hook。
func (m *WorktreeManager) fireOnSync(ctx context.Context, session *WorktreeSession) error {
    for _, rail := range m.lifecycleRails {
        if err := rail.OnWorktreeSync(ctx, session); err != nil {
            return err
        }
    }
    return nil
}
```

**状态说明**：这 8 个方法定义后**不立即在 Enter/Exit 中调用**。与 Python 端等效——Python 的 `_fire_rail` 也定义了但未被 `enter()/exit()` 调用。未来接线时直接在对应位置调用类型安全的方法即可。

### 3. Git 辅助函数改为非导出

**偏差**：16 个 git 辅助函数全部导出（大写），违反封装原则。所有调用方都在 worktree 包内，无跨包访问需求。

**修复**：全部改为小写：

| 原名 | 改为 |
|------|------|
| `FindGitRoot` | `findGitRoot` |
| `FindCanonicalGitRoot` | `findCanonicalGitRoot` |
| `GetCurrentBranch` | `getCurrentBranch` |
| `GetDefaultBranch` | `getDefaultBranch` |
| `RevParse` | `revParse` |
| `WorktreeAdd` | `worktreeAdd` |
| `WorktreeRemove` | `worktreeRemove` |
| `WorktreePrune` | `worktreePrune` |
| `BranchDelete` | `branchDelete` |
| `FetchRef` | `fetchRef` |
| `SparseCheckoutSet` | `sparseCheckoutSet` |
| `StatusPorcelain` | `statusPorcelain` |
| `CountCommitsSince` | `countCommitsSince` |
| `HasUnpushedCommits` | `hasUnpushedCommits` |
| `ReadWorktreeHeadSHA` | `readWorktreeHeadSHA` |
| `runGit` | `runGit`（已是非导出，不变） |

**同步修改**：
- `git_test.go`：同包测试，直接调用小写函数，无需改动调用方式
- `backend.go`：`GitBackend.Create`/`Remove`/`Exists`/`resolveBase` 中的调用全部改为小写
- `manager.go`：`Enter`/`Exit`/`CountChanges`/`CleanupWorktreesByPrefix`/`RecoverWorktreeForOwner`/`checkChanges`/`configureHooksPath`/`copyIncludeFiles`/`removeWorktreeInternal` 中的调用全部改为小写
- `cleanup.go`：`CleanupStaleWorktrees` 中如有调用改为小写
- `tools.go`：`slugExists` 中的调用改为小写

### 4. 导出 PublishEvent 并补全事件桥接

**偏差**：`TeamBackend.publishEvent` 是 unexported 方法，`agent/agent_configurator.go` 无法调用。schema 层的 `WorktreeCreatedEvent`/`WorktreeRemovedEvent` 事件类型已存在但未被使用。

**修复**：

4a. 将 `TeamBackend.publishEvent` 改为 `PublishEvent`（首字母大写）：

```go
// team_backend.go
func (tb *TeamBackend) PublishEvent(ctx context.Context, event events.TypedEvent) {
    // 内容不变
}
```

4b. 同步修改 `team_backend.go` 内部所有 `publishEvent` 调用为 `PublishEvent`（如 `spawnAndPublish` 等）。

4c. `agent_configurator.go` 补全事件桥接：

```go
eventHandler = func(ctx context.Context, event worktree.WorktreeEvent) error {
    switch e := event.(type) {
    case *worktree.WorktreeCreatedEvent:
        _ = wsMgr.MountWorktree(e.WorktreeName, e.WorktreePath)
        tb.PublishEvent(ctx, atevents.WorktreeCreatedEvent{
            BaseEventMessage: atevents.BaseEventMessage{TeamName: c.TeamName()},
            WorktreeName:     e.WorktreeName,
            WorktreePath:     e.WorktreePath,
            Existed:          e.Existed,
        })
    case *worktree.WorktreeRemovedEvent:
        _ = wsMgr.UnmountWorktree(e.WorktreeName)
        tb.PublishEvent(ctx, atevents.WorktreeRemovedEvent{
            BaseEventMessage: atevents.BaseEventMessage{TeamName: c.TeamName()},
            WorktreeName:     e.WorktreeName,
            WorktreePath:     e.WorktreePath,
        })
    }
    return nil
}
```

4d. 事件处理器的条件守卫恢复为同时检查 WorkspaceManager 和 TeamBackend：

```go
if c.WorkspaceManager() != nil && c.TeamBackend() != nil {
    // 创建 eventHandler
}
```

### 5. 小修补

- `doc.go`：更新 rails.go 描述为 `WorktreeRail + WorktreeLifecycleRail + AutoSetupRail + DiffSummaryRail`（补回 WorktreeLifecycleRail）
- `cleanup.go`：如引用了导出的 git 函数，同步改为小写

## 不修复项

| 偏差 | 原因 |
|------|------|
| Hook 签名缺 cbc 参数 | Python 端也未使用 cbc 特有数据，ctx 是正确的简化 |
| resolveWorkspaceWorktreePath 缺失 | 功能等价分散在 resolveTargetPath + slugExists 中 |
| fireRail 未接线到 Enter/Exit | Python 端也未接线，两边等效的"定义但未使用"状态 |
| 缺少 //go:build integration 测试 | 非本次修复范围，后续单独补 |
