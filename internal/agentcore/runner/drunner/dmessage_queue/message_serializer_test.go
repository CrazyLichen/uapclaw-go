package dmessage_queue

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/message_queue"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestMessageToMap_请求注入class 测试写侧注入 __class__ 与全字段。
//
// Python: _serialize_payload 对 BaseModel 产出 {"__class__": 类名, ...字段}
func TestMessageToMap_请求注入class(t *testing.T) {
	msg := NewDmqRequestMessage(map[string]any{"q": "hi"})
	msg.MessageID = "mid-1"
	msg.ReplyTopic = "reply.topic"
	msg.RequestID = "rid-1"
	msg.EnableStream = true

	got, err := MessageToMap(msg)
	require.NoError(t, err)

	assert.Equal(t, "DmqRequestMessage", got["__class__"])
	assert.Equal(t, "mid-1", got["message_id"])
	assert.Equal(t, "INPUT", got["type"])
	assert.Equal(t, "reply.topic", got["reply_topic"])
	assert.Equal(t, "rid-1", got["request_id"])
	assert.Equal(t, true, got["enable_stream"])
	assert.Equal(t, map[string]any{"q": "hi"}, got["payload"])
}

// TestMessageToMap_响应全字段 测试响应消息字段完整。
func TestMessageToMap_响应全字段(t *testing.T) {
	msg := NewDmqResponseMessage(map[string]any{"a": 1.0})
	msg.Seq = 3
	msg.LastChunk = true
	msg.ResultType = ResultTypeError

	got, err := MessageToMap(msg)
	require.NoError(t, err)

	assert.Equal(t, "DmqResponseMessage", got["__class__"])
	assert.Equal(t, "OUTPUT", got["type"])
	assert.Equal(t, "ERROR", got["result_type"])
	assert.Equal(t, 3, got["seq"])
	assert.Equal(t, true, got["last_chunk"])
}

// TestSerializePayload_datetime 测试 time.Time 编成 __type__ 包装。
//
// Python: _json_serializer 对 datetime 返回 {"__type__": "datetime", "value": isoformat()}
func TestSerializePayload_datetime(t *testing.T) {
	ts := time.Date(2026, 11, 3, 1, 2, 3, 0, time.UTC)

	got, err := serializePayload(ts, 0)
	require.NoError(t, err)

	m, ok := got.(map[string]any)
	require.True(t, ok, "期望 map，实际 %T", got)
	assert.Equal(t, "datetime", m["__type__"])
	assert.Equal(t, "2026-11-03T01:02:03Z", m["value"])
}

// TestSerializePayload_嵌套递归 测试 map/list 递归转换。
func TestSerializePayload_嵌套递归(t *testing.T) {
	in := map[string]any{
		"outer": []any{
			map[string]any{"inner": "v"},
		},
	}

	got, err := serializePayload(in, 0)
	require.NoError(t, err)

	m, ok := got.(map[string]any)
	require.True(t, ok)
	list, ok := m["outer"].([]any)
	require.True(t, ok)
	inner, ok := list[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "v", inner["inner"])
}

// TestSerializePayload_深度超限报错 测试超过 10 层报错。
//
// Python: raise RecursionError(f"Payload nested too deep (> {MAX_RECURSE_DEPTH})")
func TestSerializePayload_深度超限报错(t *testing.T) {
	_, err := serializePayload("v", MaxRecurseDepth+1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "nested too deep")
}

// TestMessageToMap_非Dmq类型报错 测试偏差 D6：非 Dmq 类型 fail fast。
//
// Python 是发出去、对面 Unknown payload class 才炸；Go 在发送前就报错。
func TestMessageToMap_非Dmq类型报错(t *testing.T) {
	_, err := MessageToMap(newPlainQueueMessage())
	require.Error(t, err)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newPlainQueueMessage 构造一个非 Dmq 的普通 QueueMessage，用于 D6 用例。
func newPlainQueueMessage() *message_queue.QueueMessage {
	return message_queue.NewQueueMessage(nil)
}

// TestSerializeMessage_产出合法JSON 测试序列化结果是带 __class__ 的 JSON 字节。
func TestSerializeMessage_产出合法JSON(t *testing.T) {
	msg := NewDmqRequestMessage(map[string]any{"q": "hi"})
	msg.MessageID = "mid-1"

	data, err := SerializeMessage(msg)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(data, &decoded), "产出不是合法 JSON: %s", string(data))
	assert.Equal(t, "DmqRequestMessage", decoded["__class__"])
	assert.Equal(t, "mid-1", decoded["message_id"])
}

