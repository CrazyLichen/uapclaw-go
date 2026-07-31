# 代码逻辑审查报告 (2026-07-28)

> 审查范围：近7天提交记录（2026-07-22 ~ 2026-07-28），涉及14个功能章节
> 审查方法：逐方法对比 Go 移植代码与 Python 参考项目，重点检查方法签名、步骤完整性、占位代码实现状态
> 审查人员：9组并行审查代理 + 主审查人汇总

---

## 一、审查概要

### 涉及章节

| 章节 | 名称 | 对应 Python 源码 | 审查状态 |
|------|------|-----------------|---------|
| 9.27 | CodeAgent | `openjiuwen/harness/code_agent/` | ✅ 已审查 |
| 9.28 | PlanAgent | `openjiuwen/harness/plan_agent/` | ✅ 已审查 |
| 9.29 | VerificationAgent | `openjiuwen/harness/verification_agent/` | ✅ 已审查 |
| 9.30 | ExploreAgent | `openjiuwen/harness/explore_agent/` | ✅ 已审查 |
| 9.59b | Interaction 层 | `openjiuwen/agent_teams/interaction/` + `runtime/` | ✅ 已审查 |
| 9.72a | InstructionOptimizer | `openjiuwen/agent_evolving/optimizer/llm_call/` | ✅ 已审查 |
| 9.72b | ToolOptimizer | `openjiuwen/agent_evolving/optimizer/tool_call/` | ✅ 已审查 |
| 9.73 | SignalDetector | `openjiuwen/agent_evolving/signal/` | ✅ 已审查 |
| 9.80 | UpdateExecution+Types | `openjiuwen/agent_evolving/update_execution.py` | ✅ 已审查 |
| 10.3.7 | CodeAgentRail | `jiuwenswarm/server/runtime/agent_adapter/` | ✅ 已审查 |
| 10.3.12 | AgentManager | `jiuwenswarm/server/runtime/agent_manager.py` | ✅ 已审查 |
| 10.3.14 | TenantAgentPool | `jiuwenswarm/server/runtime/tenant_agent_pool.py` | ✅ 已审查 |
| 10.3.15-18 | Session 管理 | `jiuwenswarm/server/runtime/session/` | ✅ 已审查 |
| 10.3.2 | JiuWenClaw 门面 | `jiuwenswarm/server/runtime/agent_adapter/` | ✅ 已审查 |

### 问题统计

| 严重程度 | 数量 | 说明 |
|---------|------|------|
| 🔴 严重 | 17 | 功能逻辑 Bug，会导致运行时异常或功能缺失 |
| 🟡 一般 | 24 | 行为差异、步骤缺失、参数丢失等 |
| 🔵 提示 | 20 | 日志/文本/风格差异，不影响功能流程 |

---

## 二、严重问题（🔴 共 14 个）

### S-01: AgentManager.ProcessMessage 传 subMode 导致实例隔离——与 Python 行为不一致

**文件**: `internal/swarm/server/runtime/agent_manager.go:277-280`

**问题描述**: Python `process_message` 只取 `mode = str(mode_full).split(".")[0]`，不传 sub_mode 给 `get_agent`。Go 的 `resolveModeFromRequest` 解析了完整的 `mode + subMode`，并传给 `GetAgent`。这导致 Go 为 `agent.plan` 和 `agent.normal` 创建两个独立的 agent 实例（cache key 不同），而 Python 只按 `mode="agent"` 创建一个实例共享。

**Python 原文** (`agent_manager.py:441-449`):
```python
mode_full = params.get("mode", "agent.plan")
mode = str(mode_full).split(".")[0] if mode_full else "agent"
# ...
agent = await self.get_agent(
    channel_id=channel_id,
    mode=mode,
    project_dir=workspace_dir,
)  # 不传 sub_mode
```

**Go 问题代码**:
```go
mode, subMode := resolveModeFromRequest(request)
projectDir := resolveWorkspaceDirFromRequest(request)
agent, err := am.GetAgent(ctx, channelID, mode, projectDir, subMode)  // 传了 subMode！
```

**影响**: 每个 subMode 创建独立 agent 实例，浪费内存和 LLM 连接，且同一 channel 下 agent 上下文不共享。

**修复方案**: `ProcessMessage` 和 `ProcessMessageStream` 中调用 `GetAgent` 时不传 subMode（传空字符串），对齐 Python 行为：
```go
agent, err := am.GetAgent(ctx, channelID, mode, projectDir, "")
```

---

### S-02: VerificationRail.BeforeModelCall 缺少 task_loop/plan_mode 跳过逻辑

**文件**: `internal/agentcore/harness/rails/subagent/verification_rail.go:170-194`

**问题描述**: Python `before_model_call` 有两个重要的跳过条件：(1) `enable_task_loop` 为 False 时跳过（简单一次性会话不需要验证提醒）；(2) 当前处于 plan mode 时跳过（提醒会干扰 plan agent）。Go 完全没有这两个条件，**每次模型调用都注入验证提醒**。

**Python 原文** (`verification_rail.py:142-153`):
```python
deep_config = getattr(self._agent, "_deep_config", None)
if deep_config is None or not deep_config.enable_task_loop:
    return  # 跳过：非 task loop 模式

if ctx.session is not None:
    try:
        state = self._agent.load_state(ctx.session)
        if getattr(state.plan_mode, "mode", None) == "plan":
            return  # 跳过：plan mode
    except Exception:
        pass
```

**Go 问题代码**:
```go
func (r *VerificationRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
    if r.promptBuilder == nil {
        return nil
    }
    // 缺少 task_loop 检查和 plan_mode 检查
    section := saprompt.PromptSection{...}
    r.promptBuilder.RemoveSection(reminderSectionName)
    r.promptBuilder.AddSection(section)
    ...
}
```

**影响**: plan mode 下注入验证提醒会干扰 PlanAgent 正常工作；非 task_loop 会话中注入则产生无意义噪声。

**修复方案**: (1) 在 `Init` 中保存 agent 引用；(2) 在 `BeforeModelCall` 中添加两个跳过条件：
```go
func (r *VerificationRail) Init(agent agentinterfaces.BaseAgent) error {
    r.agent = agent          // 新增：保存 agent 引用
    r.promptBuilder = agent.SystemPromptBuilder()
    return nil
}

func (r *VerificationRail) BeforeModelCall(...) error {
    // 新增：对齐 Python L142-144
    if r.agent == nil || !r.agent.DeepConfig().EnableTaskLoop {
        return nil
    }
    // 新增：对齐 Python L147-153
    if cbc.Session != nil {
        if state, err := r.agent.LoadState(cbc.Session); err == nil {
            if state.PlanMode.Mode == "plan" {
                return nil
            }
        }
    }
    // ... 原有注入逻辑
}
```

---

### S-03: VerificationRail.Init 未保存 agent 引用

