// 单次放行的审查器前置闸门。
//
// 对应 Python 代码：_auto_permission/reviewer_override_gate.py（67 行 / 1 方法）
// Python: class AutoPermissionReviewerOverrideGateMixin
//
// 职责：在「用户已单次放行」这条路径上再跑一次路由，确保硬拦截仍然生效——
// 用户点「允许一次」不能绕过宿主侧的硬性禁止。
//
// 返回值语义：
//   - (false, nil) → 放行判定交由后续流程（审查器未配置 / 领域路由要求人工）
//   - (true, resp) → 硬拦截生效，resp 为拒绝响应
//   - (true, nil)  → 闸门通过，继续走单次放行

package auto_permission

import (
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// scopedCandidateGuardResult 有领域路由时的护栏结果标记
	//
	// Python: guard_result = "scoped_candidate" if domain_route is not None else "not_applicable"
	scopedCandidateGuardResult = "scoped_candidate"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// allowOnceReviewerGateDecision 单次放行前的审查器闸门。
//
// Python: AutoPermissionReviewerOverrideGateMixin._allow_once_reviewer_gate_decision(
// facts, *, policy_level, original_user_intent, domain_route)
func (r *AutoPermissionRail) allowOnceReviewerGateDecision(
	facts permissions.ToolDecisionFacts,
	policyLevel string,
	originalUserIntent *permissions.OriginalUserIntentEvidence,
	domainRoute *permissions.DecisionRoute,
) (bool, any) {
	if r.autoReviewer == nil {
		return false, nil
	}
	guardResult := defaultGuardResult
	if domainRoute != nil {
		guardResult = scopedCandidateGuardResult
	}
	route := r.reviewerRoute(
		facts, policyLevel, guardResult, originalUserIntent, domainRoute, nil)

	if route.IsHardBlock() {
		metadata := map[string]any{
			"action_summary": reviewerActionSummary(facts),
		}
		for key, value := range ReviewerRouteAuditExtra(&route, policyLevel, boolPtr(false)) {
			metadata[key] = value
		}
		r.emitAudit(facts, auditRecord{
			Decision: permissions.AutoDecisionDenyLevel,
			Reason:   route.Reason,
			Degraded: false,
			Extra:    metadata,
		}) // ⤵️ 9.90e 回填桩体
		return true, permissions.BuildDeniedPermissionResponse(route.Reason)
	}
	// 领域路由要求人工时，交回既有人工单次放行路径处理
	if domainRoute != nil && domainRoute.RequiresManual() {
		return false, nil
	}
	return true, nil
}
