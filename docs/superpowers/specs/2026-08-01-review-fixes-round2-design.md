# 2026-08-01 审查问题修复（第二轮）设计文档

> 延续第一轮设计文档（2026-08-01-review-fixes-design.md），覆盖审查文档中剩余的 5 个需要修复的问题。

---

## 1. G03 — BackgroundTask.Wait() managerTask 路径永远阻塞

### 问题

通过 `CreateBackgroundTask` 走 TaskManager 路径时，`Start()` 从未被调用 → `t.done` 永不关闭 → `Wait()` 永远阻塞。`Stop()` 同理 — `t.cancel` 为 nil，`t.done` 永不关闭。

### Python 对比

Python 的 `BackgroundTask` 有 `_ready = asyncio.Event()`：
- `set_manager_task()` 设置 manager task 并 `_ready.set()`
- `wait()` 和 `cancel()` 先 `await self._ready.wait()`，然后委托给 `_manager_task`
- `from_asyncio_task` 路径下 `_ready` 立即设置

### 修复方案：复刻 _ready Event

在 `BackgroundTask` 结构体增加 `ready chan struct{}` 信号，复刻 Python 的 `_ready Event`：

```go
type BackgroundTask struct {
    name       string
    group      string
    fn         func(ctx context.Context) error
    cancel     context.CancelFunc
    done       chan struct{}
    err        error
    mu         sync.Mutex
    managerTask *Task
    ready       chan struct{}  // ← 新增：对齐 Python _ready Event
}
```

**关键修改点**：

1. `NewBackgroundTask` 中初始化 `ready: make(chan struct{})`
2. `CreateBackgroundTask` 走 TaskManager 路径时，赋值 managerTask 后 `close(t.ready)`（同步设置，无异步延迟）
3. `StartBackgroundTask` 跳过 TaskManager，直接 goroutine 路径，`Start()` 完成后 `close(t.ready)`
4. `Start()` 方法末尾（goroutine 启动后）`close(t.ready)` — 立即就绪
5. `Wait()` 先等 `<-t.ready`，再检查 managerTask：
   - 有 managerTask → 委托 `t.managerTask.Wait()` 返回其 error
   - 无 managerTask → 等 `<-t.done` 返回自身 error
6. `Stop(timeout)` 先等 `<-t.ready`，再检查 managerTask：
   - 有 managerTask → 委托 `t.managerTask.Cancel(reason)`
   - 无 managerTask → 用自身 cancel + done

**为什么选方案 B 而非方案 A（直接委托）**：
- 方案 A（直接委托）在 StartBackgroundTask 场景下也有时序问题：goroutine 刚启动，调用方可能立即 Wait()，此时 done 还没关闭
- 方案 B 的 ready 信号统一了两种路径的入口逻辑：先等就绪信号，再决定走哪条路径
- 与 Python 的 _ready Event 设计完全对齐，语义清晰

---

## 2. G06 — insert_after/insert_before 缺少 validateSingleTodoItem 校验

### 问题

`insertAfterTodos` / `insertBeforeTodos` 内部没有对每个 item 调用 `validateSingleTodoItem`，缺失字段被 `todoItemFromMap` 用空字符串默认值兜底，而非报错拒绝。Python 在 invoke 层先调用 `_validate_todo_data_structure` 校验每个 item 的必填字段。

### Python 对比

```python
# todo.py L516-527 (invoke)
elif action == "insert_after":
    todo_data = inputs.get("todo_data")
    self._validate_todo_data_structure(todo_data)  # ← 先校验每个 item
    target_id = todo_data["target_id"]
    insert_todos = todo_data["items"]
    results["message"] = await self._insert_after_todos(...)
```

### 修复方案：在上层调用入口统一校验

在 `NewTodoModifyTool.fn` 闭包的 `insert_after` / `insert_before` 分支中，先对 `input.TodoData.Items` 逐个调用 `validateSingleTodoItem`，校验失败则直接返回错误：

