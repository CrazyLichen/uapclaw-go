# AM 执行逻辑对齐 Python 设计文档

## 背景

Go 的 `AbilityManager.execute` 与 Python 的 `AbilityManager.execute` 存在多处执行逻辑差异，影响 Tool、Agent、Workflow 三种能力的正确执行。本设计文档记录完整的对比分析、问题清单和修复方案。

## 问题总览

| 优先级 | 编号 | 问题 | 影响范围 |
|---|---|---|---|
| P0 | P0-1 | `BuildToolMessageContent` 只处理 `map[string]any`，结构体/string 结果提取失败 | Tool + Agent + Workflow |
| P0 | P0-2 | Agent 执行直接 `ag.Invoke`，缺少子会话、conversation_id、auto_confirm、Runner 编排 | Agent |
| P0 | P0-3 | Tool invoke 不传 session，Tool 内部无法获取会话信息 | Tool |
| P1 | P1-1 | Workflow 无 session/context 隔离 | Workflow |
| P1 | P1-2 | `WorkflowOutput`/`WorkflowExecutionState` 类型未定义 | Workflow |
| P1 | P1-3 | Workflow 不经过 Runner 编排 | Workflow |
| P1 | P1-4 | Workflow 不处理 `INPUT_REQUIRED` 中断（executeWorkflow 侧保留检测，ReActAgent 侧暂不修改，等中断统一修复） | Workflow |

---

## P0-1：BuildToolMessageContent 对非 map 类型失效

### 问题

Python 的 `_build_tool_message_content` 用 `getattr(result, "data", None)` duck-typing，支持任意类型（ToolOutput 对象、dict、string）。Go 版本只做 `result.(map[string]any)` 类型断言，非 map 类型直接走 `fmt.Sprintf("%v", result)` 输出垃圾格式。

具体场景：

| result 来源 | result 类型 | Go 当前输出 | Python 输出 |
|---|---|---|---|
| InvokeFunction 返回 string → structToMap 包装 | `map[string]any{"result": "search..."}` | `map[result:search...]` | `"search..."` |
| Agent.Invoke 返回结构体 | `any`（非 map） | `"&{field1 val1}"` | 提取 data.content 或 str() |
| Workflow.Invoke 返回结构体 | `any`（非 map） | `"&{field1 val1}"` | str() |
| Todo 工具返回 dict | `map[string]any{"message": "created"}` | `map[message:created]` | `{"message": "created"}` |

### 修复方案

扩展 `BuildToolMessageContent`，增加四个处理路径：

```go
func BuildToolMessageContent(result any) string {
    // 路径 1：map[string]any — 按 key 提取
    if m, ok := result.(map[string]any); ok {
        // 1a. data.content 提取（现有）
        if data, ok := m["data"].(map[string]any); ok {
            if content, ok := data["content"]; ok {
                if s := fmt.Sprintf("%v", content); s != "" { return s }
            }
        }
        // 1b. success=false + error 提取（现有）
        if success, ok := m["success"].(bool); ok && !success {
            if errVal, ok := m["error"]; ok { return fmt.Sprintf("%v", errVal) }
        }
        // 1c. structToMap 的 {"result": v} 包装 — 解包后递归处理（新增）
        if v, ok := m["result"]; ok && len(m) == 1 {
            return BuildToolMessageContent(v)
        }
        // 1d. 普通 map — JSON 序列化（新增，对齐 Python str(dict)）
        if jsonBytes, err := json.Marshal(m); err == nil {
            return string(jsonBytes)
        }
    }

    // 路径 2：反射提取（新增，对齐 Python getattr(result, "data", None)）
    v := reflect.ValueOf(result)
    if v.Kind() == reflect.Ptr { v = v.Elem() }
    if v.Kind() == reflect.Struct {
        if f := v.FieldByName("Data"); f.IsValid() {
            if dataMap, ok := f.Interface().(map[string]any); ok {
                if content, ok := dataMap["content"]; ok {
                    if s := fmt.Sprintf("%v", content); s != "" { return s }
                }
            }
        }
        if f := v.FieldByName("Success"); f.IsValid() && f.Kind() == reflect.Bool && !f.Bool() {
            if ef := v.FieldByName("Error"); ef.IsValid() {
                return fmt.Sprintf("%v", ef.Interface())
            }
        }
    }

    // 路径 3：最终 fallback
    return fmt.Sprintf("%v", result)
}
```

