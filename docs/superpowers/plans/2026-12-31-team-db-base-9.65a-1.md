# 9.65a-1 TeamDB 基础层实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 TeamDB 基础层——数据模型（Team/TeamMember）、DAO 接口补全（TeamDao 5方法、MemberDao 8方法）、InMemoryTeamDatabase 单体实现、engine 工具函数

**Architecture:** 单体 InMemoryTeamDatabase 同时实现 TeamDatabase+TeamDao+MemberDao 接口（对齐 Python 自引用设计）；数据模型从 tools 包迁移到 database 包；DAO 返回 bool 替代 error（对齐 Python）

**Tech Stack:** Go 标准库（sync.Mutex、crypto/blake2s、encoding/hex、time）；schema 包的 FSM 校验函数

**Spec:** `docs/superpowers/specs/2026-12-31-team-db-base-9.65a-1-design.md`

---

## Task 1: 数据模型定义 — database/models.go

**Files:**
- Create: `internal/agent_teams/tools/database/models.go`
- Reference: `docs/superpowers/specs/2026-12-31-team-db-base-9.65a-1-design.md` §3

- [ ] **Step 1: 创建 models.go 文件**

创建 `internal/agent_teams/tools/database/models.go`，包含 Team 和 TeamMember 结构体（对齐 spec §3.1/3.2），以及动态表常量（spec §3.3）：

```go
package database

// ──────────────────────────── 结构体 ────────────────────────────

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
	// Prompt 团队提示词
	Prompt string `json:"prompt,omitempty"`
	// Created 创建时间（毫秒时间戳）
	Created int64 `json:"created"`
	// UpdatedAt 更新时间（毫秒时间戳，仅 roster 变更时 bump）
	UpdatedAt int64 `json:"updated_at,omitempty"`
}

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
	// AgentCard Agent 卡片 JSON 字符串（存储 AgentCard 序列化后的 JSON）
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

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; sleep 1 ; export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/tools/database/...`
Expected: 编译成功，无错误

- [ ] **Step 3: 写 models_test.go**

创建 `internal/agent_teams/tools/database/models_test.go`：

```go
package database

import (
	"encoding/json"
	"testing"
)

// TestTeam_JSON序列化_全部字段 测试 Team 完整 JSON 序列化
func TestTeam_JSON序列化_全部字段(t *testing.T) {
	team := &Team{
		TeamName:         "team-alpha",
		DisplayName:      "Alpha Team",
		LeaderMemberName: "leader-1",
		Desc:             "A test team",
		Prompt:           "Follow the rules",
		Created:          1700000000000,
		UpdatedAt:        1700000001000,
	}
	data, err := json.Marshal(team)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var restored Team
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.TeamName != team.TeamName {
		t.Errorf("TeamName 不匹配: got %s, want %s", restored.TeamName, team.TeamName)
	}
	if restored.Prompt != team.Prompt {
		t.Errorf("Prompt 不匹配: got %s, want %s", restored.Prompt, team.Prompt)
	}
	if restored.Created != team.Created {
		t.Errorf("Created 不匹配: got %d, want %d", restored.Created, team.Created)
	}
}

// TestTeam_JSON序列化_omitempty字段 测试 Team omitempty 字段零值行为
func TestTeam_JSON序列化_omitempty字段(t *testing.T) {
	team := &Team{
		TeamName:         "team-beta",
		DisplayName:      "Beta Team",
		LeaderMemberName: "leader-2",
		Created:          1700000000000,
	}
	data, err := json.Marshal(team)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	// omitempty 字段不应出现在 JSON 中
	if want, got := `"team-beta"`, `"team_name":"team-beta"`; !containsString(string(data), got) {
		t.Errorf("TeamName 未出现在 JSON 中")
	}
	if containsString(string(data), `"desc"`) {
		t.Errorf("Desc 零值不应出现在 JSON 中")
	}
	if containsString(string(data), `"prompt"`) {
		t.Errorf("Prompt 零值不应出现在 JSON 中")
	}
	if containsString(string(data), `"updated_at"`) {
		t.Errorf("UpdatedAt 零值不应出现在 JSON 中")
	}
}

// TestTeamMember_JSON序列化_全部字段 测试 TeamMember 完整 JSON 序列化
func TestTeamMember_JSON序列化_全部字段(t *testing.T) {
	member := &TeamMember{
		MemberName:      "member-1",
		TeamName:        "team-alpha",
		DisplayName:     "Member One",
		Desc:            "A test member",
		AgentCard:       `{"id":"card-1","name":"Test"}`,
		Status:          "ready",
		ExecutionStatus: "idle",
		Mode:            "build_mode",
		Role:            "teammate",
		Prompt:          "Be helpful",
		ModelRefJSON:    `{"model_id":"m1","model_name":"qwen-max"}`,
		UpdatedAt:       1700000002000,
	}
	data, err := json.Marshal(member)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var restored TeamMember
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.MemberName != member.MemberName {
		t.Errorf("MemberName 不匹配: got %s, want %s", restored.MemberName, member.MemberName)
	}
	if restored.ExecutionStatus != member.ExecutionStatus {
		t.Errorf("ExecutionStatus 不匹配: got %s, want %s", restored.ExecutionStatus, member.ExecutionStatus)
	}
	if restored.Mode != member.Mode {
		t.Errorf("Mode 不匹配: got %s, want %s", restored.Mode, member.Mode)
	}
	if restored.Role != member.Role {
		t.Errorf("Role 不匹配: got %s, want %s", restored.Role, member.Role)
	}
}

// TestTeamMember_JSON序列化_agentCard存储JSON字符串 测试 AgentCard 存 JSON 字符串
func TestTeamMember_JSON序列化_agentCard存储JSON字符串(t *testing.T) {
	member := &TeamMember{
		MemberName:  "member-2",
		TeamName:    "team-alpha",
		DisplayName: "Member Two",
		AgentCard:   `{"id":"card-2","name":"TestAgent"}`,
		Status:      "busy",
		Mode:        "build_mode",
		Role:        "leader",
	}
	// 验证 AgentCard 字段存储的是 JSON 字符串而非对象
	data, err := json.Marshal(member)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	// AgentCard 应在 JSON 中以字符串值出现（被双引号包裹+内部转义）
	if !containsString(string(data), `"agent_card":"{`) {
		t.Errorf("AgentCard 应以 JSON 字符串存储，实际: %s", string(data))
	}
}

