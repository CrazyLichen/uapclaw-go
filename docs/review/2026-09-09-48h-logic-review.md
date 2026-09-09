# 48 小时代码逻辑审查报告

> 审查时间：2026-09-09
> 审查范围：48 小时内 4 个提交
> Python 参考：`/home/opensource/agent-core/openjiuwen/` + `/home/opensource/jiuwenswarm-develop/jiuwenswarm/`

---

## 一、提交概览与涉及章节

| 提交 | 描述 | 涉及章节 | 变更文件数 |
|------|------|---------|-----------|
| `5333543a` | 修复 2026-08-31 审查文档 12 项逻辑问题 | 9.65a/6.19/7.1-7.8/9.72b/10.6.13/10.6.3/10.3.15-18 | 29 |
| `43c4688e` | 删除已完成的审查文档 | — | 1 |
| `b024aa2a` | 对齐 Python HookExecutor 无参构造，新增 RegisterConfig | 10.3.23.3/10.3.23.4/10.3.23.5 | 10 |
| `35661814` | 修复 CI 流水线两项失败 | 9.60/9.72b/10.3.23.3 | 3 |

---

## 二、问题汇总

| 编号 | 严重级别 | 章节 | 问题摘要 |
|------|---------|------|---------|
| S-01 | 严重 | 10.3.23.3 | `HookExecutor.RunAll` 中 `type: ""` 与 Python 行为不一致 |
| S-02 | 严重 | 10.3.23.3 | `runCommandHook` 缺少进程被 kill（exitCode=-1）路径判断 |
| S-03 | 严重 | 6.19 | `IntentToolkits.GetOpenAIToolSchemas` choices 参数未生效 |
| S-04 | 严重 | 6.19 | `IntentToolkits.ModifyTask` targetTaskID 应为 taskID 而非新 UUID |
| S-05 | 严重 | 9.65a | `InMemoryTeamDatabase.ClaimTask` 缺少 assignee 已存在检查 |
| S-06 | 严重 | 9.65a | SQL `GetTasksByAssignee` 查询不兼容 nullable assignee 字段 |
| S-07 | 严重 | 9.65a | SQL `GetMessages` 查询不兼容 nullable to_member_name 字段 |
| M-01 | 一般 | 10.3.23.3 | `runCommandHook` 缺少 Python 中 kill 失败的 debug 日志 |
| M-02 | 一般 | 10.3.23.3 | `runPromptHook` 缺 `isinstance(data, dict)` 防护注释 |
| M-03 | 一般 | 7.8 | `MemUpdateChecker.Check` 与 Python 异常类型处理范围不一致 |
| M-04 | 一般 | 7.8 | `MemUpdateChecker` 缺少 Python 中 `old_memories` 为空时的早期退出优化 |
| M-05 | 一般 | 9.72b | `ToolOptimizerBase.OptimizeTool` 中 `llm_api_key` 在循环内重复设置 |
| M-06 | 一般 | 10.6.3 | `AvatarPromptRail.rejectTool` 返回 map 而非 string 与 Python 不一致 |
| M-07 | 一般 | 10.6.13 | `GetForbiddenMemoryPrompt` 错误处理与 Python 日志级别不一致 |
| M-08 | 一般 | 9.65a | SQL `ClaimTask` 冗余 nil 检查 |
| M-09 | 一般 | 7.8 | `MemUpdateChecker` 旧记忆 map 转 orderedmap 丢失插入顺序 |
| T-01 | 提示 | 10.3.23.3 | doc.go 中 `LLMConfig` 描述已过时 |
| T-02 | 提示 | 10.6.3 | `AvatarPromptRail` 中 `memoryAllTools` 可提前定义（已优化） |
| T-03 | 提示 | 10.6.13 | `GetForbiddenMemoryPrompt` 每次调用都 `config.New("")` 创建实例 |
| T-04 | 提示 | 7.8 | `MemUpdateChecker` 日志缺少 Python 中的 `event_type` 字段 |
| T-05 | 提示 | 10.3.15-18 | `session_history.go` 的 `SerializeValue` 递归深度无限制 |

---

## 三、问题详情

---

### S-01：`HookExecutor.RunAll` 中 `type: ""` 与 Python 行为不一致

**章节**：10.3.23.3 HookExecutor
**严重级别**：严重

