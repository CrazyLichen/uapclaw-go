package message_handler

import (
	"context"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CancelAgentSessionsOnDisconnect 断连时取消指定会话的所有 Agent 任务。
//
// 对齐 Python _cancel_agent_sessions_on_disconnect (L530-573)：
// 遍历 sessionKeys（每项为 [channelID, sessionID]），
// 构造 cancel 消息并调 CancelAgentWorkForSession。
func (mh *MessageHandler) CancelAgentSessionsOnDisconnect(ctx context.Context, sessionKeys [][2]string) {
	if len(sessionKeys) == 0 {
		return
	}

	// 对齐 Python cancel_agent_sessions_on_disconnect (L530-573):
	// seen set 去重，避免重复取消同一 session
	seen := make(map[string]struct{})
	for _, key := range sessionKeys {
		channelID := key[0]
		sessionID := key[1]
		sid := strings.TrimSpace(sessionID)
		if sid == "" {
			continue
		}
		if _, ok := seen[sid]; ok {
			continue // 对齐 Python: sid in seen → continue
		}
		seen[sid] = struct{}{}

		// 构造 cancel 消息（注入 channel mode）
		cancelMsg := &schema.Message{
			ID:        sid,
			Type:      schema.MessageTypeReq,
			ChannelID: channelID,
			SessionID: sid,
			ReqMethod: schema.ReqMethodChatCancel,
			OK:        true,
		}

		mh.CancelAgentWorkForSession(ctx, cancelMsg, sid, true)

		logger.Info(logComponent).
			Str("event_type", "session_cancelled_on_disconnect").
			Str("channel_id", channelID).
			Str("session_id", sid).
			Msg("断连取消 session 任务")
	}
}
