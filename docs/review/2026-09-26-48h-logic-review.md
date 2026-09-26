# 48h 逻辑审查报告 — 2026-09-26

> 审查范围：48小时内（2026-09-24 ~ 2026-09-26）提交的代码变更
>
> 主要章节：9.62 CoordinationKernel / 9.63 EventBus+Dispatcher+Handlers / 7.11-7.12 GraphMemory修复 / 9.82 P6+P7 ContextEvolutionRail+MilvusConnector / 10.6.10 StreamEventRail / Evolution Rails 修复(S1-S9,M1-M14) / 9.80a ExperienceSharing / 9.55-9.58 TeamAgent/AgentConfigurator/TeamBackend

---

## 问题汇总

| 级别 | 数量 |
|------|------|
| 严重 | 32 |
| 一般 | 37 |
| 提示 | 19 |
| **合计** | **88** |

---

## 严重问题 (S)

### S-01: CoordinationKernel.start() 丢失大量基础设施初始化步骤

**章节**: 9.62 CoordinationKernel
**文件**: `internal/agent_teams/agent/coordination/kernel.go:130-159`

Python `CoordinationKernel.start()` 包含约 60 行关键初始化步骤，Go 版本仅保留了事件总线的启停，缺失以下步骤：

| 步骤 | Python | Go |
|------|--------|-----|
| set_member_id 日志上下文 | ✅ L117-119 | ❌ |
| session 类型校验 | ✅ L120-125 | ❌ |
| team_backend.db.initialize() | ✅ L126-127 | ❌ |
| session_manager.bind_session/release | ✅ L128-131 | ❌ |
| Leader: 已有团队 all-SHUTDOWN 检测 + clean_team | ✅ L133-145 | ❌ |
| Leader: recover_team() | ✅ L145 | ❌ |
| workspace_manager.initialize | ✅ L147-151 | ❌ |
| memory_manager.init_toolkit + register + inject | ✅ L156-167 | ❌ |
| update_status(MemberStatus.READY) | ✅ L169 | ❌ |
| messager.subscribe_transport | ✅ L174-177 | ❌ |

**Python 样例**:
```python
async def start(self, session: Any = None) -> None:
    set_member_id(member_name)
    if infra.team_backend:
        await infra.team_backend.db.initialize()
    if session is not None:
        await sess_mgr.bind_session(session)
    else:
        sess_mgr.release_session()
    if host.role == TeamRole.LEADER and infra.team_backend:
        existing = await infra.team_backend.db.team.get_team(infra.team_backend.team_name)
        if existing is not None:
            non_leader_members = await infra.team_backend.list_members()
            if non_leader_members and all(m.status == MemberStatus.SHUTDOWN.value for m in non_leader_members):
                await infra.team_backend.clean_team()
            else:
                await host.recover_team()
    # ... workspace, memory, status, transport ...
```

**Go 问题**:
```go
func (k *CoordinationKernel) Start(ctx context.Context) {
    if k.eventBus == nil { return }
    if !k.eventBus.IsRunning() {
        k.eventBus.Start(ctx, func(ctx context.Context, event types.CoordinationEvent) {
            k.dispatcher.Dispatch(ctx, event)
        })
    }
    k.lifecycleState = kernelStateRunning
}
```

**修复方案**: 在 `Start()` 中增加缺失步骤的调用。扩展 `KernelHost` 接口以暴露所需方法，或在 `Start()` 前要求调用者完成初始化。

---

### S-02: CoordinationKernel.pause() 丢失核心暂停逻辑

**章节**: 9.62 CoordinationKernel
**文件**: `internal/agent_teams/agent/coordination/kernel.go:166-183`

Python `pause()` 包含 12 个步骤，Go 仅实现了 2 个（停止 EventBus + 设置状态），缺失 drain_agent_task、persist_allocator_state、mark_live_teammates、cancel_recovery_tasks、shutdown_all_handles、persist_team_lifecycle、publish TeamStandbyEvent、unsubscribe_transport、close_stream、release_session。

**Python 样例**:
```python
async def pause(self) -> None:
    if self._lifecycle_state != "running": return
    await self.drain_agent_task()
    host.persist_allocator_state()
    if host.role == TeamRole.LEADER:
        await self._mark_live_teammates(MemberStatus.PAUSED)
        await host.spawn_manager.cancel_recovery_tasks()
        await host.spawn_manager.shutdown_all_handles()
        self._persist_team_lifecycle("paused")
    # ... TeamStandbyEvent, unsubscribe, bus.stop, close_stream, release_session ...
    self._lifecycle_state = "paused"
```

**修复方案**: 扩展 `KernelHost` 接口添加缺失方法，实现完整的暂停流程。

---

### S-03: CoordinationKernel.stop() 丢失核心停止逻辑

**章节**: 9.62 CoordinationKernel
**文件**: `internal/agent_teams/agent/coordination/kernel.go:191-209`

与 S-02 同理，Python `stop()` 包含 drain、persist_allocator_state、mark_live_teammates(STOPPED)、unsubscribe_transport、cancel_recovery_tasks、shutdown_all_handles、memory_manager.close、close_stream、release_session 等步骤，Go 仅实现 2 步。

**修复方案**: 同 S-02，扩展接口并实现完整流程。

---

### S-04: CoordinationKernel 缺失 subscribe_transport / unsubscribe_transport / finalize_round 等方法

**章节**: 9.62 CoordinationKernel
**文件**: `internal/agent_teams/agent/coordination/kernel.go`

Python CoordinationKernel 有以下 Go 缺失的方法：`subscribe_transport`、`unsubscribe_transport`、`finalize_round`、`enqueue_mailbox_after_first_iteration`、`drain_agent_task`、`close_stream`、`_mark_live_teammates`、`_persist_team_lifecycle`。

**Python 样例**:
```python
async def finalize_round(self) -> None:
    memory_manager = host.resources.memory_manager
    if memory_manager: await memory_manager.extract_after_round()
    host.stream_controller.stream_queue = None

async def subscribe_transport(self, team_name: str) -> None:
    # ... register DM handler, subscribe to TeamTopics ...
```

**修复方案**: 在 Go CoordinationKernel 中补充以上方法。

---

### S-05: MilvusConnectorImpl 所有方法硬编码 context.Background() 丢弃上游 context

**章节**: 9.82 P7
**文件**: `internal/agentcore/context_evolver/core/persistence/milvus_connector.go:145,263,306,330,382,456,481,515`

`SaveToDB`/`LoadFromDB`/`Exists`/`Delete`/`Search`/`DeleteNodes`/`ListNamespaces`/`Count` 共 8 个方法全部使用 `context.Background()`，无法传递超时、取消信号和 trace 信息。

**Go 问题**:
```go
func (m *MilvusConnectorImpl) SaveToDB(namespace string, data map[string]any) error {
    ctx := context.Background()  // 丢弃上游 context
    // ...
}
```

