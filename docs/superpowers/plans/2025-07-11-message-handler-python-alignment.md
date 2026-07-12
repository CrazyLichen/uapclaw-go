# MessageHandler Python 对齐实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 Go MessageHandler 完整对齐 Python `_forward_loop` 及所有关联方法，补齐缺失逻辑、方法签名和状态字段。

**Architecture:** 按 Python 方法粒度逐一对齐，新增字段/方法/逻辑按依赖顺序实施。先补基础设施（字段+锁+evolution 状态机），再改造核心流程（forwardLoop），最后修正辅助方法。

**Tech Stack:** Go 1.22+, schema/e2a/channel_manager/routing 包

---

## File Structure

| 文件 | 变更 | 职责 |
|------|------|------|
| `internal/swarm/gateway/message_handler/message_handler.go` | 修改 | 新增字段、改造 forwardLoop、删除 processInbound、改造 StopForwarding |
| `internal/swarm/gateway/message_handler/forward_loop.go` | 修改 | handleChatSend/handleChatCancel/handleChatUserAnswer/handleChatResume 补齐完整逻辑 |
| `internal/swarm/gateway/message_handler/convert.go` | 修改 | 无需改动（已对齐） |
| `internal/swarm/gateway/message_handler/cancel.go` | 修改 | CancelAgentWorkForSession 签名改造、sendInterruptToAgent/sendInterruptResultNotification 签名改造、新增 buildToolResultMessage/sendCancelledToolResults/buildErrorOutMessage |
| `internal/swarm/gateway/message_handler/outbound.go` | 修改 | HandleServerPush → handleAgentServerPush，补齐 session_id 回退 + metadata 合并 + cron 判断 |
| `internal/swarm/gateway/message_handler/slash_cmd.go` | 修改 | sendChannelNotice 修正、skillsSlashNotice/branchSlashNotice/rewindSlashNotice RPC 补齐、newSessionCancelAndNotice/modeChangeCancelAndNotice 修正 |
| `internal/swarm/gateway/message_handler/channel_state.go` | 修改 | GetOrCreateChannelState 改复合键、getChannelDefaultState 读 config、新增 saveChannelStateToConfig |
| `internal/swarm/gateway/message_handler/at_file.go` | 修改 | 删除 NormalizeStructuredAttachments，新增 resolveStructuredAttachments + stripAttachedMentions |
| `internal/swarm/gateway/message_handler/evolution.go` | 新建 | Evolution 状态机 15 个方法 |
| `internal/swarm/gateway/message_handler/dispatch.go` | 新建 | prepareAgentDispatchMessage、nonStreamRPCMayRunParallel、shouldEmitProcessingStatusForStream |
| `internal/swarm/gateway/message_handler/config_inject.go` | 新建 | SetOutboundPipeline 注入 + getConfigRaw/updateChannelInConfig 字段 |
| `internal/swarm/gateway/message_handler/disconnect.go` | 新建 | CancelAgentSessionsOnDisconnect |

---

## Task 1: 新增结构体字段和锁

**Files:**
- Modify: `internal/swarm/gateway/message_handler/message_handler.go`

- [ ] **Step 1: 在 MessageHandler struct 中新增字段**

在 `channelStates` 字段之后添加：

```go
// evolution 审批状态
evolutionMu                   sync.RWMutex
pendingEvolutionApproval      map[string]string          // sessionID → approvalRequestID
queuedSupplementInput         map[string]map[string]any // sessionID → {new_input, attachments}
sessionEvolutionInProgress    map[string]bool           // sessionID → true

// 流式 processing_status 追踪
streamEmitsProcessingStatus   map[string]bool           // requestID → should emit

// 用户查询上下文
queryMu                       sync.RWMutex
sessionLastUserQuery          map[string]string         // sessionID → last query

// config 注入回调（对齐 Python set_outbound_pipeline 注入）
getConfigRaw                  func() map[string]any
updateChannelInConfig         func(channelID string, update map[string]any)

// TODO: outboundPipeline 等 11.12 IM Pipeline 回填
```

- [ ] **Step 2: 在 NewMessageHandler 中初始化新字段**

```go
pendingEvolutionApproval:    make(map[string]string),
queuedSupplementInput:       make(map[string]map[string]any),
sessionEvolutionInProgress:  make(map[string]bool),
streamEmitsProcessingStatus: make(map[string]bool),
sessionLastUserQuery:        make(map[string]string),
```

