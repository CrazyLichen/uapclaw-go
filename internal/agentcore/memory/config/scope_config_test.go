package config

import (
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

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

// TestDefaultMemoryScopeConfig_新字段为nil 测试补全的新字段默认为 nil
func TestDefaultMemoryScopeConfig_新字段为nil(t *testing.T) {
	cfg := DefaultMemoryScopeConfig()
	assert.Nil(t, cfg.ModelCfg)
	assert.Nil(t, cfg.ModelClientCfg)
	assert.Nil(t, cfg.EmbeddingCfg)
}

// TestMemoryScopeConfig_设置模型配置 测试设置模型配置字段
func TestMemoryScopeConfig_设置模型配置(t *testing.T) {
	cfg := DefaultMemoryScopeConfig()
	modelCfg := &llmschema.ModelRequestConfig{ModelName: "test-model"}
	cfg.ModelCfg = modelCfg
	assert.Equal(t, "test-model", cfg.ModelCfg.ModelName)
}

// TestMemoryScopeConfig_ToJSON和FromJSON 测试 JSON 序列化/反序列化往返。
func TestMemoryScopeConfig_ToJSON和FromJSON(t *testing.T) {
	original := &MemoryScopeConfig{
		UserProfileDefinition:    "测试画像",
		SemanticMemoryDefinition: "测试语义",
		EpisodicMemoryDefinition: "测试情景",
	}
	jsonStr, err := original.ToJSON()
	require.NoError(t, err, "ToJSON 不应返回错误")
	parsed, err := MemoryScopeConfigFromJSON(jsonStr)
	require.NoError(t, err, "MemoryScopeConfigFromJSON 不应返回错误")
	assert.Equal(t, original.UserProfileDefinition, parsed.UserProfileDefinition)
	assert.Equal(t, original.SemanticMemoryDefinition, parsed.SemanticMemoryDefinition)
	assert.Equal(t, original.EpisodicMemoryDefinition, parsed.EpisodicMemoryDefinition)
}

// TestMemoryScopeConfigFromJSON_空输入 测试空 JSON 对象反序列化。
func TestMemoryScopeConfigFromJSON_空输入(t *testing.T) {
	cfg, err := MemoryScopeConfigFromJSON("{}")
	require.NoError(t, err, "空 JSON 不应返回错误")
	assert.Empty(t, cfg.UserProfileDefinition)
}

// TestMemoryScopeConfigToJSON_包含模型配置 测试包含模型配置时的序列化。
func TestMemoryScopeConfigToJSON_包含模型配置(t *testing.T) {
	cfg := &MemoryScopeConfig{
		UserProfileDefinition:    "画像",
		SemanticMemoryDefinition: "语义",
		EpisodicMemoryDefinition: "情景",
		ModelCfg:                 &llmschema.ModelRequestConfig{ModelName: "qwen-max"},
	}
	jsonStr, err := cfg.ToJSON()
	require.NoError(t, err)
	parsed, err := MemoryScopeConfigFromJSON(jsonStr)
	require.NoError(t, err)
	assert.Equal(t, "qwen-max", parsed.ModelCfg.ModelName)
}

// TestMemoryScopeConfigFromJSON_无效JSON 测试无效 JSON 输入返回错误。
func TestMemoryScopeConfigFromJSON_无效JSON(t *testing.T) {
	_, err := MemoryScopeConfigFromJSON("not json")
	assert.Error(t, err, "无效 JSON 应返回错误")
}
