package message_handler

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestHandleServerPush_Nil 测试 nil push 消息
func TestHandleServerPush_Nil(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.HandleServerPush(nil)
}

// TestHandleServerPush_空Map 测试空 map push 消息
func TestHandleServerPush_空Map(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	mh.HandleServerPush(map[string]any{})
}

// TestHandleCronPushPayload 测试 cron push 处理
func TestHandleCronPushPayload(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.handleCronPushPayload(map[string]any{"cron": true})
}

// TestIsCronPayload_边界值 测试 cron payload 边界值
func TestIsCronPayload_边界值(t *testing.T) {
	assert.False(t, isCronPayload(nil))
	assert.False(t, isCronPayload(map[string]any{}))
	assert.False(t, isCronPayload(map[string]any{"metadata": "not-a-map"}))
	assert.False(t, isCronPayload(map[string]any{"body": "not-a-map"}))
}
