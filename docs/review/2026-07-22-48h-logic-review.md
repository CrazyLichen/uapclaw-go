# 48小时代码逻辑审查报告

> 审查日期：2026-07-22
> 审查范围：近期提交的功能实现（9.27-9.30 子代理、9.59b Interaction 层、9.70-9.80 Evolving 模块、9.72a InstructionOptimizer、9.72b ToolOptimizer、10.3.12 AgentManager）
> 对比基准：Python 参考项目源码

## 审查章节概览

| 章节 | 内容 | 提交时间 | 状态 |
|------|------|---------|------|
| 9.27-9.30 | CodeAgent/PlanAgent/VerificationAgent/ExploreAgent | 2026-07-19 | ✅ 已完成 |
| 9.59b | Interaction 层（payload/router/inbox/runtime） | 2026-07-19 | ✅ 已完成 |
| 9.70-9.80 | Evolving 通用模块（Operator/Dataset/Trainer/Updater/Evaluator/Signal/Trajectory） | 2026-07-19 | ✅ 已完成 |
| 9.72a | InstructionOptimizer 指令优化器 | 2026-07-19 | ✅ 已完成 |
| 9.72b | ToolOptimizer 工具描述优化器 | 2026-07-19 | ✅ 已完成 |
| 10.3.12 | AgentManager 多实例管理 | 2026-07-19 | ✅ 已完成 |

## 问题统计

| 严重程度 | 数量 |
|---------|------|
| 严重 | 13 |
| 一般 | 18 |
| 提示 | 15 |

---

## 一、9.27-9.30 子代理（2 严重 / 3 一般 / 2 提示）

### S-01：[严重] PlanAgent 缺少默认 Rails

**Python 参考代码** (`plan_agent.py:119`):
```python
rails=rails if rails is not None else [SysOperationRail()],
```

**Go 问题代码** (`plan_agent.go:122`):
```go
cfg.Rails = params.Rails
```

**问题分析**：Python 的 `build_plan_agent_config` 在用户不传 rails 时默认提供 `[SysOperationRail()]`，Go 直接赋值 `params.Rails`，未处理 `nil` 情况。缺少 SysOperationRail 意味着 PlanAgent 没有文件系统/Shell 工具，无法正常工作。

**修复方案**：
```go
// 对齐 Python: rails=rails if rails is not None else [SysOperationRail()]
if params.Rails == nil {
    cfg.Rails = []sainterfaces.AgentRail{
        rails.NewSysOperationRail(),
    }
} else {
    cfg.Rails = params.Rails
}
```

---

### S-02：[严重] ExploreAgent 缺少默认 Rails 且缺少 read_only=True

**Python 参考代码** (`explore_agent.py:192`):
```python
rails=rails if rails is not None else [SysOperationRail(read_only=True)],
```

**Go 问题代码** (`explore_agent.go:143`):
```go
cfg.Rails = params.Rails
```

**问题分析**：两个问题叠加：
1. 缺少 `params.Rails == nil` 时的默认 Rails
2. 默认 Rails 应为 `SysOperationRail(read_only=True)`，而非普通的 `SysOperationRail()`

如果 ExploreAgent 获得非只读的 SysOperationRail，只读约束会被绕过，Agent 可以执行写操作。

**修复方案**：
```go
// 对齐 Python: rails=rails if rails is not None else [SysOperationRail(read_only=True)]
if params.Rails == nil {
    cfg.Rails = []sainterfaces.AgentRail{
        rails.NewSysOperationRail(rails.WithReadOnly(true)),
    }
} else {
    cfg.Rails = params.Rails
}
```

---

### M-01：[一般] ExploreAgent 缺少 search_via_bash 参数

**Python 参考代码** (`explore_agent.py:174`):
```python
def build_explore_agent_config(
    *,
    ...
    search_via_bash: bool = False,
) -> SubAgentConfig:
```

**Go 实现**：`BuildExploreAgentConfig` 和 `SubagentCreateParams` 中均无此参数。

**问题分析**：`search_via_bash` 控制是否允许 explore_agent 通过 bash 进行搜索。缺少此参数意味着无法启用此功能。

**修复方案**：在 `SubagentCreateParams` 中添加 `SearchViaBash bool` 字段，在 `BuildExploreAgentConfig` 中将其传递到配置。

---

### M-02：[一般] VerificationAgent 描述回退语言不一致

**Python 参考代码** (`verification_agent.py:297-298`):
```python
description=VERIFICATION_AGENT_DESC.get(resolved_language, VERIFICATION_AGENT_DESC["en"]),
```

**Go 问题代码** (`verification_agent.go:237`):
```go
desc := defaultVerificationAgentDescription[language]
if desc == "" {
    desc = defaultVerificationAgentDescription["cn"]
}
```

**问题分析**：Go 的回退语言是 `"cn"`，Python 的回退语言是 `"en"`。当语言代码无法匹配时，Go 会回退到中文，Python 回退到英文。

**修复方案**：
```go
desc := defaultVerificationAgentDescription[language]
if desc == "" {
    desc = defaultVerificationAgentDescription["en"]
}
```

---

### M-03：[一般] PlanAgent 的 FactoryName 常量定义但未使用

**Go 代码** (`plan_agent.go`):
```go
PlanAgentFactoryName = "plan_agent"  // 定义了但从未使用
```

**问题分析**：注释说"不设 FactoryName"，`cfg.FactoryName = ""` 是正确的。但 `PlanAgentFactoryName` 常量存在可能造成混淆。

**修复方案**：删除未使用的 `PlanAgentFactoryName` 常量，或在需要引用时使用它。

---

### I-01：[提示] CodeAgent 的 EN 描述格式与 Python 不一致

**Python 参考代码** (`code_agent.py:47-48`):
```python
DEFAULT_CODE_AGENT_DESCRIPTION_EN = """You are a senior software engineer and coding agent,
    excel at translating tasks into runnable code and verifiable results."""
```

**Go 问题代码** (`code_agent.go:39-41`):
```go
"en": "You are a senior software engineer and coding agent, " +
    "excel at translating tasks into runnable code and verifiable results.",
```

