# 9.65a-4 TeamBackend 门面 + 回填 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 TeamBackend 门面（30+ 方法）+ 一步到位回填所有 ⤵️ 标记点

**Architecture:** TeamBackend 组合 TeamDatabase + TeamTaskManager + TeamMessageManager + Messager，提供团队级业务门面。Functional Options 构造，独立 RWMutex 仅保护 HITT 缓存，串行文件清理。

**Tech Stack:** Go 1.22+, sync.RWMutex, os.RemoveAll, context.Context

---

## 文件结构

| 操作 | 路径 | 职责 |
|------|------|------|
| 创建 | `internal/agent_teams/tools/team_backend.go` | TeamBackend 结构体 + 30+ 方法 + Functional Options |
| 创建 | `internal/agent_teams/tools/team_backend_test.go` | TeamBackend 单元测试 |
| 修改 | `internal/agent_teams/agent/infra.go` | `TeamBackend any` → `*TeamBackend`，`MessageManager any` → `*TeamMessageManager` |
| 修改 | `internal/agent_teams/agent/agent_configurator.go` | `TeamBackend()/SetTeamBackend` 类型替换 + `SetupTeamBackend` 完整实现 |
| 修改 | `internal/agent_teams/agent/team_agent.go` | `TeamBackend() any` → `*TeamBackend` |
| 修改 | `internal/agent_teams/interaction/human_agent_inbox.go` | `team any` → `*TeamBackend`，`messageManager any` → `*TeamMessageManager`，stub → 真实调用 |
| 修改 | `internal/agent_teams/interaction/user_inbox.go` | `messageManager any` → `*TeamMessageManager`，stub → 真实调用 |
| 修改 | `internal/agent_teams/interaction/router.go` | `DeliverDirect` 的 `messageManager any` → `*TeamMessageManager`，stub → 真实调用 |
| 修改 | `internal/agent_teams/runtime/manager.go` | `RegisterHumanAgentInbound` stub → 真实调用，`resolveRecipients` → 真实调用 |
| 修改 | `internal/agent_teams/runtime/pool.go` | `*TeamAgent` 类型注释更新 |
| 修改 | `internal/agent_teams/tools/doc.go` | 添加 team_backend.go 条目 |
| 修改 | `IMPLEMENTATION_PLAN.md` | 9.65a-4 标记 ✅ |

---

### Task 1: 创建 TeamBackend 结构体 + Functional Options + 构造函数

**Files:**
- Create: `internal/agent_teams/tools/team_backend.go`

- [ ] **Step 1: 创建 team_backend.go 文件，写入结构体定义 + Functional Options + 构造函数**

```go
package tools

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/models"
	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OnInbound 团队→用户通知回调。
// 对齐 Python: OnInbound = Callable[[HumanAgentInboundEvent], Awaitable[None]]
// 复用 interaction 包的 OnInbound 类型签名，但 tools 包不 import interaction（避免循环），
// 因此在此独立定义。
type OnInbound func(ctx context.Context, memberName string, payload any) error

// TeamBackend 团队后端门面，组合 DB + TaskManager + MessageManager + Messager。
// 对齐 Python: TeamBackend (openjiuwen/agent_teams/tools/team.py)
//
// 提供团队级业务方法：成员生命周期、团队生命周期、HITT 名册、
// 文件清理、任务操作、跨域组合操作。
// HITT 缓存由独立 hittMu 保护，DB 操作靠 DAO 层并发安全。
type TeamBackend struct {
	// ── 必填字段 ──
	teamName         string
	memberName       string
	isLeader         bool
	leaderMemberName string
	db               database.TeamDatabase
	messager         messager.Messager
	taskManager      *TeamTaskManager
	messageManager   *TeamMessageManager

	// ── 可选字段（Functional Options 注入）──
	teammateMode         string
	predefinedMembers    []atschema.TeamMemberSpec
	modelConfigAllocator func(modelName string) *models.Allocation
	leaderAllocation     *models.Allocation
	onTeamCleaned        func(ctx context.Context) error
	onTeamBuilt          func(ctx context.Context) error
	planStorageDir       string
	planID               string

	// ── HITT 缓存（hittMu 保护）──
	hittMu               sync.RWMutex
	specEnableHITT       bool
	enableHITT           bool
	hittNames            map[string]struct{}
	hittInboundCallbacks map[string]OnInbound

	// ── 文件系统清理路径 ──
	cleanupPaths map[string]struct{}
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// tbLogComponent TeamBackend 日志组件
	tbLogComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TeamBackendOption TeamBackend 构造可选参数。
type TeamBackendOption func(*TeamBackend)

// WithTeammateMode 设置默认成员模式。
func WithTeammateMode(mode string) TeamBackendOption {
	return func(tb *TeamBackend) { tb.teammateMode = mode }
}

// WithPredefinedMembers 设置预定义成员列表。
func WithPredefinedMembers(members []atschema.TeamMemberSpec) TeamBackendOption {
	return func(tb *TeamBackend) { tb.predefinedMembers = members }
}

// WithModelConfigAllocator 设置模型分配回调。
func WithModelConfigAllocator(fn func(modelName string) *models.Allocation) TeamBackendOption {
	return func(tb *TeamBackend) { tb.modelConfigAllocator = fn }
}

// WithLeaderAllocation 设置 Leader 模型分配。
func WithLeaderAllocation(a *models.Allocation) TeamBackendOption {
	return func(tb *TeamBackend) { tb.leaderAllocation = a }
}

// WithEnableHITT 设置 HITT 能力开关（spec 级天花板）。
func WithEnableHITT(enable bool) TeamBackendOption {
	return func(tb *TeamBackend) { tb.specEnableHITT = enable; tb.enableHITT = enable }
}

// WithOnTeamCleaned 设置团队清理回调。
// 对齐 Python: on_team_cleaned 参数
func WithOnTeamCleaned(fn func(ctx context.Context) error) TeamBackendOption {
	return func(tb *TeamBackend) { tb.onTeamCleaned = fn }
}

// WithOnTeamBuilt 设置团队构建回调。
// 对齐 Python: on_team_built 参数
func WithOnTeamBuilt(fn func(ctx context.Context) error) TeamBackendOption {
	return func(tb *TeamBackend) { tb.onTeamBuilt = fn }
}

// WithPlanStorageDir 设置计划文件存储目录。
func WithPlanStorageDir(dir string) TeamBackendOption {
	return func(tb *TeamBackend) { tb.planStorageDir = dir }
}

// WithPlanID 设置团队级计划标识。
func WithPlanID(id string) TeamBackendOption {
	return func(tb *TeamBackend) { tb.planID = id }
}

// WithLeaderMemberName 设置 Leader 成员名（覆盖默认值）。
func WithLeaderMemberName(name string) TeamBackendOption {
	return func(tb *TeamBackend) { tb.leaderMemberName = name }
}

// NewTeamBackend 创建团队后端门面。
// 对齐 Python: TeamBackend.__init__(team_name, member_name, is_leader, db, messager, ...)
func NewTeamBackend(
	teamName, memberName string, isLeader bool,
	db database.TeamDatabase, msg messager.Messager,
	opts ...TeamBackendOption,
) *TeamBackend {
	tb := &TeamBackend{
		teamName:             teamName,
		memberName:           memberName,
		isLeader:             isLeader,
		leaderMemberName:     memberName, // 默认值，WithLeaderMemberName 可覆盖
		db:                   db,
		messager:             msg,
		teammateMode:         string(atschema.MemberModeBuildMode),
		hittNames:            make(map[string]struct{}),
		hittInboundCallbacks: make(map[string]OnInbound),
		cleanupPaths:         make(map[string]struct{}),
	}
	for _, opt := range opts {
		opt(tb)
	}
	// 内部构造 TaskManager 和 MessageManager
	tb.taskManager = NewTeamTaskManager(
		db, teamName, memberName, msg,
		tb.planStorageDir, tb.planID, tb.leaderMemberName,
	)
	tb.messageManager = NewTeamMessageManager(db, teamName, memberName, msg)

	logger.Info(tbLogComponent).Str("team_name", teamName).Str("member_name", memberName).
		Msg("TeamBackend 初始化完成")

	return tb
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/tools/`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agent_teams/tools/team_backend.go
git commit -m "feat(9.65a-4): add TeamBackend struct + functional options + constructor"
```

---

### Task 2: 实现属性访问 + 查询方法

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go`

