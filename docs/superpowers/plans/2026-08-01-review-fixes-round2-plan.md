# 审查修复第二轮 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复审查文档中剩余 5 个问题：G03（BackgroundTask ready Event）、G06（insert 校验入口）、G12（search_files doublestar）、T08（todo 异常包装）、T09（sys_operation 结构化日志）。

**Architecture:** 逐个问题修复，按文件分组合并修改，遵循 TDD 流程（先写测试 → 修改代码 → 测试通过 → 编译验证 → 提交）。

**Tech Stack:** Go, doublestar/v4, 对齐 Python (agent-core/openjiuwen)

---

## Task 1: G03 — BackgroundTask 增加 ready 信号，Wait()/Stop() 委托 managerTask

**Files:**
- Modify: `internal/common/utils/background.go:26-43` (BackgroundTask 结构体)
- Modify: `internal/common/utils/background.go:206-214` (NewBackgroundTask)
- Modify: `internal/common/utils/background.go:218-234` (CreateBackgroundTask)
- Modify: `internal/common/utils/background.go:239-244` (StartBackgroundTask)
- Modify: `internal/common/utils/background.go:246-258` (Start)
- Modify: `internal/common/utils/background.go:260-275` (Stop)
- Modify: `internal/common/utils/background.go:277-283` (Wait)
- Test: `internal/common/utils/background_test.go`

- [ ] **Step 1: 在 BackgroundTask 结构体增加 ready 字段**

```go
type BackgroundTask struct {
	// name 任务名称
	name string
	// group 任务分组
	group string
	// fn 任务函数
	fn func(ctx context.Context) error
	// cancel 取消函数
	cancel context.CancelFunc
	// done 完成信号通道
	done chan struct{}
	// err 执行错误
	err error
	// mu 互斥锁
	mu sync.Mutex
	// managerTask 关联的 TaskManager Task（如果通过 TaskManager 创建）
	managerTask *Task
	// ready 就绪信号通道，对齐 Python _ready Event
	// close(t.ready) 表示任务已就绪（要么 managerTask 已设置，要么 goroutine 已启动）
	ready chan struct{}
}
```

- [ ] **Step 2: 修改 NewBackgroundTask 初始化 ready**

```go
func NewBackgroundTask(name, group string, fn func(ctx context.Context) error) *BackgroundTask {
	return &BackgroundTask{
		name:  name,
		group: group,
		fn:    fn,
		done:  make(chan struct{}),
		ready: make(chan struct{}),
	}
}
```

- [ ] **Step 3: 修改 CreateBackgroundTask — TaskManager 路径 close ready**

```go
func CreateBackgroundTask(ctx context.Context, fn func(ctx context.Context) error, name string, group string) (*BackgroundTask, error) {
	manager := GetTaskManager()
	if manager != nil {
		// 优先通过 TaskManager 创建
		task, err := manager.CreateTask(ctx, func(ctx context.Context) (any, error) { return nil, fn(ctx) },
			WithTaskName(name), WithTaskGroup(group))
		if err == nil {
			handle := NewBackgroundTask(name, group, fn)
			handle.managerTask = task
			// 对齐 Python: set_manager_task → _ready.set()
			// TaskManager 路径下 managerTask 已同步设置，立即就绪
			close(handle.ready)
			return handle, nil
		}
	}
	// Fallback: 直接 goroutine
	handle := NewBackgroundTask(name, group, fn)
	handle.Start(ctx)
	return handle, nil
}
```

- [ ] **Step 4: 修改 Start — goroutine 启动后 close ready**

```go
func (t *BackgroundTask) Start(ctx context.Context) {
	ctx, t.cancel = context.WithCancel(ctx)

	go func() {
		defer close(t.done)
		if err := t.fn(ctx); err != nil {
			t.mu.Lock()
			t.err = err
			t.mu.Unlock()
		}
	}()

	// goroutine 已启动，立即就绪
	close(t.ready)
}
```

- [ ] **Step 5: 修改 StartBackgroundTask — 直接 goroutine 路径已有 Start → close ready**

`StartBackgroundTask` 调用 `handle.Start()`，Start() 内部已经 `close(t.ready)`，所以不需要额外修改。但确认 Start 调用顺序：