**文件**: `internal/agentcore/harness/rails/subagent/verification_rail.go:153-158`

**问题描述**: 与 S-02 关联。Python `VerificationRail.init` 保存了 `self._agent = agent`，用于 `before_model_call` 访问 `_deep_config` 和 `load_state`。Go 的 `Init` 只保存了 `promptBuilder`，没有保存 agent 引用。

**Python 原文** (`verification_rail.py:121-122`):
```python
self._agent = agent
self.system_prompt_builder = agent.system_prompt_builder
```

**Go 问题代码**:
```go
func (r *VerificationRail) Init(agent agentinterfaces.BaseAgent) error {
    r.promptBuilder = agent.SystemPromptBuilder()
    // 缺少: r.agent = agent
    return nil
}
```

**修复方案**: 在 VerificationRail 结构体中添加 `agent agentinterfaces.BaseAgent` 字段，并在 Init 中赋值（见 S-02 修复方案）。

---

### S-04: Interaction manager.dispatchPayload 缺少 backend==nil 前置检查

**文件**: `internal/agent_teams/runtime/manager.go:251-265`

**问题描述**: Python `_dispatch_payload` 第一步检查 `backend is None and not isinstance(payload, GodViewMessage)` → 返回 `failure("no_team_backend")`。Go 完全跳过了这个检查，导致 backend 为 nil 时 OperatorMessage/HumanAgentMessage 会创建空 inbox 执行 stub 逻辑而不报错。

**Python 原文** (`manager.py:442-444`):
```python
backend = agent.team_backend
if backend is None and not isinstance(payload, GodViewMessage):
    return DeliverResult.failure("no_team_backend")
```

**Go 问题代码**:
```go
func (m *TeamRuntimeManager) dispatchPayload(...) (*interaction.DeliverResult, error) {
    switch p := payload.(type) {
    case *interaction.GodViewMessage:
        // 直接处理，没有 backend 检查
    case *interaction.OperatorMessage:
        inbox := interaction.NewUserInbox(nil) // nil backend 直接通过
```

**修复方案**: 在 dispatchPayload 的 switch 之前添加 backend nil 检查：
```go
// 对齐 Python: if backend is None and not isinstance(payload, GodViewMessage)
if backend == nil {
    if _, isGodView := payload.(*interaction.GodViewMessage); !isGodView {
        return interaction.DeliverResultFailure("no_team_backend"), nil
    }
}
```

---

### S-05: Interaction manager.dispatchPayload 中 agentLookup 为 nil——avatar 驱动路径不可用

**文件**: `internal/agent_teams/runtime/manager.go:281-285`

**问题描述**: Python 构造 `HumanAgentInbox` 时传入 `agent_lookup=agent.lookup_human_agent_runtime`，这是 avatar 驱动路径的核心。没有它 `_drive_agent` 永远返回 `agent_unavailable`。Go 传入 `nil`，avatar 驱动路径不可能成功。

**Python 原文** (`manager.py:471-474`):
```python
inbox = HumanAgentInbox(
    backend,
    backend.message_manager,
    agent_lookup=agent.lookup_human_agent_runtime,
)
```

**Go 问题代码**:
```go
hInbox := interaction.NewHumanAgentInbox(
    nil, // ⤵️ 待 9.55 回填: team backend
    nil, // ⤵️ 待 9.55 回填: messageManager
    nil, // ⤵️ 待 9.55 回填: agentLookup
    nil, // ⤵️ 待 9.55 回填: onInbound
)
```

**修复方案**: 9.55 回填时必须从 `entry.Agent` 提取 `agentLookup`。当前应在方法注释中明确说明此路径不可用。

---

### S-06: Interaction manager.dispatchPayload 缺少 auto_start 调用

**文件**: `internal/agent_teams/runtime/manager.go:270-277`

**问题描述**: Python 在 OperatorMessage 分支中，广播前调用 `await agent.auto_start_all()`，点对点前调用 `await agent.auto_start_member(payload.target)`。Go 只用注释标注 `⤵️ 待 9.55 回填`，完全跳过了这些调用。消息写入 bus 后目标成员可能尚未订阅事件，导致消息丢失。

**Python 原文** (`manager.py:455-468`):
```python
if payload.target is None:
    await agent.auto_start_all()
    result = await inbox.broadcast(payload.body)
    return result
await agent.auto_start_member(payload.target)
result = await inbox.direct(payload.target, payload.body)
```

**Go 问题代码**:
```go
if p.Target() == nil {
    // ⤵️ 待 9.55 回填: agent.AutoStartAll()
    return inbox.Broadcast(p.Body())
}
// ⤵️ 待 9.55 回填: agent.AutoStartMember(*p.Target())
return inbox.Direct(*p.Target(), p.Body())
```

**修复方案**: 9.55 回填时从 `entry.Agent` 提取并调用 `AutoStartAll()` / `AutoStartMember()`。

---

### S-07: InstructionOptimizer backward 中 LLM 调用失败时 Go 静默 continue，Python 抛异常中止

**文件**: `internal/evolving/optimizer/llm_call/instruction_optimizer.go:201-209`

**问题描述**: Python `_backward()` 中 LLM 调用失败直接抛异常，通过 `BaseOptimizer.backward()` 的 `try/except` 包裹为 `TOOLCHAIN_OPTIMIZER_BACKWARD_EXECUTION_ERROR`，整个 backward 中止。Go 对每个 operator 的失败都是 `logger.Error + continue`，静默跳过。如果所有 operator 都失败，backward 返回 nil，上层以为成功了。

**Python 原文** (`instruction_optimizer.py:87`):
```python
textual_gradient = await self._generate_textual_gradient(op)
# 无 try/except，异常直接传播
```

**Go 问题代码**:
```go
gradient, err := o.generateTextualGradient(ctx, op)
if err != nil {
    logger.Error(logComponent)...Msg("[optimizer] 生成文本梯度失败")
    continue  // 静默跳过！
}
```

**修复方案**: 至少应在 backward 结束后检查是否有成功的结果。如果所有 operator 都失败，应返回 error：
```go
successCount := 0
for _, op := range operators {
    gradient, err := o.generateTextualGradient(ctx, op)
    if err != nil {
        logger.Error(logComponent)...Msg("[optimizer] 生成文本梯度失败")
        continue
    }
    successCount++
    // ...
}
if successCount == 0 && len(operators) > 0 {
    return fmt.Errorf("backward failed: all operators failed")
}
```

---

### S-08: InstructionOptimizer restorePlaceholders 失败后仍使用可能损坏的 prompt

**文件**: `internal/evolving/optimizer/llm_call/instruction_optimizer.go:408-416`

**问题描述**: Python 中如果 `_restore_placeholders` 抛异常，整个 `_optimize_both` 异常传播，不会设置任何 gradient。Go 只记录警告，继续使用可能缺少占位符的 prompt，最终仍设置 `param.SetGradient("system_prompt_optimized", sysPrompt)`。

