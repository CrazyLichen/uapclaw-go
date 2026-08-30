package database

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSQLMessageDao_CreateAndGet(t *testing.T) {
	db := newTestSqlDBWithSession(t, "msg-test")
	ctx := newTestCtx("msg-test")

	dao := db.Message()

	// 对齐 Python: 直发消息
	msg := &TeamMessageBase{
		MessageID:       "m1",
		TeamName:        "team1",
		FromMemberName:  "a",
		ToMemberName:    "b",
		Content:         "hello",
		Timestamp:       GetCurrentTime(),
		Broadcast:       false,
		IsRead:          BoolPtr(false),
	}
	ok := dao.CreateMessage(ctx, msg)
	assert.True(t, ok)

	got, err := dao.GetMessage(ctx, "m1")
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, "hello", got.Content)

	// 对齐 Python: GetMessage 不存在
	got2, err := dao.GetMessage(ctx, "nonexist")
	assert.NoError(t, err)
	assert.Nil(t, got2)

	// 对齐 Python: CreateMessage 重复 ID → False
	ok = dao.CreateMessage(ctx, msg)
	assert.False(t, ok)
}

func TestSQLMessageDao_BroadcastWatermark(t *testing.T) {
	db := newTestSqlDBWithSession(t, "watermark-test")
	ctx := newTestCtx("watermark-test")

	dao := db.Message()

	// 对齐 Python: 创建广播消息
	bc1 := &TeamMessageBase{
		MessageID:       "bc1",
		TeamName:        "team1",
		FromMemberName:  "a",
		Content:         "broadcast1",
		Timestamp:       1000,
		Broadcast:       true,
		IsRead:          nil,
	}
	dao.CreateMessage(ctx, bc1)

	bc2 := &TeamMessageBase{
		MessageID:       "bc2",
		TeamName:        "team1",
		FromMemberName:  "a",
		Content:         "broadcast2",
		Timestamp:       2000,
		Broadcast:       true,
		IsRead:          nil,
	}
	dao.CreateMessage(ctx, bc2)

	// 对齐 Python: 未读广播消息
	msgs, err := dao.GetBroadcastMessages(ctx, "team1", "b", true, "")
	require.NoError(t, err)
	assert.Equal(t, 2, len(msgs))

	// 对齐 Python: mark_message_read(bc1, b) — 更新 watermark
	dao.MarkMessageRead(ctx, "bc1", "b")

	// 未读广播消息应只有 bc2
	msgs2, err := dao.GetBroadcastMessages(ctx, "team1", "b", true, "")
	require.NoError(t, err)
	assert.Equal(t, 1, len(msgs2))
	assert.Equal(t, "bc2", msgs2[0].MessageID)
}

func TestSQLMessageDao_MarkMessageRead_直发(t *testing.T) {
	db := newTestSqlDBWithSession(t, "direct-read-test")
	ctx := newTestCtx("direct-read-test")

	dao := db.Message()

	msg := &TeamMessageBase{
		MessageID:       "dm1",
		TeamName:        "team1",
		FromMemberName:  "a",
		ToMemberName:    "b",
		Content:         "direct",
		Timestamp:       1000,
		Broadcast:       false,
		IsRead:          BoolPtr(false),
	}
	dao.CreateMessage(ctx, msg)

	assert.True(t, dao.HasUnreadMessages(ctx, "team1", false))

	// 对齐 Python: 直发 → is_read = True
	dao.MarkMessageRead(ctx, "dm1", "b")
	assert.False(t, dao.HasUnreadMessages(ctx, "team1", false))
}

func TestSQLMessageDao_HasUnreadMessages_广播(t *testing.T) {
	db := newTestSqlDBWithSession(t, "unread-bc-test")
	ctx := newTestCtx("unread-bc-test")

	dao := db.Message()

	// 对齐 Python: 无消息时没有未读
	assert.False(t, dao.HasUnreadMessages(ctx, "team1", true))

	// 创建广播消息
	dao.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "bc1", TeamName: "team1", FromMemberName: "a",
		Content: "broadcast", Timestamp: 1000, Broadcast: true, IsRead: nil,
	})

	// 对齐 Python: include_broadcast=true 时有未读
	assert.True(t, dao.HasUnreadMessages(ctx, "team1", true))

	// 对齐 Python: include_broadcast=false 时广播不算
	assert.False(t, dao.HasUnreadMessages(ctx, "team1", false))
}