**修复方案**: 所有公开方法增加 `ctx context.Context` 参数。

---

### S-06: ContextEvolutionRail 构造中 LoadMemories 使用 context.Background() 丢弃 context

**章节**: 9.82 P7
**文件**: `internal/agentcore/harness/rails/evolution/context_evolution_rail.go:134`

**Go 问题**:
```go
if err := r.memoryService.LoadMemories(context.Background(), r.userID); err != nil {
```

**修复方案**: 将 `LoadMemories` 调用推迟到 `Init()` 或首次 `beforeTaskIteration` 中，使用实际 context。

---

### S-07: EventBus.ResumePolls 使用 context.Background() 而非 EventBus 的运行时 context

**章节**: 9.63 EventBus
**文件**: `internal/agent_teams/agent/coordination/event_bus.go:182`

**Go 问题**:
```go
func (b *EventBus) ResumePolls() {
    b.startPollTasksLocked(context.Background())  // 丢失运行时 context
}
```

**修复方案**: EventBus 需要保存启动时的 context，`ResumePolls` 应使用保存的 context。

---

### S-08: TeamAgent 中 CoordinationKernel 相关方法全部 ⤵️ 占位未实现

**章节**: 9.62 / 9.55
**文件**: `internal/agent_teams/agent/team_agent.go`

TeamAgent 中有 14 处 `⤵️(#9.62)` 占位，核心方法如 `invoke`/`stream` 中的 coordination 启动、入队、轮次结束等均为空实现。`NewTeamAgent` 中 `coordination` 字段类型为 `any`，从未被构造赋值（L118 `⤵️(#9.62): 构建 CoordinationKernel(self)`）。

**修复方案**: 接入 CoordinationKernel 到 TeamAgent 的 invoke/stream/interact/broadcast 方法中。

---

### S-09: TeamAgent.invoke/stream 是空壳，返回 nil, nil 无任何业务逻辑

**章节**: 9.55
**文件**: `internal/agent_teams/agent/team_agent.go:490-516`

Python `TeamAgent.invoke()` 执行 `coordination.start(session)` → `enqueue_user_input` → 从 streamQueue 读取直到 nil sentinel → `coordination.finalize_round()`。Go 的 `Invoke`/`Stream` 仅创建 `streamQueue` 后返回 `nil, nil`，整个 TeamAgent 无法执行任何任务。

**Python 样例**:
```python
async def invoke(self, inputs, session=None):
    await self._coordination.start(session)
    await self._coordination.enqueue_user_input(inputs)
    # read from stream_queue until None sentinel
    # ...
    await self._coordination.finalize_round()
```

**Go 问题**:
```go
func (a *TeamAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (map[string]any, error) {
    // ⤵️(#9.62): coordination.start(session) + 入队用户输入
    if a.streamController != nil {
        a.streamController.streamQueue = make(chan stream.Schema, 64)
    }
    // ⤵️(#9.62): 从 streamQueue 读取直到 nil sentinel → coordination.finalize_round()
    return nil, nil
}
```

**修复方案**: 实现 invoke/stream 的完整流程——启动 coordination → 入队输入 → 读取 streamQueue → finalize_round。

---

### S-10: TeamMember.Status() 是 stub 始终返回 READY，导致 SHUTDOWN_REQUESTED 检测失效

**章节**: 9.55 / 9.65
**文件**: `internal/agent_teams/agent/member.go:49-52`

Python `TeamMember.status()` 从 DB 读取实际状态：
```python
async def status(self) -> MemberStatus:
    member_data = await self.db.member.get_member(self.member_name, self.team_name)
    return MemberStatus(member_data.status) if member_data else None
```

Go 始终返回 `MemberStatusReady`：
```go
func (m *TeamMember) Status(ctx context.Context) (atschema.MemberStatus, error) {
    // TODO(#9.65): 从 DB 读取成员状态
    return atschema.MemberStatusReady, nil
}
```

此 stub 导致 `StreamController.runOneRound` 中 SHUTDOWN_REQUESTED 检测永远不触发（L560-566, L587-593），队友无法正常关闭。

**修复方案**: 实现 `Status()` 方法从 DB 读取真实状态。

---

### S-11: TeamAgent.ShutdownSelf 缺少 CloseStream 调用

**章节**: 9.55
**文件**: `internal/agent_teams/agent/team_agent.go:638-648`

Python `shutdown_self()` 调用 `stream_controller.close_stream()` 在状态更新前关闭流。Go 只调用了 `CooperativeCancel` 和 `UpdateStatus`，未调用 `CloseStream`。

**Python 样例**:
```python
async def shutdown_self(self):
    await self._stream_controller.cooperative_cancel()
    if self.team_member:
        await self.team_member.update_status(MemberStatus.SHUTDOWN)
    self._stream_controller.close_stream()
```

**修复方案**: 在 `UpdateStatus` 之后添加 `a.streamController.CloseStream()` 调用。

---

### S-12: TeamAgent.UpdateStatus 绕过 TeamMember 层直接写 DB，缺失事件发布

**章节**: 9.55
**文件**: `internal/agent_teams/agent/team_agent.go:367-378`

Python `TeamAgent.update_status()` 通过 `self.team_member.update_status(new_status)` 执行，`TeamMember.update_status()` 内部会做：状态等值短路、DB 写入、发布 `MemberStatusChangedEvent`。Go 直接调用 `backend.DB().Member().UpdateMemberStatus()`，跳过了 TeamMember 层的所有逻辑。

**Python 样例**:
```python
async def update_status(self, status: MemberStatus) -> None:
    if self.team_member:
        await self.team_member.update_status(status)
```

**Go 问题**:
```go
func (a *TeamAgent) UpdateStatus(ctx context.Context, status atschema.MemberStatus) error {
    backend.DB().Member().UpdateMemberStatus(ctx, a.MemberName(), backend.TeamName(), string(status))
    return nil
}
```

**修复方案**: 通过 `a.state.TeamMember.UpdateStatus()` 执行状态更新，确保事件发布和状态等值短路逻辑生效。

---

### S-13: AgentConfigurator.SetupInfra 缺少 Messager 构造和 SetupTeamBackend 调用

**章节**: 9.57
**文件**: `internal/agent_teams/agent/agent_configurator.go:168-224`

Python `setup_infra()` 步骤 5 创建 Messager（`create_messager(messager_config)`），步骤 8 创建 TeamBackend（`setup_team_backend(spec, ctx, messager, ...)`）。Go 的步骤 5 和步骤 8 都是 TODO 占位，导致整个团队消息系统和后端完全不可用。

**Go 问题**:
```go
// 5. MessagerConfig 调整 + CreateMessager
// TODO(#9.65): messagerConfig 节点 ID 调整 + CreateMessager(messagerConfig)

// 8. 团队后端
// TODO(#9.58): 设置团队后端 c.SetupTeamBackend(spec, ctx, messager, ...)
```

