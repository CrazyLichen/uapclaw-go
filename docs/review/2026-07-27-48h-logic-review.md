# 48小时代码逻辑审查报告 (2026-07-27)

> 审查范围：2026-07-25 ~ 2026-07-27 期间的 10 个提交
> 审查方法：逐方法对比 Go 移植代码与 Python 参考项目，重点检查方法签名、步骤完整性、占位代码实现状态

---

## 提交记录与涉及章节

| 提交 | 涉及章节 | 说明 |
|------|---------|------|
| 67ec36b | 10.3.14 | feat: 实现 TenantAgentPool 对齐 Python |
| 17c2d6c | 9.72a | fix: CodeAgentRail 审查修复 |
| c531064 | 9.72b | refactor(optimizer): 接口类型安全化和代码质量改进 |
| 55a6c85 | 9.55/9.65 | fix(agent_teams,adapter,runtime): 修复逻辑审查中确认的 9 个 bug |
| b85ada8 | 9.5x | fix(tools): 对齐 Python 源码一比一复刻 |
| 其他 5 个 | — | lint 修复、格式修复、注释完善 |

---

## 严重问题（功能缺陷）

### S-01: description_method.go Step() it>0 时负例传递链路完全断裂

**影响范围**：optimizer/tool_call 整个描述优化流程
**Python 参考文件**：`openjiuwen/agent_evolving/optimizer/tool_call/utils/description_example_method.py`

**Python 样例** (L37-42):
```python
else:
    # load negative ex
    function_name = tool['name']
    neg_examples = self.get_negative_examples(function_name)
    examples_obtained = {"neg_examples": neg_examples, "examples": examples}
    # improve with neg ex
    output = self.generate(tool, examples_obtained, prev_outputs, it)
```

**Go 问题** (`description_method.go:78-80`):
```go
} else {
    outputMap = m.Generate(ctx, tool, examples, prevOutputs, it)
}
```

**问题描述**：Go 在 `it > 0` 时直接把 `examples []ExampleTuple` 传给 `Generate`，而 Python 先加载负例，然后组装 `{"neg_examples": ..., "examples": examples}` dict 再传入。这意味着 Go 完全缺失负例加载逻辑，后续的 `CritiqueAllDescriptions` 中也永远拿不到负例数据。

**影响链路**：
```
Step() → Generate() → GenerateDescriptionFromDocumentation() → CritiqueAllDescriptions()
```
整个负例传递链路断裂，导致描述优化完全依赖正例，无法识别和修正负例导致的问题。

**修复方案**：
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
同时需修改 `Generate` 方法的 `examples` 参数类型为 `any`，以及 `GenerateDescriptionFromDocumentation` 的参数类型。

---

### S-02: description_method.go GenerateDescriptionFromDocumentation 参数类型不匹配

**Python 参考文件**：同 S-01

**Python 样例** (L387-397):
```python
def generate_description_from_documentation(self, tool, examples, prev_outputs):
    pos = examples["examples"]      # 从 dict 提取正例
    neg = examples["neg_examples"]  # 从 dict 提取负例
    tmp = self.critique_descriptions(tool, pos, prev_outputs)
    tmp_contrast = self.critique_all_descriptions(tool, examples, prev_outputs)
```

**Go 问题** (`description_method.go:433-444`):
```go
func (m *ToolDescriptionMethod) GenerateDescriptionFromDocumentation(
    ctx context.Context, tool map[string]any, examples []ExampleTuple, prevOutputs []map[string]any,
) map[string]any {
    pos := examples  // examples 是 []ExampleTuple，不是 dict
    tmp, _ := m.CritiqueDescriptions(ctx, tool, pos, typedPrevOutputs)
    tmpContrast, _ := m.CritiqueAllDescriptions(ctx, tool, pos, typedPrevOutputs)
```

**问题描述**：
1. `pos` 应该从 dict 中提取正例子集 `examples["examples"]`，Go 直接用了全部 examples
2. `CritiqueAllDescriptions` 中的 `neg_examples` 永远为空，因为 `examples` 不是 dict
3. `neg` 变量（L395）在 Go 中完全不存在，Python 中用它做负例分析

**修复方案**：修改签名为 `examples map[string]any`，内部提取 `pos` 和 `neg`：
```go
func (m *ToolDescriptionMethod) GenerateDescriptionFromDocumentation(
    ctx context.Context, tool map[string]any, examples map[string]any, prevOutputs []map[string]any,
) map[string]any {
    pos := toExampleTuples(examples["examples"])  // 从 dict 提取正例
    tmp, _ := m.CritiqueDescriptions(ctx, tool, pos, typedPrevOutputs)
    tmpContrast, _ := m.CritiqueAllDescriptions(ctx, tool, examples, typedPrevOutputs)
    // ...
}
```

---

