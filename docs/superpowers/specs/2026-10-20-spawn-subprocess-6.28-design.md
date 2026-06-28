# 6.28 Spawn 子进程设计

## 概述

本文档描述领域六第 6.28 小节——Spawn 子进程的 Go 实现设计。目标是对齐 Python `openjiuwen/core/runner/spawn/` 的 OS 级子进程机制，通过 JSON over stdin/stdout（NDJSON 协议）实现父子进程通信，在独立子进程中隔离运行 Agent。

### 流程位置

6.28 属于领域六 Agent 核心 → Runner 编排子组，位于编排链末端：

```
6.23 ResourceMgr (✅) → 6.24 AsyncCallbackFramework (✅) → 6.25 Runner单例 (✅)
→ 6.26 RunnerConfig (✅) → 6.27 LocalMessageQueue (✅) → 6.28 Spawn子进程 (☐) → 6.29 AgentPrompts (☐)
```

在 Agent 执行会话中的位置：

```
1. Runner.Start()        — 初始化资源管理器、消息队列、回调框架
2. Runner.RunAgent()     — 在当前进程执行 Agent
3. Runner.SpawnAgent()   — 【6.28】在子进程中执行 Agent
4. Runner.Stop()         — 清理释放
```

### 核心作用

1. **隔离性**：子进程崩溃不影响主进程，Agent 的内存泄漏、死循环等异常不会波及 Runner
2. **并行性**：多个 Agent 可在不同子进程中并行执行，互不干扰
3. **安全性**：Agent 代码在沙箱化子进程中运行，限制对主进程资源的访问

## 关键决策

| 决策项 | 选择 | 理由 |
|--------|------|------|
| 子进程模型 | OS 级子进程（与 Python 对齐） | 完全对齐 Python，实现真正进程隔离 |
| 子进程入口 | 主二进制 + spawn-child 子命令 | 一次编译，`uap-claw spawn-child` 启动，无需额外二进制 |
| CLASS_AGENT 实例化 | ResourceMgr 注册表查找 | Go 无动态 import，用 agent_name 查注册表工厂函数 |
| 后台任务管理 | 复用 `utils.BackgroundTask`（1.8 已实现） | 项目已有完整实现，无需新建 |
| 环境变量前缀 | `UAPCLAW_SPAWN_PROCESS` / `UAPCLAW_SPAWN_LOGGING_CONFIG` | 与项目名对齐，只传给子进程不影响主进程 |
| stdout 重定向 | 无需重定向 | Go 的 os/exec 管道天然隔离，子进程日志输出到 os.Stderr |
| Windows 兼容 | 标准库自动满足 | Go 运行时处理 Windows 管道兼容，SIGTERM 在 Windows 直接 kill |
| 文件组织 | 5 文件方案（protocol/config/handle/process/child） | 职责清晰，对齐 PROJECT_STRUCTURE.md |

## 文件结构

```
internal/agentcore/runner/spawn/
├── doc.go              # 包文档
├── protocol.go         # 消息协议（MessageType 枚举 + Message 结构体 + 序列化/反序列化）
├── protocol_test.go    # 协议层测试
├── config.go           # 配置模型（SpawnAgentKind + SpawnAgentConfig + ClassAgentSpawnConfig + SpawnConfig）
├── config_test.go      # 配置层测试
├── handle.go           # 父端进程管理器（SpawnedProcessHandle：send/receive/health_check/shutdown）
├── handle_test.go      # 父端管理器测试
├── process.go          # 子进程创建工厂（SpawnProcess 函数）
├── process_test.go     # 创建工厂测试
├── child.go            # 子进程侧逻辑（消息循环 + Agent 执行 + 健康检查处理 + 关闭处理）
└── child_test.go       # 子端逻辑测试
```

## 模块设计

### 1. protocol.go — 通信协议层

**对应 Python**：`spawn/protocol.py`

#### MessageType 枚举

