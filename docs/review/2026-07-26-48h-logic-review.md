# 48小时代码逻辑审查报告

> 审查日期：2026-07-26
> 审查范围：提交 `17c2d6c..c64afe4`（9 个提交）
> 对比基准：Python 参考项目源码（openjiuwen / jiuwenswarm）
> 审查重点：方法签名/步骤一致性、⤵️占位代码验证、逻辑正确性

## 审查章节概览

| 章节 | 内容 | 变更文件数 | 状态 |
|------|------|-----------|------|
| 10.3.14 | TenantAgentPool 多租户 Agent 池化 | 3 | ✅ 已审查 |
| 9.72b | ToolOptimizer 接口类型安全化 | 10 | ✅ 已审查 |
| 9.27/CodeAgentRail | CodeAgentRail 审查修复 + AgentTool | 4 | ✅ 已审查 |
| 9.55-9.60 | agent_teams StreamController/SpawnManager | 5 | ✅ 已审查 |
| 10.3.4-6/10.3.12-13 | adapter/AgentManager/AgentConfigService | 4 | ✅ 已审查 |
| 9.38-49 | 工具集对齐 Python 修复 | 15 | ✅ 已审查 |
| 6.x/9.x | controller/runner/embedding/handoff 等杂项 | 30+ | ✅ 已审查 |

## 问题统计

| 严重程度 | 数量 |
|---------|------|
| 严重 | 8 |
| 一般 | 17 |
| 提示 | 18 |

---

## 严重问题

### S-01：ToolDescriptionMethod.Step — it>0 时缺少 neg_examples 加载和 examples_obtained 组装

**章节**：9.72b ToolOptimizer

**问题描述**：Python `step()` 在 it>0 分支中会调用 `self.get_negative_examples(function_name)` 获取负例，然后将正负例组装为 `examples_obtained = {"neg_examples": neg_examples, "examples": examples}`，再传递给 `self.generate(tool, examples_obtained, prev_outputs, it)`。Go 实现直接传递了原始 `examples`（正例），没有加载负例，也没有组装 examples_obtained 字典。这会导致 Description Stage 的增强描述环节缺少负例信息，影响优化质量。

**Python 样例**：
```python
# description_example_method.py L37-42
neg_examples = self.get_negative_examples(function_name)
examples_obtained = {"neg_examples": neg_examples, "examples": examples}
output = self.generate(tool, examples_obtained, prev_outputs, it)
```

**Go 问题代码**：
```go
// description_method.go L80
outputMap = m.Generate(ctx, tool, examples, prevOutputs, it)
// examples 直接传入，缺少 neg_examples 加载和组装
```

**修复方案**：在 it>0 分支中，先调用 `m.GetNegativeExamples(functionName)` 获取负例，然后组装：
```go
negExamples := m.GetNegativeExamples(functionName)
examplesObtained := map[string]any{"neg_examples": negExamples, "examples": examples}
outputMap = m.Generate(ctx, tool, examplesObtained, prevOutputs, it)
```

---

### S-02：ToolDescriptionMethod.Generate — 方法签名不支持正负例字典结构

**章节**：9.72b ToolOptimizer

**问题描述**：Python `generate()` 接收 `examples` 参数为字典 `{"neg_examples": ..., "examples": ...}`，并原样传递给 `generate_description_from_documentation`。Go 的 `Generate` 直接接收 `[]ExampleTuple` 类型的 examples，无法表达正负例字典结构。此问题与 S-01 连锁——根因是 Go 实现中没有实现 Python 中 `examples_obtained` 字典结构。

**Python 样例**：
```python
# description_example_method.py L50-63
def generate(self, tool, examples, prev_outputs, it):
    output = self.generate_description_from_documentation(tool, examples, prev_outputs)
```

**Go 问题代码**：
```go
// description_method.go L102-114
func (m *ToolDescriptionMethod) Generate(
    ctx context.Context, tool map[string]any, examples []ExampleTuple, prevOutputs []map[string]any, it int,
) map[string]any {
    output := m.GenerateDescriptionFromDocumentation(ctx, tool, examples, prevOutputs)
```

**修复方案**：将 `Generate` 的 `examples` 参数改为 `map[string]any`（兼容 `{"neg_examples": ..., "examples": ...}` 结构），同时修改 `GenerateDescriptionFromDocumentation` 的参数类型。

---

### S-03：GenerateDescriptionFromDocumentation — examples 参数类型错误，无法区分正负例

**章节**：9.72b ToolOptimizer

**问题描述**：Python 中 `generate_description_from_documentation` 接收 `examples` 为字典（含 "examples" 和 "neg_examples" 键），内部通过 `pos = examples["examples"]` 和 `neg = examples["neg_examples"]` 拆分正负例，分别传给 `critique_descriptions(tool, pos, ...)` 和 `critique_all_descriptions(tool, examples, ...)`。Go 实现中 `pos` 直接等于 `examples`（类型为 `[]ExampleTuple`），且没有 `neg` 变量，`critique_all_descriptions` 也接收了错误的参数。

**Python 样例**：
```python
# description_example_method.py L394-397
pos = examples["examples"]
neg = examples["neg_examples"]
tmp = self.critique_descriptions(tool, pos, prev_outputs)
tmp_contrast = self.critique_all_descriptions(tool, examples, prev_outputs)
```