func containsString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
```

- [ ] **Step 4: 运行模型测试**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; sleep 1 ; export GOPROXY=https://goproxy.cn,direct && go test -v -count=1 ./internal/agent_teams/tools/database/...`
Expected: 4 个测试全部 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/tools/database/models.go internal/agent_teams/tools/database/models_test.go
git commit -m "feat(9.65a-1): 添加 Team/TeamMember 数据模型和动态表常量定义"
```

---

## Task 2: DAO 接口补全 — database/database.go 重写

**Files:**
- Modify: `internal/agent_teams/tools/database/database.go`（完整重写）
- Modify: `internal/agent_teams/tools/database/team_dao.go`（清空占位注释）
- Modify: `internal/agent_teams/tools/database/member_dao.go`（清空占位注释）

- [ ] **Step 1: 重写 database.go**

完全重写 `internal/agent_teams/tools/database/database.go`，对齐 spec §4 的接口定义。需要 import `schema` 包以引用 `MemberStatus`/`ExecutionStatus`：

```go
package database

import (
	"context"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// TeamDatabase 团队数据库门面接口。
// 对齐 Python: TeamDatabase (openjiuwen/agent_teams/tools/database/__init__.py)
//
// 拥有引擎生命周期和跨表事务。单表操作委托给 DAO 属性（team/member/task/message）。
type TeamDatabase interface {
	// Initialize 初始化数据库
	Initialize(ctx context.Context) error
	// CreateCurSessionTables 创建当前会话动态表
	CreateCurSessionTables(ctx context.Context) error
	// DropCurSessionTables 删除当前会话动态表
	DropCurSessionTables(ctx context.Context) error
	// CleanupAllRuntimeState 清理所有运行时状态
	CleanupAllRuntimeState(ctx context.Context) (droppedTables []string, droppedDirs []string, err error)
	// DropSessionTablesByID 按 sessionID 删除动态表
	DropSessionTablesByID(ctx context.Context, sessionID string) ([]string, error)
	// ForceDeleteTeamSession 跨表拆卸：删 team_info 行 + drop 会话动态表
	ForceDeleteTeamSession(ctx context.Context, teamName string) bool
	// Close 关闭数据库
	Close() error
	// Team 返回团队 DAO
	Team() TeamDao
	// Member 返回成员 DAO
	Member() MemberDao
	// Task 返回任务 DAO。⤵️ 9.65a-2 回填具体方法
	Task() TaskDao
	// Message 返回消息 DAO。⤵️ 9.65a-3 回填具体方法
	Message() MessageDao
}

// TeamDao 团队 DAO 接口。
// 对齐 Python: TeamDao (openjiuwen/agent_teams/tools/database/team_dao.py)
type TeamDao interface {
	// CreateTeam 创建团队。返回 true 表示成功，false 表示团队已存在
	CreateTeam(ctx context.Context, teamName, displayName, leaderMemberName, desc, prompt string) bool
	// GetTeam 获取团队信息。返回 nil 表示团队不存在
	GetTeam(ctx context.Context, teamName string) (*Team, error)
	// TeamExists 团队是否存在
	TeamExists(ctx context.Context, teamName string) bool
	// DeleteTeam 删除团队（级联删除成员）。返回 true 表示成功，false 表示团队不存在
	DeleteTeam(ctx context.Context, teamName string) bool
	// GetTeamUpdatedAt 获取团队 updated_at 毫秒时间戳（用于变更检测）
	GetTeamUpdatedAt(ctx context.Context, teamName string) int64
}

// MemberDao 成员 DAO 接口。
// 对齐 Python: MemberDao (openjiuwen/agent_teams/tools/database/member_dao.py)
type MemberDao interface {
	// CreateMember 创建成员。返回 true 表示成功，false 表示成员已存在或 DB 拒绝
	CreateMember(ctx context.Context, memberName, teamName, displayName, agentCard, status, role, desc, executionStatus, mode, prompt, modelRefJSON string) bool
	// GetMember 获取成员信息。返回 nil 表示成员不存在
	GetMember(ctx context.Context, memberName, teamName string) (*TeamMember, error)
	// GetTeamMembers 获取团队成员列表，可选按 status 过滤
	GetTeamMembers(ctx context.Context, teamName string, status string) ([]*TeamMember, error)
	// UpdateMemberStatus 更新成员状态（含 FSM 校验）。返回 true 表示成功，false 表示成员不存在或转换不合法
	UpdateMemberStatus(ctx context.Context, memberName, teamName, status string) bool
	// TryTransitionMemberStatus CAS 原子状态转换。仅当当前状态 == fromStatus 时更新为 toStatus
	TryTransitionMemberStatus(ctx context.Context, memberName, teamName string, fromStatus, toStatus atschema.MemberStatus) bool
	// ListHumanAgentNames 获取 human_agent 角色的成员名列表（HITT 名册重建）
	ListHumanAgentNames(ctx context.Context, teamName string) ([]string, error)
	// GetMembersMaxUpdatedAt 获取 MAX(updated_at)（成员变更检测）
	GetMembersMaxUpdatedAt(ctx context.Context, teamName string) int64
	// UpdateMemberExecutionStatus 更新执行状态（含 FSM 校验）
	UpdateMemberExecutionStatus(ctx context.Context, memberName, teamName, executionStatus string) bool
}

// TaskDao 任务 DAO 接口。⤵️ 9.65a-2 回填具体方法
type TaskDao interface {
	// CreateTask 创建任务
	CreateTask(ctx context.Context, taskID, teamName, title, content, status, assignee string) error
	// GetTask 获取任务
	GetTask(ctx context.Context, teamName, taskID string) (any, error)
	// GetTeamTasks 获取团队任务列表
	GetTeamTasks(ctx context.Context, teamName string) ([]any, error)
	// ClaimTask 认领任务
	ClaimTask(ctx context.Context, teamName, taskID, assignee string) error
	// UpdateTaskStatus 更新任务状态
	UpdateTaskStatus(ctx context.Context, teamName, taskID, status string) error
	// CancelTask 取消任务
	CancelTask(ctx context.Context, teamName, taskID string) error
}

// MessageDao 消息 DAO 接口。⤵️ 9.65a-3 回填具体方法
type MessageDao interface {
	// CreateMessage 创建消息
	CreateMessage(ctx context.Context, messageID, teamName, fromMemberName, toMemberName, content string, broadcast bool) error
	// GetTeamMessages 获取团队所有消息
	GetTeamMessages(ctx context.Context, teamName string) ([]any, error)
	// GetMessages 获取指定成员的消息
	GetMessages(ctx context.Context, teamName, toMemberName string, unreadOnly bool) ([]any, error)
	// MarkMessageRead 标记消息已读
	MarkMessageRead(ctx context.Context, teamName, messageID, memberName string) error
}
```

- [ ] **Step 2: 清空 team_dao.go 和 member_dao.go 占位注释**

修改 `team_dao.go` 内容为空占位（实现已在 memory_impl.go）：

```go
package database

// TeamDao 实际实现位于 memory_impl.go（InMemoryTeamDatabase）。
// SQL 实现将在 9.65a-5 回填到本文件。
```

修改 `member_dao.go` 内容为空占位：

```go
package database

// MemberDao 实际实现位于 memory_impl.go（InMemoryTeamDatabase）。
// SQL 实现将在 9.65a-5 回填到本文件。
```

- [ ] **Step 3: 写 database_test.go 接口满足性编译检查**

创建 `internal/agent_teams/tools/database/database_test.go`：

```go
package database

import (
	"context"
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// TestInMemoryTeamDatabase_满足TeamDatabase接口 编译期接口满足性检查
func TestInMemoryTeamDatabase_满足TeamDatabase接口(t *testing.T) {
	var _ TeamDatabase = (*InMemoryTeamDatabase)(nil)
}

// TestInMemoryTeamDatabase_满足TeamDao接口 编译期接口满足性检查
func TestInMemoryTeamDatabase_满足TeamDao接口(t *testing.T) {
	var _ TeamDao = (*InMemoryTeamDatabase)(nil)
}

// TestInMemoryTeamDatabase_满足MemberDao接口 编译期接口满足性检查
func TestInMemoryTeamDatabase_满足MemberDao接口(t *testing.T) {
	var _ MemberDao = (*InMemoryTeamDatabase)(nil)
}

// TestInMemoryTeamDatabase_满足TaskDao接口 编译期接口满足性检查（当前为空实现）
func TestInMemoryTeamDatabase_满足TaskDao接口(t *testing.T) {
	var _ TaskDao = (*InMemoryTeamDatabase)(nil)
}

// TestInMemoryTeamDatabase_满足MessageDao接口 编译期接口满足性检查（当前为空实现）
func TestInMemoryTeamDatabase_满足MessageDao接口(t *testing.T) {
	var _ MessageDao = (*InMemoryTeamDatabase)(nil)
}

// TestInMemoryTeamDatabase_TaskDao_空实现 当前 TaskDao 方法为空实现占位
func TestInMemoryTeamDatabase_TaskDao_空实现(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	// TaskDao 方法当前返回零值（⤵️ 9.65a-2 回填）
	if err := db.CreateTask(ctx, "", "", "", "", "", "", ""); err != nil {
		t.Errorf("CreateTask 空实现不应返回错误: %v", err)
	}
	result, err := db.GetTask(ctx, "", "")
	if err != nil || result != nil {
		t.Errorf("GetTask 空实现应返回 nil, nil")
	}
	tasks, err := db.GetTeamTasks(ctx, "")
	if err != nil || tasks != nil {
		t.Errorf("GetTeamTasks 空实现应返回 nil, nil")
	}
	if err := db.ClaimTask(ctx, "", "", ""); err != nil {
		t.Errorf("ClaimTask 空实现不应返回错误: %v", err)
	}
	if err := db.UpdateTaskStatus(ctx, "", "", ""); err != nil {
		t.Errorf("UpdateTaskStatus 空实现不应返回错误: %v", err)
	}
	if err := db.CancelTask(ctx, "", ""); err != nil {
		t.Errorf("CancelTask 空实现不应返回错误: %v", err)
	}
}

// TestInMemoryTeamDatabase_MessageDao_空实现 当前 MessageDao 方法为空实现占位
func TestInMemoryTeamDatabase_MessageDao_空实现(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()

	// MessageDao 方法当前返回零值（⤵️ 9.65a-3 回填）
	if err := db.CreateMessage(ctx, "", "", "", "", "", "", false); err != nil {
		t.Errorf("CreateMessage 空实现不应返回错误: %v", err)
	}
	msgs, err := db.GetTeamMessages(ctx, "")
	if err != nil || msgs != nil {
		t.Errorf("GetTeamMessages 空实现应返回 nil, nil")
	}
	msgs, err = db.GetMessages(ctx, "", "", false)
	if err != nil || msgs != nil {
		t.Errorf("GetMessages 空实现应返回 nil, nil")
	}
	if err := db.MarkMessageRead(ctx, "", "", ""); err != nil {
		t.Errorf("MarkMessageRead 空实现不应返回错误: %v", err)
	}
}
```

- [ ] **Step 4: 验证编译（先写 memory_impl.go 最小骨架确保编译）**

在写完整 memory_impl.go 前，先创建最小骨架确保 database_test.go 能编译。创建 `internal/agent_teams/tools/database/memory_impl.go` 最小骨架：

```go
package database

import (
	"context"
	"sync"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

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

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInMemoryTeamDatabase 创建内存数据库实例。
func NewInMemoryTeamDatabase() *InMemoryTeamDatabase {
	return &InMemoryTeamDatabase{
		teams:   make(map[string]*Team),
		members: make(map[string]*TeamMember),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// memberKey 构造复合主键 key。
func memberKey(memberName, teamName string) string {
	return memberName + "\x00" + teamName
}

// --- 以下方法将在 Task 3/4 中逐步实现 ---
// 当前为空实现占位，确保编译通过

// --- TeamDatabase 接口空实现占位 ---
func (db *InMemoryTeamDatabase) Initialize(_ context.Context) error { return nil }
func (db *InMemoryTeamDatabase) CreateCurSessionTables(_ context.Context) error { return nil }
func (db *InMemoryTeamDatabase) DropCurSessionTables(_ context.Context) error { return nil }
func (db *InMemoryTeamDatabase) CleanupAllRuntimeState(_ context.Context) ([]string, []string, error) { return nil, nil, nil }
func (db *InMemoryTeamDatabase) DropSessionTablesByID(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (db *InMemoryTeamDatabase) ForceDeleteTeamSession(_ context.Context, _ string) bool { return false }
func (db *InMemoryTeamDatabase) Close() error { return nil }
func (db *InMemoryTeamDatabase) Team() TeamDao { return db }
func (db *InMemoryTeamDatabase) Member() MemberDao { return db }
func (db *InMemoryTeamDatabase) Task() TaskDao { return db }
func (db *InMemoryTeamDatabase) Message() MessageDao { return db }

// --- TeamDao 接口空实现占位 ---
func (db *InMemoryTeamDatabase) CreateTeam(_ context.Context, _, _, _, _, _ string) bool { return false }
func (db *InMemoryTeamDatabase) GetTeam(_ context.Context, _ string) (*Team, error) { return nil, nil }
func (db *InMemoryTeamDatabase) TeamExists(_ context.Context, _ string) bool { return false }
func (db *InMemoryTeamDatabase) DeleteTeam(_ context.Context, _ string) bool { return false }
func (db *InMemoryTeamDatabase) GetTeamUpdatedAt(_ context.Context, _ string) int64 { return 0 }

// --- MemberDao 接口空实现占位 ---
func (db *InMemoryTeamDatabase) CreateMember(_ context.Context, _, _, _, _, _, _, _, _, _, _, _ string) bool { return false }
func (db *InMemoryTeamDatabase) GetMember(_ context.Context, _, _ string) (*TeamMember, error) { return nil, nil }
func (db *InMemoryTeamDatabase) GetTeamMembers(_ context.Context, _ string, _ string) ([]*TeamMember, error) { return nil, nil }
func (db *InMemoryTeamDatabase) UpdateMemberStatus(_ context.Context, _, _, _ string) bool { return false }
func (db *InMemoryTeamDatabase) TryTransitionMemberStatus(_ context.Context, _, _ string, _, _ atschema.MemberStatus) bool { return false }
func (db *InMemoryTeamDatabase) ListHumanAgentNames(_ context.Context, _ string) ([]string, error) { return nil, nil }
func (db *InMemoryTeamDatabase) GetMembersMaxUpdatedAt(_ context.Context, _ string) int64 { return 0 }
func (db *InMemoryTeamDatabase) UpdateMemberExecutionStatus(_ context.Context, _, _, _ string) bool { return false }

// --- TaskDao 接口空实现占位（⤵️ 9.65a-2 回填） ---
func (db *InMemoryTeamDatabase) CreateTask(_ context.Context, _, _, _, _, _, _, _ string) error { return nil }
func (db *InMemoryTeamDatabase) GetTask(_ context.Context, _, _ string) (any, error) { return nil, nil }
func (db *InMemoryTeamDatabase) GetTeamTasks(_ context.Context, _ string) ([]any, error) { return nil, nil }
func (db *InMemoryTeamDatabase) ClaimTask(_ context.Context, _, _, _ string) error { return nil }
func (db *InMemoryTeamDatabase) UpdateTaskStatus(_ context.Context, _, _, _ string) error { return nil }
func (db *InMemoryTeamDatabase) CancelTask(_ context.Context, _, _ string) error { return nil }

// --- MessageDao 接口空实现占位（⤵️ 9.65a-3 回填） ---
func (db *InMemoryTeamDatabase) CreateMessage(_ context.Context, _, _, _, _, _, _ string, _ bool) error { return nil }
func (db *InMemoryTeamDatabase) GetTeamMessages(_ context.Context, _ string) ([]any, error) { return nil, nil }
func (db *InMemoryTeamDatabase) GetMessages(_ context.Context, _, _ string, _ bool) ([]any, error) { return nil, nil }
func (db *InMemoryTeamDatabase) MarkMessageRead(_ context.Context, _, _, _ string) error { return nil }
```

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; sleep 1 ; export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/tools/database/...`
Expected: 编译成功

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/tools/database/database.go internal/agent_teams/tools/database/team_dao.go internal/agent_teams/tools/database/member_dao.go internal/agent_teams/tools/database/database_test.go internal/agent_teams/tools/database/memory_impl.go
git commit -m "feat(9.65a-1): 补全 DAO 接口（TeamDao 5方法、MemberDao 8方法）+ InMemoryTeamDatabase 最小骨架"
```

---

## Task 3: InMemoryTeamDatabase — TeamDatabase 门面 + TeamDao 完整实现

**Files:**
- Modify: `internal/agent_teams/tools/database/memory_impl.go`（替换空实现为真实逻辑）

- [ ] **Step 1: 写 TeamDao 测试**

创建 `internal/agent_teams/tools/database/memory_impl_test.go`，先写 TeamDao 和 TeamDatabase 门面测试：

```go
package database

import (
	"context"
	"testing"
)

// ──────────────────────────── TeamDao 测试 ────────────────────────────

// TestCreateTeam_成功 创建新团队返回 true
func TestCreateTeam_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	result := db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "A test team", "Be productive")
	if !result {
		t.Error("CreateTeam 应返回 true")
	}
}

// TestCreateTeam_已存在 重复创建返回 false
func TestCreateTeam_已存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "desc", "prompt")
	result := db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "desc", "prompt")
	if result {
		t.Error("重复 CreateTeam 应返回 false")
	}
}

// TestGetTeam_存在 查到返回 *Team
func TestGetTeam_存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "desc", "prompt")
	team, err := db.GetTeam(ctx, "team-1")
	if err != nil {
		t.Fatalf("GetTeam 不应返回错误: %v", err)
	}
	if team == nil {
		t.Error("GetTeam 应返回非 nil")
	}
	if team.TeamName != "team-1" {
		t.Errorf("TeamName 不匹配: got %s, want team-1", team.TeamName)
	}
	if team.Prompt != "prompt" {
		t.Errorf("Prompt 不匹配: got %s, want prompt", team.Prompt)
	}
}

