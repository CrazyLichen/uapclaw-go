package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────── 辅助函数测试 ────────────────────

// TestMergeContent_BothText 验证两纯文本 → 字符串拼接。
func TestMergeContent_BothText(t *testing.T) {
	left := NewTextContent("你好")
	right := NewTextContent("世界")
	result := mergeContent(left, right)
	if !result.IsText() {
		t.Fatal("合并结果应为纯文本")
	}
	if result.Text() != "你好世界" {
		t.Errorf("Text() = %q, want %q", result.Text(), "你好世界")
	}
}

// TestMergeContent_BothMultiModal 验证两多模态 → Parts 拼接。
func TestMergeContent_BothMultiModal(t *testing.T) {
	left := NewMultiModalContent(ContentPart{Type: "text", Text: "A"})
	right := NewMultiModalContent(ContentPart{Type: "text", Text: "B"})
	result := mergeContent(left, right)
	if result.IsText() {
		t.Fatal("合并结果应为多模态")
	}
	if len(result.Parts()) != 2 {
		t.Fatalf("Parts() 长度 = %d, want 2", len(result.Parts()))
	}
	if result.Parts()[0].Text != "A" {
		t.Errorf("Parts()[0].Text = %q, want %q", result.Parts()[0].Text, "A")
	}
	if result.Parts()[1].Text != "B" {
		t.Errorf("Parts()[1].Text = %q, want %q", result.Parts()[1].Text, "B")
	}
}

// TestMergeContent_TextAndMultiModal 验证类型不同 → 取右侧。
func TestMergeContent_TextAndMultiModal(t *testing.T) {
	left := NewTextContent("文本")
	right := NewMultiModalContent(ContentPart{Type: "text", Text: "多模态"})
	result := mergeContent(left, right)
	if result.IsText() {
		t.Fatal("类型不同时应取右侧（多模态）")
	}
	if len(result.Parts()) != 1 || result.Parts()[0].Text != "多模态" {
		t.Errorf("合并结果不正确，应为右侧多模态内容")
	}
}

// TestMergeContent_MultiModalAndText 验证多模态+文本 → 取右侧（纯文本）。
func TestMergeContent_MultiModalAndText(t *testing.T) {
	left := NewMultiModalContent(ContentPart{Type: "text", Text: "多模态"})
	right := NewTextContent("纯文本")
	result := mergeContent(left, right)
	if !result.IsText() {
		t.Fatal("类型不同时应取右侧（纯文本）")
	}
	if result.Text() != "纯文本" {
		t.Errorf("Text() = %q, want %q", result.Text(), "纯文本")
	}
}

// TestMergeContent_EmptyLeft 验证左侧空文本 + 右侧文本 → 字符串拼接。
func TestMergeContent_EmptyLeft(t *testing.T) {
	left := NewTextContent("")
	right := NewTextContent("hello")
	result := mergeContent(left, right)
	if !result.IsText() {
		t.Fatal("合并结果应为纯文本")
	}
	if result.Text() != "hello" {
		t.Errorf("Text() = %q, want %q", result.Text(), "hello")
	}
}

// TestMergeContent_EmptyRight 验证左侧文本 + 右侧空文本 → 字符串拼接。
func TestMergeContent_EmptyRight(t *testing.T) {
	left := NewTextContent("hello")
	right := NewTextContent("")
	result := mergeContent(left, right)
	if !result.IsText() {
		t.Fatal("合并结果应为纯文本")
	}
	if result.Text() != "hello" {
		t.Errorf("Text() = %q, want %q", result.Text(), "hello")
	}
}

// TestMergeParserContent_BothString 验证字符串拼接。
func TestMergeParserContent_BothString(t *testing.T) {
	result := mergeParserContent("hello", " world")
	if result != "hello world" {
		t.Errorf("mergeParserContent(string, string) = %v, want %q", result, "hello world")
	}
}

// TestMergeParserContent_BothSlice 验证列表拼接。
func TestMergeParserContent_BothSlice(t *testing.T) {
	left := []any{1, 2}
	right := []any{3, 4}
	result := mergeParserContent(left, right)
	resultSlice, ok := result.([]any)
	if !ok {
		t.Fatal("合并结果应为 []any")
	}
	if len(resultSlice) != 4 {
		t.Fatalf("合并后长度 = %d, want 4", len(resultSlice))
	}
}

// TestMergeParserContent_BothMap 验证字典递归合并。
func TestMergeParserContent_BothMap(t *testing.T) {
	left := map[string]any{"a": "hello", "b": []any{1}}
	right := map[string]any{"a": " world", "b": []any{2}, "c": "new"}
	result := mergeParserContent(left, right)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("合并结果应为 map[string]any")
	}
	// a: 字符串拼接
	if resultMap["a"] != "hello world" {
		t.Errorf("a = %v, want %q", resultMap["a"], "hello world")
	}
	// b: 列表拼接
	bSlice, ok := resultMap["b"].([]any)
	if !ok || len(bSlice) != 2 {
		t.Errorf("b 应为长度 2 的 []any，实际: %v", resultMap["b"])
	}
	// c: 新键
	if resultMap["c"] != "new" {
		t.Errorf("c = %v, want %q", resultMap["c"], "new")
	}
}

// TestMergeParserContent_NilLeft 验证 left 为 nil → 取 right。
func TestMergeParserContent_NilLeft(t *testing.T) {
	result := mergeParserContent(nil, "value")
	if result != "value" {
		t.Errorf("mergeParserContent(nil, string) = %v, want %q", result, "value")
	}
}

