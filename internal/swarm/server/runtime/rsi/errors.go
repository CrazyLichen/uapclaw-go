package rsi

// RSI 服务域错误体系：错误码对齐 web 契约 §3.5 全集。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/errors.py（199 行）。

import (
	"fmt"
	"reflect"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// failureReasonLimit 失败原因截断上限（Python _FAILURE_REASON_LIMIT）。
const failureReasonLimit = 500

// ──────────────────────────── 结构体 ────────────────────────────

// RsiError RSI 服务域统一错误：携带对外错误码（web §3.5）。
type RsiError struct {
	// Code 对外错误码（如 BAD_REQUEST / TASK_NOT_FOUND）。
	Code string
	// Message 对外错误文案。
	Message string
	// Detail 附带明细（如 DATASET_INVALID 的 errors 列表）。
	Detail any
}

// Error 实现 error 接口。
func (e *RsiError) Error() string {
	return e.Message
}

// Is 按 Code 判等：同码即同类（哨兵匹配用，不受实例不同影响）。
func (e *RsiError) Is(target error) bool {
	te, ok := target.(*RsiError)
	return ok && te.Code == e.Code
}

// 错误哨兵（错误码逐字对齐 Python 各异常类构造）。
var (
	// ErrBadRequest 对齐 Python RsiBadRequest。
	ErrBadRequest = &RsiError{Code: "BAD_REQUEST"}
	// ErrScenarioNotSupported 对齐 Python RsiScenarioNotSupported。
	ErrScenarioNotSupported = &RsiError{Code: "SCENARIO_NOT_SUPPORTED", Message: "scenario 或 artifact_type 非法"}
	// ErrDatasetInvalid 对齐 Python RsiDatasetInvalid。
	ErrDatasetInvalid = &RsiError{Code: "DATASET_INVALID"}
	// ErrPathInvalid 对齐 Python RsiPathInvalid。
	ErrPathInvalid = &RsiError{Code: "PATH_INVALID", Message: "本地路径不存在或后缀不符"}
	// ErrPathNotAllowed 对齐 Python RsiPathNotAllowed。
	ErrPathNotAllowed = &RsiError{Code: "PATH_NOT_ALLOWED", Message: "路径不在允许的 RSI 根目录内"}
	// ErrInvalidHarness 对齐 Python RsiInvalidHarness。
	ErrInvalidHarness = &RsiError{Code: "INVALID_HARNESS", Message: "Harness 配置无效"}
	// ErrRsiHarnessNotReady 对齐 Python RsiHarnessNotReady。
	ErrRsiHarnessNotReady = &RsiError{Code: "RSI_HARNESS_NOT_READY", Message: "RSI Harness 尚未完成"}
	// ErrRsiHarnessNotPublished 对齐 Python RsiHarnessNotPublished。
	ErrRsiHarnessNotPublished = &RsiError{Code: "RSI_HARNESS_NOT_PUBLISHED", Message: "RSI Harness 没有可发布版本"}
	// ErrRsiHarnessInvalid 对齐 Python RsiHarnessInvalid。
	ErrRsiHarnessInvalid = &RsiError{Code: "RSI_HARNESS_INVALID", Message: "RSI Harness 发布物无效"}
	// ErrRsiHarnessInstallConflict 对齐 Python RsiHarnessInstallConflict。
	ErrRsiHarnessInstallConflict = &RsiError{Code: "RSI_HARNESS_INSTALL_CONFLICT", Message: "RSI Harness 安装发生资源冲突"}
	// ErrRsiHarnessInstallFailed 对齐 Python RsiHarnessInstallFailed。
	ErrRsiHarnessInstallFailed = &RsiError{Code: "RSI_HARNESS_INSTALL_FAILED", Message: "RSI Harness 安装失败"}
	// ErrModelNotFound 对齐 Python RsiModelNotFound。
	ErrModelNotFound = &RsiError{Code: "MODEL_NOT_FOUND", Message: "模型不存在"}
	// ErrModelConfigInvalid 对齐 Python RsiModelConfigInvalid。
	ErrModelConfigInvalid = &RsiError{Code: "MODEL_CONFIG_INVALID", Message: "模型配置无效"}
	// ErrUnsupportedParameter 对齐 Python RsiUnsupportedParameter。
	ErrUnsupportedParameter = &RsiError{Code: "UNSUPPORTED_PARAMETER", Message: "参数暂不支持"}
	// ErrResumeInputChanged 对齐 Python RsiResumeInputChanged。
	ErrResumeInputChanged = &RsiError{Code: "RESUME_INPUT_CHANGED", Message: "resume 输入材料已变化"}
	// ErrTaskNotFound 对齐 Python RsiTaskNotFound。
	ErrTaskNotFound = &RsiError{Code: "TASK_NOT_FOUND"}
	// ErrTaskStateConflict 对齐 Python RsiTaskStateConflict。
	ErrTaskStateConflict = &RsiError{Code: "TASK_STATE_CONFLICT"}
	// ErrResumeMismatch 对齐 Python RsiResumeMismatch。
	ErrResumeMismatch = &RsiError{Code: "RESUME_MISMATCH", Message: "resume fingerprint 校验失败"}
	// ErrArtifactNotFound 对齐 Python RsiArtifactNotFound。
	ErrArtifactNotFound = &RsiError{Code: "ARTIFACT_NOT_FOUND", Message: "artifact 不存在"}
	// ErrNotReady 对齐 Python RsiNotReady：外部依赖未就绪的预留位，
	// 显式报错而非静默假实现；前端收到 INTERNAL_ERROR 视作"暂不可用"。
	ErrNotReady = &RsiError{Code: "INTERNAL_ERROR"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRsiError 构造指定码与文案的 RSI 错误。
func NewRsiError(code, message string, detail any) *RsiError {
	return &RsiError{Code: code, Message: message, Detail: detail}
}

// NewBadRequest 对齐 Python RsiBadRequest(message)。
func NewBadRequest(message string, detail any) *RsiError {
	return &RsiError{Code: "BAD_REQUEST", Message: message, Detail: detail}
}

// NewScenarioNotSupported 对齐 Python RsiScenarioNotSupported(message)。
func NewScenarioNotSupported(message string) *RsiError {
	if message == "" {
		message = ErrScenarioNotSupported.Message
	}
	return &RsiError{Code: "SCENARIO_NOT_SUPPORTED", Message: message}
}

// NewDatasetInvalid 对齐 Python RsiDatasetInvalid(message, errors)。
func NewDatasetInvalid(message string, errors []map[string]string) *RsiError {
	detail := any(errors)
	if errors == nil {
		detail = []map[string]string{}
	}
	return &RsiError{Code: "DATASET_INVALID", Message: message, Detail: detail}
}

// NewTaskNotFound 对齐 Python RsiTaskNotFound(task_id)。
func NewTaskNotFound(taskID string) *RsiError {
	return &RsiError{Code: "TASK_NOT_FOUND", Message: fmt.Sprintf("任务不存在: %s", taskID)}
}

// NewTaskStateConflict 对齐 Python RsiTaskStateConflict(message)。
func NewTaskStateConflict(message string) *RsiError {
	return &RsiError{Code: "TASK_STATE_CONFLICT", Message: message}
}

// FailureReason 把异常或 Provider 结果归一化为简短失败原因。
// 对齐 Python errors.py failure_reason：
//  1. dict 形态：先 errors/detail 内首条结构化消息，再
//     error_message/last_reason/message 键序，最后 error_code，否则 fallback；
//  2. struct 形态（Python 属性分支，Go 以 reflect 读同名字段）同键序；
//  3. error 形态：取 Error() 文本（对齐 Python BaseException 分支）；
//  4. 其余返回 fallback。统一截断 failureReasonLimit。
func FailureReason(value any, fallback ...string) string {
	fb := ""
	if len(fallback) > 0 {
		fb = fallback[0]
	}
	if value == nil {
		return truncateReason(fb)
	}
	switch v := value.(type) {
	case map[string]any:
		return failureReasonFromMapping(v, fb)
	case error:
		if msg := strings.TrimSpace(v.Error()); msg != "" {
			return truncateReason(msg)
		}
		return truncateReason(fb)
	case string:
		if msg := strings.TrimSpace(v); msg != "" {
			return truncateReason(msg)
		}
		return truncateReason(fb)
	default:
		// Python 属性分支：struct 字段按 error_message/last_reason/message 顺序。
		return failureReasonFromStruct(value, fb)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// failureReasonFromMapping 对齐 Python dict 分支键序。
func failureReasonFromMapping(m map[string]any, fallback string) string {
	if structured := firstErrorMessage(m["errors"]); structured != "" {
		return structured
	}
	if structured := firstErrorMessage(m["detail"]); structured != "" {
		return structured
	}
	for _, key := range []string{"error_message", "last_reason", "message"} {
		if msg := strings.TrimSpace(valueToString(m[key])); msg != "" {
			return truncateReason(msg)
		}
	}
	code := strings.TrimSpace(valueToString(m["error_code"]))
	if code != "" {
		return truncateReason(code)
	}
	return truncateReason(fallback)
}

// failureReasonFromStruct 对齐 Python 属性分支（reflect 读同名字段）。
func failureReasonFromStruct(value any, fallback string) string {
	rv := reflect.ValueOf(value)
	for rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return truncateReason(fallback)
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return truncateReason(fallback)
	}
	if structured := firstErrorMessage(structField(rv, "errors")); structured != "" {
		return structured
	}
	if structured := firstErrorMessage(structField(rv, "detail")); structured != "" {
		return structured
	}
	for _, name := range []string{"error_message", "last_reason", "message"} {
		if msg := strings.TrimSpace(valueToString(structField(rv, name))); msg != "" {
			return truncateReason(msg)
		}
	}
	code := strings.TrimSpace(valueToString(structField(rv, "error_code")))
	if code != "" {
		return truncateReason(code)
	}
	return truncateReason(fallback)
}

// structField 按 JSON 键或字段名（忽略大小写）读取 struct 字段。
func structField(rv reflect.Value, name string) any {
	if !rv.IsValid() {
		return nil
	}
	rt := rv.Type()
	if f, ok := rt.FieldByNameFunc(func(fn string) bool {
		// Python snake_case 属性名 → Go 字段名去下划线后忽略大小写比对
		//（error_message ↔ ErrorMessage）。
		return strings.EqualFold(strings.ReplaceAll(fn, "_", ""), strings.ReplaceAll(name, "_", ""))
	}); ok {
		return rv.Field(f.Index[0]).Interface()
	}
	return nil
}

// firstErrorMessage 对齐 Python _first_error_message：Provider 错误列表首条可行动消息。
func firstErrorMessage(errors any) string {
	if errors == nil {
		return ""
	}
	items, ok := toSlice(errors)
	if !ok {
		return ""
	}
	for _, item := range items {
		var message string
		switch it := item.(type) {
		case map[string]any:
			message = strings.TrimSpace(valueToString(orAny(it["message"], it["reason"])))
		case string:
			message = strings.TrimSpace(it)
		default:
			message = strings.TrimSpace(valueToString(item))
		}
		if message != "" {
			return truncateReason(message)
		}
	}
	return ""
}

// orAny 返回第一个非 nil 值（对齐 Python `a or b` 语义的常见形态）。
func orAny(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

// toSlice 把 list/tuple 形态归一为 []any（Python isinstance((list, tuple))）。
func toSlice(v any) ([]any, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]any, rv.Len())
		for i := range rv.Len() {
			out[i] = rv.Index(i).Interface()
		}
		return out, true
	default:
		return nil, false
	}
}

// valueToString 对齐 Python str(x or "") 的宽松取值。
func valueToString(v any) string {
	if v == nil {
		return ""
	}
	switch s := v.(type) {
	case string:
		return s
	case error:
		return s.Error()
	case fmt.Stringer:
		return s.String()
	default:
		return fmt.Sprintf("%v", s)
	}
}

// truncateReason 对齐 Python [:_FAILURE_REASON_LIMIT]。
func truncateReason(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > failureReasonLimit {
		return string(r[:failureReasonLimit])
	}
	return string(r)
}
