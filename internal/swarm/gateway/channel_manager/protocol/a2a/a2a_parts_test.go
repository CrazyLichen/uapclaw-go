package a2a

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestIsCompletionSentinelText 对齐 Python is_completion_sentinel_text (a2a_connect.py L425-428)
func TestIsCompletionSentinelText(t *testing.T) {
	cases := map[string]bool{
		"{'is_complete': True}":   true,
		`{"is_complete": true}`:   true,
		"  {'is_complete':true} ": true,
		`{"is_complete": false}`:  false,
		"is_complete":             false,
		"":                        false,
	}
	for in, want := range cases {
		if got := IsCompletionSentinelText(in); got != want {
			t.Errorf("IsCompletionSentinelText(%q) = %v, 期望 %v", in, got, want)
		}
	}
}

// TestIsTerminalMessage 对齐 Python is_terminal_message (L430-444)
func TestIsTerminalMessage(t *testing.T) {
	res := &schema.Message{Type: schema.MessageTypeRes}
	errEvt := &schema.Message{Type: schema.MessageTypeEvent, EventType: schema.EventTypeChatError}
	interrupt := &schema.Message{Type: schema.MessageTypeEvent, EventType: schema.EventTypeChatInterruptResult}
	complete := &schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatFinal,
		Payload:   map[string]any{"is_complete": true},
	}
	delta := &schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatDelta,
		Payload:   map[string]any{"content": "x"},
	}
	notBool := &schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatFinal,
		Payload:   map[string]any{"is_complete": "true"},
	}

	cases := map[string]struct {
		msg  *schema.Message
		want bool
	}{
		"res":                {res, true},
		"chat.error":         {errEvt, true},
		"interrupt":          {interrupt, true},
		"is_complete":        {complete, true},
		"delta":              {delta, false},
		"is_complete 非 bool": {notBool, false},
		"nil":                {nil, false},
	}
	for name, tc := range cases {
		if got := IsTerminalMessage(tc.msg); got != tc.want {
			t.Errorf("%s: IsTerminalMessage = %v, 期望 %v", name, got, tc.want)
		}
	}
}

// TestMessageToText 对齐 Python message_to_text (L348-357)
func TestMessageToText(t *testing.T) {
	// ① CHAT_ERROR：error 优先
	got := MessageToText(&schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatError,
		Payload:   map[string]any{"error": "boom", "content": "ignored"},
	})
	if got != "boom" {
		t.Errorf("CHAT_ERROR error 分支 = %q, 期望 boom", got)
	}

	// ② CHAT_ERROR：error 空则 content
	got = MessageToText(&schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatError,
		Payload:   map[string]any{"error": "", "content": "from-content"},
	})
	if got != "from-content" {
		t.Errorf("CHAT_ERROR content 分支 = %q, 期望 from-content", got)
	}

	// ③ CHAT_ERROR：都空则兜底文案
	got = MessageToText(&schema.Message{
		Type:      schema.MessageTypeEvent,
		EventType: schema.EventTypeChatError,
		Payload:   map[string]any{},
	})
	if got != "agent request failed" {
		t.Errorf("CHAT_ERROR 兜底 = %q, 期望 agent request failed", got)
	}

	// ④ 有 content 键取字面
	if got = MessageToText(&schema.Message{Payload: map[string]any{"content": "hello"}}); got != "hello" {
		t.Errorf("content 分支 = %q, 期望 hello", got)
	}

	// ⑤ 非空 payload 整体 JSON 化（偏差 D5：Python str(dict) 形态不同）
	if got = MessageToText(&schema.Message{Payload: map[string]any{"a": float64(1)}}); got != `{"a":1}` {
		t.Errorf("payload 兜底 = %q, 期望 {\"a\":1}", got)
	}

	// ⑥ 空 payload
	if got = MessageToText(&schema.Message{Payload: map[string]any{}}); got != "" {
		t.Errorf("空 payload = %q, 期望空串", got)
	}

	// ⑦ nil 消息
	if got = MessageToText(nil); got != "" {
		t.Errorf("nil 消息 = %q, 期望空串", got)
	}
}
