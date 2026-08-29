package schema

import (
	"context"
	"testing"
)

// ──────────────────────────── SessionState 测试 ────────────────────────────

// TestInitSessionState 测试创建 SessionState
func TestInitSessionState(t *testing.T) {
	s := InitSessionState()
	if s == nil {
		t.Fatal("InitSessionState 不应返回 nil")
	}
	if s.GetSessionID() != "" {
		t.Errorf("初始 sessionID 应为空, 实际=%q", s.GetSessionID())
	}
}

// TestWithSessionState 测试注入和获取 SessionState
func TestWithSessionState(t *testing.T) {
	s := InitSessionState()
	s.SetSessionID("sess-123")
	ctx := WithSessionState(context.Background(), s)
	got := SessionStateFromCtx(ctx)
	if got == nil {
		t.Fatal("SessionStateFromCtx 不应返回 nil")
	}
	if got.GetSessionID() != "sess-123" {
		t.Errorf("GetSessionID = %q, want sess-123", got.GetSessionID())
	}
}

// TestSessionStateFromCtx_无状态 测试未绑定 SessionState 的 context
func TestSessionStateFromCtx_无状态(t *testing.T) {
	got := SessionStateFromCtx(context.Background())
	if got != nil {
		t.Errorf("未绑定时应返回 nil, 实际=%v", got)
	}
}

// TestGetSessionID_从Context 测试从 context 获取 session ID
func TestGetSessionID_从Context(t *testing.T) {
	s := InitSessionState()
	s.SetSessionID("abc")
	ctx := WithSessionState(context.Background(), s)
	if GetSessionID(ctx) != "abc" {
		t.Errorf("GetSessionID = %q, want abc", GetSessionID(ctx))
	}
}

// TestGetSessionID_空Context 测试未绑定 context 返回空字符串
func TestGetSessionID_空Context(t *testing.T) {
	if GetSessionID(context.Background()) != "" {
		t.Errorf("未绑定时应返回空字符串")
	}
}

// TestSetSessionID_并发安全 测试 SetSessionID 并发设置
func TestSetSessionID_并发安全(t *testing.T) {
	s := InitSessionState()
	done := make(chan bool)
	go func() {
		s.SetSessionID("goroutine-1")
		done <- true
	}()
	s.SetSessionID("main")
	<-done
	// 只要没有 race condition 就行
	id := s.GetSessionID()
	if id == "" {
		t.Error("并发设置后 sessionID 不应为空")
	}
}

// ──────────────────────────── TeamMemoryConfig 测试 ────────────────────────────

// TestNewTeamMemoryConfig 测试默认配置值
func TestNewTeamMemoryConfig(t *testing.T) {
	cfg := NewTeamMemoryConfig()
	if cfg.Enabled {
		t.Error("默认 Enabled 应为 false")
	}
	if cfg.Scenario != "general" {
		t.Errorf("Scenario = %q, want general", cfg.Scenario)
	}
	if !cfg.AutoExtract {
		t.Error("默认 AutoExtract 应为 true")
	}
	if !cfg.SharedMemory {
		t.Error("默认 SharedMemory 应为 true")
	}
	if cfg.MemberMemoryPromptMode != "proactive" {
		t.Errorf("MemberMemoryPromptMode = %q, want proactive", cfg.MemberMemoryPromptMode)
	}
	if cfg.TimezoneOffsetHours != 8.0 {
		t.Errorf("TimezoneOffsetHours = %f, want 8.0", cfg.TimezoneOffsetHours)
	}
}

// ──────────────────────────── MessagerTransportConfig 测试 ────────────────────────────

// TestBroadcastTopic 测试广播主题格式
func TestBroadcastTopic(t *testing.T) {
	cfg := MessagerTransportConfig{TeamName: "my_team"}
	want := "team:my_team:broadcast"
	if got := cfg.BroadcastTopic(); got != want {
		t.Errorf("BroadcastTopic() = %q, want %q", got, want)
	}
}

// ──────────────────────────── UnknownHumanAgentError 测试 ────────────────────────────

// TestUnknownHumanAgentError_Error 测试错误消息格式
func TestUnknownHumanAgentError_Error(t *testing.T) {
	e := &UnknownHumanAgentError{
		Sender:     "alice",
		Registered: []string{"bob", "charlie", "alice_admin"},
	}
	msg := e.Error()
	if msg == "" {
		t.Error("Error() 不应返回空字符串")
	}
	// 应包含发送者名字
	if !contains(msg, "alice") {
		t.Errorf("Error() 应包含 'alice', 实际=%q", msg)
	}
}

// ──────────────────────────── i18n 测试 ────────────────────────────

// TestGetLanguage 测试获取当前语言
func TestGetLanguage(t *testing.T) {
	lang := GetLanguage()
	if lang == "" {
		t.Error("GetLanguage() 不应返回空字符串")
	}
}

// TestFormatMap 测试模板替换
func TestFormatMap(t *testing.T) {
	result := formatMap("Hello {name}, you have {count} items", map[string]any{
		"name":  "World",
		"count": 3,
	})
	want := "Hello World, you have 3 items"
	if result != want {
		t.Errorf("formatMap() = %q, want %q", result, want)
	}
}

// TestFormatMap_无占位符 测试无占位符模板
func TestFormatMap_无占位符(t *testing.T) {
	result := formatMap("no placeholders", map[string]any{"key": "val"})
	if result != "no placeholders" {
		t.Errorf("formatMap() = %q, want no placeholders", result)
	}
}

// TestFormatMap_空kwargs 测试空 kwargs
func TestFormatMap_空kwargs(t *testing.T) {
	result := formatMap("Hello {name}", map[string]any{})
	if result != "Hello {name}" {
		t.Errorf("formatMap() = %q, want Hello {name}", result)
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsStr(s, substr))
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
