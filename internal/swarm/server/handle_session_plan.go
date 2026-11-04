package server

import (
	"context"
	"encoding/json"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// handleSessionPlanStatus 只读查询当前会话是否处于计划模式。
//
// 不调用 SwitchMode / EnsureState，不修改活跃集合；单 agent 有 live 实例时以
// DeepAgentState.PlanMode 为准（可纠正 metadata 仍是 *.plan、agent 已退出的情况），
// 否则回退 metadata.mode。
//
// Python: jiuwenswarm/server/agent_ws_server.py:3779 _handle_session_plan_status
func (s *AgentServer) handleSessionPlanStatus(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := map[string]any{}
	if len(request.Params) > 0 {
		_ = json.Unmarshal(request.Params, &params)
	}
	sid, _ := params["session_id"].(string)
	if sid == "" && request.SessionID != nil {
		sid = *request.SessionID
	}

	planMode := false
	mode := ""
	if sid != "" {
		if agent := s.agentManager.GetAgentNoWait(request.ChannelID, "", "", ""); agent != nil {
			if snapshot, ok := agent.PlanModeSnapshot(ctx, sid); ok {
				mode = snapshot
				planMode = snapshot == "plan"
			}
		}
		if mode == "" {
			if meta := session.GetSessionMetadata(sid); meta != nil {
				mode, _ = meta["mode"].(string)
				planMode = schema.IsPlanMode(mode)
			}
		}
	}

	return &schema.AgentResponse{
		RequestID: request.RequestID,
		ChannelID: request.ChannelID,
		OK:        true,
		Payload: map[string]any{
			"session_id": sid,
			"plan_mode":  planMode,
			"mode":       mode,
		},
	}, nil
}
