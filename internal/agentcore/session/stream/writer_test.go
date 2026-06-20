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

	_ = emitter.Close(ctx)

	schema := OutputSchema{Type: "message", Index: 0, Payload: "hello"}
	if err := w.Write(ctx, schema); err != nil {
		t.Fatalf("关闭后 Write 不应返回错误（丢弃数据）: %v", err)
	}
}

// TestOutputWriter_Write_EmptyType 测试 OutputSchema Type 为空时返回校验错误
func TestOutputWriter_Write_EmptyType(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewOutputStreamWriter(emitter)

	schema := OutputSchema{Type: "", Index: 0, Payload: "hello"}
	if err := w.Write(ctx, schema); err == nil {
		t.Error("Type 为空应返回校验错误")
	}
}

// TestOutputWriter_Write_NegativeIndex 测试 OutputSchema Index 为负数时返回校验错误
func TestOutputWriter_Write_NegativeIndex(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewOutputStreamWriter(emitter)

	schema := OutputSchema{Type: "message", Index: -1, Payload: "hello"}
	if err := w.Write(ctx, schema); err == nil {
		t.Error("Index 为负数应返回校验错误")
	}
}

// TestOutputWriter_Write_WhitespaceType 测试 OutputSchema Type 为纯空格时返回校验错误
func TestOutputWriter_Write_WhitespaceType(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewOutputStreamWriter(emitter)

	schema := OutputSchema{Type: "  ", Index: 0, Payload: "hello"}
	if err := w.Write(ctx, schema); err == nil {
		t.Error("Type 为纯空格应返回校验错误")
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

// TestTraceWriter_Write_EmptyType 测试 TraceSchema Type 为空时返回校验错误
func TestTraceWriter_Write_EmptyType(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewTraceStreamWriter(emitter)

	schema := TraceSchema{Type: "", Payload: "data"}
	if err := w.Write(ctx, schema); err == nil {
		t.Error("Type 为空应返回校验错误")
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

// TestCustomWriter_Write_EmptyType 测试 CustomSchema Type 为空时不报错（对齐 Python extra="allow"）
func TestCustomWriter_Write_EmptyType(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewCustomStreamWriter(emitter)

	// Python CustomSchema 无必填字段，Type 为空不报错
	schema := CustomSchema{Type: "", Data: map[string]any{"key": "val"}}
	if err := w.Write(ctx, schema); err != nil {
		t.Fatalf("CustomSchema Type 为空不应报错: %v", err)
	}
}

// TestCustomWriter_Write_NoData 测试 CustomSchema 无 Data 时不报错
func TestCustomWriter_Write_NoData(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewCustomStreamWriter(emitter)

	// Python CustomSchema 无必填字段
	schema := CustomSchema{}
	if err := w.Write(ctx, schema); err != nil {
		t.Fatalf("空 CustomSchema 不应报错: %v", err)
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

// ──────────────────────────── SW-20 类型限制校验 ────────────────────────────

// TestOutputWriter_Write_类型不匹配 测试 outputWriter 写入非 OutputSchema 类型时返回校验错误
func TestOutputWriter_Write_类型不匹配(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewOutputStreamWriter(emitter)

	// 传入 TraceSchema 应被拒绝
	if err := w.Write(ctx, TraceSchema{Type: "step", Payload: "data"}); err == nil {
		t.Error("outputWriter 写入 TraceSchema 应返回校验错误")
	}
	// 传入 CustomSchema 应被拒绝
	if err := w.Write(ctx, CustomSchema{Type: "event", Data: nil}); err == nil {
		t.Error("outputWriter 写入 CustomSchema 应返回校验错误")
	}
}

// TestTraceWriter_Write_类型不匹配 测试 traceWriter 写入非 TraceSchema 类型时返回校验错误
func TestTraceWriter_Write_类型不匹配(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewTraceStreamWriter(emitter)

	// 传入 OutputSchema 应被拒绝
	if err := w.Write(ctx, OutputSchema{Type: "message", Index: 0, Payload: "hello"}); err == nil {
		t.Error("traceWriter 写入 OutputSchema 应返回校验错误")
	}
	// 传入 CustomSchema 应被拒绝
	if err := w.Write(ctx, CustomSchema{Type: "event", Data: nil}); err == nil {
		t.Error("traceWriter 写入 CustomSchema 应返回校验错误")
	}
}

// TestCustomWriter_Write_类型不匹配 测试 customWriter 写入非 CustomSchema 类型时返回校验错误
func TestCustomWriter_Write_类型不匹配(t *testing.T) {
	emitter := NewStreamEmitter()
	ctx := context.Background()
	w := NewCustomStreamWriter(emitter)

	// 传入 OutputSchema 应被拒绝
	if err := w.Write(ctx, OutputSchema{Type: "message", Index: 0, Payload: "hello"}); err == nil {
		t.Error("customWriter 写入 OutputSchema 应返回校验错误")
	}
	// 传入 TraceSchema 应被拒绝
	if err := w.Write(ctx, TraceSchema{Type: "step", Payload: "data"}); err == nil {
		t.Error("customWriter 写入 TraceSchema 应返回校验错误")
	}
}
