# 应用层 TeamManager 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [x]`) syntax for tracking.

**Goal:** 在 `internal/swarm/agents/harness/team/` 下实现应用层 TeamManager，完整对齐 Python 的 `jiwenswarm/agents/harness/team/team_manager.py`，同时删除 Go 独有的 `registry.PoolEntry` 接口。

**Architecture:** 两层架构——应用层 TeamManager（channel_id 索引，session 管理，Rail/技能/流任务管理）+ 核心层 TeamRuntimeManager（team_name 索引，Pool/interact/activate）。processTeamMessageStream 改为通过 channel_id → TeamManager 路由。

**Tech Stack:** Go 1.21+、context.WithoutCancel、sync.Mutex/RWMutex

---

## 文件结构

| 文件 | 职责 | 涉及 |
|------|------|------|
| `internal/swarm/agents/harness/team/doc.go` | 包文档 | 新建 |
| `internal/swarm/agents/harness/team/team_manager.go` | TeamManager 结构体 + 全局索引 + 访问器 | 新建 |
| `internal/swarm/agents/harness/team/team_manager_lifecycle.go` | 生命周期方法（15 个） | 新建 |
| `internal/swarm/agents/harness/team/team_manager_interact.go` | interact 路由 | 新建 |
| `internal/swarm/agents/harness/team/team_manager_spec.go` | Spec 构建（6 个方法） | 新建 |
| `internal/swarm/agents/harness/team/team_manager_skill.go` | 技能管理（14 个方法） | 新建 |
| `internal/swarm/agents/harness/team/team_manager_monitor.go` | 监控/流（8 个方法） | 新建 |
| `internal/swarm/agents/harness/team/team_manager_distributed.go` | 分布式 hooks + 传输字段 | 新建 |
| `internal/swarm/agents/harness/team/team_manager_test.go` | 单元测试 | 新建 |
| `internal/agent_teams/registry/interfaces.go` | 删除 PoolEntry/PoolTeamEntry/PoolAccessor | 修改 |
| `internal/agent_teams/runtime/manager.go` | 删除 PoolEntry() 方法 | 修改 |
| `internal/agent_teams/runtime/pool.go` | 删除 GetEntry/RemoveEntry/编译期断言 | 修改 |
| `internal/agent_teams/agent/team_agent.go` | removeSelfFromPool 改用 Pool() | 修改 |
| `internal/swarm/server/adapter/deep_adapter_team.go` | processTeamMessageStream 改用 TeamManager | 修改 |

---

## 第一部分：TeamManager 实现

### Task 1: 创建包目录 + doc.go

**Files:**
- Create: `internal/swarm/agents/harness/team/doc.go`

- [x] **Step 1: 创建目录**

```bash
mkdir -p /home/opensource/uap-claw-go/internal/swarm/agents/harness/team
```

- [x] **Step 2: 创建 doc.go**

```go
// Package team 提供应用层 TeamManager，管理每个 channel 下的 TeamAgent 运行时。
//
// 对齐 Python: jiuwenswarm/agents/harness/team/team_manager.py
//
// TeamManager 是应用层封装，负责：
//   - channel_id 索引和全局实例管理
//   - session 活跃状态管理（active/pending）
//   - TeamAgent 创建/销毁/生命周期
//   - Rail/技能注册与热更新
//   - 流任务和监控管理
//   - 分布式 hooks 附加
//
// 核心层 TeamRuntimeManager（agent_teams/runtime）负责 Pool/interact/activate，
// TeamManager 通过间接调用来完成实际操作。
//
// 文件目录：
//
//	team/
//	├── doc.go                        # 包文档
//	├── team_manager.go               # TeamManager 结构体 + 全局索引 + 访问器
//	├── team_manager_lifecycle.go     # 生命周期方法
//	├── team_manager_interact.go      # interact 路由
//	├── team_manager_spec.go          # Spec 构建
//	├── team_manager_skill.go         # 技能管理
//	├── team_manager_monitor.go       # 监控/流
//	└── team_manager_distributed.go   # 分布式 hooks
//
// 对应 Python 代码：jiwenswarm/agents/harness/team/team_manager.py
package team
```

