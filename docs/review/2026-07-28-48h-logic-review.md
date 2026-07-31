# 48小时逻辑审查报告 (2026-07-28)

> 审查范围：48小时内提交记录（commit 677f915），对照 Python 参考项目逐一比对 Go 移植代码
> 
> 审查重点：方法签名/步骤一致性、步骤缺失、待回填占位确认

---

## 一、审查概要

### 修改文件清单

| Go 文件 | 对应 Python 参考 | 所属领域 |
|---------|-----------------|---------|
| context_engine/processor/compressor/current_round_compressor.go | openjiuwen/core/context_engine/processor/compressor/current_round_compressor.py | 领域五：上下文引擎 |
| foundation/llm/model_clients/base_client.go | openjiuwen/core/foundation/llm/model_clients/base_model_client.py | 领域二：LLM 基础层 |
| foundation/llm/model_clients/registry.go | openjiuwen/core/common/clients/client_registry.py + __init__.py | 领域二：LLM 基础层 |
| foundation/llm/schema/config.go | openjiuwen/core/foundation/llm/schema/config.py | 领域二：LLM 基础层 |
| foundation/prompt/textable_variable.go | openjiuwen/core/foundation/prompt/assemble/variables/textable.py | 领域二：LLM 基础层 |
| harness/tools/browser_move/controllers.go | openjiuwen/harness/tools/browser_move/controllers/action.py | 领域九：DeepAgent |
| harness/tools/browser_move/service.go | openjiuwen/harness/tools/browser_move/service.py | 领域九：DeepAgent |
| harness/tools/filesystem/read_file.go | openjiuwen/harness/tools/filesystem/read_file.py | 领域九：DeepAgent |
| harness/tools/shell/security.go | openjiuwen/harness/tools/shell/bash/_security.py + powershell/_security.py | 领域九：DeepAgent |
| harness/tools/todo/todo.go | openjiuwen/harness/tools/todo/todo.py | 领域九：DeepAgent |
| multi_agent/teams/handoff/handoff_tool.go | openjiuwen/core/multi_agent/teams/handoff/handoff_tool.py | 领域八：多 Agent |
| runner/resources_manager/prompt_manager.go | openjiuwen/core/runner/resources_manager/prompt_manager.py | 领域六：Agent 核心 |
| runner/resources_manager/tag_manager.go | openjiuwen/core/runner/resources_manager/tag_manager.py | 领域六：Agent 核心 |
| runner/resources_manager/workflow_manager.go | openjiuwen/core/runner/resources_manager/workflow_manager.py | 领域六：Agent 核心 |
| single_agent/agents/react_invoke.go | openjiuwen/core/single_agent/agents/react_agent.py | 领域六：Agent 核心 |
| common/schema/card.go | openjiuwen/core/common/schema/card.py | 领域一：基础设施 |
| common/utils/background.go | openjiuwen/core/common/background_tasks.py + task_manager/ | 领域一：基础设施 |
| common/utils/singleton.go | openjiuwen/core/common/utils/singleton.py | 领域一：基础设施 |
| evolving/optimizer/base.go | openjiuwen/agent_evolving/optimizer/base.py | 领域七：进化优化 |
| swarm/gateway/message_handler/forward_loop.go | jiuwenswarm/gateway/message_handler/message_handler.py | 领域十一：Gateway |
| swarm/server/adapter/agent_tool.go | jiuwenswarm/server/runtime/agent_adapter/code_agent_rail.py | 领域十：AgentServer |

### 问题统计

| 严重程度 | 数量 | 说明 |
|---------|------|------|
| 🔴 严重 | 8 | 功能逻辑 Bug，会导致运行时异常或功能缺失 |
| 🟡 一般 | 13 | 行为差异、步骤缺失、参数丢失等 |
| 🔵 提示 | 12 | 日志/文本/风格差异，不影响功能流程 |

---

## 二、严重问题（🔴 共 8 个）

### S-01: MergeSummaryBlocks 提示词填充逻辑 Bug

**文件**: `current_round_compressor.go:868-873`

**Python 原文**:
```python
filled_prompt = (
    CLEAN_PROMPT
    .replace("{compress_len}", str(self._summary_merge_target_tokens))
    .replace("{compressed_blocks}", merged_blocks if merged_blocks else "(none)")
)
```

