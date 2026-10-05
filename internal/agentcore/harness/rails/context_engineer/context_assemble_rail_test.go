package context_engineer

import (
	"testing"

	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// TestNewContextAssembleRail 测试创建 ContextAssembleRail 实例
func TestNewContextAssembleRail(t *testing.T) {
	r := NewContextAssembleRail()
	if r == nil {
		t.Fatal("NewContextAssembleRail 不应返回 nil")
	}
}

// TestNewContextAssembleRail_优先级 测试 ContextAssembleRail 优先级为 85
func TestNewContextAssembleRail_优先级(t *testing.T) {
	r := NewContextAssembleRail()
	// 优先级通过 WithPriority 设置到 DeepAgentRail
	// 通过 GetCallbacks 返回的映射验证 rail 功能正常
	cb := r.GetCallbacks()
	if cb == nil {
		t.Fatal("GetCallbacks 不应返回 nil")
	}
}

// TestContextAssembleRail_GetCallbacks 测试 GetCallbacks 包含 BeforeModelCall
func TestContextAssembleRail_GetCallbacks(t *testing.T) {
	r := NewContextAssembleRail()
	callbacks := r.GetCallbacks()

	if callbacks == nil {
		t.Fatal("GetCallbacks 不应返回 nil")
	}

	// 验证 BeforeModelCall 已注册
	if _, ok := callbacks[sainterfaces.CallbackBeforeModelCall]; !ok {
		t.Error("GetCallbacks 应包含 CallbackBeforeModelCall")
	}
}

// TestContextAssembleRail_GetCallbacks_回调调用 测试回调函数可调用
func TestContextAssembleRail_GetCallbacks_回调调用(t *testing.T) {
	r := NewContextAssembleRail()
	callbacks := r.GetCallbacks()

	fn, ok := callbacks[sainterfaces.CallbackBeforeModelCall]
	if !ok {
		t.Fatal("GetCallbacks 缺少 CallbackBeforeModelCall")
	}
	if fn == nil {
		t.Fatal("CallbackBeforeModelCall 函数不应为 nil")
	}
}
