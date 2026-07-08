# MessageHandler 路由逻辑 + WebChannel 打通 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整实现 MessageHandler 的入站/出站转发链路，打通 WebChannel ↔ MessageHandler ↔ Transport ↔ AgentServer 的全链路通信。

**Architecture:** MessageHandler 维护双 channel（userMessages 入站 + robotMessages 出站），入站消息经 E2A 编码通过 Transport 发往 AgentServer，出站响应从 Transport.Recv 读取经 E2A 解码转为 Message 通过 ChannelManager 广播到 WebChannel。同时实现 server_push 下行处理、slash 命令解析、渠道状态管理等完整逻辑。

**Tech Stack:** Go 1.22+, gorilla/websocket, go-chi/chi/v5, internal e2a/schema/gateway_push 包

**Spec:** `docs/superpowers/specs/2025-07-14-uapclaw-app-web-channel-design.md`

**对应 IMPLEMENTATION_PLAN.md 章节:** 11.3(MessageHandler), 11.5(WebSocketAgentServerClient), 11.9(GatewayServer)

---

## 文件结构

### 新建文件

```
internal/swarm/gateway/message_handler/
├── message_handler.go        # 重写：完整 MessageHandler 实现
├── message_handler_test.go   # 新建：单元测试
├── forward_loop.go           # 新建：入站转发循环
├── forward_loop_test.go      # 新建：转发循环测试
├── outbound.go               # 新建：出站循环 + dispatch 循环
├── outbound_test.go          # 新建：出站循环测试
├── channel_state.go          # 新建：ChannelControlState + ChannelMode + _apply_channel_state
├── channel_state_test.go     # 新建：渠道状态测试
├── convert.go                # 新建：E2AResponse→Message 转换函数
├── convert_test.go           # 新建：转换测试
├── cancel.go                 # 新建：_cancel_agent_work_for_session + interrupt 逻辑
├── cancel_test.go            # 新建：取消逻辑测试
├── slash_cmd.go              # 新建：slash 命令处理（_handle_channel_control）
├── slash_cmd_test.go         # 新建：slash 命令测试
├── at_file.go                # 新建：@file 引用解析 + @agent 提及
├── at_file_test.go           # 新建：@file 测试
└── doc.go                    # 更新：包文档

internal/swarm/gateway/message_handler/command_parser/
├── doc.go                    # 新建：包文档
├── slash_command.go          # 新建：slash 命令解析器（对齐 Python 337 行）
└── slash_command_test.go     # 新建：解析器测试

internal/swarm/gateway/channel_manager/web/
├── web_connect.go            # 修改：完善 Send 事件路由
└── web_handlers.go           # 修改：chat 类方法改为转发

internal/swarm/server/gateway_push/
├── transport.go              # 修改：加 GatewayPushTransport 接口
├── channel_transport.go      # 修改：加 pushCh 推送通道
├── channel_transport_test.go # 修改：补充 push 测试
└── wire.go                   # 新建：buildServerPushWire 线编码
```

### 修改文件

```
internal/swarm/gateway/app_gateway.go    # 注入 Transport + MessageHandler
cmd/uapclaw/cmd.go                       # runAppCmd 加 Transport 创建
```

---

## Task 1: command_parser/slash_command.go — Slash 命令解析器

**Files:**
- Create: `internal/swarm/gateway/message_handler/command_parser/doc.go`
- Create: `internal/swarm/gateway/message_handler/command_parser/slash_command.go`
- Test: `internal/swarm/gateway/message_handler/command_parser/slash_command_test.go`

**对齐 Python:** `jiuwenswarm/gateway/message_handler/command_parser/slash_command.py` (337 行)

- [ ] **Step 1: 创建包目录和 doc.go**

```go
// Package command_parser 提供 Gateway slash 命令解析器。
//
// 解析 IM 渠道消息中的 \new_session、\mode、\switch、\skills list、
// \branch、\rewind 等控制指令，返回结构化解析结果。
//
// 文件目录：
//
//	command_parser/
//	├── doc.go              # 包文档
//	├── slash_command.go    # Slash 命令解析器
//	└── slash_command_test.go # 解析器测试
//
// 对应 Python 代码：jiuwenswarm/gateway/message_handler/command_parser/
package command_parser
```

- [ ] **Step 2: 实现 slash_command.go — 枚举 + 结构体**

