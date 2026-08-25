# 48h 逻辑审查报告（2026-08-22）

> 审查范围：近 48 小时内提交的代码变更，对照 Python 参考实现进行逻辑一致性检查

## 审查覆盖章节

| 提交范围 | 对应章节 | 状态 |
|----------|---------|------|
| 9.65a-4 TeamBackend 门面 | 9.65a-4 | ✅ 已完成 |
| 9.51-9.53 Harness 资源/Schema/Prompts | 9.51-53 | ✅ 已完成 |
| 9.64 Team Memory (ListFragmentMemories 排序) | 9.64 | ✅ 已完成 |
| SkillToolkit Score 类型回填 | 9.38-49 | ✅ 已完成 |

---

## 🔴 严重问题（功能逻辑偏差）

### S-01: ForceCleanTeam 多余调用 onTeamCleaned 回调

**文件**: `internal/agent_teams/tools/team_backend.go:649-653`

**Python 样例**:
```python
# openjiuwen/agent_teams/tools/team.py:729-761
async def force_clean_team(self, shutdown_members: bool = True) -> bool:
    if shutdown_members:
        members = await self.db.member.get_team_members(self.team_name)
        for member_data in members:
            if member_data.member_name == self.member_name:
                continue
            try:
                await self.shutdown_member(member_data.member_name, force=True)
            except Exception as e:
                team_logger.warning(...)

    success = await self.db.force_delete_team_session(self.team_name)
    try:
        await self._remove_cleanup_paths()
    except Exception as e:
        team_logger.error(...)
        success = False
    # 注意：这里没有调用 self._on_team_cleaned
    if success:
        team_logger.info(f"Team {self.team_name} force cleaned successfully")
    return success
```

**Go 问题**:
```go
// team_backend.go:649-653
if tb.onTeamCleaned != nil {
    if err := tb.onTeamCleaned(ctx); err != nil {
        logger.Warn(tbLogComponent).Err(err).Msg("ForceCleanTeam: onTeamCleaned 回调失败")
    }
}
```

**影响**: Python 的 `force_clean_team` 故意不调用 `_on_team_cleaned`。该回调在 `clean_team()` 中触发用于通知 TeamAgent "团队正常完成"（leader 可确定性地结束轮次），而 `force_clean_team` 是强制清理（会话切换场景），不应触发"正常完成"回调。Go 的多余调用可能导致 leader 的 StreamController 在非预期时机结束轮次。

**修复方案**: 删除 `ForceCleanTeam` 中的 `onTeamCleaned` 回调调用，对齐 Python 行为。

**流程示例**:
```
Python: force_clean_team → shutdown_members → force_delete_team_session → _remove_cleanup_paths → 返回 success
Go 当前: force_clean_team → shutdown_members → force_delete_team_session → onTeamCleaned❌ → _remove_cleanup_paths → 返回
Go 修正: force_clean_team → shutdown_members → force_delete_team_session → _remove_cleanup_paths → 返回
```

---

### S-02: ShutdownMember 缺少 force 参数和 FSM 校验

**文件**: `internal/agent_teams/tools/team_backend.go:418`

**Python 样例**:
```python
# openjiuwen/agent_teams/tools/team.py:514
async def shutdown_member(self, member_name: str, force: bool = False) -> MemberOpResult:
    # ...
    current_status = MemberStatus(member_data.status)
    # 幂等返回
    if current_status == MemberStatus.SHUTDOWN or current_status == MemberStatus.SHUTDOWN_REQUESTED:
        return MemberOpResult.success()
    # FSM 校验
    if not is_valid_transition(current_status, MemberStatus.SHUTDOWN_REQUESTED, MEMBER_TRANSITIONS):
        return MemberOpResult.fail(f"Member {member_name} cannot shut down from status '{current_status.value}'")
    # update_member_status（非 CAS）
    success = await self.db.member.update_member_status(
        member_name, self.team_name, MemberStatus.SHUTDOWN_REQUESTED.value
    )
    # ...
    # 事件中带 force 字段
    MemberShutdownEvent(team_name=self.team_name, member_name=member_name, force=force)
```

