// 智能审批的审查器审计辅助。
//
// 对应 Python 代码：_auto_permission/reviewer_audit.py（77 行 / 5 函数）
//
// 两块职责：
//  1. 把 Host 路由的**不可约简溯源**投影成审计字段（host_route_reason /
//     host_route_source / policy_level / reviewer_called）
//  2. 判定用户确认载荷属于哪一类：单次放行 / 拒绝 / 其他
//
// ## 安全要点：已退役的 task-scope 载荷不能变成 allow-once
//
// `task_grant` / `task_grant_proposal_id` / `task_grant_request_id` 三个键
// 是已退役的任务级授权拼写。识别它们的目的**不是**兼容，而是**拒绝**——
// 让旧载荷无法被当作「单次放行」而绕过后续审查。
//
// ## `persist_allow`：与 Python 一致（不做额外映射，已消除的偏差 V-11）
//
// Python 的对象分支用 `getattr(user_input, "persist_allow", False)`。
// openjiuwen 的 `PermissionConfirmResponse` **没有** `persist_allow` 属性
// （它提供的是 `wants_session_persist()` / `wants_permanent_persist()` 方法），
// 因此对确认响应对象该值恒为默认 `False`。
//
// Go 侧照此实现：`persist_allow` 恒 `False`，不把 `PersistScope` 映射进来。
// `PersistScope` 字段本身服务于 9.90d-1 的持久化逻辑，与本文件的判定无关。

package auto_permission

import (
	"encoding/json"

	hsecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// retiredTaskScopeConfirmationKeys 已退役的任务级授权键。
	//
	// Python: _RETIRED_TASK_SCOPE_CONFIRMATION_KEYS
	// 存在任一键即视为旧载荷，不得当作单次放行。
	retiredTaskScopeConfirmationKeys = map[string]struct{}{
		"task_grant":             {},
		"task_grant_proposal_id": {},
		"task_grant_request_id":  {},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ReviewerRouteAuditExtra 把 Host 路由溯源投影成审计字段。
//
// Python: _reviewer_route_audit_extra(route, *, policy_level="", reviewer_called=None)
// 只保留不可约简的字段；policy_level 与 reviewer_called 按需附加。
func ReviewerRouteAuditExtra(
	route *permissions.DecisionRoute,
	policyLevel string,
	reviewerCalled *bool,
) map[string]any {
	extra := map[string]any{}
	if route != nil {
		extra["host_route_reason"] = route.Reason
		extra["host_route_source"] = route.Source
	}
	if policyLevel != "" {
		extra["policy_level"] = policyLevel
	}
	if reviewerCalled != nil {
		extra["reviewer_called"] = *reviewerCalled
	}
	return extra
}

// IsAllowOnceConfirmation 载荷是否为「仅本次放行」的确认。
//
// Python: _is_allow_once_confirmation(user_input)
// 四个否定条件：载荷不可解析 / approved 非严格 true / 有持久化意图 /
// 含退役任务级键或审查器覆盖令牌。
func IsAllowOnceConfirmation(userInput any) bool {
	payload := ParseConfirmationPayload(userInput)
	if len(payload) == 0 {
		return false
	}
	approved, isBool := payload["approved"].(bool)
	if !isBool || !approved {
		return false
	}
	if isStrictTrue(payload["auto_confirm"]) || isStrictTrue(payload["persist_allow"]) {
		return false
	}
	if containsRetiredTaskScopeConfirmation(payload) {
		return false
	}
	if _, hasToken := payload["reviewer_override_token"]; hasToken {
		return false
	}
	return true
}

// IsRejectionConfirmation 载荷是否为明确拒绝。
//
// Python: _is_rejection_confirmation(user_input)
func IsRejectionConfirmation(userInput any) bool {
	payload := ParseConfirmationPayload(userInput)
	if len(payload) == 0 {
		return false
	}
	approved, isBool := payload["approved"].(bool)
	return isBool && !approved
}

// ParseConfirmationPayload 把确认载荷归一成 map，无法解析返回 nil。
//
// Python: _parse_confirmation_payload(user_input)
// 三种形态：map 直拷贝 / JSON 字符串解析 / 对象按属性提取。
func ParseConfirmationPayload(userInput any) map[string]any {
	switch typed := userInput.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			out[key] = value
		}
		return out
	case string:
		var decoded any
		if err := json.Unmarshal([]byte(typed), &decoded); err != nil {
			return nil
		}
		mapping, ok := decoded.(map[string]any)
		if !ok {
			return nil
		}
		out := make(map[string]any, len(mapping))
		for key, value := range mapping {
			out[key] = value
		}
		return out
	case hsecurity.PermissionConfirmResponse:
		return confirmResponsePayload(&typed)
	case *hsecurity.PermissionConfirmResponse:
		return confirmResponsePayload(typed)
	default:
		return nil
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// confirmResponsePayload 把确认响应对象投影成载荷 map。
//
// Python: getattr(user_input, "persist_allow", False)
// PermissionConfirmResponse 无该属性，故恒为 False——不把 PersistScope 映射进来。
func confirmResponsePayload(response *hsecurity.PermissionConfirmResponse) map[string]any {
	if response == nil {
		return nil
	}
	return map[string]any{
		"approved":      response.Approved,
		"auto_confirm":  response.AutoConfirm,
		"persist_allow": false,
		"feedback":      response.Feedback,
	}
}

// containsRetiredTaskScopeConfirmation 载荷是否含已退役的任务级授权键。
//
// Python: _contains_retired_task_scope_confirmation(payload)
func containsRetiredTaskScopeConfirmation(payload map[string]any) bool {
	for key := range retiredTaskScopeConfirmationKeys {
		if _, exists := payload[key]; exists {
			return true
		}
	}
	return false
}

// isStrictTrue 值是否严格为 bool true（非 truthy）。
func isStrictTrue(value any) bool {
	boolean, ok := value.(bool)
	return ok && boolean
}
