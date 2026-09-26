# 9.61a + 9.61 — Metadata Namespace 函数 + RecoveryManager 恢复管理器

## 概述

本文档覆盖实现计划中 **9.61a（Team Namespace 持久化函数）** 和 **9.61（RecoveryManager 恢复管理器）** 两个步骤。

9.61a 提供 Team Namespace 的读写函数，对齐 Python `openjiuwen/agent_teams/runtime/metadata.py`。
9.61 实现 RecoveryManager 结构体及全部方法，对齐 Python `openjiuwen/agent_teams/agent/recovery_manager.py`，并回填所有 `TODO(#9.61)` 占位。

实现顺序：**先 9.61a 再 9.61**（RecoveryManager 的 `PersistLeaderConfig` / `PersistAllocatorState` 依赖 metadata 函数）。

RecoverFromSession 函数（`team_agent.go:785`）仅定义签名，实现体标注 `TODO(#9.55)`，待 9.55 完整实现。

---

## 9.61a — Metadata Namespace 函数

### 包位置

`internal/agent_teams/runtime/metadata.go`

对应 Python：`openjiuwen/agent_teams/runtime/metadata.py`

### 常量

| Go 常量 | Python 常量 | 值 | 说明 |
|---------|------------|-----|------|
| `TeamsKey` | `TEAMS_KEY` | `"teams"` | session state 中 teams 命名空间的顶层 key |
| `TeamDBStateKey` | `TEAM_DB_STATE_KEY` | `"db_state"` | 桶内 db_state 字段 key |
| `TeamDBStatePendingCreate` | `TEAM_DB_STATE_PENDING_CREATE` | `"pending_create"` | DB 表待创建 |
| `TeamDBStateCreated` | `TEAM_DB_STATE_CREATED` | `"created"` | DB 表已创建 |
| `TeamDBStateCleaned` | `TEAM_DB_STATE_CLEANED` | `"cleaned"` | DB 表已清理 |

### 导出函数（8 个，严格对齐 Python）

所有函数接收 `interfaces.SessionFacade` 作为 session 参数，通过 `sess.GetState()` / `sess.UpdateState()` 操作 `state["teams"]` 下的分桶数据。

#### 1. ReadTeamsBucket

```go
// ReadTeamsBucket 读取整个 teams 命名空间。
// 空时返回空 map（不返回 nil），对齐 Python 返回 {} 而非 None。
// Python: read_teams_bucket(session) -> dict[str, dict[str, Any]]
func ReadTeamsBucket(sess interfaces.SessionFacade) map[string]map[string]any
```

内部步骤：
1. 调 `sess.GetState(state.StateKey(TeamsKey))`，断言类型为 `map[string]any`
2. 遍历顶层 map，将每个 `map[string]any` 值断言为 `map[string]any`，收集到 `map[string]map[string]any`
3. 类型断言失败的桶跳过（防御性）
4. 若 GetState 返回 nil 或类型不对，返回空 `map[string]map[string]any{}`

#### 2. ReadTeamNamespace

```go
// ReadTeamNamespace 读取单个 team 桶。
// 不存在时返回 nil，对齐 Python 返回 None。
// Python: read_team_namespace(session, team_name) -> dict | None
func ReadTeamNamespace(sess interfaces.SessionFacade, teamName string) map[string]any
```

内部步骤：
1. 调 `ReadTeamsBucket(sess)` 获取整个 teams 命名空间
2. 查 `teams[teamName]`，存在则返回，不存在返回 nil

#### 3. ReadTeamNamesInSession

```go
// ReadTeamNamesInSession 列出 session 中已持久化的 team 名。
// Python: read_team_names_in_session(session) -> list[str]
func ReadTeamNamesInSession(sess interfaces.SessionFacade) []string
```

内部步骤：
1. 调 `ReadTeamsBucket(sess)` 获取整个 teams 命名空间
2. 收集所有 key 到 `[]string` 返回

#### 4. WriteTeamNamespace

```go
// WriteTeamNamespace 整体覆盖某 team 桶。
// Python: write_team_namespace(session, team_name, payload) -> None
func WriteTeamNamespace(sess interfaces.SessionFacade, teamName string, payload map[string]any)
```

