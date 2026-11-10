# UapClaw Go 实现计划

> 本文档描述将 Python 项目 `jiuwenswarm` + `openjiuwen` 用 Go 语言重新实现的完整计划。
> 所有代码统一放在 `/home/opensource/uap-claw-go` 项目下。

**进度说明**：每个步骤的状态列标记如下：
- ✅ 已完成
- ☐ 未开始
- 🔄 进行中

---

## 项目说明

| Go 项目 | 目录 | 对应 Python 原版 | Python 源码根目录 |
|----------|------|------------------|-------------------|
| 公共基础设施 | `internal/common/` | 两者共享 | — |
| Agent SDK (agentcore) | `internal/agentcore/` | `openjiuwen` SDK | `/home/opensource/agent-core/openjiuwen/` |
| Swarm 平台 | `internal/swarm/` | `jiuwenswarm` 平台 | `/home/opensource/jiuwenswarm-develop/jiuwenswarm/` |

### 依赖关系

```
openjiuwen (agentcore)     ← SDK 库，不可独立运行
       │
       │ 被依赖（70+ 文件 import）
       ▼
jiuwenswarm (swarm)        ← 可运行平台，所有 CLI 入口都走 swarm
       │
       │ 同仓库打包
       ▼
jiuwenbox                  ← 沙箱（独立 cmd/jiuwenbox 入口）
```

**重要**：agentcore 是 SDK 库，不直接暴露给 CLI。所有用户可见的入口（chat/serve/app/acp）都在 swarm 层，swarm 内部调用 agentcore 的 Agent 能力。

### 架构关系

```
所有外部入口统一经过 Gateway，Gateway 与 AgentServer 之间始终使用 E2A 协议通信，
区别仅在于传输层：进程内走 Go channel，跨进程走 WebSocket。

单进程模式（chat / serve / acp / app）：
  REPL/HTTP/stdio/IM → Gateway → E2A编码 → Go channel → E2A解码 → AgentServer → agentcore → LLM

跨进程模式（agentserver + gateway 独立部署）：
  IM/Web → Gateway → E2A编码 → WebSocket → E2A解码 → AgentServer → agentcore → LLM
```

### Gateway-AgentServer 通信架构决策

**决策：统一 E2A 协议 + 传输层切换**

Gateway 与 AgentServer 之间，无论进程内还是跨进程，**统一经过 E2A 编解码**，仅传输层不同：

| 场景 | E2A 协议 | 传输层 | Transport 实现 |
|------|---------|--------|---------------|
| 单进程（chat/serve/acp/app） | ✅ 必经 | Go channel | `ChannelTransport` |
| 跨进程（gateway + agentserver 独立部署） | ✅ 必经 | WebSocket | `WebSocketTransport` |

**Transport 抽象接口（定义在 `swarm/transport` 包）：**

```go
// AgentTransport Gateway ↔ AgentServer 的传输抽象
// 对齐 Python WebSocket: send(json_str) / recv() → json_str
// 不感知 E2A 协议语义，只负责 JSON 字节传输
type AgentTransport interface {
    Send(ctx context.Context, data []byte) error   // 对齐 Python ws.send(json_str)
    Recv() (<-chan []byte, error)                   // 对齐 Python ws.recv()
    Close() error
}
```

**包依赖方向（单向，无循环）：**

```
swarm/transport       ← AgentTransport 接口 + Wire 编码工具（BuildServerPushWire/WireRequestIDKey/BuildConnectionAckFrame）
  ↑                         ↑
  |                         |
swarm/server/gateway_push  swarm/gateway/routing
  (ChannelTransport 实现)    (AgentClient 使用接口)
```

Gateway 侧**禁止** import `swarm/server/` 下任何包。

**为什么统一走 E2A 而非进程内直连？**

| 维度 | 进程内直连（不走 E2A） | 统一 E2A + 传输层切换 |
|------|----------------------|---------------------|
| AgentServer 入口 | 两套（直连 + E2A），需判断来源 | 一套，永远从 E2AEnvelope 解码 |
| 行为一致性 | 需测试两条路径 | 天然一致，E2A 层保证 |
| 调试/排障 | 进程内路径无 E2A 日志 | E2A 日志格式统一 |
| 协议演进 | 需同步维护两套 | E2A 协议改一处即可 |
| 进程内性能 | 零开销 | JSON 编解码微秒级（相对 LLM 调用毫秒~秒级可忽略） |

**参考来源：**

| 模式 | 参考项目 | 关键设计 |
|------|---------|---------|
| 进程内通信 | `/home/opensource/uapclaw-main/` (PicoClaw) | `pkg/bus/` 用 Go channel 做 MessageBus，Gateway 是组合根，AgentLoop 消费 inbound channel |
| 跨进程通信 | `/home/opensource/jiuwenswarm-develop/jiuwenswarm/` (Python) | `common/e2a/` E2A 协议，`gateway/routing/agent_client.py` WS 客户端 |
| E2A 协议 | `/home/opensource/jiuwenswarm-develop/jiuwenswarm/common/e2a/` | E2AEnvelope/E2AResponse/Wire Codec/Provenance 追踪 |
| Channel 模式 | `/home/opensource/uapclaw-main/pkg/channels/` | BaseChannel → HandleMessageWithContext → bus.PublishInbound() |

**受影响的实现章节：**

- **10.2 E2A 协议**：E2A 编解码是核心，进程内外共用
- **10.3 AgentServer**：只需一套 E2AEnvelope 解码入口，无需区分来源
- **10.4 独立交互入口**：chat/serve/acp 作为"轻量 Channel"接入 Gateway，不再是"绕过 Gateway 直连 AgentServer"
- **10.3.21-22 GatewayPush**：Transport 抽象层实现
- **11.5 WebSocketAgentServerClient**：跨进程 Transport 的 WS 实现
- **11.x Gateway 核心**：Gateway 统一路由所有入口（含 REPL/HTTP/stdio）
- **12.7-12.9 CLI 入口**：命令区分的只是进程组合方式，不是通信路径

---

## 使用方式对比

| 模式 | 启动命令 | Gateway | AgentServer | Transport |
|------|---------|---------|-------------|-----------|
| CLI REPL | `uapclaw chat` | 进程内（REPL 作为 Channel） | 进程内 | ChannelTransport |
| HTTP API | `uapclaw serve` | 进程内（HTTP 作为 Channel） | 进程内 | ChannelTransport |
| ACP Stdio | `uapclaw acp` | 进程内（stdio 作为 Channel） | 进程内 | ChannelTransport |
| 完整模式 | `uapclaw app` | 进程内（IM/Web 等全部 Channel） | 进程内 | ChannelTransport |
| 独立 AgentServer | `uapclaw agentserver` | 无（等待外部 Gateway 连入） | 独立进程，监听 WS | — |
| 独立 Gateway | `uapclaw gateway` | 独立进程 | 无（连接外部 AgentServer） | WebSocketTransport |
| Web UI | `uapclaw web` | 进程内（Web Channel） | 进程内 | ChannelTransport |

> 注意：chat/serve/acp/app 命令的区别仅在于"接入 Gateway 的 Channel 类型不同"，底层统一走 Gateway → E2A → AgentServer 路径。

---

## 最小可用路径 (MVP)

```
领域一 → 领域二 → 领域三 → 领域五(会话部分) → 领域六(ReAct Agent)
→ 领域十(10.1 Schema + 10.2 E2A + 10.3 AgentServer + 10.4 CLI聊天)
```

完成以上步骤后，用户即可运行 `uapclaw chat` 与 Agent 直接对话。
`uapclaw chat` 内部流程：REPL → Gateway → E2A编码 → ChannelTransport → E2A解码 → AgentServer → agentcore → LLM。

---

## 编码规范

### 规范 1：所有注释使用中文

所有 Go 源码文件中的注释必须使用中文，包括包注释、函数注释、结构体注释、字段注释、常量注释、行内注释、TODO/FIXME 标注。

### 规范 2：源码声明排列顺序

每个 Go 源码文件内部，必须严格按以下顺序排列声明：

```
1. 结构体 (type struct / type interface)    — 接口排在结构体之前
2. 枚举   (type iota)
3. 常量   (const)
4. 全局变量 (var)
5. 导出函数 (func Xxx)                      — 大写开头
6. 非导出函数 (func xxx)                    — 小写开头
```

各类声明之间用分隔注释区分：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

**特例：**
- 类型别名 (type X = Y) 归类到枚举区块
- init() 函数归类到非导出函数区块
- 文件头只保留 package 和 import，不要在 import 后直接写 const/var

### 规范 3：单元测试覆盖率与构建标签

每个 Go 源码文件都必须配备对应的单元测试文件（`_test.go`），**整体测试覆盖率必须 ≥ 85%**。无法 mock 的外部依赖用 `//go:build` 标签隔离，被隔离的代码不纳入覆盖率计算基线。

- 每个包必须有 `*_test.go` 文件
- 导出函数必须有测试用例覆盖
- **覆盖率目标：≥ 85%**（`go test -cover ./...`），未达标的包需补充测试
- 可 mock 的必须 mock（`httptest`/接口注入/`t.TempDir()`），**禁止**用 build tag 逃避
- 真实外部环境（LLM API/数据库/网络端口等）无法 mock 的，使用 `//go:build` 标签隔离

**构建标签约定：**

| 标签 | 用途 | 运行方式 |
|------|------|---------|
| `integration` | 集成测试（真实数据库/消息队列） | `go test -tags=integration ./...` |
| `llm` | LLM API 真实调用 | `go test -tags=llm ./...` |
| `e2e` | 端到端测试 | `go test -tags=e2e ./...` |

**build tag 豁免规则（仅以下情况不计入覆盖率基线）：**

1. 无法 mock 的真实外部服务：LLM API、第三方 SaaS、远程 RPC
2. 依赖外部运行环境：数据库、消息队列、文件系统特定路径
3. 硬件/OS 绑定：CGO 调用、系统调用、平台特定行为

**测试函数命名：** `TestXxx` / `TestXxx_场景描述` / `TestXxx_真实调用`（集成测试统一后缀）

**覆盖率检查命令：**

```bash
# 运行单元测试并查看覆盖率
go test -cover ./...

# 生成覆盖率详情
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# 排除 build tag 隔离文件后计算覆盖率
go test -cover -tags=!integration,!llm,!e2e ./...
```

详细规范见 `.codebuddy/rulers/go-code-conventions.md`。

---

## 垂直领域划分

按 **29 大垂直领域** 组织，每个领域自底向上完整实现，可独立验证。

- **领域一 ~ 十四**：基线上游（jiuwenswarm 0.2.0 / openjiuwen 0.1.13）的迁移实现。
- **领域十五 ~ 二十九**：2026-11-04 上游同步扩充（jiuwenswarm 0.2.5.beta1 / openjiuwen 0.1.20），依据 `docs/sync/`；行为缺陷不在其中，见 [`docs/sync/defect-plan-2026-11-04.md`](docs/sync/defect-plan-2026-11-04.md)。

---

## 领域一：基础设施与公共层

> 两个项目共享的底座，包括配置、日志、Schema、工具函数

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 1.1 | ✅ | Go Module 初始化 + 目录结构 | `go.mod`，完整目录树 | — |
| 1.2 | ✅ | BaseCard / BaseParam 模型 | `BaseCard{ID, Name, Description}`，`ToolInfo()` | `openjiuwen/core/common/schema/card.py` |
| 1.3 | ✅ | 异常体系 | `BaseError`，`StatusCode` 枚举 | `openjiuwen/core/common/exception/base_error.py` · `codes.py` |
| 1.4 | ✅ | YAML 配置管理 | 读写 config.yaml，`${VAR:-default}` 环境变量解析，fsnotify 热重载，DecryptFunc 钩子，专用分段方法，DeepMerge 迁移 | `jiuwenswarm/common/config.py` |
| 1.5 | ✅ | 日志系统 | zerolog 分级日志，轮转文件输出，敏感数据过滤 | `jiuwenswarm/common/utils.py` (logging setup) |
| 1.6 | ✅ | 加密工具 | AES-256-GCM 加密/解密，CryptRegistry 注册表，CryptoProvider 接口，AesGcmProvider，NewDecryptFunc 桥接 config | `openjiuwen/core/common/security/crypt_utils.py` · `jiuwenswarm/common/security/base_crypto.py` |
| 1.7 | ✅ | 工作区管理 | `~/.uapclaw` 路径解析，初始化，命名实例，增量复制，语言选择 | `jiuwenswarm/common/utils.py` (路径管理) · `jiuwenswarm/init_workspace.py` · `jiuwenswarm/instance_manager/` |
| 1.8 | ✅ | 通用工具 | 端口等待，单例模式，连接池，后台任务 | `jiuwenswarm/common/utils.py` (port_wait) · `openjiuwen/core/common/utils/` |
| 1.9 | ✅ | 版本管理 | 版本号统一管理，ldflags 注入，BuildInfo()，预发布版本识别，GitHub Releases 版本源 | `jiuwenswarm/common/version.py` · `jiuwenswarm/common/version_source.py` |
| 1.10 | ✅ | WebSocket Origin 检查 | OriginChecker 校验器，gorilla/websocket 适配器，net/http 中间件适配器 | `jiuwenswarm/common/security/ws_origin.py` |
| 1.11 | ✅ | dotenv 早期解析 | `--dotenv`/`--name` 参数预解析，自实现 .env 加载器，服务类子命令 PreRunE 调用；init 的 --name 语义不同，不走此回调 | `jiuwenswarm/dotenv_early.py` |

---

## 领域二：LLM 基础层

> Agent 能"说话"的前提 — 模型调用能力

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 2.1 | ✅ | 消息模型 | `BaseMessage`/`UserMessage`/`SystemMessage`/`AssistantMessage`/`ToolMessage` | `openjiuwen/core/foundation/llm/schema/message.py` |
| 2.2 | ✅ | ToolCall 与 UsageMetadata | `ToolCall{ID, Type, Name, Arguments}`，`UsageMetadata{Tokens, Cost}` | `openjiuwen/core/foundation/llm/schema/tool_call.py` · `message.py` (UsageMetadata) |
| 2.3 | ✅ | 流式消息块 | `AssistantMessageChunk`，增量合并（tool_calls 参数拼接） | `openjiuwen/core/foundation/llm/schema/message_chunk.py` |
| 2.4 | ✅ | 多模态生成响应 | `GenerationResponse`/`ImageGenerationResponse`/`AudioGenerationResponse`/`VideoGenerationResponse` | `openjiuwen/core/foundation/llm/schema/generation_response.py` |
| 2.5 | ✅ | ProviderType / ModelClientConfig | `ProviderType` 枚举，`ModelClientConfig`，`ModelRequestConfig`，`BaseModelInfo`，`ModelConfig` | `openjiuwen/core/foundation/llm/schema/config.py` · `mode_info.py` |
| 2.6 | ✅ | BaseModelClient 接口 | `Invoke()→AssistantMessage`，`Stream()→chan AssistantMessageChunk`，`GenerateImage/Speech/Video` | `openjiuwen/core/foundation/llm/model_clients/base_model_client.py` |
| 2.7 | ✅ | OpenAI 兼容客户端 | HTTP 调用（invoke + stream），SSE 解析；✅ **2.6 回填点已实现**：`init()` 中已调用 `Register("OpenAI", "llm", ...)` + `Register("OpenRouter", "llm", ...)`；✅ **2.13 回填点已实现**：`SanitizeHeaders`/`MergeHeaders` 已统一收敛到 `headers_helper` 包，`client.go` 改用 `headers_helper.BuildBaseHeaders`/`MergeRequestHeaders`；✅ **2.14 回填点已实现**：回调框架 `callback.GetCallbackFramework().Trigger()` 已替换日志记录，事件类型 `LLMCallStarted`/`LLMCallError`/`LLMResponseReceived`；✅ **2.16 回填点已实现**：Stream 中 `_astream_with_parser` 流式解析逻辑已实现（accumulatedContent 缓冲区 + Parse 增量输出）；✅ **2.14/2.15 回填点已实现**：`llm/model_clients_register.go` blank import 触发 `init()` 注册 | `openjiuwen/core/foundation/llm/model_clients/openai_model_client.py` |
| 2.8 | ✅ | DashScope 客户端 | 阿里云百炼适配；✅ **2.6 回填点已实现**：`init()` 中已调用 `Register("DashScope", "llm", ...)` | `openjiuwen/core/foundation/llm/model_clients/dashscope_model_client.py` |
| 2.9 | ✅ | DeepSeek 客户端 | DeepSeek API 适配；✅ **2.6 回填点已实现**：`init()` 中已调用 `Register("DeepSeek", "llm", ...)`；✅ **2.13 回填点已实现**：`SanitizeHeaders`/`MergeHeaders` 已统一收敛到 `headers_helper` 包（DeepSeek 嵌入 OpenAI 客户端，自动继承）；✅ **2.14 回填点已实现**：回调框架已通过 OpenAI 客户端继承（DeepSeek 嵌入 OpenAI，自动使用 CallbackFramework）；✅ **2.16 回填点已实现**：Stream 中 `_astream_with_parser` 流式解析逻辑通过委托 OpenAI 客户端自动继承；✅ **2.14/2.15 回填点已实现**：`llm/model_clients_register.go` blank import 触发 `init()` 注册 | `openjiuwen/core/foundation/llm/model_clients/deepseek_model_client.py` |
| 2.10 | ✅ | SiliconFlow 客户端 | SiliconFlow 适配；✅ **2.6 回填点已实现**：`init()` 中已调用 `Register("SiliconFlow", "llm", ...)`；`sanitizeToolCalls` 为私有方法（对齐 Python: SiliconFlowModelClient._sanitize_tool_calls）；✅ **2.14/2.15 回填点已实现**：`llm/model_clients_register.go` blank import 触发 `init()` 注册 | `openjiuwen/core/foundation/llm/model_clients/siliconflow_model_client.py` |
| 2.11 | ✅ | InferenceAffinity 客户端 | InferenceAffinity (vLLM) 适配，嵌入 OpenAI 客户端复用 HTTP/SSE/解析，覆写 Invoke/Stream（sanitize tool_calls + cache_sharing/cache_salt 参数注入），实现 Release()（KV Cache 释放）；✅ **2.6 回填点已实现**：`init()` 中已调用 `Register("InferenceAffinity", "llm", ...)`；BaseModelClient 接口新增 Release 方法，其他客户端返回不支持错误；`sanitizeToolCalls` 为私有方法（对齐 Python: InferenceAffinityModelClient._sanitize_tool_calls）；✅ **2.14/2.15 回填点已实现**：`llm/model_clients_register.go` blank import 触发 `init()` 注册 | `openjiuwen/core/foundation/llm/model_clients/inference_affinity_model_client.py` |
| 2.12 | ✅ | IntelliRouter 客户端 | IntelliRouter 智能路由适配，自行实现 ReliableRouter（四种策略：simple-shuffle/round-robin/lowest-latency/adaptive），嵌入 OpenAI 客户端复用 HTTP/SSE/响应解析，覆写 Invoke（带重试的路由选择→替换 api_key/api_base→委托 OpenAI）/Stream（选 deployment→委托 OpenAI），BaseClientEmbed 新增 WithSkipValidate 选项，路由缓存共享（MD5 key + double-check locking），Deployment 运行时状态管理（健康/延迟/统计）；✅ **2.6 回填点已实现**：`init()` 中已调用 `Register("intelli_router", "llm", ...)`；✅ **2.14 回填点已实现**：回调框架 `callback.GetCallbackFramework().Trigger()` 已替换日志记录；✅ **2.14/2.15 回填点已实现**：`llm/model_clients_register.go` blank import 触发 `init()` 注册 | `openjiuwen/core/foundation/llm/model_clients/intelli_router_model_client.py` |
| 2.13 | ✅ | Headers Helper | 模型请求头构建辅助；✅ **回填 2.7 已实现**：`openai/request_builder.go` 中的 `SanitizeHeaders`/`MergeHeaders` 已统一收敛到 `headers_helper` 包；产出：`headers_helper/headers_helper.go`（6个导出：ProtectedHeaders/SanitizeHeaders/IsProtectedHeader/BuildBaseHeaders/MergeHeadersCaseInsensitive/MergeRequestHeaders）；`sanitizeToolCalls` 保留在各 client 内部作为私有方法（对齐 Python 各 ModelClient._sanitize_tool_calls），不在 headers_helper 中；common/utils/net.go 中的 headers 代码已清理 | `openjiuwen/core/foundation/llm/headers_helper.py` · `openjiuwen/core/common/utils/header_utils.py` |
| 2.14 | ✅ | Model 门面 | 统一入口，集成回调框架；✅ **回填 2.7 已实现**：所有 model_client 中的 `logger.Info/Error` + `event_type` 日志已替换为 `callback.GetCallbackFramework().Trigger()` 回调触发；产出：`llm/callback/callback.go`（CallbackFramework 最小子集：On/Off/Trigger + LoggingCallback + 9 种 LLMCallEventType 事件）、`llm/model.go`（Model 门面：Invoke/Stream 前后触发回调 + Generate*/Release/BuildKVCacheInvokeKwargs）、`llm/callback.go`（从 callback 子包重新导出类型别名，保持 API 兼容） | `openjiuwen/core/foundation/llm/model.py` |
| 2.15 | ✅ | init_model 工厂 | 便捷创建 Model 实例；✅ **产出**：`llm/init_model.go`（InitModel 函数 + 8 个 Functional Options，默认值对齐 Python：temperature=0.95/topP=0.1/timeout=60/maxRetries=3/verifySSL=false）；✅ **2.14/2.15 回填点已实现**：`llm/model_clients_register.go` blank import 各 model_client 包触发 `init()` 注册；补充 `WithInitSSLCert` 选项（对齐 `ModelClientConfig.SSLCert` 字段） | `openjiuwen/core/foundation/llm/__init__.py` (init_model) |
| 2.16 | ✅ | 输出解析器 | `JsonOutputParser`，`MarkdownOutputParser`；✅ **2.6 回填点已实现**：`BaseOutputParser` 接口扩展 `Parse(input any)` + `StreamParse` 方法 + `StreamParsedResult` 类型；✅ **回填 2.7 已实现**：各客户端 Stream goroutine 中内联实现 `_astream_with_parser` 流式解析逻辑（accumulatedContent + Parse 增量输出）；✅ **方案B独立实现已实现**：严格对齐 Python，每个客户端独立实现 Stream + chunk 解析 + output parser 逻辑 — SiliconFlow 有自己的 `parseStreamChunk`（含费用、不含 token_ids/logprobs、丢弃空 chunk），InferenceAffinity 有自己的 `parseStreamChunk`（不含费用、不含 token_ids/logprobs、丢弃空 chunk），IntelliRouter 有自己的 `convertChunk`（仅提取 content、无 parser），DeepSeek/DashScope 继续委托（对齐 Python 继承）；已移除共享的 `ConsumeSSEStream` 和 `ApplyStreamParser`，各客户端内联 parser 逻辑；导出 `BuildEffectiveHeaders`/`WrapError`/`HandleHTTPError`/`ExtractHTTPHeaders`；产出：`output_parsers/` 包（json_output_parser.go + markdown_output_parser.go + markdown_types.go）；✅ **2.16 补齐已实现**：(1) `Parse` 解析失败返回 `nil, error` 而非 `nil, nil`，空输入仍返回 `nil, nil`；(2) `StreamParse` 接口签名从 `<-chan *AssistantMessageChunk` 改为 `<-chan any`，支持 `string` 和 `*AssistantMessageChunk` 两种 chunk；(3) `ExtractText` 改为返回 `(string, string)` 同时返回 text 和 modelName，日志中附带 `model_name`；(4) `MarkdownOutputParser.Parse` 添加 `defer/recover` 异常捕获，`populateCategorizedLists` 中类型断言改为安全形式 `v, ok := ...`；(5) 日志级别从 `Debug` 改为 `Error`/`Warning` 对齐 Python；(6) `ExtractText` 不支持的输入类型记录 `Warn` 日志；⤵️ **回填点**：`UserConfig.is_sensitive()` 敏感信息过滤 — 当前日志直接输出原始内容（含 `truncateForLog` 截断），等 `common/security/UserConfig` 模块迁移后，参照 `base_client.go` 中 `IS_SENSITIVE` 环境变量模式，对日志中原始 content/json_str 做敏感/非敏感双模式过滤 | `openjiuwen/core/foundation/llm/output_parsers/output_parser.py` |
| 2.17 | ✅ | Prompt 模板系统 | 模板渲染，变量替换；✅ **产出**：`prompt/` 包（variable.go — Variable 接口 + baseVariable + 嵌套属性解析 resolveNestedValue + reflect 辅助；textable_variable.go — TextableVariable 字符串模板变量，支持 `{{placeholder}}` 及自定义前后缀，嵌套属性 `{{user.name}}`，类型自动转换；dictable_variable.go — DictableVariable 字典/列表模板变量，递归处理多模态 ContentPart 中的占位符；assembler.go — PromptAssembler 模板装配器，两阶段替换（先更新 Variable，再回填到 templateContent），支持部分填充；template.go — PromptTemplate 用户面向类，Format 非原地修改返回新实例支持链式调用，ToMessages 将内容转为消息列表）；✅ 已预留错误码复用：StatusPromptAssemblerVariableInitFailed / StatusPromptAssemblerTemplateParamError / StatusPromptTemplateRuntimeError / StatusPromptTemplateInvalid（180000-180004）；✅ 单元测试 44 个用例全部通过 | `openjiuwen/core/foundation/prompt/` |
| 2.18 | ✅ | Model 门面流帧超时 | `ModelClientConfig.StreamFirstChunkTimeout/StreamIdleTimeout`（`*float64`，nil=禁用，默认 300/60）+ `AssistantMessageChunk.CarriesOutputToken` + `Model.Stream` select 接收超时（`waited_since_progress` 只累计等待耗时、框架帧不重置预算、超时 cancel 派生 ctx 并排空、投递 `StreamError` 哨兵）+ `LLM_CALL_ERROR` 回调与 `LLM stream timeout: stream frame timeout: stage=...` 文案逐字对齐；⤴️ 2026-11-06 已回填 9.86 流超时识别与 2.14 门面 | `openjiuwen/core/foundation/llm/model.py` · `schema/config.py` · `schema/message_chunk.py` |

**验证点**：✅ 可通过单元测试调用 LLM，发送消息并收到流式响应

---

## 领域三：工具系统

> Agent 能"做事"的前提 — 工具调用能力

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 3.1 | ✅ | Tool 接口与 ToolCard | `Tool{Card, Invoke, Stream}`，`ToolCard` 扩展 BaseCard | `openjiuwen/core/foundation/tool/base.py` |
| 3.2 | ✅ | ToolInfo / McpToolInfo 模型 | 供 LLM function calling 消费的工具描述 | `openjiuwen/core/foundation/tool/schema.py` |
| 3.3 | ✅ | LocalFunction | Go 函数包装为 Tool，参数 schema 自动提取 | `openjiuwen/core/foundation/tool/function/function.py` |
| 3.4 | ✅ | @tool 装饰器等价 | Go 函数→Tool 注册便捷方式 | `openjiuwen/core/foundation/tool/tool.py` |
| 3.5 | ✅ | MCPTool | MCP 协议工具（SSE/stdio/StreamableHTTP 客户端） | `openjiuwen/core/foundation/tool/mcp/base.py` |
| 3.6 | ✅ | MCP 客户端 | SSE/stdio/OpenAPI/Playwright/StreamableHTTP | `openjiuwen/core/foundation/tool/mcp/client/` |
| 3.7 | ✅ | McpServerConfig | MCP 服务器配置模型 | `openjiuwen/core/foundation/tool/mcp/base.py` (McpServerConfig) |
| 3.8 | ✅ | RestfulApi | HTTP REST 工具，参数映射（path/query/header/body）；✅ **产出**：`tool/service_api/` 包（restful_api.go — RestfulApiCard + RestfulApi + URL/路径参数校验；api_param_mapper.go — APIParamLocation 枚举 + APIParamMapper；response_parser.go — JSON/Text 解析器 + Gzip/Deflate 解压器 + ParserRegistry）；RestfulApiCard 用 InputSchema（map[string]any）替代 InputParams（[]*Param）以支持 location 扩展属性；SchemaUtils 新增 FormatWithSchemaMap 方法；⤵️ **回填点**：FormHandler（✅ 3.10 已回填 FormHandlerManager 集成）、ToolAuth 回调触发（✅ 3.11 已回填）、UrlUtils SSRF 防护（common/security 迁移后回填） | `openjiuwen/core/foundation/tool/service_api/restful_api.py` |
| 3.9 | ✅ | API 参数映射 | 请求参数到 HTTP 各位置的映射；✅ **与 3.8 合并实现**，产出在 `tool/service_api/api_param_mapper.go` | `openjiuwen/core/foundation/tool/service_api/` (APIParamMapper) |
| 3.10 | ✅ | Form Handler | 表单数据处理 | `openjiuwen/core/foundation/tool/form_handler/` |
| 3.11 | ✅ | ToolAuth | 工具认证配置与结果；✅ **产出**：`common/security/` 包（ssl_utils.go — GetSSLConfig + CreateStrictTLSConfig + secureLoadCert）、`tool/auth/` 包（auth.go — ToolAuthConfig + ToolAuthResult；auth_callback.go — AuthType + AuthStrategy + SSLAuthStrategy + HeaderQueryAuthStrategy + AuthStrategyRegistry + RegisterAuthCallback + HeaderQueryProvider）；CallbackFramework 签名改造（回调返回 any，Trigger 返回 []any）；RestfulApi/SseClient/StreamableHttpClient 回填 TOOL_AUTH 回调触发 | `openjiuwen/core/foundation/tool/auth/auth.py` |
| 3.12 | ✅ | Tool 工具函数 | Schema 转换等辅助（SchemaUtils 已在 3.3 中一并实现） | `openjiuwen/core/foundation/tool/utils/` |
| 3.13 | ✅ | AbilityManager | 工具/Workflow/Agent 注册与调度，并行执行，JSON 参数修复 | `openjiuwen/core/single_agent/ability_manager.py` |

**验证点**：✅ 注册 Go 函数为 Tool，LLM 可通过 function calling 调用

---

## 领域四：存储层

> 持久化与检索能力

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 4.1 | ✅ | BaseKVStore 接口 | `Set/Get/Delete/GetByPrefix/BatchDelete/Pipeline` | `openjiuwen/core/foundation/store/base_kv_store.py` |
| 4.2 | ✅ | InMemoryKVStore | 内存实现 | `openjiuwen/core/foundation/store/kv/in_memory_kv_store.py` |
| 4.3 | ✅ | ShelveStore | 文件存储 | `openjiuwen/core/foundation/store/kv/shelve_store.py` |
| 4.4 | ✅ | DbBasedKVStore | 数据库 KV 存储 | `openjiuwen/core/foundation/store/kv/db_based_kv_store.py` |
| 4.5 | ✅ | RedisStore | Redis 实现 | `openjiuwen/extensions/store/redis_store.py` |
| 4.6 | ✅ | BaseVectorStore 接口 | `CreateCollection/AddDocs/Search/DeleteDocs` | `openjiuwen/core/foundation/store/base_vector_store.py` |
| 4.7 | ✅ | CollectionSchema / FieldSchema / VectorField 基类 | 向量集合 Schema 定义 + 索引配置基类与 vf 标签反射机制 | `openjiuwen/core/foundation/store/vector_fields/` |
| 4.8 | ✅ | MilvusVectorStore | Milvus 实现（含 Milvus 索引子类型、距离转换、UpdateSchema rename 策略） | `openjiuwen/core/foundation/store/vector/milvus_vector_store.py` |
| 4.9 | ✅ | ChromaVectorStore | ChromaDB 实现（含 PersistentClient、fieldMapping、距离转换、UpdateSchema double-copy 策略） | `openjiuwen/core/foundation/store/vector/chroma_vector_store.py` |
| 4.10 | ✅ | GaussVectorStore | GaussDB 向量实现（pgx/v5 pgxpool + DiskANN 索引 + 参数化查询、UpdateSchema ALTER TABLE RENAME 策略） | `openjiuwen/extensions/store/gauss_vector_store.py` |
| 4.11 | ✅ | ESVectorStore | Elasticsearch 向量实现（go-elasticsearch/v8 + k-NN 搜索 + _meta 文档持久化、UpdateSchema double-copy 策略） | `openjiuwen/extensions/store/es_vector_store.py` |
| 4.12 | ✅ | BaseDbStore 接口 | SQL 数据库抽象 | `openjiuwen/core/foundation/store/base_db_store.py` |
| 4.13 | ✅ | DefaultDbStore | 默认数据库实现 | `openjiuwen/core/foundation/store/db/default_db_store.py` |
| 4.14 | ✅ | GaussDbStore | GaussDB 数据库实现 | `openjiuwen/extensions/store/gauss_db_store.py` |
| 4.15 | ✅ | BaseMessageStore 接口 | 消息持久化 | `openjiuwen/core/foundation/store/base_message_store.py` |
| 4.16 | ✅ | SqlMessageStore | SQL 消息存储 | `openjiuwen/core/memory/manage/` (SqlMessageStore) |
| 4.17 | ✅ | BaseMemoryIndex 接口 | 记忆索引 | `openjiuwen/core/foundation/store/base_memory_index.py` |
| 4.18 | ✅ | SimpleMemoryIndex | 简单记忆索引实现 | `openjiuwen/core/foundation/store/index/simple_memory_index.py` |
| 4.19 | ✅ | Embedding 接口 | `EmbedQuery/EmbedDocuments/Dimension` + EmbedOption/Callback + MultimodalEmbedder | `openjiuwen/core/foundation/store/base_embedding.py` |
| 4.20 | ✅ | OpenAIEmbedding | OpenAI 向量嵌入（openai-go SDK + 多模态） | `openjiuwen/core/retrieval/embedding/openai_embedding.py` |
| 4.21 | ✅ | DashScopeEmbedding | DashScope 向量嵌入（HTTP 直接调用 + 多模态） | `openjiuwen/core/retrieval/embedding/dashscope_embedding.py` |
| 4.22 | ✅ | APIEmbedding / VLLMEmbedding | API 通用 HTTP 客户端 + VLLM 多模态嵌入 | `openjiuwen/core/retrieval/embedding/api_embedding.py` + `vllm_embedding.py` |
| 4.23 | ✅ | Reranker 接口 | `Rerank/RerankSync` | `openjiuwen/core/foundation/store/base_reranker.py` |
| 4.24 | ✅ | StandardReranker / ChatReranker | 标准和对话式重排序 | `openjiuwen/core/retrieval/reranker/` |
| 4.25 | ✅ | DashScopeReranker | 云服务重排序（支持多模态） | `openjiuwen/core/retrieval/reranker/dashscope_reranker.py` |
| 4.26 | ✅ | Graph Store | 图存储 | `openjiuwen/core/foundation/store/graph/` |
| 4.27 | ✅ | Object Store | 对象存储 (OBS/S3) | `openjiuwen/core/foundation/store/object/` |
| 4.28 | ✅ | Query Builder | 查询构建器 | `openjiuwen/core/foundation/store/query/` |
| 4.29 | ✅ | StorageCodec | 存储编解码协议 | `openjiuwen/core/foundation/store/` (StorageCodec) |

**验证点**：✅ 可存储和检索向量数据，KV 读写正常

---

## 领域五：会话与上下文引擎

> 对话状态管理与上下文窗口控制

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **5.x 会话系统** | — | | | |
| 5.1 | ✅ | State 体系 | `ReadableStateLike`/`RecoverableStateLike`/`StateLike`/`CommitStateLike`；✅ Agent State StateCollection 已回填；✅ Workflow State StateCollection/CommitState/InMemoryState 已回填；✅ SessionController scope/controller 已作为 5.6 独立子包实现，无需回填到 state 包；✅ 5.8 已回填 Checkpointer 持久化 | `openjiuwen/core/session/state/base.py` |
| 5.2 | ✅ | BaseSession 接口 | `Config/State/SessionID/Close`；✅ 5.12 已回填 Config 返回类型；✅ 5.11 已回填 Tracer 返回类型；✅ 5.10 已回填 StreamWriterManager 返回类型；✅ 5.8 已回填 Checkpointer 返回类型；⤵️ ActorManager 返回类型待后续回填 | `openjiuwen/core/session/session.py` |
| 5.3 | ✅ | AgentSession | `PreRun→Invoke/Stream→PostRun`，状态持久化；✅ 已回填 5.1 Agent State StateCollection；✅ 5.7 已回填 Interaction（SimpleAgentInteraction）；✅ card 具体化为 *schema.AgentCard + AgentID()/GetAgentID()/GetAgentName()/GetAgentDescription() 真实实现；✅ 5.12 已回填 Config 返回类型；✅ 5.11 已回填 Tracer 返回类型 + agentSpan 类型 + 自动创建逻辑；✅ 5.10 已回填 StreamWriterManager 返回类型；✅ 5.8 已回填 Checkpointer 返回类型；✅ 5.8 已回填 PreRun/Commit 检查点调用；⤵️ ActorManager 返回类型待后续回填 | `openjiuwen/core/session/agent.py` |
| 5.4 | ✅ | WorkflowSession | 从 AgentSession 创建；✅ 已回填 Workflow State StateCollection/CommitState/InMemoryState；✅ 已实现内部 WorkflowSession/NodeSession/SubWorkflowSession；✅ 已实现外部 WorkflowSession 门面；✅ 已回填 AgentSession.CreateWorkflowSession()；✅ 5.12 已回填 Config 返回类型；✅ 5.11 已回填 Tracer 返回类型；✅ 5.10 已回填 StreamWriterManager 返回类型；✅ 5.8 已回填 Checkpointer 返回类型；⤵️ ActorManager 返回类型待后续回填 | `openjiuwen/core/session/workflow.py` |
| 5.5 | ✅ | SessionNode | ✅ NodeSessionFacade 门面（18 个方法）；✅ Session.GetState 改为 StateKey；✅ AgentStateCollection.GetGlobal/GetAgent 改为 StateKey；✅ 删除 WorkflowSession 多余状态方法；✅ 5.7 已回填 Interaction（WorkflowInteraction）；✅ 5.10 已回填 StreamWriter；✅ 5.11 已回填 Tracer（Trace/TraceError 使用 TracerWorkflowUtils 真实逻辑）；✅ 5.12 已回填 Config | `openjiuwen/core/session/node.py` |
| 5.6 | ✅ | SessionController | 会话控制器；✅ 已实现 Scope/Subject 体系、DataContainer 工厂、ChainSession、SessionController、GlobalSessionController、回调集成；✅ P2P/PubSub 回调已回填（onAgentP2PReceived/onAgentPubsubRegistered + init 注册）；⤵️ AgentSessionContainer.load 简化处理待后续回填；⤵️ LoadAgentSessionContainer 待 create_agent_session 等价函数后回填 | `openjiuwen/core/session/session_controller/` |
| 5.7 | ✅ | Interaction | ✅ BaseInteraction/WorkflowInteraction/SimpleAgentInteraction/AgentInteraction/InteractiveInput/InteractionOutput/GraphInterrupt/Interrupt/AgentInterrupt 已实现；✅ ExecutableIDProvider 类型断言模式（NodeSession 满足，AgentSession 不满足，与 Python 一致）；✅ 已回填 5.5 NodeSessionFacade.Interact()；✅ 已回填 5.3 Session.Interact() | `openjiuwen/core/session/interaction/` |
| 5.8 | ✅ | Checkpointer 接口 + 工厂 | ✅ Checkpointer/Storage/CheckpointerSession 接口；✅ InMemoryCheckpointer + AgentStorage/AgentTeamStorage/WorkflowStorage；✅ CheckpointerFactory/CheckpointerProvider/CheckpointerFactoryConfig；✅ Serializer 接口 + JSONSerializer；✅ 已回填 5.1 Checkpointer 持久化；✅ 已回填 5.2/5.3/5.4 Checkpointer 返回类型；✅ 已回填 5.3 PreRun/Commit 检查点调用；✅ 已回填 5.7 InteractionCheckpointer 迁移；✅ card 具体化为 *schema.AgentCard + AgentID() 方法；✅ 5.12 已回填 Config 返回类型；⤵️ 8.7 回填 GraphStore | `openjiuwen/core/session/checkpointer/base.py` · `factory.py` |
| 5.9 | ✅ | PersistenceCheckpointer | 持久化实现；Storage 体系采用接口注入钩子模式（模拟 Python 模板方法：save/recover/clear/exists 固定骨架 + _get_entity_id/_get_state_to_save/_restore_state 钩子）；InMemory 版 Storage 不需要回填，5.9 独立实现 Persistence 版 Storage | `openjiuwen/core/session/checkpointer/persistence.py` |
| 5.10 | ✅ | StreamWriter | ✅ StreamMode 枚举 + Schema 接口 + OutputSchema/TraceSchema/CustomSchema；✅ StreamQueue + StreamEmitter + StreamWriter + StreamWriterManager；✅ 回填 5.2/5.3/5.4 StreamWriterManager 返回类型（any → *stream.StreamWriterManager）；✅ 回填 5.5 NodeSessionFacade.WriteStream/WriteCustomStream()；✅ 回填 Session.WriteStream/WriteCustomStream/StreamIterator/CloseStream；✅ 迁移 InteractionOutputWriterProvider 到 stream 包；✅ R1 normalizeOutputStream + R2 tagStreamPayload(OutputSchema) 已回填；⤵️ R6 close_stream 回调注销待 callback_framework 支持 unregister_event 后回填 | `openjiuwen/core/session/stream/` |
| 5.11 | ✅ | Session Tracer | ✅ 会话追踪已实现；✅ 回填 5.2/5.3/5.4 Tracer 返回类型（any → *tracer.Tracer）；✅ 回填 5.3 AgentSession agentSpan 类型（any → *tracer.TraceAgentSpan）+ 自动创建逻辑；✅ 回填 5.5 NodeSessionFacade.Trace/TraceError()（使用 TracerWorkflowUtils 真实逻辑）；✅ 打破 tracer→internal 循环依赖（decorator.go 用局部接口 tracerSession 替代 *internal.AgentSession） | `openjiuwen/core/session/tracer/` |
| 5.12 | ✅ | Session Config | ✅ SessionConfig 接口 + defaultSessionConfig 默认实现（BuiltinConfigLoader 钩子，同 5.9 EntityHooks 模式）；✅ WorkflowConfigProvider/AgentConfigProvider 接口占位；✅ MetadataLike 预留字段；✅ 三层优先级环境变量加载（os.Getenv > context.Value > 内置默认值）；✅ WithEnvs context 注入；✅ 回填 5.2/5.3/5.4 Config() 返回类型 any→SessionConfig；✅ 回填 5.5 NodeSessionFacade.GetEnv/GetNodeConfig()；✅ 回填 5.8 Checkpointer GetConfigEnv 改用 Config().GetEnv()；✅ 移除 CheckpointerConfigProvider 过渡接口；✅ AgentID() 启用 config.GetAgentConfig().ID() 优先级链；✅ tracerSessionAdapter 解决 tracer↔interfaces 循环依赖 | `openjiuwen/core/session/config/` |
| 5.13 | ✅ | Session Constants | ✅ 配置键名常量（9 个）；✅ 环境变量键名常量（7 个）；✅ 默认值常量（对齐 Python）；✅ EnvConfigKeys 映射表 + EnvConfigTypes 类型映射；✅ BuiltinDefaults() 函数；✅ InteractiveInputKey/ForceDelWorkflowStateKey 从 checkpointer 迁移 | `openjiuwen/core/session/constants.py` |
| 5.14 | ✅ | Session Utils | ✅ 新建 session/utils 独立子包（path/ref/dict/container/string/constants 六文件）；✅ 纯工具函数从 state/utils.go 迁出并导出（20个函数+3个常量）；✅ getBySchema 等 StateKey 依赖函数保留在 state 包；✅ state 包其他文件回填改用 utils 导出函数；✅ 测试迁移（utils 包 89.5% 覆盖率）；✅ create_wrapper_class 不实现（Python 死代码）；✅ EndFrame 标记 ⤵️ 延后到 8.x stream_actor | `openjiuwen/core/session/utils.py` |
| **5.x 上下文引擎** | — | | | |
| 5.15 | ✅ | ModelContext 接口 | `GetMessages/SetMessages/GetContextWindow/Statistic` | `openjiuwen/core/context_engine/base.py` |
| 5.16 | ✅ | ContextWindow / ContextStats | 上下文窗口与统计；✅ Statistic 改为值类型（与 Python ContextStats() 默认实例对齐）；✅ NewContextWindow 构造函数（空切片+零值初始化）；✅ StatMessages/StatTools/StatContextWindow 预留方法签名；⤴️ 5.31 已回填统计计算逻辑 | `openjiuwen/core/context_engine/base.py` (ContextWindow/ContextStats) |
| 5.17 | ✅ | ContextEngineConfig | 上下文引擎配置；✅ 纯结构体+Validate()校验；✅ NewContextEngineConfig()构造函数初始化空map；✅ schema/子包与Python对齐 | `openjiuwen/core/context_engine/schema/config.py` |
| 5.18 | ✅ | BaseMessage 接口化 + Offload 消息模型 | ✅ BaseMessage struct→interface + DefaultMessage 默认实现；✅ OffloadInfo + 4 个 Offload 子类型 + Offloadable 接口；✅ NewOffloadMessage 工厂 + IsOffloaded 辅助函数；✅ OffloadAssistantMessage 自定义序列化；✅ UnmarshalMessage + UnmarshalOffloadMessage 反序列化工厂；✅ 全项目适配（model_clients/prompt/store/memory/context_engine/session） | `openjiuwen/core/context_engine/schema/messages.py` |
| 5.19 | ✅ | ContextEvent | ✅ ContextEvent 处理器结果类型（processor/base.go）；✅ ContextCompressionState 压缩状态模型（schema/context_state.go，含 ContextCompressionMetric/Saved/Usage 辅助类型 + CompressionStatus/Phase 枚举 + ContextCompressionStateType 常量）；✅ ContextCallEventType 回调事件（5 种）+ ContextCallEventData（callback/events.go）；✅ CallbackFramework 扩展 OnContext/OffContext/TriggerContext（callback/framework.go） | `openjiuwen/core/context_engine/processor/base.py` + `schema/context_state.py` + `runner/callback/events.py` |
| 5.20 | ✅ | TokenCounter | ✅ TiktokenCounter 实现（基于 tiktoken-go/tokenizer）；✅ model2enc 映射表 + ForModel 降级策略；✅ Count/CountMessages/CountTools 三个方法（含 AssistantMessage ToolCalls 特殊处理）；✅ 两级降级（初始化失败→Cl100kBase→len//4）；✅ contentToString 多模态内容提取；✅ 测试覆盖率 ≥ 85% | `openjiuwen/core/context_engine/token/` |
| 5.21 | ✅ | ContextProcessor 插件 | ✅ ContextProcessor 接口（7个方法：OnAddMessages/OnGetContextWindow/TriggerAddMessages/TriggerGetContextWindow/SaveState/LoadState/ProcessorType）；✅ ProcessorConfig 接口（Validate）；✅ BaseProcessor 结构体 + 默认实现（no-op 透传、不触发、空状态）；✅ ProcessorOption/Option 模式（替代 Python **kwargs）；✅ OffloadMessages 方法族（in_memory/filesystem/fallback）；✅ CompressionUsage 追踪（ExtractUsageMetadata/MergeCompressionUsage/RecordCompressionUsage）；✅ GroupCompletedAPIRounds 函数 + IsAPIRound；✅ 5.24 compressor/ 子包重构：DialogueCompressor/MicroCompactProcessor 迁移至 compressor/ 子包，context_utils/ 删除函数移入 compressor/util.go；⤴️ 9.32 已回填 ProcessorOption.SysOperation 类型 + writeOffloadToFile 改用 SysOperation；⤴️ 5.31 已回填 ModelContext.OffloadMessages/WorkspaceDir 方法 | `openjiuwen/core/context_engine/processor/base.py` |
| 5.22 | ✅ | DialogueCompressor | ✅ DialogueCompressorConfig（8字段值类型+默认值）；✅ DialogueCompressor 结构体（嵌入 BaseProcessor）；✅ ProcessorFactory 注册表（context_engine.RegisterProcessorFactory/GetProcessorFactory/ListProcessorFactories）；✅ init() 自动注册到全局注册表；✅ WithCompressorModel 注入选项；✅ 核心方法链（TriggerAddMessages/OnAddMessages/GetCompressIdx/FindLastFinalAssistantIdx/GetCompressPairs/BuildCompressTargets/InvokeMultiBlockCompression）；✅ JSON/Fallback 双路径替换（BuildJSONReplacements/BuildFallbackReplacement）；✅ ReplaceMessages 通用替换函数（从后往前避免索引偏移）；✅ 内置压缩提示词（与 Python DEFAULT_COMPRESSION_PROMPT 对齐）；✅ 辅助方法（WrapMemoryBlock/IsValidBlocksPayload/SerializeMessage/EstimateContentTokens/HasCompressionBenefit/BuildSplitContextPayload/BuildTargetsPayload/ExtractCompactSummaryFromReplacements）；✅ 5.24 迁移至 compressor/ 子包；✅ 49 个单元测试全部通过 | `openjiuwen/core/context_engine/processor/compressor/dialogue_compressor.py` |
| 5.23 | ✅ | MicroCompactProcessor | ✅ MicroCompactProcessorConfig（4字段+默认值+Validate校验）；✅ MicroCompactProcessor 结构体（嵌入 BaseProcessor）；✅ ProcessorFactory 自动注册；✅ 核心方法（TriggerAddMessages/OnAddMessages/collectCompactableIndicesByTool/hasAnyToolExceedThreshold/collectFlatIndicesForCompact）；✅ 5.24 context_utils 子包删除，函数移入 compressor/util.go；✅ 5.24 迁移至 compressor/ 子包；✅ SetContent 原地替换 ToolMessage 内容；✅ force 参数支持；✅ 日志同步；✅ 单元测试全部通过 | `openjiuwen/core/context_engine/processor/compressor/micro_compact_processor.py` |
| 5.24 | ✅ | FullCompactProcessor | ✅ FullCompactProcessorConfig（17字段+默认值+Validate校验）；✅ FullCompactProcessor 结构体（嵌入 BaseProcessor）；✅ ProcessorFactory 自动注册；✅ 双路径压缩（Session Memory 优先/LLM 全量压缩回退）；✅ TriggerAddMessages（IsAPIRound + token 超阈值）；✅ OnAddMessages 主入口；✅ 消息识别方法族（_isBoundaryMessage/_isStateMessage/_isSessionMemoryBoundaryMessage/_isSessionMemorySummaryMessage/_isSyntheticMarkerMessage）；✅ 序列化方法（_serializeMessage完整ToolCalls JSON/_serializeMessages/_formatSummary/_buildFallbackSummary）；✅ 消息选择与截断（_selectMessagesToKeep/_adjustStartIndexForToolPairs/_truncateForPromptBudget）；✅ FullCompactStateReinjector + 4 个 Builder（skills/task_status⤴️5.31/plan_mode⤴️5.31/plan空实现）；✅ buildSkillReinjectedContent 完整实现；✅ BASE_COMPACT_PROMPT 完整翻译；✅ _generateSummary + _buildFullCompactMessages LLM 压缩路径；✅ Session Memory 路径回填（⤴️ 5.31 已回填 _loadSessionMemoryRuntime/_loadSessionMemoryText/_resolveSessionMemoryPath/_selectMessagesAfterSessionMemory/_invalidateSessionMemoryAnchor/buildTaskStatusReinjectedContent/buildPlanModeReinjectedContent）；✅ compressor/ 子包重构（DialogueCompressor/MicroCompactProcessor 迁移 + context_utils 删除移入 util.go）；✅ 测试覆盖率 85.2% | `openjiuwen/core/context_engine/processor/compressor/full_compact_processor.py` |
| 5.25 | ✅ | CurrentRoundCompressor | ✅ CurrentRoundCompressorConfig（11字段+默认值+Validate校验）；✅ CurrentRoundCompressor 结构体（嵌入 BaseProcessor）；✅ ProcessorFactory 自动注册；✅ TriggerAddMessages（Token 超阈值触发）；✅ OnAddMessages 主入口（MODEL_CALL_FAILED 降级 + CONTEXT_EXECUTION_ERROR）；✅ GetCompressIdx（找最新可压缩 UserMessage 边界）；✅ 两阶段压缩（Compress 增量压缩 + MergeSummaryBlocks 二次合并）；✅ 辅助方法（WrapCurrentRoundMemoryBlock 8 元数据头/UnwrapMemoryBlockSummary/BuildPrompt/FormatRecentContext 排除记忆块/FormatPriorContextAndQuery 过滤工具消息+窗口截断）；✅ 提示词与 Python 完全对齐（DEFAULT_COMPRESSION_PROMPT + CLEAN_PROMPT）；✅ util.go 统一提取（20 个共享函数从 full_compact_processor/dialogue_compressor 迁移 + 新增 IsSummaryMessage/CountMessagesTokens/FindLastCompletedAPIRoundEndIdx/IterSummaryMergeRanges/ParseToolArguments/DescribeToolCall/FindToolResultText/ExtractToolResultHint/ExtractSkillNameFromPath/ExtractSkillFileContent）；✅ 日志同步（9 个日志点）；✅ 测试覆盖率 96% | `openjiuwen/core/context_engine/processor/compressor/current_round_compressor.py` |
| 5.26 | ✅ | RoundLevelCompressor | 轮级压缩器 | `openjiuwen/core/context_engine/processor/` |
| 5.27 | ✅ | MessageOffloader | 消息卸载器 | `openjiuwen/core/context_engine/processor/` |
| 5.28 | ✅ | MessageSummaryOffloader | 消息摘要卸载器 | `openjiuwen/core/context_engine/processor/` |
| 5.29 | ✅ | ToolResultBudgetProcessor | ✅ ToolResultBudgetProcessorConfig（兼容字段+实际字段+默认值+Validate）；✅ 嵌入 *BaseProcessor 平级实现；✅ TriggerAddMessages（按轮次检查预算）；✅ OnAddMessages（逐轮卸载最大工具结果）；✅ shouldOffloadMessage（5条规则+IsText判断）；✅ isAllowlistedToolMessage（白名单保护）；✅ messageSize（Token计数+字符/3降级）；✅ offloadToolMessage（两阶段persisted-output构建）；✅ buildPersistedOutputMessage；✅ FindAllDialogueRound（processor/round.go 新增）；✅ EstimateMessageTokens（processor/util.go 新增）；✅ init()自动注册；⤴️ 5.31 已回填 newOffloadHandleAndPath WorkspaceDir；⤴️ 5.31 已回填 OffloadMessages；⤴️ 9.32 已回填 writeOffloadToFile SysOperation；测试覆盖率 85.1% | `openjiuwen/core/context_engine/processor/offloader/` |
| 5.30 | ✅ | ContextEngine 门面 | ✅ ContextEngine 接口修改（移除 RegisterProcessor，补齐 Option 参数）；✅ ModelContext 接口补充 6 个方法（WorkspaceDir/SetSessionRef/OffloadMessages/SaveState/LoadState/CompressContext）；✅ ProcessorSpec 类型；✅ Option 类型（ContextEngineOption/CreateContextOption/CompressContextOption/ClearContextOption + With* 函数）；✅ contextEngine 结构体 + NewContextEngine 构造函数（Option 模式注入 workspace/sysOperation）；✅ CreateContext（池缓存+工厂创建处理器+默认 TiktokenCounter，⤴️ 5.31 已回填 SessionModelContext 构造）；✅ GetContext；✅ CompressContext（委托 mc.CompressContext）；✅ ClearContext（三种粒度+Warn 日志）；✅ SaveContexts（批量持久化+返回 states）；✅ loadStateFromSession/saveStateToSession；✅ processContextID；✅ 6.4-6.10 已回填事件触发（6处TriggerContext）；日志同步（3 个日志点）；测试覆盖率 87.8% | `openjiuwen/core/context_engine/context_engine.py` |
| 5.31 | ✅ | Context 实现 | ✅ context/ 子包单层平铺 7 个文件；✅ SessionModelContext 核心运行时（18 个 ModelContext 接口方法）；✅ sync.Mutex + TryLock 并发控制（AddMessages 快路径/CompressContext 忙返回/GetContextWindow 互斥）；✅ ContextMessageBuffer + OffloadMessageBuffer 消息缓冲；✅ KVCacheManager（*llm.Model + Option 透传）；✅ ProcessorStateRecorder（16 字段输入+BuildState+Emit）；✅ SessionMemoryManager（config/updater 接口抽象+direct_replace 完整实现+agent_edit 骨架）；✅ ContextUtils 8 个工具函数+FindMessageIndexByContextMessageID；✅ 回填 StatMessages/StatTools/StatContextWindow 统计逻辑；✅ 回填 CreateContext SessionModelContext 构造；✅ 回填 FullCompactProcessor Session Memory 6 方法（_loadSessionMemoryRuntime/_loadSessionMemoryText/_resolveSessionMemoryPath/_selectMessagesAfterSessionMemory/_invalidateSessionMemoryAnchor/buildTaskStatusReinjectedContent/buildPlanModeReinjectedContent）；✅ 回填 OffloadMessages/WorkspaceDir（offloader 3 个处理器）；✅ ModelContext 接口新增 GetSessionRef()；⤵️ 6.x 回填 AgentUpdater agent_edit 模式；⤴️ 9.32 已回填 ProcessorOption.SysOperation + writeOffloadToFile | `openjiuwen/core/context_engine/context/` |
| 5.32 | ✅ | recall BM25 原语 | ✅ `recall/bm25.go`：BM25Index（k1=1.5/b=0.75）+ `normalizeMarkdown` + 中英混排 `tokenize`（ASCII 下划线拆分 + CJK 整段与双字滑窗）；✅ 覆盖率 100%；供 `tool_discovery/bm25` 复用；⤵️ `archive`/`retriever`/`compression_recall` 工具另章实施（B-15 在那章收口） | `openjiuwen/core/context_engine/processor/forked/compressor/recall/bm25.py` |

**验证点**：✅ 多轮对话上下文自动管理，超长对话自动压缩

---

## 领域六：Agent 核心

> ReAct 循环 — Agent 能"思考与行动"

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **6.x Agent 模型与接口** | — | | | |
| 6.1 | ✅ | AgentCard / AgentResult 模型 | ✅ AgentCard 迁移至 single_agent/schema/；✅ InputParams/OutputParams 改为 []*schema.Param（与 ToolCard 一致），ToolInfo() 调 ToJSONSchemaMap 生成 JSON Schema；✅ WithInputParamsFromStruct[I]/WithOutputParamsFromStruct[O] 泛型 Option 从 Go struct 反射提取参数（对齐 Python Type[BaseModel]）；✅ TaskStatus 9 值枚举（controller/schema/）；✅ Part（含 RawBytes 自定义 JSON marshal 对齐 Python UTF-8 decode）；✅ Artifact（artifactId camelCase）；✅ AgentResult（taskID snake_case + sessionId camelCase）；✅ 便捷构造函数；✅ 测试全部通过 | `openjiuwen/core/single_agent/schema/agent_card.py` · `agent_result.py` |
| 6.2 | ✅ | BaseAgent 接口 | ✅ BaseAgent 接口（Configure/Invoke/Stream + Card/Config/AbilityManager/CallbackManager/RegisterCallback/RegisterRail/UnregisterRail）；✅ WarpBaseAgent 默认实现（agentInvoker 虚分发 + emit_before/emit_after 回调骨架）；✅ AgentCallEventType/AgentCallEventData/AgentCallbackFunc 新增到 CallbackFramework（OnAgent/OffAgent/TriggerAgent）；✅ StatusAgentNotConfigured 错误码；✅ AgentOptions 扩展（Session/StreamModes + WithSession/WithStreamModes）；✅ 测试全部通过 | `openjiuwen/core/single_agent/base.py` |
| 6.3 | ✅ | ReActAgentConfig | ✅ AgentConfig 接口（ModelName/MemScopeID）；✅ ReActAgentConfig 迁移至 sa/config 包，打破 schema↔interfaces 循环依赖；✅ 回填 ContextEngineConfig→ceschema.ContextEngineConfig, ContextProcessors→[]ceiface.ProcessorSpec；✅ 17字段+Workspace any⤵️ + Option 模式（18基础+4复合）；✅ BaseAgent.Config()/Configure() 回填 any→AgentConfig；✅ SessionConfig 注释更新；✅ Validate+测试 | `openjiuwen/core/single_agent/agents/react_agent.py` (ReActAgentConfig) |
| **6.x 回调框架** | — | | | |
| 6.4 | ✅ | AgentCallbackEvent 枚举 | ✅ AgentCallbackEvent 枚举（type string，12 种事件，含 on_user_message / after_react_iteration，常量 Callback 前缀）；✅ AllCallbackEvents/String/GoString；✅ AgentCallEventType 重命名为 AgentCallGlobalEventType（常量名不变）；✅ 测试全部通过（rail/ 覆盖率 100%） | `openjiuwen/core/single_agent/rail/base.py` |
| 6.5 | ✅ | AgentCallbackContext | ✅ AgentCallbackContext 结构体+构造函数+getter/setter；✅ steering 方法（BindSteeringQueue/PushSteering/DrainSteering/HasPendingSteering/SteeringQueue）；✅ FireLifecycle 方法（inputs 保存/恢复+异常设置）；✅ Fire/Retry/ForceFinish 预留 panic 占位（6.6/6.10 回填）；✅ EventInputs 接口+4 个 Inputs struct 骨架（6.9 回填字段）；✅ ToolCallContext 增加 callbackCtx 字段；✅ 测试全部通过（rail/ 覆盖率 93.0%） | `openjiuwen/core/single_agent/rail/base.py` |
| 6.6 | ✅ | AgentCallbackManager | ✅ AgentCallbackManager 结构体+8个方法；✅ 委托给全局 CallbackFramework PerAgent 域；✅ 命名空间隔离({agentID}_{event})；✅ CallbackInfo[F] 泛型包装（所有域统一）；✅ Functional Options 模式；✅ triggerCallbacks 泛型公共方法+两种执行策略；✅ PerAgent 域 5 个方法；✅ GlobalAgent 域重命名；✅ 回填 Fire()/FireLifecycle() 占位；✅ 回填 BaseAgent 接口/WarpBaseAgent 委托；✅ 测试全部通过 | `openjiuwen/core/single_agent/agent_callback_manager.py` |
| **6.x Rail 系统** | — | | | |
| 6.7 | ✅ | AgentRail 接口 | ✅ AgentRail 接口(10钩子+Init/Uninit/GetCallbacks)；✅ BaseRail no-op默认+CallbackFrom/BuildCallbacks辅助；✅ RegisterRail/UnregisterRail 回填实现；✅ RailAgent 接口扩充 AgentID()；✅ interfaces 包参数类型从 any 改为 AgentRail；✅ WarpBaseAgent RegisterRail 添加 Init 调用、UnregisterRail 添加 Uninit 调用；✅ 测试全部通过 | `openjiuwen/core/single_agent/rail/base.py` |
| 6.8 | ✅ | @rail 装饰器等价 | ✅ RailExecutor 结构体(before/gate/exception/retry/after 完整流程)；✅ fireAfter 辅助方法(CancelledError 保护+异常掩盖处理)；✅ isCancelled 辅助函数；✅ ModelCallRail/ToolCallRail 预定义变量；✅ ConsumeRetryRequest/HasForceFinishRequest 真实实现(✅ 6.10 已回填)；✅ 测试全部通过 | `openjiuwen/core/single_agent/rail/base.py` (@rail decorator) |
| 6.8a | ✅ | ModelBackupRail | ✅ 模型备份护栏一比一复刻 Python（on_model_exception 按序切换备用模型 + request_retry；modelSwitcher 本地接口断言等价 hasattr；GetCallbacks 显式声明钩子映射；index 无锁对齐 Python 串行语义）；✅ 单测 7 用例全分支覆盖；✅ 集成测试对齐 Python test_model_backup_rail.py 意图（主模型 mock 错误注入 → failover 切换 → 重试成功；备用耗尽异常上抛）；跨 agents/rail/interfaces/llm/model_clients 5 子系统 | `openjiuwen/core/single_agent/rail/model_backup.py` |
| 6.9 | ✅ | Rail Inputs | ✅ InvokeQuery 接口(IsInteractiveInput+PlainText)；✅ InvokeQueryString 实现；✅ *InteractiveInput 隐式实现 InvokeQuery；✅ InvokeInputs(query→InvokeQuery/conversationID/result/runKind/runContext+IsHeartbeat/IsLightweightContext/IsCron)；✅ ModelCallInputs(messages/tools/modelContext/response)；✅ ToolCallInputs(toolCall/toolName/toolArgs/toolResult/toolMsg)；✅ TaskIterationInputs(iteration/loopEvent/conversationID/result/query/isFollowUp)；✅ MapInputs 兜底(Dict[str,Any]对齐Python EventInputs Union)；✅ RunKind/RunContext/HeartbeatReason 辅助类型；✅ 测试全部通过 | `openjiuwen/core/single_agent/rail/` (Typed inputs) |
| 6.10 | ✅ | ForceFinishRequest / RetryRequest | ✅ RetryRequest 结构体(DelaySeconds)；✅ ForceFinishRequest 结构体(Result)；✅ RequestRetry/ConsumeRetryRequest 真实实现(consume-and-clear)；✅ RequestForceFinish/ConsumeForceFinish 真实实现(consume-and-clear)；✅ HasForceFinishRequest 真实实现(检查字段)；✅ context.go 字段类型从 any 改为具体类型；✅ executor.go 重试延迟支持(RetryRequest.DelaySeconds + context 取消保护)；✅ 测试全部通过(含重试循环、force-finish 门控、一次性消费) | `openjiuwen/core/single_agent/rail/` |
| **6.x ReAct Agent** | — | | | |
| 6.11 | ✅ | ReActAgent 实现 | ReAct 循环：Think→Act→Observe，最大迭代次数 | `openjiuwen/core/single_agent/agents/react_agent.py` |
| 6.12 | ✅ | 流式输出 | `_inner_stream`，实时推送 Agent 思考过程 | `openjiuwen/core/single_agent/agents/react_agent.py` (_inner_stream) |
| 6.13 | ✅ | KV Cache 释放 | 上下文缓存管理 | `openjiuwen/core/single_agent/agents/react_agent.py` (kv_cache_release) |
| **6.x 中断/恢复系统** | — | | | |
| 6.14 | ✅ | ToolInterruptHandler | 工具级中断管理 | `openjiuwen/core/single_agent/interrupt/handler.py` |
| 6.15 | ✅ | InterruptionState / ToolInterruptionState | 工作流/工具中断状态 | `openjiuwen/core/single_agent/interrupt/` |
| 6.16 | ✅ | ResumeContext | 恢复上下文 | `openjiuwen/core/single_agent/interrupt/` |
| **6.x 技能系统** | — | | | |
| 6.17 | ✅ | Skill 模型 | `Skill{Name, Description, Directory}` | `openjiuwen/core/single_agent/skills/skill_manager.py` (Skill) |
| 6.18 | ✅ | SkillManager | 注册/注销/获取技能，YAML front-matter 加载 | `openjiuwen/core/single_agent/skills/skill_manager.py` |
| **6.x 事件驱动 Controller** | — | | | |
| 6.19 | ✅ | Controller | ✅ Controller 主结构体：两阶段初始化、懒启动、会话绑定/解绑、Stream/Invoke 流式/批量执行、状态持久化、config 级联传播、首帧超时；✅ IntentToolkits 完整实现；✅ IntentRecognizer（✅ 6.23 已回填 LLM 调用）；✅ EventHandlerWithIntentRecognition 完整实现 | `openjiuwen/core/controller/base.py` |
| 6.20 | ✅ | TaskManager / EventQueue | 任务与事件队列 + intent_toolkits.go | `openjiuwen/core/controller/` |
| 6.21 | ✅ | TaskScheduler / EventHandler | 调度器与事件处理器 + intent_recognizer.go | `openjiuwen/core/controller/` |
| 6.22 | ☐ | ControllerAgent | 基于 Controller 的 Agent | `openjiuwen/core/single_agent/base.py` (ControllerAgent) |
| **6.x Runner 编排** | — | | | |
| 6.23 | ✅ | ResourceMgr | Agent/Tool/Workflow/Model/Prompt 全局注册表；✅ SetMcpClientFactory/McpClientFactory 客户端工厂注入（ToolMgr.createClient 优先注入工厂，对齐 Python 系统测试 patch(SseClient) 类级替换语义，先例 swarm AgentManager.SetAgentFactory）；✅ MCP 生命周期集成测试（tests/integration/agentcore/runner/mcp_lifecycle_test.go，对齐 Python test_mcp_tools_sse/stdio/playwright 三用例，走 AddMcpServer→GetMcpToolInfos→Invoke→RemoveMcpServer 全路径） | `openjiuwen/core/runner/resources_manager/resource_manager.py` |
| 6.24 | ✅ | AsyncCallbackFramework | 事件注册/触发/emit_before/emit_after/transform_io | `openjiuwen/core/runner/callback/` |
| 6.25 | ✅ | Runner 单例 | `RunAgent/RunAgentStreaming/RunWorkflow/SpawnAgent`；✅ Agent 资源管理集成测试（对齐 Python test_runner_agent_resource_management 资源管理链路：AddAgent→RunAgent(ByAgentID) 会话全链路→RemoveAgent→再运行失败；交互中断恢复部分依赖 8.14-8.24 ⤵️） | `openjiuwen/core/runner/runner.py` |
| 6.26 | ✅ | RunnerConfig | Runner 配置（distributed_mode 等） | `openjiuwen/core/runner/runner_config.py` |
| 6.27 | ✅ | LocalMessageQueue | 本地消息队列 | `openjiuwen/core/runner/message_queue_inmemory.py` |
| 6.28 | ✅ | Spawn 子进程 | JSON over stdin/stdout，`SpawnedProcessHandle` | `openjiuwen/core/runner/spawn/` |
| 6.29 | ✅ | Agent Prompts | Agent 系统提示词模板 | `openjiuwen/core/single_agent/prompts/` |

**验证点**：✅ 完整 ReAct 循环可用：用户提问 → Agent 思考 → 调用工具 → 返回结果

---

## 领域七：记忆、安全与检索

> Agent 拥有"记忆"能力与安全护栏

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **7.x 记忆系统** | — | | | |
| 7.1 | ✅ | CodingMemoryManager | 轻量编程记忆，frontmatter 存储，冲突检测 | `openjiuwen/core/memory/lite/manager.py` |
| 7.2 | ✅ | CodingMemoryTools | 编程记忆工具（读写搜索） | `openjiuwen/core/memory/lite/coding_memory_tools.py` |
| 7.3 | ✅ | CodingMemoryToolContext | 编程记忆工具上下文 | `openjiuwen/core/memory/lite/coding_memory_tool_context.py` |
| 7.4 | ✅ | MemoryConfig | 记忆配置 | `openjiuwen/core/memory/lite/config.py` |
| 7.5 | ✅ | Frontmatter 解析 | YAML frontmatter 读写 | `openjiuwen/core/memory/lite/frontmatter.py` |
| 7.6 | ✅ | FragmentMemoryManager | 片段记忆管理（含冲突检查回填 ✅） | `openjiuwen/core/memory/manage/` |
| 7.7 | ✅ | SummaryManager / VariableManager | ✅ SummaryManager + ✅ VariableManager（独立实现，不嵌入 memoryManagerBase）+ ✅ KvPrefixRegistry | `openjiuwen/core/memory/manage/` |
| 7.8 | ✅ | WriteManager / SearchManager / MemUpdateChecker | ✅ MemUpdateChecker LLM 驱动冲突检查 + ✅ PromptApplier（运行时读文件+缓存） + ✅ WriteManager 写入路由器 + ✅ SearchManager 搜索路由器 + ✅ FragmentMemoryManager 冲突检查回填 + ✅ lite runChecker 回填 | `openjiuwen/core/memory/manage/update/` · `search/` · `index/write_manager.py` |
| 7.9 | ✅ | Memory DB Models | MemoryType/OperationType/FragmentMemoryUnit/VariableUnit/SummaryUnit；⤴️ 回填 UserMemStore + SemanticStore + SupportMemoryType + memory/common/base.go | `openjiuwen/core/memory/manage/mem_model/` |
| 7.10 | ✅ | Memory Index | ✅ BaseMemoryIndex 接口 + SimpleMemoryIndex 实现 + BaseMemoryManager 接口 + FragmentMemoryManager + SummaryManager + VariableManager + WriteManager（实际已在 7.6/7.7/7.8 中完成） | `openjiuwen/core/memory/manage/index/` · `openjiuwen/core/foundation/store/` |
| 7.11 | ✅ | GraphMemory | 实体抽取，三元组存储 | `openjiuwen/core/memory/graph/graph_memory/` |
| 7.12 | ✅ | Graph Extraction | 图实体抽取 | `openjiuwen/core/memory/graph/extraction/` |
| 7.13 | ✅ | MemoryProvider 协议 | MemoryProvider 接口 + BaseMemoryProvider + ProviderOption + ExternalMemoryRail（Provider 桥接 + prefetch + syncTurn + 熔断器） | `openjiuwen/core/memory/external/provider.py` |
| 7.14 | ✅ | Mem0Provider | Mem0 适配（自封装 HTTP 客户端 + 双重熔断器 + QueuePrefetch goroutine） | `openjiuwen/core/memory/external/mem0_provider.py` |
| 7.15 | ✅ | OpenVikingProvider | ✅ vikingClient（health/post/get/close + 身份 Header + 30s 超时）+ ✅ OpenVikingProvider（5 工具 Schema + Initialize 健康检查 + Prefetch + SyncTurn + HandleToolCall 5 分支 + OnSessionEnd commit + Shutdown）；环境变量 fallback；无内建熔断器；日志同步 7 点；测试覆盖率 88.5% | `openjiuwen/core/memory/external/openviking_memory_provider.py` |
| 7.16 | ✅ | OpenJiuwenMemoryProvider | ✅ OpenJiuwenProvider（双模式构造+config 驱动工厂+LTM 全局单例+ltm_search/ltm_search_summary 2 工具+Prefetch+SyncTurn+Shutdown+deep_adapter_rails 回填） | `openjiuwen/core/memory/external/openjiuwen_memory_provider.py` |
| 7.17 | ✅ | AgentArtsMemoryProvider | ✅ AgentArtsProvider（自封装 HTTP REST 客户端 + session 映射 + consecutiveFailures 计数 + external_memory_search 工具） | `openjiuwen/core/memory/external/agentarts_memory_provider.py` |
| 7.18 | ✅ | LongTermMemoryExtractor | ✅ LongTermMemoryExtractor（ExtractLongTermMemory + buildTimeContext）+ ✅ ExtractMemoryParams + ✅ MemoryOperationParams + ✅ MemoryScopeConfig + DefaultMemoryScopeConfig | `openjiuwen/core/memory/process/extract/` |
| 7.19 | ✅ | MemoryAnalyzer / Generator | ✅ MemoryAnalyzer（Analyze + VariableResult + MemoryAnalyzerResult）+ ✅ Generator（GenAllMemory + categoriesToMemoryUnit + processExtractedData + processSummaryData + getFragmentMemoryUnit + processProactiveMemoryData + semanticValidation + handleMemoryWithInstruct + processMemoryOperations）+ ✅ MemoryEngineConfig + ✅ AgentMemoryConfig + ✅ ScopeConfig 补全（ModelCfg/ModelClientCfg/EmbeddingCfg）| `openjiuwen/core/memory/process/extract/` |
| 7.20 | ✅ | Dreaming Orchestrator + Sweeper | ✅ 两层架构：Orchestrator（agentcore 纯调度）+ Sweeper（swarm 业务管线 Scan→Compress→LLM Extract→Promote）；✅ 4 套提示词模板（code/agent × zh/en）一比一复刻 Python；✅ 公共 API（StartDreaming/StopDreaming/GetDreamingOrchestrator）；✅ DeepAdapter TryStartDreaming/TryStopDreaming 回填完成；68 个测试；✅ 集成用例 37 个（原有 20 + 12 管线增量 + 4 接线 + 1 real-llm），跨 3 子系统，审计豁免已撤销 | `openjiuwen/core/memory/dreaming/` · `jiuwenswarm/agents/harness/common/memory/dreaming/` |
| 7.21 | ✅ | MigrationPlan | 迁移计划（OperationRegistry + BaseOperation + Operations + MigrationPlan） | `openjiuwen/core/memory/migration/migration_plan.py` |
| 7.22 | ✅ | Migration Operations | 迁移操作注册表 + 共享工具函数（ComputeNewSchema + BuildTransformFunc） | `openjiuwen/core/memory/migration/operation/` |
| 7.23 | ✅ | Migration Migrators | KV/SQL/Vector/Index/Message 迁移器 + run_migrations 编排 + 4 种 VectorStore UpdateSchema 回填 | `openjiuwen/core/memory/migration/migrator/` |
| 7.24 | ✅ | Memory Codec | ✅ AesStorageCodec（AES-256-GCM + passthrough + 容错降级）+ keyedProvider 适配器 + 18 个测试对齐 Python（覆盖率 94.4%） | `openjiuwen/core/memory/codec/` |
| 7.25 | ✅ | Memory Common | 记忆公共工具（base.go + KvPrefixRegistry 已在 7.7/7.9 回填 ✅；DistributedLock ✅） | `openjiuwen/core/memory/common/` |
| 7.26 | ✅ | Memory Prompts | 记忆提示词（⤴️ 7.8 回填：PromptApplier 单例 + 4 个 .md 提示词模板；测试覆盖率 94.6%） | `openjiuwen/core/memory/prompts/` |
| 7.27 | ✅ | LongTermMemory | ✅ LongTermMemory（sync.Once 单例 + 22 公开方法 + 9 私有方法 + ReActAgent 式回调包装 + 函数式选项 + integration tag 隔离） | `openjiuwen/core/memory/long_term_memory.py` |
| **7.x 安全护栏** | — | | | |
| 7.28 | ☐ | BaseGuardrail 接口 | `ExtractContext/Detect/Register/Unregister` | `openjiuwen/core/security/guardrail/guardrail.py` |
| 7.29 | ☐ | GuardrailBackend | `Analyze(ctx)→RiskAssessment` | `openjiuwen/core/security/guardrail/backends.py` |
| 7.30 | ☐ | GuardrailResult / RiskAssessment | 安全检测结果模型 | `openjiuwen/core/security/guardrail/` (models) |
| 7.31 | ☐ | PromptInjectionGuardrail | 提示注入检测 | `openjiuwen/core/security/guardrail/` |
| 7.32 | ☐ | JailbreakGuardrail | 越狱检测 | `openjiuwen/core/security/guardrail/` |
| 7.33 | ☐ | Security Schema | 安全数据模型 | `openjiuwen/core/security/schema/` |
| **7.x 检索系统** | — | | | |
| 7.34 | ✅ | 检索嵌入适配 | ✅ BaseEmbedding 接口 + EmbeddingConfig + Callback + APIEmbedding（通用HTTP+3种响应格式+重试+并发）+ OpenAIEmbedding（SDK+Matryoshka+base64）+ DashscopeEmbedding（多模态+Matryoshka）+ VLLMEmbedding（组合OpenAI+messages注入）+ MultimodalEmbedder 接口 + MultimodalDocument + lite EmbeddingProvider/Mock/工厂/环境变量解析；覆盖率 base 100%、retrieval/embedding 87.3% | `openjiuwen/core/foundation/store/base_embedding.py` 及实现 |
| 7.35 | ✅ | 检索重排序适配 | ✅ BaseReranker 接口 + RerankerConfig + Document + RerankOption(instruct/topN/extraParams/multimodalQuery) + RerankerBase(基类: requestHeaders/requestParams/parseResponse/assembleParams) + StandardReranker（/rerank API+重试+ExtraBody+ExtraParams+APIBase去除端点后缀）+ ChatReranker（/chat/completions+logprobs+logit_bias+yesNoIDs+TestCompatibility+size=1校验）+ DashScopeReranker（DashScope格式{model,input,parameters}+多模态查询+多模态文档+RerankMultimodal/Sync）+ api_requests重试工具；覆盖率 base 96.7%、retrieval/reranker 94.1% | `openjiuwen/core/foundation/store/base_reranker.py` 及实现 |
| 7.36 | ✅ | 检索索引 | ✅ BaseMemoryIndex 接口(16方法) + StorageCodec + MemoryDoc + MemorySearchResult + UserScope + MemoryIndexBase(7个默认实现+RWMutex并发安全) + SimpleMemoryIndex(KV+Vector双存储编排：AddMemories按类型分组→嵌入→Vector→KV+ID追踪 / Search嵌入→逐类型向量搜索→KV获取→解码→排序截取 / UpdateMemories先删后加 / DeleteMemories+3种DeleteBy* / GetByID / ListMemories分页+类型排序 / ListUserScopes / SchemaVersion+Backup三方法 / ensureCollection懒创建+缓存)；覆盖率 89.8%；⤵️ retrieval/indexing 文档索引管理器（Indexer+Parser+Chunker+Extractor）缺口见领域十三（含 2026-10-27 扩充的检索查询管线 13.19-13.28；领域十三已覆盖完整写入+读取管线） | `openjiuwen/core/foundation/store/index/` |

**验证点**：✅ Agent 可存取长期记忆，跨会话保持上下文；安全护栏可拦截恶意输入

---

## 领域八：工作流与图引擎 + 多 Agent 团队

> 复杂任务编排与多 Agent 协作能力

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **8.x Pregel 图引擎** | — | | | |
| 8.1 | ☐ | Graph 接口 + 构图实现 | `base.py`：`Graph`(7 方法，含 `GetNodes`) + `ExecutableGraph`(5 方法) + `Router` 类型别名；`graph.py`：`PregelGraph`(`StartNode/EndNode/AddNode/AddEdge/AddConditionalEdges/RegisterBranchTargets/Compile/Reset`，waits 屏障 + branches + branch_targets CNF 解析) + `CompiledGraph`(invoke 生命周期) + `Branch` | `openjiuwen/core/graph/base.py` · `graph.py` |
| 8.2 | ☐ | ExecutableGraph / Executable | `ExecutableGraph`：`Invoke/Stream/Collect/Transform/Interrupt`；`Executable`：`OnInvoke/OnStream/OnCollect/OnTransform` + `SkipTrace/GraphInvoker/PostCommit/ComponentType` | `openjiuwen/core/graph/executable.py` |
| 8.3 | ✅ | PregelNode / Channel ABC | ✅ Go 事实：`node.go` 提供 `NodeInput{Config InnerPregelConfig; State any}`（State 恒 nil 对齐 task.py:149；只能是 any——8.8 的 GraphState 全仓零实例化且 graph↔pregel 成编译期环）+ `NodeFunc func(ctx, NodeInput) error`（唯一出通道是 error，返回值不参与路由）+ `PregelNode{name/fn/routers}` 字段全非导出、指针使用 + `NewPregelNode(name, fn, routers...)`（fn/任一 router 为 nil 即 panic、复制入参切片）+ `addRouter`（承接 8.3a 的原地追加，Python 无此方法）+ `NoopNodeFunc`（对齐 lambda: None）；偏差 D1-D6 归档 spec §4（D1 不做形参名嗅探=8.5 D2 同源、D5 state 恒 any 属终态、D6 指针）；零日志对齐 base.py；`node.go` 覆盖率 100%；设计：docs/superpowers/specs/2026-11-01-pregel-node-8.3-design.md。`PregelNode`(name/func/routers) + `Channel` ABC(`Key`/`NodeName`/`IsReady`/`Accept`/`Consume`/`Snapshot`/`Restore`)；⤴️ `Channel` ABC 已移入 8.4（2026-11-01 方案 B：接口与两个实现同文件同测试，`Accept` 返回 bool、`Consume` 无返回值这些形状看完实现才定），本节只留 `PregelNode` | `openjiuwen/core/graph/pregel/base.py` |
| 8.3a | ☐ | PregelBuilder | `AddNode`/`AddEdge`(1→N static、N→1 barrier 含 CNF 组)/`AddBranch`/`Build` | `openjiuwen/core/graph/pregel/builder.py` |
| 8.3b | ☐ | TaskExecutorPool / NodeTask | FIRST_EXCEPTION 等待 + `_commit_failure`(GraphInterrupt→`__interrupt__`、异常→`__error__` 降级) + `CancelAll` + 签名反射注入 `config`/`state` | `openjiuwen/core/graph/pregel/task.py` |
| 8.3c | ☐ | Pregel 调度引擎 | `PregelLoop.Init`(resume 恢复 channel/node_version/step/pending) + `RunStep`(ready→submit→wait_all→buffer→flush→after_step) + `_save_state_on_error` + `Pregel.Run`(顶层中断转 `{"__interrupt__": value}`)；⚠️ 接入前置条件：`ChannelManager`（8.4）当前零调用方且无锁，必须先补并发锁（或明确串行调用约定）并加 `-race` 并发用例 | `openjiuwen/core/graph/pregel/engine.py` |
| 8.3d | ✅ | PregelConfig / 常量 | ✅ `PregelConfig`(SessionID/RecursionLimit/Ns)/`InnerPregelConfig`(+ParentNs 嵌入)/`CreateInnerConfig`（falsy 补 10000、负数保留、入参不被修改）/`DefaultPregelConfig()`（导出函数而非全局变量，杜绝可变全局）；✅ 11 常量：`Start`/`End`/`MaxRecursiveLimit=10000`/`TaskStatusInterrupt`/`TaskStatusError`/`NsSeparator`/`NsReplaceChar`（后两颗 Python 侧零消费，禁止用于 ns 拼接）/`NsKey`/`ParentNsKey`/`SessionIDKey`/`RecursionLimitKey`（四颗结构体化后零消费，对齐留档）；✅ 回填：checkpointer `isWorkflowInterrupted` 去 `"__interrupt__"` 字面量、13 处「⤵️ 8.7 回填」更正 8.9；包 `internal/agentcore/graph/pregel`（config.go 覆盖率 100%） | `openjiuwen/core/graph/pregel/config.py` · `constants.py` |
| 8.4 | ✅ | ChannelManager / TriggerChannel / BarrierChannel | ✅ 就绪节点集合 + `Buffer/Flush/GetReadyNodes/Consume` + `Snapshot`/`Restore`（跳过 END）+ BarrierChannel 的 CNF(AND-of-OR) 判定与 `barrier:...->name` 路由键；✅ Go 事实：`channels.go` 提供 `Channel` 接口(7 方法，⤴️ 自 8.3 移入，方案 B) + `ChannelSnapshot{Messages,Received}` 单结构体 + `TriggerChannel`/`BarrierChannel`（`Key()` 为 `barrier:(A\|B)&C->name` 合成键、组内升序、构造期深复制并组内去重）+ `ChannelManager`（构造期扫 `#recover`、`Flush` 出错不清 buffer、就绪集与 Received 排序、`Restore` 三类静默、`Buffer()` 返回副本（实施后修订：Go 切片无法复刻 Python list 活视图别名））；偏差 D-1..D-11；`channels.go` 覆盖率 100%。设计：docs/superpowers/specs/2026-11-01-pregel-channels-8.4-design.md | `openjiuwen/core/graph/pregel/channels.py` + `pregel/base.py`(53-84) |
| 8.5 | ✅ | IRouter 三实现 | ✅ `Router` 接口(`Dispatch(ctx, sourceNode) ([]Message, error)`，对应 Python `IRouter.dispatch`) + `Selector`/`SelectorFunc`(`Select(ctx, state any)`，state 恒 nil，见偏差 D2 与 spec §3.2) + `StaticRouter`(1→N→TriggerMessage，空 targets≡边终止)/`BarrierRouter`(N→1→BarrierMessage，target 为 `barrier:a&b->c` 合成键)/`ConditionalRouter`(selector 决定目标；Python 的 str→[]string 归一化上移到调用方，偏差 D3)；三类型不合并(D4)、targets 构造复制(D5)、selector error 原样上抛不包装(D6)、目标类型 Hashable→string 收窄(D7，非 str 在 ns 拼接/barrier 键处必然失败)；偏差 D1-D7 归档 spec §4；`router.go` 覆盖率 100%；⤴️ 本节前移至 8.3 之前（8.3 的 `PregelNode.routers` 需先有 Router 接口）。设计：docs/superpowers/specs/2026-11-01-pregel-router-8.5-design.md | `openjiuwen/core/graph/pregel/router.py` + `pregel/base.py`(23-26) |
| 8.6 | ✅ | Graph Message / Interrupt | ✅ `Message`(单结构体替代 Python 三层继承：`Sender`/`Target`/`Payload`/非导出 `kind`) + `MessageKind`(string 枚举 trigger/barrier；零值空串≡Python 基类被两种 isinstance 双重拒绝) + `NewTriggerMessage`/`NewBarrierMessage`(构造函数收口，包外无法构造非法 kind 组合) + `Kind()`(供 8.4 通道过滤/日志/8.9 打标签) + `Interrupt`(`Value`/`Resumable`/`NS` 三字段)；✅ 同批去重 `session/interaction` 本地 `Interrupt`（`GraphInterrupt.Interrupts` 指向 `pregel.Interrupt`，字段名与 `PanicGraphInterrupt` 签名不变故消费者零改动；依赖方向 interaction→pregel 单向）；偏差 R1-R4 归档 spec §6（R1: Python `pregel/base.py:13` 的 `Interrupt.__init__` 仅收 value，而 `interaction.py:61-63` 实传 `resumable`/`ns` 必抛 TypeError 且无测试，Go 保留两字段以承载作者意图）；`base.go` 覆盖率 100%。设计：docs/superpowers/specs/2026-10-31-pregel-message-interrupt-8.6-design.md | `openjiuwen/core/graph/pregel/base.py`(10-43) |
| 8.7 | ✅ | GraphInterrupt | ✅ `pregel.GraphInterrupt`（单字段 `Interrupts []Interrupt` + 指针 `Error()` 对齐 Python `str(value)` + `PanicGraphInterrupt`；`Resumable`/`NS` 按 8.6 §R1 不删）；⤴️ 已回填：本体从 `session/interaction/base.go` 迁入（原注释「8.7 实现 graph/pregel 时迁移，避免 interaction→graph 反向依赖」已兑现）；⤴️ 已回填：8.6 前移的 `Interrupt` 与 `GraphInterrupt.Interrupts` 类型指向收口完成；⚠️ 8.6 交棒的「补 `isGraphInterrupt()` 标记方法」**不可行**（未导出方法名包限定，跨包不构成同一方法；实测真实 `*pregel.GraphInterrupt` 落入 `case error` 分支、span 状态变 error），改为 `session/tracer/handler.go` 直连 `case *pregel.GraphInterrupt`（偏差 D1）；同批回填 `interaction.InteractionOutputSchema` → `stream.OutputSchema`（D2）；`Error()` 仅供日志、禁止 `r.(error)` 判中断（D3）。包 `internal/agentcore/graph/pregel`（base.go 覆盖率 100%，interaction 91.6%/tracer 87.9%）。设计：docs/superpowers/specs/2026-10-31-graph-interrupt-8.7-design.md | `openjiuwen/core/graph/pregel/base.py` |
| 8.8 | ☐ | GraphState（节点状态片段） | 仅节点返回值聚合：`source_node_id` 以 `operator.add` 归并；⚠️ 与 8.9 检查点的 `GraphState`(ns/step/channel_values/pending_*) 同名不同义，Go 靠包隔离 | `openjiuwen/core/graph/graph_state.py` |
| 8.9 | ☐ | Graph Store | `Store` 接口(`Get/Save/Delete`) + `GraphStore` 日志壳 + `InMemoryStore`(深拷贝 + ns 前缀删除) + `PendingNode`/`CreateState`/`serde`；⤴️ 回填 5.8 checkpointer 的 `graphStore any` 与 4 处 Delete；⤵️ 8.9b：`GraphState.channel_values` 为 `map[string][]pregel.ChannelSnapshot`，序列化时 `Message.kind` 非导出（JSON 反序列化得零值），必须用 `NewTriggerMessage`/`NewBarrierMessage` 重建 kind（8.4 偏差 D-10） | `openjiuwen/core/graph/store/` |
| 8.10 | ☐ | AtomicNode | 原子节点（强制校验 CommitState 后 `CommitCmp`） | `openjiuwen/core/graph/atomic_node.py` |
| 8.11 | ☐ | Vertex | 图顶点（4 能力策略 + retry/timeout + 错误恢复回调 + end-node sanitize + trace + stream 收尾）；⤴️ 依赖 8.17 段的 `ComponentAbility` 枚举提前抽取 | `openjiuwen/core/graph/vertex.py` |
| 8.12 | ☐ | StreamActor | 流式执行器（StreamActor/StreamProcessor/ActorManager）；⤴️ 回填 session 的 `ActorManager any` 占位 | `openjiuwen/core/graph/stream_actor/` |
| 8.13 | ☐ | Graph Visualization | 图可视化 | `openjiuwen/core/graph/visualization/` |
| **8.x 工作流** | — | | | |
| 8.14 | ☐ | Workflow 类 | 编排组件图，会话管理，流式输出 | `openjiuwen/core/workflow/workflow.py` · `_workflow.py` |
| 8.15 | ☐ | WorkflowConfig | 工作流配置 | `openjiuwen/core/workflow/workflow_config.py` |
| 8.16 | ☐ | WorkflowCard | 工作流元数据 | `openjiuwen/core/workflow/base.py` |
| 8.17 | ☐ | StartComp / EndComp | 流程组件 | `openjiuwen/core/workflow/components/` |
| 8.18 | ☐ | LLMComp | LLM 调用组件 | `openjiuwen/core/workflow/components/` |
| 8.19 | ☐ | BranchComp / BranchRouter | 分支组件 | `openjiuwen/core/workflow/components/` |
| 8.20 | ☐ | LoopComp | 循环组件 | `openjiuwen/core/workflow/components/` |
| 8.21 | ☐ | ToolComp | 工具调用组件 | `openjiuwen/core/workflow/components/` |
| 8.22 | ☐ | HTTPRequestComponent | HTTP 请求组件 | `openjiuwen/core/workflow/components/` |
| 8.23 | ☐ | IntentDetectionComp | 意图识别组件 | `openjiuwen/core/workflow/components/` |
| 8.24 | ☐ | QuestionerComp | 交互提问组件 | `openjiuwen/core/workflow/components/` |
| 8.25 | ☐ | KnowledgeRetrievalComp | 知识检索组件 | `openjiuwen/core/workflow/components/` |
| 8.26 | ☐ | ReactComponent | ReAct 执行组件 | `openjiuwen/core/workflow/components/` |
| **8.x 多 Agent 团队** | — | | | |
| 8.27 | ✅ | BaseTeam 接口 | `AddAgent/RemoveAgent/Send/Publish/Subscribe/Invoke/Stream` | `openjiuwen/core/multi_agent/team.py` |
| 8.28 | ✅ | TeamCard / TeamConfig | 团队元数据与配置 | `openjiuwen/core/multi_agent/schema/team_card.py` |
| 8.29 | ✅ | EventDrivenTeamCard | 事件驱动团队卡片 | `openjiuwen/core/multi_agent/schema/` |
| 8.30 | ✅ | TeamRuntime | 消息总线，P2P 通信 | `openjiuwen/core/multi_agent/team_runtime/` |
| 8.31 | ✅ | CommunicableAgent | 可通信 Agent 包装 | `openjiuwen/core/multi_agent/team_runtime/` |
| 8.32 | ✅ | MessageRouter / SubscriptionManager | 消息路由与订阅 | `openjiuwen/core/multi_agent/team_runtime/` |
| 8.33 | ✅ | MessageBus | ✅ 消息总线；✅ 回填 Pub-Sub 火忘错误抑制（handlePubsubMessage 所有错误仅记日志不抛出）；✅ 回填结构化错误码（Stop→InitiationError, Send/Publish produce→ProductionError, Send response→ProcessExecutionError, handleP2P→ProcessExecutionError）；✅ NewMessageBus 签名改为 (*MessageBus, error) 对齐 Python raise 语义 | `openjiuwen/core/multi_agent/team_runtime/` |
| 8.34 | ✅ | HandoffTeam | ✅ HandoffConfig/HandoffRoute/HandoffTeamConfig；✅ HandoffRequest/HandoffHistoryEntry；✅ HandoffSignal/ExtractHandoffSignal 两层提取；✅ HandoffTool 实现 Tool 接口；✅ TeamInterruptSignal/ExtractInterruptSignal；✅ HandoffOrchestrator 主体逻辑；✅ HandoffTeam 实现 BaseTeam 接口全部 13 方法 | `openjiuwen/core/multi_agent/teams/handoff/` |
| 8.35 | ✅ | HierarchicalTeam (msgbus) | ✅ AbilityManagerInterface 接口化（ExecuteResult/AddAbilityResult 迁移到 schema 包，ReActAgent/Controller/ContainerAgent 适配）；✅ CommunicableAgent 移除 AgentID() 解决双重嵌入冲突；✅ HierarchicalTeamConfig；✅ P2PAbilityManager 分区并行派发+Semaphore 限流；✅ SupervisorAgent 双重嵌入 CommunicableAgent+ReActAgent；✅ HierarchicalTeam 实现 BaseTeam 接口全部 13 方法，复用 StandaloneInvokeContext | `openjiuwen/core/multi_agent/teams/` |
| 8.36 | ✅ | HierarchicalTeam (tools) | 层级管理-工具委托模式 | `openjiuwen/core/multi_agent/teams/` |

**验证点**：✅ 可定义和执行工作流 DAG，多 Agent 协作完成任务

> **8.x 实施顺序（2026-11-01 调整）**：8.5 → 8.3 → 8.8 → 8.4 → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b（8.5 自原路线前移：8.3 的 `PregelNode` 必须先有 Router 接口，而 8.5 只依赖已完成的 `Message`。依据见 8.5 spec §5）。
>
> **8.x 实施顺序（2026-11-01 二次调整）**：8.5 → 8.4 → 8.3 → 8.8 → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b（`Channel` 接口随 8.4 落地后 8.4 与 8.3 互不依赖，故 8.4 前移；依据见 8.4 spec §5）。原注记保留。
>
> **8.x 实施顺序（2026-11-01 三次记录）**：8.3 已落地；9 节顺序仍为 8.5 → 8.4 → 8.3 → 8.8 → 8.3a → 8.9a → 8.3b → 8.3c → 8.9b。**本轮明确不处理** 8.3a 与 8.8 的位置问题（8.3a 的 `Build` 返回属 8.3c 的 `*Pregel`；8.8 的 `graph_state.GraphState` 为零实例化死类型且 `graph` 包尚未建立），事实留档见 8.3 spec §5。原注记保留。

---

## 领域九：DeepAgent 应用层 (Harness)

> 面向生产的高级 Agent 封装

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **9.x DeepAgent** | — | | | |
| 9.1 | ✅ | DeepAgent | 包装 ReActAgent + 任务循环 + Rails + 技能 + 子 Agent；✅ init_cwd 在 ensureInitialized 中默认兜底，adapter 层 seedRuntimeCwd 覆盖（对齐 Python 两层 init_cwd） | `openjiuwen/harness/deep_agent.py` |
| 9.2 | ✅ | DeepAgentConfig | DeepAgent 配置 | `openjiuwen/harness/harness_config/` |
| 9.3 | ✅ | DeepAgent Factory | 创建 DeepAgent 实例；⤴️ 9.86 默认注册 ModelAnomalyDetectionRail（`EnableModelAnomalyDetectionRail *bool`，nil→true，`alreadyProvidedByType` 去重） | `openjiuwen/harness/factory.py` |
| 9.4 | ✅ | TaskLoopController | 任务循环控制器 | `openjiuwen/harness/task_loop/` |
| 9.5 | ✅ | LoopCoordinator | 循环协调器 | `openjiuwen/harness/task_loop/` |
| 9.6 | ✅ | TaskLoopEventExecutor | 任务循环事件执行器；产出：`task_loop/executor.go`（TaskLoopEventExecutor + BuildDeepExecutor，DeepAgentProvider 已移至 harness/interfaces/ 改名 DeepAgentInterface）、`task_loop/handler.go`（TaskLoopEventHandler per-round channel + 事件路由 + interactionQueuesProvider 实现）；覆盖率 87.2% | `openjiuwen/harness/task_loop/` |
| 9.7 | ✅ | SessionSpawnExecutor | 会话子进程执行器；产出：`harness/interfaces/deep_agent.go`（DeepAgentInterface + LoopCoordinatorInterface）、`harness/schema/task_type.go`（DeepTaskType + SessionSpawnTaskType 常量）、`task_loop/session_spawn_executor.go`（SessionSpawnExecutor + BuildSessionSpawnExecutor）、`tools/subagent/session_tools.go`（SessionTaskRow + SessionToolkit + SessionsList/Spawn/Cancel 工具 + BuildSessionTools）、`task_loop/handler.go`（completeSessionSpawn 两路分支 + extractResultFromEvent + extractErrorFromEvent + formatSessionSpawnSteer）；ControllerInterface 补 TaskScheduler + TaskManager；SessionsCancel/SpawnTool 对齐 Python 路径（LoopController→TaskScheduler/TaskManager）；NewToolCallOptions nil 防御；task_loop 覆盖率 83.9%、subagent 覆盖率 83.3% | `openjiuwen/harness/task_loop/` |
| **9.x 安全 Rails** | — | | | |
| 9.8 | ✅ | ShellAST 分析 | ✅ ShellStructureFlags+ShellSubcommand+ShellAstParseResult+ShellAstKind枚举；✅ ParseShellForPermission(tree-sitter+go-shlex+保守回退)；✅ tree-sitter Parser 缓存(sync.Once+sync.Mutex)；✅ shlexSplit 未闭合引号返回 error 对齐 Python ValueError；覆盖率 69.4% | `openjiuwen/harness/security/` |
| 9.9 | ✅ | TieredPolicy | ✅ EvaluateTieredPolicy+Strictest+SeverityToDecision；✅ MaybeEscalateShellOperators+MatchedRuleUsesApprovalOverride+RuleToolsCategoryConsistent；✅ GetBuiltinSecurityRules(文件系统读取+mtime检测自动重载)；✅ shellPatternMatches+pathPatternMatches+collectParamRuleHits+collectApprovalOverrideHits；✅ evaluateSingleInvocation+shellAstFloor+aggregateSubcommandResults；✅ withShellCommand 条件注入 command 对齐 Python；覆盖率 69.4% | `openjiuwen/harness/security/` |
| 9.10 | ✅ | 主机校验 | ✅ URLMatcher(hostname去端口 vs netloc含端口)对齐 Python；✅ isHTTPDownloadTarget 只检查 scheme+host 对齐 Python；✅ matchHost 段数匹配移除 host==suffix 分支对齐 Python；✅ Windows (?i) 注入 runtime.GOOS 判断；✅ PatternMatcher+PathMatcher+CommandMatcher；覆盖率 69.4% | `openjiuwen/harness/security/` |
| **9.x 功能 Rails** | — | | | |
| 9.11 | ✅ | ProgressiveToolRail | 渐进式工具权限（⚠️ 旧语义 `search_tools`/`load_tools` + 可见白名单 + 导航节已由 9.11a 取代） | `openjiuwen/harness/rails/` |
| 9.11a | ✅ | 延迟工具目录（deferred tool catalog） | ✅ ToolExposure 注册期策略（显式声明优先 / direct 名单 / 渐进开关）+ registryRevision（7 处递增点，移除普通工具不涨，对齐上游）；✅ `recall/bm25`（BM25Index k1=1.5 b=0.75 + 中英混排分词）与 `tool_discovery/bm25`（指纹/精确名边界优先）；✅ `tool_search`（limit clamp 1..20、精确 schema 渲染）与 `tool_call`（acceptor 注入回调上下文、嵌套 AbilityManager 执行、RelayedToolOutput 转发渲染文本）；✅ 目录附件 delta/snapshot（版本 + delta_count≥10 兜底 + 语言切换 + 附件 metadata 恢复 + 变更即失效授权）；✅ 顶层 tools 过滤 = meta ∪ direct、before_tool_call 四分支门卫、uninit ownership；✅ 删除 search_tools/load_tools/可见白名单/导航节/三个旧配置字段；集成 7 例端到端（含 tool_search→tool_call 闭环） | `openjiuwen/harness/rails/progressive_tool_rail.py` · `harness/tools/tool_discovery/` |
| 9.12 | ✅ | TaskCompletionRail | 任务完成检测（completion_promise 信号）+ ⤴️ 9.89 已回填 goal 生命周期分支（before_invoke/before_model_call 挂/清 goal_protocol 附件、before_task_iteration 校验 state 并替换 `<goal_task>` query、after_model_call 累 token + 捕获上下文、after_task_iteration 消费报告→可选 transcript assessor→assess→apply_assessment，含 interrupt 跳过评估与 round error 直接 BLOCKED 两特判；同文件内扩 `task_completion.go` 334→约 850 行，严格对齐 Python 一模块一文件） | `openjiuwen/harness/rails/` |
| 9.13 | ✅ | TaskPlanningRail | 任务规划 | `openjiuwen/harness/rails/` |
| 9.14 | ✅ | AgentModeRail | Agent 模式切换 | `openjiuwen/harness/rails/` |
| 9.15 | ✅ | HeartbeatRail | 心跳 | `openjiuwen/harness/rails/` |
| 9.16 | ✅ | McpRail | MCP 工具管理 | `openjiuwen/harness/rails/` |
| 9.17 | ☐ | LSPRail | LSP 集成 | `openjiuwen/harness/lsp/` |
| 9.18 | ✅ | SysOperationRail | 系统操作管理 | `openjiuwen/harness/rails/` |
| 9.19-23 | ✅ | 其他 Rails | Security(✅ BaseSecurityRail+TypeName()+SafetyPromptRail+PermissionInterruptRail+TieredPolicy+Patterns+Checker+Suggestions+PermissionEngine+Factory(BuildPermissionInterruptRail+PermissionRailConstructor回调解耦循环依赖)+llm/modelName参数补齐+日志对齐(rail/tool_args/optional_tool_tags字段)+ConfirmAction三路返回+内置规则缓存+mtime检测+runSecurityCheck默认error强制子类实现)/Interrupt(✅ BaseInterruptRail+AskUserRail+ConfirmInterruptRail)/Skill(✅ SkillUseRail: 增量加载+YAML解析+enabled/disabled过滤+SkillTool/ListSkillTool注册+all/auto_list提示词注入+演化经验附加+includeTools幂等注册；✅ 2026-11-04 回填工厂默认装配：`addDefaultRails` 真实构造（workspace skills 节点 + team links + 磁盘 disabled + include_tools 判定 + `skill_budget_*` 配置）；⤴️ 2026-11-05 回填 skill baseline：会话基线（`skill_use` state，schema v1）+ `GetSkillsForSession` 合并视图（基线∪新增）+ `skills.runtime_changes` 附件（中英双语，含演进经验）+ `ClearSessionBaseline` + 三处触发链（技能安装/卸载、MCP 热同步、配置热更新）+ `skill_tool` 全量对齐（路径安全、目录树 `directory_tree`、嵌套子技能 `discovered_skill_names`、`content` 纯文本渲染、媒体提示 hint）+ init 对齐（priority 95、owner 检查、删 ResourceMgr 预删除）；⤵️ `multimodal_skill_mode=attach/branch` 未移植（branch 依赖 mobile_gui skill_branch）；⤵️ `mount_skill_root`/`validate_skill_source` 随 MoA 章节；⤵️ `TeamSkillUseRail` 见 25.10；⤵️ 包技能目录（`DeepAgentConfig.Skills`）消费侧待 **15.15a** 扩展绑定器落地后回填（当前 `skillScanDirs` 只扫 workspace，`instanceOverrides["skills"]` 分支来源错误、随该批删除）；⤵️ 2026-11-05 讨论确定待实施的修正：①移除 `updateRuntimeConfig` 内的 `RefreshSkillRails`（零偏差；清基线等 27 章 visibility handler）②快照签名改取 `SKILL.md` mtime ③技能发现改递归（停止边界+跳过表+环保护）④`RefreshSkillRails` 去掉空目录提前 return)/ContextEngine(✅ ContextProcessorRail+ContextAssembleRail+MergeConfig+RefreshTaskState+FixIncompleteToolContext; ✅ session memory 集成（`session_memory_config`/`session_memory` 解析 → `WithSessionMemoryConfig`，默认 `update_mode=agent_edit` 对齐 Python，预设由 session_memory 开关过滤激活集）)/Memory(✅ MemoryRail+CodingMemoryRail)/Verification(✅ VerificationRail+VerificationContractRail; ⤴️9.29回填)/Subagent(✅ SubagentRail同步模式+✅异步SessionTools回填: Init注册sessions_list/spawn/cancel三工具+toolkit注入+Uninit反向清理+BeforeModelCall注入session_tools节+取消静默路径; E2E见session_cancel_e2e_test.go 6场景; ⤴️9.29回填) Rails | `openjiuwen/harness/rails/` |
| 9.24 | ✅ | EvolutionRail | P1(✅ 契约层: contracts+approval_events+approval_runtime; 覆盖率95.9%) / P2(✅ 基类+纯收集: EvolutionRail基类+EvolutionExtension接口+TrajectoryRail; 覆盖率91.7%; extractor.go resourceManager any→*ResourceMgr) / P3(✅ 单Agent演化: SkillEvolutionRail+slash命令回填+DeepAdapter占位回填+Sharing集成; 对齐Python skill_evolution_rail.py+skill_evolution_sharing.py) / P4(✅ 团队演化: TeamSkillEvolutionRail+team审批回填+DeepAdapter回填; 对齐Python team_skill_evolution_rail.py; ⤵️ 10.6.3-10 Rail实例化/注入) / P5(✅ 跨用户共享组合到SkillEvolutionRail; ⤴️9.80a; ✅G1~G8差距修复对齐Python) / P6(✅ 上下文演化: ContextEvolutionRail(Priority=50+记忆注入+自动总结); ⤴️9.82 P7 ✅) | `openjiuwen/harness/rails/evolution/` |
| 9.86 | ✅ | ModelAnomalyDetectionRail | 瞬时失败分类 + 退避重试 + 退化重复检测 + 流超时识别 + 工具循环压缩（**须按背靠背相邻性判定**，`589da12a`）+ bailout；✅ 落地：重复输出检测（1024 字符尾窗按 rune / 单字符 100 / 多字符 6 次·160 字符 / `pyReprString` 对齐 `{unit!r}` 文案）+ 瞬时分类 8 级优先级（`details.should_retry` > 7 条不可重试文本 > `x-should-retry:false` > `retry-after>60s` > `x-should-retry:true` > 无状态码 > 408/409/429/5xx）+ `Retry-After` 三格式 + 三类计数按 session 分桶（只存正计数，归零即删）+ 背靠背判定 `end != 下一轮 start` + 第 3 次命中 `AbortError(cause=CONTEXT_EXECUTION_ERROR)`；接线：factory 默认开关（`EnableModelAnomalyDetectionRail *bool`，nil→true，对齐上游签名默认）+ adapter `execution_guard.model_anomaly_detection_rail` + 团队成员 `BuildModelAnomalyDetectionRail` + `resources/config.yaml`；控制流中止标记（`details.control_flow_abort`）→ rail 与 `ModelBackupRail` 跳过重试/切模型（上游无此守卫：bailout 会被当瞬时失败重试 14s/18 次调用，收紧后 0.03s）；⤴️ 流超时识别依赖 2.18；偏差：检查器去重靠代码指针、计数器分会话分桶、超时走 `StreamError` 哨兵、默认值仅在 `NewModelClientConfig` 生效、控制流中止守卫为对上游的收紧 | `openjiuwen/harness/rails/model_anomaly_detection_rail.py` |
| **9.x 持续目标（Goal）** | — | | | |
| 9.87 | ✅ | 交互监督器（Goal 前置基座） | 会话级单监督器：`RoundWorkItem`(kind=user/goal + context{goal_id,revision,session_id})/`RoundOutcome`/`InteractionEvent`(goal.updated/execution.error)/`OutputLease`+`InteractionOutputStream`+`OutputLeaseManager`（单消费者租约）+ `EventManager`(user/goal 双队列 + push_goal 按 goal_id+revision 去重 + has_running_goal + discard_goal_work/discard_all_work) + `RunKindGoal` 与 `RunContext.goal_id/revision` + DeepAgent `Start/Stop/AttachOutput/DetachOutput/RunOneRound/ExecuteRound/BuildInteractionNextWork/CancelActiveRound`（**复用现有 setupTaskLoop+SubmitRound+WaitRoundCompletion，不删 runTaskLoop**）+ `_interaction_control_lock` 四段临界区（attach 补排 goal work / send_input 入队 / detach 丢弃待办并取消在飞轮 / close-idle 判定 ACTIVE goal 保持流开）；约束：会话级单监督器 + 单输出租约不可退化；✅ **S1（2026-11-06）**：`schema/interaction.go`（12 类型 + `OutputLeaseManager` 128 满丢弃/限流日志 + `InteractionOutputDriver`/`GoalCoordinator` 窄接口，覆盖率 95.0%；⤴️ 9.88 撤销 `GoalCoordinator` 窄接口（改 `goal.Coordinator`，`BeginAttempt` 返回 `*GoalRecord`，对齐上游 `Optional[GoalRecord]`））/ `task_loop/event_manager.go`（双队列调度，覆盖率 100%）/ 回填 `RunKindGoal`+`IsGoal`、`RunContext.GoalID/Revision`+`normalizeInputs` 三形态 revision、`Session.StreamEmitter()`；✅ **S2+S3（2026-11-06）**：相位机 + `Start/Stop` + `AttachOutput/NextOutput/DetachOutput` + 常驻转发器 + `roundBoundaryFrame` 顺序屏障 + `supervisorLoop`（取 work/提升 follow-up/闲置收尾）+ `SendInput` 四分支 + `RunOneRound`/`BuildInteractionNextWork` + `ExecuteRound` 有界取消（1.5s，含代际校验）+ `DeepAgentInterface` 10 方法 + `TaskLoopController.EnqueueSteer`/`WithTaskID`；✅ 9.89 已回填：GoalManager 构造与 TaskCompletionRail 绑定（`Start` 内构造 `goal.NewGoalManager` 七回调 + `SetGoalCoordinator` + `bindLiveGoalManager`，`task_completion.go` goal 分支已落）；spec `docs/superpowers/specs/2026-11-06-9.87-interaction-supervisor-design.md` · plan `docs/superpowers/plans/2026-11-06-9.87-s1-interaction-types.md` | ⤴️ 25.1a 回填：L1 交互面的首个生产消费者（NativeHarness → TeamHarness）；本行交付时该面零调用方属**预期**（上游 L1 的消费者是 15.6 与 L2，两者本仓均 ☐）；⤵️ 遗留两项已登记：`9.66a-1`（worktree 会话态单真源 + 删 Warn）、`22.3a`（观测 session 绑定） | `openjiuwen/harness/schema/interaction.py` · `harness/task_loop/event_manager.py` · `harness/deep_agent.py`(L3731-4574) |
| 9.88 | ✅ | Goal 域模型 | GoalStatus/GoalAssessmentStatus/GoalStopStrategy/GoalRecord（revision/attempt_count/token_usage/time_used_seconds/active_started_at；from_dict 严格校验 + ACTIVE 时钟自愈）/GoalStopConfig/GoalOperationError + SessionGoalStore（键 `harness.goal.record`，Session state 唯一真源，禁止 sidecar JSON/SQLite）+ GoalManager（唯一写者：set/pause/resume/clear/peek/get/begin_attempt/accumulate_usage/apply_assessment/ensure_active_goal_work_locked；全程持 control lock；pause 不 bump revision 以允许在飞轮自然收尾）+ GoalEvaluator（agent_report/transcript/hybrid 三策略 + max_attempts/token_budget 硬上限 + fenced JSON 容错 + spot-check）；📄 spec `docs/superpowers/specs/2026-11-06-9.88-goal-domain-design.md`（2026-11-06 定稿：9.88 单章 spec + 顺带落 `prompts/sections/goal.go` 的 task-query 四函数；撤销 9.87 的 `GoalCoordinator` 窄接口）；✅ **实施（2026-11-06）**：`goal/{doc,schema,store,manager,evaluation}.go` + `prompts/sections/goal.go`（task-query 四函数，双语逐字快照）+ 42 用例，goal 包覆盖率 93.4%（`-race -count=2` 绿）；`hschema.GoalCoordinator` 删除改 `goal.Coordinator`（`BeginAttempt` 返回 `*GoalRecord`，`DeepAgentInterface` 同步移除 `SetGoalCoordinator`——`interfaces` 不能 import `goal`，Python 侧该方法亦不在公开面） | `openjiuwen/harness/goal/` |
| 9.89 | ✅ | Goal 提示词/工具/生命周期接线 | goal_protocol 附件（SectionName.GOAL_PROTOCOL，priority 88，cn/en 原文逐字）+ `<goal_task>` XML（objective/previous_assessment/current_instruction/budget_notice）+ transcript assessor 提示词 + submit_goal_report / get_current_goal 工具（GoalReportSink 只写 sink 不写 Record，last-write-wins；get_current_goal 只读）+ TaskCompletionRail goal 分支（before_invoke/before_model_call 按 run_kind 挂/清附件、before_task_iteration 校验 state 并替换 query、after_model_call 累 token、after_task_iteration 消费报告→可选 transcript assessor→assess→apply_assessment；interrupt 跳过评估、round error 直接 BLOCKED 两特判）+ DeepAgent 构造 GoalManager 与 rail 绑定；⤴️ 回填 9.12；⤴️ 工具注册见 9.38-49；⤴️ **9.88 已交付**：`goal/` 四文件 + `prompts/sections/goal.go` 的 task-query 四函数 + `goal.Coordinator`（本行只需 `set_goal_manager` 等接线，勿重写）；`SetGoalCoordinator` 为 Go 注入接缝（`Start` 内构造后调用）。**✅ 实施（2026-11-06）四批递进 9 提交**：①前置对齐——`TaskIterationInputs` 补 `RunKind`/`RunContext` + executor 回填；executor 失败路径补 `after_task_iteration`（error-shaped result + `SetException` + `afterFired` 守卫，对齐 Python executor L310-331）②`sections/goal.go` 补 `BuildGoalProtocolSection`（priority 88）/`TranscriptAssessorSystem`/`BuildTranscriptAssessorPrompt` + `SectionGoalProtocol`；`prompts/tools/goal.go` 两 provider（含 schema `required=[status,evidence]`）③`tools/goal` 新包（GoalReportSink + 两工具，非法 status 归一化 continue、`__mapped_content` 渲染惯例）④`task_completion.go` goal 分支（`SetGoalManager` 惰性建 sink/evaluator、`Init`/`Uninit` 工具注册注销、`syncGoalProtocolPrompt` 附件挂清、`doGoalBeforeIteration` 校验+replace query、`doGoalAfterIteration` 两特判→评估→apply、`maybeInvokeTranscriptAssessor` 三策略、`resolveTranscriptModel` 经 `modelSwitcher.GetLLM`、`_ATTEMPT_CONTEXT_MAX_CHARS=64K` 尾截断）+ `deep_agent.go` Start 尾段构造 GoalManager（七回调接 interactionEvents/controlMu/HasOutputStream/cancelActiveRoundLocked/EmitEvent/NotifyWork/Language）+ `bindLiveGoalManager` + `goalPromptLanguage` + 队列注入带 goalLanguage + 系统提示词热重载语言同步。**工具注册时机订正**：计划原写「仅 goal 模式激活期间注册」为笔误，上游 `init()` 语义是 **goal_manager 绑定后全程注册**（普通轮 `get_current_goal` 用于重定位），本批按 Python 语义实现。**偏差（spec §5）**：transcript 模型解析收敛为 `modelSwitcher.GetLLM()` 单源（Go 已注入 config.Model，无现场再构造场景）；executor error-shaped result 当前无其它消费者，为对齐上游形状保留。测试：sections 3 例 + tools metadata 2 例 + tools/goal 4 例 + rails goal 4 例 + 集成 2 例（query 替换/状态转移、执行错误直接 BLOCKED） | `openjiuwen/harness/prompts/sections/goal.py` · `prompts/tools/goal.py` · `tools/goal.py` · `rails/task_completion_rail.py` |
| **9.x 子 Agent** | — | | | |
| 9.25 | ✅ | ResearchAgent | 研究子 Agent（BuildResearchAgentConfig + CreateResearchAgent，默认 SysOperationRail） | `openjiuwen/harness/subagents/` |
| 9.26 | ✅ | BrowserAgent | ✅ Layer1 配置层（env/config/progress/service）；✅ Layer2 运行时核心（runtime/browser_rail/runtime_tools/browser_agent_factory）；✅ Layer3 探测与控制（profiles/probes/controllers/agents）；⤵️ 9.38-49 Playwright MCP 端到端集成（6处占位：CodeExecutor/ManagedBrowserDriver/WorkerAgent/runTaskOnce/Probe占位/restart） | `openjiuwen/harness/subagents/` · `tools/browser_move/` |
| 9.27 | ✅ | CodeAgent | 编码子 Agent（BuildCodeAgentConfig + CreateCodeAgent + injectBuiltinPlanAgents + mergeRailsWithRequired + 4 必需 Rails 去重合并 + EnableTaskPlanning=true + RestrictToWorkDir=false） | `openjiuwen/harness/subagents/` |
| 9.28 | ✅ | PlanAgent | 规划子 Agent（✅ BuildPlanAgentConfig + CreatePlanAgent 工厂 + 双语提示词一比一复刻 + SysOperationRail(WithReadOnly) 双重约束 + RestrictToWorkDir=false + MaxIterations=25 + 不设 FactoryName 走通用路径 + 11 个单元测试；⤴️ 9.68-69 team.plan 特化已回填：TeamPlanModeRail + apply_team_plan_agent_prompt 逻辑移入 rail） | `openjiuwen/harness/subagents/` |
| 9.29 | ✅ | VerificationAgent | ✅ BuildVerificationAgentConfig + 双语提示词一比一复刻 + SysOperationRail() + VerificationRail() + MaxIterations=40 + 不设 FactoryName 走通用路径 + RestrictToWorkDir=false；⤴️ 9.19-23 回填 VerificationRail + VerificationContractRail + SubagentRail | `openjiuwen/harness/subagents/` · `harness/rails/subagent/` |
| 9.30 | ✅ | ExploreAgent | 探索子 Agent（✅ BuildExploreAgentConfig + CreateExploreAgent 工厂 + 双语提示词一比一复刻 + SysOperationRail(WithReadOnly(true)) 统一只读 + 不设 FactoryName 走通用路径 + RestrictToWorkDir=false + MaxIterations=50（已对齐上游 HEAD） + 11 个单元测试） | `openjiuwen/harness/subagents/` |
| 9.31 | ☐ | MobileGUIAgent | 移动端 GUI Agent | `openjiuwen/harness/subagents/` |
| **9.x 系统操作** | — | | | |
| 9.32 | ✅ | SysOperation 接口 | 系统操作抽象 | `openjiuwen/core/sys_operation/` |
| 9.33 | ✅ | LocalSysOperation | 本地执行 | `openjiuwen/core/sys_operation/` |
| 9.34 | ☐ | SandboxSysOperation | 沙箱执行 | `openjiuwen/core/sys_operation/` |
| 9.35 | ✅ | Shell Process Registry | Shell 进程管理 | `openjiuwen/core/sys_operation/` |
| 9.36 | ☐ | JiuwenBoxProvider | JiuwenBox 沙箱 Provider | `openjiuwen/extensions/sys_operation/` |
| 9.37 | ☐ | AioProvider | Aio 沙箱 Provider | `openjiuwen/extensions/sys_operation/` |
| **9.x 内置工具集** | — | | | |
| 9.38-49 | ⤵️ | Harness 工具集 | ✅Shell(BashTool+PowerShellTool)/✅文件系统(Read/Write/Edit/Grep/Glob/ListDir)/✅CodeTool/✅MCP(MCPTool+ListMcpResources+ReadMcpResource)/✅Cron/✅Skills(SkillTool+ListSkillTool)/✅TODO(todo_create+todo_list+todo_get+todo_modify)/✅AskUser/✅AgentMode(SwitchMode+EnterPlanMode+ExitPlanMode+TaskToolFactory)/⤵️BrowserMove 基座已完成（runtime+service+6runtime_tools+rail），Playwright MCP 端到端 15 处占位待回填（browser_move/service.go 10 处 / runtime.go 5 处，均标 TODO(#9.38-49)）/✅SubAgent(TaskTool+3Sessions工具)/✅ToolDiscovery(Search+Load)/Worktree(仅models)/Memory/✅多模态(ImageOCRTool+VQATool+AudioTranscriptionTool+AudioQATool+AudioMetadataTool+VideoUnderstandingTool(incl.VideoModelConfig+ThinkingEnabled+glm-4.6v默认+注册链路回填)+TranscribeAudio接口+ContentPart扩展+Vision/Audio注册回填+覆盖率85.3%)/✅WebTool(WebFreeSearchTool+WebPaidSearchTool+WebFetchWebpageTool+DDG/Bing/Jina/Bocha/Serper/Perplexity+goquery+多编码检测+5个⤵️回填)；⤴️ 9.89 已回填 Goal 两工具：submit_goal_report + get_current_goal（**注册时机订正**：goal_manager 绑定后**全程注册**，不是「仅 goal 模式激活期间」——上游 `init()` 注释明确两工具在 DeepAgent 全生命周期可用，普通轮 `get_current_goal` 用于模型丢失 `<goal_task>` 上下文后重定位；Uninit 反向清理） | `openjiuwen/harness/tools/` |
| 9.50 | ✅ | Workspace 管理 | ✅ WorkspaceNode 枚举（15 值）；✅ DirectoryNode 类型；✅ Workspace 结构体 + NewWorkspace/GetDirectory/SetDirectory/GetNodePath/GetDefaultDirectory 方法；✅ validateDirectoryNode 校验；✅ CN/EN 双语默认模式；✅ 默认目录自动补全；✅ 深拷贝隔离；✅ 27 个单元测试全部通过；✅ SkillToolkit 回填（Code 模式注册 + SkillNet 安装轮询） | `openjiuwen/harness/workspace/` |
| 9.51-53 | ✅ | Harness 资源/Schema/Prompts | ✅ 资源管理(BuiltinRules+ParseBuiltinRules+ParseBuiltinRulesFromFile+ResolveBuiltinRulesYAMLPath+mtime检测自动重载)/Schema 定义/提示词模板 | `openjiuwen/harness/resources/` · `schema/` · `prompts/` |
| **9.x CLI** | — | | | |
| 9.54 | ☐ | CLI REPL | 命令行交互界面 | `openjiuwen/harness/cli/` |
| **9.x TeamAgent 应用层** | — | | | |
| 9.55 | ✅ | TeamAgent | 生产级团队 Agent（✅ 回填 9.86：团队成员 `BuildModelAnomalyDetectionRail`，组装序列在 security 与 heartbeat 之间，与上游 `_COMMON_RAIL_NAMES` 一致；`RAIL_WHITELIST` 不收录该 rail，对齐上游；CoordinationKernel 完整生命周期 + KernelHost 细粒度窄接口 + 回调签名升级 + Invoke/Stream/Interact/Broadcast/HumanAgentSay + RecoverFromSession + FromSpawnPayload + AutoStart + DestroyTeam + BuildContextFromDB + FindRails + Runner.TeamRuntimeManager 接入 + 回填 9.59/9.60/9.61/9.67 + deep_adapter team 分流） | `openjiuwen/agent_teams/agent/team_agent.py` · `jiuwenswarm/server/runtime/agent_adapter/team_helpers.py` |
| 9.56 | ✅ | Blueprint | 团队蓝图定义 | `openjiuwen/agent_teams/agent/blueprint.py` |
| 9.57 | ✅ | AgentConfigurator | Agent 配置器；内层 DeepAgent 由 configurator / TeamHarness 持有（对齐 Python `_harness`，`TeamAgent` 不再单设 `deepAgent` 字段） | `openjiuwen/agent_teams/agent/` |
| 9.58 | ✅ | SpawnManager | 子进程管理（InProcessSpawnHandle+InProcessSpawn+SharedResources+SpawnManager+回调类型+BuildSpawnConfig） | `openjiuwen/agent_teams/spawn/` |
| 9.59 | ✅ | SessionManager + Interaction 层 | 会话三态管理 + Interaction 层（payload/router/UserInbox/HumanAgentInbox + runtime gate/pool/manager.interact）；覆盖率 interaction 92.8%、runtime 87.9% | `openjiuwen/agent_teams/agent/session_manager.py` · `agent_teams/context.py` · `agent_teams/interaction/` · `agent_teams/runtime/` |
| 9.60 | ✅ | StreamController | 流式控制器（结构体+构造函数+26方法+常量+回填team_agent/spawn_manager/inprocess_handle） | `openjiuwen/agent_teams/agent/stream_controller.py` |
| 9.61 | ✅ | RecoveryManager | 恢复管理（metadata子包8函数 + RecoveryManager 7方法 + LiveTeammate + SpawnedHandles访问器 + 回填session_manager/team_agent全部TODO(#9.61)） | `openjiuwen/agent_teams/agent/recovery_manager.py` |
| 9.62 | ✅ | CoordinationKernel | 协调内核（facade + Setup/Start/Pause/Stop 生命周期 + KernelHost 窄接口 + 便捷方法 EnqueueUserInput/WakeMailboxIfInterruptCleared） | `openjiuwen/agent_teams/` |
| 9.63 | ✅ | EventBus / Dispatcher | 事件总线与分发（EventBus 串行消费+轮询+生命周期 + EventDispatcher 粗筛+CallbackFramework 适配 + types 子包打破循环依赖 + 6 个 Handler 骨架 + 共享 staleClaimThrottle） | `openjiuwen/agent_teams/` |
| 9.64 | ✅ | Team Memory | 共享记忆（lite 薄接口+真实实现：conflict_types/internal/SharedMemoryManager/3种Allocator；回填 config/manager_params/manager/toolkit/extractor/allocator/resources/configurator/harness） | `openjiuwen/agent_teams/memory/` · `agentcore/memory/lite/` · `agent_teams/models/` |
| **9.x TeamBackend 子系统** | — | | | |
| 9.65a-1 | ✅ | TeamDB 基础层 | 数据模型+TeamDatabase接口+InMemoryDAO（TeamDao/MemberDao含FSM校验）+会话表生命周期；覆盖率92.6% | `openjiuwen/agent_teams/tools/database/` · `tools/models.py` |
| 9.65a-2 | ✅ | TaskDao + TaskManager | InMemoryTaskDao（18方法+依赖图变更管线+终止传播）+TeamTaskManager（20+方法+PLAN_MODE审批）；database 89.1% tools 85.2% | `openjiuwen/agent_teams/tools/database/task_dao.py` · `tools/task_manager.py` |
| 9.65a-3 | ✅ | MessageDao + MessageManager | InMemoryMessageDao（7方法+read_status watermark）+TeamMessageManager（7方法薄门面）+Messager接口+InProcessMessager | `openjiuwen/agent_teams/tools/database/message_dao.py` · `tools/message_manager.py` |
| 9.65a-4 | ✅ | TeamBackend 门面 | TeamBackend结构体+30+方法（spawn_member/startup/build_team/shutdown_member/clean_team/force_clean/HITT名册/inbound回调） | `openjiuwen/agent_teams/tools/team.py` |
| 9.65a-5 | ✅ | SQL 实现 | SQLite/PostgreSQL后端（SqlTeamDatabase+newGormDB+DDL动态表+4个SQL DAO+NewTeamDatabase工厂+WithTx跨表事务）；覆盖率86.0%；后续重构：①models.go补TableName()+gorm:column tag ②GetSessionIDFunc包级变量→sessionctx独立子包（对齐Python context.py） ③SQL DAO改用GORM结构体映射替换map[string]any ④spec补充字段映射表 | `openjiuwen/agent_teams/tools/database/engine.py` · `database/*.py` |
| 9.65-1 | ✅ | Messager 接口 + InProcess | Messager接口（8方法）+InProcessMessager+全局Bus+CreateMessager工厂+TaskManager事件发布回填（9处）；循环依赖重构完成：配置搬入schema打断schema→messager/memory循环链，Messager接口改回*schema.EventMessage，删除SenderIDStamper，tools包改用schema.TypedEvent，删除sessionID字段改用schema.GetSessionID(ctx)，MessageID改用UUID v4 | `openjiuwen/agent_teams/messager/messager.py` · `messager/inprocess.py` |
| 9.65-2 | ☐ | PyZmqMessager | PyZmq 后端实现 | `openjiuwen/agent_teams/messager/pyzmq_backend.py` |
| 9.66 | ✅ | Team Workspace | 团队工作空间（TeamWorkspaceManager + TeamWorkspaceRail + WorkspaceMetaTool + ToolTranslator + i18n + locales + 回填 agent/harness） | `openjiuwen/agent_teams/team_workspace/` |
| 9.66a | ✅ | WorktreeManager | Worktree 完整实现（manager + tools + rails + git + cleanup + session + backend + notice + slug；⤴️ 9.38-49 仅完成 models；⤴️ 回填 agent/resources.go WorktreeManager any 类型 + agent_configurator.go CreateWorktreeManager + 事件镜像回调 + code_adapter.go buildWorktreeRail） | `openjiuwen/harness/tools/worktree/` |
| 9.66a-1 | ☐ | Worktree 会话态单真源 | ⤴️ 9.87 审查发现：Go 侧 holder 是**双真源**（`WorktreeManager.sessionState` 与 ctx 传播并行），`enter_worktree`/`exit_worktree` 在 ctx 取不到时 fallback 到 manager 并**每次调用打 Warn**（上游 `tools.py` 只读 ContextVar、无 manager 兜底、无告警）。既定方案 A（不改 manager/rail）：工具入口 `EnsureSessionState(ctx, manager.SessionState())` 后只读 ctx，删除兜底分支与 Warn；方案 B（删 `sessionState` 字段，`Enter`/`Exit` 改为读写 ctx holder，严格对齐上游）需先在 BaseAgent/ReAct 入口统一挂 holder（`WorktreeRail.Init` 宿主类型是 `interfaces.BaseAgent`，可能是非 DeepAgent 的 ReactAgent），留待 B 阶段 | `internal/agentcore/harness/tools/worktree/tools.go` · `manager.go` ← `openjiuwen/harness/tools/worktree/tools.py`(L161/L284) · `manager.py`(L239) · `session.py`(L40/L81) |
| 9.67 | ✅ | Team Observability | OpenTelemetry 集成（Callback/Monitor/Rail 三层；OtelSpanState ctx 注入；UnregisterNamespace 扩展；DeepAgent SpanState 注入；AttachToTeamAgent/DetachFromTeamAgent 回填） | `openjiuwen/agent_teams/observability/` |
| 9.68-69 | ✅ | Team Rails / Prompts | 团队级 Rails / 提示词（9.69 Prompts：18+1 .md 模板 + loader.go go:embed + MtimeSectionCache + 8 Section Builder + RolePolicy + BuildSystemPrompt + team_plan_agent/mode；9.68 Rails：FirstIterationGate channel 替代 asyncio.Event + TeamToolRail P:90 工具注册 + TeamPolicyRail P:12 8 PromptSection 注入 + TeamToolApprovalRail 审批流 + TeamPlanModeRail P:84 plan 模式；14 ToolCard + CreateTeamTools + QualifyTeamToolIDs；回填 harness.go MountedRails any→具体类型 + resources.go + agent_configurator.go Rail 构造 + TODO 清理；⤵️ 25.0 将 info/members 的 mtime 全量重建车道换成 TeamContextTracker 增量宣告，静态 Section 与构造参数保留） | `openjiuwen/agent_teams/rails/` · `prompts/` |
| **9.x 自演化系统** | — | | | |
| 9.70a | ✅ | Operator 基础接口 | Operator 抽象接口（operator_id/get_tunables/get_state/load_state/set_parameter/apply_update）+ LLMCallOperator + ToolCallOperator + MemoryCallOperator + SkillExperienceOperator + evolving/schema 共享契约层（protocol.go/update.go） | `openjiuwen/core/operator/` |
| 9.70b | ✅ | Dataset + Constant | Case/EvaluatedCase/CaseLoader + TuneConstant 默认超参 | `openjiuwen/agent_evolving/dataset/` · `constant.py` |
| 9.70 | ✅ | Trainer | 训练器（离线演化编排：evaluate→update→writeback 循环 + 断点续训 + 早停 + 候选列表多方案评估）；✅ ResumeIfNeeded 已回填 progress + updater.LoadState；✅ Updater.Update 返回 []map[UpdateKey]any 切片 | `openjiuwen/agent_evolving/trainer/` |
| 9.70c | ✅ | Updater Protocol | Updater 协议接口（bind/requires_forward_data/update/process/get_state/load_state）+ SingleDimUpdater + MultiDimUpdater；✅ Update/Process 返回 []map[UpdateKey]any 切片对齐 Python Union[Dict, List[Dict]] | `openjiuwen/agent_evolving/updater/` |
| 9.71 | ✅ | BaseEvaluator | 评估器 + MetricEvaluator + DefaultEvaluator + metrics（ExactMatch/LLMAsJudge）+ evaluator_pipeline（⤵️ 仅占位） | `openjiuwen/agent_evolving/evaluator/` |
| 9.72a | ✅ | InstructionOptimizer | 指令优化器（LLM prompt 文本梯度优化） | `openjiuwen/agent_evolving/optimizer/llm_call/` |
| 9.72b | ✅ | ToolOptimizer | 工具描述优化器（beam_search/schema_extractor/customized_reviewer） | `openjiuwen/agent_evolving/optimizer/tool_call/` |
| 9.72c | ✅ | MemoryOptimizer | 记忆参数优化器（domain=memory + default_targets=[enabled,max_retries] + _backward=pass 声明式骨架）；✅ 回填 ToolOptimizerBase.Bind()（✅ 委托 BaseOptimizerMixin.Bind + DefaultTargets 回退） | `openjiuwen/agent_evolving/optimizer/memory_call/` |
| 9.72d | ✅ | SkillExperienceOptimizer | 技能经验优化器（LLM 生成经验草稿→EvolutionRecord）+ TeamSkillExperienceOptimizer；✅ TextualParameter.Gradients string→any 前置变更；⤴️ 9.79 orchestrator ✅（evolutionStore 注入）；⤴️ optimizer/doc.go ✅（EvolutionStoreReader 接口迁移至 checkpointing 包）；⤴️ Gradients any ✅（LoadFullEvolutionLog 签名改为 `(*EvolutionLog, error)` 对齐 Python 异常语义） | `openjiuwen/agent_evolving/optimizer/skill_call/` |
| 9.72e | ✅ | BaseOptimizer + LLMResilience | 优化器基类（backward/step 骨架）+ LLM 调用重试策略 | `openjiuwen/agent_evolving/optimizer/base.py` · `llm_resilience.py` |
| 9.73 | ✅ | SignalDetector | 信号检测（ConversationSignalDetector/from_evaluated_case/团队信号）；✅ matchFailureKeyword 改为 findFailureKeywordIndex；✅ MakeTeamTrajectorySignal excerpt 英文对齐 Python | `openjiuwen/agent_evolving/signal/` |
| 9.74-76 | ☐ | RL 子系统 | OfflineRL/OnlineRL/RewardRegistry | `openjiuwen/agent_evolving/agent_rl/` |
| 9.77 | ✅ | Trajectory | 轨迹类型 + Builder + Extractor + Aggregator + Store + Registry（TrajectorySink/Source + InMemoryTrajectoryRegistry + TeamTrajectoryAggregator + InMemoryTrajectoryStore + FileTrajectoryStore + MemberTrajectorySnapshot + StepKind 扩展） | `openjiuwen/agent_evolving/trajectory/` |
| 9.78 | ✅ | EvolveCheckpoint | CheckpointManager 协议 + DefaultCheckpointManager + EvolveCheckpoint 状态 + FileCheckpointStore + EvolutionStore（技能文件系统IO）+ StoreArchive/StoreProjection/StoreRecords/SkillPackage | `openjiuwen/agent_evolving/checkpointing/` |
| 9.79 | ✅ | Experience | 在线经验生命周期：OnlineEvolutionOrchestrator + ExperienceManager（stage/approve/reject）+ ExperienceScorer + ExperienceTracker + PendingChange/EvolutionContext/OnlineEvolutionResult | `openjiuwen/agent_evolving/experience/` |
| 9.80 | ✅ | UpdateExecution + Types | UpdateValue/ApplyResult/normalize_updates + execute_updates/apply_updates/summarize_apply_results + protocols 常量 | `openjiuwen/agent_evolving/update_execution.py` · `types.py` · `protocols.py` |
| 9.80a | ✅ | ExperienceSharing | 跨 Agent 经验共享（ExperienceSharer + HubClient + KeywordExtractor + ShareStager + LocalFileBackend） | `openjiuwen/agent_evolving/sharing/` |
| **9.x 扩展系统** | — | | | |
| 9.81 | ☐ | A2A 扩展 | A2AServer/A2AClient/A2ARemoteClient/A2AServerAdapter | `openjiuwen/extensions/a2a/` |
| 9.82 | ✅ | Context Evolver | P1(✅ 核心框架: RuntimeContext/ServiceContext/BaseOp/SequentialOp/ParallelOp + VectorNode Schema + MemoryVectorStore + JSONFileConnector + MemoryPersistenceHelper; 覆盖率≥85%) / P2(✅ IO Schema层: Trajectory/TrajectoryBatch + BaseMemory/MemoryInterface + ACE/RB/ReMe 三系 Memory+Request+Response + SummarizeResponse[T]/RetrieveResponse[T]泛型; 跳过Python死代码TaskMemory/PersonalMemory/vector_node_to_memory，跳过Ours类型; 覆盖率≥85%) / P3(✅ ReMe算法: core层改动(LLMService+EmbeddingService本地接口+VectorStoreService加GetAll+OpBase结构体) + retrieve/reme/(RecallMemoryOp+RerankMemoryOp+RewriteMemoryOp+prompt+utils) + summary/reme/(TrajectoryPreprocessOp+SuccessExtractionOp+FailureExtractionOp+ComparativeExtractionOp+ComparativeAllExtractionOp+MemoryValidationOp+MemoryDeduplicationOp+UpdateVectorStoreOp+PersistMemoryOp+prompt+utils); 覆盖率≥85%) / P4(✅ ReasoningBank算法: RBRecallMemoryOp + LabelDeterminator+MemoryItemParser+SummarizeMemoryOp+SummarizeMemoryParallelOp+UpdateVectorStoreOp+PersistMemoryOp + RB提示词 + MaTTS: ParallelScalingOp+SequentialScalingOp+BestOfNOp+SelfContrastMemoryOp) / P5(✅ ACE算法: ACERecallMemoryOp + Playbook/DeltaBatch/Bullet/DeltaOperation/OperationType + LoadPlaybookOp+ReflectOp+ParallelReflectOp+CurateOp+ParallelCurateOp+ApplyDeltaOp+PersistMemoryOp + ACE提示词(6个text/template)+SafeJSONLoads; 覆盖率≥85%) / P6(✅ 服务层+Agent集成: TaskMemoryService + OpenAILLMWrapper/OpenAIEmbeddingWrapper + TrajectoryGenerator + ContextEvolvingReActAgent; ✅审查修复: 自建config模块+MemoryItem接口+retrieve op存入[]MemoryItem+返回值struct替代map[string]any+字段改具体类型+config运行时读取+LoadMemories对齐dict格式+AddMemory持久化/校验/memoryID格式+AutoConfigure+diversity prompt+sequential截断+接口断言删除+APIKey兜底) / P7(✅ ContextEvolutionRail + MilvusConnector + MemoryPersistenceHelper auto模式 + 9.24 P6回填; 跳过工具层: Python无此实现); ⤴️9.24 P6 ✅ | `openjiuwen/extensions/context_evolver/` |
| 9.83 | ✅ | Pulsar Message Queue | Pulsar 后端（MessageQueuePulsar + PulsarSubscription + pulsarClientAPI/pulsarConsumerAPI/pulsarProducerAPI 三窄接口 + producer LRU + 消费循环；覆盖率 96.4%）；⤴️ 提前落位 9.84 的 `drunner/dmessage_queue/{message.go, message_serializer.go}`（Dmq 消息类型 + wire 序列化器，覆盖率 87.3%）；⤵️ `runner.go` 步骤3（启动分布式队列）/步骤1（停止分布式组件）两个预留点待 9.84 回填；⤵️ 偏差 D9（Python factory import 路径不一致）待 9.84 拍板 | `openjiuwen/extensions/message_queue/` |
| 9.84 | ☐ | DistRunner | 分布式 Runner；⤴️ 需回填 9.83 已落位的 `drunner/dmessage_queue/{message.go, message_serializer.go}`（不重写）；⤴️ 回填 `runner.go` 步骤3/步骤1 两个 ⤵️ 预留点；⤴️ 决策偏差 D9（factory import 路径 `extensions.runner.pulsar_mq` vs 实际 `extensions.message_queue`） | `openjiuwen/core/runner/drunner/` |
| 9.85 | ✅ | TeamRunner | 团队 Runner（run_agent_team/streaming 三路径 + Activate 内联回填 #9.62 + FinalizeMember + GetMonitor + ListActiveTeams + ⤵️ 回填） | `openjiuwen/core/runner/team_runner.py` |
| **9.x 权限体系 permissions** | — | | | |
| 9.90a | ✅ | permissions 基础类型层 | ✅ 七文件中五项已于 `337007d0` 落地（`tool_invocation_key.py` 94 行 → `permissions/tool_invocation_key.go`；`ask_user_contract.py` 4 行 → `rails/ask_user_contract.go`；`tool_permission_context.py` 40 行 → `swarm/schema/permission.go` 的 6 个 ctx 函数；`command_execution_context.py` 59 行 → `common/tools/command_execution_context.go`；`protected_paths.py` 26 行 → `permissions/protected_paths.go`），`bce0bd92` 修正 `resolveSandboxed` 的 mode 判定；✅ 剩余两项本批落地（`tool_binding.py` 25 行 → `permissions/auto_permission/tool_binding.go`；`models.py` 101 行 → `permissions/auto_permission/models.go`，含 3 个 RE2 正则与 1 个 frozenset）；⤵️ **4 个零消费常量待 9.90d 启用**（`ShellDisplayPathPattern` / `ShellDisplayPreSplitPathPattern` / `SensitiveDisplayPathPattern` / `DeterministicReadonlyPublicWebFetchSourceKinds`，按规则 9.2-4 照抄保留）；Spec: `docs/superpowers/specs/2026-11-07-permissions-models-tool-binding-design.md` | `.../rails/permissions/tool_invocation_key.py` · `tool_permission_context.py` · `protected_paths.py` · `tool_binding.py` · `_auto_permission/models.py` · `rails/ask_user_contract.py` · `common/tools/command_execution_context.py` |
| 9.90b | ✅ | permissions 决策事实层 | tool_capabilities(615) + tool_decision_facts(182) + network_scope(175) + policy_eval(375) + permissions_layers(676) + execution_provider_contract(37) + sandbox_profile(48) + native_path_context(65)；约 2,200 行 | `.../rails/permissions/` 同上目录<br>**完成摘要**：17 个 Task 全部落地。新增 `common/permission_tools`(105) + `security/fileguard`(1,750) + `security/netguard`(700) + `security/toolguard`(750) + 8 个 9.90b 文件(2,173)；改造 `permission_engine`(A/B/C 三段) + `tiered_policy`(A 段六项) + `patterns`(按轴落盘) + `tool_security_rail`(三层持久化)；删除 `checker.go`(303) + 测试(338)。<br>**执行中修正的计划外问题 4 项**：① `tools: [shell]` 类别写法 Go 不支持致 17 条内置规则全部失效；② Python 正则用 lookahead 而 Go RE2 不支持，降级路径造成误匹配（`ls \| grep foo` 判成 `rm -rf`），经用户决策引入 `regexp2` 统一处理；③ regexp2 的 `Match.Index` 是 **rune 索引**不是字节索引，直接切片会让含中文的路径错位；④ `storageLockMu` 不可重入，`UpdateUserToolLevel` 嵌套加锁自锁。<br>**依赖新增**：`github.com/gofrs/flock` v0.13.2（跨进程文件锁，用户决策）。<br>**⤴️ 遗留**：`policy_eval` 的 send_file 分支（待 9.90e）、`ComposeHostEffectivePermissions` 最小实现（待 9.90d 完整版） |
| 9.90c | ✅ | permissions root 五文件 + 中断扩字段 | ✅ root_context(528)/root_permission_queue(635)/root_permission_queue_rail(536)/root_context_rail(403)/root_ask_user(274) 五文件对齐 + `InterruptRequest` 扩 `Metadata`/`InterruptID`/`InterruptKind`/`InterruptOwner` + `InterruptKind` 七值 + 答案按 `interrupt_owner` 归属；✅ 8 项前置件（含 `CommandExecutionBinding`）；⤵️ **意图窗口消费方待 9.90d 回填**（`_intent_view` / `original_user_intent` / `has_valid_ask_user_clarification` 三处当前无调用方）；⤵️ `PermissionEngine.llm`/`modelName` 字段对齐 Python 结构保留，消费方为 9.90d；Spec: `docs/superpowers/specs/2026-11-07-root-permission-5-files-design.md` | 同左 |
| 9.90d | ☐ | permissions 自动审查层 | ⤴️ **回填 10.3.28f 的 R-4**：`getOrCreateSessionAdapter` 的"权限隔离未完成 → 拒绝复用"判据（`deep_adapter_sessions.go:198`，上游 :4199-4212，依赖 `_permission_state.permission_isolated`）；⤴️ **回填 9.90c 的意图窗口消费方**（`_intent_view` / `original_user_intent` / `has_valid_ask_user_clarification`）+ auto_reviewer(888) + reviewer_redaction(689) + auto_decision(639) + reviewer_metadata(401) + reviewer_execution(389) + permission_compose(362) + reviewer_route(330) + runtime_bridge(316) + readonly_tool_bindings(320) + auto_config(315) + runtime_result(250) + openjiuwen_contract(183) + domain_grant_audit(162) + lifecycle(165) + deterministic(131) + code_tool_bindings(110) + models(101) + manual_authorization(90) + deny_response(86) + reviewer_override_*(151) + auto_permission_rail(75) + reviewer_audit(77) + reviewer_stream_metadata(57) + domain_manual(43) + before_tool(891) + invocation_context(715) + artifact_*(3 文件)；约 6,000 行；⤴️ 同时接上 `PermissionEngine.llm` 消费 | `.../rails/permissions/` · `_auto_permission/` |
| 9.90e | ☐ | permissions 护栏与审计 | url_safety(541) + send_file_path_guard(315) + persistent_audit(296) + trusted_search_urls(236) + generated_artifact_delivery(175) + audit(65) + permission_interaction(54)；⤴️ 回填 `permissions_persist.py` 缺失 5 函数 + `PermissionApprovalInvalidated`；⤴️ 重构 `permissions_config_rpc.go`（Go 23 函数 vs Python 1 入口，层次下沉违反规则 11.1）；⤴️ 修正 `owner_scopes.go` 命名与缺失 3 符号；约 1,700 行 | 同上 |

**验证点**：✅ DeepAgent 可完成复杂编程任务（搜索→规划→编码→验证）

### 9-G 持续目标（Goal）实现批次（2026-11-05 制定，按规则 9 执行）

> 上游 Goal 能力分两层：agent-core `openjiuwen/harness/goal/`（域 1180 行：schema 319 / manager 410 / evaluation 336 / store 77 / `__init__` 38）+ 生命周期挂载（`rails/task_completion_rail.py` 约 400 行 goal 段）；jiuwenswarm 服务面在 `server/runtime/agent_adapter/interface_deep.py`（30 个 goal 方法，L15402-17960）。
> ~~Go 侧现状：goal 相关符号 0 个；交互监督器整层缺失；后端未注册任何 goal RPC，面板当前不可用。~~ **（2026-11-09 复核已过期，保留原始基线口径备查）**
> **当前实测（2026-11-09，本地 `go build ./...` + 单测 + goal 集成用例全绿）**：B1-B4 全部落地，goal 相关 Go 文件 30 个 / 3230 行；`command.goal` 已注册（`schema/req_method.go:45`）、`goal.snapshot` / `goal.updated` / `execution.error` / `goal.confirm_required` / `runtime.accepted` 五个出站事件均已定义并接进输出流（`deep_agent.go:4130` → `adapter/deep_adapter.go:1544/1594`）；`goal busy` 排队判定**已实现**（`runtime/service.go:263-291` + `service_goal_busy_test.go` 4 例），此前计划里"plan.go:283 仍为占位"的表述已过期；`TestSupplementInterruptSuite` 回归已随 `7d0b16be3` 合并修复。

| 批次 | 步骤 | 状态 | 内容 | 估算 |
|------|------|------|------|------|
| B1 | 9.87 | ✅ 2026-11-06 | 交互监督器基座（会话级单监督器 + 单输出租约 + 双队列调度 + 控制锁四临界区） | ~1.6k 行 |
| B2 | 9.88 | ✅ 2026-11-06 | Goal 域模型（可并行） | ~1.15k 行（实际 goal/ 五文件 + sections 四函数，42 用例） |
| B3 | 9.89 | ✅ 2026-11-06 | Goal 提示词/工具/生命周期接线 | ~1.5k 行 |
| B4 | 10.3.27 | ✅ 2026-11-09 | 服务面 `command.goal`（五 action + 三个守卫 + 轮次输出语义 + goal busy 互斥 + 跨会话 session-input 准入） | ~1.0-1.5k 行 |
| B5 | 27.2 + 27.10 | ✅ 2026-11-09 | 前端契约全量验收完成：`tests/integration/swarm/web/` 7 文件 / 1088 行 / 23 条 suite 测试方法（默认门禁内 20 条 + `integration&&llm` 冒烟 1 条）；G1 9 组契约 + G2 O1-O5 + G3 R1-R4 + G4 MockLLM 状态机 + G4 真 LLM 冒烟。审计 `[通过] 调用跨度 3 / 有效测试 20` | 实际 1088 行 |

- **依赖**：B1、B2 可并行 → B3 需 B1+B2 → B4 需 B3 → B5 需 B4。
- **Spec 拆分**（2026-11-05 决策）：S1=B1、S2=B2+B3、S3=B4+B5；每份 800-1500 行改动，各自一个 plan。**（2026-11-06 修订）**：S2 拆开 —— 9.88 单章出 spec 并顺带落 `prompts/sections/goal.go` 的 task-query 四函数，9.89 单独一份；理由：9.88 单独约 1.3k 行已接近单份上限，且 9.88 是 9.89 的硬依赖，提前可独立验收。
- **S1 必守设计约束**：①会话级单监督器，不得退化为「每次 Stream 开一条独立管道」；②单输出租约（`OutputLease`），else B1 的 revision/代沟检查全部失效；③goal 轮不续 user work（`_build_interaction_next_work` 对 kind=goal 直接返回 None）；④`GoalManager` 与 attach/detach/排队共用同一把 control lock；⑤复用现有 `setupTaskLoop`+`SubmitRound`+`WaitRoundCompletion`，不删除 `runTaskLoop`。
- **上下文代价（已在设计上承认）**：goal 每轮是一次完整 ReAct，工具结果全部进历史；缓解靠 context engine 会话级压缩 + `max_attempts`/`token_budget` 硬刹车 + `<goal_task>` 每轮重建权威上下文（不依赖历史推断目标）。
- **距离 Goal 完整使用的剩余工作（2026-11-09 盘点）**：

| # | 缺口 | 性质 | 章节 | 影响 |
|---|------|------|------|------|
| 1 | ✅ 已完成 | 前端契约全量验收 | 27.2 + 27.10 | 20 条默认门禁用例 + 1 条 `llm` 冒烟；`created_at`/`updated_at` 按 Python `schema.py:225-226` 保留（前端注释为文档滞后，属超集不冲突） |
| 2 | `9.66a-1` worktree 会话态单真源（现为双真源 + 每次调用 Warn） | 9.87 审查遗留，非 goal 专属 | 9.66a-1 ☐ | goal 轮进 worktree 时日志刷屏、状态可能不一致 |
| 3 | `22.3a` 观测 session 绑定（`set_current_session_id` 未移植） | 9.87 审查遗留 | 22.3a ☐ | goal 多轮 span 缺 `session.id`，轨迹面板难归因 |
| 4 | `15.6` native provider（消费 L1 交互面的另一入口） | 独立能力 | 15.6 ☐ | **不阻塞 Web goal**；仅影响第三方 harness 协议接入 |
| 5 | ✅ 已交付（口径订正） | MockLLM 全链状态机 + `integration && llm` 真 LLM 冒烟 | 27.10 | MockLLM 硬断言 continue 自动续跑 / query 每轮重建 / `max_attempts` 硬刹车到 BLOCKED / goal work 去重；真 LLM 冒烟按「只硬断言确定性契约」不锁模型是否跑到 COMPLETED（spec §7.2） |

> **结论（2026-11-09 更新）**：第 1、5 项已由 27.2 + 27.10 验收批次闭合，Go 侧 goal 功能本体（域 / 调度 / 生命周期 / 服务面）与前端契约**全部完成且本地验证通过**。剩余第 2（`9.66a-1`）、3（`22.3a`）、4（`15.6`）项为质量/扩展项，不阻塞主链路，此前已按规则 13.2 登记归属章节，非本批新增缺口。

### 9-H 权限体系 permissions 对齐批次（2026-11-07 制定，按规则 9/11/12 执行）

> 上游 `jiuwenswarm/agents/harness/common/rails/permissions/` 共 **38 个 .py / 11,463 行**；Go 侧 `internal/swarm/agents/harness/common/rails/permissions/` 现有 4 个实现文件约 1,450 行，**缺口约 10,000 行**。
> 核心事实：Go 的 `PermissionEngine` 是**纯规则判定**（分层策略 + 外部目录，全程零 LLM）；Python 在 `check_permission` 返回 ASK 之后还有一层 `auto_authorize`（确定性规则 → 意图硬规则 → LLM 审查）。**Go 侧完全没有这一层，导致规则说 ASK 就一律弹框问用户**。
> Spec：`docs/superpowers/specs/2026-11-07-root-permission-5-files-design.md`

| 批次 | 步骤 | 内容 | 前置 | 估算 |
|------|------|------|------|------|
| B1 | 9.90a | ✅ **基础类型层**：`tool_invocation_key.py`（94，ToolInvocationKeyV1 + normalize_tool_invocation_text）、`ask_user_contract.py`（4，MAX_STRUCTURED_QUESTIONS）、`tool_permission_context.py`（40，TOOL_PERMISSION_REQUEST_ID / SKILLS_REBUILD_SILENT）、`command_execution_context.py`（~100，CommandExecutionBinding）、`protected_paths.py`（26）、`tool_binding.py`（25）、`models.py`（101）—— 2026-11-07 完成 | — | ~400 行 |
| B2 | 9.90b ✅ | **决策事实层**：`tool_capabilities.py`（615）、`tool_decision_facts.py`（182）、`network_scope.py`（175）、`policy_eval.py`（375）、`permissions_layers.py`（676）、`execution_provider_contract.py`（37）、`sandbox_profile.py`（48）、`native_path_context.py`（65） | B1 | ~2,200 行 |
| B3 | 9.90c | ✅ **root 五文件 + 中断扩字段**（2026-11-07 完成）：`root_context.py`（528）、`root_permission_queue.py`（635）、`root_permission_queue_rail.py`（536）、`root_context_rail.py`（403）、`root_ask_user.py`（274）；同时扩 `InterruptRequest` 的 `Metadata`/`InterruptID`/`InterruptKind`/`InterruptOwner` + `InterruptKind` 七值 + 答案按 `interrupt_owner` 归属 | B1（中断扩字段独立可先行） | ~2,700 行 |
| B4 | 9.90d | **自动审查层（意图窗口的真实消费方）**：`auto_decision.py`（639）、`deterministic.py`（131）、`auto_reviewer.py`（888）、`reviewer_redaction.py`（689）、`reviewer_route.py`（330）、`reviewer_execution.py`（389）、`reviewer_metadata.py`（401）、`reviewer_audit.py`（77）、`reviewer_stream_metadata.py`（57）、`reviewer_override_gate.py`（67）、`reviewer_override_consume.py`（84）、`auto_permission_rail.py`（75）、`permission_compose.py`（362）、`_auto_permission/before_tool.py`（891）、`invocation_context.py`（715）、`runtime_bridge.py`（316）、`runtime_result.py`（250）、`deny_response.py`（86）、`lifecycle.py`（165）、`manual_authorization.py`（90）、`auto_config.py`（315）、`domain_grant_audit.py`（162）、`domain_manual.py`（43）、`readonly_tool_bindings.py`（320）、`code_tool_bindings.py`（110）、`artifact_*.py`（3 文件）、`openjiuwen_contract.py`（183） | B2 + B3 | ~6,000 行 |
| B5 | 9.90e | **护栏与审计**：`url_safety.py`（541）、`send_file_path_guard.py`（315）、`persistent_audit.py`（296）、`trusted_search_urls.py`（236）、`generated_artifact_delivery.py`（175）、`audit.py`（65）、`permission_interaction.py`（54）、`permissions_persist.py` 缺失 5 函数回填、`permissions_config_rpc.py` 层次重构、`owner_scopes.go` 命名与缺失符号修正 | B4 | ~1,700 行 |

**B3 已完成（2026-11-07）**：见下方 `9.90c` 行。

- **依赖链**：B1 → B2 → B3 → B4 → B5。B1 内的中断扩字段可独立先行（不依赖其它 B1 项）。
- **每批必守**（规则 11）：目录层级一文件对一文件；类型/方法归属与原实现一致；方法内步骤顺序、条件分支嵌套不得调整；命名只做 `snake_case → CamelCase`。
- **每批必查**（规则 12.4）：迁移前输出 Python/Go 文件清单与行数对照、旧逻辑清单、缺失项清单；缺失项必须提出讨论而非默默跳过。
- **已登记的现有偏差**（不属本批，B5 处理）：
  - `permissions_persist.go` 缺 `persist_exact_permission_allow_rule` / `_check_approval_accesses` / `_merge_file_guard_path_into_permissions` / `build_command_allow_pattern` / `_normalize_exact_accesses` 及 `PermissionApprovalInvalidated` 异常
  - `permissions_config_rpc.go` Go 侧 23 个函数 vs Python 仅 1 个入口 `dispatch_permissions_config_request` + 3 私有 → **疑似把 service 层逻辑下沉，违反规则 11.1 架构层次**
  - `owner_scopes.go` 缺 `setup_permission_context` / `cleanup_permission_context` / `current_permission_owner_scope`（Go 改用 `PermissionContextFromCtx`，命名被改，违反规则 11.1-4）；多出 `CheckAvatarPermission`

---

## 领域十：AgentServer + 独立交互入口

> 本领域的核心价值：所有外部入口统一经过 Gateway，Gateway 与 AgentServer 之间始终走 E2A 协议
>
> chat/serve/acp 作为"轻量 Channel"接入 Gateway，而非绕过 Gateway 直连 AgentServer

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **10.1 Schema 层** | — | | | |
| 10.1.1 | ✅ | ReqMethod 枚举 | ~100 个 RPC 方法名 | `jiuwenswarm/common/schema/message.py` (ReqMethod) |
| 10.1.2 | ✅ | EventType 枚举 | 事件类型 | `jiuwenswarm/common/schema/message.py` (EventType) |
| 10.1.3 | ✅ | Mode 枚举 | 运行模式（agent.plan/code.normal/team 等） | `jiuwenswarm/common/schema/message.py` (Mode) |
| 10.1.4 | ✅ | Message 模型 | 内部消息格式 | `jiuwenswarm/common/schema/message.py` (Message) |
| 10.1.5 | ✅ | AgentRequest / AgentResponse | Agent 请求与响应模型 | `jiuwenswarm/common/schema/agent.py` |
| 10.1.6 | ✅ | AgentResponseChunk | Agent 流式响应块 | `jiuwenswarm/common/schema/agent.py` |
| 10.1.7 | ✅ | HookEventBase | 钩子事件基类 | `jiuwenswarm/common/schema/event_base.py` |
| 10.1.8 | ✅ | PermissionContext | 权限上下文 | `jiuwenswarm/common/schema/agent.py` |
| **10.2 E2A 协议** | — | | | |
| 10.2.1 | ✅ | E2AEnvelope | 请求信封 | `jiuwenswarm/common/e2a/models.py` (E2AEnvelope) |
| 10.2.2 | ✅ | E2AResponse | 响应模型 | `jiuwenswarm/common/e2a/models.py` (E2AResponse) |
| 10.2.3 | ✅ | E2AProvenance / E2AAuth | 来源标识与认证 | `jiuwenswarm/common/e2a/models.py` |
| 10.2.4 | ✅ | E2AFileRef / IdentityOrigin | 文件引用与身份来源 | `jiuwenswarm/common/e2a/models.py` |
| 10.2.5 | ✅ | Wire Codec | E2A ↔ AgentResponse/AgentResponseChunk 编解码 | `jiuwenswarm/common/e2a/wire_codec.py` |
| 10.2.6 | ✅ | E2A Constants | 协议常量 | `jiuwenswarm/common/e2a/constants.py` |
| 10.2.7 | ✅ | gateway_normalize | Message/E2A/AgentResponse 格式互转 | `jiuwenswarm/common/e2a/gateway_normalize.py` |
| 10.2.8 | ✅ | agent_compat | `e2a_to_agent_request()` 转换 | `jiuwenswarm/common/e2a/agent_compat.py` |
| 10.2.9 | ✅ | ACP 适配器 | 与 10.2.10 合并交付（Python 同文件 adapters.py）；6 函数；keyword-only → Options struct；converter 串 Go 化（gateway_normalize 惯例） | `jiuwenswarm/common/e2a/adapters.py` |
| 10.2.10 | ✅ | A2A 适配器 | 随 10.2.9 合并交付（A2A 入站 SendMessage/出站 stream payload；envelope_to_call 共用） | `jiuwenswarm/common/e2a/adapters.py` |
| **10.3 AgentServer 核心** | — | | | |
| 10.3.1 | ✅ | AgentWebSocketServer | WS 服务端，~56 个 RPC 方法分发（对齐 Python，JiuWenClaw/AgentManager 已 stub） | `jiuwenswarm/server/agent_ws_server.py` |
| 10.3.2 | ✅ | JiuWenClaw 门面 | SDK 路由，会话队列，流式包装，中断处理；✅ 云端记忆钩子(4处)；✅ Team模式判断+BuildInputs三路径+rawQuery替换；✅ Auto-Harness resume绕过；✅ Team中断分流+cancelTeamWork骨架；✅ Dreaming生命周期(CreateInstance/ReloadAgentConfig)；✅ CreateInstance Functional Options+SetSkillnetInstallCompleteHook；⤵️ Team后续请求绕过/TeamManager调用(10.6.19-23) | `jiuwenswarm/server/runtime/agent_adapter/interface.py` |
| 10.3.3 | ✅ | AgentAdapter 接口与工厂 | AgentAdapter ABC，`create_adapter()` | `jiuwenswarm/server/runtime/agent_adapter/agent_adapters.py` |
| 10.3.4-6 | ✅ | 模式适配器 | Agent/Code/Deep 模式适配器；✅ 回填 9.86：`_build_model_anomaly_detection_rail` 对应 `buildModelAnomalyDetectionRail`（读 `execution_guard`，security 后 heartbeat 前挂载，开关与 builder 成对传 `*bool`）；已回填 setCheckpoint/config/dotenv/Model/buildModel/HandleHeartbeat/query注入/模型校验/sessionActive/markSessionActive；✅ 已回填 seedRuntimeCwd；✅ SetLLM统一模型切换(删SwitchModel)；✅ CompressContext+GenerateRecap直接实现(ContextEngine in-memory)；✅ ToolSync5方法直接实现；✅ SlashCommand/evolve分发骨架；✅ subagents骨架(9.25-29)；✅ sysop_builder骨架；✅ 已回填 SubagentRail/ConfirmInterruptRail/Workspace.SetDirectory/ClearMemoryManagerCache(10.6.15后指向swarm包commmem：ClearConfigCache+ClearMemoryManagerCache双清)；⤵️ 标记已精确修正指向（agentcore.DeepAgent/10.6.3-10 Swarm Rails/A2X/11.10 等） | `jiuwenswarm/server/runtime/agent_adapter/interface.py` · `interface_code.py` · `interface_deep.py` |
| 10.3.7-11 | 🔄 | 适配器辅助 | CodeAgentRail✅/TeamHelpers🔄(辅助函数✅/主流程⤵️9.85)/EvolutionHelpers✅/RecapPrompts✅(对齐Python审查修复3处：多模态content提取/callModelForRecap用[]BaseMessage/countFullContextTokens补工具定义token+fallback)/SysOpBuilder✅(一比一对齐Python:BuildFilesystemPolicy+CreateSandboxSysOpCard+CreateLocalSysOpCard+展示辅助+实例化注册)/handleCommandRecap✅+Compact✅(压缩后重取context+summary字段对齐Python)+Context✅连接adapter/DeepAdapter.CreateInstance步骤17✅；✅ 回填审查修复：⤵️标记修正3处(10.3.7-11→9.55-9.65/10.6.3-10)/modelCache类型断言获取/CodeAgentRail热重载/ListAvailableTools动态构建 | `jiuwenswarm/server/runtime/agent_adapter/` |
| 10.3.12 | ✅ | AgentManager | 多实例管理（按通道/模式），含 agentFactory 注入 | `jiuwenswarm/server/runtime/agent_manager.py` |
| 10.3.13 | ✅ | AgentConfigService | Agent 配置 CRUD | `jiuwenswarm/server/runtime/agent_config_service.py` |
| 10.3.14 | ✅ | TenantAgentPool | 多租户 Agent 池化；✅ Singleton[T] 扩展 Reset（自动 Cleanup）；✅ GetInstance/ResetInstance/ProcessMessage/ProcessMessageStream/Cleanup 透传；对齐 Python tenant_agent_pool.py | `jiuwenswarm/server/runtime/tenant_agent_pool.py` |
| 10.3.15-18 | ✅ | 会话管理 | SessionHistory(JSONL)✅ / SessionMetadata✅ / SessionRename✅；✅ 子包提取(server/session/)消除跨包依赖；✅ AppendHistoryRecord 元数据联动(UpdateSessionMetadata+SetSessionDeliveryContext)；✅ ReadTeamHistoryRecords+IsTeamRelevant(team过滤+重试)；✅ TruncateHistoryRecords cutIndex对齐Python；✅ SerializeValue递归序列化；✅ AutoTitle已实现；✅ RemoveTeamModeSessionDirsAtStartup；✅ GetAllSessionsMetadata分页 | `jiuwenswarm/server/runtime/session/`；⤴️ **会话删除路径已由 10.3.28c 接管**；`SessionManager` 曾于 10.3.28d 被删除，2026-11-09 已恢复（`runtime/session_manager.go`）——它与 Runtime 协调器是**互斥**关系（`service.py:1873` 按 `uses_session_runtime` 二选一），不是冗余 |
| 10.3.19-20 | ✅ | 技能管理 | SkillManager(Server)/SkillDev 管道/SkillToolkit(search_skill+install_skill+uninstall_skill)/DeepAdapter步骤9回填；✅ HandlePluginsEnable/Disable 改用 setPluginEnabled 操作 installed_plugins 对齐 Python；✅ HandlePluginsReload 增加物理目录移动逻辑对齐 Python；✅ isHTTPDownloadTarget 对齐 Python 只检查 scheme+host；✅ matchHost 移除 host==suffix 分支对齐 Python 段数匹配；✅ skillnetNetworkContext 补全小写代理变量和 NO_PROXY 默认值注入对齐 Python；实现偏差：①locateSkillDir递归SKILL.md搜索（对齐Python _locate_skill_dir）②7个方法导出（跨包访问需要大写）③CLAWHUB_BASE_URL环境变量（测试可覆盖性）④SkillNet暂stub仅ClawHub+TeamSkillsHub | `jiuwenswarm/server/runtime/skill/` · `swarm/agents/harness/tools/` |
| 10.3.21-22 | ✅ | GatewayPush | Transport/Wire 服务端推送 | `jiuwenswarm/server/gateway_push/` |
| 10.3.23.1 | ✅ | HookType/HookEvent 常量 | AgentRailEvents/GatewayEvents | `hooks_config.py` HookType/HookEvent |
| 10.3.23.2 | ✅ | 配置模型 | CommandHookConfig/PromptHookConfig/HookMatcher/HooksConfig/LoadHooksConfig | `hooks_config.py` 配置模型 |
| 10.3.23.3 | ✅ | HookExecutor | command/prompt 两类 hook + ParseCommandOutput + ExtractJSONFromResponse；11.13 起对应物迁至 internal/common/hooks/executor.go 供 Gateway/AgentServer 共享 | `executor.py` |
| 10.3.23.4 | ✅ | UserHookRail | embed DeepAgentRail + BeforeToolCall/AfterToolCall/OnToolException/AfterInvoke | `user_hook_rail.py` |
| 10.3.23.5 | ✅ | 回填注册 | DeepAdapter 注册 UserHookRail + handleHooksList 真实实现 | `interface_deep.py` 注册 |
| 10.3.24 | ☐ | Sandbox | 延后；⤴️ /sandbox 命令面来源 10.4.4b | `sandbox/` |
| 10.3.25 | ✅ | Utils | 完成 | `utils/` + `interrupt/helpers` + `filesystem/history` + `shell/rm_tracker` + `server/utils/diff_service` |
| 10.3.26 | ☐ | 入口 | 延后 | `app_agentserver.py` |
| 10.3.27 | ✅ | Goal 控制面 | **已完成（2026-11-09 复核）**：S1 协议与分发 + S2 控制面 + S3 接线共 12+ 提交已合并（`7d0b16be3` 及后续）；`command.goal` 误路由已消除（不再写空 query user 历史、不再调模型），五 action / 斜杠入口 / 三守卫 / `AgentManager.HasActiveGoal` 探针 / `start_interaction` / chat 主链路改 `attach_output`+`send_input`（含 `OutputHandoff`）全部落地，adapter 内 `runner.RunAgent*` 调用点已删净；✅ **原「剩余」两项已闭合**：goal busy 排队判定见 `runtime/service.go:263-291`（`sessionMessageGoalBusy` + `cross-session messages must queue...` 拦截 + 4 例单测），`TestSupplementInterruptSuite` 回归已随 `7d0b16be3` 修复；✅ 集成全量收尾：`goal_dispatch_test.go` / `goal_control_plane_test.go` / `goal_interaction_test.go` / `goal_lifecycle_test.go` / `goal_round_abort_guard_test.go` 共 15 例全绿（实测 `-run Goal` 展开套件后：swarm/server 3 + server/adapter 6 + agentcore/harness 6；5 个文件的测试函数合计 19 个）；📄 续做指引（含两处执行期计划修正与八个已踩坑）见 `docs/superpowers/plans/2026-11-09-10.3.27-goal-control-plane.md` 首节；⤴️ **回填 10.3.28f 的 S-1**：`DispatchGoalControl` 骨架（本批只落分流头，方法体归本节）；`command.goal` 分发（action=set/pause/resume/clear/get；get 为只读，不得兜底新建 session metadata）+ 结构化入口 `handle_goal_command_structured` 与斜杠入口 `_parse_goal_slash_intent` + set/resume 的 attach→control 两段映射 + attempt 中间 `chat.final` 降级为 `goal_intermediate`（避免被当作正式回答）+ goal objective / completed 历史落盘（忙碌插队时 defer 到轮边界）+ 与 plan 模式互斥 + `has_active_goal_interaction`/`has_active_goal` 守卫；⤴️ 依赖 9.87-9.89 | `jiuwenswarm/server/runtime/agent_adapter/interface_deep.py` · `jiuwenswarm/server/agent_ws_server.py` |
| 10.3.28a | ✅ | 契约与租约 + 数据结构层 | `runtime/session/` 包：`model.go`(542，19 类 + `terminalSignal` 双信号)、`execution_registry.go`(398，14 方法 + 9 个原子访问器)、`session_lifecycle.go`(283，14 类型)、`session_delete.go`(246，5 类型 + 2 函数)；**核心概念**：`Terminal`（`MarkTerminal` 唯一 close 点，业务终态）与 `Done`（执行方 defer close，收尾完成）是两个独立信号，不是两个状态——清场等 `Done`、收结果等 `Terminal`；覆盖率 88.4%、52 用例；⤵️ **R-5**：`SessionOps` 的 `CopySessionContext`/`CopySessionState`/`SetSessionModelSelection` 为无操作空实现（依赖 `DeepAgent` 实例与 recommendation 包），回填 10.3.30 | `internal/swarm/runtime/session/` ← `openjiuwen` 无对应；`jiuwenswarm/runtime/session/{model,execution_registry}.py` · `jiuwenswarm/runtime/{session_lifecycle,session_delete}.py` |
| 10.3.28b | ✅ | create / switch / fork 两阶段事务 | `session_provisioner.go` 前半：`PreparedSessionProvision` 五态状态机 + 13 条非法转换；create 21 步（TUI 显式 ID 外锁、metadata 复用 CONFLICT、work_mode 37 空格文案、预热 6 判据、失败补偿释放认领）；switch 8 步（stored mode 优先、团队归属准备、前台切换 commit hook）；fork 8 步（cutoff 四字段、force_history 分支、ValueError→NOT_FOUND/ALREADY_EXISTS）；5 个可注入接口 + 6 个 provider 函数字段（spec D5/D6） | `internal/swarm/runtime/session_provisioner.go` ← `jiuwenswarm/runtime/session_provisioner.py`(L113-1385) |
| 10.3.28c | ✅ | delete 七阶段事务与团队删除 | `DeleteSession`：participant.before → trajectory.begin → lifecycle.begin → quiesce → release → dispose → rmtree/commit，`destructive_started` 为回滚唯一分界；三态结果（成功 / `DELETE_FAILED` / `DELETE_COMMIT_PENDING`）+ 60 秒有界重试缓存 + session 级锁；`DeleteTeam`（整体 quiesce → 逐会话删除 → 团队 Runner 一次）；`team_session_lifecycle.go`(197) 6 方法适配器（4 桩登记 R-7）；**quiesce 真实能力来自 10.3.28d 协调器，dispose 三依赖为桩**；✅ **2026-11-09 回填**：dispose 三步已真实编排（`PromptSuggestionForks.SessionDeleted` / `SubagentRuntimeStopper.StopSubagentRuntimes` / `SessionRuntimeCleaner.CleanupSession`，单条失败只告警）；R-3 已回填（`AgentManager.CleanupSessionRuntime` + 门面双侧清理）；团队控制面已装配（`teamExecutionControllerAdapter`）、`handleTeamDelete` 走真实事务、`ListTeamSessions` 扫描活动区+归档区、`deleteTeamLocked` 补 `mutation_lock`；会话 ID 白名单已接入（`internal/common/protocol_ids.go`）；⤵️ 回填点：**R-1** `PromptSuggestionForks.session_deleted`（`noopPromptSuggestionForks` 桩）→ 10.3.30；**R-2** `stop_subagent_runtimes`（桩）→ 25.8；**R-7** `TeamManager` 4 方法（`SetRoundStateReporter`/`RemoveWaiter`/`BroadcastEvent`/`IsGenerationRunning`）为桩 → 25.2；**R-8** 轨迹库删除钩子 `BeginSessionDelete`/`AbortSessionDelete`/`CommitSessionDelete`（`noopTrajectoryHook`）→ 领域二十二 | `internal/swarm/runtime/{session_provisioner,team_session_lifecycle}.go` ← `jiuwenswarm/runtime/session_provisioner.py`(L1444-2133) · `team_session_lifecycle.py` |
| 10.3.28d | ✅ | 会话执行协调器 | `runtime/session/work_scheduler.go`(478，`sync.Cond` 阻塞唤醒 + `container/heap` 三级优先队列)；`coordinator.go`(约 1400，26 公开 + 26 私有方法)：`RunUnary`/`RunStream`(双 goroutine + 有界 channel + **终态标记阻塞写入**) /`RunTeamStream`/`CancelExecution`(fresh/pending 两批)/`CloseSession`(十步清场，**等 `Done` 不等 `Terminal`**)/`QuiesceSession`/`DeliverControl`(在飞投递 + `AnswerInFlightError`)；**删除 `server/session/session_manager.go`(430 行)**，Runtime 主流程 8 处改走协调器，一个会话队列收敛到 Runtime 层；两级排队（latest_first 类 LIFO、其余 FIFO）替代旧一律 LIFO，行为变化由回归用例固化（spec §8 D-5）；🎯 修正 `supersedeSiblings` 锁外读 `handle.State` 的真实竞态（`-race` 实测）与 `evictTerminal` 缺 `Done` 守卫的回收缺陷；✅ **2026-11-09 回填**：恢复 `SessionManager`（`runtime/session_manager.go`，17 方法，同会话串行 LIFO + 一次性会话哨兵）；门面 `ProcessMessage`/`ExecuteMessage` 双入口分流；`StreamSessionInput`（8 步，4 分支）；会话级 adapter 分层（`deep_adapter_sessions.go`）；`UsesSessionRuntime` 归位上游语义（team 为真），删除语义相反的旧实现；⚠️ 修正旧 spec 断言「上游无 `session_manager.py`」——实际 402 行，且两层互斥非串联（`service.py:1873`）；✅ 2026-11-09 补：`_cancel_result_future`/`_track_closing_task`/`_finish_closing_task` 三个方法曾被误合并为 `cleanupProcessorGeneration` 且语义未实现（导致 `closingTasks` 只读不写、「同 session_id 可在旧 processor 退出前重连」失效），已按上游同名补齐并把 `CloseSession` 重写为六步；✅ **D-17 已由 10.3.28f 回填**：`sessionAdapterCache.entries` 改为 `map[string]AgentAdapter`，生产写入方为 `getOrCreateSessionAdapter`（根分流时 `remember`），子代理运行时部分仍待 25.8；⤵️ 回填点：**R-4** `_forget_agent_execution_owner`（无操作）→ **20.1**（2026-11-10 更正：该表的读写两端都在声明式 root-agent 接入链路，`invoke_agent`/`RuntimeAgentDefinition`/`_agent_execution` 参数 Go 侧 0 命中，28d 内只能建出无读写方的死结构，故不落地；依赖清单见 spec §10 R-4 说明）；**R-6** 团队执行控制面生产调用方（`SessionRuntimeLifecyclePort` 的 `PauseSession`/`StopSession` 有实现无消费者）→ 25.2 | `internal/swarm/runtime/session/{work_scheduler,coordinator}.go` · `internal/swarm/server/runtime/interface.go` ← `jiuwenswarm/runtime/session/{work_scheduler,coordinator}.py` |
| 10.3.28e | ✅ | 异步删除与归档（lifecycle 持久化） | `runtime/session/lifecycle/` 39 函数：`paths.go`(ID 白名单含 Windows 保留设备名 / `SessionPaths` 符号链接与双存冲突防御 / `ResolveSession` / `RawMetadata`)、`lock.go`(`AtomicJSON` O_EXCL+fsync+rename / `ResourceLock` 按协程可重入 / 共享冲突重试 100 次 / `FenceWrites` project 级联)、`operation.go`(状态机 14 函数，**双写顺序不可颠倒**：先 `operations/<op_id>.json` 再 `resources/`)、`guard.go`(`Guard`/`WriteGuard`/`AssertRuntimeOwner`/`ClaimRuntime`/`ReleaseRuntime`)、`query.go`(`ParseIDs`/`Page` 过滤排序分页 / `Projection` / `EventSnapshots`)；`server/session/archive/` 两段式：接受阶段快速返回（`accepted`/`finalizing`/`stop_pending` 三字段完成契约）+ 后台 finalizer（有界重试 5 次 / 退避 5-15-60-300s / 并行 4）+ 崩溃恢复循环；`session.delete` 从同步阻塞改为「接受即返回 + 后台收尾 + 可恢复」；归档三方法（archive/unarchive/archived.list）；⚠️ 依赖缺口以窄接口预留：`Mailbox`(25.6) / `HeartbeatStopper` / `SubagentStopper` / `SessionStopper` / `RunningProbe` / `GenerationProbe` / `WriterFlusher` / `ProjectRestorer`；✅ 已补齐 `_listable_archived_directory`（符号链接/junction 逃逸防护）/ `_backfill_archive_times`（启动回填）/ `_listing_archived_at`（只读三级回退），并让 `ListSessions` 用上三者；⚠️ 40 方法中 31 个已落，**6 个待回填**（均依赖项目存储或 cron 清单）：`project_session_inventory` / `project_sessions` / `cron_sessions` / `_cron_session_entries` / `delete_cron_sessions` / `project_batch`；⤵️ 回填点：**D-16** 项目存储 `project_store.py`（实际 1213 行，spec 原记 394 行有误）整层缺失，`lifecycle.ProjectSource` 已留注入点，未注入时 `ProjectIDFor` 退化为 `default`/`default_code` → 待项目体系章节；**D-17** ✅ 已由 10.3.28f 回填（`entries` 改 `map[string]AgentAdapter` + 生产写入方 `getOrCreateSessionAdapter`）；**lifecycle.ProjectAdmissionSnapshot**（上游 :490，定时任务准入快照）未实现 → 随 D-16 一并 | `internal/swarm/runtime/session/lifecycle/` · `internal/swarm/server/session/archive/` ← `jiuwenswarm/server/runtime/session/{lifecycle.py(805), session_archive.py(1744)}` |
| 10.3.28f | ✅ | 会话级 adapter 分身（根 router + 会话 child） | 对齐 `interface_deep.py` 双形态单类：根只做 router/模板持有，每会话由 `sessionChildFactory` 造 child 独享 DeepAgent（自己的 rail 树/工具表/context_engine）。**接口扩 5 方法**（`IsSessionScoped`/`MarkAsSessionScoped`/`GetLiveSessionInstance`/`HandleSwarmflowReply`/`PendingInteractions`）；**child 工厂**必须 `CreateAdapter("harness", mode)` 新造完整 `CodeAdapter`（从 `c.deep` 提取会丢 20+ 覆写、rails 静默降级）；**缓存与生命周期** 8 方法（`getOrCreate` 锁+waiters 计数、`CleanupSessionAdapter` 守卫链、TTL 2h/每批 3 的 `evictIdle`、`HasSessionRuntime` 四源判定）；**10 个分流头**（6 服务方法 + 4 骨架方法），`ProcessInterrupt` 按 cancel/supplement vs pause/resume 双分支；流式用转接 goroutine 代替 defer；**根 instance 惰建**（`skipOwnInstanceBuild` + `EnsureInstance` 双检，CreateInstance 在 config 段后早退）；**过程中修掉 3 个真缺陷**：`planStateStore` 用根 instance 致 plan 状态读写全废（模式切换/工具拦截静默失效，改为 `GetLiveSessionInstance` 对齐 `plan.py:85-140`）、`ReloadAgentConfig` 根分支被简化成空返回致热重载对 live 会话无效（补 `applyReloadConfigSnapshot` + `fanOutReloadToSessionAdapters`，对齐 `:13623-13631`）、`GetContextUsage` 缺 `context==nil` 降级（对齐 `:21953-21961`）；**测试** 单测净增 32 例（`deep_adapter_sessions_test.go` 8→40）+ 集成 9 例（含入口级 code 提醒用例，走真 AgentManager 全链路）；⤵️ 回填点：**R-1~R-7** 七组 child 重建判据（cron 配方/external harness/MoA 指纹/权限隔离/tool_retrieval 开关/首次 MCP reconcile/模型选择变更，均留位上流行号）；**D-1** `_start_mcp_prewarm` → R-6；**D-2** `session_has_live_subagent_runtime` 恒 false → 25.8；**D-4** child 继承七项中只落 `skillManager`（其余 6 项 Go 无对应字段）；**D-8** `drop` 的 runtime_state 文件删除（Go 是单文件、不分 session）；**D-9** 心跳保留守卫；4 个骨架方法体归各自章节；**回填映射**（15 条，详见 spec §11.1）：R-4→9.90d、R-6+D-1→10.6.11、D-2→25.8、S-1→10.3.27；R-5→9.11a（开关机制已交付，本批只缺"翻转时重建 child"的判据，可直接在 9.11a 收尾时补）；R-1/R-2/R-3/R-7 与 S-2/S-3/S-4 在计划中**尚无对应章节**（cron 执行上下文、external harness、MoA、模型路由、上下文用量、btw 旁询、团队域），这些回填点保留在代码 `⤵️` 注释与 spec §11.1 表中，待相应章节立项时按编号检索接入 | `internal/swarm/server/adapter/deep_adapter_sessions.go` · `interface.go` · `deep_adapter.go` · `code_adapter.go` · `server/runtime/interface.go` ← `jiuwenswarm/server/runtime/agent_adapter/interface_deep.py`(会话分身段) · `agent_adapters.py` |
| 10.3.28 | — | **实施说明** | 实施顺序 **a → d → b → c**（c 的 quiesce/dispose 落在 d 上，b 的 prepare_create 需要 d 产生 generation）；**删除清单 8 项**（4 个 handler 旧实现 + `session_manager.go` + 测试 + doc.go 条目 + 旧用例）；**测试 137+52 单测 + 4 集成**；**全量门禁**：build/vet/gofmt 零问题、swarm+agentcore+common+cmd 全绿、`go test -race -count=3` 无竞态、`integration-audit -verify` 通过、`session_provisioner.go` 覆盖率 91.0%；📄 spec `docs/superpowers/specs/2026-11-07-10.3.28-session-provisioner-coordinator-design.md` · 计划 `docs/superpowers/plans/2026-11-07-10.3.28-session-provisioner-coordinator.md` | — |
| 10.3.30 | ☐ | 主动推荐系统（会话删除观察者） | ⤴️ **回填 10.3.28a 的 R-5 / 10.3.28c 的 R-1**：`SessionOps` 的上下文/state 复制空实现与 `PromptSuggestionForks` 桩；承载 10.3.28 删除事务对 `prompt_suggestion` 的依赖：`PromptSuggestionForks.session_deleted`（清 turn/answered/generation 三张表 + 取消在飞 task）、`cancel_suggestion`、`shutdown`；⤴️ 依赖 10.3.28c 的 `SessionDeleteParticipant.BeforeDelete/ReleaseResources` 契约；**本节只是该领域的第一块**——完整 recommendation 包在上游有 34 文件 / 43821 行（proactive_engine / topic_pipeline / topic_product / topic_history / topic_outcomes 等），Go 侧目前 0 实现，整包迁移需单独排期 | `internal/agentcore/harness/common/recommendation/` ← `jiuwenswarm/agents/harness/common/recommendation/prompt_suggestion.py`(751) |
| **10.4 独立交互入口** | — | | | |
| 10.4.1 | ☐ | 🔥 CLI 聊天模式 | 内置 REPL 交互，直接连接 AgentServer，流式输出 | `jiuwenswarm/channels/acp/app_acp.py` (参考) · 新实现 |
| 10.4.2 | ☐ | 🔥 HTTP API | RESTful + SSE 流式 | 新实现 |
| 10.4.3 | ✅ | ACP Stdio | 标准输入输出 JSON-RPC 协议；✅ AcpStdioClient 已随 10.6.24d 实现于 swarm/acp（读写分离 goroutine+exitCh 免锁探测+三段优雅关闭+平台拆分，覆盖率 87.0%）；cli.py 一次性冒烟入口已随 10.4.6 落地（`uapclaw acp-chat`，非 REPL） | `jiuwenswarm/acp/cli.py` · `stdio_client.py` |
| 10.4.4 | ✅ | Slash 命令处理 | `/mode`/`/new`/`/sandbox`/`/model` 等；/evolve 系列分发骨架已在 adapter 实现；已拆分为 10.4.4a（已完成）/10.4.4b（⤵️ 10.3.24） | `jiuwenswarm/gateway/message_handler/command_parser/slash_command.py` |
| 10.4.4a | ✅ | Slash 命令处理（对账收口 + /model） | 8 项对账（A1/A2/A5/A6/A7/A8 改代码，A3/A4 核实等价）：handleChannelControl 顺序与日志点位（白名单→text→解析→info→SKILLS 提前返回不建 state）、/branch 取消改同步并补成功/拒绝日志、/rewind 删 preview 兜底并补两条成功日志+rewind_context、sendChannelNotice 补 params={}；command.model 三分支全量实现（API_KEY 掩码 + os.environ 非字符串报错复刻 TypeError）；/evolve 5 个命令已完整实现（deep_adapter_slash.go 563 行，非骨架）；message_handler 覆盖率 56.8%→达标 | `command_parser/slash_command.py` · `message_handler.py` L347-1145 · `agent_ws_server.py` L2277-2352 |
| 10.4.4b | ⤵️ | Slash 命令处理（/sandbox 命令面） | 9 个 sub + Linux 平台守卫 + SANDBOX_BAD_REQUEST/SANDBOX_INTERNAL 双错误码；依赖 10.3.24 Sandbox 包与 jiuwenbox（21k 行 Python 守护进程） | `agent_ws_server.py` L2853-3510 |
| 10.4.5 | ✅ | ACP Subprocess Env | 子进程环境设置；✅ BuildAcpSubprocessEnv 已随 10.6.24d 实现于 swarm/acp（profileScopedKeys 剥离+${VAR} 解析覆盖） | `jiuwenswarm/acp/subprocess_env.py` |
| 10.4.6 | ✅ | ACP CLI Smoke 入口 | ✅ `uapclaw acp-chat <profile> [message]` 已落地 `cmd/uapclaw/acp_chat_cmd.go`：一次性冒烟（connect→chat→close，message 缺省 Hello，**非 REPL**，已订正 11.25 D4 的误读）；轻量自举（logger.Setup + dotenv 非覆盖 LoadNonOverride + config.Load，不加载 extensions/crypto/workspace）；profile 解析复用 acpchat 严格校验 `LoadProfile`/`BuildClientSpec`（args/env 类型错比 Python 更严，有意偏差）；typed error + `exitCodeFor` 落 0/1/2/130；响应文本直出 stdout、诊断走 logger；`ACP_CLI_CLOSE_TIMEOUT_S` 默认 30s 限时关闭；假对端 e2e（acp_peer.sh）走通 connect→chat→close；顺带修复 logger（全局）：控制台改 stderr（对齐 Python setup_logger 的 StreamHandler 默认流）、日志目录改 `pathutil.LogsDir()` 受 `UAPCLAW_DATA_DIR` 管辖 | `jiuwenswarm/acp/cli.py` |
| **10.5 扩展系统** | — | | | |
| 10.5.1 | ✅ | 数据类型 | ExtensionMetadata + ExtensionConfig | `jiuwenswarm/extensions/types.py` |
| 10.5.2 | ✅ | 钩子事件常量 | GatewayHookEvents + AgentServerHookEvents | `jiuwenswarm/extensions/hook_event.py` |
| 10.5.3 | ✅ | 钩子上下文结构 | 4 种 HookContext（Memory/GatewayChat/AgentServerChat/SystemPrompt）；✅ FromMap 写回闭环随 10.5.7 回填（4 上下文 + TriggerMemory 辅助） | `jiuwenswarm/extensions/hooks_context.py` |
| 10.5.4 | ✅ | BaseExtension 基类 | BaseExtension 接口 + BaseExtensionImpl 默认实现 | `jiuwenswarm/extensions/sdk/base.py` |
| 10.5.5 | ✅ | AgentServerClientExtension | GetClient() 返回 AgentTransport | `jiuwenswarm/extensions/sdk/agent_server_client.py` |
| 10.5.6 | ✅ | ExtensionRegistry | 单例 + 回调触发（复用 CallbackFramework；本地接口打破 sdk→extensions 循环依赖） | `jiuwenswarm/extensions/registry.py` |
| 10.5.7 | ✅ | ExtensionLoader | go-plugin 子进程方案（D）：gRPC 协议（Register/SetExtensionDir/GetMetadata/Shutdown/HandleEvent + Transport 三件套）+ 发现/加载/声明注册编排 + 事件桥 + GRPCPluginTransport；依赖安装删除（Go 静态编译） | `jiuwenswarm/extensions/loader.py` |
| 10.5.8 | ✅ | ExtensionManager | WithSearchPaths 注入解 config↔extensions 环 + LoadAll（单失败继续）/ShutdownAll/List 对齐 Python | `jiuwenswarm/extensions/manager.py` |
| 10.5.9 | ⤴️ | CallbackCompat | Go 已有 CallbackFramework，无需额外实现 | `jiuwenswarm/extensions/callback_compat.py` |
| 10.5.10 | ✅ | CryptoUtility | 双路径：宿主内置 AesGcmProvider（密钥文件 ~/.uapclaw/crypto.key 自动生成，0600）+ 插件 provides_crypto 声明（proto 7→9 RPC：Encrypt/Decrypt；loader 注册 + 覆盖全局，最后加载者胜）；registry any→crypto.CryptoProvider 收紧；web/config 消费点接线（error 签名统一 + 失败跳过字段）；sdk stub 删除 | `jiuwenswarm/extensions/sdk/crypto_utility.py` · `jiuwenswarm/common/security/base_crypto.py` |
| **10.6 Swarm 侧 Harness 集成** | — | | | |
| 10.6.1-2 | ✅ | Prompt Builder | Agent/Code 模式提示词 | `jiuwenswarm/agents/harness/common/prompt/` · `code/prompt/` |
| 10.6.3-10 | ✅ | Swarm Rails | AskUser✅/Avatar✅/Permissions✅/Interrupt✅/ProjectMemory✅/ResponsePrompt✅/RuntimePrompt✅/StreamEvent✅ | `jiuwenswarm/agents/harness/common/rails/` |
| 10.6.11 | ☐ | AutoHarness | 自动化调度 | ⤴️ **回填 10.3.28f 的 R-6 与 D-1**：R-6 = "首次 MCP reconcile 需替换 child"判据（`deep_adapter_sessions.go:200`、`:205`，上游 :4124-4158，含 `mark_session_mcp_reconcile_started`）；D-1 = 根 `create_instance` 早退处的 `_start_mcp_prewarm()`（web channel 后台预热 connected MCP 进程级连接缓存，上游 :13109-13120 fire-and-forget）； `jiuwenswarm/agents/harness/common/auto_harness/` |
| 10.6.12 | ✅ | SessionOps | 会话操作；⚠️ 已知依赖方向偏差：sessionops→server/session+utils 为本任务破环迁移遗留（gateway 经 message_handler→sessionops 传递依赖 server），违反规则 6.5，待 server↔agents 边界收敛工程统一处理（11.10 审查登记，非 11.10 引入） | `jiuwenswarm/agents/harness/common/session_ops_service.py` |
| 10.6.13-18 | ✅ | Swarm Memory | Config✅(swarm版config.go+包内缓存+ClearConfigCache回填)/Dreaming✅/Embeddings✅(Provider接口+OpenAICompatible+Mock+工厂，含manager/internal/types依赖链补齐)/External✅(config+builder 迁入 swarm memory 包：engine 门禁+external 段解析+openjiuwen 映射+四路分发 builder；偏差修复：kv 默认 shelve/键名 embed_base_url/ltm mkdir/mem0+openviking env 回退/品牌默认 uapclawswarm/IsExternalMemoryEnabled 双条件/agentarts 移除；adapter 瘦身薄调用，agentName 回退删除)/Forbidden✅/RPC✅(独立子包 common/memoryrpc：5 handler 纯函数层 List/Edit/Status/Toggle/Open+13 个 Python 对齐私有函数+6 个 Go 辅助函数(pathPartContains/expandUser/absoluteJoin/loadConfigMap/dirOrEmpty/anyOr)；白名单校验/UAPCLAWSWARM.md 根特例/品牌映射 jiuwen→uapclaw；Toggle 写回 Load+SetRoundTrip 点路径(D3)对齐 11.14 先例+ruamel round-trip 保注释(M-05：config 包 SetRoundTrip yaml.v3 Node 模式)；Status 六块统计含 detailed 分支 external_memory(engine external/both)且 provider/model nil 对齐 Python None(T-09)；Edit preview splitlines 语义对齐(尾换行/CRLF)；CJK 字符计数 S-03(utf8.RuneCountInString)；mtime float 秒 M-02；mkdir/touch error 字段协议 M-04；写回失败 D6 new_value=old_value；覆盖率 91.5%；✅ TUI 面板注册(11.15 已回填 2026-11-03)) | `jiuwenswarm/agents/harness/common/memory/` |
| 10.6.19 | ✅ | TeamManager 主体 | 72 方法骨架回填+customizer 六步(a-e 实现/f ⤵️10.6.24)+runtime_inheritance(四结构体+双白名单+BuildMemberRails 13 步)+2 rail+exceptions+分布式纯函数层+model_pool 注入+GetDefaultModels 权威收敛三处+CodeMemberConfigurer 注入+buildAndMountMemberRailsForContext 真实实现（updateEvolutionConfig 步骤 6 闭环，审查 M-25 修复）；覆盖率 67.1%（新代码 87-100% 达标；lifecycle/interact/distributed 骨架属 10.6.20-22、stepD/DrainTeamSkillEvents 为集成路径无注入点，不追） | `team_manager.py` |
| 10.6.20 | ☐ | DistributedRuntime | zmq 传输层（✅ 纯函数层已随 10.6.19 带出：IsDistributedMode/RuntimeRole/ParsePort/NormalizeDistributedTransportFields/FallbackDistributedToLocal/PG 五函数）；⤴️ 纯函数层来源 10.6.19 | `distributed_runtime.py` |
| 10.6.21 | ☐ | RemoteMemberBootstrap | 远程成员引导 | `remote_member_bootstrap.py` |
| 10.6.22 | ☐ | A2X RegistryRuntime | A2X 注册运行时；代码回填点：`internal/swarm/server/adapter/deep_adapter.go`、`deep_adapter_a2x.go`、`code_adapter.go`（均标 `⤵️ A2X / 10.6.22`） | `a2x/` |
| 10.6.23 | ✅ | TeamRails | inheritance+MemberSkillToolkitRail+WorkspaceReportPathRail+exceptions 已随 10.6.19 实现（方案 C 合并决策） | `team_runtime_inheritance.py` · `rails/` |
| 10.6.24 | — | Swarm 内置工具集（2026-10-26 拆分决策：按依赖边界拆 5 子节 a→b→c→d→e，全部归本节不换章节；d 已随 11.25 桥接落地（✅ 见 10.6.24d 行）、e 排最后（xiaoyi_gui_agent ⤵️ 9.31）；已实现资产在 24a 建全局接线清单防回填丢失；已核实：cron(11.10)/memory_tools+MemoryRail(10.6.13-18)/skill/web/multimodal/browser_move/todo 基座均在 agentcore 层就绪，10.6.24 仅做 Swarm 侧定制工具+装配接线；browser_tools.py(672行)/swarm audio_tools.py(328行) 疑似零消费死代码，按既定决策仍复刻、待 spec 阶段核实动态引用） | | `jiuwenswarm/agents/harness/common/tools/` |
| 10.6.24a | ✅ | 轻量工具+装配接线 | wiki 3 工具(wiki_ingest/wiki_query/wiki_lint)+generate_image+mcp_toolkits 聚合(GetMcpTools 注入 SkillDevDeps.MCPToolsFactory)+todo_compat 兼容层(status=deleted→delete 映射)；回填桩：getToolCards 步骤1/7、syncToolGroup、uapclaw.go:813、deep_adapter.go:1416(_cancel_pending_todos 标记归属从 10.6.3-10 修正为本节)。已交付：10 commits(b6eb76c9..19c1aa68)——swarmtools 包 7 文件+平台拆分(Windows shell 探测 build tag 隔离)+agentcore todo 注入点(TodoModifyToolFactory)+DeepAdapter 双字段/syncToolGroup Python 同签名/TodoModifyFactory 接线/cancelPendingTodos+formatTodosForFrontendLocal；覆盖率 tools 85.0%/todo 89.5%；spec 8 偏差按定案落地（#2 save 分派外置×2、#5 sysOp 运行时恒 nil、RE2 双限制手动等价、双 _env_flag 保留等） | `wiki_tools.py` · `image_tools.py` · `mcp_toolkits.py` · `todo_compat.py` |
| 10.6.24b | ✅ | send_file 全链 | web_file_download(HMAC 令牌+密钥文件)+SendFileToolkit(chat.file 事件+AppendHistoryRecord+sendPushFunc)+gateway `/file-api/download` 路由；依赖全就绪(11.14✅/session_history✅/sendPushFunc✅)；已交付：6 commits(96bda791..d0b1c3a0)——令牌管理器载荷结构体保序(字节级对齐 Python json.dumps)+SendFileToolkit 双注入(SetSendFilePushFunc/SetSendFileHistoryFunc 启动接线)+两回填桩落地(守卫收窄对齐修正+WithRefresh 有意偏差#2)+quoteAllForDownload 全量百分号编码(对齐 quote(safe=""))；覆盖率 tools 85.8%/gateway 78.5%/adapter 45.2%/team 70.1% 均不低于基线 | `send_file_to_user.py` · `web_file_download.py` · `channels/web/app_web.py`(下载路由) |
| 10.6.24c | ✅ | 会话级工具 | agent.fast MultiSessionToolkit(session_new/cancel/list goroutine 并发+chat.session_result 推送+sync.Once 汇总 chat.final)+code 模式 user_todos(六 action+按 channel 隔离 markdown 持久化,channel 经 ctx 读取);已回填:deep_adapter_config.go updateToolsForMode、code_adapter.go:809 case、code_adapter.go:655 双调用+user_todos sync、agent_server 双注入;已交付:8 commits(bd9a514c..273bb16a)——偏差 8 项归档 spec §7(stream 三态适配/lookahead 正则手动等价/params 转换/汇总 nil 守卫/ctx channel/WithoutCancel 双用/status+priority 空串跳过等),race 修复 2 处(defer 内 map len 持锁/测试 recorder 化);覆盖率 tools 87.4%;实施后审查闭环(2026-10-31):P1 修复 cancel_session per-task done channel(旧 wg.Wait 等 toolkit 全部任务会被无关长时任务卡住,Python gather 只等单个)+删 userTodoParamsNames 死变量+偏差扩至 12 项归档(补录 #8 枚举例外/#9 cancel 语义/#10 Configure/#11 签名占位/#12 指针 case 收窄) | `multi_session_toolkits.py` · `user_todo_tool.py` |
| 10.6.24d | ✅ | ACP 工具组 | AcpStdioClient(stdio JSON-RPC)+acp_chat(profile 缓存+会话管理)+acp_output_tools(6个文件/终端工具+get_acp_output_manager 桥接+complete_jsonrpc_response)+_refresh_acp_runtime_tools(按客户端能力装卸)；已交付：12 任务（e2a/acp 纯函数+descriptor/protocol、swarm/acp 三 commit 886b1374 stdio_client+平台拆分+subprocess_env、acp_output_tools bb2f1f8f E2A server_push 闭环、acp_chat 子包 a75ae0ff 92.9%、refreshAcpRuntimeTools 5439da2e 能力装卸、权限确认桥 23b237df、handleACPToolResponse c446424f、AgentManager ACP 三分支 4a6b2b56+e2a/acp protocol 95.0%、capabilities key 913c6226、acp_chat 注册接线 ae07e1c9、SendPush 接线收尾+Chat result 先到时非阻塞排空 updatesCh 补齐聚合修复 flaky）；八回填点全落地：①②getToolCards 步骤10+code case、③deep/code 双接线、④acp.tool_response、⑤AgentManager 三处、⑥权限确认 JSON-RPC、⑦未知迟到 ignored、⑧metadata key 修正；偏差要点：AcpChatTool 恒文本不抛错对齐 Python except 兜底、descriptor title 非空保留（Python `or` 语义）、envelope.Params 层 key 不变仅 metadata 层改；全量回归 289 包零 FAIL（CGO_ENABLED=1+sqlite_fts5 test）；四包覆盖率 86.8%/95.0%/86.9%/92.9% | `acp/stdio_client.py` · `acp_chat/tool.py` · `acp_output_tools.py` |
| 10.6.24e | ☐ | 小艺电话 28 工具 | xiaoyi_phone_tools 全部(alarm/calendar/contact/file/message/note/phone/photo/push_result/save/timestamp/location/collection/image_reading 共 27 个纯 HTTP 工具+xiaoyi_gui_agent)；xiaoyi_gui_agent ⤵️ 9.31 MobileGUIAgent；开关 channels.xiaoyi.phone_tools_enabled 默认关，11.23 通道未实现时无紧迫性；回填桩：getToolCards 步骤8 | `xiaoyi_phone_tools/` |

**验证点**：✅ 用户运行 `uapclaw chat` 即可与 Agent 对话（REPL → Gateway → E2A → AgentServer）

---

## 领域十一：Gateway + IM 渠道

> 多 IM 渠道接入（可选，依赖领域十）

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **11.x Gateway 核心** | — | | | |
| 11.1 | ✅ | BaseChannel 接口 | `Config/Start/Stop/Send/OnMessage` | `jiuwenswarm/gateway/channel_manager/base.py` |
| 11.2 | ✅ | ChannelManager | 注册/注销/分发/配置热更新回调 | `jiuwenswarm/gateway/channel_manager/channel_manager.py` |
| 11.3 | ✅ | MessageHandler | 入站→AgentServer，出站→Channel（完整转发链路）；✅ before_chat_request 触发点已回填（forward_loop 步骤9，对齐 Python L2502，曾误挂 TODO(#11.13)）；✅ handleCronPushPayload 已随 11.10 实现（8 action 路由 + chat.tool_result 回发 + streamModes mode 覆盖）；stub：skillsSlashNotice/branchSlashNotice/rewindSlashNotice→AgentServer RPC | `jiuwenswarm/gateway/message_handler/message_handler.py` |
| 11.4 | ✅ | Slash Command Parser | Slash 命令解析（command_parser 子包） | `jiuwenswarm/gateway/message_handler/command_parser/slash_command.py` |
| 11.5 | ✅ | WebSocketAgentServerClient | WS 客户端，请求/响应路由，流式，自动重连 | `jiuwenswarm/gateway/routing/agent_client.py` |
| 11.6 | ✅ | RouteBinding | 路由绑定 | `jiuwenswarm/gateway/routing/route_binding.py` |
| 11.7 | ✅ | SessionMap | scope 枚举+LoadSessionMapScope（未知值/未注册降级默认）+ makeKey/makeSessionID（::分隔+hex时间戳+6位hex）+ checkpoint 持久化（load 过滤/防御 save）+ MessageHandler 集成：首建覆盖/ApplyChannelState 无条件刷新双分支（对齐 L1151-1159 陷阱）/new_session rotate 轮转 + TriggerSessionStartHook；sync.Mutex 并发补齐 | `jiuwenswarm/gateway/routing/session_map.py` |
| 11.8 | ✅ | InteractionContext | 交互上下文：PendingInteraction 15字段+8方法（Save/Remove/Load/FindPending五层过滤/FindAllGroupPending/FindGroupPending/CleanupExpired宽松解析/BuildResumeContent双模板）+dataclass三类默认值对齐（必填缺失跳过/status默认pending/时间补now）+敏感键过滤；⤵️ 消费方 11.12（im_pipeline）/11.16 飞书/11.21 企微 DM resume | `jiuwenswarm/gateway/routing/interaction_context.py` |
| 11.9 | ✅ | GatewayServer | 多路由 WS 服务器本体已补齐（GatewayServer 类 L240-720：resolveRoute 四分支/双客户端路由表/connection.ack/handleRawMessage 三层处理/Send 四段出站/断连清理）+ InboundGatewayServer 双层 chan + ExternalChannel 出站装配 + 现有 HTTP 宿主改名 AppGatewayServer；ACP 路由接线已随 11.25 落地（桥+Send/rawMessage 分支+InstallDefaultAcpHooks） | `jiuwenswarm/gateway/app_gateway.py` |
| 11.10 | ✅ | Cron 调度服务 | go-quartz v0.15.2（croniter→Quartz dow 变换）；CronJobStore tmp+rename 原子写；CronSchedulerService 事件堆三分支（wake/push/push_update）+方案B委托降级单调度器（进程注册表修正 Python 单进程双跑缺陷）；MessagePublisher 接口破环；9渠道元数据全量对齐；CronController+8个cron.job.*端点+handleCronPushPayload；Agent侧CronTools双写+E2A转发+cronToolsBackend 11方法；deep_adapter步骤9绑定+TaskScheduler取消×2；team_manager注册点；app_gateway装配；覆盖率 gateway/cron 85.9% + cronbridge 90.1% | `jiuwenswarm/gateway/cron/` |
| 11.11 | ✅ | 心跳服务 | GatewayHeartbeatService 旁路定时器（先睡后跳循环/tick 六步/三级 content 提取/HEARTBEAT_OK 判定/relay 回传 webhook/活跃时段跨午夜支持）；web 三 RPC（get_conf/set_conf 热重启+config.yaml round-trip 写回+agent 缓存清除/get_path 相对路径）；app_gateway 装配 env 三级回退+Start/Stop 接线；session.list heartbeat_ 过滤（偏差⑨）；9 项偏差归档 spec；heartbeat 包覆盖率 94.2% | `jiuwenswarm/gateway/heartbeat/heartbeat.py` |
| 11.12 | ✅ | IM Pipeline | 独立包双管线完整对齐：IMPlatformAdapter 接口(10 方法+imUserIDNameResolver 可选断言)+IMConversationProcessor(七层分支/Lazy LLM 0.2+0.7/SYSTEM_PROMPT 逐字)+IMInboundPipeline(Apply 十三步/resume 分支/peekPending 模板)+IMOutboundPipeline(十四步短路链/追问前缀 gpq+iact 落盘/classify prompt 逐字/api_key 守卫)；message_handler 双挂接(步骤6+PublishRobotMessages 入队前)+OutboundPipeline 签名改造 Apply(ctx,msg) error；app_gateway 装配步骤4.7；⤵️ 适配器注册 11.16 飞书/11.21 企微；9 偏差归档 spec §8 | `jiuwenswarm/gateway/im_pipeline/` |
| 11.13 | ✅ | Gateway Hook | GatewayHookHandler 4 事件方法（SessionStart 去重/UserPromptSubmit 空 query/SessionEnd 先 discard/Notification type query）+ activeSessions + 10s setdefault + recover 兜底；HookExecutor 迁 server/hooks→common/hooks（第三例破环，user_hook_rail 留守）；forward_loop 步骤3 回填；NewGatewayServer 装配 RegisterConfig | `jiuwenswarm/gateway/hooks/` |
| **11.x IM 渠道** | — | | | |
| 11.14 | ✅ | Web 通道 | WebSocket + HTTP RPC（M-05：config 写回点 updateYAMLKeyInConfig/replaceTeamsInConfig/updateDefaultModelsInConfig/updateMemoryForbiddenInConfig 全部切换 SetRoundTrip ruamel round-trip 保注释；modes.team 删除分支保留 Save——无删除 API 的等价路径） | `jiuwenswarm/gateway/channel_manager/web/` |
| 11.15 | 🔄 | TUI 通道 | CLI 路由最小装配已落地（/tui ForwardMethods 80 项 + 5 chat ack handler + DisconnectHandler + 全链路集成测试 tests/integration/swarm/gateway/）；纯本地 handler 27 个（config.*/session.list/cron.* 等）与 TUI 面板 ⤵️ 继续（✅ 10.6.18 memory_rpc 5 个 local handler 已注册进 /tui——memory.list/edit/status/toggle/open，wrapper 自持 res 帧 + recover + result["error"] 转 ok=false，集成用例真实计数断言，2026-11-03）；✅ 目录已对齐 Python：`tui_route.go` 自 gateway 包迁入 `internal/swarm/gateway/channel_manager/tui/`（2026-11-01），原"tui 装配放 gateway 包"偏差消除——Install 注入改为 `RouteServer` 接口（`RegisterLocalHandler` + `SendResFrame`），与 Python `register_cli_handlers(channel=gateway_server)` 同构；`routing.LocalHandlerFunc` 收口到 routing 包供两包共用 | `jiuwenswarm/gateway/channel_manager/tui/` |
| 11.16 | ☐ | 飞书通道 | 事件回调，企业多机器人 | `jiuwenswarm/gateway/channel_manager/im_platforms/feishu/` |
| 11.17 | ☐ | 钉钉通道 | DingTalk Stream 协议 | `jiuwenswarm/gateway/channel_manager/im_platforms/dingtalk/` |
| 11.18 | ☐ | Telegram 通道 | Telegram Bot API | `jiuwenswarm/gateway/channel_manager/im_platforms/telegram/` |
| 11.19 | ☐ | Discord 通道 | Discord Bot | `jiuwenswarm/gateway/channel_manager/im_platforms/discord/` |
| 11.20 | ☐ | 微信通道 | iLinkAI 桥接 WebSocket | `jiuwenswarm/gateway/channel_manager/im_platforms/wechat/` |
| 11.21 | ☐ | 企微通道 | WeCom AI 机器人 | `jiuwenswarm/gateway/channel_manager/im_platforms/wecom/` |
| 11.22 | ☐ | WhatsApp 通道 | WhatsApp 桥接 | `jiuwenswarm/gateway/channel_manager/im_platforms/whatsapp/` |
| 11.23 | ☐ | 小艺通道 | 华为小艺智能助手 | `jiuwenswarm/gateway/channel_manager/im_platforms/xiaoyi/` |
| 11.24 | ☐ | 平台适配器 | IM 平台通用适配 | `jiuwenswarm/gateway/channel_manager/im_platforms/platform_adapter/` |
| 11.25 | ✅ | ACP 协议桥接 | session_updates（e2a/acp）+ AcpGatewayBridge 桥主体/三拦截器/timer + GatewayServer/app_gateway 接线 + MessageHandler 别名链 + AcpChannel 本体/WS 客户端族/31 例映射 + cmd acp 双模式 + 桥 24 例 + e2e 3 用例全绿；**2026-10-30 完整对齐复核闭合**（P1 数字 jsonrpc id 单点归一 `NormalizeJSONRPCID`／P2 桥 final 非 idle 分支补 timer cancel／P3 stdin 末行无换行不丢弃／P4 路由工厂链真接通：binding 补 forward 集合 + `BuildTuiRouteBinding`+Install + `RegisterLocalHandler`（2026-11-01 更名：`tui.BuildRouteBinding`，见 11.15 行），/acp 与 /tui 同链／B1 WS ping 心跳／B3 桥 panic→-32603／B4 别名锁 RWMutex+ctx 透传／B5 `-g` 短选项／B8 非对象 JSON 文案），protocol/acp 覆盖率 86.1%、gateway 81.9%、e2a/acp 95.2% | `jiuwenswarm/gateway/channel_manager/protocol/acp/` |
| 11.26 | ✅ | A2A 协议通道 | 引官方 SDK `a2aproject/a2a-go/v2 v2.7.0`：A2AChannel(BaseChannel 8 方法)+a2aExecutor(a2asrv.AgentExecutor)+6 映射函数+a2a_card+三路由（rpc POST-only / card / extended card）+app_gateway 步骤4.8 与启停接线+`ChannelTypeA2A`；Python 侧 bootstrap 与其钉死的 `a2a-sdk 1.0.0` 不兼容（`a2a.server.apps`/`DefaultRequestHandler` 已移除），故基线改为「通道层语义 1:1 复刻源码 + 服务端行为 1:1 对齐 SDK 1.0.0」，12 项偏差归档 spec §9；10.2.10 E2A A2A 适配器保持无消费方（Python 同）；覆盖率 93.0% | `jiuwenswarm/gateway/channel_manager/protocol/a2a/` |

> **channels 子树目录对齐审计（2026-11-01）**：与 Python `jiuwenswarm/channels/` 对照，Go 侧 `internal/swarm/channels/` 目前只有 `web/`（= Python `channels/web/{app_web.py, frontend/}`，已 1:1）。缺口均为**功能未实现**、无物可迁，实现时按下表落位：
> - `channels/acp/app_acp.py`（ACP 服务入口，173 行）→ 对应 10.4.1 CLI 聊天模式（☐ 未实现）；⚠️ 勿与 `internal/swarm/acp/`（外部 ACP 子进程 stdio **客户端**，10.6.24d）混淆，后者不属于 channels 子树。
> - `channels/desktop/desktop_app.py`（784 行）、`channels/tui/frontend/`（112 个 src 文件）→ 无对应计划行，暂不落位。
> - `gateway/channel_manager/im_platforms/{platform_adapter,dingtalk,feishu,telegram,discord,wechat,wecom,whatsapp,xiaoyi}` → 11.16-11.24（全 ☐），计划表已按该路径记录。
> - 已对齐：`channels/web/`、`channel_manager/{base,channel_manager,protocol/{a2a,acp},web,tui}`、`gateway/im_pipeline/`。
>
> **前端产物来源已对齐 Python `_default_dist_dir`（2026-11-01）**：`channels/web/dist.go` 三级优先级 —— ① `--dist`（→ 环境变量 `UAPCLAW_WEB_DIST`，不存在即启动失败，对齐 Python `SystemExit`）② 用户数据根 `<WorkspaceDir>/channels/web/frontend/dist`（存在即用，装后可替换前端）③ 编译期内嵌 `frontend/dist`（`go:embed` 兜底）。占位产物只打 Warn 不拦截（Python 亦只判存在）。

---

## 领域十二：沙箱与部署

> JiuwenBox 沙箱系统 + CLI 入口 + 部署

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **12.x JiuwenBox 沙箱** | — | | | |
| 12.1 | ☐ | 策略引擎 | YAML 策略定义，解析与执行 | `jiuwenbox/policy/` |
| 12.2 | ☐ | 进程隔离 | go-landlock + seccomp + cgroup | `jiuwenbox/supervisor/` |
| 12.3 | ☐ | 推理隐私代理 | LLM API 代理，隐私脱敏 | `jiuwenbox/proxy/` |
| 12.4 | ☐ | 沙箱运行时 | 进程管理，文件共享 | `jiuwenbox/runtime/` |
| 12.5 | ☐ | 沙箱 HTTP 服务 | API 路由（sandbox/proxy/policy） | `jiuwenbox/server/` |
| 12.6 | ☐ | JiuwenBox CLI | 命令行工具 | `jiuwenbox/cli/` |
| **12.x CLI 入口** | — | | | |
| 12.7 | ✅ | 统一启动器 | `uapclaw app`：启动 AgentServer(goroutine) + Gateway + serverReady 等待；✅ 扩展初始化接线随 10.5.8 回填（CreateInstance→Manager→LoadAll→transport 选择：扩展优先/ChannelTransport 回落，AgentServer nil-guard） | `jiuwenswarm/app.py` |
| 12.8 | ☐ | AgentServer 启动 | `uapclaw agentserver` | `jiuwenswarm/server/app_agentserver.py` |
| 12.9 | ☐ | Gateway 启动 | `uapclaw gateway` | `jiuwenswarm/gateway/app_gateway.py` |
| 12.10 | ☐ | Web UI 启动 | `uapclaw web` | `jiuwenswarm/channels/web/app_web.py` |
| 12.11 | ☐ | 工作区初始化 | `uapclaw init` | `jiuwenswarm/init_workspace.py` |
| 12.12 | ☐ | 服务启动器 | `uapclaw start` | `jiuwenswarm/start_services.py` |
| 12.13 | ☐ | 自动更新器 | 版本检测与升级 | `jiuwenswarm/common/updater.py` · `upgrade_executor.py` |

---

## 领域十三：检索文档索引管理（retrieval/indexing 缺口）

> 文档级索引管线：解析 → 分块 → 嵌入 → 入库。与 7.36 记忆索引（用户记忆 CRUD）不同，本章节处理外部文档（上传文件/链接/图片）的索引构建与维护。
> 2026-10-27 扩充：新增 13.19-13.28 检索查询管线（Retriever 六兄弟 + QueryRewriter + KB 三兄弟，~4600 行源码 + ~4700 行 Python 测试），闭环"查"侧。13.17 TripleExtractor 为首个实施（零前置依赖：TextChunk/llm.Model/json-repair v0.0.18/错误码 155507 均已就绪）。零实现归档项：result_ranking.py（16 行纯 re-export shim，Go graph/ranking.go 已有）+ lazy_load.py/__init__.py（Go 显式 import 天然等价）。
> 2026-10-27 拍板：13.19+13.20 合并实施（接口 63 行无肉+工厂随出，方案 B）；13.21 HybridRetriever 为读取侧下一节。设计：docs/superpowers/specs/2026-10-27-retriever-13.19-13.20-design.md
> 2026-10-28：13.23 GraphRetriever 完成（beam search 图检索+fetch 链+RRF 融合，覆盖率 92.3%）。下一节 13.24 QueryRewriter。设计：docs/superpowers/specs/2026-10-28-graphretriever-13.23-design.md
> 2026-10-28：13.24 QueryRewriter 完成（检索前查询改写器：指代消解/省略补全 + 历史压缩降级，go:embed 提示词 + 模块级 repair 7 函数 + Compress/Rewrite 双 LLM 链，错误码 155603-155606 转已消费，覆盖率 91.2%，-race 干净）。下一节 13.25 AgenticRetriever。设计：docs/superpowers/specs/2026-10-28-queryrewriter-13.24-design.md
> 2026-10-28：13.25 AgenticRetriever 完成（多跳 agentic 检索：图/通用双路径 + GeAR 三件套[read 抽三元组/rewrite 判充分性/RRF 融合] + linkTriples/linkPassages 裸 gather 三分类聚合，错误码 155208-155211 转已消费，覆盖率 94.6%，-race 干净）。下一节 13.26 KnowledgeBase 抽象。设计：docs/superpowers/specs/2026-10-28-agenticretriever-13.25-design.md
> 2026-10-29：13.26 KnowledgeBase 抽象完成（编排层顶点：5抽象+4模板+配置一致性防线[__setattr__→构造尾部+双setter方案B] + 7属性attrEqual[DeepEqual跨源混配拦截] + Chroma类型名反射拦截 + ensureLoader钩子；错误码 155500/155503/155508 转已消费；回填 4 类型×6 getter；覆盖率 94.9%，-race 干净）。下一节 13.27 SimpleKnowledgeBase。设计：docs/superpowers/specs/2026-10-29-knowledgebase-13.26-design.md
> 2026-10-30：13.27 SimpleKnowledgeBase 完成（"查"侧会话总入口：5 抽象方法与 Python 逐条对齐[add 空白 id 填 uuid/update 无 uuid 且 IndexConfig 不带 use_caption_for_images 的不对称照复刻] + Retrieve 懒创建三分支[vector→Vector/bm25→Sparse/其他→Hybrid] + mode 推导 + agentic 包装 + 多 KB 聚合两函数[裸 gather 软失败/去重取 max/limit 真值链] + ConfigManager[键双轨偏差复刻]；Filters 伏笔回填死参数；断言缺口兑现；错误码 155501/155502/155504+155300-155304 转已消费+新增 155509/155510（元数据类型校验 / agentic 未知 kwargs，均对齐 Python TypeError）；审查后两处"更安全但不对齐"改为完全对齐（元数据非数值→155509 中断、数值越界不静默回绕，RetrieveMultiKBWithSource 增 error 返回；§8-6 agentic 未知 kwargs 由静默忽略反转为 155510 报错）；knowledgebase 96.2% / utils 88.1%，-race 干净；全量回归 307 包零 FAIL）。实施期发现并修正 4 处：懒创建双检在 Go 内存模型下属数据竞争（改为读路径同样持锁）、YAML 整数解析为 int 而 JSON 为 float64（统一 asInt）、ConfigManager 缺 pydantic 字段默认值（index_type=hybrid/chunk_size=512/chunk_overlap=50）、多 KB 首见序需显式记录（Python dict 插入序 + 稳定排序）。下一节 13.28 GraphKnowledgeBase。设计：docs/superpowers/specs/2026-11-05-simpleknowledgebase-13.27-design.md
> 2026-10-30：13.28 GraphKnowledgeBase 完成（图增强 KB：use_graph 双索引写入 + GraphRetriever 懒创建与 index_type 注入 + agentic 包装 + 非图回落 SimpleKB + Close 重复关闭字面复刻；Task 0 前置把 Close 统一为 `Close() error`，双变体探测消除；错误码 155505/155506 转已消费；覆盖率 96.6%）。领域十三"查"侧管线闭环。设计：docs/superpowers/specs/2026-10-30-graphknowledgebase-13.28-design.md

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **13.x Indexer 层** | — | | | |
| 13.1 | ✅ | Indexer 抽象接口 + 前置类型 | `TextChunk + IndexConfig/KnowledgeBaseConfig/RetrievalConfig/VectorStoreConfig + Indexer 接口(BuildIndex/UpdateIndex/DeleteIndex/IndexExists/GetIndexInfo)` | `openjiuwen/core/retrieval/common/config.py + document.py + indexing/indexer/base.py` |
| 13.2 | ✅ | ChromaIndexer | ChromaDB 文档索引（ChromaVectorField HNSW配置+ToConstructDict/ToSearchDict/ValidateExtraSearch + DocIndexCallback类型别名+NoOpDocIndexCallback+LoggingDocIndexCallback + ComputeChunkEmbeddings改option模式加DocIndexCallback + SearchResult/RetrievalResult + RRFFusion + retrieval层VectorStore接口+CheckConfigsMatching + retrieval层ChromaVectorStore Add/Search/SparseSearch/HybridSearch/Delete + ChromaIndexer BuildIndex去重嵌入写入/UpdateIndex先删后建/DeleteIndex按doc_id/IndexExists/GetIndexInfo） | `openjiuwen/core/retrieval/indexing/indexer/chroma_indexer.py + foundation/store/vector_fields/ + retrieval/common/callbacks.py + retrieval/vector_store/chroma_store.py` |
| 13.3 | ✅ | MilvusIndexer | `MilvusIndexer(ensureCollection含BM25 Function+dense索引+INVERTED标量索引+dimension三级推断 / BuildIndex五步:ensure→query查重→嵌入→转换→Add / UpdateIndex先删flush后建 / DeleteIndex按doc_id过滤+异常返False / IndexExists / GetIndexInfo stats+describe) + retrieval层MilvusVectorStore(MilvusClient接口+适配器+Add/SparseSearch/HybridSearch原生RRF+RRF降级/Delete/TableExists/CheckVectorField含milvusConstructKeyMap键名映射) + vector_fields.MilvusVectorFieldConfig统一接口 + CreateMilvusAlias/Deduplicate` | `openjiuwen/core/retrieval/indexing/indexer/milvus_indexer.py + vector_store/milvus_store.py + utils/common.py` |
| 13.4 | ✅ | ComputeChunkEmbeddings | ComputeChunkEmbeddings + isImageChunk（接口断言检测 MultimodalEmbedder + 纯文本/图片/文本三条路径 + os.Stat 三级防御 + 透传 EmbedOption + 原地修改 Embedding）；附带修复 document.go mime.TypeByExtension 传完整路径的 bug（改用 filepath.Ext） | `openjiuwen/core/retrieval/indexing/indexer/embed_chunks.py` |
| **13.x Chunker 层** | — | | | |
| 13.5 | ✅ | Chunker 抽象接口 + registry | `Chunker 接口(ChunkText/ChunkDocuments/Process 三方法，对齐 Python chunk_text/chunk_documents/process) + BaseChunker 默认适配器(嵌入获得 ChunkDocuments 默认实现：uuid+metadata 合并 chunk_index/total_chunks/chunk_id，Process=ChunkDocuments 的 ctx 透传) + NewBaseChunker 构造校验(size≤0→155100 / overlap<0→155101 / overlap≥size→155101) + registry(chunkerRegistry+RegisterChunker 空名/重名校验+GetChunker 未知名含注册名列表+hybridFactory 对齐 _hybrid_factory：inner 缺省造 CharChunker/多余 kwargs 报错/inner 类型校验)；13.8 已回填 hybridFactory 完整体 ⤴️` | `openjiuwen/core/retrieval/indexing/processor/chunker/base.py + __init__.py` |
| 13.6 | ✅ | CharChunker | `CharChunker(嵌入 BaseChunker 仅覆写 ChunkText：空文本→[]+每次内联新建 CharSplitter+完成日志 chunk_count) + TextSplitter 接口(Split(doc *Document) 单一入参，error 为 13.7 预留) + CharSplitter(钳制构造：size≤0→1/overlap→clamp[0,size-1]/0值→默认200/40，滑窗 rune 切片防中文截断) + 注册 "char" 到 registry + hybridFactory inner 缺省分支回填(参数过 CharChunker 构造校验穿透)`。⚠️ 实施时一并带出依赖文件 text_splitter.go 的 CharSplitter 部分（现 13.9 归属，编号不变）。设计：`docs/superpowers/specs/2026-10-19-charchunker-13.6-design.md` | `openjiuwen/core/retrieval/indexing/processor/chunker/char_chunker.py + text_splitter.py(CharSplitter)` |
| 13.7 | ✅ | TokenizerChunker | `TokenizerChunker(嵌入 BaseChunker 仅覆写 ChunkText：空文本→[]+Document 包装+Split 委托+完成日志 chunk_count) + IndexSentenceSplitter(splitterConfig 死参数归一化保留+resolveTokenizers 接口三态解析：DecodeTokenizer/Tokenizer/MaxLengthProvider 断言+nil→TiktokenTokenizer cl100k_base fallback+失败链 Warn+空格分词 proxy+resolveChunkSize：未设→maxLen/都设→min/默认200+overlap==0→chunkSize/5) + Tokenizer 接口三态(Tokenizer{Encode}/DecodeTokenizer{+Decode}/MaxLengthProvider{MaxLength}) + TiktokenTokenizer 适配器([]uint↔[]int 薄转发) + registry 注册 "tokenizer"(主动增强：Extra 三键 tokenizer/language/splitter_config)`。⚠️ 已带出 splitter 包全部（13.18 无剩余）。覆盖率：splitter 93.8% / chunker 92.4%。设计：`docs/superpowers/specs/2026-10-18-tokenizerchunker-13.7-design.md` | `openjiuwen/core/retrieval/indexing/processor/chunker/tokenizer_chunker.py + text_splitter.py(IndexSentenceSplitter)` |
| 13.8 | ✅ | HybridChunker | `HybridChunker(嵌入 BaseChunker / NewHybridChunker：inner nil 防御+DimsProvider 断言提取 inner 尺寸 fallback 512/50+155100/155101 校验+谓词可变参数空→defaultNoSplit / ChunkText 纯委托 / ChunkDocuments 完全覆写 4 分支：谓词命中+非空白→strip 单块+metadata 三键，否则逐文档单元素委托 inner，空白文本走 inner 滑窗产出整段空白块（Python 真实行为，CharChunker 只拦空串）/ Process 局部覆写转发（BaseChunker.Process 静态解析绕过谓词的缺陷修复，设计决策 3）) + base.go 增量(DimsProvider 接口+ChunkDims+mergeChunkMetadata 提取供 base/hybrid 共用) + hybridFactory 回填(inner 缺省构造并保留默认 CharChunker，Extra 透传吞掉 / 多余 kwargs 报错)`。覆盖率：chunker 93.7%。设计：`docs/superpowers/specs/2026-10-19-hybridchunker-13.8-design.md` | `openjiuwen/core/retrieval/indexing/processor/chunker/hybrid_chunker.py` |
| 13.9 | ✅ | 分块工具 | `TextChunker SDK门面(嵌入BaseChunker+三处覆写：ChunkText显式错误/ChunkDocuments完全覆写pipeline→inner.ChunkText→mergeChunkMetadata/Process局部覆写转发 + getChunker四分支路由：char→CharChunker、token→TokenizerProvider断言提取→nil则NewTiktokenTokenizer 155102失败分支保留→MaxLengthProvider断言超限下调Warn双字段→NewTokenizerChunker默认参) + text_preprocessor.go(TextPreprocessor接口 + WhitespaceNormalizer/URLEmailRemover/SpecialCharacterNormalizer三预处理器 + PreprocessingPipeline顺序应用 + D1'显式Python语义集regexp2：\w→[\p{L}\p{Nd}\p{Nl}\p{No}_]、\s→[\s\x{1c}-\x{1f}]补偿、\b原生偏差标注 + CharReplacement有序切片链式替换 + TokenizerProvider可选接口 + PreprocessOptions结构体)。registry零注册"text"(对齐Python)。覆盖率：chunker 93.8%。设计：docs/superpowers/specs/2026-10-20-textchunker-13.9-design.md` | `openjiuwen/core/retrieval/indexing/processor/chunker/chunking.py + text_preprocessor.py` |
| **13.x Parser 层** | — | | | |
| 13.10 | ✅ | Parser 抽象接口 | `Parser 五方法接口(Parse/Process 转发/LazyParse 回调式 fn 遇错提前终止/Supports 默认 false/ParseContent 子类覆写点对齐 _parse) + ParseOptions 三选项(DocID 字面空串对齐 pydantic 显式传空串/LLMClient 透传/Extra 收纳 kwargs) + BaseParser 模板适配器(NewBaseParser(self) 构造注入 nil 防御对齐 13.8 先例；Parse 经 self 动态派发 ParseContent 非空包装单文档空内容返空切片 err 传播子类自治；Metadata 显式空 map；Python base.py 零 logger 对应零日志)。覆盖率 88.6%。设计：docs/superpowers/specs/2026-10-21-parser-13.10-design.md` | `openjiuwen/core/retrieval/indexing/processor/parser/base.py` |
| 13.11 | ✅ | AutoParser 路由 | `AutoFileParser(覆写整体Parse五步:os.Stat→155103 "File {doc} does not exist"/小写扩展名registry→155104 "Unsupported format: {ext}, only {sort+Join列表} are supported"(chunker GetChunker 13.5先例)/工厂新实例+Info日志parserTypeName(13.16已知偏差延续)/底层Parse全量opts透传对齐doc_id+llm_client+kwargs/空结果→[] + Extra["file_name"]键存在即原值any透传(含非字符串123→title=123对齐dict.get+Dict[str,Any])缺失兜底filepath.Base + metadata四键nil防御[决策6]doc_id字面含空串/title/file_path/file_ext) + RegisterNewParser(last-wins小写归一无条件覆盖+Info日志,与RegisterParser first-wins并存——Python两入口语义各自对齐13.16决策5兑现) + GetSupportedFormats快照 + AutoLinkParser(docMatcher接口双形态:regexpMatcher[MatchDoc=re.MatchString(TrimSpace)对齐.match(doc.strip())]/funcMatcher[fn(doc)原样不strip]+NewRegexpMatcher/NewFuncMatcher导出包装 + 切片参数二分NewAutoLinkParser(nil→默认路由[]linkRoute两条微信优先/[]→字面空表Parse恒[]对齐routes=None/[]三态决策7 + Parse首个命中→Debug "AutoLinkParser delegating to"→withoutLLMClient剔除LLMClient[决策3字面丢弃:Python关键字调用parser.parse(doc,doc_id,**kwargs)天然不含]/无命中→[] + Supports非空&&httpURLPattern&&任一路由命中 + defaultRoutes每次新实例wechatMPURLPattern→WeChatArticleParser/httpURLPattern→WebPageParserWithDefaults) + AutoParser(isLikelyURL复用13.14正则[bool(doc and match(doc.strip()))] / URL&&link.Supports→Debug→linkParser.Parse全量透传含LLMClient[顶层传路由层丢的链路分层] / file.Supports→Debug→fileParser.Parse透传 / URL-but-unsupported fall-through到file分支[Python控制流字面保留] / 双nil默认构造 / Supports二分委托) + ensureParsersLoaded零代码归档(Go同包init天然完成,ImportError编译期不可能) + 5条日志(2 Info:using parser/dynamically registered+3 Debug:2 AutoParser delegating+1 AutoLinkParser delegating)`。Parser 层全收口（13.10-13.16+本节）。覆盖率：parser包89.3%(基线89.1%↑0.2pp；auto三文件核心Parse/Supports/matchDoc/withoutLLMClient全100%，构造81-83%+defaultRoutes 71.4%为不可达error路径与包内先例一致)。设计：docs/superpowers/specs/2026-10-26-autoparser-13.11-design.md | `openjiuwen/core/retrieval/indexing/processor/parser/auto_file_parser.py + auto_link_parser.py + auto_parser.py` |
| 13.12 | ✅ | PDF Parser | `PDFParser(嵌入BaseParser覆写ParseContent：os.ReadFile双通道共享字节 + extractImagesByPage pdfcpu api.ExtractImages回调批量提图按img.PageNr分桶+复刻命名{filename}__page_{n}__img_{i}.{原生扩展名FileType空回落png}+io.ReadAll解码字节落盘+页内序=回调序(对象号升序) + ledongthuc逐页GetPlainText fonts跨页累积read_file.go先例 + CaptionImages caption紧随该页文本(同页+追加序关联) + joinNonBlank过滤空行strings.TrimSpace + 空串软失败对齐"result if result else None" + 整体except→Error日志+空串) + init注册.pdf/.PDF(.PDF小写归一first-wins跳过——13.16既定语义) + captioner单次构造(偏差§8-6恒真判断直接调用)`。选型：pdfcpu v0.16.3(Apache-2.0纯Go)图片通道+ledongthuc文本通道（unipdf因license key否决——AGPL社区许可也需申请密匙，路线A拍板）；6项偏差归档spec §8（抽对象原生字节vs 300DPI渲染/资源声明面⊇绘制面/原生扩展名vs恒png/对象号升序vs绘制序/无crop链路+批量分桶vs逐页static/恒真判断直调）。净增传递依赖4+hhrutter/tiff等（go mod tidy）。fixture：合成PDF最小结构+Go标准库jpeg.Encode产物JPEG字节（DCTDecode透传preserveRawContent）；内容流裸Td/Tj不带BT/ET（ledongthuc BT注入前导\n真实渲染器无此行为）。覆盖率：parser包90.0%(基线90.9%微降0.9pp因pdf_parser.go文件占比扩大)。设计：docs/superpowers/specs/2026-10-24-pdfparser-13.12-design.md | `openjiuwen/core/retrieval/indexing/processor/parser/pdf_parser.py` |
| 13.13 | ✅ | Word/Excel Parser | `WordParser(嵌入BaseParser仅覆写ParseContent：loadDocxModel zip解包全部件+rels关系表[文档级word/_rels/document.xml.rels]+styles表[rel target→styles.xml styleId→w:name] + parseBlocks保序块遍历[w:body直下w:p/w:tbl交替深度跟踪] + paragraph.text语义[w:t字面/w:tab→\t/w:br→\n/hyperlink子runs同计入] + styleUI[pStyle→w:name→BabelFish 12对别名反转表] + 表格网格展开[gridSpan横向重复N次同内容+vMerge restart master按展开列号记masterText+vMerge续格空val/continue回填本列上方master文本span钳制1] + 图片链[a:blip token流扫描→r:embed→mediaBytes[rels target按word/前缀拼]→查不到字节continue对齐related_parts.get None跳过→image.Decode[png/jpeg/gif注册+x/image/webp副作用注册]解码失败error传播炸整篇对齐Image.open冒泡→toRGBImage draw.Draw RGBA画布→png.Encode落盘images/{filename}__para_{n}__img_{i}.png] + paragraphToMarkdown[Title→#/Heading N→N+1个#1≤N≤9/解析失败Error日志回落原文] + tableToMarkdown[首行表头+---分隔+cell strip+\|转义+尾空行] + "\n".join(strip过滤行)空串→None + paraXML经xml.Encoder重建供图片链二次解码 + 整体软失败Error日志空串对齐except→None) + ExcelParser(覆写整体Parse+Supports对齐Python两处覆写：os.Stat不存在→155103[WithMsg File does not exist] / CSV-TSV分支loadDelimitedRows[BOM剥离utf-8-sig+utf8.Valid非法字节error对齐UnicodeDecodeError→155104 cause链+csv.Reader Comma分隔符+FieldsPerRecord=-1行宽可变]×delimiter tsv取\t + xlsx分支excelSheetRows[excelize OpenFile+GetSheetList同序+Rows迭代器按XML行元素产出保留空行(GetRows裁尾空行不可用)+两遍扫描先记maxCols再逐轴还原+每行pad到满宽对齐openpyxl iter_rows max_column语义] 失败→155104 cause链 + rowsToDocuments黄金基准语义[行文档h非空无条件append含空值"Name: "/空表头整格跳过/include_header=false仅非空值/全空行parts空continue/zip短边截断/row_idx从2起enumerate(start=2)/列文档col_idx<len(row)越界保护/values非空过滤/空表头列回落Column N/true→"Column name: X. Values: ..."/(empty)/false→纯join空列空串/headers空列表补列名range(len(rows[0]))空等价0docs] + cellValue密封sum type[6变体对齐openpyxl values_only类型空间[None|str|int|float|bool|datetime]:cellNone/cellString/cellInt/cellFloat/cellBool/cellTime+非导出isCellValue标记密封包外不可新增变体+noneCell单例] + cellStr[nil与cellNone→""/cellInt→十进制/cellFloat→pyFloatStr/cellBool→True False/cellTime→str(datetime)格式微秒非0补.%06d/cellString→strip] + pyFloatStr[abs==0或∈[1e-4,1e16)→f记法strconv -1最短往返整数值补.0/否则e记法1e+16同形/nan inf -inf字面] + excelCellValue[GetCellType分支Bool原始值1/0→bool/Number Unset→RawCellValue字面量isDateCell判定→ExcelDateToTime还原time.Time对齐openpyxl from_excel同1900epoch/无./e/E→int否则float64对齐_cast_number/String原文] + isDateCell[CustomNumFmt code含未被_\\转义的dmhys token对齐is_date_format仅看;第一段+内置numFmtId 14..22/45..47] + 成功Info日志documents数+Int字段] + init注册.docx/.DOCX×2与.xlsx/.XLSX/.csv/.CSV/.tsv/.TSV×6小写归一first-wins(与13.12/13.15/13.16同款) + WordParser不覆写Supports继承false(Python word_parser.py无supports覆写——路由纯靠registry装饰器，Go侧init注册同构)`。fixture：python-docx 1.2.0/openpyxl 3.1.5 真实产出 golden 对拍（4保真点+vMerge回填master+gridSpan重复+hyperlink文本+tab/br+1×1 RGBA PNG图片链[xlsx数字%.16g写侧存储字面量读侧_cast_number：1e15存1000000000000000读回int无.0——xlsx格式固有行为golden如实记录]+bool True+空表头回落Column 2+全空值行仍产出+include_header=false边界+空表头行边界）。选型：DOCX stdlib自研（docxgo v2.14.0 Style()恒nil+br丢失/godocx v0.1.5 hyperlink被Paragraph.UnmarshalXML default分支d.Skip物理丢弃+docx.Table非导出ct字段reflect不可达panic，三方实测证据spec §1.2）；excelize v2.9.1(MIT)新增直接依赖+x/image v0.47.0 indirect转直接。Python对齐纠错三处：openpyxl写侧%.16g导致0.1+0.2存0.3读回0.3（非0.30000000000000004）+1e15读回int；WordParser不覆写supports；日期str()微秒0省略。覆盖率：parser包87.7%(13.16 json/txt_md review扩码分母扩大后基线)(基线90.0%)。设计：docs/superpowers/specs/2026-10-24-13.13-word-excel-parser-design.md（⤴️ ImageCaptioner 已由 13.15 提前交付） | `openjiuwen/core/retrieval/indexing/processor/parser/word_parser.py` · `excel_parser.py` |
| 13.14 | ✅ | HTML/Web Parser | `HTMLFileParser(嵌入TxtMdParser覆写整体Parse:super()._parse编码检测读文件空串→155110+findMainContent两段式[10选择器>100门槛→body内article/main/div/section树序第一个>200→body兜底]+getTextFromSoup[script/style移除+文本节点\n拼接+[ \t]+压缩+\n\s*\n→\n\n]+正文<50→155110+单Document title兜底(无标题)source_type=web_page) + WebPageParser(嵌入HTMLFileParser+dispatch动态派发接口[parseHTMLToDocs/validateURLFor构造注入最终实例——Python classmethod覆写对应物]+Parse四覆盖参数Timeout/UserAgent/Verify/HTTPClient未设回落实例字段+ParseURL[validateURL经dispatch非http→155110 %q/微信URL→155110引导]→downloadHTML[注入client直用UA仍注入/自建按verify四变体配transport+Timeout]三分支错误[网络错→fetch failed for/非2xx→request failed: %d/兜底同网络错]→effectiveID三级回落docID or url or uuid→parseHTMLToDocs经dispatch→补source_url+Info日志parsed web page] + downloadHTML(verifyDefault零配置/verifySkip InsecureSkipVerify/verifyCAPath x509读文件AppendCertsFromPEM/verifyTLSConfig直用) + verifyMode密封sum type四变体(非导出isVerifyMode密封包外不可新增,对齐13.13 cellValue先例) + ParseOptions扩展四结构化字段(Timeout time.Duration/UserAgent/Verify verifyMode/HTTPClient *http.Client注入即生效verify/timeout/UA失效) + WeChatArticleParser(嵌入WebPageParser覆写parseHTMLToDocs[title→js_content提取→缺失/空→155110无50字符门槛只判空→单Document source_type=wechat_article]+覆写validateURLFor[非微信→155110 Not a WeChat article URL: %q]+覆写Supports+不覆写Parse继承WebPage链+dispatch注入自身) + extractJSContent(div#js_content) + 共享工具(parseHTML goquery/extractTitle og:title→title标签/findMainContent/textLength不改树排除scriptstyle/getTextFromSoup) + URL正则(?i)^https?://\S+与WECHAT_MP_URL_PATTERN(尾部.*?按.*实现RE2无惰性量词等价) + init注册.htm/.HTM/.html/.HTML + ParseWebPageURL/ParseWeChatArticleURL包级别名(对齐模块尾赋值) + 155110 FETCH_ERROR首次启用九处error_msg逐字符对齐(注意#6/#7冒号后空格与前四处=不同)`。硬错误范式（parser 层唯一硬错误解析器）。覆盖率：parser包89.1%(基线87.7%↑1.4pp——13.16 json/txt_md review 扩码分母扩大后基线；若无空方法体密封标记4处0%计入升幅更高)。设计：docs/superpowers/specs/2026-10-24-html-web-wechat-parser-13.14-design.md | `openjiuwen/core/retrieval/indexing/processor/parser/html_file_parser.py · web_page_parser.py · wechat_article_parser.py` |
| 13.15 | ✅ | Image Parser + Captioner | `ImageCaptioner(构造即isLLMSupported warn双分支nil/模型名前缀白名单不匹配均不报错+CpImage四分支软失败归档:不存在err/MkdirAll err/源=目标abspath比较跳过/复制失败Warn+回落原路径+copy2元数据偏差归档§8-2+llmCall多模态消息构造NewUserMessage+WithMultiModalContent[提示词text part逐字复制+image_url data URI part mime检测未知回落image/png+Warn]+base64+Invoke+Content.Text()全链软失败返回空串+CaptionImages逐图串行os.Stat不存在Warn+空串占位) + ImageParser(覆写整体Parse非ParseContent——产出metadata["image_path"]:CpImage单次归档+B1归档路径直通ParseContent/空caption仍返回单Document接通13.4 isImageChunk纯图片嵌入管道禁用态≠失败/ParseContent构造captioner+CaptionImages+非空join对齐None falsy/软失败Error日志空切片对齐except→[]) + 双cp_image去重(spec偏差§8-1/8-5:warn单遍+复制一次)+.jfif MIME幂等注册init(对齐模块级mimetypes.add_type) + 六扩展名init注册.png/.jpg/.jpeg/.webp/.gif/.jfif + WithClient mock走真实Invoke链断言data URI base64逐字节一致`。⚠️ 换序二次修订（2026-10-23）：13.15 提前至 13.12 前实施（13.12 PDF/13.13 Word 硬依赖 ImageCaptioner.caption_images，原序按编号惯性未查消费方向）。覆盖率：parser包90.9%(基线90.2%↑)。设计：docs/superpowers/specs/2026-10-23-image-parser-13.15-design.md | `openjiuwen/core/retrieval/indexing/processor/parser/image_parser.py · captioner.py` |
| 13.16 | ✅ | JSON/TxtMd Parser | `JSONParser(嵌入BaseParser覆写ParseContent：utf-8容错读+Unmarshal失败返回原文对齐JSONDecodeError分支+两空格缩进序列化对齐indent=2+SetEscapeHTML(false)对齐ensure_ascii=False+Encoder尾换行TrimSuffix对齐json.dumps+软失败空串对齐except返回None+2条Error日志对齐) + TxtMdParser(chardet统计检测对齐charset_normalizer.detect+GB-18030带连字符名手工归一simplifiedchinese.GB18030(htmlindex不认,探针实测)+x/net Lookup映射x/text解码+UTF-8检出走零转换路径+BOM剥离+TrimSpace+软失败+1条Error日志+空输入DetectBest(nil,nil)双重防御) + registry骨架带出(parserRegistry map[ext]func()Parser+parserRegistryMu互斥+RegisterParser扩展名小写归一first-wins静默跳过对齐Python非覆盖语义+info注册日志(实例化取类型名为已知偏差)+getParserFactory包内查询留13.11消费+snapshotRegistry测试辅助;AutoFileParser类本体留13.11⤴️;两解析器init注册.json×2+.txt/.md/.markdown×6扩展名对齐装饰器import时执行;chardet v0.0.0-20230101纯Go零传递依赖)。覆盖率：parser包90.2%(基线88.6%↑)。fixture经Python encode交叉验证(GBK/Big5)。设计：docs/superpowers/specs/2026-10-22-txt-md-json-parser-13.16-design.md` | `openjiuwen/core/retrieval/indexing/processor/parser/json_parser.py + txt_md_parser.py + auto_file_parser.py(registry部分)` |
| **13.x Extractor/Splitter 层** | — | | | |
| 13.17 | ✅ | TripleExtractor | `common/triple.go(Triple 4字段无omitempty对齐pydantic default_factory=dict+必填校验零代码归档§8-4) + Extractor 接口(Extract+Process双方法不建Processor基接口13.5/13.10既定决策+ExtractOption收纳**kwargs) + TripleExtractor(构造默认temperature=0.0/maxConcurrent=50+WithTripleTemperature/WithTripleMaxConcurrent选项+modelName死参数§8-1 / buildPrompt模板逐字复制L158-262[{{}}→{}归一+Python脚本逐字节校验]+title or "Untitled"+NewReplacer唯二命名槽防花括号 / parseTriples[TrimSpace+fence>2行剥离+jsonrepair.RepairWithOptions一步取Value对齐repair_json(return_objects=True)+dict取triples键缺失/非list→false+list直达+空列表([],True)+逐项:非数组/长度<3/head含嵌套容器或null→invalidCount++含Warn日志+str(x).strip()两步+metadata注入doc_id/chunk_id+全invalid→false] + pyStr三态[json.Number合法JSON短路路径:Atoi整数字面原样→ParseFloat记法还原对齐Python int/float分野/float64深修复路径:pyFloatStr/int:Itoa]+pyFloatStr[abs∈[1e-4,1e16) f记法整数补.0否则e记法+nan/inf字面] / Extract方案A[goroutine预分配槽位results[i]各写各的+buffered chan信号量对齐asyncio.Semaphore+wg.Wait收齐后按chunk序扫描对齐gather(return_exceptions=True)全跑完语义:无fail-fast/成功按chunk序合并/首错按chunk序非完成序/BaseError errors.As原样返回/首错时成功结果丢弃§8-7] + extractChunk[nil llmClient→155507§8-5+buildPrompt+NewMessagesParam+Invoke WithInvokeTemperature透传+Invoke错→155507含cause+resp nil判空+parse不成功→155507"could not be parsed"] + 5条日志对齐[repair err Error含content[:200]truncateContent/invalidCount Warn含chunk_id/Extract扫描Task failed Error含chunk_id/extractChunk Invoke失败Error] + 编译期断言var _ Extractor)`。测试：Python 23例1:1映射（test_triple 3+pydantic校验归档§8-4+test_base 2+cannot_instantiate归档§8-4 Go接口编译期天然防+test_triple_extractor 16）+Go增强8项（nil客户端/首错按chunk序双向内容分派mock/temperature透传mu保护断言/maxConcurrent串行性CAS峰值/prompt渲染4例/pyStr边界含jsonNumber三态6断言/fence边界2例/极坏输入空串纯文本不报错走default false）。⚠️ 实施修正一处：首错按chunk序测试原按调用序mock在maxConcurrent=1下依赖goroutine调度序（chunk0/1谁先抢信号量不确定）会假失败——改dispatchTripleClient按提示词内容确定性识别chunk身份双向验证与调度序解耦。覆盖率：extractor 95.6% / common 91.8%。设计：docs/superpowers/specs/2026-10-27-tripleextractor-13.17-design.md（7偏差归档§8）。方案A gather模式为13.23/13.25/13.27共5处复用资产 | `openjiuwen/core/retrieval/indexing/processor/extractor/triple_extractor.py + base.py + common/triple.py` |
| 13.18 | ✅ | Splitter | `全部由 13.7 前置带出（SentenceSplitter：detectChinese CJK 占比+标点启发+语义早退 / SplitDoc 分句+token 预算打包 / splitLongSegment 长句滑窗子切分 decode 失败跳过+整句 fallback / flush overlap 尾缀倒序累加 + BaseSplitter：构造校验 155100/155101+GetNodesFromDocuments+SplitText+GetTokenCount + sentencizer v0.3.0 替代 pysbd）`。本步骤无剩余内容 | `openjiuwen/core/retrieval/indexing/processor/splitter/splitter.py + base.py` |
| **13.x Retriever 层** | — | | | |
| 13.19 | ✅ | Retriever 抽象接口 | `Retriever 四方法接口(Retrieve/BatchRetrieve/RetrieveSearchResults/Close) + RetrievalMode 枚举(零值""回落子类默认,偏差归档) + RetrieveOptions.Extra 收纳 **kwargs(本节收而不用对齐 Python 死参数,13.21 Hybrid 消费 alpha/13.27 SimpleKB 写 filters)（13.21 修订：平铺为 Alpha *float64 类型化字段） + 编译期断言；错误码 155200/155202 转已消费` | `openjiuwen/core/retrieval/retriever/base.py` |
| 13.20 | ✅ | Vector/Sparse/PG Retriever + 工厂 | `VectorRetriever(mode校验155200+embedModel nil 155202+向量空结果BM25兜底+归一化 chunk_id 取 metadata["chunk_id"]+裸 errgroup 批量首错即返回) + SparseRetriever(mode校验+归一化 chunk_id 取 result.ID[Python三作者不一致字面复刻]+threshold 静默忽略+rsr 有 mode 校验与 Vector 不对称) + CreateVectorStore 工厂(milvus/chroma 分支 str→强类型转换+default 155402 provider 清单文案逐字符)；⤵️ PGVectorStore(pgvector-go 新依赖)拆后交付,工厂 pgvector 分支先返回 155402 not-implemented 文案` | `openjiuwen/core/retrieval/retriever/vector_retriever.py + sparse_retriever.py + vector_store/store.py + pg_store.py` |
| 13.21 | ✅ | HybridRetriever | `HybridRetriever(三模式路由 retrieve/rsr 两份字面复刻 + 155201 前置校验首次消费[先于 mode 分支] + alpha 构造默认0.5/调用 WithRetrieveAlpha 覆写 + vector 分支 BM25 兜底 + chunk_id 取 result.ID 三作者不一致复刻 + Batch 裸 errgroup alpha 透传 + Close 鸭子探测消解) + RetrieveOptions 平铺修订(Extra map→Alpha *float64 编译期类型安全,13.19 原描述保留) + WithHybridAlpha 构造选项 + fakeVectorStore.HybridSearch 桩可编程化；8 偏差归档 spec §8` | `openjiuwen/core/retrieval/retriever/hybrid_retriever.py` |
| 13.22 | ✅ | 读取侧数据模型 | `TripleBeam(构造建 text 去重集合+Contains 按 text 判重[非全字段]+Get 负索引越界自然 panic≈IndexError+Triples slices.Clone 浅拷贝密封防 append 写穿) + TripleFact=[3]string 定长(读取侧类型与写入侧 Triple 分立,_read len==3 收窄) + TripleMemory(lower join 去重键跨轮去重[逐项 lower 非整体]+memory 保序+TriplesStr "(s p o)" 换行渲染不大写化[与去重键两套逻辑 Python 不对称复刻]+零 mutex 单协程) + MultiKBRetrievalResult(raw_score float64 直装 None 边界归档+KbIDs []string 无 omitempty default_factory 先例)；Python 0 专属用例 Go 23 例全增强(含 B8 跨对象密封判别/M4 大小写互为重复/M7 展示串保留原大小写)；6 偏差归档 spec §8` | `openjiuwen/core/retrieval/common/triple_beam.py + triple_memory.py + retrieval_result.py` |
| 13.23 | ✅ | GraphRetriever | `graph_numeric.go(gonum floats.Norm L2+cosineScores 1e-12 防零除+topkIndices 手写排序取前 k) + capability.go 五能力接口(Python hasattr/getattr 鸭子探测 Go 同构:EmbedModelProvider/IndexTypeCarrier[Getter+Setter]/VectorStoreAccessor/MaxBatchSizeProvider/SupportedModesProvider;三态语义=未实现接口↔hasattr False/实现返回 nil↔属性 None/非 nil) + 三检索器回填 indexType 字段+EmbedModel/VectorStore/IndexType/SetIndexType 访问器(Sparse 无 embedModel=hasattr False 同构) + VLLM MaxBatchSize 委托 openAI(继承同构) + Chroma/Milvus CollectionName 回填(Chroma Set 清 collection 缓存) + graph_beam_search.go TripleBeamSearch(构造默认 numBeams=10/numCandidates=100/maxLength=2/encoderBatchSize=256 死参数存而不读§8-5+max_length<1 155204+embedModel 构造期 EmbedModelProvider 提取 / BeamSearch[空输入 warn 空+无 embed 155202+embedBatch MaxBatchSizeProvider 断言失败显式 155202 拍板 c+余弦 topk 初始 beams+maxLength-1 轮 errgroup.WithContext 扩展] / expandBeams[existTriples 按 text 全 beam 判重+候选路径(beam,triple|nil) nil=保原+路径文本重嵌入打分+seenBeamTexts "; ".join 去重] / searchCandidates[尾 triple 元数据防御解析[缺失/非串/解析失败 warn 空]+entities={首,尾}+mode 三级解析 IndexTypeCarrier>retrieveMode>hybrid[bm25→sparse]+三重过滤 Contains/候选解析裸调用错误冒泡[Python L213 无 try 字面保留由 graph_expansion 降级]/首尾都不在] / resolveCandidateMode+parseCandidateTriple+formatTriples+pyAnyStr) + graph_fetch.go(queryByFilterMilvus NewQueryOption 链+WithOutputFields("*")+WithLimit / milvusResultSetToFetchItems 列遍历+metadata 三形态[map 直装/JSON 串解析/其他吞掉] / queryByFilterChroma GetCollection+Get[WithWhere+WithLimit]+score 恒 0.0 字面[Python L589 get 无分数] / chromaMetaKeysForFetch DocumentMetadataImpl.Keys 复制避免公开面扩大 / fetchItemsToRetrievalResults doc_id 双源[docIDTop 空回落 metadata or 语义]+chunk_id 回落 fallback[fetchTriples 传""/fetchChunk 传查询 id 双默认字面复刻]) + base.go RetrieveOptions 三潜伏消费字段 GraphHops/NumBeams/NumCandidatesPerBeam *int+三构造器 + graph_retriever_impl.go GraphRetriever(六可选构造 nil=动态创建路径 / GetRetrieverForMode[固定优先不支持 155205→无 store 155206→SetCollectionName 先于空校验 155207 字面顺序复刻→三分支 Vector 缺 embed 155202/Sparse/Hybrid→index_type 经 IndexTypeCarrier 注入] / Retrieve[mode 零值回落 hybrid§8-1+threshold+mode≠vector 155201+graph_hops nil→2+GraphHops 折算 WithTripleMaxLength 透传] / GraphExpansion[空 chunks sparse 二次回落 topk or 5+fallback[:topk] if topk truthiness→*topk>0 §8-10+triples 未传 fetchTriples 异常 warn 回落空+空 triples 返回原 chunks+beam search 异常 warn 降级终点+列对齐展开 L504-510 按列优先+fetchChunks+RRF k=60 融合[newChunks 空 fused=chunks]+truncateResults] / BatchRetrieve 裸 errgroup[不派生 ctx 不 cancel 对齐 Python gather 语义 WithContext 会取消兄弟] / fetchTriples[chunk_id IN Milvus/InString Chroma+Chroma 错误整体吞掉 warn+store 具体类型断言分发 spec §3.6+VectorStoreAccessor 挖取] / fetchChunks[逐 chunk_id 并发+fetchChunk 吞错返回 nil 元素过滤] / fetchChunk[chunk_id==单查 limit 1+nil 命中+doc_id 双源] / ensureModeAllowed[index_type None 免检+未知 155203+不兼容 155204 allowed sorted 清单] / allowedModesForIndexType[vector→vector/bm25→sparse/hybrid→三全] / retrieverSupportsMode[类型 switch 三兄弟+SupportedModesProvider 探测+兜底 true]) + doc.go 目录更新；11 偏差归档 spec §8[含 mode 零值回落 hybrid/BatchRetrieve 裸 errgroup/Chroma fetchChunk 错误吞掉/fetchItem 结构化/Chroma score 恒 0.0 语义收窄/encoderBatchSize 死参数/topk truthiness 统一 *topk>0/SetCollectionName 经接口断言/Chroma 测试不可达]。测试：Python 24 例 1:1 映射+Go 增强（graphFetchMilvusStub 经 opt.Request() 提取 Expr 区分 IN/== 查询按 chunk_id 分发+WithMilvusClientOption 注入真实 MilvusVectorStore；graphEmbedRetriever 双能力桩一桩两用；RRF 融合全链同文本双 chunk_id 命中使分数与顺序均确定；修复两处错误原因通过测试）；覆盖率 92.3%（retriever 包）。设计：docs/superpowers/specs/2026-10-28-graphretriever-13.23-design.md | `openjiuwen/core/retrieval/retriever/graph_retriever.py` |
| 13.24 | ✅ | QueryRewriter | `query_rewriter 新包（与 Python 目录同构）：prompts/ 4 md 逐字复制(md5 校验)+go:embed 嵌入(源码部署 OSError 分支编译期消解§8-2) + repair.go 模块级 7 函数(fillTemplate 显式占位符替换 JSON 花括号不误伤/extractJSON 首{尾}提取双 Debug/parseLLMJSON nil 守卫对齐 Python 非 dict 根不 repair 语义+json-repair 兜底/forceString dict→Marshal 非 ASCII 不转义/forceList/forceJSON 四分支/schemaRepair 缺失与 None 同路径默认填充+typo 特判三键白名单收敛[非 dict 元素 forceJSON 包裹+缺键 None→""+非 str→forceString]+三态外 155604 编译期消解) + query_rewriter.go(QueryRewriter 结构体+promptCacheKey 缓存+sync.Mutex 并发保护§8-4 + NewQueryRewriter：compressRange max(n,1) 钳制/promptLang 空回落 zh/ssl_cert+verify_ssl+custom_headers 走 BaseModelInfo.Extra 防御读取[pydantic extra="allow" 同构]/verify_ssl 无证书强制 false[BaseModelClient 要求]/temperature=0.0+top_p 透传/两 schema 字典 + WithCompressRange/WithPromptLang/WithModelClientOption[测试注入通道跳过内部构造] + LoadTemplate 嵌入 FS+缓存+155603 + Msg2Text nil 读全量上下文/role: content 格式/多模态 Parts JSON 序列化/join+strip[str(None) 死分支归档§8-1] + Compress 模板填充→Invoke[SystemMessage+temperature 0.0]→155605→解析链三层[extractJSON→parseLLMJSON→JsonOutputParser 兜底]→非 dict 155604 含 content[:500]→schemaRepair→Info 日志 theme/summary + Rewrite 空白 query 155606→全量历史 ≥ compressRange 触发压缩→成功 json.Marshal 写 SystemMessage[compressed_history]→失败 Warn 降级原文写回[original_history]→GetMessages(compressRange) 尾部截断→模板填 history+query→Invoke→解析链→155604→schemaRepair 返回 8 字段 + truncateForMsg 错误消息截断)。错误码 155603-155606 从零消费转已消费；invoke 显式 model 重传去重§8-8。测试：Python 11 类 30 例 1:1 映射 + Go 增强 8 项(并发 LoadTemplate -race/verifySSL 边界×2/非法 provider 错误透传/注入通道跳过构造/英文模板路由/压缩写回 compressed_history 形态/多模态 content 序列化)。覆盖率 91.2%。设计：docs/superpowers/specs/2026-10-28-queryrewriter-13.24-design.md（7 拍板决策+8 偏差归档§8）` | `openjiuwen/core/retrieval/query_rewriter/query_rewriter.py + prompts/` |
| 13.25 | ✅ | AgenticRetriever | `AgenticRetriever(构造[同包类型断言 *GraphRetriever 判图路径 Q1 拍板 + IndexTypeCarrier 双方法探测推导 defaultMode:vector→vector/bm25→sparse/其余→hybrid + retriever/llm 必填 155208/155209 + WithAgenticMaxIter≤0 Warn 回落2] / Retrieve[top_k≤0→155210 + mode零值回落defaultMode + 图/通用分派] / retrieveGeneric[迭代循环:每轮底层Retrieve→read抽三元组入TripleMemory→rewrite判充分性空=break + RRF融合截topK] / retrieveWithGraph[graph_expansion kwargs.pop 默认true[GraphExpansion *bool nil→true] + 每轮GetRetrieverForMode(true)→chunk检索 + 扩展时proximal read传当前轮q→linkTriples→GraphExpansion覆写chunkResults + 主read传原始query字面复刻§8-2 + 末轮linkPassages链回passages + RRF输入ret在前history在后] / read[docs先切前5再滤空+\n\n join + facts串pyTupleRepr单引号逗号空格join空兜底None + readPrompt逐字节md5校验 + jsonrepair根非list Warn + 逐项非list/长度≠3丢弃 + pyStr逐元素] / rewrite[rewritePrompt存format转义后形态{{→{偏差§8-12 + 历史行Rewritten Question {i} join单元素兜底(No rewriting steps yet) + jsonrepair根非dict Warn + pyTruthy sufficient + next_question非string收窄空串§8-6 + sufficient或空→空串break] / llmCall[Invoke失败Warn返回空串软失败] / linkTriples[RetrieveSearchResults top_k=1 裸gather三分类聚合:err→failed计数/空列表→failed+Warn类型日志/非空→取[0] + Deduplicate按ID + doc_id/chunk_id非string收窄空串§8-7] / linkPassages[Retrieve top_k=5 成功项整列表保留] / assertGraphRetriever 155211防御§8-10 / BatchRetrieve裸errgroup / Close直委 / pyStr/pyFloatStr/isPureIntegerLiteral包内私有复刻13.17同源§8-9 / RetrieveOptions新增GraphExpansion *bool潜伏激活字段[13.28 GraphKB消费])。测试：Python 23例1:1映射 + Go增强18项（探针实测jsonrepair数字一律json.Number形态断言/docs截断语义修正先切前5再滤空/pyStr分支表直测含json.Number病态与超界整数/RRF分数累加1/61+1/61/155211防御同包直改isGraph/工厂失败传播/rewrite收窄§8-6/软失败链/race）。⚠️ 实施修正一处：rewritePrompt常量曾丢失{query}/{question_rewriting_history}/{triples}三占位符——从Python源重提取format转义版逐字节替换修复。覆盖率94.6%（-race干净）`。错误码 155208-155211 零消费转已消费 | `openjiuwen/core/retrieval/retriever/agentic_retriever.py` |
| **13.x KnowledgeBase 编排层** | — | | | |
| 13.26 | ✅ | KnowledgeBase 抽象 | `knowledgebase 新包(base.go+validate.go+doc.go)：KnowledgeBase 接口[5抽象+4模板(ParseFiles只透file_name/ParseURLs Extra全透传不对称字面复刻)+Config访问器] + BaseKnowledgeBase 基座[9组件字段+构造尾部校验+SetVectorStore/SetIndexManager二次赋值setter——Python __setattr__魔法方案B同构] + postAssignValidation三段式[①ValidateIndex 7属性固定序比对+strict CheckVectorField ②isChromaType reflect类型名chroma前缀拦截 ③ensureLoader钩子(MilvusVectorStore.EnsureLoaded回填)] + storageAttrProvider消费接口[7 getter,未实现→nil对齐getattr] + attrEqual[双nil true/单nil false/DeepEqual首步类型比较——Chroma vs Milvus跨源混配天然拦截] + KBOption[FileName/Extra双语义分流] + maybeClose双变体[void直调/error Warn吞] + 错误码155500/155503/155508零消费转已消费 + 回填4类型×6getter(Chroma/Milvus×Store/Indexer,VectorField()any装箱联合类型)+MilvusVectorStore.EnsureLoaded + Python 23例1:1映射+Go增强12项(Chroma拦截路径/strict不影响7属性比对语义固化/ensureLoader计数/三段式顺序/attrEqual直测/KBOptions双语义/模板方法补全[Python基类测试未测]等) + 偏差9项归档spec§8(llmClient收紧BaseModelClient/值渲染%v/close async消解/ctx首普/触发点收窄/WithExtra逐键展开/split字面/uuid直译/storageAttrProvider整接口粒度)`。测试覆盖率 94.9%（-race 干净）。设计：docs/superpowers/specs/2026-10-29-knowledgebase-13.26-design.md | `openjiuwen/core/retrieval/knowledge_base.py` |
| 13.27 | ✅ | SimpleKnowledgeBase | `knowledgebase 包 simple_kb.go+multi_kb.go + utils/config_manager.go：SimpleKnowledgeBase[值嵌 BaseKnowledgeBase + 显式 retriever 参数 + retrieverMu 懒创建(读路径同样持锁——双检在 Go 内存模型下属数据竞争，实施期 -race 修正) + 编译期断言 var _ KnowledgeBase] / 5 抽象方法与 Python 逐条对齐[AddDocuments:chunker 155501→index_manager 155502→strict CheckVectorField→空白 id 填 uuid→ChunkDocuments→IndexConfig 带 UseCaptionForImages→WithIndexExtra("database_name", DatabaseName())→false 155504；UpdateDocuments:同前置校验但文案带 for update_documents、**无 uuid 填充**、IndexConfig **不带** UseCaptionForImages、按 DocID 过滤 chunks 逐条 UpdateIndex 成功才收集；DeleteDocuments:循环不中断任一 false→总 false；GetStatistics:无 index_manager 2 键短路、GetIndexInfo 错误直冒、8 键 map；Retrieve:懒创建三分支+mode 推导+nil config 回落 top_k=5+AgenticRetriever 包装(Extra["max_iter"]→WithAgenticMaxIter，其余键静默忽略)+WithRetrieveFilters] / RetrieveMultiKB+RetrieveMultiKBWithSource[裸 gather 槽位无 fail-fast、单库错误 Warn 软失败、首见序记录对齐 Python dict 插入序+稳定排序、score 取 max、raw_score/raw_score_scaled 仅元数据显式给出才参与、kb_ids set 并集 sorted、metadata 首见固定、limit 真值链 topK→config.TopK→5 含负值切片语义] / ConfigManager[LoadFromFile(155300/155301 编译期死路径/155302)+SaveToFile(155303，JSON indent2 非 ASCII 不转义)+GetConfig 泛型+GetKnowledgeBaseConfig(155304)+UpdateConfig 按类型名存键；**键双轨偏差字面复刻**；缺省字段回落 pydantic 默认] / 伏笔回填：13.19 Filters 槽+WithRetrieveFilters(死参数)、13.26 断言缺口、13.22 MultiKBRetrievalResult 唯一生产、13.25 AgenticRetriever 唯一包装消费、13.2/13.3 DatabaseName getter 首次消费、8 错误码转消费。测试：Python 27 例 1:1 映射(SimpleKB 17/multiKB 4/ConfigManager 6)+Go 增强 22 项(懒创建三分支类型断言/mode 四档含空串/并发 -race/agentic max_iter 行为计数/Filters 透传与 nil/负 limit/同分保首见序/YAML int 与 JSON float64 双路径/JSON 非 ASCII/键双轨交叉验证)。覆盖率 knowledgebase 95.6% / utils 89.1%。设计：docs/superpowers/specs/2026-11-05-simpleknowledgebase-13.27-design.md（15 偏差 §8 + 6 伏笔 §10）` | `openjiuwen/core/retrieval/simple_knowledge_base.py + utils/config_manager.py` |
| 13.28 | ✅ | GraphKnowledgeBase | 图增强 KB（★13.17 TripleExtractor 的真实消费方）：`AddDocuments[155501/155502 + strict校验 + 空白id补uuid + chunk索引(kb_{id}_chunks,use_caption取配置) 假则155505 + use_graph&&extractor 时 Extract→triplesToTextChunks(metadata展开+triple/chunk_index/chunk_id 三键,chunk_id+doc_id 收窄字符串)→triple索引(kb_{id}_triples,use_caption硬编码true) 假则155506] / Retrieve[use_graph 三层判定(显式true或nil+配置) + ensureGraphRetriever 持锁懒创建(无store→155503)+六个WithGraph*首次接线+SetIndexType注入 + mode推导 + agentic包装(155209/155510) + graph_expansion/filters透传 + 非图回落内建SimpleKB(不传extractor/不传strict)] / DeleteDocuments[chunk必删+use_graph删triple,triple假只Warn不影响总结果] / UpdateDocuments[先删后加] / GetStatistics[11键含use_graph/triple_index_info/has_graph_retriever] / Close[基座→graph→chunk→triple,重复关闭字面复刻] + RetrieveMultiGraphKB/WithSource 薄委托 + kb_helpers.go 四函数下沉(默认配置/mode推导/database_name/agentic包装) + Task0 Close 签名统一为 Close() error(3接口+9实现+7桩,行为零变化：maybeClose 收敛单变体保留Warn吞)；覆盖率 96.6%，-race 干净；错误码 155505/155506 零消费转已消费；13.19 GraphHops"13.28接线后激活"注释纠偏为死参数；领域十三收口 | `openjiuwen/core/retrieval/graph_knowledge_base.py` |

**验证点**：✅ 外部文档可被解析、分块、嵌入、索引，并支持增删改查；✅ 检索查询管线闭环（向量/稀疏/混合/图/agentic 检索 + 多 KB 聚合）（13.19-13.28 已全部完成）

跨域联动：8.25 KnowledgeRetrievalComp ⤴️ 依赖本领域 13.26-13.28（三者已完成，可直接消费 `retrieve_multi_kb_with_source` / `RetrieveMultiGraphKBWithSource`，按 use_graph 二选一构造 SimpleKB/GraphKB）；7.36 行 ⤵️ 缺口清单指向领域十三（13.19-13.28 已完成，覆盖完整写入+读取管线）

---

## 领域十四：devtools 开发者工具面

> 与运行时会话链路（Gateway/E2A/AgentServer/DeepAgent）平行的开发者工具面，进程内直接调用，不经过会话链路。agent_builder 为核心子模块（NL→Agent DSL 生成器）。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **14.x 基础层** | — | | | |
| 14.1 | ✅ | devtools 包骨架 + 共享 utils | `internal/agentcore/devtools/agentbuilder/utils/`：7 函数（ExtractJSONFromText/FormatDialogHistory/SafeJSONLoads三层语义/MergeDictLists原地语义+isTruthy/LoadJSONFile四分支/DeepMergeDict死函数实现/ValidateSessionID）+ 20 常量（13 死常量全量对齐）+ 62 子用例（36 unit+3 system+5 LoadJSONFile增强+1 SafeJSONLoads空串nil边界+21.5 constants；JSON number→float64 断言）+ 错误码 188002/153003 消费（BuildError+WithMsg/WithDetails/WithCause/WithParam 选项）+ 正则 (?s) 翻译 Go/Python 双端 9 边界用例交叉验证 + ValueError(顶层非dict)走 except 包装路径对齐；覆盖率 91.0%；设计：docs/superpowers/specs/2026-10-25-devtools-utils-14.1-design.md | `openjiuwen/dev_tools/agent_builder/utils/` |
| 14.2 | ✅ | 枚举层 | `internal/agentcore/devtools/agentbuilder/utils/enums.go`：AgentType(2)/BuildState(3)/ProgressStage(15,三注释组)/ProgressStatus(5) 四个 type X string + 25 常量 + ParseAgentType(唯一真实消费者,140032 入口,错误含合法值清单)/ParseBuildState/ParseProgressStage/ParseProgressStatus(语言特性级等价物) + AllAgentTypes(supported_types 消费)/AllBuildStates/AllProgressStages/AllProgressStatuses(可迭代语义)；AgentTypeLiteral 零 Go 对应物(显式偏差)；enums_test.go 统一收口 12 unit 1:1 + system 回填 **4**+混合半边(勘误:14.1 归属拆分算式少算 1,正确 12=3+1+4+4) + Parse×3 正反例+All×4 声明序增强；包覆盖率 97.2%(基线 96.7%)；设计：docs/superpowers/specs/2026-10-26-devtools-enums-14.2-design.md | `openjiuwen/dev_tools/agent_builder/utils/enums.py` |
| 14.3 | ✅ | 进度上报 | `internal/agentcore/devtools/agentbuilder/utils/progress.go`：ProgressStep/BuildProgress 值类型(ToDict×2,RFC3339Nano)+ProgressReporter(方法级锁+AddCallback 返回取消函数+Start/Update/Complete/Fail/Warn Stage+隐性语义保真:自动收尾仅前序非 INITIALIZING/删键计时/update·warn 不建新 step/fail 双写 error)+ProgressManager(RWMutex,两锁不嵌套)+GetProgressManager 单例(sync.Once)+llmAgent/workflow 双进度映射表+11 个 With 薄构造器；progress_test.go 15 unit 1:1+system 回填 4(⤴️ 清偿,utils 系 system 12/12 全清)+19 并发/语义增强+1 UTC 时间戳语义(两处断言错误经 Python 语义实测修正)；包覆盖率 99.1%(基线 97.2%),-race 全过,progress.go 函数级 100%；审查补丁:8 处 time.Now()→time.Now().UTC()(2026-10-26 审查差异 1,对齐 Python datetime.now(timezone.utc),ToDict 输出 Z/+00:00 对齐 isoformat)；设计：docs/superpowers/specs/2026-10-26-devtools-progress-14.3-design.md | `openjiuwen/dev_tools/agent_builder/utils/progress.py` |
| **14.x agent_builder** | — | | | |
| 14.4 | ☐ | resource 层 | PluginProcessor + plugins.json 内嵌 + ResourceRetriever LLM 检索 | `openjiuwen/dev_tools/agent_builder/resource/` |
| 14.5 | ☐ | executor 层 | HistoryManager + createCoreModel + AgentBuilderExecutor | `openjiuwen/dev_tools/agent_builder/executor/` |
| 14.6 | ☐ | builders 基座 | BaseAgentBuilder 三态模板方法 + Factory 注册机制 | `openjiuwen/dev_tools/agent_builder/builders/base.py + factory.py` |
| 14.7 | ☐ | llm_agent 链 | prompts 中英提示词 + Clarifier + Generator + Transformer + IntentionDetector + template | `openjiuwen/dev_tools/agent_builder/builders/llm_agent/` |
| 14.8 | ☐ | workflow 链 | builder + dl_generator + dl_reflector + dl_assets + cycle_checker + dl_transformer + workflow_designer（~4000 行最大块） | `openjiuwen/dev_tools/agent_builder/builders/workflow/` |
| 14.9 | ☐ | AgentBuilder 门面 | build_agent/get_progress/map_state_to_status | `openjiuwen/dev_tools/agent_builder/main.py` |
| 14.10 | ☐ | 测试对齐 | 36 system + 54 unit Python 测试转 Go，闭环 A-4 缺口 | `tests/*_tests/dev_tools/` |
| **14.x 其余模块（处置待裁）** | — | | | |
| 14.11 | ⤵️ | prompt_builder | 处置待裁（依赖就绪可正常实现） | `openjiuwen/dev_tools/prompt_builder/` |
| 14.12 | ⤵️ | skill_creator | 处置待裁（依赖 9.34 sys_operation LOCAL + 技能包 Python 脚本承载方案待设计） | `openjiuwen/dev_tools/skill_creator/` |
| 14.13 | ⤵️ | skill_evaluator | 处置待裁（同 14.12 依赖） | `openjiuwen/dev_tools/skill_evaluator/` |
| 14.14 | ⤵️ | tune 旧版调参 | 处置待裁（与 agent_evolving 9.70-9.72 功能重叠，倾向跳过） | `openjiuwen/dev_tools/tune/` |

**验证点**：devtools agent_builder 工具面可用：NL 多轮对话 → 澄清 → DSL 产出

---

## 上游同步扩充说明（领域十五 ~ 二十九）

> **来源**：`docs/sync/README.md`（上游 jiuwenswarm `a2abceccf` / agent-core `5a35c39f` 快照）与两份提交清单。
> **分工**：本节只收「Go 完全没有对应实现」的新模块与新能力；已迁移子系统的行为缺陷不进本节，见 [`docs/sync/defect-plan-2026-11-04.md`](docs/sync/defect-plan-2026-11-04.md)。
> **状态约定**：全部标 ☐ —— 但每个步骤**开工前必须先核查 Go 是否已有部分实现**（上游同一能力常有多次提交，早期片段可能已在领域一~十四落地）；核实后把已具备的部分回填到对应旧章节并注明 ⤴️。
> **优先级建议**：十五（协议层，被 I/H 组缺陷与后续所有外部 agent 能力依赖）→ 十九/二十一（安全与运行形态）→ 二十二（可观测性，E2E 断言依赖）→ 十六/十七/十八（体量最大的新模块）→ 二十/二十三/二十四/二十五 → 二十六/二十七/二十八/二十九。

---

## 领域十五：第三方 Harness 协议与外部 Agent 接入

> 上游把第三方 CLI agent（Claude Code / Codex / DeepSeek Harness / Gemini）从"团队内部特例"提升为协议层：agent-core 新增 `harness_protocol`（传输契约）与 `harness_providers`（provider 实现），jiuwenswarm 增补 external-cli 配置面板与 runtime manifest 安装。
> Go 现状：只有 `internal/agentcore/harness`（内部 rail/DeepAgent）与 `internal/swarm/agents/harness`（原生团队 harness），**无协议层、无外部进程 provider**。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 15.1 | ☐ | 协议数据模型 | `HarnessModel`/`ToolSpec`/`ToolResult`/`State`/`Result`/`Error` 与 JSON 编解码容错（未知字段保留） | `internal/agentcore/harness_protocol/` ← `openjiuwen/harness_protocol/{models,tools,results,state,errors}.py` |
| 15.2 | ☐ | 事件与流 | 事件 kind 全集、增量流（partial/message/tool_call/tool_result）、终止语义、顺序保证 | 同上 ← `harness_protocol/{events,stream}.py` |
| 15.3 | ☐ | 交互与钩子 | `Interaction`（审批/提问）、`Hook`、按 kind 应答、应答只回给发起方 | 同上 ← `harness_protocol/{interactions,hooks}.py` |
| 15.4 | ☐ | 检查点与序列化 | checkpoint 结构、跨进程序列化（`jsonsafe`）、会话 ID/轮次锚定 | 同上 ← `harness_protocol/{checkpoints,serialization}.py` |
| 15.5 | ☐ | provider 基座与工厂 | `BaseProvider`、工厂注册（entry-point 等价：编译期注册表）、`inputs`/`io_adapter`、`mcp_naming`、`stream` 适配 | `internal/agentcore/harness_providers/` ← `harness_providers/{base,factory,inputs,io_adapter,mcp_naming,stream,jsonsafe}.py` |
| 15.6 | ☐ | native provider | 把 **in-process DeepAgent** 适配到协议表面（公开 harness protocol），支持 in-process dispatch；消费 **L1 交互面** `DeepAgent.start/attach_output/send_input`（9.87 已落）——⚠️ 上游 `provider="native"` 用的是 plain `DeepAgent`（`harness_providers/native/harness.py`:298/359/363），**不是** NativeHarness；原描述"把现有 NativeHarness 适配…"取自文件级措辞，易误读。`native_v2`（NativeHarnessProtocolAdapter + checkpoint + pause-resume）见 **25.1f** | 同上 ← `harness_providers/native/` |
| 15.7 | ☐ | claude code provider | stdio 子进程/SDK、session resume、in-process MCP、permission prompt 工具、请求 span 配对、失败分类 | 同上 ← `harness_providers/claudecode/` |
| 15.8 | ☐ | codex provider | 动态工具、审批默认绕过、host prompt 追加、压缩开关、PID 探测（Windows 安全）、team tools 走动态工具 | 同上 ← `harness_providers/codex/` |
| 15.9 | ☐ | dsh provider | DeepSeek Harness SDK 接入 + MCP + 认证回退 | 同上 ← `harness_providers/dsh/` |
| 15.10 | ☐ | telemetry 桥 | 第三方模型请求作为协议事件上报（破坏性语义）、tool facts 取 provider 自身遥测、auth 重试可见 | 同上 ← `harness_providers/telemetry/` |
| 15.11 | ☐ | 轨迹适配 | provider 事件 → 统一轨迹 span（与领域二十二 schema 对齐） | 同上 ← `harness_providers/trajectory.py` |
| 15.12 | ☐ | skills 与工作区约定 | portable skills 复制、`cwd/workspace/project_root` 三分离、外部成员 workspace meta 暴露 | 同上 ← `harness_providers/skills.py` · `openjiuwen/agent_teams/.../external_cli*` |
| 15.13 | ☐ | 外部 CLI 成员生命周期 | spawn 时校验 SDK/display_name、配置往返（config round-trip）、auth fallback、模型切换、失败诊断、会话清理 | `internal/agent_teams/runtime/externalcli/` ← `openjiuwen/agent_teams/`（external-cli 相关） · `jiuwenswarm/agents/.../external_cli*` |
| 15.14 | ☐ | AgentTemplate 清单 | manifest identity 扁平化、AgentTemplate card、异步加载器、团队成员快照加载 | `internal/agentcore/harness/template/` ← `openjiuwen/harness/template/`（template/plugin 系列） |
| 15.15 | ☐ | templates & plugins | 模板/插件的创建、加载、执行；插件热换模型后工具不消失 | `internal/agentcore/harness/plugin/` ← `openjiuwen/harness/...` |
| 15.15a | ☐ | 扩展热绑定（extension binder） | 类型层 `ExtensionParts`/`ResolvedSkill`/`ResourceRef`/`LoadRecord` + `extension_spec`（251 行）+ `extension_resolver`（317 行）+ `extension_binder`（472 行：tool/mcp/rail/prompt/skill/subagent 绑定 + `apply_extension_hot`/`unapply_extension_hot`）+ `DeepAgent` 的 `load_plugin`/`load_extension`/`unload_extension`/`_apply_extension_parts` 接线；首个真实消费方是包技能热绑（`_bind_skill`：leaf 目录取父级作 mount root → 追加 `rail.skills_dir` 并 reload → 失败回滚 → 追加 `DeepAgentConfig.Skills`）；⤵️ 落地后**回填 9.19-23 的 SkillUse**：`DeepAdapter.skillScanDirs` 数据源改为读 `DeepAgentConfig.Skills`（leaf→父目录归一、前置、去重），并删除来源错误的 `instanceOverrides["skills"]` 分支 | `internal/agentcore/harness/extension_binder.go` · `extension_resolver.go` · `schema/extension_spec.go` ← `openjiuwen/harness/extension_binder.py` · `resources/extension_resolver.py` · `schema/extension_spec.py` · `deep_agent.py`（load/unload 段） |
| 15.16 | ☐ | 团队集成 | `spawn_teammate` 合并 external_cli、调度模式内联成员跑外部 CLI、worktree 隔离、第三方面板配置 | `internal/agent_teams/spawn/` · `internal/swarm/server` ← `jiuwenswarm/gateway/channel_manager/web`（external-cli handlers） |
| 15.17 | ☐ | 测试 | 协议一致性单测（编解码/事件序）+ 假 CLI 进程集成（stdio 双工）+ 失败恢复（崩溃/鉴权失败/超时） | `tests/integration/agentcore/harness_protocol/` |

**验证点**：一次对话可由外部 CLI provider 完成，事件、工具结果、审批与轨迹四路可观测且可恢复。

---

## 领域十六：Symphony 技能交响（技能图谱与能力编排）

> 全新模块。agent-core `openjiuwen/symphony`（207 文件 / 5.2 万行）负责技能图谱、Agentic 检索、编排与经验沉淀；jiuwenswarm 侧负责 UI 事件、增量构建与"先检索后编排"工具链。
> Go 现状：零实现（`rg symphony internal/` 无命中）。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 16.1 | ☐ | 模型与接口层 | Skill/Capability/Graph/Recipe/Pack 数据结构、接口定义、JGF 序列化 | `internal/agentcore/symphony/{models,interfaces,shared}/` ← `openjiuwen/symphony/{models,interfaces,shared}/` |
| 16.2 | ☐ | 技能图谱存储与操作 | 节点/边 CRUD、原子批量变更、原子替换、按 module 分组索引 | 同上 ← `openjiuwen/symphony/graph_engine.py` |
| 16.3 | ☐ | 增量/全量构建 | 指纹与关系缓存、断点续构建、JSONL 记录保留、重建恢复、构建进度事件 | 同上 ← `openjiuwen/symphony/flow/` |
| 16.4 | ☐ | 技能发现与索引 | 目录扫描（Windows 路径/Unicode 名称）、禁用开关、索引与前端一致性、重建触发条件 | 同上 ← `openjiuwen/symphony/discovery/` |
| 16.5 | ☐ | 检索 | BM25 + embedding 混合、Agentic 检索（先检索后编排）、依赖感知检索、集群模式不全量注入 | 同上 ← `openjiuwen/symphony/retrieval/` · `openjiuwen/symphony/agent/` |
| 16.6 | ☐ | 编排 | `symphony_compose_graph` 工具、双向束搜索、候选补齐、HITL 追问、超时与预算、失败透传 | 同上 ← `openjiuwen/symphony/orchestration/` |
| 16.7 | ☐ | 执行运行时 | 算子执行、并行、阶段/进度事件、runtime 生命周期 | 同上 ← `openjiuwen/symphony/runtime.py` |
| 16.8 | ☐ | 经验沉淀 | experience 模块：轨迹→经验→SkillPack、经验蒸馏、执行证据驱动图谱更新、经验复用跨会话/跨技能 | 同上 ← `openjiuwen/symphony/experience/` |
| 16.9 | ☐ | 评估 | 能力指纹、两两 Skill 等价分组、SkillPack 成员校验与历史 recipe 兼容 | 同上 ← `openjiuwen/symphony/evaluation/` |
| 16.10 | ☐ | 观测 | 构建进度、耗时、失败原因、trace 关联 | 同上 ← `openjiuwen/symphony/observation/` |
| 16.11 | ☐ | 与 AgentServer / Web 集成 | 工具暴露（单 agent / team / swarm 模式）、图谱事件 → web 团队事件、JGF 流程图输出、技能总谱页签 | `internal/swarm/server` · `internal/swarm/channels/web` ← `jiuwenswarm/server/...symphony*` · `jiuwenswarm/channels/web` |
| 16.12 | ☐ | 测试 | 图谱构建（含坏技能隔离）、增量等价于全量、检索质量基线、编排产物断言、HITL 往返 | `tests/integration/agentcore/symphony/` |

**验证点**：给定技能库，可增量构图、按需求检索并编排成图执行，执行经验可沉淀回图谱。

---

## 领域十七：RSI 递归自我改进

> 全新模块。`openjiuwen/rsi`（363 文件 / 9.7 万行）：artifact 优化服务流、证据驱动 harness 演进、PUCT（对齐 `agentdescent==0.4.6` 内部实现）、评审与两阶段晋级。jiuwenswarm 侧提供实验开关、任务生命周期与 AgentServer 服务域。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 17.1 | ☐ | 基础 schema / 事件 / 用量 | `rsi` 数据契约、事件 kind、token 用量累计（缺计数时保留累计值） | `internal/evolving/rsi/{schema,events,usage}.go` ← `openjiuwen/rsi/{schema,events,usage}.py` |
| 17.2 | ☐ | artifact 优化服务流 | artifact 提供方契约、优化包安装/浏览/预览、文件与版本管理、任务内生命周期 | 同上 ← `openjiuwen/rsi/artifact_rsi/` |
| 17.3 | ☐ | PUCT program artifact provider | PUCT 搜索（对齐 agentdescent 语义）、候选打分、solved 阈值提前停止 | 同上 ← `openjiuwen/rsi/artifact_rsi/` |
| 17.4 | ☐ | harness 演进 | 证据驱动 harness 演进、单 harness 引擎事件、epoch 树、非回归门（全局）、连续两阶段评分 | 同上 ← `openjiuwen/rsi/harness_rsi/` |
| 17.5 | ☐ | paper pipeline | paper 上传/评分/评审证据链、报告渲染（tex/pdf）、宽松校验 | 同上 ← `openjiuwen/rsi/`（paper 系列） |
| 17.6 | ☐ | 评估 agent | LLM judge（评分规则/自然语言 rubric）、JSON verdict 恢复、嵌套证据、并发与请求预算 | 同上 ← `openjiuwen/rsi/`（evaluator 系列） |
| 17.7 | ☐ | 任务生命周期 | 取消/终结持久化、资源清理隔离、Windows 路径预算、协作式终止 | 同上 |
| 17.8 | ☐ | 技能与插件 | native skill rails 准备、plugin 注册与加载、技能查询入口 | 同上 |
| 17.9 | ☐ | AgentServer 服务域 | RSI 服务端入口、调度、网关注册、实验特性开关、前端契约（节点进度/失败原因） | `internal/swarm/server/runtime/rsi/` ← `jiuwenswarm/server/...rsi*` |
| 17.10 | ☐ | 测试 | 引擎事件序、晋级门拒绝路径、judge 解析容错、取消后不残留 | `tests/integration/evolving/rsi/` |

**验证点**：给定基线 harness 与任务集，可产出候选 artifact、评分、晋级或拒绝，全过程事件可回放。

---

## 领域十八：CUA 桌面自动化引擎

> 全新模块。`jiuwenswarm/cua_engine`（259 文件 / 1.48 万行）：原生协议 + 三平台驱动 + 无障碍观察 + MCP 运行时，作为子代理接入。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 18.1 | ☐ | 协议与类型 | 原生协议、类型、错误、身份、路径解析 | `internal/agentcore/cua/{protocol,types,errors}.go` ← `jiuwenswarm/cua_engine/src/computer_use/{protocol,types,errors,_identity,paths}.py` |
| 18.2 | ☐ | 驱动执行器 | 子进程驱动协议、超时、退出码与错误映射 | 同上 ← `cua_engine/src/computer_use/{driver_exec,execution}/` |
| 18.3 | ☐ | macOS 驱动 | 基础对象与窗口边界、界面读取、输入控制、视觉反馈 | 同上 ← `cua_engine/src/computer_use/`（macos 驱动） |
| 18.4 | ☐ | Windows 驱动 | 界面读取（UIA）与输入控制 | 同上 ← `cua_engine/src/computer_use/`（windows 驱动） |
| 18.5 | ☐ | 无障碍观察 | 观察树构建、展示/裁剪、与模型视图的映射 | 同上 ← `cua_engine/src/computer_use/observation/` |
| 18.6 | ☐ | MCP 运行时 | 内置 MCP 命令集、native semantics | 同上 ← `cua_engine/src/computer_use/{mcp,native_semantics}.py` |
| 18.7 | ☐ | 后端与沙箱 | backends 适配（本地 / jiuwenbox） | 同上 ← `cua_engine/backends/` |
| 18.8 | ☐ | 桌面技能包 | skills 内容与安装位置 | 同上 ← `cua_engine/skills/` |
| 18.9 | ☐ | 子代理接入 | cua 子代理注册、工具暴露、与 deep agent 协同、失败回收 | `internal/agentcore/harness/subagents/cua.go` ← `openjiuwen/harness/subagents/`（cua 相关） |
| 18.10 | ☐ | 测试 | 三平台单测（驱动可 fake）、协议往返、失败降级 | `tests/integration/agentcore/cua/` |

**验证点**：在 macOS/Windows 上完成一次「读屏 → 决策 → 点击/输入 → 校验」闭环。

---

## 领域十九：AgentOS 运行形态与独立 Gateway / 企业运行时

> 上游将 `gateway/` 从包内提升为独立可发布包（510 文件 / 13.5 万行），并新增 AgentOS（YuanRong）沙箱、registry、ADX、SSH、企业配额等运行时能力。Go 侧 `internal/swarm/gateway` 已存在，但缺少 AgentOS 与企业分支能力。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 19.1 | ☐ | gateway 协议解耦 | gateway-protocol 外部契约、E2A/接口依赖改造、路径类依赖收敛 | `internal/swarm/gateway/protocol/` ← `gateway/`（protocol 相关） |
| 19.2 | ☐ | 鉴权 | IAM、token、HttpOnly cookie、OAuth、Web/TUI 同源鉴权、鉴权关闭时 X-User-Id 回退 | `internal/swarm/gateway/auth/` ← `gateway/auth/` |
| 19.3 | ☐ | registry 客户端 | 注册/注销、地址快照、CAS 清理陈旧沙箱、空闲 agent 注销 | `internal/swarm/gateway/registry/` ← `gateway/`（registry） |
| 19.4 | ☐ | AgentOS Router | `3rdagent.switch`、SSH 南向路由、channel 端口分配、3rd-agent Web UI 代理、ADX 路由 | `internal/swarm/gateway/agentos/` ← `gateway/`（agentos router） |
| 19.5 | ☐ | YuanRong 沙箱生命周期 | create/delete（inline runtime_spec）、预热、空闲回收、函数入口 loop 统一、失败清理 | `internal/swarm/gateway/agentos/yuanrong/` ← `jiuwenswarm/agents/...` · `gateway/` |
| 19.6 | ☐ | SSH 通道 | 临时密钥签发、sshd 就绪探测、user_id 透传、断连清理可配置超时 | 同上 ← `gateway/channel_manager`（ssh） |
| 19.7 | ☐ | ADX ingress | 一体机部署入口、经 Environment API 路由 agent | `internal/swarm/gateway/adx/` ← `gateway/`（adx） |
| 19.8 | ☐ | etcd 配置下发 | config_poll 轮询、agent/tool 沙箱设置、多节点仅管理节点启动 | `internal/swarm/gateway/config_poll/` ← `gateway/config_poll/` |
| 19.9 | ☐ | 企业运行时 | team quota、membership、control store、路由绑定、企业特性握手、连接器广场 | `internal/swarm/gateway/enterprise/` · `internal/swarm/extensions/enterprise/` ← `jiuwenswarm/enterprise/` · `gateway/enterprise/` |
| 19.10 | ☐ | 部署形态 | systemd / nohup 回退、分布式多机、gateway 单实例锁、一键部署 DAG、源码修订暴露 | `packaging/` · `cmd/` ← `jiuwenswarm/start_services.py` · `Makefile` · `Dockerfile.claw` |
| 19.11 | ☐ | 测试 | 鉴权矩阵、registry 生命周期、沙箱创建失败回滚、多节点配置一致性 | `tests/integration/swarm/gateway/` |

**验证点**：网关在无本地沙箱的部署形态下可拉起远端 AgentOS 沙箱并完成一次对话与文件读写。

---

## 领域二十：Process-CLI 与 Runtime SDK

> 上游新增"单 Agent 一次性机器执行"入口（`channels/process_cli`，30+ 文件）与 Runtime Public API，配套 Python/TypeScript SDK，用于 CI/脚本化调用。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 20.1 | ☐ | Runtime Public API | agent_definition、run_context、session_catalog、model_catalog、permission_catalog、tool_allowlist、mode_catalog、cron；⤴️ **回填 10.3.28d 的 R-4**：`_agent_execution_owners` 执行 owner 表（`service.py:423-426`）及其四方法 `_agent_execution_owner_key`(:1584)/`_claim_agent_execution_owner`(:1593)/`_agent_execution_owner`(:1620)/`_forget_agent_execution_owner`(:1627)，随 `invoke_agent`(:1642)/`stream_agent`(:1665) 与 `RuntimeAgentDefinition`/`RuntimeAgentExecution`（`runtime/agent_definition.py` 311 行）一并落地；三个读端在 `answer_interaction`(:1995)/`answer_interaction_input`(:2025)/`stream_interaction_answer`(:2083) 透传 `_agent_execution` | `internal/swarm/runtime/` ← `jiuwenswarm/runtime/*.py` |
| 20.2 | ☐ | 一次性执行协议 | machine_entry/machine_io/machine_policy/machine_result/machine_signals、JSON 契约、退出码契约 | `internal/swarm/channels/processcli/` ← `jiuwenswarm/channels/process_cli/*.py` |
| 20.3 | ☐ | REPL 与命令 | repl、commands、control_commands、ui、render、model_options、prompt | 同上 |
| 20.4 | ☐ | 双工与实时输入 | duplex_protocol/duplex_input/duplex_control、live_input/live_layout、steer 绑定当前执行 | 同上 |
| 20.5 | ☐ | 会话与持久化 | session_entry/session_guard/persistence、跨进程续聊、同会话并发互斥、退出前落盘 | 同上 |
| 20.6 | ☐ | 宿主工具回调 | host_tools、client_tools（客户端工具暂停-回填）、client-attachments | 同上 ← `jiuwenswarm/runtime/client_tools.py` |
| 20.7 | ☐ | 权限与策略 | machine_policy、非交互拒绝授权、shell 不得绕过文件授权、运行级能力控制 | 同上 ← `channels/process_cli/{permissions,machine_policy}.py` |
| 20.8 | ☐ | SDK | Python SDK + TypeScript SDK（会话/模型/规划/状态/权限命令、事件流） | `sdks/` ← `jiuwenswarm/sdks/` |
| 20.9 | ☐ | 诊断与输出 | 非交互输出精简、诊断日志落盘、思考分段不重复 | 同上 ← `channels/process_cli/diagnostics.py` |
| 20.10 | ☐ | 测试 | 一次性执行端到端（真子进程）、跨进程续聊、并发拒绝、退出码断言 | `tests/integration/swarm/processcli/` |

**验证点**：`uapclaw exec --json` 可一次性完成单 agent 任务并输出结构化结果，SDK 可复用同一会话续聊。

---

## 领域二十一：权限模式与智能审批

> 上游权限体系经历"加入→整体回滚→重做"：最终形态是权限引擎与产品权限模型解耦、三档模式（Full Access / Auto / Strict）、三层规则（Global/User/Session）、智能审批队列与 A4P 预授权。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 21.1 | ☐ | 权限引擎解耦 | engine 与 product model 分离、目录重构、`SessionPermissionStore` | `internal/agentcore/harness/security/permission_engine/` ← `openjiuwen/harness/security/permission_engine/` |
| 21.2 | ☐ | 权限模式 | Full/Auto/Strict 枚举、会话级持久化、网络保护、模式切换 RPC | 同上 ← `permission_engine/`（modes） |
| 21.3 | ☐ | 规则层次 | 全局/用户/会话三层、规则落盘去重、路径抽取修正、"仅本次生效" | 同上 ← `permission_engine/`（rules） |
| 21.4 | ☐ | 智能审批 | 规则放行判定、审批事务、主 agent 审批队列、快照与工作目录对齐 | `internal/swarm/server/permissions/` ← `jiuwenswarm/agentserver/...permissions*` |
| 21.5 | ☐ | 审批交互 | WebUI 卡片（title/body 一致）、Code 模式审批、控制权交接与恢复、拒绝只生效一次 | 同上 · `internal/swarm/channels/web` ← `jiuwenswarm/channels/web` |
| 21.6 | ☐ | guardrail 收编 | 命令行安全检查统一入口、bash 复合命令、通配非 ASCII、完整命令展示、内置规则生效 | `internal/agentcore/harness/rails/security/` ← `openjiuwen/core/security/guardrail/` · `harness/rails/security/` |
| 21.7 | ☐ | A4P 预授权 | 会话/定时任务预授权、无需新增授权的申请过滤、read scope 校验 | `internal/swarm/server/a4p/` ← `jiuwenswarm/agentserver/...a4p*`（外部 `a4p` 包） |
| 21.8 | ☐ | 隔离语义审查 | 安全审计、隔离语义审查、文件交付边界 | 同上 ← `openjiuwen/harness/security/` |
| 21.9 | ☐ | 团队权限 | Team 权限 rail、leader 仲裁 ASK、成员权限继承、项目目录透传 | `internal/agent_teams/rails/` ← `openjiuwen/agent_teams/`（permission） |
| 21.10 | ☐ | 测试 | 三模式 × 三层规则矩阵、审批中断恢复、绕过用例（大小写/复合/中文） | `tests/integration/agentcore/security/` |

**验证点**：三档模式下同一危险命令分别放行/弹窗/拒绝，策略可持久化且可审计。

---

## 领域二十二：可观测性与 Trajectory v2

> 上游 observability 与 trajectory 经历破坏性重构：span-level schema 统一版本、OTel/GenAI semconv 对齐、内容寻址归档 v2、团队泳道与 TTFT/TTFB。
> Go 现状：`internal/evolving/trajectory` 与 `internal/agent_teams/observability` 为基线版本，无 OTel 全局埋点与归档 v2。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 22.1 | ☐ | OTel 全局埋点 | httpx/requests/aiohttp/fastapi 自动埋点、W3C traceparent 传播、进程级开关 | `internal/common/otel/` ← `openjiuwen/core/...otel*` · `openjiuwen/extensions/observability/` | ⤴️ **回填 10.3.28c 的 R-8**：轨迹库删除钩子 `BeginSessionDelete`/`AbortSessionDelete`/`CommitSessionDelete`（现为 `noopTrajectoryHook`）；⤴️ **回填 10.3.28c 的 R-8**：轨迹库删除钩子 `BeginSessionDelete`/`AbortSessionDelete`/`CommitSessionDelete`（现为 `noopTrajectoryHook`，会话删除时不清轨迹库） |
| 22.2 | ☐ | OTLP 文件导出 | 标准 OTLP JSON 文件 exporter、枚举编码为整数、属性截断策略 | 同上 ← `openjiuwen/harness/observability/` |
| 22.3 | ☐ | span 层级 | agent → round → llm/tool → subagent、team root、成员工具 span、按 session 注册根 span | `internal/agentcore/harness/observability/` · `internal/agent_teams/observability/` ← 同上  **⤴️ 25.1e 已提供**：`NativeHarness.ActiveRound()` 访问器与 `MemberTurn`（`openjiuwen.turn.id/number` 可直接映射）。**⚠️ 前置障碍**：`runRound` 的回调上下文 agent 是 `*agentcore.DeepAgent`（Python 传 `self`=NativeHarness），观测 rail 拿不到 harness 身份；映射成员 turn 属性前须先解决该身份来源 |
| 22.3a | ☐ | 观测 session 绑定 | ⤴️ 9.87 D12b：`DeepAgent.start()` 的 `set_current_session_id(sid)`（`span_context.py` L542，调用点 `deep_agent.py` L3962）未移植——Go 无 root span 注册表，照搬 setter 只会得到无人读取的字段（违反规则 8）。既定选项甲（先落地且带真实消费者）：`observability.WithCurrentSessionID` / `CurrentSessionID` / `ClearCurrentSessionID`（ctx 级，等价 `_current_session_ctx`）→ `Start` 里挂载并让 `attachRunContexts` 继承 → `OtelCallbackHandler.OnAgentInvokeInput` 给 agent span 打 `session.id` 属性（semconv 新增 key）。root 注册表 / `get_root_span` / request 序号兜底归 22.3 本体 | `internal/agent_teams/observability/` ← `openjiuwen/extensions/observability/span_context.py`(L518/L542/L750) · `callback_handler.py`(L2132) |
| 22.4 | ☐ | GenAI 属性 | 请求/响应消息、工具参数与结果、cache usage、retrieval 属性、压缩提示词不入对话链 | 同上 |
| 22.5 | ☐ | 时延指标 | TTFT（仅 token chunk）/TTFB/inter-token → usage_metadata；未观测请求标记 | 同上 ← `harness/observability/` |
| 22.6 | ☐ | trajectory schema v2 | 单一 span-level 版本、事件 kind 闭集、epoch 基线、restated 内容按内容寻址共享 | `internal/evolving/trajectory/` ← `openjiuwen/agent_evolving/trajectory/` |
| 22.7 | ☐ | 归档 v2 | 内容寻址归档、分页保留 + checkpoint、JSONL zip 导入导出、超期清理 | 同上 |
| 22.8 | ☐ | 上下文链重建 | 压缩按 step 归属、reasoning 按 `reasoning_content`、跳过 compaction 轮次、离线演进/RL 消费统一 | 同上 |
| 22.9 | ☐ | 团队视图 | 成员泳道、定时调度轨迹、token 成本时间线、诊断页面契约 | `internal/swarm/channels/web/trajectory/` ← `jiuwenswarm/channels/web`  **⤴️ 25.1e/24.14 已提供数据源**：`context.usage` 快照现带完整归属（`agent_id`/`team_id`/`member_name`/`agent_path`/`depth`/`invocation_id`/`delegation_id`/缓存身份），成员成本分摊可直接按 `member_name`+`agent_path` 聚合；`post_compact` 路径的归属仍缺（spec D-8） |
| 22.10 | ☐ | 归因与奖励 | attribution：Fast Reward、交付物选择、证据链、任务解析、字段窗口工具 | `internal/evolving/attribution/` ← `openjiuwen/agent_evolving/attribution/` |
| 22.11 | ☐ | 测试 | span 树断言、归档 round-trip、TTFT 语义、导入非法归档报错 | `tests/integration/evolving/trajectory/` |

**验证点**：一次团队会话产出可导入导出的 v2 轨迹，span 树与 OTLP 导出可被标准工具消费。

---

## 领域二十三：LLM 协议与多模态

> 上游 LLM 层新增 Anthropic 客户端、Responses API、thinking/reasoning effort、模型组路由、显式缓存、OpenAI 账号 OAuth；多模态从"图片开关"扩展为按模型声明能力。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 23.1 | ☐ | Anthropic 客户端 | Messages API、thinking、mid-conversation system turns、温度与 top_p 互斥、max_tokens | `internal/agentcore/foundation/llm/model_clients/anthropic/` ← `model_clients/anthropic_model_client.py` |
| 23.2 | ☐ | Responses API | api_key 配置走 Responses、流式 function arguments、usage reasoning_tokens | 同上（openai 客户端 extensions） ← `model_clients/openai_model_client.py` |
| 23.3 | ☐ | 思考与推理强度 | `thinking` 开关、`reasoning_effort`/`extra_body` 透传、DeepSeek V4 参数注入、Zhipu 禁用思考兼容 | 同上 ← `model_clients/` · `jiuwenswarm/llm_provider_compat_patch.py` |
| 23.4 | ☐ | 模型组与路由 | 模型组配置与策略、团队成员指定模型、路由模型命名、失败回退与日志 | `internal/agentcore/foundation/llm/model_group/` ← `openjiuwen/core/foundation/llm/`（model group） |
| 23.5 | ☐ | 显式缓存 | OpenRouter anthropic/qwen 显式缓存、cache 创建用量解析 | 同上 ← `model_clients/openai_model_client.py` |
| 23.6 | ☐ | OpenAI 账号 OAuth | 设备码轮询与瞬态重试、鉴权方式选择 | 同上 ← `model_clients/openai_account_model_client.py` |
| 23.7 | ☐ | 能力声明 | 按模型声明 `supports_vision`、模态探测（超时/拒绝识别）、图像原生输入与 vision 工具解耦 | `internal/agentcore/foundation/llm/schema/` · `harness/tools/multimodal/` ← `openjiuwen/harness/`（modality probe） |
| 23.8 | ☐ | 多模态内容 | 图片附件、PDF 输入与内容分析、视频生成与聊天交付、附件作为系统/用户消息注入 | `internal/agentcore/harness/tools/multimodal/` ← `jiuwenswarm/agents/...` · `openjiuwen/harness/tools/` |
| 23.9 | ☐ | 全双工音视频 | TTS/ASR、语音-Agent 任务管理、vllm omni、双工配置 | `internal/swarm/extensions/duplex/` ← `jiuwenswarm/extensions/`（多模态音视频） |
| 23.11 | ☐ | application 层 LLMController（工作流恢复） | 普通查询恢复定位「首个被中断组件」（`_create_query_resume_input`：非 questioner 只喂首个组件，questioner 广播）；配套 `_find_interrupted_task`/`InteractiveInput.update` 恢复链 | `internal/agentcore/application/llm_agent/` ← `openjiuwen/core/application/llm_agent/llm_controller.py` |
| 23.10 | ☐ | 网关兼容 | SSE-only 网关的非流式 invoke、OpenAI 兼容层、错误码透传 | 同上 ← `jiuwenswarm/llm_sse_patch.py` |

**验证点**：同一套 agent 配置可切换到 Anthropic/Responses 模型并正确产出思考、用量与多模态输入。

---

## 领域二十四：Context 与 Memory 演进

> 上游上下文从"压缩器集合"扩展为"主动上下文 + 异步会话记忆 + 上下文演进（ContextEvolution/Metis/TTSE）"；记忆新增外部 provider（lakebase、jiuwen_memory 2.0）与 daily memory 注入。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 24.1 | ☐ | 主动上下文 | GitCode 服务拉取、规则式整理、采集进度、前端初始化 | `internal/agentcore/harness/personal_context/` ← `openjiuwen/harness/personal_context/` |
| 24.2 | ☐ | 异步会话记忆 | session memory processor 异步化、取消安全 | `internal/agentcore/context_engine/processor/` ← `openjiuwen/core/context_engine/` |
| 24.3 | ☐ | 上下文管理能力 | 集合管理、进度查询、上下文浏览 API | `internal/swarm/server` ← `jiuwenswarm/server` |
| 24.4 | ☐ | ContextEvolution + Metis | 语境演进框架、Metis 任务记忆、三级协作（l1-l3） | `internal/agentcore/context_evolver/`（9.82 已有 P1-P7 基础，本节补 Metis/l1-l3 与 quickstart rail） ← `openjiuwen/extensions/context_evolver/` · `core/operator/context_evolve_call/metis.py` |
| 24.5 | ☐ | TTSE | BM25/Chroma 索引持久化、consult retrieve 模式、embedding 不可用降级、规则归因 | `internal/evolving/ttse/` ← `openjiuwen/agent_evolving/ttse/` · `harness/rails/evolution/ttse_rail.py` |
| 24.6 | ☐ | 外部记忆 provider | lakebase、jiuwen_memory 2.0（可信身份）、mem2.0 契约、服务端与 SDK 行为统一 | `internal/agentcore/memory/external/` ← `openjiuwen/core/memory/external/` |
| 24.7 | ☐ | daily memory | 今日/昨日记忆自动注入系统提示、敏感信息过滤配置 | 同上 ← `openjiuwen/harness/rails/memory/` |
| 24.8 | ☐ | 记忆 rail 挂载 | JiuwenMemoryRail、coding memory 落工作区、team 记忆生成 | `internal/agent_teams/rails/memory/` ← `openjiuwen/harness/rails/memory/jiuwen_memory_rail.py` |
| 24.9 | ☐ | 测试 | 外部 provider 契约、降级路径、注入顺序、索引重建 | `tests/integration/agentcore/memory/` |
| 24.10 | ✅ | Prompt Attachment 地基（S1） | manager（存储/渲染/哈希增量/更新族）+ `ContextWindow` mutator 链 + ReAct 三处装订点 + provider 矩阵；7 例集成测试（IT-1~IT-7）；设计 `docs/superpowers/specs/2026-11-04-prompt-attachment-foundation-design.md` | `internal/agentcore/harness/prompts/attachment_*.go` ← `openjiuwen/harness/prompts/prompt_attachment_manager.py` |
| 24.11 | ✅ | Rail 生命周期与模式合并（S2） | 模式串三段化 + `internal/swarm/runtime`（请求模式解析/`PlanModeController`）+ adapter 幂等 `UpdateAgentRails` + plan 指引与 todo 改附件（`InjectPrompt` 默认 false）+ `CodeTaskPlanningRail` 与 code todo 文案；13 例集成测试（IT-S2-1~13）；设计 `docs/superpowers/specs/2026-11-04-s2-rail-and-mode-design.md` | `internal/swarm/runtime/` · `internal/swarm/agents/harness/code/{rails,tools}/` ← `jiuwenswarm/runtime/{plan,request}.py` · `jiuwenswarm/agents/harness/code/rails/code_task_planning_rail.py` |
| 24.12 | ✅ | Plan/Runtime 层对齐（S3） | `AgentRuntime`（`Invoke/Stream` 五步）+ `RuntimeEvent`（控制事件走 push）+ controller 运行期 `PlanStateAccessor` + `PrepareChatTurn`/`ResolveRequestProjectDir` 等 request.py 函数迁入 + `session.plan_status` RPC + `StreamKeepalive` + `interface.go`/`UapClawSwarm` 改名；IT 已迁到 AgentServer 入口并清掉测试侧 plan 模拟脚手架（`testhelpers.ServerEnv`） | `internal/swarm/runtime/{service,events,request,plan}.go` · `internal/swarm/server/{handle_message,handle_chat,handle_session_plan,keepalive}.go` · `internal/swarm/server/runtime/interface.go` ← `jiuwenswarm/runtime/{service,events,request,plan}.py` · `jiuwenswarm/server/agent_ws_server.py` |

| 24.13 | ✅ | mode 缺省语义对齐（0.2.5 + 规则 9） | 缺省/兜底 mode 一律不指向 plan（`getChannelDefaultState`/`channelModeFromStr`/`web._parse_mode`/ACP `msg.Mode`/网关回填）；`ChannelMode` 改字符串枚举 18 成员、`ParseChannelMode`→`channelModeFromStr`（补 debug/warn 日志）；`schema.ParseMode`→`ModeFromRaw(any, Mode)` + 9 个 legacy 成员 + `ToRuntimeMode` 折叠表；按 9.3 拆出 `handleModeSwitch`、`/switch` 内联回 `handleChannelControl`；`/mode` 白名单 25 项；未知 `team.*` chat.send 守卫（`UNKNOWN_TEAM_MODE`）；D1 渠道会话分配（`_allocate_channel_session`/`_remember_alloc_state_key`/`_ensure_inbound_session` + `_apply_channel_state` 两分支）；D2 notice 签名归位 + `Message.AppID/BotID`；✅ 2026-11-05 回填 `_resolve_external_channel_session`（a2a 协议会话→本地会话别名，`message_handler/channel_state.go`）；⤵️ 「非流式 team 拒绝」（`feishu_connect.py:3003-3013` 拦截 `/mode team` 与 `/join`，回卡片提示）属**飞书通道**内部逻辑，本仓无 `im_platforms/feishu`，随 26.1 IM 平台移植，不在 24.13。设计：docs/superpowers/specs/2026-11-04-mode-default-alignment-design.md | `internal/swarm/schema/mode.go` · `internal/swarm/gateway/message_handler/{channel_state,slash_cmd,forward_loop}.go` · `internal/swarm/gateway/{gateway_server,acp_bridge}.go` · `internal/swarm/gateway/normalize.go` ← `jiuwenswarm/gateway/message_handler/message_handler.py` · `jiuwenswarm/common/schema/message.py` · `jiuwenswarm/gateway/app_gateway.py` · `jiuwenswarm/gateway/channel_manager/{web,protocol/acp}/*.py` |

| 24.14 | ✅ | 上下文用量分析子系统（B-06 + B-10） | `context_engine/usage/` 整体迁移：`models`（`context-usage.v1` 快照 + category/part/KV cache 模型）、`provider_usage`、`analyzer`（parts 拆分 / provider 权威总量 / 差额按比例归因 / `pre_call`·`post_call`·`post_compact`）、`session_aggregator`；消息级 `usage_metadata` 基线 + 五处失效点 + 纯 append 前缀保留；`stream_event_emit` 自拼的 `{rate,context_max,tokens_used}` 载荷改发完整快照（前端 `contextUsageModel.ts:36-80` 已按 v1 校验，现载荷被判非法丢弃 → 用量指示器失效）。设计：`docs/superpowers/specs/2026-11-06-context-engine-b-group-alignment-design.md` §9；⤵️ 遗留（25.1a 核查发现）：团队成员**用量归属字段** `_usage_build_context`/`_usage_agent_id`（Python 挂在 react_agent 上，见 `native_harness.py`:182-187）Go 全仓 0 命中，需在本子系统补。**2026-11-10 已闭环**：归属链整体落地——`single_agent/rail/usage_attribution.go`（ctx 绑定 + `BuildUsageDelegationAttribution` 十键）、`InvokeInputs` 六字段 + `AgentCallbackContext.ContextUsageAttribution`、`agents/react_usage_attribution.go`（`_context_usage_attribution` 一比一）、`NativeHarness` 构造期 `SetUsageIdentity`、`usage/analyzer.go` 的 `Attribution` + `applyAttributionFields`（14 键）、快照补 9 字段、三处委派绑定（ability_manager/task_tool/session_spawn_executor）+ spawn 任务 metadata 四键；遗留 D-6~D-9（死字段不移植、post_compact 无归属、env 注入缺会话准备路径）见 spec §4。设计：`docs/superpowers/specs/2026-11-10-25.1e-turn-and-usage-attribution-design.md`。**2026-11-10 二次对齐（按 Python 逐项对齐，非按 Go 现状补）**：①`internal/agentcore/kv_cache/`（`KVCacheIdentity` + 两个亲和 env 键 + `TeamMemberCacheIdentity`，运行期 ⤵️ 未迁）；②`Session` 补血缘与缓存身份（`parentSessionID`/`teamCacheScope`/`GetParentSessionID`/`BindParentSessionID`/`SetTeamCacheScope`/`GetCacheIdentity`，对齐 `core/session/agent.py` L62-174）；③`AgentTeamSession.CreateAgentSession` 补 `memberName` → `source_member_name` + `SetTeamCacheScope`（对齐 `agent_team.py` L164-188）；④`DeepAgent.prepareSingleRoundSession` 按 `_prepare_single_round_session` 移植（含父会话 env 注入与 `ownsSession` 的 post_run 收尾），ReAct 的 `invoke`/`stream` 自建会话补 `WithCard` + 父会话 env；⑤五个 `_usage_*` 字段与 `ModelCallInputs.ContextUsageAttribution` 按 Python 声明并在回退链读取（永不赋值，与上游同）；⑥归属改加锁盒子 + `UpdateUsageAttribution` 原地刷新（替代 ContextVar 的 dict 原地更新）；**⑦步骤核对收口**：补 `member_name` 第四级回退 `getattr(self, "member_name", None)`（Python L1370-1371，落成 `ReActAgent.usageMemberName`，默认恒空）与 `_attribution_fields` 的"键存在即覆盖"语义（Python L357-373 为 `if name in values`，值为 `None` 也要把已有缺省清零；`BuildUsageDelegationAttribution` 在 `agent_id` 为空时正会产出 `nil`），新增 D-13/D-14 与两条回归用例。 | `internal/agentcore/context_engine/usage/` ← `openjiuwen/core/context_engine/usage/`（5 文件 / 1,193 行） |
| 24.15 | ☐ | forked 压缩栈迁移（B-11 + B-15） | 上游产品路径使用的 `processor/forked/` 整体迁移（41 文件 / 9,173 行：forked base、dialogue/current_round/round_level/session_memory 压缩器、prompts、recall{archive,retriever}、reinjection、offloader/rule_compression、support）+ `harness/tools/compression_recall.py` 工具；收口 B-11（按路径读过的源文件排除规则压缩）与 B-15（压缩召回 query 从结构化 user content 抽取文本），并删除 base 压缩器（回收 24-A 的 D-1 偏差） | `internal/agentcore/context_engine/processor/forked/` · `internal/agentcore/harness/tools/compression_recall/` ← `openjiuwen/core/context_engine/processor/forked/` · `openjiuwen/harness/tools/compression_recall.py` |
| 24.16 | ☐ | browser 子代理上下文栈（B-01） | `BrowserRuntimeRail` + `BrowserStateContextProcessor` + `BrowserWorkingContextProcessor` + `BrowserOffloadRecallTool` 与 runtime projection 装配；随后接入 `ToolResultWindowProcessor`（`tool_names=browser_probe_interactives/browser_probe_cards/browser_snapshot/browser_find/browser_evaluate`，`keep_last_k=2`、`trim_size=1000`）。上游该处理器无配置键、只由 browser agent 装配，故须与浏览器上下文栈同批落地（避免死代码） | `internal/agentcore/harness/tools/browser_move/` · `internal/agentcore/harness/subagents/browser_agent.go` ← `openjiuwen/harness/subagents/browser_agent.py` · `openjiuwen/harness/tools/browser_move/` |

**验证点**：外部记忆服务可用/不可用两种形态下对话均可正常进行，且记忆可检索、可注入、可审计。

### 24-A 上下文引擎 B 组缺陷修复批次（2026-11-06 制定，按规则 8/9 执行）

> 上表是**功能维度**清单；本节是 `docs/sync/defect-plan-2026-11-04.md` §3.B 的**处置维度**批次划分，两者交叉引用。
> 核验结论：B-08 已对齐、B-17 已落地；B-01/B-11/B-15 需先补上游子系统（转 24.16 / 24.15）；其余 8 组本轮修复。
> 完整设计（逐项方案、Python↔Go 映射、偏差登记 D-1~D-4、测试策略）见 `docs/superpowers/specs/2026-11-06-context-engine-b-group-alignment-design.md`；
> 任务级实施计划（32 个 task，逐 task 的失败用例 → 实现 → 验证 → 提交）见 `docs/superpowers/plans/2026-11-06-context-engine-b-group-fixes.md`。

| 批次 | 状态 | 覆盖缺陷 | 内容摘要 |
|------|------|---------|---------|
| C1 | ✅ | B-03、B-04 | 新增 `saveContextsDetached`（`context.WithoutCancel`，等价 `asyncio.shield`）供 `handleContextAbort` 与 `finally` 共用；取消后清洗结果仍能写库，不再被取消态 ctx 吞掉 |
| C2 | ✅ | B-02、B-14 | 删 `max_input_tokens`/`max_output_tokens`、`compression_trigger_ratio`/`compression_target_ratio`、`applyCompressionRatios`/`shrinkThreshold`；新增 `ModelRequestConfig.context_window`（厂商请求侧排除）+ `ModelContextWindowTokensOverride` + OpenRouter 窗口表（默认关、1h TTL）+ `UpdateModelContext` 热切换；内置窗口表按上游重写；dialogue/current_round/round_level 改 `trigger_context_ratio` + `min_target_context_ratio`（full_compact 保持绝对值）；新增 `processor/budget_guard.go` 并接入 round-level 三处预算与卸载占位预览截断 |
| C3 | ✅ | B-12、B-05 | 6 处 token 降级 `/4`→`/3`；新增 `content_sanitize.go` + 图像按尺寸计费（PNG/GIF/JPEG/WebP 头嗅探 + 显式尺寸 + `detail=low` + env 覆盖 + 1445 兜底）；`ContentPart` 增 `Width/Height/Dimensions`；`read_file` 工具级预算改用尺寸公式并标注缩放后真实尺寸 |
| C4 | ✅ | B-07、B-09 | 引擎层 `IsContextOverflowError`（9 短语 + 异常字段/因果链扫描）+ `RecoverFromModelContextOverflow`（压缩一次、流式无输出才允许）；ReAct / DeepAgent 接入；卸载器删私有关键词表改复用引擎判定 |
| C5 | ✅ | B-13、B-16 | 摘要卸载删 4 个兼容字段 + `Validate` 变 no-op + assistant/tool_calls 守卫 + config.yaml 4 键删除；压缩状态新增 `modified_messages`（id 比对 + 新卸载句柄）并透传到流载荷（前端轨迹面板 `compaction.ts:100` 已在读） |
| C6 | ✅ | B-06、B-10 | 见 24.14（用量子系统整体迁移 + `context.usage` 载荷切 `context-usage.v1` 快照），依赖 C2 的窗口来源与预算口径先稳定 |

**执行顺序**：C1 → C2 → C3 → C4 → C5 → C6；每批单独提交、单独 CI 四 job 全绿。

### 24-B LLM 客户端 C 组缺陷修复批次（2026-11-06 制定，按规则 8/9 执行）

> 处置维度批次划分，对应 `docs/sync/defect-plan-2026-11-04.md` §3.C（15 行）。
> 核验结论：C 组 15 行 = **修复 11**（C-01/03/04/05/06/07/08/09/10/13/14）+ **已对齐 4**（C-02 阻塞 channel 无丢弃、C-11 rail 层状态码分类、C-12 transient retry、C-15 早已 ➖）。
> 完整设计（逐项方案、Python↔Go 映射、偏差 D-1~D-5、测试计划）见 `docs/superpowers/specs/2026-11-06-llm-client-c-group-alignment-design.md`；
> 任务级计划（16 任务，逐任务失败用例 → 实现 → 验证 → 提交）见 `docs/superpowers/plans/2026-11-06-llm-client-c-group-alignment.md`。

| 批次 | 状态 | 覆盖缺陷 | 内容摘要 | 提交 |
|------|------|---------|---------|------|
| C-A1 | ✅ | C-14 前半 | 共享 Transport 池（按 `(verify_ssl, ssl_cert)` 分桶，Limits 100/20/60s + ResponseHeaderTimeout 360s + Dial 30s，`CloseIdleTransports` 对齐 `aclose()`） | `9f62540d` |
| C-A2 | ✅ | C-01、C-14 后半 | `BuildHTTPRequest` 增 streaming 语义：流式 `Client.Timeout=0`（读 body 由帧超时兜）、非流式 Client 级总超时；4 处 transport 构造收敛进池；默认 timeout 60→360（恢复 `timeout ≥ first_chunk=300` 不变量） | `3e844d84`+`a62e4222` |
| C-A3 | ✅ | C-01 回归 | 流式 1.2s 持续输出 vs timeout 0.8s → 12 块全收；非流式对照 1.5s 慢响应被切断 | `86d8be80` |
| C-B1 | ✅ | C-04 | `ExtractCacheTokens`/`CacheUsageMetadata`/`ExtractCacheCreationTokens`/`ExtractReasoningTokens` 一比一映射上游四静态方法（7+11 路径矩阵、legacy 不可信语义） | `14a426c6` |
| C-B2 | ✅ | C-04、C-06 | `UsageMetadata` 补 8 字段（cache_read/miss/write、cache_creation、reasoning_tokens、status/source/authoritative）+ `BuildUsageMetadata` 统一入口 | `14a2fc7d` |
| C-B3 | ✅ | C-04 | openai/siliconflow/inference_affinity 三处副本收敛；`session_aggregator` 归一字段优先、legacy 回落；顺带补 `ResponseUsage.completion_tokens_details`（集成用例暴露的硬缺口） | `884bedf9`+`c0b5435b` |
| C-C1 | ✅ | C-05 前半 | `ExtractReasoningContent` 四级回退（reasoning_details[0].text → reasoning_content → reasoning → reasoning_token_text）；ChunkDelta/ResponseMessage 补 3 字段 | `62220c7c` |
| C-C2 | ✅ | C-05 后半 | 补 reasoning_content 移入 `BuildRequestParams`、门卫改「模型名含 deepseek」（修聚合网关 400）；**删 deepseek 幽灵包**（上游 HEAD 已无该文件），注册表 DeepSeek → openAIFactory | `e6b1b580` |
| C-D1 | ✅ | C-07 | `AuthMode` 四枚举 + key/base 双白名单（对齐 `_TOP_LEVEL_*_PROVIDERS`）+ `EndpointProfile` 默认 base 回填；`verify_ssl` 不再强制 `ssl_cert`（L-07 同源）；**删 `WithSkipValidate` 兼容壳** | `96da1e1d`+`56307069` |
| C-D2 | ✅ | C-07 后半 | 鉴权头按 auth_mode：none/oauth 省略、custom_headers 空 key 省略、空 key 用 `EMPTY` 占位 | `7513d664` |
| C-D3 | ✅ | C-09 | `Temperature` 改 `*float64`、删构造默认 0.95/0.1、`nil 不下发`（修显式 0 被吞后反弹 0.95）；应用层默认保留不动 | `e755763b` |
| C-E1 | ✅ | C-08 | `NormalizeAPIBase`（strip 空白/尾斜杠/已带 `/chat/completions`，不推断 /v1）+ BuildHTTPRequest/affinity release/web 保存/dashscope 原生四处接线 | `f421d973` |
| C-E2 | ✅ | C-10 | 编码错误分类 181004 + `details.stage=request_encoding`；输入侧 `findInvalidUTF8` 严格校验（json.Marshal 会静默替换 U+FFFD）；Invoke/Stream 放行不参与重试 | `492dd34f` |
| C-F | ✅ | C-03 | finish_reason 非流式双向对齐：删 `stop→tool_calls` 改写、空值兜底 | `5a532487` |
| C-G | ✅ | C-13 | `ReliableRouter.Close`（停 health checker）+ `ResetRouterCache` 显式驱逐；loop 自动驱逐登记偏差 D-5 | `6b1c1993` |
| C-IT | ✅ | C-01/C-04/C-10/C-14 | 集成用例 `tests/integration/agentcore/foundation/llm_client`（跨度 3，审计通过）：流式长输出 + usage 归一 + callback 事件序 + 聚合统计；编码错误不发网络请求；多客户端共享 Transport | `c0b5435b` |

**已知偏差（D-1~D-5）**：D-1 分桶粒度按安全形态（Go Transport 跨 host）；D-2 ResponseHeaderTimeout 为 Transport 级统一值；D-3 IntelliRouter 无法子类覆写 → 白名单跳过；D-4 dashscope 原生侧超上游一步做归一；D-5 loop 自动驱逐不迁移（Go 无事件循环）。详见 spec §1.3/§4.1/§5.1/§7。
**执行顺序**：C-A1 → C-A2 → C-A3 → C-B1 → C-B2 → C-B3 → C-E1 → C-E2 → C-C1 → C-C2 → C-D1 → C-D2 → C-D3 → C-F → C-G → C-IT；每批单独提交、单独 CI 四 job 全绿。

---

## 领域二十五：Team / Swarm 调度与协作

> 上游团队从"leader 手工派活"演进为"调度模式 + 任务板状态机 + 验证者返工 + fork/checkpoint + 工作组隔离"。体量最大（`agent_teams` 583 文件 / 10.5 万行）。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 25.0a | ✅ | 框架前置（状态投递所需） | ① 消费 `prompts.CommitCallbacksExtraKey`：`syncPromptAttachments` 改返回成败（`bool`），成功后才由调用方 `runCommitCallbacks` 逐条执行；失败不 pop（回调留 extra 等下一边界）、单条 error/panic 只告警（recover 兜底）；三处调用点接线（admission / 模型调用窗口 / steering 批）；② 新增 `before_steering_drain` 事件：`AgentCallbackEvent` 第 13 项 + `SteeringDrainInputs{Pending,Limit *int}` + `AgentRail.BeforeSteeringDrain`/`BaseRail` 默认 no-op + `AllCallbackEvents`(13)/`AllBaseCallbackEvents`(10) + `DeepAgent.bridgeEvents` 第三条（修实注释与实现的脱节）；③ **批语义一并落地**（原 ⏵ 25.2 前移）：`DrainSteering(limit *int)`（nil=全取，非 nil 时 `max(1,limit)`，剩余留队原位）+ `SteeringPendingCount()` + `drainSteeringBatch`（无积压不发事件、Fire 出错仍 drain）；④ 输入批次载体：`UserMessageInputs{Parts,Source}` + `admitUserMessage`（临时切 inputs → fire → defer 还原 → 全丢不写 → 同步附件成功后 commit → `join("\n")`+prefix）+ `extractUserParts` + `applyInputPartsExtra` + `_input_parts` 全链路（`task_loop/executor.go` 生产，rail 改写 query 则不下发）；steering 批改走 `admitUserMessage`（join 成一条，前缀只加一次）；测试：单测 30 例、集成 13 例，C1~C15 每点双覆盖 | `internal/agentcore/single_agent/interfaces/` · `internal/agentcore/single_agent/agents/` · `internal/agentcore/harness/deep_agent.go` · `internal/agentcore/harness/task_loop/executor.go` · `tests/integration/agentcore/{attachment,harness}/` ← `core/single_agent/rail/base.py`(L378/L402/L516/L725) · `react_agent.py`(L917/L964/L1002/L1148/L2981) · `harness/deep_agent.py`(L176) · `task_loop/task_loop_event_executor.py`(L219/L431) |
| 25.0b | ✅ | 成员协作状态渲染 | 四文件纯渲染层：`inbound_render.go`（12 导出符号 = 3 常量 + 9 函数：`<team-inbound>`/`<team-clock>`/`<team-event>`/`<team-context>`（+`<identity>`/`<identity-conversion>`）/`<team-policy>`（+inline 版）/`RenderControllerInput` + `dropSupersededSnapshots`；`<team-note>` 嵌套为所修饰块最后一个子元素、`type`/`kind`/`for` 是稳定英文契约 token 不本地化、policy body 不转义）/ `prompts/messages.go`（`RosterMember` 6 字段 + `RosterDelta` + `DiffRoster`（orderedmap 对齐 Python dict「保首个位置 + 后写覆盖」）+ `FormatMemberLine`（`[human]` 标 human_agent/passive_human）+ 6 个 `Build*`（identity/identityPromptDelta/identityConversion/teamInfo/rosterSnapshot/rosterDelta）+ 中英标签表（与 sections.go 的 labels 是两张表））/ `timefmt.go`（`FormatAbsoluteTime`/`FormatTimeContext` + 相对差五分桶，`formatAbsolute` 可注入时区供测试）/ `external/format.go`（新包：`RenderMessage`/`RenderMessages`）；连带补 `TeamRole` 4 取值（`passive_human`/`bridge_agent`/`worker`/`external_cli`）+ `IsCoordinatedMember`、i18n 10 键 + `ReplyHintFor`、`inbound_tags.md` 中英各一（md5 与上游一致）+ `BuildTeamInboundTagsSection`(P:18，content 双语同载)；77 例单测 + golden 逐字节对拍（`gen_golden.py` 调上游真跑生成 42 个 golden，可重跑）；偏差 D-1（属性单引号 `&#39;` vs `&#x27;`）/D-2（`ModelsUsed` 恒空）/D-3（snapshot 集合用 map 单元素无歧义）/D-4（`RenderTaskLine`/`RenderTaskBoard` 挂 25.4）见 spec | `internal/agent_teams/inbound_render.go` · `timefmt.go` · `prompts/messages.go` · `external/format.go` · `schema/{i18n,team}.go` · `prompts/{cn,en}/inbound_tags.md` · `prompts/sections.go` ← `openjiuwen/agent_teams/inbound_render.py`(417) · `prompts/messages.py`(500) · `timefmt.py`(125) · `external/format.py`(213) |
| 25.0c | ✅ | TeamContextTracker（本节主干） | 通道与基线：identity（`identity_emitted` + `member_prompt_mtime` 增量）/team_info（`team_info_mtime`）/roster（`roster_mtime` + `roster` 快照做 diff）/policy（`policy_hash`）；`snapshot_text`/`pending_text`/`commit(delivered_text)`/`last_delivered_text`/`_compose_blocks` 顺序 identity→policy→roster；基线写成员子 `AgentSession` state key `team_prompt_context`（与对话同 bucket、`pre_run` 恢复）；钩子接线：`OnUserMessage`（附件车道优先 → 兜底前插）+ `BeforeModelCall`（静态 Section + 中途状态 append）；附件车道 `team.context` section（priority=80）+ 无快照→全量 / 无变更→重注册上次内容；TeamBackend 补 3 个探针：`member_updated_at_state`/`stamp_member_prompt_updated_at`/`fork_enabled`；⤴️ 回填 9.68 摘除 `infoCache`/`membersCache`（上游 `prompts/section_cache.py` 已无消费方）；任务通知（F_119 评审通过 / F_129 任务编辑，`_TaskNoticeTracker`/`TaskOutcomeTracker`/`TaskRevisionTracker`/`build_task_event_tracker`）**⤴️ 25.1b 已落 F-129 全链**（`TaskEventTracker.Announce` + `TeamHarness.AnnounceTaskRevisions` + `AgentRoundController` + `TaskBoardHandler.OnTaskUpdated`）；**实际交付**：`internal/agent_teams/tracker/`（D-9 子包，Python 单文件因 Go 包级循环依赖下沉） + 任务表 4 列（`reviewer`/`revision`/`observed_revision`/`change_notes`，只改建表 DDL 不写迁移） + `prompts/message_template.go`（`UnseenChangeNotes`/`ChangeNotesLines`/`RevisionDiff`，`RevisionDiff` 按 difflib 的 Ratcliff-Obershelp 复刻，260 组随机样本 0 差异） + 入站渲染与时间渲染下沉 `inboundtags` 叶子包（父包改纯转发）；偏差 D-1~D-10 见 spec（D-2 ModelsUsed 恒空 / D-3 policyText 恒 nil（**25.0g 已闭环**） / D-5 状态过滤近似 / D-8 ForkEnabled 恒 false / D-9 包归属 / D-10 DispatchMode 恒 autonomous ⤵️ 25.2） | `internal/agent_teams/team_context.go` · `rails/team_policy_rail.go` · `agent/agent_configurator.go` · `tools/team_backend.go` ← `openjiuwen/agent_teams/team_context.py`(1007) · `rails/team_policy_rail.py`(583) **⤴️ 25.0b 已提供**：`inbound_render`（六类标签）/ `messages`（`DiffRoster` + 6 个 `Build*`）/ `timefmt` / `external.RenderMessage` / `schema.ReplyHintFor` / `BuildTeamInboundTagsSection`，本节直接调用；`_list_roster` 的组装逻辑（`modelGroupLabel` / `getMemberServedModels`）与名册累积状态仍在本节。**⤴️ 本节验收项含删除存量**（新实现落地时同改动删除，规则 8）：`SectionPersona`/`SectionInfo`/`SectionMembers` 三常量 + `BuildTeamPersonaSection`/`BuildTeamInfoSection`/`BuildTeamMembersSection` 三 builder + `TeamPolicyRail.infoCache`/`membersCache` 两字段 + `MtimeSectionCache`（若其他包仍在用则保留并登记）+ `uninit` 清理列表收缩；grep 锚点 `team_persona`/`team_info`/`team_members`/`infoCache`/`membersCache` 在 `internal/`、`tests/`、`cmd/` 中 0 命中。 |
| 25.0g | ✅ | 静态 section 补齐（25.0c 暴露的缺口） | **实际交付**：`prompts/{cn,en}/` 各 10 → 34 个模板；`sections.go` 新增 8 个 builder（`BuildTeamDispatchSection` P:15 / `BuildTeamTaskStateSection` P:16 / `BuildTeamBridgeSection` P:12 / `BuildTeamStaticSections` 统一入口 / `BuildLeaderPolicyDisclosure` / + `sections_leader.go` 的 `BuildLeaderBootstrapSection` P:11 / `BuildLeaderPrebuiltSection` P:11 / `BuildLeaderBoundGroupSection` P:12）+ 6 个 section 名常量 + `dispatchRoleSlugs`/`dispatchModes` 表 + `scheduledMemberMessaging`/`hittTemplateName`/`hittContractBody`/`selfMemberLine` 助手；leader 分支严格复刻（只留 bootstrap/prebuilt + bound_group + extra，砍掉 role/HITT/workflow/lifecycle/dispatch/task_state/inbound_tags）；**D-3 闭环**：`policyText` 在 rail 构造期由 `BuildLeaderPolicyDisclosure` + `RenderTeamPolicyInline` 生成并交给 tracker；`build_team` 结果追加披露（接管首行 + 不重复 spawn 提示 + verify 回带）；团队表补 `dispatch_mode`/`enable_task_verification` 两列 + `ensureTeamInfoCapabilityColumns` 迁移；`BuildTeam` 改返回 `(*BuildTeamOutcome, error)` + 步骤 0 接管路径 `reattachTeam`；`TeamAgentSpec` 补 7 字段（`DispatchMode`/`EnableMemberMessaging`/`MaxSubtaskDepth`/`EnableTaskVerification`/`BootstrapMode`/`BoundGroupName`/`SwarmflowEnabled`）+ `validateBootstrapMode` 校验；`CreateTeamTools` 改 `TeamToolOptions` 结构体。**连带结构漂移修正**：`BuildTeamRoleSection` 删 `memberName` 渲染（上游刻意不渲染，否则同角色前缀非逐字节相同）、加 `workspacePromptVariant` 与 HUMAN_AGENT 分支；HITT 316 行内联字符串迁至 4 个模板；`Render()` 改为只替双花括号并按上游切段语义替换（替换值不再被二次解析）；`team_plan_mode` 两占位符跟随上游不替换。偏差 D-1（leader 无 inbound_tags，照抄）/D-2（prebuilt、bound_group 生产中不可达，已补可达性单测）/D-3（SwarmflowEnabled 暂作 spec 字段）/D-4（enable_bridge 未进 BuildTeam）/D-5（policy 渲染失败降级返回 facts）/D-6~D-8 见 spec。**D-9 已闭环**：补 `BuildTeamMemberSystemPrompt` + `tool_namespace` 模板 2 份（上游 15 个 `build_*` 至此全部移植）。**测试期间发现并修复**：`inboundtags` 测试曾导入 `prompts` 构成测试期 import cycle，改为字面量 + 同步守护用例。测试：新 builder 38 例 + 关联翻转 21 例 + 集成 6 例 | `internal/agent_teams/prompts/sections.go` · `sections_leader.go` · `prompts/{cn,en}/` · `rails/team_policy_rail.go` · `rails/team_tool_rail.go` · `tools/team_backend.go` · `tools/team_tools.go` · `tools/team_tool_map_result.go` · `tools/database/{models,sql_team_dao,memory_impl,sql_engine,database}.go` · `schema/{blueprint,constants}.go` · `constants.go` · `agent/agent_configurator.go` ← `openjiuwen/agent_teams/prompts/sections.py`(1001) · `rails/team_policy_rail.py`(583) · `tools/{team,tool_team,tool_factory}.py` · `tools/database/{team_dao,engine}.py` · `schema/blueprint.py` |
| 25.0d | ✅ | 测试 | **实际交付**：25.0c 已覆盖的单测项（基线键 / 增量四通道 / roster join-leave-changed / 首投与 compaction 后全量 / 无变更重注册哈希不变）不重复；补「两段式契约」2 例（`TestCommit_投递失败不推进基线` / `TestCommit_调Commit后推进基线`）——并借此发现 `persist` 曾滥用回滚（已撤回，上游 `_persist` 只告警不回滚，回滚会让 Commit 因环境原因失败时基线永远推不动）；25.0g 新 builder 33 例（dispatch 8 / task_state 3 / bridge 2 / leader 3 / 统一入口 4 / 披露 3 / 可达性 4 / role 3 / HITT 6 中的新增部分）；集成 6 例（首投全量与二轮零新增、名册增量、policy 块通道、验证开关回带、接管路径、建队结果含政策披露），跨 agent_teams/tools + tracker + prompts + agentcore/session 四子系统，审计调用跨度 9 | `internal/agent_teams/prompts/sections_test.go` · `sections_leader_test.go` · `tracker/team_context_test.go` · `tests/integration/agentteams/team_context_test.go` |
| 25.1a | ✅ | Harness 表面与接口（承载原 25.1 四关键词：生命周期事件 / 中断恢复 / TeamHarness·StreamController 对齐 / 多轮 invoke 生命周期）：① `harness/protocol.go`：`HarnessProtocol` 12 成员（`state`/`session_id`/`start`/`stop`/`outputs`/`send`/`abort`/`pause`/`resume`/`subscribe`；并发安全是**契约**：外部只入队+等 ack、状态单写者）；12 成员**一次性定义**，`Pause`/`Resume` 本阶段返回哨兵 `ErrPauseUnsupported`（⤵️ 25.1c 换真实现，接口面不再扩张）；② `harness/state.go`（`HarnessState` 5 态 + `HarnessInternalState`/`ActiveRound`/`InboxMessage`，快照段 ⤵️ 25.1c）+ `harness/control.go` 6 条控制命令（每条带 reply chan，等价 Python `ack: Future`）+ `harness/outputs.go`（队列 + `_END` 哨兵，单消费者迭代器）；`subscribe` **复用** `agentcore/runner/callback.CallbackFramework`：`OnCustom("harness.state"\|"harness.round", fn, WithNamespace("native_harness"))` + `TriggerCustom(ctx, event, map[string]any)`，回调在 supervisor 协程内**同步**执行（上游 `enable_metrics/logging=False` 在 Go 框架中不存在，无需对齐）；③ `harness/native_harness.go`：**组合** `*harness.DeepAgent`（不嵌入——嵌入会与 `DeepAgent.Abort` 同名冲突，且跨包拿不到私有成员，嵌入收益≈0），自建 command queue + supervisor + forwarder，实现 `start/stop/send/outputs/run_once/dispose/subscribe`；多轮共享**一个 session 一条流**（`outputs()` 从 start 活到 stop，而非每轮一条）；`send` 支持 `InteractiveInput` 即实现**中断恢复**；④ 私有缝核账（逐项核过，**只有 1 扇门**）：`setupTaskLoop` 私有 → 导出 `PrepareInteractionTaskLoop`（同时返回 coordinator/controller，覆盖 `loop_coordinator`）；`load_state`/`save_state`/`ReactAgent`/`AbilityManager`/`DeepConfig`/`LoopCoordinator`/`AddRail`/`EnsureInitialized`/`LoadAgentTemplateSpec` 均已导出；`_has_remaining_tasks` **无需开门**——按上游实现读 `LoadState().TaskPlan.GetNextTask()` 自算即可；`_react_agent._usage_build_context`/`_usage_agent_id`（成员用量归属）Go 全仓 **0 命中**，属特性缺失而非够不着（⤵️ 见 24.14 遗留登记）；**不改** `DeepAgent.Abort`（`deep_adapter.go`:1517/1533 生产在用）、**不动** 9.87 supervisor（组合后不启动，不存在两套调度打架）、**不使用** L1 输出租约（L2 自行从 `session.stream_iterator()` 泵队列，上游同构，两套输出机制并存有上游依据）；⑤ `harness/team_harness.go`（由扁平 `harness.go` 迁入）：`TeamHarness` 改持有 `HarnessProtocol`（L3→L2 缝）；**删除** `RunStreaming`/`streamWithSessionRef`/`interactLoop`/`closedChunkChan`/`Steer`/`FollowUp`（唯一调用方 StreamController 改直连协议面）；**保留** MemberRuntime 面方法（`HasPendingInterrupt`/`IsPendingInterruptResumeValid`/`InitCwdForRound`/`FindRails`/`RegisterRail`/`UnregisterRail`/`RegisterMemberTools`/`InjectMemberMemory`/`RunAgentCustomizer`/`Role`/`MemberName`/`RequestCompletionPoll`/`WakeMailboxIfInterruptCleared`/`ensureInitialPlanMode`/`activeAgentSession`/`PersistTeamDBState`/`MarkTeamCleaned`/`MarkTeamBuilt`）；`DeepConfig`/`Workspace`/`SysOperation`/`Model` 改从 `native.DeepAgent()` 取；`MountedRails` + `BuildTeamHarness` 6 rail 参数**本阶段暂留**（内部改为 build DeepAgent → 包 NativeHarness → 挂 rails，⤵️ 25.1b 删）；⑥ `StreamController` 切**订阅驱动**：依赖面是 **`*TeamHarness`**（Python `MemberRuntime` 超集），**不是** `HarnessProtocol`——`has_pending_interrupt`/`is_pending_interrupt_resume_valid` 来自 team_harness.py 而非 protocol.py；`start` = `Subscribe(onState,onRound)` + `outputs()` 泵；**删除** `runOneRound`/`executeRound`/`streamOneRound`/`runRetryingStream`（执行轮搬进 L2 `_run_round`）、`EnqueueOrStart`/`QueuePendingInput`/`StartRound`/`Steer`/`FollowUp`（"该开新一轮还是排队"整个交给 supervisor）；`HasInFlightRound`/`IsAgentRunning` → `State()==RUNNING`；`CancelAgent`/`CooperativeCancel` → `Abort(immediate=true/false)`；`DrainAgentTask` → 等 round 事件；`mapState`（RUNNING→BUSY、IDLE→READY+`onIdleSettled`）/`mapRound`（started→复位吞轮标志+STARTING→RUNNING；finished→COMPLETING→COMPLETED→IDLE；aborted\|paused→CANCEL_REQUESTED→CANCELLING→CANCELLED→IDLE；failed→FAILED→IDLE）照上游实现，迁移复用 `agent_teams/fsm`；**预计删 350~400 行**（现 971 行）；⑦ `TeamAgent` 五个投递入口收敛（`DeliverInput`/`StartAgent`/`FollowUp`/`Steer`/`ResumeInterrupt`，原分支在 `team_agent.go`:757-835）：全改 `Send(ctx, content, immediate=...)`，**删除手写 pending 队列**，并**退役 M-02/M-16 修复**（`degradeAtRoundBoundary` 的 `ErrNoActiveLoop` 降级入队 + `stream_controller.pendingInputs` + `deliver_input_internal_test.go` 两条用例——其存在前提"controller 决定在哪排队"已被 supervisor 内建队列取代，见 spec D-11）——上游原注释：单个 supervisor 串行化所有输入，send 在 idle 开轮、running 转向或排队；因为没有转换窗口竞态，所以这里不需要手写分支/pending 队列（`team_agent.py`:550-570）；⑧ retry 改上游形态：`detectTaskFailed` 保留（调用点从 `streamOneRound` 移到输出泵）、`retryAttempt`/`swallowFailedRound` 改 controller 字段（复位点挂 `round=started`，不再靠"下一轮循环"隐式复位）、`handleRetry` → `Send(retryQuery)`、**新增** `emitRetryOutput`（`[Retry n/N]` 的 `llm_output` chunk，content 直出、前端零改动）；⑨ `onIdleSettled` = wake mailbox（有）+ `reconcile_member_startup`（**先留钩子**，实现 ⤵️ 25.2）+ completion poll（有）；⑩ 测试：单测（协议并发安全/命令 ack 不丢/输出单消费者/相位与轮事件序列/retry 字段复位）+ 集成（真 TeamAgent + MockLLM 连发两轮，断言第二轮输入不丢、不重复开轮、流不断）；⑪ ⤴️ 回填 9.87（L1 交互面首个生产消费者）、按规则 9.1 把扁平 `harness.go` 迁入 `harness/` 包；⤵️ `MemberRuntime` 抽象（协调层→L3 的缝）本阶段不做，是 15.7-15.9 / 25.9 外部 CLI 成员的前置；📄 spec `docs/superpowers/specs/2026-11-06-25.1a-harness-surface-design.md`（19 个讨论点 C1~C19 → 单测/集成用例映射、已知偏差 D-1~D-10、删除清单、验收标准） | `internal/agent_teams/harness/`（`protocol.go`/`state.go`/`control.go`/`outputs.go`/`native_harness.go`/`team_harness.go`）· `internal/agentcore/harness/deep_agent.go`（仅导出 `PrepareInteractionTaskLoop`）· `internal/agent_teams/agent/stream_controller.go` · `internal/agent_teams/agent/team_agent.go` | `openjiuwen/agent_teams/harness/{protocol,state,control,outputs,native_harness,team_harness}.py` · `agent_teams/agent/stream_controller.py`(L305-505) · `agent_teams/agent/team_agent.py`(L550-700) | **⤴️ 25.1b 已回填**：`MountedRails` / `TeamHarnessBuild` 6 rail 参数（已删）/ `teardown_tools`（`RunOnce`+`Stop` 接线）/ spec 前向构造（`NativeHarness(spec, buildCtx)`）。
| 25.1b | ✅ | Harness 装配改 spec 驱动：删 `MountedRails`（上游全仓 0 命中）与 `BuildTeamHarness` 的 6 个 rail 参数；`TeamHarness.build` 签名收敛为 `agent_spec/role/member_name/initial_plan_mode/build_context/task_event_tracker`；rails 改由 `agent_spec.rails` 经 `BuildContext` extras provider 解析 → `ensure_initialized` 统一挂载（上游原文 "no hand-mounting here"）；落 `DeepAgentSpec.ResolveParts`（Go 全仓 0 命中 ← `harness/schema/deep_agent_spec.py`(L524)）；⚠️ 连带漂移须一并裁决：`DeepAgentSpec`/`BuildContext` 上游属 `openjiuwen/harness/schema/`，本仓放在 `internal/agent_teams/schema/` 且**无** `BuildContext`；⚠️ 本仓 `internal/agent_teams/harness.go` 对齐的是上游**已删除**的 `agent_teams/harness.py`（上游已拆包为 `agent_teams/harness/`）。✅ **2026-11-09 完结（Task 1~19 全绿）**：①`BuildTeamHarness` 收敛 6 参并改名 `TeamHarnessBuild`（对齐计划与规则 12 的命名不得改动）、删 `MountedRails`/`Rails()`/`buildDeepAgentFromSpec`；②四步接线落地（`EnsureHarnessElementsRegistered` / 按成员 derive buildContext / `InjectTeamHandles` 17 键 / `TeamRailSpecs` 折进 `agent_spec.rails`）；③`AgentConfigurator` 步骤 9-15 改声明式，删 `buildTeamPolicyRail` helper、加 `lastBuildContext()`；④`TaskEventTracker` 扩 `Announce`/`Wakes`/`Release` + `UnimplementedTaskEventTracker`，`OnTaskUpdated` 接 `AnnounceTaskRevisions` 全链；⑤删 `FirstIterationGate` 整类（决策 C7）；⑥集成 I-01~I-10 十例（审计调用跨度 6 / 有效测试 19）；⑦顺带补齐上游缺口：`member_options` 模块 + `team_member.options` 列（回收 D-17 删 `model_ref_json`）、`TeamBackend.autostart_unstarted` 与 `spawn_member` 7 个选项、`create_task` 内联成员（F-04）主路径、`MemberDao.DeleteUnstartedMember`/`GetMemberStatus`、`NewTaskSpec.Assignee`。**遗留**：F-01（空团队/无模型流不自动收尾，上游同款）、F-03（`QualifyTeamToolIDs` bespoke 路径，上游已改 `add_ability`）、D-15（`resolve_parts` 方法归属）、D-16（`member_options` 落 database 包，Go 无延迟 import）、F-04 的 expert/cli 分支（依赖未迁移域）；📄 spec `docs/superpowers/specs/2026-11-08-25.1b-spec-driven-assembly-design.md` | `internal/agent_teams/harness/team_harness.go` · `internal/agent_teams/schema/deep_agent_spec.go` · `agent/agent_configurator.go` | `openjiuwen/agent_teams/harness/team_harness.py`(L60-130) · `harness/schema/{deep_agent_spec,build_context}.py` |
| 25.1c | ✅ | pause/resume + 快照回滚：`snapshot_rail.go`（`PhaseSnapshotRail` + `capture_snapshot` + 协作式停止；`RoundPhase` BOUNDARY/MODEL/TOOL 决定"硬取消安全"还是"等工具跑完"——工具副作用不可逆）；`SafeStateSnapshot` 进内部状态（`with_history=False` 只回退本轮段落、保留历史）；`pause()` = 取消当前轮 → 回滚到本轮基线 → 缓存 query，`resume()` = 续跑 + steer 新内容（上游已废弃“拼接重来”）；`abort(immediate=true)` 补**回滚语义**（25.1a 只做"取消不回滚"，此处 ⤴️ 回填）；`emitRoundAborted` 标记 chunk（已发出的 chunk 收不回，靠 marker 让消费者丢弃该轮）；⤵️ 网关侧「团队暂停须取消 stream 消费者」上游在 `gateway/message_handler.py`(L1310-1330) 有、Go **0 命中**，须同步落，否则 25.1c 落地后留僵尸消费者 | `internal/agent_teams/harness/snapshot_rail.go` · `native_harness.go`（pause/resume 段） | `openjiuwen/agent_teams/harness/snapshot_rail.py`(240) · `native_harness.py`(L605-672/L1580-1700)  **2026-11-09 与 25.1e 合并交付**：`PhaseSnapshotRail`（5 钩子 + 协作停止）/`SafeStateSnapshot`/`RoundPhase`；`pause` 按 MODEL|TOOL 相位选硬取消或协作停止、`resume` 续跑（warm/cold）、`abort(immediate)` 回滚、`round_aborted` marker；`pending_resume` 冷恢复（metadata 三函数 + kernel pause 就地停车/start 尾部续跑）；`StreamController.PauseAgent/ResumeAgent` + `processTeamInterrupt` 接 `PauseSessionRuntime`；网关团队暂停 ack 后治理流消费者；每轮 BEFORE/AFTER_INVOKE；审查 S-01/S-02/M-01/T-01/T-02/T-03 收口；`cmdRoundFinished` 去 ack（消除 5s 互等）。📄 spec `docs/superpowers/specs/2026-11-09-25.1c-pause-resume-snapshot-design.md` · plan `docs/superpowers/plans/2026-11-09-25.1c-pause-resume-snapshot.md` |
| 25.1d | ✅ | 异步工具运行时（顺序待裁）`async_tools.go`：两阶段异步工具注册表 + 完成后经 `send` 注入（`AsyncToolRuntime`/`launch_async_tool`/`schedule_auto_invoke_on_spawn_done`）；`stop` 取消在飞任务；**实际交付（2026-11-10）**：`async_tools.go`（`AsyncToolRecord`/`AsyncToolRuntime`/`AsyncTool`/`RenderResultText`/spill/控制面）+ `id_generator.go` + `tools/async_tool_host.go` + NativeHarness 接线（runtime/launch/inject/Stop `cancel_all`/spawn-done 改道/BuildContext 句柄）；S-01 前置（`StreamController.Stop`/`FinalizeRound`/`TeamHarness.Stop` commit/`Start` 重建）；单测 **36 例** + 集成 1 例（`grep -c '^func Test'` 机器核对）；设计 `docs/superpowers/specs/2026-11-10-25.1d-async-tools-design.md`；计划 `docs/superpowers/plans/2026-11-10-25.1d-async-tools.md`。⤵️ spill 目录接线 + `background_task_controller` + `async_tasks_*` 控制工具随 25.12 | `internal/agent_teams/harness/async_tools.go` | `openjiuwen/agent_teams/harness/async_tools.py`(507) |
| 25.1e | ✅ | 成员 turn 身份（顺序待裁）`turn.go`：`openjiuwen.turn.id/number` 落成员 session state（键 `trajectory_member_turn`，harness 每轮重建/恢复后编号继续递增）；`resolve_member_turn(continues_turn)` 判定续用还是新开（steer 不计新 turn；InteractiveInput 应答/failure retry/resume continuation 续用当前 turn）；⤴️ 供领域二十二 trajectory v2 映射 span 属性 | `internal/agent_teams/harness/turn.go` | `openjiuwen/agent_teams/harness/turn.py`(151)  **2026-11-09 随 25.1c 合并交付**：`turn.go`（`MemberTurn`/`LoadMemberTurn`/`ResolveMemberTurn`，键 `trajectory_member_turn`）+ `ActiveRound.Turn/TurnOpened` + `WithTurnNumber` → metadata `_turn_number` 透传到 `InvokeInputs.Extra`（span 属性映射 ⤵️ 领域二十二）。**2026-11-10 收尾**：`InvokeInputs` 增 `TurnNumber` typed 字段（对齐 `rail/base.py` L279）+ `DeepAgent.normalizeInputs`/`toEffectiveInputs` 解析回写（`deep_agent.py` L1878-1883/L2002-2003）+ `react_invoke.go` 写 `cbc.extra` 与 `InvokeInputs.Extra`（补 Python L2966-2967 的断链）+ `NativeHarness.ActiveRound()` 访问器（`native_harness.py` L268-275）；测试 6→9 例（新增：编号单调、六种启动模式决策表、访问器生命周期）。设计：`docs/superpowers/specs/2026-11-10-25.1e-turn-and-usage-attribution-design.md` |
| 25.1f | ☐ | native_v2 协议适配（顺序待裁）`NativeHarnessProtocolAdapter`（card=`native_v2`，六能力 steer/force-abort/graceful-abort/pause-resume/checkpoint/persistent-session）+ `NativeV2HarnessProvider` + `create_native_harness_protocol`；`checkpoint.go`（`NativeCheckpoint`/`SavedInput`/`encode_contexts` + 输出态 6 字段 save/restore）；消费方为 jiuwenswarm `external_harness_helpers.create_harness`；依赖 15.1-15.5 协议层 | `internal/agentcore/harness_providers/native/` · `internal/agent_teams/harness/checkpoint.go` | `openjiuwen/agent_teams/harness/protocol_adapter.py`(490) · `checkpoint.py`(59) |
| 25.2 | ☐ | 调度模式 | ⤴️ **本节需回填 10.3.28c/d 的 3 个桩**：R-6 `SessionRuntimeLifecyclePort` 的生产调用方（`pause_session`/`stop_session` 已有实现无消费者）、R-7 `TeamManager` 的 `SetRoundStateReporter`/`RemoveWaiter`/`BroadcastEvent`/`IsGenerationRunning` 四方法（现为桩 + warn）；scheduled dispatch 运行时、review voting、会话级 bootstrap 开关、团队空闲标记、round-idle 对账；⤴️ 25.1a 已留接线点：`onIdleSettled` 的 `reconcile_member_startup` 钩子（实现归本节）与 `mapState(IDLE)` 的 `idle_since` 时钟；批上限语义已前移 25.0a（⤴️）；⤴️ 本节实现 `schedule_team_idle`/`_emit_team_idle_after_debounce`/`_is_task_board_settled`/`emit_team_idle`/`emit_team_recovered`（Go 现 `TeamIdle`/`TaskBoardSettled`/`TeamRecovered` 全 0 命中） | 同上 ← `openjiuwen/agent_teams/`（scheduler）  ⤵️ 25.1c 登记：`refresh_idle_baseline`（长 pause 后停滞时钟基线）、`Send` 的 `run_context`（待驱动队列按上下文切批）。|
| 25.3 | ☐ | 验证者与返工 | verify gate、临时 reviewer harness、review fail 重规划、reviewer 反馈透传 | 同上 ← `openjiuwen/agent_teams/`（reviewer） |
| 25.4 | ☐ | 任务板语义 | condition-named 状态机、task revisions、reassign/cancel/edit 信号、one-active-claim、verify_task | 同上 ← `openjiuwen/agent_teams/`（task board） **⤴️ 25.0b 已提供** `inbound_render.RenderEvent` / `timefmt.FormatTimeContext`；本节需补 `external.RenderTaskLine`（`_TERMINAL_TASK_STATUSES` + `dispatcher.task_unassigned_marker` + `blocked_by` 列表）与 `external.RenderTaskBoard`（`dispatcher.leader_task_board` / `dispatcher.teammate_task_list` + leader/teammate 视图差异）。 |
| 25.5 | ☐ | fork 与 checkpoint | 五种 fork 模式、命名 checkpoint、fork identity 转换、cold resume、WAL/journal 按 run_id 分片 | 同上 ← `openjiuwen/agent_teams/`（checkpoint）  ⤴️ 25.1c 已提供 `pending_resume` 冷恢复，本节只做命名 checkpoint / 五种 fork 模式。|
| 25.6 | ☐ | 会话与消息 | 群聊、跨会话消息、`team.mq.publish` 混合事件、成员 roster、消息/任务内容溢出落盘 | `internal/agent_teams/messager/` ← `openjiuwen/agent_teams/` · `jiuwenswarm/server` |
| 25.7 | ☐ | worktree 隔离 | 项目目录锚定、session 归属、随团队清理、成员注册校验、串行创建 | `internal/agent_teams/team_workspace/` ← `openjiuwen/agent_teams/` |
| 25.8 | ☐ | subagent 工具族 | ⤴️ **回填 10.3.28f 的 D-2**：`sessionHasLiveSubagentRuntime` 两处恒 false 的桩（`cleanup_session_adapter` 与 `evictIdleSessionAdapters` 的"有活子代理则跳过"守卫），落地后会话清理才不会误杀在跑的子代理；⤴️ **回填 10.3.28c 的 R-2**：`stop_subagent_runtimes`（会话删除时停止子代理运行时，现为 warn 实现）；spawn/wait/list/send_input/close/resume、closed 后可恢复、与 team 打通 | `internal/agentcore/harness/subagents/` ← `openjiuwen/harness/subagents/` |
| 25.9 | ☐ | 专家与组织 | AgentGroup、专家团、组织 id 冲突后缀、BM25 专家检索、模板 worker、按需加载成员 | `internal/agent_teams/registry/` · `internal/swarm/extensions/` ← `openjiuwen/agent_teams/` · `jiuwenswarm/agents/` |
| 25.10 | ☐ | 团队技能 | 共享 skill library、技能声明、渐进式工具暴露、技能链接视图下线 | `internal/agent_teams/rails/` ← `openjiuwen/agent_teams/` |
| 25.11 | ☐ | 测试 | 调度模式端到端（MockLLM）、任务板并发、fork/恢复、worktree 清理 | `tests/integration/agent_teams/` |
| 25.12 | ☐ | swarmflow 编排引擎（25.1d 的首个生产消费者） | 上游 `agent_teams/workflow/` 整体迁移（**40 文件 / 11,905 行**，机器核对）：① engine 内核（`primitives`/`runtime`/`runner`/`journal`/`budget`/`cap`/`admission`/`loader`/`progress`/`verify`/`errors`/`provider`/`facade`/`seam`/`schema` + `backends/{base,mock}`）；② worker 后端（`team_worker_backend`/`avatar_session_backend`/`external_{turns,fork,model}`/`expert_specs`/`budget_rail`/`activity_emitter`/`_member_spec`/`_result_text`）；③ 工具与宿主（`tool_swarmflow`/`external_swarmflow_host`/`swarmflow_infra`/`runner`/`concurrency`/`review`/`observer`/`worktree`/`schema`）；④ `background_task_controller`（session 级 pause/resume/stop 控制面，Go 现 0 命中）。`SwarmflowTool` 继承 25.1d 的 `AsyncTool`，经 `parent_agent.launch_async_tool` 两阶段启动；⤴️ 25.1d 的 runtime/spill/i18n/id_generator 在此接上生产消费者；⤴️ 25.1d 的 `TeamToolRail._wire_async_spill` 随本节的 `SwarmflowTool` 装配一起落。实施前需单独 brainstorming + spec（体量约为 25.1a 的 2.8 倍），建议拆 25.12a 引擎内核 / 25.12b worker 后端 / 25.12c 工具与宿主 / 25.12d 测试 | `internal/agent_teams/workflow/` ← `openjiuwen/agent_teams/workflow/` |

**验证点**：调度模式下团队可自主认领/评审/返工直至收敛，中断后可 cold resume。

> **25.12 登记（2026-11-10）**：25.1d 的首个生产消费者是 25.12 swarmflow（`agent_teams/workflow/` 40 文件 / 11,905 行）。在 25.12 落地前，25.1d 的 `AsyncToolRuntime` 只有单测覆盖、生产零调用；25.1d spec 的「已知偏差」登记**回收条件 = 25.12 接入**。25.12 实施前需单独 brainstorming/spec，且必须先确认 25.1d 的 `AsyncTool` 基类与 `LaunchAsyncTool` 缝已稳定。

> **25.0 前置说明**：本节是 9.68 TeamPolicyRail 动态车道的**替代**（⤴️ 回填 9.68），不是新增旁路——静态 Section（role/HITT/workflow/lifecycle/persona/extra）与 7 个构造参数全部保留，只把 info/members 的 mtime 全量重建换成"增量宣告 + 附件车道"。它是 25.2/25.3/25.4/25.6 的共同前置：调度模式要能"告诉成员该干什么"，评审返工要能"告诉作者评审过了"，名册变化要能"只播 join/leave"。Go 现状：`team_context.go`/`inbound_render.go`/`prompts/messages.go` 三个文件均不存在，`team_policy_rail.go` 为上游引入 tracker 之前的 337 行版本，`<team-inbound>/<team-event>/<team-note>/<team-policy>` 标签全仓零命中。

> **25.1 拆分说明（2026-11-06 定稿，⚠️ 实施前先读本节，可省掉一次层级误判）**
>
> **为什么要拆**：上游 `agent_teams/harness/` 是一个 **12 文件 4322 行**的包（`native_harness.py` 1785 / `team_harness.py` 601 / `async_tools.py` 507 / `protocol_adapter.py` 490 / `snapshot_rail.py` 240 / `state.py` 205 / `turn.py` 151 / `control.py` 114 / `protocol.py` 96 / `checkpoint.py` 59 / `outputs.py` 44 / `manifest/` 57），原 25.1 单行无法覆盖，按依赖切三段 a→b→c，另有 d/e/f 三项已识别但**顺序待裁**。
>
> **层级事实（这是九成误判的来源）**：Python 是**三层**——
> **L1 `DeepAgent` 交互面**（`deep_agent.py` L3731-4574：`prepare_interaction_task_loop` / `run_one_round` / `_build_interaction_next_work` / `start` / `stop` / `attach_output` / `send_input` / `next_output` / `_supervisor_loop` / `_forward_session_stream`）。**L1 本身就有 supervisor**，9.87 已按这 843 行 1:1 落地，不是我们擅自加层。
> **L2 `NativeHarness(DeepAgent)`**（`native_harness.py` 1785 行）。逐方法比对结论：**真正覆写只有 4 个**——`start`/`stop`（**不调 `super().start()`**，自建 `_prepare`→`_setup_task_loop`→`create_task(self._supervisor())`）、`outputs`、`abort`（同名但签名语义不同：L1 `abort(session)` 取消任务循环 vs L2 `abort(*, immediate)` 分级+回滚）；**同名替换私有 2 个**——`_supervisor` 替换 L1 `_supervisor_loop`、`_forward_outputs` 替换 L1 `_forward_session_stream`；**其余约 1700 行全是新增**（`send`/`pause`/`resume`/`subscribe`/`run_once`/`dispose`/`AsyncToolRuntime`/快照回滚/命令队列）。**L2 完全绕过 L1 的输出租约**（自己读 `session.stream_iterator()` 泵自己的队列）。
> **L3 `TeamHarness`**（`team_harness.py` 601 行）：组合一个 L2，转发 `HarnessProtocol` 十个成员 + 团队能力钩子。
>
> **消费者事实（决定"单 Agent 走不走 L2"）**：L1 的生产消费者是 **15.6**（`provider="native"` → `harness_providers/native/harness.py`:298 `agent.start` → :359 `attach_output` → :363 `send_input`，plain `DeepAgent`）与 **L2**；L2 的消费者是团队成员（`team_harness.py`:114/147/201）、`TinyAgent`（`tiny_agent.py`:186 单次 `run_once` / :327 持久 chat）、reviewer（`scheduler.py`:945）、swarmflow worker（`team_worker_backend.py`:889）、`native_v2` 适配器（`protocol_adapter.py`:126）。**单 Agent（web/chat/code）既不走 L2 也不走 supervisor**：`DeepAgentSpec.build()` → `DeepAgent` → `Invoke`/`RunStreaming` 走 `_run_task_loop` 自驱（上游同样保留 `_run_task_loop`，非残留）。
>
> **三种"停"不可混为一谈**：①**取消** = L1 有（`abort`/`cancel_round`）；②**暂停/继续** = L1 **没有**（L1 相位枚举只有 `idle`/`running`/`terminated` 三态；协议层 `HarnessState` 才是 `idle`/`running`/`pausing`/`paused`/`terminated` 五态），仅 L2 有（停在 ReAct 迭代边界 + 回滚到本轮基线 + 缓存 query）；③**单 Agent 暂停** = swarm 侧 `JiuClawStreamEventRail` 的每会话闸门（`BeforeModelCall`/`BeforeToolCall` 里 `checkpointWait`，协作式"卡在下一步之前"）。团队成员**也装了**该 rail（Go `agents/harness/team/runtime_inheritance.go`:563 / Python `agents/harness/team/team_runtime_inheritance.py`:219，且在 `_COMMON_RAIL_NAMES` 内）——与上游一致，团队路径上闸门恒开、不参与团队暂停，无害。
>
> **两种暂停的协议后果不同**（上游 `gateway/message_handler.py` L1320-1330 原注释）：**团队暂停**作废本轮并**取消流**、无线级终态帧 → 网关消费者必须取消，否则永挂成僵尸；**单 Agent 暂停**原地挂起、**同一条流**恢复后继续产出 → 网关消费者绝不能取消，否则丢恢复后的输出。
>
> **执行顺序建议**：25.1a 提到 25.0 之前做——9.87 的 L1 交互面目前**零生产消费者**（`Start` 全仓无调用方），是当前最大的结构悬空；25.1a 一落地即接上。15.6 压在 15.1-15.5（全 ☐）之上，不宜提前。
>
> **25.1b 定稿补充（2026-11-08）**：`BuildContext` 落 **core**（上游 `harness/schema/build_context.py` 为真相来源，团队侧仅 25 行转发；证据：core 已 import agent_teams 叶子包，且 code agent 的 `CodeBuildContext` 需要子类继承）。rail provider 注册表同样落 core（`agentcore/harness/schema/rail_registry.go`），`ctx` 用 `BuildContextInterface` 具体类型以避免 any 债。`FirstIterationGate` **删除**（上游 `kernel.py:709` 已废弃，与 25.1a 删除 `degradeAtRoundBoundary` 同属结构性删除）。三个未实现 team rail（reliability/skill_use/observability）**声明常量 + 占位工厂**，保证 `RailSpec.type` 持久化契约从第一天完整。

> **"监管"与"轮"是两个概念**：监管（supervisor）只做**决定**——现在该不该跑、跑哪一轮、跑完还接不接；**跑一轮**由任务循环内核执行（L2 `_run_round` 提交轮次 + 等完成，L1 对应 `_execute_round`）。controller 原先那 4 个方法（`runOneRound`/`executeRound`/`streamOneRound`/`runRetryingStream`）与 5 个投递方法（`EnqueueOrStart`/`QueuePendingInput`/`StartRound`/`Steer`/`FollowUp`）都属于"决定轮 + 执行轮"，25.1a 把这部分整体搬回 L2；controller 只留"看 / 转播 / 映射状态 / 判失败重试"。

> **依赖边是三条，不是两条**：`HarnessProtocol`（10 成员）是 **L3→L2** 的缝（TeamHarness 持有 NativeHarness，作用是"能换脑"）；**`MemberRuntime`**（约 20 成员，`member_runtime.py`）是**协调层→L3** 的缝（作用是"能换整个成员运行时"，外部 CLI 成员走这条），**Go 尚未抽象**、协调层直接持有 `*TeamHarness`——不是 25.1a 的活，是 15.7-15.9 / 25.9 的前置；`StreamController` 依赖的是 **`*TeamHarness`**（依据：`stream_controller.py`:746-767 调 `harness.has_pending_interrupt()`/`is_pending_interrupt_resume_valid()`，这两个方法只在 `team_harness.py` 里，**不在** `protocol.py` 的 10 成员内）。

> **组合 vs 继承的复用比例**：Python 继承能直接摸私有成员（`_setup_task_loop`/`loop_coordinator`/`_has_remaining_tasks`/`_react_agent`），复用≈100%；Go 组合只能拿导出面。逐项核账后新增导出缝**只有 1 个**：`PrepareInteractionTaskLoop`（同时返回 coordinator，覆盖 `loop_coordinator`）；`_has_remaining_tasks` **无需开门**（`LoadState().TaskPlan.GetNextTask()` 可自算）；`_usage_build_context`/`_usage_agent_id` 属**特性缺失**（见 24.14 遗留登记）。因此"嵌入还是组合"在 Go 里几乎只剩 `Abort` 撞名一条判据——组合胜。

---

## 领域二十六：渠道与客户端扩展

> 上游渠道从 Web/IM 扩展到 browser extension、IDE、desktop、process-cli，并把 ACP/A2A 能力补齐（client-tools、interrupts、swarmchat）。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 26.1 | ☐ | IM 平台补齐 | Slack、Telegram、Discord、企微、钉钉、百度网盘 connector、WhatsApp / 小艺既有缺口。⤴️ **待回填（飞书通道整体缺失）**：本仓无 `gateway/channel_manager/im_platforms/feishu`，至少含 24.13 遗留的「非流式 team 拒绝」（`feishu_connect.py:3003-3013`：`enable_streaming=False` 时拦截 `/mode team` 与 `/join` 并回卡片提示）、`_send_attachment_persist_error`（D6 附件落盘失败提示）、文件上传与卡片渲染 | `internal/swarm/gateway/channel_manager/im_platforms/` ← `gateway/channel_manager/im_platforms/` |
| 26.2 | ☐ | 浏览器通道 | browser extension 作为 channel、内置 Playwright MCP 0.0.78、headless/headed 并行隔离、批量表单、页面归属与完成判定 | `internal/swarm/channels/browser/` ← `jiuwenswarm/channels/browser/` |
| 26.3 | ☐ | IDE 通道 | IDE 插件协议与通道 | `internal/swarm/channels/ide/` ← `jiuwenswarm/channels/ide/` |
| 26.4 | ☐ | Desktop 通道 | 桌面端通道 | `internal/swarm/channels/desktop/` ← `jiuwenswarm/channels/desktop/` |
| 26.5 | ☐ | CLI 通道化 | `uapclaw chat` 独立 channel、`/cli` 路由、网关优雅断开与 session rebind | `internal/swarm/channels/cli/` ← `jiuwenswarm/channels/cli/` · `jiuwenswarm/cli/` |
| 26.6 | ☐ | A2A 演进 | reasoning/answer 分离、artifact `last_chunk`、客户端提前停止时关闭远端流 | `internal/swarm/channels/protocol/a2a/` ← `jiuwenswarm/gateway/channel_manager/protocol/a2a/` |
| 26.7 | ☐ | client-tools / interrupts | 客户端工具暂停-回填、`interrupt_id` 回显与按 kind 计数、swarmchat 消息类型 | `internal/swarm/gateway/` · `internal/agentcore/runner/interrupt/` ← `jiuwenswarm/server` · `openjiuwen/harness_protocol/interactions.py` |
| 26.8 | ☐ | 渠道鉴权与用户态 | `--user-id` 透传、Web/TUI WS 握手 user_id、渠道级 token、第三方 agent 切换 | `internal/swarm/gateway/` ← `jiuwenswarm/gateway/` |
| 26.9 | ☐ | 测试 | 每渠道入站/出站契约、断线重连补发、多应用同会话 | `tests/integration/swarm/channels/` |

**验证点**：新增渠道可在不改 agent 侧代码的前提下完成一次完整对话与文件交付。

---

## 领域二十七：Web 控制台后端契约

> 上游 Web 端新增 1508 个文件（前端重构 + 多面板）。Go 只承接**后端契约**与前端产物托管，前端源码不在本仓库。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 27.1 | ☐ | Code 模式 | 前端 MVP 后端、git 状态按轮次索引、last-turn 审核、按轮 diff 统计、work/code 双模式 | `internal/swarm/channels/web/codemode/` ← `jiuwenswarm/channels/web` · `jiuwenswarm/agents/...aipc*` |
| 27.2 | ✅ | Goal | goal 流式（is_stream）、失败文案、历史写入、目标与计划模式互斥；✅ **归属订正（2026-11-05）**：上游 `jiuwenswarm/channels/web/` 下 Python 侧 0 处 goal，goal RPC 实际在 `server/agent_ws_server.py` + `runtime/agent_adapter/interface_deep.py`，故本行范围收窄为**前端契约验证 + `goal.snapshot` 补发**，功能本体拆分至 9.87-9.89（agent-core）与 10.3.27（服务面）；✅ **后端部分已交付（2026-11-09 复核）**：`goal.snapshot` get 单帧短路（`runtime/interface.go:1515` + `interface_goal_shortcircuit_test.go`）、五个出站事件常量（`adapter/goal_events.go`）、goal 完成卡片幂等落盘（`deep_adapter_goal_history.go`）、plan 模式互斥（`service_plan_guard_test.go`）、历史写入与中间 final 降级（`deep_adapter_interaction.go:511`）；✅ **验收完成（2026-11-09，与 27.10 合批）**：`tests/integration/swarm/web/` G1 9 组 RPC 契约（一元/流式 get 分路、pause/clear 空会话 `no_goal`、set/resume 进适配器链、未确认覆盖 `already_exists`、execution.error 帧形状）+ G2 O1-O5 顺序 + G3 R1-R4 断线/隔离 + G4 状态机；**两处按 Python 语义订正测试假设**：①空会话 pause/clear 应返回 `goal_error/no_goal`（`interface_deep.py:17815`），不是成功控制响应；②默认 HYBRID 策略下 COMPLETE 报告需 transcript assessor 验证，无模型时保守回落 continue（`evaluation.py:190-208/228-237` ↔ `task_completion.go:747-759`），Go 与 Python 逐字一致，故不硬断言「跑到 COMPLETED」；③规则 7 要求每 `_test.go` 调 `suite.Run(` → helper 文件补真实 `GoalHelpersSuite`。本地 build/vet/全量单测 325 包 0 FAIL / 集成 `-race -count=2` 绿 / 审计通过；`internal/swarm/channels/web/` 下无新建 go 包（按订正归属，后端逻辑全在 server 侧，不新造目录） | 前端契约验证 → `tests/integration/swarm/web/`（与 27.10 合批） ← `jiuwenswarm/server` · `jiuwenswarm/channels/web/frontend/src/hooks/useWebSocket.ts` |
| 27.3 | ☐ | 轨迹面板 | token 成本时间线、成员泳道、错误原因、上下文压缩标记 | 同上 ← `jiuwenswarm/channels/web`（trajectory） |
| 27.4 | ☐ | 配置面板 | `/config` 交互面板、模型增删改、多模态开关、实验特性开关、API Key 长度 2048。⤴️ **待回填**：27-A Task 8 的 10 个 config 代理 RPC（`models.get/upsert/delete/references`、`model_groups.list/upsert/delete`、`vendors.list/fetch_models`、`session.selection.set`）—— 个人版恒走 E2A 而本仓无对应 `ReqMethod`，需先移植模型目录子系统（`model_catalog`+`model_selection`+`model_errors`+config upsert/delete+`model_vendor_registry` ≈1,200 行），详见 `docs/superpowers/plans/2026-11-05-web-ws-rpc-alignment.md` Task 8 | 同上 ← `jiuwenswarm/gateway/channel_manager/web` |
| 27.5 | ☐ | 技能与市场 | 技能广场/我的技能/插件市场/MCP 市场、Hub 缓存与四类资产统一列表 | 同上 ← `jiuwenswarm/server`（marketplace/skills） |
| 27.6 | ☐ | 会话导航 | 归档/恢复/删除、ephemeral/fork 会话、项目维度归档、会话侧边栏数据 | 同上 ← `jiuwenswarm/server`（session） |
| 27.7 | ☐ | 心跳与定时任务面板 | 执行与聊天会话统一、未读标记、模式/模型选择、抽屉交互契约 | 同上 ← `jiuwenswarm/channels/web` |
| 27.8 | ☐ | 富媒体 | 图片附件与多模态对话、artifact 预览/下载、SVG 渲染、文件卡片 | 同上 ← `jiuwenswarm/channels/web` · `jiuwenswarm/server` |
| 27.9 | ☐ | 前端产物托管 | dist 构建与嵌入、低版本内核 polyfill、同端口 HTTP 就绪探针 | `internal/swarm/channels/web/dist.go` ← `jiuwenswarm/channels/web/frontend` |
| 27.10 | ✅ | 测试 | RPC 契约快照、事件流顺序、断线补发；✅ **完成（2026-11-09）**：7 文件 / 1088 行 / 23 条 suite 测试方法（有效 20）；`go run ./cmd/integration-audit -verify` → `[通过] tests/integration/swarm/web（调用跨度 3，有效测试 20）`；审计总退出码 1 来自**既有违规** `tests/integration/agentteams/harness`（25.1d 并发会话 `async_tools_test.go` 未调 suite.Run，main 上同样为 0），本批未触碰；📄 spec `2026-11-09-goal-web-contract-27.2-27.10-design.md` · plan `2026-11-09-goal-web-contract-27.2-27.10.md` | `tests/integration/swarm/web/` |

**验证点**：Web 端所有面板依赖的 RPC 与事件均可用，且与 Python 契约字段一致。

### 27-A Web 后端结构对齐批次（2026-11-04 制定，按规则 9 执行）

> 上表 27.1-27.10 是**功能维度**清单；本节是**执行维度**的批次划分（文件级 1:1 对齐 + 功能补齐），两者交叉引用。
> 现状：上游 `gateway/channel_manager/web/` 38 个 py / 19,789 行 + `jiuwenswarm/channels/web/` 4 个 py / 3,033 行；
> Go 侧对应 8 个 go / 8,514 行 + 5 个 go / 960 行。前端（`internal/swarm/channels/web/frontend`，1292 源码 + 249 dist）
> 调用 163 个 RPC，Go 注册 75 个，且存在命名漂移（`models.*`→`models.get/upsert/delete` 等）。
> 估算合计 **28-33 人日**（不含与前端联调往返）。**执行顺序**：24.13（mode 缺省语义）先落地，本域从 B0 起按批推进；每批单独任务、单独 CI 绿。

| 批次 | 状态 | 内容 | 上游 | 估算 |
|------|------|------|------|------|
| B0 | ✅ | **RPC 契约矩阵 + 文件映射矩阵**：脚本扫前端 RPC 名 ↔ Go 注册表 ↔ 上游 handler 注册，产出 `docs/sync/web-rpc-matrix.md`；顺带修 `models.*`/`schedule.*`/`heartbeat.*` 命名漂移 | — | 0.5 人日 |
| B1 | ✅ | `web_connect.py` 单一化：三文件合并、删 frame DTO、方法归属回 `WebChannel`。**B1a**（`b86278c9`…`c510ab75`）：删 `frame.go`/`RPCDispatcher`、59 个 handler 转原生 `methodHandler`（帧级 `ok=false/error/code`）、分发顺序改上游四段式。**B1b 六批**（`968bafac`…`d430411d`）：补齐 39 个本地方法 —— 9 平台 IM 配置 18、health_check×2 + cron.job.meta + updater×4、杂项 4、external_cli 7、path.select_file 1、session 薄代理 2 | `web_connect.py` 2,366 | 3 人日 |

> **27-A B0/B1 收口口径（2026-11-05）**：矩阵 196 方法，**漂移 0**；本层承诺（`local` + `agent:ok`）剩余缺口
> 全部为下游或已裁定跳过项 —— `local` 剩 10 个（`models.*`/`model_groups.*`/`vendors.*`/`session.selection.set`，
> 需先移植 ~1,200 行模型目录子系统，**本期跳过**，见 `docs/superpowers/plans/2026-11-05-web-ws-rpc-alignment.md` Task 8）；
> `agent:ok` 剩 5 个由 AgentServer 侧补齐。其余 82 个下游缺口见 `docs/sync/web-rpc-downstream-gaps.md`。
> `updater.*`（7）与 `external_cli.*` 安装族（4）为"保留 handler 回默认"，已在 spec §10 登记。
| B2 | ☐ | HTTP 路由/鉴权/超时/分发 | `web_http_routes.py`+`web_http_dispatch.py`+`web_http_auth.py`+`web_http_timeouts.py`+`http_routes/*` ≈ 1,554 | 2 人日 |
| B3 | ☐ | 生命周期/投递/连接适配/传输熔断 | `lifecycle_handlers.py`+`web_channel_app.py`+`web_delivery_registry.py`+`ws_connection_adapter.py`+`transport_stall_guard.py` ≈ 786 | 1.5 人日 |
| B4 | ☐ | 文件与目录面板 | `file_picker.py`+`directory_picker.py`+`enterprise_files.py`+`container_file_http.py` ≈ 2,485 | 3 人日 |
| B5 | ☐ | git 状态与 diff | `git_diff_watcher.py`+`git_diff_status.py`+`git_ws_handler.py`+`project_git.py` ≈ 2,083 | 3 人日 |
| B6 | ☐ | 分享与导出 | `personal_share_api.py`+`share_snapshot.py`+`share_image_export.py`+`http_routes/share_jobs.py` ≈ 1,366 | 2 人日 |
| B7 | ☐ | 轨迹/诊断/invoke/ASR | `trajectory_http.py`+`diagnosis_http.py`+`trace_update.py`+`invoke.py`+`task_asr.py` ≈ 1,627 | 2.5 人日 |
| B8 | ☐ | MCP/沙箱 OAuth/agentos_compat/designer | `mcp_app_sandbox_http.py`+`mcp_oauth_http.py`+`agentos_compat.py`+`designer_handlers.py` ≈ 370 | 1 人日 |
| B9 | ☐ | `app_web_handlers.py`（6,580，占三分之一）再拆 3-4 批（`_memory_compute` 等就地归位） | `app_web_handlers.py` 6,580 | 6-8 人日 |
| B10 | ☐ | `channels/web` 包对齐 | `app_web.py`+`hub_oauth.py`+`custom_agent_upload_http.py`+`share_image_export.py` ≈ 3,033 | 3 人日 |
| B11 | ☐ | dist 重建一致性判据 + 低版本内核 polyfill + 同端口就绪探针 | — | 1 人日 |

**已知结构债（B0 前须确认归属）**：

| Go 模块 | 行数 | 上游对应物 | 处置 |
|---------|------|-----------|------|
| `web/frame.go`（`ReqFrame`/`ResFrame`/`EventFrame`/`DecodeFrame`） | 209 | **无**（上游用 `dict`） | B1 拆除 |
| `web/memory_compute.go` | 89 | `app_web_handlers.py:3995 _memory_compute` | B9 归位 |
| `web/config_apply.go` | 1,024 | **未确证**（`gateway/channel_manager/web/*.py` 无 `apply_config`；疑落 `lifecycle_handlers.py` 或 `app_web_handlers.py` 某段） | B0 定归属，B3/B9 归位 |

---

## 领域二十八：CLI / TUI 与技能生态

> 上游 TUI 已从"内嵌面板"变为独立 channel，命令族与渲染大幅扩充；技能侧新增全量创建（skill-omni-creation）、内置 skill-creator/swarmskill-creator、市场与经验分发。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 28.1 | ☐ | TUI 命令族 | `/config`、`/permissions`、`/recap`、`/statusline`、`/mode`、`/rewind`、`/btw`、`/review`、`/simplify`、`/keybindings`、`/switch-cc` | `internal/swarm/gateway/channel_manager/tui/` ← `jiuwenswarm/gateway/channel_manager/tui/` |
| 28.2 | ☐ | TUI 渲染 | 工具渲染、日志折叠、流式思考分段、英文状态文案、多窗口 | `internal/swarm/channels/tui/` ← `jiuwenswarm/channels/tui/` |
| 28.3 | ☐ | chat 入口 | `uapclaw chat`、keepalive、Ctrl+C 语义、dotenv 早期解析保护 | `cmd/uapclaw` ← `jiuwenswarm/cli/main.py` |
| 28.4 | ☐ | 技能全量创建 | skill-omni-creation 流程、知识转技能、产物位置 | `internal/swarm/server/runtime/skill/omni.go` ← `jiuwenswarm/agents/...skill*` |
| 28.5 | ☐ | 内置技能 | skill-creator、swarmskill-creator、预置办公技能、内置白名单、中英展示 | `resources/agent/skills/` ← `jiuwenswarm/resources/` |
| 28.6 | ☐ | 技能分发 | 经验增强分发、多源聚合搜索、SkillHub 推荐、去重 | `internal/swarm/server/runtime/skill/dispatch.go` ← `jiuwenswarm/server`（skill dispatch） |
| 28.7 | ☐ | 技能发布 | 我的技能、发布、版本、来源标识、卸载按 slug | 同上 ← `jiuwenswarm/server`（skills） |
| 28.8 | ☐ | 测试 | 命令解析与渲染快照、技能安装/卸载、市场安装契约 | `tests/integration/swarm/tui/` |

**验证点**：TUI 覆盖上游命令族，技能可从市场安装并在对话中被检索分发。

---

## 领域二十九：交付与运维

> 上游更名 `workswarm`、新增诊断与保留策略、改造部署与打包。这些改动不改核心语义，但直接影响交付物。

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| 29.1 | ☐ | 品牌与包名 | `workswarm` 命名、欢迎页/文案、版本号与源码修订暴露 | `cmd/` · `internal/common/version/` ← `jiuwenswarm/pyproject.toml` · `jiuwenswarm/common/version.py` |
| 29.2 | ☐ | 诊断 | 活进程诊断、findings/suggestions、诊断 agent 选择 | `internal/swarm/diagnosis/` ← `jiuwenswarm/diagnosis/` |
| 29.3 | ☐ | 保留策略 | 会话/轨迹无限保留、超期清理、Team/休眠会话库清理 | `internal/swarm/server/session/` ← `jiuwenswarm/runtime/session*` |
| 29.4 | ☐ | 进程诊断 | SIGUSR1 异步状态 dump、`debug --skip-build`、端口回显 | `cmd/uapclaw` ← `jiuwenswarm/debug_launcher.py` |
| 29.5 | ☐ | 打包与签名 | Windows 签名、内置插件打包、`make stop` | `packaging/` · `Makefile` ← `jiuwenswarm/scripts/` |
| 29.6 | ☐ | 配置迁移 | `config_version`、模板迁移、启动时补缺失字段 | `internal/common/config/` ← `jiuwenswarm/common/config.py` |
| 29.7 | ☐ | 部署 | systemd 回退 nohup、分布式多机、单管理节点、一键部署 | `packaging/` · `scripts/` ← `jiuwenswarm/scripts/` · `Makefile` |
| 29.8 | ☐ | 测试 | 配置迁移幂等、保留策略边界、打包冒烟（wine 跨平台） | `tests/e2e/` |

**验证点**：交付包在 Linux/Windows 均可启动，旧配置可无损迁移。
