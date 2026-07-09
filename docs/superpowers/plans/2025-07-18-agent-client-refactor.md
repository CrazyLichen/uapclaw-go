# AgentClient 完整实现 + Transport 层重构 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 重构 Transport 层为纯 byte 管道（对齐 Python WS 单连接模型），完整实现 AgentClient（对齐 Python WebSocketAgentServerClient），消除 gateway→server 直接引用，将 server_ready 移至 Gateway 侧。

**Architecture:** Transport 层退化为 `Send([]byte)` / `Recv() ←chan []byte` 纯 byte 管道，消灭 `GatewayPushTransport` 接口和 `pushCh`。所有协议语义（E2A 编解码、server_push 检测、request_id 路由、connection.ack）在 AgentClient 侧。AgentServer 侧的 `writeResponse`/`writeChunk`/`writeErrorResponse`/`sendKeepalive` 改为 `json.Marshal` 后写入 `recvCh []byte`，`send_push` 编码为 E2A wire 后写入 `recvCh`，`connection.ack` 编码为事件帧后写入 `recvCh`。

**Tech Stack:** Go, existing e2a/schema/gateway_push packages, testify for tests

**Design spec:** `docs/superpowers/specs/2025-07-16-agent-client-refactor-design.md`

---

## File Structure

| Action | File | Responsibility |
|--------|------|----------------|
| Modify | `internal/swarm/server/gateway_push/transport.go` | AgentTransport 接口改为 byte 管道，删除 GatewayPushTransport |
| Modify | `internal/swarm/server/gateway_push/channel_transport.go` | 通道类型改为 `chan []byte`，删除 pushCh/SendPush/SetServerPushHandler/drainPushCh |
| Modify | `internal/swarm/server/gateway_push/channel_transport_test.go` | 适配 byte 管道，删除 SendPush/SetServerPushHandler 相关测试 |
| Create | `internal/swarm/server/gateway_push/wire.go` | `buildServerPushWire` 对齐 Python gateway_push/wire.py |
| Create | `internal/swarm/server/gateway_push/wire_test.go` | buildServerPushWire 测试 |
| Modify | `internal/swarm/server/agent_server.go` | 删除 serverReady 相关字段/方法/接口，Start() 发 connection.ack 到 recvCh |
| Modify | `internal/swarm/server/agent_server_test.go` | 适配：删除 WaitServerReady 测试，改为验证 recvCh 收到 connection.ack |
| Modify | `internal/swarm/server/handle_envelope.go` | writeResponse/writeChunk/writeErrorResponse/sendKeepalive 改为 json.Marshal→写入 recvCh |
| Modify | `internal/swarm/server/handle_envelope_test.go` | 适配：recvCh 类型从 E2AResponse 改为 []byte，需 json.Unmarshal |
| Modify | `internal/swarm/gateway/routing/agent_client.go` | 完整实现 AgentClient（Connect/Disconnect/ServerReady/SendRequest/SendRequestStream/receiverLoop） |
| Modify | `internal/swarm/gateway/routing/agent_client_test.go` | 完整 AgentClient 测试 |
| Modify | `internal/swarm/gateway/message_handler/message_handler.go` | 删除 transport/pushTransport 字段，新增 agentClient 字段 |
| Modify | `internal/swarm/gateway/message_handler/forward_loop.go` | 改为调 agentClient.SendRequest/SendRequestStream |
| Modify | `internal/swarm/gateway/message_handler/cancel.go` | sendCancelToAgent 改为调 agentClient |
| Modify | `internal/swarm/gateway/message_handler/message_handler_test.go` | 适配：改为 agentClient 构造 |
| Modify | `internal/swarm/gateway/message_handler/forward_loop_test.go` | 适配 |
| Modify | `internal/swarm/gateway/message_handler/cancel_test.go` | 适配 |
| Modify | `internal/swarm/gateway/app_gateway.go` | 删除 server import，改为 agentClient |
| Modify | `internal/swarm/gateway/app_gateway_test.go` | 适配 |
| Modify | `internal/swarm/gateway/channel_manager/web/web_connect.go` | 删除 server import，改为 agentClient |
| Modify | `cmd/uapclaw/cmd.go` | 启动流程：创建 AgentClient，传给 GatewayServer |

---

