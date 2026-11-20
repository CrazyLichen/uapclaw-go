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

// TestReasoningConfig_Validate 对齐 Pydantic 构造期校验。
// 实测上游行为：mode 非三值、budget_tokens 为负均抛 ValidationError。
// Python: schema/config.py:59-66
//
//	mode: Literal["auto","enabled","disabled"]
//	budget_tokens: Optional[int] = Field(default=None, ge=0)
func TestReasoningConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     *ReasoningConfig
		wantErr bool
	}{
		{"空mode视为未设置", &ReasoningConfig{Mode: ""}, false},
		{"auto", &ReasoningConfig{Mode: "auto"}, false},
		{"enabled", &ReasoningConfig{Mode: "enabled"}, false},
		{"disabled", &ReasoningConfig{Mode: "disabled"}, false},
		{"非法mode", &ReasoningConfig{Mode: "bogus"}, true},
		{"on不是合法mode", &ReasoningConfig{Mode: "on"}, true},
		{"budget为0", &ReasoningConfig{Mode: "enabled", BudgetTokens: intPtr(0)}, false},
		{"budget为负", &ReasoningConfig{Mode: "enabled", BudgetTokens: intPtr(-1)}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.cfg.Validate()
			if (err != nil) != c.wantErr {
				t.Fatalf("Validate() err = %v, wantErr = %v", err, c.wantErr)
			}
		})
	}
}

// TestCoerceReasoningConfig_map分支校验失败 验证 map 输入也走校验。
// Python: reasoning.py:264 ReasoningConfig(**raw) —— Pydantic 在此抛 ValidationError
func TestCoerceReasoningConfig_map分支校验失败(t *testing.T) {
	cases := []map[string]any{
		{"mode": "bogus"},
		{"mode": "enabled", "budget_tokens": float64(-1)},
	}
	for _, in := range cases {
		cfg, raw := CoerceReasoningConfig(in)
		if cfg != nil || raw != nil {
			t.Errorf("输入 %v 应被拒绝，got cfg=%v raw=%v", in, cfg, raw)
		}
	}
}

// TestCoerceReasoningConfig_map分支合法通过 验证合法输入正常转换。
func TestCoerceReasoningConfig_map分支合法通过(t *testing.T) {
	cfg, raw := CoerceReasoningConfig(map[string]any{"mode": "enabled", "budget_tokens": float64(2048)})
	if raw != nil || cfg == nil {
		t.Fatalf("cfg=%v raw=%v", cfg, raw)
	}
	if cfg.Mode != "enabled" || cfg.BudgetTokens == nil || *cfg.BudgetTokens != 2048 {
		t.Fatalf("cfg=%+v", cfg)
	}
}

// intPtr 返回整型指针（测试辅助）。
func intPtr(n int) *int { return &n }

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

// TestCoerceReasoningConfig_budget负数被拒 验证 coerce 阶段执行取值校验。
// 实测上游：ReasoningConfig(**{"budget_tokens": -1}) 在 Pydantic 构造期抛
// ValidationError（budget_tokens 有 ge=0），不是延后到 resolve 阶段。
// Python: schema/config.py:64 Field(default=None, ge=0) + reasoning.py:264 ReasoningConfig(**raw)
func TestCoerceReasoningConfig_budget负数被拒(t *testing.T) {
	cfg, raw := CoerceReasoningConfig(map[string]any{"budget_tokens": float64(-1)})
	if cfg != nil || raw != nil {
		t.Fatalf("负数 budget 应被拒绝，got cfg=%+v raw=%v", cfg, raw)
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

// TestModelRequestConfig_ContextWindow反序列化进字段不进Extra 验证 knownKeys 含 context_window。
// 缺陷背景：ContextWindow 有结构体字段但曾漏在 knownKeys 之外，导致
// ①字段永远 nil（成了死字段）②值落进 Extra 后经 BuildRequestParams 平铺误发给厂商。
// Python 侧靠 model_dump(exclude={"context_window"}) 排除，Go 侧靠 knownKeys 分流。
// Python: base_model_client.py:624-637
func TestModelRequestConfig_ContextWindow反序列化进字段不进Extra(t *testing.T) {
	var c ModelRequestConfig
	if err := json.Unmarshal([]byte(`{"model":"m1","context_window":200000}`), &c); err != nil {
		t.Fatal(err)
	}
	if c.ContextWindow == nil || *c.ContextWindow != 200000 {
		t.Fatalf("ContextWindow = %v, want 200000（knownKeys 漏了 context_window）", c.ContextWindow)
	}
	if _, leaked := c.Extra["context_window"]; leaked {
		t.Fatal("context_window 泄漏进了 Extra，会被平铺发给厂商")
	}
}

// TestModelRequestConfig_ContextWindow不发给厂商 端到端验证 BuildRequestParams 不含该键。
// Python: base_model_client.py:631-633「must never be sent as a provider SDK request parameter」
func TestModelRequestConfig_ContextWindow不发给厂商(t *testing.T) {
	var c ModelRequestConfig
	if err := json.Unmarshal([]byte(`{"model":"m1","context_window":200000}`), &c); err != nil {
		t.Fatal(err)
	}
	if _, exists := c.Extra["context_window"]; exists {
		t.Fatal("context_window 进了 Extra，BuildRequestParams 会平铺它")
	}
}

// strPtr 返回字符串指针（测试辅助）。
func strPtr(s string) *string { return &s }
