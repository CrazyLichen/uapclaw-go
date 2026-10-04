package server

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	singleagentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/sessionops"
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

// handleSessionRewind 处理 session.rewind 请求，对齐 Python _handle_session_rewind_full(restore_files=False)。
func (s *AgentServer) handleSessionRewind(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params struct {
		SessionID string `json:"session_id"`
		TurnIndex int    `json:"turn_index"`
	}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, fmt.Errorf("解析参数失败: %w", err)
		}
	}
	if params.SessionID == "" {
		return nil, fmt.Errorf("session_id required")
	}
	if params.TurnIndex < 1 {
		return nil, fmt.Errorf("turn_index must be >= 1")
	}

	// Python: 1. rewind_session
	rewindResult, err := sessionops.RewindSession(sessionops.RewindSessionParams{
		SessionID: params.SessionID,
		TurnIndex: params.TurnIndex,
	})
	if err != nil {
		return nil, err
	}

	// Python: 2. resolve_rewind_agent → rewind_session_context
	rewindContext := false
	deepAgent := s.resolveRewindAgent(request.ChannelID)
	if deepAgent != nil {
		rewindContext = sessionops.RewindSessionContext(ctx, deepAgent, params.SessionID, params.TurnIndex)
	}

	payload := map[string]any{
		"session_id":        rewindResult.SessionID,
		"turn_index":        rewindResult.TurnIndex,
		"content":           rewindResult.Content,
		"content_preview":   rewindResult.ContentPreview,
		"remaining_records": rewindResult.RemainingRecords,
		"removed_records":   rewindResult.RemovedRecords,
		"rewind_context":    rewindContext,
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(payload),
	), nil
}

// handleSessionRewindAndRestore 处理 session.rewind_and_restore 请求，对齐 Python _handle_session_rewind_full(restore_files=True)。
func (s *AgentServer) handleSessionRewindAndRestore(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params struct {
		SessionID string `json:"session_id"`
		TurnIndex int    `json:"turn_index"`
	}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, fmt.Errorf("解析参数失败: %w", err)
		}
	}
	if params.SessionID == "" {
		return nil, fmt.Errorf("session_id required")
	}
	if params.TurnIndex < 1 {
		return nil, fmt.Errorf("turn_index must be >= 1")
	}

	// Python: 1. restore_session_files
	restoreResult := sessionops.RestoreSessionFiles(params.SessionID, params.TurnIndex)

	// Python: 2. rewind_session
	rewindResult, err := sessionops.RewindSession(sessionops.RewindSessionParams{
		SessionID: params.SessionID,
		TurnIndex: params.TurnIndex,
	})
	if err != nil {
		return nil, err
	}

	// Python: 3. resolve_rewind_agent → rewind_session_context
	rewindContext := false
	deepAgent := s.resolveRewindAgent(request.ChannelID)
	if deepAgent != nil {
		rewindContext = sessionops.RewindSessionContext(ctx, deepAgent, params.SessionID, params.TurnIndex)
	}

	payload := map[string]any{
		"session_id":        rewindResult.SessionID,
		"turn_index":        rewindResult.TurnIndex,
		"content":           rewindResult.Content,
		"content_preview":   rewindResult.ContentPreview,
		"remaining_records": rewindResult.RemainingRecords,
		"removed_records":   rewindResult.RemovedRecords,
		"rewind_context":    rewindContext,
		"restored_files":    restoreResult.RestoredFiles,
		"deleted_files":     restoreResult.DeletedFiles,
		"restore_errors":    restoreResult.Errors,
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(payload),
	), nil
}

// handleSessionRewindContext 处理 session.rewind_context 请求，对齐 Python _handle_session_rewind_context。
func (s *AgentServer) handleSessionRewindContext(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params struct {
		SessionID string `json:"session_id"`
		TurnIndex int    `json:"turn_index"`
	}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, fmt.Errorf("解析参数失败: %w", err)
		}
	}
	if params.SessionID == "" {
		return nil, fmt.Errorf("session_id required")
	}
	if params.TurnIndex < 1 {
		return nil, fmt.Errorf("turn_index must be >= 1")
	}

	// Python: 先截断 history 再重建上下文
	rewindResult, err := sessionops.RewindSession(sessionops.RewindSessionParams{
		SessionID: params.SessionID,
		TurnIndex: params.TurnIndex,
	})
	if err != nil {
		return nil, err
	}

	// Python: resolve_rewind_agent，如果 nil → 返回错误
	deepAgent := s.resolveRewindAgent(request.ChannelID)
	if deepAgent == nil {
		return nil, fmt.Errorf("no agent instance available")
	}

	rewindContext := sessionops.RewindSessionContext(ctx, deepAgent, params.SessionID, params.TurnIndex)

	payload := map[string]any{
		"session_id":        rewindResult.SessionID,
		"turn_index":        rewindResult.TurnIndex,
		"content":           rewindResult.Content,
		"content_preview":   rewindResult.ContentPreview,
		"remaining_records": rewindResult.RemainingRecords,
		"removed_records":   rewindResult.RemovedRecords,
		"rewind_context":    rewindContext,
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(payload),
	), nil
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

