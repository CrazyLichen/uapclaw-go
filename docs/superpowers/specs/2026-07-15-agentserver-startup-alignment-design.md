# AgentServer 启动逻辑对齐 + Transport 接口抽象修复

## 背景

当前 Go 的 AgentServer 启动逻辑与 Python 存在多处差异，且 AgentServer 绑定 ChannelTransport 具体类型，违反 transport 抽象架构（项目规则 6），导致将来 `uapclaw agentserver` 独立部署无法实现。

## 问题清单

### P1：Start() 阻塞设计——cmd 不得不 go 包一层

`AgentServer.Start(ctx)` 是阻塞方法（`startConsumeLoop` 阻塞到 ctx 取消），cmd.go 中需要 `go agentServer.Start(ctx)`。Python 的 `server.start()` 自己管理生命周期，调用方不需要额外包协程/进程。

### P2：Start() 缺少 Python 对应的初始化步骤

Python `AgentWebSocketServer.start()` + `app_agentserver.py` 的 `_run()` 包含 7 个 Go 中缺失的步骤：
1. `reset_harness_packages_state()` — 重置 harness 包状态到 native
2. `ensure_persistent_checkpointer()` — 初始化 SQLite 持久化 checkpointer
3. Extension 系统初始化（已在 cmd.go TODO）
4. `_bootstrap_internal_jiuwenbox()` — 沙箱自动启动
5. `run_teammate_bootstrap_daemon()` — 队友启动守护
6. `cancel_all_inflight_work()` — 连接断开时取消进行中任务
7. `_stop_scheduler()` + `cancel_all_team_stream_tasks()` — 停调度器 + 取消 team 流式任务

### P3：AgentServer 持有 ChannelTransport 具体类型，非 AgentTransport 接口

```go
type AgentServer struct {
    transport *gateway_push.ChannelTransport  // ❌ 具体类型
}
```

违反项目规则 6.1（包依赖方向）和 6.5（禁止直接操作 ChannelTransport 的 SendCh/RecvCh）。

### P4：handle_envelope / startConsumeLoop / connection.ack 绕过 AgentTransport 接口

直接操作 `s.transport.RecvCh() <- data` 和 `s.transport.SendCh()` 写/读，绕过接口的 Send/Recv 方法。

### P5：ChannelTransport 位置不当——应在 transport 包

`server/gateway_push/` 包只包含 ChannelTransport + 接口合规声明，Python 的 `gateway_push` 是 server_push 下行推送（不同概念）。Go 的 gateway_push 实际就是 ChannelTransport，应移入 `transport/` 包。

## 设计

### 1. Start() 改为非阻塞

**变更：**

- `Start(ctx)` 内部 `go s.run(ctx)` 并返回 error
- 新增 `run(ctx) error` 阻塞方法（原 Start 的逻辑移入）
- 新增 `stopCh chan struct{}` 字段，`run()` 退出时 close
- `Stop()` 中 cancel ctx（需新增 `cancel context.CancelFunc` 字段）+ 等 `stopCh` 关闭

**Start() 内部步骤顺序（对齐 Python）：**

```
1. 防重入检查
2. resetHarnessPackagesState()           ← TODO stub（对齐 Python L440）
3. ensurePersistentCheckpointer(ctx)     ← 完整实现（对齐 Python L443）
4. 初始化 AgentManager
5. 发送 connection.ack（通过 transport.Send）
6. bootstrapInternalJiuwenbox()          ← TODO stub（对齐 Python L475）
7. startTeammateBootstrapDaemon(ctx)     ← TODO stub（对齐 Python app_agentserver L156）
8. 进入消费循环（阻塞）
```

**Stop() 内部步骤顺序（对齐 Python）：**

```
1. cancelAllStreamTasks()                ← 已有
2. cancelAllInflightWork()               ← TODO stub（对齐 Python L726）
3. stopScheduler()                       ← TODO stub（对齐 Python L732）
4. cancelAllTeamStreamTasks()            ← TODO stub（对齐 Python L737）
5. agentManager.Cleanup()               ← 已有
6. 标记 running = false
7. 关闭 stopCh
```

### 2. ensurePersistentCheckpointer 完整实现

**位置：** `swarm/server/adapter/deep_adapter_init.go`

**实现：**

