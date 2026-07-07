# uapclaw app Web Channel 通信打通 — 实现设计

> 目标：打通前端界面与 Go 后端之间的 WebSocket 通信，前端能连上 `ws://127.0.0.1:19000/ws`，
> 收发帧，界面可加载。所有 RPC 方法对齐 Python 全量注册，业务逻辑先 stub。

---

## 1. 实现范围

### 1.1 本次目标

- 用户访问 `http://127.0.0.1:19000` → Go 后端返回前端静态文件
- 前端 JS 自动连接 `ws://127.0.0.1:19000/ws` → 收到 `connection.ack` → 界面初始化
- 前端调用所有 RPC 方法 → 后端返回 stub/真实响应，不报错
- 开发模式：前端走 Vite dev server（5173），Vite 代理 `/ws` → Go 后端（19000）

### 1.2 不在本次范围

- AgentServer 完整实现（LLM 调用、流式推理）
- MessageHandler 转发链路（on_message → Transport → AgentServer）
- IM 渠道（feishu/telegram 等）
- Cron 调度、心跳服务、Gateway Hook
- WebSocketTransport 实现

---

## 2. 对应实现计划章节

| 本次实现 | IMPLEMENTATION_PLAN 章节 | 状态变更 |
|---------|--------------------------|---------|
| `gateway/channel_manager/base.go` | 11.1 | ☐→✅ |
| `gateway/channel_manager/channel_manager.go` | 11.2 | ☐→✅ |
| `gateway/message_handler/message_handler.go` | 11.3 | ☐→🔄（骨架） |
| `gateway/routing/agent_client.go` | 11.5 | ☐→🔄（骨架） |
| `gateway/app_gateway.go` | 11.9 | ☐→🔄（WebChannel 部分） |
| `gateway/channel_manager/web/*` | 11.14 | ☐→✅ |
| `server/gateway_push/transport.go` | 10.3.21 | ☐→✅（接口） |
| `server/gateway_push/channel_transport.go` | 10.3.21 | ☐→✅ |
| `cmd/uapclaw/cmd.go` (app 命令) | 12.7 | ☐→🔄 |

---

## 3. 新建文件清单

```
internal/swarm/gateway/
├── doc.go
├── app_gateway.go              # GatewayServer（chi router + /ws + /file-api + 静态文件）
├── file_api.go                 # /file-api/* HTTP 路由处理
├── channel_manager/
│   ├── doc.go
│   ├── base.go                 # BaseChannel 接口 + ChannelType + ChannelMetadata
│   ├── channel_manager.go      # ChannelManager 注册/分发
│   └── web/
│       ├── doc.go
│       ├── web_connect.go      # WebChannel（WS 服务端 + 连接管理 + connection.ack）
│       ├── web_handlers.go     # RPC 分发器 + 全量 handlers（对齐 Python）
│       ├── frame.go            # 帧协议类型 + 编解码辅助函数
│       └── frontend/           # React 前端（从 channel/web/frontend/ 迁移）
│           └── dist/           # 构建产物（go:embed 嵌入）
├── message_handler/
│   ├── doc.go
│   └── message_handler.go      # MessageHandler 骨架
└── routing/
    ├── doc.go
    └── agent_client.go         # AgentClient 骨架

internal/swarm/server/gateway_push/
├── doc.go
├── transport.go                # AgentTransport 接口
└── channel_transport.go        # ChannelTransport 实现
```

### 修改的文件

| 文件 | 修改内容 |
|------|---------|
| `cmd/uapclaw/cmd.go` | 实现 `app` 子命令（启动 Gateway） |

### 迁移的目录

| 从 | 到 |
|---|---|
| `internal/swarm/channel/web/frontend/` | `internal/swarm/gateway/channel_manager/web/frontend/` |

---

## 4. 关键决策

| 决策点 | 选择 | 原因 |
|--------|------|------|
| `connection.ack` 的 mode | `"BUILD"` | 对齐 Python `get_build_mode()` |
| `config.get` 字段范围 | 全量 ~65 个字段 | 对齐 Python `_CONFIG_SET_ENV_MAP`，前端所有面板正确显示 |
| `config.set` 实现 | 全量写入逻辑 + AgentServer reload 通知 stub | 写入对齐 Python，通知部分本次 stub |
| 前端静态文件 | go:embed 嵌入 dist/ + Vite dev 双模式 | 生产部署单二进制，开发走 Vite 代理 |
| RPC 方法注册 | 全量对齐 Python | 所有方法都注册，未实现的返回 stub 占位数据 |
| chat 类方法响应 | 本地 ack + 延时推送模拟 event | 前端体验完整，模拟事件用注释标注 |

