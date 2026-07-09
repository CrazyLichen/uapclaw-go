# init_cwd 子系统设计文档

> 对齐 Python: `openjiuwen/core/sys_operation/cwd.py`
> 对应实现计划章节：9.1（DeepAgent 包装）
> 当前 Go 回填标记：`deep_agent.go:1552` — `⤵️ 9.1 回填：init_cwd 逻辑`

## 1. 目标

在 Go 项目中实现等价于 Python ContextVar + CwdState 的 CWD 状态管理子系统，使得：

1. **每个 Agent 拥有独立的 CWD 状态**（per-Agent 隔离）
2. **Agent 内共享 CWD 变更**（同一 CwdState 引用的 goroutine 共享修改）
3. **子 Agent 的 CWD 修改不影响父 Agent**
4. **工具层通过 `GetCwd(ctx)` 自动获取 CWD**，无需 Option 显式传入
5. **Worktree 场景**：enter_worktree 切换 CWD，exit_worktree 恢复 CWD
6. **沙箱边界不受 CWD 切换影响**（基于 projectRoot/workspace）

## 2. 核心设计：CwdState + context.Value 传播

### 2.1 方案选型

| 方案 | 优点 | 缺点 | 结论 |
|------|------|------|------|
| A. context.Value 传播（不可变） | Go 惯用 | 父改子看不到（copy-on-write） | ❌ 不满足运行时修改需求 |
| B. DeepAgent 结构体字段 | 隔离清晰 | 工具层需持有 DeepAgent 引用 | ❌ 耦合度高 |
| C. 混合方案 | 兼顾 | 复杂 | ❌ 过度设计 |
| **D. CwdState 可变容器 + context.Value** | **语义对齐 Python ContextVar** | **需修复 context 断裂点** | **✅ 采用** |

### 2.2 语义对齐验证

**CwdState（可变容器）存入 context.Value**，通过 `*CwdState` 指针传播：

| 操作 | Python | Go |
|------|--------|-----|
| 初始化 | `init_cwd(cwd)` → 新 CwdState + `_cwd_state.set(state)` | `initCwd(cwd)` → 新 CwdState + `WithCwdState(ctx, state)` |
| 读取 | `get_cwd()` → `_state().cwd or ...` | `GetCwd(ctx)` → `CwdStateFromCtx(ctx).GetCwd()` fallback `os.Getwd()` |
| 修改 | `set_cwd(new)` → `_state().cwd = resolve(new)` | `CwdStateFromCtx(ctx).SetCwd(new)` |
| 父改子可见 | ✅ 同 Task 内共享引用 | ✅ 同一 `*CwdState` 指针，修改立即可见 |
| 子改父不受影响 | ✅ `init_cwd()` 创建新 CwdState | ✅ `WithCwdState()` 派生新 ctx，父持有旧指针 |

## 3. 数据结构

### 3.1 新增包：`internal/agentcore/sys_operation/cwd/`

#### CwdState 结构体

```go
// CwdState 每个-Agent 的可变 CWD 状态容器。
// 对齐 Python: CwdState dataclass (cwd.py:48-59)。
//
// 三层 CWD 模型：
//   Layer 1 — projectRoot:  项目身份锚点（设置一次，沙箱边界依据）
//   Layer 2 — originalCwd:  会话起始点（worktree 退出时的恢复目标）
//   Layer 3 — cwd:          当前工作目录（高频更新，所有工具的路径锚点）
//
// 辅助字段：
//   workspace:     per-agent artifact 目录
//   teamWorkspace: 团队共享 workspace 目录
//
// 并发安全：所有字段读写通过 sync.RWMutex 保护。
type CwdState struct {
    mu            sync.RWMutex
    cwd           string
    originalCwd   string
    projectRoot   string
    workspace     string
    teamWorkspace string
}
```

#### context.Value 传播函数