内部步骤：
1. 调 `ReadTeamsBucket(sess)` 获取当前 teams 命名空间
2. 设置 `teams[teamName] = payload`（payload 作为 `map[string]any` 存入）
3. 调 `sess.UpdateState(map[string]any{TeamsKey: teams})` 写回

#### 5. MergeTeamNamespace

```go
// MergeTeamNamespace 浅合并 partial 到某 team 桶，key 覆盖同名，不动其他。
// Python: merge_team_namespace(session, team_name, partial) -> None
func MergeTeamNamespace(sess interfaces.SessionFacade, teamName string, partial map[string]any)
```

内部步骤：
1. 调 `ReadTeamNamespace(sess, teamName)` 获取当前桶
2. 若为 nil，创建空 `map[string]any{}`
3. 遍历 partial，逐 key 覆盖到桶中
4. 调 `WriteTeamNamespace(sess, teamName, merged)` 写回

#### 6. ReadTeamDBState

```go
// ReadTeamDBState 读取 db_state 字段值。
// 不存在时返回空字符串，对齐 Python 返回 None（Go 用空串表示"无值"）。
// Python: read_team_db_state(session, team_name) -> str | None
func ReadTeamDBState(sess interfaces.SessionFacade, teamName string) string
```

内部步骤：
1. 调 `ReadTeamNamespace(sess, teamName)` 获取桶
2. 查 `bucket[TeamDBStateKey]`，断言为 string，返回
3. 不存在或类型不匹配返回空串 `""`

#### 7. MergeTeamDBState

```go
// MergeTeamDBState 通过 MergeTeamNamespace 写入 db_state 字段。
// Python: merge_team_db_state(session, team_name, state) -> None
func MergeTeamDBState(sess interfaces.SessionFacade, teamName string, dbState string)
```

内部步骤：
1. 调 `MergeTeamNamespace(sess, teamName, map[string]any{TeamDBStateKey: dbState})`

#### 8. RemoveTeamNamespace

```go
// RemoveTeamNamespace 删除 team 桶，返回是否实际删除。
// Python: remove_team_namespace(session, team_name) -> bool
func RemoveTeamNamespace(sess interfaces.SessionFacade, teamName string) bool
```

内部步骤：
1. 调 `ReadTeamsBucket(sess)` 获取当前 teams 命名空间
2. 检查 `teams[teamName]` 是否存在
3. 若存在，删除该 key，调 `sess.UpdateState(map[string]any{TeamsKey: teams})` 写回，返回 true
4. 若不存在，返回 false

### doc.go 更新

在 `agent_teams/runtime/doc.go` 的文件目录中添加：

```
├── metadata.go       # Team Namespace 读写函数（对齐 Python metadata.py）
```

### 测试

`metadata_test.go` 中使用 fake `SessionFacade`（内存 map 实现 `GetState` / `UpdateState` / `DumpState` 等），覆盖：
- 空 teams 命名空间读写
- WriteTeamNamespace 后 ReadTeamNamespace 一致
- MergeTeamNamespace 浅合并（不覆盖无关 key）
- ReadTeamDBState / MergeTeamDBState 正常 + 空值
- RemoveTeamNamespace 存在/不存在
- ReadTeamNamesInSession

---

## 9.61 — RecoveryManager 恢复管理器

### 文件位置

`internal/agent_teams/agent/recovery_manager.go`

对应 Python：`openjiuwen/agent_teams/agent/recovery_manager.py`

### 辅助类型

```go
// LiveTeammate 活跃队友快照（成员名 + 当前状态）。
// Python: collect_live_teammates_for_session_switch 返回的 list[tuple[str, MemberStatus]]
type LiveTeammate struct {
    // MemberName 成员名
    MemberName string
    // Status 当前 DB 状态
    Status MemberStatus
}
```

### 结构体

```go
// RecoveryManager 团队恢复管理器。
// 负责：团队恢复、成员状态转换、Leader 配置持久化、Allocator 状态持久化。
//
// Python: openjiuwen/agent_teams/agent/recovery_manager.py
type RecoveryManager struct {
    // configurator Agent 配置器
    configurator *AgentConfigurator
    // spawnManager 子进程管理器
    spawnManager *SpawnManager
}
```

### 方法（7 个，严格对齐 Python）

#### 1. NewRecoveryManager

