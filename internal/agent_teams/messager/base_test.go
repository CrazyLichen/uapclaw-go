package messager

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// TestNewMessagerTransportConfig 验证默认配置值
func TestNewMessagerTransportConfig(t *testing.T) {
	cfg := schema.NewMessagerTransportConfig()

	if cfg.Backend != "inprocess" {
		t.Errorf("Backend 期望 inprocess, 实际 %s", cfg.Backend)
	}
	if cfg.TeamName != "default" {
		t.Errorf("TeamName 期望 default, 实际 %s", cfg.TeamName)
	}
	if cfg.RequestTimeout != 10.0 {
		t.Errorf("RequestTimeout 期望 10.0, 实际 %v", cfg.RequestTimeout)
	}
	if cfg.NodeID != "" {
		t.Errorf("NodeID 期望空, 实际 %s", cfg.NodeID)
	}
	if cfg.DirectAddr != "" {
		t.Errorf("DirectAddr 期望空, 实际 %s", cfg.DirectAddr)
	}
}

// TestMessagerTransportConfig_BroadcastTopic 验证广播主题名称
func TestMessagerTransportConfig_BroadcastTopic(t *testing.T) {
	cfg := schema.NewMessagerTransportConfig()
	expected := "team:default:broadcast"
	if got := cfg.BroadcastTopic(); got != expected {
		t.Errorf("BroadcastTopic() 期望 %s, 实际 %s", expected, got)
	}

	// 自定义 team_name
	cfg.TeamName = "myteam"
	expected = "team:myteam:broadcast"
	if got := cfg.BroadcastTopic(); got != expected {
		t.Errorf("BroadcastTopic() 期望 %s, 实际 %s", expected, got)
	}
}

// TestMessagerPeerConfig 验证字段赋值
func TestMessagerPeerConfig(t *testing.T) {
	peer := schema.MessagerPeerConfig{
		AgentID:  "agent-1",
		PeerID:   "peer-1",
		Addrs:    []string{"addr1", "addr2"},
		Metadata: map[string]any{"key": "value"},
	}

	if peer.AgentID != "agent-1" {
		t.Errorf("AgentID 期望 agent-1, 实际 %s", peer.AgentID)
	}
	if peer.PeerID != "peer-1" {
		t.Errorf("PeerID 期望 peer-1, 实际 %s", peer.PeerID)
	}
	if len(peer.Addrs) != 2 {
		t.Errorf("Addrs 长度期望 2, 实际 %d", len(peer.Addrs))
	}
	if peer.Metadata["key"] != "value" {
		t.Errorf("Metadata[key] 期望 value, 实际 %v", peer.Metadata["key"])
	}
}

// TestSubscriptionHandle 验证订阅句柄字段赋值
func TestSubscriptionHandle(t *testing.T) {
	handle := schema.SubscriptionHandle{
		SubscriptionID:  "sub-1",
		Topic:           "team:default:broadcast",
		AgentID:         "agent-1",
		BackendMetadata: map[string]any{"channel": "ch1"},
	}

	if handle.SubscriptionID != "sub-1" {
		t.Errorf("SubscriptionID 期望 sub-1, 实际 %s", handle.SubscriptionID)
	}
	if handle.Topic != "team:default:broadcast" {
		t.Errorf("Topic 期望 team:default:broadcast, 实际 %s", handle.Topic)
	}
}

// TestCreateMessager 验证 CreateMessager 工厂函数
func TestCreateMessager(t *testing.T) {
	CleanupInProcessBus()
	// inprocess 后端
	cfg := schema.NewMessagerTransportConfig()
	m, err := CreateMessager(cfg)
	if err != nil {
		t.Fatalf("CreateMessager 失败: %v", err)
	}
	if m == nil {
		t.Fatal("CreateMessager 返回 nil")
	}
	if _, ok := m.(*InProcessMessager); !ok {
		t.Errorf("CreateMessager 返回类型 %T, want *InProcessMessager", m)
	}

	// 不支持的 backend
	cfg.Backend = "unsupported"
	m, err = CreateMessager(cfg)
	if err == nil {
		t.Error("不支持的 backend 应返回错误")
	}
	if m != nil {
		t.Error("不支持的 backend 应返回 nil")
	}
}
