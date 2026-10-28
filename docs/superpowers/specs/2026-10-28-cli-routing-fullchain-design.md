# CLI 路由全链路往返集成测试 + 11.15 最小生产装配 设计文档

- 日期：2026-10-28
- 状态：待审查
- 对齐 Python 源：`tests/system_tests/test_cli_channel_ws.py` + `jiuwenswarm/gateway/channel_manager/tui/tui_connect.py`（路由装配部分）+ `jiuwenswarm/gateway/app_gateway.py` L1037-1069
- 关联任务：补齐 Go 集成测试缺口（对比 jiuwenswarm system_tests 后选定的第 ① 项）

## 1. 背景与目标

### 1.1 背景

对比 Python jiuwenswarm 的集成测试清单与 Go 现状，用户选定补齐 **CLI 路由全链路往返** 集成测试（对齐 `test_cli_channel_ws.py`）。调研发现 Go 端存在两层空白：

1. **全链路测试空白**：现有集成测试没有任何测试覆盖 GatewayServer 多路由入站链（`/tui`）到 AgentServer 再回 WS 客户端的完整往返。web 通道（WebChannel /ws）全链路同样无测试，但属另一条独立链路，非本次对齐目标。
2. **11.15 TUI 装配空白**：GatewayServer 本体（tui mode 注入、三层 dispatch、本地 ack、出站帧）在 11.9 已完整交付，但生产代码没有任何地方装配 `/tui` 路由——`AppGatewayServer.Start` 中 `routeBindings` 硬编码空切片（app_gateway.go:421，标注 ⤵️ 11.15）。

用户决策：**方案二——先行 11.15 最小生产装配**（而非测试内自装配），消灭回填标记。

### 1.2 目标

1. 生产代码：新增 tui 路由装配（ForwardMethods 集合 + 5 个本地 handler + DisconnectHandler），接线进 `AppGatewayServer.Start`
2. 集成测试：构建 CLI 路由全链路往返（单进程内 7 件套装配），mock 只在 llm.ClientRegistry 边界，断言 chat.send 完整事件序列 + 双 mode 注入
3. 生产单测：tui_route.go 的集合差集与 handler 行为
4. 文档：doc.go 条目 + IMPLEMENTATION_PLAN.md 11.15 状态更新

### 1.3 非目标（⤵️ 11.15 完整范围回填）

- tui 纯本地 handler（27 个：config.get/session.list/memory.*/cron.job.* 等，不在转发集合）——本次调用会得到 `unknown method`，与 Python 行为有差异，登记回填
- TUI 面板渲染、memory_rpc 注册（10.6.18 回填点）
- ACP 路由（/acp）装配（⤵️ 11.25）
- web 通道全链路测试（独立测试项）

## 2. 已确认的链路事实（调研结论）

### 2.1 入站链（全部就绪）

```
WS 客户端
→ GatewayServer(:19001) handleRawMessage 三层处理：
   ① 入站拦截器（tui 无）
   ② ForwardMethods 集合命中（chat.send ∈ 集合）→
      构造 schema.Message（params 拷贝 + setdefault mode：tui 默认 code.normal，
      gateway_server.go L508-522）→ onMessageCb
   ③ LocalHandlers 命中 → handler(ws, reqID, params, sessionID) 自行写 ack 帧
→ InboundGatewayServer（256 容量队列）
→ NormalizeAndForwardMessage → web.NormalizeGatewayMessage：
   content→query 复制 + chat.send 强制 is_stream=true（web_normalize.go L210）
→ ChannelManager.DeliverToMessageHandler → MessageHandler.userMessages
→ forwardLoop → AgentClient.SendRequestStream
→ ChannelTransport（进程内 sendCh/recvCh）
→ AgentServer.startConsumeLoop → handleEnvelope
→ applyResolvedModeToRequest（从 params 提取 mode）→ GetAgent(channelID, mode, …)
→ AgentFactory（默认=defaultAgentFactory：NewUapClaw + CreateInstance 真实 DeepAdapter）
→ UapClaw.ProcessMessage → DeepAdapter → llm.Model
→ ClientRegistry.GetClient → MockModelClient（mock 注入点）
```

