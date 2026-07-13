# AgentServer 启动逻辑对齐 + Transport 接口抽象修复 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 AgentServer 启动逻辑与 Python 的对齐差异，将 AgentServer 改为依赖 AgentTransport 接口，并将 ChannelTransport 移入 transport 包，为将来独立部署奠定基础。

**Architecture:** AgentServer.Start() 改为非阻塞（内部起 goroutine），按 Python 顺序补齐初始化步骤（ensurePersistentCheckpointer 完整实现 + 7 个 TODO stub），transport 字段从具体类型改为接口，ChannelTransport 从 server/gateway_push 移入 swarm/transport 包。

**Tech Stack:** Go 1.22+, existing checkpointer/transport/adapter 基础设施

---

## File Structure

| 文件 | 变更类型 | 职责 |
|------|---------|------|
| `swarm/transport/channel_transport.go` | 移入 | ChannelTransport 进程内传输实现 |
| `swarm/transport/channel_transport_test.go` | 移入 | ChannelTransport 测试 |
| `swarm/transport/doc.go` | 修改 | 更新包描述和文件目录 |
| `swarm/server/agent_server.go` | 重构 | Start/Stop 非阻塞 + transport 改接口 + 7 个 TODO stub |
| `swarm/server/agent_server_test.go` | 适配 | 更新 import 和 transport 创建方式 |
| `swarm/server/handle_envelope.go` | 修改 | 写响应改用 transport.Send() |
| `swarm/server/handle_envelope_test.go` | 适配 | 更新 import |
| `swarm/server/adapter/deep_adapter.go` | 修改 | 导出 ensurePersistentCheckpointer → EnsurePersistentCheckpointer |
| `swarm/server/adapter/doc.go` | 修改 | 更新文件目录 |
| `swarm/server/doc.go` | 修改 | 移除 gateway_push 条目 |
| `swarm/gateway/routing/agent_client_test.go` | 适配 | 更新 import |
| `swarm/gateway/message_handler/message_handler_test.go` | 适配 | 更新 import |
| `swarm/gateway/app_gateway_test.go` | 适配 | 更新 import |
| `cmd/uapclaw/cmd.go` | 简化 | Start 不再 go，import 改 transport |
| `swarm/server/gateway_push/` | 删除 | 整个包 |

---

### Task 1: ChannelTransport 移入 transport 包

**Files:**
- Move: `swarm/server/gateway_push/channel_transport.go` → `swarm/transport/channel_transport.go`
- Move: `swarm/server/gateway_push/channel_transport_test.go` → `swarm/transport/channel_transport_test.go`
- Modify: `swarm/transport/doc.go`
- Delete: `swarm/server/gateway_push/` 整个包

- [ ] **Step 1: 移动 channel_transport.go 到 transport 包**

将 `internal/swarm/server/gateway_push/channel_transport.go` 复制到 `internal/swarm/transport/channel_transport.go`，修改 package 声明为 `transport`。

在文件中添加接口合规声明（原 `transport.go` 的内容）：

```go
// 接口合规：ChannelTransport 实现 AgentTransport
var _ AgentTransport = (*ChannelTransport)(nil)
```

删除对 `logger.ComponentAgentServer` 的引用（改用 `logger.ComponentCommon` 或在 transport 包定义自己的组件常量）。由于 transport 包当前没有导入 logger，改为使用 `logger.ComponentCommon`：

```go
const logComponent = logger.ComponentCommon
```

- [ ] **Step 2: 移动 channel_transport_test.go 到 transport 包**

将 `internal/swarm/server/gateway_push/channel_transport_test.go` 复制到 `internal/swarm/transport/channel_transport_test.go`，修改 package 声明为 `transport`。

移除 `import "github.com/uapclaw/uapclaw-go/internal/swarm/transport"` （同包不再需要），将测试中 `transport.AgentTransport` 改为 `AgentTransport`。

- [ ] **Step 3: 更新 transport/doc.go**

