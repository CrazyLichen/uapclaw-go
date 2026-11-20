package tool

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestMapFunction_Invoke 测试弱类型 Invoke
func TestMapFunction_Invoke(t *testing.T) {
	card := NewToolCard("echo", "回显工具", []*schema.Param{
		schema.NewStringParam("message", "消息", true),
	}, nil)

	fn, err := NewMapFunction(card,
		func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
			return map[string]any{"echo": inputs["message"]}, nil
		},
		nil,
	)
	if err != nil {
		t.Fatalf("NewMapFunction 失败: %v", err)
	}

	result, err := fn.Invoke(context.Background(), map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	if result["echo"] != "hello" {
		t.Errorf("echo: 期望 hello，实际 %v", result["echo"])
	}
}

// TestMapFunction_Stream 测试弱类型 Stream
func TestMapFunction_Stream(t *testing.T) {
	card := NewToolCard("stream_echo", "流式回显", []*schema.Param{
		schema.NewStringParam("message", "消息", true),
	}, nil)

	fn, err := NewMapFunction(card,
		nil,
		func(ctx context.Context, inputs map[string]any) (<-chan map[string]any, error) {
			ch := make(chan map[string]any, 1)
			go func() {
				defer close(ch)
				ch <- map[string]any{"echo": inputs["message"]}
			}()
			return ch, nil
		},
	)
	if err != nil {
		t.Fatalf("NewMapFunction 失败: %v", err)
	}

	ch, err := fn.Stream(context.Background(), map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("Stream 失败: %v", err)
	}

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}
	if len(chunks) < 1 {
		t.Error("应至少收到 1 个数据块")
	}
}

// TestMapFunction_InvokeNilFn 测试 Invoke 函数为 nil 时返回错误
func TestMapFunction_Invoke函数为空(t *testing.T) {
	card := NewToolCard("echo", "回显工具", []*schema.Param{
		schema.NewStringParam("message", "消息", true),
	}, nil)
	fn, _ := NewMapFunction(card, nil,
		func(ctx context.Context, inputs map[string]any) (<-chan map[string]any, error) {
			ch := make(chan map[string]any, 1)
			go func() {
				defer close(ch)
				ch <- map[string]any{"echo": inputs["message"]}
			}()
			return ch, nil
		},
	)

	_, err := fn.Invoke(context.Background(), map[string]any{"message": "hello"})
	if err == nil {
		t.Error("invokeFn 为 nil 时 Invoke 应返回 ErrInvokeNotSupported")
	}
}

// TestMapFunction_StreamNilFn 测试 Stream 函数为 nil 时返回错误
func TestMapFunction_Stream函数为空(t *testing.T) {
	card := NewToolCard("echo", "回显工具", []*schema.Param{
		schema.NewStringParam("message", "消息", true),
	}, nil)
	fn, _ := NewMapFunction(card,
		func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
			return map[string]any{"echo": inputs["message"]}, nil
		},
		nil,
	)

	_, err := fn.Stream(context.Background(), map[string]any{"message": "hello"})
	if err == nil {
		t.Error("streamFn 为 nil 时 Stream 应返回 ErrStreamNotSupported")
	}
}

// TestMapFunction_Card 测试 Card 方法
func TestMapFunction_Card方法(t *testing.T) {
	card := NewToolCard("echo", "回显工具", []*schema.Param{
		schema.NewStringParam("message", "消息", true),
	}, nil)
	fn, _ := NewMapFunction(card, nil, nil)
	if fn.Card().Name != "echo" {
		t.Errorf("Card Name: 期望 echo，实际 %q", fn.Card().Name)
	}
}

// TestMapFunction_InvalidCard 测试无效卡片
func TestMapFunction_无效卡片(t *testing.T) {
	_, err := NewMapFunction(nil, nil, nil)
	if err == nil {
		t.Error("nil card 应返回错误")
	}
}

// TestMapFunction_InvokeWithFormat 测试 Invoke 的参数格式化
func TestMapFunction_Invoke带格式化(t *testing.T) {
	card := NewToolCard("echo", "回显工具", []*schema.Param{
		schema.NewStringParam("message", "消息", true),
	}, nil)
	fn, _ := NewMapFunction(card,
		func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
			return map[string]any{"echo": inputs["message"]}, nil
		},
		nil,
	)

	result, err := fn.Invoke(context.Background(), map[string]any{"message": "hello"})
	if err != nil {
		t.Fatalf("Invoke 失败: %v", err)
	}
	if result["echo"] != "hello" {
		t.Errorf("echo: 期望 hello，实际 %v", result["echo"])
	}
}