**Python 原文** (`instruction_optimizer.py:162-169`):
```python
sys_prompt = await self._restore_placeholders(
    TuneUtils.get_content_string_from_template(system_tpl),
    sys_prompt or "",
) if sys_prompt else None
# 异常直接传播
```

**Go 问题代码**:
```go
if sysPrompt != "" {
    sysPrompt, err = o.restorePlaceholders(ctx, ...)
    if err != nil {
        logger.Warn(logComponent)...Msg("恢复 system_prompt 占位符失败")
        // 继续使用可能损坏的 sysPrompt
    }
}
return sysPrompt, usrPrompt, nil
```

**修复方案**: `restorePlaceholders` 失败时，将对应的 prompt 值置空：
```go
if sysPrompt != "" {
    restored, err := o.restorePlaceholders(ctx, ...)
    if err != nil {
        sysPrompt = ""  // 等效 Python 的 None，避免损坏 prompt 被应用
    } else {
        sysPrompt = restored
    }
}
```

---

### S-09: TextualParameter.Gradients 从 `map[string]any` 缩窄为 `map[string]string`

**文件**: `internal/evolving/optimizer/base.go`

**问题描述**: Python `TextualParameter.gradients` 类型为 `Dict[str, Any]`，梯度值可以是 `str`、`list`、`None`。Go 缩窄为 `map[string]string`，无法存储非字符串类型梯度。这限制了 `TextualParameter` 的通用性——如果未来有优化器需要存储列表类型梯度，将无法复用。

**Python 原文**:
```python
class TextualParameter:
    def __init__(self, operator_id: str):
        self.gradients: Dict[str, Any] = {}  # target -> gradient value (str or list)
```

**Go 问题代码**:
```go
Gradients map[string]string  // 缩窄为 string
```

**修复方案**: 改为 `map[string]any`，`SetGradient`/`GetGradient` 使用 `any` 类型：
```go
Gradients map[string]any

func (p *TextualParameter) SetGradient(name string, gradient any) {
    p.Gradients[name] = gradient
}

func (p *TextualParameter) GetGradient(name string) any {
    return p.Gradients[name]
}
```
调用处空值检查从 `!= ""` 改为 `!= nil`。

---

### S-10: SignalDetector Detect 方法丢失 Trajectory 输入支持

**文件**: `internal/evolving/signal/from_conv.go`

**问题描述**: Python `detect()` 接受 `Union[Trajectory, List[dict]]`，当传入 Trajectory 时会调用 `_detect_collaboration_signals` 检测协作信号。Go `Detect()` 只接受 `[]map[string]any`，完全不支持 Trajectory 输入，丢失了协作信号检测能力。

**Python 原文** (`from_conv.py`):
```python
def detect(self, trajectory_or_messages: Union[Trajectory, List[dict]]) -> List[EvolutionSignal]:
    signals: List[EvolutionSignal] = []
    if isinstance(trajectory_or_messages, Trajectory):
        messages = self.convert_trajectory_to_messages(trajectory_or_messages)
        signals.extend(self._detect_from_messages(messages))
        signals.extend(self._detect_collaboration_signals(trajectory_or_messages))
    else:
        signals.extend(self._detect_from_messages(trajectory_or_messages))
    return self._deduplicate(signals)
```

**Go 问题代码**:
```go
func (d *ConversationSignalDetector) Detect(msgs []map[string]any) []*EvolutionSignal {
    signals := d.detectFromMessages(msgs)
    return d.deduplicate(signals)
    // 缺失：Trajectory 分支和协作信号检测
}
```

**修复方案**: 添加 `DetectFromTrajectory` 方法：
```go
func (d *ConversationSignalDetector) DetectFromTrajectory(traj *trajectory.Trajectory) []*EvolutionSignal {
    msgs := d.ConvertTrajectoryToMessages(traj)
    signals := d.detectFromMessages(msgs)
    signals = append(signals, d.detectCollaborationSignals(traj)...)
    return d.deduplicate(signals)
}
```

---

### S-11: GetTeamTrajectoryIssues 类型断言序列化后必然失败

**文件**: `internal/evolving/signal/team.go`

**问题描述**: Go `GetTeamTrajectoryIssues` 尝试将 `context["trajectory_issues"]` 断言为 `[]map[string]string`。但经过 JSON 序列化/反序列化后，`[]map[string]string` 会变成 `[]any` → `[]map[string]any`，类型断言必然失败，永远返回 nil。Python 不受影响因为它是动态类型。

**Go 问题代码**:
```go
func GetTeamTrajectoryIssues(sig *EvolutionSignal) []map[string]string {
    // ...
    slice, ok := issues.([]map[string]string)  // JSON 反序列化后必然失败
    if !ok {
        return nil
    }
    return slice
}
```

**修复方案**: 改为先断言 `[]any`，再逐个元素转换：
```go
func GetTeamTrajectoryIssues(sig *EvolutionSignal) []map[string]string {
    if sig == nil || sig.Context == nil {
        return nil
    }
    issues, ok := sig.Context["trajectory_issues"]
    if !ok {
        return nil
    }
    rawSlice, ok := issues.([]any)
    if !ok {
        // 尝试直接断言（未经过序列化的场景）
        if slice, ok := issues.([]map[string]string); ok {
            return slice
        }
        return nil
    }
    var result []map[string]string
    for _, item := range rawSlice {
        if m, ok := item.(map[string]any); ok {
            converted := make(map[string]string, len(m))
            for k, v := range m {
                converted[k] = fmt.Sprintf("%v", v)
            }
            result = append(result, converted)
        }
    }
    return result
}
```

---

### S-12: ToolDescriptionMethod.Step() it>0 时负例传递链路缺失

**文件**: `internal/evolving/optimizer/tool_call/description_method.go:78-80`

**问题描述**: Python `step()` 在 `it>0` 时先加载负例，组装 `{"neg_examples": neg_examples, "examples": examples}` dict 传入 `generate()`。Go 直接传 `examples []ExampleTuple`，完全缺失负例加载和组装逻辑。后续 `CritiqueAllDescriptions` 永远拿不到负例数据。

**Python 原文** (`description_example_method.py:37-42`):
```python
else:
    function_name = tool['name']
    neg_examples = self.get_negative_examples(function_name)
    examples_obtained = {"neg_examples": neg_examples, "examples": examples}
    output = self.generate(tool, examples_obtained, prev_outputs, it)
```

**Go 问题代码**:
```go
} else {
    outputMap = m.Generate(ctx, tool, examples, prevOutputs, it)
    // 缺少：负例加载和 examples_obtained 组装
}
```

**影响链路**:
```
Step() → Generate() → GenerateDescriptionFromDocumentation() → CritiqueAllDescriptions()
```
整个负例传递链路断裂，描述优化完全依赖正例，无法识别负例导致的问题。

