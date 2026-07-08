package exception

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewBaseError_BasicFields(t *testing.T) {
	err := NewBaseError(StatusError)
	if err.Status() != StatusError {
		t.Errorf("Status 期望 %v，实际 %v", StatusError, err.Status())
	}
	if err.Code() != -1 {
		t.Errorf("Code 期望 -1，实际 %d", err.Code())
	}
	if err.Category() != ErrorCategoryExecution {
		// StatusError 命中关键字 "ERROR" → Execution
		t.Errorf("Category 期望 Execution，实际 %v", err.Category())
	}
}

func TestNewBaseError_WithParams(t *testing.T) {
	err := NewBaseError(
		NewStatusCode("TEST_ERROR", 1, "error in {module}, reason: {reason}"),
		WithParam("module", "auth"),
		WithParam("reason", "token expired"),
	)
	if err.TemplateMessage() != "error in auth, reason: token expired" {
		t.Errorf("TemplateMessage 期望渲染结果，实际 %q", err.TemplateMessage())
	}
	if err.Params()["module"] != "auth" {
		t.Errorf("Params[module] 期望 auth，实际 %v", err.Params()["module"])
	}
}

func TestNewBaseError_WithMsg(t *testing.T) {
	err := NewBaseError(StatusError, WithMsg("custom message"))
	if err.Message() != "custom message" {
		t.Errorf("Message 期望 %q，实际 %q", "custom message", err.Message())
	}
	// TemplateMessage 仍为渲染后的模板消息
	if err.TemplateMessage() != "error" {
		t.Errorf("TemplateMessage 期望 %q，实际 %q", "error", err.TemplateMessage())
	}
}

func TestNewBaseError_WithCause(t *testing.T) {
	inner := fmt.Errorf("original error")
	err := NewBaseError(StatusError, WithCause(inner))
	if !errors.Is(err, inner) {
		t.Error("errors.Is 应沿 cause 链找到原始错误")
	}
	if err.Unwrap() != inner {
		t.Errorf("Unwrap 期望 %v，实际 %v", inner, err.Unwrap())
	}
}

func TestNewBaseError_WithDetails(t *testing.T) {
	details := map[string]any{"key": "value"}
	err := NewBaseError(StatusError, WithDetails(details))
	if err.Details() == nil {
		t.Error("Details 不应为 nil")
	}
	d, ok := err.Details().(map[string]any)
	if !ok {
		t.Error("Details 类型断言失败")
	}
	if d["key"] != "value" {
		t.Errorf("Details[key] 期望 value，实际 %v", d["key"])
	}
}

func TestNewBaseError_WithParamsMap(t *testing.T) {
	err := NewBaseError(
		NewStatusCode("TEST", 1, "a={a}, b={b}"),
		WithParams(map[string]any{"a": 1, "b": 2}),
	)
	if err.TemplateMessage() != "a=1, b=2" {
		t.Errorf("TemplateMessage 期望 %q，实际 %q", "a=1, b=2", err.TemplateMessage())
	}
}

func TestBaseError_Error(t *testing.T) {
	err := NewBaseError(StatusError)
	if err.Error() != "[-1] error" {
		t.Errorf("Error() 期望 %q，实际 %q", "[-1] error", err.Error())
	}
}

func TestBaseError_String(t *testing.T) {
	err := NewBaseError(StatusError)
	if err.String() != "[-1] error" {
		t.Errorf("String() 期望 %q，实际 %q", "[-1] error", err.String())
	}
}

func TestBaseError_IsFatal_Framework(t *testing.T) {
	err := SystemError(StatusError)
	if !err.IsFatal() {
		t.Error("Framework 类别应为 fatal")
	}
	if err.IsRecoverable() {
		t.Error("Framework 类别不应 recoverable")
	}
}

func TestBaseError_IsFatal_Validation(t *testing.T) {
	err := ValidateError(StatusError)
	if err.IsFatal() {
		t.Error("Validation 类别不应 fatal")
	}
	if err.IsRecoverable() {
		t.Error("Validation 类别不应 recoverable")
	}
}

func TestBaseError_IsRecoverable_Execution(t *testing.T) {
	err := NewBaseError(StatusError)
	// StatusError 命中关键字 "ERROR" → Execution
	if !err.IsRecoverable() {
		t.Error("Execution 类别应 recoverable")
	}
	if err.IsFatal() {
		t.Error("Execution 类别不应 fatal")
	}
}