### 2.2 出站链（全部就绪）

```
DeepAdapter 流式 yield：llm_output → chat.delta；llm_usage → chat.usage_metadata（无条件）；
answer chunk → hasStreamedContent=true → chat.final；chat.usage_summary 仅 TotalTokens>0 才发（mock 为 0 → 不发）
→ writeResponse/writeChunk → E2A wire → recvCh
→ AgentClient push（SetServerPushHandler → robotMessages）
→ MessageHandler.publishRobotMessages → ChannelManager.dispatchRobotMessages
→ externalChannels["tui"] 优先命中（channels 表 miss 后）
→ GatewayServer.Send：requestToClient 定向 → probeWritable → buildEventFrame
→ {type:"event", event:"chat.delta", payload:{…}} → WS 客户端
```

### 2.3 mockllm 单 chunk 流 + ReAct 无工具单轮的实际事件序列

`chat.processing_status(true)` → `chat.delta` → `chat.usage_metadata` → `chat.final` → `chat.processing_status(false)`

（5 帧；usage_summary 不发。processing_status(true) 由 AgentServer 发出，(false) 由清理路径发出。）

### 2.4 关键 API 签名

- `GatewayServerConfig.Routes map[string]*RouteConfig` 构造前填入；无运行时 RegisterLocalHandler API
- `LocalHandlerFunc(ws *websocket.Conn, reqID string, params map[string]any, sessionID string) error`（handler 自行写 ack 帧，返回 error 仅表示 handler 自身失败）
- web 包 `RPCHandlerFunc(ctx, params, sessionID) (map, error)` 与 LocalHandlerFunc 签名不同、发送机制不同（RPCDispatcher 返回 map vs 直接写 ws 帧）——不复用
- `MessageHandler.CancelAgentSessionsOnDisconnect(ctx, sessionKeys [][2]string)` 已存在（message_handler/disconnect.go）
- `AgentServer.SetAgentFactoryForTest(factory runtime.AgentFactory)`，`AgentFactory = func(config map[string]any, mode, subMode string) (*UapClaw, error)`
- `defaultAgentFactory`：`NewUapClaw() + CreateInstance(WithCreateInstanceConfig/Mode/SubMode)`（agent_manager.go L530-540）
- `llm.ClientRegistry.Register(providerName, "llm", mockClient.Factory())`；重复注册静默跳过，须 Unregister 成对清理
- Python 端：`build_cli_route_binding(CliRouteBindParams)` 返回 GatewayRouteBinding；`register_cli_handlers` 注册 5 个 chat 类 + 27 个纯本地 handler（共 32）；forward 集合 80 项、no-local 集合 75 项（差集恰为 5 个 chat 类方法）——计数已经 Python 源码机器核对（2026-10-28）

## 3. 生产代码设计（gateway 包）

### 3.1 新文件 `internal/swarm/gateway/tui_route.go`

对齐 Python `tui_connect.py` 的**路由装配部分**（L163-330 集合定义 + L1402-1433 五个 handler + L2093-2119 build_cli_route_binding）。

```
├── TuiForwardReqMethods             # map[string]bool，80 项全量对齐 CLI_FORWARD_REQ_METHODS
├── TuiForwardNoLocalHandlerMethods  # map[string]bool，75 项对齐（差集 = 5 个 chat 类方法）
├── TuiRouteParams                   # {MessageHandler mh}（对齐 CliRouteBindParams 裁剪）
├── BuildTuiRouteConfig(params) *RouteConfig  # 填两集合 + 5 LocalHandlers + DisconnectHandler
└── writeResponseFrame               # 包级帧写入函数（自 sendResponse 重构提取，零行为变更）
```

**5 个本地 handler 实现**（各 5-10 行，构造 payload + `writeResponseFrame`，对齐 Python `_chat_send` 等的自己实现模式）：