**Go 问题代码**：
```go
// description_method.go L440-444
pos := examples  // 直接等于 examples，无正负例区分
tmp, _ := m.CritiqueDescriptions(ctx, tool, pos, typedPrevOutputs)
tmpContrast, _ := m.CritiqueAllDescriptions(ctx, tool, pos, typedPrevOutputs)
// 应该传 examplesObtained 而非 pos
```

**修复方案**：修改参数类型，正确拆分正负例。完整流程示例：
```
Step(it=0): examples = get_positive_examples() → Generate(tool, examples, prev, 0)
  → GenerateDescriptionFromDocumentation:
    pos = examples["examples"]  // 只有正例
    neg = examples["neg_examples"]  // nil 或空
    critique_descriptions(tool, pos, prev)  // 正例批判
    critique_all_descriptions(tool, examples, prev)  // 正+负例对比批判

Step(it>0): neg = get_negative_examples() → examples_obtained = {"neg_examples": neg, "examples": examples}
  → Generate(tool, examples_obtained, prev, it)
    → GenerateDescriptionFromDocumentation:
      pos = examples_obtained["examples"]  // 正例
      neg = examples_obtained["neg_examples"]  // 负例（关键！）
      critique_descriptions(tool, pos, prev)
      critique_all_descriptions(tool, examples_obtained, prev)  // 对比批判需要负例
```

---

### S-04：runOneRound 缺少 BaseException 错误路径——非取消异常未设置 MemberStatus.ERROR

**章节**：9.59-9.60 StreamController

**问题描述**：Python `_run_one_round` 中 `_execute_round` 抛出非 CancelledError 异常时，会设置 `MemberStatus.ERROR`。Go 的 `runOneRound` 只有 cancelled 分支和成功分支，没有对应 BaseException 的错误处理路径。

**Python 样例**：
```python
# stream_controller.py L350-352
except BaseException as e:
    team_logger.error("Failed to execute deep agent, {}", e, exc_info=True)
    await self._update_status(MemberStatus.ERROR)
```

**Go 问题代码**：
```go
// stream_controller.go L513-585 — runOneRound 的 func() 内
// 只有 cancelled 分支和成功分支，无 BaseException 错误路径
```

**修复方案**：在 `runOneRound` 的 `func()` 中添加非取消错误的处理：
```go
if err := sc.executeRound(ctx, roundCtx, agentInput, iteration); err != nil {
    if ctx.Err() == context.Canceled {
        // 已有的取消处理
    } else {
        logger.Error(scLogComponent).Err(err).Msg("Failed to execute deep agent")
        sc.updateStatus(ctx, MemberStatusError)
    }
}
```

---

### S-05：executeRound 缺少 CancelledError/TimeoutError 区分——所有错误一律 FAILED

**章节**：9.59-9.60 StreamController

**问题描述**：Python `_execute_round` 对 CancelledError 设 CANCELLED 并 raise，对 TimeoutError 设 TIMED_OUT 并 raise，对 Exception 设 FAILED 并 raise。Go 里这三种错误被合并到 FAILED，状态机语义不对。

**Python 样例**：
```python
# stream_controller.py L486-495
except asyncio.CancelledError:
    await self._update_execution(ExecutionStatus.CANCELLED)
    raise
except asyncio.TimeoutError:
    await self._update_execution(ExecutionStatus.TIMED_OUT)
    raise
except Exception as e:
    team_logger.error("DeepAgent round error: %s", e)
    await self._update_execution(ExecutionStatus.FAILED)
    raise
```

**Go 问题代码**：
```go
// stream_controller.go L591-625 — executeRound
// err != nil 时一律走 FAILED，没有区分 context.Canceled 和 context.DeadlineExceeded
```

**修复方案**：
```go
if ctx.Err() == context.Canceled {
    sc.updateExecution(ExecutionStatusCancelled)
    return ctx.Err()
}
if ctx.Err() == context.DeadlineExceeded {
    sc.updateExecution(ExecutionStatusTimedOut)
    return ctx.Err()
}
// 其他错误
logger.Error(scLogComponent).Err(err).Msg("DeepAgent round error")
sc.updateExecution(ExecutionStatusFailed)
return err
```

---

### S-06：createSubAgent 类型断言失败时静默零值——子 Agent 以默认配置静默创建

**章节**：9.27 CodeAgentRail / 10.3.4-6 adapter

**问题描述**：`agent_tool.go` 的 `t.parentAgent.(hinterfaces.DeepAgentInterface)` 使用 `ok` 模式但断言失败时仅跳过赋值，model/ws/language 全部为零值，子 Agent 以默认配置静默创建，不报错。

**Python 样例**：
```python
# code_agent_rail.py L191-198
parent_config = getattr(self._parent_agent, "deep_config", None)
# self._parent_agent 类型为 DeepAgent，不存在断言失败场景
```

**Go 问题代码**：
```go
// agent_tool.go L225-235
if t.parentAgent != nil {
    if deepAgent, ok := t.parentAgent.(hinterfaces.DeepAgentInterface); ok {
        if deepCfg := deepAgent.DeepConfig(); deepCfg != nil {
            model = deepCfg.Model
            language = deepCfg.Language
        }
    }
    // 断言失败 → model/ws/language 全零值，无日志无error
}
```

