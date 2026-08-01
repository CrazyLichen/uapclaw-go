# 代码逻辑审查报告（2026-08-01）

> 审查范围：近 48 小时提交记录（2026-08-01 两个 commit）以及近 10 天的完整变更
> 审查方法：逐方法对比 Python 参考项目与 Go 实现，重点检查方法签名、执行步骤、逻辑分支、待回填代码
> 涉及章节：todo 工具（9.x harness）、background/task_manager（1.8）、interrupt_base（6.8）、web_handlers/route_binding（10.x gateway）、multi_agent/team_runtime（9.x）、embedding/reranker（4.x）、sys_operation/session（5.x）、callback/utils（9.x runner）

---

## 严重问题（共 16 个）

### S01. TodoModifyTool._validate_single_in_progress 与 Go validateSingleInProgress 逻辑不一致

**严重程度**：严重 — 单 in_progress 校验逻辑差异可能导致合法操作被拒绝或非法操作被通过

**Python 样例**：
```python
# todo.py L600-606
def _validate_single_in_progress(self, todos_data: List[TodoItem]):
    in_progress_count = sum(1 for todo in todos_data if todo.status == TodoStatus.IN_PROGRESS)
    if in_progress_count > 1:
        raise build_error(
            StatusCode.TOOL_TODOS_VALIDATION_INVALID,
            reason="More than one task is marked as 'in_progress' (only one allowed)"
        )
```

**Go 问题**：
Go 版本的 `validateSingleInProgress` 接收 `newInProgressIDs` 和 `removingFromInProgress` 两个额外参数，做了复杂的「现有+新增-重叠」计算，而 Python 的 `_validate_single_in_progress` 只做简单的 `sum(1 for ... if ...status == IN_PROGRESS)` 计数。

关键差异：
- Python 在 `_update_todos` 中是**先更新 todos 状态，再校验**（L689: `self._validate_single_in_progress(current_todos)`，此时 todos 已经被就地修改了）
- Go 在 `todoModifyUpdate` 中是**先校验，再修改**（L643-645: `validateSingleInProgress` 在修改之前），这导致校验逻辑必须预估变化后的状态，增加了复杂度和出错风险

**修复方案**：
对齐 Python 的两步流程：先就地修改 todos 状态，然后对修改后的结果做简单 `sum` 校验。这样 Go 的 `validateSingleInProgress` 可以简化为 Python 的版本：

```go
func validateSingleInProgress(todos []hschema.TodoItem) error {
    count := 0
    for _, item := range todos {
        if item.Status == hschema.TodoStatusInProgress {
            count++
        }
    }
    if count > 1 {
        return exception.BuildError(...)
    }
    return nil
}
```

`todoModifyUpdate` 中改为：先逐个更新 todos 状态，再调用简化版校验。

---

### S02. TodoTool.load_todos / save_todos 锁语义与 Python 不一致

**严重程度**：严重 — Python 的锁粒度是「先加锁再 load，在锁内完成 load」，Go 版在调用方加锁而非 load_todos 内部加锁

**Python 样例**：
```python
# todo.py L108-142
async def load_todos(self, session_id: str) -> List[TodoItem]:
    async with self._lock_manager.operation(session_id):  # 锁在整个 load_todos 方法内
        file_path = self._get_file_path(session_id)
        abs_path = os.path.abspath(file_path)
        if not os.path.isfile(abs_path):
            raise build_error(...)
        read_res = await self.fs.read_file(abs_path, mode="text")
        ...
        return todos

# todo.py L144-173
async def save_todos(self, session_id: str, todos: List[TodoItem]):
    async with self._lock_manager.operation(session_id):  # 锁在整个 save_todos 方法内
        ...
```

**Go 问题**：
Go 版本的 `LoadTodos` 和 `SaveTodos` 方法本身**不带锁**，锁在调用方（如 `NewTodoListTool.fn` 中）手动加：

```go
// todo.go L357-360 — 调用方加锁
lock := todoTool.lockManager.Operation(sessionID)
lock.Lock()
defer lock.Unlock()
todos, err := todoTool.LoadTodos(ctx, sessionID)
```

但 Python 的 `load_todos` 和 `save_todos` 自带锁，意味着所有子类（如 TodoModifyTool）在调用 `self.load_todos()` 时自动获得锁保护。Go 版的锁依赖调用方正确加锁，如果某个调用路径忘记加锁，就会产生竞态条件。

此外 Python 的 `operation()` 是 async 上下文管理器，退出时自动释放。Go 的 `sync.Mutex` 需要手动 `defer lock.Unlock()`，这虽然不会遗漏，但语义不一致：Python load+save 是**各自独立的锁周期**（load_todos 获取→释放，save_todos 再获取→释放），而 Go 版所有工具方法都是**一个大锁周期覆盖 load+修改+save**。

**修复方案**：
这不是功能 bug（Go 的大锁周期实际上更安全），但需要文档说明差异。如果要严格对齐 Python，可以给 `LoadTodos`/`SaveTodos` 加锁参数或内部加锁，但 Go 版的大锁周期模式实际上是更优的设计。建议在注释中明确说明锁语义差异。