### 改动文件

- `internal/agentcore/single_agent/ability/ability_types.go`：修改 `BuildToolMessageContent`

---

## P0-2：Agent 执行缺少完整生命周期

### 问题

Python 的 agent 执行路径：

```
executeAgent:
  1. agent_card = self._agents[tool_name]                    # L778
  2. agent_id = agent_card.id or agent_card.name             # L779
  3. agent = await Runner.resource_mgr.get_agent(...)        # L781
  4. child_session_id = f"{session.get_session_id()}:{tool_call.id}"  # L788
  5. tool_args["conversation_id"] = child_session_id          # L789
  6. child_session = create_agent_session(...)                # L791-794
  7. auto_confirm 传播                                        # L796-798
  8. result = await Runner.run_agent(agent, inputs, session)  # L800

Runner.run_agent:
  9. with self._root_task_group_scope()                       # L417
  10. _prepare_agent → pre_run                                # L418
  11. if _is_remote_agent → invoke(inputs)                    # L419-420
  12. elif LegacyBaseAgent → invoke(inputs, session=None)     # L421-423
  13. else → invoke(inputs, agent_session)                    # L425
  14. agent_session.post_run()                                # L426
```

Go 当前只做了步骤 1-3，然后直接 `ag.Invoke(ctx, toolArgs)`，缺少 4-14。

### 修复方案

#### 1. 创建 Runner 全局函数（`runner/runner.go`）

```go
// RunAgent 执行单个 Agent，管理完整的会话生命周期。
//
// 对齐 Python: Runner.run_agent(agent, inputs, *, session, context, envs)
// Python 源码: openjiuwen/core/runner/runner.py L399-427
//
// 步骤对照：
//   Python L417: with self._root_task_group_scope()
//   Python L418: agent_instance, agent_session = await self._prepare_agent(agent, inputs, session)
//     Python L504-512: if isinstance(session, AgentSession) → pre_run + return
//     Python L513-514: session_id = inputs.get(conversation_id, ...)
//     Python L515-522: if isinstance(agent, str) → get_agent + remote check
//     Python L524-526: agent_session = _create_agent_session + pre_run
//   Python L419: if _is_remote_agent → invoke(inputs)
//   Python L421-423: elif LegacyBaseAgent → invoke(inputs, session=None)
//   Python L425: else → invoke(inputs, agent_session)
//   Python L426: await agent_session.post_run()
func RunAgent(
    ctx context.Context,
    agent interfaces.BaseAgent,
    inputs map[string]any,
    sess *session.Session,
) (any, error) {
    // 步骤 1：任务组作用域（对齐 Python L417）
    // ⤵️ 预留章节回填：任务组作用域

    // 步骤 2：_prepare_agent → pre_run（对齐 Python L418 → L509/L511/L525）
    // ⤵️ 预留章节回填：session.PreRun

    // 步骤 3：远程 Agent 判断（对齐 Python L419-420）
    // ⤵️ 预留章节回填：远程 Agent 支持

    // 步骤 4：LegacyBaseAgent 判断（对齐 Python L421-423）
    // ⤵️ 预留章节回填：LegacyBaseAgent 兼容

    // 步骤 5：正常 Agent 调用（对齐 Python L425）
    result, err := agent.Invoke(ctx, inputs, interfaces.WithSession(sess))
    if err != nil {
        return nil, err
    }

    // 步骤 6：post_run 清理（对齐 Python L426）
    // ⤵️ 预留章节回填：session.PostRun

    return result, nil
}

// RunWorkflow 执行单个 Workflow，管理会话和上下文生命周期。
//
// 对齐 Python: Runner.run_workflow(workflow, inputs, *, session, context, envs)
// Python 源码: openjiuwen/core/runner/runner.py L350-369
//
// 步骤对照：
//   Python L367: with self._root_task_group_scope()
//   Python L368: workflow_instance, workflow_session = await self._prepare_workflow(workflow, session)
//   Python L369: workflow_instance.invoke(inputs, session=workflow_session, context=context)
func RunWorkflow(
    ctx context.Context,
    workflow interfaces.Workflow,
    inputs map[string]any,
    workflowSess *session.WorkflowSession,
    wfCtx any,
) (any, error) {
    // 步骤 1：任务组作用域（对齐 Python L367）
    // ⤵️ 预留章节回填：任务组作用域

    // 步骤 2：_prepare_workflow（对齐 Python L368）
    // ⤵️ 预留章节回填：_prepare_workflow 完整逻辑

    // 步骤 3：调用 workflow.Invoke（对齐 Python L369）
    // ⤵️ 预留章节回填：WorkflowOptions 传 session + context
    result, err := workflow.Invoke(ctx, inputs)
    if err != nil {
        return nil, err
    }
    return result, nil
}
```