| method | payload |
|---|---|
| chat.send | `{accepted:true, session_id}` |
| chat.resume | `{accepted:true, session_id}` |
| chat.interrupt | `{accepted:true, session_id, intent?}`（params.intent 非空时） |
| chat.user_answer | `{accepted:true, session_id, request_id?}`（params.request_id 非空时） |
| history.get | `{accepted:true, session_id, page_idx?}`（params.session_id 覆写 + page_idx 透传） |

**DisconnectHandler**：桥接 `params.MessageHandler.CancelAgentSessionsOnDisconnect`（nil 安全跳过）。

**放 gateway 包的原因**：Python 的 tui_connect.py 属 channel_manager 子包，但 Go 端 `gateway → channel_manager` 依赖方向已存在（channel_manager 不能反向 import gateway），故放 gateway 包。与 Python 目录结构有意偏差，doc 注释登记。

**范围裁剪登记**：纯本地 handler（config.get/session.list/memory.*/cron.job.* 等 27 个，已机器核对）本次不实现，调用得到 `unknown method`。Python 命中本地 handler 表；差异属 11.15 完整范围，⤵️ 回填。

### 3.2 接线 `app_gateway.go` L418-431

```go
gwCfg := NewGatewayServerConfig()
// ...（现有 env 覆盖逻辑不动）
// 11.15 最小装配：/tui CLI 路由（ACP ⤵️ 11.25 仍走空列表）
if s.msgHandler != nil {
    tuiRoute := BuildTuiRouteConfig(TuiRouteParams{MessageHandler: s.msgHandler})
    gwCfg.Routes[tuiRoute.Path] = tuiRoute
}
s.gwServer = NewGatewayServer(gwCfg)
s.channelMgr.RegisterExternalChannel("tui", s.gwServer)
```

注：`RegisterExternalChannel("tui", ...)` 从 routeBindings 循环中提出（循环仍空，保留 ACP ⤵️ 注释）。`routing.RouteBinding.Install` 钩子不启用——Go 端既定决策是调用方直接对 RouteConfig 赋值（route_config.go 注释）。

### 3.3 重构：sendResponse → writeResponseFrame 提取

`GatewayServer.sendResponse` 的帧构造与写入段提取为包级 `writeResponseFrame(ws, reqID, ok, payload, errMsg, code)`，`sendResponse` 改为薄包装（加日志）。零行为变更，tui handler 复用。

### 3.4 生产单测 `internal/swarm/gateway/tui_route_test.go`

1. 集合差集测试：`TuiForwardReqMethods − TuiForwardNoLocalHandlerMethods == {chat.send, chat.interrupt, chat.resume, chat.user_answer, history.get}`
2. 5 个 handler ack 行为：httptest + gorilla 客户端直连，断言 res 帧 payload 字段（含 intent/request_id/page_idx 可选字段）
3. writeResponseFrame 帧格式（ok/false 两分支）
4. DisconnectHandler nil 安全 + 桥接（nil mh / 空 keys / 正常调用）
5. BuildTuiRouteConfig 字段填充完整性

## 4. 集成测试设计

### 4.1 目录与装配

**新目录 `tests/integration/swarm/gateway/`**（新建 doc.go + cli_route_test.go），`CliRouteFullChainSuite`（嵌入 isuite.BaseIntegrationSuite / mockllm 模式，含 go:build integration 标签）。

**装配半径 = 中间层 7 件套**（消费生产 `BuildTuiRouteConfig`，**不走 AppGatewayServer**——后者捆绑 WebChannel/cron/heartbeat/reloader 等 fs 重依赖，超出测试半径；AppGatewayServer 的装配逻辑本身有既有单测）：

```
gorilla WS 客户端（随机端口）
→ GatewayServer（Routes["/tui"] = BuildTuiRouteConfig(TuiRouteParams{mh})）
→ OnMessage(InboundGatewayServer.HandleMessage)；inboundServer handler = NormalizeAndForwardMessage(msg, channelMgr)
→ ChannelManager（RegisterExternalChannel("tui", gwServer)）
→ MessageHandler（构造注入 AgentClient）
→ AgentClient（SetServerPushHandler → robotMessages 消费）
→ ChannelTransport（进程内）
→ AgentServer（SetAgentFactoryForTest(wrapper)）
```

