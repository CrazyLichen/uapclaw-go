# 2026-08-01 审查问题修复 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复审查文档 2026-08-01-48h-logic-review.md 中 15 个确认存在的代码问题，对齐 Python 项目行为。

**Architecture:** 逐个问题修复，按文件分组合并修改，遵循 TDD 流程（先写测试验证问题存在 → 修改代码 → 测试通过 → 编译验证 → 提交）。

**Tech Stack:** Go, 对齐 Python (agent-core/openjiuwen)

---

## Task 1: S16 — CancelInflightWork 替换 ⤵️ 为实际调用

**Files:**
- Modify: `internal/swarm/server/runtime/uapclaw.go:524-535`

- [ ] **Step 1: 读取当前代码确认 ⤵️ placeholder 位置**

读取 `internal/swarm/server/runtime/uapclaw.go` L521-535，确认 CancelInflightWork 方法中 `// ⤵️ 10.3.2: adapter.AbortOnGatewayDisconnect()` 行的位置。

- [ ] **Step 2: 修改 CancelInflightWork 方法**

将 `⤵️` placeholder 替换为实际调用代码。修改 L532-535 区域：

```go
// CancelInflightWork 取消在途任务。
//
// 对齐 Python: JiuWenClaw.cancel_inflight_work()
func (uc *UapClaw) CancelInflightWork() error {
	_ = uc.sessionManager.CancelAllSessionTasks(context.Background(), "[gateway disconnect]")
	uc.adapterMu.Lock()
	a := uc.adapter
	uc.adapterMu.Unlock()
	if a == nil {
		return nil
	}
	// 对齐 Python: abort_fn = getattr(adapter, "abort_on_gateway_disconnect", None)
	if aborter, ok := a.(interface{ AbortOnGatewayDisconnect(ctx context.Context) error }); ok {
		if err := aborter.AbortOnGatewayDisconnect(context.Background()); err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "GATEWAY_DISCONNECT_ABORT_FAILED").
				Msg("adapter.AbortOnGatewayDisconnect 失败")
		}
	}
	return nil
}
```

