package team_runtime

import (
	"context"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewMessageRouter 测试创建消息路由器
func TestNewMessageRouter(t *testing.T) {
	sm := NewSubscriptionManager()
	runtime := &TeamRuntime{teamID: "test-team"}
	executor := &mockAgentExecutor{}

	router := NewMessageRouter(sm, runtime, executor)
	if router == nil {
		t.Error("NewMessageRouter 返回 nil")
	}
	if router.subscriptionManager != sm {
		t.Error("subscriptionManager 未正确设置")
	}
	if router.runtime != runtime {
		t.Error("runtime 未正确设置")
	}
	if router.agentExecutor != executor {
		t.Error("agentExecutor 未正确设置")
	}
}

// TestMessageRouter_RouteP2PMessage 测试 P2P 消息路由
func TestMessageRouter_RouteP2PMessage(t *testing.T) {
	t.Run("成功路由", func(t *testing.T) {
		sm := NewSubscriptionManager()
		runtime := &TeamRuntime{teamID: "test-team"}
		executor := &mockAgentExecutor{result: "p2p-response"}

		router := NewMessageRouter(sm, runtime, executor)

		envelope := NewMessageEnvelope("msg-1", "hello", "sender",
			WithRecipient("recipient"),
			WithSessionID("session-1"),
		)

		result, err := router.RouteP2PMessage(context.Background(), envelope)
		if err != nil {
			t.Errorf("RouteP2PMessage 返回错误: %v", err)
		}
		if result != "p2p-response" {
			t.Errorf("result = %v, want %v", result, "p2p-response")
		}

		// 验证 executor 被调用
		calls := executor.getCalls()
		if len(calls) != 1 {
			t.Errorf("executor 调用次数 = %d, want 1", len(calls))
		}
		if calls[0].agentID != "recipient" {
			t.Errorf("executor agentID = %q, want %q", calls[0].agentID, "recipient")
		}
	})

	t.Run("执行失败", func(t *testing.T) {
		sm := NewSubscriptionManager()
		runtime := &TeamRuntime{teamID: "test-team"}
		executor := &mockAgentExecutor{err: context.DeadlineExceeded}

		router := NewMessageRouter(sm, runtime, executor)

		envelope := NewMessageEnvelope("msg-1", "hello", "sender",
			WithRecipient("recipient"),
		)

		_, err := router.RouteP2PMessage(context.Background(), envelope)
		if err == nil {
			t.Error("执行失败时应返回错误")
		}
	})
}

// TestMessageRouter_RoutePubsubMessage 测试 Pub-Sub 消息路由
func TestMessageRouter_RoutePubsubMessage(t *testing.T) {
	t.Run("无订阅者", func(t *testing.T) {
		sm := NewSubscriptionManager()
		runtime := &TeamRuntime{teamID: "test-team"}
		executor := &mockAgentExecutor{}

		router := NewMessageRouter(sm, runtime, executor)

		envelope := NewMessageEnvelope("msg-1", "hello", "sender",
			WithTopicID("topic-1"),
		)

		err := router.RoutePubsubMessage(context.Background(), envelope)
		if err != nil {
			t.Errorf("RoutePubsubMessage 返回错误: %v", err)
		}
		// 无订阅者，executor 不应被调用
		if len(executor.getCalls()) != 0 {
			t.Errorf("executor 调用次数 = %d, want 0", len(executor.getCalls()))
		}
	})

	t.Run("有订阅者", func(t *testing.T) {
		sm := NewSubscriptionManager()
		sm.Subscribe("agent-1", "topic-1")
		sm.Subscribe("agent-2", "topic-1")
		runtime := &TeamRuntime{teamID: "test-team"}
		executor := &mockAgentExecutor{result: "pubsub-result"}

		router := NewMessageRouter(sm, runtime, executor)

		envelope := NewMessageEnvelope("msg-1", "hello", "sender",
			WithTopicID("topic-1"),
			WithSessionID("session-1"),
		)

		err := router.RoutePubsubMessage(context.Background(), envelope)
		if err != nil {
			t.Errorf("RoutePubsubMessage 返回错误: %v", err)
		}
		// Pub-Sub 是异步并发执行，此处不验证 executor.calls
		// 因为 goroutine 可能尚未完成
	})
}

// TestMessageRouter_buildAgentSession 测试构建 Agent 会话
func TestMessageRouter_buildAgentSession(t *testing.T) {
	t.Run("无会话 ID 返回 nil", func(t *testing.T) {
		sm := NewSubscriptionManager()
		runtime := &TeamRuntime{teamID: "test-team"}
		executor := &mockAgentExecutor{}

		router := NewMessageRouter(sm, runtime, executor)

		sess := router.buildAgentSession("", "agent-1")
		if sess != nil {
			t.Errorf("无会话 ID 时应返回 nil，实际 = %v", sess)
		}
	})

	t.Run("有效会话 ID 但无绑定会话返回 nil", func(t *testing.T) {
		sm := NewSubscriptionManager()
		runtime := &TeamRuntime{teamID: "test-team"}
		executor := &mockAgentExecutor{}

		router := NewMessageRouter(sm, runtime, executor)

		sess := router.buildAgentSession("session-1", "agent-1")
		if sess != nil {
			t.Errorf("无绑定会话时应返回 nil，实际 = %v", sess)
		}
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────
