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

// TestGetExternalMemoryConfig_默认值填充 对齐 L52-70：缺失字段填默认
func TestGetExternalMemoryConfig_默认值填充(t *testing.T) {
	got := GetExternalMemoryConfig(map[string]any{"memory": map[string]any{}})
	if got.Provider != "" {
		t.Errorf("Provider = %q, want 空串", got.Provider)
	}
	if got.UserID != "__default__" {
		t.Errorf("UserID = %q, want __default__", got.UserID)
	}
	if got.ScopeID != "__default__" {
		t.Errorf("ScopeID = %q, want __default__", got.ScopeID)
	}
	if len(got.AllowedPlugins) != 0 {
		t.Errorf("AllowedPlugins = %v, want 空列表", got.AllowedPlugins)
	}
	if got.Mem0 == nil || got.OpenViking == nil || got.OpenJiuwen == nil {
		t.Error("三个 provider 子段缺失时应为空 map 而非 nil")
	}
}

// TestGetExternalMemoryConfig_读取配置段 对齐 L62-70
func TestGetExternalMemoryConfig_读取配置段(t *testing.T) {
	cfg := map[string]any{"memory": map[string]any{"external": map[string]any{
		"provider":        "  mem0  ", // strip 测试
		"user_id":         "u1",
		"scope_id":        "s1",
		"allowed_plugins": []any{"p1", "p2"},
		"mem0":            map[string]any{"api_key": "k"},
	}}}
	got := GetExternalMemoryConfig(cfg)
	if got.Provider != "mem0" {
		t.Errorf("Provider = %q, want mem0（应 strip）", got.Provider)
	}
	if got.UserID != "u1" || got.ScopeID != "s1" {
		t.Errorf("UserID/ScopeID = %q/%q, want u1/s1", got.UserID, got.ScopeID)
	}
	if len(got.AllowedPlugins) != 2 || got.AllowedPlugins[0] != "p1" {
		t.Errorf("AllowedPlugins = %v, want [p1 p2]", got.AllowedPlugins)
	}
	if got.Mem0["api_key"] != "k" {
		t.Errorf("Mem0.api_key = %v, want k", got.Mem0["api_key"])
	}
}

// TestGetExternalMemoryConfig_nil走loadConfig 对齐 Python config=None → _load_config()
func TestGetExternalMemoryConfig_nil走loadConfig(t *testing.T) {
	injectTestConfigLoader(t, "memory:\n  external:\n    provider: mem0\n")
	ClearConfigCache()
	got := GetExternalMemoryConfig(nil)
	if got.Provider != "mem0" {
		t.Errorf("nil cfg Provider = %q, want mem0", got.Provider)
	}
	ClearConfigCache()
}

// TestIsExternalMemoryEnabled_组合矩阵 对齐 L73-77：engine 放行 && provider 非空
// 回归偏差 #5：此前 Go 只查 engine，provider 清空后 rail 不注销
func TestIsExternalMemoryEnabled_组合矩阵(t *testing.T) {
	cases := []struct {
		name     string
		engine   string
		provider string
		want     bool
	}{
		{"external+provider", "external", "mem0", true},
		{"both+provider", "both", "mem0", true},
		{"both无provider", "both", "", false}, // ★ 修正点：provider 空则禁用
		{"builtin+provider", "builtin", "mem0", false},
		{"none", "none", "mem0", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ext := map[string]any{}
			if c.provider != "" {
				ext["provider"] = c.provider
			}
			cfg := map[string]any{"memory": map[string]any{
				"engine": c.engine, "external": ext,
			}}
			if got := IsExternalMemoryEnabled(cfg); got != c.want {
				t.Errorf("engine=%s provider=%q → %v, want %v", c.engine, c.provider, got, c.want)
			}
		})
	}
}
