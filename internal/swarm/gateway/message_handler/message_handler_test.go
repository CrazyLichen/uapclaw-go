package message_handler

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── MessageHandler 测试 ────────────────────────────

// TestNewMessageHandler 创建MessageHandler实例
func TestNewMessageHandler(t *testing.T) {
	mh := NewMessageHandler()
	if mh == nil {
		t.Fatal("NewMessageHandler() 返回 nil，期望非 nil")
	}
}

// TestMessageHandler_HandleInbound 骨架实现返回nil
func TestMessageHandler_HandleInbound(t *testing.T) {
	mh := NewMessageHandler()
	err := mh.HandleInbound(context.Background(), &schema.Message{})
	if err != nil {
		t.Errorf("HandleInbound() 返回错误: %v，期望 nil", err)
	}
}

// TestMessageHandler_StartOutboundLoop 骨架实现返回nil
func TestMessageHandler_StartOutboundLoop(t *testing.T) {
	mh := NewMessageHandler()
	err := mh.StartOutboundLoop(context.Background())
	if err != nil {
		t.Errorf("StartOutboundLoop() 返回错误: %v，期望 nil", err)
	}
}
