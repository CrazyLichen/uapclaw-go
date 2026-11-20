package tool

import (
	"context"
	"testing"
)

// ──────────────────────────── 测试用函数 ────────────────────────────

// streamSearchFunc 流式搜索函数
func streamSearchFunc(ctx context.Context, input searchInput, opts ...ToolOption) (<-chan searchOutput, error) {
	ch := make(chan searchOutput, 2)
	go func() {
		defer close(ch)
		ch <- searchOutput{Results: []string{input.Query + "_1"}, Total: 1}
		ch <- searchOutput{Results: []string{input.Query + "_2"}, Total: 2}
	}()
	return ch, nil
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestNewStreamFunction_自动推断 测试自动推断
func TestNewStreamFunction_自动推断(t *testing.T) {
	fn, err := NewStreamFunction("stream_search", streamSearchFunc)
	if err != nil {
		t.Fatalf("NewStreamFunction 失败: %v", err)
	}
	if fn.Card().Name != "stream_search" {
		t.Errorf("Name: 期望 stream_search，实际 %q", fn.Card().Name)
	}
}

// TestStreamFunction_Stream_完整流程 测试流式完整流程
func TestStreamFunction_Stream_完整流程(t *testing.T) {
	fn, _ := NewStreamFunction("stream_search", streamSearchFunc)
	ch, err := fn.Stream(context.Background(), map[string]any{
		"query": "hello",
	})
	if err != nil {
		t.Fatalf("Stream 失败: %v", err)
	}

	var chunks []StreamChunk
	for chunk := range ch {
		chunks = append(chunks, chunk)
	}

	// 应收到 2 个数据块 + 1 个 Done
	dataChunks := 0
	doneChunks := 0
	for _, c := range chunks {
		if c.Done {
			doneChunks++
		} else if c.Error != nil {
			t.Fatalf("意外错误: %v", c.Error)
		} else {
			dataChunks++
		}
	}
	if dataChunks != 2 {
		t.Errorf("数据块: 期望 2，实际 %d", dataChunks)
	}
	if doneChunks != 1 {
		t.Errorf("Done 块: 期望 1，实际 %d", doneChunks)
	}
}

// TestStreamFunction_Invoke_不支持 测试 Stream 模式的 Invoke 返回错误
func TestStreamFunction_Invoke_不支持(t *testing.T) {
	fn, _ := NewStreamFunction("stream_search", streamSearchFunc)
	_, err := fn.Invoke(context.Background(), map[string]any{"query": "test"})
	if err == nil {
		t.Error("Stream 模式 Invoke 应返回 ErrInvokeNotSupported")
	}
}
