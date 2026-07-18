# 2026-07-17 逻辑审查报告

> 审查范围：48小时内提交记录（`ad77590..f369c8f`，共7个提交）
> 涉及章节：9.70a Operator、9.70b Dataset、9.70 Trainer、9.56-9.57 Blueprint/AgentConfigurator、10.3 Adapter/Session、9.19 ContextEngine Rail
> 对照标准：Python 参考项目方法签名和步骤一致性

---

## 严重问题 (S)

### S01: GetContextWindow 中处理器状态事件发射完全缺失

**严重性**：严重 — 外部系统无法感知上下文引擎在 GetContextWindow 阶段的压缩行为，流式推送/回调框架收不到 started/completed/failed 事件

**Python 样例**：

```python
# openjiuwen/core/context_engine/context.py SessionModelContext._run_get_context_window_processors
for proc in self._processors:
    triggered = proc.trigger_get_context_window(context, window, **kwargs)
    if not triggered:
        continue

    # ✅ 发射 started 状态
    self._state_recorder.emit(
        self._state_recorder.build_state(
            status=CompressionStatus.STARTED, phase=phase, ...
        )
    )

    event, new_window = proc.on_get_context_window(context, window, **kwargs)

    if event:
        # ✅ 发射 completed/noop 状态
        self._state_recorder.emit(
            self._state_recorder.build_state(
                status=CompressionStatus.COMPLETED, ...
            )
        )
    else:
        # ✅ 发射 noop 状态
        self._state_recorder.emit(
            self._state_recorder.build_state(status=CompressionStatus.NOOP, ...)
        )
```

**Go 问题**：

```go
// internal/agentcore/context_engine/context/session_model_context.go:389-422
for _, proc := range mc.processors {
    triggered, err := proc.TriggerGetContextWindow(ctx, mc, *window, opts...)
    // ...
    event, newWindow, err := proc.OnGetContextWindow(ctx, mc, *window, opts...)
    if err != nil {
        logger.Error(logComponent).Msg("处理器执行失败")
        continue  // ❌ 只记日志，未发射 failed 状态
    }
    window = &newWindow
    if event != nil {
        mc.stateRecorder.recordFromEvent(event)  // ❌ 只记日志，未 Emit started/completed/failed
    }
}
```

**修复方案**：

1. 在 `OnGetContextWindow` 执行前发射 `CompressionStarted` 状态
2. 成功后发射 `CompressionCompleted` 或 `CompressionNoop` 状态
3. 失败时发射 `CompressionFailed` 状态
4. 参照 `runAddProcessors` 中已有的 `Emit(BuildState(...))` 模式

---

### S02: AddMessages 中缺少 started 状态发射

**严重性**：严重 — AddMessages 路径只有 completed/failed，缺少 started，外部系统无法感知处理器何时开始执行

**Python 样例**：

```python
# openjiuwen/core/context_engine/context.py _run_add_processors
for proc in processors:
    # ✅ 先发射 started
    self._state_recorder.emit(
        self._state_recorder.build_state(
            status=CompressionStatus.STARTED, phase=phase, ...
        )
    )
    event, new_messages = proc.on_add_messages(context, messages, **kwargs)
    # ✅ 再发射 completed/failed
```

**Go 问题**：

```go
// internal/agentcore/context_engine/context/session_model_context.go:692-784
func (mc *SessionModelContext) runAddProcessors(...) {
    // ❌ 缺少 started 状态发射
    event, newMessages, err := proc.OnAddMessages(ctx, mc, messages, opts...)
    if err != nil {
        mc.stateRecorder.Emit(ctx, mc.stateRecorder.BuildState(ProcessorStateInput{
            Status: ceschema.CompressionFailed,  // ✅ 有 failed
        }))
        continue
    }
    // ✅ 有 completed
    mc.stateRecorder.Emit(ctx, mc.stateRecorder.BuildState(ProcessorStateInput{
        Status: ceschema.CompressionCompleted,
    }))
}
```

