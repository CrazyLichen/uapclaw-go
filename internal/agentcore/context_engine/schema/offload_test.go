package schema

import (
	"encoding/json"
	"fmt"
	"testing"

	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewOffloadUserMessage 验证卸载用户消息构造
func TestNewOffloadUserMessage(t *testing.T) {
	msg := NewOffloadUserMessage("截断内容...", "handle-123", "in_memory")
	if msg.GetRole() != llm_schema.RoleTypeUser {
		t.Errorf("Role = %v, want %v", msg.GetRole(), llm_schema.RoleTypeUser)
	}
	if msg.GetContent().Text() != "截断内容..." {
		t.Errorf("Content = %q, want %q", msg.GetContent().Text(), "截断内容...")
	}
	info := msg.GetOffloadInfo()
	if info.OffloadType != "in_memory" {
		t.Errorf("OffloadType = %q, want %q", info.OffloadType, "in_memory")
	}
	if info.OffloadHandle != "handle-123" {
		t.Errorf("OffloadHandle = %q, want %q", info.OffloadHandle, "handle-123")
	}
}

// TestNewOffloadAssistantMessage 验证卸载助手消息构造
func TestNewOffloadAssistantMessage(t *testing.T) {
	msg := NewOffloadAssistantMessage("摘要内容", "handle-456", "filesystem")
	if msg.GetRole() != llm_schema.RoleTypeAssistant {
		t.Errorf("Role = %v, want %v", msg.GetRole(), llm_schema.RoleTypeAssistant)
	}
	info := msg.GetOffloadInfo()
	if info.OffloadType != "filesystem" {
		t.Errorf("OffloadType = %q, want %q", info.OffloadType, "filesystem")
	}
}

// TestNewOffloadSystemMessage 验证卸载系统消息构造
func TestNewOffloadSystemMessage(t *testing.T) {
	msg := NewOffloadSystemMessage("系统消息", "handle-789", "in_memory")
	if msg.GetRole() != llm_schema.RoleTypeSystem {
		t.Errorf("Role = %v, want %v", msg.GetRole(), llm_schema.RoleTypeSystem)
	}
}

// TestNewOffloadToolMessage 验证卸载工具消息构造
func TestNewOffloadToolMessage(t *testing.T) {
	msg := NewOffloadToolMessage("call_001", "工具结果", "handle-tool", "in_memory")
	if msg.GetRole() != llm_schema.RoleTypeTool {
		t.Errorf("Role = %v, want %v", msg.GetRole(), llm_schema.RoleTypeTool)
	}
	if msg.ToolCallID != "call_001" {
		t.Errorf("ToolCallID = %q, want %q", msg.ToolCallID, "call_001")
	}
}

// TestNewOffloadMessage_工厂函数 验证按 role 分派
func TestNewOffloadMessage_工厂函数(t *testing.T) {
	tests := []struct {
		role     llm_schema.RoleType
		wantType string
	}{
		{llm_schema.RoleTypeUser, "*schema.OffloadUserMessage"},
		{llm_schema.RoleTypeAssistant, "*schema.OffloadAssistantMessage"},
		{llm_schema.RoleTypeSystem, "*schema.OffloadSystemMessage"},
		{llm_schema.RoleTypeTool, "*schema.OffloadToolMessage"},
	}
	for _, tt := range tests {
		msg := NewOffloadMessage(tt.role, "content", "handle", "in_memory")
		gotType := fmt.Sprintf("%T", msg)
		if gotType != tt.wantType {
			t.Errorf("role=%v: got %v, want %v", tt.role, gotType, tt.wantType)
		}
	}
}

// TestIsOffloaded 验证 Offloadable 接口断言
func TestIsOffloaded(t *testing.T) {
	// Offload 消息应返回 true
	offloadMsg := NewOffloadUserMessage("content", "handle", "in_memory")
	if !IsOffloaded(offloadMsg) {
		t.Error("OffloadUserMessage 应被识别为 Offloadable")
	}

	// 普通消息应返回 false
	normalMsg := llm_schema.NewUserMessage("普通消息")
	if IsOffloaded(normalMsg) {
		t.Error("UserMessage 不应被识别为 Offloadable")
	}

	// 通过 BaseMessage 接口传递后仍能正确判断
	var baseMsg llm_schema.BaseMessage = offloadMsg
	if !IsOffloaded(baseMsg) {
		t.Error("通过 BaseMessage 接口传递的 Offload 消息应被识别为 Offloadable")
	}
}

// TestOffloadable_接口兼容性 验证 Offload 消息可赋值给 BaseMessage 接口
func TestOffloadable_接口兼容性(t *testing.T) {
	var _ llm_schema.BaseMessage = NewOffloadUserMessage("c", "h", "t")
	var _ llm_schema.BaseMessage = NewOffloadAssistantMessage("c", "h", "t")
	var _ llm_schema.BaseMessage = NewOffloadSystemMessage("c", "h", "t")
	var _ llm_schema.BaseMessage = NewOffloadToolMessage("id", "c", "h", "t")
}

// TestOffloadUserMessage_JSONRoundTrip 验证 OffloadUserMessage JSON 往返
func TestOffloadUserMessage_JSONRoundTrip(t *testing.T) {
	original := NewOffloadUserMessage("截断内容", "handle-abc", "in_memory",
		llm_schema.WithMessageName("test_user"),
	)
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored OffloadUserMessage
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.GetRole() != llm_schema.RoleTypeUser {
		t.Errorf("Role = %v, want %v", restored.GetRole(), llm_schema.RoleTypeUser)
	}
	if restored.GetContent().Text() != "截断内容" {
		t.Errorf("Content = %q, want %q", restored.GetContent().Text(), "截断内容")
	}
	if restored.OffloadInfo.OffloadHandle != "handle-abc" {
		t.Errorf("OffloadHandle = %q, want %q", restored.OffloadInfo.OffloadHandle, "handle-abc")
	}
	if restored.OffloadInfo.OffloadType != "in_memory" {
		t.Errorf("OffloadType = %q, want %q", restored.OffloadInfo.OffloadType, "in_memory")
	}
	if restored.GetName() != "test_user" {
		t.Errorf("Name = %q, want %q", restored.GetName(), "test_user")
	}
}

// TestOffloadToolMessage_JSONRoundTrip 验证 OffloadToolMessage JSON 往返
func TestOffloadToolMessage_JSONRoundTrip(t *testing.T) {
	original := NewOffloadToolMessage("call_001", "工具结果", "handle-tool", "filesystem")
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored OffloadToolMessage
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.GetRole() != llm_schema.RoleTypeTool {
		t.Errorf("Role = %v, want %v", restored.GetRole(), llm_schema.RoleTypeTool)
	}
	if restored.ToolCallID != "call_001" {
		t.Errorf("ToolCallID = %q, want %q", restored.ToolCallID, "call_001")
	}
	if restored.OffloadInfo.OffloadType != "filesystem" {
		t.Errorf("OffloadType = %q, want %q", restored.OffloadInfo.OffloadType, "filesystem")
	}
}

// TestOffloadAssistantMessage_JSONRoundTrip 验证 OffloadAssistantMessage JSON 往返（含 ToolCalls）
func TestOffloadAssistantMessage_JSONRoundTrip(t *testing.T) {
	original := NewOffloadAssistantMessage("助手摘要", "handle-asst", "in_memory",
		llm_schema.WithToolCalls([]*llm_schema.ToolCall{
			llm_schema.NewToolCall("call_001", "get_weather", `{"city":"Beijing"}`),
		}),
	)
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	// 验证 JSON 包含 offload 字段
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("解析 JSON 失败: %v", err)
	}
	if _, ok := raw["offload_type"]; !ok {
		t.Error("JSON 应包含 offload_type 字段")
	}
	if _, ok := raw["offload_handle"]; !ok {
		t.Error("JSON 应包含 offload_handle 字段")
	}
	if _, ok := raw["tool_calls"]; !ok {
		t.Error("JSON 应包含 tool_calls 字段")
	}

	// 反序列化验证
	var restored OffloadAssistantMessage
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.GetRole() != llm_schema.RoleTypeAssistant {
		t.Errorf("Role = %v, want %v", restored.GetRole(), llm_schema.RoleTypeAssistant)
	}
	if restored.OffloadInfo.OffloadHandle != "handle-asst" {
		t.Errorf("OffloadHandle = %q, want %q", restored.OffloadInfo.OffloadHandle, "handle-asst")
	}
	if len(restored.ToolCalls) != 1 {
		t.Errorf("ToolCalls 长度 = %d, want 1", len(restored.ToolCalls))
	}
	if restored.ToolCalls[0].Name != "get_weather" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", restored.ToolCalls[0].Name, "get_weather")
	}
}