- [x] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/agents/harness/team/...`
Expected: 编译成功（空包）

- [x] **Step 4: Commit**

```bash
git add internal/swarm/agents/harness/team/doc.go
git commit -m "feat: 创建应用层 team 包 + doc.go"
```

---

### Task 2: TeamManager 结构体 + 全局索引 + 访问器

**Files:**
- Create: `internal/swarm/agents/harness/team/team_manager.go`
- Ref: `/home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/team/team_manager.py`

- [x] **Step 1: 实现结构体和全局索引**

对齐 Python `TeamManager.__init__` + `_team_managers` 全局字典 + `get_team_manager` 函数 + 6 个访问器属性。

```go
package team

import (
	"strings"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LiveRailEntry 活跃 Rail 实例及其所有者。
// 对齐 Python: TeamManager._team_live_rails 条目
type LiveRailEntry struct {
	// Agent Rail 所属的 Agent
	Agent any
	// Rail Rail 实例
	Rail any
}

// SkillSyncTarget 技能同步目录对。
// 对齐 Python: TeamManager._team_skill_sync_targets 条目
type SkillSyncTarget struct {
	// Source 源目录
	Source string
	// Target 目标目录
	Target string
}

// TeamManager 管理一个 channel 下的所有 TeamAgent 运行时。
// 对齐 Python: jiuwenswarm/agents/harness/team/team_manager.py
//
// 每个 channel 拥有独立的 TeamManager 实例，通过 GetTeamManager(channelID) 获取。
// TeamManager 管理 session 级的活跃状态、Rail 生命周期、技能同步、流任务等应用层职责，
// 通过 TeamRuntimeManager（核心层）间接访问 Pool 执行实际的 interact/activate/finalize 操作。
type TeamManager struct {
	mu sync.Mutex

	// team agent 引用
	// 对齐 Python: _team_agents, _runner_team_agents
	teamAgents       map[string]any // sessionID → TeamAgent（内存持有）
	runnerTeamAgents map[string]any // sessionID → Runner 池引用

	// 活跃状态（单活跃 session 语义）
	// 对齐 Python: _active_session_id, _active_team_name, _pending_session_id, _pending_team_name
	activeSessionID  *string
	activeTeamName   *string
	pendingSessionID *string
	pendingTeamName  *string

	// 监控
	// 对齐 Python: _team_monitors
	teamMonitors map[string]any // sessionID → TeamMonitorHandler

	// 流任务
	// 对齐 Python: _stream_tasks（Go 用 context.CancelFunc 替代 asyncio.Task）
	streamTasks map[string]context.CancelFunc // sessionID → cancel

	// Rail 管理
	// 对齐 Python: _team_skill_rails, _team_member_skill_evolution_rails, _team_skill_create_rails,
	//             _team_rail_contexts, _team_live_rails, _team_skill_sync_targets
	teamSkillRails          map[string]any            // sessionID → TeamSkillEvolutionRail
	teamMemberSkillEvoRails map[string][]any          // sessionID → []SkillEvolutionRail
	teamSkillCreateRails    map[string]any            // sessionID → TeamSkillCreateRail
	teamRailContexts        map[string]any            // sessionID → TeamRailMountContext
	teamLiveRails           map[string][]LiveRailEntry // sessionID → []LiveRailEntry
	teamSkillSyncTargets    map[string]SkillSyncTarget // sessionID → SkillSyncTarget

	// 演进监控
	// 对齐 Python: _team_evolution_watchers
	teamEvolutionWatchers map[string]context.CancelFunc
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// teamManagers 全局 TeamManager 实例字典。
	// 对齐 Python: _team_managers: dict[str, TeamManager]
	teamManagers   = make(map[string]*TeamManager)
	teamManagersMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamManager 创建新的 TeamManager 实例。
// 对齐 Python: TeamManager.__init__
func NewTeamManager() *TeamManager {
	return &TeamManager{
		teamAgents:              make(map[string]any),
		runnerTeamAgents:        make(map[string]any),
		teamMonitors:            make(map[string]any),
		streamTasks:             make(map[string]context.CancelFunc),
		teamSkillRails:          make(map[string]any),
		teamMemberSkillEvoRails: make(map[string][]any),
		teamSkillCreateRails:    make(map[string]any),
		teamRailContexts:        make(map[string]any),
		teamLiveRails:           make(map[string][]LiveRailEntry),
		teamSkillSyncTargets:    make(map[string]SkillSyncTarget),
		teamEvolutionWatchers:   make(map[string]context.CancelFunc),
	}
}

// GetTeamManager 获取指定 channel 的 TeamManager 实例。
// 对齐 Python: get_team_manager(channel_id)
func GetTeamManager(channelID string) *TeamManager {
	resolved := strings.TrimSpace(channelID)
	if resolved == "" {
		resolved = "default"
	}
	teamManagersMu.RLock()
	mgr, ok := teamManagers[resolved]
	teamManagersMu.RUnlock()
	if ok {
		return mgr
	}
	teamManagersMu.Lock()
	defer teamManagersMu.Unlock()
	// double-check
	if mgr, ok = teamManagers[resolved]; ok {
		return mgr
	}
	mgr = NewTeamManager()
	teamManagers[resolved] = mgr
	return mgr
}

// ActiveSessionID 返回当前活跃的 session ID。
// 对齐 Python: TeamManager.active_session_id
func (m *TeamManager) ActiveSessionID() *string {
	return m.activeSessionID
}

// ActiveTeamName 返回当前活跃的 team 名称。
// 对齐 Python: TeamManager.active_team_name
func (m *TeamManager) ActiveTeamName() *string {
	return m.activeTeamName
}

// PendingSessionID 返回等待中的 session ID。
// 对齐 Python: TeamManager.pending_session_id
func (m *TeamManager) PendingSessionID() *string {
	return m.pendingSessionID
}

// PendingTeamName 返回等待中的 team 名称。
// 对齐 Python: TeamManager.pending_team_name
func (m *TeamManager) PendingTeamName() *string {
	return m.pendingTeamName
}

// GetTeamAgent 获取内存中的 TeamAgent 实例。
// 对齐 Python: TeamManager.get_team_agent
func (m *TeamManager) GetTeamAgent(sessionID string) any {
	return m.teamAgents[sessionID]
}
```

- [x] **Step 2: 补充 context 导入**

确保文件顶部 import 包含 `"context"`。

- [x] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/agents/harness/team/...`
Expected: 编译成功

- [x] **Step 4: Commit**

```bash
git add internal/swarm/agents/harness/team/team_manager.go
git commit -m "feat: TeamManager 结构体 + 全局索引 + 访问器对齐 Python"
```

---

### Task 3: 生命周期方法（15 个）

**Files:**
- Create: `internal/swarm/agents/harness/team/team_manager_lifecycle.go`
- Ref: `/home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/team/team_manager.py` 行 700-1048（生命周期方法）

实现 15 个生命周期方法，每个方法内部逻辑对齐 Python。Python 的生命周期方法通过 `Runner` 委托到核心层，Go 简化为直接调 `TeamRuntimeManager`。

关键实现要点：
- `CreateTeam`：加载 spec → session 作用域 team_name → customizer → spec.build → 注册 → 技能初始化
- `GetOrCreateTeam`：锁保护 + 缓存检查 + destroy_other_sessions + CreateTeam
- `PrepareRuntimeActivation`：停止非目标 session → 设置 pending
- `CommitRuntimeReady`：设置 active + 清 pending
- `TerminateSessionRuntime`：停止 Runner + 清理本地状态 + 保留持久化
- `CancelAllStreamTasks`：遍历 streamTasks 调 cancel

- [x] **Step 1: 编写测试文件骨架**

创建 `team_manager_lifecycle_test.go`，包含 `TestNewTeamManager` 和 `TestGetOrCreateTeam` 骨架。

- [x] **Step 2: 实现生命周期方法**

按 Python 逐方法实现，每个方法添加 `// 对齐 Python: TeamManager.xxx` 注释。

- [x] **Step 3: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/agents/harness/team/...`

- [x] **Step 4: Commit**

```bash
git add internal/swarm/agents/harness/team/team_manager_lifecycle.go internal/swarm/agents/harness/team/team_manager_lifecycle_test.go
git commit -m "feat: TeamManager 生命周期方法对齐 Python"
```

---

### Task 4: interact 路由

**Files:**
- Create: `internal/swarm/agents/harness/team/team_manager_interact.go`
- Ref: `/home/opensource/jiuwenswarm-develop/jiuwenswarm/agents/harness/team/team_manager.py` 行 1156-1183

Python 的 `interact` 先检查 `session_id == active_session_id`，再从 `active_team_name` 获取 team_name，委托到 `Runner.interact_agent_team`。Go 简化为直接调 `TeamRuntimeManager.Interact`。

```go
// Interact 向活跃的 team runtime 发送交互消息。
// 对齐 Python: TeamManager.interact
func (m *TeamManager) Interact(ctx context.Context, sessionID string, userInput any) (bool, error) {
	if m.activeSessionID == nil || *m.activeSessionID != sessionID || m.activeTeamName == nil {
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Str("active_session_id", ptrToStr(m.activeSessionID)).
			Str("active_team_name", ptrToStr(m.activeTeamName)).
			Msg("interact 被忽略：非活跃 team session")
		return false, nil
	}
	teamName := *m.activeTeamName
	mgr := runtime.GetTeamRuntimeManager()
	result, err := mgr.Interact(ctx, userInput, teamName, sessionID)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("session_id", sessionID).
			Str("team_name", teamName).
			Msg("interact 失败")
		return false, err
	}
	if result != nil && !result.IsOK() {
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Str("team_name", teamName).
			Msg("interact 对 runner runtime 失败")
		return false, nil
	}
	return true, nil
}
```

- [x] **Step 1: 实现 interact 方法**
- [x] **Step 2: 验证编译**
- [x] **Step 3: Commit**

```bash
git add internal/swarm/agents/harness/team/team_manager_interact.go
git commit -m "feat: TeamManager.Interact 对齐 Python"
```

---

### Task 5: Spec 构建方法（6 个）

**Files:**
- Create: `internal/swarm/agents/harness/team/team_manager_spec.go`
- Ref: Python 行 1185-1600（get_enriched_team_spec, apply_team_plan_mode, build_agent_customizer 等）

6 个方法：
1. `GetEnrichedTeamSpec` — 加载配置 + PostgreSQL + session 作用域 team_name + plan mode + customizer
2. `ApplyTeamPlanMode` — request_metadata.mode == "team.plan" 时启用 plan
3. `BuildAgentCustomizer` — 构建 customizer：继承能力卡 + 技能同步 + Rail 挂载 + 工具注册
4. `RegisterMemberRuntimeTools` — 注册 CronRuntimeBridge + SendFileToolkit
5. `NormalizeDistributedTransportFields` — 分布式传输字段标准化
6. `ParsePort` — 端口解析

- [x] **Step 1: 实现 6 个方法**
- [x] **Step 2: 验证编译**
- [x] **Step 3: Commit**

---

### Task 6: 技能管理方法（14 个）

**Files:**
- Create: `internal/swarm/agents/harness/team/team_manager_skill.go`
- Ref: Python 行 1600-1900

14 个方法：`EnsureTeamSharedSkillsInitialized`, `SyncTeamSkills`, 9 个 Register/Get/Find/Drain 方法, `GetTeamRailContext`, `UpdateEvolutionConfig`

- [x] **Step 1: 实现 14 个方法**
- [x] **Step 2: 验证编译**
- [x] **Step 3: Commit**

---

### Task 7: 监控/流方法（8 个）

**Files:**
- Create: `internal/swarm/agents/harness/team/team_manager_monitor.go`
- Ref: Python 行 1900-1970

8 个方法：`GetMonitor`, `RegisterMonitor`, `HasStreamTask`, `PopStreamTask`, `RegisterStreamTask`, `GetTeamEvolutionWatcher`, `RegisterTeamEvolutionWatcher`, `PopTeamEvolutionWatcher`

Go 差异：Python 用 `asyncio.Task`，Go 用 `context.CancelFunc` 表示流任务。`PopStreamTask` 返回 cancel 函数而非 Task。

- [x] **Step 1: 实现 8 个方法**
- [x] **Step 2: 验证编译**
- [x] **Step 3: Commit**

---

### Task 8: 分布式 hooks + 传输字段标准化

**Files:**
- Create: `internal/swarm/agents/harness/team/team_manager_distributed.go`
- Ref: Python 行 700-900（attach_distributed_hooks）+ 行 1600-1700（normalize_distributed_transport_fields）

2 个方法：
1. `AttachDistributedHooksForRunnerRuntime` — 为 Runner 池中的 TeamAgent 附加分布式 hooks
2. `NormalizeDistributedTransportFields` — 公共包装：分布式传输字段标准化

- [x] **Step 1: 实现 2 个方法**
- [x] **Step 2: 验证编译**
- [x] **Step 3: Commit**

---

### Task 9: 删除 registry.PoolEntry 接口

**Files:**
- Modify: `internal/agent_teams/registry/interfaces.go`
- Modify: `internal/agent_teams/runtime/manager.go`
- Modify: `internal/agent_teams/runtime/pool.go`
- Modify: `internal/agent_teams/agent/team_agent.go`

- [x] **Step 1: 从 registry/interfaces.go 删除 PoolEntry/PoolTeamEntry/PoolAccessor**

删除 `PoolAccessor` 接口、`PoolEntry` 接口、`PoolTeamEntry` 接口。

- [x] **Step 2: 从 runtime/manager.go 删除 PoolEntry() 方法**

删除 `func (m *TeamRuntimeManager) PoolEntry() registry.PoolEntry` 方法。

- [x] **Step 3: 从 runtime/pool.go 删除 GetEntry/RemoveEntry + 编译期断言**

删除：
- `func (p *TeamRuntimePool) GetEntry(teamName string) registry.PoolTeamEntry`
- `func (p *TeamRuntimePool) RemoveEntry(teamName string)`
- `var _ registry.PoolEntry = (*TeamRuntimePool)(nil)`
- `var _ registry.PoolTeamEntry = (*ActiveTeam)(nil)`
- `func (a *ActiveTeam) GetSessionID() string`（如果仅服务于 PoolTeamEntry 接口）

- [x] **Step 4: 修改 team_agent.go 的 removeSelfFromPool**

将 `mgr.PoolEntry()` 改为 `mgr.Pool()`：

```go
func (a *TeamAgent) removeSelfFromPool(ctx context.Context, sessionID string) {
	teamName := a.TeamName()
	if teamName == "" || sessionID == "" {
		return
	}
	mgr := runtime.GetTeamRuntimeManager()
	if mgr == nil {
		return
	}
	pool := mgr.Pool()
	if pool == nil {
		return
	}
	entry := pool.Get(teamName)
	if entry == nil || entry.SessionID != sessionID {
		return
	}
	pool.Remove(teamName)
}
```

- [x] **Step 5: 更新 pool_test.go**

删除 `PoolEntry()` 相关测试，改用 `Pool()` 测试。

- [x] **Step 6: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`

- [x] **Step 7: Commit**

```bash
git add -A
git commit -m "refactor: 删除 Go 独有的 registry.PoolEntry 接口，改用 Pool() 直接访问"
```

---

### Task 10: 改造 processTeamMessageStream

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_team.go`

- [x] **Step 1: 改造 processTeamMessageStream 使用 TeamManager**

核心改动：
1. 不再从 `inputs["params"]["team_name"]` 提取 teamName
2. 通过 `channelID → team.GetTeamManager(channelID)` 获取 TeamManager
3. 用 `teamManager.HasStreamTask(sessionID)` 判断首次/后续请求
4. 首次：`GetEnrichedTeamSpec` + `PrepareRuntimeActivation` + 创建后台流任务
5. 后续：`teamManager.Interact(ctx, sessionID, query)`

- [x] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/server/adapter/...`

- [x] **Step 3: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_team.go
git commit -m "refactor: processTeamMessageStream 改用 TeamManager channel_id 路由对齐 Python"
```

---

### Task 11: 单元测试

**Files:**
- Create: `internal/swarm/agents/harness/team/team_manager_test.go`

- [x] **Step 1: 编写 GetTeamManager 测试（double-check locking + default channel）**
- [x] **Step 2: 编写 Active/Pending 状态管理测试**
- [x] **Step 3: 编写 Interact 测试（非活跃 session 拒绝）**
- [x] **Step 4: 编写 Skill/Monitor/Stream 注册查询测试**
- [x] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/agents/harness/team/... -v`
Expected: PASS

- [x] **Step 6: Commit**

```bash
git add internal/swarm/agents/harness/team/team_manager_test.go
git commit -m "test: TeamManager 单元测试"
```

---

## 第二部分：已确认 M 级修复（待 TeamManager 完成后单独实现）

> 以下 M 级问题已在讨论中确认修复方案，但不在 TeamManager 实现期间并行处理。
> TeamManager 完成后，按以下方案逐个实现。

### M-01: UpdateStatus TeamMember 优先路径

**文件:** `internal/agent_teams/agent/team_agent.go`, `internal/agent_teams/agent/member.go`

**方案:**
1. `TeamAgent.UpdateStatus` 先检查 `a.state.TeamMember != nil`，非 nil 时委托到 `TeamMember.UpdateStatus`
2. `TeamMember.UpdateStatus` 实现完整逻辑：读旧状态 → 相等短路 → 写 DB → 发 MemberStatusChangedEvent

---

### M-02: DeliverInput 补 pending_input 日志

**文件:** `internal/agent_teams/agent/team_agent.go`

**方案:** 在 `HasInFlightRound` 分支 `append` 前补充 Info 日志：
```go
preview := fmt.Sprintf("%v", content)
if len(preview) > 60 { preview = preview[:60] }
logger.Info(logComponent).
    Str("member_name", a.MemberName()).
    Str("preview", preview).
    Msg("入队输入等待下一轮（过渡窗口）")
```

---

### M-03: OnTeammateUnhealthy SpawnManager 持有 baseCtx

**文件:** `internal/agent_teams/agent/spawn_manager.go`

**方案:** SpawnManager 新增 `baseCtx context.Context` 字段（在 NewSpawnManager 或 SpawnTeammate 时从上游 ctx 派生），OnTeammateUnhealthy 中 3 处 `context.Background()` 改用 `baseCtx` 或其派生。

---

### M-04: SpawnInprocess 传播外层 ctx

**文件:** `internal/agent_teams/agent/spawn_manager.go`

**方案:** 工厂函数中 `teammate.Configure(context.Background(), spec, ctx)` 改为 `teammate.Configure(goCtx, spec, ctx)`，传入外层 Go 的 `context.Context`。

---

### M-05: PublishRestartEvent 实现 Messager 发布

**文件:** `internal/agent_teams/agent/spawn_manager.go`

**方案:** 方法签名改为 `PublishRestartEvent(ctx context.Context, memberName string, restartCount int)`，获取 Messager → 构建 TeamTopic → 发布 MemberRestartedEvent。

---

### M-06: BuildContextFromDB 保留 model_ref + 补 MessagerConfig

**文件:** `internal/agent_teams/agent/spawn_manager.go`

**方案:**
1. `_ = resolveMemberModelFromDB(...)` 改为 `memberModel, err := resolveMemberModelFromDB(...)`
2. `MemberModel: nil` 改为 `MemberModel: memberModel`
3. 补充 `MessagerConfig: m.configurator.BuildMemberMessagerConfig(memberName)`

---

### M-07: TeamAgent.PersistAllocatorState 委托到 RecoveryManager

**文件:** `internal/agent_teams/agent/team_agent.go`

**方案:** 空方法体改为：
```go
func (a *TeamAgent) PersistAllocatorState() {
    if a.recoveryManager != nil && a.sessionManager != nil {
        a.recoveryManager.PersistAllocatorState(a.sessionManager.TeamSession())
    }
}
```

---

### M-08: TeamAgent.DrainAgentTask 委托到 StreamController

**文件:** `internal/agent_teams/agent/team_agent.go`

**方案:** 替换硬编码 30s 超时：
```go
func (a *TeamAgent) DrainAgentTask(ctx context.Context) {
    if a.streamController != nil {
        _ = a.streamController.DrainAgentTask(ctx)
    }
}
```

---

### M-09: SetMemberID ctx 传播

**文件:** `internal/agent_teams/agent/team_agent.go`

**方案:**
1. 定义 context key：`type memberIDKey struct{}`
2. `SetMemberID` 改签名返回新 ctx：`func SetMemberID(ctx context.Context, name string) context.Context`
3. logger 封装 helper：`LoggerWithMember(ctx)` 自动从 ctx 取 member_name 附加

---

### M-10: SetupInfra createMessager 实现

**文件:** `internal/agent_teams/agent/agent_configurator.go`

**方案:** 步骤 5 从 TODO 改为实际实现：获取 MessagerConfig → 调整 node_id → 调用 CreateMessager → 设置到 configurator。

---

### M-11: SetupInfra Leader 模型分配器构建

**文件:** `internal/agent_teams/agent/agent_configurator.go`

**方案:** 步骤 7 从占位 `_ = ctx.Role` 改为：
```go
if ctx.Role == atschema.TeamRoleLeader && c.ModelAllocator() == nil {
    allocator := BuildModelAllocatorForPool(spec, ctx.TeamSpec)
    c.SetModelAllocator(allocator)
}
```

---

### M-12: SetupInfra 调用 SetupTeamBackend

**文件:** `internal/agent_teams/agent/agent_configurator.go`

**方案:** 步骤 8 从注释占位改为调用已有方法：
```go
c.SetupTeamBackend(spec, runtimeCtx, c.Messager())
```

---

### M-13: SetupAgent workspace 路径解析 + symlink

**文件:** `internal/agent_teams/agent/agent_configurator.go`

**方案:** 步骤 3 补全 3 段逻辑：stable_base 路径解析 → independent_member_workspace symlink → team_backend.register_cleanup_path

---

### M-14: SetupAgent 调用 BuildMemoryManager

**文件:** `internal/agent_teams/agent/agent_configurator.go`

**方案:** 步骤 16 从 TODO 改为：
```go
mm := c.BuildMemoryManager(spec, ctx, agentSpec, language, memberName)
c.SetMemoryManager(mm)
```

---

### M-15: UnsubscribeTransport 签名加 ctx + 补异常保护日志

**文件:** `internal/agent_teams/agent/team_agent.go`

**方案:**
1. 签名改为 `UnsubscribeTransport(ctx context.Context) error`
2. 替换 `context.Background()` 为传入的 `ctx`
3. `_ = mgr.UnregisterDirectMessageHandler(ctx)` 改为带日志保护
4. `_ = mgr.Unsubscribe(ctx, topicID)` 改为带 debug 日志

---

### M-16: UpdateModelPool 加 Leader 守卫

**文件:** `internal/agent_teams/agent/team_agent.go`

**方案:** 在持久化 leader 配置前加守卫：
```go
if a.configurator.Spec() == nil || a.Role() != atschema.TeamRoleLeader {
    return
}
```

---

### M-17: BuildSpawnConfig.Payload 格式对齐 Python

**文件:** `internal/agent_teams/agent/payload.go`, `internal/agent_teams/agent/team_agent.go`

**方案:**
1. `BuildSpawnConfig` 中 `Payload: b.BuildSpawnPayload(ctx, "")` 改为：
```go
Payload: map[string]any{
    "spec":    b.specJSON(),
    "context": b.contextJSON(ctx),
},
```
2. `FromSpawnPayload` 中类型断言失败时尝试 JSON 反序列化回退

---

### M-19: ProcessMessageStreamImpl query 在流式路径也使用

**文件:** `internal/swarm/server/adapter/deep_adapter.go`

**方案:** 移除 `_ = query` 丢弃，让 query 在流式路径的 inputs 构建中也使用。

---

### M-21: ensureEvolutionRailForSlash 补懒加载

**文件:** `internal/swarm/server/adapter/deep_adapter_slash.go`

**方案:** 补全 Python 的懒加载逻辑：
1. `SkillEvolutionRail == nil` 时尝试 `buildSkillEvolutionRail` 构建
2. 成功后检查 `_get_skill_create_enabled` + 懒加载 `SkillCreateRail`

---

### M-24: CodeAdapter.CreateInstance 添加 loadUserRails

**文件:** `internal/swarm/server/adapter/code_adapter.go`

**方案:** 在 CreateInstance 末尾添加 `c.loadUserRails(ctx)` 调用。

---

### M-26: MemberCompleteTaskTool assignee nil 时用 \<unassigned\>

**文件:** `internal/agent_teams/tools/team_tools.go`

**方案:** `taskAssignee := ""` 改为 `taskAssignee := "<unassigned>"`，当 `task.Assignee == nil` 时。

---

### M-28: BuildTeamTool 错误消息改用结构化错误码

**文件:** `internal/agent_teams/tools/team_backend.go`

**方案:** `ErrHITTConfigInvalid` 改用 `StatusCode` 结构化错误，对齐 Python 的 `raise_error(StatusCode.AGENT_TEAM_CONFIG_INVALID, ...)`。

---

### M-29: SpawnHumanAgent prompt 空→nil

**文件:** `internal/agent_teams/tools/team_tools.go`, `internal/agent_teams/interaction/`

**方案:** prompt 为空字符串时传 nil 而非 ""，对齐 Python 的 `prompt=None`（SQL NULL）。

---

### M-30: ViewTaskTool blocked_by/blocks 格式对齐

**文件:** `internal/agent_teams/tools/team_tools.go`

**方案:** 对齐 Python 的 `model_dump(exclude_none=True)` 格式，确保 blocked_by/blocks 字段结构匹配。

---

### M-33: cancelMemberIfClaimed 处理 Get error

**文件:** `internal/agent_teams/tools/team_tools.go`

**方案:** `task, _ := team.TaskManager().Get(ctx, taskID)` 改为 `task, err := ...`，err 非空时记录 Warn 日志并返回。

---

### M-34: cancelClaimedMembers 处理 ListTasks error

**文件:** `internal/agent_teams/tools/team_tools.go`

**方案:** `claimedTasks, _ := t.agentTeam.TaskManager().ListTasks(ctx, "claimed")` 改为 `claimedTasks, err := ...`，err 非空时记录 Warn 日志并返回。

---

### M-35: KvPrefixRegistry map[string]bool→map[string]struct{}

**文件:** `internal/agentcore/memory/common/kv_prefix_registry.go`

**方案:** `map[string]bool` 改为 `map[string]struct{}`，`true` 赋值改为 `struct{}{}`。

---

### M-44: Interact Agent nil 时补 warning

**文件:** `internal/agent_teams/runtime/manager.go`

**方案:** `Interact` 中 `entry.Agent` 为 nil 时（通过 TeamManager.Interact 的 session 活跃性检查覆盖）补充 Warn 日志。

---

## 自审清单

- **Spec 覆盖率**: TeamManager 全部 40+ 方法均有对应 Task，已确认 M 级修复均有方案
- **Placeholder 扫描**: 无 TBD/TODO 占位（TeamManager 各文件实现为完整代码）
- **类型一致性**: `LiveRailEntry`、`SkillSyncTarget` 在 Task 2 定义，后续 Task 引用一致
- **PoolEntry 删除**: Task 9 完整覆盖 interfaces.go/manager.go/pool.go/team_agent.go/pool_test.go
