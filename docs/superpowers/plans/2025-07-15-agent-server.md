# AgentServer (10.3.1) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 AgentServer，从 ChannelTransport.SendCh() 消费 Gateway 发来的 E2AEnvelope，按 req_method 分发到具体 handler，响应通过 RecvCh() 写回 Gateway，完整对齐 Python AgentWebSocketServer。

**Architecture:** AgentServer 作为 ChannelTransport 消费者运行在独立 goroutine 中，每个请求独立 goroutine 并发处理。通过 serverReadyCh 通知 Gateway AgentServer 已就绪，WebChannel 收到通知后才向前端发 connection.ack。JiuWenClaw/AgentManager 先 stub，后续替换。

**Tech Stack:** Go, gorilla/websocket, go-chi/chi, zerolog, e2a 协议层

**设计文档:** `docs/superpowers/specs/2025-07-15-agent-server-design.md`

---

## 文件结构

### 新建文件

| 文件 | 职责 |
|------|------|
| `internal/swarm/server/agent_server.go` | AgentServer 结构体 + Start/Stop/ServerReady/WaitServerReady + SendCh 消费循环 |
| `internal/swarm/server/agent_server_test.go` | AgentServer 单元测试 |
| `internal/swarm/server/handle_envelope.go` | handleEnvelope 主分发 + handleUnary/handleStream + 流式心跳 |
| `internal/swarm/server/handle_envelope_test.go` | handleEnvelope 测试 |
| `internal/swarm/server/handle_session.go` | session.list/rename/delete/rewind/create/fork/switch |
| `internal/swarm/server/handle_session_test.go` | session handler 测试 |
| `internal/swarm/server/handle_command.go` | command.* 系列 |
| `internal/swarm/server/handle_command_test.go` | command handler 测试 |
| `internal/swarm/server/handle_team.go` | team.delete/snapshot/history.get |
| `internal/swarm/server/handle_team_test.go` | team handler 测试 |
| `internal/swarm/server/handle_history.go` | history.get（非流式+流式） |
| `internal/swarm/server/handle_history_test.go` | history handler 测试 |
| `internal/swarm/server/handle_permissions.go` | permissions.* 全部 10 个 |
| `internal/swarm/server/handle_permissions_test.go` | permissions handler 测试 |
| `internal/swarm/server/handle_agents.go` | agents.* 全部 8 个 |
| `internal/swarm/server/handle_agents_test.go` | agents handler 测试 |
| `internal/swarm/server/handle_extensions.go` | extensions.* + hooks.list |
| `internal/swarm/server/handle_extensions_test.go` | extensions handler 测试 |
| `internal/swarm/server/handle_harness.go` | harness.packages.* + schedule.* |
| `internal/swarm/server/handle_harness_test.go` | harness handler 测试 |
| `internal/swarm/server/handle_browser.go` | browser.start/runtime_restart |
| `internal/swarm/server/handle_browser_test.go` | browser handler 测试 |
| `internal/swarm/server/handle_config.go` | config.cache_clear + agent.reload_config |
| `internal/swarm/server/handle_config_test.go` | config handler 测试 |
| `internal/swarm/server/handle_initialize.go` | initialize + acp.tool_response |
| `internal/swarm/server/handle_initialize_test.go` | initialize handler 测试 |
| `internal/swarm/server/runtime/agent_manager.go` | AgentManager stub |
| `internal/swarm/server/runtime/agent_manager_test.go` | AgentManager stub 测试 |
| `internal/swarm/server/runtime/jiowenclaw.go` | JiuWenClaw stub |
| `internal/swarm/server/runtime/jiowenclaw_test.go` | JiuWenClaw stub 测试 |

### 修改文件

| 文件 | 改动 |
|------|------|
| `internal/swarm/gateway/app_gateway.go` | NewGatewayServer 增加 agentServer 参数，传给 WebChannel |
| `internal/swarm/gateway/channel_manager/web/web_connect.go` | HandleWebSocket 增加 WaitServerReady 逻辑 |
| `cmd/uapclaw/cmd.go` | runAppCmd 增加 AgentServer 创建和启动 |
| `internal/swarm/server/doc.go` | 更新文件目录 |
| `internal/swarm/server/runtime/doc.go` | 更新文件目录 |

---

### Task 1: AgentManager stub + JiuWenClaw stub

