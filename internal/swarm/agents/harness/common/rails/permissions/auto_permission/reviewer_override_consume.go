// 消费旧版 Core 的会话级自动确认标记。
//
// 对应 Python 代码：_auto_permission/reviewer_override_consume.py（84 行 / 2 方法）
// Python: class AutoPermissionReviewerOverrideConsumeMixin
//
// 职责：识别 session state 里由旧版 Core 写入的 auto_confirm 标记并消费它，
// **不扩大其适用范围**——资格判定比 Core 更严（见 sessionAutoConfirmEligible）。
//
// ## 偏差 V-15：`_is_auto_confirmed` 钩子缺失
//
// Python 用 `getattr(self.base_rail, "_is_auto_confirmed", None)` 调父类钩子；
// Go 的 `BasePermissionRail` 无此方法，改为本地严格判定（仅显式 `true` 算确认）。
// 已登记进 spec 的 9.90e 回填清单。

package auto_permission

import (
	"context"

	sessionstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sessionAutoConfirmDecisionSource 会话级自动确认的决策来源
	//
	// Python: "session_auto_confirm"
	sessionAutoConfirmDecisionSource = "session_auto_confirm"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// consumeSessionAutoConfirm 消费旧版 Core 的会话级自动确认。
//
// Python: AutoPermissionReviewerOverrideConsumeMixin._consume_session_auto_confirm(
// facts, *, invocation, user_input, domain_route)
// 返回 Handled=true 表示本次调用已被自动确认放行。
func (r *AutoPermissionRail) consumeSessionAutoConfirm(
	ctx context.Context,
	facts permissions.ToolDecisionFacts,
	invocation ToolInvocation,
	userInput any,
	domainRoute *permissions.DecisionRoute,
) *PermissionHandlingResult {
	if userInput != nil || !r.sessionAutoConfirmEligible(ctx, facts, invocation, domainRoute) {
		return notHandledResult()
	}
	key := r.runtimeAutoConfirmKey(invocation)
	if key == "" || invocation.Ctx == nil || invocation.Ctx.Session() == nil {
		return notHandledResult()
	}
	raw, err := invocation.Ctx.Session().GetState(
		sessionstate.StringKey(saschema.InterruptAutoConfirmKey))
	if err != nil || raw == nil {
		return notHandledResult()
	}
	config, ok := raw.(map[string]any)
	if !ok {
		return notHandledResult()
	}
	if !isAutoConfirmed(config, key) {
		return notHandledResult()
	}
	r.emitAudit(facts, auditRecord{
		Decision: permissions.AutoDecisionAllowLevel,
		Reason:   sessionAutoConfirmDecisionSource,
		Degraded: false,
		Extra:    map[string]any{"decision_source": sessionAutoConfirmDecisionSource},
	}) // ⤵️ 9.90e 回填桩体
	result, _ := NewPermissionHandlingResult(true, nil, sessionAutoConfirmDecisionSource)
	return result
}

// sessionAutoConfirmEligible 判定本次调用是否有资格消费会话级自动确认。
//
// Python: AutoPermissionReviewerOverrideConsumeMixin._session_auto_confirm_eligible(
// facts, *, invocation, domain_route)
//
// 9 个条件全部满足才成立——比旧版 Core 的判定更严，防止标记被滥用。
func (r *AutoPermissionRail) sessionAutoConfirmEligible(
	ctx context.Context,
	facts permissions.ToolDecisionFacts,
	invocation ToolInvocation,
	domainRoute *permissions.DecisionRoute,
) bool {
	if invocation.Ctx == nil || invocation.Ctx.Session() == nil {
		return false
	}
	if _, hasExternalSend := facts.Capability.StaticSideEffects[externalSendSideEffect]; hasExternalSend {
		return false
	}
	if !facts.ArgumentsValidObject {
		return false
	}
	if facts.Capability.FactsSource != "host_static" {
		return false
	}
	if facts.Capability.AliasConflict {
		return false
	}
	category := facts.Capability.Category
	if (category == "path" || category == "shell") && !facts.AccessesKnown {
		return false
	}
	if permissions.RequiresManualExecutionProviderReview(
		facts.Capability.ToolName, facts.Capability.Category) {
		return false
	}
	if domainRoute != nil && domainRoute.RequiresManual() {
		return false
	}
	if executionContext := permissions.CurrentRootDecisionContext(ctx); executionContext != nil &&
		executionContext.AutoReviewBlockReason != "" {
		return false
	}
	return r.runtimeAutoConfirmKey(invocation) != ""
}

// isAutoConfirmed 配置里该键是否被显式标记为自动确认。
//
// Python: base_rail._is_auto_confirmed(config, key)
// Go 侧无该钩子，改为严格判定：仅 bool true 算确认（更保守，见偏差 V-15）。
func isAutoConfirmed(config map[string]any, key string) bool {
	value, exists := config[key]
	if !exists {
		return false
	}
	boolean, ok := value.(bool)
	return ok && boolean
}

// notHandledResult 构造「未处理」结果。
func notHandledResult() *PermissionHandlingResult {
	result, _ := NewPermissionHandlingResult(false, nil, "")
	return result
}
