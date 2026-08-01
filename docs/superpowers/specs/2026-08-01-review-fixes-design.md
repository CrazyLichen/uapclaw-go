---
name: 2026-08-01 审查修复设计
description: 审查文档 2026-08-01-48h-logic-review.md 中 15 个确认问题的修复方案
type: project
---

# 2026-08-01 审查问题修复设计

> 来源：docs/review/2026-08-01-48h-logic-review.md
> 验证方式：逐个对照 Python 项目 (agent-core/openjiuwen) 与 Go 当前代码确认

---

## 不修改的问题（3 个）

| 编号 | 原因 |
|------|------|
| S02 | Go 大锁周期更安全，逻辑正确即可 |
| S05 | 语言差异（Go 返回 error vs Python 抛异常），不修改 |
| S09 | **不存在** — Go json.MarshalIndent 默认保留中文字符 |

---

## 需修改的问题（15 个）

### 1. S01 — validateSingleInProgress 逻辑不一致

**文件**：`internal/agentcore/harness/tools/todo/todo.go`

**修复**：对齐 Python 流程，先修改再校验。

- `validateSingleInProgress` 简化为只接收 `todos []hschema.TodoItem`，做简单 sum 计数（去掉 `newInProgressIDs` 和 `removingFromInProgress` 参数）
- `todoModifyUpdate` 改为先就地修改 todos 状态，再调用简化版校验
- 同理适用于 `todoModifyAppend`、`todoModifyInsertAfter`、`todoModifyInsertBefore` — 先新增任务到列表，再校验最终状态
- **函数名对齐 Python**：`todoModifyDelete` → `_deleteTodos`（Go 导出风格调整）、`todoModifyUpdate` → 对齐 `_update_todos` 等

**Python 参考**：`_validate_single_in_progress(todos_data)` 只做 `sum(1 for todo if status == IN_PROGRESS) > 1`

### 2. S03 — 缺少 create_background_task / start_background_task

**文件**：`internal/common/utils/background.go`

**修复**：补充两个入口函数，实现"优先 TaskManager、fallback 到 goroutine"的双路径。

- `CreateBackgroundTask(ctx, fn, name, group)` — 检查是否有活跃 TaskManager，有则通过 manager.CreateTask 注册并设置 managerTask 字段，否则直接 goroutine fallback
- `StartBackgroundTask(fn, name, group)` — 同步版本，从同步生命周期方法中创建后台任务
- 需要增加包级 `GetTaskManager()` 函数获取当前活跃的 TaskManager（对齐 Python 的 `_get_loaded_task_group()`）

### 3. S04 — CancelledBy 设为 taskID

**文件**：`internal/common/utils/background.go`

**修复**：修改 Cancel 方法签名增加 `cancelledBy string` 参数。

```go
func (m *TaskManager) Cancel(taskID string, reason string, cancelledBy string) bool {
    // ...
    task.CancelReason = reason
    if cancelledBy != "" {
        task.CancelledBy = cancelledBy
    }
    // ...
}
```

同步修改 `CancelGroup`、`CancelAll`、`CascadeCancel` 调用处，将发起取消的任务 ID 传入 `cancelledBy`。

### 4. S07 — 100ms 硬编码等待

**文件**：`internal/common/utils/background.go`

**修复**：TaskManager 级配置字段，默认 1s。

- 增加 `cancelWaitTimeout time.Duration` 字段（默认 `1 * time.Second`）
- `NewTaskManager` 支持通过 Option 配置该字段
- `executeTask` 中 `<-time.After(100 * time.Millisecond)` 替换为 `<-time.After(task.cancelWaitTimeout)` 或从 TaskManager 获取

### 5. S08 — delete_ids 格式化不一致 + 函数名对齐

**文件**：`internal/agentcore/harness/tools/todo/todo.go`

**修复**：入口去重 ids + 函数名对齐 Python。

