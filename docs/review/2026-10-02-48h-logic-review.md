# 48 小时逻辑审查报告（2026-10-02）

> 审查范围：2026-10-01 ~ 2026-10-02 48小时内提交的代码
> 对应章节：9.55 TeamAgent、9.68-69 Team Rails/Prompts、9.38-49 Team Tools、7.18 LongTermMemoryExtractor、registry 包、ExternalMemoryRail、适配器重构、any 消除、staticcheck 修复

## 审查统计

| 分类 | 数量 | 说明 |
|------|------|------|
| 严重 (S) | 35 | 功能缺失/逻辑错误/panic 风险 |
| 一般 (M) | 44 | 行为偏差/错误处理缺失 |
| 提示 (T) | 30 | 日志/命名/风格/测试 |
| **总计** | **109** | |

---

## 一、9.55 TeamAgent 核心流程（15 严重 / 18 一般 / 8 提示）

### S-01 [严重] Invoke/Stream 核心流程完全未实现

Go 中 `Invoke` 和 `Stream` 方法仅有 streamQueue 创建，核心步骤全部 `⤵️` 占位跳过。

**Python 样例** (`team_agent.py:511-529`):
```python
async def invoke(self, inputs, session=None):
    self._stream_controller.stream_queue = asyncio.Queue()
    self._state.pending_user_query = inputs.get("query", "") if isinstance(inputs, dict) else str(inputs)
    await self._coordination.start(session)
    try:
        await self._coordination.enqueue_user_input(inputs)
        await self._coordination.enqueue_mailbox_after_first_iteration()
        last_result = None
        while True:
            chunk = await self._stream_controller.stream_queue.get()
            if chunk is None:
                break
            last_result = chunk
        return last_result
    finally:
        await self._coordination.finalize_round()
```

**Go 问题代码** (`team_agent.go:512-523`):
```go
func (a *TeamAgent) Invoke(...) (map[string]any, error) {
    if a.streamController != nil {
        a.streamController.streamQueue = make(chan stream.Schema, 64)
    }
    // ⤵️(#9.62): coordination.start(session) + 入队用户输入
    // ⤵️(#9.62): 从 streamQueue 读取直到 nil sentinel → coordination.finalize_round()
    return nil, nil
}
```

**修复方案**: 实现 Invoke/Stream 完整流程：缓存 pending_user_query → coordination.Start → EnqueueUserInput → EnqueueMailboxAfterFirstIteration → 从 streamQueue 循环读取 → FinalizeRound。

---

### S-02 [严重] 缺少 EnqueueMailboxAfterFirstIteration 方法

Python 的 `CoordinationKernel` 有 `enqueue_mailbox_after_first_iteration()` 方法（kernel.py:391-401），在 invoke/stream 中紧随 enqueue_user_input 之后调用。Go 侧 CoordinationKernel 完全缺失此方法。

**Python 样例** (`kernel.py:391-401`):
```python
async def enqueue_mailbox_after_first_iteration(self) -> None:
    host = self._host
    if host.role == TeamRole.LEADER:
        return
    gate = host.resources.first_iter_gate
    if gate is None or self._event_bus is None:
        return
    await gate.wait()
    await self._event_bus.enqueue(
        InnerEventMessage(event_type=InnerEventType.POLL_MAILBOX),
    )
```

**修复方案**: 在 CoordinationKernel 上添加 `EnqueueMailboxAfterFirstIteration()` 方法，对齐 Python 逻辑。

---

### S-03 [严重] 缺少 FinalizeRound 方法

Python 的 `CoordinationKernel.finalize_round()` 在 invoke/stream 的 finally 块中调用，负责 `memory_manager.extract_after_round()` 和清空 stream_queue。Go 侧完全缺失。

**Python 样例** (`kernel.py:421-434`):
```python
async def finalize_round(self) -> None:
    host = self._host
    memory_manager = host.resources.memory_manager
    if memory_manager:
        await memory_manager.extract_after_round()
    host.stream_controller.stream_queue = None
```

**修复方案**: 在 CoordinationKernel 上添加 `FinalizeRound()` 方法。

---

### S-04 [严重] 缺少 TeamCompletion callback 注册逻辑

Python `TeamAgent._setup_agent()` 末尾调用 `_register_team_completion_callbacks()`，查找 TeamSkillEvolutionRail/TeamSkillCreateRail 并注册 completion callback 到 TeamCompletionHandler。Go 侧 Configure 完全缺失此步骤。

**Python 样例** (`team_agent.py:447-467`):
```python
def _register_team_completion_callbacks(self) -> None:
    harness = self._configurator.harness
    dispatcher = self._coordination.dispatcher
    if harness is None or dispatcher is None:
        return
    for rail_type in (TeamSkillEvolutionRail, TeamSkillCreateRail):
        for rail in harness.find_rails(rail_type):
            notify_team_completed = getattr(rail, "notify_team_completed", None)
            if notify_team_completed is not None:
                dispatcher.team_completion.register_completion_callback(notify_team_completed)
```

**修复方案**: 在 TeamAgent.Configure 末尾添加对 TeamSkillRail 的查找与 completion callback 注册。

---

### S-05 [严重] CoordinationKernel.Setup 传 nil blueprint/infra

Python `CoordinationKernel.setup()` 在 blueprint 或 infra 为 None 时抛出 RuntimeError。Go 侧 Configure 调用 `a.coordination.Setup(runtimeCtx.Role, nil, nil)` 传了 nil，导致 Dispatcher 无法正常工作。

**Python 样例** (`kernel.py:55-69`):
```python
def setup(self, *, role: TeamRole) -> None:
    host = self._host
    blueprint = host.blueprint
    infra = host.infra
    if blueprint is None or infra is None:
        raise RuntimeError("CoordinationKernel.setup() requires configured blueprint and infra")
```

**Go 问题代码** (`team_agent.go:503`):
```go
a.coordination.Setup(runtimeCtx.Role, nil, nil)
```

**修复方案**: 在 SetupInfra 之后调用 `a.coordination.Setup(role, a.Blueprint(), a.Infra())`，传入实际 blueprint 和 infra。

---

### S-06 [严重] Pause 缺少 3 个 Leader 步骤

Go 侧 `CoordinationKernel.Pause()` 的 Leader 路径仅有 TODO 占位，缺少 `CancelRecoveryTasks`、`ShutdownAllHandles`、`persistLifecycleState("paused")`、发布 TEAM_STANDBY 事件。

