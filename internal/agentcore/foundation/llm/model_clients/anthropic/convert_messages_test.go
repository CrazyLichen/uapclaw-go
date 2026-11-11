package anthropic

import (
	"strings"
	"testing"
)

// TestConvertMessageSchemas_完整往返 tool_calls→tool_use、role:tool→tool_result、空 content 补块。
func TestConvertMessageSchemas_完整往返(t *testing.T) {
	input := []map[string]any{
		{"role": "system", "content": "be nice"},
		{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "t1", "type": "function",
				"function": map[string]any{"name": "f", "arguments": `{"a":1}`}},
		}},
		{"role": "tool", "tool_call_id": "t1", "content": "ok"},
		{"role": "user", "content": "hi"},
	}
	system, msgs := ConvertMessageSchemas(input, false)
	if len(system) != 1 || system[0]["text"] != "be nice" {
		t.Fatalf("system 分区失败: %v", system)
	}
	if len(msgs) != 3 {
		t.Fatalf("assistant+tool_result(并入 user)+user = 3 条, got %d: %v", len(msgs), msgs)
	}
	asst := msgs[0]
	if asst["role"] != "assistant" {
		t.Fatal("第 0 条应为 assistant")
	}
	blocks := asst["content"].([]map[string]any)
	// content 为 ""：ContentToBlocks 返回空 → 只剩 tool_use block（Python 同：
	// 只在 blocks 全空时补空 text，有 tool_use 时不补）
	if len(blocks) != 1 || blocks[0]["type"] != "tool_use" {
		t.Fatalf("assistant blocks 不符: %v", blocks)
	}
	if blocks[0]["id"] != "t1" || blocks[0]["name"] != "f" {
		t.Fatalf("tool_use 字段不符: %v", blocks[0])
	}
	inputMap := blocks[0]["input"].(map[string]any)
	if inputMap["a"] != float64(1) {
		t.Fatalf("arguments 应解析为对象: %v", inputMap)
	}
	tr := msgs[1]
	if tr["role"] != "user" {
		t.Fatal("tool result 应并入 user")
	}
	content := tr["content"].([]map[string]any)
	if content[0]["type"] != "tool_result" || content[0]["tool_use_id"] != "t1" {
		t.Fatalf("tool_result 不符: %v", content)
	}
	if msgs[2]["role"] != "user" {
		t.Fatal("最后应为 user")
	}
}

// TestConvertMessageSchemas_参数解析失败兜底 非法 JSON → _raw_arguments。
func TestConvertMessageSchemas_参数解析失败兜底(t *testing.T) {
	input := []map[string]any{
		{"role": "assistant", "content": "", "tool_calls": []any{
			map[string]any{"id": "t1", "type": "function",
				"function": map[string]any{"name": "f", "arguments": `{not json`}},
		}},
	}
	_, msgs := ConvertMessageSchemas(input, false)
	blocks := msgs[0]["content"].([]map[string]any)
	inputMap := blocks[0]["input"].(map[string]any)
	if inputMap["_raw_arguments"] != `{not json` {
		t.Fatalf("_raw_arguments 兜底失效: %v", inputMap)
	}
}

// TestConvertMessageSchemas_replay块原样回放 assistant 带 thinking 回放块时优先回放。
func TestConvertMessageSchemas_replay块原样回放(t *testing.T) {
	replay := []any{
		map[string]any{"type": "thinking", "thinking": "thought", "signature": "sig"},
		map[string]any{"type": "tool_use", "id": "t1", "name": "f", "input": map[string]any{"a": 1}},
	}
	input := []map[string]any{
		{"role": "assistant", "content": "ignored",
			"metadata": map[string]any{AnthropicContentBlocksMetadataKey: replay}},
	}
	_, msgs := ConvertMessageSchemas(input, false)
	blocks := msgs[0]["content"].([]map[string]any)
	if len(blocks) != 2 || blocks[0]["type"] != "thinking" {
		t.Fatalf("replay 块应原样回放: %v", blocks)
	}
	if blocks[0]["signature"] != "sig" {
		t.Fatalf("signature 必须原样保留: %v", blocks[0])
	}
}

// TestPartitionSystemMessages_邻居判定 中位 system 可留原地。对齐 :388-421 + :366-384。
func TestPartitionSystemMessages_邻居判定(t *testing.T) {
	converted := []map[string]any{
		{"role": "system", "content": "lead"},
		{"role": "user", "content": []map[string]any{{"type": "text", "text": "u"}}},
		{"role": "system", "content": "mid"},
		{"role": "assistant", "content": []map[string]any{{"type": "text", "text": "a"}}},
	}
	system, msgs := partitionSystemMessages(converted, true)
	if len(system) != 1 || system[0]["text"] != "lead" {
		t.Fatalf("lead 应上提: %v", system)
	}
	if len(msgs) != 3 {
		t.Fatalf("mid 应留原地: %v", msgs)
	}
	// 不允许 mid-conversation system 时全部上提
	system2, msgs2 := partitionSystemMessages(converted, false)
	if len(system2) != 2 || len(msgs2) != 2 {
		t.Fatalf("禁用时全部上提: %v / %v", system2, msgs2)
	}
}

