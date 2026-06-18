# StreamWriter (5.10) 设计文档

## 概述

本设计文档描述 `session/stream` 包的实现方案，对应 Python 项目 `openjiuwen/core/session/stream/`。该包提供会话层流式数据写入与消费能力，是会话系统（领域5）的第 5.10 步。

### 核心职责

- 定义流模式枚举（Output/Trace/Custom）和数据 Schema 结构
- 提供带超时控制的流队列（StreamQueue）
- 提供流发射器（StreamEmitter），管理数据写入和关闭生命周期
- 提供三种流写入器（OutputStreamWriter/TraceStreamWriter/CustomStreamWriter）
- 提供流写入器管理器（StreamWriterManager），统一管理 Writer 集合和消费端

### 对应 Python 代码

`openjiuwen/core/session/stream/`

## 设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| 并发模型 | channel 原生模型 | Go 惯用，select + timeout 自然表达 |
| 泛型基类 | 不需要，三个具体 Writer | Go 泛型无法做运行时 Schema 校验，价值有限 |
| Schema 校验 | 强类型，Write 接受 Schema 接口 | 编译时安全，零运行时开销 |
| CustomSchema | Type + Data map[string]any | 三种 Schema 共享 Type 字段，Custom 保留动态性 |
| StreamWriter 接口 | 定义统一接口 + 三个实现 | 预留扩展点，Manager 可用 map[StreamMode]StreamWriter 统一存储 |
| StreamQueue | 封装，内置超时 | 首帧/后续帧超时是核心语义，不能推给调用方 |
| StreamEmitter | 保留 | 与 Python 对应，职责清晰（数据写入 + 关闭生命周期） |

## 核心类型体系

### 数据流

```
调用方构造Schema → Writer.Write(ctx, schema) → Emitter.Emit(schema) → Queue.Send(ctx, schema)
                                                                                       │
消费方 ← Manager.StreamOutput() <-chan any ← Queue.Ch() <-chan any ← Queue 内部 chan any
```

### 类型关系

```
StreamQueue          — 封装 chan any + Send/Receive/Close + 内置超时
StreamEmitter        — 持有 StreamQueue，负责 Emit/Close/END_FRAME 哨兵
Schema 接口          — SchemaType() string，三种 Schema 统一接口
  ├─ OutputSchema    — Type + Index + Payload
  ├─ TraceSchema     — Type + Payload
  └─ CustomSchema    — Type + Data map[string]any
StreamWriter 接口    — Write(ctx, Schema) error
  ├─ outputWriter    — 持有 *StreamEmitter
  ├─ traceWriter     — 持有 *StreamEmitter
  └─ customWriter    — 持有 *StreamEmitter
StreamMode 枚举      — Output / Trace / Custom
StreamWriterManager  — 持有 StreamEmitter + map[StreamMode]StreamWriter
                       提供 GetWriter/GetOutputWriter/GetTraceWriter/GetCustomWriter
                       提供 StreamOutput() <-chan any 消费端
```

## 文件组织

```
session/stream/
├── doc.go              # 包文档
├── base.go             # StreamMode 枚举 + Schema 接口 + OutputSchema/TraceSchema/CustomSchema 结构体
├── queue.go            # StreamQueue 封装（chan + Send/Receive/Close + 超时）
├── emitter.go          # StreamEmitter（持有 StreamQueue，Emit/Close/END_FRAME）
├── writer.go           # StreamWriter 接口 + outputWriter/traceWriter/customWriter 三个实现
├── manager.go          # StreamWriterManager（核心管理器）
├── base_test.go        # StreamMode + Schema 测试
├── queue_test.go       # StreamQueue 测试
├── emitter_test.go     # StreamEmitter 测试
├── writer_test.go      # StreamWriter 测试
└── manager_test.go     # StreamWriterManager 测试
```

与 Python 文件对应关系：

