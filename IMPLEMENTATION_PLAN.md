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

**Transport 抽象接口：**

```go
// AgentTransport Gateway → AgentServer 的传输抽象
type AgentTransport interface {
    Send(ctx context.Context, envelope *E2AEnvelope) error
    Recv() (<-chan *E2AResponse, error)
    Close() error
}
```

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
| 3.8 | ✅ | RestfulApi | HTTP REST 工具，参数映射（path/query/header/body）；✅ **产出**：`tool/service_api/` 包（restful_api.go — RestfulApiCard + RestfulApi + URL/路径参数校验；api_param_mapper.go — APIParamLocation 枚举 + APIParamMapper；response_parser.go — JSON/Text 解析器 + Gzip/Deflate 解压器 + ParserRegistry）；RestfulApiCard 用 InputSchema（map[string]any）替代 InputParams（[]*Param）以支持 location 扩展属性；SchemaUtils 新增 FormatWithSchemaMap 方法；⤵️ **回填点**：FormHandler（form_params 暂 fallback 到 body，3.10 回填）、ToolAuth 回调触发（✅ 3.11 已回填）、UrlUtils SSRF 防护（common/security 迁移后回填） | `openjiuwen/core/foundation/tool/service_api/restful_api.py` |
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
| 4.8 | ✅ | MilvusVectorStore | Milvus 实现（含 Milvus 索引子类型、距离转换；UpdateSchema 待 7.22/7.23 回填） | `openjiuwen/core/foundation/store/vector/milvus_vector_store.py` |
| 4.9 | ✅ | ChromaVectorStore | ChromaDB 实现（含 PersistentClient、fieldMapping、距离转换；UpdateSchema 待 7.22/7.23 回填） | `openjiuwen/core/foundation/store/vector/chroma_vector_store.py` |
| 4.10 | ✅ | GaussVectorStore | GaussDB 向量实现（pgx/v5 pgxpool + DiskANN 索引 + 参数化查询；UpdateSchema 待 7.22/7.23 回填） | `openjiuwen/extensions/store/gauss_vector_store.py` |
| 4.11 | ✅ | ESVectorStore | Elasticsearch 向量实现（go-elasticsearch/v8 + k-NN 搜索 + _meta 文档持久化；UpdateSchema 待 7.22/7.23 回填） | `openjiuwen/extensions/store/es_vector_store.py` |
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
| 5.2 | ✅ | BaseSession 接口 | `Config/State/SessionID/Close`；⤵️ Config 返回类型待 5.12 回填；✅ 5.11 已回填 Tracer 返回类型；✅ 5.10 已回填 StreamWriterManager 返回类型；✅ 5.8 已回填 Checkpointer 返回类型；⤵️ ActorManager 返回类型待后续回填 | `openjiuwen/core/session/session.py` |
| 5.3 | ✅ | AgentSession | `PreRun→Invoke/Stream→PostRun`，状态持久化；✅ 已回填 5.1 Agent State StateCollection；✅ 5.7 已回填 Interaction（SimpleAgentInteraction）；✅ card 具体化为 *schema.AgentCard + AgentID()/GetAgentID()/GetAgentName()/GetAgentDescription() 真实实现；⤵️ Config 返回类型待 5.12 回填；✅ 5.11 已回填 Tracer 返回类型 + agentSpan 类型 + 自动创建逻辑；✅ 5.10 已回填 StreamWriterManager 返回类型；✅ 5.8 已回填 Checkpointer 返回类型；✅ 5.8 已回填 PreRun/Commit 检查点调用；⤵️ ActorManager 返回类型待后续回填 | `openjiuwen/core/session/agent.py` |
| 5.4 | ✅ | WorkflowSession | 从 AgentSession 创建；✅ 已回填 Workflow State StateCollection/CommitState/InMemoryState；✅ 已实现内部 WorkflowSession/NodeSession/SubWorkflowSession；✅ 已实现外部 WorkflowSession 门面；✅ 已回填 AgentSession.CreateWorkflowSession()；⤵️ Config 返回类型待 5.12 回填；✅ 5.11 已回填 Tracer 返回类型；✅ 5.10 已回填 StreamWriterManager 返回类型；✅ 5.8 已回填 Checkpointer 返回类型；⤵️ ActorManager 返回类型待后续回填 | `openjiuwen/core/session/workflow.py` |
| 5.5 | ✅ | SessionNode | ✅ NodeSessionFacade 门面（18 个方法）；✅ Session.GetState 改为 StateKey；✅ AgentStateCollection.GetGlobal/GetAgent 改为 StateKey；✅ 删除 WorkflowSession 多余状态方法；✅ 5.7 已回填 Interaction（WorkflowInteraction）；✅ 5.10 已回填 StreamWriter；✅ 5.11 已回填 Tracer（Trace/TraceError 使用 TracerWorkflowUtils 真实逻辑）；⤵️ 5.12 回填 Config | `openjiuwen/core/session/node.py` |
| 5.6 | ✅ | SessionController | 会话控制器；✅ 已实现 Scope/Subject 体系、DataContainer 工厂、ChainSession、SessionController、GlobalSessionController、回调集成；⤵️ P2P/PubSub 回调待 5.13+ 回填；⤵️ AgentSessionContainer.load 简化处理待后续回填；⤵️ LoadAgentSessionContainer 待 create_agent_session 等价函数后回填 | `openjiuwen/core/session/session_controller/` |
| 5.7 | ✅ | Interaction | ✅ BaseInteraction/WorkflowInteraction/SimpleAgentInteraction/AgentInteraction/InteractiveInput/InteractionOutput/GraphInterrupt/Interrupt/AgentInterrupt 已实现；✅ ExecutableIDProvider 类型断言模式（NodeSession 满足，AgentSession 不满足，与 Python 一致）；✅ 已回填 5.5 NodeSessionFacade.Interact()；✅ 已回填 5.3 Session.Interact() | `openjiuwen/core/session/interaction/` |
| 5.8 | ✅ | Checkpointer 接口 + 工厂 | ✅ Checkpointer/Storage/CheckpointerSession 接口；✅ InMemoryCheckpointer + AgentStorage/AgentTeamStorage/WorkflowStorage；✅ CheckpointerFactory/CheckpointerProvider/CheckpointerFactoryConfig；✅ Serializer 接口 + JSONSerializer；✅ 已回填 5.1 Checkpointer 持久化；✅ 已回填 5.2/5.3/5.4 Checkpointer 返回类型；✅ 已回填 5.3 PreRun/Commit 检查点调用；✅ 已回填 5.7 InteractionCheckpointer 迁移；✅ card 具体化为 *schema.AgentCard + AgentID() 方法；⤵️ 5.12 回填 Config 返回类型；⤵️ 8.7 回填 GraphStore | `openjiuwen/core/session/checkpointer/base.py` · `factory.py` |
| 5.9 | ✅ | PersistenceCheckpointer | 持久化实现；Storage 体系采用接口注入钩子模式（模拟 Python 模板方法：save/recover/clear/exists 固定骨架 + _get_entity_id/_get_state_to_save/_restore_state 钩子）；InMemory 版 Storage 不需要回填，5.9 独立实现 Persistence 版 Storage | `openjiuwen/core/session/checkpointer/persistence.py` |
| 5.10 | ✅ | StreamWriter | ✅ StreamMode 枚举 + Schema 接口 + OutputSchema/TraceSchema/CustomSchema；✅ StreamQueue + StreamEmitter + StreamWriter + StreamWriterManager；✅ 回填 5.2/5.3/5.4 StreamWriterManager 返回类型（any → *stream.StreamWriterManager）；✅ 回填 5.5 NodeSessionFacade.WriteStream/WriteCustomStream()；✅ 回填 Session.WriteStream/WriteCustomStream/StreamIterator/CloseStream；✅ 迁移 InteractionOutputWriterProvider 到 stream 包；✅ R1 normalizeOutputStream + R2 tagStreamPayload(OutputSchema) 已回填；⤵️ R6 close_stream 回调注销待 callback_framework 支持 unregister_event 后回填 | `openjiuwen/core/session/stream/` |
| 5.11 | ✅ | Session Tracer | ✅ 会话追踪已实现；✅ 回填 5.2/5.3/5.4 Tracer 返回类型（any → *tracer.Tracer）；✅ 回填 5.3 AgentSession agentSpan 类型（any → *tracer.TraceAgentSpan）+ 自动创建逻辑；✅ 回填 5.5 NodeSessionFacade.Trace/TraceError()（使用 TracerWorkflowUtils 真实逻辑）；✅ 打破 tracer→internal 循环依赖（decorator.go 用局部接口 tracerSession 替代 *internal.AgentSession） | `openjiuwen/core/session/tracer/` |
| 5.12 | ✅ | Session Config | ✅ SessionConfig 接口 + defaultSessionConfig 默认实现（BuiltinConfigLoader 钩子，同 5.9 EntityHooks 模式）；✅ WorkflowConfigProvider/AgentConfigProvider 接口占位；✅ MetadataLike 预留字段；✅ 三层优先级环境变量加载（os.Getenv > context.Value > 内置默认值）；✅ WithEnvs context 注入；✅ 回填 5.2/5.3/5.4 Config() 返回类型 any→SessionConfig；✅ 回填 5.5 NodeSessionFacade.GetEnv/GetNodeConfig()；✅ 回填 5.8 Checkpointer GetConfigEnv 改用 Config().GetEnv()；✅ 移除 CheckpointerConfigProvider 过渡接口；✅ AgentID() 启用 config.GetAgentConfig().ID() 优先级链；✅ tracerSessionAdapter 解决 tracer↔interfaces 循环依赖 | `openjiuwen/core/session/config/` |
| 5.13 | ✅ | Session Constants | ✅ 配置键名常量（9 个）；✅ 环境变量键名常量（7 个）；✅ 默认值常量（对齐 Python）；✅ EnvConfigKeys 映射表 + EnvConfigTypes 类型映射；✅ BuiltinDefaults() 函数；✅ InteractiveInputKey/ForceDelWorkflowStateKey 从 checkpointer 迁移 | `openjiuwen/core/session/constants.py` |
| 5.14 | ✅ | Session Utils | ✅ 新建 session/utils 独立子包（path/ref/dict/container/string/constants 六文件）；✅ 纯工具函数从 state/utils.go 迁出并导出（20个函数+3个常量）；✅ getBySchema 等 StateKey 依赖函数保留在 state 包；✅ state 包其他文件回填改用 utils 导出函数；✅ 测试迁移（utils 包 89.5% 覆盖率）；✅ create_wrapper_class 不实现（Python 死代码）；✅ EndFrame 标记 ⤵️ 延后到 8.x stream_actor | `openjiuwen/core/session/utils.py` |
| **5.x 上下文引擎** | — | | | |
| 5.15 | ✅ | ModelContext 接口 | `GetMessages/SetMessages/GetContextWindow/Statistic` | `openjiuwen/core/context_engine/base.py` |
| 5.16 | ✅ | ContextWindow / ContextStats | 上下文窗口与统计；✅ Statistic 改为值类型（与 Python ContextStats() 默认实例对齐）；✅ NewContextWindow 构造函数（空切片+零值初始化）；✅ StatMessages/StatTools/StatContextWindow 预留方法签名；⤵️ 5.31 回填统计计算逻辑 | `openjiuwen/core/context_engine/base.py` (ContextWindow/ContextStats) |
| 5.17 | ✅ | ContextEngineConfig | 上下文引擎配置；✅ 纯结构体+Validate()校验；✅ NewContextEngineConfig()构造函数初始化空map；✅ schema/子包与Python对齐 | `openjiuwen/core/context_engine/schema/config.py` |
| 5.18 | ✅ | BaseMessage 接口化 + Offload 消息模型 | ✅ BaseMessage struct→interface + DefaultMessage 默认实现；✅ OffloadInfo + 4 个 Offload 子类型 + Offloadable 接口；✅ NewOffloadMessage 工厂 + IsOffloaded 辅助函数；✅ OffloadAssistantMessage 自定义序列化；✅ UnmarshalMessage + UnmarshalOffloadMessage 反序列化工厂；✅ 全项目适配（model_clients/prompt/store/memory/context_engine/session） | `openjiuwen/core/context_engine/schema/messages.py` |
| 5.19 | ✅ | ContextEvent | ✅ ContextEvent 处理器结果类型（processor/base.go）；✅ ContextCompressionState 压缩状态模型（schema/context_state.go，含 ContextCompressionMetric/Saved/Usage 辅助类型 + CompressionStatus/Phase 枚举 + ContextCompressionStateType 常量）；✅ ContextCallEventType 回调事件（5 种）+ ContextCallEventData（callback/events.go）；✅ CallbackFramework 扩展 OnContext/OffContext/TriggerContext（callback/framework.go） | `openjiuwen/core/context_engine/processor/base.py` + `schema/context_state.py` + `runner/callback/events.py` |
| 5.20 | ✅ | TokenCounter | ✅ TiktokenCounter 实现（基于 tiktoken-go/tokenizer）；✅ model2enc 映射表 + ForModel 降级策略；✅ Count/CountMessages/CountTools 三个方法（含 AssistantMessage ToolCalls 特殊处理）；✅ 两级降级（初始化失败→Cl100kBase→len//4）；✅ contentToString 多模态内容提取；✅ 测试覆盖率 ≥ 85% | `openjiuwen/core/context_engine/token/` |
| 5.21 | ✅ | ContextProcessor 插件 | ✅ ContextProcessor 接口（7个方法：OnAddMessages/OnGetContextWindow/TriggerAddMessages/TriggerGetContextWindow/SaveState/LoadState/ProcessorType）；✅ ProcessorConfig 接口（Validate）；✅ BaseProcessor 结构体 + 默认实现（no-op 透传、不触发、空状态）；✅ ProcessorOption/Option 模式（替代 Python **kwargs）；✅ OffloadMessages 方法族（in_memory/filesystem/fallback）；✅ CompressionUsage 追踪（ExtractUsageMetadata/MergeCompressionUsage/RecordCompressionUsage）；✅ GroupCompletedAPIRounds 函数 + IsAPIRound；✅ 5.24 compressor/ 子包重构：DialogueCompressor/MicroCompactProcessor 迁移至 compressor/ 子包，context_utils/ 删除函数移入 compressor/util.go；⤵️ 9.32 回填 ProcessorOption.SysOperation 类型 + writeOffloadToFile 改用 SysOperation；⤵️ 5.31 回填 ModelContext.OffloadMessages/WorkspaceDir 方法 | `openjiuwen/core/context_engine/processor/base.py` |
| 5.22 | ✅ | DialogueCompressor | ✅ DialogueCompressorConfig（8字段值类型+默认值）；✅ DialogueCompressor 结构体（嵌入 BaseProcessor）；✅ ProcessorFactory 注册表（context_engine.RegisterProcessorFactory/GetProcessorFactory/ListProcessorFactories）；✅ init() 自动注册到全局注册表；✅ WithCompressorModel 注入选项；✅ 核心方法链（TriggerAddMessages/OnAddMessages/GetCompressIdx/FindLastFinalAssistantIdx/GetCompressPairs/BuildCompressTargets/InvokeMultiBlockCompression）；✅ JSON/Fallback 双路径替换（BuildJSONReplacements/BuildFallbackReplacement）；✅ ReplaceMessages 通用替换函数（从后往前避免索引偏移）；✅ 内置压缩提示词（与 Python DEFAULT_COMPRESSION_PROMPT 对齐）；✅ 辅助方法（WrapMemoryBlock/IsValidBlocksPayload/SerializeMessage/EstimateContentTokens/HasCompressionBenefit/BuildSplitContextPayload/BuildTargetsPayload/ExtractCompactSummaryFromReplacements）；✅ 5.24 迁移至 compressor/ 子包；✅ 49 个单元测试全部通过 | `openjiuwen/core/context_engine/processor/compressor/dialogue_compressor.py` |
| 5.23 | ✅ | MicroCompactProcessor | ✅ MicroCompactProcessorConfig（4字段+默认值+Validate校验）；✅ MicroCompactProcessor 结构体（嵌入 BaseProcessor）；✅ ProcessorFactory 自动注册；✅ 核心方法（TriggerAddMessages/OnAddMessages/collectCompactableIndicesByTool/hasAnyToolExceedThreshold/collectFlatIndicesForCompact）；✅ 5.24 context_utils 子包删除，函数移入 compressor/util.go；✅ 5.24 迁移至 compressor/ 子包；✅ SetContent 原地替换 ToolMessage 内容；✅ force 参数支持；✅ 日志同步；✅ 单元测试全部通过 | `openjiuwen/core/context_engine/processor/compressor/micro_compact_processor.py` |
| 5.24 | ✅ | FullCompactProcessor | ✅ FullCompactProcessorConfig（17字段+默认值+Validate校验）；✅ FullCompactProcessor 结构体（嵌入 BaseProcessor）；✅ ProcessorFactory 自动注册；✅ 双路径压缩（Session Memory 优先/LLM 全量压缩回退）；✅ TriggerAddMessages（IsAPIRound + token 超阈值）；✅ OnAddMessages 主入口；✅ 消息识别方法族（_isBoundaryMessage/_isStateMessage/_isSessionMemoryBoundaryMessage/_isSessionMemorySummaryMessage/_isSyntheticMarkerMessage）；✅ 序列化方法（_serializeMessage完整ToolCalls JSON/_serializeMessages/_formatSummary/_buildFallbackSummary）；✅ 消息选择与截断（_selectMessagesToKeep/_adjustStartIndexForToolPairs/_truncateForPromptBudget）；✅ FullCompactStateReinjector + 4 个 Builder（skills/task_status⤵️5.31/plan_mode⤵️5.31/plan空实现）；✅ buildSkillReinjectedContent 完整实现；✅ BASE_COMPACT_PROMPT 完整翻译；✅ _generateSummary + _buildFullCompactMessages LLM 压缩路径；✅ Session Memory 路径骨架（⤵️ 5.31 回填 _loadSessionMemoryRuntime/_loadSessionMemoryText/_resolveSessionMemoryPath/_selectMessagesAfterSessionMemory/_invalidateSessionMemoryAnchor）；✅ compressor/ 子包重构（DialogueCompressor/MicroCompactProcessor 迁移 + context_utils 删除移入 util.go）；✅ 测试覆盖率 85.2% | `openjiuwen/core/context_engine/processor/compressor/full_compact_processor.py` |
| 5.25 | ✅ | CurrentRoundCompressor | ✅ CurrentRoundCompressorConfig（11字段+默认值+Validate校验）；✅ CurrentRoundCompressor 结构体（嵌入 BaseProcessor）；✅ ProcessorFactory 自动注册；✅ TriggerAddMessages（Token 超阈值触发）；✅ OnAddMessages 主入口（MODEL_CALL_FAILED 降级 + CONTEXT_EXECUTION_ERROR）；✅ GetCompressIdx（找最新可压缩 UserMessage 边界）；✅ 两阶段压缩（Compress 增量压缩 + MergeSummaryBlocks 二次合并）；✅ 辅助方法（WrapCurrentRoundMemoryBlock 8 元数据头/UnwrapMemoryBlockSummary/BuildPrompt/FormatRecentContext 排除记忆块/FormatPriorContextAndQuery 过滤工具消息+窗口截断）；✅ 提示词与 Python 完全对齐（DEFAULT_COMPRESSION_PROMPT + CLEAN_PROMPT）；✅ util.go 统一提取（20 个共享函数从 full_compact_processor/dialogue_compressor 迁移 + 新增 IsSummaryMessage/CountMessagesTokens/FindLastCompletedAPIRoundEndIdx/IterSummaryMergeRanges/ParseToolArguments/DescribeToolCall/FindToolResultText/ExtractToolResultHint/ExtractSkillNameFromPath/ExtractSkillFileContent）；✅ 日志同步（9 个日志点）；✅ 测试覆盖率 96% | `openjiuwen/core/context_engine/processor/compressor/current_round_compressor.py` |
| 5.26 | ☐ | RoundLevelCompressor | 轮级压缩器 | `openjiuwen/core/context_engine/processor/` |
| 5.27 | ☐ | MessageOffloader | 消息卸载器 | `openjiuwen/core/context_engine/processor/` |
| 5.28 | ☐ | MessageSummaryOffloader | 消息摘要卸载器 | `openjiuwen/core/context_engine/processor/` |
| 5.29 | ☐ | ToolResultBudgetProcessor | 工具结果预算处理器 | `openjiuwen/core/context_engine/processor/` |
| 5.30 | ☐ | ContextEngine 门面 | 上下文池管理，处理器注册，会话状态持久化 | `openjiuwen/core/context_engine/context_engine.py` |
| 5.31 | ☐ | Context 实现 | 具体上下文实例 | `openjiuwen/core/context_engine/context/` |

