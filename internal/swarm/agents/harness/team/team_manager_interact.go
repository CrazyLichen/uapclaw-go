package team

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Interact 向活跃的 team runtime 发送交互消息。
// 对齐 Python: TeamManager.interact(session_id, user_input)
//
// Python 步骤：
//  1. if session_id != self._active_session_id or not self._active_team_name:
//     logger.warning("interact ignored for non-active team session"); return False
//  2. team_name = self._active_team_name
//  3. success = await Runner.interact_agent_team(user_input, team_name=, session_id=)
//  4. if not success: logger.warning("interact failed against runner runtime")
//  5. return success
//
// S-07: 对齐 Python 永不抛异常——所有 error/panic 均吞掉并返回 false。
//
// Go 差异：Python 委托 Runner.interact_agent_team → TeamRuntimeManager.interact，
// Go 直接调 TeamRuntimeManager.Interact（省略 Runner 桥接层）。
// Python user_input 类型为 Any；Go 端统一为 *InteractInput，外层自行包装。
func (m *TeamManager) Interact(ctx context.Context, sessionID string, userInput *interaction.InteractInput) (success bool) {
	// S-07: 对齐 Python except Exception → return False 的兜底语义
	success = false
	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).Any("panic", r).Str("session_id", sessionID).
				Msg("Interact panic recovered")
			success = false
		}
	}()

	m.mu.Lock()
	if m.activeSessionID == "" || m.activeSessionID != sessionID || m.activeTeamName == "" {
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Str("active_session_id", displayStr(m.activeSessionID)).
			Str("active_team_name", displayStr(m.activeTeamName)).
			Msg("interact ignored: not an active team session")
		m.mu.Unlock()
		return false
	}
	teamName := m.activeTeamName
	m.mu.Unlock()

	mgr := runtime.GetTeamRuntimeManager()
	// 提取 InteractInput.Raw 传递给 TeamRuntimeManager.Interact
	var rawPayload any
	if userInput != nil {
		rawPayload = userInput.Raw
	}
	result, err := mgr.Interact(ctx, rawPayload, teamName, sessionID)
	if err != nil {
		// S-07: 对齐 Python except Exception as exc: logger.error(...); return False
		logger.Error(logComponent).Err(err).
			Str("session_id", sessionID).
			Str("team_name", teamName).
			Msg("interact failed")
		return false
	}
	if result != nil && !result.IsOK() {
		logger.Warn(logComponent).
			Str("session_id", sessionID).
			Str("team_name", teamName).
			Msg("interact failed on runner runtime")
		return false
	}
	return true
}