| Python 文件 | Go 文件 | 职责 |
|------------|---------|------|
| `base.py` | `base.go` | 枚举 + Schema 定义 |
| `emitter.py` (AsyncStreamQueue) | `queue.go` | 队列封装 |
| `emitter.py` (StreamEmitter) | `emitter.go` | 流发射器 |
| `writer.py` | `writer.go` | Writer 接口 + 三种实现 |
| `manager.py` | `manager.go` | 管理器 |

## 类型详细设计

### base.go

```go
// StreamMode 流模式枚举
type StreamMode int

const (
    // StreamModeOutput 标准输出流
    StreamModeOutput StreamMode = iota
    // StreamModeTrace 追踪流
    StreamModeTrace
    // StreamModeCustom 自定义流
    StreamModeCustom
)

// Schema 流数据统一接口
type Schema interface {
    // SchemaType 返回数据类型标识
    SchemaType() string
}

// OutputSchema 标准输出流数据
type OutputSchema struct {
    // Type 数据类型标识
    Type string
    // Index 序号索引
    Index int
    // Payload 实际数据载荷
    Payload any
}

// TraceSchema 追踪流数据
type TraceSchema struct {
    // Type 数据类型标识
    Type string
    // Payload 实际数据载荷
    Payload any
}

// CustomSchema 自定义流数据
type CustomSchema struct {
    // Type 数据类型标识
    Type string
    // Data 任意键值载荷
    Data map[string]any
}
```

### queue.go

```go
// StreamQueue 流队列，封装 channel + 超时控制
type StreamQueue struct { ... }

// NewStreamQueue 创建流队列，maxSize 为缓冲区大小
func NewStreamQueue(maxSize int) *StreamQueue

// Send 带上下文超时的发送
func (q *StreamQueue) Send(ctx context.Context, data any) error

// Receive 带上下文超时的接收
func (q *StreamQueue) Receive(ctx context.Context) (any, error)

// Close 优雅关闭，等待队列排空后关闭 channel
func (q *StreamQueue) Close(ctx context.Context) error

// Ch 返回只读 channel，供消费端 range 读取
func (q *StreamQueue) Ch() <-chan any
```

**超时语义：**
- `Send` 通过 `select { case q.ch <- data: ... case <-ctx.Done(): ... }` 实现超时
- `Receive` 通过 `select { case data := <-q.ch: ... case <-ctx.Done(): ... }` 实现超时
- `Close` 先发送 END_FRAME 哨兵，再等待队列排空（或 ctx 超时后强制清空）

**哨兵值：** 使用 `endFrame` 私有类型标记流结束，消费端收到后退出迭代。

### emitter.go

```go
// StreamEmitter 流发射器
type StreamEmitter struct { ... }

// NewStreamEmitter 创建流发射器
func NewStreamEmitter() *StreamEmitter

// Emit 写入数据到流队列，已关闭时返回错误
func (e *StreamEmitter) Emit(data Schema) error

// Close 关闭发射器，发送 END_FRAME 哨兵
func (e *StreamEmitter) Close()

// IsClosed 查询关闭状态
func (e *StreamEmitter) IsClosed() bool

// StreamQueue 返回内部队列，供 Manager 读取
func (e *StreamEmitter) StreamQueue() *StreamQueue
```

### writer.go

```go
// StreamWriter 流写入器接口
type StreamWriter interface {
    // Write 写入流数据
    Write(ctx context.Context, schema Schema) error
}

// outputWriter 标准输出流写入器
type outputWriter struct {
    emitter *StreamEmitter
}

// traceWriter 追踪流写入器
type traceWriter struct {
    emitter *StreamEmitter
}

// customWriter 自定义流写入器
type customWriter struct {
    emitter *StreamEmitter
}

// NewOutputStreamWriter 创建标准输出流写入器
func NewOutputStreamWriter(emitter *StreamEmitter) *outputWriter

// NewTraceStreamWriter 创建追踪流写入器
func NewTraceStreamWriter(emitter *StreamEmitter) *traceWriter

// NewCustomStreamWriter 创建自定义流写入器
func NewCustomStreamWriter(emitter *StreamEmitter) *customWriter
```

