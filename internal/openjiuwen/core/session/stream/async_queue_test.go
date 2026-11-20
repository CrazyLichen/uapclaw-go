package stream

import (
	"context"
	"testing"
	"time"
)

// TestNewAsyncStreamQueue_负maxsize 对齐 Python ValueError
func TestNewAsyncStreamQueue_负maxsize(t *testing.T) {
	if _, err := NewAsyncStreamQueue(-1); err == nil {
		t.Fatal("maxsize<0 应报错")
	}
	if q, err := NewAsyncStreamQueue(0); err != nil || q == nil {
		t.Fatal("maxsize=0 应为无界")
	}
}

// TestAsyncStreamQueue_SendReceive 往返
func TestAsyncStreamQueue_SendReceive(t *testing.T) {
	q, _ := NewAsyncStreamQueue(0)
	ctx := context.Background()
	if err := q.Send(ctx, "a", defaultSendAttemptTimeout, defaultMaxSendRetries); err != nil {
		t.Fatal(err)
	}
	v, err := q.Receive(ctx, DefaultReceiveTimeout)
	if err != nil || v != "a" {
		t.Fatalf("got %v err=%v", v, err)
	}
	if q.sentCount != 1 || q.receivedCount != 1 {
		t.Fatalf("计数错误 sent=%d recv=%d", q.sentCount, q.receivedCount)
	}
}

// TestAsyncStreamQueue_Send超时重试耗尽 有界队列满且无人消费 → 重试耗尽报错
func TestAsyncStreamQueue_Send超时重试耗尽(t *testing.T) {
	q, _ := NewAsyncStreamQueue(1)
	ctx := context.Background()
	if err := q.Send(ctx, "a", defaultSendAttemptTimeout, defaultMaxSendRetries); err != nil {
		t.Fatal(err)
	}
	if err := q.Send(ctx, "b", 30*time.Millisecond, 2); err == nil {
		t.Fatal("重试耗尽应报错（对齐 Python TimeoutError）")
	}
}

// TestAsyncStreamQueue_Receive超时 空队列限时接收
func TestAsyncStreamQueue_Receive超时(t *testing.T) {
	q, _ := NewAsyncStreamQueue(0)
	ctx := context.Background()
	if _, err := q.Receive(ctx, 30*time.Millisecond); err == nil {
		t.Fatal("空队列限时接收应报错")
	}
	// ctx 取消
	ctx2, cancel := context.WithCancel(context.Background())
	go func() { time.Sleep(10 * time.Millisecond); cancel() }()
	if _, err := q.Receive(ctx2, DefaultReceiveTimeout); err == nil {
		t.Fatal("ctx 取消应报错")
	}
}

// TestAsyncStreamQueue_Close幂等与排空
func TestAsyncStreamQueue_Close幂等与排空(t *testing.T) {
	q, _ := NewAsyncStreamQueue(0)
	ctx := context.Background()
	_ = q.Send(ctx, "a", defaultSendAttemptTimeout, defaultMaxSendRetries)
	q.Close(ctx, defaultCloseTimeout)
	q.Close(ctx, defaultCloseTimeout) // 幂等
	if !q.IsClosed() {
		t.Fatal("应已关闭")
	}
	if err := q.Send(ctx, "b", defaultSendAttemptTimeout, defaultMaxSendRetries); err == nil {
		t.Fatal("关闭后 send 应报错")
	}
	if _, err := q.Receive(ctx, DefaultReceiveTimeout); err == nil {
		t.Fatal("关闭后 receive 应报错")
	}
}

// TestAsyncStreamQueue_Close超时强清 有界满队列关闭时强制清空
func TestAsyncStreamQueue_Close超时强清(t *testing.T) {
	q, _ := NewAsyncStreamQueue(2)
	ctx := context.Background()
	_ = q.Send(ctx, "a", defaultSendAttemptTimeout, defaultMaxSendRetries)
	_ = q.Send(ctx, "b", defaultSendAttemptTimeout, defaultMaxSendRetries)
	q.Close(ctx, 30*time.Millisecond)
	if len(q.items) != 0 {
		t.Fatalf("超时应强制清空, got %d", len(q.items))
	}
}
