# Gateway + AgentServer MVP 设计方案

> 本文档描述 UapClaw Go 项目中领域十（Schema + E2A + AgentServer）和领域十一（Gateway + Web 通道）的 MVP + Web 界面实现设计。
> 方案选型：C + Schema 完整，自底向上，先单进程 ChannelTransport。

---

## 1. 决策记录

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 实现范围 | 方案 C + Schema 完整 | 适配器骨架就绪，他人完成后替换即可；Schema 一次到位免反复 |
| 进程模型 | 先单进程 ChannelTransport，接口预留跨进程 | 最快跑通，E2A 协议保证进程内外行为一致 |
| 依赖解耦 | Runner/DeepAgent 通过 AgentExecutor/AgentRegistry 接口隔离 | 不依赖他人进度，fake 实现跑通全链路 |
| 接口归属 | AgentAdapter 等接口在 swarm 侧定义 | Server/Gateway 掌控对 Agent 的契约 |
| Schema 范围 | ReqMethod ~100 个 + EventType ~30 个完整定义 | 后续方法扩展不改枚举，增量工作 |
| MVP 方法集 | 10 个 req_method + 11 个 EventType | 核心交互可用，其余渐进 |
| 开发节奏 | 自底向上 6 层 | 每层做扎实，有明确验证点 |
| Params 类型 | json.RawMessage | 延迟解析，性能好，不丢字段 |

---

## 2. 架构总览

```
用户入口                    swarm 层（本方案实现）              agentcore 层（他人实现）
─────────                  ──────────────────                 ─────────────────
CLI REPL ─┐
HTTP API ─┤
Web WS ───┤→ Channel ─→ Gateway ──E2A──→ AgentServer ──AgentAdapter──→ DeepAgent
           │            │         │          │                │
           │            │         │          │                └──→ ReActAgent
           │            │         │          └── AgentManager / SessionManager
           │            │         │
           │            │         └── MessageHandler → ChannelManager
           │            │
           │            └── GatewayServer（WS 服务器组装）
           │
           └── Transport 抽象
               ├── ChannelTransport（进程内 Go channel）← MVP 先做
               └── WebSocketTransport（跨进程 WS）      ← 接口预留
```

### 通信链路

**单进程模式（MVP）**：
```
REPL/HTTP/Web → Channel → Gateway → E2A编码 → Go channel → E2A解码 → AgentServer → AgentAdapter → Agent
```

**跨进程模式（预留）**：
```
Web → Gateway → E2A编码 → WebSocket → E2A解码 → AgentServer → AgentAdapter → Agent
```

---

## 3. 目录结构

```
internal/swarm/
├── doc.go
├── schema/                      # 10.1 Schema 层
│   ├── doc.go
│   ├── req_method.go            # ReqMethod 枚举（~100 个）
│   ├── event_type.go            # EventType 枚举（~30 个）
│   ├── mode.go                  # Mode 枚举
│   ├── message.go               # Message 模型
│   ├── agent.go                 # AgentRequest/AgentResponse/AgentResponseChunk
│   ├── event_base.go            # HookEventBase
│   └── permission.go            # PermissionContext
│
├── e2a/                         # 10.2 E2A 协议
│   ├── doc.go
│   ├── envelope.go              # E2AEnvelope（10 核心 + 扩展字段）
│   ├── response.go              # E2AResponse
│   ├── provenance.go            # E2AProvenance / E2AAuth / IdentityOrigin / E2AFileRef
│   ├── constants.go             # 协议常量
│   ├── wire_codec.go            # 编解码
│   ├── gateway_normalize.go     # Message/E2A/AgentResponse 格式互转
│   └── agent_compat.go          # E2A→AgentRequest
│
├── server/                      # 10.3 AgentServer 核心
│   ├── doc.go
│   ├── agent_ws_server.go       # AgentWebSocketServer（方法分发）
│   ├── adapter/                 # AgentAdapter 接口与工厂
│   │   ├── doc.go
│   │   ├── interface.go         # AgentAdapter 接口定义
│   │   ├── factory.go           # create_adapter() 工厂
│   │   ├── agent_adapter.go     # Agent 模式适配器
│   │   ├── code_adapter.go      # Code 模式适配器
│   │   ├── deep_adapter.go      # Deep 模式适配器
│   │   └── fake_adapter.go      # Fake 实现（跑通链路用）
│   ├── runtime/                 # 运行时管理
│   │   ├── doc.go
│   │   ├── agent_manager.go     # AgentManager
│   │   ├── session_manager.go   # SessionManager
│   │   └── jiowenclaw.go        # JiuWenClaw 门面
│   └── gateway_push/            # Transport 层
│       ├── doc.go
│       ├── transport.go         # AgentTransport 接口
│       ├── channel_transport.go # ChannelTransport（进程内）
│       └── ws_transport.go      # WebSocketTransport（跨进程，接口预留）
│
├── gateway/                     # 11.x Gateway 核心
│   ├── doc.go
│   ├── channel/                 # Channel 体系
│   │   ├── doc.go
│   │   ├── base.go              # BaseChannel 接口
│   │   ├── manager.go           # ChannelManager
│   │   ├── message_handler.go   # MessageHandler
│   │   ├── web.go               # Web 通道（WS + HTTP RPC）
│   │   └── repl.go              # REPL 通道
│   ├── routing/                 # 路由
│   │   ├── doc.go
│   │   ├── agent_client.go      # WebSocketAgentServerClient
│   │   └── gateway_server.go    # GatewayServer（WS 服务器组装）
│   └── local/                   # 本地处理器
│       ├── doc.go
│       └── handlers.go          # config.get/models.list 等本地方法
│
└── cmd/                         # 12.x CLI 入口
    ├── doc.go
    ├── root.go                  # 根命令
    ├── chat.go                  # uapclaw chat
    ├── serve.go                 # uapclaw serve
    └── web.go                   # uapclaw web
```