```go
type MessageType int

const (
    MessageTypeInput               MessageType = iota // INPUT — 父→子：输入数据/Agent配置
    MessageTypeOutput                                   // OUTPUT — 子→父：输出结果
    MessageTypeHealthCheck                              // HEALTH_CHECK — 父→子：健康检查请求
    MessageTypeHealthCheckResponse                      // HEALTH_CHECK_RESPONSE — 子→父：健康检查响应
    MessageTypeShutdown                                 // SHUTDOWN — 父→子：关闭请求
    MessageTypeShutdownAck                              // SHUTDOWN_ACK — 子→父：关闭确认
    MessageTypeError                                    // ERROR — 子→父：错误报告
    MessageTypeStreamChunk                              // STREAM_CHUNK — 子→父：流式块
    MessageTypeDone                                     // DONE — 子→父：执行完成
)
```

#### Message 结构体

```go
type Message struct {
    Type      MessageType `json:"type"`
    Payload   any         `json:"payload"`
    Timestamp time.Time   `json:"timestamp"`
    MessageID string      `json:"message_id"`
}
```

#### 序列化/反序列化函数

```go
// SerializeMessage 序列化消息为 JSON 字节。
func SerializeMessage(msg Message) ([]byte, error)

// DeserializeMessage 反序列化 JSON 字节为消息。
func DeserializeMessage(data []byte) (Message, error)

// WriteMessage 写入消息到 io.Writer（JSON + \n）。
func WriteMessage(w io.Writer, msg Message) error

// ReadMessage 从 io.Reader 读取一行并反序列化为消息。
// 跳过非 JSON 行（子进程可能输出非协议日志到 stdout）。
func ReadMessage(r io.Reader) (Message, error)
```

**与 Python 差异**：
- Python 用 `asyncio.StreamReader/StreamWriter`，Go 用 `io.Reader/io.Writer`（同步，调用方决定是否在 goroutine 中使用）
- Python 的 `serialize_message` 用 `await asyncio.sleep(0)` 协作让步，Go 不需要
- `ReadMessage` 对齐 Python 的 `deserialize_message_from_stream`：跳过非 JSON 行

#### 通信协议时序

```
父进程                                      子进程
──────                                      ──────

SpawnProcess() ──[os/exec]──>               RunSpawnedProcess()
  │                                            │
  ├── INPUT {agent_config, inputs} ──>        ProcessMessageLoop()
  │                                            ├── 解析 agentConfig
  │                                            ├── 启动 agentTask
  │                                            │     ├── ExecuteAgent()
  │                                            │     │     ├── CLASS_AGENT: GetAgent() + RunAgent()
  │                                            │     │     └── TEAM_AGENT: FromSpawnPayload() + RunAgentTeam()
  │                                            │     ├── 成功: ──> DONE {result}
  │                                            │     └── 失败: ──> ERROR {error, error_type}
  │                                            │
  │ <── STREAM_CHUNK {chunk} ───────────────  │  (流式模式)
  │ <── DONE {result} ──────────────────────  │
  │                                            │
  [周期性]
  HEALTH_CHECK ──────────────────────>        HandleHealthCheck()
  │ <── HEALTH_CHECK_RESPONSE ──────────────  │
  │                                            │
  SHUTDOWN ──────────────────────────>        HandleShutdown()
  │ <── SHUTDOWN_ACK ────────────────────────  │
```

### 2. config.go — 配置模型层

**对应 Python**：`spawn/agent_config.py`

#### SpawnAgentKind 枚举

```go
type SpawnAgentKind string

const (
    SpawnAgentKindClassAgent SpawnAgentKind = "class_agent"
    SpawnAgentKindTeamAgent  SpawnAgentKind = "team_agent"
)
```

#### SpawnAgentConfig

```go
type SpawnAgentConfig struct {
    AgentKind     SpawnAgentKind `json:"agent_kind"`
    RunnerConfig  map[string]any `json:"runner_config,omitempty"`
    LoggingConfig map[string]any `json:"logging_config,omitempty"`
    SessionID     string         `json:"session_id,omitempty"`
    Payload       map[string]any `json:"payload"`
}
```

