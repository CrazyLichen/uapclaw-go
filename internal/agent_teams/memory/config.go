package memory

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMemoryConfig 团队记忆配置，对齐 Python TeamMemoryConfig
type TeamMemoryConfig struct {
	// Enabled 是否启用团队记忆
	Enabled bool `json:"enabled"`
	// Scenario 记忆场景，"general" | "coding"
	Scenario string `json:"scenario"`
	// EmbeddingConfig 嵌入配置。⤴️ 9.64 回填完成
	EmbeddingConfig *embedding.EmbeddingConfig `json:"-"`
	// AutoExtract 是否自动提取记忆
	AutoExtract bool `json:"auto_extract"`
	// SharedMemory 是否启用共享记忆
	SharedMemory bool `json:"shared_memory"`
	// MemberMemoryPromptMode 成员记忆提示模式
	MemberMemoryPromptMode string `json:"member_memory_prompt_mode"`
	// TimezoneOffsetHours 时区偏移小时数
	TimezoneOffsetHours float64 `json:"timezone_offset_hours"`
	// ParentWorkspacePath 父工作空间路径，不序列化
	ParentWorkspacePath string `json:"-"`
	// TeamMemoryDir 团队记忆目录，不序列化
	TeamMemoryDir string `json:"-"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamMemoryConfig 创建默认团队记忆配置。
// 默认值：enabled=false, scenario="general", auto_extract=true,
// 对齐 Python: shared_memory=true, member_memory_prompt_mode="proactive", timezone_offset_hours=8.0
func NewTeamMemoryConfig() TeamMemoryConfig {
	return TeamMemoryConfig{
		Enabled:                false,
		Scenario:               "general",
		AutoExtract:            true,
		SharedMemory:           true,
		MemberMemoryPromptMode: "proactive",
		TimezoneOffsetHours:    8.0,
	}
}

// ResolveEmbeddingConfig 解析嵌入配置。⤴️ 9.64 回填完成
// 优先级：config 内嵌配置 → 环境变量 → nil
func ResolveEmbeddingConfig(cfg *TeamMemoryConfig) *embedding.EmbeddingConfig {
	if cfg != nil && cfg.EmbeddingConfig != nil {
		return cfg.EmbeddingConfig
	}
	return lite.ResolveEmbeddingConfigFromEnv("", "", "")
}