### Task 1: Transport 接口改为 byte 管道

**Files:**
- Modify: `internal/swarm/server/gateway_push/transport.go`
- Modify: `internal/swarm/server/gateway_push/channel_transport.go`
- Modify: `internal/swarm/server/gateway_push/channel_transport_test.go`
- Modify: `internal/swarm/server/gateway_push/doc.go`

- [ ] **Step 1: 修改 `transport.go` 接口定义**

将 `AgentTransport` 改为 byte 管道，删除 `GatewayPushTransport`：

```go
package gateway_push

import "context"

// ──────────────────────────── 结构体 ────────────────────────────

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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 修改 `channel_transport.go` 实现**

```go
package gateway_push

import (
	"context"
	"errors"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChannelTransport 进程内传输实现，基于 Go channel 在 Gateway 与 AgentServer 之间传递 JSON 字节
// 对齐 Python WebSocket 单连接模型：所有消息（请求/响应/推送/事件）走同一 recvCh
//
// 对应 Python: jiuwenswarm/server/gateway_push/transport.py (进程内路径)
type ChannelTransport struct {
	// sendCh 请求通道：Gateway → AgentServer（JSON 字节）
	sendCh chan []byte
	// recvCh 响应通道：AgentServer → Gateway（JSON 字节，统一承载响应/推送/事件）
	recvCh chan []byte
	// mu 保护 closed 标志的并发访问
	mu sync.Mutex
	// closed 是否已关闭
	closed bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultSendBufferSize 请求通道默认缓冲大小
	defaultSendBufferSize = 64
	// defaultRecvBufferSize 响应通道默认缓冲大小
	defaultRecvBufferSize = 128
)

// logComponent 日志组件
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrTransportClosed 传输通道已关闭
	ErrTransportClosed = errors.New("传输通道已关闭")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewChannelTransport 创建 ChannelTransport 实例，使用默认缓冲大小
func NewChannelTransport() *ChannelTransport {
	return NewChannelTransportWithBuffer(defaultSendBufferSize, defaultRecvBufferSize)
}

// NewChannelTransportWithBuffer 创建 ChannelTransport 实例，指定通道缓冲大小
func NewChannelTransportWithBuffer(sendBuf, recvBuf int) *ChannelTransport {
	if sendBuf <= 0 {
		sendBuf = defaultSendBufferSize
	}
	if recvBuf <= 0 {
		recvBuf = defaultRecvBufferSize
	}
	t := &ChannelTransport{
		sendCh: make(chan []byte, sendBuf),
		recvCh: make(chan []byte, recvBuf),
	}
	logger.Info(logComponent).
		Str("event_type", "channel_transport_created").
		Int("send_buf", sendBuf).
		Int("recv_buf", recvBuf).
		Msg("ChannelTransport 已创建")
	return t
}

// Send 发送 JSON 字节到 AgentServer（对齐 Python ws.send）
func (t *ChannelTransport) Send(ctx context.Context, data []byte) error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		logger.Warn(logComponent).
			Str("event_type", "channel_transport_send_closed").
			Msg("发送失败：传输已关闭")
		return ErrTransportClosed
	}
	t.mu.Unlock()

	select {
	case t.sendCh <- data:
		logger.Debug(logComponent).
			Str("event_type", "channel_transport_send").
			Int("bytes", len(data)).
			Msg("JSON 字节已发送")
		return nil
	case <-ctx.Done():
		logger.Warn(logComponent).
			Str("event_type", "channel_transport_send_cancelled").
			Err(ctx.Err()).
			Msg("发送失败：上下文已取消")
		return ctx.Err()
	}
}

// Recv 返回接收通道（对齐 Python ws.recv）
func (t *ChannelTransport) Recv() (<-chan []byte, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		logger.Warn(logComponent).
			Str("event_type", "channel_transport_recv_closed").
			Msg("接收失败：传输已关闭")
		return nil, ErrTransportClosed
	}
	return t.recvCh, nil
}

// Close 关闭传输通道，释放资源
func (t *ChannelTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil
	}
	t.closed = true
	close(t.sendCh)
	close(t.recvCh)
	logger.Info(logComponent).
		Str("event_type", "channel_transport_closed").
		Msg("ChannelTransport 已关闭")
	return nil
}

