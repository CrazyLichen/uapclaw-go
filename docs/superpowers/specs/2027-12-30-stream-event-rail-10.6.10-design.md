# StreamEvent Rail（10.6.10）设计文档

> 全量一比一对齐 Python `jiuwenswarm/agents/harness/common/rails/stream_event_rail.py`

## 1. 概述

`JiuClawStreamEventRail` 是 Swarm 侧最核心的 Rail 之一，承担三重职责：

1. **流式事件发射器** — 将 Agent 内部状态实时推送到前端（tool_call/tool_result/tool_update/context.usage/todo.updated/chat.ask_user_question）
2. **暂停/中止检查点** — 支持 Gateway 侧的 pause/resume/cancel/supplement 中断请求
3. **上下文修复** — 修复 LLM 输出的不完整 tool context + 畸形 JSON 参数

## 2. 在 Agent 会话流程中的位置

```
用户消息 → Gateway → AgentServer → DeepAdapter.processMessage
                                      │
                                      ├── ResetAbort(sessionID)  ← StreamEvent 入口清理
                                      │
                                      ▼
                                  DeepAgent.ReActLoop
                                      │
                    ┌─────────────────┼─────────────────┐
                    │                 │                  │
              BeforeModelCall   BeforeToolCall    AfterToolCall
                    │                 │                  │
                    ▼                 ▼                  ▼
              ┌─────────── JiuClawStreamEventRail ───────────┐
              │  暂停检查点     暂停检查点                    │
              │  abort→error   abort→error                  │
              │  上下文修复     发射tool_call事件              │
              │                 发射tool_update(in_progress)  │
              │                 记录inflight tool             │
              │                                               │
              │  AfterModelCall          AfterToolCall        │
              │  发射context.usage       发射tool_result      │
              │                          发射ask_user_q       │
              │                          发射todo.updated     │
              └──────────────────────────────────────────────┘
                    │
                    ▼ 全部事件通过 Session.WriteStream → OutputSchema → AgentResponseChunk → WebSocket → 前端
```

## 3. 技术选型决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 文件位置 | `internal/swarm/agents/harness/common/rails/stream_event_rail.go` | 与 Python 路径一致，与其他 Swarm Rails 同目录 |
| 暂停/中止机制 | `sync.Cond` | 语义最接近 `asyncio.Event`，goroutine-safe，不泄漏 |
| JSON 修复 | 新增 `EnsureJSONArguments` + 引入 `RealAlexandreAI/json-repair` | 不动现有 `RepairToolArgumentsJSON`，新函数含完整三阶段修复 |
| 上下文修复 | 全量实现 `_fixIncompleteToolContext` | 保障中断后恢复的消息上下文完整性 |
| json_repair 库 | `github.com/RealAlexandreAI/json-repair` | 383 stars，零依赖，Go 1.21+，API 简洁 |

## 4. 文件结构

```
internal/swarm/agents/harness/common/rails/
├── stream_event_rail.go          # JiuClawStreamEventRail 结构体 + 核心逻辑（新增）
├── stream_event_rail_test.go     # 单元测试（新增）
├── stream_event_helpers.go       # 辅助函数（新增）：boolishFalse/True, nonzeroExit, inferToolResultError 等
├── stream_event_helpers_test.go  # 辅助函数单元测试（新增）
├── stream_event_emit.go          # 流式事件发射方法（新增）：emitToolCall/Result/Update/ContextUsage/TodoUpdated/AskUserQuestion
├── stream_event_emit_test.go     # 发射方法单元测试（新增）
├── stream_event_context.go       # 上下文修复方法（新增）：fixIncompleteToolContext/ensureJSONArguments/fixMissingQuotes
├── stream_event_context_test.go  # 上下文修复单元测试（新增）
├── doc.go                        # 更新文件目录
└── ... (已有的 avatar_rail.go 等)
```

拆分为多个文件的理由：Python 源码 914 行，移植到 Go 预计 1200+ 行（Go 的类型声明/错误处理更冗长），拆分避免单文件过大。

## 5. 核心结构体