**修复方案**：在 `OnAddMessages` 调用前，发射 `CompressionStarted` 状态，对齐 Python 的双状态（started → completed/failed）模式。

---

### S03: session 缺少 `IncrementSessionRoundCount` 函数

**严重性**：严重 — Python 在 team_helpers 中调用 `increment_session_round_count` 递增轮次，Go 完全缺失此函数

**Python 样例**：

```python
# jiuwenswarm/server/runtime/session/session_metadata.py:237
def increment_session_round_count(session_id: str) -> int:
    """递增并持久化 session 的 round_id，返回递增后的值。"""
    metadata = _read_metadata(session_id)
    current_round = int(metadata.get("round_id", 0))
    new_round = current_round + 1
    metadata["round_id"] = new_round
    metadata["last_message_at"] = _current_timestamp()
    _enqueue_write(session_id, metadata)
    return new_round

# jiuwenswarm/server/runtime/agent_adapter/team_helpers.py:772
round_id = increment_session_round_count(session_id)
```

**Go 问题**：

`internal/swarm/server/handle_session.go` 中 `round_id` 只在 `init_session_metadata` 时初始化为 0，但没有递增函数。搜索 `increment_round_id`/`IncrementRound` 均无结果。

**修复方案**：

在 `handle_session.go` 中添加 `incrementSessionRoundCount` 函数：

```go
func incrementSessionRoundCount(sessionsDir, sessionID string) (int, error) {
    meta, err := readSessionMetadata(sessionsDir, sessionID)
    if err != nil {
        return 0, err
    }
    currentRound, _ := meta["round_id"].(int)
    newRound := currentRound + 1
    meta["round_id"] = newRound
    meta["last_message_at"] = currentTimestamp()
    if err := writeSessionMetadata(sessionsDir, sessionID, meta); err != nil {
        return 0, err
    }
    return newRound, nil
}
```

---

### S04: session 缺少 `RemoveSessionMetadataCache` 函数

**严重性**：严重 — Python 在 session 删除后清理缓存，Go 缺失此函数会导致内存泄漏

**Python 样例**：

```python
# jiuwenswarm/server/runtime/session/session_metadata.py:252
def remove_session_metadata_cache(session_id: str) -> None:
    """Remove cached session metadata after the session directory is deleted."""
    with _CACHE_LOCK:
        _METADATA_CACHE.pop(session_id, None)

# 被调用在 session delete 逻辑中：
# session_metadata.py:386-387
with _CACHE_LOCK:
    _METADATA_CACHE.pop(session_id, None)
```

**Go 问题**：

Go 的 `handleSessionDelete` 中没有任何缓存清理逻辑。`deliveryContextCache` 是内存缓存，删除 session 后不清理会导致陈旧数据残留。

**修复方案**：

在 `handleSessionDelete` 删除文件后添加缓存清理：

```go
func (s *AgentServer) handleSessionDelete(...) {
    // ... 删除目录 ...
    deliveryContextMu.Lock()
    delete(deliveryContextCache, sessionID)
    deliveryContextMu.Unlock()
}
```

---

### S05: LLMCallOperator 不支持多消息格式 prompt

**严重性**：严重 — Python 的 `system_prompt` 接受 `str | List[Dict]`，Go 仅接受 `string`，自演化框架无法修改多消息格式的 prompt

**Python 样例**：

```python
# openjiuwen/core/operator/llm_call/base.py:31-33
class LLMCallOperator(Operator):
    def __init__(self, system_prompt: str | List[Dict], user_prompt: str | List[Dict], ...):
        self._system_prompt = PromptTemplate(content=system_prompt)  # str 或 List[Dict]
```

**Go 问题**：

