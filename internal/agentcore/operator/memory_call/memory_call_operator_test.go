package memory_call

import (
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/operator"
	"github.com/uapclaw/uapclaw-go/internal/evolving/schema"
)

func TestNewMemoryCallOperator(t *testing.T) {
	t.Run("默认值", func(t *testing.T) {
		op := NewMemoryCallOperator()
		if op.OperatorID() != "memory_call" {
			t.Errorf("operatorID = %q, expected %q", op.OperatorID(), "memory_call")
		}
		state := op.GetState()
		if state[TargetEnabled] != true {
			t.Error("enabled should be true by default")
		}
		if state[TargetMaxRetries] != 0 {
			t.Errorf("maxRetries = %v, expected 0", state[TargetMaxRetries])
		}
	})

	t.Run("自定义ID", func(t *testing.T) {
		op := NewMemoryCallOperator(WithMemoryOperatorID("custom_mem"))
		if op.OperatorID() != "custom_mem" {
			t.Errorf("operatorID = %q, expected %q", op.OperatorID(), "custom_mem")
		}
	})
}

func TestMemoryCallOperator_GetTunables(t *testing.T) {
	op := NewMemoryCallOperator()
	tunables := op.GetTunables()
	if len(tunables) != 2 {
		t.Fatalf("tunables count = %d, expected 2", len(tunables))
	}
	enabledSpec, ok := tunables[TargetEnabled]
	if !ok {
		t.Fatal("enabled tunable missing")
	}
	if enabledSpec.Kind != operator.TunableKindDiscrete {
		t.Errorf("enabled kind = %v, expected %v", enabledSpec.Kind, operator.TunableKindDiscrete)
	}
	retriesSpec, ok := tunables[TargetMaxRetries]
	if !ok {
		t.Fatal("max_retries tunable missing")
	}
	if retriesSpec.Kind != operator.TunableKindDiscrete {
		t.Errorf("max_retries kind = %v, expected %v", retriesSpec.Kind, operator.TunableKindDiscrete)
	}
}

func TestMemoryCallOperator_SetParameter(t *testing.T) {
	t.Run("更新enabled", func(t *testing.T) {
		op := NewMemoryCallOperator()
		op.SetParameter(TargetEnabled, false)
		state := op.GetState()
		if state[TargetEnabled] != false {
			t.Error("enabled should be false")
		}
	})

	t.Run("更新maxRetries正常值", func(t *testing.T) {
		op := NewMemoryCallOperator()
		op.SetParameter(TargetMaxRetries, 3)
		state := op.GetState()
		if state[TargetMaxRetries] != 3 {
			t.Errorf("maxRetries = %v, expected 3", state[TargetMaxRetries])
		}
	})

	t.Run("maxRetries超出上限钳位到5", func(t *testing.T) {
		op := NewMemoryCallOperator()
		op.SetParameter(TargetMaxRetries, 10)
		state := op.GetState()
		if state[TargetMaxRetries] != 5 {
			t.Errorf("maxRetries = %v, expected 5 (clamped)", state[TargetMaxRetries])
		}
	})

	t.Run("maxRetries负数钳位到0", func(t *testing.T) {
		op := NewMemoryCallOperator()
		op.SetParameter(TargetMaxRetries, -5)
		state := op.GetState()
		if state[TargetMaxRetries] != 0 {
			t.Errorf("maxRetries = %v, expected 0 (clamped)", state[TargetMaxRetries])
		}
	})

	t.Run("回调触发", func(t *testing.T) {
		var capturedTarget string
		var capturedValue any
		cb := operator.ParameterUpdatedCallback(func(target string, value any) {
			capturedTarget = target
			capturedValue = value
		})
		op := NewMemoryCallOperator(WithMemoryOnParameterUpdated(cb))
		op.SetParameter(TargetEnabled, false)
		if capturedTarget != TargetEnabled {
			t.Errorf("callback target = %q, expected %q", capturedTarget, TargetEnabled)
		}
		if capturedValue != false {
			t.Errorf("callback value = %v, expected false", capturedValue)
		}
	})

	t.Run("类型转换_bool从string", func(t *testing.T) {
		op := NewMemoryCallOperator()
		op.SetParameter(TargetEnabled, "true")
		state := op.GetState()
		if state[TargetEnabled] != true {
			t.Error("should convert 'true' string to bool true")
		}
	})

	t.Run("类型转换_int从float64", func(t *testing.T) {
		op := NewMemoryCallOperator()
		op.SetParameter(TargetMaxRetries, float64(3))
		state := op.GetState()
		if state[TargetMaxRetries] != 3 {
			t.Errorf("maxRetries = %v, expected 3", state[TargetMaxRetries])
		}
	})
}