**Go 问题**:
```go
// team_backend.go:418
func (tb *TeamBackend) ShutdownMember(ctx context.Context, memberName string) atschema.MemberOpResult {
    // 没有 force 参数
    // ...
    // 使用 TryTransitionMemberStatus (CAS) 而非 update_member_status
    ok := tb.db.Member().TryTransitionMemberStatus(ctx, memberName, tb.teamName,
        member.Status, string(atschema.MemberStatusShutdownRequested))
    // ...
    // 事件中 Force 硬编码为 false
    tb.publishEvent(ctx, atschema.MemberShutdownEvent{
        BaseEventMessage: ..., Force: false,
    })
}
```

**影响**:
1. **缺少 `force` 参数**: `ForceCleanTeam` 调用 `ShutdownMember` 时无法传递 `force=True`，导致 `MemberShutdownEvent.Force` 永远为 false。Python 的 force=True 用于绕过正常关闭序列。
2. **缺少 FSM 校验**: Python 使用 `is_valid_transition` 检查当前状态是否可转换为 `SHUTDOWN_REQUESTED`，Go 直接 CAS 但不验证转换合法性，可能允许无效状态转换（如从 `UNSTARTED` 直接关闭）。
3. **CAS vs update_member_status**: Go 用 `TryTransitionMemberStatus`（需指定 from 状态），Python 用 `update_member_status`（直接设目标值）。CAS 更安全但不完全对齐 Python 语义。

**修复方案**:
1. `ShutdownMember` 增加 `force bool` 参数
2. 在 CAS 前增加 FSM 校验：检查 `is_valid_transition(currentStatus, SHUTDOWN_REQUESTED)`，无效时返回 fail
3. `MemberShutdownEvent` 中传递 `force` 值
4. `ForceCleanTeam` 调用时传递 `force=true`

---

### S-03: CleanTeam 检查 ERROR 状态但 Python 不检查

**文件**: `internal/agent_teams/tools/team_backend.go:599-600`

**Python 样例**:
```python
# openjiuwen/agent_teams/tools/team.py:680-688
for member_data in members:
    if member_data.member_name == self.member_name:
        continue
    if member_data.status != MemberStatus.SHUTDOWN.value:
        team_logger.info(f"Member {member_name} is not shutdown (status: {member_data.status})")
        all_shutdown = False
        break
```

**Go 问题**:
```go
// team_backend.go:599-600
if m.Status != string(atschema.MemberStatusShutdown) &&
    m.Status != string(atschema.MemberStatusError) {
```

**影响**: Go 额外允许 `ERROR` 状态通过清理检查，而 Python 只允许 `SHUTDOWN`。这意味着 Go 会在成员处于 ERROR 状态时也执行清理，Python 则不会。这可能导致在成员异常状态下提前删除团队数据。

**修复方案**: 移除对 `MemberStatusError` 的检查，只允许 `SHUTDOWN` 状态通过，对齐 Python 行为。如果确实需要允许 ERROR 状态清理，应在 Python 侧同步修改而非单方面偏离。

---

### S-04: Startup 方法错误跳过自身成员

**文件**: `internal/agent_teams/tools/team_backend.go:362-364`

**Python 样例**:
```python
# openjiuwen/agent_teams/tools/team.py:354-358
unstarted = await self.db.member.get_team_members(self.team_name, status=MemberStatus.UNSTARTED)
started: list[str] = []
for member in unstarted:
    await self.startup_member(member.member_name, on_created)
    started.append(member.member_name)
return started
```

**Go 问题**:
```go
// team_backend.go:362-364
for _, m := range members {
    if m.MemberName == tb.memberName {
        continue // 跳过自身
    }
    ok, err := tb.StartupMember(ctx, m.MemberName, onCreated)
```

**影响**: Python 的 `startup()` 遍历所有 UNSTARTED 成员**包括自身**（如果 leader 恰好是 UNSTARTED），而 Go 跳过了自身。虽然在正常流程中 leader 已是 BUSY 不会出现在 UNSTARTED 列表中，但这是一个语义偏差 — 当 leader 的 `startup_member` 被调用时，Python 会执行 CAS（因为 leader 已是 BUSY，CAS 会返回 False），而 Go 直接跳过。对于 HUMAN_AGENT 等特殊角色如果以 UNSTARTED 注册且恰好等于当前成员名，Go 会跳过启动。

**修复方案**: 移除 `if m.MemberName == tb.memberName { continue }`，让 CAS 逻辑自然处理（CAS 失败返回 false 不启动即可），对齐 Python 行为。