```go
// internal/agentcore/operator/llm_call/llm_call_operator.go:66
func NewLLMCallOperator(systemPrompt, userPrompt string, ...) *LLMCallOperator {

// promptContent 函数对 []any 使用 fmt.Sprintf，不会产生有效消息结构
func promptContent(value any) string {
    case []any:
        return fmt.Sprintf("%v", v)  // ❌ 产生 "[map[role:system content:...]]" 而非有效结构
}
```

**修复方案**：

1. `NewLLMCallOperator` 的 `systemPrompt` 参数改为 `any`，支持 `string` 和 `[]any`（多消息格式）
2. `promptContent` 对 `[]any` 类型应 JSON 序列化而非 `Sprintf`
3. `GetState()` 返回值应保持原始类型（`string` 或 `[]any`），而非总是 `string`

---

### S06: `resolveTeamMode` 被删除但 Python 仍依赖

**严重性**：严重 — `agent_configurator.go` 删除了 `resolveTeamMode()`，但 Python 中此函数在 `setup_agent` 的关键路径上被调用

**Python 样例**：

```python
# openjiuwen/agent_teams/agent/agent_configurator.py:58-67
def _resolve_team_mode(spec):
    if spec.team_mode:
        return spec.team_mode
    has_non_human = any(not m.is_human for m in spec.predefined_members)
    return "hybrid" if has_non_human else "default"

# 调用位置：
# agent_configurator.py:354
exclude = {"spawn_member"} if _resolve_team_mode(spec) == "predefined" else None
# agent_configurator.py:378
team_mode=_resolve_team_mode(spec)
```

**Go 问题**：

搜索 `resolveTeamMode` 无结果，函数已被删除且无替代实现。

**修复方案**：

将 `resolveTeamMode` 恢复到 `agent_configurator.go` 或迁移为 `TeamAgentSpec` 的方法：

```go
func (s TeamAgentSpec) ResolveTeamMode() string {
    if s.TeamMode != "" {
        return s.TeamMode
    }
    for _, m := range s.PredefinedMembers {
        if !m.IsHuman {
            return "hybrid"
        }
    }
    return "default"
}
```

---

### S07: Trainer.ApplyUpdates 使用 ApplyUpdate 而非 SetParameter，与 Python 行为不一致

**严重性**：严重 — Python 的 `Trainer.apply_updates` 直接调用 `op.set_parameter`，Go 的 `ApplyUpdates` 调用 `op.ApplyUpdate`，两者的语义和结果不同

**Python 样例**：

```python
# openjiuwen/agent_evolving/trainer/trainer.py:346-355
@staticmethod
def apply_updates(operators: Dict[str, Operator], updates: Updates) -> None:
    for (operator_id, target), value in updates.items():
        op = operators.get(operator_id)
        if op is not None and value is not None:
            op.set_parameter(target, value)  # ✅ 直接调用 set_parameter
```

**Go 问题**：

```go
// internal/evolving/trainer/trainer.go:163-179
func ApplyUpdates(operators map[string]operator.Operator, updates map[schema.UpdateKey]schema.UpdateValue) []schema.ApplyResult {
    for key, update := range updates {
        op, ok := operators[key.OperatorID()]
        result := op.ApplyUpdate(key.Target(), update)  // ❌ 调用 ApplyUpdate 而非 SetParameter
    }
}
```

**差异分析**：

| 行为 | Python `set_parameter` | Go `ApplyUpdate` |
|------|----------------------|------------------|
| SkillExperienceOperator | `set_parameter` 触发 `on_parameter_updated` 回调 | `ApplyUpdate` 路由到 `PreviewUpdate`，返回 ApplyResult 但不触发回调 |
| 默认 Operator | `set_parameter` 直接更新值 | `DefaultApplyUpdate` 内部调用 `SetParameter`，但多做了一次 `GetState` 比较 |
| 返回值 | 无返回值 | 返回 `ApplyResult` |

**影响**：`SkillExperienceOperator` 路径下，Go 的 `ApplyUpdates` 不会触发 `onParameterUpdated` 回调，消费者（Agent/Rail）不会收到参数变更通知。

