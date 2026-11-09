// openjiuwen 权限 rail 的运行时契约助手。
//
// 对应 Python 代码：
// jiuwenswarm/agents/harness/common/rails/permissions/openjiuwen_contract.py（183 行）
//
// 职责：把 openjiuwen 权限 rail 的能力面（类型、方法、工厂）收敛成一份契约，
// 并提供标准化的拒绝/驳回/人工审批响应构造与结果分类。
//
// 偏差（spec §五 V-3b）：Python 的 load_openjiuwen_permission_contract 用
// inspect.iscoroutinefunction / hasattr / callable 做**运行时**能力探测；
// Go 的对应能力由**类型系统**在编译期保证，故退化为常量 true +
// 编译期断言（见文件底部 var _ = ...）。

package permissions

import (
	"reflect"
	"strings"

	secrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
)

// ──────────────────────────── 结构体 ────────────────────────────

// OpenJiuwenPermissionContract openjiuwen 权限 rail 集成的运行时契约。
//
// Python: @dataclass(frozen=True) OpenJiuwenPermissionContract
// Python 的两个 `type[Any]` 字段在 Go 用 reflect.Type 承载。
type OpenJiuwenPermissionContract struct {
	// PermissionRailClass 权限 rail 类型
	PermissionRailClass reflect.Type
	// PermissionConfirmResponseClass 确认响应类型
	PermissionConfirmResponseClass reflect.Type
	// BeforeToolCallIsAsync 钩子是否异步（Go 无 async 概念，恒 true）
	BeforeToolCallIsAsync bool
	// SupportsUpdateConfig 是否支持热更新配置
	SupportsUpdateConfig bool
	// SupportsManualApprovalResponse 是否支持人工审批响应
	SupportsManualApprovalResponse bool
	// SupportsPermissionEngineFactory 是否支持权限引擎工厂
	SupportsPermissionEngineFactory bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ManualApprovalResponseID 人工审批中断的响应 id
	ManualApprovalResponseID = "auto_permission_manual_approval_required"
	// ManualApprovalToolName 人工审批中断的伪工具名
	ManualApprovalToolName = "auto_permission"
	// PermissionDeniedPrefix 拒绝反馈前缀
	PermissionDeniedPrefix = "[PERMISSION_DENIED] "
	// PermissionRejectedPrefix 用户驳回反馈前缀
	PermissionRejectedPrefix = "[PERMISSION_REJECTED] "

	// PermissionResultAllow 结果分类：允许
	PermissionResultAllow = "allow"
	// PermissionResultDenied 结果分类：拒绝
	PermissionResultDenied = "denied"
	// PermissionResultInterrupt 结果分类：待人工交互
	PermissionResultInterrupt = "interrupt"
	// PermissionResultUserRejection 结果分类：用户驳回/取消
	PermissionResultUserRejection = "user_rejection"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// 编译期断言：把 Python 的运行时能力探测固化为类型系统约束。
	// Python 的 hasattr(PermissionInterruptRail, "update_config") /
	// callable(build_permission_interrupt_rail) 在 Go 侧由下列断言保证。
	_ = (*secrail.PermissionInterruptRail)(nil)
	_ = (*secrail.PermissionInterruptRail).UpdateConfig
	_ = security.BuildPermissionInterruptRail
)

// ──────────────────────────── 导出函数 ────────────────────────────

// LoadOpenJiuwenPermissionContract 加载并校验 openjiuwen 权限 rail 契约。
//
// Python: load_openjiuwen_permission_contract()
// Python 的 ImportError → RuntimeError 在 Go 由编译期导入失败承担；
// iscoroutinefunction / hasattr / callable 三项探测退化为常量 true（偏差 V-3b）。
func LoadOpenJiuwenPermissionContract() *OpenJiuwenPermissionContract {
	return &OpenJiuwenPermissionContract{
		PermissionRailClass:             reflect.TypeOf((*secrail.PermissionInterruptRail)(nil)),
		PermissionConfirmResponseClass:  reflect.TypeOf(security.PermissionConfirmResponse{}),
		BeforeToolCallIsAsync:           true,
		SupportsUpdateConfig:            true,
		SupportsManualApprovalResponse:  true,
		SupportsPermissionEngineFactory: true,
	}
}

// BuildDeniedPermissionResponse 构造规范的拒绝权限响应。
//
// Python: build_denied_permission_response(reason)
func BuildDeniedPermissionResponse(reason string) *security.PermissionConfirmResponse {
	return &security.PermissionConfirmResponse{
		Approved:    false,
		AutoConfirm: false,
		Feedback:    PermissionDeniedPrefix + reason,
	}
}

// BuildRejectedPermissionResponse 构造规范的用户驳回权限响应。
//
// Python: build_rejected_permission_response(reason)
func BuildRejectedPermissionResponse(reason string) *security.PermissionConfirmResponse {
	return &security.PermissionConfirmResponse{
		Approved:    false,
		AutoConfirm: false,
		Feedback:    PermissionRejectedPrefix + reason,
	}
}