```go
var cwdStateKey struct{}

// WithCwdState 将 CwdState 注入 context。
// 对齐 Python: _cwd_state.set(state)
func WithCwdState(ctx context.Context, state *CwdState) context.Context {
    return context.WithValue(ctx, cwdStateKey, state)
}

// CwdStateFromCtx 从 context 中获取 CwdState。
func CwdStateFromCtx(ctx context.Context) *CwdState {
    if s, ok := ctx.Value(cwdStateKey).(*CwdState); ok {
        return s
    }
    return nil
}
```

#### 读取函数（全局入口，对齐 Python get_cwd/get_workspace 等）

```go
// GetCwd 从 context 中获取当前工作目录。
// 对齐 Python: get_cwd() (cwd.py:80-87)
// 读取优先级：cwd -> originalCwd -> os.getcwd()
func GetCwd(ctx context.Context) string

// GetOriginalCwd 从 context 中获取会话起始点。
// 对齐 Python: get_original_cwd() (cwd.py:97-99)
func GetOriginalCwd(ctx context.Context) string

// GetProjectRoot 从 context 中获取项目根目录。
// 对齐 Python: get_project_root() (cwd.py:107-109)
func GetProjectRoot(ctx context.Context) string

// GetWorkspace 从 context 中获取 agent workspace。
// 对齐 Python: get_workspace() (cwd.py:127-129)
func GetWorkspace(ctx context.Context) string

// GetTeamWorkspace 从 context 中获取团队 workspace。
// 对齐 Python: get_team_workspace() (cwd.py:147-149)
func GetTeamWorkspace(ctx context.Context) string
```

#### 初始化函数

```go
// InitCwd 初始化所有 CWD 层，创建新的 CwdState 实例。
// 对齐 Python: init_cwd(cwd, project_root, workspace, team_workspace) (cwd.py:167-198)
//
// 在 DeepAgent.ensureInitialized 中调用。
// 创建新 CwdState + WithCwdState 派生新 ctx，实现 inter-Agent 隔离。
func InitCwd(cwd string, opts ...CwdOption) *CwdState
```

#### 路径解析函数

```go
// ResolveCwd 解析工作目录。
// 对齐 Python: ShellOperation._resolve_cwd(cwd) (shell_operation.py:864-874)
//
// 解析优先级：
//   1. explicitCwd 非空且为绝对路径 → 直接使用
//   2. explicitCwd 非空且为相对路径 → 基于 GetCwd(ctx) 解析
//   3. explicitCwd 为空 → 使用 GetCwd(ctx)
func ResolveCwd(ctx context.Context, explicitCwd string) string

// ResolvePath 基于当前 CWD 解析文件路径。
// 对齐 Python: FsOperation._resolve_path(path) (fs_operation.py:1098-1133)
func ResolvePath(ctx context.Context, path string) string
```

#### CwdState 修改方法

```go
// SetCwd 更新当前工作目录。
// 对齐 Python: set_cwd(cwd) (cwd.py:90-92)
func (s *CwdState) SetCwd(cwd string)

// SetOriginalCwd 更新会话起始点。
// 对齐 Python: set_original_cwd(cwd) (cwd.py:102-104)
func (s *CwdState) SetOriginalCwd(cwd string)

// SetWorkspace 设置 agent workspace。
// 对齐 Python: set_workspace(path) (cwd.py:137-139)
func (s *CwdState) SetWorkspace(path string)

// SetTeamWorkspace 设置团队 workspace。
// 对齐 Python: set_team_workspace(path) (cwd.py:157-159)
func (s *CwdState) SetTeamWorkspace(path string)
```

## 4. Option 清理

### 4.1 删除的 Option

| 删除项 | 文件位置 | 原因 |
|--------|----------|------|
| `ShellOptions.Cwd` 字段 | `sys_operation.go:189-190` | CWD 从 context 获取 |
| `WithShellCwd()` 函数 | `sys_operation.go:392-395` | 同上 |
| `CodeOptions.Cwd` 字段 | `sys_operation.go:207-208` | 同上 |
| `WithCodeCwd()` 函数 | `sys_operation.go:427-430` | 同上 |

### 4.2 保留项

| 保留项 | 原因 |
|--------|------|
| 工具 Schema 的 `workdir` 参数 | 用户可选的显式覆盖值，非默认 CWD。对齐 Python `_resolve_cwd(cwd)` 的 `cwd` 参数 |