#### 2. 修改 `executeAgent`

```go
func (am *AbilityManager) executeAgent(...) ExecuteResult {
    // 步骤 1：获取 AgentCard（对齐 Python L777-778）
    agentCard := am.agents[toolName]

    // 步骤 2：解析 agent_id（对齐 Python L779）
    agentID := agentCard.ID
    if agentID == "" { agentID = agentCard.Name }

    // 步骤 3：从 ResourceManager 获取 agent 实例（对齐 Python L780-781）
    agAny, err := am.resourceMgr.GetAgent(agentID)
    // ... 错误处理 ...
    ag, ok := agAny.(interfaces.BaseAgent)
    // ... 类型断言 ...

    // 步骤 4：构造子会话 ID（对齐 Python L788）
    childSessionID := fmt.Sprintf("%s:%s", sess.GetSessionID(), toolCall.ID)

    // 步骤 5：注入 conversation_id（对齐 Python L789）
    toolArgs["conversation_id"] = childSessionID

    // 步骤 6：创建子会话（对齐 Python L791-794）
    childSession := session.CreateAgentSession(agentID, childSessionID)

    // 步骤 7：传播 auto_confirm（对齐 Python L796-798）
    autoConfirmVal, _ := sess.GetState(InterruptAutoConfirmKey)
    if autoConfirmVal != nil {
        childSession.UpdateState(map[string]any{InterruptAutoConfirmKey.String(): autoConfirmVal})
    }

    // 步骤 8：通过 Runner.RunAgent 执行（对齐 Python L800）
    result, err := runner.RunAgent(ctx, ag, toolArgs, childSession)
    // ... 错误处理 ...

    // 步骤 9：构建 ToolMessage（对齐 Python L834-838）
    content := BuildToolMessageContent(result)
    toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
    return ExecuteResult{Result: result, ToolMsg: toolMsg}
}
```

#### 3. 中断自动确认常量

在 `ability` 包中定义（对齐 Python `INTERRUPT_AUTO_CONFIRM_KEY = "__interrupt_auto_confirm__"`）：

```go
// InterruptAutoConfirmKey 中断自动确认状态键。
// 对齐 Python: openjiuwen/core/single_agent/interrupt/state.py INTERRUPT_AUTO_CONFIRM_KEY
var InterruptAutoConfirmKey = state.StringKey("__interrupt_auto_confirm__")
```

### 改动文件

- **新文件** `internal/agentcore/runner/runner.go`：`RunAgent` + `RunWorkflow` 全局函数
- `internal/agentcore/single_agent/ability/ability_manager.go`：修改 `executeAgent`
- `internal/agentcore/single_agent/ability/ability_types.go`：新增 `InterruptAutoConfirmKey` 常量

---

## P0-3：Tool invoke 传 session

### 问题

Go 的 `Tool.Invoke` 签名通过 `ToolOption` 传递选项，但 `ToolCallOptions` 没有 Session 字段，`executeTool` 调用时不传 session。导致需要 session 的 Tool（如 Todo、TaskTool）无法获取会话信息。

Python 中 `tool.invoke(tool_args, session=session)` 通过 `**kwargs` 传 session。

### 修复方案

#### 1. `ToolCallOptions` 增加 Session 字段

```go
type ToolCallOptions struct {
    SkipNoneValue      bool
    SkipInputsValidate bool
    Timeout            float64
    MaxResponseBytes   int
    RaiseForStatus     bool
    Session            *session.Session  // 新增：会话实例（对齐 Python kwargs["session"]）
}

func WithToolSession(sess *session.Session) ToolOption {
    return func(o *ToolCallOptions) { o.Session = sess }
}
```

