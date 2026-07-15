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
// 内部队列使用缓冲区大小 1024，与 Python asyncio.Queue(maxsize=0) 对齐（Python 0 表示无限大小）。
func NewStreamEmitter() *StreamEmitter {
	return &StreamEmitter{
		queue: NewStreamQueue(1024),
	}
}

// Emit 写入数据到流队列。
// 已关闭时返回错误，对齐 Python: raise RuntimeError("Can not emit data after the stream emitter is closed.")
func (e *StreamEmitter) Emit(ctx context.Context, data Schema) error {
	e.mu.RLock()
	isClosed := e.closed
	e.mu.RUnlock()

	if isClosed {
		return exception.NewBaseError(exception.StatusStreamWriterWriteStreamError,
			exception.WithMsg("emitter 已关闭，无法写入数据"),
		)
	}

	return e.queue.Send(ctx, data)
}

// Close 关闭发射器，单阶段关闭语义。
// 对应 Python: StreamEmitter.close()
//
// 与 Python 的差异：
// Python: emitter._closed=True + queue.send(END_FRAME)，消费端收到 END_FRAME 后调 queue.close()
// Go: emitter.closed=True + queue.Close()，消费端通过 Receive() 返回 ErrQueueClosed 感知流结束
//
// Go 的 close(ch) 保证消费端可读残留数据直到 ok=false，等价于 Python 两阶段关闭，
// 但消除了 END_FRAME 丢失导致消费端永不退出的风险。
func (e *StreamEmitter) Close(ctx context.Context) error {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return nil
	}
	e.closed = true
	e.mu.Unlock()

	// 直接关闭队列，消费端通过 Receive() 返回 ErrQueueClosed 感知流结束
	if err := e.queue.Close(ctx); err != nil {
		logger.Warn(logComponent).
			Err(err).
			Msg("StreamEmitter 关闭队列失败")
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