### 4.3 工具层变更

**BashTool / PowerShellTool**：
```go
// 修改前：cwd 通过 Option 传入
result, err := shellOp.ExecuteCmd(ctx, cmd, WithShellCwd(cwd))

// 修改后：ExecuteCmd 内部从 ctx 获取 CWD，workdir 通过 ResolveCwd 处理
resolvedCwd := cwd.ResolveCwd(ctx, workdir)  // workdir 是用户显式传入的
result, err := shellOp.ExecuteCmd(ctx, cmd)
```

**FsTool（Read/Write/Edit/Glob/Grep）**：
```go
// 路径解析
resolved := cwd.ResolvePath(ctx, path)

// 沙箱检查（不受 CWD 切换影响）
roots := []string{}
if ws := cwd.GetWorkspace(ctx); ws != "" { roots = append(roots, ws) }
if pr := cwd.GetProjectRoot(ctx); pr != "" { roots = append(roots, pr) }
```

## 5. initCwd 调用链

### 5.1 DeepAgent.ensureInitialized（核心调用点）

```go
func (d *DeepAgent) ensureInitialized(ctx context.Context) (context.Context, error) {
    // ⚠️ 关键：返回修改后的 ctx，确保 CwdState 传播到下游
    d.initMu.Lock()
    if d.initialized {
        d.initMu.Unlock()
        return ctx, nil  // 已初始化，原样返回 ctx
    }
    defer d.initMu.Unlock()

    // 初始化 CWD
    d.configMu.RLock()
    cfg := d.deepConfig
    d.configMu.RUnlock()

    if cfg != nil && cfg.Workspace != nil && cfg.Workspace.RootPath != "" {
        initRoot := cfg.Workspace.RootPath
        cwdState := InitCwd(initRoot, WithWorkspace(initRoot))
        ctx = WithCwdState(ctx, cwdState)  // 注入 CwdState 到 ctx
    }

    // ... 其他初始化逻辑 ...

    d.initialized = true
    return ctx, nil  // 返回携带 CwdState 的 ctx
}
```

### 5.2 Invoke/Stream 使用返回的 ctx

```go
func (d *DeepAgent) Invoke(ctx context.Context, ...) (map[string]any, error) {
    ctx, err := d.ensureInitialized(ctx)  // ← 获取携带 CwdState 的 ctx
    if err != nil {
        return nil, err
    }
    // 后续所有调用使用携带 CwdState 的 ctx ✅
    d.runTaskLoopInvoke(ctx, cbc, sess)
    // ...
}
```

### 5.3 子 Agent 隔离

```go
func (d *DeepAgent) CreateSubagent(ctx context.Context, ...) (*DeepAgent, error) {
    // 子 Agent 创建独立 CwdState
    subCwdState := InitCwd(subWwRootPath, WithWorkspace(subWwRootPath))
    subCtx := WithCwdState(ctx, subCwdState)  // 派生新 ctx

    subAgent, createErr := CreateDeepAgent(subCtx, createParams)
    // 父 Agent 的 ctx 仍指向原 CwdState，完全隔离 ✅
}
```

### 5.4 完整传播链

```
DeepAgent.Invoke(ctx₀)
  │
  ├─ ctx₁, _ = ensureInitialized(ctx₀)          ← 返回携带 CwdState 的 ctx₁
  │
  ├─ runTaskLoopInvoke(ctx₁, ...)
  │     └─ runTaskLoop(ctx₁, ...)
  │           └─ ctrl.SubmitRound(ctx₁, ...)
  │                 └─ HandleTaskCompletion(ctx₁, ...)
  │                       └─ completeSessionSpawn(ctx₁, ...)
  │                             └─ ScheduleAutoInvokeOnSpawnDone(ctx₁, ...)
  │                                   └─ go func() { d.Invoke(ctx₁, ...) }
  │
  ├─ runSingleRoundInvoke(ctx₁, ...)
  │     └─ reactAgent.Invoke(ctx₁, ...)
  │           └─ executeToolCalls(ctx₁, ...)
  │                 └─ Tool.Invoke(ctx₁, ...)
  │                       └─ cwd.GetCwd(ctx₁)  ← 自动获取 ✅
```

