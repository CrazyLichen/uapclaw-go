# 9.65a-4 TeamBackend 门面 + 回填设计

## 概述

实现 TeamBackend 门面（9.65a-4），对齐 Python `openjiuwen/agent_teams/tools/team.py`，
组合 TeamDatabase + TeamTaskManager + TeamMessageManager + Messager，提供 30+ 方法。
同时一步到位回填所有 ⤵️ 标记点（infra.go、agent_configurator.go、human_agent_inbox.go、
user_inbox.go、router.go、runtime/manager.go 等）。

## 在 Agent 会话中的流程位置

```
┌─────────────────────────────────────────────────────┐
│ 9.55 TeamAgent (生产级团队 Agent)                      │
│   ├── 9.56 SpawnManager (成员生成) ✅                  │
│   ├── 9.57 AgentConfigurator (配置器) ✅               │
│   ├── 9.58 InProcessSpawn (进程内生成) ✅               │
│   ├── 9.59 SessionManager + Interaction 层 ✅          │
│   ├── 9.60 StreamController (流控制) ✅                 │
│   ├── 9.61 RecoveryManager (恢复管理) ☐ ← 依赖 9.65a-4 │
│   └── 9.62 CoordinationKernel (协调内核) ☐             │
├─────────────────────────────────────────────────────┤
│ 9.65a TeamBackend 子系统                              │
│   ├── 9.65a-1 ✅ TeamDB 基础层 (DAO/FSM/数据模型)      │
│   ├── 9.65a-2 ✅ TaskDao + TaskManager (任务管理)      │
│   ├── 9.65a-3 ✅ MessageDao + MessageManager (消息管理) │
│   ├── 9.65a-4 ☐ TeamBackend 门面 (本设计)             │
│   └── 9.65a-5 ☐ SQL 实现                              │
└─────────────────────────────────────────────────────┘
```

TeamBackend 是**团队的统一业务门面**，位于数据层 → 业务层的分界线上。
上游（9.65a-1/2/3）提供纯数据和单域业务能力，TeamBackend 在此之上做跨域编排：
- 成员生命周期 + 事件发布 + 消息通知
- 团队生命周期 + 文件系统清理 + 回调触发
- HITT 名册缓存 + inbound 回调管理
- 跨域组合操作（取消任务 + 通知 assignee、审批 + 事件发布）

## 设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| 构造方式 | Functional Options 模式 | Go 惯用模式，可扩展性好 |
| 方法拆分 | 单文件单结构体（`team_backend.go`） | 对齐 Python 单类，简单直接 |
| HITT 缓存并发 | 独立 `hittMu sync.RWMutex` | 仅保护 HITT 缓存，DB 操作靠 DAO 层并发安全 |
| 文件清理 | 串行 `os.RemoveAll`，按深度排序 | Python 用 asyncio.to_thread 是为避免阻塞事件循环，Go 不需要并发 |
| 回填策略 | 9.65a-4 完整实现 + 回填一步到位 | 一次到位，避免多次修改同一文件 |
| 章节顺序 | 先实现 9.65a-4，再回 9.55 | 9.55-9.62 都消费 TeamBackend，先有门面再实现消费者 |
| RecoveryBackend 接口 | 省略，直接依赖 `*TeamBackend` | 对齐 Python，RecoveryManager 直接使用 TeamBackend |

## Python 并发模型分析

Python 的 TeamBackend 在 asyncio 单线程事件循环中运行，所有方法协作式调度。
`_human_agent_names`（set）和 `_human_agent_inbound_callbacks`（dict）天然无竞争。
Go 的 goroutine 模型理论上可能并发，但实际场景中 TeamBackend 在 Leader 进程内被调用，
真正的并发写 HITT 缓存的场景很少（build_team 一次性设置 + spawn_member 偶尔追加）。
因此仅对 HITT 缓存加独立 RWMutex，DB 操作靠 DAO 层自己的并发安全。

## 结构体定义

