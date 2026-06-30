package exception

import (
	"encoding/json"
	"fmt"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BaseError 统一异常基类，实现 error 接口。
//
// 核心设计点：
//   - StatusCode 是主要语义标识符
//   - ErrorCategory 表达控制/恢复语义
//   - 消息渲染基于模板，容忍缺失占位符
//
// 对应 Python: openjiuwen/core/common/exception/errors.py (BaseError)
type BaseError struct {
	// status 关联的 StatusCode，通过它访问 Code()/Name()/Message()
	status StatusCode
	// category 错误类别（控制流语义）
	category ErrorCategory
	// templateMessage 渲染后的模板消息
	templateMessage string
	// message 最终消息（可覆盖 templateMessage）
	message string
	// params 模板参数 + 额外结构化日志字段
	params map[string]any
	// details 附加详情（如 ToolError 的 card）
	details any
	// cause 原始错误
	cause error
}

// baseErrorBuilder 内部构造器，收集可选参数。
type baseErrorBuilder struct {
	msg     string
	details any
	cause   error
	params  map[string]any
}

// ErrorOption BaseError 构造选项函数。
type ErrorOption func(*baseErrorBuilder)

// ──────────────────────────── 枚举 ────────────────────────────

// ErrorCategory 错误类别枚举，表达控制流语义。
//
// 选择 ErrorCategory = 选择控制语义（retry? abort? terminate gracefully?）。
// 选 StatusCode = 选错误身份（哪个模块、哪种失败）。两者正交。
//
// 对应 Python: BaseError 子类层级（FrameworkError/ValidationError/ExecutionError/Termination）
type ErrorCategory int

const (
	// ErrorCategoryFramework 基础设施/依赖故障，必须终止（fatal=True, recoverable=False）
	ErrorCategoryFramework ErrorCategory = iota
	// ErrorCategoryValidation 输入/约束错误，重试无意义（fatal=False, recoverable=False）
	ErrorCategoryValidation
	// ErrorCategoryExecution 执行期错误，可重试/重规划（fatal=False, recoverable=True）
	ErrorCategoryExecution
	// ErrorCategoryTermination 正常控制流终止，非错误（fatal=False, recoverable=False）
	ErrorCategoryTermination
)

// String 实现 fmt.Stringer 接口，返回 ErrorCategory 的字符串表示。
func (c ErrorCategory) String() string {
	if int(c) >= 0 && int(c) < len(errorCategoryStrings) {
		return errorCategoryStrings[c]
	}
	return fmt.Sprintf("ErrorCategory(%d)", int(c))
}

// MarshalJSON 实现 json.Marshaler 接口。
func (c ErrorCategory) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.String())
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// errorCategoryStrings ErrorCategory 枚举值对应的字符串表示，与 Python 子类名保持一致。
var errorCategoryStrings = [...]string{
	"FrameworkError",
	"ValidationError",
	"ExecutionError",
	"Termination",
}

// errorCategoryAttrs ErrorCategory 对应的 fatal/recoverable 属性。
var errorCategoryAttrs = [...]struct {
	Fatal       bool
	Recoverable bool
}{
	{Fatal: true, Recoverable: false},  // 框架
	{Fatal: false, Recoverable: false}, // 校验
	{Fatal: false, Recoverable: true},  // 执行
	{Fatal: false, Recoverable: false}, // 终止
}

// ──────────────────────────── 导出函数 ────────────────────────────

// WithMsg 设置自定义消息（覆盖模板渲染结果）。
func WithMsg(msg string) ErrorOption {
	return func(b *baseErrorBuilder) { b.msg = msg }
}

// WithDetails 设置附加详情。
func WithDetails(details any) ErrorOption {
	return func(b *baseErrorBuilder) { b.details = details }
}

// WithCause 设置原始错误。
func WithCause(cause error) ErrorOption {
	return func(b *baseErrorBuilder) { b.cause = cause }
}

// WithParam 设置单个模板参数。
func WithParam(key string, value any) ErrorOption {
	return func(b *baseErrorBuilder) {
		if b.params == nil {
			b.params = make(map[string]any)
		}
		b.params[key] = value
	}
}

// WithParams 设置多个模板参数。
func WithParams(params map[string]any) ErrorOption {
	return func(b *baseErrorBuilder) {
		if b.params == nil {
			b.params = make(map[string]any)
		}
		for k, v := range params {
			b.params[k] = v
		}
	}
}