---

## 5. 帧协议

对齐前端 `websocket.ts` 和 Python `web_connect.py`。

### 5.1 帧类型

```go
// WsRequest 客户端→服务端请求帧
type WsRequest struct {
    Type   string         `json:"type"`              // 固定 "req"
    ID     string         `json:"id"`                // 客户端请求 ID
    Method string         `json:"method"`            // RPC 方法名
    Params map[string]any `json:"params,omitempty"`  // 方法参数
}

// WsResponse 服务端→客户端响应帧
type WsResponse struct {
    Type    string         `json:"type"`               // 固定 "res"
    ID      string         `json:"id"`                 // 匹配请求 ID
    OK      bool           `json:"ok"`                 // 是否成功
    Payload map[string]any `json:"payload,omitempty"`  // 响应数据
    Error   string         `json:"error,omitempty"`    // 错误描述
    Code    string         `json:"code,omitempty"`     // 错误码
}

// WsEvent 服务端→客户端推送事件帧
type WsEvent struct {
    Type     string         `json:"type"`                 // 固定 "event"
    Event    string         `json:"event"`                // 事件名
    Payload  map[string]any `json:"payload"`              // 事件数据
    Seq      int            `json:"seq,omitempty"`        // 流式序号
    StreamID string         `json:"stream_id,omitempty"`  // 流分组 ID
}
```

### 5.2 错误码

对齐 Python `web_connect.py`：

```go
const (
    WsErrBadRequest         = "BAD_REQUEST"
    WsErrMethodNotFound     = "METHOD_NOT_FOUND"
    WsErrInternalError      = "INTERNAL_ERROR"
    WsErrLLMError           = "LLM_ERROR"
    WsErrServiceUnavailable = "SERVICE_UNAVAILABLE"
    WsErrNotFound           = "NOT_FOUND"
    WsErrAlreadyExists      = "ALREADY_EXISTS"
    WsErrConflict           = "CONFLICT"
    WsErrAgentUnavailable   = "AGENT_UNAVAILABLE"
)
```

### 5.3 帧编解码辅助

```go
func ParseRequest(data []byte) (*WsRequest, error)
func BuildResponse(reqID string, ok bool, payload map[string]any) []byte
func BuildErrorResponse(reqID, errMsg, code string) []byte
func BuildEvent(event string, payload map[string]any) []byte
func BuildStreamEvent(event string, payload map[string]any, seq int, streamID string) []byte
```

---

## 6. WebChannel 设计

### 6.1 核心结构

```go
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

### 6.2 连接生命周期

```
1. 客户端连接 /ws
   → Origin 检查（wsorigin.GorillaCheckOrigin）
   → 升级为 WebSocket
   → 加入 clients 集合
   → 发送 connection.ack 事件（mode: "BUILD"）
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

### 6.3 connection.ack 事件

```json
{
  "type": "event",
  "event": "connection.ack",
  "payload": {
    "session_id": "sess_a1b2c3_d4e5f6",
    "mode": "BUILD",
    "tools": [],
    "protocol_version": "1.0"
  }
}
```

### 6.4 Session ID 生成

对齐 Python `_make_session_id()` 和前端 `generateSessionId`：

```go
func makeSessionID() string {
    ts := strconv.FormatInt(time.Now().UnixMilli(), 16)
    suffix := make([]byte, 3)
    rand.Read(suffix)
    return fmt.Sprintf("sess_%s_%x", ts, suffix)
}
```

---

## 7. RPC 分发器设计

### 7.1 分发器接口

```go
// RPCHandlerFunc RPC 方法处理函数签名
type RPCHandlerFunc func(ctx context.Context, params map[string]any, sessionID string) (map[string]any, error)

// RPCDispatcher RPC 方法注册与分发
type RPCDispatcher struct {
    handlers map[string]RPCHandlerFunc
    mu       sync.RWMutex
}

func (d *RPCDispatcher) Register(method string, handler RPCHandlerFunc)
func (d *RPCDispatcher) Dispatch(method string, params map[string]any, sessionID string) (map[string]any, error)
```

