package schema

import "testing"

// TestNewTeamMemoryConfig 测试默认团队记忆配置
func TestNewTeamMemoryConfig(t *testing.T) {
	cfg := NewTeamMemoryConfig()
	if cfg.Enabled {
		t.Error("默认 Enabled 应为 false")
	}
	if cfg.Scenario != "general" {
		t.Errorf("Scenario = %q, want %q", cfg.Scenario, "general")
	}
	if !cfg.AutoExtract {
		t.Error("默认 AutoExtract 应为 true")
	}
	if !cfg.SharedMemory {
		t.Error("默认 SharedMemory 应为 true")
	}
	if cfg.MemberMemoryPromptMode != "proactive" {
		t.Errorf("MemberMemoryPromptMode = %q, want %q", cfg.MemberMemoryPromptMode, "proactive")
	}
	if cfg.TimezoneOffsetHours != 8.0 {
		t.Errorf("TimezoneOffsetHours = %f, want 8.0", cfg.TimezoneOffsetHours)
	}
}
