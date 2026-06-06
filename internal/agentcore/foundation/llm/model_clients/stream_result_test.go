package model_clients

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestStreamResult_Final_MergeChunks 测试 Final 合并多个 chunk。
func TestStreamResult_Final_MergeChunks(t *testing.T) {
	ch := make(chan *llmschema.AssistantMessageChunk, 3)

	// 推送 3 个 chunk
	ch <- llmschema.NewAssistantMessageChunk("你")
	ch <- llmschema.NewAssistantMessageChunk("好")
	ch <- llmschema.NewAssistantMessageChunk("！")
	close(ch)

	result := NewStreamResult(ch)
	final := result.Final()

	if final == nil {
		t.Fatal("Final() 不应为 nil")
	}
	if final.Content.Text() != "你好！" {
		t.Errorf("Final Content = %q, 期望 %q", final.Content.Text(), "你好！")
	}
}

// TestStreamResult_Final_ToolCallsMerge 测试 Final 合并带 tool_calls 的 chunk。
func TestStreamResult_Final_ToolCallsMerge(t *testing.T) {
	ch := make(chan *llmschema.AssistantMessageChunk, 2)

	// 第一个 chunk：tool_call name
	ch <- llmschema.NewAssistantMessageChunk("", llmschema.WithChunkToolCalls([]*llmschema.ToolCall{
		llmschema.NewToolCall("call_1", "get_weather", ""),
	}))
	// 第二个 chunk：tool_call arguments
	ch <- llmschema.NewAssistantMessageChunk("", llmschema.WithChunkToolCalls([]*llmschema.ToolCall{
		llmschema.NewToolCall("", "", `{"city":"Beijing"}`),
	}))
	close(ch)

	result := NewStreamResult(ch)
	final := result.Final()

	if len(final.ToolCalls) != 1 {
		t.Fatalf("ToolCalls 数量 = %d, 期望 1", len(final.ToolCalls))
	}
	if final.ToolCalls[0].Name != "get_weather" {
		t.Errorf("ToolCall Name = %q, 期望 %q", final.ToolCalls[0].Name, "get_weather")
	}
	if final.ToolCalls[0].Arguments != `{"city":"Beijing"}` {
		t.Errorf("ToolCall Arguments = %q, 期望 %q", final.ToolCalls[0].Arguments, `{"city":"Beijing"}`)
	}
}

// TestStreamResult_Final_EmptyChannel 测试空 channel 的 Final。
func TestStreamResult_Final_EmptyChannel(t *testing.T) {
	ch := make(chan *llmschema.AssistantMessageChunk)
	close(ch)

	result := NewStreamResult(ch)
	final := result.Final()

	if final != nil {
		t.Error("空 channel 的 Final() 应为 nil")
	}
}

// TestStreamResult_Final_FinishReason 测试 Final 合并 finish_reason。
func TestStreamResult_Final_FinishReason(t *testing.T) {
	ch := make(chan *llmschema.AssistantMessageChunk, 2)

	// 第一个 chunk：content，finish_reason 仍为 "null"
	ch <- llmschema.NewAssistantMessageChunk("hello")
	// 第二个 chunk：finish_reason 变为 "stop"
	ch <- llmschema.NewAssistantMessageChunk("", llmschema.WithChunkFinishReason("stop"))
	close(ch)

	result := NewStreamResult(ch)
	final := result.Final()

	if final.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, 期望 %q", final.FinishReason, "stop")
	}
	if final.Content.Text() != "hello" {
		t.Errorf("Content = %q, 期望 %q", final.Content.Text(), "hello")
	}
}

// TestStreamResult_Final_CalledTwice 测试多次调用 Final 返回同一结果。
func TestStreamResult_Final_CalledTwice(t *testing.T) {
	ch := make(chan *llmschema.AssistantMessageChunk, 1)
	ch <- llmschema.NewAssistantMessageChunk("test")
	close(ch)

	result := NewStreamResult(ch)
	final1 := result.Final()
	final2 := result.Final()

	if final1 != final2 {
		t.Error("多次调用 Final() 应返回同一指针")
	}
}
