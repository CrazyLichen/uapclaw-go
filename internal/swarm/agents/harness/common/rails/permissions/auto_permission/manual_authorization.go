// 需要人工审批时的响应构造。
//
// 对应 Python 代码：_auto_permission/manual_authorization.py（90 行 / 1 方法）
// Python: class AutoPermissionManualAuthorizationMixin
//
// 一条方法把「判定需要人工」翻译成权限卡应答：归一 UI 元数据 → 发审计 →
// 选可见原因（三级回落 + 本地化兜底）→ 构造 manual_approval_required 响应。

package auto_permission

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// manualReviewerResponseOptions manualReviewerResponse 的入参。
//
// Python 的该方法除 facts 外全是关键字参数（reason / display_reason /
// metadata / session_id / request_id / tool_call_id / include_prompt_payload /
// channel_kind），Go 用结构体承载，字段名一一对应。
type manualReviewerResponseOptions struct {
	// Reason 原因码
	Reason string
	// DisplayReason 宿主指定的展示原因（nil 表示未指定）
	DisplayReason *string
	// Metadata 输入元数据
	Metadata map[string]any
	// SessionID 会话标识
	SessionID string
	// RequestID 宿主请求标识
	RequestID string
	// ToolCallID 工具调用标识
	ToolCallID string
	// IncludePromptPayload 是否构造权限卡提示载荷
	IncludePromptPayload bool
	// ChannelKind 通道类型（空串时回落 metadata 里的值）
	ChannelKind string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// manualApprovalRequiredReason 人工审批可见原因的兜底原因码
	manualApprovalRequiredReason = "manual_approval_required"

	// permissionReviewerAssessmentRecordKind 审查器评估记录的 record_kind
	permissionReviewerAssessmentRecordKind = "permission_reviewer_assessment"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// manualReviewerResponse 构造「需要人工审批」的响应。
//
// Python: AutoPermissionManualAuthorizationMixin._manual_reviewer_response(
// facts, *, reason, display_reason=None, metadata, session_id=None,
// request_id=None, tool_call_id=None, include_prompt_payload=True,
// channel_kind=None)
//
// 顺序对齐 Python：
//  1. 归一 channel_kind（参数优先，回落 metadata）
//  2. metadata 补 channel_kind 与 record_kind
//  3. 需要时构造 prompt_payload
//  4. 有 session 且有 request+tool_call 时打 auto_permission_manual 标记
//  5. 归一 UI 元数据
//  6. 发审计（ask 档、degraded=true）
//  7. 选可见原因（display_reason → manual_reason_summary → evidence_summary → 本地化兜底）
//  8. 构造响应
func (r *AutoPermissionRail) manualReviewerResponse(
	facts permissions.ToolDecisionFacts,
	opts manualReviewerResponseOptions,
) map[string]any {
	channelKindSource := opts.ChannelKind
	if channelKindSource == "" {
		channelKindSource, _ = opts.Metadata["channel_kind"].(string)
	}
	resolvedChannelKind := normalizeChannelKind(channelKindSource)

	metadata := copyMetadata(opts.Metadata)
	metadata["channel_kind"] = resolvedChannelKind
	metadata["record_kind"] = permissionReviewerAssessmentRecordKind

	var promptPayload map[string]any
	if opts.IncludePromptPayload {
		promptPayload = permissionPromptPayload(facts, opts.ToolCallID)
	}
	if opts.IncludePromptPayload && opts.SessionID != "" {
		if opts.RequestID != "" && opts.ToolCallID != "" {
			metadata["auto_permission_manual"] = true
		}
	}

	normalized := reviewerUIMetadata(facts, opts.Reason, metadata)

	r.emitAudit(facts, auditRecord{
		Decision: permissions.AutoDecisionAskLevel,
		Reason:   opts.Reason,
		Degraded: true,
		Extra:    normalized,
	})

	visibleReason := ""
	if opts.DisplayReason != nil && strings.TrimSpace(*opts.DisplayReason) != "" {
		visibleReason = strings.TrimSpace(*opts.DisplayReason)
	}
	if visibleReason == "" {
		if text, ok := normalized["manual_reason_summary"].(string); ok && strings.TrimSpace(text) != "" {
			visibleReason = strings.TrimSpace(text)
		}
	}
	if visibleReason == "" {
		if text, ok := normalized["evidence_summary"].(string); ok && strings.TrimSpace(text) != "" {
			visibleReason = strings.TrimSpace(text)
		}
	}
	if visibleReason == "" {
		visibleReason = localizedHostDisplayReason(
			manualApprovalRequiredReason, normalized, reviewerUIStatusManual)
	}

	return permissions.BuildManualApprovalRequiredResponse(visibleReason, normalized, promptPayload)
}

// copyMetadata 浅拷贝元数据 map（Python 的 `{**metadata}` 等价）。
func copyMetadata(metadata map[string]any) map[string]any {
	out := make(map[string]any, len(metadata)+2)
	for key, value := range metadata {
		out[key] = value
	}
	return out
}
