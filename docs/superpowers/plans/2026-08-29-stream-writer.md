# StreamWriter (5.10) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 session/stream 包，提供会话层流式数据写入与消费能力，并回填之前步骤中所有 StreamWriterManager 的 `any` 占位和桩方法。

**Architecture:** StreamQueue 封装 buffered channel + 超时控制（Send/Receive/Close），StreamEmitter 持有 Queue 负责写入和生命周期管理（END_FRAME 哨兵），三种 Writer 接受强类型 Schema 通过 Emitter 写入，StreamWriterManager 统一管理 Writer 集合并提供消费端 channel。

**Tech Stack:** Go 标准库（context, sync, time），项目内 logger/exception 包

**设计文档:** `docs/superpowers/specs/2026-08-29-stream-writer-design.md`

---

## 文件结构

### 新建文件

| 文件 | 职责 |
|------|------|
| `internal/agentcore/session/stream/doc.go` | 包文档 |
| `internal/agentcore/session/stream/base.go` | StreamMode 枚举 + Schema 接口 + 三种 Schema 结构体 |
| `internal/agentcore/session/stream/queue.go` | StreamQueue（chan + Send/Receive/Close + 超时） |
| `internal/agentcore/session/stream/emitter.go` | StreamEmitter（Emit/Close/END_FRAME） |
| `internal/agentcore/session/stream/writer.go` | StreamWriter 接口 + outputWriter/traceWriter/customWriter |
| `internal/agentcore/session/stream/manager.go` | StreamWriterManager |
| `internal/agentcore/session/stream/base_test.go` | StreamMode + Schema 测试 |
| `internal/agentcore/session/stream/queue_test.go` | StreamQueue 测试 |
| `internal/agentcore/session/stream/emitter_test.go` | StreamEmitter 测试 |
| `internal/agentcore/session/stream/writer_test.go` | StreamWriter 测试 |
| `internal/agentcore/session/stream/manager_test.go` | StreamWriterManager 测试 |

### 修改文件（回填）

| 文件 | 回填内容 |
|------|---------|
| `internal/agentcore/session/interfaces/interfaces.go:39-40` | `StreamWriterManager() any` → `StreamWriterManager() stream.StreamWriterManager` |
| `internal/agentcore/session/session.go:66` | `StreamWriterManager() any` → `StreamWriterManager() stream.StreamWriterManager` |
| `internal/agentcore/session/internal/agent_session.go:31` | 字段 `streamWriterManager any` → `stream.StreamWriterManager` |
| `internal/agentcore/session/internal/agent_session.go:84-87` | 取消注释默认实例创建 |
| `internal/agentcore/session/internal/agent_session.go:129` | `WithStreamWriterManager(mgr any)` → `WithStreamWriterManager(mgr stream.StreamWriterManager)` |
| `internal/agentcore/session/internal/agent_session.go:172` | 返回类型 `any` → `stream.StreamWriterManager` |
| `internal/agentcore/session/internal/workflow_session.go:36` | 字段 `streamWriterManager any` → `stream.StreamWriterManager` |
| `internal/agentcore/session/internal/workflow_session.go:282` | 返回类型 `any` → `stream.StreamWriterManager` |
| `internal/agentcore/session/internal/workflow_session.go:313` | `SetStreamWriterManager(mgr any)` → `SetStreamWriterManager(mgr stream.StreamWriterManager)` |
| `internal/agentcore/session/internal/workflow_session.go:423` | 返回类型 `any` → `stream.StreamWriterManager` |
| `internal/agentcore/session/agent.go:40` | 字段 `streamWriterManagerOverride any` → `stream.StreamWriterManager` |
| `internal/agentcore/session/agent.go:157` | `WithStreamWriterManager(mgr any)` → `WithStreamWriterManager(mgr stream.StreamWriterManager)` |
| `internal/agentcore/session/agent.go:259-281` | 4 个桩方法填充真实逻辑 |
| `internal/agentcore/session/node.go:176-186` | 2 个桩方法填充真实逻辑 |
| `internal/agentcore/session/interaction/base.go:19-27` | 迁移 InteractionOutputWriterProvider/InteractionOutputWriter 到 stream 包 |
| `IMPLEMENTATION_PLAN.md` | 5.10 状态 ☐ → ✅ |

---

## Task 1: base.go — StreamMode 枚举 + Schema 类型

**Files:**
- Create: `internal/agentcore/session/stream/base.go`
- Create: `internal/agentcore/session/stream/doc.go`
- Test: `internal/agentcore/session/stream/base_test.go`

- [ ] **Step 1: 写 doc.go**

```go
// Package stream 提供会话层流式数据写入与消费能力。
//
// 本包定义了流模式枚举（Output/Trace/Custom）、数据 Schema 结构、
// 流队列（StreamQueue）、流发射器（StreamEmitter）、流写入器（StreamWriter）
// 和流写入器管理器（StreamWriterManager）。
//
// 数据流：
//
//	调用方构造 Schema → Writer.Write(ctx, schema) → Emitter.Emit(schema) → Queue.Send(ctx, schema)
//	                                                                                       │
//	消费方 ← Manager.StreamOutput() <-chan any ← Queue.Ch() <-chan any ← Queue 内部 chan any
//
// 文件目录：
//
//	stream/
//	├── doc.go              # 包文档
//	├── base.go             # StreamMode 枚举 + Schema 接口 + 三种 Schema 结构体
//	├── queue.go            # StreamQueue 封装（chan + Send/Receive/Close + 超时）
//	├── emitter.go          # StreamEmitter（持有 StreamQueue，Emit/Close/END_FRAME）
//	├── writer.go           # StreamWriter 接口 + outputWriter/traceWriter/customWriter
//	└── manager.go          # StreamWriterManager（核心管理器）
//
// 对应 Python 代码：openjiuwen/core/session/stream/
package stream
```

- [ ] **Step 2: 写 base.go**

```go
package stream

// ──────────────────────────── 结构体 ────────────────────────────

// Schema 流数据统一接口，三种 Schema 均实现此接口。
type Schema interface {
	// SchemaType 返回数据类型标识
	SchemaType() string
}

// OutputSchema 标准输出流数据，对应 Python OutputSchema。
// 框架标准流数据，有 Index 字段用于排序。
type OutputSchema struct {
	// Type 数据类型标识
	Type string
	// Index 序号索引
	Index int
	// Payload 实际数据载荷
	Payload any
}

// SchemaType 实现 Schema 接口
func (s OutputSchema) SchemaType() string { return s.Type }

// TraceSchema 追踪流数据，对应 Python TraceSchema。
// 图执行产生的追踪数据，无 Index 字段。
type TraceSchema struct {
	// Type 数据类型标识
	Type string
	// Payload 实际数据载荷
	Payload any
}

// SchemaType 实现 Schema 接口
func (s TraceSchema) SchemaType() string { return s.Type }

// CustomSchema 自定义流数据，对应 Python CustomSchema。
// 用户自定义流数据，Data 字段允许任意键值对。
type CustomSchema struct {
	// Type 数据类型标识
	Type string
	// Data 任意键值载荷
	Data map[string]any
}

// SchemaType 实现 Schema 接口
func (s CustomSchema) SchemaType() string { return s.Type }

// ──────────────────────────── 枚举 ────────────────────────────

// StreamMode 流模式枚举，对应 Python BaseStreamMode。
type StreamMode int

const (
	// StreamModeOutput 标准输出流
	StreamModeOutput StreamMode = iota
	// StreamModeTrace 追踪流
	StreamModeTrace
	// StreamModeCustom 自定义流
	StreamModeCustom
)

// String 返回流模式的字符串表示
func (m StreamMode) String() string {
	switch m {
	case StreamModeOutput:
		return "output"
	case StreamModeTrace:
		return "trace"
	case StreamModeCustom:
		return "custom"
	default:
		return "unknown"
	}
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 3: 写 base_test.go**

```go
package stream

import "testing"

