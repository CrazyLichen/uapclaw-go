package adapter

import (
	"context"
	"reflect"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
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
			Msg("handleTeamSkillEvolveApproval: TeamSkillEvolutionRail not initialized")
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
				Msg("handleTeamSkillEvolveApproval: ApproveRecord failed")
			return false
		}
		logger.Info(logComponent).
			Str("request_id", requestID).
			Msg("handleTeamSkillEvolveApproval: approved")

		// 推送解决状态
		_ = d.pushTeamSkillEvolveResolutionStatus(ctx, requestID, "approved")
		return true
	}

	err := rail.RejectRecord(ctx, requestID)
	if err != nil {
		logger.Error(logComponent).Err(err).
			Str("request_id", requestID).
			Msg("handleTeamSkillEvolveApproval: RejectRecord failed")
		return false
	}
	logger.Info(logComponent).
		Str("request_id", requestID).
		Msg("handleTeamSkillEvolveApproval: rejected")

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
		Msg("pushTeamSkillEvolveResolutionStatus: approval result pushed")
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
// 改造后：通过 channel_id → TeamManager 路由，对齐 Python 的 get_team_manager(channel_id).interact()
// 而非通过 inputs["params"]["team_name"] 提取 teamName
//
// 参数分工对齐 Python：
//   - req（AgentRequest）：元信息 — session_id, request_id, channel_id
//   - inputs：业务参数 — query, params.team_name 等
func (d *DeepAdapter) processTeamMessageStream(ctx context.Context, req *agentschema.AgentRequest, inputs map[string]any) (<-chan *agentschema.AgentResponseChunk, error) {
	logger.Info(logComponent).Str("mode", "team").Msg("processTeamMessageStream: team mode routing")

	ch := make(chan *agentschema.AgentResponseChunk, 64)

	// 防御：req 为 nil 时直接返回空流
	if req == nil {
		close(ch)
		return ch, nil
	}

	// 步骤 1: 从 req 获取元信息（对齐 Python: request.session_id / request.request_id / request.channel_id）
	sessionID := ""
	if req.SessionID != nil {
		sessionID = *req.SessionID
	}
	channelID := "default"
	if req.ChannelID != "" {
		channelID = req.ChannelID
	}

	// 步骤 2: 通过 channel_id 获取 TeamManager
	// 对齐 Python: get_team_manager(channel_id)
	teamManager := team.GetTeamManager(channelID)

	// 步骤 3: 判断是否首次请求
	// 对齐 Python: if not team_manager.has_stream_task(session_id)
	isFirstRequest := !teamManager.HasStreamTask(sessionID)

	go func() {
		defer close(ch)

		if isFirstRequest {
			// 首次请求：创建 TeamAgent + 启动后台流任务
			logger.Info(logComponent).Str("session_id", sessionID).Str("channel_id", channelID).
				Msg("processTeamMessageStream: first request")

			// 对齐 Python 步骤：
			//  1. teamSpec = teamManager.GetEnrichedTeamSpec(session_id, deep_agent, ...)
			//  2. teamManager.PrepareRuntimeActivation(ctx, sessionID, teamSpec.TeamName)
			//  3. 创建 TeamAgent + 启动 streaming goroutine
			//  4. teamManager.CommitRuntimeReady(sessionID, teamSpec.TeamName)
			//  5. teamManager.OnRuntimeReady(ctx, sessionID, teamAgent, hideDM)
			//  5. 注册 stream task cancel
			// ⤵️(#9.85): TeamRunner 完整创建 TeamAgent + streaming 流程
			return
		}

		// 后续请求：通过 TeamManager.Interact 路由
		// 对齐 Python: team_manager.interact(session_id, user_input)
		query := paramsString(inputs, "query", "")
		ok, err := teamManager.Interact(ctx, sessionID, interaction.NewInteractInput(query))
		if err != nil {
			logger.Error(logComponent).Err(err).Str("session_id", sessionID).
				Msg("processTeamMessageStream: Interact failed")
			return
		}
		if !ok {
			logger.Warn(logComponent).Str("session_id", sessionID).
				Msg("processTeamMessageStream: Interact returned false (inactive session)")
		}
	}()

	return ch, nil
}