**Go 问题代码**:
```go
filledPrompt := strings.ReplaceAll(crc.cleanPrompt, "{compress_len}", fmt.Sprintf("%d", crc.summaryMergeTargetTokens))
filledPrompt = strings.ReplaceAll(filledPrompt, "{compressed_blocks}", mergedBlocks)
if mergedBlocks == "" {
    filledPrompt = strings.ReplaceAll(crc.cleanPrompt, "{compressed_blocks}", "(none)")
    filledPrompt = strings.ReplaceAll(filledPrompt, "{compress_len}", fmt.Sprintf("%d", crc.summaryMergeTargetTokens))
}
```

**问题**: 当 `mergedBlocks == ""` 时，第一次 ReplaceAll 已经将 `{compressed_blocks}` 替换为空字符串，占位符已不存在，后续无法再替换为 `"(none)"`。虽然从 `crc.cleanPrompt` 重新开始替换可以工作，但代码逻辑冗余且混乱。

**修复方案**:
```go
blocksValue := mergedBlocks
if blocksValue == "" {
    blocksValue = "(none)"
}
filledPrompt := strings.ReplaceAll(crc.cleanPrompt, "{compress_len}", fmt.Sprintf("%d", crc.summaryMergeTargetTokens))
filledPrompt = strings.ReplaceAll(filledPrompt, "{compressed_blocks}", blocksValue)
```

---

### S-02: TextableVariable.Update 解析失败时应抛错而非 continue

**文件**: `textable_variable.go:150-155`

**Python 原文**:
```python
for placeholder in self.placeholders:
    value = kwargs
    try:
        for node in placeholder.split("."):
            if isinstance(value, dict):
                value = value.get(node)
            else:
                value = getattr(value, node)
    except Exception as e:
        raise build_error(
            StatusCode.PROMPT_ASSEMBLER_VARIABLE_INIT_FAILED,
            error_msg=f"error parsing the placeholder `{placeholder}`",
            cause=e
        ) from e
```

**Go 问题代码**:
```go
for _, placeholder := range v.placeholders {
    value, err := resolveNestedValue(placeholder, kwargs)
    if err != nil {
        // 解析失败时，保留原始占位符不替换（与 Python 端行为一致：部分填充场景）
        continue
    }
```

**问题**: 注释声称"与 Python 端行为一致"，但 Python 在解析失败时 **抛出异常（raise build_error）**，Go 却 **静默 continue 跳过**。这是完全相反的行为：
- Python: 严格模式 — 任何占位符解析失败立即报错
- Go: 宽容模式 — 解析失败保留原始占位符不替换

**修复方案**: 将 `continue` 改为返回错误：
```go
value, err := resolveNestedValue(placeholder, kwargs)
if err != nil {
    return exception.NewBaseError(
        exception.StatusPromptAssemblerVariableInitFailed,
        exception.WithMsg(fmt.Sprintf("error parsing the placeholder `%s`: %s", placeholder, err.Error())),
        exception.WithCause(err),
    )
}
```
同时 `Update` 方法签名需要改为返回 `error`。

---

### S-03: react_invoke.go 缺失 Workflow 中断恢复分支

**文件**: `react_invoke.go:358-421`

**Python 原文** (`_inner_invoke` L1331-1349):
```python
if interruption_state is not None:
    is_tool_interruption = isinstance(interruption_state, ToolInterruptionState)
    
    if is_tool_interruption:
        # Tool Interrupt: not write UserMessage
        await self._handle_resume(...)
        start_iteration = ctx.extra.pop(RESUME_START_ITERATION_KEY, 0)
    else:
        # Workflow Interrupt: 先写 UserMessage，再 handle_resume
        await context.add_messages(UserMessage(content=self._extract_user_text(user_input)))
        resume_result = await self._handle_resume(...)
        if resume_result is not None:
            pass  # invoke_inputs.result already set
        else:
            start_iteration = ctx.extra.pop(RESUME_START_ITERATION_KEY, 0)
else:
    await context.add_messages(UserMessage(content=self._extract_user_text(user_input)))
```

**Go 问题代码**:
```go
if hitlState != nil {
    a.hitlHandler.Clear(sess)
    interruptionState = *hitlState
}
// ⤵️ Workflow: interruptionState = a.loadInterruptionState(sess)

if hitlState != nil {
    cbc.Extra()["_original_query"] = hitlState.OriginalQuery
}

if hitlState != nil {
    resumeResult, resumeErr := a.hitlHandler.HandleResume(...)
    ...
} else {
    // 正常路径：添加 UserMessage
    plainText := curInputs.Query.PlainText()
    ...
}
```