// TestMergeParserContent_NilRight 验证 right 为 nil → 取 left。
func TestMergeParserContent_NilRight(t *testing.T) {
	result := mergeParserContent("value", nil)
	if result != "value" {
		t.Errorf("mergeParserContent(string, nil) = %v, want %q", result, "value")
	}
}

// TestMergeParserContent_DifferentTypes 验证类型不同 → 取 right。
func TestMergeParserContent_DifferentTypes(t *testing.T) {
	result := mergeParserContent("string", 42)
	if result != 42 {
		t.Errorf("mergeParserContent(string, int) = %v, want 42", result)
	}
}

// TestMergeParserContent_MultipleParseSuccess 验证多次解析成功不丢失（核心场景）。
//
// 模拟流式场景：两次 JsonOutputParser 解析成功，parser_content 均为 dict，
// Go 实现通过字典递归合并保留所有结果，比 Python 的 `or` 更正确。
func TestMergeParserContent_MultipleParseSuccess(t *testing.T) {
	left := map[string]any{
		"blocks": []any{
			map[string]any{"block_id": "1", "summary": "你好"},
		},
	}
	right := map[string]any{
		"blocks": []any{
			map[string]any{"block_id": "2", "summary": "再见"},
		},
	}
	result := mergeParserContent(left, right)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("合并结果应为 map[string]any")
	}
	blocks, ok := resultMap["blocks"].([]any)
	if !ok {
		t.Fatal("blocks 应为 []any")
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks 长度 = %d, want 2（两次解析成功均保留）", len(blocks))
	}
}

// TestMergeDicts_StringValues 验证同键 string → 拼接。
func TestMergeDicts_StringValues(t *testing.T) {
	left := map[string]any{"key": "hello"}
	right := map[string]any{"key": " world"}
	result := mergeDicts(left, right)
	if result["key"] != "hello world" {
		t.Errorf("key = %v, want %q", result["key"], "hello world")
	}
}

// TestMergeDicts_ListValues 验证同键 list → 拼接。
func TestMergeDicts_ListValues(t *testing.T) {
	left := map[string]any{"items": []any{1, 2}}
	right := map[string]any{"items": []any{3, 4}}
	result := mergeDicts(left, right)
	items, ok := result["items"].([]any)
	if !ok {
		t.Fatal("items 应为 []any")
	}
	if len(items) != 4 {
		t.Fatalf("items 长度 = %d, want 4", len(items))
	}
}

// TestMergeDicts_NestedDicts 验证同键 dict → 递归合并。
func TestMergeDicts_NestedDicts(t *testing.T) {
	left := map[string]any{
		"nested": map[string]any{"a": "hello", "b": "left"},
	}
	right := map[string]any{
		"nested": map[string]any{"a": " world", "c": "right"},
	}
	result := mergeDicts(left, right)
	nested, ok := result["nested"].(map[string]any)
	if !ok {
		t.Fatal("nested 应为 map[string]any")
	}
	if nested["a"] != "hello world" {
		t.Errorf("nested.a = %v, want %q", nested["a"], "hello world")
	}
	if nested["b"] != "left" {
		t.Errorf("nested.b = %v, want %q", nested["b"], "left")
	}
	if nested["c"] != "right" {
		t.Errorf("nested.c = %v, want %q", nested["c"], "right")
	}
}

// TestMergeDicts_DifferentValueTypes 验证同键不同类型 → 取 right。
func TestMergeDicts_DifferentValueTypes(t *testing.T) {
	left := map[string]any{"key": "string"}
	right := map[string]any{"key": 42}
	result := mergeDicts(left, right)
	if result["key"] != 42 {
		t.Errorf("key = %v, want 42", result["key"])
	}
}

// TestMergeDicts_NewKeys 验证 right 独有键 → 直接加入。
func TestMergeDicts_NewKeys(t *testing.T) {
	left := map[string]any{"a": 1}
	right := map[string]any{"b": 2}
	result := mergeDicts(left, right)
	if result["a"] != 1 {
		t.Errorf("a = %v, want 1", result["a"])
	}
	if result["b"] != 2 {
		t.Errorf("b = %v, want 2", result["b"])
	}
}

// TestConcatTokenIDs 验证列表拼接。
func TestConcatTokenIDs(t *testing.T) {
	result := concatTokenIDs([]int{1, 2}, []int{3, 4})
	if len(result) != 4 {
		t.Fatalf("长度 = %d, want 4", len(result))
	}
	expected := []int{1, 2, 3, 4}
	for i, v := range expected {
		if result[i] != v {
			t.Errorf("result[%d] = %d, want %d", i, result[i], v)
		}
	}
}

// TestConcatTokenIDs_EmptyLeft 验证左侧空 → 取右侧。
func TestConcatTokenIDs_EmptyLeft(t *testing.T) {
	result := concatTokenIDs(nil, []int{3, 4})
	if len(result) != 2 {
		t.Fatalf("长度 = %d, want 2", len(result))
	}
}

// TestConcatTokenIDs_EmptyRight 验证右侧空 → 取左侧。
func TestConcatTokenIDs_EmptyRight(t *testing.T) {
	result := concatTokenIDs([]int{1, 2}, nil)
	if len(result) != 2 {
		t.Fatalf("长度 = %d, want 2", len(result))
	}
}

// TestConcatTokenIDs_BothEmpty 验证两侧均空 → nil。
func TestConcatTokenIDs_BothEmpty(t *testing.T) {
	result := concatTokenIDs(nil, nil)
	if result != nil {
		t.Errorf("两侧均空时应返回 nil，实际: %v", result)
	}
}