---

### S03. BackgroundTask Go 版缺少 Python 的 create_background_task 和 start_background_task 函数

**严重程度**：严重 — 缺少 Python 中核心的后台任务创建入口函数

**Python 样例**：
```python
# background_tasks.py L84-101
async def create_background_task(
    coro: Coroutine,
    *,
    name: str,
    group: str,
    fallback_to_asyncio: bool = True,
) -> BackgroundTask:
    """Create a background task via task_manager when a task group is active."""
    if _get_loaded_task_group() is not None:
        create_task = _get_loaded_create_task()
        if create_task is not None:
            task = await create_task(coro, name=name, group=group, catch_exceptions=True)
            handle = BackgroundTask(group=group)
            handle.set_manager_task(task)
            return handle
    if not fallback_to_asyncio:
        raise RuntimeError("task manager root task group is not available")
    return BackgroundTask.from_asyncio_task(asyncio.create_task(coro), group=group)

# background_tasks.py L104-131
def start_background_task(
    coro: Coroutine,
    *,
    name: str,
    group: str,
    fallback_to_asyncio: bool = True,
) -> BackgroundTask:
    """Start a background task from synchronous lifecycle methods."""
    ...
```

**Go 问题**：
Go 版的 `background.go` 有 `BackgroundTask` 结构体和 `TaskManager`，但缺少 `create_background_task` / `start_background_task` 这两个关键入口函数。Python 的核心设计是**优先走 TaskManager、fallback 到 asyncio**，Go 版没有实现这个两级 fallback 逻辑。

同时，Python 的 `BackgroundTask` 有 `from_asyncio_task` 工厂方法和 `_manager_task` 字段来桥接两种后端，Go 版只有简单的 `NewBackgroundTask` 函数。

**修复方案**：
补充 `CreateBackgroundTask` 和 `StartBackgroundTask` 函数，实现「优先 TaskManager、fallback 到 goroutine」的两级逻辑：

```go
func CreateBackgroundTask(ctx context.Context, fn func(ctx context.Context) error, name, group string) (*BackgroundTask, error) {
    // 优先通过 TaskManager 创建
    manager := GetTaskManager()
    if manager != nil {
        task, err := manager.CreateTask(ctx, func(ctx context.Context) (any, error) { return nil, fn(ctx) }, 
            WithTaskName(name), WithTaskGroup(group))
        if err == nil {
            handle := NewBackgroundTask(name, group, fn)
            handle.managerTask = task
            handle.ready.Set()
            return handle, nil
        }
    }
    // Fallback: 直接 goroutine
    handle := NewBackgroundTask(name, group, fn)
    handle.Start(ctx)
    return handle, nil
}
```

---

### S04. TaskManager.Cancel 的 CancelledBy 设置错误 — 设为 taskID 而非调用者 ID

**严重程度**：严重 — 取消操作的 cancelled_by 字段语义错误

**Python 样例**：
```python
# task.py L76-103
async def cancel(self, cascade: bool = False, reason: Optional[str] = None,
                 cancelled_by: Optional[str] = None) -> bool:
    ...
    self.cancel_reason = reason or "manual_cancel"
    if cancelled_by:
        self.cancelled_by = cancelled_by
    ...
```

Python 的 `cancel()` 方法接收 `cancelled_by` 参数，由调用者传入发起取消的 ID。

**Go 问题**：
```go
// background.go L347-349
task.CancelReason = reason
task.CancelledBy = taskID  // ← BUG: 设为自己而非调用者
```

`CancelledBy` 被设为 `taskID`（被取消任务的自身 ID），而不是发起取消操作的任务/用户 ID。Python 中 `cancelled_by` 应指向**谁发起取消**，而非被取消的对象。

**修复方案**：
修改 `Cancel` 方法签名，增加 `cancelledBy` 参数：

```go
func (m *TaskManager) Cancel(taskID string, reason string, cancelledBy string) bool {
    ...
    task.CancelReason = reason
    if cancelledBy != "" {
        task.CancelledBy = cancelledBy
    }
    ...
}
```

同步修改 `CancelGroup`、`CancelAll`、`CascadeCancel` 调用。

---

### S05. BackgroundTask.Wait() 与 Python Task.wait() 语义差异 — Python 抛异常，Go 返回 error

**严重程度**：严重 — 任务失败时 Python 会抛异常中断调用者，Go 版静默返回 error

**Python 样例**：
```python
# task.py L61-65
async def wait(self) -> Any:
    await self._done_event.wait()
    if self.exception:
        raise self.exception  # ← 抛异常！
    return self.result
```

**Go 问题**：
```go
// background.go L276-281
func (t *Task) Wait() (any, error) {
    <-t.done
    t.mu.RLock()
    defer t.mu.RUnlock()
    return t.Result, t.Err  // ← 返回 (nil, error) 而非 panic
}
```

Go 版 `Wait()` 返回 `(any, error)` 不会中断调用者。这本身是 Go 的惯用模式（无异常），但 `BackgroundTask.Wait()` 返回 `error` 而 `Task.Wait()` 返回 `(any, error)`，两个 Wait 方法签名不一致，容易混淆。

