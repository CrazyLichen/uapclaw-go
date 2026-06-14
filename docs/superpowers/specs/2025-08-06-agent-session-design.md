# 5.3 AgentSession 实现设计

> 将 Python 项目 `openjiuwen/core/session/agent.py` + `openjiuwen/core/session/internal/agent.py` 用 Go 重新实现，
> 包含公开层 Session（用户 API）和内部层 AgentSession（BaseSession 实现），
> 以及 AgentStateCollection 回填和 CallbackFramework Session 维度扩展。

---

## 设计决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 架构分层 | 保持两层：公开层 Session + 内部层 AgentSession | 对齐 Python 架构，职责边界清晰 |
| 并发模型 | 同步方法签名 `ctx context.Context + error` | Python async 是顺序执行，Go 同步即可 |
| 未实现依赖 | 先用 `any + nil` 占位 | 与现有 BaseSession 的 any 返回值策略一致 |
| StateCollection 位置 | 放 state 包内 (`agent_state_collection.go`) | 对齐 Python 的 state/agent_state.py |
| 方法范围 | 全量接口 + 桩实现 | 接口完整，后续只需填充实现 |
| 回调事件 | 扩展现有 CallbackFramework，加 Session 维度 | Python 的 trigger 就是用的 AsyncCallbackFramework |
| 代码组织 | 内部层放 `session/internal/` 子包 | 后续 AgentTeamSession/WorkflowSession 都放此处，更整洁 |

---

## 文件结构

```
internal/agentcore/
├── runner/callback/
│   ├── events.go                          # 修改：新增 SessionCallEventType 枚举 + SessionCallEventData 结构体
│   ├── framework.go                       # 修改：新增 sessionCallbacks map + OnSession/OffSession/TriggerSession
│   ├── framework_test.go                  # 修改：补充 Session 维度测试
│   └── events_test.go                     # 修改：补充 Session 事件测试
│
├── session/
│   ├── doc.go                             # 修改：更新文件目录和包文档
│   ├── agent.go                           # 新增：公开层 Session 结构体（用户 API）
│   ├── agent_test.go                      # 新增：Session 测试
│   └── internal/
│       ├── doc.go                         # 新增：internal 子包文档
│       ├── agent_session.go               # 新增：内部层 AgentSession（BaseSession 实现）
│       └── agent_session_test.go          # 新增：AgentSession 测试
│
└── session/state/
    ├── doc.go                             # 修改：更新文件目录
    ├── agent_state_collection.go          # 新增：Agent State StateCollection（回填 5.1）
    └── agent_state_collection_test.go     # 新增：AgentStateCollection 测试
```

修改文件 4 个，新增文件 6 个。

---

## 内部层 AgentSession

纯粹的"组件容器"，实现 BaseSession 接口，不包含业务逻辑。

### 结构体

```go
type AgentSession struct {
    sessionID           string       // 会话唯一标识
    config              any          // ⤵️ 5.12 回填: any → SessionConfig
    state               state.State  // 会话状态（AgentStateCollection）
    tracer              any          // ⤵️ 5.11 回填: any → Tracer
    streamWriterManager any          // ⤵️ 5.10 回填: any → StreamWriterManager
    checkpointer        any          // ⤵️ 5.8 回填: any → Checkpointer
    agentSpan           any          // Agent 追踪跨度
    card                any          // ⤵️ 后续回填: any → *schema.AgentCard
}
```

### 构造

使用 `AgentSessionOption` 函数选项模式：

```go
type AgentSessionOption func(*AgentSession)

func NewAgentSession(sessionID string, opts ...AgentSessionOption) *AgentSession
```

### BaseSession 接口方法

| 方法 | 实现 |
|------|------|
| `Config()` | 返回 `s.config`（可能为 nil） |
| `State()` | 返回 `s.state` |
| `Tracer()` | 返回 `s.tracer`（可能为 nil） |
| `StreamWriterManager()` | 返回 `s.streamWriterManager`（可能为 nil） |
| `SessionID()` | 返回 `s.sessionID` |
| `Checkpointer()` | 返回 `s.checkpointer`（可能为 nil） |
| `ActorManager()` | 返回 nil |
| `Close()` | 返回 nil |

---

## 公开层 Session

用户面向 API，组合内部层 AgentSession，实现完整生命周期和业务逻辑。

### 结构体

```go
type Session struct {
    inner                *internal.AgentSession  // 内部 AgentSession 实例
    card                 any                     // ⤵️ 后续回填: any → *schema.AgentCard
    preRunDone           bool                    // PreRun 幂等守卫
    postRunDone          bool                    // PostRun 幂等守卫
    closeStreamOnPostRun bool                    // PostRun 时是否自动关闭流
    interaction          any                     // ⤵️ 5.9 回填: any → SimpleAgentInteraction
    sourceMetadata       map[string]any          // 流数据来源元数据
}
```

### 构造

使用 `SessionOption` 函数选项模式：

```go
type SessionOption func(*Session)

func NewSession(opts ...SessionOption) *Session
```

