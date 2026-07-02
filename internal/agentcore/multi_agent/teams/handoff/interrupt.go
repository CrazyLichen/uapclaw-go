package handoff

import (
	"context"
	"errors"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamInterruptSignal 团队中断信号
type TeamInterruptSignal struct {
	// Result 中断结果，包含 result_type="interrupt"
	Result map[string]any
	// Message 中断消息
	Message string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ExtractInterruptSignal 从调用结果中提取中断信号。
// 路径1：result["result_type"] == "interrupt"（ReActAgent 返回中断时 err 为 nil）
// 路径2：errors.As(err, &AgentInterrupt)
// 无中断时返回 nil
func ExtractInterruptSignal(result map[string]any, err error) *TeamInterruptSignal {
	// 路径1：结果 dict 包含 result_type="interrupt"
	if result != nil {
		if rt, ok := result["result_type"]; ok {
			if rtStr, ok := rt.(string); ok && rtStr == "interrupt" {
				msg := ""
				if m, ok := result["message"]; ok {
					if mStr, ok := m.(string); ok {
						msg = mStr
					}
				}
				return &TeamInterruptSignal{
					Result:  result,
					Message: msg,
				}
			}
		}
	}

	// 路径2：err 为 AgentInterrupt 类型
	var agentInterrupt *interaction.AgentInterrupt
	if errors.As(err, &agentInterrupt) {
		msg := ""
		if m, ok := agentInterrupt.Message.(string); ok {
			msg = m
		}
		return &TeamInterruptSignal{
			Result:  map[string]any{"result_type": "interrupt"},
			Message: msg,
		}
	}

	return nil
}

// FlushTeamSession 刷新团队会话，尝试 CloseStream + Commit，失败仅记录警告
func FlushTeamSession(ctx context.Context, sess *session.AgentTeamSession) error {
	if sess == nil {
		return nil
	}

	// 关闭流
	if err := sess.CloseStream(); err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Err(err).
			Str("action", "flush_team_session").
			Str("session_id", sess.GetSessionID()).
			Msg("FlushTeamSession CloseStream 失败")
	}

	// 提交检查点
	if err := sess.Commit(ctx); err != nil {
		logger.Warn(logger.ComponentAgentCore).
			Err(err).
			Str("action", "flush_team_session").
			Str("session_id", sess.GetSessionID()).
			Msg("FlushTeamSession Commit 失败")
		return err
	}

	return nil
}
