# 6.10 ForceFinishRequest / RetryRequest 回填设计

## 背景

6.10 小节（ForceFinishRequest / RetryRequest）在 IMPLEMENTATION_PLAN.md 中已标记 ✅，
但对比 Python 源码发现 3 处未对齐 + 1 组冗余类型需要清理。

### 6.10 在 Agent 会话流程中的位置

6.10 位于领域 6 **Rail 系统**分组最后一个步骤（6.7→6.8→6.9→6.10），
紧排在 ReAct Agent 分组（6.11 起）之前。

- **承上**：6.8 的 `ConsumeRetryRequest`/`HasForceFinishRequest` 最初是 panic 占位桩，
  标注 `⤵️ 6.10 回填`，6.10 完成后回填为真实实现
- **启下**：6.11 ReActAgent 的 ReAct 循环依赖 `ConsumeForceFinish` 检查提前终止信号，
  以及 `RailExecutor.Execute` 中的重试逻辑

### 在 Agent 会话中的作用

ForceFinishRequest 和 RetryRequest 是 Rail 系统的两大控制信号，
构成"设置 → 一次性消费"模式：

| 信号 | 产生位置 | 消费位置 | 行为 |
|------|---------|---------|------|
| `RequestRetry()` | on_exception 钩子内 | `@rail` wrapper 内 `ConsumeRetryRequest()` | 延迟后重试当前被装饰的方法体 |
| `RequestForceFinish()` | 任意钩子内 | ReAct 循环内 `ConsumeForceFinish()` | 立即终止循环，返回 `finish.result` |
| `HasForceFinishRequest()` | — | `@rail` wrapper 内 | before 钩子请求 force-finish 时跳过方法体 |

### 完整调用链路

```
ReActAgent.invoke()
  └─ ReActAgent._inner_invoke()
       ├─ _railed_model_call()     ← @rail(BEFORE/AFTER_MODEL_CALL, ON_MODEL_EXCEPTION)
       │    │  1. fire(BEFORE)     → before 钩子可 request_force_finish()
       │    │  2. ★ force-finish 门控: HasForceFinishRequest() → 跳过 LLM 调用
       │    │  3. llm.invoke()     → 实际调用
       │    │  4. 异常 → fire(ON_EXCEPTION) → 钩子可 request_retry()
       │    │  5. ConsumeRetryRequest() → 有则 sleep+重试，无则 re-raise
       │    │  6. fire(AFTER)      → after 钩子
       │    └─ ★ consume_force_finish() → 可提前终止 ReAct 循环
       │
       ├─ _railed_execute_tool_call()  ← @rail(BEFORE/AFTER_TOOL_CALL, ON_TOOL_EXCEPTION)
       │    │  (同上流程)
       │    └─ force-finish 传播: 子 toolCtx → 父 cbc
       │
       └─ ★ consume_force_finish() → 决定是否终止循环
```

---

## Python vs Go 对齐状态

### ✅ 已对齐

| # | 项目 | Python | Go |
|---|------|--------|-----|
| 1 | `RetryRequest` 结构体 | `dataclass: delay_seconds: float = 0.0` | `struct: DelaySeconds float64` |
| 2 | `ForceFinishRequest` 结构体 | `dataclass: result: Dict[str, Any]` | `struct: Result map[string]any` |
| 3 | `ConsumeRetryRequest` 一次性消费 | read-and-clear | read-and-clear |
| 4 | `ConsumeForceFinish` 一次性消费 | read-and-clear | read-and-clear |
| 5 | `HasForceFinishRequest` | `@property` | 方法 |
| 6 | CancelledError 跳过 after 钩子 | `isinstance(..., asyncio.CancelledError)` | `ctx.Err() != nil` |
| 7 | `consume_force_finish` 两处调用位置 | 模型调用后 + 工具调用后 | 完全对齐 |
| 8 | force-finish 门控语义 | `return None` (触发 finally) | `return fireAfter(nil)` (等价) |
| 9 | 重试延迟机制 | `await asyncio.sleep(delay)` | `time.After` + `select` 监听 `ctx.Done()` |
| 10 | 模型调用 Rail 包装 | `@rail(BEFORE/AFTER_MODEL_CALL, ON_MODEL_EXCEPTION)` | `ModelCallRail.Execute()` |

### ❌ 未对齐（本次回填目标）

| # | 缺失项 | Python 行为 | Go 当前状态 |
|---|--------|------------|------------|
| A | `RequestRetry` 负数保护 | `if delay_seconds < 0: delay_seconds = 0.0` | 无校验，直接赋值 |
| B | `AbilityManager` force-finish 传播 | 遍历子 `tool_ctx` → 父 `ctx` | 仅有 `⤵️ 预留` 注释 |
| C | 工具调用 Rail 包装 | `@rail(BEFORE/AFTER_TOOL_CALL, ON_TOOL_EXCEPTION)` | 未包装 `ToolCallRail.Execute` |
| D | 冗余类型 | 无 `ToolRail`/`ToolCallContext`/`ToolCallResult` | 存在但从未使用 |