**修复方案**：
保持 Go 惯用模式（返回 error），但统一签名：
- `BackgroundTask.Wait() error` 和 `Task.Wait() (any, error)` 可以共存，但需要清晰注释说明差异
- 在注释中明确标注：Python `wait()` 抛异常 → Go `Wait()` 返回 error，对齐 Python 语义但使用 Go 错误处理惯用法

---

### S06 → 降级为一般问题

经进一步对比，Go 版 `CleanupSession` 与 Python 版逻辑一致（只清理锁），此问题降级为 G06（一般问题）。

---

### S09. todo.go json.MarshalIndent 不保留中文字符（Go 默认转义非 ASCII）

**严重程度**：严重 — 持久化文件中中文被转义为 `\uXXXX`，影响可读性且与 Python 行为不一致

**Python 样例**：
```python
# todo.py L158
json_content = json.dumps(data, ensure_ascii=False, indent=2)
# ← ensure_ascii=False: 中文字符原样保留
```

**Go 问题**：
```go
// todo.go L211
data, err := json.MarshalIndent(dicts, "", "  ")
// ← Go 默认转义非 ASCII 字符：中文 → \u5f20\u52a0 等
```

Python 保存的 `todo.json` 文件中中文直接可见，Go 保存的文件中中文变成 `\uXXXX` 转义序列。这影响文件可读性，且 LLM 读取该文件时可能解析不一致。

**修复方案**：
自定义 JSON 编码器禁用 HTML escaping：

```go
var buf bytes.Buffer
enc := json.NewEncoder(&buf)
enc.SetIndent("", "  ")
enc.SetEscapeHTML(false)  // 禁用 HTML escaping（但仍会转义非 ASCII）
// 需要额外处理：使用自定义 marshal 避免非 ASCII 转义
```

Go 的 `json.Marshal` 无法完全对齐 `ensure_ascii=False`，需要自定义 bytes 处理或使用第三方库如 `json-iterator/go`。

---

### S10. route_binding.go ForwardMethods 和 ForwardNoLocalHandler 为空 map

**严重程度**：严重 — Gateway 无法判断哪些 RPC 方法应转发到 AgentServer

**Python 样例**：
```python
# route_binding.py L172-246
_FORWARD_REQ_METHODS = frozenset({
    "initialize", "session.create", "session.switch",
    "acp.tool_response", "team.delete", "chat.send",
    "chat.interrupt", "chat.resume", "chat.user_answer",
    "history.get", "browser.start",
    "skills.*", "agents.*", "schedule.*",
    ...  # 约 50+ 方法
})

# route_binding.py L248-306
_FORWARD_NO_LOCAL_HANDLER_METHODS = frozenset({
    "chat.interrupt", "chat.resume",
    "session.switch", ...  # 约 40 方法
})
```

**Go 问题**：
```go
// route_binding.go L41-48
func NewWebRouteBinding() *WebRouteBinding {
    return &WebRouteBinding{
        ForwardMethods:          make(map[string]bool),  // ← 空 map！
        ForwardNoLocalHandler:   make(map[string]bool),  // ← 空 map！
    }
}
```

Go 版的两个路由映射完全为空，这意味着 Gateway **无法判断哪些 RPC 方法应转发到 AgentServer**。所有请求都会被当作本地处理，导致核心路由功能失效。

**修复方案**：
对齐 Python，填充 ForwardMethods 和 ForwardNoLocalHandler 映射。需从 Python 源码完整复制方法列表：

```go
func NewWebRouteBinding() *WebRouteBinding {
    rb := &WebRouteBinding{
        ForwardMethods:          make(map[string]bool, 50),
        ForwardNoLocalHandler:   make(map[string]bool, 40),
    }
    // 对齐 Python _FORWARD_REQ_METHODS
    for _, m := range []string{"initialize", "session.create", "session.switch", ...} {
        rb.ForwardMethods[m] = true
    }
    // 对齐 Python _FORWARD_NO_LOCAL_HANDLER_METHODS
    for _, m := range []string{"chat.interrupt", "chat.resume", ...} {
        rb.ForwardNoLocalHandler[m] = true
    }
    return rb
}
```

---

### S11. TeamRuntime.Send/Publish 错误码与 Python 不匹配

**严重程度**：严重 — 错误码不对齐导致调用方无法按 Python 语义处理错误

**Python 样例**：
```python
# team_runtime.py L395-408 (send)
except asyncio.TimeoutError:
    raise build_error(StatusCode.AGENT_TEAM_EXECUTION_ERROR, ...)
except Exception as e:
    raise build_error(StatusCode.AGENT_TEAM_EXECUTION_ERROR, ...)

# team_runtime.py L445-451 (publish)
except Exception as e:
    raise build_error(StatusCode.AGENT_TEAM_EXECUTION_ERROR, ...)
```

Python 在 `send()` 和 `publish()` 中统一将 bus 层错误包装为 `AGENT_TEAM_EXECUTION_ERROR`。

