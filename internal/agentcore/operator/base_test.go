package operator

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// mockOperator 用于测试 DefaultApplyUpdate
type mockOperator struct {
	operatorID string
	state      map[string]any
	callbacks  []struct {
		target string
		value  any
	}
}

func (m *mockOperator) OperatorID() string                    { return m.operatorID }
func (m *mockOperator) GetTunables() map[string]TunableSpec   { return map[string]TunableSpec{} }
func (m *mockOperator) GetState() map[string]any {
	// 返回副本，避免 before/after 指向同一 map
	result := make(map[string]any, len(m.state))
	for k, v := range m.state {
		result[k] = v
	}
	return result
}
func (m *mockOperator) SetParameter(target string, value any) {
	m.state[target] = value
	m.callbacks = append(m.callbacks, struct {
		target string
		value  any
	}{target, value})
}
func (m *mockOperator) ApplyUpdate(target string, update schema.UpdateValue) schema.ApplyResult {
	return DefaultApplyUpdate(m, target, update)
}
func (m *mockOperator) LoadState(state map[string]any) {
	for k, v := range state {
		m.state[k] = v
	}
}

func TestTunableKindConstants(t *testing.T) {
	kinds := []struct {
		name     string
		kind     TunableKind
		expected string
	}{
		{"Prompt", TunableKindPrompt, "prompt"},
		{"Continuous", TunableKindContinuous, "continuous"},
		{"Discrete", TunableKindDiscrete, "discrete"},
		{"ToolSelector", TunableKindToolSelector, "tool_selector"},
		{"MemorySelector", TunableKindMemorySelector, "memory_selector"},
		{"SkillExperience", TunableKindSkillExperience, "skill_experience"},
		{"Text", TunableKindText, "text"},
	}
	for _, tt := range kinds {
		t.Run(tt.name, func(t *testing.T) {
			if string(tt.kind) != tt.expected {
				t.Errorf("got %q, expected %q", tt.kind, tt.expected)
			}
		})
	}
}

func TestTunableSpec(t *testing.T) {
	spec := TunableSpec{
		Name:       "system_prompt",
		Kind:       TunableKindPrompt,
		Path:       "system_prompt",
		Constraint: nil,
	}
	if spec.Name != "system_prompt" {
		t.Errorf("Name = %q, expected %q", spec.Name, "system_prompt")
	}
	if spec.Kind != TunableKindPrompt {
		t.Errorf("Kind = %v, expected %v", spec.Kind, TunableKindPrompt)
	}
}

func TestDefaultApplyUpdate(t *testing.T) {
	t.Run("replace+state成功", func(t *testing.T) {
		op := &mockOperator{operatorID: "test_op", state: map[string]any{"p": "old"}}
		update := schema.NewUpdateValue("new")
		result := DefaultApplyUpdate(op, "p", update)
		if !result.Applied {
			t.Error("should be applied")
		}
		if result.Value != "new" {
			t.Errorf("value = %v, expected %q", result.Value, "new")
		}
		if result.Mode != schema.UpdateModeReplace {
			t.Errorf("mode = %v, expected %v", result.Mode, schema.UpdateModeReplace)
		}
	})

	t.Run("replace+state值未变化", func(t *testing.T) {
		op := &mockOperator{operatorID: "test_op", state: map[string]any{"p": "same"}}
		update := schema.NewUpdateValue("same")
		result := DefaultApplyUpdate(op, "p", update)
		if result.Applied {
			t.Error("should not be applied when value unchanged")
		}
	})

	t.Run("不支持的mode/effect", func(t *testing.T) {
		op := &mockOperator{operatorID: "test_op", state: map[string]any{}}
		update := schema.NewUpdateValue("data", schema.WithUpdateMode(schema.UpdateModeAppend))
		result := DefaultApplyUpdate(op, "p", update)
		if result.Applied {
			t.Error("should not be applied for unsupported mode/effect")
		}
		if len(result.Errors) == 0 {
			t.Error("should have errors")
		}
	})

	t.Run("pending_change+replace不支持", func(t *testing.T) {
		op := &mockOperator{operatorID: "test_op", state: map[string]any{}}
		update := schema.NewUpdateValue("data", schema.WithUpdateEffect(schema.UpdateEffectPendingChange))
		result := DefaultApplyUpdate(op, "p", update)
		if result.Applied {
			t.Error("should not be applied for pending_change effect")
		}
	})
}