## 6. Context 断裂点修复

### 6.1 需修复的断裂点（CWD 传播链上）

全面排查 4 种模式：
- **模式 1**：函数内部 WithValue 创建新 ctx 但不返回给调用方
- **模式 2**：函数签名接受 ctx 但内部用 context.Background() 替代
- **模式 3**：goroutine 启动新执行但未传入 ctx
- **模式 4**：调用方丢弃了返回的 ctx

| # | 优先级 | 文件 | 行号 | 模式 | 当前代码 | 修复方式 |
|---|--------|------|------|------|----------|----------|
| 1 | P0 | `deep_agent.go` | 1541 | 模式1 | `ensureInitialized(ctx) error` | 改为返回 `(context.Context, error)`，调用方用返回的 ctx |
| 2 | P0 | `callback.go` | 543 | 模式2 | `manager.Execute(context.Background(), event, c)` | `Fire()` 增加 `ctx` 参数：`Fire(ctx, event)` |
| 3 | P0 | `deep_agent.go` | 577 | 模式2+3 | `context.WithTimeout(context.Background(), 10min)` | `ScheduleAutoInvokeOnSpawnDone` 增加 `ctx` 参数，从调用方传入 |
| 4 | P0 | `handler.go` | 544 | 模式2 | `completeSessionSpawn` 无 ctx | 增加 `ctx` 参数，从 `HandleTaskCompletion/HandleTaskFailed` 传入 |
| 5 | P0 | `deep_agent.go` | 634 | 模式2 | `CreateDeepAgent(context.Background(), ...)` | `CreateSubagent` 增加 `ctx` 参数，创建独立 CwdState 后 `WithCwdState(ctx, subCwdState)` |
| 6 | P0 | `deep_agent.go` | 1518 | 模式2 | `createReactAgent` 内 `agent.Configure(context.Background(), ...)` | `createReactAgent` 增加 `ctx` 参数，透传给 `Configure` |
| 7 | P0 | `deep_agent.go` | 1407 | 模式2 | `hotReloadSystemPrompt` 内 `reactAgent.Configure(context.Background(), ...)` | `hotReloadSystemPrompt` 增加 `ctx` 参数，透传给 `Configure` |
| 8 | P1 | `react_invoke.go` | 222 | 模式2 | `ctx := context.Background()` | `ClearContextMessages` 增加 `ctx` 参数 |
| 9 | P1 | `react_helpers.go` | 94 | 模式2 | `am.ListToolInfo(context.Background(), nil)` | `getTools` 增加 `ctx` 参数 |
| 10 | P1 | `react_helpers.go` | 154 | 模式2 | `a.contextEngine.SaveContexts(context.Background(), ...)` | `saveContexts` 增加 `ctx` 参数 |

### 6.2 ensureInitialized 改签名的影响分析

改为 `ensureInitialized(ctx) (context.Context, error)` 后，3 个调用点需更新：

| 调用方 | 文件:行号 | 更新方式 |
|--------|-----------|----------|
| `DeepAgent.Invoke` | deep_agent.go:220 | `ctx, err = d.ensureInitialized(ctx)` 后续用新 ctx |
| `DeepAgent.Stream` | deep_agent.go:280 | 同上 |
| `DeepAgent.EnsureInitialized` (导出) | deep_agent.go:906 | 同步改为返回 `(context.Context, error)` |

### 6.3 createReactAgent / initialConfigure 签名变更

| 方法 | 变更 | 原因 |
|------|------|------|
| `createReactAgent()` → `createReactAgent(ctx context.Context)` | 增加 ctx，透传给 `agent.Configure(ctx, ...)` | 断裂点 6 |
| `initialConfigure(config)` → `initialConfigure(ctx, config)` | 增加 ctx，透传给 `createReactAgent(ctx)` | 同上 |
| `hotReloadSystemPrompt(config)` → `hotReloadSystemPrompt(ctx, config)` | 增加 ctx，透传给 `reactAgent.Configure(ctx, ...)` | 断裂点 7 |
| `hotReconfigure(ctx, config)` → 已有 ctx，透传给 `hotReloadSystemPrompt(ctx, config)` | 无签名变更，只需传 ctx | 同上 |