**Go 问题**：
Go 版的 `Send()` 和 `Publish()` 委托给 `messageBus.Send()` / `messageBus.Publish()`，它们使用 `StatusMessageQueueMessageProcessExecutionError` 和 `StatusMessageQueueTopicMessageProductionError` 等消息队列特定错误码，而不是 Python 的统一 `AGENT_TEAM_EXECUTION_ERROR`。

**修复方案**：
在 `TeamRuntime.Send()` 和 `Publish()` 中包装 bus 层错误：

```go
func (tr *TeamRuntime) Send(ctx context.Context, ...) error {
    if err := messageBus.Send(ctx, ...); err != nil {
        return exception.BuildError(exception.StatusAgentTeamExecutionError,
            exception.WithParam("reason", err.Error()))
    }
    return nil
}
```

---

### S12. HandoffSignal.findHandoffFromSession 缺少 ast.literal_eval fallback

**严重程度**：严重 — 无法解析 Python 风格的 handoff 信号（单引号 dict）

**Python 样例**：
```python
# handoff_signal.py L76-118
def _find_handoff_from_session(cls, session, tool_call_id):
    content = ...
    try:
        result = json.loads(content)        # ← 先尝试 JSON
    except json.JSONDecodeError:
        result = ast.literal_eval(content)   # ← fallback: Python 风格 dict
    ...
```

Python 支持 JSON 和 `ast.literal_eval` 两种解析方式。`ast.literal_eval` 可以解析 `{'__handoff_to__': 'agent_id'}`（单引号 Python dict），这在 Python 工具输出中很常见。

**Go 问题**：
```go
// handoff_signal.go L140-202
func findHandoffFromSession(...) {
    // 仅尝试 json.Unmarshal，无 ast.literal_eval fallback
    if err := json.Unmarshal([]byte(content), &result); err != nil {
        return nil, false  // ← JSON 解析失败直接放弃
    }
}
```

如果工具输出使用 Python 单引号 dict 格式，Go 无法解析 handoff 信号，导致 handoff 功能完全失效。

**修复方案**：
增加 Python 单引号→双引号的预处理尝试：

```go
// 尝试 JSON 解析
if err := json.Unmarshal([]byte(content), &result); err != nil {
    // Fallback: 替换 Python 单引号为双引号后再尝试
    normalized := pythonSingleQuoteToJSON(content)
    if err2 := json.Unmarshal([]byte(normalized), &result); err2 != nil {
        return nil, false
    }
}
```

需实现 `pythonSingleQuoteToJSON` 辅助函数（简单替换 `'` → `"` 但需处理转义单引号）。

---

### S13. MessageBus.stop() 与 Python 的操作顺序相反 — 可能导致关闭时竞态

**严重程度**：严重 — 关闭顺序差异可能导致消息在关闭过程中仍被接收处理

**Python 样例**：
```python
# message_bus.py L180-211 (stop)
async def stop(self):
    self._running = False  # ← 先标记为非运行状态
    # 然后停用订阅
    async with self._subscription_lock:
        for subscription in ...:
            await subscription.deactivate()
    # 最后停止消息队列
    await self._mq.stop()
```

**Go 问题**：
```go
// message_bus.go L184-218 (Stop)
func (mb *MessageBus) Stop(ctx context.Context) error {
    // 先停用订阅
    mb.subscriptionLock.Lock()
    for _, sub := range ... {
        sub.Deactivate()
    }
    mb.subscriptionLock.Unlock()
    // 然后停止消息队列
    mb.mq.Stop(ctx)
    // 最后标记为非运行状态 ← 与 Python 相反
    mb.running.Store(false)
}
```

Python 先标记 `running=False` 再清理，Go 先清理再标记。这意味着在 Go 的清理过程中，新的消息可能仍被接收和处理，因为 `running` 仍为 `true`。

**修复方案**：
对齐 Python，先标记 `running.Store(false)`，再停用订阅和停止队列：

```go
func (mb *MessageBus) Stop(ctx context.Context) error {
    mb.running.Store(false)  // ← 先标记停止
    // 然后停用订阅
    ...
}
```

---

### S14. deep_adapter.CreateInstance 缺少 should_enable_general_agent 的 sub_mode 检查

**严重程度**：严重 — 通用 Agent 在 plan/agent 模式下可能不应启用

**Python 样例**：
```python
# interface_deep.py L2575-2577
should_enable_general_agent = should_add_general_agent and (
    sub_mode == "plan" or mode.startswith("agent")
)
```

Python 在确定是否启用通用 Agent 时，除了 `should_add_general_agent` 之外还检查 `sub_mode == "plan"` 或 `mode.startswith("agent")`。

**Go 问题**：
Go 版的 `CreateInstance` 直接使用 `shouldEnableGeneralAgent`（来自 `buildConfiguredSubagents` 返回值），**缺少** `subMode == "plan"` 的额外条件。

**修复方案**：
在 `CreateInstance` 中增加 sub_mode 条件：

