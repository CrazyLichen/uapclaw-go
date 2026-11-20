package schema

import "testing"

// TestT_AsyncToolKeys 验证 4 个 async_tool 键中英均存在。
func TestT_AsyncToolKeys(t *testing.T) {
	orig := GetLanguage()
	defer func() { _ = SetLanguage(orig) }()

	for _, lang := range []Language{LanguageCN, LanguageEN} {
		if err := SetLanguage(lang); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{
			"async_tool.launched",
			"async_tool.completed",
			"async_tool.failed",
			"async_tool.spilled_notice",
		} {
			if _, err := T(key); err != nil {
				t.Fatalf("lang=%s key=%s 缺失: %v", lang, key, err)
			}
		}
	}
}
