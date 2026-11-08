package runtime

import (
	"errors"
	"strings"
)

// 本文件对应上游两份字节相同的副本，合并为一份（spec DEC-13，登记为偏差 D-1）：
//   - gateway/runtime/session_input.py
//   - jiuwenswarm/runtime/session_input.py

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
	// sessionInputRejectedCode 输入被拒的错误码
	// Python: SessionInputRejectedError.code
	sessionInputRejectedCode = "SESSION_INPUT_REJECTED"
	// sessionInputTargetChangedCode 目标执行已变的错误码
	// Python: SessionInputTargetError.code
	sessionInputTargetChangedCode = "SESSION_INPUT_TARGET_CHANGED"
	// sessionInputQueueRequiredCode 需先排队的错误码
	// Python: SessionInputQueueRequiredError.code
	sessionInputQueueRequiredCode = "SESSION_INPUT_QUEUE_REQUIRED"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// steerImageFields steer 图片引用允许的字段集合（5 项）。
	// Python: session_input.py _STEER_IMAGE_FIELDS
	steerImageFields = map[string]struct{}{
		"type": {}, "path": {}, "filename": {}, "mime_type": {}, "size_bytes": {},
	}
	// steerImageMIMETypes steer 图片允许的 MIME（4 项）。
	// Python: session_input.py _STEER_IMAGE_MIME_TYPES
	steerImageMIMETypes = map[string]struct{}{
		"image/png": {}, "image/jpeg": {}, "image/webp": {}, "image/gif": {},
	}
	// steerForbiddenAttachmentKeys steer 模式下不允许出现的附件字段。
	// Python: validate_steer_image_references 内的循环常量
	steerForbiddenAttachmentKeys = []string{
		"images", "image_files", "files", "attachments", "audio_files", "video_files",
	}
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
func (e *SessionInputRejectedError) Code() string { return sessionInputRejectedCode }

// Error 实现 error 接口。
func (e *SessionInputTargetError) Error() string { return e.Message }

// Code 稳定错误码。
func (e *SessionInputTargetError) Code() string { return sessionInputTargetChangedCode }

// Unwrap 让 errors.As 能命中 SessionInputRejectedError（对齐上游继承）。
func (e *SessionInputTargetError) Unwrap() error {
	return &SessionInputRejectedError{Message: e.Message}
}

// Error 实现 error 接口。
func (e *SessionInputQueueRequiredError) Error() string { return e.Message }

// Code 稳定错误码。
func (e *SessionInputQueueRequiredError) Code() string { return sessionInputQueueRequiredCode }

// Unwrap 同 TargetError。
func (e *SessionInputQueueRequiredError) Unwrap() error {
	return &SessionInputRejectedError{Message: e.Message}
}

// ResolveSessionInputMode 归一化 input_mode 与其遗留别名 runtime_mode。
// Python: session_input.py resolve_session_input_mode
//
// 未知值保留普通发送行为（返回 nil）；交互应答在此之前的别处单独分类。
// 主字段与别名冲突时上游抛 ValueError，此版本吞掉错误返回 nil；
// 需要错误时用 ResolveSessionInputModeChecked。
func ResolveSessionInputMode(params map[string]any) *SessionInputMode {
	mode, _ := ResolveSessionInputModeChecked(params)
	return mode
}

// ResolveSessionInputModeChecked 同 ResolveSessionInputMode，但冲突时报错。
// Python: session_input.py resolve_session_input_mode 的 raise ValueError 分支
func ResolveSessionInputModeChecked(params map[string]any) (*SessionInputMode, error) {
	if params == nil {
		return nil, nil
	}
	primary := params["input_mode"]
	alias := params["runtime_mode"]
	// 上游用真值判断：primary and alias and normalize(primary) != normalize(alias)
	// 空串为假，因此空的主字段不会触发冲突，而是回落到别名。
	if isTruthySessionInputValue(primary) && isTruthySessionInputValue(alias) &&
		normalizeSessionInputRaw(primary) != normalizeSessionInputRaw(alias) {
		return nil, errors.New("input_mode and runtime_mode must agree")
	}
	rawValue := primary
	if !isTruthySessionInputValue(rawValue) {
		rawValue = alias
	}
	if mode, ok := rawValue.(SessionInputMode); ok {
		return &mode, nil
	}
	switch normalizeSessionInputRaw(rawValue) {
	case string(SessionInputModeSteer):
		mode := SessionInputModeSteer
		return &mode, nil
	case string(SessionInputModeFollowUp):
		mode := SessionInputModeFollowUp
		return &mode, nil
	default:
		return nil, nil
	}
}