---

### S-05: member.go TeamMember 四个核心方法全部为 TODO 桩

**文件**: `internal/agent_teams/agent/member.go:49-82`

**Python 样例**:
```python
# openjiuwen/agent_teams/agent/member.py:75-194
async def status(self) -> MemberStatus:
    member_data = await self.db.member.get_member(self.member_name, self.team_name)
    return MemberStatus(member_data.status) if member_data else None

async def update_status(self, new_status: MemberStatus) -> bool:
    old_status = await self.status()
    if old_status is None: return False
    if old_status == new_status: return True  # 幂等短路
    success = await self.db.member.update_member_status(...)
    # 发布 MemberStatusChangedEvent ...
    return True

async def update_execution_status(self, new_status: ExecutionStatus) -> bool:
    # 类似 update_status，发布 MemberExecutionChangedEvent
```

**Go 问题**:
```go
// member.go:49-82
func (m *TeamMember) Status(ctx context.Context) (atschema.MemberStatus, error) {
    // TODO(#9.65): 从 DB 读取成员状态
    return atschema.MemberStatusReady, nil  // 硬编码返回
}
func (m *TeamMember) UpdateStatus(ctx context.Context, newStatus atschema.MemberStatus) (bool, error) {
    // TODO(#9.65): 读取旧状态 → 短路等值 → 写 DB → 发 MemberStatusChangedEvent
    return true, nil  // 硬编码返回
}
```

**影响**: `Status()` 始终返回 `READY` 而非从 DB 读取，`UpdateStatus`/`UpdateExecutionStatus` 不写 DB 也不发事件。这四个方法是成员状态管理的核心，Python 中每个成员进程通过这些方法同步自己的状态到 DB 并广播变更事件。Go 的桩实现意味着：
- 成员状态转换完全静默，DB 不会更新
- 其他成员/leader 永远看不到状态变更
- `MemberStatusChangedEvent`/`MemberExecutionChangedEvent` 永远不会发布
- 下游依赖这些事件的逻辑全部失效

**修复方案**: 实现 DB 读写 + 事件发布，完整对齐 Python：
1. `Status()`: 从 `m.DB.Member().GetMember()` 读取
2. `UpdateStatus()`: 读取旧状态 → 等值短路 → `update_member_status` → 发布 `MemberStatusChangedEvent`
3. `UpdateExecutionStatus()`: 同上 → `update_member_execution_status` → 发布 `MemberExecutionChangedEvent`

---

### S-06: is_team_completed 检查条件与 Python 不一致

**文件**: `internal/agent_teams/tools/team_backend.go:241-277`

**Python 样例**:
```python
# openjiuwen/agent_teams/tools/team.py:791-828
async def is_team_completed(self) -> Optional[TeamCompletionSnapshot]:
    tasks = await self.task_manager.list_tasks()
    if not tasks:           # 条件1: 无任务时返回 None
        return None
    if any(task.status not in TASK_TERMINAL_STATUSES for task in tasks):
        return None
    members = await self.db.member.get_team_members(self.team_name)
    if not members:         # 条件2: 无成员时返回 None
        return None
    if any(member.status not in MEMBER_SETTLED_STATUSES for member in members):
        return None
    if await self.message_manager.has_unread_messages(include_broadcast=True):
        return None
    return TeamCompletionSnapshot(member_count=len(members), task_count=len(tasks))
```

**Go 问题**:
```go
// team_backend.go:241-277
func (tb *TeamBackend) IsTeamCompleted(ctx context.Context) (*atschema.TeamCompletionSnapshot, error) {
    team, err := tb.db.Team().GetTeam(ctx, tb.teamName)
    if err != nil || team == nil {
        return nil, nil  // 额外检查: 团队是否存在（Python 无此检查）
    }
    members, err := tb.db.Member().GetTeamMembers(ctx, tb.teamName, "")
    // ...
    tasks, err := tb.db.Task().GetTeamTasks(ctx, tb.teamName, "")
    // ...
    // 缺少: if len(tasks) == 0 { return nil, nil }  —— Python 无任务返回 None
    for _, t := range tasks {
        if !fsm.IsTaskTerminal(t.Status) { return nil, nil }
    }
    for _, m := range members {
        if !atschema.MemberSettledStatuses[atschema.MemberStatus(m.Status)] { return nil, nil }
    }
    if tb.messageManager.HasUnreadMessages(ctx, true) {
        return nil, nil
    }
```

