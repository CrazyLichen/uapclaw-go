# uapclaw app 全栈单进程设计文档

> 目标：实现 `uapclaw app` 命令，单进程内启动 Gateway + AgentServer(stub) + WebChannel + 静态文件服务，
> 前端通过 `ws://127.0.0.1:19000/ws` 连接 Go 后端，界面可渲染、核心 RPC 有真实响应。

---

## 1. 架构总览

### 1.1 单进程架构

```
uapclaw app（单进程）
│
├─ HTTP Server (chi, 端口 19000)
│   ├─ /              → 静态文件 (frontend/dist/)
│   ├─ /ws            → WebSocket → WebChannel
│   └─ /file-api/*    → 文件操作 HTTP API
│
├─ Gateway
│   ├─ ChannelManager    → 注册/管理 Channel
│   ├─ WebChannel        → WebSocket RPC 服务端
│   └─ MessageHandler    → 入站→Transport→AgentServer，出站→Channel
│
├─ AgentServer (stub)
│   └─ ChannelTransport  → Go channel 本地传输
│
└─ Transport 抽象
    ├─ ChannelTransport  (本次实现)  → 进程内 Go channel
    └─ WebSocketTransport (后续实现) → 跨进程 WebSocket + E2A
```

### 1.2 与 Python 架构对比

| 维度 | Python `all` 模式 | Go `uapclaw app` |
|------|-------------------|-------------------|
| 进程数 | 3（AgentServer + Gateway + Web静态服务） | 1 |
| Gateway ↔ AgentServer | WebSocket 跨进程通信 | Go channel 进程内通信 |
| 前端 ↔ 后端 | Web静态服务反向代理 `/ws` → Gateway | 同一 HTTP 服务器的 `/ws` 路由 |
| 端口 | 19000(Gateway) + 19001(GatewayServer) + 8080(前端) | 19000（全部） |

### 1.3 Transport 抽象

```go
// AgentTransport Gateway → AgentServer 的传输抽象
type AgentTransport interface {
    Send(ctx context.Context, envelope *e2a.E2AEnvelope) error
    Recv() (<-chan *e2a.E2AResponse, error)
    Close() error
}
```

- **ChannelTransport**：进程内 Go channel，`uapclaw app/chat/serve/acp` 使用
- **WebSocketTransport**：跨进程 WebSocket + E2A，`uapclaw gateway` 使用（后续实现）

---

## 2. 目录结构

对齐 Python `jiuwenswarm/gateway/` 目录：

```
Python                                      Go
─────────────────────────────────────────── ──────────────────────────────────────────
jiuwenswarm/gateway/                        internal/swarm/gateway/
├── app_gateway.py                          ├── app_gateway.go
├── channel_manager/                        ├── channel_manager/
│   ├── base.py                             │   ├── base.go
│   ├── channel_manager.py                  │   ├── channel_manager.go
│   └── web/                                │   └── web/
│       ├── web_connect.py                  │       ├── web_connect.go
│       └── app_web_handlers.py             │       ├── web_handlers.go
│                                               │       └── frame.go
├── message_handler/                        ├── message_handler/
│   └── message_handler.py                  │   └── message_handler.go
├── routing/                                ├── routing/
│   └── agent_client.py                     │   └── agent_client.go (骨架)
└── (file_api 无 Python 对应)               └── file_api.go

jiuwenswarm/server/gateway_push/            internal/swarm/server/gateway_push/
├── transport.py                            ├── transport.go
└── wire.py                                 └── channel_transport.go
```

### 2.1 完整新建文件清单

