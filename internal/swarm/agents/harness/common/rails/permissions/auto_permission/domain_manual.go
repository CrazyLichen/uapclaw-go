// 人工审批与拒绝路径共用的紧凑响应辅助。
//
// 对应 Python 代码：_auto_permission/domain_manual.py（43 行 / 2 函数）
//
// 这两个函数产出的形状是「权限卡的应答载荷」，被 deny_response /
// manual_authorization 两条路径共同消费——放在独立文件是为了避免两者互相依赖。

package auto_permission

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultPermissionDeniedReason 原因为空时的兜底原因码
	//
	// Python: "permission_denied"
	defaultPermissionDeniedReason = "permission_denied"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// deniedPermissionResponse 构造标准拒绝响应载荷。
//
// Python: _denied_permission_response(reason, metadata)
// 9 个固定字段；metadata 作为 reviewer_metadata 挂入（深拷贝，不共享引用）。
func deniedPermissionResponse(reason string, metadata map[string]any) map[string]any {
	reasonText := strings.TrimSpace(reason)
	if reasonText == "" {
		reasonText = defaultPermissionDeniedReason
	}
	reviewerMetadata := make(map[string]any, len(metadata))
	for key, value := range metadata {
		reviewerMetadata[key] = value
	}
	return map[string]any{
		"approved":            false,
		"auto_confirm":        false,
		"feedback":            "[PERMISSION_DENIED] " + reasonText,
		"reason":              reasonText,
		"success":             false,
		"status":              "denied",
		"permission_decision": "deny",
		"permission_status":   "denied",
		"reviewer_metadata":   reviewerMetadata,
	}
}

// permissionPromptPayload 构造权限卡的提示载荷。
//
// Python: _permission_prompt_payload(facts, *, tool_call_id)
// tool_call_id 为空时不写入该键（Python 用 if tool_call_id 判定）。
func permissionPromptPayload(
	facts permissions.ToolDecisionFacts,
	toolCallID string,
) map[string]any {
	toolArgs := make(map[string]any, len(facts.UntrustedArgs))
	for key, value := range facts.UntrustedArgs {
		toolArgs[key] = value
	}
	payload := map[string]any{
		"tool_name": facts.Capability.ToolName,
		"tool_args": toolArgs,
	}
	if toolCallID != "" {
		payload["tool_call_id"] = toolCallID
	}
	return payload
}
