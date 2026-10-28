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

按 **12 大垂直领域** 组织，每个领域自底向上完整实现，可独立验证。

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
| 6.4 | ✅ | AgentCallbackEvent 枚举 | ✅ AgentCallbackEvent 枚举（type string，10 种事件，常量 Callback 前缀）；✅ AllCallbackEvents/String/GoString；✅ AgentCallEventType 重命名为 AgentCallGlobalEventType（常量名不变）；✅ 测试全部通过（rail/ 覆盖率 100%） | `openjiuwen/core/single_agent/rail/base.py` |
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
| 7.20 | ✅ | Dreaming Orchestrator + Sweeper | ✅ 两层架构：Orchestrator（agentcore 纯调度）+ Sweeper（swarm 业务管线 Scan→Compress→LLM Extract→Promote）；✅ 4 套提示词模板（code/agent × zh/en）一比一复刻 Python；✅ 公共 API（StartDreaming/StopDreaming/GetDreamingOrchestrator）；✅ DeepAdapter TryStartDreaming/TryStopDreaming 回填完成；68 个测试 | `openjiuwen/core/memory/dreaming/` · `jiuwenswarm/agents/harness/common/memory/dreaming/` |
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
| 7.36 | ✅ | 检索索引 | ✅ BaseMemoryIndex 接口(16方法) + StorageCodec + MemoryDoc + MemorySearchResult + UserScope + MemoryIndexBase(7个默认实现+RWMutex并发安全) + SimpleMemoryIndex(KV+Vector双存储编排：AddMemories按类型分组→嵌入→Vector→KV+ID追踪 / Search嵌入→逐类型向量搜索→KV获取→解码→排序截取 / UpdateMemories先删后加 / DeleteMemories+3种DeleteBy* / GetByID / ListMemories分页+类型排序 / ListUserScopes / SchemaVersion+Backup三方法 / ensureCollection懒创建+缓存)；覆盖率 89.8%；⤵️ retrieval/indexing 文档索引管理器（Indexer+Parser+Chunker+Extractor）缺口见领域十三（含 2026-10-27 扩充的检索查询管线 13.19-13.28） | `openjiuwen/core/foundation/store/index/` |

**验证点**：✅ Agent 可存取长期记忆，跨会话保持上下文；安全护栏可拦截恶意输入

---

## 领域八：工作流与图引擎 + 多 Agent 团队

> 复杂任务编排与多 Agent 协作能力

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **8.x Pregel 图引擎** | — | | | |
| 8.1 | ☐ | Graph 接口 | `StartNode/EndNode/AddNode/AddEdge/AddConditionalEdges/Compile` | `openjiuwen/core/graph/graph.py` |
| 8.2 | ☐ | ExecutableGraph | `Invoke/Stream/Collect/Transform/Interrupt` | `openjiuwen/core/graph/executable.py` |
| 8.3 | ☐ | PregelNode | 图节点（name, func, routers） | `openjiuwen/core/graph/pregel/base.py` |
| 8.4 | ☐ | Channel 接口 | `IsReady/Accept/Consume/Snapshot/Restore` | `openjiuwen/core/graph/pregel/` (Channel) |
| 8.5 | ☐ | IRouter 接口 | `Dispatch(sourceNode)→[]Message` | `openjiuwen/core/graph/pregel/` (IRouter) |
| 8.6 | ☐ | Graph Message / Interrupt | 图消息与中断机制 | `openjiuwen/core/graph/pregel/` |
| 8.7 | ☐ | GraphInterrupt Exception | 图级中断异常 | `openjiuwen/core/graph/pregel/` |
| 8.8 | ☐ | GraphState | 图状态管理 | `openjiuwen/core/graph/graph_state.py` |
| 8.9 | ☐ | Graph Store | `Get/Put/Delete/Search` (InMemoryStore) | `openjiuwen/core/graph/store/` |
| 8.10 | ☐ | AtomicNode | 原子节点 | `openjiuwen/core/graph/atomic_node.py` |
| 8.11 | ☐ | Vertex | 图顶点 | `openjiuwen/core/graph/vertex.py` |
| 8.12 | ☐ | StreamActor | 流式执行器 | `openjiuwen/core/graph/stream_actor/` |
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

---

## 领域九：DeepAgent 应用层 (Harness)