**验证点**：✅ 多轮对话上下文自动管理，超长对话自动压缩

---

## 领域六：Agent 核心

> ReAct 循环 — Agent 能"思考与行动"

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **6.x Agent 模型与接口** | — | | | |
| 6.1 | ☐ | AgentCard / AgentResult 模型 | `AgentCard`，`AgentResult{TaskID, Status, Artifacts}`，`Part/Artifact` | `openjiuwen/core/single_agent/schema/agent_card.py` · `agent_result.py` |
| 6.2 | ☐ | BaseAgent 接口 | `Configure/Invoke/Stream`，AbilityManager 挂载 | `openjiuwen/core/single_agent/base.py` |
| 6.3 | ☐ | ReActAgentConfig | ReAct Agent 配置 | `openjiuwen/core/single_agent/agents/react_agent.py` (ReActAgentConfig) |
| **6.x 回调框架** | — | | | |
| 6.4 | ☐ | AgentCallbackEvent 枚举 | 10 种事件类型 | `openjiuwen/core/single_agent/agent_callback_manager.py` |
| 6.5 | ☐ | AgentCallbackContext | 回调上下文，retry/force_finish/steering | `openjiuwen/core/single_agent/agent_callback_manager.py` (AgentCallbackContext) |
| 6.6 | ☐ | AgentCallbackManager | 回调管理器 | `openjiuwen/core/single_agent/agent_callback_manager.py` |
| **6.x Rail 系统** | — | | | |
| 6.7 | ☐ | AgentRail 接口 | 10 个生命周期钩子 | `openjiuwen/core/single_agent/rail/base.py` |
| 6.8 | ☐ | @rail 装饰器等价 | 生命周期钩子注册 | `openjiuwen/core/single_agent/rail/` (@rail decorator) |
| 6.9 | ☐ | Rail Inputs | `InvokeInputs/ModelCallInputs/ToolCallInputs/TaskIterationInputs` | `openjiuwen/core/single_agent/rail/` (Typed inputs) |
| 6.10 | ☐ | ForceFinishRequest / RetryRequest | 提前终止与重试 | `openjiuwen/core/single_agent/rail/` |
| **6.x ReAct Agent** | — | | | |
| 6.11 | ☐ | ReActAgent 实现 | ReAct 循环：Think→Act→Observe，最大迭代次数 | `openjiuwen/core/single_agent/agents/react_agent.py` |
| 6.12 | ☐ | 流式输出 | `_inner_stream`，实时推送 Agent 思考过程 | `openjiuwen/core/single_agent/agents/react_agent.py` (_inner_stream) |
| 6.13 | ☐ | KV Cache 释放 | 上下文缓存管理 | `openjiuwen/core/single_agent/agents/react_agent.py` (kv_cache_release) |
| **6.x 中断/恢复系统** | — | | | |
| 6.14 | ☐ | ToolInterruptHandler | 工具级中断管理 | `openjiuwen/core/single_agent/interrupt/handler.py` |
| 6.15 | ☐ | InterruptionState / ToolInterruptionState | 工作流/工具中断状态 | `openjiuwen/core/single_agent/interrupt/` |
| 6.16 | ☐ | ResumeContext | 恢复上下文 | `openjiuwen/core/single_agent/interrupt/` |
| **6.x 技能系统** | — | | | |
| 6.17 | ☐ | Skill 模型 | `Skill{Name, Description, Directory}` | `openjiuwen/core/single_agent/skills/skill_manager.py` (Skill) |
| 6.18 | ☐ | SkillManager | 注册/注销/获取技能，YAML front-matter 加载 | `openjiuwen/core/single_agent/skills/skill_manager.py` |
| **6.x 事件驱动 Controller** | — | | | |
| 6.19 | ☐ | Controller | 事件驱动任务编排 | `openjiuwen/core/controller/base.py` |
| 6.20 | ☐ | TaskManager / EventQueue | 任务与事件队列 | `openjiuwen/core/controller/` |
| 6.21 | ☐ | TaskScheduler / EventHandler | 调度器与事件处理器 | `openjiuwen/core/controller/` |
| 6.22 | ☐ | ControllerAgent | 基于 Controller 的 Agent | `openjiuwen/core/single_agent/base.py` (ControllerAgent) |
| **6.x Runner 编排** | — | | | |
| 6.23 | ☐ | ResourceMgr | Agent/Tool/Workflow/Model/Prompt 全局注册表 | `openjiuwen/core/runner/resources_manager/resource_manager.py` |
| 6.24 | ☐ | AsyncCallbackFramework | 事件注册/触发/emit_before/emit_after/transform_io | `openjiuwen/core/runner/callback/` |
| 6.25 | ☐ | Runner 单例 | `RunAgent/RunAgentStreaming/RunWorkflow/SpawnAgent` | `openjiuwen/core/runner/runner.py` |
| 6.26 | ☐ | RunnerConfig | Runner 配置（distributed_mode 等） | `openjiuwen/core/runner/runner_config.py` |
| 6.27 | ☐ | LocalMessageQueue | 本地消息队列 | `openjiuwen/core/runner/message_queue_inmemory.py` |
| 6.28 | ☐ | Spawn 子进程 | JSON over stdin/stdout，`SpawnedProcessHandle` | `openjiuwen/core/runner/spawn/` |
| 6.29 | ☐ | Agent Prompts | Agent 系统提示词模板 | `openjiuwen/core/single_agent/prompts/` |