```go
shouldEnableGeneralAgent := shouldAddGeneralAgent &&
    (subMode == "plan" || strings.HasPrefix(mode, "agent"))
```

---

### S15. deep_adapter.CreateInstance 缺少 ContextEngineConfig 参数传递

**严重程度**：严重 — Deep Agent 创建时未传递上下文引擎配置

**Python 样例**：
```python
# interface_deep.py L2601
context_engine_config=_deep_agent_context_engine_config(config)
```

**Go 问题**：
Go 版的 `CreateDeepAgentParams` 没有 `ContextEngineConfig` 字段，Deep Agent 创建时未传递上下文引擎配置。

**修复方案**：
在 `CreateDeepAgentParams` 中增加 `ContextEngineConfig` 字段，并在 `CreateInstance` 中从 config 构建并传递。

---

### S16. uapclaw.go CancelInflightWork 不调用 AbortOnGatewayDisconnect

**严重程度**：严重 — Gateway 断连时不终止 Agent 执行，导致僵尸会话

**Python 样例**：
```python
# interface.py L1232-1238
async def cancel_inflight_work(self, session_id: str):
    ...
    await self._adapter.abort_on_gateway_disconnect()
```

**Go 问题**：
```go
// uapclaw.go L509
func (u *UapClaw) CancelInflightWork(ctx context.Context, sessionID string) error {
    // ⤵️ placeholder — 直接返回 nil，不执行 abort
    return nil
}
```

Gateway 断连时 Go 版不调用 `AbortOnGatewayDisconnect`，Agent 继续执行直到超时或完成，产生无接收方的僵尸会话。

**修复方案**：
实现 `CancelInflightWork`，调用 adapter 的 `AbortOnGatewayDisconnect`：

```go
func (u *UapClaw) CancelInflightWork(ctx context.Context, sessionID string) error {
    adapter := u.getAdapter()
    if adapter != nil {
        return adapter.AbortOnGatewayDisconnect(ctx)
    }
    return nil
}
```

---

### S09-S16 为多模块深度审查新增的严重问题

以上 S09-S16 来自对 web_handlers、route_binding、deep_adapter、uapclaw、team_runtime、handoff_signal、message_bus 等模块的逐方法对比。

---

### S07. Task.executeTask 中 context 取消后的 100ms 等待硬编码不合理

**严重程度**：严重 — 取消后硬等待 100ms 不可配置，与 Python 的 anyio.move_on_after(timeout) 模式不一致

**Python 样例**：
```python
# background_tasks.py L65-81
async def cancel(self, *, reason: str = "background_task_cancelled", timeout: float = 1.0) -> None:
    ...
    await self._manager_task.cancel(reason=reason)
    with anyio.move_on_after(timeout):  # ← 可配置超时
        await self._manager_task.wait()
    ...
```

Python 的取消等待超时是可配置的（默认 1.0s）。

**Go 问题**：
```go
// background.go L553-558
case <-execCtx.Done():
    select {
    case fnRes = <-resultCh:
        // 任务函数在取消后也返回了
    case <-time.After(100 * time.Millisecond):  // ← 硬编码 100ms
        // 任务函数未及时返回，视为被取消/超时
    }
```

`100ms` 是硬编码的，无法配置，且远小于 Python 的默认 1s。在 LLM 调用等慢场景中，100ms 可能不够任务函数清理资源。

**修复方案**：
将 100ms 替换为可配置的取消等待超时：

```go
// 在 taskConfig 中增加字段
cancelWaitTimeout time.Duration // 默认 1s

const defaultCancelWaitTimeout = 1 * time.Second

// 在 executeTask 中使用
case <-time.After(task.cancelWaitTimeout):
```

---

### S08. TodoModifyTool 的 _delete_todos Python 返回值与 Go 不一致 — Python 用 delete_ids 集合格式化消息但 Go 用原始 ids

**严重程度**：严重 — 删除消息中的 ID 列表来源不同，可能显示重复 ID

**Python 样例**：
```python
# todo.py L635-647
async def _delete_todos(self, session_id, ids, current_todos):
    deleted_count = 0
    remaining_todos = []
    delete_ids = set(ids)  # ← 去重
    for todo in current_todos:
        if todo.id in delete_ids:
            deleted_count += 1
        else:
            remaining_todos.append(todo)
    ...
    return f"Successfully deleted {deleted_count} task(s) (IDs: {', '.join(delete_ids)})"
    # ← 注意：用 delete_ids(set) 格式化，而非原始 ids
```

**Go 问题**：
```go
// todo.go L718-723
if deletedCount == 0 {
    return result, fmt.Sprintf("未删除任何任务: 提供的 ID (%s) 均未找到", strings.Join(ids, ", ")), nil
}
return result, fmt.Sprintf("已成功删除 %d 个任务 (ID: %s)", deletedCount, strings.Join(ids, ", ")), nil
// ← 注意：用 ids（原始输入）格式化，而非 deleteSet
```

