package config

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryScopeConfig 记忆作用域配置，定义各类型记忆的提取规则。
//
// 暂不包含 model_cfg / model_client_cfg / embedding_cfg，这些字段在 7.18 中未使用，
// 后续 7.27 LongTermMemory 回填时补充。
//
// Python: openjiuwen/core/memory/config/config.py (MemoryScopeConfig)
type MemoryScopeConfig struct {
	// UserProfileDefinition 用户画像提取规则定义
	UserProfileDefinition string
	// SemanticMemoryDefinition 语义记忆提取规则定义
	SemanticMemoryDefinition string
	// EpisodicMemoryDefinition 情景记忆提取规则定义
	EpisodicMemoryDefinition string
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

// ──────────────────────────── 非导出函数 ────────────────────────────