**修复方案**：断言失败时记录 Warn 日志并返回 error：
```go
deepAgent, ok := t.parentAgent.(hinterfaces.DeepAgentInterface)
if !ok {
    return nil, fmt.Errorf("parent agent 未实现 DeepAgentInterface")
}
deepCfg := deepAgent.DeepConfig()
if deepCfg == nil {
    return nil, fmt.Errorf("parent agent 的 DeepConfig 为 nil")
}
```

---

### S-07：modelCache 始终为 nil——自定义 Agent 的 model 字段无效

**章节**：9.27 CodeAgentRail / 10.3.4-6 adapter

**问题描述**：`agent_tool.go` L222 `var modelCache map[string]*llm.Model` 声明但未从 DeepConfig 获取，传给 `agentDefToSubagentConfig` 时始终为 nil。如果用户在自定义 agent 定义中指定 `model: sonnet`，Go 会忽略，始终使用父 agent 的默认 model。

**Python 样例**：
```python
# code_agent_rail.py L198
getattr(self._parent_agent, "_model_cache", None)
# _agent_def_to_subagent_config 中:
# if agent_def.model and isinstance(model_cache, dict):
#     resolved_model = model_cache.get(agent_def.model, model)
```

**Go 问题代码**：
```go
// agent_tool.go L222
var modelCache map[string]*llm.Model  // 永远是 nil
// L236
spec := agentDefToSubagentConfig(agentDef, model, modelCache, nil)
```

**修复方案**：从 DeepConfig 获取 ModelCache：
```go
var modelCache map[string]*llm.Model
if deepAgent, ok := t.parentAgent.(hinterfaces.DeepAgentInterface); ok {
    if deepCfg := deepAgent.DeepConfig(); deepCfg != nil {
        modelCache = deepCfg.ModelCache  // 需确认 DeepConfig 是否有此字段
    }
}
```

---

### S-08：AgentManager.Cleanup() 忽略每个 agent 的 cleanup 错误——静默丢弃无日志

**章节**：10.3.12 AgentManager

**问题描述**：Go 中 `_ = entry.agent.Cleanup()` 直接丢弃错误，没有 warn 日志记录。Python 中如果某个 agent cleanup 失败，会记录 warning 但继续清理其他 agent。

**Python 样例**：
```python
# agent_manager.py L491-497
for agent in agents.values():
    if hasattr(agent, "cleanup"):
        try:
            await agent.cleanup()
        except Exception as e:
            logger.warning("[AgentManager] Agent cleanup failed: %s", e)
```

**Go 问题代码**：
```go
// agent_manager.go L479-484
for chKey, chAgents := range am.agents {
    for _, entry := range chAgents {
        _ = entry.agent.Cleanup()  // 错误完全丢弃，无日志
    }
    delete(am.agents, chKey)
}
```

**修复方案**：
```go
for chKey, chAgents := range am.agents {
    for _, entry := range chAgents {
        if err := entry.agent.Cleanup(); err != nil {
            logger.Warn(amLogComponent).
                Err(err).
                Str("channel_id", chKey).
                Msg("[AgentManager] Agent cleanup failed")
        }
    }
    delete(am.agents, chKey)
}
```

---

## 一般问题

### G-01：write_file 缺少 encoding 参数传递给 FsOperation

**章节**：9.38-49 工具集

**问题描述**：Python 版本在检测到 UTF-16-LE 编码时，将 encoding 传递给 `write_file`，确保正确写入。Go 版本的 `readExistingText` 返回了 encoding 但未使用它。

**Python 样例**：
```python
# filesystem.py L945-951
res = await self.operation.fs().write_file(
    path, content,
    prepend_newline=False,
    create_if_not_exist=True,
    encoding=encoding,
)
```

**Go 问题代码**：
```go
// write_file.go L155-158
writeRes, writeErr := op.Fs().WriteFile(ctx, path, content,
    sys_operation.WithFsPrependNewline(false),
    sys_operation.WithFsCreateIfNotExist(true),
)
```

**修复方案**：添加 `sys_operation.WithFsEncoding(encoding)` 选项（如果 FsOperation 支持），或确认 FsOperation 默认处理了编码问题。

---

### G-02：mcp_resources 直接透传数据，缺少属性映射

**章节**：9.38-49 工具集

**问题描述**：Python 的 `ListMcpResourcesTool` 和 `ReadMcpResourceTool` 会对 MCP SDK 返回的对象做属性提取，转为 `{uri, name, mimeType, description}` 标准字典。Go 版本直接透传。

**Python 样例**：
```python
# mcp_tools.py L27-35
data = [
    {
        "uri": getattr(r, "uri", str(r)),
        "name": getattr(r, "name", ""),
        "mimeType": getattr(r, "mimeType", None),
        "description": getattr(r, "description", None),
    }
    for r in (resources or [])
]
```

**Go 问题代码**：
```go
// mcp_resources.go L59-77
resources, err := resourceMgr.ListMcpResources(ctx, input.ServerID)
// ... 直接透传
return map[string]any{"success": true, "data": resources}, nil
```

