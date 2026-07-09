# AgentClient 完整实现 + serverReady 重构设计

> **背景：** 当前实现违反了 Gateway↔Server 只通过 Transport 交互的设计原则。Gateway 直接 import server 包（ServerReadyWaiter 接口、*AgentServer），绕过 AgentClient 直接操作 Transport。本重构对齐 Python，恢复正确分层。

## 1. 问题诊断

### 1.1 当前违规

| 文件 | 违规内容 |
|------|----------|
| `web/web_connect.go` | `import server` 包，使用 `ServerReadyWaiter` 接口 |
| `gateway/app_gateway.go` | `import server` 包，持有 `*server.AgentServer` |
| `server/agent_server.go` | `serverReadyCh` 在 Server 侧（应在 Gateway 侧） |
| `server/agent_server.go` | `ServerReadyWaiter` 接口（不再需要） |
| `routing/agent_client.go` | 空壳，无调用方 |

### 1.2 当前数据流（错误）

```
WebChannel → serverReadyWaiter.WaitServerReady()  ← 直接引用 server 包
  → AgentServer.serverReadyCh                       ← 就绪状态在 Server 侧

MessageHandler → transport.Send()                   ← 绕过 AgentClient
             → transport.Recv()                     ← 绕过 AgentClient
```

### 1.3 Python 正确数据流

```
AgentServer → WS send {type:"event", event:"connection.ack", payload:{status:"ready"}}
  → agent_client.connect() 读首帧 → _server_ready = True
  → Web _on_connect → 检查 agent_client.server_ready → 发 ack 给前端

WebChannel → _on_message → agent_client.send_request(envelope)  ← 唯一通信出口
```

## 2. 重构目标

**原则：Gateway ↔ Server 只通过 Transport 交互，零模块引用。**

```
Gateway 侧:
  AgentClient (routing/) — 协议层客户端，对齐 Python WebSocketAgentServerClient
    ├── Connect() — 等 connection.ack，设置 serverReady
    ├── ServerReady() — 返回就绪状态
    ├── SendRequest() — 非流式请求/响应
    ├── SendRequestStream() — 流式请求/逐 chunk 响应
    ├── SetServerPushHandler() — 注册 push 回调
    └── 底层: ChannelTransport.Send/Recv/SendPush

  MessageHandler — 路由层，调用 AgentClient 做 E2A 通信
    ├── forwardLoop → agentClient.SendRequest / SendRequestStream
    └── 不再直接操作 Transport

Server 侧:
  AgentServer — 通过 Transport.SendPush() 发 connection.ack
    ├── Start() → Init AgentManager → transport.SendPush(ack) → 消费循环
    └── 删除 serverReady/serverReadyCh/WaitServerReady/ServerReadyWaiter
```

## 3. AgentClient 完整设计

### 3.1 结构体

```go
// AgentClient Gateway 与 AgentServer 的协议层客户端
// 对齐 Python: jiuwenswarm/gateway/routing/agent_client.py (WebSocketAgentServerClient)
// 底层载体：ChannelTransport（单进程），替代 Python 的 WebSocket
type AgentClient struct {
    // transport 进程内传输通道
    transport *gateway_push.ChannelTransport
    // serverReady AgentServer 是否已发送 connection.ack 确认就绪
    serverReady bool
    // serverReadyMu 保护 serverReady
    serverReadyMu sync.RWMutex
    // serverReadyCh 就绪通知通道（close 通知所有等待者）
    serverReadyCh chan struct{}
    // onServerPush Agent 主动推送回调
    onServerPush func(msg map[string]any)
    // messageQueues 消息分发队列：request_id → 响应等待者
    messageQueues map[string]chan *e2a.E2AResponse
    // messageQueuesMu 保护 messageQueues
    messageQueuesMu sync.Mutex
    // cancelledRequests 已取消但可能有残余消息的 request_id
    cancelledRequests map[string]struct{}
    // running 是否正在运行
    running bool
    // runningMu 保护 running
    runningMu sync.RWMutex
    // cancelFunc 接收循环取消函数
    cancelFunc context.CancelFunc
}
```

