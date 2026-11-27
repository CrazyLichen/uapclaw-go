package store

import (
	"encoding/json"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session/lifecycle"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ToSessionInfo 会话元数据 → SessionInfo 投影（排除内部字段，补齐兜底值）。
// Python: jiuwenswarm/server/runtime/session/session_info.py to_session_info。
// 与 Web fallback _to_session_info 输出完全一致，保证 session.list 外部接口行为不变。
func ToSessionInfo(meta map[string]any) map[string]any {
	lum, lumIsNum := numericAny(meta["last_user_message_at"])
	var lumOut any
	if lumIsNum {
		lumOut = lum
	}
	return map[string]any{
		"session_id":           stringFromAny(meta["session_id"]),
		"title":                stringFromAny(meta["title"]),
		"created_at":           anyOrZero(meta["created_at"]),
		"last_message_at":      anyOrZero(meta["last_message_at"]),
		"message_count":        intFromAny(meta["message_count"]),
		"mode":                 orDefault(stringFromAny(meta["mode"]), "unknown"),
		"team_name":            stringFromAny(meta["team_name"]),
		"agent_group_name":     stringFromAny(meta["agent_group_name"]),
		"pinned":               boolFromAny(meta["pinned"]),
		"pin_order":            intFromAny(meta["pin_order"]),
		"project_dir":          stringFromAny(meta["project_dir"]),
		"project_id":           stringFromAny(meta["project_id"]),
		"cron_id":              stringFromAny(meta["cron_id"]),
		"last_user_message_at": lumOut,
		"model":                stringFromAny(meta["model"]),
		"model_selection":      meta["model_selection"],
		"moa_enabled":          boolFromAny(meta["moa_enabled"]),
		"work_mode":            orDefault(stringFromAny(meta["work_mode"]), WorkModeWeb),
		"history_path":         stringFromAny(meta["history_path"]),
		"lifecycle_operation":  meta["lifecycle_operation"],
		"execution_blocked":    boolFromAny(meta["execution_blocked"]),
		"stop_pending":         boolFromAny(meta["stop_pending"]),
	}
}

// sessionsDirOverride 会话目录测试注入点（生产恒走 GetSessionsDir；
// WorkspaceDir 槽进程级 memoize，测试需要目录可替换的收集口径）。
var sessionsDirOverride string

// CollectAllSessionsMetadata 全量收集会话元数据（不分页）。
// Python: session_metadata.py collect_all_sessions_metadata（:2367）。
// 读取逻辑与 GetAllSessionsMetadata 相同（跳过 heartbeat_ 前缀、补 session_id），
// 额外支持测试注入目录。
func CollectAllSessionsMetadata() []map[string]any {
	dir := sessionsDirOverride
	if dir == "" {
		all, _ := swarmsession.GetAllSessionsMetadata(1<<20, 0)
		return all
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return []map[string]any{}
	}
	var sessions []map[string]any
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), "heartbeat_") {
			continue
		}
		meta := swarmsession.ReadSessionMetadata(dir, entry.Name())
		if meta == nil {
			meta = map[string]any{}
		}
		meta["session_id"] = entry.Name()
		sessions = append(sessions, meta)
	}
	return sessions
}

// SyncSessionRequestMetadataBackfill cron 会话名回填 cron_id/project_id 的薄封装。
// Python: session_metadata.py sync_session_request_metadata（首次锁定语义，只写空值）。
func SyncSessionRequestMetadataBackfill(sessionID, cronID, projectID string) {
	if sessionsDirOverride != "" {
		// 测试注入路径：直接按首次锁定语义写 metadata.json（生产走
		// SyncSessionRequestMetadata 全链路）。
		meta := swarmsession.ReadSessionMetadata(sessionsDirOverride, sessionID)
		if meta == nil {
			meta = map[string]any{}
		}
		if _, exists := meta["cron_id"]; !exists {
			meta["cron_id"] = cronID
		}
		if _, exists := meta["project_id"]; !exists {
			meta["project_id"] = projectID
		}
		_ = swarmsession.WriteSessionMetadata(sessionsDirOverride, sessionID, meta)
		return
	}
	swarmsession.SyncSessionRequestMetadata(swarmsession.SessionRequestMetadataSync{
		SessionID: sessionID,
		CronID:    cronID,
		ProjectID: projectID,
	})
}

// LifecycleProjectionForProject 项目生命周期投影。
// Python: lifecycle/projection.py 以 ("project", project_id) 调用。
func LifecycleProjectionForProject(projectID string) (map[string]any, bool) {
	projection := lifecycle.Projection("project", projectID, "", nil, nil, nil)
	projectionJSON, _ := json.Marshal(projection)
	var out map[string]any
	_ = json.Unmarshal(projectionJSON, &out)
	return out, out != nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// numericAny 数值 any（排除 bool）。
func numericAny(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case int:
		return float64(n), true
	}
	return 0, false
}

// anyOrZero 缺省 0。
func anyOrZero(v any) any {
	if v == nil {
		return float64(0)
	}
	return v
}

// orDefault 空串回退。
func orDefault(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
