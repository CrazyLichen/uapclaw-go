package session

import (
	"os"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// heartbeatSessionPrefix 心跳会话目录前缀，不参与 session.list 等列表展示，
	// 对齐 Python _HEARTBEAT_SESSION_PREFIX = "heartbeat_"
	heartbeatSessionPrefix = "heartbeat_"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// RemoveTeamModeSessionDirsAtStartup AgentServer 启动时删除 metadata.json 中 mode 为 team 的会话目录。
//
// 对齐 Python remove_team_mode_session_dirs_at_startup()
func RemoveTeamModeSessionDirsAtStartup() {
	sessionsDir := GetSessionsDir()
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		logger.Warn(logComponent).Err(err).Msg("扫描会话目录失败")
		return
	}

	removed := 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		meta := ReadSessionMetadata(sessionsDir, sessionID)
		if meta == nil {
			continue
		}
		mode, _ := meta["mode"].(string)
		if mode != "team" {
			continue
		}

		sessionDirPath := sessionsDir + "/" + sessionID
		if err := os.RemoveAll(sessionDirPath); err != nil {
			logger.Warn(logComponent).Err(err).Str("session_id", sessionID).Msg("删除 team 会话目录失败")
			continue
		}
		RemoveSessionMetadataCache(sessionID)
		removed++
	}

	if removed > 0 {
		logger.Info(logComponent).Int("removed", removed).Msg("启动清理：已删除 team 模式会话目录")
	}
}

// GetAllSessionsMetadata 分页获取所有会话元数据。
//
// 对齐 Python get_all_sessions_metadata(limit, offset) → (sessions, total)
// 按 last_message_at 降序排列，跳过 heartbeat_ 前缀的会话。
func GetAllSessionsMetadata(limit int, offset int) ([]map[string]any, int) {
	sessionsDir := GetSessionsDir()
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return []map[string]any{}, 0
	}

	var sessions []map[string]any
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionID := entry.Name()
		// 跳过心跳会话
		if strings.HasPrefix(sessionID, heartbeatSessionPrefix) {
			continue
		}

		meta := ReadSessionMetadata(sessionsDir, sessionID)
		if meta == nil {
			// 无 metadata.json 的旧会话：构造最小信息，不读取 history.json
			// 对齐 Python: 避免大量旧会话导致接口变慢
			info, statErr := entry.Info()
			mtime := float64(0)
			if statErr == nil {
				mtime = float64(info.ModTime().UnixMilli()) / 1000.0
			}
			meta = map[string]any{
				"session_id":      sessionID,
				"channel_id":      "",
				"user_id":         "",
				"created_at":      mtime,
				"last_message_at": mtime,
				"title":           "",
				"message_count":   0,
				"mode":            "unknown",
			}
		}
		meta["session_id"] = sessionID
		sessions = append(sessions, meta)
	}

	// 按 last_message_at 降序排列，对齐 Python sessions.sort(key=lambda x: x.get("last_message_at", 0), reverse=True)
	sort.Slice(sessions, func(i, j int) bool {
		iv, _ := sessions[i]["last_message_at"].(float64)
		jv, _ := sessions[j]["last_message_at"].(float64)
		return iv > jv
	})

	total := len(sessions)
	if offset >= total {
		return []map[string]any{}, total
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return sessions[offset:end], total
}

// ──────────────────────────── 非导出函数 ────────────────────────────
