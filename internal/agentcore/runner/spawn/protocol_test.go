package spawn

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

// TestMessageType_String 测试 MessageType.String()
func TestMessageType_String(t *testing.T) {
	tests := []struct {
		t    MessageType
		want string
	}{
		{MessageTypeInput, "INPUT"},
		{MessageTypeOutput, "OUTPUT"},
		{MessageTypeHealthCheck, "HEALTH_CHECK"},
		{MessageTypeHealthCheckResponse, "HEALTH_CHECK_RESPONSE"},
		{MessageTypeShutdown, "SHUTDOWN"},
		{MessageTypeShutdownAck, "SHUTDOWN_ACK"},
		{MessageTypeError, "ERROR"},
		{MessageTypeStreamChunk, "STREAM_CHUNK"},
		{MessageTypeDone, "DONE"},
	}
	for _, tt := range tests {
		if got := tt.t.String(); got != tt.want {
			t.Errorf("MessageType(%d).String() = %q, want %q", tt.t, got, tt.want)
		}
	}
}

// TestMessageType_MarshalJSON 测试 MessageType JSON 序列化
func TestMessageType_MarshalJSON(t *testing.T) {
	data, err := json.Marshal(MessageTypeInput)
	if err != nil {
		t.Fatalf("MarshalJSON 失败: %v", err)
	}
	if string(data) != `"INPUT"` {
		t.Errorf("MarshalJSON = %s, want \"INPUT\"", data)
	}
}

// TestMessageType_UnmarshalJSON 测试 MessageType JSON 反序列化
func TestMessageType_UnmarshalJSON(t *testing.T) {
	var mt MessageType
	if err := json.Unmarshal([]byte(`"SHUTDOWN"`), &mt); err != nil {
		t.Fatalf("UnmarshalJSON 失败: %v", err)
	}
	if mt != MessageTypeShutdown {
		t.Errorf("UnmarshalJSON = %d, want %d", mt, MessageTypeShutdown)
	}
}

// TestMessageType_UnmarshalJSON_未知类型 测试未知消息类型反序列化返回错误
func TestMessageType_UnmarshalJSON_未知类型(t *testing.T) {
	var mt MessageType
	err := json.Unmarshal([]byte(`"UNKNOWN_TYPE"`), &mt)
	if err == nil {
		t.Error("未知类型应返回错误")
	}
}

// TestSerializeDeserializeMessage_往返 测试消息序列化/反序列化往返
func TestSerializeDeserializeMessage_往返(t *testing.T) {
	original := Message{
		Type:      MessageTypeInput,
		Payload:   map[string]any{"key": "value"},
		Timestamp: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		MessageID: "test-msg-1",
	}

	data, err := SerializeMessage(original)
	if err != nil {
		t.Fatalf("SerializeMessage 失败: %v", err)
	}

	got, err := DeserializeMessage(data)
	if err != nil {
		t.Fatalf("DeserializeMessage 失败: %v", err)
	}

	if got.Type != original.Type {
		t.Errorf("Type = %d, want %d", got.Type, original.Type)
	}
	if got.MessageID != original.MessageID {
		t.Errorf("MessageID = %s, want %s", got.MessageID, original.MessageID)
	}
}

// TestWriteReadMessage_往返 测试消息写入/读取往返
func TestWriteReadMessage_往返(t *testing.T) {
	var buf bytes.Buffer

	msg := NewMessage(MessageTypeHealthCheck, map[string]any{})
	if err := WriteMessage(&buf, msg); err != nil {
		t.Fatalf("WriteMessage 失败: %v", err)
	}

	got, err := ReadMessage(&buf)
	if err != nil {
		t.Fatalf("ReadMessage 失败: %v", err)
	}
	if got.Type != MessageTypeHealthCheck {
		t.Errorf("Type = %d, want %d", got.Type, MessageTypeHealthCheck)
	}
}

// TestReadMessage_跳过非JSON行 测试 ReadMessage 跳过非 JSON 行
func TestReadMessage_跳过非JSON行(t *testing.T) {
	msg := NewMessage(MessageTypeDone, map[string]any{"result": "ok"})
	input := strings.Builder{}
	input.WriteString("this is not json\n")
	input.WriteString("another bad line\n")

	var msgBuf bytes.Buffer
	if err := WriteMessage(&msgBuf, msg); err != nil {
		t.Fatalf("WriteMessage 失败: %v", err)
	}
	input.Write(msgBuf.Bytes())

	combined := input.String()
	got, err := ReadMessage(strings.NewReader(combined))
	if err != nil {
		t.Fatalf("ReadMessage 失败: %v", err)
	}
	if got.Type != MessageTypeDone {
		t.Errorf("Type = %d, want %d", got.Type, MessageTypeDone)
	}
}

// TestReadMessage_EOF 测试 ReadMessage 在 EOF 时返回错误
func TestReadMessage_EOF(t *testing.T) {
	_, err := ReadMessage(strings.NewReader(""))
	if err != io.EOF {
		t.Errorf("ReadMessage(空) 错误 = %v, want io.EOF", err)
	}
}

// TestNewMessage 测试 NewMessage 自动设置字段
func TestNewMessage(t *testing.T) {
	msg := NewMessage(MessageTypeInput, map[string]any{"key": "val"})
	if msg.Type != MessageTypeInput {
		t.Errorf("Type = %d, want %d", msg.Type, MessageTypeInput)
	}
	if msg.Timestamp.IsZero() {
		t.Error("Timestamp 不应为零值")
	}
	if msg.MessageID == "" {
		t.Error("MessageID 不应为空")
	}
}
