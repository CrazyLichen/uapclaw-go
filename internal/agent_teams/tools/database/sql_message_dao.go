package database

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/sessionctx"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SQLMessageDao MessageDao 的 SQL 实现。
// 对齐 Python: MessageDao (openjiuwen/agent_teams/tools/database/message_dao.py)
// 操作动态表 team_message_{suffix} + message_read_status_{suffix}。
type SQLMessageDao struct {
	// db GORM 数据库实例
	db *gorm.DB
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMessage 按 ID 查消息。返回 nil 表示不存在。
// 对齐 Python: get_message(message_id) → Optional[TeamMessageBase]
func (d *SQLMessageDao) GetMessage(ctx context.Context, messageID string) (*TeamMessageBase, error) {
	var msg TeamMessageBase
	result := d.db.WithContext(ctx).Table(d.msgTableName(ctx)).Where("message_id = ?", messageID).First(&msg)
	if result.Error != nil {
		if result.Error == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, result.Error
	}
	return &msg, nil
}

// CreateMessage 创建消息。指数退避重试3次。
// 对齐 Python: create_message() → bool（OperationalError 时指数退避重试 3 次）
func (d *SQLMessageDao) CreateMessage(ctx context.Context, msg *TeamMessageBase) bool {
	msgTable := d.msgTableName(ctx)
	// 对齐 Python: backoff = [0.1, 0.3, 0.5] 秒
	backoffs := []time.Duration{100 * time.Millisecond, 300 * time.Millisecond, 500 * time.Millisecond}

	for attempt := 0; attempt <= 3; attempt++ {
		err := d.db.WithContext(ctx).Table(msgTable).Create(msg).Error
		if err == nil {
			return true
		}
		if attempt < 3 {
			// 对齐 Python: time.sleep(backoff)
			time.Sleep(backoffs[attempt])
		}
		logger.Warn(logComponent).Str("message_id", msg.MessageID).Int("attempt", attempt+1).Err(err).Msg("创建消息重试")
	}
	return false
}

// GetMessages 获取直发消息（非广播）。
// 对齐 Python: get_messages(team_name, to_member_name, unread_only, from_member_name)
func (d *SQLMessageDao) GetMessages(ctx context.Context, teamName, toMemberName string, unreadOnly bool, fromMemberName string) ([]*TeamMessageBase, error) {
	msgTable := d.msgTableName(ctx)
	query := d.db.WithContext(ctx).Table(msgTable).
		Where("team_name = ? AND to_member_name = ? AND broadcast = 0", teamName, toMemberName)
	if unreadOnly {
		// 对齐 Python: filter by is_read = False
		query = query.Where("is_read = 0")
	}
	if fromMemberName != "" {
		query = query.Where("from_member_name = ?", fromMemberName)
	}
	var messages []*TeamMessageBase
	result := query.Order("timestamp ASC").Find(&messages)
	if result.Error != nil {
		return nil, result.Error
	}
	return messages, nil
}

// GetBroadcastMessages 获取广播消息（排除自己发送的）。
// 对齐 Python: get_broadcast_messages(team_name, member_name, unread_only, from_member_name)
// 使用 read_status watermark 过滤已读
func (d *SQLMessageDao) GetBroadcastMessages(ctx context.Context, teamName, memberName string, unreadOnly bool, fromMemberName string) ([]*TeamMessageBase, error) {
	msgTable := d.msgTableName(ctx)
	rsTable := d.readStatusTableName(ctx)

	query := d.db.WithContext(ctx).Table(msgTable).
		Where("team_name = ? AND broadcast = 1 AND from_member_name != ?", teamName, memberName)

	if unreadOnly {
		// 对齐 Python: 基于 MessageReadStatus 水位线的已读判断
		// SQL 条件: timestamp > COALESCE((SELECT read_at FROM message_read_status_{suffix} WHERE member_name = ? AND team_name = ?), 0)
		subQuery := fmt.Sprintf(
			"SELECT read_at FROM %s WHERE member_name = ? AND team_name = ?",
			quoteTableName(rsTable),
		)
		query = query.Where("timestamp > COALESCE(("+subQuery+"), 0)", memberName, teamName)
	}
	if fromMemberName != "" {
		query = query.Where("from_member_name = ?", fromMemberName)
	}

	var messages []*TeamMessageBase
	result := query.Order("timestamp ASC").Find(&messages)
	if result.Error != nil {
		return nil, result.Error
	}
	return messages, nil
}

// GetTeamMessages 获取团队所有消息。broadcast 为空字符串表示不过滤。
// 对齐 Python: get_team_messages(team_name, broadcast)
func (d *SQLMessageDao) GetTeamMessages(ctx context.Context, teamName string, broadcast string) ([]*TeamMessageBase, error) {
	msgTable := d.msgTableName(ctx)
	query := d.db.WithContext(ctx).Table(msgTable).Where("team_name = ?", teamName)
	switch broadcast {
	case "true":
		query = query.Where("broadcast = 1")
	case "false":
		query = query.Where("broadcast = 0")
	}
	var messages []*TeamMessageBase
	result := query.Order("timestamp ASC").Find(&messages)
	if result.Error != nil {
		return nil, result.Error
	}
	return messages, nil
}

// HasUnreadMessages 是否有未读消息。
// 对齐 Python: has_unread_messages(team_name, include_broadcast) → bool
// 直发：检查 is_read=False；广播：检查 per-member watermark
func (d *SQLMessageDao) HasUnreadMessages(ctx context.Context, teamName string, includeBroadcast bool) bool {
	msgTable := d.msgTableName(ctx)
	rsTable := d.readStatusTableName(ctx)

	// 对齐 Python: 检查直发未读
	var count int64
	d.db.WithContext(ctx).Table(msgTable).
		Where("team_name = ? AND to_member_name != '' AND broadcast = 0 AND is_read = 0", teamName).
		Count(&count)
	if count > 0 {
		return true
	}

	if includeBroadcast {
		// 对齐 Python: 检查广播未读
		subQuery := fmt.Sprintf(
			"SELECT MAX(read_at) FROM %s WHERE team_name = ?",
			quoteTableName(rsTable),
		)
		var bcCount int64
		d.db.WithContext(ctx).Table(msgTable).
			Where("team_name = ? AND broadcast = 1 AND timestamp > COALESCE(("+subQuery+"), 0)", teamName, teamName).
			Count(&bcCount)
		return bcCount > 0
	}
	return false
}

// MarkMessageRead 标记已读。直发设 is_read=true；广播更新 read_status watermark。
// 对齐 Python: mark_message_read(message_id, member_name)
func (d *SQLMessageDao) MarkMessageRead(ctx context.Context, messageID, memberName string) bool {
	msgTable := d.msgTableName(ctx)
	rsTable := d.readStatusTableName(ctx)

	// 对齐 Python: 查消息，确定直发还是广播
	var msg TeamMessageBase
	result := d.db.WithContext(ctx).Table(msgTable).Where("message_id = ?", messageID).First(&msg)
	if result.Error != nil {
		return false
	}

	if !msg.Broadcast {
		// 对齐 Python: 直发 → UPDATE is_read = True
		d.db.WithContext(ctx).Table(msgTable).Where("message_id = ?", messageID).Update("is_read", 1)
	} else {
		// 对齐 Python: 广播 → INSERT OR UPDATE message_read_status
		// read_at = max(现有 read_at, 消息 timestamp)
		// 使用 SQLite 的 INSERT OR REPLACE / PostgreSQL 的 ON CONFLICT
		d.db.WithContext(ctx).Exec(
			fmt.Sprintf(
				"INSERT INTO %s (member_name, team_name, read_at) VALUES (?, ?, ?) "+
					"ON CONFLICT (member_name, team_name) DO UPDATE SET read_at = MAX(%s.read_at, ?)",
				quoteTableName(rsTable), quoteTableName(rsTable),
			),
			memberName, msg.TeamName, msg.Timestamp, msg.Timestamp,
		)
	}
	return true
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// withTx 返回绑定指定事务的 DAO 实例。
func (d *SQLMessageDao) withTx(tx *gorm.DB) *SQLMessageDao {
	return &SQLMessageDao{db: tx}
}

// msgTableName 获取当前 session 的消息表名。
func (d *SQLMessageDao) msgTableName(ctx context.Context) string {
	sessionID := sessionctx.GetSessionID(ctx)
	suffix := SanitizeSessionIDForTable(sessionID)
	return "team_message_" + suffix
}

// readStatusTableName 获取当前 session 的已读状态表名。
func (d *SQLMessageDao) readStatusTableName(ctx context.Context) string {
	sessionID := sessionctx.GetSessionID(ctx)
	suffix := SanitizeSessionIDForTable(sessionID)
	return "message_read_status_" + suffix
}
