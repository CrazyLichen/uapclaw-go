package config

import "testing"

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDefaultMemoryScopeConfig 测试默认记忆作用域配置
func TestDefaultMemoryScopeConfig(t *testing.T) {
	cfg := DefaultMemoryScopeConfig()
	if cfg.UserProfileDefinition == "" {
		t.Error("UserProfileDefinition 不应为空")
	}
	if cfg.SemanticMemoryDefinition == "" {
		t.Error("SemanticMemoryDefinition 不应为空")
	}
	if cfg.EpisodicMemoryDefinition == "" {
		t.Error("EpisodicMemoryDefinition 不应为空")
	}
}

// TestDefaultMemoryScopeConfig_对齐Python默认值 测试默认值与 Python 一致
func TestDefaultMemoryScopeConfig_对齐Python默认值(t *testing.T) {
	cfg := DefaultMemoryScopeConfig()
	// Python: user_profile_definition: str = "用户本人的肯定或否定表述（包含不限于基本身份、兴趣偏好、人际关系、资产状况）"
	expectedUserProfile := "用户本人的肯定或否定表述（包含不限于基本身份、兴趣偏好、人际关系、资产状况）"
	if cfg.UserProfileDefinition != expectedUserProfile {
		t.Errorf("UserProfileDefinition = %q, want %q", cfg.UserProfileDefinition, expectedUserProfile)
	}
	// Python: semantic_memory_definition: str = "用户对话中涉及的和时间无明确关系的事实性内容或概念"
	expectedSemantic := "用户对话中涉及的和时间无明确关系的事实性内容或概念"
	if cfg.SemanticMemoryDefinition != expectedSemantic {
		t.Errorf("SemanticMemoryDefinition = %q, want %q", cfg.SemanticMemoryDefinition, expectedSemantic)
	}
	// Python: episodic_memory_definition: str = "用户对话中涉及的和时间有明确关系的事实性内容或概念"
	expectedEpisodic := "用户对话中涉及的和时间有明确关系的事实性内容或概念"
	if cfg.EpisodicMemoryDefinition != expectedEpisodic {
		t.Errorf("EpisodicMemoryDefinition = %q, want %q", cfg.EpisodicMemoryDefinition, expectedEpisodic)
	}
}
