# 9.65a-1 TeamDB 基础层设计方案

> 章节编号：9.65a-1
> 对齐 Python：`openjiuwen/agent_teams/tools/database/` + `tools/models.py`
> 前置依赖：无（本章节是所有后续 9.65a 子章节的基础）
> 被依赖：9.65a-2（TaskDao+TaskManager）、9.65a-3（MessageDao+MessageManager）、9.65a-4（TeamBackend 门面）、9.61（RecoveryManager）

---

## 1. 设计决策汇总

| # | 决策 | 选择 | 理由 |
|---|------|------|------|
| 1 | 拆分方式 | 5 子章节（9.65a-1~9.65a-5） | TeamBackend 1200+ 行，拆分避免单章节过大 |
| 2 | agent_card 存储 | JSON 字符串（对齐 Python） | Python 存 `AgentCard.model_dump_json()`，Go InMemory 同样存 JSON string |
| 3 | 数据模型位置 | `tools/database` 包内 | 对齐 Python `tools/database/models.py`，所有模型+接口内聚同一包 |
| 4 | InMemory 实现策略 | 单体结构体（对齐 Python） | Python 用 `self.team=self` 自引用单体，Go 用同一结构体实现多个接口 |
| 5 | DAO 返回类型 | `bool` 替代 `error` | Python DAO 返回 bool（FSM 校验失败不是异常），Go 同理 |
| 6 | 模型迁移 | TeamInfo→Team, TeamMemberInfo→TeamMember | 去掉 Info 后缀对齐 Python，迁移到 database 包 |
| 7 | 时间戳类型 | `int64`（毫秒时间戳） | Python 用 `BigInteger(ms)`，Go 用 `int64` |
| 8 | InMemoryTeamDatabase 位置 | 全部在 `database` 包 | 删掉 `tools/memory_database.go` 空接口，具体实现和类型都在 database 包 |

---

## 2. 文件目录变更

### 删除文件

| 文件 | 原因 |
|------|------|
| `internal/agent_teams/tools/models.go` | 迁移到 `database/models.go`，重命名 TeamInfo→Team, TeamMemberInfo→TeamMember |
| `internal/agent_teams/tools/memory_database.go` | InMemoryTeamDatabase 实现移入 database 包，不再需要 tools 包的空接口 |

### 新增文件

| 文件 | 内容 |
|------|------|
| `internal/agent_teams/tools/database/models.go` | Team + TeamMember 数据模型（补全缺失字段，时间戳改为 int64） |
| `internal/agent_teams/tools/database/memory_impl.go` | InMemoryTeamDatabase 单体实现（TeamDatabase + TeamDao + MemberDao 接口） |
| `internal/agent_teams/tools/database/models_test.go` | 模型序列化/反序列化测试 |
| `internal/agent_teams/tools/database/database_test.go` | 接口满足性编译检查 |
| `internal/agent_teams/tools/database/memory_impl_test.go` | InMemory DAO 全面测试 |
| `internal/agent_teams/tools/database/engine_test.go` | 会话表生命周期测试 |

### 修改文件

| 文件 | 变更内容 |
|------|---------|
| `internal/agent_teams/tools/database/database.go` | DAO 接口补全方法、改返回类型（any→具体类型, error→bool） |
| `internal/agent_teams/tools/database/engine.go` | 新增 GetCurrentTime/SanitizeSessionIDForTable/常量定义 |
| `internal/agent_teams/tools/database/team_dao.go` | 清空占位注释（实现移到 memory_impl.go） |
| `internal/agent_teams/tools/database/member_dao.go` | 清空占位注释（实现移到 memory_impl.go） |
| `internal/agent_teams/tools/database/doc.go` | 更新文件目录（添加 models.go, memory_impl.go 等） |
| `internal/agent_teams/tools/doc.go` | 更新文件目录（删除 models.go/memory_database.go，标注迁移到 database） |

### 最终文件结构