```go
func StartBackgroundTask(fn func(ctx context.Context) error, name string, group string) *BackgroundTask {
	// 同步方法无法等待 async 的 TaskManager.CreateTask，直接 goroutine
	handle := NewBackgroundTask(name, group, fn)
	handle.Start(context.Background()) // Start() 内会 close(t.ready)
	return handle
}
```

无需修改，但需确认 Start() 中 close(t.ready) 在 goroutine 启动之后调用。

- [ ] **Step 6: 修改 Wait — 先等 ready 再委托或等 done**

```go
// Wait 等待后台任务完成，返回执行错误。
// 对齐 Python: BackgroundTask.wait() — 先等 _ready，再委托 _manager_task 或等 asyncio task
func (t *BackgroundTask) Wait() error {
	// 先等就绪信号，对齐 Python: await self._ready.wait()
	<-t.ready

	// 检查是否有 managerTask，对齐 Python: await self._manager_task.wait()
	t.mu.Lock()
	mgrTask := t.managerTask
	t.mu.Unlock()

	if mgrTask != nil {
		_, err := mgrTask.Wait()
		return err
	}

	// 无 managerTask，等自身 done（goroutine 路径）
	<-t.done
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.err
}
```

- [ ] **Step 7: 修改 Stop — 先等 ready 再委托或自身 cancel**

```go
// Stop 停止后台任务，等待完成或超时。
// 对齐 Python: BackgroundTask.cancel() — 先等 _ready，再委托 _manager_task.cancel 或取消 asyncio task
func (t *BackgroundTask) Stop(timeout time.Duration) error {
	// 先等就绪信号，对齐 Python: await self._ready.wait()
	select {
	case <-t.ready:
		// 就绪，继续操作
	case <-time.After(timeout):
		return fmt.Errorf("后台任务 %q 等待就绪超时，超时时间: %v", t.name, timeout)
	}

	// 检查是否有 managerTask
	t.mu.Lock()
	mgrTask := t.managerTask
	t.mu.Unlock()

	if mgrTask != nil {
		// 委托给 TaskManager 取消
		GetTaskManager().Cancel(mgrTask.ID, "background_task_stop", "")
		// 等待 managerTask 完成
		_, err := mgrTask.Wait()
		return err
	}

	// 无 managerTask，用自身 cancel + done
	if t.cancel != nil {
		t.cancel()
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-t.done:
		return t.err
	case <-timer.C:
		return fmt.Errorf("后台任务 %q 停止超时，超时时间: %v", t.name, timeout)
	}
}
```

- [ ] **Step 8: 编写测试 — CreateBackgroundTask managerTask 路径 Wait**

```go
func TestCreateBackgroundTask_ManagerTaskWait(t *testing.T) {
	mgr := GetTaskManager()

	handle, err := CreateBackgroundTask(context.Background(), func(ctx context.Context) error {
		time.Sleep(100 * time.Millisecond)
		return nil
	}, "test-mgr", "test-group")
	if err != nil {
		t.Fatalf("CreateBackgroundTask() error = %v", err)
	}

	waitErr := handle.Wait()
	if waitErr != nil {
		t.Fatalf("Wait() = %v, want nil", waitErr)
	}

	// 验证 managerTask 已设置
	handle.mu.Lock()
	mgrTask := handle.managerTask
	handle.mu.Unlock()
	if mgrTask == nil {
		t.Fatal("managerTask 应不为 nil")
	}
}
```

- [ ] **Step 9: 编写测试 — CreateBackgroundTask managerTask 路径 Stop**

```go
func TestCreateBackgroundTask_ManagerTaskStop(t *testing.T) {
	mgr := GetTaskManager()

	started := make(chan struct{})
	handle, err := CreateBackgroundTask(context.Background(), func(ctx context.Context) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, "test-mgr-stop", "test-group")
	if err != nil {
		t.Fatalf("CreateBackgroundTask() error = %v", err)
	}

	<-started

	stopErr := handle.Stop(2 * time.Second)
	// context.Canceled 是预期行为
	if stopErr != nil && !errors.Is(stopErr, context.Canceled) {
		t.Fatalf("Stop() = %v, want context.Canceled or nil", stopErr)
	}
}
```

