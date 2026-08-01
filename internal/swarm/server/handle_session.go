package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// ──────────────────────────── 结构体 ────────────────────────────

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

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleSessionList 处理 session.list 请求，对齐 Python _handle_session_list。
//
// 扫描 ~/.uapclaw/agent/sessions/ 目录，读取每个子目录的 metadata.json，
// 按 last_message_at 降序排列，返回会话列表。
func (s *AgentServer) handleSessionList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	sessions, _ := session.GetAllSessionsMetadata(10000, 0)

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
				"error": "session_id 不能为空",
				"code":  "BAD_REQUEST",
			}),
		), nil
	}

	// 委托 session 子包处理三种语义
	result, err := session.ApplySessionRename(target, params.Title, request.ChannelID)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("session_id", target).
			Msg("session.rename 失败")
		return nil, err
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(result),
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
				"error": "session_id 不能为空",
				"code":  "BAD_REQUEST",
			}),
		), nil
	}

	sessionsDir := session.GetSessionsDir()
	sessionDir := filepath.Join(sessionsDir, params.SessionID)

	if err := os.RemoveAll(sessionDir); err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("session_id", params.SessionID).
			Msg("删除会话目录失败")
		return nil, fmt.Errorf("删除会话目录失败: %w", err)
	}

	// 清理内存缓存，对齐 Python remove_session_metadata_cache()
	session.RemoveSessionMetadataCache(params.SessionID)

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
		sessionID = session.MakeSessionID()
	}

	// 委托 session 子包初始化元数据（同步写，确保创建后立即可读）
	session.InitSessionMetadata(sessionID, request.ChannelID, "", "", "unknown", "")

	logger.Info(logComponent).
		Str("session_id", sessionID).
		Msg("会话已创建")

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"session_id": sessionID}),
	), nil
}

// handleSessionFork 处理 session.fork 请求。stub：返回 NOT_IMPLEMENTED。
func (s *AgentServer) handleSessionFork(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return notImplementedResponse(request)
}
