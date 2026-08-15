# 48 小时代码逻辑审查报告 (2026-08-12)

> 审查范围：48 小时内提交的代码变更，涉及章节 7.2（CodingMemoryTools）、7.5（Frontmatter）、9.65-1（Messager 循环依赖重构）、10.3.3（AgentAdapter 回填）

---

## 一、审查范围

| 章节 | 状态 | Python 参考 | Go 实现 |
|------|------|-------------|---------|
| 7.2 CodingMemoryTools | ✅ | `openjiuwen/core/memory/lite/coding_memory_tool_ops.py` | `agentcore/memory/lite/coding_memory_tool_ops.go` |
| 7.2 通用记忆工具 | ✅ | `openjiuwen/core/memory/lite/memory_tool_ops.py` | `agentcore/memory/lite/tool_ops.go` |
| 7.2 InitMemoryManager | ✅ | `openjiuwen/core/memory/lite/coding_memory_tools.py` + `memory_tools.py` | `agentcore/memory/lite/tools.go` |
| 7.5 Frontmatter | ✅ | `openjiuwen/core/memory/lite/frontmatter.py` | `agentcore/memory/lite/frontmatter.go` |
| 9.65-1 Messager | ✅ | `openjiuwen/agent_teams/messager/` | `agent_teams/messager/` |
| 9.65-1 循环依赖重构 | ✅ | schema/constants/i18n/session_context | `agent_teams/schema/` |
| 10.3.3 CodeAdapter | ✅ | `jiuwenswarm/server/runtime/agent_adapter/interface_code.py` | `swarm/server/adapter/code_adapter.go` |

---

## 二、严重问题（共 7 个）

### S1. CodeAdapter `resolveRuntimeLanguage()` 返回 "cn" 而非 "en" — 与 Python Code 模式英文默认不一致

**Python 样例** (`interface_code.py:196-202`):
```python
@staticmethod
def _resolve_prompt_language() -> str:
    """Code mode always uses English for system prompts."""
    return "en"

def _resolve_runtime_language(self) -> str:
    return self._runtime_language_override or "en"
```

**Go 问题** (`code_adapter.go:271,282-283`):
```go
resolvedLanguage := c.deep.resolveRuntimeLanguage()  // ← 调用 DeepAdapter 的方法，返回 "cn"
// ...
Workspace:  hworkspace.NewWorkspace(c.deep.workspaceDir, resolvedLanguage),  // "cn"
Language:   resolvedLanguage,  // "cn"
```
`DeepAdapter.resolvePromptLanguage()` 默认返回 `"zh"`，`resolveRuntimeLanguage()` 返回 `ResolveLanguage("zh")` → `"cn"`。但 Python CodeAdapter 的 `_resolve_runtime_language` 返回 `"en"`。

**影响范围**：
- `Workspace` 的 `language` 参数不一致（Go 传 "cn"，Python 传 "en"）
- `CreateDeepAgentParams.Language` 不一致
- 所有依赖 language 的 Rail（如 `TaskPlanningRail`、`StructuredAskUserRail`）行为不一致

**修复方案**：CodeAdapter 应覆写 `resolveRuntimeLanguage()` 和 `resolvePromptLanguage()`，返回 `"en"`；或在 `CreateInstance` 中直接使用 `"en"` 替代 `c.deep.resolveRuntimeLanguage()`。

---

### S2. CodeAdapter 没有重写 `buildConfiguredSubagents()`，缺少 explore/plan/code 三个核心子代理

**Python 样例** (`interface_code.py:677-729`):
```python
# 固定挂载：explore_agent（Code 模式核心子代理，始终启用）
explore_spec = build_explore_agent_config(model=model, workspace=workspace, ...)
subagents.append(explore_spec)

# 固定挂载：plan_agent（Code 模式核心子代理，始终启用）
plan_spec = build_plan_agent_config(model=model, workspace=workspace, ...)
subagents.append(plan_spec)

# code_agent 按配置启用
code_spec = build_code_agent_config(...)
subagents.append(code_spec)
```