**修复方案**: 在 KernelHost 接口添加 `CancelRecoveryTasks()` 和 `ShutdownAllHandles()` 方法，实现 persistLifecycleState 和 publishTeamStandby，在 Pause 中调用。

---

### S-07 [严重] Stop 缺少 cancel_recovery_tasks 和 shutdown_all_handles

同 S-06，Python `CoordinationKernel.stop()` 在 unsubscribe_transport 之后调用 `cancel_recovery_tasks()` 和 `shutdown_all_handles()`，Go 侧仅有 TODO。

**修复方案**: 同 S-06，补充接口方法和实现。

---

### S-08 [严重] MarkLiveTeammates 过滤逻辑不对齐

Python `_mark_live_teammates` 只标记 `spawned_handles` 中存在的成员，且跳过 UNSTARTED 和 SHUTDOWN 以及 leader 自身。Go 侧遍历所有成员且只跳过 SHUTDOWN/SHUTDOWN_REQUESTED。

**Python 样例** (`kernel.py:232-276`):
```python
async def _mark_live_teammates(self, target_status: MemberStatus) -> None:
    spawned = set(host.spawn_manager.spawned_handles.keys())
    for member in members:
        if member.member_name == leader:
            continue
        if member.member_name not in spawned:
            continue
        current = MemberStatus(member.status)
        if current in {MemberStatus.UNSTARTED, MemberStatus.SHUTDOWN}:
            continue
```

**修复方案**: 通过 SpawnManager.SpawnedHandles() 获取已生成成员集合，仅对这些成员操作，并跳过 leader 和 UNSTARTED/SHUTDOWN。

---

### S-09 [严重] RecoverForExistingSession 缺少 StopCoordination 调用

Python 先调用 `await self._stop_coordination()` 再进入 session_manager。Go 直接调用 SessionManager 不先停止协调。

**Python 样例** (`team_agent.py:853-862`):
```python
async def recover_for_existing_session(self, session) -> None:
    await self._stop_coordination()
    await self._session_manager.recover_for_existing_session(session)
```

**修复方案**: 在 RecoverForExistingSession 中先调用 `a.StopCoordination(ctx)`。

---

### S-10 [严重] SubscribeTransport 实现严重简化

Python `subscribe_transport` 实现了完整的 TeamTopic 订阅、self-filter 回调和 direct_message_handler 注册。Go 侧使用了 no-op handler，缺少：注册 direct_message_handler、遍历 TeamTopic 订阅、self-filter 回调。

**Python 样例** (`kernel.py:330-356`):
```python
async def subscribe_transport(self, team_name: str) -> None:
    async def _filter_self(event: EventMessage) -> None:
        for listener in host.state.event_listeners:
            await listener(event)
        if local_member_name and event.sender_id == local_member_name:
            return
        await self._event_bus.enqueue(event)
    await messager.register_direct_message_handler(self._event_bus.enqueue)
    for topic in TeamTopic:
        topic_str = topic.build(session_id, team_name)
        await messager.subscribe(topic_str, _filter_self)
        self._subscribed_topics.append(topic_str)
```

**修复方案**: 对齐 Python 实现，注册 direct_message_handler，遍历 TeamTopic 订阅，添加 self-filter 回调和 event_listeners 通知。

---

### S-11 [严重] UnsubscribeTransport 实现严重简化且使用 context.Background()

Python 需要取消注册 direct_message_handler 并逐个 unsubscribe 所有已订阅 topics，然后清空。Go 仅调用一个 `mgr.Unsubscribe(context.Background(), ...)`。

**修复方案**: 对齐 Python，取消注册 direct_message_handler，逐个取消 topics 订阅，清空 subscribedTopics。传递上层 ctx 而非 context.Background()。

---

### S-12 [严重] ShutdownSelf 缺少 CloseStream 调用

Python `shutdown_self()` 在更新状态后调用 `self._close_stream()` 关闭 streamQueue（写入 nil sentinel），Go 侧缺失。

**Python 样例** (`team_agent.py:613-626`):
```python
async def shutdown_self(self) -> None:
    await self._stream_controller.cooperative_cancel()
    if self._state.team_member is not None:
        await self._state.team_member.update_status(MemberStatus.SHUTDOWN)
    self._close_stream()
```

**修复方案**: 在 ShutdownSelf 末尾调用 `a.CloseStream()` 或通过 coordination 关闭 stream。

---

### S-13 [严重] LookupHumanAgentRuntime 缺少 is_human_agent 检查

Python 先检查 `backend.is_human_agent(member_name)` 再查找。Go 直接查找。

**Python 样例** (`team_agent.py:268-280`):
```python
def lookup_human_agent_runtime(self, member_name: str) -> Optional["TeamAgent"]:
    backend = self._configurator.team_backend
    if backend is None or not backend.is_human_agent(member_name):
        return None
    return self._spawn_manager.lookup_inprocess_agent(member_name)
```

**修复方案**: 添加 `backend.IsHumanAgent(memberName)` 检查。

---

### S-14 [严重] Broadcast 缺少 RuntimeError 和 UserInbox 对齐

Python 使用 `UserInbox(backend.message_manager).broadcast(content)` 实现。Go 侧直接使用 MessageManager 而非 UserInbox，缺少 HITT 交互逻辑。

**修复方案**: 对齐 Python 实现 UserInbox 或确保当前简化实现的语义正确。

---

### S-15 [严重] HumanAgentSay 缺少 RuntimeError 和 HumanAgentInbox

Python 使用 `HumanAgentInbox(backend, backend.message_manager).send(content, to=to, sender=sender)` 实现。Go 侧直接使用 MessageManager，缺少 HITT 交互和外部用户通知逻辑。

**修复方案**: 对齐 Python 实现 HumanAgentInbox 逻辑。

---

### M-01 [一般] UpdateStatus 缺少 TeamMember 优先路径

Python 通过 `self._state.team_member.update_status(status)` 更新，Go 直接操作 DB。应优先通过 TeamMember handle 更新状态。

### M-02 [一般] DeliverInput 缺少 pending_input 日志

Python 在 `has_in_flight_round` 时记录 Info 日志（含 content preview），Go 无日志。

### M-03 [一般] OnTeammateUnhealthy 使用 context.Background()

**Go 问题代码** (`spawn_manager.go:290`):
```go
CleanupTeammate(context.Background(), memberName)
```
应传播上游 context。

### M-04 [一般] SpawnInprocess 使用 context.Background()