#### 2. `InvokeFunction` 用户函数签名改为带 `opts ...ToolOption`

```go
// 旧签名：func(context.Context, I) (O, error)
// 新签名：func(context.Context, I, ...ToolOption) (O, error)

type InvokeFunction[I any, O any] struct {
    card *ToolCard
    fn   func(context.Context, I, ...ToolOption) (O, error)
}

// Invoke 内部调用改为：
output, err := f.fn(ctx, input, opts...)
```

用户函数使用示例：

```go
// 不需要 session 的 Tool：忽略 opts
func Search(ctx context.Context, input SearchInput, opts ...tool.ToolOption) (SearchOutput, error) {
    return SearchOutput{Results: []string{input.Query}}, nil
}

// 需要 session 的 Tool：从 opts 解析
func TodoCreate(ctx context.Context, input TodoCreateInput, opts ...tool.ToolOption) (TodoCreateOutput, error) {
    o := tool.NewToolCallOptions(opts...)
    if o.Session == nil {
        return TodoCreateOutput{}, fmt.Errorf("session is required")
    }
    sessionID := o.Session.GetSessionID()
    // 用 sessionID 做文件隔离
    return TodoCreateOutput{Message: "创建成功"}, nil
}
```

#### 3. `executeTool` 调用时传入 session

```go
// 旧：result, err := lt.Invoke(ctx, toolArgs)
// 新：result, err := lt.Invoke(ctx, toolArgs, tool.WithToolSession(sess))
```

`executeFallbackTool` 同理。

### 调用链路

```
AbilityManager.executeTool(ctx, toolCall, toolName, toolArgs, sess, tag)
  └─ lt.Invoke(ctx, toolArgs, tool.WithToolSession(sess))
       └─ [LifecycleTool 透传 opts]
            └─ inner.Invoke(ctx, inputs, opts...)
                 └─ f.fn(ctx, input, opts...)       // 用户函数收到 opts
                      └─ o := NewToolCallOptions(opts...)
                           o.Session.GetSessionID() // 获取 session
```

### 改动文件

- `internal/agentcore/foundation/tool/base.go`：`ToolCallOptions` 增加 Session + `WithToolSession`
- `internal/agentcore/foundation/tool/invoke_function.go`：`fn` 字段类型 + Invoke 内部调用
- `internal/agentcore/foundation/tool/stream_function.go`：同理
- `internal/agentcore/single_agent/ability/ability_manager.go`：`executeTool` + `executeFallbackTool` 传入 `WithToolSession`
- `internal/agentcore/foundation/tool/invoke_function_test.go`：所有测试函数签名加 `opts ...ToolOption`
- `internal/agentcore/foundation/tool/stream_function_test.go`：同上

---

## P1-1 + P1-3：Workflow 执行改造

### 问题

Python 的 workflow 执行路径：

```
executeWorkflow:
  1. workflow_card = self._workflows[tool_name]                # L761-762
  2. workflow = await Runner.resource_mgr.get_workflow(...)    # L763-764
  3. workflow_session = session.create_workflow_session()      # L707
  4. workflow_context = context_engine.create_context(...)      # L708-712
  5. workflow_output = await Runner.run_workflow(...)           # L713-718
  6. if WorkflowOutput.state == INPUT_REQUIRED:                # L719-723
       return (WorkflowOutput, None)  # 中断
  7. result = workflow_output.result                           # L725
  8. ToolMessage(content=str(result))                          # L726
```

Go 当前只做了步骤 1-2，然后直接 `wf.Invoke(ctx, toolArgs)`。

### 修复方案

#### 修改 `executeWorkflow`

