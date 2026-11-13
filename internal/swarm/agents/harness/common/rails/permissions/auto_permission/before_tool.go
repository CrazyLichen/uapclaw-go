// 工具执行前的自动权限判定主入口（七步编排）。
//
// 对应 Python 代码：_auto_permission/before_tool.py（891 行）
//
// 本文件是自动权限层的**编排中心**：前面各文件提供的能力在这里按固定顺序串成
// 一条链，任一环节给出结论即短路返回。
//
// ## 核心约束（规则 9.3 / 计划 Task 41.2）
//
// `beforeToolCallImpl` 必须是**单方法**，内部两个闭包
// （`trustedSendGuardResult` / `runtimeResult`）保持内联原位，
// 步骤顺序与 Python 逐条对应，**不得拆成多个 helper**。
//
// ## 七步链概览
//
//	① 清理任务级执行域 + 提取调用 + 各类短路（plan 硬拒 / resume / ask_user / client tool）
//	② 可信 send 身份解析 + 参数修复/归一 + 解析会话/工具/请求/通道标识
//	③ 定义两个内联闭包（trustedSendGuardResult / runtimeResult）
//	④ 策略评估 → 构建 facts → 三个绑定校验 → trusted_search_producer 绑定
//	⑤ 策略层短路（contract error / deny / manual_required / fail_closed / 确定性护栏）
//	⑥ 意图约束 / 领域路由 / 绑定回退 / resume / policy allow / 各快速路径
//	⑦ 兜底：审查器 → 用户单次放行 → 低风险 → policy allow → base rail → 末级短路
//
// ## 结果回传：与 Python 一致走 cbc 副作用（非设计冲突）
//
// Python 的 `before_tool_call` 返回值只是透传给框架，**真正阻止工具执行靠
// `_skip_runtime_tool` 写的 ctx 副作用**（`_skip_tool` / `inputs.tool_result` /
// `inputs.tool_msg`）与 `_raise_runtime_interrupt` 抛出的中断。
// `_result_from_base_side_effect` 再反向从 cbc 把结果捞回来。
//
// Go 侧沿用同一套：`skipRuntimeTool` 写 cbc、`raiseRuntimeInterrupt` 返回
// AbortError、`resultFromBaseSideEffect` 反向读取。`BeforeToolCall` 只返回 error
// 与 Python 并不矛盾——返回值在两边都只是透传，语义由副作用承载。
// `manual_approval` 响应被 `ClassifyPermissionResult` 兜底判为 interrupt，
// 经 `permissionInterruptMessage` 读 `value.message` 后走同一中断链路。
//
// ## ⏵ 本批三处未落地前置件（均 fail-closed，见 spec §12.35）
//
// 三处前置件均已对齐（见 spec §12.36）：
//  1. `is_client_tool` → `foundationtool.IsClientTool`（ToolCard.Execution）
//  2. 六个 `SubagentXxxTool` → Go 侧三个 `sessions_*` 工具映射
//  3. `tool_permission_context` → 三个 ContextVar 在 `swarm/schema/permission.go`

package auto_permission

