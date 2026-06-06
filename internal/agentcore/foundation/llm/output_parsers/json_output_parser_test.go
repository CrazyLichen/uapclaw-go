package output_parsers

import (
	"encoding/json"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── Parse 测试 ────────────────────────────

// TestJsonOutputParser_Parse_StringInput 测试纯字符串输入。
func TestJsonOutputParser_Parse_StringInput(t *testing.T) {
	parser := NewJsonOutputParser()

	result, err := parser.Parse(`{"name": "test", "value": 123}`)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("结果类型错误: %T", result)
	}
	if m["name"] != "test" {
		t.Errorf("name = %v, 期望 test", m["name"])
	}
}

// TestJsonOutputParser_Parse_AssistantMessageInput 测试 AssistantMessage 输入。
func TestJsonOutputParser_Parse_AssistantMessageInput(t *testing.T) {
	parser := NewJsonOutputParser()

	msg := llmschema.NewAssistantMessage(`{"key": "val"}`)
	result, err := parser.Parse(msg)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("结果类型错误: %T", result)
	}
	if m["key"] != "val" {
		t.Errorf("key = %v, 期望 val", m["key"])
	}
}

// TestJsonOutputParser_Parse_EmptyInput 测试空输入。
func TestJsonOutputParser_Parse_EmptyInput(t *testing.T) {
	parser := NewJsonOutputParser()

	result, err := parser.Parse("")
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	if result != nil {
		t.Errorf("空输入应返回 nil, 实际: %v", result)
	}
}

// TestJsonOutputParser_Parse_InvalidType 测试不支持的输入类型。
func TestJsonOutputParser_Parse_InvalidType(t *testing.T) {
	parser := NewJsonOutputParser()

	result, err := parser.Parse(12345)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	if result != nil {
		t.Errorf("不支持的类型应返回 nil, 实际: %v", result)
	}
}

// TestJsonOutputParser_Parse_MarkdownCodeBlock 测试 markdown 代码块提取。
func TestJsonOutputParser_Parse_MarkdownCodeBlock(t *testing.T) {
	parser := NewJsonOutputParser()

	input := "这是结果：\n```json\n{\"status\": \"ok\"}\n```\n其他文本"
	result, err := parser.Parse(input)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("结果类型错误: %T", result)
	}
	if m["status"] != "ok" {
		t.Errorf("status = %v, 期望 ok", m["status"])
	}
}

// TestJsonOutputParser_Parse_InvalidJSON 测试无效 JSON。
func TestJsonOutputParser_Parse_InvalidJSON(t *testing.T) {
	parser := NewJsonOutputParser()

	result, err := parser.Parse("not json at all")
	if err != nil {
		t.Fatalf("Parse 不应返回错误: %v", err)
	}
	if result != nil {
		t.Errorf("无效 JSON 应返回 nil, 实际: %v", result)
	}
}

// TestJsonOutputParser_Parse_NilAssistantMessage 测试 nil AssistantMessage。
func TestJsonOutputParser_Parse_NilAssistantMessage(t *testing.T) {
	parser := NewJsonOutputParser()

	var msg *llmschema.AssistantMessage
	result, err := parser.Parse(msg)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	if result != nil {
		t.Errorf("nil AssistantMessage 应返回 nil, 实际: %v", result)
	}
}

// TestJsonOutputParser_Parse_ArrayJSON 测试 JSON 数组。
func TestJsonOutputParser_Parse_ArrayJSON(t *testing.T) {
	parser := NewJsonOutputParser()

	input := `[1, 2, 3]`
	result, err := parser.Parse(input)
	if err != nil {
		t.Fatalf("Parse 返回错误: %v", err)
	}
	arr, ok := result.([]any)
	if !ok {
		t.Fatalf("结果类型错误: %T", result)
	}
	if len(arr) != 3 {
		t.Errorf("len = %d, 期望 3", len(arr))
	}
}

// ──────────────────────────── StreamParse 测试 ────────────────────────────

// TestJsonOutputParser_StreamParse_SingleObject 测试流式解析单个 JSON 对象。
func TestJsonOutputParser_StreamParse_SingleObject(t *testing.T) {
	parser := NewJsonOutputParser()

	// 模拟流式输出：逐字符发送 JSON
	jsonStr := `{"name": "hello"}`
	chunks := make(chan *llmschema.AssistantMessageChunk, len(jsonStr))
	for _, ch := range jsonStr {
		chunks <- llmschema.NewAssistantMessageChunk(string(ch))
	}
	close(chunks)

	results := collectStreamResults(parser.StreamParse(chunks))

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, 期望 1", len(results))
	}
	m, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("结果类型错误: %T", results[0])
	}
	if m["name"] != "hello" {
		t.Errorf("name = %v, 期望 hello", m["name"])
	}
}

// TestJsonOutputParser_StreamParse_MarkdownCodeBlock 测试流式解析 markdown 代码块中的 JSON。
func TestJsonOutputParser_StreamParse_MarkdownCodeBlock(t *testing.T) {
	parser := NewJsonOutputParser()

	// 模拟流式输出：包含 markdown 代码块
	chunks := make(chan *llmschema.AssistantMessageChunk, 10)
	// 分几段发送
	segments := []string{"结果如下：\n```json\n", `{"key": "val"}`, "\n```\n"}
	for _, seg := range segments {
		chunks <- llmschema.NewAssistantMessageChunk(seg)
	}
	close(chunks)

	results := collectStreamResults(parser.StreamParse(chunks))

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, 期望 1", len(results))
	}
	m, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("结果类型错误: %T", results[0])
	}
	if m["key"] != "val" {
		t.Errorf("key = %v, 期望 val", m["key"])
	}
}

// TestJsonOutputParser_StreamParse_MultipleObjects 测试流式解析多个 JSON 对象（增量输出）。
func TestJsonOutputParser_StreamParse_MultipleObjects(t *testing.T) {
	parser := NewJsonOutputParser()

	// 先发第一个完整 JSON，再发第二个
	chunks := make(chan *llmschema.AssistantMessageChunk, 4)
	chunks <- llmschema.NewAssistantMessageChunk(`{"a":1}`)
	chunks <- llmschema.NewAssistantMessageChunk(`{"b":2}`)
	close(chunks)

	results := collectStreamResults(parser.StreamParse(chunks))

	if len(results) != 2 {
		t.Fatalf("结果数量 = %d, 期望 2", len(results))
	}
}

// TestJsonOutputParser_StreamParse_EmptyStream 测试空流。
func TestJsonOutputParser_StreamParse_EmptyStream(t *testing.T) {
	parser := NewJsonOutputParser()

	chunks := make(chan *llmschema.AssistantMessageChunk)
	close(chunks)

	results := collectStreamResults(parser.StreamParse(chunks))

	if len(results) != 0 {
		t.Errorf("空流结果数量 = %d, 期望 0", len(results))
	}
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// collectStreamResults 从 StreamParse 输出 channel 收集所有结果。
func collectStreamResults(out <-chan StreamParsedResult) []any {
	var results []any
	for r := range out {
		if r.Error != nil {
			continue
		}
		results = append(results, r.Content)
	}
	return results
}

// mustMarshalJSON 辅助函数，JSON 序列化失败时 panic。
func mustMarshalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