**修复方案**：

对齐 Python 行为，`ApplyUpdates` 应直接调用 `op.SetParameter`：

```go
func ApplyUpdates(operators map[string]operator.Operator, updates map[schema.UpdateKey]schema.UpdateValue) {
    for key, update := range updates {
        op, ok := operators[key.OperatorID()]
        if !ok {
            continue
        }
        op.SetParameter(key.Target(), update.Payload)
    }
}
```

---

### S08: handleUnary/handleStream 中 code 模式 SwitchMode 逻辑严重简化

**严重性**：严重 — Python 在 code+非team 模式下走完整的 `create_agent_session → pre_run → switch_mode → load_state → post_run` 持久化流程，Go 只做 `_ = agent.SwitchMode(mode, subMode)` 且忽略返回值

**Python 样例**：

```python
# jiuwenswarm/server/agent_ws_server.py _handle_unary / _handle_stream
# code 模式下（非 team）:
session = create_agent_session(agent, conversation_id=conv_id)
await session.pre_run(request)
mode_changed = agent.switch_mode(mode, sub_mode)
if mode_changed:
    await session.load_state()  # ✅ 切换模式后重新加载状态
result = await agent.process_message(request)
await session.post_run(result)
```

**Go 问题**：

```go
// Go handleUnary/handleStream 中:
_ = agent.SwitchMode(mode, subMode)  // ❌ 忽略返回值，无 session pre_run/post_run
result, err := agent.ProcessMessage(ctx, request)
```

**影响**：code 模式下切换模式后不会重新加载 Agent 状态（如 prompt、工具列表），可能导致模式切换后行为不一致。

**修复方案**：对齐 Python 的完整 session 生命周期管理，SwitchMode 返回 true 时重新加载状态。

---

### S09: handle_envelope 缺少 E2A fallback 解析

**严重性**：严重 — Python 在 E2AEnvelope 解析失败时走 legacy payload fallback，Go 直接返回错误，可能导致旧客户端无法连接

**Python 样例**：

```python
# jiuwenswarm/server/agent_ws_server.py L771-791
try:
    envelope = E2AEnvelope.from_dict(data)
    request = e2a_to_agent_request(envelope)
except Exception:
    # ✅ fallback: legacy payload 解析
    request = self._payload_to_request(data)
```

**Go 问题**：

```go
// handle_envelope.go
request, err := e2a.E2AToAgentRequest(envelope)
if err != nil {
    // ❌ 直接返回错误，无 fallback
    s.writeError(...)
    return
}
```

**修复方案**：添加 legacy payload fallback 路径，当 E2A 解析失败时尝试直接从原始 JSON 解析 AgentRequest。

---

### S10: handleCancel 调用路径与 Python 不一致

**严重性**：严重 — Python cancel 走 `JiuWenClaw.process_message` 的 CHAT_CANCEL 分支（调用 `_process_interrupt`），Go 直接调用 `agent.ProcessInterrupt`，调用路径差异可能导致 side effect 不同

**Python 样例**：

```python
# jiuwenswarm/server/agent_ws_server.py _handle_cancel
# Python cancel 走 process_message
result = await agent.process_message(request)  # 内部分发到 CHAT_CANCEL 分支
# CHAT_CANCEL 分支内调用 self._process_interrupt(request)
```

**Go 问题**：

```go
// Go handleCancel:
result, err := agent.ProcessInterrupt(ctx, request)  // ❌ 直接调用 ProcessInterrupt
```

**影响**：Python 的 `process_message` 入口会触发 callback/rail 钩子，而 `ProcessInterrupt` 可能跳过这些钩子。

**修复方案**：对齐 Python，cancel 请求走 `ProcessMessage` 入口，由内部路由到 CHAT_CANCEL 分支。

---

## 一般问题 (G)

