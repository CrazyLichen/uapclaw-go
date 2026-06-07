package tool

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	runnnercallback "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockTool 用于测试的模拟 Tool
type mockTool struct {
	card     *ToolCard
	invokeFn func(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error)
	streamFn func(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error)
}

func (m *mockTool) Card() *ToolCard { return m.card }
func (m *mockTool) Invoke(ctx context.Context, inputs map[string]any, opts ...ToolOption) (map[string]any, error) {
	return m.invokeFn(ctx, inputs, opts...)
}
func (m *mockTool) Stream(ctx context.Context, inputs map[string]any, opts ...ToolOption) (<-chan StreamChunk, error) {
	return m.streamFn(ctx, inputs, opts...)
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestLifecycleTool_Card(t *testing.T) {
	card := NewToolCard("test", "测试", nil, nil)
	inner := &mockTool{card: card}
	fw := runnnercallback.NewCallbackFramework()
	lt := NewLifecycleTool(inner, fw)

	if lt.Card().Name != "test" {
		t.Errorf("Card().Name = %q, want test", lt.Card().Name)
	}
}

func TestLifecycleTool_Invoke_成功(t *testing.T) {
	card := NewToolCard("weather", "查询天气", nil, nil)
	inner := &mockTool{
		card: card,
		invokeFn: func(_ context.Context, inputs map[string]any, _ ...ToolOption) (map[string]any, error) {
			return map[string]any{"result": "晴"}, nil
		},
	}

	fw := runnnercallback.NewCallbackFramework()
	var started, invokeInput, invokeOutput, finished int32

	fw.OnTool(runnnercallback.ToolCallStarted, func(_ context.Context, _ *runnnercallback.ToolCallEventData) {
		atomic.AddInt32(&started, 1)
	})
	fw.OnTool(runnnercallback.ToolInvokeInput, func(_ context.Context, _ *runnnercallback.ToolCallEventData) {
		atomic.AddInt32(&invokeInput, 1)
	})
	fw.OnTool(runnnercallback.ToolInvokeOutput, func(_ context.Context, _ *runnnercallback.ToolCallEventData) {
		atomic.AddInt32(&invokeOutput, 1)
	})
	fw.OnTool(runnnercallback.ToolCallFinished, func(_ context.Context, _ *runnnercallback.ToolCallEventData) {
		atomic.AddInt32(&finished, 1)
	})

	lt := NewLifecycleTool(inner, fw)
	result, err := lt.Invoke(context.Background(), map[string]any{"city": "北京"})
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	if result["result"] != "晴" {
		t.Errorf("result = %v, want 晴", result["result"])
	}

	if atomic.LoadInt32(&started) != 1 {
		t.Errorf("ToolCallStarted 触发 %d 次, want 1", started)
	}
	if atomic.LoadInt32(&invokeInput) != 1 {
		t.Errorf("ToolInvokeInput 触发 %d 次, want 1", invokeInput)
	}
	if atomic.LoadInt32(&invokeOutput) != 1 {
		t.Errorf("ToolInvokeOutput 触发 %d 次, want 1", invokeOutput)
	}
	if atomic.LoadInt32(&finished) != 1 {
		t.Errorf("ToolCallFinished 触发 %d 次, want 1", finished)
	}
}

func TestLifecycleTool_Invoke_错误(t *testing.T) {
	card := NewToolCard("bad_tool", "坏工具", nil, nil)
	inner := &mockTool{
		card: card,
		invokeFn: func(_ context.Context, _ map[string]any, _ ...ToolOption) (map[string]any, error) {
			return nil, errors.New("执行失败")
		},
	}

	fw := runnnercallback.NewCallbackFramework()
	var errEvent int32
	fw.OnTool(runnnercallback.ToolCallError, func(_ context.Context, data *runnnercallback.ToolCallEventData) {
		if data.Error == nil {
			t.Error("ToolCallError 事件缺少 Error")
		}
		atomic.AddInt32(&errEvent, 1)
	})

	lt := NewLifecycleTool(inner, fw)
	_, err := lt.Invoke(context.Background(), nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}

	if atomic.LoadInt32(&errEvent) != 1 {
		t.Errorf("ToolCallError 触发 %d 次, want 1", errEvent)
	}
}

func TestLifecycleTool_Stream_成功(t *testing.T) {
	card := NewToolCard("stream_tool", "流式工具", nil, nil)
	inner := &mockTool{
		card: card,
		streamFn: func(_ context.Context, _ map[string]any, _ ...ToolOption) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk, 3)
			ch <- StreamChunk{Data: map[string]any{"chunk": 1}}
			ch <- StreamChunk{Data: map[string]any{"chunk": 2}}
			ch <- StreamChunk{Done: true}
			close(ch)
			return ch, nil
		},
	}

	fw := runnnercallback.NewCallbackFramework()
	var resultReceived, streamOutput, finished int32
	fw.OnTool(runnnercallback.ToolResultReceived, func(_ context.Context, _ *runnnercallback.ToolCallEventData) {
		atomic.AddInt32(&resultReceived, 1)
	})
	fw.OnTool(runnnercallback.ToolStreamOutput, func(_ context.Context, _ *runnnercallback.ToolCallEventData) {
		atomic.AddInt32(&streamOutput, 1)
	})
	fw.OnTool(runnnercallback.ToolCallFinished, func(_ context.Context, _ *runnnercallback.ToolCallEventData) {
		atomic.AddInt32(&finished, 1)
	})

	lt := NewLifecycleTool(inner, fw)
	ch, err := lt.Stream(context.Background(), nil)
	if err != nil {
		t.Fatalf("Stream 返回错误: %v", err)
	}

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	if len(chunks) != 3 {
		t.Fatalf("chunk 数量 = %d, want 3", len(chunks))
	}
	if atomic.LoadInt32(&resultReceived) != 2 {
		t.Errorf("ToolResultReceived 触发 %d 次, want 2（仅数据块）", resultReceived)
	}
	if atomic.LoadInt32(&streamOutput) != 3 {
		t.Errorf("ToolStreamOutput 触发 %d 次, want 3（2 数据块 + 1 结束标记）", streamOutput)
	}
	if atomic.LoadInt32(&finished) != 1 {
		t.Errorf("ToolCallFinished 触发 %d 次, want 1", finished)
	}
}