定义以下类型（对齐 Python 全部枚举值）：
- `GatewaySlashCommand` (7 个值: /new_session, /mode, /switch, /skills, /skills_list, /branch, /rewind)
- `ModeSubcommand` (8 个值: agent, code, team, agent.plan, agent.fast, code.plan, code.normal, code.team)
- `SwitchSubcommand` (4 个值: plan, fast, normal, team)
- `ParsedControlAction` (13 个值: none, new_session_ok, new_session_bad, mode_ok, mode_bad, switch_ok, switch_bad, skills_ok, branch_ok, rewind_ok, rewind_bad, rewind_confirm, rewind_cancel)
- `ParsedChannelControl` struct (action, modeSubcommand, switchSubcommand, branchName, rewindTurn, rewindPendingTurn)
- `SlashScope` 类型 (Literal "gateway"/"client")
- `SlashCommandEntry` struct (id, canonicalText, scope, reqMethod, notes)
- `FirstBatchRegistry` 切片 (9 条，对齐 Python)
- 验证常量: `ValidModeLines`, `ValidSwitchLines`, `ValidModeSubcommands`, `ValidSwitchSubcommands`, `ControlMessageTexts`

- [ ] **Step 3: 实现 ParseChannelControlText 函数**

完整对齐 Python 的决策树（18 个分支），特别注意：
- `/rewind cancel` 必须在 `/rewind N` 之前检查
- `/rewind confirm N` 必须在 `/rewind N` 之前检查
- `/skills list` 使用 normalized（折叠空白）匹配
- 有效 mode/switch 行在 BAD 检查之前匹配

- [ ] **Step 4: 实现 IsControlLikeForIMBatching 函数**

对齐 Python 12 条逻辑，更宽松的匹配（前缀匹配即可返回 true）。

- [ ] **Step 5: 实现 FormatSkillsListForNotice 函数**

对齐 Python 6 步逻辑：错误→空列表→格式化（最多 maxItems 项）→截断描述 200 字符→超出提示。

- [ ] **Step 6: 编写 slash_command_test.go**

测试用例覆盖：
- ParseChannelControlText 全部 18 个分支
- IsControlLikeForIMBatching 边界值
- FormatSkillsListForNotice 各种 payload
- 枚举值查找/校验

- [ ] **Step 7: 运行测试确认通过**

```bash
go test ./internal/swarm/gateway/message_handler/command_parser/... -v
```

- [ ] **Step 8: 提交**

```bash
git add internal/swarm/gateway/message_handler/command_parser/
git commit -m "feat(message_handler): 添加 slash 命令解析器，对齐 Python slash_command.py"
```

---

## Task 2: channel_state.go — 渠道控制状态

**Files:**
- Create: `internal/swarm/gateway/message_handler/channel_state.go`
- Test: `internal/swarm/gateway/message_handler/channel_state_test.go`

**对齐 Python:** `message_handler.py` 中 `ChannelMode`, `ChannelControlState`, `_apply_channel_state`, `_get_or_create_channel_state`

- [ ] **Step 1: 实现 ChannelMode 枚举**

```go
type ChannelMode int
const (
    ChannelModeAgentPlan ChannelMode = iota // "agent.plan"
    ChannelModeAgentFast                    // "agent.fast"
    ChannelModeCodePlan                     // "code.plan"
    ChannelModeCodeNormal                   // "code.normal"
    ChannelModeCodeTeam                     // "code.team"
    ChannelModeTeam                         // "team"
)
```

包含 `String()`, `ParseChannelMode()`, `IsValidChannelMode()` 方法。

- [ ] **Step 2: 实现 ChannelControlState 结构体**

```go
type ChannelControlState struct {
    SessionID string
    Mode      ChannelMode
}
```

- [ ] **Step 3: 实现 ApplyChannelState 方法**

对齐 Python `_apply_channel_state`：
1. 检查是否为受控渠道类型（Web 渠道不在 _controlChannelTypes 中，但 mode/session 仍需处理）
2. 获取或创建渠道状态
3. 注入 session_id（Web 渠道：如果 state 有 sessionID 则覆盖 msg.SessionID）
4. 注入 mode 到 params["mode"]（setdefault 语义）

- [ ] **Step 4: 实现 GetOrCreateChannelState + GetChannelDefaultState**

对齐 Python `_get_or_create_channel_state` 和 `_get_channel_default_state`。

- [ ] **Step 5: 实现 GenerateChannelSessionID**

格式：`{channelID}_{hex_timestamp}_{6_random_hex}`，对齐 Python。

