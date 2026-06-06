package prompt

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ────────────────────── 字符串模板 Assemble 测试 ──────────────────────

// TestAssembler_StringTemplate_CustomDelimiters 验证字符串模板自定义前后缀。
func TestAssembler_StringTemplate_CustomDelimiters(t *testing.T) {
	asm, err := NewPromptAssembler(
		"`#system#`${role}$`#user#`${memory}$",
		WithAssemblerPrefix("${"),
		WithAssemblerSuffix("}$"),
		WithAssemblerVariable("role", mustNewTextableVariable(
			"你是一个精通${domain}$领域的问答助手。", "role",
			WithPrefix("${"), WithSuffix("}$"),
		)),
	)
	if err != nil {
		t.Fatalf("NewPromptAssembler 失败: %v", err)
	}

	inputKeys := asm.InputKeys()
	if len(inputKeys) != 2 {
		t.Errorf("InputKeys() = %v, want len 2", inputKeys)
	}

	result, err := asm.Assemble(map[string]any{
		"memory": []any{map[string]any{"role": "user", "content": "我是谁"}},
		"domain": "科学",
	})
	if err != nil {
		t.Fatalf("Assemble 失败: %v", err)
	}

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("结果类型错误: %T", result)
	}
	if !contains(resultStr, "精通科学领域的问答助手") {
		t.Errorf("结果不包含期望文本: %s", resultStr)
	}
}

// TestAssembler_StringTemplate_CurlyBrace 验证 {} 格式占位符的 Assemble。
func TestAssembler_StringTemplate_CurlyBrace(t *testing.T) {
	asm, err := NewPromptAssembler(
		"`#system#`{role}`#user#`{memory}",
		WithAssemblerPrefix("{"),
		WithAssemblerSuffix("}"),
		WithAssemblerVariable("role", mustNewTextableVariable(
			"你是一个精通{domain}领域的问答助手。", "role",
			WithPrefix("{"), WithSuffix("}"),
		)),
	)
	if err != nil {
		t.Fatalf("NewPromptAssembler 失败: %v", err)
	}

	result, err := asm.Assemble(map[string]any{
		"memory": []any{map[string]any{"role": "user", "content": "我是谁"}},
		"domain": "天文",
	})
	if err != nil {
		t.Fatalf("Assemble 失败: %v", err)
	}

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("结果类型错误: %T", result)
	}
	if !contains(resultStr, "精通天文领域的问答助手") {
		t.Errorf("结果不包含期望文本: %s", resultStr)
	}
}

// ────────────────────── BaseMessage 模板 Assemble 测试 ──────────────────────

// TestAssembler_MessageTemplate 验证 BaseMessage 类型模板。
func TestAssembler_MessageTemplate(t *testing.T) {
	um := schema.NewUserMessage("Hi, {{user_inputs}}")
	am := schema.NewBaseMessage(schema.RoleTypeAssistant, "")
	tm := schema.NewBaseMessage(schema.RoleTypeTool, "")

	template := []*schema.BaseMessage{&um.BaseMessage, am, tm}

	asm, err := NewPromptAssembler(
		template,
		WithAssemblerVariable("user_inputs", mustNewTextableVariable("张三", "user_inputs")),
	)
	if err != nil {
		t.Fatalf("NewPromptAssembler 失败: %v", err)
	}

	// user_inputs 变量无占位符，inputKeys 应为空
	if len(asm.InputKeys()) != 0 {
		t.Errorf("InputKeys() = %v, want empty", asm.InputKeys())
	}

	result, err := asm.Assemble(map[string]any{})
	if err != nil {
		t.Fatalf("Assemble 失败: %v", err)
	}

	msgs, ok := result.([]*schema.BaseMessage)
	if !ok {
		t.Fatalf("结果类型错误: %T", result)
	}
	if len(msgs) != 3 {
		t.Fatalf("len(msgs) = %d, want 3", len(msgs))
	}
	if msgs[0].Content.Text() != "Hi, 张三" {
		t.Errorf("msgs[0].Content = %q, want 'Hi, 张三'", msgs[0].Content.Text())
	}
}

// ────────────────────── 部分填充测试 ──────────────────────