```go
// NewRecoveryManager 创建恢复管理器实例。
// Python: RecoveryManager.__init__(configurator, spawn_manager)
func NewRecoveryManager(configurator *AgentConfigurator, spawnManager *SpawnManager) *RecoveryManager
```

内部步骤（对齐 Python `__init__`）：
1. 存储 `configurator` 和 `spawnManager` 引用

#### 2. RecoverTeam

```go
// RecoverTeam 从数据库状态恢复团队。
// 返回成功重启的成员名列表。
// Python: async recover_team(self) -> list[str]
func (m *RecoveryManager) RecoverTeam(ctx context.Context) []string
```

内部步骤（严格对齐 Python `recover_team`）：
1. 从 `m.configurator.TeamBackend()` 取 `teamBackend`，若 nil 则返回 `[]string{}`
2. 取 `memberName := m.configurator.MemberName()`
3. 日志 Info：`"[{memberName}] recovering team"`（字段 `member_name`，值 `memberName` 或 `"?"`）
4. 调 `teamBackend.RefreshHumanAgentRoster(ctx)` 重建内存中的 HITT 名册
   - Python 注释：冷启动 Leader 后 HITT 缓存为空，需在 restart_teammate 扇出前重建
5. 调 `teamBackend.ListMembers(ctx)` 获取所有成员
6. 初始化 `restarted := []string{}`
7. 遍历所有成员：
   - 若 `member.MemberName == memberName`（自身），跳过（continue）
   - 取 `teamName := m.configurator.TeamName()`
   - 若 teamName 非空，调 `teamBackend.DB().Member().UpdateMemberStatus(ctx, member.MemberName, teamName, MemberStatusRestarting)` 更新状态为 RESTARTING
   - 调 `m.spawnManager.RestartTeammate(ctx, member.MemberName, 3)`，若 err == nil 则 append 到 restarted
8. 返回 restarted

#### 3. PersistLeaderConfig

```go
// PersistLeaderConfig 持久化 Leader 配置到 team namespace。
// Python: persist_leader_config(self, session) -> None
func (m *RecoveryManager) PersistLeaderConfig(sess interfaces.SessionFacade)
```

内部步骤（严格对齐 Python `persist_leader_config`）：
1. 从 configurator 取 `spec := m.configurator.Spec()`、`ctx := m.configurator.RuntimeContext()`、`teamName := m.configurator.TeamName()`
2. 若 spec == nil 或 ctx == nil 或 teamName == ""，直接 return
3. 构建 payload（`map[string]any`）：
   - `"spec"`: spec 的序列化（对齐 Python `spec.model_dump(mode="json")`，Go 中用 JSON marshal）
   - `"context"`: ctx 的序列化（对齐 Python `ctx.model_dump(mode="json")`）
   - `TeamDBStateKey`: 调 `ReadTeamDBState(sess, teamName)`，若返回空串则 fallback 为 `TeamDBStatePendingCreate`
4. 取 `allocator := m.configurator.ModelAllocator()`，若非 nil，加入 `"model_allocator_state": allocator.StateDict()`
5. 调 `WriteTeamNamespace(sess, teamName, payload)`

#### 4. markTeammateRestartingForSessionSwitch

```go
// markTeammateRestartingForSessionSwitch 将活跃队友的状态转为 RESTARTING。
// READY/BUSY/SHUTDOWN_REQUESTED/UNSTARTED 需先经 ERROR 中转。
// 返回是否成功转换。
// Python: _mark_teammate_restarting_for_session_switch(self, member_name, current_status) -> bool
func (m *RecoveryManager) markTeammateRestartingForSessionSwitch(
    ctx context.Context,
    memberName string,
    currentStatus MemberStatus,
) bool
```

内部步骤（严格对齐 Python `_mark_teammate_restarting_for_session_switch`）：
1. 取 `teamBackend := m.configurator.TeamBackend()`，若 nil 返回 false
2. 取 `teamName := m.configurator.TeamName()`，若空返回 false
3. 取 `db := teamBackend.DB()`
4. 若 `currentStatus == MemberStatusRestarting`，直接返回 true
5. 定义可直接转换到 RESTARTING 的状态集合：`{MemberStatusPaused, MemberStatusStopped, MemberStatusError, MemberStatusShutdown}`
6. 若 `currentStatus` 不在上述集合中（即 READY/BUSY/SHUTDOWN_REQUESTED/UNSTARTED）：
   - 调 `db.Member().UpdateMemberStatus(ctx, memberName, teamName, MemberStatusError)` 转为 ERROR
   - 若更新失败（返回 false），日志 Warn：`"Failed to move teammate {memberName} from {currentStatus} to ERROR before session rebind"`，返回 false
   - 更新 `currentStatus = MemberStatusError`