```go
// JiuClawStreamEventRail 流事件护栏 — 对齐 Python JiuClawStreamEventRail(priority=80)
//
// 三重职责：
// 1. 流式事件发射：tool_call/tool_result/tool_update/context.usage/todo.updated/ask_user_question
// 2. 暂停/中止检查点：在 BeforeModelCall/BeforeToolCall 中阻塞等待恢复或中止
// 3. 上下文修复：修复不完整的 tool context + 畸形 JSON 参数
type JiuClawStreamEventRail struct {
    rails.DeepAgentRail

    // deepAgent 主 Agent 实例引用（用于获取 language/workspace/todo tool）
    deepAgent agentinterfaces.BaseAgent

    // ── Per-session 状态（key=normalizedSessionID）──
    mu              sync.Mutex
    sessionStates   map[string]*streamSessionState
}

// streamSessionState 每个 session 的暂停/中止/追踪状态
type streamSessionState struct {
    // ── 暂停/中止 ──
    paused   bool
    aborted  bool
    cond     *sync.Cond   // 基于 mu 的条件变量，实现 pause/resume/abort

    // ── 会话上下文 ──
    conversationID string                    // 原始 conversation_id（非 "default" 哨兵）
    mainSession    sessioninterfaces.SessionFacade  // 主 Agent 的 Session 引用

    // ── 工具调用追踪 ──
    inflightToolCalls   map[string]*inflightToolInfo  // key=tool_call_id
    cancelledToolResults []map[string]any              // 中断时收集的已取消工具信息
}

// inflightToolInfo 正在执行中的工具调用信息
type inflightToolInfo struct {
    toolCall  *llm_schema.ToolCall
    session   sessioninterfaces.SessionFacade
    sessionID string
}
```

### Per-session 状态键

Python 使用 `dict[str, bool]` / `dict[str, asyncio.Event]` 等多个字典，key 为 session_id。Go 侧统一为一个 `streamSessionState` 结构体，用一个 `map[string]*streamSessionState` 管理，减少碎片化。

### `sync.Cond` 暂停/中止机制

```go
// checkpoint 检查点 — 在 BeforeModelCall/BeforeToolCall 中调用
// 对齐 Python: await self._get_pause_event(sid).wait() + if self._abort_requested.get(sid): raise CancelledError
func (r *JiuClawStreamEventRail) checkpoint(ctx context.Context, sid string) error {
    r.mu.Lock()
    state := r.getOrCreateState(sid)
    for state.paused && !state.aborted {
        state.cond.Wait()  // 阻塞直到 resume 或 abort
    }
    aborted := state.aborted
    r.mu.Unlock()

    if aborted {
        return fmt.Errorf("Agent abort requested")  // 对齐 Python: raise asyncio.CancelledError
    }
    return nil
}

// Pause 暂停 session — 对齐 Python: pause(session_id)
func (r *JiuClawStreamEventRail) Pause(sessionID string) {
    r.mu.Lock()
    state := r.getOrCreateState(sessionID)
    state.paused = true
    r.mu.Unlock()
    // 不需要 cond.Broadcast — 暂停时无人在等
}

// Resume 恢复 session — 对齐 Python: resume(session_id)
func (r *JiuClawStreamEventRail) Resume(sessionID string) {
    r.mu.Lock()
    state := r.getOrCreateState(sessionID)
    state.aborted = false
    state.paused = false
    state.cond.Broadcast()
    r.mu.Unlock()
}

// Abort 中止 session — 对齐 Python: abort(session_id)
func (r *JiuClawStreamEventRail) Abort(sessionID string) {
    r.mu.Lock()
    state := r.getOrCreateState(sessionID)
    state.aborted = true
    state.paused = false
    state.cond.Broadcast()
    r.mu.Unlock()
}

// ResetAbort 清除中止标志 — 对齐 Python: reset_abort(session_id)
func (r *JiuClawStreamEventRail) ResetAbort(sessionID string) {
    r.mu.Lock()
    state := r.getOrCreateState(sessionID)
    state.aborted = false
    r.mu.Unlock()
}

// ResetForNewTask 解除暂停但保留 abort 标志 — 对齐 Python: reset_for_new_task(session_id)
func (r *JiuClawStreamEventRail) ResetForNewTask(sessionID string) {
    r.mu.Lock()
    state := r.getOrCreateState(sessionID)
    state.paused = false
    state.conversationID = ""
    state.mainSession = nil
    state.cond.Broadcast()
    r.mu.Unlock()
    // 注意：不清除 aborted — 必须等到下次 process_message 入口调 ResetAbort
}

// CleanupSession 清除所有 per-session 状态 — 对齐 Python: cleanup_session(session_id)
func (r *JiuClawStreamEventRail) CleanupSession(sessionID string) {
    r.mu.Lock()
    delete(r.sessionStates, sessionID)
    r.mu.Unlock()
}
```

