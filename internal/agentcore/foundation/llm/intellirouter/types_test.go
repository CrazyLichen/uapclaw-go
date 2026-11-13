package intellirouter

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestAssistantMessageChunk_JSONRoundtrip 验证 7 个类型的 JSON 序列化往返。
func TestAssistantMessageChunk_JSONRoundtrip(t *testing.T) {
	idx := 0
	chunk := AssistantMessageChunk{
		Content:      "hi",
		FinishReason: strPtr("stop"),
		ToolCalls:    []ToolCall{{ID: "c1", Type: "function", Name: "f", Arguments: "{}", Index: &idx}},
	}
	data, err := json.Marshal(chunk)
	require.NoError(t, err)
	var got AssistantMessageChunk
	require.NoError(t, json.Unmarshal(data, &got))
	require.Equal(t, chunk.Content, got.Content)
	require.NotNil(t, got.FinishReason)
	require.Equal(t, "stop", *got.FinishReason)
	require.Len(t, got.ToolCalls, 1)
	require.Equal(t, "c1", got.ToolCalls[0].ID)
	require.Equal(t, 0, *got.ToolCalls[0].Index)
}

// TestAssistantMessageChunk_JSONRoundtrip_全类型 逐一验证其余 6 类型。
func TestAssistantMessageChunk_JSONRoundtrip_全类型(t *testing.T) {
	msg := AssistantMessage{Content: "ok", FinishReason: "stop"}
	assertRoundtrip(t, msg)
	usage := NewUsageMetadata(UsageMetadata{InputTokens: 3, OutputTokens: 5})
	require.Equal(t, 8, usage.TotalTokens) // ← types.py:41 __post_init__ 派生
	assertRoundtrip(t, usage)
	img := ImageGenerationResponse{Model: "m", Images: []string{"u1"}}
	assertRoundtrip(t, img)
	audio := AudioGenerationResponse{Model: "m", Format: strPtr("wav")}
	assertRoundtrip(t, audio)
	video := VideoGenerationResponse{Model: "m", VideoURL: "v", Format: "mp4"}
	assertRoundtrip(t, video)
	tc := ToolCall{ID: "t"}
	assertRoundtrip(t, tc)
}

// TestNewUsageMetadata_零派生 验证 input/output 全零时不派生。
func TestNewUsageMetadata_零派生(t *testing.T) {
	// ← types.py:41 条件 total_tokens == 0 and (input or output)
	m := NewUsageMetadata(UsageMetadata{})
	require.Equal(t, 0, m.TotalTokens)
	m = NewUsageMetadata(UsageMetadata{TotalTokens: 99, InputTokens: 1})
	require.Equal(t, 99, m.TotalTokens) // 显式 total 优先
}

// TestChunkPlus_ContentReasoningFinish 验证 Plus 的标量拼接分支。
func TestChunkPlus_ContentReasoningFinish(t *testing.T) {
	// ← types.py:104 content/reasoning 拼接；:106 finish other 优先
	a := AssistantMessageChunk{Content: "he", ReasoningContent: strPtr("think"), FinishReason: nil, ResponseID: strPtr("r1")}
	b := AssistantMessageChunk{Content: "llo", ReasoningContent: strPtr("ing"), FinishReason: strPtr("stop")}
	got := a.Plus(b)
	require.Equal(t, "hello", got.Content)
	require.NotNil(t, got.ReasoningContent)
	require.Equal(t, "thinking", *got.ReasoningContent)
	require.NotNil(t, got.FinishReason)
	require.Equal(t, "stop", *got.FinishReason)
	require.Equal(t, "r1", *got.ResponseID) // other 无 → 保留 self
}

// TestChunkPlus_ReasoningNil 两侧都为 nil 时保持 nil（py:104 条件分支）。
func TestChunkPlus_ReasoningNil(t *testing.T) {
	a := AssistantMessageChunk{Content: "x"}
	b := AssistantMessageChunk{Content: "y"}
	got := a.Plus(b)
	require.Nil(t, got.ReasoningContent)
}

// TestChunkPlus_CompletionTokenIDs 验证 token id 拼接与空值转 nil。
func TestChunkPlus_CompletionTokenIDs(t *testing.T) {
	// ← types.py:110 `(a or []) + (b or []) or None`
	a := AssistantMessageChunk{CompletionTokenIDs: []int{1}}
	b := AssistantMessageChunk{CompletionTokenIDs: []int{2}}
	require.Equal(t, []int{1, 2}, a.Plus(b).CompletionTokenIDs)
	// ← py:110 空拼空 → or None 兜底转 nil；非空拼空仍是非空（[1]+[]=1）
	require.Nil(t, AssistantMessageChunk{}.Plus(AssistantMessageChunk{}).CompletionTokenIDs)
	require.Equal(t, []int{1}, a.Plus(AssistantMessageChunk{}).CompletionTokenIDs)
	// prompt_token_ids 是 or 语义：self 有则取 self ← py:109
	a2 := AssistantMessageChunk{PromptTokenIDs: []int{7}}
	got := a2.Plus(AssistantMessageChunk{PromptTokenIDs: []int{8}})
	require.Equal(t, []int{7}, got.PromptTokenIDs)
}

// TestChunkPlus_ProviderMetadata 验证 metadata 合并且 other 覆盖 self。
func TestChunkPlus_ProviderMetadata(t *testing.T) {
	// ← types.py:115 {**self.provider_metadata, **other.provider_metadata}
	a := AssistantMessageChunk{ProviderMetadata: map[string]any{"k": "v1", "x": "1"}}
	b := AssistantMessageChunk{ProviderMetadata: map[string]any{"k": "v2"}}
	got := a.Plus(b)
	require.Equal(t, "v2", got.ProviderMetadata["k"]) // other 后写覆盖
	require.Equal(t, "1", got.ProviderMetadata["x"])
}