**Files:**
- Create: `internal/swarm/server/runtime/agent_manager.go`
- Create: `internal/swarm/server/runtime/agent_manager_test.go`
- Create: `internal/swarm/server/runtime/jiowenclaw.go`
- Create: `internal/swarm/server/runtime/jiowenclaw_test.go`
- Modify: `internal/swarm/server/runtime/doc.go`

AgentManager stub 提供 GetAgent/GetAgentNoWait/ReloadAgentsConfig/CreateSession/Initialize 等方法，返回 stub 响应。JiuWenClaw stub 提供 ProcessMessage/ProcessMessageStream/ProcessInterrupt 等方法，返回固定响应。

- [ ] **Step 1: 编写 agent_manager.go stub**

```go
// agent_manager.go — AgentManager stub（10.3.12）
// 包含 AgentManager 结构体和所有 stub 方法
```

包含方法：
- `NewAgentManager() *AgentManager`
- `GetAgent(channelID, mode, projectDir, subMode string) (*JiuWenClaw, error)` — 返回 stub JiuWenClaw
- `GetAgentNoWait(channelID, mode, projectDir, subMode string) *JiuWenClaw` — 返回 stub 或 nil
- `ReloadAgentsConfig(configPayload map[string]any, envOverrides map[string]string) error` — 返回 nil
- `CreateSession(channelID, sessionID string) error` — 创建目录 + metadata.json
- `Initialize(channelID string, extraConfig map[string]any) (map[string]any, error)` — 返回默认 capabilities
- `CancelAllInflightWork(ctx context.Context) error` — 返回 nil

- [ ] **Step 2: 编写 agent_manager_test.go**

测试 NewAgentManager、GetAgent 返回非 nil、ReloadAgentsConfig 返回 nil 等。

- [ ] **Step 3: 编写 jiowenclaw.go stub**

```go
// jiowenclaw.go — JiuWenClaw stub（10.3.2）
// 包含 JiuWenClaw 结构体和所有 stub 方法
```

包含方法：
- `NewJiuWenClaw() *JiuWenClaw`
- `ProcessMessage(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error)` — 返回 stub 响应 `{ok: true, payload: {accepted: true}}`
- `ProcessMessageStream(ctx context.Context, request *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error)` — 返回一个发送 stub chunk + terminal chunk 的 channel
- `ProcessInterrupt(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error)` — 返回 ok=true
- `GetContextUsage(sessionID string) (map[string]any, error)` — 返回 `{usage: 0, limit: 0}`
- `CompressContext(sessionID string) (map[string]any, error)` — 返回 `{ok: true, compressed: false}`
- `GenerateRecap(sessionID string) (map[string]any, error)` — 返回空回顾
- `SwitchMode(sessionID, mode string) error` — 返回 nil

- [ ] **Step 4: 编写 jiowenclaw_test.go**

测试 NewJiuWenClaw、ProcessMessage 返回 stub 响应、ProcessMessageStream 返回 channel 并最终关闭等。

- [ ] **Step 5: 更新 runtime/doc.go 文件目录**

添加 jiowenclaw.go 和 agent_manager.go 条目。