Setup 顺序：环境三件套 → mockllm 注册 → 7 件套构造 → Start 顺序（inboundServer → gwServer → channelMgr.StartDispatch → msgHandler.StartForwardLoop → AgentServer）。TearDown 逆序 Stop + Unregister + 环境恢复 + pathutil.ResetCache()。

### 4.2 环境与 mock 注入

- `UAPCLAW_DATA_DIR = t.TempDir()`；写 `config/config.yaml`：`models.defaults[0] = {model_client_config: {client_provider: "mock-cli-itest", model_name: "mock-model"}}`（格式对齐 forbidden_test.go 先例 + mockllm 注册惯例）
- workspace 目录：`t.TempDir()/workspace`
- `pathutil.ResetCache()` 设置与恢复成对（forbidden_test.go 模式）
- `ClientRegistry.Register("mock-cli-itest", "llm", mockClient.Factory())` + TearDown `Unregister` 成对（registry.Register 重复注册静默跳过约束）

DeepAdapter.CreateInstance 从 `config.New("")` 全局配置加载 configBase → createModel → buildModelCacheFromDefaults → llm.NewModel → CreateModelClient → GetClient 命中 mock。

### 4.3 测试方法（两个，共享一次装配）

| | 主测试 TestChatSendFullChain_显式agentPlan | 副测试 TestChatSendFullChain_默认codeNormal |
|---|---|---|
| 请求 | `{type:"req", id, method:"chat.send", params:{session_id, content, mode:"agent.plan"}}` | 同左但 params 无 mode |
| 断言① ack | `{type:"res", id, ok:true, payload:{accepted:true, session_id:"sess_test"}}`（对齐 Python 原断言） | 同左 |
| 断言② 事件序列 | `processing_status(true)→delta→usage_metadata→final→processing_status(false)`；**顺序断言** delta 在 final 前；processing_status 位置宽容；usage_metadata 存在但值不断言 | 同左 |
| 断言③ mode | wrapper 记录 `mode=="agent" && subMode=="plan"` | wrapper 记录 `mode=="code" && subMode=="normal"`（tui 默认注入的直接证据） |
| 断言④ 内容 | delta/final 内容含 mock 预设文本 | 同左 |
| 边界 | mock SetResponses 单文本 chunk；超时 10s（对齐 Python `_recv_until_response`） | 同左 |

**mode 断言原理**：wrapper 函数体与 defaultAgentFactory 一字不差（真实 NewUapClaw + CreateInstance 链），仅追加记录 `(mode, subMode)` 入参。若 GatewayServer 的 tui setdefault 逻辑损坏，两测试将同时 fallback agent.plan，副测试断言失败——坏注入被捕获。

**全局单例规避**：suite 生命周期内只 Start/Stop 一次（`adapter.SetDefaultSendPushFunc` / `cronbridge.SetGlobalGatewayPushFunc` 是包级全局单例注入，并行 AgentServer 互相干扰）；两个 mode 测试共享装配（GetAgent 缓存 key 含 mode，两实例共存无冲突）；wrapper 记录每测试前清零（测试按 suite 方法顺序执行，不并行）。

### 4.4 帧接收与超时

- 客户端读循环按 `type:res`（id 匹配）与 `type:event`（event 字段）分类收集
- 全部断言带 10s 超时（读 deadline），失败时 dump 已收帧辅助诊断
- usage_metadata 帧：event 名存在即可（值全 0，mock 特性）

### 4.5 Rule 7 合规性

- 真实跨子系统：gateway（GatewayServer+ChannelManager+MessageHandler）+ routing（AgentClient）+ transport（ChannelTransport）+ server（AgentServer+runtime+adapter）+ agentcore（DeepAdapter 消费链+llm）——静跨度 ≥ 3 远超门槛
- mock 只在 LLM API 边界（MockModelClient via ClientRegistry）——符合“mock 只允许在边界”要求
- `suite.Run(` 入口 + `//go:build integration` 标签 + 新目录 doc.go

## 5. 交付物清单

