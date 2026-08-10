package memory

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveEmbeddingConfig 解析嵌入配置。
// 优先级：config 内嵌配置 → 环境变量 → nil
func ResolveEmbeddingConfig(cfg *schema.TeamMemoryConfig) *embedding.EmbeddingConfig {
	if cfg != nil && cfg.EmbeddingConfig != nil {
		return cfg.EmbeddingConfig
	}
	return lite.ResolveEmbeddingConfigFromEnv("", "", "")
}

// ──────────────────────────── 非导出函数 ────────────────────────────
