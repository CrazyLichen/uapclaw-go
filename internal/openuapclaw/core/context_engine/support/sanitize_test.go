package support

import (
	"strings"
	"testing"
)

// TestSanitizeContentForText_字符串内嵌dataURL 验证字符串里的图片载荷被替换。
func TestSanitizeContentForText_字符串内嵌dataURL(t *testing.T) {
	payload := "data:image/png;base64," + strings.Repeat("A", 4000)
	got := SanitizeContentForText("截图：" + payload + " 结束")
	if strings.Contains(got, "base64") {
		t.Fatalf("载荷未被替换: %q", got[:80])
	}
	if !strings.Contains(got, ImageDataURLPlaceholder) || !strings.HasSuffix(got, " 结束") {
		t.Fatalf("应保留前后文并插入占位符，got %q", got)
	}
}

// TestSanitizeContentForText_非图片dataURL不误标 验证 application/pdf 不被当图片。
func TestSanitizeContentForText_非图片dataURL不误标(t *testing.T) {
	text := "data:application/pdf;base64,AAAA"
	if got := SanitizeContentForText(text); got != text {
		t.Fatalf("非图片 data URL 不应替换，got %q", got)
	}
}

// TestSanitizeValueForText_三种块形态 验证 image_url/data_url/source 均被替换。
func TestSanitizeValueForText_三种块形态(t *testing.T) {
	payload := "data:image/jpeg;base64," + strings.Repeat("B", 200)
	cases := []map[string]any{
		{"type": "image_url", "image_url": map[string]any{"url": payload}},
		{"type": "image", "data_url": payload},
		{"type": "image", "source": map[string]any{"type": "base64", "data": strings.Repeat("C", 200)}},
	}
	for _, block := range cases {
		sanitized, ok := SanitizeValueForText(block).(map[string]any)
		if !ok {
			t.Fatalf("应保持 map 结构: %v", block)
		}
		joined := SanitizeContentForText(sanitized)
		if strings.Contains(joined, "base64,") || strings.Contains(joined, strings.Repeat("C", 200)) {
			t.Fatalf("载荷未被替换: %s", joined)
		}
	}
}

// TestSanitizeValueForText_不改写原数据 验证浅拷贝语义。
func TestSanitizeValueForText_不改写原数据(t *testing.T) {
	payload := "data:image/png;base64," + strings.Repeat("D", 100)
	original := map[string]any{"type": "image", "data_url": payload}
	_ = SanitizeValueForText(original)
	if original["data_url"] != payload {
		t.Fatal("不应改写调用方 map")
	}
}

// TestSanitizeValueForText_嵌套列表 验证递归处理。
func TestSanitizeValueForText_嵌套列表(t *testing.T) {
	payload := "data:image/png;base64," + strings.Repeat("E", 100)
	value := []any{map[string]any{"url": payload}, "普通文本"}
	got := SanitizeContentForText(value)
	if strings.Contains(got, strings.Repeat("E", 100)) {
		t.Fatalf("嵌套列表内的载荷未被替换: %s", got)
	}
	if !strings.Contains(got, "普通文本") {
		t.Fatalf("普通文本应保留: %s", got)
	}
}
