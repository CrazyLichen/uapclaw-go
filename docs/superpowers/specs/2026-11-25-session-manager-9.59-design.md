# 9.59 SessionManager 设计文档

## 概述

9.59 实现团队会话管理器（SessionManager），负责 TeamAgent 会话的**三态生命周期管理**（Unbound / Half-bound / Fully-bound）和 session_id 的上下文传播。

### 在 Agent 会话流程中的位置

```
TeamAgent 生命周期
─────────────────
  1. NewTeamAgent()          ← 构造
  2. Configure(spec, ctx)    ← 配置（AgentConfigurator）
  3. Invoke/Stream(inputs)   ← 执行入口
      ↓
  4. CoordinationKernel.start(session)
      ├─ SessionManager.BindSession(session)    ← 9.59 核心入口
      │   ├─ 设置 sessionID 到 SessionState
      │   ├─ 注入 SessionState 到 context
      │   ├─ 存储 session 到 TeamAgentState
      │   ├─ 创建 DB 表（幂等）  TODO(#9.61)
      │   └─ Leader 侧持久化配置  TODO(#9.61)
      ├─ SpawnManager.SpawnTeammate(...)         ← 9.58
      └─ 开始事件循环
  5. 运行中...
      ├─ CoordinationKernel.pause() → SessionManager.ReleaseSession()
      ├─ CoordinationKernel.stop()  → SessionManager.ReleaseSession()
      └─ 会话切换 → SessionManager.ResumeForNewSession(session)
  6. 恢复场景
      └─ SessionManager.RecoverForExistingSession(session)
```

### 核心作用

1. **session_id 上下文传播** — 通过 Go context（对齐 Python contextvars）注入 session_id
2. **会话生命周期管理** — bind/release/resume/recover 四个关键操作
3. **Leader 侧检查点持久化** — 绑定时自动持久化配置到 checkpoint namespace（TODO #9.61）

## 设计决策

### 决策 1：实现范围