```
internal/swarm/gateway/
├── doc.go
├── app_gateway.go              # Gateway 启动入口，chi router 组装
├── file_api.go                 # /file-api/* HTTP 路由处理
├── channel_manager/
│   ├── doc.go
│   ├── base.go                 # BaseChannel 接口 + ChannelType + ChannelMetadata
│   ├── channel_manager.go      # ChannelManager 注册/分发
│   └── web/
│       ├── doc.go
│       ├── web_connect.go      # WebChannel 核心（WS 服务端 + 连接管理）
│       ├── web_handlers.go     # RPC handlers（7 核心 + ~70 stub）
│       ├── frame.go            # 帧协议类型定义
│       └── frontend/           # React 前端（从 channel/web/frontend/ 迁移）
│           └── dist/           # 构建产物（go:embed 嵌入）
├── message_handler/
│   ├── doc.go
│   └── message_handler.go      # MessageHandler 入站/出站
└── routing/
    ├── doc.go
    └── agent_client.go         # 骨架（后续实现 WS 客户端）

internal/swarm/server/gateway_push/
├── doc.go
├── transport.go                # AgentTransport 接口
└── channel_transport.go        # ChannelTransport 实现

cmd/uapclaw/cmd.go              # 修改 newAppCmd()
```

### 2.2 目录迁移

当前前端位于 `internal/swarm/channel/web/frontend/`，需迁移到 `internal/swarm/gateway/channel_manager/web/frontend/` 以对齐 Python 目录结构。

迁移步骤：
1. 创建 `internal/swarm/gateway/` 目录及子目录
2. 移动 `internal/swarm/channel/web/frontend/` → `internal/swarm/gateway/channel_manager/web/frontend/`
3. 删除旧的 `internal/swarm/channel/` 目录（迁移完成后）

---

## 3. 帧协议设计

对齐前端 `websocket.ts` 类型定义和 Python `web_connect.py` 协议。

### 3.1 帧类型

```go
// frame.go

// WsRequest 客户端→服务端请求帧
type WsRequest struct {
    Type   string         `json:"type"`            // 固定 "req"
    ID     string         `json:"id"`              // 客户端生成的请求 ID
    Method string         `json:"method"`          // RPC 方法名
    Params map[string]any `json:"params,omitempty"` // 方法参数
}

// WsResponse 服务端→客户端响应帧
type WsResponse struct {
    Type    string         `json:"type"`             // 固定 "res"
    ID      string         `json:"id"`               // 匹配请求 ID
    OK      bool           `json:"ok"`               // 是否成功
    Payload map[string]any `json:"payload,omitempty"` // 响应数据
    Error   string         `json:"error,omitempty"`   // 错误描述
    Code    string         `json:"code,omitempty"`    // 错误码
}

// WsEvent 服务端→客户端推送事件帧
type WsEvent struct {
    Type     string         `json:"type"`              // 固定 "event"
    Event    string         `json:"event"`             // 事件名
    Payload  map[string]any `json:"payload"`           // 事件数据
    Seq      int            `json:"seq,omitempty"`     // 流式序号
    StreamID string         `json:"stream_id,omitempty"` // 流分组 ID
}
```

### 3.2 错误码

对齐 Python `web_connect.py` 中的错误码：

```go
// WsErrorCode 错误码常量
const (
    WsErrBadRequest       = "BAD_REQUEST"
    WsErrMethodNotFound   = "METHOD_NOT_FOUND"
    WsErrInternalError    = "INTERNAL_ERROR"
    WsErrLLMError         = "LLM_ERROR"
    WsErrServiceUnavailable = "SERVICE_UNAVAILABLE"
    WsErrNotFound         = "NOT_FOUND"
    WsErrAlreadyExists    = "ALREADY_EXISTS"
    WsErrConflict         = "CONFLICT"
    WsErrAgentUnavailable = "AGENT_UNAVAILABLE"
)
```

### 3.3 帧编解码辅助函数

```go
func ParseRequest(data []byte) (*WsRequest, error)
func BuildResponse(reqID string, ok bool, payload map[string]any) []byte
func BuildErrorResponse(reqID, errMsg, code string) []byte
func BuildEvent(event string, payload map[string]any) []byte
func BuildStreamEvent(event string, payload map[string]any, seq int, streamID string) []byte
```

---

## 4. WebChannel 设计

对齐 Python `web_connect.py`。

### 4.1 核心结构