**验证点**：✅ 完整 ReAct 循环可用：用户提问 → Agent 思考 → 调用工具 → 返回结果

---

## 领域七：记忆、安全与检索

> Agent 拥有"记忆"能力与安全护栏

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **7.x 记忆系统** | — | | | |
| 7.1 | ☐ | CodingMemoryManager | 轻量编程记忆，frontmatter 存储，冲突检测 | `openjiuwen/core/memory/lite/manager.py` |
| 7.2 | ☐ | CodingMemoryTools | 编程记忆工具（读写搜索） | `openjiuwen/core/memory/lite/coding_memory_tools.py` |
| 7.3 | ☐ | CodingMemoryToolContext | 编程记忆工具上下文 | `openjiuwen/core/memory/lite/coding_memory_tool_context.py` |
| 7.4 | ☐ | MemoryConfig | 记忆配置 | `openjiuwen/core/memory/lite/config.py` |
| 7.5 | ☐ | Frontmatter 解析 | YAML frontmatter 读写 | `openjiuwen/core/memory/lite/frontmatter.py` |
| 7.6 | ☐ | FragmentMemoryManager | 片段记忆管理 | `openjiuwen/core/memory/manage/` |
| 7.7 | ☐ | SummaryManager / VariableManager | 摘要与变量管理 | `openjiuwen/core/memory/manage/` |
| 7.8 | ☐ | WriteManager / SearchManager | 写入与搜索管理 | `openjiuwen/core/memory/manage/update/` · `search/` |
| 7.9 | ☐ | Memory DB Models | MemoryUnit/DataIdManager/SemanticStore 等 | `openjiuwen/core/memory/manage/mem_model/` |
| 7.10 | ☐ | Memory Index | 记忆索引 | `openjiuwen/core/memory/manage/index/` |
| 7.11 | ☐ | GraphMemory | 实体抽取，三元组存储 | `openjiuwen/core/memory/graph/graph_memory/` |
| 7.12 | ☐ | Graph Extraction | 图实体抽取 | `openjiuwen/core/memory/graph/extraction/` |
| 7.13 | ☐ | MemoryProvider 协议 | 外部 Memory Provider 接口 | `openjiuwen/core/memory/external/provider.py` |
| 7.14 | ☐ | Mem0Provider | Mem0 适配 | `openjiuwen/core/memory/external/mem0_provider.py` |
| 7.15 | ☐ | OpenVikingProvider | OpenViking 适配 | `openjiuwen/core/memory/external/openviking_memory_provider.py` |
| 7.16 | ☐ | OpenJiuwenMemoryProvider | openjiuwen LTM 适配 | `openjiuwen/core/memory/external/openjiuwen_memory_provider.py` |
| 7.17 | ☐ | AgentArtsMemoryProvider | AgentArts 适配 | `openjiuwen/core/memory/external/agentarts_memory_provider.py` |
| 7.18 | ☐ | LongTermMemoryExtractor | 长期记忆提取 | `openjiuwen/core/memory/process/extract/` |
| 7.19 | ☐ | MemoryAnalyzer / Refiner | 记忆精炼 | `openjiuwen/core/memory/process/refine/` |
| 7.20 | ☐ | Dreaming Orchestrator | 后台记忆整理编排器 | `openjiuwen/core/memory/dreaming/orchestrator.py` |
| 7.21 | ☐ | MigrationPlan | 迁移计划 | `openjiuwen/core/memory/migration/migration_plan.py` |
| 7.22 | ☐ | Migration Operations | 迁移操作注册表（⤴️ 需回填 MilvusVectorStore.UpdateSchema） | `openjiuwen/core/memory/migration/operation/` |
| 7.23 | ☐ | Migration Migrators | KV/SQL/Vector/Index/Message 迁移器（⤴️ 需回填 MilvusVectorStore.UpdateSchema） | `openjiuwen/core/memory/migration/migrator/` |
| 7.24 | ☐ | Memory Codec | 记忆编解码 | `openjiuwen/core/memory/codec/` |
| 7.25 | ☐ | Memory Common | 记忆公共工具 | `openjiuwen/core/memory/common/` |
| 7.26 | ☐ | Memory Prompts | 记忆提示词 | `openjiuwen/core/memory/prompts/` |
| 7.27 | ☐ | LongTermMemory | 长期记忆模块 | `openjiuwen/core/memory/long_term_memory.py` |
| **7.x 安全护栏** | — | | | |
| 7.28 | ☐ | BaseGuardrail 接口 | `ExtractContext/Detect/Register/Unregister` | `openjiuwen/core/security/guardrail/guardrail.py` |
| 7.29 | ☐ | GuardrailBackend | `Analyze(ctx)→RiskAssessment` | `openjiuwen/core/security/guardrail/backends.py` |
| 7.30 | ☐ | GuardrailResult / RiskAssessment | 安全检测结果模型 | `openjiuwen/core/security/guardrail/` (models) |
| 7.31 | ☐ | PromptInjectionGuardrail | 提示注入检测 | `openjiuwen/core/security/guardrail/` |
| 7.32 | ☐ | JailbreakGuardrail | 越狱检测 | `openjiuwen/core/security/guardrail/` |
| 7.33 | ☐ | Security Schema | 安全数据模型 | `openjiuwen/core/security/schema/` |
| **7.x 检索系统** | — | | | |
| 7.34 | ☐ | 检索嵌入适配 | OpenAI/DashScope/API/VLLM Embedding | `openjiuwen/core/foundation/store/base_embedding.py` 及实现 |
| 7.35 | ☐ | 检索重排序适配 | Standard/Chat/DashScope Reranker | `openjiuwen/core/foundation/store/base_reranker.py` 及实现 |
| 7.36 | ☐ | 检索索引 | 向量索引编排 | `openjiuwen/core/foundation/store/index/` |

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
| 8.27 | ☐ | BaseTeam 接口 | `AddAgent/RemoveAgent/Send/Publish/Subscribe/Invoke/Stream` | `openjiuwen/core/multi_agent/team.py` |
| 8.28 | ☐ | TeamCard / TeamConfig | 团队元数据与配置 | `openjiuwen/core/multi_agent/schema/team_card.py` |
| 8.29 | ☐ | EventDrivenTeamCard | 事件驱动团队卡片 | `openjiuwen/core/multi_agent/schema/` |
| 8.30 | ☐ | TeamRuntime | 消息总线，P2P 通信 | `openjiuwen/core/multi_agent/team_runtime/` |
| 8.31 | ☐ | CommunicableAgent | 可通信 Agent 包装 | `openjiuwen/core/multi_agent/team_runtime/` |
| 8.32 | ☐ | MessageRouter / SubscriptionManager | 消息路由与订阅 | `openjiuwen/core/multi_agent/team_runtime/` |
| 8.33 | ☐ | MessageBus | 消息总线 | `openjiuwen/core/multi_agent/team_runtime/` |
| 8.34 | ☐ | HandoffTeam | 单活跃 Agent 交接模式 | `openjiuwen/core/multi_agent/teams/handoff/` |
| 8.35 | ☐ | HierarchicalTeam (msgbus) | 层级管理-消息总线模式 | `openjiuwen/core/multi_agent/teams/` |
| 8.36 | ☐ | HierarchicalTeam (tools) | 层级管理-工具委托模式 | `openjiuwen/core/multi_agent/teams/` |