func TestBaseError_IsFatal_Termination(t *testing.T) {
	err := Terminate(StatusSuccess)
	if err.IsFatal() {
		t.Error("Termination 类别不应 fatal")
	}
	if err.IsRecoverable() {
		t.Error("Termination 类别不应 recoverable")
	}
}

func TestBaseError_ToDict(t *testing.T) {
	err := NewBaseError(
		NewStatusCode("TEST_CODE", 99999, "test {param}"),
		WithParam("param", "value"),
	)
	d := err.ToDict()
	if d["code"] != 99999 {
		t.Errorf("code 期望 99999，实际 %v", d["code"])
	}
	if d["status"] != "TEST_CODE" {
		t.Errorf("status 期望 %q，实际 %v", "TEST_CODE", d["status"])
	}
	if d["message"] != "test value" {
		t.Errorf("message 期望 %q，实际 %v", "test value", d["message"])
	}
	if d["raw_message"] != "test value" {
		t.Errorf("raw_message 期望 %q，实际 %v", "test value", d["raw_message"])
	}
	if d["category"] != "ExecutionError" {
		t.Errorf("category 期望 %q，实际 %v", "ExecutionError", d["category"])
	}
	if d["fatal"] != false {
		t.Errorf("fatal 期望 false，实际 %v", d["fatal"])
	}
	if d["recoverable"] != true {
		t.Errorf("recoverable 期望 true，实际 %v", d["recoverable"])
	}
}

func TestBaseError_ToDict_WithMsg(t *testing.T) {
	err := NewBaseError(
		NewStatusCode("TEST_CODE", 99999, "test {param}"),
		WithParam("param", "value"),
		WithMsg("custom"),
	)
	d := err.ToDict()
	// message 是渲染后的模板消息
	if d["message"] != "test value" {
		t.Errorf("message 期望 %q，实际 %v", "test value", d["message"])
	}
	// raw_message 是最终消息（自定义覆盖）
	if d["raw_message"] != "custom" {
		t.Errorf("raw_message 期望 %q，实际 %v", "custom", d["raw_message"])
	}
}

func TestBaseError_ToJSON(t *testing.T) {
	err := NewBaseError(StatusError)
	j := err.ToJSON()
	var raw map[string]any
	if err := json.Unmarshal([]byte(j), &raw); err != nil {
		t.Fatalf("ToJSON 输出不是合法 JSON: %v", err)
	}
	if raw["code"] != float64(-1) {
		t.Errorf("code 期望 -1，实际 %v", raw["code"])
	}
}

func TestBaseError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("inner")
	err := NewBaseError(StatusError, WithCause(inner))
	if unwrapped := err.Unwrap(); unwrapped != inner {
		t.Errorf("Unwrap 期望 %v，实际 %v", inner, unwrapped)
	}
}

func TestBaseError_Unwrap_Nil(t *testing.T) {
	err := NewBaseError(StatusError)
	if unwrapped := err.Unwrap(); unwrapped != nil {
		t.Errorf("无 cause 时 Unwrap 期望 nil，实际 %v", unwrapped)
	}
}

func TestBaseError_ErrorsAs(t *testing.T) {
	inner := fmt.Errorf("inner error")
	err := NewBaseError(StatusError, WithCause(inner))

	var baseErr *BaseError
	if !errors.As(err, &baseErr) {
		t.Error("errors.As 应能将 error 转为 *BaseError")
	}
	if baseErr.Code() != -1 {
		t.Errorf("errors.As 后 Code 期望 -1，实际 %d", baseErr.Code())
	}
}

func TestBuildError(t *testing.T) {
	err := BuildError(StatusError)
	if err == nil {
		t.Error("BuildError 不应返回 nil")
	}
	if err.Code() != -1 {
		t.Errorf("Code 期望 -1，实际 %d", err.Code())
	}
}

func TestRaiseError(t *testing.T) {
	err := RaiseError(StatusError)
	if err == nil {
		t.Error("RaiseError 不应返回 nil")
	}
}

func TestSystemError(t *testing.T) {
	err := SystemError(StatusError)
	if err.Category() != ErrorCategoryFramework {
		t.Errorf("Category 期望 %v，实际 %v", ErrorCategoryFramework, err.Category())
	}
	if !err.IsFatal() {
		t.Error("SystemError 应为 fatal")
	}
}

func TestValidateError(t *testing.T) {
	err := ValidateError(StatusError)
	if err.Category() != ErrorCategoryValidation {
		t.Errorf("Category 期望 %v，实际 %v", ErrorCategoryValidation, err.Category())
	}
	if err.IsRecoverable() {
		t.Error("ValidateError 不应 recoverable")
	}
}