```go
func (am *AbilityManager) executeWorkflow(...) ExecuteResult {
    // 步骤 1：获取 WorkflowCard（对齐 Python L761-762）
    wfCard := am.workflows[toolName]
    wfID := wfCard.ID
    if wfID == "" { wfID = wfCard.Name }

    // 步骤 2：从 ResourceManager 获取 workflow 实例（对齐 Python L763-764）
    wfAny, err := am.resourceMgr.GetWorkflow(wfID)
    // ... 错误处理 ...
    wf, ok := wfAny.(interfaces.Workflow)
    // ... 类型断言 ...

    // 步骤 3：创建 workflow session（对齐 Python L707）
    var workflowSess *session.WorkflowSession
    if sess != nil {
        workflowSess = sess.CreateWorkflowSession()
    }

    // 步骤 4：创建隔离 context（对齐 Python L708-712）
    var wfCtx any
    if am.contextEngine != nil {
        wfCtx, _ = am.contextEngine.CreateContext(ctx, wfID, sess)
    }

    // 步骤 5：通过 Runner.RunWorkflow 执行（对齐 Python L713-718）
    result, err := runner.RunWorkflow(ctx, wf, toolArgs, workflowSess, wfCtx)
    // ... 错误处理 ...

    // 步骤 6：检测 INPUT_REQUIRED 中断（对齐 Python L719-723）
    if wfOut, ok := result.(*workflow.WorkflowOutput); ok && wfOut.State == workflow.WorkflowExecutionStateInputRequired {
        return ExecuteResult{Result: wfOut, ToolMsg: nil}
    }

    // 步骤 7：正常完成 — 提取 result（对齐 Python L725）
    actualResult := result
    if wfOut, ok := result.(*workflow.WorkflowOutput); ok {
        actualResult = wfOut.Result
    }

    // 步骤 8：构建 ToolMessage（对齐 Python L726）
    content := BuildToolMessageContent(actualResult)
    toolMsg := llmschema.NewToolMessage(toolCall.ID, content)
    return ExecuteResult{Result: actualResult, ToolMsg: toolMsg}
}
```

#### 扩展 `WorkflowOptions`

```go
// 旧：
type WorkflowOptions struct{}

// 新：
type WorkflowOptions struct {
    Session *session.WorkflowSession   // 工作流会话（对齐 Python workflow.invoke(inputs, session=...)）
    Context any                        // ModelContext，待领域八具体化（对齐 Python workflow.invoke(inputs, context=...)）
}

func WithWorkflowSession(sess *session.WorkflowSession) WorkflowOption {
    return func(o *WorkflowOptions) { o.Session = sess }
}

func WithWorkflowContext(ctx any) WorkflowOption {
    return func(o *WorkflowOptions) { o.Context = ctx }
}
```

### 改动文件

- `internal/agentcore/single_agent/ability/ability_manager.go`：修改 `executeWorkflow`
- `internal/agentcore/single_agent/interfaces/interface.go`：`WorkflowOptions` 扩展 + `WithWorkflowSession` + `WithWorkflowContext`

---

## P1-2：定义 WorkflowOutput 和 WorkflowExecutionState

### 对齐 Python

Python 定义位置：`openjiuwen/core/workflow/base.py`

```python
class WorkflowExecutionState(str, Enum):
    COMPLETED = "COMPLETED"
    INPUT_REQUIRED = "INPUT_REQUIRED"
    ERROR = "ERROR"

class WorkflowOutput(BaseModel):
    result: Any
    state: WorkflowExecutionState
```

### Go 定义

文件位置对齐 Python：`internal/agentcore/workflow/base.go`

```go
package workflow

// WorkflowExecutionState 工作流执行状态。
// 对应 Python: openjiuwen/core/workflow/base.py WorkflowExecutionState(str, Enum)
type WorkflowExecutionState string

const (
    // WorkflowExecutionStateCompleted 执行完成
    WorkflowExecutionStateCompleted WorkflowExecutionState = "COMPLETED"
    // WorkflowExecutionStateInputRequired 需要用户输入（中断）
    WorkflowExecutionStateInputRequired WorkflowExecutionState = "INPUT_REQUIRED"
    // WorkflowExecutionStateError 执行出错
    WorkflowExecutionStateError WorkflowExecutionState = "ERROR"
)

// WorkflowOutput 工作流执行结果。
// 对应 Python: openjiuwen/core/workflow/base.py WorkflowOutput(BaseModel)
type WorkflowOutput struct {
    // Result 输出数据
    Result any
    // State 执行状态
    State WorkflowExecutionState
}
```

### 改动文件

- **新文件** `internal/agentcore/workflow/base.go`
- **新文件** `internal/agentcore/workflow/doc.go`
- **新文件** `internal/agentcore/workflow/base_test.go`

---

## P1-4：ReActAgent 中断处理（暂不修改）