**验证点**：✅ 可定义和执行工作流 DAG，多 Agent 协作完成任务

---

## 领域九：DeepAgent 应用层 (Harness)

> 面向生产的高级 Agent 封装

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **9.x DeepAgent** | — | | | |
| 9.1 | ☐ | DeepAgent | 包装 ReActAgent + 任务循环 + Rails + 技能 + 子 Agent | `openjiuwen/harness/deep_agent.py` |
| 9.2 | ☐ | DeepAgentConfig | DeepAgent 配置 | `openjiuwen/harness/harness_config/` |
| 9.3 | ☐ | DeepAgent Factory | 创建 DeepAgent 实例 | `openjiuwen/harness/factory.py` |
| 9.4 | ☐ | TaskLoopController | 任务循环控制器 | `openjiuwen/harness/task_loop/` |
| 9.5 | ☐ | LoopCoordinator | 循环协调器 | `openjiuwen/harness/task_loop/` |
| 9.6 | ☐ | TaskLoopEventExecutor | 任务循环事件执行器 | `openjiuwen/harness/task_loop/` |
| 9.7 | ☐ | SessionSpawnExecutor | 会话子进程执行器 | `openjiuwen/harness/task_loop/` |
| **9.x 安全 Rails** | — | | | |
| 9.8 | ☐ | ShellAST 分析 | Shell 命令 AST 解析与安全分析 | `openjiuwen/harness/security/` |
| 9.9 | ☐ | TieredPolicy | 分层安全策略 | `openjiuwen/harness/security/` |
| 9.10 | ☐ | 主机校验 | URL/域名白名单 | `openjiuwen/harness/security/` |
| **9.x 功能 Rails** | — | | | |
| 9.11 | ☐ | ProgressiveToolRail | 渐进式工具权限 | `openjiuwen/harness/rails/` |
| 9.12 | ☐ | TaskCompletionRail | 任务完成检测 | `openjiuwen/harness/rails/` |
| 9.13 | ☐ | TaskPlanningRail | 任务规划 | `openjiuwen/harness/rails/` |
| 9.14 | ☐ | AgentModeRail | Agent 模式切换 | `openjiuwen/harness/rails/` |
| 9.15 | ☐ | HeartbeatRail | 心跳 | `openjiuwen/harness/rails/` |
| 9.16 | ☐ | McpRail | MCP 工具管理 | `openjiuwen/harness/rails/` |
| 9.17 | ☐ | LSPRail | LSP 集成 | `openjiuwen/harness/lsp/` |
| 9.18 | ☐ | SysOperationRail | 系统操作管理 | `openjiuwen/harness/rails/` |
| 9.19-24 | ☐ | 其他 Rails | Security/Interrupt/Skill/ContextEngine/Memory/Evolution Rails | `openjiuwen/harness/rails/` |
| **9.x 子 Agent** | — | | | |
| 9.25 | ☐ | ResearchAgent | 研究子 Agent | `openjiuwen/harness/subagents/` |
| 9.26 | ☐ | BrowserAgent | 浏览器子 Agent | `openjiuwen/harness/subagents/` |
| 9.27 | ☐ | CodeAgent | 编码子 Agent | `openjiuwen/harness/subagents/` |
| 9.28 | ☐ | PlanAgent | 规划子 Agent | `openjiuwen/harness/subagents/` |
| 9.29 | ☐ | VerificationAgent | 验证子 Agent | `openjiuwen/harness/subagents/` |
| 9.30 | ☐ | ExploreAgent | 探索子 Agent | `openjiuwen/harness/subagents/` |
| 9.31 | ☐ | MobileGUIAgent | 移动端 GUI Agent | `openjiuwen/harness/subagents/` |
| **9.x 系统操作** | — | | | |
| 9.32 | ☐ | SysOperation 接口 | 系统操作抽象 | `openjiuwen/core/sys_operation/` |
| 9.33 | ☐ | LocalSysOperation | 本地执行 | `openjiuwen/core/sys_operation/` |
| 9.34 | ☐ | SandboxSysOperation | 沙箱执行 | `openjiuwen/core/sys_operation/` |
| 9.35 | ☐ | Shell Process Registry | Shell 进程管理 | `openjiuwen/core/sys_operation/` |
| 9.36 | ☐ | JiuwenBoxProvider | JiuwenBox 沙箱 Provider | `openjiuwen/extensions/sys_operation/` |
| 9.37 | ☐ | AioProvider | Aio 沙箱 Provider | `openjiuwen/extensions/sys_operation/` |
| **9.x 内置工具集** | — | | | |
| 9.38-49 | ☐ | Harness 工具集 | Shell/文件系统/代码/MCP/Worktree/浏览器/Cron/TODO/AskUser/Memory/AgentMode/多模态 | `openjiuwen/harness/tools/` |
| 9.50 | ☐ | Workspace 管理 | 工作空间管理 | `openjiuwen/harness/workspace/` |
| 9.51-53 | ☐ | Harness 资源/Schema/Prompts | 资源管理/Schema 定义/提示词模板 | `openjiuwen/harness/resources/` · `schema/` · `prompts/` |
| **9.x CLI** | — | | | |
| 9.54 | ☐ | CLI REPL | 命令行交互界面 | `openjiuwen/harness/cli/` |
| **9.x TeamAgent 应用层** | — | | | |
| 9.55 | ☐ | TeamAgent | 生产级团队 Agent | `openjiuwen/agent_teams/agent/team_agent.py` |
| 9.56 | ☐ | Blueprint | 团队蓝图定义 | `openjiuwen/agent_teams/agent/blueprint.py` |
| 9.57 | ☐ | AgentConfigurator | Agent 配置器 | `openjiuwen/agent_teams/agent/` |
| 9.58 | ☐ | SpawnManager | 子进程管理 | `openjiuwen/agent_teams/spawn/` |
| 9.59 | ☐ | SessionManager | 团队会话管理 | `openjiuwen/agent_teams/interaction/` |
| 9.60 | ☐ | StreamController | 流式控制 | `openjiuwen/agent_teams/` |
| 9.61 | ☐ | RecoveryManager | 恢复管理 | `openjiuwen/agent_teams/` |
| 9.62 | ☐ | CoordinationKernel | 协调内核 | `openjiuwen/agent_teams/` |
| 9.63 | ☐ | EventBus / Dispatcher | 事件总线与分发 | `openjiuwen/agent_teams/` |
| 9.64 | ☐ | Team Memory | 共享记忆 | `openjiuwen/agent_teams/memory/` |
| 9.65 | ☐ | Team Messager | 团队消息（inprocess/ZMQ） | `openjiuwen/agent_teams/messager/` |
| 9.66 | ☐ | Team Workspace | 团队工作空间 | `openjiuwen/agent_teams/team_workspace/` |
| 9.67 | ☐ | Team Observability | OpenTelemetry 集成 | `openjiuwen/agent_teams/observability/` |
| 9.68-69 | ☐ | Team Rails / Prompts | 团队级 Rails / 提示词 | `openjiuwen/agent_teams/rails/` · `prompts/` |
| **9.x 自演化系统** | — | | | |
| 9.70 | ☐ | Trainer | 训练器 | `openjiuwen/agent_evolving/trainer/` |
| 9.71 | ☐ | BaseEvaluator | 评估器 | `openjiuwen/agent_evolving/evaluator/` |
| 9.72 | ☐ | InstructionOptimizer | 指令优化器 | `openjiuwen/agent_evolving/optimizer/` |
| 9.73 | ☐ | SignalDetector | 信号检测 | `openjiuwen/agent_evolving/signal/` |
| 9.74-76 | ☐ | RL 子系统 | OfflineRL/OnlineRL/RewardRegistry | `openjiuwen/agent_evolving/agent_rl/` |
| 9.77-80 | ☐ | 演化支撑 | Trajectory/EvolveCheckpoint/Experience/UpdateExecution | `openjiuwen/agent_evolving/trajectory/` · `checkpointing/` · `experience/` · `update_execution.py` |
| **9.x 扩展系统** | — | | | |
| 9.81 | ☐ | A2A 扩展 | A2AServer/A2AClient/A2ARemoteClient/A2AServerAdapter | `openjiuwen/extensions/a2a/` |
| 9.82 | ☐ | Context Evolver | 自演化上下文（轨迹分析） | `openjiuwen/extensions/context_evolver/` |
| 9.83 | ☐ | Pulsar Message Queue | 分布式消息队列 | `openjiuwen/extensions/message_queue/` |
| 9.84 | ☐ | DistRunner | 分布式 Runner | `openjiuwen/core/runner/drunner/` |
| 9.85 | ☐ | TeamRunner | 团队 Runner | `openjiuwen/core/runner/team_runner.py` |