```go
// TeamBackend 团队后端门面，组合 DB + TaskManager + MessageManager + Messager。
// 对齐 Python: TeamBackend (openjiuwen/agent_teams/tools/team.py)
type TeamBackend struct {
    // ── 必填字段 ──
    teamName         string
    memberName       string
    isLeader         bool
    leaderMemberName string
    db               database.TeamDatabase
    messager         messager.Messager
    taskManager      *tools.TeamTaskManager
    messageManager   *tools.TeamMessageManager

    // ── 可选字段（Functional Options 注入）──
    teammateMode         string                        // 默认 BUILD_MODE
    predefinedMembers    []atschema.TeamMemberSpec     // 预定义成员
    modelConfigAllocator func(modelName string) *models.Allocation // 模型分配回调
    leaderAllocation     *models.Allocation            // Leader 模型分配
    onTeamCleaned        func(ctx context.Context) error // 团队清理回调
    onTeamBuilt          func(ctx context.Context) error // 团队构建回调
    planStorageDir       string
    planID               string

    // ── HITT 缓存（hittMu 保护）──
    hittMu               sync.RWMutex
    specEnableHITT       bool                  // spec 级天花板（不可变）
    enableHITT           bool                  // 运行时有效开关
    hittNames            map[string]struct{}   // human-agent 名册缓存
    hittInboundCallbacks map[string]OnInbound  // per-human-agent 回调

    // ── 文件系统清理路径 ──
    cleanupPaths map[string]struct{}
}
```

## 构造函数 + Functional Options

```go
type TeamBackendOption func(*TeamBackend)

func WithTeammateMode(mode string) TeamBackendOption { ... }
func WithPredefinedMembers(members []atschema.TeamMemberSpec) TeamBackendOption { ... }
func WithModelConfigAllocator(fn func(string) *models.Allocation) TeamBackendOption { ... }
func WithLeaderAllocation(a *models.Allocation) TeamBackendOption { ... }
func WithEnableHITT(enable bool) TeamBackendOption { ... }
func WithOnTeamCleaned(fn func(ctx context.Context) error) TeamBackendOption { ... }
func WithOnTeamBuilt(fn func(ctx context.Context) error) TeamBackendOption { ... }
func WithPlanStorageDir(dir string) TeamBackendOption { ... }
func WithPlanID(id string) TeamBackendOption { ... }
func WithLeaderMemberName(name string) TeamBackendOption { ... }

func NewTeamBackend(
    teamName, memberName string, isLeader bool,
    db database.TeamDatabase, msg messager.Messager,
    opts ...TeamBackendOption,
) *TeamBackend { ... }
```

## 方法清单（30+ 方法，对齐 Python）

### 成员生命周期

| 方法 | Python 对应 | 行号 | 说明 |
|------|-----------|------|------|
| `SpawnMember` | `spawn_member` | 232-306 | 创建成员记录 + HITT 缓存写透 |
| `Startup` | `startup` | 336-359 | 启动所有 UNSTARTED 成员 |
| `StartupMember` | `startup_member` | 361-396 | CAS 启动单个成员（UNSTARTED→STARTING） |
| `ShutdownMember` | `shutdown_member` | 514-598 | 关闭成员（FSM + 消息 + 事件） |
| `CancelMember` | `cancel_member` | 600-663 | 取消成员执行（重置 CLAIMED 任务 + 事件） |

### 团队生命周期

| 方法 | Python 对应 | 行号 | 说明 |
|------|-----------|------|------|
| `BuildTeam` | `build_team` | 937-1083 | 创建团队 + 注册 leader + 预定义成员 + HITT |
| `CleanTeam` | `clean_team` | 665-727 | 清理团队（全部 SHUTDOWN → 删 DB → 回调 → 清理路径 → 事件） |
| `ForceCleanTeam` | `force_clean_team` | 729-761 | 强制清理（shutdown_all + force_delete + 清理路径） |

### 查询

| 方法 | Python 对应 | 行号 | 说明 |
|------|-----------|------|------|
| `GetMember` | `get_member` | 763-772 | 获取成员 |
| `ListMembers` | `list_members` | 774-781 | 列出成员（排除自身） |
| `GetTeamInfo` | `get_team_info` | 783-789 | 获取团队信息 |
| `IsTeamCompleted` | `is_team_completed` | 791-828 | 团队完成判定（任务终态 + 成员 settle + 消息已读） |
| `GetTeamUpdatedAt` | `get_team_updated_at` | 830-839 | 变更检测时间戳 |
| `GetMembersMaxUpdatedAt` | `get_members_max_updated_at` | 841-849 | 成员变更时间戳 |

### 任务操作

| 方法 | Python 对应 | 行号 | 说明 |
|------|-----------|------|------|
| `CancelTask` | `cancel_task` | 851-896 | 取消任务 + 通知 assignee |
| `CancelAllTasks` | `cancel_all_tasks` | 898-935 | 批量取消 + 广播 |
| `ApprovePlan` | `approve_plan` | 398-462 | 审批计划 |
| `ApproveTool` | `approve_tool` | 464-512 | 审批工具调用 |