func TestLifecycleTool_Stream_不支持(t *testing.T) {
	card := NewToolCard("no_stream", "不支持流式", nil, nil)
	inner := &mockTool{
		card: card,
		streamFn: func(_ context.Context, _ map[string]any, _ ...ToolOption) (<-chan StreamChunk, error) {
			return nil, NewErrStreamNotSupported(card.String())
		},
	}

	fw := runnnercallback.NewCallbackFramework()
	lt := NewLifecycleTool(inner, fw)
	_, err := lt.Stream(context.Background(), nil)
	if err == nil {
		t.Fatal("期望返回 ErrStreamNotSupported")
	}
}

func TestLifecycleTool_Stream_中途出错(t *testing.T) {
	card := NewToolCard("err_stream", "出错流式", nil, nil)
	inner := &mockTool{
		card: card,
		streamFn: func(_ context.Context, _ map[string]any, _ ...ToolOption) (<-chan StreamChunk, error) {
			ch := make(chan StreamChunk, 2)
			ch <- StreamChunk{Data: map[string]any{"chunk": 1}}
			ch <- StreamChunk{Error: errors.New("流中断")}
			close(ch)
			return ch, nil
		},
	}

	fw := runnnercallback.NewCallbackFramework()
	var errEvent int32
	fw.OnTool(runnnercallback.ToolCallError, func(_ context.Context, _ *runnnercallback.ToolCallEventData) {
		atomic.AddInt32(&errEvent, 1)
	})

	lt := NewLifecycleTool(inner, fw)
	ch, _ := lt.Stream(context.Background(), nil)

	for range ch {
		// 消费所有 chunk
	}

	if atomic.LoadInt32(&errEvent) != 1 {
		t.Errorf("ToolCallError 触发 %d 次, want 1", errEvent)
	}
}