**问题描述**：Go 的 `RunAll` 过滤逻辑中，`hookType == ""` 被归入 command 分支执行，而 Python 中 `cfg.get("type", "command")` 只在 key **缺失**时返回默认值 `"command"`，显式设置 `type: ""` 时会跳过。

**Python 样例**：
```python
# Python executor.py:46-51
tasks = []
for cfg in hook_configs:
    hook_type = cfg.get("type", "command")  # key 缺失 → "command"；key 存在但值="" → ""
    if hook_type == "command":
        tasks.append(self._run_command_hook(cfg, hook_input))
    elif hook_type == "prompt":
        tasks.append(self._run_prompt_hook(cfg, hook_input))
    # hook_type == "" → 不匹配任何分支，被跳过
```

**Go 问题代码**：
```go
// Go executor.go:96-101
hookType, _ := cfg["type"].(string)
// hookType == "" 包含两种情况：key 缺失（应归入 command）和显式 ""（应跳过）
if hookType == string(hookscfg.HookTypeCommand) || hookType == "" || hookType == string(hookscfg.HookTypePrompt) {
    validHooks = append(validHooks, indexedHook{idx: i, cfg: cfg})
}
```

**修复方案**：
```go
// 区分 key 缺失（默认 command）和显式 ""（跳过）
hookType, hasType := cfg["type"].(string)
if !hasType {
    hookType = "command" // key 缺失 → 默认 command，对齐 Python cfg.get("type", "command")
}
if hookType == string(hookscfg.HookTypeCommand) || hookType == string(hookscfg.HookTypePrompt) {
    validHooks = append(validHooks, indexedHook{idx: i, cfg: cfg})
}
```

同样在执行阶段也要对应调整：
```go
hookType, hasType := cfg["type"].(string)
if !hasType {
    hookType = "command"
}
if hookType == string(hookscfg.HookTypeCommand) {
    results[resultIdx] = e.runCommandHook(ctx, cfg, hookInput)
} else if hookType == string(hookscfg.HookTypePrompt) {
    results[resultIdx] = e.runPromptHook(ctx, cfg, hookInput)
}
```

**流程示例**：当配置 `{"type": "", "command": "echo hello"}` 传入时：
- Python：`cfg.get("type", "command")` 返回 `""`，不匹配 command 也不匹配 prompt，**跳过**
- Go（当前）：`hookType == ""` 匹配，**执行 command hook**
- Go（修复后）：`hasType=true, hookType=""` 不匹配 command 也不匹配 prompt，**跳过** ✓

---

### S-02：`runCommandHook` 缺少进程被 kill（exitCode=-1）路径判断

**章节**：10.3.23.3 HookExecutor
**严重级别**：严重

**问题描述**：Python 在超时 kill 后检查 `returncode is None`，返回 `"hook process killed"` 错误。Go 中被 signal kill 的进程退出码为 -1，但 Go 只判断了 `returnCode == 0`、`returnCode == 2`、else 三个分支，缺少对 `-1` 的专门处理。

**Python 样例**：
```python
# Python executor.py:111-115
if returncode is None:
    return HookResult(
        outcome=HookOutcome.NON_BLOCKING_ERROR,
        error="hook process killed",
    )
```

**Go 问题代码**：
```go
// Go executor.go:318-353
if returnCode == 0 {
    return ParseCommandOutput(stdout)
}
if returnCode == 2 {
    // ... blocking 逻辑
}
// 其他退出码 → NON_BLOCKING_ERROR(stderr or "exit code -1")
errMsg := strings.TrimSpace(stderr)
if errMsg == "" {
    errMsg = fmt.Sprintf("exit code %d", returnCode)
}
return HookResult{Outcome: HookOutcomeNonBlockingError, Error: errMsg}
```

当进程被 signal kill 时，`returnCode == -1`，走 else 分支返回 `"exit code -1"`，而 Python 返回 `"hook process killed"`。**错误信息不一致**。

**修复方案**：
```go
// 对齐 Python: if returncode is None → "hook process killed"
if returnCode == -1 {
    return HookResult{Outcome: HookOutcomeNonBlockingError, Error: "hook process killed"}
}
```

插入位置：在超时判断之后、`returnCode == 0` 之前。

---