**Go 问题** (`code_adapter.go:264`):
```go
subagentSpecs, _ := c.deep.buildConfiguredSubagents(c.deep.configCache, configBase)
```
Go 的 CodeAdapter 直接调用 `DeepAdapter.buildConfiguredSubagents()`，没有重写。Python 的 CodeAdapter 重写了 `_build_configured_subagents()`，添加了 explore_agent、plan_agent、code_agent 作为固定子代理。

**影响**：Code 模式下没有 explore/plan/code 子代理，核心功能缺失。Agent 无法进行代码探索、计划、编码等子任务。

**修复方案**：CodeAdapter 新增 `buildConfiguredSubagents()` 方法，在调用 `DeepAdapter.buildConfiguredSubagents()` 后追加 explore_agent、plan_agent、code_agent 子代理配置。

---

### S3. InProcessMessager.Publish 持锁调用 handler — 可能死锁

**Python 样例** (`inprocess.py:46-53`):
```python
async def publish(self, topic: str, message: EventMessage) -> None:
    subs = self._topic_subs.get(topic)
    if not subs:
        return
    for agent_id, handler in list(subs.items()):  # ← 复制后遍历，无锁
        try:
            await handler(message)
```
Python 使用 `list(subs.items())` 创建副本后遍历，且 asyncio 是协作式调度，`await handler` 期间其他协程可以运行。

**Go 问题** (`inprocess.go:92-106`):
```go
b := getBus()
b.mu.Lock()
defer b.mu.Unlock()  // ← 整个 for 循环期间持有锁
subs, ok := b.topicSubs[topicID]
for aid, handler := range subs {
    if err := handler(ctx, message); err != nil {  // ← 持锁期间调用 handler
```
Go 在持有 `b.mu.Lock()` 的情况下调用 handler。如果 handler 内部尝试调用 `Subscribe`、`Unsubscribe`、`RegisterDirectMessageHandler` 等方法（这些方法也会获取 `b.mu.Lock()`），就会导致**死锁**。

**死锁场景流程**：
```
goroutine A: Publish → Lock(bus.mu) → handler(ctx, msg) → Subscribe → Lock(bus.mu) ← 阻塞
```

**修复方案**：在 Publish 中先复制 subs map，然后释放锁再调用 handlers：
```go
func (m *InProcessMessager) Publish(ctx context.Context, topicID string, message *schema.EventMessage) error {
    // ... stamp senderID ...
    b := getBus()
    b.mu.Lock()
    subs := make(map[string]MessagerHandler, len(b.topicSubs[topicID]))
    for k, v := range b.topicSubs[topicID] {
        subs[k] = v
    }
    b.mu.Unlock()
    for aid, handler := range subs {
        if err := handler(ctx, message); err != nil { ... }
    }
    return nil
}
```

---

### S4. CodingMemoryWriteWithContext 创建/追加模式冲突检测逻辑错误 — 不区分 LLM 冗余判断和冲突检测

**Python 样例** (`coding_memory_tool_ops.py:396-420`):
```python
if not file_exists:
    old_memories = await _search_similar(ctx, body, basename, top_k=5, threshold=0.75)
    actions = None
    if old_memories and ctx.manager and ctx.manager.llm:
        actions = await _run_checker(ctx.manager, basename, body, old_memories)

    # REDUNDANT handling
    if actions and not any(a.id == basename for a in actions):
        return WriteResult(success=True, path=resolved, mode=WriteMode.SKIP, ...)

    # Collect conflict info — 仅 LLM 判断为 DELETE 的才算冲突
    conflicting = []
    if actions:
        conflicting = [a.id for a in actions if a.id != basename and a.status == MemoryStatus.DELETE]
```

**Go 问题** (`coding_memory_tool_ops.go:180-195`):
```go
if !fileExists {
    conflictResult = map[string]any{"conflict_detected": false, "conflicting_files": []string{}}
    similarFiles := searchSimilar(toolCtx, body, basename, 5, 0.75)
    if len(similarFiles) > 0 {
        // 所有 searchSimilar 返回的文件都标记为冲突 — 误报
        conflicting := make([]string, 0, len(similarFiles))
        for name := range similarFiles {
            conflicting = append(conflicting, name)
        }
        conflictResult["conflict_detected"] = true
        conflictResult["conflicting_files"] = conflicting
    }
}
```