- [ ] **Step 10: 编写测试 — StartBackgroundTask goroutine 路径 Wait**

```go
func TestStartBackgroundTask_GoroutineWait(t *testing.T) {
	handle := StartBackgroundTask(func(ctx context.Context) error {
		return nil
	}, "test-sync", "test-group")

	waitErr := handle.Wait()
	if waitErr != nil {
		t.Fatalf("Wait() = %v, want nil", waitErr)
	}
}
```

- [ ] **Step 11: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 12: 运行 background 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags test ./internal/common/utils/... -v -run "TestBackgroundTask|TestCreateBackground|TestStartBackground"`
Expected: 所有测试通过

- [ ] **Step 13: 提交**

```bash
git add internal/common/utils/background.go internal/common/utils/background_test.go
git commit -m "fix(G03): BackgroundTask 增加 ready 信号，Wait/Stop 先等 ready 再委托 managerTask，对齐 Python _ready Event"
```

---

## Task 2: G06 — insert_after/insert_before 入口增加 validateSingleTodoItem 校验

**Files:**
- Modify: `internal/agentcore/harness/tools/todo/todo.go:478-493` (NewTodoModifyTool.fn insert 分支)
- Test: `internal/agentcore/harness/tools/todo/todo_test.go`

- [ ] **Step 1: 在 insert_after 分支增加校验**

当前代码 L478-485：
```go
case "insert_after":
    if input.TodoData == nil { ... }
    updatedTodos, msg, err = insertAfterTodos(todos, input.TodoData.TargetID, input.TodoData.Items)
```

修改为：
```go
case "insert_after":
    if input.TodoData == nil {
        return nil, exception.BuildError(
            exception.StatusToolTodosValidationInvalid,
            exception.WithParam("reason", "无效的插入操作输入: 'todo_data' 必须为包含 'target_id' 和 'items' 的对象"),
        )
    }
    // 对齐 Python: _validate_todo_data_structure(todo_data) — 先校验每个 item 必填字段
    for _, item := range input.TodoData.Items {
        if err := validateSingleTodoItem(item); err != nil {
            return nil, err
        }
    }
    updatedTodos, msg, err = insertAfterTodos(todos, input.TodoData.TargetID, input.TodoData.Items)
```

- [ ] **Step 2: 在 insert_before 分支增加校验**

当前代码 L486-493：
```go
case "insert_before":
    if input.TodoData == nil { ... }
    updatedTodos, msg, err = insertBeforeTodos(todos, input.TodoData.TargetID, input.TodoData.Items)
```

修改为：
```go
case "insert_before":
    if input.TodoData == nil {
        return nil, exception.BuildError(
            exception.StatusToolTodosValidationInvalid,
            exception.WithParam("reason", "无效的插入操作输入: 'todo_data' 必须为包含 'target_id' 和 'items' 的对象"),
        )
    }
    // 对齐 Python: _validate_todo_data_structure(todo_data) — 先校验每个 item 必填字段
    for _, item := range input.TodoData.Items {
        if err := validateSingleTodoItem(item); err != nil {
            return nil, err
        }
    }
    updatedTodos, msg, err = insertBeforeTodos(todos, input.TodoData.TargetID, input.TodoData.Items)
