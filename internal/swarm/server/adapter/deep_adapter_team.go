package adapter

import (
	"context"
	"fmt"
	"reflect"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	agentschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// findTeamSkillRail 查找 TeamSkillEvolutionRail。
// Python: _find_team_skill_rail() (line 3651-3670)
func (d *DeepAdapter) findTeamSkillRail() *evolution.TeamSkillEvolutionRail {
	if d.teamSkillEvolutionRail != nil {
		return d.teamSkillEvolutionRail
	}
	// 在 instance 的已注册 rails 中查找
	if d.instance != nil {
		found := d.instance.FindRailsByType(reflect.TypeOf(&evolution.TeamSkillEvolutionRail{}))
		if len(found) > 0 {
			if r, ok := found[0].(*evolution.TeamSkillEvolutionRail); ok {
				d.teamSkillEvolutionRail = r
				return r
			}
		}
	}
	return nil
}

// findSkillCreateRail 查找 TeamSkillCreateRail。
// 对齐 Python: _find_skill_create_rail()
func (d *DeepAdapter) findSkillCreateRail() *evolution.TeamSkillCreateRail {
	if d.skillCreateRail != nil {
		return d.skillCreateRail
	}
	// 在 instance 的已注册 rails 中查找
	if d.instance != nil {
		found := d.instance.FindRailsByType(reflect.TypeOf(&evolution.TeamSkillCreateRail{}))
		if len(found) > 0 {
			if r, ok := found[0].(*evolution.TeamSkillCreateRail); ok {
				return r
			}
		}
	}
	return nil
}

// handleTeamSkillEvolveApproval 处理 team skill 演进审批。
//
// 对齐 Python: handle_team_skill_evolve_approval(request_id, answers, session_id, channel_id)
//
//	(1) 查找 TeamSkillEvolutionRail
//	(2) 解析 answers → approve/reject
//	(3) 调用 rail.ApproveRecord 或 rail.RejectRecord
//	(4) 推送解决状态
func (d *DeepAdapter) handleTeamSkillEvolveApproval(ctx context.Context, requestID string, answers any, sessionID string, channelID string) bool {
	rail := d.findTeamSkillRail()
	if rail == nil {
		logger.Warn(logComponent).
			Str("request_id", requestID).
			Msg("handleTeamSkillEvolveApproval: TeamSkillEvolutionRail 未初始化")
		return false
	}

	// 解析 answers 为 approve/reject
	parsedAnswers := parseApprovalAnswersFromAny(answers)
	approved := parseApprovalAnswers(parsedAnswers)

	if approved {
		err := rail.ApproveRecord(ctx, requestID)
		if err != nil {
			logger.Error(logComponent).Err(err).
				Str("request_id", requestID).
				Msg("handleTeamSkillEvolveApproval: ApproveRecord 失败")
			return false
		}
		logger.Info(logComponent).
			Str("request_id", requestID).
			Msg("handleTeamSkillEvolveApproval: 已批准")

		// 推送解决状态
		_ = d.pushTeamSkillEvolveResolutionStatus(ctx, requestID, "approved")
		return true
	}

	err := rail.RejectRecord(ctx, requestID)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("request_id", requestID).
			Msg("handleTeamSkillEvolveApproval: RejectRecord 失败")
		return false
	}
	logger.Info(logComponent).
		Str("request_id", requestID).
		Msg("handleTeamSkillEvolveApproval: 已拒绝")

	// 推送解决状态
	_ = d.pushTeamSkillEvolveResolutionStatus(ctx, requestID, "rejected")
	return true
}

// pushTeamSkillEvolveResolutionStatus 推送 team skill 演进解决状态。
//
// 对齐 Python: _push_team_skill_evolve_resolution_status(request_id, status)
//
//	将审批结果通过 stream 事件推送给前端
func (d *DeepAdapter) pushTeamSkillEvolveResolutionStatus(ctx context.Context, requestID string, status string) error {
	logger.Info(logComponent).
		Str("request_id", requestID).
		Str("status", status).
		Msg("pushTeamSkillEvolveResolutionStatus: 审批结果已推送")
	// Python: 通过 stream event 推送，当前 Go 端仅记录日志
	// 完整的 stream 推送需要 d.instance 的 stream 支持
	return nil
}

// optionMatches 检查选项是否匹配用户选择。
// Python: _option_matches() (line 3769-3790)
func (d *DeepAdapter) optionMatches(option map[string]any, answers any) bool {
	if option == nil || answers == nil {
		return false
	}
	optionID, _ := option["id"].(string)
	if optionID == "" {
		return false
	}

	// answers 可能是 []any 或 map[string]any
	switch a := answers.(type) {
	case []any:
		for _, item := range a {
			if m, ok := item.(map[string]any); ok {
				if id, ok := m["id"].(string); ok && id == optionID {
					return true
				}
			}
			if s, ok := item.(string); ok && s == optionID {
				return true
			}
		}
	case map[string]any:
		if id, ok := a["id"].(string); ok && id == optionID {
			return true
		}
	}
	return false
}

// processTeamMessageStream team 模式流式消息处理。
// 对齐 Python: team_helpers.process_team_message_stream()
// 9.55: 实现核心分流逻辑
//
// 参数分工对齐 Python：
//   - req（AgentRequest）：元信息 — session_id, request_id, channel_id
//   - inputs：业务参数 — query, params.team_name 等
func (d *DeepAdapter) processTeamMessageStream(ctx context.Context, req *agentschema.AgentRequest, inputs map[string]any) (<-chan *agentschema.AgentResponseChunk, error) {
	logger.Info(logComponent).Str("mode", "team").Msg("processTeamMessageStream: team 模式分流")

	ch := make(chan *agentschema.AgentResponseChunk, 64)

	// 步骤 1: 获取 TeamRuntimeManager
	mgr := runtime.GetTeamRuntimeManager()
	if mgr == nil {
		close(ch)
		return ch, fmt.Errorf("TeamRuntimeManager 不可用")
	}

	// 步骤 2: 从 req 获取元信息（对齐 Python: request.session_id / request.request_id / request.channel_id）
	sessionID := ""
	if req.SessionID != nil {
		sessionID = *req.SessionID
	}
	_ = req.RequestID  // 预留：后续可用于请求追踪
	_ = req.ChannelID  // 预留：后续可用于频道路由

	// 从 inputs 获取业务参数（对齐 Python: inputs.get("query", ""), inputs.get("params", {})）
	teamName := ""
	if params, ok := inputs["params"].(map[string]any); ok {
		if tn, ok := params["team_name"].(string); ok {
			teamName = tn
		}
	}

	go func() {
		defer close(ch)
		// 步骤 3: 判断是否首次请求
		entry := mgr.PoolEntry().GetEntry(teamName)
		if entry == nil {
			// 首次请求：创建 TeamAgent
			logger.Info(logComponent).Str("team_name", teamName).Msg("processTeamMessageStream: 首次请求")
			// TODO(#9.85): TeamRunner 完整创建 TeamAgent + streaming 流程
			return
		}

		// 步骤 4: 后续请求：通过 Interact 发送
		query := paramsString(inputs, "query", "")
		result, err := mgr.Interact(ctx, query, teamName, sessionID)
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("processTeamMessageStream: Interact 失败")
			return
		}
		if !result.IsOK() {
			logger.Warn(logComponent).Msg("processTeamMessageStream: Interact 返回失败")
		}
	}()

	return ch, nil
}