```
internal/agent_teams/tools/
├── doc.go                    # 更新：删除已迁移文件条目
├── task_manager.go           # 保留（9.65a-2 回填）
├── message_manager.go        # 保留（9.65a-3 回填）
│
└── database/
    ├── doc.go                # 更新：添加 models.go, memory_impl.go 等
    ├── config.go             # 保留不变
    ├── config_test.go        # 保留不变
    ├── models.go             # 新增：Team + TeamMember 数据模型
    ├── database.go           # 重写：补全 DAO 接口方法、改返回类型
    ├── engine.go             # 重写：新增时间函数/常量/SanitizeSessionID
    ├── memory_impl.go        # 新增：InMemoryTeamDatabase 单体实现
    ├── team_dao.go           # 清空占位（实现已在 memory_impl.go）
    ├── member_dao.go         # 清空占位（实现已在 memory_impl.go）
    ├── task_dao.go           # 清空占位（9.65a-2 回填）
    ├── message_dao.go        # 清空占位（9.65a-3 回填）
    ├── models_test.go        # 新增
    ├── database_test.go      # 新增
    ├── memory_impl_test.go   # 新增
    └── engine_test.go        # 新增
```

---

## 3. 数据模型定义

### 3.1 Team 结构体

对齐 Python `Team`（`tools/models.py` 第 35-48 行），6 字段 + 复合注释：

```go
// Team 团队信息模型。
// 对齐 Python: Team (openjiuwen/agent_teams/tools/models.py)
// 静态表 team_info 的行模型。
type Team struct {
    // TeamName 团队名称（主键）
    TeamName string `json:"team_name"`
    // DisplayName 显示名称
    DisplayName string `json:"display_name"`
    // LeaderMemberName Leader 成员名
    LeaderMemberName string `json:"leader_member_name"`
    // Desc 团队描述
    Desc string `json:"desc,omitempty"`
    // Prompt 团队提示词（新增，Python 原有）
    Prompt string `json:"prompt,omitempty"`
    // Created 创建时间（毫秒时间戳）
    Created int64 `json:"created"`
    // UpdatedAt 更新时间（毫秒时间戳，仅 roster 变更时 bump）
    UpdatedAt int64 `json:"updated_at,omitempty"`
}
```

**与旧 TeamInfo 的差异**：
- 新增 `Prompt` 字段（Python 原有，Go 之前缺失）
- `Created`/`UpdatedAt` 从 `time.Time` → `int64`（毫秒时间戳，对齐 Python BigInteger）
- 去掉 `Info` 后缀

### 3.2 TeamMember 结构体

对齐 Python `TeamMember`（`tools/models.py` 第 51-89 行），11 字段：

```go
// TeamMember 团队成员模型。
// 对齐 Python: TeamMember (openjiuwen/agent_teams/tools/models.py)
// 静态表 team_member 的行模型，复合主键 (member_name, team_name)。
type TeamMember struct {
    // MemberName 成员名称（主键）
    MemberName string `json:"member_name"`
    // TeamName 团队名称（主键，外键 team_info.team_name）
    TeamName string `json:"team_name"`
    // DisplayName 显示名称
    DisplayName string `json:"display_name"`
    // Desc 成员描述
    Desc string `json:"desc,omitempty"`
    // AgentCard Agent 卡片 JSON 字符串（存储 AgentCard.model_dump_json()）
    AgentCard string `json:"agent_card"`
    // Status 成员状态（MemberStatus 枚举值）
    Status string `json:"status"`
    // ExecutionStatus 执行状态（ExecutionStatus 枚举值）
    ExecutionStatus string `json:"execution_status,omitempty"`
    // Mode 成员模式（MemberMode 枚举值：build_mode / plan_mode）
    Mode string `json:"mode"`
    // Role 成员角色（TeamRole 枚举值：leader / teammate / human_agent）
    Role string `json:"role"`
    // Prompt 成员专属提示词
    Prompt string `json:"prompt,omitempty"`
    // ModelRefJSON 模型引用 JSON（{"model_id": str, "model_name": str}）
    ModelRefJSON string `json:"model_ref_json,omitempty"`
    // UpdatedAt 更新时间（毫秒时间戳，仅 roster 变更时 bump，status 变更不 bump）
    UpdatedAt int64 `json:"updated_at,omitempty"`
}
```

**与旧 TeamMemberInfo 的差异**：
- 新增 `ExecutionStatus`、`Mode`、`Prompt` 字段（Python 原有，Go 之前缺失）
- `UpdatedAt` 从 `time.Time` → `int64`
- `Status` 保持 `string`（存储 MemberStatus 枚举值）
- 去掉 `Info` 后缀

### 3.3 动态表基类与常量

