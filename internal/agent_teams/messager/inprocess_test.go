package messager

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// newMsg 创建测试用 EventMessage 指针。
func newMsg() *schema.EventMessage {
	return schema.NewEventMessage(schema.TeamEventTaskCreated, nil, "")
}

// TestInProcessMessager_Publish_Subscribe 测试发布订阅基本功能
func TestInProcessMessager_Publish_Subscribe(t *testing.T) {
	CleanupInProcessBus()
	cfg := schema.NewMessagerTransportConfig()
	cfg.NodeID = "agent-1"
	m := NewInProcessMessager(cfg)

	var received atomic.Int32
	handler := func(ctx context.Context, msg *schema.EventMessage) error {
		received.Add(1)
		return nil
	}

	ctx := context.Background()
	if err := m.Subscribe(ctx, "topic1", handler); err != nil {
		t.Fatalf("Subscribe 失败: %v", err)
	}

	msg := schema.NewEventMessage(schema.TeamEventTaskCreated, map[string]any{"team_name": "t1"}, "")
	if err := m.Publish(ctx, "topic1", msg); err != nil {
		t.Fatalf("Publish 失败: %v", err)
	}

	if received.Load() != 1 {
		t.Errorf("received = %d, want 1", received.Load())
	}
}

// TestInProcessMessager_Publish_SenderID 测试 Publish 自动设置 SenderID
func TestInProcessMessager_Publish_SenderID(t *testing.T) {
	CleanupInProcessBus()
	cfg := schema.NewMessagerTransportConfig()
	cfg.NodeID = "agent-sender"
	sender := NewInProcessMessager(cfg)

	cfg2 := schema.NewMessagerTransportConfig()
	cfg2.NodeID = "agent-receiver"
	receiver := NewInProcessMessager(cfg2)

	var gotSenderID string
	handler := func(ctx context.Context, msg *schema.EventMessage) error {
		gotSenderID = msg.SenderID
		return nil
	}

	ctx := context.Background()
	_ = receiver.Subscribe(ctx, "topic1", handler)

	msg := schema.NewEventMessage(schema.TeamEventTaskCreated, map[string]any{}, "")
	_ = sender.Publish(ctx, "topic1", msg)

	if gotSenderID != "agent-sender" {
		t.Errorf("SenderID = %q, want %q", gotSenderID, "agent-sender")
	}
}

// TestInProcessMessager_Unsubscribe 测试取消订阅
func TestInProcessMessager_Unsubscribe(t *testing.T) {
	CleanupInProcessBus()
	cfg := schema.NewMessagerTransportConfig()
	cfg.NodeID = "agent-1"
	m := NewInProcessMessager(cfg)

	var received atomic.Int32
	handler := func(ctx context.Context, msg *schema.EventMessage) error {
		received.Add(1)
		return nil
	}

	ctx := context.Background()
	_ = m.Subscribe(ctx, "topic1", handler)
	_ = m.Publish(ctx, "topic1", newMsg())
	if received.Load() != 1 {
		t.Errorf("subscribe 后 received = %d, want 1", received.Load())
	}

	_ = m.Unsubscribe(ctx, "topic1")
	_ = m.Publish(ctx, "topic1", newMsg())
	if received.Load() != 1 {
		t.Errorf("unsubscribe 后 received = %d, want 1", received.Load())
	}
}

// TestInProcessMessager_Send 测试点对点发送
func TestInProcessMessager_Send(t *testing.T) {
	CleanupInProcessBus()
	cfg := schema.NewMessagerTransportConfig()
	cfg.NodeID = "agent-1"
	sender := NewInProcessMessager(cfg)

	cfg2 := schema.NewMessagerTransportConfig()
	cfg2.NodeID = "agent-2"
	receiver := NewInProcessMessager(cfg2)

	var received atomic.Int32
	handler := func(ctx context.Context, msg *schema.EventMessage) error {
		received.Add(1)
		return nil
	}

	ctx := context.Background()
	_ = receiver.RegisterDirectMessageHandler(ctx, handler)
	_ = sender.Send(ctx, "agent-2", newMsg())

	if received.Load() != 1 {
		t.Errorf("received = %d, want 1", received.Load())
	}
}