// TestMergeLogprobs_DictMerge 验证 dict 中列表字段拼接。
func TestMergeLogprobs_DictMerge(t *testing.T) {
	left := map[string]any{
		"content": []any{map[string]any{"token": "hello"}},
	}
	right := map[string]any{
		"content": []any{map[string]any{"token": "world"}},
	}
	result := mergeLogprobs(left, right)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("合并结果应为 map[string]any")
	}
	content, ok := resultMap["content"].([]any)
	if !ok {
		t.Fatal("content 应为 []any")
	}
	if len(content) != 2 {
		t.Fatalf("content 长度 = %d, want 2", len(content))
	}
}

// TestMergeLogprobs_NilLeft 验证左 nil → 取 right。
func TestMergeLogprobs_NilLeft(t *testing.T) {
	right := map[string]any{"key": "val"}
	result := mergeLogprobs(nil, right)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
}

// TestMergeLogprobs_NilRight 验证右 nil → 取 left。
func TestMergeLogprobs_NilRight(t *testing.T) {
	left := map[string]any{"key": "val"}
	result := mergeLogprobs(left, nil)
	if result == nil {
		t.Fatal("结果不应为 nil")
	}
}

// TestMergeLogprobs_ListMerge 验证两者均为 []any → 列表拼接。
func TestMergeLogprobs_ListMerge(t *testing.T) {
	left := []any{1, 2}
	right := []any{3, 4}
	result := mergeLogprobs(left, right)
	resultSlice, ok := result.([]any)
	if !ok {
		t.Fatal("合并结果应为 []any")
	}
	if len(resultSlice) != 4 {
		t.Fatalf("合并后长度 = %d, want 4", len(resultSlice))
	}
}

// TestMergeLogprobs_DictNonListKey 验证 dict 中非列表键取右侧值。
func TestMergeLogprobs_DictNonListKey(t *testing.T) {
	left := map[string]any{"key": "left"}
	right := map[string]any{"key": "right"}
	result := mergeLogprobs(left, right)
	resultMap, ok := result.(map[string]any)
	if !ok {
		t.Fatal("合并结果应为 map[string]any")
	}
	if resultMap["key"] != "right" {
		t.Errorf("key = %v, want %q", resultMap["key"], "right")
	}
}

// ──────────────────── mergeToolCalls 测试 ────────────────────

// TestMergeToolCalls_BothEmpty 验证两空 → nil。
func TestMergeToolCalls_BothEmpty(t *testing.T) {
	result := mergeToolCalls(nil, nil)
	if result != nil {
		t.Errorf("两空时应返回 nil，实际: %v", result)
	}
}

// TestMergeToolCalls_LeftEmpty 验证左空 → 右侧全部追加。
func TestMergeToolCalls_LeftEmpty(t *testing.T) {
	right := []*ToolCall{NewToolCall("id1", "fn1", `{"a":1}`)}
	result := mergeToolCalls(nil, right)
	if len(result) != 1 {
		t.Fatalf("长度 = %d, want 1", len(result))
	}
	if result[0].Name != "fn1" {
		t.Errorf("Name = %q, want %q", result[0].Name, "fn1")
	}
}

// TestMergeToolCalls_RightEmpty 验证右空 → 保持左侧。
func TestMergeToolCalls_RightEmpty(t *testing.T) {
	left := []*ToolCall{NewToolCall("id1", "fn1", `{"a":1}`)}
	result := mergeToolCalls(left, nil)
	if len(result) != 1 {
		t.Fatalf("长度 = %d, want 1", len(result))
	}
	if result[0].Name != "fn1" {
		t.Errorf("Name = %q, want %q", result[0].Name, "fn1")
	}
}

// TestMergeToolCalls_SameIDMerge 验证 id 相同 → 合并为一个（arguments 拼接）。
func TestMergeToolCalls_SameIDMerge(t *testing.T) {
	left := []*ToolCall{NewToolCall("call_1", "search", `{"qu`, WithToolCallIndex(0))}
	right := []*ToolCall{NewToolCall("call_1", "", `ery":"test"}`)}
	result := mergeToolCalls(left, right)
	if len(result) != 1 {
		t.Fatalf("合并后长度 = %d, want 1", len(result))
	}
	if result[0].ID != "call_1" {
		t.Errorf("ID = %q, want %q", result[0].ID, "call_1")
	}
	if result[0].Name != "search" {
		t.Errorf("Name = %q, want %q", result[0].Name, "search")
	}
	if result[0].Arguments != `{"query":"test"}` {
		t.Errorf("Arguments = %q, want %q", result[0].Arguments, `{"query":"test"}`)
	}
	if result[0].Index != 0 {
		t.Errorf("Index = %d, want 0", result[0].Index)
	}
}

// TestMergeToolCalls_EmptyIDMerge 验证某一方 id 为空 + type 均为 function → 合并。
func TestMergeToolCalls_EmptyIDMerge(t *testing.T) {
	left := []*ToolCall{NewToolCall("", "search", `{"qu`)}
	right := []*ToolCall{NewToolCall("call_1", "", `ery":"test"}`)}
	result := mergeToolCalls(left, right)
	if len(result) != 1 {
		t.Fatalf("合并后长度 = %d, want 1", len(result))
	}
	if result[0].ID != "call_1" {
		t.Errorf("ID = %q, want %q（取非空值）", result[0].ID, "call_1")
	}
	if result[0].Name != "search" {
		t.Errorf("Name = %q, want %q（取左侧非空值）", result[0].Name, "search")
	}
	if result[0].Arguments != `{"query":"test"}` {
		t.Errorf("Arguments = %q, want %q", result[0].Arguments, `{"query":"test"}`)
	}
}