**问题**: Go 只处理了 HITL 中断，缺失 Workflow 中断分支。Python 中：
1. 加载中断状态时有两个来源（`hitl_state or self._load_interruption_state(session)`）
2. Workflow 中断时需要先写 UserMessage 再调用 `_handle_resume`
3. 当前仅有 `⤵️` 占位注释

**修复方案**: 等待 6.11-6.12 实现 Workflow 中断后回填。当前需确保占位注释清晰标注缺失逻辑。

---

### S-04: react_invoke.go reactLoop 缺失 Workflow 中断检测

**文件**: `react_invoke.go:668`

**Python 原文** (`_inner_invoke` L1421-1427):
```python
workflow_interrupt = self._after_execute_tool_call(
    results, ai_message.tool_calls, ai_message, iteration,
    original_query=ctx.extra.get("_original_query", ""),
)
if workflow_interrupt:
    await self._commit_interrupt(workflow_interrupt, context, session, invoke_inputs)
    break
```

**Go 问题代码**:
```go
// ⤵️ 6.11: Workflow 中断检测
```

**问题**: reactLoop 在 HITL 中断检测后，缺失 Workflow 中断检测。Python 中 HITL 和 Workflow 两种中断检测是串行执行的。

**修复方案**: 同 S-03，等待 6.11 实现后回填。

---

### S-05: read_file.go encodeImage default 分支用错编码器

**文件**: `read_file.go:886-900`

**Go 问题代码**:
```go
func encodeImage(img image.Image, format string) ([]byte, error) {
    var buf bytes.Buffer
    switch strings.ToLower(format) {
    case "jpeg", "jpg":
        if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
            return nil, err
        }
    default:
        // PNG 作为默认格式
        if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {  // BUG!
            return nil, err
        }
    }
    return buf.Bytes(), nil
}
```

**Python 原文**:
```python
detected_format = (img.format or "PNG").lower()
img.save(out, format=detected_format.upper())
```

**问题**: default 分支注释说"PNG 作为默认格式"，但实际使用了 `jpeg.Encode`。应改为 `png.Encode`。同时该函数也不支持 GIF 等其他格式。

**修复方案**:
```go
default:
    if err := png.Encode(&buf, img); err != nil {
        return nil, err
    }
```

---

### S-06: controllers.go buildDragPayload 缺少 offset 字段

**文件**: `controllers.go:1016-1027`

**Go 问题代码**:
```go
return map[string]any{
    "url":            getStr(kwargs, "url"),
    "element_source": sourceSelector,
    "element_target": targetSelector,
    "coord_source_x": sx,
    "coord_source_y": sy,
    "coord_target_x": tx,
    "coord_target_y": ty,
    "steps":          toIntPtrOrNone(kwargs, "steps"),
    "delay_ms":       toIntPtrOrNone(kwargs, "delay_ms"),
}
```

**Python 原文** (`action.py:550-562`):
```python
return {
    "url": (url or "").strip(),
    "element_source": source_selector,
    "element_target": target_selector,
    "element_source_offset": _normalize_offset(element_source_offset),
    "element_target_offset": _normalize_offset(element_target_offset),
    "coord_source_x": sx,
    ...
}
```

**问题**: 缺少 `element_source_offset` 和 `element_target_offset` 两个字段。JS 脚本中 `getPoint(params.element_source, params.element_source_offset, 'source')` 引用了这些字段，缺失会导致 offset 参数永远为 `undefined`，拖拽操作的坐标偏移功能失效。

**修复方案**: 
1. 在 `buildDragPayload` 中增加 offset 参数提取
2. 实现 `_normalize_offset` 等价函数（将 `{x: int, y: int}` 结构转为 `map[string]int`）
3. 添加 offset 字段到返回值

---

### S-07: background.go Task.Cancel() 中 CancelledBy 赋值错误

**文件**: `background.go:315`

**Go 问题代码**:
```go
task.CancelledBy = taskID  // taskID 是被取消任务的自身 ID
```

**Python 原文** (`task.py:93-94`):
```python
if cancelled_by:
    self.cancelled_by = cancelled_by  # cancelled_by 是发起取消者的 ID
```

