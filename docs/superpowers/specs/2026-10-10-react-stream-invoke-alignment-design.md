# Go ReActAgent Stream/Invoke 与 Python 语义对齐设计

## 背景

对照 Python `ReActAgent` (`openjiuwen/core/single_agent/agents/react_agent.py`) 严格检查 Go `ReActAgent` 的 stream 和 invoke 流程，发现若干语义差异。

## 差异总览与决策

| # | 差异 | 严重度 | 决定 |
|---|------|--------|------|
| 1 | Stream PreRun 对外部 agent session | 中 | **保持现状**（调用方负责） |
| 2 | 输入类型校验 | 低 | **无需修复**（Go 静态类型覆盖） |
| 3 | Query 校验位置 | 低 | **保持在 lifecycle 内**（对齐 Python） |
| 4 | System prompt 构建时机 | 中 | **修复** |
| 5 | Workflow 中断检测/恢复 | 中 | **保持 ⤵️ 占位** |
| 6 | @with_session 装饰器 | 低 | **无需修复**（语言范式差异） |
| 7 | CancelledError + cleanup | 中 | **修复**（用 defer） |
| 8 | is_agent_session 判断位置 | 低 | **无需修复**（功能等价） |
| 9 | stream_modes 参数 | 中 | **修复** |
| 10 | 工具执行整体错误 | 中 | **修复** |
| 11 | 迭代计数日志 | 低 | **修复** |
| 12 | 多模态工具结果 | 中 | **修复** |
| 13 | Preview messages 缺 system prompt | 低 | **修复** |
| 14 | LLM 诊断日志 | 低 | **修复** |
| 15 | context_processors + reloader | 中 | **修复**（Runner 部分用 ⤵️） |
| 16 | skill read_file 警告 | 低 | **⤵️ 占位** |
| 17 | usage_metadata 载荷格式 | 低 | **不修复** |

## 需修复的 9 项设计

### 差异 4：System prompt 构建移到 invoke 入口

**文件**：`react_invoke.go`

**位置**：`InvokeImpl` 中 `FireLifecycle` 内部，`initContext` 之后、reactLoop 之前

**修改**：对齐 Python L1317-1328，在 invoke 入口处构建 system prompt 并注册 identity section。

```go
// 对齐 Python L1317-1326: 构建 system prompt
renderedPrompt := a.promptBuilder.Build()
a.promptBuilder.AddSection(_IdentitySection, renderedPrompt, ...)
// ⤵️ Skill: a.updateSkillPromptBuilderSection(renderedPrompt)
tools, _ := a.getTools()
```

**注意**：`reactLoop` 中已有 `a.getTools()`，需避免重复调用或提取为参数。

### 差异 7：Cleanup 始终执行（用 defer）

**文件**：`react_invoke.go`

**位置**：`InvokeImpl`

**修改**：将 needCleanup 逻辑改为 `defer`，对齐 Python `try/finally` 语义。

```go
// 在 FireLifecycle 之前注册 defer
if needCleanup {
    defer func() {
        a.saveContexts(sess)
        if as, ok := sess.(*session.Session); ok {
            _ = as.CloseStream()
            _ = as.Commit(ctx)
        }
    }()
}

err := cbc.FireLifecycle(...)

if err != nil && ctx.Err() == context.Canceled {
    a.ClearContextMessages(sess)
}
if err != nil {
    return nil, err
}
```

### 差异 9：StreamModes 传递

**文件**：`react_invoke.go`（StreamImpl）

**位置**：`StreamImpl` 中自建 session 时

**修改**：从 AgentOptions 提取 StreamModes，创建带 modes 的 StreamWriterManager 传入 session。

```go
if sess == nil {
    agentOpts := interfaces.NewAgentOptions(opts...)
    modes := agentOpts.StreamModes

    emitter := stream.NewStreamEmitter()
    mgr := stream.NewStreamWriterManager(emitter, modes...)
    newSess := session.NewSession(
        session.WithSessionID(sessionID),
        session.WithStreamWriterManager(mgr),
    )
    if err := newSess.PreRun(ctx, inputs); err != nil {
        return nil, err
    }
    sess = newSess
    opts = append(opts, interfaces.WithSession(sess))
    needCleanup = true
}
```

