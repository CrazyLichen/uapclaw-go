package team_runtime

import (
	"context"
	"fmt"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewRuntimeConfig 测试运行时配置构造
func TestNewRuntimeConfig(t *testing.T) {
	t.Run("默认值", func(t *testing.T) {
		cfg := NewRuntimeConfig()
		if cfg.P2PTimeout != defaultP2PTimeout {
			t.Errorf("P2PTimeout = %f, want %f", cfg.P2PTimeout, defaultP2PTimeout)
		}
		if cfg.MessageBus == nil {
			t.Error("MessageBus 不应为 nil")
		}
	})

	t.Run("自定义选项", func(t *testing.T) {
		cfg := NewRuntimeConfig(
			WithRuntimeTeamID("my-team"),
			WithRuntimeP2PTimeout(60.0),
		)
		if cfg.TeamID != "my-team" {
			t.Errorf("TeamID = %q, want %q", cfg.TeamID, "my-team")
		}
		if cfg.P2PTimeout != 60.0 {
			t.Errorf("P2PTimeout = %f, want 60.0", cfg.P2PTimeout)
		}
	})
}

// TestNewTeamRuntime 测试创建团队运行时
func TestNewTeamRuntime(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)

	if runtime.teamID != "test-team" {
		t.Errorf("teamID = %q, want %q", runtime.teamID, "test-team")
	}
	if runtime.IsRunning() {
		t.Error("新创建的运行时不应处于运行状态")
	}
	if runtime.GetAgentCount() != 0 {
		t.Error("新创建的运行时 Agent 数量应为 0")
	}
}

// TestTeamRuntime_StartStop 测试启停
func TestTeamRuntime_StartStop(t *testing.T) {
	t.Run("未设置消息总线时启动失败", func(t *testing.T) {
		config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
		runtime := NewTeamRuntime(*config)
		err := runtime.Start(context.Background())
		if err == nil {
			t.Error("未设置消息总线时 Start 应返回错误")
		}
	})

	t.Run("设置消息总线后启动成功", func(t *testing.T) {
		config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
		runtime := NewTeamRuntime(*config)
		mockBus := newMockMessageBus()
		runtime.SetMessageBus(mockBus)

		err := runtime.Start(context.Background())
		if err != nil {
			t.Errorf("Start 返回错误: %v", err)
		}
		if !runtime.IsRunning() {
			t.Error("启动后 IsRunning 应为 true")
		}
	})

	t.Run("停止", func(t *testing.T) {
		config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
		runtime := NewTeamRuntime(*config)
		mockBus := newMockMessageBus()
		runtime.SetMessageBus(mockBus)
		_ = runtime.Start(context.Background())

		err := runtime.Stop(context.Background())
		if err != nil {
			t.Errorf("Stop 返回错误: %v", err)
		}
		if runtime.IsRunning() {
			t.Error("停止后 IsRunning 应为 false")
		}
	})
}

// TestTeamRuntime_RegisterAgent 测试 Agent 注册
func TestTeamRuntime_RegisterAgent(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)

	card := agentschema.NewAgentCard(
		schema.WithID("agent-1"),
		schema.WithName("test-agent"),
	)

	err := runtime.RegisterAgent(context.Background(), card, nil)
	if err != nil {
		t.Errorf("RegisterAgent 返回错误: %v", err)
	}

	if !runtime.HasAgent("agent-1") {
		t.Error("HasAgent 应返回 true")
	}

	if runtime.GetAgentCount() != 1 {
		t.Errorf("GetAgentCount = %d, want 1", runtime.GetAgentCount())
	}
}

// TestTeamRuntime_UnregisterAgent 测试 Agent 注销
func TestTeamRuntime_UnregisterAgent(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)

	card := agentschema.NewAgentCard(
		schema.WithID("agent-1"),
		schema.WithName("test-agent"),
	)
	_ = runtime.RegisterAgent(context.Background(), card, nil)

	// 设置 mock 消息总线以测试订阅移除
	mockBus := newMockMessageBus()
	runtime.SetMessageBus(mockBus)

	unregisteredCard, err := runtime.UnregisterAgent(context.Background(), "agent-1")
	if err != nil {
		t.Errorf("UnregisterAgent 返回错误: %v", err)
	}
	if unregisteredCard != card {
		t.Error("返回的 AgentCard 不匹配")
	}

	if runtime.HasAgent("agent-1") {
		t.Error("HasAgent 应返回 false")
	}

	t.Run("注销不存在的 Agent", func(t *testing.T) {
		_, err := runtime.UnregisterAgent(context.Background(), "nonexistent")
		if err == nil {
			t.Error("注销不存在的 Agent 应返回错误")
		}
	})
}

