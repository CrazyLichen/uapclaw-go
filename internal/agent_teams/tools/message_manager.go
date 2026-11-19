package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMessageManager 团队消息管理器。
// Python: TeamMessageManager (openjiuwen/agent_teams/tools/message_manager.py)
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
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentTeam

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamMessageManager 创建团队消息管理器。
// Python: TeamMessageManager.__init__(team_name, member_name, db, messager)
// sessionID 从 context 中获取（schema.GetSessionID(ctx)），不再作为构造参数。
func NewTeamMessageManager(db database.TeamDatabase, teamName, memberName string, msg messager.Messager) *TeamMessageManager {
	return &TeamMessageManager{
		db:         db,
		teamName:   teamName,
		memberName: memberName,
		messager:   msg,
	}
}

// MemberName 返回当前成员标识。
// Python: TeamMessageManager.member_name
func (tm *TeamMessageManager) MemberName() string { return tm.memberName }

// SendMessage 发送直发消息。
// Python: TeamMessageManager.send_message()
func (tm *TeamMessageManager) SendMessage(ctx context.Context, content string, toMemberName string, fromMemberName string) (string, error) {
	sender := fromMemberName
	if sender == "" {
		sender = tm.memberName
	}
	// Python: message_id = uuid.uuid4().hex
	messageID := uuid.New().String()

	msg := &database.TeamMessageBase{
		MessageID:      messageID,
		TeamName:       tm.teamName,
		FromMemberName: sender,
		ToMemberName:   database.StringPtr(toMemberName),
		Content:        content,
		Broadcast:      false,
		IsRead:         database.BoolPtr(false),
	}
	if !tm.db.Message().CreateMessage(ctx, msg) {
		logger.Error(logComponent).Str("message_id", messageID).
			Msg("failed to create message")
		return "", fmt.Errorf("创建消息失败: message_id 冲突 %s", messageID)
	}

	// 发布 MessageEvent
	tm.publishMessageEvent(ctx, events.MessageEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
		MessageID:        messageID,
		FromMemberName:   sender,
		ToMemberName:     toMemberName,
	})

	return messageID, nil
}

// SendMessageWithMeta 带框架投递载荷的直发消息（F_63 两阶段渲染）：
// 信箱行 content 为空、meta 携带 template key + refs/params，投递时按当前
// 任务行现场渲染。供调度器 leader 身份 handoff（_send_as_leader）使用。
// Python: message_manager.send_message(..., meta=...)（message_manager.py L60-95）
func (tm *TeamMessageManager) SendMessageWithMeta(ctx context.Context, meta map[string]any, toMemberName string) (string, error) {
	metaRaw, err := json.Marshal(meta)
	if err != nil {
		return "", fmt.Errorf("序列化 meta 失败: %w", err)
	}
	metaStr := string(metaRaw)
	// Python: sender = self.member_name —— leader 身份由 manager 持有的成员名决定
	messageID := uuid.New().String()
	msg := &database.TeamMessageBase{
		MessageID:      messageID,
		TeamName:       tm.teamName,
		FromMemberName: tm.memberName,
		ToMemberName:   database.StringPtr(toMemberName),
		Content:        "",
		Meta:           &metaStr,
		Broadcast:      false,
		IsRead:         database.BoolPtr(false),
	}
	if !tm.db.Message().CreateMessage(ctx, msg) {
		logger.Error(logComponent).Str("to_member", toMemberName).
			Msg("SendMessageWithMeta: failed to create message")
		return "", fmt.Errorf("创建消息失败: message_id 冲突 %s", msg.MessageID)
	}
	// 发布 MessageEvent 唤醒（行已落库为凭；成员启动是调用方的独立 best-effort 步骤）
	tm.publishMessageEvent(ctx, events.MessageEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
		MessageID:        msg.MessageID,
		FromMemberName:   tm.memberName,
		ToMemberName:     toMemberName,
	})
	return msg.MessageID, nil
}

