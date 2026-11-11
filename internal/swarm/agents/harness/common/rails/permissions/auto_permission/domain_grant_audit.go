// 固定领域路由的判定与响应。
//
// 对应 Python 代码：_auto_permission/domain_grant_audit.py（162 行 / 7 方法）
// Python: class AutoPermissionDomainGrantAuditMixin
//
// 「固定路由表」指宿主写死的领域规则（不走插件、不走注册表）。本文件负责：
//   - 用 `DeterministicDomainRoute` 算出领域路由
//   - 按路由来源分发到审查器 / 拒绝 / 人工三条出口
//   - 维护沙箱档位缓存

package auto_permission

import (
	"context"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// handleDomainRouteOptions handleDomainRoute 的入参。
//
// Python 的该方法除 facts 外全是关键字参数，Go 用结构体承载。
type handleDomainRouteOptions struct {
	// DomainRoute 领域路由（必填）
	DomainRoute *permissions.DecisionRoute
	// PolicyLevel 策略档位
	PolicyLevel string
	// SessionID 会话标识
	SessionID string
	// RequestID 宿主请求标识
	RequestID string
	// ToolCallID 工具调用标识
	ToolCallID string
	// UserInput 用户确认载荷
	UserInput any
	// OriginalUserIntent 原始用户意图证据
	OriginalUserIntent *permissions.OriginalUserIntentEvidence
	// ChannelKind 通道类型（默认 web）
	ChannelKind string
	// RuntimeCtx 运行时上下文
	RuntimeCtx *agentinterfaces.AgentCallbackContext
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// domainPolicyDecisionSource 领域策略的决策来源
	//
	// Python: "domain_policy"
	domainPolicyDecisionSource = "domain_policy"

	// hostRouteDecisionSource 宿主路由的决策来源
	//
	// Python: "host_route"
	hostRouteDecisionSource = "host_route"

	// domainFinalStatusDenied 领域路由的拒绝终态
	//
	// Python: final_status="denied"
	domainFinalStatusDenied = "denied"

	// domainFinalStatusManual 领域路由的人工终态
	//
	// Python: final_status="manual"
	domainFinalStatusManual = "manual"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// evaluateDomainRoute 用固定路由表算出领域路由。
//
// Python: AutoPermissionDomainGrantAuditMixin._evaluate_domain_route(
// facts, *, original_user_intent, recent_url_sources=())
func (r *AutoPermissionRail) evaluateDomainRoute(
	facts permissions.ToolDecisionFacts,
	originalUserIntent *permissions.OriginalUserIntentEvidence,
	recentURLSources []permissions.RecentURLSource,
) *permissions.DecisionRoute {
	return permissions.DeterministicDomainRoute(
		facts, originalUserIntent, recentURLSources, r.browserRuntimeSecurityProfile)
}

// currentSandboxProfile 返回当前沙箱档位（有 sysOperation 时重建并缓存）。
//
// Python: AutoPermissionDomainGrantAuditMixin._current_sandbox_profile()
func (r *AutoPermissionRail) currentSandboxProfile() *permissions.SandboxDescriptor {
	if r.sysOperation == nil {
		return r.sandbox
	}
	profile := permissions.BuildSandboxProfile(r.sysOperation)
	r.sandbox = &profile
	return r.sandbox
}

// handleDomainRoute 按领域路由分发到审查器或人工。
//
// Python: AutoPermissionDomainGrantAuditMixin._handle_domain_route(
// facts, *, domain_route, policy_level, session_id, request_id, tool_call_id,
// user_input, original_user_intent, channel_kind="web", runtime_ctx=None)
//
// 顺序对齐：路由要求审查器 → 交给 maybeRunReviewer；其结论未处理时才落人工。
func (r *AutoPermissionRail) handleDomainRoute(
	ctx context.Context,
	facts permissions.ToolDecisionFacts,
	opts handleDomainRouteOptions,
) (*PermissionHandlingResult, context.Context) {
	if opts.DomainRoute != nil && opts.DomainRoute.RequiresReviewer() {
		channelKind := opts.ChannelKind
		if channelKind == "" {
			channelKind = defaultChannelKindForExecution
		}
		reviewerResult, derivedCtx := r.maybeRunReviewer(ctx, facts, reviewerExecutionOptions{
			PolicyLevel:        opts.PolicyLevel,
			PolicyReason:       opts.DomainRoute.Reason,
			SessionID:          opts.SessionID,
			RequestID:          opts.RequestID,
			ToolCallID:         opts.ToolCallID,
			UserInput:          opts.UserInput,
			OriginalUserIntent: opts.OriginalUserIntent,
			DomainRoute:        opts.DomainRoute,
			GuardResult:        scopedCandidateGuardResult,
			ChannelKind:        channelKind,
			RuntimeCtx:         opts.RuntimeCtx,
		})
		ctx = derivedCtx
		if reviewerResult.Handled {
			return reviewerResult, ctx
		}
	}
	result, _ := NewPermissionHandlingResult(
		true, r.manualDomainRouteResponse(facts, opts.DomainRoute), domainPolicyDecisionSource)
	return result, ctx
}

// denyDomainRoute 构造领域路由的拒绝响应。
//
// Python: AutoPermissionDomainGrantAuditMixin._deny_domain_route(facts, route)
func (r *AutoPermissionRail) denyDomainRoute(
	facts permissions.ToolDecisionFacts,
	route *permissions.DecisionRoute,
) map[string]any {
	metadata := domainRouteMetadata(facts, route, domainFinalStatusDenied)
	r.emitAudit(facts, auditRecord{
		Decision: permissions.AutoDecisionDenyLevel,
		Reason:   routeReason(route),
		Degraded: false,
		Extra:    metadata,
	})
	return deniedPermissionResponse(routeReason(route), metadata)
}

// manualDomainRouteResponse 构造领域路由的人工审批响应。
//
// Python: AutoPermissionDomainGrantAuditMixin._manual_domain_route_response(facts, route)
func (r *AutoPermissionRail) manualDomainRouteResponse(
	facts permissions.ToolDecisionFacts,
	route *permissions.DecisionRoute,
) map[string]any {
	metadata := domainRouteMetadata(facts, route, domainFinalStatusManual)
	r.emitAudit(facts, auditRecord{
		Decision: permissions.AutoDecisionAskLevel,
		Reason:   routeReason(route),
		Degraded: true,
		Extra:    metadata,
	})
	return permissions.BuildManualApprovalRequiredResponse(routeReason(route), metadata, nil)
}

// domainRouteMetadata 构造领域路由的审计元数据。
//
// Python: AutoPermissionDomainGrantAuditMixin._domain_route_metadata(
// facts, route, *, final_status)
// 10 个固定字段；hide_always_allow 恒 true（领域规则不允许「始终允许」）。
func domainRouteMetadata(
	facts permissions.ToolDecisionFacts,
	route *permissions.DecisionRoute,
	finalStatus string,
) map[string]any {
	reason := routeReason(route)
	source := ""
	if route != nil {
		source = route.Source
	}
	return map[string]any{
		"action_summary":        reviewerActionSummary(facts),
		"decision_source":       hostRouteDecisionSource,
		"host_route_reason":     reason,
		"host_route_source":     source,
		"final_reviewer_status": finalStatus,
		"hide_always_allow":     true,
		"manual_reason_code":    reason,
		"permission_status":     finalStatus,
		"reviewer_status":       finalStatus,
		"risk_tier":             facts.Capability.RiskTier,
	}
}

// deny 构造通用拒绝响应。
//
// Python: AutoPermissionDomainGrantAuditMixin._deny(facts, reason, *, decision_source)
func (r *AutoPermissionRail) deny(
	facts permissions.ToolDecisionFacts,
	reason string,
	decisionSource string,
) map[string]any {
	metadata := map[string]any{
		"action_summary":        reviewerActionSummary(facts),
		"decision_source":       decisionSource,
		"final_reviewer_status": domainFinalStatusDenied,
		"reviewer_status":       domainFinalStatusDenied,
		"risk_tier":             facts.Capability.RiskTier,
	}
	r.emitAudit(facts, auditRecord{
		Decision: permissions.AutoDecisionDenyLevel,
		Reason:   reason,
		Degraded: false,
	})
	return deniedPermissionResponse(reason, metadata)
}

// routeReason 安全取路由原因（nil 路由返回空串）。
func routeReason(route *permissions.DecisionRoute) string {
	if route == nil {
		return ""
	}
	return route.Reason
}
