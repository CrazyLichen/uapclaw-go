package extensions

import (
	"testing"
)

// TestExtensionMetadata_字段完整性 测试 ExtensionMetadata 所有字段可正确赋值
func TestExtensionMetadata_字段完整性(t *testing.T) {
	m := ExtensionMetadata{
		ID:                    "ext-001",
		Name:                  "测试扩展",
		Version:               "1.0.0",
		Description:           "这是一个测试扩展",
		Author:                "test-author",
		MinJiuwenSwarmVersion: "0.1.0",
		Dependencies:          map[string]string{"core": ">=0.1.0"},
		ConfigSchema:          map[string]any{"type": "object"},
	}

	if m.ID != "ext-001" {
		t.Errorf("ID = %q, want %q", m.ID, "ext-001")
	}
	if m.Name != "测试扩展" {
		t.Errorf("Name = %q, want %q", m.Name, "测试扩展")
	}
	if m.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", m.Version, "1.0.0")
	}
	if m.Dependencies["core"] != ">=0.1.0" {
		t.Errorf("Dependencies[core] = %q, want %q", m.Dependencies["core"], ">=0.1.0")
	}
	if m.ConfigSchema["type"] != "object" {
		t.Errorf("ConfigSchema[type] = %v, want %q", m.ConfigSchema["type"], "object")
	}
}

// TestExtensionMetadata_可选字段 测试 ConfigSchema 为 nil 时默认值
func TestExtensionMetadata_可选字段(t *testing.T) {
	m := ExtensionMetadata{
		ID:      "ext-002",
		Name:    "无配置扩展",
		Version: "0.5.0",
	}
	if m.ConfigSchema != nil {
		t.Errorf("ConfigSchema should be nil when not set, got %v", m.ConfigSchema)
	}
}

// TestExtensionConfig_字段完整性 测试 ExtensionConfig 字段赋值
func TestExtensionConfig_字段完整性(t *testing.T) {
	cfg := ExtensionConfig{
		Config: map[string]any{"key": "value"},
	}
	if cfg.Config["key"] != "value" {
		t.Errorf("Config[key] = %v, want %q", cfg.Config["key"], "value")
	}
}

// TestExtensionConfig_默认值 测试 ExtensionConfig 空 Config
func TestExtensionConfig_默认值(t *testing.T) {
	cfg := ExtensionConfig{}
	if cfg.Config != nil {
		t.Errorf("Config should be nil when not set, got %v", cfg.Config)
	}
}
