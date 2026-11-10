package permission_mode

import "testing"

// ──────────────────────────── 常量 ────────────────────────────

func TestModeConstants(t *testing.T) {
	if ManualPermissionMode != "manual" {
		t.Errorf("ManualPermissionMode = %q", ManualPermissionMode)
	}
	if AutoPermissionMode != "auto" {
		t.Errorf("AutoPermissionMode = %q", AutoPermissionMode)
	}
}

// ──────────────────────────── 用例：ResolvePermissionRuntimeMode ───────────────────────────────

func TestResolvePermissionRuntimeMode_各输入(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]any
		want string
	}{
		{"nil配置", nil, ManualPermissionMode},
		{"空配置", map[string]any{}, ManualPermissionMode},
		{"显式auto", map[string]any{"mode": "auto"}, AutoPermissionMode},
		{"显式manual", map[string]any{"mode": "manual"}, ManualPermissionMode},
		{"大写AUTO", map[string]any{"mode": "AUTO"}, AutoPermissionMode},
		{"带空白", map[string]any{"mode": "  Auto  "}, AutoPermissionMode},
		{"未知值回落", map[string]any{"mode": "turbo"}, ManualPermissionMode},
		{"非字符串回落", map[string]any{"mode": 42}, ManualPermissionMode},
		{"nil值回落", map[string]any{"mode": nil}, ManualPermissionMode},
		{"空串回落", map[string]any{"mode": ""}, ManualPermissionMode},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ResolvePermissionRuntimeMode(c.cfg); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

// ──────────────────────────── 用例：IsAutoPermissionMode ───────────────────────────────

func TestIsAutoPermissionMode(t *testing.T) {
	if !IsAutoPermissionMode(map[string]any{"mode": "auto"}) {
		t.Error("mode=auto should be true")
	}
	if IsAutoPermissionMode(map[string]any{"mode": "manual"}) {
		t.Error("mode=manual should be false")
	}
	if IsAutoPermissionMode(nil) {
		t.Error("nil should be false")
	}
}

// ──────────────────────────── 用例：IsPermissionBoundaryEnabled ───────────────────────────────

func TestIsPermissionBoundaryEnabled_严格判等true(t *testing.T) {
	if !IsPermissionBoundaryEnabled(map[string]any{"enabled": true}) {
		t.Error("enabled=true should be true")
	}
	// 非布尔真值不算（对齐 Python 的 `is True`）
	for _, v := range []any{false, 1, "true", nil, 1.0} {
		if IsPermissionBoundaryEnabled(map[string]any{"enabled": v}) {
			t.Errorf("enabled=%v (%T) should be false", v, v)
		}
	}
	if IsPermissionBoundaryEnabled(nil) {
		t.Error("nil config should be false")
	}
	if IsPermissionBoundaryEnabled(map[string]any{}) {
		t.Error("missing enabled should be false")
	}
}

// ──────────────────────────── 用例：IsAutoPermissionEnabled ───────────────────────────────

func TestIsAutoPermissionEnabled_需同时满足(t *testing.T) {
	if !IsAutoPermissionEnabled(map[string]any{"mode": "auto", "enabled": true}) {
		t.Error("auto + enabled should be true")
	}
	if IsAutoPermissionEnabled(map[string]any{"mode": "auto"}) {
		t.Error("auto without enabled should be false")
	}
	if IsAutoPermissionEnabled(map[string]any{"mode": "manual", "enabled": true}) {
		t.Error("manual + enabled should be false")
	}
	if IsAutoPermissionEnabled(nil) {
		t.Error("nil should be false")
	}
}

// ──────────────────────────── 非导出辅助 ────────────────────────────

func TestTrimLower(t *testing.T) {
	cases := []struct{ in, want string }{
		{"  Auto  ", "auto"},
		{"MANUAL", "manual"},
		{"\tAUTO\n", "auto"},
		{"", ""},
		{"   ", ""},
		{"already", "already"},
	}
	for _, c := range cases {
		if got := trimLower(c.in); got != c.want {
			t.Errorf("trimLower(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestIsSpace(t *testing.T) {
	for _, c := range []byte{' ', '\t', '\n', '\r', '\v', '\f'} {
		if !isSpace(c) {
			t.Errorf("%q should be space", c)
		}
	}
	for _, c := range []byte{'a', '0', '.', '-'} {
		if isSpace(c) {
			t.Errorf("%q should not be space", c)
		}
	}
}
