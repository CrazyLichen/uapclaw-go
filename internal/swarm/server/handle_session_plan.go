package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
)

// handleSessionPlanStatus 只读查询当前会话是否处于计划模式。
// 不调用 SwitchMode / EnsureState，不修改活跃集合；单 agent 有 live 实例时以
// DeepAgentState.PlanMode 为准（可纠正 metadata 仍是 *.plan、agent 已退出的情况），
// 否则回退 metadata.mode。team 会话以 metadata.mode 为准——同 session 上常有为
// Goal 等 RPC 拉起的 DeepAdapter，其默认 plan_mode=normal 会把 team.*.plan 误判。
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
	// cache_bust 的语义是「读取当前真相」：本进程的元数据写入是异步队列，
	// 同一轮 chat 刚结束时 mode 更新可能尚未落盘——不排空队列就会从磁盘
	// 读到陈旧 mode（实测：退出 plan 后 plan_status 偶发返回 in_plan=true）。
	// 先 flush 再强制读盘（磁盘尚无文件时回退内存缓存）。
	_ = session.FlushMetadataQueue(2.0)
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
		// GetAgentNoWait 全空参在多 agent 渠道按 map 遍历返回任意一个
		// （Python dict 是插入序，Go map 无序），拿到的不一定是承接本会话
		// 的 agent——读到的 plan 状态可能是另一个实例的陈旧快照（实测：
		// 退出 plan 后 plan_status 偶发返回 in_plan=true）。只有该 agent
		// 真持有该会话的运行时才采信 live 快照，否则回退 metadata/active。
		if agent := s.agentManager.GetAgentNoWait(request.ChannelID, "", "", ""); agent != nil && agent.HasSessionRuntime(sid) {
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
// Python: AgentWebSocketServer._combine_session_in_plan
func combineSessionInPlan(livePlanMode string, active bool, metadataMode string) bool {
	if schema.IsTeamMode(metadataMode) {
		if active {
			return true
		}
		return schema.IsPlanMode(metadataMode)
	}
	if strings.TrimSpace(livePlanMode) != "" {
		live := strings.TrimSpace(livePlanMode)
		// live=plan 与 metadata=normal 矛盾且无 active 标记：live 快照可能来自
		// 未承接本会话的 agent（GetAgentNoWait 全空参多 agent 渠道按 map 遍历
		// 返回任意一个，Go map 无序），metadata mode 由每轮 chat 显式同步且
		// 读取前已 flush，取值更新，以 metadata 为准。
		if live == "plan" && !schema.IsPlanMode(metadataMode) && !active {
			return false
		}
		return live == "plan"
	}
	if active {
		return true
	}
	return schema.IsPlanMode(metadataMode)
}

// planStatusErrorResponse 构造 plan_status 的失败响应（payload 直接带 code）。
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