**差异**：Python 中冲突检测分两层：(1) `searchSimilar` 找到相似文件，(2) `runChecker`（LLM）判断哪些是冗余/需要删除。只有 LLM 返回 `MemoryStatus.DELETE` 的才标记为冲突。Go 直接把所有 `searchSimilar` 返回的文件都标记为冲突，**没有经过 LLM 判断**，导致误报。

**影响**：所有写入操作（即使没有真正冲突）都会被标记为冲突，影响用户体验。当前 `runChecker` 是 TODO（⤵️ 7.8），所以这是预期的降级行为，但降级策略应该更保守（不标记冲突而非全标记冲突）。

**修复方案**：在 `runChecker` 实现之前，降级策略应为：`searchSimilar` 找到相似文件时，仅记录日志提示，不标记 `conflict_detected=true`。等 7.8 MemUpdateChecker 实现后再恢复冲突检测。

---

### S5. prepareAppendMode 追加模式同样误报冲突

**Python 样例** (`coding_memory_tool_ops.py:256-280`):
```python
if old_memories and coding_memory_manager and coding_memory_manager.llm:
    actions = await _run_checker(coding_memory_manager, basename, body, old_memories)
    # REDUNDANT: skip write
    if actions and not any(a.id == basename for a in actions):
        return WriteResult(success=True, path=resolved, mode=WriteMode.SKIP, ...)
    # Collect all conflicts — 仅 DELETE status
    conflicting = []
    for a in actions:
        if a.status == MemoryStatus.DELETE and a.id != basename:
            conflicting.append(basename if a.id == "__self__" else a.id)
```

**Go 问题** (`coding_memory_tool_ops.go:453-468`):
```go
if len(other) > 0 {
    // 所有 searchSimilar 结果都当冲突
    conflicting := make([]string, 0, len(other))
    for name := range other {
        conflicting = append(conflicting, name)
    }
    result["conflict_detected"] = true
    result["conflicting_files"] = conflicting
}
```

**差异**：
1. Python 的冲突列表来自 `runChecker` 返回的 `actions` 中 `status == MemoryStatus.DELETE` 的条目，Go 直接把 `searchSimilar` 的所有结果都当冲突
2. Python 在追加模式下会先检查 REDUNDANT（如果冗余则返回 SKIP），Go 缺少这个检查
3. Go 只检查 `len(other) > 0`，忽略了 `oldMemories["__self__"]` 在冲突判断中的作用

**修复方案**：同 S4，降级策略应为不标记冲突，等 7.8 实现后恢复。

---

### S6. CodeAdapter 热重载时 CodeAgentRail 丢失

**Python 样例** (`interface_code.py:836-848`):
```python
def _get_current_agent_rails(self, config, config_base=None):
    rails_list = super()._get_current_agent_rails(config, config_base)
    if self._code_agent_rail is not None:
        rails_list.append(self._code_agent_rail)
    return rails_list
```

**Go 问题**：CodeAdapter 的 `ReloadAgentConfig` 直接委托给 `DeepAdapter.ReloadAgentConfig()`，没有覆写。DeepAdapter 的 `ReloadAgentConfig` 调用 `getCurrentAgentRails()`，后者调用 `buildAgentRails()`，不会包含 CodeAdapter 的 `buildCodeAgentRails()`。

**影响**：Code 模式热重载后，CodeAgentRail 丢失，自定义 Agent 工具不再可用。

**修复方案**：CodeAdapter 应覆写 `ReloadAgentConfig()`，在调用 `DeepAdapter.ReloadAgentConfig()` 后确保 CodeAdapter 特有的 Rails（CodeAgentRail、CodingMemoryRail 等）被纳入。

---

### S7. CancelAllTasks 缺少 publishUnblockedEvents 调用 — 被取消的任务阻塞的下游任务无法感知解除阻塞

**Python 样例** (`task_manager.py:1060-1076`):
```python
async def cancel_all_tasks(self, skip_assignees=None):
    result = await self.db.task.cancel_all_tasks(self.team_name, skip_assignees=skip_assignees)
    cancelled_tasks = result.get("cancelled_tasks") or []
    unblocked_tasks = result.get("unblocked_tasks") or []

    for task in cancelled_tasks:
        await self._publish_task_event(TaskCancelledEvent(...))

    await self._publish_unblocked_events(unblocked_tasks)  # ← 关键步骤
```