### 7.2 RPC 方法分类

对齐 Python `app_web_handlers.py` 的方法注册，全量注册：

#### 本地实现方法（Gateway 侧处理）

| 方法 | 实现状态 | 说明 |
|------|---------|------|
| `config.get` | **全量实现** | 读取 `_CONFIG_SET_ENV_MAP` 环境变量 + config.yaml，对齐 Python ~65 字段 |
| `config.set` | **全量实现** | 写入 .env + config.yaml + os.Setenv，AgentServer reload 通知 stub |
| `config.save_all` | stub | 返回 `{updated: []}` |
| `config.validate_model` | stub | 返回 `{ok: true}` |
| `models.list` | **实现** | 从 config 读取 models.defaults |
| `models.replace_all` | stub | 返回 `{ok: true}` |
| `models.validate` | stub | 返回 `{ok: true}` |
| `channel.get` | **实现** | 返回 `{channels: {web: {enabled: true}}}` |
| `session.list` | **实现** | 遍历会话目录读取 metadata.json |
| `session.create` | **实现** | 创建会话目录 + metadata.json |
| `session.delete` | **实现** | 删除会话目录 |
| `session.switch` | stub | 返回 `{ok: true}` |
| `path.get` / `path.set` | stub | 浏览器路径配置 |
| `memory.compute` | stub | 返回 `{rss: 0, total: 0}` |
| `locale.get_conf` | stub | 返回 `{preferred_language: "zh"}` |
| `locale.set_conf` | stub | 返回 `{ok: true}` |
| `heartbeat.get_conf` | stub | 返回 `{every: 0, target: "", active_hours: {}}` |
| `heartbeat.set_conf` | stub | 返回 `{ok: true}` |
| `heartbeat.get_path` | stub | 返回 `{path: ""}` |
| `updater.check` / `updater.download` / `updater.install` / `updater.cancel` / `updater.get_status` | stub | 返回 `{status: "up_to_date"}` |
| `hooks.list` | stub | 返回 `{hooks: []}` |
| `permissions.owner_scopes.get` / `permissions.owner_scopes.set` | stub | 返回 `{}` |
| `permissions.tools.*` / `permissions.rules.*` / `permissions.approval_overrides.*` | stub | 返回 `{}` |
| `memory.forbidden.get` / `memory.forbidden.set` | stub | 返回 `{}` |
| `channel.<platform>.get_conf` / `channel.<platform>.set_conf` | stub | 各 IM 平台配置，返回 `{}` |
| `channel.wechat.get_login_ui` / `channel.wechat.unbind` | stub | 返回 `{}` |
| `cron.job.*`（8 个方法） | stub | 返回 `{jobs: []}` |
| `harness.*`（7 个方法） | stub | 返回 `{packages: []}` |

#### chat 类方法（本地 ack + 延时模拟 event）

| 方法 | 本地 ack 响应 | 模拟 event（延时 500ms，用注释标注 `// [模拟事件]`） |
|------|-------------|------|
| `chat.send` | `{accepted: true, session_id}` | `chat.final`：`{content: "此功能尚未实现"}` |
| `chat.resume` | `{accepted: true, session_id}` | `chat.final`：`{content: "此功能尚未实现"}` |
| `chat.interrupt` | `{accepted: true, session_id, intent: "interrupt"}` | `chat.interrupt_result`：`{success: true}` |
| `chat.user_answer` | `{accepted: true, session_id, request_id}` | `chat.final`：`{content: "此功能尚未实现"}` |

#### 转发方法（全部 stub，不转发 AgentServer）

对齐 Python `_FORWARD_REQ_METHODS`，以下方法全部返回 stub 占位数据：

```
initialize, session.switch, acp.tool_response,
team.delete, team.snapshot, team.history.get,
history.get, browser.start,
skills.marketplace.list, skills.list, skills.installed, skills.get,
skills.toggle, skills.install, skills.import_local, skills.uninstall,
skills.marketplace.add, skills.marketplace.remove, skills.marketplace.toggle,
skills.skillnet.search, skills.skillnet.install, skills.skillnet.install_status,
skills.skillnet.evaluate, skills.clawhub.get_token, skills.clawhub.set_token,
skills.clawhub.search, skills.clawhub.download,
skills.teamskillshub.info, skills.teamskillshub.init, skills.teamskillshub.validate,
skills.teamskillshub.pack, skills.teamskillshub.search, skills.teamskillshub.install,
skills.teamskillshub.publish, skills.teamskillshub.delete,
skills.evolution.status, skills.evolution.get, skills.evolution.save,
plugins.list, plugins.install, plugins.uninstall, plugins.enable, plugins.disable, plugins.reload,
extensions.list, extensions.import, extensions.delete, extensions.toggle,
agents.list, agents.get, agents.create, agents.update, agents.delete,
agents.enable, agents.disable, agents.tools_list,
schedule.check_config, schedule.update_config, schedule.create, schedule.run,
schedule.list, schedule.status, schedule.logs, schedule.cancel, schedule.delete
```