---

## 4. Runner 解耦接口

定义在 `internal/swarm/server/adapter/` 下，swarm 侧掌控契约：

```go
// AgentExecutor 替代 Runner.run_agent()
type AgentExecutor interface {
    RunAgent(ctx context.Context, agent Agent, inputs map[string]any, opts ...Option) (any, error)
    RunAgentStreaming(ctx context.Context, agent Agent, inputs map[string]any, opts ...Option) (<-chan AgentResponseChunk, error)
}

// AgentRegistry 替代 Runner.resource_mgr
type AgentRegistry interface {
    GetTool(toolID string) (tool.Tool, error)
    AddTool(t tool.Tool) error
    GetAgent(agentID string) (BaseAgent, error)
    AddMCPServer(config McpServerConfig) error
}
```

MVP 阶段提供 fake 实现，他人完成领域六+九后替换为真实实现。

---

## 5. AgentAdapter 接口

```go
// AgentAdapter Agent 适配器接口（swarm 侧定义）
type AgentAdapter interface {
    // Initialize 初始化 Agent
    Initialize(ctx context.Context, req *AgentRequest) (*AgentResponse, error)
    // Invoke 非流式调用
    Invoke(ctx context.Context, req *AgentRequest) (*AgentResponse, error)
    // Stream 流式调用
    Stream(ctx context.Context, req *AgentRequest) (<-chan AgentResponseChunk, error)
    // Interrupt 中断当前对话
    Interrupt(ctx context.Context, req *AgentRequest) (*AgentResponse, error)
    // Resume 恢复对话
    Resume(ctx context.Context, req *AgentRequest) (*AgentResponse, error)
    // UserAnswer 用户回答 Agent 提问
    UserAnswer(ctx context.Context, req *AgentRequest) (*AgentResponse, error)
    // GetHistory 获取历史记录
    GetHistory(ctx context.Context, req *AgentRequest) (*AgentResponse, error)
    // Close 关闭适配器
    Close() error
}
```

三种模式适配器均实现此接口：
- `AgentAdapter`（agent 模式）：最小实现，直接委托 ReActAgent
- `CodeAdapter`（code 模式）：骨架，等 agentcore CodeAgent 就绪
- `DeepAdapter`（deep 模式）：骨架，等 agentcore DeepAgent 就绪
- `FakeAdapter`：MVP 跑通用，返回固定/模拟响应

---

## 6. 步骤计划

### 层 1：Schema 层（10.1）— 8 步

| # | 步骤 | 产出 | 依赖 | 复杂度 |
|---|------|------|------|--------|
| 1 | ReqMethod 枚举 | ~100 个 RPC 方法名常量 | 无 | 低 |
| 2 | EventType 枚举 | ~30 个事件类型常量 | 无 | 低 |
| 3 | Mode 枚举 | 6 个运行模式常量 | 无 | 低 |
| 4 | Message 模型 | 内部统一消息结构 | 1,2 | 低 |
| 5 | AgentRequest / AgentResponse | 请求响应模型 | 1,2,3 | 低 |
| 6 | AgentResponseChunk | 流式响应块 | 2 | 低 |
| 7 | HookEventBase | 钩子事件基类 | 2 | 低 |
| 8 | PermissionContext | 权限上下文 | 无 | 低 |

