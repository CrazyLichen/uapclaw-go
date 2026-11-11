package schema

import (
	"encoding/json"
	"testing"
)

// TestNewReasoningConfig_默认mode为auto 验证零值陷阱：
// Python ReasoningConfig 的 mode 默认 "auto"，Go 零值 "" 必须被构造器修正。
// Python: schema/config.py:62 mode: Literal["auto","enabled","disabled"] = "auto"
func TestNewReasoningConfig_默认mode为auto(t *testing.T) {
	c := NewReasoningConfig()
	if c.Mode != "auto" {
		t.Fatalf("mode = %q, want auto", c.Mode)
	}
}

// TestCoerceReasoningConfig_五分支 逐条覆盖 Python _coerce_reasoning_config。
// Python: reasoning.py:255-265
func TestCoerceReasoningConfig_五分支(t *testing.T) {
	t.Run("nil→双空", func(t *testing.T) {
		cfg, raw := CoerceReasoningConfig(nil)
		if cfg != nil || raw != nil {
			t.Fatalf("got cfg=%v raw=%v, want nil/nil", cfg, raw)
		}
	})
	t.Run("结构体→新式", func(t *testing.T) {
		in := &ReasoningConfig{Mode: "enabled", Effort: strPtr("high")}
		cfg, raw := CoerceReasoningConfig(in)
		if cfg != in || raw != nil {
			t.Fatalf("got cfg=%v raw=%v", cfg, raw)
		}
	})
	t.Run("非map→双空", func(t *testing.T) {
		cfg, raw := CoerceReasoningConfig("not-a-map")
		if cfg != nil || raw != nil {
			t.Fatalf("got cfg=%v raw=%v", cfg, raw)
		}
	})
	t.Run("键集在范围内→新式", func(t *testing.T) {
		cfg, raw := CoerceReasoningConfig(map[string]any{"mode": "enabled", "effort": "high"})
		if raw != nil {
			t.Fatalf("raw = %v, want nil", raw)
		}
		if cfg == nil || cfg.Mode != "enabled" || cfg.Effort == nil || *cfg.Effort != "high" {
			t.Fatalf("cfg = %+v", cfg)
		}
	})
	t.Run("键集超出范围→遗留raw", func(t *testing.T) {
		in := map[string]any{"effort": "high", "summary": "auto"}
		cfg, raw := CoerceReasoningConfig(in)
		if cfg != nil {
			t.Fatalf("cfg = %+v, want nil", cfg)
		}
		if raw == nil || raw["summary"] != "auto" {
			t.Fatalf("raw = %v", raw)
		}
	})
}

// TestCoerceReasoningConfig_budget负数不校验 验证 coerce 阶段不做取值校验
// （Python ReasoningConfig 的 ge=0 由 Pydantic 在构造时校验，coerce 只做形态判定）。
func TestCoerceReasoningConfig_budget负数不校验(t *testing.T) {
	cfg, _ := CoerceReasoningConfig(map[string]any{"budget_tokens": float64(-1)})
	if cfg == nil || cfg.BudgetTokens == nil || *cfg.BudgetTokens != -1 {
		t.Fatalf("cfg = %+v", cfg)
	}
}

// TestCoerceReasoningConfig_空map视为新式 键集为空亦为 subset，构造默认 mode=auto。
// Python: reasoning.py:263 set(raw).issubset(_NEW_REASONING_KEYS) —— 空集恒真
func TestCoerceReasoningConfig_空map视为新式(t *testing.T) {
	cfg, raw := CoerceReasoningConfig(map[string]any{})
	if raw != nil {
		t.Fatalf("raw = %v, want nil", raw)
	}
	if cfg == nil || cfg.Mode != "auto" {
		t.Fatalf("cfg = %+v, want mode=auto", cfg)
	}
}

// TestModelRequestConfig_Reasoning反序列化进字段不进Extra 验证 knownKeys 含 reasoning。
// 若漏加，reasoning 会落进 Extra，被 BuildRequestParams 平铺发给厂商。
func TestModelRequestConfig_Reasoning反序列化进字段不进Extra(t *testing.T) {
	var c ModelRequestConfig
	body := `{"model":"m1","reasoning":{"mode":"enabled","effort":"high","summary":"auto"}}`
	if err := json.Unmarshal([]byte(body), &c); err != nil {
		t.Fatal(err)
	}
	if c.Reasoning == nil {
		t.Fatal("Reasoning 为 nil，说明已知键表漏了 reasoning")
	}
	if _, leaked := c.Extra["reasoning"]; leaked {
		t.Fatal("reasoning 泄漏进了 Extra")
	}
	raw, ok := c.Reasoning.(map[string]any)
	if !ok || raw["summary"] != "auto" {
		t.Fatalf("Reasoning = %#v", c.Reasoning)
	}
}

// TestReasoningIntent_区分未传与显式nil 验证三态语义。
// Python: reasoning.py:33 UNSET_REASONING 哨兵
func TestReasoningIntent_区分未传与显式nil(t *testing.T) {
	unset := ReasoningIntent{Set: false}
	explicitNil := ReasoningIntent{Set: true, Value: nil}
	if unset.Set == explicitNil.Set {
		t.Fatal("两种状态必须可区分")
	}
	if IsReasoningConfigIntent(explicitNil.Value) {
		t.Fatal("显式 nil 不是 config 意图")
	}
}

// strPtr 返回字符串指针（测试辅助）。
func strPtr(s string) *string { return &s }