**修复方案**: 实现 Messager 创建和 `SetupTeamBackend` 调用。`SetupTeamBackend` 方法本身已实现（L311-390），只需在 `SetupInfra` 中调用。

---

### S-14: AgentConfigurator.SetupAgent 大量 TODO 占位，BuildTeamHarness 传 nil 参数

**章节**: 9.57
**文件**: `internal/agent_teams/agent/agent_configurator.go:228-292`

Python `setup_agent()` 共约 17 个步骤，Go 实现了步骤 3（workspace 初始化）和步骤 5（mount workspace），其余全部 TODO。步骤 15 `BuildTeamHarness` 被调用时所有 Rail 参数传入 `nil`：

```go
harness := agentteams.BuildTeamHarness(
    nil,   // TODO(#9.56): 构建规格
    string(ctx.Role),
    ctx.MemberName,
    nil,   // TODO(#9.68): 团队工具Rail
    nil,   // TODO(#9.68): 团队策略Rail
    nil,   // TODO(#9.68): 首轮门控
    nil,   // TODO(#9.68): 团队工作空间Rail
    nil,   // TODO(#9.68): 工具审批Rail
    nil,   // TODO(#9.68): 团队规划模式Rail
    false, // TODO(#9.runtime): 是否启用团队规划模式
)
```

**修复方案**: 逐步实现步骤 6-14（modelConfig、sysOperationSpec、buildSpec、Rails），确保 BuildTeamHarness 传入有效参数。

---

### S-15: TeamBackend.IsTeamCompleted 缺少"至少一个任务存在"和"成员列表非空"检查

**章节**: 9.58
**文件**: `internal/agent_teams/tools/team_backend.go:319-355`

Python `is_team_completed()` 在步骤 4 检查 `if not tasks: return None`（至少一个任务），步骤 5 检查 `if not members: return None`（成员非空）。Go 缺少这两个前置检查。

**Python 样例**:
```python
tasks = await self.task_manager.list_tasks()
if not tasks:
    return None
# ...
members = await self.db.member.get_team_members(self.team_name)
if not members:
    return None
```

**Go 问题**: Go 直接跳过空列表检查，空团队或无任务团队可能被误判为"完成"。

**修复方案**: 添加 `if len(tasks) == 0 { return nil, nil }` 和 `if len(members) == 0 { return nil, nil }` 前置检查。

---