// TestMergeToolCalls_DifferentIDAppend 验证 id 不同 → 作为新元素追加。
func TestMergeToolCalls_DifferentIDAppend(t *testing.T) {
	left := []*ToolCall{NewToolCall("call_1", "search", `{"q":"1"}`)}
	right := []*ToolCall{NewToolCall("call_2", "calculate", `{"e":"2"}`)}
	result := mergeToolCalls(left, right)
	if len(result) != 2 {
		t.Fatalf("合并后长度 = %d, want 2", len(result))
	}
	if result[0].ID != "call_1" {
		t.Errorf("result[0].ID = %q, want %q", result[0].ID, "call_1")
	}
	if result[1].ID != "call_2" {
		t.Errorf("result[1].ID = %q, want %q", result[1].ID, "call_2")
	}
}

// TestMergeToolCalls_NonFunctionType 验证 type 不为 function → 不合并，追加。
func TestMergeToolCalls_NonFunctionType(t *testing.T) {
	left := []*ToolCall{{ID: "id1", Type: "custom", Name: "fn1", Arguments: "a"}}
	right := []*ToolCall{{ID: "id1", Type: "custom", Name: "fn2", Arguments: "b"}}
	result := mergeToolCalls(left, right)
	if len(result) != 2 {
		t.Fatalf("非 function type 不应合并，长度 = %d, want 2", len(result))
	}
}

// TestMergeToolCalls_NameMerge 验证合并时 name 取左侧非空值。
func TestMergeToolCalls_NameMerge(t *testing.T) {
	// 左侧 name 为空，右侧 name 有值 → 取右侧
	left := []*ToolCall{NewToolCall("id1", "", "arg1")}
	right := []*ToolCall{NewToolCall("id1", "search", "arg2")}
	result := mergeToolCalls(left, right)
	if len(result) != 1 {
		t.Fatalf("合并后长度 = %d, want 1", len(result))
	}
	if result[0].Name != "search" {
		t.Errorf("Name = %q, want %q（左侧为空取右侧）", result[0].Name, "search")
	}
}

// TestMergeToolCalls_IndexPreserved 验证合并时 index 取左侧值。
func TestMergeToolCalls_IndexPreserved(t *testing.T) {
	left := []*ToolCall{NewToolCall("id1", "fn", "a", WithToolCallIndex(2))}
	right := []*ToolCall{NewToolCall("id1", "", "b", WithToolCallIndex(5))}
	result := mergeToolCalls(left, right)
	if len(result) != 1 {
		t.Fatalf("合并后长度 = %d, want 1", len(result))
	}
	if result[0].Index != 2 {
		t.Errorf("Index = %d, want 2（取左侧值）", result[0].Index)
	}
}

// TestMergeToolCalls_MultipleFragmentsSameCall 验证多个 fragment 逐步合并到同一 ToolCall。
func TestMergeToolCalls_MultipleFragmentsSameCall(t *testing.T) {
	// 模拟流式场景：一个 tool call 的 arguments 分三个 chunk 到达
	frag1 := []*ToolCall{{ID: "", Type: "function", Name: "search", Arguments: `{"qu`, Index: 0}}
	frag2 := []*ToolCall{{ID: "", Type: "function", Name: "", Arguments: `ery":`}}
	frag3 := []*ToolCall{{ID: "call_123", Type: "function", Name: "", Arguments: `"test"}`}}

	// 逐步合并
	result := mergeToolCalls(frag1, frag2)
	result = mergeToolCalls(result, frag3)

	if len(result) != 1 {
		t.Fatalf("最终合并后长度 = %d, want 1", len(result))
	}
	if result[0].ID != "call_123" {
		t.Errorf("ID = %q, want %q", result[0].ID, "call_123")
	}
	if result[0].Name != "search" {
		t.Errorf("Name = %q, want %q", result[0].Name, "search")
	}
	if result[0].Arguments != `{"query":"test"}` {
		t.Errorf("Arguments = %q, want %q", result[0].Arguments, `{"query":"test"}`)
	}
}

// TestMergeToolCalls_TwoDistinctCalls 验证两个不同工具调用分步合并。
//
// 流式场景中，多个 tool call 的 fragment 是按顺序逐个到达的：
// 每个 chunk 只包含一个 tool call 的增量片段，不会在一个 chunk 中混合多个不同的 tool call。
// 因此正确的测试方式是逐步合并：先合并完第一个 call 的所有片段，再合并第二个。
func TestMergeToolCalls_TwoDistinctCalls(t *testing.T) {
	// 第一步：合并第一个 tool call 的两个片段
	left1 := []*ToolCall{
		NewToolCall("call_1", "search", `{"q":"`, WithToolCallIndex(0)),
	}
	right1 := []*ToolCall{
		NewToolCall("call_1", "", `test"}`),
	}
	result := mergeToolCalls(left1, right1)
	if len(result) != 1 {
		t.Fatalf("第一步合并后长度 = %d, want 1", len(result))
	}
	if result[0].Arguments != `{"q":"test"}` {
		t.Errorf("result[0].Arguments = %q, want %q", result[0].Arguments, `{"q":"test"}`)
	}

	// 第二步：合并第二个 tool call 的两个片段（与第一个 call id 不同，应追加）
	right2 := []*ToolCall{
		NewToolCall("call_2", "calc", `{"e":"`, WithToolCallIndex(1)),
	}
	result = mergeToolCalls(result, right2)
	if len(result) != 2 {
		t.Fatalf("第二步合并后长度 = %d, want 2", len(result))
	}

	// 第三步：合并第二个 tool call 的第二个片段
	right3 := []*ToolCall{
		NewToolCall("call_2", "", `1+1"}`),
	}
	result = mergeToolCalls(result, right3)
	if len(result) != 2 {
		t.Fatalf("第三步合并后长度 = %d, want 2", len(result))
	}
	if result[0].Arguments != `{"q":"test"}` {
		t.Errorf("result[0].Arguments = %q, want %q", result[0].Arguments, `{"q":"test"}`)
	}
	if result[1].Arguments != `{"e":"1+1"}` {
		t.Errorf("result[1].Arguments = %q, want %q", result[1].Arguments, `{"e":"1+1"}`)
	}
}

