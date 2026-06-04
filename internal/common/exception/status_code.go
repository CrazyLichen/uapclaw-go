package exception

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StatusCode 统一状态码，每个实例携带整数编码和消息模板。
//
// StatusCode 是全局错误码的单一真相源。Message 是带 {placeholder} 占位符的
// 消息模板，实例化 BaseError 时才渲染。
//
// 字段不导出，保证创建后不可修改，与 Python Enum 不可变语义一致。
//
// 对应 Python: openjiuwen/core/common/exception/codes.py (StatusCode)
type StatusCode struct {
	// code 整数错误码
	code int
	// message 消息模板（带 {placeholder} 占位符）
	message string
	// name 枚举名称（如 "WORKFLOW_EXECUTION_ERROR"）
	name string
}

// ──────────────────────────── 常量 ────────────────────────────

// missingKeyPlaceholder 模板渲染时缺失 key 的占位符格式
const missingKeyPlaceholder = "<missing:%s>"

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStatusCode 创建 StatusCode 实例。
//
// name 为枚举名称（如 "WORKFLOW_EXECUTION_ERROR"），
// code 为整数编码，msg 为消息模板。
//
// 对应 Python: StatusCode.NAME = (code, msg)
func NewStatusCode(name string, code int, msg string) StatusCode {
	return StatusCode{code: code, message: msg, name: name}
}

// Code 返回整数错误码。
//
// 对应 Python: StatusCode.XXX.code
func (s StatusCode) Code() int {
	return s.code
}

// Message 返回消息模板（未渲染）。
//
// 对应 Python: StatusCode.XXX.errmsg
func (s StatusCode) Message() string {
	return s.message
}

// Name 返回枚举名称。
//
// 对应 Python: StatusCode.XXX.name
func (s StatusCode) Name() string {
	return s.name
}

// String 实现 fmt.Stringer 接口，返回 "NAME(code)" 格式。
func (s StatusCode) String() string {
	return fmt.Sprintf("%s(%d)", s.name, s.code)
}

// GoString 实现 fmt.GoStringer 接口，用于 %#v 格式化。
func (s StatusCode) GoString() string {
	return fmt.Sprintf("StatusCode{Name:%q, Code:%d, Message:%q}", s.name, s.code, s.message)
}

// MarshalJSON 实现 json.Marshaler 接口，序列化为 {"name":"...","code":...,"message":"..."}。
func (s StatusCode) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{
		"name":    s.name,
		"code":    s.code,
		"message": s.message,
	})
}

// UnmarshalJSON 实现 json.Unmarshaler 接口。
func (s *StatusCode) UnmarshalJSON(data []byte) error {
	var raw struct {
		Name    string `json:"name"`
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("StatusCode 反序列化失败: %w", err)
	}
	s.name = raw.Name
	s.code = raw.Code
	s.message = raw.Message
	return nil
}

// RenderMessage 渲染消息模板，使用 params 填充占位符。
//
// 缺失的 key 不报错，显示为 <missing:key>。
// 渲染过程不会产生异常，保证错误路径安全。
//
// 对应 Python: _format_template(status.errmsg, params)
func (s StatusCode) RenderMessage(params map[string]any) string {
	return renderTemplate(s.message, params)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// renderTemplate 安全渲染消息模板，缺失 key 显示 <missing:key>。
//
// 对应 Python: _format_template() + _SafeDict
func renderTemplate(template string, params map[string]any) string {
	if template == "" {
		return ""
	}

	result := template
	if params == nil {
		// 无参数时，将所有占位符标记为 missing
		return replaceMissingPlaceholders(result)
	}

	// 用提供的参数逐个替换占位符
	for key, val := range params {
		placeholder := "{" + key + "}"
		valStr := fmt.Sprintf("%v", val)
		result = strings.ReplaceAll(result, placeholder, valStr)
	}

	// 将剩余未替换的占位符标记为 <missing:key>
	result = replaceMissingPlaceholders(result)

	return result
}

// replaceMissingPlaceholders 将模板中未替换的 {xxx} 替换为 <missing:xxx>。
//
// 仅处理合法的占位符名（字母/数字/下划线），其他形式（如 JSON 对象）原样保留。
func replaceMissingPlaceholders(s string) string {
	var result strings.Builder
	result.Grow(len(s))
	i := 0
	for i < len(s) {
		if s[i] == '{' {
			// 查找对应的 }
			j := i + 1
			for j < len(s) && s[j] != '}' {
				j++
			}
			if j < len(s) && j > i+1 {
				// 找到了 {xxx} 形式的占位符
				key := s[i+1 : j]
				// 仅处理合法的占位符名（字母/数字/下划线）
				if isValidPlaceholderName(key) {
					result.WriteString(fmt.Sprintf(missingKeyPlaceholder, key))
					i = j + 1
					continue
				}
				// 非法占位符名，原样保留
				result.WriteString(s[i : j+1])
				i = j + 1
				continue
			}
		}
		result.WriteByte(s[i])
		i++
	}
	return result.String()
}

// isValidPlaceholderName 判断是否为合法的占位符名称（字母/数字/下划线）。
func isValidPlaceholderName(s string) bool {
	if len(s) == 0 {
		return false
	}
	for _, c := range s {
		if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_') {
			return false
		}
	}
	return true
}