#### ClassAgentSpawnConfig

```go
type ClassAgentSpawnConfig struct {
    SpawnAgentConfig
    AgentName  string         `json:"agent_name"`            // ResourceMgr 注册表中的名字
    InitKwargs map[string]any `json:"init_kwargs,omitempty"` // 实例化参数
}
```

`AgentName` 替代 Python 的 `agent_module` + `agent_class`（动态导入），通过 `resources_manager.GetAgent(agentName)` 查找已注册的 Agent 工厂函数。

#### SpawnConfig

```go
type SpawnConfig struct {
    HealthCheckInterval time.Duration // 默认 5s
    ShutdownTimeout     time.Duration // 默认 10s
    HealthCheckTimeout  time.Duration // 默认 3s
}

func DefaultSpawnConfig() SpawnConfig
```

用 `time.Duration` 替代 Python 的 `float`（秒），符合 Go 惯例。

#### 配置解析函数

```go
func ParseSpawnAgentConfig(payload map[string]any) (SpawnAgentConfig, error)
func SerializeRunnerConfig(cfg *config.RunnerConfig) (map[string]any, error)
func DeserializeRunnerConfig(payload map[string]any) (*config.RunnerConfig, error)
```

### 3. handle.go — 父端进程管理器

**对应 Python**：`spawn/process_manager.py` 中的 `SpawnedProcessHandle`

#### SpawnedProcessHandle 结构体

```go
type SpawnedProcessHandle struct {
    processID         string                  // 进程标识（UUID）
    cmd               *exec.Cmd               // os/exec 子进程对象
    stdin             io.WriteCloser          // 子进程 stdin 管道
    stdout            io.Reader               // 子进程 stdout 管道
    config            SpawnConfig             // 管理配置
    onUnhealthy       func()                  // 不健康回调（可选，nil 表示无回调）
    maxHealthFailures int                     // 最大连续失败次数，默认 2

    // 内部状态
    healthCheckTask   *utils.BackgroundTask   // 健康检查后台任务
    isHealthy         bool                    // 是否健康
    shutdownRequested bool                    // 是否已请求关闭
    consecutiveFails  int                     // 连续失败次数
    unhealthyFired    bool                    // onUnhealthy 是否已触发
    mu                sync.Mutex              // 保护内部状态
}
```

#### 属性方法

```go
func (h *SpawnedProcessHandle) ProcessID() string
func (h *SpawnedProcessHandle) IsAlive() bool          // 进程仍在运行
func (h *SpawnedProcessHandle) PID() int               // OS 进程 ID
func (h *SpawnedProcessHandle) ExitCode() int          // 退出码
func (h *SpawnedProcessHandle) IsHealthy() bool        // isHealthy && IsAlive()
```

#### 通信方法

```go
func (h *SpawnedProcessHandle) SendMessage(ctx context.Context, msg Message) error
func (h *SpawnedProcessHandle) ReceiveMessage(ctx context.Context) (Message, error)
```

#### 健康检查方法

```go
func (h *SpawnedProcessHandle) StartHealthCheck(ctx context.Context, interval ...time.Duration) error
func (h *SpawnedProcessHandle) StopHealthCheck() error
```

- 健康检查是**跨进程消息通信**：父进程发送 HEALTH_CHECK，子进程回复 HEALTH_CHECK_RESPONSE
- 后台协程使用 `utils.BackgroundTask`（1.8 节已实现）
- 连续失败次数 >= `maxHealthFailures` 时触发 `onUnhealthy` 回调（只触发一次）

#### 关闭方法

```go
func (h *SpawnedProcessHandle) Shutdown(ctx context.Context, timeout ...time.Duration) (bool, error)
func (h *SpawnedProcessHandle) ForceKill() error
func (h *SpawnedProcessHandle) WaitForCompletion() (int, error)
```

