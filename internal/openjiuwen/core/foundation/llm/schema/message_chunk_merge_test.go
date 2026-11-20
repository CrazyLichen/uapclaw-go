package schema

import (
	"errors"
	"testing"
)

// TestMergeToolCalls_真实流式增量序列 用真 LLM 抓到的增量帧验证聚合语义。
// 真实模型 SSE 会把一次调用拆成：多个 name/id 为空、arguments 为 JSON 片段的帧，
// 最后才出现完整帧。对齐 Python message_chunk.py:164-200 的 __add__：同一调用
// （id 相同或任一方为空）应拼接 arguments、name 取非空侧，而不是追加新元素。
func TestMergeToolCalls_真实流式增量序列(t *testing.T) {
	fragments := []string{"/", "Test", "}", "Team", "2"}
	var calls []*ToolCall
	for _, f := range fragments {
		calls = mergeToolCalls(calls, []*ToolCall{{Type: "function", Arguments: f}})
	}
	if len(calls) != 1 {
		t.Fatalf("碎片应合并为 1 个调用，实际 %d", len(calls))
	}
	if calls[0].Arguments != "/Test}Team2" {
		t.Fatalf("arguments 应拼接，实际 %q", calls[0].Arguments)
	}

	// 完整帧补齐 id/name 后仍是同一调用
	calls = mergeToolCalls(calls, []*ToolCall{{ID: "call_00_x", Type: "function", Name: "list_members"}})
	if len(calls) != 1 {
		t.Fatalf("同一调用不应追加，实际 %d", len(calls))
	}
	if calls[0].Name != "list_members" || calls[0].ID != "call_00_x" {
		t.Fatalf("name/id 应取非空侧，实际 name=%q id=%q", calls[0].Name, calls[0].ID)
	}
}

// TestMerge_StreamError 锁定流中断错误在增量合并中的传播：
// 错误一旦出现就不能被后续内容掩盖，否则消费方会当成正常结束。
func TestMerge_StreamError(t *testing.T) {
	cause := errors.New("unexpected EOF")

	// 右侧带错误 → 合并结果带错误
	left := NewAssistantMessageChunk("半截内容")
	right := NewAssistantMessageChunk("", WithChunkStreamError(cause))
	merged := left.Merge(right)
	if merged.StreamError == nil {
		t.Fatal("Merge 应保留右侧的 StreamError")
	}

	// 左侧带错误、右侧是普通内容 → 错误不能被冲掉
	merged = right.Merge(NewAssistantMessageChunk("后续内容"))
	if merged.StreamError == nil {
		t.Fatal("StreamError 不应被后续 chunk 掩盖")
	}

	// 都无错误 → 结果无错误
	clean := NewAssistantMessageChunk("a").Merge(NewAssistantMessageChunk("b"))
	if clean.StreamError != nil {
		t.Fatalf("无错误时不应凭空产生 StreamError: %v", clean.StreamError)
	}
}