> 面向生产的高级 Agent 封装

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **9.x DeepAgent** | — | | | |
| 9.1 | ✅ | DeepAgent | 包装 ReActAgent + 任务循环 + Rails + 技能 + 子 Agent；✅ init_cwd 在 ensureInitialized 中默认兜底，adapter 层 seedRuntimeCwd 覆盖（对齐 Python 两层 init_cwd） | `openjiuwen/harness/deep_agent.py` |
| 9.2 | ✅ | DeepAgentConfig | DeepAgent 配置 | `openjiuwen/harness/harness_config/` |
| 9.3 | ✅ | DeepAgent Factory | 创建 DeepAgent 实例 | `openjiuwen/harness/factory.py` |
| 9.4 | ✅ | TaskLoopController | 任务循环控制器 | `openjiuwen/harness/task_loop/` |
| 9.5 | ✅ | LoopCoordinator | 循环协调器 | `openjiuwen/harness/task_loop/` |
| 9.6 | ✅ | TaskLoopEventExecutor | 任务循环事件执行器；产出：`task_loop/executor.go`（TaskLoopEventExecutor + BuildDeepExecutor，DeepAgentProvider 已移至 harness/interfaces/ 改名 DeepAgentInterface）、`task_loop/handler.go`（TaskLoopEventHandler per-round channel + 事件路由 + interactionQueuesProvider 实现）；覆盖率 87.2% | `openjiuwen/harness/task_loop/` |
| 9.7 | ✅ | SessionSpawnExecutor | 会话子进程执行器；产出：`harness/interfaces/deep_agent.go`（DeepAgentInterface + LoopCoordinatorInterface）、`harness/schema/task_type.go`（DeepTaskType + SessionSpawnTaskType 常量）、`task_loop/session_spawn_executor.go`（SessionSpawnExecutor + BuildSessionSpawnExecutor）、`tools/subagent/session_tools.go`（SessionTaskRow + SessionToolkit + SessionsList/Spawn/Cancel 工具 + BuildSessionTools）、`task_loop/handler.go`（completeSessionSpawn 两路分支 + extractResultFromEvent + extractErrorFromEvent + formatSessionSpawnSteer）；ControllerInterface 补 TaskScheduler + TaskManager；SessionsCancel/SpawnTool 对齐 Python 路径（LoopController→TaskScheduler/TaskManager）；NewToolCallOptions nil 防御；task_loop 覆盖率 83.9%、subagent 覆盖率 83.3% | `openjiuwen/harness/task_loop/` |
| **9.x 安全 Rails** | — | | | |
| 9.8 | ✅ | ShellAST 分析 | ✅ ShellStructureFlags+ShellSubcommand+ShellAstParseResult+ShellAstKind枚举；✅ ParseShellForPermission(tree-sitter+go-shlex+保守回退)；✅ tree-sitter Parser 缓存(sync.Once+sync.Mutex)；✅ shlexSplit 未闭合引号返回 error 对齐 Python ValueError；覆盖率 69.4% | `openjiuwen/harness/security/` |
| 9.9 | ✅ | TieredPolicy | ✅ EvaluateTieredPolicy+Strictest+SeverityToDecision；✅ MaybeEscalateShellOperators+MatchedRuleUsesApprovalOverride+RuleToolsCategoryConsistent；✅ GetBuiltinSecurityRules(文件系统读取+mtime检测自动重载)；✅ shellPatternMatches+pathPatternMatches+collectParamRuleHits+collectApprovalOverrideHits；✅ evaluateSingleInvocation+shellAstFloor+aggregateSubcommandResults；✅ withShellCommand 条件注入 command 对齐 Python；覆盖率 69.4% | `openjiuwen/harness/security/` |
| 9.10 | ✅ | 主机校验 | ✅ URLMatcher(hostname去端口 vs netloc含端口)对齐 Python；✅ isHTTPDownloadTarget 只检查 scheme+host 对齐 Python；✅ matchHost 段数匹配移除 host==suffix 分支对齐 Python；✅ Windows (?i) 注入 runtime.GOOS 判断；✅ PatternMatcher+PathMatcher+CommandMatcher；覆盖率 69.4% | `openjiuwen/harness/security/` |
| **9.x 功能 Rails** | — | | | |
| 9.11 | ✅ | ProgressiveToolRail | 渐进式工具权限 | `openjiuwen/harness/rails/` |
| 9.12 | ✅ | TaskCompletionRail | 任务完成检测 | `openjiuwen/harness/rails/` |
| 9.13 | ✅ | TaskPlanningRail | 任务规划 | `openjiuwen/harness/rails/` |
| 9.14 | ✅ | AgentModeRail | Agent 模式切换 | `openjiuwen/harness/rails/` |
| 9.15 | ✅ | HeartbeatRail | 心跳 | `openjiuwen/harness/rails/` |
| 9.16 | ✅ | McpRail | MCP 工具管理 | `openjiuwen/harness/rails/` |
| 9.17 | ☐ | LSPRail | LSP 集成 | `openjiuwen/harness/lsp/` |
| 9.18 | ✅ | SysOperationRail | 系统操作管理 | `openjiuwen/harness/rails/` |
| 9.19-23 | ✅ | 其他 Rails | Security(✅ BaseSecurityRail+TypeName()+SafetyPromptRail+PermissionInterruptRail+TieredPolicy+Patterns+Checker+Suggestions+PermissionEngine+Factory(BuildPermissionInterruptRail+PermissionRailConstructor回调解耦循环依赖)+llm/modelName参数补齐+日志对齐(rail/tool_args/optional_tool_tags字段)+ConfirmAction三路返回+内置规则缓存+mtime检测+runSecurityCheck默认error强制子类实现)/Interrupt(✅ BaseInterruptRail+AskUserRail+ConfirmInterruptRail)/Skill(✅ SkillUseRail: 增量加载+YAML解析+enabled/disabled过滤+SkillTool/ListSkillTool注册+all/auto_list提示词注入+演化经验附加+includeTools幂等注册)/ContextEngine(✅ ContextProcessorRail+ContextAssembleRail+MergeConfig+RefreshTaskState+FixIncompleteToolContext; ⤵️ session memory 集成)/Memory(✅ MemoryRail+CodingMemoryRail)/Verification(✅ VerificationRail+VerificationContractRail; ⤴️9.29回填)/Subagent(✅ SubagentRail同步模式+✅异步SessionTools回填: Init注册sessions_list/spawn/cancel三工具+toolkit注入+Uninit反向清理+BeforeModelCall注入session_tools节+取消静默路径; E2E见session_cancel_e2e_test.go 6场景; ⤴️9.29回填) Rails | `openjiuwen/harness/rails/` |
| 9.24 | ✅ | EvolutionRail | P1(✅ 契约层: contracts+approval_events+approval_runtime; 覆盖率95.9%) / P2(✅ 基类+纯收集: EvolutionRail基类+EvolutionExtension接口+TrajectoryRail; 覆盖率91.7%; extractor.go resourceManager any→*ResourceMgr) / P3(✅ 单Agent演化: SkillEvolutionRail+slash命令回填+DeepAdapter占位回填+Sharing集成; 对齐Python skill_evolution_rail.py+skill_evolution_sharing.py) / P4(✅ 团队演化: TeamSkillEvolutionRail+team审批回填+DeepAdapter回填; 对齐Python team_skill_evolution_rail.py; ⤵️ 10.6.3-10 Rail实例化/注入) / P5(✅ 跨用户共享组合到SkillEvolutionRail; ⤴️9.80a; ✅G1~G8差距修复对齐Python) / P6(✅ 上下文演化: ContextEvolutionRail(Priority=50+记忆注入+自动总结); ⤴️9.82 P7 ✅) | `openjiuwen/harness/rails/evolution/` |
| **9.x 子 Agent** | — | | | |
| 9.25 | ✅ | ResearchAgent | 研究子 Agent（BuildResearchAgentConfig + CreateResearchAgent，默认 SysOperationRail） | `openjiuwen/harness/subagents/` |
| 9.26 | ✅ | BrowserAgent | ✅ Layer1 配置层（env/config/progress/service）；✅ Layer2 运行时核心（runtime/browser_rail/runtime_tools/browser_agent_factory）；✅ Layer3 探测与控制（profiles/probes/controllers/agents）；⤵️ 9.38-49 Playwright MCP 端到端集成（6处占位：CodeExecutor/ManagedBrowserDriver/WorkerAgent/runTaskOnce/Probe占位/restart） | `openjiuwen/harness/subagents/` · `tools/browser_move/` |
| 9.27 | ✅ | CodeAgent | 编码子 Agent（BuildCodeAgentConfig + CreateCodeAgent + injectBuiltinPlanAgents + mergeRailsWithRequired + 4 必需 Rails 去重合并 + EnableTaskPlanning=true + RestrictToWorkDir=false） | `openjiuwen/harness/subagents/` |
| 9.28 | ✅ | PlanAgent | 规划子 Agent（✅ BuildPlanAgentConfig + CreatePlanAgent 工厂 + 双语提示词一比一复刻 + SysOperationRail(WithReadOnly) 双重约束 + RestrictToWorkDir=false + MaxIterations=25 + 不设 FactoryName 走通用路径 + 11 个单元测试；⤴️ 9.68-69 team.plan 特化已回填：TeamPlanModeRail + apply_team_plan_agent_prompt 逻辑移入 rail） | `openjiuwen/harness/subagents/` |
| 9.29 | ✅ | VerificationAgent | ✅ BuildVerificationAgentConfig + 双语提示词一比一复刻 + SysOperationRail() + VerificationRail() + MaxIterations=40 + 不设 FactoryName 走通用路径 + RestrictToWorkDir=false；⤴️ 9.19-23 回填 VerificationRail + VerificationContractRail + SubagentRail | `openjiuwen/harness/subagents/` · `harness/rails/subagent/` |
| 9.30 | ✅ | ExploreAgent | 探索子 Agent（✅ BuildExploreAgentConfig + CreateExploreAgent 工厂 + 双语提示词一比一复刻 + SysOperationRail(WithReadOnly(true)) 统一只读 + 不设 FactoryName 走通用路径 + RestrictToWorkDir=false + MaxIterations=15 + 11 个单元测试） | `openjiuwen/harness/subagents/` |
| 9.31 | ☐ | MobileGUIAgent | 移动端 GUI Agent | `openjiuwen/harness/subagents/` |
| **9.x 系统操作** | — | | | |
| 9.32 | ✅ | SysOperation 接口 | 系统操作抽象 | `openjiuwen/core/sys_operation/` |
| 9.33 | ✅ | LocalSysOperation | 本地执行 | `openjiuwen/core/sys_operation/` |
| 9.34 | ☐ | SandboxSysOperation | 沙箱执行 | `openjiuwen/core/sys_operation/` |
| 9.35 | ✅ | Shell Process Registry | Shell 进程管理 | `openjiuwen/core/sys_operation/` |
| 9.36 | ☐ | JiuwenBoxProvider | JiuwenBox 沙箱 Provider | `openjiuwen/extensions/sys_operation/` |
| 9.37 | ☐ | AioProvider | Aio 沙箱 Provider | `openjiuwen/extensions/sys_operation/` |
| **9.x 内置工具集** | — | | | |
| 9.38-49 | ✅ | Harness 工具集 | ✅Shell(BashTool+PowerShellTool)/✅文件系统(Read/Write/Edit/Grep/Glob/ListDir)/✅CodeTool/✅MCP(MCPTool+ListMcpResources+ReadMcpResource)/✅Cron/✅Skills(SkillTool+ListSkillTool)/✅TODO(todo_create+todo_list+todo_get+todo_modify)/✅AskUser/✅AgentMode(SwitchMode+EnterPlanMode+ExitPlanMode+TaskToolFactory)/✅BrowserMove(runtime+service+6runtime_tools+rail)/✅SubAgent(TaskTool+3Sessions工具)/✅ToolDiscovery(Search+Load)/Worktree(仅models)/Memory/✅多模态(ImageOCRTool+VQATool+AudioTranscriptionTool+AudioQATool+AudioMetadataTool+VideoUnderstandingTool(incl.VideoModelConfig+ThinkingEnabled+glm-4.6v默认+注册链路回填)+TranscribeAudio接口+ContentPart扩展+Vision/Audio注册回填+覆盖率85.3%)/✅WebTool(WebFreeSearchTool+WebPaidSearchTool+WebFetchWebpageTool+DDG/Bing/Jina/Bocha/Serper/Perplexity+goquery+多编码检测+5个⤵️回填) | `openjiuwen/harness/tools/` |
| 9.50 | ✅ | Workspace 管理 | ✅ WorkspaceNode 枚举（15 值）；✅ DirectoryNode 类型；✅ Workspace 结构体 + NewWorkspace/GetDirectory/SetDirectory/GetNodePath/GetDefaultDirectory 方法；✅ validateDirectoryNode 校验；✅ CN/EN 双语默认模式；✅ 默认目录自动补全；✅ 深拷贝隔离；✅ 27 个单元测试全部通过；✅ SkillToolkit 回填（Code 模式注册 + SkillNet 安装轮询） | `openjiuwen/harness/workspace/` |
| 9.51-53 | ✅ | Harness 资源/Schema/Prompts | ✅ 资源管理(BuiltinRules+ParseBuiltinRules+ParseBuiltinRulesFromFile+ResolveBuiltinRulesYAMLPath+mtime检测自动重载)/Schema 定义/提示词模板 | `openjiuwen/harness/resources/` · `schema/` · `prompts/` |
| **9.x CLI** | — | | | |
| 9.54 | ☐ | CLI REPL | 命令行交互界面 | `openjiuwen/harness/cli/` |
| **9.x TeamAgent 应用层** | — | | | |
| 9.55 | ✅ | TeamAgent | 生产级团队 Agent（CoordinationKernel 完整生命周期 + KernelHost 细粒度窄接口 + 回调签名升级 + Invoke/Stream/Interact/Broadcast/HumanAgentSay + RecoverFromSession + FromSpawnPayload + AutoStart + DestroyTeam + BuildContextFromDB + FindRails + Runner.TeamRuntimeManager 接入 + 回填 9.59/9.60/9.61/9.67 + deep_adapter team 分流） | `openjiuwen/agent_teams/agent/team_agent.py` · `jiuwenswarm/server/runtime/agent_adapter/team_helpers.py` |
| 9.56 | ✅ | Blueprint | 团队蓝图定义 | `openjiuwen/agent_teams/agent/blueprint.py` |
| 9.57 | ✅ | AgentConfigurator | Agent 配置器（骨架+TODO占位） | `openjiuwen/agent_teams/agent/` |
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
| 9.67 | ✅ | Team Observability | OpenTelemetry 集成（Callback/Monitor/Rail 三层；OtelSpanState ctx 注入；UnregisterNamespace 扩展；DeepAgent SpanState 注入；AttachToTeamAgent/DetachFromTeamAgent 回填） | `openjiuwen/agent_teams/observability/` |
| 9.68-69 | ✅ | Team Rails / Prompts | 团队级 Rails / 提示词（9.69 Prompts：18+1 .md 模板 + loader.go go:embed + MtimeSectionCache + 8 Section Builder + RolePolicy + BuildSystemPrompt + team_plan_agent/mode；9.68 Rails：FirstIterationGate channel 替代 asyncio.Event + TeamToolRail P:90 工具注册 + TeamPolicyRail P:12 8 PromptSection 注入 + TeamToolApprovalRail 审批流 + TeamPlanModeRail P:84 plan 模式；14 ToolCard + CreateTeamTools + QualifyTeamToolIDs；回填 harness.go MountedRails any→具体类型 + resources.go + agent_configurator.go Rail 构造 + TODO 清理） | `openjiuwen/agent_teams/rails/` · `prompts/` |
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
| 9.83 | ☐ | Pulsar Message Queue | 分布式消息队列 | `openjiuwen/extensions/message_queue/` |
| 9.84 | ☐ | DistRunner | 分布式 Runner | `openjiuwen/core/runner/drunner/` |
| 9.85 | ✅ | TeamRunner | 团队 Runner（run_agent_team/streaming 三路径 + Activate 内联回填 #9.62 + FinalizeMember + GetMonitor + ListActiveTeams + ⤵️ 回填） | `openjiuwen/core/runner/team_runner.py` |