- [ ] **Step 1: 在导出函数区块添加属性访问方法 + 6 个查询方法**

在 `NewTeamBackend` 函数之后、非导出函数区块之前添加：

```go
// TeamName 返回团队名。
// 对齐 Python: TeamBackend.team_name
func (tb *TeamBackend) TeamName() string { return tb.teamName }

// MemberName 返回当前成员名。
// 对齐 Python: TeamBackend.member_name
func (tb *TeamBackend) MemberName() string { return tb.memberName }

// IsLeader 返回是否 Leader。
// 对齐 Python: TeamBackend.is_leader
func (tb *TeamBackend) IsLeader() bool { return tb.isLeader }

// LeaderMemberName 返回 Leader 成员名。
// 对齐 Python: TeamBackend.leader_member_name
func (tb *TeamBackend) LeaderMemberName() string { return tb.leaderMemberName }

// DB 返回团队数据库实例。
// 对齐 Python: TeamBackend.db
func (tb *TeamBackend) DB() database.TeamDatabase { return tb.db }

// TaskManager 返回任务管理器。
// 对齐 Python: TeamBackend.task_manager
func (tb *TeamBackend) TaskManager() *TeamTaskManager { return tb.taskManager }

// MessageManager 返回消息管理器。
// 对齐 Python: TeamBackend.message_manager
func (tb *TeamBackend) MessageManager() *TeamMessageManager { return tb.messageManager }

// GetMember 获取成员信息。
// 对齐 Python: TeamBackend.get_member(member_name)
func (tb *TeamBackend) GetMember(ctx context.Context, memberName string) (*database.TeamMember, error) {
	return tb.db.Member().GetMember(ctx, memberName, tb.teamName)
}

// ListMembers 列出团队成员（排除自身）。
// 对齐 Python: TeamBackend.list_members()
func (tb *TeamBackend) ListMembers(ctx context.Context) ([]*database.TeamMember, error) {
	members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, "")
	if err != nil {
		return nil, err
	}
	// 对齐 Python: 排除自身
	filtered := make([]*database.TeamMember, 0, len(members))
	for _, m := range members {
		if m.MemberName != tb.memberName {
			filtered = append(filtered, m)
		}
	}
	return filtered, nil
}

// GetTeamInfo 获取团队信息。
// 对齐 Python: TeamBackend.get_team_info()
func (tb *TeamBackend) GetTeamInfo(ctx context.Context) (*database.Team, error) {
	return tb.db.Team().GetTeam(ctx, tb.teamName)
}

// IsTeamCompleted 判断团队是否完成（所有任务终态 + 所有成员 settled + 无未读消息）。
// 对齐 Python: TeamBackend.is_team_completed()
// 返回 TeamCompletionSnapshot 或 nil（未完成）。
func (tb *TeamBackend) IsTeamCompleted(ctx context.Context) (*atschema.TeamCompletionSnapshot, error) {
	// 步骤 1: 查团队
	team, err := tb.db.Team().GetTeam(ctx, tb.teamName)
	if err != nil || team == nil {
		return nil, nil
	}
	// 步骤 2: 查成员
	members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, "")
	if err != nil {
		return nil, err
	}
	// 步骤 3: 查任务
	tasks, err := tb.db.Task().GetTeamTasks(ctx, tb.teamName, "")
	if err != nil {
		return nil, err
	}
	// 步骤 4: 判定 — 所有任务终态
	for _, t := range tasks {
		if !fsm.IsTaskTerminal(t.Status) {
			return nil, nil
		}
	}
	// 步骤 5: 判定 — 所有成员 settled
	for _, m := range members {
		if !atschema.MemberSettledStatuses[atschema.MemberStatus(m.Status)] {
			return nil, nil
		}
	}
	// 步骤 6: 判定 — 无未读消息
	if tb.messageManager.HasUnreadMessages(ctx, true) {
		return nil, nil
	}
	return &atschema.TeamCompletionSnapshot{
		MemberCount: len(members),
		TaskCount:   len(tasks),
	}, nil
}

// GetTeamUpdatedAt 获取团队 updated_at 时间戳。
// 对齐 Python: TeamBackend.get_team_updated_at()
func (tb *TeamBackend) GetTeamUpdatedAt(ctx context.Context) int64 {
	return tb.db.Team().GetTeamUpdatedAt(ctx, tb.teamName)
}

// GetMembersMaxUpdatedAt 获取成员 MAX(updated_at)。
// 对齐 Python: TeamBackend.get_members_max_updated_at()
func (tb *TeamBackend) GetMembersMaxUpdatedAt(ctx context.Context) int64 {
	return tb.db.Member().GetMembersMaxUpdatedAt(ctx, tb.teamName)
}
```