**问题分析**：Python EN 描述中有换行和4空格缩进，Go 版本是单行连续文本。按项目规则"提示词一比一复刻 Python 原文"，应保留原文格式。

**修复方案**：使用 Go 原始字符串保留换行：
```go
"en": "You are a senior software engineer and coding agent,\n    excel at translating tasks into runnable code and verifiable results.",
```

---

### I-02：[提示] ExploreAgent EN 提示词中工具名硬编码而非变量引用

**Python 参考代码** (`explore_agent.py:41-46`): 工具名通过常量 `_EXPLORE_TOOL_GLOB` 等引用，实现解耦。

**Go 实现**：工具名硬编码在提示词字符串中。

**问题分析**：功能等价，但若工具名未来变更，Go 需手动修改提示词字符串。

---

## 二、9.59b Interaction 层（5 严重 / 7 一般 / 5 提示）

### S-03：[严重] dispatchPayload 缺少 no_team_backend 检查

**Python 参考代码** (`manager.py:442-444`):
```python
backend = agent.team_backend
if backend is None and not isinstance(payload, GodViewMessage):
    return DeliverResult.failure("no_team_backend")
```

**Go 问题代码** (`manager.go:256-304`):
```go
func (m *TeamRuntimeManager) dispatchPayload(...) (*interaction.DeliverResult, error) {
    switch p := payload.(type) {
    case *interaction.GodViewMessage:
        // 直接处理，无 backend 检查
    case *interaction.OperatorMessage:
        // 没有检查 backend == nil
    case *interaction.HumanAgentMessage:
        // 没有检查 backend == nil
    }
}
```

**问题分析**：当 `backend == nil` 时，OperatorMessage 和 HumanAgentMessage 会传给 nil 的 inbox 参数，导致运行时 panic 或静默失败。Python 会返回 `failure("no_team_backend")`。

**修复方案**：
```go
if entry.Agent == nil && payload.Kind() != interaction.PayloadKindGodView {
    return interaction.NewDeliverResultFailure("no_team_backend"), nil
}
```

---

### S-04：[严重] handleInteractiveInput 缺少 has_pending_interrupt 检查

**Python 参考代码** (`manager.py:386-390`):
```python
if isinstance(payload, InteractiveInput):
    if entry.agent.has_pending_interrupt():
        await entry.agent.resume_interrupt(payload)
        return DeliverResult.success(None)
    return DeliverResult.failure("unsupported_interactive_input")
```

**Go 问题代码** (`manager.go:211-216`):
```go
func (m *TeamRuntimeManager) handleInteractiveInput(...) (*interaction.DeliverResult, error) {
    // ⤵️ 待 9.55 回填: 完整实现
    return interaction.NewDeliverResultSuccess(nil), nil
}
```

**问题分析**：stub 返回 success 而非 failure，但当前无 TeamAgent 可检查中断状态。不应假装成功，否则调用方认为交互输入已处理。

**修复方案**：默认返回 `failure("unsupported_interactive_input")`，待 9.55 回填后实现完整逻辑。

---

### S-05：[严重] DeliverDirect 缺少 send_failed 失败路径

**Python 参考代码** (`router.py:286-293`):
```python
msg_id = await message_manager.send_message(
    content=body,
    to_member_name=target,
    from_member_name=sender,
)
if msg_id is None:
    return DeliverResult.failure(f"send_failed:{target}")
return DeliverResult.success(msg_id)
```

**Go 问题代码** (`router.go:241-248`):
```go
// ⤵️ 待 9.55 回填: messageManager.SendMessage(...)
msgID := "stub-msg-id"
return NewDeliverResultSuccess(&msgID), nil
```

**问题分析**：stub 总是返回成功，但 Python 有 `send_message` 返回 None → `failure` 的分支。测试会遗漏此分支。

**修复方案**：stub 注释中明确标注需实现 `send_failed` 路径。

---

### S-06：[严重] HumanAgentInbox.Send 广播路径缺少 broadcast_failed 检查

**Python 参考代码** (`human_agent_inbox.py:194-200`):
```python
if to in BROADCAST_TARGETS:
    msg_id = await self._mm.broadcast_message(...)
    if msg_id is None:
        return DeliverResult.failure("broadcast_failed")
    return DeliverResult.success(msg_id)
```

**Go 问题代码** (`human_agent_inbox.go:113-118`):
```go
if BroadcastTargets[*to] {
    msgID := "stub-ha-broadcast-msg-id"
    return NewDeliverResultSuccess(&msgID), nil
}
```

**问题分析**：同 S-05，stub 缺少失败路径。

---

### S-07：[严重] UserInbox.Direct 和 Broadcast 缺少 send_failed/broadcast_failed 检查

**Python 参考代码** (`user_inbox.py:49-56`):
```python
msg_id = await self._mm.send_message(...)
if msg_id is None:
    return DeliverResult.failure(f"send_failed:{target}")
```

**Go 问题代码** (`user_inbox.go:56-64`):
```go
msgID := "stub-direct-msg-id"
return NewDeliverResultSuccess(&msgID), nil
```

**问题分析**：同 S-05/S-06。

---

### M-04：[一般] ActiveTeam.SessionID 字段名与 Python 不一致

**Python 参考代码** (`pool.py:48`):
```python
current_session_id: str
```

**Go 问题代码** (`pool.go:16`):
```go
SessionID string    // 应为 CurrentSessionID
```

**问题分析**：外部 API 消费者（CLI、SDK）依赖此字段名，不一致会导致跨语言调用时的字段映射问题。

**修复方案**：将 `SessionID` 改为 `CurrentSessionID`，同步修改 `ActiveTeamInfo.SessionID` → `ActiveTeamInfo.CurrentSessionID`。

---

### M-05：[一般] dispatchPayload 缺少 auto_start_all / auto_start_member 调用

