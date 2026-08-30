# 9.65a-5 SQL 实现 Design

## 概述

为 `agent_teams/tools/database` 包新增 SQLite/PostgreSQL/MySQL 持久化后端，对齐 Python
`openjiuwen/agent_teams/tools/database/engine.py` 及 4 个 SQL DAO 实现。
当前已有 InMemoryTeamDatabase（9.65a-1 完成），本节补齐 SQL 后端，
使 TeamDatabase 接口具备生产级持久化能力。

### 在 Agent 会话流程中的位置

```
User Request
    ↓
TeamAgent (9.55)          ─── 会话编排
    ↓
TeamBackend (9.65a-4)     ─── 业务门面
    ↓
TaskManager / MessageManager (9.65a-2/3) ─── 业务逻辑
    ↓
TeamDatabase 接口 (9.65a-1)  ─── 数据访问抽象
    ↓
┌──────────────────────────────────┐
│ 9.65a-5: SqlTeamDatabase (本次) │ ← 持久化存储后端
│ InMemoryTeamDatabase (已有)     │ ← 内存存储后端
└──────────────────────────────────┘
```

**作用**：
1. **持久化**：team_info/team_member/task/message 跨进程重启存活
2. **动态会话表**：按 session_id 物理隔离任务/消息（`team_task_{suffix}`），会话结束 DROP
3. **多引擎**：SQLite（开发）/ PostgreSQL（生产）/ MySQL（可选）
4. **解锁后续**：9.61 RecoveryManager 等需要持久化的组件

## 设计决策

| # | 决策 | 选择 | 理由 |
|---|------|------|------|
| 1 | SQL 方案 | **混合**：GORM 生命周期+静态表 AutoMigrate，动态表手写 DDL + `db.Table()` CRUD | GORM 生态统一，动态表名灵活可控 |
| 2 | 事务管理 | **混合**：简单方法 `db.Transaction()`，跨表操作 `WithTx()` | 对齐 Python 单 session 单事务模式 |
| 3 | InitializeEngine 归属 | SqlTeamDatabase 私有方法 `newGormDB()` | InMemory 不需要，无需暴露为顶层函数 |
| 4 | 动态表 DDL | 手写 `db.Exec("CREATE TABLE IF NOT EXISTS ...")` | 与 Python `conn.run_sync(model.__table__.create)` 对齐最直接 |
| 5 | 工厂 | `NewTeamDatabase(ctx, config) TeamDatabase` | 调用方只拿接口，按 db_type 自动选择实现 |
| 6 | 方言处理 | 统一兼容类型（INTEGER/TEXT） | SQLite 和 PostgreSQL 都能接受，避免维护两套 DDL |

## 文件变更清单

### 新增文件

| 文件 | 预估行数 | 职责 |
|------|---------|------|
| `sql_engine.go` | ~350 | SqlTeamDatabase 门面 + newGormDB + DDL + 清理 + WithTx |
| `sql_team_dao.go` | ~120 | SQLTeamDao (5 方法) |
| `sql_member_dao.go` | ~180 | SQLMemberDao (8 方法，含 CAS) |
| `sql_task_dao.go` | ~550 | SQLTaskDao (18 方法 + 5 底层辅助函数 + WithTx) |
| `sql_message_dao.go` | ~220 | SQLMessageDao (7 方法，含重试 + watermark) |
| `sql_engine_test.go` | ~200 | 引擎初始化 + DDL + 清理测试 |
| `sql_team_dao_test.go` | ~150 | Team DAO SQL 测试 |
| `sql_member_dao_test.go` | ~200 | Member DAO SQL 测试（重点 CAS） |
| `sql_task_dao_test.go` | ~400 | Task DAO SQL 测试（重点 5 步管线） |
| `sql_message_dao_test.go` | ~250 | Message DAO SQL 测试（重点 watermark） |

### 修改文件

