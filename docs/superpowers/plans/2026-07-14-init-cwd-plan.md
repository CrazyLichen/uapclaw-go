# init_cwd 子系统实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Go 版 CwdState + context.Value 传播，对齐 Python ContextVar 的三层 CWD 模型

**Architecture:** 新增 `sys_operation/cwd` 包定义 CwdState 可变容器 + context 传播函数；在 DeepAgent.ensureInitialized 中调用 InitCwd 注入 CwdState 到 ctx；修复 10 个 context 断裂点确保 CWD 一路透传到工具层；删除 WithShellCwd/WithCodeCwd Option，统一从 ctx 获取

**Tech Stack:** Go 1.22+, context.Context, sync.RWMutex

**设计文档:** `docs/superpowers/specs/2026-07-14-init-cwd-design.md`

---

## 文件结构

### 新增
- `internal/agentcore/sys_operation/cwd/doc.go` — 包文档
- `internal/agentcore/sys_operation/cwd/cwd.go` — CwdState 结构体 + 全部方法 + context 传播函数
- `internal/agentcore/sys_operation/cwd/cwd_test.go` — 单元测试

### 修改
- `internal/agentcore/sys_operation/sys_operation.go` — 删除 ShellOptions.Cwd, CodeOptions.Cwd, WithShellCwd, WithCodeCwd
- `internal/agentcore/sys_operation/sys_operation_test.go` — 删除对应测试
- `internal/agentcore/sys_operation/doc.go` — 更新文件目录
- `internal/agentcore/harness/deep_agent.go` — 核心变更：ensureInitialized 返回 ctx + initCwd 实现 + 断裂点修复
- `internal/agentcore/harness/deep_agent_test.go` — 更新测试
- `internal/agentcore/harness/interfaces/deep_agent.go` — 接口签名变更
- `internal/agentcore/harness/task_loop/handler.go` — completeSessionSpawn 增加 ctx
- `internal/agentcore/single_agent/interfaces/callback.go` — Fire() 增加 ctx
- `internal/agentcore/single_agent/agents/react_invoke.go` — ClearContextMessages 增加 ctx
- `internal/agentcore/single_agent/agents/react_helpers.go` — getTools/saveContexts 增加 ctx
- `internal/agentcore/harness/factory.go` — CreateDeepAgent CWD 注入
- `internal/agentcore/harness/rails/*_test.go` — fake 接口签名更新
- `internal/agentcore/harness/task_loop/executor_test.go` — fake 接口签名更新

---

### Task 1: 创建 cwd 包 — CwdState 核心数据结构

**Files:**
- Create: `internal/agentcore/sys_operation/cwd/doc.go`
- Create: `internal/agentcore/sys_operation/cwd/cwd.go`
- Test: `internal/agentcore/sys_operation/cwd/cwd_test.go`

- [ ] **Step 1: 编写 CwdState 核心测试**

创建 `internal/agentcore/sys_operation/cwd/cwd_test.go`，包含以下测试：