- [ ] **Step 3: 运行测试确认编译通过**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/message_handler/...`
Expected: BUILD SUCCESS

- [ ] **Step 4: Commit**

```
feat(message_handler): 新增 evolution/processing_status/query/config 字段
```

---

## Task 2: Evolution 状态机（15 个方法）

**Files:**
- Create: `internal/swarm/gateway/message_handler/evolution.go`
- Create: `internal/swarm/gateway/message_handler/evolution_test.go`

- [ ] **Step 1: 创建 evolution.go，实现 15 个方法**

所有方法对齐 Python 方法名和逻辑，逐个实现：

1. `isEvolutionApprovalRequestID(requestID string) bool` — 检查 requestID 前缀是否为 evolution 审批格式
2. `queueSupplementInput(sessionID, newInput string, attachments []map[string]any)` — 存入 queuedSupplementInput
3. `popQueuedSupplementInput(sessionID string) map[string]any` — 取出并删除
4. `markPendingEvolutionApproval(sessionID, requestID string)` — 存入 pendingEvolutionApproval
5. `clearPendingEvolutionApproval(sessionID string)` — 删除
6. `finishEvolutionApprovalIfCurrent(sessionID, requestID string) map[string]any` — 如果 current 则清除 + 返回排队输入
7. `markSessionEvolutionInProgress(sessionID string)` — 存入 sessionEvolutionInProgress
8. `clearSessionEvolutionInProgress(sessionID string)` — 删除
9. `isSessionEvolutionInProgress(sessionID string) bool` — 查询
10. `clearSessionEvolutionStates(sessionID string)` — 清除所有三种状态
11. `handleEvolutionChunk(chunk *schema.AgentResponseChunk, sessionID string, metadata map[string]any)` — 处理 evolution_status chunk
12. `buildAutoAcceptEvolutionAnswer(channelID, sessionID, requestID string, metadata map[string]any) *schema.Message` — 自动接受（对齐 Python 签名，直接传 channelID/requestID）
13. `maybeAutoAcceptReplacedEvolutionApproval(sessionID, incomingRequestID, channelID string, metadata map[string]any)` — 可能自动接受被替换的（对齐 Python 签名，需要 channelID/metadata 构造 auto-accept 消息）
14. `buildSupplementContinuationQuery(newInput, originalRequest string) string` — 构造续接查询
15. `buildQueuedChatSendMessage(msg *schema.Message, input string, attachments []map[string]any, originalRequest string) *schema.Message` — 构造排队 chat.send

每个方法实现需对照 Python 对应方法的逻辑，使用 `evolutionMu` 保护并发访问。

- [ ] **Step 2: 创建 evolution_test.go，覆盖核心方法测试**

测试 `isEvolutionApprovalRequestID`、`queueSupplementInput`/`popQueuedSupplementInput`、`markPendingEvolutionApproval`/`finishEvolutionApprovalIfCurrent`、`clearSessionEvolutionStates`、`buildSupplementContinuationQuery`。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/message_handler/... -run TestEvolution -v`
Expected: PASS

- [ ] **Step 4: Commit**

```
feat(message_handler): 新增 evolution 状态机 15 个方法
```

---

## Task 3: dispatch 辅助方法

**Files:**
- Create: `internal/swarm/gateway/message_handler/dispatch.go`
- Create: `internal/swarm/gateway/message_handler/dispatch_test.go`

- [ ] **Step 1: 创建 dispatch.go，实现 3 个方法**

```go
// prepareAgentDispatchMessage 准备发往 AgentServer 的消息
// 对齐 Python _prepare_agent_dispatch_message (L1287-1312)
func (mh *MessageHandler) prepareAgentDispatchMessage(_ context.Context, msg *schema.Message) *schema.Message {
    // TODO: ACP session alias 处理（等 ACP 章节回填）
    // Python: if msg.channel_id == _ACP_CHANNEL_ID:
    //     msg = await self._ensure_acp_agent_session(msg)
    return msg
}

// shouldEmitProcessingStatusForStream 判断是否需要为流式请求发送 processing_status 事件
// 对齐 Python _should_emit_processing_status_for_stream (L1866-1890)
func (mh *MessageHandler) shouldEmitProcessingStatusForStream(msg *schema.Message) bool {
    channelType := mh.resolveControlChannelType(msg)
    return string(channelType) != string(channel_manager.ChannelTypeWeb)
}

// nonStreamRPCMayRunParallel 判断非流式 RPC 是否可以并行执行
// 对齐 Python _non_stream_rpc_may_run_parallel (L1837-1865)
func (mh *MessageHandler) nonStreamRPCMayRunParallel(env *e2a.E2AEnvelope) bool {
    if env.IsStream {
        return false
    }
    method := strings.ToLower(env.Method)
    return method != string(schema.ReqMethodChatSend) && method != string(schema.ReqMethodChatCancel) && method != "chat.resume"
}
```