### S-03: description_method.go CritiqueAllDescriptions 负例来源错误

**Python 参考文件**：同 S-01

**Python 样例** (L328-331):
```python
if len(examples) > 0 and prev_outputs is not None and len(prev_outputs) > 0:
    positive_examples = examples["examples"]
    negative_examples = examples["neg_examples"]
```

**Go 问题** (`description_method.go:302-303`):
```go
positiveExamples := examples    // examples 是 []ExampleTuple
negExamples := m.GetNegativeExamples(functionName)
```

**问题描述**：Python 中负例通过 `examples` dict 传入（已在 `Step()` 中加载），Go 重新从文件加载负例。虽然重新加载可以拿到负例，但：
1. 与 Python 数据流不一致
2. 如果 `GetNegativeExamples` 的文件路径与 Python 不一致，可能加载不到
3. 配合 S-01/S-02 修复后，应从 dict 中提取

**修复方案**：配合 S-02 修复，将 `examples` 参数改为 `map[string]any`，从中提取正负例。

---

### S-04: stream_controller.go executeRound 缺少 TIMED_OUT 状态和异常重抛

**Python 参考文件**：`openjiuwen/agent_teams/agent/stream_controller.py`

**Python 样例** (L472-497):
```python
async def _execute_round(self, message: Any) -> None:
    await self._update_execution(ExecutionStatus.STARTING)
    await self._update_execution(ExecutionStatus.RUNNING)
    try:
        await self._run_retrying_stream(message)
        if self._cancel_requested:
            await self._update_execution(ExecutionStatus.CANCELLED)
        else:
            await self._update_execution(ExecutionStatus.COMPLETING)
            await self._update_execution(ExecutionStatus.COMPLETED)
    except asyncio.CancelledError:
        await self._update_execution(ExecutionStatus.CANCELLED)
        raise  # ← 重抛
    except asyncio.TimeoutError:
        await self._update_execution(ExecutionStatus.TIMED_OUT)  # ← 超时状态
        raise  # ← 重抛
    except Exception as e:
        team_logger.error("DeepAgent round error: %s", e)
        await self._update_execution(ExecutionStatus.FAILED)
        raise  # ← 重抛
    finally:
        await self._update_execution(ExecutionStatus.IDLE)
```

**Go 问题** (`stream_controller.go:591-625`):
```go
func (sc *StreamController) executeRound(ctx context.Context, message any) {
    _ = sc.updateExecution(ctx, atschema.ExecutionStatusStarting)
    _ = sc.updateExecution(ctx, atschema.ExecutionStatusRunning)
    // ...
    err := sc.runRetryingStream(ctx, message)
    if err != nil {
        if isCancelRequested {
            _ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelled)
        } else {
            // ← 缺少 TIMED_OUT 分支
            logger.Error(scLogComponent).Err(err)...
            _ = sc.updateExecution(ctx, atschema.ExecutionStatusFailed)
        }
    }
    // ← 错误被吞掉，不重抛
}
```

**问题描述**：
1. 缺少 `TIMED_OUT` 状态处理，超时场景下状态为 FAILED 而非 TIMED_OUT
2. 异常不重抛，导致外层 `runOneRound` 无法感知执行失败，不会设 `MemberStatus.ERROR`

**修复方案**：
```go
func (sc *StreamController) executeRound(ctx context.Context, message any) error {
    _ = sc.updateExecution(ctx, atschema.ExecutionStatusStarting)
    _ = sc.updateExecution(ctx, atschema.ExecutionStatusRunning)

    if ctx.Err() != nil {
        _ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelled)
        _ = sc.updateExecution(ctx, atschema.ExecutionStatusIdle)
        return ctx.Err()
    }

    err := sc.runRetryingStream(ctx, message)
    sc.mu.Lock()
    isCancelRequested := sc.cancelRequested
    sc.mu.Unlock()

    if err != nil {
        if isCancelRequested {
            _ = sc.updateExecution(ctx, atschema.ExecutionStatusCancelled)
        } else if ctx.Err() != nil {
            // 对齐 Python: asyncio.TimeoutError → TIMED_OUT
            _ = sc.updateExecution(ctx, atschema.ExecutionStatusTimedOut)
        } else {
            logger.Error(scLogComponent).Err(err)...
            _ = sc.updateExecution(ctx, atschema.ExecutionStatusFailed)
        }
    } else {
        // ... 成功逻辑不变
    }
    _ = sc.updateExecution(ctx, atschema.ExecutionStatusIdle)
    return err  // 重抛错误
}
```

---

### S-05: stream_controller.go runOneRound 缺少 BaseException 分支（MemberStatus.ERROR 更新）

**Python 参考文件**：同 S-04