```go
// EnsurePersistentCheckpointer 确保全局默认 checkpointer 使用 SQLite 持久化。
// 对齐 Python: server/runtime/agent_adapter/interface_deep.py L393-424
func EnsurePersistentCheckpointer(ctx context.Context) error {
    checkpointDir := workspace.CheckpointDir()
    cp, err := checkpointer.CreateCheckpointer(ctx, checkpointer.CheckpointerFactoryConfig{
        Type: "persistence",
        Conf: map[string]any{
            "db_type": "sqlite",
            "db_path": filepath.Join(checkpointDir, "checkpoint"),
        },
    })
    if err != nil {
        return fmt.Errorf("持久化 checkpointer 初始化失败: %w", err)
    }
    checkpointer.SetDefaultCheckpointer(cp)
    return nil
}
```

### 3. AgentServer 改用 AgentTransport 接口

**变更：**

```go
type AgentServer struct {
    transport transport.AgentTransport  // 接口类型，不依赖具体实现
    // ...
}

func NewAgentServer(cfg *config.Config, transport transport.AgentTransport) *AgentServer
func (s *AgentServer) Transport() transport.AgentTransport
```

**startConsumeLoop 改造：**

```go
func (s *AgentServer) startConsumeLoop(ctx context.Context) {
    recvCh, err := s.transport.Recv()
    if err != nil { return }
    for {
        select {
        case <-ctx.Done(): return
        case data, ok := <-recvCh:
            // ... 解码 + 分发
        }
    }
}
```

**handleEnvelope 写响应改造：**

```go
// 原: case s.transport.RecvCh() <- data:
// 新:
if err := s.transport.Send(ctx, data); err != nil {
    logger.Error(logComponent).Err(err).Msg("发送响应失败")
}
```

**connection.ack 发送改造：**

```go
// 原: case s.transport.RecvCh() <- ackData:
// 新:
if err := s.transport.Send(ctx, ackData); err != nil {
    logger.Error(logComponent).Err(err).Msg("发送 connection.ack 失败")
}
```

### 4. ChannelTransport 移入 transport 包

**操作：**

- `server/gateway_push/channel_transport.go` → `swarm/transport/channel_transport.go`
- `server/gateway_push/channel_transport_test.go` → `swarm/transport/channel_transport_test.go`
- 接口合规声明 `var _ AgentTransport = (*ChannelTransport)(nil)` 放入 `channel_transport.go`
- `SendCh()` / `RecvCh()` 辅助方法保留（ChannelTransport 特有，AgentClient.Connect 需要）
- 更新 `transport/doc.go` 文件目录
- 删除 `server/gateway_push/` 包
- 更新所有 import：
  - `cmd.go`: `gateway_push.NewChannelTransport()` → `transport.NewChannelTransport()`
  - `agent_server.go`: 删除 `server/gateway_push` import
  - `agent_server_test.go`: 更新 import
  - 其他引用处

### 5. cmd.go 简化

```go
// 原:
go func() {
    if err := agentServer.Start(ctx); err != nil { ... }
}()

// 新:
if err := agentServer.Start(ctx); err != nil {
    return fmt.Errorf("启动 AgentServer 失败: %w", err)
}
```

### 6. 不动项

- `harness.ResetFreeSearchRuntimeFlags()` 保留 cmd.go（Gateway + AgentServer 共有的全局环境准备）
- 扩展系统 TODO 保留 cmd.go（两边共享，等扩展系统实现后回填）
- `BuildServerPushWire` / `BuildConnectionAckFrame` / `WireRequestIDKey` 保留 `transport/wire.go`

## 受影响文件

| 文件 | 变更类型 |
|------|---------|
| `swarm/server/agent_server.go` | 重构：Start/Stop 改非阻塞，transport 改接口，7 个 TODO stub |
| `swarm/server/agent_server_test.go` | 适配：更新 import 和 transport 创建方式 |
| `swarm/server/handle_envelope.go` | 修改：写响应改用 transport.Send() |
| `swarm/server/handle_envelope_test.go` | 适配：如有直接操作 RecvCh 的测试 |
| `swarm/server/adapter/deep_adapter_init.go` | 新增：EnsurePersistentCheckpointer 实现 |
| `swarm/transport/channel_transport.go` | 移入：从 gateway_push 移来 |
| `swarm/transport/channel_transport_test.go` | 移入：从 gateway_push 移来 |
| `swarm/transport/doc.go` | 更新：文件目录 + 包描述 |
| `swarm/server/gateway_push/` | 删除：整个包 |
| `cmd/uapclaw/cmd.go` | 简化：Start 不再 go，import 改 transport |
| `swarm/gateway/routing/agent_client.go` | 适配：import 改 transport |
| `swarm/gateway/routing/agent_client_test.go` | 适配：import 改 transport |