### S-16: AgentLifecycleHandler.OnUserInput 传递 useSteer=false，Python 默认 True

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/agent_lifecycle.go:77`

Python `deliver_input(content)` 默认 `use_steer=True`，Go 硬编码 `false`。

**Python 样例**:
```python
await self._round.deliver_input(content)
# deliver_input 签名: async def deliver_input(self, content: Any, *, use_steer: bool = True)
```

**Go 问题**:
```go
if err := h.round.DeliverInput(ctx, content, false); err != nil {
```

**修复方案**: 将 `false` 改为 `true`，对齐 Python 默认值。

---

### S-17: TaskBoardHandler.OnTaskClaimed 缺少 human-agent 对非自身认领的过滤

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/task_board.go:79-96`

Python 对非自身认领，human-agent 直接 return（不执行 `on_task_board_event`），Go 缺少此过滤。

**Python 样例**:
```python
if payload.member_name != member_name:
    if is_self_human:
        return
    await self.on_task_board_event(event)
    return
```

**修复方案**: 在非自身认领分支加 human-agent 判断：
```go
if claimMember != memberName {
    if role == schema.TeamRoleHumanAgent {
        return
    }
    h.OnTaskBoardEvent(ctx, event)
    return
}
```

---

### S-18: TaskBoardHandler.OnTaskPlanDecision 缺少非目标成员的 board event 转发

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/task_board.go:102-122`

Python 对 `payload.member_name != member_name` 转发到 `on_task_board_event`，Go 缺少此分支。

**Python 样例**:
```python
if payload.member_name != member_name:
    await self.on_task_board_event(event)
    return
```

**修复方案**: 增加 memberName 检查，非目标成员转发 `OnTaskBoardEvent`。

---

### S-19: MessageHandler.OnMessageOrBroadcast 对 BROADCAST 误调用 ackUserBoundMessage

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/message.go:72-75`

Python 区分 MESSAGE 和 BROADCAST：只有 MESSAGE 时调用 `_ack_user_bound_message`，BROADCAST 不调用。

**Python 样例**:
```python
if event.event_type == TeamEvent.MESSAGE:
    await self._ack_user_bound_message(event)
await self._notify_human_agent_inbound(event)
```

**Go 问题**:
```go
if role == schema.TeamRoleLeader {
    h.ackUserBoundMessage(event)      // 对 MESSAGE 和 BROADCAST 都调用
    h.notifyHumanAgentInbound(event)
}
```

**修复方案**: 只在 `event.Transport.EventType == events.TeamEventMessage` 时调用 `ackUserBoundMessage`。

---

### S-20: ContextEvolutionRail 缺失 retrieval_query 逻辑

**章节**: 9.82 P7
**文件**: `internal/agentcore/harness/rails/evolution/context_evolution_rail.go:229-230`

Python `before_task_iteration` 中先取 `retrieval_query = getattr(ctx.inputs, "retrieval_query", None) or query`，用 `retrieval_query` 做检索和缓存键，Go 只用 `query`。

**Python 样例**:
```python
query = getattr(ctx.inputs, "query", None) or ""
retrieval_query = getattr(ctx.inputs, "retrieval_query", None) or query
```

**Go 问题**:
```go
query := taskInputs.Query
if query == "" { return nil }
```

**修复方案**: 增加 `retrieval_query` 字段读取，优先使用 `retrieval_query`，回退到 `query`，并使用 `retrieval_query` 作为检索键和缓存键。

---

### S-21: ContextEvolutionRail 缺失 pending_tools / tools_applied 机制

**章节**: 9.82 P7
**文件**: `internal/agentcore/harness/rails/evolution/context_evolution_rail.go`

Python `ContextEvolutionRail` 有 `_pending_tools: List[Any]` 和 `_tools_applied: bool` 字段，Go 完全缺失此机制。

**Python 样例**:
```python
self._pending_tools: List[Any] = []
self._tools_applied: bool = False
```

**修复方案**: 在结构体中添加 `pendingTools []any` 和 `toolsApplied bool` 字段，并提供属性访问器。

---

### S-22: ContextEvolutionRail memories_used 标注到 Extra 而非 result dict

**章节**: 9.82 P7
**文件**: `internal/agentcore/harness/rails/evolution/context_evolution_rail.go:353-354`

**Python 样例**:
```python
result = getattr(ctx.inputs, "result", None)
if isinstance(result, dict):
    result["memories_used"] = self.memories_used
```

**Go 问题**:
```go
if cbc.Extra() != nil {
    cbc.Extra()["memories_used"] = r.memoriesUsed
}
```

**修复方案**: 增加 `TaskIterationInputs.Result` 字段检查，当 result 为 `map[string]any` 时写入 `memories_used`，同时保留 `Extra()` 写入作为兜底。

---

### S-23: SkillEvolutionRail.extractToolContent 缺失 .data 属性层

**章节**: 9.24
**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:2003-2010`

Python 先检查 `getattr(result, "data", None)`，`data` 为 dict 时从中取 `skill_content` 或 `content`。Go 直接将 result 断言为 `map[string]any`，跳过了 `.data` 层。

**Python 样例**:
```python
result = inputs.tool_result
data = getattr(result, "data", None)
if isinstance(data, dict):
    content = data.get("skill_content") or data.get("content") or ""
```

**修复方案**: 增加中间层：先检查 ToolResult 是否有 `GetData()` 方法或 `.data` 字段（通过接口断言），如果有则从 data dict 中取值。TeamSkillEvolutionRail 的 `teamExtractToolContent` 也需同样修复。

---

### S-24: syncSkillPackage 创建的 SkillPackageMeta 未设置 UploadedAt

**章节**: 9.80a
**文件**: `internal/evolving/sharing/experience_sharer.go`

Python `SkillPackageMeta` 的 `uploaded_at` 字段有 `default_factory=_now_iso`，Go 未设置导致空字符串。

**修复方案**:
```go
meta := SkillPackageMeta{
    SkillID:     skillID,
    SkillName:   firstNonEmpty(resolvedName, skillName),
    Description: description,
    UploadedAt:  time.Now().UTC().Format(time.RFC3339Nano),
}
```

---

### S-25: HasSkillPackage 接口不返回 error，无法处理 backend 异常

**章节**: 9.80a
**文件**: `internal/evolving/sharing/interface.go:25`

Python `has_skill_package` 有 `try/except` 异常保护，Go 的 `SharingBackend` 接口只返回 `bool`（无 error），backend 出错时无法感知。

**Python 样例**:
```python
try:
    already_present = await self._backend.has_skill_package(skill_id)
except Exception as exc:
    logger.warning("[ExperienceSharer] backend.has_skill_package failed: %s", exc)
    return
```

**修复方案**: 修改接口签名 `HasSkillPackage(ctx context.Context, skillID string) (bool, error)`，调用方处理 error。

---

### S-26: DownloadSkillPackage/GetSkillPackageMeta 使用 recover 不合理

**章节**: 9.80a
**文件**: `internal/evolving/sharing/experience_sharer.go:388-410`

Go 使用 `defer recover` 捕获 panic，但 `SharingBackend` 接口已返回 error，调用方应直接处理 error 而非依赖 recover。使用 recover 掩盖了真正的编程错误（如 nil pointer dereference）。

**修复方案**: 移除 `defer recover`，直接依赖 error 返回值。

---

### S-27: ResolveSkillID error 传播语义与 Python 不一致

**章节**: 9.80a
**文件**: `internal/evolving/sharing/experience_sharer.go:149-157`

Python `resolve_skill_id` 在 provider 异常时吞掉错误并返回空字符串（resilience boundary），Go 版本返回 `("", err)` 传播错误。

**Python 样例**:
```python
try:
    skill_id, _, _, _ = await provider(skill_name)
except Exception as exc:
    logger.warning(...)
    return ""  # 吞掉异常，返回空字符串
```

**修复方案**: 与 Python 对齐，吞掉 error 返回空字符串：`return "", nil`。

---

### S-28: GraphMemory parseRelationFilteringResult 中 LHS/RHS 赋值构造空壳 Entity

**章节**: 7.11
**文件**: `internal/agentcore/memory/graph/graph_memory/base.go:1586-1590`

Python `setattr(relation, attr, val)` 将 UUID 字符串直接赋值到 `relation.lhs/rhs`，Go 构造了只含 UUID 的空壳 Entity，丢失了 Name/Content 等信息。

**修复方案**: 应从 `state.LookupTable.Entities[value]` 获取完整实体引用赋值。

---

### S-29: GraphMemory entityMerge 缺少等待 tasks 完成步骤

**章节**: 7.11
**文件**: `internal/agentcore/memory/graph/graph_memory/base.go:1329-1332`

Python `_entity_merge` 开头先等待所有 pending tasks 完成：`if state.tasks: await asyncio.wait(state.tasks)`，Go 缺少这个等待步骤。

**修复方案**: 在 `entityMerge` 开头等待 `state.EmbedTask` 和其他可能残留的 task 完成。

---

### S-30: GraphMemory handleRelationDedupe 修改 relations slice 不传播到调用方

**章节**: 7.11
**文件**: `internal/agentcore/memory/graph/graph_memory/base.go:1612-1619`

Python 的 `relations.remove()` 是 in-place 操作，Go 中 `relations` 是值传递的 slice header，函数内修改不影响外部。

**修复方案**: `handleRelationDedupe` 应返回修改后的 `relations` 列表，调用方赋值回原变量。

---

### S-31: StreamEventRail checkpointWait 缺少 context 传播，可能 goroutine 泄漏

**章节**: 10.6.10
**文件**: `internal/swarm/agents/harness/common/rails/stream_event_rail.go:476-481`

`checkpointWait` 使用 `sync.Cond.Wait()` 无限阻塞，不检查 context 取消信号。当 context 被取消（如 abort），goroutine 仍阻塞在 `pc.cond.Wait()` 上，导致 goroutine 泄漏。

**Go 问题**:
```go
func (r *JiuClawStreamEventRail) checkpointWait(sid string) {
    r.mu.Lock()
    pc := r.getPauseCond(sid)
    r.mu.Unlock()
    pc.Wait()  // 无 context 传播
}
```

**修复方案**: 改用带 context 的等待机制（如 channel + select），或在 pauseCond 中增加 context 取消时自动 Resume 的逻辑。

---

### S-32: inferStringError 正则缺少 case-insensitive 标志，且每次调用重新编译

**章节**: 10.6.10
**文件**: `internal/swarm/agents/harness/common/rails/stream_event_helpers.go:311,319`

Python 的 `re.search(r"\bsuccess\s*[:=]\s*False\b", text, re.IGNORECASE)` 使用 IGNORECASE 标志，Go 的 `regexp.MatchString` 未使用 `(?i)` 前缀，且每次调用 `regexp.MatchString` 和 `regexp.MustCompile` 都会重新编译正则，性能浪费。

**Python 样例**:
```python
if re.search(r"\bsuccess\s*[:=]\s*False\b", text, re.IGNORECASE):
    return True
```

**Go 问题**:
```go
if matched, _ := regexp.MatchString(`\bsuccess\s*[:=]\s*False\b`, text); matched {
```

**修复方案**: 1) 将正则预编译为包级变量：`var successFalseRe = regexp.MustCompile(`(?i)\bsuccess\s*[:=]\s*False\b`)` 2) exit_code 正则同样预编译。

---

## 一般问题 (M)

### M-01: CoordinationKernel.Setup() 缺少 blueprint/infra nil 校验

**章节**: 9.62
**文件**: `internal/agent_teams/agent/coordination/kernel.go:87-96`

Python `setup()` 在 blueprint/infra 为 None 时 raise RuntimeError，Go 未校验。

**修复方案**: 在 `Setup()` 开头添加 nil 校验。

---

### M-02: EventBus 事件队列 channel 容量 256 可能不足

**章节**: 9.63
**文件**: `internal/agent_teams/agent/coordination/event_bus.go:75`

Python `asyncio.Queue()` 无大小限制，Go 256 缓冲可能不足。

**修复方案**: 增大缓冲或改为非阻塞入队+丢弃策略。

---

### M-03: EventBus.Enqueue 阻塞式写入 channel，可能导致死锁

**章节**: 9.63
**文件**: `internal/agent_teams/agent/coordination/event_bus.go:188-190`

```go
b.eventCh <- event  // channel 满时永久阻塞
```

**修复方案**: 使用 `select` + `default` 实现非阻塞写入。

---

### M-04: EventDispatcher 白名单事件类型使用硬编码字符串

**章节**: 9.63
**文件**: `internal/agent_teams/agent/coordination/dispatcher.go:143-155`

**修复方案**: 使用 `schema.TeamEvent` 枚举常量替代硬编码字符串。

---

### M-05: MilvusConnectorImpl.Truncate 按 maxBytes 截断而非 maxChars

**章节**: 9.82 P7
**文件**: `internal/agentcore/context_evolver/core/persistence/milvus_connector.go:608-637`

Python `truncate(text, max_len)` 按字符数截断，Go 按字节数截断，中文等多字节字符差异明显。

**修复方案**: 确认 Milvus VARCHAR 限制语义，至少在参数命名上区分。

---

### M-06: MilvusConnectorImpl.LoadFromDB namespace 过滤表达式字符串拼接注入风险

**章节**: 9.82 P7
**文件**: `internal/agentcore/context_evolver/core/persistence/milvus_connector.go:270`

**修复方案**: 对 namespace 进行转义或添加字符白名单校验。

---

### M-07: SkillEvolutionRail.OnAfterToolCall/OnAfterModelCall 缺失异常保护

**章节**: 9.24
**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go`

Python `run_evolution` 整体包在 `try/except Exception` 中，Go 的 `OnAfterToolCall` 中若 panic 会直接传播。

**修复方案**: 增加 `defer func() { if rec := recover(); rec != nil { ... } }()` 保护。

---

### M-08: SkillEvolutionRail.RunEvolution 同步路径缺失 presented_entries 消费

**章节**: 9.24
**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:596-599`

**Python 样例**:
```python
elif ctx is not None:
    presented_entries = self._experience_tracker.consume_eval_state(session)
```

**修复方案**: 在同步路径增加 `presentedEntries := r.experienceTracker.ConsumeEvalState(sessionID)`。

---

### M-09: TeamSkillEvolutionRail detectExperienceDetailRead 内 context.Background()

**章节**: 9.24
**文件**: `internal/agentcore/harness/rails/evolution/team_skill_evolution_rail.go:1247`

```go
return r.teamSkillForExperienceDetailFile(context.Background(), filePath)
```

**修复方案**: 将 `context.Background()` 改为使用传入的 `ctx` 参数。

---

### M-10: EvolutionRail safeRunEvolution 使用 context.Background() 丢失上游 context

**章节**: 9.24
**文件**: `internal/agentcore/harness/rails/evolution/evolution_rail.go:709,738`

**修复方案**: 传递实际 ctx 或使用 `context.WithTimeout` 加超时保护。

---

### M-11: ContextEvolvingReActAgent.AutoConfigure 在 API_KEY 缺失时行为与 Python 不对齐

**章节**: 9.82 P6
**文件**: `internal/agentcore/context_evolver/context_evolving_react_agent.go:232-260`

Python `_auto_configure()` 在 API_KEY 缺失时静默跳过，Go 返回 error。

**修复方案**: API_KEY 缺失时应 `return nil`（静默跳过，对齐 Python）。

---

### M-12: ContextEvolvingReActAgent.Invoke 传 MemoryService=a 而非 a.memoryService

**章节**: 9.82 P6
**文件**: `internal/agentcore/context_evolver/context_evolving_react_agent.go:157-164`

Python 传的是 `self.memory_service`（TaskMemoryService 实例），Go 传的是 `a`（ContextEvolvingReActAgent 自身）。

**修复方案**: 将 `MemoryService: a` 改为 `MemoryService: a.memoryService`。

---

### M-13: RunTrials 缺少独立模式的持久化后处理和记忆预加载

**章节**: 9.82 P6
**文件**: `internal/agentcore/context_evolver/service/trajectory_generator.go`

Python `run_trials()` 在独立模式下（persist_type is not None）会创建 `persistence_helper` 并加载已有记忆，完成后再写回。Go 完全缺失这两个步骤。

**修复方案**: 在 `RunTrials` 开头检查 `params.PersistType`，创建 `MemoryPersistenceHelper` 加载/写回记忆。

---

### M-14: ContextEvolutionRail autoSummarize 仅支持 matts_mode="none" 但未限制

**章节**: 9.82 P7
**文件**: `internal/agentcore/harness/rails/evolution/context_evolution_rail.go:359-377`

**修复方案**: 在 `WithAutoSummarizeMattsMode` 中添加校验，非 "none" 值时 Warn。

---

### M-15: ContextEvolvingReActAgent 缺少 add_tool / add_tools 方法

**章节**: 9.82 P6
**文件**: `internal/agentcore/context_evolver/context_evolving_react_agent.go`

Python 提供了 `add_tool(tool)` 和 `add_tools(tools)` 方法，Go 完全缺失。

**Python 样例**:
```python
def add_tool(self, tool):
    self.ability_manager.add(tool.card)
    Runner.resource_mgr.add_tool(tool)
```

**修复方案**: 在 `ContextEvolvingReActAgent` 上添加 `AddTool` / `AddTools` 方法。

---

### M-16: FlushPendingUploads 对 upload_bundle error 重试与 Python 不一致

**章节**: 9.80a
**文件**: `internal/evolving/sharing/experience_sharer.go:256-275`

Python `upload_bundle` 抛异常时直接传播不重试，Go 对 `err != nil` 做退避重试。

**修复方案**: 保留 Go 的重试逻辑（更健壮），但添加注释说明这是 Go 侧增强。如严格对齐 Python，`err != nil` 时直接返回错误。

---

### M-17: ShareStager.wrap 缺少 record 深拷贝

**章节**: 9.80a
**文件**: `internal/evolving/sharing/share_stager.go:195-200`

Python 使用 `copy.deepcopy(record)`，Go 使用值拷贝，但 `EvolutionRecord` 可能包含指针字段（如 `Change EvolutionPatch`），浅拷贝导致共享底层指针数据。

**修复方案**: 检查 `EvolutionRecord` 是否包含指针字段，确保等效深拷贝安全。

---

### M-18: LocalFileBackend.UploadSkillPackage 中 meta.UploadedAt 未兜底

**章节**: 9.80a
**文件**: `internal/evolving/sharing/backend/local_file.go:334-339`

当调用方未设置 `UploadedAt` 时，写入也是空字符串。Python 的 `SkillPackageMeta` 构造时自动填充。

**修复方案**: 在 `UploadSkillPackage` 中对空 `UploadedAt` 设置当前时间。

---

### M-19: GraphMemory entityEnrich 缺少 state.MergingTasks 移除

**章节**: 7.11
**文件**: `internal/agentcore/memory/graph/graph_memory/base.go:1480-1497`

Python 阻塞实体等待 pending merge 完成后从 `state.merging_tasks.remove(task)`，Go 缺少移除步骤。

**修复方案**: 在阻塞实体等待完成后，从 `state.MergingTasks` 中移除该 task。

---

### M-20: GraphMemory resolveEachRelation 使用 context.Background() 丢上下文

**章节**: 7.11
**文件**: `internal/agentcore/memory/graph/graph_memory/base.go:2195`

**修复方案**: 使用函数参数 `ctx` 而非 `context.Background()`。

---

### M-21: GraphMemory updateEntitiesForRelationRemoval 用名字匹配替代 UUID 匹配

**章节**: 7.11
**文件**: `internal/agentcore/memory/graph/graph_memory/base.go:1787-1790`

Python 用 UUID 精确匹配 `existing_entity_item.uuid == entity.uuid`，Go 用名字模糊匹配 `decl.Name == entity.Name`。

**修复方案**: 改用 UUID 匹配，且当匹配时替换 entity 引用。

---

### M-22: GraphMemory fetchRelevantEntities 缺少 len(tasks)<=1 前置判断

**章节**: 7.11
**文件**: `internal/agentcore/memory/graph/graph_memory/base.go:1186-1200`

Python 在 `len(state.tasks) <= 1` 时直接返回，Go 直接 `state.EmbedTask.Wait()` 无前置条件。

**修复方案**: 添加 `if len(state.Tasks) <= 1 { return nil }` 前置判断。

---

### M-23: MessageHandler.OnMessageOrBroadcast 缺少 member_name 和 message_manager 空值守卫

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/message.go:64-85`

**Python 样例**:
```python
if not member_name or self._infra.message_manager is None:
    return
```

**修复方案**: 在方法开头添加空值守卫。

---

### M-24: MessageHandler.OnPollMailbox 缺少 message_manager 空值守卫

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/message.go:89-91`

**Python 样例**:
```python
if member_name and self._infra.message_manager:
    await self._process_unread_messages(member_name)
```

**修复方案**: 添加 `if h.blueprint.MemberName() == "" || h.infra.MessageManager == nil { return }`。

---

### M-25: TaskBoardHandler.OnTaskClaimed/OnTaskBoardEvent/OnTaskPlanDecision 缺少 task_manager 空值守卫

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/task_board.go:71-122`

Python 在三个方法开头都有 `if not member_name or self._infra.task_manager is None: return`，Go 全部缺失。

**修复方案**: 添加 `memberName` 和 `task_manager` 空值守卫。

---

### M-26: TaskBoardHandler.OnTaskClaimed 缺少 resume_polls 调用

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/task_board.go:81-91`

Python 在自身认领时先 `resume_polls()`，Go 缺少此调用。

**Python 样例**:
```python
await self._poll.resume_polls()
```

**修复方案**: 在自身认领分支开头添加 `h.poll.ResumePolls()`。

---

### M-27: TaskBoardHandler.OnTaskPlanDecision 缺少 resume_polls 调用

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/task_board.go:102-122`

同 M-26。

**修复方案**: 添加 `h.poll.ResumePolls()`。

---

### M-28: MemberHandler.OnMemberShutdownDrain 角色判断应改用 != Teammate 模式

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/member.go:109-113`

Python 用 `if self._blueprint.role != TeamRole.TEAMMATE: return`，Go 用 `if role == TeamRoleLeader || role == TeamRoleHumanAgent`。逻辑等价但 Python 写法更安全（未来新增角色时自动过滤）。

**修复方案**: 改为 `if role != schema.TeamRoleTeammate { return }`。

---

### M-29: MemberHandler.nudgeIdleMemberWithStaleClaims 缺少 newStatus==oldStatus 退出和 targetID 空检查

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/member.go:180-192`

Python 检查 `if new_status == old_status: return` 和 `if not target_id: return`，Go 两个检查都缺失。

**修复方案**: 添加 `if newStatus == oldStatus { return }` 和 `if targetID == "" { return }`。

---

### M-30: MemberHandler.handleLeaderMemberEvent 缺少 i18n 和事件类型分支

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/member.go:157-175`

Python 对每种事件类型做了 i18n 格式化，Go 只记录了通用日志。

**修复方案**: 等 i18n 就绪后对齐 Python 的事件类型分支处理。

---

### M-31: TeamCompletionHandler.OnPollTask 缺少 has_in_flight_round / is_agent_running 守卫

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/team_completion.go:89-111`

**Python 样例**:
```python
if self._round.has_in_flight_round() or self._round.is_agent_running():
    return
```

**修复方案**: 在实现时加上这两个条件。

---

### M-32: inferToolResultError/structuredToolResultPayload 不处理 map[string]string 类型

**章节**: 10.6.10
**文件**: `internal/swarm/agents/harness/common/rails/stream_event_helpers.go:78-105`

Python 的 `_infer_tool_result_error` 和 `_structured_tool_result_payload` 对 dict 类型处理，Python dict 不区分 `dict[str, any]` 和 `dict[str, str]`。但 Go 中工具结果可能是 `map[string]string` 类型（常见于简单 JSON 返回），`inferToolResultError` 的 `case map[string]any` 不会匹配 `map[string]string`，导致错误推断失效。

**修复方案**: 在 `inferToolResultError` 中添加 `case map[string]string` 分支，转为 `map[string]any` 后递归调用。`structuredToolResultPayload` 同理。

---

### M-33: inferStringError 中 success=False 正则硬编码 "False"，不匹配 "false" 小写

**章节**: 10.6.10
**文件**: `internal/swarm/agents/harness/common/rails/stream_event_helpers.go:311`

Python 的 `re.search(r"\bsuccess\s*[:=]\s*False\b", text, re.IGNORECASE)` 匹配 `False`、`false`、`FALSE` 等各种大小写。Go 的 `regexp.MatchString(`\bsuccess\s*[:=]\s*False\b`, text)` 只匹配大写 `False`。

**修复方案**: 使用 `(?i)` 标志：`regexp.MatchString(`(?i)\bsuccess\s*[:=]\s*False\b`, text)`。

---

### M-34: ContextEvolvingReActAgent.Invoke 缺少 auto_summarize 后处理逻辑

**章节**: 9.82 P6
**文件**: `internal/agentcore/context_evolver/context_evolving_react_agent.go:266-327`

Go 的 `autoSummarize` 和 `autoSummarizeMattsMode` 字段存在但从未使用，构造参数接收但无任何效果。

**修复方案**: 在 `invokeWithMemory` 返回前，检查 `a.autoSummarize`，若为 true 则提取轨迹并调用 `SummarizeTrajectories`。

---

### M-35: UpdateModelPool 缺少 PersistLeaderConfig 调用

**章节**: 9.55
**文件**: `internal/agent_teams/agent/team_agent.go:797-805`

Python `update_model_pool()` 在更新后调用 `self.recovery_manager.persist_leader_config(session)` 持久化配置。Go 的 `UpdateModelPool` 只更新了 allocator，缺少持久化步骤。

**修复方案**: 在 `UpdateModelPool` 中调用 `a.PersistSessionManifest(session)` 或等 recoveryManager 就绪后调用 `persist_leader_config`。

---

### M-36: AgentLifecycleHandler.buildInteractiveInput 对 task_plan_response 缺少 plan_id 字段

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/agent_lifecycle.go:186-196`

Python `on_task_plan_response` 传 `plan_id` 字段，Go 的 `buildInteractiveInput` 统一传 `auto_confirm` 字段。

**Python 样例**:
```python
interactive_input.update(payload.tool_call_id, {
    "approved": payload.approved,
    "feedback": payload.feedback,
    "plan_id": getattr(payload, "plan_id", "") or "",
})
```

**修复方案**: `on_task_plan_response` 不应使用 `buildInteractiveInput`，而应单独构建含 `plan_id` 的 InteractiveInput。

---

### M-37: DispatcherInfra 是空接口，handler 无法通过 infra 访问任何功能

**章节**: 9.63
**文件**: `internal/agent_teams/agent/coordination/protocols.go:69`

Python 的 `TeamInfra` 有 `task_manager`、`message_manager`、`team_backend`、`messager` 等丰富字段，Go 的 `DispatcherInfra` 是空接口 `type DispatcherInfra interface{}`，所有 handler 都无法通过 `infra` 访问任何功能。这是导致大量空值守卫缺失和 TODO 占位的根本原因。

**修复方案**: 在 `DispatcherInfra` 中定义 handler 实际需要的窄接口（TaskManager、MessageManager、TeamBackend、Messager），使 handler 可以通过接口调用后端。

---

## 提示问题 (T)

### T-01: EventBus 日志使用 ComponentChannel 而非专用组件

**章节**: 9.63
**文件**: `internal/agent_teams/agent/coordination/event_bus.go:63`

coordination 包应使用专用组件常量，与 channel（IM 通道）区分。

**修复方案**: 在 logger 包新增 `ComponentCoordination` 常量。

---

### T-02: KernelHost 接口过窄

**章节**: 9.62
**文件**: `internal/agent_teams/agent/coordination/kernel.go:16-26`

当前 `KernelHost` 仅组合 `DispatcherHost` + 4 个方法，但 Python 的 `CoordinationKernel` 需要访问 host 的大量属性。

**修复方案**: 扩展 KernelHost 接口或在 coordination 包内部定义更完整的 host 协议。

---

### T-03: ContextEvolutionRail originalPromptTemplate 使用 []map[string]any 弱类型

**章节**: 9.82 P7
**文件**: `internal/agentcore/harness/rails/evolution/context_evolution_rail.go:68`

**修复方案**: 后续重构时考虑使用具体消息类型替代 `[]map[string]any`。

---

### T-04: MilvusConnectorImpl ProbeReachable 使用 getClient 触发连接+初始化

**章节**: 9.82 P7
**文件**: `internal/agentcore/context_evolver/core/persistence/milvus_connector.go:597-605`

`ProbeReachable` 调用 `getClient`，但 `getClient` 内部会尝试 `initCollection`，超出了"探测可达性"的语义。

**修复方案**: `ProbeReachable` 应直接尝试连接而不触发 collection 初始化。

---

### T-05: MemoryPersistenceHelper auto 模式探测超时仅 5 秒

**章节**: 9.82 P7
**文件**: `internal/agentcore/context_evolver/core/persistence/persistence_helper.go:252`

**修复方案**: 增大默认探测超时或允许通过 Option 配置。

---

### T-06: EventBus runLoop 中的 panic recover 缺少堆栈信息

**章节**: 9.63
**文件**: `internal/agent_teams/agent/coordination/event_bus.go:225-236`

**修复方案**: 使用 `logger.Error(...).Str("stack", string(debug.Stack()))` 记录完整堆栈。

---

### T-07: CoordinationKernel.Setup() 中 EventBus 创建参数硬编码

**章节**: 9.62
**文件**: `internal/agent_teams/agent/coordination/kernel.go:88`

```go
eventBus := NewEventBus(role, 30.0, 30.0)
```

**修复方案**: 将轮询间隔作为 `Setup()` 或构造函数的参数。

---

### T-08: SkillEvolutionRail requestIDPrefix 可能与 Python 默认值不一致

**章节**: 9.24
**文件**: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:263`

Go 传 `"skill_evolve_"`，Python 的 `OnlineEvolutionOrchestrator` 未显式传 `request_id_prefix`。

**修复方案**: 检查 Python `OnlineEvolutionOrchestrator` 构造函数的 `request_id_prefix` 默认值。

---

### T-09: GraphMemory maybeGC 缺少 compact

**章节**: 7.11
**文件**: `internal/agentcore/memory/graph/graph_memory/base.go:2063`

Python GC 后执行 `db_backend.refresh(skip_compact=False)`，Go 使用 `WithFlush(false)` 对应 `skip_compact=True`，未做 compact。

**修复方案**: GC 时的 Refresh 应使用 `graph.WithFlush(true)` 对应 `skip_compact=False`。

---

### T-10: ExperienceSharing DownloadRelevant error 处理与 Python 不一致

**章节**: 9.80a
**文件**: `internal/evolving/sharing/experience_sharer.go:342-349`

Python `download_relevant` 吞掉异常返回空列表，Go 返回 `error` 传播。

**修复方案**: 与 Python 对齐，吞掉 error 返回空切片。

---

### T-11: RunTrialsInput.MemoryService 接口无法访问 vector_store

**章节**: 9.82 P6
**文件**: `internal/agentcore/context_evolver/service/trajectory_generator.go`

Python `run_trials` 依赖 `params.memory_service.vector_store` 属性做独立模式持久化，Go 的 `SummarizeTrajectorier` 接口无法访问。

**修复方案**: 在 `RunTrials` 中通过 type assertion 或扩展接口获取 vector_store。

---

### T-12: EventBus.Stop() 中 shutdown 事件和 context cancel 的竞态

**章节**: 9.63
**文件**: `internal/agent_teams/agent/coordination/event_bus.go:139-150`

shutdown 事件可能在 context cancel 之后被 runLoop 处理。

**修复方案**: 确保先发送 shutdown 事件并等待处理，再 cancel context；或去掉 shutdown 事件仅依赖 context cancel。

---

### T-13: GraphMemory _relation_dedupe 中 existing_entities 静默跳过

**章节**: 7.11
**文件**: `internal/agentcore/memory/graph/graph_memory/base.go:1713-1719`

Python 在 lookup table 中找不到实体会 raise error，Go 静默跳过。

**修复方案**: 找不到 lhs/rhs 对应实体时应返回错误而非静默跳过。

---

### T-14: AgentLifecycleHandler.OnStandby 缺少 member_name 日志字段

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/agent_lifecycle.go:87-90`

Python 日志包含 member_name，Go 缺少。

**修复方案**: 添加 `.Str("member_name", h.blueprint.MemberName())`。

---

### T-15: AgentLifecycleHandler.OnCleaned 缺少 member_name 日志字段

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/agent_lifecycle.go:95-105`

同 T-14。

**修复方案**: 在日志中添加 `member_name` 字段。

---

### T-16: TeamCompletionHandler.OnTeamCompleted 缺少 member_count / task_count 日志字段

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/team_completion.go:141-153`

**Python 样例**:
```python
team_logger.info("team {} reported completed: {} members, {} tasks", payload.team_name, payload.member_count, payload.task_count)
```

**修复方案**: 添加 `.Int("member_count", ...) .Int("task_count", ...)`。

---

### T-17: TeamCompletionHandler.OnTaskListDrained 缺少 team_name / task_count 日志字段

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/team_completion.go:122-125`

**修复方案**: 添加 `team_name` 和 `task_count` 字段。

---

### T-18: buildInteractiveInput 忽略 Update error 返回

**章节**: 9.63 Handlers
**文件**: `internal/agent_teams/agent/coordination/handlers/agent_lifecycle.go:186-196`

`input.Update(toolCallID, value)` 的错误被 `_ =` 忽略，如果 Update 失败返回的 input 可能不完整。

**修复方案**: 检查 `Update` 的 error，失败时返回 nil。

---

### T-19: updateExecution 是空实现（仅日志），缺失状态持久化

**章节**: 9.55
**文件**: `internal/agent_teams/agent/team_agent.go:858-863`

Python `_update_execution` 通过 TeamMember 层更新执行状态到 DB，Go 只记录日志。

**Go 问题**:
```go
func (a *TeamAgent) updateExecution(ctx context.Context, status atschema.ExecutionStatus) error {
    // ⤵️ 待 9.55 完善后实现具体状态持久化逻辑
    logger.Debug(logComponent).Str("member_name", a.MemberName()).
        Str("execution_status", string(status)).Msg("更新执行状态")
    return nil
}
```

**修复方案**: 实现 `updateExecution`，通过 `a.state.TeamMember.UpdateExecutionStatus()` 更新到 DB。

---

## ⤵️ 占位代码状态确认

| 占位位置 | 描述 | 状态 |
|---------|------|------|
| `team_agent.go` 14 处 `⤵️(#9.62)` | CoordinationKernel 集成 | ❌ 未实现 |
| `stream_controller.go` 3 处 `⤵️(#9.62)` | Coordination 回调 | ❌ 未实现 |
| `runtime/manager.go` 9 处 `⤵️(#9.62)` | Runtime 协调集成 | ❌ 未实现 |
| `agent_configurator.go` 多处 `TODO(#9.58/#9.65/#9.68)` | Messager/TeamBackend/Rails | ❌ 未实现 |
| `member.go` 4 处 `TODO(#9.65)` | TeamMember Status/UpdateStatus | ❌ stub |
| `human_agent_inbox.go` 2 处 `⤵️(9.55)` | TeamAgent 集成 | ❌ 未实现 |
| `deep_adapter.go` 多处 `⤵️(10.6.3-10/11.10)` | Swarm Rails/A2X/Cron | ❌ 未实现（标记正确） |
| `memory/manager.go` 多处 `⤵️(7.1+7.2+9.65a)` | 团队记忆工具集 | ❌ 未实现（标记正确） |
| `session_tools.go` `⤵️(异步模式)` | SubagentRail 异步 | ❌ 未实现（标记正确） |

---

## 修复优先级建议

1. **P0（立即修复）**: S-01/02/03/04 — CoordinationKernel 生命周期方法缺失核心步骤；S-10 — TeamMember.Status() stub 导致 SHUTDOWN_REQUESTED 失效；S-16 — OnUserInput useSteer=false 与 Python 不一致；S-19 — ackUserBoundMessage 对 BROADCAST 误调用；S-24/25/26 — ExperienceSharing UploadedAt/接口签名/recover 不合理
2. **P1（本迭代修复）**: S-05/06/07 — context.Background() 误用；S-08/09/11/12 — TeamAgent 核心流程空壳/缺失逻辑；S-13/14 — AgentConfigurator SetupInfra/SetupAgent 关键缺失；S-15 — IsTeamCompleted 缺少前置检查；S-20/21/22 — ContextEvolutionRail 功能缺失；S-28/29/30 — GraphMemory 核心逻辑 bug；S-31/32 — StreamEventRail goroutine 泄漏/正则问题
3. **P2（下迭代修复）**: S-17/18 — TaskBoard handler 缺少过滤；S-23 — SkillEvolutionRail .data 层；S-27 — ResolveSkillID error 语义；M-01~37 — 接口校验、事件白名单、异常保护、语义对齐、i18n 等一般问题
4. **P3（随迭代修复）**: T-01~19 — 日志组件、类型安全、堆栈信息等提示问题

---

## 模块问题分布

| 模块 | 严重 | 一般 | 提示 | 合计 |
|------|------|------|------|------|
| CoordinationKernel (9.62) | 4 | 1 | 3 | 8 |
| EventBus+Dispatcher (9.63) | 3 | 2 | 3 | 8 |
| Coordination Handlers (9.63) | 5 | 13 | 6 | 24 |
| ContextEvolutionRail (9.82 P7) | 3 | 2 | 1 | 6 |
| MilvusConnector (9.82 P7) | 1 | 2 | 2 | 5 |
| Evolution Rails (9.24) | 1 | 3 | 1 | 5 |
| ContextEvolvingReActAgent (9.82 P6) | 0 | 4 | 1 | 5 |
| ExperienceSharing (9.80a) | 4 | 3 | 1 | 8 |
| GraphMemory (7.11) | 3 | 4 | 2 | 9 |
| StreamEventRail (10.6.10) | 2 | 2 | 0 | 4 |
| TeamAgent/StreamController (9.55) | 4 | 1 | 1 | 6 |
| AgentConfigurator (9.57) | 2 | 0 | 0 | 2 |
| TeamBackend (9.58) | 1 | 0 | 0 | 1 |
| **合计** | **32** | **37** | **19** | **88** |