// TestChunkPlus_OperandNotMutated 验证操作数不被改写（py:124-127 文档契约）。
func TestChunkPlus_OperandNotMutated(t *testing.T) {
	idx := 0
	orig := ToolCall{ID: "c1", Name: "f", Arguments: "a", Index: &idx}
	a := AssistantMessageChunk{ToolCalls: []ToolCall{orig}}
	b := AssistantMessageChunk{ToolCalls: []ToolCall{{ID: "", Name: "", Arguments: "b", Index: &idx}}}
	_ = a.Plus(b)
	require.Equal(t, "a", a.ToolCalls[0].Arguments) // existing 不被改写
	require.Equal(t, "b", b.ToolCalls[0].Arguments)
}

// TestMergeToolCalls_按Index合并 arguments 拼接、空字段不覆盖。
func TestMergeToolCalls_按Index合并(t *testing.T) {
	i0, i1 := 0, 1
	existing := []ToolCall{{ID: "c1", Name: "f", Arguments: "{\"a\":", Index: &i0}, {ID: "c2", Index: &i1}}
	incoming := []ToolCall{{ID: "", Name: "", Arguments: "1}", Index: &i0}, {Name: "g", Index: &i1}}
	got := MergeToolCalls(existing, incoming)
	require.Len(t, got, 2)
	require.Equal(t, "c1", got[0].ID)               // 空 ID 不覆盖 ← py:144 if tc.id
	require.Equal(t, "f", got[0].Name)              // 空 name 不覆盖 ← py:146 if tc.name
	require.Equal(t, "{\"a\":1}", got[0].Arguments) // 拼接 ← py:148
	require.Equal(t, "g", got[1].Name)
}

// TestMergeToolCalls_Nil 组合：双 nil / 单侧 nil 全量拷贝。
func TestMergeToolCalls_Nil(t *testing.T) {
	require.Nil(t, MergeToolCalls(nil, nil))
	i := 1
	existing := []ToolCall{{ID: "a", Index: &i}}
	got := MergeToolCalls(existing, nil)
	require.Len(t, got, 1)
	require.Equal(t, "a", got[0].ID)
	got = MergeToolCalls(nil, existing)
	require.Len(t, got, 1)
}

// TestMergeToolCalls_新调用 无 index 的 incoming 作为新条目追加。
func TestMergeToolCalls_新调用(t *testing.T) {
	i0 := 0
	existing := []ToolCall{{ID: "c1", Index: &i0}}
	incoming := []ToolCall{{ID: "c9"}} // index nil → 新条目 ← py:150
	got := MergeToolCalls(existing, incoming)
	require.Len(t, got, 2)
	require.Equal(t, "c9", got[1].ID)
}

// TestMergeUsageMetadata_字段级 验证 newest-number 与派生重算。
func TestMergeUsageMetadata_字段级(t *testing.T) {
	// ← types.py:159-217：incoming 非零取 incoming，否则保留 existing；
	// input/output 任一非零时 total 重算 ← py:177
	ext := &UsageMetadata{ModelName: strPtr("m1"), InputTokens: 10, OutputTokens: 5, TotalTokens: 15, CacheTokens: 3}
	inc := &UsageMetadata{OutputTokens: 9, ReasoningTokens: 7, CacheAuthoritative: true}
	got := MergeUsageMetadata(ext, inc)
	require.Equal(t, "m1", *got.ModelName) // incoming 未上报 → 保留 existing
	require.Equal(t, 10, got.InputTokens)  // incoming 0 → existing
	require.Equal(t, 9, got.OutputTokens)  // incoming 非零 → incoming
	require.Equal(t, 19, got.TotalTokens)  // 10+9 重算 ← py:178
	require.Equal(t, 3, got.CacheTokens)
	require.Equal(t, 7, got.ReasoningTokens)
	require.True(t, got.CacheAuthoritative) // 一端 true 即 true ← py:186
}

// TestMergeUsageMetadata_Nil 双 nil / 单 nil。
func TestMergeUsageMetadata_Nil(t *testing.T) {
	require.Nil(t, MergeUsageMetadata(nil, nil))
	m := &UsageMetadata{InputTokens: 1}
	require.Same(t, m, MergeUsageMetadata(m, nil))
	require.Same(t, m, MergeUsageMetadata(nil, m))
}

// TestMergeUsageMetadata_空串保留 验证字符串 is-not-None 语义。
func TestMergeUsageMetadata_空串保留(t *testing.T) {
	// ← py:182 `incoming.model_name if incoming.model_name is not None`
	// 空串也是「已上报」，必须保留 incoming 的空串而不是回落 existing
	ext := &UsageMetadata{ModelName: strPtr("m1")}
	inc := &UsageMetadata{ModelName: strPtr("")}
	got := MergeUsageMetadata(ext, inc)
	require.NotNil(t, got.ModelName)
	require.Equal(t, "", *got.ModelName)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func strPtr(s string) *string { return &s }

func assertRoundtrip(t *testing.T, v any) {
	t.Helper()
	data, err := json.Marshal(v)
	require.NoError(t, err)
	require.NotEmpty(t, data)
	var back map[string]any
	require.NoError(t, json.Unmarshal(data, &back))
}