**Python 样例** (L339-353):
```python
try:
    await self._execute_round(message)
    team_member = self._state.team_member
    if team_member is None or await team_member.status() != MemberStatus.SHUTDOWN_REQUESTED:
        await self._update_status(MemberStatus.READY)
except asyncio.CancelledError:
    cancelled = True
    raise
except BaseException as e:
    team_logger.error("Failed to execute deep agent, {}", e, exc_info=True)
    await self._update_status(MemberStatus.ERROR)  # ← 关键：执行异常时设 ERROR
```

**Go 问题** (`stream_controller.go:564-585`):
Go 的 `runOneRound` 在 `executeRound` 返回后只处理取消和完成，没有处理执行异常时更新 `MemberStatus.ERROR`。由于 `executeRound` 吞掉了错误（见 S-04），`runOneRound` 无法感知执行失败。

**问题描述**：如果 Agent 执行失败（非取消），状态应该更新为 `MemberStatus.ERROR`，但 Go 中完全缺失此分支。这导致 Agent 执行异常后状态仍停留在 `MemberStatus.BUSY`。

**修复方案**：配合 S-04 修复，让 `executeRound` 返回 error：
```go
roundErr := sc.executeRound(ctx, message)

// 对齐 Python: except BaseException as e: _update_status(MemberStatus.ERROR)
if roundErr != nil {
    logger.Error(scLogComponent).Err(roundErr).Str("member_name", sc.memberName()).
        Msg("执行 DeepAgent 轮次失败")
    _ = sc.updateStatus(ctx, atschema.MemberStatusError)
    return  // 不再继续续轮逻辑
}

// 对齐 Python: if team_member is None or status != SHUTDOWN_REQUESTED: READY
memberStatus := atschema.MemberStatusReady
if sc.state != nil && sc.state.TeamMember != nil {
    status, _ := sc.state.TeamMember.Status(ctx)
    memberStatus = status
}
if memberStatus != atschema.MemberStatusShutdownRequested {
    _ = sc.updateStatus(ctx, atschema.MemberStatusReady)
}
```

---

### S-06: base.go OptimizeTool 中 latest_description 索引与 Python 不一致

**Python 参考文件**：`openjiuwen/agent_evolving/optimizer/tool_call/base.py`

**Python 样例** (L57):
```python
latest_description = result_descs[-1][-1][0]["description"]
```

**Go 问题** (`base.go:149-153`):
```go
lastNode := lastDescBatch[len(lastDescBatch)-1]
if len(lastNode) > 0 {
    lastStep := lastNode[len(lastNode)-1]  // 即 [-1][-1][-1]
```

**问题描述**：Python 用 `[-1][-1][0]`（取第一个 step output），Go 用 `[-1][-1][-1]`（取最后一个 step output）。

注意 Python 自身两处索引不一致：
- L57: `result_descs[-1][-1][0]` — 取 `[0]`（第一个 step）
- L82: `result_descs[-1][-1][-1]` — 取 `[-1]`（最后一个 step）

Go 需一比一复刻 Python 的 `[0]` 索引。由于 `result_descs` 的结构是 `[][][]map`，`[0]` 通常是 beam search 根节点的输出（即 it=0 的原始描述），而 `[-1]` 是最后一轮迭代的结果。如果意图是"用最新描述更新 tool"，则 Python 的 `[0]` 可能是 bug，但我们应该先一比一复刻。

**修复方案**：
```go
if len(lastNode) > 0 {
    lastStep := lastNode[0]  // 对齐 Python: result_descs[-1][-1][0]
    if desc, ok := lastStep["description"].(string); ok {
        tool["description"] = desc
    }
}
```

---

### S-07: read_file.go encodeImage default 分支用 JPEG 而非 PNG（代码与注释矛盾）

**Python 参考文件**：`openjiuwen/harness/tools/shell/bash/_output.py`

**Go 问题** (`filesystem/read_file.go:891-896`):
```go
default:
    // PNG 作为默认格式
    if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
        return nil, err
    }
```

**问题描述**：注释明确说"PNG 作为默认格式"，但实际代码调用了 `jpeg.Encode`。当图片格式不是 jpeg/jpg 时（如 gif、bmp、webp），都会被错误地编码为 JPEG，导致图片格式信息丢失，可能影响 LLM 的图像理解。

**修复方案**：
```go
default:
    // PNG 作为默认格式
    if err := png.Encode(&buf, img); err != nil {
        return nil, err
    }
```

---

### S-08: read_file.go readText 行数计算使用 Split("", "") 而非 Split("", "\n")

**Go 问题** (`filesystem/read_file.go:270-280`):
```go
if strings.TrimSpace(content) == "" {
    if offset == 0 {
        rendered = "Warning: the file exists but the contents are empty."
    } else {
        lineCount := len(strings.Split(content, ""))  // ← BUG：按字符分割而非换行
        rendered = fmt.Sprintf(
            "Warning: the file exists but is shorter than the provided offset (%d). The file has %d lines.",
            offset, lineCount,
        )
    }
}
```