// BuildManualApprovalRequiredResponse 构造规范的人工审批中断响应。
//
// Python: build_manual_approval_required_response(reason, metadata=None, *, prompt_payload=None)
// 合并顺序：先 message/tool_name，再 prompt_payload，最后 metadata（后者覆盖前者）。
// metadata 非空时同时并入 value 与顶层 metadata（顶层是副本）。
func BuildManualApprovalRequiredResponse(
	reason string,
	metadata map[string]any,
	promptPayload map[string]any,
) map[string]any {
	value := map[string]any{
		"message":   reason,
		"tool_name": ManualApprovalToolName,
	}
	// Python 的 `if prompt_payload:` / `if metadata:` 对空 map 为假，不合并
	if len(promptPayload) > 0 {
		for k, v := range promptPayload {
			value[k] = v
		}
	}
	if len(metadata) > 0 {
		for k, v := range metadata {
			value[k] = v
		}
	}
	response := map[string]any{
		"id":        ManualApprovalResponseID,
		"interrupt": true,
		"value":     value,
	}
	if len(metadata) > 0 {
		copied := make(map[string]any, len(metadata))
		for k, v := range metadata {
			copied[k] = v
		}
		response["metadata"] = copied
	}
	return response
}

// ClassifyPermissionResult 对 openjiuwen before_tool_call 的结果分类。
//
// Python: classify_permission_result(result)
// 判定顺序（不可调换）：
//  1. nil → allow
//  2. feedback 含 [permission_rejected] → user_rejection
//  3. feedback 含 [permission_denied] → denied
//  4. 对象有 approved 属性：true → allow，false → denied
//  5. map 的 interrupt 为 true → interrupt
//  6. map 同时有 id 与 value → interrupt
//  7. 其余 → interrupt
//
// ⚠️ Python 的 getattr(result, "approved") 对 **dict 无效**（dict 无属性），
// 故 map 里的 "approved" 键不参与第 4 步，最终落到 interrupt。Go 侧忠实复刻。
func ClassifyPermissionResult(result any) string {
	if result == nil {
		return PermissionResultAllow
	}
	if isUserRejectionPayload(result) {
		return PermissionResultUserRejection
	}
	if isPermissionDeniedPayload(result) {
		return PermissionResultDenied
	}
	// 第 4 步：只对「有 approved 属性」的对象生效（不含 map / string）
	if approved, ok := approvedAttribute(result); ok {
		if approved {
			return PermissionResultAllow
		}
		return PermissionResultDenied
	}
	if m, ok := result.(map[string]any); ok {
		if v, exists := m["interrupt"]; exists && v == true {
			return PermissionResultInterrupt
		}
		_, hasID := m["id"]
		_, hasValue := m["value"]
		if hasID && hasValue {
			return PermissionResultInterrupt
		}
	}
	return PermissionResultInterrupt
}

// IsAllowResult 结果是否表示可继续执行。
//
// Python: is_allow_result(result)
func IsAllowResult(result any) bool {
	return ClassifyPermissionResult(result) == PermissionResultAllow
}

// IsDeniedResult 结果是否表示执行被拒绝。
//
// Python: is_denied_result(result)
func IsDeniedResult(result any) bool {
	return ClassifyPermissionResult(result) == PermissionResultDenied
}

// IsInterruptResult 结果是否表示待人工交互。
//
// Python: is_interrupt_result(result)
func IsInterruptResult(result any) bool {
	return ClassifyPermissionResult(result) == PermissionResultInterrupt
}

// IsUserRejectionResult 结果是否表示用户驳回或取消。
//
// Python: is_user_rejection_result(result)
func IsUserRejectionResult(result any) bool {
	return ClassifyPermissionResult(result) == PermissionResultUserRejection
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isUserRejectionPayload 判定反馈里是否含用户驳回标记。
//
// Python: _is_user_rejection_payload(result)
// 注意字符串分支的特殊性：只认 [permission_rejected]，**不认** cancelled 标记
// （Python 在 `isinstance(result, str)` 时直接 return False）。
func isUserRejectionPayload(result any) bool {
	feedback := permissionFeedback(result)
	normalized := strings.ToLower(feedback)
	if strings.Contains(normalized, "[permission_rejected]") {
		return true
	}
	if _, isString := result.(string); isString {
		return false
	}
	return strings.Contains(normalized, "cancelled") || strings.Contains(normalized, "canceled")
}

// isPermissionDeniedPayload 判定反馈里是否含权限拒绝标记。
//
// Python: _is_permission_denied_payload(result)
func isPermissionDeniedPayload(result any) bool {
	normalized := strings.ToLower(permissionFeedback(result))
	return strings.Contains(normalized, "[permission_denied]")
}

// permissionFeedback 从结果里取反馈文本。
//
// Python: _is_user_rejection_payload / _is_permission_denied_payload 共用的
// feedback 提取逻辑（string / dict / 对象三态）。
func permissionFeedback(result any) string {
	switch v := result.(type) {
	case nil:
		return ""
	case string:
		return v
	case map[string]any:
		if raw, exists := v["feedback"]; exists && raw != nil {
			return anyToStringValue(raw)
		}
		if nested, ok := v["value"].(map[string]any); ok {
			if raw, exists := nested["feedback"]; exists && raw != nil {
				return anyToStringValue(raw)
			}
		}
		return ""
	case *security.PermissionConfirmResponse:
		if v == nil {
			return ""
		}
		return v.Feedback
	default:
		return ""
	}
}

// approvedAttribute 取对象的 approved 属性（仅结构体，不含 map/string）。
//
// Python: getattr(result, "approved", None)
// 第二个返回值为 false 表示该类型没有 approved 属性。
func approvedAttribute(result any) (bool, bool) {
	switch v := result.(type) {
	case *security.PermissionConfirmResponse:
		if v == nil {
			return false, false
		}
		return v.Approved, true
	case security.PermissionConfirmResponse:
		return v.Approved, true
	default:
		return false, false
	}
}
