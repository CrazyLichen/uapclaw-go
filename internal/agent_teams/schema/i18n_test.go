package schema

import "testing"

// TestGetLanguage 测试获取当前全局语言
func TestGetLanguage(t *testing.T) {
	// 保存当前语言
	origLang := GetLanguage()

	// 设置为英语
	if err := SetLanguage("en"); err != nil {
		t.Fatalf("SetLanguage(en) 返回 error: %v", err)
	}
	if lang := GetLanguage(); lang != "en" {
		t.Errorf("GetLanguage() = %q, want %q", lang, "en")
	}

	// 恢复
	_ = SetLanguage(origLang)
}

// TestFormatMap 测试模板占位符替换
func TestFormatMap(t *testing.T) {
	result := formatMap("Hello {name}, you have {count} messages", map[string]any{
		"name":  "Alice",
		"count": 3,
	})
	expected := "Hello Alice, you have 3 messages"
	if result != expected {
		t.Errorf("formatMap() = %q, want %q", result, expected)
	}
}

// TestFormatMap_空模板 测试空模板
func TestFormatMap_空模板(t *testing.T) {
	result := formatMap("", map[string]any{"key": "value"})
	if result != "" {
		t.Errorf("formatMap() = %q, want %q", result, "")
	}
}

// TestFormatMap_无占位符 测试无占位符模板
func TestFormatMap_无占位符(t *testing.T) {
	result := formatMap("no placeholders", map[string]any{"key": "value"})
	if result != "no placeholders" {
		t.Errorf("formatMap() = %q, want %q", result, "no placeholders")
	}
}