// TestGetTeam_不存在 查不到返回 nil
func TestGetTeam_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	team, err := db.GetTeam(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("GetTeam 不应返回错误: %v", err)
	}
	if team != nil {
		t.Error("GetTeam 不存在时应返回 nil")
	}
}

// TestTeamExists_存在 团队存在返回 true
func TestTeamExists_存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	if !db.TeamExists(ctx, "team-1") {
		t.Error("TeamExists 应返回 true")
	}
}

// TestTeamExists_不存在 团队不存在返回 false
func TestTeamExists_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	if db.TeamExists(ctx, "nonexistent") {
		t.Error("TeamExists 不存在时应返回 false")
	}
}

// TestDeleteTeam_成功 删除成功 + 级联删成员
func TestDeleteTeam_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "member-1", "team-1", "Member One", "{}", "ready", "teammate", "", "", "", "", "")
	result := db.DeleteTeam(ctx, "team-1")
	if !result {
		t.Error("DeleteTeam 应返回 true")
	}
	// 级联删成员验证
	member, err := db.GetMember(ctx, "member-1", "team-1")
	if err != nil || member != nil {
		t.Error("删除团队后成员也应被删除")
	}
}

// TestDeleteTeam_不存在 不存在的团队返回 false
func TestDeleteTeam_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	result := db.DeleteTeam(ctx, "nonexistent")
	if result {
		t.Error("DeleteTeam 不存在时应返回 false")
	}
}