### S-03：`IntentToolkits.GetOpenAIToolSchemas` choices 参数未生效

**章节**：6.19 IntentToolkits
**严重级别**：严重

**问题描述**：Go 的 `GetOpenAIToolSchemas` 无论 choices 参数是否传入，都返回全部 tool schema。Python 虽然也有相同的 bug（遍历 `self._tool_schema_choices.keys()` 而非 `choices`），但 Go 代码注释标为"对齐 Python Bug"——这意味着 Go **刻意复刻了 Python 的 bug**而非修复它。

**Python 样例**：
```python
# Python intent_toolkits.py:383-392
def get_openai_tool_schemas(self, choices: List[str] = None) -> List[Dict]:
    if not choices:
        return list(self._tool_schema_choices.values())
    # Python Bug: 应遍历 choices 而非 self._tool_schema_choices.keys()
    return [self._tool_schema_choices[k] for k in self._tool_schema_choices.keys()]
```

**Go 问题代码**：
```go
// Go intent_toolkits.go:217-231
func (t *IntentToolkits) GetOpenAIToolSchemas(choices ...string) []map[string]any {
    if len(choices) == 0 {
        // ...返回全部
    }
    // 对齐 Python Bug: choices 非空时遍历 self._tool_schema_choices.keys() 而非 choices
    for _, v := range t.toolSchemaChoices {
        result = append(result, v)
    }
    return result
}
```

**修复方案**：应修复此 bug 而非复刻：
```go
func (t *IntentToolkits) GetOpenAIToolSchemas(choices ...string) []map[string]any {
    if len(choices) == 0 {
        result := make([]map[string]any, 0, len(t.toolSchemaChoices))
        for _, v := range t.toolSchemaChoices {
            result = append(result, v)
        }
        return result
    }
    // 修复：按 choices 参数过滤
    result := make([]map[string]any, 0, len(choices))
    for _, c := range choices {
        if v, ok := t.toolSchemaChoices[c]; ok {
            result = append(result, v)
        }
    }
    return result
}
```

---

### S-04：`IntentToolkits.ModifyTask` targetTaskID 应为 taskID 而非新 UUID

**章节**：6.19 IntentToolkits
**严重级别**：严重

**问题描述**：Go 的 `ModifyTask` 使用 `uuid.New().String()` 生成新的 `targetTaskID`，而 Python 直接使用传入的 `task_id`。这导致修改意图指向了错误的目标任务 ID。

**Python 样例**：
```python
# Python intent_toolkits.py:351-365
async def modify_task(self, confidence: float, task_id: str, new_task_description: str) -> Tuple[Intent, str]:
    # ...
    return Intent(
        intent_type=IntentType.MODIFY_TASK,
        event=self.event,
        target_task_id=task_id,  # ← 直接使用传入的 task_id
        target_task_description=new_task_description,
        depend_task_id=[task_id],
        modification_details=new_task_description,
        confidence=confidence,
        clarification_prompt=None,
    ), (...)
```

**Go 问题代码**：
```go
// Go intent_toolkits.go:172-192
func (t *IntentToolkits) ModifyTask(confidence float64, taskID string, newTaskDescription string) (*schema.Intent, string, error) {
    // ...
    targetTaskID := uuid.New().String()  // ← BUG: 应使用 taskID 而非新 UUID
    intent, err := schema.NewIntent(
        schema.IntentModifyTask,
        t.event,
        schema.WithTargetTaskID(targetTaskID),  // ← 应改为 taskID
        schema.WithTargetTaskDescription(newTaskDescription),
        schema.WithDependTaskID([]string{taskID}),
        schema.WithModificationDetails(newTaskDescription),
        schema.WithConfidence(confidence),
    )
```

**修复方案**：
```go
func (t *IntentToolkits) ModifyTask(confidence float64, taskID string, newTaskDescription string) (*schema.Intent, string, error) {
    if confidence < t.confidenceThreshold {
        intent, result := t.lowConfidenceIntent(confidence)
        return intent, result, nil
    }
    // 对齐 Python: target_task_id=task_id（不是新 UUID）
    intent, err := schema.NewIntent(
        schema.IntentModifyTask,
        t.event,
        schema.WithTargetTaskID(taskID),  // 修复：直接使用 taskID
        schema.WithTargetTaskDescription(newTaskDescription),
        schema.WithDependTaskID([]string{taskID}),
        schema.WithModificationDetails(newTaskDescription),
        schema.WithConfidence(confidence),
    )
    // ...
```