注意：Go 中公开层构造函数命名为 `NewSession`（返回 `*Session`），
与内部层 `internal.NewAgentSession`（返回 `*internal.AgentSession`）通过命名区分，避免混淆。

### 方法清单

#### 身份/配置

| 方法 | 签名 | 说明 |
|------|------|------|
| `GetSessionID` | `() string` | 返回会话唯一标识 |
| `GetEnv` | `(key string, defaultValue ...any) any` | 获取环境变量值 |
| `GetEnvs` | `() map[string]any` | 获取所有环境变量 |
| `GetAgentID` | `() string` | 返回 Agent ID |
| `GetAgentName` | `() string` | 返回 Agent 名称 |
| `GetAgentDescription` | `() string` | 返回 Agent 描述 |

#### 状态读写

| 方法 | 签名 | 说明 |
|------|------|------|
| `UpdateState` | `(data map[string]any)` | 更新全局状态，委托 inner.State() |
| `GetState` | `(key string) any` | 获取全局状态值，委托 inner.State() |
| `DumpState` | `() map[string]any` | 导出完整状态快照，委托 inner.State() |

#### 流操作（桩实现，依赖 5.10）

| 方法 | 签名 | 当前行为 |
|------|------|---------|
| `WriteStream` | `(data any) error` | 返回 nil |
| `WriteCustomStream` | `(data any) error` | 返回 nil |
| `StreamIterator` | `() <-chan any` | 返回 nil channel |
| `CloseStream` | `() error` | 返回 nil |

#### 生命周期

**PreRun 流程：**

```
PreRun(ctx, inputs)
  1. 幂等检查：preRunDone == true → 直接返回 nil
  2. 触发 SessionEvents.AgentSessionCreated 回调（CallbackFramework.TriggerSession）
  3. Checkpointer.preAgentExecute(inner, inputs) — 当前 checkpointer 为 nil，跳过
  4. preRunDone = true
  5. 返回 nil
```

**PostRun 流程：**

```
PostRun(ctx)
  1. 幂等检查：postRunDone == true → 直接返回 nil
  2. 若 closeStreamOnPostRun → CloseStream()
  3. Commit(ctx)
  4. postRunDone = true
  5. 返回 nil
```

**Commit 流程：**

```
Commit(ctx)
  1. Checkpointer.postAgentExecute(inner) — 当前 checkpointer 为 nil，跳过
  2. 返回 nil
```

| 方法 | 签名 | 说明 |
|------|------|------|
| `PreRun` | `(ctx context.Context, inputs ...map[string]any) error` | 会话预运行 |
| `PostRun` | `(ctx context.Context) error` | 会话后运行 |
| `Commit` | `(ctx context.Context) error` | 提交检查点（不关闭流） |

#### 交互（桩实现，依赖 5.9）

| 方法 | 签名 | 当前行为 |
|------|------|---------|
| `Interact` | `(value any) error` | 返回 nil |

#### 子会话（桩实现，依赖 5.5）

| 方法 | 签名 | 当前行为 |
|------|------|---------|
| `CreateWorkflowSession` | `() any` | 返回 nil |

### 桩实现解除依赖对照表

| 桩方法 | 解除依赖步骤 | 回填方式 |
|--------|-------------|---------|
| `WriteStream/WriteCustomStream/StreamIterator/CloseStream` | 5.10 StreamWriterManager | inner.StreamWriterManager() 返回真实实例后实现 |
| `Interact` | 5.9 Interaction | 实现 SimpleAgentInteraction 后实现 |
| `CreateWorkflowSession` | 5.5 WorkflowSession | 实现 WorkflowSession 后实现 |
| `GetEnv/GetEnvs` | 5.12 SessionConfig | Config() 返回真实类型后实现 |
| `GetAgentID/Name/Description` | 后续 card 类型回填 | card 字段类型从 any 变为 *schema.AgentCard |

---

## AgentStateCollection（回填 5.1）

Agent 场景的状态集合，组合 `globalState + agentState` 两层 InMemoryState。

### 结构体

```go
type AgentStateCollection struct {
    globalState *InMemoryState  // 全局状态（跨 Agent 共享）
    agentState  *InMemoryState  // Agent 专属状态
}
```

### 方法与 Python 对齐

| Python 方法 | Go 方法 | 说明 |
|------------|---------|------|
| `get_global(key)` | `GetGlobal(key string) any` | key="" 返回完整全局状态 |
| `update_global(data)` | `UpdateGlobal(data map[string]any)` | 更新全局状态 |
| `get(key)` | `Get(key string) any` | key="" 返回完整 Agent 状态 |
| `update(data)` | `Update(data map[string]any)` | 更新 Agent 状态 |
| `get_state()` | `GetState() map[string]any` | 返回 {global_state, agent_state} |
| `set_state(state)` | `SetState(state map[string]any)` | 从快照恢复 |
| `dump()` | `Dump() map[string]any` | 返回 {global_state, agent_state} |
| — | `GetByPrefix(key, prefix string) any` | 实现 State 接口 |
| — | `GetByTransformer(fn Transformer) any` | 实现 State 接口 |

### State 接口实现