| 文件 | 变更 |
|------|------|
| `engine.go` | 删除 5 个 `⤵️ 9.65a-5` 占位函数，保留 `GetCurrentTime` + `SanitizeSessionIDForTable` |
| `database.go` | 新增 `NewTeamDatabase(ctx, config) TeamDatabase` 工厂函数 |
| `doc.go` | 更新文件目录，添加 sql_*.go 条目 |

### 删除文件

| 文件 | 原因 |
|------|------|
| `team_dao.go` | 纯占位，实现分别在 memory_impl.go 和 sql_team_dao.go |
| `member_dao.go` | 同上 |
| `task_dao.go` | 同上 |
| `message_dao.go` | 同上 |

## 详细设计

### sql_engine.go — 门面 + 引擎

#### SqlTeamDatabase 结构体

```go
type SqlTeamDatabase struct {
    db          *gorm.DB
    config      DatabaseConfig
    teamDao     *SQLTeamDao
    memberDao   *SQLMemberDao
    taskDao     *SQLTaskDao
    messageDao  *SQLMessageDao
    initialized bool
    mu          sync.Mutex
}
```

编译期接口满足性检查：
```go
var _ TeamDatabase = (*SqlTeamDatabase)(nil)
```

#### newGormDB(ctx, config) — 私有方法

对齐 Python `initialize_engine(config)`：

| 数据库 | 连接串 | 连接池 | 特殊配置 |
|--------|--------|--------|---------|
| SQLite `:memory:` | `sqlite3:///:memory:` | 无池 | `PRAGMA foreign_keys=ON` |
| SQLite 文件 | `sqlite3:///path` | MaxOpenConns=5 | `PRAGMA foreign_keys=ON`, 可选 `PRAGMA journal_mode=WAL` |
| PostgreSQL | `postgres://...` | MaxOpenConns=10, MaxIdleConns=10 | pool_recycle=1800 |
| MySQL | `mysql://...` | MaxOpenConns=10, MaxIdleConns=10 | pool_recycle=1800 |

初始化后：
1. `db.AutoMigrate(&Team{}, &TeamMember{})` — 建静态表
2. `ensureTeamMemberRoleColumn(db)` — 迁移补 `role` 列（对齐 Python `_ensure_team_member_role_column`）

#### 动态表 DDL

4 张动态表，表名 = 前缀 + `SanitizeSessionIDForTable(sessionID)` 后缀：

```sql
-- team_task_{suffix}
CREATE TABLE IF NOT EXISTS team_task_{suffix} (
    task_id      TEXT PRIMARY KEY,
    team_name    TEXT NOT NULL DEFAULT '',
    title        TEXT NOT NULL DEFAULT '',
    content      TEXT NOT NULL DEFAULT '',
    status       TEXT NOT NULL DEFAULT '',
    assignee     TEXT NOT NULL DEFAULT '',
    updated_at   INTEGER NOT NULL DEFAULT 0
);

-- team_task_dependency_{suffix}
CREATE TABLE IF NOT EXISTS team_task_dependency_{suffix} (
    task_id           TEXT NOT NULL DEFAULT '',
    depends_on_task_id TEXT NOT NULL DEFAULT '',
    team_name         TEXT NOT NULL DEFAULT '',
    resolved          INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (task_id, depends_on_task_id)
);

-- team_message_{suffix}
CREATE TABLE IF NOT EXISTS team_message_{suffix} (
    message_id       TEXT PRIMARY KEY,
    team_name        TEXT NOT NULL DEFAULT '',
    from_member_name TEXT NOT NULL DEFAULT '',
    to_member_name   TEXT NOT NULL DEFAULT '',
    content          TEXT NOT NULL DEFAULT '',
    timestamp        INTEGER NOT NULL DEFAULT 0,
    broadcast        INTEGER NOT NULL DEFAULT 0,
    is_read          INTEGER DEFAULT NULL
);

-- message_read_status_{suffix}
CREATE TABLE IF NOT EXISTS message_read_status_{suffix} (
    member_name TEXT NOT NULL DEFAULT '',
    team_name   TEXT NOT NULL DEFAULT '',
    read_at     INTEGER DEFAULT NULL,
    PRIMARY KEY (member_name, team_name)
);
```

