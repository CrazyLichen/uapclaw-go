package schema

import (
	"encoding/json"
	"testing"
)

// ────────────────────── RoleType 测试 ──────────────────────

// TestRoleType_String 验证枚举字符串表示。
func TestRoleType_String(t *testing.T) {
	tests := []struct {
		role    RoleType
		want    string
	}{
		{RoleTypeSystem, "system"},
		{RoleTypeUser, "user"},
		{RoleTypeAssistant, "assistant"},
		{RoleTypeTool, "tool"},
	}
	for _, tt := range tests {
		if got := tt.role.String(); got != tt.want {
			t.Errorf("RoleType(%d).String() = %q, want %q", tt.role, got, tt.want)
		}
	}
}

// TestRoleType_MarshalJSON 验证枚举 JSON 序列化。
func TestRoleType_MarshalJSON(t *testing.T) {
	tests := []struct {
		role RoleType
		want string
	}{
		{RoleTypeSystem, `"system"`},
		{RoleTypeUser, `"user"`},
		{RoleTypeAssistant, `"assistant"`},
		{RoleTypeTool, `"tool"`},
	}
	for _, tt := range tests {
		data, err := json.Marshal(tt.role)
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		if string(data) != tt.want {
			t.Errorf("MarshalJSON(%d) = %s, want %s", tt.role, data, tt.want)
		}
	}
}

// TestRoleType_UnmarshalJSON 验证枚举 JSON 反序列化。
func TestRoleType_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		jsonStr string
		want    RoleType
	}{
		{`"system"`, RoleTypeSystem},
		{`"user"`, RoleTypeUser},
		{`"assistant"`, RoleTypeAssistant},
		{`"tool"`, RoleTypeTool},
	}
	for _, tt := range tests {
		var role RoleType
		if err := json.Unmarshal([]byte(tt.jsonStr), &role); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}
		if role != tt.want {
			t.Errorf("UnmarshalJSON(%s) = %d, want %d", tt.jsonStr, role, tt.want)
		}
	}
}

// TestRoleType_UnmarshalJSON_Unknown 验证未知角色类型报错。
func TestRoleType_UnmarshalJSON_Unknown(t *testing.T) {
	var role RoleType
	err := json.Unmarshal([]byte(`"unknown"`), &role)
	if err == nil {
		t.Error("未知 RoleType 应返回错误")
	}
}

// ────────────────────── MessageContent 测试 ──────────────────────

// TestNewTextContent 验证纯文本内容创建。
func TestNewTextContent(t *testing.T) {
	c := NewTextContent("hello")
	if !c.IsText() {
		t.Error("应为纯文本内容")
	}
	if c.Text() != "hello" {
		t.Errorf("Text() = %q, want %q", c.Text(), "hello")
	}
	if c.Parts() != nil {
		t.Error("纯文本内容 Parts() 应为 nil")
	}
}

// TestNewMultiModalContent 验证多模态内容创建。
func TestNewMultiModalContent(t *testing.T) {
	parts := []ContentPart{
		{Type: "text", Text: "描述下这张图片"},
		{Type: "image_url", ImageURL: &ImageURL{URL: "https://example.com/img.png"}},
	}
	c := NewMultiModalContent(parts...)
	if c.IsText() {
		t.Error("应为多模态内容")
	}
	if len(c.Parts()) != 2 {
		t.Errorf("Parts() 长度 = %d, want 2", len(c.Parts()))
	}
	if c.Text() != "" {
		t.Errorf("多模态内容 Text() 应为空，got %q", c.Text())
	}
}

// TestMessageContent_MarshalJSON_Text 验证纯文本序列化为 JSON string。
func TestMessageContent_MarshalJSON_Text(t *testing.T) {
	c := NewTextContent("你好")
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	if string(data) != `"你好"` {
		t.Errorf("MarshalJSON = %s, want %q", data, `"你好"`)
	}
}