```go
// web_connect.go

// WebChannelConfig Web 通道配置
type WebChannelConfig struct {
    Enabled   bool     // 是否启用
    Host      string   // 监听地址，默认 "127.0.0.1"
    Port      int      // 监听端口，默认 19000
    Path      string   // WS 路径，默认 "/ws"
    AllowFrom []string // Origin 白名单
}

// WebChannel Web 通道，实现 BaseChannel 接口
type WebChannel struct {
    config     WebChannelConfig
    name       string                    // 固定 "web"
    clients    map[*websocket.Conn]bool  // 活跃连接
    clientsMu  sync.RWMutex              // 连接集并发保护
    dispatcher *RPCDispatcher            // RPC 分发器
    upgrader   websocket.Upgrader        // gorilla WS 升级器
    running    bool
    runningMu  sync.RWMutex
}
```

### 4.2 连接生命周期

```
1. 客户端连接 /ws
   → Origin 检查（wsorigin.GorillaCheckOrigin）
   → 升级为 WebSocket
   → 加入 clients 集合
   → 发送 connection.ack 事件
   → 进入消息读取循环

2. 接收 req 帧
   → 解析 WsRequest
   → 验证 type=="req", id 非空, method 非空
   → 自动生成 session_id（如缺失）: sess_{hex_timestamp}_{6_random_hex}
   → dispatcher.Dispatch(method, params) → handler
   → handler 处理 → 返回 WsResponse

3. 连接断开
   → 从 clients 集合移除
   → 关闭连接
```

### 4.3 connection.ack 事件

```json
{
  "type": "event",
  "event": "connection.ack",
  "payload": {
    "session_id": "sess_a1b2c3_d4e5f6",
    "mode": "code.normal",
    "tools": [],
    "protocol_version": "1.0"
  }
}
```

### 4.4 WebSocket 配置

对齐 Python `web_connect.py` 中的 `websockets.serve()` 参数：

| 参数 | Python 值 | Go 值 |
|------|----------|-------|
| max_size | 8MB | `Upgrader.ReadLimit = 8 * 1024 * 1024` |
| ping_interval | 20s | `gorilla/websocket` 默认自动 ping/pong |
| ping_timeout | 20s | 同上 |

---

## 5. RPC 分发器设计

### 5.1 分发器接口

```go
// rpc_dispatcher.go

// RPCHandlerFunc RPC 方法处理函数签名
// ctx: 请求上下文
// params: 客户端请求参数
// sessionID: 会话 ID（WebChannel 自动生成或客户端提供）
type RPCHandlerFunc func(ctx context.Context, params map[string]any, sessionID string) (map[string]any, error)

// RPCDispatcher RPC 方法注册与分发
type RPCDispatcher struct {
    handlers map[string]RPCHandlerFunc
    mu       sync.RWMutex
}

// Register 注册 RPC 方法
func (d *RPCDispatcher) Register(method string, handler RPCHandlerFunc)

// Dispatch 分发 RPC 方法，未注册返回 METHOD_NOT_FOUND 错误
func (d *RPCDispatcher) Dispatch(method string, params map[string]any, sessionID string) (map[string]any, error)
```

### 5.2 核心 RPC 实现（7 个）

| 方法 | 实现逻辑 | 数据来源 |
|------|---------|---------|
| `config.get` | 读取 config.yaml 所有键 + 环境变量，解密敏感值 | `config.Get()` + `os.Getenv()` |
| `config.set` | 写入 config.yaml / .env，可选通知 AgentServer 重载 | `config.Set()` + `.env` 写入 |
| `models.list` | 从 config 读取 `models.defaults` + `models.active_model` | `config.Get("models")` |
| `session.list` | 遍历会话目录，读取各 session 的 metadata.json | 文件系统 `~/.uapclaw/agent/sessions/` |
| `session.create` | 创建会话目录 + metadata.json | 文件系统 |
| `session.delete` | 删除会话目录（非团队会话） | 文件系统 `os.RemoveAll()` |
| `channel.get` | 从 ChannelManager 获取启用的渠道列表 | `ChannelManager.GetEnabledChannels()` |

