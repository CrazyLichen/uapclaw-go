package handlers

import (
	"context"
	"sync"
	"testing"

	types "github.com/uapclaw/uapclaw-go/internal/agent_teams/agent/coordination/types"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/interaction"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	schema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// capturedInbound 捕获回调参数
type capturedInbound struct {
	memberName string
	sender     string
	body       string
	broadcast  bool
	messageID  string
	timestamp  int64
}

// newMessageHandlerFixture 构造带真实 MessageManager/TeamBackend 的 MessageHandler
//
// 返回 handler + db + backend + 回调捕获 map。
func newMessageHandlerFixture(t *testing.T, humanAgentNames []string) (*MessageHandler, *tools.TeamMessageManager, map[string]*capturedInbound) {
	t.Helper()

	db := database.NewInMemoryTeamDatabase()
	if err := db.Initialize(context.Background()); err != nil {
		t.Fatalf("db init 失败: %v", err)
	}

	// leader 成员 + human-agent 成员
	if ok := db.Member().CreateMember(context.Background(), "leader", "t1", "Leader", "{}",
		"running", string(schema.TeamRoleLeader), "", "idle", "build_mode", "", ""); !ok {
		t.Fatalf("创建 leader 失败")
	}
	for _, name := range humanAgentNames {
		if ok := db.Member().CreateMember(context.Background(), name, "t1", name, "{}",
			"running", string(schema.TeamRoleHumanAgent), "", "idle", "build_mode", "", ""); !ok {
			t.Fatalf("创建 human-agent %s 失败", name)
		}
	}

	backend := tools.NewTeamBackend("t1", "leader", true, db, nil)

	// 重建 HITT 名册（从 DB 读 human_agent 成员）
	backend.RefreshHumanAgentRoster(context.Background())

	// 注册捕获回调
	captured := make(map[string]*capturedInbound)
	var mu sync.Mutex
	for _, name := range humanAgentNames {
		name := name
		callback := func(ctx context.Context, memberName string, payload any) error {
			evt, ok := payload.(*interaction.HumanAgentInboundEvent)
			if !ok {
				t.Errorf("payload 应为 *HumanAgentInboundEvent，实际 %T", payload)
				return nil
			}
			mu.Lock()
			defer mu.Unlock()
			captured[memberName] = &capturedInbound{
				memberName: evt.MemberName,
				sender:     evt.Sender,
				body:       evt.Body,
				broadcast:  evt.Broadcast,
				messageID:  evt.MessageID,
				timestamp:  evt.Timestamp,
			}
			return nil
		}
		if err := backend.RegisterHumanAgentInbound(context.Background(), name, callback); err != nil {
			t.Fatalf("注册回调失败 %s: %v", name, err)
		}
	}

	msgMgr := tools.NewTeamMessageManager(db, "t1", "leader", nil)

	// fakeInfra 不能直接满足接口（Messager 返回类型），改用轻量 struct 满足接口
	infra := &handlerTestInfra{
		msgMgr:  msgMgr,
		backend: backend,
	}

	h := NewMessageHandler(&fakeHost{}, &fakeBP{role: schema.TeamRoleLeader, memberName: "leader"}, infra, &fakePollCtrl{})
	return h, msgMgr, captured
}

// handlerTestInfra 满足 types.DispatcherInfra 的最小实现
type handlerTestInfra struct {
	msgMgr  *tools.TeamMessageManager
	backend *tools.TeamBackend
}

func (i *handlerTestInfra) TaskManager() *tools.TeamTaskManager       { return nil }
func (i *handlerTestInfra) MessageManager() *tools.TeamMessageManager { return i.msgMgr }
func (i *handlerTestInfra) TeamBackend() *tools.TeamBackend           { return i.backend }
func (i *handlerTestInfra) Messager() messager.Messager               { return nil }

// ──────────────────────────── notifyHumanAgentInbound 测试 ────────────────────────────

// TestNotifyHumanAgentInbound_直发消息 测试 M-29 直发路由
//
// teammate → human-agent 直接消息：仅触发接收方回调，载荷字段完整。
func TestNotifyHumanAgentInbound_直发消息(t *testing.T) {
	h, mm, captured := newMessageHandlerFixture(t, []string{"alice"})

	// 预置消息体：teammate bob → human-agent alice
	msgID, err := mm.SendMessage(context.Background(), "hello alice", "alice", "bob")
	if err != nil {
		t.Fatalf("创建消息失败: %v", err)
	}

	event := types.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventMessage,
		Payload: map[string]any{
			"message_id":       msgID,
			"from_member_name": "bob",
			"to_member_name":   "alice",
		},
	}}

	h.notifyHumanAgentInbound(context.Background(), event)

	c, ok := captured["alice"]
	if !ok {
		t.Fatalf("alice 回调应被触发")
	}
	if c.sender != "bob" {
		t.Errorf("sender = %q, want bob", c.sender)
	}
	if c.body != "hello alice" {
		t.Errorf("body = %q, want hello alice", c.body)
	}
	if c.broadcast {
		t.Errorf("直发消息 broadcast 应为 false")
	}
	if c.messageID != msgID {
		t.Errorf("messageID = %q, want %q", c.messageID, msgID)
	}
}