动态表模型（TeamTaskBase 等）属于 9.65a-2/9.65a-3 范围，本章节只定义常量和工厂函数：

```go
// ──────────────────────────── 常量 ────────────────────────────

const (
    // TeamDynamicTablePrefixes 动态表名前缀（用于识别和清理）
    // 对齐 Python: TEAM_DYNAMIC_TABLE_PREFIXES
    TeamDynamicTablePrefixes = [...]string{
        "team_task_dependency_",
        "team_task_",
        "team_message_",
        "message_read_status_",
    }
    // TeamStaticTablesToClear 需要清空的静态表名
    // 对齐 Python: TEAM_STATIC_TABLES_TO_CLEAR
    TeamStaticTablesToClear = [...]string{
        "team_info",
        "team_member",
    }
)
```

---

## 4. DAO 接口补全

### 4.1 TeamDao 接口（5→5 方法，签名全面修订）

```go
// TeamDao 团队 DAO 接口。
// 对齐 Python: TeamDao (openjiuwen/agent_teams/tools/database/team_dao.py)
type TeamDao interface {
    // CreateTeam 创建团队。返回 true 表示成功，false 表示团队已存在（对齐 Python IntegrityError → False）
    CreateTeam(ctx context.Context, teamName, displayName, leaderMemberName, desc, prompt string) bool
    // GetTeam 获取团队信息。返回 nil 表示团队不存在（对齐 Python Optional[Team]）
    GetTeam(ctx context.Context, teamName string) (*Team, error)
    // TeamExists 团队是否存在
    TeamExists(ctx context.Context, teamName string) bool
    // DeleteTeam 删除团队（级联删除成员）。返回 true 表示删除成功，false 表示团队不存在
    DeleteTeam(ctx context.Context, teamName string) bool
    // GetTeamUpdatedAt 获取团队 updated_at 毫秒时间戳（用于变更检测）
    GetTeamUpdatedAt(ctx context.Context, teamName string) int64
}
```

**Python 对齐**：
- `CreateTeam`：新增 `prompt` 参数（Python 原有），返回 `bool`（Python 返回 `bool`，IntegrityError → False）
- `GetTeam`：返回 `*Team`（不再是 `any`），对齐 Python `Optional[Team]`
- `DeleteTeam`：返回 `bool`（Python 返回 `bool`）
- `GetTeamUpdatedAt`：新增，对齐 Python `get_team_updated_at() → int`

### 4.2 MemberDao 接口（4→8 方法，签名全面修订）

```go
// MemberDao 成员 DAO 接口。
// 对齐 Python: MemberDao (openjiuwen/agent_teams/tools/database/member_dao.py)
type MemberDao interface {
    // CreateMember 创建成员。返回 true 表示成功，false 表示成员已存在或 DB 拒绝
    CreateMember(ctx context.Context, memberName, teamName, displayName, agentCard, status, role, desc, executionStatus, mode, prompt, modelRefJSON string) bool
    // GetMember 获取成员信息。返回 nil 表示成员不存在（对齐 Python Optional[TeamMember]）
    GetMember(ctx context.Context, memberName, teamName string) (*TeamMember, error)
    // GetTeamMembers 获取团队成员列表，可选按 status 过滤（对齐 Python get_team_members(team, status=None)）
    GetTeamMembers(ctx context.Context, teamName string, status string) ([]*TeamMember, error)
    // UpdateMemberStatus 更新成员状态（含 FSM 校验）。返回 true 表示成功，false 表示成员不存在或转换不合法
    UpdateMemberStatus(ctx context.Context, memberName, teamName, status string) bool
    // TryTransitionMemberStatus CAS 原子状态转换（对齐 Python try_transition_member_status）
    // 仅当当前状态 == fromStatus 时才更新为 toStatus，否则返回 false
    TryTransitionMemberStatus(ctx context.Context, memberName, teamName string, fromStatus, toStatus MemberStatus) bool
    // ListHumanAgentNames 获取 human_agent 角色的成员名列表（HITT 名册重建）
    ListHumanAgentNames(ctx context.Context, teamName string) ([]string, error)
    // GetMembersMaxUpdatedAt 获取 MAX(updated_at)（成员变更检测）
    GetMembersMaxUpdatedAt(ctx context.Context, teamName string) int64
    // UpdateMemberExecutionStatus 更新执行状态（含 FSM 校验）
    UpdateMemberExecutionStatus(ctx context.Context, memberName, teamName, executionStatus string) bool
}
```