## 6. AgentRail 钩子方法

### 6.1 BeforeInvoke

```go
// 对齐 Python: before_invoke
// 捕获 conversation_id + session，写入 cbc.Extra 供后续钩子使用
func (r *JiuClawStreamEventRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
    inputs, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs)
    if !ok {
        return nil
    }
    // 子 Agent 的 session 为 nil，只有主 Agent 才记录
    if cbc.Session() == nil {
        return nil
    }
    rawConvID := inputs.ConversationID
    sid := rawConvID
    if sid == "" {
        sid = "default"
    }

    r.mu.Lock()
    state := r.getOrCreateState(sid)
    if rawConvID != "" {
        state.conversationID = rawConvID
    }
    state.mainSession = cbc.Session()
    r.mu.Unlock()

    // 写入 extra，后续钩子通过 _SID_KEY 查找 per-session 状态
    cbc.Extra()[sidKey] = sid
    return nil
}
```

### 6.2 BeforeModelCall

```go
// 对齐 Python: before_model_call
// 1. 暂停/中止检查点
// 2. 上下文修复（_fixIncompleteToolContext）
func (r *JiuClawStreamEventRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
    sid := r.resolveSID(cbc, cbc.Session())
    // 暂停/中止检查点
    if err := r.checkpoint(ctx, sid); err != nil {
        return err
    }
    // 上下文修复
    if cbc.ModelContext() != nil {
        if err := r.fixIncompleteToolContext(ctx, cbc.ModelContext()); err != nil {
            logger.Warn(logComponent).Err(err).Msg("修复不完整工具上下文失败")
        }
    }
    return nil
}
```

### 6.3 AfterModelCall

```go
// 对齐 Python: after_model_call
// 发射 context.usage 事件
func (r *JiuClawStreamEventRail) AfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
    return r.emitContextUsage(ctx, cbc)
}
```

### 6.4 BeforeToolCall

```go
// 对齐 Python: before_tool_call
// 1. 暂停/中止检查点
// 2. 发射 tool_call 事件
// 3. 发射 tool_update(in_progress) 事件
// 4. 记录 inflight tool
func (r *JiuClawStreamEventRail) BeforeToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
    sid := r.resolveSID(cbc, cbc.Session())
    // 暂停/中止检查点
    if err := r.checkpoint(ctx, sid); err != nil {
        return err
    }

    session := cbc.Session()
    inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
    if !ok || session == nil {
        return nil
    }
    tc := inputs.ToolCall()
    if tc == nil {
        return nil
    }

    // 发射事件
    r.emitToolCall(ctx, session, tc)
    r.emitToolUpdate(ctx, session, tc, "in_progress")

    // 记录 inflight tool
    tcID := tc.ID
    if tcID != "" {
        r.mu.Lock()
        state := r.getOrCreateState(sid)
        state.inflightToolCalls[tcID] = &inflightToolInfo{
            toolCall:  tc,
            session:   session,
            sessionID: sid,
        }
        r.mu.Unlock()
    }
    return nil
}
```

### 6.5 AfterToolCall

