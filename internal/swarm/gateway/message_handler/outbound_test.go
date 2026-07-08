package message_handler

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestPushLoop_退出 测试 pushLoop 退出
func TestPushLoop_退出(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	go func() {
		mh.pushLoop(ctx)
		close(done)
	}()

	cancel()

	select {
	case <-done:
		// 正常退出
	case <-time.After(time.Second):
		t.Fatal("pushLoop 未在超时内退出")
	}
}

// TestHandleAgentServerPush_Nil 测试 nil push 消息
func TestHandleAgentServerPush_Nil(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	mh.handleAgentServerPush(context.Background(), nil)
}

// TestHandleAgentServerPush_空Map 测试空 map push 消息
func TestHandleAgentServerPush_空Map(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	mh.handleAgentServerPush(context.Background(), map[string]any{})
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