**修复方案**:
```go
} else {
    functionName := getToolName(tool)
    negExamples := m.GetNegativeExamples(functionName)
    examplesObtained := map[string]any{
        "neg_examples": negExamples,
        "examples":     examples,
    }
    outputMap = m.Generate(ctx, tool, examplesObtained, prevOutputs, it)
}
```
同时需修改 `Generate` 和 `GenerateDescriptionFromDocumentation` 的 `examples` 参数类型为 `any`。

---

### S-13: ToolDescriptionMethod.GenerateDescriptionFromDocumentation 参数类型不匹配

**文件**: `internal/evolving/optimizer/tool_call/description_method.go:433-444`

**问题描述**: 与 S-12 关联。Python `generate_description_from_documentation` 中 `examples` 参数是 `dict`，通过 `examples["examples"]` 和 `examples["neg_examples"]` 分别提取正负例。Go 的 `examples` 参数是 `[]ExampleTuple`（只有正例），`CritiqueAllDescriptions` 中永远无法获取负例。

**Python 原文** (`description_example_method.py:387-397`):
```python
def generate_description_from_documentation(self, tool, examples, prev_outputs):
    pos = examples["examples"]      # 从 dict 提取正例
    neg = examples["neg_examples"]  # 从 dict 提取负例
    tmp = self.critique_descriptions(tool, pos, prev_outputs)
    tmp_contrast = self.critique_all_descriptions(tool, examples, prev_outputs)
```

**Go 问题代码**:
```go
func (m *ToolDescriptionMethod) GenerateDescriptionFromDocumentation(
    ctx context.Context, tool map[string]any, examples []ExampleTuple, prevOutputs []map[string]any,
) map[string]any {
    pos := examples  // examples 是 []ExampleTuple，不是 dict
    tmp, _ := m.CritiqueDescriptions(ctx, tool, pos, typedPrevOutputs)
    tmpContrast, _ := m.CritiqueAllDescriptions(ctx, tool, pos, typedPrevOutputs)
```

**修复方案**: 修改签名为 `examples map[string]any`，内部提取 `pos` 和 `neg`（见 S-12）。

---

### S-14: ToolOptimizerBase.OptimizeTool 索引用 [-1] 而 Python 用 [0]

**文件**: `internal/evolving/optimizer/tool_call/base.go:149-153`

**问题描述**: Python `optimize_tool` L57 使用 `result_descs[-1][-1][0]`（取第一个 step output），Go 使用 `[-1][-1][-1]`（取最后一个 step output）。

**Python 原文** (`base.py:57`):
```python
latest_description = result_descs[-1][-1][0]["description"]
```

**Go 问题代码**:
```go
lastNode := lastDescBatch[len(lastDescBatch)-1]
if len(lastNode) > 0 {
    lastStep := lastNode[len(lastNode)-1]  // 即 [-1][-1][-1]
```

**修复方案**: 对齐 Python 的 `[0]` 索引：
```go
if len(lastNode) > 0 {
    lastStep := lastNode[0]  // 对齐 Python: result_descs[-1][-1][0]
```

---

### S-15: SessionManager.CancelSessionTask 不等待任务 goroutine 实际完成

**文件**: `internal/swarm/server/runtime/session_manager.go:86-101`

**问题描述**: Python `cancel_session_task` 在 `task.cancel()` 后会 `await task`（或带超时 `await asyncio.wait_for`）等待任务真正完成，确保资源清理。Go 调用 `cancelFn()` 后只做 `time.After` 等待超时，**没有等待任务 goroutine 实际退出**，可能导致资源泄漏。

**Python 原文** (`session_manager.py:52-65`):
```python
task.cancel()
try:
    if wait_timeout is None:
        await task  # ← 等待任务完成
    else:
        await asyncio.wait_for(task, timeout=wait_timeout)
except asyncio.TimeoutError:
    logger.warning(...)
except (asyncio.CancelledError, Exception):
    pass
```

**Go 问题代码**:
```go
cancelFn()
if waitTimeout != nil {
    select {
    case <-time.After(*waitTimeout):
        logger.Warn(...)
    case <-ctx.Done():
    }
}
// ← 没有等待任务 goroutine 实际退出
```

**影响**: 取消任务后 goroutine 可能仍在运行，持有资源（如 LLM 连接、文件句柄），导致资源泄漏或竞态。

**修复方案**: 在 `processSessionQueue` 中，任务执行完成后通过 channel 通知。`CancelSessionTask` 应等待该通知 channel 收到信号：
```go
type sessionTaskEntry struct {
    cancelFn context.CancelFunc
    doneCh   chan struct{}  // 新增：任务完成通知
}

// processSessionQueue 中任务完成时：
defer close(entry.doneCh)

// CancelSessionTask 中：
cancelFn()
if waitTimeout != nil {
    select {
    case <-entry.doneCh:  // 等待任务完成
    case <-time.After(*waitTimeout):
        logger.Warn(...)
    }
}
```

---

### S-16: AppendHistoryRecord 不触发 metadata 更新——message_count/标题永不更新

**文件**: `internal/swarm/server/runtime/session_history.go:51-101`

**问题描述**: Python `append_history_record` 写入后调用 `update_session_metadata(increment_message_count=True, user_content=..., channel_metadata=..., mode=...)` 和 `set_session_delivery_context`。Go `AppendHistoryRecord` 只写 history 记录，完全不同步更新 metadata，导致 message_count、last_message_at、会话标题等字段永不更新。

**Python 原文** (`session_history.py:176-200`):
```python
# 更新会话元数据
update_session_metadata(
    session_id=sid,
    channel_id=cid,
    increment_message_count=True,
    user_content=content_text if role_norm == "user" else None,
    channel_metadata=channel_metadata,
    mode=mode,
)
if role_norm == "user":
    set_session_delivery_context(
        session_id=sid, channel_id=cid,
        source_request_id=rid, route_metadata=channel_metadata,
    )
```

**Go 问题代码**: 无 metadata 更新调用

**影响**: 会话的 message_count 始终为 0，last_message_at 不更新，自动标题生成不触发，delivery context 不设置。

**修复方案**: 在 `AppendHistoryRecord` 入队/同步写入成功后，调用 `updateSessionMetadata` 和 `SetSessionDeliveryContext`。

---

### S-17: UapClaw.ProcessMessageStream 流式任务未通过 SessionManager 队列提交

**文件**: `internal/swarm/server/runtime/uapclaw.go:362-365`

**问题描述**: Python `process_message_stream` 中常规请求通过 `await self._session_manager.submit_task(session_id, run_stream_task)` 提交到 LIFO 队列，保证同 session 内请求串行执行。Go `ProcessMessageStream` 只调了 `EnsureSessionProcessor`，没有 `SubmitTask`，**破坏了 LIFO 语义**，同 session 请求可能并发执行。

