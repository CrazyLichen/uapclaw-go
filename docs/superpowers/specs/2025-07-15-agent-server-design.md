# AgentServer 设计文档

> 目标：实现 10.3.1 AgentServer，从 ChannelTransport.SendCh() 消费 Gateway 发来的 E2AEnvelope，
> 按 req_method 分发到具体 handler，响应通过 RecvCh() 写回 Gateway。
> 完整对齐 Python AgentWebSocketServer 的请求接收、处理、响应转发逻辑，
> 依赖的 JiuWenClaw/AgentManager/TeamManager/Runner 等先 stub。

---

## 1. 定位与职责

### 1.1 Python 对应

Python 中 `AgentWebSocketServer` 是 WS 服务端，监听 Gateway 的 WebSocket 连接，接收 E2AEnvelope 请求并分发处理。

Go 单进程模式中，AgentServer 不需要 WS 监听，而是作为 **ChannelTransport 消费者**：

| Python AgentWebSocketServer | Go AgentServer |
|---|---|
| WS 服务端，监听端口 | ChannelTransport.SendCh() 消费者 |
| WS 连接后发 connection.ack | 设置 serverReady 标志（进程内直接通知） |
| WS 帧收发 | Go channel 读写（SendCh/RecvCh/PushCh） |
| asyncio 并发 | goroutine 并发 |
| send_lock 保证 WS 发送原子性 | 不需要（channel 写入天然串行） |

### 1.2 核心职责

1. 从 `SendCh()` 读取 E2AEnvelope
2. 解析为 AgentRequest（`e2a.E2AToAgentRequest`）
3. 按 req_method 分发到具体 handler
4. handler 处理后，响应编码为 E2AResponse 写入 `RecvCh()`
5. 管理 serverReady 状态，通知 Gateway 可以向前端发 connection.ack
6. 追踪流式任务（按 session_id），支持 interrupt 精准取消
7. 流式心跳（keepalive chunk，10s 间隔）

---

## 2. 结构体设计

### 2.1 AgentServer

```go
// AgentServer Agent 服务端，消费 ChannelTransport 请求并分发处理。
//
// 单进程模式：从 ChannelTransport.SendCh() 读取 E2AEnvelope，
// 按 req_method 分发到具体 handler，响应写入 RecvCh()。
// 对齐 Python AgentWebSocketServer，适配进程内 Go channel 传输。
type AgentServer struct {
    // config 配置
    config *config.Config

    // transport 进程内传输（提供 SendCh/RecvCh/PushCh）
    transport *gateway_push.ChannelTransport

    // agentManager Agent 实例管理器（10.3.12，先 stub）
    agentManager *runtime.AgentManager

    // sessionStreamTasks 流式任务追踪：session_id → cancel func
    sessionStreamTasks   map[string]context.CancelFunc
    sessionStreamTasksMu sync.RWMutex

    // serverReady AgentServer 是否就绪
    serverReady   bool
    serverReadyMu sync.RWMutex
    // serverReadyCh 通知 channel，容量 1，发一次后不再发
    serverReadyCh chan struct{}

    // schedulerService 定时任务调度服务（先 stub）
    schedulerService SchedulerService

    // running 运行状态
    running   bool
    runningMu sync.RWMutex
}
```

### 2.2 构造与生命周期

```go
// NewAgentServer 创建 AgentServer
func NewAgentServer(cfg *config.Config, transport *gateway_push.ChannelTransport) *AgentServer

// Start 启动 AgentServer（阻塞，直到 ctx 取消）
// 1. 初始化 AgentManager（stub）
// 2. 设置 serverReady = true，通知 Gateway
// 3. 进入 SendCh 消费循环
func (s *AgentServer) Start(ctx context.Context) error

// Stop 优雅关闭
// 1. 取消所有流式任务
// 2. 关闭 AgentManager
// 3. 清理资源
func (s *AgentServer) Stop() error

// ServerReady 返回 serverReady 状态
func (s *AgentServer) ServerReady() bool

// WaitServerReady 阻塞等待 serverReady（用于 Gateway 侧等待）
func (s *AgentServer) WaitServerReady(ctx context.Context) bool
```

---

## 3. serverReady 与 connection.ack 时机

### 3.1 Python 机制（跨进程）

```
AgentServer 启动 → WS 监听
    → Gateway WS Client 连接
    → AgentServer 发 connection.ack {status:"ready"}（WS 帧）
    → Gateway.agent_client.server_ready = True
    → 前端连接 WebChannel → _on_connect 检查 server_ready=True
    → 发 connection.ack {session_id, mode, tools, protocol_version} 给前端
```