```go
// 对齐 Python: after_tool_call
// 1. 从 inflight 移除
// 2. 发射 tool_result 事件
// 3. 发射 ask_user_question（如果是 ask_user 中断）
// 4. 发射 todo.updated（如果是 todo 工具）
func (r *JiuClawStreamEventRail) AfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
    session := cbc.Session()
    inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
    if !ok || session == nil {
        return nil
    }
    tc := inputs.ToolCall()
    tcID := ""
    if tc != nil {
        tcID = tc.ID
    }

    // 从 inflight 移除
    if tcID != "" {
        r.mu.Lock()
        sid := r.resolveSID(cbc, session)
        state := r.getOrCreateState(sid)
        delete(state.inflightToolCalls, tcID)
        r.mu.Unlock()
    }

    // 发射 tool_result
    r.emitToolResult(ctx, session, tc, inputs.ToolResult())

    // 发射 ask_user_question（如果是 ask_user 中断）
    r.emitAskUserQuestionIfInterrupted(ctx, session, tc, inputs.ToolName(), inputs.ToolResult(), cbc.Exception())

    // 发射 todo.updated
    toolName := inputs.ToolName()
    sid := r.resolveSID(cbc, session)
    r.mu.Lock()
    state := r.getOrCreateState(sid)
    convID := state.conversationID
    r.mu.Unlock()
    if convID != "" && todoToolNames[toolName] {
        r.emitTodoUpdated(ctx, session, convID)
    }
    return nil
}
```

### 6.6 OnModelException

```go
// 对齐 Python: on_model_exception
// 上下文修复
func (r *JiuClawStreamEventRail) OnModelException(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
    if cbc.ModelContext() != nil {
        logger.Info(logComponent).Msg("模型异常后尝试修复上下文")
        if err := r.fixIncompleteToolContext(ctx, cbc.ModelContext()); err != nil {
            logger.Warn(logComponent).Err(err).Msg("修复不完整工具上下文失败")
        }
    }
    return nil
}
```

## 7. 流式事件发射方法

所有事件通过 `Session.WriteStream(ctx, OutputSchema{...})` 写入，对齐 Python `session.write_stream(OutputSchema(...))`。

### 7.1 事件类型映射

| Python `OutputSchema.type` | Go `stream.OutputSchema.Type` | payload 结构 |
|---|---|---|
| `"tool_call"` | `"tool_call"` | `{tool_call: {name, arguments, tool_call_id}}` |
| `"tool_result"` | `"tool_result"` | `{tool_result: {tool_name, tool_call_id, result, raw_output?, success?, status?, is_error?}}` |
| `"tool_update"` | `"tool_update"` | `{tool_update: {tool_name, tool_call_id, arguments, status}}` |
| `"context.usage"` | `"context.usage"` | `{rate, context_max, tokens_used}` |
| `"todo.updated"` | `"todo.updated"` | `{todos: [...]}` |
| `"chat.ask_user_question"` | `"chat.ask_user_question"` | `{event_type, request_id, questions, source}` |

### 7.2 emitToolCall

```go
func (r *JiuClawStreamEventRail) emitToolCall(ctx context.Context, session sessioninterfaces.SessionFacade, tc *llm_schema.ToolCall) {
    payload := map[string]any{
        "tool_call": map[string]any{
            "name":         tc.Name,
            "arguments":    tc.Arguments,
            "tool_call_id": tc.ID,
        },
    }
    _ = session.WriteStream(ctx, stream.OutputSchema{
        Type:    "tool_call",
        Index:   0,
        Payload: payload,
    })
}
```

### 7.3 emitToolResult

```go
func (r *JiuClawStreamEventRail) emitToolResult(ctx context.Context, session sessioninterfaces.SessionFacade, tc *llm_schema.ToolCall, result any) {
    rawOutput := structuredToolResultPayload(result)
    toolResultPayload := map[string]any{
        "tool_name":     toolCallName(tc),
        "tool_call_id":  toolCallID(tc),
        "result":        truncateString(fmt.Sprintf("%v", result), 60000),
    }
    if rawOutput != nil {
        toolResultPayload["raw_output"] = rawOutput
    }
    errorState := inferToolResultError(rawOutput)
    if errorState != nil {
        toolResultPayload["success"] = !*errorState
        if *errorState {
            toolResultPayload["status"] = "error"
            toolResultPayload["is_error"] = true
        }
    }
    _ = session.WriteStream(ctx, stream.OutputSchema{
        Type:    "tool_result",
        Index:   0,
        Payload: map[string]any{"tool_result": toolResultPayload},
    })
}
```