func TestTerminate(t *testing.T) {
	err := Terminate(StatusSuccess)
	if err.Category() != ErrorCategoryTermination {
		t.Errorf("Category 期望 %v，实际 %v", ErrorCategoryTermination, err.Category())
	}
	if err.IsFatal() {
		t.Error("Terminate 不应 fatal")
	}
	if err.IsRecoverable() {
		t.Error("Terminate 不应 recoverable")
	}
}

func TestErrorCategory_String(t *testing.T) {
	tests := []struct {
		category ErrorCategory
		expected string
	}{
		{ErrorCategoryFramework, "FrameworkError"},
		{ErrorCategoryValidation, "ValidationError"},
		{ErrorCategoryExecution, "ExecutionError"},
		{ErrorCategoryTermination, "Termination"},
	}
	for _, tt := range tests {
		if got := tt.category.String(); got != tt.expected {
			t.Errorf("ErrorCategory(%d).String() = %q，期望 %q", tt.category, got, tt.expected)
		}
	}
}

func TestErrorCategory_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(ErrorCategoryExecution)
	if err != nil {
		t.Fatalf("MarshalJSON 失败: %v", err)
	}
	if string(data) != `"ExecutionError"` {
		t.Errorf("期望 %q，实际 %q", `"ExecutionError"`, string(data))
	}
}

func TestBaseError_Code_DelegatesToStatus(t *testing.T) {
	// 验证 BaseError.Code() 委托给 StatusCode.Code()，无冗余缓存
	status := NewStatusCode("TEST", 42, "msg")
	err := NewBaseError(status)
	if err.Code() != 42 {
		t.Errorf("Code 期望 42（委托 status.Code()），实际 %d", err.Code())
	}
	if err.Status().Code() != 42 {
		t.Errorf("Status.Code() 期望 42，实际 %d", err.Status().Code())
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestBaseError_Cause 验证 Cause() 返回原始错误。
func TestBaseError_Cause(t *testing.T) {
	inner := fmt.Errorf("root cause")
	err := NewBaseError(StatusError, WithCause(inner))
	if got := err.Cause(); got != inner {
		t.Errorf("Cause() = %v, want %v", got, inner)
	}
}

// TestBaseError_Cause_Nil 验证无 cause 时 Cause() 返回 nil。
func TestBaseError_Cause_Nil(t *testing.T) {
	err := NewBaseError(StatusError)
	if got := err.Cause(); got != nil {
		t.Errorf("无 cause 时 Cause() 应为 nil，实际 %v", got)
	}
}

// TestErrorCategory_String_越界 验证越界 ErrorCategory.String() 格式化输出。
func TestErrorCategory_String_越界(t *testing.T) {
	cat := ErrorCategory(99)
	got := cat.String()
	want := "ErrorCategory(99)"
	if got != want {
		t.Errorf("ErrorCategory(99).String() = %q, want %q", got, want)
	}
}

// TestBaseError_SetCategory 验证 SetCategory 覆盖自动解析的类别。
func TestBaseError_SetCategory(t *testing.T) {
	// StatusError 默认命中 "ERROR" → Execution
	err := NewBaseError(StatusError)
	if err.Category() != ErrorCategoryExecution {
		t.Errorf("默认 Category 期望 Execution，实际 %v", err.Category())
	}

	// 覆盖为 Framework
	err.SetCategory(ErrorCategoryFramework)
	if err.Category() != ErrorCategoryFramework {
		t.Errorf("SetCategory 后期望 Framework，实际 %v", err.Category())
	}
	if !err.IsFatal() {
		t.Error("Framework 类别应为 fatal")
	}

	// 覆盖为 Validation
	err.SetCategory(ErrorCategoryValidation)
	if err.Category() != ErrorCategoryValidation {
		t.Errorf("SetCategory 后期望 Validation，实际 %v", err.Category())
	}
	if err.IsRecoverable() {
		t.Error("Validation 类别不应 recoverable")
	}

	// 覆盖为 Termination
	err.SetCategory(ErrorCategoryTermination)
	if err.Category() != ErrorCategoryTermination {
		t.Errorf("SetCategory 后期望 Termination，实际 %v", err.Category())
	}
	if err.IsFatal() {
		t.Error("Termination 类别不应 fatal")
	}

	// 覆盖为 Execution
	err.SetCategory(ErrorCategoryExecution)
	if err.Category() != ErrorCategoryExecution {
		t.Errorf("SetCategory 后期望 Execution，实际 %v", err.Category())
	}
	if !err.IsRecoverable() {
		t.Error("Execution 类别应 recoverable")
	}
}