### 差异 10：工具执行整体错误终止循环

**文件**：`react_invoke.go`

**位置**：`reactLoop` 中 `executeToolCalls` 后的错误处理

**修改**：

```go
results, err := a.executeToolCalls(...)
if err != nil {
    return nil, fmt.Errorf("工具执行失败: %w", err)
}
```

### 差异 11：迭代计数日志

**文件**：`react_invoke.go`

**位置**：`reactLoop` 循环体开头

**修改**：

```go
for iteration := startIteration; iteration < maxIter; iteration++ {
    logger.Info(logComponent).
        Int("iteration", iteration+1).
        Int("max_iterations", maxIter).
        Msg("ReAct 迭代")
    // ...
}
```

### 差异 12：多模态工具结果消息

**文件**：`react_invoke.go`（executeToolCalls 末尾）

**修改**：在添加 ToolMessage 之后，检查工具结果中是否包含图片数据，构建多模态 UserMessage。

```go
// 对齐 Python: _build_multimodal_tool_results_message
multimodalMsg := a.buildMultimodalToolResultsMessage(results)
if multimodalMsg != nil && modelCtx != nil {
    _, _ = modelCtx.AddMessages(ctx, multimodalMsg)
}
```

新增方法 `buildMultimodalToolResultsMessage`，对照 Python `_build_multimodal_tool_results_message` 实现。

### 差异 13：Preview messages 加 system prompt 前缀

**文件**：`react_model_call.go`

**位置**：`callModel` 中构建 ModelCallInputs

**修改**：对齐 Python `_build_preview_messages`，在 preview messages 前插入 system prompt。

```go
previewMsgs := make([]llmschema.BaseMessage, 0)
previewPrompt := a.promptBuilder.Build()
if previewPrompt != "" {
    previewMsgs = append(previewMsgs, llmschema.NewSystemMessage(previewPrompt))
}
if modelCtx != nil {
    previewMsgs = append(previewMsgs, modelCtx.GetMessages(0, true)...)
}
```

### 差异 14：LLM 请求/响应诊断日志

**文件**：`react_model_call.go`

**位置**：`railedModelCall` 中 LLM 调用前后

**修改**：对照 Python `log_llm_request` / `log_llm_response`，新增辅助函数并在调用前后记录。

### 差异 15：initContext 使用 context_processors + reloader

**文件**：`react_helpers.go`

**位置**：`initContext`

**修改**：

```go
func (a *ReActAgent) initContext(ctx context.Context, sess sessioninterfaces.SessionFacade) (ceinterface.ModelContext, error) {
    if a.contextEngine == nil {
        return nil, nil
    }

    // 1. 传递 context_processors
    var opts []ceinterface.CreateContextOption
    if a.config != nil && len(a.config.ContextProcessors) > 0 {
        opts = append(opts, ceinterface.WithProcessors(a.config.ContextProcessors))
    }
    modelCtx, err := a.contextEngine.CreateContext(ctx, "default_context", sess, opts...)
    if err != nil {
        return nil, err
    }

    // 2. reloader tool 动态注册/注销
    reloaderTool := modelCtx.ReloaderTool()
    am := a.getAbilityManager()
    if a.config != nil && a.config.ContextEngineConfig.EnableReload {
        if am != nil && reloaderTool != nil {
            am.Add(reloaderTool)
        }
        // ⤵️ Runner.resource_mgr 注册
    } else {
        if am != nil && reloaderTool != nil {
            am.Remove(reloaderTool.Card().Name)
        }
    }

    return modelCtx, nil
}
```

## 修改文件清单

| 文件 | 涉及差异 |
|------|---------|
| `internal/agentcore/single_agent/agents/react_invoke.go` | 4, 7, 9, 10, 11, 12 |
| `internal/agentcore/single_agent/agents/react_helpers.go` | 15 |
| `internal/agentcore/single_agent/agents/react_model_call.go` | 13, 14 |