```go
// Package transport 提供 Gateway ↔ AgentServer 的传输抽象、Wire 编码工具与进程内传输实现。
//
// 本包定义 AgentTransport 接口（Send/Recv/Close），对齐 Python WebSocket 单连接模型，
// E2A Wire 编码工具函数（WireRequestIDKey、BuildConnectionAckFrame、BuildServerPushWire），
// 以及进程内传输实现 ChannelTransport（基于 Go channel）。
// 将来跨进程传输实现 WebSocketTransport 也在本包中。
//
// 文件目录：
//
//	transport/
//	├── doc.go                 # 包文档
//	├── interface.go           # AgentTransport 接口定义
//	├── channel_transport.go   # ChannelTransport 进程内实现
//	├── wire.go                # Wire 编码工具
//	├── wire_test.go           # Wire 编码测试
//	└── channel_transport_test.go # ChannelTransport 测试
//
// 对应 Python 代码：jiuwenswarm/server/gateway_push/transport.py (进程内路径)
package transport
```

- [ ] **Step 4: 删除 server/gateway_push/ 包**

删除以下文件：
- `internal/swarm/server/gateway_push/channel_transport.go`
- `internal/swarm/server/gateway_push/channel_transport_test.go`
- `internal/swarm/server/gateway_push/transport.go`
- `internal/swarm/server/gateway_push/doc.go`

- [ ] **Step 5: 更新 server/doc.go — 移除 gateway_push 条目**

从 `internal/swarm/server/doc.go` 的文件目录中移除 `gateway_push/` 子包条目。

- [ ] **Step 6: 更新 cmd.go import**

在 `cmd/uapclaw/cmd.go` 中：
- 删除 `"github.com/uapclaw/uapclaw-go/internal/swarm/server/gateway_push"` import
- 将 `gateway_push.NewChannelTransport()` 改为 `transport.NewChannelTransport()`
- 注意：cmd.go 已有 `transport` 的 alias import（给 `swarm/transport`），需要调整避免冲突。当前 cmd.go 没有 import `swarm/transport`，所以直接添加即可。

- [ ] **Step 7: 更新测试文件 import**

在以下测试文件中，将 `server/gateway_push` import 改为 `swarm/transport`，将 `gateway_push.NewChannelTransport()` 改为 `transport.NewChannelTransport()`，将 `gateway_push.NewChannelTransportWithBuffer()` 改为 `transport.NewChannelTransportWithBuffer()`：

- `internal/swarm/server/agent_server_test.go`
- `internal/swarm/server/handle_envelope_test.go`
- `internal/swarm/gateway/routing/agent_client_test.go` — 此文件大量使用 `chTransport.RecvCh()` 和 `chTransport.SendCh()`，这些方法已随 ChannelTransport 移入 transport 包，只需改 import
- `internal/swarm/gateway/message_handler/message_handler_test.go`
- `internal/swarm/gateway/app_gateway_test.go`

- [ ] **Step 8: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./...
```

- [ ] **Step 9: 测试验证**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/transport/... -v -count=1
```

- [ ] **Step 10: 提交**

```bash
git add -A && git commit -m "refactor: 将 ChannelTransport 从 server/gateway_push 移入 swarm/transport 包"
```

---

### Task 2: AgentServer 改用 AgentTransport 接口 + handleEnvelope 改用 Send/Recv

**Files:**
- Modify: `internal/swarm/server/agent_server.go`
- Modify: `internal/swarm/server/handle_envelope.go`

- [ ] **Step 1: 修改 AgentServer 结构体和构造函数**

在 `agent_server.go` 中：

1. 删除 `"github.com/uapclaw/uapclaw-go/internal/swarm/server/gateway_push"` import
2. 添加 `"github.com/uapclaw/uapclaw-go/internal/swarm/transport"` import（如果没有的话）
3. 将 `transport` 字段类型从 `*gateway_push.ChannelTransport` 改为 `transport.AgentTransport`
4. 将 `NewAgentServer` 参数类型从 `*gateway_push.ChannelTransport` 改为 `transport.AgentTransport`
5. 将 `Transport()` 返回类型从 `*gateway_push.ChannelTransport` 改为 `transport.AgentTransport`