```go
package cwd

import (
    "context"
    "os"
    "path/filepath"
    "sync"
    "testing"

    "github.com/stretchr/testify/assert"
)

// TestInitCwd_基本初始化 测试 InitCwd 创建 CwdState
func TestInitCwd_基本初始化(t *testing.T) {
    state := InitCwd("/project", WithWorkspace("/project/ws"))
    assert.Equal(t, "/project", state.GetCwd())
    assert.Equal(t, "/project", state.GetOriginalCwd())
    assert.Equal(t, "/project", state.GetProjectRoot())
    assert.Equal(t, "/project/ws", state.GetWorkspace())
}

// TestInitCwd_自定义ProjectRoot 测试显式设置 project_root
func TestInitCwd_自定义ProjectRoot(t *testing.T) {
    state := InitCwd("/workspace", WithProjectRoot("/project"))
    assert.Equal(t, "/workspace", state.GetCwd())
    assert.Equal(t, "/project", state.GetProjectRoot())
}

// TestCwdState_SetCwd 测试运行时修改 CWD
func TestCwdState_SetCwd(t *testing.T) {
    state := InitCwd("/project")
    state.SetCwd("/project/worktree")
    assert.Equal(t, "/project/worktree", state.GetCwd())
    // originalCwd 和 projectRoot 不受影响
    assert.Equal(t, "/project", state.GetOriginalCwd())
    assert.Equal(t, "/project", state.GetProjectRoot())
}

// TestCwdState_读取优先级 测试 cwd -> originalCwd -> os.Getwd() 优先级
func TestCwdState_读取优先级(t *testing.T) {
    state := InitCwd("/project")
    // 正常：返回 cwd
    assert.Equal(t, "/project", state.GetCwd())

    // 清空 cwd：回退到 originalCwd
    state.mu.Lock()
    state.cwd = ""
    state.mu.Unlock()
    assert.Equal(t, "/project", state.GetOriginalCwd())

    // 清空 originalCwd：回退到 os.Getwd()
    state.mu.Lock()
    state.originalCwd = ""
    state.mu.Unlock()
    wd, _ := os.Getwd()
    assert.Equal(t, wd, state.GetCwd())
}

// TestCwdState_并发安全 测试并发读写不 panic
func TestCwdState_并发安全(t *testing.T) {
    state := InitCwd("/project")
    var wg sync.WaitGroup
    for i := 0; i < 100; i++ {
        wg.Add(2)
        go func() {
            defer wg.Done()
            _ = state.GetCwd()
        }()
        go func(i int) {
            defer wg.Done()
            state.SetCwd(filepath.Join("/tmp", "dir"))
        }(i)
    }
    wg.Wait()
}

// TestWithCwdState_上下文传播 测试 CwdState 通过 context 传播
func TestWithCwdState_上下文传播(t *testing.T) {
    state := InitCwd("/project")
    ctx := WithCwdState(context.Background(), state)
    got := CwdStateFromCtx(ctx)
    assert.Equal(t, state, got)
}

// TestWithCwdState_上下文无CwdState 测试无 CwdState 时返回 nil
func TestWithCwdState_上下文无CwdState(t *testing.T) {
    got := CwdStateFromCtx(context.Background())
    assert.Nil(t, got)
}

// TestGetCwd_从上下文获取 测试 GetCwd(ctx) 全局函数
func TestGetCwd_从上下文获取(t *testing.T) {
    state := InitCwd("/project")
    ctx := WithCwdState(context.Background(), state)
    assert.Equal(t, "/project", GetCwd(ctx))
}

// TestGetCwd_上下文无CwdState回退 测试 ctx 无 CwdState 时回退到 os.Getwd()
func TestGetCwd_上下文无CwdState回退(t *testing.T) {
    cwd := GetCwd(context.Background())
    wd, _ := os.Getwd()
    assert.Equal(t, wd, cwd)
}

// TestResolveCwd_显式绝对路径 测试显式绝对路径直接使用
func TestResolveCwd_显式绝对路径(t *testing.T) {
    state := InitCwd("/project")
    ctx := WithCwdState(context.Background(), state)
    assert.Equal(t, "/tmp", ResolveCwd(ctx, "/tmp"))
}

// TestResolveCwd_显式相对路径 测试相对路径基于 GetCwd 解析
func TestResolveCwd_显式相对路径(t *testing.T) {
    state := InitCwd("/project")
    ctx := WithCwdState(context.Background(), state)
    assert.Equal(t, filepath.Join("/project", "subdir"), ResolveCwd(ctx, "subdir"))
}

// TestResolveCwd_空值回退 测试空值使用 GetCwd
func TestResolveCwd_空值回退(t *testing.T) {
    state := InitCwd("/project")
    ctx := WithCwdState(context.Background(), state)
    assert.Equal(t, "/project", ResolveCwd(ctx, ""))
}

// TestResolvePath_相对路径 测试相对路径基于 GetCwd 解析
func TestResolvePath_相对路径(t *testing.T) {
    state := InitCwd("/project")
    ctx := WithCwdState(context.Background(), state)
    assert.Equal(t, filepath.Join("/project", "src/main.go"), ResolvePath(ctx, "src/main.go"))
}

// TestResolvePath_绝对路径 测试绝对路径直接使用
func TestResolvePath_绝对路径(t *testing.T) {
    state := InitCwd("/project")
    ctx := WithCwdState(context.Background(), state)
    assert.Equal(t, "/tmp/file.go", ResolvePath(ctx, "/tmp/file.go"))
}

// Test子Agent隔离 测试子 Agent 创建独立 CwdState 不影响父
func Test子Agent隔离(t *testing.T) {
    parentState := InitCwd("/project", WithWorkspace("/project"))
    parentCtx := WithCwdState(context.Background(), parentState)

    // 子 Agent 创建独立 CwdState
    subState := InitCwd("/project/.sub/xxx", WithWorkspace("/project/.sub/xxx"))
    subCtx := WithCwdState(parentCtx, subState)

    // 子 Agent 修改 CWD
    subState.SetCwd("/project/.sub/xxx/worktree")

    // 父 Agent 不受影响
    assert.Equal(t, "/project", GetCwd(parentCtx))
    // 子 Agent 看到新 CWD
    assert.Equal(t, "/project/.sub/xxx/worktree", GetCwd(subCtx))
}

// Test父改子可见 测试父修改 CWD 后同一 ctx 下的子 goroutine 可见
func Test父改子可见(t *testing.T) {
    state := InitCwd("/project")
    ctx := WithCwdState(context.Background(), state)

    // 父修改 CWD（同一 CwdState 指针）
    state.SetCwd("/project/worktree")

    // 同一 ctx 读取可见
    assert.Equal(t, "/project/worktree", GetCwd(ctx))
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/cwd/... -v -count=1 2>&1 | head -20
```