- `todoModifyDelete` 入口处先对 ids 去重：`ids = unique(ids)`
- 后续所有位置使用去重后的 ids，与 Python 的 `delete_ids = set(ids)` 对齐
- **函数名对齐**：`todoModifyDelete` → 对齐 Python `_delete_todos`；`todoModifyUpdate` → `_update_todos`；`todoModifyAppend` → `_append_todos`；`todoModifyInsertAfter` → `_insert_after_todos`；`todoModifyInsertBefore` → `_insert_before_todos`；`todoModifyCancel` → `_cancel_todos`

### 6. S10 — route_binding 死代码字段

**文件**：`internal/swarm/gateway/routing/route_binding.go`

**修复**：删除 RouteBinding 的死代码字段。

- 从 `RouteBinding` 结构体中删除 `ForwardMethods` 和 `ForwardNoLocalHandler` 字段
- 从 `NewWebRouteBinding()` 中删除对这两个字段的初始化
- 运行时路由使用 `web_normalize.go` 的包级变量 `web.ForwardReqMethods` 和 `web.ForwardNoLocalHandlerMethods`
- 同步更新 `route_binding_test.go` 中相关的 nil 检查测试

### 7. S11 — Send/Publish 错误码不匹配

**文件**：`internal/agentcore/multi_agent/team_runtime/team_runtime.go`

**修复**：严格对齐 Python。

- 去掉 `Send` 中的 `!tr.HasAgent(sender)` 检查（Python 没有此检查）
- 去掉 `Publish` 中的 `!tr.HasAgent(sender)` 检查
- 将 `!tr.HasAgent(recipient)` 的错误码从 `StatusAgentTeamAgentNotFound` 改为 `StatusAgentTeamExecutionError`
- Python 只检查 sender 非空、recipient 非空且已注册
- 检查 `StatusAgentTeamAgentNotFound` 在 `hierarchical_team.go` 中仍有使用，**不删除错误码定义**

### 8. S12 — HandoffSignal 缺少单引号 fallback

**文件**：`internal/agentcore/multi_agent/teams/handoff/handoff_signal.go`

**修复**：在 JSON 解析失败后增加单引号→双引号预处理 fallback。

- 复用已有的 `ParseJSON` 逻辑（`evolving/optimizer/tool_call/format.go` L66-68 的 `'` → `"` 替换）
- 在 `findHandoffFromSession` 中 JSON 解析失败后，尝试 `strings.ReplaceAll(contentStr, "'", "\"")` 再解析
- 或将单引号→双引号预处理提取为通用辅助函数放在 `common/utils` 中供多处使用

### 9. S13 — MessageBus stop 顺序

**文件**：`internal/agentcore/multi_agent/team_runtime/message_bus.go`

**修复**：对齐 Python，先标记再清理。

- 将 `mb.running.Store(false)` 移到清理订阅之前（L184 之后）
- 操作顺序变为：running.Store(false) → 停用订阅 → 停止消息队列
- 同步修改 `TeamRuntime.Stop()` 中 `tr.running.Store(false)` 的位置（移到 messageBus.Stop 之前）
- 更新相关注释

### 10. S14 — deep_adapter 缺少 sub_mode 条件

**文件**：`internal/swarm/server/adapter/deep_adapter.go`

**修复**：在 CreateInstance 步骤 18 之后追加条件过滤。

```go
// 对齐 Python: should_enable_general_agent = should_add_general_agent and (sub_mode == "plan" or mode.startswith("agent"))
shouldEnableGeneralAgent = shouldEnableGeneralAgent && (subMode == "plan" || strings.HasPrefix(mode, "agent"))
```

`subMode` 和 `mode` 变量已在 L389-390 赋值，可直接使用。

### 11. S15 — deep_adapter 缺少 ContextEngineConfig

**文件**：`internal/swarm/server/adapter/deep_adapter.go` + `params.go`

**修复**：增加字段 + 构建函数。

- 在 `CreateDeepAgentParams` 中增加 `ContextEngineConfig *ceschema.ContextEngineConfig` 字段
- 在 `deep_adapter.go` 中实现 `deepAgentContextEngineConfig(config map[string]any) *ceschema.ContextEngineConfig` 函数，对齐 Python 的 `_deep_agent_context_engine_config(react_cfg)`
- 在 CreateInstance 组装 params 时设置 `ContextEngineConfig: deepAgentContextEngineConfig(config)`