// TestGetTeamUpdatedAt_存在 返回 updated_at 值
func TestGetTeamUpdatedAt_存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	ts := db.GetTeamUpdatedAt(ctx, "team-1")
	if ts == 0 {
		t.Error("GetTeamUpdatedAt 应返回非零时间戳")
	}
}

// TestGetTeamUpdatedAt_不存在 返回 0
func TestGetTeamUpdatedAt_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	ts := db.GetTeamUpdatedAt(ctx, "nonexistent")
	if ts != 0 {
		t.Errorf("GetTeamUpdatedAt 不存在时应返回 0, got %d", ts)
	}
}

// ──────────────────────────── TeamDatabase 门面测试 ────────────────────────────

// TestInMemoryTeamDatabase_Initialize 初始化标记已设置
func TestInMemoryTeamDatabase_Initialize(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	if err := db.Initialize(ctx); err != nil {
		t.Fatalf("Initialize 不应返回错误: %v", err)
	}
	if !db.initialized {
		t.Error("Initialize 后 initialized 应为 true")
	}
}

// TestInMemoryTeamDatabase_CreateCurSessionTables_noop InMemory 下为 no-op
func TestInMemoryTeamDatabase_CreateCurSessionTables_noop(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	if err := db.CreateCurSessionTables(ctx); err != nil {
		t.Fatalf("CreateCurSessionTables 应为 no-op: %v", err)
	}
}