**问题**: `CancelledBy` 应该记录"谁取消了该任务"，但 Go 把目标任务自身 ID 赋给了 `CancelledBy`，语义错误。

**修复方案**: `Cancel` 方法签名需要增加 `cancelledBy string` 参数：
```go
func (m *TaskManager) Cancel(taskID string, reason string, cancelledBy string) bool {
    // ...
    task.CancelledBy = cancelledBy
    // ...
}
```

---

### S-08: forwardLoop 缺少 panic recovery / 异常保护

**文件**: `forward_loop.go:63-119`

**Python 原文** (`message_handler.py:2163-2558`):
```python
while self._running:
    try:
        msg = await self.consume_user_messages(timeout=None)
        # ... 所有步骤 ...
        try:
            if env.is_stream:
                # ...
        except Exception as e:
            logger.exception("AgentServer send_request failed for %s: %s", msg.id, e)
            err_msg = self._build_error_out_message(msg, e)
            await self.publish_robot_messages(err_msg)
    except asyncio.CancelledError:
        break
```

**Go 问题代码**:
```go
func (mh *MessageHandler) forwardLoop(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case msg := <-mh.userMessages:
            // ... 步骤1-10，无 recover/try-catch
        }
    }
}
```

**问题**: 没有 panic recovery 或错误捕获。任何步骤 panic 会导致整个转发循环 goroutine 崩溃，消息处理停止。

**修复方案**: 在消息处理分支中添加 `defer recover()`：
```go
case msg := <-mh.userMessages:
    func() {
        defer func() {
            if r := recover(); r != nil {
                logger.Error(logComponent).
                    Any("panic", r).
                    Str("msg_id", msg.ID).
                    Msg("forwardLoop panic recovered")
                errMsg := mh.buildErrorOutMessage(msg, fmt.Errorf("internal error: %v", r))
                mh.PublishRobotMessages(errMsg)
            }
        }()
        // ... 原有步骤
    }()
```

---

## 三、一般问题（🟡 共 13 个）

### G-01: ModelRequestConfig.TopP 默认值不一致

**文件**: `config.go:51-52`

**Python 原文**:
```python
top_p: float = Field(default=0.1, description="Top-p sampling parameter")
```

**Go 代码**:
```go
TopP *float64 `json:"top_p,omitempty"`  // 默认 nil，不传给模型
```

**差异**: Python 的 `top_p` 默认值 `0.1` 会被传给 API；Go 默认 `nil` 不会传。行为差异：API 收到的请求参数不同。

**修复方案**: 在 `NewModelRequestConfig` 中设置 `TopP` 默认值为 `0.1`：
```go
defaultTopP := 0.1
cfg := &ModelRequestConfig{
    Temperature: 0.95,
    TopP: &defaultTopP,
}
```

---

### G-02: ValidateProvider 宽松策略 — Go 允许未知 provider

**文件**: `config.go:179-180`, `registry.go:171-183`

**Python 原文**:
```python
@model_validator(mode='after')
def validate_client_provider(self) -> Self:
    # ... 尝试匹配 ...
    else:
        raise build_error(StatusCode.MODEL_PROVIDER_INVALID, ...)
```

**Go 代码**:
```go
// 4. 宽松策略：保留原字符串
return provider
```

**差异**: Python 验证失败时抛异常，Go 返回原字符串（宽松策略）。Go 允许未注册的 provider 通过验证。

**修复方案**: 需确认是否有意设计。如果需要对齐 Python，应在 `ValidateAndNormalizeProvider` 最后抛出错误而非返回原字符串。

---

### G-03: base_client.go reasoning_content 检查独立于 tool_calls

**文件**: `base_client.go:521-531`

**Python 原文**: `reasoning_content` 检查在 `if msg.tool_calls:` 条件内部。

**Go 代码**: `reasoning_content` 检查在 `ToolCalls` 条件外部。

**差异**: 当 `AssistantMessage` 没有 `tool_calls` 但有 `reasoning_content` 时，Go 会添加字段，Python 不会。

---

### G-04: react_invoke.go sub_agent_outputs 被丢弃

**文件**: `react_invoke.go:659-665`

**Python 原文**:
```python
hitl_interrupt, sub_agent_outputs = self._after_execute_tool_call_for_hitl(...)
if hitl_interrupt:
    await self._commit_interrupt(hitl_interrupt, ..., sub_agent_outputs)
```