- [ ] **Step 2: 创建 dispatch_test.go**

测试 `shouldEmitProcessingStatusForStream`（web=false, feishu=true）、`nonStreamRPCMayRunParallel`（stream=false, chat.send=false, skills.list=true）。

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/message_handler/... -run TestDispatch -v`
Expected: PASS

- [ ] **Step 4: Commit**

```
feat(message_handler): 新增 prepareAgentDispatchMessage/shouldEmit/nonStreamRPCMayRunParallel
```

---

## Task 4: rememberUserQueryContext + HandleMessage 调用点

**Files:**
- Modify: `internal/swarm/gateway/message_handler/message_handler.go`

- [ ] **Step 1: 补齐 rememberUserQueryContext 实现**

```go
func (mh *MessageHandler) rememberUserQueryContext(msg *schema.Message) {
    if msg.SessionID == "" {
        return
    }
    if len(msg.Params) == 0 {
        return
    }
    var paramsMap map[string]any
    if err := json.Unmarshal(msg.Params, &paramsMap); err != nil {
        return
    }
    query := ""
    if q, ok := paramsMap["query"]; ok {
        if s, isStr := q.(string); isStr && s != "" {
            query = s
        }
    }
    if query == "" {
        if c, ok := paramsMap["content"]; ok {
            if s, isStr := c.(string); isStr && s != "" {
                query = s
            }
        }
    }
    if query == "" {
        return
    }
    mh.queryMu.Lock()
    mh.sessionLastUserQuery[msg.SessionID] = query
    mh.queryMu.Unlock()
}
```

- [ ] **Step 2: 新增 getSessionLastUserQuery 方法**

```go
func (mh *MessageHandler) getSessionLastUserQuery(sessionID string) string {
    mh.queryMu.RLock()
    defer mh.queryMu.RUnlock()
    return mh.sessionLastUserQuery[sessionID]
}
```

- [ ] **Step 3: 在 HandleMessage 中入队前调 rememberUserQueryContext**

在 `HandleMessage` 方法的 `select { case mh.userMessages <- msg:` 之前添加：
```go
mh.rememberUserQueryContext(msg)
```

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/message_handler/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
feat(message_handler): 补齐 rememberUserQueryContext + HandleMessage 调用点
```

---

## Task 5: cancel.go 方法签名改造

**Files:**
- Modify: `internal/swarm/gateway/message_handler/cancel.go`

- [ ] **Step 1: CancelAgentWorkForSession 签名改造**

旧签名：`func (mh *MessageHandler) CancelAgentWorkForSession(ctx context.Context, sessionID string, intent string)`

新签名：`func (mh *MessageHandler) CancelAgentWorkForSession(ctx context.Context, msg *schema.Message, oldSessionID string, publishInterruptResult bool)`

逻辑对齐 Python L381-528：
1. `mh.clearSessionEvolutionStates(oldSessionID)`
2. 收集流式任务
3. 构造 cancel 请求，从 `msg.Params` 或 `channelStates` 注入 mode + trusted_dirs
4. `resp, err := mh.agentClient.SendRequest(ctx, cancelEnv)` — 先等 AgentServer 响应
5. 取消 gateway 侧任务
6. 处理响应：`publishInterruptResult` 控制是否转发 + `sendCancelledToolResults`

- [ ] **Step 2: sendInterruptResultNotification 签名改造**

新签名：`func (mh *MessageHandler) sendInterruptResultNotification(requestID, channelID, sessionID, intent string, message string, success bool, hasActiveTask *bool)`

逻辑对齐 Python L2693-2748，含 success_messages_map / failure_messages_map。

- [ ] **Step 3: sendProcessingStatus 签名改造**

新签名：`func (mh *MessageHandler) sendProcessingStatus(requestID, sessionID, channelID string, isProcessing bool)`

payload 补齐 `is_complete`。

- [ ] **Step 4: publishStreamCancelledFinal 签名改造**

新签名：`func (mh *MessageHandler) publishStreamCancelledFinal(requestID, channelID, sessionID string, requestMetadata map[string]any)`

- [ ] **Step 5: sendStreamCancelledNotification 对齐**

新签名：`func (mh *MessageHandler) sendStreamCancelledNotification(requestID, channelID, sessionID string)`

改为 `CHAT_INTERRUPT_RESULT` 事件。

- [ ] **Step 6: 新增 buildErrorOutMessage**

```go
func (mh *MessageHandler) buildErrorOutMessage(msg *schema.Message, err error) *schema.Message {
    return &schema.Message{
        ID:        msg.ID,
        Type:      schema.MessageTypeRes,
        ChannelID: msg.ChannelID,
        SessionID: msg.SessionID,
        Timestamp: schema.NowTimestamp(),
        OK:        false,
        Payload:   map[string]any{"error": err.Error()},
        Metadata:  msg.Metadata,
    }
}
```

- [ ] **Step 7: 新增 buildToolResultMessage + sendCancelledToolResults**

对齐 Python L2790-2841。

- [ ] **Step 8: 更新测试适配新签名**

- [ ] **Step 9: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/message_handler/... -v`
Expected: PASS

- [ ] **Step 10: Commit**

```
refactor(message_handler): CancelAgentWorkForSession 签名改造 + 辅助方法对齐
```

---

## Task 6: at_file.go 改造（删除 NormalizeStructuredAttachments，新增 resolveStructuredAttachments + stripAttachedMentions）

**Files:**
- Modify: `internal/swarm/gateway/message_handler/at_file.go`

- [ ] **Step 1: 删除 NormalizeStructuredAttachments 函数**

- [ ] **Step 2: 新增 normalizeStructuredAttachments（内联原逻辑，改为非导出）**

去重 + 路径规范化，逻辑与原 `NormalizeStructuredAttachments` 完全相同，但改为非导出方法。

- [ ] **Step 3: 新增 stripAttachedMentions**

对齐 Python L1477-1510，从 content 中移除已被 attachments 覆盖的 @ 引用。

- [ ] **Step 4: 新增 ResolveStructuredAttachments**

对齐 Python L1513-1526：
1. `normalized := normalizeStructuredAttachments(attachments, cwd)`
2. `prefix := "@"path1" @"path2" ...`
3. `cleanedContent := stripAttachedMentions(content, normalized, cwd)`
4. `mergedContent := prefix + " " + cleanedContent`
5. `return ResolveAtFileReferences(mergedContent, cwd, 0)`

- [ ] **Step 5: 更新测试**

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/message_handler/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```
refactor(message_handler): 删除 NormalizeStructuredAttachments，新增 resolveStructuredAttachments
```

---

## Task 7: forwardLoop 改造（删除 processInbound，逻辑内联 + 步骤对齐）

**Files:**
- Modify: `internal/swarm/gateway/message_handler/forward_loop.go`
- Modify: `internal/swarm/gateway/message_handler/message_handler.go`

- [ ] **Step 1: 删除 processInbound 方法**

- [ ] **Step 2: 重写 forwardLoop，对齐 Python _forward_loop 11 步骤**

```go
func (mh *MessageHandler) forwardLoop(ctx context.Context) {
    logger.Info(logComponent).Msg("入站转发循环已启动")
    defer logger.Info(logComponent).Msg("入站转发循环已退出")

    for {
        select {
        case <-ctx.Done():
            return
        case msg := <-mh.userMessages:
            if msg == nil {
                continue
            }

            // 步骤1: 处理 slash 命令
            if mh.handleChannelControl(msg) {
                continue
            }

            // 步骤2: 注入渠道状态
            mh.ApplyChannelState(msg)

            // TODO: 步骤3 - Gateway hook: UserPromptSubmit（等 11.13 Gateway Hook 回填）

            // 步骤4: CHAT_ANSWER 分支
            if msg.ReqMethod == schema.ReqMethodChatAnswer {
                mh.handleChatUserAnswer(ctx, msg)
                continue
            }

            // 步骤5: CHAT_CANCEL 分支
            if msg.ReqMethod == schema.ReqMethodChatCancel {
                mh.handleChatCancel(ctx, msg)
                continue
            }

            // TODO: 步骤6 - Inbound Pipeline（等 11.12 IM Pipeline 回填）

            // 步骤7: Resolve @file/@agent（仅 CHAT_SEND）
            if msg.ReqMethod == schema.ReqMethodChatSend {
                mh.resolveInboundReferences(msg)
            }

            // 步骤8: 准备 Agent 派发消息
            agentMsg := mh.prepareAgentDispatchMessage(ctx, msg)

            // TODO: 步骤9 - before_chat_request hook（等 11.13 Gateway Hook 回填）

            // 步骤10: stream / non-stream 分发
            env := e2a.MessageToE2AOrFallback(agentMsg)
            streamRid := env.RequestID
            if streamRid == "" {
                streamRid = msg.ID
            }

            if env.IsStream {
                // 流式分发
                emitProcessingStatus := mh.shouldEmitProcessingStatusForStream(msg)
                if emitProcessingStatus {
                    mh.sendProcessingStatus(streamRid, msg.SessionID, msg.ChannelID, true)
                }
                mh.registerStreamTask(streamRid, msg.SessionID, msg.Metadata, nil)
                mh.streamEmitsProcessingStatus[streamRid] = emitProcessingStatus
                mh.streamModes[streamRid] = mh.extractModeFromParams(msg)
                go mh.processStream(ctx, msg, env, emitProcessingStatus)
            } else if mh.nonStreamRPCMayRunParallel(env) {
                // 非流式并行
                go mh.processNonStreamRequest(ctx, msg, env)
            } else {
                // 非流式串行
                if _, err := mh.processNonStreamRequest(ctx, msg, env); err != nil {
                    errMsg := mh.buildErrorOutMessage(msg, err)
                    mh.PublishRobotMessages(errMsg)
                }
            }
        }
    }
}
```

- [ ] **Step 3: 补齐 handleChatSend（新增 rememberUserQueryContext 调用）**

```go
func (mh *MessageHandler) handleChatSend(ctx context.Context, msg *schema.Message) {
    mh.rememberUserQueryContext(msg)
    mh.forwardToAgent(ctx, msg)
}
```

- [ ] **Step 4: 补齐 handleChatCancel 完整逻辑**

supplement/pause/resume/cancel 三个子分支，对齐 Python L2241-2437。

- [ ] **Step 5: 补齐 handleChatUserAnswer 完整逻辑**

非流式 + evolution approval 判断，对齐 Python L2200-2239。

- [ ] **Step 6: 补齐 resolveInboundReferences**

对齐 Python L2450-2495，含 @file/@agent-xxx 解析 + structured attachments + agent mentions system-reminder。

- [ ] **Step 7: 补齐 processStream**

新增 `emitProcessingStatus` 参数、`hasProcessingStatusFalse` 追踪、evolution chunk 处理、cancelled final、processing_status=false 通知。

- [ ] **Step 8: 补齐 processNonStreamRequest 返回值**

返回 `(*schema.AgentResponse, error)`。

- [ ] **Step 9: 更新测试**

- [ ] **Step 10: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/message_handler/... -v`
Expected: PASS

- [ ] **Step 11: Commit**

```
feat(message_handler): forwardLoop 对齐 Python _forward_loop 11 步骤
```

---

## Task 8: outbound.go 改造（HandleServerPush → handleAgentServerPush）

**Files:**
- Modify: `internal/swarm/gateway/message_handler/outbound.go`

- [ ] **Step 1: HandleServerPush 重命名为 handleAgentServerPush**

- [ ] **Step 2: 补齐 session_id 回退逻辑**

优先 `wire["session_id"]`，否则 `streamSessions[requestID]`。

- [ ] **Step 3: 补齐 metadata 合并逻辑**

```go
requestMetadata := mh.getStreamMetadata(rid)
respMetadata := filterInternalMetadata(wire)
busMetadata := MergeAgentMetadata(requestMetadata, respMetadata)
```

- [ ] **Step 4: 修正 cron 判断逻辑**

从 `isCronPayload(msg)` 改为 `chunk.Payload["event_type"] == "cron.response"`。

- [ ] **Step 5: 补齐 evolution chunk 处理**

在 terminal chunk 检查后添加 `mh.handleEvolutionChunk(chunk, sessionID, busMetadata)`。

- [ ] **Step 6: ACP TODO 标注**

```go
// TODO: ACP session_id 解析（等 ACP 章节回填）
// Python: if chunk.channel_id == _ACP_CHANNEL_ID:
//     session_id = self._resolve_acp_external_session_id(session_id, bus_metadata)
```

- [ ] **Step 7: 更新 StartForwarding 中的 push handler 注册**

`mh.agentClient.SetServerPushHandler(func(msg map[string]any) { mh.handleAgentServerPush(msg) })`

- [ ] **Step 8: 更新测试**

- [ ] **Step 9: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/message_handler/... -v`
Expected: PASS

- [ ] **Step 10: Commit**

```
feat(message_handler): handleAgentServerPush 对齐 Python _handle_agent_server_push
```

---

## Task 9: slash_cmd.go 改造

**Files:**
- Modify: `internal/swarm/gateway/message_handler/slash_cmd.go`

- [ ] **Step 1: sendChannelNotice 修正**

event_type 改为 `CHAT_FINAL`，payload key 改为 `content`，补齐 `is_complete: true`。

- [ ] **Step 2: skillsSlashNotice RPC 补齐**

构造 `SKILLS_LIST` E2A → `AgentClient.SendRequest` → 格式化通知。

- [ ] **Step 3: branchSlashNotice RPC 补齐**

构造 `SESSION_FORK` E2A → `AgentClient.SendRequest` → 格式化通知。

- [ ] **Step 4: rewindSlashNotice RPC 补齐**

E2A-first + fallback：构造 `SESSION_REWIND` E2A → `AgentClient.SendRequest` → 成功/失败处理。

- [ ] **Step 5: newSessionCancelAndNotice 修正**

调换顺序（先更新 state 再 cancel）+ 静默模式（`publishInterruptResult=false`）+ 异步取消。

- [ ] **Step 6: modeChangeCancelAndNotice 补齐 cancel 逻辑**

更新 mode 后检查活跃流式任务，有则异步静默取消。

- [ ] **Step 7: 更新测试**

- [ ] **Step 8: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/message_handler/... -v`
Expected: PASS

- [ ] **Step 9: Commit**

```
feat(message_handler): slash 命令 RPC 补齐 + sendChannelNotice 修正
```

---

## Task 10: channel_state.go 改造

**Files:**
- Modify: `internal/swarm/gateway/message_handler/channel_state.go`

- [ ] **Step 1: GetOrCreateChannelState 改为复合键**

`key := getChannelStateKey(ch, msg.SessionID)` + SessionMap TODO。

- [ ] **Step 2: getChannelDefaultState 从 config 读取**

如果 `mh.getConfigRaw != nil` → 调用读取 `channels[channelID]` 的 `default_session_id` / `default_mode`。

- [ ] **Step 3: 新增 saveChannelStateToConfig**

对齐 Python L301-312（dead code 但补定义）。

- [ ] **Step 4: 新增 SetOutboundPipeline 注入方法**

在 `config_inject.go` 中实现，注入 `pipeline` / `getConfigRaw` / `updateChannelInConfig`。

```go
func (mh *MessageHandler) SetOutboundPipeline(
    pipeline OutboundPipeline,
    getConfigRaw func() map[string]any,
    updateChannelInConfig func(channelID string, update map[string]any),
)
```

- [ ] **Step 5: 更新测试**

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/message_handler/... -v`
Expected: PASS

- [ ] **Step 7: Commit**

```
feat(message_handler): channel state 复合键 + config 注入 + SetOutboundPipeline
```

---

## Task 11: CancelAgentSessionsOnDisconnect

**Files:**
- Create: `internal/swarm/gateway/message_handler/disconnect.go`

- [ ] **Step 1: 实现 CancelAgentSessionsOnDisconnect**

对齐 Python L530-573，遍历 sessionKeys 构造 cancel 消息 + 调 `CancelAgentWorkForSession`。

- [ ] **Step 2: 运行测试**

- [ ] **Step 3: Commit**

```
feat(message_handler): 新增 CancelAgentSessionsOnDisconnect
```

---

## Task 12: StopForwarding 清理补齐

**Files:**
- Modify: `internal/swarm/gateway/message_handler/message_handler.go`

- [ ] **Step 1: cancelAllStreamTasks 补齐清理**

新增清理 `streamEmitsProcessingStatus`、`sessionEvolutionInProgress`、`pendingEvolutionApproval`、`queuedSupplementInput`、`sessionLastUserQuery`。

- [ ] **Step 2: 运行测试**

- [ ] **Step 3: Commit**

```
fix(message_handler): StopForwarding 补齐 evolution/query/processing_status 清理
```

---

## Task 13: doc.go 更新

**Files:**
- Modify: `internal/swarm/gateway/message_handler/doc.go`

- [ ] **Step 1: 更新文件目录**

新增 `evolution.go`、`dispatch.go`、`config_inject.go`、`disconnect.go`。

- [ ] **Step 2: Commit**

```
docs(message_handler): 更新 doc.go 文件目录
```

---

## Task 14: 全量编译 + 测试

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/... -v -count=1`
Expected: ALL PASS

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 11.3 状态**

11.3 已标记 ✅，无需修改。

- [ ] **Step 4: Commit**

```
chore: MessageHandler Python 对齐完成
```