### 6.4 接口签名变更

| 接口 | 变更 |
|------|------|
| `DeepAgentInterface.ScheduleAutoInvokeOnSpawnDone` | 增加 `ctx context.Context` 第一参数 |
| `DeepAgentInterface.CreateSubagent` | 增加 `ctx context.Context` 第一参数 |
| `AgentCallbackContext.Fire` | 增加 `ctx context.Context` 第一参数 |
| `DeepAgent.EnsureInitialized` (导出) | 改为返回 `(context.Context, error)` |

### 6.5 不需要修复的断裂点

以下位置使用 `context.Background()` 是合理的（独立生命周期 / CWD 不关键）：

| 文件 | 行号 | 原因 |
|------|------|------|
| `registry.go:164` | 注册表加载 DeepAgent | 创建阶段只做配置组装 |
| `session/agent.go:95,281,450,476` | Session CloseStream / agent_team 操作 | 独立生命周期，CWD 不关键 |
| `session/stream/manager.go:196,198,206` | 流超时管理 | 独立超时控制 |
| `session/stream/writer.go:91` | WriteInteraction | 接口无 ctx 参数，已标注 ⤵️ |
| `session/controller/data_container.go:152` | PreRun | 初始化阶段 |
| `session/controller/global_controller.go:142` | errgroup | 独立生命周期 |
| `interrupt/handler.go:215` | 中断写入流 | 不在 CWD 传播热路径 |
| `remote_skill_util.go:289,415,475` | 远程技能 HTTP 请求 | 接口限制 |
| `processor_state_recorder.go:157` | trace 写入 | 不在 CWD 传播热路径 |
| `offload.go:150` | 上下文 offload 写文件 | 不在 CWD 传播热路径 |

## 7. Worktree CWD 切换

### 7.1 EnterWorktreeTool

对齐 Python: `EnterWorktreeTool.invoke()` (worktree/tools.py:110-175)

```go
cwdState := cwd.CwdStateFromCtx(ctx)
if cwdState != nil {
    cwdState.SetCwd(session.WorktreePath)       // Layer 3: 切到 worktree
    cwdState.SetOriginalCwd(session.WorktreePath) // Layer 2: 同步切到 worktree
    // Layer 1 projectRoot 不变！
}
```

### 7.2 ExitWorktreeTool

对齐 Python: `ExitWorktreeTool.invoke()` (worktree/tools.py:231-313)

```go
cwdState := cwd.CwdStateFromCtx(ctx)
if cwdState != nil && result.OriginalCwd != "" {
    cwdState.SetCwd(result.OriginalCwd)       // Layer 3: 恢复
    cwdState.SetOriginalCwd(result.OriginalCwd) // Layer 2: 同步恢复
}
```

### 7.3 WorktreeRail.before_invoke — 中断/恢复

对齐 Python: `WorktreeRail.before_invoke()` (worktree/rails.py:150-188)

```go
cwdState := cwd.CwdStateFromCtx(ctx)
if cwdState != nil && stored.WorktreePath != "" {
    cwdState.SetCwd(stored.WorktreePath)
    cwdState.SetOriginalCwd(stored.WorktreePath)
}
```

## 8. 沙箱检查

沙箱边界使用 `GetWorkspace(ctx)` / `GetProjectRoot(ctx)`，**不依赖 `GetCwd(ctx)`**。

这确保即使 CWD 被 worktree 改变，沙箱边界仍由 workspace/projectRoot 固定。

对齐 Python: `FsOperation._resolve_path` (fs_operation.py:1098-1133)