**Go 问题代码** (`spawn_manager.go:450`):
```go
teammate.Configure(context.Background(), *spec, ctx)
```
应使用传入的 ctx。

### M-05 [一般] PublishRestartEvent 为 no-op（标记 TODO #9.65）

Python 通过 Messager 发布 MemberRestartedEvent，Go 为 no-op。

### M-06 [一般] BuildContextFromDB 缺少 messager_config 和 model_ref 解析

Python 解析 `model_ref_json` 获取 member_model，并设置 `messager_config`。Go 都缺失。

### M-07 [一般] PersistAllocatorState 为空操作

Python 委托给 `recovery_manager.persist_allocator_state`，Go 为 TODO 空操作。

### M-08 [一般] DrainAgentTask 硬编码 30 秒超时

Python 通过 `stream_controller.drain_agent_task()` 真正等待。Go 用 `time.After(30s)` 硬编码。

### M-09 [一般] SetMemberID 为 no-op

Python 通过 `set_member_id(member_name)` 设置 contextvar，Go 仅记录日志。

### M-10 [一般] SetupInfra 缺少 Messager 创建

Python 在 setup_infra 中 `create_messager(messager_config)`，Go 为 TODO。

### M-11 [一般] SetupInfra 缺少 Leader 模型分配器构建

Python 在 `role == LEADER && model_allocator is None` 时构建 `build_model_allocator(spec, team_spec)`，Go 仅有 `_ = ctx.Role` 占位。

### M-12 [一般] SetupInfra 缺少 SetupTeamBackend 调用

Python setup_infra 末尾调用 `setup_team_backend(spec, ctx, self.messager, ...)`，Go 注释说回填完成但实际未调用。

### M-13 [一般] SetupAgent 缺少 workspace 路径解析和 symlink 逻辑

Python 有 `ws_spec.stable_base` 路径解析 + symlink 逻辑、model_config 覆盖等，Go 缺失。

### M-14 [一般] SetupAgent 缺少 MemoryManager 构建

Python 在 setup_agent 末尾调用 `_build_memory_manager(...)` 并设置 `self.memory_manager`，Go 为 TODO。

### M-15 [一般] UnsubscribeTransport 使用 context.Background()

**Go 问题代码** (`team_agent.go:1101`):
```go
return mgr.Unsubscribe(context.Background(), a.TeamName())
```

### M-16 [一般] UpdateModelPool 缺少角色检查

Python 检查 `if self.role != TeamRole.LEADER: return`，Go 缺少。

### M-17 [一般] FromSpawnPayload 类型断言可能不安全

`specAny.(atschema.TeamAgentSpec)` 直接类型断言，payload 为 `map[string]any` 时会 panic。应使用安全断言或 JSON 反序列化。

### M-18 [一般] RecoverForSession 缺少 set_session_id 语义

Python 调用 `set_session_id(session.get_session_id())` 设置 contextvar。Go 通过 `state.SetSessionID()` 设置，但依赖 ctx 中的 SessionState 注入。

---

### T-01 [提示] SetupAgent 中 TeamPolicyRail 重复构造

当有 workspace_manager 时 TeamPolicyRail 被构造两次，应合并。

### T-02 [提示] TeamAgentTest 缺少有意义的集成测试

大量测试仅验证 nil/not-nil，未验证 Configure 后 TeamBackend、Messager 等是否正确设置。

### T-03 [提示] RemoveEventListener 切片删除未处理并发安全

### T-04 [提示] BuildTeamHarness 中 agentSpec 参数类型为 any

Python 传入具体 spec，Go 传入 any 且为 nil。

### T-05 [提示] SubscribeTransport 缺少 team_name 参数对齐

### T-06 [提示] EventBus.Start 使用 variadic session 参数，语义不清晰

### T-07 [提示] KernelHost 接口缺少 SpawnManager 访问方法

Python pause/stop 通过 `host.spawn_manager` 操作。

### T-08 [提示] RemoveSelfFromPool 缺少异常保护日志

Python 在 except 中记录 warning。

---

## 二、适配器/DeepAdapter/CodeAdapter（8 严重 / 7 一般 / 5 提示）

### S-16 [严重] CodeAdapter.CreateInstance 缺失 updateRuntimeConfig 调用

Go 中 DeepAdapter.CreateInstance 在 L556 调用 `d.updateRuntimeConfig(ctx, ...)` 设置 Language/Channel/Mode，但 CodeAdapter.CreateInstance 完全没有调用。导致 Code 模式下 RuntimePromptRail 7 个 setter 从未被初始化。

**修复方案**: 在 CodeAdapter.CreateInstance 步骤 21.2 之后加入 `c.updateRuntimeConfig(ctx, &runtimeConfig{...})`。

---

### S-17 [严重] CodeAdapter.updateRuntimeConfig 不被 ProcessMessageImpl 委托调用

`ProcessMessageImpl` 和 `ProcessMessageStreamImpl` 委托给 `c.deep.ProcessMessageImpl`，内部调用的是 `d.updateRuntimeConfig`（DeepAdapter 版本），而非 CodeAdapter 覆写版本。Python 通过继承自动走到子类版本。

**修复方案**: 在 DeepAdapter 中引入接口/函数指针（如 `runtimeConfigUpdater func(ctx, *runtimeConfig)` 字段），CodeAdapter 创建时覆写。

---

### S-18 [严重] ProcessMessageStreamImpl 缺少 StreamEventRail.ResetAbort 调用

DeepAdapter.ProcessMessageImpl 有 `d.streamEventRail.ResetAbort(sessionID)` 调用（步骤 15），但 ProcessMessageStreamImpl 缺失。流式模式下残留 abort 状态不会被清除。

**Python 样例**:
```python
if self._stream_event_rail:
    self._stream_event_rail.reset_abort(session_id)
```

**修复方案**: 在 ProcessMessageStreamImpl 的 markSessionActive 之后添加 ResetAbort 调用。

---

### S-19 [严重] isOutcomeEvent 类型断言 map[string]string 不匹配 map[string]any

`isOutcomeEvent` 中 `meta, ok := payload["_evolution_meta"].(map[string]string)` 断言为 `map[string]string`，但 `evolutionEventKindLocal` 中同一字段断言为 `map[string]any`。类型不匹配导致 isOutcomeEvent 永远返回 false，`watchEvolutionAndPush` 无法检测到 outcome 事件。