**验证点**：✅ DeepAgent 可完成复杂编程任务（搜索→规划→编码→验证）

---

## 领域十：AgentServer + 独立交互入口

> 本领域的核心价值：所有外部入口统一经过 Gateway，Gateway 与 AgentServer 之间始终走 E2A 协议
>
> chat/serve/acp 作为"轻量 Channel"接入 Gateway，而非绕过 Gateway 直连 AgentServer

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **10.1 Schema 层** | — | | | |
| 10.1.1 | ☐ | ReqMethod 枚举 | ~100 个 RPC 方法名 | `jiuwenswarm/common/schema/message.py` (ReqMethod) |
| 10.1.2 | ☐ | EventType 枚举 | 事件类型 | `jiuwenswarm/common/schema/message.py` (EventType) |
| 10.1.3 | ☐ | Mode 枚举 | 运行模式（agent.plan/code.normal/team 等） | `jiuwenswarm/common/schema/message.py` (Mode) |
| 10.1.4 | ☐ | Message 模型 | 内部消息格式 | `jiuwenswarm/common/schema/message.py` (Message) |
| 10.1.5 | ☐ | AgentRequest / AgentResponse | Agent 请求与响应模型 | `jiuwenswarm/common/schema/agent.py` |
| 10.1.6 | ☐ | AgentResponseChunk | Agent 流式响应块 | `jiuwenswarm/common/schema/agent.py` |
| 10.1.7 | ☐ | HookEventBase | 钩子事件基类 | `jiuwenswarm/common/schema/event_base.py` |
| 10.1.8 | ☐ | PermissionContext | 权限上下文 | `jiuwenswarm/common/schema/agent.py` |
| **10.2 E2A 协议** | — | | | |
| 10.2.1 | ☐ | E2AEnvelope | 请求信封 | `jiuwenswarm/common/e2a/models.py` (E2AEnvelope) |
| 10.2.2 | ☐ | E2AResponse | 响应模型 | `jiuwenswarm/common/e2a/models.py` (E2AResponse) |
| 10.2.3 | ☐ | E2AProvenance / E2AAuth | 来源标识与认证 | `jiuwenswarm/common/e2a/models.py` |
| 10.2.4 | ☐ | E2AFileRef / IdentityOrigin | 文件引用与身份来源 | `jiuwenswarm/common/e2a/models.py` |
| 10.2.5 | ☐ | Wire Codec | E2A ↔ AgentResponse/AgentResponseChunk 编解码 | `jiuwenswarm/common/e2a/wire_codec.py` |
| 10.2.6 | ☐ | E2A Constants | 协议常量 | `jiuwenswarm/common/e2a/constants.py` |
| 10.2.7 | ☐ | gateway_normalize | Message/E2A/AgentResponse 格式互转 | `jiuwenswarm/common/e2a/gateway_normalize.py` |
| 10.2.8 | ☐ | agent_compat | `e2a_to_agent_request()` 转换 | `jiuwenswarm/common/e2a/agent_compat.py` |
| 10.2.9 | ☐ | ACP 适配器 | JSON-RPC ACP 协议桥接 | `jiuwenswarm/common/e2a/adapters.py` · `acp/` |
| 10.2.10 | ☐ | A2A 适配器 | Agent-to-Agent 协议适配 | `jiuwenswarm/common/e2a/adapters.py` |
| **10.3 AgentServer 核心** | — | | | |
| 10.3.1 | ☐ | AgentWebSocketServer | WS 服务端，~100 个 RPC 方法分发 | `jiuwenswarm/server/agent_ws_server.py` |
| 10.3.2 | ☐ | JiuWenClaw 门面 | SDK 路由，会话队列，流式包装，中断处理 | `jiuwenswarm/server/runtime/agent_adapter/interface.py` |
| 10.3.3 | ☐ | AgentAdapter 接口与工厂 | AgentAdapter ABC，`create_adapter()` | `jiuwenswarm/server/runtime/agent_adapter/agent_adapters.py` |
| 10.3.4-6 | ☐ | 模式适配器 | Agent/Code/Deep 模式适配器 | `jiuwenswarm/server/runtime/agent_adapter/interface.py` · `interface_code.py` · `interface_deep.py` |
| 10.3.7-11 | ☐ | 适配器辅助 | CodeAgentRail/TeamHelpers/EvolutionHelpers/RecapPrompts/SysOpBuilder | `jiuwenswarm/server/runtime/agent_adapter/` |
| 10.3.12 | ☐ | AgentManager | 多实例管理（按通道/模式） | `jiuwenswarm/server/runtime/agent_manager.py` |
| 10.3.13 | ☐ | AgentConfigService | Agent 配置 CRUD | `jiuwenswarm/server/runtime/agent_config_service.py` |
| 10.3.14 | ☐ | TenantAgentPool | 多租户 Agent 池化 | `jiuwenswarm/server/runtime/tenant_agent_pool.py` |
| 10.3.15-18 | ☐ | 会话管理 | SessionManager(LIFO)/SessionHistory(JSONL)/SessionMetadata/SessionRename | `jiuwenswarm/server/runtime/session/` |
| 10.3.19-20 | ☐ | 技能管理 | SkillManager(Server)/SkillDev 管道 | `jiuwenswarm/server/runtime/skill/` |
| 10.3.21-22 | ☐ | GatewayPush | Transport/Wire 服务端推送 | `jiuwenswarm/server/gateway_push/` |
| 10.3.23-26 | ☐ | 服务端辅助 | Hooks/Sandbox/Utils/入口 | `jiuwenswarm/server/hooks/` · `sandbox/` · `utils/` · `app_agentserver.py` |
| **10.4 独立交互入口** | — | | | |
| 10.4.1 | ☐ | 🔥 CLI 聊天模式 | 内置 REPL 交互，直接连接 AgentServer，流式输出 | `jiuwenswarm/channels/acp/app_acp.py` (参考) · 新实现 |
| 10.4.2 | ☐ | 🔥 HTTP API | RESTful + SSE 流式 | 新实现 |
| 10.4.3 | ☐ | ACP Stdio | 标准输入输出 JSON-RPC 协议 | `jiuwenswarm/acp/cli.py` · `stdio_client.py` |
| 10.4.4 | ☐ | Slash 命令处理 | `/mode`/`/new`/`/sandbox`/`/model` 等 | `jiuwenswarm/gateway/message_handler/command_parser/slash_command.py` |
| 10.4.5 | ☐ | ACP Subprocess Env | 子进程环境设置 | `jiuwenswarm/acp/subprocess_env.py` |
| **10.5 扩展系统** | — | | | |
| 10.5.1-10 | ☐ | 扩展框架 | BaseExtension/Registry/Manager/Hooks/Loader/Types | `jiuwenswarm/extensions/` |
| **10.6 Swarm 侧 Harness 集成** | — | | | |
| 10.6.1-2 | ☐ | Prompt Builder | Agent/Code 模式提示词 | `jiuwenswarm/agents/harness/common/prompt/` · `code/prompt/` |
| 10.6.3-10 | ☐ | Swarm Rails | AskUser/Avatar/Permissions/Interrupt/ProjectMemory/ResponsePrompt/RuntimePrompt/StreamEvent | `jiuwenswarm/agents/harness/common/rails/` |
| 10.6.11-12 | ☐ | AutoHarness + SessionOps | 自动化调度/会话操作 | `jiuwenswarm/agents/harness/common/auto_harness/` · `session_ops_service.py` |
| 10.6.13-18 | ☐ | Swarm Memory | Config/Dreaming/Embeddings/External/Forbidden/RPC | `jiuwenswarm/agents/harness/common/memory/` |
| 10.6.19-23 | ☐ | Swarm Team | TeamManager/Bootstrap/DistributedRuntime/A2X/TeamRails | `jiuwenswarm/agents/harness/team/` |
| 10.6.24 | ☐ | Swarm 内置工具集 | 浏览器/MCP/搜索/视频/发文件/TODO/Cron/小艺电话 | `jiuwenswarm/agents/harness/common/tools/` |