**流程示例**：
- 用户调用 `modify_task(confidence=0.9, task_id="task-123", new_task_description="新描述")`
- Python：`Intent(target_task_id="task-123", depend_task_id=["task-123"])` — 修改操作指向原任务
- Go（当前）：`Intent(target_task_id="uuid-xxx-yyy", depend_task_id=["task-123"])` — 修改操作指向不存在的 ID

---

### M-01：`runCommandHook` 缺少 Python 中 kill 失败的 debug 日志

**章节**：10.3.23.3 HookExecutor
**严重级别**：一般

**Python 样例**：
```python
# Python executor.py:91-97
except asyncio.TimeoutError:
    try:
        if proc is not None:
            proc.kill()
            await proc.wait()
    except Exception:
        logger.debug("Failed to kill hook process after timeout", exc_info=True)
```

**Go 问题代码**：Go 依赖 `exec.CommandContext` 自动 kill，但没有 kill 失败时的日志记录。

**修复方案**：可选。Go 的 `CommandContext` 在 context 取消后会发送 kill 信号并等待进程退出，行为已等价。如需严格对齐可补充：
```go
if timeoutCtx.Err() == context.DeadlineExceeded {
    // Go 的 CommandContext 已自动 kill，但补充对齐日志
    logger.Debug(logComponent).Int("timeout", timeout).Str("command", command).Msg("hook 子进程超时，已 kill")
    return HookResult{Outcome: HookOutcomeNonBlockingError, Error: fmt.Sprintf("hook timeout after %ds", timeout)}
}
```

---

### M-02：`runPromptHook` 缺 `isinstance(data, dict)` 防护注释

**章节**：10.3.23.3 HookExecutor
**严重级别**：一般

**问题描述**：Python 在 `extract_json_from_response` 后对 data 做了 `isinstance(data, dict)` 检查，Go 的 `ExtractJSONFromResponse` 返回类型是 `map[string]any`，不可能返回非 dict。但缺少注释说明这个隐含假设。

**Python 样例**：
```python
# Python executor.py:195
decision = data.get("decision", "allow") if isinstance(data, dict) else "allow"
# Python executor.py:205
if isinstance(data, dict):
    if "modifiedInput" in data:
        result.modified_input = data["modifiedInput"]
```

**修复方案**：在 Go 的 `ExtractJSONFromResponse` 调用处添加注释：
```go
// ExtractJSONFromResponse 保证只返回 map[string]any（非 dict/非 JSON 均返回空 map），
// 因此此处无需 isinstance(data, dict) 检查，与 Python 行为等价
data := ExtractJSONFromResponse(result.text)
```

---

### M-03：`MemUpdateChecker.Check` 与 Python 异常类型处理范围不一致

**章节**：7.8 MemUpdateChecker
**严重级别**：一般

**问题描述**：Python 只捕获 `(KeyError, ValueError)` 两种异常进行重试，Go 捕获所有 LLM 调用和解析错误。Go 的范围更宽，可能导致本应快速失败的错误（如权限问题）被不必要地重试。

**Python 样例**：
```python
# Python mem_update_checker.py:216-234
except (KeyError, ValueError) as e:
    if attempt < retries - 1:
        memory_logger.warning(
            f"Memory check parse error, retrying ({attempt + 1}/{retries}): {e}",
            ...
        )
        continue
    else:
        memory_logger.error("Memory check failed after retries", ...)
        return [MemoryActionItem(id=mid, content=content, status=MemoryStatus.ADD) for mid, content in new_memories.items()]
```

**Go 问题代码**：
```go
// Go update_checker.go:170-218
for attempt := 0; attempt < cfg.retries; attempt++ {
    response, invokeErr := cfg.model.Invoke(ctx, msgsParam, ...)
    if invokeErr != nil {
        // 任何 Invoke 错误都重试 — 范围比 Python 宽
        if attempt < cfg.retries-1 {
            continue
        }
        return allAddItems(newMemories), nil
    }
    // ...
    items, parseErr := parseCheckItems(parsedResult)
    if parseErr != nil {
        // 任何解析错误都重试 — 范围比 Python 宽
        if attempt < cfg.retries-1 {
            continue
        }
        return allAddItems(newMemories), nil
    }
}
```