// SendCh 返回请求通道的读取端，供 AgentServer 消费
func (t *ChannelTransport) SendCh() <-chan []byte {
	return t.sendCh
}

// RecvCh 返回响应通道的写入端，供 AgentServer 写入响应
func (t *ChannelTransport) RecvCh() chan<- []byte {
	return t.recvCh
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 3: 重写 `channel_transport_test.go`**

删除所有 `SendPush`/`SetServerPushHandler`/`GatewayPushTransport` 相关测试，适配 byte 管道。测试要点：
- `TestNewChannelTransport` 基本创建
- `TestChannelTransport_SendRecv` 发送和接收 `[]byte`
- `TestChannelTransport_Send_关闭后返回错误`
- `TestChannelTransport_Recv_关闭后返回错误`
- `TestChannelTransport_Close_重复关闭`
- 接口合规：`var _ AgentTransport = (*ChannelTransport)(nil)`

- [ ] **Step 4: 更新 `doc.go`**

更新包文档反映新的 byte 管道设计。

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/gateway_push/...
```

- [ ] **Step 6: 测试验证**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/gateway_push/... -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/swarm/server/gateway_push/
git commit -m "refactor: Transport 接口改为 byte 管道，消灭 GatewayPushTransport 和 pushCh"
```

---

### Task 2: 新增 buildServerPushWire

**Files:**
- Create: `internal/swarm/server/gateway_push/wire.go`
- Create: `internal/swarm/server/gateway_push/wire_test.go`

- [ ] **Step 1: 编写 `wire.go`**

对齐 Python `jiuwenswarm/server/gateway_push/wire.py` 的 `build_server_push_wire`：

```go
package gateway_push

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponentWire wire 编码日志组件
const logComponentWire = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildServerPushWire 将 send_push 入参编码为 E2A 响应线 dict
// 对齐 Python: jiuwenswarm/server/gateway_push/wire.py (build_server_push_wire)
// 编码后的 dict 带有 metadata[E2A_WIRE_SERVER_PUSH_KEY] = True 标记
func BuildServerPushWire(msg map[string]any) map[string]any {
	responseKind := ""
	if rk, ok := msg["response_kind"].(string); ok {
		responseKind = strings.TrimSpace(rk)
	}

	if responseKind != "" {
		// 有 response_kind → 编码为完整 E2AResponse wire
		return buildServerPushWireWithResponseKind(msg, responseKind)
	}
	// 无 response_kind → 编码为 chunk 形 wire
	return buildServerPushWireChunk(msg)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildServerPushWireWithResponseKind 有 response_kind 的 server_push 编码
func buildServerPushWireWithResponseKind(msg map[string]any, responseKind string) map[string]any {
	requestID := ""
	if rid, ok := msg["request_id"]; ok {
		requestID = wireRequestIDKey(rid)
	}

	e2aResp := e2a.NewE2AResponse()
	e2aResp.ResponseID = requestID
	e2aResp.RequestID = requestID
	e2aResp.Sequence = 0
	e2aResp.IsFinal = true
	e2aResp.Status = e2a.E2AResponseStatusSucceeded
	e2aResp.ResponseKind = responseKind
	e2aResp.EnsureTimestamp()

	if channel, ok := msg["channel_id"].(string); ok && channel != "" {
		e2aResp.Channel = channel
	}
	if sessionID, ok := msg["session_id"].(string); ok {
		e2aResp.SessionID = sessionID
	}
	if body, ok := msg["body"].(map[string]any); ok {
		e2aResp.Body = body
	}
	if metadata, ok := msg["metadata"].(map[string]any); ok {
		e2aResp.Metadata = metadata
	}
	// 设置 server_push 标记
	e2aResp.Metadata[e2a.E2AWireServerPushKey] = true

	wire := e2aResp.ToMap()
	return wire
}

// buildServerPushWireChunk 无 response_kind 的 server_push 编码（chunk 形）
func buildServerPushWireChunk(msg map[string]any) map[string]any {
	requestID := ""
	if rid, ok := msg["request_id"]; ok {
		requestID = wireRequestIDKey(rid)
	}
	channelID := ""
	if cid, ok := msg["channel_id"].(string); ok {
		channelID = cid
	}
	var payload map[string]any
	if p, ok := msg["payload"].(map[string]any); ok {
		payload = p
	}
	isComplete := false
	if ic, ok := msg["is_complete"].(bool); ok {
		isComplete = ic
	}

	// 使用 EncodeAgentChunkForWire 编码
	chunk := schema.NewAgentResponseChunk(requestID, channelID, payload)
	// chunk.IsComplete = isComplete  // 如果 schema 支持的话
	wire := e2a.EncodeAgentChunkForWire(chunk, requestID, 0)
	// 注意：EncodeAgentChunkForWire 返回 map[string]any

	// 合并 metadata
	metadata := make(map[string]any)
	if wireMeta, ok := wire["metadata"].(map[string]any); ok {
		for k, v := range wireMeta {
			metadata[k] = v
		}
	}
	if msgMeta, ok := msg["metadata"].(map[string]any); ok {
		for k, v := range msgMeta {
			if _, isInternal := e2a.E2AWireInternalMetadataKeys[k]; isInternal {
				continue
			}
			metadata[k] = v
		}
	}
	metadata[e2a.E2AWireServerPushKey] = true
	wire["metadata"] = metadata

	if sessionID, ok := msg["session_id"].(string); ok && sessionID != "" {
		wire["session_id"] = sessionID
	}
	return wire
}

// wireRequestIDKey 统一 request_id 为字符串，对齐 Python _wire_request_id_key
func wireRequestIDKey(v any) string {
	if v == nil {
		return ""
	}
	s, ok := v.(string)
	if ok {
		return s
	}
	return ""
}
```

**注意：** chunk 形的具体实现需检查 `e2a.EncodeAgentChunkForWire` 的签名和 `schema.NewAgentResponseChunk` 的参数，确保字段对齐。实现时以编译通过为准。

- [ ] **Step 2: 编写 `wire_test.go`**

测试要点：
- `TestBuildServerPushWire_有ResponseKind` — 验证输出含 `metadata[E2A_WIRE_SERVER_PUSH_KEY]=true`、`response_kind` 字段
- `TestBuildServerPushWire_无ResponseKind` — chunk 形编码，验证 `metadata` 标记
- `TestWireRequestIDKey` — nil→""、string→string

- [ ] **Step 3: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/gateway_push/...
```

- [ ] **Step 4: 测试验证**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/gateway_push/... -v -run TestBuildServerPushWire
```

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/server/gateway_push/wire.go internal/swarm/server/gateway_push/wire_test.go
git commit -m "feat: 新增 BuildServerPushWire，对齐 Python gateway_push/wire.py"
```

---

### Task 3: AgentServer 适配 byte 管道

**Files:**
- Modify: `internal/swarm/server/agent_server.go`
- Modify: `internal/swarm/server/handle_envelope.go`
- Modify: `internal/swarm/server/agent_server_test.go`
- Modify: `internal/swarm/server/handle_envelope_test.go`

- [ ] **Step 1: 修改 `agent_server.go`**

1. 删除 `ServerReadyWaiter` 接口
2. 删除 `serverReady` / `serverReadyMu` / `serverReadyCh` 字段
3. 删除 `ServerReady()` / `WaitServerReady()` 方法
4. 修改 `NewAgentServer`：删除 `serverReadyCh` 初始化
5. 修改 `Start()`：将 `close(s.serverReadyCh)` 改为发送 connection.ack 事件帧到 recvCh：

```go
// 发送 connection.ack 事件帧（对齐 Python AgentWebSocketServer._connection_handler 首帧）
ackFrame := map[string]any{
    "type":    "event",
    "event":   "connection.ack",
    "payload": map[string]any{"status": "ready"},
}
ackData, err := json.Marshal(ackFrame)
if err != nil {
    logger.Error(logComponent).Err(err).Msg("编码 connection.ack 失败")
} else {
    select {
    case s.transport.RecvCh() <- ackData:
        logger.Info(logComponent).Msg("AgentServer 已就绪（connection.ack 已发送）")
    default:
        logger.Warn(logComponent).Msg("RecvCh 已满，connection.ack 发送失败")
    }
}
```

6. 修改 `startConsumeLoop`：`sendCh` 类型从 `<-chan *e2a.E2AEnvelope` 改为 `<-chan []byte`，需要 `json.Unmarshal` 反序列化为 `E2AEnvelope`：

```go
func (s *AgentServer) startConsumeLoop(ctx context.Context) {
    sendCh := s.transport.SendCh()
    for {
        select {
        case <-ctx.Done():
            logger.Info(logComponent).Msg("AgentServer 消费循环退出（上下文取消）")
            return
        case data, ok := <-sendCh:
            if !ok {
                logger.Info(logComponent).Msg("AgentServer 消费循环退出（通道已关闭）")
                return
            }
            envelope := e2a.EnvelopeFromMap(mapFromJSON(data))
            if envelope == nil {
                logger.Warn(logComponent).Msg("消费循环：E2AEnvelope 反序列化失败")
                continue
            }
            logger.Debug(logComponent).
                Str("request_id", envelope.RequestID).
                Str("method", envelope.Method).
                Msg("收到 E2A 请求信封")
            go s.handleEnvelope(ctx, envelope)
        }
    }
}
```

其中 `mapFromJSON` 做 `json.Unmarshal(data, &m)` → `e2a.EnvelopeFromMap(m)`。

- [ ] **Step 2: 修改 `handle_envelope.go` 的写入方法**

所有写入 `s.transport.RecvCh()` 的方法改为 `json.Marshal` 后写入 `[]byte`：

**`writeResponse`：**
```go
func (s *AgentServer) writeResponse(requestID, channelID string, resp *schema.AgentResponse) {
    wire := e2a.EncodeAgentResponseForWire(resp, requestID, 0)
    data, err := json.Marshal(wire)
    if err != nil {
        logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("响应 JSON 编码失败")
        return
    }
    select {
    case s.transport.RecvCh() <- data:
    default:
        logger.Warn(logComponent).Str("request_id", requestID).Msg("RecvCh 已满，丢弃响应")
    }
}
```

**`writeChunk`、`writeErrorResponse`、`sendKeepalive`** 同理改为 `json.Marshal` → `RecvCh() <- data`。

- [ ] **Step 3: 适配 `agent_server_test.go`**

- 删除 `TestAgentServer_Start_ServerReady`（不再有 WaitServerReady）
- 删除 `TestAgentServer_WaitServerReady_Timeout`
- 修改 `TestAgentServer_ConsumeEnvelope`：`transport.Send()` 改为发送 `[]byte`，`transport.Recv()` 改为读 `[]byte`
- 新增 `TestAgentServer_Start_发送ConnectionAck`：验证 `Start()` 后 `recvCh` 收到 connection.ack 事件帧 JSON

- [ ] **Step 4: 适配 `handle_envelope_test.go`**

- `newTestServer` 返回值不变
- `transport.Send()` 调用改为 `json.Marshal(envelope.ToMap())` → `transport.Send(ctx, data)`
- `transport.Recv()` 读到 `[]byte` 后 `json.Unmarshal` → `map[string]any` → `e2a.ResponseFromMap()` → 验证
- 所有 `recvCh` 读取改为：`data := <-recvCh; json.Unmarshal(data, &m); resp := e2a.ResponseFromMap(m)`

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/...
```

- [ ] **Step 6: 测试验证**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/... -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/swarm/server/
git commit -m "refactor: AgentServer 适配 byte 管道，删除 ServerReadyWaiter，connection.ack 走 recvCh"
```

---

### Task 4: 完整实现 AgentClient

**Files:**
- Modify: `internal/swarm/gateway/routing/agent_client.go`
- Modify: `internal/swarm/gateway/routing/agent_client_test.go`
- Modify: `internal/swarm/gateway/routing/doc.go`

- [ ] **Step 1: 实现完整的 `agent_client.go`**

对齐 Python `WebSocketAgentServerClient`，完整实现所有方法和 receiverLoop。核心结构：

```go
type AgentClient struct {
    transport          gateway_push.AgentTransport
    serverReady        bool
    serverReadyMu      sync.RWMutex
    serverReadyCh      chan struct{}
    onServerPush       func(msg map[string]any)
    sendMu             sync.Mutex
    messageQueues      map[string]chan map[string]any
    messageQueuesMu    sync.Mutex
    cancelledRequests  map[string]struct{}
    running            bool
    runningMu          sync.RWMutex
    cancelFunc         context.CancelFunc
}
```

方法清单：
- `NewAgentClient(transport AgentTransport) *AgentClient`
- `Connect(ctx context.Context) error` — 启动 receiverLoop，等 serverReadyCh 或 5s 超时
- `Disconnect()` — 停止 receiverLoop，清理
- `ServerReady() bool`
- `WaitServerReady(ctx context.Context) bool`
- `SendRequest(ctx context.Context, envelope *e2a.E2AEnvelope) (*schema.AgentResponse, error)`
- `SendRequestStream(ctx context.Context, envelope *e2a.E2AEnvelope) (<-chan *schema.AgentResponseChunk, error)`
- `SetServerPushHandler(handler func(msg map[string]any))`

非导出方法：
- `receiverLoop(ctx context.Context)` — 统一消费 `transport.Recv()`，区分事件帧/server_push/正常响应
- `handleEventFrame(msg map[string]any)` — 处理 connection.ack
- `routeToQueue(msg map[string]any)` — 按 request_id 路由到 messageQueues
- `drainAndRemoveQueue(rid string)` — 对齐 Python `_drain_and_remove_queue`
- `delayedCleanupCancelledRequestID(rid string)` — 2s 延迟清理

- [ ] **Step 2: 编写完整测试 `agent_client_test.go`**

测试要点（使用 `NewChannelTransport` 做 round-trip 测试）：
- `TestNewAgentClient` — 基本创建
- `TestAgentClient_ServerReady_初始为false`
- `TestAgentClient_Connect_收到ConnectionAck` — 模拟 AgentServer 写 connection.ack 到 recvCh，验证 Connect 后 ServerReady() == true
- `TestAgentClient_Connect_超时不报错` — 5s 内无 connection.ack，Connect 仅 warn 不报错
- `TestAgentClient_SendRequest_非流式` — 模拟完整 round-trip：Send envelope → AgentServer 从 sendCh 读 → 写响应到 recvCh → AgentClient 收到
- `TestAgentClient_SendRequestStream_流式` — 模拟流式 round-trip
- `TestAgentClient_SetServerPushHandler` — 模拟 server_push 消息通过 recvCh 到达
- `TestAgentClient_Disconnect` — 验证 receiverLoop 停止

**测试辅助：** 创建 `newTestAgentClientWithTransport()` 辅助函数，同时返回 `*AgentClient` 和 `*ChannelTransport`，供测试方写入 recvCh 模拟 AgentServer 行为。

- [ ] **Step 3: 更新 `doc.go`**

- [ ] **Step 4: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/routing/...
```

- [ ] **Step 5: 测试验证**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/routing/... -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/gateway/routing/
git commit -m "feat: 完整实现 AgentClient，对齐 Python WebSocketAgentServerClient"
```

---

### Task 5: MessageHandler 改用 AgentClient

**Files:**
- Modify: `internal/swarm/gateway/message_handler/message_handler.go`
- Modify: `internal/swarm/gateway/message_handler/forward_loop.go`
- Modify: `internal/swarm/gateway/message_handler/cancel.go`
- Modify: `internal/swarm/gateway/message_handler/message_handler_test.go`
- Modify: `internal/swarm/gateway/message_handler/forward_loop_test.go`
- Modify: `internal/swarm/gateway/message_handler/cancel_test.go`

- [ ] **Step 1: 修改 `message_handler.go`**

1. 删除 `transport gateway_push.AgentTransport` 和 `pushTransport gateway_push.GatewayPushTransport` 字段
2. 新增 `agentClient *routing.AgentClient` 字段
3. `NewMessageHandler` 改为 `NewMessageHandler(agentClient *routing.AgentClient, channelMgr *channel_manager.ChannelManager) *MessageHandler`
4. `StartForwarding()` 中删除 `mh.pushTransport.SetServerPushHandler(...)` 注册，改为 `mh.agentClient.SetServerPushHandler(func(msg map[string]any) { mh.HandleServerPush(msg) })`

- [ ] **Step 2: 修改 `forward_loop.go`**

1. `forwardToAgent` 中将 `mh.transport == nil` 检查改为 `mh.agentClient == nil`
2. 删除 `mh.transport.Send` / `mh.transport.Recv` 直接调用
3. `handleChatSend` 改为调 `mh.agentClient.SendRequest` 或 `mh.agentClient.SendRequestStream`
4. `processStream` 和 `processNonStreamRequest` 整体重写，改为从 AgentClient 的返回值读取（不再直接操作 transport）

**`processNonStreamRequest` 新实现：**
```go
func (mh *MessageHandler) processNonStreamRequest(ctx context.Context, msg *schema.Message, envelope *e2a.E2AEnvelope) {
    requestID := envelope.RequestID
    resp, err := mh.agentClient.SendRequest(ctx, envelope)
    if err != nil {
        logger.Error(logComponent).
            Str("event_type", "non_stream_error").
            Err(err).
            Str("request_id", requestID).
            Msg("非流式请求失败")
        return
    }
    outMsg := ResponseToMessage(resp, msg.SessionID, msg.Metadata)
    mh.enqueueOutbound(outMsg)
}
```

**`processStream` 新实现：**
```go
func (mh *MessageHandler) processStream(ctx context.Context, msg *schema.Message, envelope *e2a.E2AEnvelope) {
    requestID := envelope.RequestID
    streamCtx, streamCancel := context.WithCancel(ctx)
    mh.registerStreamTask(requestID, msg.SessionID, msg.Metadata, streamCancel)
    defer mh.unregisterStreamTask(requestID)

    chunkCh, err := mh.agentClient.SendRequestStream(streamCtx, envelope)
    if err != nil {
        logger.Error(logComponent).
            Str("event_type", "stream_error").
            Err(err).
            Str("request_id", requestID).
            Msg("流式请求失败")
        return
    }
    for {
        select {
        case <-streamCtx.Done():
            return
        case chunk, ok := <-chunkCh:
            if !ok { return }
            if chunk == nil { continue }
            if IsTerminalStreamChunk(chunk) { return }
            reqMetadata := mh.getStreamMetadata(requestID)
            outMsg := ChunkToMessage(chunk, mh.getStreamSessionID(requestID), reqMetadata)
            mh.enqueueOutbound(outMsg)
        }
    }
}
```

- [ ] **Step 3: 修改 `cancel.go`**

`sendCancelToAgent` 改为通过 AgentClient 发送：
```go
func (mh *MessageHandler) sendCancelToAgent(ctx context.Context, requestID, sessionID, intent string) {
    msg := schema.NewReqMessage("", sessionID, schema.ReqMethodChatCancel, nil, schema.WithSessionID(sessionID))
    envelope := e2a.MessageToE2AOrFallback(msg)
    envelope.IsStream = false
    // 发送取消请求（不等待响应）
    data, err := json.Marshal(envelope)
    if err != nil {
        logger.Warn(logComponent).Err(err).Str("request_id", requestID).Msg("取消请求编码失败")
        return
    }
    if err := mh.agentClient.SendTransportBytes(ctx, data); err != nil {
        logger.Warn(logComponent).Err(err).Str("request_id", requestID).Msg("发送取消请求到 AgentServer 失败")
    }
}
```

**注意：** `sendCancelToAgent` 发送 cancel 请求但不等响应，需要一个 `SendTransportBytes` 方法或直接用 `SendRequest` 并忽略响应。实现时选择最简方式。

- [ ] **Step 4: 适配所有测试文件**

- `message_handler_test.go`：`NewMessageHandler(transport, transport, cm)` → `NewMessageHandler(agentClient, cm)`
- `forward_loop_test.go`：适配新的方法签名
- `cancel_test.go`：适配

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/message_handler/...
```

- [ ] **Step 6: 测试验证**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/message_handler/... -v
```

- [ ] **Step 7: Commit**

```bash
git add internal/swarm/gateway/message_handler/
git commit -m "refactor: MessageHandler 改用 AgentClient 通信，删除直接 transport 操作"
```

---

### Task 6: Gateway + WebChannel 消除 server 引用

**Files:**
- Modify: `internal/swarm/gateway/app_gateway.go`
- Modify: `internal/swarm/gateway/app_gateway_test.go`
- Modify: `internal/swarm/gateway/channel_manager/web/web_connect.go`

- [ ] **Step 1: 修改 `app_gateway.go`**

1. 删除 `import server` 包
2. 删除 `agentServer *server.AgentServer` 字段
3. 新增 `agentClient *routing.AgentClient` 字段
4. `NewGatewayServer` 签名改为 `NewGatewayServer(cfg *config.Config, agentClient *routing.AgentClient) (*GatewayServer, error)`
5. 删除 `wc.SetServerReadyWaiter(agentServer)`
6. 将 `agentClient` 传给 WebChannel 和 MessageHandler
7. `Start()` 中调 `agentClient.Connect(ctx)` 启动 receiverLoop
8. `Stop()` 中调 `agentClient.Disconnect()`

- [ ] **Step 2: 修改 `web_connect.go`**

1. 删除 `import server` 包
2. 删除 `serverReadyWaiter server.ServerReadyWaiter` 字段
3. 删除 `SetServerReadyWaiter` 方法
4. 新增 `agentClient *routing.AgentClient` 字段（或通过构造函数注入）
5. `HandleWebSocket` 中将 `wc.serverReadyWaiter.WaitServerReady(r.Context())` 改为 `wc.agentClient.WaitServerReady(r.Context())`

- [ ] **Step 3: 适配 `app_gateway_test.go`**

`newTestGatewayServer` 改为创建 `AgentClient` 并传入：
```go
func newTestGatewayServer(t *testing.T) *GatewayServer {
    t.Helper()
    cfg, err := config.New("")
    require.NoError(t, err)
    transport := gateway_push.NewChannelTransport()
    agentClient := routing.NewAgentClient(transport)
    gs, err := NewGatewayServer(cfg, agentClient)
    require.NoError(t, err)
    return gs
}
```

- [ ] **Step 4: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/gateway/...
```

- [ ] **Step 5: 测试验证**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/... -v
```

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/gateway/
git commit -m "refactor: Gateway + WebChannel 消除 server 包引用，改用 AgentClient"
```

---

### Task 7: cmd.go 启动流程重构

**Files:**
- Modify: `cmd/uapclaw/cmd.go`

- [ ] **Step 1: 修改 `runAppCmd`**

重构启动流程：

```go
// 创建 ChannelTransport（进程内传输）
transport := gateway_push.NewChannelTransport()

// 创建 AgentClient（Gateway 侧协议客户端）
agentClient := routing.NewAgentClient(transport)

// 创建 AgentServer（Server 侧）
agentServer := server.NewAgentServer(cfg, transport)

// 创建 GatewayServer，注入 AgentClient
gs, err := gateway.NewGatewayServer(cfg, agentClient)
if err != nil {
    return fmt.Errorf("创建 GatewayServer 失败: %w", err)
}

logger.Info(logger.ComponentGateway).
    Str("version", version.Version).
    Msg("uapclaw app 启动中")

// 先启动 AgentServer（goroutine），它会 Init AgentManager → 发 connection.ack 到 recvCh → 进入消费循环
go func() {
    if err := agentServer.Start(ctx); err != nil {
        logger.Error(logger.ComponentAgentServer).
            Err(err).
            Msg("AgentServer 启动失败")
    }
}()

// 启动 GatewayServer（HTTP + WebSocket）
// 内部会调 agentClient.Connect() 启动 receiverLoop + 等 connection.ack
if err := gs.Start(ctx); err != nil {
    return fmt.Errorf("启动 GatewayServer 失败: %w", err)
}

// 等待退出信号
<-ctx.Done()
logger.Info(logger.ComponentGateway).Msg("收到退出信号，正在关闭...")

// 停止顺序：AgentServer → GatewayServer
_ = agentServer.Stop()
return gs.Stop()
```

删除 `pushTransport := transport` 行，删除 `server` 包的 gateway 相关 import（如果 `server` 包仍被 `NewAgentServer` 使用则保留）。

- [ ] **Step 2: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./cmd/uapclaw/...
```

- [ ] **Step 3: Commit**

```bash
git add cmd/uapclaw/cmd.go
git commit -m "refactor: cmd.go 启动流程重构，创建 AgentClient 传给 GatewayServer"
```

---

### Task 8: 全量编译 + 测试 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 全量编译**

```bash
cd /home/opensource/uapclaw-gateway && go build ./...
```

- [ ] **Step 2: 全量测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./... -count=1
```

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将本次重构涉及的步骤状态从 `☐` 改为 `✅`。

- [ ] **Step 4: Final Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md，AgentClient 重构完成"
```
