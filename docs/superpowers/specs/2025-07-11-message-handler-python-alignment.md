# 11.3 MessageHandler 对齐 Python 实现计划

> 本计划基于 Python `_forward_loop` 及相关方法的逐行对比，补齐 Go 实现中缺失的逻辑、
> 方法签名和状态字段。所有方法名对齐 Python，步骤顺序对齐 Python `_forward_loop`。

## 结构变更

| 变更 | 说明 |
|------|------|
| 删除 `processInbound()` | 逻辑直接写在 `forwardLoop()` 中，对齐 Python `_forward_loop` |
| 删除 `NormalizeStructuredAttachments()` | 逻辑内联到 `resolveStructuredAttachments()`，对齐 Python `_resolve_structured_attachments` |
| `HandleServerPush` → `handleAgentServerPush` | 方法名对齐 Python `_handle_agent_server_push` |
| `isCronPayload()` 逻辑修正 | 对齐 Python：判断 `chunk.payload.event_type == "cron.response"` |

---

## 一、MessageHandler 结构体字段补齐

### 1.1 新增字段（对齐 Python `__init__` L119-170）

```go
// evolution 审批状态
pendingEvolutionApproval    map[string]string            // sessionID → approvalRequestID（对齐 _pending_evolution_approval）
queuedSupplementInput       map[string]map[string]any   // sessionID → {new_input, attachments}（对齐 _queued_supplement_input）
sessionEvolutionInProgress  map[string]bool             // sessionID → true（对齐 _session_evolution_in_progress）

// 流式 processing_status 追踪
streamEmitsProcessingStatus map[string]bool             // requestID → should emit（对齐 _stream_emits_processing_status）

// 用户查询上下文
sessionLastUserQuery        map[string]string           // sessionID → last query（对齐 _session_last_user_query）

// config 注入回调（对齐 Python set_outbound_pipeline 注入）
getConfigRaw                func() map[string]any       // 对齐 _get_config_raw
updateChannelInConfig       func(channelID string, update map[string]any) // 对齐 _update_channel_in_config

// outbound pipeline（对齐 _outbound_pipeline）
outboundPipeline            InboundOutboundPipeline     // TODO: 等 11.12 IM Pipeline 回填
```

### 1.2 新增锁

```go
evolutionMu    sync.RWMutex   // evolution 状态锁
queryMu        sync.RWMutex   // sessionLastUserQuery 锁
```

---

## 二、forwardLoop 对齐 Python `_forward_loop`（L2163-2558）

### 2.1 删除 `processInbound`，逻辑直接写在 `forwardLoop` 中

**Python `_forward_loop` 步骤顺序**（Go `forwardLoop` 必须严格对齐）：

```
while running:
    msg = consume_user_messages()
    
    步骤1: _handle_channel_control(msg)         → handleChannelControl(msg)
    步骤2: _apply_channel_state(msg)             → ApplyChannelState(msg)
    步骤3: Gateway hook: UserPromptSubmit        → TODO: 等 11.13 Gateway Hook 回填
    步骤4: CHAT_ANSWER 分支                      → handleChatUserAnswer(ctx, msg)
    步骤5: CHAT_CANCEL 分支                      → handleChatCancel(ctx, msg)
    步骤6: Inbound Pipeline                      → TODO: 等 11.12 IM Pipeline 回填
    步骤7: Resolve @file/@agent（仅 CHAT_SEND）  → resolveInboundReferences(msg)
    步骤8: _prepare_agent_dispatch_message(msg)  → prepareAgentDispatchMessage(ctx, msg)
    步骤9: before_chat_request hook              → TODO: 等 11.13 Gateway Hook 回填
    步骤10: stream / non-stream 分发
        - 流式 → processStream(...)
        - 非流式并行 → go processNonStreamRequest(...)
        - 非流式串行 → processNonStreamRequest(...)
    步骤11: 异常 → buildErrorOutMessage → publishRobotMessages
```

### 2.2 步骤3：Gateway Hook UserPromptSubmit（预留 TODO）