```go
type AgentServer struct {
    // config 配置实例
    config *config.Config
    // transport 传输通道（AgentTransport 接口，支持 ChannelTransport 和将来的 WebSocketTransport）
    transport transport.AgentTransport
    // ... 其余不变
}

func NewAgentServer(cfg *config.Config, transport transport.AgentTransport) *AgentServer {
    return &AgentServer{
        config:             cfg,
        transport:          transport,
        sessionStreamTasks: make(map[string]context.CancelFunc),
        sessionsDir:        workspace.AgentSessionsDir(),
    }
}

func (s *AgentServer) Transport() transport.AgentTransport {
    return s.transport
}
```

- [ ] **Step 2: 修改 startConsumeLoop — 用 Recv() 替代 SendCh()**

```go
func (s *AgentServer) startConsumeLoop(ctx context.Context) {
    recvCh, err := s.transport.Recv()
    if err != nil {
        logger.Error(logComponent).Err(err).Msg("获取接收通道失败")
        return
    }
    for {
        select {
        case <-ctx.Done():
            logger.Info(logComponent).Msg("AgentServer 消费循环退出（上下文取消）")
            return
        case data, ok := <-recvCh:
            if !ok {
                logger.Info(logComponent).Msg("AgentServer 消费循环退出（通道已关闭）")
                return
            }
            // ... 解码 + 分发逻辑不变
        }
    }
}
```

- [ ] **Step 3: 修改 connection.ack 发送 — 用 Send() 替代 RecvCh()**

将 Start() 中发送 connection.ack 的逻辑改为：

```go
ackFrame := transportpkg.BuildConnectionAckFrame()
ackData, err := json.Marshal(ackFrame)
if err != nil {
    logger.Error(logComponent).Err(err).Msg("编码 connection.ack 失败")
} else if err := s.transport.Send(ctx, ackData); err != nil {
    logger.Error(logComponent).Err(err).Msg("发送 connection.ack 失败")
} else {
    logger.Info(logComponent).Msg("AgentServer 已就绪（connection.ack 已发送）")
}
```

注意：原来用 `select + default` 非阻塞写入 RecvCh（防止满通道阻塞），改为 `Send()` 后是阻塞调用。这是正确的——Python 的 `ws.send()` 也是阻塞的，如果连接出问题应该报错而不是静默丢弃。

- [ ] **Step 4: 修改 handle_envelope.go 中的写响应方法**

三个方法都需要改：`writeResponse`、`writeChunk`、`sendKeepalive`。

以 `writeResponse` 为例：

```go
func (s *AgentServer) writeResponse(requestID, channelID string, resp *schema.AgentResponse) {
    wire := e2a.EncodeAgentResponseForWire(resp, requestID, 0)
    data, err := json.Marshal(wire)
    if err != nil {
        logger.Error(logComponent).Err(err).Str("request_id", requestID).Msg("响应编码失败")
        return
    }
    if err := s.transport.Send(context.Background(), data); err != nil {
        logger.Warn(logComponent).Err(err).Str("request_id", requestID).Msg("发送响应失败")
    }
}
```

`writeChunk` 和 `sendKeepalive` 同理：`case s.transport.RecvCh() <- data:` → `s.transport.Send(context.Background(), data)`。

注意：原来用 `select + default` 非阻塞写入丢弃满通道消息。改为 `Send()` 后如果通道满会阻塞。对于响应写入，阻塞比静默丢弃更合理——丢弃响应会导致客户端永远等不到回复。

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/...
```

- [ ] **Step 6: 测试验证**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/... -v -count=1 -timeout 30s
```

- [ ] **Step 7: 提交**

```bash
git add -A && git commit -m "refactor: AgentServer 改用 AgentTransport 接口，handleEnvelope 改用 Send/Recv"
```

---

### Task 3: Start() 改为非阻塞 + 按 Python 顺序补齐初始化步骤

**Files:**
- Modify: `internal/swarm/server/agent_server.go`
- Modify: `internal/swarm/server/adapter/deep_adapter.go` — 导出 EnsurePersistentCheckpointer
- Modify: `internal/swarm/server/adapter/doc.go`