func TestMemoryCallOperator_LoadState(t *testing.T) {
	t.Run("完整恢复", func(t *testing.T) {
		op := NewMemoryCallOperator()
		op.LoadState(map[string]any{
			TargetEnabled:    false,
			TargetMaxRetries: 4,
		})
		state := op.GetState()
		if state[TargetEnabled] != false {
			t.Error("enabled should be false")
		}
		if state[TargetMaxRetries] != 4 {
			t.Errorf("maxRetries = %v, expected 4", state[TargetMaxRetries])
		}
	})

	t.Run("回调触发", func(t *testing.T) {
		callbacks := []string{}
		cb := operator.ParameterUpdatedCallback(func(target string, _ any) {
			callbacks = append(callbacks, target)
		})
		op := NewMemoryCallOperator(WithMemoryOnParameterUpdated(cb))
		op.LoadState(map[string]any{
			TargetEnabled:    false,
			TargetMaxRetries: 2,
		})
		if len(callbacks) != 2 {
			t.Errorf("callbacks count = %d, expected 2", len(callbacks))
		}
	})

	t.Run("maxRetries超出范围钳位", func(t *testing.T) {
		op := NewMemoryCallOperator()
		op.LoadState(map[string]any{
			TargetMaxRetries: 100,
		})
		state := op.GetState()
		if state[TargetMaxRetries] != 5 {
			t.Errorf("maxRetries = %v, expected 5 (clamped)", state[TargetMaxRetries])
		}
	})
}

func TestMemoryCallOperator_ApplyUpdate(t *testing.T) {
	op := NewMemoryCallOperator()
	update := schema.NewUpdateValue(false)
	result := op.ApplyUpdate(TargetEnabled, update)
	if !result.Applied {
		t.Error("should be applied")
	}
	if result.OperatorID != "memory_call" {
		t.Errorf("operatorID = %q, expected %q", result.OperatorID, "memory_call")
	}
}

func TestToBool(t *testing.T) {
	tests := []struct {
		input    any
		expected bool
	}{
		{true, true},
		{false, false},
		{int(1), true},
		{int(0), false},
		{float64(1.5), true},
		{float64(0.0), false},
		{"true", true},
		{"1", true},
		{"false", false},
		{"0", false},
		{nil, false},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v→%v", tt.input, tt.expected), func(t *testing.T) {
			result := toBool(tt.input)
			if result != tt.expected {
				t.Errorf("toBool(%v) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}

func TestToInt(t *testing.T) {
	tests := []struct {
		input    any
		expected int
	}{
		{int(5), 5},
		{int64(3), 3},
		{float64(2.7), 2},
		{true, 1},
		{false, 0},
		{"hello", 0},
		{nil, 0},
	}
	for _, tt := range tests {
		t.Run(fmt.Sprintf("%v→%v", tt.input, tt.expected), func(t *testing.T) {
			result := toInt(tt.input)
			if result != tt.expected {
				t.Errorf("toInt(%v) = %v, expected %v", tt.input, result, tt.expected)
			}
		})
	}
}