**问题描述**：`strings.Split(content, "")` 按每个字符拆分，返回的是字符数而非行数。当 offset 超出文件末尾时，错误消息中报告的行数远大于实际行数（如一个 100 字符 5 行的文件，会报告 100 行）。应使用 `strings.Split(content, "\n")` 或 `strings.Count(content, "\n") + 1` 计算行数。

**修复方案**：
```go
lineCount := len(strings.Split(content, "\n"))
```
或更精确：
```go
lineCount := strings.Count(content, "\n") + 1
```

---

## 一般问题（逻辑不一致）

### M-01: spawn_manager.go OnTeammateUnhealthy 缺少先 cleanup 和 RESTARTING 状态更新

**Python 参考文件**：`openjiuwen/agent_teams/agent/spawn_manager.py`

**Python 样例** (L205-216):
```python
async def on_teammate_unhealthy(self, member_name: str) -> None:
    team_logger.warning("Teammate {} detected as unhealthy, initiating restart", member_name)
    await self.cleanup_teammate(member_name)  # ← 先清理
    team_backend = self._configurator.team_backend
    team_name = self._configurator.team_name
    if team_backend and team_name:
        await team_backend.db.member.update_member_status(
            member_name, team_name, MemberStatus.RESTARTING.value,  # ← 设置 RESTARTING
        )
    await self.restart_teammate(member_name)
```

**Go 问题** (`spawn_manager.go:249-271`):
Go 直接调用 `RestartTeammate`，缺少：
1. 先调用 `CleanupTeammate`（Python 先 `cleanup_teammate` 再重启）
2. 更新 DB 状态为 `RESTARTING`

**修复方案**：
```go
func (m *SpawnManager) OnTeammateUnhealthy(memberName string) {
    logger.Warn(spawnLogComponent)...
    // 对齐 Python: 先清理旧句柄
    m.CleanupTeammate(context.Background(), memberName)
    // ⤵️ 待 #9.64 TeamDatabase 实现后回填: update_member_status(RESTARTING)
    // ...
}
```

---

### M-02: spawn_manager.go CleanupTeammate 缺少 stop_health_check 和错误处理

**Python 参考文件**：同 M-01

**Python 样例** (L146-164):
```python
try:
    await handle.stop_health_check()
    if handle.is_alive:
        await handle.force_kill()
except Exception as e:
    team_logger.error("Error cleaning up teammate {}: {}", member_name, e)
```

**Go 问题** (`spawn_manager.go:178-179`):
```go
// 强制终止
_ = handle.ForceKill()
```

**问题描述**：
1. 没有调用 `stop_health_check()`
2. `ForceKill()` 的错误被忽略
3. 没有检查 `IsAlive()` 再决定是否 force_kill

**修复方案**：
```go
// 对齐 Python: try: handle.stop_health_check(); if handle.is_alive: handle.force_kill()
if h, ok := handle.(interface{ StopHealthCheck() error }); ok {
    _ = h.StopHealthCheck()
}
if h, ok := handle.(interface{ IsAlive() bool }); ok {
    if h.IsAlive() {
        if err := handle.ForceKill(); err != nil {
            logger.Error(spawnLogComponent).
                Str("member_name", memberName).Err(err).
                Msg("清理 teammate 时 force_kill 失败")
        }
    }
} else {
    _ = handle.ForceKill()
}
```

---

### M-03: agent_tool.go createSubAgent 缺少多个 Python create_kwargs 字段

**Python 参考文件**：`jiuwenswarm/server/runtime/agent_adapter/code_agent_rail.py`

**Python 样例** (L234-252):
```python
create_kwargs = {
    "model": spec.model or parent_config.model,
    "card": spec.agent_card,
    "system_prompt": spec.system_prompt,
    "tools": parent_tool_cards,
    "mcps": spec.mcps,                              # ← Go 缺失
    "enable_task_loop": spec.enable_task_loop,
    "max_iterations": spec.max_iterations if spec.max_iterations is not None else parent_config.max_iterations,
    "workspace": spec.workspace if spec.workspace is not None else workspace,
    "skills": spec.skills,
    "backend": spec.backend if spec.backend is not None else parent_config.backend,  # ← Go 缺失
    "sys_operation": None,
    "language": spec.language if spec.language is not None else parent_config.language,
    "prompt_mode": spec.prompt_mode if spec.prompt_mode is not None else parent_config.prompt_mode,  # ← Go 缺失
    "subagents": None,
    "enable_async_subagent": False,
    "add_general_purpose_agent": False,
    "restrict_to_work_dir": spec.restrict_to_work_dir,  # ← Go 硬编码 true
}
factory_kwargs = dict(spec.factory_kwargs or {})       # ← Go 缺失
```