```go
// TODO: 步骤3 - Gateway hook: UserPromptSubmit（等 11.13 Gateway Hook 回填）
// Python: if self._gateway_hook_handler:
//     await self._gateway_hook_handler.on_user_prompt_submit(session_id, prompt_text)
```

### 2.3 步骤4：handleChatUserAnswer（对齐 Python L2200-2239）

**方法签名**：`func (mh *MessageHandler) handleChatUserAnswer(ctx context.Context, msg *schema.Message)`

**逻辑**：
1. `agentMsg := mh.prepareAgentDispatchMessage(ctx, msg)`
2. `env := e2a.MessageToE2AOrFallback(agentMsg)`
3. `resp, err := mh.processNonStreamRequest(ctx, msg, env)` — 必须非流式
4. `answerRequestID := msg.Params["request_id"]`
5. `if mh.isEvolutionApprovalRequestID(answerRequestID)`：
   - 检查 `resp.Payload["resolved"] == true`
   - 如果 resolved：`mh.finishEvolutionApprovalIfCurrent(msg.SessionID, answerRequestID)` → 取出排队输入 → 构造新 chat.send 入队
   - 如果 not resolved：打日志
6. **不调 forwardToAgent**，方法内完整处理

### 2.4 步骤5：handleChatCancel（对齐 Python L2241-2437）

**方法签名**：`func (mh *MessageHandler) handleChatCancel(ctx context.Context, msg *schema.Message)`

**逻辑**：
1. 解析 `newInput`, `hasNewInput`, `intent` 从 `msg.Params`
2. **if hasNewInput**：supplement 分支
   - 检查 evolution 状态：`if mh.isSessionEvolutionInProgress(sessionID) || sessionID in pendingEvolutionApproval`
     - → `mh.queueSupplementInput(sessionID, newInput, attachments)`
     - → `mh.sendInterruptResultNotification(..., intent="supplement", message="已加入队列，等待演进完成")`
     - → return
   - 取消当前 session 流式任务
   - `mh.sendInterruptResultNotification(..., intent="supplement")`
   - 构造 supplement cancel E2A（注入 mode + trusted_dirs + cwd）
   - `mh.agentClient.SendRequest(ctx, supplementEnv)`
   - `mh.sendCancelledToolResults(...)`
   - 构造新 chat.send 消息（`mh.buildSupplementContinuationQuery` + `mh.buildQueuedChatSendMessage`）
   - `mh.PublishUserMessagesNowait(newMsg)`
3. **elif intent == "cancel"**：
   - `mh.CancelAgentWorkForSession(ctx, msg, sessionID, true)` — publishInterruptResult=true
4. **elif intent == "pause" / "resume"**：
   - `agentMsg := mh.prepareAgentDispatchMessage(ctx, msg)`
   - 注入 mode 信息（从 channelStates）
   - `env := e2a.MessageToE2AOrFallback(agentMsg)`
   - `go mh.sendInterruptToAgent(ctx, env)` — fire-and-forget
   - 检查是否有活跃流式任务
   - `mh.sendInterruptResultNotification(..., intent, hasActiveTask=...)`

### 2.5 步骤6：Inbound Pipeline（预留 TODO）

```go
// TODO: 步骤6 - Inbound Pipeline（数字分身入站过滤）（等 11.12 IM Pipeline 回填）
// Python: if self._inbound_pipeline is not None and msg.req_method == ReqMethod.CHAT_SEND:
//     should_forward = await self._inbound_pipeline.apply(msg)
//     if not should_forward: continue
```

### 2.6 步骤7：resolveInboundReferences（对齐 Python L2450-2495）

**方法签名**：`func (mh *MessageHandler) resolveInboundReferences(msg *schema.Message)`

**逻辑**（仅对 CHAT_SEND 生效）：
1. `content = params["query"] or params["content"]`
2. `attachments = params["attachments"]`
3. `cwd = metadata["cwd"]`
4. 如果有 `attachments` → `enriched = mh.resolveStructuredAttachments(content, attachments, cwd)`
5. 否则如果 `content` 含 `@` → `enriched = ResolveAtFileReferences(content, cwd, 0)`
6. `agentMentions = ExtractAgentMentions(content)`
7. 如果有 agentMentions → 追加 `<system-reminder>` 提示文本（硬编码中文）
8. 如果 `enriched != content` → 回写 `params["query"]` 和 `params["content"]`

