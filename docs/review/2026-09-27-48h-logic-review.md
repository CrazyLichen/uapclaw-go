# 2026-09-27 48h 逻辑审查

> 审查范围：2026-09-25 ~ 2026-09-27 提交（56 commits, 124 Go 源文件）
> 
> 主要完成章节：
> - 9.63 Coordination（EventBus + Dispatcher + 6 Handlers + CoordinationKernel）
> - 9.61a Team Namespace + 9.61 Metadata + RecoveryManager
> - 7.13 ExternalMemoryRail + MemoryProvider
> - 9.67 Team Observability（Callback/Monitor/Rail 三层）
> - 9.82 P7 ContextEvolutionRail + MilvusConnector + MemoryPersistenceHelper
> - 9.24 P6 Session Memory 集成 + TeamSkillCreateRail
> - 7.11+7.12 Entity/Relation 对齐修复 + GraphMemory
> - StreamEventRail 完整实现
> - 9-26 审查修复（S/M 级问题）

---

## 严重问题（S）：34 项

### S-01 CoordinationKernel.start() 大量基础设施初始化缺失

**Python 样例**（`agent_teams/agent/coordination/kernel.py` L105-183）：
```python
async def start(self, session=None):
    # 1. session bind/release
    if session is not None:
        await sess_mgr.bind_session(session)
    else:
        sess_mgr.release_session()
    # 2. team recovery (leader path)
    if host.role == TeamRole.LEADER and infra.team_backend:
        existing = await infra.team_backend.db.team.get_team(...)
        if existing is not None:
            non_leader_members = await infra.team_backend.list_members()
            if non_leader_members and all(m.status == MemberStatus.SHUTDOWN.value for m in non_leader_members):
                await infra.team_backend.clean_team()
            else:
                await host.recover_team()
    # 3. workspace init
    if infra.workspace_manager and not infra.workspace_initialized:
        await infra.workspace_manager.initialize(remote_url=remote_url)
    # 4. memory toolkit wire
    memory_manager = resources.memory_manager
    if memory_manager and harness:
        success = await memory_manager.init_toolkit()
        if success:
            harness.register_member_tools(memory_manager)
            await harness.inject_member_memory(memory_manager, query=...)
    # 5. member status update
    await host.update_status(MemberStatus.READY)
    # 6. start bus + subscribe transport
    await self._event_bus.start(wake_callback=self._dispatcher.dispatch)
    if infra.messager:
        await self.subscribe_transport(team_name)
    # 7. rearm team-completion
    self._dispatcher.team_completion.rearm()
```

**Go 问题**（`internal/agent_teams/agent/coordination/kernel.go` L130-159）：
`Start()` 仅 15 行：绑定 dispatcher callback + 启动 bus + rearm。注释说"Go 端由各自的 Manager 负责"但无证据调用方已补全。session bind、team recovery、workspace init、memory toolkit wire、member status update 全部缺失。

**修复方案**：在 `Start()` 中补充所有步骤，或确认调用方 `TeamAgent` 的 `Start()` 方法已补全这些逻辑。如由调用方负责，需在 `Start()` 注释中明确列出"调用方需在调用 Start 前完成以下步骤"。

---

### S-02 CoordinationKernel.pause() 大量清理逻辑缺失

**Python 样例**（`kernel.py` L185-230）：
```python
async def pause(self):
    if self._lifecycle_state != "running": return
    await self.drain_agent_task()
    host.persist_allocator_state()
    if host.role == TeamRole.LEADER:
        await self._mark_live_teammates(MemberStatus.PAUSED)
        await host.spawn_manager.cancel_recovery_tasks()
        await host.spawn_manager.shutdown_all_handles()
        self._persist_team_lifecycle("paused")
    # 发布 TeamStandbyEvent
    if messager and host.role == TeamRole.LEADER:
        await messager.publish(TeamTopic.TEAM.build(...), ...)
    await self.unsubscribe_transport()
    await self._event_bus.stop()
    self.close_stream()
    host.session_manager.release_session()
    self._lifecycle_state = "paused"
```

