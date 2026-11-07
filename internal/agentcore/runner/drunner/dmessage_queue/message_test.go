package dmessage_queue

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewDmqRequestMessage_默认值 测试请求消息默认 type 为 INPUT。
// Python: DmqRequestMessage(type=DMessageType.INPUT)
func TestNewDmqRequestMessage_默认值(t *testing.T) {
	msg := NewDmqRequestMessage(map[string]any{"q": "hi"})

	assert.Equal(t, DMessageTypeInput, msg.Type)
	assert.Equal(t, "hi", msg.Payload["q"])
	assert.Equal(t, 0, msg.ErrorCode)
	assert.False(t, msg.EnableStream)
	assert.Nil(t, msg.ExpireAt)
}

// TestNewDmqResponseMessage_默认值 测试响应消息默认 type/result_type。
// Python: DmqResponseMessage(type=DMessageType.OUTPUT, result_type=ResultType.MESSAGE)
func TestNewDmqResponseMessage_默认值(t *testing.T) {
	msg := NewDmqResponseMessage(map[string]any{"a": 1})

	assert.Equal(t, DMessageTypeOutput, msg.Type)
	assert.Equal(t, ResultTypeMessage, msg.ResultType)
	assert.Equal(t, 0, msg.Seq)
	assert.False(t, msg.LastChunk)
}

// TestDmqMessage_实现QueueMessageBase 测试两个消息类型都满足基础接口。
func TestDmqMessage_实现QueueMessageBase(t *testing.T) {
	var req message_queue.QueueMessageBase = NewDmqRequestMessage(nil)
	assert.NotNil(t, req)
	req.SetMessageID("mid-1")
	assert.Equal(t, "mid-1", req.GetMessageID())
	req.SetErrorCode(500)
	assert.Equal(t, 500, req.GetErrorCode())
	req.SetErrorMsg("boom")
	assert.Equal(t, "boom", req.GetErrorMsg())

	var resp message_queue.QueueMessageBase = NewDmqResponseMessage(nil)
	resp.SetMessageID("mid-2")
	assert.Equal(t, "mid-2", resp.GetMessageID())
}

// TestDMessageType_取值 测试枚举取值与 Python 一致。
func TestDMessageType_取值(t *testing.T) {
	assert.Equal(t, "INPUT", string(DMessageTypeInput))
	assert.Equal(t, "STOP", string(DMessageTypeStop))
	assert.Equal(t, "OUTPUT", string(DMessageTypeOutput))
	assert.Equal(t, "MESSAGE", string(ResultTypeMessage))
	assert.Equal(t, "ERROR", string(ResultTypeError))
}