**Go 问题代码** (`deep_adapter_evolution.go:553`):
```go
meta, ok := payload["_evolution_meta"].(map[string]string)  // 错误
```

**修复方案**: 改为 `meta, ok := payload["_evolution_meta"].(map[string]any)`。

---

### S-20 [严重] CodeAdapter.CreateInstance 缺失 instance 属性设置

CodeAdapter 设置了 `c.uapswarmAdapterMode = "code"` 到自身字段，但没有调用 `d.instance.SetUapswarmAdapterMode("code")` 等设置到 DeepAgent 实例上。

**Python 样例** (`interface_code.py`):
```python
setattr(self._instance, "_jiuwenswarm_adapter_mode", "code")
```

**修复方案**: 在 CreateInstance 中增加对 `c.deep.instance` 的 SetUapswarm* 调用。

---

### S-21 [严重] handleEvolutionApproval 使用 context.Background() 丢弃上游 ctx

**Go 问题代码** (`deep_adapter_evolution.go:253`):
```go
ctx := context.Background()
```

**修复方案**: 将 `handleEvolutionApproval` 签名改为接收 `ctx context.Context`，从 `HandleUserAnswer` 传入 ctx。

---

### S-22 [严重] pushEventToFrontend 使用 context.Background() 丢弃上游 ctx

**Go 问题代码** (`deep_adapter_evolution.go:522,536`):
```go
globalSendPushFunc(context.Background(), msg)
```

**修复方案**: 将 ctx 传入 `pushEventToFrontend`，使用传入的 ctx。

---

### S-23 [严重] processTeamMessageStream 从 inputs 而非 req 解析参数

**Go 问题代码** (`deep_adapter_team.go:182-189`):
```go
inputs["params"]  // map[string]any
inputs["conversation_id"]
```

Python 中从 `request` 对象获取 `session_id`（`request.session_id`），而非从 `inputs`。Go 的 `req` 类型为 `any`，无法直接访问字段。

**修复方案**: 将 `req` 类型改为 `*schema.AgentRequest`，从中正确解析 sessionID 和参数。

---

### M-19 [一般] ProcessMessageStreamImpl 中 ParseParams 提取 query 仅用于 slash 命令

与 ProcessMessageImpl 不一致，query 未存储到局部变量。

### M-20 [一般] CodeAdapter.ReloadAgentConfig 未同步 CodeAdapter 自身字段

委托 `c.deep.ReloadAgentConfig` 后仅额外调用 `CodeAgentRail.Reload`，但 `uapswarmCodeProjectDir` 等未同步更新。

### M-21 [一般] ensureEvolutionRailForSlash 错误信息不准确

错误信息 "agent 模式下演进功能不可用" 应为 "当前模式下演进功能不可用，仅 agent.plan 模式支持"。

### M-22 [一般] processTeamMessageStream 中 teamName 提取路径不正确

`inputs["params"]` 不一定是 `map[string]any`。

### M-23 [一般] CodeAdapter.buildCodeAgentRails 中 SkillEvolutionRail 未处理

code 模式下 SkillEvolutionRail 相关 slash 命令不可用。

### M-24 [一般] CodeAdapter.CreateInstance 缺少 loadUserRails 调用

DeepAdapter 有 `d.loadUserRails()`，CodeAdapter 未调用。

### M-25 [一般] DeepAdapter.ReloadAgentConfig 缺失 updateRailsForMode 调用

---

### T-09 [提示] syncToolGroup 是空壳实现（⤵️ 10.6.24）

### T-10 [提示] buildImageGenModelConfig 未调用 applyImageGenModelConfigFromYAML（⤵️ 10.6.24）

### T-11 [提示] CodeAdapter buildLspRail 返回 nil 占位（⤵️ 10.6.3-10）

### T-12 [提示] DeepAdapter struct 中多个 any 类型字段待具体化

### T-13 [提示] CodeAdapter.CreateInstance 中 agent_history 路径修正未实现（⤵️ 10.6.3-10）

---

## 三、Team Tools 9.38-49（8 严重 / 12 一般 / 4 提示）

### S-24 [严重] SpawnMemberTool human_agent 路径漏检 prompt

Python 同时校验 model_name 和 prompt，Go 只检查了 model_name。

**Python 样例** (`team_tools.py:365`):
```python
if inputs.get("model_name") or inputs.get("prompt"):
    return ToolOutput(success=False, error="role_type='human_agent' does not accept 'model_name' or 'prompt'; ...")
```

**Go 问题代码** (`team_tools.go:646-649`):
```go
if _, ok := inputs["model_name"]; ok {
    return toolError("role_type='human_agent' does not accept 'model_name' or 'prompt'; ...")
}
```

**修复方案**: 增加 `prompt` 检查：
```go
if _, ok := inputs["model_name"]; ok || roleType == "human_agent" {
    if _, ok2 := inputs["prompt"]; ok2 {
        return toolError(...)
    }
}
```

---

### S-25 [严重] SpawnMemberTool inputs["prompt"].(string) 裸断言会 panic

`prompt` 是可选参数（Schema 中 `required: false`），当 LLM 不传时 `nil.(string)` 会 panic。

**Python 样例** (`team_tools.py:398`):
```python
prompt=inputs.get("prompt"),  # Optional[str]
```

**Go 问题代码** (`team_tools.go:672`):
```go
inputs["prompt"].(string),  // panic if nil
```

**修复方案**: 改为安全断言：
```go
prompt, _ := inputs["prompt"].(string)
```

---

### S-26 [严重] ViewTaskTool claimable 未传 status=PENDING 过滤

Go 的 `claimable` 和 `list` 走了完全相同的代码路径，Python 中 `claimable` 传入 `status=PENDING` 只显示待认领任务。

**Python 样例** (`team_tools.py:792-795`):
```python
if action == "claimable":
    result = await self.task_manager.list_tasks_with_deps(
        status=TaskStatus.PENDING.value,
    )
```

**Go 问题代码** (`team_tools.go:923-927`):
```go
if action == "claimable" {
    summaries, err = t.taskManager.ListTasksWithDeps(ctx)
} else {
    summaries, err = t.taskManager.ListTasksWithDeps(ctx)
}
```

**修复方案**: `ListTasksWithDeps` 增加 `status` 参数，claimable 路径传 `"pending"`。

---

### S-27 [严重] ViewTaskTool list 未使用 inputs["status"] 过滤

同 S-26，Go 完全忽略了 `inputs["status"]`。

