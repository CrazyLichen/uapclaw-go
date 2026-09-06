# 9.66 Team Workspace 设计文档

## 概述

实现团队共享工作空间（Team Workspace），为多 Agent 并发编辑共享文件提供基础设施层。核心解决两个问题：

1. **共享文件访问**：通过 `.team/{teamName}/` symlink 挂载点，让所有成员 Agent 用标准 `read_file`/`write_file` 透明访问共享产物目录
2. **并发安全**：当多个 Agent 同时写 `.team/` 下的文件时，通过文件锁 + Git 版本控制确保一致性——TeamWorkspaceRail 作为 DeepAgentRail 在工具调用前后拦截，Agent 本身无感知

**实现范围**：仅 LOCAL 模式（单机内存锁 + 本地 git + symlink）。DISTRIBUTED 模式的方法签名预留但返回 `ErrDistributedNotImplemented`。

## 在 Agent 会话中的流程位置

```
AgentConfigurator.SetupInfra(spec, ctx)
  ├── 1. 解析 team_home / worktree 路径
  ├── 2. 创建 Messager
  ├── 3. 创建 TeamBackend
  ├── 4. 创建 WorkspaceManager ← 9.66
  │     └── TeamWorkspaceManager.Initialize() → 创建目录 + git init
  ├── 5. WorkspaceManager.MountIntoWorkspace() ← .team/{teamName} symlink
  └── 6. 模型分配

AgentConfigurator.SetupAgent(spec, ctx)
  ├── init_cwd(WithTeamWorkspace(path))
  ├── 构建 DeepAgent
  │     └── AddRail(teamWorkspaceRail) ← TeamWorkspaceRail
  └── ...

DeepAgent 运行时:
  └── 每次 tool_call:
        ├── before_tool_call: TeamWorkspaceRail.BeforeToolCall()
        │     ├── 读操作(.team/...) → maybePull (LOCAL no-op)
        │     └── 写操作(.team/...) → 检查锁 → 拒绝写入或放行
        └── after_tool_call: TeamWorkspaceRail.AfterToolCall()
              ├── auto_commit (git add + commit)
              └── publish_event(WorkspaceArtifactUpdated)
```

## 包结构

```
team_workspace/
├── doc.go              ← 更新：补充新文件列表
├── models.go           ← 已有（不变）
├── models_test.go      ← 已有（不变）
├── manager.go          ← 新增：TeamWorkspaceManager
├── manager_test.go     ← 新增
├── rail.go             ← 新增：TeamWorkspaceRail
├── rail_test.go        ← 新增
├── tool.go             ← 新增：WorkspaceMetaTool
└── tool_test.go        ← 新增

agent_teams/tools/
├── team_tool.go        ← 新增：TeamTool 基类
├── team_tool_test.go   ← 新增

agent_teams/tools/locales/
├── doc.go              ← 新增
├── translator.go       ← 新增：Translator + MakeTranslator + loadToolDesc
├── translator_test.go  ← 新增
├── descs/
│   ├── cn/
│   │   └── workspace_meta.md  ← 新增（复刻 Python）
│   └── en/
│       └── workspace_meta.md  ← 新增（复刻 Python）
```

## 1. TeamWorkspaceManager

### 结构体

```go
type TeamWorkspaceManager struct {
    config        TeamWorkspaceConfig
    workspacePath string                        // 绝对路径
    teamName      string
    mode          WorkspaceMode                  // 当前仅 LOCAL

    // 本地锁状态
    locks         map[string]WorkspaceFileLock   // filePath → lock
    lockMu        sync.Mutex                     // Go 用 sync.Mutex 替代 Python asyncio.Lock

    // 事件发布回调
    publishEvent  PublishEventFunc               // func(eventType string, event EventMessage)
}
```

### PublishEventFunc 类型

```go
// PublishEventFunc 事件发布回调函数类型。
// 对齐 Python: Callable[[str, BaseEventMessage], Awaitable[None]]
// Go 端签名与 TeamBackend.publishEvent 对齐，接受 TypedEvent 接口。
type PublishEventFunc func(eventType string, event atschema.TypedEvent)
```

Go 端用同步签名（Python 是 async 但 Go 的事件发布通常是同步入队）。`TypedEvent` 接口已在 `schema/events.go` 中定义（含 `EventTypeName()` + `ToPayload()` 方法）。Workspace 相关事件（`WorkspaceArtifactEvent` 等）已实现此接口。

### 方法清单