预期：编译失败，包不存在

- [ ] **Step 3: 编写 doc.go**

创建 `internal/agentcore/sys_operation/cwd/doc.go`：

```go
// Package cwd 提供每 Agent CWD 状态管理。
//
// 对齐 Python: openjiuwen/core/sys_operation/cwd.py
// 通过 context.Context 传播 CwdState（可变容器模式），
// 实现 Agent 间隔离（InitCwd 创建新实例 + WithCwdState 派生新 ctx），
// Agent 内共享（同一 CwdState 引用的 goroutine 共享修改）。
//
// 三层 CWD 模型：
//
//	Layer 1 — ProjectRoot:  项目身份锚点（设置一次，沙箱边界依据）
//	Layer 2 — OriginalCwd:  会话起始点（worktree 退出时的恢复目标）
//	Layer 3 — Cwd:          当前工作目录（高频更新，所有工具的路径锚点）
//
// 辅助字段：Workspace（per-agent artifact 目录）、TeamWorkspace（团队共享目录）
//
// 文件目录：
//
//	cwd/
//	├── doc.go           # 包文档
//	└── cwd.go           # CwdState 结构体及全部方法
//
// 对应 Python 代码：openjiuwen/core/sys_operation/cwd.py
package cwd
```

- [ ] **Step 4: 编写 cwd.go — CwdState 核心实现**

创建 `internal/agentcore/sys_operation/cwd/cwd.go`，包含：
- CwdState 结构体（5 个字段 + sync.RWMutex）
- context key + WithCwdState / CwdStateFromCtx
- 全部 Get 方法（GetCwd, GetOriginalCwd, GetProjectRoot, GetWorkspace, GetTeamWorkspace）
- 全部 Set 方法（SetCwd, SetOriginalCwd, SetProjectRoot, SetWorkspace, SetTeamWorkspace）
- InitCwd + CwdOption
- ResolveCwd / ResolvePath
- resolve 辅助函数

按项目编码规范：中文注释、声明排列顺序（结构体→枚举→常量→全局变量→导出函数→非导出函数）

- [ ] **Step 5: 运行测试确认通过**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/cwd/... -v -count=1
```

预期：全部 PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/sys_operation/cwd/
git commit -m "feat(cwd): 新增 CwdState 包 — 三层 CWD 模型 + context 传播"
```

---

### Task 2: 删除 WithShellCwd / WithCodeCwd Option

**Files:**
- Modify: `internal/agentcore/sys_operation/sys_operation.go`
- Modify: `internal/agentcore/sys_operation/sys_operation_test.go`
- Modify: `internal/agentcore/sys_operation/doc.go`

- [ ] **Step 1: 从 sys_operation.go 删除 CWD Option**

删除以下内容：
- `ShellOptions.Cwd` 字段（行 189-190）
- `CodeOptions.Cwd` 字段（行 207-208）
- `WithShellCwd` 函数（行 392-395）
- `WithCodeCwd` 函数（行 427-430）