- [ ] **Step 6: 编写测试 + 运行 + 提交**

---

## Task 3: convert.go — E2AResponse→Message 转换

**Files:**
- Create: `internal/swarm/gateway/message_handler/convert.go`
- Test: `internal/swarm/gateway/message_handler/convert_test.go`

**对齐 Python:** `message_handler.py` 中 `_response_to_message`, `_chunk_to_message`, `_is_terminal_stream_chunk`, `_merge_agent_metadata`

- [ ] **Step 1: 实现 mergeAgentMetadata**

合并请求 metadata 和响应 metadata，响应优先。

- [ ] **Step 2: 实现 responseToMessage**

`AgentResponse → Message`，对齐 Python `_response_to_message`：
1. 合并 metadata
2. 提取 group_digital_avatar / enable_memory
3. 从 payload 提取 event_type → 如果合法 EventType 则构造事件消息
4. 否则构造响应消息（type=res, event_type=CHAT_FINAL）

- [ ] **Step 3: 实现 chunkToMessage**

`AgentResponseChunk → Message`，对齐 Python `_chunk_to_message`：
1. 提取 metadata 中的字段
2. 从 payload 提取 event_type
3. 构造事件消息（type=event）

- [ ] **Step 4: 实现 isTerminalStreamChunk**

对齐 Python `_is_terminal_stream_chunk`：is_complete=true 且 payload 为空或仅为 `{"is_complete": true}`。

- [ ] **Step 5: 编写测试 + 运行 + 提交**

---

## Task 4: at_file.go — @file 引用解析 + @agent 提及

**Files:**
- Create: `internal/swarm/gateway/message_handler/at_file.go`
- Test: `internal/swarm/gateway/message_handler/at_file_test.go`

**对齐 Python:** `message_handler.py` 中 `resolve_at_file_references`, `extract_agent_mentions`, `_normalize_structured_attachments`

- [ ] **Step 1: 实现 ResolveAtFileReferences**

对齐 Python `resolve_at_file_references`：解析 `@path` 引用，读取文件内容内联替换。默认限制 128KB。

- [ ] **Step 2: 实现 ExtractAgentMentions**

对齐 Python `extract_agent_mentions`：正则 `@agent-([a-zA-Z0-9_-]+)` 提取智能体名称。

- [ ] **Step 3: 实现 NormalizeStructuredAttachments**

对齐 Python `_normalize_structured_attachments`：处理结构化附件（图片、文件等）。

- [ ] **Step 4: 编写测试 + 运行 + 提交**

---

## Task 5: gateway_push — PushTransport + push 通道 + wire 编码

**Files:**
- Modify: `internal/swarm/server/gateway_push/transport.go`
- Modify: `internal/swarm/server/gateway_push/channel_transport.go`
- Modify: `internal/swarm/server/gateway_push/channel_transport_test.go`
- Create: `internal/swarm/server/gateway_push/wire.go`

**对齐 Python:** `jiuwenswarm/server/gateway_push/transport.py` + `wire.py`

- [ ] **Step 1: 在 transport.go 中添加 GatewayPushTransport 接口**

```go
type GatewayPushTransport interface {
    SendPush(msg map[string]any) error
}
```

- [ ] **Step 2: 在 ChannelTransport 中添加 pushCh**

```go
type ChannelTransport struct {
    sendCh  chan *e2a.E2AEnvelope   // Gateway → AgentServer
    recvCh  chan *e2a.E2AResponse   // AgentServer → Gateway (RPC 响应)
    pushCh  chan map[string]any      // AgentServer → Gateway (server_push)
    // ...
}
```

添加 `PushCh()` 和 `SendPush()` 方法。`SendPush` 实现 `GatewayPushTransport` 接口。

- [ ] **Step 3: 实现 wire.go — BuildServerPushWire**

对齐 Python `build_server_push_wire`：
1. 如果有 response_kind → 构造完整 E2AResponse
2. 否则 → 用 AgentResponseChunk 编码
3. 在 metadata 中设置 `E2A_WIRE_SERVER_PUSH_KEY = true`
4. 合并用户 metadata（排除内部键）
5. 注入 session_id

- [ ] **Step 4: 更新 doc.go + 编写测试 + 运行 + 提交**

---

## Task 6: message_handler.go — MessageHandler 核心结构重构

**Files:**
- Modify: `internal/swarm/gateway/message_handler/message_handler.go`
- Test: `internal/swarm/gateway/message_handler/message_handler_test.go`

