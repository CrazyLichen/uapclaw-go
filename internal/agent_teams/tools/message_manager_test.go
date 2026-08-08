package tools

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// newTestMessageManager 创建测试用消息管理器
func newTestMessageManager() *TeamMessageManager {
	db := database.NewInMemoryTeamDatabase()
	_ = db.Initialize(context.Background())
	_ = db.CreateTeam(context.Background(), "team1", "Team1", "leader", "", "")
	_ = db.CreateMember(context.Background(), "alice", "team1", "Alice", "", "active", "teammate", "", "idle", "build_mode", "", "")
	_ = db.CreateMember(context.Background(), "bob", "team1", "Bob", "", "active", "teammate", "", "idle", "build_mode", "", "")
	return NewTeamMessageManager(db, "team1", "alice", nil, "")
}

// TestTeamMessageManager_SendMessage 测试发送直发消息
func TestTeamMessageManager_SendMessage(t *testing.T) {
	mm := newTestMessageManager()
	ctx := context.Background()

	msgID, err := mm.SendMessage(ctx, "hello bob", "bob", "alice")
	if err != nil {
		t.Fatalf("SendMessage 失败: %v", err)
	}
	if msgID == "" {
		t.Fatal("SendMessage 返回空 messageID")
	}

	// 验证消息已创建
	msgs, err := mm.GetMessages(ctx, "bob", false, "")
	if err != nil {
		t.Fatalf("GetMessages 失败: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("GetMessages 返回 %d 条, want 1", len(msgs))
	}
	if msgs[0].Content != "hello bob" {
		t.Errorf("Content = %q, want %q", msgs[0].Content, "hello bob")
	}
	if msgs[0].Broadcast {
		t.Error("Broadcast = true, want false")
	}
}

// TestTeamMessageManager_BroadcastMessage 测试发送广播消息
func TestTeamMessageManager_BroadcastMessage(t *testing.T) {
	mm := newTestMessageManager()
	ctx := context.Background()

	msgID, err := mm.BroadcastMessage(ctx, "announcement", "alice")
	if err != nil {
		t.Fatalf("BroadcastMessage 失败: %v", err)
	}
	if msgID == "" {
		t.Fatal("BroadcastMessage 返回空 messageID")
	}

	// 验证消息已创建
	msgs, err := mm.GetBroadcastMessages(ctx, "bob", false, "")
	if err != nil {
		t.Fatalf("GetBroadcastMessages 失败: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("GetBroadcastMessages 返回 %d 条, want 1", len(msgs))
	}
	if !msgs[0].Broadcast {
		t.Error("Broadcast = false, want true")
	}
}

// TestTeamMessageManager_HasUnreadMessages 测试未读消息检查
func TestTeamMessageManager_HasUnreadMessages(t *testing.T) {
	mm := newTestMessageManager()
	ctx := context.Background()

	// 初始无消息
	if mm.HasUnreadMessages(ctx, true) {
		t.Error("无消息时 HasUnreadMessages 返回 true")
	}

	// 发送直发消息
	_, _ = mm.SendMessage(ctx, "hello", "bob", "alice")
	if !mm.HasUnreadMessages(ctx, true) {
		t.Error("有未读直发消息时 HasUnreadMessages 返回 false")
	}

	// 标记已读
	msgs, _ := mm.GetMessages(ctx, "bob", false, "")
	if len(msgs) > 0 {
		mm.MarkMessageRead(ctx, msgs[0].MessageID, "bob")
		if mm.HasUnreadMessages(ctx, true) {
			t.Error("全部已读后 HasUnreadMessages 返回 true")
		}
	}
}

// TestTeamMessageManager_MarkMessageRead 测试标记已读
func TestTeamMessageManager_MarkMessageRead(t *testing.T) {
	mm := newTestMessageManager()
	ctx := context.Background()

	_, _ = mm.SendMessage(ctx, "hello", "bob", "alice")
	msgs, _ := mm.GetMessages(ctx, "bob", false, "")
	if len(msgs) == 0 {
		t.Fatal("没有消息")
	}

	// 标记已读
	if !mm.MarkMessageRead(ctx, msgs[0].MessageID, "bob") {
		t.Error("MarkMessageRead 返回 false")
	}

	// 验证已读
	unread, _ := mm.GetMessages(ctx, "bob", true, "")
	if len(unread) != 0 {
		t.Errorf("未读消息数 = %d, want 0", len(unread))
	}
}

// TestTeamMessageManager_GetTeamMessages 测试获取团队所有消息
func TestTeamMessageManager_GetTeamMessages(t *testing.T) {
	mm := newTestMessageManager()
	ctx := context.Background()

	_, _ = mm.SendMessage(ctx, "hello", "bob", "alice")
	_, _ = mm.BroadcastMessage(ctx, "announcement", "alice")

	msgs, err := mm.GetTeamMessages(ctx, "team1")
	if err != nil {
		t.Fatalf("GetTeamMessages 失败: %v", err)
	}
	if len(msgs) != 2 {
		t.Errorf("GetTeamMessages 返回 %d 条, want 2", len(msgs))
	}
}
