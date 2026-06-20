package stream

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamQueue 流队列，封装 buffered channel + 超时控制。
// 对应 Python: AsyncStreamQueue
//
// 与 Python 的关键设计差异：
// Python 使用 END_FRAME 哨兵通知消费端流结束，Go 直接使用 close(ch) 信号。
// Go 的 close(ch) 语义保证消费端可读取残留数据直到 ok=false，
// 等价于 Python 的 END_FRAME + queue.join() 两阶段关闭，但更简洁可靠。
type StreamQueue struct {
	// ch 内部缓冲 channel，只传输 Schema 数据
	ch chan Schema
	// closed 队列是否已关闭（原子操作，读多写少场景替代 RWMutex）
	// 对齐 Python AsyncStreamQueue._closed，closed=true 后不再接受新 Send
	closed atomic.Bool
	// chCloseOnce 保证 channel 只 close 一次
	chCloseOnce sync.Once
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
	// DefaultReceiveTimeout 默认接收超时，对应 Python DEFAULT_RECEIVE_TIMEOUT = -1（无限等待）。
	// 值 <= 0 表示无限等待，与 Python 语义一致。
	DefaultReceiveTimeout time.Duration = -1
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// ErrQueueClosed 队列已关闭
	ErrQueueClosed = errors.New("stream queue is closed")
	// ErrQueueSendRetryExhausted 发送重试耗尽
	ErrQueueSendRetryExhausted = errors.New("stream queue send retry exhausted")
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStreamQueue 创建流队列，maxSize 为缓冲区大小（0 为无缓冲）。
// 对应 Python: AsyncStreamQueue(maxsize=0)
// maxSize 为负数时 panic，对齐 Python asyncio.Queue 负数 maxsize 抛 ValueError。
func NewStreamQueue(maxSize int) *StreamQueue {
	if maxSize < 0 {
		panic(fmt.Sprintf("StreamQueue maxSize 不能为负数: %d", maxSize))
	}
	return &StreamQueue{
		ch: make(chan Schema, maxSize),
	}
}

// Send 带超时的发送，对齐 Python AsyncStreamQueue.send()。
// closed 后直接返回 ErrQueueClosed；使用 recover 捕获 closed 检查与 close(ch) 之间的窗口 panic。
func (q *StreamQueue) Send(ctx context.Context, data Schema, attemptTimeout ...time.Duration) (err error) {
	// recover 兜底：closed.Load() 返回 false 后、ch<- 之前，Close 可能已 close(ch)
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).
				Str("event_type", "SESSION_STREAM_ERROR").
				Msg("Send 检测到 channel 已关闭，返回 ErrQueueClosed")
			err = ErrQueueClosed
		}
	}()

	timeout := defaultSendAttemptTimeout
	if len(attemptTimeout) > 0 {
		timeout = attemptTimeout[0]
	}
	maxRetries := defaultMaxSendRetries

	if q.closed.Load() {
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
//
// 与 Python 的关键差异：Python receive 在 _closed=True 时直接抛 RuntimeError，
// 但 Python 的 close() 先 queue.join() 等消费端排空才设 _closed。
// Go 的 close(ch) 不会等消费端排空，所以 Receive 必须支持从已关闭 channel 读取残留数据：
//   - closed 后仍从 channel 读取，直到 channel 为空且已关闭（ok=false）时返回 ErrQueueClosed
//   - 超时时检查 closed 状态：closed 返回 ErrQueueClosed，否则返回 context 错误
func (q *StreamQueue) Receive(ctx context.Context, timeout ...time.Duration) (Schema, error) {
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
			// channel 已关闭且为空，所有数据已消费完毕
			return nil, ErrQueueClosed
		}
		logger.Debug(logComponent).
			Str("event_type", "SESSION_STREAM_CHUNK").
			Msg("流数据接收成功")
		return data, nil
	case <-recvCtx.Done():
		// 超时：区分 closed 和正常超时
		if q.closed.Load() {
			return nil, ErrQueueClosed
		}
		return nil, recvCtx.Err()
	}
}

// Close 关闭队列，对齐 Python AsyncStreamQueue.close()。
//
// 流程：设 closed=true → close(ch)
//
// 使用 sync.Once 保证 close(ch) 只执行一次，天然幂等。
// 关闭后消费端仍可读取残留数据直到 ok=false（Go channel 语义保证）。
func (q *StreamQueue) Close(ctx context.Context, timeout ...time.Duration) error {
	if q.closed.Load() {
		return nil
	}
	q.closed.Store(true)

	// close(ch)：消费端仍可读取残留数据，直到 ok=false
	q.chCloseOnce.Do(func() {
		close(q.ch)
	})

	return nil
}

// Ch 返回只读 channel，供消费端 range 读取。
func (q *StreamQueue) Ch() <-chan Schema {
	return q.ch
}

// IsClosed 查询关闭状态
func (q *StreamQueue) IsClosed() bool {
	return q.closed.Load()
}

// IsEndOfStream 判断 Receive 返回的错误是否表示流正常结束。
// 对应 Python: data == StreamEmitter.END_FRAME
// Go 用 close(ch) 替代 END_FRAME 哨兵，流结束通过 ErrQueueClosed 标识。
func IsEndOfStream(err error) bool {
	return errors.Is(err, ErrQueueClosed)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
