package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// sessionListParams session.list 请求参数
type sessionListParams struct{}

// sessionRenameParams session.rename 请求参数
type sessionRenameParams struct {
	// SessionID 会话标识（可选，未指定时使用 request.SessionID）
	SessionID string `json:"session_id"`
	// Title 新标题（nil=查询，空串=清除，非空=设置）
	Title *string `json:"title"`
}

// sessionDeleteParams session.delete 请求参数
type sessionDeleteParams struct {
	// SessionID 会话标识
	SessionID string `json:"session_id"`
}

// sessionCreateParams session.create 请求参数
type sessionCreateParams struct {
	// SessionID 会话标识（可选，未指定时自动生成）
	SessionID string `json:"session_id"`
}

// sessionSwitchParams session.switch 请求参数
type sessionSwitchParams struct {
	// SessionID 目标会话标识
	SessionID string `json:"session_id"`
}

// sessionMetadata 会话元数据（对应 metadata.json）
type sessionMetadata struct {
	// SessionID 会话标识
	SessionID string `json:"session_id"`
	// ChannelID 来源渠道标识
	ChannelID string `json:"channel_id,omitempty"`
	// UserID 用户标识
	UserID string `json:"user_id,omitempty"`
	// Title 会话标题
	Title string `json:"title,omitempty"`
	// CreatedAt 创建时间
	CreatedAt float64 `json:"created_at,omitempty"`
	// LastMessageAt 最后消息时间
	LastMessageAt float64 `json:"last_message_at,omitempty"`
	// MessageCount 消息计数
	MessageCount int `json:"message_count,omitempty"`
	// Mode 模式
	Mode string `json:"mode,omitempty"`
	// TeamName 团队名称
	TeamName string `json:"team_name,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// renameTitleMaxLen rename 允许的最大标题长度，对齐 Python _RENAME_TITLE_MAX_LEN
	renameTitleMaxLen = 200
	// metadataFileName 元数据文件名
	metadataFileName = "metadata.json"
	// heartbeatSessionPrefix 心跳会话前缀，不参与 session.list 展示
	heartbeatSessionPrefix = "heartbeat_"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// handleSessionList 处理 session.list 请求，对齐 Python _handle_session_list。
//
// 扫描 ~/.uapclaw/agent/sessions/ 目录，读取每个子目录的 metadata.json，
// 按 last_message_at 降序排列，返回会话列表。
func (s *AgentServer) handleSessionList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	sessionsDir := s.sessionsDir

	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return schema.NewAgentResponse(request.RequestID, request.ChannelID,
				schema.WithPayload(map[string]any{"sessions": []any{}}),
			), nil
		}
		logger.Error(logComponent).
			Err(err).
			Str("sessions_dir", sessionsDir).
			Msg("扫描会话目录失败")
		return nil, fmt.Errorf("读取会话目录失败: %w", err)
	}

	var sessions []map[string]any
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// 跳过心跳会话
		if strings.HasPrefix(entry.Name(), heartbeatSessionPrefix) {
			continue
		}

		meta := readSessionMetadata(sessionsDir, entry.Name())
		if meta == nil {
			// 无 metadata.json 的旧会话，用目录 mtime 构造最小信息
			info, statErr := entry.Info()
			mtime := float64(0)
			if statErr == nil {
				mtime = float64(info.ModTime().Unix())
			}
			sessions = append(sessions, map[string]any{
				"session_id":      entry.Name(),
				"channel_id":      "",
				"title":           "",
				"message_count":   0,
				"last_message_at": mtime,
			})
			continue
		}
		meta["session_id"] = entry.Name()
		sessions = append(sessions, meta)
	}

	// 按 last_message_at 降序排列，对齐 Python sessions.sort(key=lambda x: x.get("last_message_at", 0), reverse=True)
	sort.Slice(sessions, func(i, j int) bool {
		iv, _ := sessions[i]["last_message_at"].(float64)
		jv, _ := sessions[j]["last_message_at"].(float64)
		return iv > jv
	})

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"sessions": sessions}),
	), nil
}

// handleSessionRename 处理 session.rename 请求，对齐 Python apply_session_rename。
//
// 支持三种语义：
//   - title 为 nil：查询当前标题
//   - title 为空串（strip 后）：清除标题
//   - title 为非空串：设置标题
func (s *AgentServer) handleSessionRename(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params sessionRenameParams
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			logger.Error(logComponent).
				Err(err).
				Msg("session.rename 解析参数失败")
			return nil, fmt.Errorf("解析参数失败: %w", err)
		}
	}

	// 确定 session_id：优先 params，其次 request.SessionID
	target := params.SessionID
	if target == "" && request.SessionID != nil {
		target = *request.SessionID
	}
	target = strings.TrimSpace(target)
	if target == "" {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": "session_id is required",
				"code":  "BAD_REQUEST",
			}),
		), nil
	}

	// 读取当前 metadata
	sessionsDir := s.sessionsDir
	meta := readSessionMetadata(sessionsDir, target)

	// title 为 nil：查询模式
	if params.Title == nil {
		currentTitle := ""
		if meta != nil {
			if t, ok := meta["title"].(string); ok {
				currentTitle = t
			}
		}
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"session_id":     target,
				"title":          currentTitle,
				"previous_title": currentTitle,
			}),
		), nil
	}

	// metadata 不存在时初始化
	if meta == nil {
		meta = map[string]any{
			"session_id":      target,
			"channel_id":      "",
			"created_at":      currentTimestamp(),
			"last_message_at": currentTimestamp(),
			"title":           "",
			"message_count":   0,
			"mode":            "unknown",
		}
	}

	previousTitle := ""
	if t, ok := meta["title"].(string); ok {
		previousTitle = t
	}

	// 截断标题，对齐 Python str(raw_title).strip()[:_RENAME_TITLE_MAX_LEN]
	newTitle := strings.TrimSpace(*params.Title)
	if len(newTitle) > renameTitleMaxLen {
		newTitle = newTitle[:renameTitleMaxLen]
	}

	if newTitle != "" {
		meta["title"] = newTitle
	} else {
		meta["title"] = ""
	}
	meta["last_message_at"] = currentTimestamp()

	// 写入 metadata.json
	if err := writeSessionMetadata(sessionsDir, target, meta); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("session_id", target).
			Msg("session.rename 写入 metadata 失败")
		return nil, fmt.Errorf("写入 metadata 失败: %w", err)
	}

	updatedTitle := ""
	if t, ok := meta["title"].(string); ok {
		updatedTitle = t
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"session_id":     target,
			"title":          updatedTitle,
			"previous_title": previousTitle,
		}),
	), nil
}

// handleSessionSwitch 处理 session.switch 请求。stub：返回 ok=true。
func (s *AgentServer) handleSessionSwitch(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"ok": true}),
	), nil
}

// handleSessionDelete 处理 session.delete 请求，对齐 Python _handle_session_delete。
//
// 从 request.Params 读取 session_id，删除会话目录。
func (s *AgentServer) handleSessionDelete(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params sessionDeleteParams
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			logger.Error(logComponent).
				Err(err).
				Msg("session.delete 解析参数失败")
			return nil, fmt.Errorf("解析参数失败: %w", err)
		}
	}

	if params.SessionID == "" {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": "session_id is required",
				"code":  "BAD_REQUEST",
			}),
		), nil
	}

	sessionsDir := s.sessionsDir
	sessionDir := filepath.Join(sessionsDir, params.SessionID)

	if err := os.RemoveAll(sessionDir); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("session_id", params.SessionID).
			Msg("删除会话目录失败")
		return nil, fmt.Errorf("删除会话目录失败: %w", err)
	}

	logger.Info(logComponent).
		Str("session_id", params.SessionID).
		Msg("会话已删除")

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"session_id": params.SessionID}),
	), nil
}

// handleSessionRewind 处理 session.rewind 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleSessionRewind(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// handleSessionRewindAndRestore 处理 session.rewind_and_restore 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleSessionRewindAndRestore(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// handleSessionRewindContext 处理 session.rewind_context 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleSessionRewindContext(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// handleSessionCreate 处理 session.create 请求，对齐 Python _handle_session_create。
//
// 从 request.Params 读取 session_id（可选，没有则生成），
// 创建会话目录和 metadata.json，返回 session_id。
func (s *AgentServer) handleSessionCreate(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params sessionCreateParams
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			logger.Error(logComponent).
				Err(err).
				Msg("session.create 解析参数失败")
			return nil, fmt.Errorf("解析参数失败: %w", err)
		}
	}

	sessionID := params.SessionID
	if sessionID == "" {
		sessionID = makeSessionID()
	}

	sessionsDir := s.sessionsDir
	sessionDir := filepath.Join(sessionsDir, sessionID)

	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("session_id", sessionID).
			Msg("创建会话目录失败")
		return nil, fmt.Errorf("创建会话目录失败: %w", err)
	}

	// 写入 metadata.json，对齐 Python init_session_metadata
	ts := currentTimestamp()
	meta := map[string]any{
		"session_id":      sessionID,
		"channel_id":      "",
		"created_at":      ts,
		"last_message_at": ts,
		"title":           "",
		"message_count":   0,
		"mode":            "unknown",
	}
	if err := writeSessionMetadata(sessionsDir, sessionID, meta); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("session_id", sessionID).
			Msg("写入 metadata.json 失败")
		return nil, fmt.Errorf("写入 metadata.json 失败: %w", err)
	}

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Msg("会话已创建")

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"sessionId": sessionID}),
	), nil
}

// handleSessionFork 处理 session.fork 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleSessionFork(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// readSessionMetadata 读取会话元数据文件。
//
// 不产生副作用：session 目录不存在时返回 nil 而非创建目录，
// 对齐 Python _read_metadata 的"读路径不应产生副作用"原则。
func readSessionMetadata(sessionsDir, sessionID string) map[string]any {
	metaPath := filepath.Join(sessionsDir, sessionID, metadataFileName)
	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil
	}
	var meta map[string]any
	if err := json.Unmarshal(data, &meta); err != nil {
		logger.Warn(logComponent).
			Err(err).
			Str("session_id", sessionID).
			Msg("读取 metadata.json 失败")
		return nil
	}
	return meta
}

// writeSessionMetadata 写入会话元数据文件。
func writeSessionMetadata(sessionsDir, sessionID string, meta map[string]any) error {
	metaPath := filepath.Join(sessionsDir, sessionID, metadataFileName)
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(metaPath, data, 0o644)
}

// makeSessionID 生成会话标识，对齐 Python _make_session_id 和前端 generateSessionId。
//
// 格式：sess_{hex_timestamp}_{6_random_hex}
func makeSessionID() string {
	ts := strconv.FormatInt(time.Now().UnixMilli(), 16)
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return fmt.Sprintf("sess_%s_%x", ts, suffix)
}

// currentTimestamp 返回当前 UTC 时间戳（秒），对齐 Python _current_timestamp。
func currentTimestamp() float64 {
	return float64(time.Now().UnixMilli()) / 1000.0
}