import (
	"context"
	"fmt"
	"reflect"
	"sync"

	foundationtool "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	security "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
	runpermissions "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/ctx"
	schemaswarm "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	runtimesandbox "github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// askUserToolName 直通父轨的询问工具名
	//
	// Python: ASK_USER_TOOL_NAME
	askUserToolName = "ask_user"

	// mcpFetchWebpageTool 公开 HTTPS 抓取标记针对的工具
	//
	// Python: "mcp_fetch_webpage"
	mcpFetchWebpageTool = "mcp_fetch_webpage"

	// defaultSearchMaxResults 搜索结果数默认值
	//
	// Python: DEFAULT_SEARCH_MAX_RESULTS
	defaultSearchMaxResults = 10

	// finalGateDecisionSource 终态守卫决策来源
	//
	// Python: "final_gate"
	finalGateDecisionSource = "final_gate"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// subagentRuntimeControlToolNames Go 侧会话控制工具闭集（V-22）
	//
	// Python: _SUBAGENT_RUNTIME_CONTROL_TYPES 的六个 subagent_* 名字。
	// Go 侧合并为三个 sessions_*（deep_agent.go:312 已登记）；
	// 同时保留六个上游名字，避免上游载荷直接带这些名字时被漏过。
	subagentRuntimeControlToolNames = map[string]struct{}{
		"sessions_spawn": {}, "sessions_list": {}, "sessions_cancel": {},
		"subagent_spawn": {}, "subagent_close": {}, "subagent_list": {},
		"subagent_resume": {}, "subagent_send_input": {}, "subagent_wait": {},
	}

	// subagentControlToolTypes 装配期登记的会话控制工具具体类型表。
	subagentControlToolTypesMu sync.RWMutex
	subagentControlToolTypes   = map[string]reflect.Type{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// SetSubagentRuntimeControlToolTypes 在装配期登记会话控制工具的具体类型。
//
// 与 SetReadonlyToolTypes / SetCodeControlToolTypes 同一设计（V-18）。
func SetSubagentRuntimeControlToolTypes(types map[string]reflect.Type) {
	subagentControlToolTypesMu.Lock()
	defer subagentControlToolTypesMu.Unlock()
	subagentControlToolTypes = make(map[string]reflect.Type, len(types))
	for name, toolType := range types {
		if toolType == nil {
			continue
		}
		subagentControlToolTypes[name] = toolType
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// BeforeToolCall 工具执行前的入口包装。
//
// Python: AutoPermissionBeforeToolMixin.before_tool_call(*args, **kwargs)
// 负责：设置/复位 NATIVE_PATH_ACCESS、plan 硬拒短路、permission_check_context
// 包裹、取消时清理权限队列中的活跃项。
//
// 偏差 V-19：Go 侧 NATIVE_PATH_ACCESS 是包级全局（非 ContextVar token），
// 复位用 defer 显式清 nil；ctx 取消对应 Python 的 asyncio.CancelledError。
// 见 spec §12.35。
func (r *AutoPermissionRail) BeforeToolCall(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	// Python: native_token = NATIVE_PATH_ACCESS.set(None) ... finally: reset(native_token)
	permissions.SetNativePathAccess(nil)
	defer permissions.SetNativePathAccess(nil)

	if permissions.PlanHardRejected(cbc) {
		return r.beforeToolCallImpl(ctx, cbc)
	}

	// Python: capture = getattr(base_rail, "permission_check_context", None)
	//         with context: return await impl(...)
	// BasePermissionRail.PermissionCheckContext 返回 cleanup（业务类），
	// 不返回派生 ctx（Python 的 nullcontext / contextmanager 等价）。
	if r.baseRail != nil {
		if done := r.baseRail.PermissionCheckContext(HostSessionID(cbc)); done != nil {
			defer done()
		}
	}
	if err := ctx.Err(); err != nil {
		// Python: except asyncio.CancelledError → queue.finish_active(key)
		key := permissions.ToolInvocationKeyFromContext(cbc)
		queue := permissions.OptionalRootPermissionQueue(ctx)
		if key != nil && queue != nil {
			queue.FinishActive(key)
		}
		return err
	}
	return r.beforeToolCallImpl(ctx, cbc)
}

// clientToolPermission 客户端工具按用户写的规则判定。
//
// Python: _client_tool_permission(args, kwargs, invocation)
// 「allow 且策略评估不 deny」时放行，其余交回父轨。
func (r *AutoPermissionRail) clientToolPermission(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
	invocation ToolInvocation,
) error {
	written := r.permissionConfig
	if r.writtenPermissions != nil {
		written = r.writtenPermissions()
	}
	overlayed, err := runpermissions.OverlayRunPermissions(ctx, written)
	if err != nil {
		overlayed = written
	}
	toolArgs := NormalizeInvocationToolArgs(invocation.ToolName, invocation.ToolArgs)
	level, _ := security.EvaluateTieredPolicy(overlayed, invocation.ToolName, toolArgs)
	if level.String() == permissions.AutoDecisionAllowLevel {
		evaluation := r.policyEvaluator.Evaluate(ctx, permissions.PolicyInvocation{
			ToolName: invocation.ToolName,
			ToolCall: invocation.ToolCall,
			ToolArgs: invocation.ToolArgs,
			Ctx:      cbc,
		}, false)
		if evaluation.Level != permissions.AutoDecisionDenyLevel {
			return nil
		}
	}
	_, callErr := r.callBaseRail(ctx, cbc, invocation)
	return callErr
}

// beforeToolCallImpl 七步编排主方法（单方法，两个闭包保持内联）。
//
// Python: AutoPermissionBeforeToolMixin._before_tool_call_impl(*args, **kwargs)
//
// ⚠️ 规则 9.3：本方法**不得拆分**。两个内联闭包
// （trustedSendGuardResult / runtimeResult）保持在 Python 原位，
// 步骤顺序逐条对应上游 891 行。
func (r *AutoPermissionRail) beforeToolCallImpl(
	ctx context.Context,
	cbc *agentinterfaces.AgentCallbackContext,
) error {
	// ① 清理任务级执行域（Python: 三个 clear_* 调用）
	ctx = runtimesandbox.ClearNoHostFallback(ctx)
	ctx = permissions.ClearSendFileExecutionGrant(ctx)
	ctx = permissions.ClearTrustedSearchProducer(ctx)
	_ = ctx // 派生 ctx 在本批无法回传（偏差 V-16），保留调用以对齐步骤

	invocation := ExtractInvocation(cbc, nil, map[string]any{})
	if permissions.PlanHardRejected(cbc) {
		return nil
	}
	wrapperResume := permissions.ConsumePermissionWrapperResume(cbc)

	// mcp_fetch_webpage 打公开 HTTPS 标记
	if cbc != nil && invocation.ToolName == mcpFetchWebpageTool {
		cbc.SetExtra(schemaswarm.PublicHTTPSFetchContextAttr, true)
	}
	// _skip_tool + 根上下文失败 → 短路
	if cbc != nil {
		if cbc.Extra()[skipToolExtraKey] == true &&
			permissions.RootContextFailureFromContext(cbc) != "" {
			return nil
		}
	}
	if permissions.RootNonPermissionResumeFromContext(cbc) != nil {
		return nil
	}
	if invocation.ToolName == askUserToolName {
		_, err := r.callBaseRail(ctx, cbc, invocation)
		return err
	}
	// 客户端工具短路（Go 侧无 is_client_tool，用能力卡标记判定）
	if cbc != nil && cbc.Agent() != nil && cbc.Agent().AbilityManager() != nil {
		if card, isCard := cbc.Agent().AbilityManager().Get(
			invocation.ToolName).(*foundationtool.ToolCard); isCard &&
			foundationtool.IsClientTool(card) {
			return r.clientToolPermission(ctx, cbc, invocation)
		}
	}
	invocationResume := permissions.RootPermissionResumeFromContext(cbc)

	// ② 可信 send 身份解析 + 参数修复/归一
	trustedSendResolution := ResolveTrustedSendIdentity(
		ctx, cbc, map[string]any{}, invocation)
	if trustedSendResolution != nil {
		invocation = trustedSendResolution.Invocation
	}
	originalArgsWereValidObject := argsWereValidJSONObject(invocation.ToolArgs)
	invocation = repairInvocationArgsForExecution(invocation)
	invocation, commandContractError := NormalizeCommandInvocationForExecution(
		ctx, invocation, map[string]any{})
	invocation, nativeContractError := NormalizeNativePathInvocationForExecution(
		ctx, invocation, map[string]any{}, r.workspaceRoot)
	if commandContractError == "" {
		commandContractError = nativeContractError
	}
	normalizedArgs := NormalizeInvocationToolArgs(invocation.ToolName, invocation.ToolArgs)

	// 解析会话/工具/请求/通道标识
	sessionID := HostSessionID(cbc)
	toolCallID := HostToolCallID(invocation.ToolCall)
	requestID := resolveRequestID(ctx, cbc, map[string]any{})
	channelKind := resolveChannelKind(ctx, cbc, map[string]any{})
	if trustedSendResolution != nil && trustedSendResolution.Identity != nil {
		identity := trustedSendResolution.Identity
		sessionID = identity.SessionID
		requestID = identity.RequestID
		toolCallID = identity.ToolCallID
		channelKind = identity.ChannelKind
	}

	userInput := resolveUserInput(cbc, toolCallID, map[string]any{})
	if invocationResume != nil {
		if invocationResume.Card.Key != nil {
			requestID = invocationResume.Card.Key.RequestID
		}
	}

	// 供两个内联闭包捕获（Python 闭包读取外层 facts / policy_evaluation）
	var currentFacts permissions.ToolDecisionFacts
	var policyEvaluation permissions.PolicyEvaluation

	// ③ 内联闭包一：trusted_send_guard_result
	// Python: def trusted_send_guard_result(result, *, session_id, tool_call_id)
	trustedSendGuardResult := func(result any, localSessionID, localToolCallID string) any {
		_ = runtimesandbox.ClearNoHostFallback(ctx)
		_ = permissions.ClearSendFileExecutionGrant(ctx)
		r.emitPermissionResultObservation(
			permissions.ToolDecisionFacts{}, result,
			localSessionID, localToolCallID,
			sendFileAuthorizationDecisionSource, cbc)
		if cbc == nil {
			return result
		}
		resultClass := permissions.ClassifyPermissionResult(result)
		if resultClass == permissions.PermissionResultInterrupt {
			return r.raiseRuntimeInterrupt(invocation, result)
		}
		if resultClass == permissions.PermissionResultDenied ||
			resultClass == permissions.PermissionResultUserRejection {
			r.skipRuntimeTool(invocation, result)
		}
		return result
	}

	// ③ 内联闭包二：runtime_result
	// Python: def runtime_result(result, *, decision_source, explicit_send_authorization=False)
	runtimeResult := func(result any, decisionSource string, explicitSendAuthorization bool) error {
		resultClass := permissions.ClassifyPermissionResult(result)
		if result == nil || resultClass == permissions.PermissionResultAllow {
			// no_host_fallback 标记（V-16：派生 ctx 不回传，保留调用对齐步骤）
			_ = runtimesandbox.RequireNoHostFallback(ctx)
		} else {
			_ = runtimesandbox.ClearNoHostFallback(ctx)
		}
		if result == nil || resultClass == permissions.PermissionResultAllow {
			grant, grantErr := sendFileExecutionGrantForAllow(ctx, currentFacts)
			if grantErr == "send_file_authorization_missing" && explicitSendAuthorization {
				authReason, derived := issueSendFileAuthorization(ctx, currentFacts)
				ctx = derived
				if authReason != "" {
					_ = runtimesandbox.ClearNoHostFallback(ctx)
					result = permissions.BuildManualApprovalRequiredResponse(authReason, nil, nil)
					decisionSource = sendFileAuthorizationDecisionSource
					grantErr = ""
				} else {
					grant, grantErr = sendFileExecutionGrantForAllow(ctx, currentFacts)
				}
			}
			if grantErr != "" {
				_ = runtimesandbox.ClearNoHostFallback(ctx)
				_ = permissions.ClearSendFileExecutionGrant(ctx)
				result = permissions.BuildDeniedPermissionResponse(grantErr)
				decisionSource = sendFileAuthorizationDecisionSource
			} else if grant == nil {
				_ = permissions.ClearSendFileExecutionGrant(ctx)
			}
		} else {
			_ = permissions.ClearSendFileExecutionGrant(ctx)
		}
		result = normalizeRuntimePermissionResult(
			invocation, result, currentFacts, decisionSource)
		if decisionSource == "policy_evaluator" &&
			policyEvaluation.Level == permissions.AutoDecisionAllowLevel &&
			permissions.ClassifyPermissionResult(result) == permissions.PermissionResultAllow {
			recordReviewerSuccessMetadata(cbc, currentFacts, toolCallID, "policy_allow",
				map[string]any{
					"decision_source":       "policy_evaluator",
					"reviewer_status":       "approved",
					"final_reviewer_status": "approved",
				})
		}
		if invocationResume != nil && decisionSource == manualApprovalDecisionSource &&
			permissions.ClassifyPermissionResult(result) == permissions.PermissionResultAllow {
			recordReviewerSuccessMetadata(cbc, currentFacts, toolCallID, "manual_approval",
				map[string]any{
					"decision_source":       manualApprovalDecisionSource,
					"reviewer_status":       "approved",
					"final_reviewer_status": "approved",
				})
		}
		r.emitPermissionResultObservation(currentFacts, result,
			sessionID, toolCallID, decisionSource, cbc)
		// Python 的 self._runtime_result 是返回值；Go 侧副作用已写入 cbc，
		// 用 error 表达中断（allow 返回 nil）
		finalClass := permissions.ClassifyPermissionResult(result)
		if finalClass == permissions.PermissionResultInterrupt {
			return r.raiseRuntimeInterrupt(invocation, result)
		}
		if finalClass == permissions.PermissionResultDenied ||
			finalClass == permissions.PermissionResultUserRejection {
			r.skipRuntimeTool(invocation, result)
		}
		return nil
	}

	// ④-a 拒绝确认的提前分支：先建 facts 再走 runtime_result
	if invocationResume != nil && IsRejectionConfirmation(userInput) {
		currentFacts = buildToolDecisionFactsForBeforeTool(
			r, invocation, normalizedArgs, originalArgsWereValidObject, nil, nil)
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionDenyLevel,
			Reason:   "user_rejected",
			Degraded: false,
		})
		return runtimeResult(
			permissions.BuildRejectedPermissionResponse("user_rejected"),
			manualApprovalDecisionSource, false)
	}

	policyEvaluation = r.policyEvaluator.Evaluate(ctx, permissions.PolicyInvocation{
		ToolName: invocation.ToolName,
		ToolCall: invocation.ToolCall,
		ToolArgs: invocation.ToolArgs,
		Ctx:      cbc,
	}, false)

	var sendPaths []string
	if policyEvaluation.PathResult != nil {
		sendPaths = policyEvaluation.PathResult.CanonicalPaths
	}
	currentFacts = buildToolDecisionFactsForBeforeTool(
		r, invocation, normalizedArgs, originalArgsWereValidObject,
		policyEvaluation.ExternalPaths, sendPaths)

	// 三个绑定校验
	subagentRuntimeControlVerified :=
		currentFacts.Capability.OperationFamily == "subagent_runtime_control" &&
			trustedSubagentRuntimeControlBinding(invocation)
	readonlyBindingVerified :=
		currentFacts.Capability.OperationFamily == "internal_readonly" &&
			trustedReadonlyBinding(invocation, sessionID, r.ownerAgent)

	// trusted send 描述符校验
	if trustedSendResolution != nil {
		if trustedSendResolution.Identity != nil &&
			!CompatibilitySendMatchesHost(
				map[string]any{}, invocation, trustedSendResolution.Identity) {
			trustedSendResolution.Error = "send_file_authorization_context_mismatch"
		}
		if trustedSendResolution.Error != "" {
			var result any
			if trustedSendResolution.Error == "send_file_authorization_context_missing" {
				result = permissions.BuildManualApprovalRequiredResponse(
					trustedSendResolution.Error, nil, nil)
			} else {
				result = permissions.BuildDeniedPermissionResponse(trustedSendResolution.Error)
			}
			return asError(trustedSendGuardResult(result, sessionID, toolCallID))
		}
	}

	// trusted_search_producer 绑定
	invocationKey := permissions.ToolInvocationKeyFromContext(cbc)
	if r.trustedSearchUrls != nil && invocationKey != nil &&
		(invocation.ToolName == "free_search" ||
			invocation.ToolName == "mcp_free_search" ||
			invocation.ToolName == "mcp_paid_search") {
		maxResults := defaultSearchMaxResults
		if raw, exists := currentFacts.UntrustedArgs["max_results"]; exists {
			if parsed, ok := intOfAny(raw); ok {
				maxResults = parsed
			}
		}
		ctx = permissions.BindTrustedSearchProducer(
			ctx, r.trustedSearchUrls, *invocationKey, invocation.ToolName, maxResults)
	}

	// ⑤ 策略层短路
	if commandContractError != "" {
		return runtimeResult(
			permissions.BuildDeniedPermissionResponse(commandContractError),
			finalGateDecisionSource, false)
	}
	if policyEvaluation.Level == permissions.AutoDecisionDenyLevel {
		return runtimeResult(
			r.deny(currentFacts, policyEvaluation.Reason, "policy_evaluator"),
			"policy_evaluator", false)
	}
	if policyEvaluation.ManualApprovalRequired {
		baseResult, err := r.callBaseRail(ctx, cbc, invocation)
		if err != nil {
			return err
		}
		return runtimeResult(baseResult, manualApprovalDecisionSource, false)
	}
	if policyEvaluation.Source == policySourceFailClosed {
		if invocationResume != nil ||
			policyEvaluation.Reason == "policy_config_refresh_failed" {
			return runtimeResult(
				permissions.BuildDeniedPermissionResponse(policyEvaluation.Reason),
				"policy_evaluator", false)
		}
		return runtimeResult(
			permissions.BuildManualApprovalRequiredResponse(
				policyEvaluation.Reason, nil, nil),
			"policy_evaluator", false)
	}
	if deterministic := permissions.DeterministicGuardRoute(currentFacts); deterministic != nil {
		return runtimeResult(
			r.deny(currentFacts, deterministic.Reason, "deterministic_guard"),
			"deterministic_guard", false)
	}

	// ⑥ 意图约束 / 领域路由 / 绑定回退
	originalUserIntent := permissions.OriginalUserIntent(ctx)
	pathPolicyRequiresApproval := policyEvaluation.PathResult != nil &&
		policyEvaluation.PathResult.Level == permissions.AutoDecisionAskLevel
	initialConfirmationPayload := ParseConfirmationPayload(userInput)
	if initialConfirmationPayload != nil &&
		containsRetiredTaskScopeConfirmation(initialConfirmationPayload) {
		return runtimeResult(
			r.deny(currentFacts, "task_scope_unsupported", manualApprovalDecisionSource),
			manualApprovalDecisionSource, false)
	}
	if IsFileDeliveryAction(currentFacts) &&
		HasUserFileDeliveryProhibition(
			permissions.FileDeliveryConstraintText(originalUserIntent)) {
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionDenyLevel,
			Reason:   ProhibitedFileDeliveryReason,
			Degraded: false,
			Extra:    map[string]any{"channel_kind": channelKind},
		})
		return runtimeResult(
			permissions.BuildDeniedPermissionResponse(ProhibitedFileDeliveryReason),
			"user_intent_constraint", false)
	}

	domainRoute := r.evaluateDomainRoute(currentFacts, originalUserIntent,
		r.recentURLSources(sessionID))
	if domainRoute != nil && domainRoute.IsHardBlock() {
		return runtimeResult(
			r.denyDomainRoute(currentFacts, domainRoute), domainPolicyDecisionSource, false)
	}
	if currentFacts.Capability.OperationFamily == "subagent_runtime_control" &&
		!subagentRuntimeControlVerified {
		domainRoute = &permissions.DecisionRoute{
			Level:  permissions.AutoDecisionAskLevel,
			Reason: "subagent_runtime_control_binding_unverified",
			Source: permissions.ManualReviewSource,
		}
	}
	if currentFacts.Capability.OperationFamily == "internal_readonly" &&
		!readonlyBindingVerified {
		domainRoute = &permissions.DecisionRoute{
			Level:  permissions.AutoDecisionAskLevel,
			Reason: "readonly_tool_binding_unverified",
			Source: permissions.ManualReviewSource,
		}
	}
	if valid, checked := permissions.SessionObservationArgsValid(
		invocation.ToolName, currentFacts.UntrustedArgs); checked && !valid {
		domainRoute = &permissions.DecisionRoute{
			Level:  permissions.AutoDecisionAskLevel,
			Reason: "readonly_tool_arguments_unverified",
			Source: permissions.ManualReviewSource,
		}
	}

	if IsRejectionConfirmation(userInput) && invocationResume == nil {
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionDenyLevel,
			Reason:   "tool_invocation_resume_required",
			Degraded: false,
		})
		return runtimeResult(
			permissions.BuildDeniedPermissionResponse(
				"tool_invocation_resume_required"),
			"root_permission_queue", false)
	}
	if wrapperResume {
		return runtimeResult(nil, "permission_wrapper_resume", false)
	}
	if invocationResume != nil {
		baseResult, err := r.callBaseRail(ctx, cbc, invocation)
		if err != nil {
			return err
		}
		return runtimeResult(baseResult, manualApprovalDecisionSource, true)
	}
	bindingUnverified := domainRoute != nil &&
		(domainRoute.Reason == "subagent_runtime_control_binding_unverified" ||
			domainRoute.Reason == "readonly_tool_binding_unverified" ||
			domainRoute.Reason == "readonly_tool_arguments_unverified")
	if policyEvaluation.Level == permissions.AutoDecisionAllowLevel && !bindingUnverified {
		return runtimeResult(nil, "policy_evaluator", true)
	}

	// ⑥ 内联闭包三：run_reviewer（Python 的 async def run_reviewer()）
	runReviewer := func() (*PermissionHandlingResult, context.Context) {
		return r.maybeRunReviewer(ctx, currentFacts, reviewerExecutionOptions{
			PolicyLevel:        policyEvaluation.Level,
			PolicyReason:       policyEvaluation.Reason,
			PolicySource:       policyEvaluation.Source,
			SessionID:          sessionID,
			RequestID:          requestID,
			ToolCallID:         toolCallID,
			UserInput:          userInput,
			OriginalUserIntent: originalUserIntent,
			ChannelKind:        channelKind,
			RuntimeCtx:         cbc,
		})
	}

	if permissions.RequiresManualExecutionProviderReview(
		currentFacts.Capability.ToolName, currentFacts.Capability.Category) {
		reviewerResult, _ := runReviewer()
		return runtimeResult(reviewerResult.Result, reviewerResult.DecisionSource, false)
	}

	sessionAutoConfirm := r.consumeSessionAutoConfirm(ctx, currentFacts,
		invocation, userInput, domainRoute)
	if sessionAutoConfirm.Handled {
		return runtimeResult(
			sessionAutoConfirm.Result, sessionAutoConfirm.DecisionSource, false)
	}

	if IsAllowOnceConfirmation(userInput) {
		allowOnceRequiresReviewer := false
		var allowOnceTerminalResult any
		if currentFacts.Capability.OperationFamily == "subagent_runtime_control" &&
			!subagentRuntimeControlVerified {
			allowOnceRequiresReviewer = false
			allowOnceTerminalResult = nil
		} else {
			allowOnceRequiresReviewer, allowOnceTerminalResult =
				r.allowOnceReviewerGateDecision(currentFacts,
					policyEvaluation.Level, originalUserIntent, domainRoute)
		}
		if allowOnceTerminalResult != nil {
			return runtimeResult(
				allowOnceTerminalResult, manualApprovalDecisionSource, false)
		}
		if !allowOnceRequiresReviewer {
			authReason, derived := issueSendFileAuthorization(ctx, currentFacts)
			ctx = derived
			if authReason != "" {
				return runtimeResult(
					permissions.BuildManualApprovalRequiredResponse(
						authReason, nil, nil),
					sendFileAuthorizationDecisionSource, false)
			}
			r.emitAudit(currentFacts, auditRecord{
				Decision: permissions.AutoDecisionAllowLevel,
				Reason:   "user_allow_once",
				Degraded: false,
			})
			return runtimeResult(nil, manualApprovalDecisionSource, true)
		}
	}

	if permissions.TerminalInternalRoute(
		currentFacts, subagentRuntimeControlVerified, readonlyBindingVerified) != nil {
		recordReviewerSuccessMetadata(cbc, currentFacts, toolCallID,
			"canonical_internal_action_allow", map[string]any{
				"decision_source":       "canonical_internal_action",
				"final_reviewer_status": deterministicReviewerStatus,
				"reviewer_lifecycle":    deterministicReviewerLifecycle,
				"reviewer_outcome":      permissions.ReviewerOutcomeAllowOnce,
				"reviewer_reason_code":  "canonical_internal_action_allow",
				"reviewer_status":       deterministicReviewerStatus,
			})
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionAllowLevel,
			Reason:   "canonical_internal_action_allow",
			Degraded: false,
		})
		return runtimeResult(nil, "canonical_internal_action", false)
	}
	if trustedTaskToolName(currentFacts) {
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionAllowLevel,
			Reason:   "task_tool_control_silent",
			Degraded: false,
		})
		return runtimeResult(nil, "task_tool_control_silent", false)
	}

	if domainRoute != nil {
		domainResult, _ := r.handleDomainRoute(ctx, currentFacts,
			handleDomainRouteOptions{
				DomainRoute:        domainRoute,
				PolicyLevel:        policyEvaluation.Level,
				SessionID:          sessionID,
				RequestID:          requestID,
				ToolCallID:         toolCallID,
				UserInput:          userInput,
				OriginalUserIntent: originalUserIntent,
				ChannelKind:        channelKind,
				RuntimeCtx:         cbc,
			})
		if domainResult.Handled {
			return runtimeResult(
				domainResult.Result, domainResult.DecisionSource,
				IsAllowOnceConfirmation(userInput))
		}
	}

	if IsFileDeliveryAction(currentFacts) &&
		policyEvaluation.Level == permissions.AutoDecisionAllowLevel {
		authReason, derived := issueSendFileAuthorization(ctx, currentFacts)
		ctx = derived
		if authReason != "" {
			return runtimeResult(
				permissions.BuildDeniedPermissionResponse(authReason),
				sendFileAuthorizationDecisionSource, false)
		}
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionAllowLevel,
			Reason:   "send_file_policy_allow",
			Degraded: false,
		})
		return runtimeResult(nil, "policy_evaluator", false)
	}

	codeRoute := TrustedCodeRoute(
		invocation, currentFacts, pathPolicyRequiresApproval, r.ownerAgent)
	if codeRoute != "" {
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionAllowLevel,
			Reason:   codeRoute,
			Degraded: false,
		})
		return runtimeResult(nil, codeRoute, false)
	}

	// ⑦ 兜底链
	reviewerResult, _ := runReviewer()
	if reviewerResult.Handled {
		return runtimeResult(
			reviewerResult.Result, reviewerResult.DecisionSource, false)
	}
	if IsAllowOnceConfirmation(userInput) {
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionAllowLevel,
			Reason:   "user_allow_once",
			Degraded: false,
		})
		return runtimeResult(nil, manualApprovalDecisionSource, true)
	}
	if !pathPolicyRequiresApproval &&
		permissions.TerminalLowRiskRoute(currentFacts) != nil {
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionAllowLevel,
			Reason:   "terminal_low_risk_allow",
			Degraded: false,
		})
		return runtimeResult(nil, "deterministic_guard", false)
	}
	if policyEvaluation.Level == permissions.AutoDecisionAllowLevel {
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionAllowLevel,
			Reason:   "policy_allow",
			Degraded: false,
		})
		return runtimeResult(nil, "policy_evaluator", false)
	}

	baseResult, err := r.callBaseRail(ctx, cbc, invocation)
	if err != nil {
		return err
	}
	resultClass := permissions.ClassifyPermissionResult(baseResult)
	if resultClass == permissions.PermissionResultUserRejection {
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionDenyLevel,
			Reason:   "user_rejected",
			Degraded: false,
		})
		return runtimeResult(baseResult, manualApprovalDecisionSource, false)
	}
	if resultClass == permissions.PermissionResultInterrupt ||
		resultClass == permissions.PermissionResultDenied {
		r.emitAudit(currentFacts, auditRecord{
			Decision: resultClass,
			Reason:   "base_" + resultClass,
			Degraded: false,
		})
		return runtimeResult(baseResult, manualApprovalDecisionSource, false)
	}
	if pathPolicyRequiresApproval {
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionAskLevel,
			Reason:   policyEvaluation.PathResult.Reason,
			Degraded: true,
			Extra:    map[string]any{"decision_source": "file_guard_adapter"},
		})
		return runtimeResult(
			permissions.BuildManualApprovalRequiredResponse(
				policyEvaluation.PathResult.Reason, nil, nil),
			manualApprovalDecisionSource, false)
	}
	if postPolicy := permissions.PostPolicyRoute(
		currentFacts, permissions.AutoDecisionAllowLevel); postPolicy != nil &&
		postPolicy.Level == permissions.AutoDecisionAskLevel {
		r.emitAudit(currentFacts, auditRecord{
			Decision: permissions.AutoDecisionAskLevel,
			Reason:   postPolicy.Reason,
			Degraded: true,
		})
		return runtimeResult(
			permissions.BuildManualApprovalRequiredResponse(
				postPolicy.Reason, nil, nil),
			manualApprovalDecisionSource, false)
	}
	r.emitAudit(currentFacts, auditRecord{
		Decision: permissions.AutoDecisionAllowLevel,
		Reason:   "base_allow",
		Degraded: false,
	})
	return runtimeResult(baseResult, manualApprovalDecisionSource, true)
}

