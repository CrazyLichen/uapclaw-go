package exception

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestStatusCode_Code(t *testing.T) {
	if StatusSuccess.Code() != 0 {
		t.Errorf("StatusSuccess.Code() 期望 0，实际 %d", StatusSuccess.Code())
	}
	if StatusError.Code() != -1 {
		t.Errorf("StatusError.Code() 期望 -1，实际 %d", StatusError.Code())
	}
}

func TestStatusCode_Message(t *testing.T) {
	if StatusSuccess.Message() != "success" {
		t.Errorf("StatusSuccess.Message() 期望 %q，实际 %q", "success", StatusSuccess.Message())
	}
	if StatusError.Message() != "error" {
		t.Errorf("StatusError.Message() 期望 %q，实际 %q", "error", StatusError.Message())
	}
}

func TestStatusCode_Name(t *testing.T) {
	if StatusSuccess.Name() != "SUCCESS" {
		t.Errorf("StatusSuccess.Name() 期望 %q，实际 %q", "SUCCESS", StatusSuccess.Name())
	}
	if StatusError.Name() != "ERROR" {
		t.Errorf("StatusError.Name() 期望 %q，实际 %q", "ERROR", StatusError.Name())
	}
}

func TestStatusCode_String(t *testing.T) {
	s := StatusSuccess.String()
	if s != "SUCCESS(0)" {
		t.Errorf("期望 %q，实际 %q", "SUCCESS(0)", s)
	}
	s = StatusError.String()
	if s != "ERROR(-1)" {
		t.Errorf("期望 %q，实际 %q", "ERROR(-1)", s)
	}
}

func TestStatusCode_GoString(t *testing.T) {
	s := StatusSuccess.GoString()
	expected := `StatusCode{Name:"SUCCESS", Code:0, Message:"success"}`
	if s != expected {
		t.Errorf("期望 %q，实际 %q", expected, s)
	}
}

func TestStatusCode_RenderMessage_AllParams(t *testing.T) {
	status := NewStatusCode("TEST", 1, "hello {name}, age={age}")
	result := status.RenderMessage(map[string]any{"name": "world", "age": 25})
	if result != "hello world, age=25" {
		t.Errorf("期望 %q，实际 %q", "hello world, age=25", result)
	}
}

func TestStatusCode_RenderMessage_MissingParam(t *testing.T) {
	status := NewStatusCode("TEST", 1, "hello {name}, age={age}")
	result := status.RenderMessage(map[string]any{"name": "world"})
	if result != "hello world, age=<missing:age>" {
		t.Errorf("期望 %q，实际 %q", "hello world, age=<missing:age>", result)
	}
}

func TestStatusCode_RenderMessage_NoParams(t *testing.T) {
	status := NewStatusCode("TEST", 1, "hello {name}")
	result := status.RenderMessage(nil)
	if result != "hello <missing:name>" {
		t.Errorf("期望 %q，实际 %q", "hello <missing:name>", result)
	}
}

func TestStatusCode_RenderMessage_EmptyTemplate(t *testing.T) {
	status := NewStatusCode("TEST", 1, "")
	result := status.RenderMessage(nil)
	if result != "" {
		t.Errorf("期望空字符串，实际 %q", result)
	}
}

func TestStatusCode_RenderMessage_NoPlaceholders(t *testing.T) {
	status := NewStatusCode("TEST", 1, "simple message")
	result := status.RenderMessage(nil)
	if result != "simple message" {
		t.Errorf("期望 %q，实际 %q", "simple message", result)
	}
}

func TestStatusCode_RenderMessage_ExtraParams(t *testing.T) {
	// 多余的 key 不影响渲染
	status := NewStatusCode("TEST", 1, "hello {name}")
	result := status.RenderMessage(map[string]any{"name": "world", "extra": "ignored"})
	if result != "hello world" {
		t.Errorf("期望 %q，实际 %q", "hello world", result)
	}
}

func TestStatusCode_RenderMessage_NonStringParam(t *testing.T) {
	status := NewStatusCode("TEST", 1, "count={count}, flag={flag}")
	result := status.RenderMessage(map[string]any{"count": 42, "flag": true})
	if result != "count=42, flag=true" {
		t.Errorf("期望 %q，实际 %q", "count=42, flag=true", result)
	}
}

func TestStatusCode_RenderMessage_NonPlaceholderBraces(t *testing.T) {
	// 非占位符形式的花括号（含特殊字符）应原样保留
	status := NewStatusCode("TEST", 1, "json {key} and {{literal}}")
	result := status.RenderMessage(map[string]any{"key": "value"})
	if result != "json value and {{literal}}" {
		t.Errorf("期望 %q，实际 %q", "json value and {{literal}}", result)
	}
}

func TestStatusCode_JSONSerialization(t *testing.T) {
	data, err := json.Marshal(StatusSuccess)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if raw["name"] != "SUCCESS" {
		t.Errorf("name 期望 %q，实际 %v", "SUCCESS", raw["name"])
	}
	if raw["code"] != float64(0) {
		t.Errorf("code 期望 0，实际 %v", raw["code"])
	}
	if raw["message"] != "success" {
		t.Errorf("message 期望 %q，实际 %v", "success", raw["message"])
	}
}

func TestStatusCode_JSONRoundTrip(t *testing.T) {
	original := NewStatusCode("TEST_CODE", 12345, "test message {param}")
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("JSON 序列化失败: %v", err)
	}

	var decoded StatusCode
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("JSON 反序列化失败: %v", err)
	}
	if decoded.Name() != original.Name() {
		t.Errorf("Name 期望 %q，实际 %q", original.Name(), decoded.Name())
	}
	if decoded.Code() != original.Code() {
		t.Errorf("Code 期望 %d，实际 %d", original.Code(), decoded.Code())
	}
	if decoded.Message() != original.Message() {
		t.Errorf("Message 期望 %q，实际 %q", original.Message(), decoded.Message())
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func TestIsValidPlaceholderName(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"name", true},
		{"comp_id", true},
		{"reason", true},
		{"timeout", true},
		{"error_msg", true},
		{"", false},
		{"a b", false},
		{"a-b", false},
		{"a.b", false},
		{"123", true}, // 纯数字也是合法的占位符名
	}
	for _, tt := range tests {
		if got := isValidPlaceholderName(tt.input); got != tt.expected {
			t.Errorf("isValidPlaceholderName(%q) = %v，期望 %v", tt.input, got, tt.expected)
		}
	}
}

func TestReplaceMissingPlaceholders(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello {name}", "hello <missing:name>"},
		{"{a} and {b}", "<missing:a> and <missing:b>"},
		{"no placeholders", "no placeholders"},
		{"{a} filled", "<missing:a> filled"},
	}
	for _, tt := range tests {
		if got := replaceMissingPlaceholders(tt.input); got != tt.expected {
			t.Errorf("replaceMissingPlaceholders(%q) = %q，期望 %q", tt.input, got, tt.expected)
		}
	}
}