// TestSerializeMessage_非Dmq类型报错 测试序列化阶段的 fail fast。
func TestSerializeMessage_非Dmq类型报错(t *testing.T) {
	_, err := SerializeMessage(newPlainQueueMessage())
	require.Error(t, err)
}

// TestDeserializeMessage_请求往返 测试序列化→反序列化字段全等。
func TestDeserializeMessage_请求往返(t *testing.T) {
	orig := NewDmqRequestMessage(map[string]any{"q": "hi"})
	orig.MessageID = "mid-1"
	orig.ReplyTopic = "reply.t"
	orig.RequestID = "rid-1"
	orig.SenderID = "s1"
	orig.ReceiverID = "r1"
	orig.EnableStream = true

	data, err := SerializeMessage(orig)
	require.NoError(t, err)

	got, err := DeserializeMessage(data)
	require.NoError(t, err)

	req, ok := got.(*DmqRequestMessage)
	require.True(t, ok, "期望 *DmqRequestMessage，实际 %T", got)
	assert.Equal(t, "mid-1", req.MessageID)
	assert.Equal(t, "reply.t", req.ReplyTopic)
	assert.Equal(t, "rid-1", req.RequestID)
	assert.Equal(t, "s1", req.SenderID)
	assert.Equal(t, "r1", req.ReceiverID)
	assert.True(t, req.EnableStream)
	assert.Equal(t, "hi", req.Payload["q"])
}

// TestDeserializeMessage_响应往返 测试响应类型与流式字段。
func TestDeserializeMessage_响应往返(t *testing.T) {
	orig := NewDmqResponseMessage(map[string]any{"a": "b"})
	orig.Seq = 7
	orig.LastChunk = true

	data, err := SerializeMessage(orig)
	require.NoError(t, err)

	got, err := DeserializeMessage(data)
	require.NoError(t, err)

	resp, ok := got.(*DmqResponseMessage)
	require.True(t, ok, "期望 *DmqResponseMessage，实际 %T", got)
	assert.Equal(t, 7, resp.Seq)
	assert.True(t, resp.LastChunk)
	assert.Equal(t, "b", resp.Payload["a"])
}

// TestDeserializeMessage_补默认值 测试缺 type/result_type 时补默认值。
//
// Python: pydantic 字段默认值 DMessageType.OUTPUT / ResultType.MESSAGE
func TestDeserializeMessage_补默认值(t *testing.T) {
	raw := []byte(`{"__class__":"DmqResponseMessage","message_id":"m1","payload":{}}`)

	got, err := DeserializeMessage(raw)
	require.NoError(t, err)

	resp, ok := got.(*DmqResponseMessage)
	require.True(t, ok)
	assert.Equal(t, DMessageTypeOutput, resp.Type)
	assert.Equal(t, ResultTypeMessage, resp.ResultType)
}

// TestDeserializeMessage_补请求默认值 测试缺 type 时补 INPUT。
func TestDeserializeMessage_补请求默认值(t *testing.T) {
	raw := []byte(`{"__class__":"DmqRequestMessage","message_id":"m1","payload":{}}`)

	got, err := DeserializeMessage(raw)
	require.NoError(t, err)

	req, ok := got.(*DmqRequestMessage)
	require.True(t, ok)
	assert.Equal(t, DMessageTypeInput, req.Type)
}

// TestDeserializeMessage_未知类名报错 测试对齐 Python ValueError。
//
// Python: raise ValueError(f"Unknown payload class: {class_name}")
func TestDeserializeMessage_未知类名报错(t *testing.T) {
	_, err := DeserializeMessage([]byte(`{"__class__":"UnknownThing"}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unknown payload class: UnknownThing")
}

// TestDeserializeMessage_嵌套payload保留map 测试偏差 D4：内层不重建类型。
func TestDeserializeMessage_嵌套payload保留map(t *testing.T) {
	raw := []byte(`{"__class__":"DmqResponseMessage","payload":{"__class__":"OutputSchema","content":"x"}}`)

	got, err := DeserializeMessage(raw)
	require.NoError(t, err)

	resp, ok := got.(*DmqResponseMessage)
	require.True(t, ok)
	assert.Equal(t, "OutputSchema", resp.Payload["__class__"])
}

// TestDeserializeMessage_坏JSON报错 测试非法字节返回错误。
func TestDeserializeMessage_坏JSON报错(t *testing.T) {
	_, err := DeserializeMessage([]byte("not-json"))
	require.Error(t, err)
}
