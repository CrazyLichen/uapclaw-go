package messager

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
)

// TestEventListenerHandle_Handler 测试 Handler 返回底层回调函数
func TestEventListenerHandle_Handler(t *testing.T) {
	var called bool
	handler := MessagerHandler(func(_ context.Context, _ *events.EventMessage) error {
		called = true
		return nil
	})
	h := NewEventListenerHandle(handler)

	got := h.Handler()
	if got == nil {
		t.Fatal("Handler() 不应返回 nil")
	}
	// 调用返回的 handler 验证是同一个
	if err := got(context.Background(), nil); err != nil {
		t.Fatalf("调用 handler 失败: %v", err)
	}
	if !called {
		t.Error("handler 未被调用")
	}
}

// TestEventListenerHandle_ID 测试 ID 返回唯一递增标识
func TestEventListenerHandle_ID(t *testing.T) {
	// 重置序列（测试环境，不影响其他测试）
	h1 := NewEventListenerHandle(nil)
	h2 := NewEventListenerHandle(nil)

	id1 := h1.ID()
	id2 := h2.ID()

	if id1 == 0 {
		t.Error("ID 不应为 0")
	}
	if id2 == 0 {
		t.Error("ID 不应为 0")
	}
	if id1 >= id2 {
		t.Errorf("ID 应递增: id1=%d, id2=%d", id1, id2)
	}
}

// TestNewEventListenerHandle 测试创建带唯一 ID 的监听器句柄
func TestNewEventListenerHandle(t *testing.T) {
	handler := MessagerHandler(func(_ context.Context, _ *events.EventMessage) error {
		return nil
	})
	h := NewEventListenerHandle(handler)

	if h == nil {
		t.Fatal("NewEventListenerHandle 不应返回 nil")
	}
	if h.ID() == 0 {
		t.Error("句柄 ID 不应为 0")
	}
	if h.Handler() == nil {
		t.Error("句柄 Handler 不应为 nil")
	}
}

// TestNewEventListenerHandle_多个句柄ID唯一 测试多个句柄 ID 唯一
func TestNewEventListenerHandle_多个句柄ID唯一(t *testing.T) {
	handler := MessagerHandler(func(_ context.Context, _ *events.EventMessage) error {
		return nil
	})
	ids := make(map[uint64]bool)
	for i := 0; i < 10; i++ {
		h := NewEventListenerHandle(handler)
		if ids[h.ID()] {
			t.Errorf("重复 ID: %d", h.ID())
		}
		ids[h.ID()] = true
	}
	if len(ids) != 10 {
		t.Errorf("应生成 10 个唯一 ID, 实际 %d 个", len(ids))
	}
}