**Python 对齐**：
- `CreateMember`：补全 6 个缺失参数（`status/executionStatus/mode/prompt/modelRefJSON`），对齐 Python 11 参数
- `GetMember`：返回 `*TeamMember`（不再是 `any`），参数顺序 `(memberName, teamName)` 对齐 Python
- `GetTeamMembers`：新增可选 `status` 过滤参数，返回 `[]*TeamMember`
- `UpdateMemberStatus`：返回 `bool`（不再是 `error`），FSM 校验失败返回 false
- `TryTransitionMemberStatus`：新增（CAS 原子转换，Python `try_transition_member_status`）
- `ListHumanAgentNames`：新增（HITT 名册重建）
- `GetMembersMaxUpdatedAt`：新增（变更检测）
- `UpdateMemberExecutionStatus`：新增（含 FSM 校验）

### 4.3 TeamDatabase 门面接口（新增 ForceDeleteTeamSession）

```go
type TeamDatabase interface {
    Initialize(ctx context.Context) error
    CreateCurSessionTables(ctx context.Context) error
    DropCurSessionTables(ctx context.Context) error
    CleanupAllRuntimeState(ctx context.Context) (droppedTables []string, droppedDirs []string, err error)
    DropSessionTablesByID(ctx context.Context, sessionID string) ([]string, error)
    // ForceDeleteTeamSession 跨表拆卸：删除 team_info 行 + drop 会话动态表（对齐 Python）
    ForceDeleteTeamSession(ctx context.Context, teamName string) bool
    Close() error
    Team() TeamDao
    Member() MemberDao
    Task() TaskDao       // ⤵️ 9.65a-2 回填具体方法
    Message() MessageDao // ⤵️ 9.65a-3 回填具体方法
}
```

### 4.4 TaskDao / MessageDao 接口

保持当前占位不变（`⤵️ 9.65a-2/9.65a-3 回填`），但接口定义需要在 `database.go` 中保留，确保 InMemoryTeamDatabase 的 `Task()` / `Message()` 方法能编译通过。9.65a-2/3 回填时再补全方法签名。

---

## 5. InMemoryTeamDatabase 单体实现

### 5.1 结构体定义

对齐 Python `InMemoryTeamDatabase`（`tools/memory_database.py`）的单体设计：

```go
// InMemoryTeamDatabase 内存数据库替代实现。
// 对齐 Python: InMemoryTeamDatabase (openjiuwen/agent_teams/tools/memory_database.py)
//
// 单体结构体同时实现 TeamDatabase + TeamDao + MemberDao 接口，
// 对齐 Python 的 self.team = self / self.member = self 自引用设计。
// TaskDao 和 MessageDao 接口方法由 ⤵️ 9.65a-2/9.65a-3 回填。
type InMemoryTeamDatabase struct {
    // teams 团队数据，key=teamName
    teams map[string]*Team
    // members 成员数据，key=memberName+"\x00"+teamName（复合主键编码）
    members map[string]*TeamMember
    // initialized 是否已初始化
    initialized bool
    // mu 保护并发访问
    mu sync.Mutex
}
```

**复合主键编码**：`memberName+"\x00"+teamName`，用 null 字符分隔避免碰撞。Python 用复合主键 `(member_name, team_name)`，Go 用字符串编码等价表达。

### 5.2 构造函数与 TeamDatabase 接口方法

