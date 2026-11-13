package router

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	intellirouter "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter"
	intelliroutercore "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/core"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/health"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/observability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/intellirouter/strategy"
)

// ──────────────────────────── 常量 ────────────────────────────

// 路由参数与保留键。← reliable_router.py:59-68
var (
	// reservedRequestDefaultKeys deployment.request_defaults 的保留键
	reservedRequestDefaultKeys = map[string]bool{"model": true, "messages": true, "deployment": true, "stream": true}
	// routingInternalKeys 路由专用参数（review 9）：由 RoutingContext 承载、
	// 供策略消费，不属于 provider 请求体。合并 request_defaults 与
	// request_kwargs 时统一剔除，避免随请求发给上游（严格网关会对未知
	// 字段返回 400）。
	routingInternalKeys = map[string]bool{"session_id": true, "tags": true}
)

// ──────────────────────────── 结构体 ────────────────────────────

// ReliableRouter 可靠性路由器 - 集成状态管理，策略选择，健康检查。
// ← reliable_router.py:47 ReliableRouter
//
// 特性: API 池化 / 策略插拔 / 状态管理（失败计数、冷却）/ 健康检查 /
// 重试机制 / 流式支持。（py:50-56 原文）
type ReliableRouter struct {
	// BaseRouter 组合（Go 无继承；Python 本就是类继承，此处组合 +
	// 显式转发，不用 embedding——避免同名 State 字段遮蔽与虚方法
	// 派发陷阱）。← py:66 super().__init__(...)
	*BaseRouter

	// Strategy 策略 ← py:71-79（**导出契约**：上游
	// intelli_router_errors.pinned_route_tag 直接读 strategy）
	Strategy strategy.Strategy
	// CooldownTime 冷却基数（秒）← py:81
	CooldownTime float64
	// EventBus 可观测事件总线 ← py:90
	EventBus *observability.EventBus
	// ModelGroupID 业务模型组 ID ← py:91
	ModelGroupID *string
	// HealthChecker 健康检查（可选，用于加速恢复）← py:96-101
	HealthChecker *health.SDKHealthChecker
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RouterParams ReliableRouter 构造参数（对齐 py:59-66 __init__ 签名）。
type RouterParams struct {
	// Deployments 部署列表
	Deployments []*intelliroutercore.Deployment
	// StrategyType 策略名（"simple-shuffle"/"ordered-failover"/"tag-filtered"；
	// 空则默认 simple-shuffle ← py:60 默认值）。偏差 D-1：其余五种
	// 上游不可达，不移植。
	StrategyType string
	// Strategy 自定义策略实例（优先于 StrategyType ← py:72-78）
	Strategy strategy.Strategy
	// StrategyKwargs 策略参数（weights/default_weight/fallback_tag）
	StrategyKwargs map[string]any
	// NumRetries 重试次数（0 → 默认 3 ← py:61）
	NumRetries int
	// Timeout 超时秒（0 → 默认 30 ← py:62）
	Timeout float64
	// CooldownTime 冷却基数秒（0 → 默认 60 ← py:63）
	CooldownTime float64
	// EnableHealthCheck 是否启用健康检查 ← py:64
	EnableHealthCheck bool
	// HealthCheckInterval 健康检查间隔 ← py:65
	HealthCheckInterval float64
	// EventBus 事件总线（nil 则新建 ← py:90）
	EventBus *observability.EventBus
	// ModelGroupID 模型组 ID ← py:66
	ModelGroupID string
	// HTTPClient 注入 HTTP 客户端（测试用）
	HTTPClient *http.Client
}

// NewReliableRouter 构造。← reliable_router.py:59-103 __init__
func NewReliableRouter(p RouterParams) *ReliableRouter {
	base := NewBaseRouter(p.Deployments, p.NumRetries, p.Timeout, p.HTTPClient)
	if p.NumRetries == 0 {
		base.NumRetries = 3 // ← py:61 num_retries: int = 3
	}
	if p.Timeout == 0 {
		base.Timeout = 30.0 // ← py:62 timeout: float = 30.0
	}
	cooldownTime := p.CooldownTime
	if cooldownTime == 0 {
		cooldownTime = 60.0 // ← py:63 cooldown_time: float = 60.0
	}

	// 状态管理 ← py:70（覆盖 BaseRouter.state，ReliableRouter 是唯一写入方）
	state := intelliroutercore.NewLocalRouterState()
	base.State = state

	// 策略 ← py:71-79
	var strat strategy.Strategy
	if p.Strategy != nil {
		strat = p.Strategy // ← py:73-74 策略实例优先
	} else {
		strategyType := p.StrategyType
		if strategyType == "" {
			strategyType = "simple-shuffle" // ← py:60 默认
		}
		var err error
		strat, err = createStrategy(strategyType, p.StrategyKwargs)
		if err != nil {
			// ← py:99-100 create_strategy 未知策略抛 ValueError；
			// Go 侧构造函数无 error 返回（对齐调用面），以 panic 表达
			// 编程错误（与 Python 未捕获 ValueError 等价的 fail-fast）
			panic(err)
		}
	}

	r := &ReliableRouter{
		BaseRouter:   base,
		Strategy:     strat,
		CooldownTime: cooldownTime,
		EventBus:     p.EventBus,
	}
	if r.EventBus == nil {
		r.EventBus = &observability.EventBus{} // ← py:90 event_bus or EventBus()
	}
	if p.ModelGroupID != "" {
		r.ModelGroupID = &p.ModelGroupID
	}

	// 同步 deployment 自带的初始状态（如配置中声明的 COOLDOWN）到 state，
	// 避免 deployment 对象与 state 两套状态并存且互不可见。← py:83-89
	for _, dep := range p.Deployments {
		if dep.Status == intelliroutercore.DeploymentStatusCooldown {
			state.DeploymentStatuses[dep.ID] = intelliroutercore.DeploymentStatusCooldown
			if dep.CooldownUntil != nil {
				state.CooldownUntil[dep.ID] = dep.CooldownUntil
			} else {
				until := nowSec() + r.CooldownTime // ← py:87-88
				state.CooldownUntil[dep.ID] = &until
			}
		}
	}
	// 把部署声明的 tpm/rpm 配额接入 state ← py:90-96
	r.syncQuotaState(p.Deployments)

	// 注入 _available_deployments_for_model 覆写钩子（py:206-212 review 16：
	// Python 子类覆写，Go 无虚方法派发，必须构造期注入）
	r.hookAvailableDeploymentsForModel = r.availableDeploymentsForModel

	// 健康检查（可选，用于加速恢复）← py:96-101
	if p.EnableHealthCheck {
		// ← py:97-101 SDKHealthChecker(deployments, state, check_interval)
		r.HealthChecker = health.NewSDKHealthChecker(p.Deployments, state, p.HealthCheckInterval, 0, nil, 0, p.HTTPClient)
	}
	return r
}

// Completion 可靠性 completion - 自动路由选择和重试。
// ← reliable_router.py:415-630 completion
func (r *ReliableRouter) Completion(ctx context.Context, model string, messages []map[string]any, deployment *intelliroutercore.Deployment, kwargs map[string]any) (map[string]any, error) {
	// ← py:424-426 显式指定部署：跳过选路与重试循环，直接转发
	if deployment != nil {
		return r.completionOnDeployment(ctx, model, messages, deployment, kwargs)
	}
	requestID := observability.NewRequestID()
	available := r.getAvailableDeployments(model)
	requestExtra := r.requestExtra()
	if len(available) == 0 {
		// 计数口径配对：先发 REQUEST_STARTED 使 total_requests +1，
		// 再发 EXHAUSTED（py:431-436 注释）
		r.EventBus.Emit(routingEvent(observability.EventTypeRequestStarted, requestID, model, requestExtra))
		msg := "No available deployments"
		r.EventBus.Emit(exhaustedEvent(requestID, model, nil, &msg, requestExtra))
		return nil, intellirouter.NewNoDeploymentAvailable(model, "No available deployments")
	}

	totalAttempts := minInt(r.NumRetries+1, len(available)) // ← py:440
	started := routingEvent(observability.EventTypeRequestStarted, requestID, model, requestExtra)
	started.TotalAttempts = totalAttempts
	r.EventBus.Emit(started)

	context := intelliroutercore.NewRoutingContext(model, messages, kwargs)
	var _ = 0 // depErr 使用共享 depError（见 helpers.go）
	var errors []depError
	overallStart := nowSec()
	var lastFailureMetadata map[string]any

	for attempt := 0; attempt < totalAttempts; attempt++ { // ← py:456 for attempt in range(total_attempts)
		selected := r.selectDeployment(available, context) // ← py:457
		if selected == nil {
			if len(errors) > 0 {
				break // ← py:459-460
			}
			// ← py:462-471 补发 EXHAUSTED
			msg := "No deployment matched routing strategy"
			r.EventBus.Emit(exhaustedEvent(requestID, model, nil, &msg, requestExtra))
			return nil, intellirouter.NewNoDeploymentAvailable(model, "No deployment matched routing strategy")
		}
		context.MarkAttempt(selected)
		// request_defaults 校验（含保留键时抛 ValueError）放在 try 内
		//（意见 C）：跳过 d1 由其他部署服务 ← py:473-481 注释
		requestKwargs, kwerr := r.requestKwargsForDeployment(selected, kwargs)
		startTime := nowSec()
		if kwerr == nil {
			actualModel := actualModelOf(model, selected)
			var response map[string]any
			response, kwerr = r.BaseRouter.Completion(ctx, actualModel, messages, selected, requestKwargs)
			if kwerr == nil {
				endTime := nowSec()
				latency := endTime - startTime
				// usage 按 0 处理（review 6，py:494-496）
				usage := usageFromResponse(response)
				tokens := intOfAny(usage["completion_tokens"])
				promptTokens := intOfAny(usage["prompt_tokens"])
				// router 层是 state 的唯一写入方；不再调 strategy.on_success ← py:499-501
				r.State.OnSuccess(selected.ID, latency, tokens)
				context.SetSuccess(selected, response)
				routeMetadata := r.routeMetadata(selected, map[string]any{"attempt": attempt + 1})
				ev := routingEvent(observability.EventTypeRequestSucceeded, requestID, model, routeMetadata)
				ev.DeploymentID = &selected.ID
				ev.Provider = &selected.Provider
				ev.Latency = &latency
				ev.PromptTokens = &promptTokens
				ev.CompletionTokens = &tokens
				total := promptTokens + tokens
				ev.TotalTokens = &total
				ev.Attempt = attempt + 1
				ev.TotalAttempts = totalAttempts
				r.EventBus.Emit(ev)
				return response, nil
			}
		}
		e := kwerr
		// ← py:523-529 失败记账：请求侧错误不 on_failure 不冷却（review 7）
		if !isRequestSideError(e) {
			r.State.OnFailure(selected.ID, e, r.CooldownTime)
		}
		context.SetFailure(e)
		errors = append(errors, depError{selected.ID, e.Error()})
		lastFailureMetadata = r.routeMetadata(selected, map[string]any{
			"attempt": attempt + 1, "fallback_reason": fallbackReasonFromError(e),
		})

		// ← py:531-543 strict fail-fast：non-fallbackable 错误直接抛
		if r.usesStrictFallbackErrors() && !isFallbackableError(e, false) {
			r.emitTerminalExhausted(requestID, model, overallStart, attempt+1, totalAttempts, e,
				firstNonNil(lastFailureMetadata, requestExtra))
			return nil, e
		}

		// ← py:545 剔除已失败部署
		available = removeDeployment(available, selected.ID)
		canRetry := attempt < totalAttempts-1 && len(available) > 0
		if canRetry {
			ev := routingEvent(observability.EventTypeRequestRetried, requestID, model,
				r.routeMetadata(selected, map[string]any{"fallback_reason": fallbackReasonFromError(e)}))
			ev.DeploymentID = &selected.ID
			ev.Provider = &selected.Provider
			ev.Attempt = attempt + 1
			ev.TotalAttempts = totalAttempts
			errType := fallbackReasonFromError(e)
			ev.ErrorType = &errType
			msg := e.Error()
			ev.ErrorMessage = &msg
			r.EventBus.Emit(ev)
		}
		if len(available) == 0 {
			break // ← py:559-560
		}
	}

	// ← py:562-573 终态事件 + AllDeploymentsFailed
	ev := exhaustedEvent(requestID, model, ptrFloat(nowSec()-overallStart), contextErrMsg(context),
		firstNonNil(lastFailureMetadata, requestExtra))
	ev.Attempt = len(errors)
	ev.TotalAttempts = totalAttempts
	if context.Error != nil {
		errType := fallbackReasonFromError(context.Error)
		ev.ErrorType = &errType
	}
	r.EventBus.Emit(ev)
	return nil, allDeploymentsFailed(model, errors)
}

// StreamCompletion 可靠性流式 completion - 自动路由选择和重试。
// ← reliable_router.py:730-1043 stream_completion（async generator → channel）
//
// Retry only happens before the first chunk is received. Once streaming
// starts, mid-stream errors are raised directly.（py:736-738 原文）
//
// 意见 B：首个可见 chunk 之前的 chunk（如 role-only 头部块）先进缓冲、
// 不产出——首个可见 token 前失败切部署时，新部署重发头部 chunk 会让消费者
// 收到重复的 role 块。（py:845-849 原文）
func (r *ReliableRouter) StreamCompletion(ctx context.Context, model string, messages []map[string]any, kwargs map[string]any) <-chan StreamChunk {
	out := make(chan StreamChunk)
	go func() {
		defer close(out)
		available := r.getAvailableDeployments(model)
		if len(available) == 0 {
			// 与 completion() 对齐：无可用部署也发 EXHAUSTED（py:741-757）。
			// 注意用 REQUEST_STARTED 而非 STREAM_STARTED——此路径连可用性
			// 检查都未通过，不应计入 streams。
			requestID := observability.NewRequestID()
			requestExtra := r.requestExtra()
			r.EventBus.Emit(routingEvent(observability.EventTypeRequestStarted, requestID, model, requestExtra))
			msg := "No available deployments"
			r.EventBus.Emit(exhaustedEvent(requestID, model, nil, &msg, requestExtra))
			out <- StreamChunk{Err: intellirouter.NewNoDeploymentAvailable(model, "No available deployments")}
			return
		}

		requestID := observability.NewRequestID()
		requestExtra := r.requestExtra()
		context := intelliroutercore.NewRoutingContext(model, messages, kwargs)
		totalAttempts := minInt(r.NumRetries+1, len(available))
		started := routingEvent(observability.EventTypeStreamStarted, requestID, model, requestExtra)
		started.TotalAttempts = totalAttempts
		r.EventBus.Emit(started)
		overallStart := nowSec()
		var _ = 0 // depErr 使用共享 depError（见 helpers.go）
		var errors []depError
		var lastFailureMetadata map[string]any
		// 意见 A：消费者中断可能发生在首个 chunk 之前，提前初始化 ← py:768-770
		var selected *intelliroutercore.Deployment
		chunkCount := 0

		for attempt := 0; attempt < totalAttempts; attempt++ { // ← py:772
			selected = r.selectDeployment(available, context)
			if selected == nil {
				if len(errors) > 0 {
					break
				}
				msg := "No deployment matched routing strategy"
				r.EventBus.Emit(exhaustedEvent(requestID, model, nil, &msg, requestExtra))
				out <- StreamChunk{Err: intellirouter.NewNoDeploymentAvailable(model, "No deployment matched routing strategy")}
				return
			}
			context.MarkAttempt(selected)
			visibleOutputStarted := false
			routeSelectedEmitted := false
			ttfb := 0.0

			requestKwargs, kwerr := r.requestKwargsForDeployment(selected, kwargs)
			if kwerr == nil {
				actualModel := actualModelOf(model, selected)
				startTime := nowSec()
				chunkCount = 0
				var streamUsage map[string]any
				var pendingChunks []map[string]any // 意见 B 缓冲（py:849）
				var streamErr error
				for sc := range r.BaseRouter.CompletionStream(ctx, actualModel, messages, selected, requestKwargs) {
					if sc.Err != nil {
						streamErr = sc.Err
						break
					}
					chunk := sc.Chunk
					chunkCount++
					if usage, ok := chunk["usage"].(map[string]any); ok && len(usage) > 0 {
						// 取最后一份非空 usage 作为整条流的用量（py:837-843）
						streamUsage = usage
					}
					if !visibleOutputStarted && hasVisibleStreamOutput(chunk) {
						visibleOutputStarted = true
						ttfb = nowSec() - startTime // ← py:856-857
						ev := routingEvent(observability.EventTypeStreamRouteSelected, requestID, model,
							r.routeMetadata(selected, map[string]any{"attempt": attempt + 1, "ttft": ttfb}))
						ev.DeploymentID = &selected.ID
						ev.Provider = &selected.Provider
						ev.Latency = &ttfb
						ev.Attempt = attempt + 1
						ev.TotalAttempts = totalAttempts
						r.EventBus.Emit(ev)
						routeSelectedEmitted = true
						// 意见 B：先放行缓冲的头部 chunk，再放行当前 chunk（py:869-871）
						for _, buffered := range pendingChunks {
							if !emitChunk(out, ctx, buffered) {
								return // 消费者中断
							}
						}
						pendingChunks = nil
						if !emitChunk(out, ctx, chunk) {
							return
						}
					} else if visibleOutputStarted {
						if !emitChunk(out, ctx, chunk) {
							return
						}
					} else {
						pendingChunks = append(pendingChunks, chunk)
					}
				}
				// 消费者中断检测（GeneratorExit/CancelledError 等价）
				if ctx.Err() != nil {
					r.emitStreamCancelled(requestID, model, selected, overallStart, chunkCount)
					return
				}
				if streamErr == nil {
					// 意见 B：流正常结束但从未出现可见 chunk：flush 缓冲（py:891-896）
					for _, buffered := range pendingChunks {
						if !emitChunk(out, ctx, buffered) {
							return
						}
					}
					pendingChunks = nil
					// 流结束后用真实数据更新 state（review 8，py:898-903）
					latency := nowSec() - startTime
					tokens := intOfAny(streamUsage["completion_tokens"])
					promptTokens := intOfAny(streamUsage["prompt_tokens"])
					r.State.OnSuccess(selected.ID, latency, tokens)
					context.SetSuccess(selected, nil)
					extra := r.routeMetadata(selected, map[string]any{"attempt": attempt + 1})
					if routeSelectedEmitted {
						extra["ttft"] = ttfb
					}
					ev := routingEvent(observability.EventTypeStreamSucceeded, requestID, model, extra)
					ev.DeploymentID = &selected.ID
					ev.Provider = &selected.Provider
					lat := nowSec() - overallStart
					ev.Latency = &lat
					ev.PromptTokens = &promptTokens
					ev.CompletionTokens = &tokens
					total := promptTokens + tokens
					ev.TotalTokens = &total
					ev.Attempt = attempt + 1
					ev.TotalAttempts = totalAttempts
					ev.ChunkCount = &chunkCount
					r.EventBus.Emit(ev)
					return // ← py:933
				}
				kwerr = streamErr
			}
			e := kwerr
			// 请求侧错误不计失败、不冷却（review 7，py:936-939）
			if !isRequestSideError(e) {
				r.State.OnFailure(selected.ID, e, r.CooldownTime)
			}
			if visibleOutputStarted {
				// 可见输出已交付，重试不安全直接抛：补 EXHAUSTED 终态
				//（py:940-953）
				r.emitTerminalExhausted(requestID, model, overallStart, attempt+1, totalAttempts, e,
					r.routeMetadata(selected, map[string]any{"fallback_reason": fallbackReasonFromError(e)}))
				out <- StreamChunk{Err: e}
				return
			}
			if r.usesStrictFallbackErrors() && !isFallbackableError(e, false) {
				// strict fail-fast（py:954-966）
				r.emitTerminalExhausted(requestID, model, overallStart, attempt+1, totalAttempts, e,
					r.routeMetadata(selected, map[string]any{"fallback_reason": fallbackReasonFromError(e)}))
				out <- StreamChunk{Err: e}
				return
			}
			context.SetFailure(e)
			errors = append(errors, depError{selected.ID, e.Error()})
			lastFailureMetadata = r.routeMetadata(selected, map[string]any{
				"attempt": attempt + 1, "fallback_reason": fallbackReasonFromError(e),
			})
			available = removeDeployment(available, selected.ID)
			if attempt < totalAttempts-1 && len(available) > 0 {
				ev := routingEvent(observability.EventTypeRequestRetried, requestID, model,
					r.routeMetadata(selected, map[string]any{"fallback_reason": fallbackReasonFromError(e)}))
				ev.DeploymentID = &selected.ID
				ev.Provider = &selected.Provider
				ev.Attempt = attempt + 1
				ev.TotalAttempts = totalAttempts
				errType := fallbackReasonFromError(e)
				ev.ErrorType = &errType
				msg := e.Error()
				ev.ErrorMessage = &msg
				r.EventBus.Emit(ev)
			}
			if len(available) == 0 {
				break
			}
		}

		// ← py:1031-1042 终态 + AllDeploymentsFailed
		ev := exhaustedEvent(requestID, model, ptrFloat(nowSec()-overallStart), contextErrMsg(context),
			firstNonNil(lastFailureMetadata, requestExtra))
		ev.Attempt = len(errors)
		ev.TotalAttempts = totalAttempts
		if context.Error != nil {
			errType := fallbackReasonFromError(context.Error)
			ev.ErrorType = &errType
		}
		r.EventBus.Emit(ev)
		out <- StreamChunk{Err: allDeploymentsFailed(model, errors)}
	}()
	return out
}

// UpdateDeployments 更新部署列表（热替换）。← reliable_router.py:1063-1109
func (r *ReliableRouter) UpdateDeployments(newDeployments []*intelliroutercore.Deployment) {
	r.deploymentsLock.Lock()
	oldIDs := map[string]bool{}
	for _, dep := range r.Deployments {
		oldIDs[dep.ID] = true
	}
	newIDs := map[string]bool{}
	for _, dep := range newDeployments {
		newIDs[dep.ID] = true
	}
	var removedIDs []string
	for id := range oldIDs {
		if !newIDs[id] {
			removedIDs = append(removedIDs, id)
		}
	}

	r.Deployments = newDeployments
	r.buildModelIndices()
	// 仅清理本 router 之前管理、且已从新列表移除的部署状态——不能按新
	// 列表全集反向清理，多个 router 可能共享同一 state ← py:1079-1083
	if len(removedIDs) > 0 {
		for _, depID := range removedIDs {
			r.State.RemoveDeployment(depID)
		}
	}
	// 为新列表中的部署初始化 tpm/rpm 配额条目 ← py:1086-1088
	r.syncQuotaState(newDeployments)
	r.deploymentsLock.Unlock()

	if len(removedIDs) > 0 {
		// 意见 E：事件驱动清理订阅方统计键（py:1091-1105）。
		// RoutingEvent.model 必填，此事件传空串表示「路由器级/管理事件」。
		sort.Strings(removedIDs) // ← py:1103 sorted(removed_ids)
		ev := routingEvent(observability.EventTypeDeploymentsUpdated, observability.NewRequestID(), "",
			map[string]any{"removed_ids": removedIDs})
		r.emitEventWithoutAwait(ev)
	}
}

// Invoke 发送 chat completion 请求，返回类型化响应。
// ← reliable_router.py:1127-1186 invoke
//
// outputParser 可选：解析失败时调用方仍拿到原始 content（降级行为，
// py:1170-1176）。
func (r *ReliableRouter) Invoke(ctx context.Context, messages []map[string]any, opts InvokeParams) (*intellirouter.AssistantMessage, error) {
	params := buildParams(map[string]any{
		"tools": nilToNil(opts.Tools), "temperature": nilToNil(opts.Temperature),
		"top_p": nilToNil(opts.TopP), "max_tokens": nilToNil(opts.MaxTokens),
		"stop": nilToNil(opts.Stop),
	})
	if opts.Kwargs != nil {
		for k, v := range opts.Kwargs {
			if v != nil {
				params[k] = v
			}
		}
	}
	modelName, err := r.resolveModelName(opts.Model)
	if err != nil {
		return nil, err
	}
	raw, err := r.Completion(ctx, modelName, messages, nil, params)
	if err != nil {
		return nil, err
	}
	msg := responseToMessage(raw)
	deploymentID := ""
	if raw != nil {
		deploymentID, _ = raw["deployment_id"].(string)
	}
	if opts.OutputParser != nil && msg.Content != "" {
		parsed, perr := opts.OutputParser(msg)
		if perr == nil && parsed != nil {
			// ← py:1166-1168 dict → json dumps，否则 str()
			if m, ok := parsed.(map[string]any); ok {
				b, _ := json.Marshal(m)
				msg.Content = string(b)
			} else {
				msg.Content = fmt.Sprintf("%v", parsed)
			}
		} else if perr != nil {
			// 降级：记录告警不静默（py:1170-1176 语义），内容保留原文
			_ = deploymentID
		}
	}
	return msg, nil
}

// InvokeParams invoke 选项。← py:1129-1142 关键字参数面
type InvokeParams struct {
	// Tools 工具定义列表
	Tools []any
	// Temperature 采样温度
	Temperature *float64
	// TopP 核采样参数
	TopP *float64
	// Model 模型名（覆盖 deployment 配置）
	Model string
	// MaxTokens 最大生成 token 数
	MaxTokens *int
	// Stop 停止序列
	Stop any
	// OutputParser 可选输出解析器（py:1137 output_parser）
	OutputParser func(msg *intellirouter.AssistantMessage) (any, error)
	// Kwargs 透传给底层 completion 的额外参数
	Kwargs map[string]any
}

// Stream 流式 chat completion，逐 chunk 返回 AssistantMessageChunk。
// ← reliable_router.py:1188-1511 stream（async generator → channel）
func (r *ReliableRouter) Stream(ctx context.Context, messages []map[string]any, opts InvokeParams) <-chan ChunkResult {
	out := make(chan ChunkResult)
	go func() {
		defer close(out)
		params := buildParams(map[string]any{
			"tools": nilToNil(opts.Tools), "temperature": nilToNil(opts.Temperature),
			"top_p": nilToNil(opts.TopP), "max_tokens": nilToNil(opts.MaxTokens),
			"stop": nilToNil(opts.Stop),
		})
		if opts.Kwargs != nil {
			for k, v := range opts.Kwargs {
				if v != nil {
					params[k] = v
				}
			}
		}
		modelName, err := r.resolveModelName(opts.Model)
		if err != nil {
			out <- ChunkResult{Err: err}
			return
		}
		available := r.getAvailableDeployments(modelName)
		if len(available) == 0 {
			// 计数口径配对（py:1206-1221）：REQUEST_STARTED 而非 STREAM_STARTED
			requestID := observability.NewRequestID()
			requestExtra := r.requestExtra()
			r.EventBus.Emit(routingEvent(observability.EventTypeRequestStarted, requestID, modelName, requestExtra))
			msg := "No available deployments"
			r.EventBus.Emit(exhaustedEvent(requestID, modelName, nil, &msg, requestExtra))
			out <- ChunkResult{Err: intellirouter.NewNoDeploymentAvailable(modelName, "No available deployments")}
			return
		}
		requestID := observability.NewRequestID()
		requestExtra := r.requestExtra()
		totalAttempts := minInt(r.NumRetries+1, len(available))
		started := routingEvent(observability.EventTypeStreamStarted, requestID, modelName, requestExtra)
		started.TotalAttempts = totalAttempts
		r.EventBus.Emit(started)
		overallStart := nowSec()
		var _ = 0 // depErr 使用共享 depError（见 helpers.go）
		var errors []depError
		var lastFailureMetadata map[string]any
		var lastFallbackReason *string
		var selected *intelliroutercore.Deployment
		chunkCount := 0

		for attempt := 0; attempt < totalAttempts; attempt++ {
			selected = r.selectDeployment(available, intelliroutercore.NewRoutingContext(modelName, messages, params))
			if selected == nil {
				if len(errors) > 0 {
					break
				}
				msg := "No deployment matched routing strategy"
				r.EventBus.Emit(exhaustedEvent(requestID, modelName, nil, &msg, requestExtra))
				out <- ChunkResult{Err: intellirouter.NewNoDeploymentAvailable(modelName, "No deployment matched routing strategy")}
				return
			}
			visibleOutputStarted := false
			routeSelectedEmitted := false
			var ttft *float64
			requestKwargs, kwerr := r.requestKwargsForDeployment(selected, params)
			if kwerr == nil {
				actualModel := actualModelOf(modelName, selected)
				chunkCount = 0
				streamStart := nowSec()
				var streamUsageMetadata *intellirouter.UsageMetadata
				var pendingChunks []intellirouter.AssistantMessageChunk // 意见 B（py:1297）
				var streamErr error
				for sc := range r.BaseRouter.CompletionStream(ctx, actualModel, messages, selected, requestKwargs) {
					if sc.Err != nil {
						streamErr = sc.Err
						break
					}
					parsed := chunkToAssistantMessageChunk(sc.Chunk)
					if parsed == nil {
						continue // ← py:1305-1306
					}
					chunkCount++
					if parsed.UsageMetadata != nil {
						// 取最后一份非空 usage_metadata（py:1310-1315）
						streamUsageMetadata = parsed.UsageMetadata
					}
					if parsed.Content != "" || parsed.ReasoningContent != nil || parsed.ToolCalls != nil {
						if !routeSelectedEmitted {
							t := nowSec() - streamStart // ← py:1319-1320 ttft
							ttft = &t
							extra := r.routeMetadata(selected, map[string]any{"attempt": attempt + 1})
							if lastFallbackReason != nil {
								extra["fallback_reason"] = *lastFallbackReason
							}
							extra["ttft"] = t
							ev := routingEvent(observability.EventTypeStreamRouteSelected, requestID, modelName, extra)
							ev.DeploymentID = &selected.ID
							ev.Provider = &selected.Provider
							ev.Latency = &t
							ev.Attempt = attempt + 1
							ev.TotalAttempts = totalAttempts
							r.EventBus.Emit(ev)
							routeSelectedEmitted = true
						}
						visibleOutputStarted = true
						// 意见 B：先放行缓冲，再放行当前 chunk（py:1342-1348）
						for _, buffered := range pendingChunks {
							if !emitTypedChunk(out, ctx, buffered) {
								return
							}
						}
						pendingChunks = nil
						if !emitTypedChunk(out, ctx, *parsed) {
							return
						}
					} else if visibleOutputStarted {
						if !emitTypedChunk(out, ctx, *parsed) {
							return
						}
					} else {
						pendingChunks = append(pendingChunks, *parsed)
					}
				}
				if ctx.Err() != nil {
					r.emitStreamCancelledTyped(requestID, modelName, selected, overallStart, chunkCount)
					return
				}
				if streamErr == nil {
					// 意见 B flush + review 8 真实数据记账（py:1356-1408）
					for _, buffered := range pendingChunks {
						if !emitTypedChunk(out, ctx, buffered) {
							return
						}
					}
					pendingChunks = nil
					latency := nowSec() - streamStart
					tokens := 0
					promptTokens := 0
					if streamUsageMetadata != nil {
						tokens = streamUsageMetadata.OutputTokens
						promptTokens = streamUsageMetadata.InputTokens
					}
					r.State.OnSuccess(selected.ID, latency, tokens)
					extra := r.routeMetadata(selected, map[string]any{"attempt": attempt + 1})
					if lastFallbackReason != nil {
						extra["fallback_reason"] = *lastFallbackReason
					}
					if ttft != nil {
						extra["ttft"] = *ttft
					}
					ev := routingEvent(observability.EventTypeStreamSucceeded, requestID, modelName, extra)
					ev.DeploymentID = &selected.ID
					ev.Provider = &selected.Provider
					lat := nowSec() - overallStart
					ev.Latency = &lat
					ev.PromptTokens = &promptTokens
					ev.CompletionTokens = &tokens
					total := promptTokens + tokens
					ev.TotalTokens = &total
					ev.Attempt = attempt + 1
					ev.TotalAttempts = totalAttempts
					ev.ChunkCount = &chunkCount
					r.EventBus.Emit(ev)
					return
				}
				kwerr = streamErr
			}
			e := kwerr
			if !isRequestSideError(e) {
				r.State.OnFailure(selected.ID, e, r.CooldownTime)
			}
			if visibleOutputStarted {
				// 可见输出已交付，重试不安全直接抛（py:1417-1429）
				r.emitTerminalExhausted(requestID, modelName, overallStart, attempt+1, totalAttempts, e,
					r.routeMetadata(selected, map[string]any{"fallback_reason": fallbackReasonFromError(e)}))
				out <- ChunkResult{Err: e}
				return
			}
			if r.usesStrictFallbackErrors() && !isFallbackableError(e, false) {
				r.emitTerminalExhausted(requestID, modelName, overallStart, attempt+1, totalAttempts, e,
					r.routeMetadata(selected, map[string]any{"fallback_reason": fallbackReasonFromError(e)}))
				out <- ChunkResult{Err: e}
				return
			}
			reason := fallbackReasonFromError(e)
			lastFallbackReason = &reason
			errors = append(errors, depError{selected.ID, e.Error()})
			lastFailureMetadata = r.routeMetadata(selected, map[string]any{
				"attempt": attempt + 1, "fallback_reason": reason,
			})
			available = removeDeployment(available, selected.ID)
			if attempt < totalAttempts-1 && len(available) > 0 {
				ev := routingEvent(observability.EventTypeRequestRetried, requestID, modelName,
					r.routeMetadata(selected, map[string]any{"fallback_reason": reason}))
				ev.DeploymentID = &selected.ID
				ev.Provider = &selected.Provider
				ev.Attempt = attempt + 1
				ev.TotalAttempts = totalAttempts
				ev.ErrorType = &reason
				msg := e.Error()
				ev.ErrorMessage = &msg
				r.EventBus.Emit(ev)
			}
			if len(available) == 0 {
				break
			}
		}

		// ← py:1490-1510 终态（error_type 用类名口径，error_message 取末条）
		ev := exhaustedEvent(requestID, modelName, ptrFloat(nowSec()-overallStart), nil,
			firstNonNil(lastFailureMetadata, requestExtra))
		ev.Attempt = len(errors)
		ev.TotalAttempts = totalAttempts
		if len(errors) > 0 && lastFallbackReason != nil {
			ev.ErrorType = lastFallbackReason
		}
		if len(errors) > 0 {
			ev.ErrorMessage = &errors[len(errors)-1].msg
		}
		r.EventBus.Emit(ev)
		out <- ChunkResult{Err: allDeploymentsFailed(modelName, errors)}
	}()
	return out
}

// ChunkResult 流式类型化 chunk 或错误。
type ChunkResult struct {
	Chunk intellirouter.AssistantMessageChunk
	Err   error
}

// BatchRequest 批量请求项（py:1050 每个 req 含 model/messages/**kwargs）。
type BatchRequest struct {
	// Model 模型名
	Model string
	// Messages 消息列表
	Messages []map[string]any
	// Kwargs 其他参数
	Kwargs map[string]any
}

// BatchCompletion 批量 completion。← reliable_router.py:1044-1062
// （asyncio.Semaphore(max_concurrent) → 有界并发 goroutine；
// gather(return_exceptions=True) → 结果含 error 不中断整批）
func (r *ReliableRouter) BatchCompletion(ctx context.Context, requests []BatchRequest, maxConcurrent int) []BatchResult {
	if maxConcurrent <= 0 {
		maxConcurrent = 10 // ← py:1045 max_concurrent: int = 10
	}
	results := make([]BatchResult, len(requests))
	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup
	for i, req := range requests {
		wg.Add(1)
		go func(i int, req BatchRequest) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			resp, err := r.Completion(ctx, req.Model, req.Messages, nil, req.Kwargs)
			results[i] = BatchResult{Response: resp, Err: err}
		}(i, req)
	}
	wg.Wait()
	return results
}