### 2.7 步骤8：prepareAgentDispatchMessage（对齐 Python L1287-1312）

**方法签名**：`func (mh *MessageHandler) prepareAgentDispatchMessage(ctx context.Context, msg *schema.Message) *schema.Message`

**逻辑**：
```go
func (mh *MessageHandler) prepareAgentDispatchMessage(_ context.Context, msg *schema.Message) *schema.Message {
    // TODO: ACP session alias 处理（等 ACP 章节回填）
    // Python: if msg.channel_id == _ACP_CHANNEL_ID:
    //     msg = await self._ensure_acp_agent_session(msg)
    return msg
}
```

### 2.8 步骤9：before_chat_request hook（预留 TODO）

```go
// TODO: 步骤9 - before_chat_request hook（等 11.13 Gateway Hook 回填）
// Python: await self._trigger_before_chat_request_hook(agent_msg)
```

### 2.9 步骤10：stream / non-stream 分发

**流式**（对齐 Python L2506-2532）：
1. `emitProcessingStatus := mh.shouldEmitProcessingStatusForStream(msg)`
2. 如果 `emitProcessingStatus` → `mh.sendProcessingStatus(requestID, sessionID, channelID, true)`
3. `go mh.processStream(ctx, msg, env, emitProcessingStatus)`
4. 注册 streamTasks/streamSessions/streamMetadata/streamEmitsProcessingStatus/streamModes

**非流式并行判断**（对齐 Python L2534-2545）：
- 新增 `func (mh *MessageHandler) nonStreamRPCMayRunParallel(env *e2a.E2AEnvelope) bool`
- 逻辑：`env.IsStream == false && env.Method not in {"chat.send", "chat.cancel", "chat.resume"}`
- 并行：`go mh.processNonStreamRequest(...)`
- 串行：直接调用 `mh.processNonStreamRequest(...)`

### 2.10 步骤11：异常处理（对齐 Python L2548-2555）

- 新增 `func (mh *MessageHandler) buildErrorOutMessage(msg *schema.Message, err error) *schema.Message`
- 构造 `ok=false, payload={"error": err.Error()}, metadata=msg.Metadata`
- `mh.PublishRobotMessages(errMsg)`

---

## 三、handleChatSend 补齐

**方法签名**：`func (mh *MessageHandler) handleChatSend(ctx context.Context, msg *schema.Message)`

**逻辑**（在 `forwardLoop` 中被步骤7处理 @file/@agent 后调用）：
1. 调 `mh.rememberUserQueryContext(msg)` — 对齐 Python `_remember_user_query_context`
2. 调 `mh.forwardToAgent(ctx, msg)`

---

## 四、Cancel / Interrupt 方法对齐

### 4.1 CancelAgentWorkForSession 签名改造（对齐 Python L381-528）

**新签名**：
```go
func (mh *MessageHandler) CancelAgentWorkForSession(
    ctx context.Context,
    msg *schema.Message,
    oldSessionID string,
    publishInterruptResult bool,
)
```

**逻辑**：
1. `mh.clearSessionEvolutionStates(oldSessionID)`
2. 收集该 session 的流式任务 `tasksToCancel`
3. 构造 cancel 请求，**注入 mode + trusted_dirs**（从 `msg.Params` 或 `channelStates` 获取）
4. `resp, err := mh.agentClient.SendRequest(ctx, cancelEnv)` — **先发到 AgentServer 等响应**
5. 取消 gateway 侧流式任务
6. 处理 AgentServer 响应：
   - 如果 `payload.event_type == "chat.interrupt_result"`：
     - 如果 `publishInterruptResult` → `ResponseToMessage` → `PublishRobotMessages` + `sendCancelledToolResults`
     - 否则静默（`/new_session` 场景）
   - 否则 → `sendInterruptResultNotification(..., success=false)`

### 4.2 sendInterruptToAgent（对齐 Python L2682-2691）

**方法签名**：`func (mh *MessageHandler) sendInterruptToAgent(ctx context.Context, env *e2a.E2AEnvelope)`