Go 版用原始 `ids` 而不是 `deleteSet` 来格式化消息。如果用户传入重复 ID（如 `["a", "a", "b"]`），Python 返回 `"IDs: a, b"`（去重后），Go 返回 `"ID: a, a, b"`（不去重）。同时 `deletedCount` 在 Python 中如果传入 `[a, a, b]` 且 todos 中有 a 和 b，`deleted_count` 只为 2（因为 a 匹配一次），而 Go 的 `deletedCount` 也是 2（`deleteSet` 去重后只有 a 和 b）。

**修复方案**：
格式化消息中使用 `deleteSet` 替代 `ids`：

```go
return result, fmt.Sprintf("已成功删除 %d 个任务 (ID: %s)", deletedCount, joinSet(deleteSet)), nil
```

需要写一个辅助函数 `joinSet` 将 `map[string]struct{}` 格式化为逗号分隔字符串。

---

## 一般问题（共 12 个）

### G01. BackgroundTask 缺少 Python 的 group 属性暴露方法

Go 版已通过 `Group()` 方法暴露，对齐。无实际 bug。降级为提示级。

---

### G02. TaskManager 缺少 Python 的 Task.wait() 中 self.exception 抛异常模式

已在 S05 中覆盖。此处降级为一般问题，因为这是 Go/Python 语言风格差异而非功能缺失。

---

### G03. BackgroundTask 缺少 Python 的 _ready Event 机制

如果 Go 版需要 bridge 两种后端（TaskManager 和 goroutine），需要增加 `_ready` 信号。当前不需要。

---

### G04. TodoModifyTool._convert_to_todo_item 与 Go todoItemFromMap 的 activeForm 默认值不一致

两边逻辑一致（缺失字段默认空字符串），无实际 bug。

---

### G05. TaskManager.executeTask 中 goroutine 内执行 fn 但可能泄漏 goroutine

Go/Python 语言层面限制。建议要求所有传入 `fn` 必须尊重 context 取消。

---

### G06. TodoModifyTool insert_after/insert_before 校验时机差异

Go 版在分支内校验 nil，后续校验在独立函数中完成。逻辑等效但分布不同。

---

### G07. todoItemFromMap 缺少 .strip() — Python 对 ID 去除前后空白

**Python 样例**：
```python
# todo.py L626-627
id=str(todo_data.get("id") or "").strip() or str(_uuid.uuid4())
```

**Go 问题**：
```go
// todo.go L1052
id, _ := data["id"].(string)  // ← 无 .strip() 前后空白去除
```

Go 版保留 ID 的前后空白，Python 会去除。如果 LLM 传入带空白的 ID，两边行为不一致。

**修复方案**：
```go
id := strings.TrimSpace(data["id"].(string))
```

---

### G08. formatCreateResult 缺少 .strip() — Python 对最终结果字符串去前后空白

**Python 样例**：
```python
# todo.py L266
return result.strip()
```

**Go 问题**：Go 版的 `formatCreateResult` 不对结果做 `.strip()`，保留末尾 `\n`。

**修复方案**：
```go
return strings.TrimSpace(result)
```

---

### G09. ConfirmRail.isAutoConfirmed 真值判断严格度差异

**Python 样例**：
```python
# confirm_rail.py
config.get(key, False)  # ← 任何 truthy 值（如 "yes"）都会 auto-confirm
```

**Go 问题**：
```go
val, ok := config[key]
b, ok2 := val.(bool)
return ok && ok2 && b  // ← 只接受 bool true，"yes" 不通过
```

如果配置中 `"write_file": "yes"` 被传入，Python 自动确认，Go 不确认。实践中配置值应为 bool，但差异可能导致行为不一致。

---

### G10. CommunicableAgent 返回 plain fmt.Errorf 而非 structured exception

**Python 样例**：
```python
# communicable_agent.py L84-103
raise build_error(StatusCode.AGENT_TEAM_EXECUTION_ERROR, ...)
```

**Go 问题**：
```go
errRuntimeNotBound = fmt.Errorf("runtime 未绑定")  // ← plain error, 无 StatusCode
```

Python 用 `build_error` 带状态码，Go 用 `fmt.Errorf`。调用方无法按错误码分类处理。

**修复方案**：改用 `exception.BuildError(exception.StatusAgentTeamExecutionError, ...)`。

---

### G11. ContainerAgent.buildAgentInput 缺少非 dict 输入的 {"query": msg} fallback

**Python 样例**：
```python
# container_agent.py L56-62
if not isinstance(msg, dict):
    agent_input = {"query": msg, "handoff_history": inputs.history}
```

**Go 问题**：Go 版只处理 `map[string]any`，非 dict 输入不做 `{"query": msg}` 包装。

---

### G12. fs_operation.go search_files 模式匹配只匹配文件名而非递归 glob

**Python 样例**：
```python
# fs_operation.py L1082-1088
base.rglob(pattern)  # ← 支持递归 glob 如 **/*.py
```

**Go 问题**：
```go
filepath.Match(pattern, info.Name())  // ← 只匹配 basename，不支持 **/*.py
```

Go 的 `search_files` 无法匹配递归 glob 模式（如 `**/*.py`），只匹配单层文件名。

---

## 提示问题（共 10 个）

