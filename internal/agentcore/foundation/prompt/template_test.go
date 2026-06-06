package prompt

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ────────────────────── Format 测试 ──────────────────────

// TestPromptTemplate_Format_StringTemplate 验证字符串模板完整填充。
func TestPromptTemplate_Format_StringTemplate(t *testing.T) {
	tmpl := NewPromptTemplate("", "`#system#`你是一个精通{{domain}}领域的问答助手`#user#`{{memory}}")

	formatted, err := tmpl.Format(map[string]any{
		"memory": "some context",
		"domain": "数学",
	})
	if err != nil {
		t.Fatalf("Format 失败: %v", err)
	}

	msgs, err := formatted.ToMessages()
	if err != nil {
		t.Fatalf("ToMessages 失败: %v", err)
	}

	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	content := msgs[0].Content.Text()
	if !contains(content, "精通数学领域的问答助手") {
		t.Errorf("内容不包含期望文本: %s", content)
	}
	if !contains(content, "some context") {
		t.Errorf("内容不包含期望文本: %s", content)
	}
}

// TestPromptTemplate_Format_PartialFill 验证部分填充保留未传入的占位符。
func TestPromptTemplate_Format_PartialFill(t *testing.T) {
	tmpl := NewPromptTemplate("", "`#system#`你是一个精通{{domain}}领域的问答助手`#user#`{{memory}}")

	// 只传 memory，不传 domain
	step1, err := tmpl.Format(map[string]any{
		"memory": "some context",
	})
	if err != nil {
		t.Fatalf("Format 失败: %v", err)
	}

	// domain 占位符应保留
	contentStr, ok := step1.Content.(string)
	if !ok {
		t.Fatalf("Content 类型错误: %T", step1.Content)
	}
	if !contains(contentStr, "{{domain}}") {
		t.Errorf("domain 占位符应保留，实际: %s", contentStr)
	}

	// 补传 domain，完成所有替换
	step2, err := step1.Format(map[string]any{
		"domain": "数学",
	})
	if err != nil {
		t.Fatalf("Format 失败: %v", err)
	}

	contentStr2, ok := step2.Content.(string)
	if !ok {
		t.Fatalf("Content 类型错误: %T", step2.Content)
	}
	if contains(contentStr2, "{{domain}}") {
		t.Errorf("domain 占位符不应再存在: %s", contentStr2)
	}
	if !contains(contentStr2, "精通数学领域的问答助手") {
		t.Errorf("内容不包含期望文本: %s", contentStr2)
	}
}

// TestPromptTemplate_Format_MessageTemplate 验证消息列表模板。
func TestPromptTemplate_Format_MessageTemplate(t *testing.T) {
	um := schema.NewUserMessage("Hello {{name}}!")
	am := schema.NewBaseMessage(schema.RoleTypeAssistant, "I'm your assistant for {{domain}}.")
	template := []*schema.BaseMessage{&um.BaseMessage, am}

	tmpl := NewPromptTemplate("", template)

	formatted, err := tmpl.Format(map[string]any{
		"name":   "Alice",
		"domain": "AI",
	})
	if err != nil {
		t.Fatalf("Format 失败: %v", err)
	}

	msgs, err := formatted.ToMessages()
	if err != nil {
		t.Fatalf("ToMessages 失败: %v", err)
	}

	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}
	if msgs[0].Content.Text() != "Hello Alice!" {
		t.Errorf("msgs[0] = %q, want 'Hello Alice!'", msgs[0].Content.Text())
	}
	if msgs[1].Content.Text() != "I'm your assistant for AI." {
		t.Errorf("msgs[1] = %q, want 'I'm your assistant for AI.'", msgs[1].Content.Text())
	}
}

