package message_queue

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestQueueMessage_创建 测试 NewQueueMessage 创建消息。
func TestQueueMessage_创建(t *testing.T) {
	msg := NewQueueMessage(map[string]any{"key": "value"})
	assert.Equal(t, map[string]any{"key": "value"}, msg.Payload)
	assert.Equal(t, "", msg.MessageID)
	assert.Equal(t, 0, msg.ErrorCode)
	assert.Equal(t, "", msg.ErrorMsg)
}

// TestInvokeQueueMessage_创建 测试 NewInvokeQueueMessage 创建同步消息。
func TestInvokeQueueMessage_创建(t *testing.T) {
	msg := NewInvokeQueueMessage(map[string]any{"key": "value"})
	assert.Equal(t, map[string]any{"key": "value"}, msg.Payload)
	assert.Equal(t, "", msg.MessageID)
}

// TestInvokeQueueMessage_等待完成 测试同步消息的 WaitResponse/CompleteResponse 语义。
func TestInvokeQueueMessage_等待完成(t *testing.T) {
	msg := NewInvokeQueueMessage(map[string]any{"key": "value"})

	go func() {
		time.Sleep(50 * time.Millisecond)
		msg.CompleteResponse("result", nil)
	}()

	result, err := msg.WaitResponse(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "result", result)
}

// TestInvokeQueueMessage_等待错误 测试同步消息的 WaitResponse 收到错误。
func TestInvokeQueueMessage_等待错误(t *testing.T) {
	msg := NewInvokeQueueMessage(nil)
	msg.CompleteResponse(nil, assert.AnError)

	result, err := msg.WaitResponse(context.Background())
	assert.ErrorIs(t, err, assert.AnError)
	assert.Nil(t, result)
}

// TestInvokeQueueMessage_上下文取消 测试 WaitResponse 受 context 取消影响。
func TestInvokeQueueMessage_上下文取消(t *testing.T) {
	msg := NewInvokeQueueMessage(nil)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	result, err := msg.WaitResponse(ctx)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Nil(t, result)
}

// TestStreamQueueMessage_创建 测试 NewStreamQueueMessage 创建流式消息。
func TestStreamQueueMessage_创建(t *testing.T) {
	msg := NewStreamQueueMessage(map[string]any{"key": "value"})
	assert.Equal(t, map[string]any{"key": "value"}, msg.Payload)
	assert.Equal(t, "", msg.MessageID)
}

// TestStreamQueueMessage_等待完成 测试流式消息的 WaitResponse/CompleteResponse 语义。
func TestStreamQueueMessage_等待完成(t *testing.T) {
	msg := NewStreamQueueMessage(nil)

	go func() {
		time.Sleep(50 * time.Millisecond)
		msg.CompleteResponse("stream_result", nil)
	}()

	result, err := msg.WaitResponse(context.Background())
	assert.NoError(t, err)
	assert.Equal(t, "stream_result", result)
}

// TestQueueMessage_字段完整性 测试 QueueMessage 所有字段可读写。
func TestQueueMessage_字段完整性(t *testing.T) {
	msg := &QueueMessage{
		MessageID: "msg-123",
		Payload:   map[string]any{"key": "value"},
		ErrorCode: 500,
		ErrorMsg:  "internal error",
	}
	assert.Equal(t, "msg-123", msg.MessageID)
	assert.Equal(t, 500, msg.ErrorCode)
	assert.Equal(t, "internal error", msg.ErrorMsg)
}

// TestQueueMessageBase_接口getter 测试 QueueMessageBase 接口的 getter/setter 方法。
func TestQueueMessageBase_接口getter(t *testing.T) {
	// QueueMessage
	qm := NewQueueMessage(map[string]any{"key": "value"})
	assert.Equal(t, "", qm.GetMessageID())
	qm.SetMessageID("msg-1")
	assert.Equal(t, "msg-1", qm.GetMessageID())
	assert.Equal(t, map[string]any{"key": "value"}, qm.GetPayload())
	assert.Equal(t, 0, qm.GetErrorCode())
	qm.SetErrorCode(500)
	assert.Equal(t, 500, qm.GetErrorCode())
	assert.Equal(t, "", qm.GetErrorMsg())
	qm.SetErrorMsg("error")
	assert.Equal(t, "error", qm.GetErrorMsg())

	// InvokeQueueMessage
	im := NewInvokeQueueMessage(map[string]any{"key": "invoke"})
	im.SetMessageID("msg-2")
	assert.Equal(t, "msg-2", im.GetMessageID())
	assert.Equal(t, map[string]any{"key": "invoke"}, im.GetPayload())

	// StreamQueueMessage
	sm := NewStreamQueueMessage(map[string]any{"key": "stream"})
	sm.SetMessageID("msg-3")
	assert.Equal(t, "msg-3", sm.GetMessageID())
	assert.Equal(t, map[string]any{"key": "stream"}, sm.GetPayload())

	// 通过接口调用
	var base QueueMessageBase = im
	assert.Equal(t, "msg-2", base.GetMessageID())
}