**修复方案**：确认 `resourceMgr.ListMcpResources` 返回格式与 Python 的 `{uri, name, mimeType, description}` 结构一致。如果返回的是 Go 结构体，需要做类似 Python 的属性映射。

---

### G-03：task_tool 错误消息 'task' vs 'task_description' — 违反一比一复刻规则

**章节**：9.38-49 工具集

**问题描述**：Python 中参数名是 `task_description`，但错误消息中用的是 `'task'`。Go 使用了 `'task_description'`，与 Python 原文不一致。

**Python 样例**：
```python
# task_tool.py L87-91
if not subagent_type or not task_description:
    raise build_error(
        StatusCode.TOOL_TASK_TOOL_INVOKED,
        reason="Both 'subagent_type' and 'task' are required",
    )
```

**Go 问题代码**：
```go
// task_tool.go L48-49
if input.SubagentType == "" || input.TaskDescription == "" {
    return nil, fmt.Errorf("both 'subagent_type' and 'task_description' are required")
}
```

**修复方案**：改为 `"Both 'subagent_type' and 'task' are required"` 以与 Python 原文对齐。

---

### G-04：SubAgentConfig.Tools 类型应为 []string 而非 []*tool.ToolCard

**章节**：9.27 CodeAgentRail

**问题描述**：Go 的 `SubAgentConfig.Tools` 类型为 `[]*tool.ToolCard`，但 Python 的 `SubAgentConfig.tools` 实际存的是 `List[str]`。Go 当前绕过 `spec.Tools`，直接用 `agentDef.Tools` + inline disallowed 过滤，功能等价但不符合 Python 数据流。

**Python 样例**：
```python
# code_agent_rail.py L213-217
parent_tool_cards = _filter_tool_cards(
    all_tool_cards,
    allowed_tools=list(spec.tools) if spec.tools else ["*"],
    disallowed_tools=None,
)
```

**Go 问题代码**：
```go
// agent_tool.go L259-278
allowedTools := agentDef.Tools  // 直接用 agentDef.Tools，绕过 spec.Tools
```

**修复方案**：将 `SubAgentConfig.Tools` 改为 `[]string`，`agentDefToSubagentConfig` 中合并 disallowed_tools，`createSubAgent` 中用 `spec.Tools` 传给 `filterToolCards`。

---

### G-05：agentDefToSubagentConfig 在两处重复定义且逻辑有差异

**章节**：10.3.4-6 adapter

**问题描述**：`deep_adapter_config.go` 和 `agent_tool.go` 各有一个 `agentDefToSubagentConfig`，且 `agent_tool.go` 中调用时传入 `modelCache=nil, allToolCards=nil`，返回的 `spec.Tools` 为 nil，然后 `createSubAgent` 又自行获取并过滤工具，逻辑冗余且容易出错。

**Python 样例**：Python 只有一个 `_agent_def_to_subagent_config` 函数，在 `interface_deep.py` 中定义，被多处共用。

**Go 问题代码**：
```go
// agent_tool.go L236
spec := agentDefToSubagentConfig(agentDef, model, modelCache, nil)
// modelCache 和 allToolCards 都是 nil
```

**修复方案**：统一 `agentDefToSubagentConfig` 的调用点，在 `agent_tool.go` 中也应传入正确的 `modelCache` 和 `allToolCards`，或者去掉内部工具过滤逻辑，工具过滤统一在调用方做。

---

### G-06：createSubAgent 缺少 backend、prompt_mode、restrict_to_work_dir 参数映射

**章节**：9.27 CodeAgentRail

**问题描述**：Go 的 `CreateDeepAgentParams` 中 `RestrictToWorkDir` 硬编码为 `true`，没有从 `spec` 中读取。`backend` 和 `prompt_mode` 没有对应字段。

**Python 样例**：
```python
# code_agent_rail.py L234-252
create_kwargs = {
    ...
    "backend": spec.backend if spec.backend is not None else parent_config.backend,
    "prompt_mode": spec.prompt_mode if spec.prompt_mode is not None else parent_config.prompt_mode,
    "restrict_to_work_dir": spec.restrict_to_work_dir,
    ...
}
```

**Go 问题代码**：
```go
// agent_tool.go L304
RestrictToWorkDir: true,  // 硬编码，不从 spec 读取
```

**修复方案**：从 `spec` 中读取这些参数，如果没有则使用父 agent 配置的默认值。

---

### G-07：AgentTool.invoke 中缺少 description 参数校验

**章节**：9.27 CodeAgentRail

**问题描述**：Python 的 ToolCard JSON Schema 声明 `description` 为必填参数（与 `prompt`、`subagent_type` 并列），Go 只校验了 `subagent_type` 和 `prompt`，没有校验 `description`。

**Python 样例**：
```python
# code_agent_rail.py L165
"required": ["description", "prompt", "subagent_type"],
```

**Go 问题代码**：
```go
// agent_tool.go L91-99
// 只校验了 subagent_type 和 prompt，没有校验 description
```

**修复方案**：在参数校验中添加 `description` 的检查。

---

### G-08：detectTaskFailed 的 payload 类型断言可能不全面

**章节**：9.59-9.60 StreamController

**问题描述**：Python 中 `payload` 是带有 `.type` 属性的对象（Pydantic model 或 dataclass），Go 假设 `payload` 是 `map[string]any`。如果 Go 侧 chunk 的 Payload 实际上是结构体，类型断言 `payload.(map[string]any)` 会失败，导致 `detectTaskFailed` 永远检测不到 task_failed。