**Python 参考代码** (`manager.py:453-468`):
```python
if isinstance(payload, OperatorMessage):
    inbox = UserInbox(backend.message_manager)
    if payload.target is None:
        await agent.auto_start_all()       # <-- 启动必须在消息发送之前
        result = await inbox.broadcast(payload.body)
        return result
    await agent.auto_start_member(payload.target)  # <-- 同上
    result = await inbox.direct(payload.target, payload.body)
    return result
```

**Go 问题代码** (`manager.go:267-277`):
```go
case *interaction.OperatorMessage:
    inbox := interaction.NewUserInbox(nil)
    if p.Target() == nil {
        // ⤵️ 待 9.55 回填: agent.AutoStartAll()
        return inbox.Broadcast(p.Body())
    }
    // ⤵️ 待 9.55 回填: agent.AutoStartMember(*p.Target())
    return inbox.Direct(*p.Target(), p.Body())
```

**问题分析**：已有 `⤵️` 标注，但需确保回填时**启动在消息发送之前**，保证成员先订阅事件总线再发布消息。

---

### M-06：[一般] HumanAgentInbox 创建时缺少 agentLookup 传递

**Python 参考代码** (`manager.py:470-474`):
```python
inbox = HumanAgentInbox(
    backend,
    backend.message_manager,
    agent_lookup=agent.lookup_human_agent_runtime,
)
```

**Go 问题代码** (`manager.go:281-286`):
```go
hInbox := interaction.NewHumanAgentInbox(
    nil, // ⤵️ 待 9.55 回填: team backend
    nil, // ⤵️ 待 9.55 回填: messageManager
    nil, // ⤵️ 待 9.55 回填: agentLookup
    nil, // ⤵️ 待 9.55 回填: onInbound
)
```

**问题分析**：已有 `⤵️` 标注，回填时需传 `agent.lookup_human_agent_runtime` 作为 agentLookup。

---

### M-07：[一般] Python runtime 包有 3 个文件 Go 未实现

| Python 文件 | 功能 | Go 状态 |
|------------|------|---------|
| `dispatch.py` | `RunActionKind` / `decide_run_action` 分发决策 | ❌ 未实现 |
| `metadata.py` | session checkpoint 命名空间读写 | ❌ 未实现 |
| `team_plan.py` | plan 模式配置辅助 | ❌ 未实现 |

**问题分析**：这些是 `activate` / `finalize` 完整实现所必需的。当前 stub 标注了 `⤵️ 待 9.62`，合理。

---

### M-08：[一般] Activate/Finalize/DeleteTeam 签名与 Python 不同

**Python 参考代码**:
```python
async def activate(self, spec: "TeamAgentSpec", session, inputs=None) -> TeamRuntimeActivation
async def finalize(self, *, team_name, session_id) -> None
async def delete_team(self, team_name, session_ids: list[str], *, force: bool = False) -> bool
```

**Go 问题代码**:
```go
func (m *TeamRuntimeManager) Activate(ctx, teamName, sessionID string, agent any) error
func (m *TeamRuntimeManager) Finalize(ctx, teamName, sessionID string) error
func (m *TeamRuntimeManager) DeleteTeam(ctx, teamName, sessionID string) (bool, error)
```

**问题分析**：Activate 缺少 `spec` 参数和 `TeamRuntimeActivation` 返回值；Finalize 缺少 `FinalizeMember` 方法；DeleteTeam 缺少 `sessionIDs []string` 和 `force bool` 参数。待 9.62 实现时需对齐。

---

### M-09：[一般] DeliverToLeader 使用 context.Background() 而非传入 ctx

**Go 问题代码** (`user_inbox.go:96-114`):
```go
func DeliverToLeader(deliverInput func(ctx context.Context, content string) error, body string) *DeliverResult {
    ctx := context.Background()
    if err := deliverInput(ctx, body); err != nil {
```

**问题分析**：`DeliverToLeader` 无法响应取消信号，上层 `Interact` 方法有 `ctx` 但未传递。

**修复方案**：为 `DeliverToLeader` 添加 `ctx context.Context` 参数。

---

### M-10：[一般] Python 的 TeamRuntimeManager 有 finalize_member，Go 完全缺失

**Python 参考代码** (`manager.py:171-229`):
```python
@staticmethod
async def finalize_member(*, member_name, team_name, session_id) -> None:
```

**问题分析**：finalize_member 处理非 leader 成员的暂停/停止决策，Go 完全缺失。待 9.62 实现时补上。

---

### I-03：[提示] RuntimeState 缺少 String() 方法

**Go 问题代码** (`pool.go:63-70`):
```go
type RuntimeState int
const (
    RuntimeStateRunning RuntimeState = iota
    RuntimeStatePaused
)
```

**问题分析**：Python 的 RuntimeState 是 `str, Enum`，值自然为 `"running"` / `"paused"`。Go 用 iota 无 String()，日志输出为数字。

**修复方案**：添加 `String()` 方法返回 `"running"` / `"paused"`。

---

### I-04：[提示] isReservedMemberName 与 IsReservedName 功能重复

**Go 问题代码** (`payload.go:222-226`):
```go
func isReservedMemberName(name string) bool {
    return agentteams.ReservedMemberNames[name]
}
```

**问题分析**：`router.go` 中有导出的 `IsReservedName` 功能相同。`isReservedMemberName` 没有测试也没有调用者。

**修复方案**：删除 `isReservedMemberName`，统一使用 `IsReservedName`。

---

### I-05：[提示] HumanAgentInboundEvent.Timestamp 单位未标注

**问题分析**：Python 的 `timestamp` 是 `int`（毫秒级），Go 用 `int64`。应在注释中明确标注单位是毫秒。

---

### I-06：[提示] stop_team/pause 参数顺序需标注

**问题分析**：Python 使用 keyword-only 参数（`*, team_name, session_id`），Go 自然无此约束。应在注释中标注参数与 Python keyword 的对应关系。

---

### I-07：[提示] Interaction 包缺少 __init__.py 等价的导出汇总

**问题分析**：Python 的 `__init__.py` 定义了 `__all__` 导出列表。Go 包的导出由首字母大写决定，这是 Go 的正常模式，但需注意文档完整性。

---

## 三、9.72a InstructionOptimizer（1 严重 / 4 一般 / 3 提示）

