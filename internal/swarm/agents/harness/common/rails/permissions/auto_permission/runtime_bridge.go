// 权限判定结果到运行时行为的桥接。
//
// 对应 Python 代码：_auto_permission/runtime_bridge.py（316 行 / 1 mixin / 12 方法）
// Python: class AutoPermissionRuntimeBridgeMixin
//
// 职责：把权限轨给出的判定结果翻译成 openjiuwen 运行时能理解的动作：
//   - allow            → 原样返回，继续执行
//   - denied / 用户拒绝 → 跳过工具执行，把拒绝原因塞进 tool_result
//   - interrupt        → 抛中断，等人工审批
//
// ## 偏差 V-8：审计观测为 no-op 桩
//
// `emitAudit` / `emitPermissionResultObservation` 依赖 audit.py（9.90e 落地），
// 本批保留完整签名与全部调用点，函数体为空。9.90e 只需替换桩体。
//
// ## 偏差 V-9：`_reviewer_ui_metadata` 未落地（⤵️ Task 30）
//
// `normalizeRuntimePermissionResult` 的 user_rejection 分支需要
// reviewer_metadata.py 的 `_reviewer_ui_metadata`（Task 30 内容）。
// 本批该分支直接返回原 result，Task 30 落地后回填。

package auto_permission