**验证点**：Schema 全部类型 JSON 序列化往返通过

---

### 层 2：E2A 协议（10.2）— 7 步

| # | 步骤 | 产出 | 依赖 | 复杂度 |
|---|------|------|------|--------|
| 9 | E2AEnvelope | 请求信封（10 核心 + 扩展字段） | 层1 | 中 |
| 10 | E2AResponse | 响应模型 | 层1 | 低 |
| 11 | E2AProvenance / E2AAuth / IdentityOrigin / E2AFileRef | 追踪+认证+身份+文件引用 | 无 | 低 |
| 12 | E2A Constants | 协议常量（版本号等） | 无 | 低 |
| 13 | Wire Codec | 编解码，JSON 序列化往返 | 9,10 | 中 |
| 14 | gateway_normalize | Message↔E2A, AgentResponse→E2A 格式互转 | 9,10,层1 | 中 |
| 15 | agent_compat | E2A→AgentRequest 转换 | 9,层1 | 低 |

**验证点**：Wire Codec 往返、gateway_normalize 往返通过

---

### 层 3：AgentServer 核心（10.3）— 10 步

| # | 步骤 | 产出 | 依赖 | 复杂度 |
|---|------|------|------|--------|
| 16 | AgentTransport 接口 | `Send/Recv/Close` 传输抽象 | 层2 | 低 |
| 17 | ChannelTransport | 进程内 Go channel 实现 | 16 | 中 |
| 18 | AgentAdapter 接口 + 工厂 | 接口定义 + `create_adapter()` 工厂 | 层1 | 中 |
| 19 | FakeAdapter | fake 实现，跑通链路 | 18 | 中 |
| 20 | Agent 模式适配器 | AgentAdapter 最小实现 | 18, agentcore | 中 |
| 21 | Code 模式适配器 | CodeAdapter 骨架 | 18, agentcore | 中 |
| 22 | Deep 模式适配器 | DeepAdapter 骨架 | 18, agentcore | 中 |
| 23 | AgentManager | 多实例管理（按通道/模式） | 18 | 中 |
| 24 | SessionManager | LIFO 会话栈 + CRUD | 层1 | 中 |
| 25 | JiuWenClaw 门面 | SDK 路由：会话队列、流式包装、中断处理 | 23,24,18 | 高 |

**验证点**：FakeAdapter 可 Invoke/Stream，SessionManager CRUD 通过

---

### 层 4：AgentWebSocketServer + GatewayPush（10.3 续）— 4 步

| # | 步骤 | 产出 | 依赖 | 复杂度 |
|---|------|------|------|--------|
| 26 | AgentWebSocketServer | 10 个核心方法分发 | 层3,层2 | 高 |
| 27 | GatewayPush ChannelTransport 集成 | AgentServer 通过 ChannelTransport 推送响应给 Gateway | 16,17,26 | 中 |
| 28 | WebSocketTransport 接口预留 | 接口+骨架，不实现 | 16 | 低 |
| 29 | 适配器辅助 | CodeAgentRail / SysOpBuilder 骨架 | 层3 | 低 |

**验证点**：AgentWebSocketServer 可处理 10 个 MVP 方法

---

### 层 5：Gateway 核心（11.x）— 6 步

| # | 步骤 | 产出 | 依赖 | 复杂度 |
|---|------|------|------|--------|
| 30 | BaseChannel 接口 | `Config/Start/Stop/Send/OnMessage` | 层1 | 低 |
| 31 | ChannelManager | 注册/注销/分发/配置热更新回调 | 30 | 中 |
| 32 | MessageHandler | 入站→E2A→AgentServer，出站→Channel | 31,层2,层4 | 高 |
| 33 | WebSocketAgentServerClient | WS 客户端（MVP 用 ChannelTransport，接口预留 WS） | 16,17 | 中 |
| 34 | GatewayServer | 多路由 WS 服务器组装 | 32,33 | 高 |
| 35 | 本地处理器 | config.get/models.list/session.list/path.get 等 | 层1,31 | 中 |

**验证点**：Gateway 可路由消息：Channel → MessageHandler → AgentServer → 回传

---

### 层 6：CLI 入口 + Web 通道（10.4 + 11.14 + 12.x）— 5 步

