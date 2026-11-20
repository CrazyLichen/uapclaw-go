package schema

import (
	"encoding/json"
	"testing"
)

// TestNewToolMessage 验证 role 固定为 "tool"，tool_call_id 必填。
func TestNewToolMessage(t *testing.T) {
	msg := NewToolMessage("call_123", `{"result": "sunny"}`)
	if msg.Role != RoleTypeTool {
		t.Errorf("Role = %v, want %v", msg.Role, RoleTypeTool)
	}
	if msg.ToolCallID != "call_123" {
		t.Errorf("ToolCallID = %q, want %q", msg.ToolCallID, "call_123")
	}
	if msg.Content.Text() != `{"result": "sunny"}` {
		t.Errorf("Content = %q, want %q", msg.Content.Text(), `{"result": "sunny"}`)
	}
}

// TestNewToolMessage_WithOptions 验证 MessageOption 对 ToolMessage 生效。
func TestNewToolMessage_WithOptions(t *testing.T) {
	msg := NewToolMessage("call_1", "ok",
		WithMessageName("weather_tool"),
		WithMetadata(map[string]any{"source": "api"}),
	)
	if msg.Name != "weather_tool" {
		t.Errorf("Name = %q, want %q", msg.Name, "weather_tool")
	}
	if msg.Metadata["source"] != "api" {
		t.Errorf("Metadata[source] = %v, want %q", msg.Metadata["source"], "api")
	}
}

// TestToolMessage_JSONRoundTrip 验证序列化/反序列化一致性。
func TestToolMessage_JSONRoundTrip(t *testing.T) {
	original := NewToolMessage("call_abc", `{"temp": 25}`,
		WithMessageName("weather"),
	)
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var restored ToolMessage
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if restored.Role != RoleTypeTool {
		t.Errorf("Role: got %v, want %v", restored.Role, RoleTypeTool)
	}
	if restored.ToolCallID != "call_abc" {
		t.Errorf("ToolCallID: got %q, want %q", restored.ToolCallID, "call_abc")
	}
	if restored.Content.Text() != `{"temp": 25}` {
		t.Errorf("Content: got %q, want %q", restored.Content.Text(), `{"temp": 25}`)
	}
	if restored.Name != "weather" {
		t.Errorf("Name: got %q, want %q", restored.Name, "weather")
	}
}

// TestToolMessage_JSONFormat 验证 JSON 输出格式正确。
func TestToolMessage_JSONFormat(t *testing.T) {
	msg := NewToolMessage("call_1", "result")
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if result["role"] != "tool" {
		t.Errorf("role = %v, want %q", result["role"], "tool")
	}
	if result["tool_call_id"] != "call_1" {
		t.Errorf("tool_call_id = %v, want %q", result["tool_call_id"], "call_1")
	}
	if result["content"] != "result" {
		t.Errorf("content = %v, want %q", result["content"], "result")
	}
}