// ValidateSteerImageReferences 校验 steer 的"仅引用"线格式，不碰文件系统。
// Python: session_input.py validate_steer_image_references
//
// 六类拒收，顺序与上游一致：
//  1. 六个附件字段任一非空
//  2. media_items 不是 list
//  3. 条目不是 mapping，或含 _STEER_IMAGE_FIELDS 之外的键
//  4. type != "image"
//  5. path / filename / mime_type 任一非非空字符串
//  6. mime_type 不在允许集合内
func ValidateSteerImageReferences(params map[string]any) ([]map[string]any, error) {
	for _, key := range steerForbiddenAttachmentKeys {
		if value, ok := params[key]; ok && isTruthySessionInputValue(value) {
			return nil, errors.New(
				"steering accepts uploaded image references only; send other attachments as a queued task")
		}
	}
	rawItems, ok := params["media_items"]
	if !ok || rawItems == nil {
		return nil, nil
	}
	items, ok := rawItems.([]any)
	if !ok {
		return nil, errors.New("steer media_items must be a list of uploaded image references")
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		entry, ok := item.(map[string]any)
		if !ok {
			return nil, errors.New("steer images require uploaded file references without inline data")
		}
		for key := range entry {
			if _, allowed := steerImageFields[key]; !allowed {
				return nil, errors.New("steer images require uploaded file references without inline data")
			}
		}
		if entry["type"] != "image" {
			return nil, errors.New("steering accepts uploaded images only")
		}
		for _, key := range []string{"path", "filename", "mime_type"} {
			text, isString := entry[key].(string)
			if !isString || strings.TrimSpace(text) == "" {
				return nil, errors.New("steer image " + key + " must be a non-empty string")
			}
		}
		mimeType, _ := entry["mime_type"].(string)
		if _, allowed := steerImageMIMETypes[mimeType]; !allowed {
			return nil, errors.New("unsupported steer image MIME type")
		}
		out = append(out, entry)
	}
	return out, nil
}

// ValidateSessionInput 校验会话补充输入的整体形状。
// Python: session_input.py validate_session_input
//
// 步骤与上游一致：
//  1. 解析 mode
//  2. 有 expected_execution_id 时：必须非空字符串，且必须 steer 模式
//  3. steer 模式下校验图片引用，得到 image_references
//  4. 有图片引用且（无 query 或 query 是字符串）→ 通过
//  5. 否则 query 必须是非空字符串
func ValidateSessionInput(params map[string]any) error {
	mode, modeErr := ResolveSessionInputModeChecked(params)
	if modeErr != nil {
		return modeErr
	}
	if target, ok := params["expected_execution_id"]; ok {
		targetText, isString := target.(string)
		if !isString || strings.TrimSpace(targetText) == "" {
			return errors.New("expected_execution_id must be a non-empty string")
		}
		if mode == nil || *mode != SessionInputModeSteer {
			return errors.New("expected_execution_id requires steer mode")
		}
	}
	var imageReferences []map[string]any
	if mode != nil && *mode == SessionInputModeSteer {
		refs, refErr := ValidateSteerImageReferences(params)
		if refErr != nil {
			return refErr
		}
		imageReferences = refs
	}
	query := params["query"]
	if len(imageReferences) > 0 {
		if query == nil {
			return nil
		}
		if _, isString := query.(string); isString {
			return nil
		}
	}
	queryText, isString := query.(string)
	if !isString || strings.TrimSpace(queryText) == "" {
		return errors.New("session input requires non-empty text")
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// normalizeSessionInputRaw 归一化输入意图取值。
// Python: session_input.py normalize（内部闭包）
//
//	raw.value if isinstance(raw, SessionInputMode) else str(raw).strip().lower()
func normalizeSessionInputRaw(value any) string {
	if mode, ok := value.(SessionInputMode); ok {
		return string(mode)
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(text))
}

// isTruthySessionInputValue 复刻 Python 真值判断。
// None / False / 0 / "" / [] / {} 均为假；其余为真。
func isTruthySessionInputValue(value any) bool {
	switch typed := value.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case []any:
		return len(typed) > 0
	case map[string]any:
		return len(typed) > 0
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0
	default:
		return true
	}
}
