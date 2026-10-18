package memory

import "testing"

// TestGetMemoryEngine_合法值 对齐 Python get_memory_engine L30-39
func TestGetMemoryEngine_合法值(t *testing.T) {
	cases := []struct {
		engine string
		want   string
	}{
		{"builtin", "builtin"},
		{"external", "external"},
		{"both", "both"},
		{"none", "none"},
		{"  External  ", "external"}, // strip + lower
	}
	for _, c := range cases {
		cfg := map[string]any{"memory": map[string]any{"engine": c.engine}}
		if got := GetMemoryEngine(cfg); got != c.want {
			t.Errorf("engine=%q → %q, want %q", c.engine, got, c.want)
		}
	}
}

// TestGetMemoryEngine_回落builtin 空白/缺失/非法值回落 builtin（Python: or "builtin" + in _VALID_ENGINES 检查）
func TestGetMemoryEngine_回落builtin(t *testing.T) {
	for _, cfg := range []map[string]any{
		nil,                          // 无 memory 段
		{"memory": map[string]any{}}, // engine 缺失
		{"memory": map[string]any{"engine": ""}},
		{"memory": map[string]any{"engine": "  "}},
		{"memory": map[string]any{"engine": "invalid"}}, // 非法值 → builtin
	} {
		if got := GetMemoryEngine(cfg); got != "builtin" {
			t.Errorf("cfg=%v → %q, want builtin", cfg, got)
		}
	}
}

// TestGetMemoryEngine_nil走loadConfig 对齐 Python config=None → _load_config()
func TestGetMemoryEngine_nil走loadConfig(t *testing.T) {
	injectTestConfigLoader(t, "memory:\n  engine: external\n")
	ClearConfigCache()
	if got := GetMemoryEngine(nil); got != "external" {
		t.Errorf("nil cfg → %q, want external（应走包内 loadConfig）", got)
	}
	ClearConfigCache()
}

// TestIsBuiltinMemoryAllowed 对齐 L42-44：engine ∈ {builtin, both}
func TestIsBuiltinMemoryAllowed(t *testing.T) {
	cases := []struct {
		engine string
		want   bool
	}{{"builtin", true}, {"both", true}, {"external", false}, {"none", false}}
	for _, c := range cases {
		cfg := map[string]any{"memory": map[string]any{"engine": c.engine}}
		if got := IsBuiltinMemoryAllowed(cfg); got != c.want {
			t.Errorf("engine=%s → %v, want %v", c.engine, got, c.want)
		}
	}
}

// TestIsExternalMemoryAllowed 对齐 L47-49：engine ∈ {external, both}（Go 新增函数）
func TestIsExternalMemoryAllowed(t *testing.T) {
	cases := []struct {
		engine string
		want   bool
	}{{"external", true}, {"both", true}, {"builtin", false}, {"none", false}}
	for _, c := range cases {
		cfg := map[string]any{"memory": map[string]any{"engine": c.engine}}
		if got := IsExternalMemoryAllowed(cfg); got != c.want {
			t.Errorf("engine=%s → %v, want %v", c.engine, got, c.want)
		}
	}
}
