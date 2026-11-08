package runtime

import (
	"errors"
	"testing"
)

// TestResolveSessionInputMode 主字段 / 遗留别名 / 冲突 / 未知值。
func TestResolveSessionInputMode(t *testing.T) {
	if got := ResolveSessionInputMode(map[string]any{"input_mode": "steer"}); got == nil || *got != SessionInputModeSteer {
		t.Errorf("input_mode=steer 应解析为 steer，实际 %v", got)
	}
	if got := ResolveSessionInputMode(map[string]any{"runtime_mode": "follow_up"}); got == nil || *got != SessionInputModeFollowUp {
		t.Errorf("runtime_mode 别名应生效，实际 %v", got)
	}
	if got := ResolveSessionInputMode(map[string]any{"input_mode": "STEER"}); got == nil || *got != SessionInputModeSteer {
		t.Errorf("大小写应归一化，实际 %v", got)
	}
	if got := ResolveSessionInputMode(map[string]any{"input_mode": "unknown"}); got != nil {
		t.Errorf("未知值应返回 nil，实际 %v", got)
	}
	if got := ResolveSessionInputMode(nil); got != nil {
		t.Errorf("nil 参数应返回 nil，实际 %v", got)
	}
	if got := ResolveSessionInputMode(map[string]any{}); got != nil {
		t.Errorf("无字段应返回 nil，实际 %v", got)
	}
	// 上游用真值判断：空串主字段不算"存在"，不触发冲突
	if got := ResolveSessionInputMode(map[string]any{"input_mode": "", "runtime_mode": "steer"}); got == nil || *got != SessionInputModeSteer {
		t.Errorf("主字段为空串时应取别名的值，实际 %v", got)
	}
}

// TestResolveSessionInputMode_主字段与别名冲突 冲突时报错。
func TestResolveSessionInputMode_主字段与别名冲突(t *testing.T) {
	_, err := ResolveSessionInputModeChecked(map[string]any{
		"input_mode":   "steer",
		"runtime_mode": "follow_up",
	})
	if err == nil {
		t.Fatal("主字段与别名冲突应报错")
	}
	if err.Error() != "input_mode and runtime_mode must agree" {
		t.Errorf("错误文案不符: %q", err.Error())
	}
	// 非冲突版本应吞掉错误返回 nil
	if got := ResolveSessionInputMode(map[string]any{
		"input_mode": "steer", "runtime_mode": "follow_up",
	}); got != nil {
		t.Errorf("冲突时 ResolveSessionInputMode 应返回 nil，实际 %v", got)
	}
}

// TestValidateSteerImageReferences 六类拒收分支 + 合法路径。
func TestValidateSteerImageReferences(t *testing.T) {
	for _, key := range []string{"images", "image_files", "files", "attachments", "audio_files", "video_files"} {
		_, err := ValidateSteerImageReferences(map[string]any{key: []any{"x"}})
		if err == nil {
			t.Errorf("字段 %s 非空时应拒收", key)
		}
	}

	if _, err := ValidateSteerImageReferences(map[string]any{"media_items": "not-a-list"}); err == nil {
		t.Error("media_items 非 list 应报错")
	}

	if _, err := ValidateSteerImageReferences(map[string]any{
		"media_items": []any{map[string]any{"type": "image", "path": "p", "filename": "f",
			"mime_type": "image/png", "size_bytes": float64(1), "extra": "x"}},
	}); err == nil {
		t.Error("含多余键应报错")
	}

	if _, err := ValidateSteerImageReferences(map[string]any{
		"media_items": []any{map[string]any{"type": "video", "path": "p", "filename": "f",
			"mime_type": "image/png", "size_bytes": float64(1)}},
	}); err == nil {
		t.Error("type 非 image 应报错")
	}

	if _, err := ValidateSteerImageReferences(map[string]any{
		"media_items": []any{map[string]any{"type": "image", "path": "", "filename": "f",
			"mime_type": "image/png", "size_bytes": float64(1)}},
	}); err == nil {
		t.Error("path 为空应报错")
	}

	if _, err := ValidateSteerImageReferences(map[string]any{
		"media_items": []any{map[string]any{"type": "image", "path": "p", "filename": "f",
			"mime_type": "image/bmp", "size_bytes": float64(1)}},
	}); err == nil {
		t.Error("不支持的 MIME 应报错")
	}

	refs, err := ValidateSteerImageReferences(map[string]any{
		"media_items": []any{map[string]any{"type": "image", "path": "p", "filename": "f",
			"mime_type": "image/png", "size_bytes": float64(1)}},
	})
	if err != nil {
		t.Fatalf("合法输入不应报错: %v", err)
	}
	if len(refs) != 1 {
		t.Errorf("应返回 1 条引用，实际 %d", len(refs))
	}

	refs, err = ValidateSteerImageReferences(map[string]any{})
	if err != nil || len(refs) != 0 {
		t.Errorf("无 media_items 应返回空且无错，实际 %v / %v", refs, err)
	}

	// 空切片视为"未提供"，不应拒收
	if _, err := ValidateSteerImageReferences(map[string]any{"files": []any{}}); err != nil {
		t.Errorf("空切片不应拒收: %v", err)
	}
}