### 7.4 emitToolUpdate

```go
func (r *JiuClawStreamEventRail) emitToolUpdate(ctx context.Context, session sessioninterfaces.SessionFacade, tc *llm_schema.ToolCall, status string) {
    if status == "" {
        status = "in_progress"
    }
    payload := map[string]any{
        "tool_update": map[string]any{
            "tool_name":     toolCallName(tc),
            "tool_call_id":  toolCallID(tc),
            "arguments":     toolCallArguments(tc),
            "status":        status,
        },
    }
    _ = session.WriteStream(ctx, stream.OutputSchema{
        Type:    "tool_update",
        Index:   0,
        Payload: payload,
    })
}
```

### 7.5 emitContextUsage

```go
func (r *JiuClawStreamEventRail) emitContextUsage(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
    session := cbc.Session()
    if session == nil {
        return nil
    }
    modelCtx := cbc.ModelContext()
    if modelCtx == nil {
        return nil
    }

    // 获取 model_name
    var modelName string
    // 从 agent._config.model_name 获取（对齐 Python: ctx.agent._config.model_name）

    // 解析 context_max
    rawTotalTokens := contextutils.ResolveContextMax(modelName, 0, nil)

    // 从 response.usage_metadata 获取当前 token 使用量
    var currentContextTokens int
    inputs, ok := cbc.Inputs().(*agentinterfaces.ModelCallInputs)
    if ok && inputs != nil && inputs.Response != nil && inputs.Response.UsageMetadata != nil {
        currentContextTokens = inputs.Response.UsageMetadata.TotalTokens
    }

    var rate float64
    if rawTotalTokens != 0 {
        rate = float64(currentContextTokens) / float64(rawTotalTokens) * 100
    }

    _ = session.WriteStream(ctx, stream.OutputSchema{
        Type:  "context.usage",
        Index: 0,
        Payload: map[string]any{
            "rate":         rate,
            "context_max":  rawTotalTokens,
            "tokens_used":  currentContextTokens,
        },
    })
    return nil
}
```

### 7.6 emitAskUserQuestionIfInterrupted

```go
func (r *JiuClawStreamEventRail) emitAskUserQuestionIfInterrupted(
    ctx context.Context,
    session sessioninterfaces.SessionFacade,
    tc *llm_schema.ToolCall,
    toolName string,
    result any,
    exception error,
) {
    if strings.TrimSpace(toolName) != "ask_user" {
        return
    }
    interrupt := extractToolInterrupt(result)
    if interrupt == nil {
        interrupt = extractToolInterrupt(exception)
    }
    if interrupt == nil {
        return
    }
    payload := askUserQuestionPayloadFromInterrupt(tc, interrupt)
    if payload == nil {
        return
    }
    _ = session.WriteStream(ctx, stream.OutputSchema{
        Type:    "chat.ask_user_question",
        Index:   0,
        Payload: payload,
    })
}
```

### 7.7 emitTodoUpdated

```go
func (r *JiuClawStreamEventRail) emitTodoUpdated(ctx context.Context, session sessioninterfaces.SessionFacade, conversationID string) {
    todoTool := r.getTodoTool()
    if todoTool == nil {
        return
    }
    todosData, err := todoTool.LoadTodos(conversationID)
    if err != nil {
        logger.Debug(logComponent).Err(err).Msg("加载 TODO 列表失败")
        return
    }
    todos := formatTodosForFrontend(todosData)
    _ = session.WriteStream(ctx, stream.OutputSchema{
        Type:    "todo.updated",
        Index:   0,
        Payload: map[string]any{"todos": todos},
    })
}
```

## 8. 上下文修复方法

### 8.1 fixIncompleteToolContext

对齐 Python `_fix_incomplete_tool_context`（~90行），核心逻辑：