`AgentStateCollection` 实现 `state.State` 接口，`State()` 方法委托到内部 `agentState`。
当 AgentSession 构造时，`state` 字段为 `NewAgentStateCollection()` 实例。

---

## CallbackFramework Session 维度扩展

### 新增事件类型

```go
type SessionCallEventType string

const (
    SessionCreated       SessionCallEventType = "_framework:session_created"
    AgentSessionCreated  SessionCallEventType = "_framework:agent_session_created"
)
```

对应 Python: `openjiuwen/core/runner/callback/events.py` 中的 `SessionEvents`。

### 新增事件数据

```go
type SessionCallEventData struct {
    Event     SessionCallEventType
    SessionID string
    Card      any
    Session   any
    Extra     map[string]any
}
```

### CallbackFramework 扩展

```go
type SessionCallbackFunc func(ctx context.Context, data *SessionCallEventData) any

// CallbackFramework 新增字段：
//   sessionCallbacks map[SessionCallEventType][]SessionCallbackFunc

func (fw *CallbackFramework) OnSession(event SessionCallEventType, fn SessionCallbackFunc)
func (fw *CallbackFramework) OffSession(event SessionCallEventType, fn SessionCallbackFunc)
func (fw *CallbackFramework) TriggerSession(ctx context.Context, data *SessionCallEventData) []any
```

与 LLM/Tool 维度完全对齐：同样的 On/Off/Trigger 三件套，同样的 map[eventType][]callbackFunc 存储结构。

---

## 测试策略

### AgentStateCollection 测试（≥ 90%）

```
TestNewAgentStateCollection                — 构造函数
TestAgentStateCollection_GetGlobal         — 全局状态读取（有值/空 key/不存在）
TestAgentStateCollection_UpdateGlobal      — 全局状态更新
TestAgentStateCollection_Get               — Agent 状态读取（有值/空 key/不存在）
TestAgentStateCollection_Update            — Agent 状态更新
TestAgentStateCollection_GetState          — 导出快照
TestAgentStateCollection_SetState          — 从快照恢复
TestAgentStateCollection_Dump              — 完整导出
TestAgentStateCollection_GetByPrefix       — 前缀查询
TestAgentStateCollection_状态隔离           — globalState 和 agentState 互不干扰
```

### CallbackFramework Session 扩展测试（≥ 90%）

```
TestCallbackFramework_OnSession和TriggerSession       — 注册+触发
TestCallbackFramework_OffSession                      — 注销
TestCallbackFramework_TriggerSession_无回调时返回空      — 防御性
TestCallbackFramework_TriggerSession_Nil上下文          — ctx 或 data 为 nil
TestCallbackFramework_Session事件与LLMTool隔离          — Session 回调不影响 LLM/Tool
```

### 内部层 AgentSession 测试（≥ 85%）

```
TestNewAgentSession                      — 构造函数（默认值 + 选项模式）
TestAgentSession_接口实现                 — 验证满足 BaseSession 接口
TestAgentSession_SessionID               — SessionID 返回正确值
TestAgentSession_默认字段为Nil            — 未传选项时 Config/Tracer/Checkpointer 等返回 nil
TestAgentSession_选项注入                 — 通过选项注入各组件后方法返回正确值
TestAgentSession_ActorManager返回Nil      — ActorManager 始终返回 nil
TestAgentSession_Close返回Nil            — Close 始终返回 nil
```

### 公开层 Session 测试（≥ 80%）

```
TestNewSession_公开层               — 构造函数
TestSession_PreRun                       — 幂等执行，触发回调
TestSession_PreRun_幂等                  — 重复调用只执行一次
TestSession_PostRun                      — 幂等执行，关闭流+提交
TestSession_PostRun_幂等                 — 重复调用只执行一次
TestSession_PostRun_不关闭流             — closeStreamOnPostRun=false 时不关闭流
TestSession_Commit                       — 提交检查点
TestSession_GetSessionID                 — 返回正确 ID
TestSession_UpdateState                  — 委托到 inner.State()
TestSession_GetState                     — 委托到 inner.State()
TestSession_DumpState                    — 委托到 inner.State()
TestSession_桩方法返回Nil                — WriteStream/Interact/CreateWorkflowSession 返回 nil
```

桩方法后续回填时会补充真实测试。

---

## 对应 Python 源码

| Go 组件 | Python 源码 |
|---------|------------|
| 公开层 Session | `openjiuwen/core/session/agent.py` (Session) |
| 内部层 AgentSession | `openjiuwen/core/session/internal/agent.py` (AgentSession) |
| AgentStateCollection | `openjiuwen/core/session/state/agent_state.py` (StateCollection) |
| Session 事件类型 | `openjiuwen/core/runner/callback/events.py` (SessionEvents) |
| CallbackFramework trigger | `openjiuwen/core/runner/callback/utils.py` (trigger) |

---

## IMPLEMENTATION_PLAN.md 回填标记

5.3 完成后需更新：

- 5.3 状态：`☐` → `✅`
- 5.1 StateCollection：在 doc.go 中补充 AgentStateCollection 条目
- BaseSession 中的 `any` 返回值保持不变（等 5.8/5.10/5.11/5.12 各自回填）
