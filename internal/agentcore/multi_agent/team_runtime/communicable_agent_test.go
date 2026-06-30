package team_runtime

import (
	"context"
	"testing"

	maschema "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCommunicableAgent_BindRuntime 测试绑定运行时
func TestCommunicableAgent_BindRuntime(t *testing.T) {
	t.Run("绑定后可访问运行时", func(t *testing.T) {
		c := NewCommunicableAgent()
		if c.Runtime() != nil {
			t.Error("初始 Runtime 应为 nil")
		}
		if c.AgentID() != "" {
			t.Error("初始 AgentID 应为空")
		}

		runtime := &TeamRuntime{teamID: "test-team"}
		c.BindRuntime(runtime, "agent-1")

		if c.Runtime() != runtime {
			t.Error("Runtime 绑定失败")
		}
		if c.AgentID() != "agent-1" {
			t.Errorf("AgentID = %q, want %q", c.AgentID(), "agent-1")
		}
	})
}

// TestCommunicableAgent_Send 测试 P2P 发送
func TestCommunicableAgent_Send(t *testing.T) {
	t.Run("未绑定时返回错误", func(t *testing.T) {
		c := NewCommunicableAgent()
		_, err := c.Send(context.Background(), "hello", "recipient")
		if err == nil {
			t.Error("未绑定时 Send 应返回错误")
		}
	})

	t.Run("绑定后委托运行时", func(t *testing.T) {
		c := NewCommunicableAgent()
		// 创建运行时并绑定
		config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
		runtime := NewTeamRuntime(*config)
		// 设置 mock 消息总线
		mockBus := newMockMessageBus()
		mockBus.sendResult = "response"
		runtime.SetMessageBus(mockBus)
		runtime.mu.Lock()
		runtime.running = true
		runtime.mu.Unlock()

		// 注册 recipient agent
		runtime.agentCards["recipient"] = nil

		c.BindRuntime(runtime, "sender")

		result, err := c.Send(context.Background(), "hello", "recipient")
		if err != nil {
			t.Errorf("Send 返回错误: %v", err)
		}
		if result != "response" {
			t.Errorf("Send 结果 = %v, want %v", result, "response")
		}
	})
}

// TestCommunicableAgent_Publish 测试 Pub-Sub 发布
func TestCommunicableAgent_Publish(t *testing.T) {
	t.Run("未绑定时返回错误", func(t *testing.T) {
		c := NewCommunicableAgent()
		err := c.Publish(context.Background(), "hello", "topic-1")
		if err == nil {
			t.Error("未绑定时 Publish 应返回错误")
		}
	})

	t.Run("绑定后委托运行时", func(t *testing.T) {
		c := NewCommunicableAgent()
		config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
		runtime := NewTeamRuntime(*config)
		mockBus := newMockMessageBus()
		runtime.SetMessageBus(mockBus)
		runtime.mu.Lock()
		runtime.running = true
		runtime.mu.Unlock()

		c.BindRuntime(runtime, "sender")

		err := c.Publish(context.Background(), "hello", "topic-1")
		if err != nil {
			t.Errorf("Publish 返回错误: %v", err)
		}
	})
}

// TestCommunicableAgent_Subscribe 测试订阅
func TestCommunicableAgent_Subscribe(t *testing.T) {
	t.Run("未绑定时返回错误", func(t *testing.T) {
		c := NewCommunicableAgent()
		err := c.Subscribe(context.Background(), "topic-1")
		if err == nil {
			t.Error("未绑定时 Subscribe 应返回错误")
		}
	})

	t.Run("绑定后委托运行时", func(t *testing.T) {
		c := NewCommunicableAgent()
		config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
		runtime := NewTeamRuntime(*config)
		mockBus := newMockMessageBus()
		runtime.SetMessageBus(mockBus)

		c.BindRuntime(runtime, "agent-1")

		err := c.Subscribe(context.Background(), "topic-1")
		if err != nil {
			t.Errorf("Subscribe 返回错误: %v", err)
		}
	})
}

// TestCommunicableAgent_Unsubscribe 测试取消订阅
func TestCommunicableAgent_Unsubscribe(t *testing.T) {
	t.Run("未绑定时返回错误", func(t *testing.T) {
		c := NewCommunicableAgent()
		err := c.Unsubscribe(context.Background(), "topic-1")
		if err == nil {
			t.Error("未绑定时 Unsubscribe 应返回错误")
		}
	})

	t.Run("绑定后委托运行时", func(t *testing.T) {
		c := NewCommunicableAgent()
		config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
		runtime := NewTeamRuntime(*config)
		mockBus := newMockMessageBus()
		runtime.SetMessageBus(mockBus)

		c.BindRuntime(runtime, "agent-1")

		err := c.Unsubscribe(context.Background(), "topic-1")
		if err != nil {
			t.Errorf("Unsubscribe 返回错误: %v", err)
		}
	})
}

// TestCommunicableAgent_接口满足 测试编译时接口满足
func TestCommunicableAgent_接口满足(t *testing.T) {
	var _ maschema.Communicable = (*CommunicableAgent)(nil)
	var _ RuntimeBindable = (*CommunicableAgent)(nil)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
