# 9.65a-4 审查修复设计

## 概述

9.65a-4 TeamBackend 门面实现完成后，对实现过程进行审查，发现 5 个问题需要修复。
本文档记录每个问题的根因、修复方案和验证结果。

## 问题 1：member.go 的 DB/Messager 类型回填

### 根因

9.65a-4 实现时回填了 `infra.go`、`agent_configurator.go`、`team_agent.go`、`human_agent_inbox.go`、
`user_inbox.go`、`router.go` 中的 `any` → 具体类型，但遗漏了 `member.go` 的 `DB` 和 `Messager` 字段。

Python 的 `TeamMember.__init__` 签名中 `db: TeamDatabase` 和 `messager: Messager` 都是具体类型。

### 修复方案

```go
// Before
DB any       // TODO(#9.65): TeamDatabase 类型
Messager any  // TODO(#9.65): Messager 类型

// After
DB database.TeamDatabase
Messager messager.Messager
```

### 循环依赖验证

- `agent` 包当前 import：`memory, messager, models, schema, spawn, tools, tools/database`
- `database` 和 `messager` 都不 import `agent` → ✅ 无循环依赖

### 影响范围

- `member.go`：字段类型 + import
- `member_test.go`：如果有使用 `DB`/`Messager` 字段的测试用例需更新

## 问题 2：SpawnMember 格式校验

### 根因

设计文档（9.65a-4 design spec）第 267 行写了"校验：member_name 非空、不含"/"、非保留名"，
但 Python 的实际分层是：
- **`TeamBackend.spawn_member()`**：只做 DB 重复检查，**不做**成员名格式校验
- **`SpawnMemberTool.invoke()`**：做正则校验 `^[a-z][a-z0-9-]*$`（tool 层）
- **`TeamAgentSpec._validate_reserved_names()`**：在 spec 构建时校验保留名（config 层）

Go 的 `TeamBackend.SpawnMember` 当前只做 DB 重复检查，**和 Python 一致**。

### 修复方案

不修改代码，修正设计文档中的描述：

```
// Before
1. 校验：member_name 非空、不含"/"、非保留名

// After
1. 校验：仅检查 DB 中是否已存在同名成员，格式校验留给 Tool 层（9.68）
```

## 问题 3：提取 spawnAndPublish + 补全 Startup/StartupMember

### 根因

Python 的 `startup_member` 流程：
1. CAS: UNSTARTED→STARTING
2. `_spawn_and_publish(member_name, on_created)` — 调用回调启动 agent + 发布 MemberSpawnedEvent
3. 如果 `_spawn_and_publish` 失败 → 回滚 STARTING→UNSTARTED

Go 当前 `StartupMember` 只做了步骤 1，**步骤 2 和 3 都缺失**。

### 修复方案

**新增 `spawnAndPublish` 内部方法：**

```go
// spawnAndPublish 启动成员 agent 并发布 MemberSpawnedEvent。
// 对齐 Python: _spawn_and_publish(member_name, on_created)
// 事件发布失败仅记日志不抛异常。
func (tb *TeamBackend) spawnAndPublish(
    ctx context.Context,
    memberName string,
    onCreated func(ctx context.Context, memberName string) error,
) error {
    // 1. 调用 onCreated 回调（启动 agent 进程）
    if err := onCreated(ctx, memberName); err != nil {
        return err
    }
    // 2. 发布 MemberSpawnedEvent（失败只记日志）
    tb.publishEvent(ctx, atschema.MemberSpawnedEvent{
        BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName},
        MemberName:       memberName,
    })
    // 3. 日志
    logger.Info(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
        Msg("spawnAndPublish: 成员已启动")
    return nil
}
```

**修改 `StartupMember` 签名，补 onCreated + 失败回滚：**

```go
// Before
func (tb *TeamBackend) StartupMember(ctx context.Context, memberName string) (bool, error)

// After
func (tb *TeamBackend) StartupMember(
    ctx context.Context,
    memberName string,
    onCreated func(ctx context.Context, memberName string) error,
) (bool, error)
```

流程对齐 Python：
1. CAS: UNSTARTED→STARTING
2. `spawnAndPublish(memberName, onCreated)` — 调回调 + 发布事件
3. 如果 `spawnAndPublish` 失败 → 回滚 STARTING→UNSTARTED

**修改 `Startup` 签名，透传 onCreated：**

```go
// Before
func (tb *TeamBackend) Startup(ctx context.Context) ([]string, error)

// After
func (tb *TeamBackend) Startup(
    ctx context.Context,
    onCreated func(ctx context.Context, memberName string) error,
) ([]string, error)
```

内部循环调用 `StartupMember(ctx, memberName, onCreated)`。

### 签名变更影响