**Python 样例** (`team_tools.py:797-799`):
```python
else:
    result = await self.task_manager.list_tasks_with_deps(
        status=inputs.get("status"),
    )
```

**修复方案**: list 路径传入 `inputs["status"]` 过滤。

---

### S-28 [严重] ListMembersTool 返回字段不完整

Python 的 `model_dump()` 返回完整字段（包括 `role`、`mode`、`execution_status`），Go 只返回 `member_name`、`display_name`、`status` 三个字段。

**Python 样例** (`team_tools.py:585`):
```python
data={"members": [member.model_dump() for member in members], "count": len(members)}
```

**修复方案**: 补全返回字段，至少包含 `role_type`、`mode`、`execution_status`。

---

### S-29 [严重] SubmitPlanTool 缺少 message 字段

Python 返回 `"message": "Member plan submitted. Wait for leader approval before execution."`，Go 缺失。LLM 无法理解后续行为。

**Python 样例** (`task_manager.py:1007-1015`):
```python
return {
    "success": True,
    "task_id": task_id,
    "plan_id": plan_id,
    "status": TaskStatus.CLAIMED.value,
    "member_plan_md": str(member_plan_path),
    "leader_message_id": leader_message_id,
    "message": "Member plan submitted. Wait for leader approval before execution.",
}
```

**修复方案**: 在 resultMap 中增加 `"message"` 字段。

---

### S-30 [严重] SendMessageTool multicast 缺少全员覆盖拒绝逻辑

Python 当 multicast 目标集合恰好覆盖除自身外的所有成员时拒绝并引导使用 `to='*'` 广播，Go 允许低效用法。

**Python 样例** (`team_tools.py:1384-1393`):
```python
roster = {member.member_name for member in await self._team.list_members()}
if roster and set(deduped) == roster:
    return ToolOutput(success=False, error="Multicast targets cover every other team member; use to='*' to broadcast instead ...")
```

**修复方案**: 在 multicast 方法中增加全员覆盖检查。

---

### S-31 [严重] CancelMember 返回值被忽略

Python `cancel_member` 返回 `bool`，Go 返回 `MemberOpResult` 但调用方完全忽略返回值。

**Go 问题代码** (`team_tools.go:1057,1076`):
```go
team.CancelMember(ctx, *task.Assignee)  // 返回值被忽略
```

**修复方案**: 至少在关键调用路径检查返回值并记录日志。

---

### M-26 [一般] MemberCompleteTaskTool assignee nil 时缺 `<unassigned>` 标识

Python 显示 `"<unassigned>"`，Go 显示空字符串 `""`。

### M-27 [一般] UpdateTaskTool 错误消息硬编码未走 i18n

Python 使用 `self.t("update_task", "error_human_agent_locked_cancel")` 翻译键，Go 使用硬编码英文。

### M-28 [一般] BuildTeamTool ErrHITTConfigInvalid 错误消息不友好

将内部 Go error 原样暴露给 LLM。

### M-29 [一般] SpawnHumanAgent prompt 空字符串 vs NULL

Python `prompt=None`，Go 传空字符串，DB 层可能区分。

### M-30 [一般] ViewTaskTool get 返回的 blocked_by/blocks 格式不对齐

Python 返回 task_id 字符串列表，Go 返回完整 taskBrief map。

### M-31 [一般] ClaimTaskTool Claim/Complete error 被吞

**Go 问题代码** (`team_tools.go:1126-1136`):
```go
result, _ := t.taskManager.Claim(ctx, taskID)
result, _ := t.taskManager.Complete(ctx, taskID)
```

### M-32 [一般] UpdateTaskTool Reset/Assign error 被吞

**Go 问题代码** (`team_tools.go:1018-1023`):
```go
resetResult, _ := t.agentTeam.TaskManager().Reset(ctx, taskID)
assignResult, _ := t.agentTeam.TaskManager().Assign(ctx, taskID, assignee)
```

### M-33 [一般] cancelMemberIfClaimed Get error 被吞

```go
task, _ := team.TaskManager().Get(ctx, taskID)
```

### M-34 [一般] cancelClaimedMembers ListTasks error 被吞

```go
claimedTasks, _ := t.agentTeam.TaskManager().ListTasks(ctx, "claimed")
```

### M-35 [一般] KvPrefixRegistry 使用 map[string]bool 而非 map[string]struct{}

Go 惯用的 set 实现是 `map[string]struct{}`，与项目其他代码风格不一致。

### M-36 [一般] DistributedLock.Acquire 中 lockValue 在循环外生成

Python `async with` 每次尝试应生成新 UUID，Go 只在循环前生成一次。

### M-37 [一般] TeamPolicyRail.membersCache.Refresh 使用 context.Background() 遮蔽传入 ctx

**Go 问题代码** (`team_policy_rail.go:160`):
```go
ctx := context.Background()  // 遮蔽了外层 ctx
```

**修复方案**: 删除此行，直接使用函数参数 `ctx`。

---

### T-14 [提示] index.json 是测试残留

整个文件是测试数据，不应作为源码提交。

### T-15 [提示] CAS 错误消息对 LLM 不友好

Go: "CAS 状态转换失败"，Python: "Database rejected status update"。

### T-16 [提示] generateTaskID 使用时间戳而非 UUID

Python 用 `uuid.uuid4()`，Go 用毫秒时间戳，高并发可能碰撞。

### T-17 [提示] plan_id 生成缺少 _safe_token 处理

Python 的 plan_id 经过 `_safe_token`（正则替换+截断 96 字符），Go 未做。

---

## 四、ExternalMemoryRail / registry / providers（11 严重 / 12 一般 / 7 提示）

### S-32 [严重] ExternalMemoryRail.AfterInvoke goroutine 中 ctx 可能已取消

goroutine 中 `SyncTurn` 使用的是请求传入的 ctx，可能随请求结束而取消。Python 中 asyncio.Task 不受请求 ctx 影响。

**Python 样例**:
```python
self._sync_task = asyncio.create_task(_serialized_sync())
# asyncio.Task 在独立协程中执行，不受请求 context 影响
```

**Go 问题代码** (`external_memory_rail.go:408-413`):
```go
go func() {
    defer close(done)
    err := r.provider.SyncTurn(ctx, query, output, ...)  // ctx 可能已取消
```

**修复方案**: 使用 `context.WithoutCancel(ctx)` (Go 1.21+) 或 `context.Background()` + 超时隔离请求生命周期。

---

### S-33 [严重] TeamRuntimeManager.handleInteractiveInput 使用 context.Background()

