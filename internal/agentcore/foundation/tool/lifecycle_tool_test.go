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

	fw.OnTool(runnnercallback.ToolCallStarted, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		atomic.AddInt32(&started, 1)
		return nil
	})
	fw.OnTool(runnnercallback.ToolInvokeInput, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		atomic.AddInt32(&invokeInput, 1)
		return nil
	})
	fw.OnTool(runnnercallback.ToolInvokeOutput, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		atomic.AddInt32(&invokeOutput, 1)
		return nil
	})
	fw.OnTool(runnnercallback.ToolCallFinished, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		atomic.AddInt32(&finished, 1)
		return nil
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
	fw.OnTool(runnnercallback.ToolCallError, func(_ context.Context, data *runnnercallback.ToolCallEventData) any {
		if data.Error == nil {
			t.Error("ToolCallError 事件缺少 Error")
		}
		atomic.AddInt32(&errEvent, 1)
		return nil
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
	fw.OnTool(runnnercallback.ToolResultReceived, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		atomic.AddInt32(&resultReceived, 1)
		return nil
	})
	fw.OnTool(runnnercallback.ToolStreamOutput, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		atomic.AddInt32(&streamOutput, 1)
		return nil
	})
	fw.OnTool(runnnercallback.ToolCallFinished, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		atomic.AddInt32(&finished, 1)
		return nil
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
	fw.OnTool(runnnercallback.ToolCallError, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		atomic.AddInt32(&errEvent, 1)
		return nil
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

// ──────────────────────────── TransformIO 测试 ────────────────────────────

func TestLifecycleTool_Invoke_TransformIO(t *testing.T) {
	card := NewToolCard("transform_tool", "变换工具", nil, nil)
	inner := &mockTool{
		card: card,
		invokeFn: func(_ context.Context, inputs map[string]any, _ ...ToolOption) (map[string]any, error) {
			return map[string]any{"echo": inputs["msg"]}, nil
		},
	}

	fw := runnnercallback.NewCallbackFramework()
	// 注册 TransformIO：输入加前缀，输出加后缀
	fw.RegisterToolTransformIO(
		runnnercallback.ToolInvokeInput, runnnercallback.ToolInvokeOutput,
		func(_ context.Context, _ runnnercallback.ToolCallEventType, input map[string]any) map[string]any {
			input["msg"] = "prefix_" + input["msg"].(string)
			return input
		},
		func(_ context.Context, _ runnnercallback.ToolCallEventType, output map[string]any) map[string]any {
			output["echo"] = output["echo"].(string) + "_suffix"
			return output
		},
	)

	lt := NewLifecycleTool(inner, fw)
	result, err := lt.Invoke(context.Background(), map[string]any{"msg": "hello"})
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}
	// 输入变换：msg = "prefix_hello"，inner 返回 echo = "prefix_hello"
	// 输出变换：echo = "prefix_hello_suffix"
	if result["echo"] != "prefix_hello_suffix" {
		t.Errorf("result[echo] = %v, want prefix_hello_suffix", result["echo"])
	}
}

func TestLifecycleTool_Invoke_事件顺序对齐Python(t *testing.T) {
	card := NewToolCard("order_tool", "顺序工具", nil, nil)
	inner := &mockTool{
		card: card,
		invokeFn: func(_ context.Context, _ map[string]any, _ ...ToolOption) (map[string]any, error) {
			return map[string]any{"ok": true}, nil
		},
	}

	fw := runnnercallback.NewCallbackFramework()
	var order []string
	fw.OnTool(runnnercallback.ToolInvokeInput, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		order = append(order, "INVOKE_INPUT")
		return nil
	})
	fw.OnTool(runnnercallback.ToolCallStarted, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		order = append(order, "STARTED")
		return nil
	})
	fw.OnTool(runnnercallback.ToolCallFinished, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		order = append(order, "FINISHED")
		return nil
	})
	fw.OnTool(runnnercallback.ToolInvokeOutput, func(_ context.Context, _ *runnnercallback.ToolCallEventData) any {
		order = append(order, "INVOKE_OUTPUT")
		return nil
	})

	lt := NewLifecycleTool(inner, fw)
	_, err := lt.Invoke(context.Background(), nil)
	if err != nil {
		t.Fatalf("Invoke 返回错误: %v", err)
	}

	expected := []string{"INVOKE_INPUT", "STARTED", "FINISHED", "INVOKE_OUTPUT"}
	if len(order) != len(expected) {
		t.Fatalf("事件数 = %d, want %d; order = %v", len(order), len(expected), order)
	}
	for i, e := range expected {
		if order[i] != e {
			t.Errorf("事件[%d] = %s, want %s", i, order[i], e)
		}
	}
}

func TestNewLifecycleTool_自动获取全局fw(t *testing.T) {
	card := NewToolCard("auto_fw", "自动fw", nil, nil)
	inner := &mockTool{
		card: card,
		invokeFn: func(_ context.Context, inputs map[string]any, _ ...ToolOption) (map[string]any, error) {
			return inputs, nil
		},
	}

	// 不传 fw，应自动使用全局回调框架
	lt := NewLifecycleTool(inner)
	if lt.fw == nil {
		t.Error("fw 不应为 nil")
	}
	if lt.fw != runnnercallback.GetCallbackFramework() {
		t.Error("fw 应为全局回调框架")
	}
}