### 5.3 config.get 返回字段

对齐 Python `_config_get` 返回的字段集合（前端 `App.tsx` 启动时依赖这些字段）：

```json
{
  "model_provider": "OpenAI",
  "model": "gpt-4o",
  "api_base": "https://api.openai.com/v1",
  "api_key": "sk-***",
  "mode": "code.normal",
  "app_version": "0.1.0",
  "context_engine_enabled": false,
  "channels": { "web": { "enabled": true } }
}
```

### 5.4 Stub RPC 实现策略

未实现的 RPC 方法按 Python 返回格式返回占位数据：

| 方法类别 | stub 返回 |
|---------|----------|
| `chat.send/resume/interrupt/user_answer` | `{accepted: true, session_id: "..."}` |
| `history.get` | `{accepted: true, session_id: "..."}` |
| `heartbeat.get_conf` | `{every: 0, target: "", active_hours: {}}` |
| `heartbeat.get_path` | `{path: ""}` |
| `updater.*` | `{status: "up_to_date"}` 或空对象 |
| `cron.job.*` | `{jobs: []}` |
| `harness.*` | `{packages: []}` |
| `skills.*` | `{skills: []}` |
| `extensions.*` | `{extensions: []}` |
| `permissions.*` | `{}` |
| `locale.*` | `{preferred_language: "zh"}` |
| `team.*` | `{}` |

---

## 6. Gateway 组装设计

### 6.1 app_gateway.go

```go
// GatewayServer Gateway 服务器
type GatewayServer struct {
    config        *config.Config
    router        *chi.Mux
    webChannel    *WebChannel
    channelMgr    *ChannelManager
    msgHandler    *MessageHandler
    transport     AgentTransport
    httpServer    *http.Server
}

// NewGatewayServer 创建 Gateway 服务器
func NewGatewayServer(cfg *config.Config, transport AgentTransport) (*GatewayServer, error)

// Start 启动 Gateway（阻塞，直到 ctx 取消）
func (s *GatewayServer) Start(ctx context.Context) error

// Stop 优雅关闭
func (s *GatewayServer) Stop() error
```

### 6.2 chi 路由挂载

```go
func (s *GatewayServer) setupRouter() {
    r := chi.NewRouter()

    // 中间件
    r.Use(requestIDMiddleware)
    r.Use(recoverMiddleware)
    r.Use(zeroLogMiddleware)    // zerolog 请求日志

    // WebSocket 路由
    r.Get("/ws", s.webChannel.HandleWebSocket)

    // 文件 API 路由
    r.Route("/file-api", func(r chi.Router) {
        r.Get("/file-content", handleFileContentGet)
        r.Post("/file-content", handleFileContentPost)
        r.Get("/list-files", handleListFiles)
        r.Get("/list-markdown", handleListMarkdown)
        r.Get("/ws-debug-config", handleWsDebugConfigGet)
        r.Post("/ws-debug-config", handleWsDebugConfigPost)
        r.Post("/rebuild-agent-data", handleRebuildAgentData)
    })

    // 静态文件（SPA fallback）
    r.HandleFunc("/*", spaHandler(frontendDistPath))

    s.router = r
}
```

### 6.3 SPA 静态文件处理

```go
// spaHandler 返回 SPA 静态文件处理器
// 对齐 Python app_web.py 的 ThreadingHTTPServer 静态文件服务
// 特殊处理：非文件路径返回 index.html（SPA 路由 fallback）
func spaHandler(distDir string) http.HandlerFunc {
    fileServer := http.FileServer(http.Dir(distDir))
    return func(w http.ResponseWriter, r *http.Request) {
        path := filepath.Join(distDir, r.URL.Path)
        if _, err := os.Stat(path); os.IsNotExist(err) {
            // SPA fallback：返回 index.html
            http.ServeFile(w, r, filepath.Join(distDir, "index.html"))
            return
        }
        fileServer.ServeHTTP(w, r)
    }
}
```

