package tool_call

import "testing"

// TestNewBaseMethod 测试 BaseMethod 构造
func TestNewBaseMethod(t *testing.T) {
	config := map[string]any{
		"verbose":       1,
		"gen_model_id":  "test-model",
		"eval_model_id": "test-eval",
	}
	m := NewBaseMethod(config, nil)
	if !m.verbose {
		t.Error("expected verbose=true for verbose=1")
	}
	if m.config["gen_model_id"] != "test-model" {
		t.Error("gen_model_id not set correctly")
	}
}

// TestNewBaseMethod_VerboseBool 测试 bool 类型 verbose
func TestNewBaseMethod_VerboseBool(t *testing.T) {
	config := map[string]any{"verbose": true}
	m := NewBaseMethod(config, nil)
	if !m.verbose {
		t.Error("expected verbose=true")
	}
}

// TestNewBaseMethod_VerboseFalse 测试默认 verbose 为 false
func TestNewBaseMethod_VerboseFalse(t *testing.T) {
	config := map[string]any{}
	m := NewBaseMethod(config, nil)
	if m.verbose {
		t.Error("expected verbose=false by default")
	}
}

// TestGetConfigString 测试配置获取
func TestGetConfigString(t *testing.T) {
	config := map[string]any{"key": "value"}
	if got := getConfigString(config, "key"); got != "value" {
		t.Errorf("got %q, want %q", got, "value")
	}
	if got := getConfigString(config, "missing"); got != "" {
		t.Errorf("got %q, want empty string", got)
	}
}

// TestGetConfigInt 测试整数配置获取
func TestGetConfigInt(t *testing.T) {
	config := map[string]any{"num": 5}
	if got := getConfigInt(config, "num"); got != 5 {
		t.Errorf("got %d, want 5", got)
	}
	config2 := map[string]any{"num": float64(3)}
	if got := getConfigInt(config2, "num"); got != 3 {
		t.Errorf("got %d, want 3", got)
	}
}

// TestGetConfigFloat 测试浮点配置获取
func TestGetConfigFloat(t *testing.T) {
	config := map[string]any{"weight": 0.4}
	if got := getConfigFloat(config, "weight"); got != 0.4 {
		t.Errorf("got %f, want 0.4", got)
	}
	config2 := map[string]any{"weight": 1}
	if got := getConfigFloat(config2, "weight"); got != 1.0 {
		t.Errorf("got %f, want 1.0", got)
	}
}