**Go 问题代码** (`manager.go:272`):
```go
err := entry.Agent.ResumeInterrupt(context.Background(), input)
```

**修复方案**: 改用 `ctx` 参数（`handleInteractiveInput` 签名已有 ctx，但未接收上层 Interact 的 ctx）。

---

### S-34 [严重] TeamRuntimeManager.resolveRecipients 使用 context.Background()

**Go 问题代码** (`manager.go:297-298`):
```go
ctx := context.Background()
memberExists := func(name string) (bool, error) {
    member, _ := backend.GetMember(ctx, name)
```

**修复方案**: `resolveRecipients` 应接收 ctx 参数，从 `Interact` 传递下来。

---

### S-35 [严重] PoolAccessor/PoolEntry 接口方法不完整

Python `TeamRuntimeManager.pool` 暴露完整 `TeamRuntimePool`，Go 的 `PoolEntry` 接口仅定义 `GetEntry` 和 `RemoveEntry`，缺少 `HasActive`、`ListTeamNames`、`TeamsForSession` 等方法。agent 包需要类型断言回 `*TeamRuntimePool`，打破编译期类型安全。

**修复方案**: 在 `PoolEntry` 接口中添加 `HasActive(teamName string) bool`、`ListTeamNames() []string`、`TeamsForSession(sessionID string) []PoolTeamEntry` 方法。

---

### S-36 [严重] OpenVikingProvider.Initialize 健康检查失败返回 error，Python 静默

Python `initialize` 在 health 失败时仅 warning + `self._client = None`，不抛异常。Go 返回 error 导致 `initialized` 不会被设为 true，所有后续 prefetch/sync_turn 永远跳过。

**Python 样例** (`openviking_memory_provider.py`):
```python
if not healthy:
    logger.warning("OpenViking at %s not reachable", self._endpoint)
    self._client = None  # 静默，不抛异常
```

**Go 问题代码** (`viking_provider.go:256-262`):
```go
if !healthy {
    return fmt.Errorf("OpenViking at %s not reachable", p.endpoint)
}
```

**修复方案**: 对齐 Python——health 失败时 warning + 设 client=nil，返回 nil error。Provider 标记 `initialized = true`，后续 Prefetch/SyncTurn 检查 `client == nil` 自然跳过。

---

### S-37 [严重] CodingMemoryRail.autoRecall 截断时 rune 数 vs 字节数单位不匹配

`remaining` 是字节数，但截断判断使用 `utf8.RuneCountInString(truncated) > remaining`（rune 数），两者单位不匹配。

**Python 样例**:
```python
body_bytes = len(body.encode("utf-8"))  # 字节数
remaining = maxRecallTotalBytes - totalBytes  # 字节数
body = body[:remaining]  # Python 字符串切片按字符，但 body 是 encode 后切片再 decode
```

**Go 问题代码** (`coding_memory_rail.go:576-588`):
```go
remaining := maxRecallTotalBytes - totalBytes  // 字节数
if utf8.RuneCountInString(truncated) > remaining {  // rune 数 vs 字节数
    runes := []rune(truncated)
    truncated = string(runes[:remaining]) + "\n\n... (truncated)"
```

**修复方案**: 截断判断应使用 `len(truncated) > remaining`（字节数对比），截断时用 rune 确保不破坏 UTF-8。

---

### M-38 [一般] ExternalMemoryRail.isBackgroundRun 缺少 RunKind 字符串比较

Python 还检查 `isinstance(run_kind, str)` + 字符串比较。Go 只检查 `IsHeartbeat()` 和 `IsCron()` 方法。

### M-39 [一般] OpenVikingProvider.Prefetch 忽略 opts 参数

签名接收 `opts ...ProviderOption` 但用 `_` 忽略，如果上层传了 `WithTopK` 不会生效。

### M-40 [一般] Mem0Provider.Initialize 忽略 ctx 参数

合理（对齐 Python），但创建 client 时无法做超时控制。

### M-41 [一般] Mem0Provider.QueuePrefetch 未取消旧 prefetch 的 WaitGroup 计数

连续快速调用可能导致多个 goroutine 同时存在。

### M-42 [一般] OpenVikingProvider.Shutdown 忽略 ctx 参数

`client.close()` 是本地操作几乎不会阻塞，忽略合理。

### M-43 [一般] ExternalMemoryRail.registerProviderTools 中 GetTool error 被吞

**Go 问题代码** (`external_memory_rail.go:602`):
```go
existing, err := resourceMgr.GetTool([]string{pt.Card().ID})
if err != nil || len(existing) == 0 {
```
应区分 `err != nil` 和 `len(existing) == 0`，对 `err != nil` 记录 warning。

### M-44 [一般] TeamRuntimeManager.Interact 中 OperatorMessage 分支 Agent 为 nil 时无 warning

### M-45 [一般] Mem0Provider.SyncTurn 签名忽略 opts 参数

`WithUserID/WithScopeID/WithSessionID` 被忽略，使用构造时的 user_id/agent_id。对齐 Python 行为。

### M-46 [一般] unwrapResults 中 dict 格式和 list 格式可能误判

空 JSON `{}` 也会 unmarshal 成 `mem0SearchResponse{Results: nil}`，返回空切片而非继续尝试 list 格式。

### M-47 [一般] CodingMemoryRail.BeforeModelCall 缺少 language fallback 对齐

### M-48 [一般] ExternalMemoryRail.AfterInvoke 缺少对 syncDone channel 异常处理

Python 有 `except Exception as exc` 分支，Go 只检查超时。

### M-49 [一般] Mem0Provider.SyncTurn 吞错返回 nil — Rail 层熔断器检测失效

双层熔断器架构问题：内层熔断器 `recordFailure()` 后返回 nil，外层 Rail 层的 `err != nil` 分支永远不触发。需确认设计意图。

---

### T-18 [提示] ExternalMemoryRail.cbcRefID 使用 reflect.ValueOf 模拟 id(ctx)

添加注释说明即可。

### T-19 [提示] CodingMemoryRail.registerCodingMemoryTools 中错误日志使用英文

`Msg("tool has no card, skipping registration")` 应改为中文。

### T-20 [提示] ExternalMemoryRail.Init 中 super().init(agent) 调用缺失

需确认 Go 基类 DeepAgentRail 的 Init 是否有需要执行的逻辑。

### T-21 [提示] MemoryToolContext 缺少 node_name="memory" 设置