// TestValidateSessionInput expected_execution_id 两条约束 + query 约束。
func TestValidateSessionInput(t *testing.T) {
	if err := ValidateSessionInput(map[string]any{}); err == nil {
		t.Error("无 query 应报错")
	}
	if err := ValidateSessionInput(map[string]any{"query": "   "}); err == nil {
		t.Error("空白 query 应报错")
	}
	if err := ValidateSessionInput(map[string]any{"query": "hello"}); err != nil {
		t.Errorf("合法 query 不应报错: %v", err)
	}
	if err := ValidateSessionInput(map[string]any{
		"query": "hi", "input_mode": "steer", "expected_execution_id": "",
	}); err == nil {
		t.Error("expected_execution_id 为空串应报错")
	}
	if err := ValidateSessionInput(map[string]any{
		"query": "hi", "expected_execution_id": "e1",
	}); err == nil {
		t.Error("非 steer 模式带 expected_execution_id 应报错")
	}
	if err := ValidateSessionInput(map[string]any{
		"query": "hi", "input_mode": "steer", "expected_execution_id": "e1",
	}); err != nil {
		t.Errorf("合法组合不应报错: %v", err)
	}
	if err := ValidateSessionInput(map[string]any{
		"input_mode": "steer",
		"media_items": []any{map[string]any{"type": "image", "path": "p", "filename": "f",
			"mime_type": "image/jpeg", "size_bytes": float64(1)}},
	}); err != nil {
		t.Errorf("仅图片的 steer 输入不应报错: %v", err)
	}
}

// TestSessionInputErrors_错误码 三个异常的稳定 code。
func TestSessionInputErrors_错误码(t *testing.T) {
	cases := []struct {
		err  error
		code string
	}{
		{&SessionInputRejectedError{Message: "x"}, "SESSION_INPUT_REJECTED"},
		{&SessionInputTargetError{Message: "x"}, "SESSION_INPUT_TARGET_CHANGED"},
		{&SessionInputQueueRequiredError{Message: "x"}, "SESSION_INPUT_QUEUE_REQUIRED"},
	}
	for _, tc := range cases {
		var coded interface{ Code() string }
		if !errors.As(tc.err, &coded) {
			t.Fatalf("%T 应实现 Code()", tc.err)
		}
		if coded.Code() != tc.code {
			t.Errorf("%T code = %q, want %q", tc.err, coded.Code(), tc.code)
		}
	}
}

// TestSessionInputErrors_继承关系 两个子类可被 errors.As 命中父类（对齐上游继承）。
func TestSessionInputErrors_继承关系(t *testing.T) {
	var rejected *SessionInputRejectedError
	if !errors.As(&SessionInputTargetError{Message: "x"}, &rejected) {
		t.Error("TargetError 应可被识别为 RejectedError")
	}
	if !errors.As(&SessionInputQueueRequiredError{Message: "x"}, &rejected) {
		t.Error("QueueRequiredError 应可被识别为 RejectedError")
	}
}

// TestSessionInputErrors_Error消息 Error() 返回 Message 原文。
func TestSessionInputErrors_Error消息(t *testing.T) {
	cases := []error{
		&SessionInputRejectedError{Message: "rejected-msg"},
		&SessionInputTargetError{Message: "target-msg"},
		&SessionInputQueueRequiredError{Message: "queue-msg"},
	}
	wants := []string{"rejected-msg", "target-msg", "queue-msg"}
	for i, err := range cases {
		if err.Error() != wants[i] {
			t.Errorf("%T.Error() = %q, want %q", err, err.Error(), wants[i])
		}
	}
}

// TestIsTruthySessionInputValue 复刻 Python 真值判断。
func TestIsTruthySessionInputValue(t *testing.T) {
	truthy := []any{true, "x", []any{"a"}, map[string]any{"k": "v"}, 1, int64(2), 3.5}
	for _, value := range truthy {
		if !isTruthySessionInputValue(value) {
			t.Errorf("%v 应为真", value)
		}
	}
	falsy := []any{nil, false, "", []any{}, map[string]any{}, 0, int64(0), 0.0}
	for _, value := range falsy {
		if isTruthySessionInputValue(value) {
			t.Errorf("%v 应为假", value)
		}
	}
}

// TestResolveSessionInputMode_枚举直传 直接给枚举值。
func TestResolveSessionInputMode_枚举直传(t *testing.T) {
	if got := ResolveSessionInputMode(map[string]any{"input_mode": SessionInputModeSteer}); got == nil || *got != SessionInputModeSteer {
		t.Errorf("枚举值直传应识别，实际 %v", got)
	}
}

// TestNormalizeSessionInputRaw 非字符串输入归一化为空串。
func TestNormalizeSessionInputRaw(t *testing.T) {
	if got := normalizeSessionInputRaw(nil); got != "" {
		t.Errorf("nil 应归一化为空串，实际 %q", got)
	}
	if got := normalizeSessionInputRaw(42); got != "" {
		t.Errorf("非字符串应归一化为空串，实际 %q", got)
	}
	if got := normalizeSessionInputRaw("  STEER  "); got != "steer" {
		t.Errorf("应去空白并小写，实际 %q", got)
	}
}

// TestValidateSessionInput_非字符串query 有图片但 query 非字符串时仍要求文本。
func TestValidateSessionInput_非字符串query(t *testing.T) {
	err := ValidateSessionInput(map[string]any{
		"input_mode": "steer",
		"query":      map[string]any{"bad": true},
		"media_items": []any{map[string]any{"type": "image", "path": "p", "filename": "f",
			"mime_type": "image/png", "size_bytes": float64(1)}},
	})
	if err == nil {
		t.Error("query 非字符串时应报 session input requires non-empty text")
	}
}