func TestSQLMessageDao_MarkMessageRead_广播(t *testing.T) {
	db := newTestSqlDBWithSession(t, "bc-read-test")
	ctx := newTestCtx("bc-read-test")

	dao := db.Message()

	dao.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "bc1", TeamName: "team1", FromMemberName: "a",
		Content: "broadcast", Timestamp: 1000, Broadcast: true, IsRead: nil,
	})

	// 对齐 Python: 广播 → 更新 watermark
	ok := dao.MarkMessageRead(ctx, "bc1", "b")
	assert.True(t, ok)

	// 标记后广播应无未读
	assert.False(t, dao.HasUnreadMessages(ctx, "team1", true))

	// 对齐 Python: MarkMessageRead 消息不存在
	ok = dao.MarkMessageRead(ctx, "nonexist", "b")
	assert.False(t, ok)
}

func TestSQLMessageDao_GetTeamMessages(t *testing.T) {
	db := newTestSqlDBWithSession(t, "team-msg-test")
	ctx := newTestCtx("team-msg-test")

	dao := db.Message()

	dao.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "dm1", TeamName: "team1", FromMemberName: "a",
		ToMemberName: "b", Content: "direct", Timestamp: 1000, Broadcast: false, IsRead: BoolPtr(false),
	})
	dao.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "bc1", TeamName: "team1", FromMemberName: "a",
		Content: "broadcast", Timestamp: 2000, Broadcast: true, IsRead: nil,
	})

	all, err := dao.GetTeamMessages(ctx, "team1", "")
	require.NoError(t, err)
	assert.Equal(t, 2, len(all))

	bcOnly, err := dao.GetTeamMessages(ctx, "team1", "true")
	require.NoError(t, err)
	assert.Equal(t, 1, len(bcOnly))

	dmOnly, err := dao.GetTeamMessages(ctx, "team1", "false")
	require.NoError(t, err)
	assert.Equal(t, 1, len(dmOnly))
}

func TestSQLMessageDao_GetMessages_直发(t *testing.T) {
	db := newTestSqlDBWithSession(t, "dm-list-test")
	ctx := newTestCtx("dm-list-test")

	dao := db.Message()

	// 对齐 Python: 创建直发消息
	dao.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "dm1", TeamName: "team1", FromMemberName: "a",
		ToMemberName: "b", Content: "hello", Timestamp: 1000, Broadcast: false, IsRead: BoolPtr(false),
	})
	dao.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "dm2", TeamName: "team1", FromMemberName: "c",
		ToMemberName: "b", Content: "world", Timestamp: 2000, Broadcast: false, IsRead: BoolPtr(true),
	})
	dao.CreateMessage(ctx, &TeamMessageBase{
		MessageID: "bc1", TeamName: "team1", FromMemberName: "a",
		Content: "broadcast", Timestamp: 3000, Broadcast: true, IsRead: nil,
	})

	// 查询 b 收到的所有直发消息
	all, err := dao.GetMessages(ctx, "team1", "b", false, "")
	require.NoError(t, err)
	assert.Equal(t, 2, len(all))

	// 对齐 Python: unread_only=True
	unread, err := dao.GetMessages(ctx, "team1", "b", true, "")
	require.NoError(t, err)
	assert.Equal(t, 1, len(unread))
	assert.Equal(t, "dm1", unread[0].MessageID)

	// 对齐 Python: from_member_name 过滤
	fromA, err := dao.GetMessages(ctx, "team1", "b", false, "a")
	require.NoError(t, err)
	assert.Equal(t, 1, len(fromA))
	assert.Equal(t, "dm1", fromA[0].MessageID)
}