**Go 问题** (`task_manager.go:382-396`):
```go
func (tm *TeamTaskManager) CancelAllTasks(ctx context.Context, skipAssignees []string) ([]*database.TeamTaskBase, error) {
    cancelled, err := tm.db.Task().CancelAllTasks(ctx, tm.teamName, skipAssignees)
    for _, task := range cancelled {
        tm.publishTaskEvent(ctx, schema.TaskCancelledEvent{...})
    }
    tm.maybePublishTaskListDrained(ctx)
    // ← 缺少 tm.publishUnblockedEvents(ctx, unblockedIDs)
    return cancelled, nil
}
```

**影响**：如果一个被取消的任务阻塞了其他任务，那些任务不会被通知已解除阻塞，可能永远停留在 BLOCKED 状态。

**修复方案**：`CancelAllTasks` 应从 `db.Task().CancelAllTasks` 的返回值中提取 unblocked tasks，并调用 `publishUnblockedEvents`。需确认 Go 的 `CancelAllTasks` DAO 方法是否返回 unblocked tasks（Python 的返回了 `result.get("unblocked_tasks")`）。

---

## 三、一般问题（共 11 个）

### G1. InProcessMessager.Publish 直接修改传入的 EventMessage

**Python 样例** (`inprocess.py:130-131`):
```python
if hasattr(message, "sender_id") and not message.sender_id:
    message = message.model_copy(update={"sender_id": self._agent_id})  # 创建新对象
```

**Go 问题** (`inprocess.go:89-91`):
```go
if message.SenderID == "" {
    message.SenderID = agentID  // 直接修改原始对象
}
```

Python 使用 `model_copy` 创建新对象不修改原始 message，Go 直接修改传入的 `*EventMessage` 指针。在并发场景中可能导致数据竞争。

**修复方案**：创建 EventMessage 副本再设置 SenderID，或文档化此行为差异。

---

### G2. RebuildContentWithFrontmatter 中 map 迭代顺序不确定

**Python 样例** (`frontmatter.py:49-50`):
```python
for key, value in fm.items():  # Python 3.7+ dict 保证插入顺序
    fm_lines.append(f"{key}: {value}")
```

**Go 问题** (`frontmatter.go:76-78`):
```go
for key, value := range fm {  // Go map 迭代顺序随机
    fmLines = append(fmLines, key+": "+value)
}
```

Go map 迭代顺序随机，每次调用 `RebuildContentWithFrontmatter` 即使内容不变，输出字符串可能不同。依赖写入后 `ParseFrontmatter` → `RebuildContentWithFrontmatter` 的循环（如 `appendToExistingFile` 中更新 `updated_at`）会产生格式不稳定的文件。

**修复方案**：按固定顺序输出字段，或使用有序结构按 key 排序输出。例如：
```go
// 按 key 排序输出
keys := make([]string, 0, len(fm))
for k := range fm { keys = append(keys, k) }
sort.Strings(keys)
for _, k := range keys {
    fmLines = append(fmLines, k+": "+fm[k])
}
```

---

### G3. InitCodingMemoryManagerAsync 缺少 `llm` 参数

**Python 样例** (`coding_memory_tools.py:20-26`):
```python
async def init_memory_manager_async(
    workspace, agent_id="default", embedding_config=None,
    sys_operation=None, llm=None,  # ← 有 llm 参数
) -> Optional[MemoryIndexManager]:
    manager = await MemoryIndexManager.get(params)
    if manager:
        if llm:
            manager.llm = llm  # ← 传入 LLM
```

**Go 问题** (`tools.go:44`):
```go
func InitCodingMemoryManagerAsync(ctx context.Context, ws *workspace.Workspace, agentID string, embeddingConfig *embedding.EmbeddingConfig, sysOp sysop.SysOperation) (MemoryIndexManager, error) {
    // 没有 llm 参数
```

`manager_impl.go` 中 `memoryIndexManager` 有 `llm any` 字段（标注了 `TODO: 7.2`），但从未被设置。当前 `runChecker` 也是 TODO，所以 `llm` 缺失暂时不影响功能。

