package context

import "testing"

// TestSessionMemoryConfigFromMap_默认值 空字典返回 Python 默认配置。
func TestSessionMemoryConfigFromMap_默认值(t *testing.T) {
	cfg, err := SessionMemoryConfigFromMap(map[string]any{})
	if err != nil {
		t.Fatalf("解析空配置失败: %v", err)
	}
	if cfg.TriggerTokens != 10000 || cfg.TriggerAddTokens != 5000 || cfg.ToolMin != 3 {
		t.Errorf("默认阈值不符: %+v", cfg)
	}
	if cfg.UpdateMode != sessionMemoryUpdateModeAgentEdit {
		t.Errorf("UpdateMode = %q, want %q（对齐 Python 默认）", cfg.UpdateMode, sessionMemoryUpdateModeAgentEdit)
	}
	if cfg.DirectReplaceMaxRetries != 2 {
		t.Errorf("DirectReplaceMaxRetries = %d, want 2", cfg.DirectReplaceMaxRetries)
	}
}

// TestSessionMemoryConfigFromMap_覆盖 覆盖全部标量键。
func TestSessionMemoryConfigFromMap_覆盖(t *testing.T) {
	cfg, err := SessionMemoryConfigFromMap(map[string]any{
		"trigger_tokens":             20000,
		"trigger_add_tokens":         float64(8000),
		"tool_min_":                  5,
		"update_mode":                "direct_replace",
		"direct_replace_max_retries": 4,
	})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cfg.TriggerTokens != 20000 || cfg.TriggerAddTokens != 8000 || cfg.ToolMin != 5 {
		t.Errorf("阈值覆盖未生效: %+v", cfg)
	}
	if cfg.UpdateMode != sessionMemoryUpdateModeDirectReplace {
		t.Errorf("UpdateMode = %q, want direct_replace", cfg.UpdateMode)
	}
	if cfg.DirectReplaceMaxRetries != 4 {
		t.Errorf("DirectReplaceMaxRetries = %d, want 4", cfg.DirectReplaceMaxRetries)
	}
}

// TestSessionMemoryConfigFromMap_无尾下划线也接受 兼容 tool_min 写法。
func TestSessionMemoryConfigFromMap_无尾下划线也接受(t *testing.T) {
	cfg, err := SessionMemoryConfigFromMap(map[string]any{"tool_min": 7})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if cfg.ToolMin != 7 {
		t.Errorf("ToolMin = %d, want 7", cfg.ToolMin)
	}
}

// TestSessionMemoryConfigFromMap_非法值报错 非法类型/取值返回错误（调用方降级为关闭）。
func TestSessionMemoryConfigFromMap_非法值报错(t *testing.T) {
	cases := []struct {
		name string
		raw  map[string]any
	}{
		{"update_mode 类型错误", map[string]any{"update_mode": 1}},
		{"update_mode 取值非法", map[string]any{"update_mode": "unknown"}},
		{"trigger_tokens 非整数", map[string]any{"trigger_tokens": "abc"}},
		{"trigger_tokens 小数", map[string]any{"trigger_tokens": 1.5}},
		{"trigger_tokens 非正", map[string]any{"trigger_tokens": 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SessionMemoryConfigFromMap(tc.raw); err == nil {
				t.Fatal("期望返回错误")
			}
		})
	}
}