- [ ] **Step 6: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/... -v -count=1
```

- [ ] **Step 7: 提交**

```bash
git add internal/swarm/server/runtime/ && git commit -m "feat(server): 添加 AgentManager 和 JiuWenClaw stub (10.3.12/10.3.2)"
```

---

### Task 2: AgentServer 核心结构体 + Start/Stop/ServerReady

**Files:**
- Create: `internal/swarm/server/agent_server.go`
- Create: `internal/swarm/server/agent_server_test.go`

- [ ] **Step 1: 编写 agent_server.go**

结构体字段：
- `config *config.Config`
- `transport *gateway_push.ChannelTransport`
- `agentManager *runtime.AgentManager`
- `sessionStreamTasks map[string]context.CancelFunc` + `sessionStreamTasksMu sync.RWMutex`
- `serverReady bool` + `serverReadyMu sync.RWMutex`
- `serverReadyCh chan struct{}` — 容量 1，close 通知
- `running bool` + `runningMu sync.RWMutex`

方法：
- `NewAgentServer(cfg *config.Config, transport *gateway_push.ChannelTransport) *AgentServer`
- `Start(ctx context.Context) error` — 初始化 AgentManager → 设置 serverReady → close(serverReadyCh) → 进入 consumeLoop
- `Stop() error` — 取消所有流式任务 → 清理
- `ServerReady() bool` — 读 serverReady
- `WaitServerReady(ctx context.Context) bool` — select serverReadyCh / ctx.Done()
- `startConsumeLoop(ctx context.Context)` — for-select 从 transport.SendCh() 读取 → go handleEnvelope
- `registerStreamTask(sessionID string, cancel context.CancelFunc)` / `cancelStreamTask(sessionID string)` — 流式任务追踪

- [ ] **Step 2: 编写 agent_server_test.go**

测试用例：
- `TestNewAgentServer` — 验证创建后 serverReady=false
- `TestAgentServer_Start_ServerReady` — 启动后验证 ServerReady()=true 和 WaitServerReady 不阻塞
- `TestAgentServer_WaitServerReady_Timeout` — 未启动时 WaitServerReady 应在 ctx 取消后返回 false
- `TestAgentServer_Start_ConsumesEnvelope` — 往 SendCh 写入 E2AEnvelope，验证被消费（不阻塞）

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/ -run TestNewAgentServer -v -count=1
```

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/agent_server.go internal/swarm/server/agent_server_test.go && git commit -m "feat(server): 添加 AgentServer 结构体和 Start/Stop/ServerReady"
```

---

### Task 3: handleEnvelope 主分发逻辑

**Files:**
- Create: `internal/swarm/server/handle_envelope.go`
- Create: `internal/swarm/server/handle_envelope_test.go`

- [ ] **Step 1: 编写 handle_envelope.go**

核心函数：
- `handleEnvelope(ctx context.Context, envelope *e2a.E2AEnvelope)` — E2AToAgentRequest → 按 req_method switch 分发 → 未命中走 handleUnary/handleStream
- `handleUnary(ctx context.Context, request *schema.AgentRequest)` — 获取 Agent → ProcessMessage → 编码写入 RecvCh
- `handleStream(ctx context.Context, request *schema.AgentRequest)` — 注册流式任务 → 获取 Agent → ProcessMessageStream → 逐 chunk 编码写入 RecvCh → 清理
- `handleCancel(ctx context.Context, request *schema.AgentRequest)` — 取消流式 goroutine → 转发 interrupt 给 Agent
- `writeResponse(requestID, channelID string, resp *schema.AgentResponse)` — 编码写入 RecvCh
- `writeChunk(requestID, channelID string, chunk *schema.AgentResponseChunk, sequence int, isStream bool)` — 编码写入 RecvCh
- `sendKeepalive(requestID, channelID string)` — 构造 keepalive chunk 写入 RecvCh

分发 switch 覆盖所有 56 个显式分支（按设计文档 §5.1 A-D 分类），每个分支调用对应的 handle 函数。

- [ ] **Step 2: 编写 handle_envelope_test.go**

测试用例：
- `TestHandleEnvelope_UnknownMethod` — 未注册方法走 handleUnary
- `TestHandleEnvelope_SessionList` — 验证分发到 handleSessionList
- `TestHandleEnvelope_ChatSend_Stream` — 验证 chat.send is_stream=true 走 handleStream
- `TestHandleEnvelope_ChatInterrupt` — 验证 chat.interrupt 走 handleCancel
- `TestHandleUnary_StubAgent` — JiuWenClaw stub 返回响应，写入 RecvCh
- `TestHandleStream_StubAgent` — JiuWenClaw stub 返回 chunk channel，逐个写入 RecvCh
- `TestHandleCancel_CancelStreamTask` — 注册流式任务后 interrupt 能精准取消

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/ -run TestHandleEnvelope -v -count=1
```

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/handle_envelope.go internal/swarm/server/handle_envelope_test.go && git commit -m "feat(server): 添加 handleEnvelope 主分发逻辑和 handleUnary/handleStream"
```

---

### Task 4: session handler 本地实现

**Files:**
- Create: `internal/swarm/server/handle_session.go`
- Create: `internal/swarm/server/handle_session_test.go`

- [ ] **Step 1: 编写 handle_session.go**

实现方法：
- `handleSessionList(ctx, request) (*schema.AgentResponse, error)` — 扫描 sessions 目录，读 metadata.json，按 mtime 降序排列（对齐 Python `_handle_session_list`）
- `handleSessionRename(ctx, request) (*schema.AgentResponse, error)` — 更新 metadata.json 的 title 字段
- `handleSessionDelete(ctx, request) (*schema.AgentResponse, error)` — stub: os.RemoveAll 删除目录
- `handleSessionSwitch(ctx, request) (*schema.AgentResponse, error)` — stub: 返回 `{ok: true}`
- `handleSessionCreate(ctx, request) (*schema.AgentResponse, error)` — stub: 创建目录 + metadata.json
- `handleSessionFork(ctx, request) (*schema.AgentResponse, error)` — stub: 返回 NOT_IMPLEMENTED
- `handleSessionRewind(ctx, request) (*schema.AgentResponse, error)` — stub: 仅截断 history.json
- `handleSessionRewindAndRestore(ctx, request) (*schema.AgentResponse, error)` — stub: 同上
- `handleSessionRewindContext(ctx, request) (*schema.AgentResponse, error)` — stub: 返回 NOT_IMPLEMENTED

- [ ] **Step 2: 编写 handle_session_test.go**

测试用例覆盖 session.list（空目录/有数据）、session.rename、session.delete、session.create 等。

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/ -run TestHandleSession -v -count=1
```

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/handle_session.go internal/swarm/server/handle_session_test.go && git commit -m "feat(server): 添加 session handler 本地实现"
```

---

### Task 5: command handler 实现

**Files:**
- Create: `internal/swarm/server/handle_command.go`
- Create: `internal/swarm/server/handle_command_test.go`

- [ ] **Step 1: 编写 handle_command.go**

实现方法（按设计文档 §5.1 分类）：

**完整本地实现：**
- `handleCommandAddDir` — 写入受信目录配置
- `handleCommandChrome` — 空操作返回 ok=true
- `handleCommandDiff` — 读 history.json 中 tool diff
- `handleCommandResume` — mock 返回
- `handleCommandSession` — mock 返回
- `handleCommandStatus` — 会话统计 + 配置路径 + 版本/模型诊断

**需要 AgentManager(stub)：**
- `handleCommandModel` — stub: 设置环境变量 + 返回 ok=true
- `handleCommandMCP` — stub: 返回空 MCP 配置
- `handleCommandSandbox` — stub: 返回 NOT_IMPLEMENTED

**需要 Agent 实例(stub)：**
- `handleCommandCompact` — stub: 返回 `{ok: true, compressed: false}`
- `handleCommandContext` — stub: 返回 `{usage: 0, limit: 0}`
- `handleCommandRecap` — stub: 返回空回顾

- [ ] **Step 2: 编写 handle_command_test.go**

- [ ] **Step 3: 运行测试并提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/ -run TestHandleCommand -v -count=1 && git add internal/swarm/server/handle_command.go internal/swarm/server/handle_command_test.go && git commit -m "feat(server): 添加 command handler 实现"
```

