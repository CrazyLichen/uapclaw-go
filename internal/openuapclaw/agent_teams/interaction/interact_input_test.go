package interaction

import "testing"

// TestInteractInput_NewInteractInput 测试 NewInteractInput 构造
func TestInteractInput_NewInteractInput(t *testing.T) {
	input := NewInteractInput("hello")
	if input.Raw != "hello" {
		t.Errorf("Raw = %v, want hello", input.Raw)
	}

	mapInput := NewInteractInput(map[string]any{"key": "value"})
	m, ok := mapInput.Raw.(map[string]any)
	if !ok {
		t.Errorf("Raw 断言 map[string]any 失败")
	}
	if m["key"] != "value" {
		t.Errorf("m[key] = %v, want value", m["key"])
	}

	nilInput := NewInteractInput(nil)
	if nilInput.Raw != nil {
		t.Errorf("Raw = %v, want nil", nilInput.Raw)
	}
}
