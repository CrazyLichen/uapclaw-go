package spawn

import (
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/config"
)

// TestSpawnAgentKind 测试枚举值
func TestSpawnAgentKind(t *testing.T) {
	if SpawnAgentKindClassAgent != "class_agent" {
		t.Errorf("SpawnAgentKindClassAgent = %q, want \"class_agent\"", SpawnAgentKindClassAgent)
	}
	if SpawnAgentKindTeamAgent != "team_agent" {
		t.Errorf("SpawnAgentKindTeamAgent = %q, want \"team_agent\"", SpawnAgentKindTeamAgent)
	}
}

// TestDefaultSpawnConfig 测试默认配置
func TestDefaultSpawnConfig(t *testing.T) {
	cfg := DefaultSpawnConfig()
	if cfg.HealthCheckInterval != 5*time.Second {
		t.Errorf("HealthCheckInterval = %v, want 5s", cfg.HealthCheckInterval)
	}
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("ShutdownTimeout = %v, want 10s", cfg.ShutdownTimeout)
	}
	if cfg.HealthCheckTimeout != 3*time.Second {
		t.Errorf("HealthCheckTimeout = %v, want 3s", cfg.HealthCheckTimeout)
	}
}

// TestParseSpawnAgentConfig_ClassAgent 测试解析 CLASS_AGENT 配置
func TestParseSpawnAgentConfig_ClassAgent(t *testing.T) {
	payload := map[string]any{
		"agent_kind":  "class_agent",
		"agent_name":  "search_agent",
		"init_kwargs": map[string]any{"model": "gpt-4"},
		"session_id":  "sess-123",
	}
	cfg, err := ParseSpawnAgentConfig(payload)
	if err != nil {
		t.Fatalf("ParseSpawnAgentConfig 失败: %v", err)
	}
	if cfg.AgentKind != SpawnAgentKindClassAgent {
		t.Errorf("AgentKind = %q, want \"class_agent\"", cfg.AgentKind)
	}
	if cfg.SessionID != "sess-123" {
		t.Errorf("SessionID = %q, want \"sess-123\"", cfg.SessionID)
	}
}

// TestParseSpawnAgentConfig_TeamAgent 测试解析 TEAM_AGENT 配置
func TestParseSpawnAgentConfig_TeamAgent(t *testing.T) {
	payload := map[string]any{
		"agent_kind": "team_agent",
		"payload":    map[string]any{"team_id": "team-1"},
	}
	cfg, err := ParseSpawnAgentConfig(payload)
	if err != nil {
		t.Fatalf("ParseSpawnAgentConfig 失败: %v", err)
	}
	if cfg.AgentKind != SpawnAgentKindTeamAgent {
		t.Errorf("AgentKind = %q, want \"team_agent\"", cfg.AgentKind)
	}
}

// TestSerializeDeserializeRunnerConfig_往返 测试 RunnerConfig 序列化/反序列化往返
func TestSerializeDeserializeRunnerConfig_往返(t *testing.T) {
	original := &config.RunnerConfig{}

	data, err := SerializeRunnerConfig(original)
	if err != nil {
		t.Fatalf("SerializeRunnerConfig 失败: %v", err)
	}

	got, err := DeserializeRunnerConfig(data)
	if err != nil {
		t.Fatalf("DeserializeRunnerConfig 失败: %v", err)
	}
	if got == nil {
		t.Error("DeserializeRunnerConfig 返回 nil")
	}
}

// TestClassAgentSpawnConfig 测试 ClassAgentSpawnConfig 嵌入
func TestClassAgentSpawnConfig(t *testing.T) {
	cfg := ClassAgentSpawnConfig{
		SpawnAgentConfig: SpawnAgentConfig{
			AgentKind: SpawnAgentKindClassAgent,
			SessionID: "sess-1",
		},
		AgentName:  "my_agent",
		InitKwargs: map[string]any{"key": "val"},
	}
	if cfg.AgentName != "my_agent" {
		t.Errorf("AgentName = %q, want \"my_agent\"", cfg.AgentName)
	}
	if cfg.SessionID != "sess-1" {
		t.Errorf("SessionID = %q, want \"sess-1\"", cfg.SessionID)
	}
}
