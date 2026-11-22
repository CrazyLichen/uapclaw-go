package rsi

// errors.go 单测：错误码逐字对齐 + failure_reason 键序对齐。

import (
	"errors"
	"strings"
	"testing"
)

// Test错误码逐字对齐 校验哨兵 Code 与 Python errors.py 各异常类一致。
func Test错误码逐字对齐(t *testing.T) {
	cases := map[string]string{
		"BAD_REQUEST":                 ErrBadRequest.Code,
		"SCENARIO_NOT_SUPPORTED":      ErrScenarioNotSupported.Code,
		"DATASET_INVALID":             ErrDatasetInvalid.Code,
		"PATH_INVALID":                ErrPathInvalid.Code,
		"PATH_NOT_ALLOWED":            ErrPathNotAllowed.Code,
		"INVALID_HARNESS":             ErrInvalidHarness.Code,
		"RSI_HARNESS_NOT_READY":       ErrRsiHarnessNotReady.Code,
		"RSI_HARNESS_NOT_PUBLISHED":   ErrRsiHarnessNotPublished.Code,
		"RSI_HARNESS_INVALID":         ErrRsiHarnessInvalid.Code,
		"RSI_HARNESS_INSTALL_CONFLICT": ErrRsiHarnessInstallConflict.Code,
		"RSI_HARNESS_INSTALL_FAILED":  ErrRsiHarnessInstallFailed.Code,
		"MODEL_NOT_FOUND":             ErrModelNotFound.Code,
		"MODEL_CONFIG_INVALID":        ErrModelConfigInvalid.Code,
		"UNSUPPORTED_PARAMETER":       ErrUnsupportedParameter.Code,
		"RESUME_INPUT_CHANGED":        ErrResumeInputChanged.Code,
		"TASK_NOT_FOUND":              ErrTaskNotFound.Code,
		"TASK_STATE_CONFLICT":         ErrTaskStateConflict.Code,
		"RESUME_MISMATCH":             ErrResumeMismatch.Code,
		"ARTIFACT_NOT_FOUND":          ErrArtifactNotFound.Code,
		"INTERNAL_ERROR":              ErrNotReady.Code,
	}
	for want, got := range cases {
		if got != want {
			t.Fatalf("错误码漂移: want=%s got=%s", want, got)
		}
	}
}

// TestIs_同码即同类 errors.Is 哨兵匹配不受实例影响。
func TestIs_同码即同类(t *testing.T) {
	err := NewTaskStateConflict("非法迁移")
	if !errors.Is(err, ErrTaskStateConflict) {
		t.Fatalf("同码哨兵应匹配")
	}
	if errors.Is(err, ErrTaskNotFound) {
		t.Fatalf("异码不应匹配")
	}
}

// TestTaskNotFound文案 对齐 Python f"任务不存在: {task_id}"。
func TestTaskNotFound文案(t *testing.T) {
	err := NewTaskNotFound("rsi-abc")
	if err.Error() != "任务不存在: rsi-abc" {
		t.Fatalf("got %q", err.Error())
	}
}

// TestFailureReason_mapping键序 对齐 Python dict 分支：
// errors/detail 结构化优先，再 error_message/last_reason/message，再 error_code。
func TestFailureReason_mapping键序(t *testing.T) {
	// errors 列表首条 message 优先。
	got := FailureReason(map[string]any{
		"errors":        []any{map[string]any{"message": "结构化消息"}},
		"error_message": "兜底消息",
	})
	if got != "结构化消息" {
		t.Fatalf("got %q", got)
	}
	// errors 缺失时走 error_message。
	got = FailureReason(map[string]any{"error_message": "兜底消息"})
	if got != "兜底消息" {
		t.Fatalf("got %q", got)
	}
	// 仅 error_code。
	got = FailureReason(map[string]any{"error_code": "SOME_CODE"})
	if got != "SOME_CODE" {
		t.Fatalf("got %q", got)
	}
	// 全空走 fallback。
	got = FailureReason(map[string]any{}, "fallback值")
	if got != "fallback值" {
		t.Fatalf("got %q", got)
	}
	// reason 键作为 message 缺席时的候补（Python item.get("message") or item.get("reason")）。
	got = FailureReason(map[string]any{"errors": []any{map[string]any{"reason": "原因键"}}})
	if got != "原因键" {
		t.Fatalf("got %q", got)
	}
}

// TestFailureReason_error形态 对齐 Python BaseException 分支。
func TestFailureReason_error形态(t *testing.T) {
	got := FailureReason(errors.New("引擎崩溃"))
	if got != "引擎崩溃" {
		t.Fatalf("got %q", got)
	}
}

// TestFailureReason_struct形态 对齐 Python 属性分支（reflect 同名字段）。
func TestFailureReason_struct形态(t *testing.T) {
	type providerResult struct {
		ErrorMessage string
		ErrorCode    string
	}
	got := FailureReason(providerResult{ErrorMessage: "模型超时", ErrorCode: "TIMEOUT"})
	if got != "模型超时" {
		t.Fatalf("got %q", got)
	}
	got = FailureReason(providerResult{ErrorCode: "TIMEOUT"})
	if got != "TIMEOUT" {
		t.Fatalf("got %q", got)
	}
}

// TestTruncateReason 对齐 Python [:500] 截断。
func TestTruncateReason(t *testing.T) {
	long := strings.Repeat("长", 600)
	got := FailureReason(map[string]any{"message": long})
	if len([]rune(got)) != 500 {
		t.Fatalf("截断长度漂移: %d", len([]rune(got)))
	}
}
