package lite

import (
	"os"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ChunkingConfig 分块配置。对齐 Python MemorySettings.chunking
type ChunkingConfig struct {
	// Tokens 分块 token 数，默认 256
	Tokens float64
	// Overlap 重叠 token 数，默认 32
	Overlap float64
}

// HybridConfig 混合搜索配置。对齐 Python MemorySettings.query.hybrid
type HybridConfig struct {
	// Enabled 是否启用混合搜索，默认 true
	Enabled bool
	// VectorWeight 向量搜索权重，默认 0.7
	VectorWeight float64
	// TextWeight 文本搜索权重，默认 0.3
	TextWeight float64
	// CandidateMultiplier 候选倍数，默认 2.0
	CandidateMultiplier float64
}

// QueryConfig 查询配置。对齐 Python MemorySettings.query
type QueryConfig struct {
	// MaxResults 最大结果数，默认 10
	MaxResults float64
	// MinScore 最小相关度分数，默认 0.3
	MinScore float64
	// Hybrid 混合搜索配置
	Hybrid HybridConfig
}

// VectorStoreConfig 向量存储配置。对齐 Python MemorySettings.store.vector
type VectorStoreConfig struct {
	// Enabled 是否启用向量存储，默认 true
	Enabled bool
}

// FtsConfig 全文搜索配置。对齐 Python MemorySettings.store.fts
type FtsConfig struct {
	// Enabled 是否启用全文搜索，默认 true
	Enabled bool
}

// StoreConfig 存储配置。对齐 Python MemorySettings.store
type StoreConfig struct {
	// Path 数据库文件路径，默认 "memory.db"
	Path string
	// Vector 向量存储配置
	Vector VectorStoreConfig
	// Fts 全文搜索配置
	Fts FtsConfig
}

// SyncConfig 同步配置。对齐 Python MemorySettings.sync
type SyncConfig struct {
	// Watch 是否监听文件变更，默认 true
	Watch bool
	// WatchDebounceMs 防抖毫秒数，默认 2000
	WatchDebounceMs int
	// OnSearch 搜索前同步，默认 true
	OnSearch bool
	// OnSessionStart 会话开始时同步，默认 true
	OnSessionStart bool
	// IntervalMinutes 定时同步间隔分钟数，默认 0（禁用）
	IntervalMinutes int
}

// CacheConfig 缓存配置。对齐 Python MemorySettings.cache
type CacheConfig struct {
	// Enabled 是否启用缓存，默认 true
	Enabled bool
	// MaxEntries 最大缓存条目数，默认 10000
	MaxEntries float64
}

// MemorySettings 记忆配置。对齐 Python MemorySettings
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
	// Chunking 分块配置
	Chunking ChunkingConfig
	// Query 查询配置
	Query QueryConfig
	// Store 存储配置
	Store StoreConfig
	// Sync 同步配置
	Sync SyncConfig
	// Cache 缓存配置
	Cache CacheConfig
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// IsMemoryEnabled 判断记忆系统是否启用。对齐 Python is_memory_enabled
func IsMemoryEnabled() bool {
	envEnabled := strings.ToLower(strings.TrimSpace(os.Getenv("MEMORY_ENABLED")))
	if envEnabled == "" {
		return true
	}
	return envEnabled == "true" || envEnabled == "1" || envEnabled == "yes"
}

// CreateMemorySettings 创建默认记忆配置。对齐 Python create_memory_settings
func CreateMemorySettings(workspaceDir string, overrides map[string]any) *MemorySettings {
	s := &MemorySettings{
		Provider:   "openai_compatible",
		Model:      "text-embedding-v3",
		Fallback:   "mock",
		Sources:    []string{"memory", "sessions"},
		ExtraPaths: nil,
		Chunking: ChunkingConfig{
			Tokens:  256,
			Overlap: 32,
		},
		Query: QueryConfig{
			MaxResults: 10,
			MinScore:   0.3,
			Hybrid: HybridConfig{
				Enabled:             true,
				VectorWeight:        0.7,
				TextWeight:          0.3,
				CandidateMultiplier: 2.0,
			},
		},
		Store: StoreConfig{
			Path:   "memory.db",
			Vector: VectorStoreConfig{Enabled: true},
			Fts:    FtsConfig{Enabled: true},
		},
		Sync: SyncConfig{
			Watch:           true,
			WatchDebounceMs: 2000,
			OnSearch:        true,
			OnSessionStart:  true,
			IntervalMinutes: 0,
		},
		Cache: CacheConfig{
			Enabled:    true,
			MaxEntries: 10000,
		},
	}

	for key, value := range overrides {
		switch key {
		case "provider":
			if v, ok := value.(string); ok {
				s.Provider = v
			}
		case "model":
			if v, ok := value.(string); ok {
				s.Model = v
			}
		case "fallback":
			if v, ok := value.(string); ok {
				s.Fallback = v
			}
		case "sources":
			if v, ok := value.([]string); ok {
				s.Sources = v
			} else if v, ok := value.([]any); ok {
				strs := make([]string, 0, len(v))
				for _, item := range v {
					if str, ok := item.(string); ok {
						strs = append(strs, str)
					}
				}
				s.Sources = strs
			}
		case "extra_paths":
			if v, ok := value.([]string); ok {
				s.ExtraPaths = v
			} else if v, ok := value.([]any); ok {
				strs := make([]string, 0, len(v))
				for _, item := range v {
					if str, ok := item.(string); ok {
						strs = append(strs, str)
					}
				}
				s.ExtraPaths = strs
			}
		case "chunking":
			if v, ok := value.(map[string]any); ok {
				if v2, ok := v["tokens"].(float64); ok {
					s.Chunking.Tokens = v2
				} else if v2, ok := v["tokens"].(int); ok {
					s.Chunking.Tokens = float64(v2)
				}
				if v2, ok := v["overlap"].(float64); ok {
					s.Chunking.Overlap = v2
				} else if v2, ok := v["overlap"].(int); ok {
					s.Chunking.Overlap = float64(v2)
				}
			}
		case "query":
			if v, ok := value.(map[string]any); ok {
				if v2, ok := v["max_results"].(float64); ok {
					s.Query.MaxResults = v2
				} else if v2, ok := v["max_results"].(int); ok {
					s.Query.MaxResults = float64(v2)
				}
				if v2, ok := v["min_score"].(float64); ok {
					s.Query.MinScore = v2
				}
				if hybrid, ok := v["hybrid"].(map[string]any); ok {
					if v2, ok := hybrid["enabled"].(bool); ok {
						s.Query.Hybrid.Enabled = v2
					}
					if v2, ok := hybrid["vectorWeight"].(float64); ok {
						s.Query.Hybrid.VectorWeight = v2
					}
					if v2, ok := hybrid["textWeight"].(float64); ok {
						s.Query.Hybrid.TextWeight = v2
					}
					if v2, ok := hybrid["candidateMultiplier"].(float64); ok {
						s.Query.Hybrid.CandidateMultiplier = v2
					}
				}
			}
		case "store":
			if v, ok := value.(map[string]any); ok {
				if v2, ok := v["path"].(string); ok {
					s.Store.Path = v2
				}
				if vec, ok := v["vector"].(map[string]any); ok {
					if v2, ok := vec["enabled"].(bool); ok {
						s.Store.Vector.Enabled = v2
					}
				}
				if fts, ok := v["fts"].(map[string]any); ok {
					if v2, ok := fts["enabled"].(bool); ok {
						s.Store.Fts.Enabled = v2
					}
				}
			}
		case "sync":
			if v, ok := value.(map[string]any); ok {
				if v2, ok := v["watch"].(bool); ok {
					s.Sync.Watch = v2
				}
				if v2, ok := v["watchDebounceMs"].(int); ok {
					s.Sync.WatchDebounceMs = v2
				}
				if v2, ok := v["onSearch"].(bool); ok {
					s.Sync.OnSearch = v2
				}
				if v2, ok := v["onSessionStart"].(bool); ok {
					s.Sync.OnSessionStart = v2
				}
				if v2, ok := v["intervalMinutes"].(int); ok {
					s.Sync.IntervalMinutes = v2
				}
			}
		case "cache":
			if v, ok := value.(map[string]any); ok {
				if v2, ok := v["enabled"].(bool); ok {
					s.Cache.Enabled = v2
				}
				if v2, ok := v["maxEntries"].(float64); ok {
					s.Cache.MaxEntries = v2
				} else if v2, ok := v["maxEntries"].(int); ok {
					s.Cache.MaxEntries = float64(v2)
				}
			}
		}
	}

	return s
}

// ──────────────────────────── 非导出函数 ────────────────────────────