// BatchResult 单条批量结果（异常折叠为 Err，不中断整批 ← py:1059 return_exceptions=True）。
type BatchResult struct {
	Response map[string]any
	Err      error
}

// StartBackgroundCheck 启动后台健康检查（async 上下文入口 __aenter__ 的等价面）。
// ← reliable_router.py:105-109 __aenter__
func (r *ReliableRouter) StartBackgroundCheck() {
	if r.HealthChecker != nil {
		r.HealthChecker.StartBackgroundCheck()
	}
}

// StopBackgroundCheck 停止后台健康检查（__aexit__ 等价面）。
// ← reliable_router.py:111-116 __aexit__
func (r *ReliableRouter) StopBackgroundCheck() {
	if r.HealthChecker != nil {
		r.HealthChecker.StopBackgroundCheck()
	}
}

// GetStats 获取统计信息。← reliable_router.py:1701-1731
func (r *ReliableRouter) GetStats() map[string]any {
	latencyStats := map[string]any{}
	for _, dep := range r.Deployments {
		raw := r.State.GetAverageLatencyRaw(dep.ID)
		var avgRaw any
		if raw != nil {
			avgRaw = round4(*raw)
		}
		norm := r.State.GetAverageLatency(dep.ID)
		var avgNorm any
		if norm != floatInf() {
			avgNorm = round4(norm)
		}
		latencyStats[dep.ID] = map[string]any{
			"avg_latency":            avgRaw,  // ← py:1715 真实延迟，无记录 None
			"avg_normalized_latency": avgNorm, // ← py:1722 归一化延迟
			"total_tokens":           r.State.TotalTokens[dep.ID],
			"total_requests":         r.State.TotalRequests[dep.ID],
		}
	}
	deploymentStatus := map[string]any{}
	for _, dep := range r.Deployments {
		status, ok := r.State.DeploymentStatuses[dep.ID]
		if !ok {
			status = intelliroutercore.DeploymentStatusHealthy
		}
		deploymentStatus[dep.ID] = status.String()
	}
	consecutiveFailures := map[string]any{}
	for k, v := range r.State.ConsecutiveFailures {
		consecutiveFailures[k] = v
	}
	return map[string]any{
		"total_deployments":    len(r.Deployments),
		"model_list":           r.GetModelList(),
		"deployment_status":    deploymentStatus,
		"consecutive_failures": consecutiveFailures, // ← py:1712 23.4b 错误翻译层契约
		"latency_stats":        latencyStats,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// completionOnDeployment 显式指定部署的直通路径（review 11/16）。
// ← reliable_router.py:631-728 _completion_on_deployment
func (r *ReliableRouter) completionOnDeployment(ctx context.Context, model string, messages []map[string]any, deployment *intelliroutercore.Deployment, kwargs map[string]any) (map[string]any, error) {
	requestID := observability.NewRequestID()
	requestExtra := r.requestExtra()
	started := routingEvent(observability.EventTypeRequestStarted, requestID, model, requestExtra)
	started.TotalAttempts = 1
	r.EventBus.Emit(started)
	startTime := nowSec()
	actualModel := actualModelOf(model, deployment)
	requestKwargs, err := r.requestKwargsForDeployment(deployment, kwargs)
	if err == nil {
		var response map[string]any
		response, err = r.BaseRouter.Completion(ctx, actualModel, messages, deployment, requestKwargs)
		if err == nil {
			latency := nowSec() - startTime
			usage := usageFromResponse(response)
			tokens := intOfAny(usage["completion_tokens"])
			promptTokens := intOfAny(usage["prompt_tokens"])
			ev := routingEvent(observability.EventTypeRequestSucceeded, requestID, model,
				r.routeMetadata(deployment, map[string]any{"attempt": 1}))
			ev.DeploymentID = &deployment.ID
			ev.Provider = &deployment.Provider
			ev.Latency = &latency
			ev.PromptTokens = &promptTokens
			ev.CompletionTokens = &tokens
			total := promptTokens + tokens
			ev.TotalTokens = &total
			ev.Attempt = 1
			ev.TotalAttempts = 1
			r.EventBus.Emit(ev)
			return response, nil
		}
	}
	// 异常路径：EXHAUSTED 终态（py:690-702）
	latency := nowSec() - startTime
	ev := exhaustedEvent(requestID, model, &latency, ptrStr(err.Error()), r.routeMetadata(deployment,
		map[string]any{"attempt": 1, "fallback_reason": fallbackReasonFromError(err)}))
	ev.Attempt = 1
	ev.TotalAttempts = 1
	r.EventBus.Emit(ev)
	return nil, err
}

// getAvailableDeployments 获取可用部署。← reliable_router.py:181-203
func (r *ReliableRouter) getAvailableDeployments(model string) []*intelliroutercore.Deployment {
	now := nowSec()
	r.deploymentsLock.RLock()
	var allDeployments []*intelliroutercore.Deployment
	if model == ModelWildcard {
		// model="*" 时从所有 deployment 中选（统一调度）← py:186-188
		allDeployments = append([]*intelliroutercore.Deployment{}, r.Deployments...)
	} else {
		allDeployments = r.GetDeploymentsForModel(model)
	}
	r.deploymentsLock.RUnlock()
	var available []*intelliroutercore.Deployment
	for _, dep := range allDeployments {
		status, ok := r.State.DeploymentStatuses[dep.ID]
		if !ok {
			status = intelliroutercore.DeploymentStatusHealthy
		}
		if status == intelliroutercore.DeploymentStatusCooldown {
			until := 0.0
			if u, ok := r.State.CooldownUntil[dep.ID]; ok && u != nil {
				until = *u
			}
			if now < until {
				continue
			}
			// 软恢复：状态与 health_state 一并恢复（reset_deployment 统一
			// 处理），否则 health_state 残留 false，部署永远不会被再次调度。
			r.State.ResetDeployment(dep.ID) // ← py:197-199
		}
		available = append(available, dep)
	}
	return available
}

// availableDeploymentsForModel 按 state 可用性过滤模型的候选部署（review 16）。
// ← reliable_router.py:206-212（覆写 BaseRouter 钩子）
func (r *ReliableRouter) availableDeploymentsForModel(model string) []*intelliroutercore.Deployment {
	return r.getAvailableDeployments(model)
}

// syncQuotaState 把部署声明的 tpm/rpm 配额接入/同步到 state（review 4，P0）。
// ← reliable_router.py:146-179
func (r *ReliableRouter) syncQuotaState(deployments []*intelliroutercore.Deployment) {
	for _, dep := range deployments {
		// Token (tpm) ← py:151-161
		if dep.TPM != nil {
			usage, ok := r.State.TokenUsage[dep.ID]
			if !ok {
				r.State.TokenUsage[dep.ID] = &intelliroutercore.TokenUsage{Limit: *dep.TPM}
			} else if usage.Limit != *dep.TPM {
				usage.Limit = *dep.TPM // 热更新配额值：只改 limit，保留累计 used
			}
		} else if _, exists := r.State.TokenUsage[dep.ID]; exists {
			delete(r.State.TokenUsage, dep.ID) // 配额被移除：回到 inf 语义
		}
		// RPM (rpm) ← py:163-175
		if dep.RPM != nil {
			tracker, ok := r.State.RPMTrackers[dep.ID]
			if !ok {
				r.State.RPMTrackers[dep.ID] = &intelliroutercore.RPMTracker{RPMLimit: *dep.RPM}
			} else if tracker.RPMLimit != *dep.RPM {
				tracker.RPMLimit = *dep.RPM
			}
		} else if _, exists := r.State.RPMTrackers[dep.ID]; exists {
			delete(r.State.RPMTrackers, dep.ID)
		}
	}
}

// requestKwargsForDeployment 合并 request_defaults 与请求参数。
// ← reliable_router.py:217-240 _request_kwargs_for_deployment
func (r *ReliableRouter) requestKwargsForDeployment(deployment *intelliroutercore.Deployment, requestKwargs map[string]any) (map[string]any, error) {
	merged := map[string]any{}
	for key, value := range deployment.RequestDefaults {
		if routingInternalKeys[key] {
			continue // ← py:221-224 剔除路由专用参数
		}
		merged[key] = value
	}
	// ← py:225-231 保留键校验
	var invalidKeys []string
	for key := range reservedRequestDefaultKeys {
		if _, exists := merged[key]; exists {
			invalidKeys = append(invalidKeys, key)
		}
	}
	if len(invalidKeys) > 0 {
		sort.Strings(invalidKeys) // ← py:229 sorted(invalid_keys)
		return nil, fmt.Errorf("deployment.request_defaults cannot contain reserved keys: %v", invalidKeys)
	}
	for key, value := range requestKwargs {
		if value != nil && !routingInternalKeys[key] { // ← py:232-235 过滤 None
			merged[key] = value
		}
	}
	return merged, nil
}

// usesStrictFallbackErrors ← py:245-247 _uses_strict_fallback_errors
func (r *ReliableRouter) usesStrictFallbackErrors() bool {
	return r.Strategy.StrictFallbackErrors()
}

// fallbackToFirstOnEmptySelection ← py:242-244
func (r *ReliableRouter) fallbackToFirstOnEmptySelection() bool {
	return r.Strategy.FallbackToFirstOnEmptySelection()
}

// routeMetadata 组装路由元数据（None 值剔除）。← py:249-257 _route_metadata
func (r *ReliableRouter) routeMetadata(deployment *intelliroutercore.Deployment, extra map[string]any) map[string]any {
	metadata := map[string]any{
		"route_id":   deployment.ID,
		"model_id":   deployment.ModelID,
		"model_name": deployment.ModelName,
		"provider":   deployment.Provider,
	}
	if r.ModelGroupID != nil {
		metadata["model_group_id"] = *r.ModelGroupID // ← py:251（None 剔除见末尾）
	}
	for k, v := range extra {
		metadata[k] = v
	}
	for k, v := range metadata {
		if v == nil {
			delete(metadata, k) // ← py:257 剔除 None
		}
	}
	return metadata
}

// requestExtra model_group_id 请求附加（py:430 等多处惯用）。
func (r *ReliableRouter) requestExtra() map[string]any {
	if r.ModelGroupID != nil {
		return map[string]any{"model_group_id": *r.ModelGroupID}
	}
	return map[string]any{}
}

// emitEventWithoutAwait 在不能等待的上下文投递事件。
// ← py:259-290 _emit_event_without_await（create_task → goroutine 投递，
// 同步调用后由调度器执行——语义等价）
func (r *ReliableRouter) emitEventWithoutAwait(event observability.RoutingEvent) {
	go r.EventBus.Emit(event)
}

// emitStreamCancelled STREAM_CANCELLED 终态（消费者中断）。
// ← py:987-1030 except GeneratorExit/CancelledError 分支
func (r *ReliableRouter) emitStreamCancelled(requestID, model string, selected *intelliroutercore.Deployment, overallStart float64, chunkCount int) {
	extra := map[string]any{"reason": "consumer_break"}
	ev := routingEvent(observability.EventTypeStreamCancelled, requestID, model, extra)
	if selected != nil {
		ev.DeploymentID = &selected.ID
		ev.Provider = &selected.Provider
	}
	lat := nowSec() - overallStart
	ev.Latency = &lat
	ev.ChunkCount = &chunkCount
	// 技术约束：中断处理里不能阻塞等待——事件经 goroutine 投递
	//（py:1009-1011 create_task 等价）
	go r.EventBus.Emit(ev)
}

// emitStreamCancelledTyped Stream 路径的取消终态（同上，py:1462-1506）。
func (r *ReliableRouter) emitStreamCancelledTyped(requestID, model string, selected *intelliroutercore.Deployment, overallStart float64, chunkCount int) {
	r.emitStreamCancelled(requestID, model, selected, overallStart, chunkCount)
}

// emitTerminalExhausted 在「无法继续 fallback、直接 raise」的路径上补发
// EXHAUSTED 终态。← py:294-325 _emit_terminal_exhausted
func (r *ReliableRouter) emitTerminalExhausted(requestID, model string, overallStart float64, attempt, totalAttempts int, err error, extra map[string]any) {
	ev := exhaustedEvent(requestID, model, ptrFloat(nowSec()-overallStart), nil, extra)
	ev.Attempt = attempt
	ev.TotalAttempts = totalAttempts
	if err != nil {
		errType := fallbackReasonFromError(err) // error_type 保持异常类名口径
		ev.ErrorType = &errType
		msg := err.Error()
		ev.ErrorMessage = &msg
	}
	r.EventBus.Emit(ev)
}

// selectDeployment 策略选择 + 空选回退。← py:372-383 _select_deployment
func (r *ReliableRouter) selectDeployment(available []*intelliroutercore.Deployment, context *intelliroutercore.RoutingContext) *intelliroutercore.Deployment {
	selected := r.Strategy.SelectDeployment(available, context)
	if selected != nil {
		return selected
	}
	if r.fallbackToFirstOnEmptySelection() && len(available) > 0 { // ← py:380-381
		return available[0]
	}
	return nil
}

// ──────────────────────────── 包级助手（_static 方法面） ────────────────────

// isRequestSideError 判断异常是否源于请求本身而非部署（review 7）。
// ← reliable_router.py:341-370 _is_request_side_error
//
// 分类规则：
//   - 本地参数构造或校验问题（ValueError/TypeError）→ 请求侧
//   - DeploymentError 携带 4xx status_code（如 400/404/413/422）→ 请求侧；
//     401/403（认证）与 429（限流）是部署侧问题（换部署可恢复）
//   - 其余（5xx / 超时 / 网络 / 未知）→ 部署侧，保持计数冷却行为
func isRequestSideError(err error) bool {
	if err == nil {
		return false
	}
	// json.JSONDecodeError 虽是 ValueError 子类，但由远端非法 JSON 触发，
	// 归部署侧（py:361）——Go 侧 200 非 JSON 已折为 DeploymentNetworkError，
	// 不属于 ValueError 通道，无需特判
	if intellirouter.IsDeploymentError(err) {
		// 401/403（Auth）与 429（RateLimit）专属类型 → 部署侧（py:362-363）
		var base *intellirouter.IRBase
		if intellirouter.AsIRBase(err, &base) && base != nil {
			if sc, ok := base.Details()["status_code"].(int); ok && sc >= 400 && sc < 500 {
				return true
			}
		}
		return false
	}
	// 其余本地错误（Go 的 fmt.Errorf/参数校验 error）→ 请求侧（py:360-361）
	return true
}

// isFallbackableError 流式 fallback 可行性判定。
// ← reliable_router.py:385-397 _is_fallbackable_error
func isFallbackableError(err error, visibleOutputStarted bool) bool {
	if visibleOutputStarted {
		return false // ← py:386 可见输出已交付后不再换部署
	}
	if err == nil {
		return false
	}
	return intellirouter.IsDeploymentNetworkError(err) ||
		intellirouter.IsDeploymentRateLimitError(err) ||
		intellirouter.IsDeploymentServerError(err) ||
		intellirouter.IsDeploymentTimeoutError(err)
}

// hasVisibleStreamOutput 判定 chunk 是否携带可见输出。
// ← reliable_router.py:399-413 _has_visible_stream_output
func hasVisibleStreamOutput(chunk map[string]any) bool {
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		return false
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		choice = map[string]any{}
	}
	delta, _ := choice["delta"].(map[string]any)
	if delta == nil {
		delta = map[string]any{}
	}
	message, _ := choice["message"].(map[string]any)
	if message == nil {
		message = map[string]any{}
	}
	return truthy(delta["content"]) || truthy(delta["reasoning_content"]) ||
		truthy(delta["tool_calls"]) || truthy(message["content"]) ||
		truthy(message["reasoning_content"]) || truthy(message["tool_calls"])
}

// usageFromResponse 提取响应中的 usage 字段（review 6）。
// ← reliable_router.py:327-339 _usage_from_response
func usageFromResponse(response map[string]any) map[string]any {
	if response == nil {
		return map[string]any{}
	}
	usage, ok := response["usage"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return usage
}

// fallbackReasonFromError 错误 → 类名口径。← py:291-292
func fallbackReasonFromError(err error) string {
	return intellirouter.ErrorTypeName(err)
}

// allDeploymentsFailed 构造 AllDeploymentsFailed。← py:630 raise
func allDeploymentsFailed(model string, errors []depError) error {
	var list []any
	for _, e := range errors {
		list = append(list, []any{e.id, e.msg})
	}
	return intellirouter.NewAllDeploymentsFailed(model, list)
}

// buildParams 从 kwargs 中滤除 None 值。← py:1113-1115 _build_params
func buildParams(kwargs map[string]any) map[string]any {
	result := map[string]any{}
	for k, v := range kwargs {
		if v != nil {
			result[k] = v
		}
	}
	return result
}

// resolveModelName 解析模型名。← py:1117-1125 _resolve_model_name
func (r *ReliableRouter) resolveModelName(model string) (string, error) {
	if model != "" {
		return model, nil
	}
	if len(r.Deployments) == 0 {
		return "", fmt.Errorf(
			"No model specified and no deployments configured. " +
				"Use model='*' for unified routing across all deployments.")
	}
	return r.Deployments[0].ModelName, nil
}

// actualModelOf model=="*" 时用部署自身 model_name（py:491/694/852/1281）。
func actualModelOf(model string, deployment *intelliroutercore.Deployment) string {
	if model == ModelWildcard {
		return deployment.ModelName
	}
	return model
}

// routingEvent 构造带 extra 的事件。
func routingEvent(eventType observability.RoutingEventType, requestID, model string, extra map[string]any) observability.RoutingEvent {
	ev := observability.NewRoutingEvent(eventType, requestID, model)
	ev.Extra = extra
	return ev
}

// exhaustedEvent 构造 ALL_DEPLOYMENTS_EXHAUSTED 事件。← py:294-325 构造形参
func exhaustedEvent(requestID, model string, latency *float64, errMsg *string, extra map[string]any) observability.RoutingEvent {
	ev := routingEvent(observability.EventTypeAllDeploymentsExhausted, requestID, model, extra)
	ev.Latency = latency
	ev.ErrorMessage = errMsg
	return ev
}

// removeDeployment 从候选列表剔除指定 ID（py:545/1428 列表推导）。
func removeDeployment(deployments []*intelliroutercore.Deployment, id string) []*intelliroutercore.Deployment {
	var result []*intelliroutercore.Deployment
	for _, d := range deployments {
		if d.ID != id {
			result = append(result, d)
		}
	}
	return result
}

// emitChunk 产出流 chunk；ctx 取消（消费者中断）返回 false。
func emitChunk(out chan<- StreamChunk, ctx context.Context, chunk map[string]any) bool {
	select {
	case out <- StreamChunk{Chunk: chunk}:
		return true
	case <-ctx.Done():
		return false
	}
}

// emitTypedChunk 同 emitChunk 的类型化版本。
func emitTypedChunk(out chan<- ChunkResult, ctx context.Context, chunk intellirouter.AssistantMessageChunk) bool {
	select {
	case out <- ChunkResult{Chunk: chunk}:
		return true
	case <-ctx.Done():
		return false
	}
}

// usageToMetadata usage dict → UsageMetadata。← py:1524-1584 _usage_to_metadata
func usageToMetadata(usage map[string]any, modelName *string) *intellirouter.UsageMetadata {
	if len(usage) == 0 {
		return nil // ← py:1526 if not usage
	}
	intValue := func(v any) int {
		if v == nil || isBool(v) {
			return 0
		}
		f, err := toFloat64(v)
		if err != nil {
			return 0
		}
		i := int(f)
		if i < 0 {
			i = 0
		}
		return i
	}
	optionalInt := func(v any) *int {
		if v == nil || isBool(v) {
			return nil
		}
		f, err := toFloat64(v)
		if err != nil {
			return nil
		}
		i := int(f)
		if i < 0 {
			i = 0
		}
		return &i
	}
	floatValue := func(v any) float64 {
		if v == nil || isBool(v) {
			return 0.0
		}
		f, err := toFloat64(v)
		if err != nil {
			return 0.0
		}
		return f
	}
	promptDetails, _ := usage["prompt_tokens_details"].(map[string]any)
	if promptDetails == nil {
		promptDetails = map[string]any{}
	}
	completionDetails, _ := usage["completion_tokens_details"].(map[string]any)
	if completionDetails == nil {
		completionDetails = map[string]any{}
	}
	cacheStatus, _ := usage["cache_status"].(string)
	cacheSource, _ := usage["cache_source"].(string)
	cacheAuthoritative := usage["cache_authoritative"] == true // ← py:1554 is True
	cacheCreation := optionalInt(orAny(usage["cache_creation_input_tokens"], promptDetails["cache_creation_input_tokens"]))
	reasoningTokens := intValue(orAny(completionDetails["reasoning_tokens"], usage["reasoning_tokens"]))
	// ← py UsageMetadata dataclass __post_init__（types.py:41）派生 total_tokens
	m := intellirouter.NewUsageMetadata(intellirouter.UsageMetadata{
		ModelName:                modelName,
		InputTokens:              intValue(usage["prompt_tokens"]),
		OutputTokens:             intValue(usage["completion_tokens"]),
		TotalTokens:              intValue(usage["total_tokens"]),
		CacheTokens:              intValue(orAny(promptDetails["cached_tokens"], usage["cache_tokens"])),
		CacheReadTokens:          optionalInt(usage["cache_read_tokens"]),
		CacheMissTokens:          optionalInt(usage["cache_miss_tokens"]),
		CacheWriteTokens:         optionalInt(usage["cache_write_tokens"]),
		CacheStatus:              strPtrOrNil(cacheStatus), // ← py:1555 isinstance(str)
		CacheSource:              strPtrOrNil(cacheSource),
		CacheAuthoritative:       cacheAuthoritative,
		CacheCreationInputTokens: cacheCreation,
		ReasoningTokens:          reasoningTokens,
		InputCost:                floatValue(usage["input_cost"]),
		OutputCost:               floatValue(usage["output_cost"]),
		TotalCost:                floatValue(usage["total_cost"]),
	})
	return &m
}

// responseToMessage OpenAI 格式 dict 响应 → AssistantMessage。
// ← reliable_router.py:1586-1632 _response_to_message
func responseToMessage(raw map[string]any) *intellirouter.AssistantMessage {
	choices, _ := raw["choices"].([]any)
	if len(choices) == 0 {
		return &intellirouter.AssistantMessage{Content: ""} // ← py:1590-1592
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		choice = map[string]any{}
	}
	message, _ := choice["message"].(map[string]any)
	if message == nil {
		message = map[string]any{}
	}
	content := strOfAny(message["content"])
	finishReason := strOfAny(choice["finish_reason"])
	if finishReason == "" {
		finishReason = "stop" // ← py:1597 or "stop"
	}
	reasoningContent := strPtrOrNil(strOfAny(message["reasoning_content"]))
	if message["reasoning_content"] == nil {
		reasoningContent = nil
	}
	var toolCalls []intellirouter.ToolCall
	if rawToolCalls, ok := message["tool_calls"].([]any); ok && len(rawToolCalls) > 0 {
		for idx, tc := range rawToolCalls {
			tcMap, _ := tc.(map[string]any)
			if tcMap == nil {
				continue
			}
			fn, _ := tcMap["function"].(map[string]any)
			if fn == nil {
				fn = map[string]any{}
			}
			index := idx // ← py:1614 index=tc.get("index", idx)
			if v, ok := tcMap["index"].(float64); ok {
				index = int(v)
			}
			toolCalls = append(toolCalls, intellirouter.ToolCall{
				ID:        strOfAny(tcMap["id"]),
				Type:      orDefault(strOfAny(tcMap["type"]), "function"), // ← py:1611
				Name:      strOfAny(fn["name"]),
				Arguments: strOfAny(fn["arguments"]),
				Index:     &index,
			})
		}
	}
	modelName := strPtrOrNil(strOfAny(raw["model"]))
	usageMetadata := usageToMetadata(mapOf(raw["usage"]), modelName)
	choiceProviderMetadata := providerMetadataFromResponse(choice)
	providerMetadata := map[string]any{}
	for k, v := range providerMetadataFromResponse(raw) {
		providerMetadata[k] = v
	}
	for k, v := range choiceProviderMetadata {
		providerMetadata[k] = v // ← py:1625 {**raw, **choice}
	}
	var providerContent any = raw // ← py:1626 raw.get("provider_content", raw)
	if pc, ok := raw["provider_content"]; ok {
		providerContent = pc
	}
	msg := &intellirouter.AssistantMessage{
		Content:          content,
		UsageMetadata:    usageMetadata,
		FinishReason:     finishReason,
		ReasoningContent: reasoningContent,
		ResponseID:       strPtrOrNil(strOfAny(raw["id"])),
		ResponseModel:    modelName,
		ProviderMetadata: providerMetadata,
		ProviderContent:  providerContent,
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}
	if lp, ok := choice["logprobs"]; ok {
		msg.Logprobs = lp
	}
	return msg
}

// chunkToAssistantMessageChunk OpenAI streaming chunk → AssistantMessageChunk。
// ← reliable_router.py:1634-1699 _chunk_to_assistant_message_chunk
func chunkToAssistantMessageChunk(chunk map[string]any) *intellirouter.AssistantMessageChunk {
	choices, _ := chunk["choices"].([]any)
	if len(choices) == 0 {
		// 无 choices：仅 usage chunk（py:1637-1649）
		usageMetadata := usageToMetadata(mapOf(chunk["usage"]), strPtrOrNil(strOfAny(chunk["model"])))
		if usageMetadata == nil {
			return nil
		}
		return &intellirouter.AssistantMessageChunk{
			UsageMetadata:    usageMetadata,
			ResponseID:       strPtrOrNil(strOfAny(chunk["id"])),
			ResponseModel:    strPtrOrNil(strOfAny(chunk["model"])),
			ProviderMetadata: providerMetadataFromResponse(chunk),
			ProviderContent:  orDefaultAny(chunk["provider_content"], chunk),
		}
	}
	choice, _ := choices[0].(map[string]any)
	if choice == nil {
		choice = map[string]any{}
	}
	delta, _ := choice["delta"].(map[string]any)
	if delta == nil {
		delta = map[string]any{}
	}
	var finishReason *string
	if fr := choice["finish_reason"]; fr != nil {
		finishReason = strPtrOrNil(strOfAny(fr))
	}
	content := strOfAny(delta["content"])
	var reasoningContent *string
	if delta["reasoning_content"] != nil {
		reasoningContent = strPtrOrNil(strOfAny(delta["reasoning_content"]))
	}
	rawToolCalls, _ := delta["tool_calls"].([]any)
	usageMetadata := usageToMetadata(mapOf(chunk["usage"]), strPtrOrNil(strOfAny(chunk["model"])))

	// ← py:1668-1675 空白 chunk 判 None
	if content == "" && finishReason == nil && len(rawToolCalls) == 0 &&
		reasoningContent == nil && usageMetadata == nil {
		return nil
	}

	var toolCalls []intellirouter.ToolCall
	if len(rawToolCalls) > 0 {
		for _, tc := range rawToolCalls {
			tcMap, _ := tc.(map[string]any)
			if tcMap == nil {
				continue
			}
			fn, _ := tcMap["function"].(map[string]any)
			if fn == nil {
				fn = map[string]any{}
			}
			var index *int
			if tcMap["index"] != nil {
				// ← py:1688 index=tc.get("index") 无兜底（int/float 均收）
				i := intOfAny(tcMap["index"])
				index = &i
			}
			toolCalls = append(toolCalls, intellirouter.ToolCall{
				ID:        strOfAny(tcMap["id"]),
				Type:      orDefault(strOfAny(tcMap["type"]), "function"),
				Name:      strOfAny(fn["name"]),
				Arguments: strOfAny(fn["arguments"]),
				Index:     index,
			})
		}
	}

	providerMetadata := map[string]any{}
	for k, v := range providerMetadataFromResponse(chunk) {
		providerMetadata[k] = v
	}
	for k, v := range providerMetadataFromResponse(choice) {
		providerMetadata[k] = v // ← py:1694 {**chunk, **choice}
	}
	var providerContent any = chunk
	if pc, ok := chunk["provider_content"]; ok {
		providerContent = pc
	}
	return &intellirouter.AssistantMessageChunk{
		Content:          content,
		ReasoningContent: reasoningContent,
		ToolCalls:        toolCalls,
		FinishReason:     finishReason,
		UsageMetadata:    usageMetadata,
		ResponseID:       strPtrOrNil(strOfAny(chunk["id"])),
		ResponseModel:    strPtrOrNil(strOfAny(chunk["model"])),
		ProviderMetadata: providerMetadata,
		ProviderContent:  providerContent,
	}
}

// providerMetadataFromResponse 白名单元数据。← py:1512-1522
func providerMetadataFromResponse(raw map[string]any) map[string]any {
	allowed := []string{
		"system_fingerprint", "service_tier", "status", "stop_reason",
		"stop_sequence", "incomplete_details",
	}
	result := map[string]any{}
	for _, key := range allowed {
		if v, ok := raw[key]; ok {
			result[key] = v
		}
	}
	return result
}

// createStrategy 策略工厂。← strategy/__init__.py create_strategy
// 偏差 D-1：lowest-latency/tag-based/token-aware/rate-limit-aware/adaptive
// 上游不可达，不移植；未知策略报错（对齐 py raise ValueError）。
func createStrategy(strategyType string, kwargs map[string]any) (strategy.Strategy, error) {
	switch strategyType {
	case "simple-shuffle": // ← py __init__:17-18
		var weights map[string]float64
		var defWeight float64
		if w, ok := kwargs["weights"].(map[string]any); ok {
			weights = map[string]float64{}
			for k, v := range w {
				weights[k] = toFloatMust(v)
			}
		}
		if dw, ok := kwargs["default_weight"].(float64); ok {
			defWeight = dw
		}
		return strategy.NewSimpleShuffle(weights, defWeight, nil), nil
	case "ordered-failover": // ← py:23-24
		return strategy.NewOrderedFailover(), nil
	case "tag-filtered": // ← py:25-26
		var tag *string
		if t, ok := kwargs["fallback_tag"].(string); ok && t != "" {
			tag = &t
		}
		return strategy.NewTagFiltered(tag, nil), nil
	default:
		return nil, fmt.Errorf("Unknown strategy: %s", strategyType)
	}
}

// depError (deployment_id, error_message) 二元组。← py:931 errors.append((selected.id, str(e)))
type depError struct {
	id  string
	msg string
}

// nowSec 当前秒级时间戳（对齐 time.time()）。
func nowSec() float64 {
	return float64(time.Now().UnixNano()) / 1e9
}

// minInt min(a, b)。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// intOfAny usage 字段取值（py:496 `usage.get("x") or 0`：None/缺省 → 0）。
func intOfAny(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case float64:
		return int(t)
	default:
		return 0
	}
}

// strOfAny any → string（None/缺省 → 空串）。
func strOfAny(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// truthy Python truthiness（非空判定）。
func truthy(v any) bool {
	switch t := v.(type) {
	case nil:
		return false
	case string:
		return t != ""
	case bool:
		return t
	case int:
		return t != 0
	case float64:
		return t != 0
	case []any:
		return len(t) > 0
	case map[string]any:
		return len(t) > 0
	default:
		return true
	}
}

// isBool Python isinstance(v, bool) 判定。
func isBool(v any) bool {
	_, ok := v.(bool)
	return ok
}

// toFloat64 Python float(v) 等价（int/float64/数字字符串）。
func toFloat64(v any) (float64, error) {
	switch t := v.(type) {
	case int:
		return float64(t), nil
	case float64:
		return t, nil
	case string:
		var f float64
		_, err := fmt.Sscanf(strings.TrimSpace(t), "%g", &f)
		return f, err
	default:
		return 0, fmt.Errorf("not a number")
	}
}

// toFloatMust 宽松转 float（失败回 0；weights 场景）。
func toFloatMust(v any) float64 {
	f, _ := toFloat64(v)
	return f
}

// mapOf any → map（非 dict 返回 nil，对齐 py isinstance(usage, dict) 判定）。
func mapOf(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return nil
}

// orAny `a or b` 语义（a 为 nil/零值取 b）。
func orAny(a, b any) any {
	if truthy(a) {
		return a
	}
	return b
}

// orDefault 空串取默认（py:1611 tc.get("type", "function")）。
func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

// orDefaultAny 非 nil 取 a，否则 b（py:1626 raw.get(k, raw)）。
func orDefaultAny(a, b any) any {
	if a != nil {
		return a
	}
	return b
}

// strPtrOrNil 字符串指针（空串仍保留——调用方决定 None 语义）。
func strPtrOrNil(s string) *string {
	return &s
}

// ptrFloat *float64 构造。
func ptrFloat(f float64) *float64 { return &f }

// firstNonNil 取第一个非 nil map。
func firstNonNil(maps ...map[string]any) map[string]any {
	for _, m := range maps {
		if m != nil {
			return m
		}
	}
	return map[string]any{}
}

// contextErrMsg str(context.error) if context.error else None。
func contextErrMsg(c *intelliroutercore.RoutingContext) *string {
	if c.Error != nil {
		return ptrStr(c.Error.Error())
	}
	return nil
}

// round4 四舍五入 4 位小数（py:1715 round(raw_latency, 4) 浮点噪声控制）。
func round4(f float64) float64 {
	return math.Round(f*10000) / 10000
}

// floatInf float('inf')。
func floatInf() float64 {
	return math.Inf(1)
}

// nilToNil 值包装为 any（指针 nil → nil）。
func nilToNil(v any) any { return v }

// ptrStr 字符串指针。
func ptrStr(s string) *string { return &s }

// floatPtrOf float → 指针。
func floatPtrOf(f float64) *float64 { return &f }
