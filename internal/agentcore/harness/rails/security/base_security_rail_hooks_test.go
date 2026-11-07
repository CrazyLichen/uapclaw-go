package security

import (
	"context"
	"testing"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestBaseSecurityRail_Hooks_未声明事件时短路 验证未声明支持的事件直接返回 nil。
func TestBaseSecurityRail_Hooks_未声明事件时短路(t *testing.T) {
	ctx := context.Background()
	r := NewBaseSecurityRail()
	cbc := &agentinterfaces.AgentCallbackContext{}

	hooks := map[string]func() error{
		"BeforeInvoke":    func() error { return r.BeforeInvoke(ctx, cbc) },
		"AfterInvoke":     func() error { return r.AfterInvoke(ctx, cbc) },
		"BeforeToolCall":  func() error { return r.BeforeToolCall(ctx, cbc) },
		"AfterToolCall":   func() error { return r.AfterToolCall(ctx, cbc) },
		"BeforeModelCall": func() error { return r.BeforeModelCall(ctx, cbc) },
		"AfterModelCall":  func() error { return r.AfterModelCall(ctx, cbc) },
	}
	for name, call := range hooks {
		t.Run(name, func(t *testing.T) {
			if err := call(); err != nil {
				t.Fatalf("未声明事件应返回 nil，实际 %v", err)
			}
		})
	}
}

// TestBaseSecurityRail_Hooks_已声明事件走检查 验证声明后路由到安全检查函数。
func TestBaseSecurityRail_Hooks_已声明事件走检查(t *testing.T) {
	ctx := context.Background()
	var called []agentinterfaces.AgentCallbackEvent
	var r *BaseSecurityRail

	r = NewBaseSecurityRail(
		WithSupportedEvents(
			agentinterfaces.CallbackBeforeInvoke,
			agentinterfaces.CallbackAfterInvoke,
			agentinterfaces.CallbackBeforeToolCall,
			agentinterfaces.CallbackAfterToolCall,
			agentinterfaces.CallbackBeforeModelCall,
			agentinterfaces.CallbackAfterModelCall,
		),
		WithSecurityCheckFn(func(_ context.Context, sc *SecurityCheckContext) (SecurityDecision, error) {
			called = append(called, sc.Event)
			return r.Allow(nil), nil
		}),
	)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)

	for _, fn := range []func() error{
		func() error { return r.BeforeInvoke(ctx, cbc) },
		func() error { return r.AfterInvoke(ctx, cbc) },
		func() error { return r.BeforeToolCall(ctx, cbc) },
		func() error { return r.AfterToolCall(ctx, cbc) },
		func() error { return r.BeforeModelCall(ctx, cbc) },
		func() error { return r.AfterModelCall(ctx, cbc) },
	} {
		if err := fn(); err != nil {
			t.Fatalf("检查函数返回 nil 时钩子应返回 nil，实际 %v", err)
		}
	}
	if len(called) != 6 {
		t.Fatalf("6 个钩子应各自触发一次检查，实际 %d 次: %v", len(called), called)
	}
}

// TestBaseSecurityRail_PopLastUserMessage 验证无 ModelContext 时返回 nil。
func TestBaseSecurityRail_PopLastUserMessage(t *testing.T) {
	r := NewBaseSecurityRail()
	cbc := &agentinterfaces.AgentCallbackContext{}
	if got := r.PopLastUserMessage(cbc); got != nil {
		t.Fatalf("无 ModelContext 应返回 nil，实际 %v", got)
	}
}

// TestBaseSecurityRail_PopMatchingMessages 验证无 ModelContext 时返回 nil。
func TestBaseSecurityRail_PopMatchingMessages(t *testing.T) {
	r := NewBaseSecurityRail()
	cbc := &agentinterfaces.AgentCallbackContext{}
	if got := r.PopMatchingMessages(cbc, []string{"secret"}, false); got != nil {
		t.Fatalf("无 ModelContext 应返回 nil，实际 %v", got)
	}
}
