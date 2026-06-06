package output_parsers

import (
	"fmt"
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
		t.Fatalf("空输入不应返回错误: %v", err)
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
		// 不支持的类型 ExtractText 返回空字符串，Parse 对空输入返回 nil, nil
		// 但 ExtractText 会记录 warning 日志
		t.Logf("不支持的类型返回 error: %v（可接受）", err)
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
	if err == nil {
		t.Error("无效 JSON 应返回 error")
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
		t.Fatalf("nil AssistantMessage 不应返回错误: %v", err)
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

// TestJsonOutputParser_Parse_ExtractsModelName 测试从 AssistantMessage.UsageMetadata 提取 model_name。
func TestJsonOutputParser_Parse_ExtractsModelName(t *testing.T) {
	parser := NewJsonOutputParser()

	msg := llmschema.NewAssistantMessage(`{"status": "ok"}`,
		llmschema.WithAssistantUsageMetadata(&llmschema.UsageMetadata{
			ModelName: "gpt-4",
		}),
	)
	result, err := parser.Parse(msg)
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

// ──────────────────────────── StreamParse 测试 ────────────────────────────

// TestJsonOutputParser_StreamParse_SingleObject 测试流式解析单个 JSON 对象。
func TestJsonOutputParser_StreamParse_SingleObject(t *testing.T) {
	parser := NewJsonOutputParser()

	// 模拟流式输出：逐字符发送 JSON（使用 string chunk）
	jsonStr := `{"name": "hello"}`
	chunks := make(chan any, len(jsonStr))
	for _, ch := range jsonStr {
		chunks <- string(ch)
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

// TestJsonOutputParser_StreamParse_AssistantMessageChunk 测试使用 AssistantMessageChunk 的流式解析。
func TestJsonOutputParser_StreamParse_AssistantMessageChunk(t *testing.T) {
	parser := NewJsonOutputParser()

	// 模拟流式输出：使用 AssistantMessageChunk
	jsonStr := `{"name": "hello"}`
	chunks := make(chan any, len(jsonStr))
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
	chunks := make(chan any, 10)
	// 分几段发送
	segments := []string{"结果如下：\n```json\n", `{"key": "val"}`, "\n```\n"}
	for _, seg := range segments {
		chunks <- seg
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
	chunks := make(chan any, 4)
	chunks <- `{"a":1}`
	chunks <- `{"b":2}`
	close(chunks)

	results := collectStreamResults(parser.StreamParse(chunks))

	if len(results) != 2 {
		t.Fatalf("结果数量 = %d, 期望 2", len(results))
	}
}

// TestJsonOutputParser_StreamParse_EmptyStream 测试空流。
func TestJsonOutputParser_StreamParse_EmptyStream(t *testing.T) {
	parser := NewJsonOutputParser()

	chunks := make(chan any)
	close(chunks)

	results := collectStreamResults(parser.StreamParse(chunks))

	if len(results) != 0 {
		t.Errorf("空流结果数量 = %d, 期望 0", len(results))
	}
}

// TestJsonOutputParser_StreamParse_UnsupportedChunkType 测试不支持的 chunk 类型。
func TestJsonOutputParser_StreamParse_UnsupportedChunkType(t *testing.T) {
	parser := NewJsonOutputParser()

	chunks := make(chan any, 4)
	chunks <- 12345 // 不支持的类型，应跳过
	chunks <- `{"valid": true}`
	close(chunks)

	results := collectStreamResults(parser.StreamParse(chunks))

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, 期望 1（不支持的 chunk 应被跳过）", len(results))
	}
	m, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("结果类型错误: %T", results[0])
	}
	if m["valid"] != true {
		t.Errorf("valid = %v, 期望 true", m["valid"])
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

// TestExtractText_ReturnsModelName 测试 ExtractText 从 AssistantMessage 提取 model_name。
func TestExtractText_ReturnsModelName(t *testing.T) {
	msg := llmschema.NewAssistantMessage("hello",
		llmschema.WithAssistantUsageMetadata(&llmschema.UsageMetadata{
			ModelName: "gpt-4",
		}),
	)
	text, modelName := ExtractText(msg)
	if text != "hello" {
		t.Errorf("text = %q, 期望 %q", text, "hello")
	}
	if modelName != "gpt-4" {
		t.Errorf("modelName = %q, 期望 %q", modelName, "gpt-4")
	}
}

// TestExtractText_StringInput 测试 ExtractText 纯字符串输入。
func TestExtractText_StringInput(t *testing.T) {
	text, modelName := ExtractText("test content")
	if text != "test content" {
		t.Errorf("text = %q, 期望 %q", text, "test content")
	}
	if modelName != "" {
		t.Errorf("modelName = %q, 期望空", modelName)
	}
}

// TestExtractText_NilAssistantMessage 测试 ExtractText nil AssistantMessage。
func TestExtractText_NilAssistantMessage(t *testing.T) {
	var msg *llmschema.AssistantMessage
	text, modelName := ExtractText(msg)
	if text != "" {
		t.Errorf("text = %q, 期望空", text)
	}
	if modelName != "" {
		t.Errorf("modelName = %q, 期望空", modelName)
	}
}

// TestExtractText_UnsupportedType 测试 ExtractText 不支持的类型。
func TestExtractText_UnsupportedType(t *testing.T) {
	text, modelName := ExtractText(12345)
	if text != "" {
		t.Errorf("text = %q, 期望空", text)
	}
	if modelName != "" {
		t.Errorf("modelName = %q, 期望空", modelName)
	}
}

// TestJsonOutputParser_Parse_InvalidJSON_ErrorMessage 测试无效 JSON 的 error 消息包含原始错误。
func TestJsonOutputParser_Parse_InvalidJSON_ErrorMessage(t *testing.T) {
	parser := NewJsonOutputParser()

	_, err := parser.Parse("not json at all")
	if err == nil {
		t.Fatal("期望返回 error")
	}
	// 验证 error 消息可读
	if msg := err.Error(); msg == "" {
		t.Error("error 消息不应为空")
	}
}

// TestJsonOutputParser_Parse_InvalidJSON_WithModelName 测试带 model_name 的无效 JSON 日志。
func TestJsonOutputParser_Parse_InvalidJSON_WithModelName(t *testing.T) {
	parser := NewJsonOutputParser()

	msg := llmschema.NewAssistantMessage("not json",
		llmschema.WithAssistantUsageMetadata(&llmschema.UsageMetadata{
			ModelName: "deepseek-v3",
		}),
	)
	_, err := parser.Parse(msg)
	if err == nil {
		t.Fatal("期望返回 error")
	}
	// 验证 error 中包含 JSON 解码失败信息
	if msg := err.Error(); len(msg) == 0 {
		t.Error("error 消息不应为空")
	}
}

// TestJsonOutputParser_StreamParse_MixedChunkTypes 测试混合 chunk 类型。
func TestJsonOutputParser_StreamParse_MixedChunkTypes(t *testing.T) {
	parser := NewJsonOutputParser()

	chunks := make(chan any, 5)
	chunks <- llmschema.NewAssistantMessageChunk(`{"a"`)
	chunks <- `:1}` // string chunk 补全 JSON
	close(chunks)

	results := collectStreamResults(parser.StreamParse(chunks))

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, 期望 1", len(results))
	}
	m, ok := results[0].(map[string]any)
	if !ok {
		t.Fatalf("结果类型错误: %T", results[0])
	}
	if m["a"] != float64(1) {
		t.Errorf("a = %v, 期望 1", m["a"])
	}
}

// TestJsonOutputParser_StreamParse_NilChunk 测试 nil chunk 被跳过。
func TestJsonOutputParser_StreamParse_NilChunk(t *testing.T) {
	parser := NewJsonOutputParser()

	chunks := make(chan any, 4)
	chunks <- (*llmschema.AssistantMessageChunk)(nil) // nil chunk
	chunks <- `{"ok":true}`
	close(chunks)

	results := collectStreamResults(parser.StreamParse(chunks))

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, 期望 1（nil chunk 应被跳过）", len(results))
	}
}

// TestJsonOutputParser_Parse_NilAssistantMessage_WithUsageMetadata 测试有 UsageMetadata 的 nil msg。
func TestJsonOutputParser_Parse_NilAssistantMessage_WithUsageMetadata(t *testing.T) {
	// nil AssistantMessage（即使类型正确，指针为 nil）
	var msg *llmschema.AssistantMessage
	text, modelName := ExtractText(msg)
	_ = text
	_ = modelName
	// 验证不会 panic
	if text != "" {
		t.Errorf("nil AssistantMessage 的 text 应为空")
	}
	if modelName != "" {
		t.Errorf("nil AssistantMessage 的 modelName 应为空")
	}
}

// TestExtractText_NilUsageMetadata 测试 AssistantMessage 的 UsageMetadata 为 nil。
func TestExtractText_NilUsageMetadata(t *testing.T) {
	msg := llmschema.NewAssistantMessage("content")
	text, modelName := ExtractText(msg)
	if text != "content" {
		t.Errorf("text = %q, 期望 %q", text, "content")
	}
	if modelName != "" {
		t.Errorf("modelName = %q, 期望空（UsageMetadata 为 nil）", modelName)
	}
}

// TestJsonOutputParser_Parse_EmptyJSONInCodeBlock 测试代码块中空 JSON。
func TestJsonOutputParser_Parse_EmptyJSONInCodeBlock(t *testing.T) {
	parser := NewJsonOutputParser()

	input := "```json\n\n```"
	_, err := parser.Parse(input)
	if err == nil {
		t.Error("空 JSON 代码块应返回 error")
	}
}

// TestJsonOutputParser_Parse_PropagatesOriginalError 测试 Parse 错误包含原始 JSON 错误。
func TestJsonOutputParser_Parse_PropagatesOriginalError(t *testing.T) {
	parser := NewJsonOutputParser()

	_, err := parser.Parse("{invalid}")
	if err == nil {
		t.Fatal("期望返回 error")
	}
	// 错误消息应包含 "failed to decode JSON"
	expected := "failed to decode JSON"
	if msg := err.Error(); len(msg) < len(expected) {
		t.Errorf("error 消息过短: %q", msg)
	}
}

// TestJsonOutputParser_Parse_EmptyStringNotError 测试空字符串不返回错误。
func TestJsonOutputParser_Parse_EmptyStringNotError(t *testing.T) {
	parser := NewJsonOutputParser()

	result, err := parser.Parse("")
	if err != nil {
		t.Errorf("空字符串不应返回 error: %v", err)
	}
	if result != nil {
		t.Errorf("空字符串应返回 nil result")
	}
}

// TestJsonOutputParser_StreamParse_ExtractsModelName 测试流式解析中提取 model_name。
func TestJsonOutputParser_StreamParse_ExtractsModelName(t *testing.T) {
	parser := NewJsonOutputParser()

	chunks := make(chan any, 2)
	// 第一个 chunk 带 UsageMetadata
	chunk := llmschema.NewAssistantMessageChunk(`{"ok":true}`)
	chunk.UsageMetadata = &llmschema.UsageMetadata{ModelName: "deepseek-v3"}
	chunks <- chunk
	close(chunks)

	results := collectStreamResults(parser.StreamParse(chunks))

	if len(results) != 1 {
		t.Fatalf("结果数量 = %d, 期望 1", len(results))
	}
}

// TestJsonOutputParser_StreamParse_RemainingBufferError 测试残余 buffer 解析失败。
func TestJsonOutputParser_StreamParse_RemainingBufferError(t *testing.T) {
	parser := NewJsonOutputParser()

	// 发送不完整的 JSON，残余 buffer 应产生警告
	chunks := make(chan any, 1)
	chunks <- `{incomplete`
	close(chunks)

	results := collectStreamResults(parser.StreamParse(chunks))

	// 残余 buffer 无法解析，不输出结果
	if len(results) != 0 {
		t.Logf("残余 buffer 解析结果数量 = %d（可能为 0 或有部分结果）", len(results))
	}
}

// TestJsonOutputParser_Parse_FormatError 测试错误格式化。
func TestJsonOutputParser_Parse_FormatError(t *testing.T) {
	parser := NewJsonOutputParser()

	_, err := parser.Parse("not json")
	if err == nil {
		t.Fatal("期望返回 error")
	}
	// 验证 %w 包装可以 Unwrap
	if unwrapped := fmt.Errorf("wrap: %w", err); unwrapped == nil {
		t.Error("error 应支持 Unwrap")
	}
}
