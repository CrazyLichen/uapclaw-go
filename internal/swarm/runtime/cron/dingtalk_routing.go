package cron

import "strings"

// ──────────────────────────── 全局变量 ────────────────────────────

// internalSessionPrefixes Gateway 内部会话 ID 前缀（如 dingtalk_19fb...），
// 不能当作钉钉 staffId 使用。
// Python: dingtalk_routing.py:13-28 _INTERNAL_SESSION_PREFIXES
var internalSessionPrefixes = []string{
	"dingtalk_", "feishu_", "xiaoyi_", "wecom_", "wechat_",
	"whatsapp_", "telegram_", "discord_", "slack_",
	"sess_", "cron_", "web_", "tui_", "heartbeat_",
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// dingtalkBindingPrefix 钉钉投递绑定前缀
	// Python: dingtalk_routing.py:53 "dingtalk::"
	dingtalkBindingPrefix = "dingtalk::"
	// dingtalkConversationTypeGroup 群聊
	// Python: dingtalk_routing.py:119-120 conversation_type == "2" → group
	dingtalkConversationTypeGroup = "2"
	// dingtalkConversationTypeP2P 私聊
	// Python: dingtalk_routing.py:121-122 conversation_type == "1" → p2p
	dingtalkConversationTypeP2P = "1"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// IsUsableDingtalkStaffID 判断该值能否直接作为钉钉私聊 userIds 使用。
// Python: dingtalk_routing.py:31-40 is_usable_dingtalk_staff_id
func IsUsableDingtalkStaffID(value string) bool {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return false
	}
	// 投递绑定形如 dingtalk::…，整串不是 staffId
	if strings.HasPrefix(trimmed, dingtalkBindingPrefix) {
		return false
	}
	for _, prefix := range internalSessionPrefixes {
		if strings.HasPrefix(trimmed, prefix) {
			return false
		}
	}
	return true
}

// EncodeDingtalkCronSessionID 把来源钉钉路由编码进 CronJob.session_id。
// Python: dingtalk_routing.py:43-58 encode_dingtalk_cron_session_id
//
// 格式：dingtalk::{conversation_id}::{sender_id}::{conversation_type}
func EncodeDingtalkCronSessionID(senderID, conversationID, conversationType string) string {
	sender := strings.TrimSpace(senderID)
	if !IsUsableDingtalkStaffID(sender) {
		return ""
	}
	convID := strings.TrimSpace(conversationID)
	convType := strings.TrimSpace(conversationType)
	if convType == "" {
		convType = dingtalkConversationTypeP2P
	}
	return dingtalkBindingPrefix + convID + "::" + sender + "::" + convType
}

// ParseDingtalkCronSessionID 解析投递绑定为发送元数据。
// Python: dingtalk_routing.py:61-88 parse_dingtalk_cron_session_id
//
// 非钉钉投递绑定（含 Gateway 内部 id 与裸 staffId）返回 ok=false。
func ParseDingtalkCronSessionID(sessionID string) (map[string]string, bool) {
	sid := strings.TrimSpace(sessionID)
	if !strings.HasPrefix(sid, dingtalkBindingPrefix) {
		return nil, false
	}
	parts := strings.Split(sid, "::")
	if len(parts) < 3 {
		return nil, false
	}
	convID := strings.TrimSpace(parts[1])
	sender := strings.TrimSpace(parts[2])
	convType := dingtalkConversationTypeP2P
	if len(parts) >= 4 {
		if trimmed := strings.TrimSpace(parts[3]); trimmed != "" {
			convType = trimmed
		}
	}
	if sender == "" && convID == "" {
		return nil, false
	}
	// 拒绝把内部会话 ID 误编码成 staffId 的绑定
	if sender != "" && !IsUsableDingtalkStaffID(sender) {
		return nil, false
	}
	return map[string]string{
		"dingtalk_sender_id": sender,
		"dingtalk_chat_id":   convID,
		"conversation_id":    convID,
		"conversation_type":  convType,
	}, true
}

// DingtalkChatTypeFromMetadata 把钉钉会话类型映射为 cron chat_type。
// Python: dingtalk_routing.py:91-104 dingtalk_chat_type_from_metadata
func DingtalkChatTypeFromMetadata(metadata map[string]any) *string {
	if metadata == nil {
		return nil
	}
	if chatType, ok := metadata["chat_type"].(string); ok {
		if trimmed := strings.TrimSpace(chatType); trimmed != "" {
			return &trimmed
		}
	}
	if convType, ok := metadata["conversation_type"].(string); ok {
		switch strings.TrimSpace(convType) {
		case dingtalkConversationTypeGroup:
			group := "group"
			return &group
		case dingtalkConversationTypeP2P:
			p2p := "p2p"
			return &p2p
		}
	}
	return nil
}

// BuildDingtalkCronSessionIDFromContext 从工具上下文构造绑定的钉钉会话 ID。
// Python: dingtalk_routing.py:107-133 build_dingtalk_cron_session_id_from_context
//
// 只接受真实 staffId；Gateway 内部会话 ID（dingtalk_…）绝不编码进投递绑定。
func BuildDingtalkCronSessionIDFromContext(sessionID *string, metadata map[string]any) *string {
	md := metadata
	if md == nil {
		md = map[string]any{}
	}

	sender := strings.TrimSpace(stringField(md["dingtalk_sender_id"]))
	if !IsUsableDingtalkStaffID(sender) {
		alt := ""
		if sessionID != nil {
			alt = strings.TrimSpace(*sessionID)
		}
		if IsUsableDingtalkStaffID(alt) {
			sender = alt
		} else {
			sender = ""
		}
	}
	if sender == "" {
		return nil
	}

	convID := strings.TrimSpace(stringField(md["conversation_id"]))
	if convID == "" {
		convID = strings.TrimSpace(stringField(md["dingtalk_chat_id"]))
	}
	convType := strings.TrimSpace(stringField(md["conversation_type"]))
	if convType == "" {
		convType = dingtalkConversationTypeP2P
	}

	encoded := EncodeDingtalkCronSessionID(sender, convID, convType)
	if encoded == "" {
		return nil
	}
	return &encoded
}

// ResolveDingtalkPushMetadata 从 job.session_id 解析推送元数据。
// Python: dingtalk_routing.py:136-152 resolve_dingtalk_push_metadata
//
//   - dingtalk::… 投递绑定 → 来源会话元数据
//   - 裸 staffId → 对该用户私聊
//   - Gateway 内部会话 ID（dingtalk_…）→ nil（调用方可回退 last_*）
func ResolveDingtalkPushMetadata(routingSessionID string) map[string]string {
	sid := strings.TrimSpace(routingSessionID)
	if sid == "" {
		return nil
	}
	if bound, ok := ParseDingtalkCronSessionID(sid); ok {
		return bound
	}
	if IsUsableDingtalkStaffID(sid) {
		return map[string]string{
			"dingtalk_sender_id": sid,
			"conversation_type":  dingtalkConversationTypeP2P,
		}
	}
	return nil
}
