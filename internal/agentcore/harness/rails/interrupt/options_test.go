package interrupt

import (
	"testing"
)

// ──────────────────────────── extractUIOptions ────────────────────────────

// TestExtractUIOptions_取dict项 测试只保留 dict 元素。
func TestExtractUIOptions_取dict项(t *testing.T) {
	value := map[string]any{
		"ui_options": []any{
			map[string]any{"label": "A", "value": "allow_once"},
			"plain_string",
			map[string]any{"label": "B", "value": "reject"},
		},
	}
	got := extractUIOptions(value)
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2（字符串项应被丢弃）", len(got))
	}
	if got[0]["label"] != "A" || got[1]["label"] != "B" {
		t.Errorf("内容错误: %v", got)
	}
}

// TestExtractUIOptions_无字段返回nil 测试缺字段。
func TestExtractUIOptions_无字段返回nil(t *testing.T) {
	if got := extractUIOptions(map[string]any{}); got != nil {
		t.Errorf("got %v, want nil", got)
	}
	if got := extractUIOptions(nil); got != nil {
		t.Errorf("nil got %v, want nil", got)
	}
}

// TestExtractUIOptions_非map输入 测试类型兜底。
func TestExtractUIOptions_非map输入(t *testing.T) {
	if got := extractUIOptions("string"); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

// ──────────────────────────── normalizeQuestionOption ────────────────────────────

// TestNormalizeQuestionOption_label优先 测试 label 字段优先。
func TestNormalizeQuestionOption_label优先(t *testing.T) {
	got := normalizeQuestionOption(map[string]any{
		"label":       "本次允许",
		"value":       "allow_once",
		"description": "仅本次授权执行",
	})
	if got["label"] != "本次允许" {
		t.Errorf("label = %v", got["label"])
	}
	if got["value"] != "allow_once" {
		t.Errorf("value = %v", got["value"])
	}
	if got["description"] != "仅本次授权执行" {
		t.Errorf("description = %v", got["description"])
	}
}

// TestNormalizeQuestionOption_label缺失回落value 测试回落。
func TestNormalizeQuestionOption_label缺失回落value(t *testing.T) {
	got := normalizeQuestionOption(map[string]any{"value": "reject"})
	if got["label"] != "reject" {
		t.Errorf("label = %v, want reject（回落 value）", got["label"])
	}
}

// TestNormalizeQuestionOption_空value不保留 测试空字符串 value 被省略。
func TestNormalizeQuestionOption_空value不保留(t *testing.T) {
	got := normalizeQuestionOption(map[string]any{"label": "X", "value": ""})
	if _, exists := got["value"]; exists {
		t.Error("空 value 不应保留")
	}
}

// TestNormalizeQuestionOption_preview保留 测试非空 preview。
func TestNormalizeQuestionOption_preview保留(t *testing.T) {
	got := normalizeQuestionOption(map[string]any{
		"label":   "X",
		"preview": "diff...",
	})
	if got["preview"] != "diff..." {
		t.Errorf("preview = %v", got["preview"])
	}
}

// TestNormalizeQuestionOption_空白preview不保留 测试空白 preview 被省略。
func TestNormalizeQuestionOption_空白preview不保留(t *testing.T) {
	got := normalizeQuestionOption(map[string]any{"label": "X", "preview": "   "})
	if _, exists := got["preview"]; exists {
		t.Error("空白 preview 不应保留")
	}
}

// ──────────────────────────── defaultInterruptOptions ────────────────────────────

// TestDefaultInterruptOptions_个人版四档 测试个人版含「永久记住」。
func TestDefaultInterruptOptions_个人版四档(t *testing.T) {
	t.Setenv(envEdition, "")
	options := defaultInterruptOptions()
	if len(options) != 4 {
		t.Fatalf("len = %d, want 4（个人版四档）", len(options))
	}
	wantOrder := []string{AllowOnce, SessionAllow, AlwaysAllow, Reject}
	for index, want := range wantOrder {
		if options[index]["value"] != want {
			t.Errorf("options[%d].value = %v, want %v", index, options[index]["value"], want)
		}
	}
}

// TestDefaultInterruptOptions_企业版三档 测试企业版不含「永久记住」。
//
// Python: 企业版不提供 always 档（与 Manager 权威源不一致）。
func TestDefaultInterruptOptions_企业版三档(t *testing.T) {
	t.Setenv(envEdition, "enterprise")
	options := defaultInterruptOptions()
	if len(options) != 3 {
		t.Fatalf("len = %d, want 3（企业版三档）", len(options))
	}
	wantOrder := []string{AllowOnce, SessionAllow, Reject}
	for index, want := range wantOrder {
		if options[index]["value"] != want {
			t.Errorf("options[%d].value = %v, want %v", index, options[index]["value"], want)
		}
	}
	for _, option := range options {
		if option["value"] == AlwaysAllow {
			t.Error("企业版不应含 always_allow")
		}
	}
}

// TestDefaultInterruptOptions_选项值可被解析 测试词表一致（出题端与解题端共用）。
func TestDefaultInterruptOptions_选项值可被解析(t *testing.T) {
	t.Setenv(envEdition, "")
	for _, option := range defaultInterruptOptions() {
		value := option["value"]
		if ResolvePermissionAction(value) == "" {
			t.Errorf("选项 value %v 无法被 ResolvePermissionAction 解析——词表不一致", value)
		}
		label := option["label"]
		if ResolvePermissionAction(label) == "" {
			t.Errorf("选项 label %v 无法被解析（TUI 回传 label）", label)
		}
	}
}

// ──────────────────────────── isAlwaysAllowOption ────────────────────────────

// TestIsAlwaysAllowOption_永久记住 测试 always 档识别。
func TestIsAlwaysAllowOption_永久记住(t *testing.T) {
	if !isAlwaysAllowOption(map[string]any{"value": AlwaysAllow}) {
		t.Error("always_allow 应识别为 always 档")
	}
	if !isAlwaysAllowOption(map[string]any{"label": "总是允许"}) {
		t.Error("「总是允许」应识别为 always 档（演进卡说法）")
	}
}

// TestIsAlwaysAllowOption_其它档 测试非 always 档。
func TestIsAlwaysAllowOption_其它档(t *testing.T) {
	for _, option := range []map[string]any{
		{"value": AllowOnce},
		{"value": SessionAllow},
		{"value": Reject},
	} {
		if isAlwaysAllowOption(option) {
			t.Errorf("%v got true, want false", option)
		}
	}
}

// ──────────────────────────── stripPermanentRememberHint ────────────────────────────

// TestStripPermanentRememberHint_去掉提示 测试正则剥离。
func TestStripPermanentRememberHint_去掉提示(t *testing.T) {
	got := stripPermanentRememberHint("工具 `bash` 需要授权才能执行；选择「永久记住」将写回磁盘。")
	if got != "工具 `bash` 需要授权才能执行" {
		t.Errorf("got %q", got)
	}
}

// TestStripPermanentRememberHint_无提示不变 测试原文不含提示时不变。
func TestStripPermanentRememberHint_无提示不变(t *testing.T) {
	input := "工具 `bash` 需要授权才能执行"
	if got := stripPermanentRememberHint(input); got != input {
		t.Errorf("got %q, want %q", got, input)
	}
}

// TestStripPermanentRememberHint_空串 测试空输入。
func TestStripPermanentRememberHint_空串(t *testing.T) {
	if got := stripPermanentRememberHint(""); got != "" {
		t.Errorf("got %q", got)
	}
}