7. 若 `currentStatus == MemberStatusRestarting`，返回 true（上一步可能已转为此状态）
8. 调 `db.Member().UpdateMemberStatus(ctx, memberName, teamName, MemberStatusRestarting)` 转为 RESTARTING
9. 若更新失败，日志 Warn：`"Failed to move teammate {memberName} into RESTARTING during session rebind"`，返回 false
10. 返回 true

#### 5. CollectLiveTeammatesForSessionSwitch

```go
// CollectLiveTeammatesForSessionSwitch 快照需要 session 切换时重新绑定的活跃队友。
// 仅 Leader 角色且 teamBackend 存在时执行。
// Python: collect_live_teammates_for_session_switch(self) -> list[tuple[str, MemberStatus]]
func (m *RecoveryManager) CollectLiveTeammatesForSessionSwitch(ctx context.Context) []LiveTeammate
```

内部步骤（严格对齐 Python `collect_live_teammates_for_session_switch`）：
1. 若 `m.configurator.Role() != TeamRoleLeader` 或 `m.configurator.TeamBackend() == nil`，返回空
2. 调 `teamBackend.ListMembers(ctx)` 获取成员列表
3. 取 `leaderMemberName := m.configurator.MemberName()`
4. 取 `spawned := m.spawnManager.SpawnedHandles()` — map[string]*InprocessHandle
5. 构建 `liveTeammates := map[string]struct{}{}`，遍历 spawned：
   - `memberName != leaderMemberName` 且 `handle != nil`（存活），加入集合
6. 遍历 DB 成员列表：
   - 若 `member.MemberName` 不在 liveTeammates 中，跳过
   - 解析 `memberStatus := MemberStatus(member.Status)`（对齐 Python `MemberStatus(member.status)`）
   - 若 memberStatus 在排除集合 `{MemberStatusUnstarted, MemberStatusShutdown, MemberStatusStopped}` 中，跳过
   - append `LiveTeammate{MemberName: member.MemberName, Status: memberStatus}` 到结果
7. 返回结果

#### 6. RestartForSessionSwitch

```go
// RestartForSessionSwitch 在 session 切换后重启指定队友。
// cleanupFirst=true 时先清理旧 handle 再重启；false 表示协调层已清理。
// Python: restart_for_session_switch(self, recoverable_members, *, cleanup_first) -> None
func (m *RecoveryManager) RestartForSessionSwitch(
    ctx context.Context,
    recoverableMembers []LiveTeammate,
    cleanupFirst bool,
)
```

内部步骤（严格对齐 Python `restart_for_session_switch`）：
1. 遍历 `recoverableMembers`
2. 若 `cleanupFirst` 为 true，调 `m.spawnManager.CleanupTeammate(memberName)` 清理旧 handle
3. 调 `m.markTeammateRestartingForSessionSwitch(ctx, memberName, memberStatus)`，若返回 false 则 continue（跳过该队友）
4. 调 `m.spawnManager.RestartTeammate(ctx, memberName, 3)` 重启（忽略返回错误，对齐 Python 不抛异常）

#### 7. PersistAllocatorState

```go
// PersistAllocatorState 持久化 allocator 状态到 team namespace。
// Python: persist_allocator_state(self, team_session) -> None
func (m *RecoveryManager) PersistAllocatorState(sess interfaces.SessionFacade)
```

内部步骤（严格对齐 Python `persist_allocator_state`）：
1. 取 `allocator := m.configurator.ModelAllocator()`、`teamName := m.configurator.TeamName()`
2. 若 sess 为 nil 或 allocator 为 nil 或 teamName 为空，直接 return
3. defer recover 或 try 逻辑：
   - 调 `MergeTeamNamespace(sess, teamName, map[string]any{"model_allocator_state": allocator.StateDict()})`
4. 若发生异常/错误，日志 Error：`"[{memberName}] failed to persist allocator state: {err}"`，字段 `member_name` 值为 `m.configurator.MemberName()` 或 `"?"`