// ──────────────────────────── before_tool 局部辅助 ────────────────────────────
//
// 这些是 Python 模块级函数 / 常量在 Go 侧的等价物，仅供本文件的单方法使用，
// 不参与七步编排（规则 9.3 禁止拆的是「方法内步骤」，不是模块级常量）。

// buildToolDecisionFactsForBeforeTool 构造当前调用的判定事实。
//
// Python: build_tool_decision_facts(...)
func buildToolDecisionFactsForBeforeTool(
	r *AutoPermissionRail,
	invocation ToolInvocation,
	normalizedArgs map[string]any,
	originalArgsWereValidObject bool,
	externalPaths []string,
	sendPaths []string,
) permissions.ToolDecisionFacts {
	return permissions.BuildToolDecisionFacts(
		invocation.ToolName, normalizedArgs,
		r.workspaceRoot, r.platformTrustedRoot,
		originalArgsWereValidObject, externalPaths, sendPaths)
}

// trustedSubagentRuntimeControlBinding 校验子代理运行时控制工具的可执行绑定。
//
// Python: _trusted_subagent_runtime_control_binding(invocation)
//
// ## 命名偏差 V-22：六个上游工具名 → Go 侧三个 sessions_* 工具
//
// Python 用 `_SUBAGENT_RUNTIME_CONTROL_TYPES` 六个类对象常量
// （SubagentCloseTool / List / Resume / SendInput / Spawn / Wait），
// 再读 `resource._parent_agent` 校验归属。
//
// Go 侧在 `deep_agent.go:312` 已登记架构决策：这六项合并注册为
// `sessions_spawn` / `sessions_list` / `sessions_cancel` 三个工具，
// 且 `NewSessions*Tool` 把 provider 捕获进闭包、无导出访问器。
//
// 因此改为按 V-18 的装配期登记模式：`SetSubagentRuntimeControlToolTypes`
// 登记具体类型，`ResolveToolBinding` 做精确类型 + 卡片同一性校验。
// 未登记一律 false（fail-closed → 强制走人工审批）。
func trustedSubagentRuntimeControlBinding(invocation ToolInvocation) bool {
	if !isSubagentRuntimeControlTool(invocation.ToolName) {
		return false
	}
	if invocation.Ctx == nil || invocation.Ctx.Agent() == nil {
		return false
	}
	am := invocation.Ctx.Agent().AbilityManager()
	if am == nil {
		return false
	}
	expectedType := registeredSubagentControlToolType(invocation.ToolName)
	if expectedType == nil {
		return false
	}
	return ResolveToolBinding(am, invocation.ToolName, expectedType) != nil
}