Python 有，Go 未设置。

### T-22 [提示] joinLines 和 joinParams 可使用 strings.Join 替换

### T-23 [提示] viking_client_test.go 测试覆盖率需确认

### T-24 [提示] ExternalMemoryRail 中 providerTool.Card() 返回的 card 是指针可能被外部修改

---

## 五、7.18 LongTermMemoryExtractor（1 严重 / 3 一般 / 5 提示）

### S-38 [严重] 多模态消息 Text() 可能返回空串

Python `msg.content` 可以是 `str` 或 `List[Union[str, dict]]`（多模态消息），Python f-string 对 list 会调用 `str()` 得到如 `["image_url", "text"]` 的表示。Go 中 `msg.GetContent().Text()` 只返回 text 字段，对于多模态消息返回空字符串。

**Python 样例**:
```python
reference_str += f"{msg.name or msg.role}: {msg.content}\n"
```

**Go 问题代码** (`extractor.go`):
```go
referenceStr += fmt.Sprintf("%s: %s\n", name, msg.GetContent().Text())
```

**修复方案**: 当 `msg.GetContent().IsText()` 为 false 时，回退到遍历 parts 拼接或 `msg.GetContent().String()`，确保多模态消息不会静默丢失内容。

---

### M-50 [一般] 无时区时间戳解析默认 UTC vs Python 本地时间

对于不带时区的时间戳（如 `"2026-01-06T23:30:00"`），Python `fromisoformat` 解析为本地时间，Go `time.Parse` 解析为 UTC。在 UTC+8 环境下，如果时间在 23:00~00:00 之间，可能导致周范围计算差一天。

**Python 代码**:
```python
dt = datetime.fromisoformat(timestamp)  # 本地时间
```

**Go 问题代码**:
```go
time.Parse("2006-01-02T15:04:05", timestamp)  // 默认 UTC
```

**修复方案**: 对不带时区的时间戳使用 `time.ParseInLocation(layout, timestamp, time.Local)`。

### M-51 [一般] 非 dict 结果时 Go 多记了 Error 日志

Python 中 `isinstance(result, dict)` 为 False 时不记日志，Go 在最后一次重试时记了 Error。Go 行为更优，保留。

### M-52 [一般] MemoryAnalyzer 和 Generator 尚未实现

已有 TODO 说明，不在 7.18 范围但需确保回填。

### M-53 [一般] MemoryScopeConfig 字段缺少 `or ""` 防御性处理

Python 使用 `scope_config.user_profile_definition or ""` 防止 `None`，Go 没有。当前 `string` 类型不会 nil，且 `DefaultMemoryScopeConfig` 有默认值，但如果调用方传入空串或未来从 JSON 反序列化，可能产生差异。

**Python 样例**:
```python
"user_profile_definition": scope_config.user_profile_definition or "",
```

**Go 问题代码** (`extractor.go:89-91`):
```go
"user_profile_definition":    scopeConfig.UserProfileDefinition,
```

**修复方案**: 在 Apply 调用时添加防御：
```go
upDef := scopeConfig.UserProfileDefinition
if upDef == "" { upDef = config.DefaultMemoryScopeConfig().UserProfileDefinition }
```

### M-54 [一般] PromptTemplate.ToMessages() 调用与 Python 构造消息方式不对齐

Python 直接构造 `[{"role": "user", "content": prompt_content}]`，Go 多了一层 `PromptTemplate` + `ToMessages()` 包装，增加了不必要的中间对象。更对齐 Python 的写法：
```go
msgsParam := model_clients.NewMessagesParam(llmschema.NewUserMessage(userPrompt))
```

---

### T-25 [提示] formats 列表冗余：`"2006-01-02T15:04:05-07:00"` 与 `"2006-01-02T15:04:05Z07:00"` 重复

Go 的 `"2006-01-02T15:04:05Z07:00"` 格式已同时匹配 `Z`、`+08:00`、`-05:00` 等时区形式。`"2006-01-02T15:04:05-07:00"` 是冗余的，实际永远不会走到（因为 `Z07:00` 格式已在前面成功匹配）。

**修复方案**: 删除 `"2006-01-02T15:04:05-07:00"` 这一行。

### T-26 [提示] MemoryScopeConfig 缺少 model_cfg/model_client_cfg/embedding_cfg 字段

已有 TODO 注释。

### T-27 [提示] ExtractMemoryParams.BaseModel 字段名与 Python base_chat_model 不对齐

Python 用 `base_chat_model` 区分于 embedding model，Go 用 `BaseModel` 可能产生歧义。

### T-28 [提示] 缺少 retries=1 的边界测试

### T-29 [提示] 缺少 Z 后缀时间戳测试

### T-30 [提示] Invoke 错误不重试缺少注释说明

Go 中 `Invoke` 错误直接返回 error，与 Python 的 `except json.JSONDecodeError` 只捕获 JSON 解析错误的行为一致，但缺少注释说明为什么 Invoke 错误不重试，容易让后续维护者误解为遗漏。

### T-31 [提示] 缺少 context 取消时的行为测试

Python 版本是 async 的，天然支持 asyncio cancellation，Go 版本应通过 ctx 传播支持取消，需增加测试验证。

---

## 六、S 级修复 / any 消除 / staticcheck（合并入上述各模块）

> 以下问题已在各模块详细列表中记录，此处仅标注合并关系。

- SpawnMemberTool human_agent 路径漏检 prompt → **已合并入 S-24**
- TeamPolicyRail.membersCache.Refresh context.Background → **已合并入 S-39**
- UnsubscribeTransport context.Background → **已合并入 S-11/M-15**
- unwrapResults dict/list 格式误判 → **已合并入 M-46**
- KvPrefixRegistry map[string]bool vs map[string]struct{} → **已合并入 M-35**
- DistributedLock.Acquire lockValue 在循环外生成 → **已合并入 M-36**
- Viking Initialize 返回 error 行为与 Python 不一致 → **已合并入 S-36**

**修复确认清单（48h 内修复正确的部分）**：