// TestTeamRuntime_GetAgentCard 测试获取 Agent 卡片
func TestTeamRuntime_GetAgentCard(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)

	card := agentschema.NewAgentCard(
		schema.WithID("agent-1"),
		schema.WithName("test-agent"),
	)
	_ = runtime.RegisterAgent(context.Background(), card, nil)

	t.Run("已注册的 Agent", func(t *testing.T) {
		got, err := runtime.GetAgentCard("agent-1")
		if err != nil {
			t.Errorf("GetAgentCard 返回错误: %v", err)
		}
		if got != card {
			t.Error("返回的 AgentCard 不匹配")
		}
	})

	t.Run("不存在的 Agent", func(t *testing.T) {
		_, err := runtime.GetAgentCard("nonexistent")
		if err == nil {
			t.Error("获取不存在的 Agent 应返回错误")
		}
	})
}

// TestTeamRuntime_ListAgents 测试列出 Agent
func TestTeamRuntime_ListAgents(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)

	agents := runtime.ListAgents()
	if len(agents) != 0 {
		t.Errorf("空运行时 ListAgents 长度 = %d, want 0", len(agents))
	}

	card1 := agentschema.NewAgentCard(schema.WithID("agent-1"), schema.WithName("a1"))
	card2 := agentschema.NewAgentCard(schema.WithID("agent-2"), schema.WithName("a2"))
	_ = runtime.RegisterAgent(context.Background(), card1, nil)
	_ = runtime.RegisterAgent(context.Background(), card2, nil)

	agents = runtime.ListAgents()
	if len(agents) != 2 {
		t.Errorf("ListAgents 长度 = %d, want 2", len(agents))
	}
}

// TestTeamRuntime_Subscribe 测试订阅管理
func TestTeamRuntime_Subscribe(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)
	mockBus := newMockMessageBus()
	runtime.SetMessageBus(mockBus)

	err := runtime.Subscribe(context.Background(), "agent-1", "topic-1")
	if err != nil {
		t.Errorf("Subscribe 返回错误: %v", err)
	}

	if mockBus.GetSubscriptionCount() != 1 {
		t.Errorf("GetSubscriptionCount = %d, want 1", mockBus.GetSubscriptionCount())
	}

	err = runtime.Unsubscribe(context.Background(), "agent-1", "topic-1")
	if err != nil {
		t.Errorf("Unsubscribe 返回错误: %v", err)
	}

	if mockBus.GetSubscriptionCount() != 0 {
		t.Errorf("GetSubscriptionCount = %d, want 0", mockBus.GetSubscriptionCount())
	}
}

// TestTeamRuntime_P2PTimeout 测试 P2P 超时配置
func TestTeamRuntime_P2PTimeout(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeP2PTimeout(60.0))
	runtime := NewTeamRuntime(*config)

	if runtime.P2PTimeout() != 60.0 {
		t.Errorf("P2PTimeout = %f, want 60.0", runtime.P2PTimeout())
	}

	runtime.SetP2PTimeout(120.0)
	if runtime.P2PTimeout() != 120.0 {
		t.Errorf("SetP2PTimeout 后 P2PTimeout = %f, want 120.0", runtime.P2PTimeout())
	}
}

// TestTeamRuntime_BindTeamSession 测试团队会话绑定
func TestTeamRuntime_BindTeamSession(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)

	t.Run("绑定 nil 会话", func(t *testing.T) {
		runtime.BindTeamSession(nil)
		// 不应 panic
	})

	t.Run("获取不存在的会话", func(t *testing.T) {
		sess := runtime.GetTeamSession("nonexistent")
		if sess != nil {
			t.Error("不存在的会话应返回 nil")
		}
	})

	t.Run("解绑不存在的会话", func(t *testing.T) {
		runtime.UnbindTeamSession("nonexistent")
		// 不应 panic
	})
}

// TestTeamRuntime_Send未启动 测试未启动时发送消息
func TestTeamRuntime_Send未启动(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)

	_, err := runtime.Send(context.Background(), "hello", "recipient", "sender")
	if err == nil {
		t.Error("未启动时 Send 应返回错误")
	}
}

// TestTeamRuntime_Publish未启动 测试未启动时发布消息
func TestTeamRuntime_Publish未启动(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)

	err := runtime.Publish(context.Background(), "hello", "topic-1", "sender")
	if err == nil {
		t.Error("未启动时 Publish 应返回错误")
	}
}

// TestTeamRuntime_Send接收者不存在 测试发送给不存在的接收者
func TestTeamRuntime_Send接收者不存在(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)
	mockBus := newMockMessageBus()
	runtime.SetMessageBus(mockBus)
	runtime.mu.Lock()
	runtime.running = true
	runtime.mu.Unlock()

	_, err := runtime.Send(context.Background(), "hello", "nonexistent", "sender")
	if err == nil {
		t.Error("发送给不存在的接收者应返回错误")
	}
}

