package kv_cache

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AffinityLLM 模型客户端需要提供的亲和能力（窄接口）。
// Python: hook.py 用 getattr(llm, "supports_kv_cache_affinity") /
// getattr(llm, "build_kv_cache_affinity_invoke_kwargs") 动态探测。
type AffinityLLM interface {
	// SupportsKVCacheAffinity provider 是否支持亲和
	SupportsKVCacheAffinity() bool
	// BuildKVCacheAffinityInvokeKwargs 构造 agent_hint 亲和参数
	BuildKVCacheAffinityInvokeKwargs(sess any, sessionID, parentSessionID string, turnNum int) map[string]any
}

// TurnNumSession 解析轮次需要的会话状态读写能力。
// Python: hook.py:47/55 用 try/except 包住 get_state/update_state 并全吞，
// 故 Go 窄接口不带 error（用户决策「无 error」）。
type TurnNumSession interface {
	// GetState 读会话状态；读取异常由实现方内部吞掉并返回 nil
	GetState(key string) any
	// UpdateState 写会话状态；写入异常由实现方内部吞掉
	UpdateState(data map[string]any)
}

// KVCacheCallCapabilities 一次模型调用的亲和能力判定结果。
// Python: hook.py:14 KVCacheCallCapabilities (@dataclass frozen slots)
type KVCacheCallCapabilities struct {
	// EnableAffinity 配置是否开启亲和
	EnableAffinity bool
	// SupportsAffinity provider 是否支持亲和
	SupportsAffinity bool
}

// KVCacheModelCallHook ReAct 模型调用的亲和与窗口变更钩子。
// Python: hook.py:23 KVCacheModelCallHook
type KVCacheModelCallHook struct {
	// affinityWarningLogged 保证「配置开但 provider 不支持」只告警一次
	// Python: self._affinity_warning_logged
	affinityWarningLogged bool
	// mu 保护 affinityWarningLogged
	mu sync.Mutex
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// KVCacheTurnNumberStateKey 会话级 KV 轮次状态键
	// Python: hook.py:11 _KVC_TURN_NUMBER_STATE_KEY
	KVCacheTurnNumberStateKey = "kv_cache_turn_number"
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewKVCacheModelCallHook 创建钩子。
// Python: hook.py:26 __init__
func NewKVCacheModelCallHook() *KVCacheModelCallHook {
	return &KVCacheModelCallHook{}
}

// ResetWarnings 复位一次性告警标记。
// Python: hook.py:29 reset_warnings
func (h *KVCacheModelCallHook) ResetWarnings() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.affinityWarningLogged = false
}

// ResolveTurnNum 解析本轮 1-based 协议轮次。
// Python: hook.py:33 resolve_turn_num
func (h *KVCacheModelCallHook) ResolveTurnNum(callbackExtra map[string]any, sess TurnNumSession) int {
	// Python: 1. for key in ("_turn_number", "_kv_cache_turn_num"):
	//            value = callback_extra.get(key)
	//            if isinstance(value, int) and not isinstance(value, bool) and value > 0: return value
	for _, key := range []string{"_turn_number", "_kv_cache_turn_num"} {
		if v, ok := callbackExtra[key].(int); ok && v > 0 {
			return v
		}
	}

	// Python: 2. previous = 0; try: value = session.get_state(key)
	previous := 0
	if sess != nil {
		if raw := sess.GetState(KVCacheTurnNumberStateKey); raw != nil {
			if v, ok := raw.(int); ok && v > 0 {
				previous = v
			}
		}
	}
	// Python: 3. turn_num = previous + 1
	turnNum := previous + 1
	// Python: 4. try: session.update_state({key: turn_num}) except: debug
	if sess != nil {
		sess.UpdateState(map[string]any{KVCacheTurnNumberStateKey: turnNum})
	}
	// Python: 5. callback_extra["_kv_cache_turn_num"] = turn_num
	if callbackExtra != nil {
		callbackExtra["_kv_cache_turn_num"] = turnNum
	}
	// Python: 6. return turn_num
	return turnNum
}

