# 9.66a WorktreeManager 实现偏差修复计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 9.66a WorktreeManager 实现中的 4 个结构性偏差：补全 resourceMgr 注册、fireRail 类型安全化、Git 函数非导出化、PublishEvent 导出及事件桥接。

**Architecture:** 按依赖顺序修复——先改 git.go 非导出（其他文件依赖），再改 manager.go fireRail（依赖 git 小写），再改 rails.go resourceMgr 注册，最后改 team_backend.go PublishEvent 导出 + agent_configurator.go 事件桥接。

**Tech Stack:** Go 1.22+, golint

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/agentcore/harness/tools/worktree/git.go` | 16 个函数改为非导出 |
| 修改 | `internal/agentcore/harness/tools/worktree/git_test.go` | 测试调用同步改为小写 |
| 修改 | `internal/agentcore/harness/tools/worktree/backend.go` | 调用同步改为小写 |
| 修改 | `internal/agentcore/harness/tools/worktree/manager.go` | fireRail 拆为 8 个 typed 方法 + 调用同步改为小写 |
| 修改 | `internal/agentcore/harness/tools/worktree/manager_test.go` | 如有 fireRail 引用同步修改 |
| 修改 | `internal/agentcore/harness/tools/worktree/cleanup.go` | 调用同步改为小写 |
| 修改 | `internal/agentcore/harness/tools/worktree/rails.go` | Init/Uninit 补全 resourceMgr |
| 修改 | `internal/agentcore/harness/tools/worktree/doc.go` | rails.go 描述补回 WorktreeLifecycleRail |
| 修改 | `internal/agent_teams/tools/team_backend.go` | publishEvent → PublishEvent |
| 修改 | `internal/agent_teams/tools/team_backend_test.go` | 如有 publishEvent 引用同步修改 |
| 修改 | `internal/agent_teams/agent/agent_configurator.go` | 补全 PublishEvent 事件桥接 |

---

### Task 1: Git 函数非导出化

**Files:**
- Modify: `internal/agentcore/harness/tools/worktree/git.go`
- Modify: `internal/agentcore/harness/tools/worktree/git_test.go`
- Modify: `internal/agentcore/harness/tools/worktree/backend.go`
- Modify: `internal/agentcore/harness/tools/worktree/manager.go`
- Modify: `internal/agentcore/harness/tools/worktree/cleanup.go`

- [ ] **Step 1: 修改 git.go — 15 个导出函数改为非导出**

对以下函数执行首字母小写重命名：

```
FindGitRoot → findGitRoot
FindCanonicalGitRoot → findCanonicalGitRoot
GetCurrentBranch → getCurrentBranch
GetDefaultBranch → getDefaultBranch
RevParse → revParse
WorktreeAdd → worktreeAdd
WorktreeRemove → worktreeRemove
WorktreePrune → worktreePrune
BranchDelete → branchDelete
FetchRef → fetchRef
SparseCheckoutSet → sparseCheckoutSet
StatusPorcelain → statusPorcelain
CountCommitsSince → countCommitsSince
HasUnpushedCommits → hasUnpushedCommits
ReadWorktreeHeadSHA → readWorktreeHeadSHA
```

同时修改所有函数的中文注释，将导出注释格式改为非导出格式。

注意：`GitError`、`GitResult` 保持导出（类型在 `backend_test.go` 等处可能被间接引用）；`runGit` 已是非导出不变。

- [ ] **Step 2: 同步修改 git_test.go 中的调用**

将测试文件中对大写函数名的调用全部改为小写。例如：
- `FindGitRoot(` → `findGitRoot(`
- `FindCanonicalGitRoot(` → `findCanonicalGitRoot(`
- `GetCurrentBranch(` → `getCurrentBranch(`
- `ReadWorktreeHeadSHA(` → `readWorktreeHeadSHA(`
- `StatusPorcelain(` → `statusPorcelain(`

- [ ] **Step 3: 同步修改 backend.go 中的调用**

```go
// 原调用 → 新调用
ReadWorktreeHeadSHA( → readWorktreeHeadSHA(
WorktreeAdd( → worktreeAdd(
SparseCheckoutSet( → sparseCheckoutSet(
WorktreeRemove( → worktreeRemove(
RevParse( → revParse(
GetCurrentBranch( → getCurrentBranch(
BranchDelete( → branchDelete(
GetDefaultBranch( → getDefaultBranch(
FetchRef( → fetchRef(
```

- [ ] **Step 4: 同步修改 manager.go 中的调用**

```go
// 原调用 → 新调用
FindCanonicalGitRoot( → findCanonicalGitRoot(
GetCurrentBranch( → getCurrentBranch(
ReadWorktreeHeadSHA( → readWorktreeHeadSHA(
StatusPorcelain( → statusPorcelain(
CountCommitsSince( → countCommitsSince(
WorktreePrune( → worktreePrune(
```

- [ ] **Step 5: 同步修改 cleanup.go 中的调用**

```go
// 原调用 → 新调用
FindCanonicalGitRoot( → findCanonicalGitRoot(
StatusPorcelain( → statusPorcelain(
HasUnpushedCommits( → hasUnpushedCommits(
WorktreePrune( → worktreePrune(
```

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/harness/tools/worktree/...`
Expected: 编译成功

- [ ] **Step 7: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/tools/worktree/... -count=1`
Expected: PASS

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/git.go internal/agentcore/harness/tools/worktree/git_test.go internal/agentcore/harness/tools/worktree/backend.go internal/agentcore/harness/tools/worktree/manager.go internal/agentcore/harness/tools/worktree/cleanup.go
git commit -m "refactor(worktree): git 辅助函数改为非导出

- 15 个 git 函数全部改为小写，保持封装
- 同步修改 backend.go/manager.go/cleanup.go/git_test.go 中的调用
- GitError/GitResult 类型保持导出"
```

---

### Task 2: fireRail 类型安全化

**Files:**
- Modify: `internal/agentcore/harness/tools/worktree/manager.go`

- [ ] **Step 1: 删除旧的 fireRail 方法**

删除 `manager.go` 中的：
```go
func (m *WorktreeManager) fireRail(method string, args ...any) any {
    // ... 整个方法体
}
```

- [ ] **Step 2: 添加 8 个类型安全的 fire 方法**

在 `manager.go` 的非导出函数区块中添加：

```go
// fireBeforeCreate 调用 BeforeWorktreeCreate hook。
// 返回最后一个非空 slug 修改，空字符串表示不干预。
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
    return lastResult, nil
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
// 返回最后一个非空 action 修改，空字符串表示不干预。
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
    return lastResult, nil
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
// 返回最后一个非空 message 修改，空字符串表示不干预。
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
    return lastResult, nil
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

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/harness/tools/worktree/...`
Expected: 编译成功

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/tools/worktree/... -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/manager.go
git commit -m "refactor(worktree): fireRail 类型安全化

- 删除 fireRail(method string, args ...any) any
- 替换为 8 个 typed dispatch 方法：fireBeforeCreate/fireAfterCreate/fireBeforeExit/fireAfterExit/fireOnFileWrite/fireBeforeCommit/fireAfterCommit/fireOnSync
- 消除 string method 参数和 any 类型断言
- 与 Python 等效：定义但不调用"
```

---

### Task 3: WorktreeRail.Init/Uninit 补全 resourceMgr 注册

**Files:**
- Modify: `internal/agentcore/harness/tools/worktree/rails.go`

- [ ] **Step 1: 在 Init 方法中补全 resourceMgr.AddTool**

在 `rails.go` 的 `Init` 方法中，在 `AbilityManager` 注册代码块之后添加 resourceMgr 注册：

```go
// Python: Runner.resource_mgr.add_tool(self._tools)
resourceMgr := runner.GetResourceMgr()
if resourceMgr != nil {
    for _, t := range r.tools {
        _ = resourceMgr.AddTool(t)
    }
}
```

- [ ] **Step 2: 在 Uninit 方法中补全 resourceMgr.RemoveTool**

在 `rails.go` 的 `Uninit` 方法中，在 `AbilityManager` 清理代码块之前添加 resourceMgr 清理：

```go
// Python: Runner.resource_mgr.remove_tool(tool_id)
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

- [ ] **Step 3: 添加 import**

在 `rails.go` 的 import 块中添加：

```go
"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
```

注意：需检查是否与已有的 `rails` 包 alias 冲突（`runner` 不是 `rails`，不会冲突）。

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/harness/tools/worktree/...`
Expected: 编译成功

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/tools/worktree/... -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/rails.go
git commit -m "fix(worktree): WorktreeRail.Init/Uninit 补全 resourceMgr 注册

- Init 中添加 runner.GetResourceMgr().AddTool(t)，对齐 Python Runner.resource_mgr.add_tool
- Uninit 中添加 runner.GetResourceMgr().RemoveTool(toolID)，对齐 Python Runner.resource_mgr.remove_tool
- 与 AbilityManager 注册形成完整双注册模式，对齐 SysOperationRail"
```

---

### Task 4: 导出 PublishEvent + 补全事件桥接

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go`
- Modify: `internal/agent_teams/tools/team_backend_test.go`（如有 publishEvent 引用）
- Modify: `internal/agent_teams/agent/agent_configurator.go`

- [ ] **Step 1: 将 team_backend.go 中的 publishEvent 改为 PublishEvent**

1. 方法签名：`func (tb *TeamBackend) publishEvent(` → `func (tb *TeamBackend) PublishEvent(`
2. 方法注释：更新为导出格式
3. 所有内部调用 `tb.publishEvent(` → `tb.PublishEvent(`

涉及行（搜索 `tb.publishEvent(`）：
- MemberShutdownEvent 发布
- MemberCanceledEvent 发布
- TeamCreatedEvent 发布
- TeamCleanedEvent 发布
- TaskCancelledEvent 发布
- TaskUnblockedEvent 发布
- TaskPlanResponseEvent 发布
- ToolApprovalResultEvent 发布
- MemberSpawnedEvent 发布

- [ ] **Step 2: 同步修改 team_backend_test.go**

如果测试文件中有 `publishEvent` 引用，同步改为 `PublishEvent`。搜索确认。

- [ ] **Step 3: 编译验证 team_backend 改动**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/tools/...`
Expected: 编译成功

- [ ] **Step 4: 修改 agent_configurator.go 补全事件桥接**

1. 修改事件处理器条件守卫，从 `if c.WorkspaceManager() != nil` 改为 `if c.WorkspaceManager() != nil && c.TeamBackend() != nil`
2. 在事件处理器闭包中添加 `tb` 变量引用
3. 替换 `// TODO(#9.58)` 注释为实际的 `tb.PublishEvent` 调用
4. 添加 `atevents` import（如尚未导入）

事件处理器完整代码：

```go
var eventHandler worktree.WorktreeEventHandler
if c.WorkspaceManager() != nil && c.TeamBackend() != nil {
    wsMgr := c.WorkspaceManager()
    tb := c.TeamBackend()
    eventHandler = func(ctx context.Context, event worktree.WorktreeEvent) error {
        switch e := event.(type) {
        case *worktree.WorktreeCreatedEvent:
            // 挂载 worktree symlink
            _ = wsMgr.MountWorktree(e.WorktreeName, e.WorktreePath)
            // 发布 schema 层事件
            tb.PublishEvent(ctx, atevents.WorktreeCreatedEvent{
                BaseEventMessage: atevents.BaseEventMessage{TeamName: c.TeamName()},
                WorktreeName:     e.WorktreeName,
                WorktreePath:     e.WorktreePath,
                Existed:          e.Existed,
            })
        case *worktree.WorktreeRemovedEvent:
            // 卸载 worktree symlink
            _ = wsMgr.UnmountWorktree(e.WorktreeName)
            // 发布 schema 层事件
            tb.PublishEvent(ctx, atevents.WorktreeRemovedEvent{
                BaseEventMessage: atevents.BaseEventMessage{TeamName: c.TeamName()},
                WorktreeName:     e.WorktreeName,
                WorktreePath:     e.WorktreePath,
            })
        }
        return nil
    }
}
```

确认 import 中包含：
```go
atevents "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
```

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/... -count=1 -timeout 120s`
Expected: PASS

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/tools/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agent_teams/tools/team_backend.go internal/agent_teams/tools/team_backend_test.go internal/agent_teams/agent/agent_configurator.go
git commit -m "feat(agent): 导出 PublishEvent 并补全 worktree 事件桥接

- TeamBackend.publishEvent → PublishEvent（导出）
- agent_configurator.go 补全 tb.PublishEvent 调用
- WorktreeCreatedEvent/WorktreeRemovedEvent schema 层事件发布
- 事件处理器条件守卫恢复双检查（WorkspaceManager + TeamBackend）"
```

---

### Task 5: 更新 doc.go + 最终验证

**Files:**
- Modify: `internal/agentcore/harness/tools/worktree/doc.go`

- [ ] **Step 1: 修改 doc.go 中 rails.go 的描述**

将：
```
└── rails.go         # WorktreeRail + AutoSetupRail + DiffSummaryRail
```
改为：
```
└── rails.go         # WorktreeRail + WorktreeLifecycleRail + AutoSetupRail + DiffSummaryRail
```

- [ ] **Step 2: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/harness/tools/worktree/... && go build ./internal/agent_teams/... && go build ./internal/swarm/...`
Expected: 全部编译成功

- [ ] **Step 3: 全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/tools/worktree/... -count=1`
Expected: PASS

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/... -count=1 -timeout 120s`
Expected: PASS

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/tools/... -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/tools/worktree/doc.go
git commit -m "docs(worktree): doc.go 补回 WorktreeLifecycleRail 描述"
```