`executeWorkflow` 中已保留 `WorkflowOutput.INPUT_REQUIRED` 检测，返回 `ExecuteResult{ToolMsg: nil}` 作为中断信号。ReActAgent 侧对 `ToolMsg == nil` 的中断处理暂不修改，等后续中断机制统一修复。

---

## 改动文件总览

| 文件 | 改动类型 | 涉及问题 |
|---|---|---|
| `internal/agentcore/single_agent/ability/ability_types.go` | 修改 | P0-1: BuildToolMessageContent 扩展 |
| `internal/agentcore/runner/runner.go` | **新增** | P0-2: RunAgent + RunWorkflow 全局函数 |
| `internal/agentcore/single_agent/ability/ability_manager.go` | 修改 | P0-2: executeAgent 改造; P0-3: executeTool 传 session; P1-1: executeWorkflow 改造 |
| `internal/agentcore/foundation/tool/base.go` | 修改 | P0-3: ToolCallOptions 增加 Session + WithToolSession |
| `internal/agentcore/foundation/tool/invoke_function.go` | 修改 | P0-3: 用户函数签名改为 `func(ctx, I, opts ...ToolOption) (O, error)` |
| `internal/agentcore/foundation/tool/stream_function.go` | 修改 | P0-3: 同上 |
| `internal/agentcore/foundation/tool/invoke_function_test.go` | 修改 | P0-3: 测试函数签名适配 |
| `internal/agentcore/foundation/tool/stream_function_test.go` | 修改 | P0-3: 同上 |
| `internal/agentcore/single_agent/interfaces/interface.go` | 修改 | P1-1: WorkflowOptions 扩展 |
| `internal/agentcore/workflow/base.go` | **新增** | P1-2: WorkflowOutput + WorkflowExecutionState |
| `internal/agentcore/workflow/doc.go` | **新增** | P1-2: 包文档 |
| `internal/agentcore/workflow/base_test.go` | **新增** | P1-2: 单元测试 |

---

## Python vs Go 执行流程对照图

### Tool 执行

```
Python:
  tool.invoke(tool_args, session=session)
  → _build_tool_message_content(result)  ← getattr 支持任意类型

Go (修复后):
  lt.Invoke(ctx, toolArgs, tool.WithToolSession(sess))
  → [LifecycleTool 透传 opts]
  → inner.Invoke(ctx, inputs, opts...)
  → f.fn(ctx, input, opts...)  ← 用户函数通过 opts 获取 session
  → BuildToolMessageContent(result)  ← map路径 + 反射路径 + result解包
```

### Agent 执行

```
Python:
  child_session_id = f"{session.id}:{tool_call.id}"
  tool_args["conversation_id"] = child_session_id
  child_session = create_agent_session(child_session_id, card=agent.card)
  propagate auto_confirm
  Runner.run_agent(agent, inputs, session=child_session)
    → _prepare_agent → pre_run
    → invoke(inputs, agent_session)
    → post_run

Go (修复后):
  childSessionID := fmt.Sprintf("%s:%s", sess.GetSessionID(), toolCall.ID)
  toolArgs["conversation_id"] = childSessionID
  childSession := session.CreateAgentSession(agentID, childSessionID)
  propagate auto_confirm (直接实现，不预留)
  runner.RunAgent(ctx, ag, toolArgs, childSession)
    → [PreRun/PostRun 预留注释]
    → agent.Invoke(ctx, inputs, interfaces.WithSession(sess))
```

### Workflow 执行

```
Python:
  workflow_session = session.create_workflow_session()
  workflow_context = context_engine.create_context(workflow_id, session)
  Runner.run_workflow(workflow, inputs, session, context)
  if WorkflowOutput.state == INPUT_REQUIRED:
    return (WorkflowOutput, None)  # 中断
  result = workflow_output.result
  ToolMessage(content=str(result))

Go (修复后):
  workflowSess = sess.CreateWorkflowSession()
  wfCtx, _ = am.contextEngine.CreateContext(ctx, wfID, sess)
  runner.RunWorkflow(ctx, wf, toolArgs, workflowSess, wfCtx)
  if WorkflowOutput.State == WorkflowExecutionStateInputRequired:
    return ExecuteResult{Result: wfOut, ToolMsg: nil}  # 中断信号
  actualResult = workflow_output.Result
  BuildToolMessageContent(actualResult)
```