// ResolveRuntime 判定配置开关与 provider 能力。
// Python: hook.py:62 resolve_runtime
func (h *KVCacheModelCallHook) ResolveRuntime(llm AffinityLLM, cfg *KVCacheAffinityConfig) KVCacheCallCapabilities {
	// Python: 1. config = config or KVCacheAffinityConfig()
	enable := cfg != nil && cfg.EnableKVCacheAffinity
	// Python: 2. supports_affinity = False; if enable_affinity: 探测 provider
	supports := false
	if enable && llm != nil {
		supports = llm.SupportsKVCacheAffinity()
	}
	// Python: 3. runtime = KVCacheCallCapabilities(...); self._warn_unsupported(runtime)
	runtime := KVCacheCallCapabilities{EnableAffinity: enable, SupportsAffinity: supports}
	h.warnUnsupported(runtime)
	// Python: 4. return runtime
	return runtime
}

// ResolveLineage 在亲和生效时解析会话血缘。
// Python: hook.py:83 resolve_lineage（3 参数，含 fallback_session_id）
func (h *KVCacheModelCallHook) ResolveLineage(
	runtime KVCacheCallCapabilities, sess LineageSession, fallbackSessionID string,
) (string, string) {
	// Python: 1. if runtime.enable_affinity and runtime.supports_affinity:
	if !runtime.EnableAffinity || !runtime.SupportsAffinity {
		return "", ""
	}
	// Python: 2. session_id, parent_session_id = resolve_session_lineage(session)
	sid, parent := ResolveSessionLineage(sess)
	// Python: 3. if not session_id: return fallback_session_id, fallback_session_id
	if sid == "" {
		return fallbackSessionID, fallbackSessionID
	}
	// Python: 4. return session_id, parent_session_id
	return sid, parent
}

// HandleContextWindowChange 上下文窗口变化时释放旧 KV。
// Python: hook.py:95 handle_context_window_change -> None
// 内部 _evict_changed_window 的 evict_kvc 用 try/except 全吞 + Warn，
// 故 releaseFn 不带 error（用户决策 #7）；调用方在 lambda 内部记 Warn。
func (h *KVCacheModelCallHook) HandleContextWindowChange(
	releaseFn func(), runtime KVCacheCallCapabilities,
) {
	// Python: if runtime.enable_affinity and runtime.supports_affinity: await self._evict_changed_window(...)
	if !runtime.EnableAffinity || !runtime.SupportsAffinity || releaseFn == nil {
		return
	}
	releaseFn()
}

// BuildInvokeKwargs 构造父请求与重放共用的 affinity kwargs。
// Python: hook.py:119 build_invoke_kwargs
// ★ 父调用与 prompt_suggestion 重放必须共用同一份结果，否则 cache key 失配。
func (h *KVCacheModelCallHook) BuildInvokeKwargs(
	runtime KVCacheCallCapabilities, llm AffinityLLM, sess any,
	sessionID, parentSessionID string, turnNum int,
) map[string]any {
	// Python: 1. extra_kwargs: dict = {}
	out := map[string]any{}
	// Python: 2. build_affinity = getattr(llm, "build_kv_cache_affinity_invoke_kwargs", None)
	// Python: 3. if enable and supports and callable(build_affinity): extra_kwargs.update(...)
	if !runtime.EnableAffinity || !runtime.SupportsAffinity || llm == nil {
		return out
	}
	for k, v := range llm.BuildKVCacheAffinityInvokeKwargs(sess, sessionID, parentSessionID, turnNum) {
		out[k] = v
	}
	// Python: 4. return extra_kwargs
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// warnUnsupported 配置开启但 provider 不支持时只告警一次。
// Python: hook.py:211 _warn_unsupported
func (h *KVCacheModelCallHook) warnUnsupported(runtime KVCacheCallCapabilities) {
	// Python: 1. if runtime.enable_affinity and not runtime.supports_affinity
	//            and not self._affinity_warning_logged:
	if !runtime.EnableAffinity || runtime.SupportsAffinity {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.affinityWarningLogged {
		return
	}
	// Python: 2. logger.warning(...)
	logger.Warn(logComponent).
		Msg("KVCacheAffinityConfig.enable_kv_cache_affinity is True, but the current LLM does not support Ascend KV cache affinity; agent_hint will not take effect")
	// Python: 3. self._affinity_warning_logged = True
	h.affinityWarningLogged = true
}
