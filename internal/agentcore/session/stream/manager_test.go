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
	_ = emitter.Close(ctx)

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
	if err := mgr.RemoveWriter(StreamModeTrace); err == nil {
		t.Error("移除默认 Trace Writer 应返回错误")
	}
	if err := mgr.RemoveWriter(StreamModeCustom); err == nil {
		t.Error("移除默认 Custom Writer 应返回错误")
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

// TestStreamWriterManager_AddNilWriter 测试添加 nil writer 返回错误
func TestStreamWriterManager_AddNilWriter(t *testing.T) {
	emitter := NewStreamEmitter()
	mgr := NewStreamWriterManager(emitter)

	if err := mgr.AddWriter(StreamMode(10), nil); err == nil {
		t.Error("添加 nil writer 应返回错误")
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

	_ = emitter.Close(ctx)
}

// TestStreamWriterManager_InteractionOutputWriter 测试交互输出写入器接口
func TestStreamWriterManager_InteractionOutputWriter(t *testing.T) {
	emitter := NewStreamEmitter()
	mgr := NewStreamWriterManager(emitter)

	// 验证满足 InteractionOutputWriterProvider 接口
	var _ InteractionOutputWriterProvider = mgr

	iw := mgr.GetInteractionOutputWriter()
	if iw == nil {
		t.Fatal("GetInteractionOutputWriter 不应为 nil")
	}

	// 通过接口写入
	if err := iw.WriteInteraction("__interaction__", 0, "test"); err != nil {
		t.Fatalf("WriteInteraction 失败: %v", err)
	}

	// 验证数据在队列中
	ctx := context.Background()
	data, err := emitter.StreamQueue().Receive(ctx)
	if err != nil {
		t.Fatalf("Receive 失败: %v", err)
	}
	if out, ok := data.(OutputSchema); !ok || out.Type != "__interaction__" {
		t.Errorf("交互输出数据 = %v, want OutputSchema{Type:__interaction__}", data)
	}
}