### 12. S16 — CancelInflightWork 空 stub

**文件**：`internal/swarm/server/runtime/uapclaw.go`

**修复**：替换 ⤵️ 为实际调用。

```go
// 对齐 Python: abort_fn = getattr(adapter, "abort_on_gateway_disconnect", None)
if aborter, ok := a.(interface{ AbortOnGatewayDisconnect(ctx context.Context) error }); ok {
    if err := aborter.AbortOnGatewayDisconnect(context.Background()); err != nil {
        logger.Error(logComponent).Err(err).Msg("adapter.AbortOnGatewayDisconnect 失败")
    }
}
return nil
```

### 13. G07 — todoItemFromMap 缺少 TrimSpace

**文件**：`internal/agentcore/harness/tools/todo/todo.go`

**修复**：对 id/content/activeForm/description/selectedModelID 等字段统一 TrimSpace。

```go
id := strings.TrimSpace(data["id"].(string))
content := strings.TrimSpace(data["content"].(string))
activeForm := strings.TrimSpace(data["activeForm"].(string))
// ...
```

### 14. G08 — formatCreateResult 缺少 TrimSpace

**文件**：`internal/agentcore/harness/tools/todo/todo.go`

**修复**：return 时加 strings.TrimSpace。

```go
return strings.TrimSpace(result)
```

### 15. G09 — isAutoConfirmed 真值判断

**文件**：`internal/agentcore/harness/rails/interrupt/confirm_rail.go`

**修复**：对齐 Python truthy 判断逻辑。

- 先尝试 bool 类型断言
- 不是 bool 时尝试其他 truthy 判断：非零数字、非空字符串、"yes"/"true" 等
- 对齐 Python `config.get(key, False)` 的宽松真值判断

### 16. G10 — CommunicableAgent plain error

**文件**：`internal/agentcore/multi_agent/team_runtime/communicable_agent.go`

**修复**：改用 exception.BuildError。

```go
var errRuntimeNotBound = exception.BuildError(exception.StatusAgentTeamExecutionError,
    exception.WithParam("error_msg", "Agent not bound to a TeamRuntime. Register the agent with a TeamRuntime first."))
var errAgentIDNotBound = exception.BuildError(exception.StatusAgentTeamExecutionError,
    exception.WithParam("error_msg", "Agent not bound to a TeamRuntime. Register the agent with a TeamRuntime first."))
```

---

## 涉及文件清单

| 文件 | 修改内容 |
|------|---------|
| `internal/agentcore/harness/tools/todo/todo.go` | S01, S08, G07, G08 |
| `internal/common/utils/background.go` | S03, S04, S07 |
| `internal/swarm/gateway/routing/route_binding.go` | S10 |
| `internal/agentcore/multi_agent/team_runtime/team_runtime.go` | S11 |
| `internal/agentcore/multi_agent/teams/handoff/handoff_signal.go` | S12 |
| `internal/agentcore/multi_agent/team_runtime/message_bus.go` | S13 |
| `internal/swarm/server/adapter/deep_adapter.go` | S14, S15 |
| `internal/swarm/server/runtime/uapclaw.go` | S16 |
| `internal/agentcore/harness/rails/interrupt/confirm_rail.go` | G09 |
| `internal/agentcore/multi_agent/team_runtime/communicable_agent.go` | G10 |

---

## 优先级排序

1. **S16**（Gateway 断连不中止）— 资源泄漏
2. **S04**（CancelledBy 错误）— 一行修改
3. **S01**（validateSingleInProgress 简化）— 消除预估逻辑复杂度
4. **S08 + S01 函数名对齐**（todo 函数命名）
5. **S13**（MessageBus stop 顺序）— 交换一行位置
6. **S14**（sub_mode 条件）— 一行修改
7. **S10**（删除死代码）
8. **S11**（错误码对齐）
9. **S12**（单引号 fallback）
10. **G07/G08**（TrimSpace）
11. **G09**（truthy 判断）
12. **G10**（structured error）
13. **S07**（取消等待超时配置化）
14. **S03**（create/start_background_task）
15. **S15**（ContextEngineConfig）