---

## 设计方案

### A: RequestRetry 负数保护

**改动文件**：`rail/context.go`

**改动**：`RequestRetry` 方法内添加负数归零：

```go
func (c *AgentCallbackContext) RequestRetry(delaySeconds float64) {
    if delaySeconds < 0 {
        delaySeconds = 0
    }
    c.retryRequest = &RetryRequest{DelaySeconds: delaySeconds}
}
```

**对应 Python**：

```python
def request_retry(self, delay_seconds: float = 0.0) -> None:
    if delay_seconds < 0:
        delay_seconds = 0.0
    self._retry_request = RetryRequest(delay_seconds=delay_seconds)
```

**测试**：`rail/context_test.go` 添加 `TestRequestRetry_负数归零` 用例。

---

### B: AbilityManager force-finish 传播

#### B-1：AbilityManager.Execute 接收 cbc 参数

**改动文件**：`ability/ability_manager.go`、`agents/react_agent.go`

**当前签名**：
```go
func (am *AbilityManager) Execute(
    ctx context.Context, toolCalls []*llmschema.ToolCall, sess *session.Session, tag string,
) []ExecuteResult
```

**改为**：
```go
func (am *AbilityManager) Execute(
    ctx context.Context,
    cbc *rail.AgentCallbackContext,
    toolCalls []*llmschema.ToolCall,
    sess *session.Session,
    tag string,
) []ExecuteResult
```

**调用方同步修改**（`react_agent.go`）：
```go
// 当前：results, err := am.Execute(ctx, toolCalls, sess)
// 改为：results, err := am.Execute(ctx, cbc, toolCalls, sess)
```

**对应 Python**：
```python
async def execute(self, ctx: AgentCallbackContext, tool_call, session, tag=None):
```

#### B-2：每个 tool_call 创建独立子上下文

**改动文件**：`rail/context.go`

新增 `ForkForToolCall` 方法：

```go
// ForkForToolCall 为单个工具调用创建隔离的子上下文。
//
// 共享字段（引用共享，跨 rail 通信）：
//   - agent、extra、steeringQueue、session、config
//
// 独立字段（每个工具调用各自持有）：
//   - retryRequest、forceFinishRequest、exception、retryAttempt、event、inputs
//
// 对应 Python: AbilityManager.execute 中 tool_ctx = AgentCallbackContext(
//   agent=ctx.agent, inputs=ToolCallInputs(...), config=ctx.config,
//   session=session, context=ctx.context, extra=ctx.extra,
// )
func (c *AgentCallbackContext) ForkForToolCall(toolCall *llmschema.ToolCall) *AgentCallbackContext {
    return &AgentCallbackContext{
        agent:        c.agent,
        inputs:       &ToolCallInputs{
            ToolCall: toolCall,
            ToolName: toolCall.Name,
            ToolArgs: toolCall.Arguments,
        },
        config:       c.config,
        session:      c.session,
        modelContext:  c.modelContext,
        extra:        c.extra,  // 引用共享
        // retryRequest / forceFinishRequest / exception / retryAttempt 各自零值
        steeringQueue: c.steeringQueue,  // 引用共享
    }
}
```

**AbilityManager.Execute 中使用**：

```go
// 为每个 tool_call 创建子上下文
toolCtxs := make([]*rail.AgentCallbackContext, len(toolCalls))
for i, tc := range toolCalls {
    toolCtxs[i] = cbc.ForkForToolCall(tc)
}

// 并行执行
var wg sync.WaitGroup
for i, tc := range toolCalls {
    wg.Add(1)
    go func(idx int, toolCall *llmschema.ToolCall, toolCtx *rail.AgentCallbackContext) {
        defer wg.Done()
        results[idx] = am.railedExecuteSingleToolCall(ctx, toolCtx, toolCall, sess, tag)
    }(i, tc, toolCtxs[i])
}
wg.Wait()
```

#### B-3：force-finish 传播

**改动文件**：`ability/ability_manager.go`

在 `wg.Wait()` 后添加传播逻辑（替换 `⤵️ 预留` 注释）：

```go
// force-finish 信号传播：子 toolCtx → 父 cbc
// 对应 Python: for tool_ctx in tool_contexts:
//   ff = tool_ctx.consume_force_finish()
//   if ff is not None: ctx.request_force_finish(ff.result); break
for _, toolCtx := range toolCtxs {
    if ff := toolCtx.ConsumeForceFinish(); ff != nil {
        cbc.RequestForceFinish(ff.Result)
        break
    }
}
```

