package utils

import (
	"testing"
	"unicode/utf8"
)

// TestTruncateRunes 验证按字符截断（不产生半个 UTF-8 序列）。
func TestTruncateRunes(t *testing.T) {
	cases := []struct {
		name   string
		in     string
		maxLen int
		want   string
	}{
		{"短串原样", "hello", 10, "hello"},
		{"等长原样", "hello", 5, "hello"},
		{"英文截断", "hello world", 5, "hello"},
		{"中文整字截断", "中文文本测试", 4, "中文文本"},
		{"中文超长", "中文文本", 2, "中文"},
		{"混合字符", "a中b文c", 3, "a中b"},
		{"零长度", "abc", 0, ""},
		{"负长度", "abc", -1, ""},
		{"空串", "", 3, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TruncateRunes(c.in, c.maxLen)
			if got != c.want {
				t.Errorf("TruncateRunes(%q, %d) = %q, 期望 %q", c.in, c.maxLen, got, c.want)
			}
			// 截断结果必须是合法 UTF-8（字节切片实现会在此失败）
			if !utf8.ValidString(got) {
				t.Errorf("TruncateRunes(%q, %d) 产生了非法 UTF-8: %q", c.in, c.maxLen, got)
			}
		})
	}
}

// TestTruncateRunesEllipsis 验证省略号变体（原 evolving/store_projection 语义）。
func TestTruncateRunesEllipsis(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		maxRune int
		want    string
	}{
		{"短串原样", "hello", 10, "hello"},
		{"等长原样", "hello", 5, "hello"},
		{"英文补省略号", "hello world", 8, "hello..."},
		{"中文补省略号", "中文文本测试", 5, "中文..."},
		{"上限3退化纯截断", "abcdef", 3, "abc"},
		{"上限3中文", "中文文本", 3, "中文文"},
		{"上限0", "abcdef", 0, ""},
		{"尾部空格去掉", "ab  cd ef", 6, "ab..."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := TruncateRunesEllipsis(c.in, c.maxRune)
			if got != c.want {
				t.Errorf("TruncateRunesEllipsis(%q, %d) = %q, 期望 %q", c.in, c.maxRune, got, c.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("结果非法 UTF-8: %q", got)
			}
			if c.maxRune > 3 && utf8.RuneCountInString(got) > c.maxRune {
				t.Errorf("结果超长: %q (%d rune)", got, utf8.RuneCountInString(got))
			}
		})
	}
}