#### 构造与初始化

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `NewTeamWorkspaceManager(config, workspacePath, teamName, opts ...ManagerOption) *TeamWorkspaceManager` | `__init__` | 构造函数，opts 支持 WithPublishEvent 等 |
| `Initialize(ctx context.Context, remoteURL ...string) error` | `initialize()` | 创建目录 + git init + 空 commit；remoteURL 参数保留签名但 LOCAL 模式下忽略 |

#### Symlink 挂载

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `MountIntoWorkspace(workspaceRoot string) error` | `mount_into_workspace()` | 创建 `.team/{teamName}` symlink |
| `MountWorktree(slug, worktreePath string) error` | `mount_worktree()` | 创建 `.worktree/{slug}` symlink |
| `UnmountWorktree(slug string) error` | `unmount_worktree()` | 删除 symlink |
| `MountIntoWorktree(worktreePath string) error` | `mount_into_worktree()` | worktree 内创建 `.team` symlink + .gitignore |

#### 版本控制

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `AutoCommit(ctx, relativePath, memberName string) (sha string, err error)` | `auto_commit()` | git add + diff --cached + commit + 返回 SHA |
| `GetHistory(ctx, relativePath string, limit int) ([]HistoryEntry, error)` | `get_history()` | git log 解析，返回结构化历史 |

```go
// HistoryEntry 版本历史条目。
type HistoryEntry struct {
    Commit  string `json:"commit"`
    Author  string `json:"author"`
    Date    string `json:"date"`
    Message string `json:"message"`
}
```

#### 文件锁

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `GetLock(filePath string) *WorkspaceFileLock` | `get_lock()` | 读锁，自动清过期 |
| `AcquireLock(ctx, filePath, memberName, displayName string, timeoutSeconds ...int) (bool, error)` | `acquire_lock()` | sync.Mutex 保护，可重入，过期回收 |
| `ReleaseLock(ctx, filePath, memberName string) (bool, error)` | `release_lock()` | sync.Mutex 保护 |
| `ListLocks() []WorkspaceFileLock` | `list_locks()` | 清过期 + 返回全部 |

#### 访问器

| 方法 | 说明 |
|------|------|
| `Config() TeamWorkspaceConfig` | 返回配置 |
| `WorkspacePath() string` | 返回绝对路径 |
| `TeamName() string` | 返回团队名 |
| `Mode() WorkspaceMode` | 返回模式 |

#### DISTRIBUTED 占位方法

```go
var ErrDistributedNotImplemented = errors.New("分布式模式尚未实现")

func (m *TeamWorkspaceManager) Pull(ctx context.Context) (bool, error) {
    if m.mode != WorkspaceModeDistributed { return false, nil }
    return false, ErrDistributedNotImplemented
}
func (m *TeamWorkspaceManager) Push(ctx context.Context) (bool, error) {
    if m.mode != WorkspaceModeDistributed { return true, nil }
    return false, ErrDistributedNotImplemented
}
func (m *TeamWorkspaceManager) RemoteAcquireLock(...) (bool, error) { return false, ErrDistributedNotImplemented }
func (m *TeamWorkspaceManager) RemoteReleaseLock(...) (bool, error) { return false, ErrDistributedNotImplemented }
func (m *TeamWorkspaceManager) HandleLockRequest(...) (*WorkspaceLockResponseEvent, error) { return nil, ErrDistributedNotImplemented }
func (m *TeamWorkspaceManager) HandleLockResponse(...) error { return ErrDistributedNotImplemented }
```

### Git 辅助函数（包内非导出）

```go
type gitResult struct {
    OK     bool
    Stdout string
    Stderr string
}

func runGit(ctx context.Context, args []string, cwd string) gitResult
func revParse(ctx context.Context, ref, cwd string) (string, error)
```

使用 `os/exec.CommandContext`，与 `swarm/server/runtime/skill/git_ops.go` 风格一致。带超时（默认 30s）。

### Symlink 挂载实现

- 使用 `os.Symlink`，Linux/macOS 原生支持
- `_prepareMountPath` / `_mergeExistingMountContents` / `_backupExistingMountPath` 全部移植
- **Windows junction 延后**：`_mountDirectory` 中 `os.Symlink` 失败时，非 Windows 直接返回错误；Windows 下加 `// TODO: Windows junction fallback`
- 删除 Python 的 `winerror` / `ERROR_PRIVILEGE_NOT_HELD` / `_create_windows_junction`

## 2. TeamWorkspaceRail

### 结构体