- [ ] **Step 1: 导出 EnsurePersistentCheckpointer**

在 `internal/swarm/server/adapter/deep_adapter.go` 中：
- 将 `func ensurePersistentCheckpointer() error` 改为 `func EnsurePersistentCheckpointer() error`
- 更新注释中的函数名引用
- `setCheckpoint()` 内部调用同步改为 `EnsurePersistentCheckpointer()`

- [ ] **Step 2: 更新 adapter/doc.go**

在文件目录中确认 `deep_adapter.go` 包含 `EnsurePersistentCheckpointer` 导出函数。

- [ ] **Step 3: 重构 AgentServer.Start() 为非阻塞**

在 `agent_server.go` 中添加新字段：

```go
type AgentServer struct {
    // ... 已有字段
    // cancel 停止 AgentServer 的取消函数
    cancel context.CancelFunc
    // stopCh run() 退出时关闭的信号通道
    stopCh chan struct{}
    // runErr run() 的返回错误
    runErr error
}
```

修改 `NewAgentServer`：

```go
func NewAgentServer(cfg *config.Config, transport transport.AgentTransport) *AgentServer {
    return &AgentServer{
        config:             cfg,
        transport:          transport,
        sessionStreamTasks: make(map[string]context.CancelFunc),
        sessionsDir:        workspace.AgentSessionsDir(),
        stopCh:             make(chan struct{}),
    }
}
```

新增 `run(ctx) error` 阻塞方法（原 Start 的逻辑，按 Python 顺序补齐）：

```go
// run 执行 AgentServer 主循环（阻塞直到 ctx 取消）。
// 按 Python AgentWebSocketServer.start() + app_agentserver.py _run() 顺序初始化。
func (s *AgentServer) run(ctx context.Context) error {
    defer close(s.stopCh)

    // 1. 重置 harness 包状态到 native（对齐 Python agent_ws_server.py L440）
    // TODO(⤵️ AutoHarness): 实现 resetHarnessPackagesState()
    s.resetHarnessPackagesState()

    // 2. 确保持久化检查点器就绪（对齐 Python agent_ws_server.py L443）
    if err := adapter.EnsurePersistentCheckpointer(); err != nil {
        logger.Error(logComponent).Err(err).Msg("持久化检查点器初始化失败")
        // 对齐 Python：raise RuntimeError，Go 侧记录错误但继续启动（best-effort）
    }

    // 3. 初始化 AgentManager
    s.agentManager = runtime.NewAgentManager()
    logger.Info(logComponent).Msg("AgentManager 已初始化")

    // 4. 发送 connection.ack 事件帧
    ackFrame := transportpkg.BuildConnectionAckFrame()
    ackData, err := json.Marshal(ackFrame)
    if err != nil {
        logger.Error(logComponent).Err(err).Msg("编码 connection.ack 失败")
    } else if err := s.transport.Send(ctx, ackData); err != nil {
        logger.Error(logComponent).Err(err).Msg("发送 connection.ack 失败")
    } else {
        logger.Info(logComponent).Msg("AgentServer 已就绪（connection.ack 已发送）")
    }

    // 5. 沙箱自动启动（对齐 Python agent_ws_server.py L475）
    // TODO(⤵️ JiuwenBox): 实现 bootstrapInternalJiuwenbox()
    s.bootstrapInternalJiuwenbox()

    // 6. 队友启动守护进程（对齐 Python app_agentserver.py L156）
    // TODO(⤵️ Team): 实现 startTeammateBootstrapDaemon()
    s.startTeammateBootstrapDaemon(ctx)

    // 7. 进入消费循环（阻塞直到 ctx 取消）
    s.startConsumeLoop(ctx)

    return nil
}
```

修改 `Start()` 为非阻塞：