| # | 步骤 | 产出 | 依赖 | 复杂度 |
|---|------|------|------|--------|
| 36 | CLI chat 命令 | REPL 交互：读取输入→Gateway→流式输出 | 层5 | 中 |
| 37 | HTTP API | RESTful + SSE 流式 | 层5 | 中 |
| 38 | Web 通道 | WebSocket + HTTP RPC（浏览器接入） | 30,层5 | 高 |
| 39 | 统一启动器 | chat/serve/web 命令→组装 Gateway+AgentServer | 层5,层4 | 中 |
| 40 | Web UI 启动 | 静态文件服务 + WS 连接 | 38,39 | 低 |

**验证点**：
- `uapclaw chat` 可与 Agent 对话（REPL → Gateway → E2A → AgentServer → FakeAdapter）
- 浏览器可访问 Web 通道与 Agent 交互

---

## 7. 依赖关系总图

```
层1 Schema ──→ 层2 E2A ──→ 层3 AgentServer核心 ──→ 层4 WS+Push
                                                     │
                                                     ▼
                                              层5 Gateway核心 ──→ 层6 CLI+Web
```

每层严格依赖前序层，层内步骤可并行。

---

## 8. 步骤统计

| 层 | 步骤数 | 复杂度 |
|----|--------|--------|
| 1 Schema | 8 | 低 |
| 2 E2A 协议 | 7 | 中 |
| 3 AgentServer 核心 | 10 | 高 |
| 4 WS + Push | 4 | 高 |
| 5 Gateway 核心 | 6 | 高 |
| 6 CLI + Web | 5 | 中 |
| **合计** | **40** | |

---

## 9. 不在本方案范围内（延后）

| 内容 | 步骤编号 | 延后原因 |
|------|----------|----------|
| ACP 适配器 | 10.2.9 | 非核心协议 |
| A2A 适配器 | 10.2.10 | 非核心协议 |
| AgentConfigService | 10.3.13 | 配置 CRUD 非必需 |
| TenantAgentPool | 10.3.14 | 多租户非必需 |
| 会话历史/元数据/重命名 | 10.3.16-18 | 高级会话管理 |
| 技能管理 (Server) | 10.3.19-20 | 服务端技能非必需 |
| 服务端辅助 | 10.3.23-26 | 可简化 |
| ACP Stdio / Slash 命令 | 10.4.3-5 | 特定交互入口 |
| Slash Command Parser | 11.4 | 命令解析延后 |
| RouteBinding / SessionMap / InteractionContext | 11.6-8 | 路由细节延后 |
| Cron / 心跳 / IM Pipeline / Hook | 11.10-13 | 服务端增强延后 |
| IM 渠道（9 种） | 11.15-11.26 | MVP 只需 Web 通道 |
| 扩展框架 | 10.5 | 非核心 |
| Swarm 侧 Harness 集成 | 10.6 | 等 agentcore 完成 |

---

## 10. 与他人工作的接口契约

### swarm 侧定义的接口（他人需实现）

| 接口 | 位置 | 用途 |
|------|------|------|
| AgentAdapter | `swarm/server/adapter/interface.go` | Agent 适配器（Initialize/Invoke/Stream/Interrupt/Resume） |
| AgentExecutor | `swarm/server/adapter/interface.go` | 替代 Runner.run_agent() |
| AgentRegistry | `swarm/server/adapter/interface.go` | 替代 Runner.resource_mgr |

### swarm 侧消费的 agentcore 类型（纯数据，已完成 ✅）

| 类型 | 来源包 | 用途 |
|------|--------|------|
| AgentCard | agentcore/single_agent/schema | Agent 元数据 |
| ToolCard | agentcore/foundation/tool | 工具元数据 |
| McpServerConfig | agentcore/foundation/tool/mcp | MCP 配置 |
| ModelClientConfig | agentcore/foundation/llm/schema | 模型配置 |
| UserMessage / AssistantMessage | agentcore/foundation/llm/schema | 消息类型 |
| Session | agentcore/session | 会话实例 |

### MVP 阶段 fake 实现

所有接口提供 `Fake*` 实现，保证全链路可跑通。他人完成 agentcore 后：
1. 实现 `AgentAdapter`（三种模式各一个）
2. 实现 `AgentExecutor`（委托 Runner）
3. 实现 `AgentRegistry`（委托 Runner.resource_mgr）
4. 替换 fake 注册为真实注册