### 6.4 前端资源路径

```
internal/swarm/gateway/channel_manager/web/frontend/dist/
```

使用 `go:embed` 将 `frontend/dist/` 嵌入二进制，部署时无需额外文件。
通过 `//go:embed frontend/dist` 指令将静态资源编译进可执行文件，
使用 `http.FS(embeddedFS)` 提供给 chi 路由。
优势：单二进制部署，无外部文件依赖。

---

## 7. Transport 抽象设计

### 7.1 AgentTransport 接口

```go
// server/gateway_push/transport.go

// AgentTransport Gateway → AgentServer 的传输抽象
//
// 单进程模式（uapclaw app/chat/serve/acp）：
//   Gateway → ChannelTransport → AgentServer（Go channel，零网络开销）
//
// 跨进程模式（uapclaw gateway 独立部署）：
//   Gateway → WebSocketTransport → AgentServer（WebSocket + E2A 协议）
type AgentTransport interface {
    // Send 发送 E2A 请求信封到 AgentServer
    Send(ctx context.Context, envelope *e2a.E2AEnvelope) error
    // Recv 接收 AgentServer 的响应流
    Recv() (<-chan *e2a.E2AResponse, error)
    // Close 关闭传输连接
    Close() error
}
```

### 7.2 ChannelTransport 实现

```go
// server/gateway_push/channel_transport.go

// ChannelTransport 进程内 Go channel 传输
type ChannelTransport struct {
    reqCh  chan *e2a.E2AEnvelope
    respCh chan *e2a.E2AResponse
    closed atomic.Bool
}

func NewChannelTransport() *ChannelTransport {
    return &ChannelTransport{
        reqCh:  make(chan *e2a.E2AEnvelope, 256),
        respCh: make(chan *e2a.E2AResponse, 256),
    }
}

func (t *ChannelTransport) Send(ctx context.Context, envelope *e2a.E2AEnvelope) error {
    select {
    case t.reqCh <- envelope:
        return nil
    case <-ctx.Done():
        return ctx.Err()
    }
}

func (t *ChannelTransport) Recv() (<-chan *e2a.E2AResponse, error) {
    return t.respCh, nil
}

func (t *ChannelTransport) Close() error {
    t.closed.Store(true)
    close(t.reqCh)
    close(t.respCh)
    return nil
}

// RequestCh 暴露请求通道，供 AgentServer 端消费
func (t *ChannelTransport) RequestCh() <-chan *e2a.E2AEnvelope {
    return t.reqCh
}

// SendResponse 供 AgentServer 端发送响应
func (t *ChannelTransport) SendResponse(resp *e2a.E2AResponse) {
    t.respCh <- resp
}
```

### 7.3 启动时 Transport 选择

```go
// cmd/uapclaw/cmd.go

func newAppCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "app",
        Short: "启动完整模式（AgentServer + Gateway）",
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            cfg := loadConfig()

            // 单进程模式：使用 ChannelTransport
            transport := gateway_push.NewChannelTransport()

            gw, err := gateway.NewGatewayServer(cfg, transport)
            if err != nil { return err }

            return gw.Start(ctx)
        },
    }
    return cmd
}
```

---

## 8. BaseChannel 接口设计

对齐 Python `channel_manager/base.py`。

```go
// gateway/channel_manager/base.go

// ChannelType 渠道类型枚举
type ChannelType int

const (
    ChannelTypeACP      ChannelType = iota
    ChannelTypeWeb
    ChannelTypeFeishu
    ChannelTypeXiaoyi
    ChannelTypeDingTalk
    ChannelTypeTelegram
    ChannelTypeDiscord
    ChannelTypeWhatsApp
    ChannelTypeWeCom
    ChannelTypeWeChat
    ChannelTypeCLI
)

// ChannelMetadata 渠道元数据
type ChannelMetadata struct {
    ChannelID string         // 渠道标识，如 "web"
    Source     string         // 来源，如 "websocket"
    UserID    string         // 用户 ID（可选）
    Extra     map[string]any // 额外信息
}

// BaseChannel 渠道基础接口
type BaseChannel interface {
    // Name 渠道名称
    Name() string
    // Start 启动渠道
    Start(ctx context.Context) error
    // Stop 停止渠道
    Stop() error
    // Send 发送消息到渠道（出站，Agent→用户）
    Send(ctx context.Context, msg *schema.Message) error
    // IsRunning 是否运行中
    IsRunning() bool
}
```