---

## 8. config.get 全量实现

### 8.1 _CONFIG_SET_ENV_MAP（对齐 Python，47 个环境变量映射）

```go
// configEnvMap 前端配置键名 → 环境变量名映射
var configEnvMap = map[string]string{
    // default 模型（主对话）
    "model_provider": "MODEL_PROVIDER",
    "model":          "MODEL_NAME",
    "api_base":       "API_BASE",
    "api_key":        "API_KEY",
    // video 模型
    "video_api_base":  "VIDEO_API_BASE",
    "video_api_key":   "VIDEO_API_KEY",
    "video_model":     "VIDEO_MODEL_NAME",
    "video_provider":  "VIDEO_PROVIDER",
    // audio 模型
    "audio_api_base":  "AUDIO_API_BASE",
    "audio_api_key":   "AUDIO_API_KEY",
    "audio_model":     "AUDIO_MODEL_NAME",
    "audio_provider":  "AUDIO_PROVIDER",
    // vision 模型
    "vision_api_base":  "VISION_API_BASE",
    "vision_api_key":   "VISION_API_KEY",
    "vision_model":     "VISION_MODEL_NAME",
    "vision_provider":  "VISION_PROVIDER",
    // 其他
    "email_address":   "EMAIL_ADDRESS",
    "email_token":     "EMAIL_TOKEN",
    "embed_api_key":   "EMBED_API_KEY",
    "embed_api_base":  "EMBED_API_BASE",
    "embed_model":     "EMBED_MODEL",
    "jina_api_key":    "JINA_API_KEY",
    "bocha_api_key":   "BOCHA_API_KEY",
    "serper_api_key":  "SERPER_API_KEY",
    "perplexity_api_key": "PERPLEXITY_API_KEY",
    "github_token":    "GITHUB_TOKEN",
    "evolution_auto_scan": "EVOLUTION_AUTO_SCAN",
    "skill_create":    "SKILL_CREATE",
    "teamskills_market_url":           "TEAM_SKILLS_HUB_BASE_URL",
    "teamskills_user_token":           "TEAM_SKILLS_HUB_USER_TOKEN",
    "teamskills_system_token":         "TEAM_SKILLS_HUB_SYSTEM_TOKEN",
    "teamskills_allowed_download_hosts": "TEAM_SKILLS_HUB_ALLOWED_DOWNLOAD_HOSTS",
    "free_search_ddg_enabled":  "FREE_SEARCH_DDG_ENABLED",
    "free_search_bing_enabled": "FREE_SEARCH_BING_ENABLED",
    "free_search_proxy_url":    "FREE_SEARCH_PROXY_URL",
    // agents
    "skills":             "SKILLS",
    "max_iterations":     "MAX_ITERATIONS",
    "completion_timeout": "COMPLETION_TIMEOUT",
    // team
    "team_name":    "TEAM_NAME",
    "lifecycle":    "LIFECYCLE",
    "teammate_mode": "TEAMATE_MODE",
    "spawn_mode":   "SPAWN_MODE",
    "member_name":  "MEMBER_NAME",
    "display_name": "DISPLAY_NAME",
    "persona":      "PERSONA",
    "agent_key":    "AGENT_KEY",
    "role_type":    "ROLE_TYPE",
    "prompt_hint":  "PROMPT_HINT",
}
```

### 8.2 config.yaml 补充字段

除环境变量映射外，`config.get` 还需从 config.yaml 读取：

