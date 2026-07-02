package team_runtime

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewMessageBusConfig 测试消息总线配置构造
func TestNewMessageBusConfig(t *testing.T) {
	t.Run("默认值", func(t *testing.T) {
		cfg := NewMessageBusConfig()
		if cfg.MaxQueueSize != defaultMaxQueueSize {
			t.Errorf("MaxQueueSize = %d, want %d", cfg.MaxQueueSize, defaultMaxQueueSize)
		}
		if cfg.ProcessTimeout != defaultProcessTimeout {
			t.Errorf("ProcessTimeout = %f, want %f", cfg.ProcessTimeout, defaultProcessTimeout)
		}
	})

	t.Run("自定义选项", func(t *testing.T) {
		cfg := NewMessageBusConfig(
			WithMaxQueueSize(500),
			WithProcessTimeout(60.0),
			WithTeamID("my-team"),
		)
		if cfg.MaxQueueSize != 500 {
			t.Errorf("MaxQueueSize = %d, want 500", cfg.MaxQueueSize)
		}
		if cfg.ProcessTimeout != 60.0 {
			t.Errorf("ProcessTimeout = %f, want 60.0", cfg.ProcessTimeout)
		}
		if cfg.TeamID != "my-team" {
			t.Errorf("TeamID = %q, want %q", cfg.TeamID, "my-team")
		}
	})
}

// TestNewMessageBus 测试创建消息总线
func TestNewMessageBus(t *testing.T) {
	config := NewMessageBusConfig(WithTeamID("test-team"))
	runtime := &TeamRuntime{teamID: "test-team"}

	bus, err := NewMessageBus(*config, runtime)
	if err != nil {
		t.Fatalf("NewMessageBus 返回错误: %v", err)
	}
	if bus == nil {
		t.Fatal("NewMessageBus 返回 nil")
	}
	if bus.teamID != "test-team" {
		t.Errorf("teamID = %q, want %q", bus.teamID, "test-team")
	}
	if bus.running {
		t.Error("新创建的消息总线不应处于运行状态")
	}
}

// TestMessageBus_StartStop 测试消息总线启停
func TestMessageBus_StartStop(t *testing.T) {
	config := NewMessageBusConfig(WithTeamID("test-team"))
	runtime := &TeamRuntime{teamID: "test-team"}
	bus, err := NewMessageBus(*config, runtime)
	if err != nil {
		t.Fatalf("NewMessageBus 返回错误: %v", err)
	}

	t.Run("启动", func(t *testing.T) {
		err := bus.Start(context.Background())
		if err != nil {
			t.Errorf("Start 返回错误: %v", err)
		}
		if !bus.running {
			t.Error("启动后 running 应为 true")
		}
	})

	t.Run("重复启动", func(t *testing.T) {
		err := bus.Start(context.Background())
		if err != nil {
			t.Errorf("重复启动应返回 nil，实际: %v", err)
		}
	})

	t.Run("停止", func(t *testing.T) {
		err := bus.Stop(context.Background())
		if err != nil {
			t.Errorf("Stop 返回错误: %v", err)
		}
		if bus.running {
			t.Error("停止后 running 应为 false")
		}
	})

	t.Run("重复停止", func(t *testing.T) {
		err := bus.Stop(context.Background())
		if err != nil {
			t.Errorf("重复停止应返回 nil，实际: %v", err)
		}
	})
}