**对应 Python**：
```python
for tool_ctx in tool_contexts:
    ff = tool_ctx.consume_force_finish()
    if ff is not None:
        ctx.request_force_finish(ff.result)
        break
```

---

### C: 工具调用 Rail 包装

**改动文件**：`ability/ability_manager.go`

`railedExecuteSingleToolCall` 改为接收 `toolCtx *rail.AgentCallbackContext`，
用 `ToolCallRail.Execute` 包装：

```go
func (am *AbilityManager) railedExecuteSingleToolCall(
    ctx context.Context,
    toolCtx *rail.AgentCallbackContext,
    toolCall *llmschema.ToolCall,
    sess *session.Session,
    tag string,
) ExecuteResult {
    var result ExecuteResult
    _ = rail.ToolCallRail.Execute(ctx, toolCtx, func() error {
        result = am.executeSingleToolCall(ctx, toolCall, sess, tag)
        return result.Err
    })
    return result
}
```

**对应 Python**：
```python
@rail(
    before=AgentCallbackEvent.BEFORE_TOOL_CALL,
    after=AgentCallbackEvent.AFTER_TOOL_CALL,
    on_exception=AgentCallbackEvent.ON_TOOL_EXCEPTION,
)
async def _railed_execute_single_tool_call(self, ctx, tool_call, session, tag=None):
    ...
```

**对称参考**（模型调用的已有实现）：
```go
// react_agent.go 中
var result *llmschema.AssistantMessage
err := rail.ModelCallRail.Execute(ctx, cbc, func() error {
    var e error
    result, e = a.railedModelCall(ctx, cbc)
    return e
})
```

---

### D: 删除冗余类型（ToolRail + ToolCallContext + ToolCallResult）

**原因**：
1. Python 没有 `ToolRail`/`ToolCallContext`/`ToolCallResult`——所有钩子统一通过 `AgentRail` + `AgentCallbackContext` 处理
2. Go 的 `AgentRail`（`rail/rail.go`）已包含 `BeforeToolCall`/`AfterToolCall`/`OnToolException` 且已接收 `*AgentCallbackContext`
3. 三件套从未被实际使用，全是 `⤵️ 预留`

**删除清单**：

| 文件 | 删除内容 |
|------|---------|
| `ability/ability_types.go` | `ToolRail` 接口、`ToolCallContext` 结构体、`ToolCallResult` 结构体、`rail` 导入 |
| `ability/ability_manager.go` | `rail ToolRail` 字段、`SetRail` 方法 |
| `single_agent/base.go` | `ToolRail = ability.ToolRail` re-export |
| `ability/doc.go` | 文档中 `ToolRail 预留` 描述更新 |
| `single_agent/doc.go` | 文档中 `ToolRail 预留` 描述更新 |

---

## ⤵️ 回填标记处理

| 文件 | 位置 | 当前标记 | 回填后 |
|------|------|---------|--------|
| `ability_manager.go` L390 | Execute 末尾 | `⤵️ 预留：force_finish 信号传播` | 替换为 B-3 传播代码 |
| `ability_manager.go` L405 | railed 开头 | `⤵️ 预留：BeforeToolCall Rail 钩子` | 删除（C 项由 ToolCallRail.Execute 处理） |
| `ability_manager.go` L410 | railed 结尾 | `⤵️ 预留：AfterToolCall Rail 钩子` | 删除（C 项由 ToolCallRail.Execute 处理） |
| `ability_manager.go` L49 | rail 字段 | `rail ToolRail` + `⤵️ 预留` | 整个字段删除（D 项） |
| `ability_types.go` L70 | ToolCallContext | `⤵️ 预留字段：force_finish / steering_queue / skip_tool` | 整个类型删除（D 项） |

---

## 测试计划

| 测试文件 | 测试用例 | 覆盖项 |
|---------|---------|--------|
| `rail/context_test.go` | `TestRequestRetry_负数归零` | A |
| `rail/context_test.go` | `TestForkForToolCall_字段共享与隔离` | B-2 |
| `ability/ability_manager_test.go` | `TestExecute_forceFinish传播` | B-3 |
| `ability/ability_manager_test.go` | `TestRailedExecuteSingleToolCall_Rail包装` | C |
| `rail/executor_test.go` | 已有重试+force-finish 门控测试 | 无新增 |

---

## 影响范围

| 包 | 影响 |
|---|------|
| `single_agent/rail` | 新增 `ForkForToolCall` 方法、`RequestRetry` 负数保护 |
| `single_agent/ability` | 删除 ToolRail/ToolCallContext/ToolCallResult，Execute 签名变更，railedExecuteSingleToolCall 重写 |
| `single_agent/agents` | `react_agent.go` 调用方传入 cbc |
| `single_agent` (base.go) | 删除 ToolRail re-export |