// TestPromptTemplate_Format_EmptyKeywords 验证空 keywords 返回深拷贝。
func TestPromptTemplate_Format_EmptyKeywords(t *testing.T) {
	tmpl := NewPromptTemplate("", "Hello {{name}}")

	// nil keywords
	copy1, err := tmpl.Format(nil)
	if err != nil {
		t.Fatalf("Format 失败: %v", err)
	}
	if copy1.Content.(string) != tmpl.Content.(string) {
		t.Errorf("深拷贝内容不一致")
	}

	// 空 map
	copy2, err := tmpl.Format(map[string]any{})
	if err != nil {
		t.Fatalf("Format 失败: %v", err)
	}
	if copy2.Content.(string) != tmpl.Content.(string) {
		t.Errorf("深拷贝内容不一致")
	}
}

// TestPromptTemplate_Format_ExtraKeys 验证多余 keywords 被忽略。
func TestPromptTemplate_Format_ExtraKeys(t *testing.T) {
	tmpl := NewPromptTemplate("", "Hi {{name}}")

	formatted, err := tmpl.Format(map[string]any{
		"name": "Bob",
		"age":  20,
	})
	if err != nil {
		t.Fatalf("Format 失败: %v", err)
	}

	contentStr, ok := formatted.Content.(string)
	if !ok {
		t.Fatalf("Content 类型错误: %T", formatted.Content)
	}
	if contentStr != "Hi Bob" {
		t.Errorf("Content = %q, want 'Hi Bob'", contentStr)
	}
}

// ────────────────────── ToMessages 测试 ──────────────────────

// TestPromptTemplate_ToMessages_Empty 验证空内容返回 nil。
func TestPromptTemplate_ToMessages_Empty(t *testing.T) {
	tmpl := NewPromptTemplate("", "")
	msgs, err := tmpl.ToMessages()
	if err != nil {
		t.Fatalf("ToMessages 失败: %v", err)
	}
	if msgs != nil {
		t.Errorf("期望 nil，实际 %v", msgs)
	}
}

