# AgentClient 完整实现 + Transport 层重构设计

> **背景：** 当前实现违反了 Gateway↔Server 只通过 Transport 交互的设计原则。Gateway 直接 import server 包（ServerReadyWaiter 接口、*AgentServer），绕过 AgentClient 直接操作 Transport。Transport 层暴露了 3 通道内部实现细节（sendCh/recvCh/pushCh），未封装成 Python WS 的单连接模型，导致 AgentClient 无法在 WebSocketTransport 下复用。本重构对齐 Python，恢复正确分层。

## 1. 问题诊断

### 1.1 当前违规

| 文件 | 违规内容 |
|------|----------|
| `gateway_push/transport.go` | `AgentTransport` 接口暴露 `*E2AEnvelope` / `*E2AResponse` 强类型，`GatewayPushTransport` 拆出独立推送接口，3 通道未封装 |
| `gateway_push/channel_transport.go` | 3 个 channel（sendCh/recvCh/pushCh）暴露内部实现，`SendPush` 写 `map[string]any` 未编码成 E2A wire 格式 |
| `web/web_connect.go` | `import server` 包，使用 `ServerReadyWaiter` 接口 |
| `gateway/app_gateway.go` | `import server` 包，持有 `*server.AgentServer` |
| `server/agent_server.go` | `serverReadyCh` 在 Server 侧（应在 Gateway 侧） |
| `server/agent_server.go` | `ServerReadyWaiter` 接口（不再需要） |
| `routing/agent_client.go` | 空壳，无调用方 |
| `message_handler/forward_loop.go` | 直接调 `transport.Send/Recv`，无 request_id 路由，多请求并发响应错乱 |

### 1.2 当前数据流（错误）

```
WebChannel → serverReadyWaiter.WaitServerReady()  ← 直接引用 server 包
  → AgentServer.serverReadyCh                       ← 就绪状态在 Server 侧

MessageHandler → transport.Send(*E2AEnvelope)       ← 绕过 AgentClient
             → transport.Recv() ←chan *E2AResponse  ← 共享通道，多请求响应错乱

AgentServer → transport.SendPush(map[string]any)    ← 走独立 pushCh，未编码成 E2A wire
```

### 1.3 Python 正确数据流

```
Wire 层（Transport）: ws.send(json_str) / ws.recv() → json_str
  - 单 WS 连接，所有消息（请求/响应/推送/事件）走同一连接

应用层（AgentClient）:
  发送: E2AEnvelope.to_dict() → json.dumps() → transport.send()
  接收: transport.recv() → json_str → json.loads() → dict
    ├── type=="event" → 事件帧（connection.ack）
    ├── metadata[E2A_WIRE_SERVER_PUSH_KEY] → server_push → onServerPush 回调
    └── 其余 → parse_agent_server_wire_unary/chunk → 按 request_id 路由

AgentServer 侧:
  - 普通响应: E2AResponse → json.dumps() → ws.send()
  - send_push: build_server_push_wire(msg) → json.dumps() → ws.send()  ← 同一 WS 连接
  - connection.ack: {"type":"event","event":"connection.ack",...} → ws.send()  ← 同一 WS 连接
```

## 2. 重构目标

**原则 1：Gateway ↔ Server 只通过 Transport 交互，零模块引用。**

**原则 2：Transport 层封装为纯 byte 管道，对齐 Python WS 的单连接模型。** Transport 不感知 E2A 协议语义，只负责 byte 传输。AgentClient 拥有全部协议逻辑，换底层 Transport（ChannelTransport → WebSocketTransport）时 AgentClient 零修改。