```go
// Start 启动 AgentServer（非阻塞，内部起 goroutine 运行主循环）。
// 对齐 Python: AgentWebSocketServer.start() 风格——调用方无需 go 包一层。
func (s *AgentServer) Start(ctx context.Context) error {
    s.runningMu.Lock()
    if s.running {
        s.runningMu.Unlock()
        logger.Warn(logComponent).Msg("AgentServer 已在运行中，跳过重复启动")
        return nil
    }
    s.running = true
    s.runningMu.Unlock()

    ctx, s.cancel = context.WithCancel(ctx)
    go func() {
        s.runErr = s.run(ctx)
        s.runningMu.Lock()
        s.running = false
        s.runningMu.Unlock()
    }()

    return nil
}
```

修改 `Stop()` 按 Python 顺序补齐清理步骤：

```go
// Stop 停止 AgentServer：取消所有任务 → 清理 AgentManager → 等待主循环退出。
func (s *AgentServer) Stop() error {
    // 1. 取消所有流式任务
    s.cancelAllStreamTasks()
    logger.Info(logComponent).Msg("所有流式任务已取消")

    // 2. 取消所有进行中的任务（对齐 Python agent_ws_server.py L726）
    // TODO(⤵️ AgentManager): 实现 cancelAllInflightWork()
    s.cancelAllInflightWork()

    // 3. 停止调度器（对齐 Python agent_ws_server.py L732）
    // TODO(⤵️ Scheduler): 实现 stopScheduler()
    s.stopScheduler()

    // 4. 取消所有 team 流式任务（对齐 Python agent_ws_server.py L737）
    // TODO(⤵️ Team): 实现 cancelAllTeamStreamTasks()
    s.cancelAllTeamStreamTasks()

    // 5. 清理 AgentManager
    if s.agentManager != nil {
        if err := s.agentManager.Cleanup(); err != nil {
            logger.Error(logComponent).Err(err).Msg("AgentManager 清理失败")
        } else {
            logger.Info(logComponent).Msg("AgentManager 已清理")
        }
    }

    // 6. 取消主循环
    if s.cancel != nil {
        s.cancel()
    }

    // 7. 等待主循环退出
    <-s.stopCh

    logger.Info(logComponent).Msg("AgentServer 已停止")
    return s.runErr
}
```

- [ ] **Step 4: 添加 7 个 TODO stub 方法**

在 `agent_server.go` 非导出函数区域添加：

```go
// resetHarnessPackagesState 重置 harness 包状态到 native。
// 对齐 Python: jiuwenswarm/agents/harness/common/auto_harness/service.py reset_harness_packages_state()
// TODO(⤵️ AutoHarness): 清空 harness-packages.json 中的 active_package_ids
func (s *AgentServer) resetHarnessPackagesState() {
    // 未实现：等 AutoHarness 包管理系统实现后回填
}

// bootstrapInternalJiuwenbox 沙箱自动启动。
// 对齐 Python: jiuwenswarm/server/agent_ws_server.py _bootstrap_internal_jiuwenbox()
// TODO(⤵️ JiuwenBox): 按 config.yaml::sandbox.startup_mode 决定是否自动拉起 jiuwenbox
func (s *AgentServer) bootstrapInternalJiuwenbox() {
    // 未实现：等 JiuwenBox 沙箱系统实现后回填
}

// startTeammateBootstrapDaemon 启动队友 bootstrap 守护进程。
// 对齐 Python: jiuwenswarm/agents/harness/team/remote_member_bootstrap.py run_teammate_bootstrap_daemon()
// TODO(⤵️ Team): 启动守护 goroutine 消费远程队友 bootstrap
func (s *AgentServer) startTeammateBootstrapDaemon(ctx context.Context) {
    // 未实现：等 Team 功能实现后回填
}

// cancelAllInflightWork 取消所有进行中的任务。
// 对齐 Python: jiuwenswarm/server/runtime/agent_manager.py cancel_all_inflight_work()
// TODO(⤵️ AgentManager): 等 AgentManager inflight work 追踪实现后回填
func (s *AgentServer) cancelAllInflightWork() {
    // 未实现：等 AgentManager 完整实现后回填
}

// stopScheduler 停止调度器。
// 对齐 Python: jiuwenswarm/server/agent_ws_server.py _stop_scheduler()
// TODO(⤵️ Scheduler): 等调度器实现后回填
func (s *AgentServer) stopScheduler() {
    // 未实现：等调度器实现后回填
}

// cancelAllTeamStreamTasks 取消所有 team 流式任务。
// 对齐 Python: jiuwenswarm/agents/harness/team/ cancel_all_team_stream_tasks_across_managers()
// TODO(⤵️ Team): 等 Team 流式任务管理实现后回填
func (s *AgentServer) cancelAllTeamStreamTasks() {
    // 未实现：等 Team 功能实现后回填
}
```

