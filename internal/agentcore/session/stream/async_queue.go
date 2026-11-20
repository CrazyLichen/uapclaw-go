package stream

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

// 复用 queue.go 已有常量：defaultSendAttemptTimeout/defaultMaxSendRetries/defaultCloseTimeout/
// DefaultReceiveTimeout（Python AsyncStreamQueue 与 StreamQueue 共用同一组默认值）

const asyncQueueLogComponent = logger.ComponentCommon

// ──────────────────────────── 结构体 ────────────────────────────

// AsyncStreamQueue 任意数据的异步流队列（非 Schema 强类型，区别于 StreamQueue）。
// Python: openjiuwen/core/session/stream/emitter.py AsyncStreamQueue。
// maxsize>0 时为有界队列（put 满时阻塞等待，send 按 attempt_timeout×max_retries 重试）。
type AsyncStreamQueue struct {
	mu      sync.Mutex
	cond    chan struct{} // 广播信号：每次 put/clear 换新并关闭旧
	items   []any
	maxsize int
	closed  bool
	// sentCount/receivedCount 收发计数（close 时汇总日志）
	sentCount     int
	receivedCount int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAsyncStreamQueue 创建队列；maxsize<0 报错（对齐 Python ValueError）。
func NewAsyncStreamQueue(maxsize int) (*AsyncStreamQueue, error) {
	if maxsize < 0 {
		return nil, fmt.Errorf("maxsize must be >= 0")
	}
	return &AsyncStreamQueue{maxsize: maxsize, cond: make(chan struct{})}, nil
}

// IsClosed 队列是否已关闭
func (q *AsyncStreamQueue) IsClosed() bool {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.closed
}

// Send 发送数据（Python send 一比一）：已关闭报错；单次尝试限时放入，
// 超时记 error 日志并重试；重试耗尽后报错（Python raise TimeoutError）。
func (q *AsyncStreamQueue) Send(ctx context.Context, data any,
	attemptTimeout time.Duration, maxRetries int) error {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return fmt.Errorf("StreamQueue is already closed")
	}
	q.mu.Unlock()
	if attemptTimeout <= 0 {
		attemptTimeout = defaultSendAttemptTimeout
	}
	if maxRetries <= 0 {
		maxRetries = defaultMaxSendRetries
	}
	for attempt := 0; attempt < maxRetries; attempt++ {
		if q.putWithTimeout(ctx, data, attemptTimeout) {
			q.mu.Lock()
			q.sentCount++
			q.mu.Unlock()
			logger.Debug(asyncQueueLogComponent).
				Str("event_type", "SESSION_STREAM_CHUNK").
				Int("attempt", attempt+1).
				Msg("Stream data sent successfully")
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		logger.Error(asyncQueueLogComponent).
			Str("event_type", "SESSION_STREAM_ERROR").
			Str("timeout", attemptTimeout.String()).
			Int("attempt", attempt+1).
			Msg("Stream data send timeout")
	}
	logger.Error(asyncQueueLogComponent).
		Str("event_type", "SESSION_STREAM_ERROR").
		Int("max_retries", maxRetries).
		Str("timeout", attemptTimeout.String()).
		Msg("Failed to send stream data after max retries")
	return fmt.Errorf("failed to send stream data after %d attempts (attempt_timeout=%s)",
		maxRetries, attemptTimeout)
}

// Receive 接收数据（Python receive 一比一）：已关闭报错；timeout<=0 不限时。
func (q *AsyncStreamQueue) Receive(ctx context.Context, timeout time.Duration) (any, error) {
	// closed 时不立即报错：先排空缓冲（Python emitter 的 END_FRAME 哨兵是入队元素，
	// drain 循环按序读完数据帧才遇哨兵——Go 以 close 等价哨兵（D-14-3），缓冲帧
	// 必须仍可消费，缓冲排空才承载 closed 语义；否则 close 瞬间丢帧）
	q.mu.Lock()
	closed := q.closed
	q.mu.Unlock()
	_ = closed
	if timeout <= 0 {
		timeout = 0
	}
	item, ok := q.getWithTimeout(ctx, timeout)
	if !ok {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("receive timeout after %s", timeout)
	}
	q.mu.Lock()
	q.receivedCount++
	q.mu.Unlock()
	logger.Debug(asyncQueueLogComponent).
		Str("event_type", "SESSION_STREAM_CHUNK").
		Msg("Stream data received successfully")
	return item, nil
}

// Close 关闭队列（Python close 一比一）：幂等；限时等待队列排空，超时强制清空。
func (q *AsyncStreamQueue) Close(ctx context.Context, timeout time.Duration) {
	q.mu.Lock()
	if q.closed {
		q.mu.Unlock()
		return
	}
	q.closed = true
	old := q.cond
	q.cond = make(chan struct{})
	q.mu.Unlock()
	close(old)
	logger.Info(asyncQueueLogComponent).
		Str("event_type", "SESSION_STREAM_CHUNK").
		Int("sent_count", q.sentCount).
		Int("received_count", q.receivedCount).
		Msg("StreamQueue closed")
	if timeout <= 0 {
		timeout = defaultCloseTimeout
	}
	// 等待队列排空（对齐 asyncio.wait_for(queue.join(), timeout)）
	deadline := time.Now().Add(timeout)
	for {
		q.mu.Lock()
		empty := len(q.items) == 0
		q.mu.Unlock()
		if empty {
			return
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			logger.Error(asyncQueueLogComponent).
				Str("event_type", "SESSION_STREAM_ERROR").
				Str("timeout", timeout.String()).
				Msg("StreamQueue close timeout, force clearing queue")
			q.forceClear()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// putWithTimeout 限时放入；有界队列满时等待，超时返回 false
func (q *AsyncStreamQueue) putWithTimeout(ctx context.Context, data any, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		q.mu.Lock()
		if q.closed {
			q.mu.Unlock()
			return false
		}
		if q.maxsize <= 0 || len(q.items) < q.maxsize {
			q.items = append(q.items, data)
			old := q.cond
			q.cond = make(chan struct{})
			q.mu.Unlock()
			close(old)
			return true
		}
		wait := q.cond
		q.mu.Unlock()
		remain := time.Until(deadline)
		if remain <= 0 {
			return false
		}
		timer := time.NewTimer(remain)
		select {
		case <-ctx.Done():
			timer.Stop()
			return false
		case <-timer.C:
		case <-wait:
			timer.Stop()
		}
	}
}

// getWithTimeout 限时取出；timeout==0 不限时
func (q *AsyncStreamQueue) getWithTimeout(ctx context.Context, timeout time.Duration) (any, bool) {
	var deadline time.Time
	if timeout > 0 {
		deadline = time.Now().Add(timeout)
	}
	for {
		q.mu.Lock()
		if q.closed && len(q.items) == 0 {
			// close 等价 END_FRAME 哨兵：缓冲排空后的 closed 才终止消费
			q.mu.Unlock()
			return nil, false
		}
		if len(q.items) > 0 {
			v := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			return v, true
		}
		if timeout > 0 && !time.Now().Before(deadline) {
			q.mu.Unlock()
			return nil, false
		}
		wait := q.cond
		q.mu.Unlock()
		var timer <-chan time.Time
		if timeout > 0 {
			timer = time.After(time.Until(deadline))
		}
		select {
		case <-ctx.Done():
			return nil, false
		case <-timer:
			return nil, false
		case <-wait:
		}
	}
}

// forceClear 强制清空队列（Python _force_clear：计数与日志）
func (q *AsyncStreamQueue) forceClear() {
	q.mu.Lock()
	cleared := len(q.items)
	q.items = nil
	old := q.cond
	q.cond = make(chan struct{})
	q.mu.Unlock()
	close(old)
	logger.Info(asyncQueueLogComponent).
		Str("event_type", "SESSION_STREAM_CHUNK").
		Int("cleared_items", cleared).
		Msg("StreamQueue force cleared")
}