// TestTeamRuntime_ListSubscriptions 测试列出订阅
func TestTeamRuntime_ListSubscriptions(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)

	t.Run("无消息总线", func(t *testing.T) {
		result := runtime.ListSubscriptions("agent-1")
		if result != nil {
			t.Error("无消息总线时应返回 nil")
		}
	})

	t.Run("有消息总线", func(t *testing.T) {
		mockBus := newMockMessageBus()
		mockBus.AddSubscription("agent-1", "topic-1")
		runtime.SetMessageBus(mockBus)

		result := runtime.ListSubscriptions("agent-1")
		if result == nil {
			t.Error("ListSubscriptions 不应返回 nil")
		}
	})
}

// TestTeamRuntime_GetSubscriptionCount 测试获取订阅数
func TestTeamRuntime_GetSubscriptionCount(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)

	t.Run("无消息总线", func(t *testing.T) {
		count := runtime.GetSubscriptionCount()
		if count != 0 {
			t.Errorf("无消息总线时 GetSubscriptionCount = %d, want 0", count)
		}
	})

	t.Run("有消息总线", func(t *testing.T) {
		mockBus := newMockMessageBus()
		mockBus.AddSubscription("agent-1", "topic-1")
		runtime.SetMessageBus(mockBus)

		count := runtime.GetSubscriptionCount()
		if count != 1 {
			t.Errorf("GetSubscriptionCount = %d, want 1", count)
		}
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestTeamRuntime_CleanupSession 测试会话清理
func TestTeamRuntime_CleanupSession(t *testing.T) {
	t.Run("无消息总线", func(t *testing.T) {
		config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
		runtime := NewTeamRuntime(*config)
		err := runtime.CleanupSession(context.Background(), "session-1")
		if err != nil {
			t.Errorf("无消息总线时 CleanupSession 应返回 nil，实际: %v", err)
		}
	})

	t.Run("有消息总线", func(t *testing.T) {
		config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
		runtime := NewTeamRuntime(*config)
		mockBus := newMockMessageBus()
		runtime.SetMessageBus(mockBus)
		err := runtime.CleanupSession(context.Background(), "session-1")
		if err != nil {
			t.Errorf("CleanupSession 返回错误: %v", err)
		}
	})
}

// TestTeamRuntime_BindTeamSession_有效会话 测试绑定有效团队会话
func TestTeamRuntime_BindTeamSession_有效会话(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)

	// 创建真实 AgentTeamSession 测试绑定
	sess := session.NewAgentTeamSession(
		session.WithAgentTeamSessionID("session-1"),
		session.WithAgentTeamTeamID("test-team"),
	)

	runtime.BindTeamSession(sess)

	got := runtime.GetTeamSession("session-1")
	if got != sess {
		t.Error("GetTeamSession 应返回绑定的会话")
	}

	runtime.UnbindTeamSession("session-1")
	got = runtime.GetTeamSession("session-1")
	if got != nil {
		t.Error("UnbindTeamSession 后 GetTeamSession 应返回 nil")
	}
}

// TestTeamRuntime_wrapProvider 测试 provider 包装
func TestTeamRuntime_wrapProvider(t *testing.T) {
	config := NewRuntimeConfig(WithRuntimeTeamID("test-team"))
	runtime := NewTeamRuntime(*config)

	t.Run("Agent 实现 RuntimeBindable", func(t *testing.T) {
		_ = &mockRuntimeBindable{} // 验证 mock 类型
		provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
			// 返回 nil agent，类型断言不会匹配 RuntimeBindable
			return nil, nil
		}

		wrapped := runtime.wrapProvider(provider, "agent-1")
		_, err := wrapped(context.Background(), nil)
		// provider 返回 nil agent，类型断言不会匹配 RuntimeBindable
		if err != nil {
			t.Errorf("wrapProvider 返回错误: %v", err)
		}
	})

	t.Run("provider 返回错误", func(t *testing.T) {
		provider := func(_ context.Context, _ *agentschema.AgentCard) (agentinterfaces.BaseAgent, error) {
			return nil, fmt.Errorf("provider error")
		}

		wrapped := runtime.wrapProvider(provider, "agent-1")
		_, err := wrapped(context.Background(), nil)
		if err == nil {
			t.Error("provider 返回错误时 wrapProvider 应转发错误")
		}
	})
}

// TestWithRuntimeMessageBus 测试消息总线配置选项
func TestWithRuntimeMessageBus(t *testing.T) {
	busConfig := NewMessageBusConfig(WithTeamID("custom-team"))
	cfg := NewRuntimeConfig(WithRuntimeMessageBus(busConfig))

	if cfg.MessageBus != busConfig {
		t.Error("WithRuntimeMessageBus 未正确设置")
	}
}