```go
// TeamWorkspaceRail 团队工作空间 Rail。
// 拦截 .team/ 路径下的标准文件工具调用，透明施加锁检查和版本控制。
// 对齐 Python: TeamWorkspaceRail (team_workspace/rails.py)
type TeamWorkspaceRail struct {
    rails.DeepAgentRail
    ws           *TeamWorkspaceManager
    memberName   string
    lastPullTime float64    // monotonic 时间（仅 DISTRIBUTED）
    pullInterval float64    // 默认 5.0s
}
```

### 方法

| 方法 | Python 对齐 | 说明 |
|------|------------|------|
| `NewTeamWorkspaceRail(ws *TeamWorkspaceManager, memberName string) *TeamWorkspaceRail` | `__init__` | 构造 |
| `Init(agent BaseAgent) error` | `init()` | 调 `CwdState.SetTeamWorkspace(ws.workspacePath)` |
| `BeforeToolCall(ctx, cbc) error` | `before_tool_call()` | 读→maybePull；写→锁检查 |
| `AfterToolCall(ctx, cbc) error` | `after_tool_call()` | 写→auto_commit + publish_event |

### 常量

```go
const (
    teamPrefix  = ".team/"
    pullIntervalDefault = 5.0
)

var (
    writeTools = map[string]bool{"write_file": true, "edit_file": true}
    readTools  = map[string]bool{"read_file": true, "glob": true, "grep": true, "list_files": true}
)
```

### BeforeToolCall 逻辑

```
1. 从 cbc.Inputs() 提取 toolName, toolArgs（类型断言为 map[string]any）
2. 提取 file_path 参数（toolArgs["file_path"]）
3. file_path 不以 ".team/" 开头 → 返回 nil
4. 读操作 → maybePull()（LOCAL 下 no-op）
5. 写操作 → maybePull() + 检查锁
   - config.ConflictStrategy == ConflictStrategyLock
   - 锁被他人持有且未过期 → cbc.Extra()["workspace_lock_rejected"] = 拒绝信息
   - 打 Warn 日志
6. 返回 nil（不阻止工具执行，只标记）
```

**关键**：与 Python 行为对齐，Rail 不返回 error 阻断工具执行，只设置 Extra 标记。

### AfterToolCall 逻辑

```
1. 仅处理 writeTools
2. file_path 不以 ".team/" 开头 → 返回
3. resolveWorkspaceRelative(path) → 提取 workspace 相对路径
4. config.VersionControl → AutoCommit(ctx, relPath, memberName)
5. publishEvent != nil → 发布 WorkspaceArtifactUpdated 事件
```

### Init 方法

对齐 Python `TeamWorkspaceRail.init(agent)`，调用 `CwdState.SetTeamWorkspace()`。

需要从 `agent` 获取 `CwdState`。Go 端 `BaseAgent` 接口需要支持获取 CwdState——查看现有接口，通过 agent 的 Context 中 `CwdStateFromCtx` 获取。

## 3. TeamTool 基类

### 文件位置

`agent_teams/tools/team_tool.go`

### 结构体

```go
// TeamTool 团队工具基类。
// 对齐 Python: TeamTool(Tool, ABC)
// 子类嵌入 TeamTool 获得 Card() 默认实现，只需实现 Invoke()。
type TeamTool struct {
    card *tool.ToolCard
}

func NewTeamTool(card *tool.ToolCard) TeamTool
func (t *TeamTool) Card() *tool.ToolCard { return t.card }
```

**设计决策**：Python 的 `TeamTool` 有 `map_result()` 方法将 `ToolOutput` 映射为 LLM 可见文本。Go 端 `Tool` 接口的 `Invoke` 直接返回 `map[string]any`，没有 `ToolOutput` 中间类型，因此 `map_result` 不适用。`WorkspaceMetaTool.Invoke` 直接返回结构化数据。

## 4. WorkspaceMetaTool

### 结构体

```go
type WorkspaceMetaTool struct {
    TeamTool
    ws *TeamWorkspaceManager
}
```

### 构造

```go
func NewWorkspaceMetaTool(ws *TeamWorkspaceManager) *WorkspaceMetaTool
```

- 使用 `locales.MakeTranslator(schema.GetLanguage())` 获取翻译器
- `t("workspace_meta")` → 加载 Markdown 文件作为 description
- `t("workspace_meta", "action")` → STRINGS 中取参数描述
- `t("workspace_meta", "path")` → STRINGS 中取参数描述

### ToolCard 定义