**Python 原文** (`interface.py:1013-1014`):
```python
else:
    await self._session_manager.submit_task(session_id, run_stream_task)
```

**Go 问题代码**:
```go
// 9. 提交流式任务
// ⤵️ 10.3.2: Team 后续请求 / Auto-Harness resume 绕过 Session 队列
_ = uc.sessionManager.EnsureSessionProcessor(ctx, sessionID)
// ← 只调了 EnsureSessionProcessor，没有 SubmitTask！
```

**影响**: 同一 session 的多个请求可能并发执行，破坏 LIFO 语义，导致消息乱序或状态不一致。

**修复方案**: 将流式任务包装后通过 `uc.sessionManager.SubmitTask` 提交，与 Python 对齐。Team/AutoHarness 的绕过逻辑应作为条件分支。

---

## 三、一般问题（🟡 共 24 个）

### G-01: AgentManager.Cleanup 忽略 agent.Cleanup() 错误且无日志

**文件**: `internal/swarm/server/runtime/agent_manager.go:480-481`

**Python 原文** (`agent_manager.py:493-497`):
```python
try:
    await agent.cleanup()
except Exception as e:
    logger.warning("[AgentManager] Agent cleanup failed: %s", e)
```

**Go 问题代码**:
```go
for _, entry := range chAgents {
    _ = entry.agent.Cleanup()  // 错误完全忽略，无日志
}
```

**修复方案**: 记录 cleanup 错误日志：
```go
if err := entry.agent.Cleanup(); err != nil {
    logger.Warn(amLogComponent).Err(err).Str("channel_id", chKey).
        Msg("[AgentManager] Agent cleanup failed")
}
```

---

### G-02: AgentManager.CancelAllInflightWork 忽略错误且缺少 reason 参数

**文件**: `internal/swarm/server/runtime/agent_manager.go:466-468`

**Python 原文** (`agent_manager.py:188-191`):
```python
try:
    await agent.cancel_inflight_work(reason)
except Exception:
    logger.exception("[AgentManager] cancel_inflight_work failed")
```

**Go 问题代码**:
```go
for _, agent := range agentsCopy {
    _ = agent.CancelInflightWork()  // 忽略错误，无 reason 参数
}
```

**修复方案**: 添加错误日志记录和 reason 参数支持。

---

### G-03: AgentManager.GetAgent 存在 check-then-act 竞态

**文件**: `internal/swarm/server/runtime/agent_manager.go:142-160`

**问题描述**: RLock 检查 cache miss → RUnlock → createAgent（内部 Lock 写入），两个并发 GetAgent 可能同时发现 cache miss，都进入 createAgent，导致同一 cacheKey 创建两个 agent 实例。Python 无此问题因为 asyncio 单线程。

**修复方案**: 使用 double-check locking：在 RUnlock 后用 Lock 再次检查 cache。

---

### G-04: VerificationAgent description fallback "cn" 应为 "en"

**文件**: `internal/agentcore/harness/rails/subagent/verification_agent.go:236-238`

**Python 原文** (`verification_agent.py:296-297`):
```python
description=VERIFICATION_AGENT_DESC.get(resolved_language, VERIFICATION_AGENT_DESC["en"]),
```

**Go 问题代码**:
```go
desc := defaultVerificationAgentDescription[language]
if desc == "" {
    desc = defaultVerificationAgentDescription["cn"]  // 应 fallback 到 "en"
}
```

**修复方案**: 改为 `defaultVerificationAgentDescription["en"]`，与 Python 对齐。注意其他 4 个 Agent 的 Python fallback 是 "cn"，唯独 VerificationAgent 是 "en"。

---

### G-05: CodeAgent description 英文 prompt 包含多余换行和空格

**文件**: `internal/agentcore/harness/rails/subagent/code_agent.go:39-40`

**Python 原文**:
```python
DEFAULT_CODE_AGENT_DESCRIPTION_EN = """You are a senior software engineer and coding agent, 
    excel at translating tasks into runnable code and verifiable results."""
```
实际字符串含 `\n    `。

**Go 问题代码**:
```go
"en": "You are a senior software engineer and coding agent, " +
    "excel at translating tasks into runnable code and verifiable results.",
```
缺少 `\n    `。

**修复方案**: 添加换行和空格以对齐 Python 原文。

---

### G-06: ExploreAgent/PlanAgent Create 默认 Rail 与 Python 不一致

**文件**: `explore_agent_factory.go:30`, `plan_agent_factory.go:32`

**Python 原文** (`explore_agent.py:238`, `plan_agent.py:166`):
```python
final_rails = rails if rails is not None else [SysOperationRail()]  # 无 read_only
```

**Go 问题代码**:
```go
finalRails = []sainterfaces.AgentRail{rails.NewSysOperationRail(rails.WithReadOnly(true))}
```

**分析**: Go 使用 `WithReadOnly(true)` 是有意增强（双重只读保障），但与 Python `create_explore_agent` / `create_plan_agent` 不一致。

**修复方案**: 如需严格对齐 Python，改为 `rails.NewSysOperationRail()`（无 WithReadOnly）。

---

### G-07: InstructionOptimizer formatBadCases 非 string Content 被丢弃

**文件**: `internal/evolving/optimizer/llm_call/instruction_optimizer.go:534-536`

**Python 原文** (`instruction_optimizer.py:208-211`):
```python
if isinstance(content, str):
    parts.append(content)
elif content:
    parts.append(str(content))  # 非 str 也保留
```

**Go 问题代码**:
```go
if s, ok := formatted.Content.(string); ok {
    parts = append(parts, s)
}
// 非 string Content（如 []BaseMessage）被静默丢弃
```

**修复方案**: 添加对 `[]schema.BaseMessage` 类型 Content 的处理。

---

### G-08: InstructionOptimizer step() 无更新时返回空 map 而非 nil

**文件**: `internal/evolving/optimizer/llm_call/instruction_optimizer.go:291-304`

**Python 原文** (`instruction_optimizer.py:124`):
```python
return updates if updates else None
```

**Go 问题代码**:
```go
updates := make(map[schema.UpdateKey]any)  // 永远不为 nil
return updates
```

**修复方案**: 当没有更新时返回 nil，与 Python 的 None 语义对齐。

---

### G-09: SignalDetector DetectUserMessageFeedback 静默吞掉 error

**文件**: `internal/evolving/signal/from_conv.go`

**Go 问题代码**:
```go
func (d *ConversationSignalDetector) DetectUserMessageFeedback(ctx context.Context, msgs []map[string]any) []*EvolutionSignal {
    signals, _ := d.DetectUserIntent(ctx, msgs)  // error 被忽略
```

**修复方案**: `DetectUserMessageFeedback` 应返回 `([]*EvolutionSignal, error)` 并传播 error。

---