// TestMessageBus_Subscriptions 测试订阅管理
func TestMessageBus_Subscriptions(t *testing.T) {
	config := NewMessageBusConfig(WithTeamID("test-team"))
	runtime := &TeamRuntime{teamID: "test-team"}
	bus, err := NewMessageBus(*config, runtime)
	if err != nil {
		t.Fatalf("NewMessageBus 返回错误: %v", err)
	}

	t.Run("添加订阅", func(t *testing.T) {
		bus.AddSubscription("agent-1", "topic-1")
		if bus.GetSubscriptionCount() != 1 {
			t.Errorf("GetSubscriptionCount = %d, want 1", bus.GetSubscriptionCount())
		}
	})

	t.Run("重复添加", func(t *testing.T) {
		bus.AddSubscription("agent-1", "topic-1") // 重复
		if bus.GetSubscriptionCount() != 1 {
			t.Errorf("重复添加后 GetSubscriptionCount = %d, want 1", bus.GetSubscriptionCount())
		}
	})

	t.Run("添加多个", func(t *testing.T) {
		bus.AddSubscription("agent-1", "topic-2")
		bus.AddSubscription("agent-2", "topic-1")
		count := bus.GetSubscriptionCount()
		if count != 3 {
			t.Errorf("GetSubscriptionCount = %d, want 3", count)
		}
	})

	t.Run("移除订阅", func(t *testing.T) {
		bus.RemoveSubscription("agent-1", "topic-1")
		count := bus.GetSubscriptionCount()
		if count != 2 {
			t.Errorf("GetSubscriptionCount = %d, want 2", count)
		}
	})

	t.Run("移除所有订阅", func(t *testing.T) {
		bus.RemoveAllSubscriptions("agent-1")
		count := bus.GetSubscriptionCount()
		if count != 1 {
			t.Errorf("GetSubscriptionCount = %d, want 1", count)
		}
	})

	t.Run("列出订阅", func(t *testing.T) {
		result := bus.ListSubscriptions("agent-2")
		if result == nil {
			t.Error("ListSubscriptions 不应返回 nil")
		}
	})
}

// TestMessageBus_Send未启动 测试未启动时发送消息
func TestMessageBus_Send未启动(t *testing.T) {
	config := NewMessageBusConfig(WithTeamID("test-team"))
	runtime := &TeamRuntime{teamID: "test-team"}
	bus, err := NewMessageBus(*config, runtime)
	if err != nil {
		t.Fatalf("NewMessageBus 返回错误: %v", err)
	}

	_, err = bus.Send(context.Background(), "hello", "recipient", "sender", "session-1", 30.0)
	if err == nil {
		t.Error("未启动时 Send 应返回错误")
	}
}

// TestMessageBus_Publish未启动 测试未启动时发布消息
func TestMessageBus_Publish未启动(t *testing.T) {
	config := NewMessageBusConfig(WithTeamID("test-team"))
	runtime := &TeamRuntime{teamID: "test-team"}
	bus, err := NewMessageBus(*config, runtime)
	if err != nil {
		t.Fatalf("NewMessageBus 返回错误: %v", err)
	}

	err = bus.Publish(context.Background(), "hello", "topic-1", "sender", "session-1")
	if err == nil {
		t.Error("未启动时 Publish 应返回错误")
	}
}

// TestMessageBus_getP2PTopic 测试 P2P topic 命名
func TestMessageBus_getP2PTopic(t *testing.T) {
	config := NewMessageBusConfig(WithTeamID("team-1"))
	runtime := &TeamRuntime{teamID: "team-1"}
	bus, err := NewMessageBus(*config, runtime)
	if err != nil {
		t.Fatalf("NewMessageBus 返回错误: %v", err)
	}

	topic := bus.getP2PTopic("session-1")
	want := "team-1_session-1__p2p__"
	if topic != want {
		t.Errorf("getP2PTopic = %q, want %q", topic, want)
	}
}

// TestMessageBus_getPubsubTopic 测试 Pub-Sub topic 命名
func TestMessageBus_getPubsubTopic(t *testing.T) {
	config := NewMessageBusConfig(WithTeamID("team-1"))
	runtime := &TeamRuntime{teamID: "team-1"}
	bus, err := NewMessageBus(*config, runtime)
	if err != nil {
		t.Fatalf("NewMessageBus 返回错误: %v", err)
	}

	topic := bus.getPubsubTopic("session-1")
	want := "team-1_session-1__pubsub__"
	if topic != want {
		t.Errorf("getPubsubTopic = %q, want %q", topic, want)
	}
}

// TestMessageBus_extractEnvelopeFromPayload 测试信封提取
func TestMessageBus_extractEnvelopeFromPayload(t *testing.T) {
	bus := &MessageBus{}

	t.Run("直接信封", func(t *testing.T) {
		envelope := NewMessageEnvelope("msg-1", "hello", "sender", WithRecipient("recipient"))
		payload := map[string]any{"envelope": envelope}

		extracted, err := bus.extractEnvelopeFromPayload(payload)
		if err != nil {
			t.Errorf("extractEnvelopeFromPayload 返回错误: %v", err)
		}
		if extracted.MessageID != "msg-1" {
			t.Errorf("MessageID = %q, want %q", extracted.MessageID, "msg-1")
		}
	})

	t.Run("缺少 envelope 字段", func(t *testing.T) {
		payload := map[string]any{"other": "data"}

		_, err := bus.extractEnvelopeFromPayload(payload)
		if err == nil {
			t.Error("缺少 envelope 字段时应返回错误")
		}
	})
}