```go
// NewInMemoryTeamDatabase 创建内存数据库实例。
func NewInMemoryTeamDatabase() *InMemoryTeamDatabase {
    return &InMemoryTeamDatabase{
        teams:    make(map[string]*Team),
        members:  make(map[string]*TeamMember),
    }
}

// Initialize 初始化（InMemory 无需操作，直接标记已初始化）。
func (db *InMemoryTeamDatabase) Initialize(_ context.Context) error {
    db.mu.Lock()
    db.initialized = true
    db.mu.Unlock()
    return nil
}

// CreateCurSessionTables InMemory 模式下为 no-op（无动态表）。
func (db *InMemoryTeamDatabase) CreateCurSessionTables(_ context.Context) error { return nil }

// DropCurSessionTables InMemory 模式下为 no-op。
func (db *InMemoryTeamDatabase) DropCurSessionTables(_ context.Context) error { return nil }

// CleanupAllRuntimeState 清空所有 map（对齐 Python 清空所有 dict）。
func (db *InMemoryTeamDatabase) CleanupAllRuntimeState(_ context.Context) ([]string, []string, error) {
    db.mu.Lock()
    db.teams = make(map[string]*Team)
    db.members = make(map[string]*TeamMember)
    db.mu.Unlock()
    return nil, nil, nil
}

// DropSessionTablesByID InMemory 模式下为 no-op。
func (db *InMemoryTeamDatabase) DropSessionTablesByID(_ context.Context, _ string) ([]string, error) {
    return nil, nil
}

// ForceDeleteTeamSession 跨表拆卸：删 team 行 + 删该 team 下所有成员。
// 对齐 Python: force_delete_team_session(team_name) — 删 team_info 行 + drop 会话表
func (db *InMemoryTeamDatabase) ForceDeleteTeamSession(ctx context.Context, teamName string) bool {
    db.mu.Lock()
    _, exists := db.teams[teamName]
    delete(db.teams, teamName)
    // 删除该 team 下所有成员
    for key, member := range db.members {
        if member.TeamName == teamName {
            delete(db.members, key)
        }
    }
    db.mu.Unlock()
    return exists // 对齐 Python：不存在也视为成功，但返回实际删除状态
}

// Close 关闭数据库（清空所有数据）。
func (db *InMemoryTeamDatabase) Close() error {
    db.mu.Lock()
    db.teams = nil
    db.members = nil
    db.initialized = false
    db.mu.Unlock()
    return nil
}

// Team 返回 TeamDao（自引用：self.team = self）。
func (db *InMemoryTeamDatabase) Team() TeamDao { return db }

// Member 返回 MemberDao（自引用：self.member = self）。
func (db *InMemoryTeamDatabase) Member() MemberDao { return db }

// Task 返回 TaskDao（⤵️ 9.65a-2 回填后返回 db）。
func (db *InMemoryTeamDatabase) Task() TaskDao { return db }

// Message 返回 MessageDao（⤵️ 9.65a-3 回填后返回 db）。
func (db *InMemoryTeamDatabase) Message() MessageDao { return db }
```

### 5.3 TeamDao 接口实现

```go
// CreateTeam 创建团队。对齐 Python: TeamDao.create_team()
// 成功返回 true，团队已存在返回 false（对齐 Python IntegrityError → False）
func (db *InMemoryTeamDatabase) CreateTeam(_ context.Context, teamName, displayName, leaderMemberName, desc, prompt string) bool {
    db.mu.Lock()
    defer db.mu.Unlock()
    if _, exists := db.teams[teamName]; exists {
        return false // 对齐 Python IntegrityError → False
    }
    ts := GetCurrentTime()
    db.teams[teamName] = &Team{
        TeamName:         teamName,
        DisplayName:      displayName,
        LeaderMemberName: leaderMemberName,
        Desc:             desc,
        Prompt:           prompt,
        Created:          ts,
        UpdatedAt:        ts,
    }
    return true
}

// GetTeam 获取团队信息。对齐 Python: TeamDao.get_team()
func (db *InMemoryTeamDatabase) GetTeam(_ context.Context, teamName string) (*Team, error) {
    db.mu.Lock()
    defer db.mu.Unlock()
    team, exists := db.teams[teamName]
    if !exists {
        return nil, nil // 对齐 Python Optional[Team] → None
    }
    return team, nil
}

// TeamExists 团队是否存在。对齐 Python: TeamDao.team_exists()
func (db *InMemoryTeamDatabase) TeamExists(_ context.Context, teamName string) bool {
    db.mu.Lock()
    defer db.mu.Unlock()
    _, exists := db.teams[teamName]
    return exists
}

// DeleteTeam 删除团队（级联删除成员）。对齐 Python: TeamDao.delete_team()
func (db *InMemoryTeamDatabase) DeleteTeam(_ context.Context, teamName string) bool {
    db.mu.Lock()
    defer db.mu.Unlock()
    _, exists := db.teams[teamName]
    if !exists {
        return false // 对齐 Python: team not found → False
    }
    delete(db.teams, teamName)
    // 级联删除成员（对齐 Python CASCADE on delete）
    for key, member := range db.members {
        if member.TeamName == teamName {
            delete(db.members, key)
        }
    }
    return true
}

// GetTeamUpdatedAt 获取团队 updated_at 毫秒时间戳。对齐 Python: TeamDao.get_team_updated_at()
func (db *InMemoryTeamDatabase) GetTeamUpdatedAt(_ context.Context, teamName string) int64 {
    db.mu.Lock()
    defer db.mu.Unlock()
    team, exists := db.teams[teamName]
    if !exists || team.UpdatedAt == 0 {
        return 0 // 对齐 Python: missing → 0
    }
    return team.UpdatedAt
}
```