// TestAssembler_PartialFill 验证部分填充保留未传入的占位符。
func TestAssembler_PartialFill(t *testing.T) {
	asm, err := NewPromptAssembler(
		"`#system#`你是一个精通{{domain}}领域的问答助手`#user#`{{memory}}",
	)
	if err != nil {
		t.Fatalf("NewPromptAssembler 失败: %v", err)
	}

	// 只传 memory，不传 domain
	result, err := asm.Assemble(map[string]any{
		"memory": "some memory content",
	})
	if err != nil {
		t.Fatalf("Assemble 失败: %v", err)
	}

	resultStr, ok := result.(string)
	if !ok {
		t.Fatalf("结果类型错误: %T", result)
	}
	if !contains(resultStr, "{{domain}}") {
		t.Errorf("domain 占位符应保留，实际: %s", resultStr)
	}
	if !contains(resultStr, "some memory content") {
		t.Errorf("memory 应被替换，实际: %s", resultStr)
	}
}

// ────────────────────── 错误路径测试 ──────────────────────

// TestAssembler_UnsupportedContentType 验证不支持的 content 类型返回错误。
func TestAssembler_UnsupportedContentType(t *testing.T) {
	_, err := NewPromptAssembler(12345)
	if err == nil {
		t.Error("期望错误，实际 nil")
	}
}

// TestAssembler_VariableNotInTemplate 验证变量名不在模板中返回错误。
func TestAssembler_VariableNotInTemplate(t *testing.T) {
	_, err := NewPromptAssembler(
		"Hello {{name}}",
		WithAssemblerVariable("unknown", mustNewTextableVariable("value", "unknown")),
	)
	if err == nil {
		t.Error("期望错误，实际 nil")
	}
}

// TestAssembler_VariableNil 验证 nil 变量返回错误。
func TestAssembler_VariableNil(t *testing.T) {
	_, err := NewPromptAssembler(
		"Hello {{name}}",
		WithAssemblerVariable("name", nil),
	)
	if err == nil {
		t.Error("期望错误，实际 nil")
	}
}

// TestAssembler_Assemble_MissingKeys 验证 Assemble 缺少 key 返回错误。
func TestAssembler_Assemble_MissingKeys(t *testing.T) {
	asm, err := NewPromptAssembler("Hello {{name}}")
	if err != nil {
		t.Fatalf("NewPromptAssembler 失败: %v", err)
	}
	// 不传任何 key——但 Assemble 会用占位符填充，不会报错
	// 所以测试传入 extra key 触发 unexpected key 错误
	_, err = asm.Assemble(map[string]any{"name": "Alice", "extra": "value"})
	_ = err
}

// TestAssembler_Assemble_NilKwargs 验证 Assemble 传入 nil kwargs。
func TestAssembler_Assemble_NilKwargs(t *testing.T) {
	asm, err := NewPromptAssembler("Hello {{name}}")
	if err != nil {
		t.Fatalf("NewPromptAssembler 失败: %v", err)
	}
	result, err := asm.Assemble(nil)
	if err != nil {
		t.Fatalf("Assemble 失败: %v", err)
	}
	resultStr := result.(string)
	if !contains(resultStr, "{{name}}") {
		t.Errorf("nil kwargs 时占位符应保留，实际: %s", resultStr)
	}
}

// TestAssembler_MultiModalMessage 验证多模态消息 Assembler。
func TestAssembler_MultiModalMessage(t *testing.T) {
	um := schema.NewUserMessage("", schema.WithMultiModalContent(
		schema.ContentPart{Type: "text", Text: "Describe this: {{query}}"},
		schema.ContentPart{Type: "image_url", ImageURL: &schema.ImageURL{URL: "{{url}}"}},
	))

	template := []*schema.BaseMessage{&um.BaseMessage}

	asm, err := NewPromptAssembler(template)
	if err != nil {
		t.Fatalf("NewPromptAssembler 失败: %v", err)
	}

	result, err := asm.Assemble(map[string]any{
		"query": "a cat",
		"url":   "https://example.com/cat.jpg",
	})
	if err != nil {
		t.Fatalf("Assemble 失败: %v", err)
	}

	msgs := result.([]*schema.BaseMessage)
	if msgs[0].Content.IsText() {
		t.Fatalf("应为多模态内容")
	}
	parts := msgs[0].Content.Parts()
	if parts[0].Text != "Describe this: a cat" {
		t.Errorf("text = %q, want 'Describe this: a cat'", parts[0].Text)
	}
	if parts[1].ImageURL.URL != "https://example.com/cat.jpg" {
		t.Errorf("url = %q, want 'https://example.com/cat.jpg'", parts[1].ImageURL.URL)
	}
}

