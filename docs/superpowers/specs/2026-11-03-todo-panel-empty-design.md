# 右侧任务栏始终「暂无任务」修复设计

- 日期：2026-11-03
- 现象来源：界面实测，模型调用 todo 相关工具后，右侧任务栏始终显示「暂无任务」，任务项从未出现过
- 影响面：Web 前端任务栏（agent / code / team 三种模式共用同一条事件链）

## 1. 根因

`todo.updated` 事件每轮都正常发出，但 payload 里的 `todos` 列表被消费端静默吞成空数组。

| # | 环节 | 位置 | 行为 |
|---|---|---|---|
| 1 | 模型调用 `todo_create` / `todo_modify` / `todo_list` / `todo_get` | — | 工具把待办写入 `{workspace}/todo/{session_id}/todo.json` |
| 2 | Rail 钩子命中工具名白名单 | `rails/stream_event_rail.go:391,422` + `stream_event_helpers.go:31` | `AfterToolCall` → `emitTodoUpdated` |
| 3 | 生产 payload | `rails/stream_event_emit.go:244` `formatTodosForFrontend` | 返回 `[]map[string]any` |
| 4 | 写流 | `rails/stream_event_emit.go:169` | `OutputSchema{Type:"todo.updated", Payload:{"todos": todos}}` |
| 5 | 桥接 payload | `server/utils/payload_bridge.go:56` | payload 已是 `map[string]any` → 快路径**原样返回**，内层切片类型不变 |
| 6 | 消费断言 | `server/utils/stream_utils.go:298` | `payload["todos"].([]any)` **失败** → 兜成 `[]any{}` |
| 7 | 前端 | `useWebSocket.ts:1480` → `TodoList/index.tsx:15` | `setTodos([])` → `todos.length === 0` → 「暂无任务」 |

同一形态的第二个消费点：`server/adapter/deep_adapter_team.go:1694`（`parseTypedChunkMap`，team 模式消费链）。

## 2. 为什么 Python 没有对应的转换点

Python 的 list 不携带元素类型，`payload["todos"]` 取出即可迭代，不存在「map → list」这一步。Go 里 `payload["todos"].([]any)` 是**运行时具体类型断言**，只接受 `[]any` 一个类型；生产端给的是 `[]map[string]any`，断言必然返回 `(nil, false)`。

因此这不是「少了一步转换」，而是 Go 静态类型与 Python duck typing 的固有差异，只能在生产端或消费端二选一收口。

## 3. 方案决策

| 生产端 | 消费端 | 结果 |
|---|---|---|
| `[]map[string]any` | 只认 `[]any` | 必坏（当前 bug） |
| `[]any` | 只认 `[]any` | 可修好，但把约定押在生产者身上：任何人在 Go 里自然地写具体切片类型就会静默复发 |
| `[]map[string]any` | 只认 `[]map[string]any` | 可修好，但走过 JSON 边界的来源（跨进程 team chunk、测试夹具、将来的落盘重放）反序列化后是 `[]any`，反向塌陷 |
| **`[]map[string]any`（保持）** | **两种形态都接受** | 全通 ✅ **采用** |

**决策**

1. 生产端保持 `[]map[string]any`：它是更精确的 Go 表达，可读性优于 `[]any`。
2. 消费端只改一处：`server/utils/stream_utils.go` 的 `todo.updated` 分支同时接受 `[]any` 与 `[]map[string]any`。
3. 用 type switch，不引入反射：目前只有这两种真实形态；未知形态**记录日志**暴露出来，而不是继续静默兜成空列表。
4. `parseTypedChunkMap` 的 `case "todo.updated"` 是**死代码**：函数开头的 `strings.Contains(chunkType, ".")` 已对 `todo.updated` 提前 return（值原样透传，不做类型断言），team 路径本就不受影响。该分支删除，并在 dotted 分支补注释。
5. ACP 侧（`e2a/acp/acp_tool_updates.go:305`）**不改**：它的 payload 来自已解析事件（`acp_channel.go:1096` `payload := msg.Payload`），其中 `todos` 早已是 `[]any`，从未出问题。
6. 前端**零改动**：两种 Go 类型 `json.Marshal` 后是同一串 JSON，前端 `Array.isArray` 判断不受影响。

### 3.1 为什么 team 路径不需要修

`parseTypedChunkMap`（`deep_adapter_team.go:1500`）的分派顺序是：

```go
if strings.Contains(chunkType, ".") {     // "todo.updated" 命中
    result := map[string]any{"event_type": chunkType}
    for k, v := range payload {
        result[k] = serializeValueAny(v)  // 只转 time.Time，其余原样
    }
    return result
}
switch chunkType {
case "todo.updated":                      // 永不可达
```