### 3.2 方法

| 方法 | 对齐 Python | 说明 |
|------|-------------|------|
| `NewAgentClient(transport)` | `__init__` | 创建实例 |
| `Connect(ctx)` | `connect(uri)` | 启动 receiverLoop，等 connection.ack |
| `Disconnect()` | `disconnect()` | 停止 receiverLoop，清理 |
| `ServerReady() bool` | `server_ready` property | 返回就绪状态 |
| `WaitServerReady(ctx) bool` | 无（Go 新增） | 阻塞等待就绪，供 WebChannel 使用 |
| `SendRequest(ctx, envelope)` | `send_request(envelope)` | 非流式：transport.Send → 等 messageQueue → 解码 |
| `SendRequestStream(ctx, envelope)` | `send_request_stream(envelope)` | 流式：transport.Send → 逐 chunk 从 messageQueue 读 → 解码 |
| `SetServerPushHandler(handler)` | `set_server_push_handler` | 注册 push 回调，调 transport.SetServerPushHandler |
| `SetOrUpdateServerConfig(config, env)` | `set_or_update_server_config` | 预留，暂不实现 |

### 3.3 Connect 流程（对齐 Python connect(uri)）

Python `connect(uri)`:
1. `self._server_ready = False`
2. `self._ws = await websockets.connect(uri)` — 建立 WS 连接
3. `raw = await asyncio.wait_for(self._ws.recv(), timeout=5.0)` — 等首帧
4. 首帧是 connection.ack → `self._server_ready = True`
5. `self._receiver_task = asyncio.create_task(self._message_receiver_loop())`

Go `Connect(ctx)`:
1. `ac.serverReady = False`
2. 启动 `receiverLoop` goroutine（从 `transport.Recv()` 持续读取 E2AResponse，按 request_id 分发到 messageQueues）
3. 注册 push handler：`transport.SetServerPushHandler(ac.handleServerPush)`
4. 在 push handler 中检测 connection.ack：收到 `{type:"event", event:"connection.ack"}` → `ac.serverReady = True; close(ac.serverReadyCh)`
5. 等待 serverReady 或 ctx 超时（5s，对齐 Python `timeout=5.0`）

**注意：单进程模式下 AgentServer 在 goroutine 中启动，Connect 不需要主动连入（Transport 已共享），只需启动 receiverLoop 并等待 connection.ack push 消息。**

### 3.4 SendRequest 流程（对齐 Python send_request）

Python:
1. `queue = asyncio.Queue(); self._message_queues[rid] = queue`
2. `await self._ws.send(json.dumps(envelope.to_dict()))` — WS 发送
3. `data = await asyncio.wait_for(queue.get(), timeout=600)` — 等队列
4. `resp = parse_agent_server_wire_unary(data)` — 解码

Go:
1. `queue := make(chan *e2a.E2AResponse, 1); ac.messageQueues[rid] = queue`
2. `ac.transport.Send(ctx, envelope)` — ChannelTransport 发送
3. `resp := <-queue` 或 `select` + timeout — 等队列
4. `e2a.E2AResponseToAgentResponse(resp)` — 解码

### 3.5 receiverLoop（对齐 Python _message_receiver_loop）

Python:
- `while running: raw = await ws.recv(); data = json.loads(raw)`
- 如果 `metadata[E2A_WIRE_SERVER_PUSH_KEY]` → 调 `onServerPush`
- 否则按 `request_id` 路由到 `message_queues[rid]`

Go:
- `for { resp := <-recvCh }` — 从 `transport.Recv()` 读
- 如果 `resp.Metadata[E2A_WIRE_SERVER_PUSH_KEY]` → 调 `onServerPush`
- 否则按 `resp.RequestID` 路由到 `messageQueues[rid]`

### 3.6 server_push 中 connection.ack 的检测