// TestNotifyHumanAgentInbound_广播消息 测试 M-29 广播路由（排除发送者）
func TestNotifyHumanAgentInbound_广播消息(t *testing.T) {
	h, mm, captured := newMessageHandlerFixture(t, []string{"alice", "bob"})

	// bob（human-agent）广播：alice 收到，bob 不回显自己
	msgID, err := mm.BroadcastMessage(context.Background(), "team announcement", "bob")
	if err != nil {
		t.Fatalf("创建广播失败: %v", err)
	}

	event := types.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventBroadcast,
		Payload: map[string]any{
			"message_id":       msgID,
			"from_member_name": "bob",
		},
	}}

	h.notifyHumanAgentInbound(context.Background(), event)

	if _, ok := captured["alice"]; !ok {
		t.Error("alice 应收到广播")
	}
	if _, ok := captured["bob"]; ok {
		t.Error("bob 不应收到自己的广播（排除发送者）")
	}
	if c := captured["alice"]; c != nil && !c.broadcast {
		t.Errorf("broadcast 标记应为 true")
	}
}

// TestNotifyHumanAgentInbound_非human接收方 测试直发目标非 human-agent 时不触发
func TestNotifyHumanAgentInbound_非human接收方(t *testing.T) {
	h, mm, captured := newMessageHandlerFixture(t, []string{"alice"})

	// teammate bob → teammate carol（非 human-agent）
	msgID, err := mm.SendMessage(context.Background(), "internal", "carol", "bob")
	if err != nil {
		t.Fatalf("创建消息失败: %v", err)
	}

	event := types.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventMessage,
		Payload: map[string]any{
			"message_id":       msgID,
			"from_member_name": "bob",
			"to_member_name":   "carol",
		},
	}}

	h.notifyHumanAgentInbound(context.Background(), event)

	if len(captured) != 0 {
		t.Errorf("非 human-agent 接收方不应触发回调，实际 %d 次", len(captured))
	}
}

// TestNotifyHumanAgentInbound_消息不存在 测试 message_id 查不到时静默返回
func TestNotifyHumanAgentInbound_消息不存在(t *testing.T) {
	h, _, captured := newMessageHandlerFixture(t, []string{"alice"})

	event := types.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventMessage,
		Payload: map[string]any{
			"message_id":       "no-such-message",
			"from_member_name": "bob",
			"to_member_name":   "alice",
		},
	}}

	// 不应 panic，不应触发回调
	h.notifyHumanAgentInbound(context.Background(), event)
	if len(captured) != 0 {
		t.Errorf("消息不存在时不应触发回调")
	}
}

// ──────────────────────────── ackUserBoundMessage 测试 ────────────────────────────

// TestAckUserBoundMessage_标记user已读 测试 M-29 leader 代 user 确认
func TestAckUserBoundMessage_标记user已读(t *testing.T) {
	h, mm, _ := newMessageHandlerFixture(t, []string{"alice"})

	// teammate bob → user 伪成员
	msgID, err := mm.SendMessage(context.Background(), "answer for user", "user", "bob")
	if err != nil {
		t.Fatalf("创建消息失败: %v", err)
	}

	event := types.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventMessage,
		Payload: map[string]any{
			"message_id":       msgID,
			"from_member_name": "bob",
			"to_member_name":   "user",
		},
	}}

	h.ackUserBoundMessage(context.Background(), event)

	// 验证 user 视角已读
	unread, err := mm.GetMessages(context.Background(), "user", true, "")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	for _, m := range unread {
		if m.MessageID == msgID {
			t.Errorf("user-bound 消息 %s 应已被 leader 标记已读", msgID)
		}
	}
}

// TestAckUserBoundMessage_非user接收方 测试接收方非 user 时不处理
func TestAckUserBoundMessage_非user接收方(t *testing.T) {
	h, mm, _ := newMessageHandlerFixture(t, []string{"alice"})

	msgID, err := mm.SendMessage(context.Background(), "not for user", "alice", "bob")
	if err != nil {
		t.Fatalf("创建消息失败: %v", err)
	}

	event := types.CoordinationEvent{Transport: &events.EventMessage{
		EventType: events.TeamEventMessage,
		Payload: map[string]any{
			"message_id":       msgID,
			"from_member_name": "bob",
			"to_member_name":   "alice",
		},
	}}

	h.ackUserBoundMessage(context.Background(), event)

	// alice 视角消息应保持未读
	unread, err := mm.GetMessages(context.Background(), "alice", true, "")
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	found := false
	for _, m := range unread {
		if m.MessageID == msgID {
			found = true
		}
	}
	if !found {
		t.Errorf("非 user 接收方的消息不应被 leader 标记已读")
	}
}