### 日志对齐（4 处，对齐 Python team_logger）

| # | Python 日志 | Go 日志 | 级别 |
|---|------------|---------|------|
| 1 | `team_logger.info("[{}] recovering team", member_name or "?")` | `logger.Info(ComponentAgentCore).Str("member_name", name).Msg("recovering team")` | Info |
| 2 | `team_logger.warning("Failed to move teammate {} from {} to ERROR before session rebind", member_name, current_status.value)` | `logger.Warn(ComponentAgentCore).Str("member_name", memberName).Str("current_status", currentStatus.String()).Msg("Failed to move teammate from current_status to ERROR before session rebind")` | Warn |
| 3 | `team_logger.warning("Failed to move teammate {} into RESTARTING during session rebind", member_name)` | `logger.Warn(ComponentAgentCore).Str("member_name", memberName).Msg("Failed to move teammate into RESTARTING during session rebind")` | Warn |
| 4 | `team_logger.error("[{}] failed to persist allocator state: {}", self._configurator.member_name or "?", e)` | `logger.Error(ComponentAgentCore).Str("member_name", name).Err(err).Msg("failed to persist allocator state")` | Error |

### doc.go 更新

```
recovery_manager.go # ⤵️ 回填: #9.61 恢复管理器
```
→
```
recovery_manager.go # 恢复管理器（团队恢复、会话切换、配置持久化）
```

---

## 回填 TODO(#9.61)

### session_manager.go

| 位置 | 当前 | 回填后 |
|------|------|--------|
| 字段 `recoveryManager any` | `// TODO(#9.61): 替换 any 为 RecoveryManager 类型` | `recoveryManager *RecoveryManager` |
| `NewSessionManager` 参数 | `recoveryManager any, // TODO(#9.61)` | `recoveryManager *RecoveryManager` |
| BindSession 步骤4 | `// TODO(#9.61): if teamBackend := m.configurator.TeamBackend(); ...` | `if teamBackend := m.configurator.TeamBackend(); teamBackend != nil { if err := teamBackend.DB().CreateCurSessionTables(ctx); err != nil { logger.Warn(...).Err(err).Msg("创建会话表失败") } }` |
| BindSession 步骤5 | `// TODO(#9.61): if m.configurator.Role() == TeamRoleLeader ...` | `if m.configurator.Role() == schema.TeamRoleLeader && m.configurator.Spec() != nil && sess != nil { m.recoveryManager.PersistLeaderConfig(sess) }` |
| ResumeForNewSession 步骤1 | `// TODO(#9.61): recoverableMembers := m.recoveryManager.CollectLiveTeammatesForSessionSwitch()` | `recoverableMembers := m.recoveryManager.CollectLiveTeammatesForSessionSwitch(ctx)` |
| ResumeForNewSession 步骤3-4 | `// TODO(#9.61): if m.configurator.Role() != Leader ...` | `if m.configurator.Role() == schema.TeamRoleLeader { m.recoveryManager.RestartForSessionSwitch(ctx, recoverableMembers, true) }` |
| RecoverForExistingSession 步骤1 | `// TODO(#9.61): recoverableMembers := ...` | `recoverableMembers := m.recoveryManager.CollectLiveTeammatesForSessionSwitch(ctx)` |
| RecoverForExistingSession 步骤3-4 | `// TODO(#9.61): ...` | `if m.configurator.Role() == schema.TeamRoleLeader { m.recoveryManager.RestartForSessionSwitch(ctx, recoverableMembers, false) }` |

### team_agent.go

| 位置 | 当前 | 回填后 |
|------|------|--------|
| 字段 `recoveryManager any` | `// TODO(#9.61): RecoveryManager 类型` | `recoveryManager *RecoveryManager` |
| `NewTeamAgent` 构造 | `// TODO(#9.61): 构建 RecoveryManager(configurator, spawnManager)` | `recoveryManager: NewRecoveryManager(configurator, spawnManager)` |
| `RecoverTeam` 方法 | `// TODO(#9.61): recoveryManager.recover_team()` | `return a.recoveryManager.RecoverTeam(ctx), nil` |
| `PersistSessionManifest` | `// TODO(#9.61): persist_leader_config` | `a.recoveryManager.PersistLeaderConfig(sess)` |
| `UpdateModelPool` | `// TODO(#9.61): persist_leader_config` | `a.recoveryManager.PersistLeaderConfig(sess)` |
| `RecoveryManager()` 访问器 | 返回 `any` | 返回 `*RecoveryManager` |

