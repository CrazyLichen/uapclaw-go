# ReActAgent 回调骨架重构与输入输出变换修复

## 背景

ReActAgent 当前使用 `WarpBaseAgent` + `AgentInvoker` 接口实现全局回调骨架
（emit_before → transform_io → invokeImpl → transform_io → emit_after），
等价于 Python `_AgentMeta` 元类的装饰器链。但此模式存在以下问题：

1. **WarpBaseAgent/AgentInvoker 样板代码过多**：Go 无元类/装饰器，
   用 `AgentInvoker` 接口 + `SetInvoker()` 绕过内嵌结构体虚分发限制，
   子类必须写 `base.SetInvoker(agent)` 样板代码。
2. **InvokeImpl 闭包内用局部变量 `invokeInputs` 而非 `cbc.Inputs()` 重新取值**：
   `before_invoke` 钩子可能修改 `cbc.inputs`（如替换整个 InvokeInputs 对象），
   但闭包内读的是 FireLifecycle 外创建的局部变量，无法感知变更。
   Python 在 `lifecycle()` 体内用 `ctx.inputs.query` 从 ctx 重新取值。
3. **错误判断冗余**：`ctx.Err() == context.Canceled` 和 `err != nil` 两个
   if 可以合并为一个。

## 设计

### 1. 去掉 WarpBaseAgent，回调骨架直接写入 Invoke/Stream

**核心思路**：不定义额外的包装函数（如 WrapInvoke/WrapStream），
也不定义 EmitAgentInvokeBefore/TransformAgentInvokeInput 等独立函数，
而是**直接在 ReActAgent.Invoke/Stream 方法体内写回调骨架调用**，
与 FireLifecycle、RailExecutor.Execute 的调用风格一致——都是方法内显式调用，
不需要额外的抽象层。

**WarpBaseAgent 简化为 BaseAgent**：只保留配置/管理能力，
不再持有 `AgentInvoker` 接口和 `invoker` 字段，不再实现 Invoke/Stream 骨架。

#### 1.1 BaseAgent 简化

```go
// BaseAgent Agent 基础配置/管理容器。
// 不实现 Invoke/Stream，子类自行实现并在方法体内调用回调骨架。
//
// 对应 Python: BaseAgent（不含 _AgentMeta 装饰逻辑）
type BaseAgent struct {
    card            *agentschema.AgentCard
    config          interfaces.AgentConfig
    abilityManager  *ability.AbilityManager
    callbackManager *rail.AgentCallbackManager
}
```

去掉的字段和方法：
- `invoker AgentInvoker` 字段
- `AgentInvoker` 接口
- `SetInvoker()` 方法
- `Invoke()` / `Stream()` 方法（骨架逻辑移入子类）

保留的方法：
- `NewBaseAgent()` 构造
- `Configure()` / `Card()` / `Config()` / `AbilityManager()` / `CallbackManager()`
- `AgentID()` / `RegisterCallback()` / `RegisterRail()` / `UnregisterRail()`

#### 1.2 ReActAgent.Invoke 直接写骨架

```go
func (a *ReActAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...interfaces.AgentOption) (any, error) {
    fw := callback.GetCallbackFramework()
    agentOpts := interfaces.NewAgentOptions(opts...)

    // ① transform_io 输入变换（对齐 Python transform_io input_fn）
    if transformed := fw.TransformAgentIOInput(ctx, callback.GlobalAgentInvokeInput, inputs); transformed != nil {
        if v, ok := transformed.(map[string]any); ok { inputs = v }
    }

    // ② emit_before: 触发全局 AgentInvokeInput 事件
    fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
        Event: callback.GlobalAgentInvokeInput, AgentID: a.card.ID,
        AgentName: a.card.Name, Inputs: inputs, Session: agentOpts.Session,
    })

    // ③ 执行真实逻辑
    result, err := a.invokeImpl(ctx, inputs, opts...)
    if err != nil {
        // context.Canceled 时清除上下文消息后返回错误
        if ctx.Err() == context.Canceled { a.ClearContextMessages(agentOpts.Session) }
        if _, ok := err.(*exception.BaseError); ok { return nil, err }
        return nil, exception.NewBaseError(exception.StatusAgentControllerRuntimeError,
            exception.WithCause(err))
    }

    // ④ transform_io 输出变换
    result = fw.TransformAgentIOOutput(ctx, callback.GlobalAgentInvokeOutput, result)

    // ⑤ emit_after: 触发全局 AgentInvokeOutput 事件
    fw.TriggerGlobalAgent(ctx, &callback.GlobalAgentEventData{
        Event: callback.GlobalAgentInvokeOutput, AgentID: a.card.ID,
        AgentName: a.card.Name, Result: result,
    })

    return result, nil
}
```

#### 1.3 ReActAgent.Stream 同理

