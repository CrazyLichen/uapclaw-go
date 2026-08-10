package tools

import (
	"context"
	"fmt"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMessageManager 团队消息管理器。
// 对齐 Python: TeamMessageManager (openjiuwen/agent_teams/tools/message_manager.py)
// 薄门面，委托 db.Message() 执行 DAO 操作，通过 messager 发布事件。
type TeamMessageManager struct {
	// db 团队数据库实例（内含 MessageDao）
	db database.TeamDatabase
	// teamName 团队标识
	teamName string
	// memberName 当前成员标识
	memberName string
	// messager 事件发布器
	messager messager.Messager
	// sessionID 会话标识（用于构建 topic）
	sessionID string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamMessageManager 创建团队消息管理器。
// 对齐 Python: TeamMessageManager.__init__(team_name, member_name, db, messager)
func NewTeamMessageManager(db database.TeamDatabase, teamName, memberName string, msg messager.Messager, sessionID string) *TeamMessageManager {
	return &TeamMessageManager{
		db:         db,
		teamName:   teamName,
		memberName: memberName,
		messager:   msg,
		sessionID:  sessionID,
	}
}

// SendMessage 发送直发消息。
// 对齐 Python: TeamMessageManager.send_message()
func (tm *TeamMessageManager) SendMessage(ctx context.Context, content string, toMemberName string, fromMemberName string) (string, error) {
	sender := fromMemberName
	if sender == "" {
		sender = tm.memberName
	}
	messageID := fmt.Sprintf("msg_%s_%d_%d", tm.teamName, time.Now().UnixMilli(), time.Now().UnixNano()%1000)

	msg := &database.TeamMessageBase{
		MessageID:      messageID,
		TeamName:       tm.teamName,
		FromMemberName: sender,
		ToMemberName:   toMemberName,
		Content:        content,
		Broadcast:      false,
		IsRead:         database.BoolPtr(false),
	}
	if !tm.db.Message().CreateMessage(ctx, msg) {
		logger.Error(logger.ComponentAgentCore).Str("message_id", messageID).
			Msg("创建消息失败")
		return "", fmt.Errorf("创建消息失败: message_id 冲突 %s", messageID)
	}

	// 发布 MessageEvent
	tm.publishMessageEvent(ctx, schema.MessageEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
		MessageID:        messageID,
		FromMemberName:   sender,
		ToMemberName:     toMemberName,
	})

	return messageID, nil
}

// BroadcastMessage 广播消息。
// 对齐 Python: TeamMessageManager.broadcast_message()
func (tm *TeamMessageManager) BroadcastMessage(ctx context.Context, content string, fromMemberName string) (string, error) {
	sender := fromMemberName
	if sender == "" {
		sender = tm.memberName
	}
	messageID := fmt.Sprintf("msg_%s_%d_%d", tm.teamName, time.Now().UnixMilli(), time.Now().UnixNano()%1000)

	msg := &database.TeamMessageBase{
		MessageID:      messageID,
		TeamName:       tm.teamName,
		FromMemberName: sender,
		Content:        content,
		Broadcast:      true,
		IsRead:         nil, // 广播消息 is_read=nil
	}
	if !tm.db.Message().CreateMessage(ctx, msg) {
		logger.Error(logger.ComponentAgentCore).Str("message_id", messageID).
			Msg("创建广播消息失败")
		return "", fmt.Errorf("创建广播消息失败: message_id 冲突 %s", messageID)
	}

	// 发布 BroadcastEvent
	tm.publishMessageEvent(ctx, schema.BroadcastEvent{
		BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
		MessageID:        messageID,
		FromMemberName:   sender,
	})

	return messageID, nil
}

// GetMessages 获取直发消息。
// 对齐 Python: TeamMessageManager.get_messages()
func (tm *TeamMessageManager) GetMessages(ctx context.Context, toMemberName string, unreadOnly bool, fromMemberName string) ([]*database.TeamMessageBase, error) {
	return tm.db.Message().GetMessages(ctx, tm.teamName, toMemberName, unreadOnly, fromMemberName)
}

// GetBroadcastMessages 获取广播消息。
// 对齐 Python: TeamMessageManager.get_broadcast_messages()
func (tm *TeamMessageManager) GetBroadcastMessages(ctx context.Context, memberName string, unreadOnly bool, fromMemberName string) ([]*database.TeamMessageBase, error) {
	return tm.db.Message().GetBroadcastMessages(ctx, tm.teamName, memberName, unreadOnly, fromMemberName)
}

// GetTeamMessages 获取团队所有消息。
// 对齐 Python: TeamMessageManager.get_team_messages()
func (tm *TeamMessageManager) GetTeamMessages(ctx context.Context, teamName string) ([]*database.TeamMessageBase, error) {
	return tm.db.Message().GetTeamMessages(ctx, teamName, "")
}

// HasUnreadMessages 是否有未读消息。
// 对齐 Python: TeamMessageManager.has_unread_messages()
func (tm *TeamMessageManager) HasUnreadMessages(ctx context.Context, includeBroadcast bool) bool {
	return tm.db.Message().HasUnreadMessages(ctx, tm.teamName, includeBroadcast)
}

// MarkMessageRead 标记已读。
// 对齐 Python: TeamMessageManager.mark_message_read()
func (tm *TeamMessageManager) MarkMessageRead(ctx context.Context, messageID, memberName string) bool {
	success := tm.db.Message().MarkMessageRead(ctx, messageID, memberName)
	if success {
		logger.Debug(logger.ComponentAgentCore).Str("message_id", messageID).Str("member_name", memberName).
			Msg("消息已标记已读")
	} else {
		logger.Error(logger.ComponentAgentCore).Str("message_id", messageID).Str("member_name", memberName).
			Msg("标记消息已读失败")
	}
	return success
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// publishMessageEvent 发布消息事件到 TeamTopic。
func (tm *TeamMessageManager) publishMessageEvent(ctx context.Context, event schema.TypedEvent) {
	if tm.messager == nil {
		return
	}
	msg := schema.EventMessageFromEvent(event)
	topicID := schema.TeamTopicMessage.Build(tm.sessionID, tm.teamName)
	if err := tm.messager.Publish(ctx, topicID, msg); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).
			Str("event_type", event.EventTypeName()).
			Msg("发布消息事件失败")
	}
}
