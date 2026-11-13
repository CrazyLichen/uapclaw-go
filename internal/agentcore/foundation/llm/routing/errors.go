package routing

import (
	"fmt"
	"sort"
	"strings"
	"time"

	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RouterView 错误翻译层读取路由器的窄接口。Python 侧经 getattr 鸭子类型读
// router 的 deployments/model_group_id/strategy/state 公开属性（模块头注释
// "The router is read through its public attributes"）；Go 无鸭子类型，
// 显式窄接口等价（Python 侧无对应接口，属平台差异）。
// Python: intelli_router_errors.py:426 to_model_error 的 router 形参
type RouterView interface {
	// Deployments 部署列表（Python: router.deployments）
	Deployments() []*intelliroutercore.Deployment
	// ModelGroupID 业务模型组 ID（Python: router.model_group_id）
	ModelGroupID() string
	// PinnedRouteTag tag-filtered 路由固定的 tag；统一路由返回空串
	// （Python: router.strategy.fallback_tag，:337-340 pinned_route_tag）
	PinnedRouteTag() string
	// State 路由器状态（Python: router.state）
	State() *intelliroutercore.LocalRouterState
}

// candidate 一个候选 deployment 及其失败快照。
// Python: intelli_router_errors.py:375-408 _Candidate（frozen dataclass）
type candidate struct {
	// deployment 候选 deployment
	deployment *intelliroutercore.Deployment
	// failure 最近失败记录（可选）
	failure *RouteFailure
	// cooldownRemaining 冷却剩余秒数（可选中）
	cooldownRemaining *float64
	// position 组序 1-based 位置（可选）
	position *int
}

// deploymentID 候选 ID（Python: :383-384）。
func (c candidate) deploymentID() string {
	return c.deployment.ID
}

// category 无记录按 transient 处理：缺数据绝不升级为需要人工
// （Python: :387-389）。
func (c candidate) category() string {
	if c.failure != nil {
		return c.failure.Category
	}
	return CategoryTransient
}

// describe 候选描述（Python: :391-393）。
func (c candidate) describe() string {
	reason := "is temporarily unavailable"
	if c.failure != nil {
		reason = c.failure.Message
	}
	return fmt.Sprintf("model %s %s", labelDeployment(c.deployment), reason)
}

// toDict 候选结构化输出（Python: :395-408）。
func (c candidate) toDict() map[string]any {
	var statusCode *int
	var errorType, message string
	if c.failure != nil {
		statusCode = c.failure.StatusCode
		errorType = c.failure.ErrorType
		message = c.failure.Message
	} else {
		message = "is temporarily unavailable"
	}
	var position any
	if c.position != nil {
		position = *c.position
	}
	var cooldown any
	if c.cooldownRemaining != nil {
		cooldown = *c.cooldownRemaining
	}
	return map[string]any{
		"deployment_id":              c.deploymentID(),
		"model_name":                 c.deployment.ModelName,
		"provider":                   c.deployment.Provider,
		"endpoint":                   endpointOf(c.deployment),
		"position":                   position,
		"category":                   c.category(),
		"status_code":                statusCode,
		"error_type":                 errorType,
		"message":                    message,
		"cooldown_remaining_seconds": cooldown,
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToModelError 把 intelli_router 异常翻译为框架 BaseError。
// Python: intelli_router_errors.py:426-562 to_model_error
func ToModelError(exc error, router RouterView, modelName string, outputEmitted bool) *exception.BaseError {
	now := float64(time.Now().UnixMilli()) / 1000
	groupID := router.ModelGroupID()
	triggerID, trigger := recordException(groupID, exc, outputEmitted)
	tag := router.PinnedRouteTag()
	var candidates []candidate
	for _, d := range routingCandidates(router.Deployments(), tag, modelName) {
		candidates = append(candidates, candidate{
			deployment:        d,
			failure:           loadFailure(groupID, d.ID),
			cooldownRemaining: cooldownRemaining(router.State(), d.ID, now),
			position:          positionOf(router.Deployments(), d),
		})
	}
	// 触发本次错误的 deployment 排在最前，只报一个原因的消费方
	// （可靠性通知）才能报对。（Python: :447-452）
	failed := []candidate{}
	for _, c := range candidates {
		if c.cooldownRemaining != nil {
			failed = append(failed, c)
		}
	}
	// Python: key=lambda c: c.deployment_id != trigger_id（False=触发方在前）
	sort.SliceStable(failed, func(i, j int) bool {
		return failed[i].deploymentID() == triggerID && failed[j].deploymentID() != triggerID
	})
	usable := []candidate{}
	for _, c := range candidates {
		if c.cooldownRemaining == nil {
			usable = append(usable, c)
		}
	}
	var triggerDeployment *intelliroutercore.Deployment
	for _, d := range router.Deployments() {
		if d.ID == triggerID {
			triggerDeployment = d
			break
		}
	}
	// "*" 是"组内任意模型"的逻辑名：绝不展示给用户。（Python: :458-459）
	requested := tag
	if requested == "" && modelName != "" && modelName != "*" {
		requested = modelName
	}
	whatHappened := ""
	switch {
	case triggerDeployment != nil && trigger != nil:
		whatHappened = fmt.Sprintf("The model %s %s.", labelDeployment(triggerDeployment), trigger.Message)
	case requested != "":
		whatHappened = fmt.Sprintf("The model '%s' failed.", requested)
	default:
		whatHappened = "A model in the model group failed."
	}
	pinNote := ""
	if tag != "" {
		pinNote = fmt.Sprintf("This member is set to use '%s' only, so no other model was tried.", tag)
	}

	build := func(status exception.StatusCode, routeCase, reason, message string, retryAfter *float64) *exception.BaseError {
		var failures []map[string]any
		for _, c := range failed {
			failures = append(failures, c.toDict())
		}
		var usableList []map[string]any
		for _, c := range usable {
			var position any
			if c.position != nil {
				position = *c.position
			}
			usableList = append(usableList, map[string]any{
				"model_name": c.deployment.ModelName,
				"provider":   c.deployment.Provider,
				"endpoint":   endpointOf(c.deployment),
				"position":   position,
			})
		}
		var statusCode *int
		if trigger != nil {
			statusCode = trigger.StatusCode
		}
		var retryAfterValue any
		shouldRetry := false
		if retryAfter != nil {
			retryAfterValue = *retryAfter
			shouldRetry = true
		}
		var pinnedModel any
		if tag != "" {
			pinnedModel = tag
		}
		var groupIDValue any
		if groupID != "" {
			groupIDValue = groupID
		}
		// ``msg`` 覆盖技术模板：文本直接展示给用户。（Python: :492-493）
		return exception.BuildError(status,
			exception.WithMsg(message),
			exception.WithDetails(map[string]any{
				"route_case":           routeCase,
				"reason":               reason,
				"should_retry":         shouldRetry,
				"retry_after_seconds":  retryAfterValue,
				"user_action_required": routeCase == RouteCaseExhaustedUserAction,
				"output_emitted":       outputEmitted,
				"model_group_id":       groupIDValue,
				"pinned_model":         pinnedModel,
				"failures":             failures,
				// 路由器仍可去的方向；通知可据此说明切换到了什么
				"usable":      usableList,
				"status_code": statusCode,
			}),
			exception.WithCause(exc))
	}

	// 分支顺序与 Python 逐条对应（:495-562）
	if len(candidates) == 0 {
		if requested == "" {
			return build(exception.StatusModelGroupNoAvailableRoute, RouteCaseExhaustedUserAction, ReasonTagNotFound,
				"The selected model group has no model. Please add a model to the model group.", nil)
		}
		return build(exception.StatusModelGroupNoAvailableRoute, RouteCaseExhaustedUserAction, ReasonTagNotFound,
			fmt.Sprintf("The model '%s' is not in the selected model group. "+
				"Please choose another model or add it back to the model group.", requested), nil)
	}

	if len(usable) > 0 && outputEmitted {
		return build(exception.StatusModelCallFailed, RouteCaseSwitchable, ReasonStreamInterrupted,
			sentences(whatHappened, "Retrying with another model in the group."), nil)
	}
	if len(usable) > 0 {
		category := CategoryTransient
		if trigger != nil {
			category = trigger.Category
		}
		delay := SwitchRetryDelaySeconds
		return build(exception.StatusModelCallFailed, RouteCaseSwitchable, category,
			sentences(whatHappened, "Switching to another model in the group."), &delay)
	}

	listed := ""
	for i, c := range candidates {
		if i > 0 {
			listed += "; "
		}
		listed += c.describe()
	}
	var overview string
	if len(candidates) == 1 {
		overview = fmt.Sprintf("The %s.", listed)
	} else {
		scope := "model in the group"
		if tag != "" {
			scope = fmt.Sprintf("model '%s'", tag)
		}
		overview = fmt.Sprintf("No %s is available: %s.", scope, listed)
	}

	if outputEmitted {
		fallback := "No other model in the group is available."
		if pinNote != "" {
			fallback = pinNote
		}
		return build(exception.StatusModelGroupNoAvailableRoute, RouteCaseExhaustedUserAction, ReasonStreamInterrupted,
			sentences(whatHappened, fallback), nil)
	}

	var transient, rejected []candidate
	for _, c := range candidates {
		switch c.category() {
		case CategoryTransient:
			transient = append(transient, c)
		case CategoryRequestRejected:
			rejected = append(rejected, c)
		}
	}
	if len(transient) == 0 && len(rejected) > 0 {
		// 请求本身被拒（如过长）。释放这些 deployment，让更小的请求
		// 重试可以立即使用它们。（Python: :535-543）
		for _, c := range rejected {
			releaseCooldown(router.State(), c.deploymentID())
		}
		return build(exception.StatusModelGroupNoAvailableRoute, RouteCaseExhaustedRequestRejected, CategoryRequestRejected,
			overview, nil)
	}
	if len(transient) == 0 {
		var failures []*RouteFailure
		for _, c := range candidates {
			failures = append(failures, c.failure)
		}
		return build(exception.StatusModelGroupNoAvailableRoute, RouteCaseExhaustedUserAction, ReasonAllDeterministic,
			sentences(overview, fixHint(failures), pinNote), nil)
	}
	allRepeated := true
	for _, c := range transient {
		if consecutiveFailures(router.State(), c.deploymentID()) < repeatedFailureThreshold {
			allRepeated = false
			break
		}
	}
	if allRepeated {
		return build(exception.StatusModelGroupNoAvailableRoute, RouteCaseExhaustedUserAction, ReasonTransientRepeated,
			sentences(overview, "Still unavailable after retrying.", pinNote), nil)
	}

	var waits []float64
	for _, c := range transient {
		if c.cooldownRemaining != nil {
			waits = append(waits, *c.cooldownRemaining)
		}
	}
	var wait float64
	if len(waits) > 0 {
		min := waits[0]
		for _, w := range waits[1:] {
			if w < min {
				min = w
			}
		}
		wait = min + cooldownMarginSeconds
	} else {
		wait = SwitchRetryDelaySeconds
	}
	retryAfter := wait
	if retryAfter < SwitchRetryDelaySeconds {
		retryAfter = SwitchRetryDelaySeconds
	}
	if retryAfter > maxRetryAfterSeconds {
		retryAfter = maxRetryAfterSeconds
	}
	return build(exception.StatusModelGroupNoAvailableRoute, RouteCaseExhaustedRetryable, ReasonHasTransient,
		sentences(overview, fmt.Sprintf("Retrying automatically in %.0f seconds.", retryAfter)), &retryAfter)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fixHint 从确定性失败归纳用户应检查什么（Python: :411-419 _fix_hint）。
func fixHint(failures []*RouteFailure) string {
	statusSet := map[int]struct{}{}
	for _, f := range failures {
		if f != nil && f.StatusCode != nil {
			statusSet[*f.StatusCode] = struct{}{}
		}
	}
	var hints []string
	if _, has401 := statusSet[401]; has401 {
		hints = append(hints, "check the API key configured for this model")
	}
	if _, has403 := statusSet[403]; has403 {
		hints = append(hints, "check the API key configured for this model")
	}
	if _, has404 := statusSet[404]; has404 {
		hints = append(hints, "check the model name and endpoint")
	}
	if len(hints) == 0 {
		return ""
	}
	joined := ""
	for i, h := range hints {
		if i > 0 {
			joined += " and "
		}
		joined += h
	}
	return fmt.Sprintf(" Please %s.", joined)
}

// sentences 以空格拼接非空片段（Python: :422-423 _sentences）。
func sentences(parts ...string) string {
	var out []string
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	result := ""
	for i, p := range out {
		if i > 0 {
			result += " "
		}
		result += p
	}
	return result
}