1. ✅ Mem0 SyncTurn 吞错：`return nil` 正确对齐 Python 静默吞错
2. ✅ Mem0 熔断器并发保护：`breakerMu`/`prefetchMu` + `prefetchCancel` 保护完整
3. ✅ Viking truncStr CJK 截断：`utf8.RuneCountInString` + `[]rune` 截断正确
4. ✅ Viking score=0 区分：`floatValOK` 区分 None 和 0
5. ✅ Viking total=0 区分：`hasTotal` 检查
6. ✅ TeamTools 14 工具 Schema 已补齐
7. ✅ RolePolicy 签名改为 TeamRole 消除 string 弱类型
8. ✅ MtimeSectionCache.Invalidate 补充 cached/cachedMtime 清理
9. ✅ any 消除：Broadcast/HumanAgentSay 返回 *DeliverResult，RegisterRail 参数改为 AgentRail，Stream 返回 <-chan stream.Schema
10. ✅ staticcheck S1005：`x, _ := m[k]` → `x := m[k]`
11. ✅ staticcheck QF1011：`//nolint:staticcheck` 保留编译时接口检查
12. ✅ DistributedLock 注释修正与代码行为一致
13. ✅ KvPrefixRegistry.GetAllPrefixes 排序保证确定性输出

---

## 去重后总表

| 编号 | 级别 | 模块 | 问题摘要 |
|------|------|------|----------|
| S-01 | 严重 | TeamAgent | Invoke/Stream 核心流程完全未实现 |
| S-02 | 严重 | TeamAgent | 缺少 EnqueueMailboxAfterFirstIteration |
| S-03 | 严重 | TeamAgent | 缺少 FinalizeRound |
| S-04 | 严重 | TeamAgent | 缺少 TeamCompletion callback 注册 |
| S-05 | 严重 | TeamAgent | Setup 传 nil blueprint/infra |
| S-06 | 严重 | TeamAgent | Pause 缺少 3 个 Leader 步骤 |
| S-07 | 严重 | TeamAgent | Stop 缺少 cancel_recovery/shutdown |
| S-08 | 严重 | TeamAgent | MarkLiveTeammates 过滤逻辑不对齐 |
| S-09 | 严重 | TeamAgent | RecoverForExistingSession 缺少 StopCoordination |
| S-10 | 严重 | TeamAgent | SubscribeTransport 严重简化 |
| S-11 | 严重 | TeamAgent | UnsubscribeTransport 简化+context.Background |
| S-12 | 严重 | TeamAgent | ShutdownSelf 缺少 CloseStream |
| S-13 | 严重 | TeamAgent | LookupHumanAgentRuntime 缺少 is_human_agent 检查 |
| S-14 | 严重 | TeamAgent | Broadcast 缺少 UserInbox 对齐 |
| S-15 | 严重 | TeamAgent | HumanAgentSay 缺少 HumanAgentInbox |
| S-16 | 严重 | CodeAdapter | CreateInstance 缺失 updateRuntimeConfig |
| S-17 | 严重 | CodeAdapter | updateRuntimeConfig 不被委托调用 |
| S-18 | 严重 | DeepAdapter | ProcessMessageStreamImpl 缺 ResetAbort |
| S-19 | 严重 | DeepAdapter | isOutcomeEvent 类型断言 map[string]string vs map[string]any |
| S-20 | 严重 | CodeAdapter | CreateInstance 缺失 instance 属性设置 |
| S-21 | 严重 | DeepAdapter | handleEvolutionApproval context.Background |
| S-22 | 严重 | DeepAdapter | pushEventToFrontend context.Background |
| S-23 | 严重 | DeepAdapter | processTeamMessageStream 参数解析路径错误 |
| S-24 | 严重 | TeamTools | SpawnMemberTool human_agent 漏检 prompt |
| S-25 | 严重 | TeamTools | SpawnMemberTool inputs["prompt"] 裸断言 panic |
| S-26 | 严重 | TeamTools | ViewTaskTool claimable 未传 status=PENDING |
| S-27 | 严重 | TeamTools | ViewTaskTool list 未传 inputs["status"] |
| S-28 | 严重 | TeamTools | ListMembersTool 返回字段不完整 |
| S-29 | 严重 | TeamTools | SubmitPlanTool 缺少 message 字段 |
| S-30 | 严重 | TeamTools | SendMessageTool multicast 缺全员覆盖拒绝 |
| S-31 | 严重 | TeamTools | CancelMember 返回值被忽略 |
| S-32 | 严重 | ExternalMemoryRail | AfterInvoke goroutine ctx 可能已取消 |
| S-33 | 严重 | Runtime | handleInteractiveInput context.Background |
| S-34 | 严重 | Runtime | resolveRecipients context.Background |
| S-35 | 严重 | registry | PoolAccessor/PoolEntry 接口方法不完整 |
| S-36 | 严重 | VikingProvider | Initialize 健康检查失败返回 error，Python 静默 |
| S-37 | 严重 | CodingMemoryRail | autoRecall 截断 rune 数 vs 字节数单位不匹配 |
| S-38 | 严重 | LongTermMemoryExtractor | 多模态消息 Text() 可能返回空串 |
| S-39 | 严重 | TeamPolicyRail | membersCache.Refresh context.Background |
| M-01~M-54 | 一般 | 各模块 | 见上文详细列表（含 M-53 scopeConfig or"" 防御、M-54 PromptTemplate 包装偏差） |
| T-01~T-31 | 提示 | 各模块 | 见上文详细列表（含 T-30 Invoke 错误不重试缺注释、T-31 缺 ctx 取消测试） |

---

## 最高优先修复 Top 10

| 优先级 | 编号 | 问题 | 影响 |
|--------|------|------|------|
| P0-1 | S-01 | Invoke/Stream 核心流程未实现 | TeamAgent 完全无法工作 |
| P0-2 | S-02 | 缺少 EnqueueMailboxAfterFirstIteration | Teammate 无法收到邮箱通知 |
| P0-3 | S-03 | 缺少 FinalizeRound | 会话结束无清理，内存泄漏 |
| P0-4 | S-05 | Setup 传 nil blueprint/infra | Dispatcher 无法正常工作 |
| P0-5 | S-19 | isOutcomeEvent 类型断言错误 | Evolution 审批永远检测不到 outcome |
| P0-6 | S-25 | inputs["prompt"] 裸断言 panic | 运行时 crash |
| P0-7 | S-32 | AfterInvoke goroutine ctx 取消 | SyncTurn 被中断，记忆同步失败 |
| P0-8 | S-17 | CodeAdapter.updateRuntimeConfig 不被调用 | Code 模式配置不生效 |
| P0-9 | S-36 | Viking Initialize 返回 error | 所有后续 prefetch/sync 永远跳过 |
| P0-10 | S-18 | StreamImpl 缺 ResetAbort | 流式模式 abort 状态残留 |
