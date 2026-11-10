package kv_cache

// ──────────────────────────── 结构体 ────────────────────────────

// KVCacheAffinityConfig KV cache 亲和开关。
// Python: openjiuwen/core/kv_cache/kv_cache_config.py:31 KVCacheAffinityConfig
type KVCacheAffinityConfig struct {
	// EnableKVCacheAffinity 是否启用统一 agent_hint 亲和协议，默认 false
	// Python: enable_kv_cache_affinity: bool = Field(default=False)
	EnableKVCacheAffinity bool `json:"enable_kv_cache_affinity" yaml:"enable_kv_cache_affinity"`
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// KVCRangeActionTimeoutSeconds messages/tools 区间动作的整体预算（秒）
	// Python: KVC_RANGE_ACTION_TIMEOUT_SECONDS = 1.5
	KVCRangeActionTimeoutSeconds = 1.5
	// KVCSessionOffloadPrefetchTimeoutSeconds 会话卸载预取的整体预算（秒）
	// Python: KVC_SESSION_OFFLOAD_PREFETCH_TIMEOUT_SECONDS = 2.0
	KVCSessionOffloadPrefetchTimeoutSeconds = 2.0
	// KVCSessionEvictTimeoutSeconds 会话驱逐的整体预算（秒）
	// Python: KVC_SESSION_EVICT_TIMEOUT_SECONDS = 3.0
	KVCSessionEvictTimeoutSeconds = 3.0
	// KVCTerminalCleanupTimeoutSeconds 终态清理的整体预算（秒）
	// Python: KVC_TERMINAL_CLEANUP_TIMEOUT_SECONDS = 5.0
	KVCTerminalCleanupTimeoutSeconds = 5.0
	// KVCManagementMaxAttempts KV 管理动作最大尝试次数
	// Python: KVC_MANAGEMENT_MAX_ATTEMPTS = 1
	KVCManagementMaxAttempts = 1
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveKVCActionTimeout 解析单个 KV 管理动作的整体预算。
// Python: kv_cache_config.py:16 resolve_kvc_action_timeout
func ResolveKVCActionTimeout(action, target string, timeout *float64) float64 {
	// Python: 1. if timeout is not None: return float(timeout)
	if timeout != nil {
		return *timeout
	}
	// Python: 2. if target in {"messages", "tools"}: return KVC_RANGE_ACTION_TIMEOUT_SECONDS
	if target == "messages" || target == "tools" {
		return KVCRangeActionTimeoutSeconds
	}
	// Python: 3. if action == "evict": return KVC_SESSION_EVICT_TIMEOUT_SECONDS
	if action == "evict" {
		return KVCSessionEvictTimeoutSeconds
	}
	// Python: 4. return KVC_SESSION_OFFLOAD_PREFETCH_TIMEOUT_SECONDS
	return KVCSessionOffloadPrefetchTimeoutSeconds
}