// TestUnmarshalOffloadMessage_按role分派 验证反序列化工厂
func TestUnmarshalOffloadMessage_按role分派(t *testing.T) {
	tests := []struct {
		role     string
		wantType string
	}{
		{"user", "*schema.OffloadUserMessage"},
		{"assistant", "*schema.OffloadAssistantMessage"},
		{"system", "*schema.OffloadSystemMessage"},
		{"tool", "*schema.OffloadToolMessage"},
	}
	for _, tt := range tests {
		jsonData := `{"role":"` + tt.role + `","content":"test","offload_type":"in_memory","offload_handle":"h1"}`
		msg, err := UnmarshalOffloadMessage([]byte(jsonData))
		if err != nil {
			t.Errorf("role=%s: 反序列化失败: %v", tt.role, err)
			continue
		}
		gotType := fmt.Sprintf("%T", msg)
		if gotType != tt.wantType {
			t.Errorf("role=%s: got %v, want %v", tt.role, gotType, tt.wantType)
		}
	}
}

// TestUnmarshalOffloadMessage_不支持的角色 验证错误处理
func TestUnmarshalOffloadMessage_不支持的角色(t *testing.T) {
	jsonData := `{"role":"unknown","content":"test","offload_type":"in_memory","offload_handle":"h1"}`
	_, err := UnmarshalOffloadMessage([]byte(jsonData))
	if err == nil {
		t.Error("不支持的角色应返回错误")
	}
}

// TestOffloadInfo_Metadata 验证 OffloadInfo 附加元数据
func TestOffloadInfo_Metadata(t *testing.T) {
	msg := NewOffloadUserMessage("content", "handle", "in_memory")
	msg.OffloadInfo.Metadata = map[string]any{
		"original_tokens": 1500,
		"timestamp":       "2025-01-01T00:00:00Z",
	}
	info := msg.GetOffloadInfo()
	if info.Metadata["original_tokens"] != 1500 {
		t.Errorf("Metadata[original_tokens] = %v, want 1500", info.Metadata["original_tokens"])
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