### 5.4 MemberDao 接口实现

```go
// memberKey 构造复合主键 key。
func memberKey(memberName, teamName string) string {
    return memberName + "\x00" + teamName
}

// CreateMember 创建成员。对齐 Python: MemberDao.create_member()
// 成功返回 true，成员已存在返回 false（对齐 Python IntegrityError → False）
func (db *InMemoryTeamDatabase) CreateMember(_ context.Context, memberName, teamName, displayName, agentCard, status, role, desc, executionStatus, mode, prompt, modelRefJSON string) bool {
    db.mu.Lock()
    defer db.mu.Unlock()
    key := memberKey(memberName, teamName)
    if _, exists := db.members[key]; exists {
        return false // 对齐 Python IntegrityError → False
    }
    db.members[key] = &TeamMember{
        MemberName:      memberName,
        TeamName:        teamName,
        DisplayName:     displayName,
        Desc:            desc,
        AgentCard:       agentCard,
        Status:          status,
        ExecutionStatus: executionStatus,
        Mode:            mode,
        Role:            role,
        Prompt:          prompt,
        ModelRefJSON:    modelRefJSON,
        UpdatedAt:       GetCurrentTime(), // 对齐 Python: updated_at = get_current_time()
    }
    return true
}

// GetMember 获取成员信息。对齐 Python: MemberDao.get_member()
func (db *InMemoryTeamDatabase) GetMember(_ context.Context, memberName, teamName string) (*TeamMember, error) {
    db.mu.Lock()
    defer db.mu.Unlock()
    member, exists := db.members[memberKey(memberName, teamName)]
    if !exists {
        return nil, nil // 对齐 Python Optional[TeamMember] → None
    }
    return member, nil
}

// GetTeamMembers 获取团队成员列表，可选按 status 过滤。对齐 Python: MemberDao.get_team_members(team, status=None)
func (db *InMemoryTeamDatabase) GetTeamMembers(_ context.Context, teamName string, status string) ([]*TeamMember, error) {
    db.mu.Lock()
    defer db.mu.Unlock()
    var result []*TeamMember
    for _, member := range db.members {
        if member.TeamName != teamName {
            continue
        }
        if status != "" && member.Status != status {
            continue // 对齐 Python: status 过滤
        }
        result = append(result, member)
    }
    return result, nil
}

// UpdateMemberStatus 更新成员状态（含 FSM 校验）。对齐 Python: MemberDao.update_member_status()
// 返回 true 表示成功，false 表示成员不存在或状态转换不合法
func (db *InMemoryTeamDatabase) UpdateMemberStatus(_ context.Context, memberName, teamName, status string) bool {
    db.mu.Lock()
    defer db.mu.Unlock()
    key := memberKey(memberName, teamName)
    member, exists := db.members[key]
    if !exists {
        return false // 对齐 Python: member not found → False
    }
    // FSM 校验（对齐 Python: is_valid_transition）
    if !IsValidMemberTransition(MemberStatus(member.Status), MemberStatus(status)) {
        return false // 对齐 Python: invalid transition → False
    }
    member.Status = status
    return true
}

// TryTransitionMemberStatus CAS 原子状态转换。对齐 Python: MemberDao.try_transition_member_status()
// 仅当当前状态 == fromStatus 时才更新为 toStatus，否则返回 false
func (db *InMemoryTeamDatabase) TryTransitionMemberStatus(_ context.Context, memberName, teamName string, fromStatus, toStatus MemberStatus) bool {
    db.mu.Lock()
    defer db.mu.Unlock()
    key := memberKey(memberName, teamName)
    member, exists := db.members[key]
    if !exists {
        return false
    }
    if MemberStatus(member.Status) != fromStatus {
        return false // 对齐 Python: rowcount == 0 → False (CAS 失败)
    }
    member.Status = string(toStatus)
    return true // 对齐 Python: rowcount == 1 → True (CAS 成功)
}

// ListHumanAgentNames 获取 human_agent 角色的成员名列表。对齐 Python: MemberDao.list_human_agent_names()
func (db *InMemoryTeamDatabase) ListHumanAgentNames(_ context.Context, teamName string) ([]string, error) {
    db.mu.Lock()
    defer db.mu.Unlock()
    var result []string
    for _, member := range db.members {
        if member.TeamName == teamName && member.Role == "human_agent" {
            result = append(result, member.MemberName)
        }
    }
    return result, nil
}

// GetMembersMaxUpdatedAt 获取 MAX(updated_at)。对齐 Python: MemberDao.get_members_max_updated_at()
func (db *InMemoryTeamDatabase) GetMembersMaxUpdatedAt(_ context.Context, teamName string) int64 {
    db.mu.Lock()
    defer db.mu.Unlock()
    var maxVal int64
    for _, member := range db.members {
        if member.TeamName == teamName && member.UpdatedAt > maxVal {
            maxVal = member.UpdatedAt
        }
    }
    return maxVal // 对齐 Python: 无数据返回 0
}

// UpdateMemberExecutionStatus 更新执行状态（含 FSM 校验）。
// 对齐 Python: MemberDao.update_member_execution_status()
func (db *InMemoryTeamDatabase) UpdateMemberExecutionStatus(_ context.Context, memberName, teamName, executionStatus string) bool {
    db.mu.Lock()
    defer db.mu.Unlock()
    key := memberKey(memberName, teamName)
    member, exists := db.members[key]
    if !exists {
        return false
    }
    if !IsValidExecutionTransition(ExecutionStatus(member.ExecutionStatus), ExecutionStatus(executionStatus)) {
        return false
    }
    member.ExecutionStatus = executionStatus
    return true
}
```