---

## 9. ChannelManager 设计

对齐 Python `channel_manager/channel_manager.py`。

```go
// gateway/channel_manager/channel_manager.go

// ChannelManager 渠道管理器
type ChannelManager struct {
    channels map[string]BaseChannel  // channelID → channel
    mu       sync.RWMutex
}

func NewChannelManager() *ChannelManager

// RegisterChannel 注册渠道
func (cm *ChannelManager) RegisterChannel(channel BaseChannel) error

// UnregisterChannel 注销渠道
func (cm *ChannelManager) UnregisterChannel(channelID string) error

// GetEnabledChannels 获取启用的渠道列表（channel.get RPC 使用）
func (cm *ChannelManager) GetEnabledChannels() []string

// DeliverToMessageHandler 投递消息到 MessageHandler（入站，用户→Agent）
func (cm *ChannelManager) DeliverToMessageHandler(msg *schema.Message) error

// BroadcastToChannels 广播消息到所有渠道（出站，Agent→用户）
func (cm *ChannelManager) BroadcastToChannels(ctx context.Context, msg *schema.Message) error
```

---

## 10. MessageHandler 设计

对齐 Python `message_handler/message_handler.py`，本次为最简骨架。

```go
// gateway/message_handler/message_handler.go

// MessageHandler 消息处理器
// 入站：Channel → MessageHandler → Transport → AgentServer
// 出站：AgentServer → Transport → MessageHandler → Channel
type MessageHandler struct {
    transport   AgentTransport
    channelMgr  *ChannelManager
}

func NewMessageHandler(transport AgentTransport, channelMgr *ChannelManager) *MessageHandler

// HandleInbound 处理入站消息（用户→Agent）
func (mh *MessageHandler) HandleInbound(ctx context.Context, msg *schema.Message) error

// StartOutboundLoop 启动出站消息循环（Agent→用户）
func (mh *MessageHandler) StartOutboundLoop(ctx context.Context) error
```

**本次实现**：
- `HandleInbound`：将 Message 转换为 E2AEnvelope，通过 Transport.Send 发送
- `StartOutboundLoop`：从 Transport.Rev 持续读取响应，转换为 Message，通过 ChannelManager.BroadcastToChannels 分发

---

## 11. file_api 设计

前端的文件操作 HTTP 请求，当前由 Vite dev middleware 处理，生产环境需要 Go 后端提供。

### 11.1 路由

| 路由 | 方法 | 处理 |
|------|------|------|
| `/file-api/file-content` | GET | 读取文件内容（query: `path`, `encoding`） |
| `/file-api/file-content` | POST | 写入 Markdown 文件（body: `{path, content}`） |
| `/file-api/list-files` | GET | 列目录内容（query: `dir`） |
| `/file-api/list-markdown` | GET | 列 Markdown 文件（query: `dir`） |
| `/file-api/ws-debug-config` | GET | 获取 WS 调试配置 |
| `/file-api/ws-debug-config` | POST | 设置 WS 调试配置 |
| `/file-api/rebuild-agent-data` | POST | 重建 Agent 数据 |

### 11.2 安全约束

- 文件路径必须在**工作区目录**内，禁止路径穿越（`../`）
- 写操作仅允许 Markdown 文件
- `list-files` / `list-markdown` 限制在工作区子目录

---

## 12. 配置与端口

### 12.1 默认值

对齐 Python 默认配置：

