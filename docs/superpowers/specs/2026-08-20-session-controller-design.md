# SessionController 设计方案

> 对应实现计划 5.6：会话控制器
> Python 源码：`openjiuwen/core/session/session_controller/`

## 1. 设计决策汇总

| # | 决策项 | 选择 | 理由 |
|---|--------|------|------|
| 1 | 并发模型 | sync.Mutex + 全同步 | Python async 实质是 Mutex + 同步等待，Go 直接等价 |
| 2 | 持久化方式 | 完全对齐 Python（sessions.json + state.data + downstreams/*.link） | Python ↔ Go 数据兼容 |
| 3 | DataContainer | 接口 + 工厂注册 | 对齐 Python DataContainerFactory 模式 |
| 4 | GlobalSessionController | sync.Once 全局变量 | Go 惯用单例 |
| 5 | Scope/Subject | 接口体系 | 对齐 Python ABC 继承体系 |
| 6 | 回调策略 | AGENT_SESSION_CREATED 现在实现，P2P/PubSub 预留 | AgentTeamEvents 尚未定义（5.13+） |
| 7 | 包结构 | session/controller/ 子包 | 对齐 Python，与 PROJECT_STRUCTURE.md 一致 |
| 8 | 循环依赖 | 定义最小接口 StateAccessor | 避免子包导入父包，Session 隐式满足接口 |

## 2. 文件结构

```
internal/agentcore/session/controller/
├── doc.go                       # 包文档
├── scope.go                     # Scope/MainScope + Subject/DirectSubject/GroupSubject/GroupUserSubject + SessionScope + SessionScopeKey
├── scope_factory.go             # SessionScopeFactory
├── schema.go                    # SessionMeta + ScopeSessionsMeta
├── data_container.go            # DataContainer 接口 + StateAccessor + Permission + SharingPolicy + DataContainerFactory + AgentSessionContainer
├── chain_session.go             # ChainSession
├── session_controller.go        # SessionController
├── global_controller.go         # GlobalSessionController + GlobalSessionConfig + 回调函数
├── paths.go                     # SessionPaths
├── scope_test.go
├── scope_factory_test.go
├── schema_test.go
├── data_container_test.go
├── chain_session_test.go
├── session_controller_test.go
├── global_controller_test.go
└── paths_test.go
```

## 3. 类型对照表

| Python | Go | 文件 |
|--------|-----|------|
| `Scope` (ABC) | `Scope` (interface) | scope.go |
| `MainScope` | `MainScope` (struct) | scope.go |
| `Subject` (ABC) | `Subject` (interface) | scope.go |
| `DirectSubject` | `DirectSubject` (struct) | scope.go |
| `GroupSubject` | `GroupSubject` (struct) | scope.go |
| `GroupUserSubject` | `GroupUserSubject` (struct) | scope.go |
| `SessionScope` | `SessionScope` (struct) | scope.go |
| `SessionScopeKey` | `SessionScopeKey` (struct) | scope.go |
| `SessionScopeFactory` | `SessionScopeFactory` (struct) | scope_factory.go |
| `SessionMeta` | `SessionMeta` (struct) | schema.go |
| `ScopeSessionsMeta` | `ScopeSessionsMeta` (struct) | schema.go |
| `DataContainer` (ABC) | `DataContainer` (interface) | data_container.go |
| `Permission` (Enum) | `Permission` (int iota) | data_container.go |
| `SharingPolicy` | `SharingPolicy` (struct) | data_container.go |
| `DataContainerFactory` | `DataContainerFactory` (struct + 注册表) | data_container.go |
| `AgentSessionContainer` | `AgentSessionContainer` (struct) | data_container.go |
| `ChainSession` | `ChainSession` (struct) | chain_session.go |
| `SessionController` | `SessionController` (struct) | session_controller.go |
| `GlobalSessionController` | `GlobalSessionController` (struct, sync.Once) | global_controller.go |
| `GlobalSessionConfig` | `GlobalSessionConfig` (struct) | global_controller.go |
| `SessionPaths` | `SessionPaths` (struct, 静态方法) | paths.go |

## 4. Scope/Subject 接口体系

```go
// scope.go

// Scope 隔离边界接口，定义数据隔离的基本边界
type Scope interface {
    // String 转为字符串表示，用于序列化和存储键生成
    String() string
}

// MainScope 主域，系统内置默认域，字符串表示为 "main"
type MainScope struct{}

func (m MainScope) String() string { return "main" }

// Subject 会话参与者接口，在 Scope 内进一步细分数据隔离
type Subject interface {
    // String 转为字符串表示
    String() string
}

// DirectSubject 私聊参与者，格式 "direct:{user_id}"
type DirectSubject struct {
    UserID string
}

// GroupSubject 群聊参与者，格式 "group:{group_id}"
type GroupSubject struct {
    GroupID string
}

// GroupUserSubject 群内用户参与者，格式 "group:{gid}:user:{uid}"
type GroupUserSubject struct {
    GroupID string
    UserID  string
}

// SessionScope 会话作用域，由 Scope + 可选 Subject 组成
// 不同 SessionScope 下的数据完全隔离
type SessionScope struct {
    Scope   Scope
    Subject Subject  // 可为 nil
}

// String 格式 "{scope}" 或 "{scope}:{subject}"
func (s SessionScope) String() string

// ParseSessionScope 从字符串解析 SessionScope
func ParseSessionScope(keyStr string) (SessionScope, error)

// SessionScopeKey 全局唯一键，格式 "agent:{agent_id}:{session_scope}"
type SessionScopeKey struct {
    AgentID      string
    SessionScope SessionScope
}

// String 格式 "agent:{agent_id}:{session_scope}"
func (k SessionScopeKey) String() string

// ParseSessionScopeKey 从字符串解析 SessionScopeKey
func ParseSessionScopeKey(keyStr string) (SessionScopeKey, error)
```

**与 Python 的差异：**
- Python 用 `@classmethod from_string` 解析，Go 用包级函数 `ParseSessionScope` / `ParseSessionScopeKey`（Go 接口不适合放工厂方法）
- Python 用 `__eq__` / `__hash__`，Go 让 `SessionScope` 和 `SessionScopeKey` 作为值类型可直接比较
- `Scope` 和 `Subject` 接口比 Python 简化：去掉 `from_string` 类方法

## 5. SessionScopeFactory

```go
// scope_factory.go

// SessionScopeFactory 会话作用域工厂，提供创建常见 SessionScope 实例的静态方法
type SessionScopeFactory struct{}

func (SessionScopeFactory) CreateMain() SessionScope
func (SessionScopeFactory) CreateDirect(userID string) SessionScope
func (SessionScopeFactory) CreateGroup(groupID string) SessionScope
func (SessionScopeFactory) CreateGroupUser(groupID, userID string) SessionScope
func (SessionScopeFactory) CreateCustom(scope Scope, subject Subject) SessionScope
func (SessionScopeFactory) FromString(keyStr string) (SessionScope, error)
```

## 6. Schema（元数据）

```go
// schema.go

// SessionMeta 单会话元数据
type SessionMeta struct {
    SessionID         string  `json:"session_id"`
    CreatedAt         float64 `json:"created_at"`
    UpdatedAt         float64 `json:"updated_at"`
    Version           int     `json:"version"`
    IsActive          bool    `json:"is_active"`
    DataContainerType string  `json:"data_container_type"`
}

// CreateNewSessionMeta 创建新的会话元数据
func CreateNewSessionMeta(sessionID string, dataContainerType string) SessionMeta

// UpdateTimestamp 更新时间戳
func (m *SessionMeta) UpdateTimestamp()

// IncrementVersion 递增版本号
func (m *SessionMeta) IncrementVersion()

// ScopeSessionsMeta 单 Scope 下所有会话的元数据集合
type ScopeSessionsMeta struct {
    SessionScopeKey string        `json:"session_scope_key"`
    ActiveSession   string        `json:"active_session"`  // 当前活跃 session_id，可为空
    Sessions        []SessionMeta `json:"sessions"`         // 按更新时间降序
}

func (m *ScopeSessionsMeta) GetSession(sessionID string) *SessionMeta
func (m *ScopeSessionsMeta) AddSession(meta SessionMeta)
func (m *ScopeSessionsMeta) RemoveSession(sessionID string) *SessionMeta
func (m *ScopeSessionsMeta) ActivateSession(sessionID string) bool
func (m *ScopeSessionsMeta) DeactivateAllSessions()
func (m *ScopeSessionsMeta) SortSessions()
func (m *ScopeSessionsMeta) GetActiveSession() *SessionMeta
func (m *ScopeSessionsMeta) UpdateSessionTimestamp(sessionID string) bool
func (m *ScopeSessionsMeta) IncrementSessionVersion(sessionID string) bool
```

**与 Python 的差异：**
- `SessionMeta.from_dict` / `to_dict` → Go 用 JSON struct tag + `encoding/json` 直接序列化
- `ScopeSessionsMeta.Sessions` 排序用 `sort.Slice`

## 7. DataContainer + 工厂

### 7.1 循环依赖解决方案

`controller` 子包不能导入 `session` 父包。Python 通过延迟导入规避，Go 需要接口解耦。

`AgentSessionContainer` 只用了 Session 的 4 个方法，定义最小接口 `StateAccessor`：

```go
// StateAccessor 会话状态访问接口，AgentSessionContainer 通过此接口
// 委托给 Session 实例，避免 controller 子包反向导入 session 父包。
//
// *session.Session 天然实现此接口（Go 隐式接口满足）。
type StateAccessor interface {
    // UpdateState 更新状态数据
    UpdateState(data map[string]any)
    // GetState 根据 key 获取状态值
    GetState(key state.StateKey) (any, error)
    // DumpState 导出完整状态快照
    DumpState() map[string]any
    // PreRun 预运行（load 时调用）
    PreRun(ctx context.Context, inputs ...map[string]any) error
}
```

### 7.2 DataContainer 接口

```go
// DataContainer 数据容器接口，封装会话核心业务数据
type DataContainer interface {
    // Get 获取数据（key 可选过滤）
    Get(key any) map[string]any
    // Update 原子更新数据
    Update(data map[string]any) bool
    // Dump 序列化为可持久化格式
    Dump() (any, error)
}

// ContainerLoader 从序列化数据重建 DataContainer 的函数类型
// 对应 Python DataContainer.load 类方法
type ContainerLoader func(agentID, sessionID string, serialized any) (DataContainer, error)

// ContainerOption DataContainer 创建选项
type ContainerOption func(DataContainer)
```

**与 Python 的差异：**
- Python `DataContainer.load` 是 `@classmethod async`，Go 改为 `ContainerLoader` 函数类型（因为 Go 接口不含静态方法）
- Python `dump()` 是 `async`，Go 的 `Dump()` 是同步的（Go 文件 I/O 天然同步）

### 7.3 Permission + SharingPolicy

```go
// Permission 数据访问权限枚举
type Permission int

const (
    // PermissionRead 只读权限
    PermissionRead Permission = iota + 1
)

// SharingPolicy 下游会话共享策略
type SharingPolicy struct {
    // Permission 授予的权限级别
    Permission Permission
    // FieldScopes 允许访问的字段名集合，nil 表示全部字段可访问
    FieldScopes map[string]struct{}
}
```

### 7.4 DataContainerFactory

```go
// DataContainerFactory 数据容器工厂（注册表模式）
type DataContainerFactory struct{}

// 默认数据容器类型
const DefaultDataContainerType = "agent"

var (
    factoryOnce    sync.Once
    factoryInstance *DataContainerFactory
)

// GetFactory 获取全局工厂单例
func GetFactory() *DataContainerFactory

func (f *DataContainerFactory) Register(containerType string, loader ContainerLoader, constructor func(opts ...ContainerOption) DataContainer)
func (f *DataContainerFactory) Create(containerType string, opts ...ContainerOption) (DataContainer, error)
func (f *DataContainerFactory) Load(containerType, agentID, sessionID string, serialized any) (DataContainer, error)
func (f *DataContainerFactory) Has(containerType string) bool
func (f *DataContainerFactory) ListTypes() []string
```

### 7.5 AgentSessionContainer

```go
// AgentSessionContainer 默认数据容器实现，委托给 StateAccessor
type AgentSessionContainer struct {
    // session 被委托的会话实例，初始为 nil
    // 通过 SetSession 注入，或通过 Load 创建
    session StateAccessor
}

func NewAgentSessionContainer() *AgentSessionContainer
func (c *AgentSessionContainer) Get(key any) map[string]any
func (c *AgentSessionContainer) Update(data map[string]any) bool
func (c *AgentSessionContainer) Dump() (any, error)
func (c *AgentSessionContainer) SetSession(session StateAccessor)

// LoadAgentSessionContainer 从序列化数据重建 AgentSessionContainer
// 对应 Python AgentSessionContainer.load()
// ⤵️ 后续回填：需要 create_agent_session 等价函数后完善
func LoadAgentSessionContainer(agentID, sessionID string, serialized any) (DataContainer, error)
```

### 7.6 工厂注册（init 时）

```go
func init() {
    GetFactory().Register(
        DefaultDataContainerType,
        LoadAgentSessionContainer,
        func(opts ...ContainerOption) DataContainer {
            return NewAgentSessionContainer()
        },
    )
}
```

## 8. ChainSession

```go
// chain_session.go

// ChainSession 链式会话，持有 DataContainer + 下游关系 + 持久化能力
type ChainSession struct {
    mu                sync.Mutex
    AgentID           string
    SessionScope      SessionScope
    SessionID         string
    DataContainer     DataContainer
    sessionDir        string
    dataContainerType string
    downstreamPolicies map[[2]string]SharingPolicy  // key: [agentID, sessionID]
    createdAt         float64
    updatedAt         float64
    version           int
    isActive          bool
}

// 下游关系 key 辅助
func downstreamKey(agentID, sessionID string) [2]string {
    return [2]string{agentID, sessionID}
}

// 持久化：
func (cs *ChainSession) Load() error
func (cs *ChainSession) Flush() error

// 下游关系管理：
func (cs *ChainSession) AddDownstream(targetAgent, targetSession string, policy SharingPolicy)
func (cs *ChainSession) RemoveDownstream(targetAgent, targetSession string)
func (cs *ChainSession) HasDownstream(targetAgent, targetSession string) bool
func (cs *ChainSession) GetDownstreams() map[[2]string]SharingPolicy
func (cs *ChainSession) GetDownstreamPolicy(targetAgent, targetSession string) *SharingPolicy
func (cs *ChainSession) RemoveAllDownstreams()

// 数据访问：
func (cs *ChainSession) GetData() map[string]any
func (cs *ChainSession) UpdateData(data map[string]any) bool
func (cs *ChainSession) CanSee(targetAgent, targetSession string) bool

// 元数据：
func (cs *ChainSession) ToSessionMeta() SessionMeta
func (cs *ChainSession) UpdateFromMeta(meta SessionMeta)
func (cs *ChainSession) SessionKey() SessionScopeKey
func (cs *ChainSession) CreatedAt() float64
func (cs *ChainSession) UpdatedAt() float64
func (cs *ChainSession) Version() int
func (cs *ChainSession) IsActive() bool
func (cs *ChainSession) SetIsActive(value bool)
```

**与 Python 的差异：**
- 全部方法同步，`sync.Mutex` 替代 `asyncio.Lock`
- Python 用 `dict[tuple[str, str], SharingPolicy]`，Go 用 `map[[2]string]SharingPolicy`
- `Flush()` 中并行刷多个下游用 `errgroup`，等价于 Python 的 `asyncio.gather`
- Python `ChainSession` 是泛型 `Generic[T]`，Go 不需要泛型（DataContainer 接口已足够抽象）

**Flush 策略（对齐 Python）：**
1. 更新 updatedAt 时间戳
2. 调用 `DataContainer.Dump()` 写入 `state.data`
3. 为每个下游关系写 `{target_agent}_{target_session}.link` 文件
4. 清理已删除的下游关系：先标记 `removed: true`，再删除 .link 文件（崩溃恢复安全）

## 9. SessionController

```go
// session_controller.go

// SessionController 单 Agent 会话管理器
type SessionController struct {
    mu               sync.Mutex
    AgentID          string
    rootPath         string
    BasePath         string
    dataContainerType string
    SessionCache     map[string]*ChainSession           // sessionID → ChainSession
    MetaMap          map[SessionScope]*ScopeSessionsMeta // SessionScope → 元数据
}

func NewSessionController(agentID string, basePath string, dataContainerType ...string) *SessionController

// 持久化：
func (sc *SessionController) Flush() error
func (sc *SessionController) FlushSession(sessionID string) error
func (sc *SessionController) FlushScope(sessionScope SessionScope) error
func (sc *SessionController) Load(loadActiveOnly bool) error
func (sc *SessionController) LoadScope(sessionScope SessionScope, loadActiveOnly bool) error

// 会话管理：
func (sc *SessionController) CreateIfNotExists(sessionScope SessionScope, sessionID string, opts ...ContainerOption) (bool, *ChainSession, error)
func (sc *SessionController) GetScopeActiveSession(sessionScope SessionScope) *ChainSession
func (sc *SessionController) GetScopeSessions(sessionScope SessionScope) []*ChainSession
func (sc *SessionController) ActivateSession(sessionID string) error
func (sc *SessionController) GetScopeMeta(sessionScope SessionScope) ScopeSessionsMeta
func (sc *SessionController) ListMetas() map[SessionScope]ScopeSessionsMeta

// 清理：
func (sc *SessionController) CleanupScopeInactiveSessions(sessionScope SessionScope) ([]CleanupResult, error)
func (sc *SessionController) RemoveSession(sessionID string, sessionScope *SessionScope) []RemoveResult
func (sc *SessionController) RemoveScopeSessions(sessionScope SessionScope) []SessionMeta
func (sc *SessionController) RemoveAll()

// 内部方法：
func (sc *SessionController) loadSession(sessionScope SessionScope, sessionID string, opts ...ContainerOption) error
func (sc *SessionController) writeMetaFile() error
```

**辅助类型：**

```go
// CleanupResult 清理结果
type CleanupResult struct {
    SessionScope SessionScope
    Sessions     []SessionMeta
}

// RemoveResult 删除结果
type RemoveResult struct {
    SessionScope SessionScope
    SessionMeta  SessionMeta
}
```

**Flush 多 session 并行（对齐 Python asyncio.gather）：**

```go
func (sc *SessionController) Flush() error {
    sc.mu.Lock()
    // 收集需要 flush 的 session 列表
    sessions := make([]*ChainSession, 0, len(sc.SessionCache))
    for _, s := range sc.SessionCache {
        sessions = append(sessions, s)
    }
    sc.mu.Unlock()

    // 并行 flush 所有 session（等价于 Python asyncio.gather）
    g, _ := errgroup.WithContext(context.Background())
    for _, s := range sessions {
        s := s
        g.Go(func() error {
            return s.Flush()
        })
    }
    if err := g.Wait(); err != nil {
        return err
    }

    // 写元数据文件
    sc.mu.Lock()
    defer sc.mu.Unlock()
    return sc.writeMetaFile()
}
```

## 10. GlobalSessionController

```go
// global_controller.go

// GlobalSessionConfig 全局会话控制器配置
type GlobalSessionConfig struct {
    BasePath string
}

// GlobalSessionController 全局会话控制器（sync.Once 单例）
// 统一入口，管理所有 Agent SessionController 实例，提供跨 Agent 批量操作
type GlobalSessionController struct {
    mu                sync.Mutex
    BasePath          string
    Controllers       map[string]*SessionController  // agentID → SessionController
    dataContainerType string
}

var (
    globalController     *GlobalSessionController
    globalControllerOnce sync.Once
)

// GetGlobalSessionController 获取全局会话控制器单例
func GetGlobalSessionController() *GlobalSessionController

// 配置：
func (g *GlobalSessionController) SetConfig(config GlobalSessionConfig)

// 批量加载：
func (g *GlobalSessionController) LoadAgent(agentID string, loadActiveOnly bool) error
func (g *GlobalSessionController) LoadScope(sessionScope SessionScope, loadActiveOnly bool) error
func (g *GlobalSessionController) LoadAll(loadActiveOnly bool) error

// 批量刷盘：
func (g *GlobalSessionController) FlushAgent(agentID string) error
func (g *GlobalSessionController) FlushSession(sessionID string) error
func (g *GlobalSessionController) FlushScope(sessionScope SessionScope) error
func (g *GlobalSessionController) FlushAll() error

// Agent 管理：
func (g *GlobalSessionController) GetAgent(agentID string) *SessionController
func (g *GlobalSessionController) CreateIfNotExistsAgent(agentID string) (bool, *SessionController, error)
func (g *GlobalSessionController) RemoveAgent(agentID string) (bool, error)
func (g *GlobalSessionController) RemoveAll()

// 批量清理：
func (g *GlobalSessionController) CleanupAgentInactiveSessions(agentID string) (map[string][]CleanupResult, error)
func (g *GlobalSessionController) CleanupScopeInactiveSessions(sessionScope SessionScope) map[string][]SessionMeta
func (g *GlobalSessionController) CleanupOrphanFiles(agentID string, dryRun bool) map[string][]string
```

### 便捷方法（包级函数，对齐 Python 静态方法）

```go
func CreateDirectSession(agentID, userID, sessionID string, opts ...ContainerOption) (bool, *ChainSession, error)
func CreateGroupSession(agentID, groupID, sessionID string, opts ...ContainerOption) (bool, *ChainSession, error)
func GetDirectSessionData(agentID, userID string) map[string]any
func UpdateDirectSessionData(agentID, userID string, data map[string]any) bool
func AddDirectSessionDownstream(callerAgentID, callerUserID, targetAgentID, targetUserID string, policy SharingPolicy) bool
func CleanupUserSessions(agentID, userID string) ([]CleanupResult, error)
func GetUserSessionHistory(agentID, userID string) []*ChainSession
func FlushUserSession(agentID, userID string) error
func VisualizeCallChain(agentID, sessionID string, depth int) string
```

## 11. SessionPaths

```go
// paths.go

// SessionPaths 会话存储路径工具
type SessionPaths struct{}

func (SessionPaths) AgentDir(basePath, agentID string) string
func (SessionPaths) SessionsDir(basePath, agentID string) string
func (SessionPaths) MetaFile(basePath, agentID string) string
func (SessionPaths) SessionDir(basePath, agentID, sessionID string) string
func (SessionPaths) StateFile(sessionDir string) string
func (SessionPaths) DownstreamsDir(sessionDir string) string
func (SessionPaths) LinkFile(sessionDir, targetAgent, targetSession string) string
```

**磁盘结构（完全对齐 Python）：**

```
{basePath}/
└── {agentID}/
    └── sessions/
        ├── sessions.json                     # 元数据文件
        ├── {sessionID1}/
        │   ├── state.data                   # 会话状态数据
        │   └── downstreams/
        │       ├── {agent}_{session}.link   # 下游关系
        │       └── ...
        └── {sessionID2}/
            └── ...
```

## 12. 回调集成

### 12.1 AGENT_SESSION_CREATED（现在实现）

```go
// global_controller.go

func init() {
    // 注册 AGENT_SESSION_CREATED 回调
    callback.GetCallbackFramework().OnSession(callback.AgentSessionCreated, onAgentSessionCreated)
}

// onAgentSessionCreated AGENT_SESSION_CREATED 回调：
// 将 ChainSession 的 DataContainer.session 注入真实 Session 实例
func onAgentSessionCreated(ctx context.Context, data *callback.SessionCallEventData) any {
    if data.SessionID == "" || data.Card == nil || data.Session == nil {
        return nil
    }

    // 从 Card 提取 AgentID
    card, ok := data.Card.(*schema.AgentCard)
    if !ok {
        return nil
    }

    instance := GetGlobalSessionController()
    controller := instance.GetAgent(card.ID)
    if controller == nil {
        return nil
    }

    chainSession := controller.SessionCache[data.SessionID]
    if chainSession == nil {
        return nil
    }

    // 将 DataContainer 转为 AgentSessionContainer，注入 StateAccessor
    if asc, ok := chainSession.DataContainer.(*AgentSessionContainer); ok {
        if sa, ok := data.Session.(StateAccessor); ok {
            asc.SetSession(sa)
        }
    }
    return nil
}
```

### 12.2 P2P/PubSub 回调（预留）

```go
// ⤵️ 5.13+ 回填：等 AgentTeamEvents 定义后注册
// callback.GetCallbackFramework().OnTeamEvent(callback.AgentP2PReceived, onAgentP2PReceived)
// callback.GetCallbackFramework().OnTeamEvent(callback.AgentPubsubReceived, onAgentPubsubReceived)
```

## 13. 5.1 回填

5.1 State 体系中预留了 `SessionController scope 待 5.6 回填`。5.6 完成后需要：

1. 确认 `state/key.go` 是否需要补充 `SessionControllerScope` 相关的 StateKey
2. 在 `session/agent.go` 的 `Session` 结构体中添加 `SessionController` 关联（如果需要）

## 14. 测试策略

- **scope_test.go**：Scope/Subject 的 String/Parse 方法、等值比较
- **scope_factory_test.go**：工厂方法创建的正确性
- **schema_test.go**：SessionMeta/ScopeSessionsMeta 的 CRUD、序列化/反序列化
- **data_container_test.go**：AgentSessionContainer 委托、工厂注册/创建/加载
- **chain_session_test.go**：下游关系管理、Load/Flush 持久化、CanSee 可见性
- **session_controller_test.go**：CreateIfNotExists、ActivateSession、Remove、Cleanup（使用 t.TempDir()）
- **global_controller_test.go**：单例行为、批量操作、便捷方法、回调集成
- **paths_test.go**：路径拼接正确性

所有文件 I/O 测试使用 `t.TempDir()`，不依赖外部环境。