### G01: SkillExperienceOperator 使用本地常量而非 schema 包常量

**严重性**：一般 — `experiencesTarget` 是本地私有常量 `"experiences"`，但 `schema.ExperiencesTarget` 已在共享契约层定义

**Python 样例**：

```python
# openjiuwen/core/operator/skill_call/base.py:9-14
from openjiuwen.agent_evolving.protocols import (
    EXPERIENCES_TARGET,  # ✅ 从共享契约层导入
    ...
)

class SkillExperienceOperator(PreviewableOperator):
    def get_tunables(self):
        return {EXPERIENCES_TARGET: TunableSpec(name=EXPERIENCES_TARGET, ...)}  # ✅ 使用共享常量
```

**Go 问题**：

```go
// internal/agentcore/operator/skill_call/skill_experience_operator.go:36
const (
    experiencesTarget = "experiences"  // ❌ 本地硬编码，而非 schema.ExperiencesTarget
)
```

**修复方案**：删除本地常量，改用 `schema.ExperiencesTarget`：

```go
import "github.com/uapclaw/uapclaw-go/internal/evolving/schema"

// GetTunables 使用 schema.ExperiencesTarget
tunables[schema.ExperiencesTarget] = operator.TunableSpec{
    Name: schema.ExperiencesTarget,
    ...
}
```

---

### G02: CompressContext 不支持 return_state 模式

**严重性**：一般 — Python 支持一次调用获取压缩详情，Go 需额外查询

**Python 样例**：

```python
# openjiuwen/core/context_engine/context.py
def compress_context(self, *, return_state=False):
    if return_state:
        return self._build_active_compression_result()
    return summary_text
```

**Go 问题**：

```go
func (mc *SessionModelContext) CompressContext() (string, error) {
    // ❌ 只返回 string，无法返回完整压缩状态
}
```

**修复方案**：添加 `CompressContextWithState` 方法，返回 `(string, *ceschema.ContextCompressionState, error)`。

---

### G03: ValidateLeaderModelResolved 错误信息过于简化

**严重性**：一般 — Python 版本有详细诊断和修复建议，Go 只返回通用错误

**Python 样例**：

```python
# openjiuwen/agent_teams/schema/blueprint.py
def _validate_leader_model_resolved(self):
    if not self.leader.model_name and not self.model_pool and not self.model_router:
        raise ValueError(
            "leader.model_name is required when neither model_pool nor model_router is set. "
            "Either set leader.model_name, or configure model_pool/model_router. "
            "If using a router, leave leader.model_name unset to fall back on the router's first declared name."
        )
```

**Go 问题**：

```go
func (s TeamAgentSpec) ValidateLeaderModelResolved() error {
    // 只返回通用错误，缺少具体诊断
    return fmt.Errorf("leader model not resolved")
}
```

**修复方案**：对齐 Python 的详细错误信息，区分不同场景（router/pool/none）。

---

### G04: DeepAgentSpec.Tools/Mcps/Subagents/Rails 使用 `[]any` 弱类型

**严重性**：一般 — 失去编译期类型检查，反序列化时可能丢失数据

**Python 样例**：

```python
class DeepAgentSpec(BaseModel):
    tools: Optional[list[ToolCard | BuiltinToolSpec]] = None     # ✅ 强类型联合
    mcps: Optional[list[McpServerConfig]] = None                 # ✅ 强类型
    subagents: Optional[list[SubAgentSpec]] = None               # ✅ 强类型
    rails: Optional[list[RailSpec]] = None                       # ✅ 强类型
```

**Go 问题**：

```go
type DeepAgentSpec struct {
    Tools      []any `json:"tools,omitempty"`       // ❌ 弱类型
    Mcps       []any `json:"mcps,omitempty"`        // ❌ 弱类型
    Subagents  []any `json:"subagents,omitempty"`   // ❌ 弱类型
    Rails      []any `json:"rails,omitempty"`        // ❌ 弱类型
}
```

