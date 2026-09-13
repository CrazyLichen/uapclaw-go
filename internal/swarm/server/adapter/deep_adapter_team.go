package adapter

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// findTeamSkillRail 查找 TeamSkillEvolutionRail。
// Python: _find_team_skill_rail() (line 3651-3670)
// ⤵️ 10.6.3-10: Rail 实例化/注入部分依赖对应章节实现
func (d *DeepAdapter) findTeamSkillRail() *evolution.TeamSkillEvolutionRail {
	if d.teamSkillEvolutionRail != nil {
		return d.teamSkillEvolutionRail
	}
	// ⤵️ 10.6.3-10: 在 instance.rails 中查找 TeamSkillEvolutionRail
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
// Python: process_team_message_stream()
// ⤵️ 10.3.7-11: 依赖 TeamHelpers
func (d *DeepAdapter) processTeamMessageStream(ctx context.Context, req any, inputs map[string]any) error {
	// ⤵️ 10.3.7-11: team 模式分流，调用 team_helpers.process_team_message_stream
	logger.Info(logComponent).Msg("processTeamMessageStream 等待 10.3.7-11 回填")
	return nil
}