**Go 问题** (`agent_tool.go:306-322`):
1. **`mcps`**: Python 传入 `spec.mcps`，Go 没有
2. **`backend`**: Python 用 `spec.backend or parent_config.backend`，Go 没有设置
3. **`prompt_mode`**: Python 用 `spec.prompt_mode or parent_config.prompt_mode`，Go 没有设置
4. **`factory_kwargs`**: Python 展开 `spec.factory_kwargs` 传入 `create_deep_agent`，Go 完全缺失
5. **`max_iterations`**: Python 从 parent 读取默认值，Go 硬编码 15
6. **`restrict_to_work_dir`**: Python 用 `spec.restrict_to_work_dir`，Go 硬编码 `true`

**修复方案**：
```go
params := hconfig.CreateDeepAgentParams{
    // ...
    Mcps:              spec.Mcps,
    Backend:           spec.Backend,
    PromptMode:        spec.PromptMode,
    // ...
    RestrictToWorkDir: &spec.RestrictToWorkDir,  // 使用 spec 而非硬编码
}
```
同时 `maxIterations` 应从 parent config 读取默认值。

---

### M-04: deep_adapter_config.go loadCustomSubagents 缺少 factory_kwargs 设置

**Python 参考文件**：`jiuwenswarm/server/runtime/agent_adapter/interface_deep.py`

**Python 样例**:
```python
custom_spec = _agent_def_to_subagent_config(agent_def, model, workspace, model_cache)
custom_spec.factory_kwargs = {"auto_create_workspace": False}  # ← 关键
```

**Go 问题** (`deep_adapter_config.go:199-201`):
```go
spec := agentDefToSubagentConfig(agentDef, d.model, d.modelCache, d.toolCards)
if spec != nil {
    result = append(result, spec)  // ← 没有 FactoryKwargs 设置
}
```

**问题描述**：Python 为每个自定义 agent 设置 `factory_kwargs = {"auto_create_workspace": False}`，控制子 Agent 不自动创建工作空间。Go 缺少此设置，可能导致子 Agent 在不需要时自动创建工作空间。

**修复方案**：
```go
spec := agentDefToSubagentConfig(agentDef, d.model, d.modelCache, d.toolCards)
if spec != nil {
    // 对齐 Python: custom_spec.factory_kwargs = {"auto_create_workspace": False}
    spec.FactoryKwargs = map[string]any{"auto_create_workspace": false}
    result = append(result, spec)
}
```

---

### M-05: stream_controller.go streamOneRound 缺少 sessionID/teamSession 传入

**Python 参考文件**：同 S-04

**Python 样例** (L397-408):
```python
async def _stream_one_round(self, query: Any) -> ...:
    inputs = {"query": query}
    try:
        stream_kwargs: dict[str, Any] = {"session_id": get_session_id() or None}
        if self._state.team_session is not None:
            stream_kwargs["team_session"] = self._state.team_session
        async for chunk in harness.run_streaming(inputs, **stream_kwargs):
```

**Go 问题** (`stream_controller.go:647-648`):
```go
inputMap := map[string]any{"query": query}
chunkCh, err := harness.RunStreaming(ctx, inputMap, "", nil)  // sessionID 为空，teamSession 为 nil
```

**问题描述**：Python 通过 `get_session_id()` 和 `self._state.team_session` 传入了有意义的值。Go 传空字符串和 nil，可能影响 session 上下文传递。

**修复方案**：从 `state` 读取 sessionID 和 teamSession：
```go
sessionID := ""
if sc.state != nil {
    // 对齐 Python: get_session_id() or None
    if sid := sc.state.SessionID; sid != "" {
        sessionID = sid
    }
}
var teamSession any
if sc.state != nil && sc.state.TeamSession != nil {
    teamSession = sc.state.TeamSession
}
chunkCh, err := harness.RunStreaming(ctx, inputMap, sessionID, teamSession)
```

---

### M-06: spawn_manager.go RestartTeammate 失败后缺少 ERROR 状态更新

**Python 参考文件**：同 M-01

**Python 样例** (L199-203):
```python
if team_backend:
    team_name = self._configurator.team_name
    if team_name:
        await team_backend.db.member.update_member_status(
            member_name, team_name, MemberStatus.ERROR.value
        )
return False
```

**Go 问题** (`spawn_manager.go:244`):
Go 只返回 error，没有更新 DB 状态为 ERROR。

**修复方案**：在重试循环结束处添加注释/预留位：
```go
// ⤵️ 待 #9.64 TeamDatabase 实现后回填:
// 对齐 Python: team_backend.db.member.update_member_status(member_name, team_name, MemberStatus.ERROR.value)
logger.Error(spawnLogComponent).Str("member_name", memberName).
    Msg("重启 teammate 最终失败，应更新 DB 状态为 ERROR")
```