```
1. PopMessages 获取全部消息
2. 遍历消息列表，维护 toolIDCache（待匹配的 tool_call ID 列表）
3. 遇到 AssistantMessage：
   - 如果 toolIDCache 非空 → 为每个未匹配的 tool_call 插入占位 ToolMessage
   - 记录新的 tool_calls 到 toolIDCache
4. 遇到 ToolMessage：
   - 如果 toolIDCache 非空且 tool_call_id 匹配 → 从 cache 移除，正常 AddMessages
   - 否则 → 存入 toolMessageCache
5. 遇到其他消息：
   - 如果 toolIDCache 非空 → 插入占位 ToolMessage
6. 遍历结束后如果 toolIDCache 非空 → 插入占位 ToolMessage
```

Go 实现需要处理 `BaseMessage` 接口的类型判断（`AssistantMessage` / `ToolMessage`），以及对 `ToolCall.Arguments` 调用 `ensureJSONArguments` 修复。

### 8.2 ensureJSONArguments

对齐 Python `_ensure_json_arguments`，四阶段修复：

```
1. dict → json.Marshal（Go 侧 arguments 是 string，此阶段简化）
2. json.Unmarshal 直接解析 → 成功则返回原文
3. `json_repair.RepairJSON(arguments)` → 修复后 `json.Unmarshal` 验证（`github.com/RealAlexandreAI/json-repair` 包）
4. fixMissingQuotes 正则修复 → json.Unmarshal 验证
兜底：返回 "{}"
```

### 8.3 fixMissingQuotes

对齐 Python `_fix_missing_quotes`，三种正则修复：

1. Windows 路径：`{"path": D:/work/file.txt}` → `{"path": "D:/work/file.txt"}`
2. 缺结束引号：`{"query": hello}` → `{"query": "hello"}`
3. 缺键引号：`{query: "hello"}` → `{"query": "hello"}`

## 9. 辅助函数

### 9.1 模块级辅助函数（stream_event_helpers.go）

| 函数 | 对齐 Python | 说明 |
|------|------------|------|
| `structuredToolResultPayload(result any) any` | `_structured_tool_result_payload` | dict/list 直接返回，其他返回 nil |
| `parseToolCallArguments(tc *ToolCall) map[string]any` | `_parse_tool_call_arguments` | 解析 tool_call.Arguments 为 map |
| `extractToolInterrupt(value any) any` | `_extract_tool_interrupt` | 递归查找 ToolInterruptException |
| `askUserQuestionPayloadFromInterrupt(tc, interrupt) map[string]any` | `_ask_user_question_payload_from_interrupt` | 构造 ask_user_question payload |
| `boolishFalse(value any) bool` | `_boolish_false` | false/"false"/"0"/"no" |
| `boolishTrue(value any) bool` | `_boolish_true` | true/"true"/"1"/"yes" |
| `nonzeroExit(value any) *bool` | `_nonzero_exit` | 非0退出码判断 |
| `inferToolResultError(value any) *bool` | `_infer_tool_result_error` | 推断工具结果是否错误 |
| `truncateString(s string, maxLen int) string` | Python `str(result)[:60000]` | 截断字符串 |

### 9.2 常量

```go
const (
    sidKey           = "__jiuwenswarm_session_id__"  // 对齐 Python: _SID_KEY
    defaultSessionID = "default"
)

var todoToolNames = map[string]bool{
    "todo_create": true,
    "todo_get":    true,
    "todo_list":   true,
    "todo_modify": true,
}
```

## 10. CancelledTool 工具信息收集

对齐 Python 的 `collect_cancelled_tool_updates` / `get_cancelled_tool_results` / `clear_cancelled_tool_results`：

