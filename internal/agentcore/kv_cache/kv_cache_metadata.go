package kv_cache

import (
	"errors"
	"fmt"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// KVCacheAffinitySessionIDEnv 指定缓存身份的 env 键
	// Python: KV_CACHE_AFFINITY_SESSION_ID_ENV (kv_cache_metadata.py L13)
	KVCacheAffinitySessionIDEnv = "kv_cache_affinity_session_id"
	// KVCacheAffinityParentSessionIDEnv 指定父级缓存身份的 env 键
	// Python: KV_CACHE_AFFINITY_PARENT_SESSION_ID_ENV (kv_cache_metadata.py L14)
	KVCacheAffinityParentSessionIDEnv = "kv_cache_affinity_parent_session_id"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TeamMemberCacheIdentity 返回一个 Team 成员的稳定 KV cache 身份。
// 三个入参缺一不可——成员之间共享产品会话 id，必须靠 team/member 区分缓存。
// Python: team_member_cache_identity (kv_cache_metadata.py L45-53)
func TeamMemberCacheIdentity(teamSessionID, teamID, memberID string) (string, error) {
	if teamSessionID == "" || teamID == "" || memberID == "" {
		return "", errors.New("team_session_id, team_id and member_id are required")
	}
	return fmt.Sprintf("team:%s:team:%s:member:%s", teamSessionID, teamID, memberID), nil
}