### HITT 管理

| 方法 | Python 对应 | 行号 | 说明 |
|------|-----------|------|------|
| `SpawnHumanAgent` | `spawn_human_agent` | 1085-1154 | 注册 human-agent 成员 |
| `RefreshHumanAgentRoster` | `refresh_human_agent_roster` | 1156-1188 | 从 DB 重建 HITT 名册 |
| `IsHumanAgent` | `is_human_agent` | 1190-1194 | 判断是否 human-agent（读缓存） |
| `RegisterHumanAgentInbound` | `register_human_agent_inbound` | 1196-1217 | 注册/清除 inbound 回调 |
| `GetHumanAgentInbound` | `get_human_agent_inbound` | 1219-1221 | 获取 inbound 回调 |
| `HumanAgentNames` | `human_agent_names` | 1223-1225 | HITT 名册快照 |
| `HITTEnabled` | `hitt_enabled` | 1227-1237 | HITT 能力开关 |

### 文件清理

| 方法 | Python 对应 | 行号 | 说明 |
|------|-----------|------|------|
| `RegisterCleanupPath` | `register_cleanup_path` | 196-204 | 注册清理路径（去重） |
| `RemoveCleanupPaths` | `_remove_cleanup_paths` | 206-230 | 串行删除清理路径（按深度排序，失败不中止） |

### 内部辅助

| 方法 | Python 对应 | 行号 | 说明 |
|------|-----------|------|------|
| `spawnAndPublish` | `_spawn_and_publish` | 308-334 | 生成 + 发布事件（共享辅助） |

### 属性访问

| 方法 | Python 对应 | 说明 |
|------|-----------|------|
| `TeamName()` | `team_name` | 团队名 |
| `MemberName()` | `member_name` | 当前成员名 |
| `IsLeader()` | `is_leader` | 是否 Leader |
| `DB()` | `db` | 数据库访问 |
| `TaskManager()` | `task_manager` | 任务管理器 |
| `MessageManager()` | `message_manager` | 消息管理器 |

## 回填清单

### 第一类：TeamBackend 类型替换（any → *TeamBackend / *TeamMessageManager）

| 文件 | 行号 | 当前 | 回填后 |
|------|------|------|--------|
| `infra.go:19` | 19 | `TeamBackend any` | `TeamBackend *TeamBackend` |
| `infra.go:28` | 28 | `MessageManager any` | `MessageManager *TeamMessageManager` |
| `agent_configurator.go:395` | 395 | `TeamBackend() any` | `TeamBackend() *TeamBackend` |
| `agent_configurator.go:400` | 400 | `SetTeamBackend(v any)` | `SetTeamBackend(v *TeamBackend)` |
| `team_agent.go:264` | 264 | `TeamBackend() any` | `TeamBackend() *TeamBackend` |
| `human_agent_inbox.go:52` | 52 | `team any` | `team *TeamBackend` |
| `human_agent_inbox.go:55` | 55 | `messageManager any` | `messageManager *TeamMessageManager` |
| `runtime/pool.go:19` | 19 | `*TeamAgent` 注释 | 类型可用 |

### 第二类：方法实现回填（stub → 真实调用）

| 文件 | 行号 | 当前 stub | 回填后调用 | Python 对齐 |
|------|------|----------|-----------|-------------|
| `human_agent_inbox.go:165` | 165 | `names := []string{agentteams.HumanAgentMemberName}` | `names := h.team.HumanAgentNames()` | `self._team.human_agent_names()` |
| `human_agent_inbox.go:232` | 232 | `return true, nil` | `member, _ := h.team.GetMember(ctx, name); return member != nil, nil` | `await backend.get_member(name)` |
| `human_agent_inbox.go:114` | 114 | `msgID := "stub-ha-broadcast-msg-id"` | `msgID := h.messageManager.BroadcastMessage(...)` | `await self._mm.broadcast_message(...)` |
| `runtime/manager.go:229` | 229 | `memberExists := func(name string) (bool, error) { return true, nil }` | `member, _ := team.GetMember(ctx, name); return member != nil, nil` | `await backend.get_member(name)` |
| `runtime/manager.go:182` | 182 | `RegisterHumanAgentInbound` 返回 `false, nil` | `team.RegisterHumanAgentInbound(ctx, memberName, callback)` | `team_backend.register_human_agent_inbound(...)` |
| `interaction/router.go:234` | 234 | `// ⤵️ 待 9.55 回填` | `messageManager.SendMessage(content, to, from)` | `await self._mm.send_message(...)` |
| `interaction/router.go:245` | 245 | `// ⤵️ 待 9.55 回填` | `messageManager.SendMessage(content, to, from)` | `await self._mm.send_message(...)` |
| `interaction/user_inbox.go:55-81` | 55-81 | `// ⤵️ 待 9.55 回填` | `messageManager.SendMessage/BroadcastMessage` | `await self._mm.send_message/broadcast_message` |