### S-08：[严重] Step() 缺少异常处理 — Python 的 try/except 未复刻

**Python 参考代码** (`base.py:129-140`):
```python
def step(self) -> Dict[tuple[str, str], Any]:
    self._validate_parameters()
    try:
        updates = self._step()
        self.clear_trajectories()
        return updates or {}
    except Exception as e:
        self.clear_trajectories()
        raise build_error(
            StatusCode.TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR,
            error_msg=f"{str(e)}", cause=e
        ) from e
```

**Go 问题代码** (`instruction_optimizer.go:158-163`):
```go
func (o *InstructionOptimizer) Step() map[schema.UpdateKey]any {
    o.ValidateParameters()
    updates := o.step()
    o.ClearTrajectories()
    return updates
}
```

**问题分析**：Python 用 try/except 保证 `clear_trajectories()` 始终被调用，并将错误包装为 `TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR`。Go 完全缺失异常处理，如果 `step()` 内部 panic：
1. `ClearTrajectories()` 永远不被调用（轨迹泄漏）
2. 错误没有被包装为特定错误码

**修复方案**：
```go
func (o *InstructionOptimizer) Step() (map[schema.UpdateKey]any, error) {
    o.ValidateParameters()
    updates, err := o.step()
    // 对齐 Python: 无论成功失败都清理轨迹
    o.ClearTrajectories()
    if err != nil {
        return nil, fmt.Errorf("%w: %w", ErrOptimizerUpdateExecution, err)
    }
    if updates == nil {
        return map[schema.UpdateKey]any{}, nil
    }
    return updates, nil
}
```

---

### M-11：[一般] SelectSignals 中 score 判断的值类型不安全

**Python 参考代码** (`instruction_optimizer.py:67`):
```python
if context.get("score", 1) == 0 or signal.signal_type in failure_signal_types:
```

**Go 问题代码** (`instruction_optimizer.go:115`):
```go
if score, ok := ctx["score"]; ok && score == 0 {
```

**问题分析**：Go 中 `any` 类型与 `0` 比较时，只有值恰好是 `int(0)` 才为 true。JSON 反序列化后 score 可能是 `float64(0)` 或 `json.Number("0")`，不会匹配，导致信号被错误过滤。

**修复方案**：
```go
func isScoreZero(v any) bool {
    switch s := v.(type) {
    case int:
        return s == 0
    case int64:
        return s == 0
    case float64:
        return s == 0
    case json.Number:
        n, _ := s.Int64()
        return n == 0
    default:
        return false
    }
}
```

---

### M-12：[一般] formatBadCases 中 context 值类型断言不安全

**Python 参考代码** (`instruction_optimizer.py:200-206`):
```python
ctx = signal.context or {}
formatted = CREATE_BAD_CASE_TEMPLATE.format({
    "question": ctx.get("question", ""),
    "label": ctx.get("label", ""),
    "answer": ctx.get("answer", ""),
    "reason": ctx.get("reason", ""),
})
```

**Go 问题代码** (`instruction_optimizer.go:519-522`):
```go
question, _ := ctx["question"].(string)
label, _ := ctx["label"].(string)
answer, _ := ctx["answer"].(string)
reason, _ := ctx["reason"].(string)
```

**问题分析**：JSON 反序列化后这些值可能是非 string 类型，断言失败取空字符串，丢失信息。Python 的 `ctx.get("question", "")` 对任何类型都能参与模板格式化。

**修复方案**：
```go
func anyToString(v any) string {
    if v == nil { return "" }
    if s, ok := v.(string); ok { return s }
    return fmt.Sprintf("%v", v)
}

question := anyToString(ctx["question"])
```

---

### M-13：[一般] Backward 中 LLM 调用失败时 continue 静默跳过，Python 会抛异常

**Python 参考代码** (`base.py:119-127`):
```python
async def backward(self, signals):
    self._validate_parameters()
    self._selected_signals = self._select_signals(signals)
    try:
        await self._backward(signals)
    except Exception as e:
        raise build_error(...) from e
```

**Go 问题代码** (`instruction_optimizer.go:200-209`):
```go
gradient, err := o.generateTextualGradient(ctx, op)
if err != nil {
    logger.Error(logComponent).Str("method", "backward").Str("operator_id", opID).Err(err).
        Msg("[optimizer] 生成文本梯度失败")
    continue  // 静默跳过！
}
```

**问题分析**：Go 的 `continue` 导致：
1. 部分 signal 丢失，该 operator 的本轮优化完全跳过
2. 调用方不知道有 LLM 调用失败，误以为 backward 成功完成

**修复方案**：至少将第一个 LLM 调用失败作为错误返回，或累积错误在遍历结束后返回 aggregate error。

---

### M-14：[一般] Gradients 字段类型从 `map[string]any` 收窄为 `map[string]string`

**Python 参考代码** (`base.py:176`):
```python
self.gradients: Dict[str, Any] = {}  # target -> gradient value (str or list)
```

**Go 问题代码** (`base.go:72`):
```go
Gradients map[string]string
```

**问题分析**：Python 的 gradients 支持任意类型（str、list、None等），Go 只支持 string。当前 InstructionOptimizer 只用了字符串梯度，功能上一致。但 `set_gradient("system_prompt_optimized", None)` 在 Python 中设置 None，Go 用空字符串代替——在 step() 判断是否有优化结果时行为一致。若后续有 ExampleOptimizer 需存储 list，当前设计无法扩展。

**修复方案**：保持现状但添加注释说明限制。如果后续需要扩展，改为 `map[string]any`。

---

### I-08：[提示] extractTag 中 regexp.MustCompile 每次调用都重新编译

**Go 问题代码** (`instruction_optimizer.go:656`):
```go
pattern := regexp.MustCompile(fmt.Sprintf(`(?s)<%s>(.*?)</%s>`, regexp.QuoteMeta(tag), regexp.QuoteMeta(tag)))
```

**修复方案**：用 `sync.Map` 缓存编译后的正则。

---

