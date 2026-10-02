package config

import (
	"encoding/json"
	"fmt"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryScopeConfig 记忆作用域配置，定义各类型记忆的提取规则。
//
// Python: openjiuwen/core/memory/config/config.py (MemoryScopeConfig)
type MemoryScopeConfig struct {
	// UserProfileDefinition 用户画像提取规则定义
	UserProfileDefinition string
	// SemanticMemoryDefinition 语义记忆提取规则定义
	SemanticMemoryDefinition string
	// EpisodicMemoryDefinition 情景记忆提取规则定义
	EpisodicMemoryDefinition string
	// ModelCfg 模型请求配置（7.27 回填时使用）
	// Python: model_cfg: ModelRequestConfig = None
	ModelCfg *llmschema.ModelRequestConfig
	// ModelClientCfg 模型客户端配置（7.27 回填时使用）
	// Python: model_client_cfg: ModelClientConfig = None
	ModelClientCfg *llmschema.ModelClientConfig
	// EmbeddingCfg 嵌入模型配置（7.27 回填时使用）
	// Python: embedding_cfg: EmbeddingConfig = None
	EmbeddingCfg *embedding.EmbeddingConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultMemoryScopeConfig 返回默认记忆作用域配置。
//
// 默认值对齐 Python MemoryScopeConfig 的字段默认值。
// Python: MemoryScopeConfig(user_profile_definition="...", semantic_memory_definition="...", episodic_memory_definition="...")
func DefaultMemoryScopeConfig() *MemoryScopeConfig {
	return &MemoryScopeConfig{
		UserProfileDefinition:    "用户本人的肯定或否定表述（包含不限于基本身份、兴趣偏好、人际关系、资产状况）",
		SemanticMemoryDefinition: "用户对话中涉及的和时间无明确关系的事实性内容或概念",
		EpisodicMemoryDefinition: "用户对话中涉及的和时间有明确关系的事实性内容或概念",
	}
}

// ToJSON 将 MemoryScopeConfig 序列化为 JSON 字符串。
// 对齐 Python: MemoryScopeConfig.model_dump_json(by_alias=True)
func (c *MemoryScopeConfig) ToJSON() (string, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return "", fmt.Errorf("序列化 MemoryScopeConfig 失败: %w", err)
	}
	return string(data), nil
}

// MemoryScopeConfigFromJSON 从 JSON 字符串反序列化 MemoryScopeConfig。
// 对齐 Python: MemoryScopeConfig.model_validate_json(config_json)
func MemoryScopeConfigFromJSON(data string) (*MemoryScopeConfig, error) {
	cfg := &MemoryScopeConfig{}
	if err := json.Unmarshal([]byte(data), cfg); err != nil {
		return nil, fmt.Errorf("反序列化 MemoryScopeConfig 失败: %w", err)
	}
	return cfg, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
