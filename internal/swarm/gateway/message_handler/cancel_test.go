package message_handler

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestCancelAgentWorkForSession_完整流程 测试完整取消流程
func TestCancelAgentWorkForSession_完整流程(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	// 注册多个流式任务
	cancelled1 := false
	cancelled2 := false
	mh.registerStreamTask("req-1", "sess-1", map[string]any{"key": "val"}, func() { cancelled1 = true })
	mh.registerStreamTask("req-2", "sess-1", nil, func() { cancelled2 = true })
	mh.registerStreamTask("req-3", "sess-2", nil, func() {})

	// 取消 sess-1 的任务
	mh.CancelAgentWorkForSession(context.Background(), "sess-1", "cancel")

	// 验证 sess-1 的任务被取消
	assert.True(t, cancelled1, "req-1 应被取消")
	assert.True(t, cancelled2, "req-2 应被取消")

	// 验证 sess-2 的任务不受影响
	mh.streamMu.RLock()
	_, exists := mh.streamTasks["req-3"]
	mh.streamMu.RUnlock()
	assert.True(t, exists, "sess-2 的任务不应被取消")
}

// TestSendInterruptResultNotification 测试发送中断结果通知
func TestSendInterruptResultNotification(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	mh.sendInterruptResultNotification("sess-1", "cancel")

	// 从 robotMessages 读取
	select {
	case msg := <-mh.robotMessages:
		assert.Equal(t, schema.MessageTypeEvent, msg.Type)
		assert.Equal(t, schema.EventTypeChatInterruptResult, msg.EventType)
		assert.Equal(t, "sess-1", msg.SessionID)
	default:
		t.Fatal("未收到 interrupt_result 通知")
	}
}

// TestSendStreamCancelledNotification 测试流式取消通知
func TestSendStreamCancelledNotification(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	mh.sendStreamCancelledNotification("sess-1")
}

// TestPublishStreamCancelledFinal 测试发布流式取消最终消息
func TestPublishStreamCancelledFinal(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	mh.publishStreamCancelledFinal("sess-1")
}

// TestCancelStreamTask 测试取消单个流式任务
func TestCancelStreamTask(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	cancelled := false
	mh.registerStreamTask("req-1", "sess-1", nil, func() { cancelled = true })

	mh.cancelStreamTask("req-1")
	assert.True(t, cancelled, "cancel 函数应被调用")

	// 取消不存在的任务不应 panic
	mh.cancelStreamTask("req-unknown")
}
