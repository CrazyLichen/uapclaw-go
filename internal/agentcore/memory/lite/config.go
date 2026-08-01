package lite

// ──────────────────────────── 结构体 ────────────────────────────

// MemorySettings 记忆配置。⤵️ 回填: 7.4
type MemorySettings struct {
	// Provider 嵌入提供者类型，默认 "openai_compatible"
	Provider string
	// Model 嵌入模型名称，默认 "text-embedding-v3"
	Model string
	// Fallback 嵌入回退策略，默认 "mock"
	Fallback string
	// Sources 记忆来源目录列表，默认 ["memory", "sessions"]
	Sources []string
	// ExtraPaths 额外记忆路径
	ExtraPaths []string
	// Chunking 分块配置，含 tokens 和 overlap
	Chunking map[string]any
	// Query 查询配置，含 max_results/min_score/hybrid
	Query map[string]any
	// Store 存储配置，含 path/vector/fts
	Store map[string]any
	// Sync 同步配置，含 watch/intervalMinutes
	Sync map[string]any
	// Cache 缓存配置
	Cache map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsMemoryEnabled 判断记忆系统是否启用。
// ⤵️ 回填: 7.4 — 当前始终返回 false
func IsMemoryEnabled() bool { return false }

// CreateMemorySettings 创建默认记忆配置。
// ⤵️ 回填: 7.4 — 当前返回零值 MemorySettings
func CreateMemorySettings(workspaceDir string, overrides map[string]any) *MemorySettings {
	return &MemorySettings{}
}