// TestMergeToolCalls_DeepCopy 验证合并结果与原始切片无共享引用。
func TestMergeToolCalls_DeepCopy(t *testing.T) {
	left := []*ToolCall{NewToolCall("id1", "fn", "arg1")}
	right := []*ToolCall{NewToolCall("id2", "fn2", "arg2")}
	result := mergeToolCalls(left, right)

	// 修改原始切片，不应影响合并结果
	left[0].Arguments = "modified"
	if result[0].Arguments != "arg1" {
		t.Errorf("修改原始切片后 result[0].Arguments = %q, want %q（深拷贝隔离）", result[0].Arguments, "arg1")
	}
}

// ──────────────────── AssistantMessageChunk 测试 ────────────────────

// TestNewAssistantMessageChunk 验证默认值：role=assistant, finish_reason="null", content=空文本。
func TestNewAssistantMessageChunk(t *testing.T) {
	chunk := NewAssistantMessageChunk("你好")
	if chunk.Role != RoleTypeAssistant {
		t.Errorf("Role = %v, want %v", chunk.Role, RoleTypeAssistant)
	}
	if chunk.Content.Text() != "你好" {
		t.Errorf("Content = %q, want %q", chunk.Content.Text(), "你好")
	}
	if chunk.FinishReason != FinishReasonNull {
		t.Errorf("FinishReason = %q, want %q", chunk.FinishReason, FinishReasonNull)
	}
	if len(chunk.ToolCalls) != 0 {
		t.Errorf("ToolCalls 长度 = %d, want 0", len(chunk.ToolCalls))
	}
	if chunk.UsageMetadata != nil {
		t.Error("UsageMetadata 应为 nil")
	}
}

// TestNewAssistantMessageChunk_WithOptions 验证各选项函数生效。
func TestNewAssistantMessageChunk_WithOptions(t *testing.T) {
	meta := &UsageMetadata{InputTokens: 10}
	chunk := NewAssistantMessageChunk("test",
		WithChunkToolCalls([]*ToolCall{NewToolCall("id1", "fn", "{}")}),
		WithChunkUsageMetadata(meta),
		WithChunkFinishReason("stop"),
		WithChunkReasoningContent("思考"),
		WithChunkParserContent(map[string]any{"key": "val"}),
		WithChunkPromptTokenIDs([]int{1, 2}),
		WithChunkCompletionTokenIDs([]int{3, 4}),
		WithChunkLogprobs(map[string]any{"content": []any{1}}),
	)
	if len(chunk.ToolCalls) != 1 {
		t.Errorf("ToolCalls 长度 = %d, want 1", len(chunk.ToolCalls))
	}
	if chunk.UsageMetadata.InputTokens != 10 {
		t.Errorf("UsageMetadata.InputTokens = %d, want 10", chunk.UsageMetadata.InputTokens)
	}
	if chunk.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", chunk.FinishReason, "stop")
	}
	if chunk.ReasoningContent != "思考" {
		t.Errorf("ReasoningContent = %q, want %q", chunk.ReasoningContent, "思考")
	}
	if chunk.ParserContent == nil {
		t.Error("ParserContent 不应为 nil")
	}
	if len(chunk.PromptTokenIDs) != 2 {
		t.Errorf("PromptTokenIDs 长度 = %d, want 2", len(chunk.PromptTokenIDs))
	}
	if len(chunk.CompletionTokenIDs) != 2 {
		t.Errorf("CompletionTokenIDs 长度 = %d, want 2", len(chunk.CompletionTokenIDs))
	}
	if chunk.Logprobs == nil {
		t.Error("Logprobs 不应为 nil")
	}
}

// TestAssistantMessageChunk_Merge_ContentText 验证 content 文本合并。
func TestAssistantMessageChunk_Merge_ContentText(t *testing.T) {
	c1 := NewAssistantMessageChunk("你好")
	c2 := NewAssistantMessageChunk("世界")
	result := c1.Merge(c2)
	if result.Content.Text() != "你好世界" {
		t.Errorf("Content = %q, want %q", result.Content.Text(), "你好世界")
	}
}

// TestAssistantMessageChunk_Merge_FinishReason 验证 finish_reason 合并。
func TestAssistantMessageChunk_Merge_FinishReason(t *testing.T) {
	// right 非 "null" → 取 right
	c1 := NewAssistantMessageChunk("test")
	c2 := NewAssistantMessageChunk("", WithChunkFinishReason("stop"))
	result := c1.Merge(c2)
	if result.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "stop")
	}
}

// TestAssistantMessageChunk_Merge_FinishReason_BothNull 验证两者均为 "null" → 保持 "null"。
func TestAssistantMessageChunk_Merge_FinishReason_BothNull(t *testing.T) {
	c1 := NewAssistantMessageChunk("test")
	c2 := NewAssistantMessageChunk("test")
	result := c1.Merge(c2)
	if result.FinishReason != FinishReasonNull {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, FinishReasonNull)
	}
}