```
Transport 层（纯 byte 管道）:
  AgentTransport {
    Send(ctx, []byte) error       // 对齐 Python ws.send(json_str)
    Recv() ←chan []byte           // 对齐 Python ws.recv() → json_str
    Close() error
  }
  ChannelTransport: sendCh chan []byte + recvCh chan []byte
  WebSocketTransport: ws.WriteMessage() / ws.ReadMessage()

应用层（AgentClient，协议逻辑）:
  发送: json.Marshal(envelope) → transport.Send()
  接收: transport.Recv() → json.Unmarshal() → map[string]any → 区分:
    ├── type=="event" → handleEventFrame（connection.ack → serverReady）
    ├── metadata[E2A_WIRE_SERVER_PUSH_KEY] → onServerPush 回调
    └── 其余 → e2a.ResponseFromMap() → 按 request_id 路由到 messageQueues

Server 侧:
  普通响应: json.Marshal(e2aResponse) → 写入 recvCh
  send_push: buildServerPushWire(msg) → json.Marshal() → 写入 recvCh
  connection.ack: json.Marshal(eventFrame) → 写入 recvCh
```

## 3. Transport 层重构

### 3.1 AgentTransport 接口

```go
// AgentTransport Gateway ↔ AgentServer 的传输抽象
// 对齐 Python WebSocket: send(json_str) / recv() → json_str
// 不感知 E2A 协议语义，只负责 JSON 字节传输
type AgentTransport interface {
    // Send 发送 JSON 字节到对端（对齐 Python ws.send(json_str)）
    Send(ctx context.Context, data []byte) error
    // Recv 返回接收通道，每条消息为 JSON 字节（对齐 Python ws.recv()）
    Recv() (<-chan []byte, error)
    // Close 关闭连接
    Close() error
}
```

- **消灭 `GatewayPushTransport` 接口**：不再有独立的 `SendPush` / `SetServerPushHandler`
- **消灭 `pushCh`**：所有服务端→客户端消息统一走 `recvCh`
- AgentClient 通过 `Recv()` 收到 `[]byte` 后自行 `json.Unmarshal` + 应用层区分

### 3.2 ChannelTransport 实现

```go
type ChannelTransport struct {
    sendCh chan []byte   // 客户端→服务端（请求 JSON）
    recvCh chan []byte   // 服务端→客户端（响应/推送/事件 JSON，统一）
    mu     sync.Mutex
    closed bool
}
```

**变化：**
- `sendCh` 类型从 `chan *e2a.E2AEnvelope` 改为 `chan []byte`
- `recvCh` 类型从 `chan *e2a.E2AResponse` 改为 `chan []byte`
- 删除 `pushCh chan map[string]any`
- 删除 `onServerPushCb` / `pushCancel` / `drainPushCh` / `SetServerPushHandler` / `SendPush`
- 新增 `RecvCh()` 方法返回 `chan<- []byte`（供 AgentServer 写入）

### 3.3 AgentServer 写入方式

AgentServer 通过 `transport.RecvCh()` 获取 `chan<- []byte`，所有写入都序列化为 JSON 字节：

```go
// 普通响应
data, _ := json.Marshal(e2aResp)
transport.RecvCh() <- data

// send_push（对齐 Python build_server_push_wire）
wire := buildServerPushWire(msg)  // 编码为 E2AResponse + metadata[E2A_WIRE_SERVER_PUSH_KEY]
data, _ := json.Marshal(wire)
transport.RecvCh() <- data

// connection.ack
ackFrame := map[string]any{
    "type": "event", "event": "connection.ack",
    "payload": map[string]any{"status": "ready"},
}
data, _ := json.Marshal(ackFrame)
transport.RecvCh() <- data
```

### 3.4 buildServerPushWire（对齐 Python gateway_push/wire.py）

```go
// buildServerPushWire 将 send_push 入参编码为 E2A 响应线 dict
// 对齐 Python: jiuwenswarm/server/gateway_push/wire.py (build_server_push_wire)
func buildServerPushWire(msg map[string]any) map[string]any {
    responseKind := ""
    if rk, ok := msg["response_kind"].(string); ok {
        responseKind = strings.TrimSpace(rk)
    }
    var wire map[string]any
    if responseKind != "" {
        // 有 response_kind → 编码为完整 E2AResponse
        e2aResp := e2a.NewE2AResponse()
        // ... 填充字段 ...
        e2aResp.Metadata[e2a.E2AWireServerPushKey] = true
        wire = e2aResp.ToMap()
    } else {
        // 无 response_kind → 编码为 chunk 形
        // 对齐 Python encode_agent_chunk_for_wire
        // ...
        wire["metadata"] = map[string]any{e2a.E2AWireServerPushKey: true}
    }
    return wire
}
```

