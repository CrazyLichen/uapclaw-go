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

// TestI18nKeys_25_0b新增键齐全 验证 10 个键在中英两表都有定义。
func TestI18nKeys_25_0b新增键齐全(t *testing.T) {
	keys := []string{
		"time.just_now", "time.seconds_ago", "time.minutes_ago",
		"time.hours_ago", "time.days_ago", "time.unknown",
		"dispatcher.reply_hint", "dispatcher.reply_hint_user",
		"hitt.silence_note", "team_context.roster_announcement_note",
	}
	for _, lang := range []Language{LanguageCN, LanguageEN} {
		table, ok := STRINGS[lang]
		if !ok {
			t.Fatalf("语言 %q 的字符串表不存在", string(lang))
		}
		for _, k := range keys {
			if _, ok := table[k]; !ok {
				t.Errorf("语言 %q 缺键 %q", string(lang), k)
			}
		}
	}
}

// TestI18n_time占位符 验证时间文案的 {value} 占位符替换。
func TestI18n_time占位符(t *testing.T) {
	if err := SetLanguage("cn"); err != nil {
		t.Fatal(err)
	}
	got, err := T("time.seconds_ago", map[string]any{"value": 30})
	if err != nil {
		t.Fatal(err)
	}
	if got != "30 秒前" {
		t.Errorf("得到 %q，期望 %q", got, "30 秒前")
	}
	got, err = T("time.just_now")
	if err != nil {
		t.Fatal(err)
	}
	if got != "刚刚" {
		t.Errorf("得到 %q，期望 %q", got, "刚刚")
	}
}

// TestReplyHintFor_user走专用文案 验证 user 伪成员用强硬措辞，其余用通用提示。
// Python: reply_hint_for (i18n.py L1122-1138)
func TestReplyHintFor_user走专用文案(t *testing.T) {
	if err := SetLanguage("cn"); err != nil {
		t.Fatal(err)
	}
	wantUser, err := T("dispatcher.reply_hint_user")
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReplyHintFor("user")
	if err != nil {
		t.Fatal(err)
	}
	if got != wantUser {
		t.Errorf("user 的回复提示应为 reply_hint_user：得到 %q", got)
	}

	wantOther, err := T("dispatcher.reply_hint", map[string]any{"sender": "dev-1"})
	if err != nil {
		t.Fatal(err)
	}
	got, err = ReplyHintFor("dev-1")
	if err != nil {
		t.Fatal(err)
	}
	if got != wantOther {
		t.Errorf("普通成员的回复提示应为 reply_hint：得到 %q", got)
	}
}

// TestReplyHintFor_英文 验证英文语言下走英文文案。
func TestReplyHintFor_英文(t *testing.T) {
	if err := SetLanguage("en"); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = SetLanguage("cn") }()
	got, err := ReplyHintFor("dev-1")
	if err != nil {
		t.Fatal(err)
	}
	want := "If the sender is asking or waiting for a reply, be sure to reply to dev-1 via send_message."
	if got != want {
		t.Errorf("得到 %q\n期望 %q", got, want)
	}
}