// TestAssistantMessageChunk_Merge_UsageMetadata 验证 usage_metadata 合并。
func TestAssistantMessageChunk_Merge_UsageMetadata(t *testing.T) {
	meta1 := &UsageMetadata{InputTokens: 10}
	meta2 := &UsageMetadata{InputTokens: 20}
	c1 := NewAssistantMessageChunk("test", WithChunkUsageMetadata(meta1))
	c2 := NewAssistantMessageChunk("", WithChunkUsageMetadata(meta2))
	result := c1.Merge(c2)
	if result.UsageMetadata.InputTokens != 20 {
		t.Errorf("UsageMetadata.InputTokens = %d, want 20（优先右侧）", result.UsageMetadata.InputTokens)
	}
}

// TestAssistantMessageChunk_Merge_ReasoningContent 验证 reasoning_content 字符串拼接。
func TestAssistantMessageChunk_Merge_ReasoningContent(t *testing.T) {
	c1 := NewAssistantMessageChunk("test", WithChunkReasoningContent("思考"))
	c2 := NewAssistantMessageChunk("", WithChunkReasoningContent("继续"))
	result := c1.Merge(c2)
	if result.ReasoningContent != "思考继续" {
		t.Errorf("ReasoningContent = %q, want %q", result.ReasoningContent, "思考继续")
	}
}

// TestAssistantMessageChunk_Merge_PromptTokenIDs 验证 prompt_token_ids 取非空。
func TestAssistantMessageChunk_Merge_PromptTokenIDs(t *testing.T) {
	c1 := NewAssistantMessageChunk("test", WithChunkPromptTokenIDs([]int{1, 2}))
	c2 := NewAssistantMessageChunk("")
	result := c1.Merge(c2)
	if len(result.PromptTokenIDs) != 2 {
		t.Errorf("PromptTokenIDs 长度 = %d, want 2", len(result.PromptTokenIDs))
	}
}

// TestAssistantMessageChunk_Merge_CompletionTokenIDs 验证 completion_token_ids 列表拼接。
func TestAssistantMessageChunk_Merge_CompletionTokenIDs(t *testing.T) {
	c1 := NewAssistantMessageChunk("test", WithChunkCompletionTokenIDs([]int{1, 2}))
	c2 := NewAssistantMessageChunk("", WithChunkCompletionTokenIDs([]int{3, 4}))
	result := c1.Merge(c2)
	if len(result.CompletionTokenIDs) != 4 {
		t.Fatalf("CompletionTokenIDs 长度 = %d, want 4", len(result.CompletionTokenIDs))
	}
	expected := []int{1, 2, 3, 4}
	for i, v := range expected {
		if result.CompletionTokenIDs[i] != v {
			t.Errorf("CompletionTokenIDs[%d] = %d, want %d", i, result.CompletionTokenIDs[i], v)
		}
	}
}

// TestAssistantMessageChunk_Merge_ParserContent 验证 parser_content 智能合并。
func TestAssistantMessageChunk_Merge_ParserContent(t *testing.T) {
	left := map[string]any{
		"blocks": []any{map[string]any{"block_id": "1", "summary": "你好"}},
	}
	right := map[string]any{
		"blocks": []any{map[string]any{"block_id": "2", "summary": "再见"}},
	}
	c1 := NewAssistantMessageChunk("test", WithChunkParserContent(left))
	c2 := NewAssistantMessageChunk("", WithChunkParserContent(right))
	result := c1.Merge(c2)
	resultMap, ok := result.ParserContent.(map[string]any)
	if !ok {
		t.Fatal("ParserContent 应为 map[string]any")
	}
	blocks, ok := resultMap["blocks"].([]any)
	if !ok {
		t.Fatal("blocks 应为 []any")
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks 长度 = %d, want 2（智能合并保留两次解析结果）", len(blocks))
	}
}

// TestAssistantMessageChunk_Merge_Logprobs 验证 logprobs 合并。
func TestAssistantMessageChunk_Merge_Logprobs(t *testing.T) {
	leftLogprobs := map[string]any{"content": []any{map[string]any{"token": "A"}}}
	rightLogprobs := map[string]any{"content": []any{map[string]any{"token": "B"}}}
	c1 := NewAssistantMessageChunk("test", WithChunkLogprobs(leftLogprobs))
	c2 := NewAssistantMessageChunk("", WithChunkLogprobs(rightLogprobs))
	result := c1.Merge(c2)
	resultMap, ok := result.Logprobs.(map[string]any)
	if !ok {
		t.Fatal("Logprobs 应为 map[string]any")
	}
	content, ok := resultMap["content"].([]any)
	if !ok {
		t.Fatal("content 应为 []any")
	}
	if len(content) != 2 {
		t.Errorf("content 长度 = %d, want 2", len(content))
	}
}

// TestAssistantMessageChunk_Merge_NilOther 验证 other 为 nil → 返回自身。
func TestAssistantMessageChunk_Merge_NilOther(t *testing.T) {
	c1 := NewAssistantMessageChunk("test")
	result := c1.Merge(nil)
	if result != c1 {
		t.Error("other 为 nil 时应返回自身")
	}
}