## 4. AgentClient 完整设计

### 4.1 结构体

```go
// AgentClient Gateway 与 AgentServer 的协议层客户端
// 对齐 Python: jiuwenswarm/gateway/routing/agent_client.py (WebSocketAgentServerClient)
// 依赖 AgentTransport 接口（byte 管道），不依赖具体 Transport 实现
type AgentClient struct {
    // transport 传输层（byte 管道）
    transport gateway_push.AgentTransport
    // serverReady AgentServer 是否已发送 connection.ack 确认就绪
    serverReady bool
    // serverReadyMu 保护 serverReady
    serverReadyMu sync.RWMutex
    // serverReadyCh 就绪通知通道（close 通知所有等待者）
    serverReadyCh chan struct{}
    // onServerPush Agent 主动推送回调（对齐 Python _on_server_push）
    onServerPush func(msg map[string]any)
    // sendMu 发送互斥锁（对齐 Python self._lock）
    sendMu sync.Mutex
    // messageQueues 消息分发队列：request_id → 响应等待者（对齐 Python _message_queues）
    messageQueues map[string]chan map[string]any
    // messageQueuesMu 保护 messageQueues
    messageQueuesMu sync.Mutex
    // cancelledRequests 已取消但可能有残余消息的 request_id（对齐 Python _cancelled_request_ids）
    cancelledRequests map[string]struct{}
    // running 是否正在运行
    running bool
    // runningMu 保护 running
    runningMu sync.RWMutex
    // cancelFunc 接收循环取消函数
    cancelFunc context.CancelFunc
}
```

**注意：`messageQueues` 的 value 类型是 `chan map[string]any`，不是 `chan *e2a.E2AResponse`。** 和 Python 的 `asyncio.Queue` 一样存原始 dict，解码在消费方做。这样可以统一处理 E2A wire / legacy 格式。

### 4.2 方法

| 方法 | 对齐 Python | 说明 |
|------|-------------|------|
| `NewAgentClient(transport)` | `__init__` | 创建实例 |
| `Connect(ctx)` | `connect(uri)` | 启动 receiverLoop，等 connection.ack（5s 超时不报错） |
| `Disconnect()` | `disconnect()` | 停止 receiverLoop，清理 |
| `ServerReady() bool` | `server_ready` property | 返回就绪状态 |
| `WaitServerReady(ctx) bool` | 无（Go 新增） | 阻塞等待就绪，供 WebChannel 使用 |
| `SendRequest(ctx, envelope)` | `send_request(envelope)` | 非流式：注册 queue → Send → 等 queue → parse_wire_unary |
| `SendRequestStream(ctx, envelope)` | `send_request_stream(envelope)` | 流式：注册 queue → Send → 逐 chunk 从 queue 读 → parse_wire_chunk |
| `SetServerPushHandler(handler)` | `set_server_push_handler` | 注册 push 回调 |
| `SetOrUpdateServerConfig(config, env)` | `set_or_update_server_config` | 预留，暂不实现 |

### 4.3 Connect 流程（对齐 Python connect(uri)）

Python `connect(uri)`:
1. `self._server_ready = False`
2. `self._ws = await websockets.connect(uri)` — 建立 WS 连接
3. `raw = await asyncio.wait_for(self._ws.recv(), timeout=5.0)` — 等首帧
4. 首帧是 connection.ack → `self._server_ready = True`
5. `self._receiver_task = asyncio.create_task(self._message_receiver_loop())`