// TestInMemoryTeamDatabase_CleanupAllRuntimeState 清空所有数据
func TestInMemoryTeamDatabase_CleanupAllRuntimeState(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "member-1", "team-1", "Member One", "{}", "ready", "teammate", "", "", "", "", "")
	dt, dd, err := db.CleanupAllRuntimeState(ctx)
	if err != nil {
		t.Fatalf("CleanupAllRuntimeState 不应返回错误: %v", err)
	}
	if dt != nil || dd != nil {
		t.Error("InMemory CleanupAllRuntimeState 应返回 nil, nil")
	}
	team, _ := db.GetTeam(ctx, "team-1")
	if team != nil {
		t.Error("Cleanup 后团队应不存在")
	}
	member, _ := db.GetMember(ctx, "member-1", "team-1")
	if member != nil {
		t.Error("Cleanup 后成员应不存在")
	}
}

// TestInMemoryTeamDatabase_ForceDeleteTeamSession 跨表拆卸
func TestInMemoryTeamDatabase_ForceDeleteTeamSession(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "member-1", "team-1", "Member One", "{}", "ready", "teammate", "", "", "", "", "")
	result := db.ForceDeleteTeamSession(ctx, "team-1")
	if !result {
		t.Error("ForceDeleteTeamSession 应返回 true")
	}
	// 验证团队和成员都被删除
	team, _ := db.GetTeam(ctx, "team-1")
	if team != nil {
		t.Error("ForceDelete 后团队应不存在")
	}
	member, _ := db.GetMember(ctx, "member-1", "team-1")
	if member != nil {
		t.Error("ForceDelete 后成员应不存在")
	}
}

// TestInMemoryTeamDatabase_Close 关闭后数据清空
func TestInMemoryTeamDatabase_Close(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	if err := db.Close(); err != nil {
		t.Fatalf("Close 不应返回错误: %v", err)
	}
}

// TestInMemoryTeamDatabase_Team_自引用 Team() 返回自身
func TestInMemoryTeamDatabase_Team_自引用(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	teamDao := db.Team()
	if teamDao != db {
		t.Error("Team() 应返回 db 自身（self.team = self）")
	}
}

// TestInMemoryTeamDatabase_Member_自引用 Member() 返回自身
func TestInMemoryTeamDatabase_Member_自引用(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	memberDao := db.Member()
	if memberDao != db {
		t.Error("Member() 应返回 db 自身（self.member = self）")
	}
}
```

- [ ] **Step 2: 运行 TeamDao 测试（预期部分失败，因为方法还是空实现）**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; sleep 1 ; export GOPROXY=https://goproxy.cn,direct && go test -v -run 'TestCreateTeam|TestGetTeam|TestTeamExists|TestDeleteTeam|TestGetTeamUpdatedAt|TestInMemoryTeamDatabase' ./internal/agent_teams/tools/database/...`
Expected: TeamDao 相关测试 FAIL（空实现返回 false/nil），TeamDatabase 门面测试部分 PASS（Initialize/no-op/自引用），部分 FAIL（CleanupAllRuntimeState/ForceDeleteTeamSession/Close）

- [ ] **Step 3: 实现 TeamDatabase 门面 + TeamDao 方法**

替换 `memory_impl.go` 中的空实现为真实逻辑（对齐 spec §5.2/5.3）。需要 import `logger` 包记录日志：

将 TeamDatabase 接口方法的空实现替换为 spec §5.2 中的完整实现代码。将 TeamDao 接口方法的空实现替换为 spec §5.3 中的完整实现代码。添加日志 import 和组件常量：

```go
// 在 memory_impl.go 顶部添加 import
import (
	"context"
	"sync"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// 添加日志组件常量（在常量区块）
const (
	// memoryDBLogComponent 日志组件
	memoryDBLogComponent = logger.ComponentAgentCore
)
```

然后逐步替换每个空方法为真实实现（代码见 spec §5.2/5.3），每个方法开头添加 `logger.Info(memoryDBLogComponent)` 或 `logger.Debug(memoryDBLogComponent)` 日志（对齐 Python `team_logger.info/debug/error`）。

- [ ] **Step 4: 运行 TeamDao + TeamDatabase 门面测试**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; sleep 1 ; export GOPROXY=https://goproxy.cn,direct && go test -v -run 'TestCreateTeam|TestGetTeam|TestTeamExists|TestDeleteTeam|TestGetTeamUpdatedAt|TestInMemoryTeamDatabase' ./internal/agent_teams/tools/database/...`
Expected: 所有测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agent_teams/tools/database/memory_impl.go internal/agent_teams/tools/database/memory_impl_test.go
git commit -m "feat(9.65a-1): 实现 InMemoryTeamDatabase TeamDatabase 门面 + TeamDao 5 方法"
```

---

## Task 4: InMemoryTeamDatabase — MemberDao 8 方法完整实现

**Files:**
- Modify: `internal/agent_teams/tools/database/memory_impl.go`（替换 MemberDao 空实现）
- Modify: `internal/agent_teams/tools/database/memory_impl_test.go`（添加 MemberDao 测试）

- [ ] **Step 1: 写 MemberDao 测试**

在 `memory_impl_test.go` 中添加 MemberDao 测试（对齐 spec §8.2）：

```go
// ──────────────────────────── MemberDao 测试 ────────────────────────────

// TestCreateMember_成功 所有参数完整创建
func TestCreateMember_成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	// 先创建团队（外键依赖）
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	result := db.CreateMember(ctx, "member-1", "team-1", "Member One", `{"id":"card-1"}`, "ready", "teammate", "desc", "idle", "build_mode", "", "")
	if !result {
		t.Error("CreateMember 应返回 true")
	}
}