`serializeValueAny`（`deep_adapter_team.go:1785`）原样返回非时间值，因此 team 链路上的 `todos` 保持 `[]map[string]any`，但下游没有任何 Go 侧类型断言，最终直接 JSON 序列化给前端——功能正常。该分支自移植起就不可达（Python `_parse_typed_chunk` 同样把 `if "." in chunk_type: return` 写在 `switch` 之前）。

## 4. 改动清单

### 4.1 新增读取助手（`server/utils/stream_utils.go`）

与既有 `ExtractStringFromPayload` / `ExtractIntFromPayload` 同族，命名保持 `Extract*FromPayload`：

```go
// ExtractAnySliceFromPayload 读取 payload 里的列表字段，收敛为 []any。
// 支持两种进程内形态：[]any（JSON 反序列化 / ACP / 测试夹具）与
// []map[string]any（业务代码直接构造，如 formatTodosForFrontend）。
// ok=false 表示键存在、值非 nil，但不是上述两种形态。
func ExtractAnySliceFromPayload(payload map[string]any, key string) ([]any, bool)
```

语义表：

| 输入 | 返回 |
|---|---|
| 键不存在 | `([]any{}, true)` |
| 值为 nil | `([]any{}, true)` |
| `[]any` | `(v, true)` |
| `[]map[string]any` | `(逐个装箱为 []any, true)` |
| 其他（string / map / 结构体等） | `(nil, false)` |

### 4.2 消费点改造（`server/utils/stream_utils.go`）

`todo.updated` 分支改为（同包内直接调用助手）：

```go
todos, ok := ExtractAnySliceFromPayload(payload, "todos")
if !ok {
    logger.Warn(logComponent).
        Str("event_type", "todo.updated").
        Str("key", "todos").
        Str("actual_type", fmt.Sprintf("%T", payload["todos"])).
        Msg("todo.updated todos type assertion failed, using empty list")
    todos = []any{}
}
```

日志字段与同文件既有 `ExtractIntFromPayload` / `ExtractFloatFromPayload` 的 Warn 保持一致（`key` + `actual_type`），便于按字段检索。

### 4.3 删除 team 侧死分支（`server/adapter/deep_adapter_team.go`）

1. 删除 `parseTypedChunkMap` 中不可达的 `case "todo.updated":`（约 8 行，含其兜底 `[]any{}`）。
2. 在 `strings.Contains(chunkType, ".")` 分支处补注释：带点号的事件类型（含 `todo.updated`）走这里原样透传，值不归一化，下游按 JSON 直出。

### 4.4 回滚上一轮工作区的多余改动

| 文件 | 处置 |
|---|---|
| `internal/common/utils/slice.go` + `slice_test.go` | 删除（反射助手不再需要） |
| `internal/common/utils/doc.go` | 回滚 |
| `internal/swarm/e2a/acp/acp_tool_updates.go` + `_test.go` | 回滚（ACP 未坏） |
| `internal/swarm/server/utils/stream_utils.go` | 替换为 4.1/4.2 版本 |
| `internal/swarm/server/adapter/deep_adapter_team.go` | 替换为 4.3 版本（删死分支） |
| `tests/integration/swarm/server/adapter/todo_panel_test.go` | **保留**（真实链路回归用例，与本方案兼容） |

## 5. 测试策略

| 层级 | 文件 | 用例 |
|---|---|---|
| 单测 | `server/utils/stream_utils_test.go` | `[]any`（已有）、`[]map[string]any`（已有）、未知形态 → 空列表 + 不 panic（新增）、缺键（已有）、键为 nil（新增） |
| 单测 | `server/adapter/deep_adapter_team_chunktomap_test.go` | 新增 1 例：typed 形状的 `todo.updated` 走 dotted 前缀分支、`todos` 原样保留（锁定"删除死分支后行为不变"） |
| 集成 | `tests/integration/swarm/server/adapter/todo_panel_test.go` | MockLLM 真调 `todo_create` → 断言事件非空、元素为 `map[string]any`、`todo.json` 真落盘 |
| 门禁 | — | 改动行覆盖 ≥85%、`-race`、`integration-audit -verify` 两层门禁 |

## 6. 验收标准

1. 单测、集成、race、lint 全绿（远端 CI 四个 job）。
2. 手动验证：agent 模式下让模型创建 2 个待办，右侧任务栏出现「进行中 / 待处理」分组；调用 `todo_modify` 改为 completed 后状态实时变化；切换会话后清空。
3. 正常路径日志中不出现新的 Warn；构造一个未知形态时该 Warn 能打出真实类型。

## 7. 明确不做

- 不改 `formatTodosForFrontend` 的返回类型
- 不改 ACP 出站组帧
- 不引入反射型通用切片助手（不预造第三种形态的兼容）
- 不审计 `payload` 里其他 list 字段（`controller_output.data` 等），属于另一轮系统性排查