**对齐 Python:** `message_handler.py` `__init__` + `handle_message` + `start_forwarding`/`stop_forwarding`

- [ ] **Step 1: 重写 MessageHandler 结构体**

```go
type MessageHandler struct {
    transport   gateway_push.AgentTransport
    pushTransport gateway_push.GatewayPushTransport
    channelMgr  *cm.ChannelManager

    userMessages  chan *schema.Message    // 入站
    robotMessages chan *schema.Message    // 出站

    running    atomic.Bool
    cancelFunc context.CancelFunc

    streamMu       sync.RWMutex
    streamTasks    map[string]context.CancelFunc  // requestID → cancel
    streamSessions map[string]string              // requestID → sessionID
    streamMetadata map[string]map[string]any      // requestID → metadata
    streamModes    map[string]string              // requestID → mode

    statesMu      sync.RWMutex
    channelStates map[string]*ChannelControlState

    controlChannelTypes map[string]bool
    sessionLastUserQuery map[string]string

    mu sync.Mutex
}
```

- [ ] **Step 2: 实现 NewMessageHandler + HandleInbound**

`NewMessageHandler` 创建实例，初始化双 channel。
`HandleInbound` 将消息写入 userMessages channel（对齐 Python `handle_message`）。

- [ ] **Step 3: 实现 StartForwarding / StopForwarding**

启动 forward_loop goroutine + outbound goroutine + dispatch goroutine。
停止时取消所有流式任务 + 关闭 channel。

- [ ] **Step 4: 实现 StartOutboundLoop**

从 robotMessages channel 读取 → ChannelManager.BroadcastToChannels。

- [ ] **Step 5: 实现 rememberUserQueryContext**

对齐 Python `_remember_user_query_context`：记录 chat.send 的 query 上下文。

- [ ] **Step 6: 编写测试 + 运行 + 提交**

---

## Task 7: forward_loop.go — 入站转发循环

**Files:**
- Create: `internal/swarm/gateway/message_handler/forward_loop.go`
- Test: `internal/swarm/gateway/message_handler/forward_loop_test.go`

**对齐 Python:** `message_handler.py` `_forward_loop` (L2163-L2556)

- [ ] **Step 1: 实现 forwardLoop 主循环**

```
for running:
    msg := <-userMessages
    
    1. _handle_channel_control(msg) → if handled, continue
    2. _apply_channel_state(msg) → 注入 session_id/mode
    3. CHAT_ANSWER → 非流式转发 + evolution 处理
    4. CHAT_CANCEL → cancel/supplement/pause/resume 分支
    5. chat.send → @file 解析 + @agent 提及 + E2A 转换 + Transport.Send
```

- [ ] **Step 2: 实现 CHAT_CANCEL 分支处理**

对齐 Python L2241-L2437：
- `intent=cancel` → `_cancel_agent_work_for_session`
- `intent=pause/resume` → 转发 AgentServer + 发送 interrupt_result 事件
- 有 `new_input` → supplement 逻辑（取消旧任务 + 发 supplement intent + 入队新任务）

- [ ] **Step 3: 实现 processStream 流式处理**

对齐 Python `process_stream`：
1. 从 Transport.Recv 逐个读取 E2AResponse
2. 跳过终止 chunk
3. 处理 evolution chunk
4. chunkToMessage → robotMessages
5. 流完成时发送 processing_status=false

- [ ] **Step 4: 实现 processNonStreamRequest 非流式处理**

对齐 Python `_process_non_stream_request`：同步发收 → responseToMessage → robotMessages。

- [ ] **Step 5: 编写测试 + 运行 + 提交**

测试使用 fakeTransport（mock AgentTransport）验证完整转发链路。

---

## Task 8: cancel.go — 取消/中断逻辑

**Files:**
- Create: `internal/swarm/gateway/message_handler/cancel.go`
- Test: `internal/swarm/gateway/message_handler/cancel_test.go`

**对齐 Python:** `message_handler.py` `_cancel_agent_work_for_session`, `_send_interrupt_result_notification`, `_send_processing_status`, `_send_stream_cancelled_notification`

- [ ] **Step 1: 实现 cancelAgentWorkForSession**

对齐 Python L381-L528：
1. 收集 session 关联的流式任务
2. 构造 cancel 请求 → Transport.Send
3. 取消流式任务
4. 转发 AgentServer 的 interrupt_result 响应