**Python 样例**：
```python
# stream_controller.py L56-75
payload = getattr(chunk, "payload", None)
if payload is None: return None
if getattr(payload, "type", None) != _TASK_FAILED_PAYLOAD_TYPE: return None
```

**Go 问题代码**：
```go
// stream_controller.go L772-811
// payload.(map[string]any) 断言可能失败
```

**修复方案**：确认 Payload 字段在 Go 侧的实际类型，增加对应类型断言分支，或统一确保序列化后 Payload 一定是 `map[string]any`。

---

### G-09：streamOneRound 缺少 session_id 和 team_session 参数传递

**章节**：9.59-9.60 StreamController

**问题描述**：Go 的 `streamOneRound` 中 `harness.RunStreaming(ctx, inputMap, "", nil)` 硬编码空 sessionID 和 nil teamSession，Python 中从 `get_session_id()` 和 `self._state.team_session` 获取。

**Python 样例**：
```python
# stream_controller.py L405-408
stream_kwargs = {"session_id": get_session_id() or None}
if self._state.team_session is not None:
    stream_kwargs["team_session"] = self._state.team_session
async for chunk in harness.run_streaming(inputs, **stream_kwargs):
```

**Go 问题代码**：
```go
// stream_controller.go L648
harness.RunStreaming(ctx, inputMap, "", nil)
```

**修复方案**：从 `sc.state` 或 context 中读取 sessionID 和 teamSession，传入 `RunStreaming`。

---

### G-10：HandoffTool reason 参数描述与 Python 不一致

**章节**：8.34 HandoffTeam

**问题描述**：Go 中 reason 的 description 是 `"Reason for handoff to the target agent."`，Python 中是 `"Reason for handoff: briefly explain why the task is being transferred."`。违反提示词一比一复刻规则。

**Python 样例**：
```python
# handoff_tool.py L43-45
"reason": {
    "type": "string",
    "description": "Reason for handoff: briefly explain why the task is being transferred.",
},
```

**Go 问题代码**：
```go
// handoff_tool.go L53-57
schema.NewStringParam(
    "reason",
    "Reason for handoff to the target agent.",  // 与 Python 不同
    true,
),
```

**修复方案**：改为 `"Reason for handoff: briefly explain why the task is being transferred."`。

---

### G-11：HandoffSignal message/reason 不能区分 nil 和空字符串

**章节**：8.34 HandoffTeam

**问题描述**：Python 中 `HandoffSignal` 的 message/reason 可为 `None`，Go 中对应零值为空字符串 `""`，无法区分"未设置"和"设置为空字符串"。

**Python 样例**：
```python
# handoff_signal.py L31-32
message: Optional[str] = None
reason: Optional[str] = None
# L71-72
message=payload.get(HANDOFF_MESSAGE_KEY) or None,
reason=payload.get(HANDOFF_REASON_KEY) or None,
```

**Go 问题代码**：
```go
// handoff_signal.go L16-23
type HandoffSignal struct {
    Target  string
    Message string  // 不能区分 nil 和 ""
    Reason  string  // 不能区分 nil 和 ""
}
```

**修复方案**：将 Message 和 Reason 改为 `*string`，或使用 `omitempty` 标签并在提取时赋 nil。

---

### G-12：OptimizeTool resultExamples 维度错误——展开导致少一维

**章节**：9.72b ToolOptimizer

**问题描述**：Python `result_examples.append(result_example)` 追加三维列表，Go 使用 `...` 展开后变成二维。虽然 `resultExamples` 后续仅用于调试日志，不影响核心逻辑，但与 Python 不对齐。

**Python 样例**：
```python
# base.py L69
result_examples.append(result_example)  # result_example 是 List[List[Dict]]
```

**Go 问题代码**：
```go
// base.go L175
resultExamples = append(resultExamples, resultExample...)  // 展开后变成二维
```

**修复方案**：改为 `resultExamples = append(resultExamples, resultExample)`（不用 `...`），保持三维结构。

---

### G-13：OptimizeTool description/example 阶段失败时不中断——与 Python 行为不一致

**章节**：9.72b ToolOptimizer

**问题描述**：Python 中 `customized_pipeline` 无 try/except，失败会抛异常终止整个循环。Go 中 description/example 阶段失败时记录日志但继续循环，可能导致多轮空转。

**Python 样例**：无异常处理，直接 `result_desc = customized_pipeline("description", ...)`

**Go 问题代码**：
```go
if err != nil { logger.Error(...); } else { resultDescs = append(...) }
```

**修复方案**：如果要严格对齐 Python，description/example 阶段失败应该直接返回 error。当前 Go 的"不中断"行为更健壮但与 Python 不一致，建议至少 description 阶段失败时立即返回 error。

---

### G-14：writeRuntimeState 缺少 runtime_state.yaml 写入和 git 信息收集

**章节**：10.3.4-6 adapter

**问题描述**：Python `_write_runtime_state` 写入 git_branch、git_main_branch、git_status、git_recent_commits、git_user、model、mode 等到 `runtime_state.yaml`。Go 只做 `os.Setenv("JCLAW_RUNTIME_"+key, value)`。