### T01. todo.go 错误消息从英文改为中文但格式化字符串与 Python 不对齐

**严重程度**：提示 — 消息内容语义对齐但语言不同

本次 48h commit 的大量改动是将 todo.go 的英文错误消息改为中文。如：
- `"Successfully created %d task(s)"` → `"已成功创建 %d 个任务"`
- `"Next step: Immediately execute task '%s'"` → `"下一步: 立即执行任务 '%s'"`

项目规范要求注释用中文，但错误消息面向 LLM 消费者，Python 用英文。根据反馈记忆（提示词一比一复刻 Python），错误消息应保持英文原文。但这些是面向 LLM 的结果字符串而非提示词，中文翻译是合理的。

**修复方案**：保持中文消息，这是面向用户/LLM 的本地化输出，不属于「提示词一比一复刻」范畴。

---

### T02. background.go 缺少 Python 的日志对齐（runner_logger）

**Python 样例**：
```python
# background_tasks.py 无日志（轻量级）
# task.py L224-230
logger.error(
    "Task failed",
    event_type=LogEventType.CORO_MANAGER_TASK_STATUS_CHANGED,
    exception=e,
    metadata={"task_id": self.task_id, ...},
)
```

**Go 问题**：Go 版的 `background.go` 没有任何 logger 调用。对齐项目日志规则，应在 TaskManager 关键操作点补充日志。

**修复方案**：补充日志：
- `CreateTask`: Info 日志记录 task 创建
- `Cancel`: Warn 日志记录 task 取消
- `executeTask` 失败分支: Error 日志记录 task 失败
- `RemoveCompleted`: Info 日志记录清理

---

### T03. interrupt_base.go 补充注释但逻辑无变化

48h commit 给 `interrupt_base.go` 添加了大量注释（如 `// InterruptDecision 中断决策接口`、`// BaseInterruptRail 中断拦截基础 Rail` 等），但逻辑本身无变化。注释对齐是正确行为。

---

### T04. read_file.go 错误消息改为中文

同 T01，`read_file.go` 中的英文错误消息改为中文：
- `"failed to read file"` → `"读取文件失败"`
- `"failed to parse notebook JSON"` → `"解析笔记本 JSON 失败"`

属于本地化行为，无需改回。

---

### T05. web_handlers.go 的 EventSender 类型声明位置移动

48h commit 将 `EventSender` 从枚举区块之前移到枚举区块之后，对齐源码声明排列顺序规范（规范 2）。纯格式调整，无逻辑变化。

---

### T06. singleton.go 的格式化调整

48h commit 给 `singleton.go` 添加了注释和格式化。纯格式调整，无逻辑变化。

---

### T07. 待回填代码统计 — 770 个 ⤵️/待实现标记

当前 Go 源码中有 770 个 `⤵️`/`TODO.*回填`/`placeholder`/`待回填`/`待实现` 标记。其中关键的未实现功能包括：

| 位置 | 数量 | 关键缺失 |
|------|------|---------|
| swarm/gateway/ | ~15 | IM Pipeline、Gateway Hook、SessionMap、ACP |
| swarm/server/ | ~15 | AgentManager ACP、trigger hooks |
| agentcore/harness/ | ~25 | SecurityRail/SkillUseRail/CodingMemoryRail/PermissionInterruptRail |
| agentcore/harness/harness_config/ | ~10 | 内置工具集实例化、包级/entry_point 工具加载 |
| swarm/server/runtime/skill/skilldev/ | ~5 | create_stage_agent 尚未接入 |

这些是计划中的 ⤵️ 回填点，不属于 48h 内的逻辑 bug，但需后续关注。

---

### T08. todo.go 缺少 Python 工具级异常包装日志

Python 在每个工具的 invoke 方法中有 `try/except` 块，捕获所有异常后用 `tool_logger.error(event_type=LogEventType.TOOL_CALL_ERROR)` 记录。Go 版无等效的外层异常处理和日志。

**Python 样例**：
```python
# todo.py L236-244 (TodoCreateTool.invoke)
except Exception as e:
    tool_logger.error("Todo create tool invocation failed",
                      event_type=LogEventType.TOOL_CALL_ERROR, exception=str(e))
    raise build_error(StatusCode.TOOL_TODOS_INVOKE_FAILED, reason=str(e)) from e
```

**Go 问题**：Go 版直接返回错误，无外层包装和 TOOL_CALL_ERROR 日志。

---

### T09. sys_operation 多模块缺少 Python 的结构化事件日志

Python 的 `code_operation.py` 和 `fs_operation.py` 使用 `_create_sys_operation_event` 记录 `SYS_OP_START`/`SYS_OP_END`/`SYS_OP_ERROR` 事件，包含 `method_name`、`method_params`、`method_result`、`method_exec_time_ms` 等结构化字段。

Go 版只有简单的开始/结束字符串日志，缺少方法参数、结果详情、执行时间、事件类型分类。

---

### T10. web_handlers.go 多个 handler 为 stub