**修复方案**：7.8 MemUpdateChecker 实现时，需同时补充 `llm` 参数传递。

---

### G4. CodingMemoryToolContext 通过构造函数设置 NodeName，直接构造会出错

**Python 样例** (`coding_memory_tool_context.py:13-17`):
```python
@dataclass
class CodingMemoryToolContext(LiteMemoryToolContextBase):
    coding_memory_dir: str = ""
    node_name: str = "coding_memory"  # 默认值直接在字段上
```

**Go 问题** (`coding_memory_tool_context.go:6-9`):
```go
type CodingMemoryToolContext struct {
    LiteMemoryToolContextBase
    CodingMemoryDir string
    // NodeName 不在结构体中，依赖构造函数设置
}
```

如果直接构造 `CodingMemoryToolContext{}` 而不使用 `NewCodingMemoryToolContext()`，`NodeName` 会是默认值 `"memory"` 而非 `"coding_memory"`。

**修复方案**：在 `CodingMemoryToolContext` 结构体中显式声明 `NodeName` 字段，或使用 Go 的零值不可变设计（文档化必须使用构造函数）。

---

### G5. CodingMemoryRead/Write/Edit 缺少外层 panic recovery 保护

**Python 样例** (`coding_memory_tool_ops.py:105-147`):
```python
async def coding_memory_read_with_context(...):
    try:                          # ← 外层 try/except
        ...
    except Exception as e:
        logger.error(f"Read failed: {e}")
        return {"success": False, "path": path, "content": "", "error": str(e)}
```

**Go 问题**：`CodingMemoryReadWithContext`、`CodingMemoryWriteWithContext`、`CodingMemoryEditWithContext` 都没有外层 `defer recover()` 保护。如果 `ReadFile` 或其他操作 panic，整个程序会崩溃。

**修复方案**：为每个函数添加 `defer` + `recover()` 保护，返回错误 map。

---

### G6. CodingMemoryWrite/Read/Edit 各错误路径缺少日志记录

**Python 样例** (`coding_memory_tool_ops.py:492-494`):
```python
except Exception as e:
    logger.error(f"coding_memory_write failed: {e}")
    return {"success": False, "path": path, "error": str(e)}
```

**Go 问题**：各错误路径（如 `ValidateCodingMemoryPath` 失败、`ParseFrontmatter` 失败、`ValidateFrontmatter` 失败）只返回错误 map 而没有日志记录。违反日志同步规则。

**修复方案**：在每个错误返回路径添加 `logger.Error` 或 `logger.Warn` 日志。

---

### G7. DeepAdapter 缺少 `agentWorkspaceDir` 字段

**Python 样例** (`interface_code.py:250`):
```python
self._agent_workspace_dir = str(get_agent_workspace_dir())
```

**Go 问题**：DeepAdapter 结构体中没有 `agentWorkspaceDir` 字段。Python 的 `_agent_workspace_dir` 用于 `create_coding_memory_rail` 和 `.agent_history` 路径修正。

**修复方案**：在 DeepAdapter 中添加 `agentWorkspaceDir` 字段，并在 CodeAdapter.CreateInstance 的步骤 10 中赋值。

---

### G8. CodeAdapter 缺少 `_agent_history` 路径修正和 `setattr` 设置 adapter mode 属性

**Python 样例** (`interface_code.py:296-312`):
```python
# agent_history 路径修正
for rail in getattr(self._instance, '_registered_rails', []):
    for tool in getattr(rail, 'tools', []) or []:
        if hasattr(tool, '_workspace_path'):
            setattr(tool, '_workspace_path', self._agent_workspace_dir)

# setattr 设置 adapter mode
setattr(self._instance, "_jiuwenswarm_adapter_mode", "code")
setattr(self._instance, "_jiuwenswarm_code_project_dir", self._project_dir or self._workspace_dir)
setattr(self._instance, "_jiuwenswarm_project_dir", self._project_dir or self._workspace_dir)
```

**Go 问题**：已有 ⤵️ 标记（`code_adapter.go:311-319`），但确认这些标记确实未实现。`_jiuwenswarm_code_project_dir` 和 `_jiuwenswarm_project_dir` 的设置在 Python 中被 `_update_runtime_config`、`_update_rails_for_mode`、`configure_team_member_agent` 等多处使用。