| 内容 | 位置 | 状态 |
|------|------|------|
| SessionState 可变容器 | `agent_teams/context.go` 扩展 | ✅ 实现 |
| WithSessionState / GetSessionID | `agent_teams/context.go` 扩展 | ✅ 实现 |
| SessionManager | `agent/session/session_manager.go` | ✅ 实现 |
| 回填 6 处 TODO(#9.59) | `agent/team_agent.go` + `agent/state.go` | ✅ 实现 |
| Interaction 层 | `agent_teams/interaction/` | ⤵️ 延后单独章节 |

**原因**：Python `interaction/` 目录是独立的输入路由子系统（UserInbox / HumanAgentInbox / Router / Payload），与 SessionManager 无直接依赖关系，应独立成章实现。

### 决策 2：Python contextvars → Go 可变状态容器 + context.Value

复用 CwdState 模式（`internal/agentcore/sys_operation/cwd/`）：

| Python | Go |
|--------|-----|
| `_session_id_context: ContextVar` | `SessionState` 可变 struct + `context.Value` 传指针 |
| `set_session_id(sid) → Token` | `SessionState.SetSessionID(sid)` 原地修改 |
| `reset_session_id(token)` | `SessionState.SetSessionID("")` 清空 |
| `get_session_id()` | `GetSessionID(ctx)` 从 context 取指针读字段 |
| `copy_context()` spawn 继承 | spawn 时创建新 `SessionState` 实例 + `WithSessionState` 派生新 ctx |

**不需要 Token/reset 机制**：Go 的 context 是值式的，不是副作用式的。`SetSessionID` 原地修改共享指针，同一 TeamAgent 内的 goroutine 立即可见。

### 决策 3：SessionState 只放 sessionID

SessionState 只包含 `sessionID` 字段，`teamSession` 仍留在 `TeamAgentState.TeamSession`。

**原因**：职责单一，SessionState 只管 session_id 传播（对齐 Python contextvar），teamSession 是业务对象留在 state 中。

### 决策 4：BindSession 时按需创建并注入

SessionState 在 `NewSessionManager` 时创建实例，但**不立即注入 ctx**。第一次调用 `BindSession` 时按需通过 `WithSessionState` 注入。返回的 ctx 必须沿调用链传播。

**原因**：最接近 Python 的"首次 `set_session_id` 时才出现"语义。`GetSessionID(ctx)` 处理 nil 回退返回 `""`。

### 决策 5：依赖处理

- `RecoveryManager`（#9.61）用 `any` 占位 + `TODO(#9.61)` 注释标记回填
- `AgentTeamSession` 类型未定义，`session` 参数用 `any` + `TODO(#9.59)` 注释
- `ResumeForNewSession` / `RecoverForExistingSession` 中 RecoveryManager 相关逻辑用 TODO 占位

## 详细设计

### 1. SessionState（扩展 agent_teams/context.go）

```go
// SessionState 每-TeamAgent 的可变 session 状态容器。
// 对齐 Python: _session_id_context (contextvars.ContextVar)
//
// 通过 context.Value 传播 *SessionState 指针：
//   - 同一 TeamAgent 内的 goroutine 共享同一 SessionState 引用，SetSessionID 后立即可见
//   - 子 Teammate 调用 InitSessionState 创建新实例 + WithSessionState 派生新 ctx，父不受影响
//
// 并发安全：所有字段读写通过 sync.RWMutex 保护。
type SessionState struct {
    mu        sync.RWMutex
    sessionID string
}

type sessionStateKeyType struct{}

// InitSessionState 创建新的 SessionState 实例。
func InitSessionState() *SessionState

// WithSessionState 将 SessionState 注入 context。
func WithSessionState(ctx context.Context, state *SessionState) context.Context

// SessionStateFromCtx 从 context 中获取 SessionState。返回 nil 表示未绑定。
func SessionStateFromCtx(ctx context.Context) *SessionState

// GetSessionID 从 context 中获取当前 session_id。
// nil 回退返回 ""。
func GetSessionID(ctx context.Context) string

// GetSessionID 获取当前 session_id。
func (s *SessionState) GetSessionID() string

// SetSessionID 设置当前 session_id。并发安全。
func (s *SessionState) SetSessionID(sessionID string)
```

**注意**：替换现有的 `SetSessionID(parent, id) context.Context` 和 `GetSessionID(ctx) string` 函数。现有调用方需适配。

### 2. SessionManager（新建 agent/session_manager.go）

```go
// SessionManager 管理团队会话生命周期和持久化。
// 对齐 Python: SessionManager (openjiuwen/agent_teams/agent/session_manager.py)
//
// 三态模型：
//
//	Unbound     — sessionID="" teamSession=nil  从未绑定或显式解绑
//	Half-bound  — sessionID="X" teamSession=nil 会话释放但 ID 保留用于日志
//	Fully-bound — sessionID="X" teamSession=ses 活跃会话，可读写
//
// 禁止状态：sessionID="" + teamSession!=nil（悬空状态）
type SessionManager struct {
    state           *TeamAgentState
    configurator    *AgentConfigurator
    recoveryManager any  // TODO(#9.61): RecoveryManager 类型
    sessionState    *SessionState
}

// NewSessionManager 创建新的 SessionManager。
func NewSessionManager(
    state *TeamAgentState,
    configurator *AgentConfigurator,
    recoveryManager any,  // TODO(#9.61)
) *SessionManager

// TeamSession 返回当前团队会话。
func (m *SessionManager) TeamSession() any

// SetTeamSession 设置团队会话。
func (m *SessionManager) SetTeamSession(session any)

// BindSession 完整绑定到指定会话。
// 返回新的 context.Context（含 SessionState），调用方必须用于后续传播。
func (m *SessionManager) BindSession(
    ctx context.Context,
    session any,  // TODO(#9.59): AgentTeamSession 接口
) (context.Context, error)

// ReleaseSession 从当前会话解绑。
// 只清空 SessionState.sessionID + TeamAgentState.TeamSession，不返回新 ctx。
func (m *SessionManager) ReleaseSession()

// ResumeForNewSession 切换到新会话并重新绑定活着的 teammate 运行时。
// TODO(#9.61): RecoveryManager 回填
func (m *SessionManager) ResumeForNewSession(
    ctx context.Context,
    session any,
) (context.Context, error)

// RecoverForExistingSession 绑定到检查点恢复的会话（不清理）。
// TODO(#9.61): RecoveryManager 回填
func (m *SessionManager) RecoverForExistingSession(
    ctx context.Context,
    session any,
) (context.Context, error)
```

### 3. BindSession 实现逻辑

```
BindSession(ctx, session):
  1. sessionID = extractSessionID(session)
  2. m.sessionState.SetSessionID(sessionID)        // 原地修改，共享指针立即可见
  3. if SessionStateFromCtx(ctx) == nil:
       newCtx = WithSessionState(ctx, m.sessionState)  // 首次注入
     else:
       newCtx = ctx
  4. m.state.TeamSession = session
  5. // TODO(#9.61): teamBackend.DB().CreateCurSessionTables()
  6. // TODO(#9.61): if Leader → recoveryManager.PersistLeaderConfig(session)
  7. return newCtx, nil
```

### 4. ReleaseSession 实现逻辑

```
ReleaseSession():
  1. m.sessionState.SetSessionID("")                // 清空 sessionID
  2. m.state.TeamSession = nil                       // 清空 teamSession
```

### 5. 回填 TODO(#9.59) 清单

| 文件 | 行 | 当前 | 改为 |
|------|-----|------|------|
| `team_agent.go:77` | sessionManager 字段 | `sessionManager any // TODO(#9.59)` | `sessionManager *SessionManager` |
| `team_agent.go:106` | 构造函数 | `// TODO(#9.59): 构建 SessionManager(...)` | `a.sessionManager = NewSessionManager(a.state, a.configurator, a.recoveryManager)` |
| `team_agent.go:269` | SessionID 方法 | `// TODO(#9.59): 从 GetSessionID(ctx) 读取` | `return agentteams.GetSessionID(ctx)` |
| `team_agent.go:641` | ResumeForNewSession | `// TODO(#9.59): sessionManager.resume_for_new_session(session)` | `newCtx, err := a.sessionManager.ResumeForNewSession(ctx, session)` + 传播 ctx |
| `team_agent.go:650` | RecoverForExistingSession | `// TODO(#9.59+#9.62): ...` | `newCtx, err := a.sessionManager.RecoverForExistingSession(ctx, session)` + 传播 ctx |
| `state.go:15` | TeamSession 字段 | `// TODO(#9.59): AgentTeamSession 类型` | 保留 `any`（AgentTeamSession 接口尚未定义），更新注释 |

### 6. extractSessionID 辅助函数

从 `any` 类型的 session 中提取 session_id。当前 session 类型为 `any`，通过类型断言或反射提取：

```go
// extractSessionID 从 session 对象中提取 session_id。
// TODO(#9.59): 定义 AgentTeamSession 接口后替换为接口方法调用
func extractSessionID(session any) string {
    if session == nil {
        return ""
    }
    // 尝试类型断言到含 GetSessionID() 方法的接口
    type sessionIDer interface {
        GetSessionID() string
    }
    if s, ok := session.(sessionIDer); ok {
        return s.GetSessionID()
    }
    return ""
}
```

## 延后项

| 项目 | 原因 | 回填章节 |
|------|------|---------|
| Interaction 层（payload / router / UserInbox / HumanAgentInbox） | 独立子系统，无直接依赖 | 后续独立章节 |
| RecoveryManager 集成 | 9.61 未实现 | #9.61 |
| AgentTeamSession 接口定义 | 类型未定义 | #9.59 后续或相关章节 |
| teamBackend.DB().CreateCurSessionTables() | DB 层未实现 | #9.64 |
| Leader 侧 PersistLeaderConfig | 依赖 RecoveryManager | #9.61 |

## 测试计划

### SessionState 测试

| 测试用例 | 说明 |
|---------|------|
| TestInitSessionState_基本初始化 | 创建实例，默认 sessionID="" |
| TestSessionState_SetSessionID | 设置后立即可见 |
| TestSessionState_并发安全 | 并发读写不 panic |
| TestWithSessionState_上下文传播 | 注入 ctx 后可取出 |
| TestWithSessionState_上下文无SessionState | 未注入时返回 nil |
| TestGetSessionID_从上下文获取 | 通过全局函数读取 |
| TestGetSessionID_上下文无SessionState回退 | nil 时返回 "" |
| Test子Agent隔离 | 子 Agent 创建新实例不影响父 |
| Test父改子可见 | 同一指针修改后同 ctx 可见 |

### SessionManager 测试

| 测试用例 | 说明 |
|---------|------|
| TestNewSessionManager_基本构造 | 验证字段初始化 |
| TestBindSession_基本绑定 | sessionID 设置 + ctx 注入 + teamSession 存储 |
| TestBindSession_返回新Ctx | 验证返回的 ctx 含 SessionState |
| TestBindSession_重复绑定 | 两次 bind，后一次覆盖前一次 |
| TestReleaseSession_释放 | sessionID 清空 + teamSession 置 nil |
| TestReleaseSession_幂等 | 多次 release 不 panic |
| TestResumeForNewSession_基本流程 | TODO(#9.61) 回填后补充 |
| TestRecoverForExistingSession_基本流程 | TODO(#9.61) 回填后补充 |
| TestSessionManager_三态转换 | Unbound → Fully-bound → Half-bound → Unbound |

## 对齐 Python 方法映射

| Go 方法 | Python 方法 | 说明 |
|---------|------------|------|
| `SessionManager.BindSession` | `SessionManager.bind_session` | 返回新 ctx（Go 特有） |
| `SessionManager.ReleaseSession` | `SessionManager.release_session` | 不返回 ctx |
| `SessionManager.ResumeForNewSession` | `SessionManager.resume_for_new_session` | 返回新 ctx |
| `SessionManager.RecoverForExistingSession` | `SessionManager.recover_for_existing_session` | 返回新 ctx |
| `SessionManager.TeamSession` | `SessionManager.team_session` (getter) | 委托 state |
| `SessionManager.SetTeamSession` | `SessionManager.team_session` (setter) | 委托 state |
| `agent_teams.GetSessionID(ctx)` | `get_session_id()` | Go 需传 ctx |
| `SessionState.SetSessionID(id)` | `set_session_id(id) → Token` | Go 不需要 Token |
| `WithSessionState(ctx, state)` | `copy_context()` (spawn) | 派生新 ctx |
