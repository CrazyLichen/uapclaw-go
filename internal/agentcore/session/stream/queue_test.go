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

// TestStreamQueue_ReceiveAfterClose 测试关闭后接收行为
// Close 只做 closed=true + close(ch)，不发送 endFrame（endFrame 由 Emitter 负责）。
// Close 后 channel 为空且已关闭，Receive 返回 ErrQueueClosed。
func TestStreamQueue_ReceiveAfterClose(t *testing.T) {
	q := NewStreamQueue(10)
	ctx := context.Background()

	if err := q.Close(ctx); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	// Close 后 channel 为空且已关闭，Receive 直接返回 ErrQueueClosed
	_, err := q.Receive(ctx)
	if err != ErrQueueClosed {
		t.Errorf("Close 后 Receive 应返回 ErrQueueClosed，实际 err=%v", err)
	}
}

// TestStreamQueue_ReceiveResidualDataAfterClose 测试关闭后读取残留数据
// 对齐 Python：消费端收到 END_FRAME 后调 queue.close()，此时 channel 中可能有残留数据。
// Go 的 close(ch) 后仍可读取残留数据，直到 ok=false 返回 ErrQueueClosed。
func TestStreamQueue_ReceiveResidualDataAfterClose(t *testing.T) {
	q := NewStreamQueue(10)
	ctx := context.Background()

	// 先发送数据
	_ = q.Send(ctx, "data1")
	_ = q.Send(ctx, "data2")

	// Close 后 channel 中仍有残留数据
	_ = q.Close(ctx)

	// 第一次 Receive 读到残留 data1
	data, err := q.Receive(ctx)
	if err != nil {
		t.Fatalf("第一次 Receive 应读到残留数据，实际 err=%v", err)
	}
	if data != "data1" {
		t.Errorf("第一次 Receive 数据 = %v, want %q", data, "data1")
	}

	// 第二次 Receive 读到残留 data2
	data, err = q.Receive(ctx)
	if err != nil {
		t.Fatalf("第二次 Receive 应读到残留数据，实际 err=%v", err)
	}
	if data != "data2" {
		t.Errorf("第二次 Receive 数据 = %v, want %q", data, "data2")
	}

	// 第三次 Receive：channel 为空且已关闭，返回 ErrQueueClosed
	_, err = q.Receive(ctx)
	if err != ErrQueueClosed {
		t.Errorf("残留数据读完后 Receive 应返回 ErrQueueClosed，实际 err=%v", err)
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

// TestStreamQueue_CloseIdempotent 测试重复关闭是幂等的
func TestStreamQueue_CloseIdempotent(t *testing.T) {
	q := NewStreamQueue(10)
	ctx := context.Background()

	if err := q.Close(ctx); err != nil {
		t.Fatalf("第一次 Close 失败: %v", err)
	}
	if err := q.Close(ctx); err != nil {
		t.Fatalf("第二次 Close 失败: %v", err)
	}
}

// TestStreamQueue_Ch 测试只读 channel
func TestStreamQueue_Ch(t *testing.T) {
	q := NewStreamQueue(10)
	ctx := context.Background()

	_ = q.Send(ctx, "data1")
	_ = q.Send(ctx, endFrame{})

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

// TestStreamQueue_SendWithContextCancel 测试上下文取消时 Send 返回错误
func TestStreamQueue_SendWithContextCancel(t *testing.T) {
	q := NewStreamQueue(0) // 无缓冲，没有消费者
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // 立即取消

	err := q.Send(ctx, "data", 50*time.Millisecond)
	if err == nil {
		t.Error("上下文取消后 Send 应返回错误")
	}
}
