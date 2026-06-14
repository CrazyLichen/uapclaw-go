# SessionController 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 5.6 SessionController，包含 Scope/Subject 体系、DataContainer 工厂、ChainSession 链式会话、SessionController 单 Agent 管理器、GlobalSessionController 全局单例及回调集成。

**Architecture:** 完全对齐 Python `session_controller/` 包结构，使用 sync.Mutex 全同步并发模型，持久化对齐 Python 三级文件格式（sessions.json + state.data + downstreams/*.link）。通过 StateAccessor 最小接口解决 controller 子包不能导入 session 父包的循环依赖问题。

**Tech Stack:** Go 1.22+, sync.Mutex, sync.Once, encoding/json, os/filepath, errgroup, session/state 子包, runner/callback 子包

**Design Spec:** `docs/superpowers/specs/2026-08-20-session-controller-design.md`

**Python 源码参考:** `/home/opensource/agent-core/openjiuwen/core/session/session_controller/`

---

## 文件结构

```
internal/agentcore/session/controller/
├── doc.go                       # 包文档
├── scope.go                     # Scope/MainScope + Subject/DirectSubject/GroupSubject/GroupUserSubject + SessionScope + SessionScopeKey + ParseSessionScope + ParseSessionScopeKey
├── scope_factory.go             # SessionScopeFactory
├── schema.go                    # SessionMeta + ScopeSessionsMeta + CleanupResult + RemoveResult
├── data_container.go            # DataContainer + StateAccessor + Permission + SharingPolicy + DataContainerFactory + AgentSessionContainer
├── chain_session.go             # ChainSession
├── session_controller.go        # SessionController
├── global_controller.go         # GlobalSessionController + GlobalSessionConfig + 便捷方法 + 回调函数
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

---

### Task 1: paths.go — 会话路径工具

**Files:**
- Create: `internal/agentcore/session/controller/paths.go`
- Test: `internal/agentcore/session/controller/paths_test.go`

- [ ] **Step 1: 创建 paths.go**

```go
package controller

import "path/filepath"

// ──────────────────────────── 结构体 ────────────────────────────

// SessionPaths 会话存储路径工具，提供静态方法构建各种存储路径。
// 对应 Python: openjiuwen/core/session/session_controller/utils.py (SessionPaths)
type SessionPaths struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// AgentDir 获取指定 Agent 的根目录路径
func (SessionPaths) AgentDir(basePath, agentID string) string {
	return filepath.Join(basePath, agentID)
}

// SessionsDir 获取指定 Agent 的 sessions 目录路径
func (SessionPaths) SessionsDir(basePath, agentID string) string {
	return filepath.Join(basePath, agentID, "sessions")
}

// MetaFile 获取指定 Agent 的 sessions.json 元数据文件路径
func (SessionPaths) MetaFile(basePath, agentID string) string {
	return filepath.Join(basePath, agentID, "sessions", "sessions.json")
}

// SessionDir 获取指定会话的目录路径
func (SessionPaths) SessionDir(basePath, agentID, sessionID string) string {
	return filepath.Join(basePath, agentID, "sessions", sessionID)
}

// StateFile 获取指定会话目录下的 state.data 文件路径
func (SessionPaths) StateFile(sessionDir string) string {
	return filepath.Join(sessionDir, "state.data")
}

// DownstreamsDir 获取指定会话目录下的 downstreams 目录路径
func (SessionPaths) DownstreamsDir(sessionDir string) string {
	return filepath.Join(sessionDir, "downstreams")
}

// LinkFile 获取指定下游关系的 .link 文件路径
func (SessionPaths) LinkFile(sessionDir, targetAgent, targetSession string) string {
	return filepath.Join(sessionDir, "downstreams", targetAgent+"_"+targetSession+".link")
}
```

- [ ] **Step 2: 创建 paths_test.go**

```go
package controller

import "testing"

func TestSessionPaths_AgentDir(t *testing.T) {
	p := SessionPaths{}
	got := p.AgentDir("/data", "agent1")
	want := "/data/agent1"
	if got != want {
		t.Errorf("AgentDir() = %q, want %q", got, want)
	}
}

func TestSessionPaths_SessionsDir(t *testing.T) {
	p := SessionPaths{}
	got := p.SessionsDir("/data", "agent1")
	want := "/data/agent1/sessions"
	if got != want {
		t.Errorf("SessionsDir() = %q, want %q", got, want)
	}
}

func TestSessionPaths_MetaFile(t *testing.T) {
	p := SessionPaths{}
	got := p.MetaFile("/data", "agent1")
	want := "/data/agent1/sessions/sessions.json"
	if got != want {
		t.Errorf("MetaFile() = %q, want %q", got, want)
	}
}

func TestSessionPaths_SessionDir(t *testing.T) {
	p := SessionPaths{}
	got := p.SessionDir("/data", "agent1", "sess1")
	want := "/data/agent1/sessions/sess1"
	if got != want {
		t.Errorf("SessionDir() = %q, want %q", got, want)
	}
}

func TestSessionPaths_StateFile(t *testing.T) {
	p := SessionPaths{}
	got := p.StateFile("/data/agent1/sessions/sess1")
	want := "/data/agent1/sessions/sess1/state.data"
	if got != want {
		t.Errorf("StateFile() = %q, want %q", got, want)
	}
}

func TestSessionPaths_DownstreamsDir(t *testing.T) {
	p := SessionPaths{}
	got := p.DownstreamsDir("/data/agent1/sessions/sess1")
	want := "/data/agent1/sessions/sess1/downstreams"
	if got != want {
		t.Errorf("DownstreamsDir() = %q, want %q", got, want)
	}
}

func TestSessionPaths_LinkFile(t *testing.T) {
	p := SessionPaths{}
	got := p.LinkFile("/data/agent1/sessions/sess1", "agent2", "sess2")
	want := "/data/agent1/sessions/sess1/downstreams/agent2_sess2.link"
	if got != want {
		t.Errorf("LinkFile() = %q, want %q", got, want)
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/controller/... -run TestSessionPaths -v`
Expected: PASS（需要先创建目录和 doc.go）

- [ ] **Step 4: 创建 doc.go 占位**

```go
// Package controller 提供会话控制器，管理会话生命周期、作用域隔离、数据持久化和跨 Agent 批量操作。
//
// 本包实现链式会话（ChainSession）和两级控制器（SessionController 管理单 Agent、
// GlobalSessionController 管理全局），支持 Scope/Subject 维度的数据隔离、
// 下游会话单向可见性、磁盘持久化（sessions.json + state.data + downstreams/*.link）。
//
// 文件目录：
//
//	controller/
//	├── doc.go                # 包文档
//	├── scope.go              # Scope/Subject 接口体系 + SessionScope + SessionScopeKey
//	├── scope_factory.go      # SessionScopeFactory 工厂
//	├── schema.go             # SessionMeta + ScopeSessionsMeta 元数据
//	├── data_container.go     # DataContainer 接口 + 工厂 + AgentSessionContainer
//	├── chain_session.go      # ChainSession 链式会话
//	├── session_controller.go # SessionController 单 Agent 管理器
//	├── global_controller.go  # GlobalSessionController 全局单例 + 便捷方法 + 回调
//	└── paths.go              # SessionPaths 路径工具
//
// 对应 Python 代码：openjiuwen/core/session/session_controller/
//
// 核心类型/接口索引：
//
//	Scope                — 隔离边界接口
//	MainScope            — 主域
//	Subject              — 会话参与者接口
//	DirectSubject        — 私聊参与者
//	GroupSubject         — 群聊参与者
//	GroupUserSubject     — 群内用户参与者
//	SessionScope         — 会话作用域（Scope + Subject）
//	SessionScopeKey      — 全局唯一键（agent:{id}:{scope}）
//	SessionScopeFactory  — 作用域工厂
//	SessionMeta          — 单会话元数据
//	ScopeSessionsMeta    — 单 Scope 下会话元数据集合
//	DataContainer        — 数据容器接口
//	StateAccessor        — 会话状态访问最小接口
//	Permission           — 访问权限枚举
//	SharingPolicy        — 下游共享策略
//	DataContainerFactory — 数据容器工厂
//	AgentSessionContainer — 默认数据容器（委托 StateAccessor）
//	ChainSession         — 链式会话
//	SessionController    — 单 Agent 会话管理器
//	GlobalSessionController — 全局会话控制器单例
//	SessionPaths         — 路径工具
package controller
```

先创建目录，再写入 doc.go 和 paths.go，然后运行测试。

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/session/controller/
git commit -m "feat(controller): 添加 paths.go 路径工具和 doc.go 包文档"
```

---

### Task 2: scope.go — Scope/Subject 接口体系

**Files:**
- Create: `internal/agentcore/session/controller/scope.go`
- Test: `internal/agentcore/session/controller/scope_test.go`

- [ ] **Step 1: 创建 scope.go**

按设计文档实现 Scope/MainScope/Subject/DirectSubject/GroupSubject/GroupUserSubject/SessionScope/SessionScopeKey + ParseSessionScope + ParseSessionScopeKey。关键要点：
- `Scope` 和 `Subject` 是只有 `String() string` 的接口
- `MainScope` 是空结构体，`String()` 返回 `"main"`
- `DirectSubject.String()` 返回 `"direct:{UserID}"`
- `GroupSubject.String()` 返回 `"group:{GroupID}"`
- `GroupUserSubject.String()` 返回 `"group:{GroupID}:user:{UserID}"`
- `SessionScope.String()` 无 Subject 时返回 `scope.String()`，有 Subject 时返回 `scope.String() + ":" + subject.String()`
- `ParseSessionScope(keyStr)` 按 `:` 分割第一段为 scope，剩余部分为 subject；scope 为 `"main"` 时创建 `MainScope{}`，subject 按 `direct:`/`group:`/`group:...:user:` 前缀解析
- `SessionScopeKey.String()` 返回 `"agent:{AgentID}:{SessionScope}"`
- `ParseSessionScopeKey(keyStr)` 必须以 `"agent:"` 开头，解析 agentID 和 SessionScope

Python 对应源码：`/home/opensource/agent-core/openjiuwen/core/session/session_controller/scope.py`

- [ ] **Step 2: 创建 scope_test.go**

测试项：
- `MainScope.String()` == `"main"`
- `DirectSubject{UserID:"u1"}.String()` == `"direct:u1"`
- `GroupSubject{GroupID:"g1"}.String()` == `"group:g1"`
- `GroupUserSubject{GroupID:"g1",UserID:"u1"}.String()` == `"group:g1:user:u1"`
- `SessionScope{Scope:MainScope{}}.String()` == `"main"`
- `SessionScope{Scope:MainScope{},Subject:DirectSubject{UserID:"u1"}}.String()` == `"main:direct:u1"`
- `ParseSessionScope("main")` 返回 `{Scope:MainScope{}}`
- `ParseSessionScope("main:direct:u1")` 返回含 DirectSubject 的 SessionScope
- `ParseSessionScope("main:group:g1")` 返回含 GroupSubject 的 SessionScope
- `ParseSessionScope("main:group:g1:user:u1")` 返回含 GroupUserSubject 的 SessionScope
- `ParseSessionScope("unknown")` 返回错误
- `ParseSessionScopeKey("agent:a1:main:direct:u1")` 返回正确的 SessionScopeKey
- `ParseSessionScopeKey("invalid")` 返回错误
- SessionScope 和 SessionScopeKey 的等值比较

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/controller/... -run TestScope -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/controller/scope.go internal/agentcore/session/controller/scope_test.go
git commit -m "feat(controller): 实现 Scope/Subject 接口体系和 SessionScope/SessionScopeKey 解析"
```

---

### Task 3: scope_factory.go — 作用域工厂

**Files:**
- Create: `internal/agentcore/session/controller/scope_factory.go`
- Test: `internal/agentcore/session/controller/scope_factory_test.go`

- [ ] **Step 1: 创建 scope_factory.go**

实现 `SessionScopeFactory` 结构体及方法：CreateMain/CreateDirect/CreateGroup/CreateGroupUser/CreateCustom/FromString。全部是值接收者方法，无需实例状态。

Python 对应源码：`/home/opensource/agent-core/openjiuwen/core/session/session_controller/scope_factory.py`

- [ ] **Step 2: 创建 scope_factory_test.go**

测试项：
- `CreateMain()` → SessionScope.String() == `"main"`
- `CreateDirect("u1")` → SessionScope.String() == `"main:direct:u1"`
- `CreateGroup("g1")` → SessionScope.String() == `"main:group:g1"`
- `CreateGroupUser("g1","u1")` → SessionScope.String() == `"main:group:g1:user:u1"`
- `FromString("main:direct:u1")` 等价于 `CreateDirect("u1")`

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/controller/... -run TestScopeFactory -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/controller/scope_factory.go internal/agentcore/session/controller/scope_factory_test.go
git commit -m "feat(controller): 实现 SessionScopeFactory 工厂方法"
```

---

### Task 4: schema.go — 元数据

**Files:**
- Create: `internal/agentcore/session/controller/schema.go`
- Test: `internal/agentcore/session/controller/schema_test.go`

- [ ] **Step 1: 创建 schema.go**

实现 `SessionMeta` 和 `ScopeSessionsMeta`，以及辅助类型 `CleanupResult`/`RemoveResult`。

关键要点：
- `SessionMeta` 字段：SessionID/CreatedAt/UpdatedAt/Version/IsActive/DataContainerType，带 JSON tag
- `CreateNewSessionMeta(sessionID, dataContainerType)` 工厂函数，设置当前时间戳
- `SessionMeta.UpdateTimestamp()` / `IncrementVersion()`
- `ScopeSessionsMeta` 字段：SessionScopeKey/ActiveSession/Sessions，带 JSON tag
- `ScopeSessionsMeta` 方法：GetSession/AddSession/RemoveSession/ActivateSession/DeactivateAllSessions/SortSessions/GetActiveSession/UpdateSessionTimestamp/IncrementSessionVersion
- `AddSession` 时若 is_active 则先 DeactivateAllSessions
- `SortSessions` 按 UpdatedAt 降序
- `CleanupResult`/`RemoveResult` 辅助类型

Python 对应源码：`/home/opensource/agent-core/openjiuwen/core/session/session_controller/schema.py`

- [ ] **Step 2: 创建 schema_test.go**

测试项：
- `CreateNewSessionMeta` 设置正确的时间戳和版本
- `SessionMeta.UpdateTimestamp` 更新 UpdatedAt
- `SessionMeta.IncrementVersion` 递增 Version
- `ScopeSessionsMeta.AddSession` 添加会话
- `ScopeSessionsMeta.AddSession` 激活会话时自动去激活其他会话
- `ScopeSessionsMeta.RemoveSession` 删除会话
- `ScopeSessionsMeta.ActivateSession` 激活指定会话
- `ScopeSessionsMeta.DeactivateAllSessions` 全部去激活
- `ScopeSessionsMeta.SortSessions` 按更新时间降序排列
- `ScopeSessionsMeta.GetActiveSession` 获取活跃会话
- `ScopeSessionsMeta.UpdateSessionTimestamp` 更新指定会话时间戳
- `ScopeSessionsMeta.IncrementSessionVersion` 递增指定会话版本

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/controller/... -run TestSchema -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/controller/schema.go internal/agentcore/session/controller/schema_test.go
git commit -m "feat(controller): 实现 SessionMeta 和 ScopeSessionsMeta 元数据"
```

---

### Task 5: data_container.go — 数据容器接口与工厂

**Files:**
- Create: `internal/agentcore/session/controller/data_container.go`
- Test: `internal/agentcore/session/controller/data_container_test.go`

- [ ] **Step 1: 创建 data_container.go**

实现：
- `DataContainer` 接口：Get/Update/Dump
- `StateAccessor` 接口：UpdateState/GetState/DumpState/PreRun（解决循环依赖的最小接口）
- `ContainerLoader` 函数类型
- `ContainerOption` 函数类型
- `Permission` 枚举（PermissionRead = 1）
- `SharingPolicy` 结构体：Permission + FieldScopes(map[string]struct{})
- `DataContainerFactory` 工厂单例：Register/Create/Load/Has/ListTypes
- `AgentSessionContainer`：持有 StateAccessor，实现 DataContainer 接口
- `LoadAgentSessionContainer` 函数（当前简化实现，⤵️ 后续回填 create_agent_session）
- `DefaultDataContainerType` 常量 = `"agent"`
- `init()` 中注册默认 AgentSessionContainer

Python 对应源码：`/home/opensource/agent-core/openjiuwen/core/session/session_controller/data_container.py`

关键要点：
- `DataContainerFactory` 用 `sync.Once` 保证单例
- 内部注册表 `map[string]factoryEntry`，每个 entry 包含 loader 和 constructor
- `AgentSessionContainer.Get(key)` 委托给 `StateAccessor.DumpState()`（key==nil）或 `StateAccessor.GetState(key)`
- `AgentSessionContainer.Update(data)` 委托给 `StateAccessor.UpdateState(data)`
- `AgentSessionContainer.Dump()` 返回空 map（对齐 Python `return {}`）
- `LoadAgentSessionContainer` 当前返回空的 AgentSessionContainer（⤵️ 后续回填）

- [ ] **Step 2: 创建 data_container_test.go**

测试项：
- `DataContainerFactory.Register` + `Create` 创建 AgentSessionContainer
- `DataContainerFactory.Has` 检查类型是否注册
- `DataContainerFactory.ListTypes` 返回已注册类型列表
- `DataContainerFactory.Create` 未注册类型返回错误
- `AgentSessionContainer.Update` 委托给 mock StateAccessor
- `AgentSessionContainer.Get` 委托给 mock StateAccessor
- `AgentSessionContainer.Dump` 返回空 map
- `AgentSessionContainer.SetSession` 注入 StateAccessor
- `Permission` 枚举值验证
- `SharingPolicy` 字段访问

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/controller/... -run TestDataContainer -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/controller/data_container.go internal/agentcore/session/controller/data_container_test.go
git commit -m "feat(controller): 实现 DataContainer 接口、StateAccessor、工厂和 AgentSessionContainer"
```

---

### Task 6: chain_session.go — 链式会话

**Files:**
- Create: `internal/agentcore/session/controller/chain_session.go`
- Test: `internal/agentcore/session/controller/chain_session_test.go`

- [ ] **Step 1: 创建 chain_session.go**

实现 `ChainSession` 结构体，核心字段和方法：

```go
type ChainSession struct {
    mu                sync.Mutex
    AgentID           string
    SessionScope      SessionScope
    SessionID         string
    DataContainer     DataContainer
    sessionDir        string
    dataContainerType string
    downstreamPolicies map[[2]string]SharingPolicy
    createdAt         float64
    updatedAt         float64
    version           int
    isActive          bool
}
```

方法（全部同步，sync.Mutex 保护）：
- 持久化：`Load() error` / `Flush() error`
  - `Load`：读 state.data → 恢复 meta + data container；扫描 downstreams/*.link → 恢复下游关系
  - `Flush`：写 state.data（meta + data container dump）；写/清理 .link 文件
- 下游关系：AddDownstream/RemoveDownstream/HasDownstream/GetDownstreams/GetDownstreamPolicy/RemoveAllDownstreams
- 数据访问：GetData/UpdateData/CanSee
- 元数据：ToSessionMeta/UpdateFromMeta/SessionKey/CreatedAt/UpdatedAt/Version/IsActive/SetIsActive

Python 对应源码：`/home/opensource/agent-core/openjiuwen/core/session/session_controller/chain_session.py`

关键要点：
- `downstreamKey(agentID, sessionID) [2]string` 辅助函数
- `CanSee` 自身返回 true，有下游关系返回 true，否则 false
- `Flush` 时对每个下游关系写 `{target_agent}_{target_session}.link`，清理已删除的下游（先标记 removed:true 再删除文件）
- `Load` 时跳过 `removed:true` 的 .link 文件（崩溃恢复安全）
- 使用 `encoding/json` 序列化 state.data 和 .link 文件
- 日志使用 `logger.Info(logger.ComponentAgentCore)` 等

- [ ] **Step 2: 创建 chain_session_test.go**

测试项（使用 `t.TempDir()` 创建临时目录）：
- `NewChainSession` 创建实例
- `AddDownstream` / `RemoveDownstream` / `HasDownstream` 下游关系管理
- `GetDownstreams` 返回副本
- `RemoveAllDownstreams` 清空
- `CanSee` 自身可见 / 有下游可见 / 无下游不可见
- `UpdateData` 更新数据
- `GetData` 获取数据
- `Flush` + `Load` 持久化往返测试
- `Flush` 后 .link 文件正确生成
- `Flush` 后删除下游关系，.link 文件被清理
- `ToSessionMeta` / `UpdateFromMeta` 元数据转换
- `SetIsActive` 设置活跃状态并更新时间戳

需要 mock DataContainer 实现（在测试文件中定义 fakeDataContainer）

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/controller/... -run TestChainSession -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/controller/chain_session.go internal/agentcore/session/controller/chain_session_test.go
git commit -m "feat(controller): 实现 ChainSession 链式会话，含持久化和下游关系管理"
```

---

### Task 7: session_controller.go — 单 Agent 会话管理器

**Files:**
- Create: `internal/agentcore/session/controller/session_controller.go`
- Test: `internal/agentcore/session/controller/session_controller_test.go`

- [ ] **Step 1: 创建 session_controller.go**

实现 `SessionController` 结构体和方法：

```go
type SessionController struct {
    mu               sync.Mutex
    AgentID          string
    rootPath         string
    BasePath         string
    dataContainerType string
    SessionCache     map[string]*ChainSession
    MetaMap          map[SessionScope]*ScopeSessionsMeta
}
```

方法分组：
1. 构造：`NewSessionController(agentID, basePath string, dataContainerType ...string) *SessionController`
2. 持久化：`Flush/FlushSession/FlushScope` + `Load/LoadScope`
3. 会话管理：`CreateIfNotExists/GetScopeActiveSession/GetScopeSessions/ActivateSession/GetScopeMeta/ListMetas`
4. 清理：`CleanupScopeInactiveSessions/RemoveSession/RemoveScopeSessions/RemoveAll`
5. 内部方法：`loadSession/writeMetaFile`

Python 对应源码：`/home/opensource/agent-core/openjiuwen/core/session/session_controller/session_controller.py`

关键要点：
- `NewSessionController` 创建 BasePath 目录（os.MkdirAll）
- `CreateIfNotExists` 返回 `(bool, *ChainSession, error)`，bool 表示是否新建
- `Flush` 先释放锁收集 session 列表，用 errgroup 并行 flush，再取锁写 metaFile
- `Load` 读 sessions.json → 解析 SessionScopeKey → 重建 metaMap → 按需加载 session
- `CleanupScopeInactiveSessions` 保留 active session，删除其余的磁盘数据和缓存
- `RemoveSession` 删除缓存 + 磁盘目录 + metaMap 记录
- `RemoveAll` 清空缓存 + metaMap + 删除整个 BasePath 目录
- 日志使用 `logger.Info(logger.ComponentAgentCore)` 等

- [ ] **Step 2: 创建 session_controller_test.go**

测试项（使用 `t.TempDir()` 创建临时目录）：
- `NewSessionController` 创建实例并创建目录
- `CreateIfNotExists` 首次创建返回 is_new=true
- `CreateIfNotExists` 已有 active session 返回 is_new=false
- `CreateIfNotExists` sessionID 重复返回错误
- `GetScopeActiveSession` 获取活跃会话
- `GetScopeActiveSession` 无活跃会话返回 nil
- `GetScopeSessions` 获取所有会话
- `ActivateSession` 激活会话
- `GetScopeMeta` 获取元数据
- `ListMetas` 列出所有元数据
- `Load` + `Flush` 持久化往返测试
- `Load` loadActiveOnly=true 只加载活跃会话
- `CleanupScopeInactiveSessions` 清理非活跃会话
- `RemoveSession` 删除指定会话
- `RemoveScopeSessions` 删除 scope 下所有会话
- `RemoveAll` 删除所有数据

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/controller/... -run TestSessionController -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/controller/session_controller.go internal/agentcore/session/controller/session_controller_test.go
git commit -m "feat(controller): 实现 SessionController 单 Agent 会话管理器"
```

---

### Task 8: global_controller.go — 全局会话控制器

**Files:**
- Create: `internal/agentcore/session/controller/global_controller.go`
- Test: `internal/agentcore/session/controller/global_controller_test.go`

- [ ] **Step 1: 创建 global_controller.go**

实现：
- `GlobalSessionConfig` 结构体
- `GlobalSessionController` 结构体 + `sync.Once` 单例
- `GetGlobalSessionController()` 单例获取
- `SetConfig/LoadAgent/LoadScope/LoadAll/FlushAgent/FlushSession/FlushScope/FlushAll`
- `GetAgent/CreateIfNotExistsAgent/RemoveAgent/RemoveAll`
- `CleanupAgentInactiveSessions/CleanupScopeInactiveSessions/CleanupOrphanFiles`
- 便捷方法（包级函数）：`CreateDirectSession/CreateGroupSession/GetDirectSessionData/UpdateDirectSessionData/AddDirectSessionDownstream/CleanupUserSessions/GetUserSessionHistory/FlushUserSession/VisualizeCallChain`
- `onAgentSessionCreated` 回调函数
- `init()` 中注册 `callback.AgentSessionCreated` 回调
- P2P/PubSub 回调预留注释

Python 对应源码：`/home/opensource/agent-core/openjiuwen/core/session/session_controller/global_controller.py`

关键要点：
- `sync.Once` 保证单例初始化，`init()` 中注册回调
- `CreateIfNotExistsAgent` 返回 `(bool, *SessionController, error)`
- `CleanupOrphanFiles` 扫描磁盘上不在 sessions.json 中的会话目录
- `VisualizeCallChain` 生成调用链可视化文本
- 便捷方法内部通过 `GetGlobalSessionController()` 获取单例后操作
- `onAgentSessionCreated` 回调：从 SessionCallEventData 提取 agentID/sessionID → 查找 ChainSession → 注入 StateAccessor
- P2P/PubSub 回调注释：`// ⤵️ 5.13+ 回填：等 AgentTeamEvents 定义后注册`

- [ ] **Step 2: 创建 global_controller_test.go**

测试项（使用 `t.TempDir()` 创建临时目录，注意不污染全局单例，测试用独立实例）：
- `NewGlobalSessionController` 创建实例
- `SetConfig` 设置 BasePath
- `CreateIfNotExistsAgent` 首次创建返回 is_new=true
- `CreateIfNotExistsAgent` 已存在返回 is_new=false
- `GetAgent` 获取已注册的 Agent
- `GetAgent` 未注册返回 nil
- `LoadAgent` 加载指定 Agent
- `FlushAgent` 刷盘指定 Agent
- `FlushAll` 刷盘所有 Agent
- `RemoveAgent` 删除指定 Agent
- `RemoveAll` 删除所有
- `CleanupAgentInactiveSessions` 清理指定 Agent 的非活跃会话
- `CleanupOrphanFiles` 扫描孤立目录（dryRun=true 只扫描不删除）
- `CleanupOrphanFiles` 删除孤立目录（dryRun=false）
- `CreateDirectSession` 便捷方法
- `CreateGroupSession` 便捷方法
- `VisualizeCallChain` 可视化调用链

注意：全局单例测试需要特殊处理，避免测试间互相影响。可以测试独立构造的实例，而不是通过 `GetGlobalSessionController()`。

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/controller/... -run TestGlobal -v`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/controller/global_controller.go internal/agentcore/session/controller/global_controller_test.go
git commit -m "feat(controller): 实现 GlobalSessionController 全局单例、便捷方法和回调集成"
```

---

### Task 9: 回填与集成

**Files:**
- Modify: `internal/agentcore/session/agent.go` — 确认 StateAccessor 接口兼容性
- Modify: `IMPLEMENTATION_PLAN.md` — 更新 5.6 状态为 ✅

- [ ] **Step 1: 验证 StateAccessor 接口兼容**

确认 `*session.Session` 隐式满足 `StateAccessor` 接口（UpdateState/GetState/DumpState/PreRun 方法签名匹配）。在 `session_controller_test.go` 中添加编译期接口断言：

```go
var _ controller.StateAccessor = (*session.Session)(nil)
```

如果有签名不匹配，调整 `StateAccessor` 接口或 `Session` 方法。

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 5.6 行的状态从 `☐` 改为 `✅`。

- [ ] **Step 3: 更新 doc.go**

确保 `doc.go` 文件目录与实际文件一致。

- [ ] **Step 4: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -v`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add -A
git commit -m "feat(controller): 完成回填验证和 IMPLEMENTATION_PLAN 状态更新"
```

---

## 自检

**1. Spec 覆盖：**
- Scope/Subject 体系 → Task 2 ✅
- SessionScopeFactory → Task 3 ✅
- SessionMeta/ScopeSessionsMeta → Task 4 ✅
- DataContainer/StateAccessor/Permission/SharingPolicy/Factory/AgentSessionContainer → Task 5 ✅
- ChainSession + 持久化 + 下游关系 → Task 6 ✅
- SessionController → Task 7 ✅
- GlobalSessionController + 便捷方法 + 回调 → Task 8 ✅
- SessionPaths → Task 1 ✅
- 回填与集成 → Task 9 ✅

**2. Placeholder 扫描：** 无 TBD/TODO/实现后补充等占位符。LoadAgentSessionContainer 有明确标记 `⤵️ 后续回填` 且提供了当前简化实现。✅

**3. 类型一致性：**
- `downstreamKey` 返回 `[2]string`，在 chain_session.go 和 session_controller.go 中一致使用 ✅
- `CreateIfNotExists` 返回 `(bool, *ChainSession, error)` ✅
- `StateAccessor` 接口的 4 个方法与 `*session.Session` 签名对齐 ✅
- `SessionPaths` 方法签名在 paths.go 定义，在 chain_session.go/session_controller.go 中使用一致 ✅