### RecoverFromSession — 占位函数

```go
// RecoverFromSession 从 session 状态恢复 Leader TeamAgent。
// 流程：读取 team namespace → 解析 spec/context → NewTeamAgent → configure → restore_allocator_state → set_session_id
//
// Python: 对应 leader 侧团队恢复流程
// TODO(#9.55): 实现完整恢复逻辑
func RecoverFromSession(sess interfaces.SessionFacade, card *agentschema.AgentCard) (*TeamAgent, error) {
    return nil, fmt.Errorf("RecoverFromSession 尚未实现（#9.55）")
}
```

### session_manager_test.go

- 移除 `TODO(#9.61)` 注释
- 补充 RecoveryManager 相关的测试用例（使用 fake RecoveryManager）

---

## IMPLEMENTATION_PLAN.md 状态更新

完成 9.61a 后：
- 9.61a 行标记为 ✅

完成 9.61 后：
- 9.61 行标记为 ✅

---

## 测试策略

### metadata_test.go

使用 fake `SessionFacade`（内存 map 实现），覆盖率目标 ≥ 85%：

| 测试用例 | 覆盖函数 |
|---------|---------|
| 空 teams 命名空间 → ReadTeamsBucket 返回空 map | ReadTeamsBucket |
| WriteTeamNamespace → ReadTeamNamespace 一致 | WriteTeamNamespace, ReadTeamNamespace |
| 多次 WriteTeamNamespace 覆盖 | WriteTeamNamespace |
| MergeTeamNamespace 浅合并不覆盖无关 key | MergeTeamNamespace |
| MergeTeamNamespace 覆盖同名 key | MergeTeamNamespace |
| ReadTeamDBState 正常读取 | ReadTeamDBState |
| ReadTeamDBState 不存在返回空串 | ReadTeamDBState |
| MergeTeamDBState 写入后读取一致 | MergeTeamDBState, ReadTeamDBState |
| RemoveTeamNamespace 存在时删除返回 true | RemoveTeamNamespace |
| RemoveTeamNamespace 不存在返回 false | RemoveTeamNamespace |
| ReadTeamNamesInSession 返回所有 key | ReadTeamNamesInSession |

### recovery_manager_test.go

使用 mock AgentConfigurator（提供 Spec/TeamBackend/MemberName 等访问器）、mock SpawnManager、mock TeamBackend，覆盖率目标 ≥ 85%：

| 测试用例 | 覆盖方法 |
|---------|---------|
| RecoverTeam: teamBackend nil 返回空 | RecoverTeam |
| RecoverTeam: 正常恢复跳过自身 | RecoverTeam |
| RecoverTeam: 刷新 HITT 名册后重启 | RecoverTeam |
| PersistLeaderConfig: spec nil 时跳过 | PersistLeaderConfig |
| PersistLeaderConfig: 正常持久化含 allocator | PersistLeaderConfig |
| PersistLeaderConfig: 正常持久化无 allocator | PersistLeaderConfig |
| markTeammateRestarting: RESTARTING 直接返回 true | markTeammateRestartingForSessionSwitch |
| markTeammateRestarting: PAUSED 可直接转 | markTeammateRestartingForSessionSwitch |
| markTeammateRestarting: READY 需经 ERROR 中转 | markTeammateRestartingForSessionSwitch |
| markTeammateRestarting: ERROR 中转失败返回 false | markTeammateRestartingForSessionSwitch |
| markTeammateRestarting: RESTARTING 转换失败返回 false | markTeammateRestartingForSessionSwitch |
| CollectLiveTeammates: 非 Leader 返回空 | CollectLiveTeammatesForSessionSwitch |
| CollectLiveTeammates: Leader 过滤终态 | CollectLiveTeammatesForSessionSwitch |
| RestartForSessionSwitch: cleanupFirst=true 先清理 | RestartForSessionSwitch |
| RestartForSessionSwitch: markTeammate 失败跳过 | RestartForSessionSwitch |
| PersistAllocatorState: nil allocator 跳过 | PersistAllocatorState |
| PersistAllocatorState: 异常时日志记录 | PersistAllocatorState |