// TestMessageContent_MarshalJSON_MultiModal 验证多模态序列化为 JSON array。
func TestMessageContent_MarshalJSON_MultiModal(t *testing.T) {
	c := NewMultiModalContent(
		ContentPart{Type: "text", Text: "hi"},
	)
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var result []ContentPart
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("解析序列化结果失败: %v", err)
	}
	if len(result) != 1 || result[0].Type != "text" || result[0].Text != "hi" {
		t.Errorf("多模态序列化结果不正确: %s", data)
	}
}

// TestMessageContent_UnmarshalJSON_String 验证 JSON string 反序列化为纯文本。
func TestMessageContent_UnmarshalJSON_String(t *testing.T) {
	var c MessageContent
	if err := json.Unmarshal([]byte(`"hello world"`), &c); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if !c.IsText() {
		t.Error("应为纯文本内容")
	}
	if c.Text() != "hello world" {
		t.Errorf("Text() = %q, want %q", c.Text(), "hello world")
	}
}

// TestMessageContent_UnmarshalJSON_Array 验证 JSON array 反序列化为多模态。
func TestMessageContent_UnmarshalJSON_Array(t *testing.T) {
	jsonStr := `[{"type":"text","text":"描述下这张图片"},{"type":"image_url","image_url":{"url":"https://example.com/img.png"}}]`
	var c MessageContent
	if err := json.Unmarshal([]byte(jsonStr), &c); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if c.IsText() {
		t.Error("应为多模态内容")
	}
	if len(c.Parts()) != 2 {
		t.Errorf("Parts() 长度 = %d, want 2", len(c.Parts()))
	}
	if c.Parts()[0].Type != "text" {
		t.Errorf("Parts()[0].Type = %q, want %q", c.Parts()[0].Type, "text")
	}
	if c.Parts()[1].ImageURL == nil || c.Parts()[1].ImageURL.URL != "https://example.com/img.png" {
		t.Error("Parts()[1].ImageURL 不正确")
	}
}

// TestMessageContent_RoundTrip_Text 验证纯文本序列化/反序列化一致性。
func TestMessageContent_RoundTrip_Text(t *testing.T) {
	original := NewTextContent("测试消息")
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var restored MessageContent
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.Text() != original.Text() {
		t.Errorf("Text(): got %q, want %q", restored.Text(), original.Text())
	}
}

// TestMessageContent_RoundTrip_MultiModal 验证多模态序列化/反序列化一致性。
func TestMessageContent_RoundTrip_MultiModal(t *testing.T) {
	original := NewMultiModalContent(
		ContentPart{Type: "text", Text: "看图"},
		ContentPart{Type: "image_url", ImageURL: &ImageURL{URL: "https://example.com/a.png", Detail: "high"}},
	)
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	var restored MessageContent
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.IsText() {
		t.Error("应为多模态内容")
	}
	if len(restored.Parts()) != 2 {
		t.Fatalf("Parts() 长度 = %d, want 2", len(restored.Parts()))
	}
	if restored.Parts()[0].Text != "看图" {
		t.Errorf("Parts()[0].Text = %q, want %q", restored.Parts()[0].Text, "看图")
	}
	if restored.Parts()[1].ImageURL.URL != "https://example.com/a.png" {
		t.Errorf("Parts()[1].ImageURL.URL 不正确")
	}
}

// ────────────────────── BaseMessage 测试 ──────────────────────

// TestNewBaseMessage 验证构造函数 + 选项。
func TestNewBaseMessage(t *testing.T) {
	msg := NewBaseMessage(RoleTypeUser, "hello",
		WithMessageName("alice"),
		WithMetadata(map[string]any{"key": "value"}),
	)
	if msg.Role != RoleTypeUser {
		t.Errorf("Role = %v, want %v", msg.Role, RoleTypeUser)
	}
	if msg.Content.Text() != "hello" {
		t.Errorf("Content = %q, want %q", msg.Content.Text(), "hello")
	}
	if msg.Name != "alice" {
		t.Errorf("Name = %q, want %q", msg.Name, "alice")
	}
	if msg.Metadata["key"] != "value" {
		t.Errorf("Metadata[key] = %v, want %q", msg.Metadata["key"], "value")
	}
}