**修复方案**：Go 中 `Invoke` 错误可细分为可重试（网络超时/限流）和不可重试（认证失败/权限不足），建议对 `Invoke` 错误区分处理：
```go
response, invokeErr := cfg.model.Invoke(ctx, msgsParam, ...)
if invokeErr != nil {
    // 对齐 Python：LLM 调用失败直接返回 fallback（不重试）
    // Python 只有 parse 阶段的 KeyError/ValueError 才重试
    logger.Error(logComponent).Err(invokeErr).Msg("记忆冲突检查 LLM 调用失败")
    return allAddItems(newMemories), nil
}
```

---

### M-04：`MemUpdateChecker` 缺少 Python 中 `old_memories` 为空时的早期退出优化

**章节**：7.8 MemUpdateChecker
**严重级别**：一般

**问题描述**：Python 注释中 `if not base_chat_model` 早期退出包含"no old memories"条件，但实际实现只检查了 `base_chat_model`。Go 也没有对 `old_memories` 为空的情况做早期退出。当没有旧记忆时，不需要做冲突检查，直接返回所有新记忆为 ADD 即可，这是一个性能优化点。

**Python 样例**：
```python
# Python mem_update_checker.py:148-149
# Skip checking if no old memories or no model
if not base_chat_model:
    # Return all new memories as ADD
```

**Go 当前代码**：Go 只检查 `cfg.model == nil`，未检查 `oldMemories.Len() == 0`。

**修复方案**：
```go
// 无旧记忆或无 LLM 模型 → 直接返回所有新记忆为 ADD
// 对齐 Python: Skip checking if no old memories or no model
if cfg.model == nil || (oldMemories != nil && oldMemories.Len() == 0) {
    logger.Debug(logComponent).
        Int("new_count", newMemories.Len()).
        Int("old_count", func() int { if oldMemories != nil { return oldMemories.Len() }; return 0 }()).
        Msg("无旧记忆或无 LLM 模型，跳过记忆冲突检查")
    return allAddItems(newMemories), nil
}
```

---

### M-05：`ToolOptimizerBase.OptimizeTool` 中 `llm_api_key` 在循环内重复设置

**章节**：9.72b ToolOptimizer
**严重级别**：一般

**问题描述**：Go 在 `OptimizeTool` 的循环内重复设置 `configEg["llm_api_key"]` 和 `configDesc["llm_api_key"]`，但这些值在构造函数中已设置且循环中不会改变。Python 中没有在循环内设置 `llm_api_key`。

**Python 样例**：
```python
# Python tool_call/base.py — optimize_tool 中无循环内 llm_api_key 设置
# llm_api_key 在 __init__ 中一次性设置
```

**Go 问题代码**：
```go
// Go tool_call/base.go:172-173
// 对应 Python: default_config_desc['llm_api_key'] = self.llm_api_key
// 对应 Python: default_config_eg['llm_api_key'] = self.llm_api_key
b.configEg["llm_api_key"] = b.llmAPIKey
b.configDesc["llm_api_key"] = b.llmAPIKey
```

**修复方案**：删除循环内的重复设置（构造函数已设置），或者确认这是有意为之的防御性代码后添加注释说明。

---

### S-05：`InMemoryTeamDatabase.ClaimTask` 缺少 assignee 已存在检查

**章节**：9.65a TeamDB
**严重级别**：严重

**问题描述**：内存实现的 `ClaimTask` 完全跳过了 `if task.assignee` 检查，允许已认领的任务被二次认领。SQL 实现已正确添加了该检查，但内存实现遗漏了。

**Python 样例**：
```python
# Python task_dao.py:403-405
if task.assignee:
    team_logger.warning("Task %s is already claimed by member %s", task_id, task.assignee)
    return False
```

**Go 问题代码**：
```go
// Go memory_impl.go:466-479
func (db *InMemoryTeamDatabase) ClaimTask(_ context.Context, taskID, assignee string) (bool, error) {
    // ... 无 assignee 检查，直接 FSM 校验 + 设置
    if !IsValidTaskTransition(task.Status, fsm.TaskStatusClaimed) {
        return false, nil
    }
    task.Status = fsm.TaskStatusClaimed
    task.Assignee = StringPtr(assignee)
```

