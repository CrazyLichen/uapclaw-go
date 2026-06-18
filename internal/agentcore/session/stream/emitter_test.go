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

// TestStreamEmitter_StreamQueue 测试获取内部队列
func TestStreamEmitter_StreamQueue(t *testing.T) {
	e := NewStreamEmitter()
	if e.StreamQueue() == nil {
		t.Error("StreamQueue 不应为 nil")
	}
}