**逻辑**：fire-and-forget，调 `SendRequest`，丢弃响应，失败仅 warn。

### 4.3 sendInterruptResultNotification 签名改造（对齐 Python L2693-2748）

**新签名**：
```go
func (mh *MessageHandler) sendInterruptResultNotification(
    requestID, channelID, sessionID, intent string,
    message string, success bool, hasActiveTask *bool,
)
```

**逻辑**：
- 根据 intent + success 组合中文提示文本（对齐 Python success_messages_map / failure_messages_map）
- payload 含 `event_type`, `intent`, `success`, `message`, `has_active_task`
- 消息 ID 使用 requestID

### 4.4 sendCancelledToolResults（对齐 Python L2820-2841）

**方法签名**：`func (mh *MessageHandler) sendCancelledToolResults(channelID, sessionID string, payload map[string]any, metadata map[string]any)`

**逻辑**：
- `cancelledTools = payload["cancelled_tools"]`
- 逐个构造 `buildToolResultMessage` → `PublishRobotMessages`

### 4.5 buildToolResultMessage（对齐 Python L2790-2818）

**方法签名**：`func (mh *MessageHandler) buildToolResultMessage(channelID, sessionID string, toolInfo map[string]any, metadata map[string]any) *schema.Message`

**逻辑**：
- `id = "tool_result_{timestamp:x}_{random_hex}"`
- `type = event`, `event_type = CHAT_TOOL_RESULT`
- `payload = {"tool_result": {"tool_name", "tool_call_id", "result", "status"}}`

---

## 五、processStream 对齐 Python L2559-2648

### 5.1 签名改造

```go
func (mh *MessageHandler) processStream(
    ctx context.Context,
    msg *schema.Message,
    envelope *e2a.E2AEnvelope,
    emitProcessingStatus bool,
)
```

### 5.2 新增逻辑

1. 追踪 `hasProcessingStatusFalse`（`chunk.payload.event_type == "chat.processing_status" && is_processing == false`）
2. 调 `mh.handleEvolutionChunk(chunk, sessionID, requestMetadata)`
3. CancelledError → `mh.publishStreamCancelledFinal(requestID, channelID, sessionID, requestMetadata)`
4. finally：
   - 清理 `streamEmitsProcessingStatus`
   - evolution 清理：`if sessionID not in streamSessions.values() → clearSessionEvolutionInProgress(sessionID)`
   - 如果 `emitProcessingStatus && !cancelled && !hasProcessingStatusFalse`：
     - 检查 session 是否还有活跃任务
     - 如果没有 → `mh.sendProcessingStatus(requestID, sessionID, channelID, false)`

---

## 六、handleAgentServerPush 对齐 Python L1610-1672

### 6.1 方法名变更

`HandleServerPush` → `handleAgentServerPush`（对齐 Python `_handle_agent_server_push`）

### 6.2 逻辑补齐

1. **session_id 回退**：优先 `wire["session_id"]`，否则 `streamSessions[requestID]`
2. **metadata 合并**：`requestMetadata = streamMetadata[rid]`，`respMetadata = wire["metadata"]`（过滤 `E2A_WIRE_INTERNAL_METADATA_KEYS`），`busMetadata = MergeAgentMetadata(requestMetadata, respMetadata)`
3. **ACP**：`if channelID == ACP_CHANNEL_ID → TODO: 等 ACP 章节回填`
4. **cron 判断对齐**：`if chunk.Payload["event_type"] == "cron.response" → handleCronPushPayload(...)`
5. **evolution chunk**：`mh.handleEvolutionChunk(chunk, sessionID, busMetadata)`
6. `ChunkToMessage(chunk, sessionID, busMetadata)` → `PublishRobotMessages`

---

## 七、Slash 命令对齐

### 7.1 sendChannelNotice 修正（对齐 Python L347-365 + A8）

- `event_type` 改为 `CHAT_FINAL`
- payload key 从 `notice` 改为 `content`
- payload 补齐 `is_complete: true`

### 7.2 skillsSlashNotice（对齐 Python L885-937）