**修复方案**：
```go
func (db *InMemoryTeamDatabase) ClaimTask(_ context.Context, taskID, assignee string) (bool, error) {
    db.mu.Lock()
    defer db.mu.Unlock()
    task, exists := db.tasks[taskID]
    if !exists {
        return false, nil
    }
    // 对齐 Python: if task.assignee → warning + return False
    if task.Assignee != nil && *task.Assignee != "" {
        logger.Warn(logComponent).Str("task_id", taskID).Str("assignee", *task.Assignee).Msg("任务已被认领")
        return false, nil
    }
    if !IsValidTaskTransition(task.Status, fsm.TaskStatusClaimed) {
        return false, nil
    }
    task.Status = fsm.TaskStatusClaimed
    task.Assignee = StringPtr(assignee)
    task.UpdatedAt = GetCurrentTime()
    return true, nil
}
```

**流程示例**：
- 任务 `task-1` 已被 `member-A` 认领（status=CLAIMED, assignee="member-A"）
- `member-B` 再次调用 `ClaimTask("task-1", "member-B")`
- Python：`if task.assignee` 为 True → warning + return False → **拒绝**
- Go（当前）：跳过检查 → `task.Assignee = "member-B"` → **assignee 被覆盖**

---

### S-06：SQL `GetTasksByAssignee` 查询不兼容 nullable assignee 字段

**章节**：9.65a TeamDB
**严重级别**：严重

**问题描述**：DDL 已将 `assignee` 改为 `TEXT`（nullable），模型改为 `*string`。但 SQL 查询仍用 `assignee = ?`，当传入 `assignee = ""` 查找"未分配"任务时，SQL `= ''` 不会匹配 `NULL` 行（SQL 中 `NULL != ''`）。内存实现已用 nil→"" 映射处理，但 SQL 实现没有。

**Python 样例**：
```python
# Python task_dao.py:378-380
query = select(team_task_model).where(
    team_task_model.team_name == team_name,
    team_task_model.assignee == assignee_id,
)
```

**Go 问题代码**：
```go
// Go sql_task_dao.go:88-89
query := d.db.WithContext(ctx).Table(table).
    Where("team_name = ? AND assignee = ?", teamName, assignee)
```

**修复方案**：
```go
query := d.db.WithContext(ctx).Table(table).Where("team_name = ?", teamName)
if assignee == "" {
    query = query.Where("assignee IS NULL")
} else {
    query = query.Where("assignee = ?", assignee)
}
if status != "" {
    query = query.Where("status = ?", status)
}
```

**流程示例**：数据库中存在任务 assignee=NULL（未分配）和 assignee="member-A"（已认领）
- 查询 `GetTasksByAssignee(team, "", "")` 期望返回未分配的任务
- Python `assignee_id=""` 时 SQLAlchemy 的行为取决于具体版本，但 SQL 标准 `= ''` 不匹配 NULL
- Go（当前）：`WHERE assignee = ''` → **返回空**（NULL 不匹配空字符串）
- Go（修复后）：`WHERE assignee IS NULL` → **返回未分配任务** ✓

---

### S-07：SQL `GetMessages` 查询不兼容 nullable to_member_name 字段

**章节**：9.65a TeamDB
**严重级别**：严重

**问题描述**：与 S-06 同理，`to_member_name` 改为 nullable 后，SQL `= ?` 不匹配 NULL 值。

**修复方案**：同 S-06 模式，区分空字符串（查广播/未分配）和指定成员查询：
```go
if toMemberName == "" {
    query = query.Where("to_member_name IS NULL")
} else {
    query = query.Where("to_member_name = ?", toMemberName)
}
```

---

### M-06：`AvatarPromptRail.rejectTool` 返回 map 而非 string 与 Python 不一致

**章节**：10.6.3 AvatarPromptRail
**严重级别**：一般

**问题描述**：Python 中 `_reject_tool` 直接将字符串赋给 `tool_result`，Go 改为 `map[string]any{"error": message}`。可能导致下游 LLM 输入格式差异。

**Python 样例**：
```python
# Python avatar_rail.py:184
ctx.inputs.tool_result = message  # 直接赋值字符串
```

