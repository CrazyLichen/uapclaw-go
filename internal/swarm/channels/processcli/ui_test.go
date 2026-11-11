package processcli

import "testing"

// TestDisplayWidth_CJK 东亚宽字符记 2。
// Python: ui.py:28-35 _display_width
func TestDisplayWidth_CJK(t *testing.T) {
	cases := map[string]int{
		"":     0,
		"abc":  3,
		"中文字符": 8,
		"ｆｕｌｌ": 8, // 全角拉丁
		"ｱｲｳ":  3, // 半角片假名（Narrow，记 1）
		"アイウ":  6, // 全角片假名（Wide，记 2）
		"a中b":  4,
		"한글":   4, // 谚文
	}
	for in, want := range cases {
		if got := displayWidth(in); got != want {
			t.Errorf("displayWidth(%q) = %d, 期望 %d", in, got, want)
		}
	}
}

// TestDisplayWidth_组合字符 组合字符记 0。
func TestDisplayWidth_组合字符(t *testing.T) {
	if got := displayWidth("e\u0301"); got != 1 {
		t.Errorf("e+组合符 = %d, 期望 1", got)
	}
}

// TestTruncate_按显示宽度 不切半个字符。
// Python: ui.py:38-54 _truncate
//
// 宽度 4 时 target = 4-1 = 3，中文字宽 2 → 只放得下 1 个字，故 "中…"（与上游一致）。
func TestTruncate_按显示宽度(t *testing.T) {
	if got := truncate("中文字符", 4); got != "中…" {
		t.Errorf("truncate(中文字符, 4) = %q, 期望 中…", got)
	}
	// 宽度 6 时 target = 5，放得下 2 个字
	if got := truncate("中文字符", 6); got != "中文…" {
		t.Errorf("truncate(中文字符, 6) = %q, 期望 中文…", got)
	}
	got := truncate("中文字符", 5)
	if displayWidth(got) > 5 {
		t.Errorf("truncate 结果宽度 %d 超过 5：%q", displayWidth(got), got)
	}
	if got := truncate("abc", 10); got != "abc" {
		t.Errorf("truncate(abc, 10) = %q", got)
	}
	if got := truncate("abc", 0); got != "" {
		t.Errorf("truncate(abc, 0) = %q, 期望空", got)
	}
}

// TestWrapDisplay_折行 按显示宽度折行。
// Python: ui.py:57-70 _wrap_display
func TestWrapDisplay_折行(t *testing.T) {
	lines := wrapDisplay("中文字符", 4)
	if len(lines) != 2 || lines[0] != "中文" || lines[1] != "字符" {
		t.Errorf("wrapDisplay(中文字符, 4) = %v", lines)
	}
	if got := wrapDisplay("", 10); len(got) != 1 || got[0] != "" {
		t.Errorf("空串应返回一个空行：%v", got)
	}
}

// TestShortSession 超过 12 字符截断加省略号。
// Python: ui.py:325-328
func TestShortSession(t *testing.T) {
	if got := shortSession("short"); got != "short" {
		t.Errorf("短 ID 原样：%q", got)
	}
	if got := shortSession("0123456789abcdef"); got != "0123456789ab…" {
		t.Errorf("长 ID 截断 = %q", got)
	}
	if got := shortSession(""); got != "" {
		t.Errorf("空串 = %q", got)
	}
}
