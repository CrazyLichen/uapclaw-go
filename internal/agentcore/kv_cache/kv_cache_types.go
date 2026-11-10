package kv_cache

// ──────────────────────────── 结构体 ────────────────────────────

// KVCacheIdentity provider 面向的 KV cache 血缘身份。
// Python: KVCacheIdentity (core/kv_cache/kv_cache_types.py L12-17)
type KVCacheIdentity struct {
	// CacheID 本会话占用的缓存标识
	CacheID string
	// ParentCacheID 可复用的父级缓存标识
	ParentCacheID string
}
