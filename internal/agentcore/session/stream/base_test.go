package stream

import "testing"

// TestStreamMode_String 测试 StreamMode 的字符串表示
func TestStreamMode_String(t *testing.T) {
	tests := []struct {
		mode     StreamMode
		expected string
	}{
		{StreamModeOutput, "output"},
		{StreamModeTrace, "trace"},
		{StreamModeCustom, "custom"},
		{StreamMode(99), "unknown"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.expected {
			t.Errorf("StreamMode(%d).String() = %q, want %q", tt.mode, got, tt.expected)
		}
	}
}

// TestOutputSchema_SchemaType 测试 OutputSchema 实现 Schema 接口
func TestOutputSchema_SchemaType(t *testing.T) {
	s := OutputSchema{Type: "message", Index: 0, Payload: "hello"}
	if s.SchemaType() != "message" {
		t.Errorf("SchemaType() = %q, want %q", s.SchemaType(), "message")
	}
}

// TestTraceSchema_SchemaType 测试 TraceSchema 实现 Schema 接口
func TestTraceSchema_SchemaType(t *testing.T) {
	s := TraceSchema{Type: "step", Payload: "data"}
	if s.SchemaType() != "step" {
		t.Errorf("SchemaType() = %q, want %q", s.SchemaType(), "step")
	}
}

// TestCustomSchema_SchemaType 测试 CustomSchema 实现 Schema 接口
func TestCustomSchema_SchemaType(t *testing.T) {
	s := CustomSchema{Type: "my_event", Data: map[string]any{"key": "val"}}
	if s.SchemaType() != "my_event" {
		t.Errorf("SchemaType() = %q, want %q", s.SchemaType(), "my_event")
	}
}

// TestSchema 接口多态测试
func TestSchema(t *testing.T) {
	var schemas = []Schema{
		OutputSchema{Type: "message", Index: 0, Payload: "hello"},
		TraceSchema{Type: "step", Payload: "data"},
		CustomSchema{Type: "event", Data: map[string]any{"k": "v"}},
	}
	expected := []string{"message", "step", "event"}
	for i, s := range schemas {
		if s.SchemaType() != expected[i] {
			t.Errorf("schemas[%d].SchemaType() = %q, want %q", i, s.SchemaType(), expected[i])
		}
	}
}