**影响**:
1. **缺少"无任务返回 None"检查**: Python 在 `tasks` 为空列表时返回 None（表示未完成），Go 跳过此检查，无任务时直接走到成员检查并可能返回 `TeamCompletionSnapshot{TaskCount: 0}`，表示"完成"。这改变了团队完成的语义。
2. **额外检查团队是否存在**: Python 不检查 `get_team` 是否返回 None（因为 `is_team_completed` 只在团队已创建后调用），Go 多了一个检查，逻辑上多余但无害。

**修复方案**: 在任务循环前增加 `if len(tasks) == 0 { return nil, nil }`，对齐 Python 语义。

---

## 🟡 一般问题

### M-01: SkillNet score 默认值缺失（nil vs 0）

**文件**: `internal/swarm/agents/harness/tools/skill_toolkit.go` `_normalizeSearchItem` 函数

**Python 样例**:
```python
# skill_toolkit.py:119
score = item.get("stars", 0)  # 缺少 stars 键时默认为 0
```

**Go 问题**: `toIntPtr(item["stars"])` 当 `stars` 键不存在时返回 `nil`，而 Python 返回 `0`。

**影响**: 序列化后 Go 输出 `"score": null`，Python 输出 `"score": 0`。对于 SkillNet 结果中确实没有 `stars` 字段的情况，LLM 看到的值不同。

**修复方案**:
```go
score := toIntPtr(item["stars"])
if score == nil {
    zero := 0
    score = &zero  // 对齐 Python: item.get("stars", 0)
}
```

---

### M-02: cancel_member 返回类型不一致

**文件**: `internal/agent_teams/tools/team_backend.go:459`

**Python 样例**:
```python
async def cancel_member(self, member_name: str) -> bool:
```

**Go 问题**:
```go
func (tb *TeamBackend) CancelMember(ctx context.Context, memberName string) atschema.MemberOpResult {
```

**影响**: Python 返回 `bool`，Go 返回 `MemberOpResult`。虽然 `MemberOpResult` 提供更多信息（含 reason），但类型签名不匹配。调用方需要适配。这在 Python 侧 `cancel_member` 的调用者使用 `if await team.cancel_member(...):` 模式时，Go 侧需要 `if result.OK` 判断。

**修复方案**: 此为设计选择，Go 的 `MemberOpResult` 更优。建议保留但标记为已知差异，确保所有调用方使用 `result.OK` 判断。

---

### M-03: TodoItem/TaskPlan ToDict 键省略行为

**文件**: `internal/agentcore/harness/schema/task.go`

**Python 样例**:
```python
# Pydantic model_dump(mode="python") 始终输出所有键
{"id": "1", "content": "xxx", "depends_on": None, "result_summary": None, ...}
```

**Go 问题**: Go 使用 `omitempty` JSON tag，空值字段被省略：
```go
DependsOn []string `json:"depends_on,omitempty"`
ResultSummary string `json:"result_summary,omitempty"`
```

**影响**: 如果有跨语言序列化/反序列化场景（Go 写入 → Python 读取），Python 端可能因缺少键而报错或使用默认值。但在当前架构中这些 dict 主要供 LLM 消费，影响较小。

**修复方案**: 在 `ToDict()` 方法中显式输出空值键（空列表 `[]string{}`、空字符串 `""`），对齐 Python 的 `model_dump` 行为。

---

### M-04: TaskPlan ToMarkdown 格式差异

**文件**: `internal/agentcore/harness/schema/task.go`

**Python 样例**:
```python
## Goal: {goal}
√ Task 1 (completed)
> Task 2 (in progress)
× Task 3 (cancelled)
Task 4 (pending) result_summary_text
```

**Go 问题**:
```markdown
# Task Plan

**Goal:** {goal}
[√] Task 1
[→] Task 2
[×] Task 3
[ ] Task 4
```

**影响**: 标题级别（`##` vs `#`）、Goal 格式（`## Goal:` vs `**Goal:**`）、状态符号（`√/>/×` vs `[√]/[→]/[×]`）、缺少 `result_summary` 后缀。如果 Markdown 仅用于日志展示影响小；若 LLM 解析该输出可能影响行为。