- [ ] **Step 2: 实现 sendInterruptResultNotification**

构造 interrupt_result 事件消息 → robotMessages。

- [ ] **Step 3: 实现 sendProcessingStatus**

构造 processing_status 事件消息 → robotMessages。

- [ ] **Step 4: 实现 sendStreamCancelledNotification + publishStreamCancelledFinal**

对齐 Python 流式取消通知逻辑。

- [ ] **Step 5: 编写测试 + 运行 + 提交**

---

## Task 9: slash_cmd.go — Slash 命令处理

**Files:**
- Create: `internal/swarm/gateway/message_handler/slash_cmd.go`
- Test: `internal/swarm/gateway/message_handler/slash_cmd_test.go`

**对齐 Python:** `message_handler.py` `_handle_channel_control`, `_new_session_cancel_and_notice`, `_mode_change_cancel_and_notice`, `_send_channel_notice`, `_skills_slash_notice`, `_branch_slash_notice`, `_rewind_slash_notice`

- [ ] **Step 1: 实现 handleChannelControl**

对齐 Python L615-L870：调用 `command_parser.ParseChannelControlText`，按 action 分发处理。Web 渠道不在受控类型中，直接返回 false。

- [ ] **Step 2: 实现 newSessionCancelAndNotice / modeChangeCancelAndNotice**

先 cancel 旧会话，再发送变更通知。

- [ ] **Step 3: 实现 sendChannelNotice**

构造系统提示消息 → robotMessages。

- [ ] **Step 4: 实现 skillsSlashNotice / branchSlashNotice / rewindSlashNotice**

构造对应 slash 命令的响应消息。

- [ ] **Step 5: 编写测试 + 运行 + 提交**

---

## Task 10: outbound.go — 出站循环 + handleAgentServerPush + cron push

**Files:**
- Create: `internal/swarm/gateway/message_handler/outbound.go`
- Test: `internal/swarm/gateway/message_handler/outbound_test.go`

**对齐 Python:** `message_handler.py` `_handle_agent_server_push`, `_handle_cron_push_payload`, `publish_robot_messages`, `consume_robot_messages`

- [ ] **Step 1: 实现 handleAgentServerPush**

对齐 Python L1610-L1672：
1. 解析 wire → AgentResponseChunk
2. 判断是否 cron push → handleCronPushPayload
3. 判断是否终止 chunk → 跳过
4. chunkToMessage → robotMessages

- [ ] **Step 2: 实现 handleCronPushPayload**

对齐 Python L1677-L1737：路由 cron action 到 CronController（当前 CronController 为 stub，直接返回空结果）。

- [ ] **Step 3: 实现 pushLoop**

从 ChannelTransport.PushCh() 持续读取 → handleAgentServerPush。

- [ ] **Step 4: 编写测试 + 运行 + 提交**

---

## Task 11: WebChannel.send 事件路由完善

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/web/web_connect.go`

**对齐 Python:** `web_connect.py` `send()` (L313-L414)

- [ ] **Step 1: 重写 WebChannel.Send 方法**

对齐 Python `send()` 的完整逻辑：
1. `msg.type == "res"` → 构造 res 帧
2. 确定事件名（默认 chat.final，优先 msg.EventType，fallback payload.event_type）
3. **full-payload 事件**：connection.ack, todo.updated, chat.tool_call, chat.tool_result, chat.processing_status, chat.interrupt_result, chat.evolution_status, chat.error, heartbeat.relay, context.usage, context.compression_state, chat.ask_user_question, chat.subtask_update, history.message, chat.session_result, chat.usage_metadata, chat.usage_summary, chat.file, team.*, harness.* → 透传完整 payload
4. **pure-text 事件**：chat.delta, chat.final 等 → 提取 content + session_id + role + member_name
5. cron 元数据附加（chat.final 时检查 payload.cron）

- [ ] **Step 2: 实现 interrupt_result 副作用**

发送 interrupt_result 事件后，自动广播 processing_status 事件：
- intent=pause/supplement/resume → is_processing=true
- intent=cancel → is_processing=false

- [ ] **Step 3: 编写测试 + 运行 + 提交**

---

## Task 12: WebChannel 转发 RPC 与 OnMessage 打通

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/web/web_handlers.go`

**对齐 Python:** `app_web_handlers.py` 中 chat.send/resume/interrupt/user_answer 的转发模式

- [ ] **Step 1: 修改 chat 类 RPC 处理函数**