**FSM 校验引用**：`MemberDao.UpdateMemberStatus` 和 `TryTransitionMemberStatus` 调用 `schema.IsValidMemberTransition()`，对齐 Python 的 `is_valid_transition()` + `MEMBER_TRANSITIONS`。需要 import `schema` 包。

---

## 6. engine.go 重写

新增时间工具函数和常量定义（SQL 实现部分保持占位）：

```go
package database

import (
    "context"
    "crypto/blake2s"
    "encoding/hex"
    "time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCurrentTime 返回当前毫秒时间戳。对齐 Python: get_current_time()
func GetCurrentTime() int64 {
    return time.Now().UnixMilli()
}

// SanitizeSessionIDForTable 将 session_id 转为 SQL-safe 的固定长度 hex 后缀。
// 对齐 Python: _sanitize_session_id_for_table(session_id)
// 使用 BLAKE2s(digest_size=8) → 16 hex chars
func SanitizeSessionIDForTable(sessionID string) string {
    h, _ := blake2s.New(8, nil)
    h.Write([]byte(sessionID))
    return hex.EncodeToString(h.Sum(nil))
}

// InitializeEngine 初始化数据库引擎。⤵️ 9.65a-5 SQL 实现时回填
func InitializeEngine(_ context.Context, _ DBConfigProvider) (any, error) { return nil, nil }

// CreateCurSessionTablesFromEngine 创建当前会话动态表。⤵️ 9.65a-5
func CreateCurSessionTablesFromEngine(_ context.Context, _ any) error { return nil }

// DropCurSessionTablesFromEngine 删除当前会话动态表。⤵️ 9.65a-5
func DropCurSessionTablesFromEngine(_ context.Context, _ any) error { return nil }

// CleanupAllRuntimeStateFromEngine 清理所有运行时状态。⤵️ 9.65a-5
func CleanupAllRuntimeStateFromEngine(_ context.Context, _ any) ([]string, []string, error) {
    return nil, nil, nil
}

// DropSessionTablesByIDFromEngine 按 ID 删除动态表。⤵️ 9.65a-5
func DropSessionTablesByIDFromEngine(_ context.Context, _ any, _ string) ([]string, error) {
    return nil, nil
}
```

---

## 7. 回填影响清单

### 7.1 直接回填（本章节完成后）

