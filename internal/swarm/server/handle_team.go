package server

import (
	"context"
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	teamharness "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// handleTeamMqPublishRelay 把一条外部团队事件中继进活跃的核心团队 runtime。
//
// Python: handle_team_mq_publish (jiuwenswarm/server/handlers/team.py:659-684)：
//
//	1. session_id 缺失 → ok=False, "session_id is required"
//	2. params.payload 缺失 → ok=False, "payload is required"
//	3. payload 非 dict 或 type != "team.external_event" → ok=False, "invalid_external_event"
//	4. get_team_manager(channel_id).interact(session_id, payload)
//	5. 成功 → {"published": true}；失败 → {"error": reason}
//
// ⤴️25.6 回填登记：25.6 原文含「team.mq.publish 混合事件」，本 handler 为
// 15.16b 端到端出站链的承载件，随本批提前落地（最小实现）。
func (s *AgentServer) handleTeamMqPublishRelay(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	sessionID := ""
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}
	channelID := request.ChannelID
	if channelID == "" {
		channelID = "web"
	}
	var params map[string]any
	_ = json.Unmarshal(request.Params, &params)
	payload, hasPayload := params["payload"]

	var success bool
	reason := ""
	switch {
	case sessionID == "":
		success, reason = false, "session_id is required"
	case !hasPayload || payload == nil:
		success, reason = false, "payload is required"
	default:
		// Python: payload.get("type") != "team.external_event" → invalid_external_event
		if _, err := events.ExternalTeamEventFromWire(payload); err != nil {
			success, reason = false, "invalid_external_event"
		} else if _, ok := payload.(map[string]any); !ok {
			success, reason = false, "invalid_external_event"
		} else {
			// Python: get_team_manager(channel_id).interact(session_id, payload)
			// TeamManager.Interact 永不抛错，返回 bool（S-07）。
			success = teamharness.GetTeamManager(channelID).Interact(ctx, sessionID,
				interaction.NewInteractInput(payload))
			if !success {
				reason = "interact_failed"
			}
		}
	}

	respPayload := map[string]any{"error": reason}
	if success {
		respPayload = map[string]any{"published": true}
	} else {
		logger.Warn(logger.ComponentAgentServer).
			Str("session_id", sessionID).
			Str("reason", reason).
			Msg("team.mq.publish failed")
	}
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(respPayload),
	), nil
}

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