```go
case "insert_after":
    if input.TodoData == nil { ... }
    // 对齐 Python: _validate_todo_data_structure(todo_data)
    for _, item := range input.TodoData.Items {
        if err := validateSingleTodoItem(item); err != nil {
            return nil, err
        }
    }
    updatedTodos, msg, err = insertAfterTodos(todos, input.TodoData.TargetID, input.TodoData.Items)

case "insert_before":
    if input.TodoData == nil { ... }
    // 对齐 Python: _validate_todo_data_structure(todo_data)
    for _, item := range input.TodoData.Items {
        if err := validateSingleTodoItem(item); err != nil {
            return nil, err
        }
    }
    updatedTodos, msg, err = insertBeforeTodos(todos, input.TodoData.TargetID, input.TodoData.Items)
```

**为什么选方案 B 而非方案 A（函数内校验）**：
- Python 的校验在 invoke 层（即 fn 闭包），不在 `_insert_after_todos` 函数内部
- 在上层统一校验与 Python 的校验位置完全对齐
- 保持 insert 函数本身职责单一（只做插入逻辑）

---

## 3. G12 — search_files 只匹配 basename 不支持递归 glob

### 问题

Go 版使用 `filepath.Match(pattern, info.Name())` 只匹配 basename，不支持 `**/*.py` 等递归 glob 模式。Python 使用 `base.rglob(pattern)` 支持完整递归 glob。

### Python 对比

```python
# fs_operation.py L1082-1088
matched_paths = list(base.rglob(pattern))  # ← 支持 **/*.py 等递归 glob
```

### 修复方案：引入第三方 glob 库

使用 `github.com/bmatcuk/doublestar/v4` 替换 `filepath.Match`。`doublestar` 支持 `**` 递归通配符，与 `filepath.Walk` 集成良好。

**修改点**：

1. 添加 `doublestar` 依赖：`go get github.com/bmatcuk/doublestar/v4`
2. `SearchFiles` 中替换匹配逻辑：

```go
// 替换 filepath.Match(pattern, info.Name())
// 改为 doublestar.Match(pattern, relativePath)
// relativePath = walkPath 相对于 resolvedPath 的路径

err = filepath.Walk(resolvedPath, func(walkPath string, info os.FileInfo, walkErr error) error {
    if walkErr != nil {
        return nil
    }
    if !info.IsDir() {
        // 计算相对路径
        relPath, err := filepath.Rel(resolvedPath, walkPath)
        if err != nil {
            return nil
        }
        // 使用 doublestar 匹配（支持 **）
        matchedPattern, err := doublestar.Match(pattern, relPath)
        if matchedPattern && err == nil {
            matched = append(matched, f.createFSItem(walkPath, info))
        }
    }
    return nil
})
```

3. exclude_patterns 同理替换为 `doublestar.Match(ep, relPath)`
4. 同时需确认 Python 的 `rglob` 行为：`rglob("*.py")` 等价于 `glob("**/*.py")`，所以 Go 版需在 pattern 前自动补 `**/`（如果 pattern 不含 `/`），对齐 Python rglob 语义

**关于 `**/` 自动补全**：
- Python `rglob("*.py")` = `glob("**/*.py")` — 自动递归
- 如果用户传 `*.py`，Go 需将其视为 `**/*.py`（递归匹配所有子目录）
- 如果用户传 `src/**/*.go`，保持原样（路径限定）
- 规则：pattern 不含 `/` → 自动加 `**/` 前缀

---

## 4. T08 — todo.go 缺少工具级异常包装日志

### 问题

Go 版工具方法直接返回错误，无外层 TOOL_CALL_ERROR 日志包装和 InvokeFailed 异常包装。

### Python 对比

```python
# todo.py L236-244 (TodoCreateTool.invoke)
except Exception as e:
    tool_logger.error("Todo create tool invocation failed",
                      event_type=LogEventType.TOOL_CALL_ERROR, exception=str(e))
    raise build_error(StatusCode.TOOL_TODOS_INVOKE_FAILED, reason=str(e)) from e
```

每个工具（Create/List/Modify）都有 try/except 包装。

### 修复方案：在工具入口 fn 闭包增加异常包装