- [ ] **Step 5: 添加 `swarm/server/adapter` import**

在 `agent_server.go` 的 import 中添加：
```go
"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
```

- [ ] **Step 6: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/...
```

- [ ] **Step 7: 测试验证**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/... -v -count=1 -timeout 30s
```

- [ ] **Step 8: 提交**

```bash
git add -A && git commit -m "feat: Start() 改非阻塞 + ensurePersistentCheckpointer + 7 个 TODO stub 对齐 Python"
```

---

### Task 4: cmd.go 简化 + 全量编译测试

**Files:**
- Modify: `cmd/uapclaw/cmd.go`

- [ ] **Step 1: 简化 cmd.go 中 AgentServer 启动**

将：
```go
// 先启动 AgentServer（goroutine），它会 Init AgentManager → 标记 serverReady → 进入消费循环
go func() {
    if err := agentServer.Start(ctx); err != nil {
        logger.Error(logger.ComponentAgentServer).
            Err(err).
            Msg("AgentServer 启动失败")
    }
}()
```

改为：
```go
// 启动 AgentServer（非阻塞，内部起 goroutine 运行主循环）
if err := agentServer.Start(ctx); err != nil {
    return fmt.Errorf("启动 AgentServer 失败: %w", err)
}
```

同时修改停止逻辑，将：
```go
_ = agentServer.Stop()
```
改为（Stop() 现在会等 run() 退出）：
```go
if err := agentServer.Stop(); err != nil {
    logger.Error(logger.ComponentAgentServer).Err(err).Msg("AgentServer 停止失败")
}
```

- [ ] **Step 2: 全量编译**

```bash
cd /home/opensource/uapclaw-gateway && go build ./...
```

- [ ] **Step 3: 全量测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/... -count=1 -timeout 60s
```

- [ ] **Step 4: cmd 包测试**

```bash
cd /home/opensource/uapclaw-gateway && go test -tags test ./cmd/... -count=1 -timeout 30s
```

- [ ] **Step 5: 提交**

```bash
git add -A && git commit -m "refactor: cmd.go 简化 AgentServer 启动（Start 非阻塞，不再 go 包一层）"
```

---

### Task 5: 更新 agent_server_test.go 适配新 Start/Stop 语义

**Files:**
- Modify: `internal/swarm/server/agent_server_test.go`

- [ ] **Step 1: 适配 Start 非阻塞语义**

原测试中 Start() 是阻塞的（配合 ctx cancel 退出），现在 Start() 非阻塞。

检查测试中是否有依赖 Start() 阻塞行为的逻辑，如有则调整：
- `TestAgentServer_Start_发送ConnectionAck`：Start() 现在非阻塞返回，connection.ack 在 goroutine 中发送。测试需要短暂等待或通过 Recv() 读取验证。
- `TestAgentServer_ConsumeEnvelope`：同上。

具体调整：在 Start() 返回后，通过 `transport.Recv()` 读取 connection.ack 验证（已有此模式，只需确保时序正确，可能需要添加短暂 `time.Sleep` 或 channel 读取超时）。

- [ ] **Step 2: 适配 Stop 等待语义**

原 Stop() 不等 goroutine 退出，新 Stop() 会等 `stopCh` 关闭。测试中 Stop() 调用现在是同步的——更简单，不需要额外调整。

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/... -v -count=1 -timeout 30s
```

- [ ] **Step 4: 提交**

```bash
git add -A && git commit -m "test: 适配 AgentServer Start/Stop 非阻塞语义"
```
