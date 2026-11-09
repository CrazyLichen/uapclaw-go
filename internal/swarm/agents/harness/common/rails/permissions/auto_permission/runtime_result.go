// 权限判定结果在运行时的还原与投影。
//
// 对应 Python 代码：_auto_permission/runtime_result.py（250 行 / 15 函数）
//
// 职责：把「被包装权限轨返回的判定结果」翻译成三种运行时产物：
//  1. 工具结果 payload（拒绝时给模型看的结构化错误）
//  2. 中断消息与附加字段（弹权限卡时 UI 用的文案与元数据）
//  3. 终态结局（allow / deny / cancel）与审计用的决策来源
//
// ## 偏差 V-3：5 个反射适配函数不实现
//
// Python 用 inspect.signature 动态适配父类 before_tool_call 的调用形状：
//   - _resolve_base_call_args
//   - _needs_runtime_tool_call
//   - _positional_capacity
//   - _filter_base_call_kwargs
//   - _is_openjiuwen_runtime_context
//
// Go 的函数签名静态固定，无 inspect 需求，这 5 个函数没有对应物。
// 调用父类的部分由 runtime_bridge.go 的 callBaseRail 直接以固定签名完成。

package auto_permission

import (
	"regexp"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// skipToolExtraKey 父类设置「跳过工具执行」的 extra 键
	//
	// Python: extra.get("_skip_tool")
	skipToolExtraKey = "_skip_tool"

	// defaultPermissionDeniedText 无自定义反馈时的默认拒绝文案
	//
	// Python: "[PERMISSION_DENIED] Tool execution was denied."
	defaultPermissionDeniedText = "[PERMISSION_DENIED] Tool execution was denied."

	// defaultManualApprovalMessage 无中断消息时的默认文案
	//
	// Python: "manual approval required"
	defaultManualApprovalMessage = "manual approval required"

	// unknownDecisionSource 决策来源非法时的回落值
	//
	// Python: "unknown"
	unknownDecisionSource = "unknown"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// terminalDecisionSourcePattern 合法决策来源的形态（小写字母开头 + 字母数字下划线）
	//
	// Python: re.fullmatch(r"[a-z][a-z0-9_]{0,63}", normalized)
	terminalDecisionSourcePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

	// terminalOutcomeByResultClass 结果类别 → 终态结局（interrupt 无终态）
	//
	// Python: {"allow": "allow", "denied": "deny", "interrupt": None,
	// "user_rejection": "cancel"}
	terminalOutcomeByResultClass = map[string]string{
		permissions.PermissionResultAllow:         "allow",
		permissions.PermissionResultDenied:        "deny",
		permissions.PermissionResultUserRejection: "cancel",
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// resultFromBaseSideEffect 从父类的副作用里还原判定结果。
//
// Python: _result_from_base_side_effect(ctx, result)
// 父类返回 nil 但设置了 _skip_tool 时，真实结果在 inputs.ToolResult 里。
func resultFromBaseSideEffect(
	ctx *agentinterfaces.AgentCallbackContext,
	result any,
) any {
	if result != nil || ctx == nil {
		return result
	}
	skipTool, exists := ctx.Extra()[skipToolExtraKey]
	if !exists || skipTool != true {
		return result
	}
	inputs, ok := ctx.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok || inputs.ToolResult == nil {
		return result
	}
	return inputs.ToolResult
}

// permissionToolResultText 生成给模型看的拒绝文本。
//
// Python: _permission_tool_result_text(result)
// 优先级：feedback 文本 → 非空字符串 → 默认文案。
func permissionToolResultText(result any) string {
	if feedback := permissionFeedbackText(result); feedback != "" {
		return feedback
	}
	if text, ok := result.(string); ok && text != "" {
		return text
	}
	return defaultPermissionDeniedText
}

// permissionToolResultPayload 生成工具结果 payload。
//
// Python: _permission_tool_result_payload(result, text)
// 拒绝类结果返回结构化 payload，其余直接返回文本。
func permissionToolResultPayload(result any, text string) any {
	resultClass := permissions.ClassifyPermissionResult(result)
	if resultClass == permissions.PermissionResultDenied ||
		resultClass == permissions.PermissionResultUserRejection ||
		isPermissionDeniedMetadataPayload(result) {
		return permissionDeniedToolResultPayload(result, text)
	}
	return text
}

// isPermissionDeniedMetadataPayload 结果是否自带「已拒绝」标记。
//
// Python: _is_permission_denied_metadata_payload(result)
func isPermissionDeniedMetadataPayload(result any) bool {
	mapping, ok := result.(map[string]any)
	if !ok {
		return false
	}
	if strings.ToLower(strings.TrimSpace(anyToStringValue(mapping["permission_status"]))) == "denied" {
		return true
	}
	if strings.ToLower(strings.TrimSpace(anyToStringValue(mapping["status"]))) == "denied" {
		return true
	}
	return strings.Contains(strings.ToLower(permissionFeedbackText(result)), "[permission_denied]")
}

// permissionDeniedToolResultPayload 构造拒绝结果的结构化 payload。
//
// Python: _permission_denied_tool_result_payload(result, text)
// 固定 7 个字段 + 可选的 reviewer_metadata / reason。
func permissionDeniedToolResultPayload(result any, text string) map[string]any {
	payload := map[string]any{
		"success":             false,
		"status":              "denied",
		"is_error":            true,
		"result":              text,
		"error":               text,
		"permission_decision": "deny",
		"permission_status":   "denied",
	}
	mapping, ok := result.(map[string]any)
	if !ok {
		return payload
	}
	if reviewerMetadata, isMap := mapping["reviewer_metadata"].(map[string]any); isMap {
		copied := make(map[string]any, len(reviewerMetadata))
		for key, value := range reviewerMetadata {
			copied[key] = value
		}
		payload["reviewer_metadata"] = copied
	}
	if reason, exists := mapping["reason"]; exists && anyToStringValue(reason) != "" {
		payload["reason"] = anyToStringValue(reason)
	}
	return payload
}

// permissionInterruptMessage 提取中断消息文案。
//
// Python: _permission_interrupt_message(result)
// 优先级：value.message → message → feedback → 默认文案。
func permissionInterruptMessage(result any) string {
	if mapping, ok := result.(map[string]any); ok {
		if value, isMap := mapping["value"].(map[string]any); isMap {
			if message, exists := value["message"]; exists && message != nil {
				return anyToStringValue(message)
			}
		}
		if message, exists := mapping["message"]; exists && message != nil {
			return anyToStringValue(message)
		}
	}
	if feedback := permissionFeedbackText(result); feedback != "" {
		return feedback
	}
	return defaultManualApprovalMessage
}

// permissionInterruptExtraFields 收集中断的附加字段。
//
// Python: _permission_interrupt_extra_fields(result)
// metadata 全量合并；value 段排除 message / question。
func permissionInterruptExtraFields(result any) map[string]any {
	mapping, ok := result.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	extraFields := map[string]any{}
	if metadata, isMap := mapping["metadata"].(map[string]any); isMap {
		for key, value := range metadata {
			extraFields[key] = value
		}
	}
	if value, isMap := mapping["value"].(map[string]any); isMap {
		for key, nestedValue := range value {
			if key == "message" || key == "question" {
				continue
			}
			extraFields[key] = nestedValue
		}
	}
	return extraFields
}

// permissionTerminalOutcome 结果类别对应的终态结局，无终态返回空串。
//
// Python: _permission_terminal_outcome(result)
func permissionTerminalOutcome(result any) string {
	return terminalOutcomeByResultClass[permissions.ClassifyPermissionResult(result)]
}

// permissionTerminalDecisionSource 归一宿主提供的决策来源（供审计观测）。
//
// Python: _permission_terminal_decision_source(source)
// 只接受 `[a-z][a-z0-9_]{0,63}`，其余回落 unknown。
func permissionTerminalDecisionSource(source string) string {
	normalized := strings.ToLower(strings.TrimSpace(source))
	if terminalDecisionSourcePattern.MatchString(normalized) {
		return normalized
	}
	return unknownDecisionSource
}

// permissionFeedbackText 提取结果的 feedback 文本。
//
// Python: _permission_feedback_text(result)
// 三种形态：字符串本身、map 的 feedback（含 value.feedback 嵌套）、对象的 Feedback 方法。
func permissionFeedbackText(result any) string {
	switch typed := result.(type) {
	case string:
		return typed
	case map[string]any:
		rawFeedback, exists := typed["feedback"]
		if !exists || rawFeedback == nil {
			if value, isMap := typed["value"].(map[string]any); isMap {
				rawFeedback = value["feedback"]
			}
		}
		if rawFeedback == nil {
			return ""
		}
		return anyToStringValue(rawFeedback)
	default:
		if provider, ok := result.(interface{ Feedback() string }); ok {
			return provider.Feedback()
		}
		return ""
	}
}

// 编译期断言：确保 ToolCallInputs 可用于副作用还原
var _ = (*agentinterfaces.ToolCallInputs)(nil)
var _ = (*llmschema.ToolCall)(nil)