注意：需要额外 import `"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"`。

- [ ] **Step 2: 在 fsm 包添加 IsTaskTerminal 函数（如果不存在）**

先检查 `internal/agent_teams/fsm/transitions.go` 是否已有 `IsTaskTerminal`，如果没有则添加：

```go
// IsTaskTerminal 判断任务状态是否为终态。
func IsTaskTerminal(status string) bool {
	return status == TaskStatusCompleted || status == TaskStatusCancelled
}
```

- [ ] **Step 3: 确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/tools/`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add internal/agent_teams/tools/team_backend.go internal/agent_teams/fsm/transitions.go
git commit -m "feat(9.65a-4): add TeamBackend property accessors + query methods"
```

---

### Task 3: 实现成员生命周期方法

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go`

- [ ] **Step 1: 在导出函数区块添加 SpawnMember + Startup + StartupMember + ShutdownMember + CancelMember**

```go
// SpawnMember 创建成员记录。
// 对齐 Python: TeamBackend.spawn_member(member_name, display_name, agent_card, role, ...)
//
// Python 步骤：
//  1. 校验 member_name 非空、不含"/"、非保留名
//  2. 查预定义成员 persona
//  3. 模型分配：_allocate_model_config(model_name)
//  4. DB 写入：db.member.create_member(...)
//  5. HITT 缓存写透：若 role == HUMAN_AGENT，hittNames.add
//  6. 日志
//  7. 返回 MemberOpResult
func (tb *TeamBackend) SpawnMember(ctx context.Context, memberName, displayName, agentCard, role, desc, prompt, modelName string) atschema.MemberOpResult {
	// 步骤 1: 校验
	if memberName == "" || strings.Contains(memberName, "/") {
		return atschema.NewMemberOpResultFail("invalid member_name: " + memberName)
	}
	if atschema.ReservedMemberNames[memberName] {
		return atschema.NewMemberOpResultFail("reserved member_name: " + memberName)
	}
	// 步骤 2: 查预定义成员 persona
	persona := desc
	hint := prompt
	for _, pm := range tb.predefinedMembers {
		if pm.MemberName == memberName {
			persona = pm.Persona
			hint = pm.PromptHint
			if pm.DisplayName != "" {
				displayName = pm.DisplayName
			}
			if pm.ModelName != "" {
				modelName = pm.ModelName
			}
			break
		}
	}
	// 步骤 3: 模型分配
	modelRefJSON := ""
	if tb.modelConfigAllocator != nil {
		if alloc := tb.modelConfigAllocator(modelName); alloc != nil {
			refMap := map[string]any{"model_name": alloc.Entry.ModelName, "model_index": alloc.GroupIndex}
			if data, err := json.Marshal(refMap); err == nil {
				modelRefJSON = string(data)
			}
		}
	}
	// 步骤 4: DB 写入
	ok := tb.db.Member().CreateMember(ctx, memberName, tb.teamName, displayName, agentCard,
		string(atschema.MemberStatusUnstarted), role, persona,
		string(atschema.ExecutionStatusIdle), tb.teammateMode, hint, modelRefJSON)
	if !ok {
		logger.Warn(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
			Msg("SpawnMember: 成员已存在")
		return atschema.NewMemberOpResultFail("member already exists: " + memberName)
	}
	// 步骤 5: HITT 缓存写透
	if role == string(atschema.TeamRoleHumanAgent) {
		tb.hittMu.Lock()
		tb.hittNames[memberName] = struct{}{}
		tb.hittMu.Unlock()
	}
	// 步骤 6: 日志
	logger.Info(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Str("role", role).Msg("SpawnMember: 成员已创建")
	return atschema.NewMemberOpResultSuccess()
}

// Startup 启动所有 UNSTARTED 成员。
// 对齐 Python: TeamBackend.startup(on_created=...)
// 返回已启动的成员名列表。
func (tb *TeamBackend) Startup(ctx context.Context) ([]string, error) {
	members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, string(atschema.MemberStatusUnstarted))
	if err != nil {
		return nil, err
	}
	var started []string
	for _, m := range members {
		if m.MemberName == tb.memberName {
			continue // 跳过自身
		}
		ok := tb.db.Member().TryTransitionMemberStatus(ctx, m.MemberName, tb.teamName,
			string(atschema.MemberStatusUnstarted), string(atschema.MemberStatusStarting))
		if ok {
			started = append(started, m.MemberName)
			logger.Info(tbLogComponent).Str("member_name", m.MemberName).Str("team_name", tb.teamName).
				Msg("Startup: 成员已启动")
		}
	}
	return started, nil
}

// StartupMember CAS 启动单个成员（UNSTARTED→STARTING）。
// 对齐 Python: TeamBackend.startup_member(member_name, on_created=...)
func (tb *TeamBackend) StartupMember(ctx context.Context, memberName string) (bool, error) {
	ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
		string(atschema.MemberStatusUnstarted), string(atschema.MemberStatusStarting))
	if ok {
		logger.Info(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
			Msg("StartupMember: 成员已启动")
	}
	return ok, nil
}

// ShutdownMember 关闭成员（FSM + 取消任务 + 事件）。
// 对齐 Python: TeamBackend.shutdown_member(member_name)
//
// Python 步骤：
//  1. 查成员
//  2. 若不存在/已是终态 → fail
//  3. CAS: current → SHUTDOWN_REQUESTED
//  4. 取消该成员的任务（skip self）
//  5. 发布 MemberShutdownEvent
//  6. 返回 MemberOpResult
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string) atschema.MemberOpResult {
	// 步骤 1: 查成员
	member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if err != nil || member == nil {
		return atschema.NewMemberOpResultFail("member not found: " + memberName)
	}
	// 步骤 2: 若已是终态
	if member.Status == string(atschema.MemberStatusShutdown) ||
		member.Status == string(atschema.MemberStatusError) {
		return atschema.NewMemberOpResultFail("member already in terminal state: " + memberName)
	}
	// 步骤 3: CAS 转换
	ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
		member.Status, string(atschema.MemberStatusShutdownRequested))
	if !ok {
		return atschema.NewMemberOpResultFail("CAS transition failed for: " + memberName)
	}
	// 步骤 4: 取消该成员的任务
	tb.taskManager.CancelAllTasks(ctx, tb.teamName, []string{memberName})
	// 步骤 5: 发布事件
	tb.publishEvent(ctx, atschema.MemberShutdownEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
		Force:            false,
	})
	logger.Info(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Msg("ShutdownMember: 成员已请求关闭")
	return atschema.NewMemberOpResultSuccess()
}