---

### M-07: eval.go runs>1 时 results 层级与 Python 不同

**Python 参考文件**：`openjiuwen/agent_evolving/optimizer/tool_call/utils/customized_eval.py`

**Python 样例** (L110):
```python
'results': all_results[0] if runs == 1 else all_results,
```

**Go 问题** (`eval.go:153-191`): Go 始终将所有 run 的结果平铺到一个 `[]EvalItemResult` 中，没有按 run 分组。

**当前影响**：调用处始终传 `runs=1`，暂不影响功能。但若未来 `runs>1`，需要按 run 分组。

**修复方案**：暂不修改，添加注释说明差异。未来需要时再调整。

---

### M-08: base.go result_examples 追加方式与 Python 不同

**Python 参考文件**：同 S-06

**Python 样例** (L69):
```python
result_examples.append(result_example)  # append 整个结果
```

**Go 问题** (`base.go:175`):
```go
resultExamples = append(resultExamples, resultExample...)  // 展开 append
```

**问题描述**：Python 是 append 整个结果（保持 `[][][]map` 层级），Go 是展开 append（平铺为 `[][]map`）。

**当前影响**：`result_examples` 在 Python 中也不被后续消费（仅调试），暂不影响最终输出。

**修复方案**：对齐 Python，不展开：
```go
resultExamples = append(resultExamples, resultExample)  // 不展开
```
需要将 `resultExamples` 类型改为 `[][][]map[string]any`。

---

### M-09: todo.go _update_todos 校验顺序与 Python 不一致

**Python 参考文件**：`openjiuwen/harness/tools/todo/todo.py`

**Python 样例** (L662-691):
```python
async def _update_todos(self, session_id, todos_data, current_todos):
    todo_map = {todo.id: todo for todo in current_todos}
    updated_count = 0
    for todo_data in todos_data:
        # ... 先执行所有更新 ...
        updated_count += 1
    self._validate_single_in_progress(current_todos)  # <-- 更新后才校验
    await self.save_todos(session_id, current_todos)
```

**Go 问题** (`todo/todo.go:616-694`):
Go 先预校验 in_progress 数量，再执行更新。Python 是先执行所有更新，再统一校验。

**问题描述**：两种方式在边界场景行为不一致：
- 场景：当前有 task A (in_progress)，要同时将 A 改为 completed、B 改为 in_progress
- Python：先执行更新 (A→completed, B→in_progress)，再校验，此时只有 1 个 in_progress → 通过
- Go：预校验时发现要新增 B (in_progress)，当前有 A (in_progress)，可能拦截

**修复方案**：将 `validateSingleInProgress` 调用移到更新循环之后，对最终结果做校验，对齐 Python 行为。

---

### M-10: write_file.go content 空 strings 检查比 Python 更严格

**Python 参考文件**：`openjiuwen/harness/tools/filesystem/filesystem.py`

**Python 样例** (L870-871):
```python
if content is None:
    return ToolOutput(success=False, error="content is required")
```

**Go 问题** (`filesystem/write_file.go:59-63`):
```go
if input.Content == "" {
    return map[string]any{
        "success": false,
        "error":   "content is required",
    }, nil
}
```

**问题描述**：Python 只检查 `None`（允许空字符串写入空文件），Go 检查空字符串（`""`）。在 Python 中传入 `content=""` 会通过校验并写入空文件；在 Go 中传入 `content=""` 会被拒绝。

**修复方案**：如果要对齐 Python，应将 `Content` 字段改为 `*string` 类型以区分"未提供"和"空字符串"。

---

### M-11: subagent/task_tool.go 错误提示词与 Python 不一致

**Python 参考文件**：`openjiuwen/harness/tools/subagent/task_tool.py`

**Python 样例** (L89-90):
```python
reason="Both 'subagent_type' and 'task' are required",
```

**Go 问题** (`subagent/task_tool.go:49`):
```go
return nil, fmt.Errorf("both 'subagent_type' and 'task_description' are required")
```

**问题描述**：Python 使用 `'task'`（旧参数名），Go 使用 `'task_description'`。应确认使用哪个名称，与 Python 保持一致或明确标注修正原因。

---

## 提示问题（日志/注释/风格）

### L-01: TenantAgentPool GetInstance 缺少初始化日志

**Python 参考文件**：`jiuwenswarm/server/runtime/tenant_agent_pool.py`

**Python 样例** (L23-26):
```python
def __init__(self) -> None:
    self._agent_manager = AgentManager()
    logger.info("[TenantAgentPool] Initialized with AgentManager")
```

**Go 问题** (`tenant_pool.go:57-62`):
Go 中 `GetInstance` 只有 `"Created singleton instance"` 日志，缺少 `"Initialized with AgentManager"` 日志。

