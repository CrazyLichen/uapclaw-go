package rails

import (
	"fmt"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

func TestBoolishFalse(t *testing.T) {
	tests := []struct {
		input any
		want  bool
	}{
		{false, true},
		{"false", true},
		{"False", true},
		{"FALSE", true},
		{"0", true},
		{"no", true},
		{"NO", true},
		{true, false},
		{"true", false},
		{1, false},
		{"yes", false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := boolishFalse(tt.input); got != tt.want {
			t.Errorf("boolishFalse(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestBoolishTrue(t *testing.T) {
	tests := []struct {
		input any
		want  bool
	}{
		{true, true},
		{"true", true},
		{"1", true},
		{"yes", true},
		{false, false},
		{"false", false},
		{0, false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := boolishTrue(tt.input); got != tt.want {
			t.Errorf("boolishTrue(%v) = %v, want %v", tt.input, got, tt.want)
		}
	}
}

func TestNonzeroExit(t *testing.T) {
	tests := []struct {
		input any
		want  *bool
	}{
		{false, nil},
		{0, boolPtr(false)},
		{1, boolPtr(true)},
		{-1, boolPtr(true)},
		{"0", boolPtr(false)},
		{"1", boolPtr(true)},
		{"-1", boolPtr(true)},
		{"abc", nil},
		{nil, nil},
	}
	for _, tt := range tests {
		got := nonzeroExit(tt.input)
		if (got == nil) != (tt.want == nil) {
			t.Errorf("nonzeroExit(%v) = %v, want %v", tt.input, got, tt.want)
		} else if got != nil && *got != *tt.want {
			t.Errorf("nonzeroExit(%v) = %v, want %v", tt.input, *got, *tt.want)
		}
	}
}

func TestInferToolResultError(t *testing.T) {
	// success=false
	got := inferToolResultError(map[string]any{"success": false})
	assertBoolPtr(t, got, true)

	// success=true
	got = inferToolResultError(map[string]any{"success": true})
	assertBoolPtr(t, got, false)

	// is_error=true
	got = inferToolResultError(map[string]any{"is_error": true})
	assertBoolPtr(t, got, true)

	// status="error"
	got = inferToolResultError(map[string]any{"status": "error"})
	assertBoolPtr(t, got, true)

	// exit_code != 0
	got = inferToolResultError(map[string]any{"exit_code": 1})
	assertBoolPtr(t, got, true)

	// exit_code == 0
	got = inferToolResultError(map[string]any{"exit_code": 0})
	assertBoolPtr(t, got, false)

	// nested data
	got = inferToolResultError(map[string]any{"data": map[string]any{"success": false}})
	assertBoolPtr(t, got, true)

	// normal string
	got = inferToolResultError("some result")
	if got != nil {
		t.Errorf("expected nil for plain string, got %v", got)
	}

	// [ERROR] prefix
	got = inferToolResultError("[ERROR] something failed")
	assertBoolPtr(t, got, true)

	// nil
	got = inferToolResultError(nil)
	if got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}

func TestStructuredToolResultPayload(t *testing.T) {
	if structuredToolResultPayload(map[string]any{"a": 1}) == nil {
		t.Error("expected non-nil for dict")
	}
	if structuredToolResultPayload([]any{1, 2}) == nil {
		t.Error("expected non-nil for list")
	}
	if structuredToolResultPayload("string") != nil {
		t.Error("expected nil for string")
	}
	if structuredToolResultPayload(42) != nil {
		t.Error("expected nil for int")
	}
}

func TestParseToolCallArguments(t *testing.T) {
	// 合法 JSON 参数
	got := parseToolCallArguments(&llmschema.ToolCall{Arguments: `{"key": "value"}`})
	if got["key"] != "value" {
		t.Errorf("expected key=value, got %v", got)
	}

	// 无效 JSON
	got = parseToolCallArguments(&llmschema.ToolCall{Arguments: `not json`})
	if len(got) != 0 {
		t.Errorf("expected empty map for invalid JSON, got %v", got)
	}

	// nil tool call
	got = parseToolCallArguments(nil)
	if len(got) != 0 {
		t.Errorf("expected empty map for nil, got %v", got)
	}

	// 空参数
	got = parseToolCallArguments(&llmschema.ToolCall{Arguments: ""})
	if len(got) != 0 {
		t.Errorf("expected empty map for empty args, got %v", got)
	}
}

func TestExtractToolInterrupt(t *testing.T) {
	// ToolInterruptException
	exc := &saschema.ToolInterruptException{Request: testInterruptRequest{msg: "test"}}
	got := extractToolInterrupt(exc)
	if got == nil {
		t.Error("expected non-nil for ToolInterruptException")
	}

	// nested cause via Unwrap
	wrapper := &testError{cause: exc}
	got = extractToolInterrupt(wrapper)
	if got == nil {
		t.Error("expected non-nil for nested ToolInterruptException")
	}

	// no interrupt
	got = extractToolInterrupt(fmt.Errorf("plain error"))
	if got != nil {
		t.Error("expected nil for plain error")
	}

	// nil
	got = extractToolInterrupt(nil)
	if got != nil {
		t.Error("expected nil for nil input")
	}
}

func TestTruncateString(t *testing.T) {
	if truncateString("hello", 3) != "hel" {
		t.Errorf("expected 'hel', got '%s'", truncateString("hello", 3))
	}
	if truncateString("hi", 10) != "hi" {
		t.Errorf("expected 'hi', got '%s'", truncateString("hi", 10))
	}
	if truncateString("", 5) != "" {
		t.Errorf("expected '', got '%s'", truncateString("", 5))
	}
}

func TestTodoToolNames(t *testing.T) {
	if !todoToolNames["todo_create"] {
		t.Error("expected todo_create in todoToolNames")
	}
	if !todoToolNames["todo_list"] {
		t.Error("expected todo_list in todoToolNames")
	}
	if !todoToolNames["todo_get"] {
		t.Error("expected todo_get in todoToolNames")
	}
	if !todoToolNames["todo_modify"] {
		t.Error("expected todo_modify in todoToolNames")
	}
	if todoToolNames["other_tool"] {
		t.Error("unexpected other_tool in todoToolNames")
	}
}

// ── 辅助 ──

func assertBoolPtr(t *testing.T, got *bool, want bool) {
	t.Helper()
	if got == nil {
		t.Errorf("expected %v, got nil", want)
	} else if *got != want {
		t.Errorf("expected %v, got %v", want, *got)
	}
}

type testError struct {
	cause error
}

func (e *testError) Error() string { return "test error" }
func (e *testError) Unwrap() error { return e.cause }

type testInterruptRequest struct{ msg string }

func (r testInterruptRequest) GetMessage() string        { return r.msg }
func (r testInterruptRequest) GetAutoConfirmKey() string { return "" }