---

### Task 6: team/history/permissions/agents handlers

**Files:**
- Create: `internal/swarm/server/handle_team.go`
- Create: `internal/swarm/server/handle_team_test.go`
- Create: `internal/swarm/server/handle_history.go`
- Create: `internal/swarm/server/handle_history_test.go`
- Create: `internal/swarm/server/handle_permissions.go`
- Create: `internal/swarm/server/handle_permissions_test.go`
- Create: `internal/swarm/server/handle_agents.go`
- Create: `internal/swarm/server/handle_agents_test.go`

- [ ] **Step 1: 编写 handle_team.go**

- `handleTeamDelete` — stub: 返回 ok=true
- `handleTeamSnapshot` — stub: 返回空快照
- `handleTeamHistoryGet` — 读 team history 记录（纯文件系统）

- [ ] **Step 2: 编写 handle_history.go**

- `handleHistoryGet` — 读 history.json + 分页（非流式）
- `handleHistoryGetStream` — 读 history.json + 逐条发送 chunk（流式）

- [ ] **Step 3: 编写 handle_permissions.go**

全部 10 个 permissions 方法，纯 config.yaml 读写：
- `handlePermissionsToolsGet/Set/Update/Delete`
- `handlePermissionsRulesGet/Create/Update/Delete`
- `handlePermissionsApprovalOverridesGet/Delete`

统一入口 `handlePermissionsConfig` 按 req_method 二次分发。