Go `Connect(ctx)`:
1. `ac.serverReady = False`
2. 启动 `receiverLoop` goroutine（从 `transport.Recv()` 持续读取 `[]byte`，`json.Unmarshal` → `map[string]any`，应用层区分：事件帧 / server_push / 正常响应）
3. 等待 `serverReadyCh` 或 5s 超时（对齐 Python `timeout=5.0`，超时不报错，仅 warn）

**注意：单进程模式下 AgentServer 在 goroutine 中启动，Connect 不需要主动连入（Transport 已共享），只需启动 receiverLoop 并等待 AgentServer 发来的 connection.ack。**

### 4.4 SendRequest 流程（对齐 Python send_request）

Python:
1. `envelope.is_stream = False`
2. `queue = asyncio.Queue(); self._message_queues[rid] = queue`
3. `await self._ws.send(json.dumps(envelope.to_dict()))` — WS 发送
4. `data = await asyncio.wait_for(queue.get(), timeout=600)` — 等队列
5. `resp = parse_agent_server_wire_unary(data)` — 解码

Go:
1. `envelope.IsStream = false`
2. `queue := make(chan map[string]any, 1); ac.messageQueues[rid] = queue`
3. `data, _ := json.Marshal(envelope); ac.transport.Send(ctx, data)` — 发送 JSON 字节
4. `msg := <-queue` 或 `select` + 600s timeout — 等队列
5. `parseAgentServerWireUnary(msg)` — 解码（对齐 Python parse_agent_server_wire_unary）

### 4.5 SendRequestStream 流程（对齐 Python send_request_stream）

与 SendRequest 类似，但：
- `envelope.IsStream = true`
- 循环从 queue 读取，直到 `chunk.is_complete`
- `is_complete` 后再读 0.7s（对齐 Python `_STREAM_TRAILING_MESSAGE_GRACE_SECONDS`）

### 4.6 receiverLoop（对齐 Python _message_receiver_loop）

```go
func (ac *AgentClient) receiverLoop(ctx context.Context) {
    recvCh, _ := ac.transport.Recv()
    for {
        select {
        case <-ctx.Done():
            return
        case data, ok := <-recvCh:
            if !ok { return }
            // json.Unmarshal → map[string]any（对齐 Python json.loads(raw)）
            var msg map[string]any
            if err := json.Unmarshal(data, &msg); err != nil {
                logger.Warn(...).Err(err).Msg("接收消息 JSON 解码失败")
                continue
            }
            // 1. 事件帧？（connection.ack 等）
            if msg["type"] == "event" {
                ac.handleEventFrame(msg)
                continue
            }
            // 2. server_push？（对齐 Python metadata[E2A_WIRE_SERVER_PUSH_KEY]）
            if meta, ok := msg["metadata"].(map[string]any); ok {
                if _, has := meta[e2a.E2AWireServerPushKey]; has {
                    if ac.onServerPush != nil {
                        go ac.onServerPush(msg)  // 对齐 Python asyncio.create_task
                    }
                    continue
                }
            }
            // 3. 正常响应 → 按 request_id 路由到 messageQueues
            ac.routeToQueue(msg)
        }
    }
}
```

**对比 Python `_message_receiver_loop`：**
| Python | Go |
|--------|-----|
| `raw = await ws.recv()` | `data := <-recvCh` |
| `data = json.loads(raw)` | `json.Unmarshal(data, &msg)` |
| `data.get("type") == "event"` — 未处理（connect 中读首帧） | `msg["type"] == "event"` → handleEventFrame |
| `metadata.get(E2A_WIRE_SERVER_PUSH_KEY)` → `on_server_push(data)` | `msg["metadata"][E2A_WIRE_SERVER_PUSH_KEY]` → `go onServerPush(msg)` |
| 按 `request_id` → `_message_queues[rid].put(data)` | 按 `request_id` → `messageQueues[rid] <- msg` |

### 4.7 handleEventFrame