// CancelMember 取消成员执行（重置 CLAIMED 任务 + 事件）。
// 对齐 Python: TeamBackend.cancel_member(member_name)
//
// Python 步骤：
//  1. 查成员
//  2. 若不存在/已是终态 → fail
//  3. CAS: current → SHUTDOWN_REQUESTED
//  4. 重置该成员的 CLAIMED 任务
//  5. 发布 MemberCanceledEvent
//  6. 返回 MemberOpResult
func (tb *TeamBackend) CancelMember(ctx context.Context, memberName string) atschema.MemberOpResult {
	// 步骤 1: 查成员
	member, err := tb.db.Member().GetMember(ctx, memberName, tb.teamName)
	if err != nil || member == nil {
		return atschema.NewMemberOpResultFail("member not found: " + memberName)
	}
	// 步骤 2: 若已是终态
	if member.Status == string(atschema.MemberStatusShutdown) ||
		member.Status == string(atschema.MemberStatusError) {
		return atschema.NewMemberOpResultFail("member already in terminal state: " + memberName)
	}
	// 步骤 3: CAS 转换
	ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
		member.Status, string(atschema.MemberStatusShutdownRequested))
	if !ok {
		return atschema.NewMemberOpResultFail("CAS transition failed for: " + memberName)
	}
	// 步骤 4: 重置该成员的 CLAIMED 任务
	tasks, _ := tb.db.Task().GetTasksByAssignee(ctx, tb.teamName, memberName, string(atschema.TaskStatusClaimed))
	for _, t := range tasks {
		tb.db.Task().ResetTask(ctx, t.TaskID)
	}
	// 步骤 5: 发布事件
	tb.publishEvent(ctx, atschema.MemberCanceledEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
	})
	logger.Info(tbLogComponent).Str("member_name", memberName).Str("team_name", tb.teamName).
		Msg("CancelMember: 成员已取消")
	return atschema.NewMemberOpResultSuccess()
}
```

需要额外 import `"encoding/json"`。

- [ ] **Step 2: 确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/tools/`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agent_teams/tools/team_backend.go
git commit -m "feat(9.65a-4): add SpawnMember/Startup/StartupMember/ShutdownMember/CancelMember"
```

---

### Task 4: 实现团队生命周期 + 任务操作 + 文件清理

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go`

- [ ] **Step 1: 在导出函数区块添加 BuildTeam + CleanTeam + ForceCleanTeam + CancelTask + CancelAllTasks + ApprovePlan + ApproveTool + RegisterCleanupPath + RemoveCleanupPaths**

