package team_runtime

import "testing"

// TestNewMessageEnvelope 测试消息信封构造
func TestNewMessageEnvelope(t *testing.T) {
	t.Run("基本构造", func(t *testing.T) {
		e := NewMessageEnvelope("msg-1", "hello", "agent-a")
		if e.MessageID != "msg-1" {
			t.Errorf("MessageID = %q, want %q", e.MessageID, "msg-1")
		}
		if e.Message != "hello" {
			t.Errorf("Message = %v, want %v", e.Message, "hello")
		}
		if e.Sender != "agent-a" {
			t.Errorf("Sender = %q, want %q", e.Sender, "agent-a")
		}
		if e.Metadata == nil {
			t.Error("Metadata 不应为 nil")
		}
	})

	t.Run("P2P 选项", func(t *testing.T) {
		e := NewMessageEnvelope("msg-2", "data", "sender", WithRecipient("receiver"))
		if e.Recipient != "receiver" {
			t.Errorf("Recipient = %q, want %q", e.Recipient, "receiver")
		}
	})

	t.Run("Pub-Sub 选项", func(t *testing.T) {
		e := NewMessageEnvelope("msg-3", "event", "sender", WithTopicID("topic-1"))
		if e.TopicID != "topic-1" {
			t.Errorf("TopicID = %q, want %q", e.TopicID, "topic-1")
		}
	})

	t.Run("SessionID 选项", func(t *testing.T) {
		e := NewMessageEnvelope("msg-4", "payload", "sender", WithSessionID("sess-1"))
		if e.SessionID != "sess-1" {
			t.Errorf("SessionID = %q, want %q", e.SessionID, "sess-1")
		}
	})

	t.Run("Metadata 选项", func(t *testing.T) {
		meta := map[string]any{"key": "value"}
		e := NewMessageEnvelope("msg-5", "payload", "sender", WithMetadata(meta))
		if e.Metadata["key"] != "value" {
			t.Errorf("Metadata[key] = %v, want %v", e.Metadata["key"], "value")
		}
	})

	t.Run("多选项组合", func(t *testing.T) {
		e := NewMessageEnvelope("msg-6", "payload", "a",
			WithRecipient("b"),
			WithSessionID("sess"),
			WithMetadata(map[string]any{"k": 1}),
		)
		if e.Recipient != "b" || e.SessionID != "sess" || e.Metadata["k"] != 1 {
			t.Errorf("多选项组合失败: %+v", e)
		}
	})
}

// TestMessageEnvelope_IsP2P 测试 P2P 模式判断
func TestMessageEnvelope_IsP2P(t *testing.T) {
	t.Run("Recipient 非空时返回 true", func(t *testing.T) {
		e := &MessageEnvelope{MessageID: "1", Recipient: "agent-b"}
		if !e.IsP2P() {
			t.Error("IsP2P() = false, want true")
		}
	})

	t.Run("Recipient 为空时返回 false", func(t *testing.T) {
		e := &MessageEnvelope{MessageID: "1"}
		if e.IsP2P() {
			t.Error("IsP2P() = true, want false")
		}
	})

	t.Run("仅有 TopicID 时返回 false", func(t *testing.T) {
		e := &MessageEnvelope{MessageID: "1", TopicID: "topic-1"}
		if e.IsP2P() {
			t.Error("IsP2P() = true, want false")
		}
	})
}

// TestMessageEnvelope_IsPubSub 测试 Pub-Sub 模式判断
func TestMessageEnvelope_IsPubSub(t *testing.T) {
	t.Run("TopicID 非空时返回 true", func(t *testing.T) {
		e := &MessageEnvelope{MessageID: "1", TopicID: "topic-1"}
		if !e.IsPubSub() {
			t.Error("IsPubSub() = false, want true")
		}
	})

	t.Run("TopicID 为空时返回 false", func(t *testing.T) {
		e := &MessageEnvelope{MessageID: "1"}
		if e.IsPubSub() {
			t.Error("IsPubSub() = true, want false")
		}
	})

	t.Run("仅有 Recipient 时返回 false", func(t *testing.T) {
		e := &MessageEnvelope{MessageID: "1", Recipient: "agent-b"}
		if e.IsPubSub() {
			t.Error("IsPubSub() = true, want false")
		}
	})
}

// TestMessageEnvelope_String 测试精简表示
func TestMessageEnvelope_String(t *testing.T) {
	t.Run("P2P 模式", func(t *testing.T) {
		e := &MessageEnvelope{MessageID: "msg-1", Sender: "a", Recipient: "b"}
		want := "MessageEnvelope{id=msg-1, sender=a, recipient=b}"
		if got := e.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	})

	t.Run("Pub-Sub 模式", func(t *testing.T) {
		e := &MessageEnvelope{MessageID: "msg-2", Sender: "a", TopicID: "t"}
		want := "MessageEnvelope{id=msg-2, sender=a, topic=t}"
		if got := e.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	})

	t.Run("无模式（既非 P2P 也非 Pub-Sub）", func(t *testing.T) {
		e := &MessageEnvelope{MessageID: "msg-3", Sender: "a"}
		want := "MessageEnvelope{id=msg-3, sender=a}"
		if got := e.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	})

	t.Run("P2P 优先于 Pub-Sub", func(t *testing.T) {
		// 当 Recipient 和 TopicID 同时存在时，P2P 优先
		e := &MessageEnvelope{MessageID: "msg-4", Sender: "a", Recipient: "b", TopicID: "t"}
		want := "MessageEnvelope{id=msg-4, sender=a, recipient=b}"
		if got := e.String(); got != want {
			t.Errorf("String() = %q, want %q", got, want)
		}
	})
}