**Write 方法统一逻辑：**

1. Schema nil 检查 → 返回 `StatusStreamWriterWriteStreamValidationError`
2. Emitter 已关闭 → 记录 warn 日志，丢弃数据，返回 nil（与 Python 一致）
3. 调用 `emitter.Emit(schema)` → 失败返回 `StatusStreamWriterWriteStreamError`

三种 Writer 的 Write 逻辑相同，但保留独立结构体以预留扩展空间。

### manager.go

```go
// StreamWriterManager 流写入器管理器
type StreamWriterManager struct { ... }

// NewStreamWriterManager 创建流写入器管理器
func NewStreamWriterManager(emitter *StreamEmitter, modes ...StreamMode) *StreamWriterManager

// StreamEmitter 返回内部发射器
func (m *StreamWriterManager) StreamEmitter() *StreamEmitter

// AddWriter 添加自定义写入器
func (m *StreamWriterManager) AddWriter(key StreamMode, writer StreamWriter) error

// GetWriter 按模式获取写入器
func (m *StreamWriterManager) GetWriter(key StreamMode) StreamWriter

// GetOutputWriter 获取标准输出流写入器
func (m *StreamWriterManager) GetOutputWriter() StreamWriter

// GetTraceWriter 获取追踪流写入器
func (m *StreamWriterManager) GetTraceWriter() StreamWriter

// GetCustomWriter 获取自定义流写入器
func (m *StreamWriterManager) GetCustomWriter() StreamWriter

// RemoveWriter 移除写入器（不允许移除默认写入器）
func (m *StreamWriterManager) RemoveWriter(key StreamMode) error

// StreamOutput 返回流输出 channel，消费端通过 range 读取
func (m *StreamWriterManager) StreamOutput() <-chan any
```

**StreamOutput() 实现：**

1. 启动 goroutine 从 `emitter.StreamQueue().Ch()` 读取
2. 收到 `endFrame` 哨兵 → 关闭输出 channel，退出 goroutine
3. 收到正常 Schema 数据 → 写入输出 channel
4. 输出 channel 缓冲区大小与 StreamQueue 一致

## 错误处理

已预留错误码（`codes_session.go` 111130-111135）：

| 错误码 | 常量 | 触发场景 |
|--------|------|---------|
| 111130 | `StatusStreamWriterManagerAddWriterError` | 添加不支持的 StreamMode 的 Writer |
| 111131 | `StatusStreamWriterManagerRemoveWriterError` | 尝试移除默认 Writer |
| 111132 | `StatusStreamWriterWriteStreamValidationError` | Write 时 Schema 为 nil |
| 111133 | `StatusStreamWriterWriteStreamError` | Write 时 emitter 写入失败 |
| 111134 | `StatusStreamOutputFirstChunkIntervalTimeout` | 消费端首帧超时 |
| 111135 | `StatusStreamOutputChunkIntervalTimeout` | 消费端后续帧超时 |

## 日志对照

根据项目规则3，逐条对照 Python 日志：

| Python 位置 | Python 日志 | Go 映射 |
|------------|-----------|---------|
| `queue.send` 成功 | `logger.debug(event_type=SESSION_STREAM_CHUNK)` | `logger.Debug(ComponentAgentCore).Str("event_type", "SESSION_STREAM_CHUNK")` |
| `queue.send` 超时 | `logger.error(event_type=SESSION_STREAM_ERROR)` | `logger.Error(ComponentAgentCore).Str("event_type", "SESSION_STREAM_ERROR")` |
| `queue.send` 重试耗尽 | `logger.error(event_type=SESSION_STREAM_ERROR)` | `logger.Error(ComponentAgentCore).Str("event_type", "SESSION_STREAM_ERROR")` |
| `queue.receive` 成功 | `logger.debug(event_type=SESSION_STREAM_CHUNK)` | `logger.Debug(ComponentAgentCore).Str("event_type", "SESSION_STREAM_CHUNK")` |
| `queue._force_clear` | `logger.info(event_type=SESSION_STREAM_CHUNK)` | `logger.Info(ComponentAgentCore).Str("event_type", "SESSION_STREAM_CHUNK")` |
| `writer._do_write` 丢弃 | `logger.warning("Stream message discarded, emitter already closed")` | `logger.Warn(ComponentAgentCore).Msg("流消息已丢弃，emitter 已关闭")` |
| `manager.stream_output` 等待 | `logger.debug` | `logger.Debug(ComponentAgentCore)` |
| `manager.stream_output` 敏感模式 | `logger.debug(is_sensitive=True)` | `logger.Debug(ComponentAgentCore).Bool("is_sensitive", true)` |