locale.get_conf/set_conf、heartbeat.get_conf/set_conf、updater.*、channel.*、memory.compute 等大量 handler 使用 stub 返回硬编码值。这些是 ⤵️ 占位，不属于逻辑 bug 但影响功能完整性。

---

## 实现计划文档中的过期 ⤵️ 标记

以下 ⤵️ 标记引用的回填目标已经 ✅ 完成，但标记文本未更新：

1. ⤵️ 5.12 Config 回填（出现在 5.2/5.3/5.4/5.5/5.8）— 5.12 已 ✅
2. ⤵️ 6.10 回填（出现在 6.8）— 6.10 已 ✅
3. ⤵️ 6.23 回填（出现在 6.19）— 6.23 已 ✅
4. ⤵️ 6.4-6.10 回填（出现在 5.30）— 6.4-6.10 已 ✅

建议检查这些回填是否真的已实现，然后更新 IMPLEMENTATION_PLAN.md 中的 ⤵️ → ✅。

---

## 流程示例：validateSingleInProgress 的正确流程

以 `todo_modify update` 操作为例，展示 Python 和 Go 的执行流程差异：

### Python 流程（todo.py L662-691）：
```
1. 接收 todos_data 和 current_todos
2. 建立 todo_map = {id: todo}
3. 逐个更新 todo 字段（就地修改 current_todos）
4. 调用 _validate_single_in_progress(current_todos)  ← 此时 todos 已被修改
5. 调用 save_todos(session_id, current_todos)         ← 保存修改后的结果
6. 返回成功消息
```

### Go 当前流程（todo.go L616-694）：
```
1. 接收 updates 和 todos
2. 预计算 inProgressIDs 和 removingFromInProgress       ← 预估变化
3. 调用 validateSingleInProgress(todos, inProgressIDs, removingFromInProgress) ← 校验预估结果
4. 逐个更新 todo 字段（就地修改 todos）
5. 返回 todos 和成功消息
6. 调用方保存 todoTool.SaveTodos(ctx, sessionID, updatedTodos) ← 在 NewTodoModifyTool.fn 中
```

### 修复后的 Go 流程（对齐 Python）：
```
1. 接收 updates 和 todos
2. 逐个更新 todo 字段（就地修改 todos）
3. 调用 validateSingleInProgress(todos) ← 校验修改后的结果（简化版）
4. 如果校验失败，恢复原始状态或直接返回错误
5. 返回修改后的 todos 和成功消息
6. 调用方保存
```

---

## 总结

| 分类 | 数量 | 说明 |
|------|------|------|
| 严重问题 | 14（S06降级） | todo validateSingleInProgress、锁语义、BackgroundTask 缺函数、CancelledBy 错误、cancel 超时硬编码、delete_ids 格式化、json 中文转义、route_binding 空 map、错误码不匹配、ast.literal_eval 缺失、MessageBus stop 顺序、general_agent 条件缺、ContextEngineConfig 缺、CancelInflightWork 空 stub |
| 一般问题 | 12 | 校验时机、goroutine 泄漏、字段默认值、todoItem strip、formatCreateResult strip、isAutoConfirmed 严格度、CommunicableAgent error type、buildAgentInput fallback、search_files glob、BackgroundTask _ready、BackgroundTask Wait 签名 |
| 提示问题 | 10 | 中文消息、注释补充、格式调整、日志缺失、770 ⤵️ 标记、todo 异常包装日志、sys_op 结构化日志、web_handlers stubs |
| 过期标记 | 4 | IMPLEMENTATION_PLAN.md 中 ⤵️ 标记应更新 |

**优先修复建议**：
1. **S10**（route_binding ForwardMethods 空 map）— 最紧急，直接影响 Gateway 路由功能
2. **S16**（CancelInflightWork 不执行 abort）— Gateway 断连不终止 Agent
3. **S04**（CancelledBy 设置错误）— 一行修改
4. **S01**（validateSingleInProgress 简化）— 消除预估逻辑复杂度
5. **S09**（json 中文转义）— 对齐 Python ensure_ascii=False
6. **S12**（ast.literal_eval fallback）— handoff 信号解析
7. **S13**（MessageBus stop 顺序）— 交换 running.Store(false) 和 cleanup 顺序
8. **S08**（delete_ids 格式化）— 一行修改
9. **S07**（取消等待超时配置化）— 替换硬编码 100ms
10. **S03**（补充 create_background_task / start_background_task）— 需要新函数

**关键模块待回填概览**：

| 模块 | ⤵️ 占位数 | 影响等级 | 核心缺失 |
|------|---------|---------|---------|
| deep_adapter (rails) | ~15 | 阻塞 | 所有 Swarm Rail 未实例化 |
| uapclaw (session/slash) | ~10 | 阻塞 | InteractiveInput、slash command、team mode |
| gateway (IM/cron) | ~15 | 重要 | IM Pipeline、SessionMap、ACP |
| harness (rails/tools) | ~25 | 重要 | SecurityRail、SkillUseRail、CodingMemoryRail |
| skilldev (agent接入) | ~5 | 一般 | create_stage_agent 未接入 openjiuwen |