**Shutdown 流程**（对齐 Python）：
1. 停止健康检查
2. 发送 SHUTDOWN 消息
3. 等待 SHUTDOWN_ACK（带超时，默认 10s）
4. 等待进程退出（2s 宽限）
5. 超时则回退到 ForceKill

**ForceKill 平台兼容**：
- Unix：`cmd.Process.Signal(syscall.SIGTERM)` → 等 3s → `cmd.Process.Kill()`（SIGKILL）
- Windows：直接 `cmd.Process.Kill()`（SIGTERM 在 Windows 无效）

### 4. process.go — 子进程创建工厂

**对应 Python**：`spawn/process_manager.py` 中的 `spawn_process()`

```go
func SpawnProcess(
    ctx context.Context,
    agentConfig SpawnAgentConfig,
    inputs map[string]any,
    config ...SpawnConfig,
) (*SpawnedProcessHandle, error)
```

**内部流程**：
1. 生成 UUID 作为 processID
2. 获取自身可执行文件路径：`os.Executable()`
3. 构建子进程命令：`exec.Command(exePath, "spawn-child")`
4. 设置管道：`cmd.Stdin/Stdout/Stderr` 全部 PIPE
5. 构建环境变量：继承 `os.Environ()` + `UAPCLAW_SPAWN_PROCESS=1` + 日志配置
6. 启动子进程：`cmd.Start()`
7. 创建 `SpawnedProcessHandle`
8. 发送初始 INPUT 消息（包含 agent_config + inputs）
9. 返回 handle

### 5. child.go — 子进程侧逻辑

**对应 Python**：`spawn/child_process.py`

#### 入口函数

```go
func RunSpawnedProcess(ctx context.Context, agentConfig map[string]any, inputs map[string]any) error
```

**内部流程**：
1. 检查 `UAPCLAW_SPAWN_PROCESS` 环境变量
2. 应用日志配置（从 `UAPCLAW_SPAWN_LOGGING_CONFIG` 解析）
3. 解析 agentConfig → SpawnAgentConfig
4. 如果有 runnerConfig → SetConfig()
5. Runner.Start()
6. ProcessMessageLoop()
7. Runner.Stop()

#### 消息循环

```go
func ProcessMessageLoop(
    ctx context.Context,
    stdin io.Reader,
    stdout io.Writer,
    agentConfig *SpawnAgentConfig,
    inputs map[string]any,
) error
```

**竞争机制**（对齐 Python 的 `asyncio.wait`）：

Python 用 `asyncio.wait({read_task, agent_task}, return_when=FIRST_COMPLETED)`。Go 用 `select` + channel：

```go
msgCh := make(chan Message, 1)
errCh := make(chan error, 1)
go func() {
    msg, err := ReadMessage(stdin)
    if err != nil { errCh <- err; return }
    msgCh <- msg
}()

select {
case msg := <-msgCh:    // stdin 有消息
case <-agentDoneCh:     // agent 任务完成
case err := <-errCh:    // stdin 读取错误
}
```

**消息处理**：
- `HEALTH_CHECK` → `HandleHealthCheck()` → 回复 HEALTH_CHECK_RESPONSE
- `SHUTDOWN` → 取消 agentTask → `HandleShutdown()` → 回复 SHUTDOWN_ACK → 退出
- `INPUT` → 提取 agentConfig + inputs → 启动 agentTask（只启动一次）

#### Agent 执行

```go
func ExecuteAgent(
    ctx context.Context,
    agentConfig SpawnAgentConfig,
    inputs map[string]any,
    stdout io.Writer,
    streaming bool,
    streamModes []string,
) (any, error)
```

**CLASS_AGENT**：
1. `resources_manager.GetAgent(agentName)` 查注册表
2. 构建实例（应用 initKwargs）
3. 非流式：`Runner.RunAgent()`
4. 流式：`Runner.RunAgentStreaming()`，每个 chunk 发送 STREAM_CHUNK