**逻辑**：
1. `agentMsg := mh.prepareAgentDispatchMessage(ctx, msg)`
2. 构造 `SKILLS_LIST` E2A envelope（`e2aFromAgentFields`）
3. `resp := mh.agentClient.SendRequest(ctx, env)`
4. `skills = resp.Payload["skills"]`
5. `noticeText = formatSkillsListForNotice(skills)`（对齐 Python `format_skills_list_for_notice`）
6. `mh.sendChannelNotice(msg, noticeText)`

### 7.3 branchSlashNotice（对齐 Python L939-1015）

**逻辑**：
1. 构造 `SESSION_FORK` E2A envelope（params: `{branch_name}`）
2. `resp := mh.agentClient.SendRequest(ctx, env)`
3. `newSessionID = resp.Payload["new_session_id"]`
4. 格式化通知文本
5. `mh.sendChannelNotice(msg, noticeText)`

### 7.4 rewindSlashNotice（对齐 Python L1047-1137）

**逻辑**：E2A-first + fallback
1. 构造 `SESSION_REWIND` E2A envelope（params: `{turn, session_id}`）
2. try → `resp := mh.agentClient.SendRequest(ctx, env)`
3. 成功 → `rewoundTurn = resp.Payload["rewound_turn"]` → 通知
4. 失败 → fallback 本地通知

---

## 八、Evolution 状态机（对齐 Python C8）

### 8.1 新增方法

| Go 方法名 | Python 对应 | 签名 |
|-----------|------------|------|
| `isEvolutionApprovalRequestID` | `_is_evolution_approval_request_id` | `(requestID string) bool` |
| `queueSupplementInput` | `_queue_supplement_input` | `(sessionID, newInput string, attachments []map[string]any)` |
| `popQueuedSupplementInput` | `_pop_queued_supplement_input` | `(sessionID string) map[string]any` |
| `markPendingEvolutionApproval` | `_mark_pending_evolution_approval` | `(sessionID, requestID string)` |
| `clearPendingEvolutionApproval` | `_clear_pending_evolution_approval` | `(sessionID string)` |
| `finishEvolutionApprovalIfCurrent` | `_finish_evolution_approval_if_current` | `(sessionID, requestID string) map[string]any` |
| `markSessionEvolutionInProgress` | `_mark_session_evolution_in_progress` | `(sessionID string)` |
| `clearSessionEvolutionInProgress` | `_clear_session_evolution_in_progress` | `(sessionID string)` |
| `isSessionEvolutionInProgress` | `_is_session_evolution_in_progress` | `(sessionID string) bool` |
| `clearSessionEvolutionStates` | `_clear_session_evolution_states` | `(sessionID string)` |
| `handleEvolutionChunk` | `_handle_evolution_chunk` | `(chunk *AgentResponseChunk, sessionID string, metadata map[string]any)` |
| `buildAutoAcceptEvolutionAnswer` | `_build_auto_accept_evolution_answer` | `(chunk, sessionID, metadata)` |
| `maybeAutoAcceptReplacedEvolutionApproval` | `_maybe_auto_accept_replaced_evolution_approval` | `(sessionID, newRequestID string)` |
| `buildSupplementContinuationQuery` | `_build_supplement_continuation_query` | `(newInput, originalRequest string) string` |
| `buildQueuedChatSendMessage` | `_build_queued_chat_send_message` | `(msg, input, attachments, original) *Message` |

---

## 九、辅助方法签名对齐

### 9.1 processNonStreamRequest 返回值改造

```go
// 旧：func (mh *MessageHandler) processNonStreamRequest(ctx, msg, envelope)
// 新：
func (mh *MessageHandler) processNonStreamRequest(
    ctx context.Context, msg *schema.Message, envelope *e2a.E2AEnvelope,
) (*schema.AgentResponse, error)
```

### 9.2 sendProcessingStatus 签名改造（对齐 Python L2750-2773）

```go
// 旧：func (mh *MessageHandler) SendProcessingStatus(sessionID string, isProcessing bool)
// 新：
func (mh *MessageHandler) sendProcessingStatus(
    requestID, sessionID, channelID string, isProcessing bool,
)
```

