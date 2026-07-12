package message_handler

import (
	"context"
	"encoding/json"
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
	msg := schema.NewReqMessage("feishu_test", "sess-1", schema.ReqMethodChatCancel, json.RawMessage(`{}`))
	mh.CancelAgentWorkForSession(context.Background(), msg, "sess-1", true)

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

	hasActive := true
	mh.sendInterruptResultNotification("req-1", "feishu_test", "sess-1", "cancel", "任务已取消", true, &hasActive)

	// 从 robotMessages 读取
	select {
	case msg := <-mh.robotMessages:
		assert.Equal(t, schema.MessageTypeEvent, msg.Type)
		assert.Equal(t, schema.EventTypeChatInterruptResult, msg.EventType)
		assert.Equal(t, "sess-1", msg.SessionID)
		assert.Equal(t, "req-1", msg.ID)
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

	mh.sendStreamCancelledNotification("req-1", "feishu_test", "sess-1")
}

// TestPublishStreamCancelledFinal 测试发布流式取消最终消息
func TestPublishStreamCancelledFinal(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	mh.publishStreamCancelledFinal("req-1", "feishu_test", "sess-1", map[string]any{"key": "val"})
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

// TestBuildErrorOutMessage 测试构造错误响应消息
func TestBuildErrorOutMessage(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	_ = mh // 避免 unused 警告

	origMsg := &schema.Message{
		ID:        "req-1",
		Type:      schema.MessageTypeReq,
		ChannelID: "feishu_test",
		SessionID: "sess-1",
		Metadata:  map[string]any{"key": "val"},
	}

	errMsg := mh.buildErrorOutMessage(origMsg, assert.AnError)
	assert.Equal(t, "req-1", errMsg.ID)
	assert.Equal(t, schema.MessageTypeRes, errMsg.Type)
	assert.Equal(t, "feishu_test", errMsg.ChannelID)
	assert.Equal(t, "sess-1", errMsg.SessionID)
	assert.False(t, errMsg.OK)
	assert.Contains(t, errMsg.Payload["error"].(string), "assert.AnError")
}

// TestBuildToolResultMessage 测试构造工具结果消息
func TestBuildToolResultMessage(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()
	_ = mh

	toolInfo := map[string]any{
		"tool_name":    "read_file",
		"tool_call_id": "call-1",
		"result":       "file content",
		"status":       "cancelled",
	}

	msg := mh.buildToolResultMessage("feishu_test", "sess-1", toolInfo, map[string]any{"key": "val"})
	assert.Equal(t, schema.MessageTypeEvent, msg.Type)
	assert.Equal(t, schema.EventTypeChatToolResult, msg.EventType)
	assert.Equal(t, "feishu_test", msg.ChannelID)
	assert.Equal(t, "sess-1", msg.SessionID)
	assert.Contains(t, msg.ID, "tool_result_")

	toolResult, ok := msg.Payload["tool_result"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "read_file", toolResult["tool_name"])
	assert.Equal(t, "call-1", toolResult["tool_call_id"])
	assert.Equal(t, "cancelled", toolResult["status"])
}

// TestSendCancelledToolResults 测试发送已取消工具结果
func TestSendCancelledToolResults(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	payload := map[string]any{
		"cancelled_tools": []any{
			map[string]any{"tool_name": "read_file", "tool_call_id": "call-1", "result": "", "status": "cancelled"},
			map[string]any{"tool_name": "write_file", "tool_call_id": "call-2", "result": "", "status": "cancelled"},
		},
	}
	mh.sendCancelledToolResults("feishu_test", "sess-1", payload, nil)
}

// TestSendProcessingStatus_新签名 测试新签名的 processing_status
func TestSendProcessingStatus_新签名(t *testing.T) {
	mh := createTestMessageHandlerWithTransport()

	go func() {
		for range mh.robotMessages {
		}
	}()

	mh.sendProcessingStatus("req-1", "sess-1", "feishu_test", true)
	mh.sendProcessingStatus("req-1", "sess-1", "feishu_test", false)
}
