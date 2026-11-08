package server

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// handleTeamDelete 处理 team.delete 请求。stub：返回 ok=true。
// handleTeamDelete 处理 team.delete 请求。
// Python: agent_ws_server.py _handle_lifecycle_request 的 team 分支（:6103）
//
// 走与会话删除相同的两阶段事务：整体静默整队 → 逐会话删 → Runner 只删一次。
// 失败时返回 failed_session_ids 与 recovery_required，让前端能提示"部分失败"。
func (s *AgentServer) handleTeamDelete(
	ctx context.Context, request *schema.AgentRequest,
) (*schema.AgentResponse, error) {
	params := parseSessionParams(request)
	teamName, _ := params["team_name"].(string)
	if teamName == "" {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": "team_name is required",
				"code":  "BAD_REQUEST",
			}),
		), nil
	}
	result, err := s.provisioner.DeleteTeam(ctx, teamName, request.ChannelID)
	if err != nil {
		return nil, err
	}
	if !result.OK {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error":              result.ErrorMessage,
				"code":               result.ErrorCode,
				"failed_session_ids": result.FailedSessionIDs,
				"recovery_required":  result.RecoveryRequired,
			}),
		), nil
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"ok":          true,
			"team_name":   result.TeamName,
			"session_ids": result.SessionIDs,
			"deleted":     result.Deleted,
		}),
	), nil
}

// handleTeamSnapshot 处理 team.snapshot 请求。stub：返回空快照。
func (s *AgentServer) handleTeamSnapshot(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"snapshot": map[string]any{},
		}),
	), nil
}

// handleTeamHistoryGet 处理 team.history.get 请求。读 team history 记录（纯文件系统），返回空列表。
func (s *AgentServer) handleTeamHistoryGet(_ context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// TODO(#team-history): 从文件系统读取 team history 记录
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(map[string]any{
			"history": []any{},
		}),
	), nil
}