```go
// BuildTeam 创建团队 + 注册 leader + 预定义成员 + HITT。
// 对齐 Python: TeamBackend.build_team(display_name, desc, prompt, enable_hitt)
//
// Python 步骤：
//  1. HITT 能力开关：effective_enable_hitt = spec_enable_hitt and enable_hitt
//  2. DB 初始化 + 创建动态表
//  3. 创建团队行
//  4. 注册 Leader
//  5. 模型分配持久化
//  6. 注册预定义成员
//  7. HITT 名册重建
//  8. 回调触发
//  9. 事件发布
func (tb *TeamBackend) BuildTeam(ctx context.Context, displayName, desc, prompt string, enableHITT bool) error {
	// 步骤 1: HITT 能力开关
	effectiveHITT := tb.specEnableHITT && enableHITT
	tb.hittMu.Lock()
	tb.enableHITT = effectiveHITT
	tb.hittMu.Unlock()

	// 步骤 2: DB 初始化
	if err := tb.db.Initialize(ctx); err != nil {
		return err
	}
	if err := tb.db.CreateCurSessionTables(ctx); err != nil {
		return err
	}
	// 步骤 3: 创建团队行
	tb.db.Team().CreateTeam(ctx, tb.teamName, displayName, tb.leaderMemberName, desc, prompt)
	// 步骤 4: 注册 Leader
	leaderModelRefJSON := ""
	if tb.leaderAllocation != nil {
		refMap := map[string]any{"model_name": tb.leaderAllocation.Entry.ModelName, "model_index": tb.leaderAllocation.GroupIndex}
		if data, err := json.Marshal(refMap); err == nil {
			leaderModelRefJSON = string(data)
		}
	}
	tb.db.Member().CreateMember(ctx, tb.memberName, tb.teamName, tb.memberName, "",
		string(atschema.MemberStatusUnstarted), string(atschema.TeamRoleLeader), "",
		string(atschema.ExecutionStatusIdle), tb.teammateMode, "", leaderModelRefJSON)
	// 步骤 5: 模型分配持久化（已在 Leader 的 model_ref_json 中）
	// 步骤 6: 注册预定义成员
	for _, pm := range tb.predefinedMembers {
		tb.SpawnMember(ctx, pm.MemberName, pm.DisplayName, "", string(pm.RoleType), pm.Persona, pm.PromptHint, pm.ModelName)
	}
	// 步骤 7: HITT 名册重建
	tb.RefreshHumanAgentRoster(ctx)
	// 步骤 8: 回调触发
	if tb.onTeamBuilt != nil {
		if err := tb.onTeamBuilt(ctx); err != nil {
			logger.Warn(tbLogComponent).Err(err).Msg("BuildTeam: onTeamBuilt 回调失败")
		}
	}
	// 步骤 9: 事件发布
	tb.publishEvent(ctx, atschema.TeamCreatedEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName},
		DisplayName:      displayName,
		LeaderMemberName: tb.leaderMemberName,
		Created:          database.GetCurrentTime(),
	})
	logger.Info(tbLogComponent).Str("team_name", tb.teamName).Msg("BuildTeam: 团队已创建")
	return nil
}

// CleanTeam 清理团队（全部 SHUTDOWN → 删 DB → 回调 → 清理路径 → 事件）。
// 对齐 Python: TeamBackend.clean_team()
// 返回 true 表示成功清理，false 表示仍有活跃成员。
func (tb *TeamBackend) CleanTeam(ctx context.Context) (bool, error) {
	// 步骤 1: 查询活跃成员
	members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, "")
	if err != nil {
		return false, err
	}
	for _, m := range members {
		if m.Status != string(atschema.MemberStatusShutdown) &&
			m.Status != string(atschema.MemberStatusError) {
			logger.Warn(tbLogComponent).Str("team_name", tb.teamName).
				Str("active_member", m.MemberName).Str("status", m.Status).
				Msg("CleanTeam: 仍有活跃成员，无法清理")
			return false, nil
		}
	}
	// 步骤 3: 删除团队行
	tb.db.Team().DeleteTeam(ctx, tb.teamName)
	// 步骤 4: 删动态表
	tb.db.DropCurSessionTables(ctx)
	// 步骤 5: 回调触发
	if tb.onTeamCleaned != nil {
		if err := tb.onTeamCleaned(ctx); err != nil {
			logger.Warn(tbLogComponent).Err(err).Msg("CleanTeam: onTeamCleaned 回调失败")
		}
	}
	// 步骤 6: 清理路径
	tb.RemoveCleanupPaths(ctx)
	// 步骤 7: 事件发布
	tb.publishEvent(ctx, atschema.TeamCleanedEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName},
	})
	logger.Info(tbLogComponent).Str("team_name", tb.teamName).Msg("CleanTeam: 团队已清理")
	return true, nil
}

// ForceCleanTeam 强制清理团队（shutdown_all + force_delete + 清理路径）。
// 对齐 Python: TeamBackend.force_clean_team(shutdown_members=force)
func (tb *TeamBackend) ForceCleanTeam(ctx context.Context, shutdownMembers bool) (bool, error) {
	// 步骤 1: 可选关闭所有成员
	if shutdownMembers {
		members, _ := tb.db.Member().GetTeamMembers(ctx, tb.teamName, "")
		for _, m := range members {
			if m.Status != string(atschema.MemberStatusShutdown) {
				tb.db.Member().UpdateMemberStatus(ctx, m.MemberName, tb.teamName, string(atschema.MemberStatusShutdown))
			}
		}
	}
	// 步骤 2: 强制删除
	tb.db.ForceDeleteTeamSession(ctx, tb.teamName)
	// 步骤 3: 回调触发
	if tb.onTeamCleaned != nil {
		if err := tb.onTeamCleaned(ctx); err != nil {
			logger.Warn(tbLogComponent).Err(err).Msg("ForceCleanTeam: onTeamCleaned 回调失败")
		}
	}
	// 步骤 4: 清理路径
	tb.RemoveCleanupPaths(ctx)
	logger.Info(tbLogComponent).Str("team_name", tb.teamName).Msg("ForceCleanTeam: 团队已强制清理")
	return true, nil
}

// CancelTask 取消任务 + 通知 assignee。
// 对齐 Python: TeamBackend.cancel_task(task_id)
func (tb *TeamBackend) CancelTask(ctx context.Context, taskID string) atschema.MemberOpResult {
	unblocked, err := tb.taskManager.Cancel(ctx, taskID)
	if err != nil {
		return atschema.NewMemberOpResultFail("cancel_task failed: " + err.Error())
	}
	// 通知 assignee（如果有）
	task, _ := tb.taskManager.Get(ctx, taskID)
	if task != nil && task.Assignee != "" {
		tb.publishEvent(ctx, atschema.TaskCancelledEvent{
			BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: task.Assignee},
			TaskID:           taskID,
		})
	}
	// 通知 unblocked 任务
	for _, uid := range unblocked {
		tb.publishEvent(ctx, atschema.TaskUnblockedEvent{
			BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName},
			TaskID:           uid,
		})
	}
	logger.Info(tbLogComponent).Str("task_id", taskID).Msg("CancelTask: 任务已取消")
	return atschema.NewMemberOpResultSuccess()
}

// CancelAllTasks 批量取消 + 广播。
// 对齐 Python: TeamBackend.cancel_all_tasks(skip_assignees)
func (tb *TeamBackend) CancelAllTasks(ctx context.Context, skipAssignees []string) atschema.MemberOpResult {
	_, err := tb.taskManager.CancelAllTasks(ctx, tb.teamName, skipAssignees)
	if err != nil {
		return atschema.NewMemberOpResultFail("cancel_all_tasks failed: " + err.Error())
	}
	logger.Info(tbLogComponent).Str("team_name", tb.teamName).Msg("CancelAllTasks: 所有任务已取消")
	return atschema.NewMemberOpResultSuccess()
}

// ApprovePlan 审批计划。
// 对齐 Python: TeamBackend.approve_plan(task_id)
func (tb *TeamBackend) ApprovePlan(ctx context.Context, taskID string) atschema.MemberOpResult {
	ok := tb.taskManager.ApprovePlan(ctx, taskID)
	if !ok {
		return atschema.NewMemberOpResultFail("approve_plan failed: " + taskID)
	}
	task, _ := tb.taskManager.Get(ctx, taskID)
	memberName := ""
	if task != nil {
		memberName = task.Assignee
	}
	tb.publishEvent(ctx, atschema.TaskPlanResponseEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
		TaskID:           taskID,
		Approved:         true,
		Status:           string(atschema.TaskStatusPlanApproved),
	})
	logger.Info(tbLogComponent).Str("task_id", taskID).Msg("ApprovePlan: 计划已审批")
	return atschema.NewMemberOpResultSuccess()
}

// ApproveTool 审批工具调用。
// 对齐 Python: TeamBackend.approve_tool(member_name, tool_call_id, approved, feedback, auto_confirm)
func (tb *TeamBackend) ApproveTool(ctx context.Context, memberName, toolCallID string, approved bool, feedback string, autoConfirm bool) atschema.MemberOpResult {
	tb.publishEvent(ctx, atschema.ToolApprovalResultEvent{
		BaseEventMessage: atschema.BaseEventMessage{TeamName: tb.teamName, MemberName: memberName},
		ToolCallID:       toolCallID,
		Approved:         approved,
		Feedback:         feedback,
		AutoConfirm:      autoConfirm,
	})
	logger.Info(tbLogComponent).Str("member_name", memberName).Str("tool_call_id", toolCallID).
		Bool("approved", approved).Msg("ApproveTool: 工具调用审批结果")
	return atschema.NewMemberOpResultSuccess()
}

// RegisterCleanupPath 注册清理路径（去重）。
// 对齐 Python: TeamBackend.register_cleanup_path(path)
func (tb *TeamBackend) RegisterCleanupPath(path string) {
	if path == "" {
		return
	}
	expanded := filepath.Clean(os.ExpandEnv(path))
	tb.cleanupPaths[expanded] = struct{}{}
}

// RemoveCleanupPaths 串行删除清理路径（按深度排序，失败不中止）。
// 对齐 Python: TeamBackend._remove_cleanup_paths()
func (tb *TeamBackend) RemoveCleanupPaths(ctx context.Context) {
	if len(tb.cleanupPaths) == 0 {
		return
	}
	// 按深度排序（最深先删）
	ordered := make([]string, 0, len(tb.cleanupPaths))
	for p := range tb.cleanupPaths {
		ordered = append(ordered, p)
	}
	sort.Slice(ordered, func(i, j int) bool {
		return len(strings.Split(ordered[i], string(filepath.Separator))) >
			len(strings.Split(ordered[j], string(filepath.Separator)))
	})
	for _, p := range ordered {
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			continue
		}
		if err := os.RemoveAll(p); err != nil {
			logger.Error(tbLogComponent).Str("path", p).Err(err).Msg("RemoveCleanupPaths: 删除失败")
		} else {
			logger.Info(tbLogComponent).Str("path", p).Msg("RemoveCleanupPaths: 已删除")
		}
	}
	tb.cleanupPaths = make(map[string]struct{})
}
```

