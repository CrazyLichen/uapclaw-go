package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/sessionops"
	swruntime "github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	swarmsession "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
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
// 扫描 ~/.uapclaw/agent/sessions/ 目录，读取每个子目录的 metadata.json，
// 按 last_message_at 降序排列，返回会话列表。
func (s *AgentServer) handleSessionList(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	sessions, _ := swarmsession.GetAllSessionsMetadata(10000, 0)

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"sessions": sessions}),
	), nil
}

// handleSessionRename 处理 session.rename 请求，对齐 Python apply_session_rename。
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
				Msg("session.rename failed to parse params")
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
	result, err := swarmsession.ApplySessionRename(target, params.Title, request.ChannelID)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("session_id", target).
			Msg("session.rename failed")
		return nil, err
	}

	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(result),
	), nil
}

// handleSessionSwitch 处理 session.switch 请求。
// 10.3.28b：改为两阶段事务（prepare → commit → 失败 abort）。
// Python: agent_ws_server.py _handle_session_switch
func (s *AgentServer) handleSessionSwitch(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := parseSessionParams(request)
	input := swruntime.SessionSwitchInput{
		ChannelID:         request.ChannelID,
		TargetSessionID:   paramString(params, "session_id"),
		PreviousSessionID: paramString(params, "previous_session_id"),
		Mode:              firstNonEmptyString(paramString(params, "mode"), "agent.plan"),
		PreviousMode:      paramString(params, "previous_mode"),
		TeamHint:          paramBool(params, "team"),
	}
	lease, err := s.provisioner.PrepareSessionSwitch(ctx, input)
	if err != nil {
		return sessionProvisionErrorResponse(request, err), nil
	}
	commitCtx := swruntime.SessionProvisionCommitContext{
		ForegroundScopeID: firstNonEmptyString(paramString(params, "view_id"),
			"ws:"+request.RequestID),
	}
	committed, commitErr := s.provisioner.CommitSessionProvision(
		lease, swruntime.SessionProvisionCommitTimingBeforeResultDelivery, &commitCtx)
	if commitErr != nil {
		_ = s.provisioner.AbortSessionProvision(lease)
		return sessionProvisionErrorResponse(request, commitErr), nil
	}
	switched, ok := committed.(swruntime.SessionSwitchResult)
	if !ok {
		return sessionProvisionErrorResponse(request, fmt.Errorf(
			"session.switch commit returned %T, want SessionSwitchResult", committed)), nil
	}
	logger.Info(logComponent).
		Str("session_id", switched.SessionID).
		Str("mode", switched.Mode).
		Msg("session switched")
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"session_id":  switched.SessionID,
			"mode":        switched.Mode,
			"switched":    switched.Switched,
			"moa_enabled": switched.MoaEnabled,
		}),
	), nil
}

// handleSessionDelete 处理 session.delete 请求。
// 10.3.28c：改为 provisioner 的七阶段事务（原 147 行行内实现已删除）。
// 阶段顺序与回滚分界见设计文档 §6.1；本函数只做参数解析与结果映射。
// Python: agent_ws_server.py _handle_session_delete
func (s *AgentServer) handleSessionDelete(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := parseSessionParams(request)
	target := strings.TrimSpace(paramString(params, "session_id"))
	if target == "" {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": "session_id is required",
				"code":  "BAD_REQUEST",
			}),
		), nil
	}
	result, err := s.provisioner.DeleteSession(ctx, swruntime.DeleteSessionOptions{
		ChannelID:      request.ChannelID,
		SessionID:      target,
		QuiesceSession: s.quiesceSessionForDelete,
		DisposeSession: s.disposeSessionAfterResourceRelease,
	})
	if err != nil {
		return nil, err
	}
	if !result.OK {
		logger.Warn(logComponent).
			Str("session_id", target).
			Str("code", result.ErrorCode).
			Bool("recovery_required", result.RecoveryRequired).
			Msg("session.delete failed")
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error":             result.ErrorMessage,
				"code":              result.ErrorCode,
				"deleted":           result.Deleted,
				"recovery_required": result.RecoveryRequired,
			}),
		), nil
	}
	s.planController.ResetSession(target)
	logger.Info(logComponent).
		Str("session_id", target).
		Msg("session deleted")
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{"session_id": target}),
	), nil
}

// quiesceSessionForDelete 关闭准入并等执行停下。
// spec §6.1：QuiesceSession 置 QUIESCING → CloseSession(带 generation) 逐条取消并等 Done。
// 超时返回 SessionCloseTimeoutError，使删除事务判定为 DELETE_FAILED。
func (s *AgentServer) quiesceSessionForDelete(ctx context.Context, channelID, sessionID string) error {
	if s.coordinator == nil {
		return nil
	}
	lifecycleCtx, err := s.coordinator.QuiesceSession(sessionID, nil)
	if err != nil {
		return err
	}
	if lifecycleCtx == nil {
		// 该会话不在协调器账本内：无需清场
		return nil
	}
	timeout := 10 * time.Second
	res, closeErr := s.coordinator.CloseSession(sessionID, &lifecycleCtx.Generation, &timeout)
	if closeErr != nil {
		return closeErr
	}
	if len(res.TimedOut) > 0 {
		return &session.SessionCloseTimeoutError{
			SessionID:    sessionID,
			ExecutionIDs: res.TimedOut,
		}
	}
	return nil
}