| 前端键 | config.yaml 路径 | 默认值 |
|--------|-----------------|--------|
| `context_engine_enabled` | `react.context_engine_config.enabled` | `"false"` |
| `kv_cache_affinity_enabled` | `react.context_engine_config.enable_kv_cache_release` | `"false"` |
| `permissions_enabled` | `permissions.enabled` | `"false"` |
| `skill_create` | env `SKILL_CREATE` 优先，fallback `react.evolution.skill_create` | `"false"` |
| `evolution_auto_scan` | env `EVOLUTION_AUTO_SCAN` 优先，fallback `react.evolution.auto_scan` | `"false"` |
| `memory_forbidden_enabled` | `memory.forbidden_memory_definition.enabled` | `"false"` |
| `memory_forbidden_description` | `memory.forbidden_memory_definition.description` | `""` |
| `app_version` | 代码版本 | — |
| `free_search_ddg_enabled` | fallback 默认 | `"false"` |
| `free_search_bing_enabled` | fallback 默认 | `"false"` |

此外，还需合并 `_flatten_modes_team_for_config_panel(raw)` 展平的 team 配置字段。

### 8.3 API Key 解密

Python 中 `config.get` 对 `api_key`/`token` 类字段会尝试通过 `ExtensionRegistry` 解密。Go 中暂时跳过解密逻辑（本次无 crypto provider 实现），直接返回环境变量原文。

---

## 9. config.set 实现

### 9.1 写入流程（对齐 Python）

1. 遍历 `_CONFIG_SET_ENV_MAP`，将前端传入的参数映射到环境变量名
2. 对 `_provider` 后缀字段校验 provider 合法性（可选，本次可简化）
3. 写入 `os.Setenv()` 更新进程内环境变量
4. 持久化到 `.env` 文件（`_persistEnvUpdates`）：读取现有 .env → 覆盖/追加 → 写回
5. 处理 `_CONFIG_YAML_KEYS` 字段：写入 config.yaml 对应路径
6. 处理 `agents`/`team` 参数：更新 config.yaml 中 `modes.team`

### 9.2 .env 持久化

对齐 Python `_persist_env_updates`：
- 读取现有 `.env` 文件所有行
- 对每个更新的 key，找到 `KEY=...` 行替换
- 未找到的 key 追加到文件末尾
- 值用双引号包裹：`KEY="value"`
- 空值写为 `KEY=`

### 9.3 AgentServer reload 通知

Python 中 `config.set` 完成后调用 `_notify_config_saved_once` → 发送 `AGENT_RELOAD_CONFIG` 到 AgentServer。

本次实现：**stub**，在日志中记录 `[config.set] AgentServer reload 通知已跳过（stub）`，不实际发送。

---

## 10. Gateway 组装

### 10.1 app_gateway.go

```go
// GatewayServer Gateway 服务器
type GatewayServer struct {
    config     *config.Config
    router     *chi.Mux
    webChannel *WebChannel
    channelMgr *ChannelManager
    httpServer *http.Server
}

func NewGatewayServer(cfg *config.Config) (*GatewayServer, error)
func (s *GatewayServer) Start(ctx context.Context) error
func (s *GatewayServer) Stop() error
```

### 10.2 chi 路由

```go
func (s *GatewayServer) setupRouter() {
    r := chi.NewRouter()

    // 中间件
    r.Use(requestIDMiddleware)
    r.Use(recoverMiddleware)
    r.Use(zeroLogMiddleware)

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
    r.HandleFunc("/*", spaHandler(frontendDistFS))

    s.router = r
}
```

### 10.3 前端资源嵌入

```go
//go:embed frontend/dist
var frontendDist embed.FS
```

使用 `http.FS(embed.FS)` 提供给 chi 路由，SPA fallback 返回 `index.html`。

开发模式下，前端走 Vite dev server（5173），Vite 配置已代理 `/ws` → `http://127.0.0.1:19000`。

---

## 11. BaseChannel 接口

对齐 Python `channel_manager/base.py`：

```go
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
    ChannelID string
    Source     string
    UserID    string
    Extra     map[string]any
}

// BaseChannel 渠道基础接口
type BaseChannel interface {
    Name() string
    Start(ctx context.Context) error
    Stop() error
    Send(ctx context.Context, msg *schema.Message) error
    IsRunning() bool
}
```

---

## 12. ChannelManager

对齐 Python `channel_manager/channel_manager.py`：

```go
// ChannelManager 渠道管理器
type ChannelManager struct {
    channels map[string]BaseChannel
    mu       sync.RWMutex
}

func NewChannelManager() *ChannelManager
func (cm *ChannelManager) RegisterChannel(channel BaseChannel) error
func (cm *ChannelManager) UnregisterChannel(channelID string) error
func (cm *ChannelManager) GetEnabledChannels() []string
```