**修复方案**：10.6.3-10 实现时需回填这些步骤。

---

### G9. SubmitPlan 缺少 `_notify_leader_of_plan` 功能

**Python 样例** (`task_manager.py:997`):
```python
leader_message_id = await self._notify_leader_of_plan(plan_record)
```

**Go 问题**：`SubmitPlan` 方法没有通知 leader 的步骤。Python 中 leader 通过 messager 事件 + 直接消息双重通知，Go 缺少直接消息通知。

**修复方案**：在 `SubmitPlan` 中添加 `_notifyLeaderOfPlan` 调用。

---

### G10. CodeAdapter 缺少 `configure_team_member_agent` 方法

**Python 样例** (`interface_code.py:1074-1171`):
```python
def configure_team_member_agent(self, ...):
    """Code mode team member configuration."""
```

**Go 问题**：CodeAdapter 没有此方法。Team 模式下无法为 team member 配置 code 运行时 profile。

**修复方案**：10.3.7-11 实现时需补充。

---

### G11. upsertMemoryIndex 降级写入返回值缺少 note 字段

**Python 样例** (`coding_memory_tool_ops.py:486-491`):
```python
return WriteResult(
    success=True, path=resolved,
    mode=WriteMode.CREATE if not file_exists_now else WriteMode.APPEND,
    type=fm.get("type"),
    **result  # ← 冲突检测结果通过 **result 合并，包含 note
).to_dict()
```

**Go 问题** (`coding_memory_tool_ops.go:305-312`):
```go
wr := &WriteResult{...}
if conflictResult != nil {
    if cd, ok := conflictResult["conflict_detected"].(bool); ok && cd {
        wr.ConflictDetected = true
    }
    if cf, ok := conflictResult["conflicting_files"].([]string); ok {
        wr.ConflictingFiles = cf
    }
    // ← 缺少 note 字段的合并
}
```

**修复方案**：补充 note 字段的合并逻辑。

---

## 四、提示问题（共 11 个）

### T1. `bus.clear()` 将 map 设为 nil，清理后 nil map 写入会 panic

**Python 样例** (`inprocess.py:73-75`):
```python
def clear(self) -> None:
    self._topic_subs.clear()  # 保留 map 对象
    self._p2p.clear()
```

**Go 问题** (`inprocess.go:198-203`):
```go
func (b *bus) clear() {
    b.topicSubs = nil  // nil map
    b.p2p = nil
}
```

`CleanupInProcessBus()` 后，如果其他 goroutine 持有旧 bus 引用并尝试 `Subscribe`（写入 nil map），会 panic。

**修复方案**：改为 `make(map[...])` 而非 nil。

---

### T2. ValidTypes 应为不可变常量

**Python 样例** (`frontmatter.py:9`):
```python
VALID_TYPES = ("user", "feedback", "project", "reference")  # tuple 不可变
```

**Go 问题** (`frontmatter.go:11`):
```go
var ValidTypes = []string{"user", "feedback", "project", "reference"}  // slice 可变
```

**修复方案**：使用 `[...]string` 或 unexported slice + 导出函数保护。

---

### T3. ValidateFrontmatter 缺少 nil map 防御

当前调用方已保护（先检查 `fm == nil`），但作为公共函数不够健壮。应添加 `fm == nil` 的防御性检查。

---

### T4. upsertMemoryIndex 缺少 `Failed to read memory index` 的 warning 日志

Python 在读取 MEMORY.md 索引失败时记录 warning 日志，Go 静默忽略错误。

---

### T5. upsertMemoryIndex 缺少 `prepend_newline=False` 参数

Go 的 `WriteFile` 调用没有传 `WithFsPrependNewline(false)`，如果 Go 的 `WriteFile` 默认行为不同，可能导致索引文件多出空行。

---

### T6. CodingMemoryWriteWithContext 降级写入日志缺少冲突检测信息

Python 降级日志包含 `conflict_detected` 和 `conflicting_files`，Go 只记录了 "Exceeded max conflict retries"。

---

### T7. CodeAdapter `buildConfirmInterruptRail` 缺少 `tool_names` 参数

Python 传 `{"tool_names": ["switch_mode"]}`，Go 没有传。当前返回 nil（⤵️），实现时需对齐。

