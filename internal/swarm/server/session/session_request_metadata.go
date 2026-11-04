package session

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionRequestMetadataSync 「请求级参数 → 会话级元数据」同步入参。
//
// 字段与 Python sync_session_request_metadata 的关键字参数一一对应；
// 指针字段沿用「nil = 不写」语义（对应 Python 的 None）。
type SessionRequestMetadataSync struct {
	// SessionID 会话标识（空则直接返回，不做任何操作）
	SessionID string
	// ChannelID 渠道标识（首次锁定）
	ChannelID string
	// Mode 权威 canonical mode（仅 ExplicitModeProvided 时覆盖）
	Mode string
	// ExplicitModeProvided 请求是否显式携带 mode
	ExplicitModeProvided bool
	// Model 模型名（仅 ExplicitModelProvided 时覆盖）
	Model string
	// ExplicitModelProvided 请求是否显式携带 model
	ExplicitModelProvided bool
	// ProjectDir 请求携带的工程目录候选值（首次锁定）
	ProjectDir string
	// ProjectID 工程标识（首次锁定）
	ProjectID string
	// CronID 定时任务标识（首次锁定）
	CronID string
	// UserID 用户标识（首次锁定）
	UserID string
	// LastUserMessageAt 最后用户消息时间戳（非 nil 时覆盖）
	LastUserMessageAt *float64
	// IsChatTurn 是否真实 chat 轮次（决定是否刷新 last_message_at）
	IsChatTurn bool
	// WorkMode work/code（首次锁定，非法值不写）
	WorkMode string
	// PersistSession 持久化开关（仅 session 创建期锁定，后续只告警）
	PersistSession *bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// SyncSessionRequestMetadata 校验请求参数与磁盘 metadata 是否需要更新并按字段语义写入。
//
// 字段语义（逐条对齐上游）：
//   - project_dir：首次锁定，不可改；不一致只告警不覆盖
//   - project_id / cron_id / user_id / channel_id / work_mode：首次锁定
//   - model：显式覆盖式（仅显式携带时覆盖，避免只读 RPC 把 MODEL_NAME 写回）
//   - mode：显式覆盖式（未显式携带时保持磁盘值，不腐蚀已锁定的会话 mode）
//   - last_user_message_at：覆盖式（nil 不写）
//   - last_message_at：仅 chat 轮次刷新（agent 最后输出时间语义）
//   - persist_session：创建期锁定，字段一旦存在后续只告警
//
// 返回本会话生效的 project_dir（磁盘已锁定即返回锁定值）。
//
// Python: jiuwenswarm/server/runtime/session/session_metadata.py
// sync_session_request_metadata（L1404-1637）
func SyncSessionRequestMetadata(in SessionRequestMetadataSync) string {
	sessionID := strings.TrimSpace(in.SessionID)
	if sessionID == "" {
		return ""
	}

	// Python: _read_metadata(session_id, cache_bust=True)——强制读盘，避免用陈旧缓存
	// 覆盖 Gateway 刚落盘的 pinned/pin_order。
	metadata := ReadSessionMetadata(GetSessionsDir(), sessionID)
	effectiveProjectDir := ""
	projectDir := strings.TrimSpace(in.ProjectDir)

	if len(metadata) == 0 {
		// 会话元数据不存在：兜底新建（外部渠道隐式创建 session 的场景）
		now := CurrentTimestamp()
		resolvedWorkMode := strings.TrimSpace(in.WorkMode)
		if resolvedWorkMode == "" {
			resolvedWorkMode = defaultWorkModeForChannel(in.ChannelID)
		}
		mode := "unknown"
		if in.ExplicitModeProvided && in.Mode != "" {
			mode = in.Mode
		}
		model := ""
		if in.ExplicitModelProvided && in.Model != "" {
			model = in.Model
		}
		persistSession := in.PersistSession != nil && *in.PersistSession
		lastUserMessageAt := now
		if in.LastUserMessageAt != nil {
			lastUserMessageAt = *in.LastUserMessageAt
		}
		metadata = map[string]any{
			"session_id":              sessionID,
			"channel_id":              in.ChannelID,
			"user_id":                 in.UserID,
			"created_at":              now,
			"last_message_at":         now,
			"title":                   "",
			"message_count":           0,
			"mode":                    mode,
			"team_name":               "",
			"team_template_id":        "",
			"round_id":                0,
			"project_dir":             projectDir,
			"project_id":              in.ProjectID,
			"persist_session":         persistSession,
			"model":                   model,
			"cron_id":                 in.CronID,
			"last_user_message_at":    lastUserMessageAt,
			"pinned":                  false,
			"pin_order":               0,
			"status":                  "idle",
			"work_mode":               resolvedWorkMode,
		}
		effectiveProjectDir = projectDir
	} else {
		// persist_session：创建期锁定，字段一旦存在后续请求不得改变
		if _, ok := metadata["persist_session"]; !ok {
			metadata["persist_session"] = in.PersistSession != nil && *in.PersistSession
			logger.Info(logComponent).
				Str("session_id", sessionID).
				Any("persist_session", metadata["persist_session"]).
				Msg("initialized missing persist_session")
		} else {
			stored, isBool := metadata["persist_session"].(bool)
			if !isBool {
				logger.Warn(logComponent).
					Str("session_id", sessionID).
					Msg("persist_session is not boolean, failing closed to false")
				stored = false
				metadata["persist_session"] = false
			}
			if in.PersistSession != nil && *in.PersistSession != stored {
				logger.Warn(logComponent).
					Str("session_id", sessionID).
					Bool("stored", stored).
					Bool("requested", *in.PersistSession).
					Msg("persist_session is locked; ignoring inconsistent request value")
			}
		}

		// project_dir：首次锁定 / 不一致告警不覆盖
		lockedProjectDir, _ := metadata["project_dir"].(string)
		if strings.TrimSpace(lockedProjectDir) != "" {
			effectiveProjectDir = strings.TrimSpace(lockedProjectDir)
			if projectDir != "" && projectDir != effectiveProjectDir {
				logger.Warn(logComponent).
					Str("session_id", sessionID).
					Str("locked", effectiveProjectDir).
					Str("requested", projectDir).
					Msg("project_dir is locked; ignoring inconsistent request value")
			}
		} else if projectDir != "" {
			metadata["project_dir"] = projectDir
			effectiveProjectDir = projectDir
		}

		// project_id / cron_id / user_id / channel_id：首次锁定
		if in.ProjectID != "" && !hasNonEmptyString(metadata, "project_id") {
			metadata["project_id"] = in.ProjectID
		}
		if in.CronID != "" && !hasNonEmptyString(metadata, "cron_id") {
			metadata["cron_id"] = in.CronID
		}
		if in.ChannelID != "" && !hasNonEmptyString(metadata, "channel_id") {
			metadata["channel_id"] = in.ChannelID
		}
		if in.UserID != "" && !hasNonEmptyString(metadata, "user_id") {
			metadata["user_id"] = in.UserID
		}

		// work_mode：首次锁定（磁盘为空或非法时才写）
		if wm := normalizeSyncWorkMode(in.WorkMode); wm != "" {
			existing, _ := metadata["work_mode"].(string)
			if !isValidWorkMode(existing) {
				metadata["work_mode"] = wm
			}
		}

		// model：显式覆盖式
		if in.Model != "" && in.ExplicitModelProvided {
			metadata["model"] = in.Model
		}
		// last_user_message_at：覆盖式
		if in.LastUserMessageAt != nil {
			metadata["last_user_message_at"] = *in.LastUserMessageAt
		}
		// mode：显式覆盖式（写入前归一为新三段 canonical）
		if in.ExplicitModeProvided && in.Mode != "" {
			metadata["mode"] = in.Mode
		}
		// last_message_at：仅 chat 轮次刷新
		if in.IsChatTurn {
			metadata["last_message_at"] = CurrentTimestamp()
		}
	}

	EnqueueMetadataWrite(sessionID, metadata)
	if effectiveProjectDir == "" {
		if locked, ok := metadata["project_dir"].(string); ok {
			effectiveProjectDir = strings.TrimSpace(locked)
		}
	}
	return effectiveProjectDir
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// hasNonEmptyString 判断 metadata 中指定键是否已有非空字符串值。
func hasNonEmptyString(metadata map[string]any, key string) bool {
	v, ok := metadata[key].(string)
	return ok && strings.TrimSpace(v) != ""
}

// isValidWorkMode 判断 work_mode 是否为合法值。
func isValidWorkMode(raw string) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "code", "work":
		return true
	default:
		return false
	}
}

// normalizeSyncWorkMode 归一 work_mode，非法时返回空串。
func normalizeSyncWorkMode(raw string) string {
	if isValidWorkMode(raw) {
		return strings.ToLower(strings.TrimSpace(raw))
	}
	return ""
}

// defaultWorkModeForChannel 按渠道推断默认 work_mode（tui → code，其余 work）。
//
// Python: jiuwenswarm/server/runtime/session/work_mode.default_work_mode_for_channel
func defaultWorkModeForChannel(channelID string) string {
	if strings.EqualFold(strings.TrimSpace(channelID), "tui") {
		return "code"
	}
	return "work"
}