// TestInProcessMessager_UnregisterDirectMessageHandler 测试取消注册 P2P handler
func TestInProcessMessager_UnregisterDirectMessageHandler(t *testing.T) {
	CleanupInProcessBus()
	cfg := schema.NewMessagerTransportConfig()
	cfg.NodeID = "agent-1"
	m := NewInProcessMessager(cfg)

	var received atomic.Int32
	handler := func(ctx context.Context, msg *schema.EventMessage) error {
		received.Add(1)
		return nil
	}

	ctx := context.Background()
	_ = m.RegisterDirectMessageHandler(ctx, handler)
	_ = m.Send(ctx, "agent-1", newMsg())
	if received.Load() != 1 {
		t.Errorf("register 后 received = %d, want 1", received.Load())
	}

	_ = m.UnregisterDirectMessageHandler(ctx)
	_ = m.Send(ctx, "agent-1", newMsg())
	if received.Load() != 1 {
		t.Errorf("unregister 后 received = %d, want 1", received.Load())
	}
}

// TestCleanupInProcessBus 测试 Bus 清理
func TestCleanupInProcessBus(t *testing.T) {
	CleanupInProcessBus()
	cfg := schema.NewMessagerTransportConfig()
	cfg.NodeID = "agent-1"
	m := NewInProcessMessager(cfg)

	var received atomic.Int32
	handler := func(ctx context.Context, msg *schema.EventMessage) error {
		received.Add(1)
		return nil
	}

	ctx := context.Background()
	_ = m.Subscribe(ctx, "topic1", handler)
	_ = m.Publish(ctx, "topic1", newMsg())
	if received.Load() != 1 {
		t.Errorf("cleanup 前 received = %d, want 1", received.Load())
	}

	CleanupInProcessBus()
	_ = m.Publish(ctx, "topic1", newMsg())
	if received.Load() != 1 {
		t.Errorf("cleanup 后 received = %d, want 1 (Bus 已重置)", received.Load())
	}
}

// TestInProcessMessager_StartStop 测试 Start/Stop
func TestInProcessMessager_StartStop(t *testing.T) {
	CleanupInProcessBus()
	cfg := schema.NewMessagerTransportConfig()
	m := NewInProcessMessager(cfg)
	ctx := context.Background()

	if err := m.Start(ctx); err != nil {
		t.Fatalf("Start 失败: %v", err)
	}
	if err := m.Stop(ctx); err != nil {
		t.Fatalf("Stop 失败: %v", err)
	}
}

// TestInProcessMessager_Publish_SenderIDStamper 测试 SenderID 自动设置
func TestInProcessMessager_Publish_SenderIDStamper(t *testing.T) {
	CleanupInProcessBus()
	cfg := schema.NewMessagerTransportConfig()
	cfg.NodeID = "agent-sender"
	m := NewInProcessMessager(cfg)

	ctx := context.Background()
	msg := schema.NewEventMessage(schema.TeamEventTaskCreated, nil, "")
	// 对齐 Python: model_copy 创建副本，原始消息不受影响
	_ = m.Publish(ctx, "topic1", msg)
	if msg.SenderID != "" {
		// G19 修复后，原始消息的 SenderID 不再被原地修改
		// Python 使用 model_copy，Go 也创建副本
		t.Errorf("SenderID = %q, want %q (original message should not be modified)", msg.SenderID, "")
	}
}

// TestInProcessMessager_Send_NoHandler 测试向不存在的 agent 发送
func TestInProcessMessager_Send_NoHandler(t *testing.T) {
	CleanupInProcessBus()
	cfg := schema.NewMessagerTransportConfig()
	cfg.NodeID = "agent-1"
	m := NewInProcessMessager(cfg)

	ctx := context.Background()
	// 不应 panic，仅 warn
	err := m.Send(ctx, "nonexistent", newMsg())
	if err != nil {
		t.Errorf("Send 不存在的 agent 应返回 nil, 实际: %v", err)
	}
}