**TEAM_AGENT**：
1. `TeamAgent.FromSpawnPayload(payload)` 构造
2. 非流式：`Runner.RunAgentTeam(member=true)`
3. 流式：`Runner.RunAgentTeamStreaming(member=true)`，每个 chunk 发送 STREAM_CHUNK

#### 消息处理器

```go
func HandleHealthCheck(ctx context.Context, msg Message, stdout io.Writer) error
func HandleShutdown(ctx context.Context, msg Message, stdout io.Writer) error
```

#### Agent 任务包装

```go
func runAgentTask(
    ctx context.Context,
    agentConfig SpawnAgentConfig,
    inputs map[string]any,
    stdout io.Writer,
    messageID string,
    streaming bool,
    streamModes []string,
) error
```

成功发送 DONE，失败发送 ERROR（对齐 Python `_run_agent_task`）。

#### Windows 兼容

Go 的 `os/exec` 管道在 Windows 上由 Go 运行时正确处理，`bufio.Scanner` 读取管道在 Windows 也能正常工作。无需额外平台特殊代码。

## cmd 包：spawn-child 子命令

```go
// cmd/uapclaw/cmd.go 中新增
func newSpawnChildCmd() *cobra.Command {
    return &cobra.Command{
        Use:    "spawn-child",
        Short:  "作为子进程运行 Agent（内部命令，不应直接调用）",
        Hidden: true,  // 对用户隐藏
        RunE:   runSpawnChild,
    }
}
```

子进程启动方式：`uap-claw spawn-child`，通过环境变量和 stdin 消息接收配置。

## 回填点清单

### 回填点 1：runner.go — SpawnAgent / SpawnAgentStreaming

**当前**：两个 stub 函数，参数全为 `any`，返回"依赖 6.28"错误。

**回填后**：

```go
func SpawnAgent(
    ctx context.Context,
    agentConfig spawn.SpawnAgentConfig,
    inputs map[string]any,
    sess sessioninterfaces.SessionFacade,
    envs map[string]any,
    spawnCfg ...spawn.SpawnConfig,
) (*spawn.SpawnedProcessHandle, error)

func SpawnAgentStreaming(
    ctx context.Context,
    agentConfig spawn.SpawnAgentConfig,
    inputs map[string]any,
    sess sessioninterfaces.SessionFacade,
    streamModes []string,
    envs map[string]any,
    spawnCfg ...spawn.SpawnConfig,
) (<-chan stream.Schema, error)
```

**关键变化**：
- 参数类型从 `any` 改为具体类型
- 返回值从 `any` 改为 `*spawn.SpawnedProcessHandle`
- 移除 `modelCtx any` 参数（配置已包含在 SpawnAgentConfig.RunnerConfig 中）
- `spawnConfig` 改为可变参数 `spawnCfg ...spawn.SpawnConfig`
- 移除所有 `⤵️` 标记

### 回填点 2：runner_test.go — Spawn 测试

两个"未实现"测试改为正式功能测试：
- `TestSpawnAgent_基本调用`：启动子进程，验证返回 handle
- `TestSpawnAgent_配置缺失时返回错误`：无效配置，验证返回错误
- `TestSpawnAgentStreaming_基本调用`：流式启动，验证 channel 收到 STREAM_CHUNK

### 回填点 3：cmd/uapclaw/cmd.go — spawn-child 子命令注册

在 `newRootCmd()` 的 `AddCommand` 中添加 `newSpawnChildCmd()`。

### 回填点 4：cmd 测试更新

更新子命令列表测试，加入 `spawn-child`。

### 回填点 5：runner/doc.go — 添加 spawn/ 子包条目

文件目录中添加 `spawn/` 子包及其下所有文件的描述。

### 回填点 6：IMPLEMENTATION_PLAN.md — 状态更新

6.28 状态 `☐` → `✅`。

## 依赖关系

### 内部依赖（已实现）