import (
	"context"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	interruptrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// manualApprovalDecisionSource 人工审批的决策来源标识
	manualApprovalDecisionSource = "manual_approval"

	// baseRailUnavailableReason 父权限轨不可用时的原因码
	baseRailUnavailableReason = "base_permission_rail_unavailable"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// emitAudit 发出一条权限审计记录。
//
// Python: AutoPermissionRuntimeBridgeMixin._emit_audit
//
// ⤵️ 9.90e 回填：audit.py 本批不落地（D7 决策），此处为空实现。
// 保留完整签名与全部调用点（before_tool 16 处 + domain_grant_audit 3 处），
// 9.90e 只需把桩体换成真实 EmitPermissionAudit 调用。
func (r *AutoPermissionRail) emitAudit(facts permissions.ToolDecisionFacts, opts ...any) any {
	return nil // ⤵️ 9.90e 回填
}

// emitPermissionResultObservation 观测一次「等待人工」或「最终终态」。
//
// Python: AutoPermissionRuntimeBridgeMixin._emit_permission_result_observation
//
// ⤵️ 9.90e 回填：与 emitAudit 同批落地。
func (r *AutoPermissionRail) emitPermissionResultObservation(
	facts permissions.ToolDecisionFacts,
	result any,
	sessionID string,
	toolCallID string,
	decisionSource string,
	ctx *agentinterfaces.AgentCallbackContext,
) {
	// ⤵️ 9.90e 回填
}

// runtimeResult 把判定结果翻译成运行时动作并返回（可能改写）结果。
//
// Python: AutoPermissionRuntimeBridgeMixin._runtime_result
// 只有「运行时上下文」才做副作用；无上下文时原样返回。
func (r *AutoPermissionRail) runtimeResult(
	invocation ToolInvocation,
	result any,
) any {
	resultClass := permissions.ClassifyPermissionResult(result)
	isRuntimeContext := invocation.Ctx != nil
	if isRuntimeContext && resultClass == permissions.PermissionResultInterrupt {
		result = prepareManualPendingInvocation(invocation, result)
		resultClass = permissions.ClassifyPermissionResult(result)
	}
	if !isRuntimeContext {
		return result
	}
	switch resultClass {
	case permissions.PermissionResultAllow:
		return result
	case permissions.PermissionResultDenied, permissions.PermissionResultUserRejection:
		r.skipRuntimeTool(invocation, result)
		return result
	case permissions.PermissionResultInterrupt:
		// 中断由调用方处理（Go 用返回 error 而非 raise）
		return result
	}
	return result
}

// prepareManualPendingInvocation 把 tool_invocation_key 注入中断结果的
// value 与 metadata 两段。
//
// Python: AutoPermissionRuntimeBridgeMixin._prepare_manual_pending_invocation
func prepareManualPendingInvocation(invocation ToolInvocation, result any) any {
	key := permissions.ToolInvocationKeyFromContext(invocation.Ctx)
	if key == nil {
		return result
	}
	mapping, ok := result.(map[string]any)
	if !ok {
		return result
	}
	wireKey := key.ToWire()
	merged := make(map[string]any, len(mapping)+2)
	for name, value := range mapping {
		merged[name] = value
	}

	valueSection := map[string]any{}
	if existing, isMap := merged["value"].(map[string]any); isMap {
		for name, value := range existing {
			valueSection[name] = value
		}
	}
	valueSection["tool_invocation_key"] = wireKey
	merged["value"] = valueSection

	metadataSection := map[string]any{}
	if existing, isMap := merged["metadata"].(map[string]any); isMap {
		for name, value := range existing {
			metadataSection[name] = value
		}
	}
	metadataSection["tool_invocation_key"] = wireKey
	merged["metadata"] = metadataSection
	return merged
}

// normalizeRuntimePermissionResult 在观测前做运行时专属的终态转换。
//
// Python: AutoPermissionRuntimeBridgeMixin._normalize_runtime_permission_result
//
// ⤵️ Task 30 回填：`decision_source == "manual_approval"` 且结果为
// user_rejection 时，Python 会构造含 `_reviewer_ui_metadata` 的标准 denied
// payload。该函数属 reviewer_metadata.py（401 行，Task 30 内容），
// 本批直接返回原 result。
func normalizeRuntimePermissionResult(
	invocation ToolInvocation,
	result any,
	decisionSource string,
) any {
	// ⤵️ Task 30 回填 user_rejection → denied 的 payload 构造
	return result
}

// skipRuntimeTool 跳过工具执行，把拒绝结果写入上下文。
//
// Python: AutoPermissionRuntimeBridgeMixin._skip_runtime_tool
// Go 侧 BasePermissionRail 无 `_skip_tool` 钩子，直接走副作用式写入。
func (r *AutoPermissionRail) skipRuntimeTool(invocation ToolInvocation, result any) {
	ctx := invocation.Ctx
	if ctx == nil {
		return
	}
	toolResultText := permissionToolResultText(result)
	toolResult := permissionToolResultPayload(result, toolResultText)
	toolCallID := HostToolCallID(invocation.ToolCall)

	// 记录 reviewer 元数据供后续投影消费
	if mapping, ok := toolResult.(map[string]any); ok {
		reviewerMetadata, isMap := mapping["reviewer_metadata"].(map[string]any)
		if !isMap {
			reviewerMetadata, _ = mapping["reviewer"].(map[string]any)
		}
		if reviewerMetadata != nil {
			permissions.RecordReviewerToolResultMetadata(ctx.Extra(), toolCallID, reviewerMetadata)
		}
	}

	toolMessage := llmschema.NewToolMessage(toolCallID, toolResultText)
	ctx.SetExtra(skipToolExtraKey, true)
	if inputs, ok := ctx.Inputs().(*agentinterfaces.ToolCallInputs); ok {
		replaced := *inputs
		replaced.ToolResult = toolResult
		replaced.ToolMsg = toolMessage
		ctx.SetInputs(&replaced)
	}
}

// raiseRuntimeInterrupt 构造并抛出工具中断。
//
// Python: AutoPermissionRuntimeBridgeMixin._raise_runtime_interrupt
// Python 用 raise；Go 返回 error 由调用方上抛。
func (r *AutoPermissionRail) raiseRuntimeInterrupt(
	invocation ToolInvocation,
	result any,
) error {
	request := r.buildRuntimeInterruptRequest(invocation, result)
	if request == nil {
		return &callback.AbortError{Reason: "permission_interrupt_request_invalid"}
	}
	permissions.MarkPermissionInterruptRequest(invocation.Ctx, request)
	toolCall, _ := invocation.ToolCall.(*llmschema.ToolCall)
	return &callback.AbortError{
		Reason: "Tool execution interrupted: " + invocation.ToolName,
		Cause:  &saschema.ToolInterruptException{Request: request, ToolCall: toolCall},
	}
}

// buildRuntimeInterruptRequest 构造权限中断请求。
//
// Python: AutoPermissionRuntimeBridgeMixin._build_runtime_interrupt_request
func (r *AutoPermissionRail) buildRuntimeInterruptRequest(
	invocation ToolInvocation,
	result any,
) *saschema.InterruptRequest {
	extraFields := permissionInterruptExtraFields(result)

	autoManual := false
	if raw, exists := extraFields["auto_permission_manual"]; exists {
		autoManual = raw == true
		delete(extraFields, "auto_permission_manual")
	}

	requestMetadata := map[string]any{}
	if invocationKey, ok := extraFields["tool_invocation_key"].(map[string]any); ok {
		requestMetadata["resume_user_input_key"] = permissions.RootPermissionQueueUserInputKey
		copied := make(map[string]any, len(invocationKey))
		for key, value := range invocationKey {
			copied[key] = value
		}
		requestMetadata["tool_invocation_key"] = copied
	}
	if autoManual {
		requestMetadata["auto_permission_manual"] = true
	}

	autoConfirmKey := ""
	if !autoManual {
		autoConfirmKey = r.runtimeAutoConfirmKey(invocation)
	}
	request := &saschema.InterruptRequest{
		Message:        permissionInterruptMessage(result),
		PayloadSchema:  interruptrail.ConfirmPayloadSchema(),
		AutoConfirmKey: autoConfirmKey,
		Metadata:       requestMetadata,
		InterruptKind:  saschema.InterruptKindPermission,
	}
	// 其余 extra 字段并入 Metadata（Python 是 **extra_fields 展开）
	for key, value := range extraFields {
		request.Metadata[key] = value
	}

	if binder, ok := r.baseRail.(interface {
		BindApprovalRequest(saschema.InterruptRequester, *llmschema.ToolCall) error
	}); ok {
		var toolCall *llmschema.ToolCall
		if typed, isCall := invocation.ToolCall.(*llmschema.ToolCall); isCall {
			toolCall = typed
		}
		_ = binder.BindApprovalRequest(request, toolCall)
	}
	return request
}

// runtimeAutoConfirmKey 取自动确认键。
//
// Python: AutoPermissionRuntimeBridgeMixin._runtime_auto_confirm_key
// Go 侧 BasePermissionRail 无 `_get_auto_confirm_key` 钩子，返回空串
// （等价 Python 的 getattr 缺失分支）。
func (r *AutoPermissionRail) runtimeAutoConfirmKey(invocation ToolInvocation) string {
	if invocation.ToolCall == nil {
		return ""
	}
	if provider, ok := r.baseRail.(interface {
		AutoConfirmKey(toolCall any) string
	}); ok {
		return strings.TrimSpace(provider.AutoConfirmKey(invocation.ToolCall))
	}
	return ""
}

// callBaseRail 调用被包装权限轨的 BeforeToolCall。
//
// Python: AutoPermissionRuntimeBridgeMixin._call_base_rail
// Python 用 inspect.signature 适配调用形状；Go 签名静态固定，
// 直接以 (ctx, cbc) 调用（偏差 V-3）。
func (r *AutoPermissionRail) callBaseRail(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	invocation ToolInvocation,
) (any, error) {
	if r.baseRail == nil {
		return permissions.BuildManualApprovalRequiredResponse(
			baseRailUnavailableReason, nil, nil), nil
	}
	// BasePermissionRail.BeforeToolCall 只返回 error；判定结果通过
	// 「设 _skip_tool + 写 inputs.ToolResult」的副作用传递，
	// 由 resultFromBaseSideEffect 还原（与 Python 的 _skip_tool 语义一致）
	if err := r.baseRail.BeforeToolCall(ctx, cbc); err != nil {
		return nil, err
	}
	return resultFromBaseSideEffect(invocation.Ctx, nil), nil
}

// callBaseAfterRail 调用被包装权限轨的 AfterToolCall（若有）。
//
// Python: AutoPermissionRuntimeBridgeMixin._call_base_after_rail
func (r *AutoPermissionRail) callBaseAfterRail(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	invocation ToolInvocation,
) (any, error) {
	afterRail, ok := r.baseRail.(interface {
		AfterToolCall(context.Context, *agentinterfaces.AgentCallbackContext) (any, error)
	})
	if !ok {
		return nil, nil
	}
	return afterRail.AfterToolCall(ctx, cbc)
}