**修复方案**：使用具体类型或 `json.RawMessage` + 自定义反序列化：

```go
Tools     []json.RawMessage  `json:"tools,omitempty"`
Mcps      []json.RawMessage  `json:"mcps,omitempty"`
```

---

### G05: session 缺少 `UpdateSessionMetadata` 函数

**严重性**：一般 — Python 有独立的 `update_session_metadata` 函数处理增量更新，Go 的 session metadata 更新逻辑散落在各个 handler 中

**Python 样例**：

```python
# jiuwenswarm/server/runtime/session/session_metadata.py:153-208
def update_session_metadata(
    *, session_id, channel_id=None, user_id=None, title=None,
    clear_title=False, increment_message_count=False,
    set_message_count=None, user_content=None,
    channel_metadata=None, mode=None, team_name=None,
):
    """更新会话元数据(异步写入,不阻塞调用方)"""
```

**Go 问题**：Go 没有 `updateSessionMetadata` 统一函数，各 handler 各自组装 metadata 更新逻辑，容易出错且难以维护。

**修复方案**：抽取统一的 `updateSessionMetadata` 函数，对齐 Python 的参数签名和异步写入模式。

---

### G06: before_chat_request 钩子未实现

**严重性**：一般 — Python 在 CHAT_SEND/CHAT_RESUME/CHAT_ANSWER 前触发 `_trigger_before_chat_request_hook`，Go 标记为 ⤵️ Extension 章节

**Python 样例**：

```python
# jiuwenswarm/server/agent_ws_server.py L812
await self._trigger_before_chat_request_hook(request)
```

**Go 问题**：

```go
// handle_envelope.go:63
// 3. before_chat_request 钩子
// ⤵️ Extension 章节：实现 _trigger_before_chat_request_hook
```

**修复方案**：等 Extension 框架（10.5）实现后回填。

---

### G07: AgentServer Stop 流程多个步骤为 stub

**严重性**：一般 — Python 的 `AgentWebSocketServer.stop()` 有完整的资源清理逻辑，Go 多个步骤为 TODO stub

**Python 样例**：

```python
# jiuwenswarm/server/agent_ws_server.py stop()
# 完整清理：cancel inflight tasks, stop scheduler, cancel team streams,
# stop internal jiuwenbox, stop bootstrap daemon, reset harness state
```

**Go 问题**：

```go
// agent_server.go Stop():
// cancelAllInflightWork — TODO stub
// stopScheduler — TODO stub
// cancelAllTeamStreamTasks — TODO stub
```

**修复方案**：对齐 Python 的完整 Stop 流程，确保资源不泄漏。

---

### G08: session rewind/fork 功能未实现

**严重性**：一般 — Python 有 `session.rewind`/`session.rewind_and_restore`/`session.rewind_context`/`session.fork`，Go 全部返回 NOT_IMPLEMENTED

**Python 样例**：

```python
# jiuwenswarm/server/runtime/session/session_history.py
class SessionHistory:
    def rewind(self, session_id, message_id): ...
    def rewind_and_restore(self, session_id, message_id): ...
    def rewind_context(self, session_id, message_id): ...
```

**Go 问题**：`handleSessionRewind` 等返回 `not implemented` 错误。

**修复方案**：等 10.3.16 SessionHistory 实现后回填。

---

## 提示问题 (T)

### T01: AddMessages 签名与 Python 不一致（有意设计）

**说明**：Python `add_messages` 接受 `BaseMessage | List[BaseMessage]`，Go `AddMessages` 只接受单条消息。这是 Go 惯例（避免 any 参数），调用方需逐条调用。功能等价，记录差异。

---

### T02: LLMCall / SkillCallOperator 别名已删除（有意设计）

**说明**：Python 保留 `LLMCall = LLMCallOperator` 和 `SkillCallOperator = SkillExperienceOperator` 向后兼容别名，Go 重构时有意删除。Go 不鼓励类型别名，可接受。