// BroadcastMessage 广播消息。
// Python: TeamMessageManager.broadcast_message()
func (tm *TeamMessageManager) BroadcastMessage(ctx context.Context, content string, fromMemberName string) (string, error) {
	sender := fromMemberName
	if sender == "" {
		sender = tm.memberName
	}
	// Python: message_id = uuid.uuid4().hex
	messageID := uuid.New().String()

	msg := &database.TeamMessageBase{
		MessageID:      messageID,
		TeamName:       tm.teamName,
		FromMemberName: sender,
		Content:        content,
		Broadcast:      true,
		IsRead:         nil, // 广播消息 is_read=nil
	}
	if !tm.db.Message().CreateMessage(ctx, msg) {
		logger.Error(logComponent).Str("message_id", messageID).
			Msg("failed to create broadcast message")
		return "", fmt.Errorf("创建广播消息失败: message_id 冲突 %s", messageID)
	}

	// 发布 BroadcastEvent
	tm.publishMessageEvent(ctx, events.BroadcastEvent{
		BaseEventMessage: events.BaseEventMessage{TeamName: tm.teamName},
		MessageID:        messageID,
		FromMemberName:   sender,
	})

	return messageID, nil
}

// GetMessages 获取直发消息。
// Python: TeamMessageManager.get_messages()
func (tm *TeamMessageManager) GetMessages(ctx context.Context, toMemberName string, unreadOnly bool, fromMemberName string) ([]*database.TeamMessageBase, error) {
	return tm.db.Message().GetMessages(ctx, tm.teamName, toMemberName, unreadOnly, fromMemberName)
}

// GetBroadcastMessages 获取广播消息。
// Python: TeamMessageManager.get_broadcast_messages()
func (tm *TeamMessageManager) GetBroadcastMessages(ctx context.Context, memberName string, unreadOnly bool, fromMemberName string) ([]*database.TeamMessageBase, error) {
	return tm.db.Message().GetBroadcastMessages(ctx, tm.teamName, memberName, unreadOnly, fromMemberName)
}

// GetTeamMessages 获取团队所有消息。
// Python: TeamMessageManager.get_team_messages()
func (tm *TeamMessageManager) GetTeamMessages(ctx context.Context, teamName string) ([]*database.TeamMessageBase, error) {
	return tm.db.Message().GetTeamMessages(ctx, teamName, "")
}

// HasUnreadMessages 是否有未读消息。
// Python: TeamMessageManager.has_unread_messages()
func (tm *TeamMessageManager) HasUnreadMessages(ctx context.Context, includeBroadcast bool) bool {
	return tm.db.Message().HasUnreadMessages(ctx, tm.teamName, includeBroadcast)
}

// MarkMessageRead 标记已读。
// Python: TeamMessageManager.mark_message_read()
func (tm *TeamMessageManager) MarkMessageRead(ctx context.Context, messageID, memberName string) bool {
	success := tm.db.Message().MarkMessageRead(ctx, messageID, memberName)
	if success {
		logger.Debug(logComponent).Str("message_id", messageID).Str("member_name", memberName).
			Msg("message marked as read")
	} else {
		logger.Error(logComponent).Str("message_id", messageID).Str("member_name", memberName).
			Msg("failed to mark message as read")
	}
	return success
}

// MessageDao 返回消息 DAO（暴露 db.Message() 供 handler 层查消息体）。
// M-29: Python handler 直接 mm.db.message.get_message(message_id)，
// Go 端 db 为私有字段，经此访问器暴露（参照 Python 属性访问模式）。
func (tm *TeamMessageManager) MessageDao() database.MessageDao {
	return tm.db.Message()
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// publishMessageEvent 发布消息事件到 TeamTopic。
// sessionID 从 context 中获取（schema.GetSessionID(ctx)），对齐 Python: get_session_id()。
func (tm *TeamMessageManager) publishMessageEvent(ctx context.Context, event events.TypedEvent) {
	if tm.messager == nil {
		return
	}
	msg := events.EventMessageFromEvent(event)
	topicID := events.TeamTopicMessage.Build(schema.GetSessionID(ctx), tm.teamName)
	if err := tm.messager.Publish(ctx, topicID, msg); err != nil {
		logger.Error(logComponent).Err(err).
			Str("event_type", event.EventTypeName()).
			Msg("failed to publish message event")
	}
}