将 `handleChatSend`, `handleChatResume`, `handleChatInterrupt`, `handleChatUserAnswer` 从"返回 ack + 模拟事件"改为"返回 ack + 触发 OnMessage 回调"。

```go
func handleChatSend(onMessage func(*schema.Message)) RPCHandlerFunc {
    return func(ctx context.Context, params map[string]any, sessionID string) (map[string]any, error) {
        // 构造 Message → 调用 onMessage 回调
        msg := schema.NewReqMessage("web", sessionID, schema.ReqMethodChatSend, paramsJSON, ...)
        if onMessage != nil {
            onMessage(msg)
        }
        return map[string]any{"accepted": true, "session_id": sessionID}, nil
    }
}
```

- [ ] **Step 2: 修改 NewAppRPCHandlers 签名**

将 `EventSender` 扩展为同时接收 `OnMessage` 回调：

```go
func NewAppRPCHandlers(sendEvent EventSender, onMessage func(*schema.Message)) *RPCDispatcher
```

- [ ] **Step 3: 在 WebChannel 中连接 OnMessage**

`NewWebChannel` 时传入 `onMessage` 回调，回调内部调用 `wc.onMessageCb(msg)`。

- [ ] **Step 4: 将 history.get 也改为转发模式**

对齐 Python：history.get 属于 `_FORWARD_REQ_METHODS`，返回 ack 后转发 AgentServer。

- [ ] **Step 5: 编写测试 + 运行 + 提交**

---

## Task 13: GatewayServer 组装 — 注入 Transport + MessageHandler

**Files:**
- Modify: `internal/swarm/gateway/app_gateway.go`
- Modify: `cmd/uapclaw/cmd.go`

**对齐 Python:** `app_gateway.py` 中 Gateway 启动流程

- [ ] **Step 1: 修改 NewGatewayServer 签名**

接受 `AgentTransport` + `GatewayPushTransport` 参数：

```go
func NewGatewayServer(cfg *config.Config, transport gateway_push.AgentTransport, pushTransport gateway_push.GatewayPushTransport) (*GatewayServer, error)
```

- [ ] **Step 2: 在 GatewayServer 中创建 MessageHandler**

```go
type GatewayServer struct {
    config        *config.Config
    router        *chi.Mux
    webChannel    *web.WebChannel
    channelMgr    *cm.ChannelManager
    msgHandler    *mh.MessageHandler
    transport     gateway_push.AgentTransport
    httpServer    *http.Server
}
```

- [ ] **Step 3: 在 Start 中连接 OnMessage 回调**

WebChannel.OnMessage → MessageHandler.HandleInbound。
启动 MessageHandler 的转发循环和出站循环。

- [ ] **Step 4: 修改 runAppCmd 创建 Transport**

```go
func runAppCmd() error {
    transport := gateway_push.NewChannelTransport()
    pushTransport := transport // ChannelTransport 同时实现两个接口
    gw, err := gateway.NewGatewayServer(cfg, transport, pushTransport)
    // ...
}
```

- [ ] **Step 5: 在 Stop 中优雅关闭**

停止顺序：WebChannel → MessageHandler → Transport → HTTP Server。

- [ ] **Step 6: 编写测试 + 运行 + 提交**

---

## Task 14: 更新 doc.go + IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `internal/swarm/gateway/message_handler/doc.go`
- Modify: `internal/swarm/server/gateway_push/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 message_handler/doc.go 文件目录**

添加所有新建文件到文件目录树。

- [ ] **Step 2: 更新 gateway_push/doc.go 文件目录**

添加 wire.go 和 push 相关说明。

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 章节状态**

- 11.3 MessageHandler: ☐→✅
- 10.3.21 GatewayPush Transport: ☐→✅（补充 push 通道）
- 11.9 GatewayServer: 更新状态
- 12.7 统一启动器: 更新状态

- [ ] **Step 4: 提交**

---

## 后续延后实现

| 逻辑 | 原因 |
|------|------|
| Gateway hooks (UserPromptSubmit, SessionStart) | 依赖 extensions 包 |
| IM inbound/outbound pipeline | 依赖数字分身模块 |
| evolution 审批 auto-accept 完整逻辑 | 基础状态追踪已实现，auto-accept 简化 |
| _handle_agent_server_push 的 ACP session alias | ACP 渠道后续 |
| WebSocketTransport 实现 | 跨进程模式后续 |