**修复方案**: 对齐 Python 格式：使用 `## Goal:` 标题、`√/>/× ` 前缀、追加 `result_summary`。

---

### M-05: TaskPlan mark_* 错误处理差异

**文件**: `internal/agentcore/harness/schema/task.go`

**Python 样例**:
```python
def mark_in_progress(self, task_id: str):
    task = self.get_task(task_id)
    if task is not None:  # 静默忽略不存在的 task
        task.status = TodoStatus.IN_PROGRESS
```

**Go 问题**:
```go
func (tp *TaskPlan) MarkInProgress(taskID string) error {
    task := tp.GetTask(taskID)
    if task == nil {
        return fmt.Errorf("task not found: %s", taskID)  // 返回 error
    }
```

**影响**: Python 静默忽略不存在的 task_id，Go 返回 error。如果调用方依赖 Python 的静默行为，Go 会返回错误中断流程。

**修复方案**: 对齐 Python 静默忽略，将 `return fmt.Errorf(...)` 改为 `return nil`。

---

### M-06: ShutdownMember 中 SendMessage 返回值未检查

**文件**: `internal/agent_teams/tools/team_backend.go:436`

**Python 样例**:
```python
msg_id = await self.message_manager.send_message(
    content=t("team.shutdown_request_content"),
    to_member_name=member_name,
)
if not msg_id:
    team_logger.warning(f"Failed to send shutdown request message to member {member_name}")
```

**Go 问题**:
```go
_, _ = tb.messageManager.SendMessage(ctx, atschema.T("team.shutdown_request_content"), memberName, tb.memberName)
```

**影响**: `_, _` 丢弃返回值，发送失败时无任何日志记录。Python 会记录 warning。当消息发送失败时，leader 不知道 shutdown 通知没有送达成员。

**修复方案**: 检查返回值并记录 warning 日志：
```go
msgID, err := tb.messageManager.SendMessage(ctx, atschema.T("team.shutdown_request_content"), memberName, tb.memberName)
if err != nil || msgID == "" {
    logger.Warn(tbLogComponent).Str("member_name", memberName).Err(err).Msg("ShutdownMember: 发送 shutdown 消息失败")
}
```

---

### M-07: CancelMember 消息发送失败时返回值不一致

**文件**: `internal/agent_teams/tools/team_backend.go:480`

**Python 样例**:
```python
# openjiuwen/agent_teams/tools/team.py:645-650
success = await self.message_manager.send_message(
    content=t("team.cancel_request_content"), to_member_name=member_name
)
if not success:
    team_logger.error(f"Failed to send cancel request message to member {member_name}")
    return False  # 消息发送失败 → 取消失败
```

**Go 问题**:
```go
// team_backend.go:480
_, _ = tb.messageManager.SendMessage(ctx, atschema.T("team.cancel_request_content"), memberName, tb.memberName)
// 后续直接返回 success
return atschema.NewMemberOpResultSuccess()
```

**影响**: Python 在 `send_message` 失败时返回 `False`（取消失败），Go 忽略发送结果始终返回 success。当消息发送失败时，成员不知道需要取消执行，但 leader 认为取消成功。

**修复方案**: 检查 SendMessage 返回值，失败时返回 fail：
```go
msgID, err := tb.messageManager.SendMessage(ctx, atschema.T("team.cancel_request_content"), memberName, tb.memberName)
if err != nil || msgID == "" {
    logger.Error(tbLogComponent).Str("member_name", memberName).Err(err).Msg("CancelMember: 发送取消消息失败")
    return atschema.NewMemberOpResultFail("failed to send cancel message to " + memberName)
}
```

---

### M-08: ShutdownMember 多出 CancelAllTasks 调用（Python 无此步骤）

**文件**: `internal/agent_teams/tools/team_backend.go:438`

**Python 样例**:
```python
# openjiuwen/agent_teams/tools/team.py:514-598
# shutdown_member 中只做: 状态变更 + 发消息 + 发事件
# 没有取消任务的逻辑
```

**Go 问题**:
```go
// team_backend.go:438
// 步骤 5: 取消该成员的任务
_, _ = tb.taskManager.CancelAllTasks(ctx, []string{memberName})
```

