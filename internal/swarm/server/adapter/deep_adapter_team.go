package adapter

import (
	"context"

	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// findTeamSkillRail 查找 TeamSkillEvolutionRail。
// 对齐 Python: _find_team_skill_rail() (line 3651-3670)
// ⤵️ 10.6.3-10: 依赖 TeamSkillEvolutionRail
func (d *DeepAdapter) findTeamSkillRail() sainterfaces.AgentRail {
	// ⤵️ 10.6.3-10: 在 instance.rails 中查找 TeamSkillEvolutionRail
	return nil
}

// handleTeamSkillEvolveApproval 处理 team skill 演进审批。
// 对齐 Python: handle_team_skill_evolve_approval() (line 3651-3767)
// ⤵️ 10.6.3-10: 依赖 TeamSkillEvolutionRail
func (d *DeepAdapter) handleTeamSkillEvolveApproval(ctx context.Context, requestID string, answers any, sessionID string, channelID string) bool {
	// ⤵️ 10.6.3-10: 实现 team skill 审批
	logger.Info(logComponent).
		Str("request_id", requestID).
		Str("session_id", sessionID).
		Msg("handleTeamSkillEvolveApproval 等待 10.6.3-10 回填")
	return false
}

// pushTeamSkillEvolveResolutionStatus 推送 team skill 演进解决状态。
// 对齐 Python: _push_team_skill_evolve_resolution_status() (line 3768-3790)
// ⤵️ 10.6.3-10: 依赖 TeamSkillEvolutionRail
func (d *DeepAdapter) pushTeamSkillEvolveResolutionStatus(ctx context.Context, requestID string, status string) error {
	// ⤵️ 10.6.3-10: 推送演进审批结果
	logger.Info(logComponent).
		Str("request_id", requestID).
		Str("status", status).
		Msg("pushTeamSkillEvolveResolutionStatus 等待 10.6.3-10 回填")
	return nil
}

// optionMatches 检查选项是否匹配用户选择。
// 对齐 Python: _option_matches() (line 3769-3790)
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
// 对齐 Python: process_team_message_stream()
// ⤵️ 10.3.7-11: 依赖 TeamHelpers
func (d *DeepAdapter) processTeamMessageStream(ctx context.Context, req any, inputs map[string]any) error {
	// ⤵️ 10.3.7-11: team 模式分流，调用 team_helpers.process_team_message_stream
	logger.Info(logComponent).Msg("processTeamMessageStream 等待 10.3.7-11 回填")
	return nil
}