DDL 统一用兼容类型：INTEGER（布尔/整数/时间戳）、TEXT（字符串），SQLite 和 PostgreSQL 都能接受。
`is_read` / `read_at` 用 `DEFAULT NULL` 表示广播消息/未初始化水位。

#### CreateCurSessionTables

```go
func (s *SqlTeamDatabase) CreateCurSessionTables(ctx context.Context) error {
    sessionID := schema.GetSessionID(ctx)
    if sessionID == "" { return nil }
    suffix := SanitizeSessionIDForTable(sessionID)
    // 执行 4 条 CREATE TABLE IF NOT EXISTS DDL
}
```

#### DropCurSessionTables / DropSessionTablesByID

```sql
DROP TABLE IF EXISTS team_task_{suffix};
DROP TABLE IF EXISTS team_task_dependency_{suffix};
DROP TABLE IF EXISTS team_message_{suffix};
DROP TABLE IF EXISTS message_read_status_{suffix};
```

#### CleanupAllRuntimeState

对齐 Python `cleanup_all_runtime_state`：
1. `db.Raw("SELECT name FROM sqlite_master WHERE type='table'")` / PostgreSQL `information_schema.tables` 获取所有表名
2. 匹配 `TeamDynamicTablePrefixes` 前缀 → DROP
3. 匹配 `TeamStaticTablesToClear` → DELETE FROM

**跨方言处理**：GORM 提供 `db.Migrator().GetTables()` 可统一获取表名列表，无需手写方言查询。

#### WithTx — 事务绑定

```go
// WithTx 返回绑定指定事务的临时 DAO 实例，用于跨表操作。
func (s *SqlTeamDatabase) WithTx(tx *gorm.DB) (*SQLTeamDao, *SQLMemberDao, *SQLTaskDao, *SQLMessageDao) {
    return s.teamDao.withTx(tx),
           s.memberDao.withTx(tx),
           s.taskDao.withTx(tx),
           s.messageDao.withTx(tx)
}
```

每个 SQL*Dao 有非导出方法 `withTx(tx *gorm.DB) *SQLXxxDao` 返回同一个 DAO 但 db 字段替换为 tx。

#### ensureTeamMemberRoleColumn

对齐 Python `_ensure_team_member_role_column`：
```go
func ensureTeamMemberRoleColumn(db *gorm.DB) {
    // 检查 team_member 表是否有 role 列
    // 没有 → ALTER TABLE team_member ADD COLUMN role TEXT NOT NULL DEFAULT 'teammate'
}
```

### sql_team_dao.go — SQLTeamDao (5 方法)

对齐 Python `TeamDao (team_dao.py)`：

```go
type SQLTeamDao struct {
    db *gorm.DB
}
```

| 方法 | Python 对应 | SQL 策略 |
|------|------------|---------|
| `CreateTeam` | `create_team` | INSERT，IntegrityError → false |
| `GetTeam` | `get_team` | SELECT by team_name |
| `TeamExists` | `team_exists` | SELECT team_name LIMIT 1 |
| `DeleteTeam` | `delete_team` | DELETE + 级联删成员（静态表无 FK 级联时手动删） |
| `GetTeamUpdatedAt` | `get_team_updated_at` | SELECT updated_at |

**动态表名**：team_info 是静态表，GORM AutoMigrate 创建，DAO 直接 `db.Table("team_info")` 操作。

### sql_member_dao.go — SQLMemberDao (8 方法)

对齐 Python `MemberDao (member_dao.py)`：

```go
type SQLMemberDao struct {
    db *gorm.DB
}
```