| 文件 | 行号 | 当前占位 | 回填内容 |
|------|------|---------|---------|
| `agent/infra.go` | 15 | `TeamBackend any` | 可改为 `*TeamBackend`（但 9.65a-4 才有具体类型，本次仍为 `any`） |
| `agent/infra.go` | 21-24 | `TaskManager/MessageManager any` | 仍为 `any`（9.65a-2/3 才有具体类型） |
| `agent/spawn_manager.go` | 268 | `TODO(#9.64): 更新 DB 状态为 ERROR` | 可回填为 `db.Member().UpdateMemberStatus()`，但需要 TeamBackend 具体类型 |
| `spawn/shared_resources.go` | 62 | `TODO(#9.64): 解析 config.db_type` | 可回填为 `GetSharedDB()` 返回 InMemoryTeamDatabase |
| `tools/memory_database.go` | 整文件 | 空接口定义 | 删除（迁移到 database 包） |
| `tools/models.go` | 整文件 | TeamInfo/TeamMemberInfo | 删除（迁移到 database/models.go，重命名补全） |

### 7.2 间接解锁（后续章节才能回填）

| 章节 | 解锁的回填 |
|------|----------|
| 9.65a-2 | `InMemoryTeamDatabase.Task()` 的 TaskDao 方法实现 |
| 9.65a-3 | `InMemoryTeamDatabase.Message()` 的 MessageDao 方法实现 |
| 9.65a-4 | `infra.TeamBackend` 从 `any` → `*TeamBackend`；AgentConfigurator.SetupTeamBackend 回填；TeamAgent.AutoStartMember/AutoStartAll 回填 |
| 9.61 | RecoveryManager 依赖 `RecoveryBackend` 接口（从 TeamBackend 提取） |

---

## 8. 测试覆盖要求

### 8.1 models_test.go

- Team/TeamMember JSON 序列化/反序列化
- TeamMember 所有字段的 round-trip 测试
- `omitempty` 字段零值行为

### 8.2 memory_impl_test.go

**TeamDao 测试**（5 个方法）：
- `TestCreateTeam_成功` — 创建新团队返回 true
- `TestCreateTeam_已存在` — 重复创建返回 false
- `TestGetTeam_存在` — 查到返回 *Team
- `TestGetTeam_不存在` — 查不到返回 nil
- `TestTeamExists_存在/不存在`
- `TestDeleteTeam_成功` — 删除成功 + 级联删成员
- `TestDeleteTeam_不存在` — 返回 false
- `TestGetTeamUpdatedAt_存在/不存在/零值`

**MemberDao 测试**（8 个方法）：
- `TestCreateMember_成功` — 所有参数完整创建
- `TestCreateMember_已存在` — 返回 false
- `TestGetMember_存在/不存在`
- `TestGetTeamMembers_全部/按状态过滤`
- `TestUpdateMemberStatus_合法转换` — READY→BUSY 返回 true
- `TestUpdateMemberStatus_非法转换` — UNSTARTED→BUSY 返回 false
- `TestUpdateMemberStatus_成员不存在` — 返回 false
- `TestTryTransitionMemberStatus_CAS成功` — from_status 匹配时更新
- `TestTryTransitionMemberStatus_CAS失败` — from_status 不匹配时返回 false
- `TestListHumanAgentNames_有/无_human_agent`
- `TestGetMembersMaxUpdatedAt_有/无数据`
- `TestUpdateMemberExecutionStatus_合法/非法`

**TeamDatabase 门面测试**：
- `TestInMemoryTeamDatabase_Initialize`
- `TestInMemoryTeamDatabase_CreateCurSessionTables_noop`
- `TestInMemoryTeamDatabase_CleanupAllRuntimeState`
- `TestInMemoryTeamDatabase_ForceDeleteTeamSession`
- `TestInMemoryTeamDatabase_Close`
- `TestInMemoryTeamDatabase_Team_自引用` — `db.Team() == db`
- `TestInMemoryTeamDatabase_Member_自引用` — `db.Member() == db`

### 8.3 engine_test.go

- `TestGetCurrentTime` — 返回 int64 毫秒时间戳
- `TestSanitizeSessionIDForTable` — 不同 sessionID 产生不同 16 字符 hex 后缀
- `TestSanitizeSessionIDForTable_幂等` — 同一 sessionID 多次调用结果一致

### 8.4 覆盖率目标

≥ 85%（InMemory 实现全部是可 mock 的纯逻辑，无外部依赖）