- ID: `team.workspace_meta`
- Name: `workspace_meta`
- Description: 从 Markdown 文件加载
- InputParams:
  - `action`：string，enum [lock, unlock, locks, history]，required
  - `path`：string，lock/unlock/history 时必填

### Invoke 实现

4 个 action 对齐 Python `tools.py`：

| action | 调用 | 返回 |
|--------|------|------|
| `lock` | `ws.AcquireLock(ctx, path, memberName, displayName)` | 成功→`{"locked": path}`；失败→`{"error": "Locked by xxx"}` |
| `unlock` | `ws.ReleaseLock(ctx, path, memberName)` | `{"released": true/false}` |
| `locks` | `ws.ListLocks()` | `{"locks": [...]}` |
| `history` | `ws.GetHistory(ctx, path, 10)` | `{"history": [...]}` |

memberName / displayName 从 `opts` 的 kwargs 中提取（对齐 Python `kwargs.get("member_name")`）。

## 5. i18n：Locales + Markdown 描述

### 新增文件结构

```
agent_teams/tools/locales/
├── doc.go
├── translator.go       ← Translator 类型 + MakeTranslator + loadToolDesc
├── translator_test.go
├── descs/
│   ├── cn/
│   │   └── workspace_meta.md    ← 复刻 Python descs/cn/workspace_meta.md
│   └── en/
│       └── workspace_meta.md    ← 复刻 Python descs/en/workspace_meta.md
```

### Translator 类型

```go
// Translator 翻译器闭包。对齐 Python: Translator = Callable[..., str]
// 调用方式:
//   t("workspace_meta")             → 加载 descs/{lang}/workspace_meta.md 作为 _desc
//   t("workspace_meta", "action")   → 查 STRINGS["workspace_meta.action"]
type Translator func(tool string, key ...string) string
```

### MakeTranslator

```go
func MakeTranslator(lang schema.Language) Translator
```

- `key` 为空或 `"_desc"` → 调 `loadToolDesc(tool, lang)` 读 Markdown
- `key` 非空 → 查 `schema.STRINGS[lang]["tool.key"]`
- key 缺失时：_desc 返回空字符串（不 panic），参数描述返回 key 本身（对齐 Python KeyError 的可恢复行为）

### loadToolDesc

- 使用 `//go:embed descs/*.md` 将 Markdown 文件嵌入二进制
- 缓存结果（`sync.Map`），避免重复读取

### STRINGS 补充（schema/i18n.go）

CN:
```
"workspace_meta.action": "操作类型：lock（获取文件锁）、unlock（释放文件锁）、locks（列出所有活跃锁）、history（查看文件版本历史）"
"workspace_meta.path":   "目标文件的相对路径（lock/unlock/history 时必填）"
```

EN:
```
"workspace_meta.action": "Operation type: lock (acquire file lock), unlock (release file lock), locks (list active locks), history (view file version history)"
"workspace_meta.path":   "Relative path of the target file (required for lock/unlock/history)"
```

## 6. 回填点清单

实现 9.66 时需要同时修改以下文件，清零所有 `TODO(#9.66)`：

| 文件 | 当前 | 修改 |
|------|------|------|
| `agent/infra.go` | `WorkspaceManager any` + `TODO(#9.66)` | 改为 `WorkspaceManager *team_workspace.TeamWorkspaceManager` |
| `agent/agent_configurator.go` `WorkspaceManager()` | 返回 `any` | 返回 `*team_workspace.TeamWorkspaceManager` |
| `agent/agent_configurator.go` `SetWorkspaceManager()` | 接收 `any` | 接收 `*team_workspace.TeamWorkspaceManager` |
| `agent/agent_configurator.go` `CreateWorkspaceManager()` | `TODO(#9.66): return nil` | 实现：构造 `NewTeamWorkspaceManager` + 调用 `Initialize` |
| `agent/agent_configurator.go` `SetupInfra()` 步骤 6 | `TODO(#9.66): 设置工作空间管理器` | 调用 `c.SetWorkspaceManager(c.CreateWorkspaceManager(spec, ctx))` |
| `agent/agent_configurator.go` `SetupAgent()` 步骤 3 | `TODO(#9.66): workspace 管理器` | 调用 `ws.MountIntoWorkspace(...)` |
| `agent/agent_configurator.go` `SetupAgent()` 步骤 5 | `TODO(#9.66): 工作空间管理器路径` | 调用 `ws.MountIntoWorktree(...)` |
| `agent/agent_configurator.go` `SetupTeamBackend()` 步骤 9 | `TODO(#9.66): WorkspaceManager cleanup path` | 调用 `tb.RegisterCleanupPath(ws.WorkspacePath())` |
| `harness.go` `MountedRails.TeamWorkspace` | `any` + `TODO(#9.66+#9.68)` | 改为 `*team_workspace.TeamWorkspaceRail`，TODO 标记改为 `#9.68` |
| `harness.go` `BuildTeamAgent()` | `TODO(#9.66+#9.68)` | TODO 标记改为 `#9.68`（Rail 实例化和 AddRail 归 9.68） |