**影响**: Go 的 `ShutdownMember` 额外调用了 `CancelAllTasks`，这在 Python 中不存在。Python 的 `shutdown_member` 只做状态变更 + 消息通知 + 事件发布，不主动取消任务。任务取消由成员进程自行处理（收到 shutdown 事件后清理）。Go 的额外取消可能导致任务状态不一致。

**修复方案**: 移除 `ShutdownMember` 中的 `CancelAllTasks` 调用，对齐 Python 行为。如果确实需要取消，应在下游事件处理器中完成，而非在 TeamBackend 门面层。

---

### M-09: ForceCleanTeam 永远返回 true 且忽略 ForceDeleteTeamSession 返回值

**文件**: `internal/agent_teams/tools/team_backend.go:648-658`

**Python 样例**:
```python
# openjiuwen/agent_teams/tools/team.py:751-761
success = await self.db.force_delete_team_session(self.team_name)
try:
    await self._remove_cleanup_paths()
except Exception as e:
    team_logger.error(...)
    success = False  # 清理失败会修改返回值
if success:
    team_logger.info(...)
return success  # 可能返回 False
```

**Go 问题**:
```go
// team_backend.go:648-658
tb.db.ForceDeleteTeamSession(ctx, tb.teamName) // 返回值被忽略
// ...
tb.RemoveCleanupPaths(ctx)  // 失败不影响返回值
return true, nil  // 永远返回 true
```

**影响**: Python 的 `force_clean_team` 返回 `force_delete_team_session` 的结果（可能为 False），且清理路径失败时也会置 False。Go 永远返回 `true`，调用方无法判断清理是否真正成功。

**修复方案**: 检查 `ForceDeleteTeamSession` 返回值，清理路径失败时影响返回值：
```go
success := tb.db.ForceDeleteTeamSession(ctx, tb.teamName)
// RemoveCleanupPaths 失败不影响返回值（对齐 Python 中 exception 被捕获后 success=False）
// 但 Go 的 RemoveCleanupPaths 内部已记录日志，此处只需关注 ForceDeleteTeamSession
if !success {
    return false, nil
}
return true, nil
```

---

### M-10: ApprovePlan 简化过度，缺少 Python 的校验逻辑

**文件**: `internal/agent_teams/tools/team_backend.go:711-729`

**Python 样例**:
```python
# openjiuwen/agent_teams/tools/team.py:398-462
async def approve_plan(self, plan_id: str, approved: bool = True, feedback: Optional[str] = None) -> bool:
    if not plan_id: return False                           # 1. plan_id 非空校验
    plan_record = self.task_manager.get_plan_record(plan_id)  # 2. 查 plan_record
    if not plan_record: return False                       # 3. plan_record 存在校验
    member_name = str(plan_record.get("member_name") or "")
    task_id = str(plan_record.get("task_id") or "")
    if not member_name: return False                       # 4. member_name 非空校验
    member_data = await self.db.member.get_member(member_name, self.team_name)
    if member_data is None: return False                   # 5. member 存在校验
    result = await self.task_manager.approve_plan(
        plan_id=plan_id, approved=approved, feedback=feedback or "", leader_name=self.member_name
    )
    if not result.ok: return False
    # 发布事件
```

**Go 问题**:
```go
// team_backend.go:711-729
func (tb *TeamBackend) ApprovePlan(ctx context.Context, taskID string) atschema.MemberOpResult {
    err := tb.taskManager.ApprovePlan(ctx, taskID, true, "")  // 用 taskID 作为 planID，approved 硬编码 true
    if err != nil { return fail }
    task, _ := tb.taskManager.Get(ctx, taskID)
    // 无 plan_record 查询
    // 无 member 存在校验
    // 无 approved/feedback 参数
    // 无 leader_name 传递
```

**影响**:
1. Go 用 `taskID` 替代 `planID`（Python 区分两者）
2. `approved` 硬编码为 `true`，无法拒绝计划
3. 缺少 `feedback` 参数
4. 缺少前置校验（plan_record、member 存在性）
5. 缺少 `leader_name` 传递

**修复方案**: 增加 `approved` 和 `feedback` 参数，增加前置校验步骤，对齐 Python 签名：
```go
func (tb *TeamBackend) ApprovePlan(ctx context.Context, planID string, approved bool, feedback string) atschema.MemberOpResult {
    if planID == "" { return fail("plan_id required") }
    // 查 plan_record → 校验 member → approve_plan → 发事件
}
```