### 3.2 Go 机制（单进程）

```
runAppCmd 启动:
  1. transport := NewChannelTransport()
  2. agentServer := NewAgentServer(cfg, transport)
  3. go agentServer.Start(ctx)              // 先启动 AgentServer
  4. gatewayServer := NewGatewayServer(cfg, transport, pushTransport, agentServer)
  5. gatewayServer.Start(ctx)               // 再启动 Gateway

AgentServer.Start():
  → 初始化 AgentManager(stub)
  → 设置 serverReady = true
  → serverReadyCh <- struct{}{}              // 通知 Gateway
  → 进入 SendCh 消费循环

WebChannel.HandleWebSocket():
  → 等待 agentServer.WaitServerReady(ctx)   // 阻塞等待
  → 发 connection.ack {session_id, mode, tools, protocol_version} 给前端
```

### 3.3 前端收到的 connection.ack 帧格式（对齐 Python）

```json
{
    "type": "event",
    "event": "connection.ack",
    "payload": {
        "session_id": "sess_xxxxxxxx_abcd12",
        "mode": "BUILD",
        "tools": [],
        "protocol_version": "1.0"
    }
}
```

对齐 Python `app_web_handlers._on_connect` 中的 payload 字段：
- `session_id`：由 `MakeSessionID()` 生成，格式 `sess_{hex_ts}_{6位随机hex}`
- `mode`：硬编码 `"BUILD"`（Python 也是硬编码）
- `tools`：空列表 `[]`（Python 也是空列表）
- `protocol_version`：硬编码 `"1.0"`

---

## 4. 请求处理核心流程

### 4.1 SendCh 消费循环

```go
func (s *AgentServer) startConsumeLoop(ctx context.Context) {
    for {
        select {
        case envelope := <-s.transport.SendCh():
            // 每个请求独立 goroutine 处理（并发）
            go s.handleEnvelope(ctx, envelope)
        case <-ctx.Done():
            return
        }
    }
}
```

### 4.2 handleEnvelope 主流程

```
收到 E2AEnvelope
  → E2AToAgentRequest(envelope) 构造 AgentRequest
  → ACP channel 特殊处理（注入 client_capabilities）
  → 触发 before_chat_request hook（仅 CHAT_SEND/CHAT_RESUME/CHAT_ANSWER）
  → 按 req_method 分发到具体 handler
  → 未命中显式分支 → 兜底到 handleUnary / handleStream
```

对齐 Python `_handle_message` 的完整逻辑。

### 4.3 非流式响应 handleUnary

```
1. 特殊方法拦截：
   - INITIALIZE → handleInitialize
   - SESSION_CREATE → handleSessionCreate
   - SESSION_FORK → handleSessionFork
   - ACP_TOOL_RESPONSE → handleACPToolResponse
2. 解析 mode: _applyResolvedModeToRequest(request)
3. 获取 Agent 实例: agentManager.GetAgent(channelID, mode, projectDir, subMode)
4. code 模式特殊处理: switchMode + state 持久化
5. 调用 Agent: resp := agent.ProcessMessage(request)
6. 编码为 E2AResponse: wire := e2a.EncodeAgentResponseForWire(resp, request.RequestID)
7. 写入 RecvCh: transport.RecvCh() <- wire
```

### 4.4 流式响应 handleStream

```
1. 记录流式任务: sessionStreamTasks[sessionID] = cancel
2. 获取 Agent 实例（同 handleUnary 步骤 2-4）
3. 启动心跳 goroutine: 每 10s 发 keepalive chunk（sequence=-1）
4. 流式调用 Agent:
   for chunk := range agent.ProcessMessageStream(request):
     chunkCount++
     通知心跳有真实数据
     wire := e2a.EncodeAgentChunkForWire(chunk, request.RequestID, chunkCount-1)
     transport.RecvCh() <- wire
5. 停止心跳，清理 sessionStreamTasks
```

### 4.5 流式心跳 keepalive chunk

对齐 Python `_handle_stream` 中 `_heartbeat_loop`：

- 间隔：10 秒（`_STREAM_HEARTBEAT_INTERVAL_SECONDS`）
- 触发条件：流式过程中，如果 10 秒内没有真实 chunk，发送 keepalive
- keepalive chunk 格式：