**Python 样例**：
```python
# interface_deep.py L756-821 — 写入 runtime_state.yaml（含 git 信息、平台信息、model 等）
```

**Go 问题代码**：
```go
// deep_adapter_config.go L280-284
func writeRuntimeState(key, value string) {
    os.Setenv("JCLAW_RUNTIME_"+key, value)
}
```

**修复方案**：补齐 runtime_state.yaml 文件写入和 git 信息收集逻辑。

---

### G-15：OnTeammateUnhealthy 流程差异——Go 不先 cleanup 且无 RESTARTING 状态

**章节**：9.58 SpawnManager

**问题描述**：Python 先 cleanup → 设 RESTARTING → restart（串行），Go 不先 cleanup（由 RestartTeammate 内部 cleanup），且在独立 goroutine 中重启，没有先设 RESTARTING 状态到 DB。

**Python 样例**：
```python
# spawn_manager.py L205-216
async def on_teammate_unhealthy(self, member_name):
    team_logger.warning(...)
    await self.cleanup_teammate(member_name)
    # ... update DB status to RESTARTING
    await self.restart_teammate(member_name)
```

**Go 问题代码**：
```go
// spawn_manager.go L249-271 — 不先 cleanup，在 goroutine 中 restart
```

**修复方案**：在 `OnTeammateUnhealthy` 中添加 DB 状态更新为 RESTARTING（当 9.64 实现后），并确保不会在 cleanup 完成前重复触发。

---

### G-16：SysOperationToolAdapter 使用硬编码 switch，不支持自定义操作类型

**章节**：9.32-9.33 sys_operation

**问题描述**：Python 通过 `getattr(instance, op_type, None)` 动态获取子操作，支持注册自定义操作类型。Go 使用硬编码 `switch opType { case "fs": ... case "shell": ... case "code": ... }`，自定义操作类型被跳过。

**Python 样例**：
```python
# tool_adapter.py L31-33
sub_op_getter = getattr(instance, op_type, None)
if not sub_op_getter or not callable(sub_op_getter):
    continue
```

**Go 问题代码**：
```go
// tool_adapter.go L60-69
switch opType {
case "fs":
    subOp = instance.Fs()
case "shell":
    subOp = instance.Shell()
case "code":
    subOp = instance.Code()
default:
    continue  // 自定义操作类型被跳过
}
```

**修复方案**：引入反射或接口查询机制支持自定义操作类型，或在注释中明确说明此限制。

---

### G-17：filterToolCards 同时匹配 Name 和 ID，与 Python 只匹配 Name 不一致

**章节**：9.27 CodeAgentRail

**问题描述**：Go 的 `filterToolCards` 同时匹配 `tc.Name` 和 `tc.ID`，但 Python 只匹配 `tc.name`。当 `tc.Name != tc.ID` 时可能产生误匹配。

**Python 样例**：
```python
# code_agent_rail.py L102
result = [tc for tc in all_tool_cards if tc.name in target_names]
```

**Go 问题代码**：
```go
// code_agent_rail.go L294-296
if targetNames[tc.Name] || targetNames[tc.ID] {
    result = append(result, tc)
}
```

**修复方案**：如果 Go 中 ToolCard.Name 和 ToolCard.ID 存在语义差异，应只匹配 Name 对齐 Python；如果确实需要双匹配，应在注释中说明原因。

---

## 提示问题

### T-01：TenantAgentPool 缺少 "Initialized with AgentManager" 日志

**章节**：10.3.14

**问题描述**：Python 的 `__init__` 和 `get_instance` 各有一行日志，Go 只有 "Created singleton instance"。

**修复方案**：在 factory 函数中补充 `logger.Info(tapLogComponent).Msg("[TenantAgentPool] Initialized with AgentManager")`。

---

### T-02：TenantAgentPool 缺少 "Resetting singleton instance" 日志

**章节**：10.3.14

**问题描述**：Python 中 `reset_instance` 在重置前记录 "Resetting singleton instance"，Go 的 `ResetInstance` 直接调用 `Singleton.Reset()` 无日志。

**修复方案**：在 `ResetInstance()` 中添加日志。

---

### T-03：Singleton.Reset() 非线程安全（仅测试使用）

**章节**：10.3.14

**问题描述**：`Reset()` 没有任何互斥保护，如果并发调用 `Get()` 可能出现数据竞争。但 Python 的 `reset_instance` 也仅用于测试且非线程安全，实际影响有限。

**修复方案**：维持现状可接受，因为仅测试使用。

---

### T-04：todo formatCreateResult 末尾缺少 TrimSpace

**章节**：9.38-49 工具集

**问题描述**：Python 的 `_format_create_result` 返回 `result.strip()`，Go 版本返回的字符串末尾有换行符。

**修复方案**：在 `formatCreateResult` 返回前添加 `strings.TrimSpace(result)`。

---

### T-05：search_tools DetailLevel 默认值差异

**章节**：9.38-49 工具集

**问题描述**：Go 版本 `DetailLevel` 字段默认为 0，Python 中默认值为 1。

**修复方案**：
```go
detailLevel := input.DetailLevel
if detailLevel == 0 {
    detailLevel = 1
}
```

---

### T-06：session_tools task_id 格式差异