// isSubagentRuntimeControlTool 是否属于 Go 侧的会话控制工具闭集。
func isSubagentRuntimeControlTool(toolName string) bool {
	_, exists := subagentRuntimeControlToolNames[toolName]
	return exists
}

// registeredSubagentControlToolType 取登记的具体类型，未登记返回 nil。
func registeredSubagentControlToolType(toolName string) reflect.Type {
	subagentControlToolTypesMu.RLock()
	defer subagentControlToolTypesMu.RUnlock()
	return subagentControlToolTypes[toolName]
}

// trustedTaskToolName 是否为宿主注册的、未走别名的 task_tool。
//
// Python: _trusted_task_tool_name(facts)
func trustedTaskToolName(facts permissions.ToolDecisionFacts) bool {
	if facts.Capability.ToolName != "task_tool" {
		return false
	}
	if facts.Capability.RegisteredName != facts.Capability.ToolName {
		return false
	}
	if len(facts.Capability.Aliases) > 0 || facts.Capability.AliasConflict {
		return false
	}
	if facts.Capability.FactsSource != "host_static" {
		return false
	}
	return facts.ArgumentsValidObject
}

// intOfAny 从任意值取 int（Python: int(x) 的保守等价）。
func intOfAny(value any) (int, bool) {
	switch typed := value.(type) {
	case int:
		return typed, true
	case float64:
		return int(typed), true
	case string:
		var parsed int
		_, err := fmt.Sscan(typed, &parsed)
		return parsed, err == nil
	default:
		return 0, false
	}
}

// asError 把 trusted_send_guard_result 的返回值转成 error。
//
// Python 该分支透传任意结果对象，但**副作用已在闭包内写完**
// （`_skip_runtime_tool` / `_raise_runtime_interrupt`）。Go 侧的
// `trustedSendGuardResult` 同样在闭包内调用了 `skipRuntimeTool` /
// `raiseRuntimeInterrupt`，故此处丢弃返回值是等价的，非偏差。
func asError(result any) error {
	if err, ok := result.(error); ok {
		return err
	}
	return nil
}