// handleSessionFork 处理 session.fork 请求，对齐 Python _handle_session_fork。
func (s *AgentServer) handleSessionFork(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params struct {
		SourceSessionID string `json:"source_session_id"`
		TargetSessionID string `json:"target_session_id"`
		Title           string `json:"title"`
	}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, fmt.Errorf("解析参数失败: %w", err)
		}
	}
	if params.SourceSessionID == "" {
		return nil, fmt.Errorf("source_session_id required")
	}
	if params.TargetSessionID == "" {
		return nil, fmt.Errorf("target_session_id required")
	}

	// Python: 1. fork_session
	forkResult, err := sessionops.ForkSession(sessionops.ForkSessionParams{
		SourceSessionID: params.SourceSessionID,
		TargetSessionID: params.TargetSessionID,
		Title:           params.Title,
		ChannelID:       request.ChannelID,
	})
	if err != nil {
		return nil, err
	}

	// Python: 2. resolve_rewind_agent → copy_session_context
	deepAgent := s.resolveRewindAgent(request.ChannelID)
	if deepAgent != nil {
		sessionops.CopySessionContext(ctx, deepAgent, params.SourceSessionID, params.TargetSessionID)
	}

	// Python: 3. copy_session_state
	var card *singleagentschema.AgentCard
	if deepAgent != nil {
		card = deepAgent.Card()
	}
	sessionops.CopySessionState(ctx, params.SourceSessionID, params.TargetSessionID, card, deepAgent)

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"session_id":        forkResult.SessionID,
			"source_session_id": forkResult.SourceSessionID,
			"title":             forkResult.Title,
		}),
	), nil
}

// handleHistoryListTurns 处理 history.list_turns 请求，对齐 Python _handle_history_list_turns。
func (s *AgentServer) handleHistoryListTurns(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params struct {
		SessionID string `json:"session_id"`
	}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, fmt.Errorf("解析参数失败: %w", err)
		}
	}
	if params.SessionID == "" {
		return nil, fmt.Errorf("session_id required")
	}

	result := sessionops.ListSessionTurns(params.SessionID)

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"turns": result.Turns,
			"total": result.Total,
		}),
	), nil
}

// handleSessionRestoreFiles 处理 session.restore_files 请求，对齐 Python _handle_session_restore_files。
func (s *AgentServer) handleSessionRestoreFiles(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	var params struct {
		SessionID string `json:"session_id"`
		TurnIndex int    `json:"turn_index"`
	}
	if request.Params != nil {
		if err := json.Unmarshal(request.Params, &params); err != nil {
			return nil, fmt.Errorf("解析参数失败: %w", err)
		}
	}
	if params.SessionID == "" {
		return nil, fmt.Errorf("session_id required")
	}
	if params.TurnIndex < 1 {
		return nil, fmt.Errorf("turn_index must be >= 1")
	}

	result := sessionops.RestoreSessionFiles(params.SessionID, params.TurnIndex)

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"session_id":     result.SessionID,
			"turn_index":     result.TurnIndex,
			"restored_files": result.RestoredFiles,
			"deleted_files":  result.DeletedFiles,
			"errors":         result.Errors,
		}),
	), nil
}

// resolveRewindAgent 获取指定渠道的 DeepAgent 实例，用于 rewind/fork 操作。
// Python: AgentWebSocketServer._resolve_rewind_agent(channel_id)
func (s *AgentServer) resolveRewindAgent(channelID string) *harness.DeepAgent {
	if channelID == "" {
		channelID = "default"
	}
	agent := s.agentManager.GetAgentNoWait(channelID, "", "", "")
	if agent == nil {
		return nil
	}
	return agent.GetInstance()
}