本次简化：不含 `DeliverToMessageHandler` / `BroadcastToChannels`（无 MessageHandler 转发链路）。

---

## 13. Transport 抽象

### 13.1 AgentTransport 接口

```go
// AgentTransport Gateway → AgentServer 的传输抽象
type AgentTransport interface {
    Send(ctx context.Context, envelope *e2a.E2AEnvelope) error
    Recv() (<-chan *e2a.E2AResponse, error)
    Close() error
}
```

### 13.2 ChannelTransport 实现

```go
// ChannelTransport 进程内 Go channel 传输
type ChannelTransport struct {
    reqCh  chan *e2a.E2AEnvelope
    respCh chan *e2a.E2AResponse
    closed atomic.Bool
}

func NewChannelTransport() *ChannelTransport
```

本次 ChannelTransport 仅作为接口实现存在，`app` 命令中创建但不实际使用（无 AgentServer 交互）。

---

## 14. file_api 设计

对齐 Vite dev server 中的 `devFileContentApi()` 插件逻辑。

### 14.1 路由

| 路由 | 方法 | 处理 |
|------|------|------|
| `/file-api/file-content` | GET | 读取文件内容（query: `path`, `encoding`） |
| `/file-api/file-content` | POST | 写入 Markdown 文件（body: `{path, content}`） |
| `/file-api/list-files` | GET | 列目录内容（query: `dir`） |
| `/file-api/list-markdown` | GET | 列 Markdown 文件（query: `dir`） |
| `/file-api/ws-debug-config` | GET | 获取 WS 调试配置 |
| `/file-api/ws-debug-config` | POST | 设置 WS 调试配置 |
| `/file-api/rebuild-agent-data` | POST | 重建 Agent 数据 |

### 14.2 安全约束

- 文件路径必须在**工作区目录**内，禁止路径穿越（`../`）
- 写操作仅允许 Markdown 文件
- `list-files` / `list-markdown` 限制在工作区子目录
- 工作区路径从 `config.GetWorkspaceDir()` 获取

---

## 15. cmd/uapclaw app 命令

```go
func newAppCmd() *cobra.Command {
    cmd := &cobra.Command{
        Use:   "app",
        Short: "启动完整模式（Gateway + 静态文件服务）",
        RunE: func(cmd *cobra.Command, args []string) error {
            ctx := cmd.Context()
            cfg := loadConfig()

            gw, err := gateway.NewGatewayServer(cfg)
            if err != nil { return err }

            return gw.Start(ctx)
        },
    }
    return cmd
}
```

---

## 16. 依赖项

### 16.1 新增 go.mod 依赖

| 依赖 | 版本 | 用途 |
|------|------|------|
| `github.com/go-chi/chi/v5` | latest | HTTP 路由框架 |
| `github.com/gorilla/websocket` | v1.5.3 | WebSocket（提升为直接依赖） |

### 16.2 已有可复用依赖

| 依赖 | 用途 |
|------|------|
| `internal/common/config/` | 配置读写 |
| `internal/common/wsorigin/` | WebSocket Origin 检查 |
| `internal/common/logger/` | zerolog 日志 |
| `internal/common/version/` | 版本信息 |
| `internal/swarm/schema/` | ReqMethod/EventType/Mode/Message 类型 |
| `internal/swarm/e2a/` | E2A 协议编解码 |

---

## 17. 测试策略

### 17.1 单元测试

| 包 | 测试重点 |
|---|---------|
| `gateway/channel_manager/web/frame` | 帧编解码、边界值、畸形 JSON |
| `gateway/channel_manager/web` (rpc_dispatcher) | 方法注册/分发/未找到 |
| `gateway/channel_manager/web` (rpc_handlers) | config.get/set、session CRUD |
| `gateway/channel_manager` | ChannelManager 注册/注销 |
| `server/gateway_push` | ChannelTransport Send/Recv/Close |

### 17.2 集成测试（build tag: integration）

| 测试 | 说明 |
|------|------|
| WebSocket 连接生命周期 | 真实 WS 连接 + connection.ack |
| RPC 请求-响应 | 真实 WS 发 req → 收 res |
| 静态文件服务 | HTTP GET /index.html |
| /file-api/* | 真实文件读写 |