### G-10: SignalDetector FromEvaluatedCase 未使用 MakeEvolutionSignal

**文件**: `internal/evolving/signal/from_eval.go`

**问题描述**: Go 直接构造 `EvolutionSignal`，绕过了 `MakeEvolutionSignal` 的 context 合并逻辑（source/tool_name 合并到 context）。Python 使用 `make_evolution_signal` 统一入口。

**修复方案**: 改用 `MakeEvolutionSignal` + `WithSource("offline_evaluation")` + `WithContext(...)` 构造。

---

### G-11: TeamSignalType 常量未引用 schema 常量

**文件**: `internal/evolving/signal/team.go`

**Python 原文**:
```python
class TeamSignalType(str, Enum):
    USER_INTENT = USER_INTENT_SIGNAL       # 引用 protocols 常量
    TRAJECTORY_ISSUE = TRAJECTORY_ISSUE_SIGNAL
```

**Go 问题代码**:
```go
TeamSignalTypeUserIntent TeamSignalType = "user_intent"         // 硬编码
TeamSignalTypeTrajectoryIssue TeamSignalType = "trajectory_issue"  // 硬编码
```

**修复方案**: 改为引用 schema 常量：
```go
TeamSignalTypeUserIntent TeamSignalType = schema.UserIntentSignal
TeamSignalTypeTrajectoryIssue TeamSignalType = schema.TrajectoryIssueSignal
```

---

### G-12: BeamSearch expand 串行模式失败时 Go 跳过 vs Python 抛 RuntimeError

**文件**: `internal/evolving/optimizer/tool_call/beam_search.go`

**问题描述**: Python `expand` 中如果 `expand_single_step` 无法生成有效节点，直接 `raise RuntimeError`。Go 在 `expandSerial` 中 `expandSingleStep` 失败时 `continue` 跳过。Python 并行模式下某个 future 抛异常也会 `continue`，但串行模式下会整体失败。

**修复方案**: 串行模式下所有 expand 都失败时应返回 error。

---

### G-13: ToolOptimizer Format 使用 evalModelID 而非硬编码 gpt-5.2

**文件**: `internal/evolving/optimizer/tool_call/reviewer.go`

**Python 原文** (`customized_reviewer.py`):
```python
response = get_rits_response('gpt-5.2', prompt, self.llm_api_key, ...)
```

**Go 问题代码**: 使用 `r.evalModelID` 而非硬编码 `'gpt-5.2'`。

**修复方案**: 这可能是 Python 的 bug（应该用 `self.eval_model_id`），需确认。如果 Python 确实有意硬编码 `gpt-5.2`，则 Go 也应硬编码。

---

### G-14: ToolOptimizer llm_api_key 赋值时机差异

**文件**: `internal/evolving/optimizer/tool_call/base.go`

**问题描述**: Python 修改全局 `default_config_desc['llm_api_key']`（所有实例共享），Go 修改实例副本 `b.configDesc["llm_api_key"]`。Go 更安全但与 Python 不一致。

**修复方案**: 保持 Go 实现（更合理），但记录差异。

---

### G-15: Interaction HumanAgentNotEnabledError 错误信息不完整

**文件**: `internal/agent_teams/interaction/human_agent_inbox.go:131-136`

**Python 原文**:
```python
"No human-agent member is registered on this team; "
"create the team with enable_hitt=True or declare "
"TeamMemberSpec(role_type=TeamRole.HUMAN_AGENT, ...) "
"entries in predefined_members"
```

**Go 问题代码**:
```go
return "no human-agent member is registered on this team"
```

**修复方案**: 对齐 Python 的完整指导文本。

---

### G-16: Interaction DeliverToLeader 使用 context.Background() 而非传入 ctx

**文件**: `internal/agent_teams/interaction/user_inbox.go:96-114`

**问题描述**: Python 是 async 函数不需要显式 ctx，Go 应让调用方传入 ctx 以支持超时/取消。

**修复方案**: 添加 ctx 参数。

---

### G-17: Interaction RuntimeState 缺少 String() 方法

**文件**: `internal/agent_teams/runtime/pool.go`

**问题描述**: Python `RuntimeState` 是 `str, Enum`，有自然字符串表示。Go 是 `int` iota，日志中只显示数字。

**修复方案**: 添加 `String()` 方法返回 "running"/"paused"。

---

### G-18: todo 所有返回消息未一比一复刻 Python 英文原文

**文件**: `internal/agentcore/harness/tools/todo/todo.go` 多处

**问题描述**: 根据 feedback 记忆"提示词一比一复刻 Python"，所有用户可见消息应使用 Python 英文原文。当前使用中文翻译。

**Go 问题代码** (多处):
```go
"已成功创建 %d 个任务"          // Python: "Successfully created N task(s)"
"未删除任何任务"                // Python: "No tasks deleted: ..."
"已成功删除 %d 个任务"          // Python: "Successfully deleted N task(s)"
"缺少必填字段: '%s'"           // Python: "Missing required field: '%s'"
"无效的状态 '%s'"              // Python: "Invalid status '%s'. Valid values: ..."
```

**修复方案**: 全部改为 Python 英文原文。

---

### G-19: write_file 空 strings 检查比 Python 更严格

**文件**: `internal/agentcore/harness/tools/filesystem/write_file.go:59-63`

**Python 原文**:
```python
if content is None:  # 只检查 None，允许空字符串
    return ToolOutput(success=False, error="content is required")
```

**Go 问题代码**:
```go
if input.Content == "" {  // 检查空字符串，不允许空文件
    return map[string]any{"success": false, "error": "content is required"}, nil
}
```

**修复方案**: 改为使用 `*string` 类型区分"未提供"和"空字符串"。

---

### G-20: SessionManager.HasActiveTasks 语义差异——只检查 cancelFn 而非任务完成状态

**文件**: `internal/swarm/server/runtime/session_manager.go`

**Python 原文** (`session_manager.py`):
```python
def has_active_tasks(self) -> bool:
    return any(t is not None and not t.done() for t in self._session_tasks.values())
```

**Go 问题代码**:
```go
func (sm *SessionManager) HasActiveTasks() bool {
    for _, cancelFn := range sm.sessionTasks {
        if cancelFn != nil {
            return true
        }
    }
    return false
}
```

**问题描述**: Python 检查 `not t.done()`（任务存在且未完成），Go 只检查 `cancelFn != nil`，不检查任务是否已完成。已完成的任务仍会被报告为 active。

**修复方案**: 增加 `sessionTaskDone map[string]chan struct{}`，任务完成时关闭 channel，`HasActiveTasks` 检查 channel 是否已关闭。

---

### G-21: processSessionQueue 缺少异常路径日志

**文件**: `internal/swarm/server/runtime/session_manager.go`

**问题描述**: Python 处理器循环有 `except Exception as e: logger.error(...)` ，Go 中 `item.task(taskCtx)` 的错误被 `_, _` 丢弃。

