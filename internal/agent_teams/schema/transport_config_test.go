package schema

import "testing"

// TestNewMessagerTransportConfig 测试默认消息通信传输配置
func TestNewMessagerTransportConfig(t *testing.T) {
	cfg := NewMessagerTransportConfig()
	if cfg.Backend != "inprocess" {
		t.Errorf("Backend = %q, want %q", cfg.Backend, "inprocess")
	}
	if cfg.TeamName != "default" {
		t.Errorf("TeamName = %q, want %q", cfg.TeamName, "default")
	}
	if cfg.RequestTimeout != 10.0 {
		t.Errorf("RequestTimeout = %f, want 10.0", cfg.RequestTimeout)
	}
}

// TestMessagerTransportConfig_BroadcastTopic 测试广播主题
func TestMessagerTransportConfig_BroadcastTopic(t *testing.T) {
	cfg := MessagerTransportConfig{TeamName: "my-team"}
	topic := cfg.BroadcastTopic()
	expected := "team:my-team:broadcast"
	if topic != expected {
		t.Errorf("BroadcastTopic() = %q, want %q", topic, expected)
	}
}
