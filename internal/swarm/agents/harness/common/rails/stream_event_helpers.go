package rails

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	interruptHelpers "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sidKey 跨 Rail 传递 session ID 的 extra 键，对齐 Python: _SID_KEY
	sidKey = "__jiuwenswarm_session_id__"
	// defaultSessionID 默认 session ID，对齐 Python: "default"
	defaultSessionID = "default"
)

// todoToolNames TODO 相关工具名称集合，对齐 Python: _TODO_TOOL_NAMES
var todoToolNames = map[string]bool{
	"todo_create": true,
	"todo_get":    true,
	"todo_list":   true,
	"todo_modify": true,
}

var (
	// 对齐 Python: re.search(r"\bsuccess\s*[:=]\s*False\b", text, re.IGNORECASE)
	successFalseRe = regexp.MustCompile(`(?i)\bsuccess\s*[:=]\s*False\b`)
	// 对齐 Python: exit_code/returncode/return_code 正则，re.IGNORECASE
	exitCodeRe = regexp.MustCompile(`(?i)\b(?:exit(?:[_ ]?code)?|returncode|return[_ ]code)\s*[:= ]\s*(-?\d+)\b`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BoolishFalse 判断值是否为"假-ish"，对齐 Python: _boolish_false
func boolishFalse(value any) bool {
	if v, ok := value.(bool); ok {
		return !v
	}
	if s, ok := value.(string); ok {
		lower := strings.TrimSpace(strings.ToLower(s))
		return lower == "false" || lower == "0" || lower == "no"
	}
	return false
}

// BoolishTrue 判断值是否为"真-ish"，对齐 Python: _boolish_true
func boolishTrue(value any) bool {
	if v, ok := value.(bool); ok {
		return v
	}
	if s, ok := value.(string); ok {
		lower := strings.TrimSpace(strings.ToLower(s))
		return lower == "true" || lower == "1" || lower == "yes"
	}
	return false
}

// NonzeroExit 判断值是否为非零退出码，对齐 Python: _nonzero_exit
func nonzeroExit(value any) *bool {
	if _, ok := value.(bool); ok {
		return nil
	}
	if v, ok := value.(int); ok {
		result := v != 0
		return &result
	}
	if s, ok := value.(string); ok {
		parsed, err := parseInt(strings.TrimSpace(s))
		if err != nil {
			return nil
		}
		result := parsed != 0
		return &result
	}
	return nil
}

// InferToolResultError 推断工具结果是否为错误，对齐 Python: _infer_tool_result_error
func inferToolResultError(value any) *bool {
	switch v := value.(type) {
	case map[string]any:
		return inferDictError(v)
	case []any:
		for _, item := range v {
			if itemErr := inferToolResultError(item); itemErr != nil && *itemErr {
				return boolPtr(true)
			}
		}
		return nil
	case string:
		return inferStringError(v)
	default:
		return nil
	}
}

// StructuredToolResultPayload 提取结构化工具结果，对齐 Python: _structured_tool_result_payload
func structuredToolResultPayload(result any) any {
	if _, ok := result.(map[string]any); ok {
		return result
	}
	if _, ok := result.([]any); ok {
		return result
	}
	return nil
}

// ParseToolCallArguments 解析工具调用参数为 map，对齐 Python: _parse_tool_call_arguments
func parseToolCallArguments(tc *llmschema.ToolCall) map[string]any {
	if tc == nil {
		return map[string]any{}
	}
	args := tc.Arguments
	if args == "" {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(args), &result); err != nil {
		return map[string]any{}
	}
	return result
}

// ExtractToolInterrupt 递归查找 ToolInterruptException，对齐 Python: _extract_tool_interrupt
//
// 入参为 any 因为调用来源（ToolResult、exception）类型不固定，对齐 Python result: Any；
// 返回值为具体类型 *saschema.ToolInterruptException（Python 返回 Any 是因为鸭子类型，Go 应更严格）。
func extractToolInterrupt(value any) *saschema.ToolInterruptException {
	if value == nil {
		return nil
	}
	// 直接匹配 ToolInterruptException
	if exc, ok := value.(*saschema.ToolInterruptException); ok {
		return exc
	}
	// 通过 Unwrap 链递归查找（对齐 Python: cause / __cause__）
	if unwrapper, ok := value.(interface{ Unwrap() error }); ok {
		if cause := unwrapper.Unwrap(); cause != nil {
			if result := extractToolInterrupt(cause); result != nil {
				return result
			}
		}
	}
	return nil
}

// AskUserQuestionPayloadFromInterrupt 从中断构造 ask_user_question payload，对齐 Python: _ask_user_question_payload_from_interrupt
func askUserQuestionPayloadFromInterrupt(tc *llmschema.ToolCall, interrupt *saschema.ToolInterruptException) map[string]any {
	// 获取 request_id，对齐 Python: request_id = str(getattr(getattr(interrupt, "request", None), "tool_call_id", None) or getattr(tool_call, "id", "") or "")
	var requestID string
	if interrupt != nil && interrupt.Request != nil {
		// 尝试从 Request 获取 tool_call_id
		if accessor, ok := interrupt.Request.(interface{ GetToolCallID() string }); ok {
			requestID = strings.TrimSpace(accessor.GetToolCallID())
		}
	}
	if requestID == "" && tc != nil {
		requestID = strings.TrimSpace(tc.ID)
	}
	if requestID == "" {
		return nil
	}

	// 构造 value_obj，对齐 Python: value_obj = getattr(interrupt, "request", None)
	var valueObj any
	if interrupt != nil && interrupt.Request != nil {
		valueObj = interrupt.Request
	}
	if valueObj == nil {
		args := parseToolCallArguments(tc)
		if len(args) == 0 {
			return nil
		}
		valueObj = map[string]any{
			"tool_args": args,
			"questions": args["questions"],
		}
	}

	// 使用已有的 ConvertInteractionsToAskUserQuestion，对齐 Python: convert_interactions_to_ask_user_question([{"id": request_id, "value": value_obj}])
	interactions := []any{map[string]any{
		"id":    requestID,
		"value": valueObj,
	}}
	return interruptHelpers.ConvertInteractionsToAskUserQuestion(interactions)
}

// TruncateString 截断字符串到指定最大长度，对齐 Python: str(result)[:60000]
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// ToolCallName 安全获取 ToolCall 的 Name 字段
func toolCallName(tc *llmschema.ToolCall) string {
	if tc == nil {
		return ""
	}
	return tc.Name
}

// ToolCallID 安全获取 ToolCall 的 ID 字段
func toolCallID(tc *llmschema.ToolCall) string {
	if tc == nil {
		return ""
	}
	return tc.ID
}

// ToolCallArguments 安全获取 ToolCall 的 Arguments 字段
func toolCallArguments(tc *llmschema.ToolCall) any {
	if tc == nil {
		return map[string]any{}
	}
	if tc.Arguments == "" {
		return map[string]any{}
	}
	return tc.Arguments
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func boolPtr(b bool) *bool { return &b }

func parseInt(s string) (int, error) {
	var neg bool
	s = strings.TrimSpace(s)
	if len(s) > 0 && s[0] == '-' {
		neg = true
		s = s[1:]
	}
	if s == "" {
		return 0, fmt.Errorf("invalid int: empty")
	}
	var result int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, fmt.Errorf("invalid int: %s", s)
		}
		result = result*10 + int(c-'0')
	}
	if neg {
		return -result, nil
	}
	return result, nil
}

// inferDictError 推断 dict 类型结果的错误状态，对齐 Python: _infer_tool_result_error (dict branch)
func inferDictError(v map[string]any) *bool {
	// success 字段
	if val, ok := v["success"]; ok {
		if boolishFalse(val) {
			return boolPtr(true)
		}
		if boolishTrue(val) {
			return boolPtr(false)
		}
	}
	// is_error / isError 字段
	if boolishTrue(v["is_error"]) || boolishTrue(v["isError"]) {
		return boolPtr(true)
	}
	// status 字段
	if status, ok := v["status"].(string); ok {
		lower := strings.TrimSpace(strings.ToLower(status))
		if lower == "error" || lower == "failed" || lower == "failure" {
			return boolPtr(true)
		}
	}
	// exit_code / exitCode / returncode / return_code
	for _, key := range []string{"exit_code", "exitCode", "returncode", "return_code"} {
		if val, ok := v[key]; ok {
			if exitFailed := nonzeroExit(val); exitFailed != nil {
				return exitFailed
			}
		}
	}
	// 递归检查嵌套
	for _, key := range []string{"data", "raw_output", "rawOutput", "result"} {
		if nested, ok := v[key]; ok {
			switch n := nested.(type) {
			case map[string]any:
				if nestedErr := inferDictError(n); nestedErr != nil {
					return nestedErr
				}
			case []any:
				if nestedErr := inferToolResultError(n); nestedErr != nil {
					return nestedErr
				}
			}
		}
	}
	return nil
}

// inferStringError 推断 string 类型结果的错误状态，对齐 Python: _infer_tool_result_error (string branch)
func inferStringError(s string) *bool {
	text := strings.TrimSpace(s)
	if text == "" {
		return nil
	}
	// 尝试解析为 JSON
	var parsed any
	if err := json.Unmarshal([]byte(text), &parsed); err == nil {
		if nestedErr := inferToolResultError(parsed); nestedErr != nil {
			return nestedErr
		}
	}
	// success=False 模式，对齐 Python: re.search(r"\bsuccess\s*[:=]\s*False\b", text, re.IGNORECASE)
	if successFalseRe.MatchString(text) {
		return boolPtr(true)
	}
	// [ERROR] 前缀
	if strings.HasPrefix(text, "[ERROR]") {
		return boolPtr(true)
	}
	// exit_code 模式，使用预编译正则
	if m := exitCodeRe.FindStringSubmatch(text); len(m) > 1 {
		if code := parseIntSafe(m[1]); code != 0 {
			return boolPtr(true)
		}
		return boolPtr(false)
	}
	return nil
}

func parseIntSafe(s string) int {
	s = strings.TrimSpace(s)
	if len(s) > 0 && s[0] == '-' {
		n := parseIntPositive(s[1:])
		return -n
	}
	return parseIntPositive(s)
}

func parseIntPositive(s string) int {
	var result int
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0
		}
		result = result*10 + int(c-'0')
	}
	return result
}
