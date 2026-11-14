// 模型选择错误码与错误类型，runtime 与 RPC 层共用。
// 对应 Python 代码：jiuwenswarm/common/model_errors.py:1-30（完整版，12 个错误码；
// gateway 裁剪副本仅保留 5 个，按裁决 D 以完整版为准）。
package common

// ──────────────────────────── 结构体 ────────────────────────────

// ModelSelectionError 模型目录/选择失败。
// Python: model_errors.py:22 ModelSelectionError(RuntimeError)。
type ModelSelectionError struct {
	// Code 错误码（上方 12 个常量之一）
	Code string
	// Message 错误描述
	Message string
	// Details 附加细节字段（Python **details）
	Details map[string]any
}

// Error 实现 error 接口，返回 Message（对齐 Python super().__init__(message)）。
func (e *ModelSelectionError) Error() string {
	return e.Message
}

// ToPayload 返回 RPC 响应载荷。
// Python: model_errors.py:28 to_payload —— {"code", "message", **details}。
func (e *ModelSelectionError) ToPayload() map[string]any {
	payload := map[string]any{
		"code":    e.Code,
		"message": e.Error(),
	}
	for k, v := range e.Details {
		payload[k] = v
	}
	return payload
}

// ──────────────────────────── 常量 ────────────────────────────

// 十二个错误码常量。Python: model_errors.py:8-19，字符串值逐字照抄（协议契约）。
const (
	ModelSelectionNotFound     = "MODEL_SELECTION_NOT_FOUND"
	ModelSelectionDisabled     = "MODEL_SELECTION_DISABLED"
	ModelSelectionForbidden    = "MODEL_SELECTION_FORBIDDEN"
	ModelGroupInvalid          = "MODEL_GROUP_INVALID"
	ModelGroupNoAvailableRoute = "MODEL_GROUP_NO_AVAILABLE_ROUTE"
	ModelRequestConfigInvalid  = "MODEL_REQUEST_CONFIG_INVALID"
	ModelRouteFailed           = "MODEL_ROUTE_FAILED"
	ModelStreamInterrupted     = "MODEL_STREAM_INTERRUPTED"
	ModelOutcomeUnknown        = "MODEL_OUTCOME_UNKNOWN"
	ModelSelectionReferenced   = "MODEL_SELECTION_REFERENCED"
	ModelRuntimeUnavailable    = "MODEL_RUNTIME_UNAVAILABLE"
	TeamModelSelectionStale    = "TEAM_MODEL_SELECTION_STALE"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewModelSelectionError 构造模型选择错误。
// Python: model_errors.py:23 __init__(code, message, **details)。
func NewModelSelectionError(code string, message string, details map[string]any) *ModelSelectionError {
	return &ModelSelectionError{Code: code, Message: message, Details: details}
}
