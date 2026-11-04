package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// handleSessionPlanStatus 只读查询当前会话是否处于计划模式。
//
// 不调用 SwitchMode / EnsureState，不修改活跃集合；单 agent 有 live 实例时以
// DeepAgentState.PlanMode 为准（可纠正 metadata 仍是 *.plan、agent 已退出的情况），
// 否则回退 metadata.mode。team 会话以 metadata.mode 为准——同 session 上常有为
// Goal 等 RPC 拉起的 DeepAdapter，其默认 plan_mode=normal 会把 team.*.plan 误判。
//
// Python: jiuwenswarm/server/handlers/session.py handle_session_plan_status
// （agent_ws_server.py:3779 _handle_session_plan_status 同构）
func (s *AgentServer) handleSessionPlanStatus(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	params := map[string]any{}
	if len(request.Params) > 0 {
		_ = json.Unmarshal(request.Params, &params)
	}
	sid, _ := params["session_id"].(string)
	sid = strings.TrimSpace(sid)
	if sid == "" && request.SessionID != nil {
		sid = strings.TrimSpace(*request.SessionID)
	}
	if sid == "" {
		return planStatusErrorResponse(request, "session_id is required", "BAD_REQUEST"), nil
	}

	// Python: get_session_metadata(cache_bust=True, enable_writeback=False)。
	// 先强制读盘（避免用本进程陈旧缓存覆盖 Gateway 刚写入的状态）；磁盘尚无文件时
	// 回退内存缓存——元数据写入是异步队列，同一轮 chat 刚结束时存在短暂未落盘窗口。
	meta := session.ReadSessionMetadata(session.GetSessionsDir(), sid)
	if len(meta) == 0 {
		meta = session.GetSessionMetadata(sid)
	}
	if len(meta) == 0 {
		return planStatusErrorResponse(request, "session not found", "NOT_FOUND"), nil
	}
	metadataMode, _ := meta["mode"].(string)
	livePlanMode := ""
	if !schema.IsTeamMode(metadataMode) {
		if agent := s.agentManager.GetAgentNoWait(request.ChannelID, "", "", ""); agent != nil {
			if snapshot, ok := agent.PlanModeSnapshot(ctx, sid); ok {
				livePlanMode = snapshot
			}
		}
	}
	_, active := s.planController.ActiveSessions()[sid]
	inPlan := combineSessionInPlan(livePlanMode, active, metadataMode)

	return &schema.AgentResponse{
		RequestID: request.RequestID,
		ChannelID: request.ChannelID,
		OK:        true,
		Payload: map[string]any{
			"session_id": sid,
			"in_plan":    inPlan,
		},
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// combineSessionInPlan 合并 live plan_mode、进程内活跃标记与 metadata.mode。
//
// Python: AgentWebSocketServer._combine_session_in_plan
func combineSessionInPlan(livePlanMode string, active bool, metadataMode string) bool {
	if schema.IsTeamMode(metadataMode) {
		if active {
			return true
		}
		return schema.IsPlanMode(metadataMode)
	}
	if strings.TrimSpace(livePlanMode) != "" {
		return strings.TrimSpace(livePlanMode) == "plan"
	}
	if active {
		return true
	}
	return schema.IsPlanMode(metadataMode)
}

// planStatusErrorResponse 构造 plan_status 的失败响应（payload 直接带 code）。
//
// Python: payload={"error": ..., "code": ...}
func planStatusErrorResponse(request *schema.AgentRequest, message, code string) *schema.AgentResponse {
	return &schema.AgentResponse{
		RequestID: request.RequestID,
		ChannelID: request.ChannelID,
		OK:        false,
		Payload: map[string]any{
			"error": message,
			"code":  code,
		},
	}
}