**Go 问题代码**：
```go
// Go avatar_rail.go:257
toolInputs.ToolResult = map[string]any{"error": message}
```

**修复方案**：除非有明确的下游需求要求 map 格式，否则应恢复为字符串对齐 Python：
```go
toolInputs.ToolResult = message  // 对齐 Python: ctx.inputs.tool_result = message
```

---

### M-07：`GetForbiddenMemoryPrompt` 错误处理与 Python 日志级别不一致

**章节**：10.6.13 ForbiddenMemory
**严重级别**：一般

**问题描述**：Python 中 `_get_memory_forbidden_config()` 异常时用 `logger.warning`，Go 的 `injectForbiddenMemory` 收到 error 后用 `logger.Debug`。日志级别不一致可能导致生产环境无法及时发现配置加载问题。

**Python 样例**：
```python
# Python forbidden.py:25-27
except Exception as e:
    logger.warning("[forbidden] Failed to load memory forbidden config: %s", e)
    return {"enabled": False, "patterns": [], "description": {}}
```

**Go 问题代码**：
```go
// Go avatar_rail.go:234-235
forbidden, err := commmem.GetForbiddenMemoryPrompt(language)
if err != nil {
    logger.Debug(avatarLogComponent).Err(err).Str("language", language).Msg("获取禁止记忆提示词失败，跳过注入")
```

**修复方案**：将 `logger.Debug` 改为 `logger.Warn` 对齐 Python：
```go
logger.Warn(avatarLogComponent).Err(err).Str("language", language).Msg("获取禁止记忆提示词失败，跳过注入")
```

---

### M-08：SQL `ClaimTask` 冗余 nil 检查

**章节**：9.65a TeamDB
**严重级别**：一般

**Go 问题代码**：
```go
// Go sql_task_dao.go:114-119
if task.Assignee != nil && *task.Assignee != "" {
    assigneeStr := ""
    if task.Assignee != nil {  // ← 冗余检查！外层已判断 != nil
        assigneeStr = *task.Assignee
    }
    logger.Warn(logComponent).Str("task_id", taskID).Str("assignee", assigneeStr).Msg("任务已被认领")
```

**修复方案**：
```go
if task.Assignee != nil && *task.Assignee != "" {
    logger.Warn(logComponent).Str("task_id", taskID).Str("assignee", *task.Assignee).Msg("任务已被认领")
    return nil
}
```

---

### M-09：`MemUpdateChecker` 旧记忆 map 转 orderedmap 丢失插入顺序

**章节**：7.8 MemUpdateChecker
**严重级别**：一般

**问题描述**：`coding_memory_tool_ops.go` 中将 `map[string]string`（搜索结果）转为 `orderedmap` 时，Go 的 map 遍历顺序不确定，导致旧记忆在 orderedmap 中的插入顺序与 Python 不一致。

**Python 样例**：
```python
# Python 中 _get_related_old_memories 返回 dict 按搜索结果插入顺序
# 遍历时也按此顺序，formatInput 中旧记忆按插入顺序输出
```

**Go 问题代码**：
```go
// Go coding_memory_tool_ops.go:489-493
oldMemOrdered := orderedmap.New[string, string]()
for k, v := range oldMemories {  // map 遍历顺序不确定
    oldMemOrdered.Set(k, v)
}
```

**修复方案**：`runChecker` 中应保留搜索结果的顺序信息，将 `oldMemories` 从 `map[string]string` 改为按搜索结果顺序构建的有序结构。或在此处按 key 排序后插入以获得确定性输出。

---

### T-01：doc.go 中 `LLMConfig` 描述已过时

**章节**：10.3.23.3 HookExecutor
**严重级别**：提示

**问题描述**：`b024aa2a` 重构删除了 `LLMConfig` 结构体（改为 `RegisterConfig` 全局注册），但 `doc.go` 中仍描述 `executor.go` 包含 `LLMConfig`。

**修复方案**：更新 doc.go：
```go
//	├── executor.go       # HookOutcome/HookResult + HookExecutor + RegisterConfig + ParseCommandOutput + ExtractJSONFromResponse
```

---

### T-02：`AvatarPromptRail` 中 `memoryAllTools` 可提前定义

**章节**：10.6.3 AvatarPromptRail
**严重级别**：提示

