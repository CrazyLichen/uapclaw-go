// 审查器判定为拒绝时的响应构造。
//
// 对应 Python 代码：_auto_permission/deny_response.py（86 行 / 2 方法）
// Python: class AutoPermissionDenyResponseMixin
//
// 两个方法：
//   - reviewerDenyResponse：归一 UI 元数据 → 发审计 → 构造拒绝响应 → 覆写 feedback
//   - recordReviewerSuccessMetadata：把审查器元数据写入 ctx.extra 供后续投影消费

package auto_permission

import (
	"strings"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// reviewerDenyReason 审查器拒绝的原因码
	reviewerDenyReason = "reviewer_deny"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// reviewerDenyResponse 构造审查器拒绝的响应载荷。
//
// Python: AutoPermissionDenyResponseMixin._reviewer_deny_response(
// facts, *, channel_kind, audit_extra)
// 返回 (响应载荷, 决策来源)。
//
// feedback 覆写规则：`[PERMISSION_DENIED] reviewer_deny{": 摘要"} {拒绝引导语}`
// ——摘要取自 reviewer_reason_summary 或 evidence_summary，为空时省略冒号段。
func (r *AutoPermissionRail) reviewerDenyResponse(
	facts permissions.ToolDecisionFacts,
	channelKind string,
	auditExtra map[string]any,
) (map[string]any, string) {
	metadata := make(map[string]any, len(auditExtra)+1)
	for key, value := range auditExtra {
		metadata[key] = value
	}
	metadata["channel_kind"] = normalizeChannelKind(channelKind)

	normalized := reviewerUIMetadata(facts, reviewerDenyReason, metadata)

	r.emitAudit(facts, auditRecord{
		Decision: permissions.AutoDecisionDenyLevel,
		Reason:   reviewerDenyReason,
		Degraded: false,
		Extra:    normalized,
	})

	response := deniedPermissionResponse(reviewerDenyReason, normalized)

	denialSummary := ""
	if text, ok := normalized["reviewer_reason_summary"].(string); ok {
		denialSummary = text
	}
	if denialSummary == "" {
		if text, ok := normalized["evidence_summary"].(string); ok {
			denialSummary = text
		}
	}
	denialSummary = strings.TrimSpace(denialSummary)

	summarySuffix := ""
	if denialSummary != "" {
		summarySuffix = ": " + denialSummary
	}
	response["feedback"] = "[PERMISSION_DENIED] " + reviewerDenyReason + summarySuffix +
		". " + ReviewerDenialGuidance
	return response, "auto_reviewer"
}

// recordReviewerSuccessMetadata 把审查器元数据写入上下文的 extra。
//
// Python: AutoPermissionDenyResponseMixin._record_reviewer_success_metadata(
// ctx, facts, *, tool_call_id, reason, metadata)
// ctx 为 nil 或其 extra 不可写时直接返回（对齐 Python 的 MutableMapping 判定）。
func recordReviewerSuccessMetadata(
	ctx *agentinterfaces.AgentCallbackContext,
	facts permissions.ToolDecisionFacts,
	toolCallID string,
	reason string,
	metadata map[string]any,
) {
	if ctx == nil {
		return
	}
	extra := ctx.Extra()
	if extra == nil {
		return
	}
	reviewerMetadata := reviewerUIMetadata(facts, reason, metadata)
	permissions.RecordReviewerToolResultMetadata(extra, toolCallID, reviewerMetadata)
}