**修复方案**: 记录 `item.task` 返回的 error。

---

### G-22: handleSessionRename 初始化 metadata 缺少 user_id 字段

**文件**: `internal/swarm/server/handle_session.go:442-452`

**问题描述**: Python `init_session_metadata` 创建的 metadata 包含 `user_id: ""`，Go 手动构造的 metadata 缺少此字段。

**修复方案**: 添加 `"user_id": ""` 字段。

---

### G-23: updateSessionMetadata 自动标题生成未实现（⤵️ 占位）

**文件**: `internal/swarm/server/handle_session.go`

**问题描述**: Python 当 `not title and user_content` 时调用 `_auto_title(user_content)` 生成标题。Go 标注 `⤵️ 11.x` 占位，功能缺失。会话标题不会自动从首条用户消息生成。

---

### G-24: SessionHistory 缺少 ReadTeamHistoryRecords 和团队记录过滤

**文件**: `internal/swarm/server/runtime/session_history.go`

**问题描述**: Python 有 `read_team_history_records(session_id)` 用 `_is_team_relevant` 过滤出 team 相关记录。Go 只有 `ReadHistoryRecords` 返回全部记录，无过滤功能。

---

## 四、提示问题（🔵 共 20 个）

### T-01: TenantAgentPool GetInstance 缺少 "Initialized with AgentManager" 日志

**文件**: `internal/swarm/server/runtime/tenant_pool.go:57-62`

Python `__init__` 中有 `logger.info("[TenantAgentPool] Initialized with AgentManager")`，Go 缺少。

**修复方案**: 在 factory 函数中 `NewAgentManager()` 之后补加日志。

---

### T-02: TenantAgentPool ResetInstance 缺少重置前日志

**文件**: `internal/swarm/server/runtime/tenant_pool.go:68-70`

Python `reset_instance` 在实例非 None 时记录重置日志，Go 无。

**修复方案**: 在 `Reset()` 前检查实例是否存在并记录日志。

---

### T-03: SignalDetector SelectSignals score 比较对 float64 类型不安全

**文件**: `internal/evolving/signal/signal.go`

**问题描述**: Go 中 `any(0)` 和 `any(0.0)` 是不同类型，`any(0.0) == 0` 为 `false`。

**修复方案**: 使用类型安全的数值比较：
```go
switch v := score.(type) {
case int:    if v == 0 { ... }
case float64: if v == 0.0 { ... }
}
```

---

### T-04: MakeTeamTrajectorySignal excerpt 使用中文未复刻 Python 原文

**文件**: `internal/evolving/signal/team.go`

**Python 原文**: `"Detected team skill trajectory issues requiring evolution."`
**Go 问题代码**: `"检测到团队技能轨迹问题，需要进行进化。"`

**修复方案**: 改为 Python 英文原文。

---

### T-05: ToolOptimizer reviewer.go 缺少 Python 中未使用的英文 prompt 版本

**文件**: `internal/evolving/optimizer/tool_call/reviewer.go`

**问题描述**: Python `format()` 中有 `prompt_original`、`prompt_1`、`prompt_2` 和最终的中文 `prompt`，最终只使用中文 `prompt`。Go 只实现了中文 prompt。逻辑正确但缺英文版本。

**修复方案**: 添加注释说明 Python 中存在未使用的英文版本。

---

### T-06: ToolOptimizer Pipeline 缺少 "EXAMPLE STAGE FINISHED" 日志

**文件**: `internal/evolving/optimizer/tool_call/pipeline.go`

Python 有 `logger.info("=== EXAMPLE STAGE FINISHED ===")`，Go 在 pipeline 层面缺少（但 OptimizeTool 层面有）。

**修复方案**: 无需修改，OptimizeTool 层已有等价日志。

---

### T-07: ToolOptimizer eval.go api_wrapper 为 nil 时 Go 继续执行 vs Python raise

**文件**: `internal/evolving/optimizer/tool_call/eval.go`

Python `_evaluate_single_example` 在 api_wrapper 为 None 时 `raise ValueError`。Go 只记录 error 并继续执行 `evaluateOutputEffectiveness`。

**修复方案**: api_wrapper 为 nil 时应直接返回零值结果。

---

### T-08: ToolOptimizer Format ParseJSON 缺少 True/False/None 替换

**文件**: `internal/evolving/optimizer/tool_call/format.go`

Python `parse_json` 在 `json.loads` 失败时用 `ast.literal_eval`（支持 `True/False/None`）。Go 用单引号替换，不支持 Python 字面量。

**修复方案**: 增加 `True→true`、`False→false`、`None→null` 替换。

---

### T-09: InstructionOptimizer extractTag 每次调用都重新编译正则

**文件**: `internal/evolving/optimizer/llm_call/instruction_optimizer.go`

**修复方案**: 使用 `sync.Map` 缓存编译后的正则。

---

### T-10: CodeAgentRail filterToolCards 同时匹配 tc.ID 和 tc.Name

**文件**: `internal/swarm/server/adapter/code_agent_rail.go:294-295, 313`

Python 只检查 `tc.name`，Go 同时检查 `tc.Name` 和 `tc.ID`，可能额外匹配不该匹配的 ToolCard。

**修复方案**: 移除 `tc.ID` 匹配，只检查 `tc.Name`。

---

### T-11: Interaction BroadcastTargets 用 map[string]bool 而非 frozenset

**文件**: `internal/agent_teams/interaction/router.go:41`

Python 用 `frozenset`（不可变），Go 用 `map[string]bool`（可被外部修改）。

**修复方案**: 改为非导出变量 + 导出查询函数。

---

### T-12: Interaction payload.go isReservedMemberName 与 router.go IsReservedName 重复

**文件**: `internal/agent_teams/interaction/payload.go:218-222`

**修复方案**: 删除 `isReservedMemberName`，统一使用 `IsReservedName`。

---

### T-13: matchFailureKeyword 语义边界差异

**文件**: `internal/evolving/signal/from_conv.go`

**问题描述**: 当内容同时包含 "error = None" 和另一个合法 "error" 匹配时，Go 的全局检查会误判。

**修复方案**: 让 `matchFailureKeyword` 复用 `findFailureKeywordIndex` 的逐匹配验证逻辑。

---

### T-14: UpdateExecution nil 值错误消息 "nil" vs Python "None"

**文件**: `internal/evolving/update_execution.go`

Python: `"update value is None"`，Go: `"update value is nil"`。

**修复方案**: 根据提示词一比一复刻规则，改为 `"update value is None"`。

---

### T-15: todo _update_todos 校验顺序与 Python 不一致

**文件**: `internal/agentcore/harness/tools/todo/todo.go:616-694`

Python 先执行更新后校验，Go 先校验后更新。可能导致 Go 拒绝了 Python 会接受的更新（同时将 A→completed、B→in_progress 的场景）。