- [ ] **Step 2: 从 sys_operation_test.go 删除对应测试**

删除：
- `TestWithShellCwd` 测试
- `TestWithCodeCwd` 测试

- [ ] **Step 3: 更新 sys_operation/doc.go 文件目录**

在文件目录中新增 `cwd/` 子目录条目

- [ ] **Step 4: 运行编译确认**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/sys_operation/...
```

预期：编译通过（需确认没有其他代码引用被删除的字段/函数）

- [ ] **Step 5: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/sys_operation/... -v -count=1
```

预期：PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/sys_operation/
git commit -m "refactor(sys_operation): 删除 WithShellCwd/WithCodeCwd Option，CWD 改从 context 获取"
```

---

### Task 3: 修复 P0 断裂点 — ensureInitialized 返回 ctx

**Files:**
- Modify: `internal/agentcore/harness/deep_agent.go`

- [ ] **Step 1: 修改 ensureInitialized 签名**

将 `func (d *DeepAgent) ensureInitialized(ctx context.Context) error` 改为 `func (d *DeepAgent) ensureInitialized(ctx context.Context) (context.Context, error)`

在已初始化分支返回 `ctx, nil`，在末尾返回 `ctx, nil`

- [ ] **Step 2: 实现 initCwd 逻辑（替换 ⤵️ 占位）**

在 ensureInitialized 中，替换 `_ = d.deepConfig // ⤵️ 9.1 回填` 为：

```go
if cfg != nil && cfg.Workspace != nil && cfg.Workspace.RootPath != "" {
    initRoot := cfg.Workspace.RootPath
    cwdState := InitCwd(initRoot, WithWorkspace(initRoot))
    ctx = WithCwdState(ctx, cwdState)
    logger.Info(logComponent).Str("init_root", initRoot).Msg("CWD 已初始化")
}
```

- [ ] **Step 3: 更新 Invoke 中的调用**

```go
// 改前
if err := d.ensureInitialized(ctx); err != nil {
    return nil, err
}
// 改后
var initErr error
ctx, initErr = d.ensureInitialized(ctx)
if initErr != nil {
    return nil, initErr
}
```

- [ ] **Step 4: 更新 Stream 中的调用**

同 Step 3 模式

- [ ] **Step 5: 更新 EnsureInitialized 导出方法签名**

```go
// 改前
func (d *DeepAgent) EnsureInitialized(ctx context.Context) error {
    return d.ensureInitialized(ctx)
}
// 改后
func (d *DeepAgent) EnsureInitialized(ctx context.Context) (context.Context, error) {
    return d.ensureInitialized(ctx)
}
```

- [ ] **Step 6: 更新 EnsureInitialized 的所有调用方**

搜索所有调用 `EnsureInitialized` 的外部代码，更新为使用返回的 ctx

- [ ] **Step 7: 运行编译确认**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/...
```

- [ ] **Step 8: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... -v -count=1 -timeout 120s 2>&1 | tail -30
```

- [ ] **Step 9: 提交**

```bash
git add internal/agentcore/harness/deep_agent.go
git commit -m "feat(deep_agent): ensureInitialized 返回 ctx + 实现 initCwd 逻辑"
```

---

### Task 4: 修复 P0 断裂点 — Fire/ScheduleAutoInvokeOnSpawnDone/completeSessionSpawn/CreateSubagent/createReactAgent/hotReloadSystemPrompt

**Files:**
- Modify: `internal/agentcore/single_agent/interfaces/callback.go`
- Modify: `internal/agentcore/harness/deep_agent.go`
- Modify: `internal/agentcore/harness/interfaces/deep_agent.go`
- Modify: `internal/agentcore/harness/task_loop/handler.go`
- Modify: `internal/agentcore/harness/factory.go`

- [ ] **Step 1: 修复 callback.go — Fire() 增加 ctx 参数**

将 `func (c *AgentCallbackContext) Fire(event AgentCallbackEvent) error` 改为 `func (c *AgentCallbackContext) Fire(ctx context.Context, event AgentCallbackEvent) error`

内部 `manager.Execute(context.Background(), ...)` 改为 `manager.Execute(ctx, ...)`

更新所有调用 `Fire` 的地方（`FireLifecycle` 内部等）