**章节**：9.38-49 工具集

**问题描述**：Python 的 task_id 格式为 32 位十六进制（`uuid.uuid4().hex`），Go 的格式为带连字符的 UUID（`uuid.New().String()`，36 字符）。

**修复方案**：改为 `strings.ReplaceAll(uuid.New().String(), "-", "")` 以严格对齐。

---

### T-07：buildAgentToolCard 中 agentID 为空时无 UUID fallback

**章节**：9.27 CodeAgentRail

**问题描述**：Python 中 `tool_id = f"agent_tool_{agent_id}" if agent_id else f"agent_tool_{uuid.uuid4().hex}"`，Go 中直接 `fmt.Sprintf("agent_tool_%s", agentID)` 无 fallback。

**修复方案**：当 `agentID` 为空时生成 UUID fallback。

---

### T-08：HandoffTool.Invoke 缺少 str inputs 解析

**章节**：8.34 HandoffTeam

**问题描述**：Python 的 `HandoffTool.invoke` 处理 `inputs` 为 str 的情况（先 json.loads，失败则当作 reason），Go 直接假设 inputs 是 `map[string]any`。

**修复方案**：如果 LLM 可能传 string 类型的 inputs，应在入口处增加 string 解析逻辑。

---

### T-09：findHandoffFromSession 缺少 ast.literal_eval fallback

**章节**：8.34 HandoffTeam

**问题描述**：Python 的 `_find_handoff_from_session` 在 JSON 解析失败后还会尝试 `ast.literal_eval`，Go 只尝试 JSON 解析。

**修复方案**：如果 LLM 工具调用框架保证返回 JSON 格式，可以忽略此差异。

---

### T-10：ShellOperation pkill/killall 手动检查 -tui 后缀的边界问题

**章节**：9.32-9.33 sys_operation

**问题描述**：Go 的 regex 不支持 `(?!...)` 前瞻，使用手动检查替代。但存在边界情况：如果匹配的字符串结尾不是 jiuwenswarm（如 pkill jiuwenswarm-extra），`loc[1]` 指向的可能是 jiuwenswarm 结束位置，`strings.HasPrefix` 可能误判。

**修复方案**：仔细测试边界情况，如 `pkill jiuwenswarm-extra` 不应被排除。

---

### T-11：write_file 缺少操作历史记录（_append_op_history）

**章节**：9.38-49 工具集

**问题描述**：Python 版本在写入文件后会将操作记录到 `.agent_history/` 目录下（用于审计/回滚），Go 版本完全没有实现此功能。

**修复方案**：确认是否需要在后续版本中实现，当前为有意省略。

---

### T-12：write_file content 类型检查——Go 类型系统部分补偿

**章节**：9.38-49 工具集

**问题描述**：Python 还会检查 content 是否为 string 类型（`isinstance(content, str)`），Go 的 `WriteFileInput.Content` 是 `string` 类型，JSON 反序列化时非字符串会被拒绝。

**修复方案**：Go 类型系统已隐式处理，无需额外修改。

---

### T-13：OptimizeTool processed 参数类型差异——Go 先尝试 JSON 解析再传 map

**章节**：9.72b ToolOptimizer

**问题描述**：Python 直接传 string（可能二次转义），Go 先尝试解析 JSON 为 dict 再传 map（避免二次转义）。Go 的处理更合理。

**修复方案**：保持 Go 当前行为并注释说明差异。

---

### T-14：SimpleEval.Eval runs>1 时 results 维度差异

**章节**：9.72b ToolOptimizer

**问题描述**：Python `'results': all_results[0] if runs == 1 else all_results`，当 runs > 1 时返回二维数组，Go 始终返回一维。

**修复方案**：当前 `runs` 始终为 1，无需修改。如需支持 runs > 1 应改为二维结构。

---

### T-15：rits.go 应保留——是 Go 特有的 LLM 调用适配层

**章节**：9.72b ToolOptimizer

**问题描述**：之前有任务要求确认"rits.go 已被删除"，但实际上 `rits.go` 仍然存在。它不再是 Python `rits.py` 的直接复刻，而是 Go 特有的适配层（复用 `llm_resilience.InvokeTextWithRetry`），应保留。

**修复方案**：保留 rits.go，它是 Go 特有的 LLM 调用适配层，不再对应 Python rits.py。

---

### T-16：todo 错误码 StatusToolTodosValidationInvalid 与 Python TOOL_TODOS_INVOKE_FAILED 不一致

**章节**：9.38-49 工具集

**问题描述**：Python 对 tasks 为空使用 `TOOL_TODOS_INVOKE_FAILED`，Go 使用 `StatusToolTodosValidationInvalid`。Go 更精确但与 Python 不对齐。

**修复方案**：确认 StatusCode 映射是否需要调整。

---

### T-17：RecreateAgent cleanup 错误无 warn 日志

**章节**：10.3.12 AgentManager

**问题描述**：与 S-08 类似，`RecreateAgent` 中的 cleanup 错误也被静默丢弃。

**修复方案**：在 cleanup 失败时添加 warn 日志。

---

### T-18：todo LoadTodos 锁粒度差异——Go 实际更安全

**章节**：9.38-49 工具集