**Go 代码**:
```go
hitlInterrupt, _ := a.AfterExecuteToolCallForHITL(...)
if hitlInterrupt != nil {
    _, _ = a.CommitInterrupt(ctx, hitlInterrupt, modelCtx, sess, invokeInputs, nil)
}
```

**差异**: Go 用 `_` 丢弃了 `sub_agent_outputs`，`CommitInterrupt` 中硬编码传 `nil`。

---

### G-05: workflow_manager.go WorkflowCard 硬编码为 nil

**文件**: `workflow_manager.go:56-58`

**Go 代码**:
```go
wrappedProvider := func(ctx context.Context) (interfaces.Workflow, error) {
    return provider(ctx, nil)  // WorkflowCard 硬编码 nil
}
```

**差异**: Python 的 `WorkflowProvider` 接受 `WorkflowCard` 参数可动态创建不同实例，Go 永远传 nil，丧失动态创建能力。

---

### G-06: workflow_manager.go GetWorkflow 找不到资源返回 error vs Python 返回 None

**文件**: `workflow_manager.go:136-149`

**差异**: Python 返回 None（静默失败），Go 返回 error。行为差异影响上层调用者。

---

### G-07: controllers.go 缺少 Restore 方法

**文件**: `controllers.go`

**Python 原文** (`action.py:261-267`):
```python
def restore(self, snapshot: dict[str, Any]) -> None:
    self._actions.clear()
    self._actions.update(dict(snapshot.get("actions", {})))
    ...
```

**差异**: Go 有 `Snapshot()` 但无 `Restore()`。Python 中 snapshot/restore 是配对功能。

---

### G-08: controllers.go RunAction 缺少 panic recover

**文件**: `controllers.go:336`

**Python 原文**: `run_action` 有 try/except 包裹 handler 调用，handler 异常时返回 `{"ok": false, "error": ...}`。

**Go 代码**: 无 recover，handler panic 会导致程序崩溃。

---

### G-09: agent_tool.go createSubAgent 缺失 mcps/backend/prompt_mode/factory_kwargs

**文件**: `agent_tool.go:306-322`

**Python 原文** (`code_agent_rail.py:234-254`): create_kwargs 包含 `mcps`, `backend`, `prompt_mode`, `factory_kwargs`。

**Go 代码**: CreateDeepAgentParams 缺少这些字段。

---

### G-10: agent_tool.go Invoke() 缺少 session 非空校验

**文件**: `agent_tool.go:84-98`

**Python 原文**:
```python
if not isinstance(parent_session, Session):
    raise build_error(StatusCode.TOOL_TASK_TOOL_INVOKED, reason="Agent tool requires a valid session")
```

**Go 代码**: 不强制要求 session 存在，session 为 nil 时 parentSessionID 默认为 "default"。

---

### G-11: BaseOptimizerMixin.Bind() 缺少 targets 为空时回退 DefaultTargets()

**文件**: `evolving/optimizer/base.go:167`

**Python 原文**:
```python
self._targets = list(targets or self.default_targets())
```

**Go 代码**: 直接赋值 `m.targets = targets`，没有 DefaultTargets() 回退。

---

### G-12: TextualParameter.Gradients 类型从 Any 收窄为 string

**文件**: `evolving/optimizer/base.go:72`

**Python 原文**:
```python
self.gradients: Dict[str, Any] = {}  # target -> gradient value (str or list)
```

**Go 代码**:
```go
Gradients map[string]string  // 空字符串 "" 表示 nil
```

**差异**: Python 梯度值可以是 `Any`（str 或 list），Go 只允许 string。list 类型梯度值无法表达。

---

### G-13: OnAddMessages 额外添加了 isModelCallFailedError 降级逻辑

**文件**: `current_round_compressor.go:637-651`

**Python 原文**: `on_add_messages` 中所有异常都直接抛 `CONTEXT_EXECUTION_ERROR`。

**Go 代码**: 在 `OnAddMessages` 层面额外添加了 `isModelCallFailedError` 降级处理（返回 nil 跳过）。由于子方法 `Compress` 和 `MergeSummaryBlocks` 内部已处理了 LLM 调用失败，这层降级可能是冗余的。

---

## 四、提示问题（🔵 共 12 个）

### T-01: 日志级别不一致（compressor）