### I-09：[提示] getPromptTemplate 中 PromptTemplate 第一个参数传空字符串

**Go 问题代码** (`base.go:78`):
```go
return prompt.NewPromptTemplate("", v)
```

**问题分析**：需确认 `NewPromptTemplate(name, content)` 的 name 参数是否影响 `ToMessages()` 的行为。如果 name 对应 message role，应传入正确的 role。

---

### I-10：[提示] restorePlaceholders 中 NewPromptAssembler 失败时静默返回优化后 prompt

**Go 问题代码** (`instruction_optimizer.go:556-561`):
```go
originalAssembler, err := prompt.NewPromptAssembler(originalPrompt)
if err != nil {
    return optimizedPrompt, nil  // 静默吞掉错误
}
```

**修复方案**：添加 Warn 日志，让开发者知道解析失败。

---

## 四、9.72b ToolOptimizer（2 严重 / 1 一般 / 0 提示）

### S-09：[严重] BeamSearch.expand() 并行模式下 DATA RACE

**Python 参考代码** (`beam_search.py:126-127`):
```python
new_node.parent = node
node.children.append(new_node)
```

**Go 问题代码** (`beam_search.go:370`):
```go
res.parent.Children = append(res.parent.Children, res.node)
```

**问题分析**：并行模式下多个 goroutine 可能同时 append 到同一个 parent 的 Children，**存在 DATA RACE**。虽然 Go 代码有注释说"统一设置父子关系（避免 DATA RACE）"，但实际上 `expandParallel` 中仍然是直接 append，不是线程安全的。

**复杂流程示例**：
```
并行扩展 parent 节点 P:
  goroutine-1: P.Children = append(P.Children, child1)  // 读 P.Children, 写新切片
  goroutine-2: P.Children = append(P.Children, child2)  // 同时读 P.Children, 写新切片
  → DATA RACE: 两个 goroutine 读到相同的底层数组，写入时互相覆盖
```

**修复方案**：将 `res.parent.Children` 的修改移到收集结果的串行循环中：
```go
// 并行生成结果
results := m.expandParallel(ctx, nodes, ...)

// 串行设置父子关系
for _, res := range results {
    res.node.parent = res.parent
    res.parent.Children = append(res.parent.Children, res.node)
}
```

---

### S-10：[严重] BeamSearch 早停分支可能返回空结果

**Python 参考代码** (`beam_search.py:88`):
```python
best_nodes = [root]
# ... 早停触发时:
nodes_sorted = sorted(best_nodes, ...)
return [node.history for node in nodes_sorted]  # 不过滤 depth
```

**Go 问题代码** (`beam_search.go:221`):
```go
// 早停分支
bestNodes = sortAndTakeTopK(bestNodes, bs.topK)
// ... 最终返回路径:
// filtered by depth > 0
```

**问题分析**：Python 早停时不过滤 depth，Go 的最终返回路径过滤了 `depth > 0`。如果 `bestNodes` 里只有 root（depth=0），`sortAndTakeTopK` 后全部被 `depth > 0` 过滤掉，返回空结果。

**修复方案**：早停分支单独返回，不走 `depth > 0` 过滤逻辑（与 Python 一致）。

---

### M-15：[一般] SimpleAPIWrapperFromCallable.Call 返回值格式与 Python 不一致

**Python 参考代码** (`customized_api.py:87-88`):
```python
output = fn(params)
return json.dumps({'response': output}, ensure_ascii=False), 0
```

**Go 问题代码** (`api_wrapper.go:83-86`):
```go
output, err := w.callable(tool, toolInput)
if err != 0 {
    return output, err
}
return output, 0
```

**问题分析**：Python 的 callable 返回原始数据，由 wrapper 统一包装为 `{'response': output}`；Go 的 callable 需要调用者自己封装。下游代码（如 `SimpleEval.evaluateSingleExample`）期望 JSON 格式是 `{'response': ...}` 或 `{'error': ...}`。

**修复方案**：让 `APIWrapperFunc` 返回原始数据（`any`），由 wrapper 统一包装，或文档说明接口约定。

---

## 五、9.70-9.80 Evolving 通用模块（3 严重 / 8 一般 / 7 提示）

### S-11：[严重] Updater.Update/Process 返回类型不支持候选列表

**Python 参考代码** (`protocol.py:48`):
```python
async def update(...) -> Union[Dict[tuple[str, str], Any], List[Dict[tuple[str, str], Any]]]:
    ...

# trainer.py:201-211
if isinstance(updated, list):
    val_score, val_evaluated = self._select_best_candidate_on_val(...)
else:
    updates: Updates = updated or {}
    self.apply_updates(operators, updates)
    val_score, val_evaluated = self.evaluate(agent, val_cases)
```

**Go 问题代码** (`trainer.go:223-225`):
```go
updates := normalizeUpdates(updated)
ApplyUpdates(operators, updates)
valScore, valEvaluated, _ = t.Evaluate(ctx, agent, valCases)
```

**问题分析**：Go 只支持单个更新映射，不支持候选列表。Trainer.train() 中对 `updated` 是否为 list 的分支判断被完全省略。当前有 `⤵️ 待 9.72 Optimizer 回填：候选列表多方案评估` 的注释，但核心接口签名需要先修改。

**修复方案**：Update/Process 返回类型应支持候选列表。定义 `UpdateResult` 类型：
```go
type UpdateResult struct {
    Single    map[schema.UpdateKey]any
    Candidates []map[schema.UpdateKey]any
}
```
在 Trainer 中通过类型判断区分单映射和候选列表路径。

---

### S-12：[严重] GetTeamTrajectoryIssues 类型断言可能失败

**Python 参考代码** (`signal/team_signal.py`):
```python
def get_team_trajectory_issues(signal):
    context = signal.context or {}
    issues = context.get(_TEAM_TRAJECTORY_ISSUES_KEY)
    if not isinstance(issues, list):
        return []
    return [item for item in issues if isinstance(item, dict)]
```

