package team

// ──────────────────────────── 结构体 ────────────────────────────

// TeamError Team 基础错误。
// 对齐 Python: TeamError (exceptions.py L11-13)
type TeamError struct {
	// message 错误描述
	message string
}

// TeamCreateError Team 创建失败。
// 对齐 Python: TeamCreateError (L16-18)
type TeamCreateError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// TeamRecoverError Team 恢复失败。
// 对齐 Python: TeamRecoverError (L21-23)
type TeamRecoverError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// TeamInteractError Team 交互失败。
// 对齐 Python: TeamInteractError (L26-28)
type TeamInteractError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// TeamConfigError Team 配置错误。
// 对齐 Python: TeamConfigError (L31-33)
type TeamConfigError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// TeamMonitorError Team Monitor 错误。
// 对齐 Python: TeamMonitorError (L36-38)
type TeamMonitorError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// TeamSessionError Team 会话错误。
// 对齐 Python: TeamSessionError (L41-43)
type TeamSessionError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// TeamStorageError Team 存储错误。
// 对齐 Python: TeamStorageError (L46-48)
type TeamStorageError struct {
	// TeamError 嵌入基础错误
	TeamError
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Error 实现 error 接口。
func (e *TeamError) Error() string { return "TeamError: " + e.message }

// NewTeamError 创建 Team 基础错误。
func NewTeamError(message string) *TeamError { return &TeamError{message: message} }

// Error 覆写错误前缀。
func (e *TeamCreateError) Error() string { return "TeamCreateError: " + e.message }

// Unwrap 解包为 TeamError 基类（errors.As 匹配，等价 Python isinstance）。
func (e *TeamCreateError) Unwrap() error { return &e.TeamError }

// NewTeamCreateError 创建 Team 创建失败错误。
func NewTeamCreateError(message string) *TeamCreateError {
	return &TeamCreateError{TeamError{message: message}}
}

// Error 覆写错误前缀。
func (e *TeamRecoverError) Error() string { return "TeamRecoverError: " + e.message }

// Unwrap 解包为 TeamError 基类（errors.As 匹配，等价 Python isinstance）。
func (e *TeamRecoverError) Unwrap() error { return &e.TeamError }

// NewTeamRecoverError 创建 Team 恢复失败错误。
func NewTeamRecoverError(message string) *TeamRecoverError {
	return &TeamRecoverError{TeamError{message: message}}
}

// Error 覆写错误前缀。
func (e *TeamInteractError) Error() string { return "TeamInteractError: " + e.message }

// Unwrap 解包为 TeamError 基类（errors.As 匹配，等价 Python isinstance）。
func (e *TeamInteractError) Unwrap() error { return &e.TeamError }

// NewTeamInteractError 创建 Team 交互失败错误。
func NewTeamInteractError(message string) *TeamInteractError {
	return &TeamInteractError{TeamError{message: message}}
}

// Error 覆写错误前缀。
func (e *TeamConfigError) Error() string { return "TeamConfigError: " + e.message }

// Unwrap 解包为 TeamError 基类（errors.As 匹配，等价 Python isinstance）。
func (e *TeamConfigError) Unwrap() error { return &e.TeamError }

// NewTeamConfigError 创建 Team 配置错误。
func NewTeamConfigError(message string) *TeamConfigError {
	return &TeamConfigError{TeamError{message: message}}
}

// Error 覆写错误前缀。
func (e *TeamMonitorError) Error() string { return "TeamMonitorError: " + e.message }

// Unwrap 解包为 TeamError 基类（errors.As 匹配，等价 Python isinstance）。
func (e *TeamMonitorError) Unwrap() error { return &e.TeamError }

// NewTeamMonitorError 创建 Team Monitor 错误。
func NewTeamMonitorError(message string) *TeamMonitorError {
	return &TeamMonitorError{TeamError{message: message}}
}

// Error 覆写错误前缀。
func (e *TeamSessionError) Error() string { return "TeamSessionError: " + e.message }

// Unwrap 解包为 TeamError 基类（errors.As 匹配，等价 Python isinstance）。
func (e *TeamSessionError) Unwrap() error { return &e.TeamError }

// NewTeamSessionError 创建 Team 会话错误。
func NewTeamSessionError(message string) *TeamSessionError {
	return &TeamSessionError{TeamError{message: message}}
}

// Error 覆写错误前缀。
func (e *TeamStorageError) Error() string { return "TeamStorageError: " + e.message }

// Unwrap 解包为 TeamError 基类（errors.As 匹配，等价 Python isinstance）。
func (e *TeamStorageError) Unwrap() error { return &e.TeamError }

// NewTeamStorageError 创建 Team 存储错误。
func NewTeamStorageError(message string) *TeamStorageError {
	return &TeamStorageError{TeamError{message: message}}
}
