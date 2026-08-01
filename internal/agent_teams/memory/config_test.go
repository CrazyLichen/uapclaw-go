package memory

import (
	"encoding/json"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// TestNewTeamMemoryConfig 验证默认配置值
func TestNewTeamMemoryConfig(t *testing.T) {
	cfg := NewTeamMemoryConfig()

	if cfg.Enabled != false {
		t.Errorf("Enabled 期望 false, 实际 %v", cfg.Enabled)
	}
	if cfg.Scenario != "general" {
		t.Errorf("Scenario 期望 general, 实际 %s", cfg.Scenario)
	}
	if cfg.AutoExtract != true {
		t.Errorf("AutoExtract 期望 true, 实际 %v", cfg.AutoExtract)
	}
	if cfg.SharedMemory != true {
		t.Errorf("SharedMemory 期望 true, 实际 %v", cfg.SharedMemory)
	}
	if cfg.MemberMemoryPromptMode != "proactive" {
		t.Errorf("MemberMemoryPromptMode 期望 proactive, 实际 %s", cfg.MemberMemoryPromptMode)
	}
	if cfg.TimezoneOffsetHours != 8.0 {
		t.Errorf("TimezoneOffsetHours 期望 8.0, 实际 %v", cfg.TimezoneOffsetHours)
	}
}

// TestTeamMemoryConfig_JSON序列化 验证不序列化字段被排除
func TestTeamMemoryConfig_JSON序列化(t *testing.T) {
	cfg := NewTeamMemoryConfig()
	cfg.ParentWorkspacePath = "/some/path"
	cfg.TeamMemoryDir = "/some/dir"
	cfg.EmbeddingConfig = &embedding.EmbeddingConfig{ModelName: "test"}

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("序列化失败: %v", err)
	}

	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}

	// json:"-" 字段不应出现在序列化结果中
	for _, field := range []string{"EmbeddingConfig", "ParentWorkspacePath", "TeamMemoryDir"} {
		if _, ok := m[field]; ok {
			t.Errorf("字段 %s 不应出现在序列化结果中", field)
		}
	}

	// 应该存在的字段
	for _, field := range []string{"enabled", "scenario", "auto_extract", "shared_memory", "member_memory_prompt_mode", "timezone_offset_hours"} {
		if _, ok := m[field]; !ok {
			t.Errorf("字段 %s 应出现在序列化结果中", field)
		}
	}
}

// TestResolveEmbeddingConfig 验证回填实现
func TestResolveEmbeddingConfig(t *testing.T) {
	// 无内嵌配置 → 调 env（当前返回 nil）
	cfg := NewTeamMemoryConfig()
	result := ResolveEmbeddingConfig(&cfg)
	if result != nil {
		t.Errorf("Expected nil when no env vars, got %v", result)
	}
	// 有内嵌配置 → 直接返回
	embCfg := &embedding.EmbeddingConfig{ModelName: "test"}
	cfg.EmbeddingConfig = embCfg
	result = ResolveEmbeddingConfig(&cfg)
	if result != embCfg {
		t.Errorf("Expected embCfg, got %v", result)
	}
	// nil config → 返回 nil
	result = ResolveEmbeddingConfig(nil)
	if result != nil {
		t.Errorf("Expected nil for nil config, got %v", result)
	}
}
