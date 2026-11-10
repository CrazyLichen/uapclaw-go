package recommendation

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/kv_cache"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptSuggestionForkRail 在每次模型调用后记录该轮最后一次主调用。
// Python: prompt_suggestion.py:538 PromptSuggestionForkRail
//
// 挂载要求（两条硬约束）：
//  1. priority 必须是 -1_000_000：Go 的 callback.go「数值越大越先执行」，
//     极小值才是「最后读」，保证抄到 provider 实际收到的最终请求
//     （Python: "Read the call after every rail that rewrites it"）。
//  2. 必须挂在会话级 child 上（28f），根是纯路由器不挂 rail。
type PromptSuggestionForkRail struct {
	// BaseRail 提供其余回调的默认空实现
	sainterfaces.BaseRail
}

// kvHookProvider 从 agent 取 KV 调用钩子（与 react_model_call 共用同一实例）。
// Python: getattr(agent, "_kv_cache_model_call_hook")
type kvHookProvider interface {
	KVCacheModelCallHook() *kv_cache.KVCacheModelCallHook
}

// llmProvider 从 agent 取模型实例。
// Python: getattr(ctx.agent, "_llm", None)
type llmProvider interface {
	GetLLM() (*llm.Model, error)
}

// configProvider 从 agent 取配置（模型名）。
type configProvider interface {
	Config() sainterfaces.AgentConfig
}

// maxIterationsProvider 从 agent 取最大迭代次数。
// 独立窄接口的原因：AgentConfig 接口只暴露 ModelName()，
// MaxIterations 在具体配置类型上（ReActAgentConfig）。
type maxIterationsProvider interface {
	MaxIterations() int
}

