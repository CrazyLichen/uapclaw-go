package schema

import (
	"testing"
)

// ──────────────────────────── BuildEventName 测试 ────────────────────────────

// TestBuildEventName 拼接 scope:eventName
func TestBuildEventName(t *testing.T) {
	got := BuildEventName("gateway", "before_chat_request")
	want := "gateway:before_chat_request"
	if got != want {
		t.Errorf("BuildEventName(\"gateway\", \"before_chat_request\") = %q, want %q", got, want)
	}
}

// TestBuildEventName_空scope scope 为空字符串
func TestBuildEventName_空scope(t *testing.T) {
	got := BuildEventName("", "event")
	want := ":event"
	if got != want {
		t.Errorf("BuildEventName(\"\", \"event\") = %q, want %q", got, want)
	}
}

// TestBuildEventName_空eventName eventName 为空字符串
func TestBuildEventName_空eventName(t *testing.T) {
	got := BuildEventName("scope", "")
	want := "scope:"
	if got != want {
		t.Errorf("BuildEventName(\"scope\", \"\") = %q, want %q", got, want)
	}
}

// TestBuildEventName_默认Scope 使用 DefaultScope 拼接
func TestBuildEventName_默认Scope(t *testing.T) {
	got := BuildEventName(DefaultScope, "started")
	want := "_framework:started"
	if got != want {
		t.Errorf("BuildEventName(DefaultScope, \"started\") = %q, want %q", got, want)
	}
}

// ──────────────────────────── ParseEventName 测试 ────────────────────────────

// TestParseEventName 正常解析
func TestParseEventName(t *testing.T) {
	scope, eventName := ParseEventName("gateway:before_chat_request")
	if scope != "gateway" {
		t.Errorf("scope = %q, want %q", scope, "gateway")
	}
	if eventName != "before_chat_request" {
		t.Errorf("eventName = %q, want %q", eventName, "before_chat_request")
	}
}

// TestParseEventName_无冒号 无冒号回退默认 scope
func TestParseEventName_无冒号(t *testing.T) {
	scope, eventName := ParseEventName("before_chat_request")
	if scope != DefaultScope {
		t.Errorf("scope = %q, want %q", scope, DefaultScope)
	}
	if eventName != "before_chat_request" {
		t.Errorf("eventName = %q, want %q", eventName, "before_chat_request")
	}
}

// TestParseEventName_多冒号 多冒号只拆第一个
func TestParseEventName_多冒号(t *testing.T) {
	scope, eventName := ParseEventName("a:b:c")
	if scope != "a" {
		t.Errorf("scope = %q, want %q", scope, "a")
	}
	if eventName != "b:c" {
		t.Errorf("eventName = %q, want %q", eventName, "b:c")
	}
}

// TestParseEventName_与Build往返 往返一致性
func TestParseEventName_与Build往返(t *testing.T) {
	tests := []struct {
		scope     string
		eventName string
	}{
		{"gateway", "before_chat_request"},
		{"agent_server", "memory_before_chat"},
		{DefaultScope, "started"},
		{"a", "b"},
	}
	for _, tt := range tests {
		built := BuildEventName(tt.scope, tt.eventName)
		scope, eventName := ParseEventName(built)
		if scope != tt.scope {
			t.Errorf("ParseEventName(BuildEventName(%q, %q)): scope = %q, want %q", tt.scope, tt.eventName, scope, tt.scope)
		}
		if eventName != tt.eventName {
			t.Errorf("ParseEventName(BuildEventName(%q, %q)): eventName = %q, want %q", tt.scope, tt.eventName, eventName, tt.eventName)
		}
	}
}

// TestParseEventName_空字符串 空字符串输入
func TestParseEventName_空字符串(t *testing.T) {
	scope, eventName := ParseEventName("")
	if scope != DefaultScope {
		t.Errorf("scope = %q, want %q", scope, DefaultScope)
	}
	if eventName != "" {
		t.Errorf("eventName = %q, want %q", eventName, "")
	}
}

// ──────────────────────────── HookEventBase 测试 ────────────────────────────

// TestNewHookEventBase 工厂函数默认 Scope
func TestNewHookEventBase(t *testing.T) {
	h := NewHookEventBase()
	if h.Scope != DefaultScope {
		t.Errorf("NewHookEventBase().Scope = %q, want %q", h.Scope, DefaultScope)
	}
}

// TestHookEventBase_GetEvent GetEvent 方法
func TestHookEventBase_GetEvent(t *testing.T) {
	h := &HookEventBase{Scope: "gateway"}
	got := h.GetEvent("started")
	want := "gateway:started"
	if got != want {
		t.Errorf("GetEvent(\"started\") = %q, want %q", got, want)
	}
}

// TestHookEventBase_GetEvent_默认Scope 默认 Scope 的 GetEvent
func TestHookEventBase_GetEvent_默认Scope(t *testing.T) {
	h := NewHookEventBase()
	got := h.GetEvent("started")
	want := "_framework:started"
	if got != want {
		t.Errorf("GetEvent(\"started\") = %q, want %q", got, want)
	}
}

// TestDefaultScope 常量值
func TestDefaultScope(t *testing.T) {
	if DefaultScope != "_framework" {
		t.Errorf("DefaultScope = %q, want %q", DefaultScope, "_framework")
	}
}

// TestHookEventBase_JSON往返 JSON 序列化往返
func TestHookEventBase_JSON往返(t *testing.T) {
	original := &HookEventBase{Scope: "gateway"}

	data, err := marshalHookEventBase(original)
	if err != nil {
		t.Fatalf("marshalHookEventBase 失败: %v", err)
	}

	decoded, err := unmarshalHookEventBase(data)
	if err != nil {
		t.Fatalf("unmarshalHookEventBase 失败: %v", err)
	}

	if decoded.Scope != original.Scope {
		t.Errorf("decoded.Scope = %q, want %q", decoded.Scope, original.Scope)
	}
}

// TestHookEventBase_JSON往返_默认Scope 默认 Scope 的 JSON 往返
func TestHookEventBase_JSON往返_默认Scope(t *testing.T) {
	original := NewHookEventBase()

	data, err := marshalHookEventBase(original)
	if err != nil {
		t.Fatalf("marshalHookEventBase 失败: %v", err)
	}

	decoded, err := unmarshalHookEventBase(data)
	if err != nil {
		t.Fatalf("unmarshalHookEventBase 失败: %v", err)
	}

	if decoded.Scope != DefaultScope {
		t.Errorf("decoded.Scope = %q, want %q", decoded.Scope, DefaultScope)
	}
}