// NewBaseError 创建 BaseError 实例。
//
// 参数：
//   - status: 关联的 StatusCode
//   - opts: 可选配置（WithMsg, WithDetails, WithCause, WithParam, WithParams）
//
// 对应 Python: BaseError.__init__
func NewBaseError(status StatusCode, opts ...ErrorOption) *BaseError {
	// 获取该 StatusCode 对应的 ErrorCategory
	category := ResolveCategory(status)

	// 应用选项
	builder := &baseErrorBuilder{}
	for _, opt := range opts {
		opt(builder)
	}

	// 渲染模板消息
	templateMsg := status.RenderMessage(builder.params)

	// 确定最终消息：如果指定了自定义消息，使用自定义消息；否则使用渲染后的模板消息
	message := templateMsg
	if builder.msg != "" {
		message = builder.msg
	}

	return &BaseError{
		status:          status,
		category:        category,
		templateMessage: templateMsg,
		message:         message,
		params:          builder.params,
		details:         builder.details,
		cause:           builder.cause,
	}
}

// Status 返回关联的 StatusCode。
func (e *BaseError) Status() StatusCode { return e.status }

// Category 返回错误类别。
func (e *BaseError) Category() ErrorCategory { return e.category }

// SetCategory 设置错误类别。
// 通常由 ResolveCategory 自动决定，此方法用于需要覆盖自动解析结果的场景
// （如 HTTP 5xx 错误需要强制为 Execution 类别以确保可重试）。
func (e *BaseError) SetCategory(c ErrorCategory) { e.category = c }

// Code 返回整数错误码，委托给 StatusCode.Code()。
//
// 对应 Python: BaseError.code
func (e *BaseError) Code() int { return e.status.Code() }

// Message 返回最终消息。
func (e *BaseError) Message() string { return e.message }

// TemplateMessage 返回渲染后的模板消息（可能被 msg 覆盖）。
func (e *BaseError) TemplateMessage() string { return e.templateMessage }

// Params 返回模板参数。
func (e *BaseError) Params() map[string]any { return e.params }

// Details 返回附加详情。
func (e *BaseError) Details() any { return e.details }

// Cause 返回原始错误。
func (e *BaseError) Cause() error { return e.cause }

// Unwrap 实现 errors.Unwrap 接口，支持 errors.Is/As 链式查找。
func (e *BaseError) Unwrap() error { return e.cause }

// IsFatal 是否为致命错误（必须终止）。
func (e *BaseError) IsFatal() bool {
	idx := int(e.category)
	if idx >= 0 && idx < len(errorCategoryAttrs) {
		return errorCategoryAttrs[idx].Fatal
	}
	return false
}

// IsRecoverable 是否可恢复（可重试/重规划）。
func (e *BaseError) IsRecoverable() bool {
	idx := int(e.category)
	if idx >= 0 && idx < len(errorCategoryAttrs) {
		return errorCategoryAttrs[idx].Recoverable
	}
	return false
}

// Error 实现 error 接口，返回 "[code] message" 格式。
//
// 对应 Python: BaseError.__str__
func (e *BaseError) Error() string {
	return fmt.Sprintf("[%d] %s", e.Code(), e.message)
}

// String 实现 fmt.Stringer 接口。
func (e *BaseError) String() string {
	return e.Error()
}

// ToDict 返回标准结构化输出，用于 API/RPC/日志。
//
// 对应 Python: BaseError.to_dict()
func (e *BaseError) ToDict() map[string]any {
	return map[string]any{
		"code":        e.Code(),
		"status":      e.status.Name(),
		"message":     e.templateMessage,
		"params":      e.params,
		"raw_message": e.message,
		"details":     e.details,
		"category":    e.category.String(),
		"fatal":       e.IsFatal(),
		"recoverable": e.IsRecoverable(),
	}
}

// ToJSON 返回 JSON 格式的结构化输出。
//
// 对应 Python: BaseError.to_json()
func (e *BaseError) ToJSON() string {
	data, _ := json.Marshal(e.ToDict())
	return string(data)
}

// BuildError 构建异常实例但不抛出，用于延迟抛出或包装到 Result 中。
//
// 对应 Python: build_error()
func BuildError(status StatusCode, opts ...ErrorOption) *BaseError {
	return NewBaseError(status, opts...)
}

// RaiseError 返回异常实例，供调用方 return 或 panic。
//
// Go 没有 raise 语义，此函数返回 *BaseError 供调用方直接 return。
//
// 对应 Python: raise_error()
func RaiseError(status StatusCode, opts ...ErrorOption) *BaseError {
	return NewBaseError(status, opts...)
}

// SystemError 返回 Framework 类别的异常。
//
// 对应 Python: system_error()
func SystemError(status StatusCode, opts ...ErrorOption) *BaseError {
	err := NewBaseError(status, opts...)
	err.category = ErrorCategoryFramework
	return err
}

// ValidateError 返回 Validation 类别的异常。
//
// 对应 Python: validate_error()
func ValidateError(status StatusCode, opts ...ErrorOption) *BaseError {
	err := NewBaseError(status, opts...)
	err.category = ErrorCategoryValidation
	return err
}

// Terminate 返回 Termination 类别的异常。
//
// 对应 Python: terminate()
func Terminate(status StatusCode, opts ...ErrorOption) *BaseError {
	err := NewBaseError(status, opts...)
	err.category = ErrorCategoryTermination
	return err
}
