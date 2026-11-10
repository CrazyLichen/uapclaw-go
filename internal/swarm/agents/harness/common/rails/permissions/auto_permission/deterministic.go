// 确定性放行判定与路由包装。
//
// 对应 Python 代码：_auto_permission/deterministic.py（131 行 / 2 方法）
// Python: class AutoPermissionDeterministicMixin
//
// 两个方法：
//   - maybeAllowDeterministicSandboxScope：把「只读公开 Web 的近期搜索抓取」
//     这类宿主可证明的确定性场景直接放行，跳过审查器
//   - reviewerRoute：用本轨的 workspace_root / auto_options 包装顶层
//     reviewer_route 函数

package auto_permission

import (
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// deterministicAllowOptions maybeAllowDeterministicSandboxScope 的入参。
//
// Python 的该方法除 facts 外全是关键字参数，Go 用结构体承载。
type deterministicAllowOptions struct {
	// Route 当前路由
	Route *permissions.DecisionRoute
	// PolicyLevel 策略档位
	PolicyLevel string
	// SessionID 会话标识
	SessionID string
	// ToolCallID 工具调用标识
	ToolCallID string
	// RuntimeCtx 运行时上下文
	RuntimeCtx *agentinterfaces.AgentCallbackContext
	// DomainRoute 领域路由（非 nil 时不做确定性放行）
	DomainRoute *permissions.DecisionRoute
	// ChannelKind 通道类型
	ChannelKind string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultBoundedWriteMaxFiles 有界写入的默认文件数上限
	//
	// Python: auto_options.get("bounded_write_max_files", 3)
	defaultBoundedWriteMaxFiles = 3

	// deterministicEvidenceSummary 确定性放行的证据摘要文案
	//
	// Python: "Bounded public web evidence proved an exact recent-search fetch."
	deterministicEvidenceSummary = "Bounded public web evidence proved an exact recent-search fetch."

	// deterministicReasonSummary 确定性放行的原因摘要文案
	//
	// Python: "Readonly public web access is allowed by host deterministic
	// bounded-scope checks."
	deterministicReasonSummary = "Readonly public web access is allowed by host deterministic " +
		"bounded-scope checks."

	// deterministicReviewerLifecycle 确定性放行时的审查器生命周期标记
	deterministicReviewerLifecycle = "not_called"

	// deterministicReviewerStatus 确定性放行时的审查器状态
	deterministicReviewerStatus = "deterministic_allow"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// maybeAllowDeterministicSandboxScope 尝试确定性放行。
//
// Python: AutoPermissionDeterministicMixin._maybe_allow_deterministic_sandbox_scope(
// facts, *, route, policy_level, session_id, tool_call_id, runtime_ctx,
// domain_route, channel_kind)
//
// 放行条件：路由标记为确定性放行，且没有领域路由覆盖。
// 不满足时返回 Handled=false 的结果，调用方继续走审查器。
func (r *AutoPermissionRail) maybeAllowDeterministicSandboxScope(
	facts permissions.ToolDecisionFacts,
	opts deterministicAllowOptions,
) *PermissionHandlingResult {
	deterministicReason := ""
	if opts.Route != nil && opts.Route.Source == permissions.RecentFetchSource {
		deterministicReason = DeterministicReadonlyPublicWebReason
	}
	if deterministicReason == "" {
		result, _ := NewPermissionHandlingResult(false, nil, "")
		return result
	}
	if opts.DomainRoute != nil {
		result, _ := NewPermissionHandlingResult(false, nil, "")
		return result
	}

	decisionSource := DeterministicBoundedScopeDecisionSource
	metadata := map[string]any{
		"action_summary":  reviewerActionSummary(facts),
		"channel_kind":    opts.ChannelKind,
		"decision_source": decisionSource,
		"deterministic_public_web_fetch_bypassed_reviewer": true,
		"evidence_summary":        deterministicEvidenceSummary,
		"final_reviewer_status":   deterministicReviewerStatus,
		"policy_level":            opts.PolicyLevel,
		"reviewer_lifecycle":      deterministicReviewerLifecycle,
		"reviewer_outcome":        permissions.ReviewerOutcomeAllowOnce,
		"reviewer_reason_code":    deterministicReason,
		"reviewer_reason_summary": deterministicReasonSummary,
		"reviewer_status":         deterministicReviewerStatus,
	}
	for key, value := range ReviewerRouteAuditExtra(opts.Route, opts.PolicyLevel, boolPtr(false)) {
		metadata[key] = value
	}

	recordReviewerSuccessMetadata(
		opts.RuntimeCtx, facts, opts.ToolCallID, deterministicReason, metadata)

	r.emitAudit(facts, auditRecord{
		Decision: permissions.AutoDecisionAllowLevel,
		Reason:   deterministicReason,
		Degraded: false,
		Extra:    metadata,
	}) // ⤵️ 9.90e 回填桩体

	result, _ := NewPermissionHandlingResult(true, nil, decisionSource)
	return result
}

// reviewerRoute 用本轨的配置包装顶层 reviewer_route 函数。
//
// Python: AutoPermissionDeterministicMixin._reviewer_route(
// facts, *, policy_level, guard_result, original_user_intent,
// domain_route=None, recent_url_sources=())
func (r *AutoPermissionRail) reviewerRoute(
	facts permissions.ToolDecisionFacts,
	policyLevel string,
	guardResult string,
	originalUserIntent *permissions.OriginalUserIntentEvidence,
	domainRoute *permissions.DecisionRoute,
	recentURLSources []permissions.RecentURLSource,
) permissions.DecisionRoute {
	return permissions.ReviewerRoute(
		facts,
		policyLevel,
		guardResult,
		r.workspaceRoot,
		boundedWriteMaxFiles(r.autoOptions),
		boundedWriteExcludedPaths(r.autoOptions),
		originalUserIntent,
		domainRoute,
		recentURLSources,
	)
}

// boundedWriteMaxFiles 从 auto 选项取有界写入的文件数上限。
//
// Python: int(self.auto_options.get("bounded_write_max_files", 3))
func boundedWriteMaxFiles(autoOptions map[string]any) int {
	raw, exists := autoOptions["bounded_write_max_files"]
	if !exists {
		return defaultBoundedWriteMaxFiles
	}
	switch typed := raw.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	default:
		return defaultBoundedWriteMaxFiles
	}
}

// boundedWriteExcludedPaths 从 auto 选项取有界写入的排除路径。
//
// Python: tuple(self.auto_options.get("bounded_write_excluded_paths", ()))
func boundedWriteExcludedPaths(autoOptions map[string]any) []string {
	raw, exists := autoOptions["bounded_write_excluded_paths"]
	if !exists {
		return nil
	}
	switch typed := raw.(type) {
	case []string:
		return typed
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			if text, ok := item.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

// boolPtr 返回 bool 指针（供可选参数使用）。
func boolPtr(value bool) *bool {
	return &value
}