// TestPromptTemplate_ToMessages_String 验证字符串内容转为 UserMessage。
func TestPromptTemplate_ToMessages_String(t *testing.T) {
	tmpl := NewPromptTemplate("", "Hello world")
	msgs, err := tmpl.ToMessages()
	if err != nil {
		t.Fatalf("ToMessages 失败: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}
	if msgs[0].Role != schema.RoleTypeUser {
		t.Errorf("Role = %v, want RoleTypeUser", msgs[0].Role)
	}
	if msgs[0].Content.Text() != "Hello world" {
		t.Errorf("Content = %q, want 'Hello world'", msgs[0].Content.Text())
	}
}

// TestPromptTemplate_ToMessages_MessageList 验证消息列表深拷贝。
func TestPromptTemplate_ToMessages_MessageList(t *testing.T) {
	um := schema.NewUserMessage("original")
	template := []*schema.BaseMessage{&um.BaseMessage}

	tmpl := NewPromptTemplate("", template)
	msgs, err := tmpl.ToMessages()
	if err != nil {
		t.Fatalf("ToMessages 失败: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("len(msgs) = %d, want 1", len(msgs))
	}

	// 修改返回的 msgs 不应影响原始模板
	msgs[0].Content = schema.NewTextContent("modified")
	if tmpl.Content.([]*schema.BaseMessage)[0].Content.Text() != "original" {
		t.Errorf("深拷贝失败：原始内容被修改")
	}
}

// ────────────────────── InputKeys 测试 ──────────────────────

// TestPromptTemplate_InputKeys 验证获取模板中的占位符键名。
func TestPromptTemplate_InputKeys(t *testing.T) {
	tmpl := NewPromptTemplate("", "Hello {{name}}, {{user.age}}!")
	keys, err := tmpl.InputKeys()
	if err != nil {
		t.Fatalf("InputKeys 失败: %v", err)
	}
	if len(keys) != 2 {
		t.Errorf("InputKeys() = %v, want len 2", keys)
	}
}

// ────────────────────── 多模态消息测试 ──────────────────────

// TestPromptTemplate_Format_MultiModalMessage 验证多模态消息模板。
func TestPromptTemplate_Format_MultiModalMessage(t *testing.T) {
	sm := schema.NewSystemMessage("You are a helper.")
	um := schema.NewUserMessage("", schema.WithMultiModalContent(
		schema.ContentPart{Type: "text", Text: "Describe this: {{query}}"},
		schema.ContentPart{Type: "image_url", ImageURL: &schema.ImageURL{URL: "{{image_url}}"}},
	))
	template := []*schema.BaseMessage{&sm.BaseMessage, &um.BaseMessage}

	tmpl := NewPromptTemplate("", template)

	formatted, err := tmpl.Format(map[string]any{
		"query":     "a cute cat",
		"image_url": "https://picsum.photos/200",
	})
	if err != nil {
		t.Fatalf("Format 失败: %v", err)
	}

	msgs, err := formatted.ToMessages()
	if err != nil {
		t.Fatalf("ToMessages 失败: %v", err)
	}

	if len(msgs) != 2 {
		t.Fatalf("len(msgs) = %d, want 2", len(msgs))
	}

	// 系统消息不变
	if msgs[0].Content.Text() != "You are a helper." {
		t.Errorf("系统消息 = %q, want 'You are a helper.'", msgs[0].Content.Text())
	}

	// 用户消息：多模态内容
	if msgs[1].Content.IsText() {
		t.Fatalf("用户消息应为多模态")
	}
	parts := msgs[1].Content.Parts()
	if len(parts) != 2 {
		t.Fatalf("len(parts) = %d, want 2", len(parts))
	}
	if parts[0].Text != "Describe this: a cute cat" {
		t.Errorf("text part = %q, want 'Describe this: a cute cat'", parts[0].Text)
	}
	if parts[1].ImageURL == nil || parts[1].ImageURL.URL != "https://picsum.photos/200" {
		t.Errorf("image_url part 不正确")
	}
}

// ────────────────────── 错误路径测试 ──────────────────────

// TestPromptTemplate_ToMessages_InvalidType 验证非法 Content 类型。
func TestPromptTemplate_ToMessages_InvalidType(t *testing.T) {
	tmpl := NewPromptTemplate("", 12345)
	_, err := tmpl.ToMessages()
	if err == nil {
		t.Error("期望错误，实际 nil")
	}
}

// TestPromptTemplate_NewWithOpts 验证 WithTemplatePrefix / WithTemplateSuffix。
func TestPromptTemplate_NewWithOpts(t *testing.T) {
	tmpl := NewPromptTemplate("test", "Hi ${name}$",
		WithTemplatePrefix("${"),
		WithTemplateSuffix("}$"),
	)
	if tmpl.PlaceholderPrefix != "${" {
		t.Errorf("PlaceholderPrefix = %q, want ${", tmpl.PlaceholderPrefix)
	}
	if tmpl.PlaceholderSuffix != "}$" {
		t.Errorf("PlaceholderSuffix = %q, want }$", tmpl.PlaceholderSuffix)
	}
	if tmpl.Name != "test" {
		t.Errorf("Name = %q, want test", tmpl.Name)
	}
}

// TestPromptTemplate_Format_NilContent 验证 nil Content 的 Format。
func TestPromptTemplate_Format_NilContent(t *testing.T) {
	tmpl := NewPromptTemplate("", nil)
	copy, err := tmpl.Format(nil)
	if err != nil {
		t.Fatalf("Format 失败: %v", err)
	}
	if copy.Content != nil {
		t.Errorf("期望 nil Content，实际 %v", copy.Content)
	}
}

// TestPromptTemplate_DeepCopyContent_NonStandardType 验证非标准 Content 类型的深拷贝。
func TestPromptTemplate_DeepCopyContent_NonStandardType(t *testing.T) {
	tmpl := NewPromptTemplate("", 42)
	copy, err := tmpl.deepCopyContent()
	if err != nil {
		t.Fatalf("deepCopyContent 失败: %v", err)
	}
	if copy != 42 {
		t.Errorf("deepCopyContent = %v, want 42", copy)
	}
}