// TestStreamMode_String 测试 StreamMode 的字符串表示
func TestStreamMode_String(t *testing.T) {
	tests := []struct {
		mode     StreamMode
		expected string
	}{
		{StreamModeOutput, "output"},
		{StreamModeTrace, "trace"},
		{StreamModeCustom, "custom"},
		{StreamMode(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.expected {
			t.Errorf("StreamMode(%d).String() = %q, want %q", tt.mode, got, tt.expected)
		}
	}
}

// TestOutputSchema_SchemaType 测试 OutputSchema 实现 Schema 接口
func TestOutputSchema_SchemaType(t *testing.T) {
	s := OutputSchema{Type: "message", Index: 0, Payload: "hello"}
	if s.SchemaType() != "message" {
		t.Errorf("SchemaType() = %q, want %q", s.SchemaType(), "message")
	}
}

// TestTraceSchema_SchemaType 测试 TraceSchema 实现 Schema 接口
func TestTraceSchema_SchemaType(t *testing.T) {
	s := TraceSchema{Type: "step", Payload: "data"}
	if s.SchemaType() != "step" {
		t.Errorf("SchemaType() = %q, want %q", s.SchemaType(), "step")
	}
}

// TestCustomSchema_SchemaType 测试 CustomSchema 实现 Schema 接口
func TestCustomSchema_SchemaType(t *testing.T) {
	s := CustomSchema{Type: "my_event", Data: map[string]any{"key": "val"}}
	if s.SchemaType() != "my_event" {
		t.Errorf("SchemaType() = %q, want %q", s.SchemaType(), "my_event")
	}
}

// TestSchema 接口多态测试
func TestSchema(t *testing.T) {
	var schemas []Schema = []Schema{
		OutputSchema{Type: "message", Index: 0, Payload: "hello"},
		TraceSchema{Type: "step", Payload: "data"},
		CustomSchema{Type: "event", Data: map[string]any{"k": "v"}},
	}
	expected := []string{"message", "step", "event"}
	for i, s := range schemas {
		if s.SchemaType() != expected[i] {
			t.Errorf("schemas[%d].SchemaType() = %q, want %q", i, s.SchemaType(), expected[i])
		}
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/stream/... -v -run "TestStreamMode|TestOutputSchema|TestTraceSchema|TestCustomSchema|TestSchema$" -count=1`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/session/stream/
git commit -m "feat(stream): 添加 StreamMode 枚举和 Schema 类型定义 (5.10 Task 1)"
```

---

## Task 2: queue.go — StreamQueue 封装

**Files:**
- Create: `internal/agentcore/session/stream/queue.go`
- Test: `internal/agentcore/session/stream/queue_test.go`

- [ ] **Step 1: 写 queue.go**

```go
package stream

import (
	"context"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// endFrame 流结束哨兵类型，消费端收到后退出迭代。
// 对应 Python: StreamEmitter.END_FRAME = "all streaming outputs finish"
type endFrame struct{}

// StreamQueue 流队列，封装 buffered channel + 超时控制。
// 对应 Python: AsyncStreamQueue
type StreamQueue struct {
	// ch 内部缓冲 channel
	ch chan any
	// mu 保护 closed 字段
	mu sync.RWMutex
	// closed 队列是否已关闭
	closed bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultSendAttemptTimeout 每次发送尝试超时，对应 Python DEFAULT_SEND_ATTEMPT_TIMEOUT = 0.2
	defaultSendAttemptTimeout = 200 * time.Millisecond
	// defaultMaxSendRetries 最大发送重试次数，对应 Python DEFAULT_MAX_SEND_RETRIES = 5
	defaultMaxSendRetries = 5
	// defaultCloseTimeout 关闭超时，对应 Python DEFAULT_CLOSE_TIMEOUT = 5.0
	defaultCloseTimeout = 5 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamQueue 创建流队列，maxSize 为缓冲区大小（0 为无缓冲）。
// 对应 Python: AsyncStreamQueue(maxsize=0)
func NewStreamQueue(maxSize int) *StreamQueue {
	return &StreamQueue{
		ch: make(chan any, maxSize),
	}
}

// Send 带超时的发送，对齐 Python AsyncStreamQueue.send()。
// 通过 select + ctx.Done() 实现超时，失败时重试 maxRetries 次。
func (q *StreamQueue) Send(ctx context.Context, data any, attemptTimeout ...time.Duration) error {
	timeout := defaultSendAttemptTimeout
	if len(attemptTimeout) > 0 {
		timeout = attemptTimeout[0]
	}
	maxRetries := defaultMaxSendRetries

	q.mu.RLock()
	isClosed := q.closed
	q.mu.RUnlock()
	if isClosed {
		logger.Error(logComponent).
			Str("event_type", "SESSION_STREAM_ERROR").
			Msg("StreamQueue 已关闭，无法发送数据")
		return ErrQueueClosed
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		sendCtx, cancel := context.WithTimeout(ctx, timeout)
		select {
		case q.ch <- data:
			cancel()
			logger.Debug(logComponent).
				Str("event_type", "SESSION_STREAM_CHUNK").
				Dur("timeout", timeout).
				Int("attempt", attempt).
				Msg("流数据发送成功")
			return nil
		case <-sendCtx.Done():
			cancel()
			logger.Error(logComponent).
				Str("event_type", "SESSION_STREAM_ERROR").
				Dur("timeout", timeout).
				Int("attempt", attempt).
				Msg("流数据发送超时")
			continue
		}
	}

	logger.Error(logComponent).
		Str("event_type", "SESSION_STREAM_ERROR").
		Int("max_retries", maxRetries).
		Dur("timeout", timeout).
		Msg("流数据发送重试耗尽")
	return ErrQueueSendRetryExhausted
}

// Receive 带超时的接收，对齐 Python AsyncStreamQueue.receive()。
// timeout <= 0 表示无限等待。
func (q *StreamQueue) Receive(ctx context.Context, timeout ...time.Duration) (any, error) {
	q.mu.RLock()
	isClosed := q.closed
	q.mu.RUnlock()
	if isClosed {
		return nil, ErrQueueClosed
	}

	var recvCtx context.Context
	var cancel context.CancelFunc
	if len(timeout) > 0 && timeout[0] > 0 {
		recvCtx, cancel = context.WithTimeout(ctx, timeout[0])
	} else {
		recvCtx, cancel = context.WithCancel(ctx)
	}
	defer cancel()

	select {
	case data, ok := <-q.ch:
		if !ok {
			return nil, ErrQueueClosed
		}
		logger.Debug(logComponent).
			Str("event_type", "SESSION_STREAM_CHUNK").
			Msg("流数据接收成功")
		return data, nil
	case <-recvCtx.Done():
		return nil, recvCtx.Err()
	}
}

// Close 优雅关闭队列，对齐 Python AsyncStreamQueue.close()。
// 发送哨兵值后等待队列排空（或超时后强制清空）。
func (q *StreamQueue) Close(ctx context.Context, timeout ...time.Duration) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return nil
	}
	q.closed = true
	q.mu.Unlock()

	closeTimeout := defaultCloseTimeout
	if len(timeout) > 0 {
		closeTimeout = timeout[0]
	}

	// 发送结束哨兵
	select {
	case q.ch <- endFrame{}:
	default:
		// channel 已满或已关闭，尝试强制发送
		logger.Warn(logComponent).
			Msg("StreamQueue 关闭时无法发送 endFrame，channel 可能已满")
	}

	// 等待队列排空或超时
	closeCtx, cancel := context.WithTimeout(ctx, closeTimeout)
	defer cancel()

	// 启动 goroutine 消费剩余数据以排空队列
	drained := make(chan struct{})
	go func() {
		defer close(drained)
		for {
			select {
			case _, ok := <-q.ch:
				if !ok {
					return
				}
			default:
				return
			}
		}
	}()

	select {
	case <-drained:
		close(q.ch)
		return nil
	case <-closeCtx.Done():
		logger.Error(logComponent).
			Str("event_type", "SESSION_STREAM_ERROR").
			Dur("timeout", closeTimeout).
			Msg("StreamQueue 关闭超时，强制清空")
		q.forceClear()
		return nil
	}
}

// Ch 返回只读 channel，供消费端 range 读取。
func (q *StreamQueue) Ch() <-chan any {
	return q.ch
}

// IsClosed 查询关闭状态
func (q *StreamQueue) IsClosed() bool {
	q.mu.RLock()
	defer q.mu.RUnlock()
	return q.closed
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// forceClear 强制清空队列，对齐 Python AsyncStreamQueue._force_clear()
func (q *StreamQueue) forceClear() {
	clearedItems := 0
	for {
		select {
		case <-q.ch:
			clearedItems++
		default:
			close(q.ch)
			logger.Info(logComponent).
				Str("event_type", "SESSION_STREAM_CHUNK").
				Int("cleared_items", clearedItems).
				Msg("StreamQueue 强制清空完成")
			return
		}
	}
}
```

- [ ] **Step 2: 写 queue 错误变量（添加到 queue.go 或单独 errors.go）**

在 `queue.go` 底部添加错误变量：

```go
// 错误定义
var (
	// ErrQueueClosed 队列已关闭
	ErrQueueClosed = errors.New("stream queue is closed")
	// ErrQueueSendRetryExhausted 发送重试耗尽
	ErrQueueSendRetryExhausted = errors.New("stream queue send retry exhausted")
)
```

注意：需要在文件顶部 import 中添加 `"errors"`。

- [ ] **Step 3: 写 queue_test.go**

```go
package stream

import (
	"context"
	"testing"
	"time"
)

// TestNewStreamQueue 测试创建流队列
func TestNewStreamQueue(t *testing.T) {
	q := NewStreamQueue(10)
	if q == nil {
		t.Fatal("NewStreamQueue 返回 nil")
	}
	if q.IsClosed() {
		t.Error("新创建的队列不应为关闭状态")
	}
}

// TestStreamQueue_SendReceive 测试基本发送和接收
func TestStreamQueue_SendReceive(t *testing.T) {
	q := NewStreamQueue(10)
	ctx := context.Background()

	// 发送
	if err := q.Send(ctx, "hello"); err != nil {
		t.Fatalf("Send 失败: %v", err)
	}

	// 接收
	data, err := q.Receive(ctx)
	if err != nil {
		t.Fatalf("Receive 失败: %v", err)
	}
	if data != "hello" {
		t.Errorf("Receive 数据 = %v, want %q", data, "hello")
	}
}

// TestStreamQueue_SendAfterClose 测试关闭后发送返回错误
func TestStreamQueue_SendAfterClose(t *testing.T) {
	q := NewStreamQueue(10)
	ctx := context.Background()

	if err := q.Close(ctx); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	if err := q.Send(ctx, "data"); err == nil {
		t.Error("关闭后 Send 应返回错误")
	}
}

// TestStreamQueue_ReceiveWithTimeout 测试带超时的接收
func TestStreamQueue_ReceiveWithTimeout(t *testing.T) {
	q := NewStreamQueue(0) // 无缓冲
	ctx := context.Background()

	// 无数据时超时
	_, err := q.Receive(ctx, 100*time.Millisecond)
	if err == nil {
		t.Error("无数据时 Receive 应超时返回错误")
	}
}

// TestStreamQueue_CloseTimeout 测试关闭超时后强制清空
func TestStreamQueue_CloseTimeout(t *testing.T) {
	q := NewStreamQueue(10)
	ctx := context.Background()

	// 填充数据不消费
	for i := 0; i < 10; i++ {
		q.Send(ctx, i)
	}

	// 用极短超时关闭，触发强制清空
	if err := q.Close(ctx, 1*time.Nanosecond); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if !q.IsClosed() {
		t.Error("Close 后应为关闭状态")
	}
}

// TestStreamQueue_Ch 测试只读 channel
func TestStreamQueue_Ch(t *testing.T) {
	q := NewStreamQueue(10)
	ctx := context.Background()

	q.Send(ctx, "data1")
	q.Send(ctx, endFrame{})

	ch := q.Ch()
	data := <-ch
	if data != "data1" {
		t.Errorf("Ch 接收数据 = %v, want %q", data, "data1")
	}
	frame := <-ch
	if _, ok := frame.(endFrame); !ok {
		t.Error("应收到 endFrame 哨兵")
	}
}
```

- [ ] **Step 4: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/stream/... -v -run "TestNewStreamQueue|TestStreamQueue" -count=1`

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/session/stream/
git commit -m "feat(stream): 添加 StreamQueue 封装 (5.10 Task 2)"
```

---

## Task 3: emitter.go — StreamEmitter

**Files:**
- Create: `internal/agentcore/session/stream/emitter.go`
- Test: `internal/agentcore/session/stream/emitter_test.go`

- [ ] **Step 1: 写 emitter.go**

```go
package stream

import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamEmitter 流发射器，持有 StreamQueue，负责数据写入和生命周期管理。
// 对应 Python: StreamEmitter
type StreamEmitter struct {
	// queue 内部流队列
	queue *StreamQueue
	// mu 保护 closed 字段
	mu sync.RWMutex
	// closed 是否已关闭
	closed bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamEmitter 创建流发射器。
// 对应 Python: StreamEmitter()
func NewStreamEmitter() *StreamEmitter {
	return &StreamEmitter{
		queue: NewStreamQueue(0),
	}
}

// Emit 写入数据到流队列。
// 已关闭时返回错误，对齐 Python: raise RuntimeError("Can not emit data after the stream emitter is closed.")
func (e *StreamEmitter) Emit(ctx context.Context, data Schema) error {
	e.mu.RLock()
	isClosed := e.closed
	e.mu.RUnlock()

	if isClosed {
		return exception.NewError(exception.StatusStreamWriterWriteStreamError).
			Str("reason", "emitter 已关闭，无法写入数据").
			Err(nil)
	}

	return e.queue.Send(ctx, data)
}

// Close 关闭发射器，发送 endFrame 哨兵。
// 对应 Python: StreamEmitter.close()
func (e *StreamEmitter) Close(ctx context.Context) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()

	// 发送哨兵到队列
	if err := e.queue.Send(ctx, endFrame{}); err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("StreamEmitter 关闭时发送 endFrame 失败")
	}
	return nil
}

// IsClosed 查询关闭状态
func (e *StreamEmitter) IsClosed() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.closed
}

// StreamQueue 返回内部队列，供 Manager 读取。
// 对应 Python: StreamEmitter.stream_queue
func (e *StreamEmitter) StreamQueue() *StreamQueue {
	return e.queue
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 写 emitter_test.go**

```go
package stream

import (
	"context"
	"testing"
)

// TestNewStreamEmitter 测试创建流发射器
func TestNewStreamEmitter(t *testing.T) {
	e := NewStreamEmitter()
	if e == nil {
		t.Fatal("NewStreamEmitter 返回 nil")
	}
	if e.IsClosed() {
		t.Error("新创建的 emitter 不应为关闭状态")
	}
}

// TestStreamEmitter_Emit 测试正常写入数据
func TestStreamEmitter_Emit(t *testing.T) {
	e := NewStreamEmitter()
	ctx := context.Background()

	schema := OutputSchema{Type: "message", Index: 0, Payload: "hello"}
	if err := e.Emit(ctx, schema); err != nil {
		t.Fatalf("Emit 失败: %v", err)
	}

	// 验证数据在队列中
	data, err := e.StreamQueue().Receive(ctx)
	if err != nil {
		t.Fatalf("Receive 失败: %v", err)
	}
	if out, ok := data.(OutputSchema); !ok || out.Type != "message" {
		t.Errorf("Emit 数据 = %v, want OutputSchema{Type:message}", data)
	}
}

// TestStreamEmitter_EmitAfterClose 测试关闭后 Emit 返回错误
func TestStreamEmitter_EmitAfterClose(t *testing.T) {
	e := NewStreamEmitter()
	ctx := context.Background()

	if err := e.Close(ctx); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	schema := OutputSchema{Type: "message", Index: 0, Payload: "hello"}
	if err := e.Emit(ctx, schema); err == nil {
		t.Error("关闭后 Emit 应返回错误")
	}
}

// TestStreamEmitter_Close 测试关闭发射器
func TestStreamEmitter_Close(t *testing.T) {
	e := NewStreamEmitter()
	ctx := context.Background()

	if err := e.Close(ctx); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if !e.IsClosed() {
		t.Error("Close 后应为关闭状态")
	}
}

// TestStreamEmitter_CloseIdempotent 测试重复关闭是幂等的
func TestStreamEmitter_CloseIdempotent(t *testing.T) {
	e := NewStreamEmitter()
	ctx := context.Background()

	if err := e.Close(ctx); err != nil {
		t.Fatalf("第一次 Close 失败: %v", err)
	}
	if err := e.Close(ctx); err != nil {
		t.Fatalf("第二次 Close 失败: %v", err)
	}
}

// TestStreamEmitter_CloseSendsEndFrame 测试关闭时发送 endFrame
func TestStreamEmitter_CloseSendsEndFrame(t *testing.T) {
	e := NewStreamEmitter()
	ctx := context.Background()

	if err := e.Close(ctx); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// 队列中应有 endFrame
	data, err := e.StreamQueue().Receive(ctx)
	if err != nil {
		t.Fatalf("Receive 失败: %v", err)
	}
	if _, ok := data.(endFrame); !ok {
		t.Error("Close 后队列中应有 endFrame 哨兵")
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/stream/... -v -run "TestNewStreamEmitter|TestStreamEmitter" -count=1`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/stream/
git commit -m "feat(stream): 添加 StreamEmitter 流发射器 (5.10 Task 3)"
```

---

## Task 4: writer.go — StreamWriter 接口 + 三种实现

**Files:**
- Create: `internal/agentcore/session/stream/writer.go`
- Test: `internal/agentcore/session/stream/writer_test.go`

- [ ] **Step 1: 写 writer.go**

```go
package stream

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// outputWriter 标准输出流写入器，对应 Python OutputStreamWriter。
type outputWriter struct {
	// emitter 流发射器
	emitter *StreamEmitter
}

// traceWriter 追踪流写入器，对应 Python TraceStreamWriter。
type traceWriter struct {
	// emitter 流发射器
	emitter *StreamEmitter
}

// customWriter 自定义流写入器，对应 Python CustomStreamWriter。
type customWriter struct {
	// emitter 流发射器
	emitter *StreamEmitter
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewOutputStreamWriter 创建标准输出流写入器
func NewOutputStreamWriter(emitter *StreamEmitter) *outputWriter {
	return &outputWriter{emitter: emitter}
}

// NewTraceStreamWriter 创建追踪流写入器
func NewTraceStreamWriter(emitter *StreamEmitter) *traceWriter {
	return &traceWriter{emitter: emitter}
}

// NewCustomStreamWriter 创建自定义流写入器
func NewCustomStreamWriter(emitter *StreamEmitter) *customWriter {
	return &customWriter{emitter: emitter}
}

// Write 写入标准输出流数据。
// Schema 为 nil 时返回校验错误；emitter 已关闭时丢弃数据并返回 nil。
func (w *outputWriter) Write(ctx context.Context, schema Schema) error {
	return writeStream(w.emitter, ctx, schema)
}

// Write 写入追踪流数据
func (w *traceWriter) Write(ctx context.Context, schema Schema) error {
	return writeStream(w.emitter, ctx, schema)
}

// Write 写入自定义流数据
func (w *customWriter) Write(ctx context.Context, schema Schema) error {
	return writeStream(w.emitter, ctx, schema)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// writeStream 统一的写入逻辑，对齐 Python StreamWriter.write()。
// 1. nil 校验 → STREAM_WRITER_WRITE_STREAM_VALIDATION_ERROR
// 2. emitter 已关闭 → warn 日志 + 丢弃数据（与 Python 一致，返回 nil）
// 3. 正常写入 → emitter.Emit()
func writeStream(emitter *StreamEmitter, ctx context.Context, schema Schema) error {
	if schema == nil {
		return exception.NewError(exception.StatusStreamWriterWriteStreamValidationError).
			Str("reason", "stream data is nil")
	}

	if emitter.IsClosed() {
		logger.Warn(logComponent).
			Str("event_type", "SESSION_STREAM_CHUNK").
			Str("data_type", schema.SchemaType()).
			Msg("流消息已丢弃，emitter 已关闭")
		return nil
	}

	if err := emitter.Emit(ctx, schema); err != nil {
		return exception.NewError(exception.StatusStreamWriterWriteStreamError).
			Err(err).
			Str("data_type", schema.SchemaType())
	}
	return nil
}
```

- [ ] **Step 2: 写 writer_test.go**

```go
package stream

import (
	"context"
	"testing"
)

// TestOutputWriter_Write 测试正常写入
func TestOutputWriter_Write(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewOutputStreamWriter(emitter)

	schema := OutputSchema{Type: "message", Index: 0, Payload: "hello"}
	if err := w.Write(ctx, schema); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	data, err := emitter.StreamQueue().Receive(ctx)
	if err != nil {
		t.Fatalf("Receive 失败: %v", err)
	}
	if out, ok := data.(OutputSchema); !ok || out.Type != "message" {
		t.Errorf("写入数据 = %v, want OutputSchema{Type:message}", data)
	}
}

// TestOutputWriter_WriteNil 测试写入 nil 返回校验错误
func TestOutputWriter_WriteNil(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewOutputStreamWriter(emitter)

	if err := w.Write(ctx, nil); err == nil {
		t.Error("写入 nil 应返回校验错误")
	}
}

// TestOutputWriter_WriteAfterClose 测试关闭后写入丢弃数据
func TestOutputWriter_WriteAfterClose(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewOutputStreamWriter(emitter)

	emitter.Close(ctx)

	schema := OutputSchema{Type: "message", Index: 0, Payload: "hello"}
	if err := w.Write(ctx, schema); err != nil {
		t.Fatalf("关闭后 Write 不应返回错误（丢弃数据）: %v", err)
	}
}

// TestTraceWriter_Write 测试 TraceWriter 正常写入
func TestTraceWriter_Write(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewTraceStreamWriter(emitter)

	schema := TraceSchema{Type: "step", Payload: "data"}
	if err := w.Write(ctx, schema); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	data, err := emitter.StreamQueue().Receive(ctx)
	if err != nil {
		t.Fatalf("Receive 失败: %v", err)
	}
	if out, ok := data.(TraceSchema); !ok || out.Type != "step" {
		t.Errorf("写入数据 = %v, want TraceSchema{Type:step}", data)
	}
}

// TestCustomWriter_Write 测试 CustomWriter 正常写入
func TestCustomWriter_Write(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewCustomStreamWriter(emitter)

	schema := CustomSchema{Type: "event", Data: map[string]any{"key": "val"}}
	if err := w.Write(ctx, schema); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	data, err := emitter.StreamQueue().Receive(ctx)
	if err != nil {
		t.Fatalf("Receive 失败: %v", err)
	}
	if out, ok := data.(CustomSchema); !ok || out.Type != "event" {
		t.Errorf("写入数据 = %v, want CustomSchema{Type:event}", data)
	}
}
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/stream/... -v -run "TestOutputWriter|TestTraceWriter|TestCustomWriter" -count=1`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/stream/
git commit -m "feat(stream): 添加 StreamWriter 接口和三种实现 (5.10 Task 4)"
```

---

## Task 5: manager.go — StreamWriterManager

**Files:**
- Create: `internal/agentcore/session/stream/manager.go`
- Test: `internal/agentcore/session/stream/manager_test.go`

- [ ] **Step 1: 写 manager.go**

```go
package stream

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamWriterManager 流写入器管理器，对应 Python StreamWriterManager。
// 持有 StreamEmitter 和 map[StreamMode]StreamWriter，统一管理 Writer 集合和消费端。
type StreamWriterManager struct {
	// emitter 流发射器
	emitter *StreamEmitter
	// writers 流写入器集合
	writers map[StreamMode]StreamWriter
	// defaultModes 默认模式列表（不允许移除默认 Writer）
	defaultModes []StreamMode
}

// StreamWriter 流写入器接口
type StreamWriter interface {
	// Write 写入流数据
	Write(ctx context.Context, schema Schema) error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamWriterManager 创建流写入器管理器。
// modes 为空时默认注册 Output/Trace/Custom 三种 Writer。
// 对应 Python: StreamWriterManager(stream_emitter, modes)
func NewStreamWriterManager(emitter *StreamEmitter, modes ...StreamMode) *StreamWriterManager {
	if emitter == nil {
		panic("stream_emitter 不能为 nil")
	}

	defaultModes := modes
	if len(defaultModes) == 0 {
		defaultModes = []StreamMode{StreamModeOutput, StreamModeTrace, StreamModeCustom}
	}

	mgr := &StreamWriterManager{
		emitter:      emitter,
		writers:      make(map[StreamMode]StreamWriter),
		defaultModes: defaultModes,
	}
	mgr.addDefaultWriters()
	return mgr
}

// StreamEmitter 返回内部发射器
// 对应 Python: StreamWriterManager.stream_emitter()
func (m *StreamWriterManager) StreamEmitter() *StreamEmitter {
	return m.emitter
}

// AddWriter 添加自定义写入器。
// 对应 Python: StreamWriterManager.add_writer(key, writer)
func (m *StreamWriterManager) AddWriter(key StreamMode, writer StreamWriter) error {
	if writer == nil {
		return exception.NewError(exception.StatusStreamWriterManagerAddWriterError).
			Str("mode", key.String()).
			Str("reason", "writer 不能为 nil")
	}
	m.writers[key] = writer
	return nil
}

// GetWriter 按模式获取写入器。
// 对应 Python: StreamWriterManager.get_writer(key)
func (m *StreamWriterManager) GetWriter(key StreamMode) StreamWriter {
	return m.writers[key]
}

// GetOutputWriter 获取标准输出流写入器
// 对应 Python: StreamWriterManager.get_output_writer()
func (m *StreamWriterManager) GetOutputWriter() StreamWriter {
	return m.GetWriter(StreamModeOutput)
}

// GetTraceWriter 获取追踪流写入器
// 对应 Python: StreamWriterManager.get_trace_writer()
func (m *StreamWriterManager) GetTraceWriter() StreamWriter {
	return m.GetWriter(StreamModeTrace)
}

// GetCustomWriter 获取自定义流写入器
// 对应 Python: StreamWriterManager.get_custom_writer()
func (m *StreamWriterManager) GetCustomWriter() StreamWriter {
	return m.GetWriter(StreamModeCustom)
}

// RemoveWriter 移除写入器，不允许移除默认 Writer。
// 对应 Python: StreamWriterManager.remove_writer(key)
func (m *StreamWriterManager) RemoveWriter(key StreamMode) error {
	for _, mode := range m.defaultModes {
		if mode == key {
			return exception.NewError(exception.StatusStreamWriterManagerRemoveWriterError).
				Str("mode", key.String()).
				Str("reason", "不允许移除默认 Writer")
		}
	}
	delete(m.writers, key)
	return nil
}

// StreamOutput 返回流输出 channel，消费端通过 range 读取。
// 对应 Python: StreamWriterManager.stream_output()
// 内部启动 goroutine 从 emitter 的队列读取数据，转发到输出 channel。
func (m *StreamWriterManager) StreamOutput() <-chan any {
	out := make(chan any, 0)

	go func() {
		defer close(out)
		queue := m.emitter.StreamQueue()
		for {
			ctx := context.Background()
			data, err := queue.Receive(ctx)
			if err != nil {
				// 队列关闭或超时
				logger.Debug(logComponent).
					Str("event_type", "SESSION_STREAM_CHUNK").
					Str("status", "queue_closed").
					Msg("流输出队列已关闭")
				return
			}

			// 收到 endFrame 哨兵 → 关闭输出 channel
			if _, ok := data.(endFrame); ok {
				logger.Debug(logComponent).
					Str("event_type", "SESSION_STREAM_CHUNK").
					Msg("收到 END_FRAME，流输出结束")
				return
			}

			if data != nil {
				logger.Debug(logComponent).
					Str("event_type", "SESSION_STREAM_CHUNK").
					Str("data_type", logger.SchemaTypeOf(data)).
					Msg("流数据接收成功")
				out <- data
			} else {
				logger.Debug(logComponent).
					Str("event_type", "SESSION_STREAM_CHUNK").
					Str("status", "waiting").
					Msg("未收到流数据，继续等待")
			}
		}
	}()

	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// addDefaultWriters 注册默认 Writer，对齐 Python StreamWriterManager._add_default_writers()
func (m *StreamWriterManager) addDefaultWriters() {
	for _, mode := range m.defaultModes {
		switch mode {
		case StreamModeOutput:
			m.writers[mode] = NewOutputStreamWriter(m.emitter)
		case StreamModeTrace:
			m.writers[mode] = NewTraceStreamWriter(m.emitter)
		case StreamModeCustom:
			m.writers[mode] = NewCustomStreamWriter(m.emitter)
		default:
			panic(exception.NewError(exception.StatusStreamWriterManagerAddWriterError).
				Str("mode", mode.String()).
				Str("reason", "默认模式必须为 OUTPUT/TRACE/CUSTOM"))
		}
	}
}
```

**注意：** `logger.SchemaTypeOf(data)` 是一个辅助函数，需要在 logger 包或 stream 包内定义。如果 logger 包尚未有此函数，先用 `fmt.Sprintf("%T", data)` 替代。

- [ ] **Step 2: 写 manager_test.go**

```go
package stream

import (
	"context"
	"testing"
	"time"
)

// TestNewStreamWriterManager 测试创建管理器
func TestNewStreamWriterManager(t *testing.T) {
	emitter := NewStreamEmitter()
	mgr := NewStreamWriterManager(emitter)

	if mgr.StreamEmitter() != emitter {
		t.Error("StreamEmitter 应与传入的一致")
	}
	if mgr.GetOutputWriter() == nil {
		t.Error("应有默认 OutputWriter")
	}
	if mgr.GetTraceWriter() == nil {
		t.Error("应有默认 TraceWriter")
	}
	if mgr.GetCustomWriter() == nil {
		t.Error("应有默认 CustomWriter")
	}
}

// TestNewStreamWriterManager_NilEmitter 测试 emitter 为 nil 时 panic
func TestNewStreamWriterManager_NilEmitter(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("emitter 为 nil 应 panic")
		}
	}()
	NewStreamWriterManager(nil)
}

// TestStreamWriterManager_WriteAndRead 测试写入和消费完整流程
func TestStreamWriterManager_WriteAndRead(t *testing.T) {
	emitter := NewStreamEmitter()
	mgr := NewStreamWriterManager(emitter)
	ctx := context.Background()

	// 启动消费端
	outCh := mgr.StreamOutput()

	// 写入数据
	writer := mgr.GetOutputWriter()
	schema := OutputSchema{Type: "message", Index: 0, Payload: "hello"}
	if err := writer.Write(ctx, schema); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	// 读取数据
	select {
	case data := <-outCh:
		if out, ok := data.(OutputSchema); !ok || out.Payload != "hello" {
			t.Errorf("读取数据 = %v, want OutputSchema{Payload:hello}", data)
		}
	case <-time.After(time.Second):
		t.Fatal("读取超时")
	}

	// 关闭后消费端应退出
	emitter.Close(ctx)

	select {
	case _, ok := <-outCh:
		if ok {
			t.Error("关闭后 channel 应已关闭")
		}
	case <-time.After(time.Second):
		t.Fatal("关闭后 channel 未关闭，超时")
	}
}

// TestStreamWriterManager_RemoveDefaultWriter 测试不允许移除默认 Writer
func TestStreamWriterManager_RemoveDefaultWriter(t *testing.T) {
	emitter := NewStreamEmitter()
	mgr := NewStreamWriterManager(emitter)

	if err := mgr.RemoveWriter(StreamModeOutput); err == nil {
		t.Error("移除默认 Writer 应返回错误")
	}
}

// TestStreamWriterManager_AddAndRemoveCustomWriter 测试添加和移除自定义 Writer
func TestStreamWriterManager_AddAndRemoveCustomWriter(t *testing.T) {
	emitter := NewStreamEmitter()
	mgr := NewStreamWriterManager(emitter)

	customMode := StreamMode(10)
	customWriter := NewOutputStreamWriter(emitter)

	if err := mgr.AddWriter(customMode, customWriter); err != nil {
		t.Fatalf("AddWriter 失败: %v", err)
	}
	if mgr.GetWriter(customMode) == nil {
		t.Error("添加后 GetWriter 不应为 nil")
	}
	if err := mgr.RemoveWriter(customMode); err != nil {
		t.Fatalf("RemoveWriter 失败: %v", err)
	}
	if mgr.GetWriter(customMode) != nil {
		t.Error("移除后 GetWriter 应为 nil")
	}
}

// TestStreamWriterManager_CustomStream 测试自定义流写入和消费
func TestStreamWriterManager_CustomStream(t *testing.T) {
	emitter := NewStreamEmitter()
	mgr := NewStreamWriterManager(emitter)
	ctx := context.Background()

	outCh := mgr.StreamOutput()

	writer := mgr.GetCustomWriter()
	schema := CustomSchema{Type: "my_event", Data: map[string]any{"key": "val"}}
	if err := writer.Write(ctx, schema); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}

	select {
	case data := <-outCh:
		if out, ok := data.(CustomSchema); !ok || out.Type != "my_event" {
			t.Errorf("读取数据 = %v, want CustomSchema{Type:my_event}", data)
		}
	case <-time.After(time.Second):
		t.Fatal("读取超时")
	}

	emitter.Close(ctx)
}
```

- [ ] **Step 3: 运行测试验证通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/stream/... -v -run "TestNewStreamWriterManager|TestStreamWriterManager" -count=1`

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/session/stream/
git commit -m "feat(stream): 添加 StreamWriterManager 流写入器管理器 (5.10 Task 5)"
```

---

## Task 6: 运行全部 stream 包测试

**Files:** 无新增

- [ ] **Step 1: 运行全部 stream 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/stream/... -v -count=1 -cover`

Expected: 所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 2: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/stream/... -coverprofile=coverage.out -count=1 && go tool cover -func=coverage.out | tail -1`

如果覆盖率低于 85%，补充测试用例。

- [ ] **Step 3: 修复问题（如有）后重新测试**

---

## Task 7: 回填 interfaces.BaseSession 类型

**Files:**
- Modify: `internal/agentcore/session/interfaces/interfaces.go:39-40`

- [ ] **Step 1: 修改 StreamWriterManager 返回类型**

将 `interfaces.go` 第 38-40 行：

```go
// StreamWriterManager 获取流写入管理器
// ⤵️ 5.10 回填：返回类型从 any 改为 StreamWriterManager
StreamWriterManager() any
```

改为：

```go
// StreamWriterManager 获取流写入管理器
StreamWriterManager() StreamWriterManager
```

同时在 `interfaces.go` 的 import 中添加 stream 包引用，并在 BaseSession 接口上方定义类型别名：

```go
// StreamWriterManager 流写入器管理器，类型别名指向 stream.StreamWriterManager
type StreamWriterManager = stream.StreamWriterManager
```

注意：需要在 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"`。

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/... `

此时会有多处编译错误（其他文件还在用 `any`），这是预期的。仅确认 interfaces.go 自身无语法错误。

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/interfaces/
git commit -m "refactor(session): 回填 BaseSession.StreamWriterManager 返回类型 (5.10 Task 7)"
```

---

## Task 8: 回填 ProxySession 类型

**Files:**
- Modify: `internal/agentcore/session/session.go:65-68`

- [ ] **Step 1: 修改 ProxySession.StreamWriterManager 返回类型**

将 `session.go` 第 65-68 行：

```go
// StreamWriterManager 获取底层会话的流写入管理器
func (p *ProxySession) StreamWriterManager() any {
	return p.stub.StreamWriterManager()
}
```

改为：

```go
// StreamWriterManager 获取底层会话的流写入管理器
func (p *ProxySession) StreamWriterManager() interfaces.StreamWriterManager {
	return p.stub.StreamWriterManager()
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/... `

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/session.go
git commit -m "refactor(session): 回填 ProxySession.StreamWriterManager 返回类型 (5.10 Task 8)"
```

---

## Task 9: 回填 AgentSession 内部层类型 + 默认实例创建

**Files:**
- Modify: `internal/agentcore/session/internal/agent_session.go`

- [ ] **Step 1: 修改字段类型、选项函数签名、方法返回类型**

1. 第 29-31 行，字段类型 `any` → `stream.StreamWriterManager`：
```go
// streamWriterManager 流写入管理器
streamWriterManager stream.StreamWriterManager
```

2. 第 82-87 行，取消注释默认实例创建：
```go
// streamWriterManager: nil 时自动创建默认实例
if s.streamWriterManager == nil {
    s.streamWriterManager = stream.NewStreamWriterManager(stream.NewStreamEmitter())
}
```

3. 第 128-133 行，选项函数参数类型：
```go
func WithStreamWriterManager(mgr stream.StreamWriterManager) AgentSessionOption {
```

4. 第 171-173 行，方法返回类型：
```go
func (s *AgentSession) StreamWriterManager() stream.StreamWriterManager {
```

5. 在 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"`。

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/... `

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/internal/agent_session.go
git commit -m "refactor(session): 回填 AgentSession StreamWriterManager 类型 + 默认实例创建 (5.10 Task 9)"
```

---

## Task 10: 回填 WorkflowSession/NodeSession 内部层类型

**Files:**
- Modify: `internal/agentcore/session/internal/workflow_session.go`

- [ ] **Step 1: 修改字段类型、方法返回类型、Set 方法签名**

1. 第 34-36 行，字段类型：
```go
// streamWriterManager 流写入管理器
streamWriterManager stream.StreamWriterManager
```

2. 第 281-283 行，方法返回类型：
```go
func (s *WorkflowSession) StreamWriterManager() stream.StreamWriterManager {
```

3. 第 312-316 行，Set 方法签名：
```go
func (s *WorkflowSession) SetStreamWriterManager(mgr stream.StreamWriterManager) {
```

4. 第 422-424 行，NodeSession 委托方法返回类型：
```go
func (n *NodeSession) StreamWriterManager() stream.StreamWriterManager {
```

5. 在 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"`。

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/... `

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/internal/workflow_session.go
git commit -m "refactor(session): 回填 WorkflowSession/NodeSession StreamWriterManager 类型 (5.10 Task 10)"
```

---

## Task 11: 回填 Session 公开层类型 + 桩方法实现

**Files:**
- Modify: `internal/agentcore/session/agent.go`

- [ ] **Step 1: 修改字段类型和选项函数签名**

1. 第 37-40 行，字段类型：
```go
// streamWriterManagerOverride 流写入管理器覆盖
streamWriterManagerOverride stream.StreamWriterManager
```

2. 第 156-161 行，选项函数签名：
```go
func WithStreamWriterManager(mgr stream.StreamWriterManager) SessionOption {
```

3. 在 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"`。

- [ ] **Step 2: 实现 WriteStream 桩方法（第 259-263 行）**

替换：

```go
// WriteStream 写入标准输出流。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
func (s *Session) WriteStream(data any) error {
	return nil
}
```

为：

```go
// WriteStream 写入标准输出流。
// 对应 Python: Session.write_stream(data)
// data 接受 any 类型，内部通过 normalizeOutputStream 统一转为 OutputSchema。
func (s *Session) WriteStream(data any) error {
	ctx := context.Background()
	streamData := s.normalizeOutputStream(s.tagStreamPayload(data))

	// 触发回调
	// 对应 Python: await trigger(self._session_id + "write_stream", data=stream_data)
	callback.GetCallbackFramework().TriggerSession(ctx, &callback.SessionCallEventData{
		Event:     callback.SessionWriteStream,
		SessionID: s.GetSessionID(),
		Data:      streamData,
	})

	// 通过 StreamWriterManager 写入
	mgr := s.inner.StreamWriterManager()
	if mgr == nil {
		return nil
	}
	writer := mgr.GetOutputWriter()
	if writer == nil {
		return nil
	}
	return writer.Write(ctx, streamData)
}
```

**注意：** `callback.SessionWriteStream` 事件类型需要确认是否已存在。如果不存在，需要添加。`callback.SessionCallEventData.Data` 字段也需要确认。

- [ ] **Step 3: 实现 WriteCustomStream 桩方法（第 265-269 行）**

替换为：

```go
// WriteCustomStream 写入自定义流。
// 对应 Python: Session.write_custom_stream(data)
func (s *Session) WriteCustomStream(data any) error {
	ctx := context.Background()
	streamData := s.tagStreamPayload(data)

	// 触发回调
	callback.GetCallbackFramework().TriggerSession(ctx, &callback.SessionCallEventData{
		Event:     callback.SessionWriteStream,
		SessionID: s.GetSessionID(),
		Data:      streamData,
	})

	mgr := s.inner.StreamWriterManager()
	if mgr == nil {
		return nil
	}
	writer := mgr.GetCustomWriter()
	if writer == nil {
		return nil
	}
	// 构造 CustomSchema
	schema := stream.CustomSchema{Type: "custom", Data: data.(map[string]any)}
	return writer.Write(ctx, schema)
}
```

- [ ] **Step 4: 实现 StreamIterator 桩方法（第 271-275 行）**

替换为：

```go
// StreamIterator 返回流迭代 channel。
// 对应 Python: Session.stream_iterator()
func (s *Session) StreamIterator() <-chan any {
	mgr := s.inner.StreamWriterManager()
	if mgr == nil {
		ch := make(chan any)
		close(ch)
		return ch
	}
	return mgr.StreamOutput()
}
```

- [ ] **Step 5: 实现 CloseStream 桩方法（第 277-281 行）**

替换为：

```go
// CloseStream 关闭流发射器并注销回调。
// 对应 Python: Session.close_stream()
func (s *Session) CloseStream() error {
	ctx := context.Background()
	mgr := s.inner.StreamWriterManager()
	if mgr == nil {
		return nil
	}
	// 关闭 emitter，发送 END_FRAME
	mgr.StreamEmitter().Close(ctx)
	// 注销回调
	// 对应 Python: await Runner.callback_framework.unregister_event(event=self._session_id + "write_stream")
	// ⤵️ R6 回填：callback_framework 需要支持 unregister_event 后补充
	return nil
}
```

- [ ] **Step 6: 添加 normalizeOutputStream 辅助方法**

在 `agent.go` 非导出函数区块添加：

```go
// normalizeOutputStream 将流数据统一转为 OutputSchema。
// 对应 Python: Session._normalize_output_stream(data)
func (s *Session) normalizeOutputStream(data any) stream.OutputSchema {
	switch v := data.(type) {
	case stream.OutputSchema:
		return v
	case map[string]any:
		// 检查是否包含完整 OutputSchema 字段
		if _, hasType := v["type"]; hasType {
			if _, hasIndex := v["index"]; hasIndex {
				if _, hasPayload := v["payload"]; hasPayload {
					return stream.OutputSchema{
						Type:    v["type"].(string),
						Index:   v["index"].(int),
						Payload: v["payload"],
					}
				}
			}
		}
	}
	// 默认构造
	return stream.OutputSchema{Type: "message", Index: 0, Payload: data}
}
```

- [ ] **Step 7: 修改 tagStreamPayload 方法以支持 OutputSchema**

将现有的 `tagStreamPayload(data map[string]any) map[string]any` 修改为接受 `any` 类型：

```go
// tagStreamPayload 为流数据添加来源元数据。
// 对应 Python: Session._tag_stream_payload(data)
func (s *Session) tagStreamPayload(data any) any {
	if len(s.sourceMetadata) == 0 {
		return data
	}
	switch v := data.(type) {
	case map[string]any:
		result := make(map[string]any, len(v)+len(s.sourceMetadata))
		for k, val := range v {
			result[k] = val
		}
		for k, val := range s.sourceMetadata {
			result[k] = val
		}
		return result
	case stream.OutputSchema:
		payload := v.Payload
		if payloadMap, ok := payload.(map[string]any); ok {
			newPayload := make(map[string]any, len(payloadMap)+len(s.sourceMetadata))
			for k, val := range payloadMap {
				newPayload[k] = val
			}
			for k, val := range s.sourceMetadata {
				newPayload[k] = val
			}
			payload = newPayload
		} else {
			newPayload := make(map[string]any, 1+len(s.sourceMetadata))
			newPayload["value"] = payload
			for k, val := range s.sourceMetadata {
				newPayload[k] = val
			}
			payload = newPayload
		}
		return stream.OutputSchema{Type: v.Type, Index: v.Index, Payload: payload}
	default:
		return data
	}
}
```

- [ ] **Step 8: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/... `

- [ ] **Step 9: 运行 session 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -count=1`

- [ ] **Step 10: 提交**

```bash
git add internal/agentcore/session/agent.go
git commit -m "feat(session): 回填 Session StreamWriterManager 类型 + 实现流写入/消费方法 (5.10 Task 11)"
```

---

## Task 12: 回填 NodeSessionFacade 桩方法

**Files:**
- Modify: `internal/agentcore/session/node.go`

- [ ] **Step 1: 实现 WriteStream 方法（第 173-179 行）**

替换：

```go
// WriteStream 写入标准输出流。
// ⤵️ 5.10 回填：StreamWriterManager 实现后填充真实逻辑
// 对应 Python: Session.write_stream(data)
func (f *NodeSessionFacade) WriteStream(ctx context.Context, data any) error {
	// ⤵️ 5.10 回填：writer := f.streamWriter(); if writer != nil { return writer.Write(ctx, data) }
	return nil
}
```

为：

```go
// WriteStream 写入标准输出流。
// 对应 Python: Session.write_stream(data)
func (f *NodeSessionFacade) WriteStream(ctx context.Context, data any) error {
	mgr := f.inner.StreamWriterManager()
	if mgr == nil {
		return nil
	}
	writer := mgr.GetOutputWriter()
	if writer == nil {
		return nil
	}
	// 构造 OutputSchema
	schema := stream.OutputSchema{Type: "message", Index: 0, Payload: data}
	return writer.Write(ctx, schema)
}
```

- [ ] **Step 2: 实现 WriteCustomStream 方法（第 181-187 行）**

替换为：

```go
// WriteCustomStream 写入自定义流。
// 对应 Python: Session.write_custom_stream(data)
func (f *NodeSessionFacade) WriteCustomStream(ctx context.Context, data any) error {
	mgr := f.inner.StreamWriterManager()
	if mgr == nil {
		return nil
	}
	writer := mgr.GetCustomWriter()
	if writer == nil {
		return nil
	}
	// 构造 CustomSchema
	var dataMap map[string]any
	if m, ok := data.(map[string]any); ok {
		dataMap = m
	} else {
		dataMap = map[string]any{"value": data}
	}
	schema := stream.CustomSchema{Type: "custom", Data: dataMap}
	return writer.Write(ctx, schema)
}
```

- [ ] **Step 3: 在 import 中添加 stream 包引用**

- [ ] **Step 4: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/... `

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/session/... -count=1`

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/node.go
git commit -m "feat(session): 回填 NodeSessionFacade WriteStream/WriteCustomStream (5.10 Task 12)"
```

---

## Task 13: 迁移 Interaction 临时接口到 stream 包

**Files:**
- Modify: `internal/agentcore/session/interaction/base.go`
- Modify: `internal/agentcore/session/stream/manager.go`（添加接口定义）

- [ ] **Step 1: 在 stream/manager.go 中定义 InteractionOutputWriterProvider 接口**

在 StreamWriter 接口定义之后添加：

```go
// InteractionOutputWriterProvider 交互所需的输出写入器提供者接口。
// StreamWriterManager 天然满足此接口。
// 从 interaction 包迁移到 stream 包（原 interaction.InteractionOutputWriterProvider）。
type InteractionOutputWriterProvider interface {
	GetOutputWriter() InteractionOutputWriter
}

// InteractionOutputWriter 交互输出写入器接口。
// StreamWriterManager.GetOutputWriter() 返回的 outputWriter 天然满足此接口。
// 从 interaction 包迁移到 stream 包（原 interaction.InteractionOutputWriter）。
type InteractionOutputWriter interface {
	WriteInteraction(outputType string, index int, payload any) error
}
```

- [ ] **Step 2: 让 outputWriter 实现 InteractionOutputWriter 接口**

在 `writer.go` 中为 outputWriter 添加 WriteInteraction 方法：

```go
// WriteInteraction 实现 InteractionOutputWriter 接口，供交互层写入输出。
func (w *outputWriter) WriteInteraction(outputType string, index int, payload any) error {
	ctx := context.Background()
	schema := OutputSchema{Type: outputType, Index: index, Payload: payload}
	return writeStream(w.emitter, ctx, schema)
}
```

- [ ] **Step 3: 让 StreamWriterManager 实现 InteractionOutputWriterProvider 接口**

StreamWriterManager 已有 `GetOutputWriter() StreamWriter` 方法。但接口要求返回 `InteractionOutputWriter`。需要确保 `GetOutputWriter()` 返回的类型同时满足 `InteractionOutputWriter`。由于 outputWriter 已实现 `WriteInteraction`，只需调整 GetOutputWriter 返回类型或添加适配方法。

最简方案：在 StreamWriterManager 上添加一个 GetInteractionOutputWriter 方法：

```go
// GetInteractionOutputWriter 获取交互输出写入器。
// 返回的 outputWriter 同时满足 StreamWriter 和 InteractionOutputWriter 接口。
func (m *StreamWriterManager) GetInteractionOutputWriter() InteractionOutputWriter {
	if w, ok := m.GetWriter(StreamModeOutput).(*outputWriter); ok {
		return w
	}
	return nil
}
```

但 `InteractionOutputWriterProvider` 接口要求的是 `GetOutputWriter() InteractionOutputWriter`，而当前 `GetOutputWriter()` 返回 `StreamWriter`。有两个选择：

选择 A（推荐）：修改 `InteractionOutputWriterProvider` 接口的方法名：
```go
type InteractionOutputWriterProvider interface {
	GetOutputWriter() InteractionOutputWriter
}
```
这与 interaction/base.go 中 `writeInteractionOutput` 的使用方式一致：`provider.GetOutputWriter().WriteInteraction(...)`。

为了让 StreamWriterManager 满足此接口，添加：
```go
// GetOutputWriterInteraction 返回 InteractionOutputWriter，满足 InteractionOutputWriterProvider 接口
func (m *StreamWriterManager) GetOutputWriterInteraction() InteractionOutputWriter {
	return m.GetWriter(StreamModeOutput).(InteractionOutputWriter)
}
```

或者选择 B：让 `GetOutputWriter()` 返回同时满足两个接口的类型。由于 Go 接口是隐式的，只要 outputWriter 实现了 WriteInteraction，`mgr.GetOutputWriter()` 的返回值就可以类型断言为 InteractionOutputWriter。

**采用选择 B**：保持现有 `GetOutputWriter() StreamWriter` 签名不变，在 `interaction/base.go` 的 `writeInteractionOutput` 函数中通过类型断言获取：

```go
if provider, ok := mgr.(stream.InteractionOutputWriterProvider); ok {
```

为此需要让 StreamWriterManager 实现 `InteractionOutputWriterProvider` 接口。添加方法：

```go
// GetInteractionOutputWriter 满足 InteractionOutputWriterProvider 接口
func (m *StreamWriterManager) GetInteractionOutputWriter() InteractionOutputWriter {
	w := m.GetWriter(StreamModeOutput)
	if w == nil {
		return nil
	}
	// outputWriter 天然满足 InteractionOutputWriter
	if iw, ok := w.(InteractionOutputWriter); ok {
		return iw
	}
	return nil
}
```

然后修改 `InteractionOutputWriterProvider` 接口的方法名：

```go
type InteractionOutputWriterProvider interface {
	GetInteractionOutputWriter() InteractionOutputWriter
}
```

- [ ] **Step 4: 修改 interaction/base.go**

1. 删除 `InteractionOutputWriterProvider` 和 `InteractionOutputWriter` 接口定义（第 18-28 行）
2. 在 import 中添加 `"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"`
3. 添加类型别名：
```go
// InteractionOutputWriterProvider 交互输出写入器提供者，类型别名指向 stream 包。
// ✅ 5.10 已回填：从 stream 包导入
type InteractionOutputWriterProvider = stream.InteractionOutputWriterProvider

// InteractionOutputWriter 交互输出写入器，类型别名指向 stream 包。
// ✅ 5.10 已回填：从 stream 包导入
type InteractionOutputWriter = stream.InteractionOutputWriter
```

4. 修改 `writeInteractionOutput` 函数使用新的接口方法名：
```go
if provider, ok := mgr.(InteractionOutputWriterProvider); ok {
    writer := provider.GetInteractionOutputWriter()
    err := writer.WriteInteraction(outputType, index, payload)
```

- [ ] **Step 5: 验证编译 + 测试**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/... && go test ./internal/agentcore/session/... -count=1`

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/session/stream/manager.go internal/agentcore/session/stream/writer.go internal/agentcore/session/interaction/base.go
git commit -m "refactor(session): 迁移 InteractionOutputWriterProvider 到 stream 包 (5.10 Task 13)"
```

---

## Task 14: 更新 session 包 doc.go

**Files:**
- Modify: `internal/agentcore/session/doc.go`

- [ ] **Step 1: 更新 doc.go 中的说明**

将 doc.go 中关于 StreamWriterManager 的占位说明更新为已回填状态。查找并修改包含 "StreamWriterManager" 和 "any 占位" 的文字，标注 ✅ 5.10 已回填。

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/session/... `

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/session/doc.go
git commit -m "docs(session): 更新 doc.go StreamWriterManager 回填状态 (5.10 Task 14)"
```

---

## Task 15: 全量编译 + 测试 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./... `

- [ ] **Step 2: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -tags=test ./internal/agentcore/session/... -count=1 -cover`

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 5.10 行的状态从 `☐` 改为 `✅`，并更新内容描述：

```
| 5.10 | ✅ | StreamWriter | ✅ StreamMode 枚举 + Schema 接口 + OutputSchema/TraceSchema/CustomSchema；✅ StreamQueue + StreamEmitter + StreamWriter + StreamWriterManager；✅ 回填 5.2/5.3/5.4 StreamWriterManager 返回类型；✅ 回填 5.5 NodeSessionFacade.WriteStream/WriteCustomStream()；✅ 迁移 InteractionOutputWriterProvider 到 stream 包 | `openjiuwen/core/session/stream/` |
```

同时更新 5.2/5.3/5.4/5.5 行中 ⤵️ 5.10 相关的标记为 ✅ 5.10 已回填。

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 5.10 StreamWriter 实现状态为已完成"
```

---

## 自检清单

### 1. Spec 覆盖检查

| Spec 要求 | 对应 Task |
|-----------|----------|
| StreamMode 枚举 | Task 1 |
| Schema 接口 + OutputSchema/TraceSchema/CustomSchema | Task 1 |
| StreamQueue 封装 + Send/Receive/Close + 超时 | Task 2 |
| StreamEmitter + Emit/Close/END_FRAME | Task 3 |
| StreamWriter 接口 + outputWriter/traceWriter/customWriter | Task 4 |
| StreamWriterManager + GetWriter/AddWriter/RemoveWriter/StreamOutput | Task 5 |
| BaseSession.StreamWriterManager() 类型回填 | Task 7 |
| ProxySession.StreamWriterManager() 类型回填 | Task 8 |
| AgentSession 字段 + 默认创建 + 选项 + 方法返回类型回填 | Task 9 |
| WorkflowSession/NodeSession 类型回填 | Task 10 |
| Session 公开层字段 + 选项 + 4 个桩方法 + normalizeOutputStream + tagStreamPayload | Task 11 |
| NodeSessionFacade WriteStream/WriteCustomStream 回填 | Task 12 |
| InteractionOutputWriterProvider 迁移 | Task 13 |
| _normalize_output_stream (R1) | Task 11 Step 6 |
| _tag_stream_payload 对 OutputSchema 的处理 (R2) | Task 11 Step 7 |
| close_stream 回调注销 (R6) — 标注为 ⤵️ 后续回填 | Task 11 Step 5 |
| 错误码 111130-111135 | Task 2-5（使用 exception 包已定义的错误码） |
| 日志对照 | Task 2-5（在具体代码中内联实现） |

### 2. Placeholder 扫描

- 无 TBD/TODO/待定
- R6（unregister_event）标注为 ⤵️ 后续回填，因为 callback 框架当前不支持 unregister_event

### 3. 类型一致性

- Schema 接口：base.go 定义 → writer.go 使用 → manager.go 传递
- StreamMode 枚举：base.go 定义 → manager.go 使用
- StreamWriter 接口：writer.go 定义 → manager.go 使用
- stream.StreamWriterManager：stream/manager.go 定义 → interfaces.go 类型别名 → 所有回填文件引用