**修复方案**：在 factory 函数中增加日志：
```go
func GetInstance() *TenantAgentPool {
    return tenantAgentPoolSingleton.Get(func() *TenantAgentPool {
        logger.Info(tapLogComponent).Msg("[TenantAgentPool] Created singleton instance")
        pool := &TenantAgentPool{
            agentManager: NewAgentManager(),
        }
        logger.Info(tapLogComponent).Msg("[TenantAgentPool] Initialized with AgentManager")
        return pool
    })
}
```

---

### L-02: TenantAgentPool ResetInstance 缺少重置日志

**Python 参考文件**：同 L-01

**Python 样例** (L37-41):
```python
@classmethod
def reset_instance(cls) -> None:
    if cls._instance is not None:
        logger.info("[TenantAgentPool] Resetting singleton instance")
    cls._instance = None
```

**Go 问题** (`tenant_pool.go:68-70`):
Go 直接调用 `Reset()`，没有日志。

**修复方案**：
```go
func ResetInstance() {
    logger.Info(tapLogComponent).Msg("[TenantAgentPool] Resetting singleton instance")
    tenantAgentPoolSingleton.Reset()
}
```

---

### L-03: TenantAgentPool 异常路径日志缺少 method 字段

**Python 参考文件**：同 L-01

**Python 样例** (L59-60):
```python
except Exception as e:
    logger.error(f"[TenantAgentPool] Error in process_message: {e}", exc_info=True)
    raise
```

**Go 问题** (`tenant_pool.go:76-81`):
Go 的错误日志按项目日志规则 3.3 第9点，异常路径日志应包含 `method` 等结构化字段。

**修复方案**：
```go
logger.Error(tapLogComponent).
    Err(err).
    Str("request_id", request.RequestID).
    Str("channel_id", request.ChannelID).
    Str("method", "process_message").
    Msg("[TenantAgentPool] Error in process_message")
```

---

### L-04: process_message_stream 错误捕获范围语义差异（设计确认项）

**Python 参考文件**：同 L-01

**Python 样例** (L64-83):
Python 的 `try/except` 包裹整个 `async for ... yield` 块，异常中断迭代并重新抛出。

**Go 问题** (`tenant_pool.go:88-104`):
Go 的异常捕获只覆盖"获取 channel"这一步。Channel 已成功返回后，消费期间产生的错误不会被 TenantAgentPool 感知。

**分析**：这是 Go channel vs Python async generator 的惯用差异。Python 在流式产出过程中可以捕获异常，Go 只返回 channel 不消费它。如果 `AgentManager.ProcessMessageStream` 内部会在 channel 中发送错误 chunk，则语义等价。

**修复方案**：维持现状，在方法注释中标注设计差异。如需完全对齐，可在返回的 channel 外包装一层。

---

### L-05: todo.go formatCreateResult 缺少 TrimSpace

**Python 参考文件**：`openjiuwen/harness/tools/todo/todo.py`

**Python 样例** (L259-266):
```python
result += f"  {status_icon} task_id: {todo.id} , content: {todo.content}{model_info}\n"
first_task = todos[0].content if todos else ""
result += f"\nNext step: Immediately execute task '{first_task}'"
return result.strip()  # <-- Python 最后做了 .strip()
```

**Go 问题** (`todo/todo.go:578-592`):
Go 没有做 `strings.TrimSpace(result)` 处理，导致末尾可能有多余换行。

**修复方案**：在 `formatCreateResult` 返回前加 `strings.TrimSpace(result)`。

---

### L-06: shell/output.go RenderPartialOnFailure 返回空字符串而非 nil

**Python 参考文件**：`openjiuwen/harness/tools/shell/bash/_output.py`

**Python 样例** (L212-213):
```python
if not output.stdout and not output.stderr:
    return None
```

**Go 问题** (`shell/output.go:140-142`):
```go
if output.Stdout == "" && output.Stderr == "" {
    return ""
}
```

**问题描述**：Python 返回 `None`（让调用方回退到原始失败消息），Go 返回空字符串 `""`。调用方需要区分"没有输出"和"有部分输出"。

**修复方案**：改为返回 `nil`（如果返回类型支持），或调用方明确处理空字符串。

---

## 待回填占位代码审查

以下 ⤵️ 标记的代码确认确实尚未实现，需要后续章节完成后回填：

| 文件 | 标记 | 对应章节 | 状态 |
|------|------|---------|------|
| stream_controller.go:503 | `⤵️ 待 9.62 set_member_id 上下文变量` | 9.62 CoordinationKernel | 确认未实现 |
| stream_controller.go:645 | `⤵️ 待 9.55 TeamAgent 完善后回填 sessionID` | 9.55 TeamAgent | 确认未实现（见 M-05） |
| spawn_manager.go:210 | `⤵️ 待 #9.64 BuildContextFromDB 回填` | 9.64 TeamDatabase | 确认未实现 |
| spawn_manager.go:268 | `TODO(#9.64): 更新 DB 状态为 ERROR` | 9.64 TeamDatabase | 确认未实现（见 M-06） |
| spawn_manager.go:289 | `⤵️ 预留：Messager（9.65）实现后回填` | 9.65 Messager | 确认未实现 |
| stream_controller.go:548 | `⤵️ 待 #9.65 TeamMember.Status() 实现后替换` | 9.65 TeamMember | 确认未实现（有 stub，始终返回 READY） |