**修复方案**: 对齐 Python 顺序——先执行更新，再验证 in_progress 限制。

---

### T-16: SessionHistory ReadHistoryRecords 缺少重试机制

**文件**: `internal/swarm/server/runtime/session_history.go`

**问题描述**: Python `read_team_history_records` 在读到空结果但文件存在时，以 0.2s 递增间隔重试最多 5 次（防御非原子写入的截断窗口）。Go 无重试机制。

---

### T-17: SessionHistory TruncateHistoryRecords 语义差异

**文件**: `internal/swarm/server/runtime/session_history.go`

**问题描述**: Python 按**索引位置**截断 `truncate_history_records(session_id, cut_index)`，Go 按 **request_id** 匹配截断 `TruncateHistoryRecords(sessionID, requestID)`。接口不同，可能是有意设计，但需确认调用方对齐。

---

### T-18: SessionHistory 缺少 _serialize_value 函数

**文件**: `internal/swarm/server/runtime/session_history.go`

**问题描述**: Python `_serialize_value` 将 datetime 对象转 ISO 格式，递归处理 dict/list。Go extra 字段直接展开，如果含 time.Time 等不可 JSON 序列化类型会出错。

---

### T-19: InstructionOptimizer Step() 缺少 TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR 异常包装

**文件**: `internal/evolving/optimizer/llm_call/instruction_optimizer.go:158-163`

**问题描述**: Python `BaseOptimizer.step()` 对 `_step()` 调用包裹了 `try/except`，捕获异常后包装为 `TOOLCHAIN_OPTIMIZER_UPDATE_EXECUTION_ERROR`。Go `Step()` 直接调用 `o.step()` 无异常包装。

---

### T-20: InstructionOptimizer backward 中 per-operator 容错 vs Python 整体失败

**文件**: `internal/evolving/optimizer/llm_call/instruction_optimizer.go:201-209`

**问题描述**: Go `backward` 中对单个 operator 的 `generateTextualGradient`/`optimizeBoth`/`optimizeSingle` 失败采用 `continue` 容错（跳过当前 operator 继续处理其他），Python 中异常传播导致整个 backward 失败。Go 的行为更健壮但与 Python 不一致。

**修复方案**: 需确认这是有意增强还是移植偏差。如是有意增强，建议在代码注释中注明。

---

## 五、待回填占位确认

以下 `⤵️` 标记的代码确认尚未实现，需后续章节完成后回填：

| 文件 | 占位内容 | 回填计划 | 严重性 |
|------|---------|---------|--------|
| manager.go:251-285 | dispatchPayload: backend/agentLookup/autoStart | ⤵️ 9.55 | 功能不可用 |
| manager.go:270-277 | auto_start_all / auto_start_member | ⤵️ 9.55 | 消息可能丢失 |
| manager.go:281-285 | agentLookup / messageManager | ⤵️ 9.55 | avatar 路径不可用 |
| human_agent_inbox.go:164 | names 硬编码 | ⤵️ 9.55 | 多 agent 不支持 |
| agent_manager.go:157-158 | ACP 配置合并 | ⤵️ ACP | ACP 通道不可用 |
| base.go:263 | Bind() 返回 0 | ⤵️ 9.70 | 优化器绑定不完整 |
| uapclaw.go:362-365 | ProcessMessageStream 未 SubmitTask | ⤵️ 10.3.2 | 破坏 LIFO 语义 |
| deep_adapter 多文件 (~50处) | CreateInstance/Rails/Tools/Slash/Evolution/Team/Dreaming/MCP | ⤵️ 各章节 | DeepAdapter 大量步骤待回填 |
| handle_session.go | 自动标题生成 | ⤵️ 11.x | 会话标题不自动生成 |

---

## 六、修复优先级建议

### P0 — 立即修复（功能 bug，影响运行时行为）

| 编号 | 修复工作量 | 说明 |
|------|-----------|------|
| S-01 | 低 | AgentManager ProcessMessage 不传 subMode，一行修改 |
| S-02+S-03 | 中 | VerificationRail 添加跳过逻辑和 agent 引用 |
| S-14 | 低 | OptimizeTool 索引 [0] vs [-1] |
| S-11 | 中 | GetTeamTrajectoryIssues 类型断言修复 |
| S-16 | 中 | AppendHistoryRecord 后调用 updateSessionMetadata |
| S-17 | 中 | ProcessMessageStream 通过 SubmitTask 提交 |

### P1 — 高优先级（逻辑偏差，影响功能正确性）

| 编号 | 修复工作量 | 说明 |
|------|-----------|------|
| S-12+S-13 | 中 | description_method 负例传递链路修复（S-12/S-13/S-14 三联动） |
| S-07 | 中 | InstructionOptimizer backward 错误传播 |
| S-08 | 低 | restorePlaceholders 失败后置空 prompt |
| S-09 | 中 | TextualParameter.Gradients 改 any 类型 |
| S-10 | 中 | SignalDetector 添加 Trajectory 重载方法 |

### P2 — 中优先级（行为差异或信息不完整）

| 编号 | 修复工作量 | 说明 |
|------|-----------|------|
| G-01+G-02 | 低 | AgentManager Cleanup/Cancel 错误日志 |
| G-04 | 低 | VerificationAgent fallback "en" |
| G-18 | 中 | todo 消息一比一复刻英文 |
| G-19 | 中 | write_file 空字符串检查 |
| G-15 | 低 | HumanAgentNotEnabledError 完整信息 |

### P3 — 低优先级（提示/日志/风格）

T-01 ~ T-15 按需逐步修复。

---

## 七、统计

| 严重度 | 数量 | 占比 |
|--------|------|------|
| 🔴 严重 | 17 | 28% |
| 🟡 一般 | 24 | 40% |
| 🔵 提示 | 20 | 33% |
| **合计** | **61** | **100%** |

### 按模块分布

| 模块 | 严重 | 一般 | 提示 |
|------|------|------|------|
| AgentManager (10.3.12) | 1 | 3 | 0 |
| Session 管理 (10.3.15-18) | 3 | 5 | 4 |
| JiuWenClaw 门面 (10.3.2) | 1 | 0 | 0 |
| VerificationRail (9.29) | 2 | 1 | 0 |
| Interaction (9.59b) | 3 | 4 | 3 |
| InstructionOptimizer (9.72a) | 2 | 2 | 2 |
| ToolOptimizer (9.72b) | 1 | 3 | 4 |
| SignalDetector (9.73) | 3 | 3 | 3 |
| Tools (todo/write_file) | 0 | 2 | 1 |
| CodeAgentRail (10.3.7) | 0 | 0 | 1 |
| TenantAgentPool (10.3.14) | 0 | 0 | 2 |
| UpdateExecution (9.80) | 0 | 0 | 1 |
| Harness Agents (9.27-9.30) | 2 | 2 | 0 |
