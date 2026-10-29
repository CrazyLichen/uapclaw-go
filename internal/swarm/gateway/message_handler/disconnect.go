package message_handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CancelAgentSessionsOnDisconnect 断连时取消指定会话的所有 Agent 任务。
//
// Python: _cancel_agent_sessions_on_disconnect (L530-573)：
// 遍历 sessionKeys（每项为 [channelID, sessionID]），
// 构造 cancel 消息并调 CancelAgentWorkForSession。
func (mh *MessageHandler) CancelAgentSessionsOnDisconnect(ctx context.Context, sessionKeys [][2]string) {
	if len(sessionKeys) == 0 {
		return
	}

	// Python: cancel_agent_sessions_on_disconnect (L530-573):
	// seen 集合，去重避免重复取消同一 session
	seen := make(map[string]struct{})
	for _, key := range sessionKeys {
		channelID := key[0]
		sessionID := key[1]
		sid := strings.TrimSpace(sessionID)
		if sid == "" {
			continue
		}
		if _, ok := seen[sid]; ok {
			continue // Python: sid in seen → continue
		}
		seen[sid] = struct{}{}

		// 构造 cancel 消息。T-01 对齐 Python L544-564：
		// params 注入 intent + session_id（mode 由 buildCancelMessage
		// 从 msg.Params/channelStates 补充，同 Python disconnect_state
		// 查询兜底）；ID 用 ws_drop_ 前缀（毫秒十六进制 + 4 字节随机
		// hex，Python secrets.token_hex(4) 同构），排查断连取消日志时
		// 可区分来源
		paramsJSON, _ := json.Marshal(map[string]any{
			"intent":     "cancel",
			"session_id": sid,
		})
		cancelMsg := &schema.Message{
			ID:        fmt.Sprintf("ws_drop_%x_%s", time.Now().UnixMilli(), generateRandomHex(2)),
			Type:      schema.MessageTypeReq,
			ChannelID: channelID,
			SessionID: sid,
			Params:    json.RawMessage(paramsJSON),
			ReqMethod: schema.ReqMethodChatCancel,
			OK:        true,
		}

		mh.CancelAgentWorkForSession(ctx, cancelMsg, sid, true)

		logger.Info(logComponent).
			Str("event_type", "session_cancelled_on_disconnect").
			Str("channel_id", channelID).
			Str("session_id", sid).
			Msg("cancelling session tasks on disconnect")
	}
}