- `agent_configurator.go` 中如果有调用 `Startup`/`StartupMember` 需更新
- `team_agent.go` 中如果有调用需更新
- `runtime/manager.go` 中如果有调用需更新
- 测试文件需更新

## 问题 4：BuildTeam 的 db.Initialize / db.CreateCurSessionTables

### 根因

设计文档中写了"DB 初始化：db.initialize() + db.create_cur_session_tables()"，
但 Python 的 `build_team()` **不调用**这两个方法：
- `create_cur_session_tables()` 在 `SessionManager.bind_session()` 中调用
- `db.initialize()` 在 `runtime/manager.py` 和 `coordination/kernel.py` 中调用

Go 当前 `BuildTeam` 也不调用，**和 Python 一致**。

### 修复方案

不修改代码，修正设计文档中的描述。

## 问题 5：ActiveTeam.Agent any → *TeamAgent

### 根因

`ActiveTeam.Agent` 字段用 `any` 是当初实现时 TeamAgent 类型还没完善，用 `any` 做了占位。
为了在 `runtime/manager.go` 中访问 `TeamBackend()`，引入了 `TeamBackendProvider` 接口做类型断言。

### 循环依赖验证

当前依赖图：
```
runtime → interaction, tools
agent → memory, messager, models, schema, spawn, tools, tools/database
interaction → schema, tools
```

- `runtime` 和 `agent` 之间**没有直接 import** → ✅ 无循环依赖
- `interaction` 和 `agent` 之间**没有直接 import** → ✅ 无循环依赖
- `runtime` 的依赖（`interaction`、`tools`）也不 import `agent` → ✅ 无间接循环

### 修复方案

**`runtime/pool.go`：**

```go
// Before
type TeamBackendProvider interface {
    TeamBackend() *tools.TeamBackend
}
type ActiveTeam struct {
    Agent any  // ⤵️ 待 9.55 回填: *TeamAgent
    ...
}

// After
import "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"

type ActiveTeam struct {
    Agent *agent.TeamAgent
    ...
}
```

删除 `TeamBackendProvider` 接口。

**`runtime/manager.go`：**

```go
// Before
func getTeamBackend(agent any) *tools.TeamBackend {
    provider, ok := agent.(TeamBackendProvider)
    ...
}

// After
func getTeamBackend(agent *agent.TeamAgent) *tools.TeamBackend {
    return agent.TeamBackend()
}
```

**`interaction/human_agent_inbox.go`：**

```go
// Before
type AgentLookup func(sender string) any

// After
import "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
type AgentLookup func(sender string) *agent.TeamAgent
```

`driveAgent` 方法中可直接调用 `agent.DeliverInput(ctx, body)`（目前标注 ⤵️ 待 9.55 回填）。

### 影响范围

- `runtime/pool.go`：删除 `TeamBackendProvider`，`ActiveTeam.Agent` 改类型
- `runtime/manager.go`：`getTeamBackend` 简化，所有调用点无需改动
- `runtime/manager_test.go`：`mockAgent` 不再需要，直接用 `*agent.TeamAgent` 或其子集
- `interaction/human_agent_inbox.go`：`AgentLookup` 类型 + `driveAgent` 方法
- `interaction/human_agent_inbox_test.go`：测试用例更新

## 修改文件清单

| 文件 | 修改类型 | 说明 |
|------|---------|------|
| `agent/member.go` | 类型回填 | `DB any` → `database.TeamDatabase`，`Messager any` → `messager.Messager` |
| `tools/team_backend.go` | 方法提取 | 新增 `spawnAndPublish`，`Startup`/`StartupMember` 签名变更 |
| `tools/team_backend_test.go` | 测试更新 | `Startup`/`StartupMember` 调用需传 onCreated |
| `runtime/pool.go` | 类型回填 | `Agent any` → `*agent.TeamAgent`，删除 `TeamBackendProvider` |
| `runtime/manager.go` | 简化 | `getTeamBackend` 简化，import `agent` |
| `runtime/manager_test.go` | 测试更新 | `mockAgent` 改为真实 `*agent.TeamAgent` 或子集 |
| `interaction/human_agent_inbox.go` | 类型回填 | `AgentLookup` 返回 `*agent.TeamAgent` |
| `interaction/human_agent_inbox_test.go` | 测试更新 | 适配新 `AgentLookup` 类型 |
| `docs/superpowers/specs/2027-03-10-team-backend-9.65a-4-design.md` | 文档修正 | SpawnMember 校验描述（第 267 行）+ BuildTeam db.Initialize 描述（第 280 行） |
| `agent/agent_configurator.go` | 签名适配 | `Startup`/`StartupMember` 调用需传 onCreated（如有调用） |
| `agent/team_agent.go` | 签名适配 | `Startup`/`StartupMember` 调用需传 onCreated（如有调用） |