---

## 🟢 提示问题

### L-01: ForceCleanTeam 事件发布缺失

**文件**: `internal/agent_teams/tools/team_backend.go:628-659`

**Python 样例**:
```python
# Python force_clean_team 也不发布 TeamCleanedEvent，与 Go 一致
```

**Go 问题**: `ForceCleanTeam` 没有发布 `TeamCleanedEvent`，这与 Python 一致（Python 也不发布）。但 `CleanTeam` 会发布事件。这是正确行为，仅做记录。

---

### L-02: register_cleanup_path 路径展开方式差异

**文件**: `internal/agent_teams/tools/team_backend.go:855`

**Python 样例**:
```python
self._cleanup_paths.add(str(Path(path).expanduser()))
```

**Go 问题**:
```go
expanded := filepath.Clean(os.ExpandEnv(path))
```

**影响**: Python 用 `Path.expanduser()` 展开 `~`，Go 用 `os.ExpandEnv` 展开 `$HOME` 等。两者语义不同：`~/foo` 在 Go 中不会展开（需 `$HOME/foo`），而 Python 会。这在团队工作空间路径使用 `~` 前缀时会导致清理路径无效。

**修复方案**: 增加 `~/` 展开逻辑：
```go
if strings.HasPrefix(path, "~/") {
    home, _ := os.UserHomeDir()
    path = filepath.Join(home, path[2:])
}
```

---

### L-03: ListFragmentMemories 缺少 mem_type 校验

**文件**: `internal/agentcore/memory/manage/index/fragment_manager.go`

**Python 样例**:
```python
# list_fragment_memories
if mem_type.value not in FRAGMENT_MEMORY_TYPE:
    logger.error(...)
    return []
```

**Go 问题**: Go 不校验 `memType` 是否在合法的 `FragmentMemoryTypes` 集合中，无效类型直接透传给索引查询，返回空结果无日志。

**影响**: 输入无效类型时 Python 有错误日志提醒开发者，Go 静默。属于防御性日志差异。

**修复方案**: 增加类型校验和 Warn 日志。

---

### L-04: installSkill name 回退逻辑缺少 SkillNet 分支

**文件**: `internal/swarm/agents/harness/tools/skill_toolkit.go:393-396`

**Python 样例**:
```python
name = str(skill.get("name", "")).strip()
if not name:
    name = Path(target).name if resolved_source == "skillnet" else target
```

**Go 问题**:
```go
name := strings.TrimSpace(toString(skill["name"]))
if name == "" {
    name = identifier  // 所有来源统一使用 identifier
}
```

**影响**: SkillNet 安装时如果 skill 缺少 name 字段，Python 从 URL 路径提取文件名作为回退，Go 使用 `identifier`。通常 `identifier` 已包含正确名称，实际影响小。

**修复方案**: 可忽略，或增加 SkillNet 分支对齐。

---

### L-05: ListFragmentMemories 零值时间戳排序差异

**文件**: `internal/agentcore/memory/manage/index/fragment_manager.go:301-308`

**Python 样例**:
```python
result.sort(key=lambda x: (x['mem'], str(x.get('timestamp') or '')), reverse=True)
# None/缺失 timestamp → str('') → 空字符串 → 在 reverse=True 时排到末尾
```

**Go 问题**: `time.Time` 零值格式化为 `"0001-01-01T00:00:00Z"`，不是空字符串。在 reverse=True 排序中，零值时间戳排到中间位置（而非末尾）。

**影响**: 实际中 `ListFragmentMemories` 返回的记录都有时间戳，零值情况罕见。属于理论差异。

**修复方案**: 可忽略。如需严格对齐，对零值 `time.Time` 返回空字符串。

---

### L-06: ModelUsageRecord.String() 格式差异

**文件**: `internal/agentcore/harness/schema/task.go`

**Python 样例**: `model_id={model_id} input={input} output={output}`
**Go 问题**: `{model_id}: input={input}, output={output}`

**影响**: 纯展示格式差异，不影响功能。

---

## ⤵️ 待回填占位代码汇总