// TestAssistantMessageChunk_Merge_Immutable 验证 Merge 不修改接收者。
func TestAssistantMessageChunk_Merge_Immutable(t *testing.T) {
	c1 := NewAssistantMessageChunk("你好")
	c2 := NewAssistantMessageChunk("世界")
	result := c1.Merge(c2)
	// 验证 c1 未被修改
	if c1.Content.Text() != "你好" {
		t.Errorf("c1.Content 被修改为 %q，应保持 %q", c1.Content.Text(), "你好")
	}
	// 验证 result 是新对象
	if result.Content.Text() != "你好世界" {
		t.Errorf("result.Content = %q, want %q", result.Content.Text(), "你好世界")
	}
}

// TestAssistantMessageChunk_ToAssistantMessage 验证转换为 AssistantMessage 后字段一致。
func TestAssistantMessageChunk_ToAssistantMessage(t *testing.T) {
	chunk := NewAssistantMessageChunk("你好",
		WithChunkFinishReason("stop"),
		WithChunkReasoningContent("思考"),
	)
	msg := chunk.ToAssistantMessage()
	if msg.Role != RoleTypeAssistant {
		t.Errorf("Role = %v, want %v", msg.Role, RoleTypeAssistant)
	}
	if msg.Content.Text() != "你好" {
		t.Errorf("Content = %q, want %q", msg.Content.Text(), "你好")
	}
	if msg.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", msg.FinishReason, "stop")
	}
	if msg.ReasoningContent != "思考" {
		t.Errorf("ReasoningContent = %q, want %q", msg.ReasoningContent, "思考")
	}
}

// TestAssistantMessageChunk_ToAssistantMessage_NilChunk 验证 nil chunk → nil。
func TestAssistantMessageChunk_ToAssistantMessage_NilChunk(t *testing.T) {
	var chunk *AssistantMessageChunk
	msg := chunk.ToAssistantMessage()
	if msg != nil {
		t.Error("nil chunk 应返回 nil")
	}
}

// ──────────────────── ToolMessageChunk 测试 ────────────────────

// TestNewToolMessageChunk 验证构造 + 默认值。
func TestNewToolMessageChunk(t *testing.T) {
	chunk := NewToolMessageChunk("call_1", "结果")
	if chunk.Role != RoleTypeTool {
		t.Errorf("Role = %v, want %v", chunk.Role, RoleTypeTool)
	}
	if chunk.Content.Text() != "结果" {
		t.Errorf("Content = %q, want %q", chunk.Content.Text(), "结果")
	}
	if chunk.ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q, want %q", chunk.ToolCallID, "call_1")
	}
}

// TestToolMessageChunk_Merge 验证 content 拼接 + tool_call_id 取非空。
func TestToolMessageChunk_Merge(t *testing.T) {
	c1 := NewToolMessageChunk("call_1", "部分1")
	c2 := NewToolMessageChunk("", "部分2")
	result := c1.Merge(c2)
	if result.Content.Text() != "部分1部分2" {
		t.Errorf("Content = %q, want %q", result.Content.Text(), "部分1部分2")
	}
	if result.ToolCallID != "call_1" {
		t.Errorf("ToolCallID = %q, want %q（右侧为空取左侧）", result.ToolCallID, "call_1")
	}
}

// TestToolMessageChunk_Merge_ToolCallIDRight 验证 tool_call_id 右侧优先。
func TestToolMessageChunk_Merge_ToolCallIDRight(t *testing.T) {
	c1 := NewToolMessageChunk("old_id", "data")
	c2 := NewToolMessageChunk("new_id", "")
	result := c1.Merge(c2)
	if result.ToolCallID != "new_id" {
		t.Errorf("ToolCallID = %q, want %q（右侧非空取右侧）", result.ToolCallID, "new_id")
	}
}

// TestToolMessageChunk_Merge_EmptyContent 验证空 content 合并。
func TestToolMessageChunk_Merge_EmptyContent(t *testing.T) {
	c1 := NewToolMessageChunk("id", "")
	c2 := NewToolMessageChunk("id", "内容")
	result := c1.Merge(c2)
	if result.Content.Text() != "内容" {
		t.Errorf("Content = %q, want %q", result.Content.Text(), "内容")
	}
}

// TestToolMessageChunk_Merge_NilOther 验证 other 为 nil → 返回自身。
func TestToolMessageChunk_Merge_NilOther(t *testing.T) {
	c1 := NewToolMessageChunk("id", "data")
	result := c1.Merge(nil)
	if result != c1 {
		t.Error("other 为 nil 时应返回自身")
	}
}

// ──────────────────── 流式场景集成测试 ────────────────────

// TestAssistantMessageChunk_StreamSimulation 模拟纯文本 SSE 流。
func TestAssistantMessageChunk_StreamSimulation(t *testing.T) {
	// 模拟 LLM 流式返回 "你好！" 的三个 chunk
	chunk1 := NewAssistantMessageChunk("你")
	chunk2 := NewAssistantMessageChunk("好")
	chunk3 := NewAssistantMessageChunk("！",
		WithChunkFinishReason("stop"),
		WithChunkUsageMetadata(&UsageMetadata{InputTokens: 10, OutputTokens: 3, TotalTokens: 13}),
	)

	// 逐步合并
	result := chunk1
	result = result.Merge(chunk2)
	result = result.Merge(chunk3)

	// 验证最终结果
	if result.Content.Text() != "你好！" {
		t.Errorf("Content = %q, want %q", result.Content.Text(), "你好！")
	}
	if result.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "stop")
	}
	if result.UsageMetadata == nil {
		t.Fatal("UsageMetadata 不应为 nil")
	}
	if result.UsageMetadata.TotalTokens != 13 {
		t.Errorf("TotalTokens = %d, want 13", result.UsageMetadata.TotalTokens)
	}

	// 转换为 AssistantMessage
	msg := result.ToAssistantMessage()
	if msg.Content.Text() != "你好！" {
		t.Errorf("ToAssistantMessage: Content = %q, want %q", msg.Content.Text(), "你好！")
	}
	if !msg.IsFinished() {
		t.Error("ToAssistantMessage: IsFinished() 应为 true")
	}
}