### 第三类：部分回填（9.65a-4 解锁部分，9.55 TeamAgent 配合）

| 文件 | 行号 | 标记内容 | 9.65a-4 能做的 | 还需 9.55 才能做的 |
|------|------|---------|---------------|-------------------|
| `agent_configurator.go:246` | 246 | `SetupTeamBackend` 返回 `nil` | **完整实现** — 构造 TeamBackend 并注入 | — |
| `spawn_manager.go:200` | 200 | `TeamBackend()` nil check | `team.GetMember()` 可用 | — |
| `team_agent.go:335` | 335 | 检查 team_member.status() | `team.GetMember()` 可查 | — |
| `runtime/manager.go:262` | 262 | `agent.(*TeamAgent).DeliverInput(ctx, content)` | — | 需要 TeamAgent 的 DeliverInput |
| `runtime/manager.go:269` | 269 | `inbox := interaction.NewUserInbox(nil)` | 可以注入 messageManager | — |
| `runtime/manager.go:272` | 272 | `agent.AutoStartAll()` | — | 需要 TeamAgent 的 AutoStartAll |
| `runtime/manager.go:276` | 276 | `agent.AutoStartMember(target)` | — | 需要 TeamAgent 的 AutoStartMember |
| `runtime/manager.go:282-285` | 282 | `nil` team/messageManager/agentLookup/onInbound | 可注入 team + messageManager | agentLookup + onInbound 需 TeamAgent |
| `team_agent.go:493-524` | 493 | Interact/Broadcast/HumanAgentSay | — | 需要 TeamAgent 完整方法 |
| `stream_controller.go:61` | 61 | pendingInterruptResumes 类型 | — | 需要 TeamAgent 类型定义 |

## 9.61 RecoveryManager — 9.65a-4 解锁

Python 的 RecoveryManager 直接使用 TeamBackend，不定义 RecoveryBackend 接口。
Go 同样省略 RecoveryBackend 接口，直接依赖 `*TeamBackend`。

RecoveryManager 调用的 3 个 TeamBackend 方法：

| Python 调用 | Go 等价 | 说明 |
|------------|--------|------|
| `team_backend.refresh_human_agent_roster()` | `teamBackend.RefreshHumanAgentRoster(ctx)` | 重建 HITT 名册 |
| `team_backend.list_members()` | `teamBackend.ListMembers(ctx, excludeSelf)` | 列出成员 |
| `team_backend.db.member.update_member_status(...)` | `teamBackend.DB().Member().UpdateMemberStatus(...)` | 更新成员状态 |

## 实现步骤

1. **实现 `tools/team_backend.go`** — TeamBackend 结构体 + 全部 30+ 方法 + Functional Options
2. **回填第一类**（类型替换）— 8 个文件的 `any` → `*TeamBackend` / `*TeamMessageManager`
3. **回填第二类**（方法实现）— 8 个 stub → 真实调用
4. **回填第三类（部分）** — `agent_configurator.go:246` 的 `SetupTeamBackend` 完整实现 + `spawn_manager.go:200` 的 TeamBackend nil check + `team_agent.go:335` 的成员状态检查
5. **更新 IMPLEMENTATION_PLAN.md** — 9.65a-4 标记 ✅，9.61 标记 🔄
6. **单元测试** — `team_backend_test.go`，覆盖率 ≥ 85%
7. **更新 doc.go** — tools 包文档添加 team_backend.go 条目

## 关键 Python 方法实现细节

### spawn_member（Python 232-306）

```
1. 校验：仅检查 DB 中是否已存在同名成员，格式校验留给 Tool 层（9.68）
2. 查 agent_card：若 predefined_members 中有则用其 persona，否则用参数
3. 模型分配：调用 _allocate_model_config(model_name)
4. DB 写入：db.member.create_member(...)
5. HITT 缓存写透：若 role == HUMAN_AGENT，_human_agent_names.add(member_name)
6. 日志：team_logger.info
7. 返回 MemberOpResult
```

### build_team（Python 937-1083）