| 文件 | 行号 | 占位内容 | 对应章节 |
|------|------|---------|---------|
| `agent/member.go` | 50-51 | `Status()` 硬编码返回 READY | 9.65 |
| `agent/member.go` | 56-58 | `ExecutionStatus()` 硬编码返回 IDLE | 9.65 |
| `agent/member.go` | 66-70 | `UpdateStatus()` 不写 DB 不发事件 | 9.65 |
| `agent/member.go` | 77-81 | `UpdateExecutionStatus()` 不写 DB 不发事件 | 9.65 |
| `agent/agent_configurator.go` | 175-176 | 语言解析 TODO(#9.53) | 9.53 |
| `agent/agent_configurator.go` | 193-194 | Messager 创建 TODO(#9.65) | 9.65 |
| `agent/agent_configurator.go` | 199-200 | WorkspaceManager TODO(#9.66) | 9.66 |
| `agent/agent_configurator.go` | 203-204 | 模型分配器 TODO(#9.64) | 9.64 |
| `agent/agent_configurator.go` | 207-208 | 团队后端 TODO(#9.58) | 9.58 |
| `agent/agent_configurator.go` | 220-223 | workspace 路径解析 TODO(#9.66) | 9.66 |
| `agent/agent_configurator.go` | 236-256 | Rails 构造全部 nil TODO(#9.68) | 9.68 |
| `agent/agent_configurator.go` | 260 | 记忆管理器 TODO(#9.64) | 9.64 |
| `runtime/manager.go` | 143-148 | Activate 桩实现 | 9.62 |
| `runtime/manager.go` | 152-156 | Finalize 桩实现 | 9.62 |
| `runtime/manager.go` | 160-164 | Pause 桩实现 | 9.62 |
| `runtime/manager.go` | 167-172 | StopTeam 桩实现 | 9.62 |
| `runtime/manager.go` | 175-180 | DeleteTeam 桩实现 | 9.62 |
| `runtime/manager.go` | 233-236 | handleInteractiveInput 桩实现 | 9.55 |
| `agent/team_agent.go` | 704 | AutoStartMember 返回 false | 9.58 |
| `agent/team_agent.go` | 711 | AutoStartAll 返回 nil | 9.58 |
| `agent/team_agent.go` | 745 | FromSpawnPayload 返回 nil | 9.57 |
| `agent/team_agent.go` | 775 | RecoverTeam 返回 nil | 9.61 |
| `agent/team_agent.go` | 782 | RecoverFromSession 返回 nil | 9.61 |
| `agent/team_agent.go` | — | Interact/Broadcast/HumanAgentSay 全部 stub | 9.55 |
| `agent/team_agent.go` | — | DestroyTeam/StartCoordination/PauseCoordination/StopCoordination 全部 stub | 9.62 |
| `runtime/manager.go` | 143 | `agent any` 参数应为 `*agent.TeamAgent` | 9.62 |
| `runtime/manager.go` | 184 | `callback any` 参数应为 `tools.OnInbound` | 9.55 |

> **说明**: `agent/member.go` 的 4 个 TODO 是最紧急的回填项，它们是成员状态管理的核心，当前全部为硬编码桩。`agent/team_agent.go` 的大量 stub 方法待 9.55-9.62 章节回填。

---

## 问题统计

| 严重程度 | 数量 | 问题编号 |
|---------|------|---------|
| 🔴 严重 | 6 | S-01 ~ S-06 |
| 🟡 一般 | 10 | M-01 ~ M-10 |
| 🟢 提示 | 6 | L-01 ~ L-06 |
| **合计** | **22** | |

---

## 修复优先级建议

1. **立即修复（P0）**: S-01（ForceCleanTeam 多余回调）、S-02（ShutdownMember 缺 force/FSM）、S-05（member.go 四个核心方法桩）
2. **尽快修复（P1）**: S-03（CleanTeam ERROR 检查）、S-04（Startup 跳过自身）、S-06（is_team_completed 无任务检查）、M-01（SkillNet score 默认值）、M-07（CancelMember 返回值）、M-08（ShutdownMember 多余 CancelAllTasks）
3. **排期修复（P2）**: M-03/M-04/M-05（schema 序列化/格式差异）、M-06（SendMessage 返回值日志）、M-09（ForceCleanTeam 返回值）、M-10（ApprovePlan 简化过度）
4. **酌情修复（P3）**: L-02~L-05（防御性差异）