- [ ] **Step 2: 确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/tools/`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agent_teams/tools/team_backend.go
git commit -m "feat(9.65a-4): add BuildTeam/CleanTeam/ForceCleanTeam/CancelTask/ApprovePlan/ApproveTool/RemoveCleanupPaths"
```

---

### Task 5: 实现 HITT 管理方法 + publishEvent 辅助

**Files:**
- Modify: `internal/agent_teams/tools/team_backend.go`

- [ ] **Step 1: 在导出函数区块添加 HITT 方法，在非导出函数区块添加 publishEvent**

```go
// SpawnHumanAgent 注册 human-agent 成员。
// 对齐 Python: TeamBackend.spawn_human_agent(member_name, display_name, agent_card, prompt)
func (tb *TeamBackend) SpawnHumanAgent(ctx context.Context, memberName, displayName, agentCard, prompt string) atschema.MemberOpResult {
	if !tb.HITTEnabled() {
		return atschema.NewMemberOpResultFail("hitt_not_enabled")
	}
	result := tb.SpawnMember(ctx, memberName, displayName, agentCard, string(atschema.TeamRoleHumanAgent), "", prompt, "")
	if !result.OK {
		return result
	}
	// 注册 inbound 回调占位（由 register_human_agent_inbound 后续设置）
	logger.Info(tbLogComponent).Str("member_name", memberName).Msg("SpawnHumanAgent: human-agent 已创建")
	return atschema.NewMemberOpResultSuccess()
}

// RefreshHumanAgentRoster 从 DB 重建 HITT 名册缓存。
// 对齐 Python: TeamBackend.refresh_human_agent_roster()
func (tb *TeamBackend) RefreshHumanAgentRoster(ctx context.Context) {
	names, err := tb.db.Member().ListHumanAgentNames(ctx, tb.teamName)
	if err != nil {
		logger.Error(tbLogComponent).Err(err).Msg("RefreshHumanAgentRoster: 查询失败")
		return
	}
	tb.hittMu.Lock()
	tb.hittNames = make(map[string]struct{}, len(names))
	for _, n := range names {
		tb.hittNames[n] = struct{}{}
	}
	tb.hittMu.Unlock()
	logger.Info(tbLogComponent).Int("count", len(names)).Msg("RefreshHumanAgentRoster: 名册已重建")
}

// IsHumanAgent 判断是否 human-agent（读缓存）。
// 对齐 Python: TeamBackend.is_human_agent(member_name)
func (tb *TeamBackend) IsHumanAgent(memberName string) bool {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	_, ok := tb.hittNames[memberName]
	return ok
}

// RegisterHumanAgentInbound 注册/清除 inbound 回调。
// 对齐 Python: TeamBackend.register_human_agent_inbound(member_name, callback)
// callback 为 nil 时清除。
func (tb *TeamBackend) RegisterHumanAgentInbound(ctx context.Context, memberName string, callback OnInbound) error {
	tb.hittMu.Lock()
	defer tb.hittMu.Unlock()
	if callback == nil {
		delete(tb.hittInboundCallbacks, memberName)
		return nil
	}
	// 校验：member_name 必须在 hittNames 中
	if _, ok := tb.hittNames[memberName]; !ok {
		return &atschema.UnknownHumanAgentError{Sender: memberName}
	}
	tb.hittInboundCallbacks[memberName] = callback
	return nil
}

// GetHumanAgentInbound 获取 inbound 回调。
// 对齐 Python: TeamBackend.get_human_agent_inbound(member_name)
func (tb *TeamBackend) GetHumanAgentInbound(memberName string) OnInbound {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	return tb.hittInboundCallbacks[memberName]
}

// HumanAgentNames 返回 HITT 名册快照。
// 对齐 Python: TeamBackend.human_agent_names()
func (tb *TeamBackend) HumanAgentNames() []string {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	names := make([]string, 0, len(tb.hittNames))
	for n := range tb.hittNames {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// HITTEnabled 返回 HITT 能力开关。
// 对齐 Python: TeamBackend.hitt_enabled
func (tb *TeamBackend) HITTEnabled() bool {
	tb.hittMu.RLock()
	defer tb.hittMu.RUnlock()
	return tb.enableHITT
}
```

在非导出函数区块添加：