---

### T8. CodeAdapter `buildPermissionRail` 缺少 `llm` 和 `model_name` 参数

Python 传 `config`、`llm`、`model_name` 三个参数，Go 只传 `configBase`。当前返回 nil（⤵️），实现时需对齐。

---

### T9. CodeAdapter `buildStructuredAskUserRail` 缺少 `language` 参数

Python 传 `language=self._resolve_runtime_language()`，Go 没有传。当前返回 nil（⤵️），实现时需对齐。

---

### T10. CodeAdapter `_resolve_output_language` 缺失

Python CodeAdapter 有 `_resolve_output_language` 方法（与 `_resolve_runtime_language` 不同，用于用户输出语言），Go 没有实现。

---

### T11. CodeAdapter `codeFixedRailNames` 多了 `CodeAgentRail`

Python 的 `_FIXED_RAIL_NAMES` 不包含 `CodeAgentRail`（因为在固定列表中构建，不会出现在动态 Rails 配置中），Go 多加了。无功能影响，但与 Python 不一致。

---

## 五、⤵️ 标记验证汇总

| 位置 | 标记 | 是否确认未实现 |
|------|------|----------------|
| `coding_memory_tool_ops.go:196-198` | ⤵️ 7.8 MemUpdateChecker LLM 冗余判断 | ✅ 确认未实现，runChecker 返回空列表 |
| `coding_memory_tool_ops.go:386-387` | ⤵️ 7.8 searchSimilar 不做 LLM 判断 | ✅ 确认未实现 |
| `coding_memory_tool_ops.go:422-425` | ⤵️ 7.8 runChecker 返回空列表 | ✅ 确认未实现 |
| `coding_memory_tool_ops.go:453-454` | ⤵️ 7.8 prepareAppendMode LLM 冗余/冲突 | ✅ 确认未实现 |
| `code_adapter.go:269` | ⤵️ 10.6.1-2 system_prompt 等待 build_code_system_prompt | ✅ 确认未实现，使用 buildAgentIdentityPrompt("en") 替代 |
| `code_adapter.go:311` | ⤵️ 10.6.3-10 _jiuwenswarm_adapter_mode | ✅ 确认未实现 |
| `code_adapter.go:315` | ⤵️ 10.6.3-10 coding_memory workspace set_directory | ✅ 确认未实现 |
| `code_adapter.go:319` | ⤵️ 10.6.3-10 agent_history 写入路径修正 | ✅ 确认未实现 |
| `code_adapter.go:331` | ⤵️ 10.6.3-10 load_user_rails() | ✅ 确认未实现 |
| `code_adapter.go:454-659` | ⤵️ 10.6.3-10 7 个 Rail builder 返回 nil | ✅ 确认未实现 |
| `deep_adapter_rails.go:75-373` | ⤵️ 10.6.3-10 大量 Rail builder 返回 nil | ✅ 确认未实现 |

---

## 六、问题统计

| 严重程度 | 数量 | 问题编号 |
|---------|------|---------|
| 严重 | 7 | S1, S2, S3, S4, S5, S6, S7 |
| 一般 | 11 | G1, G2, G3, G4, G5, G6, G7, G8, G9, G10, G11 |
| 提示 | 11 | T1-T11 |

---

## 七、修复优先级建议

| 优先级 | 问题 | 理由 |
|--------|------|------|
| P0 | S3 (InProcessMessager 死锁) | 运行时死锁风险，任何 handler 回调 bus 操作都会触发 |
| P0 | S1 (CodeAdapter 语言错误) | Code 模式核心参数不一致，影响所有 Rail 行为 |
| P0 | S2 (CodeAdapter 缺少子代理) | Code 模式核心功能缺失 |
| P1 | S7 (CancelAllTasks 缺少 unblocked) | 可能导致任务永远 BLOCKED |
| P1 | S4/S5 (冲突检测误报) | 降级策略应更保守，等 7.8 实现后再恢复 |
| P1 | S6 (热重载 CodeAgentRail 丢失) | 热重载后功能降级 |
| P2 | G1-G11 | 一般问题，不影响核心流程 |
| P3 | T1-T11 | 提示问题，不影响功能 |
