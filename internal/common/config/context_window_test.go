package config

import "testing"

// TestParsePositiveTokens 覆盖配置里可能出现的各种写法。
func TestParsePositiveTokens(t *testing.T) {
	cases := []struct {
		name   string
		value  any
		want   int
		wantOK bool
	}{
		{"整数", 262144, 262144, true},
		{"int64", int64(4096), 4096, true},
		{"浮点整数", float64(2048), 2048, true},
		{"字符串纯数字", "131072", 131072, true},
		{"带 k 后缀", "256k", 262144, true},
		{"带空格单位", "256 K", 262144, true},
		{"带 kib", "256KiB", 262144, true},
		{"带 m 后缀", "1m", 1048576, true},
		{"带千分位", "1,048,576", 1048576, true},
		{"零", 0, 0, false},
		{"负数", -1, 0, false},
		{"布尔", true, 0, false},
		{"nil", nil, 0, false},
		{"小数", 1.5, 0, false},
		{"非法字符串", "abc", 0, false},
		{"超界", 1 << 54, 0, false},
	}
	for _, c := range cases {
		got, ok := ParsePositiveTokens(c.value)
		if ok != c.wantOK || got != c.want {
			t.Fatalf("%s: ParsePositiveTokens(%v) = (%d,%v), want (%d,%v)",
				c.name, c.value, got, ok, c.want, c.wantOK)
		}
	}
}

func TestResolveContextWindowTokens_全局优先(t *testing.T) {
	cfg := map[string]any{"context_window_tokens": "128k"}
	got := ResolveContextWindowTokens("gpt-x", cfg, map[string]any{"context_window": 4096}, nil)
	if got != 128*1024 {
		t.Fatalf("want %d got %d", 128*1024, got)
	}
}

func TestResolveContextWindowTokens_模型窗口次优(t *testing.T) {
	got := ResolveContextWindowTokens("gpt-x", map[string]any{}, map[string]any{"context_window": 4096}, 8192)
	if got != 4096 {
		t.Fatalf("want 4096 got %d", got)
	}
}

func TestResolveContextWindowTokens_覆盖回落(t *testing.T) {
	got := ResolveContextWindowTokens("gpt-x", map[string]any{}, map[string]any{}, 8192)
	if got != 8192 {
		t.Fatalf("want 8192 got %d", got)
	}
}

func TestResolveContextWindowTokens_显式模型映射(t *testing.T) {
	cfg := map[string]any{"model_context_window_tokens": map[string]any{"gpt-x": "1m", "bad": "x"}}
	got := ResolveContextWindowTokens("gpt-x", cfg, nil, nil)
	if got != 1024*1024 {
		t.Fatalf("want %d got %d", 1024*1024, got)
	}
}

func TestResolveContextWindowTokens_嵌套下钻(t *testing.T) {
	cfg := map[string]any{"context_engine_config": map[string]any{"context_window_tokens": 1024}}
	got := ResolveContextWindowTokens("", cfg, nil, nil)
	if got != 1024 {
		t.Fatalf("want 1024 got %d", got)
	}
}

func TestResolveContextWindowTokens_默认值(t *testing.T) {
	got := ResolveContextWindowTokens("", nil, nil, nil)
	if got != DefaultContextWindowTokens {
		t.Fatalf("want %d got %d", DefaultContextWindowTokens, got)
	}
}

func TestNormalizeModelWindowMapping_剔除非法值(t *testing.T) {
	m := NormalizeModelWindowMapping(map[string]any{"a": "2k", "b": 0, "c": "abc", "d": true})
	if len(m) != 1 || m["a"] != 2048 {
		t.Fatalf("want {a:2048} got %v", m)
	}
	if NormalizeModelWindowMapping(map[string]any{}) != nil {
		t.Fatal("empty mapping should be nil")
	}
}