注意：需确认 `logger` 包已在 uapclaw.go 中导入。

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功，无错误

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/runtime/uapclaw.go
git commit -m "fix(S16): CancelInflightWork 替换 ⤵️ 为实际调用 AbortOnGatewayDisconnect"
```

---

## Task 2: S04 — Cancel 增加 cancelledBy 参数

**Files:**
- Modify: `internal/common/utils/background.go:333-355` (Cancel 方法)
- Modify: `internal/common/utils/background.go:358-375` (CancelGroup)
- Modify: `internal/common/utils/background.go:377-395` (CancelAll)
- Modify: `internal/common/utils/background.go:477-499` (CascadeCancel)
- Test: `internal/common/utils/background_test.go`

- [ ] **Step 1: 修改 Cancel 方法签名增加 cancelledBy 参数**

将 `Cancel(taskID string, reason string) bool` 改为 `Cancel(taskID string, reason string, cancelledBy string) bool`：

```go
// Cancel 取消指定任务。
func (m *TaskManager) Cancel(taskID string, reason string, cancelledBy string) bool {
	m.mu.RLock()
	task, ok := m.registry[taskID]
	m.mu.RUnlock()

	if !ok {
		return false
	}

	task.mu.Lock()
	if task.Status.IsTerminal() {
		task.mu.Unlock()
		return false
	}
	task.CancelReason = reason
	if cancelledBy != "" {
		task.CancelledBy = cancelledBy
	}
	task.mu.Unlock()

	if task.cancel != nil {
		task.cancel()
	}
	return true
}
```

- [ ] **Step 2: 修改 CancelGroup 调用**

CancelGroup 中 `m.Cancel(task.ID, reason)` 改为 `m.Cancel(task.ID, reason, "")`：

```go
func (m *TaskManager) CancelGroup(group string, reason string) int {
	m.mu.RLock()
	var tasks []*Task
	for _, task := range m.registry {
		if task.Group == group {
			tasks = append(tasks, task)
		}
	}
	m.mu.RUnlock()

	count := 0
	for _, task := range tasks {
		if m.Cancel(task.ID, reason, "") {
			count++
		}
	}
	return count
}
```

- [ ] **Step 3: 修改 CancelAll 调用**

CancelAll 中 `m.Cancel(id, reason)` 改为 `m.Cancel(id, reason, "")`：

```go
func (m *TaskManager) CancelAll(reason string) int {
	m.mu.RLock()
	var taskIDs []string
	for id, task := range m.registry {
		if !task.IsTerminal() {
			taskIDs = append(taskIDs, id)
		}
	}
	m.mu.RUnlock()

	count := 0
	for _, id := range taskIDs {
		if m.Cancel(id, reason, "") {
			count++
		}
	}
	return count
}
```

- [ ] **Step 4: 修改 CascadeCancel 调用**

CascadeCancel 中父任务取消传 taskID 作为 cancelledBy，子任务取消传父任务 ID 作为 cancelledBy：

```go
func (m *TaskManager) CascadeCancel(taskID string, reason string) int {
	m.mu.RLock()
	var children []*Task
	for _, task := range m.registry {
		if task.ParentID == taskID && !task.IsTerminal() {
			children = append(children, task)
		}
	}
	m.mu.RUnlock()

	// 先取消目标任务，cancelledBy 为空（自己取消自己）
	count := 0
	if m.Cancel(taskID, reason, "") {
		count++
	}

	// 递归取消子任务，cancelledBy 为父任务 ID
	for _, child := range children {
		count += m.CascadeCancel(child.ID, "parent_cancelled", taskID)
	}

	return count
}
```

注意：CascadeCancel 的签名也需要从 `(taskID string, reason string) int` 改为 `(taskID string, reason string, cancelledBy string) int`，内部父任务 Cancel 调用传 cancelledBy，子任务 CascadeCancel 递归时传 taskID 作为 cancelledBy。

- [ ] **Step 5: 搜索项目中所有其他 Cancel 调用位置并修改**

搜索 `m.Cancel(` 或 `taskManager.Cancel(` 或 `Cancel(` 在 background.go 以外的文件中的调用，逐个添加 `cancelledBy` 参数（多数场景传空字符串 ""）。

Run: `grep -rn 'Cancel(' /home/opensource/uap-claw-go/internal --include='*.go' | grep -v '_test.go' | grep -v 'background.go'`

- [ ] **Step 6: 修改 background_test.go 中所有 Cancel 调用**

给测试中的 Cancel 调用添加第三个参数 `cancelledBy`。

- [ ] **Step 7: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 8: 运行 background 相关测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags test ./internal/common/utils/... -v -run TestCancel`
Expected: 所有 Cancel 相关测试通过

- [ ] **Step 9: 提交**

```bash
git add internal/common/utils/background.go internal/common/utils/background_test.go
git add -A  # 其他 Cancel 调用修改的文件
git commit -m "fix(S04): Cancel 方法增加 cancelledBy 参数，对齐 Python cancel(cancelled_by=)"
```

---

## Task 3: S07 — TaskManager 增加 cancelWaitTimeout 配置字段

**Files:**
- Modify: `internal/common/utils/background.go:85-90` (TaskManager 结构体)
- Modify: `internal/common/utils/background.go:556` (executeTask 中 100ms)
- Test: `internal/common/utils/background_test.go`

- [ ] **Step 1: 在 TaskManager 结构体中增加 cancelWaitTimeout 字段**

```go
type TaskManager struct {
	// registry 任务注册表
	registry map[string]*Task
	// mu 读写锁
	mu sync.RWMutex
	// cancelWaitTimeout 取消任务后等待函数完成的超时时间，默认 1s
	// 对齐 Python: BackgroundTask.cancel(timeout=1.0)
	cancelWaitTimeout time.Duration
}
```

- [ ] **Step 2: 定义默认常量**

在常量区块增加：

```go
const (
	// defaultCancelWaitTimeout 取消任务后等待函数完成的默认超时时间
	defaultCancelWaitTimeout = 1 * time.Second
)
```

- [ ] **Step 3: 修改 NewTaskManager/GetTaskManager 初始化默认值**

GetTaskManager 中设置默认值：

```go
func GetTaskManager() *TaskManager {
	return taskManagerSingleton.Get(func() *TaskManager {
		return &TaskManager{
			registry:          make(map[string]*Task),
			cancelWaitTimeout: defaultCancelWaitTimeout,
		}
	})
}
```

- [ ] **Step 4: 增加 TaskOption 支持配置 cancelWaitTimeout**

增加新的 TaskManagerOption（注意：当前 TaskOption 用于 CreateTask，需要新增 TaskManagerConfigOption 或直接在 TaskManager 上设公开方法）：

```go
// SetCancelWaitTimeout 设置取消等待超时时间。
func (m *TaskManager) SetCancelWaitTimeout(timeout time.Duration) {
	m.cancelWaitTimeout = timeout
}
```

- [ ] **Step 5: 修改 executeTask 中使用 cancelWaitTimeout**

将 `<-time.After(100 * time.Millisecond)` 替换为 `<-time.After(m.cancelWaitTimeout)`：

```go
case <-execCtx.Done():
	// Context 被取消或超时，等待任务函数完成
	select {
	case fnRes = <-resultCh:
		// 任务函数在取消后也返回了
	case <-time.After(m.cancelWaitTimeout):
		// 任务函数未及时返回，视为被取消/超时
	}
```

- [ ] **Step 6: 编译验证 + 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./... && go test -tags test ./internal/common/utils/... -v`
Expected: 编译成功，测试通过

- [ ] **Step 7: 提交**

```bash
git add internal/common/utils/background.go internal/common/utils/background_test.go
git commit -m "fix(S07): TaskManager 增加 cancelWaitTimeout 配置字段，默认 1s 替代硬编码 100ms"
```

---

## Task 4: S01 + S08 + G07 + G08 — todo.go 综合修复

这是最复杂的修改，涉及 4 个问题（S01 校验流程、S08 去重+函数名对齐、G07 TrimSpace、G08 TrimSpace），全部在同一个文件中。按步骤逐个修改。

**Files:**
- Modify: `internal/agentcore/harness/tools/todo/todo.go`
- Test: `internal/agentcore/harness/tools/todo/todo_test.go`

### 4a: G07 — todoItemFromMap TrimSpace

- [ ] **Step 1: 修改 todoItemFromMap 中所有字符串字段加 TrimSpace**

```go
func todoItemFromMap(data map[string]any) hschema.TodoItem {
	id := strings.TrimSpace(strVal(data, "id", ""))
	if id == "" {
		id = uuid.New().String()
	}
	content := strings.TrimSpace(strVal(data, "content", ""))
	activeForm := strings.TrimSpace(strVal(data, "activeForm", ""))
	description := strings.TrimSpace(strVal(data, "description", ""))
	selectedModelID := strings.TrimSpace(strVal(data, "selected_model_id", ""))
	// ...
```

注意：需要确认 `strVal` 辅助函数的位置。如果 data["id"] 的类型断言方式不同，需要调整。当前代码用 `id, _ := data["id"].(string)`，改为使用 TrimSpace 包裹。

### 4b: G08 — formatCreateResult TrimSpace

- [ ] **Step 2: 修改 formatCreateResult 返回值加 TrimSpace**

找到 `formatCreateResult` 函数末尾的 `return result`，改为：

```go
return strings.TrimSpace(result)
```

### 4c: S08 — delete_ids 入口去重 + 函数名对齐 Python

- [ ] **Step 3: 写 uniqueIDs 辅助函数**

```go
// uniqueIDs 对 ID 列表去重，保持首次出现顺序。
// 对齐 Python: delete_ids = set(ids)
func uniqueIDs(ids []string) []string {
	seen := make(map[string]struct{}, len(ids))
	result := make([]string, 0, len(ids))
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			result = append(result, id)
		}
	}
	return result
}
```

- [ ] **Step 4: 修改 todoModifyDelete 入口去重**

在 `todoModifyDelete` 入口处添加去重：

```go
func todoModifyDelete(todos []hschema.TodoItem, ids []string) ([]hschema.TodoItem, string, error) {
	ids = uniqueIDs(ids)
	// ... 后续代码不变，但 deleteSet 构建可以简化为直接遍历 ids
```

- [ ] **Step 5: 函数名对齐 Python**

重命名所有 todo modify 函数对齐 Python 命名：
- `todoModifyDelete` → `deleteTodos`
- `todoModifyUpdate` → `updateTodos`
- `todoModifyAppend` → `appendTodos`
- `todoModifyInsertAfter` → `insertAfterTodos`
- `todoModifyInsertBefore` → `insertBeforeTodos`
- `todoModifyCancel` → `cancelTodos`

注意：Go 中非导出函数用小写开头，Python 用下划线分隔。选择 `deleteTodos` 而非 `_delete_todos`（Go 风格的 camelCase 小写开头）。

同步修改所有调用这些函数的位置（在 `NewTodoModifyTool.fn` 闭包中的调用）。

### 4d: S01 — validateSingleInProgress 简化 + 先修改再校验

- [ ] **Step 6: 简化 validateSingleInProgress 函数**

将 `validateSingleInProgress(existingTodos, newInProgressIDs, removingFromInProgress)` 简化为 `validateSingleInProgress(todos []hschema.TodoItem)`：

```go
// validateSingleInProgress 校验同一时间只能有一个 in_progress 任务。
// 对齐 Python: _validate_single_in_progress(todos_data)
func validateSingleInProgress(todos []hschema.TodoItem) error {
	count := 0
	for _, item := range todos {
		if item.Status == hschema.TodoStatusInProgress {
			count++
		}
	}
	if count > 1 {
		return exception.BuildError(
			exception.StatusToolTodosValidationInvalid,
			exception.WithParam("reason", "超过一个任务被标记为 'in_progress'（仅允许一个）"),
		)
	}
	return nil
}
```

- [ ] **Step 7: 修改 updateTodos（原 todoModifyUpdate）为先修改再校验**

```go
func updateTodos(todos []hschema.TodoItem, updates []map[string]any) ([]hschema.TodoItem, string, error) {
	// 先逐个更新 todo 状态（就地修改）
	updatedCount := 0
	for _, update := range updates {
		id := strings.TrimSpace(strVal(update, "id", ""))
		if id == "" {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", "批量更新失败: 缺少必填字段 'id'"),
			)
		}
		found := false
		for i := range todos {
			if todos[i].ID == id {
				found = true
				if content, ok := update["content"].(string); ok {
					todos[i].Content = strings.TrimSpace(content)
				}
				if activeForm, ok := update["activeForm"].(string); ok {
					todos[i].ActiveForm = strings.TrimSpace(activeForm)
				}
				if description, ok := update["description"].(string); ok {
					todos[i].Description = strings.TrimSpace(description)
				}
				if status, ok := update["status"].(string); ok {
					parsed, err := hschema.ParseTodoStatus(strings.TrimSpace(status))
					if err != nil {
						return nil, "", exception.BuildError(
							exception.StatusToolTodosValidationInvalid,
							exception.WithParam("reason", fmt.Sprintf("无效的状态 '%s'（任务 '%s'）", status, id)),
						)
					}
					todos[i].Status = parsed
				}
				if selectedModelID, ok := update["selected_model_id"].(string); ok {
					todos[i].SelectedModelID = strings.TrimSpace(selectedModelID)
				}
				updatedCount++
				break
			}
		}
		if !found {
			return nil, "", exception.BuildError(
				exception.StatusToolTodosValidationInvalid,
				exception.WithParam("reason", fmt.Sprintf("批量更新失败: 任务 '%s' 不存在", id)),
			)
		}
	}
	// 修改完成后校验（对齐 Python: _validate_single_in_progress(current_todos)）
	if err := validateSingleInProgress(todos); err != nil {
		return nil, "", err
	}
	return todos, fmt.Sprintf("已成功更新 %d 个任务", updatedCount), nil
}
```

- [ ] **Step 8: 修改 appendTodos（原 todoModifyAppend）为先修改再校验**

`appendTodos` 中先构建新任务并追加到 todos，再校验：

```go
func appendTodos(todos []hschema.TodoItem, newItems []map[string]any) ([]hschema.TodoItem, string, error) {
	// ... 校验 newItems 非空 ...

	// 构建新任务并校验每个 item
	for _, raw := range newItems {
		if err := validateSingleTodoItem(raw); err != nil {
			return nil, "", err
		}
		todoItem := todoItemFromMap(raw)
		todos = append(todos, todoItem)
	}

	// 修改完成后校验 in_progress 数量
	if err := validateSingleInProgress(todos); err != nil {
		return nil, "", err
	}
	return todos, fmt.Sprintf("已成功追加 %d 个任务", len(newItems)), nil
}
```

- [ ] **Step 9: 修改 insertAfterTodos 和 insertBeforeTodos 同理**

同样改为先插入新任务再校验的模式。去掉之前"先校验 newInProgressIDs"的预计算逻辑。

- [ ] **Step 10: 删除所有旧的 validateSingleInProgress 调用参数**

搜索所有 `validateSingleInProgress(todos, inProgressIDs, ...)` 调用，改为 `validateSingleInProgress(todos)`。删除所有 `inProgressIDs` 和 `removingFromInProgress` 的预计算代码块。

- [ ] **Step 11: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 12: 运行 todo 测试**

Run: `cd /home/opensource/uap-claw-go && go test -tags test ./internal/agentcore/harness/tools/todo/... -v`
Expected: 测试通过

- [ ] **Step 13: 提交**

```bash
git add internal/agentcore/harness/tools/todo/todo.go internal/agentcore/harness/tools/todo/todo_test.go
git commit -m "fix(S01/S08/G07/G08): todo 模块综合修复 — validateSingleInProgress 简化、先修改再校验、ids 去重、TrimSpace、函数名对齐 Python"
```

---

## Task 5: S13 — MessageBus.Stop 先标记再清理

**Files:**
- Modify: `internal/agentcore/multi_agent/team_runtime/message_bus.go:184-219`
- Modify: `internal/agentcore/multi_agent/team_runtime/team_runtime.go:156-181`

- [ ] **Step 1: 修改 MessageBus.Stop 方法**

将 `mb.running.Store(false)` 移到清理之前：

```go
func (mb *MessageBus) Stop(ctx context.Context) error {
	if !mb.running.Load() {
		return nil
	}

	// 对齐 Python: 先标记 running=False，阻止新请求进入
	mb.running.Store(false)

	// 然后停用所有活跃订阅
	mb.subscriptionLock.Lock()
	for topic, sub := range mb.activeSubscriptions {
		sub.Deactivate()
		delete(mb.activeSubscriptions, topic)
	}
	mb.subscriptionLock.Unlock()

	// 停止消息队列
	if err := mb.mq.Stop(ctx); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", "MESSAGE_BUS_STOP_ERROR").
			Str("team_id", mb.teamID).
			Msg("消息总线停止失败")
		return exception.BuildError(
			exception.StatusMessageQueueInitiationError,
			exception.WithCause(err),
			exception.WithParam("type", "MessageQueueInMemory"),
			exception.WithParam("reason", fmt.Sprintf("[shutdown phase] %s", err.Error())),
		)
	}

	logger.Info(logComponent).
		Str("event_type", "MESSAGE_BUS_STOPPED").
		Str("team_id", mb.teamID).
		Msg("消息总线已停止")

	return nil
}
```

- [ ] **Step 2: 修改 TeamRuntime.Stop 方法**

将 `tr.running.Store(false)` 移到 messageBus.Stop 之前，对齐 Python L127-128:

```go
func (tr *TeamRuntime) Stop(ctx context.Context) error {
	// 幂等检查
	if !tr.running.Load() {
		return nil
	}

	// 对齐 Python: 先标记 running=False，阻止新请求进入
	tr.running.Store(false)

	// 然后停 messageBus
	if tr.messageBus != nil {
		if err := tr.messageBus.Stop(ctx); err != nil {
			logger.Error(logComponent).Err(err).
				Str("event_type", "TEAM_RUNTIME_STOP_ERROR").
				Str("team_id", tr.teamID).
				Msg("团队运行时停止失败")
			return err
		}
	}

	logger.Info(logComponent).
		Str("event_type", "TEAM_RUNTIME_STOPPED").
		Str("team_id", tr.teamID).
		Msg("团队运行时已停止")

	return nil
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/team_runtime/message_bus.go internal/agentcore/multi_agent/team_runtime/team_runtime.go
git commit -m "fix(S13): MessageBus/TeamRuntime Stop 先标记 running=false 再清理，对齐 Python"
```

---

## Task 6: S14 — deep_adapter 增加 sub_mode 条件

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go:424`

- [ ] **Step 1: 在 CreateInstance 步骤 18 之后追加条件**

在 L424 `subagentSpecs, shouldEnableGeneralAgent := ...` 之后追加：

```go
	// 对齐 Python: should_enable_general_agent = should_add_general_agent and (sub_mode == "plan" or mode.startswith("agent"))
	shouldEnableGeneralAgent = shouldEnableGeneralAgent && (d.subMode == "plan" || strings.HasPrefix(d.mode, "agent"))
```

注意：需确认 `d.subMode` 和 `d.mode` 变量名（当前代码中可能用的是局部变量 `subMode` 和 `mode`，根据 L389-390 确认）。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter.go
git commit -m "fix(S14): CreateInstance 增加 sub_mode/mode 条件过滤 shouldEnableGeneralAgent"
```

---

## Task 7: S10 — 删除 RouteBinding 死代码字段

**Files:**
- Modify: `internal/swarm/gateway/routing/route_binding.go`
- Modify: `internal/swarm/gateway/routing/route_binding_test.go`

- [ ] **Step 1: 从 RouteBinding 结构体删除 ForwardMethods 和 ForwardNoLocalHandler 字段**

修改 RouteBinding 结构体，删除这两个字段及注释：

```go
type RouteBinding struct {
	// Path WS 路径，如 "/ws"、"/acp"、"/tui"
	Path string
	// ChannelID 通道标识，如 "web"、"acp"、"tui"
	ChannelID string
	// InboundInterceptor 入站拦截器（如 ACP JSON-RPC 翻译）
	InboundInterceptor InterceptorFunc
	// OutboundInterceptor 出站拦截器
	OutboundInterceptor InterceptorFunc
	// DisconnectHandler 连接断开回调
	DisconnectHandler DisconnectFunc
	// Install 在 GatewayServer 上注册本地 handler 的钩子
	Install InstallFunc
}
```

- [ ] **Step 2: 从 NewWebRouteBinding 删除空 map 初始化**

```go
func NewWebRouteBinding() *RouteBinding {
	return &RouteBinding{
		Path:      "/ws",
		ChannelID: "web",
	}
}
```

- [ ] **Step 3: 更新 route_binding_test.go**

删除测试中对 `ForwardMethods` 和 `ForwardNoLocalHandler` 的 nil 检查。

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/gateway/routing/route_binding.go internal/swarm/gateway/routing/route_binding_test.go
git commit -m "fix(S10): 删除 RouteBinding 死代码字段 ForwardMethods/ForwardNoLocalHandler"
```

---

## Task 8: S11 — Send/Publish 错误码对齐 Python

**Files:**
- Modify: `internal/agentcore/multi_agent/team_runtime/team_runtime.go:293-360`

- [ ] **Step 1: 修改 Send 方法**

去掉 `!tr.HasAgent(sender)` 检查（311-315 行），将 `!tr.HasAgent(recipient)` 的错误码改为 `StatusAgentTeamExecutionError`：

```go
func (tr *TeamRuntime) Send(ctx context.Context, message any, recipient string, sender string, opts ...maschema.TeamOption) (any, error) {
	if err := tr.ensureStarted(ctx); err != nil {
		return nil, err
	}
	// 对齐 Python: if not sender: raise AGENT_TEAM_EXECUTION_ERROR
	if sender == "" {
		return nil, exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", "sender 不能为空"),
		)
	}
	// 对齐 Python: if not recipient: raise AGENT_TEAM_EXECUTION_ERROR
	if recipient == "" {
		return nil, exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", "recipient 不能为空"),
		)
	}
	// 对齐 Python: if recipient not in self._agent_cards: raise AGENT_TEAM_EXECUTION_ERROR
	if !tr.HasAgent(recipient) {
		return nil, exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", fmt.Sprintf("接收者 Agent %s 不存在", recipient)),
		)
	}
	// ... 后续不变
}
```

- [ ] **Step 2: 修改 Publish 方法**

去掉 `!tr.HasAgent(sender)` 检查（350-354 行）：

```go
func (tr *TeamRuntime) Publish(ctx context.Context, message any, topicID string, sender string, opts ...maschema.TeamOption) error {
	if err := tr.ensureStarted(ctx); err != nil {
		return err
	}
	// 对齐 Python: if not sender: raise AGENT_TEAM_EXECUTION_ERROR
	if sender == "" {
		return exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", "sender 不能为空"),
		)
	}
	// 对齐 Python: if not topic_id: raise AGENT_TEAM_EXECUTION_ERROR
	if topicID == "" {
		return exception.BuildError(exception.StatusAgentTeamExecutionError,
			exception.WithParam("error_msg", "topic_id 不能为空"),
		)
	}
	// ... 后续不变（去掉 HasAgent(sender) 检查）
}
```

- [ ] **Step 3: 编译验证 + 测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./... && go test -tags test ./internal/agentcore/multi_agent/team_runtime/... -v`
Expected: 编译成功，测试通过

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/team_runtime/team_runtime.go
git commit -m "fix(S11): Send/Publish 错误码对齐 Python，去掉 sender HasAgent 检查，统一用 ExecutionError"
```

---

## Task 9: S12 — HandoffSignal 增加单引号 fallback

**Files:**
- Modify: `internal/agentcore/multi_agent/teams/handoff/handoff_signal.go:194-198`

- [ ] **Step 1: 在 findHandoffFromSession 的 JSON 解析后增加 fallback**

修改 L194-198 区域，在 JSON 解析失败后增加单引号→双引号预处理尝试：

```go
		// 尝试 JSON 解析
		var parsed map[string]any
		if err := json.Unmarshal([]byte(contentStr), &parsed); err != nil {
			// 对齐 Python: ast.literal_eval fallback — 单引号 dict 如 {'__handoff_to__': 'agent1'}
			// 尝试将 Python 单引号替换为双引号后再解析
			fixed := strings.ReplaceAll(contentStr, "'", "\"")
			if err2 := json.Unmarshal([]byte(fixed), &parsed); err2 != nil {
				continue // JSON 和单引号 fallback 都失败，跳过此消息
			}
		}
		if _, ok := parsed[HandoffTargetKey]; ok {
			return parsed
		}
```

注意：需确认 `strings` 包已在 handoff_signal.go 中导入。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/teams/handoff/handoff_signal.go
git commit -m "fix(S12): findHandoffFromSession 增加 Python 单引号→双引号 fallback，对齐 ast.literal_eval"
```

---

## Task 10: G09 — isAutoConfirmed truthy 判断对齐

**Files:**
- Modify: `internal/agentcore/harness/rails/interrupt/confirm_rail.go:169-179`
- Test: `internal/agentcore/harness/rails/interrupt/confirm_rail_test.go`

- [ ] **Step 1: 修改 isAutoConfirmed 函数**

```go
// isAutoConfirmed 检查 auto_confirm 配置中指定 key 是否为 truthy。
// 对齐 Python: ConfirmInterruptRail._is_auto_confirmed(config, key)
// Python 的 config.get(key, False) 使用宽松真值判断：
// True/1/"yes"/"true"/非空字符串 都视为 truthy。
func isAutoConfirmed(config map[string]any, key string) bool {
	if config == nil {
		return false
	}
	val, ok := config[key]
	if !ok {
		return false
	}
	// 先尝试 bool 类型断言
	if b, ok := val.(bool); ok {
		return b
	}
	// 尝试数字类型：非零视为 truthy
	switch n := val.(type) {
	case int:
		return n != 0
	case int64:
		return n != 0
	case float64:
		return n != 0
	}
	// 尝试字符串类型："yes"/"true"/非空字符串视为 truthy
	if s, ok := val.(string); ok {
		s = strings.TrimSpace(strings.ToLower(s))
		return s != "" && s != "false" && s != "no" && s != "0"
	}
	return false
}
```

- [ ] **Step 2: 编译验证 + 测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./... && go test -tags test ./internal/agentcore/harness/rails/interrupt/... -v`
Expected: 编译成功，测试通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/interrupt/confirm_rail.go
git commit -m "fix(G09): isAutoConfirmed truthy 判断对齐 Python config.get(key, False)"
```

---

## Task 11: G10 — CommunicableAgent 改用 exception.BuildError

**Files:**
- Modify: `internal/agentcore/multi_agent/team_runtime/communicable_agent.go:33`

- [ ] **Step 1: 修改 errRuntimeNotBound 定义**

将 `var errRuntimeNotBound = fmt.Errorf(...)` 改为 `exception.BuildError(...)`：

```go
var errRuntimeNotBound = exception.BuildError(exception.StatusAgentTeamExecutionError,
	exception.WithParam("error_msg", "Agent not bound to a TeamRuntime. Register the agent with a TeamRuntime first."),
)
```

注意：需要导入 `exception` 包，删除 `fmt` 包的导入（如果不再使用）。

检查 fmt 包是否在其他地方仍被使用。当前文件只在 errRuntimeNotBound 和 panic 中用了 fmt，panic 不需要 fmt。如果 fmt 不再需要，删除 import。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/team_runtime/communicable_agent.go
git commit -m "fix(G10): CommunicableAgent errRuntimeNotBound 改用 exception.BuildError 对齐 Python StatusCode"
```

---

## Task 12: S03 — 补充 CreateBackgroundTask / StartBackgroundTask

**Files:**
- Modify: `internal/common/utils/background.go`

- [ ] **Step 1: 增加 CreateBackgroundTask 函数**

在 BackgroundTask 结构体之后增加：

```go
// CreateBackgroundTask 创建后台任务，优先通过 TaskManager 注册，fallback 到直接 goroutine。
// 对齐 Python: create_background_task(coro, name, group, fallback_to_asyncio=True)
func CreateBackgroundTask(ctx context.Context, fn func(ctx context.Context) error, name string, group string) (*BackgroundTask, error) {
	manager := GetTaskManager()
	if manager != nil {
		// 优先通过 TaskManager 创建
		task, err := manager.CreateTask(ctx, func(ctx context.Context) (any, error) { return nil, fn(ctx) },
			WithTaskName(name), WithTaskGroup(group))
		if err == nil {
			handle := NewBackgroundTask(name, group, fn)
			handle.managerTask = task
			return handle, nil
		}
	}
	// Fallback: 直接 goroutine
	handle := NewBackgroundTask(name, group, fn)
	handle.Start(ctx)
	return handle, nil
}
```

注意：需要在 BackgroundTask 结构体中增加 `managerTask *Task` 字段。

- [ ] **Step 2: 增加 StartBackgroundTask 函数**

```go
// StartBackgroundTask 从同步生命周期方法中创建后台任务。
// 对齐 Python: start_background_task(coro, name, group, fallback_to_asyncio=True)
// 同步版本：不等待 TaskManager 注册完成，直接 fallback 到 goroutine。
func StartBackgroundTask(fn func(ctx context.Context) error, name string, group string) *BackgroundTask {
	// 同步方法无法等待 async 的 TaskManager.CreateTask，直接 goroutine
	handle := NewBackgroundTask(name, group, fn)
	handle.Start(context.Background())
	return handle
}
```

- [ ] **Step 3: BackgroundTask 增加 managerTask 字段**

在 BackgroundTask 结构体中增加：

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
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功

- [ ] **Step 5: 提交**

```bash
git add internal/common/utils/background.go
git commit -m "fix(S03): 补充 CreateBackgroundTask/StartBackgroundTask 双路径创建函数"
```

---

## Task 13: S15 — deep_adapter 增加 ContextEngineConfig

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`
- Modify: `internal/agentcore/harness/harness_config/params.go` (CreateDeepAgentParams 结构体)

- [ ] **Step 1: 查找 CreateDeepAgentParams 结构体位置**

搜索 `CreateDeepAgentParams` 结构体定义，确认其位置和现有字段列表。

- [ ] **Step 2: 在 CreateDeepAgentParams 中增加 ContextEngineConfig 字段**

```go
type CreateDeepAgentParams struct {
	// ... 现有字段 ...
	// ContextEngineConfig 上下文引擎配置
	// 对齐 Python: context_engine_config=_deep_agent_context_engine_config(config)
	ContextEngineConfig *ceschema.ContextEngineConfig
}
```

注意：需确认 `ceschema` 包的导入路径（context engine schema 包）。

- [ ] **Step 3: 实现 deepAgentContextEngineConfig 函数**

在 deep_adapter.go 非导出函数区块增加：

```go
// deepAgentContextEngineConfig 从 react 配置构建上下文引擎配置。
// 对齐 Python: _deep_agent_context_engine_config(react_cfg)
func (d *DeepAdapter) deepAgentContextEngineConfig(config map[string]any) *ceschema.ContextEngineConfig {
	if config == nil {
		return nil
	}
	cecRaw, ok := config["context_engine_config"]
	if !ok {
		return nil
	}
	cecMap, ok := cecRaw.(map[string]any)
	if !ok {
		return nil
	}
	// 对齐 Python: 仅根据 enable_kv_cache_release 切换亲和开关
	enableKvCacheRelease := false
	if v, ok := cecMap["enable_kv_cache_release"]; ok {
		if b, ok := v.(bool); ok {
			enableKvCacheRelease = b
		}
	}
	// 构建默认 ContextEngineConfig 并覆盖 kv_cache_release 字段
	result := ceschema.DefaultContextEngineConfig()
	result.EnableKVCacheRelease = enableKvCacheRelease
	return &result
}
```

注意：需确认 `ceschema.DefaultContextEngineConfig()` 函数是否存在，如果不存在需要调整。

- [ ] **Step 4: 在 CreateInstance 组装 params 时传入 ContextEngineConfig**

在 params 结构体初始化中增加一行：

```go
params := harness_config.CreateDeepAgentParams{
	// ... 现有字段 ...
	ContextEngineConfig:      d.deepAgentContextEngineConfig(config),
}
```

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: 编译成功（可能有未导入包的问题，需要根据实际编译错误调整）

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/adapter/deep_adapter.go internal/agentcore/harness/harness_config/params.go
git commit -m "fix(S15): CreateDeepAgentParams 增加 ContextEngineConfig 字段，deepAdapter 构建并传入"
```

---

## 最终编译验证

- [ ] **Step: 全项目编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./... && go test -tags test ./internal/... -count=1`
Expected: 编译成功，核心测试通过
