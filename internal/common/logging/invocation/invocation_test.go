package invocation

import (
	"errors"
	"strings"
	"testing"
)

// TestClip 对照 Python _clip：先按 rune 截 limit，再按字节截 limit，坏 rune 丢弃
func TestClip(t *testing.T) {
	// ASCII 短串原样
	if got := clip("abc", 5); got != "abc" {
		t.Fatalf("clip(abc,5) = %q", got)
	}
	// ASCII 超长截断
	if got := clip("abcdef", 3); got != "abc" {
		t.Fatalf("clip(abcdef,3) = %q", got)
	}
	// 多字节：limit 个 rune（encode 后 [:2] 字节为残缺序列，decode ignore 全部丢弃——Python 同语义）
	if got := clip("你好世界", 2); got != "" {
		t.Fatalf("clip(你好世界,2) = %q, want 空", got)
	}
	// 字节上限：3 字节 = 1 个中文 rune（encode 后 [:3] 截掉半个字符，decode ignore 丢弃）
	if got := clip("你好", 3); got != "你" {
		t.Fatalf("clip(你好,3) = %q, want 你", got)
	}
	if clip("", 10) != "" {
		t.Fatal("空串应原样")
	}
}

// TestSummary 脱敏正则族逐条正例/反例
func TestSummary(t *testing.T) {
	cases := []struct {
		name  string
		value any
		want  string
	}{
		{"None返回空", nil, ""},
		// Python 实测：Bearer 先脱敏后 SECRET_RE 再命中 "Bearer" 值
		{"Bearer脱敏", "Authorization: Bearer abc.def.ghi", "Authorization: <REDACTED> <REDACTED>"},
		{"apiKey脱敏", "api_key = supersecret123", "api_key = <REDACTED>"},
		{"skKey脱敏", "key: sk-abcdefghijklmnopqrstuvwxyz", "key: <REDACTED>"},
		{"普通文本保留", "diagnosis complete", "diagnosis complete"},
		{"布尔标量保留", true, "true"},
		{"数字标量保留", 42, "42"},
		// Python 实测：payload 键在串首时 text[:0] 截断
		{"payload键截断", "input_content: {\"huge\": 1}", "[payload omitted]"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := summary(c.value, 768); got != c.want {
				t.Fatalf("summary(%v) = %q, want %q", c.value, got, c.want)
			}
		})
	}
	// 容器值不 stringify
	if got := summary(map[string]any{"k": "v"}, 768); got != "[payload omitted]" {
		t.Fatalf("容器值 = %q, want [payload omitted]", got)
	}
	// 控制字符替换为空格
	if got := summary("a\x00b\x1fc", 768); got != "a b c" {
		t.Fatalf("控制字符 = %q", got)
	}
}

// TestLabel label 上限 96
func TestLabel(t *testing.T) {
	if got := label(strings.Repeat("x", 200)); len(got) != 96 {
		t.Fatalf("label 长度 = %d, want 96", len(got))
	}
}

// TestFormatSummary 占位符替换
func TestFormatSummary(t *testing.T) {
	// 花括号位置参数
	if got := formatSummary("turn {0} of {1}", []any{1, 5}); got != "turn 1 of 5" {
		t.Fatalf("brace = %q", got)
	}
	// %s 位置参数
	if got := formatSummary("model: %s", []any{"qwen"}); got != "model: qwen" {
		t.Fatalf("percent = %q", got)
	}
	// 越界占位符原样保留
	if got := formatSummary("x {0} y", nil); got != "x {0} y" {
		t.Fatalf("越界 = %q", got)
	}
	// 敏感模板：secret 先于 formatSummary 在 _summary 内脱敏，敏感词命中后不再做占位符替换（Python 同序）
	if got := formatSummary("password: {0}", []any{"secret"}); got != "password: <REDACTED>}" {
		t.Fatalf("敏感模板 = %q", got)
	}
}

// TestMessageFields JSON 消息字段提取
func TestMessageFields(t *testing.T) {
	// dict 直返
	m := map[string]any{"message": "x"}
	if got := messageFields(m); got["message"] != "x" {
		t.Fatalf("dict 直返失败: %v", got)
	}
	// JSON 字符串解析
	got := messageFields(`{"message": "parsed", "status": "ok"}`)
	if got["message"] != "parsed" || got["status"] != "ok" {
		t.Fatalf("JSON 解析失败: %v", got)
	}
	// 非对象 JSON
	if got := messageFields("[1,2]"); got["message"] != "[structured payload omitted]" {
		t.Fatalf("数组 JSON = %v", got)
	}
	// 坏 JSON
	if got := messageFields("{not json"); got["message"] == "" {
		t.Fatalf("坏 JSON 应回退 summary: %v", got)
	}
	// 超 _MAX_JSON_CHARS
	oversized := "{" + strings.Repeat("x", maxJSONChars) + "}"
	if got := messageFields(oversized); got["message"] != "[oversized structured payload omitted]" {
		t.Fatalf("超大 JSON = %v", got)
	}
	// 普通字符串
	if got := messageFields("plain"); got["message"] != "plain" {
		t.Fatalf("普通串 = %v", got)
	}
}

// TestExceptionSummary 异常摘要
func TestExceptionSummary(t *testing.T) {
	if got := exceptionSummary(nil); got != "" {
		t.Fatalf("nil 应返回空串, got %q", got)
	}
	err := errors.New("boom detail")
	got := exceptionSummary(err)
	if !strings.HasPrefix(got, "runtimeError: boom detail") && !strings.Contains(got, "boom detail") {
		t.Fatalf("异常摘要缺类型与详情: %q", got)
	}
}
