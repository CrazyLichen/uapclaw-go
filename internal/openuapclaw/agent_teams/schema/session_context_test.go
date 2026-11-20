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
	id := s.GetSessionID()
	if id == "" {
		t.Error("并发设置后 sessionID 不应为空")
	}
}
