package team_runtime

import (
	"context"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCommunicableAgent_BindRuntime 测试绑定运行时
func TestCommunicableAgent_BindRuntime(t *testing.T) {
	t.Run("绑定后可访问运行时", func(t *testing.T) {
		c := NewCommunicableAgent()
		if c.Runtime() != nil {
			t.Error("初始 Runtime 应为 nil")
		}
		if c.IsBound() {
			t.Error("初始 IsBound 应为 false")
		}

		runtime := &TeamRuntime{teamID: "test-team"}
		c.BindRuntime(runtime, "agent-1")

		if c.Runtime() != runtime {
			t.Error("Runtime 绑定失败")
		}
		if !c.IsBound() {
			t.Error("绑定后 IsBound 应为 true")
		}
	})
}

// TestCommunicableAgent_IsBound 测试绑定状态判断
func TestCommunicableAgent_IsBound(t *testing.T) {
	t.Run("初始未绑定", func(t *testing.T) {
		c := NewCommunicableAgent()
		if c.IsBound() {
			t.Error("初始 IsBound 应为 false")
		}
	})

	t.Run("绑定后为 true", func(t *testing.T) {
		c := NewCommunicableAgent()
		runtime := &TeamRuntime{teamID: "test-team"}
		c.BindRuntime(runtime, "agent-1")
		if !c.IsBound() {
			t.Error("绑定后 IsBound 应为 true")
		}
	})

	t.Run("仅 runtime 非 nil 但 agentID 为空时为 false", func(t *testing.T) {
		c := NewCommunicableAgent()
		c.runtime = &TeamRuntime{teamID: "test-team"}
		if c.IsBound() {
			t.Error("agentID 为空时 IsBound 应为 false")
		}
	})

	t.Run("仅 agentID 非空但 runtime 为 nil 时为 false", func(t *testing.T) {
		c := NewCommunicableAgent()
		c.agentID = "agent-1"
		if c.IsBound() {
			t.Error("runtime 为 nil 时 IsBound 应为 false")
		}
	})
}

// TestCommunicableAgent_BindRuntime_幂等绑定 测试相同 runtime 和 agentID 再次绑定
func TestCommunicableAgent_BindRuntime_幂等绑定(t *testing.T) {
	c := NewCommunicableAgent()
	runtime := &TeamRuntime{teamID: "test-team"}
	c.BindRuntime(runtime, "agent-1")

	// 相同 runtime 和 agentID 再次绑定 — 应幂等跳过
	c.BindRuntime(runtime, "agent-1")

	if c.Runtime() != runtime {
		t.Error("幂等绑定后 Runtime 应不变")
	}
	if !c.IsBound() {
		t.Error("幂等绑定后 IsBound 应为 true")
	}
}

// TestCommunicableAgent_BindRuntime_重绑定 测试不同 runtime 再次绑定
func TestCommunicableAgent_BindRuntime_重绑定(t *testing.T) {
	c := NewCommunicableAgent()
	runtime1 := &TeamRuntime{teamID: "team-1"}
	c.BindRuntime(runtime1, "agent-1")

	// 不同 runtime 再次绑定 — 应覆盖并记录 warning
	runtime2 := &TeamRuntime{teamID: "team-2"}
	c.BindRuntime(runtime2, "agent-2")

	if c.Runtime() != runtime2 {
		t.Error("重绑定后 Runtime 应为新值")
	}
	if !c.IsBound() {
		t.Error("重绑定后 IsBound 应为 true")
	}
}

// TestCommunicableAgent_BindRuntime_相同runtime不同agentID 测试相同 runtime 但不同 agentID
func TestCommunicableAgent_BindRuntime_相同runtime不同agentID(t *testing.T) {
	c := NewCommunicableAgent()
	runtime := &TeamRuntime{teamID: "test-team"}
	c.BindRuntime(runtime, "agent-1")

	// 相同 runtime 但不同 agentID — 应覆盖并记录 warning
	c.BindRuntime(runtime, "agent-2")

	if !c.IsBound() {
		t.Error("重绑定后 IsBound 应为 true")
	}
}

// TestCommunicableAgent_Send 测试 P2P 发送
func TestCommunicableAgent_Send(t *testing.T) {
	t.Run("未绑定时返回错误", func(t *testing.T) {
		c := NewCommunicableAgent()
		_, err := c.Send(context.Background(), map[string]any{"msg": "hello"}, "recipient")
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
		runtime.running.Store(true)
		runtime.mu.Unlock()

		// 注册 recipient agent 和 sender agent
		runtime.agentCards["recipient"] = nil
		runtime.agentCards["sender"] = nil

		c.BindRuntime(runtime, "sender")

		result, err := c.Send(context.Background(), map[string]any{"msg": "hello"}, "recipient")
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
		err := c.Publish(context.Background(), map[string]any{"msg": "hello"}, "topic-1")
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
		runtime.running.Store(true)
		runtime.mu.Unlock()

		c.BindRuntime(runtime, "sender")

		// 注册 sender agent
		runtime.agentCards["sender"] = nil

		err := c.Publish(context.Background(), map[string]any{"msg": "hello"}, "topic-1")
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
	var _ Communicable = (*CommunicableAgent)(nil)
	var _ RuntimeBindable = (*CommunicableAgent)(nil)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