func TestParameterUpdatedCallback(t *testing.T) {
	called := false
	var capturedTarget string
	var capturedValue any
	cb := ParameterUpdatedCallback(func(target string, value any) {
		called = true
		capturedTarget = target
		capturedValue = value
	})
	cb("test_target", "test_value")
	if !called {
		t.Error("callback should have been called")
	}
	if capturedTarget != "test_target" {
		t.Errorf("target = %q, expected %q", capturedTarget, "test_target")
	}
	if capturedValue != "test_value" {
		t.Errorf("value = %v, expected %q", capturedValue, "test_value")
	}
}

func TestStateEqual(t *testing.T) {
	t.Run("长度不同", func(t *testing.T) {
		if stateEqual(map[string]any{"a": 1}, map[string]any{"a": 1, "b": 2}) {
			t.Error("should not be equal")
		}
	})
	t.Run("key缺失", func(t *testing.T) {
		if stateEqual(map[string]any{"a": 1}, map[string]any{"b": 1}) {
			t.Error("should not be equal")
		}
	})
	t.Run("嵌套map相等", func(t *testing.T) {
		a := map[string]any{"nested": map[string]any{"x": 1}}
		b := map[string]any{"nested": map[string]any{"x": 1}}
		if !stateEqual(a, b) {
			t.Error("should be equal")
		}
	})
	t.Run("嵌套map不等", func(t *testing.T) {
		a := map[string]any{"nested": map[string]any{"x": 1}}
		b := map[string]any{"nested": map[string]any{"x": 2}}
		if stateEqual(a, b) {
			t.Error("should not be equal")
		}
	})
}

func TestValueEqual(t *testing.T) {
	t.Run("两个nil", func(t *testing.T) {
		if !valueEqual(nil, nil) {
			t.Error("nil == nil should be true")
		}
	})
	t.Run("一个nil", func(t *testing.T) {
		if valueEqual(nil, "x") {
			t.Error("nil != x should be true")
		}
	})
	t.Run("map[string]string相等", func(t *testing.T) {
		a := map[string]string{"k": "v"}
		b := map[string]string{"k": "v"}
		if !valueEqual(a, b) {
			t.Error("should be equal")
		}
	})
	t.Run("map[string]string不等", func(t *testing.T) {
		a := map[string]string{"k": "v1"}
		b := map[string]string{"k": "v2"}
		if valueEqual(a, b) {
			t.Error("should not be equal")
		}
	})
	t.Run("map[string]string长度不同", func(t *testing.T) {
		a := map[string]string{"k": "v"}
		b := map[string]string{"k": "v", "k2": "v2"}
		if valueEqual(a, b) {
			t.Error("should not be equal")
		}
	})
	t.Run("[]any相等", func(t *testing.T) {
		a := []any{1, "x"}
		b := []any{1, "x"}
		if !valueEqual(a, b) {
			t.Error("should be equal")
		}
	})
	t.Run("[]any长度不同", func(t *testing.T) {
		a := []any{1}
		b := []any{1, 2}
		if valueEqual(a, b) {
			t.Error("should not be equal")
		}
	})
	t.Run("[]any元素不同", func(t *testing.T) {
		a := []any{1}
		b := []any{2}
		if valueEqual(a, b) {
			t.Error("should not be equal")
		}
	})
	t.Run("普通值相等", func(t *testing.T) {
		if !valueEqual(42, 42) {
			t.Error("42 == 42 should be true")
		}
	})
	t.Run("普通值不等", func(t *testing.T) {
		if valueEqual(42, "42") {
			t.Error("42 != '42'")
		}
	})
}