AgentServer 通过 `transport.SendPush()` 发送：
```go
transport.SendPush(map[string]any{
    "type":    "event",
    "event":   "connection.ack",
    "payload": map[string]any{"status": "ready"},
})
```

AgentClient 的 push handler 收到此消息后：
```go
if msg["type"] == "event" && msg["event"] == "connection.ack" {
    ac.serverReadyMu.Lock()
    ac.serverReady = true
    ac.serverReadyMu.Unlock()
    close(ac.serverReadyCh)
}
```

## 4. 其他文件改动

### 4.1 server/agent_server.go — 删除 serverReady 相关，改用 SendPush

删除：
- `serverReady bool` / `serverReadyMu` / `serverReadyCh` 字段
- `ServerReady()` / `WaitServerReady()` 方法
- `ServerReadyWaiter` 接口

修改 `Start()`:
```go
// 旧：close(s.serverReadyCh)
// 新：s.transport.SendPush(map[string]any{
//     "type": "event", "event": "connection.ack",
//     "payload": map[string]any{"status": "ready"},
// })
```

### 4.2 gateway/app_gateway.go — 删除 server 包引用

删除：
- `import server` 包
- `agentServer *server.AgentServer` 字段
- `NewGatewayServer` 的 `agentServer` 参数
- `wc.SetServerReadyWaiter(agentServer)`

新增：
- `agentClient *routing.AgentClient` 字段
- `NewGatewayServer` 接受 `agentClient` 参数
- 将 `agentClient` 传给 WebChannel 和 MessageHandler

### 4.3 web/web_connect.go — 删除 server 包引用，改用 AgentClient

删除：
- `import server` 包
- `serverReadyWaiter server.ServerReadyWaiter` 字段
- `SetServerReadyWaiter` 方法

新增：
- `agentClient *routing.AgentClient` 字段（或通过 GatewayServer 传递检查函数）
- HandleWebSocket 中：`if !wc.agentClient.ServerReady() { return }`

### 4.4 message_handler — 改用 AgentClient 通信

`forward_loop.go` 中：
- 删除 `mh.transport.Send(ctx, envelope)` + `mh.transport.Recv()` 直接调用
- 改为 `mh.agentClient.SendRequest(ctx, envelope)` 或 `mh.agentClient.SendRequestStream(ctx, envelope)`
- MessageHandler 结构体新增 `agentClient *routing.AgentClient` 字段
- MessageHandler 不再持有 `transport` 和 `pushTransport` 字段

### 4.5 cmd/uapclaw/cmd.go — 重构启动流程

```go
// 创建 ChannelTransport
transport := gateway_push.NewChannelTransport()

// 创建 AgentClient（Gateway 侧协议客户端）
agentClient := routing.NewAgentClient(transport)

// 创建 AgentServer（Server 侧）
agentServer := server.NewAgentServer(cfg, transport)

// 先启动 AgentServer（goroutine），它会 SendPush connection.ack
go func() { agentServer.Start(ctx) }()

// 创建 GatewayServer，注入 AgentClient
gs := gateway.NewGatewayServer(cfg, agentClient)

// 启动 GatewayServer（HTTP + WebSocket）
gs.Start(ctx)  // 内部会调 agentClient.Connect() 等待 connection.ack
```

## 5. 依赖关系（重构后）

```
cmd/uapclaw → gateway → routing → e2a, schema, gateway_push
                      → web → routing (仅 AgentClient 引用，不再 import server)
                      → message_handler → routing (调 AgentClient)
           → server → e2a, schema, gateway_push, runtime

gateway 不再 import server ✓
web 不再 import server ✓
server 不再 import gateway ✓
零循环依赖 ✓
```

## 6. 不在本次范围

- AgentClient 的 `SetOrUpdateServerConfig` — 预留，暂不实现
- WebSocket 版 AgentClient（跨进程模式） — 后续实现
- MessageHandler 的 slash command / channel state — 不变