Stream 的 per-item channel 包装也直接写在 `Stream()` 方法内，
与当前 WarpBaseAgent.Stream 的 channel 包装逻辑一致，只是从 `w.invoker.StreamImpl` 改为 `a.streamImpl`。

#### 1.4 invokeImpl / streamImpl 降级为非导出方法

原来的 `InvokeImpl`/`StreamImpl`（满足 AgentInvoker 接口）降级为
`invokeImpl`/`streamImpl`（非导出），因为不再需要接口满足。
它们只被 `Invoke`/`Stream` 内部调用。

### 2. InvokeImpl 闭包内改用 cbc.Inputs() 重新取值

**问题**：`before_invoke` 钩子可能通过 `cbc.SetInputs()` 替换整个 inputs 对象，
但闭包内用局部变量 `invokeInputs` 无法感知。

**修复**：在 `FireLifecycle` 闭包内，从 `cbc.Inputs()` 重新取值，
对齐 Python `ctx.inputs.query`。

```go
err := cbc.FireLifecycle(rail.CallbackBeforeInvoke, rail.CallbackAfterInvoke, func() error {
    // 从 cbc 重新取 inputs（对齐 Python: user_input = ctx.inputs.query）
    curInputs, ok := cbc.Inputs().(*rail.InvokeInputs)
    if !ok || curInputs == nil { curInputs = invokeInputs }

    if curInputs.Query.PlainText() == "" && !curInputs.Query.IsInteractiveInput() {
        return fmt.Errorf("input must contain 'query'")
    }

    // HITL 恢复
    if hitlState != nil {
        ...
        res.ResumeContext.UserInput = curInputs.Query  // 用 curInputs
        ...
    } else {
        plainText := curInputs.Query.PlainText()  // 用 curInputs
        ...
    }

    if curInputs.Result == nil {  // 用 curInputs
        result, loopErr = a.reactLoop(...)
    }
    return loopErr
})
```

FireLifecycle 外的返回也同理：
```go
// 合并错误判断 + 从 cbc 取结果
if err != nil {
    if ctx.Err() == context.Canceled { a.ClearContextMessages(sess) }
    return nil, err
}

// 对齐 Python: return ctx.extra.get("invoke_result", invoke_inputs.result)
if invokeResult, ok := cbc.Extra()["invoke_result"]; ok {
    if r, ok2 := invokeResult.(map[string]any); ok2 { return r, nil }
}
if curInputs, ok := cbc.Inputs().(*rail.InvokeInputs); ok && curInputs.Result != nil {
    return curInputs.Result, nil
}
return result, nil
```

### 3. 合并冗余错误判断

**当前代码**：
```go
// context.Canceled 时清除上下文消息
if err != nil && ctx.Err() == context.Canceled {
    a.ClearContextMessages(sess)
}
if err != nil {
    return nil, err
}
```

**修复后**：
```go
if err != nil {
    if ctx.Err() == context.Canceled { a.ClearContextMessages(sess) }
    return nil, err
}
```

同样的问题也出现在 `WarpBaseAgent.Stream` 中，统一合并。

## 变更清单

| 文件 | 变更 |
|------|------|
| `single_agent/base.go` | WarpBaseAgent → BaseAgent；去掉 AgentInvoker 接口、invoker 字段、SetInvoker、Invoke、Stream 方法 |
| `single_agent/interfaces/interface.go` | BaseAgent 接口如有 Invoke/Stream 签名需同步调整 |
| `agents/react_agent.go` | 内嵌 BaseAgent（非 WarpBaseAgent）；去掉 SetInvoker 调用；新增 Invoke/Stream 公开方法（内含骨架） |
| `agents/react_invoke.go` | InvokeImpl → invokeImpl（非导出）；闭包内改用 cbc.Inputs() 取值；合并错误判断 |
| `agents/react_model_call.go` | 无变更（callModel/railedModelCall 不受影响） |
| 各 doc.go | 同步更新文件目录 |

## 对齐验证

| Python 行为 | Go 修复后 | 状态 |
|-------------|----------|------|
| `_AgentMeta` 自动装饰 invoke/stream | ReActAgent.Invoke/Stream 内显式写骨架 | ✅ |
| `ctx.inputs.query` 从 ctx 重新取值 | `cbc.Inputs().(*InvokeInputs).Query` | ✅ |
| `ctx.extra.get("invoke_result", invoke_inputs.result)` | `cbc.Extra()["invoke_result"]` 优先 + `cbc.Inputs().(*InvokeInputs).Result` 次选 | ✅ |
| `@rail` 装饰器输入输出变换 | RailExecutor + cbc.SetInputs/写回 Response | ✅（已对齐，无变更） |
| `lifecycle()` 输入保存恢复 | FireLifecycle savedInputs 保存恢复 | ✅（已对齐，无变更） |