```go
func (ac *AgentClient) handleEventFrame(msg map[string]any) {
    event, _ := msg["event"].(string)
    switch event {
    case "connection.ack":
        ac.serverReadyMu.Lock()
        ac.serverReady = true
        ac.serverReadyMu.Unlock()
        close(ac.serverReadyCh)
        logger.Info(...).Msg("收到 connection.ack，AgentServer 已就绪")
    default:
        logger.Warn(...).Str("event", event).Msg("收到未知事件帧")
    }
}
```

### 4.8 routeToQueue

```go
func (ac *AgentClient) routeToQueue(msg map[string]any) {
    rid := wireRequestIDKey(msg["request_id"])  // 对齐 Python _wire_request_id_key
    ac.messageQueuesMu.Lock()
    if _, cancelled := ac.cancelledRequests[rid]; cancelled {
        ac.messageQueuesMu.Unlock()
        logger.Debug(...).Str("request_id", rid).Msg("收到已取消请求的残余消息，已丢弃")
        return
    }
    if queue, ok := ac.messageQueues[rid]; ok && rid != "" {
        ac.messageQueuesMu.Unlock()
        select {
        case queue <- msg:
        default:
            logger.Warn(...).Str("request_id", rid).Msg("消息队列已满，丢弃")
        }
    } else {
        ac.messageQueuesMu.Unlock()
        logger.Debug(...).Str("request_id", rid).Msg("收到无目标队列的消息")
    }
}
```

### 4.9 drainAndRemoveQueue（对齐 Python _drain_and_remove_queue）

```go
func (ac *AgentClient) drainAndRemoveQueue(rid string) {
    ac.messageQueuesMu.Lock()
    queue, ok := ac.messageQueues[rid]
    if !ok {
        ac.messageQueuesMu.Unlock()
        return
    }
    // 1. 标记为已取消
    ac.cancelledRequests[rid] = struct{}{}
    // 2. 删除队列注册
    delete(ac.messageQueues, rid)
    // 3. 排空残余消息
    for {
        select {
        case <-queue:
        default:
            goto done
        }
    }
done:
    ac.messageQueuesMu.Unlock()
    // 4. 延迟清理已取消标记（对齐 Python 2s 延迟）
    go ac.delayedCleanupCancelledRequestID(rid)
}

func (ac *AgentClient) delayedCleanupCancelledRequestID(rid string) {
    time.Sleep(2 * time.Second)
    ac.messageQueuesMu.Lock()
    delete(ac.cancelledRequests, rid)
    ac.messageQueuesMu.Unlock()
}
```

## 5. 其他文件改动

### 5.1 server/agent_server.go — 删除 serverReady 相关，改用写入 recvCh

删除：
- `serverReady bool` / `serverReadyMu` / `serverReadyCh` 字段
- `ServerReady()` / `WaitServerReady()` 方法
- `ServerReadyWaiter` 接口

修改 `Start()`:
```go
// 旧：close(s.serverReadyCh)
// 新：发送 connection.ack 事件帧到 recvCh
ackFrame := map[string]any{
    "type": "event", "event": "connection.ack",
    "payload": map[string]any{"status": "ready"},
}
ackData, _ := json.Marshal(ackFrame)
s.transport.RecvCh() <- ackData
```

AgentServer 的 `send_push` 调用也需要改为：
```go
// 旧：s.transport.SendPush(msg)
// 新：编码为 E2A wire 后写入 recvCh
wire := buildServerPushWire(msg)
wireData, _ := json.Marshal(wire)
s.transport.RecvCh() <- wireData
```

### 5.2 gateway/app_gateway.go — 删除 server 包引用

删除：
- `import server` 包
- `agentServer *server.AgentServer` 字段
- `NewGatewayServer` 的 `agentServer` 参数
- `wc.SetServerReadyWaiter(agentServer)`

新增：
- `agentClient *routing.AgentClient` 字段
- `NewGatewayServer` 接受 `agentClient` 参数
- 将 `agentClient` 传给 WebChannel 和 MessageHandler

### 5.3 web/web_connect.go — 删除 server 包引用，改用 AgentClient