```go
// publishEvent 发布团队事件。
// 对齐 Python: TeamBackend 中通过 messager.publish 调用
func (tb *TeamBackend) publishEvent(ctx context.Context, event atschema.TypedEvent) {
	if tb.messager == nil {
		return
	}
	topicID := atschema.TeamTopicTeam.Build(atschema.GetSessionID(ctx), tb.teamName)
	msg := atschema.EventMessageFromEvent(event)
	if err := tb.messager.Publish(ctx, topicID, msg); err != nil {
		logger.Error(tbLogComponent).Str("event_type", event.EventTypeName()).Err(err).
			Msg("publishEvent: 发布事件失败")
	}
}
```

注意：需要确认 `atschema.GetSessionID` 和 `atschema.UnknownHumanAgentError` 存在。检查 `schema` 包是否有 `GetSessionID` 函数和 `UnknownHumanAgentError` 类型。如果 `UnknownHumanAgentError` 在 `interaction` 包中，需要把它移到 `schema` 包或在此处定义等价类型。

- [ ] **Step 2: 确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/tools/`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agent_teams/tools/team_backend.go
git commit -m "feat(9.65a-4): add HITT methods + publishEvent helper"
```

---

### Task 6: 回填第一类 — 类型替换（any → *TeamBackend / *TeamMessageManager）

**Files:**
- Modify: `internal/agent_teams/agent/infra.go`
- Modify: `internal/agent_teams/agent/agent_configurator.go`
- Modify: `internal/agent_teams/agent/team_agent.go`
- Modify: `internal/agent_teams/interaction/human_agent_inbox.go`
- Modify: `internal/agent_teams/interaction/user_inbox.go`
- Modify: `internal/agent_teams/interaction/router.go`
- Modify: `internal/agent_teams/runtime/pool.go`

- [ ] **Step 1: 修改 infra.go**

将 `TeamBackend any` 改为 `TeamBackend *tools.TeamBackend`，将 `MessageManager any` 改为 `MessageManager *tools.TeamMessageManager`。需要添加 import `"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"`。

- [ ] **Step 2: 修改 agent_configurator.go**

将 `TeamBackend() any` 改为 `TeamBackend() *tools.TeamBackend`，将 `SetTeamBackend(v any)` 改为 `SetTeamBackend(v *tools.TeamBackend)`。需要添加 import。

- [ ] **Step 3: 修改 team_agent.go**

将 `TeamBackend() any` 改为 `TeamBackend() *tools.TeamBackend`。

- [ ] **Step 4: 修改 human_agent_inbox.go**

将 `team any` 改为 `team *tools.TeamBackend`，将 `messageManager any` 改为 `messageManager *tools.TeamMessageManager`。更新 `NewHumanAgentInbox` 签名。需要添加 import `"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"`。

- [ ] **Step 5: 修改 user_inbox.go**

将 `messageManager any` 改为 `messageManager *tools.TeamMessageManager`。更新 `NewUserInbox` 签名。需要添加 import。

- [ ] **Step 6: 修改 router.go**

将 `DeliverDirect` 的 `messageManager any` 参数改为 `messageManager *tools.TeamMessageManager`。需要添加 import。

- [ ] **Step 7: 修改 runtime/pool.go**

更新 `*TeamAgent` 注释，确认类型可用。

- [ ] **Step 8: 确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 9: 提交**

```bash
git add internal/agent_teams/agent/infra.go internal/agent_teams/agent/agent_configurator.go internal/agent_teams/agent/team_agent.go internal/agent_teams/interaction/human_agent_inbox.go internal/agent_teams/interaction/user_inbox.go internal/agent_teams/interaction/router.go internal/agent_teams/runtime/pool.go
git commit -m "feat(9.65a-4): backfill type any → *TeamBackend/*TeamMessageManager"
```

---

### Task 7: 回填第二类 — 方法实现回填（stub → 真实调用）

**Files:**
- Modify: `internal/agent_teams/interaction/human_agent_inbox.go`
- Modify: `internal/agent_teams/interaction/user_inbox.go`
- Modify: `internal/agent_teams/interaction/router.go`
- Modify: `internal/agent_teams/runtime/manager.go`

- [ ] **Step 1: 回填 human_agent_inbox.go**

1. `resolveSender` 中 `names := []string{agentteams.HumanAgentMemberName}` → `names := h.team.HumanAgentNames()`
2. `memberExists` 中 `return true, nil` → `member, _ := h.team.GetMember(ctx, name); return member != nil, nil`（注意需添加 ctx 参数）
3. `Send` 中 broadcast 分支 `msgID := "stub-ha-broadcast-msg-id"` → `msgID, _ := h.messageManager.BroadcastMessage(ctx, body, resolvedSender)`（注意需添加 ctx 参数）

- [ ] **Step 2: 回填 user_inbox.go**

1. `Direct` 中 `msgID := "stub-direct-msg-id"` → `msgID, err := u.messageManager.SendMessage(ctx, body, target, agentteams.UserPseudoMemberName)`，处理 err
2. `Broadcast` 中 `msgID := "stub-broadcast-msg-id"` → `msgID, err := u.messageManager.BroadcastMessage(ctx, body, agentteams.UserPseudoMemberName)`，处理 err

- [ ] **Step 3: 回填 router.go**

1. `DeliverDirect` 中 `msgID := "stub-msg-id"` → `msgID, err := messageManager.SendMessage(ctx, body, target, sender)`，处理 err

- [ ] **Step 4: 回填 runtime/manager.go**

1. `RegisterHumanAgentInbound` stub → 调用 `team.RegisterHumanAgentInbound(ctx, memberName, callback)`
2. `resolveRecipients` 中 `memberExists := func(name string) (bool, error) { return true, nil }` → `memberExists := func(name string) (bool, error) { member, _ := team.GetMember(ctx, name); return member != nil, nil }`

- [ ] **Step 5: 确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 6: 提交**

```bash
git add internal/agent_teams/interaction/human_agent_inbox.go internal/agent_teams/interaction/user_inbox.go internal/agent_teams/interaction/router.go internal/agent_teams/runtime/manager.go
git commit -m "feat(9.65a-4): backfill stubs → real TeamBackend/MessageManager calls"
```

---

### Task 8: 回填第三类（部分）— SetupTeamBackend + spawn_manager + team_agent

**Files:**
- Modify: `internal/agent_teams/agent/agent_configurator.go`
- Modify: `internal/agent_teams/agent/spawn_manager.go`
- Modify: `internal/agent_teams/agent/team_agent.go`

- [ ] **Step 1: 实现完整的 SetupTeamBackend**