| 配置项 | 默认值 | 环境变量 | CLI 参数 |
|--------|--------|---------|---------|
| WebChannel host | `127.0.0.1` | `WEB_HOST` | `--host` |
| WebChannel port | `19000` | `WEB_PORT` | `--port` |
| WebChannel path | `/ws` | `WEB_PATH` | `--web-path` |

### 12.2 config.yaml 相关段落

```yaml
server:
  gateway:
    host: "127.0.0.1"
    port: 19000
  agentserver:
    host: "127.0.0.1"
    port: 18092

channels:
  web:
    enabled: true
    send_file_allowed: true
```

---

## 13. 依赖项

### 13.1 新增 go.mod 依赖

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/go-chi/chi/v5` | latest | HTTP 路由框架 |
| `github.com/gorilla/websocket` | v1.5.3 | WebSocket（提升为直接依赖） |

### 13.2 已有可复用依赖

| 依赖 | 用途 |
|------|------|
| `internal/common/config/` | 配置读写 |
| `internal/common/wsorigin/` | WebSocket Origin 检查 |
| `internal/common/logger/` | zerolog 日志 |
| `internal/common/version/` | 版本信息 |
| `internal/swarm/schema/` | ReqMethod/EventType/Mode/Message 类型 |
| `internal/swarm/e2a/` | E2A 协议编解码 |

---

## 14. IMPLEMENTATION_PLAN.md 章节对应

| 本次实现 | 章节状态变更 |
|---------|-------------|
| `gateway/channel_manager/base.go` | 11.1 ☐→✅ |
| `gateway/channel_manager/channel_manager.go` | 11.2 ☐→✅ |
| `gateway/message_handler/message_handler.go` | 11.3 ☐→🔄（骨架） |
| `gateway/routing/agent_client.go` | 11.5 ☐→🔄（骨架） |
| `gateway/app_gateway.go` | 11.9 ☐→🔄（WebChannel 部分） |
| `gateway/channel_manager/web/*` | 11.14 ☐→✅ |
| `server/gateway_push/transport.go` | 10.3.21 ☐→✅（接口 + ChannelTransport） |
| `server/gateway_push/channel_transport.go` | 10.3.21 ☐→✅ |
| `cmd/uapclaw/cmd.go` (app 命令) | 12.7 ☐→🔄 |

---

## 15. 测试策略

### 15.1 单元测试

| 包 | 测试重点 |
|---|---------|
| `gateway/channel_manager/web/frame` | 帧编解码、边界值、畸形 JSON |
| `gateway/channel_manager/web` (rpc_dispatcher) | 方法注册/分发/未找到 |
| `gateway/channel_manager/web` (rpc_handlers) | config.get/set、session CRUD |
| `gateway/channel_manager` | ChannelManager 注册/注销 |
| `server/gateway_push` | ChannelTransport Send/Recv/Close |

### 15.2 集成测试（build tag: integration）

| 测试 | 说明 |
|------|------|
| WebSocket 连接生命周期 | 真实 WS 连接 + connection.ack |
| RPC 请求-响应 | 真实 WS 发 req → 收 res |
| 静态文件服务 | HTTP GET /index.html |
| /file-api/* | 真实文件读写 |

### 15.3 端到端测试（build tag: e2e）

| 测试 | 说明 |
|------|------|
| 前端→后端完整流程 | 浏览器 WS 连接 → config.get → session.list → 界面渲染 |

---

## 16. 不在本次范围

| 章节 | 原因 |
|------|------|
| 10.3.1 AgentWebSocketServer | AgentServer 完整实现后续 |
| 10.3.2 JiuWenClaw | 后续 |
| 11.10 Cron 调度 | stub 返回空 |
| 11.11 心跳服务 | stub 返回空 |
| 11.12 IM Pipeline | 不涉及 |
| 11.13 Gateway Hook | 不涉及 |
| 11.15-11.26 其他 IM 渠道 | 后续 |
| 12.8/12.9 AgentServer/Gateway 独立启动 | 后续 |
| WebSocketTransport 实现 | 后续（11.5 完善时） |
