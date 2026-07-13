package agent_teams

import (
	"testing"
)

// TestT_中文默认 验证默认语言为中文时 T() 返回中文字符串。
func TestT_中文默认(t *testing.T) {
	// 确保默认语言
	if err := SetLanguage(LanguageCN); err != nil {
		t.Fatalf("SetLanguage 失败: %v", err)
	}

	got := T("blueput.default_persona")
	want := "天才项目管理专家"
	if got != want {
		t.Errorf("T(\"blueput.default_persona\") = %q, want %q", got, want)
	}
}

// TestT_英文切换 验证切换到英文后 T() 返回英文字符串。
func TestT_英文切换(t *testing.T) {
	if err := SetLanguage(LanguageEN); err != nil {
		t.Fatalf("SetLanguage 失败: %v", err)
	}
	defer SetLanguage(LanguageCN) // 恢复

	got := T("blueput.default_persona")
	want := "Genius project management expert"
	if got != want {
		t.Errorf("T(\"blueput.default_persona\") = %q, want %q", got, want)
	}
}

// TestT_缺失Key 验证缺失 key 时 panic。
func TestT_缺失Key(t *testing.T) {
	defer func() {
		r := recover()
		if r == nil {
			t.Error("期望 panic，但未发生")
		}
	}()
	SetLanguage(LanguageCN)
	T("nonexistent.key")
}

// TestT_插值 验证 T() 支持插值参数。
func TestT_插值(t *testing.T) {
	SetLanguage(LanguageCN)
	got := T("dispatcher.member_online", map[string]any{"target_id": "dev-1"})
	want := "[成员事件] 成员 dev-1 已上线"
	if got != want {
		t.Errorf("T with kwargs = %q, want %q", got, want)
	}
}

// TestSetLanguage_不支持 验证不支持的 lang 报错。
func TestSetLanguage_不支持(t *testing.T) {
	err := SetLanguage("fr")
	if err == nil {
		t.Error("期望报错，但 SetLanguage 成功")
	}
}

// TestGetLanguage 验证默认值和切换后值。
func TestGetLanguage(t *testing.T) {
	// 恢复默认
	SetLanguage(LanguageCN)

	if got := GetLanguage(); got != LanguageCN {
		t.Errorf("GetLanguage() = %q, want %q", got, LanguageCN)
	}

	SetLanguage(LanguageEN)
	if got := GetLanguage(); got != LanguageEN {
		t.Errorf("GetLanguage() after set = %q, want %q", got, LanguageEN)
	}

	// 恢复
	SetLanguage(LanguageCN)
}