- payload 补齐 `is_complete: !isProcessing`
- message ID 使用 requestID

### 9.3 publishStreamCancelledFinal 签名改造（对齐 Python L1798-1850）

```go
// 旧：func (mh *MessageHandler) publishStreamCancelledFinal(sessionID string)
// 新：
func (mh *MessageHandler) publishStreamCancelledFinal(
    requestID, channelID, sessionID string, requestMetadata map[string]any,
)
```

- message type 改为 `res`（非 event）
- message ID 使用 requestID
- 携带 requestMetadata

### 9.4 sendStreamCancelledNotification 对齐（对齐 Python L2650-2680）

```go
// 旧：func (mh *MessageHandler) sendStreamCancelledNotification(sessionID string) — 调 SendProcessingStatus
// 新：
func (mh *MessageHandler) sendStreamCancelledNotification(
    requestID, channelID, sessionID string,
)
```

- 构造 `CHAT_INTERRUPT_RESULT` 事件，payload 含 `intent=cancel, success=true, message="任务已取消"`

### 9.5 rememberUserQueryContext 补齐（对齐 Python L223-235）

```go
func (mh *MessageHandler) rememberUserQueryContext(msg *schema.Message) {
    if msg.SessionID == "" { return }
    params := msg.Params 解析
    query := params["query"] or params["content"]
    if query != "" {
        mh.queryMu.Lock()
        mh.sessionLastUserQuery[msg.SessionID] = query
        mh.queryMu.Unlock()
    }
}
```

- 新增 `getSessionLastUserQuery(sessionID string) string`
- **调用点**：`HandleMessage` 中入队前调 `mh.rememberUserQueryContext(msg)`

### 9.6 shouldEmitProcessingStatusForStream（对齐 Python L1866-1890）

```go
func (mh *MessageHandler) shouldEmitProcessingStatusForStream(msg *schema.Message) bool {
    // web 渠道不发送，其他渠道默认发送
    channelType := mh.resolveControlChannelType(msg)
    return string(channelType) != "web"
}
```

### 9.7 nonStreamRPCMayRunParallel（对齐 Python L1837-1865）

```go
func (mh *MessageHandler) nonStreamRPCMayRunParallel(env *e2a.E2AEnvelope) bool {
    if env.IsStream { return false }
    method := strings.ToLower(env.Method)
    return method != "chat.send" && method != "chat.cancel" && method != "chat.resume"
}
```

---

## 十、Slash 命令控制流程对齐

### 10.1 newSessionCancelAndNotice 修正（对齐 Python L575-591 + A9）

**顺序修正**：
1. 生成 `newSID`
2. 更新 `state.SessionID = newSID`
3. 异步取消旧会话：`go mh.CancelAgentWorkForSession(ctx, msg, oldSID, false)` — `publishInterruptResult=false`（静默）
4. `mh.sendChannelNotice(msg, ...)`

### 10.2 modeChangeCancelAndNotice 补齐 cancel 逻辑（对齐 Python L593-613 + C17）

**补齐**：
1. 更新 mode 后
2. 检查当前 session 是否有活跃流式任务
3. 如果有 → 异步调 `CancelAgentWorkForSession(ctx, msg, oldSID, false)`（静默）
4. `mh.sendChannelNotice(msg, ...)`

---

## 十一、Channel State 对齐

### 11.1 GetOrCreateChannelState 改为复合键（对齐 Python L278-299 + D12）

- `key := getChannelStateKey(ch, msg.SessionID)` — 而非 `key := ch`
- TODO: SessionMap 集成（等 11.7 回填）

### 11.2 getChannelDefaultState 从 config 读取（对齐 Python L247-270 + D10-D11）

**逻辑**：
1. 如果 `mh.getConfigRaw != nil` → 调用读取 config
2. 从 `config["channels"][channelID]` 读取 `default_session_id` 和 `default_mode`
3. 如果无 `default_session_id` → `GenerateChannelSessionID(channelID)`
4. 如果无 `default_mode` → `ChannelModeAgentPlan`

### 11.3 saveChannelStateToConfig（对齐 Python L301-312）