// TestInlineSystemIsPlaceable_边界 index0 永不可留、后邻非 assistant 不可留。
func TestInlineSystemIsPlaceable_边界(t *testing.T) {
	converted := []map[string]any{
		{"role": "system"}, {"role": "user"}, {"role": "system"}, {"role": "user"},
		{"role": "system"},
	}
	if inlineSystemIsPlaceable(converted, 0) {
		t.Fatal("index 0 永不可留")
	}
	if inlineSystemIsPlaceable(converted, 2) {
		t.Fatal("前邻是 system（按分区前列表判定）→ 不可留")
	}
	if !inlineSystemIsPlaceable(converted, 4) {
		t.Fatal("前邻 user + 末条应可留")
	}
	// 前邻 user 但后邻非 assistant → 不可留
	mid := []map[string]any{
		{"role": "user"}, {"role": "system"}, {"role": "user"},
	}
	if inlineSystemIsPlaceable(mid, 1) {
		t.Fatal("后邻 user（非 assistant）应不可留")
	}
}

// TestApplyStaticCacheBreakpoints 对齐 :542-549。
func TestApplyStaticCacheBreakpoints(t *testing.T) {
	tools := []map[string]any{{"name": "f"}}
	system := []map[string]any{{"type": "text", "text": "s"}}
	ApplyStaticCacheBreakpoints(system, tools)
	if tools[0]["cache_control"] == nil {
		t.Fatal("tools 尾应挂 ephemeral")
	}
	if system[0]["cache_control"] == nil {
		t.Fatal("system 应挂 5m")
	}
	// 空列表不 panic
	ApplyStaticCacheBreakpoints(nil, nil)
}

// TestLastInputIsTransient 对齐 :552-569。
func TestLastInputIsTransient(t *testing.T) {
	if LastInputIsTransient(nil) {
		t.Fatal("空列表 → false")
	}
	if LastInputIsTransient([]map[string]any{{"role": "user", "content": "x"}}) {
		t.Fatal("无 metadata → false")
	}
	if !LastInputIsTransient([]map[string]any{
		{"role": "user", "content": "x", "metadata": map[string]any{"transient": true}},
	}) {
		t.Fatal("transient=true → true")
	}
	if LastInputIsTransient([]map[string]any{
		{"role": "user", "content": "x", "metadata": map[string]any{"transient": false}},
	}) {
		t.Fatal("transient=false → false")
	}
}

// TestApplyMessagesCacheBreakpoint_transient尾跳过 对齐 :571-601。
func TestApplyMessagesCacheBreakpoint_transient尾跳过(t *testing.T) {
	msgs := []map[string]any{
		{"role": "user", "content": []map[string]any{{"type": "text", "text": "stable"}}},
		{"role": "system", "content": "mid-system"}, // 非 list content
		{"role": "user", "content": []map[string]any{{"type": "text", "text": "tail"}}},
	}
	ApplyMessagesCacheBreakpoint(msgs, true) // exclude_tail：锚在 tail 前
	// mid-system 无块可挂 → 回走到 stable
	blocks := msgs[0]["content"].([]map[string]any)
	if blocks[0]["cache_control"] == nil {
		t.Fatalf("应回走锚定 stable: %v", msgs)
	}
	// 全部非 list → 不锚定、不 panic
	ApplyMessagesCacheBreakpoint([]map[string]any{{"role": "system", "content": "text"}}, false)
	ApplyMessagesCacheBreakpoint(nil, false)
}

// TestConvertMessageSchemas_工具结果空内容补空块 API 要求非空 content。
func TestConvertMessageSchemas_工具结果空内容补空块(t *testing.T) {
	input := []map[string]any{
		{"role": "tool", "tool_call_id": "t1", "content": ""},
	}
	_, msgs := ConvertMessageSchemas(input, false)
	content := msgs[0]["content"].([]map[string]any)
	if content[0]["content"].([]map[string]any)[0]["text"] != "" {
		t.Fatalf("空结果应补空 text: %v", content)
	}
	if !strings.Contains(content[0]["type"].(string), "tool_result") {
		t.Fatalf("type 应为 tool_result: %v", content[0]["type"])
	}
}
