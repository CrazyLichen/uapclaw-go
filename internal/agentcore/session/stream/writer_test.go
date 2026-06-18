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

// TestOutputWriter_WriteInteraction 测试交互输出写入
func TestOutputWriter_WriteInteraction(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewOutputStreamWriter(emitter)

	if err := w.WriteInteraction("__interaction__", 0, "hello"); err != nil {
		t.Fatalf("WriteInteraction 失败: %v", err)
	}

	data, err := emitter.StreamQueue().Receive(ctx)
	if err != nil {
		t.Fatalf("Receive 失败: %v", err)
	}
	if out, ok := data.(OutputSchema); !ok || out.Type != "__interaction__" || out.Payload != "hello" {
		t.Errorf("WriteInteraction 数据 = %v, want OutputSchema{Type:__interaction__,Payload:hello}", data)
	}
}