| # | 文件 | 类型 | 内容 |
|---|---|---|---|
| 1 | `internal/swarm/gateway/tui_route.go` | 生产 | 集合 + 5 handler + BuildTuiRouteConfig + writeResponseFrame |
| 2 | `internal/swarm/gateway/tui_route_test.go` | 生产单测 | 差集/ack/帧格式/nil 安全/字段完整性 |
| 3 | `internal/swarm/gateway/app_gateway.go` | 生产（改） | /tui 接线段 |
| 4 | `internal/swarm/gateway/gateway_server.go` | 生产（改，重构） | sendResponse → writeResponseFrame 提取 |
| 5 | `internal/swarm/gateway/doc.go` | 文档（改） | 文件条目 |
| 6 | `tests/integration/swarm/gateway/doc.go` | 新 | 包文档 |
| 7 | `tests/integration/swarm/gateway/cli_route_test.go` | 集成测试 | CliRouteFullChainSuite 双 mode 测试 |
| 8 | `IMPLEMENTATION_PLAN.md` | 计划（改） | 11.15 → 🔄 备注 |

## 6. 风险与对策

| 风险 | 对策 |
|---|---|
| wrapper 复刻 defaultAgentFactory 漂移 | 函数注释标注来源行号 + 审查时对照 |
| 双 mode 两 Agent 实例并存资源占用 | suite 级共享装配 + 测试顺序执行 |
| 事件帧顺序偶发（processing_status 与 delta 交错） | 顺序断言只约束 delta<final 与 processing_status(true) 首帧、(false) 尾帧 |
| config.yaml 格式错误导致 mock 未命中（静默 fallback 真实 LLM） | 断言前先验证 MockModelClient.InvokeCallCount() ≥ 1（未命中即失败） |
| ClientRegistry 全局态泄漏 | Register/Unregister 严格成对出现在 suite Setup/TearDown |
| writeResponseFrame 重构引入回归 | 零行为变更重构 + gateway 包既有单测全跑 |
| code.normal DeepAdapter 装配比 agent.plan 重（harness 工具装载） | 超时放宽（主 10s / 副 30s） |

## 6.1 已知坑（历史记忆预防）

- YAML 整数解出 int 非 float64（timeout 旧断言翻车先例）——测试断言 params 值时用双类型 switch
- 空提交不触发 CI——收尾用 Makefile 计数器模式
- 集成测试目录新建必须带 doc.go（规则 1）
- mock 队列耗尽静默成功坑——SetupTest 重置响应队列（第 2 次出现，规范级）

## 7. 验收标准

1. `go build ./...` 通过
2. `go test ./internal/swarm/gateway/...` 全绿（含新 tui_route_test.go 与既有单测）
3. `go test -tags=integration ./tests/integration/swarm/gateway/...` 本地全绿
4. Rule 7 双层门禁：`go run ./cmd/integration-audit -verify` 通过（新目录静跨度 ≥ 3）
5. 远端 CI 全绿（Build/Lint/UnitTest/IntegrationTest 四 job，规则 6）——集成测试完成标准
6. gofmt + staticcheck 干净（风格审查链门禁）
7. IMPLEMENTATION_PLAN.md 11.15 状态 🔄 + 备注准确

## 附录 A：机器核对数据（2026-10-28，从 tui_connect.py 解析）

**差集 = 5 个有本地 handler 的转发方法**（Go 本次实现 ack）：
`chat.interrupt, chat.resume, chat.send, chat.user_answer, history.get`

**纯本地 handler 27 个**（本次不实现，⤵️ 11.15 回填，调用得 `unknown method`）：
`command.model, config.get, config.set, config.validate_model, cron.job.create, cron.job.delete, cron.job.get, cron.job.list, cron.job.preview, cron.job.run_now, cron.job.toggle, cron.job.update, history.list_turns, hooks.list, memory.edit, memory.list, memory.open, memory.status, memory.toggle, models.list, session.create, session.delete, session.list, session.rename, session.restore_files, session.rewind, session.rewind_and_restore`

**集合计数**：forward 80 / no-local 75 / 注册本地 handler 32（5 转发 + 27 纯本地）。