**Go 问题代码** (`team_signal.go`):
```go
func GetTeamTrajectoryIssues(sig *EvolutionSignal) []map[string]string {
    ctx := sig.Context
    issues, ok := ctx[teamTrajectoryIssuesKey]
    slice, ok := issues.([]map[string]string)  // 这会失败如果实际类型是 []map[string]any
```

**问题分析**：JSON 反序列化后 context 中的 issues 类型是 `[]map[string]any`（因为 JSON unmarshal 默认解码为 `[]interface{}` + `map[string]interface{}`），Go 的类型断言 `issues.([]map[string]string)` 会失败返回 nil，丢失所有轨迹问题数据。

**修复方案**：
```go
func GetTeamTrajectoryIssues(sig *EvolutionSignal) []map[string]string {
    ctx := sig.Context
    if ctx == nil { return nil }
    issues, ok := ctx[teamTrajectoryIssuesKey]
    if !ok { return nil }

    // 处理 []map[string]any (JSON 反序列化结果)
    if rawSlice, ok := issues.([]any); ok {
        var result []map[string]string
        for _, item := range rawSlice {
            if m, ok := item.(map[string]any); ok {
                converted := make(map[string]string)
                for k, v := range m {
                    converted[k] = fmt.Sprintf("%v", v)
                }
                result = append(result, converted)
            }
        }
        return result
    }

    // 也处理 []map[string]string (直接构造的情况)
    if slice, ok := issues.([]map[string]string); ok {
        return slice
    }
    return nil
}
```

---

### S-13：[严重] trainer.ApplyUpdates 与 evolving.ApplyUpdates 同名不同行为

**问题分析**：Go 有两个 `ApplyUpdates` 函数：
1. `trainer.ApplyUpdates(operators, updates)` → 调用 `op.SetParameter(target, update.Payload)`（直接设值）
2. `evolving.ApplyUpdates(operators, updates)` → 调用 `op.ApplyUpdate(target, update)` 返回 `ApplyResult`（带结果验证）

Python 的 `Trainer.apply_updates` 对应路径 1，`execute_updates` 对应路径 2。两者在不同包中功能不同是设计意图，但**同名函数行为不同**极易造成误用。

**修复方案**：重命名其中一个，如 `trainer.ApplyUpdates` → `trainer.ApplyParameterUpdates`，或在文档中明确区分。

---

### M-16：[一般] ExactMatch normalize=false 行为差异

**Python 参考代码** (`exact_match.py:37`):
```python
return 1.0 if str(prediction) == str(label) else 0.0
```

**Go 问题代码** (`exact_match.go:70`):
```go
score = boolToScore(reflect.DeepEqual(prediction, label))
```

**问题分析**：Python 先转字符串再比较（字典的字符串表示可能因 key 顺序不同而不等），Go 用 DeepEqual（真正深度比较）。行为不同但 Go 的方式更合理。

**修复方案**：保持 DeepEqual 但在注释中注明与 Python 的差异。

---

### M-17：[一般] MetricEvaluator 缺少 float 分支处理

**Python 参考代码** (`evaluator.py:233-244`):
```python
out = metric.compute(predict, case.label, question=case.inputs, case=case)
if isinstance(out, dict):
    for k, v in out.items():
        per_metric[k] = self._safe_convert(v)
else:
    score = self._safe_convert(out)  # float 分支
    per_metric[metric.name] = score
```

**Go 问题代码**：`MetricResult` 定义为 `map[string]float64`，只有 dict 分支。

**问题分析**：当前所有 Metric 实现都返回 map，功能上覆盖了 Python 的 dict 分支。但丢失了 float 分支的灵活性。

**修复方案**：保持现状，确认所有 Metric 实现都返回 map。

---

### M-18：[一般] Case 缺少 inputs/label 非空校验

**Python 参考代码**:
```python
inputs: Dict[str, Any] = Field(..., min_length=1, description="Input data")
label: Dict[str, Any] = Field(..., min_length=1, description="Expected answer")
```

**Go 实现**：`NewCase` 不校验 inputs/label 是否为空。

**修复方案**：在 `NewCase` 中添加非空校验。

---

### M-19：[一般] ConversationSignalDetector.Detect 不支持 Trajectory 输入

**Python 参考代码** (`conversation_signal.py`):
```python
def detect(self, trajectory_or_messages):
    if isinstance(trajectory_or_messages, Trajectory):
        messages = self.convert_trajectory_to_messages(trajectory_or_messages)
        signals.extend(self._detect_from_messages(messages))
        signals.extend(self._detect_collaboration_signals(trajectory_or_messages))
    else:
        signals.extend(self._detect_from_messages(trajectory_or_messages))
    return self._deduplicate(signals)
```

**Go 问题代码**：
```go
func (d *ConversationSignalDetector) Detect(msgs []map[string]any) []*EvolutionSignal {
    signals := d.detectFromMessages(msgs)
    return d.deduplicate(signals)
}
```

**问题分析**：Python 的 `detect(Trajectory)` 会同时运行 `_detect_from_messages` 和 `_detect_collaboration_signals`，Go 的 `DetectTrajectorySignals` 未完全覆盖此行为。

**修复方案**：在 `Detect` 中支持 Trajectory 输入，或在 `DetectFromTrajectory` 中确保同时调用两个检测方法。

---

### M-20：[一般] matchFailureKeyword 过于激进的排除

**Python 参考代码**:
```python
_FAILURE_KEYWORDS = re.compile(
    r"error(?!\s*=\s*None)|exception|traceback|...", re.IGNORECASE)
```

**Go 问题代码**:
```go
func matchFailureKeyword(content string) bool {
    return failureKeywords.MatchString(content) && !errorEqualsNonePattern.MatchString(content)
}
```

**问题分析**：Go 对整个内容做全局排除，Python 只排除紧跟 `= None` 的 error。例如 `"error: file not found, error = None"` 会被 Go 完全排除，但 Python 只排除后半部分的 `error = None`。

**修复方案**：使用逐匹配验证逻辑（已在 `from_conv.go` 的 `findFailureKeywordIndex` 中实现），在 `matchFailureKeyword` 中也应使用。

---

### M-21：[一般] TeamSignalDetector 构造函数缺少 llm_policy 快捷参数