```go
AgentResponseChunk{
    RequestID:  requestID,
    ChannelID:  channelID,
    Payload:    map[string]any{"event_type": "keepalive"},
    IsComplete: false,
}
// sequence = -1（特殊值标识心跳）
```

编码后写入 RecvCh。

---

## 5. RPC 方法分发完整清单

### 5.1 按实现策略分类

#### A. 完整本地实现（纯文件系统/配置，无外部依赖）— 约 30 个

| 方法 | 核心逻辑 |
|------|---------|
| `session.list` | 扫描 sessions 目录，读 metadata.json |
| `session.rename` | 重命名 metadata.json 字段 |
| `history.get` | 读 history.json + 分页（非流式+流式） |
| `team.history.get` | 读 team history 记录 |
| `command.add_dir` | 写入受信目录配置 |
| `command.chrome` | 空操作，返回 ok=true |
| `command.diff` | 读 history.json 中 tool diff |
| `command.resume` | mock 返回 |
| `command.session` | mock 返回 |
| `command.status` | 会话统计 + 配置路径 + 版本/模型诊断 |
| `config.cache_clear` | 清除配置缓存 |
| `hooks.list` | 读 hooks 配置 |
| `permissions.tools.get/set/update/delete` | config.yaml permissions.tools 读写 |
| `permissions.rules.get/create/update/delete` | config.yaml permissions.rules 读写 |
| `permissions.approval_overrides.get/delete` | config.yaml approval_overrides 读写 |
| `extensions.list` | RailManager 列扩展 |
| `extensions.import` | 导入扩展 |
| `extensions.delete` | 删除扩展 |
| `harness.packages.get` | AutoHarnessService 包信息 |
| `harness.packages.scan` | 扫描运行时扩展 |
| `harness.packages.delete` | 删除 harness 包 |
| `schedule.check_config` | 调度配置检查 |
| `schedule.update_config` | 调度配置更新 |
| `schedule.list` | 列调度任务 |
| `schedule.status` | 任务状态 |
| `schedule.logs` | 任务日志 |
| `agents.list` | AgentConfigService 列 agents |
| `agents.get` | 获取单个 agent |
| `agents.tools_list` | 可用工具列表 |
| `acp.tool_response` | ACP JSON-RPC 响应匹配 |

#### B. 需要 AgentManager（先 stub AgentManager）— 约 15 个

| 方法 | 依赖 | stub 策略 |
|------|------|----------|
| `session.create` | AgentManager.createSession | stub: 创建目录+metadata |
| `session.delete` | Runner.release, TeamManager | stub: 仅删除目录 |
| `session.switch` | TeamManager.prepareSessionSwitch | stub: 返回 ok=true |
| `session.fork` | AgentManager.getAgent | stub: 返回 NOT_IMPLEMENTED |
| `command.model` | AgentManager.reloadAgentsConfig | stub: 设置环境变量+返回 ok=true |
| `command.mcp` | AgentManager.reloadAgentsConfig, MCP | stub: 返回空 MCP 配置 |
| `command.sandbox` | AgentManager.recreateAgent, JiuwenBox | stub: 返回 NOT_IMPLEMENTED |
| `agent.reload_config` | AgentManager.reloadAgentsConfig | stub: 返回 ok=true |
| `extensions.toggle` | AgentManager.getAgent | stub: 返回 ok=true |
| `initialize` | AgentManager.initialize | stub: 返回默认 capabilities |
| `agents.create` | AgentManager.reloadAgentsConfig | stub: 返回 NOT_IMPLEMENTED |
| `agents.update` | AgentManager.reloadAgentsConfig | stub: 返回 NOT_IMPLEMENTED |
| `agents.delete` | AgentManager.reloadAgentsConfig | stub: 返回 NOT_IMPLEMENTED |
| `agents.enable` | AgentManager.reloadAgentsConfig | stub: 返回 NOT_IMPLEMENTED |
| `agents.disable` | AgentManager.reloadAgentsConfig | stub: 返回 NOT_IMPLEMENTED |

#### C. 需要 Agent 实例（JiuWenClaw/DeepAgent）— 先 stub — 约 6 个

| 方法 | 需要的 Agent 方法 | stub 策略 |
|------|-----------------|----------|
| `command.compact` | agent.compressContext | stub: 返回 {ok:true, compressed:false} |
| `command.context` | agent.getContextUsage | stub: 返回 {usage:0, limit:0} |
| `command.recap` | agent.generateRecap | stub: 返回空回顾 |
| `session.rewind` | agent.rewindSessionContext | stub: 仅截断 history.json |
| `session.rewind_and_restore` | agent.rewindSessionContext | stub: 同上 |
| `session.rewind_context` | agent.rewindSessionContext | stub: 返回 NOT_IMPLEMENTED |