| 位置 | Python | Go | 说明 |
|------|--------|-----|------|
| Compress: 跳过（Token 不足） | `logger.info` | `logger.Debug` | Go 降级 |
| Compress: 压缩无收益 | `logger.info` | `logger.Debug` | Go 降级 |
| MergeSummaryBlocks: 合并失败 | `logger.info` | `logger.Warn` | Go 升级 |

### T-02: formatValue 日志缺少关键字段

**文件**: `textable_variable.go:188-190`

Go 记录 `placeholder` 字段值（`fmt.Sprintf("%v", value)`），Python 记录占位符名称。Go 缺少 `input_data`、`output_data` 字段。

### T-03: security.go 警告文本中文 vs 英文

Go 使用中文警告文本，Python 使用英文。可能是项目有意的本地化设计。

### T-04: forwardLoop 步骤11异常处理只覆盖串行分支

流式和并行非流式分支的错误不会触发步骤11异常处理。Python 的 try-except 包裹了所有分支。

### T-05: handoff_tool.go Invoke 缺少字符串/非字典类型 inputs 的处理

Python 有 3 层防御（str→json.loads/str→reason、非dict→空dict），Go 直接声明 `map[string]any` 跳过。如果 Go 框架保证传入解析后的 map 则可接受。

### T-06: prompt_manager.go RemovePrompt/GetPrompt 行为差异

Python 的 `remove_prompt` 对不存在的 key 返回 None（幂等），Go 返回 error。Python 的 `get_prompt` 对不存在的 template 返回 None，Go 返回 error。

### T-07: tag_manager.go tags 参数不支持单个 Tag

Python 接受 `List[Tag] | Tag`，Go 只接受 `[]Tag`。

### T-08: controllers.go Snapshot 缺少 action_specs 深拷贝

Python 用 `copy.deepcopy`，Go 直接引用原 map。

### T-09: innerVarName 常量未使用

**文件**: `textable_variable.go:42`

`innerVarName = "__inner__"` 在整个包中从未被使用。

### T-10: CreateModelClient 错误消息包含 `llm_` 前缀

**文件**: `registry.go:105`

Go 列出完整 key（含 `llm_` 前缀），Python 列出去掉前缀的名称。

### T-11: ClientFactory 无 error 返回

**文件**: `registry.go:32`

Go 的 `ClientFactory` 返回 `BaseModelClient`（无 error），创建失败只能 panic 或返回 nil。

### T-12: buildDragPayload 偏移量缺失附带 _normalize_offset 函数

**文件**: `controllers.go`

Python 有 `_normalize_offset` 辅助函数，Go 缺少对应实现。

---

## 五、待回填占位确认

以下 `⤵️` 标记的代码确实尚未实现，属于设计预期：

| 位置 | 占位内容 | 回填计划 |
|------|---------|---------|
| react_invoke.go:255 | Workflow 中断写入流 | ⤵️ 6.12 |
| react_invoke.go:366 | Workflow: loadInterruptionState | ⤵️ 6.11 |
| react_invoke.go:386 | Skill: 更新技能提示词区段 | ⤴️ Skill |
| react_invoke.go:668 | Workflow 中断检测 | ⤵️ 6.11 |
| browser_move/service.go 多处 | ManagedBrowserDriver / WorkerAgent | ⤵️ 9.38-49 |
| browser_move/runtime.go 多处 | Playwright MCP 工具 | ⤵️ 9.38-49 |

---

## 六、修复优先级建议

| 优先级 | 问题编号 | 修复工作量 | 说明 |
|--------|---------|-----------|------|
| P0 | S-05 | 低 | encodeImage 一行修改，立即生效 |
| P0 | S-01 | 低 | MergeSummaryBlocks 逻辑修正 |
| P0 | S-02 | 中 | Update 方法签名需改，涉及调用链 |
| P1 | S-06 | 中 | buildDragPayload 增加 offset 字段 |
| P1 | S-07 | 中 | Cancel 签名变更，涉及调用链 |
| P1 | S-08 | 中 | forwardLoop 增加 recover |
| P2 | G-01 | 低 | TopP 默认值修正 |
| P2 | G-04 | 低 | sub_agent_outputs 传递 |
| P2 | G-05 | 中 | WorkflowCard 参数传递 |
| P3 | S-03/S-04 | — | 等 6.11-6.12 实现后回填 |
| P3 | G-02 | 中 | 需确认设计意图 |
