package result

// ──────────────────────────── 结构体 ────────────────────────────

// BaseResult 操作结果基类，所有子操作（fs/shell/code）的返回值都组合此类型。
// 对齐 Python BaseResult：code=0 表示成功，非 0 表示失败。
type BaseResult struct {
	// Code 状态码：0 = 成功，非 0 = 失败
	Code int `json:"code"`
	// Message 状态消息
	Message string `json:"message"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildOperationErrorResult 构造标准化错误结果。
// 对齐 Python build_operation_error_result，简化版（Go 不使用泛型 Result 类）。
func BuildOperationErrorResult(errorCode int, errMsg string) BaseResult {
	return BaseResult{Code: errorCode, Message: errMsg}
}

// IsSuccess 判断结果是否成功
func (r BaseResult) IsSuccess() bool {
	return r.Code == 0
}