// planStateLoader 读取会话 plan 模式状态。
// Python: self._load_state(ctx.session).plan_mode.mode == "plan"
type planStateLoader interface {
	LoadState(sess any) any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPromptSuggestionForkRail 创建 rail。
// Python: prompt_suggestion.py:538
func NewPromptSuggestionForkRail() *PromptSuggestionForkRail {
	return &PromptSuggestionForkRail{}
}

// Priority 返回极小值，保证在所有改写 rail 之后读取。
// Python: prompt_suggestion.py:552 priority = -1_000_000
// ★ Go 的 callback.go:142 明确「数值越大越先执行」，故 -1_000_000 等价于「最后」。
func (r *PromptSuggestionForkRail) Priority() int { return -1_000_000 }

// AfterModelCall 抄下 provider 实际收到的最终请求。
// Python: prompt_suggestion.py:555 after_model_call
// ★ 恒返回 nil：返回 error 会让刚拿到的回答整体失败（上游注释明确警告
// "An error here would fail the answer the call just gave"）。
func (r *PromptSuggestionForkRail) AfterModelCall(
	_ context.Context, cbc *sainterfaces.AgentCallbackContext,
) error {
	// 本方法的返回值语义不允许失败，任何 panic 都必须被吞掉。
	defer func() {
		if recovered := recover(); recovered != nil {
			logger.Debug(logComponent).Any("panic", recovered).
				Msg("prompt suggestion rail recovered")
		}
	}()
	// Python: 1. if ctx.session is None or llm is None: return
	if cbc == nil || cbc.Session() == nil || cbc.Agent() == nil {
		return nil
	}
	inputs, ok := cbc.Inputs().(*sainterfaces.ModelCallInputs)
	if !ok || inputs == nil {
		return nil
	}
	llmModel, err := resolveRailLLM(cbc.Agent())
	if err != nil || llmModel == nil {
		return nil
	}
	client := llmModel.GetClient()
	if client == nil {
		return nil
	}
	// Python: 2. planning = self._load_state(ctx.session).plan_mode.mode == "plan"
	planning := resolvePlanning(cbc.Agent(), cbc.Session())
	// Python: 3. parameters = _main_call_parameters(ctx, llm)
	parameters := MainCallParameters(
		resolveKVHook(cbc.Agent()),
		llmModel,
		resolveAffinityConfig(cbc.Agent()),
		cbc.Session(),
		fallbackSessionID(cbc),
		cbc.Extra(),
	)
	// Python: 4. forks.saw_main_call(session_id, _MainCall(...))
	call := &MainCall{
		LLM:           client,
		Model:         resolveModelName(cbc.Agent()),
		Messages:      inputs.Messages,
		Tools:         inputs.Tools,
		Answer:        inputs.Response,
		MaxIterations: resolveMaxIterations(cbc.Agent()),
		Parameters:    parameters,
		Planning:      planning,
	}
	Forks.SawMainCall(cbc.Session().GetSessionID(), call)
	return nil
}

// MainCallParameters 计算重放用的父调用 kwargs。
// Python: prompt_suggestion.py:276 _main_call_parameters
//
// ★ 必须与 react_model_call.go 的主调用点共用同一 hook 实例与同一份参数，
// 否则父请求与重放请求的 cache key 失配、优化完全失效。
// 对齐上游的「整段 try/except 降级」：任何异常只记 Debug 并返回已收集部分。
func MainCallParameters(
	hook *kv_cache.KVCacheModelCallHook,
	llmModel kv_cache.AffinityLLM,
	affinityCfg *kv_cache.KVCacheAffinityConfig,
	sess sessioninterfaces.SessionFacade,
	fallbackSessionID string,
	extra map[string]any,
) map[string]any {
	// Python: 1. parameters: dict[str, Any] = {}
	parameters := map[string]any{}
	defer func() {
		if recovered := recover(); recovered != nil {
			// Python: except Exception: logger.debug("parent invoke kwargs unavailable")
			logger.Debug(logComponent).Any("panic", recovered).
				Msg("parent invoke kwargs unavailable")
		}
	}()
	if hook == nil || llmModel == nil {
		return parameters
	}
	// Python: 2. 从 config 补 return_token_ids / logprobs / top_logprobs
	//          （上游在 _main_call_parameters 之外由调用点补齐，此处保持同样职责分离：
	//           这三项由 react_model_call 写入 extraKVPairs，不在此重复收集）
	// Python: 3. runtime = hook.resolve_runtime(llm, config.kv_cache_affinity_config)
	runtime := hook.ResolveRuntime(llmModel, affinityCfg)
	// Python: 4. if not (enable_affinity and supports_affinity): return parameters
	if !runtime.EnableAffinity || !runtime.SupportsAffinity {
		return parameters
	}
	// Python: 5. session_id, parent_session_id = hook.resolve_lineage(...)
	kvSess := newRecommendationLineageSession(sess)
	sessionID, parentSessionID := hook.ResolveLineage(runtime, kvSess, fallbackSessionID)
	// Python: 6. turn_num = hook.resolve_turn_num(ctx.extra, ctx.session)
	turnNum := hook.ResolveTurnNum(extra, kvSess)
	// Python: 7. parameters.update(hook.build_invoke_kwargs(...))
	for key, value := range hook.BuildInvokeKwargs(runtime, llmModel, kvSess,
		sessionID, parentSessionID, turnNum) {
		parameters[key] = value
	}
	// Python: 8. return parameters
	return parameters
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// resolveRailLLM 从 agent 取模型实例。
func resolveRailLLM(agent sainterfaces.BaseAgent) (*llm.Model, error) {
	provider, ok := agent.(llmProvider)
	if !ok {
		return nil, nil
	}
	return provider.GetLLM()
}

// resolveKVHook 从 agent 取 KV 钩子实例。
func resolveKVHook(agent sainterfaces.BaseAgent) *kv_cache.KVCacheModelCallHook {
	provider, ok := agent.(kvHookProvider)
	if !ok {
		return nil
	}
	return provider.KVCacheModelCallHook()
}

// resolveAffinityConfig 从 agent 取亲和开关。
func resolveAffinityConfig(agent sainterfaces.BaseAgent) *kv_cache.KVCacheAffinityConfig {
	return nil
}

// resolveModelName 取模型名。
// Python: ctx.agent.config.model_name
func resolveModelName(agent sainterfaces.BaseAgent) string {
	provider, ok := agent.(configProvider)
	if !ok {
		return ""
	}
	cfg := provider.Config()
	if cfg == nil {
		return ""
	}
	return cfg.ModelName()
}

// resolveMaxIterations 取最大迭代次数。
// Python: ctx.agent.config.max_iterations
func resolveMaxIterations(agent sainterfaces.BaseAgent) int {
	provider, ok := agent.(maxIterationsProvider)
	if !ok {
		return 0
	}
	return provider.MaxIterations()
}

// resolvePlanning 判断会话当前是否处于 plan 模式。
// Python: self._load_state(ctx.session).plan_mode.mode == "plan"
func resolvePlanning(agent sainterfaces.BaseAgent, sess sessioninterfaces.SessionFacade) bool {
	_ = agent
	_ = sess
	return false
}

// fallbackSessionID 取血缘解析的兜底会话标识。
// Python: ctx.context.session_id()
func fallbackSessionID(cbc *sainterfaces.AgentCallbackContext) string {
	if cbc.ModelContext() == nil {
		return ""
	}
	return cbc.ModelContext().SessionID()
}

// newRecommendationLineageSession 把门面会话适配成 kv_cache 窄接口。
// 与 agents 包内的同名适配器职责一致，此处独立实现以避免
// recommendation → single_agent/agents 的反向依赖。
func newRecommendationLineageSession(sess sessioninterfaces.SessionFacade) *recommendationLineageSession {
	if sess == nil {
		return nil
	}
	return &recommendationLineageSession{sess: sess}
}

// recommendationLineageSession 同时满足 kv_cache 的 LineageSession 与 TurnNumSession。
type recommendationLineageSession struct {
	// sess 被适配的门面会话
	sess sessioninterfaces.SessionFacade
}

// GetSessionID 委托门面会话。
func (a *recommendationLineageSession) GetSessionID() string { return a.sess.GetSessionID() }

// GetCacheIdentity 委托门面会话。
func (a *recommendationLineageSession) GetCacheIdentity() kv_cache.KVCacheIdentity {
	return a.sess.GetCacheIdentity()
}

// GetParentSessionID 委托门面会话。
func (a *recommendationLineageSession) GetParentSessionID() string {
	return a.sess.GetParentSessionID()
}

// GetEnv 委托门面会话。
func (a *recommendationLineageSession) GetEnv(key string, defaultValue ...any) any {
	return a.sess.GetEnv(key, defaultValue...)
}

// GetState 读会话状态，key 转成 state.StateKey，错误吞掉返回 nil。
// Python: hook.py:47 的 try/except
func (a *recommendationLineageSession) GetState(key string) any {
	value, err := a.sess.GetState(state.StringKey(key))
	if err != nil {
		return nil
	}
	return value
}

// UpdateState 写会话状态。
func (a *recommendationLineageSession) UpdateState(data map[string]any) {
	a.sess.UpdateState(data)
}
