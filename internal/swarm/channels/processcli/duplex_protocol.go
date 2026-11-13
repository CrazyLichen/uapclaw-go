package processcli

import (
	"math"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DuplexControl 一次双工命令内相关联的 answer/cancel/tool_result 控制记录。
// Python: duplex_protocol.py:110-127 DuplexControl（frozen dataclass → 构造期校验的不可变 Go 结构体）
//
// 信封字段构造期冻结；answers 是防御性拷贝；ToDict 返回全新 JSON 数据，
// 调用方的修改不会穿透到先前解码出的对象或其序列化输出。
type DuplexControl struct {
	// Kind 控制类型：answer / cancel / tool_result
	Kind string
	// RequestID 关联请求标识（必填，已 strip）
	RequestID string
	// SessionID 会话标识（answer/tool_result 必填；cancel 可空）
	SessionID *string
	// InteractionID 交互标识（仅 answer 必填）
	InteractionID *string
	// Answers 交互答案（仅 answer，防御性拷贝）
	Answers []map[string]any
	// CallID 宿主工具调用标识（仅 tool_result 必填）
	CallID *string
	// Result 工具结果（仅 tool_result，与 Error 恰好其一）
	Result any
	// Error 工具错误（仅 tool_result）
	Error *string
}

// DuplexProtocolError 被拒控制帧的安全诊断（可带可用的 request 关联）。
// Python: duplex_protocol.py:32-47 DuplexProtocolError（code = "INVALID_CONTROL"）
type DuplexProtocolError struct {
	// Message 安全诊断消息
	Message string
	// RequestID 从损坏帧中捞出的关联标识（可选）
	RequestID *string
	// Details 字段级定位（来自 schema 诊断）
	Details map[string]any
}

// Error 实现 error 接口。
func (e *DuplexProtocolError) Error() string { return e.Message }

// Code 错误码（常量方法，对齐 Python 类属性）。
// Python: duplex_protocol.py:35 code = "INVALID_CONTROL"
func (e *DuplexProtocolError) Code() string { return "INVALID_CONTROL" }

// ──────────────────────────── 常量 ────────────────────────────

// 三类控制帧的字段白名单与必填集合。
// Python: duplex_protocol.py:26-29
var (
	cancelFields       = []string{"schema_version", "type", "request_id", "session_id"}
	answerFields       = []string{"schema_version", "type", "request_id", "session_id", "interaction_id", "answers"}
	toolResultFields   = []string{"schema_version", "type", "request_id", "session_id", "call_id", "result", "error"}
	commonRequired     = []string{"schema_version", "type", "request_id"}
	toolResultRequired = []string{"schema_version", "type", "request_id", "session_id", "call_id"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDuplexControl 构造并按 kind 交叉校验（对齐 Python __post_init__）。
// Python: duplex_protocol.py:128-167 __post_init__
func NewDuplexControl(
	kind, requestID string,
	sessionID, interactionID *string,
	answers []map[string]any,
	callID *string,
	result any,
	err *string,
) (*DuplexControl, error) {
	// Python: :129-132 kind 白名单
	if kind != "answer" && kind != "cancel" && kind != "tool_result" {
		return nil, &DuplexProtocolError{Message: "control type must be answer, tool_result or cancel"}
	}
	// Python: :133-135 request_id 必填
	rid, err2 := requiredText("request_id", requestID)
	if err2 != nil {
		return nil, err2
	}
	ctrl := &DuplexControl{Kind: kind, RequestID: rid}
	switch kind {
	case "answer":
		// Python: :136-145 session_id/interaction_id 必填 + answers 防御拷贝
		sid, sidErr := requiredText("session_id", derefText(sessionID))
		if sidErr != nil {
			return nil, sidErr
		}
		ctrl.SessionID = &sid
		iid, iidErr := requiredText("interaction_id", derefText(interactionID))
		if iidErr != nil {
			return nil, iidErr
		}
		ctrl.InteractionID = &iid
		copied, copyErr := copyAnswers(answers)
		if copyErr != nil {
			return nil, copyErr
		}
		ctrl.Answers = copied
	case "tool_result":
		// Python: :146-156 session_id/call_id 必填；不得携带 answer 字段；
		// error 与 result 恰好其一
		sid, sidErr := requiredText("session_id", derefText(sessionID))
		if sidErr != nil {
			return nil, sidErr
		}
		ctrl.SessionID = &sid
		cid, cidErr := requiredText("call_id", derefText(callID))
		if cidErr != nil {
			return nil, cidErr
		}
		ctrl.CallID = &cid
		if interactionID != nil || len(answers) != 0 {
			return nil, &DuplexProtocolError{Message: "tool_result must not contain answer fields"}
		}
		if err != nil {
			e, eErr := requiredText("error", *err)
			if eErr != nil {
				return nil, eErr
			}
			ctrl.Error = &e
		} else {
			copied, copyErr := copyJSON(result)
			if copyErr != nil {
				return nil, copyErr
			}
			ctrl.Result = copied
		}
	default:
		// Python: :157-167 cancel：不得携带 answer 字段或 tool 字段
		if sessionID != nil {
			sid, sidErr := requiredText("session_id", *sessionID)
			if sidErr != nil {
				return nil, sidErr
			}
			ctrl.SessionID = &sid
		}
		hasAnswerFields := interactionID != nil || len(answers) != 0
		hasToolFields := callID != nil || err != nil
		if hasAnswerFields || hasToolFields {
			return nil, &DuplexProtocolError{Message: "cancel must not contain answer fields or tool fields"}
		}
	}
	return ctrl, nil
}

// ToDict 返回版本化的全新 JSON 兼容数据。
// Python: duplex_protocol.py:169-187 to_dict
func (c *DuplexControl) ToDict() map[string]any {
	record := map[string]any{
		"schema_version": protocol.CurrentSchemaVersion,
		"type":           c.Kind,
		"request_id":     c.RequestID,
	}
	if c.SessionID != nil {
		record["session_id"] = *c.SessionID
	} else {
		record["session_id"] = nil
	}
	switch c.Kind {
	case "answer":
		// Python: :178-180
		record["interaction_id"] = *c.InteractionID
		fresh := make([]any, 0, len(c.Answers))
		for _, answer := range c.Answers {
			copied, _ := copyJSON(answer)
			fresh = append(fresh, copied)
		}
		record["answers"] = fresh
	case "tool_result":
		// Python: :181-186
		record["call_id"] = *c.CallID
		if c.Error == nil {
			copied, _ := copyJSON(c.Result)
			record["result"] = copied
		} else {
			record["error"] = *c.Error
		}
	}
	return record
}

// DecodeControl 解码一条严格控制帧；绝不接受第二条 run 记录。
// Python: duplex_protocol.py:218-257 decode_control
func DecodeControl(line []byte) (*DuplexControl, error) {
	// Python: :221-226 UTF-8 bytes → document（Go string 直接持有字节，
	// 无效字节由下方 decodeStrict 的 JSON 校验拒绝）
	document := string(line)
	// Python: :227-231 decode_machine_document；MachineInputError →
	// 消息首个 "run input" 替换为 "control input"，request_id 透传
	record, err := DecodeMachineDocument(document)
	if err != nil {
		mie := asInputError(err)
		msg := strings.Replace(mie.Message, "run input", "control input", 1)
		return nil, &DuplexProtocolError{Message: msg, RequestID: mie.RequestID}
	}
	// Python: :232-235 关联 request_id 捞出（strip 后非空才保留）
	correlation := requestIDOf(record)
	// Python: :236-257 白名单/必填/版本校验 + 构造；失败统一包 details 与关联
	ctrl, ctlErr := validateControlAndBuild(record)
	if ctlErr != nil {
		kind, _ := record["type"].(string)
		definition := kind
		if kind != "answer" && kind != "cancel" && kind != "tool_result" {
			definition = "control"
		}
		return nil, &DuplexProtocolError{
			Message:   ctlErr.Error(),
			RequestID: correlation,
			Details:   protocol.InputErrorDetails(record, definition),
		}
	}
	return ctrl, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// requiredText 非空字符串校验并 strip。Python: duplex_protocol.py:49-54 _required_text
func requiredText(name, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", &DuplexProtocolError{Message: name + " must be a non-empty string"}
	}
	return strings.TrimSpace(value), nil
}

// derefText 可选字符串解引用（nil → 空串，交由 requiredText 拒绝）。
func derefText(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}

// copyJSON 递归校验并拷贝 JSON 值（防御性深拷贝）。
// Python: duplex_protocol.py:57-73 _copy_json
//
// None/字符串/布尔/整数直传；浮点必须有限；dict 键必须字符串；
// 其余类型拒绝。Go 的 JSON 解码把一切数字落为 float64，有限性用
// IsInf/IsNaN 判定（DecodeMachineDocument 已预拒 NaN/Infinity 字面量，
// 此处是构造路径 tool_result.result 的同款守卫）。
func copyJSON(value any) (any, error) {
	switch v := value.(type) {
	case nil, string, bool:
		return v, nil
	case float64:
		if math.IsInf(v, 0) || math.IsNaN(v) {
			return nil, &DuplexProtocolError{Message: "answers must contain only finite JSON numbers"}
		}
		return v, nil
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			copied, err := copyJSON(item)
			if err != nil {
				return nil, err
			}
			result[key] = copied
		}
		return result, nil
	case []any:
		result := make([]any, 0, len(v))
		for _, item := range v {
			copied, err := copyJSON(item)
			if err != nil {
				return nil, err
			}
			result = append(result, copied)
		}
		return result, nil
	default:
		return nil, &DuplexProtocolError{Message: "answers must contain only JSON values"}
	}
}

// validateAnswerFields 校验公共 answer 字段并保留既有 JSON 扩展。
// Python: duplex_protocol.py:76-91 _validate_answer_fields
func validateAnswerFields(answer map[string]any) error {
	// Python: :78-81 question/custom_input 必须字符串
	for _, name := range []string{"question", "custom_input"} {
		if v, ok := answer[name]; ok {
			if _, isStr := v.(string); !isStr {
				return &DuplexProtocolError{Message: "answers." + name + " must be a string"}
			}
		}
	}
	// Python: :81-82 card_id 必须非空字符串
	if v, ok := answer["card_id"]; ok {
		s, _ := v.(string)
		if _, err := requiredText("answers.card_id", s); err != nil {
			return err
		}
	}
	// Python: :83-91 selected_options 必须字符串数组
	if v, ok := answer["selected_options"]; ok {
		options, isArray := v.([]any)
		if !isArray {
			return &DuplexProtocolError{Message: "answers.selected_options must be an array of strings"}
		}
		for _, option := range options {
			if _, isStr := option.(string); !isStr {
				return &DuplexProtocolError{Message: "answers.selected_options must be an array of strings"}
			}
		}
	}
	return nil
}

// copyAnswers 校验并防御性拷贝 answers 数组。
// Python: duplex_protocol.py:93-107 _copy_answers
func copyAnswers(items []map[string]any) ([]map[string]any, error) {
	if len(items) == 0 {
		return nil, &DuplexProtocolError{Message: "answers must be a non-empty array of objects"}
	}
	copied := make([]map[string]any, 0, len(items))
	for _, answer := range items {
		if answer == nil {
			return nil, &DuplexProtocolError{Message: "answers entries must be objects"}
		}
		if err := validateAnswerFields(answer); err != nil {
			return nil, err
		}
		dup, err := copyJSON(answer)
		if err != nil {
			return nil, err
		}
		copied = append(copied, dup.(map[string]any))
	}
	return copied, nil
}

// validateControlAndBuild 白名单/必填/版本校验后构造（Python _validate_fields + 构造）。
// Python: duplex_protocol.py:190-215 _validate_fields + :238-247
func validateControlAndBuild(record map[string]any) (*DuplexControl, error) {
	if err := validateControlFields(record); err != nil {
		return nil, err
	}
	return buildControlFromRecord(record)
}

// validateControlFields 字段白名单 / 必填 / schema_version 校验。
// Python: duplex_protocol.py:190-215 _validate_fields
func validateControlFields(record map[string]any) error {
	kind, _ := record["type"].(string)
	var allowed, required []string
	switch kind {
	case "answer":
		allowed, required = answerFields, answerFields
	case "cancel":
		allowed, required = cancelFields, commonRequired
	case "tool_result":
		allowed = toolResultFields
		required = toolResultRequired
		// Python: :200-204 result 与 error 必须恰好其一
		if hasKey(record, "result") == hasKey(record, "error") {
			return &DuplexProtocolError{Message: "tool_result requires exactly one result or error"}
		}
	default:
		return &DuplexProtocolError{Message: "control type must be answer, tool_result or cancel"}
	}
	// Python: :207-211 白名单外字段拒、必填缺失拒
	for key := range record {
		if !containsField(allowed, key) {
			return &DuplexProtocolError{Message: "control contains unknown fields"}
		}
	}
	for _, key := range required {
		if !hasKey(record, key) {
			return &DuplexProtocolError{Message: "control is missing required fields"}
		}
	}
	// Python: :212-215 schema_version 必须受支持
	if _, verr := protocol.RequireSupportedSchemaVersion(record["schema_version"]); verr != nil {
		return &DuplexProtocolError{Message: "schema_version is not supported"}
	}
	return nil
}

// buildControlFromRecord 从解码记录构造 DuplexControl。
// Python: duplex_protocol.py:238-247 DuplexControl(kind=..., ...) 构造调用
func buildControlFromRecord(record map[string]any) (*DuplexControl, error) {
	kind, _ := record["type"].(string)
	requestID, _ := record["request_id"].(string)
	answers, _ := record["answers"].([]any)
	return NewDuplexControl(
		kind,
		requestID,
		optionalText(record, "session_id"),
		optionalText(record, "interaction_id"),
		convertAnswers(answers),
		optionalText(record, "call_id"),
		optionalValue(record, "result"),
		optionalTextPtr(record, "error"),
	)
}

// convertAnswers JSON 数组转 answer 对象切片（非对象元素交由 copyAnswers 拒绝）。
func convertAnswers(items []any) []map[string]any {
	answers := make([]map[string]any, 0, len(items))
	for _, item := range items {
		answer, isObj := item.(map[string]any)
		if !isObj {
			answers = append(answers, nil)
			continue
		}
		answers = append(answers, answer)
	}
	return answers
}

// hasKey 键存在性判断（JSON null 也算存在，对齐 Python `in` 语义）。
func hasKey(record map[string]any, key string) bool {
	_, ok := record[key]
	return ok
}

// containsField 白名单包含判断。
func containsField(fields []string, key string) bool {
	for _, f := range fields {
		if f == key {
			return true
		}
	}
	return false
}

// optionalText 可选非空字符串字段（nil 或缺失 → nil）。
func optionalText(record map[string]any, key string) *string {
	v, ok := record[key]
	if !ok {
		return nil
	}
	s, isStr := v.(string)
	if !isStr || strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

// optionalTextPtr 可选字符串字段（存在且为字符串 → 指针；否则 nil）。
// 与 optionalText 的差别：cancel 的 error 字段由构造层拒绝，这里只搬运。
func optionalTextPtr(record map[string]any, key string) *string {
	v, ok := record[key]
	if !ok {
		return nil
	}
	s, isStr := v.(string)
	if !isStr {
		return nil
	}
	return &s
}

// derefAnyText 任意 JSON 值转文本（非字符串 → 空串，交由 requiredText 拒绝）。
func derefAnyText(value any) string {
	s, _ := value.(string)
	return s
}

// optionalValue 可选任意 JSON 字段（缺失 → nil）。
func optionalValue(record map[string]any, key string) any {
	v, ok := record[key]
	if !ok {
		return nil
	}
	return v
}