---

## 问题汇总

| 编号 | 严重度 | 文件 | 问题简述 |
|------|--------|------|---------|
| S-01 | 🔴严重 | description_method.go:78 | Step() it>0 负例加载和 examples_obtained 组装完全缺失 |
| S-02 | 🔴严重 | description_method.go:433 | GenerateDescriptionFromDocumentation examples 参数类型应为 map[string]any |
| S-03 | 🔴严重 | description_method.go:276 | CritiqueAllDescriptions examples 参数类型不匹配，无法提取 neg_examples |
| S-04 | 🔴严重 | stream_controller.go:591 | executeRound 缺少 TIMED_OUT 状态和异常重抛 |
| S-05 | 🔴严重 | stream_controller.go:564 | runOneRound 缺少 BaseException 分支（MemberStatus.ERROR） |
| S-06 | 🔴严重 | base.go:151 | OptimizeTool 索引用 [-1] Python 用 [0] |
| S-07 | 🔴严重 | read_file.go:891 | encodeImage default 分支用 JPEG 而非 PNG（代码与注释矛盾） |
| S-08 | 🔴严重 | read_file.go:270 | readText 行数计算用 Split("","") 应为 Split("","\n") |
| M-01 | 🟡一般 | spawn_manager.go:249 | OnTeammateUnhealthy 缺少先 cleanup 和 RESTARTING 状态 |
| M-02 | 🟡一般 | spawn_manager.go:178 | CleanupTeammate 缺少 stop_health_check 和错误处理 |
| M-03 | 🟡一般 | agent_tool.go:306 | createSubAgent 缺少 mcps/backend/prompt_mode/factory_kwargs |
| M-04 | 🟡一般 | deep_adapter_config.go:199 | loadCustomSubagents 缺少 factory_kwargs 设置 |
| M-05 | 🟡一般 | stream_controller.go:648 | streamOneRound 缺少 sessionID/teamSession |
| M-06 | 🟡一般 | spawn_manager.go:244 | RestartTeammate 失败后缺少 ERROR 状态更新 |
| M-07 | 🟡一般 | eval.go:153 | runs>1 时 results 层级与 Python 不同（当前不影响） |
| M-08 | 🟡一般 | base.go:175 | result_examples 追加方式不同（当前不影响） |
| M-09 | 🟡一般 | todo.go:616 | _update_todos 校验顺序与 Python 不一致 |
| M-10 | 🟡一般 | write_file.go:59 | content 空 strings 检查比 Python 更严格 |
| M-11 | 🟡一般 | task_tool.go:49 | 错误提示词用 'task_description' 而 Python 用 'task' |
| L-01 | 🔵提示 | tenant_pool.go:57 | GetInstance 缺少 "Initialized with AgentManager" 日志 |
| L-02 | 🔵提示 | tenant_pool.go:68 | ResetInstance 缺少重置日志 |
| L-03 | 🔵提示 | tenant_pool.go:76 | 异常路径日志缺少 method 字段 |
| L-04 | 🔵提示 | tenant_pool.go:88 | process_message_stream 错误捕获范围语义差异 |
| L-05 | 🔵提示 | todo.go:578 | formatCreateResult 缺少 TrimSpace（多余尾部换行） |
| L-06 | 🔵提示 | output.go:140 | RenderPartialOnFailure 返回空字符串而非 nil |

---

## 修复优先级建议

1. **第一批（S-01 ~ S-03 一起修复）**：description_method.go 负例传递链路，这三个问题形成链条，必须一起修复
2. **第二批（S-04 ~ S-05 一起修复）**：stream_controller.go 状态机和错误传播
3. **第三批（S-06 ~ S-08 单独修复）**：base.go 索引对齐、read_file.go 图片编码和行数计算
4. **第四批（M-01 ~ M-04）**：spawn_manager/agent_tool/deep_adapter 逻辑问题
5. **第五批（M-05 ~ M-11）**：其他一般逻辑问题
6. **第六批（L-01 ~ L-06）**：日志/注释/风格提示问题

---

## 统计

| 严重度 | 数量 | 占比 |
|--------|------|------|
| 🔴 严重 | 8 | 33% |
| 🟡 一般 | 11 | 46% |
| 🔵 提示 | 6 | 25% |
| **合计** | **25** | **100%** |