| 依赖 | 包路径 | 状态 |
|------|--------|------|
| BackgroundTask | `internal/common/utils` | ✅ 1.8 |
| ResourceMgr | `internal/agentcore/runner/resources_manager` | ✅ 6.23 |
| RunnerConfig | `internal/agentcore/runner/config` | ✅ 6.26 |
| Runner 单例 | `internal/agentcore/runner` | ✅ 6.25 |
| MessageQueue | `internal/agentcore/runner/message_queue` | ✅ 6.27 |
| SessionFacade | `internal/agentcore/session/interfaces` | ✅ |

### 外部依赖

| 依赖 | 用途 |
|------|------|
| `os/exec` | 创建子进程 |
| `encoding/json` | NDJSON 序列化/反序列化 |
| `bufio` | 行读取（Scanner） |
| `context` | 超时和取消 |
| `sync` | 互斥锁 |
| `syscall` | SIGTERM 信号（Unix） |
| `github.com/google/uuid` | UUID 生成 |
| `github.com/spf13/cobra` | spawn-child 子命令 |

### 不在本次范围的依赖

| 依赖 | 原因 |
|------|------|
| SpawnManager (9.58) | 属于领域九 Agent Teams，更高层的多子进程管理器 |
| TeamAgent.FromSpawnPayload | TEAM_AGENT 模式依赖 TeamAgent 实现，待 9.x 完成 |

## 测试策略

### 单元测试

- `protocol_test.go`：序列化/反序列化往返测试、非 JSON 行跳过测试
- `config_test.go`：配置解析测试、枚举值测试、RunnerConfig 序列化/反序列化往返测试
- `handle_test.go`：使用 `httptest` 或 mock 管道测试 SpawnedProcessHandle 的健康检查、关闭逻辑
- `child_test.go`：消息循环逻辑测试、消息处理器测试

### 集成测试（build tag: integration）

- `process_test.go`：真实启动子进程，验证 SpawnProcess → SendMessage → ReceiveMessage → Shutdown 全流程
- 需要 `//go:build integration` 标签，因为依赖真实子进程环境

### 子进程端测试

- 子进程入口通过集成测试覆盖（真实启动 `uap-claw spawn-child`）
- 消息循环和 Agent 执行通过 mock stdin/stdout 管道单元测试

## 日志同步规则

对齐 Python `spawn/` 中的所有 logger 调用，使用 `logger.ComponentAgentCore` 组件：

| Python 位置 | Go 位置 | 日志级别 | 关键字段 |
|-------------|---------|---------|---------|
| `spawn_process` 启动 | `SpawnProcess` | Info | process_id, command |
| `spawn_process` 成功 | `SpawnProcess` | Info | process_id, pid |
| `SpawnedProcessHandle.send_message` | `SendMessage` | Debug | message_type, process_id |
| `SpawnedProcessHandle.receive_message` | `ReceiveMessage` | Debug | message_type, process_id |
| `start_health_check` 启动 | `StartHealthCheck` | Info | process_id, interval |
| `stop_health_check` | `StopHealthCheck` | Info | process_id |
| `_perform_health_check` 通过 | `_performHealthCheck` | Debug | process_id |
| `_perform_health_check` 超时 | `_performHealthCheck` | Warn | process_id, timeout |
| `_perform_health_check` 失败 | `_performHealthCheck` | Error | process_id, err |
| `shutdown` 收到 ack | `Shutdown` | Info | process_id |
| `shutdown` 超时 | `Shutdown` | Warn | process_id, timeout |
| `force_kill` | `ForceKill` | Info | process_id |
| `child_process` 收到消息 | `readInputFromStdin` | Debug | message_type |
| `child_process` 发送消息 | `writeOutputToStdout` | Debug | message_type |
| `child_process` shutdown | `HandleShutdown` | Info | — |
| `_run_agent_task` 完成 | `runAgentTask` | Info | — |
| `_run_agent_task` 错误 | `runAgentTask` | Error | error, error_type |
| `process_message_loop` stdin 关闭 | `ProcessMessageLoop` | Info | — |