- [ ] **Step 4: 编写 handle_agents.go**

- `handleAgentsList` — AgentConfigService 列 agents（stub: 返回空列表）
- `handleAgentsGet` — stub: 返回 NOT_FOUND
- `handleAgentsCreate` — stub: 返回 NOT_IMPLEMENTED
- `handleAgentsUpdate` — stub: 返回 NOT_IMPLEMENTED
- `handleAgentsDelete` — stub: 返回 NOT_IMPLEMENTED
- `handleAgentsEnable` — stub: 返回 NOT_IMPLEMENTED
- `handleAgentsDisable` — stub: 返回 NOT_IMPLEMENTED
- `handleAgentsToolsList` — stub: 返回空列表

- [ ] **Step 5: 编写对应测试文件**

- [ ] **Step 6: 运行测试并提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/ -run "TestHandleTeam|TestHandleHistory|TestHandlePermissions|TestHandleAgents" -v -count=1 && git add internal/swarm/server/handle_team.go internal/swarm/server/handle_team_test.go internal/swarm/server/handle_history.go internal/swarm/server/handle_history_test.go internal/swarm/server/handle_permissions.go internal/swarm/server/handle_permissions_test.go internal/swarm/server/handle_agents.go internal/swarm/server/handle_agents_test.go && git commit -m "feat(server): 添加 team/history/permissions/agents handlers"
```

---

### Task 7: extensions/harness/browser/config/initialize handlers

**Files:**
- Create: `internal/swarm/server/handle_extensions.go`
- Create: `internal/swarm/server/handle_extensions_test.go`
- Create: `internal/swarm/server/handle_harness.go`
- Create: `internal/swarm/server/handle_harness_test.go`
- Create: `internal/swarm/server/handle_browser.go`
- Create: `internal/swarm/server/handle_browser_test.go`
- Create: `internal/swarm/server/handle_config.go`
- Create: `internal/swarm/server/handle_config_test.go`
- Create: `internal/swarm/server/handle_initialize.go`
- Create: `internal/swarm/server/handle_initialize_test.go`

- [ ] **Step 1: 编写 handle_extensions.go**

- `handleExtensionsList` — stub: 返回 `{extensions: []}`
- `handleExtensionsImport` — stub: 返回 `{ok: true}`
- `handleExtensionsDelete` — stub: 返回 `{ok: true}`
- `handleExtensionsToggle` — stub: 返回 `{ok: true}`
- `handleHooksList` — 读 hooks 配置，stub: 返回 `{hooks: []}`

- [ ] **Step 2: 编写 handle_harness.go**

- `handleHarnessPackagesGet` — stub: 返回 `{packages: []}`
- `handleHarnessPackagesScan` — stub: 返回 `{packages: []}`
- `handleHarnessPackagesActivate` — stub: 返回 `{ok: true}`
- `handleHarnessPackagesDeactivate` — stub: 返回 `{ok: true}`
- `handleHarnessPackagesDelete` — stub: 返回 `{ok: true}`
- `handleScheduleRequest` — 按 action 二次分发（9 个 schedule 方法，大部分 stub 返回空/NOT_IMPLEMENTED）

- [ ] **Step 3: 编写 handle_browser.go**

- `handleBrowserStart` — stub: 返回 NOT_IMPLEMENTED
- `handleBrowserRuntimeRestart` — stub: 返回 NOT_IMPLEMENTED

- [ ] **Step 4: 编写 handle_config.go**

- `handleConfigCacheClear` — 清除配置缓存
- `handleAgentReloadConfig` — stub: 返回 `{ok: true}`

- [ ] **Step 5: 编写 handle_initialize.go**

- `handleInitialize` — stub: 返回默认 capabilities
- `handleACPToolResponse` — stub: 返回 `{ok: true}`

- [ ] **Step 6: 编写对应测试文件**

- [ ] **Step 7: 运行测试并提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/ -v -count=1 && git add internal/swarm/server/handle_*.go internal/swarm/server/handle_*_test.go && git commit -m "feat(server): 添加 extensions/harness/browser/config/initialize handlers"
```

---

### Task 8: serverReady 机制 + WebChannel connection.ack 时机变更

**Files:**
- Modify: `internal/swarm/gateway/channel_manager/web/web_connect.go`
- Modify: `internal/swarm/gateway/channel_manager/web/web_connect_test.go`
- Modify: `internal/swarm/gateway/app_gateway.go`