| 方法 | Python 对应 | SQL 策略 |
|------|------------|---------|
| `CreateMember` | `create_member` | INSERT，IntegrityError → false |
| `GetMember` | `get_member` | SELECT by (member_name, team_name) |
| `GetTeamMembers` | `get_team_members` | SELECT by team_name，可选 status WHERE |
| `UpdateMemberStatus` | `update_member_status` | UPDATE + FSM 校验（先查再改） |
| `TryTransitionMemberStatus` | `try_transition_member_status` | **CAS**: `WHERE status = fromStatus` + `RowsAffected == 1` |
| `ListHumanAgentNames` | `list_human_agent_names` | SELECT WHERE role='human_agent' |
| `GetMembersMaxUpdatedAt` | `get_members_max_updated_at` | SELECT MAX(updated_at) |
| `UpdateMemberExecutionStatus` | `update_member_execution_status` | UPDATE + FSM 校验 |

**CAS 关键实现**（对齐 Python `try_transition_member_status`）：
```go
func (d *SQLMemberDao) TryTransitionMemberStatus(ctx context.Context, memberName, teamName, fromStatus, toStatus string) bool {
    result := d.db.WithContext(ctx).
        Table("team_member").
        Where("member_name = ? AND team_name = ? AND status = ?", memberName, teamName, fromStatus).
        Update("status", toStatus)
    return result.RowsAffected == 1
}
```

### sql_task_dao.go — SQLTaskDao (18 方法 + 5 辅助函数)

对齐 Python `TaskDao (task_dao.py)`，最复杂的 DAO。

```go
type SQLTaskDao struct {
    db *gorm.DB
}
```

**表名策略**：task 表是动态表，每次操作需要从 ctx 获取 sessionID → 拼接 `team_task_{suffix}`。
DAO 方法接收 ctx，内部计算表名。

#### 动态表名获取

```go
func (d *SQLTaskDao) taskTableName(ctx context.Context) string {
    sessionID := schema.GetSessionID(ctx)
    suffix := SanitizeSessionIDForTable(sessionID)
    return "team_task_" + suffix
}

func (d *SQLTaskDao) depTableName(ctx context.Context) string {
    sessionID := schema.GetSessionID(ctx)
    suffix := SanitizeSessionIDForTable(sessionID)
    return "team_task_dependency_" + suffix
}
```

**注意**：withTx 返回的 DAO 实例共享同一个 db（tx），表名仍从 ctx 计算。

#### 5 个底层辅助函数（非导出，接收 *gorm.DB 参数）

对齐 Python 的 5 个模块级 `_xxx_in_session` 函数：

| Go 函数 | Python 对应 | 作用 |
|---------|------------|------|
| `refreshStatusInTx(tx, taskTableName, taskIDs, now)` | `_refresh_status_in_session` | 根据 unresolved deps 重算 PENDING/BLOCKED |
| `terminateTaskInTx(tx, taskTableName, depTableName, taskID, newStatus, now)` | `_terminate_task_in_session` | 终态 + 标记依赖 resolved + 传播解除阻塞 |
| `stageNewTasksInTx(tx, taskTableName, teamName, newTasks, now)` | `_stage_new_tasks` | INSERT 新任务行 |
| `loadEndpointsAndValidateInTx(tx, taskTableName, addEdges)` | `_load_endpoints_and_validate` | 解析边端点 + 校验存在性和源状态 |
| `checkCycleAndComputeNewEdgesInTx(tx, depTableName, teamName, addEdges, ...)` | `_check_cycle_and_compute_new_edges` | 环检测 + 计算新边集 |

这些函数接收显式的表名参数和 `*gorm.DB`（可能是 db 或 tx），
在 `MutateDependencyGraph` 的单事务中被调用。

#### MutateDependencyGraph — 5 步管线