**验证点**：✅ DeepAgent 可完成复杂编程任务（搜索→规划→编码→验证）

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
| 10.2.9 | ☐ | ACP 适配器 | JSON-RPC ACP 协议桥接 | `jiuwenswarm/common/e2a/adapters.py` · `acp/` |
| 10.2.10 | ☐ | A2A 适配器 | Agent-to-Agent 协议适配 | `jiuwenswarm/common/e2a/adapters.py` |
| **10.3 AgentServer 核心** | — | | | |
| 10.3.1 | ✅ | AgentWebSocketServer | WS 服务端，~56 个 RPC 方法分发（对齐 Python，JiuWenClaw/AgentManager 已 stub） | `jiuwenswarm/server/agent_ws_server.py` |
| 10.3.2 | ✅ | JiuWenClaw 门面 | SDK 路由，会话队列，流式包装，中断处理；✅ 云端记忆钩子(4处)；✅ Team模式判断+BuildInputs三路径+rawQuery替换；✅ Auto-Harness resume绕过；✅ Team中断分流+cancelTeamWork骨架；✅ Dreaming生命周期(CreateInstance/ReloadAgentConfig)；✅ CreateInstance Functional Options+SetSkillnetInstallCompleteHook；⤵️ Team后续请求绕过/TeamManager调用(10.6.19-23) | `jiuwenswarm/server/runtime/agent_adapter/interface.py` |
| 10.3.3 | ✅ | AgentAdapter 接口与工厂 | AgentAdapter ABC，`create_adapter()` | `jiuwenswarm/server/runtime/agent_adapter/agent_adapters.py` |
| 10.3.4-6 | ✅ | 模式适配器 | Agent/Code/Deep 模式适配器；已回填 setCheckpoint/config/dotenv/Model/buildModel/HandleHeartbeat/query注入/模型校验/sessionActive/markSessionActive；✅ 已回填 seedRuntimeCwd；✅ SetLLM统一模型切换(删SwitchModel)；✅ CompressContext+GenerateRecap直接实现(ContextEngine in-memory)；✅ ToolSync5方法直接实现；✅ SlashCommand/evolve分发骨架；✅ subagents骨架(9.25-29)；✅ sysop_builder骨架；✅ 已回填 SubagentRail/ConfirmInterruptRail/Workspace.SetDirectory/ClearMemoryManagerCache(10.6.15后指向swarm包commmem：ClearConfigCache+ClearMemoryManagerCache双清)；⤵️ 标记已精确修正指向（agentcore.DeepAgent/10.6.3-10 Swarm Rails/A2X/11.10 等） | `jiuwenswarm/server/runtime/agent_adapter/interface.py` · `interface_code.py` · `interface_deep.py` |
| 10.3.7-11 | 🔄 | 适配器辅助 | CodeAgentRail✅/TeamHelpers🔄(辅助函数✅/主流程⤵️9.85)/EvolutionHelpers✅/RecapPrompts✅(对齐Python审查修复3处：多模态content提取/callModelForRecap用[]BaseMessage/countFullContextTokens补工具定义token+fallback)/SysOpBuilder✅(一比一对齐Python:BuildFilesystemPolicy+CreateSandboxSysOpCard+CreateLocalSysOpCard+展示辅助+实例化注册)/handleCommandRecap✅+Compact✅(压缩后重取context+summary字段对齐Python)+Context✅连接adapter/DeepAdapter.CreateInstance步骤17✅；✅ 回填审查修复：⤵️标记修正3处(10.3.7-11→9.55-9.65/10.6.3-10)/modelCache类型断言获取/CodeAgentRail热重载/ListAvailableTools动态构建 | `jiuwenswarm/server/runtime/agent_adapter/` |
| 10.3.12 | ✅ | AgentManager | 多实例管理（按通道/模式），含 agentFactory 注入 | `jiuwenswarm/server/runtime/agent_manager.py` |
| 10.3.13 | ✅ | AgentConfigService | Agent 配置 CRUD | `jiuwenswarm/server/runtime/agent_config_service.py` |
| 10.3.14 | ✅ | TenantAgentPool | 多租户 Agent 池化；✅ Singleton[T] 扩展 Reset（自动 Cleanup）；✅ GetInstance/ResetInstance/ProcessMessage/ProcessMessageStream/Cleanup 透传；对齐 Python tenant_agent_pool.py | `jiuwenswarm/server/runtime/tenant_agent_pool.py` |
| 10.3.15-18 | ✅ | 会话管理 | SessionManager(LIFO)✅ / SessionHistory(JSONL)✅ / SessionMetadata✅ / SessionRename✅；✅ 子包提取(server/session/)消除跨包依赖；✅ AppendHistoryRecord 元数据联动(UpdateSessionMetadata+SetSessionDeliveryContext)；✅ ReadTeamHistoryRecords+IsTeamRelevant(team过滤+重试)；✅ TruncateHistoryRecords cutIndex对齐Python；✅ SerializeValue递归序列化；✅ AutoTitle已实现；✅ RemoveTeamModeSessionDirsAtStartup；✅ GetAllSessionsMetadata分页 | `jiuwenswarm/server/runtime/session/` |
| 10.3.19-20 | ✅ | 技能管理 | SkillManager(Server)/SkillDev 管道/SkillToolkit(search_skill+install_skill+uninstall_skill)/DeepAdapter步骤9回填；✅ HandlePluginsEnable/Disable 改用 setPluginEnabled 操作 installed_plugins 对齐 Python；✅ HandlePluginsReload 增加物理目录移动逻辑对齐 Python；✅ isHTTPDownloadTarget 对齐 Python 只检查 scheme+host；✅ matchHost 移除 host==suffix 分支对齐 Python 段数匹配；✅ skillnetNetworkContext 补全小写代理变量和 NO_PROXY 默认值注入对齐 Python；实现偏差：①locateSkillDir递归SKILL.md搜索（对齐Python _locate_skill_dir）②7个方法导出（跨包访问需要大写）③CLAWHUB_BASE_URL环境变量（测试可覆盖性）④SkillNet暂stub仅ClawHub+TeamSkillsHub | `jiuwenswarm/server/runtime/skill/` · `swarm/agents/harness/tools/` |
| 10.3.21-22 | ✅ | GatewayPush | Transport/Wire 服务端推送 | `jiuwenswarm/server/gateway_push/` |
| 10.3.23.1 | ✅ | HookType/HookEvent 常量 | AgentRailEvents/GatewayEvents | `hooks_config.py` HookType/HookEvent |
| 10.3.23.2 | ✅ | 配置模型 | CommandHookConfig/PromptHookConfig/HookMatcher/HooksConfig/LoadHooksConfig | `hooks_config.py` 配置模型 |
| 10.3.23.3 | ✅ | HookExecutor | command/prompt 两类 hook + ParseCommandOutput + ExtractJSONFromResponse；11.13 起对应物迁至 internal/common/hooks/executor.go 供 Gateway/AgentServer 共享 | `executor.py` |
| 10.3.23.4 | ✅ | UserHookRail | embed DeepAgentRail + BeforeToolCall/AfterToolCall/OnToolException/AfterInvoke | `user_hook_rail.py` |
| 10.3.23.5 | ✅ | 回填注册 | DeepAdapter 注册 UserHookRail + handleHooksList 真实实现 | `interface_deep.py` 注册 |
| 10.3.24 | ☐ | Sandbox | 延后 | `sandbox/` |
| 10.3.25 | ✅ | Utils | 完成 | `utils/` + `interrupt/helpers` + `filesystem/history` + `shell/rm_tracker` + `server/utils/diff_service` |
| 10.3.26 | ☐ | 入口 | 延后 | `app_agentserver.py` |
| **10.4 独立交互入口** | — | | | |
| 10.4.1 | ☐ | 🔥 CLI 聊天模式 | 内置 REPL 交互，直接连接 AgentServer，流式输出 | `jiuwenswarm/channels/acp/app_acp.py` (参考) · 新实现 |
| 10.4.2 | ☐ | 🔥 HTTP API | RESTful + SSE 流式 | 新实现 |
| 10.4.3 | ☐ | ACP Stdio | 标准输入输出 JSON-RPC 协议 | `jiuwenswarm/acp/cli.py` · `stdio_client.py` |
| 10.4.4 | 🔄 | Slash 命令处理 | `/mode`/`/new`/`/sandbox`/`/model` 等；/evolve 系列分发骨架已在 adapter 实现 | `jiuwenswarm/gateway/message_handler/command_parser/slash_command.py` |
| 10.4.5 | ☐ | ACP Subprocess Env | 子进程环境设置 | `jiuwenswarm/acp/subprocess_env.py` |
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
| 10.6.11 | ☐ | AutoHarness | 自动化调度 | `jiuwenswarm/agents/harness/common/auto_harness/` |
| 10.6.12 | ✅ | SessionOps | 会话操作；⚠️ 已知依赖方向偏差：sessionops→server/session+utils 为本任务破环迁移遗留（gateway 经 message_handler→sessionops 传递依赖 server），违反规则 6.5，待 server↔agents 边界收敛工程统一处理（11.10 审查登记，非 11.10 引入） | `jiuwenswarm/agents/harness/common/session_ops_service.py` |
| 10.6.13-18 | ✅ | Swarm Memory | Config✅(swarm版config.go+包内缓存+ClearConfigCache回填)/Dreaming✅/Embeddings✅(Provider接口+OpenAICompatible+Mock+工厂，含manager/internal/types依赖链补齐)/External✅(config+builder 迁入 swarm memory 包：engine 门禁+external 段解析+openjiuwen 映射+四路分发 builder；偏差修复：kv 默认 shelve/键名 embed_base_url/ltm mkdir/mem0+openviking env 回退/品牌默认 uapclawswarm/IsExternalMemoryEnabled 双条件/agentarts 移除；adapter 瘦身薄调用，agentName 回退删除)/Forbidden✅/RPC✅(独立子包 common/memoryrpc：5 handler 纯函数层 List/Edit/Status/Toggle/Open+13 个 Python 对齐私有函数+6 个 Go 辅助函数(pathPartContains/expandUser/absoluteJoin/loadConfigMap/dirOrEmpty/anyOr)；白名单校验/UAPCLAWSWARM.md 根特例/品牌映射 jiuwen→uapclaw；Toggle 写回 Load+SetRoundTrip 点路径(D3)对齐 11.14 先例+ruamel round-trip 保注释(M-05：config 包 SetRoundTrip yaml.v3 Node 模式)；Status 六块统计含 detailed 分支 external_memory(engine external/both)且 provider/model nil 对齐 Python None(T-09)；Edit preview splitlines 语义对齐(尾换行/CRLF)；CJK 字符计数 S-03(utf8.RuneCountInString)；mtime float 秒 M-02；mkdir/touch error 字段协议 M-04；写回失败 D6 new_value=old_value；覆盖率 91.5%；⤵️ TUI 面板注册(11.15)) | `jiuwenswarm/agents/harness/common/memory/` |
| 10.6.19 | ✅ | TeamManager 主体 | 72 方法骨架回填+customizer 六步(a-e 实现/f ⤵️10.6.24)+runtime_inheritance(四结构体+双白名单+BuildMemberRails 13 步)+2 rail+exceptions+分布式纯函数层+model_pool 注入+GetDefaultModels 权威收敛三处+CodeMemberConfigurer 注入+buildAndMountMemberRailsForContext 真实实现（updateEvolutionConfig 步骤 6 闭环，审查 M-25 修复）；覆盖率 67.1%（新代码 87-100% 达标；lifecycle/interact/distributed 骨架属 10.6.20-22、stepD/DrainTeamSkillEvents 为集成路径无注入点，不追） | `team_manager.py` |
| 10.6.20 | ☐ | DistributedRuntime | zmq 传输层（✅ 纯函数层已随 10.6.19 带出：IsDistributedMode/RuntimeRole/ParsePort/NormalizeDistributedTransportFields/FallbackDistributedToLocal/PG 五函数）；⤴️ 纯函数层来源 10.6.19 | `distributed_runtime.py` |
| 10.6.21 | ☐ | RemoteMemberBootstrap | 远程成员引导 | `remote_member_bootstrap.py` |
| 10.6.22 | ☐ | A2X RegistryRuntime | A2X 注册运行时（lifecycle ⤵️ 标记已改指本节） | `a2x/` |
| 10.6.23 | ✅ | TeamRails | inheritance+MemberSkillToolkitRail+WorkspaceReportPathRail+exceptions 已随 10.6.19 实现（方案 C 合并决策） | `team_runtime_inheritance.py` · `rails/` |
| 10.6.24 | — | Swarm 内置工具集（2026-10-26 拆分决策：按依赖边界拆 5 子节 a→b→c→d→e，全部归本节不换章节；d 待 11.25 桥接后实施、e 排最后（xiaoyi_gui_agent ⤵️ 9.31）；已实现资产在 24a 建全局接线清单防回填丢失；已核实：cron(11.10)/memory_tools+MemoryRail(10.6.13-18)/skill/web/multimodal/browser_move/todo 基座均在 agentcore 层就绪，10.6.24 仅做 Swarm 侧定制工具+装配接线；browser_tools.py(672行)/swarm audio_tools.py(328行) 疑似零消费死代码，按既定决策仍复刻、待 spec 阶段核实动态引用） | | `jiuwenswarm/agents/harness/common/tools/` |
| 10.6.24a | ✅ | 轻量工具+装配接线 | wiki 3 工具(wiki_ingest/wiki_query/wiki_lint)+generate_image+mcp_toolkits 聚合(GetMcpTools 注入 SkillDevDeps.MCPToolsFactory)+todo_compat 兼容层(status=deleted→delete 映射)；回填桩：getToolCards 步骤1/7、syncToolGroup、uapclaw.go:813、deep_adapter.go:1416(_cancel_pending_todos 标记归属从 10.6.3-10 修正为本节)。已交付：10 commits(b6eb76c9..19c1aa68)——swarmtools 包 7 文件+平台拆分(Windows shell 探测 build tag 隔离)+agentcore todo 注入点(TodoModifyToolFactory)+DeepAdapter 双字段/syncToolGroup Python 同签名/TodoModifyFactory 接线/cancelPendingTodos+formatTodosForFrontendLocal；覆盖率 tools 85.0%/todo 89.5%；spec 8 偏差按定案落地（#2 save 分派外置×2、#5 sysOp 运行时恒 nil、RE2 双限制手动等价、双 _env_flag 保留等） | `wiki_tools.py` · `image_tools.py` · `mcp_toolkits.py` · `todo_compat.py` |
| 10.6.24b | ✅ | send_file 全链 | web_file_download(HMAC 令牌+密钥文件)+SendFileToolkit(chat.file 事件+AppendHistoryRecord+sendPushFunc)+gateway `/file-api/download` 路由；依赖全就绪(11.14✅/session_history✅/sendPushFunc✅)；已交付：6 commits(96bda791..d0b1c3a0)——令牌管理器载荷结构体保序(字节级对齐 Python json.dumps)+SendFileToolkit 双注入(SetSendFilePushFunc/SetSendFileHistoryFunc 启动接线)+两回填桩落地(守卫收窄对齐修正+WithRefresh 有意偏差#2)+quoteAllForDownload 全量百分号编码(对齐 quote(safe=""))；覆盖率 tools 85.8%/gateway 78.5%/adapter 45.2%/team 70.1% 均不低于基线 | `send_file_to_user.py` · `web_file_download.py` · `channels/web/app_web.py`(下载路由) |
| 10.6.24c | ☐ | 会话级工具 | agent.fast MultiSessionToolkit(session_new/cancel/list 子代理并发)+code 模式 user_todos(全局 workspace/channel 状态+按 channel 隔离的 todo 文件)；回填桩：deep_adapter_config.go:144(_update_tools_for_mode)、code_adapter.go:809(标记归属从 10.6.3-10 修正为本节) | `multi_session_toolkits.py` · `user_todo_tool.py` |
| 10.6.24d | ☐ | ACP 工具组 | AcpStdioClient(stdio JSON-RPC)+acp_chat(profile 缓存+会话管理)+acp_output_tools(6个文件/终端工具+get_acp_output_manager 桥接+complete_jsonrpc_response)+_refresh_acp_runtime_tools(按客户端能力装卸)；实施排在 11.25 ACP 桥接后（语义同域不同位：代码在 agent 侧 adapter+ws_server，不换章节）；回填桩：getToolCards 步骤10、code_adapter.go:829 | `acp/stdio_client.py` · `acp_chat/tool.py` · `acp_output_tools.py` |
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
| 11.9 | ✅ | GatewayServer | 多路由 WS 服务器本体已补齐（GatewayServer 类 L240-720：resolveRoute 四分支/双客户端路由表/connection.ack/handleRawMessage 三层处理/Send 四段出站/断连清理）+ InboundGatewayServer 双层 chan + ExternalChannel 出站装配 + 现有 HTTP 宿主改名 AppGatewayServer；ACP/TUI 路由接线 ⤵️ 11.25/11.15 | `jiuwenswarm/gateway/app_gateway.py` |
| 11.10 | ✅ | Cron 调度服务 | go-quartz v0.15.2（croniter→Quartz dow 变换）；CronJobStore tmp+rename 原子写；CronSchedulerService 事件堆三分支（wake/push/push_update）+方案B委托降级单调度器（进程注册表修正 Python 单进程双跑缺陷）；MessagePublisher 接口破环；9渠道元数据全量对齐；CronController+8个cron.job.*端点+handleCronPushPayload；Agent侧CronTools双写+E2A转发+cronToolsBackend 11方法；deep_adapter步骤9绑定+TaskScheduler取消×2；team_manager注册点；app_gateway装配；覆盖率 gateway/cron 85.9% + cronbridge 90.1% | `jiuwenswarm/gateway/cron/` |
| 11.11 | ✅ | 心跳服务 | GatewayHeartbeatService 旁路定时器（先睡后跳循环/tick 六步/三级 content 提取/HEARTBEAT_OK 判定/relay 回传 webhook/活跃时段跨午夜支持）；web 三 RPC（get_conf/set_conf 热重启+config.yaml round-trip 写回+agent 缓存清除/get_path 相对路径）；app_gateway 装配 env 三级回退+Start/Stop 接线；session.list heartbeat_ 过滤（偏差⑨）；9 项偏差归档 spec；heartbeat 包覆盖率 94.2% | `jiuwenswarm/gateway/heartbeat/heartbeat.py` |
| 11.12 | ✅ | IM Pipeline | 独立包双管线完整对齐：IMPlatformAdapter 接口(10 方法+imUserIDNameResolver 可选断言)+IMConversationProcessor(七层分支/Lazy LLM 0.2+0.7/SYSTEM_PROMPT 逐字)+IMInboundPipeline(Apply 十三步/resume 分支/peekPending 模板)+IMOutboundPipeline(十四步短路链/追问前缀 gpq+iact 落盘/classify prompt 逐字/api_key 守卫)；message_handler 双挂接(步骤6+PublishRobotMessages 入队前)+OutboundPipeline 签名改造 Apply(ctx,msg) error；app_gateway 装配步骤4.7；⤵️ 适配器注册 11.16 飞书/11.21 企微；9 偏差归档 spec §8 | `jiuwenswarm/gateway/im_pipeline/` |
| 11.13 | ✅ | Gateway Hook | GatewayHookHandler 4 事件方法（SessionStart 去重/UserPromptSubmit 空 query/SessionEnd 先 discard/Notification type query）+ activeSessions + 10s setdefault + recover 兜底；HookExecutor 迁 server/hooks→common/hooks（第三例破环，user_hook_rail 留守）；forward_loop 步骤3 回填；NewGatewayServer 装配 RegisterConfig | `jiuwenswarm/gateway/hooks/` |
| **11.x IM 渠道** | — | | | |
| 11.14 | ✅ | Web 通道 | WebSocket + HTTP RPC（M-05：config 写回点 updateYAMLKeyInConfig/replaceTeamsInConfig/updateDefaultModelsInConfig/updateMemoryForbiddenInConfig 全部切换 SetRoundTrip ruamel round-trip 保注释；modes.team 删除分支保留 Save——无删除 API 的等价路径） | `jiuwenswarm/gateway/channel_manager/web/` |
| 11.15 | ☐ | TUI 通道 | 终端 UI 交互（⤴️ 需回填来源：10.6.18 memory_rpc 5 个 local handler 注册进 TUI 记忆管理面板） | `jiuwenswarm/gateway/channel_manager/tui/` |
| 11.16 | ☐ | 飞书通道 | 事件回调，企业多机器人 | `jiuwenswarm/gateway/channel_manager/im_platforms/feishu/` |
| 11.17 | ☐ | 钉钉通道 | DingTalk Stream 协议 | `jiuwenswarm/gateway/channel_manager/im_platforms/dingtalk/` |
| 11.18 | ☐ | Telegram 通道 | Telegram Bot API | `jiuwenswarm/gateway/channel_manager/im_platforms/telegram/` |
| 11.19 | ☐ | Discord 通道 | Discord Bot | `jiuwenswarm/gateway/channel_manager/im_platforms/discord/` |
| 11.20 | ☐ | 微信通道 | iLinkAI 桥接 WebSocket | `jiuwenswarm/gateway/channel_manager/im_platforms/wechat/` |
| 11.21 | ☐ | 企微通道 | WeCom AI 机器人 | `jiuwenswarm/gateway/channel_manager/im_platforms/wecom/` |
| 11.22 | ☐ | WhatsApp 通道 | WhatsApp 桥接 | `jiuwenswarm/gateway/channel_manager/im_platforms/whatsapp/` |
| 11.23 | ☐ | 小艺通道 | 华为小艺智能助手 | `jiuwenswarm/gateway/channel_manager/im_platforms/xiaoyi/` |
| 11.24 | ☐ | 平台适配器 | IM 平台通用适配 | `jiuwenswarm/gateway/channel_manager/im_platforms/platform_adapter/` |
| 11.25 | ☐ | ACP 协议桥接 | ACP Gateway Bridge | `jiuwenswarm/gateway/channel_manager/protocol/acp/` |
| 11.26 | ☐ | A2A 协议通道 | A2A Agent 通道 | `jiuwenswarm/gateway/channel_manager/protocol/a2a/` |

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

---

## 领域十三：检索文档索引管理（retrieval/indexing 缺口）

> 文档级索引管线：解析 → 分块 → 嵌入 → 入库。与 7.36 记忆索引（用户记忆 CRUD）不同，本章节处理外部文档（上传文件/链接/图片）的索引构建与维护。
> 2026-10-27 扩充：新增 13.19-13.28 检索查询管线（Retriever 六兄弟 + QueryRewriter + KB 三兄弟，~4600 行源码 + ~4700 行 Python 测试），闭环"查"侧。13.17 TripleExtractor 为首个实施（零前置依赖：TextChunk/llm.Model/json-repair v0.0.18/错误码 155507 均已就绪）。零实现归档项：result_ranking.py（16 行纯 re-export shim，Go graph/ranking.go 已有）+ lazy_load.py/__init__.py（Go 显式 import 天然等价）。
> 2026-10-27 拍板：13.19+13.20 合并实施（接口 63 行无肉+工厂随出，方案 B）；13.21 HybridRetriever 为读取侧下一节。设计：docs/superpowers/specs/2026-10-27-retriever-13.19-13.20-design.md
> 2026-10-28：13.23 GraphRetriever 完成（beam search 图检索+fetch 链+RRF 融合，覆盖率 92.3%）。下一节 13.24 QueryRewriter。设计：docs/superpowers/specs/2026-10-28-graphretriever-13.23-design.md

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
| 13.24 | ☐ | QueryRewriter | 查询压缩/重写（ModelContext 注入，Go 接口 GetMessages/SetMessages 已兼容；4 个 md 提示词逐字复制） | `openjiuwen/core/retrieval/query_rewriter/query_rewriter.py + prompts/` |
| 13.25 | ☐ | AgenticRetriever | 多跳 agentic 检索（消费 TripleMemory + GraphRetriever） | `openjiuwen/core/retrieval/retriever/agentic_retriever.py` |
| **13.x KnowledgeBase 编排层** | — | | | |
| 13.26 | ☐ | KnowledgeBase 抽象 | AddDocuments/Retrieve/Delete/Update/GetStatistics 接口层 | `openjiuwen/core/retrieval/knowledge_base.py` |
| 13.27 | ☐ | SimpleKnowledgeBase | 简单 KB + retrieve_multi_kb / retrieve_multi_kb_with_source；ConfigManager（112 行）随出 | `openjiuwen/core/retrieval/simple_knowledge_base.py + utils/config_manager.py` |
| 13.28 | ☐ | GraphKnowledgeBase | 图增强 KB（★13.17 TripleExtractor 的真实消费方：use_graph 时抽取三元组建 kb_{id}_triples 索引）；⤴️ 消费 13.17 | `openjiuwen/core/retrieval/graph_knowledge_base.py` |

**验证点**：✅ 外部文档可被解析、分块、嵌入、索引，并支持增删改查；☐ 检索查询管线闭环（向量/稀疏/混合/图/agentic 检索 + 多 KB 聚合）（13.19-13.28 完成后达成）

跨域联动：8.25 KnowledgeRetrievalComp ⤴️ 依赖本领域 13.26-13.28；7.36 行 ⤵️ 缺口清单指向领域十三（2026-10-27 扩充后覆盖完整写入+读取管线）

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
| 12.13 | ☐ | 自动更新器 | 版本检测与升级 | `jiuwenswarm/common/updater.py` · `upgrade_executor.py` |