```go
// CollectCancelledToolUpdates 收集中断时的工具调用信息 — 对齐 Python: collect_cancelled_tool_updates
func (r *JiuClawStreamEventRail) CollectCancelledToolUpdates(sessionID string) {
    r.mu.Lock()
    state := r.getOrCreateState(sessionID)
    for tcID, info := range state.inflightToolCalls {
        if sessionID != "" && info.sessionID != sessionID {
            continue
        }
        if info.toolCall == nil {
            continue
        }
        state.cancelledToolResults = append(state.cancelledToolResults, map[string]any{
            "tool_name":     info.toolCall.Name,
            "tool_call_id":  tcID,
            "result":        "[Interrupted] Tool execution cancelled by user.",
            "status":        "error",
        })
        delete(state.inflightToolCalls, tcID)
    }
    count := len(state.cancelledToolResults)
    r.mu.Unlock()
    logger.Info(logComponent).Int("count", count).Str("session_id", sessionID).Msg("收集已取消工具信息")
}

// GetCancelledToolResults 获取中断收集的工具结果 — 对齐 Python: get_cancelled_tool_results
func (r *JiuClawStreamEventRail) GetCancelledToolResults(sessionID string) []map[string]any {
    r.mu.Lock()
    state := r.getOrCreateState(sessionID)
    results := make([]map[string]any, len(state.cancelledToolResults))
    copy(results, state.cancelledToolResults)
    r.mu.Unlock()
    return results
}

// ClearCancelledToolResults 清除已收集的工具结果 — 对齐 Python: clear_cancelled_tool_results
func (r *JiuClawStreamEventRail) ClearCancelledToolResults(sessionID string) {
    r.mu.Lock()
    state := r.getOrCreateState(sessionID)
    state.cancelledToolResults = nil
    r.mu.Unlock()
}
```

## 11. DeepAdapter 回填

StreamEventRail 实现后，需回填 DeepAdapter 中 6 处 `⤵️ 10.6.3-10` 占位：

| 位置 | 当前占位 | 回填内容 |
|------|---------|---------|
| `deep_adapter.go:122` `streamEventRail` 字段类型 | `sainterfaces.AgentRail` | 改为 `*commrails.JiuClawStreamEventRail` |
| `deep_adapter_rails.go:406` `buildStreamEventRail()` | `return nil` | `return commrails.NewJiuClawStreamEventRail()` |
| `deep_adapter.go:843` `processMessage` 入口 | `// ⤵️ reset_abort` | `d.streamEventRail.ResetAbort(sessionID)` |
| `deep_adapter.go:1317-1322` `ProcessInterrupt` pause/resume | `// ⤵️ pause/resume` | `d.streamEventRail.Pause/Resume(normalizedSID)` |
| `deep_adapter.go:1332-1374` `ProcessInterrupt` supplement/cancel | `// ⤵️ abort + collect` | 完整 abort + collect + get/clear + resetForNewTask 链路 |
| `deep_adapter.go:1537-1547` `AbortOnGatewayDisconnect` | `_ = activeSIDs` | 遍历 activeSIDs 调 `d.streamEventRail.Abort(sid)` |
| `deep_adapter.go:2097` `unmarkSessionActive` | `// ⤵️ cleanup_session` | `d.streamEventRail.CleanupSession(sessionID)` |

### CodeAdapter 热重载白名单

已有 `code_adapter.go:106` 的 `"JiuClawStreamEventRail": true`，无需修改。

### AutoHarness 传参

`10.6.11-12 AutoHarness` 尚未实现，传参占位留 `// ⤵️ 10.6.11-12` 标记。

## 12. GetCallbacks 覆盖

需要覆盖 `DeepAgentRail.GetCallbacks()` 注册 6 个生命周期钩子：

```go
func (r *JiuClawStreamEventRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
    callbacks := r.DeepAgentRail.GetCallbacks()
    callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
        return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
        return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    callbacks[agentinterfaces.CallbackAfterModelCall] = func(ctx context.Context, railCtx any) error {
        return r.AfterModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    callbacks[agentinterfaces.CallbackBeforeToolCall] = func(ctx context.Context, railCtx any) error {
        return r.BeforeToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
        return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    callbacks[agentinterfaces.CallbackOnModelException] = func(ctx context.Context, railCtx any) error {
        return r.OnModelException(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
    }
    return callbacks
}
```

## 13. Priority