对齐 Python `mutate_dependency_graph`，在 `db.Transaction(func(tx) { ... })` 中执行。
管线失败时 `return err` 触发 rollback，对齐 Python 的 `session.rollback()`：

```go
func (d *SQLTaskDao) MutateDependencyGraph(ctx context.Context, teamName string, newTasks []NewTaskSpec, addEdges []EdgeSpec) GraphMutationResult {
    taskTable := d.taskTableName(ctx)
    depTable := d.depTableName(ctx)
    now := GetCurrentTime()

    var result GraphMutationResult
    var mutationErr error  // 对齐 Python _MutationFailure

    err := d.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
        // 步骤1: stageNewTasksInTx
        if mutationErr = stageNewTasksInTx(tx, taskTable, teamName, newTasks, now); mutationErr != nil {
            return mutationErr  // rollback
        }

        // 步骤2: loadEndpointsAndValidateInTx
        var endpointTasks map[string]*TeamTaskBase
        if endpointTasks, mutationErr = loadEndpointsAndValidateInTx(tx, taskTable, addEdges); mutationErr != nil {
            return mutationErr  // rollback
        }

        // 步骤3: checkCycleAndComputeNewEdgesInTx
        var newEdges []TeamTaskDependencyBase
        if newEdges, mutationErr = checkCycleAndComputeNewEdgesInTx(tx, depTable, teamName, addEdges, endpointTasks); mutationErr != nil {
            return mutationErr  // rollback
        }

        // 步骤4: applyNewEdgesInTx — INSERT 依赖行
        if mutationErr = applyNewEdgesInTx(tx, depTable, teamName, newEdges); mutationErr != nil {
            return mutationErr  // rollback
        }

        // 步骤5: refreshStatusInTx
        refreshedIDs := refreshStatusInTx(tx, taskTable, allAffectedTaskIDs, now)

        result.Ok = true
        result.RefreshedTasks = refreshedIDs
        return nil  // commit
    })

    if err != nil {
        // 对齐 Python except _MutationFailure: session.rollback(); return fail
        result.Reason = mutationErr.Error()
    }
    return result
}
```

**事务语义对齐说明**：
- Python：`async with session` → 步骤抛 `_MutationFailure` → `except` 捕获 → `session.rollback()` → 返回 fail
- Go：`db.Transaction` → 步骤返回 error → `return err` 触发 rollback → 外层记录 reason → 返回 fail
- 两者行为等价：管线失败时回滚全部变更（包括步骤1已 INSERT 的 newTasks），返回失败原因

#### CancelTask / CompleteTask / CancelAllTasks — 原子终止传播

在 `db.Transaction` 中执行，调用 `terminateTaskInTx`。

#### CancelAllTasks 的 skipAssignees

对齐 Python `cancel_all_tasks(skip_assignees)`：
```sql
UPDATE team_task_{suffix} SET status = 'CANCELLED'
WHERE team_name = ? AND status NOT IN ('COMPLETED', 'CANCELLED')
  AND (assignee NOT IN (?) OR ?)  -- skip_assignees 过滤
```

### sql_message_dao.go — SQLMessageDao (7 方法)

对齐 Python `MessageDao (message_dao.py)`：

```go
type SQLMessageDao struct {
    db *gorm.DB
}
```

**表名策略**：message 表也是动态表，从 ctx 获取 sessionID → `team_message_{suffix}` / `message_read_status_{suffix}`。

#### CreateMessage — 指数退避重试

对齐 Python `create_message` 的 `OperationalError` 重试逻辑：

```go
func (d *SQLMessageDao) CreateMessage(ctx context.Context, msg *TeamMessageBase) bool {
    msgTable := d.msgTableName(ctx)
    var backoff = []time.Duration{100 * time.Millisecond, 300 * time.Millisecond, 500 * time.Millisecond}
    for attempt := 0; attempt <= 3; attempt++ {
        err := d.db.WithContext(ctx).Table(msgTable).Create(msg).Error
        if err == nil { return true }
        if attempt < 3 { time.Sleep(backoff[attempt]) }
    }
    return false
}
```