**验证点**：✅ 用户运行 `uapclaw chat` 即可与 Agent 对话（REPL → Gateway → E2A → AgentServer）

---

## 领域十一：Gateway + IM 渠道

> 多 IM 渠道接入（可选，依赖领域十）

| 步骤 | 状态 | 内容 | 产出 | Python 参考路径 |
|------|------|------|------|-----------------|
| **11.x Gateway 核心** | — | | | |
| 11.1 | ☐ | BaseChannel 接口 | `Config/Start/Stop/Send/OnMessage` | `jiuwenswarm/gateway/channel_manager/base.py` |
| 11.2 | ☐ | ChannelManager | 注册/注销/分发/配置热更新回调 | `jiuwenswarm/gateway/channel_manager/channel_manager.py` |
| 11.3 | ☐ | MessageHandler | 入站→AgentServer，出站→Channel | `jiuwenswarm/gateway/message_handler/message_handler.py` |
| 11.4 | ☐ | Slash Command Parser | Slash 命令解析 | `jiuwenswarm/gateway/message_handler/command_parser/slash_command.py` |
| 11.5 | ☐ | WebSocketAgentServerClient | WS 客户端，请求/响应路由，流式，自动重连 | `jiuwenswarm/gateway/routing/agent_client.py` |
| 11.6 | ☐ | RouteBinding | 路由绑定 | `jiuwenswarm/gateway/routing/route_binding.py` |
| 11.7 | ☐ | SessionMap | 会话映射 | `jiuwenswarm/gateway/routing/session_map.py` |
| 11.8 | ☐ | InteractionContext | 交互上下文 | `jiuwenswarm/gateway/routing/interaction_context.py` |
| 11.9 | ☐ | GatewayServer | 多路由 WS 服务器组装 | `jiuwenswarm/gateway/app_gateway.py` |
| 11.10 | ☐ | Cron 调度服务 | Cron 表达式，Job 持久化，调度执行 | `jiuwenswarm/gateway/cron/` |
| 11.11 | ☐ | 心跳服务 | 定时心跳，活跃时段控制 | `jiuwenswarm/gateway/heartbeat/heartbeat.py` |
| 11.12 | ☐ | IM Pipeline | 数字人入站/出站管道 | `jiuwenswarm/gateway/im_pipeline/` |
| 11.13 | ☐ | Gateway Hook | 钩子处理 | `jiuwenswarm/gateway/hooks/` |
| **11.x IM 渠道** | — | | | |
| 11.14 | ☐ | Web 通道 | WebSocket + HTTP RPC | `jiuwenswarm/gateway/channel_manager/web/` |
| 11.15 | ☐ | TUI 通道 | 终端 UI 交互 | `jiuwenswarm/gateway/channel_manager/tui/` |
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
| 12.7 | ☐ | 统一启动器 | `uapclaw app`：同时启动 AgentServer + Gateway | `jiuwenswarm/app.py` |
| 12.8 | ☐ | AgentServer 启动 | `uapclaw agentserver` | `jiuwenswarm/server/app_agentserver.py` |
| 12.9 | ☐ | Gateway 启动 | `uapclaw gateway` | `jiuwenswarm/gateway/app_gateway.py` |
| 12.10 | ☐ | Web UI 启动 | `uapclaw web` | `jiuwenswarm/channels/web/app_web.py` |
| 12.11 | ☐ | 工作区初始化 | `uapclaw init` | `jiuwenswarm/init_workspace.py` |
| 12.12 | ☐ | 服务启动器 | `uapclaw start` | `jiuwenswarm/start_services.py` |
| 12.13 | ☐ | 自动更新器 | 版本检测与升级 | `jiuwenswarm/common/updater.py` · `upgrade_executor.py` |