## 回填清单

### 类型回填（any → stream.StreamWriterManager）

| 回填目标文件 | 当前占位 | 回填内容 |
|------------|---------|---------|
| `interfaces/interfaces.go` | `StreamWriterManager() any` | → `StreamWriterManager() stream.StreamWriterManager` |
| `session.go` (ProxySession) | `StreamWriterManager() any` | → `StreamWriterManager() stream.StreamWriterManager` |
| `internal/agent_session.go` 字段 | `streamWriterManager any` | → `streamWriterManager stream.StreamWriterManager` |
| `internal/agent_session.go` 默认创建 | 注释掉的代码 | → 取消注释 `stream.NewStreamWriterManager(stream.NewStreamEmitter())` |
| `internal/agent_session.go` 选项 | `WithStreamWriterManager(mgr any)` | → `WithStreamWriterManager(mgr stream.StreamWriterManager)` |
| `internal/workflow_session.go` 字段 | `streamWriterManager any` | → `streamWriterManager stream.StreamWriterManager` |
| `internal/workflow_session.go.Set` | `SetStreamWriterManager(mgr any)` | → `SetStreamWriterManager(mgr stream.StreamWriterManager)` |
| `internal/workflow_session.go` (NodeSession) | `StreamWriterManager() any` | → `StreamWriterManager() stream.StreamWriterManager` |
| `agent.go` 字段 | `streamWriterManagerOverride any` | → `streamWriterManagerOverride stream.StreamWriterManager` |
| `agent.go` 选项 | `WithStreamWriterManager(mgr any)` | → `WithStreamWriterManager(mgr stream.StreamWriterManager)` |

### 桩方法回填（return nil → 真实逻辑）

| 回填目标 | 方法 | 真实逻辑 |
|---------|------|---------|
| `agent.go` Session | `WriteStream(data)` | 构造 OutputSchema → GetOutputWriter().Write() |
| `agent.go` Session | `WriteCustomStream(data)` | 构造 CustomSchema → GetCustomWriter().Write() |
| `agent.go` Session | `StreamIterator()` | 返回 manager.StreamOutput() |
| `agent.go` Session | `CloseStream()` | 关闭 emitter + 注销回调 |
| `node.go` NodeSessionFacade | `WriteStream(ctx, data)` | 委托父 session 的 StreamWriterManager |
| `node.go` NodeSessionFacade | `WriteCustomStream(ctx, data)` | 委托父 session 的 StreamWriterManager |

### Interaction 接口迁移

| 迁移项 | 说明 |
|--------|------|
| `InteractionOutputWriterProvider` 接口 | 从 `interaction/base.go` 迁移到 `session/stream` 包 |
| `InteractionOutputWriter` 接口 | 从 `interaction/base.go` 迁移到 `session/stream` 包 |
| `writeInteractionOutput` 函数 | StreamWriterManager 天然满足 `InteractionOutputWriterProvider`，类型断言自动生效 |

### Code Review 额外回填项

| 编号 | 内容 | Python 位置 |
|------|------|------------|
| R1 | `_normalize_output_stream` 方法 | `agent.py:164-169` |
| R2 | `_tag_stream_payload` 对 OutputSchema 的处理 | `agent.py:152-160` |
| R6 | `close_stream` 中的回调注销 `unregister_event` | `agent.py:123` |