// disposeSessionAfterResourceRelease 释放会话内存运行时。
// spec §10 R-1/R-2/R-3：三个依赖（prompt_suggestion.forks.session_deleted /
// stop_subagent_runtimes / cleanup_session_runtime）Go 侧尚未落地，此处仅
// 记告警。Runner.release 由 provisioner 在其后调用，语义不丢失。
func (s *AgentServer) disposeSessionAfterResourceRelease(_ context.Context, channelID, sessionID string) error {
	logger.Debug(logComponent).
		Str("channel_id", channelID).
		Str("session_id", sessionID).
		Msg("dispose: prompt_suggestion/subagent/cleanup_session_runtime pending (10.3.28 spec R-1/R-2/R-3)")
	return nil
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
// 从 request.Params 读取 session_id（可选，没有则生成），
// 创建会话目录和 metadata.json，返回 session_id。
func (s *AgentServer) handleSessionCreate(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := parseSessionParams(request)
	explicit := paramString(params, "persist_session")
	input := swruntime.SessionCreateInput{
		ChannelID:              request.ChannelID,
		RequestedSessionID:     paramString(params, "session_id"),
		PreviousSessionID:      paramString(params, "previous_session_id"),
		CreateToken:            paramString(params, "create_token"),
		PersistSession:         paramBool(params, "persist_session"),
		PersistSessionSupplied: explicit != "",
		Mode:                   firstNonEmptyString(paramString(params, "mode"), "agent"),
		PreviousMode:           paramString(params, "previous_mode"),
		IsSwarm:                paramBool(params, "is_swarm"),
		TeamHint:               paramBool(params, "team"),
		ProjectID:              paramString(params, "project_id"),
		ProjectDir:             paramString(params, "project_dir"),
		Cwd:                    paramString(params, "cwd"),
		WorkMode:               paramString(params, "work_mode"),
		Title:                  paramString(params, "title"),
		UserID:                 requestUserID(request),
		ModelName:              paramString(params, "model_name"),
		CronID:                 paramString(params, "cron_id"),
	}
	lease, err := s.provisioner.PrepareSessionCreate(ctx, input)
	if err != nil {
		return sessionProvisionErrorResponse(request, err), nil
	}
	created, createdErr := swruntime.CreateResultOf(lease)
	if createdErr != nil {
		return sessionProvisionErrorResponse(request, createdErr), nil
	}

	// 结果投递边界之后提交（AFTER_RESULT_DELIVERY，KVC 派发在响应之后）
	commitCtx := swruntime.SessionProvisionCommitContext{
		ForegroundScopeID: firstNonEmptyString(paramString(params, "view_id"),
			"ws:"+request.RequestID),
	}
	if _, commitErr := s.provisioner.CommitSessionProvision(
		lease, swruntime.SessionProvisionCommitTimingAfterResultDelivery, &commitCtx); commitErr != nil {
		// KVC 派发失败不撤销已返回的成功响应（commitAttemptIsTerminal=true）
		logger.Warn(logComponent).Err(commitErr).Str("session_id", created.SessionID).
			Msg("session.create post-delivery commit failed")
	}
	logger.Info(logComponent).
		Str("session_id", created.SessionID).
		Bool("created", created.Created).
		Msg("session created")
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"session_id":  created.SessionID,
			"project_id":  created.ProjectID,
			"project_dir": created.ProjectDir,
			"work_mode":   created.WorkMode,
			"created":     created.Created,
			"prewarm_hit": created.PrewarmHit,
			"moa_enabled": created.MoaEnabled,
		}),
	), nil
}

// handleSessionFork 处理 session.fork 请求。
// 10.3.28b：改为两阶段事务（BEFORE_RESULT_DELIVERY，无后置副作用）。
// Python: agent_ws_server.py _handle_session_fork
func (s *AgentServer) handleSessionFork(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := parseSessionParams(request)
	input := swruntime.SessionForkInput{
		ChannelID:       request.ChannelID,
		SourceSessionID: paramString(params, "source_session_id"),
		TargetSessionID: paramString(params, "target_session_id"),
		Title:           paramString(params, "title"),
	}
	if input.SourceSessionID == "" {
		return nil, fmt.Errorf("source_session_id required")
	}
	lease, err := s.provisioner.PrepareSessionFork(ctx, input)
	if err != nil {
		return nil, err
	}
	commitCtx := swruntime.NewEmptyCommitContext()
	committed, commitErr := s.provisioner.CommitSessionProvision(
		lease, swruntime.SessionProvisionCommitTimingBeforeResultDelivery, &commitCtx)
	if commitErr != nil {
		_ = s.provisioner.AbortSessionProvision(lease)
		return nil, commitErr
	}
	forked, ok := committed.(swruntime.SessionForkResult)
	if !ok {
		return nil, fmt.Errorf("session.fork commit returned %T, want SessionForkResult", committed)
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"session_id":        forked.SessionID,
			"source_session_id": forked.SourceSessionID,
			"title":             forked.Title,
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