#### GetBroadcastMessages — watermark 过滤

对齐 Python `get_broadcast_messages` 的 read_status 水位线机制：

```sql
SELECT * FROM team_message_{suffix}
WHERE team_name = ? AND broadcast = 1
  AND from_member_name != ?   -- 排除自己发的
  AND timestamp > COALESCE(
    (SELECT read_at FROM message_read_status_{suffix}
     WHERE member_name = ? AND team_name = ?), 0)
```

unreadOnly=true 时追加 `AND is_read = 0`；fromMemberName 非空时追加 `AND from_member_name = ?`。

#### MarkMessageRead — 直发 vs 广播分支

```go
func (d *SQLMessageDao) MarkMessageRead(ctx context.Context, messageID, memberName string) bool {
    // 1. 查消息，确定是直发还是广播
    // 2. 直发：UPDATE team_message_{suffix} SET is_read = 1 WHERE message_id = ?
    // 3. 广播：INSERT OR UPDATE message_read_status_{suffix}
    //    SET read_at = max(现有 read_at, 消息 timestamp)
    //    WHERE member_name = ? AND team_name = ?
}
```

## NewTeamDatabase 工厂函数

```go
func NewTeamDatabase(ctx context.Context, config DBConfigProvider) TeamDatabase {
    switch config.GetDBType() {
    case DatabaseTypeMemory:
        return NewInMemoryTeamDatabase()
    default:
        // SQLite / PostgreSQL / MySQL
        sqlDB := &SqlTeamDatabase{config: config.(DatabaseConfig)}
        if err := sqlDB.Initialize(ctx); err != nil {
            logger.Error(ComponentCommon).Err(err).Msg("SQL 数据库初始化失败")
            return NewInMemoryTeamDatabase() // 降级为 InMemory
        }
        return sqlDB
    }
}
```

## 回填清单

| 来源 | 回填内容 | 目标 |
|------|---------|------|
| `engine.go` 5个 `⤵️ 9.65a-5` 占位函数 | 删除，实现移入 SqlTeamDatabase 方法 | `sql_engine.go` |
| `team_dao.go` 等4个占位文件 | 删除占位，SQL 实现在 `sql_*.go` | `sql_team_dao.go` 等 |
| `database.go` | 新增 `NewTeamDatabase` 工厂函数 | `database.go` |
| `doc.go` | 更新文件目录，添加 sql_*.go 条目 | `doc.go` |

## 测试策略

- 全部使用 SQLite `:memory:` 模式测试，不依赖外部数据库
- 测试函数签名：`TestSQLXxx_场景描述`，区分于 InMemory 测试
- `//go:build integration` 标签的 PostgreSQL 真实连接测试作为可选扩展
- SQL DAO 测试模式：每个测试先 `Initialize` 建 engine + 动态表，测试完 `CleanupAllRuntimeState`
- 重点测试项：
  - CAS 原子转换并发场景（`TryTransitionMemberStatus`）
  - MutateDependencyGraph 5 步管线（环检测、端点校验、状态刷新）
  - 消息重试 + watermark 过滤
  - 动态表创建/删除/清理生命周期

## 对应 Python 代码

| Go 文件 | Python 文件 |
|---------|------------|
| `sql_engine.go` | `openjiuwen/agent_teams/tools/database/engine.py` + `__init__.py` |
| `sql_team_dao.go` | `openjiuwen/agent_teams/tools/database/team_dao.py` |
| `sql_member_dao.go` | `openjiuwen/agent_teams/tools/database/member_dao.py` |
| `sql_task_dao.go` | `openjiuwen/agent_teams/tools/database/task_dao.py` |
| `sql_message_dao.go` | `openjiuwen/agent_teams/tools/database/message_dao.py` |