Python `priority = 80`（注意：Go 侧 `user_hook_rail.go` 注释写的 `JiuClawStreamEventRail(50)` 是错误的，Python 源码实际为 `priority = 80`，实现时需修正该注释）。

Go 侧设置：

```go
func NewJiuClawStreamEventRail() *JiuClawStreamEventRail {
    r := &JiuClawStreamEventRail{
        sessionStates: make(map[string]*streamSessionState),
    }
    r.DeepAgentRail.WithPriority(80)
    return r
}
```

## 14. 依赖引入

### 14.1 新增第三方依赖

```
go get github.com/RealAlexandreAI/json-repair
```

### 14.2 项目内已有依赖（无需新增）

| 依赖 | 路径 | 用途 |
|------|------|------|
| `rails.DeepAgentRail` | `agentcore/harness/rails/` | 嵌入基类 |
| `agentinterfaces.AgentCallbackContext` | `agentcore/single_agent/interfaces/` | 回调上下文 |
| `sessioninterfaces.SessionFacade` | `agentcore/session/interfaces/` | Session 写流 |
| `stream.OutputSchema` | `agentcore/session/stream/` | 流式输出格式 |
| `llm_schema.ToolCall` / `AssistantMessage` / `ToolMessage` | `agentcore/foundation/llm/schema/` | 消息类型 |
| `ceinterface.ModelContext` | `agentcore/context_engine/interface/` | 上下文操作 |
| `contextutils.ResolveContextMax` | `agentcore/context_engine/context/` | 上下文窗口解析 |
| `interrupt.ConvertInteractionsToAskUserQuestion` | `agentcore/harness/rails/interrupt/` | 中断交互转换 |
| `todo.TodoListTool` / `schema.TodoStatus` / `schema.TodoItem` | `agentcore/harness/tools/todo/` · `harness/schema/` | TODO 工具 |
| `saschema.ToolInterruptException` | `agentcore/single_agent/schema/` | 中断异常类型 |
| `logger` / `cb` | `common/logger/` · `runner/callback/` | 日志 + 回调 |

## 15. 测试策略

### 15.1 单元测试（覆盖率 ≥ 85%）

| 文件 | 测试重点 |
|------|---------|
| `stream_event_rail_test.go` | Pause/Resume/Abort/ResetAbort/ResetForNewTask/CleanupSession 生命周期；checkpoint 阻塞/唤醒；resolveSID；BeforeInvoke/BeforeModelCall/AfterModelCall/BeforeToolCall/AfterToolCall/OnModelException 钩子逻辑；CollectCancelledToolUpdates/Get/Clear 链路 |
| `stream_event_helpers_test.go` | boolishFalse/True、nonzeroExit、inferToolResultError（含各种 dict/list/string 边界）、parseToolCallArguments、extractToolInterrupt、structuredToolResultPayload、truncateString |
| `stream_event_emit_test.go` | 每个 emit 方法验证 WriteStream 写入的 OutputSchema 的 Type 和 Payload 结构正确；使用 mock SessionFacade |
| `stream_event_context_test.go` | fixIncompleteToolContext：各种消息序列（有缺失 ToolMessage、无缺失、空列表）；ensureJSONArguments：合法JSON/闭合括号修复/json_repair修复/规则修复/全部失败兜底；fixMissingQuotes：Windows路径/缺引号键/缺引号值 |

### 15.2 Mock 策略

- `SessionFacade` → mock 实现 `WriteStream` 方法，记录调用参数
- `ModelContext` → mock 实现 `PopMessages` / `AddMessages`，使用内存消息列表
- `BaseAgent` → mock 提供 `Config` 等字段
- 暂停/中止 → 单元测试中直接调 Pause/Abort/Resume 验证 checkpoint 行为

## 16. 不实现 / 延后项

| 项目 | 状态 | 理由 |
|------|------|------|
| AutoHarness 传参 | ⤵️ 10.6.11-12 占位 | AutoHarness 未实现，留传参占位 |
| Python 的 `_stream_tasks: set[asyncio.Task]` | 不实现 | Go 侧无 asyncio.Task 等价，goroutine 由 runtime 管理 |
