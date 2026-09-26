package metadata

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TeamsKey session state 中 teams 命名空间的顶层 key
	// Python: TEAMS_KEY = "teams"
	TeamsKey = "teams"
	// TeamDBStateKey 桶内 db_state 字段 key
	// Python: TEAM_DB_STATE_KEY = "db_state"
	TeamDBStateKey = "db_state"
	// TeamDBStatePendingCreate DB 表待创建
	// Python: TEAM_DB_STATE_PENDING_CREATE = "pending_create"
	TeamDBStatePendingCreate = "pending_create"
	// TeamDBStateCreated DB 表已创建
	// Python: TEAM_DB_STATE_CREATED = "created"
	TeamDBStateCreated = "created"
	// TeamDBStateCleaned DB 表已清理
	// Python: TEAM_DB_STATE_CLEANED = "cleaned"
	TeamDBStateCleaned = "cleaned"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ReadTeamsBucket 读取整个 teams 命名空间，空时返回空 map（不返回 nil）。
// 对齐 Python 返回 {} 而非 None。
// Python: read_teams_bucket(session) -> dict[str, dict[str, Any]]
func ReadTeamsBucket(sess interfaces.SessionFacade) map[string]map[string]any {
	teamsAny, _ := sess.GetState(state.StringKey(TeamsKey))
	if teamsAny == nil {
		return map[string]map[string]any{}
	}
	teamsMap, ok := teamsAny.(map[string]any)
	if !ok {
		return map[string]map[string]any{}
	}
	result := make(map[string]map[string]any, len(teamsMap))
	for k, v := range teamsMap {
		if bucket, ok := v.(map[string]any); ok {
			result[k] = bucket
		}
	}
	return result
}

// ReadTeamNamespace 读取单个 team 桶，不存在时返回 nil。
// Python: read_team_namespace(session, team_name) -> dict | None
func ReadTeamNamespace(sess interfaces.SessionFacade, teamName string) map[string]any {
	bucket := ReadTeamsBucket(sess)[teamName]
	if bucket == nil {
		return nil
	}
	return bucket
}

// ReadTeamNamesInSession 列出 session 中已持久化的 team 名。
// Python: read_team_names_in_session(session) -> list[str]
func ReadTeamNamesInSession(sess interfaces.SessionFacade) []string {
	teams := ReadTeamsBucket(sess)
	names := make([]string, 0, len(teams))
	for k := range teams {
		names = append(names, k)
	}
	return names
}

// ReadTeamDBState 读取 db_state 字段值，不存在时返回空字符串。
// Python: read_team_db_state(session, team_name) -> str | None
func ReadTeamDBState(sess interfaces.SessionFacade, teamName string) string {
	bucket := ReadTeamNamespace(sess, teamName)
	if bucket == nil {
		return ""
	}
	value, ok := bucket[TeamDBStateKey]
	if !ok {
		return ""
	}
	strVal, ok := value.(string)
	if !ok {
		return ""
	}
	return strVal
}

// WriteTeamNamespace 整体覆盖某 team 桶。
// Python: write_team_namespace(session, team_name, payload) -> None
func WriteTeamNamespace(sess interfaces.SessionFacade, teamName string, payload map[string]any) {
	teams := readTeamsBucketMutable(sess)
	teams[teamName] = payload
	sess.UpdateState(map[string]any{TeamsKey: teams})
}

// MergeTeamNamespace 浅合并 partial 到某 team 桶，key 覆盖同名，不动其他。
// Python: merge_team_namespace(session, team_name, partial) -> None
func MergeTeamNamespace(sess interfaces.SessionFacade, teamName string, partial map[string]any) {
	teams := readTeamsBucketMutable(sess)
	bucketAny, ok := teams[teamName]
	var bucket map[string]any
	if !ok || bucketAny == nil {
		bucket = make(map[string]any)
	} else if bucketMap, ok := bucketAny.(map[string]any); ok {
		// 深拷贝桶，避免修改原始数据
		bucket = copyMap(bucketMap)
	} else {
		bucket = make(map[string]any)
	}
	for k, v := range partial {
		bucket[k] = v
	}
	teams[teamName] = bucket
	sess.UpdateState(map[string]any{TeamsKey: teams})
}

// MergeTeamDBState 通过 MergeTeamNamespace 写入 db_state 字段。
// Python: merge_team_db_state(session, team_name, state) -> None
func MergeTeamDBState(sess interfaces.SessionFacade, teamName string, dbState string) {
	MergeTeamNamespace(sess, teamName, map[string]any{TeamDBStateKey: dbState})
}

// RemoveTeamNamespace 删除 team 桶，返回是否实际删除。
// Python: remove_team_namespace(session, team_name) -> bool
func RemoveTeamNamespace(sess interfaces.SessionFacade, teamName string) bool {
	teams := readTeamsBucketMutable(sess)
	if _, exists := teams[teamName]; !exists {
		return false
	}
	delete(teams, teamName)
	sess.UpdateState(map[string]any{TeamsKey: teams})
	return true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readTeamsBucketMutable 读取 teams 命名空间的可变副本。
// 对齐 Python：read → mutate → write back 模式需要可变副本。
func readTeamsBucketMutable(sess interfaces.SessionFacade) map[string]any {
	teamsAny, _ := sess.GetState(state.StringKey(TeamsKey))
	if teamsAny == nil {
		return make(map[string]any)
	}
	teamsMap, ok := teamsAny.(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	return copyMap(teamsMap)
}

// copyMap 浅拷贝 map[string]any。
func copyMap(m map[string]any) map[string]any {
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}
