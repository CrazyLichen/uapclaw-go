package session

// 本文件承载会话补充输入的三个拒收异常，以及输入意图枚举。
//
// 为何在 session 包而非 runtime 包：Python 的 session_input.py 位于 runtime/ 下，
// 与 session/ 同级、被 session/coordinator.py 单向 import；Go 的包即编译边界，
// session 包零内部依赖、被 runtime 包依赖，异常必须定义在被依赖侧，否则成环。
// 对应 Python：jiuwenswarm/runtime/session_input.py（98 行）
// spec 登记：异常定义位置下移（偏差 D-14）。

// ──────────────────────────── 枚举 ────────────────────────────

// SessionInputMode 会话补充输入的意图。
// Python: session_input.py SessionInputMode(str, Enum)
type SessionInputMode string

const (
	// SessionInputModeSteer 转向：注入正在跑的轮次
	SessionInputModeSteer SessionInputMode = "steer"
	// SessionInputModeFollowUp 追加：排在当前轮之后
	SessionInputModeFollowUp SessionInputMode = "follow_up"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SessionInputRejectedCode 输入被拒的错误码
	// Python: SessionInputRejectedError.code
	SessionInputRejectedCode = "SESSION_INPUT_REJECTED"
	// SessionInputTargetChangedCode 目标执行已变的错误码
	// Python: SessionInputTargetError.code
	SessionInputTargetChangedCode = "SESSION_INPUT_TARGET_CHANGED"
	// SessionInputQueueRequiredCode 需先排队的错误码
	// Python: SessionInputQueueRequiredError.code
	SessionInputQueueRequiredCode = "SESSION_INPUT_QUEUE_REQUIRED"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionInputRejectedError 已知拒收：输入未到达 SDK 队列。
// Python: session_input.py SessionInputRejectedError(RuntimeError)
type SessionInputRejectedError struct {
	// Message 错误说明
	Message string
}

// SessionInputTargetError 补充输入不得变成另一个执行的工作。
// Python: session_input.py SessionInputTargetError(SessionInputRejectedError)
type SessionInputTargetError struct {
	// Message 错误说明
	Message string
}

// SessionInputQueueRequiredError 跨会话输入须等普通任务准入。
// Python: session_input.py SessionInputQueueRequiredError(SessionInputRejectedError)
type SessionInputQueueRequiredError struct {
	// Message 错误说明
	Message string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *SessionInputRejectedError) Error() string { return e.Message }

// Code 稳定错误码。
func (e *SessionInputRejectedError) Code() string { return SessionInputRejectedCode }

// Error 实现 error 接口。
func (e *SessionInputTargetError) Error() string { return e.Message }

// Code 稳定错误码。
func (e *SessionInputTargetError) Code() string { return SessionInputTargetChangedCode }

// Unwrap 让 errors.As 能命中 SessionInputRejectedError（对齐上游继承）。
func (e *SessionInputTargetError) Unwrap() error {
	return &SessionInputRejectedError{Message: e.Message}
}

// Error 实现 error 接口。
func (e *SessionInputQueueRequiredError) Error() string { return e.Message }

// Code 稳定错误码。
func (e *SessionInputQueueRequiredError) Code() string { return SessionInputQueueRequiredCode }

// Unwrap 同 SessionInputTargetError。
func (e *SessionInputQueueRequiredError) Unwrap() error {
	return &SessionInputRejectedError{Message: e.Message}
}