删除：
- `import server` 包
- `serverReadyWaiter server.ServerReadyWaiter` 字段
- `SetServerReadyWaiter` 方法

新增：
- `agentClient *routing.AgentClient` 字段
- HandleWebSocket 中：`if !wc.agentClient.WaitServerReady(r.Context()) { return }`

### 5.4 message_handler — 改用 AgentClient 通信

`message_handler.go` 中：
- 删除 `transport` 和 `pushTransport` 字段
- 新增 `agentClient *routing.AgentClient` 字段
- `NewMessageHandler` 改为接受 `agentClient`
- `StartForwarding()` 中不再注册 push handler 到 transport，改为调 `agentClient.SetServerPushHandler(mh.HandleServerPush)`

`forward_loop.go` 中：
- 删除 `mh.transport.Send(ctx, envelope)` + `mh.transport.Recv()` 直接调用
- `handleChatSend` / `handleChatCancel` 等改为调 `mh.agentClient.SendRequest` / `mh.agentClient.SendRequestStream`
- 不再需要 `processStream` / `processNonStreamRequest` 直接操作 transport

### 5.5 cmd/uapclaw/cmd.go — 重构启动流程

```go
// 创建 ChannelTransport
transport := gateway_push.NewChannelTransport()

// 创建 AgentClient（Gateway 侧协议客户端）
agentClient := routing.NewAgentClient(transport)

// 创建 AgentServer（Server 侧）
agentServer := server.NewAgentServer(cfg, transport)

// 先启动 AgentServer（goroutine），它会 Init AgentManager → 发 connection.ack 到 recvCh → 进入消费循环
go func() { agentServer.Start(ctx) }()

// 创建 GatewayServer，注入 AgentClient
gs := gateway.NewGatewayServer(cfg, agentClient)

// 启动 GatewayServer（HTTP + WebSocket）
gs.Start(ctx)  // 内部会调 agentClient.Connect() 启动 receiverLoop + 等 connection.ack
```

### 5.6 gateway_push/channel_transport.go — 重构为 byte 管道

- `sendCh` / `recvCh` 类型改为 `chan []byte`
- 删除 `pushCh` / `onServerPushCb` / `pushCancel`
- 删除 `SetServerPushHandler` / `SendPush` / `drainPushCh`
- `Send` 改为接受 `[]byte`
- `Recv` 返回 `<-chan []byte`
- `RecvCh()` 返回 `chan<- []byte`（供 AgentServer 写入）

### 5.7 gateway_push/transport.go — 接口改为 byte 管道

- `AgentTransport` 改为 `Send([]byte)` / `Recv() ←chan []byte`
- 删除 `GatewayPushTransport` 接口

### 5.8 新增 gateway_push/wire.go — server_push 编码

新增 `buildServerPushWire(msg map[string]any) map[string]any`，对齐 Python `gateway_push/wire.py`。

### 5.9 server 侧 handler 改动

当前 AgentServer 的 handler 通过 `transport.RecvCh()` 写入 `*e2a.E2AResponse`。重构后需要改为 `json.Marshal(e2aResponse)` → 写入 `[]byte`。

搜索所有 `s.transport.RecvCh() <- resp` 的调用点，逐一改为 `json.Marshal` 后写入。

## 6. 依赖关系（重构后）

```
cmd/uapclaw → gateway → routing → e2a, schema, gateway_push
                      → web → routing (仅 AgentClient 引用，不再 import server)
                      → message_handler → routing (调 AgentClient)
           → server → e2a, schema, gateway_push, runtime

gateway 不再 import server ✓
web 不再 import server ✓
server 不再 import gateway ✓
AgentClient 只依赖 AgentTransport 接口，不依赖 ChannelTransport 具体 ✓
零循环依赖 ✓
```

## 7. 不在本次范围

- AgentClient 的 `SetOrUpdateServerConfig` — 预留，暂不实现
- WebSocket 版 Transport + AgentClient 跨进程模式 — 后续实现，但接口已就绪
- MessageHandler 的 slash command / channel state — 不变