#### D. 需要 TeamManager/Runner/Browser/AutoHarnessService — 先 stub — 约 9 个

| 方法 | 依赖 | stub 策略 |
|------|------|----------|
| `team.delete` | Runner, TeamManager | stub: 返回 ok=true |
| `team.snapshot` | TeamManager | stub: 返回空快照 |
| `browser.start` | browser_start_client | stub: 返回 NOT_IMPLEMENTED |
| `browser.runtime_restart` | browser runtime | stub: 返回 NOT_IMPLEMENTED |
| `harness.packages.activate` | Agent 实例 + AutoHarnessService | stub: 返回 ok=true |
| `harness.packages.deactivate` | Agent 实例 + AutoHarnessService | stub: 返回 ok=true |
| `schedule.create` | AutoHarnessService + Agent | stub: 返回 NOT_IMPLEMENTED |
| `schedule.run` | AutoHarnessService + Agent | stub: 返回 NOT_IMPLEMENTED |
| `schedule.cancel` | AutoHarnessService + Agent | stub: 返回 ok=true |
| `schedule.delete` | AutoHarnessService + Agent | stub: 返回 ok=true |

#### E. 兜底到 handleUnary/handleStream — 先 stub JiuWenClaw — 3 个

| 方法 | 处理路径 | stub 策略 |
|------|---------|----------|
| `chat.send` | handleStream → agent.ProcessMessageStream | stub: 返回固定 AgentResponseChunk |
| `chat.resume` | handleStream → agent.ProcessMessageStream | stub: 同上 |
| `chat.user_answer` | handleStream → agent.ProcessMessageStream | stub: 同上 |

#### F. chat.interrupt — 本地处理

| 方法 | 核心逻辑 |
|------|---------|
| `chat.interrupt` | 取消 sessionStreamTasks[session_id] + 转发 interrupt 给 Agent |

---

## 6. 响应编码与写入

### 6.1 非流式响应

```go
// 构造 AgentResponse
resp := &schema.AgentResponse{
    RequestID:  request.RequestID,
    ChannelID:  request.ChannelID,
    SessionID:  request.SessionID,
    OK:         true,
    Payload:    resultPayload,
    IsComplete: true,
}

// 编码为 E2AResponse wire 格式
wire := e2a.EncodeAgentResponseForWire(resp, request.RequestID)

// 写入 RecvCh
s.transport.RecvCh() <- wire
```

### 6.2 流式响应

```go
// 逐 chunk 编码
chunk := &schema.AgentResponseChunk{
    RequestID:  request.RequestID,
    ChannelID:  request.ChannelID,
    SessionID:  request.SessionID,
    Payload:    chunkPayload,
    IsComplete: false,
    Sequence:   chunkCount - 1,
}

wire := e2a.EncodeAgentChunkForWire(chunk, request.RequestID, chunkCount-1)
s.transport.RecvCh() <- wire

// 最后发送 is_complete=true 的终止标记
terminalChunk := schema.NewTerminalChunk(request.RequestID, request.ChannelID, request.SessionID)
wire = e2a.EncodeAgentChunkForWire(terminalChunk, request.RequestID, chunkCount)
s.transport.RecvCh() <- wire
```

对齐 Python `encode_agent_response_for_wire` / `encode_agent_chunk_for_wire`。

---

## 7. Interrupt 精准取消

```go
// handleCancel 处理 chat.interrupt
func (s *AgentServer) handleCancel(ctx context.Context, request *schema.AgentRequest) {
    sessionID := request.SessionID

    // 1. 取消流式 goroutine
    s.sessionStreamTasksMu.RLock()
    cancel, ok := s.sessionStreamTasks[sessionID]
    s.sessionStreamTasksMu.RUnlock()
    if ok && cancel != nil {
        cancel()  // 精准取消该 session 的流式任务
    }

    // 2. 转发 interrupt 给 Agent（通过 AgentManager）
    agent := s.agentManager.GetAgentNoWait(request.ChannelID, mode, projectDir, subMode)
    if agent != nil {
        resp := agent.ProcessMessage(request)  // 调用 interrupt
        // 编码写入 RecvCh
    }
}
```

