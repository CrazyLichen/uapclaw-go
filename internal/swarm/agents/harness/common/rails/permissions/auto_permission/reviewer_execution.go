// 审查器执行编排：一次工具调用要走完的完整判定链。
//
// 对应 Python 代码：_auto_permission/reviewer_execution.py（389 行 / 1 方法）
// Python: class AutoPermissionReviewerExecutionMixin
//
// `maybeRunReviewer` 是自动权限层的**主入口**，把前面各文件的能力串成一条链：
//
//	路由 → 硬拦截 → 人工转交三选一 → 确定性放行 → 审查器调用
//	     → allow_once / deny / manual 三条结局分支
//
// 每个分支都以 PermissionHandlingResult 收尾：Handled=true 表示本层已给出结论，
// 调用方不再走后续判定。

package auto_permission

import (
	"context"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// reviewerExecutionOptions maybeRunReviewer 的入参。
//
// Python 的该方法除 facts 外全是关键字参数（13 个），Go 用结构体承载，
// 字段名与 Python 一一对应。
type reviewerExecutionOptions struct {
	// PolicyLevel 策略档位
	PolicyLevel string
	// PolicyReason 策略原因
	PolicyReason string
	// PolicySource 策略来源（默认 permission_engine）
	PolicySource string
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
	// DomainRoute 领域路由
	DomainRoute *permissions.DecisionRoute
	// GuardResult 护栏结果（默认 not_applicable）
	GuardResult string
	// ChannelKind 通道类型（默认 web）
	ChannelKind string
	// RuntimeCtx 运行时上下文
	RuntimeCtx *agentinterfaces.AgentCallbackContext
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultPolicySource 未指定策略来源时的默认值
	//
	// Python: policy_source="permission_engine"
	defaultPolicySource = "permission_engine"

	// defaultGuardResult 未指定护栏结果时的默认值
	//
	// Python: guard_result="not_applicable"
	defaultGuardResult = "not_applicable"

	// defaultChannelKind 未指定通道类型时的默认值
	//
	// Python: channel_kind="web"
	defaultChannelKindForExecution = "web"

	// policySourceFailClosed 策略不可用时的来源标记
	//
	// Python: policy_source == "fail_closed"
	policySourceFailClosed = "fail_closed"

	// intentContractDecisionSource 意图契约的决策来源
	//
	// Python: "intent_contract"
	intentContractDecisionSource = "intent_contract"

	// executionProviderContractDecisionSource 执行器契约的决策来源
	//
	// Python: "execution_provider_contract"
	executionProviderContractDecisionSource = "execution_provider_contract"

	// sendFileAuthorizationDecisionSource 文件授权失败的决策来源
	//
	// Python: "send_file_authorization"
	sendFileAuthorizationDecisionSource = "send_file_authorization"

	// reviewerUnavailableDecisionSource 审查器不可用时的决策来源
	//
	// Python: "reviewer_unavailable"
	reviewerUnavailableDecisionSource = "reviewer_unavailable"

	// reviewerManualReason 人工兜底的原因码
	//
	// Python: "reviewer_manual"
	reviewerManualReason = "reviewer_manual"

	// permissionReviewerAssessmentKind 审查器评估的记录类型
	//
	// Python: "permission_reviewer_assessment"
	permissionReviewerAssessmentKind = "permission_reviewer_assessment"

	// policyUnavailableSummary 策略不可用时的展示摘要
	//
	// Python: "The permission policy result is unavailable or malformed;
	// review this invocation manually."
	policyUnavailableSummary = "The permission policy result is unavailable or malformed; " +
		"review this invocation manually."

	// executionProviderSummary 执行器契约不可验证时的展示摘要
	//
	// Python: "Host cannot verify this tool's execution-provider contract;
	// review this invocation manually."
	executionProviderSummary = "Host cannot verify this tool's execution-provider contract; " +
		"review this invocation manually."

	// intentContractSummary 意图窗口超限时的展示摘要
	//
	// Python: "The trusted user-intent window exceeds the automatic review
	// capacity; review this invocation manually."
	intentContractSummary = "The trusted user-intent window exceeds the automatic " +
		"review capacity; review this invocation manually."
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// maybeRunReviewer 自动权限层主入口：走完完整判定链并给出结论。
//
// Python: AutoPermissionReviewerExecutionMixin._maybe_run_reviewer(
// facts, *, policy_level, policy_reason, policy_source="permission_engine",
// session_id, request_id, tool_call_id, user_input, original_user_intent,
// domain_route, guard_result="not_applicable", channel_kind="web",
// runtime_ctx=None) -> PermissionHandlingResult
//
// 返回的 ctx 可能因签发文件授权而派生（见 issueSendFileAuthorization 的偏差说明）。
func (r *AutoPermissionRail) maybeRunReviewer(
	ctx context.Context,
	facts permissions.ToolDecisionFacts,
	opts reviewerExecutionOptions,
) (*PermissionHandlingResult, context.Context) {
	policySource := opts.PolicySource
	if policySource == "" {
		policySource = defaultPolicySource
	}
	guardResult := opts.GuardResult
	if guardResult == "" {
		guardResult = defaultGuardResult
	}
	channelKind := opts.ChannelKind
	if channelKind == "" {
		channelKind = defaultChannelKindForExecution
	}

	// ① 路由
	route := r.reviewerRoute(
		facts, opts.PolicyLevel, guardResult,
		opts.OriginalUserIntent, opts.DomainRoute,
		r.recentURLSources(opts.SessionID),
	)

	// ② 硬拦截
	if route.IsHardBlock() {
		metadata := map[string]any{
			"action_summary": reviewerActionSummary(facts),
		}
		for key, value := range ReviewerRouteAuditExtra(&route, opts.PolicyLevel, boolPtr(false)) {
			metadata[key] = value
		}
		r.emitAudit(facts, auditRecord{
			Decision: permissions.AutoDecisionDenyLevel,
			Reason:   route.Reason,
			Degraded: false,
			Extra:    metadata,
		}) // ⤵️ 9.90e 回填桩体
		result, _ := NewPermissionHandlingResult(
			true, permissions.BuildDeniedPermissionResponse(route.Reason), "hard_guard")
		return result, ctx
	}

	// ③ 人工转交三选一
	manualHandoff := resolveManualHandoff(ctx, route, policySource, opts.PolicyReason)
	if manualHandoff != nil {
		metadata := map[string]any{
			"action_summary":        reviewerActionSummary(facts),
			"decision_source":       manualHandoff.Source,
			"final_reviewer_status": reviewerUIStatusManual,
			"manual_reason_code":    manualHandoff.Reason,
			"manual_reason_summary": manualHandoff.Summary,
			"reviewer_called":       false,
			"reviewer_status":       reviewerUIStatusManual,
		}
		for key, value := range ReviewerRouteAuditExtra(&route, opts.PolicyLevel, boolPtr(false)) {
			metadata[key] = value
		}
		response := r.manualReviewerResponse(facts, manualReviewerResponseOptions{
			Reason:               manualHandoff.Reason,
			Metadata:             metadata,
			SessionID:            opts.SessionID,
			RequestID:            opts.RequestID,
			ToolCallID:           opts.ToolCallID,
			IncludePromptPayload: true,
			ChannelKind:          channelKind,
		})
		result, _ := NewPermissionHandlingResult(true, response, manualHandoff.Source)
		return result, ctx
	}

	// ④ 确定性放行
	deterministic := r.maybeAllowDeterministicSandboxScope(facts, deterministicAllowOptions{
		Route:       &route,
		PolicyLevel: opts.PolicyLevel,
		SessionID:   opts.SessionID,
		ToolCallID:  opts.ToolCallID,
		RuntimeCtx:  opts.RuntimeCtx,
		DomainRoute: opts.DomainRoute,
		ChannelKind: channelKind,
	})
	if deterministic.Handled {
		return deterministic, ctx
	}

	// ⑤ 审查器不可用
	if r.autoReviewer == nil {
		return r.handleReviewerUnavailable(ctx, facts, opts, route, channelKind)
	}

	// ⑥ 调用审查器
	request := permissions.BuildReviewerActionView(
		facts,
		opts.PolicyLevel,
		opts.PolicyReason,
		route.AllowedOutcomes(),
		route.NoAutoAllowReason(),
		opts.OriginalUserIntent,
		opts.DomainRoute,
	)
	assessment := r.autoReviewer.Assess(ctx, request)

	reviewerCalled := request.PayloadError == ""
	auditExtra := map[string]any{
		"action_summary":          reviewerActionSummary(facts),
		"channel_kind":            channelKind,
		"decision_source":         "auto_reviewer",
		"evidence_summary":        assessment.ReasonSummary,
		"final_reviewer_status":   assessment.Status,
		"policy_level":            opts.PolicyLevel,
		"reviewer_called":         reviewerCalled,
		"reviewer_lifecycle":      assessment.Status,
		"reviewer_outcome":        assessment.Outcome,
		"reviewer_reason_code":    assessment.ReasonCode,
		"reviewer_reason_summary": assessment.ReasonSummary,
		"user_review_hint":        assessment.UserReviewHint,
	}
	for key, value := range ReviewerRouteAuditExtra(&route, opts.PolicyLevel, &reviewerCalled) {
		auditExtra[key] = value
	}
	if assessment.FallbackReason != "" {
		auditExtra["reviewer_fallback_reason"] = assessment.FallbackReason
	}
	auditExtra["record_kind"] = permissionReviewerAssessmentKind
	auditExtra["decision_source"] = "auto_reviewer"

	// ⑦ allow_once
	if assessment.Outcome == permissions.ReviewerOutcomeAllowOnce {
		for key, value := range ReviewerRouteAuditExtra(&route, opts.PolicyLevel, boolPtr(true)) {
			auditExtra[key] = value
		}
		auditExtra["action_summary"] = reviewerActionSummary(facts)

		authReason, derivedCtx := issueSendFileAuthorization(ctx, facts)
		ctx = derivedCtx
		if authReason != "" {
			return r.manualFromAuthorization(facts, opts, auditExtra, authReason, channelKind), ctx
		}
		recordReviewerSuccessMetadata(
			opts.RuntimeCtx, facts, opts.ToolCallID, "reviewer_allow_once", auditExtra)
		r.emitAudit(facts, auditRecord{
			Decision: permissions.AutoDecisionAllowLevel,
			Reason:   "reviewer_allow_once",
			Degraded: false,
			Extra:    auditExtra,
		}) // ⤵️ 9.90e 回填桩体

		decisionSource := "auto_reviewer"
		if IsAllowOnceConfirmation(opts.UserInput) {
			decisionSource = manualApprovalDecisionSource
		}
		result, _ := NewPermissionHandlingResult(true, nil, decisionSource)
		return result, ctx
	}

	// ⑧ deny
	if assessment.Outcome == permissions.ReviewerOutcomeDeny {
		response, decisionSource := r.reviewerDenyResponse(facts, channelKind, auditExtra)
		result, _ := NewPermissionHandlingResult(true, response, decisionSource)
		return result, ctx
	}

	// ⑨ 用户已单次放行（审查器给的是 manual，但用户确认过）
	if IsAllowOnceConfirmation(opts.UserInput) {
		authReason, derivedCtx := issueSendFileAuthorization(ctx, facts)
		ctx = derivedCtx
		if authReason != "" {
			return r.manualFromAuthorization(facts, opts, auditExtra, authReason, channelKind), ctx
		}
		recordReviewerSuccessMetadata(
			opts.RuntimeCtx, facts, opts.ToolCallID,
			"user_allow_once_after_reviewer_manual", auditExtra)
		r.emitAudit(facts, auditRecord{
			Decision: permissions.AutoDecisionAllowLevel,
			Reason:   "user_allow_once_after_reviewer_manual",
			Degraded: false,
			Extra:    auditExtra,
		}) // ⤵️ 9.90e 回填桩体
		result, _ := NewPermissionHandlingResult(true, nil, manualApprovalDecisionSource)
		return result, ctx
	}

	// ⑩ 人工兜底
	manualMetadata := make(map[string]any, len(auditExtra)+3)
	for key, value := range auditExtra {
		manualMetadata[key] = value
	}
	manualReasonCode := assessment.ManualReasonCode
	if manualReasonCode == "" {
		manualReasonCode = assessment.ReasonCode
	}
	if manualReasonCode == "" {
		manualReasonCode = reviewerManualReason
	}
	manualMetadata["manual_reason_code"] = manualReasonCode
	manualReasonSummary := assessment.ManualReasonSummary
	if manualReasonSummary == "" {
		manualReasonSummary = assessment.ReasonSummary
	}
	manualMetadata["manual_reason_summary"] = manualReasonSummary
	manualMetadata["user_review_hint"] = assessment.UserReviewHint

	manualMetadata, displayReason := withHostOwnedManualReviewDisplay(facts, &route, manualMetadata)

	reason := assessment.FallbackReason
	if reason == "" {
		reason = assessment.ReasonSummary
	}
	if reason == "" {
		reason = reviewerManualReason
	}
	response := r.manualReviewerResponse(facts, manualReviewerResponseOptions{
		Reason:               reason,
		DisplayReason:        displayReason,
		Metadata:             manualMetadata,
		SessionID:            opts.SessionID,
		RequestID:            opts.RequestID,
		ToolCallID:           opts.ToolCallID,
		IncludePromptPayload: true,
		ChannelKind:          channelKind,
	})
	result, _ := NewPermissionHandlingResult(true, response, "auto_reviewer")
	return result, ctx
}

// manualHandoffResult 人工转交的三元组。
type manualHandoffResult struct {
	// Reason 原因码
	Reason string
	// Source 决策来源
	Source string
	// Summary 展示摘要
	Summary string
}

// resolveManualHandoff 判定是否需要跳过审查器直接转人工。
//
// Python: _maybe_run_reviewer 里的 manual_handoff 三选一
// 顺序对齐：fail_closed → execution_provider_contract → intent_contract。
func resolveManualHandoff(
	ctx context.Context,
	route permissions.DecisionRoute,
	policySource string,
	policyReason string,
) *manualHandoffResult {
	if policySource == policySourceFailClosed {
		return &manualHandoffResult{
			Reason:  policyReason,
			Source:  "policy_evaluator",
			Summary: policyUnavailableSummary,
		}
	}
	if route.NoAutoAllowReason() == permissions.ExecutionProviderContractUnverified {
		return &manualHandoffResult{
			Reason:  permissions.ExecutionProviderContractUnverified,
			Source:  executionProviderContractDecisionSource,
			Summary: executionProviderSummary,
		}
	}
	executionContext := permissions.CurrentRootDecisionContext(ctx)
	if executionContext != nil && executionContext.AutoReviewBlockReason != "" {
		return &manualHandoffResult{
			Reason:  executionContext.AutoReviewBlockReason,
			Source:  intentContractDecisionSource,
			Summary: intentContractSummary,
		}
	}
	return nil
}

// handleReviewerUnavailable 审查器未配置时的兜底。
//
// Python: _maybe_run_reviewer 里 auto_reviewer is None 分支
// 若用户已单次放行则放行，否则转人工。
func (r *AutoPermissionRail) handleReviewerUnavailable(
	ctx context.Context,
	facts permissions.ToolDecisionFacts,
	opts reviewerExecutionOptions,
	route permissions.DecisionRoute,
	channelKind string,
) (*PermissionHandlingResult, context.Context) {
	metadata := map[string]any{
		"decision_source": reviewerUnavailableDecisionSource,
		"reviewer_status": reviewerUIStatusFallback,
	}
	for key, value := range ReviewerRouteAuditExtra(&route, opts.PolicyLevel, boolPtr(false)) {
		metadata[key] = value
	}

	if IsAllowOnceConfirmation(opts.UserInput) {
		authReason, derivedCtx := issueSendFileAuthorization(ctx, facts)
		ctx = derivedCtx
		if authReason != "" {
			return r.manualFromAuthorization(facts, opts, metadata, authReason, channelKind), ctx
		}
		recordReviewerSuccessMetadata(
			opts.RuntimeCtx, facts, opts.ToolCallID,
			"user_allow_once_after_reviewer_unavailable", metadata)
		r.emitAudit(facts, auditRecord{
			Decision: permissions.AutoDecisionAllowLevel,
			Reason:   "user_allow_once_after_reviewer_unavailable",
			Degraded: false,
			Extra:    metadata,
		}) // ⤵️ 9.90e 回填桩体
		result, _ := NewPermissionHandlingResult(true, nil, manualApprovalDecisionSource)
		return result, ctx
	}

	response := r.manualReviewerResponse(facts, manualReviewerResponseOptions{
		Reason:               reviewerUnavailableDecisionSource,
		Metadata:             metadata,
		SessionID:            opts.SessionID,
		RequestID:            opts.RequestID,
		ToolCallID:           opts.ToolCallID,
		IncludePromptPayload: true,
		ChannelKind:          channelKind,
	})
	result, _ := NewPermissionHandlingResult(true, response, reviewerUnavailableDecisionSource)
	return result, ctx
}

// manualFromAuthorization 文件授权失败时转人工。
//
// Python: 三处 `if authorization_error:` 分支的共用形态
func (r *AutoPermissionRail) manualFromAuthorization(
	facts permissions.ToolDecisionFacts,
	opts reviewerExecutionOptions,
	baseMetadata map[string]any,
	authReason string,
	channelKind string,
) *PermissionHandlingResult {
	metadata := make(map[string]any, len(baseMetadata)+4)
	for key, value := range baseMetadata {
		metadata[key] = value
	}
	metadata["decision_source"] = sendFileAuthorizationDecisionSource
	metadata["final_reviewer_status"] = reviewerUIStatusManual
	metadata["manual_reason_code"] = authReason
	metadata["reviewer_status"] = reviewerUIStatusManual

	response := r.manualReviewerResponse(facts, manualReviewerResponseOptions{
		Reason:               authReason,
		Metadata:             metadata,
		SessionID:            opts.SessionID,
		RequestID:            opts.RequestID,
		ToolCallID:           opts.ToolCallID,
		IncludePromptPayload: true,
		ChannelKind:          channelKind,
	})
	result, _ := NewPermissionHandlingResult(true, response, sendFileAuthorizationDecisionSource)
	return result
}