**问题描述**：Python 在 `before_tool_call` 方法内每次创建 `frozenset`，Go 已将其提升为包级变量（优化），与 Python 行为不同但更合理。无需修改，仅记录差异。

---

### T-03：`GetForbiddenMemoryPrompt` 每次调用都 `config.New("")` 创建实例

**章节**：10.6.13 ForbiddenMemory
**严重级别**：提示

**问题描述**：Go 的 `getMemoryForbiddenConfigSafe` 每次调用都 `config.New("")` 创建新的 Config 实例，而 Python 的 `get_config()` 返回单例。频繁调用可能产生不必要的配置加载开销。

**修复方案**：考虑缓存或接受注入的 Config 实例。

---

### T-04：`MemUpdateChecker` 日志缺少 Python 中的 `event_type` 字段

**章节**：7.8 MemUpdateChecker
**严重级别**：提示

**问题描述**：Python 使用 `event_type=LogEventType.MEMORY_PROCESS` 记录所有记忆相关日志，Go 的日志缺少此字段。

**Python 样例**：
```python
memory_logger.debug(
    "No need to check memories - no old memories or no model",
    event_type=LogEventType.MEMORY_PROCESS,
    metadata={"new_count": len(new_memories), "old_count": len(old_memories)},
)
```

**修复方案**：对齐项目日志规范，在 Go 日志中补充 `event_type` 字段：
```go
logger.Debug(logComponent).
    Str("event_type", "MEMORY_PROCESS").
    Int("new_count", newMemories.Len()).
    Int("old_count", oldMemories.Len()).
    Msg("无 LLM 模型，跳过记忆冲突检查")
```

---

### T-05：`session_history.go` 的 `SerializeValue` 递归深度无限制

**章节**：10.3.15-18 SessionHistory
**严重级别**：提示

**问题描述**：Go 的 `SerializeValue` 对嵌套 dict/list 递归序列化，但没有深度限制。如果传入循环引用或极深嵌套的 map，可能导致栈溢出。Python 同样没有限制，但 Go 的栈默认更小。

**修复方案**：可选——添加递归深度参数，超过 10 层时返回原值。

---

## 四、待回填占位代码审查

| 章节 | 占位标记 | 实际状态 | 风险 |
|------|---------|---------|------|
| 10.3.7-11 | TeamHelpers ☐ | 确认未实现（⤵️ 9.55-9.65） | 低 — 依赖 TeamAgent |
| 9.24 EvolutionRail | P3/P4/P5/P6 ☐ | 确认未实现 | 低 — 逐步迭代 |
| 2.16 | UserConfig.is_sensitive() ⤵️ | 确认未实现 | 中 — 日志可能泄露敏感信息 |
| 5.10 | R6 close_stream 回调注销 ⤵️ | 确认未实现 | 低 — callback_framework 缺 unregister |
| 5.3 | ActorManager 返回类型 ⤵️ | 确认未实现 | 低 — 待后续回填 |
| 5.6 | LoadAgentSessionContainer ⤵️ | 确认未实现 | 低 — 依赖 create_agent_session |
| 9.26 BrowserAgent | 6 处 Playwright MCP 占位 | 确认未实现 | 中 — 核心功能不可用 |

---

## 五、修复优先级建议

| 优先级 | 编号 | 理由 |
|--------|------|------|
| P0（立即） | S-04 | ModifyTask 指向错误 task ID，影响任务修改功能 |
| P0（立即） | S-05 | ClaimTask 缺少 assignee 检查，已认领任务可被覆盖 |
| P0（立即） | S-06, S-07 | SQL nullable 查询不兼容，查询"未分配"返回空结果 |
| P1（本周） | S-01 | type: "" 行为不一致，影响 hook 配置过滤 |
| P1（本周） | S-02 | 进程被 kill 时的错误信息不一致 |
| P1（本周） | S-03 | GetOpenAIToolSchemas choices 参数无效 |
| P2（下版本） | M-03, M-04, M-09 | MemUpdateChecker 行为差异 |
| P2（下版本） | M-01, M-02 | HookExecutor 日志对齐 |
| P2（下版本） | M-06, M-07 | AvatarPromptRail 对齐 |
| P3（可选） | M-05, M-08, T-01~T-05 | 代码质量改进 |