- [ ] **Step 1: WebChannel 增加 agentServer 引用**

在 `WebChannel` 结构体中增加 `agentServer` 字段（接口类型，避免循环依赖）。

定义接口：
```go
// ServerReadyWaiter AgentServer 就绪等待接口（避免循环依赖）
type ServerReadyWaiter interface {
    WaitServerReady(ctx context.Context) bool
    ServerReady() bool
}
```

在 `WebChannelConfig` 或 `NewWebChannel` 中传入。

- [ ] **Step 2: HandleWebSocket 增加 WaitServerReady 逻辑**

在 `HandleWebSocket` 中，WS 升级后、发 connection.ack 之前，增加：
```go
if wc.agentServer != nil {
    wsCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
    defer cancel()
    if !wc.agentServer.WaitServerReady(wsCtx) {
        logger.Warn(logComponent).Msg("AgentServer 未就绪，跳过 connection.ack")
        return
    }
}
```

- [ ] **Step 3: GatewayServer 传递 agentServer 给 WebChannel**

修改 `NewGatewayServer` 签名增加 `agentServer` 参数，传给 WebChannel。

- [ ] **Step 4: 更新测试**

修改现有 `TestHandleWebSocket_ConnectionAck` 测试，验证 WaitServerReady 行为。

- [ ] **Step 5: 运行测试并提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/gateway/... -v -count=1 && git add internal/swarm/gateway/ && git commit -m "feat(gateway): WebChannel connection.ack 等待 AgentServer 就绪"
```

---

### Task 9: runAppCmd 集成 AgentServer

**Files:**
- Modify: `cmd/uapclaw/cmd.go`

- [ ] **Step 1: 修改 runAppCmd**

在 `runAppCmd` 中：
1. 创建 AgentServer：`agentServer := server.NewAgentServer(cfg, transport)`
2. 启动 AgentServer goroutine：`go func() { agentServer.Start(ctx) }()`
3. 修改 NewGatewayServer 调用，传入 agentServer
4. 退出时调用 `agentServer.Stop()`

- [ ] **Step 2: 更新 cmd.go 中 app 命令的注释**

更新 Long 描述，说明 AgentServer 已在同进程内启动。

- [ ] **Step 3: 运行测试并提交**

```bash
cd /home/opensource/uapclaw-gateway && go test ./cmd/uapclaw/ -v -count=1 && git add cmd/uapclaw/cmd.go && git commit -m "feat(cmd): runAppCmd 集成 AgentServer 启动"
```

---

### Task 10: doc.go 更新 + 整体集成测试

**Files:**
- Modify: `internal/swarm/server/doc.go`
- Modify: `internal/swarm/server/runtime/doc.go`

- [ ] **Step 1: 更新 server/doc.go**

添加所有新文件到文件目录树。

- [ ] **Step 2: 更新 runtime/doc.go**

确认 jiowenclaw.go 和 agent_manager.go 已在文件目录中。

- [ ] **Step 3: 整体编译检查**

```bash
cd /home/opensource/uapclaw-gateway && go build ./...
```

- [ ] **Step 4: 整体单元测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/... -v -count=1
```

- [ ] **Step 5: 端到端验证 — 启动 uapclaw app**

```bash
cd /home/opensource/uapclaw-gateway && go build -o /tmp/uapclaw-test ./cmd/uapclaw/ && timeout 10 /tmp/uapclaw-test app 2>&1 || true
```

验证：
- 进程启动无 panic
- 日志中包含 "AgentServer 已就绪"
- 日志中包含 "WebSocket 客户端已连接"（如果有前端连接）

- [ ] **Step 6: 提交**

```bash
git add internal/swarm/server/doc.go internal/swarm/server/runtime/doc.go && git commit -m "docs(server): 更新 doc.go 文件目录"
```

---

### Task 11: IMPLEMENTATION_PLAN.md 状态同步

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新章节状态**

将 10.3.1 AgentWebSocketServer 从 ☐ 改为 ✅
将 10.3.2 JiuWenClaw 门面从 ☐ 改为 🔄
将 10.3.12 AgentManager 从 ☐ 改为 🔄
将 12.7 统一启动器从 🔄 改为 ✅

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md && git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md — 10.3.1 AgentServer ✅"
```