// TestNewBaseMessage_WithMultiModalContent 验证多模态选项。
func TestNewBaseMessage_WithMultiModalContent(t *testing.T) {
	msg := NewBaseMessage(RoleTypeUser, "",
		WithMultiModalContent(
			ContentPart{Type: "text", Text: "看图"},
			ContentPart{Type: "image_url", ImageURL: &ImageURL{URL: "https://example.com/a.png"}},
		),
	)
	if msg.Content.IsText() {
		t.Error("应为多模态内容")
	}
	if len(msg.Content.Parts()) != 2 {
		t.Errorf("Parts() 长度 = %d, want 2", len(msg.Content.Parts()))
	}
}

// TestNewUserMessage 验证 role 固定为 "user"。
func TestNewUserMessage(t *testing.T) {
	msg := NewUserMessage("你好")
	if msg.Role != RoleTypeUser {
		t.Errorf("Role = %v, want %v", msg.Role, RoleTypeUser)
	}
	if msg.Content.Text() != "你好" {
		t.Errorf("Content = %q, want %q", msg.Content.Text(), "你好")
	}
}

// TestNewSystemMessage 验证 role 固定为 "system"。
func TestNewSystemMessage(t *testing.T) {
	msg := NewSystemMessage("你是一个助手")
	if msg.Role != RoleTypeSystem {
		t.Errorf("Role = %v, want %v", msg.Role, RoleTypeSystem)
	}
	if msg.Content.Text() != "你是一个助手" {
		t.Errorf("Content = %q, want %q", msg.Content.Text(), "你是一个助手")
	}
}

// TestBaseMessage_JSONRoundTrip 验证 BaseMessage 完整序列化/反序列化。
func TestBaseMessage_JSONRoundTrip(t *testing.T) {
	original := NewBaseMessage(RoleTypeUser, "测试消息",
		WithMessageName("test_user"),
		WithMetadata(map[string]any{"session": "123"}),
	)
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored BaseMessage
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.Role != original.Role {
		t.Errorf("Role: got %v, want %v", restored.Role, original.Role)
	}
	if restored.Content.Text() != original.Content.Text() {
		t.Errorf("Content: got %q, want %q", restored.Content.Text(), original.Content.Text())
	}
	if restored.Name != original.Name {
		t.Errorf("Name: got %q, want %q", restored.Name, original.Name)
	}
}

// TestUserMessage_JSONRoundTrip 验证 UserMessage JSON 序列化/反序列化。
func TestUserMessage_JSONRoundTrip(t *testing.T) {
	original := NewUserMessage("你好世界")
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored UserMessage
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.Role != RoleTypeUser {
		t.Errorf("Role: got %v, want %v", restored.Role, RoleTypeUser)
	}
	if restored.Content.Text() != "你好世界" {
		t.Errorf("Content: got %q, want %q", restored.Content.Text(), "你好世界")
	}
}

// TestSystemMessage_JSONRoundTrip 验证 SystemMessage JSON 序列化/反序列化。
func TestSystemMessage_JSONRoundTrip(t *testing.T) {
	original := NewSystemMessage("系统提示")
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored SystemMessage
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.Role != RoleTypeSystem {
		t.Errorf("Role: got %v, want %v", restored.Role, RoleTypeSystem)
	}
}

// TestBaseMessage_EmptyContent 验证空内容消息的序列化。
func TestBaseMessage_EmptyContent(t *testing.T) {
	msg := NewUserMessage("")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}
	// 验证 content 字段是空字符串
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if content, ok := result["content"].(string); !ok || content != "" {
		t.Errorf("content = %v, want empty string", result["content"])
	}
}