**Go 问题**（`kernel.go` L166-184）：
`Pause()` 仅 5 行：停 bus + 改状态。TODO(#9.63) 标注待补充。drain_agent_task、persist_allocator_state、mark_live_teammates、cancel_recovery_tasks、shutdown_all_handles、persist_team_lifecycle、publish_standby、unsubscribe_transport、close_stream、release_session 全部缺失。

**修复方案**：逐步补充上述所有步骤，优先级：drain_agent_task > mark_live_teammates > unsubscribe_transport > release_session > close_stream。

---

### S-03 CoordinationKernel.stop() 大量清理逻辑缺失

**Python 样例**（`kernel.py` L296-328）：
```python
async def stop(self):
    if self._lifecycle_state in ("idle", "stopped"): return
    await self.drain_agent_task()
    host.persist_allocator_state()
    if host.role == TeamRole.LEADER:
        await self._mark_live_teammates(MemberStatus.STOPPED)
    await self.unsubscribe_transport()
    await host.spawn_manager.cancel_recovery_tasks()
    await host.spawn_manager.shutdown_all_handles()
    if memory_manager:
        await memory_manager.close()
    await self._event_bus.stop()
    self.close_stream()
    host.session_manager.release_session()
    self._lifecycle_state = "stopped"
```

**Go 问题**（`kernel.go` L191-209）：
`Stop()` 仅 5 行。缺失：drain_agent_task、mark_live_teammates、unsubscribe_transport、cancel_recovery_tasks、shutdown_all_handles、memory_manager.close、close_stream、release_session。

**修复方案**：同 S-02。

---

### S-04 CoordinationKernel 缺少 EnqueueMailboxAfterFirstIteration

**Python 样例**（`kernel.py` L391-401）：
```python
async def enqueue_mailbox_after_first_iteration(self):
    host = self._host
    if host.role == TeamRole.LEADER: return
    gate = host.resources.first_iter_gate
    if gate is None or self._event_bus is None: return
    await gate.wait()
    await self._event_bus.enqueue(InnerEventMessage(event_type=InnerEventType.POLL_MAILBOX))
```

**Go 问题**：`CoordinationKernel` 完全缺失此方法。Teammate 等待 first_iter_gate 后的首次邮箱轮询永远不会触发。

**修复方案**：添加 `EnqueueMailboxAfterFirstIteration(gate FirstIterGate)` 方法，对齐 Python 逻辑。

---

### S-05 CoordinationKernel 缺少 SubscribeTransport / UnsubscribeTransport

**Python 样例**（`kernel.py` L330-372）：
```python
async def subscribe_transport(self, team_name):
    # 注册直接消息处理器 + 订阅所有 TeamTopic
    await messager.register_direct_message_handler(self._event_bus.enqueue)
    for topic in TeamTopic:
        topic_str = topic.build(session_id, team_name)
        await messager.subscribe(topic_str, _filter_self)
        self._subscribed_topics.append(topic_str)

async def unsubscribe_transport(self):
    await messager.unregister_direct_message_handler()
    for topic in self._subscribed_topics:
        await messager.unsubscribe(topic)
    self._subscribed_topics.clear()
```

**Go 问题**：`CoordinationKernel` 完全缺失这两个方法。传输层事件（跨进程消息）无法接入协调系统。

**修复方案**：添加 `SubscribeTransport(ctx, teamName)` 和 `UnsubscribeTransport()` 方法。需要 `messager.Messager` 接口注入。

---

### S-06 CoordinationKernel 缺少 DrainAgentTask / CloseStream / FinalizeRound

**Python 样例**：
```python
async def drain_agent_task(self):
    await self._host.stream_controller.drain_agent_task()
def close_stream(self):
    self._host.stream_controller.close_stream()
async def finalize_round(self):
    memory_manager = host.resources.memory_manager
    if memory_manager:
        await memory_manager.extract_after_round()
    host.stream_controller.stream_queue = None
```

**Go 问题**：`CoordinationKernel` 完全缺失这三个方法。stop/pause 路径依赖的清理操作无法执行。

**修复方案**：添加 `DrainAgentTask()`、`CloseStream()`、`FinalizeRound(ctx)` 方法，委托 `KernelHost` 的对应接口。

---

### S-07 CoordinationKernel 缺少 MarkLiveTeammates / PersistTeamLifecycle

**Python 样例**（`kernel.py` L232-294）：
```python
async def _mark_live_teammates(self, target_status: MemberStatus):
    spawned = set(host.spawn_manager.spawned_handles.keys())
    members = await team_backend.list_members()
    for member in members:
        if member.member_name not in spawned: continue
        current = MemberStatus(member.status)
        if current in {MemberStatus.UNSTARTED, MemberStatus.SHUTDOWN}: continue
        await team_backend.db.member.update_member_status(
            member.member_name, team_name, target_status.value)

def _persist_team_lifecycle(self, lifecycle):
    merge_team_namespace(session, team_name, {"lifecycle": lifecycle})
```

**Go 问题**：缺失。pause 路径不会标记队友 PAUSED，stop 路径不会标记 STOPPED。团队恢复时无法知道队友为何停止。

**修复方案**：添加 `markLiveTeammates(ctx, targetStatus)` 和 `persistTeamLifecycle(sess, lifecycle)` 方法。

---

### S-08 Coordination Handlers 大量 TODO 空实现

**Go 问题**（`internal/agent_teams/agent/coordination/handlers/`）：
- `message.go`：`processUnreadMessagesWithSteer` 和 `deliverUnreadMessageSummary` 都是 TODO 空实现
- `stale_task.go`：`checkStaleClaimedTasks` 和 `checkStalePendingTasks` 都是 TODO 空实现
- `task_board.go`：`nudgeIdleAgent` 和 `onTaskPlanDecision` 都是 TODO 空实现
- `team_completion.go`：`onPollTask` 缺失 `is_team_completed` 检查
- `member.go`：`nudgeStaleMember` 缺失完整提醒逻辑

**Python 样例**（`handlers/message.py`）：
```python
async def process_unread_messages_with_steer(self, event):
    hitt = self._infra.team_backend
    messages = await hitt.msg.get_unread_messages(member_name)
    if not messages:
        return
    # 格式化消息 + 调用 deliver_input
    content = self._format_messages_for_agent(messages)
    await self._host.deliver_input(content, use_steer=True)
    await hitt.msg.mark_read(member_name, [m.message_id for m in messages])
```

**修复方案**：逐步补充各 handler 的核心逻辑。优先级：message > stale_task > task_board > team_completion > member。

---

### S-09 buildExternalMemoryRail() 返回 nil 空壳

**Python 样例**（`interface_deep.py` L2131-2145）：
```python
def _build_external_memory_rail(self):
    config = self._deep_config
    workspace_dir = self._workspace_dir
    return build_external_memory_rail(config, workspace_dir)
```

**Go 问题**（`deep_adapter_rails.go` L569-571）：
```go
func buildExternalMemoryRail() *memory.ExternalMemoryRail {
    return nil // ⤵️ 10.6.3-10: 实现 ExternalMemoryRail
}
```

**修复方案**：实现 builder 函数，根据 config 选择 provider（openjiuwen/mem0/openviking/plugin）创建 Provider 实例并返回 Rail。需先完成 S-10 的 config 包。

---

### S-10 缺少 external_memory_builder / external_memory_config 包

**Python 路径**：`jiuwenswarm/agents/harness/common/memory/external_memory_builder.py` + `external_memory_config.py`

**Go 问题**：`internal/swarm/agents/harness/common/memory/` 只有 `forbidden.go`，没有 builder/config 文件。缺失函数：
- `GetMemoryEngine()` → 返回 "builtin"|"external"|"both"|"none"
- `IsExternalMemoryAllowed()` / `IsExternalMemoryEnabled()`
- `GetExternalMemoryConfig()` → 读取 config 中的 `memory.external` 段
- `BuildExternalMemoryRail()` → 按 provider 名称分发构建

**修复方案**：创建 `external_memory_config.go` 和 `external_memory_builder.go`，对齐 Python 的配置读取和 builder 分发逻辑。

---

### S-11 handleExternalMemoryRailByConfig() 未实现

**Python 样例**：
```python
async def _handle_external_memory_rail_by_config(self):
    if is_external_memory_enabled(self._deep_config):
        if not self._external_memory_rail_registered:
            rail = await self._build_external_memory_rail()
            if rail:
                self._instance.register_rail(rail)
                self._external_memory_rail_registered = True
    else:
        if self._external_memory_rail_registered:
            provider = self._external_memory_rail._provider
            if provider:
                await provider.on_session_end(messages)
            self._instance.unregister_rail(rail)
            self._external_memory_rail_registered = False
```

**Go 问题**（`deep_adapter_rails.go` L842-843, L992）：仅标注 `⤵️ 待回填: handleExternalMemoryRailByConfig`，无任何实现。

**修复方案**：实现按配置注册/注销 ExternalMemoryRail 的完整逻辑，包括去重标志和 `OnSessionEnd` 回调。

---

### S-12 OnSessionEnd() 在 ExternalMemoryRail 生命周期中从未被调用

**Python 样例**：在 `_handle_external_memory_rail_by_config()` 注销分支中先调用 `provider.on_session_end(messages)` 再 `unregister_rail()`。

**Go 问题**：虽然 `MemoryProvider` 接口定义了 `OnSessionEnd()`，但 Go 的 `ExternalMemoryRail` 的任何生命周期钩子都没有调用它。会话结束时清理回调永远不会触发。

**修复方案**：在 `ExternalMemoryRail.Uninit()` 中调用 `provider.OnSessionEnd(ctx, messages)`，或在 `handleExternalMemoryRailByConfig` 的注销分支中调用。

---

### S-13 StreamEventRail 的 _ensure_json_arguments 三阶段 JSON 修复缺失

**Python 样例**（`stream_event_rail.py` L711-781）：
```python
def _ensure_json_arguments(self, arguments):
    # 阶段1: 尝试直接 json.loads
    try:
        json.loads(arguments)
        return arguments
    except json.JSONDecodeError:
        pass
    # 阶段2: json_repair 库修复
    try:
        repaired = json_repair.from_json(arguments)
        return json.dumps(repaired)
    except:
        pass
    # 阶段3: 规则修复 (_fix_missing_quotes)
    fixed = self._fix_missing_quotes(arguments)
    try:
        json.loads(fixed)
        return fixed
    except:
        return arguments  # 返回原始参数
```

**Go 问题**（`stream_event_context.go`）：`fixIncompleteToolContext` 中的 arguments 修复逻辑只有基础的 `json.Unmarshal`/`json.Marshal`，完全缺少规则修复和三阶段 fallback。不完整的 JSON 参数可能导致模型调用失败（尤其是 Windows 路径、缺失引号等常见 LLM 输出问题）。

**修复方案**：实现三阶段 fallback：①基础 JSON 解析 → ②规则修复（Windows 路径 `D:/path`、无引号字符串值、无引号 key）→ ③返回原始参数。

---

### S-14 StreamEventRail 的 _tool_interrupted_message 无语言感知

**Python 样例**（`stream_event_rail.py` L223-227）：
```python
def _tool_interrupted_message(self, tool_name, language):
    if language == "cn":
        return f"[工具执行被中断] 工具 {tool_name} 执行过程中被用户打断，没有执行结果。"
    return f"[Tool interrupted] Tool {tool_name} was interrupted by the user and has no result."
```

**Go 问题**：`fixIncompleteToolContext` 中硬编码了中文中断消息，没有根据 `getPromptLanguage()` 动态选择。英文模式下插入中文消息会导致上下文语言不一致。

**修复方案**：在 `fixIncompleteToolContext` 中调用 `getPromptLanguage()` 获取语言，根据语言返回不同的中断消息。

---

### S-15 StreamEventRail 的 _get_todo_tool 未初始化

**Python 样例**（`stream_event_rail.py` L593-624）：
```python
def _get_todo_tool(self, agent):
    if self._main_todo_tool is not None:
        return self._main_todo_tool
    workspace = agent._config.workspace
    language = agent._config.language
    self._main_todo_tool = TodoListTool(operation=..., workspace=..., language=...)
    return self._main_todo_tool
```

**Go 问题**：`todoToolLoader` 接口替代了 Python 的直接引用，但 `SetTodoTool` 在 `buildStreamEventRail` 中没有被调用——`_main_todo_tool` 始终为 nil，导致 `emitTodoUpdated` 永远不发射 `todo.updated` 事件。

**修复方案**：在 `DeepAdapter.CreateInstance` 的 rail 初始化阶段调用 `streamEventRail.SetTodoTool(loader)`，或在 `AfterToolCall` 中懒加载 TodoTool。

---

### S-16 agent_tool.go 中 context.Background() 丢弃 context

**Go 问题**（`agent_tool.go` L366）：
```go
subAgent, err := harness.CreateDeepAgent(context.Background(), params)
```

Python 中 `create_deep_agent()` 不需要显式 context，但 Go 的 `CreateDeepAgent` 需要 ctx。使用 `context.Background()` 意味着子 Agent 创建过程不受父 context 的超时/取消控制。如果父请求被取消，子 Agent 仍会完成创建。

**修复方案**：给 `createSubAgent` 方法添加 `ctx context.Context` 参数，并将 ctx 传递给 `CreateDeepAgent`。

---

### S-17 ObservabilityRail/MonitorHandler 使用 context.Background() 丢弃 parent context

**Go 问题**：
- `rail.go:72`：`r.tracer().Start(context.Background(), ...)` — task_iteration span 无 parent
- `monitor_handler.go:136`：`h.tracer().Start(context.Background(), "team."+teamName, ...)` — team span 无 parent
- `monitor_handler.go:180`：`h.tracer().Start(context.Background(), "task."+taskID, ...)` — task span 无 parent
- `callback_handler.go:385`：`trace.ContextWithSpan(context.Background(), state.Span)` — reasoning span parent 从 Background 开始

**Python 样例**：Python 的 `start_span` 会自动从当前 context 继承 parent span。

**修复方案**：
- Rail 的 `BeforeTaskIteration` 应从 `railCtx` 中提取 parent context：`parentCtx := trace.ContextWithSpan(ctx, parentSpan)`
- Monitor handler 需要传入带 parent 的 context，或使用 `trace.ContextWithSpan` 建立父子关系
- callback_handler.go L385 的 reasoning span 应使用 `trace.ContextWithSpan(ctx, state.Span)` 而非 `context.Background()`

---

### S-18 Observability AttachToTeamAgent/DetachFromTeamAgent 桩实现

**Go 问题**（`setup.go` L194-214）：
```go
func AttachToTeamAgent(teamAgent EventListenerRegistrar) {
    if monitorHandler == nil { return }
    // TODO(#9.55): 待 TeamAgent.add_event_listener 实现后回填
    logger.Info(logComponent).Msg("attach_to_team_agent 暂为桩实现")
}
```

**Python 样例**（`setup.py` L219-223）：
```python
def attach_to_team_agent(team_agent):
    if _monitor_handler is None: return
    team_agent.add_event_listener(_monitor_handler)
```

**修复方案**：确认 `EventListenerRegistrar.AddEventListener` 接口已可用后，实现 `AttachToTeamAgent` 和 `DetachFromTeamAgent`。Monitor handler 需要注册到 TeamAgent 才能消费团队事件。

---

### S-19 load_user_rails() 完全未实现

**Python 样例**（`interface_deep.py` 步骤 25）：
```python
await self.load_user_rails()  # 动态加载用户自定义的 Rail 扩展
```

**Go 问题**（`deep_adapter.go:579`）：标注 `⤵️ 10.6.3-10: 动态加载用户自定义的 Rail 扩展`。用户自定义 Rail 无法加载，功能完全缺失。

**修复方案**：实现 `loadUserRails()` 方法，扫描 workspace 中的 Rail 扩展并注册到 instance。优先级低于核心 Rail。

---

### S-20 ContextEvolutionRail 缺失 _pending_tools / _tools_applied 字段

**Python 样例**：
```python
class ContextEvolutionRail(EvolutionRail):
    def __init__(self, ...):
        self._pending_tools: List[Any] = []
        self._tools_applied: bool = False
    
    @property
    def pending_tools(self) -> List[Any]:
        return self._pending_tools
    
    @property
    def tools_applied(self) -> bool:
        return self._tools_applied
```

**Go 问题**：`ContextEvolutionRail` 完全缺失这两个字段及访问器。工具注册管理逻辑丢失。

**修复方案**：添加 `pendingTools []any` 和 `toolsApplied bool` 字段，以及对应的 `PendingTools()` 和 `ToolsApplied()` 访问器。

---

### S-21 TeamSkillCreateRail 缺失 _detect_used_team_skill 检查

**Python 样例**：
```python
def _can_enqueue_creation_follow_up(self, ctx):
    if self._detect_used_team_skill() is not None:
        return False  # 已有 team skill 在用，跳过创建提议
    ...
```

**Go 问题**：`canEnqueueCreationFollowUp` 完全缺失此检查。如果已有 team skill 在用，Go 仍会尝试创建新的 team skill，可能导致冲突。

**修复方案**：实现 `detectUsedTeamSkill` 方法，检查 harness 中是否已注册 team skill 类型的工具，并在 `canEnqueueCreationFollowUp` 中加入检查。

---

### S-22 Migration 缺少 5 个 Migrator + run_migrations

**Python 路径**：
- `openjiuwen/core/memory/migration/migrator/vector_migrator.py`
- `openjiuwen/core/memory/migration/migrator/kv_migrator.py`
- `openjiuwen/core/memory/migration/migrator/sql_migrator.py`
- `openjiuwen/core/memory/migration/migrator/message_migrator.py`
- `openjiuwen/core/memory/migration/migrator/index_version_migrator.py`
- `openjiuwen/core/memory/migration/run_migrations.py`

**Go 问题**：`internal/agentcore/memory/migration/migrator/` 目录只有 `memory_meta_manager.go`，缺少所有具体 Migrator 实现和 `run_migrations.go`。迁移操作无法执行。

**修复方案**：按 IMPLEMENTATION_PLAN.md 7.22-7.23 逐步实现 Vector/KV/SQL/Message/IndexVersion 五个 Migrator 和 `RunMigrations` 函数。注意：IMPLEMENTATION_PLAN 中 7.22-7.23 标记为 ☐。

---

### S-23 entityFromMap 中 nil map 写入风险

**Python 样例**（`entity.py` model_validator）：
```python
@model_validator(mode="before")
def set_default_values(cls, values):
    if values.get("metadata") is None:
        values["metadata"] = {}
    if values.get("attributes") is None:
        values["attributes"] = {}
    return values
```

**Go 问题**：`entityFromMap` 反序列化时，如果 metadata/attributes 为 nil 则不设置默认空 dict，后续代码对 nil map 写入会 panic。

**修复方案**：在 `entityFromMap` 中添加：
```go
if e.Metadata == nil {
    e.Metadata = make(map[string]any)
}
if e.Attributes == nil {
    e.Attributes = make(map[string]any)
}
```

---

### S-24 containsTeamSkillKind 用字符串全文匹配可能误匹配

**Python 样例**：
```python
frontmatter.get("kind") in _TEAM_SKILL_KINDS  # 精确匹配 frontmatter 字段
```

**Go 问题**：`containsTeamSkillKind` 用 `strings.Contains(content, "kind: ...")` 做全文匹配，可能误匹配注释或其他内容中的 "kind:" 字符串。

**修复方案**：改为解析 frontmatter 后精确匹配 kind 字段值，或至少使用正则 `(?m)^kind:\s*(.+)$` 限制行首匹配。

---

### S-25 Observability otlp_http 导出器未实现

**Python 样例**（`setup.py` L67-71）：
```python
if config.exporter == "otlp_http":
    from opentelemetry.exporter.otlp.proto.http.trace_exporter import OTLPSpanExporter as HttpExporter
    return HttpExporter(endpoint=config.endpoint)
```

**Go 问题**（`setup.go` L266）：
```go
case "otlp_http":
    return nil, fmt.Errorf("otlp_http 导出器暂未实现，请使用 otlp_grpc 或 console")
```

**修复方案**：引入 `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp` 包，实现 otlp_http 导出器。

---

### S-26 _external_memory_rail_registered 去重标志缺失

**Python 样例**：DeepAdapter 有 `_external_memory_rail_registered` bool 标志，防止 plan/agent 模式切换时重复注册。

**Go 问题**：DeepAdapter 没有此标志。模式切换时可能重复注册 ExternalMemoryRail。

**修复方案**：在 `DeepAdapter` 结构体中添加 `externalMemoryRailRegistered bool` 字段，在 `handleExternalMemoryRailByConfig` 中使用。

---

### S-27 extractAssistantOutput 不支持非 dict result

**Python 样例**：
```python
if not isinstance(result, dict):
    return str(result) if result else ""
```

**Go 问题**：只处理 `InvokeInputs.Result` 为 `map[string]any` 的情况，如果 result 是 string 等其他类型则返回空字符串。

**修复方案**：添加对 string 类型的 fallback：`if s, ok := result.(string); ok { return s }`。

---

### S-28 StrictSchemaEnforce 中 Go 直接赋值 required 而 Python 用 setdefault

**Python 样例**：
```python
node.setdefault("required", list(property_field.keys()))
```

**Go 问题**：
```go
node["required"] = keys  // 始终覆盖
```

Python 的 `setdefault` 保留已存在的 `required`，Go 直接赋值会覆盖。

**修复方案**：改为 `if _, ok := node["required"]; !ok { node["required"] = keys }`。

---

### S-29 TeamCompletionHandler.OnPollTask 上升沿保护逻辑错误

**Python 样例**（`handlers/team_completion.py`）：
```python
async def on_poll_task(self, event):
    # 1. 检查 leader 角色 + agent 非忙碌
    if self._round.has_in_flight_round() or self._round.is_agent_running():
        self._team_completed_emitted = False  # falling edge 重置
        return
    # 2. 检查 is_team_completed
    completed = await self._is_team_completed()
    if completed and not self._team_completed_emitted:
        self._team_completed_emitted = True  # rising edge
        await self._host.conclude_completed_round(...)
```

**Go 问题**（`handlers/team_completion.go:100`）：
函数入口 `if h.teamCompletedEmitted { return }` 直接返回，**falling edge 永远不会重置标志**。如果团队从 completed 变回 not-completed 再变回 completed，Go 不会重新发出 TEAM_COMPLETED 事件。Python 在 `has_in_flight_round()` 或 `is_agent_running()` 为 True 时重置 `_team_completed_emitted = False`，允许后续重新触发。

**修复方案**：在 `OnPollTask` 中，当 `hasInFlightRound` 或 `isAgentRunning` 为 True 时，重置 `h.teamCompletedEmitted = false`。

---

### S-30 TaskBoardHandler.OnTaskClaimed Human-agent 自认领被丢弃

**Python 样例**（`handlers/task_board.py`）：
```python
if claim_member == member_name:
    if role == TeamRole.HUMAN_AGENT:
        content = hitt.task_assigned_to_self_human(...)
        await self._host.deliver_input(content)
    else:
        await self._poll.resume_polls()
```

**Go 问题**（`handlers/task_board.go`）：
条件 `claimMember == memberName && role != TeamRoleHumanAgent` 排除了 human-agent，随后 `if role == TeamRoleHumanAgent { return }` 静默丢弃。**Human-agent 永远收不到自己的任务分配通知**。

**修复方案**：当 `claimMember == memberName && role == TeamRoleHumanAgent` 时，走 `deliver_input` 路径通知 human-agent。

---

### S-31 MemoryPersistenceHelper.probeMilvus 逻辑缺陷

**Go 问题**（`persistence_helper.go:260`）：
```go
return h.milvusConnector.Exists(context.Background(), "__probe__") || true
```

`|| true` 导致 `Exists` 返回值被忽略，即使 Milvus 不可达也返回 true。并且 `Exists("__probe__")` 会在 Milvus 中执行查询，namespace 校验可能拒绝 `__probe__`。

**Python 样例**（`persistence.py:96-103`）：通过尝试创建 `MilvusConnector` 实例判断可达性（构造函数内部调用 `connections.connect`）。

**修复方案**：移除 `|| true`，改为检查 Exists 的实际返回值；或使用专用的 `ProbeReachable()` 方法。

---

### S-32 MemoryPersistenceHelper Save/Load 使用 context.Background()

**Go 问题**（`persistence_helper.go:142,156`）：
```go
h.milvusConnector.SaveToDB(context.Background(), ns, nodesDict)
h.milvusConnector.LoadFromDB(context.Background(), ns)
```

Milvus gRPC 调用无法被取消/超时，可能导致 goroutine 泄漏。Python 的 pymilvus 是同步阻塞的，不需要 context。

**修复方案**：Save/Load 方法应接受 context 参数，传入带超时的 ctx。

---

### S-33 callback_handler.go openLlmSpan 首次调用时清除 parent span

**Go 问题**（`callback_handler.go:331`）：
```go
parentCtx := trace.ContextWithSpanContext(ctx, spanState.CurrentLLMSpanContext())
```

首次 LLM 调用时 `CurrentLLMSpanContext()` 返回空 `SpanContext{}`，`trace.ContextWithSpanContext(ctx, SpanContext{})` 会**清除** ctx 中已有的 parent span，导致 span 成为根 span 而非继续现有 trace。

**Python 样例**：`otel_context.attach(set_span_in_context(span))` 自动从当前 context 继承 parent。

**修复方案**：当 `CurrentLLMSpanContext()` 为空时，应直接使用原始 ctx：
```go
sc := spanState.CurrentLLMSpanContext()
if sc.IsValid() {
    parentCtx = trace.ContextWithSpanContext(ctx, sc)
} else {
    parentCtx = ctx
}
```

---

### S-34 MilvusConnector.Search 缺少 search params（ef/metric_type）

**Python 样例**（`milvus_connector.py:458-462`）：
```python
search_params = {"metric_type": milvus_metric, "params": {"ef": max(top_k * 2, 64)}}
results = collection.search(data=vectors, anns_field="vector", param=search_params, limit=top_k)
```

**Go 问题**（`milvus_connector.go:424`）：`NewSearchOption` 未设置 HNSW search params `ef`（默认值可能不够，Python 用 `max(top_k * 2, 64)`），且未传递 `metric_type`。影响向量搜索召回率。

**修复方案**：
```go
opt := milvusclient.NewSearchOption(m.collectionName, topK, vectors).
    WithSearchParam("ef", fmt.Sprintf("%d", max(topK*2, 64))).
    WithANNSField("vector")
```

---

## 一般问题（M）：24 项

### M-01 CoordinationKernel.Setup 缺少 blueprint/infra nil 检查

**Python**：`if blueprint is None or infra is None: raise RuntimeError(...)`
**Go**：直接构造不检查。

**修复方案**：添加 nil 检查。

---

### M-02 CoordinationKernel.Setup 参数差异

**Python**：`setup(*, role)` 只传 role，blueprint/infra 从 `host.blueprint`/`host.infra` 获取。
**Go**：`Setup(role, bp, inf)` 额外传入 bp/inf 参数。

**修复方案**：统一为从 KernelHost 接口获取 bp/inf，减少参数传递。

---

### M-03 Dispatcher Human Agent 白名单过滤 ✅ 已对齐

**Python**（`dispatcher.py` L262-273）和 **Go**（`dispatcher.go`）均已实现 Human Agent 白名单过滤（7 个事件 + inner event 中 POLL_TASK/POLL_MAILBOX 过滤）。**确认对齐，无需修复。**

---

### M-04 Dispatcher stale_claim_throttle 共享 ✅ 已对齐

**Python**（`dispatcher.py` L174）和 **Go**（`dispatcher.go`）均已实现共享 `staleClaimThrottle` map。**确认对齐，无需修复。**

---

### M-05 extractAssistantOutput 缺少 warn 日志

**Python**：无法提取 assistant output 时记录 `logger.warning("[ExternalMemoryRail] Cannot extract assistant output...session_id=..., available_keys=...")`
**Go**：直接返回空字符串。

**修复方案**：添加 Warn 日志包含 session_id 和 available_keys。

---

### M-06 resolveUserTextForMemory warn 日志缺字段

**Python**：`logger.warning("Cannot resolve user text for memory. session_id=..., trace_id=..., has_query=..., has_messages=...")`
**Go**：仅记录 `input_kind`，缺少 `session_id`、`trace_id`、`has_query`、`has_messages` 字段。

**修复方案**：补充缺失字段。

---

### M-07 Provider 级 consecutive_failures 计数缺失

**Python**：AgentArtsMemoryProvider 内部维护 `_consecutive_failures` 计数。
**Go**：MemoryProvider 接口和 BaseMemoryProvider 都没有此字段。

**修复方案**：在具体 Provider 实现时补回。当前 ExternalMemoryRail 熔断器已在 rail 层面实现，影响有限。

---

### M-08 FormatExistingEntities 输出格式差异

**Python**：`template.format(i=i, **ent)` — Python format 对缺失 key 抛 KeyError
**Go**：`strings.ReplaceAll` 逐个替换，缺失 key 保留原始占位符。列表类型输出格式不同（Python `[uuid1, uuid2]`，Go `[uuid1 uuid2]` 无逗号）。

**修复方案**：对列表类型字段使用逗号分隔格式化。

---

### M-09 RelationDef.LHS/RHS 类型语义差异

**Python**：`lhs: type[EntityDef]` — 类引用
**Go**：`LHS *EntityDef` — 实例指针

**修复方案**：当前实现通过预构造子类实例（HumanEntity/AIEntity）对齐，功能等价。需确保子类实例的 Name 字段与 Python 子类的 name 属性一致。

---

### M-10 EntityOrDeclaration.EntityTypeID() 对 Entity 类型默认返回 0

**Go 问题**：
```go
func (e EntityOrDeclaration) EntityTypeID() int {
    if e.Decl != nil {
        return e.Decl.EntityTypeID
    }
    return 0  // Entity 类型时默认返回 0
}
```

Python 中 `Entity` 没有 `entity_type_id` 字段。当 `EntityOrDeclaration` 持有 Entity 时返回 0 可能导致 `ParseAllRelations` → `DeclareEntities` 中对该 Entity 赋予错误的 type_id。当前代码路径中 Entity 类型项直接使用 `item.Entity`（已有 UUID），不会调用 `declareSingleEntity`，影响有限。

**修复方案**：添加注释说明返回 0 的语义，或在 Entity 持有者中从 Entity 自身获取 type_id。

---

### M-11 ContextEvolutionRail auto_summarize 条件差异

**Python**：`if self.auto_summarize and self._current_query`
**Go**：`if r.autoSummarize && r.autoSummarizeMattsMode == "none" && r.currentQuery != "" && r.memoryService != nil`

Go 多了 `mattsMode` 和 `memoryService` 非空检查——更严格但更安全，Python 对非 "none" 模式注释说 "only support matts_mode = none"。

**修复方案**：Go 的实现更正确，保持不变但需在注释中说明差异原因。

---

### M-12 agent_tool 错误码不一致

**Python**：所有错误统一用 `StatusCode.TOOL_TASK_TOOL_INVOKED`
**Go**：用了三种不同错误码（`StatusAgentToolExecutionError`、`StatusToolTaskToolInvoked`、`StatusAgentToolNotFound`）

**修复方案**：统一为 `StatusToolTaskToolInvoked` 对齐 Python，或保持 Go 的细粒度分类但确保上层错误处理逻辑兼容。

---

### M-13 agent_tool tool_id 生成：空 agentID 时无随机后缀

**Python**：`tool_id = f"agent_tool_{agent_id}" if agent_id else f"agent_tool_{uuid.uuid4().hex}"`
**Go**：`toolID := fmt.Sprintf("agent_tool_%s", agentID)` — 空 agentID 时生成 `agent_tool_`（无随机后缀），可能导致 ID 冲突。

**修复方案**：空 agentID 时使用 UUID 后缀：`if agentID == "" { agentID = uuid.New().String()[:8] }`。

---

### M-14 deep_adapter_evolution.go 多处 context.Background()

**Go 问题**：`deep_adapter_evolution.go:253` 和 `:522/:536` 使用 `context.Background()`，这些是 evolution 触发和审批推送路径，应使用请求 context。

**修复方案**：从方法参数或结构体字段中获取 ctx 传递。

---

### M-15 DeepAdapter EnsurePersistentCheckpointer 使用 context.Background()

**Go 问题**（`deep_adapter.go:1634`）：SQLite 打开操作不受超时/取消控制。

**修复方案**：传入带超时的 context（如 `context.WithTimeout(ctx, 30*time.Second)`）。

---

### M-16 _FAILURE_KEYWORDS 正则差异

**Python**：`re.compile(r"error(?!\s*=\s*None)|exception|...", re.IGNORECASE)` — negative lookahead 排除 `error = None`
**Go**：`\berror\b` 无 negative lookahead

**修复方案**：实现 negative lookahead 等价逻辑，或在匹配后检查后续是否为 `= None`。

---

### M-17 processTeamMessageStream 空壳

**Go 问题**（`deep_adapter_team.go:164-168`）：标注 `⤵️ 10.3.7-11: team 模式分流`，Python 有完整实现。

**修复方案**：按 IMPLEMENTATION_PLAN 10.3.7-11 实现。

---

### M-18 pushTeamSkillEvolveResolutionStatus 仅记日志

**Go 问题**（`deep_adapter_team.go:119-127`）：只 `logger.Info`，没有实际的 stream 推送。Python 中通过 stream event 将审批结果推送给前端。

**修复方案**：实现 stream 推送逻辑。

---

### M-19 team_skill_create_rail.go NotifyTeamCompleted 签名差异

**Python**：`async def notify_team_completed(self, ctx=None) -> bool` 接受可选 ctx
**Go**：`NotifyTeamCompleted(cbc *AgentCallbackContext) bool` 直接传 cbc，不能为 nil

**修复方案**：确保所有调用方都传入非 nil cbc，或改为接收 `*AgentCallbackContext` 可以为 nil。

---

### M-20 RegisterSearchStrategy entity 默认 min_score 差异

**Python**：默认 `WeightedRankConfig()`（min_score 默认 0.3）
**Go**：默认 `newSearchConfigWithMinScore(0.02)`

**修复方案**：对齐 Python 默认值 0.3，或确认 Go 的 0.02 是有意的调优。

---

### M-21 EventBus Start 中 Human Agent 不启动 poll tasks ✅ 已对齐

**Python**（`event_bus.py`）和 **Go**（`event_bus.go`）均已实现 Human Agent 跳过 poll tasks（Go 用 `periodicPollEnabled` 标志）。**确认对齐，无需修复。**

---

### M-22 OtelSpanState 使用 context.Value 传播而非 Python 的 contextvars

**Python**：使用 `contextvars.ContextVar` 在 asyncio.Task 间自动传播。
**Go**：使用 `context.Value` 手动传播，需要确保每个回调路径都正确注入 OtelSpanState。

**修复方案**：确认 CallbackFramework 触发回调时是否将带 SpanState 的 ctx 传递给处理器。如果 ctx 丢失，span 栈会断裂。

---

### M-23 shutdown_observability 使用 UnregisterNamespace 而非逐个 unregister

**Python**（`setup.py` L119-124）：逐个 `(event, cb)` 调用 `framework.unregister_sync(event, cb)`，仅注销 observability 注册的回调。
**Go**（`setup.go:161`）：`fw.UnregisterNamespace(namespace)` 删除该 namespace 下**所有**回调，如果其他模块也注册了同 namespace 的回调，会被误删。

**修复方案**：改为逐个注销，与 Python 行为一致；或确认 namespace 是 observability 独占的。

---

### M-24 ObservabilityRail 缺少 Priority() 方法

**Python**（`rail.py:46`）：`priority: int = 10`
**Go**：未定义 `Priority()` 方法，默认使用 DeepAgentRail 的默认优先级（可能不为 10）。

**修复方案**：添加 `func (r *ObservabilityRail) Priority() int { return 10 }`。

---

### M-25 MilvusConnector.Count 全局计数效率低

**Python**（`milvus_connector.py:691`）：`return collection.num_entities`（快速缓存属性）
**Go**（`milvus_connector.go:540-548`）：用 `Query` 全表扫描统计，效率低。

**修复方案**：使用 Milvus SDK 的 `Query` 带 `count(*)` 表达式或 `GetCollectionStatistics` API 替代全表扫描。

---

### M-26 DispatcherInfra 为空接口

**Go 问题**（`types/protocols.go`）：`type DispatcherInfra interface{}` 没有任何方法，Python 的 `TeamInfra` 提供了 `task_manager`、`message_manager`、`team_backend`、`messager` 等大量依赖。handler 内部多处 TODO 标注"等接口就绪后补充"，根源在此。

**修复方案**：为 `DispatcherInfra` 添加必要的方法签名（`TeamBackend()`、`Messager()`、`TaskManager()`、`MessageManager()` 等），使 handler 可以通过接口访问依赖。

---

## 提示问题（T）：15 项

### T-01 StreamEventRail _stream_tasks 字段缺失

**Python**：`self._stream_tasks: set[asyncio.Task]` — 跟踪流式任务集合。
**Go**：完全缺失。Go 的并发模型不同（goroutine vs asyncio.Task），可能不需要此字段。

---

### T-02 ExternalMemoryRail forbidden.go 包位置差异

Python 中 forbidden 和 external_memory 同属 `jiuwenswarm/agents/harness/common/memory/`，Go 中 ExternalMemoryRail 在 `agentcore/harness/rails/memory/`，forbidden 在 `swarm/agents/harness/common/memory/`。不影响功能。

---

### T-03 缺少 _load_plugin_provider 插件发现机制

**Python**：`_load_plugin_provider()` 支持从 `~/.jiuwenswarm/plugins/memory/` 加载用户自定义 provider。
**Go**：无对应实现。P1 阶段可暂不实现。

---

### T-04 buildMemoryContextBlock 空内容时仍生成标签

Go `buildMemoryContextBlock("")` 生成 `<memory-context>...\n</memory-context>`，但 `injectMemoryContext` 会在 rawContext 为空时不注入。行为与 Python 一致。

---

### T-05 Dict2Relation 参数差异

**Python**：`**kwargs` 透传，**Go**：显式命名参数。功能等价，Go 更类型安全。

---

### T-06 asyncTask channel 化

Go 的 `sync.Once` + channel 正确对齐 Python `asyncio.Future` 语义。串行 Wait vs Python 的并行等待不影响正确性。

---

### T-07 EntityDefAttr Go 为纯 struct

Python 继承 `MultilingualBaseModel`（有 JSON Schema 生成），Go 为纯 struct。当前不影响功能。

---

### T-08 EntityOrDeclaration Go 用 tagged union

正确的 Go 惯用模式，与 Python Union 等价。

---

### T-09 _endpoint_to_entity_dict 差异

Go 内联实现，找不到时跳过而非抛异常。行为更宽容。

---

### T-10 pending_merge 类型差异

Go 的 `pendingMergeTask` 更复杂但功能等价。

---

### T-11 EnsureValidLanguage 参数类型

需确认 Go 的 `StorageConfig.Language` 字段是 int 还是 string。

---

### T-12 Go 独有 Rails

Go 添加了 Python 中不存在的 Rails（agentModeRail、mcpRail、progressiveToolRail、contextAssembleRail），可能是刻意设计差异。

---

### T-13 code_agent_rail.go Reload 使用 context.Background()

当前 `Init` 不使用 ctx，风险低。后续 Init 如需要 ctx 则需修复。

---

### T-14 RecoveryManager.PersistLeaderConfig JSON 序列化可能丢失字段

Go 使用 `json.Marshal → json.Unmarshal` 转换 struct 为 `map[string]any`，可能丢失非 JSON 标签字段。Python 的 `model_dump(mode="json")` 更可靠。

---

### T-15 Metadata 包 copyMap 仅浅拷贝

`copyMap` 只做一层浅拷贝。如果 teams bucket 的值是嵌套 map，修改可能影响原始数据。`MergeTeamNamespace` 中已做深拷贝 bucket，但 `readTeamsBucketMutable` 中的浅拷贝对一级 key-value 已足够。

---

## 问题统计

| 严重程度 | 数量 | 主要分布 |
|---------|------|---------|
| **严重（S）** | 34 | CoordinationKernel(7)、ExternalMemory(4)、StreamEventRail(4)、Observability(4)、DeepAdapter(2)、ContextEvolutionRail(1)、TeamSkillCreate(2)、Migration(1)、GraphMemory(3)、Entity(1)、agent_tool(1)、MilvusConnector(2)、callback_handler(1) |
| **一般（M）** | 24 | Coordination(2)、ExternalMemory(3)、Observability(4)、Evolution(4)、agent_tool(3)、DeepAdapter(3)、GraphMemory(2)、MilvusConnector(1)、DispatcherInfra(1) |
| **提示（T）** | 15 | 各模块零散 |

> 注：M-03/M-04/M-21 确认已对齐，不纳入问题计数。

---

## 优先修复建议（P0 - 必须立即修复）

1. **S-01~S-07**：CoordinationKernel 生命周期缺失——这是团队协作的核心框架，没有它 team 模式完全不可用
2. **S-29**：TeamCompletionHandler 上升沿保护逻辑错误——团队完成事件只触发一次，后续恢复后无法再触发
3. **S-30**：Human-agent 自认领任务被静默丢弃
4. **S-16**：agent_tool context.Background() — 子 Agent 创建不受取消控制
5. **S-17/S-33**：Observability span parent 丢失 — 首次 LLM 调用清除 parent + 多处 context.Background()
6. **S-13~S-15**：StreamEventRail JSON 修复/语言感知/todo 工具 — 前端事件链路不完整
7. **S-23**：entityFromMap nil map panic — 运行时崩溃
8. **S-31/S-32**：MemoryPersistenceHelper probe 逻辑缺陷 + Save/Load 丢 context

## 优先修复建议（P1 - 本迭代内修复）

9. **S-09~S-12**：ExternalMemoryRail 不可用（空壳 builder + 缺 config + 缺 session end）
10. **S-20~S-21/S-24**：ContextEvolutionRail/TeamSkillCreateRail 字段/方法/解析缺失
11. **S-18**：Observability AttachToTeamAgent 桩实现
12. **S-34**：MilvusConnector.Search 缺少 ef 参数，影响召回率
13. **S-22**：Migration 5 个 Migrator + run_migrations 缺失
14. **M-24/M-26**：ObservabilityRail Priority 缺失 + DispatcherInfra 空接口
15. **M-08, M-12, M-13**：输出格式/错误码/ID 生成差异
