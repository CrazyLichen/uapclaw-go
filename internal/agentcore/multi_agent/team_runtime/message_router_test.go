package team_runtime

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewMessageRouter 测试创建消息路由器
func TestNewMessageRouter(t *testing.T) {
	sm := NewSubscriptionManager()
	runtime := &TeamRuntime{teamID: "test-team"}

	router := NewMessageRouter(sm, runtime)
	if router == nil {
		t.Fatal("NewMessageRouter 返回 nil")
	}
	if router.subscriptionManager != sm {
		t.Error("subscriptionManager 未正确设置")
	}
	if router.runtime != runtime {
		t.Error("runtime 未正确设置")
	}
}

// TestMessageRouter_buildAgentSession 测试构建 Agent 会话
func TestMessageRouter_buildAgentSession(t *testing.T) {
	t.Run("无会话 ID 返回 nil", func(t *testing.T) {
		sm := NewSubscriptionManager()
		runtime := &TeamRuntime{teamID: "test-team"}
		router := NewMessageRouter(sm, runtime)

		sess := router.buildAgentSession("", "agent-1")
		if sess != nil {
			t.Errorf("无会话 ID 时应返回 nil，实际 = %v", sess)
		}
	})

	t.Run("有效会话 ID 但无绑定会话返回 nil", func(t *testing.T) {
		sm := NewSubscriptionManager()
		runtime := &TeamRuntime{teamID: "test-team"}
		router := NewMessageRouter(sm, runtime)

		sess := router.buildAgentSession("session-1", "agent-1")
		if sess != nil {
			t.Errorf("无绑定会话时应返回 nil，实际 = %v", sess)
		}
	})
}

// TestToInputsMap 测试消息转换
func TestToInputsMap(t *testing.T) {
	t.Run("map 类型直接返回", func(t *testing.T) {
		input := map[string]any{"key": "value"}
		result := toInputsMap(input)
		if result["key"] != "value" {
			t.Errorf("toInputsMap 应直接返回 map，实际 = %v", result)
		}
	})

	t.Run("非 map 类型包装为 message 键", func(t *testing.T) {
		result := toInputsMap("hello")
		if result["message"] != "hello" {
			t.Errorf("toInputsMap 应将非 map 包装为 message 键，实际 = %v", result)
		}
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────