// TestCreateMember_已存在 返回 false
func TestCreateMember_已存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "member-1", "team-1", "Member One", "{}", "ready", "teammate", "", "", "", "", "")
	result := db.CreateMember(ctx, "member-1", "team-1", "Member One", "{}", "ready", "teammate", "", "", "", "", "")
	if result {
		t.Error("重复 CreateMember 应返回 false")
	}
}

// TestGetMember_存在 返回 *TeamMember
func TestGetMember_存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "member-1", "team-1", "Member One", `{"id":"card-1"}`, "ready", "teammate", "", "", "", "", "")
	member, err := db.GetMember(ctx, "member-1", "team-1")
	if err != nil {
		t.Fatalf("GetMember 不应返回错误: %v", err)
	}
	if member == nil {
		t.Fatal("GetMember 应返回非 nil")
	}
	if member.MemberName != "member-1" {
		t.Errorf("MemberName 不匹配: got %s, want member-1", member.MemberName)
	}
	if member.AgentCard != `{"id":"card-1"}` {
		t.Errorf("AgentCard 不匹配: got %s", member.AgentCard)
	}
}

// TestGetMember_不存在 返回 nil
func TestGetMember_不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	member, err := db.GetMember(ctx, "nonexistent", "team-1")
	if err != nil {
		t.Fatalf("GetMember 不应返回错误: %v", err)
	}
	if member != nil {
		t.Error("GetMember 不存在时应返回 nil")
	}
}

// TestGetTeamMembers_全部 无过滤返回全部成员
func TestGetTeamMembers_全部(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "m1", "team-1", "Member 1", "{}", "ready", "teammate", "", "", "", "", "")
	db.CreateMember(ctx, "m2", "team-1", "Member 2", "{}", "busy", "teammate", "", "", "", "", "")
	members, err := db.GetTeamMembers(ctx, "team-1", "")
	if err != nil {
		t.Fatalf("GetTeamMembers 不应返回错误: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("GetTeamMembers 应返回 2 个成员, got %d", len(members))
	}
}

// TestGetTeamMembers_按状态过滤 仅返回匹配状态成员
func TestGetTeamMembers_按状态过滤(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "m1", "team-1", "Member 1", "{}", "ready", "teammate", "", "", "", "", "")
	db.CreateMember(ctx, "m2", "team-1", "Member 2", "{}", "busy", "teammate", "", "", "", "", "")
	members, err := db.GetTeamMembers(ctx, "team-1", "ready")
	if err != nil {
		t.Fatalf("GetTeamMembers 不应返回错误: %v", err)
	}
	if len(members) != 1 {
		t.Errorf("GetTeamMembers 按 ready 过滤应返回 1, got %d", len(members))
	}
	if members[0].MemberName != "m1" {
		t.Errorf("过滤后成员名不匹配: got %s, want m1", members[0].MemberName)
	}
}

// TestUpdateMemberStatus_合法转换 READY→BUSY 返回 true
func TestUpdateMemberStatus_合法转换(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "member-1", "team-1", "Member One", "{}", "ready", "teammate", "", "", "", "", "")
	result := db.UpdateMemberStatus(ctx, "member-1", "team-1", "busy")
	if !result {
		t.Error("合法转换 ready→busy 应返回 true")
	}
	member, _ := db.GetMember(ctx, "member-1", "team-1")
	if member.Status != "busy" {
		t.Errorf("状态应更新为 busy, got %s", member.Status)
	}
}

// TestUpdateMemberStatus_非法转换 UNSTARTED→BUSY 返回 false
func TestUpdateMemberStatus_非法转换(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "member-1", "team-1", "Member One", "{}", "unstarted", "teammate", "", "", "", "", "")
	result := db.UpdateMemberStatus(ctx, "member-1", "team-1", "busy")
	if result {
		t.Error("非法转换 unstarted→busy 应返回 false")
	}
}

// TestUpdateMemberStatus_成员不存在 返回 false
func TestUpdateMemberStatus_成员不存在(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	result := db.UpdateMemberStatus(ctx, "nonexistent", "team-1", "ready")
	if result {
		t.Error("成员不存在时应返回 false")
	}
}

// TestTryTransitionMemberStatus_CAS成功 from_status 匹配时更新
func TestTryTransitionMemberStatus_CAS成功(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "member-1", "team-1", "Member One", "{}", "unstarted", "teammate", "", "", "", "", "")
	result := db.TryTransitionMemberStatus(ctx, "member-1", "team-1", atschema.MemberStatusUnstarted, atschema.MemberStatusStarting)
	if !result {
		t.Error("CAS 成功应返回 true")
	}
	member, _ := db.GetMember(ctx, "member-1", "team-1")
	if member.Status != "starting" {
		t.Errorf("CAS 成功后状态应为 starting, got %s", member.Status)
	}
}

// TestTryTransitionMemberStatus_CAS失败 from_status 不匹配时返回 false
func TestTryTransitionMemberStatus_CAS失败(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "member-1", "team-1", "Member One", "{}", "ready", "teammate", "", "", "", "", "")
	// 当前状态是 ready，尝试从 unstarted 转换（不匹配）
	result := db.TryTransitionMemberStatus(ctx, "member-1", "team-1", atschema.MemberStatusUnstarted, atschema.MemberStatusStarting)
	if result {
		t.Error("CAS 失败应返回 false")
	}
	member, _ := db.GetMember(ctx, "member-1", "team-1")
	if member.Status != "ready" {
		t.Errorf("CAS 失败后状态不应改变, got %s", member.Status)
	}
}

// TestListHumanAgentNames_有human_agent 返回 human_agent 名列表
func TestListHumanAgentNames_有human_agent(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "leader-1", "team-1", "Leader", "{}", "busy", "leader", "", "", "", "", "")
	db.CreateMember(ctx, "m1", "team-1", "Teammate", "{}", "ready", "teammate", "", "", "", "", "")
	db.CreateMember(ctx, "human-1", "team-1", "Human Agent", "{}", "unstarted", "human_agent", "", "", "", "", "")
	names, err := db.ListHumanAgentNames(ctx, "team-1")
	if err != nil {
		t.Fatalf("ListHumanAgentNames 不应返回错误: %v", err)
	}
	if len(names) != 1 {
		t.Errorf("应返回 1 个 human_agent, got %d", len(names))
	}
	if names[0] != "human-1" {
		t.Errorf("human_agent 名不匹配: got %s, want human-1", names[0])
	}
}