在 `NewTodoCreateTool`、`NewTodoListTool`、`NewTodoModifyTool` 的 fn 闭包顶层增加异常捕获逻辑：

```go
func NewTodoModifyTool(...) *Tool {
    return &Tool{
        fn: func(ctx context.Context, input any) (any, error) {
            result, err := todoModifyInner(ctx, input, todoTool)
            if err != nil {
                // 对齐 Python: tool_logger.error + build_error(TOOL_TODOS_INVOKE_FAILED)
                logger.Error(logComponent).Err(err).
                    Str("event_type", "TOOL_CALL_ERROR").
                    Str("tool_name", "todo_modify").
                    Msg("Todo modify tool invocation failed")
                return nil, exception.BuildError(exception.StatusToolTodosInvokeFailed,
                    exception.WithParam("reason", err.Error()),
                )
            }
            return result, nil
        },
    }
}
```

同理对 `NewTodoCreateTool` 和 `NewTodoListTool` 增加相同包装。

注意：当前 fn 闭包已含部分逻辑（如加锁、load/save），需要将核心逻辑提取为内部函数（如 `todoModifyInner`），然后在 fn 闭包顶层做异常包装。

---

## 5. T09 — sys_operation 缺少 Python 的结构化事件日志

### 问题

Go 版只有简单的开始/结束字符串日志，缺少 Python 的结构化事件日志字段（method_name、method_params、method_result、method_exec_time_ms）。

### Python 对比

```python
# fs_operation.py / code_operation.py
event = _create_sys_operation_event(
    method_name=...,
    method_params=...,
    method_result=...,
    method_exec_time_ms=...,
)
logger.info("...", event_type=SYS_OP_START, metadata=event)
logger.info("...", event_type=SYS_OP_END, metadata=event)
logger.error("...", event_type=SYS_OP_ERROR, metadata=event)
```

### 修复方案：对齐 Python 补充结构化日志

在每个 sys_operation 方法中增加结构化事件日志：

1. 入口处记录 `SYS_OP_START`：
```go
logger.Info(logComponent).
    Str("event_type", "SYS_OP_START").
    Str("method_name", "read_file").
    Str("method_params", fmt.Sprintf("path=%s", path)).
    Msg("系统操作开始")
```

2. 成功返回记录 `SYS_OP_END`：
```go
logger.Info(logComponent).
    Str("event_type", "SYS_OP_END").
    Str("method_name", "read_file").
    Str("method_result", fmt.Sprintf("content_length=%d", len(content))).
    Float64("method_exec_time_ms", elapsedMs).
    Msg("系统操作完成")
```

3. 错误路径记录 `SYS_OP_ERROR`：
```go
logger.Error(logComponent).Err(err).
    Str("event_type", "SYS_OP_ERROR").
    Str("method_name", "read_file").
    Str("method_params", fmt.Sprintf("path=%s", path)).
    Float64("method_exec_time_ms", elapsedMs).
    Msg("系统操作失败")
```

需要在每个方法入口记录 `startTime := time.Now()`，出口计算 `elapsedMs := float64(time.Since(startTime).Milliseconds())`。

涉及的文件：
- `internal/agentcore/sys_operation/local/fs_operation.go`
- `internal/agentcore/sys_operation/local/code_operation.go`

需对齐 Python `_create_sys_operation_event` 中具体包含哪些参数字段，按方法逐一对照。

---

## 不修改的问题

| 编号 | 原因 |
|------|------|
| G01 | Go 已有 Group() 方法，对齐 Python |
| G02 | S05 已覆盖，Go 用 error 代替 panic 是惯例 |
| G04 | 两边 activeForm 默认值都是 "" |
| G05 | Go goroutine 不泄漏（resultCh 缓冲 1），只是无法强制终止 |
| G11 | Go InputMessage 类型为 map[string]any，不存在非 dict 场景 |
| T10 | ⤵️ 计划占位符，随后续章节实现 |

---

## 延后问题

| 编号 | 说明 |
|------|------|
| T02 | background.go 日志对齐，本轮跳过讨论 |