替换 `agent_configurator.go` 中的 `SetupTeamBackend` 方法（当前返回 `nil`）：

```go
// SetupTeamBackend 构造 TeamBackend 并注册 cleanup 路径。
// 对齐 Python: AgentConfigurator.setup_team_backend(spec, ctx, messager, ...)
func (c *AgentConfigurator) SetupTeamBackend(spec atschema.TeamAgentSpec, ctx atschema.TeamRuntimeContext, msg messager.Messager, opts ...SetupTeamBackendOption) *tools.TeamBackend {
	cfg := &setupTeamBackendConfig{}
	for _, opt := range opts {
		opt(cfg)
	}
	// 获取 DB
	db := cfg.db
	if db == nil {
		db = database.NewInMemoryTeamDatabase()
	}
	// 确定团队名
	teamName := "default"
	if ctx.TeamSpec != nil && ctx.TeamSpec.TeamName != "" {
		teamName = ctx.TeamSpec.TeamName
	}
	// 构造 TeamBackend
	tbOpts := []tools.TeamBackendOption{
		tools.WithLeaderMemberName(c.leaderMemberName),
	}
	if cfg.modelConfigAllocator != nil {
		tbOpts = append(tbOpts, tools.WithModelConfigAllocator(cfg.modelConfigAllocator))
	}
	if cfg.leaderAllocation != nil {
		tbOpts = append(tbOpts, tools.WithLeaderAllocation(cfg.leaderAllocation))
	}
	if cfg.onTeamCleaned != nil {
		tbOpts = append(tbOpts, tools.WithOnTeamCleaned(cfg.onTeamCleaned))
	}
	if cfg.onTeamBuilt != nil {
		tbOpts = append(tbOpts, tools.WithOnTeamBuilt(cfg.onTeamBuilt))
	}
	tb := tools.NewTeamBackend(teamName, c.memberName, c.isLeader, db, msg, tbOpts...)
	c.SetTeamBackend(tb)
	c.SetTaskManager(tb.TaskManager())
	c.SetMessageManager(tb.MessageManager())
	// 注册 cleanup 路径
	if c.WorkspaceManager() != nil {
		// TODO(#9.66): WorkspaceManager 类型回填后调用 tb.RegisterCleanupPath(...)
	}
	return tb
}
```

同时需要更新 `setupTeamBackendConfig` 和 `SetupTeamBackendOption` 类型。

- [ ] **Step 2: 更新 spawn_manager.go 的 TeamBackend nil check**

将 `spawn_manager.go:200` 附近的 nil check 改为使用 `*tools.TeamBackend` 类型。

- [ ] **Step 3: 更新 team_agent.go:335 的成员状态检查**

将 `team_agent.go:335` 的 `// ⤵️ 待 TeamMember 状态管理回填` 改为使用 `team.GetMember()` 查询。

- [ ] **Step 4: 确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/...`
Expected: 编译成功

- [ ] **Step 5: 提交**

```bash
git add internal/agent_teams/agent/agent_configurator.go internal/agent_teams/agent/spawn_manager.go internal/agent_teams/agent/team_agent.go
git commit -m "feat(9.65a-4): backfill SetupTeamBackend + spawn_manager + team_agent status check"
```

---

### Task 9: 单元测试

**Files:**
- Create: `internal/agent_teams/tools/team_backend_test.go`

- [ ] **Step 1: 创建测试文件，覆盖所有 30+ 方法**

测试策略：
- 使用 `database.NewInMemoryTeamDatabase()` 作为 DB
- 使用 `messager.NewInProcessMessager()` 作为 Messager
- 每个方法至少一个正常路径测试 + 一个异常路径测试
- HITT 缓存并发测试（并发读写 HumanAgentNames）
- 文件清理路径测试（使用 `t.TempDir()`）

关键测试用例：
- `TestNewTeamBackend` — 构造函数 + Functional Options
- `TestSpawnMember` — 正常创建 + 已存在 + 保留名 + HITT 缓存写透
- `TestStartup` — 启动 UNSTARTED 成员
- `TestStartupMember` — CAS 启动
- `TestShutdownMember` — 正常关闭 + 不存在 + 已终态 + CAS 失败
- `TestCancelMember` — 正常取消 + 重置 CLAIMED 任务
- `TestBuildTeam` — 完整构建流程 + HITT 开关
- `TestCleanTeam` — 正常清理 + 有活跃成员
- `TestForceCleanTeam` — 强制清理
- `TestGetMember` / `TestListMembers` — 查询
- `TestIsTeamCompleted` — 完成/未完成
- `TestCancelTask` / `TestCancelAllTasks` — 任务操作
- `TestApprovePlan` / `TestApproveTool` — 审批
- `TestHumanAgentNames` / `TestIsHumanAgent` / `TestHITTEnabled` — HITT 缓存
- `TestRegisterHumanAgentInbound` — 注册/清除 + 未知成员
- `TestRefreshHumanAgentRoster` — DB 重建
- `TestRegisterCleanupPath` / `TestRemoveCleanupPaths` — 文件清理
- `TestHumanAgentNames_Concurrent` — 并发读写 HITT 缓存

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agent_teams/tools/ -run TestTeamBackend -v`
Expected: 全部通过，覆盖率 ≥ 85%

- [ ] **Step 3: 提交**

```bash
git add internal/agent_teams/tools/team_backend_test.go
git commit -m "test(9.65a-4): add TeamBackend unit tests (30+ methods)"
```

---

### Task 10: 更新 doc.go + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agent_teams/tools/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 tools/doc.go**

在文件目录中添加 `team_backend.go` 条目：

```
//	├── team_backend.go      # TeamBackend 门面（30+ 方法 + Functional Options） ✅ 9.65a-4
```

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

将 9.65a-4 行的 `☐` 改为 `✅`。

- [ ] **Step 3: 提交**

```bash
git add internal/agent_teams/tools/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs(9.65a-4): update doc.go + IMPLEMENTATION_PLAN.md"
```

---

### Task 11: 全量编译 + 测试验证

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 运行 agent_teams 包测试**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agent_teams/...`
Expected: 全部通过

- [ ] **Step 3: 确认覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test -coverprofile=coverage.out ./internal/agent_teams/tools/ && go tool cover -func=coverage.out | grep team_backend`
Expected: team_backend.go 覆盖率 ≥ 85%