**Python 参考代码**:
```python
class TeamSignalDetector:
    def __init__(self, llm_policy=None, ...):
```

**Go 实现**：接受 `trajectoryIssueLLMPolicy` 和 `userIntentLLMPolicy` 两个独立策略，缺少 `llm_policy` 快捷参数。

**修复方案**：添加 `WithLLMPolicy` 选项函数作为快捷方式同时设置两个策略。

---

### M-22：[一般] DefaultEvaluator 模板格式化方式不同

**问题分析**：Python 使用 `LLM_METRIC_TEMPLATE.format({"user_metrics": metric})` 二次格式化，Go 版本先 `Format` 创建新模板，后续再 `Format` 填充。逻辑一致但有防御性回退（`tmpl == nil` 时用原始模板）。合理适配，非 bug。

---

### M-23：[一般] NormalizeUpdateValue 防御性回填差异

**问题分析**：Go 对已是 `UpdateValue` 的 value 回填空 `Mode`/`Effect`/`Metadata`，Python 不回填。差异原因：Go struct 零值中 Mode/Effect 为空字符串，Python dataclass 默认 `mode=REPLACE_MODE`。Go 的回填是正确的适配。

---

### I-11：[提示] Evaluator.Evaluate 签名不一致 — Go 返回 error

**问题分析**：Go 惯用 error 返回值，Python 不返回 error 但在内部设置 reason。实际行为一致。保持现状。

---

### I-12：[提示] safeConvert 类型差异

**问题分析**：Go 的 `safeConvert` 只接受 `float64`，依赖 `Metric.Compute` 返回 `map[string]float64`。Go 的强类型在编译期保证安全，不需要运行时回退。保持现状。

---

### I-13：[提示] Case.inputs/label 校验差异

**问题分析**：已在 M-18 中描述。提示级别记录此差异。

---

### I-14：[提示] from_evaluated_case 中 Python skill_name 用 None/空处理

**问题分析**：Go 用 `*string` 指针，空字符串时为 nil。行为等价于 Python 的 `operator_id or None`。

---

### I-15：[提示] context 中 Python 用 str()，Go 用 fmt.Sprintf("%v")

**问题分析**：对嵌套 map 输出格式可能不同，但语义等价。保持现状。

---

### I-16：[提示] batch_evaluate 只接受 []Case

**问题分析**：Python 还接受 CaseLoader，Go 只接受 `[]Case`。调用方已先转换为 `[]Case`，功能等价。

---

### I-17：[提示] Trainer.Predict 缺少进度条

**问题分析**：Python 有 tqdm 进度条，Go 没有。UI 差异，非逻辑问题。

---

## 六、10.3.12 AgentManager（2 严重 / 4 一般 / 6 提示）

### S-14：[严重] ProcessMessage/ProcessMessageStream 中 switch_mode 逻辑不应在 AgentManager 中

**Python 参考代码** (`agent_manager.py:438-453`):
```python
async def process_message(self, request: Any) -> Any:
    try:
        channel_id = getattr(request, "channel_id", "")
        params = getattr(request, "params", {})
        mode_full = params.get("mode", "agent.plan")
        mode = str(mode_full).split(".")[0] if mode_full else "agent"
        workspace_dir = params.get("workspace_dir")
        agent = await self.get_agent(channel_id=channel_id, mode=mode, project_dir=workspace_dir)
        if agent is None:
            raise RuntimeError(f"[AgentManager] No agent available for channel {channel_id}")
        return await agent.process_message(request)
    except Exception as e:
        logger.error(f"[AgentManager] Error in process_message: {e}", exc_info=True)
        raise
```

**Go 问题代码** (`agent_manager.go:299-305`):
```go
// code 模式 switchMode（排除 team 子模式，对齐 Python sub_mode != "team"）
if mode == "code" && subMode != "team" {
    sid := ""
    if request.SessionID != nil {
        sid = *request.SessionID
    }
    _ = agent.SwitchMode(ctx, sid, subMode)
}
```

**问题分析**：Python 中 switch_mode 是在 `agent_ws_server.py:1145-1154` 中由 WSServer 执行的，不属于 AgentManager 的职责。Go 把 switch_mode 放在 AgentManager 中违反了职责分离，且当前的 SwitchMode 调用过于简化（缺少 pre_run/postRun 等步骤）。

**修复方案**：删除 AgentManager 的 ProcessMessage 和 ProcessMessageStream 中的 SwitchMode 逻辑。SwitchMode 应在 WSServer 层实现。

---

### S-15：[严重] ProcessMessage/ProcessMessageStream 不应传 subMode 给 GetAgent

**Python 参考代码** (`agent_manager.py:445-448`):
```python
agent = await self.get_agent(
    channel_id=channel_id,
    mode=mode,
    project_dir=workspace_dir,
)
# 注意：不传 sub_mode
```

**Go 问题代码** (`agent_manager.go:287`):
```go
agent, err := am.GetAgent(ctx, channelID, mode, projectDir, subMode)
```

**问题分析**：Python 中同一 mode 下的不同 sub_mode 共享同一个 Agent 实例，sub_mode 仅用于 switch_mode 切换，不参与 agent 缓存区分。Go 传了 subMode，导致不同的 sub_mode（如 `plan` vs `normal`）创建不同的 cache key 和 Agent 实例，与 Python 行为不一致。

**复杂流程示例**：
```
Python 行为：
  channel_id="ch1", mode="code", sub_mode="plan"
  → cache_key = ("ch1", "code", project_dir)
  → 同一 Agent 实例，仅 switch_mode 切换

Go 行为（当前）：
  channel_id="ch1", mode="code", subMode="plan"
  → cache_key = ("ch1", "code", "plan", project_dir)
  → 新 Agent 实例

  channel_id="ch1", mode="code", subMode="normal"
  → cache_key = ("ch1", "code", "normal", project_dir)
  → 又一个新 Agent 实例

  → 资源浪费，且两个实例状态不共享
```