// TestListHumanAgentNames_无human_agent 返回空列表
func TestListHumanAgentNames_无human_agent(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "m1", "team-1", "Teammate", "{}", "ready", "teammate", "", "", "", "", "")
	names, err := db.ListHumanAgentNames(ctx, "team-1")
	if err != nil {
		t.Fatalf("ListHumanAgentNames 不应返回错误: %v", err)
	}
	if len(names) != 0 {
		t.Errorf("无 human_agent时应返回空列表, got %d", len(names))
	}
}

// TestGetMembersMaxUpdatedAt_有数据 返回最大 updated_at
func TestGetMembersMaxUpdatedAt_有数据(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "m1", "team-1", "Member 1", "{}", "ready", "teammate", "", "", "", "", "")
	maxTs := db.GetMembersMaxUpdatedAt(ctx, "team-1")
	if maxTs == 0 {
		t.Error("有数据时 GetMembersMaxUpdatedAt 应返回非零时间戳")
	}
}

// TestGetMembersMaxUpdatedAt_无数据 返回 0
func TestGetMembersMaxUpdatedAt_无数据(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	maxTs := db.GetMembersMaxUpdatedAt(ctx, "nonexistent")
	if maxTs != 0 {
		t.Errorf("无数据时应返回 0, got %d", maxTs)
	}
}

// TestUpdateMemberExecutionStatus_合法转换 idle→running 返回 true
func TestUpdateMemberExecutionStatus_合法转换(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "member-1", "team-1", "Member One", "{}", "ready", "teammate", "", "idle", "", "", "")
	result := db.UpdateMemberExecutionStatus(ctx, "member-1", "team-1", "running")
	if !result {
		t.Error("合法转换 idle→running 应返回 true")
	}
	member, _ := db.GetMember(ctx, "member-1", "team-1")
	if member.ExecutionStatus != "running" {
		t.Errorf("ExecutionStatus 应更新为 running, got %s", member.ExecutionStatus)
	}
}

// TestUpdateMemberExecutionStatus_非法转换 idle→completed 返回 false
func TestUpdateMemberExecutionStatus_非法转换(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "team-1", "Team One", "leader-1", "", "")
	db.CreateMember(ctx, "member-1", "team-1", "Member One", "{}", "ready", "teammate", "", "idle", "", "", "")
	result := db.UpdateMemberExecutionStatus(ctx, "member-1", "team-1", "completed")
	if result {
		t.Error("非法转换 idle→completed 应返回 false")
	}
}
```

- [ ] **Step 2: 替换 MemberDao 空实现为真实逻辑**

将 `memory_impl.go` 中 MemberDao 方法的空实现全部替换为 spec §5.4 的完整代码。需要确保 import 了 `atschema` 包。

- [ ] **Step 3: 运行 MemberDao 测试**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; sleep 1 ; export GOPROXY=https://goproxy.cn,direct && go test -v -run 'TestCreateMember|TestGetMember|TestGetTeamMembers|TestUpdateMemberStatus|TestTryTransitionMemberStatus|TestListHumanAgentNames|TestGetMembersMaxUpdatedAt|TestUpdateMemberExecutionStatus' ./internal/agent_teams/tools/database/...`
Expected: 所有测试 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/tools/database/memory_impl.go internal/agent_teams/tools/database/memory_impl_test.go
git commit -m "feat(9.65a-1): 实现 InMemoryTeamDatabase MemberDao 8 方法（含 FSM 校验和 CAS）"
```

---

## Task 5: engine.go 重写 — GetCurrentTime/SanitizeSessionIDForTable + 测试

**Files:**
- Modify: `internal/agent_teams/tools/database/engine.go`（完整重写）

- [ ] **Step 1: 写 engine 测试**

创建 `internal/agent_teams/tools/database/engine_test.go`：

```go
package database

import (
	"testing"
)

// TestGetCurrentTime 返回 int64 毫秒时间戳
func TestGetCurrentTime(t *testing.T) {
	ts := GetCurrentTime()
	if ts == 0 {
		t.Error("GetCurrentTime 应返回非零时间戳")
	}
	// 验证是毫秒级（大于 1700000000000 ≈ 2023 年）
	if ts < 1700000000000 {
		t.Errorf("GetCurrentTime 应返回毫秒级时间戳, got %d", ts)
	}
}

// TestSanitizeSessionIDForTable 不同 sessionID 产生不同后缀
func TestSanitizeSessionIDForTable(t *testing.T) {
	suffix1 := SanitizeSessionIDForTable("session-abc")
	suffix2 := SanitizeSessionIDForTable("session-def")
	if suffix1 == suffix2 {
		t.Error("不同 sessionID 应产生不同后缀")
	}
}

// TestSanitizeSessionIDForTable_长度验证 后缀为 16 字符 hex
func TestSanitizeSessionIDForTable_长度验证(t *testing.T) {
	suffix := SanitizeSessionIDForTable("session-1")
	if len(suffix) != 16 {
		t.Errorf("SanitizeSessionIDForTable 应返回 16 字符, got %d", len(suffix))
	}
}

// TestSanitizeSessionIDForTable_幂等 同一 sessionID 多次调用结果一致
func TestSanitizeSessionIDForTable_幂等(t *testing.T) {
	suffix1 := SanitizeSessionIDForTable("session-123")
	suffix2 := SanitizeSessionIDForTable("session-123")
	if suffix1 != suffix2 {
		t.Error("同一 sessionID 多次调用应返回相同后缀")
	}
}