```
1. HITT 能力开关：effective_enable_hitt = _spec_enable_hitt and enable_hitt
2. DB 初始化：不在 build_team 中调用（db.initialize 在 runtime/manager 和 coordination/kernel 中调用，create_cur_session_tables 在 SessionManager.bind_session 中调用）
3. 创建团队行：db.team.create_team(team_name, display_name, leader_member_name, desc, prompt)
4. 注册 Leader：db.member.create_member(leader 信息)
5. 模型分配持久化：leader_allocation 写入 leader 的 model_ref_json
6. 注册预定义成员：循环调用 _spawn_and_publish
7. HITT 名册重建：refresh_human_agent_roster()
8. 回调触发：on_team_built()
9. 事件发布：TeamCreatedEvent
```

### clean_team（Python 665-727）

```
1. 查询活跃成员：db.member.get_team_members(team_name, status=非SHUTDOWN)
2. 若有活跃成员：warn + return False
3. 删除团队行：db.team.delete_team(team_name)
4. 删动态表：db.drop_cur_session_tables()
5. 回调触发：on_team_cleaned()
6. 清理路径：_remove_cleanup_paths()
7. 事件发布：TeamCleanedEvent
```

### shutdown_member（Python 514-598）

```
1. 查成员：db.member.get_member(member_name, team_name)
2. 若成员不存在：return MemberOpResult.fail
3. 若成员已是 SHUTDOWN/ERROR：return MemberOpResult.fail
4. FSM 转换：try_transition_member_status(member_name, team_name, current_status, SHUTDOWN_REQUESTED)
5. 若 CAS 失败：return MemberOpResult.fail
6. 日志：team_logger.info
7. 取消该成员的任务：cancel_all_tasks(team_name, skip_assignees=[member_name])
8. 通知成员：messager.publish(MemberShutdownEvent)
9. 返回 MemberOpResult.ok
```

### cancel_member（Python 600-663）

```
1. 查成员：db.member.get_member(member_name, team_name)
2. 若成员不存在：return MemberOpResult.fail
3. 若成员已是终态：return MemberOpResult.fail
4. FSM 转换：try_transition_member_status(member_name, team_name, current_status, SHUTDOWN_REQUESTED)
5. 若 CAS 失败：return MemberOpResult.fail
6. 重置该成员的 CLAIMED 任务：遍历 task_manager.list_tasks，将 CLAIMED 任务 reset
7. 事件发布：MemberCanceledEvent
8. 返回 MemberOpResult.ok
```

### is_team_completed（Python 791-828）

```
1. 查团队：db.team.get_team(team_name)
2. 若团队不存在：return None
3. 查成员：db.member.get_team_members(team_name, status="")
4. 查任务：db.task.get_team_tasks(team_name, status="")
5. 判定：所有任务终态 + 所有成员 settled + 无未读消息
6. 返回 TeamCompletionSnapshot 或 None
```

### approve_plan（Python 398-462）

```
1. 审批计划：task_manager.approve_plan(task_id)
2. 若失败：return MemberOpResult.fail
3. 查任务：task_manager.get(task_id)
4. 事件发布：TaskPlanResponseEvent(approved=true)
5. 日志：team_logger.info
6. 返回 MemberOpResult.ok
```

### approve_tool（Python 464-512）

```
1. 校验：member_name 非空
2. 事件发布：ToolApprovalResultEvent(approved, feedback, auto_confirm)
3. 日志：team_logger.info
4. 返回 MemberOpResult.ok
```

### _remove_cleanup_paths（Python 206-230）

```
1. 若 _cleanup_paths 为空：return
2. 按深度排序（最深先删）：sorted by len(Path(p).parts), reverse=True
3. 逐个删除：shutil.rmtree(target)（asyncio.to_thread 包装）
4. 失败不中止：try/except + team_logger.error
5. 日志：team_logger.info("Removed team filesystem path: {target}")
```

### refresh_human_agent_roster（Python 1156-1188）

```
1. 清空缓存：_human_agent_names.clear()
2. 从 DB 查询：db.member.list_human_agent_names(team_name)
3. 写入缓存：_human_agent_names.update(names)
4. 日志：team_logger.info
```

### register_human_agent_inbound（Python 1196-1217）

```
1. 若 callback 为 None：从 _human_agent_inbound_callbacks 中 pop
2. 若 callback 非 None：
   a. 校验：member_name 是否在 _human_agent_names 中
   b. 若不在：raise UnknownHumanAgentError
   c. 写入：_human_agent_inbound_callbacks[member_name] = callback
```