**修复方案**：ProcessMessage 和 ProcessMessageStream 中调用 GetAgent 时不传 subMode（传空字符串），与 Python 一致。sub_mode 仅在 WSServer 层用于 switch_mode 调用。

---

### M-24：[一般] ProcessMessage/ProcessMessageStream 缺少顶层异常日志

**Python 参考代码**:
```python
except Exception as e:
    logger.error(f"[AgentManager] Error in process_message: {e}", exc_info=True)
    raise
```

**Go 问题代码**：如果 `agent.ProcessMessage()` 返回错误，没有在 AgentManager 层记录错误日志，直接向上抛出。

**修复方案**：
```go
resp, err := agent.ProcessMessage(ctx, request)
if err != nil {
    logger.Error(amLogComponent).Err(err).Str("channel_id", channelID).
        Msg("[AgentManager] Error in process_message")
    return nil, err
}
return resp, nil
```

---

### M-25：[一般] ReloadAgentsConfig 对 envOverrides nil 处理有 nil pointer 风险

**Python 参考代码**:
```python
self._latest_env_overrides = dict(env) if isinstance(env, dict) else {}
```

**Go 问题代码** (`agent_manager.go:376`):
```go
if err := entry.agent.ReloadAgentConfig(configPayload, envOverrides); err != nil {
```

**问题分析**：当 `envOverrides == nil` 时，传入 nil。Python 侧传入空 dict `{}`。虽然当前 `ReloadAgentConfig` 可能处理了 nil，但不应假设。

**修复方案**：
```go
envForReload := envOverrides
if envForReload == nil {
    envForReload = map[string]any{}
}
```

---

### M-26：[一般] CancelAllInflightWork 缺少 reason 参数

**Python 参考代码**:
```python
async def cancel_all_inflight_work(self, reason: str = "[gateway ws disconnect] ") -> None:
    for agent in ...:
        await agent.cancel_inflight_work(reason)
```

**Go 问题代码**:
```go
func (am *AgentManager) CancelAllInflightWork(ctx context.Context) error {
    _ = agent.CancelInflightWork()  // 不传 reason
}
```

**修复方案**：添加 reason 参数并传递。

---

### M-27：[一般] Cleanup 缺少 latestEnvOverrides 清理

**Go 问题代码** (`agent_manager.go:512-513`):
```go
am.agentCreateParams = make(map[string]map[string]*agentCreateParamsEntry)
am.clientCapabilitiesByChannel = make(map[string]map[string]any)
// 缺少: am.latestEnvOverrides = make(map[string]any)
```

**修复方案**：添加 `am.latestEnvOverrides = make(map[string]any)`。

---

### I-18：[提示] CancelAllInflightWork 忽略每个 agent 的错误

**Go 问题代码**:
```go
_ = agent.CancelInflightWork()
```

**修复方案**：添加错误日志：
```go
if err := agent.CancelInflightWork(reason); err != nil {
    logger.Warn(amLogComponent).Err(err).Msg("[AgentManager] cancel_inflight_work failed")
}
```

---

### I-19：[提示] RecreateAgent 中 Cleanup 忽略错误

**修复方案**：同 I-18，添加错误日志。

---

### I-20：[提示] Cleanup 方法忽略了每个 agent 的 cleanup 错误

**修复方案**：同 I-18/I-19。

---

### I-21：[提示] GetAgent 中 ACP 通道合并逻辑标记 ⤵️ 未实现

**问题分析**：已正确标注 ⤵️，ACP 实现阶段补上。

---

### I-22：[提示] Initialize 方法空实现

**问题分析**：已正确标注 ⤵️，ACP 实现阶段补上。

---

### I-23：[提示] CreateSession 缺少 ACP 通道 UUID 生成

**问题分析**：已正确标注 ⤵️，ACP 实现阶段补上。

---

## 修复优先级建议

### P0 — 立即修复（功能问题/数据损坏风险）

| 编号 | 章节 | 问题 |
|------|------|------|
| S-09 | 9.72b | BeamSearch 并行模式 DATA RACE |
| S-14 | 10.3.12 | switch_mode 不应在 AgentManager 中 |
| S-15 | 10.3.12 | GetAgent 不应传 subMode |
| S-01 | 9.27-9.30 | PlanAgent 缺少默认 Rails |
| S-02 | 9.27-9.30 | ExploreAgent 缺少默认 Rails + read_only |

### P1 — 尽快修复（功能遗漏/行为不一致）

| 编号 | 章节 | 问题 |
|------|------|------|
| S-06 | 9.59b | dispatchPayload 缺少 no_team_backend 检查 |
| S-04 | 9.59b | handleInteractiveInput 缺少中断检查 |
| S-08 | 9.72a | Step() 缺少异常处理 |
| S-11 | 9.70-9.80 | Updater 返回类型不支持候选列表 |
| S-12 | 9.70-9.80 | GetTeamTrajectoryIssues 类型断言失败 |
| S-10 | 9.72b | BeamSearch 早停分支可能返回空 |
| S-13 | 9.70-9.80 | 两个 ApplyUpdates 同名不同行为 |

### P2 — 计划修复（接口不一致/缺失参数）

| 编号 | 章节 | 问题 |
|------|------|------|
| S-03/S-05/S-06/S-07 | 9.59b | stub 缺少失败路径 |
| M-11 | 9.72a | score 类型比较不安全 |
| M-12 | 9.72a | context 值类型断言不安全 |
| M-13 | 9.72a | Backward LLM 失败静默 continue |
| M-01 | 9.27-9.30 | ExploreAgent 缺少 search_via_bash |
| M-02 | 9.27-9.30 | 描述回退语言不一致 |
| M-04 | 9.59b | SessionID → CurrentSessionID |
| M-24 | 10.3.12 | 缺顶层异常日志 |
| M-25 | 10.3.12 | envOverrides nil 风险 |
| M-26 | 10.3.12 | CancelAllInflightWork 缺 reason |
| M-15 | 9.72b | APIWrapper 返回格式不一致 |

### P3 — 后续改进（日志/文档/性能）

所有"提示"级别问题，以及一般级别中的接口命名差异、构造函数参数差异等。