对齐 Python `_handle_cancel`：先取消 asyncio Task，再转发 interrupt 给 Agent。

---

## 8. 文件组织

```
internal/swarm/server/
├── agent_server.go          # AgentServer 结构体 + Start/Stop/ServerReady
├── agent_server_test.go     # 单元测试
├── handle_envelope.go       # handleEnvelope 主分发 + handleUnary/handleStream
├── handle_session.go        # session.list/rename/delete/rewind/create/fork 等
├── handle_command.go        # command.* 系列
├── handle_team.go           # team.delete/snapshot/history.get
├── handle_history.go        # history.get（非流式+流式）
├── handle_permissions.go    # permissions.* 全部
├── handle_agents.go         # agents.* 全部
├── handle_extensions.go     # extensions.* + hooks.list
├── handle_harness.go        # harness.packages.* + schedule.*
├── handle_browser.go        # browser.start/runtime_restart
├── handle_config.go         # config.cache_clear + agent.reload_config
├── handle_initialize.go     # initialize + acp.tool_response
├── adapter/                 # 已有：适配器层
├── gateway_push/            # 已有：传输层
└── runtime/                 # 已有：运行时管理
    ├── session_manager.go   # 已有
    ├── agent_manager.go     # 新建（10.3.12，先 stub）
    └── jiowenclaw.go        # 新建（10.3.2，先 stub）
```

---

## 9. runAppCmd 改动

```go
func runAppCmd(cmd *cobra.Command, args []string) error {
    // ... 已有的 workspace/logger/dotenv/config 初始化 ...

    transport := gateway_push.NewChannelTransport()
    pushTransport := transport

    // 新增：创建并启动 AgentServer
    agentServer := server.NewAgentServer(cfg, transport)
    go agentServer.Start(ctx)  // 先启动 AgentServer

    // 修改：GatewayServer 接收 agentServer 引用（用于 WaitServerReady）
    gs, err := gateway.NewGatewayServer(cfg, transport, pushTransport, agentServer)

    // GatewayServer.Start() 中 WebChannel 会等待 agentServer.WaitServerReady()

    return gs.Start(ctx)
}
```

---

## 10. GatewayServer 改动

### 10.1 新增 agentServer 引用

```go
type GatewayServer struct {
    // ... 已有字段 ...
    agentServer *server.AgentServer  // 新增：用于 WaitServerReady
}
```

### 10.2 WebChannel connection.ack 时机变更

```go
func (wc *WebChannel) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
    conn, err := wc.upgrader.Upgrade(w, r, nil)
    // ...

    // 变更：等待 AgentServer 就绪后再发 connection.ack
    if wc.agentServer != nil {
        if !wc.agentServer.WaitServerReady(ctx) {
            // 等待超时或 ctx 取消，跳过 connection.ack
            logger.Warn(logComponent).Msg("AgentServer 未就绪，跳过 connection.ack")
            return
        }
    }

    // 发送 connection.ack（对齐 Python payload 格式）
    sessionID := MakeSessionID()
    ackPayload := map[string]any{
        "session_id":       sessionID,
        "mode":             "BUILD",
        "tools":            []any{},
        "protocol_version": "1.0",
    }
    ackFrame := NewEventFrame("connection.ack", ackPayload, 0, "")
    // ... 发送 ...
}
```

---

## 11. IMPLEMENTATION_PLAN.md 章节对应

| 本次实现 | 章节状态变更 |
|---------|-------------|
| `server/agent_server.go` + 分发逻辑 | 10.3.1 ☐→✅ |
| `runtime/agent_manager.go`（stub） | 10.3.12 ☐→🔄 |
| `runtime/jiowenclaw.go`（stub） | 10.3.2 ☐→🔄 |
| `cmd/uapclaw/cmd.go` 改动 | 12.7 🔄→✅ |
| WebChannel connection.ack 时机变更 | 11.14 补充 |
| serverReady 机制 | 新增子步骤 |

---

## 12. 不在本次范围

| 章节 | 原因 |
|------|------|
| AgentManager 完整实现 | 10.3.12 后续 |
| JiuWenClaw 完整实现 | 10.3.2 后续 |
| DeepAdapter ProcessMessageImpl 回填 | 依赖 10.3.7-11 |
| WebSocketTransport | 跨进程模式后续 |
| before_chat_request hook | 后续 |
| code 模式 switch_mode/state 持久化 | 依赖 JiuWenClaw |