## 7. 测试计划

### manager_test.go

- `TestNewTeamWorkspaceManager` — 构造
- `TestInitialize_目录创建` — t.TempDir() 下验证目录和 artifact 子目录
- `TestInitialize_Git初始化` — 验证 .git 目录和初始 commit
- `TestInitialize_已有Git跳过` — .git 存在时跳过初始化
- `TestInitialize_无版本控制` — VersionControl=false 时仅创建目录
- `TestMountIntoWorkspace` — symlink 创建 + 路径验证
- `TestMountWorktree` — .worktree/{slug} symlink
- `TestUnmountWorktree` — symlink 删除
- `TestMountIntoWorktree` — .team symlink + .gitignore
- `TestAcquireLock_成功` — 获取锁
- `TestAcquireLock_重入` — 同一持有者再次获取刷新超时
- `TestAcquireLock_被他人持有` — 返回 false
- `TestAcquireLock_过期回收` — 过期锁可被他人获取
- `TestReleaseLock_成功` — 释放自己的锁
- `TestReleaseLock_非持有者` — 返回 false
- `TestListLocks_清除过期` — 过期锁不出现
- `TestGetLock_过期自动清除` — GetLock 触发过期清理
- `TestAutoCommit_有变更` — git add + commit + 返回 SHA
- `TestAutoCommit_无变更` — 无变更返回空
- `TestAutoCommit_无版本控制` — VersionControl=false 返回空
- `TestGetHistory` — 验证 commit/author/date/message 解析
- `TestDistributed_占位方法` — Pull/Push/RemoteAcquire 等返回 ErrDistributedNotImplemented
- `TestMergeExistingMountContents` — 旧目录内容合并
- `TestBackupExistingMountPath` — 备份旧路径
- `TestPrepareMountPath` — 新/已挂载/过期路径处理

### rail_test.go

- `TestNewTeamWorkspaceRail` — 构造
- `TestBeforeToolCall_非团队路径忽略` — file_path 不以 .team/ 开头
- `TestBeforeToolCall_读操作放行` — read_file 无锁检查
- `TestBeforeToolCall_写操作无锁放行` — 无锁时写操作放行
- `TestBeforeToolCall_写操作锁冲突` — 锁被他人持有时设 Extra 标记
- `TestBeforeToolCall_写操作自己的锁` — 同一持有者放行
- `TestAfterToolCall_非团队路径忽略`
- `TestAfterToolCall_写操作后AutoCommit` — 验证 AutoCommit 被调用
- `TestAfterToolCall_写操作后发布事件` — 验证 publishEvent 被调用
- `TestResolveWorkspaceRelative` — .team/{teamName}/path → path

### tool_test.go

- `TestNewWorkspaceMetaTool` — 构造 + ToolCard 验证
- `TestWorkspaceMetaTool_Lock成功` — action=lock, path 非空
- `TestWorkspaceMetaTool_Lock失败` — 被他人持锁
- `TestWorkspaceMetaTool_Lock缺path` — 返回错误
- `TestWorkspaceMetaTool_Unlock` — action=unlock
- `TestWorkspaceMetaTool_Locks` — action=locks
- `TestWorkspaceMetaTool_History` — action=history
- `TestWorkspaceMetaTool_未知Action` — 返回错误

### translator_test.go

- `TestMakeTranslator_中文` — 加载 cn Markdown + STRINGS
- `TestMakeTranslator_英文` — 加载 en Markdown + STRINGS
- `TestMakeTranslator_Desc缺失` — 不存在的工具返回空字符串
- `TestMakeTranslator_Key缺失` — 不存在的 key 返回 key 本身

## 8. 后续章节建议

9.66 完成后推荐下一个实现 **9.61 RecoveryManager**（无外部依赖阻塞，独立性强）。9.68-69 Team Rails/Prompts 的 TeamWorkspaceRail 类型依赖已解除，但 9.68-69 自身还有大量其他 Rails 需要实现。