- [ ] **Step 2: 修复 deep_agent.go — ScheduleAutoInvokeOnSpawnDone 增加 ctx 参数**

```go
// 改前
func (d *DeepAgent) ScheduleAutoInvokeOnSpawnDone(steerText string, delay float64) error {
// 改后
func (d *DeepAgent) ScheduleAutoInvokeOnSpawnDone(ctx context.Context, steerText string, delay float64) error {
```

内部 `context.WithTimeout(context.Background(), ...)` 改为 `context.WithTimeout(ctx, ...)`

- [ ] **Step 3: 更新 DeepAgentInterface 接口签名**

在 `harness/interfaces/deep_agent.go` 中：

```go
// 改前
ScheduleAutoInvokeOnSpawnDone(steerText string, delay float64) error
CreateSubagent(subagentType string, subSessionID string) (DeepAgentInterface, error)

// 改后
ScheduleAutoInvokeOnSpawnDone(ctx context.Context, steerText string, delay float64) error
CreateSubagent(ctx context.Context, subagentType string, subSessionID string) (DeepAgentInterface, error)
```

- [ ] **Step 4: 修复 handler.go — completeSessionSpawn 增加 ctx 参数**

```go
// 改前
func (h *TaskLoopEventHandler) completeSessionSpawn(taskID string, input *modules.EventHandlerInput, isError bool) {
// 改后
func (h *TaskLoopEventHandler) completeSessionSpawn(ctx context.Context, taskID string, input *modules.EventHandlerInput, isError bool) {
```

更新 HandleTaskCompletion 和 HandleTaskFailed 中的调用，传入 ctx

将 `h.provider.ScheduleAutoInvokeOnSpawnDone(steerText, ...)` 改为 `h.provider.ScheduleAutoInvokeOnSpawnDone(ctx, steerText, ...)`

- [ ] **Step 5: 修复 deep_agent.go — CreateSubagent 增加 ctx 参数**

```go
// 改前
func (d *DeepAgent) CreateSubagent(subagentType string, subSessionID string) (hinterfaces.DeepAgentInterface, error) {
    ...
    subAgent, createErr := CreateDeepAgent(context.Background(), createParams)

// 改后
func (d *DeepAgent) CreateSubagent(ctx context.Context, subagentType string, subSessionID string) (hinterfaces.DeepAgentInterface, error) {
    ...
    // 子 Agent 创建独立 CwdState
    subCwdState := InitCwd(subWwRootPath, WithWorkspace(subWwRootPath))
    subCtx := WithCwdState(ctx, subCwdState)
    subAgent, createErr := CreateDeepAgent(subCtx, createParams)
```

- [ ] **Step 6: 修复 deep_agent.go — createReactAgent 增加 ctx 参数**

```go
// 改前
func (d *DeepAgent) createReactAgent() *agents.ReActAgent {
    ...
    agent.Configure(context.Background(), reactConfig)

// 改后
func (d *DeepAgent) createReactAgent(ctx context.Context) *agents.ReActAgent {
    ...
    agent.Configure(ctx, reactConfig)
```

同步更新 `initialConfigure` 签名和调用链

- [ ] **Step 7: 修复 deep_agent.go — hotReloadSystemPrompt 增加 ctx 参数**

```go
// 改前
func (d *DeepAgent) hotReloadSystemPrompt(config *hschema.DeepAgentConfig) {
    ...
    d.reactAgent.Configure(context.Background(), &newReactConfig)

// 改后
func (d *DeepAgent) hotReloadSystemPrompt(ctx context.Context, config *hschema.DeepAgentConfig) {
    ...
    d.reactAgent.Configure(ctx, &newReactConfig)
```

更新 `hotReconfigure` 中的调用，传入 ctx

- [ ] **Step 8: 更新所有 fake/mock 实现**

搜索所有实现 `DeepAgentInterface` 的 fake 类型，更新 `ScheduleAutoInvokeOnSpawnDone` 和 `CreateSubagent` 的签名

涉及文件：
- `harness/rails/heartbeat_test.go`
- `harness/rails/agent_mode_test.go`
- `harness/rails/task_planning_test.go`
- `harness/rails/task_completion_test.go`
- `harness/task_loop/executor_test.go`
- `harness/tools/subagent/session_tools_test.go`

