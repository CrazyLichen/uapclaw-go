package kv_cache

import (
	"errors"
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LineageSession 解析会话血缘需要的最小会话能力。
// Python: resolve_session_lineage(session: Any) 用 getattr 逐个探测方法，
// Go 用窄接口表达同一组能力；kv_cache 包不反向 import session 包。
type LineageSession interface {
	// GetSessionID 会话标识
	GetSessionID() string
	// GetCacheIdentity provider 侧缓存身份
	GetCacheIdentity() KVCacheIdentity
	// GetParentSessionID 父会话标识，无父时返回空串
	GetParentSessionID() string
	// GetEnv 读取会话环境变量
	GetEnv(key string, defaultValue ...any) any
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// KVCacheAffinitySessionIDEnv 指定缓存身份的 env 键
	// Python: KV_CACHE_AFFINITY_SESSION_ID_ENV (kv_cache_metadata.py L13)
	KVCacheAffinitySessionIDEnv = "kv_cache_affinity_session_id"
	// KVCacheAffinityParentSessionIDEnv 指定父级缓存身份的 env 键
	// Python: KV_CACHE_AFFINITY_PARENT_SESSION_ID_ENV (kv_cache_metadata.py L14)
	KVCacheAffinityParentSessionIDEnv = "kv_cache_affinity_parent_session_id"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// contextCompressorCacheSuffixes 压缩器类型到稳定身份后缀的映射。
// Python: kv_cache_metadata.py:16-20 _CONTEXT_COMPRESSOR_CACHE_SUFFIXES
var contextCompressorCacheSuffixes = map[string]string{
	"RoundLevelCompressor":   "round-level",
	"CurrentRoundCompressor": "current-round",
	"DialogueCompressor":     "dialogue",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ContextCompressorCacheIdentity 返回某个上下文压缩器的稳定子身份。
// Python: kv_cache_metadata.py:23 context_compressor_cache_identity
// 两处 raise ValueError → Go 保留 error（用户决策：Python 抛异常时返回 error 是对的）。
func ContextCompressorCacheIdentity(ownerCacheID, compressorType string) (KVCacheIdentity, error) {
	// Python: 1. normalized_owner = str(owner_cache_id or "").strip()
	normalized := strings.TrimSpace(ownerCacheID)
	// Python: 2. if not normalized_owner: raise ValueError("owner_cache_id is required")
	if normalized == "" {
		return KVCacheIdentity{}, errors.New("owner_cache_id is required")
	}
	// Python: 3. try: suffix = _CONTEXT_COMPRESSOR_CACHE_SUFFIXES[compressor_type]
	//         except KeyError as exc: raise ValueError(...) from exc
	suffix, ok := contextCompressorCacheSuffixes[compressorType]
	if !ok {
		return KVCacheIdentity{}, fmt.Errorf("unsupported context compressor type: %s", compressorType)
	}
	// Python: 4. return KVCacheIdentity(cache_id=..., parent_cache_id=normalized_owner)
	return KVCacheIdentity{
		CacheID:       normalized + ":compressor:" + suffix,
		ParentCacheID: normalized,
	}, nil
}

// SelfParentKwargs 返回「自己为自己父」的 affinity kwargs。
// Python: kv_cache_metadata.py:41 self_parent_kwargs
func SelfParentKwargs(cacheID string) map[string]any {
	return map[string]any{"session_id": cacheID, "parent_session_id": cacheID}
}

// TeamMemberCacheIdentity 返回一个 Team 成员的稳定 KV cache 身份。
// 三个入参缺一不可——成员之间共享产品会话 id，必须靠 team/member 区分缓存。
// Python: team_member_cache_identity (kv_cache_metadata.py L45-53)
func TeamMemberCacheIdentity(teamSessionID, teamID, memberID string) (string, error) {
	if teamSessionID == "" || teamID == "" || memberID == "" {
		return "", errors.New("team_session_id, team_id and member_id are required")
	}
	return fmt.Sprintf("team:%s:team:%s:member:%s", teamSessionID, teamID, memberID), nil
}

// ResolveSessionLineage 解析当前与父会话 id（AscendAffinity hints）。
// Python: kv_cache_metadata.py:56 resolve_session_lineage
// ★ 严格对齐：Python 单参数、返回 tuple[str|None, str|None]；Go 不加 fallback 参数、不加 error。
func ResolveSessionLineage(sess LineageSession) (string, string) {
	// Python: 1. if session is None or not hasattr(session, "get_session_id"): return None, None
	if sess == nil {
		return "", ""
	}
	// Python: 2. _normalize_identity(value): 非 string 返回 None，strip 后空串返回 None
	normalize := func(v any) string {
		s, ok := v.(string)
		if !ok {
			return ""
		}
		return strings.TrimSpace(s)
	}
	sessionID := normalize(sess.GetSessionID())

	// Python: 3. get_cache_identity = getattr(session, ...); if callable: identity = get_cache_identity()
	identity := sess.GetCacheIdentity()
	cacheID := normalize(identity.CacheID)
	if cacheID != "" {
		// Python: return cache_id, parent_cache_id or cache_id
		parent := normalize(identity.ParentCacheID)
		if parent == "" {
			parent = cacheID
		}
		return cacheID, parent
	}

	// Python: 4. cache_session_id = _normalize_identity(session.get_env(KV_CACHE_AFFINITY_SESSION_ID_ENV))
	cacheSessionID := normalize(sess.GetEnv(KVCacheAffinitySessionIDEnv))
	// Python: 5. parent_session_id = _normalize_identity(get_parent_session_id())
	parentSessionID := normalize(sess.GetParentSessionID())
	// Python: 6. if not parent_session_id and hasattr(session, "get_env"):
	//              parent_session_id = _normalize_identity(session.get_env(KV_CACHE_AFFINITY_PARENT_SESSION_ID_ENV))
	if parentSessionID == "" {
		parentSessionID = normalize(sess.GetEnv(KVCacheAffinityParentSessionIDEnv))
	}
	// Python: 7. resolved_session_id = cache_session_id or session_id
	resolved := cacheSessionID
	if resolved == "" {
		resolved = sessionID
	}
	// Python: 8. return resolved_session_id, parent_session_id or resolved_session_id
	if parentSessionID == "" {
		parentSessionID = resolved
	}
	return resolved, parentSessionID
}

// FirstChangedIndex 返回新旧序列首个不同的下标；纯追加返回 nil。
// Python: kv_cache_metadata.py:88 first_changed_index（泛型逐元素 old_item != new_item）
func FirstChangedIndex[T comparable](old, new []T) *int {
	// Python: 1. for idx, (old_item, new_item) in enumerate(zip(old, new)): if != return idx
	n := len(old)
	if len(new) < n {
		n = len(new)
	}
	for i := 0; i < n; i++ {
		if old[i] != new[i] {
			idx := i
			return &idx
		}
	}
	// Python: 2. if len(new) < len(old): return len(new)
	if len(new) < len(old) {
		idx := len(new)
		return &idx
	}
	// Python: 3. return None
	return nil
}

// MessageRangeKwargs 构造 messages 变更区间参数。
// Python: kv_cache_metadata.py:98 message_range_kwargs
func MessageRangeKwargs(firstChanged, endExclusive int) map[string]any {
	return map[string]any{"msg_start": firstChanged, "msg_end": endExclusive}
}

// ToolsRangeKwargs 构造 tools 变更区间参数。
// Python: kv_cache_metadata.py:102 tools_range_kwargs
func ToolsRangeKwargs(firstChanged, endExclusive int) map[string]any {
	return map[string]any{"tools_start": firstChanged, "tools_end": endExclusive}
}