**方法签名**：`func (mh *MessageHandler) saveChannelStateToConfig(channelID string)`

**逻辑**：
1. `state := mh.channelStates[channelID]`
2. `mh.updateChannelInConfig(channelID, {"default_session_id": state.SessionID, "default_mode": ChannelModeString(state.Mode)})`

> 注：Python 中此方法当前未被调用（dead code），但补定义以对齐。

### 11.4 SetOutboundPipeline 注入（对齐 Python L182-195）

```go
func (mh *MessageHandler) SetOutboundPipeline(pipeline InboundOutboundPipeline, getConfigRaw func() map[string]any, updateChannelInConfig func(string, map[string]any)) {
    mh.outboundPipeline = pipeline
    mh.getConfigRaw = getConfigRaw
    mh.updateChannelInConfig = updateChannelInConfig
}
```

---

## 十二、断连取消（对齐 Python L530-573 + D5）

### 12.1 cancelAgentSessionsOnDisconnect

**方法签名**：`func (mh *MessageHandler) CancelAgentSessionsOnDisconnect(ctx context.Context, sessionKeys [][2]string)`

**逻辑**：
1. 遍历 `sessionKeys`（每项为 `[channelID, sessionID]`）
2. 构造 cancel 消息（注入 channel mode）
3. `mh.CancelAgentWorkForSession(ctx, cancelMsg, sessionID, true)`

---

## 十三、resolveStructuredAttachments（对齐 Python L1513-1526 + A1）

**方法签名**：`func ResolveStructuredAttachments(content string, attachments []map[string]any, cwd string) string`

**逻辑**（内联原 `NormalizeStructuredAttachments` 的去重+路径规范化逻辑）：
1. `normalized := normalizeStructuredAttachments(attachments, cwd)` — 内联原方法逻辑
2. `prefix := ` `"@"path1" @"path2" ...` 拼接
3. `cleanedContent := stripAttachedMentions(content, normalized, cwd)` — 新增方法
4. `mergedContent := prefix + " " + cleanedContent`
5. `return ResolveAtFileReferences(mergedContent, cwd, 0)`

### 新增 stripAttachedMentions（对齐 Python L1477-1510）

**方法签名**：`func stripAttachedMentions(content string, attachments []map[string]any, cwd string) string`

---

## 十四、StopForwarding 清理补齐（对齐 Python L2851-2883 + C11）

`cancelAllStreamTasks` 中补齐清理：
- `streamEmitsProcessingStatus`
- `sessionEvolutionInProgress`
- `pendingEvolutionApproval`
- `queuedSupplementInput`
- `sessionLastUserQuery`

---

## 十五、TODO 标注汇总

| TODO 位置 | 等待章节 | 说明 |
|-----------|---------|------|
| forwardLoop 步骤3 | 11.13 Gateway Hook | UserPromptSubmit hook |
| forwardLoop 步骤6 | 11.12 IM Pipeline | Inbound Pipeline |
| forwardLoop 步骤9 | 11.13 Gateway Hook | before_chat_request hook |
| prepareAgentDispatchMessage | ACP 章节 | ACP session alias |
| handleAgentServerPush ACP | ACP 章节 | ACP session_id 解析 |
| cronController 字段 | 11.10 Cron | CronController 对接 |
| handleCronPushPayload | 11.10 Cron | Cron action 分发 |
| GetOrCreateChannelState | 11.7 SessionMap | SessionMap 集成 |
| _apply_channel_state | 11.7 SessionMap | SessionMap 重新解析 |
| _extract_identity_tuple | 11.7 SessionMap | identity 提取 |
| _channel_id_matches_session_map_types | 11.7 SessionMap | session_map 渠道判断 |
| _is_session_map_style_session_id | 11.7 SessionMap | session_id 格式判断 |
| _is_known_jiuwenswarm_session_id | ACP/SessionMap | known prefix 判断 |
| triggerSessionStartHook | 11.13 Gateway Hook | session start 事件 |
| outboundPipeline | 11.12 IM Pipeline | publishRobotMessages 中 pipeline |
| publishRobotMessages | 11.12 IM Pipeline | outbound pipeline 调用 |