**问题描述**：Python 的 `load_todos` 内部持锁，Go 的 `LoadTodos` 不持锁（由外部统一持锁）。Go 的 Load+Save 在同一锁区间内，提供更强一致性保证。

**修复方案**：无需修改，Go 设计更安全。

---

## ⤵️ 占位代码完整清单

以下汇总本次审查范围内所有 `⤵️` 标记的未实现代码：

| 文件 | 标记内容 | 回填章节 | 优先级 |
|------|---------|---------|--------|
| spawn_manager.go L210-214 | initialMessage/sessionID | #9.64 | 高 |
| spawn_manager.go L276-283 | BuildContextFromDB 返回空上下文 | #9.64 | 高 |
| spawn_manager.go L289-295 | PublishRestartEvent no-op | #9.65 | 中 |
| stream_controller.go L503 | set_member_id 上下文变量 | 9.62 | 中 |
| stream_controller.go L546-584 | TeamMember.Status() stub | #9.65 | 中 |
| stream_controller.go L60-61 | pendingInterruptResumes 类型 | 9.55 | 中 |
| stream_controller.go L645-648 | sessionID/teamSession | 待回填 | 中 |
| team_agent.go L112-113 | WithWakeMailbox/WithRequestCompletionPoll | 9.62 | 中 |
| deep_adapter_config.go L92-94 | rail/tool 模式切换 | 待实现 | 中 |
| agent_manager.go L157-159 | ACP 通道 config 合并 | 10.2.9 | 中 |
| agent_manager.go L225-233 | ACP initialize | 10.2.9 | 中 |
| agent_manager.go L360-363 | team evolution config 更新 | 10.3.2 | 中 |
| allocator.go L64-90 | BuildModelAllocatorForPool/ResolveMemberModelFromPool | 9.64 | 低 |
| team_model_config.go L39-41 | Build() 返回 nil, nil | 9.64 | 低 |
| controller/modules/intent_recognizer.go | IntentRecognizer LLM 调用 | 6.23 | 高 |
| context_engine/context/session_memory_manager.go | agent_edit 模式 | 6.x | 中 |
| harness/tools/browser_move/ | 多处 Playwright MCP 工具集 | 9.38-49 | 中 |
| harness/harness_config/builder.go | 内置工具/Rail 实例化 | 9.38-9.24 | 高 |

---

## commit 55a6c85 修复验证

| Bug ID | 描述 | 修复状态 | 残余问题 |
|--------|------|---------|---------|
| S09 | agentDefToSubagentConfig 工具列表不再丢弃 | ✅ 已修复 | 调用方存在重复过滤（G-05） |
| S10 | agent_config.go 用 pathutil.DefaultDir 替换硬编码 | ✅ 已修复 | 无 |
| S17 | ctx 取消不设 ERROR | ✅ 已修复 | BaseException 路径仍缺失（S-04） |
| S18 | SHUTDOWN_REQUESTED → CloseStream | ✅ 已修复 | 依赖 TeamMember.Status() stub |
| S19 | detectTaskFailed 改返回 *taskFailedError | ✅ 已修复 | payload 类型断言可能不全面（G-08） |
| S20 | AddChunkObserver/RemoveChunkObserver 加锁 | ✅ 已修复 | 无 |
| S21/S22 | CleanupTeammate 先 RemoveObserver 再 SetNil | ✅ 已修复 | 无 |
| S23 | RestartTeammate 构建 SpawnConfig | ✅ 已修复 | initialMessage/sessionID 仍为占位 |
| S26 | spawnSubprocess 用 payload 覆盖 | ✅ 已修复 | 无 |

---

## StreamController 方法对齐汇总

| Go 方法 | Python 方法 | 对齐状态 |
|---------|------------|---------|
| NewStreamController | __init__ | ✅ |
| AddChunkObserver | add_chunk_observer | ✅ |
| RemoveChunkObserver | remove_chunk_observer | ✅ |
| IsAgentRunning | is_agent_running | ✅ |
| HasInFlightRound | has_in_flight_round | ✅ |
| HasPendingInterrupt | has_pending_interrupt | ✅ |
| IsValidInterruptResume | is_valid_interrupt_resume | ✅ |
| StartRound | start_round | ✅ |
| Steer | steer | ✅ |
| FollowUp | follow_up | ✅ |
| CancelAgent | cancel_agent | ✅ |
| CloseStream | close_stream | ✅ |
| EmitCompletionAndClose | emit_completion_and_close | ✅ |
| DrainAgentTask | drain_agent_task | ✅ |
| CooperativeCancel | cooperative_cancel | ✅ |
| memberName | _member_name | ✅ |
| tagChunk | _tag_chunk | ✅ |
| fanOutToObservers | (内联) | ✅ |
| startRound | (内联) | ✅ |
| logRoundPanic | _log_agent_task_exception | ✅ |
| runOneRound | _run_one_round | ⚠️ 缺 BaseException 路径（S-04） |
| executeRound | _execute_round | ⚠️ 缺 CancelledError/TimeoutError 区分（S-05） |
| streamOneRound | _stream_one_round | ⚠️ 缺 session_id/team_session（G-09） |
| detectTaskFailed | _detect_task_failed | ⚠️ payload 类型断言（G-08） |

**总计 26 个方法/函数，22 个完全对齐，4 个部分对齐。**