```go
func (op *LocalFsOperation) resolvePath(ctx context.Context, path string) string {
    base := cwd.GetCwd(ctx)
    resolved := filepath.Join(base, path)

    if op.runConfig.RestrictToSandbox {
        var roots []string
        if op.runConfig.SandboxRoot != "" {
            roots = []string{op.runConfig.SandboxRoot}
        } else {
            if ws := cwd.GetWorkspace(ctx); ws != "" {
                roots = append(roots, ws)
            }
            if pr := cwd.GetProjectRoot(ctx); pr != "" {
                roots = append(roots, pr)
            }
        }
        // 检查 resolved 是否在 roots 范围内
    }
    return resolved
}
```

## 9. 文件变更清单

### 新增文件

| 文件 | 内容 |
|------|------|
| `internal/agentcore/sys_operation/cwd/doc.go` | 包文档 |
| `internal/agentcore/sys_operation/cwd/cwd.go` | CwdState 结构体 + 全部方法 + context 传播函数 |
| `internal/agentcore/sys_operation/cwd/cwd_test.go` | 单元测试 |

### 修改文件

| 文件 | 变更 |
|------|------|
| `sys_operation/sys_operation.go` | 删除 `ShellOptions.Cwd`、`CodeOptions.Cwd`、`WithShellCwd`、`WithCodeCwd` |
| `sys_operation/sys_operation_test.go` | 删除对应测试 |
| `sys_operation/doc.go` | 更新文件目录（新增 cwd/） |
| `harness/deep_agent.go` | 实现 initCwd（替换 ⤵️ 占位）+ ensureInitialized 返回 ctx + createReactAgent/initialConfigure/hotReloadSystemPrompt 增加 ctx + CreateSubagent 增加 ctx + ScheduleAutoInvokeOnSpawnDone 增加 ctx |
| `harness/deep_agent_test.go` | 更新测试 |
| `harness/interfaces/deep_agent.go` | ScheduleAutoInvokeOnSpawnDone + CreateSubagent 增加 ctx 参数 |
| `harness/task_loop/handler.go` | completeSessionSpawn 增加 ctx 参数 |
| `harness/task_loop/executor_test.go` | 更新 fake 接口实现 |
| `single_agent/interfaces/callback.go` | Fire() 增加 ctx 参数 |
| `single_agent/agents/react_invoke.go` | ClearContextMessages 增加 ctx 参数 |
| `single_agent/agents/react_helpers.go` | getTools/saveContexts 增加 ctx 参数 |
| `harness/factory.go` | 更新 CreateDeepAgent 中 CWD 注入逻辑 |
| `harness/rails/*_test.go` | 更新 fake DeepAgentInterface 实现（ScheduleAutoInvokeOnSpawnDone/CreateSubagent 签名） |
| `swarm/server/adapter/deep_adapter.go` | 实现 seedRuntimeCwd（对齐 Python _seed_runtime_cwd）+ CreateInstance 步骤 21 + ProcessMessage 请求级 CWD 注入 |
| `swarm/server/adapter/code_adapter.go` | CreateInstance 步骤 20.5 委托 deep.seedRuntimeCwd |

## 10. 对应 Python 代码映射

| Go | Python |
|----|--------|
| `cwd.InitCwd(root, WithWorkspace(ws))` | `init_cwd(root, workspace=ws)` |
| `cwd.GetCwd(ctx)` | `get_cwd()` |
| `cwd.GetOriginalCwd(ctx)` | `get_original_cwd()` |
| `cwd.GetProjectRoot(ctx)` | `get_project_root()` |
| `cwd.GetWorkspace(ctx)` | `get_workspace()` |
| `cwd.GetTeamWorkspace(ctx)` | `get_team_workspace()` |
| `cwd.WithCwdState(ctx, state)` | `_cwd_state.set(state)` |
| `cwd.CwdStateFromCtx(ctx)` | `_cwd_state.get()` |
| `cwdState.SetCwd(new)` | `set_cwd(new)` |
| `cwdState.SetOriginalCwd(new)` | `set_original_cwd(new)` |
| `cwd.ResolveCwd(ctx, workdir)` | `ShellOperation._resolve_cwd(cwd)` |
| `cwd.ResolvePath(ctx, path)` | `FsOperation._resolve_path(path)` |