// TestMessageBus_CleanupSession 测试会话清理
func TestMessageBus_CleanupSession(t *testing.T) {
	config := NewMessageBusConfig(WithTeamID("test-team"))
	runtime := &TeamRuntime{teamID: "test-team"}
	bus, err := NewMessageBus(*config, runtime)
	if err != nil {
		t.Fatalf("NewMessageBus 返回错误: %v", err)
	}
	_ = bus.Start(context.Background())

	// CleanupSession 不应返回错误（即使无活跃订阅）
	err = bus.CleanupSession(context.Background(), "session-1")
	if err != nil {
		t.Errorf("CleanupSession 返回错误: %v", err)
	}

	_ = bus.Stop(context.Background())
}

// TestContainsP2PMarker 测试 P2P 标记判断辅助函数
func TestContainsP2PMarker(t *testing.T) {
	tests := []struct {
		topic string
		want  bool
	}{
		{"team-1__p2p__session-1", true},
		{"team-1__pubsub__session-1", false},
		{"short", false},
	}
	for _, tt := range tests {
		got := containsP2PMarker(tt.topic)
		if got != tt.want {
			t.Errorf("containsP2PMarker(%q) = %v, want %v", tt.topic, got, tt.want)
		}
	}
}

// TestContainsSubstring 测试子串判断辅助函数
func TestContainsSubstring(t *testing.T) {
	tests := []struct {
		topic  string
		substr string
		want   bool
	}{
		{"team-1__p2p__session-1", "__p2p__", true},
		{"team-1__pubsub__session-1", "__pubsub__", true},
		{"team-1__pubsub__session-1", "__p2p__", false},
		{"short", "__p2p__", false},
	}
	for _, tt := range tests {
		got := containsSubstring(tt.topic, tt.substr)
		if got != tt.want {
			t.Errorf("containsSubstring(%q, %q) = %v, want %v", tt.topic, tt.substr, got, tt.want)
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestMessageBus_ensureSubscription 测试确保订阅
func TestMessageBus_ensureSubscription(t *testing.T) {
	config := NewMessageBusConfig(WithTeamID("test-team"))
	runtime := &TeamRuntime{teamID: "test-team"}
	bus, err := NewMessageBus(*config, runtime)
	if err != nil {
		t.Fatalf("NewMessageBus 返回错误: %v", err)
	}
	_ = bus.Start(context.Background())
	defer func() { _ = bus.Stop(context.Background()) }()

	t.Run("P2P topic 订阅", func(t *testing.T) {
		topic := bus.getP2PTopic("session-1")
		err := bus.ensureSubscription(context.Background(), topic)
		if err != nil {
			t.Errorf("ensureSubscription 返回错误: %v", err)
		}
		sub, ok := bus.activeSubscriptions[topic]
		if !ok {
			t.Error("订阅未添加到 activeSubscriptions")
		}
		if !sub.IsActive() {
			t.Error("订阅应处于活跃状态")
		}
	})

	t.Run("重复订阅同一 topic", func(t *testing.T) {
		topic := bus.getP2PTopic("session-1")
		err := bus.ensureSubscription(context.Background(), topic)
		if err != nil {
			t.Errorf("重复 ensureSubscription 返回错误: %v", err)
		}
	})

	t.Run("Pub-Sub topic 订阅", func(t *testing.T) {
		topic := bus.getPubsubTopic("session-2")
		err := bus.ensureSubscription(context.Background(), topic)
		if err != nil {
			t.Errorf("ensureSubscription 返回错误: %v", err)
		}
	})
}

// TestMessageBus_handleP2PMessage_无效信封 测试 P2P 消息处理无效信封
// P2P 模式下无效信封应返回 BuildError(StatusMessageQueueMessageProcessExecutionError)
func TestMessageBus_handleP2PMessage_无效信封(t *testing.T) {
	config := NewMessageBusConfig(WithTeamID("test-team"))
	runtime := &TeamRuntime{teamID: "test-team"}
	bus, err := NewMessageBus(*config, runtime)
	if err != nil {
		t.Fatalf("NewMessageBus 返回错误: %v", err)
	}

	payload := map[string]any{"other": "data"}
	_, err = bus.handleP2PMessage(context.Background(), payload)
	if err == nil {
		t.Fatal("无效信封应返回错误")
	}
	// 验证错误类型为 BaseError 且状态码为 MESSAGE_QUEUE_MESSAGE_PROCESS_EXECUTION_ERROR
	var baseErr *exception.BaseError
	if !errors.As(err, &baseErr) {
		t.Fatalf("错误应为 *BaseError 类型，实际: %T", err)
	}
	if baseErr.Status().Name() != "MESSAGE_QUEUE_MESSAGE_PROCESS_EXECUTION_ERROR" {
		t.Errorf("状态码名称 = %q, want %q", baseErr.Status().Name(), "MESSAGE_QUEUE_MESSAGE_PROCESS_EXECUTION_ERROR")
	}
}

// TestMessageBus_handlePubsubMessage_无效信封 测试 Pub-Sub 消息处理无效信封
// Pub-Sub 火忘语义：即使信封无效，也仅记日志不返回错误
func TestMessageBus_handlePubsubMessage_无效信封(t *testing.T) {
	config := NewMessageBusConfig(WithTeamID("test-team"))
	runtime := &TeamRuntime{teamID: "test-team"}
	bus, err := NewMessageBus(*config, runtime)
	if err != nil {
		t.Fatalf("NewMessageBus 返回错误: %v", err)
	}

	payload := map[string]any{"other": "data"}
	_, err = bus.handlePubsubMessage(context.Background(), payload)
	// 火忘语义：即使信封无效也不返回错误
	if err != nil {
		t.Errorf("Pub-Sub 火忘语义下不应返回错误，实际: %v", err)
	}
}

// TestMessageBus_buildEnvelopePayload 测试构建信封 payload
func TestMessageBus_buildEnvelopePayload(t *testing.T) {
	bus := &MessageBus{}
	envelope := NewMessageEnvelope("msg-1", "hello", "sender")
	payload := bus.buildEnvelopePayload(envelope)

	if _, ok := payload["envelope"]; !ok {
		t.Error("payload 中应包含 envelope 字段")
	}
}

// TestMessageBus_extractEnvelopeFromPayload_JSON 测试信封提取 JSON 路径
func TestMessageBus_extractEnvelopeFromPayload_JSON(t *testing.T) {
	bus := &MessageBus{}

	// JSON map 路径（非 *MessageEnvelope 类型）
	envelopeMap := map[string]any{
		"MessageID": "msg-json",
		"Message":   "hello",
		"Sender":    "sender",
		"Recipient": "recipient",
	}
	payload := map[string]any{"envelope": envelopeMap}

	extracted, err := bus.extractEnvelopeFromPayload(payload)
	if err != nil {
		t.Errorf("extractEnvelopeFromPayload JSON 路径返回错误: %v", err)
	}
	if extracted.MessageID != "msg-json" {
		t.Errorf("MessageID = %q, want %q", extracted.MessageID, "msg-json")
	}
}

// TestMessageBus_Stop_有活跃订阅 测试停止时清理活跃订阅
func TestMessageBus_Stop_有活跃订阅(t *testing.T) {
	config := NewMessageBusConfig(WithTeamID("test-team"))
	runtime := &TeamRuntime{teamID: "test-team"}
	bus, err := NewMessageBus(*config, runtime)
	if err != nil {
		t.Fatalf("NewMessageBus 返回错误: %v", err)
	}
	_ = bus.Start(context.Background())

	// 创建订阅
	topic := bus.getP2PTopic("session-1")
	_ = bus.ensureSubscription(context.Background(), topic)

	err = bus.Stop(context.Background())
	if err != nil {
		t.Errorf("Stop 返回错误: %v", err)
	}
	if len(bus.activeSubscriptions) != 0 {
		t.Error("停止后 activeSubscriptions 应为空")
	}
}

// suppress unused import warning
var _ = time.Second
