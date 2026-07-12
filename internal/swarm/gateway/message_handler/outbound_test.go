package message_handler

import (
	"testing"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestHandleAgentServerPush_Nil 测试 nil push 消息
func TestHandleAgentServerPush_Nil(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.handleAgentServerPush(nil)
}

// TestHandleAgentServerPush_空Map 测试空 map push 消息
func TestHandleAgentServerPush_空Map(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	mh.handleAgentServerPush(map[string]any{})
}

// TestHandleCronPushPayload 测试 cron push 处理
func TestHandleCronPushPayload(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.handleCronPushPayload(map[string]any{"action": "execute", "cron": true})
}