// TestAssistantMessageChunk_StreamWithToolCalls 模拟 function calling 流式场景。
func TestAssistantMessageChunk_StreamWithToolCalls(t *testing.T) {
	// 模拟流式返回一个 tool call，arguments 分三个 chunk 到达
	chunk1 := NewAssistantMessageChunk("",
		WithChunkToolCalls([]*ToolCall{{ID: "", Type: "function", Name: "search", Arguments: `{"qu`, Index: 0}}),
	)
	chunk2 := NewAssistantMessageChunk("",
		WithChunkToolCalls([]*ToolCall{{ID: "", Type: "function", Name: "", Arguments: `ery":`}}),
	)
	chunk3 := NewAssistantMessageChunk("",
		WithChunkToolCalls([]*ToolCall{{ID: "call_123", Type: "function", Name: "", Arguments: `"test"}`}}),
		WithChunkFinishReason("tool_calls"),
	)

	// 逐步合并
	result := chunk1
	result = result.Merge(chunk2)
	result = result.Merge(chunk3)

	// 验证 tool_calls 合并结果
	if len(result.ToolCalls) != 1 {
		t.Fatalf("ToolCalls 长度 = %d, want 1", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ID != "call_123" {
		t.Errorf("ID = %q, want %q", tc.ID, "call_123")
	}
	if tc.Name != "search" {
		t.Errorf("Name = %q, want %q", tc.Name, "search")
	}
	if tc.Arguments != `{"query":"test"}` {
		t.Errorf("Arguments = %q, want %q", tc.Arguments, `{"query":"test"}`)
	}
	if result.FinishReason != "tool_calls" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "tool_calls")
	}
}

// TestAssistantMessageChunk_StreamWithReasoning 模拟带思维链的流式场景。
func TestAssistantMessageChunk_StreamWithReasoning(t *testing.T) {
	chunk1 := NewAssistantMessageChunk("", WithChunkReasoningContent("让我"))
	chunk2 := NewAssistantMessageChunk("", WithChunkReasoningContent("想想"))
	chunk3 := NewAssistantMessageChunk("答案是42", WithChunkFinishReason("stop"))

	result := chunk1
	result = result.Merge(chunk2)
	result = result.Merge(chunk3)

	if result.ReasoningContent != "让我想想" {
		t.Errorf("ReasoningContent = %q, want %q", result.ReasoningContent, "让我想想")
	}
	if result.Content.Text() != "答案是42" {
		t.Errorf("Content = %q, want %q", result.Content.Text(), "答案是42")
	}
}

// TestAssistantMessageChunk_JSONRoundTrip 验证序列化/反序列化一致性。
func TestAssistantMessageChunk_JSONRoundTrip(t *testing.T) {
	original := NewAssistantMessageChunk("你好",
		WithChunkFinishReason("stop"),
		WithChunkReasoningContent("思考"),
		WithChunkToolCalls([]*ToolCall{NewToolCall("call_1", "search", `{"q":"test"}`)}),
	)

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored AssistantMessageChunk
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	if restored.Role != original.Role {
		t.Errorf("Role: got %v, want %v", restored.Role, original.Role)
	}
	if restored.Content.Text() != original.Content.Text() {
		t.Errorf("Content: got %q, want %q", restored.Content.Text(), original.Content.Text())
	}
	if restored.FinishReason != original.FinishReason {
		t.Errorf("FinishReason: got %q, want %q", restored.FinishReason, original.FinishReason)
	}
	if restored.ReasoningContent != original.ReasoningContent {
		t.Errorf("ReasoningContent: got %q, want %q", restored.ReasoningContent, original.ReasoningContent)
	}
	if len(restored.ToolCalls) != 1 {
		t.Fatalf("ToolCalls 长度: got %d, want 1", len(restored.ToolCalls))
	}
	if restored.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCalls[0].Name: got %q, want %q", restored.ToolCalls[0].Name, "search")
	}
}

// TestAssistantMessageChunk_StreamWithParserContent 模拟带输出解析器的流式场景。
//
// 验证多次解析成功时，Go 智能合并不会丢失前面的结果（比 Python 更正确）。
func TestAssistantMessageChunk_StreamWithParserContent(t *testing.T) {
	// 模拟两次 JsonOutputParser 解析成功的场景
	chunk1 := NewAssistantMessageChunk("test",
		WithChunkParserContent(map[string]any{
			"blocks": []any{map[string]any{"block_id": "1", "summary": "你好"}},
		}),
	)
	chunk2 := NewAssistantMessageChunk("",
		WithChunkParserContent(map[string]any{
			"blocks": []any{map[string]any{"block_id": "2", "summary": "再见"}},
		}),
		WithChunkFinishReason("stop"),
	)

	result := chunk1.Merge(chunk2)

	// 验证 parser_content 智能合并：blocks 列表拼接
	resultMap, ok := result.ParserContent.(map[string]any)
	if !ok {
		t.Fatal("ParserContent 应为 map[string]any")
	}
	blocks, ok := resultMap["blocks"].([]any)
	if !ok {
		t.Fatal("blocks 应为 []any")
	}
	if len(blocks) != 2 {
		t.Fatalf("blocks 长度 = %d, want 2（智能合并保留所有解析结果）", len(blocks))
	}
	if result.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want %q", result.FinishReason, "stop")
	}
}
