package responses

import (
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// TestParseSSEBlock SSE 块解析：event/data、[DONE]、非法 JSON、无 data。
// Python: responses_utils.py:349 parse_sse_block
func TestParseSSEBlock(t *testing.T) {
	// event 行 + data → dict
	ev, err := ParseSSEBlock([]string{
		"event: response.completed",
		`data: {"type":"response.completed","id":"r1"}`,
	})
	if err != nil || ev["type"] != "response.completed" || ev["id"] != "r1" {
		t.Fatalf("event+data: %v err=%v", ev, err)
	}
	// 仅 event 行无 data → {"type": event}
	ev2, err := ParseSSEBlock([]string{"event: ping"})
	if err != nil || ev2["type"] != "ping" {
		t.Fatalf("仅 event: %v err=%v", ev2, err)
	}
	// data 无 type、有 event → 补 type
	ev3, err := ParseSSEBlock([]string{"event: foo", `data: {"a":1}`})
	if err != nil || ev3["type"] != "foo" || ev3["a"] != float64(1) {
		t.Fatalf("data 无 type: %v err=%v", ev3, err)
	}
	// [DONE] → nil
	ev4, err := ParseSSEBlock([]string{"data: [DONE]"})
	if err != nil || ev4 != nil {
		t.Fatalf("[DONE]: %v err=%v", ev4, err)
	}
	// 注释行/空行忽略 → nil
	ev5, err := ParseSSEBlock([]string{": comment"})
	if err != nil || ev5 != nil {
		t.Fatalf("注释行: %v err=%v", ev5, err)
	}
	// 非法 JSON → ResponsesError
	_, err = ParseSSEBlock([]string{"data: {bad"})
	if err == nil || !strings.Contains(err.Error(), "Invalid OpenAI account SSE JSON") {
		t.Fatalf("非法 JSON: %v", err)
	}
}

// TestIterSSEEvents 空行分块 + 尾块结算。
// Python: responses_utils.py:330 iter_sse_events
func TestIterSSEEvents(t *testing.T) {
	lines := []string{
		`data: {"type":"a"}`, "",
		`data: {"type":"b"}`, "",
		`data: {"type":"c"}`, // 无尾空行 → 收尾结算
	}
	var types []string
	for ev := range IterSSEEvents(lines) {
		if ev.Err != nil {
			t.Fatalf("事件错误: %v", ev.Err)
		}
		if ev.Event != nil {
			types = append(types, ev.Event["type"].(string))
		}
	}
	if strings.Join(types, ",") != "a,b,c" {
		t.Fatalf("events: %v", types)
	}
}

// TestStreamParser_TextAndReasoning 文本与推理 delta。
func TestStreamParser_TextAndReasoning(t *testing.T) {
	p := NewStreamParser("m1")
	c, err := p.Parse(map[string]any{"type": "response.output_text.delta", "delta": "he"})
	if err != nil || c == nil || c.Content.String() != "he" {
		t.Fatalf("text delta: %+v err=%v", c, err)
	}
	// refusal.delta 同走 content
	r, err := p.Parse(map[string]any{"type": "response.refusal.delta", "delta": "no"})
	if err != nil || r == nil || r.Content.String() != "no" {
		t.Fatalf("refusal delta: %+v err=%v", r, err)
	}
	th, err := p.Parse(map[string]any{"type": "response.reasoning_summary_text.delta", "delta": "th"})
	if err != nil || th == nil || th.ReasoningContent != "th" {
		t.Fatalf("reasoning delta: %+v err=%v", th, err)
	}
	// 空 delta → nil
	empty, err := p.Parse(map[string]any{"type": "response.output_text.delta", "delta": ""})
	if err != nil || empty != nil {
		t.Fatalf("空 delta: %+v err=%v", empty, err)
	}
}

// TestStreamParser_FunctionCallLifecycle added→delta→done 补差。
func TestStreamParser_FunctionCallLifecycle(t *testing.T) {
	p := NewStreamParser("m1")
	added, err := p.Parse(map[string]any{"type": "response.output_item.added", "item": map[string]any{
		"type": "function_call", "id": "item_1", "call_id": "call_1", "name": "f1", "arguments": `{"a":`,
	}})
	if err != nil || added == nil || len(added.ToolCalls) != 1 {
		t.Fatalf("added: %+v err=%v", added, err)
	}
	if added.ToolCalls[0].Arguments != `{"a":` || added.ToolCalls[0].ID != "call_1" {
		t.Fatalf("added 帧: %+v", added.ToolCalls[0])
	}
	if added.FinishReason != "null" {
		t.Fatalf("added finish: %q", added.FinishReason)
	}
	delta, err := p.Parse(map[string]any{"type": "response.function_call_arguments.delta",
		"item_id": "item_1", "delta": `"b":1}`})
	if err != nil || delta == nil || delta.ToolCalls[0].Arguments != `"b":1}` || delta.ToolCalls[0].Name != "" {
		t.Fatalf("delta: %+v err=%v", delta, err)
	}
	if delta.ToolCalls[0].ResponseItemID != "item_1" {
		t.Fatalf("delta response_item_id: %+v", delta.ToolCalls[0])
	}
	done, err := p.Parse(map[string]any{"type": "response.output_item.done", "item": map[string]any{
		"type": "function_call", "id": "item_1", "call_id": "call_1", "name": "f1", "arguments": `{"a":"b":1}`,
	}})
	if err != nil || done == nil {
		t.Fatalf("done: %+v err=%v", done, err)
	}
	// done 参数与已流出完全一致 → remainder 空
	if done.ToolCalls[0].Arguments != "" {
		t.Fatalf("done remainder: %q", done.ToolCalls[0].Arguments)
	}
	if done.FinishReason != "tool_calls" || done.ToolCalls[0].Name != "" {
		t.Fatalf("done finish/name: %q/%q", done.FinishReason, done.ToolCalls[0].Name)
	}
	// done 带未流出尾部 → 只补 remainder（重新登记 item_2）
	_, _ = p.Parse(map[string]any{"type": "response.output_item.added", "item": map[string]any{
		"type": "function_call", "id": "item_2", "call_id": "call_1", "name": "f1", "arguments": `{"a":"b":1}`,
	}})
	done2, err := p.Parse(map[string]any{"type": "response.output_item.done", "item": map[string]any{
		"type": "function_call", "id": "item_2", "call_id": "call_1", "name": "f1", "arguments": `{"a":"b":1}tail`,
	}})
	if err != nil || done2 == nil {
		t.Fatalf("done2: %+v err=%v", done2, err)
	}
	if done2.ToolCalls[0].Arguments != "tail" {
		t.Fatalf("done2 remainder: %q", done2.ToolCalls[0].Arguments)
	}
}

// TestStreamParser_DoneDiverged done 参数与已流出分歧 → 保已流出并告警。
func TestStreamParser_DoneDiverged(t *testing.T) {
	p := NewStreamParser("m1")
	_, _ = p.Parse(map[string]any{"type": "response.output_item.added", "item": map[string]any{
		"type": "function_call", "id": "item_1", "call_id": "call_1", "name": "f1", "arguments": `{"x"`,
	}})
	done, err := p.Parse(map[string]any{"type": "response.output_item.done", "item": map[string]any{
		"type": "function_call", "id": "item_1", "call_id": "call_1", "name": "f1", "arguments": `{"y":1}`,
	}})
	if err != nil {
		t.Fatal(err)
	}
	// 已流出 {"x" 不是 done 参数前缀 → remainder 空（保已流出）
	if done.ToolCalls[0].Arguments != "" {
		t.Fatalf("分歧应保已流出: %q", done.ToolCalls[0].Arguments)
	}
}

// TestStreamParser_NoItemIDSkipped 无 item id 时 done 单独交付完整调用。
func TestStreamParser_NoItemIDSkipped(t *testing.T) {
	p := NewStreamParser("m1")
	added, _ := p.Parse(map[string]any{"type": "response.output_item.added", "item": map[string]any{
		"type": "function_call", "call_id": "call_1", "name": "f1", "arguments": `{}`,
	}})
	if added != nil {
		t.Fatalf("无 item id 的 added 应跳过: %+v", added)
	}
	delta, _ := p.Parse(map[string]any{"type": "response.function_call_arguments.delta",
		"item_id": "unknown", "delta": "x"})
	if delta != nil {
		t.Fatalf("未登记 item 的 delta 应跳过: %+v", delta)
	}
	done, err := p.Parse(map[string]any{"type": "response.output_item.done", "item": map[string]any{
		"type": "function_call", "id": "item_2", "call_id": "call_2", "name": "f2", "arguments": `{"z":0}`,
	}})
	if err != nil || done == nil || done.ToolCalls[0].Arguments != `{"z":0}` {
		t.Fatalf("done 交付完整调用: %+v err=%v", done, err)
	}
}

// TestStreamParser_Terminal completed/incomplete 终帧。
func TestStreamParser_Terminal(t *testing.T) {
	p := NewStreamParser("m1")
	term, err := p.Parse(map[string]any{"type": "response.completed", "response": map[string]any{
		"id": "resp_1", "model": "m1", "status": "completed",
		"usage": map[string]any{"input_tokens": 1, "output_tokens": 2},
	}})
	if err != nil || term == nil {
		t.Fatalf("terminal: %+v err=%v", term, err)
	}
	if term.FinishReason != "stop" || term.ResponseID != "resp_1" || term.ResponseModel != "m1" {
		t.Fatalf("terminal 字段: %+v", term)
	}
	if term.UsageMetadata == nil || term.UsageMetadata.TotalTokens != 3 {
		t.Fatalf("terminal usage: %+v", term.UsageMetadata)
	}
	inc, _ := NewStreamParser("m1").Parse(map[string]any{"type": "response.incomplete", "response": map[string]any{
		"model": "m2",
	}})
	if inc == nil || inc.FinishReason != "length" {
		t.Fatalf("incomplete: %+v", inc)
	}
	if inc.ResponseModel != "m2" {
		t.Fatalf("incomplete model 兜底: %q", inc.ResponseModel)
	}
}

// TestStreamParser_Failed failed/error 事件 → ResponsesError。
func TestStreamParser_Failed(t *testing.T) {
	for _, evType := range []string{"response.failed", "error"} {
		p := NewStreamParser("m1")
		_, err := p.Parse(map[string]any{"type": evType, "message": "boom"})
		if err == nil || err.Error() != "boom" {
			t.Fatalf("%s: err=%v", evType, err)
		}
	}
	// error.message 兜底
	p := NewStreamParser("m1")
	_, err := p.Parse(map[string]any{"type": "response.failed", "error": map[string]any{"message": "inner"}})
	if err == nil || err.Error() != "inner" {
		t.Fatalf("error.message 兜底: %v", err)
	}
	// 双缺失 → 固定文案
	p2 := NewStreamParser("m1")
	_, err = p2.Parse(map[string]any{"type": "response.failed"})
	if err == nil || err.Error() != "OpenAI account Responses stream failed." {
		t.Fatalf("固定文案: %v", err)
	}
}

// TestStreamParser_未知事件返回nil 与空事件。
func TestStreamParser_未知事件返回nil(t *testing.T) {
	p := NewStreamParser("m1")
	c, err := p.Parse(map[string]any{"type": "response.created"})
	if err != nil || c != nil {
		t.Fatalf("未知事件: %+v err=%v", c, err)
	}
	c2, err := p.Parse(nil)
	if err != nil || c2 != nil {
		t.Fatalf("空事件: %+v err=%v", c2, err)
	}
}

// TestStreamParser_非FunctionCallItem跳过 added/done 非 function_call 类型。
func TestStreamParser_非FunctionCallItem跳过(t *testing.T) {
	p := NewStreamParser("m1")
	c, _ := p.Parse(map[string]any{"type": "response.output_item.added", "item": map[string]any{
		"type": "reasoning",
	}})
	if c != nil {
		t.Fatalf("非 function_call added 应跳过: %+v", c)
	}
	c2, _ := p.Parse(map[string]any{"type": "response.output_item.done", "item": map[string]any{
		"type": "message",
	}})
	if c2 != nil {
		t.Fatalf("非 function_call done 应跳过: %+v", c2)
	}
}

// _ 占位保持 llmschema 引用（parser 断言均经 chunk 类型）。
var _ = llmschema.FinishReasonNull
