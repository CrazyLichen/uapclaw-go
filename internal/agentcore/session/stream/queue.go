package stream

import (
	"context"
	"errors"
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

var (
	// ErrQueueClosed 队列已关闭
	ErrQueueClosed = errors.New("stream queue is closed")
	// ErrQueueSendRetryExhausted 发送重试耗尽
	ErrQueueSendRetryExhausted = errors.New("stream queue send retry exhausted")
)

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