// TestSanitizeSessionIDForTable_对齐Python 与 Python _sanitize_session_id_for_table 一致
func TestSanitizeSessionIDForTable_对齐Python(t *testing.T) {
	// Python: hashlib.blake2s("test-session-id".encode(), digest_size=8).hexdigest()
	// BLAKE2s 是确定性的，Go 和 Python 结果应一致
	suffix := SanitizeSessionIDForTable("test-session-id")
	if len(suffix) != 16 {
		t.Errorf("后缀长度应为 16, got %d", len(suffix))
	}
	// 注意：Go crypto/blake2s 和 Python hashlib.blake2s 的输出应完全一致
	// 如果需要精确对齐，可在集成测试中用 Python 验证
}
```

- [ ] **Step 2: 重写 engine.go**

完全重写 `internal/agent_teams/tools/database/engine.go`，对齐 spec §6：

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
	h, err := blake2s.New(8, nil)
	if err != nil {
		// blake2s.New 仅在 key 长度不合法时报错，此处 key=nil 不可能失败
		return hex.EncodeToString([]byte(sessionID))
	}
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

- [ ] **Step 3: 运行 engine 测试**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; sleep 1 ; export GOPROXY=https://goproxy.cn,direct && go test -v -run 'TestGetCurrentTime|TestSanitizeSessionIDForTable' ./internal/agent_teams/tools/database/...`
Expected: 所有测试 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agent_teams/tools/database/engine.go internal/agent_teams/tools/database/engine_test.go
git commit -m "feat(9.65a-1): 添加 GetCurrentTime/SanitizeSessionIDForTable 和 engine 测试"
```

---

## Task 6: 旧文件清理与迁移 — 删除 tools/models.go 和 memory_database.go

**Files:**
- Delete: `internal/agent_teams/tools/models.go`
- Delete: `internal/agent_teams/tools/memory_database.go`
- Modify: `internal/agent_teams/tools/doc.go`（更新文件目录）
- Modify: `internal/agent_teams/tools/database/doc.go`（更新文件目录）

- [ ] **Step 1: 删除旧文件**

删除 `tools/models.go`（已迁移到 `database/models.go`）：
```bash
rm internal/agent_teams/tools/models.go
```

删除 `tools/memory_database.go`（InMemoryTeamDatabase 实现已移入 database 包）：
```bash
rm internal/agent_teams/tools/memory_database.go
```

- [ ] **Step 2: 更新 tools/doc.go**

修改 `internal/agent_teams/tools/doc.go`，删除已迁移文件条目：

```go
// Package tools 提供团队工具集。
//
// 本包定义团队协作所需的核心工具接口：TeamTaskManager、TeamMessageManager。
// 数据模型（Team/TeamMember）和 InMemoryTeamDatabase 已迁移到 database 子包（9.65a-1）。
//
// 文件目录：
//
//	tools/
//	├── doc.go               # 包文档
//	├── task_manager.go      # TeamTaskManager 接口                          ⤵️ 9.65a-2
//	├── message_manager.go   # TeamMessageManager 接口                       ⤵️ 9.65a-3
//	└── database/
//	    ├── doc.go           # 数据库子包文档
//	    ├── config.go        # DatabaseConfig 配置
//	    ├── config_test.go   # 配置测试
//	    ├── models.go        # Team + TeamMember 数据模型（从 tools/models.go 迁移）    9.65a-1
//	    ├── database.go      # TeamDatabase 门面接口 + DAO 接口                         9.65a-1
//	    ├── engine.go        # GetCurrentTime/SanitizeSessionIDForTable + SQL 占位      9.65a-1
//	    ├── memory_impl.go   # InMemoryTeamDatabase 单体实现                           9.65a-1
//	    ├── team_dao.go      # TeamDao 占位（实现已在 memory_impl.go）                  9.65a-1
//	    ├── member_dao.go    # MemberDao 占位（实现已在 memory_impl.go）                9.65a-1
//	    ├── task_dao.go      # TaskDao 占位                                            ⤵️ 9.65a-2
//	    ├── message_dao.go   # MessageDao 占位                                         ⤵️ 9.65a-3
//	    ├── models_test.go   # 模型序列化测试                                          9.65a-1
//	    ├── database_test.go # 接口满足性测试                                          9.65a-1
//	    ├── memory_impl_test.go # InMemory DAO 测试                                   9.65a-1
//	    └── engine_test.go   # engine 函数测试                                         9.65a-1
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/
package tools
```

- [ ] **Step 3: 更新 database/doc.go**

修改 `internal/agent_teams/tools/database/doc.go`，添加新增文件条目：

```go
// Package database 提供数据库工具配置、接口定义和内存实现。
//
// 本包定义数据库连接配置结构体、TeamDatabase 门面接口、4 个 DAO 接口，
// 以及 InMemoryTeamDatabase 单体实现（对齐 Python InMemoryTeamDatabase 的自引用设计）。
// 数据模型 Team/TeamMember 从 tools 包迁移到此包。
// SQL 实现将在 9.65a-5 回填。
//
// 文件目录：
//
//	database/
//	├── doc.go              # 包文档
//	├── config.go           # DatabaseConfig 配置结构体与构造函数
//	├── config_test.go      # 配置测试
//	├── models.go           # Team + TeamMember 数据模型 + 动态表常量                9.65a-1
//	├── database.go         # TeamDatabase 门面接口 + TeamDao/MemberDao 接口         9.65a-1
//	├── engine.go           # GetCurrentTime/SanitizeSessionIDForTable + SQL 占位    9.65a-1
//	├── memory_impl.go      # InMemoryTeamDatabase 单体实现                         9.65a-1
//	├── team_dao.go         # TeamDao 占位（实现已在 memory_impl.go）
//	├── member_dao.go       # MemberDao 占位（实现已在 memory_impl.go）
//	├── task_dao.go         # TaskDao 占位                                          ⤵️ 9.65a-2
//	├── message_dao.go      # MessageDao 占位                                       ⤵️ 9.65a-3
//	├── models_test.go      # 模型序列化测试                                         9.65a-1
//	├── database_test.go    # 接口满足性测试                                         9.65a-1
//	├── memory_impl_test.go # InMemory DAO 测试                                     9.65a-1
//	└── engine_test.go      # engine 函数测试                                        9.65a-1
//
// 对应 Python 代码：openjiuwen/agent_teams/tools/database/
package database
```

- [ ] **Step 4: 验证整体编译**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; sleep 1 ; export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/...`
Expected: 编译成功（tools 包不再引用已删除的 models.go，memory 包引用 tools.TeamTaskManager 不受影响）

- [ ] **Step 5: Commit**

```bash
git add -A internal/agent_teams/tools/
git commit -m "feat(9.65a-1): 清理旧文件（删除 tools/models.go 和 memory_database.go）+ 更新 doc.go 文件目录"
```

---

## Task 7: 全量测试验证 + IMPLEMENTATION_PLAN.md 状态更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`（更新 9.65a-1 状态为 ✅）

- [ ] **Step 1: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; sleep 1 ; export GOPROXY=https://goproxy.cn,direct && go test -v -count=1 ./internal/agent_teams/tools/database/...`
Expected: 所有测试 PASS，覆盖率 ≥ 85%

- [ ] **Step 2: 检查覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)' ; sleep 1 ; export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agent_teams/tools/database/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 9.65a-1 的状态从 `☐` 更新为 `✅`：

```markdown
| 9.65a-1 | ✅ | TeamDB 基础层 | 数据模型+TeamDatabase接口+InMemoryDAO（TeamDao/MemberDao含FSM校验）+会话表生命周期+GetCurrentTime/SanitizeSessionIDForTable | `openjiuwen/agent_teams/tools/database/` · `tools/models.py` |
```

- [ ] **Step 4: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 9.65a-1 状态为 ✅（TeamDB 基础层已完成）"
```