```

- [ ] **Step 3: 编写测试 — insert_after 缺少 content 字段应报错**

```go
func TestInsertAfter_缺少必填字段报错(t *testing.T) {
    items := []map[string]any{
        {"id": "task1", "status": "pending"}, // 缺少 content、activeForm、description
    }
    todos := []hschema.TodoItem{
        {ID: "target", Content: "target task", Status: hschema.TodoStatusCompleted},
    }
    _, _, err := insertAfterTodos(todos, "target", items)
    // 此测试实际验证的是上层 fn 闭包的校验，需通过 NewTodoModifyTool 整体测试
    // 或直接测试 validateSingleTodoItem 对缺失字段的拒绝
    item := map[string]any{"id": "task1", "status": "pending"}
    err = validateSingleTodoItem(item)
    if err == nil {
        t.Fatal("validateSingleTodoItem 对缺少 content 的 item 应返回错误")
    }
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 5: 运行 todo 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags test ./internal/agentcore/harness/tools/todo/... -v`
Expected: 测试通过

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/tools/todo/todo.go internal/agentcore/harness/tools/todo/todo_test.go
git commit -m "fix(G06): insert_after/insert_before 入口增加 validateSingleTodoItem 校验，对齐 Python _validate_todo_data_structure"
```

---

## Task 3: G12 — SearchFiles 引入 doublestar 替换 filepath.Match

**Files:**
- Modify: `internal/agentcore/sys_operation/local/fs_operation.go:649-695` (SearchFiles 方法)
- Modify: `go.mod` (添加 doublestar 依赖)
- Test: `internal/agentcore/sys_operation/local/fs_operation_test.go`

- [ ] **Step 1: 添加 doublestar 依赖**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go get github.com/bmatcuk/doublestar/v4`

- [ ] **Step 2: 在 fs_operation.go 增加 doublestar 导入**

在 import 区块增加：
```go
"github.com/bmatcuk/doublestar/v4"
```

- [ ] **Step 3: 修改 SearchFiles 主匹配逻辑**

将 L656-667 的 filepath.Walk + filepath.Match 替换为 filepath.Walk + doublestar.Match：

```go
func (f *LocalFsOperation) SearchFiles(ctx context.Context, path string, pattern string, opts ...sysop.FsOption) (*result.SearchFilesResult, error) {
	resolvedPath, err := f.resolvePath(path, false)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: rglob(pattern) — 不含 "/" 的 pattern 自动加 "**/" 前缀以递归匹配
	globPattern := pattern
	if !strings.Contains(pattern, "/") {
		globPattern = "**/" + pattern
	}

	var matched []result.FileSystemItem
	err = filepath.Walk(resolvedPath, func(walkPath string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return nil
		}
		if !info.IsDir() {
			// 计算相对路径
			relPath, relErr := filepath.Rel(resolvedPath, walkPath)
			if relErr != nil {
				return nil
			}
			// 使用 doublestar 匹配（支持 ** 递归 glob）
			matchedPattern, matchErr := doublestar.Match(globPattern, relPath)
			if matchedPattern && matchErr == nil {
				matched = append(matched, f.createFSItem(walkPath, info))
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
```

注意：需确认 `strings` 包已在 fs_operation.go 中导入。

- [ ] **Step 4: 修改 exclude_patterns 逻辑**

将 L672-689 的 filepath.Match 替换为 doublestar.Match：

```go
	// 对齐 Python: exclude_set = set(); for pat in exclude_patterns: exclude_set.update(set(base.rglob(pat)))
	o := sysop.NewFsOptions(opts...)
	if len(o.ExcludePatterns) > 0 {
		filtered := matched[:0]
		for _, p := range matched {
			excluded := false
			for _, ep := range o.ExcludePatterns {
				// exclude pattern 也需要递归 glob 支持
				globEp := ep
				if !strings.Contains(ep, "/") {
					globEp = "**/" + ep
				}
				relPath, _ := filepath.Rel(resolvedPath, p.Path)
				if matchedEp, matchErr := doublestar.Match(globEp, relPath); matchedEp && matchErr == nil {
					excluded = true
					break
				}
			}
			if !excluded {
				filtered = append(filtered, p)
			}
		}
		matched = filtered
	}
```

- [ ] **Step 5: 编写测试 — SearchFiles 递归 glob**

```go
func TestSearchFiles_递归Glob(t *testing.T) {
    // 创建测试目录结构
    tmpDir := t.TempDir()
    os.MkdirAll(filepath.Join(tmpDir, "sub", "deep"), 0755)
    os.WriteFile(filepath.Join(tmpDir, "a.py"), []byte("hello"), 0644)
    os.WriteFile(filepath.Join(tmpDir, "sub", "b.py"), []byte("world"), 0644)
    os.WriteFile(filepath.Join(tmpDir, "sub", "deep", "c.py"), []byte("deep"), 0644)
    os.WriteFile(filepath.Join(tmpDir, "a.txt"), []byte("text"), 0644)

    fsOp := NewLocalFsOperation(tmpDir)

    // 测试 *.py 应匹配所有层级（对齐 Python rglob）
    result, err := fsOp.SearchFiles(context.Background(), ".", "*.py")
    if err != nil {
        t.Fatalf("SearchFiles() error = %v", err)
    }
    if result.Data.TotalMatches != 3 {
        t.Fatalf("*.py 匹配 %d 个文件，期望 3（含递归子目录）", result.Data.TotalMatches)
    }

    // 测试 **/*.py 与 *.py 行为一致（对齐 Python rglob）
    result2, err := fsOp.SearchFiles(context.Background(), ".", "**/*.py")
    if err != nil {
        t.Fatalf("SearchFiles() error = %v", err)
    }
    if result2.Data.TotalMatches != 3 {
        t.Fatalf("**/*.py 匹配 %d 个文件，期望 3", result2.Data.TotalMatches)
    }
}
```

- [ ] **Step 6: 编写测试 — SearchFiles exclude 递归 glob**

```go
func TestSearchFiles_排除模式(t *testing.T) {
    tmpDir := t.TempDir()
    os.MkdirAll(filepath.Join(tmpDir, "sub"), 0755)
    os.WriteFile(filepath.Join(tmpDir, "a.py"), []byte("hello"), 0644)
    os.WriteFile(filepath.Join(tmpDir, "b.go"), []byte("world"), 0644)
    os.WriteFile(filepath.Join(tmpDir, "sub", "c.py"), []byte("deep"), 0644)

    fsOp := NewLocalFsOperation(tmpDir)

    // 搜索所有文件但排除 *.py（对齐 Python exclude 的 rglob 行为）
    result, err := fsOp.SearchFiles(context.Background(), ".", "*",
        sysop.WithExcludePatterns([]string{"*.py"}))
    if err != nil {
        t.Fatalf("SearchFiles() error = %v", err)
    }
    if result.Data.TotalMatches != 1 {
        t.Fatalf("排除 *.py 后匹配 %d 个文件，期望 1（只有 b.go）", result.Data.TotalMatches)
    }
}
```

- [ ] **Step 7: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 8: 运行 fs_operation 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags test ./internal/agentcore/sys_operation/local/... -v -run "TestSearchFiles"`
Expected: 测试通过

- [ ] **Step 9: 提交**

```bash
git add go.mod go.sum internal/agentcore/sys_operation/local/fs_operation.go internal/agentcore/sys_operation/local/fs_operation_test.go
git commit -m "fix(G12): SearchFiles 引入 doublestar 替换 filepath.Match，支持递归 glob，对齐 Python rglob"
```

---

## Task 4: T08 — todo 工具入口 fn 闭包增加异常包装

**Files:**
- Modify: `internal/agentcore/harness/tools/todo/todo.go:249-343` (NewTodoCreateTool.fn)
- Modify: `internal/agentcore/harness/tools/todo/todo.go:347-394` (NewTodoListTool.fn)
- Modify: `internal/agentcore/harness/tools/todo/todo.go:398-442` (NewTodoGetTool.fn)
- Modify: `internal/agentcore/harness/tools/todo/todo.go:446-515` (NewTodoModifyTool.fn)

- [ ] **Step 1: 在 NewTodoCreateTool.fn 闭包增加异常包装**

当前 fn 闭包直接返回错误。在闭包最外层增加异常捕获包装：

将 fn 闭包体提取为内部函数 `todoCreateInner`，在 fn 中调用并包装错误：

```go
fn := func(ctx context.Context, input TodoCreateInput, opts ...tool.ToolOption) (map[string]any, error) {
	result, err := todoCreateInner(ctx, input, opts, todoTool)
	if err != nil {
		// 对齐 Python: tool_logger.error("Todo create tool invocation failed", event_type=TOOL_CALL_ERROR)
		logger.Error(logComponent).Err(err).
			Str("event_type", "TOOL_CALL_ERROR").
			Str("tool_name", "todo_create").
			Msg("Todo create tool invocation failed")
		return nil, exception.BuildError(exception.StatusToolTodosInvokeFailed,
			exception.WithParam("reason", err.Error()),
		)
	}
	return result, nil
}
```

注意：`todoCreateInner` 是从当前 fn 闭包体整体提取出来的非导出函数，签名需要包含 todoTool 参数。

但考虑闭包中已引用外部变量（todoTool、language、agentID），更简洁的做法是不提取内部函数，而是在 fn 闭包体的**最后一行 return 之前**增加错误检查包装：

```go
fn := func(ctx context.Context, input TodoCreateInput, opts ...tool.ToolOption) (map[string]any, error) {
	// ... 原有闭包体 ...

	// 成功返回时直接返回
	// 错误返回时需要在闭包所有可能返回 error 的地方做包装
	// 更简洁：在 fn 闭包定义后用一个 wrapper 函数包裹
```

**推荐做法**：定义一个通用的 `todoToolInvokeWrapper` 函数：

```go
// todoToolInvokeWrapper 对齐 Python invoke 的 try/except 异常包装。
// 记录 TOOL_CALL_ERROR 日志并包装为 StatusToolTodosInvokeFailed。
func todoToolInvokeWrapper(toolName string, fn func(ctx context.Context) (map[string]any, error)) func(ctx context.Context) (map[string]any, error) {
	return func(ctx context.Context) (map[string]any, error) {
		result, err := fn(ctx)
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "TOOL_CALL_ERROR").
				Str("tool_name", toolName).
				Msg(fmt.Sprintf("Todo %s tool invocation failed", toolName))
			return nil, exception.BuildError(exception.StatusToolTodosInvokeFailed,
				exception.WithParam("reason", err.Error()),
			)
		}
		return result, nil
	}
}
```

然后在每个 NewTodoXxxTool 中，用 wrapper 包裹 fn 闭包返回值：

```go
// NewTodoCreateTool
invokeFn, _ := tool.NewTool(todoToolInvokeWrapper("todo_create", fnInner), ...)
```

但这需要调整 fn 闭包的签名，因为当前 fn 的输入参数类型不同（TodoCreateInput vs TodoModifyInput 等）。

**最终做法**：在每个 fn 闭包的返回处，将所有 `return nil, err` 统一改走错误包装。具体做法是：在每个 fn 闭包体中，将所有直接返回 error 的路径改为先记录日志再包装。

但这会使得代码非常冗长。更实用的做法是：**在 fn 闭包最后统一处理**，将 fn 闭包体包装在一个 outer 函数中：

实际最简做法：在每个 fn 定义后，将 fn 传给 `tool.NewTool` 之前，用一个通用的 invoke wrapper 函数包裹。但 Tool 的 fn 签名是 `func(ctx context.Context, input any, opts ...tool.ToolOption) (any, error)`，需要适配。

**最终决定**：由于 fn 闭包参数类型各异，最实用的做法是在每个 fn 闭包体末尾，将成功返回和错误返回统一处理：

在 `NewTodoCreateTool` fn 闭包中，将最终的成功返回从：
```go
return map[string]any{"message": resultStr}, nil
```
保持不变，但在 fn 闭包外层用一个通用 wrapper 包裹。

查看 tool.NewTool 的签名，fn 参数实际是 `func(ctx context.Context, input TodoCreateInput, opts ...tool.ToolOption) (map[string]any, error)`，需要适配为 `func(ctx context.Context, input any, opts ...tool.ToolOption) (any, error)`。

实际上 `tool.NewTool` 内部已经做了类型断言适配。所以 wrapper 只需要包裹 `func(ctx context.Context) (map[string]any, error)` 这种签名。

**最终方案**：在每个 NewTodoXxxTool 的 fn 闭包定义后，创建一个 wrappedFn，在 wrappedFn 中调用原始 fn 并包装错误：

```go
// NewTodoCreateTool 中
fn := func(ctx context.Context, input TodoCreateInput, opts ...tool.ToolOption) (map[string]any, error) {
    // ... 原有逻辑 ...
}

wrappedFn := func(ctx context.Context, input any, opts ...tool.ToolOption) (any, error) {
    result, err := fn(ctx, input.(TodoCreateInput), opts...)
    if err != nil {
        logger.Error(logComponent).Err(err).
            Str("event_type", "TOOL_CALL_ERROR").
            Str("tool_name", "todo_create").
            Msg("Todo create tool invocation failed")
        return nil, exception.BuildError(exception.StatusToolTodosInvokeFailed,
            exception.WithParam("reason", err.Error()),
        )
    }
    return result, nil
}

invokeFn, _ := tool.NewTool(wrappedFn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
```

等等，需要确认 `tool.NewTool` 的 fn 签名。当前代码是：
```go
invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
```

`fn` 的签名是 `func(ctx context.Context, input TodoCreateInput, opts ...tool.ToolOption) (map[string]any, error)`。`tool.NewTool` 如何处理这个签名？

查看当前代码可以直接传 fn 给 `tool.NewTool`，说明 `tool.NewTool` 内部做了类型断言。所以 wrapper 需要在 `tool.NewTool` 之外包裹。

**最终实现**：在每个 fn 闭包末尾返回处，不对所有 error 路径逐一修改（太冗长），而是将 fn 闭包体整体包装。修改方式：

1. 将每个 fn 闭包的原有逻辑保持不变
2. 在 `tool.NewTool` 调用前，用 wrapper 函数包裹 fn
3. wrapper 函数记录 TOOL_CALL_ERROR 日志 + 包装为 StatusToolTodosInvokeFailed

由于 tool.NewTool 接受泛型 fn，wrapper 需要适配。最简单的方式是在每个 fn 闭包内部，在最外层加一个 recover-like 的错误包装：

```go
fn := func(ctx context.Context, input TodoCreateInput, opts ...tool.ToolOption) (map[string]any, error) {
    result, err := func() (map[string]any, error) {
        // ... 原有闭包体全部搬到这个内部匿名函数中 ...
    }()
    if err != nil {
        logger.Error(logComponent).Err(err).
            Str("event_type", "TOOL_CALL_ERROR").
            Str("tool_name", "todo_create").
            Msg("Todo create tool invocation failed")
        return nil, exception.BuildError(exception.StatusToolTodosInvokeFailed,
            exception.WithParam("reason", err.Error()),
        )
    }
    return result, nil
}
```

这个方式最干净：将原有闭包体搬到内部匿名函数，外层只做错误包装。

- [ ] **Step 2: 对 NewTodoCreateTool.fn 增加异常包装**

将 L252-338 的 fn 闭包体搬到内部匿名函数，外层加包装。具体修改：

```go
fn := func(ctx context.Context, input TodoCreateInput, opts ...tool.ToolOption) (map[string]any, error) {
	// 对齐 Python: TodoCreateTool.invoke 的 try/except 异常包装
	result, err := func() (map[string]any, error) {
		sessionID, err := extractSessionID(opts)
		if err != nil {
			return nil, err
		}
		// ... 原有闭包体全部内容（L254-338）...
		return map[string]any{
			"message": resultStr,
		}, nil
	}()
	if err != nil {
		// 对齐 Python: tool_logger.error(event_type=TOOL_CALL_ERROR) + build_error(TOOL_TODOS_INVOKE_FAILED)
		logger.Error(logComponent).Err(err).
			Str("event_type", "TOOL_CALL_ERROR").
			Str("tool_name", "todo_create").
			Msg("Todo create tool invocation failed")
		return nil, exception.BuildError(exception.StatusToolTodosInvokeFailed,
			exception.WithParam("reason", err.Error()),
		)
	}
	return result, nil
}
```

- [ ] **Step 3: 对 NewTodoListTool.fn 增加异常包装**

同 Step 2 方式，将 L350-389 闭包体搬到内部匿名函数，外层加包装，tool_name 为 "todo_list"。

- [ ] **Step 4: 对 NewTodoModifyTool.fn 增加异常包装**

同 Step 2 方式，将 L449-515 闭包体搬到内部匿名函数，外层加包装，tool_name 为 "todo_modify"。

注意：NewTodoGetTool 已经在内部使用了 `StatusToolTodosInvokeFailed`（L434-437），不需要额外包装。但为一致性，也可以加上 TOOL_CALL_ERROR 日志。**决定：对 NewTodoGetTool 也加包装**。

- [ ] **Step 5: 对 NewTodoGetTool.fn 增加异常包装**

同 Step 2 方式，tool_name 为 "todo_get"。

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 7: 运行 todo 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags test ./internal/agentcore/harness/tools/todo/... -v`
Expected: 测试通过

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/harness/tools/todo/todo.go
git commit -m "fix(T08): todo 工具入口 fn 闭包增加 TOOL_CALL_ERROR 异常包装，对齐 Python invoke try/except"
```

---

## Task 5: T09 — sys_operation 补充结构化事件日志

**Files:**
- Modify: `internal/agentcore/sys_operation/local/fs_operation.go` (多个方法的日志)
- Modify: `internal/agentcore/sys_operation/local/code_operation.go` (多个方法的日志)

- [ ] **Step 1: 统一现有日志格式，增加 event_type 字段**

Go 版已有部分日志（如 L93 `"开始读取文件"`、L165 `"读取文件完成"`），但缺少 `event_type=SYS_OP_START/END/ERROR`、`method_params`、`method_result` 等结构化字段。

修改方式：在每个方法的开头日志加 `event_type=SYS_OP_START` 和 `method_params`，在结尾日志加 `event_type=SYS_OP_END` 和 `method_result`，在错误路径加 `event_type=SYS_OP_ERROR`。

**ReadFile 方法**（已有 startTime L92, 开始日志 L93, 完成日志 L165）：

修改开始日志（L93）：
```go
logger.Info(fsLogComponent).
    Str("event_type", "SYS_OP_START").
    Str("method_name", methodName).
    Str("method_params", fmt.Sprintf("path=%s, mode=%s", path, o.Mode)).
    Msg("开始读取文件")
```

修改完成日志（L165）：
```go
logger.Info(fsLogComponent).
    Str("event_type", "SYS_OP_END").
    Str("method_name", methodName).
    Str("method_result", fmt.Sprintf("path=%s, content_length=%d, mode=%s", resolvedPath, len(textContent), o.Mode)).
    Float64("method_exec_time_ms", float64(time.Since(startTime).Milliseconds())).
    Msg("读取文件完成")
```

修改错误路径日志（createErrorResult L1118）：
```go
logger.Error(fsLogComponent).Str("event_type", "SYS_OP_ERROR").
    Str("method_name", methodName).
    Str("method_params", fmt.Sprintf("error_msg=%s", errMsg)).
    Float64("method_exec_time_ms", float64(time.Since(startTime).Milliseconds())).
    Msg("文件操作失败")
```

注意：createErrorResult 需要传入 startTime 参数以计算执行时间。当前签名 `createErrorResult(methodName string, errMsg string, startTime time.Time)` 已有 startTime，只需补充日志字段。

- [ ] **Step 2: 对 WriteFile 方法增加结构化日志**

找到 WriteFile 方法的开始/完成/错误日志点，增加 event_type 和 method_params/method_result 字段。

- [ ] **Step 3: 对 SearchFiles 方法增加结构化日志**

在 SearchFiles 方法开头增加 SYS_OP_START 日志（含 pattern 参数），结尾增加 SYS_OP_END 日志（含 total_matches 结果）。

- [ ] **Step 4: 对 ListFiles 方法增加结构化日志**

同 Step 3 方式。

- [ ] **Step 5: 对 code_operation.go 中的方法增加结构化日志**

检查 code_operation.go 中有哪些方法需要补充日志，按同样模式增加 SYS_OP_START/END/ERROR。

- [ ] **Step 6: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 7: 运行 sys_operation 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags test ./internal/agentcore/sys_operation/... -v`
Expected: 测试通过

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/sys_operation/local/fs_operation.go internal/agentcore/sys_operation/local/code_operation.go
git commit -m "fix(T09): sys_operation 补充 SYS_OP_START/END/ERROR 结构化事件日志，对齐 Python _create_sys_operation_event"
```

---

## 最终编译验证

- [ ] **Step: 全项目编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./... && go test -tags test ./internal/... -count=1`
Expected: 编译成功，核心测试通过
