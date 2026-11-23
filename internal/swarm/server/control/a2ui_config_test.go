package control

import (
	"os"
	"testing"
)

// TestGetA2UIConfig 默认配置（Python: get_a2ui_config 默认值）
func TestGetA2UIConfig(t *testing.T) {
	cfg, err := GetA2UIConfig(nil)
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if cfg.Enabled {
		t.Errorf("Enabled = true, want false")
	}
	if cfg.ProtocolVersion != "0.8" {
		t.Errorf("ProtocolVersion = %q, want 0.8", cfg.ProtocolVersion)
	}
	if !cfg.StreamValidationEnabled {
		t.Errorf("StreamValidationEnabled = false, want true")
	}
	if cfg.NonWebFallbackEnabled {
		t.Errorf("NonWebFallbackEnabled = true, want false")
	}
	if cfg.DevSmokeToolsEnabled {
		t.Errorf("DevSmokeToolsEnabled = true, want false")
	}
}

// TestGetA2UIConfig_段值解析 yml 段布尔三态
func TestGetA2UIConfig_段值解析(t *testing.T) {
	cfg, err := GetA2UIConfig(map[string]any{
		"a2ui": map[string]any{
			"enabled":                   "yes",
			"stream_validation_enabled": false,
			"non_web_fallback_enabled":  1,
		},
	})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !cfg.Enabled {
		t.Errorf("Enabled = false, want true")
	}
	if cfg.StreamValidationEnabled {
		t.Errorf("StreamValidationEnabled = true, want false")
	}
	if !cfg.NonWebFallbackEnabled {
		t.Errorf("NonWebFallbackEnabled = false, want true")
	}
}

// TestGetA2UIConfig_环境变量覆盖 环境变量优先于段值
func TestGetA2UIConfig_环境变量覆盖(t *testing.T) {
	t.Setenv("JIUWENSWARM_A2UI_ENABLED", "true")
	t.Setenv("JIUWENSWARM_A2UI_PROTOCOL_VERSION", "0.8")
	cfg, err := GetA2UIConfig(map[string]any{"a2ui": map[string]any{"enabled": false}})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if !cfg.Enabled {
		t.Errorf("Enabled = false, want true（env 覆盖）")
	}
}

// TestGetA2UIConfig_不支持版本报错 非法协议版本回错误
func TestGetA2UIConfig_不支持版本报错(t *testing.T) {
	t.Setenv("JIUWENSWARM_A2UI_PROTOCOL_VERSION", "9.9")
	_, err := GetA2UIConfig(nil)
	if err == nil {
		t.Fatal("err = nil, want Unsupported A2UI protocol version")
	}
	want := "Unsupported A2UI protocol version: 9.9"
	if err.Error() != want {
		t.Errorf("err = %q, want %q", err.Error(), want)
	}
}

// TestGetA2UIConfig_段非dict回空 a2ui 段类型不符回空段
func TestGetA2UIConfig_段非dict回空(t *testing.T) {
	cfg, err := GetA2UIConfig(map[string]any{"a2ui": "bad"})
	if err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
	if cfg.Enabled {
		t.Errorf("Enabled = true, want false")
	}
}

// TestIsA2UIEnabled 便捷判断
func TestIsA2UIEnabled(t *testing.T) {
	if IsA2UIEnabled(map[string]any{}) {
		t.Errorf("IsA2UIEnabled = true, want false")
	}
	if !IsA2UIEnabled(map[string]any{"a2ui": map[string]any{"enabled": true}}) {
		t.Errorf("IsA2UIEnabled = false, want true")
	}
	// 异常路径（非法版本）回 false
	t.Setenv("JIUWENSWARM_A2UI_PROTOCOL_VERSION", "bad")
	os.Unsetenv("JIUWENSWARM_A2UI_ENABLED")
	if IsA2UIEnabled(map[string]any{"a2ui": map[string]any{"enabled": true}}) {
		t.Errorf("IsA2UIEnabled = true, want false（GetA2UIConfig 报错时）")
	}
}