---

### T03: TunableSpec.Constraint 类型收窄

**说明**：Python `constraint: Optional[Any]`，Go `Constraint map[string]any`。目前所有 Python 使用场景都是 dict，功能上等价。但 Go 无法区分"未设置"（nil map）和 Python 的 `None`。当前不影响功能。

---

### T04: Trainer 核心方法仍为桩实现

**说明**：`Train/Forward/Evaluate/Predict` 等 13 个方法返回 `"not implemented"` 错误。这是已知的阶段性行为，依赖 9.70c/9.77/9.78 等后续章节填充。`ApplyUpdates` 和 `meanScore` 已真实实现。

---

### T05: context_engineer Rail 有 3 个待回填点

**说明**：
- ContextAssembleRail 的 context section 注入（需 ReadContextFiles 支持）
- SessionMemoryManager 功能（预留字段未实现）
- `_maybe_schedule_session_memory_update` 和 `update_inherited_system_prompt` 未实现

这些标注为 ⤵️ 回填点，不影响核心功能。

---

## 已确认的活跃回填点汇总

| 回填点 | 位置 | 状态 |
|--------|------|------|
| ⤵️ AgentUpdater agent_edit 模式 | `session_memory_manager.go` | 未实现，返回 error |
| ⤵️ 6.23 IntentRecognizer LLM 调用 | `intent_recognizer.go` | 未实现，返回 nil |
| ⤵️ ActorManager 返回类型 | `session/interfaces.go` | any 占位 |
| ⤵️ 9.57 DeepAgentSpec/TeamAgentSpec Build | `schema/blueprint.go` | 返回 nil,nil |
| ⤵️ 9.65 Messager Build | `messager/base.go` | 返回 nil,nil |
| ⤵️ 9.64 Team Memory | `memory/config.go` | 返回 nil |
| ⤵️ 5.12 Config 回填 | `session/node.go` | 待回填 |
| ⤵️ 8.7 Graph Store | `session/interfaces.go` | 待回填 |
| ⤴️ 7.22/7.23 MilvusVectorStore.UpdateSchema | `vector/milvus_adapter.go` | 待回填 |
| ⤵️ SSRF 防护 | `tool/service_api/restful_api.go` | 待回填 |

---

## 问题统计

| 严重性 | 数量 | 编号 |
|--------|------|------|
| 严重 | 10 | S01-S10 |
| 一般 | 8 | G01-G08 |
| 提示 | 5 | T01-T05 |

---

## 修复优先级建议

### P0 — 影响核心功能流程，应立即修复
1. **S01+S02**：GetContextWindow/AddMessages 状态事件发射缺失 — 影响所有使用流式推送的前端
2. **S07**：Trainer.ApplyUpdates 语义不一致 — 自演化框架核心路径行为与 Python 不同
3. **S08**：SwitchMode 逻辑简化 — code 模式切换后状态不正确
4. **S10**：handleCancel 调用路径差异 — cancel 可能跳过 callback/rail 钩子

### P1 — 影响功能完整性，应尽快修复
5. **S03+S04**：Session round_count 和缓存清理 — session 生命周期完整性
6. **S05**：LLMCallOperator 多消息格式 — 自演化对复杂 prompt 的修改
7. **S06**：resolveTeamMode 删除 — team agent 构建依赖
8. **S09**：E2A fallback 解析 — 旧客户端兼容性

### P2 — 影响可维护性或类型安全，择机修复
9. **G01**：SkillExperienceOperator 本地常量 → schema 包常量
10. **G02**：CompressContext return_state 模式
11. **G03**：ValidateLeaderModelResolved 错误信息
12. **G04**：DeepAgentSpec 弱类型字段
13. **G05**：缺少统一 UpdateSessionMetadata 函数
14. **G06-G08**：Extension hooks / Stop 流程 / session rewind
