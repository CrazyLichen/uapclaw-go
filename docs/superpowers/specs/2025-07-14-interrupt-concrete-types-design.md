# 消除 interrupt 链路中 `[]any` / `[2]any`，改用具体类型

## 背景

当前 `interrupt` 包的工具中断/HITL 链路大量使用 `[]any` 和 `[2]any` 模拟 Python tuple，
原因是当初 `ToolInterruptException` 在 `interrupt` 包中，`interrupt → ability` 会造成循环依赖。
现在 `ToolInterruptException` 已迁移到 `schema` 包，循环依赖已打破，
可以全面改用具体类型。

同时 `(*llmModel).Invoke(...)` 冗余解引用也一并修复。

## 修改范围

### 问题 1：`(*llmModel).Invoke` 冗余解引用

- `react_model_call.go:188` — `(*llmModel).Invoke` → `llmModel.Invoke`
- `react_model_call.go:220` — `(*llmModel).Stream` → `llmModel.Stream`

### 问题 2：消除 `[]any` / `[2]any`

#### 2a. 新增具体类型

在 `schema/result.go`（新文件）定义：

```go
// ToolCallResult 单次工具调用的结果对 (toolResult, toolMsg)。
// 替代原有 [2]any{result, toolMsg} tuple 模拟。
//
// 对应 Python: (tool_result, tool_msg) 元组
type ToolCallResult struct {
    // Result 执行结果
    //   - 正常: map[string]any
    //   - 中断: *ToolInterruptException
    //   - 子Agent中断: map[string]any (含 result_type="interrupt")
    //   - 工作流中断: *workflow.WorkflowOutput
    //   - 错误: *AbilityExecutionError
    Result any
    // ToolMsg 返回给 LLM 的 ToolMessage
    ToolMsg *llmschema.ToolMessage
}

// PayloadEntry 中断 payload 条目 (innerID, payload)。
// 替代原有 [2]any{innerID, payloadObj} tuple 模拟。
//
// 对应 Python: (inner_id, payload) 元组
type PayloadEntry struct {
    // InnerID 内部工具调用 ID
    InnerID string
    // Payload payload 对象：
    //   - *ToolCallInterruptRequest
    //   - *stream.OutputSchema
    Payload any
}
```

#### 2b. 类型替换表

| 位置 | 原类型 | 新类型 |
|------|--------|--------|
| `ExecuteToolCallFunc` 返回值 | `[]any` | `[]saschema.ToolCallResult` |
| `BuildInterruptState` results 参数 | `[]any` | `[]saschema.ToolCallResult` |
| `BuildInterruptState` 返回值（payloads） | `[]any` | `[]saschema.PayloadEntry` |
| `collectInterrupts` results 参数 | `[]any` | `[]saschema.ToolCallResult` |
| `collectInterrupts` payloads 返回值 | `[]any` | `[]saschema.PayloadEntry` |
| `CommitInterrupt` subAgentOutputs | `[]any` | `[]saschema.PayloadEntry` |
| `BuildInterruptResult` payloads 参数 | `[]any` | `[]saschema.PayloadEntry` |
| `handleToolInterruptException` payloads | `*[]any` | `*[]saschema.PayloadEntry` |
| `handleSubAgentInterrupt` payloads | `*[]any` | `*[]saschema.PayloadEntry` |
| `AfterExecuteToolCallForHITL` results 参数 | `[]any` | `[]saschema.ToolCallResult` |
| `AfterExecuteToolCallForHITL` 返回值 | `[]any` | `[]saschema.PayloadEntry` |
| `CommitInterrupt`（ReActAgent 委托）subAgentOutputs | `[]any` | `[]saschema.PayloadEntry` |
| `HandleResume` results 局部变量 | `[]any` | `[]saschema.ToolCallResult` |

#### 2c. `[2]any` 解包逻辑消除

所有 `[2]any` 解包逻辑替换为结构体字段访问：

- `result.([2]any)` → `tcResult.Result` / `tcResult.ToolMsg`
- `payload.([2]any)` → `entry.InnerID` / `entry.Payload`
- `anyResults[i] = [2]any{r.Result, r.ToolMsg}` → `toolCallResults[i] = saschema.ToolCallResult{Result: r.Result, ToolMsg: r.ToolMsg}`
- `*payloads = append(*payloads, [2]any{innerID, payload})` → `*payloads = append(*payloads, saschema.PayloadEntry{InnerID: innerID, Payload: payload})`

#### 2d. `isSubAgentInterrupt` 参数类型

`isSubAgentInterrupt(result any)` 保持 `any` — 因为调用方传入的是 `ToolCallResult.Result`（内部是 `any`），
子 Agent 中断检查本质上是检查一个动态 dict 结构。

#### 2e. `handleSubAgentInterrupt` 的 `toolResult any` 参数

保持 `any` — 与 `isSubAgentInterrupt` 同理，子 Agent 中断的 toolResult 是动态 dict。

#### 2f. 不修改的类型

- `ExecuteResult.Result` 保持 `any` — 它确实是联合类型
- `ToolCallInputs.ToolResult` 保持 `any` — 同上
- `ResumeContext.UserInput` 保持 `any` — 对齐 Python `Any`
- `saveAutoConfirmFromState` / `buildSubAgentResumeToolCall` 的 `userInput any` 保持 `any`
- `InterruptRequest.PayloadSchema` (`map[string]any`) — JSON Schema dict，保持
- `BuildInterruptResult` 返回 `map[string]any` — 这是最终输出 dict，保持

## 依赖关系验证

```
interrupt → schema   (已有，ToolCallResult/PayloadEntry 在 schema 中)
interrupt → ability   (新增，ExecuteToolCallFunc 返回 []ability.ExecuteResult? NO —
                        用 []saschema.ToolCallResult，不引入 ability 依赖)
ability → schema     (已有)
```

关键决策：`ExecuteToolCallFunc` 返回 `[]saschema.ToolCallResult`（不是 `[]ability.ExecuteResult`），
因为 `ToolCallResult` 是 `schema` 包的轻量类型，`ability.ExecuteResult` 包含更多逻辑。
调用方（`makeExecuteToolCallFunc`、`reactLoop`）从 `[]ability.ExecuteResult` 构造 `[]ToolCallResult` 即可。

这样 `interrupt` 不需要 import `ability`，保持单向依赖。