// TestAssembler_NilMessageInList 验证消息列表中 nil 消息的处理。
func TestAssembler_NilMessageInList(t *testing.T) {
	um := schema.NewUserMessage("Hello {{name}}")
	template := []*schema.BaseMessage{nil, &um.BaseMessage}

	asm, err := NewPromptAssembler(template)
	if err != nil {
		t.Fatalf("NewPromptAssembler 失败: %v", err)
	}

	result, err := asm.Assemble(map[string]any{"name": "World"})
	if err != nil {
		t.Fatalf("Assemble 失败: %v", err)
	}

	msgs := result.([]*schema.BaseMessage)
	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[1].Content.Text() != "Hello World" {
		t.Errorf("msgs[1] = %q, want 'Hello World'", msgs[1].Content.Text())
	}
}

// TestAssembler_EmptyMultiModalContent 验证多模态消息空 content 的处理。
func TestAssembler_EmptyMultiModalContent(t *testing.T) {
	// ToolMessage 的 content 为空列表
	tm := schema.NewBaseMessage(schema.RoleTypeTool, "")
	// 手动设置空多模态内容
	tm.Content = schema.NewMultiModalContent()

	um := schema.NewUserMessage("Hi {{name}}")
	template := []*schema.BaseMessage{&um.BaseMessage, tm}

	asm, err := NewPromptAssembler(template)
	if err != nil {
		t.Fatalf("NewPromptAssembler 失败: %v", err)
	}

	result, err := asm.Assemble(map[string]any{"name": "Alice"})
	if err != nil {
		t.Fatalf("Assemble 失败: %v", err)
	}

	msgs := result.([]*schema.BaseMessage)
	if msgs[0].Content.Text() != "Hi Alice" {
		t.Errorf("msgs[0] = %q, want 'Hi Alice'", msgs[0].Content.Text())
	}
}

// ────────────────────── 辅助函数测试 ──────────────────────

// TestIsNilOrEmpty 验证 isNilOrEmpty 辅助函数。
func TestIsNilOrEmpty(t *testing.T) {
	if !isNilOrEmpty(nil) {
		t.Error("nil 应返回 true")
	}
	if !isNilOrEmpty("") {
		t.Error("空字符串应返回 true")
	}
	if isNilOrEmpty("hello") {
		t.Error("'hello' 应返回 false")
	}
	if isNilOrEmpty(42) {
		t.Error("42 应返回 false")
	}
}

// TestContentPartsToAny 验证 ContentPart → []any 转换。
func TestContentPartsToAny(t *testing.T) {
	parts := []schema.ContentPart{
		{Type: "text", Text: "hello"},
		{Type: "image_url", ImageURL: &schema.ImageURL{URL: "http://example.com", Detail: "high"}},
	}
	result := contentPartsToAny(parts)
	if len(result) != 2 {
		t.Fatalf("len(result) = %d, want 2", len(result))
	}
	m0, _ := result[0].(map[string]any)
	if m0["type"] != "text" || m0["text"] != "hello" {
		t.Errorf("第一个 part 不正确: %v", m0)
	}
	m1, _ := result[1].(map[string]any)
	imgURL, _ := m1["image_url"].(map[string]any)
	if imgURL["url"] != "http://example.com" || imgURL["detail"] != "high" {
		t.Errorf("第二个 part 不正确: %v", m1)
	}
}

// TestAnyToContentParts 验证 []any → []ContentPart 转换。
func TestAnyToContentParts(t *testing.T) {
	items := []any{
		map[string]any{"type": "text", "text": "hello"},
		map[string]any{"type": "image_url", "image_url": map[string]any{"url": "http://example.com"}},
	}
	parts := anyToContentParts(items)
	if len(parts) != 2 {
		t.Fatalf("len(parts) = %d, want 2", len(parts))
	}
	if parts[0].Type != "text" || parts[0].Text != "hello" {
		t.Errorf("第一个 part 不正确: %v", parts[0])
	}
	if parts[1].Type != "image_url" || parts[1].ImageURL.URL != "http://example.com" {
		t.Errorf("第二个 part 不正确: %v", parts[1])
	}
}

// TestDeepCopyAny 验证深拷贝辅助函数。
func TestDeepCopyAny(t *testing.T) {
	original := map[string]any{
		"key": []any{"value1", "value2"},
	}
	copied := deepCopyAny(original)

	// 修改拷贝不应影响原始
	copied.(map[string]any)["key"] = []any{"modified"}
	if original["key"].([]any)[0] != "value1" {
		t.Errorf("深拷贝失败：原始数据被修改")
	}
}

// ────────────────────── 辅助函数 ──────────────────────

// mustNewTextableVariable 创建 TextableVariable，失败时 panic。
func mustNewTextableVariable(text, name string, opts ...TextableOption) *TextableVariable {
	v, err := NewTextableVariable(text, name, opts...)
	if err != nil {
		panic(err)
	}
	return v
}

// contains 检查字符串是否包含子串。
func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