- [ ] **Step 9: 运行编译确认**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/...
```

- [ ] **Step 10: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/... ./internal/agentcore/single_agent/... -v -count=1 -timeout 120s 2>&1 | tail -30
```

- [ ] **Step 11: 提交**

```bash
git add internal/agentcore/
git commit -m "fix(context): 修复 P0 断裂点 — Fire/ScheduleAutoInvoke/CreateSubagent/createReactAgent/hotReload 增加 ctx"
```

---

### Task 5: 修复 P1 断裂点 — ClearContextMessages/getTools/saveContexts

**Files:**
- Modify: `internal/agentcore/single_agent/agents/react_invoke.go`
- Modify: `internal/agentcore/single_agent/agents/react_helpers.go`

- [ ] **Step 1: 修复 react_invoke.go — ClearContextMessages 增加 ctx 参数**

```go
// 改前
func (a *ReActAgent) ClearContextMessages(sess sessioninterfaces.SessionFacade) {
    ctx := context.Background()

// 改后
func (a *ReActAgent) ClearContextMessages(ctx context.Context, sess sessioninterfaces.SessionFacade) {
```

更新所有调用 `ClearContextMessages` 的地方，传入 ctx

- [ ] **Step 2: 修复 react_helpers.go — getTools 增加 ctx 参数**

```go
// 改前
func (a *ReActAgent) getTools() ([]cschema.ToolInfoInterface, error) {
    tools, _ := am.ListToolInfo(context.Background(), nil)

// 改后
func (a *ReActAgent) getTools(ctx context.Context) ([]cschema.ToolInfoInterface, error) {
    tools, _ := am.ListToolInfo(ctx, nil)
```

更新所有调用 `getTools` 的地方，传入 ctx

- [ ] **Step 3: 修复 react_helpers.go — saveContexts 增加 ctx 参数**

```go
// 改前
func (a *ReActAgent) saveContexts(sess sessioninterfaces.SessionFacade) {
    a.contextEngine.SaveContexts(context.Background(), sess, nil)

// 改后
func (a *ReActAgent) saveContexts(ctx context.Context, sess sessioninterfaces.SessionFacade) {
    a.contextEngine.SaveContexts(ctx, sess, nil)
```

更新所有调用 `saveContexts` 的地方，传入 ctx

- [ ] **Step 4: 运行编译确认**

```bash
cd /home/opensource/uap-claw-go && go build ./internal/agentcore/single_agent/...
```

- [ ] **Step 5: 运行测试**

```bash
cd /home/opensource/uap-claw-go && go test ./internal/agentcore/single_agent/... -v -count=1 -timeout 120s 2>&1 | tail -30
```

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/single_agent/
git commit -m "fix(context): 修复 P1 断裂点 — ClearContextMessages/getTools/saveContexts 增加 ctx"
```

---

### Task 6: 更新 IMPLEMENTATION_PLAN.md 回填标记

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 init_cwd 回填标记**

将 `deep_agent.go` 中的 `⤵️ 9.1 回填：init_cwd 逻辑` 相关标记在 IMPLEMENTATION_PLAN.md 中更新状态（如果有的话）

- [ ] **Step 2: 更新 _seed_runtime_cwd 回填标记**

将 `deep_adapter.go` 和 `code_adapter.go` 中的 `⤵️ agentcore.DeepAgent: _seed_runtime_cwd` 相关标记更新

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN — init_cwd 回填完成"
```

---

### Task 7: 全量编译 + 测试验证

- [ ] **Step 1: 全量编译**

```bash
cd /home/opensource/uap-claw-go && go build ./...
```

- [ ] **Step 2: 运行全量测试（非 integration/llm/e2e）**

```bash
cd /home/opensource/uap-claw-go && go test -cover -tags=!integration,!llm,!e2e ./... -timeout 300s 2>&1 | tail -50
```

- [ ] **Step 3: 检查覆盖率**

```bash
cd /home/opensource/uap-claw-go && go test -coverprofile=coverage.out ./internal/agentcore/sys_operation/cwd/... && go tool cover -func=coverage.out
```

预期：cwd 包覆盖率 ≥ 85%

- [ ] **Step 4: 提交最终状态**

```bash
git add -A
git commit -m "feat(cwd): init_cwd 子系统实现完成 — CwdState + context 传播 + 断裂点修复"
```
